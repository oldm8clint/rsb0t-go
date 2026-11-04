// Mule dashboard: what each MuleReceiver is doing and how well. Who has announced a hand-off and
// is on the way, the trade in progress and its stage, trades completed and failed per hour with
// the success rate and the last failure reasons, cycle timing and demand against supply (the same
// measures tools/mule-throughput.py takes from a log, computed live), the time split, the stock
// its last bank read showed, and the hand-outs it gave.
//
// Data: GET /api/mule (internal/controlpanel/mulepanel.go), from the mule's own published status
// (internal/mulestatus: live state and cycle timing), the fleet's persisted trade record
// (internal/muleledger: rates, declines, items, hand-outs), the cached bank read and the phase
// recorder.

import { anyOf, el, fmtCount, fmtDuration, kv, table } from '../util.js';
import { legend, phaseColor, stackedBar, tooltip } from '../phasebar.js';

const REFRESH_MS = 3000;
const STAGE_TEXT = {
    reciprocating: 'answered the trade request',
    offer: 'waiting for the offer to settle',
    accepted: 'accepted, waiting for the confirm screen',
    confirm: 'confirmed, waiting for the trade to close',
    receipt: 'checking the items arrived'
};

function perHour(n, windowMs) {
    return windowMs > 0 ? (n * 3600000) / windowMs : 0;
}

function ago(now, t) {
    return `${fmtDuration(Math.max(0, now - t))} ago`;
}

function phaseDot(phase) {
    const s = el('span', 'st', phase || '-');
    if (phase) s.style.setProperty('--c', phaseColor(phase));
    return s;
}

// A table whose rows are not accounts, so clicking one selects nothing.
function plain(headers, rows) {
    const t = table(headers, rows);
    for (const tr of t.querySelectorAll('tr[data-name]')) delete tr.dataset.name;
    return t;
}

function itemRows(list) {
    return (list ?? []).map(i => [i.name, fmtCount(i.count)]);
}

function drawMule(m, now, tip) {
    const box = el('section', 'mule');
    const head = el('div', 'mule-head');
    head.append(el('b', '', m.account), phaseDot(m.phase));
    if (m.trade) {
        head.append(el('span', '', `trading with ${m.trade.partner || '?'}: ${STAGE_TEXT[m.trade.stage] ?? m.trade.stage} (${fmtDuration(now - m.trade.stageSince)}, ${fmtDuration(now - m.trade.since)} in all)`));
    } else {
        head.append(el('span', 'none', 'no trade in progress'));
    }
    box.append(head);

    // Throughput, in tools/mule-throughput.py's own terms: rates from the fleet's trade record,
    // busy time and cycle timing from the mule's own live measurements. Demand (donors
    // announcing or trading) is the mule's own live count.
    const h = m.history;
    const tm = m.timing ?? {};
    const win = h ? h.windowMs : m.windowMs;
    const winText = win >= 3600000 - 1000 ? 'last hour' : `last ${fmtDuration(win)}`;
    const donors = perHour(m.donorsHour, m.windowMs);
    const tiles = el('div', 'tiles');
    const tile = (label, value, title) => {
        const t = el('div', 'tile');
        t.title = title;
        t.append(el('b', '', value), label);
        tiles.append(t);
    };
    if (h) {
        const done = perHour(h.completedHour, win);
        tile('done / h', done.toFixed(1), `${h.completedHour} completed in the ${winText}; ${h.completed} in the trade record`);
        tile('failed / h', perHour(h.failedHour, win).toFixed(1), `${h.failedHour} failed in the ${winText}; ${h.failed} in the trade record`);
        tile('success', h.successRate < 0 ? '-' : `${Math.round(h.successRate * 100)}%`, `completed / (completed + failed), ${winText}`);
        tile('busy', m.windowMs > 0 ? `${Math.round(((tm.busyMs ?? 0) / m.windowMs) * 100)}%` : '-', "time inside a trade or banking its items, the throughput tool's 'busy %'; high with donors waiting means the mule is at capacity");
        tile('donors / h', donors.toFixed(1), 'distinct donors that announced a hand-off or traded: demand');
        tile('demand ÷ supply', done > 0 ? `${(donors / done).toFixed(1)}x` : '-', 'donors per hour over trades per hour: about how many mules would keep up');
        box.append(tiles);
        const cyc = el('dl', 'kv');
        kv(cyc, 'cycle (median)', tm.cycles ? `${fmtDuration(tm.negotiateMedianMs + tm.bankMedianMs)} over ${tm.cycles}: request→accept ${fmtDuration(tm.negotiateMedianMs)} (the donor drawing its offer), accept→banked ${fmtDuration(tm.bankMedianMs)} (the mule banking)` : 'no completed trade timed yet');
        kv(cyc, 'received', `${fmtCount(h.stacksReceived)} stacks in ${fmtCount(h.completed)} trades`);
        box.append(cyc);
        const received = h.received ?? [];
        box.append(el('h3', '', `Items received · ${winText}`));
        box.append(received.length ? plain(['Item', 'Count'], itemRows(received)) : el('div', 'none', 'none yet'));
    } else {
        tile('donors / h', donors.toFixed(1), 'distinct donors that announced a hand-off or traded: demand');
        tile('busy', m.windowMs > 0 ? `${Math.round(((tm.busyMs ?? 0) / m.windowMs) * 100)}%` : '-', "time inside a trade or banking its items, the throughput tool's 'busy %'");
        box.append(tiles, el('div', 'none', "trade history (rates, success, declines, items, hand-outs) comes from the fleet's mule trade record, which this fleet does not keep"));
    }

    const incoming = m.incoming ?? [];
    box.append(el('h3', '', `On the way · ${incoming.length}`));
    if (incoming.length) {
        const list = el('div', 'mule-incoming');
        for (const i of incoming) {
            const chip = el('span', 'mule-chip', `${i.name} · ${ago(now, i.since)}`);
            chip.title = `${i.name} announced a hand-off ${ago(now, i.since)}`;
            list.append(chip);
        }
        box.append(list);
    } else {
        box.append(el('div', 'none', 'no donor has announced a hand-off'));
    }

    if (m.timeSplit && Object.keys(m.timeSplit).length) {
        box.append(el('h3', '', `Time split · ${winText}`), stackedBar(m.timeSplit, tip, m.account), legend([m.timeSplit]));
    }

    if (h) {
        const reasons = h.reasons ?? [];
        const failed = reasons.reduce((s, r) => s + r.count, 0);
        const engaged = h.completedHour + h.failedHour;
        box.append(el('h3', '', `Declines by reason · ${winText}`));
        box.append(reasons.length
            ? plain(['Reason', 'Count', 'Of all trades'], reasons.map(r => [r.reason || '?', fmtCount(r.count), engaged ? `${Math.round((r.count / engaged) * 100)}%` : '-']))
            : el('div', 'none', failed ? '' : 'no trade failed in this window'));
        box.append(el('h3', '', 'Last failures'));
        const failures = h.failures ?? [];
        box.append(failures.length
            ? table(['Partner', 'When', 'Reason'], failures.map(f => [f.partner || '?', ago(now, f.at), f.reason]))
            : el('div', 'none', 'none'));
    }

    const st = m.stock;
    box.append(el('h3', '', st ? `Stock · bank read ${ago(now, st.updated)}` : 'Stock'));
    if (st) {
        const grid = el('div', 'mule-stock');
        const col = (title, rows, empty) => {
            const c = el('div');
            c.append(el('div', 'none', title));
            c.append(rows.length ? plain(['Item', 'Count'], rows) : el('div', 'none', empty));
            return c;
        };
        const rawTotal = (st.raw ?? []).reduce((s, i) => s + i.count, 0);
        const cookedTotal = (st.cooked ?? []).reduce((s, i) => s + i.count, 0);
        grid.append(
            col(`Raw fish · ${fmtCount(rawTotal)}`, itemRows(st.raw), 'none'),
            col(`Cooked fish · ${fmtCount(cookedTotal)}`, itemRows(st.cooked), 'none'),
            col(`Hand-out stock · coins ${fmtCount(st.coins)}`, itemRows(st.catalog), 'no catalogue')
        );
        box.append(grid);
    } else {
        box.append(el('div', 'none', 'no bank read yet — the stock shows once the mule has opened its bank'));
    }

    if (h) {
        const outs = h.handouts ?? [];
        box.append(el('h3', '', `Hand-outs · ${fmtCount(h.handoutsTotal)} given${h.handoutsFailed ? `, ${fmtCount(h.handoutsFailed)} failed` : ''}`));
        box.append(outs.length
            ? table(['To', 'Item', 'Count', 'When'], outs.map(o => [o.to, o.item, fmtCount(o.count), ago(now, o.at)]))
            : el('div', 'none', 'none yet'));
    }
    return box;
}

export default {
    type: 'mule',
    title: 'Mule dashboard',
    description: 'Each mule: donors on the way, the trade in progress and its stage, trades per hour and success rate, failure reasons, time split, stock and hand-outs.',
    size: { w: 5, h: 6 },
    accounts: 'none',
    settings: [],
    create(body, ctx) {
        const wrap = el('div', 'mule-wrap');
        const note = el('div', 'none');
        const list = el('div');
        wrap.append(note, list);
        body.append(wrap);
        const tip = tooltip(wrap);
        let data = null;
        let status = '';
        let at = 0;
        let busy = false;
        let closed = false;

        async function load() {
            busy = true;
            at = Date.now();
            try {
                const res = await fetch('api/mule', { cache: 'no-store' });
                if (closed) return;
                if (res.status === 501) status = 'this fleet does not report mule status';
                else if (!res.ok) status = `could not load the mule status (${res.status})`;
                else { data = await res.json(); status = ''; }
            } catch {
                status = 'could not reach the control panel';
            } finally {
                busy = false;
                if (!closed) draw();
            }
        }

        function draw() {
            note.textContent = status;
            list.textContent = '';
            tip.hide();
            if (!data) {
                if (!status) note.textContent = 'loading…';
                return;
            }
            if (!data.mules.length) {
                note.textContent = 'no MuleReceiver is running';
                ctx.setSub('no mule');
                return;
            }
            for (const m of data.mules) list.append(drawMule(m, data.now, tip));
            const m = data.mules[0];
            const tally = m.history ? ` · ${m.history.completedHour} done, ${m.history.failedHour} failed this hour` : '';
            ctx.setSub(data.mules.length === 1 ? `${m.account} · ${m.phase || '-'}${tally}` : `${data.mules.length} mules`);
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock')) return;
                if (!busy && Date.now() - at >= REFRESH_MS) load();
            },
            destroy() { closed = true; }
        };
    }
};
