import { anyOf, button, el, fmtCount, fmtDuration, kv, logLine, serverNow, statePill } from '../util.js';
import { iconFor, itemIconWrap } from '../itemicons.js';
import { drawSkillsGrid } from '../skillrender.js';

const INVENTORY_EVERY_MS = 2000;

export default {
    type: 'account',
    title: 'Account detail',
    description: 'Everything one account reports: state, counters, what it carries, its latest log lines, and — when the fleet allows it — connect/disconnect plus start/stop and settings for its script. For the full Skills tab on its own, use the dedicated Skills panel instead of turning this one on for every account.',
    size: { w: 5, h: 5 },
    accounts: 'one',
    settings: [
        { key: 'lines', label: 'Log lines shown', kind: 'number', min: 0, max: 200, step: 1, default: 20 },
        { key: 'skills', label: 'Show skills here too', kind: 'bool', default: false },
        { key: 'counters', label: 'Show counters', kind: 'bool', default: true },
        { key: 'inventories', label: 'Show inventories', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const info = el('dl', 'kv');
        const skillHead = el('h3', '', 'Skills');
        const skills = el('div', 'skillsgrid');
        const countersHead = el('h3', '', 'Counters');
        const counters = el('dl', 'kv');
        const connHead = el('h3', '', 'Connection');
        const connControl = el('div', 'control');
        const disconnectBtn = button('Disconnect', 'log this account out cleanly');
        const connectBtn = button('Reconnect', 'log this account back in');
        const connNote = el('div', 'none');
        connControl.append(disconnectBtn, connectBtn, connNote);
        const controlHead = el('h3', '', 'Script');
        const control = el('div', 'control');
        const scriptPick = el('select');
        const startBtn = button('Start', 'start the picked script on this account, with the settings below');
        const stopBtn = button('Stop', 'stop the running script on this account');
        // Restart: stop and start again with this account's CURRENTLY RUNNING code and
        // settings unchanged — recovers a genuinely stuck script. It does NOT pick up a
        // newer commit: the code that runs after this is whatever is already compiled
        // into this controlpanel process. Applying a newer commit needs a full rebuild
        // and a restart of the whole process (every account on it, not just this one)
        // — see the Script versions panel for which scripts, if any, have one waiting.
        const restartBtn = button('Restart script', 'stop and start this account’s script again with its current settings unchanged — recovers a stuck script. This reloads only the code ALREADY RUNNING in this process; it does not fetch or apply a newer commit. Picking up new code needs a full rebuild and a restart of the whole controlpanel process (see the Script versions panel).');
        const controlNote = el('div', 'none');
        control.append(scriptPick, startBtn, stopBtn, restartBtn, controlNote);
        // Mule account: a user-friendly stand-in for hand-editing the settings JSON's own
        // "mule" key — the account owner's own ask (2026-09-14): "trade the mule or any
        // given mule account configurable in the script config which should be
        // userfriendly." Kept in sync with settingsBox both ways (see muleFieldToSettings/
        // the settingsFor-gated load below) rather than becoming a second source of truth.
        const muleFieldWrap = el('label', 'mulefield');
        muleFieldWrap.append('Mule account: ');
        const muleField = el('input');
        muleField.type = 'text';
        muleField.placeholder = 'mule';
        muleField.title = 'the MuleReceiver account this script hands its bank off to — same as the settings JSON’s "mule" key, edited here instead of by hand';
        muleFieldWrap.append(muleField);
        const settingsBox = el('textarea', 'settingsbox');
        settingsBox.spellcheck = false;
        settingsBox.placeholder = '{ "maxLaps": 5 }';
        const applyBtn = button('Apply & restart', 'stop the running script and start it again with the settings above');
        const settingsNote = el('div', 'none');
        const settingsControl = el('div', 'control');
        settingsControl.append(applyBtn, settingsNote);
        // Mule hand-off: the account owner's own ask (2026-09-14) for a manual "hand off
        // now" trigger plus a live-editable muleAt threshold, reachable from the panel
        // rather than only a fleet.config.json edit + restart — see
        // internal/controlpanel/source.go's HandoffControl and server.go's
        // /api/bot/handoff, /api/bot/muleat.
        const handoffHead = el('h3', '', 'Mule hand-off');
        const handoffControl = el('div', 'control');
        const handoffBtn = button('Hand off now', 'ask this account’s script to start a mule hand-over at its next check, regardless of its own threshold');
        const muleAtInput = el('input');
        muleAtInput.type = 'number';
        muleAtInput.min = '0';
        muleAtInput.step = '1';
        muleAtInput.placeholder = 'muleAt';
        muleAtInput.title = 'live-override the banked-item count that starts a hand-over; 0 or blank clears the override';
        const muleAtBtn = button('Set threshold', 'apply the muleAt override above to this running script');
        const handoffNote = el('div', 'none');
        handoffControl.append(handoffBtn, muleAtInput, muleAtBtn, handoffNote);
        const invHead = el('h3', '', 'Inventories');
        const inv = el('div');
        const logHead = el('h3', '', 'Log');
        const log = el('div', 'feed');
        body.append(info, skillHead, skills, countersHead, counters, connHead, connControl, controlHead, control, muleFieldWrap, settingsBox, settingsControl, handoffHead, handoffControl, invHead, inv, logHead, log);
        skillHead.hidden = true;
        invHead.hidden = true;
        controlHead.hidden = true;
        connHead.hidden = true;
        muleFieldWrap.hidden = true;
        settingsBox.hidden = true;
        settingsControl.hidden = true;
        handoffHead.hidden = true;
        handoffControl.hidden = true;
        let logKey = '';
        let scannedLogKey = '';
        let infoKey = '', countersKey = '', inventoryKey = '';
        let durationCell = null, retryCell = null;
        let invName = '';
        let invAt = 0;
        let invBusy = false;
        let closed = false;
        let busy = false;
        let connBusy = false;
        let scriptsDrawn = '';
        let settingsFor = '';

        // post sends one POST and reports the result into note, returning whether it
        // succeeded. It does not manage any busy flag itself — applyBtn's handler needs
        // busy held across two calls (stop, then start), so each call site sets/clears
        // its own flag around post() rather than post() owning that.
        async function post(path, payload, okText, note) {
            note.textContent = '';
            note.classList.remove('bad');
            try {
                const res = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
                const data = await res.json().catch(() => ({}));
                if (res.ok) { note.textContent = okText; return true; }
                note.textContent = data.error || `HTTP ${res.status}`;
                note.classList.add('bad');
                return false;
            } catch (err) {
                note.textContent = `request failed: ${err}`;
                note.classList.add('bad');
                return false;
            }
        }

        // parseSettings reads settingsBox as JSON, treating blank as "no settings" rather
        // than an error — most scripts have every setting optional (see internal/bot/*.go's
        // own doc comments), so an operator who never touches this box should not be
        // blocked from starting a script at all.
        function parseSettings() {
            const text = settingsBox.value.trim();
            if (!text) return {};
            return JSON.parse(text);
        }

        // muleFieldToSettings folds muleField's current value into settingsBox's JSON —
        // the one place this "user friendly" field actually writes back — so Start/Apply
        // send it the same way any other setting already flows, without a second code path
        // on the server for it. Invalid existing JSON is left alone with a note rather than
        // silently discarded; a blank field removes the "mule" key entirely so the script's
        // own default ("mule") applies.
        muleField.addEventListener('change', () => {
            let settings;
            try {
                settings = parseSettings();
            } catch (err) {
                settingsNote.textContent = `settings is not valid JSON, so the mule account field could not be applied: ${err.message}`;
                settingsNote.classList.add('bad');
                return;
            }
            const v = muleField.value.trim();
            if (v) settings.mule = v; else delete settings.mule;
            settingsBox.value = Object.keys(settings).length ? JSON.stringify(settings, null, 2) : '';
        });

        startBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name || !scriptPick.value) return;
            let settings;
            try {
                settings = parseSettings();
            } catch (err) {
                controlNote.textContent = `settings is not valid JSON: ${err.message}`;
                controlNote.classList.add('bad');
                return;
            }
            busy = true;
            await post('api/bot/start', { account: name, script: scriptPick.value, settings }, `starting ${scriptPick.value}...`, controlNote);
            busy = false;
        });
        stopBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name) return;
            busy = true;
            await post('api/bot/stop', { account: name }, 'stop requested...', controlNote);
            busy = false;
        });

        // restartBtn: stop then start again with whatever script/settings this account
        // is ALREADY running (a.bot / a.settings from the store, not scriptPick/
        // settingsBox — an operator should not need to have anything typed in for this
        // to work). See restartBtn's own tooltip above for exactly what this does and
        // does not do; this handler must keep matching that wording.
        restartBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            const a = ctx.store.byName.get(name);
            if (!name || !a || !a.bot) return;
            busy = true;
            controlNote.classList.remove('bad');
            controlNote.textContent = 'stopping...';
            if (!await post('api/bot/stop', { account: name }, '', controlNote)) { busy = false; return; }
            controlNote.textContent = 'waiting for it to stop...';
            const stopped = await waitUntilNotRunning(name, 5000);
            if (!stopped) {
                controlNote.textContent = `${name} did not report stopped within 5s; try Restart again once it has`;
                controlNote.classList.add('bad');
                busy = false;
                return;
            }
            controlNote.textContent = `restarting ${a.bot}...`;
            await post('api/bot/start', { account: name, script: a.bot, settings: a.settings || {} }, `restarted ${a.bot} with its existing settings`, controlNote);
            busy = false;
        });

        // applyBtn stops the running bot and starts it again with settingsBox's current
        // contents — there is no live "change settings on a running bot" mechanism today
        // (internal/bot.Context.Settings is read once at start and never updated), so a
        // settings change means a brief restart, not a hot reload. This polls /api/status
        // directly (rather than the page's own store, which a hidden/background tab may
        // not be refreshing as fast) for the bot to actually leave "running" before
        // starting again, since /api/bot/start refuses to start over one still running.
        applyBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name || !scriptPick.value) return;
            let settings;
            try {
                settings = parseSettings();
            } catch (err) {
                settingsNote.textContent = `settings is not valid JSON: ${err.message}`;
                settingsNote.classList.add('bad');
                return;
            }
            busy = true;
            settingsNote.classList.remove('bad');
            settingsNote.textContent = 'stopping...';
            if (!await post('api/bot/stop', { account: name }, '', settingsNote)) { busy = false; return; }
            settingsNote.textContent = 'waiting for it to stop...';
            const stopped = await waitUntilNotRunning(name, 5000);
            if (!stopped) {
                settingsNote.textContent = `${name} did not report stopped within 5s; try Apply again once it has`;
                settingsNote.classList.add('bad');
                busy = false;
                return;
            }
            settingsNote.textContent = 'starting with the new settings...';
            await post('api/bot/start', { account: name, script: scriptPick.value, settings }, `restarted ${scriptPick.value} with the new settings`, settingsNote);
            busy = false;
        });

        handoffBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name) return;
            await post('api/bot/handoff', { account: name }, 'hand-off requested...', handoffNote);
        });
        muleAtBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name) return;
            const raw = muleAtInput.value.trim();
            const n = raw === '' ? 0 : Number(raw);
            if (!Number.isFinite(n) || n < 0) {
                handoffNote.textContent = 'muleAt must be a whole number, or blank to clear the override';
                handoffNote.classList.add('bad');
                return;
            }
            await post('api/bot/muleat', { account: name, muleAt: Math.trunc(n) }, n > 0 ? `muleAt overridden to ${Math.trunc(n)}` : 'muleAt override cleared', handoffNote);
        });

        async function waitUntilNotRunning(name, timeoutMs) {
            const deadline = Date.now() + timeoutMs;
            while (Date.now() < deadline) {
                try {
                    const res = await fetch('api/status', { cache: 'no-store' });
                    if (res.ok) {
                        const acc = ((await res.json()).accounts ?? []).find(a => a.name === name);
                        if (!acc || acc.botState !== 'running') return true;
                    }
                } catch {
                    // Retried on the next iteration.
                }
                await new Promise(r => setTimeout(r, 250));
            }
            return false;
        }

        disconnectBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name) return;
            connBusy = true;
            await post('api/fleet/disconnect', { account: name }, 'disconnect requested...', connNote);
            connBusy = false;
        });
        connectBtn.addEventListener('click', async () => {
            const name = ctx.accounts[0] || ctx.store.selected;
            if (!name) return;
            connBusy = true;
            connectBtn.disabled = disconnectBtn.disabled = true;
            await post('api/fleet/connect', { account: name }, 'reconnect queued; progress appears in the account state', connNote);
            connBusy = false;
        });

        // itemChip renders one slot as a small icon — a real captured in-game sprite
        // (web/itemicons_png/<id>.png) for the ~60 items this fleet's own scripts actually
        // handle, falling back to itemicons.js's hand-drawn category glyph for anything else,
        // see itemIconWrap's own doc comment — plus the resolved name and count, so "what's
        // incoming" reads at a glance instead of decoding raw ids. Falls back to "#<id>" for the
        // label only when the fleet had no cache archive to resolve a name.
        function itemChip(s) {
            const icon = iconFor(s.name);
            const chip = el('div', `item item-${icon.cls}`);
            const iconWrap = itemIconWrap(s.id, s.name);
            chip.append(iconWrap, el('span', 'item-name', s.name || `#${s.id}`));
            if (s.count > 1) chip.append(el('span', 'item-count', `x${fmtCount(s.count)}`));
            chip.title = `${s.name || `item #${s.id}`}${s.count > 1 ? ` x${fmtCount(s.count)}` : ''} (id ${s.id})`;
            return chip;
        }

        function drawInventories(list) {
            const key = JSON.stringify([invName, list]);
            if (key === inventoryKey) return;
            inventoryKey = key;
            inv.textContent = '';
            if (list.length === 0) {
                inv.append(el('div', 'none', 'nothing the session has seen'));
                return;
            }
            for (const one of list) {
                inv.append(el('div', 'none', `component ${one.id}, ${one.slots.length} slots`));
                const grid = el('div', 'itemgrid');
                for (const s of one.slots) grid.append(itemChip(s));
                inv.append(grid);
            }
        }

        async function loadInventories(name) {
            invBusy = true;
            invAt = Date.now();
            try {
                const res = await fetch(`api/inventory?account=${encodeURIComponent(name)}`, { cache: 'no-store' });
                if (closed || name !== invName) return;
                if (res.ok) drawInventories((await res.json()).inventories ?? []);
                else { inv.textContent = ''; inventoryKey = ''; }
            } catch {
                // Tried again on a later beat.
            } finally {
                invBusy = false;
            }
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'select', 'clock', 'event', 'reset', 'meta')) return;
                const fixed = ctx.accounts[0];
                const name = fixed || store.selected || store.accounts[0]?.name || '';
                ctx.setSub(fixed || !name ? name : `${name}, following the table`);
                const a = store.byName.get(name);
                if (!a) {
                    info.textContent = '';
                    infoKey = ''; countersKey = ''; inventoryKey = '';
                    durationCell = retryCell = null;
                    kv(info, 'account', name ? `${name} is not in the fleet's report` : 'no accounts reported yet');
                    skills.textContent = '';
                    delete skills.dataset.key;
                    counters.textContent = '';
                    log.textContent = '';
                    inv.textContent = '';
                    skillHead.hidden = countersHead.hidden = logHead.hidden = invHead.hidden = controlHead.hidden = connHead.hidden = handoffHead.hidden = handoffControl.hidden = muleFieldWrap.hidden = true;
                    settingsBox.hidden = settingsControl.hidden = true;
                    connControl.hidden = control.hidden = true;
                    logKey = '';
                    settingsFor = '';
                    return;
                }
                const now = serverNow(store);
                const detailKey = JSON.stringify([name, a.state, a.bot, a.botState, a.botError, a.activity, a.pos, a.packets, a.staffLevel, a.reconnects, a.lastError, a.manuallyDisconnected, a.nextRetry > 0]);
                if (detailKey !== infoKey) {
                    infoKey = detailKey; info.replaceChildren(); retryCell = null;
                    kv(info, 'state', statePill(a.state));
                    durationCell = kv(info, 'for', fmtDuration(now - a.since));
                    kv(info, 'bot', a.bot);
                    if (a.botState) kv(info, 'bot state', a.botState);
                    if (a.botError) kv(info, 'bot error', a.botError).title = a.botError;
                    kv(info, 'activity', a.activity);
                    kv(info, 'position', a.pos ? `${a.pos.x}, ${a.pos.z}, level ${a.pos.level}` : '');
                    kv(info, 'packets', fmtCount(a.packets));
                    if (a.staffLevel > 0) kv(info, 'staff level', a.staffLevel);
                    kv(info, 'reconnects', a.reconnects);
                    if (a.nextRetry > 0) retryCell = kv(info, 'next retry', a.nextRetry > now ? `in ${fmtDuration(a.nextRetry - now)}` : 'due');
                    if (a.lastError) kv(info, 'last error', a.lastError).title = a.lastError;
                    if (a.manuallyDisconnected) kv(info, 'disconnected', 'by the operator');
                }
                if (durationCell) {
                    const value = fmtDuration(now - a.since);
                    if (durationCell.textContent !== value) durationCell.textContent = value;
                }
                if (retryCell) {
                    const value = a.nextRetry > now ? `in ${fmtDuration(a.nextRetry - now)}` : 'due';
                    if (retryCell.textContent !== value) retryCell.textContent = value;
                }

                // Skills: the full, fixed-order Skills tab (see skillrender.js — shared with the
                // dedicated Skills panel so both draw identically). Off by default here now that
                // the Skills panel exists as its own thing (agent/panelrestyle) — this toggle
                // stays for anyone who still wants it inline on the detail view too.
                const allSkills = a.skills ?? {};
                const showSkills = ctx.settings.skills;
                skillHead.hidden = !showSkills;
                if (showSkills) drawSkillsGrid(skills, store, allSkills, a.primarySkill);
                else {
                    skills.textContent = '';
                    delete skills.dataset.key;
                }

                const showCounters = ctx.settings.counters && (a.counters ?? []).length > 0;
                countersHead.hidden = !showCounters;
                const counterKey = JSON.stringify([name, showCounters, a.counters]);
                if (counterKey !== countersKey) {
                    countersKey = counterKey; counters.replaceChildren();
                    if (showCounters) for (const c of a.counters) kv(counters, c.name, fmtCount(c.value));
                }

                // Connection control needs the fleet's say-so (meta.connectionControl); a
                // fleet without it leaves this section out entirely, same reasoning as
                // Script below.
                const showConn = store.meta?.connectionControl === true;
                connHead.hidden = !showConn;
                connControl.hidden = !showConn;
                if (showConn) {
                    const connectedIsh = ['ingame', 'connecting', 'reconnecting', 'queued'].includes(a.state);
                    disconnectBtn.disabled = !connectedIsh || connBusy;
                    connectBtn.disabled = ['ingame', 'connecting', 'queued'].includes(a.state) || connBusy;
                    connectBtn.textContent = a.state === 'reconnecting' ? 'retry login' : 'reconnect';
                }

                // Control needs both the fleet's say-so (meta.botControl) and a script list to
                // pick from; a fleet with neither leaves this section out entirely rather than
                // showing disabled controls that can never work.
                const scripts = store.meta?.scripts ?? [];
                const showControl = store.meta?.botControl === true;
                controlHead.hidden = !showControl;
                control.hidden = !showControl;
                muleFieldWrap.hidden = settingsBox.hidden = settingsControl.hidden = !showControl;
                if (showControl) {
                    const running = a.botState === 'running';
                    const key = scripts.join(',');
                    if (key !== scriptsDrawn) {
                        scriptsDrawn = key;
                        scriptPick.textContent = '';
                        if (scripts.length === 0) scriptPick.append(el('option', '', '(no scripts registered)'));
                        else for (const s of scripts) scriptPick.append(el('option', '', s));
                    }
                    if (a.bot && scripts.includes(a.bot)) scriptPick.value = a.bot;
                    scriptPick.disabled = running || scripts.length === 0 || busy;
                    startBtn.disabled = running || scripts.length === 0 || busy;
                    stopBtn.disabled = !running || busy;
                    restartBtn.disabled = !running || busy;
                    applyBtn.disabled = !running || busy;

                    // Load the account's current settings into the box once per account
                    // switch, never on a later refresh — an operator mid-edit should not
                    // have their in-progress typing overwritten by the next status poll.
                    if (name !== settingsFor) {
                        settingsFor = name;
                        settingsBox.value = a.settings ? JSON.stringify(a.settings, null, 2) : '';
                        muleField.value = a.settings?.mule ?? '';
                    }
                }

                // Hand-off control needs the fleet's say-so (meta.handoffControl) and a
                // running bot to act on — same reasoning as Script above. Left visible
                // whenever a bot is running even if that particular script never checks
                // TakeHandoffRequest/SetMuleAt (only ProgressiveFisher does, tonight):
                // fleet.Manager.RequestHandoff/SetMuleAt only fail on "no bot running", not
                // on "this script ignores it," so there is no reliable way to hide it only
                // for scripts that would ignore it, and clicking it is harmless either way.
                const showHandoff = store.meta?.handoffControl === true;
                handoffHead.hidden = handoffControl.hidden = !showHandoff;
                if (showHandoff) {
                    const running = a.botState === 'running';
                    handoffBtn.disabled = !running;
                    muleAtInput.disabled = !running;
                    muleAtBtn.disabled = !running;
                }

                const showInventories = ctx.settings.inventories && store.meta?.inventories === true;
                invHead.hidden = !showInventories;
                if (!showInventories) { inv.textContent = ''; inventoryKey = ''; }
                else if (name !== invName) {
                    invName = name;
                    inv.textContent = '';
                    inventoryKey = '';
                    loadInventories(name);
                } else if (!invBusy && Date.now() - invAt >= INVENTORY_EVERY_MS) {
                    loadInventories(name);
                }

                logHead.hidden = ctx.settings.lines === 0;
                const scanKey = `${name}|${store.events.at(-1)?.seq ?? 0}|${store.events.length}`;
                if (scanKey === scannedLogKey && !why.has('reset')) return;
                scannedLogKey = scanKey;
                const lines = [];
                for (let i = store.events.length - 1; i >= 0 && lines.length < ctx.settings.lines; i--) {
                    if (store.events[i].account === name) lines.push(store.events[i]);
                }
                const key = `${name}|${lines.length ? lines[0].seq : 0}|${lines.length}`;
                if (key !== logKey) {
                    logKey = key;
                    log.textContent = '';
                    for (const ev of lines) log.append(logLine(ev, false));
                }
            },
            destroy() {
                closed = true;
            }
        };
    }
};
