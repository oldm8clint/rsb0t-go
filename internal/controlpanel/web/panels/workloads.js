import { anyOf, el, fmtCount, pick, table } from '../util.js';
import { classify, dataFresh } from './incidents.js';

export function workloadRows(accounts, by = 'script') {
    const groups = new Map();
    for (const a of accounts) {
        const name = by === 'world' ? (a.world ? `World ${a.world}` : 'World unknown') : a.bot || 'No script';
        if (!groups.has(name)) groups.set(name, { name, total: 0, online: 0, running: 0, attention: 0, xp: 0 });
        const g = groups.get(name);
        g.total++; g.online += a.state === 'ingame';
        g.running += a.state === 'ingame' && a.botState === 'running';
        g.attention += classify(a).some(i => i.sev !== 'info');
        for (const sk of Object.values(a.skills ?? {})) g.xp += Math.max(0, Number(sk.xpGained) || 0);
    }
    return [...groups.values()].sort((a,b) => b.attention - a.attention || b.total - a.total || a.name.localeCompare(b.name));
}

export default {
    type: 'workloads', title: 'Workloads',
    description: 'Compare script or world coverage, running accounts, incidents and reported session XP.',
    size: { w: 6, h: 3 }, accounts: 'many',
    settings: [{ key: 'group', label: 'Group by', kind: 'select', default: 'script', options: [{value:'script',label:'Script'}, {value:'world',label:'World'}] }],
    create(body, ctx) {
        const fresh = el('div', 'none'), wrap = el('div'), note = el('p', 'none', 'XP is the sum of reported skill gains in current sessions; it can reset on reconnect. Lifetime totals live in What the fleet made.');
        body.append(fresh, wrap, note);
        let signature = '';
        return { update(store, why) {
            if (!anyOf(why, 'init', 'status', 'conn', 'clock', 'reset')) return;
            fresh.hidden = dataFresh(store);
            fresh.textContent = 'Last known workload; current status is stale or disconnected.';
            if (!anyOf(why, 'init', 'status', 'reset')) return;
            const groups = workloadRows(pick(store.accounts, ctx.accounts), ctx.settings.group);
            const key = JSON.stringify(groups);
            if (signature === key) return;
            signature = key;
            wrap.replaceChildren(groups.length ? table([ctx.settings.group === 'world' ? 'World' : 'Script', 'Accounts', 'Online', 'Running', 'Attention', 'Session XP'], groups.map(g => [g.name, g.total, g.online, g.running, g.attention, fmtCount(g.xp)])) : el('div', 'none', 'No accounts reported yet.'));
            ctx.setSub(`${groups.length} groups`);
        }};
    }
};
