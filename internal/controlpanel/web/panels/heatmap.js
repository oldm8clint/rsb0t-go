import { anyOf, el, button, fmtDuration, fmtCount, fmtClock } from '../util.js';
import { loadLeaflet, loadTileMeta, rasterBounds } from './map.js';
import { createHeatRenderer, rampGradient } from '../heat-render.js';
import { mapOverlayBar, fillPanel } from '../map-overlay.js';

// Travel heat map: where our accounts have spent their time, drawn as a heat layer over the
// same Leaflet terrain the GPU account map uses. The server samples every in-game account's
// tile once a second and keeps sparse counts per account, floor and time bucket (see
// internal/controlpanel/heatmap.go); this panel asks /api/heatmap for the part of the world in
// view, on a grid coarse enough for the current zoom, and redraws as new samples arrive.
//
// Colour: one hue (orange), dark and transparent for little time to light and opaque for a
// lot, on a log scale auto-fitted to the busiest cell in view — the legend says so. The GPU
// path sums point-sprite kernels into a float texture and colours it in a second pass; the
// canvas path does the same with 2D stamps (heat-render.js).
//
// The toolbar's choices and where the map was looking are a per-viewer convenience kept in
// this browser (localStorage); the panel's settings (gear) choose what a fresh view starts
// on.

const POLL_MS = 5000;
const MAX_TILE = 16383;

const WINDOWS = [
    { value: 'hour', label: 'Last hour' },
    { value: 'day', label: 'Last 24 hours' },
    { value: 'all', label: 'All time' }
];
const LEVELS = [
    { value: 'all', label: 'All floors' },
    { value: '0', label: 'Floor 0' },
    { value: '1', label: 'Floor 1' },
    { value: '2', label: 'Floor 2' },
    { value: '3', label: 'Floor 3' }
];

function readView(id) {
    try { return JSON.parse(localStorage.getItem(`heatmap-view:${id}`) || 'null'); } catch { return null; }
}
function writeView(id, view) {
    try { localStorage.setItem(`heatmap-view:${id}`, JSON.stringify(view)); } catch { /* storage off: the view still works */ }
}

function selectOf(options, value, title) {
    const s = el('select');
    if (title) { s.title = title; s.setAttribute('aria-label', title); }
    for (const o of options) {
        const opt = el('option', '', o.label);
        opt.value = o.value;
        s.append(opt);
    }
    s.value = value;
    return s;
}

/** Seconds of presence for a count of samples. */
function presence(count, sampleMs) {
    return fmtDuration(count * (sampleMs || 1000));
}

// How to describe a bucket of samples, which is NOT the same thing for one account as for the
// whole fleet. Every in-game account contributes one sample per tick, so with 243 accounts one
// real second of wall time produces 243 samples. For a single account the sample count IS its
// own elapsed presence; summed over the fleet it is COMBINED ACCOUNT TIME, and printing that as
// a bare duration read as "242d 17h in view" on a heat map barely a day old (owner, 2026-09-20).
// The data was correct and the label was not: 21,007,838 samples over 243 accounts is about 24
// hours of wall time, which is exactly how long it had been recording.
function presenceOf(count, sampleMs, accountCount) {
    const total = presence(count, sampleMs);
    if (!(accountCount > 1)) return total;
    return `${total} of account time (~${presence(count / accountCount, sampleMs)} each across ${accountCount})`;
}

export default {
    type: 'heatmap',
    title: 'Travel heat map',
    description: 'Where the accounts have spent their time, built up live from a once-a-second position sample and kept across restarts — per account or the whole fleet, per floor, for the last hour, day or all time.',
    size: { w: 6, h: 4 },
    accounts: 'none',
    settings: [
        { key: 'renderer', label: 'Draw the heat with', kind: 'select', options: [{ value: 'gpu', label: 'GPU / WebGL' }, { value: 'canvas', label: 'Canvas fallback' }], default: 'gpu' },
        { key: 'account', label: 'Account to start on', kind: 'select', options: [{ value: 'all', label: 'All accounts' }, { value: 'selected', label: 'Follow the selected account' }], default: 'all' },
        { key: 'window', label: 'Time window to start on', kind: 'select', options: WINDOWS, default: 'all' },
        { key: 'level', label: 'Floor to start on', kind: 'select', options: LEVELS, default: 'all' },
        { key: 'intensity', label: 'Intensity to start on', kind: 'number', min: 0.25, max: 4, step: 0.25, default: 1 }
    ],
    create(body, ctx) {
        const { store } = ctx;
        const saved = readView(ctx.id) || {};
        const view = {
            account: typeof saved.account === 'string' ? saved.account : ctx.settings.account,
            window: WINDOWS.some(w => w.value === saved.window) ? saved.window : ctx.settings.window,
            level: LEVELS.some(l => l.value === saved.level) ? saved.level : ctx.settings.level,
            intensity: Number.isFinite(saved.intensity) ? Math.min(4, Math.max(0.25, saved.intensity)) : ctx.settings.intensity,
            // Where this viewer was looking, as a world tile and a Leaflet zoom; absent until
            // the first move, and a fresh view fits everything recorded instead.
            center: Array.isArray(saved.center) && saved.center.length === 2 && saved.center.every(Number.isFinite) ? saved.center : null,
            zoom: Number.isFinite(saved.zoom) ? saved.zoom : null
        };

        const accountSel = selectOf([{ value: 'all', label: 'All accounts' }, { value: 'selected', label: 'Selected account' }], 'all', 'Whose travel to show');
        const levelSel = selectOf(LEVELS, view.level, 'Which building floor to show');
        const windowSel = selectOf(WINDOWS, view.window, 'How far back to count');
        const intensityIn = el('input');
        intensityIn.type = 'range'; intensityIn.min = '-2'; intensityIn.max = '2'; intensityIn.step = '0.25';
        intensityIn.value = String(Math.log2(view.intensity));
        intensityIn.title = 'Intensity'; intensityIn.setAttribute('aria-label', 'Intensity');
        const intensityLbl = el('span', 'heatnum');
        const fitBtn = button('Fit', 'Zoom to where the heat is: the busiest part of the world first, the next on another click');
        const clearBtn = button('Clear…', 'Delete recorded heat for all accounts or the chosen one');
        const intensityBox = el('span', 'heatintensity');
        intensityBox.append(el('span', 'heatlbl', 'Intensity'), intensityIn, intensityLbl);
        const toolbar = [accountSel, levelSel, windowSel, intensityBox, fitBtn, clearBtn];

        const confirmRow = el('div', 'heatconfirm');
        confirmRow.hidden = true;
        const confirmText = el('span');
        const confirmYes = button('Clear it', 'Delete the recorded heat now', 'heatdanger');
        const confirmNo = button('Cancel');
        const note = el('span', 'heatnote');
        confirmRow.append(confirmText, confirmYes, confirmNo);

        const wrap = el('div', 'mapwrap heatwrap');
        const leafletEl = el('div', 'mapleaflet');
        leafletEl.hidden = true;
        const plain = el('div', 'heatplain');
        plain.hidden = true;
        const tip = el('div', 'tip');
        tip.hidden = true;
        const legend = el('div', 'heatlegend');
        const legendBar = el('div', 'heatramp');
        legendBar.style.background = rampGradient();
        const legendText = el('div', 'heatlegendtext');
        legend.append(legendBar, legendText);
        const status = el('div', 'heatstatus');
        const foot = el('div', 'foot mapfoot');
        // Everything but the map floats over it (map-overlay.js): the panel is exactly its
        // map, fills its whole body and never scrolls.
        wrap.append(leafletEl, plain, legend, tip, status, foot, confirmRow, note);
        const overlay = mapOverlayBar(wrap, toolbar, 'Heat');
        fillPanel(body);
        body.append(wrap);

        let destroyed = false;
        let data = null; // the last /api/heatmap answer drawn
        let cellIndex = new Map();
        let seq = 0;
        let pollTimer = 0, moveTimer = 0;
        let mode = null; // 'leaflet' | 'plain'
        let leaflet = null; // { L, map, meta, layer, worldToLatLng }
        let plainView = null; // { renderer, canvas, x0, z0, scale }

        function persist() { writeView(ctx.id, view); }
        function sampleMs() { return data?.store?.sampleMs || 1000; }

        // How many accounts the numbers on screen are summed over: 0 when one account is
        // picked (its samples are its own elapsed time), otherwise however many the server
        // aggregated, so the label can say "of account time ... each across N".
        function fleetWide() { return accountName() ? 0 : (data?.accounts || []).length; }

        function accountName() {
            if (view.account === 'all') return '';
            if (view.account === 'selected') return store.selected || '';
            return view.account;
        }
        function accountLabel() {
            const name = accountName();
            if (view.account === 'selected' && !name) return 'the selected account (none selected)';
            return name ? `account ${name}` : 'every account';
        }

        function refreshAccountOptions() {
            const names = new Set([...(data?.accounts || []), ...store.accounts.map(a => a.name)]);
            const sig = [...names].sort().join('\n');
            if (accountSel.dataset.sig === sig) return;
            accountSel.dataset.sig = sig;
            accountSel.replaceChildren();
            for (const o of [{ value: 'all', label: 'All accounts' }, { value: 'selected', label: 'Selected account (follows the table)' }, ...[...names].sort().map(n => ({ value: n, label: n }))]) {
                const opt = el('option', '', o.label);
                opt.value = o.value;
                accountSel.append(opt);
            }
            if (![...accountSel.options].some(o => o.value === view.account)) {
                const opt = el('option', '', `${view.account} (nothing recorded)`);
                opt.value = view.account;
                accountSel.append(opt);
            }
            accountSel.value = view.account;
        }

        function setIntensityLabel() { intensityLbl.textContent = `${view.intensity.toFixed(view.intensity < 1 ? 2 : 1)}×`; }
        setIntensityLabel();

        // ---- projection helpers (Leaflet mode) ----
        function frame() {
            const { map, worldToLatLng, meta } = leaflet;
            const rx = meta.originTileX, rz = meta.originTileZ;
            // Not latLngToContainerPoint: that rounds to whole pixels, and zoomed out a tile
            // is well under one pixel.
            const cp = ll => map.layerPointToContainerPoint(map.project(ll).subtract(map.getPixelOrigin()));
            const o = cp(worldToLatLng(rx, rz));
            const ex = (cp(worldToLatLng(rx + 1024, rz)).x - o.x) / 1024;
            const ez = (cp(worldToLatLng(rx, rz + 1024)).y - o.y) / 1024;
            return { rx, rz, ox: o.x, oy: o.y, ex, ez };
        }
        function viewBox(f, size, pad) {
            const xs = [f.rx + (0 - f.ox) / f.ex, f.rx + (size.x - f.ox) / f.ex];
            const zs = [f.rz + (0 - f.oy) / f.ez, f.rz + (size.y - f.oy) / f.ez];
            const w = xs[1] - xs[0], h = Math.abs(zs[1] - zs[0]);
            if (![...xs, ...zs].every(Number.isFinite) || !(w > 0) || !(h > 0)) return {};
            const clamp = v => Math.max(0, Math.min(MAX_TILE, Math.round(v)));
            return {
                x0: clamp(Math.min(...xs) - w * pad), x1: clamp(Math.max(...xs) + w * pad),
                z0: clamp(Math.min(...zs) - h * pad), z1: clamp(Math.max(...zs) + h * pad)
            };
        }
        function cellFor(pxPerTile) {
            let cell = 1;
            while (cell < 1024 && cell * pxPerTile < 3) cell *= 2;
            return cell;
        }

        // ---- fetching ----
        function query(extra) {
            const p = new URLSearchParams({ window: view.window, level: view.level });
            const name = accountName();
            if (name) p.set('account', name);
            for (const [k, v] of Object.entries(extra)) p.set(k, String(v));
            return `api/heatmap?${p}`;
        }
        async function fetchJSON(url) {
            const res = await fetch(url, { cache: 'no-store' });
            const body = await res.json().catch(() => ({}));
            if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
            return body;
        }
        async function load() {
            if (destroyed || !mode || ctx.isVisible?.() === false) return;
            if (view.account === 'selected' && !store.selected) {
                data = null; cellIndex = new Map();
                redraw();
                status.textContent = 'Select an account in a table to follow it.';
                status.hidden = false;
                return;
            }
            const mine = ++seq;
            let url;
            if (mode === 'leaflet') {
                const f = frame();
                url = query({ cell: Number.isFinite(f.ex) && f.ex > 0 ? cellFor(f.ex) : 16, ...viewBox(f, leaflet.map.getSize(), 0.25) });
            } else {
                url = query({ cell: 8 });
            }
            const t0 = performance.now();
            try {
                const body = await fetchJSON(url);
                if (destroyed || mine !== seq) return;
                body.fetchMs = performance.now() - t0;
                data = body;
                cellIndex = new Map();
                for (let i = 0; i < body.cells.length; i += 3) cellIndex.set(`${body.cells[i]},${body.cells[i + 1]}`, body.cells[i + 2]);
                status.hidden = true;
                refreshAccountOptions();
                redraw();
            } catch (err) {
                if (destroyed || mine !== seq) return;
                status.textContent = `Could not load the heat map: ${err.message}`;
                status.hidden = false;
            }
        }
        function schedulePoll() {
            clearTimeout(pollTimer);
            if (destroyed || ctx.isVisible?.() === false) return;
            pollTimer = setTimeout(async () => {
                if (!document.hidden) await load();
                if (!destroyed) schedulePoll();
            }, POLL_MS);
        }
        // After a new account, floor or window: when none of its heat is in view but some is
        // recorded elsewhere, go there instead of leaving the viewer on an empty map.
        async function loadOrFit() {
            await load();
            if (data && !data.total && data.areas?.length) await fit(0);
        }
        function reloadSoon() {
            clearTimeout(moveTimer);
            moveTimer = setTimeout(load, 120);
        }

        // ---- drawing ----
        let drawCount = 0, drawMsTotal = 0;
        function pointsFor(f, size, radius) {
            const cells = data?.cells || [];
            const cell = data?.cell || 1;
            const logMax = Math.log1p(data?.max || 1);
            const out = new Float32Array(cells.length);
            let n = 0;
            const half = cell / 2 - 0.5;
            for (let i = 0; i < cells.length; i += 3) {
                const px = f.ox + (cells[i] + half - f.rx) * f.ex;
                const py = f.oy + (cells[i + 1] + half - f.rz) * f.ez;
                if (px < -radius || py < -radius || px > size.x + radius || py > size.y + radius) continue;
                // A floor so one visit still shows; log so a mule stand does not wash out a path.
                out[n++] = px; out[n++] = py; out[n++] = 0.2 + 0.8 * Math.log1p(cells[i + 2]) / logMax;
            }
            return out.subarray(0, n);
        }
        function redraw() {
            const t0 = performance.now();
            let renderer, points, radius;
            if (mode === 'leaflet' && leaflet?.layer?.renderer) {
                const f = frame();
                const size = leaflet.map.getSize();
                const cellPx = (data?.cell || 1) * f.ex;
                radius = Math.max(6, Math.min(48, cellPx * 1.5));
                points = pointsFor(f, size, radius);
                renderer = leaflet.layer.renderer;
            } else if (mode === 'plain' && plainView) {
                const size = { x: plain.clientWidth, y: plain.clientHeight };
                fitPlain(size);
                const pv = plainView;
                const f = { rx: pv.x0, rz: pv.z0, ox: pv.pad, oy: size.y - pv.pad, ex: pv.scale, ez: -pv.scale };
                radius = Math.max(5, Math.min(40, (data?.cell || 1) * pv.scale * 1.5));
                points = pointsFor(f, size, radius);
                renderer = pv.renderer;
            } else {
                return;
            }
            renderer.draw(points, radius, view.intensity);
            drawCount++; drawMsTotal += performance.now() - t0;
            describe(renderer, points.length / 3);
        }
        function describe(renderer, drawn) {
            const cell = data?.cell || 1;
            const ms = sampleMs();
            legendText.textContent = data && data.max
                ? `log scale · brightest = ${presenceOf(data.max, ms, fleetWide())} in one ${cell}×${cell}-tile cell in view`
                : data?.areas?.length
                    ? `nothing recorded in view · Fit goes to the ${data.areas[0].name}`
                    : 'nothing recorded here yet';
            const s = data?.store;
            const parts = [];
            parts.push(renderer.kind);
            if (s) {
                parts.push(`${accountLabel()}: ${presenceOf(data.total, ms, fleetWide())} in view`);
                parts.push(`${fmtCount(s.samples)} account-seconds since ${new Date(s.sinceUnixMs).toLocaleString()}`);
                parts.push(`${fmtCount(s.cells)}/${fmtCount(s.maxCells)} cells stored`);
                if (s.storeCell > 1) parts.push(`coarsened to ${s.storeCell}×${s.storeCell} tiles`);
                if (s.full) parts.push(`FULL: ${fmtCount(s.refused)} new cells refused`);
                parts.push(s.persisted ? (s.lastSaveUnixMs ? `saved ${fmtClock(s.lastSaveUnixMs)}` : 'not saved yet') : 'memory only');
                if (s.saveError) parts.push(`save failed: ${s.saveError}`);
                if (view.window !== 'all' && data.windowStartUnixMs) parts.push(`window from ${fmtClock(data.windowStartUnixMs)}`);
            }
            parts.push(`${drawn} cells drawn`);
            foot.textContent = parts.join(' · ');
            foot.title = foot.textContent;
            ctx.setSub(`${WINDOWS.find(w => w.value === view.window).label.toLowerCase()} · ${accountName() || 'all accounts'}`);
            wrap.dataset.drawn = String(drawn);
            wrap.dataset.renderer = renderer.kind;
            wrap.dataset.avgDrawMs = (drawMsTotal / Math.max(1, drawCount)).toFixed(2);
            if (data) { wrap.dataset.fetchMs = data.fetchMs.toFixed(1); wrap.dataset.cells = String(data.cells.length / 3); }
        }

        // ---- Leaflet mode ----
        function initLeaflet() {
            mode = 'leaflet';
            leafletEl.hidden = false;
            Promise.all([loadLeaflet(), loadTileMeta()]).then(([L, meta]) => {
                if (destroyed) return;
                if (!L || !meta) { initPlain('the map tiles could not be loaded'); return; }
                const unit = Math.pow(2, meta.nativeZoom);
                // The same transforms as the account map (map.js): lng follows world X, lat
                // follows the raster row, north up.
                const worldToLatLng = (x, z) => L.latLng(-(meta.heightAtNative - 0.5 - (z - meta.originTileZ)) / unit, (x - meta.originTileX + 0.5) / unit);
                const map = L.map(leafletEl, { crs: L.CRS.Simple, minZoom: meta.minZoom, maxZoom: meta.maxZoom + 4, attributionControl: false, zoomControl: true });
                L.tileLayer('maptiles/{z}/{x}/{y}.png', { tileSize: meta.tileSize, minZoom: meta.minZoom, maxNativeZoom: meta.maxZoom, maxZoom: meta.maxZoom + 4, noWrap: true, bounds: rasterBounds(L, meta), className: 'maptile' }).addTo(map);
                const HeatLayer = L.Layer.extend({
                    onAdd(m) {
                        this.el = el('div', 'heatlayer leaflet-zoom-animated');
                        this.canvas = el('canvas');
                        this.el.append(this.canvas);
                        m.getPanes().overlayPane.append(this.el);
                        this.renderer = createHeatRenderer(this.canvas, { prefer: ctx.settings.renderer });
                        this.renderer.onchange = () => this.reset();
                        m.on('moveend resize', this.reset, this);
                        m.on('zoomanim', this.animate, this);
                        this.reset();
                    },
                    onRemove(m) {
                        m.off('moveend resize', this.reset, this);
                        m.off('zoomanim', this.animate, this);
                        this.renderer.destroy();
                        this.el.remove();
                    },
                    reset() {
                        const m = this._map;
                        L.DomUtil.setPosition(this.el, m.containerPointToLayerPoint([0, 0]));
                        const size = m.getSize();
                        this.renderer.resize(size.x, size.y, Math.min(devicePixelRatio || 1, 2));
                        redraw();
                    },
                    animate(e) {
                        const m = this._map;
                        L.DomUtil.setTransform(this.el, m._latLngBoundsToNewLayerBounds(m.getBounds(), e.zoom, e.center).min, m.getZoomScale(e.zoom));
                    }
                });
                leaflet = { L, map, meta, worldToLatLng, layer: null };
                const restored = view.center && view.zoom !== null;
                if (restored) map.setView(worldToLatLng(view.center[0], view.center[1]), view.zoom);
                else map.setView(worldToLatLng(3222, 3218), meta.nativeZoom - 1);
                leaflet.layer = new HeatLayer().addTo(map);
                map.on('moveend', () => {
                    const f = frame(), size = map.getSize();
                    view.center = [Math.round(f.rx + (size.x / 2 - f.ox) / f.ex), Math.round(f.rz + (size.y / 2 - f.oy) / f.ez)];
                    view.zoom = map.getZoom();
                    persist();
                    reloadSoon();
                });
                map.on('mousemove', e => showTip(e.containerPoint));
                map.on('mouseout', () => { tip.hidden = true; });
                // A saved view that shows none of the heat (such as one saved on the empty band
                // between surface and underground by the old Fit) is no view worth keeping:
                // fit it like a fresh one.
                if (restored) loadOrFit();
                else load().then(() => fit(0));
                schedulePoll();
            }).catch(err => initPlain(`the map could not start (${err.message})`));
        }

        function showTip(p) {
            if (!leaflet || !data) return;
            const f = frame();
            const x = Math.round(f.rx + (p.x - f.ox) / f.ex);
            const z = Math.round(f.rz + (p.y - f.oy) / f.ez);
            const cell = data.cell;
            const gx = Math.floor(x / cell) * cell, gz = Math.floor(z / cell) * cell;
            const count = cellIndex.get(`${gx},${gz}`);
            if (!count) { tip.hidden = true; return; }
            tip.replaceChildren(el('span', 't', presence(count, sampleMs())), el('span', '', cell > 1 ? ` in ${gx}–${gx + cell - 1}, ${gz}–${gz + cell - 1}` : ` on ${gx}, ${gz}`));
            tip.hidden = false;
            tip.style.left = `${Math.min(p.x + 12, wrap.clientWidth - tip.offsetWidth - 4)}px`;
            tip.style.top = `${Math.max(4, p.y - 28)}px`;
        }

        // Fit zooms to ONE part of the world, never to the box around all of it: the
        // underground is 6400 tiles north of the surface, so the box around both is centred on
        // the empty band between them, and even the furthest zoom out cannot show both. That
        // is what left this panel black (the fleet is on both): the view landed on z ~6700,
        // drew no heat and no terrain, and was saved, so every reload went back there. The
        // server reports each area's box (areas, busiest first); the first fit takes the
        // busiest, and each further click of Fit moves on to the next.
        let fitTurn = 0;
        async function fit(turn = fitTurn) {
            try {
                // Areas ignore the box and the cell size; the coarsest grid keeps this small.
                const body = await fetchJSON(query({ cell: 1024 }));
                const areas = body.areas || [];
                if (destroyed || !areas.length) return;
                const a = areas[turn % areas.length];
                fitTurn = turn + 1;
                if (mode === 'leaflet' && leaflet) {
                    const { map, worldToLatLng, meta } = leaflet;
                    map.fitBounds([worldToLatLng(a.x0, a.z0), worldToLatLng(a.x1, a.z1)], { padding: [24, 24], maxZoom: meta.nativeZoom + 2 });
                }
                fitBtn.title = areas.length > 1
                    ? `Showing the ${a.name}; click to zoom to the ${areas[fitTurn % areas.length].name}`
                    : `Zoom to everything recorded on the ${a.name} for this choice`;
            } catch { /* the status line already reports load failures */ }
        }

        // ---- plain mode: no tile pyramid configured ----
        function initPlain(why) {
            mode = 'plain';
            leafletEl.hidden = true;
            plain.hidden = false;
            const canvas = el('canvas');
            plain.append(canvas, el('div', 'heatplainnote', `No map terrain here (${why}); heat is drawn on a plain grid fitted to what is recorded.`));
            plainView = { renderer: createHeatRenderer(canvas, { prefer: ctx.settings.renderer }), x0: 0, z0: 0, scale: 1, pad: 12, size: '' };
            plainView.renderer.onchange = () => { plainView.size = ''; redraw(); };
            load();
            schedulePoll();
        }
        function fitPlain(size) {
            const pv = plainView;
            const sig = `${size.x}x${size.y}`;
            if (pv.size !== sig) { pv.renderer.resize(size.x, size.y, Math.min(devicePixelRatio || 1, 2)); pv.size = sig; }
            const cells = data?.cells || [];
            if (!cells.length) return;
            let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity;
            for (let i = 0; i < cells.length; i += 3) {
                x0 = Math.min(x0, cells[i]); z0 = Math.min(z0, cells[i + 1]);
                x1 = Math.max(x1, cells[i] + data.cell); z1 = Math.max(z1, cells[i + 1] + data.cell);
            }
            pv.x0 = x0; pv.z0 = z0;
            pv.scale = Math.max(0.05, Math.min((size.x - 2 * pv.pad) / Math.max(1, x1 - x0), (size.y - 2 * pv.pad) / Math.max(1, z1 - z0)));
        }

        // ---- controls ----
        accountSel.addEventListener('change', () => { view.account = accountSel.value; persist(); loadOrFit(); });
        levelSel.addEventListener('change', () => { view.level = levelSel.value; persist(); loadOrFit(); });
        windowSel.addEventListener('change', () => { view.window = windowSel.value; persist(); loadOrFit(); });
        intensityIn.addEventListener('input', () => { view.intensity = Math.pow(2, Number(intensityIn.value)); setIntensityLabel(); persist(); redraw(); });
        fitBtn.addEventListener('click', () => fit());
        clearBtn.addEventListener('click', () => {
            note.textContent = '';
            if (view.account === 'selected' && !store.selected) { note.textContent = 'Select an account first, or choose All accounts.'; return; }
            confirmText.textContent = `Clear the recorded heat for ${accountLabel()}? It is deleted from memory and from the saved file, for every window and floor. This cannot be undone.`;
            confirmRow.hidden = false;
            confirmNo.focus();
        });
        confirmNo.addEventListener('click', () => { confirmRow.hidden = true; });
        confirmYes.addEventListener('click', async () => {
            confirmYes.disabled = true;
            const account = accountName();
            try {
                const res = await fetch('api/heatmap/reset', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account }) });
                const body = await res.json().catch(() => ({}));
                note.textContent = res.ok ? `Cleared ${account ? `account ${account}` : 'every account'}.` : (body.error || `HTTP ${res.status}`);
                note.classList.toggle('bad', !res.ok);
            } catch (err) {
                note.textContent = `request failed: ${err}`;
                note.classList.add('bad');
            }
            confirmYes.disabled = false;
            confirmRow.hidden = true;
            load();
        });

        function start() {
            if (mode || !store.meta) return;
            if (store.meta.mapTiles) initLeaflet();
            else initPlain('this panel server has no -maptiles pyramid');
        }
        refreshAccountOptions();
        start();
        const onResize = new ResizeObserver(() => {
            overlay.fit();
            if (mode === 'leaflet' && leaflet) leaflet.map.invalidateSize();
            else if (mode === 'plain') redraw();
        });
        onResize.observe(wrap);
        ctx.onVisibility?.(visible => {
            if (visible) { load(); schedulePoll(); }
            else { clearTimeout(pollTimer); clearTimeout(moveTimer); }
        });

        return {
            update(s, why) {
                if (anyOf(why, 'meta', 'init')) start();
                if (anyOf(why, 'status')) refreshAccountOptions();
                if (anyOf(why, 'select') && view.account === 'selected') loadOrFit();
            },
            destroy() {
                destroyed = true;
                clearTimeout(pollTimer); clearTimeout(moveTimer);
                onResize.disconnect();
                if (leaflet) leaflet.map.remove();
                plainView?.renderer.destroy();
            }
        };
    }
};
