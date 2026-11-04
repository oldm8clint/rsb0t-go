import { anyOf, el, fmtDuration, kv, serverNow, statePill, table } from '../util.js';
import { routeState } from './connections.js';

export default {
    type: 'logins',
    title: 'Logins and reconnects',
    description: 'Who is waiting to log in and when the next login is due, and every account waiting to reconnect.',
    size: { w: 4, h: 2 },
    accounts: 'many',
    settings: [{ key: 'problems', label: 'List failed and stopped accounts', kind: 'bool', default: true }],
    create(body, ctx) {
        const summary = el('dl', 'kv');
        const detail = el('div');
        body.append(summary, detail);
        detail.addEventListener('click', ev => {
            const tr = ev.target instanceof Element ? ev.target.closest('tr') : null;
            if (tr && tr.dataset.name) ctx.store.select(tr.dataset.name);
        });
        let nextCell = null, heldCell = null, routeCells = [], retryCells = [];
        let signature = '';
        const setText = (node, value) => { if (node && node.textContent !== value) node.textContent = value; };
        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'reset')) return;
                const l = store.logins;
                const now = serverNow(store);
                let changed = anyOf(why, 'init', 'reset');
                if (why.has('status')) {
                    const key = JSON.stringify([l, store.accounts.map(a => [a.name,a.state,a.reconnects,a.nextRetry,a.lastError])]);
                    changed ||= key !== signature;
                    signature = key;
                }
                if (!changed) {
                    setText(nextCell, l?.nextAttempt > now ? `in ${fmtDuration(l.nextAttempt - now)}` : 'due');
                    if (heldCell) {
                        setText(heldCell, l?.heldUntil > now ? `${fmtDuration(l.heldUntil - now)} more, after the server refused a login` : 'open');
                    }
                    for (const { node, route } of routeCells) {
                        const value = routeState(route, now);
                        if (node.textContent !== value) node.textContent = value;
                    }
                    for (const { node, account } of retryCells) setText(node, account.nextRetry > now ? `in ${fmtDuration(account.nextRetry - now)}` : 'due');
                    return;
                }
                nextCell = null; heldCell = null; routeCells = []; retryCells = [];
                summary.textContent = '';
                detail.textContent = '';
                if (!l) {
                    kv(summary, 'queue', 'waiting for the first report');
                    return;
                }
                const names = ctx.accounts.length ? new Set(ctx.accounts) : null;
                const mine = n => names === null || names.has(n);
                const queue = l.queue ?? [];

                kv(summary, 'waiting to log in', queue.length);
                kv(summary, 'logging in now', l.inFlight);
                kv(summary, 'logged in', l.maxSessions > 0 ? `${l.sessions} of ${l.maxSessions}` : l.sessions);
                if (l.spacingMs > 0) kv(summary, 'logins spaced', fmtDuration(l.spacingMs));
                if (queue.length > 0) nextCell = kv(summary, 'next login', l.nextAttempt > now ? `in ${fmtDuration(l.nextAttempt - now)}` : 'due');
                if (l.heldUntil > now) heldCell = kv(summary, 'queue held', `${fmtDuration(l.heldUntil - now)} more, after the server refused a login`);

                // The proxy pool's routes (fleet/routes.go): how full each address is and whether
                // it is logging in, resting between bursts, held or unhealthy. Absent with no pool.
                const routes = l.routes ?? [];
                if (routes.length) {
                    const rows = routes.map(r => [
                        r.id,
                        `${r.sessions} of ${r.max}`,
                        routeState(r, now),
                        `${r.loginsOk} ok, ${r.loginsFailed} failed`,
                        r.drops,
                        r.latencyMedianMs ? `${r.latencyMedianMs} ms` : '-'
                    ]);
                    const t = table(['Route', 'In game', 'Status', 'Logins', 'Drops', 'Latency'], rows);
                    [...t.tBodies[0].rows].forEach((row, i) => {
                        delete row.dataset.name;
                        routeCells.push({node: row.cells[2], route: routes[i]});
                    });
                    detail.append(el('h3', '', 'Routes'), t);
                }

                const queued = queue.map((name, i) => [name, `#${i + 1}`]).filter(([name]) => mine(name));
                if (queued.length) detail.append(el('h3', '', 'Queue'), table(['Account', 'Position'], queued));

                const waiting = store.accounts.filter(a => mine(a.name) && a.state === 'reconnecting').sort((a, b) => a.nextRetry - b.nextRetry);
                if (waiting.length) {
                    const rows = waiting.map(a => [a.name, a.reconnects, a.nextRetry > now ? `in ${fmtDuration(a.nextRetry - now)}` : 'due', a.lastError || '-']);
                    const t = table(['Account', 'Attempt', 'Retry', 'Why'], rows);
                    [...t.tBodies[0].rows].forEach((row, i) => retryCells.push({node: row.cells[2], account: waiting[i]}));
                    detail.append(el('h3', '', 'Reconnecting'), t);
                }
                if (ctx.settings.problems) {
                    const failed = store.accounts.filter(a => mine(a.name) && ['error', 'stopped', 'offline'].includes(a.state) && a.lastError);
                    if (failed.length) detail.append(el('h3', '', 'Failed, dropped or stopped'), table(['Account', 'State', 'Last error'], failed.map(a => [a.name, statePill(a.state), a.lastError])));
                }
                ctx.setSub(`${queue.length} waiting, ${waiting.length} reconnecting`);
            }
        };
    }
};
