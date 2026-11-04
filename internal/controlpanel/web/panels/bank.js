// One account's last-known bank contents — real item names/icons, sourced from
// GET /api/bank (see internal/controlpanel.BankSource and internal/fleet.
// AccountStatus.BankItems/BankUpdatedAt). This used to be a needsData placeholder; the
// primitive it was waiting on (Bank.Items, already live-proven for banking actions) is
// now wired through to the dashboard, one account at a time, the same on-demand
// contract account.js already uses for its own Inventories section.
//
// Deliberately not "live every tick": a bank's contents only change when a script
// actually opens it, so this shows a plain "as of <time ago>" note rather than
// implying a live reading — see the fleet-side field doc comments for why.

import { anyOf, el, fmtCount, fmtDuration, serverNow } from '../util.js';
import { iconFor, itemIconWrap } from '../itemicons.js';

const REFRESH_EVERY_MS = 4000;

// itemChip mirrors account.js's own helper of the same name (kept as a separate small
// copy rather than a shared export, to avoid a cross-panel coupling over a ~10 line
// function while itemicons.js is also being actively extended by another session
// tonight — see COLLAB.md).
function itemChip(s) {
    const icon = iconFor(s.name);
    const chip = el('div', `item bank-slot item-${icon.cls}`);
    chip.tabIndex = 0;
    const iconWrap = itemIconWrap(s.id, s.name);
    chip.append(iconWrap, el('span', 'item-name', s.name || `#${s.id}`));
    if (s.count > 1) chip.append(el('span', 'item-count', fmtCount(s.count)));
    chip.title = `${s.name || `item #${s.id}`}${s.count > 1 ? ` x${fmtCount(s.count)}` : ''} (id ${s.id})`;
    chip.setAttribute('aria-label', chip.title);
    return chip;
}

export default {
    type: 'bank',
    title: 'Bank contents',
    description: "One account's bank, as of the last time a script actually opened it — real item names and icons, not a live reading.",
    size: { w: 4, h: 3 },
    accounts: 'one',
    settings: [],
    create(body, ctx) {
        const note = el('div', 'none');
        const grid = el('div', 'itemgrid bank-grid');
        body.append(note, grid);
        let name = '';
        let at = 0;
        let busy = false;
        let closed = false;
        let drawnKey = '';

        function draw(items, updatedAt, now) {
            const key = `${items.length}|${updatedAt}`;
            if (key === drawnKey) return;
            drawnKey = key;
            grid.textContent = '';
            if (items.length === 0) {
                grid.append(el('div', 'none', updatedAt ? 'empty' : 'never opened this session'));
            } else {
                for (const s of items) grid.append(itemChip(s));
            }
            note.textContent = updatedAt ? `as of ${fmtDuration(now - updatedAt)} ago` : '';
        }

        async function load(account) {
            busy = true;
            at = Date.now();
            try {
                const res = await fetch(`api/bank?account=${encodeURIComponent(account)}`, { cache: 'no-store' });
                if (closed || account !== name) return;
                if (res.ok) {
                    const body = await res.json();
                    draw(body.items ?? [], body.updatedAt ?? 0, serverNow(ctx.store));
                } else if (res.status === 501) {
                    note.textContent = "this fleet does not report bank contents";
                    grid.textContent = '';
                } else {
                    note.textContent = '';
                    grid.textContent = '';
                }
            } catch {
                // Tried again on a later beat.
            } finally {
                busy = false;
            }
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'select', 'clock', 'reset', 'meta')) return;
                const fixed = ctx.accounts[0];
                const picked = fixed || store.selected || store.accounts[0]?.name || '';
                ctx.setSub(fixed || !picked ? picked : `${picked}, following the table`);
                if (!picked) {
                    note.textContent = 'no account selected';
                    grid.textContent = '';
                    drawnKey = '';
                    name = '';
                    return;
                }
                if (store.meta && store.meta.bank === false) {
                    note.textContent = 'this fleet does not report bank contents';
                    grid.textContent = '';
                    return;
                }
                if (picked !== name) {
                    name = picked;
                    drawnKey = '';
                    grid.textContent = '';
                    note.textContent = 'loading...';
                    load(picked);
                } else if (!busy && Date.now() - at >= REFRESH_EVERY_MS) {
                    load(picked);
                }
            },
            destroy() {
                closed = true;
            }
        };
    }
};
