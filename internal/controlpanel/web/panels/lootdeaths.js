// The Deaths section of the Loot panel (a user request: "we should have a loot panel that shows deaths and items that are lost
// and never picked up by the account that died or another account in our fleet"), shown under the "Deaths" source filter in place
// of the source cards.
//
// The figures come from GET /api/loot's `deaths` block, one per period (internal/controlpanel/lootdeaths.go). The fleet records every
// death with the pack and worn items it dropped and matches the pickups it sees to the pile (internal/deathloot): by the account that
// died, or by any other account within reach. What is left when the pile's 200 ticks are up is lost for good.
//
// Pure model first (deathsModel: checked in node without a browser), then the drawing (drawDeaths).

import { el, fmtCount } from '../util.js';
import { itemIconWrap } from '../itemicons.js';

const STATUS_TEXT = {
    recovered: 'recovered',
    partial: 'part recovered',
    open: 'still on the ground',
    lost: 'lost for good',
    nothing: 'nothing dropped'
};

/** Row ids left expanded, so a refresh does not close the row being read. */
const expanded = new Set();

const pct = (part, whole) => (whole > 0 ? `${Math.round((100 * part) / whole)}%` : '–');

/**
 * The section's figures, ready to draw, or null when the period holds no death.
 *
 * dp        the period's `deaths` block
 * ctx.now   the server's clock (ms)
 * ctx.life  whether the figures are the lifetime ones
 */
export function deathsModel(dp, ctx) {
    if (!dp || !(dp.deaths > 0)) return null;
    const { now, life = false } = ctx;
    const tile = (key, label, value, note) => ({ key, label, value, note });
    const tiles = [
        tile('deaths', 'Deaths', dp.deaths, `${(dp.byAccount ?? []).length} account${(dp.byAccount ?? []).length === 1 ? '' : 's'}`),
        tile('value', 'Value dropped (gp)', dp.value, 'what the piles were worth when they hit the ground'),
        tile('recovered', 'Recovered (gp)', dp.recovered,
            `${pct(dp.recovered, dp.value)} of it · by the dead account ${fmtCount(dp.recoveredSelf ?? 0)}, by another account ${fmtCount(dp.recoveredOthers ?? 0)}`),
        tile('lost', 'Lost for good (gp)', dp.lost, `${pct(dp.lost, dp.value)} of it · nobody picked it up before it despawned`)
    ];
    if ((dp.open ?? 0) > 0) tiles.push(tile('open', 'Still on the ground (gp)', dp.open, 'inside the 2-minute window, may yet be taken'));

    const rows = (dp.rows ?? []).map(r => ({
        id: r.id, at: r.at, account: r.account, script: r.script ?? '', place: r.place || 'unknown tile', cause: r.cause || 'unknown',
        tile: r.haveTile ? `${r.x},${r.z}${r.level ? `,${r.level}` : ''}` : '',
        status: r.status, value: r.value ?? 0, recovered: r.recovered ?? 0, lost: r.lost ?? 0, open: r.open ?? 0,
        items: (r.items ?? []).map(it => ({
            id: it.id, name: it.name || `item #${it.id}`, count: it.count, recovered: it.recovered ?? 0, unit: it.unit ?? 0,
            value: it.value ?? 0, status: it.status, by: (it.by ?? []).map(b => ({ account: b.account, count: b.count, self: !!b.self }))
        })),
        kept: (r.kept ?? []).map(k => ({ id: k.id, name: k.name, count: k.count }))
    }));

    const groups = list => (list ?? []).map(g => ({ name: g.name, deaths: g.deaths, value: g.value, recovered: g.recovered, lost: g.lost, open: g.open ?? 0 }));
    return {
        life, now, tiles, rows,
        shown: rows.length, total: dp.deaths,
        byPlace: groups(dp.byPlace), byCause: groups(dp.byCause), byAccount: groups(dp.byAccount),
        lostItems: (dp.lostItems ?? []).map(l => ({ id: l.id, name: l.name, count: l.count, value: l.value })),
        first: dp.first ?? 0, last: dp.last ?? 0
    };
}

function makeTable(headers, rows, cls) {
    const t = el('table', `tally-table loot-rc-table ${cls}`);
    const head = el('tr');
    for (const h of headers) head.append(el('th', '', h));
    const thead = el('thead');
    thead.append(head);
    const tbody = el('tbody');
    for (const row of rows) {
        const tr = el('tr');
        for (const cell of row) {
            const td = el('td');
            if (cell && typeof cell === 'object' && 'nodeType' in cell) td.append(cell);
            else { td.textContent = String(cell); td.title = String(cell); }
            tr.append(td);
        }
        tbody.append(tr);
    }
    t.append(thead, tbody);
    return t;
}

function named(id, name, text = name) {
    const wrap = el('span', 'loot-rc-named');
    if (id >= 0) wrap.append(itemIconWrap(id, name));
    wrap.append(el('span', '', text));
    return wrap;
}

function chip(status) {
    return el('span', `loot-dx-chip loot-dx-${status}`, STATUS_TEXT[status] ?? status);
}

/** Who took an item back: "alice 3, bob 1". */
function takers(by) {
    return by.map(b => `${b.account}${b.self ? ' (died)' : ''} ${fmtCount(b.count)}`).join(', ');
}

function drawRow(r, h, now) {
    const d = el('details', `loot-dx-row loot-dx-row-${r.status}`);
    d.open = expanded.has(r.id);
    d.addEventListener('toggle', () => { if (d.open) expanded.add(r.id); else expanded.delete(r.id); });
    const sum = el('summary', 'loot-dx-sum');
    sum.append(
        el('span', 'loot-dx-when', h.ago(now - r.at)),
        el('span', 'loot-dx-who', r.account),
        el('span', 'loot-dx-where', r.place),
        el('span', 'loot-dx-cause', r.cause),
        el('span', 'loot-dx-num', fmtCount(r.value)),
        el('span', 'loot-dx-num loot-dx-got', fmtCount(r.recovered)),
        el('span', `loot-dx-num ${r.open > 0 ? 'loot-dx-still' : 'loot-dx-gone'}`, fmtCount(r.open > 0 ? r.open : r.lost)),
        chip(r.status)
    );
    sum.querySelector('.loot-dx-still')?.setAttribute('title', 'still on the ground, inside the 2-minute window');
    sum.title = `${r.account}${r.script ? ` running ${r.script}` : ''} died at ${r.tile || 'an unknown tile'} (${r.place}), facing ${r.cause}`;
    d.append(sum);

    const body = el('div', 'loot-dx-items');
    if (!r.items.length) body.append(el('div', 'none', 'the account carried nothing the server drops (the three priciest items are kept)'));
    else {
        body.append(makeTable(['Item', 'Count', 'Each (gp)', 'Value (gp)', 'Recovered', 'Status', 'Picked up by'],
            r.items.map(it => [named(it.id, it.name), fmtCount(it.count), fmtCount(it.unit), fmtCount(it.value),
                fmtCount(it.recovered), chip(it.status), it.by.length ? takers(it.by) : '–']), 'loot-dx-itemtable'));
    }
    if (r.kept.length) body.append(el('div', 'loot-rc-note', `kept by the server: ${r.kept.map(k => `${fmtCount(k.count)} × ${k.name}`).join(', ')}`));
    d.append(body);
    return d;
}

/**
 * Draws the section into host (emptied first; a null model draws the empty note). h supplies loot.js's own `ago`.
 */
export function drawDeaths(host, model, h) {
    host.textContent = '';
    host.hidden = false;
    if (!model) {
        host.append(el('div', 'none loot-rc-empty', 'no death is recorded in this period yet'));
        return;
    }
    const head = el('div', 'loot-rc-head');
    head.append(el('b', '', 'Deaths'), el('span', '', model.last ? `last death ${h.ago(model.now - model.last)}` : ''));
    host.append(head);

    const tiles = el('div', 'loot-tiles loot-rc-tiles');
    for (const t of model.tiles) {
        const tile = el('div', `loot-tile loot-rc-tile loot-dx-tile-${t.key}`);
        tile.append(el('b', '', fmtCount(t.value)), el('span', '', t.label), el('em', 'loot-rc-note', t.note));
        tile.title = `${t.label}: ${fmtCount(t.value)}\n${t.note}`;
        tiles.append(tile);
    }
    host.append(tiles);

    const grid = el('div', 'loot-rc-grid');
    const block = (title, groups) => {
        const b = el('div', 'loot-rc-block');
        b.append(el('div', 'loot-rc-title', title));
        if (!groups.length) { b.append(el('div', 'none', 'none')); return b; }
        b.append(makeTable([title.replace('By ', '').replace(/^./, c => c.toUpperCase()), 'Deaths', 'Dropped (gp)', 'Recovered', 'Lost for good', 'Open'],
            groups.map(g => [g.name, fmtCount(g.deaths), fmtCount(g.value), fmtCount(g.recovered), fmtCount(g.lost), g.open ? fmtCount(g.open) : '–']), 'loot-dx-groups'));
        return b;
    };
    grid.append(block('By place', model.byPlace), block('By cause', model.byCause));
    host.append(grid);

    const lost = el('div', 'loot-rc-block');
    lost.append(el('div', 'loot-rc-title', 'Lost for good, by item'));
    if (model.lostItems.length) {
        lost.append(makeTable(['Item', 'Count', 'Value (gp)'], model.lostItems.map(l => [named(l.id, l.name), fmtCount(l.count), fmtCount(l.value)]), 'loot-dx-lostitems'));
    } else lost.append(el('div', 'none', 'nothing is lost for good yet'));
    const accts = block('By account', model.byAccount);
    const pair = el('div', 'loot-rc-grid');
    pair.append(lost, accts);
    host.append(pair);

    const list = el('div', 'loot-rc-block loot-dx-list');
    list.append(el('div', 'loot-rc-title', `Deaths, newest first${model.shown < model.total ? ` (the latest ${model.shown} of ${fmtCount(model.total)})` : ''}`));
    const header = el('div', 'loot-dx-sum loot-dx-header');
    for (const t of ['When', 'Account', 'Where', 'Cause', 'Dropped', 'Recovered', 'Lost / open', 'Status']) header.append(el('span', /^(Dropped|Recovered|Lost \/ open)$/.test(t) ? 'loot-dx-num' : '', t));
    list.append(header);
    for (const r of model.rows) list.append(drawRow(r, h, model.now));
    host.append(list);

    host.append(el('div', 'loot-rc-note loot-dx-foot',
        'Items are the pack and worn items the death dropped, less the three priciest the server keeps (the skull and Protect Item are not read). A pickup is a gain of the same item within 2 tiles of the death tile inside the pile’s 2 minutes, by any fleet account (other accounts only after it is revealed to them, 1 minute in): inferred from the pack, not read off the ground.'));
}
