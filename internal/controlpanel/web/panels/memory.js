import { anyOf, el, fmtBytes, fmtClock } from '../util.js';

// Each series keeps its colour whichever others are shown, so a line never changes colour when a
// neighbour is switched off. One axis only: every series is bytes. Goroutines/CPU% go in the footer.
//
// rssBytes is the odd one out here — everything else is the Go RUNTIME's own accounting
// (runtime.MemStats), which can drift well away from what the OS actually has resident.
// "Total from the OS" (sys) in particular is virtual memory the Go allocator has RESERVED,
// not necessarily touched — live-found 2026-09-16: the account owner saw this exact line
// read close to 1GB during last night's 179-account login storm while Task Manager's own
// current-memory figure for the same process was ~170MB, and reasonably read the panel's
// own label as a real, comparable "how much RAM is this using" number. rssBytes
// (MemStats.RSSBytes, added the same session — see procstats_windows.go) is that real,
// OS-reported, currently-resident figure instead, the one actually comparable to Task
// Manager. Shown by default alongside heapInuse so the gap between "Go thinks it reserved
// this much" and "the OS says this much is actually resident" is visible at a glance rather
// than requiring a settings click to notice.
const SERIES = [
    { value: 'heapInuse', label: 'Heap in use', color: '--s1' },
    { value: 'rssBytes', label: 'Real memory (OS-reported)', color: '--s6' },
    { value: 'sys', label: 'Reserved from the OS (Go runtime)', color: '--s2' },
    { value: 'heapAlloc', label: 'Heap allocated', color: '--s3' },
    { value: 'stackInuse', label: 'Stacks', color: '--s4' },
    { value: 'heapSys', label: 'Heap reserved', color: '--s5' }
];

const WINDOWS = [
    { value: '60', label: '1 minute' },
    { value: '300', label: '5 minutes' },
    { value: '900', label: '15 minutes' },
    { value: '3600', label: '1 hour' },
    { value: '7200', label: '2 hours' }
];

const MB = 1024 * 1024;

function niceStep(raw) {
    const exp = Math.pow(10, Math.floor(Math.log10(raw)));
    const f = raw / exp;
    return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * exp;
}

/** The first index whose sample is at or after t. */
function lowerBound(samples, t) {
    let lo = 0;
    let hi = samples.length;
    while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if (samples[mid].t < t) lo = mid + 1;
        else hi = mid;
    }
    return lo;
}

export default {
    type: 'memory',
    title: 'Memory',
    description: 'Process memory over time, charted from samples this browser keeps.',
    size: { w: 5, h: 2 },
    accounts: 'none',
    settings: [
        { key: 'series', label: 'Lines', kind: 'multi', options: SERIES, default: ['heapInuse', 'rssBytes'] },
        { key: 'window', label: 'Time shown', kind: 'select', options: WINDOWS, default: '300' },
        { key: 'target', label: 'Target line in MB, the fleet goal (0 for none)', kind: 'number', min: 0, max: 4096, step: 1, default: 50 },
        { key: 'gc', label: 'Mark each GC cycle', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const chart = el('div', 'chart');
        const legend = el('div', 'legend');
        const plot = el('div', 'plot');
        const canvas = el('canvas');
        const tip = el('div', 'tip');
        const foot = el('div', 'foot');
        tip.hidden = true;
        plot.append(canvas, tip);
        chart.append(legend, plot, foot);
        body.append(chart);

        const series = SERIES.filter(s => ctx.settings.series.includes(s.value));
        const span = Number(ctx.settings.window) * 1000;
        const spanLabel = WINDOWS.find(o => o.value === ctx.settings.window)?.label ?? '';
        // A goal to read the lines against, not a cap: nothing happens when the fleet crosses it.
        const budget = ctx.settings.target * MB;
        const values = new Map();
        for (const s of series) {
            const item = el('span');
            const swatch = el('i');
            swatch.style.background = `var(${s.color})`;
            const value = el('b', '', '-');
            item.append(swatch, `${s.label} `, value);
            legend.append(item);
            values.set(s.value, value);
        }
        canvas.setAttribute('role', 'img');

        let latest = null;
        let hoverX = null;
        let paint = 0;

        function scheduleDraw() {
            if (paint || !ctx.isVisible()) return;
            paint = requestAnimationFrame(() => { paint = 0; draw(); });
        }

        function palette() {
            const cs = getComputedStyle(body);
            const read = name => cs.getPropertyValue(name).trim();
            return { grid: read('--grid'), dim: read('--dim'), plot: read('--plot'), series: Object.fromEntries(SERIES.map(s => [s.value, read(s.color)])) };
        }

        function draw() {
            const store = latest;
            if (store === null || !ctx.isVisible()) return;
            const samples = store.memory;
            const newest = samples.length ? samples[samples.length - 1] : null;
            for (const s of series) values.get(s.value).textContent = newest ? fmtBytes(newest[s.value]) : '-';
            const cpuText = newest && newest.cpuPercent > 0 ? `, ${newest.cpuPercent.toFixed(1)}% CPU` : '';
            foot.textContent = newest ? `${newest.goroutines} goroutines, ${newest.numGC} GC cycles, last pause ${(newest.lastPauseNs / 1e6).toFixed(2)} ms${cpuText}` : 'waiting for the first sample';
            canvas.setAttribute('aria-label', `Memory over the last ${spanLabel}. ${legend.textContent}`);

            const w = plot.clientWidth;
            const h = plot.clientHeight;
            if (w < 20 || h < 20) return;
            const dpr = Math.min(2, window.devicePixelRatio || 1);
            if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
                canvas.width = Math.round(w * dpr);
                canvas.height = Math.round(h * dpr);
            }
            const g = canvas.getContext('2d');
            g.setTransform(dpr, 0, 0, dpr, 0, 0);
            const c = palette();
            g.fillStyle = c.plot;
            g.fillRect(0, 0, w, h);
            g.font = '10px Helvetica, Arial, sans-serif';

            const end = newest ? newest.t : Date.now() - store.skew;
            const start = end - span;
            const from = lowerBound(samples, start);
            if (from >= samples.length) {
                g.fillStyle = c.dim;
                g.textAlign = 'center';
                g.textBaseline = 'middle';
                g.fillText('waiting for samples', w / 2, h / 2);
                return;
            }

            let max = budget;
            for (let i = from; i < samples.length; i++) for (const s of series) max = Math.max(max, samples[i][s.value] || 0);
            if (max <= 0) max = MB;
            const step = niceStep(max / 4);
            const top = Math.ceil((max * 1.05) / step) * step;
            const padL = Math.ceil(g.measureText(fmtBytes(top)).width) + 8;
            const padR = 8;
            const padT = 8;
            const padB = 16;
            const iw = w - padL - padR;
            const ih = h - padT - padB;
            const X = t => padL + ((t - start) / span) * iw;
            const Y = v => padT + (1 - v / top) * ih;

            // Recessive gridlines, labelled at the left.
            g.lineWidth = 1;
            g.strokeStyle = c.grid;
            g.fillStyle = c.dim;
            g.textAlign = 'right';
            g.textBaseline = 'middle';
            for (let v = 0; v <= top + 1; v += step) {
                const y = Math.round(Y(v)) + 0.5;
                g.beginPath();
                g.moveTo(padL, y);
                g.lineTo(w - padR, y);
                g.stroke();
                g.fillText(fmtBytes(v), padL - 4, y);
            }
            g.textBaseline = 'alphabetic';
            g.textAlign = 'left';
            g.fillText(`${spanLabel} ago`, padL, h - 3);
            g.textAlign = 'right';
            g.fillText(fmtClock(end), w - padR, h - 3);

            if (budget > 0) {
                const y = Math.round(Y(budget)) + 0.5;
                g.save();
                g.setLineDash([4, 4]);
                g.strokeStyle = c.dim;
                g.beginPath();
                g.moveTo(padL, y);
                g.lineTo(w - padR, y);
                g.stroke();
                g.restore();
                g.textAlign = 'left';
                g.textBaseline = 'bottom';
                g.fillText(`target ${fmtBytes(budget)}`, padL + 4, y - 2);
            }

            if (ctx.settings.gc) {
                g.fillStyle = c.dim;
                for (let i = Math.max(from, 1); i < samples.length; i++) {
                    if (samples[i].numGC > samples[i - 1].numGC) g.fillRect(Math.round(X(samples[i].t)), padT + ih - 5, 1, 5);
                }
            }

            // A gap in the samples (the tab or the stream was away) breaks the line rather than
            // drawing a straight run across time nobody measured.
            const gap = Math.max(3000, (store.meta?.tickMs ?? 1000) * 3);
            g.lineWidth = 2;
            g.lineJoin = 'round';
            g.lineCap = 'round';
            for (const s of series) {
                g.strokeStyle = c.series[s.value];
                g.beginPath();
                let prev = -Infinity;
                for (let i = from; i < samples.length; i++) {
                    const m = samples[i];
                    const x = X(m.t);
                    const y = Y(m[s.value] || 0);
                    if (m.t - prev > gap) g.moveTo(x, y);
                    else g.lineTo(x, y);
                    prev = m.t;
                }
                g.stroke();
            }

            if (hoverX === null || hoverX < padL) {
                tip.hidden = true;
                return;
            }
            let i = lowerBound(samples, start + ((hoverX - padL) / iw) * span);
            if (i >= samples.length) i = samples.length - 1;
            const target = start + ((hoverX - padL) / iw) * span;
            if (i > from && Math.abs(samples[i - 1].t - target) < Math.abs(samples[i].t - target)) i--;
            const m = samples[Math.max(i, from)];
            const x = Math.round(X(m.t)) + 0.5;
            g.strokeStyle = c.dim;
            g.lineWidth = 1;
            g.beginPath();
            g.moveTo(x, padT);
            g.lineTo(x, padT + ih);
            g.stroke();
            for (const s of series) {
                g.beginPath();
                g.arc(x, Y(m[s.value] || 0), 4, 0, Math.PI * 2);
                g.fillStyle = c.series[s.value];
                g.fill();
                g.lineWidth = 2;
                g.strokeStyle = c.plot;
                g.stroke();
            }
            tip.textContent = '';
            tip.append(el('div', 't', fmtClock(m.t)));
            for (const s of series) {
                const row = el('div');
                const swatch = el('i');
                swatch.style.background = `var(${s.color})`;
                row.append(swatch, `${s.label} `, el('b', '', fmtBytes(m[s.value])));
                tip.append(row);
            }
            tip.append(el('div', '', `${m.goroutines} goroutines, GC ${m.numGC}`));
            tip.hidden = false;
            const tw = tip.offsetWidth;
            tip.style.left = `${x + 12 + tw > w ? Math.max(0, x - 12 - tw) : x + 12}px`;
            tip.style.top = `${padT}px`;
        }

        const resize = new ResizeObserver(scheduleDraw);
        resize.observe(plot);
        const offVisibility = ctx.onVisibility(visible => { if (visible) scheduleDraw(); });
        canvas.addEventListener('pointermove', ev => {
            hoverX = ev.clientX - canvas.getBoundingClientRect().left;
            scheduleDraw();
        });
        canvas.addEventListener('pointerleave', () => {
            hoverX = null;
            scheduleDraw();
        });

        return {
            update(store, why) {
                latest = store;
                if (anyOf(why, 'init', 'memory')) scheduleDraw();
            },
            destroy() {
                resize.disconnect();
                offVisibility();
                cancelAnimationFrame(paint);
            }
        };
    }
};
