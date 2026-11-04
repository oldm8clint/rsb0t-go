// What the fleet made — accumulated run statistics, from GET /api/stats (see
// internal/controlpanel/stats.go). Was a needsData placeholder ("bots that report a run
// summary... run history kept on disk by the fleet, since the control panel keeps only
// short rings in memory") until that exact primitive was built: the fleet-side rings
// still only hold recent activity, but this panel's own data now comes from a separate
// aggregator that polls the same account snapshots continuously (not gated on a browser
// tab being open) and persists lifetime totals to disk, so this panel's numbers survive
// every restart — the account owner's own "always remember previous session data of
// things that are important" ask.
//
// Two independent toggles, both the account owner's own explicit ask ("split in
// individual and combined total view"): Period (Lifetime, persisted across every
// restart, vs. Since restart, this process's own contribution) and View (Combined, one
// fleet-wide total, vs. Individual, a per-account leaderboard).

import { anyOf, el, fmtCount, fmtDuration, kv, table } from '../util.js';

const PERIODS = [
    { value: 'lifetime', label: 'Lifetime (persisted)' },
    { value: 'sinceRestart', label: 'Since this restart' }
];
const VIEWS = [
    { value: 'combined', label: 'Combined total' },
    { value: 'individual', label: 'Individual accounts' }
];

const REFRESH_EVERY_MS = 8000; // the server-side aggregator itself only samples every 10s

// totalsRows lists the rows for one combinedTotals. "Current level sum" and "Accounts
// tracked" are current-state snapshots, not accumulating deltas (see stats.go's own
// combinedTotals.minus doc comment) — meaningless for the "since restart" period, where
// they'd always read as 0 or the same number either way, so they're only shown for
// Lifetime rather than implying a since-restart figure that doesn't exist.
function totalsRows(t, period) {
    const rows = [];
    if (period === 'lifetime') {
        rows.push(['Accounts tracked', fmtCount(t.accountsTracked)]);
    }
    rows.push(
        ['Total XP gained', fmtCount(t.xpGained)],
        ['Total levels gained', fmtCount(t.levelsGained)]
    );
    if (period === 'lifetime') {
        rows.push(['Current level sum', fmtCount(t.currentLevelSum)]);
    }
    rows.push(
        ['Total deaths', fmtCount(t.deaths)],
        // Three separate things, because lumping them together is what produced "17,802
        // failed" against "6,963 cleared" on 2026-09-20. Completed is the only success;
        // "not ours" is the guardian correctly declining another player's event; retries are
        // further attempts on the SAME event instance, most of which clear later. Only a
        // handler error or a final give-up is a failure. See internal/controlpanel/stats.go.
        ['Random events completed', fmtCount(t.randomsCompleted ?? t.randomsCleared)],
        ['Random events failed', fmtCount(t.randomsFailed)],
        ['Random events not ours', fmtCount(t.randomsNotOurs)],
        ['Random event retries', fmtCount(t.randomsRetried)],
        ['Reconnect episodes', fmtCount(t.reconnects)],
        ['Combined uptime', fmtDuration(t.uptimeSecs * 1000)]
    );
    return rows;
}

export default {
    type: 'tally',
    title: 'What the fleet made',
    description: 'Accumulated run statistics — total XP/levels gained, deaths, random events cleared/failed, reconnects, uptime — combined across the fleet or per account, persisted across restarts.',
    size: { w: 7, h: 4 },
    accounts: 'many',
    settings: [
        { key: 'period', label: 'Period', kind: 'select', options: PERIODS, default: 'lifetime' },
        { key: 'view', label: 'View', kind: 'select', options: VIEWS, default: 'combined' }
    ],
    create(body, ctx) {
        const note = el('div', 'none');
        const wrap = el('div', 'bankall-wrap');
        body.append(note, wrap);

        let data = null;
        let at = 0;
        let busy = false;
        let closed = false;
        let drawnKey = '';
        let drawnData = null;

        async function load() {
            busy = true;
            at = Date.now();
            try {
                const res = await fetch('api/stats', { cache: 'no-store' });
                if (closed) return;
                if (res.ok) data = await res.json();
            } catch {
                // Tried again on a later beat.
            } finally {
                busy = false;
            }
        }

        function draw() {
            if (data === drawnData && drawnKey) return;
            drawnData = data;
            const key = `${ctx.settings.period}|${ctx.settings.view}|${JSON.stringify(data)}`;
            if (key === drawnKey) return;
            drawnKey = key;
            wrap.textContent = '';

            if (!data) {
                note.textContent = 'loading...';
                return;
            }

            const trackingAgo = fmtDuration(Date.now() - data.trackingSince - (ctx.store?.skew ?? 0));
            note.textContent = `tracking since ${trackingAgo} ago — figures are sampled every few seconds, not a live stopwatch`;

            const totals = ctx.settings.period === 'sinceRestart' ? data.sinceRestart : data.lifetime;

            if (ctx.settings.view === 'combined') {
                const dl = el('dl', 'kv');
                for (const [term, value] of totalsRows(totals, ctx.settings.period)) kv(dl, term, value);
                wrap.append(dl);
                return;
            }

            const picked = ctx.accounts.length
                ? data.accounts.filter(a => ctx.accounts.includes(a.username))
                : data.accounts;
            if (picked.length === 0) {
                wrap.append(el('div', 'none', data.accounts.length ? 'no stats yet for the picked accounts' : 'no accounts tracked yet'));
                return;
            }
            const sorted = [...picked].sort((a, b) => b.xpGained - a.xpGained || a.username.localeCompare(b.username));
            const rows = sorted.map(a => {
                // For "since restart", per-account deltas aren't computed server-side
                // (see stats.go's own doc comment — the SinceRestart view is fleet-wide
                // only); the individual table always shows each account's own lifetime
                // figures, labeled plainly rather than silently mismatching the picked
                // Period toggle above.
                return [
                    a.username,
                    fmtCount(a.xpGained),
                    fmtCount(a.levelsGained),
                    fmtCount(a.currentLevelSum),
                    fmtCount(a.deaths),
                    fmtCount(a.randomsCleared),
                    fmtCount(a.randomsFailed),
                    fmtCount(a.randomsNotOurs),
                    fmtCount(a.randomsRetried),
                    fmtCount(a.reconnects),
                    fmtDuration(a.uptimeSecs * 1000)
                ];
            });
            const t = table(['Account', 'XP gained', 'Levels gained', 'Level sum', 'Deaths', 'Randoms OK', 'Randoms failed', 'Not ours', 'Retries', 'Reconnects', 'Uptime'], rows);
            t.classList.add('tally-table');
            if (ctx.settings.period === 'sinceRestart') {
                wrap.append(el('div', 'none', 'individual figures below are always lifetime totals — see Combined total for this restart’s own contribution'));
            }
            wrap.append(t);
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'reset', 'meta')) return;
                if (store.meta && store.meta.stats === false) {
                    note.textContent = 'this fleet does not report accumulated statistics';
                    wrap.textContent = '';
                    return;
                }
                if (!busy && Date.now() - at >= REFRESH_EVERY_MS) load();
                draw();
            },
            destroy() {
                closed = true;
            }
        };
    }
};
