import { anyOf, el, button, kv } from '../util.js';

// Optional renderer integration. Snapshot playback is deliberately not a provider.
// The backend must advertise these capabilities only after the actual session
// bridge and triangle renderer are installed; see docs/stream_client_panel.md.
export default {
    type: 'stream', title: 'Stream client',
    // Selection, disconnect and capability changes must release leases offscreen too.
    backgroundUpdates: true,
    description: 'One existing account, live GPU game window and optional input. Renderer integration required.',
    size: { w: 8, h: 5 }, accounts: 'one', settings: [
        { key: 'fps', label: 'Stream frame rate', kind: 'select', default: '30', options: [
            { value: '15', label: '15 FPS' }, { value: '30', label: '30 FPS' }, { value: '60', label: '60 FPS' }
        ] },
        // Which ERA's assets the same live session is drawn with. The ids are the closed
        // set internal/controlpanel/streamskin.go knows; which of them are actually
        // OFFERED is whatever the installed provider declares in its manifest, so this
        // list is filtered against /api/meta.streamClient.skins at runtime and a skin this
        // build has no provider for disables Load with a message naming what it does have.
        // A build whose provider declares no skins ignores this setting entirely.
        { key: 'skin', label: 'Era skin (needs a provider that declares it)', kind: 'select', default: '2004', options: [
            { value: '2004', label: '2004' }, { value: 'classic', label: 'RuneScape Classic' },
            { value: '2009', label: '2009' }, { value: '2011', label: '2011' }
        ] }
    ],
    create(body, ctx) {
        const controls = el('div', 'controls');
        const start = button('Load stream client');
        const stop = button('Close client');
        const input = button('Take input control');
        const fullscreen = button('Full screen');
        // Force lets input control be taken over a bank/trade or a MuleReceiver, which the
        // server otherwise refuses (internal/controlpanel/streaminput.go).
        const forceBox = document.createElement('input'); forceBox.type = 'checkbox';
        const force = el('label'); force.append(forceBox, ' force (bank/trade/mule)');
        controls.append(start, stop, input, force, fullscreen);
        const message = el('p'); message.setAttribute('role', 'status');
        const capabilities = el('dl', 'kv');
        const gpu = kv(capabilities, 'GPU scene', 'Not installed');
        const session = kv(capabilities, 'Existing session', 'Not installed');
        const era = kv(capabilities, 'Era skin', 'Not offered by this provider');
        const crowd = kv(capabilities, 'CrowdPlay', 'Not connected');
        const ml = kv(capabilities, 'ML outcome recording', 'Not connected');
        const host = el('div');
        // The view fills whatever the panel has left and never scrolls: the provider scales the
        // game window to fit (aspect kept, letterboxed on a clear background).
        host.style.cssText = 'flex:1 1 auto;min-height:0;width:100%;overflow:hidden;background:transparent;position:relative';
        body.classList.add('streambody');
        body.style.cssText = 'display:flex;flex-direction:column;overflow:hidden';
        host.hidden = true;
        body.append(controls, message, capabilities, host);
        let selected = '', mounted = '', mountedRenderer = '', controller = null, client = null;
        let loading = false, takingInput = false, hasInput = false, destroyed = false, epoch = 0;
        // wanted: the operator asked for this view (Load) and has not closed it. A view that ends for
        // any other reason (the account changed, the provider failed, the page lost the dashboard)
        // is loaded again by itself once it can be, instead of staying blank until somebody notices.
        let wanted = false, resumeTimer = 0, failures = 0;
        const capability = () => ctx.store.meta?.streamClient;
        // The renderer identity is whatever the BACKEND says is installed on this build
        // (read from the installed provider's own manifest — see
        // internal/controlpanel/streamclient.go), never a constant in this page. That is
        // what lets an honestly weaker provider be installed and labelled as such,
        // instead of every provider having to call itself 'webgl3d' to get loaded.
        const renderer = () => capability()?.sceneRenderer || '';
        const authentic = () => renderer() === 'webgl3d';
        const ready = () => renderer() !== '' && capability()?.existingSession === true;
        const online = () => ctx.store.byName.get(selected)?.state === 'ingame';
        // Era skins (internal/controlpanel/streamskin.go). skins() is whatever the
        // INSTALLED provider declares, never a list this page invents: a build whose
        // provider declares none gets an empty array, wantSkin() is then '' and nothing
        // about this panel changes. skinOK() is what stops Load from mounting a skin the
        // installed provider never said it could draw.
        const skins = () => capability()?.skins || [];
        const wantSkin = () => (skins().length ? (ctx.settings.skin || capability()?.skin || '') : '');
        const skinInfo = () => skins().find(s => s.id === wantSkin()) || null;
        // Declared by the provider AND backed by assets on the machine running the fleet.
        // The backend computes `available` from what is really on disk
        // (internal/eraassets), so a provider that can draw an era on a machine without
        // that era's files is offered nothing rather than allowed to fail at mount.
        const skinOK = () => !skins().length || skinInfo()?.available === true;
        function buttons() {
            start.disabled = destroyed || loading || !!client || !selected || !online() || !ready() || !skinOK() || ctx.store.conn !== 'live';
            stop.disabled = !controller;
            input.disabled = !client || takingInput || capability()?.sessionInput !== true;
            input.textContent = hasInput ? 'Release input control' : 'Take input control';
            forceBox.disabled = input.disabled || hasInput;
            fullscreen.disabled = !client;
        }
        function release(candidate) {
            try { Promise.resolve(candidate?.destroy()).catch(() => {}); } catch (_) { /* cleanup continues */ }
        }
        function resumeSoon(ms) {
            clearTimeout(resumeTimer);
            if (!wanted || destroyed || client || loading) return;
            resumeTimer = setTimeout(() => { if (wanted && !destroyed && !client && !loading && !start.disabled) void load(); else resumeSoon(ms); }, ms);
        }
        function close() {
            epoch++; controller?.abort(); controller = null;
            release(client); client = null; mounted = ''; mountedRenderer = ''; loading = false; takingInput = false; hasInput = false;
            host.replaceChildren(); host.hidden = true; delete host.dataset.skin; capabilities.hidden = false; buttons();
        }
        // The provider reports the state of its connection: a lost lease or an account between sessions
        // is retried by the provider itself with the last frame left on screen; only the message
        // and the input state are the panel's to show.
        function onLink(attempt, next) {
            if (attempt !== epoch || destroyed) return;
            hasInput = next.input === true;
            if (next.phase === 'reconnecting') message.textContent = `Reconnecting to ${selected}` + (next.message ? `: ${next.message}` : '') + '. The last frame stays up; input is off.';
            else if (next.phase === 'paused') message.textContent = 'Paused while this view is hidden; it resumes when shown.';
            else if (next.phase === 'live' && client) message.textContent = hasInput ? `Controlling ${mounted}: its bot is paused and resumes when you release. Left-click does the first option, right-click lists them. Every action is audit-logged.` : `Viewing ${mounted}. Input is off.`;
            else if (next.phase === 'ended') { close(); message.textContent = `Stream ended: ${next.message}`; resumeSoon(5000); }
            buttons();
        }
        start.addEventListener('click', () => { wanted = true; failures = 0; void load(); });
        async function load() {
            if (start.disabled) return;
            controller = new AbortController(); const signal = controller.signal;
            const attempt = ++epoch; const account = selected; const expected = renderer(); const skin = wantSkin();
            loading = true; buttons(); message.textContent = 'Loading the selected account renderer...';
            try {
                // Fixed same-origin integration path: no arbitrary script URL in layouts.
                const provider = await import('/stream/client.js');
                if (signal.aborted || destroyed || attempt !== epoch) return;
                const candidate = await provider.mount(host, { account, fps: Number(ctx.settings.fps), skin, signal, onState: next => onLink(attempt, next) });
                if (signal.aborted || destroyed || attempt !== epoch) { release(candidate); return; }
                // The module that actually loaded must confirm the SAME renderer identity
                // the backend advertised for the installed provider, and the same account.
                // A mismatch means the served module is not the one this build described,
                // so it is rejected and destroyed rather than trusted.
                if (candidate?.sceneRenderer !== expected || candidate?.account !== account || typeof candidate?.destroy !== 'function') {
                    release(candidate); throw Error(`Provider reported renderer "${candidate?.sceneRenderer ?? 'none'}" for "${candidate?.account ?? 'no account'}"; this build advertises "${expected}" for "${account}".`);
                }
                // Same rule for the era skin: a provider that silently drew 2004 when 2011
                // was asked for would be indistinguishable from one that has no 2011
                // assets, which is exactly the confusion skins exist to prevent. Only
                // checked when a skin was actually requested, so providers that declare
                // none are unaffected.
                if (skin && candidate?.skin !== skin) {
                    release(candidate); throw Error(`Provider mounted the "${candidate?.skin ?? 'unnamed'}" skin when "${skin}" was requested.`);
                }
                client = candidate; mounted = account; mountedRenderer = expected; loading = false; host.hidden = false; failures = 0;
                // The era the PROVIDER said it mounted, in a machine-readable place: the
                // panel's own status line is prose, and a fixture that has to parse prose
                // to find out which era is on screen will eventually parse it wrong.
                if (skin) host.dataset.skin = candidate.skin; else delete host.dataset.skin;
                capabilities.hidden = true; // the view gets the space; shown again on close
                const era = skin ? ` in the "${skinInfo()?.label || skin}" skin` : '';
                message.textContent = authentic()
                    ? `Viewing ${account}${era}. Input is off. Capture this full-screen view for your stream.`
                    : `Viewing ${account}${era} through the "${expected}" provider — a diagnostic view, not the authentic game window. Input is off.`;
            } catch (error) {
                if (signal.aborted || destroyed || attempt !== epoch) return;
                close();
                // Another viewer, an account not yet in game and a slow cache are all worth another try;
                // after five in a row it stops asking and waits for the operator.
                if (++failures < 5) { message.textContent = `Stream client unavailable: ${error.message || error}. Trying again shortly.`; resumeSoon(Math.min(30000, 3000 * failures)); }
                else { wanted = false; message.textContent = `Stream client unavailable: ${error.message || error}`; }
            }
            buttons();
        }
        stop.addEventListener('click', () => { wanted = false; clearTimeout(resumeTimer); close(); message.textContent = 'Stream client closed; rendering and input released.'; });
        input.addEventListener('click', async () => {
            if (input.disabled) return;
            const active = client, attempt = epoch, desired = !hasInput;
            takingInput = true; buttons();
            try {
                if (typeof active.setInputEnabled !== 'function') throw Error('Session input bridge is unavailable.');
                const acknowledged = await active.setInputEnabled(desired, { force: forceBox.checked });
                if (destroyed || active !== client || attempt !== epoch) return;
                if (acknowledged !== desired) throw Error('Input lease was not acknowledged.');
                hasInput = desired;
                message.textContent = hasInput ? `Controlling ${mounted}: its bot is paused and resumes when you release. Left-click does the first option, right-click lists them. Every action is audit-logged.` : `Viewing ${mounted}. Input is off.`;
            } catch (error) {
                if (destroyed || active !== client || attempt !== epoch) return;
                // A refusal from the server is just that; the view carries on without input.
                if (error.refused) { message.textContent = `Input control refused: ${error.message}`; return; }
                // Any other failed acknowledgement is ambiguous: release the whole lease.
                close(); message.textContent = `Input control unavailable: ${error.message || error}`;
            } finally { if (attempt === epoch) takingInput = false; buttons(); }
        });
        fullscreen.addEventListener('click', async () => {
            if (fullscreen.disabled) return;
            try { await host.requestFullscreen(); } catch (_) { message.textContent = 'The browser could not enter full screen.'; }
        });
        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'select', 'status', 'meta', 'conn')) return;
                const next = ctx.accounts[0] || store.selected || '';
                // A provider swapped underneath a live instance (mountedRenderer no longer
                // what the build advertises) is a capability change like any other: the
                // instance is released rather than left painting under a stale identity.
                // The account being in game or the dashboard connection dropping does NOT close the view:
                // the provider keeps its renderer and retries the lease itself (stream-link.js), which is
                // what keeps it loaded across the relogs and reconnects a fleet has all day.
                if (next !== selected || (controller && (!ready() || (mountedRenderer && mountedRenderer !== renderer()) || (hasInput && capability()?.sessionInput !== true)))) close();
                selected = next;
                const cap = capability();
                gpu.textContent = !renderer()
                    ? 'No provider installed (captures use CPU scene rendering)'
                    : authentic()
                      ? 'Authentic GPU scene renderer (webgl3d)'
                      : `Real WebGL triangles, but NOT the authentic scene: "${renderer()}"`;
                gpu.title = cap?.description || '';
                session.textContent = cap?.existingSession === true
                    ? `Existing Go session, ${cap.maxFps || '?'} observations/s, ${(cap.frameTransports || [cap.frameTransport || 'unknown']).join(' or ')} frames`
                    : 'Session bridge not installed';
                crowd.textContent = cap?.crowdplay === true ? 'Connected' : 'Not connected';
                ml.textContent = cap?.mlRecording === true ? 'Recording outcomes' : 'Not connected';
                // The era row reports the installed provider's declared skins and what the
                // selected one actually draws — the provider's own words, not this page's.
                const info = skinInfo();
                // Three distinct states, because they need three different actions from
                // the operator: no skins at all, a skin this provider cannot draw, and a
                // skin it can draw but whose assets are not on this machine.
                era.textContent = !skins().length
                    ? 'Not offered by this provider (one era only)'
                    : !info
                      ? `"${ctx.settings.skin}" is not one this provider draws; it declares ${skins().map(s => s.id).join(', ')}`
                      : info.available
                        ? `${info.label} - ${info.found || info.assets || 'asset source unstated'}`
                        : `${info.label} - assets missing: ${info.found || 'not found on this machine'}`;
                era.title = info?.description || '';
                if (!controller) message.textContent = !ready()
                    ? 'No stream client provider is installed on this build. Install one (see docs/stream_client_panel.md) to enable this panel; nothing here renders or connects on its own.'
                    : !skinOK()
                      ? (skinInfo()
                          ? `The "${skinInfo().label}" era skin cannot be drawn on this machine: ${skinInfo().found || 'its assets were not found'}. Point the panel's -era-assets at that era's files, or choose another skin in this panel's settings. Available now: ${skins().filter(s => s.available).map(s => s.id).join(', ') || 'none'}.`
                          : `The installed provider does not draw the "${ctx.settings.skin}" era skin. It declares: ${skins().map(s => s.id).join(', ')}. Change the skin in this panel's settings, or install a provider that has that era's assets.`)
                      : authentic()
                        ? 'Load the selected account; no renderer starts until you request it.'
                        : `Installed provider is "${renderer()}" — real GPU triangles of live session data, not the authentic 2004 game window. Load it to view the selected account.`;
                ctx.setSub(selected || 'No account selected'); buttons();
                resumeSoon(2500);
            },
            destroy() { destroyed = true; wanted = false; clearTimeout(resumeTimer); close(); }
        };
    }
};
