// A hiscores lookup panel: type a name (or pick a connected account) and see their real
// example.invalid skills/rank table rendered natively in this app's own style, not just a
// link out.
//
// Investigated before rewriting this (see internal/controlpanel/hiscores.go's own doc
// comment for the full account): no separate JSON API behind the page (its own network
// requests show the table arrives as part of the server-rendered HTML), and no framing
// block either (no X-Frame-Options/CSP frame-ancestors on the response) — an iframe
// embed would also have worked, but parsing was chosen so this reads as part of the
// same app instead of a different site's full page (nav bar, footer, donation blurb)
// showing up inside a panel. The direct link out is kept too, one click away, for
// whoever wants the real page (recent-progress history, raw HTML, etc. this panel
// doesn't reproduce).
//
// The account owner confirmed the URL pattern live against the real site:
// https://example.invalid/hiscores/player/<username>, with the player name lowercased and
// spaces turned into underscores ("Red Bracket" -> "red_bracket") — internal/
// controlpanel/hiscores.go does the same normalisation server-side.

import { anyOf, el, fmtCount } from '../util.js';

export default {
    type: 'hiscores',
    title: 'Hiscores lookup',
    description: "A player's real example.invalid skills/rank table, rendered natively — type a name or pick a connected account.",
    size: { w: 4, h: 4 },
    accounts: 'many',
    settings: [],
    create(body, ctx) {
        const row = el('div', 'hiscores-row');
        const input = el('input');
        input.type = 'text';
        input.placeholder = 'player name';
        input.autocomplete = 'off';
        input.spellcheck = false;
        const go = el('button', '', 'Look up');
        go.type = 'button';
        go.className = 'primary';
        row.append(input, go);
        const status = el('div', 'none');
        const table = el('table', 'hiscores-table');
        table.hidden = true;
        const link = el('a', 'hiscores-link');
        link.target = '_blank';
        link.rel = 'noopener noreferrer';
        link.hidden = true;
        const recent = el('div', 'hiscores-recent');
        body.append(row, status, table, link, recent);

        let busy = false;
        let closed = false;
        let lastLookup = '';

        function renderSkills(skills) {
            table.textContent = '';
            const head = el('tr');
            head.append(el('th', '', 'Skill'), el('th', 'num', 'Rank'), el('th', 'num', 'Level'), el('th', 'num', 'XP'));
            table.append(head);
            for (const sk of skills) {
                const tr = el('tr', sk.name === 'Overall' ? 'overall' : '');
                tr.append(el('td', '', sk.name));
                tr.append(el('td', 'num', sk.ranked ? fmtCount(sk.rank) : '-'));
                tr.append(el('td', 'num', sk.ranked ? sk.level : '-'));
                tr.append(el('td', 'num', sk.ranked ? fmtCount(sk.xp) : '-'));
                table.append(tr);
            }
        }

        async function lookup(name) {
            const clean = name.trim();
            if (!clean || busy) return;
            lastLookup = clean;
            busy = true;
            status.textContent = `looking up ${clean}...`;
            status.classList.remove('bad');
            table.hidden = true;
            link.hidden = true;
            try {
                const res = await fetch(`api/hiscores?player=${encodeURIComponent(clean)}`, { cache: 'no-store' });
                if (closed || clean !== lastLookup) return;
                const data = await res.json().catch(() => ({}));
                if (!res.ok) {
                    status.textContent = data.error || `HTTP ${res.status}`;
                    status.classList.add('bad');
                    return;
                }
                status.textContent = '';
                renderSkills(data.skills ?? []);
                table.hidden = false;
                link.href = data.url;
                link.textContent = 'view full page on example.invalid →';
                link.hidden = false;
            } catch (err) {
                if (closed || clean !== lastLookup) return;
                status.textContent = `request failed: ${err}`;
                status.classList.add('bad');
            } finally {
                busy = false;
            }
        }

        go.addEventListener('click', () => lookup(input.value));
        input.addEventListener('keydown', ev => {
            if (ev.key === 'Enter') lookup(input.value);
        });

        let drawnAccounts = '';

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status')) return;
                const list = (ctx.accounts.length ? store.accounts.filter(a => ctx.accounts.includes(a.name)) : store.accounts).map(a => a.name);
                const key = list.join(',');
                if (key === drawnAccounts) return;
                drawnAccounts = key;
                recent.textContent = '';
                for (const name of list) {
                    const b = el('button', '', name);
                    b.type = 'button';
                    b.addEventListener('click', () => {
                        input.value = name;
                        lookup(name);
                    });
                    recent.append(b);
                }
            },
            destroy() {
                closed = true;
            }
        };
    }
};
