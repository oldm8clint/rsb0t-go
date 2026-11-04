// Preserve the saved "alerts" panel id while grouping related incidents.
import { anyOf, el, button, fmtDuration } from '../util.js';
import { groupIncidents, dataFresh } from './incidents.js';

export default {
    type: 'alerts', title: 'Incidents',
    description: 'Grouped failures and watchdog stalls, with affected accounts and observed ages.',
    size: { w: 5, h: 3 }, accounts: 'many',
    settings: [{ key: 'includeInfo', label: 'Include low-priority notes', kind: 'bool', default: false }],
    create(body, ctx) {
        const freshness = el('div', 'none');
        const list = el('div', 'incident-list');
        const empty = el('div', 'none');
        body.append(freshness, list, empty);
        const names = ctx.accounts.length ? new Set(ctx.accounts) : null;
        const seen = new Map();
        let drawn = '', ages = [];
        let groupCount = 0, affectedCount = 0;
        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'reset', 'conn')) return;
                if (why.has('reset')) { seen.clear(); drawn = ''; }
                const now = Date.now();
                const fresh = dataFresh(store, now);
                freshness.hidden = fresh;
                freshness.textContent = store.statusReceivedAt
                    ? `Status is stale (${fmtDuration(now - store.statusReceivedAt)} since update); last known incidents below.`
                    : 'Waiting for the first fleet status.';
                if (!anyOf(why, 'init', 'status', 'reset')) {
                    for (const age of ages) {
                        const text = `observed ${fmtDuration(now - age.since)}`;
                        if (age.node.textContent !== text) age.node.textContent = text;
                    }
                    empty.textContent = fresh ? 'No current incidents in this selection.' : 'No incidents in the last received status; current health unknown.';
                    ctx.setSub(`${groupCount} groups - ${affectedCount} accounts${fresh ? '' : ' - stale'}`);
                    return;
                }
                const groups = groupIncidents(store.accounts.filter(a => !names || names.has(a.name)), ctx.settings.includeInfo);
                groupCount = groups.length;
                affectedCount = new Set(groups.flatMap(g => g.members.map(m => m.account.name))).size;
                const active = new Set();
                for (const g of groups) for (const m of g.members) {
                    const key = JSON.stringify([g.key, m.account.name]);
                    active.add(key);
                    if (!seen.has(key)) seen.set(key, now);
                    m.since = seen.get(key);
                }
                for (const key of seen.keys()) if (!active.has(key)) seen.delete(key);
                const key = JSON.stringify(groups.map(g => [g.key, g.members.map(m => [m.account.name, m.alert.text, m.since])]));
                if (key !== drawn) {
                    const expanded = new Set([...list.children].filter(n => n.open).map(n => n.dataset.key));
                    drawn = key; list.replaceChildren(); ages = [];
                    for (const g of groups) {
                        const details = el('details', `incident sev-${g.sev}`);
                        details.dataset.key = g.key; details.open = expanded.has(g.key);
                        const summary = el('summary', '', `${g.sev === 'crit' ? 'Critical' : g.sev === 'warn' ? 'Warning' : 'Note'} - ${g.members.length} account${g.members.length === 1 ? '' : 's'} - ${g.title}`);
                        details.append(summary);
                        for (const m of g.members) {
                            const row = el('div', 'incident-member');
                            const pick = button(m.account.name, `Select ${m.account.name} in account detail panels`);
                            pick.addEventListener('click', () => store.select(m.account.name));
                            const age = el('span', 'muted');
                            ages.push({node: age, since: m.since});
                            row.append(pick, el('span', '', m.alert.text), age);
                            details.append(row);
                        }
                        list.append(details);
                    }
                }
                for (const age of ages) age.node.textContent = `observed ${fmtDuration(now - age.since)}`;
                empty.hidden = groups.length > 0;
                empty.textContent = fresh ? 'No current incidents in this selection.' : 'No incidents in the last received status; current health unknown.';
                ctx.setSub(`${groupCount} groups - ${affectedCount} accounts${fresh ? '' : ' - stale'}`);
            }
        };
    }
};
