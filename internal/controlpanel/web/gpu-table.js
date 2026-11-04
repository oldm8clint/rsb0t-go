// A virtualised, GPU-drawn stand-in for a plain <table> of many rows.
//
// Why it exists (measured, see docs/control_panel_gpu.md): the Accounts panel at the live
// 266-account roster is ~3,400 DOM nodes whose text changes every second, and scrolling it
// forced a pre-paint walk and re-raster of the whole table. Here only the rows actually on
// screen are drawn, into one canvas, as one WebGL draw call (or Canvas 2D without WebGL).
// A native scroller holding an empty spacer keeps the browser's own scrollbar, wheel,
// touch and keyboard scrolling.
//
// Visual fidelity comes from the page's own CSS rather than constants copied here: a hidden
// sample table using the real classes is measured once for fonts, colours, row height and
// paddings, so the theme in style.css stays the single source of truth.
//
// What it gives up versus the DOM table, deliberately and only when an operator opts in:
// selectable/copyable text, find-in-page, and per-cell screen-reader semantics. The DOM
// table stays the default renderer for exactly that reason.

import { createSurface, parseColor } from './gpu-text.js';
import { el } from './util.js';

function styleOf(node, pseudo) {
    const cs = getComputedStyle(node, pseudo);
    return { color: parseColor(cs.color), bg: parseColor(cs.backgroundColor), font: { weight: cs.fontWeight, size: parseFloat(cs.fontSize), family: cs.fontFamily } };
}

// Reads the table look from the real stylesheet via a hidden sample built from the same
// classes the DOM renderer uses.
function sampleTheme(host) {
    const probe = el('div');
    probe.style.position = 'absolute'; probe.style.visibility = 'hidden'; probe.style.pointerEvents = 'none'; probe.style.left = '0'; probe.style.top = '0'; probe.style.width = '600px';
    const table = el('table'), thead = el('thead'), tbody = el('tbody'), hr = el('tr'), th = el('th', '', 'Account');
    hr.append(th); thead.append(hr);
    const row = el('tr'); row.dataset.name = 'x';
    const plain = el('td', '', 'Name'), head = el('td', 'headline', 'Headline'), sec = el('td', 'secondary', 'Secondary');
    const pill = el('span', 'st st-ingame', 'ingame'); head.append(pill);
    const gained = el('span', 'gained-inline', '+1 xp'); sec.append(gained);
    row.append(plain, head, sec); tbody.append(row);
    const selRow = el('tr', 'sel'); const selTd = el('td', 'headline', 'sel'); selRow.append(selTd); tbody.append(selRow);
    // The hover colour is a rule on tr:hover, which cannot be sampled without a pointer; its
    // value is the --yellow custom property, read through a probe so any colour syntax works.
    const hoverProbe = el('span'); hoverProbe.style.color = 'var(--yellow)';
    table.append(thead, tbody); probe.append(table, hoverProbe); host.append(probe);
    const thCs = getComputedStyle(th), tdCs = getComputedStyle(plain);
    const dotColors = {};
    for (const cls of ['st-ingame', 'st-queued', 'st-connecting', 'st-reconnecting', 'st-error', '']) {
        pill.className = `st ${cls}`;
        dotColors[cls] = parseColor(getComputedStyle(pill, '::before').backgroundColor);
    }
    const theme = {
        headerH: th.getBoundingClientRect().height,
        rowH: plain.getBoundingClientRect().height,
        padTop: parseFloat(tdCs.paddingTop), padRight: parseFloat(tdCs.paddingRight),
        thPadRight: parseFloat(thCs.paddingRight), thPadBottom: parseFloat(thCs.paddingBottom),
        maxCell: parseFloat(tdCs.maxWidth) || 9999,
        th: styleOf(th), td: styleOf(plain), headline: styleOf(head), secondary: styleOf(sec), gained: styleOf(gained), sel: styleOf(selTd),
        hover: parseColor(getComputedStyle(hoverProbe).color),
        rowBorder: parseColor(tdCs.borderTopColor), headBorder: parseColor(thCs.borderBottomColor), headBg: parseColor(thCs.backgroundColor),
        dotColors, dotSize: 7, dotGap: 4, none: styleOf(plain)
    };
    probe.remove();
    return theme;
}

/**
 * createGpuTable(body, { headers, prefer, onSelect }) -> {
 *   setRows(rows, selected, emptyText), kind, stats(), destroy() }
 * A row is { key, cells: [cell] }; a cell is { runs: [{ text, style }], pill?, align?, title }
 * where style is 'td' | 'headline' | 'secondary' | 'gained' and pill is a dot colour class.
 */
export function createGpuTable(body, { headers, prefer = 'webgl', onSelect }) {
    const wrap = el('div', 'gputable');
    const scroller = el('div', 'gputable-scroll');
    const spacer = el('div', 'gputable-spacer');
    const canvas = el('canvas', 'gputable-canvas');
    canvas.setAttribute('role', 'img');
    scroller.append(spacer); wrap.append(canvas, scroller); body.append(wrap);
    const theme = sampleTheme(body);
    const surface = createSurface(canvas, prefer);
    let rows = [], selected = '', empty = '', hover = -1, widths = [], frame = 0, drawn = 0, destroyed = false;

    function font(style) { return (theme[style] || theme.td).font; }
    function color(style, rowIndex, row) {
        if (rowIndex === hover) return theme.hover;
        if (row && row.key === selected && style !== 'gained') return theme.sel.color;
        return (theme[style] || theme.td).color;
    }
    function cellWidth(cell) {
        if (cell.width !== undefined) return cell.width;
        let w = cell.pill !== undefined ? theme.dotSize + theme.dotGap : 0;
        for (const r of cell.runs) w += surface.measure(r.text, font(r.style));
        cell.width = w;
        return w;
    }
    // Column widths depend on every row, so they are laid out when the rows or the width
    // change — never per scroll step, which only chooses which rows to draw.
    let laidOut = null, layoutWidth = -1, layoutScale = -1;
    function columnWidths(available, scale) {
        if (laidOut !== rows || layoutWidth !== available || layoutScale !== scale) { widths = layoutColumns(available); laidOut = rows; layoutWidth = available; layoutScale = scale; }
        return widths;
    }
    // Auto table layout, approximately: natural width capped at max-width, extra space shared
    // in proportion, overflow taken from the widest columns first.
    function layoutColumns(available) {
        const natural = headers.map(h => Math.min(theme.maxCell, surface.measure(h, theme.th.font)) + theme.thPadRight);
        for (const row of rows) row.cells.forEach((c, j) => { natural[j] = Math.max(natural[j], Math.min(theme.maxCell, cellWidth(c)) + theme.padRight); });
        const total = natural.reduce((a, b) => a + b, 0);
        if (total <= available) return natural.map(w => w + (available - total) * (w / total));
        const floor = headers.map(h => surface.measure(h, theme.th.font) + theme.thPadRight);
        const out = [...natural]; let excess = total - available;
        while (excess > 0.5) {
            const room = out.map((w, j) => w - floor[j]);
            const shrinkable = room.reduce((a, b) => a + Math.max(0, b), 0);
            if (shrinkable <= 0) break;
            const take = Math.min(excess, shrinkable);
            out.forEach((w, j) => { if (room[j] > 0) out[j] = w - take * (room[j] / shrinkable); });
            excess -= take;
        }
        return out;
    }

    function draw() {
        frame = 0;
        if (destroyed) return;
        const w = scroller.clientWidth, h = scroller.clientHeight;
        if (w < 2 || h < 2) return;
        const dpr = window.devicePixelRatio || 1;
        surface.resize(w, h, dpr);
        spacer.style.height = (theme.headerH + Math.max(1, rows.length) * theme.rowH) + 'px';
        columnWidths(w, dpr);
        const top = scroller.scrollTop;
        surface.begin();
        const ascent = theme.td.font.size * 0.8; // baseline within a line box of font-size * line-height
        const lineBox = theme.rowH - theme.padTop * 2 - 1;
        const baseOff = theme.padTop + 1 + (lineBox - theme.td.font.size) / 2 + ascent;
        const first = Math.max(0, Math.floor((top) / theme.rowH));
        const last = Math.min(rows.length, Math.ceil((top + h - theme.headerH) / theme.rowH) + 1);
        for (let i = first; i < last; i++) {
            const row = rows[i];
            const y = theme.headerH + i * theme.rowH - top;
            surface.rect(0, y, w, 1, theme.rowBorder);
            let x = 0;
            row.cells.forEach((cell, j) => {
                const cw = widths[j] - theme.padRight;
                let cx = x;
                if (cell.align === 'right') cx = x + Math.max(0, cw - Math.min(cw, cellWidth(cell)));
                const baseline = y + baseOff;
                if (cell.pill !== undefined) {
                    surface.dot(cx + theme.dotSize / 2, baseline - theme.td.font.size * 0.3, theme.dotSize / 2, theme.dotColors[cell.pill] || theme.dotColors['']);
                    cx += theme.dotSize + theme.dotGap;
                }
                let room = x + cw - cx;
                for (const run of cell.runs) {
                    if (room <= 0) break;
                    const used = surface.text(run.text, cx, baseline, font(run.style), color(run.style, i, row), room);
                    cx += used; room -= used;
                }
                x += widths[j];
            });
        }
        if (!rows.length && empty) surface.text(empty, 0, theme.headerH + baseOff, theme.td.font, theme.secondary.color, w);
        // Sticky header, drawn last so rows scroll underneath it.
        surface.rect(0, 0, w, theme.headerH, theme.headBg);
        surface.rect(0, theme.headerH - 1, w, 1, theme.headBorder);
        let hx = 0;
        const headBase = theme.headerH - theme.thPadBottom - 1 - (theme.th.font.size * 1.15 - theme.th.font.size) / 2 - theme.th.font.size * 0.2;
        headers.forEach((label, j) => { surface.text(label, hx, headBase, theme.th.font, theme.th.color, widths[j] - theme.thPadRight); hx += widths[j]; });
        drawn = surface.end();
        canvas.setAttribute('aria-label', `${rows.length} rows drawn with ${surface.kind === 'webgl' ? 'WebGL' : 'Canvas 2D'}; switch this panel to the DOM renderer for selectable, screen-reader text`);
    }
    function schedule() { if (!frame && !destroyed) frame = requestAnimationFrame(draw); }

    function rowAt(ev) {
        const r = scroller.getBoundingClientRect();
        const y = ev.clientY - r.top + scroller.scrollTop - theme.headerH;
        if (ev.clientY - r.top < theme.headerH) return -1;
        const i = Math.floor(y / theme.rowH);
        return i >= 0 && i < rows.length ? i : -1;
    }
    function cellAt(ev, i) {
        const r = scroller.getBoundingClientRect(); let x = ev.clientX - r.left;
        for (let j = 0; j < widths.length; j++) { if (x < widths[j]) return rows[i].cells[j]; x -= widths[j]; }
        return null;
    }
    scroller.addEventListener('scroll', schedule, { passive: true });
    scroller.addEventListener('pointermove', ev => {
        const i = rowAt(ev);
        if (i !== hover) { hover = i; scroller.style.cursor = i >= 0 ? 'pointer' : ''; schedule(); }
        const cell = i >= 0 ? cellAt(ev, i) : null;
        const title = cell ? cell.title : '';
        if (scroller.title !== title) scroller.title = title;
    });
    scroller.addEventListener('pointerleave', () => { if (hover !== -1) { hover = -1; schedule(); } });
    scroller.addEventListener('click', ev => { const i = rowAt(ev); if (i >= 0 && onSelect) onSelect(rows[i].key); });
    const resize = new ResizeObserver(schedule); resize.observe(scroller);
    // Glyph advances depend on the web fonts; redraw once they are ready.
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(schedule);

    return {
        get kind() { return surface.kind; },
        setRows(next, sel, emptyText) { rows = next; selected = sel || ''; empty = emptyText || ''; schedule(); },
        stats() { return { kind: surface.kind, rows: rows.length, triangles: drawn, lost: surface.lost }; },
        destroy() { destroyed = true; cancelAnimationFrame(frame); resize.disconnect(); surface.destroy(); wrap.remove(); }
    };
}
