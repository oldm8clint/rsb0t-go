import { anyOf, el, fmtCount, fmtDuration, kv, pick, serverNow } from '../util.js';
import { classify, dataFresh } from './incidents.js';

export default {
    type: 'operations', title: 'Operations',
    description: 'Fleet readiness, script health and the shared login gate in one view.',
    size: { w: 4, h: 3 }, accounts: 'many', settings: [],
    create(body, ctx) {
        const tiles = el('div', 'tiles');
        const cells = new Map();
        for (const label of ['In game', 'Running scripts', 'Needs attention', 'Reconnecting']) {
            const tile = el('div', 'tile'), value = el('b', '', '—');
            tile.append(value, el('span', '', label)); tiles.append(tile); cells.set(label, value);
        }
        const dl = el('dl', 'kv'), queue = kv(dl, 'Fleet login queue', '—');
        const capacity = kv(dl, 'Fleet sessions', '—'), next = kv(dl, 'Next login', '—');
        const held = kv(dl, 'Login gate', '—'), fresh = el('p', 'none');
        body.append(tiles, dl, fresh);
        let key = '', login = null;
        return { update(store, why) {
            if (!anyOf(why, 'init', 'status', 'clock', 'conn', 'reset')) return;
            if (anyOf(why, 'init', 'status', 'reset')) {
                const accounts = pick(store.accounts, ctx.accounts);
                const values = [accounts.filter(a => a.state === 'ingame').length,
                    accounts.filter(a => a.state === 'ingame' && a.botState === 'running').length,
                    accounts.filter(a => classify(a).some(i => i.sev !== 'info')).length,
                    accounts.filter(a => a.state === 'reconnecting').length];
                const sig = values.join('|');
                if (sig !== key) { key = sig; [...cells.values()].forEach((cell, i) => cell.textContent = fmtCount(values[i])); }
                login = store.logins;
                queue.textContent = login ? fmtCount(login.queue?.length ?? 0) : 'Waiting for status';
                capacity.textContent = login ? `${login.sessions ?? 0}${login.maxSessions > 0 ? ` / ${login.maxSessions}` : ''}` : '—';
                ctx.setSub(`${accounts.length} shown${store.truncated ? ` · ${store.truncated} omitted` : ''}`);
            }
            const now = serverNow(store);
            next.textContent = !login ? '—' : !login.queue?.length ? 'Queue empty' : login.nextAttempt > now ? `in ${fmtDuration(login.nextAttempt - now)}` : 'Due';
            held.textContent = !login ? 'Unknown' : login.heldUntil > now ? `Held ${fmtDuration(login.heldUntil - now)}` : `${login.inFlight ?? 0} logging in`;
            fresh.textContent = dataFresh(store) ? 'Live status · queue and capacity cover the whole fleet' : 'Status stale or disconnected; values are last known';
            fresh.classList.toggle('bad', !dataFresh(store));
        }};
    }
};
