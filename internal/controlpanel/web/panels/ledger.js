// Mule ledger — every finished trade the fleet's mules have made, from GET /api/mule-ledger
// (see cmd/controlpanel's -mule-ledger flag and internal/controlpanel's ledger recorder).
//
// This panel was a `needsData` placeholder until 2026-09-20, saying it needed "a per-mule
// tally of items in and out by trade partner, written after each trade". That had been
// shipped some time earlier: the endpoint answers with the newest few thousand trades, each
// carrying the mule, the partner, what moved, the outcome, and — when a trade did not
// complete — the reason it did not. The placeholder simply outlived the gap it described.
//
// What it is for. The mule is the fleet's throughput bottleneck (measured: capacity-bound,
// not scheduling-bound, at ~84% busy), so the two questions worth answering continuously are
// "is it keeping up" and "when it fails, why". The failure reasons are the valuable half and
// are deliberately given their own breakdown rather than being buried per row: on the live
// fleet 20 of 22 failures in a 200-trade window were a single cause, "no stable offer",
// which is a fixable defect rather than background noise.
//
// Items are summarised per trade rather than listed in full: one hand-off routinely carries
// a dozen stacks of thousands, and the interesting figure is how much moved, not every line.
// Hovering a row's items shows the full breakdown.

import { anyOf, el, fmtCount, fmtDuration, kv, table } from '../util.js';

const REFRESH_EVERY_MS = 10000;
const MAX_ROWS = 60;

/** Total items in one direction of a trade. */
function totalOf(items) {
    let n = 0;
    for (const it of items ?? []) n += it.count ?? 0;
    return n;
}

/** "Raw bass 380, Raw mackerel 774, +4 more" — the shape of a hand-off at a glance. */
function summarise(items, max = 3) {
    const list = [...(items ?? [])].sort((a, b) => (b.count ?? 0) - (a.count ?? 0));
    if (!list.length) return 'nothing';
    const head = list.slice(0, max).map(it => `${it.name} ${fmtCount(it.count)}`).join(', ');
    return list.length > max ? `${head}, +${list.length - max} more` : head;
}

function fullBreakdown(items) {
    return [...(items ?? [])]
        .sort((a, b) => (b.count ?? 0) - (a.count ?? 0))
        .map(it => `${it.name} x${fmtCount(it.count)}`)
        .join('\n');
}

export default {
    type: 'ledger',
    title: 'Mule ledger',
    description: "Every trade the fleet's mules have finished: who handed over what, which trades failed and why.",
    size: { w: 6, h: 4 },
    accounts: 'none',
    settings: [
        { key: 'outcome', label: 'Show trades', kind: 'select', options: [
            { value: 'all', label: 'All trades' },
            { value: 'failed', label: 'Failed only' }
        ], default: 'all' },
        { key: 'rows', label: 'Trades listed', kind: 'select', options: [
            { value: '20', label: '20' },
            { value: '60', label: '60' },
            { value: '200', label: '200' }
        ], default: '60' }
    ],
    create(body, ctx) {
        const summary = el('dl', 'kv');
        const reasons = el('div', 'ledger-reasons');
        const listWrap = el('div');
        body.append(summary, reasons, listWrap);

        let data = null, status = '', busy = false, at = 0, closed = false;
        let drawnData = undefined, drawnStatus = null, ages = [];

        function ageRows() {
            const now = Date.now() - (ctx.store.skew ?? 0);
            for (const {cell, at} of ages) {
                const text = at ? fmtDuration(Math.max(0, now - at)) : '—';
                if (cell.textContent !== text) { cell.textContent = text; cell.title = text; }
            }
        }

        async function load() {
            if (busy || closed) return;
            busy = true;
            at = Date.now();
            try {
                const res = await fetch('api/mule-ledger', { cache: 'no-store' });
                if (closed) return;
                if (res.status === 501) status = 'this fleet is not recording a mule ledger (start it with -mule-ledger)';
                else if (!res.ok) status = `could not load the mule ledger (${res.status})`;
                else { data = await res.json(); status = ''; }
            } catch {
                status = 'could not reach the control panel';
            } finally {
                busy = false;
            }
            draw();
        }

        function draw() {
            if (drawnData === data && drawnStatus === status) { ageRows(); return; }
            drawnData = data; drawnStatus = status; ages = [];
            summary.textContent = '';
            reasons.textContent = '';
            listWrap.textContent = '';

            if (status) { kv(summary, 'ledger', status); return; }
            const entries = data?.entries ?? [];
            if (!entries.length) {
                kv(summary, 'ledger', data ? 'no trade has finished yet' : 'loading…');
                return;
            }

            const now = Date.now() - (ctx.store.skew ?? 0);
            const accepted = entries.filter(e => e.outcome === 'accepted');
            const failed = entries.filter(e => e.outcome !== 'accepted');
            const mules = new Set(entries.map(e => e.mule));
            const partners = new Set(entries.map(e => e.partner));
            let items = 0;
            for (const e of accepted) items += totalOf(e.in);

            // The window is however many trades the endpoint keeps, so say what it covers
            // rather than implying these are lifetime figures.
            const span = entries.length > 1 ? Math.max(0, (entries[entries.length - 1].at ?? 0) - (entries[0].at ?? 0)) : 0;
            kv(summary, 'trades', `${fmtCount(entries.length)}${span ? ` over ${fmtDuration(span)}` : ''}`);
            kv(summary, 'completed', `${fmtCount(accepted.length)} (${Math.round(accepted.length / entries.length * 100)}%)`);
            kv(summary, 'failed', fmtCount(failed.length));
            kv(summary, 'items received', fmtCount(items));
            kv(summary, 'mules', [...mules].join(', ') || '—');
            kv(summary, 'donors', fmtCount(partners.size));
            if (accepted.length) kv(summary, 'per trade', `${fmtCount(Math.round(items / accepted.length))} items`);

            // Why trades fail, biggest first. This is the half worth acting on.
            if (failed.length) {
                const byReason = new Map();
                for (const e of failed) {
                    const r = e.reason || 'no reason recorded';
                    byReason.set(r, (byReason.get(r) ?? 0) + 1);
                }
                const sorted = [...byReason].sort((a, b) => b[1] - a[1]);
                reasons.append(el('div', 'ledger-reasons-label', `Why ${fmtCount(failed.length)} trade${failed.length === 1 ? '' : 's'} did not complete`));
                const chips = el('div', 'ledger-chips');
                for (const [reason, count] of sorted) {
                    const chip = el('span', 'ledger-chip');
                    chip.append(el('span', 'ledger-chip-count', fmtCount(count)), el('span', 'ledger-chip-name', reason));
                    chip.title = `${count} of ${failed.length} failed trades: ${reason}`;
                    chips.append(chip);
                }
                reasons.append(chips);
            }

            const want = ctx.settings.outcome === 'failed' ? failed : entries;
            const limit = Number(ctx.settings.rows) || MAX_ROWS;
            // Newest first: the endpoint appends, so the tail is the most recent.
            const rows = want.slice(-limit).reverse().map(e => {
                const when = e.at ? fmtDuration(Math.max(0, now - e.at)) : '—';
                const outcome = e.outcome === 'accepted' ? 'completed' : (e.reason ? `failed — ${e.reason}` : 'failed');
                return [when, e.mule ?? '—', e.partner ?? '—', outcome, `${fmtCount(totalOf(e.in))} · ${summarise(e.in)}`];
            });
            if (!rows.length) {
                listWrap.append(el('div', 'none', 'no failed trades in this window'));
                return;
            }
            const t = table(['Ago', 'Mule', 'Donor', 'Outcome', 'Received'], rows);
            t.classList.add('tally-table', 'ledger-table');
            // Mark the failed rows so the stylesheet can tint them, and hang the full item
            // breakdown off the row rather than truncating it away.
            const source = want.slice(-limit).reverse();
            [...t.tBodies[0]?.rows ?? []].forEach((tr, i) => {
                const e = source[i];
                if (!e) return;
                ages.push({cell: tr.cells[0], at: e.at});
                if (e.outcome !== 'accepted') tr.classList.add('is-bad');
                const cell = tr.cells[tr.cells.length - 1];
                if (cell) cell.title = fullBreakdown(e.in) || 'nothing received';
            });
            listWrap.append(t);
        }

        load();
        return {
            // 'clock' ticks once a second; the endpoint is polled at most every
            // REFRESH_EVERY_MS and the rest of the ticks just re-age the "Ago" column. A
            // settings change is not a reason here: the page destroys and recreates a panel
            // when its settings change (see panels/index.js), so create() runs again.
            update(store, why) {
                if (!anyOf(why, 'init', 'clock')) return;
                if (Date.now() - at >= REFRESH_EVERY_MS) load();
                else draw();
            },
            destroy() { closed = true; }
        };
    }
};
