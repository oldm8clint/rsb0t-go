// Where each panel sits, and every rule about what may sit where. Pure: no DOM, so each rule reads
// the same in the browser and in layout.go, which enforces the same bounds on the server.
//
// The model is the Control page's (controlLayout.ts in rsb0t-scripts) with two changes: panels
// are instances of a type rather than one fixed panel each, and the grid grows downwards instead
// of being one screen. A layout is { v, panels: [{ id, type, title, col, row, w, h, accounts, settings }] }.

export const COLUMNS = 12;
export const MAX_ROWS = 48;
export const MAX_PANELS = 32;
export const LAYOUT_VERSION = 1;

const ID = /^[a-z0-9][a-z0-9-]{0,23}$/;
const TYPE = /^[a-z][a-z0-9-]{0,23}$/;
const code = String.fromCharCode;
/** C0 and C1 control characters, which the server refuses in a title or an account name. */
export const CONTROL = new RegExp(`[${code(0)}-${code(31)}${code(127)}-${code(159)}]`, 'g');
const HAS_CONTROL = new RegExp(CONTROL.source);

const whole = n => Number.isInteger(n);
const clamp = (n, low, high) => Math.max(low, Math.min(high, n));

/** A spot wholly on the board. */
export function onGrid(s) {
    return whole(s.col) && whole(s.row) && whole(s.w) && whole(s.h) && s.w >= 1 && s.h >= 1 && s.col >= 0 && s.row >= 0 && s.col + s.w <= COLUMNS && s.row + s.h <= MAX_ROWS;
}

export function clash(a, b) {
    return a.col < b.col + b.w && b.col < a.col + a.w && a.row < b.row + b.h && b.row < a.row + a.h;
}

/** On the board and free. `except` is the panel being moved, which must not collide with itself. */
export function vacant(layout, spot, except) {
    return onGrid(spot) && layout.panels.every(p => p.id === except || !clash(spot, p));
}

/** The layout with one panel moved or resized, or null when the spot will not have it. */
export function place(layout, id, spot) {
    if (!vacant(layout, spot, id)) return null;
    return { ...layout, panels: layout.panels.map(p => (p.id === id ? { ...p, col: spot.col, row: spot.row, w: spot.w, h: spot.h } : p)) };
}

export function add(layout, panel) {
    if (layout.panels.length >= MAX_PANELS || !vacant(layout, panel)) return null;
    return { ...layout, panels: [...layout.panels, panel] };
}

export function remove(layout, id) {
    return { ...layout, panels: layout.panels.filter(p => p.id !== id) };
}

export function replace(layout, panel) {
    return { ...layout, panels: layout.panels.map(p => (p.id === panel.id ? panel : p)) };
}

/** The first free spot of that size in reading order, or null when the grid has none. */
export function room(layout, w, h) {
    for (let row = 0; row + h <= MAX_ROWS; row++) {
        for (let col = 0; col + w <= COLUMNS; col++) {
            const spot = { col, row, w, h };
            if (vacant(layout, spot)) return spot;
        }
    }
    return null;
}

export function rowsUsed(layout) {
    return layout.panels.reduce((n, p) => Math.max(n, p.row + p.h), 0);
}

/** Which cell a point in the grid is over, for a grid currently showing `rows` rows. */
export function cellAt(rect, rows, x, y) {
    const col = rect.width <= 0 ? 0 : Math.floor((x / rect.width) * COLUMNS);
    const row = rect.height <= 0 ? 0 : Math.floor((y / rect.height) * rows);
    return { col: clamp(col, 0, COLUMNS - 1), row: clamp(row, 0, MAX_ROWS - 1) };
}

/** A moved panel keeps its size and its grip, and stops at an edge rather than hanging off it. */
export function dragSpot(spot, grab, at) {
    return { col: clamp(spot.col + at.col - grab.col, 0, COLUMNS - spot.w), row: clamp(spot.row + at.row - grab.row, 0, MAX_ROWS - spot.h), w: spot.w, h: spot.h };
}

export function resizeSpot(spot, at) {
    return { col: spot.col, row: spot.row, w: clamp(at.col - spot.col + 1, 1, COLUMNS - spot.col), h: clamp(at.row - spot.row + 1, 1, MAX_ROWS - spot.row) };
}

export function newId(layout, type) {
    const base = type.slice(0, 15);
    for (let n = 1; ; n++) {
        const id = `${base}-${n}`;
        if (!layout.panels.some(p => p.id === id)) return id;
    }
}

/**
 * A stored layout made safe, or null when it is not a layout at all.
 *
 * Nothing here throws. A panel with a bad id or type is dropped; one whose spot is taken or off the
 * board is moved to the first free spot of its size. Panels are read in stored order, so two that
 * claim the same cells untangle the same way every time.
 */
export function readLayout(raw) {
    if (typeof raw !== 'object' || raw === null || raw.v !== LAYOUT_VERSION || !Array.isArray(raw.panels)) return null;
    let out = { v: LAYOUT_VERSION, panels: [] };
    for (const p of raw.panels) {
        if (out.panels.length >= MAX_PANELS) break;
        if (typeof p !== 'object' || p === null || typeof p.id !== 'string' || !ID.test(p.id) || typeof p.type !== 'string' || !TYPE.test(p.type)) continue;
        if (out.panels.some(q => q.id === p.id)) continue;
        const base = {
            id: p.id,
            type: p.type,
            title: typeof p.title === 'string' ? p.title.replace(CONTROL, '').slice(0, 48) : '',
            accounts: Array.isArray(p.accounts) ? p.accounts.filter(a => typeof a === 'string' && a.length > 0 && a.length <= 32 && !HAS_CONTROL.test(a)).slice(0, 256) : [],
            settings: typeof p.settings === 'object' && p.settings !== null && !Array.isArray(p.settings) ? p.settings : {}
        };
        const want = { col: p.col, row: p.row, w: p.w, h: p.h };
        const spot = vacant(out, want) ? want : room(out, whole(p.w) ? clamp(p.w, 1, COLUMNS) : 2, whole(p.h) ? clamp(p.h, 1, MAX_ROWS) : 2);
        if (spot === null) continue;
        out = { ...out, panels: [...out.panels, { ...base, ...spot }] };
    }
    return out;
}
