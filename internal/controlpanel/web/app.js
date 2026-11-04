// The page: the grid of panels, arranging it, and keeping the arrangement.
//
// Every rule about what may sit where is in layout.js, and every panel type is in panels/index.js.
// What is left here is the part that needs a browser: sections, pointer events, fetch.

import { PANELS, defaultLayout, panelType } from './panels/index.js';
import { COLUMNS, MAX_PANELS, MAX_ROWS, add, cellAt, dragSpot, newId, place, readLayout, remove, replace, resizeSpot, room, rowsUsed, vacant } from './layout.js';
import { normaliseSettings } from './settings.js';
import { customize } from './customize.js';
import { connect, store } from './store.js';
import { button, el } from './util.js';
import { setBitmapText } from './bitmapfont.js';
import { dataFresh } from './panels/incidents.js';

const LOCAL_KEY = 'rsb0t-go.dashboard.layout';
// ?layout=session: this one page load neither reads nor writes the server's saved layout (or
// this browser's copy of it). It starts from a session-only copy (or this browser's copy, or
// the default) and keeps edits in sessionStorage for this tab. For checks and scripts run
// against a panel whose operator saved a layout; a normal page load is unaffected.
const SESSION_LAYOUT = new URLSearchParams(location.search).get('layout') === 'session';
const SESSION_KEY = 'rsb0t-go.dashboard.layout.session';
const EVERYTHING = new Set(['init', 'status', 'event', 'memory', 'select', 'clock', 'conn']);
const byId = id => document.getElementById(id);
const grid = byId('grid');

let layout = readLayout(defaultLayout());
let persisted = false;
let serverHadLayout = false;
let shownRows = 1;
let dragging = null;
/** id -> { panel, sig, type, section, view } */
const mounted = new Map();
const visibilityObserver = new IntersectionObserver(changes => {
    for (const change of changes) {
        const entry = mounted.get(change.target.dataset.id);
        if (!entry) continue;
        entry.inViewport = change.isIntersecting;
        updateVisibility(entry);
    }
}, { rootMargin: '150px' });

function updateVisibility(entry) {
    const visible = entry.inViewport && !document.hidden;
    if (visible === entry.visible) return;
    entry.visible = visible;
    for (const fn of entry.visibilityListeners) fn(visible);
    if (visible) refresh(entry, new Set(['clock']));
}

document.addEventListener('visibilitychange', () => {
    for (const entry of mounted.values()) updateVisibility(entry);
});

const editing = () => document.body.classList.contains('is-editing');

function note(text, bad = false) {
    const n = byId('note');
    n.textContent = text;
    n.classList.toggle('bad', bad);
}

// ---- keeping the arrangement: the server's file when it has one, this browser's copy always ----

function readLocal() {
    try {
        const raw = localStorage.getItem(LOCAL_KEY);
        return raw === null ? null : JSON.parse(raw);
    } catch {
        return null;
    }
}

function writeLocal() {
    try {
        localStorage.setItem(LOCAL_KEY, JSON.stringify(layout));
        return true;
    } catch {
        return false;
    }
}

async function loadLayout() {
    if (SESSION_LAYOUT) {
        let session = null;
        try { const raw = sessionStorage.getItem(SESSION_KEY); session = raw === null ? null : JSON.parse(raw); } catch { session = null; }
        note('session layout: this page does not load or save the saved layout');
        return readLayout(session) ?? readLayout(readLocal()) ?? readLayout(defaultLayout());
    }
    let saved = null;
    try {
        const res = await fetch('api/layout', { cache: 'no-store' });
        if (res.ok) {
            const body = await res.json();
            persisted = body.persisted === true;
            saved = body.layout;
            serverHadLayout = saved !== null;
            if (body.problem) note(`the saved layout could not be read (${body.problem}), so this one is not it`, true);
        }
    } catch {
        // No answer: this browser's copy is the next best thing.
    }
    return readLayout(saved) ?? readLayout(readLocal()) ?? readLayout(defaultLayout());
}

let saveTimer = 0;

function save() {
    if (SESSION_LAYOUT) {
        try { sessionStorage.setItem(SESSION_KEY, JSON.stringify(layout)); } catch { /* this tab only */ }
        note('session layout: kept in this tab only, not saved');
        return;
    }
    const local = writeLocal();
    if (!persisted) {
        note(local ? 'layout saved in this browser' : 'layout not saved: this browser refuses storage', !local);
        return;
    }
    clearTimeout(saveTimer);
    saveTimer = setTimeout(async () => {
        try {
            const res = await fetch('api/layout', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(layout) });
            if (res.ok) {
                note('layout saved');
                return;
            }
            const body = await res.json().catch(() => ({}));
            note(`layout kept in this browser only; the server refused it: ${body.error ?? `HTTP ${res.status}`}`, true);
        } catch {
            note('layout kept in this browser only; the server did not answer', true);
        }
    }, 300);
}

// ---- panels on the page ----

const signature = p => JSON.stringify([p.type, p.title, p.accounts, p.settings]);

function refresh(entry, why) {
    for (const reason of why) entry.pending.add(reason);
    if (!entry.visible && !entry.type?.backgroundUpdates) return;
    entry.start?.();
    if (entry.view === null) return;
    const pending = entry.pending;
    entry.pending = new Set();
    try {
        entry.view.update(store, pending);
    } catch (err) {
        console.error(`[dashboard] panel ${entry.panel.id} failed to update`, err);
    }
}

function mount(panel, sig) {
    const type = panelType(panel.type);
    const name = panel.title || (type ? type.title : panel.type);
    const section = el('section', 'window');
    section.dataset.id = panel.id;
    const head = el('h2');
    const sub = el('span', 'sub');
    const tools = el('span', 'tools');
    const gear = button('⚙', `customize ${name}`, 'gear');
    const drop = button('×', `remove ${name}`, 'drop');
    tools.append(gear, drop);
    const titleEl = el('span', 'title');
    setBitmapText(titleEl, name, 'p12');
    head.append(titleEl, sub, tools);
    const body = el('div', 'body');
    section.append(head, body, el('span', 'grip'));
    grid.append(section);

    const entry = { panel, sig, type, section, view: null, inViewport: false, visible: false, pending: new Set(), visibilityListeners: new Set() };
    mounted.set(panel.id, entry);
    visibilityObserver.observe(section);
    drop.addEventListener('click', () => {
        layout = remove(layout, panel.id);
        settle();
    });
    if (type === null) {
        gear.hidden = true;
        body.append(el('div', 'none', `"${panel.type}" is not a panel type this build knows. It is kept for the build that added it; remove it with the cross while editing.`));
        return;
    }
    gear.addEventListener('click', () => openCustomize(panel.id));
    const ctx = {
        id: panel.id,
        store,
        accounts: panel.accounts,
        settings: normaliseSettings(type.settings, panel.settings),
        isVisible: () => entry.visible,
        onVisibility: fn => { entry.visibilityListeners.add(fn); return () => entry.visibilityListeners.delete(fn); },
        setSub: text => {
            if (sub.textContent !== text) sub.textContent = text;
        }
    };
    // Heavy panels create their canvases, observers and requests only on first exposure.
    entry.start = () => {
        entry.start = null;
        try { entry.view = type.create(body, ctx); }
        catch (err) { body.textContent = `this panel failed to start: ${err}`; }
    };
    refresh(entry, EVERYTHING);
}

function unmount(id) {
    const entry = mounted.get(id);
    if (!entry) return;
    visibilityObserver.unobserve(entry.section);
    try {
        entry.view?.destroy?.();
    } catch {
        // A panel that fails to tidy up is still removed.
    }
    entry.section.remove();
    mounted.delete(id);
}

/** Make the page match the layout: mount, remount what was customized, unmount, then place. */
function sync() {
    const ids = new Set(layout.panels.map(p => p.id));
    for (const id of [...mounted.keys()]) if (!ids.has(id)) unmount(id);
    for (const panel of layout.panels) {
        const sig = signature(panel);
        if (mounted.has(panel.id) && mounted.get(panel.id).sig !== sig) unmount(panel.id);
        if (!mounted.has(panel.id)) mount(panel, sig);
        const entry = mounted.get(panel.id);
        entry.panel = panel;
        const style = entry.section.style;
        style.gridColumn = `${panel.col + 1} / span ${panel.w}`;
        style.gridRow = `${panel.row + 1} / span ${panel.h}`;
        style.order = String(panel.row * COLUMNS + panel.col);
        style.setProperty('--h', String(panel.h));
    }
    // While arranging, two spare rows give a panel somewhere to be dragged to.
    shownRows = Math.min(MAX_ROWS, Math.max(1, rowsUsed(layout) + (editing() ? 2 : 0)));
    const rows = `repeat(${shownRows}, var(--row))`;
    grid.style.gridTemplateRows = rows;
    const cells = byId('cells');
    cells.style.gridTemplateRows = rows;
    if (cells.childElementCount !== shownRows * COLUMNS) {
        cells.textContent = '';
        for (let i = 0; i < shownRows * COLUMNS; i++) cells.append(el('div'));
    }
    byId('empty').hidden = layout.panels.length > 0;
    drawNavigation();
}

function drawNavigation() {
    const select = byId('panel-jump');
    if (!select) return;
    const placeholder = el('option', '', 'Jump to panel…'); placeholder.value = '';
    select.replaceChildren(placeholder);
    for (const panel of [...layout.panels].sort((a,b) => a.row - b.row || a.col - b.col)) {
        const option = el('option', '', panel.title || panelType(panel.type)?.title || panel.type);
        option.value = panel.id; select.append(option);
    }
}

function wireNavigation() {
    const bar = el('nav', 'dashboard-nav'); bar.setAttribute('aria-label', 'Dashboard navigation');
    const jump = el('select'); jump.id = 'panel-jump'; jump.setAttribute('aria-label', 'Jump to panel');
    jump.addEventListener('change', () => {
        const section = mounted.get(jump.value)?.section;
        section?.scrollIntoView({block:'start'});
        if (section) { section.tabIndex = -1; section.focus({preventScroll:true}); }
        jump.value = '';
    });
    const fresh = el('span', 'pill'); fresh.id = 'data-freshness'; fresh.setAttribute('role', 'status');
    const selected = button('No account selected', 'Jump to account details'); selected.id = 'selected-account';
    selected.addEventListener('click', () => {
        const entry = [...mounted.values()].find(e => e.panel.type === 'account' && (!e.panel.accounts.length || e.panel.accounts[0] === store.selected));
        entry?.section.scrollIntoView({block:'start'});
    });
    bar.append(jump, fresh, selected); grid.before(bar);
    const input = el('input'); input.type = 'search'; input.placeholder = 'Find a panel…';
    input.setAttribute('aria-label', 'Search available panels');
    // Add menu is rebuilt only at boot; search never touches the saved arrangement.
    input.addEventListener('input', () => {
        const query = input.value.trim().toLowerCase();
        for (const b of byId('addmenu').querySelectorAll('button')) b.hidden = !b.textContent.toLowerCase().includes(query);
    });
    byId('addmenu').prepend(input);
}

function drawContext() {
    const fresh = byId('data-freshness');
    const text = !store.statusReceivedAt ? 'Waiting for status' : dataFresh(store) ? 'Status current' : `Status stale · ${Math.floor((Date.now()-store.statusReceivedAt)/1000)}s old`;
    if (fresh.textContent !== text) fresh.textContent = text;
    fresh.classList.toggle('bad', !dataFresh(store));
    const selected = byId('selected-account');
    const label = store.selected ? `Selected: ${store.selected}` : 'No account selected';
    if (selected.textContent !== label) selected.textContent = label;
    selected.disabled = !store.selected;
}

function settle() {
    sync();
    save();
}

function openCustomize(id) {
    const entry = mounted.get(id);
    if (!entry || entry.type === null) return;
    customize(
        byId('customize'),
        entry.type,
        entry.panel,
        store.accounts.map(a => a.name),
        next => {
            layout = replace(layout, next);
            settle();
        }
    );
}

function drawAddMenu() {
    const menu = byId('addmenu');
    menu.textContent = '';
    for (const type of PANELS) {
        const b = button('', `add ${type.title}`);
        b.append(el('b', '', `+ ${type.title}`), el('small', '', type.description));
        if (type.needsData) b.append(el('small', 'tag', 'needs data the fleet does not report yet'));
        b.addEventListener('click', () => {
            if (layout.panels.length >= MAX_PANELS) {
                note(`a page holds at most ${MAX_PANELS} panels`, true);
                return;
            }
            const spot = room(layout, type.size.w, type.size.h);
            if (spot === null) {
                note('no room left for a panel that size; remove or shrink one first', true);
                return;
            }
            const panel = { id: newId(layout, type.type), type: type.type, title: '', ...spot, accounts: [], settings: normaliseSettings(type.settings, {}) };
            layout = add(layout, panel) ?? layout;
            menu.hidden = true;
            byId('add').setAttribute('aria-expanded', 'false');
            settle();
            mounted.get(panel.id)?.section.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        });
        menu.append(b);
    }
}

// ---- arranging: move by dragging a panel, resize by its corner ----

function cellUnder(ev) {
    const rect = grid.getBoundingClientRect();
    return cellAt(rect, shownRows, ev.clientX - rect.left, ev.clientY - rect.top);
}

function showGhost(spot, ok) {
    const ghost = byId('ghost');
    ghost.hidden = false;
    ghost.className = ok ? '' : 'is-clash';
    ghost.style.gridColumn = `${spot.col + 1} / span ${spot.w}`;
    ghost.style.gridRow = `${spot.row + 1} / span ${spot.h}`;
}

function startDrag(ev) {
    // A narrow window stacks the panels, and a drag there would have no grid to land on.
    if (!editing() || ev.button !== 0 || getComputedStyle(grid).display !== 'grid') return;
    const target = ev.target;
    if (!(target instanceof Element) || target.closest('.tools')) return;
    const section = target.closest('section.window');
    const panel = section ? layout.panels.find(p => p.id === section.dataset.id) : undefined;
    if (!panel) return;
    ev.preventDefault();
    const from = { col: panel.col, row: panel.row, w: panel.w, h: panel.h };
    dragging = { id: panel.id, section, mode: target.closest('.grip') ? 'size' : 'move', from, grab: cellUnder(ev), x: ev.clientX, y: ev.clientY, want: from };
    if (dragging.mode === 'move') section.classList.add('is-dragging');
    try {
        grid.setPointerCapture(ev.pointerId);
    } catch {
        // Without capture the moves still arrive while the pointer is over the grid.
    }
}

function moveDrag(ev) {
    if (!dragging) return;
    const at = cellUnder(ev);
    dragging.want = dragging.mode === 'move' ? dragSpot(dragging.from, dragging.grab, at) : resizeSpot(dragging.from, at);
    if (dragging.mode === 'move') dragging.section.style.transform = `translate(${ev.clientX - dragging.x}px, ${ev.clientY - dragging.y}px)`;
    const ok = vacant(layout, dragging.want, dragging.id);
    dragging.section.classList.toggle('is-clash', !ok);
    showGhost(dragging.want, ok);
}

/** Let go. A refused move springs back: clearing the transform on a panel still in its cells is the spring. */
function endDrag(keep) {
    const drag = dragging;
    if (!drag) return;
    dragging = null;
    byId('ghost').hidden = true;
    drag.section.classList.remove('is-clash');
    const before = drag.section.getBoundingClientRect();
    drag.section.style.transition = 'none';
    drag.section.style.transform = '';
    const next = keep ? place(layout, drag.id, drag.want) : null;
    if (next !== null) {
        layout = next;
        settle();
    }
    if (drag.mode === 'move') {
        const after = drag.section.getBoundingClientRect();
        drag.section.style.transform = `translate(${before.left - after.left}px, ${before.top - after.top}px)`;
        void drag.section.offsetWidth;
    }
    drag.section.classList.remove('is-dragging');
    drag.section.style.transition = '';
    drag.section.style.transform = '';
}

function setEditing(on) {
    document.body.classList.toggle('is-editing', on);
    const b = byId('edit');
    b.classList.toggle('is-active', on);
    b.textContent = on ? 'done' : 'edit layout';
    sync();
}

function drawConn() {
    const pill = byId('conn');
    const words = { connecting: 'connecting', live: 'live', lost: 'reconnecting', unauthorized: 'not signed in' };
    pill.className = `pill ${store.conn}`;
    pill.textContent = store.conn === 'live' && store.dropped > 0 ? `live, ${store.dropped} dropped` : (words[store.conn] ?? store.conn);
    pill.title =
        store.conn === 'unauthorized'
            ? 'This dashboard needs its token. Open it once as /?token=<token>.'
            : store.dropped > 0
              ? `${store.dropped} events were dropped because this tab fell behind the stream — fetched the gap automatically`
              : '';
}

// pollEngineTick shows the local test engine's current game-tick speed (set live by the
// ::speed cheat) in the header — see Config.EngineHTTPBase's own doc comment on the Go side.
// Hidden entirely for a production fleet (the route doesn't exist there, so `available` is
// always false) or before the first successful fetch.
async function pollEngineTick() {
    if (document.hidden) return;
    const pill = byId('enginetick');
    try {
        const res = await fetch('api/enginetick', { cache: 'no-store' });
        if (!res.ok) { pill.hidden = true; return; }
        const body = await res.json();
        if (!body.available) { pill.hidden = true; return; }
        const ms = body.tickRateMs;
        pill.hidden = false;
        pill.textContent = ms >= 600 ? `tick ${ms}ms` : `tick ${ms}ms (${(600 / ms).toFixed(1)}x)`;
        pill.classList.toggle('fast', ms < 600);
    } catch {
        pill.hidden = true;
    }
}

// ---- whole-fleet process controls: Stop/Restart/Start in the header ----
//
// Deliberately separate from account.js's own per-account Start/Stop (POST
// /api/bot/start|stop, one account's script) and Disconnect/Reconnect (POST
// /api/fleet/disconnect|connect, one account's connection) — these three affect every
// connected account at once, via POST /api/fleet/stop-all and /api/fleet/restart-all
// (see internal/controlpanel/server.go's handleFleetStopAll/handleFleetRestartAll and
// Config.OnStopAll/OnRestartAll's own doc comments for exactly what each does).
//
// Start is kept visible but permanently disabled rather than omitted: a genuine
// chicken-and-egg problem, not an oversight — if the fleet process is not already
// running, there is no server here to have served this page, or a working Start button
// on it, at all. Its tooltip (set in index.html) points at run\start-fleet.bat instead.

function fleetNoteSay(text, bad = false) {
    const n = byId('fleetnote');
    n.textContent = text;
    n.classList.toggle('bad', bad);
}

function setFleetBusy(busy) {
    byId('fleet-stop').disabled = busy || !store.meta?.fleetStop;
    byId('fleet-restart').disabled = busy || !store.meta?.fleetRestart;
    byId('scripts-stop-all').disabled = busy || !store.meta?.botControl;
    byId('scripts-start-all').disabled = busy || !store.meta?.botControl;
    for (const action of ['requeue', 'logout', 'login']) byId(`accounts-${action}`).disabled = busy || !store.meta?.bulkConnectionControl;
}

// updateFleetControls enables Stop/Restart only once /api/meta has actually reported
// Config.OnStopAll/OnRestartAll are wired up on THIS build (e.g. still disabled for a
// `go test` harness, or a future build that runs this package with neither wired) — the
// same "hide what this build cannot actually do" gating account.js's own Start/Stop
// already follows for BotControl.
function updateFleetControls() {
    const meta = store.meta;
    if (!meta) return;
    byId('fleet-stop').disabled = !meta.fleetStop;
    byId('fleet-restart').disabled = !meta.fleetRestart;
    byId('scripts-stop-all').disabled = !meta.botControl;
    byId('scripts-start-all').disabled = !meta.botControl;
    for (const action of ['requeue', 'logout', 'login']) byId(`accounts-${action}`).disabled = !meta.bulkConnectionControl;
    if (!meta.fleetStop && !meta.fleetRestart) {
        fleetNoteSay('whole-fleet stop/restart is not wired up on this build');
    }
}

function accountCount() {
    return store.accounts.length;
}

function wireFleetControls() {
    for (const action of ['requeue', 'logout', 'login']) {
        byId(`accounts-${action}`).addEventListener('click', async () => {
            setFleetBusy(true);
            fleetNoteSay(`${action}: updating account connections...`);
            try {
                const res = await fetch(`api/fleet/${action}-accounts`, { method: 'POST' });
                const body = await res.json();
                const errors = Object.entries(body.errors ?? {});
                const queued = body.skippedQueued ? `; ${body.skippedQueued} startup entries unchanged` : '';
                const message = res.ok ? `${action}: ${body.changed} accepted, ${body.skipped} skipped${queued}${errors.length ? `; ${errors.length} failed` : ''}` : (body.error ?? `HTTP ${res.status}`);
                fleetNoteSay(message, !res.ok || body.ok === false);
                byId('fleetnote').title = errors.map(([name, error]) => `${name}: ${error}`).join('\n');
            } catch (error) {
                fleetNoteSay(`account connection control failed: ${error.message}`, true);
            } finally {
                setFleetBusy(false);
            }
        });
    }

    for (const action of ['stop', 'start']) {
        byId(`scripts-${action}-all`).addEventListener('click', async () => {
            setFleetBusy(true);
            fleetNoteSay(`${action === 'stop' ? 'stopping' : 'starting'} all scripts...`);
            try {
                const res = await fetch(`api/bot/${action}-all`, { method: 'POST' });
                const body = await res.json();
                const errors = Object.entries(body.errors ?? {}).map(([name, error]) => `${name}: ${error}`);
                fleetNoteSay(res.ok ? `${body.changed} scripts ${action === 'stop' ? 'stopping' : 'started'}, ${body.skipped} skipped${errors.length ? '; ' + errors.join('; ') : ''}` : (body.error ?? `HTTP ${res.status}`), !res.ok || body.ok === false);
            } catch (error) {
                fleetNoteSay(`script control failed: ${error.message}`, true);
            } finally {
                setFleetBusy(false);
            }
        });
    }
    byId('fleet-stop').addEventListener('click', async () => {
        const n = accountCount();
        const sure = confirm(
            `Stop the WHOLE fleet?\n\nAll ${n} connected account(s) will be logged out, and this process will exit.\n\n` +
            'This cannot be undone from here — starting it again needs run\\start-fleet.bat, outside this dashboard.'
        );
        if (!sure) return;
        setFleetBusy(true);
        fleetNoteSay('stopping the fleet — every account is being logged out...');
        try {
            const res = await fetch('api/fleet/stop-all', { method: 'POST' });
            const body = await res.json().catch(() => ({}));
            fleetNoteSay(res.ok ? (body.message ?? 'stopping...') : (body.error ?? `HTTP ${res.status}`), !res.ok);
            // A failure here (501/other) leaves the fleet running, so re-enable; success
            // means this process is on its way down and there is nothing left to click.
            if (!res.ok) setFleetBusy(false);
        } catch {
            fleetNoteSay('the dashboard stopped answering — it is very likely shutting down now');
        }
    });

    byId('fleet-restart').addEventListener('click', async () => {
        const n = accountCount();
        const sure = confirm(
            `Restart the WHOLE fleet?\n\nAll ${n} connected account(s) will be logged out, then it relaunches on whatever build ` +
            'currently sits next to this binary (run\\restart-fleet.bat: stop, a ~10 minute cooldown, then start).\n\n' +
            'This dashboard will go offline until the new process comes back up.'
        );
        if (!sure) return;
        setFleetBusy(true);
        fleetNoteSay('launching the restart...');
        try {
            const res = await fetch('api/fleet/restart-all', { method: 'POST' });
            const body = await res.json().catch(() => ({}));
            fleetNoteSay(res.ok ? (body.message ?? 'restarting...') : (body.error ?? `HTTP ${res.status}`), !res.ok);
            // A failure means the RELAUNCH ITSELF could not be triggered (e.g. the script
            // was not found) — the fleet is still up, so let the operator try again.
            if (!res.ok) setFleetBusy(false);
        } catch {
            fleetNoteSay('the dashboard stopped answering — it is very likely restarting now');
        }
    });
}

function wire() {
    const h1 = document.querySelector('header h1');
    if (h1) setBitmapText(h1, h1.textContent, 'b12');
    grid.addEventListener('pointerdown', startDrag);
    grid.addEventListener('pointermove', moveDrag);
    grid.addEventListener('pointerup', () => endDrag(true));
    // A pointer taken away is not a drop; without this the panel is left in mid-air.
    grid.addEventListener('pointercancel', () => endDrag(false));
    document.addEventListener('keydown', ev => {
        if (ev.key === 'Escape') endDrag(false);
    });
    byId('edit').addEventListener('click', () => setEditing(!editing()));
    byId('add').addEventListener('click', () => {
        const menu = byId('addmenu');
        menu.hidden = !menu.hidden;
        byId('add').setAttribute('aria-expanded', String(!menu.hidden));
    });
    byId('reset').addEventListener('click', () => {
        if (confirm("Replace every panel with the default set? Each panel's settings are replaced too.")) {
            layout = readLayout(defaultLayout());
            settle();
        }
    });
    wireFleetControls();
}

async function boot() {
    wire();
    drawAddMenu();
    wireNavigation();
    const hadLocal = readLocal() !== null;
    layout = await loadLayout();
    sync();
    if (persisted && !serverHadLayout) save();
    // Without -layout FILE the arrangement lives only in this browser's storage, which is
    // per ADDRESS: 127.0.0.1:7300 and localhost:7300 are different stores. Say so, rather
    // than silently showing the default layout as if the operator's arrangement were gone.
    if (!persisted && !SESSION_LAYOUT) {
        note(hadLocal
            ? `layout kept only in this browser for ${location.host} (the panel runs without -layout FILE)`
            : `default layout: nothing saved for ${location.host} in this browser. Layouts are kept per browser address unless the panel runs with -layout FILE, so an arrangement made at another address is still there`, !hadLocal);
    }
    store.on(why => {
        if (why.has('conn') || why.has('dropped')) drawConn();
        if (why.has('meta')) updateFleetControls();
        if (['status','clock','conn','select'].some(reason => why.has(reason))) drawContext();
        for (const entry of mounted.values()) refresh(entry, why);
    });
    drawConn();
    drawContext();
    connect();
    pollEngineTick();
    setInterval(pollEngineTick, 5000);
}

boot();
