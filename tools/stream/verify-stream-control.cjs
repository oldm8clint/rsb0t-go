// node tools/stream/verify-stream-control.cjs PANEL_URL ACCOUNT [MULE_ACCOUNT]
//
// The SERVER's input-control rules (internal/controlpanel/streaminput.go), exercised against
// a panel YOU run with -stream-input on the LOCAL LAB ENGINE. Loopback only, and it refuses
// anything else: taking control stops the account's bot and sends a real walk to the game.
//
// This is the complement to tools/stream/verify-stream-input.cjs, which drives the browser,
// the panel and the Load button. The rules proven here are server-side and have nothing to
// draw, so putting them through a headless browser would only add a way to fail:
//
//   1. Taking control of an account whose BOT IS RUNNING stops that bot first, and control is
//      acknowledged only after the bot reports stopped. The browser fixture could not show
//      this, because the lab accounts it ran against had no bot (its botBefore/During/After
//      were all empty) — this one starts a real bot first so the path is actually taken.
//   2. A validated action reaches the game while control is held (the account's own tile
//      changes), and is audit-logged.
//   3. RELEASING restarts the remembered bot, and the audit says which one.
//   4. An account running MuleReceiver is REFUSED without force and accepted with it. Mules
//      are the one script where a careless takeover costs other accounts their hand-offs.
//   5. The audit ring records every request, accepted or refused.
//
// It leaves the fleet as it found it: whatever bot each account was running before is
// running again at the end, and anything this script started is stopped.
const BASE = process.argv[2];
const ACCOUNT = process.argv[3];
const MULE = process.argv[4] || '';
// Any registered script with no required settings will do for the pause/resume path: what is
// being proven is the transition running -> stopped -> running, not what the bot does.
const FILLER_SCRIPT = process.env.STREAM_BOT || 'BoneBurier';

const sleep = ms => new Promise(r => setTimeout(r, ms));
const failures = [];
const check = (ok, what) => { if (!ok) failures.push(what); return ok; };

async function api(path, body) {
    const res = await fetch(new URL(path, BASE), body === undefined
        ? undefined
        : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const json = await res.json().catch(() => ({}));
    return { status: res.status, json };
}
const statusOf = async name => (await api('/api/status')).json.accounts.find(a => a.name === name) || {};
const auditSince = async at => ((await api('/api/stream/audit')).json.entries || [])
    .filter(e => e.at >= at).map(e => `${e.request} ${e.detail || ''} -> ${e.result}`.replace(/\s+/g, ' ').trim());

// waitFor polls a predicate rather than sleeping a guessed interval: a bot reports "stopped"
// when it actually stopped, which is the whole point of the acknowledgement rule.
async function waitFor(what, predicate, ms = 20000) {
    const until = Date.now() + ms;
    for (;;) {
        const value = await predicate();
        if (value) return value;
        if (Date.now() > until) { failures.push(`timed out waiting for ${what}`); return null; }
        await sleep(400);
    }
}

// A view lease, then control. Returns the lease so the caller can act and release.
async function openView(account) {
    const open = await api('/api/stream/open', { account });
    if (!check(open.status === 200, `/api/stream/open ${account} = ${open.status} ${open.json.error || ''}`)) return null;
    return open.json;
}

(async () => {
    if (!BASE || !ACCOUNT) throw Error('usage: verify-stream-control.cjs PANEL_URL ACCOUNT [MULE_ACCOUNT]');
    const host = new URL(BASE).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') throw Error('loopback panels only: this sends real actions and stops bots');
    const meta = (await api('/api/meta')).json.streamClient;
    if (!meta?.sessionInput) throw Error('this panel does not advertise sessionInput (start it with -stream-input)');
    const started = Date.now();
    const result = { account: '[measured]', filler: FILLER_SCRIPT };
    const restore = [];

    try {
        // ---- 1-3. bot stopped on take, action lands, bot resumed on release ----
        const was = await statusOf(ACCOUNT);
        result.botBefore = [was.bot || '', was.botState || null];
        // An account can carry a script name while its bot is stopped or in error, and
        // /api/bot/start refuses only when one is actually RUNNING. What this path needs is a
        // running bot to pause, so the test is on the state, not on whether a name is set.
        if (was.botState !== 'running') {
            const script = was.bot || FILLER_SCRIPT;
            const start = await api('/api/bot/start', { account: ACCOUNT, script });
            check(start.status === 200, `could not start ${script} on ${ACCOUNT}: ${start.status} ${start.json.error || ''}`);
            restore.push(async () => {
                await api('/api/bot/stop', { account: ACCOUNT });
                if (was.botState === 'running' && was.bot) await api('/api/bot/start', { account: ACCOUNT, script: was.bot });
            });
            await waitFor(`${script} to start`, async () => (await statusOf(ACCOUNT)).botState === 'running');
        }
        const running = await statusOf(ACCOUNT);
        check(running.botState === 'running', `no bot is running on ${ACCOUNT} to pause: ${JSON.stringify([running.bot, running.botState])}`);
        result.botRunningBeforeControl = [running.bot || '', running.botState || null];

        const view = await openView(ACCOUNT);
        if (!view) throw Error('could not take a view lease');
        const take = await api('/api/stream/input', { session: view.session, enabled: true });
        result.take = { status: take.status, acknowledged: take.json.acknowledged, pausedBot: take.json.pausedBot || '' };
        check(take.status === 200 && take.json.acknowledged === true, `take control = ${take.status} ${JSON.stringify(take.json)}`);
        // The rule is not "the stop was requested" but "control was acknowledged only after the
        // bot reported stopped", so the state is read straight after the acknowledgement.
        const paused = await statusOf(ACCOUNT);
        check(paused.botState === 'stopped', `bot state after take control = ${JSON.stringify(paused.botState)}, want "stopped"`);
        check(take.json.pausedBot === running.bot, `take control reported pausedBot ${JSON.stringify(take.json.pausedBot)}, want ${JSON.stringify(running.bot)}`);
        result.botDuringControl = [paused.bot || '', paused.botState || null];

        // 2. a validated action reaches the game. A frame first: an action must be decided
        // from a recent observation, and it carries the tile to walk from.
        const frame = await api(`/api/stream/frame?session=${encodeURIComponent(view.session)}`);
        const snap = frame.json.snapshot || {};
        result.from = { x: snap.x, z: snap.z, level: snap.level };
        // Four directions, not one. The server validates a walk against range and level and
        // then hands the route to the game, which is free to refuse a target behind a wall —
        // and a bot can leave the account anywhere, including a cellar three tiles wide. A
        // single fixed offset therefore measures the account's surroundings, not the input
        // path. Each attempt is spaced past the server's own one-action-per-300ms rule.
        const attempts = [];
        let moved = null;
        for (const [dx, dz] of [[0, -3], [0, 3], [-3, 0], [3, 0]]) {
            const f = await api(`/api/stream/frame?session=${encodeURIComponent(view.session)}`);
            const at = f.json.snapshot || snap;
            const act = await api('/api/stream/action', { session: view.session, epoch: f.json.epoch, seq: f.json.seq,
                action: { kind: 'walk', x: at.x + dx, z: at.z + dz } });
            attempts.push({ d: [dx, dz], status: act.status, error: act.json.error || '' });
            if (act.status !== 200) { await sleep(400); continue; }
            moved = await waitFor(`the account to move ${dx},${dz}`, async () => {
                const g = await api(`/api/stream/frame?session=${encodeURIComponent(view.session)}`);
                const t = g.json.snapshot;
                return t && (t.x !== at.x || t.z !== at.z) ? { x: t.x, z: t.z, level: t.level } : null;
            }, 6000);
            if (moved) break;
            failures.pop(); // that direction's timeout is not a failure while others remain
            await sleep(400);
        }
        result.action = attempts;
        check(attempts.some(a => a.status === 200), `every walk was refused by the server: ${JSON.stringify(attempts)}`);
        result.to = moved;
        check(!!moved, `the account did not move after a walk the server accepted, in any of four directions from ${JSON.stringify(result.from)}`);

        // 3. release resumes the remembered bot
        const release = await api('/api/stream/input', { session: view.session, enabled: false });
        check(release.status === 200, `release = ${release.status} ${release.json.error || ''}`);
        const resumed = await waitFor(`${running.bot} to resume`, async () => (await statusOf(ACCOUNT)).botState === 'running');
        const after = await statusOf(ACCOUNT);
        result.botAfterRelease = [after.bot || '', after.botState || null];
        check(!!resumed && after.bot === running.bot, `bot after release = ${JSON.stringify([after.bot, after.botState])}, want ${JSON.stringify(running.bot)} running`);
        await api('/api/stream/close', { session: view.session });

        // ---- 4. MuleReceiver is refused without force ----
        if (MULE) {
            const muleWas = await statusOf(MULE);
            if (muleWas.bot !== 'MuleReceiver') {
                const start = await api('/api/bot/start', { account: MULE, script: 'MuleReceiver' });
                check(start.status === 200, `could not start MuleReceiver on ${MULE}: ${start.status} ${start.json.error || ''}`);
                restore.push(async () => {
                    await api('/api/bot/stop', { account: MULE });
                    if (muleWas.bot) await api('/api/bot/start', { account: MULE, script: muleWas.bot });
                });
                // ASSIGNED, not running. The refusal is keyed on the account's script name
                // (streaminput.go: `status.Bot == "MuleReceiver"`), which is deliberate and
                // worth stating: a mule whose bot has crashed is still the account every
                // donor is trading to, so it still must not be seized without force. On a
                // lab account with no mule settings the bot reaches "error" within a second
                // and never runs, and the refusal below fires exactly the same.
                await waitFor('MuleReceiver to be assigned', async () => (await statusOf(MULE)).bot === 'MuleReceiver');
            }
            const muleView = await openView(MULE);
            if (muleView) {
                const muleNow = await statusOf(MULE);
                result.muleBotState = [muleNow.bot || '', muleNow.botState || null];
                const soft = await api('/api/stream/input', { session: muleView.session, enabled: true });
                result.muleWithoutForce = { status: soft.status, error: soft.json.error || '' };
                check(soft.status === 409, `taking a mule without force = ${soft.status}, want 409`);
                check(/mule/i.test(soft.json.error || ''), `the mule refusal does not say why: ${JSON.stringify(soft.json.error)}`);
                const forced = await api('/api/stream/input', { session: muleView.session, enabled: true, force: true });
                result.muleWithForce = { status: forced.status, acknowledged: forced.json.acknowledged, pausedBot: forced.json.pausedBot || '' };
                check(forced.status === 200 && forced.json.acknowledged === true, `taking a mule WITH force = ${forced.status} ${JSON.stringify(forced.json)}`);
                const muleRelease = await api('/api/stream/input', { session: muleView.session, enabled: false });
                check(muleRelease.status === 200, `releasing the mule = ${muleRelease.status} ${muleRelease.json.error || ''}`);
                await api('/api/stream/close', { session: muleView.session });
            }
        }

        // ---- 5. the audit recorded all of it ----
        result.audit = await auditSince(started);
        check(result.audit.some(l => /^take control .*accepted/.test(l)), 'the audit has no accepted take control');
        check(result.audit.some(l => /^action walk .*sent/.test(l)), 'the audit has no sent walk action');
        check(result.audit.some(l => /^release .*released/.test(l)), 'the audit has no release');
        if (MULE) check(result.audit.some(l => /refused: MuleReceiver/.test(l)), 'the audit has no MuleReceiver refusal');
    } finally {
        for (const undo of restore.reverse()) { try { await undo(); } catch (_) { /* best effort */ } }
    }

    result.failures = failures;
    console.log(JSON.stringify(result, null, 1));
    if (failures.length) { console.error(failures.join('\n')); process.exitCode = 1; }
})().catch(error => { console.error(error); process.exitCode = 1; });
