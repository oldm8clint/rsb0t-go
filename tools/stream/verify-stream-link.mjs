// Drives tools/stream/stream-link.js against a scripted fake of the panel's bridge on real (short)
// timers: the loop's own delays are its constants, so the checks that wait on a backoff are slow by
// design (a few seconds in all). Run: node tools/stream/verify-stream-link.mjs
import assert from 'node:assert/strict';
import { createStreamLink, RETRY_MS } from './stream-link.js';

const wait = ms => new Promise(r => setTimeout(r, ms));
const until = async (fn, ms = 6000) => { const end = Date.now() + ms; while (!fn()) { if (Date.now() > end) throw Error('timed out waiting: ' + fn); await wait(5); } };

// A fake bridge: opens hand out sessions; frames follow `handler.frame(session, have, requestNo)` so
// each check chooses exactly what the server says.
function bridge(handler) {
    const log = { opens: [], closes: [], frames: [], states: [] };
    let n = 0;
    return {
        log,
        post: async (path, body) => {
            if (path === '/api/stream/open') {
                log.opens.push(body);
                const r = handler.open?.(body, log.opens.length) ?? { session: 's' + (++n), fps: 5, maxFps: 5, transport: 'scene-delta' };
                if (r.status) throw Object.assign(Error(r.error), { status: r.status });
                return r;
            }
            if (path === '/api/stream/close') { log.closes.push(body.session); return { ok: true }; }
            throw Error('unexpected ' + path);
        },
        get: async url => {
            const m = /session=([^&]+)&have=(\d+)/.exec(url);
            log.frames.push({ session: m[1], have: +m[2] });
            const r = handler.frame(m[1], +m[2], log.frames.length);
            if (r === 'throw') throw Error('network down');
            if (r === 'stall') throw Object.assign(Error('aborted'), { stalled: true });
            const status = r.status ?? 200;
            return { ok: status < 400, status, text: JSON.stringify(r.body ?? r) };
        },
    };
}
const make = (b, extra = {}) => {
    const frames = []; let showing = true;
    const link = createStreamLink({ account: 'a', transport: 'scene-delta', owner: 'tab-1', post: b.post, get: b.get, showing: () => showing,
        maxFps: () => 200, have: () => frames.length, onFrame: body => frames.push(body), onState: s => b.log.states.push(s.phase), ...extra });
    return { link, frames, setShowing: v => { showing = v; link.poke(); } };
};
const frame = (seq, extra = {}) => ({ body: { seq, epoch: 1, scene: { tick: seq }, ...extra } });

// 1. A normal run applies frames in order, sends the owner token, and closes the lease on stop.
{
    const b = bridge({ frame: (s, have, i) => frame(i) });
    const { link, frames } = make(b);
    await link.start();
    await until(() => frames.length >= 3);
    await link.stop();
    assert.equal(b.log.opens[0].owner, 'tab-1');
    assert.deepEqual(frames.slice(0, 3).map(f => f.seq), [1, 2, 3]);
    assert.deepEqual(b.log.closes, ['s1']);
    assert.equal(link.phase, 'ended');
}

// 2. 410 (the lease is gone): the link reopens at once and asks for a whole scene (have=0).
{
    const b = bridge({ frame: (s, have, i) => (s === 's1' && i >= 3) ? { status: 410, error: 'lease lost' } : frame(s === 's1' ? i : 1000 + i) });
    const { link, frames } = make(b);
    await link.start();
    await until(() => b.log.opens.length === 2 && frames.length >= 5);
    assert.ok(b.log.frames.some(f => f.session === 's2' && f.have === 0), 'the first request on the new lease must ask for a whole scene');
    assert.ok(b.log.states.includes('reconnecting') && b.log.states.at(-1) === 'live');
    await link.stop();
}

// 3. 409 (the account is between sessions): backed off, never fatal; resumes when it is back.
{
    let back = false;
    const b = bridge({
        open: (body, n) => (n === 1 || back) ? undefined : { status: 409, error: 'account is reconnecting, not in game' },
        frame: (s, have, i) => (s === 's1' ? (i === 2 ? { status: 409, error: 'session changed' } : frame(i)) : frame(2000 + i)),
    });
    const { link, frames } = make(b);
    await link.start();
    await until(() => b.log.states.includes('reconnecting'));
    const t0 = Date.now();
    await wait(300);
    back = true;
    await until(() => b.log.opens.length >= 3 || (b.log.opens.length === 2 && frames.length >= 3), 8000);
    assert.ok(Date.now() - t0 >= 300 && link.stats.leaseLost >= 1);
    await until(() => link.phase === 'live' && frames.length >= 3, 8000);
    await link.stop();
}

// 4. Network failures keep the same lease for two misses and reopen on the third in a row.
{
    let fail = 0;
    const b = bridge({ frame: (s, have, i) => (i >= 2 && fail < 3 ? (fail++, 'throw') : frame(s === 's1' ? i : 3000 + i)) });
    const { link } = make(b);
    await link.start();
    await until(() => fail === 3 && b.log.opens.length === 2, 12000);
    assert.equal(link.stats.requestErrors, 3);
    await link.stop();
}

// 4b. A request the page itself stalled past its deadline (a long map decode blocked the main thread)
//     is not a bridge failure: it is retried at once, counted apart, and never reopens the lease.
{
    let stalls = 0;
    const b = bridge({ frame: (s, have, i) => (i % 2 === 0 && stalls < 5 ? (stalls++, 'stall') : frame(i)) });
    const { link, frames } = make(b);
    await link.start();
    await until(() => frames.length >= 4 && (link.stats.stalls || 0) >= 1);
    assert.equal(link.stats.requestErrors, 0);
    assert.equal(b.log.opens.length, 1, 'a stall must not reopen the lease');
    await link.stop();
}

// 5. A fatal answer (no bridge) ends the link and says why.
{
    const b = bridge({ frame: () => ({ status: 501, error: 'this fleet cannot observe an existing session' }) });
    const { link } = make(b);
    await link.start();
    await until(() => link.phase === 'ended');
    await link.stop();
}

// 5b. A stale sequence (another connection's numbering) reopens instead of applying the frame.
{
    const b = bridge({ frame: (s, have, i) => frame(s === 's1' ? 1 : i + 10) });
    const { link, frames } = make(b);
    await link.start();
    await until(() => b.log.opens.length >= 2 && frames.length >= 2);
    assert.equal(frames[0].seq, 1);
    await link.stop();
}

// 6. start() rejects with the bridge's message and status when the FIRST open is refused.
{
    const b = bridge({ open: () => ({ status: 409, error: 'another viewer holds the stream client lease' }), frame: () => frame(1) });
    const { link } = make(b);
    await assert.rejects(link.start(), e => e.kind === 'account' && e.status === 409 && /another viewer/.test(e.message));
}

// 7. A delta the view cannot apply makes the next request ask for a whole scene.
{
    let applied = 0;
    const b = bridge({ frame: (s, have, i) => frame(i, { delta: { base: i - 1 } }) });
    const { link } = make(b, { onFrame: () => { if (++applied === 2) throw Error('delta on the wrong base'); } });
    await link.start();
    await until(() => b.log.frames.length >= 4);
    assert.equal(b.log.frames[2].have, 0, 'after a failed delta the next request must be have=0');
    await link.stop();
}

// 8. Hidden views release the lease, stay quiet, and reopen when shown again.
{
    const b = bridge({ frame: (s, have, i) => frame(s === 's1' ? i : 5000 + i) });
    let clock = 0;
    const { link, setShowing } = make(b, { now: () => clock });
    await link.start();
    await until(() => b.log.frames.length >= 2);
    setShowing(false); await wait(60); clock += 60000; link.poke();
    await until(() => link.phase === 'paused');
    assert.deepEqual(b.log.closes, ['s1']);
    const frozen = b.log.frames.length;
    await wait(150);
    assert.equal(b.log.frames.length, frozen, 'a paused view must not observe');
    setShowing(true);
    await until(() => link.phase === 'live' && b.log.opens.length === 2 && b.log.frames.length > frozen, 4000);
    await link.stop();
}

// 9. Holding input control keeps the lease while hidden.
{
    const b = bridge({ frame: (s, have, i) => frame(i) });
    let clock = 0;
    const { link, setShowing } = make(b, { now: () => clock, controlHeld: () => true });
    await link.start();
    setShowing(false); await wait(60); clock += 60000; link.poke();
    await wait(1200);
    assert.equal(link.phase, 'live');
    assert.equal(b.log.closes.length, 0);
    await link.stop();
}

// 10. releaseNow sends one beacon and stops without awaiting anything.
{
    const b = bridge({ frame: (s, have, i) => frame(i) });
    const beacons = [];
    const { link } = make(b, { beacon: (p, body) => beacons.push([p, body.session]) });
    await link.start();
    link.releaseNow();
    assert.deepEqual(beacons, [['/api/stream/close', 's1']]);
    await link.stop();
}

console.log('stream-link: all checks passed (RETRY_MS ' + RETRY_MS.join('/') + ')');
