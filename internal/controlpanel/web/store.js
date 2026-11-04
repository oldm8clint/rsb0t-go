// Everything the page knows about the fleet, and the one stream that keeps it current.
//
// The server holds only short rings; longer history lives here, capped so a tab left open for a
// week does not grow either. Panels read the store and are told why it changed: a set of reasons
// such as 'status', 'event', 'memory', 'select', 'clock', 'conn' and 'reset', delivered at most once
// a frame, so a burst of events costs one redraw and a hidden tab costs none.

const EVENT_CAP = 2000;
const MEMORY_CAP = 7200;

const listeners = new Set();
let pending = new Set();
let frame = 0;
let retryMs = 3000;

export const store = {
    meta: null,
    /** connecting | live | lost | unauthorized */
    conn: 'connecting',
    /** Browser clock minus server clock, in ms. */
    skew: 0,
    accounts: [],
    statusReceivedAt: 0,
    byName: new Map(),
    counts: {},
    logins: null,
    truncated: 0,
    events: [],
    lastSeq: 0,
    dropped: 0,
    memory: [],
    /** The account last clicked in a table, which detail panels follow. */
    selected: null,
    on(fn) {
        listeners.add(fn);
        return () => listeners.delete(fn);
    },
    select(name) {
        store.selected = name;
        emit('select');
    }
};

export function emit(why) {
    pending.add(why);
    if (frame === 0) frame = requestAnimationFrame(flush);
}

function flush() {
    frame = 0;
    const why = pending;
    pending = new Set();
    for (const fn of listeners) fn(why);
}

async function getJSON(path) {
    const res = await fetch(path, { cache: 'no-store' });
    if (res.status === 401) {
        store.conn = 'unauthorized';
        emit('conn');
        throw new Error('unauthorized');
    }
    if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
    return res.json();
}

function applyStatus(body) {
    store.statusReceivedAt = Date.now();
    store.accounts = Array.isArray(body.accounts) ? body.accounts : [];
    store.byName = new Map(store.accounts.map(a => [a.name, a]));
    store.counts = body.counts ?? {};
    store.logins = body.logins ?? null;
    store.truncated = body.truncated ?? 0;
    emit('status');
}

function pushEvent(ev) {
    if (typeof ev.seq !== 'number' || ev.seq <= store.lastSeq) return;
    store.lastSeq = ev.seq;
    store.events.push(ev);
    if (store.events.length > EVENT_CAP + 200) store.events.splice(0, store.events.length - EVENT_CAP);
    emit('event');
}

// The server's per-stream queue is bounded (see hub.go's own doc comment on stream.offer):
// a tab that falls behind a real burst of chat/log lines gets a 'dropped' frame telling it
// how many frames it missed, but — unlike a dropped TCP connection, which the browser's own
// EventSource reconnects and resumes via Last-Event-ID automatically — this happens on a
// connection that never actually closes, so nothing built into SSE fills the gap. Without
// this, those events are gone from this tab's store.events forever: the chat feed and event
// log panels would silently stop gaining anything from that gap onward, looking "stuck",
// until the human noticed and refreshed the page (a fresh load replays the current ring from
// scratch). Fetching the plain JSON endpoint the ring already backs closes that gap in place.
let resyncTimer = null;
let resyncInFlight = false;
let resyncPending = false;

function scheduleResync() {
    if (resyncTimer !== null) return;
    resyncTimer = setTimeout(runResync, 500);
}

async function runResync() {
    resyncTimer = null;
    if (resyncInFlight) { resyncPending = true; return; }
    resyncInFlight = true;
    try {
        const body = await getJSON(`api/events?since=${store.lastSeq}`);
        for (const ev of body.events ?? []) pushEvent(ev);
    } catch {
        // Offline or unauthorized; the next 'dropped' frame (or reconnect) tries again.
    } finally {
        resyncInFlight = false;
        if (resyncPending) { resyncPending = false; scheduleResync(); }
    }
}

function pushMemory(sample) {
    const last = store.memory[store.memory.length - 1];
    if (typeof sample.t !== 'number' || (last && sample.t <= last.t)) return;
    store.memory.push(sample);
    if (store.memory.length > MEMORY_CAP + 300) store.memory.splice(0, store.memory.length - MEMORY_CAP);
    emit('memory');
}

export async function connect() {
    try {
        store.meta = await getJSON('api/meta');
        store.skew = Date.now() - store.meta.serverTime;
        emit('meta');
    } catch {
        if (store.conn === 'unauthorized') return;
    }
    try {
        const mem = await getJSON('api/memory');
        for (const s of mem.samples ?? []) pushMemory(s);
        if (mem.now) pushMemory(mem.now);
    } catch {
        // The stream brings samples as well; the chart just starts emptier.
    }
    open();
    setInterval(() => { if (!document.hidden) emit('clock'); }, 1000);
    // A backgrounded tab can go a long time between redraws (rAF is throttled while
    // hidden), and — separately from that — a long-frozen tab's stream may have gone
    // quiet without the visible 'lost' state ever firing. Either way, a plain catch-up
    // fetch the moment the tab is looked at again is cheap and makes "switch back to the
    // dashboard" behave like a refresh without actually needing one.
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) { emit('clock'); scheduleResync(); }
    });
}

function open() {
    const source = new EventSource('api/stream');
    const on = (name, fn) =>
        source.addEventListener(name, ev => {
            let body;
            try {
                body = JSON.parse(ev.data);
            } catch {
                return;
            }
            fn(body);
        });
    on('hello', hello => {
        store.skew = Date.now() - hello.serverTime;
        // A lower newest seq than ours means the process restarted and numbering began again.
        if (hello.last < store.lastSeq) {
            store.lastSeq = 0;
            store.events = [];
            emit('reset');
        }
        retryMs = 3000;
        store.conn = 'live';
        emit('conn');
    });
    on('status', applyStatus);
    on('event', pushEvent);
    on('memory', pushMemory);
    on('dropped', d => {
        store.dropped += d.count;
        scheduleResync();
        emit('dropped');
    });
    source.onerror = () => {
        store.conn = 'lost';
        emit('conn');
        // EventSource retries a dropped connection itself, but gives up for good on a refusal
        // (401, or 503 when too many tabs are open), so those are retried here with a backoff.
        if (source.readyState === EventSource.CLOSED) setTimeout(probe, retryMs);
    };
}

async function probe() {
    retryMs = Math.min(retryMs * 2, 30000);
    try {
        const res = await fetch('api/meta', { cache: 'no-store' });
        if (res.status === 401) {
            store.conn = 'unauthorized';
            emit('conn');
            return;
        }
    } catch {
        // Still down; the stream will keep trying.
    }
    open();
}
