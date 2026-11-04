import { anyOf, el, fmtBytes, fmtCount, fmtDuration, kv, serverNow } from '../util.js';

export function resourceWindow(samples, seconds = 300) {
    const last = samples.at(-1);
    if (!last) return null;
    const start = last.t - seconds * 1000;
    let from = samples.length - 1;
    while (from > 0 && samples[from - 1].t >= start) from--;
    const first = samples[from], elapsed = last.t - first.t;
    let sum = 0, count = 0, peak = 0;
    for (let i = from; i < samples.length; i++) {
        const cpu = samples[i].cpuPercent;
        if (Number.isFinite(cpu) && cpu > 0) { sum += cpu; count++; peak = Math.max(peak, cpu); }
    }
    const resident = first.rssBytes > 0 && last.rssBytes > 0;
    return { last, from, elapsed, average: count ? sum / count : null, peak: count ? peak : null,
        growth: resident && elapsed > 0 ? (last.rssBytes - first.rssBytes) * 60000 / elapsed : null,
        gc: elapsed > 0 && last.numGC >= first.numGC ? (last.numGC - first.numGC) * 60000 / elapsed : null };
}

export default {
    type: 'resources', title: 'Resource monitor',
    description: 'Process CPU trend, resident memory growth and garbage-collection pressure from existing samples.',
    size: { w: 4, h: 3 }, accounts: 'none', settings: [],
    create(body, ctx) {
        const dl = el('dl', 'kv'), values = new Map();
        for (const label of ['CPU now', 'CPU average / peak', 'Resident memory', 'Resident change / min', 'Heap in use', 'Goroutines', 'GC cycles / min', 'Last GC pause']) values.set(label, kv(dl, label, '—'));
        const canvas = el('canvas', 'cpu-trend');
        canvas.setAttribute('aria-label', 'Process CPU over the last five minutes, 0 to 100 percent of machine capacity');
        canvas.setAttribute('role', 'img');
        const age = el('p', 'none'), note = el('p', 'none', 'Five-minute observed window. CPU uses total machine capacity; zero CPU/RSS can mean unavailable. Memory slope is not a leak diagnosis.');
        body.append(canvas, dl, age, note);
        let latest = null;
        const percent = n => n === null || !(n > 0) ? 'Unavailable' : `${n.toFixed(1)}%`;
        function draw() {
            if (!latest || !ctx.isVisible()) return;
            const summary = resourceWindow(latest.memory);
            if (!summary) return;
            const { last, from } = summary;
            values.get('CPU now').textContent = percent(last.cpuPercent);
            values.get('CPU average / peak').textContent = `${percent(summary.average)} / ${percent(summary.peak)}`;
            values.get('Resident memory').textContent = last.rssBytes > 0 ? fmtBytes(last.rssBytes) : 'Unavailable';
            values.get('Resident change / min').textContent = summary.growth === null ? 'Insufficient samples' : `${summary.growth < 0 ? '−' : '+'}${fmtBytes(Math.abs(summary.growth))}`;
            values.get('Heap in use').textContent = fmtBytes(last.heapInuse);
            values.get('Goroutines').textContent = fmtCount(last.goroutines);
            values.get('GC cycles / min').textContent = summary.gc === null ? 'Insufficient samples' : summary.gc.toFixed(1);
            values.get('Last GC pause').textContent = `${((last.lastPauseNs ?? 0) / 1e6).toFixed(2)} ms`;
            const width = Math.max(1, Math.round(canvas.clientWidth)), height = 64;
            const dpr = Math.min(2, window.devicePixelRatio || 1);
            if (canvas.width !== width*dpr || canvas.height !== height*dpr) { canvas.width = width*dpr; canvas.height = height*dpr; }
            const g = canvas.getContext('2d'); g.setTransform(dpr,0,0,dpr,0,0); g.clearRect(0,0,width,height);
            g.strokeStyle = '#6fd5c5'; g.lineWidth = 1.5; g.beginPath();
            let connected = false, previousX = -1;
            for (let i = from; i < latest.memory.length; i++) {
                const m = latest.memory[i], x = Math.round((m.t - (last.t - 300000)) / 300000 * (width-1));
                if (!(m.cpuPercent > 0)) { connected = false; continue; }
                // Bound chart work to one point per CSS pixel.
                if (x === previousX) continue;
                previousX = x;
                const y = height - 3 - Math.min(100,m.cpuPercent) / 100 * (height-6);
                if (connected) g.lineTo(x,y); else g.moveTo(x,y);
                connected = true;
            }
            g.stroke();
            ctx.setSub(`${fmtDuration(summary.elapsed)} observed`);
        }
        const resize = new ResizeObserver(draw); resize.observe(canvas);
        return { update(store, why) {
            latest = store;
            if (anyOf(why, 'init', 'memory', 'reset')) draw();
            if (anyOf(why, 'init', 'memory', 'clock', 'conn')) {
                const last = store.memory.at(-1), ms = last ? Math.max(0,serverNow(store) - last.t) : null;
                age.textContent = ms === null ? 'Waiting for resource samples.' : `Sample age ${fmtDuration(ms)}${store.conn !== 'live' || ms >= 15000 ? ' · stale or disconnected' : ''}`;
            }
        }, destroy() { resize.disconnect(); } };
    }
};
