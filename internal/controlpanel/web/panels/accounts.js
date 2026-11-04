import { anyOf, botStatePill, el, fmtCount, fmtDuration, pick, serverNow, statePill } from '../util.js';
import { createGpuTable } from '../gpu-table.js';

const FIELDS = [
    { value: 'state', label: 'State' },
    { value: 'bot', label: 'Bot' },
    { value: 'botState', label: 'Bot state' },
    { value: 'primary', label: 'Primary skill' },
    { value: 'activity', label: 'Activity' },
    { value: 'pos', label: 'Position' },
    { value: 'route', label: 'Route' },
    { value: 'since', label: 'In state for' },
    { value: 'packets', label: 'Packets' },
    { value: 'staffLevel', label: 'Staff level' },
    { value: 'reconnects', label: 'Reconnects' },
    { value: 'lastError', label: 'Last error' },
    { value: 'counters', label: 'Counters' }
];

const SORTS = [
    { value: 'name', label: 'Name' },
    { value: 'state', label: 'State, trouble first' },
    { value: 'bot', label: 'Bot' },
    { value: 'since', label: 'Longest in its state' },
    { value: 'route', label: 'Route' }
];

const RANK = { error: 0, reconnecting: 1, queued: 2, connecting: 3, ingame: 4, stopped: 5, offline: 6 };

// The "status strip" columns users asked for — connected/bot-state/activity/key-stat
// all visible without clicking into a detail panel — get the bold, full-brightness treatment
// (see .headline in style.css); the rest (packets, staff level, since, ...), useful once you've
// already clicked in but not worth shouting at, stay dim (.secondary). Kept as a Set rather than
// hardcoding per-column so a future FIELDS entry defaults to "secondary" unless added here.
const HEADLINE = new Set(['state', 'botState', 'activity', 'primary']);

function sorted(list, by) {
    const out = [...list];
    const name = (a, b) => a.name.localeCompare(b.name);
    if (by === 'state') out.sort((a, b) => (RANK[a.state] ?? 9) - (RANK[b.state] ?? 9) || name(a, b));
    else if (by === 'bot') out.sort((a, b) => a.bot.localeCompare(b.bot) || name(a, b));
    else if (by === 'since') out.sort((a, b) => a.since - b.since || name(a, b));
    else if (by === 'route') out.sort((a, b) => (a.route ?? '').localeCompare(b.route ?? '') || name(a, b));
    else out.sort(name);
    return out;
}

function text(a, field, now) {
    switch (field) {
        case 'state':
            return a.state;
        case 'bot':
            return a.bot || '-';
        case 'botState':
            return a.botState || '-';
        case 'primary': {
            if (!a.primarySkill) return '-';
            const sk = (a.skills ?? {})[a.primarySkill];
            if (!sk) return `${a.primarySkill} (not yet observed)`;
            const gained = sk.xpGained > 0 ? ` +${fmtCount(sk.xpGained)}` : '';
            return `${a.primarySkill} ${sk.level} (${fmtCount(sk.xp)} xp${gained})`;
        }
        case 'activity':
            return a.activity || '-';
        case 'route':
            // The proxy pool route this account logs in through; '-' with no pool.
            return a.routeWait ? 'queued for a route' : (a.route || '-');
        case 'pos':
            return a.pos ? `${a.pos.x}, ${a.pos.z}${a.pos.level ? `, level ${a.pos.level}` : ''}` : '-';
        case 'since':
            return fmtDuration(now - a.since);
        case 'packets':
            return fmtCount(a.packets);
        case 'staffLevel':
            return String(a.staffLevel);
        case 'reconnects':
            return String(a.reconnects);
        case 'lastError':
            return a.lastError || '-';
        case 'counters':
            return (a.counters ?? []).map(c => `${c.name} ${fmtCount(c.value)}`).join(', ') || '-';
        default:
            return '';
    }
}

export default {
    type: 'accounts',
    title: 'Accounts',
    description: 'A row per account with the columns you pick. Click a row to follow it in a detail panel.',
    size: { w: 8, h: 3 },
    accounts: 'many',
    settings: [
        { key: 'fields', label: 'Columns', kind: 'multi', options: FIELDS, default: ['state', 'botState', 'activity', 'primary', 'bot', 'since', 'packets', 'lastError'] },
        { key: 'sort', label: 'Sort by', kind: 'select', options: SORTS, default: 'name' },
        // DOM stays the default: it keeps selectable text, find-in-page and screen readers.
        // The GPU table draws only the rows on screen (see gpu-table.js for what it gives up).
        { key: 'renderer', label: 'Draw rows with', kind: 'select', options: [
            { value: 'dom', label: 'DOM table (selectable text)' },
            { value: 'gpu', label: 'GPU / WebGL (Canvas 2D fallback)' }
        ], default: 'dom' }
    ],
    create(body, ctx) {
        const controls = el('div', 'account-filters');
        const search = el('input'); search.type = 'search'; search.placeholder = 'Find account, script, activity, route...';
        search.setAttribute('aria-label', 'Filter accounts');
        const state = el('select'); state.setAttribute('aria-label', 'Filter connection state');
        for (const value of ['', 'ingame', 'reconnecting', 'connecting', 'queued', 'error', 'offline', 'stopped']) {
            const option = el('option', '', value || 'All states'); option.value = value; state.append(option);
        }
        controls.append(search, state); body.append(controls);
        const filtered = {...ctx, filter: list => {
            const query = search.value.trim().toLowerCase();
            if (!query && !state.value) return list;
            return list.filter(a => (!state.value || a.state === state.value) &&
                (!query || [a.name,a.bot,a.activity,a.route,a.world].some(v => String(v ?? '').toLowerCase().includes(query))));
        }};
        const view = ctx.settings.renderer === 'gpu' ? createGpu(body, filtered) : createDom(body, filtered);
        const refresh = () => view.update(ctx.store, new Set(['init']));
        search.addEventListener('input', refresh); state.addEventListener('change', refresh);
        return view;
    }
};

function createDom(body, ctx) {
        const fields = ctx.settings.fields;
        const head = el('tr');
        head.append(el('th', '', 'Account'));
        for (const f of fields) head.append(el('th', '', FIELDS.find(x => x.value === f).label));
        const thead = el('thead');
        thead.append(head);
        const rows = el('tbody');
        const t = el('table');
        t.append(thead, rows);
        body.append(t);
        rows.addEventListener('click', ev => {
            const tr = ev.target instanceof Element ? ev.target.closest('tr') : null;
            if (tr && tr.dataset.name) ctx.store.select(tr.dataset.name);
        });
        const rowViews = new Map();
        let emptyRow = null;
        let sourceAccounts = null, ordered = [];
        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'select', 'clock')) return;
                const now = serverNow(store);
                const changed = sourceAccounts !== store.accounts || anyOf(why, 'init', 'reset');
                if (changed) {
                    sourceAccounts = store.accounts;
                    ordered = sorted(ctx.filter(pick(store.accounts, ctx.accounts)), ctx.settings.sort);
                }
                if (!changed && why.has('select')) {
                    for (const [name, view] of rowViews) view.tr.classList.toggle('sel', name === store.selected);
                }
                if (!changed && !why.has('clock')) return;
                if (!changed && !fields.includes('since')) return;
                const list = ordered;
                const cells = list.map(a => fields.map((f, j) => changed || f === 'since' ? text(a, f, now) : rowViews.get(a.name)?.values[j]));
                const names = new Set(list.map(a => a.name));
                for (const [name, view] of rowViews) {
                    if (!names.has(name)) { view.tr.remove(); rowViews.delete(name); }
                }
                if (list.length && emptyRow) { emptyRow.remove(); emptyRow = null; }
                list.forEach((a, i) => {
                    let view = rowViews.get(a.name);
                    if (!view) {
                        const tr = el('tr');
                        tr.dataset.name = a.name;
                        tr.append(el('td', '', a.name));
                        view = { tr, cells: [], values: [] };
                        rowViews.set(a.name, view);
                    }
                    const tr = view.tr;
                    tr.classList.toggle('sel', a.name === store.selected);
                    fields.forEach((f, j) => {
                        if (view.values[j] === cells[i][j]) return;
                        view.values[j] = cells[i][j];
                        const numeric = f === 'reconnects' || f === 'packets' || f === 'staffLevel';
                        const cls = [numeric ? 'num' : '', HEADLINE.has(f) ? 'headline' : 'secondary'].filter(Boolean).join(' ');
                        let td = view.cells[j];
                        if (!td) { td = el('td', cls); view.cells[j] = td; tr.append(td); }
                        td.replaceChildren();
                        td.title = cells[i][j];
                        if (f === 'state') td.append(statePill(a.state));
                        else if (f === 'botState') td.append(botStatePill(a.botState));
                        else if (f === 'primary' && a.primarySkill) {
                            // Same fix as the Skills panel (skillrender.js): xp gained this
                            // session needs to stand out, not blend into the level/total-xp text
                            // and risk the column's own text-overflow ellipsis hiding it.
                            const sk = (a.skills ?? {})[a.primarySkill];
                            td.title = cells[i][j];
                            td.append(`${a.primarySkill} ${sk ? sk.level : '?'}`);
                            if (sk && sk.xpGained > 0) td.append(' ', el('span', 'gained-inline', `+${fmtCount(sk.xpGained)} xp`));
                        } else {
                            td.textContent = cells[i][j];
                            td.title = cells[i][j];
                        }
                    });
                    // Keep existing nodes and selections; move only when sorting changes.
                    const at = rows.children[i];
                    if (at !== tr) rows.insertBefore(tr, at || null);
                });
                if (list.length === 0) {
                    if (!emptyRow) {
                        emptyRow = el('tr');
                        const td = el('td', 'none');
                        td.colSpan = fields.length + 1;
                        emptyRow.append(td);
                        rows.append(emptyRow);
                    }
                    emptyRow.firstChild.textContent = store.accounts.length ? 'no reported account matches this panel' : 'no accounts reported yet';
                }
                ctx.setSub(ctx.accounts.length ? `${list.length} of ${store.accounts.length}` : String(list.length));
            }
        };
}

const BOT_PILL = { running: 'st-ingame', error: 'st-error', stopped: 'st-reconnecting' };
const NUMERIC = new Set(['reconnects', 'packets', 'staffLevel']);

// The same rows and text as the DOM table, as data for gpu-table.js: one run per styled
// span, a dot class where the DOM draws a state pill.
function gpuCell(a, f, value) {
    const style = HEADLINE.has(f) ? 'headline' : 'secondary';
    const cell = { runs: [{ text: value, style }], title: value, align: NUMERIC.has(f) ? 'right' : 'left' };
    if (f === 'state') cell.pill = `st-${a.state}`;
    else if (f === 'botState') cell.pill = BOT_PILL[a.botState] || '';
    else if (f === 'primary' && a.primarySkill) {
        const sk = (a.skills ?? {})[a.primarySkill];
        cell.runs = [{ text: `${a.primarySkill} ${sk ? sk.level : '?'}`, style }];
        if (sk && sk.xpGained > 0) cell.runs.push({ text: ' ', style }, { text: `+${fmtCount(sk.xpGained)} xp`, style: 'gained' });
    }
    return cell;
}

function createGpu(body, ctx) {
    const fields = ctx.settings.fields;
    const table = createGpuTable(body, {
        headers: ['Account', ...fields.map(f => FIELDS.find(x => x.value === f).label)],
        onSelect: name => ctx.store.select(name)
    });
    let source = null, ordered = [], rows = [];
    return {
        update(store, why) {
            if (!anyOf(why, 'init', 'status', 'select', 'clock')) return;
            const now = serverNow(store);
            const changed = source !== store.accounts || why.has('init');
            if (changed) {
                source = store.accounts;
                ordered = sorted(ctx.filter(pick(store.accounts, ctx.accounts)), ctx.settings.sort);
            }
            const list = ordered;
            if (changed || (why.has('clock') && fields.includes('since'))) rows = list.map(a => ({ key: a.name, cells: [{ runs: [{ text: a.name, style: 'td' }], title: a.name }, ...fields.map(f => gpuCell(a, f, text(a, f, now)))] }));
            else if (!why.has('select')) return;
            table.setRows(rows, store.selected, store.accounts.length ? 'no reported account matches this panel' : 'no accounts reported yet');
            const count = ctx.accounts.length ? `${list.length} of ${store.accounts.length}` : String(list.length);
            ctx.setSub(`${count} · ${table.kind === 'webgl' ? 'WebGL' : 'Canvas'}`);
        },
        destroy() { table.destroy(); }
    };
}
