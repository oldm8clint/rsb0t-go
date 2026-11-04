// Shared pieces for the panels that show where time goes (timesplit.js, mule.js): one fixed
// colour per phase, a stacked percentage bar with a legend, a Gantt strip and a hover tooltip.
//
// Colour follows the phase, never its rank: every phase maps to one of eight categorical slots
// (the validated dataviz reference palette, stepped for a dark surface: checked against this
// page's --stone with scripts/validate_palette.js, all checks pass in this order) or to one of
// four greys for "no work" states. Stacks are drawn in slot order, so neighbours are always the
// validated adjacent pairs. The colours themselves live in style.css (--ph-*).
//
// Two phases share a slot only where they never share one account's bar (a fisher's "fishing"
// and the mule's "trading" are both the account's main job); a bar that does mix them — the
// fleet-wide one — merges them into one segment labelled with both names.

import { el, fmtDuration } from './util.js';

const SLOT = {
    fishing: 1, trading: 1,
    banking: 2,
    walking: 3,
    handoff: 4, handout: 4,
    event: 5,
    recovering: 6, cooking: 6,
    shopping: 7, setup: 7,
    stranded: 8
};
const NEUTRAL = ['idle', 'unknown', 'no script', 'offline'];

/** The colour key of a phase: a slot number, or a neutral's own name. Unknown phase names are a
 * script's own work phase ("mining", "chopping") and share slot 1. */
export function phaseKey(phase) {
    if (NEUTRAL.includes(phase)) return phase;
    return SLOT[phase] ?? 1;
}

function keyRank(key) {
    return typeof key === 'number' ? key : 10 + NEUTRAL.indexOf(key);
}

export function phaseColor(phase) {
    const key = phaseKey(phase);
    return `var(--ph-${typeof key === 'number' ? key : key.replace(' ', '')})`;
}

/** A shared tooltip that follows the pointer inside one panel body. */
export function tooltip(host) {
    const tip = el('div', 'ph-tip');
    tip.hidden = true;
    host.append(tip);
    return {
        show(ev, text) {
            tip.textContent = text;
            tip.hidden = false;
            const r = host.getBoundingClientRect();
            let x = ev.clientX - r.left + host.scrollLeft + 12;
            const y = ev.clientY - r.top + host.scrollTop + 14;
            if (x + tip.offsetWidth > host.scrollLeft + host.clientWidth) x = Math.max(0, x - tip.offsetWidth - 24);
            tip.style.left = `${x}px`;
            tip.style.top = `${y}px`;
        },
        hide() { tip.hidden = true; }
    };
}

/** Groups {phase: ms} by colour key, in stack order. Each group: { key, names, ms }. */
export function groupPhases(msByPhase) {
    const groups = new Map();
    for (const [phase, ms] of Object.entries(msByPhase)) {
        if (!(ms > 0)) continue;
        const key = phaseKey(phase);
        const g = groups.get(key) ?? { key, names: [], ms: 0, color: phaseColor(phase) };
        g.names.push(phase);
        g.ms += ms;
        groups.set(key, g);
    }
    return [...groups.values()].sort((a, b) => keyRank(a.key) - keyRank(b.key));
}

/**
 * A 100% stacked bar of {phase: ms}, with direct percentage labels on segments wide enough to
 * hold one and a hover tooltip on every segment. Returns the bar element.
 */
export function stackedBar(msByPhase, tip, label) {
    const bar = el('div', 'phasebar');
    const groups = groupPhases(msByPhase);
    const total = groups.reduce((s, g) => s + g.ms, 0);
    if (total <= 0) {
        bar.classList.add('empty');
        bar.textContent = 'no samples yet';
        return bar;
    }
    for (const g of groups) {
        const share = g.ms / total;
        const seg = el('div', 'phaseseg');
        seg.style.flexGrow = String(share);
        seg.style.background = g.color;
        const pct = `${Math.round(share * 100)}%`;
        if (share >= 0.08) seg.append(el('span', '', pct));
        const text = `${label ? label + ': ' : ''}${g.names.join(' / ')} ${(share * 100).toFixed(1)}% (${fmtDuration(g.ms)})`;
        seg.setAttribute('aria-label', text);
        seg.addEventListener('pointermove', ev => tip.show(ev, text));
        seg.addEventListener('pointerleave', () => tip.hide());
        bar.append(seg);
    }
    return bar;
}

/** A legend of every phase present in any of the given splits, in stack order. */
export function legend(splits) {
    const merged = {};
    for (const s of splits) for (const [p, ms] of Object.entries(s)) merged[p] = (merged[p] ?? 0) + ms;
    const box = el('div', 'phaselegend');
    for (const g of groupPhases(merged)) {
        const item = el('span', 'phasekey');
        const sw = el('i');
        sw.style.background = g.color;
        item.append(sw, g.names.join(' / '));
        box.append(item);
    }
    return box;
}

/**
 * A Gantt strip of one account's segments between from and to, with time ticks under it.
 * segments: [{ phase, start, end }] in ms. events, optional, is the random-event overlay in the
 * same shape with the event's name as its phase: drawn as a labelled band above the strip.
 */
export function gantt(segments, from, to, tip, nowText, events) {
    const wrap = el('div', 'gantt');
    const strip = el('div', 'gantt-strip');
    const span = Math.max(1, to - from);
    if (events && events.length) {
        const band = el('div', 'gantt-events');
        for (const e of events) {
            const left = (Math.max(e.start, from) - from) / span;
            const width = (Math.min(e.end, to) - Math.max(e.start, from)) / span;
            if (width <= 0) continue;
            const mark = el('div', 'gantt-event');
            mark.style.left = `${left * 100}%`;
            mark.style.width = `max(3px, ${width * 100}%)`;
            if (width >= 0.06) mark.append(el('span', '', e.phase));
            const text = `random event: ${e.phase} ${fmtDuration(e.end - e.start)} (${new Date(e.start).toTimeString().slice(0, 8)} to ${new Date(e.end).toTimeString().slice(0, 8)})`;
            mark.setAttribute('aria-label', text);
            mark.addEventListener('pointermove', ev => tip.show(ev, text));
            mark.addEventListener('pointerleave', () => tip.hide());
            band.append(mark);
        }
        wrap.append(band);
    }
    for (const s of segments) {
        const left = (Math.max(s.start, from) - from) / span;
        const width = (Math.min(s.end, to) - Math.max(s.start, from)) / span;
        if (width <= 0) continue;
        const seg = el('div', 'gantt-seg');
        seg.style.left = `${left * 100}%`;
        seg.style.width = `max(1px, calc(${width * 100}% - 1px))`;
        seg.style.background = phaseColor(s.phase);
        const text = `${s.phase} ${fmtDuration(s.end - s.start)} (${new Date(s.start).toTimeString().slice(0, 8)} to ${new Date(s.end).toTimeString().slice(0, 8)})`;
        seg.setAttribute('aria-label', text);
        seg.addEventListener('pointermove', ev => tip.show(ev, text));
        seg.addEventListener('pointerleave', () => tip.hide());
        strip.append(seg);
    }
    const axis = el('div', 'gantt-axis');
    const minutes = Math.round(span / 60000);
    const steps = 4;
    for (let i = 0; i <= steps; i++) {
        const t = el('span', '', i === steps ? (nowText || 'now') : `-${Math.round(minutes * (1 - i / steps))}m`);
        t.style.left = `${(i / steps) * 100}%`;
        axis.append(t);
    }
    wrap.append(strip, axis);
    return wrap;
}
