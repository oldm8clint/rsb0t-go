// node tools/stream/measure-transport.cjs PANEL_URL ACCOUNT [SECONDS]
//
// Measures the stream bridge's two frame transports on ONE in-game account of a panel you
// own: bytes per frame and observations per second for "full-snapshot" (a whole
// gamestate.RenderSnapshot per frame) and for "scene-delta" (a compact scene, then only what
// changed). Each run takes the single stream VIEW lease (POST /api/stream/open) and releases
// it (POST /api/stream/close) — so only point it at a panel whose operator agreed to that;
// never at a live fleet panel without the owner's go-ahead. It sends nothing to the game and
// prints counters only (no chat, names or inventory).
const [base, account, secondsArg] = process.argv.slice(2);
if (!base || !account) { console.error('usage: measure-transport.cjs PANEL_URL ACCOUNT [SECONDS]'); process.exit(2); }
const seconds = Number(secondsArg || 20);

async function post(path, body) {
    const r = await fetch(new URL(path, base), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const v = await r.json();
    if (!r.ok) throw Error(`${path}: ${v.error || r.status}`);
    return v;
}

async function run(transport) {
    const lease = await post('/api/stream/open', { account, fps: 60, transport });
    const out = { transport: lease.transport, frames: 0, whole: 0, wholeBytes: 0, deltas: 0, deltaBytes: 0, emptyDeltas: 0, throttled: 0, maxBytes: 0 };
    const until = Date.now() + seconds * 1000, started = Date.now();
    let have = 0;
    try {
        while (Date.now() < until) {
            const r = await fetch(new URL(`/api/stream/frame?session=${encodeURIComponent(lease.session)}&have=${have}`, base), { cache: 'no-store' });
            const text = await r.text(), body = JSON.parse(text);
            if (!r.ok) throw Error(`frame: ${body.error || r.status}`);
            if (body.throttled) { out.throttled++; await new Promise(res => setTimeout(res, Math.max(20, body.retryInMs || 100))); continue; }
            out.frames++; out.maxBytes = Math.max(out.maxBytes, text.length);
            if (body.delta) {
                out.deltas++; out.deltaBytes += text.length;
                const d = body.delta;
                if (!d.self && !d.upsert && !d.remove && !d.inv && !d.chat && !d.status) out.emptyDeltas++;
            } else { out.whole++; out.wholeBytes += text.length; }
            if (body.seq) have = body.seq;
        }
    } finally { await post('/api/stream/close', { session: lease.session }).catch(() => {}); }
    const elapsed = (Date.now() - started) / 1000;
    return { ...out, seconds: Math.round(elapsed * 10) / 10, observationsPerSecond: Math.round(out.frames / elapsed * 10) / 10,
        avgWholeBytes: out.whole ? Math.round(out.wholeBytes / out.whole) : 0, avgDeltaBytes: out.deltas ? Math.round(out.deltaBytes / out.deltas) : 0,
        bytesPerSecond: Math.round((out.wholeBytes + out.deltaBytes) / elapsed) };
}

(async () => {
    const full = await run('full-snapshot');
    const delta = await run('scene-delta');
    console.log(JSON.stringify({ account: '[measured]', fullSnapshot: full, sceneDelta: delta }, null, 1));
})().catch(e => { console.error(e.message || e); process.exitCode = 1; });
