package controlpanel

import (
	"net/http"
	"strings"
)

// GET /stream/view — the stream client on a bare black page, for OBS.
//
// Why this exists, and why it is a URL rather than a program. Users asked to get this
// view into OBS "light and easy to run on my pc". OBS already ships a Browser Source (its
// own embedded Chromium, GPU-accelerated), so pointing that at a page costs one source in
// an existing scene: no second process to start or supervise, no encoder, no virtual
// camera driver, no extra game session, and nothing new listening on the network. The
// alternatives all cost more for no picture this page cannot give:
//
//   - Window Capture of a browser window: works today with no code at all, but it needs a
//     browser window kept open, sized and never covered, and it captures the dashboard
//     chrome around the view.
//   - A virtual camera or NDI feed: a second process encoding frames, plus a driver, to
//     hand OBS pixels it can already render itself.
//   - Server-side video (WebRTC/RTMP): an encoder on the machine running the fleet, which
//     is the one machine whose spare CPU is worth protecting.
//
// What this page is. It imports the same installed provider at the same fixed same-origin
// path the dashboard panel does (/stream/client.js), mounts it full-bleed on black, and
// does nothing else: no dashboard bundle, no layout, no panels, no diagnostic overlay, and
// no input controls. It takes the same single exclusive VIEW lease through the same
// bridge, so it observes an ALREADY-CONNECTED session and cannot create, join or duplicate
// one, exactly as the panel cannot.
//
// The lease is exclusive, and that is worth saying plainly to whoever opens this: while
// OBS is showing an account, the dashboard's own Stream client panel cannot show one, and
// opening the panel on another account takes the lease away from OBS. The page notices and
// reopens by itself rather than sitting on a frozen frame.
//
// Query: account (required), fps (the RENDER rate the provider paints at, not the
// observation rate the server serves — see DefaultStreamMaxFPS), skin (an era skin the
// installed provider declares — see streamskin.go; omitted means its default, and an
// undeclared one is a 400 rather than a source that retries forever), and status=1 to show
// a one-line status while setting the source up, plus controls=0 to hide the toolbar. Nothing here is operator-supplied code: the
// provider path is fixed, and the account is checked by the bridge, not by this page.
func (s *Server) handleStreamView(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.streamRenderer == "" {
		writeError(w, http.StatusNotImplemented, "no stream client provider is installed on this build: start it with -stream-client")
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(streamViewScript))
		return
	case strings.HasSuffix(r.URL.Path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(streamViewStyle))
		return
	}
	// ?skin= picks an era skin (streamskin.go). It is checked HERE rather than left to
	// the provider because an OBS Browser Source retries a failed page forever: a skin
	// this build's provider does not declare is a permanent mistake in the URL, and the
	// operator setting the source up needs to read it once, plainly, instead of watching
	// a black source retry. No skin at all means the provider's own default.
	if skin := r.URL.Query().Get("skin"); !s.streamSkinAllowed(skin) {
		writeError(w, http.StatusBadRequest, s.streamSkinRefusal(skin))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(streamViewPage))
}

// The element ids here avoid "canvas" and "status": the cache-terrain provider refuses to
// mount when either already exists, because the reference client's own decoder shell claims
// them, and a page that had them would fight it for the same nodes.
//
// streamViewPage carries no inline script or style: the dashboard's Content-Security-Policy
// is script-src 'self'; style-src 'self', and this page is deliberately held to it rather
// than given an exception.
const streamViewPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Stream view</title>
<link rel="stylesheet" href="/stream/view.css">
</head>
<body>
<div id="host"></div>
<p id="viewstatus" hidden role="status" aria-live="polite"></p>
<div id="viewcontrols" aria-label="Stream controls">
<button id="viewinput" type="button" disabled aria-pressed="false">Take control</button>
<button id="viewfullscreen" type="button">Fullscreen</button>
<button id="viewtools" type="button" aria-expanded="false" aria-controls="viewtoolbox">Gameplay tools</button>
<span id="viewhealth" role="status">Connecting...</span>
</div>
<aside id="viewtoolbox" hidden aria-label="Gameplay tools">
<strong>Camera presets</strong>
<div><button data-preset="combat">Combat</button> <button data-preset="loot">Loot</button> <button data-preset="overview">Overview</button> <button data-preset="reset">Reset</button></div>
<label><input type="checkbox" data-toggle="names"> Player / NPC names</label>
<label><input type="checkbox" data-toggle="items"> Ground item labels</label>
<label><input type="checkbox" data-toggle="distances"> Distances on labels</label>
<label><input type="checkbox" data-toggle="grid"> Tile grid</label>
<label><input type="checkbox" data-toggle="hud"> Activity / XP per hour</label>
<label><input type="checkbox" data-toggle="fog"> Distance fog</label>
<label><input type="checkbox" data-toggle="shadows"> Ground shadows</label>
<label><input type="checkbox" id="viewclean"> Clean broadcast</label>
<label>Render FPS <select id="viewfps"><option>15</option><option>30</option><option>60</option></select></label>
<label>Render distance <select id="viewdistance"><option value="16">16 tiles · low</option><option value="24">24 tiles</option><option value="32">32 tiles · default</option><option value="48">48 tiles · extended</option></select></label>
<label>Scene resolution <select id="viewresolution"><option value="0.75">75% · performance</option><option value="1">100% · native</option><option value="1.5">150% · sharp</option><option value="2">200% · sharpest</option></select></label>
<label>Roofs <select id="viewroofs"><option value="auto">Automatic</option><option value="hide">Hide above current floor</option><option value="show">Show all</option></select></label>
<button id="viewscreenshot" type="button">Save screenshot</button>
<div><button id="viewsavecamera" type="button">Save camera angle</button> <button id="viewrestorecamera" type="button">Restore camera angle</button></div>
<div><button data-quality="low">Low CPU</button> <button data-quality="balanced">Balanced</button> <button data-quality="sharp">Sharp</button></div>
<button id="viewdiagnostics" type="button">Save diagnostics</button>
<output id="viewmetrics" aria-label="Stream performance"></output>
<small>Camera and labels affect this view only. Settings saved per account.</small>
</aside>
<script type="module" src="/stream/view.js"></script>
</body>
</html>
`

const streamViewStyle = `html,body{margin:0;padding:0;height:100%;background:#000;overflow:hidden}
#host{position:absolute;inset:0;background:#000}
#host canvas{display:block}
#viewcontrols{position:absolute;right:8px;top:8px;z-index:20;display:flex;align-items:center;gap:6px;padding:6px;background:rgba(0,0,0,.85);color:#ddd;font:12px system-ui;border-radius:4px;transition:opacity .2s}
#viewcontrols[data-idle="true"]{opacity:0;pointer-events:none}
#viewcontrols:focus-within,#viewcontrols:hover{opacity:1;pointer-events:auto}
#viewcontrols[hidden]{display:none}
#viewtoolbox{position:absolute;right:8px;top:58px;z-index:20;width:280px;max-height:calc(100% - 80px);overflow:auto;padding:12px;background:rgba(15,13,10,.96);color:#eee;font:13px/1.6 system-ui;border:1px solid #8b7b60;border-radius:4px}
#viewtoolbox[hidden]{display:none}
#viewtoolbox label,#viewtoolbox output,#viewtoolbox small{display:block;margin:6px 0}
#viewtoolbox button,#viewtoolbox select{background:#332c21;color:#fff;border:1px solid #8b7b60;padding:5px;cursor:pointer}
#viewtoolbox output{white-space:pre-line;color:#aee2a6}
body[data-clean="true"] #host [data-stream-diagnostics="true"]{display:none!important}
#viewcontrols button{background:#332c21;color:#fff;border:1px solid #8b7b60;padding:6px 10px;cursor:pointer}
#viewcontrols button:disabled{opacity:.5;cursor:default}
#viewcontrols button[aria-pressed="true"]{background:#305c2b}
#viewstatus{z-index:21;position:absolute;left:8px;bottom:6px;margin:0;font:12px/1.4 system-ui,sans-serif;color:#cfc9bd;background:rgba(0,0,0,.55);padding:3px 7px;border-radius:3px;pointer-events:none}
`

// streamViewScript keeps the source alive without anybody watching it: a lease can be lost
// to the dashboard, an account can log out and back in, and OBS may leave the source
// running for hours. Every failure retries with a bounded backoff instead of leaving a
// frozen frame on the stream.
const streamViewScript = `const q = new URLSearchParams(location.search);
const account = q.get('account') || '';
const fps = Math.max(1, Math.min(60, Number(q.get('fps')) || 30));
const skin = q.get('skin') || '';
const host = document.getElementById('host');
const status = document.getElementById('viewstatus');
const controls = document.getElementById('viewcontrols');
const input = document.getElementById('viewinput');
const health = document.getElementById('viewhealth');
const toolbox = document.getElementById('viewtoolbox');
const prefsKey = 'stream-view:' + account + ':' + skin;
let preferences = {};
try { preferences = JSON.parse(localStorage.getItem(prefsKey)) || {}; } catch (_) { }
if (typeof preferences !== 'object' || Array.isArray(preferences)) preferences = {};
if (q.has('fps')) preferences.fps = fps;
if (q.has('clean')) preferences.clean = q.get('clean') === '1';
controls.hidden = q.get('controls') === '0';
document.body.dataset.clean = String(preferences.clean === true || controls.hidden);
function saveSettings(settings) {
    Object.assign(preferences, settings);
    view?.setViewSettings?.(settings);
    document.body.dataset.clean = String(preferences.clean === true || controls.hidden);
    try { localStorage.setItem(prefsKey, JSON.stringify(preferences)); } catch (_) { }
    syncTools();
}
function syncTools(stats = view?.stats?.()) {
    for (const el of toolbox.querySelectorAll('[data-toggle]')) el.checked = !!(stats?.toggles?.[el.dataset.toggle] ?? preferences[el.dataset.toggle]);
    document.getElementById('viewclean').checked = document.body.dataset.clean === 'true';
    document.getElementById('viewfps').value = String(stats?.renderFps || preferences.fps || fps);
    document.getElementById('viewdistance').value = String(stats?.renderDistance || preferences.renderDistance || 32);
    document.getElementById('viewresolution').value = String(stats?.resolution || preferences.resolution || 1);
    document.getElementById('viewroofs').value = stats?.roofs || preferences.roofs || 'auto';
}
document.getElementById('viewtools').onclick = () => {
    toolbox.hidden = !toolbox.hidden;
    document.getElementById('viewtools').setAttribute('aria-expanded', String(!toolbox.hidden));
    syncTools(); wake();
};
for (const el of toolbox.querySelectorAll('[data-toggle]')) el.onchange = () => saveSettings({[el.dataset.toggle]: el.checked});
document.getElementById('viewclean').onchange = e => saveSettings({clean:e.target.checked});
document.getElementById('viewfps').onchange = e => saveSettings({fps:Number(e.target.value)});
document.getElementById('viewdistance').onchange = e => saveSettings({renderDistance:Number(e.target.value)});
document.getElementById('viewresolution').onchange = e => saveSettings({resolution:Number(e.target.value)});
document.getElementById('viewroofs').onchange = e => saveSettings({roofs:e.target.value});
document.getElementById('viewscreenshot').onclick = () => view?.screenshot?.();
document.getElementById('viewsavecamera').onclick = () => {
    const camera = view?.stats?.().camera;
    if (camera) saveSettings({cameraBookmark:{mode:camera.mode,yaw:camera.yaw,pitch:camera.pitch,distance:camera.targetDistance ?? camera.distance}});
};
document.getElementById('viewrestorecamera').onclick = () => {
    if (preferences.cameraBookmark) saveSettings(preferences.cameraBookmark);
};
const quality = {
    low:{fps:15,renderDistance:16,resolution:1},
    balanced:{fps:30,renderDistance:32,resolution:1.5},
    sharp:{fps:60,renderDistance:48,resolution:2}
};
for (const el of toolbox.querySelectorAll('[data-quality]')) el.onclick = () => saveSettings(quality[el.dataset.quality]);
document.getElementById('viewdiagnostics').onclick = () => {
    const blob = new Blob([JSON.stringify({capturedAt:new Date().toISOString(),stats:view?.stats?.()},null,2)],{type:'application/json'});
    const url = URL.createObjectURL(blob), link = document.createElement('a');
    link.href = url; link.download = 'stream-diagnostics.json'; link.click();
    setTimeout(() => URL.revokeObjectURL(url),1000);
};
const presets = {
    combat:{mode:'follow',distance:12,pitch:0.95,names:true},
    loot:{mode:'top-down',distance:18,items:true,grid:true},
    overview:{mode:'top-down',distance:32,names:true},
    reset:{mode:'follow',distance:20,pitch:0.95,yaw:0,names:false,items:false,grid:false,hud:true,distances:false,fog:true,shadows:true}
};
for (const el of toolbox.querySelectorAll('[data-preset]')) el.onclick = () => saveSettings(presets[el.dataset.preset]);
let view = null, stopped = false, attempt = 0, timer = 0, watcher = 0, idle = 0, inputBusy = false;
const controller = new AbortController();
function say(text, error = false) {
    status.hidden = !error && q.get('status') !== '1';
    status.textContent = text;
    health.textContent = text;
}
function wake() {
    controls.dataset.idle = 'false';
    clearTimeout(idle);
    idle = setTimeout(() => { if (toolbox.hidden && input.getAttribute('aria-pressed') !== 'true' && !inputBusy) controls.dataset.idle = 'true'; }, 5000);
}
addEventListener('pointermove', wake);
addEventListener('keydown', wake);
wake();
function resetInput() {
    input.disabled = true;
    input.setAttribute('aria-pressed', 'false');
    input.textContent = 'Take control';
}
input.addEventListener('click', async () => {
    const current = view;
    if (!current || inputBusy) return;
    const enabled = input.getAttribute('aria-pressed') !== 'true';
    inputBusy = true; input.disabled = true;
    try {
        const acknowledged = await current.setInputEnabled(enabled);
        if (current !== view || stopped) return;
        if (!!acknowledged !== enabled) throw Error('Input control was not acknowledged.');
        input.setAttribute('aria-pressed', String(enabled));
        input.textContent = enabled ? 'Release control' : 'Take control';
        say(enabled ? 'Manual input active' : 'Viewing ' + account);
        if (enabled) host.focus({ preventScroll: true });
    } catch (error) { say(error.message || String(error), true); }
    finally { inputBusy = false; input.disabled = !view; wake(); }
});
document.getElementById('viewfullscreen').addEventListener('click', async () => {
    try { if (document.fullscreenElement) await document.exitFullscreen(); else await document.documentElement.requestFullscreen(); }
    catch (error) { say(error.message || String(error), true); }
});
async function dispose() {
    clearTimeout(watcher); watcher = 0;
    const current = view; view = null; resetInput();
    try { await current?.destroy(); } catch (_) { }
}
// The provider keeps its own connection alive (a lost lease or an account between sessions is
// retried with the last frame on screen); this only mirrors what it reports.
function linkState(next) {
    if (stopped || !view) return;
    if (next.input === false && input.getAttribute('aria-pressed') === 'true') { resetInput(); input.disabled = false; }
    if (next.phase === 'reconnecting') say('Reconnecting ' + account + (next.message ? ': ' + next.message : ''));
    else if (next.phase === 'paused') say('Paused while hidden');
    else if (next.phase === 'live') say(input.getAttribute('aria-pressed') === 'true' ? 'Manual input active' : 'Viewing ' + account);
}
async function open() {
    timer = 0;
    if (stopped) return;
    if (!account) { say('Add ?account=<name> to this URL.', true); return; }
    try {
        say('Connecting ' + account);
        const provider = await import('/stream/client.js');
        const candidate = await provider.mount(host, { account, fps, skin, signal: controller.signal, onState: linkState });
        if (stopped) { await candidate?.destroy(); return; }
        if (typeof candidate?.destroy !== 'function') throw Error('The installed provider returned no view');
        view = candidate; attempt = 0;
        candidate.setViewSettings?.(preferences); syncTools();
        input.disabled = typeof candidate.setInputEnabled !== 'function';
        say('Viewing ' + account);
        watch();
    } catch (error) {
        await dispose();
        if (!stopped) { say(error.message || String(error), true); retry(); }
    }
}
function watch() {
    watcher = setTimeout(async () => {
        if (stopped || !view) return;
        let alive = !!host.querySelector('canvas');
        try {
            const stats = view.stats?.();
            if (stats?.stopped || stats?.running === false) alive = false;
            if (!toolbox.hidden && stats) {
                syncTools(stats);
                const self = stats.self;
                const combat = self?.combat;
                document.getElementById('viewmetrics').textContent =
                    'Paint ' + stats.paintFps + ' FPS / ' + stats.renderFps + ' cap\n' +
                    'Observations ' + stats.maxFps + '/s · GPU ' + ((stats.gl?.gpuBytes || 0) / 1048576).toFixed(1) + ' MB\n' +
                    (self ? 'Tile ' + self.x + ', ' + self.z + ' · level ' + self.level : '') +
                    (combat?.seen ? '\nLast observed HP ' + combat.health + '/' + combat.total : '');
            }
        } catch (_) { alive = false; }
        if (!alive) { await dispose(); retry(); return; }
        watch();
    }, 1000);
}
function retry() {
    if (stopped || timer) return;
    attempt++;
    const wait = Math.min(15000, 1000 * Math.pow(1.6, Math.min(attempt, 6)));
    health.textContent = 'Reconnecting in ' + Math.ceil(wait / 1000) + 's';
    timer = setTimeout(open, wait);
}
addEventListener('pagehide', () => {
    stopped = true; controller.abort();
    clearTimeout(timer); clearTimeout(watcher); clearTimeout(idle);
    void dispose();
});
open();
`
