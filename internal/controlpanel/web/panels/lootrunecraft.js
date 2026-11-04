// The Runecrafting section of the Loot panel (a user request: "runecrafting loot panel is needed"), shown under the
// "Runecrafting" source filter above the Runecraft and Essence supply cards.
//
// The figures come from GET /api/loot's `runecraft` block (internal/controlpanel/lootrunecraft.go), one per period, and
// from `essenceStock` (the mule's cached bank). Every craft and every hand-over is an ordinary loot Drop that
// runecraftrunner.Craft and the EssenceMiner supply loop report, so the block survives a restart like the rest of the panel and
// the generic cards (the Runecraft card with a row per altar, the Essence supply card) show the same records.
//
// WHAT IT ADDS TO THE CARDS: the runes by type with their market value and rate, the essence bound and its rate, the experience
// and the level, each account's share, and the supply side: the essence mined, the essence handed to the mule, and what the
// mule holds. The per-hour rates are not the server's: whether a window is long enough to give one, and whether the lifetime
// figure is an average over a changing fleet, is the rule loot.js already has for every card (hourlyRate), so it is handed in.
//
// Pure model first (runecraftModel: checked in node without a browser), then the drawing (drawRunecraft).

import { el, fmtCount } from '../util.js';
import { itemIconWrap } from '../itemicons.js';

export const RUNECRAFT_SKILL = 'Runecraft';
export const SUPPLY_KIND = 'supply';

/** Whether a source belongs to the Runecrafting filter: a row of the Runecraft card, or the Essence supply card. */
export function isRunecraftSource(src) {
    if (!src) return false;
    return src.kind === SUPPLY_KIND || (src.kind === 'production' && src.skill === RUNECRAFT_SKILL);
}

/** A craft the recent-drops list holds: a production source of the Runecraft card (its Items are the runes). */
const isCraft = src => !!src && src.kind === 'production' && src.skill === RUNECRAFT_SKILL;

/**
 * The section's figures, ready to draw, or null when the period holds nothing of runecrafting.
 *
 * rc      the period's `runecraft` block
 * ctx.now      the server's clock (ms)
 * ctx.rate     (count, since) => per hour, or null when the window is too short to give one (loot.js's perHour)
 * ctx.life     whether the figures are the lifetime ones (an average, not a rate)
 * ctx.recent   the body's recent-drops list
 * ctx.stock    the body's essenceStock
 */
export function runecraftModel(rc, ctx) {
    if (!rc) return null;
    const { now, rate, life = false, recent = [], stock = [] } = ctx;
    const per = (count, since) => (since > 0 ? rate(count, since) : null);
    const s = rc.supply ?? {};
    const crafts = rc.crafts ?? 0;
    const tile = (key, label, value, rateOf, note = '') => ({ key, label, value, rate: rateOf, note });
    const tiles = [
        tile('crafts', 'Crafts', crafts, per(crafts, rc.first), `${rc.accounts ?? 0} account${rc.accounts === 1 ? '' : 's'}`),
        tile('runes', 'Runes crafted', rc.runes ?? 0, per(rc.runes ?? 0, rc.first), `${(rc.byRune ?? []).length} kind${(rc.byRune ?? []).length === 1 ? '' : 's'}`),
        tile('value', 'Rune value (net gp)', rc.net ?? 0, per(rc.net ?? 0, rc.first),
            rc.costKnown ? `${fmtCount(rc.gross ?? 0)} gp of runes less ${fmtCount(rc.cost ?? 0)} gp of essence`
                : `GROSS: ${fmtCount(rc.gross ?? 0)} gp of runes, the essence has no trustworthy price`),
        tile('essence', 'Essence consumed', rc.essence ?? 0, per(rc.essence ?? 0, rc.first),
            crafts > 0 ? `${(rc.essence / crafts).toFixed(1)} a craft` : ''),
        tile('xp', 'Runecraft XP', rc.xp ?? 0, per(rc.xp ?? 0, rc.first), crafts > 0 ? `${fmtCount(Math.round(rc.xp / crafts))} a craft` : ''),
        { key: 'level', label: 'Runecraft level', value: rc.level > 0 ? rc.level : null, rate: null, note: rc.level > 0 ? 'highest read after a craft' : 'not read yet' }
    ];

    const runes = (rc.byRune ?? []).map(r => ({
        id: r.id, name: r.name || `item #${r.id}`, count: r.count, unit: r.unit, value: r.value,
        perHour: per(r.count, rc.first), gpPerHour: per(r.value, rc.first)
    }));
    const altars = (rc.altars ?? []).map(a => ({
        name: a.name, runeId: a.runeId, crafts: a.crafts, essence: a.essence, runes: a.runes, xp: a.xp, level: a.level ?? 0,
        accounts: a.accounts, perHour: per(a.crafts, a.first)
    }));

    const stockTotal = stock.reduce((n, m) => n + (m.essence ?? 0), 0);
    const supply = {
        mined: s.mined ?? 0, minedRate: per(s.mined ?? 0, s.minedFirst), minedAccounts: s.minedAccounts ?? 0,
        handed: s.handed ?? 0, handedRate: per(s.handed ?? 0, s.handedFirst), handedAccounts: s.handedAccounts ?? 0,
        handedLast: s.handedLast ?? 0,
        used: rc.essence ?? 0, usedRate: per(rc.essence ?? 0, rc.first),
        stock: stock.length ? stockTotal : null,
        stockNote: stock.length ? stock.map(m => `${m.account} read ${m.updated > 0 ? ageText(now - m.updated) : 'at an unknown time'}`).join(', ') : 'the mule’s bank has not been read'
    };

    const accounts = (rc.perAccount ?? []).map(a => ({
        account: a.account, crafts: a.crafts ?? 0, essence: a.essence ?? 0, runes: a.runes ?? 0, value: a.value ?? 0, cost: a.cost ?? 0,
        xp: a.xp ?? 0, level: a.level ?? 0, mined: a.mined ?? 0, handed: a.handed ?? 0, last: a.last ?? 0
    }));

    const crafted = recent.filter(r => isCraft(r.source) && (r.items ?? []).length).slice(0, 8).map(r => ({
        at: r.at, account: r.account, altar: r.source.name, items: r.items.map(it => ({ id: it.id, name: it.name, count: it.count }))
    }));

    return { life, now, tiles, runes, altars, supply, accounts, recent: crafted, first: rc.first ?? 0, last: rc.last ?? 0 };
}

/** "3m 5s ago"-style age for a stock reading, "just now" under a second and a half. */
export function ageText(ms) {
    if (!(ms >= 0)) return 'at an unknown time';
    if (ms < 1500) return 'just now';
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s ago`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    return h < 48 ? `${h}h ${m % 60}m ago` : `${Math.floor(h / 24)}d ago`;
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

/** An item icon beside a name, for a table's first cell. */
function named(id, name, text = name) {
    const wrap = el('span', 'loot-rc-named');
    if (id >= 0) wrap.append(itemIconWrap(id, name));
    wrap.append(el('span', '', text));
    return wrap;
}

/**
 * Draws the section into host (emptied first; a null model draws the empty note). h supplies loot.js's own formatters: fmtRate (a per-hour figure) and ago (a
 * duration as "3m 5s ago"), so the section reads the way the cards do.
 */
export function drawRunecraft(host, model, h) {
    host.textContent = '';
    host.hidden = false;
    if (!model) {
        host.append(el('div', 'none loot-rc-empty', 'nothing of runecrafting is recorded in this period yet: no craft, no essence mined, no hand-over to the mule'));
        return;
    }
    const life = model.life;
    const unit = life ? ' avg' : '';
    const perHour = (value, suffix) => (value === null || value === undefined ? 'no rate yet' : `${h.fmtRate(value)} ${suffix}/h${unit}`);

    const head = el('div', 'loot-rc-head');
    head.append(el('b', '', 'Runecrafting'), el('span', '', model.last ? `last craft ${h.ago(model.now - model.last)}` : 'nothing crafted yet'));
    host.append(head);

    const tiles = el('div', 'loot-tiles loot-rc-tiles');
    const unitOf = { crafts: 'crafts', runes: 'runes', value: 'gp', essence: 'essence', xp: 'xp' };
    for (const t of model.tiles) {
        const tile = el('div', `loot-tile loot-rc-tile loot-rc-${t.key}`);
        tile.append(el('b', '', t.value === null ? '–' : fmtCount(t.value)), el('span', '', t.label));
        const sub = [];
        if (unitOf[t.key]) sub.push(perHour(t.rate, unitOf[t.key]));
        if (t.note) sub.push(t.note);
        const note = el('em', 'loot-rc-note', sub.join(' · '));
        tile.title = `${t.label}: ${t.value === null ? 'unknown' : fmtCount(t.value)}${sub.length ? `\n${sub.join('\n')}` : ''}`;
        tile.append(note);
        tiles.append(tile);
    }
    host.append(tiles);

    const grid = el('div', 'loot-rc-grid');

    const runesBlock = el('div', 'loot-rc-block');
    runesBlock.append(el('div', 'loot-rc-title', 'Runes crafted, by type'));
    if (model.runes.length) {
        runesBlock.append(makeTable(['Rune', 'Crafted', 'Each (gp)', 'Value (gp)', `Per hour${unit}`, `Gp per hour${unit}`],
            model.runes.map(r => [named(r.id, r.name), fmtCount(r.count), fmtCount(r.unit), fmtCount(r.value),
                r.perHour === null ? '–' : h.fmtRate(r.perHour), r.gpPerHour === null ? '–' : h.fmtRate(r.gpPerHour)]), 'loot-rc-runes'));
    } else runesBlock.append(el('div', 'none', 'no rune has been crafted in this period yet'));
    grid.append(runesBlock);

    const altarBlock = el('div', 'loot-rc-block');
    altarBlock.append(el('div', 'loot-rc-title', 'By altar'));
    if (model.altars.length) {
        altarBlock.append(makeTable(['Altar', 'Crafts', 'Essence', 'Runes', 'XP', 'Level', `Crafts/h${unit}`],
            model.altars.map(a => [named(a.runeId, a.name), fmtCount(a.crafts), fmtCount(a.essence), fmtCount(a.runes), fmtCount(a.xp),
                a.level > 0 ? String(a.level) : '–', a.perHour === null ? '–' : h.fmtRate(a.perHour)]), 'loot-rc-altars'));
    } else altarBlock.append(el('div', 'none', 'no craft in this period yet'));
    grid.append(altarBlock);
    host.append(grid);

    // ---- the supply side ----
    const sup = model.supply;
    const supplyBlock = el('div', 'loot-rc-supply');
    supplyBlock.append(el('div', 'loot-rc-title', 'Essence supply'));
    const sTiles = el('div', 'loot-tiles loot-rc-tiles');
    const sTile = (key, label, value, sub) => {
        const tile = el('div', `loot-tile loot-rc-tile loot-rc-${key}`);
        tile.append(el('b', '', value === null ? '–' : fmtCount(value)), el('span', '', label), el('em', 'loot-rc-note', sub));
        tile.title = `${label}: ${value === null ? 'unknown' : fmtCount(value)}\n${sub}`;
        sTiles.append(tile);
    };
    sTile('mined', 'Essence mined', sup.mined, `${perHour(sup.minedRate, 'essence')} · ${sup.minedAccounts} miner${sup.minedAccounts === 1 ? '' : 's'}`);
    sTile('handed', 'Handed to the mule', sup.handed,
        `${perHour(sup.handedRate, 'essence')} · ${sup.handedAccounts} account${sup.handedAccounts === 1 ? '' : 's'}${sup.handedLast ? ` · last ${h.ago(model.now - sup.handedLast)}` : ''}`);
    sTile('stock', 'Mule’s essence', sup.stock, sup.stockNote);
    sTile('used', 'Bound at the altars', sup.used, perHour(sup.usedRate, 'essence'));
    supplyBlock.append(sTiles);
    host.append(supplyBlock);

    // ---- per account ----
    const acctBlock = el('div', 'loot-rc-block loot-rc-accounts');
    acctBlock.append(el('div', 'loot-rc-title', 'Per account'));
    if (model.accounts.length) {
        acctBlock.append(makeTable(['Account', 'Crafts', 'Essence', 'Runes', 'Value (gp)', 'XP', 'Level', 'Mined', 'Handed over', 'Last'],
            model.accounts.map(a => [a.account, fmtCount(a.crafts), fmtCount(a.essence), fmtCount(a.runes), fmtCount(a.value),
                fmtCount(a.xp), a.level > 0 ? String(a.level) : '–', fmtCount(a.mined), fmtCount(a.handed), a.last ? h.ago(model.now - a.last) : '–']), 'loot-rc-accounts-table'));
    } else acctBlock.append(el('div', 'none', 'no account has done any of it in this period yet'));
    host.append(acctBlock);

    // ---- recent crafts ----
    const recent = el('div', 'loot-ticker loot-rc-recent');
    recent.append(el('span', 'loot-ticker-label', 'Recent crafts'));
    if (!model.recent.length) recent.append(el('span', 'none', 'none yet'));
    for (const r of model.recent) {
        const line = el('span', 'loot-tick');
        line.append(el('span', 'loot-tick-t', h.ago(model.now - r.at)), el('span', 'who', r.account), el('span', '', r.altar));
        for (const it of r.items) {
            const chip = el('span', 'loot-tick-item');
            chip.append(itemIconWrap(it.id, it.name), el('span', '', `${fmtCount(it.count)} × ${it.name}`));
            line.append(chip);
        }
        recent.append(line);
    }
    host.append(recent);
}
