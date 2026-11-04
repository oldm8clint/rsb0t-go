// The stream view's connection to the panel's existing-session bridge (internal/controlpanel/
// streamclient.go): the view lease and the observation loop, kept alive for as long as the view is
// open. No DOM, no WebGL, no globals: everything it touches is injected, so tools/stream/
// verify-stream-link.mjs drives it with a fake server and a virtual clock.
//
// WHY IT EXISTS. The view used to end itself on the first thing that went wrong: any failed or
// slow request, any 409 while the account relogged (a fleet of hundreds relogs constantly), any
// 410 after the lease ran past its TTL (a terrain decode that took longer than the TTL on a busy
// laptop, or a background tab whose timers were throttled), a stale sequence. It tore the whole
// renderer down and the panel did not notice. Here none of those is fatal: the renderer, its
// models and its last frame stay on screen while the link reopens the lease with backoff and asks
// for a whole scene again. The only fatal answers are the ones a retry cannot fix (no bridge,
// unknown transport).
//
// Phases, reported through onState: opening (first lease), live, reconnecting (the lease was
// lost or the account is between sessions; retrying), paused (the view is hidden: the lease is
// released and nothing is polled until it shows again), ended (stop() or a fatal answer).

export const RETRY_MS = [1000, 2000, 4000, 8000, 10000];
// A frame request that has not answered in this long is abandoned (the server never blocks that
// long on one; a hung connection must not hold the loop).
export const REQUEST_TIMEOUT_MS = 12000;
// Hidden for this long, the view releases its lease and stops observing, then reopens when shown.
// Hidden pages' timers are throttled (to once a minute after five minutes), which no lease TTL
// survives, and nobody sees the frames anyway.
export const HIDDEN_RELEASE_MS = 20000;
// Failed requests in a row on one lease before it is given up and reopened.
const REOPEN_AFTER_FAILS = 3;

class LinkError extends Error {
    constructor(kind, message, status = 0) { super(message); this.kind = kind; this.status = status; }
}

// What a non-OK status from the bridge means (streamclient.go): 410 the lease is gone (another
// viewer took it, or it was dropped), 409 the account is not in game / its session changed / the
// lease is held by another viewer, 400/404/501 are mistakes or a build without the bridge (fatal),
// and any other 5xx is the server having a bad moment (retried).
function classify(status, message) {
    if (status === 410) return new LinkError('lease', message, status);
    if (status === 409) return new LinkError('account', message, status);
    if (status === 429 || (status >= 500 && status !== 501)) return new LinkError('net', message, status);
    return new LinkError('fatal', message, status);
}

export function createStreamLink(o) {
    const now = o.now || (() => Date.now());
    const abort = new AbortController();
    const state = { phase: 'opening', message: '', attempts: 0 };
    const stats = { opens: 0, reopens: 0, requestErrors: 0, leaseLost: 0, pauses: 0, lastError: '', lastOpenAt: 0 };
    let session = null, lastSeq = 0, epoch = null, needFull = true, hiddenSince = null, wake = null, running = null, stopped = false;
    let retry = 0; // consecutive failed opens / lost leases, indexes RETRY_MS
    let lastLoss = ''; // why the lease was last lost, shown while reconnecting

    const sleep = ms => new Promise(resolve => {
        if (abort.signal.aborted) { resolve(); return; }
        const done = () => { clearTimeout(timer); wake = null; resolve(); };
        const timer = setTimeout(done, ms);
        wake = done;
    });
    abort.signal.addEventListener('abort', () => wake?.());

    function emit(phase, message = '') {
        if (state.phase === phase && state.message === message) return;
        state.phase = phase; state.message = message; state.attempts = retry;
        try { o.onState?.({ ...state }); } catch (_) { /* a listener must not stop the loop */ }
    }

    async function openLease() {
        const lease = await o.post('/api/stream/open', { account: o.account, fps: 60, transport: o.transport, owner: o.owner }, abort.signal)
            .catch(error => { throw classifyError(error); });
        if (stopped) { await closeLease(lease?.session); throw new LinkError('stopped', 'closed while opening'); }
        if (!lease?.session) throw new LinkError('fatal', 'The bridge returned no view lease.');
        session = lease.session; lastSeq = 0; epoch = null; needFull = true;
        stats.opens++; stats.lastOpenAt = now();
        o.onOpen?.(lease);
    }

    async function closeLease(id) {
        if (!id) return;
        try { await o.post('/api/stream/close', { session: id }, undefined); } catch (_) { /* it expires server-side */ }
    }

    // classifyError maps a thrown transport error. Statuses arrive as LinkErrors from o.post
    // (o.post throws Error with .status for non-OK answers); anything else is a network error.
    function classifyError(error) {
        if (error instanceof LinkError) return error;
        if (abort.signal.aborted) return new LinkError('stopped', 'closed');
        // The page itself was stalled past the request deadline: nothing is wrong with the bridge.
        if (error?.stalled) return new LinkError('stall', 'this page was busy');
        if (Number.isInteger(error?.status) && error.status > 0) return classify(error.status, error.message || `HTTP ${error.status}`);
        return new LinkError('net', error?.message || String(error));
    }

    // One observation. Returns how long to wait before the next one.
    async function observe() {
        const have = needFull ? 0 : (o.have?.() || 0);
        let reply;
        try { reply = await o.get(`/api/stream/frame?session=${encodeURIComponent(session)}&have=${have}`, abort.signal); }
        catch (error) { throw classifyError(error); }
        if (stopped) return 0;
        let body = null;
        try { body = JSON.parse(reply.text); } catch (_) { /* below */ }
        if (!reply.ok) throw classify(reply.status, body?.error || `HTTP ${reply.status}`);
        if (!body) throw new LinkError('net', 'The bridge answered with something that is not JSON.');
        if (body.throttled) return Math.max(100, body.retryInMs || 200);
        // A frame must continue this lease's own numbering on one session epoch; anything else is
        // another connection's observation and is never applied.
        if (!Number.isSafeInteger(body.seq) || body.seq <= lastSeq || (epoch !== null && body.epoch !== epoch)) throw new LinkError('lease', 'The bridge sent a stale sequence or the session changed.');
        epoch = body.epoch; lastSeq = body.seq;
        try { o.onFrame(body, reply.text); needFull = false; }
        catch (error) { if (!body.delta) throw new LinkError('net', error.message || String(error)); needFull = true; stats.resyncs = (stats.resyncs || 0) + 1; }
        return Math.round(1000 / Math.max(1, o.maxFps?.() || 5));
    }

    // The next retry delay (and the count of retries so far), capped at the last entry.
    const backoff = () => RETRY_MS[Math.min(retry++, RETRY_MS.length - 1)];

    async function run() {
        let fails = 0;
        while (!stopped) {
            try {
                if (!session) {
                    emit('reconnecting', lastLoss);
                    await openLease();
                    fails = 0; retry = 0; lastLoss = '';
                    emit('live', '');
                }
                if (!o.showing()) {
                    if (hiddenSince === null) hiddenSince = now();
                    if (now() - hiddenSince >= HIDDEN_RELEASE_MS && !o.controlHeld?.()) {
                        const id = session; session = null; stats.pauses++;
                        await closeLease(id);
                        lastLoss = ''; emit('paused', 'Hidden: observation paused; it resumes when this view is shown.');
                        while (!stopped && !o.showing()) await sleep(1000);
                        hiddenSince = null;
                        continue;
                    }
                } else hiddenSince = null;
                const wait = await observe();
                fails = 0; retry = 0;
                if (!stopped) await sleep(o.showing() ? wait : Math.max(wait, 1000));
            } catch (error) {
                if (stopped || error.kind === 'stopped') break;
                if (error.kind === 'stall') { stats.stalls = (stats.stalls || 0) + 1; continue; }
                stats.lastError = lastLoss = error.message;
                if (error.kind === 'fatal') { emit('ended', error.message); stopped = true; break; }
                // The lease is gone or the account is between sessions: reopen. A lease that merely
                // lapsed is retaken at once the first time; a missing account is waited for with
                // backoff. A few failed requests in a row on a live lease also reopen it.
                const lost = error.kind === 'lease' || error.kind === 'account';
                if (lost) stats.leaseLost++; else stats.requestErrors++;
                if (lost || ++fails >= REOPEN_AFTER_FAILS) {
                    if (session) { stats.reopens++; o.onLost?.(error); }
                    session = null; fails = 0;
                }
                emit('reconnecting', error.message);
                const immediate = error.kind === 'lease' && retry === 0;
                const wait = immediate ? 0 : backoff();
                if (immediate) retry = 1;
                if (wait) await sleep(wait);
            }
        }
        const id = session; session = null;
        await closeLease(id);
        if (state.phase !== 'ended') emit('ended', state.message);
    }

    return {
        get session() { return session; },
        get phase() { return state.phase; },
        get epoch() { return epoch; },
        get seq() { return lastSeq; },
        stats,
        // start opens the FIRST lease and then keeps it alive. It rejects, with the bridge's own
        // message and an HTTP status, when that first open fails, so a caller can show why (another
        // viewer, the account is not in game) instead of retrying behind a blank view. Every later
        // loss is retried by the loop.
        async start() {
            emit('opening', '');
            await openLease();
            retry = 0; emit('live', '');
            running = run();
            return session;
        },
        // Ask for a whole scene on the next observation (a delta the view could not apply).
        resync() { needFull = true; },
        // The view's visibility or control state changed: look again now.
        poke() { wake?.(); },
        async stop() {
            if (stopped && !running) return;
            stopped = true; abort.abort(); wake?.();
            await running;
        },
        // The browser is unloading: release the lease without waiting for anything (a keepalive
        // request outlives the page). Used from pagehide.
        releaseNow() { const id = session; session = null; if (id) o.beacon?.('/api/stream/close', { session: id }); stopped = true; abort.abort(); }
    };
}
