import { anyOf, el, fmtDuration, serverNow, table } from '../util.js';
import { dataFresh } from './incidents.js';

export function routeState(r, now) {
    if (r.health && r.health !== 'healthy') return `${r.health}${r.lastError ? `: ${r.lastError}` : ''}`;
    if (r.fullUntil > now) return `Full · ${fmtDuration(r.fullUntil - now)}`;
    if (r.restUntil > now) return `Resting · ${fmtDuration(r.restUntil - now)}`;
    if (r.latencyGate && r.latencyGate !== 'passed') return `Latency gate: ${r.latencyGate}`;
    return r.inFlight > 0 ? `Logging in: ${r.inFlight}` : 'Ready';
}

export default {
    type: 'connections', title: 'Connection health',
    description: 'Route capacity, latency, login failures and drops, plus game-world admission status.',
    size: { w: 6, h: 3 }, accounts: 'none', settings: [],
    create(body, ctx) {
        const fresh = el('div', 'none'), routes = el('div'), worlds = el('div');
        body.append(fresh, routes, worlds);
        let signature = '';
        return { update(store, why) {
            if (!anyOf(why, 'init', 'status', 'clock', 'conn', 'reset')) return;
            fresh.hidden = dataFresh(store); fresh.textContent = 'Status stale or disconnected; route health is last known.';
            const now = serverNow(store), login = store.logins;
            const rows = (login?.routes ?? []).map(r => [r.id, `${r.sessions ?? 0} / ${r.max ?? '—'}`, routeState(r, now), `${r.loginsOk ?? 0} / ${r.loginsFailed ?? 0}`, r.drops ?? 0, r.latencyMedianMs > 0 ? `${r.latencyMedianMs} ms` : 'Not sampled']);
            const worldRows = (login?.worlds ?? []).map(w => [w.world, w.accounts, w.full ? 'Last login refused: full' : 'No full-world refusal reported']);
            const key = JSON.stringify([rows, worldRows]);
            if (signature === key) return;
            signature = key;
            routes.replaceChildren(rows.length ? table(['Route','Sessions / cap','Admission','OK / failed','Drops','Median latency'], rows) : el('div', 'none', login ? 'No proxy pool reported; direct connections may be in use.' : 'Waiting for login status.'));
            worlds.replaceChildren();
            if (worldRows.length) worlds.append(el('h3', '', 'Worlds'), table(['World','Assigned','Admission'], worldRows));
            ctx.setSub(`${rows.length} routes · fleet-wide`);
        }};
    }
};
