// What the player-run markets are holding, so an operator (or a script's operator) can tell
// whether a trip across the map for an axe is worth making at all, and to WHICH market.
//
// WHY A PANEL IS THE ONLY WAY TO SEE THIS. These are PLAYER-run shops, not shop interfaces.
// Confirmed for Seers by reading the game content rather than assuming: there is no `.inv` file
// anywhere under `content/scripts/areas/area_seers/`, none of its four NPCs carries a
// `param=owned_shop`, and `openshop` appears nowhere in the area at all. So no account can ever
// query this stock in game — buying means standing there, saying `buying <n> <item>` in public
// chat, and waiting for the person on the other side to tally it and open a trade. This panel
// is the substitute for the shop interface the game does not have.
//
// WHAT THE NUMBERS ARE. A periodic snapshot of each market character's BANK, not live stock. The
// upstream page says so itself and stamps its own age, and both that stamp and the age are shown
// here rather than quietly dropped — a stale number presented as current is exactly how someone
// sends an account on a wasted trip. When the fetch fails the panel says it could not read the
// market; it never falls back to showing the last numbers as if they were fresh.
//
// The fetch happens SERVER-side (internal/controlpanel/market.go, behind /api/market) because
// this dashboard's own Content-Security-Policy restricts connect-src to 'self', so the browser
// cannot reach example.invalid. That is deliberate and was not widened for this panel.
//
// Everything shown here comes from a third-party page and is treated strictly as data: every
// value is written with textContent, never as HTML, and nothing in it is ever executed.

import { anyOf, el, fmtCount } from '../util.js';

// The woodcutting axes, worst to best — the reason this panel exists, so it gets a one-click
// filter. These are the chopping axes only: the battleaxes the market also lists are weapons and
// are deliberately not here, and a pickaxe is for mining.
const AXES = ['Bronze axe', 'Iron axe', 'Steel axe', 'Black axe', 'Mithril axe', 'Adamant axe', 'Rune axe'];

// The markets the upstream page's own character selector lists, mirrored from
// internal/controlpanel/market.go's KnownMarkets. All five were fetched and parsed before being
// listed; none is a guess. '0' means every market at once, which is the default, because the
// real question is "which market has one", not "what does this one market hold".
//
// Seers Market and Tradevault are both in Seers' Village, at the same bank, so an account
// standing there reaches both with one public-chat line — which is why a market being out of
// stock is a reason to look at its neighbour before falling back to Bob's in Lumbridge.
const MARKET_OPTIONS = [
    { value: '0', label: 'All markets' },
    { value: '795', label: 'Seers Market' },
    { value: '2169', label: 'Tradevault (Seers)' },
    { value: '864', label: 'Constructor' },
    { value: '941', label: 'Edge Market' },
    { value: '2148', label: 'Sil Stuff' }
];

// How often the panel re-asks. The server caches for two minutes and the upstream data is itself
// only a periodic snapshot, so asking faster would just re-serve identical bytes.
const REFRESH_MS = 60_000;

export default {
    type: 'market',
    title: 'Player markets',
    description: "What the player-run markets are holding and what things last traded for — periodic snapshots, since these are player shops with no in-game interface.",
    size: { w: 4, h: 4 },
    accounts: 'none',
    settings: [
        // Which market to open on. 0 means all of them together, which is the default because
        // "where can I buy an axe" is a question across markets, not about one.
        { key: 'character', label: 'Market to show', kind: 'select', options: MARKET_OPTIONS, default: '0' },
        { key: 'axesOnly', label: 'Start filtered to axes', kind: 'bool', default: false }
    ],
    create(body, ctx) {
        const startAt = String(ctx.settings?.character ?? '0');

        const controls = el('div', 'hiscores-row');
        const pick = el('select', 'market-pick');
        for (const o of MARKET_OPTIONS) {
            const opt = el('option', '', o.label);
            opt.value = o.value;
            pick.append(opt);
        }
        pick.value = MARKET_OPTIONS.some(o => o.value === startAt) ? startAt : '0';
        const search = el('input');
        search.type = 'text';
        search.placeholder = 'filter items';
        search.autocomplete = 'off';
        search.spellcheck = false;
        const axesBtn = el('button', '', 'Axes only');
        axesBtn.type = 'button';
        const refresh = el('button', '', 'Refresh');
        refresh.type = 'button';
        refresh.className = 'primary';
        controls.append(pick, search, axesBtn, refresh);

        const stamp = el('div', 'market-stamp');
        const status = el('div', 'none');
        const table = el('table', 'hiscores-table market-table');
        table.hidden = true;
        const link = el('a', 'hiscores-link');
        link.target = '_blank';
        link.rel = 'noopener noreferrer';
        link.hidden = true;
        body.append(controls, stamp, status, table, link);

        let axesOnly = !!ctx.settings?.axesOnly;
        axesBtn.classList.toggle('primary', axesOnly);
        let items = [];
        let busy = false;
        let closed = false;

        const isAxe = name => AXES.some(a => a.toLowerCase() === String(name ?? '').toLowerCase());

        // Each row carries the market it came from, so the all-markets view can answer the
        // question that actually matters — "which market has one" — rather than merging five
        // stocks into an unattributable total.
        function render() {
            const q = search.value.trim().toLowerCase();
            const rows = items.filter(it => {
                if (axesOnly && !isAxe(it.name)) return false;
                if (q && !String(it.name ?? '').toLowerCase().includes(q)) return false;
                return true;
            });
            const everywhere = pick.value === '0';
            table.textContent = '';
            const head = el('tr');
            head.append(el('th', '', 'Item'));
            if (everywhere) head.append(el('th', '', 'Market'));
            head.append(el('th', 'num', 'In stock'), el('th', 'num', 'Last sold'), el('th', 'num', 'Last bought'));
            table.append(head);
            for (const it of rows) {
                const tr = el('tr', isAxe(it.name) ? 'market-axe' : '');
                tr.append(el('td', '', it.name ?? ''));
                if (everywhere) tr.append(el('td', '', it.market ?? ''));
                tr.append(el('td', 'num', fmtCount(it.stock ?? 0)));
                // A missing price means "never traded", which is not the same as free, so it
                // shows as a dash and never as 0.
                tr.append(el('td', 'num', it.lastSold ? fmtCount(it.lastSold) : '-'));
                tr.append(el('td', 'num', it.lastBought ? fmtCount(it.lastBought) : '-'));
                table.append(tr);
            }
            table.hidden = rows.length === 0;
            status.classList.remove('bad');
            status.textContent = rows.length === 0
                ? (items.length ? 'nothing matches that filter' : 'nothing listed')
                : '';
            ctx.setSub?.(`${rows.length} of ${items.length} item(s)`);
        }

        function fail(text) {
            // Deliberately clears the table: showing the previous snapshot beside an error
            // reads as current stock, which is the one mistake this panel must not make.
            items = [];
            table.hidden = true;
            stamp.textContent = '';
            link.hidden = true;
            status.textContent = text;
            status.classList.add('bad');
            ctx.setSub?.('unavailable');
        }

        async function load() {
            if (ctx.isVisible?.() === false) return;
            if (busy) return;
            busy = true;
            const everywhere = pick.value === '0';
            if (!items.length) status.textContent = 'reading the market...';
            try {
                const url = everywhere ? 'api/market?all=1' : `api/market?character=${encodeURIComponent(pick.value)}`;
                const res = await fetch(url, { cache: 'no-store' });
                if (closed) return;
                const data = await res.json().catch(() => ({}));
                if (!res.ok) {
                    fail(`could not read the market: ${data.error || `HTTP ${res.status}`}`);
                    return;
                }
                if (everywhere) {
                    // A market that is down is reported as down. It must never look like a
                    // market that is simply out of stock.
                    const down = [];
                    const oldest = [];
                    items = [];
                    for (const m of Array.isArray(data.markets) ? data.markets : []) {
                        if (!m.result) {
                            down.push(m.name);
                            continue;
                        }
                        if (m.result.snapshotAge) oldest.push(`${m.name} ${m.result.snapshotAge}`);
                        for (const it of m.result.items ?? []) items.push({ ...it, market: m.name });
                    }
                    stamp.textContent = `${oldest.length} market(s) - bank snapshots, ${oldest.join(', ') || 'age unknown'}. Not live stock.`
                        + (down.length ? ` Could not read: ${down.join(', ')}.` : '');
                    stamp.title = '';
                    link.href = 'https://example.invalid/market/inventory?character=795';
                    link.textContent = 'view the markets on example.invalid →';
                    link.hidden = false;
                } else {
                    items = Array.isArray(data.items) ? data.items : [];
                    const age = data.snapshotAge ? `as of ${data.snapshotAge}` : 'age unknown';
                    stamp.textContent = `${data.market || 'Market'} - bank snapshot, ${age}. Not live stock.`;
                    stamp.title = data.snapshotAt ? `snapshot taken ${data.snapshotAt}` : '';
                    link.href = data.url || '#';
                    link.textContent = 'view the market on example.invalid →';
                    link.hidden = !data.url;
                }
                render();
            } catch (err) {
                if (closed) return;
                fail(`could not read the market: ${err}`);
            } finally {
                busy = false;
            }
        }

        search.addEventListener('input', render);
        pick.addEventListener('change', () => {
            items = [];
            load();
        });
        axesBtn.addEventListener('click', () => {
            axesOnly = !axesOnly;
            axesBtn.classList.toggle('primary', axesOnly);
            render();
        });
        refresh.addEventListener('click', load);

        const timer = setInterval(load, REFRESH_MS);
        ctx.onVisibility?.(visible => { if (visible && started) load(); });
        let started = false;

        return {
            update(store, why) {
                if (started || !anyOf(why, 'init', 'status')) return;
                started = true;
                load();
            },
            destroy() {
                closed = true;
                clearInterval(timer);
            }
        };
    }
};
