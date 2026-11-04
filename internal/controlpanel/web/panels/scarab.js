import { anyOf, el, button, fmtDuration } from '../util.js';
import { dataFresh } from './incidents.js';

export const STAGES = {1: 'Bootstrap', 2: 'Food and funds', 3: 'Rune supply / Magic', 5: 'Survival / combat', 6: 'Queen'};
export function scarabRows(accounts, phases) {
    const byName = new Map((phases?.accounts || []).map(p => [p.name, p]));
    return accounts.filter(a => (a.bot || a.assignedScript || a.rosterScript) === 'ScarabSwarm').map(a => {
        const p = byName.get(a.name);
        const phase = p?.script === 'ScarabSwarm' ? p.phase : '';
        const match = /^s(\d+)\//.exec(phase || '');
        const stage = match ? Number(match[1]) : null;
        const targets = [];
        for (const [key, skill] of [['stage2TargetThievingLevel', 'Thieving'], ['stage3MagicTarget', 'Magic'], ['stage5HitpointsTarget', 'Hitpoints']]) {
            const target = Number(a.settings?.[key]);
            const level = a.skills?.[skill]?.level;
            if (target > 0) targets.push(`${skill} ${level ?? '?'}/${target}`);
        }
        let condition = 'Active';
        if (a.dead) condition = 'Dead';
        else if (a.watchdog) condition = `Watchdog: ${a.watchdog.state}`;
        else if (a.botState === 'error') condition = 'Script error';
        else if (a.state !== 'ingame') condition = a.state || 'Unknown';
        else if (a.botState !== 'running') condition = a.botState || 'No script state';
        else if (/(?:^|\/)(wait|rest|muster|mule|kit|stock)$/.test(phase)) condition = 'Reported wait / supply phase';
        return {account: a, stage, phase: phase || 'unreported', since: phase ? p?.since : null, condition, targets};
    }).sort((a,b) => (a.stage ?? 99) - (b.stage ?? 99) || a.account.name.localeCompare(b.account.name));
}

export default {
    type: 'scarab', title: 'ScarabSwarm progression',
    description: 'Stage cohorts, current phases, configured skill targets and accounts needing attention.',
    size: {w: 8, h: 4}, accounts: 'many', settings: [],
    create(body, ctx) {
        const note = el('div', 'none');
        const summary = el('div', 'scarab-summary');
        const table = el('table', 'scarab-table');
        const foot = el('div', 'none', 'Ages are time in the current reported phase, not total stage time. Skill targets shown only when configured. Inventory requirements and lifetime deaths / Queen kills are not exposed by this feed.');
        body.append(note, summary, table, foot);
        const names = ctx.accounts.length ? new Set(ctx.accounts) : null;
        let phases = null, received = 0, busy = false, disposed = false, error = '';
        let drawn = '', ages = [];
        const controller = new AbortController();
        async function poll() {
            if (busy || disposed || ctx.isVisible?.() === false) return;
            busy = true;
            try {
                const res = await fetch('api/phases?view=current', {cache:'no-store', signal:controller.signal});
                if (!res.ok) throw Error(`HTTP ${res.status}`);
                const value = await res.json();
                if (!disposed) { phases = value; received = Date.now(); error = ''; render(ctx.store); }
            } catch (e) {
                if (!disposed) { error = `Phase data unavailable: ${e.message}`; render(ctx.store); }
            } finally { busy = false; }
        }
        function render(store) {
            const fresh = dataFresh(store) && received > 0 && Date.now() - received < 15000 && !error;
            note.textContent = error || (!received ? 'Loading reported phases...' : fresh ? 'Live reported stages; select an account to inspect its details.' : 'Stale data: showing the last received stages.');
            const rows = scarabRows(store.accounts.filter(a => !names || names.has(a.name)), phases);
            const key = JSON.stringify(rows.map(r => [r.account.name, r.stage, r.phase, r.since, r.condition, r.targets]));
            if (key !== drawn) {
                drawn = key;
                ages = [];
                summary.replaceChildren();
                for (const [stage,label] of Object.entries(STAGES)) summary.append(el('span', 'scarab-stage', `S${stage} ${label}: ${rows.filter(r => r.stage === Number(stage)).length}`));
                summary.append(el('span', 'scarab-stage', `Other / unreported: ${rows.filter(r => !STAGES[r.stage]).length}`));
                table.replaceChildren();
                const head = el('thead'), hr = el('tr');
                for (const text of ['Account', 'Stage / phase', 'Phase age', 'Condition', 'Configured targets']) hr.append(el('th', '', text));
                head.append(hr); table.append(head);
                const tbody = el('tbody');
                for (const row of rows) {
                    const tr = el('tr'), name = el('td');
                    const pick = button(row.account.name, `Select ${row.account.name} in account detail panels`);
                    pick.addEventListener('click', () => store.select(row.account.name));
                    name.append(pick); tr.append(name);
                    const age = el('td');
                    ages.push({node: age, since: row.since});
                    tr.append(el('td', '', row.phase), age, el('td', '', row.condition), el('td', '', row.targets.join(' / ') || 'not configured'));
                    tbody.append(tr);
                }
                table.append(tbody);
            }
            // Freeze phase ages when stale instead of pretending time was observed.
            const phaseNow = fresh ? phases.now + Date.now() - received : phases?.now;
            for (const age of ages) age.node.textContent = age.since > 0 && phaseNow ? fmtDuration(Math.max(0, phaseNow - age.since)) : 'unknown';
            ctx.setSub(`${rows.length} accounts${fresh ? '' : ' - awaiting fresh data'}`);
        }
        poll();
        ctx.onVisibility?.(visible => { if (visible) poll(); });
        const timer = setInterval(poll, 5000);
        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'conn', 'reset')) return;
                if (why.has('reset')) { phases = null; received = 0; poll(); }
                render(store);
            },
            destroy() { disposed = true; clearInterval(timer); controller.abort(); }
        };
    }
};
