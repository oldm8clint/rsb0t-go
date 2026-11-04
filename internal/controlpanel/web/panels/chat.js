// A live feed of the game's own chat/system messages — public chat, trade requests,
// "you have been PKed"-style system lines, GM/staff messages — as distinct from
// feed.js's "Event log" (what the BOT is doing) or account.js's own Log section (also
// the bot's decisions). A headless client has no game window to glance at, so this is
// the account's own eyes on the world: see internal/fleet.AccountStatus.RecentChat and
// internal/controlpanel.KindChat for where the lines come from.

import { anyOf, el, fmtClock } from '../util.js';

export default {
    type: 'chat',
    title: 'Chat feed',
    description: "What the game itself says to each account — public chat, trade requests, PK/system lines, GM messages — not what the bot is doing. A headless client's only window into the world.",
    size: { w: 4, h: 3 },
    accounts: 'many',
    settings: [
        { key: 'lines', label: 'Lines kept on screen', kind: 'number', min: 10, max: 1000, step: 10, default: 200 },
        { key: 'showAccount', label: 'Show which account', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const list = el('div', 'feed chatfeed');
        body.append(list);
        const names = ctx.accounts.length ? new Set(ctx.accounts) : null;
        const wanted = ev => ev.kind === 'chat' && (names === null || names.has(ev.account));
        let shown = 0;

        function line(ev) {
            const row = el('div', 'line lv-info chatline');
            row.append(el('span', 't', fmtClock(ev.t)));
            if (ctx.settings.showAccount && ev.account) row.append(el('span', 'who', ev.account));
            row.append(el('span', 'msg', ev.text));
            row.title = ev.text;
            return row;
        }

        // Only events newer than the newest already drawn are appended, matching feed.js's
        // own approach, so a busy fleet costs a handful of DOM nodes a frame rather than a
        // full redraw.
        function append(store) {
            const fresh = [];
            for (let i = store.events.length - 1; i >= 0 && store.events[i].seq > shown && fresh.length < ctx.settings.lines; i--) {
                if (wanted(store.events[i])) fresh.push(store.events[i]);
            }
            if (store.events.length) shown = Math.max(shown, store.events[store.events.length - 1].seq);
            const frag = document.createDocumentFragment();
            for (const ev of fresh) frag.append(line(ev));
            list.prepend(frag);
            while (list.childElementCount > ctx.settings.lines) list.lastElementChild.remove();
            ctx.setSub(String(list.childElementCount));
        }

        return {
            update(store, why) {
                if (anyOf(why, 'init', 'reset')) {
                    list.textContent = '';
                    shown = 0;
                    append(store);
                } else if (why.has('event')) {
                    append(store);
                }
            }
        };
    }
};
