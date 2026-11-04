// Every picked account's bank in one panel — the account owner's own words: "make sure
// the combined bank panel is working so i can see inside the 3 banks currently
// connected". A sibling to bank.js (which follows one account, table-selection style);
// this one shows several banks side by side (or merged into one sorted list), with a
// pick of how to organise what it shows — the account owner's second explicit ask,
// "make sure we can organise that panel to view the items in different ways".
//
// One batch request refreshes the picked accounts, rather than one request per
// account. Reflects fleet's own bank-persistence cache (see internal/fleet/
// bankstore.go): an account that has since disconnected or stopped still shows its
// last-known contents here, same as the single-account panel.
//
// 2026-09-20, the owner's three follow-ups:
//
//  1. WHAT IT IS WORTH. Every stack is valued through the same PriceBook the Loot panel
//     uses — the market's own median traded price, then high alchemy (60% of the cache's
//     unit cost), then that cost. The panel never hides which of the three answered: the
//     summary splits the total by source and every item says so, because a high-alch
//     estimate presented as a market quote is a lie the operator cannot see through. An
//     item with no market price and no cache cost is UNKNOWN, never zero.
//  2. WEBGL, WITH THE DOM KEPT. "Draw items with" picks a DOM grid (default, unchanged,
//     selectable and findable with Ctrl+F) or a virtualised GPU table. Same rule the
//     Accounts panel follows, and gpu-text.js refuses software WebGL, so a machine
//     without a GPU takes Canvas 2D and then the DOM default is still there.
//  3. BANKED EXPERIENCE. What the banked goods are worth in XP if they were processed.
//     Every route comes from internal/bankxp, whose table is generated from this server's
//     own content, and the panel is built around the two ways that figure can mislead:
//     an item with several routes (logs can be fletched three ways or burnt) and a route
//     the account has no level for. Both are settings, both are stated on screen.

import { anyOf, el, fmtCount, fmtDuration, serverNow } from '../util.js';
import { iconFor, itemIconWrap } from '../itemicons.js';
import { createGpuTable } from '../gpu-table.js';
import { createLootRenderer } from '../loot-gl.js';
import { cachedMesh, loadMeshes, meshWanted, modelsUsable, textureAverages } from '../loot-models.js';

const REFRESH_EVERY_MS = 5000;
// Match the batch endpoint's bounded roster size.
const MAX_ACCOUNTS = 2048;
let tooltipSequence = 0;

const SORTS = [
    { value: 'value', label: 'Value, highest first' },
    { value: 'xp', label: 'Banked XP, highest first' },
    { value: 'name', label: 'Name' },
    { value: 'category', label: 'Category' },
    { value: 'count', label: 'Quantity, highest first' }
];
const GROUPS = [
    { value: 'account', label: 'By account' },
    { value: 'none', label: 'Merged totals (hover for each account)' }
];
const RENDERERS = [
    { value: 'dom', label: 'DOM grid with icons (selectable text)' },
    { value: 'models', label: 'GPU / WebGL grid with real rotating ground models' },
    { value: 'gpu', label: 'GPU / WebGL table (dense, no models)' }
];

// How fast a bank item turns, in radians per second. Slow enough to read a shape, fast enough
// that a whole face comes round inside a few seconds: a ground model lies flat and at a fixed
// angle, so a still one is often unreadable, which is why users asked for it to rotate
// "so its fully visible".
const SPIN_RATE = 0.9;
// The model grid redraws at this cadence rather than every animation frame. A turntable does
// not need 60 fps to read, and the panel shares the page's main thread with the Accounts
// table and two map panels.
const MODEL_FPS = 30;
// How many models to ask the cache decoder for in one go. The adapter answers serially and a
// merged fleet bank is only a few dozen distinct items, so this is a politeness bound rather
// than a real limit.
const MODEL_BATCH = 24;
// The most stages drawn in one frame. Culling normally leaves far fewer on screen than this;
// the cap is there so a very tall panel full of small cards cannot quietly turn into a
// thousand draw calls a frame.
const MODEL_STAGE_CAP = 160;
const XP_ROUTES = [
    { value: 'fleet', label: 'Only routes a script here actually runs' },
    { value: 'best', label: 'Best XP route available (named per item)' }
];
const XP_LEVELS = [
    { value: 'ignore', label: 'Ignore levels (what it is worth to anyone)' },
    { value: 'current', label: "At the fleet's current levels (and burn rates)" }
];

// What each value source is called on screen, and the one-line explanation behind it. Named
// here rather than inline so the wording cannot drift between the summary and the tooltips.
const SOURCES = {
    market: { label: 'market', why: "median price of this item's completed trades on example.invalid" },
    listing: { label: 'listing', why: "a market's own last-sold or last-bought figure, not a completed trade" },
    alch: { label: 'high alch', why: "estimate: high alchemy, 60% of the cache's unit cost — not a traded price" },
    cost: { label: 'cache cost', why: 'the shop price the content ships with — the weakest source' },
    none: { label: 'unknown', why: 'no market price and no cache cost: counted as unknown, not as zero' }
};
// The ORDER the summary and the source bar walk, strongest evidence first. Kept separate from
// SOURCES so adding a source to marketprice.go's ValueSource shows up in one place here.
const SOURCE_ORDER = ['market', 'listing', 'alch', 'cost'];

// A source the server names that this panel has never heard of must not break the page, and
// must not be quietly relabelled either: it is shown under its own name with no explanation,
// which is the honest way to say "newer server than this panel".
function sourceOf(name) {
    return SOURCES[name] || { label: name || 'unknown', why: 'this panel does not know this price source' };
}

function fmtCoins(n) {
    if (!Number.isFinite(n)) return '?';
    const abs = Math.abs(n);
    if (abs >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
    if (abs >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
    if (abs >= 1e4) return `${Math.round(n / 1e3)}K`;
    return fmtCount(n);
}

function fmtXP(n) {
    if (!Number.isFinite(n) || n <= 0) return '0';
    if (n >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
    if (n >= 1e4) return `${Math.round(n / 1e3)}K`;
    return fmtCount(n);
}

export default {
    type: 'bankall',
    title: 'Combined bank',
    description: "Every picked account's bank in one panel, grouped or merged, priced at what it really trades for, with the experience still sitting in it unprocessed.",
    size: { w: 7, h: 5 },
    accounts: 'many',
    settings: [
        { key: 'groupBy', label: 'Group by', kind: 'select', options: GROUPS, default: 'none' },
        { key: 'sortBy', label: 'Sort items by', kind: 'select', options: SORTS, default: 'value' },
        { key: 'renderer', label: 'Draw items with', kind: 'select', options: RENDERERS, default: 'dom' },
        { key: 'showValue', label: 'Show what it is worth', kind: 'bool', default: true },
        { key: 'showXP', label: 'Show banked experience', kind: 'bool', default: true },
        { key: 'xpRoute', label: 'Count experience from', kind: 'select', options: XP_ROUTES, default: 'fleet' },
        { key: 'xpLevels', label: 'Experience levels', kind: 'select', options: XP_LEVELS, default: 'ignore' },
        { key: 'xpRate', label: "Server XP rate (Engine's NODE_XPRATE)", kind: 'number', min: 1, max: 100, step: 1, default: 1 }
    ],
    create(body, ctx) {
        const note = el('div', 'none');
        const summary = el('div', 'bankall-summary');
        const wrap = el('div', 'bankall-wrap');
        const filter = el('input', 'bankall-filter');
        filter.type = 'search';
        filter.placeholder = 'Find an item by name or ID';
        filter.setAttribute('aria-label', 'Filter combined bank items');
        body.append(filter, note, summary, wrap);

        let names = [];
        let banks = new Map(); // account -> { items, updatedAt }
        let prices = new Map(); // item id -> price entry
        let priceInfo = null;
        let xp = null;          // the server's banked-experience answer
        let routes = new Map(); // item id -> [route]
        let routeInfo = null;
        let bankSignature = '';
        let bankAges = [];
        let summaryAt = 0;
        let loading = false;
        let currentNow = 0;
        let lastFetch = 0;
        let lastXPQuery = '';
        let closed = false;
        let gpu = null;         // the GPU table, built lazily and only when picked
        let models = null;      // the shared-canvas ground-model renderer, likewise
        let modelSlots = [];    // {id, el, phase} per drawn item, the rects the models fill
        let modelFrame = 0;     // pending rAF handle
        let modelLast = 0;      // last frame's timestamp, for the MODEL_FPS throttle
        let modelLoading = false;
        let modelNote = '';     // why models are not on screen, shown in the subtitle
        let modelsOn = false;   // whether the grid being built is feeding the model canvas
        let modelDrawn = -1, modelStaged = -1; // last reported counts, for the subtitle
        let modelMeasured = false; // whether modelSlots carry current offsets (see measureSlots)
        let meta = null;        // /api/meta, which is what says whether models are possible at all
        const tooltip = el('div', 'bankall-tooltip');
        tooltip.id = `bankall-tooltip-${++tooltipSequence}`;
        tooltip.setAttribute('role', 'tooltip');
        tooltip.tabIndex = 0;
        tooltip.hidden = true;
        document.body.append(tooltip);
        let tooltipOwner = null;
        let tooltipHideTimer = 0;
        const details = new WeakMap();
        function hideTooltip() {
            clearTimeout(tooltipHideTimer);
            if (tooltipOwner) tooltipOwner.removeAttribute('aria-describedby');
            tooltipOwner = null;
            tooltip.hidden = true;
        }
        function showTooltip(chip) {
            const text = details.get(chip);
            if (!text) return;
            hideTooltip();
            tooltipOwner = chip;
            tooltip.textContent = text;
            chip.setAttribute('aria-describedby', tooltip.id);
            tooltip.hidden = false;
            tooltip.style.left = '8px'; tooltip.style.top = '8px';
            const rect = chip.getBoundingClientRect();
            const left = Math.max(8, Math.min(rect.left, window.innerWidth - tooltip.offsetWidth - 8));
            const below = rect.bottom + 4;
            const top = below + tooltip.offsetHeight <= window.innerHeight - 8 ? below : Math.max(8, rect.top - tooltip.offsetHeight - 4);
            tooltip.style.left = `${left}px`; tooltip.style.top = `${top}px`;
        }
        function decorate(chip) {
            details.set(chip, chip.title);
            chip.removeAttribute('title');
            return chip;
        }
        wrap.addEventListener('pointerover', event => {
            const chip = event.target.closest('.bank-slot');
            if (chip && chip !== tooltipOwner) showTooltip(chip);
        });
        wrap.addEventListener('focusin', event => {
            const chip = event.target.closest('.bank-slot');
            if (chip) showTooltip(chip);
        });
        wrap.addEventListener('pointerout', event => {
            if (!wrap.contains(event.relatedTarget) && !tooltip.contains(event.relatedTarget)) {
                clearTimeout(tooltipHideTimer);
                tooltipHideTimer = setTimeout(hideTooltip, 180);
            }
        });
        wrap.addEventListener('focusout', event => {
            if (!tooltip.contains(event.relatedTarget)) hideTooltip();
        });
        tooltip.addEventListener('pointerenter', () => clearTimeout(tooltipHideTimer));
        tooltip.addEventListener('pointerleave', hideTooltip);
        tooltip.addEventListener('focusout', hideTooltip);
        const dismiss = event => { if (event.key === 'Escape') hideTooltip(); };
        window.addEventListener('keydown', dismiss);
        window.addEventListener('resize', hideTooltip);
        // A panel resize reflows the grid, so every cached slot offset is stale.
        const remeasure = () => { modelMeasured = false; scheduleModels(); };
        window.addEventListener('resize', remeasure);
        let drawnKey = '';
        filter.addEventListener('input', () => { drawnKey = ''; draw(currentNow); });
        // Scrolling moves every stage, so the canvas has to be repainted; the throttle in
        // modelTick keeps that bounded, and the loop stops on its own once nothing is drawn.
        wrap.addEventListener('scroll', scheduleModels, { passive: true });
        function matches(item) {
            const query = filter.value.trim().toLowerCase();
            return !query || String(item.id) === query || (item.name || '').toLowerCase().includes(query);
        }

        // ---- valuing and experience ----

        /** The price record for one item, or a stated-unknown stand-in. */
        function priceOf(item) {
            return prices.get(item.id) || { id: item.id, name: item.name, unit: 0, source: 'none' };
        }
        /** What a whole stack is worth; null when the item has no known value at all. */
        function stackValue(item) {
            const p = priceOf(item);
            return p.unit > 0 ? p.unit * item.count : null;
        }
        /** The chosen route line for one item, from the server's own combined plan. */
        function lineFor(id) {
            return xp && xp.combined && xp.combined.lines ? xp.combined.lines.find(l => l.id === id) : null;
        }

        /**
         * One bank's value, split by where each price came from. The split is the point: a
         * total that silently mixes traded prices with alchemy estimates cannot be judged.
         */
        function valueOf(items) {
            const by = {};
            let total = 0, unknownKinds = 0, unknownItems = 0;
            for (const it of items) {
                const p = priceOf(it);
                if (p.unit > 0) {
                    const v = p.unit * it.count;
                    total += v;
                    by[p.source] = (by[p.source] || 0) + v;
                } else {
                    unknownKinds++;
                    unknownItems += it.count;
                }
            }
            return { total, by, unknownKinds, unknownItems };
        }

        function xpForAccount(name) {
            if (!xp || !xp.accounts) return null;
            return xp.accounts.find(a => a.account === name) || null;
        }

        // ---- loading ----

        function xpQuery() {
            const rate = Number(ctx.settings.xpRate) || 1;
            return `xp=${encodeURIComponent(ctx.settings.xpRoute)}&levels=${encodeURIComponent(ctx.settings.xpLevels)}&rate=${rate}`;
        }

        async function loadRoutes() {
            // The processing table is static for the life of the server binary, so it is
            // fetched once per panel rather than riding along with every five-second refresh.
            if (routeInfo) return;
            try {
                const res = await fetch('api/item-routes', { cache: 'no-store' });
                if (!res.ok) return;
                const data = await res.json();
                if (closed) return;
                routeInfo = data;
                routes = new Map();
                for (const r of data.routes ?? []) {
                    const id = r.inputs && r.inputs.length ? r.inputs[0].id : -1;
                    if (id < 0) continue;
                    if (!routes.has(id)) routes.set(id, []);
                    routes.get(id).push(r);
                }
                drawnKey = '';
                draw(currentNow);
            } catch { /* the panel works without it; the XP block just stays quiet */ }
        }

        async function loadAll() {
            if (loading || closed || ctx.isVisible?.() === false) return;
            loading = true;
            lastFetch = Date.now();
            try {
                const res = await fetch(`api/bank-all?${xpQuery()}`, { cache: 'no-store' });
                if (!res.ok) throw new Error(`Bank refresh failed (${res.status})`);
                const data = await res.json();
                if (closed) return;
                const picked = new Set(names);
                banks = new Map();
                for (const bank of data.banks ?? []) {
                    if (picked.has(bank.account)) banks.set(bank.account, { items: bank.items ?? [], updatedAt: bank.updatedAt ?? 0 });
                }
                prices = new Map();
                for (const p of data.prices ?? []) prices.set(p.id, p);
                priceInfo = data.priceBook ?? null;
                xp = data.xp ?? null;
                // Include price and XP changes too; an unchanged refresh keeps existing
                // item nodes, textures, focus and model slots instead of rebuilding them.
                bankSignature = JSON.stringify([[...banks.entries()], [...prices.entries()], priceInfo, xp]);
                note.textContent = '';
                draw(currentNow);
            } catch {
                if (!closed) note.textContent = 'Bank refresh failed; showing last known contents. Retrying shortly.';
            } finally {
                loading = false;
            }
        }

        // ---- the summary strip ----

        function tile(cls, label, big, sub) {
            const t = el('div', `bankall-tile ${cls}`);
            t.append(el('div', 'bankall-tile-label', label), el('div', 'bankall-tile-big', big));
            if (sub) t.append(el('div', 'bankall-tile-sub', sub));
            return t;
        }

        function drawSummary(items) {
            summaryAt = Date.now();
            summary.textContent = '';
            if (!items.length) return;

            if (ctx.settings.showValue) {
                const v = valueOf(items);
                const parts = [];
                for (const key of SOURCE_ORDER) {
                    if (v.by[key] > 0) parts.push(`${fmtCoins(v.by[key])} ${sourceOf(key).label}`);
                }
                if (v.unknownKinds) parts.push(`${v.unknownKinds} kind${v.unknownKinds === 1 ? '' : 's'} unpriced`);
                const t = tile('val', 'Worth', `${fmtCoins(v.total)} gp`, parts.join(' · ') || 'nothing priced yet');
                t.title = [
                    `${fmtCount(v.total)} coins across ${items.length} stacks.`,
                    ...SOURCE_ORDER.filter(k => v.by[k] > 0).map(k => `${fmtCount(v.by[k])} from ${sourceOf(k).label}: ${sourceOf(k).why}`),
                    v.unknownItems ? `${fmtCount(v.unknownItems)} items have no known value and are counted as nothing.` : ''
                ].filter(Boolean).join('\n');
                summary.append(t);
                // The source split as a bar, so a total that is mostly estimate LOOKS mostly
                // estimate without having to read the numbers.
                if (v.total > 0) {
                    const bar = el('div', 'bankall-srcbar');
                    for (const key of SOURCE_ORDER) {
                        if (!v.by[key]) continue;
                        const seg = el('div', `bankall-src-${key}`);
                        seg.style.width = `${(100 * v.by[key]) / v.total}%`;
                        seg.title = `${fmtCoins(v.by[key])} gp — ${sourceOf(key).why}`;
                        bar.append(seg);
                    }
                    summary.append(bar);
                }
            }

            if (ctx.settings.showXP && xp && xp.combined) {
                const c = xp.combined;
                const top = (c.skills ?? []).slice(0, 4).map(s => `${s.skill} ${fmtXP(s.xp)}`).join(' · ');
                const t = tile('xp', 'Banked XP', fmtXP(c.xp), top || 'no route for anything banked');
                const how = ctx.settings.xpRoute === 'fleet'
                    ? 'Only routes a registered script here actually runs are counted.'
                    : 'The best-paying route available is counted for each item, and named on the item.';
                const gate = xp.levels === 'current'
                    ? `Gated at the fleet's best level per skill${xp.combinedLevels ? ` (${Object.entries(xp.combinedLevels).filter(([, l]) => l > 0).map(([s, l]) => `${s} ${l}`).join(', ')})` : ''}, and scaled by content's own burn/fail chance at that level.`
                    : 'Levels ignored: this is what the goods are worth to somebody who can process them, not what any one account can collect today.';
                t.title = [
                    `${fmtCount(c.xp)} experience, at server XP rate ${xp.rate}.`, how, gate,
                    c.unrouted ? `${fmtCount(c.unrouted)} banked items (${c.unroutedKinds} kinds) have no route at all and are counted as nothing.` : '',
                    c.levelBlocked ? `${fmtCount(c.levelBlocked)} banked items have a route no account has the level for.` : '',
                    routeInfo && routeInfo.mismatches ? `${routeInfo.mismatches} routes were refused: this server's item cache disagrees with the content the table was built from.` : ''
                ].filter(Boolean).join('\n');
                summary.append(t);
            }

            const from = [];
            if (priceInfo) {
                if (!priceInfo.enabled) from.push('prices: market lookup off, high alch then cache cost');
                else if (!priceInfo.items) from.push(priceInfo.error ? `prices: market unreachable (${priceInfo.error})` : 'prices: market table still loading');
                else from.push(`prices: ${priceInfo.items} items from the market${priceInfo.fetchedAt ? `, ${fmtDuration(currentNow - priceInfo.fetchedAt)} old` : ''}`);
                if (!priceInfo.cache) from.push('no item cache: no high-alch fallback here');
            }
            if (xp) from.push(`xp: ${xp.routes} routes from this server's content${xp.rate !== 1 ? `, rate ${xp.rate}x` : ''}`);
            if (from.length) summary.append(el('div', 'bankall-from', from.join(' · ')));
        }

        // ---- drawing ----

        function mergedItems(withData) {
            const totals = new Map();
            for (const name of withData) {
                for (const it of banks.get(name).items) {
                    let total = totals.get(it.id);
                    if (!total) { total = { ...it, count: 0, account: 'Combined', banks: [] }; totals.set(it.id, total); }
                    total.count += it.count;
                    total.banks.push(`${name}: ${fmtCount(it.count)}`);
                }
            }
            return [...totals.values()];
        }

        function sortItems(items, by) {
            const out = [...items];
            if (by === 'category') out.sort((a, b) => iconFor(a.name).cls.localeCompare(iconFor(b.name).cls) || (a.name || '').localeCompare(b.name || ''));
            else if (by === 'count') out.sort((a, b) => b.count - a.count);
            else if (by === 'value') out.sort((a, b) => (stackValue(b) ?? -1) - (stackValue(a) ?? -1));
            else if (by === 'xp') out.sort((a, b) => ((lineFor(b.id) || {}).xp ?? -1) - ((lineFor(a.id) || {}).xp ?? -1));
            else out.sort((a, b) => (a.name || `#${a.id}`).localeCompare(b.name || `#${b.id}`));
            return out;
        }

        /** The multi-line tooltip one item gets, wherever it is drawn. */
        function itemDetail(item, extraLines) {
            const p = priceOf(item);
            const lines = [`${item.name || `item #${item.id}`}: ${fmtCount(item.count)}${item.account && item.account !== 'Combined' ? ` (${item.account})` : ''} (id ${item.id})`];
            if (p.unit > 0) {
                lines.push(`${fmtCount(p.unit)} gp each = ${fmtCount(p.unit * item.count)} gp — ${sourceOf(p.source).why}`);
                if (p.source === 'market' && p.trades) lines.push(`sample: ${p.trades} trade${p.trades === 1 ? '' : 's'}, ${fmtCount(p.units)} units`);
                if (p.source === 'market' && p.cost) lines.push(`the cache's own cost is ${fmtCount(p.cost)} gp`);
            } else {
                lines.push(sourceOf('none').why);
            }
            const line = lineFor(item.id);
            const alts = routes.get(item.id) || [];
            if (line && line.xp > 0) {
                lines.push(`${fmtCount(line.xp)} ${line.skill} XP — ${line.via}${line.chance && line.chance < 1 ? ` (${Math.round(line.chance * 100)}% succeed at this level)` : ''}`);
                if (line.limitedBy) lines.push(`limited by the banked ${line.limitedBy}, not by this item`);
                if (alts.length > 1) lines.push(`${alts.length} routes exist for this item; this is the one counted`);
            } else if (line && line.blocked === 'level') {
                lines.push(`needs ${line.skill} ${line.needLevel}; nothing counted`);
            } else if (alts.length) {
                lines.push(ctx.settings.xpRoute === 'fleet'
                    ? `${alts.length} route${alts.length === 1 ? '' : 's'} exist, but no script here runs one`
                    : 'no route counted');
            } else {
                lines.push('no processing route in this content');
            }
            return lines.concat(extraLines || []).join('\n');
        }

        /** True while either figure is on screen, which is what turns the tiles into cards. */
        function detailed() { return ctx.settings.showValue || ctx.settings.showXP; }

        // Two shapes of tile, on purpose. With both figures switched off this is the original
        // icon-only bank slot, unchanged, 8 to a row. With either on, the slot becomes a card
        // — icon, name, then a line of count/worth/XP — because a 42px square has room for a
        // count badge and nothing else, and squeezing a price into it made the grid unreadable.
        function itemChip(item) {
            const icon = iconFor(item.name);
            const chip = el('div', `item bank-slot item-${icon.cls}`);
            chip.tabIndex = 0;
            const iconEl = itemIconWrap(item.id, item.name);
            chip.append(iconEl);
            if (modelsOn) {
                // The icon box IS the model's stage. A phase derived from the item id keeps
                // the grid from turning in lockstep, and is stable across redraws so an item
                // does not jump when the bank refreshes.
                modelSlots.push({ id: item.id, el: iconEl, phase: (item.id % 16) * (Math.PI / 8) });
            }
            if (!detailed()) {
                chip.append(el('span', 'item-name', item.name || `#${item.id}`));
                if (item.count > 1) chip.append(el('span', 'item-count', fmtCount(item.count)));
            } else {
                const lines = el('div', 'bank-lines');
                lines.append(el('span', 'item-name', item.name || `#${item.id}`));
                const meta = el('div', 'bank-meta');
                meta.append(el('span', 'item-count', fmtCount(item.count)));
                if (ctx.settings.showValue) {
                    const p = priceOf(item);
                    meta.append(el('span', `item-worth src-${p.source}`, p.unit > 0 ? `${fmtCoins(p.unit * item.count)} gp` : '? gp'));
                }
                if (ctx.settings.showXP) {
                    const line = lineFor(item.id);
                    if (line && line.xp > 0) meta.append(el('span', 'item-xp', `${fmtXP(line.xp)} ${line.skill.toLowerCase()}`));
                    else if (line && line.blocked === 'level') meta.append(el('span', 'item-xp blocked', `needs ${line.needLevel}`));
                }
                lines.append(meta);
                chip.append(lines);
            }
            chip.title = itemDetail(item, item.banks && item.banks.length > 1 ? ['', ...item.banks] : null);
            chip.setAttribute('aria-label', chip.title);
            return decorate(chip);
        }

        function headSummary(items, account) {
            const bits = [];
            if (ctx.settings.showValue) {
                const v = valueOf(items);
                bits.push(`${fmtCoins(v.total)} gp`);
            }
            if (ctx.settings.showXP && account) {
                const a = xpForAccount(account);
                if (a) {
                    if (xp.levels === 'current' && !a.levelsKnown) bits.push('xp unknown (no levels reported)');
                    else bits.push(`${fmtXP(a.xp)} xp`);
                }
            }
            return bits.join(' · ');
        }

        function gpuRows(items, withAccount) {
            return items.map(it => {
                const p = priceOf(it);
                const line = lineFor(it.id);
                const detail = itemDetail(it);
                const cell = (text, style, align) => ({ runs: [{ text, style: style || 'td' }], align, title: detail });
                return {
                    key: withAccount ? `${it.account}/${it.id}` : String(it.id),
                    cells: [
                        ...(withAccount ? [cell(it.account || '', 'secondary')] : []),
                        cell(it.name || `#${it.id}`, 'headline'),
                        cell(fmtCount(it.count), 'td', 'right'),
                        cell(p.unit > 0 ? fmtCount(p.unit) : '?', 'td', 'right'),
                        cell(p.unit > 0 ? fmtCoins(p.unit * it.count) : '?', 'td', 'right'),
                        cell(sourceOf(p.source).label, 'secondary'),
                        cell(line && line.xp > 0 ? fmtXP(line.xp) : '—', 'gained', 'right'),
                        cell(line && line.xp > 0 ? line.skill : (line && line.blocked === 'level' ? `needs ${line.needLevel}` : ''), 'secondary'),
                        cell(line && line.via ? line.via : '', 'secondary')
                    ]
                };
            });
        }

        // The GPU table needs a box with a real height to fill, and the panel body is a plain
        // scroller. Rather than change that layout for everyone, the flex layout is switched
        // on ONLY while the GPU renderer is active, so the DOM path behaves exactly as it
        // always has — including the browser's own scrolling of the panel body.
        const gpuHost = el('div', 'bankall-gpu');
        let gpuHasAccount = false;
        function ensureGpu(withAccount) {
            // The column set changes with the grouping, and gpu-table lays its headers out
            // once at construction, so switching grouping rebuilds the table rather than
            // silently drawing rows into the wrong columns.
            if (gpu && gpuHasAccount !== withAccount) dropGpu();
            if (gpu) return gpu;
            gpuHasAccount = withAccount;
            body.classList.add('bankbody');
            wrap.hidden = true;
            body.append(gpuHost);
            gpu = createGpuTable(gpuHost, {
                headers: [...(withAccount ? ['Account'] : []), 'Item', 'Qty', 'Unit', 'Worth', 'Price from', 'XP', 'Skill', 'Route']
            });
            return gpu;
        }

        function dropGpu() {
            if (!gpu) return;
            gpu.destroy();
            gpu = null;
            gpuHost.remove();
            wrap.hidden = false;
            body.classList.remove('bankbody');
        }

        // ---- real ground models (the owner's own ask: "items should display with webgl and
        // use their ground item making sure to rotate it so its fully visible") ----
        //
        // ONE WebGL context for the whole panel, not one per item. A page gets roughly 16
        // contexts before the browser starts dropping the oldest, and this panel can show
        // hundreds of items; loot-gl.js already solves exactly that by setting viewport and
        // scissor per stage on a single canvas, so it is reused here rather than copied.
        //
        // The DOM grid still draws every card: the canvas only sits OVER it and fills each
        // card's icon box. So the text stays selectable and findable, the tooltips and the
        // filter keep working, and an item whose model has not decoded (or never will) shows
        // its ordinary icon instead of a hole. That per-item fallback is automatic, and it is
        // the same thing that happens for every item when the page has no capture adapter.
        const modelHost = el('div', 'bankall-stage');
        const modelCanvas = el('canvas', 'bankall-modelcanvas');
        modelCanvas.setAttribute('aria-hidden', 'true');

        /** Models are wanted AND the page can actually decode them. */
        function modelsWanted() { return ctx.settings.renderer === 'models'; }

        function ensureModels() {
            if (models) return models;
            if (!modelsUsable(meta)) {
                // The cache decoder is served by the capture adapter, which the fleet only
                // installs with -capture-renderer (and the page's CSP only allows its blob:
                // images alongside a stream client). Saying so beats an empty grid.
                modelNote = 'models need the capture renderer (-capture-renderer + -stream-client); showing icons';
                return null;
            }
            body.classList.add('bankbody');
            wrap.after(modelHost);
            modelHost.append(wrap, modelCanvas);
            try {
                models = createLootRenderer(modelCanvas);
            } catch (error) {
                // gpu-text.js's rule, and loot-gl.js's: a browser with no real GPU is refused
                // (failIfMajorPerformanceCaveat) rather than made to rasterise models in
                // software. That is the owner's "non webgl fall back" case, and it lands on
                // the ordinary icon grid.
                modelNote = `models unavailable (${error.message}); showing icons`;
                dropModels();
                return null;
            }
            modelNote = '';
            models.onRestore = () => { modelLast = 0; scheduleModels(); };
            textureAverages(meta).then(t => models && models.setTextureAverages(t)).catch(() => {});
            return models;
        }

        function dropModels() {
            if (modelFrame) { cancelAnimationFrame(modelFrame); modelFrame = 0; }
            if (models) { models.destroy(); models = null; }
            modelSlots = [];
            if (modelHost.parentNode) {
                modelHost.before(wrap);
                modelCanvas.remove();
                modelHost.remove();
            }
            if (!gpu) body.classList.remove('bankbody');
        }

        function scheduleModels() {
            if (!modelFrame && models && ctx.isVisible?.() !== false) modelFrame = requestAnimationFrame(modelTick);
        }

        /** Ask the cache decoder for the models this screenful still needs. */
        function requestModels(missing) {
            if (modelLoading || !missing.length) return;
            modelLoading = true;
            loadMeshes(meta, missing.slice(0, MODEL_BATCH))
                .catch(error => { if (!error?.slow) modelNote = `model decode failed: ${error.message}`; })
                .finally(() => { modelLoading = false; });
        }

        /**
         * Read every slot's position ONCE per DOM rebuild, not once per frame.
         *
         * This is the difference between a panel that works at fleet scale and one that does
         * not. Calling getBoundingClientRect on every slot every frame measured 1,186 ms/s of
         * main-thread work with the bank grouped by account (1,343 slots, 269 accounts) —
         * worse than any DOM variant. The slots do not move except by scrolling, so their
         * offsets inside the scroller are cached here and each frame just subtracts the
         * current scroll. wrap fills modelHost exactly (inset: 0), so a slot's canvas-relative
         * position is its cached offset minus the scroll.
         */
        function measureSlots() {
            if (!modelSlots.length) return;
            const w = wrap.getBoundingClientRect();
            for (const slot of modelSlots) {
                const r = slot.el.getBoundingClientRect();
                slot.top = r.top - w.top + wrap.scrollTop;
                slot.left = r.left - w.left + wrap.scrollLeft;
                slot.w = r.width;
                slot.h = r.height;
            }
            modelMeasured = true;
        }

        function modelTick(ts) {
            modelFrame = 0;
            if (closed || !models || models.lost || ctx.isVisible?.() === false) return;
            if (ts - modelLast < 1000 / MODEL_FPS) { scheduleModels(); return; }
            modelLast = ts;
            if (!modelMeasured) measureSlots();
            const host = modelHost.getBoundingClientRect();
            const dpr = Math.min(window.devicePixelRatio || 1, 2);
            const cw = Math.max(1, Math.round(host.width * dpr)), ch = Math.max(1, Math.round(host.height * dpr));
            if (modelCanvas.width !== cw || modelCanvas.height !== ch) {
                modelCanvas.width = cw; modelCanvas.height = ch;
                // The slots moved with the reflow that changed the panel's size.
                measureSlots();
            }
            // One clock for the whole grid, with a per-item phase, so the panel does not look
            // like a single machine turning in lockstep.
            const base = (ts / 1000) * SPIN_RATE;
            const top = wrap.scrollTop, left = wrap.scrollLeft;
            const stages = [], missing = [];
            for (const slot of modelSlots) {
                const y = slot.top - top, x = slot.left - left;
                // Only what is on screen is drawn, and only what is on screen is even asked
                // for: scrolling a 269-account bank must not decode every model in it.
                if (y + slot.h < -60 || y > host.height + 60 || slot.w < 2) {
                    if (slot.hidden) { slot.el.style.visibility = ''; slot.hidden = false; }
                    continue;
                }
                const key = `obj:${slot.id}`;
                const mesh = cachedMesh(key);
                if (!mesh) {
                    if (slot.hidden) { slot.el.style.visibility = ''; slot.hidden = false; }
                    if (meshWanted(key)) missing.push({ key, kind: 'obj', id: slot.id });
                    continue;
                }
                if (!models.hasModel(key)) models.setModel(key, mesh);
                // The icon hides only once its model is genuinely on the canvas, so an item
                // never flickers into an empty square while the cache is still decoding. The
                // write is guarded because setting an inline style every frame on hundreds of
                // elements is its own layout cost.
                if (!slot.hidden) { slot.el.style.visibility = 'hidden'; slot.hidden = true; }
                if (stages.length < MODEL_STAGE_CAP) {
                    stages.push({ model: key, yaw: base + slot.phase, rect: { x, y, w: slot.w, h: slot.h } });
                }
            }
            const painted = models.draw(stages, dpr);
            // Reported in the panel subtitle rather than kept private: "WebGL" on its own is a
            // claim, and 0 stages drawn out of 25 items is exactly the failure a label like
            // that hides. The Accounts panel and the GPU map say which backend they used for
            // the same reason.
            const drawn = painted ? models.stats.stages : 0;
            if (drawn !== modelDrawn || stages.length !== modelStaged) {
                modelDrawn = drawn; modelStaged = stages.length;
                ctx.setSub(`${banks.size} banks · WebGL ground models · ${drawn} on screen of ${modelSlots.length}`);
            }
            requestModels(missing);
            scheduleModels();
        }
        function draw(now) {
            if (ctx.isVisible?.() === false) return;
            const key = [ctx.settings.groupBy, ctx.settings.sortBy, ctx.settings.renderer, ctx.settings.showValue, ctx.settings.showXP,
                ctx.settings.xpRoute, ctx.settings.xpLevels, ctx.settings.xpRate, filter.value, names.join(','), bankSignature,
                prices.size, xp ? xp.combined.xp : -1].join('|');
            if (key === drawnKey) {
                if (Date.now() - summaryAt >= REFRESH_EVERY_MS) {
                    const withData = names.filter(n => banks.has(n));
                    drawSummary(ctx.settings.groupBy === 'none' ? mergedItems(withData) : withData.flatMap(n => banks.get(n).items));
                }
                for (const age of bankAges) {
                    const value = age.updatedAt ? `as of ${fmtDuration(now - age.updatedAt)} ago` : 'never opened this session';
                    if (age.node.textContent !== value) age.node.textContent = value;
                }
                return;
            }
            drawnKey = key;
            bankAges = [];
            hideTooltip();

            const withData = names.filter(n => banks.has(n));
            const everything = withData.flatMap(n => banks.get(n).items);
            drawSummary(ctx.settings.groupBy === 'none' ? mergedItems(withData) : everything);

            if (withData.length === 0) {
                dropGpu();
                dropModels();
                wrap.textContent = '';
                wrap.append(el('div', 'none', names.length ? 'no bank data yet for the picked accounts' : 'no accounts picked — edit this panel to choose some, or leave it on "all"'));
                return;
            }

            // The GPU renderer is ONE virtualised list either way: merged, it is one row per
            // item; grouped by account, it is one row per account-and-item with an Account
            // column instead of a DOM section each. That second case is the one the
            // measurement cared about — 269 accounts is ~1,340 rows, which is 12,000-odd DOM
            // nodes rebuilt every five seconds, and where the half-second frames came from.
            //
            // What this gives up, and why DOM stays the default: the text cannot be selected,
            // copied or found with Ctrl+F, and a screen reader gets one summary label instead
            // of cells. The table's own aria-label says to switch back for that.
            if (ctx.settings.renderer === 'gpu') {
                dropModels();
                wrap.textContent = '';
                const byAccount = ctx.settings.groupBy === 'account';
                const table = ensureGpu(byAccount);
                const items = byAccount
                    ? withData.flatMap(n => banks.get(n).items.map(it => ({ ...it, account: n })))
                    : mergedItems(withData);
                const rows = gpuRows(sortItems(items.filter(matches), ctx.settings.sortBy), byAccount);
                table.setRows(rows, '', filter.value ? 'No matching items' : "Every picked account's bank is empty (or never opened)");
                ctx.setSub(`${withData.length} banks · ${rows.length} rows · ${table.kind === 'webgl' ? 'WebGL' : 'Canvas'}`);
                return;
            }
            dropGpu();
            wrap.textContent = '';
            modelSlots = []; modelMeasured = false;
            // Both remaining modes draw the SAME DOM grid. "models" simply also puts a GL
            // canvas over it; if that cannot be set up, this is already the fallback and
            // there is nothing to undo.
            const wantModels = modelsWanted();
            const renderer = wantModels ? ensureModels() : (dropModels(), null);
            modelsOn = !!renderer;
            modelDrawn = modelStaged = -1; // the counts below are about to change; re-report them
            wrap.classList.toggle('bank-models-on', modelsOn);
            const backend = modelsOn
                ? 'WebGL ground models'
                : `DOM icons${wantModels && modelNote ? ` — ${modelNote}` : ''}`;
            ctx.setSub(`${withData.length} banks · ${backend}`);

            if (ctx.settings.groupBy === 'none') {
                const merged = mergedItems(withData).filter(matches);
                if (merged.length === 0) {
                    wrap.append(el('div', 'none', filter.value ? 'No matching items' : "Every picked account's bank is empty (or never opened)"));
                    return;
                }
                const grid = el('div', `itemgrid bank-grid${detailed() ? ' bank-detail' : ''}`);
                for (const it of sortItems(merged, ctx.settings.sortBy)) grid.append(itemChip(it));
                wrap.append(grid);
                scheduleModels();
                return;
            }

            for (const name of withData) {
                const b = banks.get(name);
                const section = el('div', 'bankall-section');
                const head = el('div', 'bankall-head');
                head.append(el('span', 'who', name));
                const worth = headSummary(b.items, name);
                if (worth) head.append(el('span', 'bankall-headsum', worth));
                const age = el('span', 'none', b.updatedAt ? `as of ${fmtDuration(now - b.updatedAt)} ago` : 'never opened this session');
                bankAges.push({ node: age, updatedAt: b.updatedAt });
                head.append(age);
                section.append(head);
                const grid = el('div', `itemgrid bank-grid${detailed() ? ' bank-detail' : ''}`);
                const visible = b.items.filter(matches);
                if (visible.length === 0) grid.append(el('div', 'none', filter.value ? 'No matching items' : 'empty'));
                else for (const it of sortItems(visible, ctx.settings.sortBy)) grid.append(itemChip({ ...it, account: name }));
                section.append(grid);
                wrap.append(section);
            }
            scheduleModels();
        }

        loadRoutes();
        ctx.onVisibility?.(visible => {
            if (visible) { modelLast = 0; scheduleModels(); }
            else if (modelFrame) { cancelAnimationFrame(modelFrame); modelFrame = 0; }
        });

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'reset', 'meta')) return;
                if (store.meta && store.meta.bank === false) {
                    note.textContent = 'this fleet does not report bank contents';
                    summary.textContent = '';
                    dropGpu();
                    wrap.textContent = '';
                    return;
                }
                meta = store.meta || meta;
                const picked = ctx.accounts.length ? ctx.accounts : store.accounts.map(a => a.name);
                names = picked.slice(0, MAX_ACCOUNTS);
                currentNow = serverNow(store);
                // The experience settings are answered by the SERVER, so changing one has to
                // re-ask rather than wait out the refresh interval — otherwise switching
                // "best route" on leaves the old numbers on screen for up to five seconds and
                // looks like the setting did nothing.
                const q = xpQuery();
                if (q !== lastXPQuery) { lastXPQuery = q; lastFetch = 0; }
                if (Date.now() - lastFetch >= REFRESH_EVERY_MS) loadAll();
                draw(currentNow);
            },
            destroy() {
                closed = true;
                dropGpu();
                // Hand the WebGL context back rather than waiting for collection: the page's
                // other GL panels share the browser's context budget.
                dropModels();
                hideTooltip(); tooltip.remove();
                window.removeEventListener('keydown', dismiss);
                window.removeEventListener('resize', hideTooltip);
                window.removeEventListener('resize', remeasure);
            }
        };
    }
};
