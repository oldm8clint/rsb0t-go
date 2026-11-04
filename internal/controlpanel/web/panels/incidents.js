const LOW_HP_FRACTION = 0.25;
export function classify(a) {
    const out = [];
    if (a.dead) {
        out.push({ sev: 'crit', text: 'died' });
    }
    if (a.hpKnown && a.maxHp > 0 && a.hp / a.maxHp <= LOW_HP_FRACTION && !a.dead) {
        out.push({ sev: 'crit', text: `low HP: ${a.hp}/${a.maxHp}` });
    }
    if (a.botState === 'error') {
        out.push({ sev: 'crit', text: `bot stopped with an error${a.botError ? `: ${a.botError}` : ''}` });
    }
    // Only the TERMINAL random-event failures. Treating everything that is not "cleared" as
    // a failure is a mistake this codebase has already made and already fixed once, on the Go
    // side, on 2026-09-20: see internal/fleet.parseRandomEvent and internal/controlpanel's
    // stats.go, whose doc comments record that it made the dashboard report 17,802 failed
    // random events against 6,963 cleared. This file was missed by that sweep, and on a
    // HEALTHY 269-account fleet on 2026-09-21 it was raising 217 criticals of which about 214
    // were wrong: 103 whirlpool retries, and 116 "not_ours".
    //
    //   - not_ours means the guardian checked and the event belongs to ANOTHER player
    //     (randomevents/speech_owner.go). That is a correct decision, the commonest outcome
    //     on a crowded fleet, and the opposite of trouble.
    //   - retry means the guardian released the account to try the SAME event again. One
    //     stubborn event logs it many times and still clears on a later attempt.
    //
    // Neither belongs in a panel whose whole promise is "a short, prioritized list of
    // accounts with real trouble ... instead of scrolling the full log to find them" — a
    // death or a bot error cannot be seen in 217 rows of routine.
    //
    // The two terminal outcomes are not equally bad, so they do not share a severity.
    //
    //   - error is a HANDLER failure: code went wrong, and nothing is going to retry it.
    //   - gave_up is the guardian's own designed escalation — it exhausted its attempts and
    //     is ignoring that event for a cooldown, and the account usually carries straight on.
    //     Watched live 2026-09-21: the single gave_up on the fleet (acct45, a Genie) was
    //     back at the Fishing Guild catching fish four minutes later. That is n=1 and is not
    //     a claim that a give-up is always harmless — an account genuinely trapped in a maze
    //     or a mime looks exactly like this too, which is why it still appears — but it does
    //     not belong in the same row as "died".
    const lastEvent = (a.randomEvents ?? [])[0];
    if (lastEvent && lastEvent.outcome === 'error') {
        out.push({ sev: 'crit', text: `random event "${lastEvent.name}" failed with a handler error` });
    } else if (lastEvent && lastEvent.outcome === 'gave_up') {
        out.push({ sev: 'warn', text: `gave up on random event "${lastEvent.name}" — check it is not stuck in one` });
    }
    if (a.state === 'error') {
        out.push({ sev: 'warn', text: a.lastError ? `login/connection error: ${a.lastError}` : 'in an error state' });
    } else if (a.state === 'reconnecting') {
        out.push({ sev: 'warn', text: `reconnecting (attempt ${a.reconnects || 1})` });
    } else if (a.state === 'offline' && !a.manuallyDisconnected) {
        out.push({ sev: 'warn', text: 'disconnected unexpectedly' });
    }
    if (a.watchdog?.state === 'needs_attention' || a.watchdog?.state === 'stalled') {
        out.push({ sev: a.watchdog.state === 'needs_attention' ? 'crit' : 'warn',
            text: `watchdog ${a.watchdog.state}: ${a.watchdog.detail || a.watchdog.step || 'check progress'}` });
    }
    if (!out.length && a.lastError) {
        out.push({ sev: 'info', text: a.lastError });
    }
    return out;
}

// Exact error text is preserved: do not collapse unrelated failures by stripping numbers.
export function incidentKey(alert) {
    return `${alert.sev}|${alert.text.startsWith('low HP:') ? 'low HP' : alert.text.startsWith('reconnecting (attempt ') ? 'reconnecting' : alert.text}`;
}
export function groupIncidents(accounts, includeInfo = false) {
    const groups = new Map();
    for (const a of accounts) for (const alert of classify(a)) {
        if (alert.sev === 'info' && !includeInfo) continue;
        const key = incidentKey(alert);
        if (!groups.has(key)) groups.set(key, {key, sev: alert.sev, title: key.slice(key.indexOf('|') + 1), members: []});
        groups.get(key).members.push({account: a, alert});
    }
    return [...groups.values()].sort((a,b) => ({crit:0,warn:1,info:2}[a.sev] - {crit:0,warn:1,info:2}[b.sev]) || b.members.length - a.members.length || a.title.localeCompare(b.title));
}

export function dataFresh(store, now = Date.now()) {
    return store.conn === 'live' && store.statusReceivedAt > 0 && now - store.statusReceivedAt < 15000;
}
