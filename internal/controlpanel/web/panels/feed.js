import { anyOf, el, logLine } from '../util.js';

const LEVELS = [
    { value: 'debug', label: 'debug' },
    { value: 'info', label: 'info' },
    { value: 'warn', label: 'warnings' },
    { value: 'error', label: 'errors' }
];

const SOURCES = [
    { value: 'both', label: 'log lines and events' },
    { value: 'log', label: 'log lines only' },
    { value: 'events', label: 'events only: logins, state changes, crashes' }
];

export default {
    type: 'feed',
    title: 'Event log',
    description: 'Log lines and events from the accounts you pick, newest first. Warnings and errors alone make a "needs you" feed.',
    size: { w: 4, h: 3 },
    accounts: 'many',
    settings: [
        { key: 'levels', label: 'Levels', kind: 'multi', options: LEVELS, default: ['info', 'warn', 'error'] },
        { key: 'source', label: 'Show', kind: 'select', options: SOURCES, default: 'both' },
        { key: 'lines', label: 'Lines kept on screen', kind: 'number', min: 10, max: 2000, step: 10, default: 200 },
        { key: 'wrap', label: 'Wrap long lines', kind: 'bool', default: false }
    ],
    create(body, ctx) {
        const list = el('div', ctx.settings.wrap ? 'feed wrap' : 'feed');
        body.append(list);
        const levels = new Set(ctx.settings.levels);
        const names = ctx.accounts.length ? new Set(ctx.accounts) : null;
        const source = ctx.settings.source;
        const wanted = ev => ev.kind !== 'chat' && levels.has(ev.level) && (names === null || names.has(ev.account)) && (source === 'both' || (source === 'log') === (ev.kind === 'log'));
        let shown = 0;

        // Only lines newer than the newest drawn are added, so a busy fleet costs a few nodes a frame.
        function append(store) {
            const fresh = [];
            for (let i = store.events.length - 1; i >= 0 && store.events[i].seq > shown && fresh.length < ctx.settings.lines; i--) {
                if (wanted(store.events[i])) fresh.push(store.events[i]);
            }
            if (store.events.length) shown = Math.max(shown, store.events[store.events.length - 1].seq);
            const frag = document.createDocumentFragment();
            for (const ev of fresh) frag.append(logLine(ev, true));
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
