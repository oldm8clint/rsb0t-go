// Where the time goes: how wall time splits between phases (fishing, banking, walking, hand-off,
// random events, recovery, idle), fleet-wide, per script, for the selected account as a Gantt
// strip, and a ranking of the accounts that spend the least of their in-game time fishing.
// The point is finding XP/h losses: idle gaps, long bank trips, event handling.
//
// Data: GET /api/phases (internal/controlpanel/phases.go), sampled server-side once a second
// from each script's own reported phase. Offline time is left out of every share unless the
// "count offline time" setting is on — a logged-out account is a login problem, not an XP/h one.

import { anyOf, el, fmtDuration } from '../util.js';
import { createGpuTable } from '../gpu-table.js';
import { gantt, legend, stackedBar, tooltip } from '../phasebar.js';

const WINDOWS = [
    { value: '15m', label: 'Last 15 minutes' },
    { value: '1h', label: 'Last hour' },
    { value: '4h', label: 'Last 4 hours' }
];
const TIMELINES = [
    { value: '15', label: '15 minutes' },
    { value: '30', label: '30 minutes' },
    { value: '60', label: '1 hour' },
    { value: '120', label: '2 hours' }
];
const REFRESH_MS = 5000;
// Ranking columns, in the order an operator reads a fisher's losses.
const RANK_COLS = ['fishing', 'walking', 'banking', 'handoff', 'event', 'recovering', 'idle'];

// The fleet's random events in the window, by name: "Genie 12 (4m 10s) · Sandwich lady 3 ...".
function eventLine(events) {
    const line = el('div', 'tsplit-events');
    if (!events || !events.length) {
        line.textContent = 'Random events: none handled in this window';
        return line;
    }
    const total = events.reduce((s, e) => s + e.count, 0);
    line.append(`Random events · ${total}: `);
    events.forEach((e, i) => {
        if (i) line.append(' · ');
        const b = el('b', '', `${e.name} ${e.count}`);
        b.title = `${e.name}: ${e.count} handled across ${e.accounts} account${e.accounts === 1 ? '' : 's'}, ${fmtDuration(e.ms)} in all`;
        line.append(b, ` (${fmtDuration(e.ms)})`);
    });
    return line;
}

function pct(ms, total) {
    return total > 0 ? `${((ms / total) * 100).toFixed(0)}%` : '-';
}

export default {
    type: 'timesplit',
    title: 'Where the time goes',
    description: 'How wall time splits between fishing, banking, walking, hand-offs, random events, recovery and idle: fleet-wide, per script, a timeline for the selected account, and the accounts fishing least.',
    size: { w: 8, h: 5 },
    accounts: 'none',
    settings: [
        { key: 'window', label: 'Split over', kind: 'select', options: WINDOWS, default: '1h' },
        { key: 'timeline', label: 'Timeline length', kind: 'select', options: TIMELINES, default: '30' },
        { key: 'offline', label: 'Count offline time in shares', kind: 'bool', default: false }
    ],
    create(body, ctx) {
        const wrap = el('div', 'tsplit');
        const note = el('div', 'none');
        const fleetBox = el('div');
        const acctBox = el('div');
        const rankHead = el('h3', '', 'Least time fishing');
        const rankBox = el('div', 'tsplit-rank');
        wrap.append(note, fleetBox, acctBox, rankHead, rankBox);
        body.append(wrap);
        const tip = tooltip(wrap);
        const table = createGpuTable(rankBox, {
            headers: ['Account', 'Now', 'Fishing', 'Walking', 'Banking', 'Hand-off', 'Event', 'Recovering', 'Idle', 'Events', 'In game'],
            onSelect: name => ctx.store.select(name)
        });

        let data = null;
        let at = 0;
        let busy = false;
        let closed = false;
        let status = '';
        let asked = '';

        const drop = ms => (ctx.settings.offline ? ms : Object.fromEntries(Object.entries(ms).filter(([p]) => p !== 'offline')));
        const asMap = vec => Object.fromEntries(data.phases.map((p, i) => [p, vec[i] ?? 0]));

        async function load() {
            busy = true;
            at = Date.now();
            const sel = ctx.store.selected || '';
            const q = new URLSearchParams({ window: ctx.settings.window, minutes: ctx.settings.timeline });
            if (sel) q.set('account', sel);
            asked = sel;
            try {
                const res = await fetch(`api/phases?${q}`, { cache: 'no-store' });
                if (closed) return;
                if (res.status === 501) status = 'this fleet does not report script phases';
                else if (!res.ok) status = `could not load phases (${res.status})`;
                else { data = await res.json(); status = ''; }
            } catch {
                status = 'could not reach the control panel';
            } finally {
                busy = false;
                if (!closed) draw();
            }
        }

        function drawFleet() {
            fleetBox.textContent = '';
            const fleet = drop(asMap(data.fleet.ms));
            const scripts = data.scripts.map(g => ({ g, ms: drop(asMap(g.ms)) }));
            fleetBox.append(el('h3', '', `Fleet · ${data.fleet.accounts} accounts`), stackedBar(fleet, tip, 'fleet'));
            const rows = el('div', 'tsplit-scripts');
            for (const { g, ms } of scripts) {
                const row = el('div', 'tsplit-row');
                const name = el('span', 'tsplit-name', `${g.script} (${g.accounts})`);
                name.title = `${g.script}: ${g.accounts} account${g.accounts === 1 ? '' : 's'}`;
                row.append(name, stackedBar(ms, tip, g.script));
                rows.append(row);
            }
            fleetBox.append(rows, legend([fleet, ...scripts.map(s => s.ms)]), eventLine(data.events));
        }

        function drawAccount() {
            acctBox.textContent = '';
            const tl = data.timeline;
            const sel = ctx.store.selected;
            if (!sel) {
                acctBox.append(el('h3', '', 'Selected account'), el('div', 'none', 'click an account in any table to see its timeline'));
                return;
            }
            const row = data.accounts.find(a => a.name === sel);
            acctBox.append(el('h3', '', `${sel}${row && row.script ? ` · ${row.script}` : ''}${row && row.phase ? ` · now ${row.phase} for ${fmtDuration(data.now - row.since)}` : ''}`));
            if (!tl || tl.account !== sel || !row) {
                acctBox.append(el('div', 'none', row ? 'loading its timeline…' : 'no phase samples for this account yet'));
                return;
            }
            acctBox.append(gantt(tl.segments, tl.from, tl.to, tip, '', tl.events));
            const split = drop(asMap(row.ms));
            const bar = stackedBar(split, tip, sel);
            bar.classList.add('thin');
            acctBox.append(bar);
            if (tl.totals) {
                const total = Object.entries(drop(tl.totals)).reduce((s, [, ms]) => s + ms, 0);
                const fish = tl.totals.fishing ?? 0;
                acctBox.append(el('div', 'none', `since first seen ${fmtDuration(data.now - tl.firstSeen)} ago: ${fmtDuration(total)} counted, ${pct(fish, total)} fishing`));
            }
        }

        function drawRanking() {
            const idx = Object.fromEntries(data.phases.map((p, i) => [p, i]));
            const col = (a, p) => (idx[p] === undefined ? 0 : a.ms[idx[p]] ?? 0);
            const rows = [];
            for (const a of data.accounts) {
                const fishing = col(a, 'fishing');
                if (!/fisher/i.test(a.script) && fishing <= 0) continue;
                const inGame = a.ms.reduce((s, ms, i) => s + (data.phases[i] === 'offline' ? 0 : ms), 0);
                if (inGame <= 0) continue;
                const share = fishing / inGame;
                const text = v => ({ runs: [{ text: v, style: 'secondary' }], title: v, align: 'right' });
                rows.push({
                    share,
                    row: {
                        key: a.name,
                        cells: [
                            { runs: [{ text: a.name, style: 'td' }], title: a.name },
                            { runs: [{ text: a.phase || '-', style: 'secondary' }], title: a.phase || '' },
                            { runs: [{ text: pct(fishing, inGame), style: 'headline' }], title: `${fmtDuration(fishing)} fishing of ${fmtDuration(inGame)} in game`, align: 'right' },
                            ...RANK_COLS.slice(1).map(p => text(pct(col(a, p), inGame))),
                            text(String(a.events ?? 0)),
                            text(fmtDuration(inGame))
                        ]
                    }
                });
            }
            rows.sort((a, b) => a.share - b.share || a.row.key.localeCompare(b.row.key));
            table.setRows(rows.map(r => r.row), ctx.store.selected, 'no fishing accounts with samples in this window');
            return rows.length;
        }

        function draw() {
            note.textContent = status;
            if (!data) {
                if (!status) note.textContent = 'loading…';
                return;
            }
            drawFleet();
            drawAccount();
            const ranked = drawRanking();
            const w = WINDOWS.find(x => x.value === data.window);
            ctx.setSub(`${w ? w.label.toLowerCase() : data.window} · ${ranked} fishers ranked · ${table.kind === 'webgl' ? 'WebGL' : 'Canvas'}`);
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'select')) return;
                const selChanged = (store.selected || '') !== asked;
                if (!busy && (selChanged || Date.now() - at >= REFRESH_MS)) load();
                else if (why.has('select') && data) draw();
            },
            destroy() {
                closed = true;
                table.destroy();
            }
        };
    }
};
