// Optional stream client provider "webgl-cache-terrain". Existing-session observations only.
// Install alongside a rebuilt capture adapter exposing terrainGpuMesh:
//     -stream-client ../tools/stream/cache-terrain-client.js  (+ -capture-renderer, -capture-cache)
// Its sibling modules (scene-state.js, scene-gl.js, scene-chrome.js) are served from the same
// directory at /stream/<name> by the panel's handleStreamAsset.
//
// GPU: decoded terrain, scenery, textures, animated actors/items and packet-driven effects.
// Native interfaces, bitmap menus, hitmark sprites and health bars surround the GPU viewport.
// Input uses the existing server control lease and validated world/interface actions.
//
// TRANSPORT. It asks the bridge for "scene-delta" frames (internal/controlpanel/
// streamscene.go): one compact whole scene, then only what changed, confirmed against the
// seq it last applied (`have`), with a whole-scene resync at least every 10s. A server that
// predates that transport answers "full-snapshot"; the same view is then projected from each
// whole RenderSnapshot in the browser (without npc names). Observations are paced by the
// server (maxFps, 5/s); the view paints at the panel's own 15/30/60 FPS only while something
// is moving or the camera is being dragged, and idles otherwise.
import { SceneState, terrainHeights, gridHeights, isMoving, faceYaw, primaryFrame, loopFrame, cycleFrame, roofTop, CYCLE_MS } from './scene-state.js';
import { createSceneRenderer, MarkerMesh, MARKER, FLOATS, modelMatrix, widenTerrain } from './scene-gl.js';
import { createChrome, createGameFrame } from './scene-chrome.js';
import { createClassicFrame, loadEraSprites } from './scene-chrome-classic.js';
import { createActions } from './scene-actions.js';
import { createViewControls, fitBox, HELP, advancePaintDeadline } from './scene-view.js';
import { bitmapFont, bitmapSprite, effectFrame, projectileAt } from './scene-effects.js';
import { createStreamLink, REQUEST_TIMEOUT_MS } from './stream-link.js';
import { createOverlay } from './scene-overlay.js';

const TERRAIN_RADIUS = 32;   // default; browser options allow 16/24/32/48 tiles
const MAX_LABELS = 24;       // nearest named entities labelled per frame
const MAX_MARKERS = 260;     // server caps a scene at 255 entities, plus the account itself
const MODEL_BATCH = 24;      // models requested from the adapter per call
const DEFAULT_RESOLUTION = 1.5; // scene supersampling: x1.5 of the device pixel ratio (capped at 2 in all)

let active = false, adapter, detail = { highDetail: false };
let decoding = Promise.resolve();
// One request with a deadline. A non-OK answer throws an Error carrying the HTTP status, which the
// stream link turns into "retry" or "give up" (stream-link.js); a transport failure has none.
async function request(path, options = {}, signal) {
    const deadline = new AbortController(), started = performance.now();
    let stalled = false;
    const cancel = () => deadline.abort();
    // The deadline fires from a timer; when the page's main thread was blocked (a map decode takes
    // seconds on a busy machine) the timer runs late and the reply may already be waiting. A late
    // deadline is a stall of this page, not a failure of the bridge: it is flagged so the caller
    // retries instead of counting it.
    const expire = () => { stalled = performance.now() - started > REQUEST_TIMEOUT_MS + 1500; cancel(); };
    signal?.addEventListener('abort', cancel, { once: true });
    if (signal?.aborted) cancel();
    const timeout = setTimeout(expire, REQUEST_TIMEOUT_MS);
    try {
        const response = await fetch(path, { cache: 'no-store', ...options, signal: deadline.signal });
        return { response, text: await response.text() };
    } catch (error) {
        throw stalled ? Object.assign(error, { stalled: true }) : error;
    } finally { clearTimeout(timeout); signal?.removeEventListener('abort', cancel); }
}
async function post(path, body, signal) {
    const { response, text } = await request(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }, signal);
    let value = null;
    try { value = JSON.parse(text); } catch (_) { /* a non-JSON error page */ }
    if (!response.ok) throw Object.assign(Error(value?.error || `HTTP ${response.status}`), { status: response.status });
    return value;
}
// The reference decoder's two hidden elements (ids the adapter's shell looks up). Created once and
// kept for the page's life: tearing them down per view raced with the next view's setup.
function decoderHost() {
    if (!document.getElementById('canvas')) document.body.append(Object.assign(document.createElement('canvas'), { id: 'canvas', width: 765, height: 503, hidden: true }));
    if (!document.getElementById('status')) document.body.append(Object.assign(document.createElement('div'), { id: 'status', hidden: true }));
}
// A random token naming this browser tab, so a reload or a reconnect retakes its own view lease at
// once instead of waiting out the old one (streamclient.go, the open request's owner).
function leaseOwner() {
    try {
        let id = sessionStorage.getItem('stream-lease-owner');
        if (!id) { id = crypto.randomUUID(); sessionStorage.setItem('stream-lease-owner', id); }
        return id;
    } catch (_) { return crypto.randomUUID?.() || String(Math.random()).slice(2); }
}

// ERA SKINS this provider declares, kept beside the code that would have to draw them and
// mirrored in cache-terrain-client.js.manifest.json — which is the copy the Go server
// validates and advertises (internal/controlpanel/streamskin.go). One entry today: this
// provider decodes the fleet's own Lost City 289 cache through the capture adapter and
// holds no second era's assets of any kind. Adding an id here without the assets behind it
// would make the panel offer a skin that silently drew 2004, which is exactly what the
// panel's returned-skin check exists to catch.
export const SKINS = ['2004', 'classic'];
const DEFAULT_SKIN = '2004';

export async function mount(host, {account, fps, skin, signal, onState}) {
    if (active) throw Error('Only one native terrain decoder can be viewed at a time.');
    if (signal?.aborted) throw Error('Aborted before opening terrain view.');
    // An unknown skin is refused rather than quietly drawn as 2004: the point of naming an
    // era is that "it drew something" and "it drew that era" are different answers. An
    // empty skin is a caller that did not ask, which gets the default.
    const eraSkin = skin || DEFAULT_SKIN;
    if (!SKINS.includes(eraSkin)) throw Error(`This provider draws only the ${SKINS.join(', ')} era skin; "${eraSkin}" has no assets installed here.`);
    active = true;
    const controller = new AbortController();
    // The chrome is chosen once the adapter is loaded (see below): the game's own window when
    // it can draw it, else the DOM panels. Until then the DOM layout shows the loading text.
    let chrome = createChrome(host);
    const adapterCall = fn => { const task = decoding.then(fn); decoding = task.catch(() => {}); return task; };
    let nativeFonts = {}, hitSprites = [], effectInfo = new Map(), effectPending = new Set(), effectRetry = new Map(), effectBusy = false;
    let downX = 0, downY = 0;
    let canvas = chrome.glCanvas, overlay = chrome.overlay, uiTab = 3, uiDirty = true, uiBusy = false, uiLastYaw = null, uiInfo = null;
    chrome.overlay.dataset.streamDiagnostics = 'true';
    let renderFps = [15, 30, 60].includes(fps) ? fps : 30;
    let renderDistance = TERRAIN_RADIUS, resolution = DEFAULT_RESOLUTION, roofs = 'auto', terrainChunks = [];
    const state = new SceneState(), mesh = new MarkerMesh(MAX_MARKERS);
    const modelInfo = new Map(); // model key -> {height, triangles} | {retryAt}
    // Animated scenery (fires, torches, wheels...): the adapter exports each distinct one as a sequence
    // beside the static batches; locKinds is key -> {delays, total}, locPlaced where each one stands.
    let locKinds = new Map(), locPlaced = [];
    let modelBusy = false, lastModelError = '';
    let renderer, stopped = false, raf = 0, lastError = '', transport = '', link = null, linkState = { phase: 'opening', message: '' };
    let roof = null, drawnTop = 3, textureAvgs = null, atlasInfo = null, terrainKey = '', locsKey = '[]', terrainOrigin = null, heightAt = null, terrainInfo = null, decodeBusy = false, decodeRetryAt = 0;
    let camera = { yaw: 0, pitch: 0.95, distance: 20 }, dragging = false, px = 0, py = 0, dirty = true, nextPaint = 0;
    const opened = performance.now();
    const counters = { frames: 0, uploads: 0, decodes: 0, decodeMs: 0, fullFrames: 0, fullBytes: 0, deltaFrames: 0, deltaBytes: 0, throttled: 0, resyncs: 0,
        leases: 0, firstFrameMs: 0, paints: 0, paintWindow: [], maxPaintMs: 0, modelsLoaded: 0, modelErrors: 0, modelMs: 0, animationsLoaded: 0, interfaceFrames: 0, inputActions: 0, screenshots: 0 };

    // The entities drawn last (the overlay, picking and the shadows read it); entries are pooled.
    const placed = [];
    // Input: world and interface clicks, the game's option menus, Use mode and typing (scene-actions.js).
    const actions = createActions({
        state, host, controller, counters, placed, adapterCall, markUi: () => { uiDirty = true; },
        get chrome() { return chrome; }, get canvas() { return canvas; }, get renderer() { return renderer; }, get heightAt() { return heightAt; },
        get adapter() { return adapter; }, get renderDistance() { return renderDistance; }, get uiTab() { return uiTab; },
        get nativeFonts() { return nativeFonts; }, get link() { return link; }, get stopped() { return stopped; },
    });

    function down(e) { if (e.button !== 0) return; dragging=true;px=e.clientX;py=e.clientY;downX=e.clientX;downY=e.clientY;canvas.setPointerCapture?.(e.pointerId); }
    function move(e) {
        if (!dragging || stopped) return;
        view?.rotate(e.clientX-px, e.clientY-py);
        px=e.clientX; py=e.clientY; paint(performance.now()); // synchronous: a drag redraws in the same task
    }
    function up(e) {
        const click = dragging && actions.inputOn && Math.hypot(e.clientX - downX, e.clientY - downY) < 5;
        dragging=false;
        if (click) void actions.onClick(e, false);
    }
    // Right-click never opens the browser's own menu over the view: it shows the game's option
    // menu, read-only (for information) unless input control is held.
    function context(e) { e.preventDefault(); host.focus({preventScroll:true}); void actions.onClick(e, true); }
    function wheel(e) { if (!view) e.preventDefault(); } // zoom belongs to the view controls
    // View controls (camera, overlays, shortcuts), fit-to-panel and visibility.
    let view = null, lastStep = 0, hud = null, help = null, visible = true, fitObserver = null, visObserver = null;
    function fit() {
        const r = host.getBoundingClientRect();
        if (!r.width || !r.height) return;
        const b = chrome.aspect ? fitBox(r.width, r.height, chrome.aspect.w, chrome.aspect.h) : fitBox(r.width, r.height);
        Object.assign(chrome.root.style, { inset: 'auto', left: b.left + 'px', top: b.top + 'px', width: b.width + 'px', height: b.height + 'px' });
        dirty = true; uiDirty = true;
    }
    function toggleHelp() {
        if (help) { help.remove(); help = null; return; }
        help = document.createElement('div');
        help.setAttribute('role', 'dialog'); help.setAttribute('aria-label', 'Stream client shortcuts');
        help.style.cssText = 'position:absolute;right:6px;top:6px;background:rgba(0,0,0,.82);color:#ff0;font:12px Arial,sans-serif;padding:6px 10px;border:1px solid #5d5447;z-index:6;pointer-events:none';
        const title = document.createElement('b'); title.textContent = 'Stream client keys (click the view first)'; help.append(title);
        for (const [k, what] of HELP) { const row = document.createElement('div'); row.textContent = `${k} — ${what}`; row.style.color = '#fff'; help.append(row); }
        canvas.parentElement.append(help);
    }
    function screenshot() {
        paint(performance.now()); // the GL buffer is only readable in the task that drew it
        const root = chrome.root, rb = root.getBoundingClientRect(), dpr = Math.min(devicePixelRatio || 1, 2);
        const out = document.createElement('canvas'); out.width = Math.round(rb.width * dpr); out.height = Math.round(rb.height * dpr);
        const g = out.getContext('2d');
        g.fillStyle = '#000'; g.fillRect(0, 0, out.width, out.height);
        for (const c of [canvas, chrome.labels]) {
            const cb = c.getBoundingClientRect();
            g.drawImage(c, (cb.left - rb.left) * dpr, (cb.top - rb.top) * dpr, cb.width * dpr, cb.height * dpr);
        }
        const frameCanvas = root.querySelector('canvas[aria-label^="Game interfaces"]');
        if (frameCanvas) { g.imageSmoothingEnabled = false; g.drawImage(frameCanvas, 0, 0, out.width, out.height); }
        out.toBlob(blob => {
            if (!blob) return;
            const a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = `${account}-${new Date().toISOString().replace(/[:.]/g, '-')}.png`;
            a.click();
            setTimeout(() => URL.revokeObjectURL(a.href), 5000);
        }, 'image/png');
        counters.screenshots++;
    }
    function installView() {
        view?.destroy(); hud?.remove();
        view = createViewControls(host, canvas, { onChange: () => { dirty = true; }, onAction: a => { if (a === 'screenshot') screenshot(); else if (a === 'help') toggleHelp(); },
            onKey: e => actions.onKey(e), wheelTaken: e => { if (!chrome.gameFrame || !state.scene?.ui || state.scene.ui.main === -1) return false; void actions.interfaceWheel(e); return true; } });
        hud = document.createElement('div');
        hud.dataset.streamDiagnostics = 'true';
        hud.setAttribute('role', 'note'); hud.setAttribute('aria-label', 'Account status');
        hud.style.cssText = 'position:absolute;left:4px;top:4px;color:#ff0;font:11px Arial,sans-serif;text-shadow:1px 1px #000;pointer-events:none;white-space:pre;z-index:4';
        canvas.parentElement.append(hud);
        fitObserver?.disconnect(); fitObserver = new ResizeObserver(fit); fitObserver.observe(host); fit();
        // Nothing is drawn while the panel is scrolled out of view or the tab is hidden, and
        // observations slow to one a second (enough to keep the view lease).
        visObserver?.disconnect();
        visObserver = new IntersectionObserver(es => { visible = es.some(e => e.isIntersecting); wakeLink(); });
        visObserver.observe(host);
    }
    const showing = () => visible && document.visibilityState !== 'hidden';

    const destroy = async () => {
        if (stopped) return;
        stopped=true;controller.abort();cancelAnimationFrame(raf);
        signal?.removeEventListener('abort',abort);
        document.removeEventListener('visibilitychange', wakeLink); removeEventListener('pagehide', pageHide);
        canvas.removeEventListener('pointerdown',down);window.removeEventListener('pointermove',move);window.removeEventListener('pointerup',up);canvas.removeEventListener('wheel',wheel);canvas.removeEventListener('contextmenu',context);
        actions.destroy();
        view?.destroy(); view = null; fitObserver?.disconnect(); visObserver?.disconnect(); help?.remove(); hud?.remove();
        renderer?.destroy();renderer=null;
        nativeFonts = {}; hitSprites = []; effectInfo.clear(); effectPending.clear(); effectRetry.clear();
        chrome.destroy();
        active = false;
        // Releases the view lease (a close request) and ends the observation loop.
        await link?.stop();
    };
    const abort = () => { void destroy(); };
    signal?.addEventListener('abort',abort,{once:true});
    canvas.addEventListener('pointerdown',down);window.addEventListener('pointermove',move);window.addEventListener('pointerup',up);
    canvas.addEventListener('wheel',wheel,{passive:false});canvas.addEventListener('contextmenu',context);
    let maxFps = 5;
    // The link's state, for the panel and OBS page (onState) and for the overlay on the view.
    let banner = null;
    function setLinkState(next) {
        linkState = { ...linkState, ...next };
        // A lost or paused link keeps the last scene on screen under a short notice (hidden in the
        // clean/OBS mode, which hides every diagnostics element).
        const notice = linkState.phase === 'reconnecting' ? 'Reconnecting to ' + account + '…' + (linkState.message ? ' (' + linkState.message + ')' : '')
            : linkState.phase === 'paused' ? 'Paused while hidden' : '';
        if (notice && !banner) {
            banner = document.createElement('div'); banner.setAttribute('role', 'status'); banner.dataset.streamDiagnostics = 'true';
            banner.style.cssText = 'position:absolute;left:50%;top:8px;transform:translateX(-50%);background:rgba(0,0,0,.78);color:#ffcf6a;font:12px Arial,sans-serif;padding:3px 10px;border-radius:3px;z-index:7;pointer-events:none;max-width:90%';
            canvas.parentElement.append(banner);
        }
        if (banner) { banner.hidden = !notice; banner.textContent = notice; }
        try { onState?.({ ...linkState, input: actions.inputOn }); } catch (_) { /* a listener must not stop the view */ }
    }
    const wakeLink = () => { dirty = true; uiDirty = true; link?.poke(); };
    // The page is going away: release the lease now, with a request that outlives it, instead of
    // leaving the one view slot held for the TTL (a reload would then be refused as "another viewer").
    const pageHide = () => { link?.releaseNow(); };
    try {
        decoderHost();
        overlay.textContent='Loading native cache terrain…';
        // Older servers gave this module no revalidation policy. A versioned URL also
        // bypasses an already cached adapter when upgrading those installations.
        adapter ||= await import('/capture/client.js?v=stream-20261006-opt1');
        // High detail (all levels, roofs, ground decorations, full textures), when this adapter
        // offers the choice; it only takes effect before the adapter's first use in this page.
        detail = typeof adapter.configure === 'function' ? adapter.configure({ highDetail: true }) : { highDetail: false };
        void adapterCall(async () => {
            if (adapter.panelFontData) for (const f of await adapter.panelFontData()) nativeFonts[f.name] = bitmapFont(f);
            if (adapter.combatSpriteData) hitSprites = (await adapter.combatSpriteData()).map(bitmapSprite);
        }).then(() => { dirty = true; }).catch(e => { lastModelError = 'overlay assets: ' + e.message; });
        if (typeof adapter.interfaceFrame === 'function') {
            canvas.removeEventListener('pointerdown', down); canvas.removeEventListener('wheel', wheel); canvas.removeEventListener('contextmenu', context);
            chrome.destroy();
            if (eraSkin === 'classic') {
                // The Classic skin's chrome is Classic's OWN art, decoded by this process
                // from the operator's -era-assets root. A skin whose sprites will not load
                // fails here rather than drawing 2004 under a Classic label.
                overlay.textContent = 'Loading RuneScape Classic interface sprites…';
                chrome = createClassicFrame(host, await loadEraSprites('classic', controller.signal), tab => { uiTab = tab; uiDirty = true; });
            } else {
                chrome = createGameFrame(host, tab => { uiTab = tab; uiDirty = true; });
            }
            chrome.root.addEventListener('contextmenu', ev => { ev.preventDefault(); if (ev.target !== canvas) void actions.onInterfaceClick(ev, true); });
            // The side panel and chatbox: focus the view (keys go to it only while focused)
            // and act on a left click the way the game's own interfaces do.
            chrome.root.addEventListener('pointerdown', ev => { if (ev.target !== canvas) host.focus({ preventScroll: true }); });
            chrome.root.addEventListener('click', ev => { if (ev.target !== canvas && ev.button === 0) void actions.onInterfaceClick(ev, false); });
            chrome.root.addEventListener('wheel', ev => { if (ev.target !== canvas) { ev.preventDefault(); void actions.interfaceWheel(ev); } }, { passive: false });
            canvas = chrome.glCanvas; overlay = chrome.overlay; overlay.dataset.streamDiagnostics = 'true';
            canvas.addEventListener('pointerdown', down); canvas.addEventListener('wheel', wheel, { passive: false }); canvas.addEventListener('contextmenu', context);
            overlay.textContent = 'Loading native cache terrain…';
        }
        if (typeof adapter.terrainGpuMesh !== 'function') throw Error('Rebuild/install the capture adapter: terrainGpuMesh is missing.');
        if (stopped) throw Error('Terrain view was closed while loading.');
        renderer=createSceneRenderer(canvas); renderer.setResolution(resolution);
        installView();
        // The server's tick (600 ms live; a lab engine may run faster): movement speeds scale to it.
        try { const t = await (await fetch('/api/enginetick', { cache: 'no-store' })).json(); if (t?.available && t.tickRateMs > 0) state.tickScale = 600 / t.tickRateMs; } catch (_) { /* default 600 ms */ }
        if (typeof adapter.textureAverages === 'function') { textureAvgs = await adapter.textureAverages(); renderer.setTextureAverages(textureAvgs); }
        link = createStreamLink({
            account, transport: 'scene-delta', owner: leaseOwner(), post: (path, body, sig) => post(path, body, sig),
            get: async (url, sig) => { const { response, text } = await request(url, {}, sig); return { ok: response.ok, status: response.status, text }; },
            beacon: (path, body) => { try { fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), keepalive: true }); } catch (_) { /* expires server-side */ } },
            have: () => state.seq, maxFps: () => maxFps, showing: () => showing(), controlHeld: () => actions.inputOn,
            onOpen: lease => {
                transport = lease.transport === 'scene-delta' ? 'scene-delta' : 'full-snapshot';
                maxFps = Math.max(1, Math.min(lease.fps || 2, lease.maxFps || 2));
                counters.leases++;
                // The previous lease's input control died with it (the server resumed the bot), and the
                // scene it was applying is replaced by a whole one.
                actions.setInput(false);
            },
            onLost: error => { lastError = error.message; },
            onFrame: onFrame,
            onState: next => {
                setLinkState(next);
                if (next.phase === 'live') overlay.textContent = '';
                // A fatal answer (no bridge, an unknown transport) is the only thing a retry cannot fix.
                if (next.phase === 'ended' && !stopped) { lastError = next.message; overlay.textContent = 'Stream ended: ' + next.message; void destroy(); }
            },
        });
        await link.start();
        document.addEventListener('visibilitychange', wakeLink); addEventListener('pagehide', pageHide);
        setLinkState({ phase: 'live', message: '' });
    } catch(error) { await destroy();throw error; }

    async function decodeTerrain(self) {
        decodeBusy = true;
        const started = performance.now();
        overlay.textContent = terrainInfo ? overlay.textContent : 'Loading native cache terrain…';
        // The session's live loc changes ride along so the adapter's World shows opened doors,
        // cut trees and the like exactly as the game client applies them.
        const liveLocs = state.scene?.locs || [];
        const locs = liveLocs.map(([X, Z, Level, Type, Shape, Rotate]) => ({ X, Z, Level, Type, Shape, Rotate, Deleted: Type < 0 }));
        const snapshot = { x: self.x, z: self.z, level: self.level, centreX: self.centreX, centreZ: self.centreZ, selfSlot: self.slot, players: [], npcs: [], locs };
        const scenery = typeof adapter.sceneGpuMesh === 'function';
        const decodeRadius = renderDistance;
        const task = decoding.then(() => (scenery ? adapter.sceneGpuMesh(snapshot, decodeRadius, true) : adapter.terrainGpuMesh(snapshot, decodeRadius)));
        decoding = task.catch(() => {});
        try {
            const m = await task;
            if (stopped) return;
            const offX = self.x + 0.5, offZ = self.z + 0.5;
            if (scenery) {
                // Terrain and scenery per level, as static models; which levels are drawn is
                // decided every frame by the client's roof rule (roofTop).
                if (!m.terrain.some(level => level.length)) throw Error('No supported terrain faces at this location.');
                if (m.perLevel) {
                    for (const chunk of terrainChunks) renderer.dropModel(chunk.key);
                    terrainChunks = (m.chunks || []).map((chunk, i) => {
                        const key = 'chunk:' + i;
                        renderer.setModel(key, new Float32Array(chunk.vertices), new Float32Array(chunk.alpha));
                        return {key, level:chunk.level};
                    });
                    for (let l = 0; l < 4; l++) {
                        renderer.dropModel('terrain:' + l); renderer.dropModel('scenery:' + l);
                        renderer.setModel('terrain:' + l, widenTerrain(m.terrain[l] || [], FLOATS, 0, 0), new Float32Array(0));
                        renderer.setModel('scenery:' + l, new Float32Array(m.scenery[l] || []), new Float32Array(m.sceneryAlpha[l] || []));
                    }
                    roof = { flags: m.roofFlags, base: m.mapBase, level: self.level };
                    applyAnimatedLocs(m);
                } else {
                    renderer.setTerrain(widenTerrain([].concat(...m.terrain), FLOATS, offX, offZ));
                }
                chrome.setMinimapTerrain(widenTerrain(m.terrain[Math.min(self.level, m.terrain.length - 1)] || [], FLOATS, offX, offZ), FLOATS, textureAvgs);
                heightAt = gridHeights(m.heights, m.heightsSide, self.x, self.z, m.radius);
                if (!m.perLevel) {
                    renderer.dropModel('scenery');
                    renderer.setModel('scenery', new Float32Array(m.scenery), new Float32Array(m.sceneryAlpha || []));
                }
                terrainInfo = { triangles: m.terrainTriangles, texturedFaces: m.texturedTerrain, scenery: m.sceneryTriangles, sceneryTextured: m.texturedFaces,
                    nodes: m.nodes, levels: m.levels, underRoof: m.underRoof, adapterMs: m.ms, radius: m.radius, chunks: terrainChunks.length };
            } else {
                if (!m.triangles) throw Error('No supported terrain faces at this location.');
                const floats = m.floats || 7;
                const v = widenTerrain(m.vertices, floats, offX, offZ);
                renderer.setTerrain(v);
                chrome.setMinimapTerrain(v, FLOATS);
                heightAt = terrainHeights(m.vertices, self.x, self.z, floats);
                terrainInfo = { triangles: m.triangles, tiles: m.tiles, texturedFaces: m.texturedFaces, radius: decodeRadius };
            }
            terrainInfo.requestedRadius = decodeRadius;
            terrainOrigin = { x: self.x, z: self.z };
            terrainKey = `${self.centreX}:${self.centreZ}:${self.level}`;
            locsKey = JSON.stringify(liveLocs);
            // The cache texture atlas, once: texels the software rasterizer itself uses.
            if (!atlasInfo && typeof adapter.textureAtlas === 'function' && detail.highDetail) {
                const atlasTask = decoding.then(() => adapter.textureAtlas());
                decoding = atlasTask.catch(() => {});
                try {
                    const atlas = await atlasTask;
                    if (stopped) return;
                    renderer.setTextures(atlas);
                    atlasInfo = { textures: atlas.present.length, translucent: atlas.translucent.length, bytes: atlas.pixels.byteLength };
                } catch (e) { atlasInfo = { error: e.message || String(e) }; }
            }
            counters.uploads++; counters.decodes++; counters.decodeMs = Math.round(performance.now() - started);
            if (!counters.firstFrameMs) counters.firstFrameMs = Math.round(performance.now() - opened);
            dirty = true;
        } finally { decodeBusy = false; }
    }

    // applyAnimatedLocs uploads the decode's animated scenery and forgets what the last decode had.
    function applyAnimatedLocs(m) {
        const next = new Map();
        for (const a of m.animLocs || []) {
            const key = 'loc:' + a.key;
            if (!renderer.hasAnimation(key)) renderer.setAnimation(key, new Float32Array(a.base), new Float32Array(a.baseAlpha), a.positions.map(p => new Float32Array(p)), a.alphaPositions.map(p => new Float32Array(p)));
            const delays = a.delays.map(d => Math.max(1, d || 1));
            next.set(key, { delays, total: delays.reduce((t, d) => t + d, 0) });
        }
        for (const key of locKinds.keys()) if (!next.has(key)) renderer.dropAnimation(key);
        locKinds = next;
        locPlaced = (m.animPlacements || []).map(p => ({ kind: next.get('loc:' + p.key), key: 'loc:' + p.key, level: p.level, phase: p.phase, x: p.x, z: p.z, m: modelMatrix(p.x, p.y, p.z, p.yaw) }));
        counters.animatedLocs = locPlaced.length;
    }

    function needsDecode(self) {
        if (!terrainOrigin) return true;
        if (`${self.centreX}:${self.centreZ}:${self.level}` !== terrainKey) return true;
        if (typeof adapter.sceneGpuMesh === 'function' && JSON.stringify(state.scene?.locs || []) !== locsKey) return true;
        return terrainInfo?.requestedRadius !== renderDistance || Math.max(Math.abs(self.x - terrainOrigin.x), Math.abs(self.z - terrainOrigin.z)) >= Math.max(5, Math.floor(Math.min(renderDistance,terrainInfo.radius || renderDistance) / 4));
    }

    // One observation from the link (stream-link.js has checked its sequence and epoch). A delta
    // this view cannot apply throws, and the link then asks for a whole scene.
    function onFrame(body, text) {
        counters.frames++;
        if (body.delta) { counters.deltaFrames++; counters.deltaBytes += text.length; }
        else { counters.fullFrames++; counters.fullBytes += text.length; }
        if (body.lapsed && actions.inputOn) { actions.setInput(false); setLinkState({}); }
        try { state.applyFrame(body, performance.now()); }
        catch (e) { if (!body.delta) throw e; counters.resyncs++; throw e; }
        const s = state.scene;
        if (!s) return;
        const inv = state.takeInvChanges();
        if (inv.all || inv.slots.length) chrome.setInventory(s.inv, inv);
        if (state.chatDirty || state.statusDirty) { chrome.setStatusAndChat(s.status, s.chat, s.self.name || account); state.chatDirty = state.statusDirty = false; }
        // An unchanged delta (the usual case between two game ticks) repaints nothing.
        const d = body.delta;
        if (!d || d.self || d.upsert || d.remove || d.effects || d.inv || d.chat || d.status) dirty = true;
        // Decoding the map takes seconds on a busy machine; it must never hold the observation loop
        // (the lease is renewed by observing), so it runs beside it.
        if (!decodeBusy && performance.now() >= decodeRetryAt && needsDecode(s.self)) {
            decodeTerrain(s.self).catch(error => {
                // Not fatal: keep the last good map, show why, try again shortly.
                lastError = error.message || String(error); decodeRetryAt = performance.now() + 5000;
                overlay.textContent = 'Terrain error: ' + lastError;
            });
        }
        void loadModels();
        void loadAnimations();
        if (d?.ui || d?.self || d?.upsert || d?.remove || !d) uiDirty = true;
    }

    // Model keys: one GPU buffer per npc type, per distinct appearance block, per item/count.
    const npcKey = e => 'npc:' + e.t;
    const lookKey = look => (look ? 'look:' + look : null);
    const objKey = (id, count) => 'obj:' + id + ':' + count;

    // loadModels asks the adapter for every model the scene needs and does not have yet, a
    // batch at a time, through the same serialized decoder chain as the terrain.
    async function loadModels() {
        if (modelBusy || stopped || typeof adapter?.entityGpuMeshes !== 'function' || !state.scene) return;
        const now = performance.now(), want = [], seen = new Set();
        const need = (key, req) => {
            if (!key || seen.has(key) || renderer.hasModel(key)) return;
            seen.add(key);
            const info = modelInfo.get(key);
            if (info && !(info.retryAt <= now)) return;
            if (want.length < MODEL_BATCH) want.push({ key, ...req });
        };
        const looks = state.scene.looks || {};
        need(lookKey(looks.self), { kind: 'player', appearance: looks.self });
        for (const { e } of state.entitiesAt(now)) {
            if (e.k[0] === 'n') need(npcKey(e), { kind: 'npc', type: e.t });
            else if (looks[e.k]) need(lookKey(looks[e.k]), { kind: 'player', appearance: looks[e.k] });
        }
        for (const [, , id, count] of state.scene.objs || []) need(objKey(id, count), { kind: 'obj', id, count });
        if (!want.length) return;
        modelBusy = true;
        const started = performance.now();
        try {
            const task = decoding.then(() => adapter.entityGpuMeshes(want, 4000));
            decoding = task.catch(() => {});
            const res = await task;
            if (stopped) return;
            for (const [key, m] of Object.entries(res.meshes || {})) {
                renderer.setModel(key, new Float32Array(m.vertices), new Float32Array(m.alpha || []));
                const req = want.find(r => r.key === key);
                modelInfo.set(key, { height: m.minY || 1.5, triangles: m.triangles, textured: m.texturedFaces, anims: m.anims || null,
                    req: req ? { kind: req.kind, type: req.type, appearance: req.appearance } : null });
                counters.modelsLoaded++;
            }
            for (const key of res.pending || []) modelInfo.set(key, { retryAt: performance.now() + 3000 });
            counters.modelMs = Math.round(performance.now() - started);
            dirty = true;
        } catch (e) {
            counters.modelErrors++; lastModelError = e.message || String(e);
            for (const r of want) modelInfo.set(r.key, { retryAt: performance.now() + 10000 });
        } finally { modelBusy = false; }
    }

    // Animations: which sequence an entity shows right now, as the game chooses it — its
    // primary animation (the one the session decoded from NPC_INFO/PLAYER_INFO, replayed from
    // the start whenever it restarts) until that finishes, otherwise walk or run while an
    // observed step is being eased and ready when standing. Each (model, sequence) is
    // exported once by the adapter (animationGpuFrames) and kept on the GPU.
    const animInfo = new Map(); // "model|seq" -> {delays, loops, maxloops} | {retryAt} | {failed}
    let animBusy = false;
    const animKey = (key, seq) => key + '|' + seq;
    function chooseSequence(key, motion, a, now) {
        const info = modelInfo.get(key);
        if (!info?.anims) return null;
        if (a > 0 && motion) {
            const seq = a - 1, ak = animKey(key, seq), meta = animInfo.get(ak);
            if (!meta?.delays) return { seq, key: ak, frame: 0, loaded: false };
            const frame = primaryFrame(meta, now - motion.primaryStart);
            if (frame >= 0) return { seq, key: ak, frame, loaded: true };
        }
        // The movement sequence the client's routeMove picks: walk, or walk-back/left/right by
        // the angle between the way it moves and the way it faces (falling back to walk), run
        // at speed 8 and above (only when the entity has a run sequence), turn while turning in
        // place, else ready.
        const an = info.anims, kind = motion?.walk || 'ready';
        let seq = an.ready;
        if (kind === 'run') seq = an.run >= 0 ? an.run : an.walk;
        else if (kind === 'walk_b' || kind === 'walk_l' || kind === 'walk_r') seq = an[kind] >= 0 ? an[kind] : an.walk;
        else if (kind === 'walk') seq = an.walk;
        else if (kind === 'turn') seq = an.turn >= 0 ? an.turn : an.ready;
        if (seq == null || seq < 0) return null;
        const ak = animKey(key, seq), meta = animInfo.get(ak);
        if (!meta?.delays) return { seq, key: ak, frame: 0, loaded: false };
        return { seq, key: ak, frame: loopFrame(meta, now - opened), loaded: true };
    }
    async function loadAnimations() {
        if (animBusy || stopped || typeof adapter?.animationGpuFrames !== 'function' || !state.scene) return;
        const now = performance.now(), wanted = [];
        const want = (key, choice) => {
            if (!choice || choice.loaded || renderer.hasAnimation(choice.key)) return;
            const meta = animInfo.get(choice.key);
            if (meta && (meta.failed || !(meta.retryAt <= now))) return;
            const req = modelInfo.get(key)?.req;
            if (req && !wanted.some(w => w.ak === choice.key)) wanted.push({ ak: choice.key, req, seq: choice.seq });
        };
        const looks = state.scene.looks || {};
        const selfKey = lookKey(looks.self);
        // Prefetch each model's ready, walk and run sequences, so an entity never starts to
        // move in its bind pose while a sequence loads.
        const prefetch = key => {
            const an = modelInfo.get(key)?.anims;
            if (an) for (const seq of [an.ready, an.walk, an.run, an.walk_b, an.walk_l, an.walk_r]) if (seq >= 0) want(key, { seq, key: animKey(key, seq), loaded: false });
        };
        want(selfKey, chooseSequence(selfKey, state.selfMotion, state.scene.self.a || 0, now));
        prefetch(selfKey);
        for (const { e, motion } of state.entitiesAt(now)) {
            const key = e.k[0] === 'n' ? npcKey(e) : lookKey(looks[e.k]);
            want(key, chooseSequence(key, motion, e.a || 0, now));
        }
        for (const { e } of state.entitiesAt(now)) prefetch(e.k[0] === 'n' ? npcKey(e) : lookKey(looks[e.k]));
        if (!wanted.length) return;
        animBusy = true;
        try {
            for (const w of wanted.slice(0, 6)) {
                const task = decoding.then(() => adapter.animationGpuFrames(w.req, w.seq, 4000));
                decoding = task.catch(() => {});
                const res = await task;
                if (stopped) return;
                if (!res.frameCount) { animInfo.set(w.ak, res.pending ? { retryAt: performance.now() + 3000 } : { failed: true, layoutChanged: !!res.layoutChanged }); continue; }
                renderer.setAnimation(w.ak, new Float32Array(res.base), new Float32Array(res.baseAlpha || []),
                    res.positions.map(p => new Float32Array(p)), res.alphaPositions.map(p => new Float32Array(p)));
                animInfo.set(w.ak, { delays: res.delays, loops: res.loops, maxloops: res.maxloops, frames: res.frameCount });
                counters.animationsLoaded++;
            }
        } catch (e) {
            counters.modelErrors++; lastModelError = e.message || String(e);
            for (const w of wanted) animInfo.set(w.ak, { retryAt: performance.now() + 10000 });
        } finally { animBusy = false; dirty = true; }
    }

    // faceOf: the yaw an entity turns to for its face target, or null (Client.ts entityFace).
    function faceOf(m) {
        const t = targetTile(m.e?.f);
        return t ? faceYaw(m.x, m.z, t[0], t[1], m.dstYaw) : null;
    }
    // Where a face target stands: 'p<slot>' / 'n<index>', or the account itself.
    function targetTile(f) {
        if (!f) return null;
        const v = f - 1, selfSlot = state.scene?.self?.slot;
        if (v >= 32768 && v - 32768 === selfSlot) { const [x, z] = state.selfAt(performance.now()); return [x, z]; }
        const m = state.entities.get(v >= 32768 ? 'p' + (v - 32768) : 'n' + v);
        return m ? [m.x, m.z] : null;
    }

    const matrices = [];
    const instances = [], instancePool = [];
    let lastEvict = 0, lastAnimated = false, viewMoving = false, itemNames = null;
    // Client cycles (20 ms) since a game tick: whole ticks of 30 cycles, plus the time since this
    // observation's tick on the playout clock.
    function cyclesSince(tick, now) {
        return (state.scene.tick - tick) * 30 + Math.max(0, now - (state.frameAt + (state.offset || 0))) / 20 * (state.tickScale || 1);
    }
    const effectCycles = (e, now) => cyclesSince(e.tick, now);
    // The 2D layer over the scene (labels canvas): grid, item labels, health bars, hitsplats, spray, names.
    const labels = createOverlay();
    const fogState = { on: false, radius: 0 };
    const placedPool = [], shadowData = new Float32Array((MAX_MARKERS + 1) * 4);
    const overlayFrame = { canvas: null, dpr: 1, now: 0, scale: 1, project: (out, x, y, z) => renderer.projectInto(out, x, y, z), cycles: tick => cyclesSince(tick, overlayFrame.now),
        placed, placedCount: 0, self: { x: 0, z: 0, y: 0, top: 0, name: '', combat: null, motion: null }, objs: null, itemNames: null, heightAt: null, toggles: null, gameFrame: false, sprites: [], font: null };
    function drawLabels(sx, sz, selfY, now) {
        const f = overlayFrame, c = chrome.labels, s = f.self;
        f.canvas = c; f.dpr = Math.min(devicePixelRatio || 1, 2); f.now = now;
        f.scale = chrome.gameFrame ? Math.max(0.1, c.clientWidth / 512) : 1;
        f.placedCount = placed.length;
        s.x = sx; s.z = sz; s.y = selfY; s.top = modelInfo.get(lookKey(state.scene.looks?.self))?.height || MARKER.self.height;
        s.name = state.scene.self.name || account; s.combat = state.scene.self.combat; s.motion = state.selfMotion;
        f.objs = state.scene.objs; f.itemNames = itemNames; f.heightAt = heightAt; f.toggles = view?.toggles; f.gameFrame = !!chrome.gameFrame;
        f.sprites = hitSprites; f.font = nativeFonts.p11;
        labels.draw(f);
    }
    function visibleEffects() {
        const effects = [...(state.scene?.effects || [])];
        const add = (combat, x, z) => {
            const e = combat?.spot;
            if (e?.seen) effects.push({ ...e, x, z, start:e.delay, end:e.delay+250, projectile:false });
        };
        if (state.scene) { const [x,z] = state.selfAt(performance.now()); add(state.scene.self.combat,x,z); }
        for (const m of state.entities.values()) add(m.e.combat,m.x,m.z);
        return effects;
    }
    async function loadEffects(now) {
        if (effectBusy || stopped || !renderer || !adapter?.spotGpuFrames) return;
        const e = visibleEffects().find(e => effectCycles(e,now) <= e.end && !effectInfo.has(e.id) && !effectPending.has(e.id) && now >= (effectRetry.get(e.id) || 0));
        if (!e) return;
        effectBusy = true; effectPending.add(e.id);
        try {
            const info = await adapterCall(() => adapter.spotGpuFrames(e.id));
            if (stopped) return;
            if (!info) { effectRetry.set(e.id, now+5000); return; }
            const bytes = info.frames.reduce((n,f) => n + (f.opaque.length + f.alpha.length) * 4, 0);
            const budget = 32 * 1048576;
            if (bytes > budget) { effectRetry.set(e.id,now+60000); lastModelError='Effect exceeds GPU budget'; return; }
            let used = [...effectInfo.values()].reduce((n,i) => n + i.bytes,0);
            for (const [id,old] of [...effectInfo].sort((a,b) => (a[1].used || 0)-(b[1].used || 0))) {
                if (used + bytes <= budget) break;
                old.frames.forEach((_,i) => renderer.dropModel('effect:'+id+':'+i)); effectInfo.delete(id); used -= old.bytes;
            }
            info.bytes = bytes; info.used = now;
            info.frames.forEach((f,i) => renderer.setModel('effect:'+e.id+':'+i,new Float32Array(f.opaque),new Float32Array(f.alpha)));
            info.frames = info.frames.map(() => null);
            effectInfo.set(e.id,info); dirty = true;
        } catch (error) { effectRetry.set(e.id,now+5000); lastModelError = 'effect: '+error.message; }
        finally { effectBusy = false; effectPending.delete(e.id); }
    }
    // Anything of combat still moving: a health bar or hitsplat within its life, or spray in the air.
    function combatActive(now) {
        const active = c => c && ((c.seen && cyclesSince(c.tick, now) < 300) || (c.hits || []).some(h => h.seen && cyclesSince(h.tick, now) < 70));
        if (labels.fx.active() || active(state.scene?.self?.combat)) return true;
        for (const m of state.entities.values()) if (active(m.e.combat)) return true;
        return false;
    }

    function paint(now) {
        if (!renderer || !state.scene || !heightAt) return false;
        const t0 = performance.now();
        state.advance(now, faceOf);
        const [sx, sz] = state.selfAt(now);
        const ground = (x, z) => heightAt(x, z) ?? 0;
        const selfY = ground(sx + 0.5, sz + 0.5);
        if (view) {
            const t = performance.now(), dt = lastStep ? Math.min(0.1, (t - lastStep) / 1000) : 0;
            lastStep = t;
            const r = view.step(dt, { x: sx + 0.5, y: selfY, z: sz + 0.5 });
            camera = r.camera; viewMoving = r.moving;
        }
        const looks = state.scene.looks || {};
        instances.length = 0;
        let mi = 0;
        let animatedNow = false;
        const place = (key, x, y, z, yaw, motion, a) => {
            if (!key || !renderer.hasModel(key)) return false;
            const m = matrices[mi] ||= new Float32Array(16);
            const inst = instancePool[mi] ||= {key:'',m,anim:undefined,frame:undefined}; mi++;
            inst.key = key; modelMatrix(x, y, z, yaw, m);
            inst.anim = undefined; inst.frame = undefined;
            if (motion) {
                const choice = chooseSequence(key, motion, a || 0, now);
                if (choice?.loaded && renderer.hasAnimation(choice.key)) { inst.anim = choice.key; inst.frame = choice.frame; animatedNow = true; }
            }
            instances.push(inst);
            return true;
        };
        mesh.reset();
        if (terrainOrigin && renderer.hasModel('scenery')) place('scenery', terrainOrigin.x + 0.5, 0, terrainOrigin.z + 0.5, 0);
        if (terrainOrigin && roof) {
            // The client's roofCheck, from the account's (eased) tile and this camera.
            const cp = Math.cos(camera.pitch);
            const tx = camera.x ?? sx + 0.5, tz = camera.z ?? sz + 0.5;
            const eyeX = tx + camera.distance * Math.sin(camera.yaw) * cp, eyeZ = tz - camera.distance * Math.cos(camera.yaw) * cp;
            drawnTop = roofTop(roof.flags, roof.base, roof.level, Math.round(sx), Math.round(sz), eyeX, eyeZ, camera.pitch * 2048 / (2 * Math.PI));
            if (roofs === 'hide') drawnTop = roof.level;
            else if (roofs === 'show') drawnTop = 3;
            for (let l = 0; l <= drawnTop; l++) {
                place('terrain:' + l, terrainOrigin.x + 0.5, 0, terrainOrigin.z + 0.5, 0);
                place('scenery:' + l, terrainOrigin.x + 0.5, 0, terrainOrigin.z + 0.5, 0);
            }
            for (const chunk of terrainChunks) if (chunk.level <= drawnTop) place(chunk.key, terrainOrigin.x + 0.5, 0, terrainOrigin.z + 0.5, 0);
        }
        const selfKey = lookKey(looks.self);
        if (!place(selfKey, sx + 0.5, selfY, sz + 0.5, state.selfMotion?.yaw || 0, state.selfMotion, state.scene.self.a)) mesh.marker(sx + 0.5, sz + 0.5, selfY, MARKER.self);
        placed.length = 0;
        for (const motion of state.entities.values()) {
            const { e, x, z, yaw } = motion;
            const size = e.s || 1, cx = x + size / 2, cz = z + size / 2, y = ground(cx, cz);
            const kind = e.k[0] === 'n' ? MARKER.npc : MARKER.player;
            const key = e.k[0] === 'n' ? npcKey(e) : lookKey(looks[e.k]);
            // Facing comes from the motion model: the way it walks, or its face target, turned
            // gradually at the client's turn speed.
            const modelled = place(key, cx, y, cz, yaw, motion, e.a);
            if (modelled || mesh.marker(cx, cz, y, kind, size)) {
                const p = placedPool[placed.length] ||= {};
                p.e = e; p.x = x; p.z = z; p.cx = cx; p.cz = cz; p.y = y; p.kind = kind; p.npc = kind === MARKER.npc; p.top = modelled ? modelInfo.get(key)?.height : kind.height; p.motion = motion; p.size = size;
                placed.push(p);
            }
        }
        for (const [x, z, id, count] of state.scene.objs || []) place(objKey(id, count), x + 0.5, ground(x + 0.5, z + 0.5), z + 0.5, 0);
        // Animated scenery: each placed instance plays its own sequence from its own phase (the game
        // starts every one on a random frame), on the levels currently drawn and near the camera.
        if (locPlaced.length) {
            const cycle = now / CYCLE_MS, cxw = camera.x ?? sx + 0.5, czw = camera.z ?? sz + 0.5, reach = renderDistance + 4;
            for (const p of locPlaced) {
                if (p.level > drawnTop || Math.abs(p.x - cxw) > reach || Math.abs(p.z - czw) > reach) continue;
                const inst = instancePool[mi] ||= { key: '', m: matrices[mi] ||= new Float32Array(16), anim: undefined, frame: undefined }; mi++;
                inst.key = ''; inst.m.set(p.m); inst.anim = p.key; inst.frame = cycleFrame(p.kind.delays, p.kind.total, cycle + p.phase);
                instances.push(inst); animatedNow = true;
            }
        }
        for (const e of visibleEffects()) {
            const info = effectInfo.get(e.id); if (!info) continue; info.used = now;
            const cycles = effectCycles(e, now);
            const index = effectFrame(info, cycles - e.start, e.projectile);
            if (index < 0 || cycles > e.end) continue;
            let at;
            if (e.projectile) {
                let target;
                if (e.target > 0) { const m = state.entities.get('n' + (e.target - 1)); if (m) target = [m.x + (m.e.s || 1)/2, m.z + (m.e.s || 1)/2]; }
                else if (e.target < 0) { const slot = -e.target - 1; const m = state.entities.get('p' + slot); if (slot === state.scene.self.slot) target = [sx + .5, sz + .5]; else if (m) target = [m.x + .5, m.z + .5]; }
                at = projectileAt(e, cycles, ground, target);
            } else at = { x:e.x+.5, z:e.z+.5, y:ground(e.x+.5,e.z+.5)+e.height/128, yaw:0 };
            if (!at) continue;
            if (place('effect:' + e.id + ':' + index, at.x, at.y, at.z, at.yaw * 1024 / Math.PI)) {
                const m = instances[instances.length-1].m;
                if (at.pitch) { const c = Math.cos(at.pitch), s = Math.sin(at.pitch); for (let row=0; row<3; row++) { const y=m[4+row], z=m[8+row]; m[4+row]=y*c+z*s; m[8+row]=z*c-y*s; } }
                animatedNow = true;
            }
        }
        // Soft shadows under every actor, so models sit on the ground instead of floating over it.
        let shadows = 0;
        shadowData[shadows++] = sx + 0.5; shadowData[shadows++] = selfY; shadowData[shadows++] = sz + 0.5; shadowData[shadows++] = 0.4;
        for (const p of placed) { shadowData[shadows++] = p.cx; shadowData[shadows++] = p.y; shadowData[shadows++] = p.cz; shadowData[shadows++] = 0.38 * p.size; }
        const tg = view?.toggles;
        renderer.setShadows(shadowData, tg?.shadows === false ? 0 : shadows / 4);
        if (fogState.on !== (tg?.fog !== false) || fogState.radius !== renderDistance) {
            fogState.on = tg?.fog !== false; fogState.radius = renderDistance; renderer.setFog(fogState);
        }
        renderer.setMarkers(mesh.data, mesh.count);
        renderer.draw({ x: sx + 0.5, y: selfY, z: sz + 0.5, ...camera }, instances);
        drawLabels(sx, sz, selfY, now);
        chrome.drawMinimap(sx, sz, placed);
        counters.paints++;
        counters.paintWindow.push(now); while (counters.paintWindow.length && now - counters.paintWindow[0] > 1000) counters.paintWindow.shift();
        const paintMs = performance.now() - t0;
        counters.totalPaintMs = (counters.totalPaintMs || 0) + paintMs;
        counters.maxPaintMs = Math.max(counters.maxPaintMs, paintMs);
        dirty = false;
        lastAnimated = animatedNow;
        return true;
    }

    let lastOverlay = 0;
    // The status HUD: the dashboard's own status line and XP gained per hour since this view opened.
    let xpStart = null, lastHud = 0;
    function updateHud(now) {
        if (!hud || now - lastHud < 1000) return;
        lastHud = now;
        const tg = view?.toggles || {};
        hud.hidden = !tg.hud;
        // Item names for the ground item labels (I), from the cache config, once per new id.
        if (tg.items && typeof adapter?.menuInfo === 'function' && state.scene) {
            const missing = [...new Set((state.scene.objs || []).map(o => o[2]))].filter(id => !itemNames?.[id]);
            if (missing.length) {
                const task = decoding.then(() => adapter.menuInfo({ objs: missing.slice(0, 64) }));
                decoding = task.catch(() => {});
                task.then(info => { itemNames = { ...(itemNames || {}) }; for (const [id, o] of Object.entries(info.objs || {})) itemNames[id] = o.name; dirty = true; }).catch(() => {});
            }
        }
        const s = state.scene;
        if (!s || !tg.hud) return;
        const skills = s.ui?.skills || [];
        const xp = skills.reduce((t, k) => t + (k?.Generation ? k.XP : 0), 0);
        if (xpStart === null && xp > 0) xpStart = { xp, at: now };
        const hours = xpStart ? (now - xpStart.at) / 3600000 : 0;
        const rate = hours > 1 / 120 ? Math.round((xp - xpStart.xp) / hours) : null;
        const st = s.status || {};
        hud.textContent = [
            [s.self.name || account, st.bot, st.botState].filter(Boolean).join(' · '),
            st.activity ? st.activity.slice(0, 90) : '',
            [st.hpKnown ? `HP ${st.hp}/${st.maxHp}` : '', typeof st.run === 'number' ? `run ${st.run}%` : '', rate !== null ? `${rate.toLocaleString()} XP/h` : 'XP/h: measuring'].filter(Boolean).join(' · '),
            `camera: ${view?.cam.mode || 'follow'}${actions.inputOn ? ' · INPUT CONTROL' : ''} · ? for keys`
        ].filter(Boolean).join('\n');
    }
    function updateOverlay(now) {
        if (now - lastOverlay < 500 || !terrainInfo) return;
        lastOverlay = now;
        const st = renderer?.stats || {};
        const avgDelta = counters.deltaFrames ? Math.round(counters.deltaBytes / counters.deltaFrames) : 0;
        const avgFull = counters.fullFrames ? Math.round(counters.fullBytes / counters.fullFrames) : 0;
        overlay.textContent = `webgl-cache-terrain: ${st.triangles | 0} triangles (levels 0-${drawnTop} drawn${drawnTop < 3 ? ', roofs removed' : ''}; ${st.modelTriangles | 0} model incl. terrain/scenery, ${st.markerTriangles | 0} marker), ${st.drawCalls | 0} draw calls, ${st.instances | 0} models of ${state.entities.size + 1} entities\n` +
            `seq ${link?.seq ?? 0} · ${linkState.phase === 'live' ? '' : linkState.phase + ' · '}${transport} · ${maxFps}/s observations · whole ${avgFull} B, delta ${avgDelta} B · painting ${counters.paintWindow.length} fps (cap ${renderFps}) · GPU ${(st.gpuBytes / 1048576).toFixed(1)} MB\n` +
            `${st.animatedInstances | 0} animated models (${st.animations | 0} sequences, ${((st.animationBytes || 0) / 1048576).toFixed(1)} MB); ${terrainInfo.nodes | 0} scenery models on ${terrainInfo.levels || 1} level(s)${terrainInfo.underRoof ? ' (under a roof)' : ''}. ${atlasInfo?.textures ? `${atlasInfo.textures} cache textures; ${terrainInfo.texturedFaces} textured ground faces` : 'Textures drawn as average colours'}; ${chrome.gameFrame ? `game interfaces by the reference client (${uiInfo ? uiInfo.ms + ' ms' : 'loading'})` : 'no game interfaces'}; ${actions.inputOn ? 'input control held' : 'no input'}.`;
    }

    // The game's own interfaces, redrawn by the adapter when the interface state, the entities
    // on the minimap or the camera yaw (the minimap rotates with it) change — at most ~7/s.
    const rsYaw = () => Math.round(camera.yaw * 1024 / Math.PI) & 2047;
    async function drawInterfaces() {
        if (uiBusy || stopped || !chrome.gameFrame || !state.scene || !terrainOrigin || !showing()) return;
        const yaw = rsYaw();
        if (!uiDirty && yaw === uiLastYaw) return;
        uiBusy = true; uiDirty = false; uiLastYaw = yaw;
        const snapshot = actions.uiSnapshot();
        const started = performance.now();
        try {
            const task = decoding.then(() => adapter.interfaceFrame(snapshot, { tab: uiTab, yaw, typed: actions.inputOn ? actions.typed : '', use: actions.inputOn ? actions.useItem : null }));
            decoding = task.catch(() => {});
            const res = await task;
            if (stopped || !res) return;
            chrome.setInterface(res.canvas);
            chrome.setUi?.(snapshot.ui);
            uiInfo = { ms: Math.round(performance.now() - started), modal: res.modal, side: res.side, chatModal: res.chatModal, tab: res.tab, modalPixels: res.modalPixels };
            counters.interfaceFrames++;
        } catch (e) { lastModelError = 'interface: ' + (e.message || e); counters.modelErrors++; }
        finally { setTimeout(() => { uiBusy = false; }, 140); }
    }

    function frame(now) {
        if (stopped) return;
        raf = requestAnimationFrame(frame);
        if (!showing()) return;
        updateOverlay(now);
        updateHud(now);
        // Entity models and animations not drawn for a minute are released (a crowded bank
        // passes hundreds of appearances); they are re-requested if they come back.
        if (renderer && now - lastEvict > 10000) {
            lastEvict = now;
            for (const k of renderer.evictModels(60000, k => /^(terrain|scenery|chunk:|loc:)/.test(k))) {
                modelInfo.delete(k);
                if (k.startsWith('effect:')) { const id = Number(k.split(':')[1]); const info = effectInfo.get(id); info?.frames.forEach((_,i) => renderer.dropModel('effect:'+id+':'+i)); effectInfo.delete(id); }
            }
        }
        void loadEffects(now);
        if (renderer?.needsRedraw()) dirty = true;
        if (chrome.gameFrame) void drawInterfaces();
        if (now + 1 < nextPaint) return;
        // Animated models need every frame; otherwise paint only on change or motion.
        if (!dirty && !dragging && !lastAnimated && !combatActive(now) && !viewMoving && !state.animating(performance.now())) { lastStep = 0; return; }
        nextPaint = advancePaintDeadline(nextPaint,now,renderFps);
        paint(performance.now());
    }
    raf = requestAnimationFrame(frame);
    const instance = {
        account, sceneRenderer:'webgl-cache-terrain', skin: eraSkin,
        screenshot,
        setViewSettings(settings = {}) {
            if (!view || stopped) return;
            for (const key of ['grid', 'names', 'items', 'hud', 'distances', 'fog', 'shadows']) {
                if (typeof settings[key] === 'boolean') view.toggles[key] = settings[key];
            }
            if (['follow', 'free', 'top-down'].includes(settings.mode)) {
                view.cam.mode = settings.mode; view.cam.free = null;
            }
            if (Number.isFinite(settings.distance)) view.cam.targetDistance = Math.max(6, Math.min(45, settings.distance));
            if (Number.isFinite(settings.pitch)) view.cam.pitch = Math.max(128 * Math.PI / 1024, Math.min(383 * Math.PI / 1024, settings.pitch));
            if (Number.isFinite(settings.yaw)) view.cam.yaw = settings.yaw % (2 * Math.PI);
            if ([15, 30, 60].includes(settings.fps)) renderFps = settings.fps;
            if ([16,24,32,48].includes(settings.renderDistance)) renderDistance = settings.renderDistance;
            if ([0.75,1,1.5,2].includes(settings.resolution)) { resolution = settings.resolution; renderer.setResolution(resolution); }
            if (['auto','hide','show'].includes(settings.roofs)) roofs = settings.roofs;
            dirty = true;
        },
        // `skin` is in stats() as well as on the instance so an OBS view — which has no
        // panel around it to compare against — can still report which era it is drawing.
        stats:()=>({ stopped, skin: eraSkin, renderFps, renderDistance, resolution, roofs, self: state.scene?.self ? { x: state.scene.self.x, z: state.scene.self.z, level: state.scene.self.level, combat: state.scene.self.combat } : null, frames: counters.frames, uploads: counters.uploads, lastSeq: link?.seq ?? 0, lastError, session: link?.session ?? null, phase: linkState.phase, link: link ? { ...link.stats } : null, leases: counters.leases, transport, maxFps,
            decodes: counters.decodes, decodeMs: counters.decodeMs, firstFrameMs: counters.firstFrameMs, throttled: counters.throttled, resyncs: counters.resyncs,
            fullFrames: counters.fullFrames, fullBytes: counters.fullBytes, deltaFrames: counters.deltaFrames, deltaBytes: counters.deltaBytes,
            paints: counters.paints, paintFps: counters.paintWindow.filter(t => performance.now() - t < 1000).length, avgPaintMs: Math.round((counters.totalPaintMs || 0) / Math.max(1,counters.paints) * 10) / 10, maxPaintMs: Math.round(counters.maxPaintMs * 10) / 10,
            entities: state.entities.size, inventorySlots: state.scene?.inv?.filter(s => s[1] > 0).length ?? 0,
            gl: renderer ? { ...renderer.stats } : null, terrain: terrainInfo, highDetail: !!detail.highDetail, atlas: atlasInfo, drawnTop,
            modelsLoaded: counters.modelsLoaded, modelErrors: counters.modelErrors, modelMs: counters.modelMs, lastModelError,
            combatSpray: labels.fx.count, animationsLoaded: counters.animationsLoaded, animatedLocs: counters.animatedLocs | 0, animatedInstances: renderer?.stats.animatedInstances | 0,
            sequences: Object.fromEntries([...animInfo].map(([k, v]) => [k.split('|').pop() + '@' + k.slice(0, 12), v.delays ? `ok ${v.frames}f` : v.failed ? (v.layoutChanged ? 'failed: frame layout changes' : 'failed') : 'pending'])),
            selfSequence: (() => { const k = lookKey(state.scene?.looks?.self); const c = k && chooseSequence(k, state.selfMotion, state.scene?.self?.a || 0, performance.now()); return c ? { tile: state.scene?.self ? [state.scene.self.x, state.scene.self.z] : null, seq: c.seq, loaded: c.loaded, frame: c.frame, moving: isMoving(state.selfMotion), walk: state.selfMotion?.walk, speed: state.selfMotion?.speed, queued: state.selfMotion?.route.length, anims: modelInfo.get(k)?.anims } : null; })(),
            gameFrame: !!chrome.gameFrame, interfaceFrames: counters.interfaceFrames, interface: uiInfo,
            camera: view ? { ...view.cam, free: undefined } : null, toggles: view ? { ...view.toggles } : null, showing: showing(), screenshots: counters.screenshots,
            nativeFonts: Object.keys(nativeFonts), hitSprites: hitSprites.filter(Boolean).length, effectsLoaded: effectInfo.size,
            input: actions.inputOn, inputActions: counters.inputActions, inputLog: actions.inputLog.slice(-10), typed: actions.typed, useItem: actions.useItem, uiTab,
            ui: state.scene?.ui ? { main: state.scene.ui.main, side: state.scene.ui.side, chatModal: state.scene.ui.chatModal, countDialog: !!state.scene.ui.countDialog,
                inventories: Object.fromEntries(Object.entries(state.scene.ui.inventories || {}).map(([k, v]) => [k, (v?.Slots || []).map(s => s.Count > 0 ? [s.ID, s.Count] : 0)])),
                chat: (state.scene.ui.chat || []).slice(-5).map(l => l.text), varps: state.scene.ui.varps || {} } : null }),
        // Input: the server decides. It acknowledges only on a -stream-input build, with the
        // view lease, no bank/trade open and no MuleReceiver (unless force), after stopping
        // the account's bot, which it resumes on release.
        async setInputEnabled(enabled, options = {}) {
            if (stopped) return false;
            if (!link?.session) throw Error('The view is reconnecting; try again in a moment.');
            let reply;
            try {
                reply = await request('/api/stream/input', { method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ session: link.session, enabled: !!enabled, force: !!options.force }) }, controller.signal);
            } catch (error) { await destroy(); throw error; }
            const res = reply.response;
            let body;
            try { body=JSON.parse(reply.text); } catch (error) { await destroy(); throw Error('Invalid input acknowledgement'); }
            // A refusal (a bank is open, the account is a mule, a rival holds the lease) is an answer,
            // not an ambiguity: the view stays up and the reason is shown.
            if (!res.ok) throw Object.assign(Error(body?.error || `HTTP ${res.status}`), { refused: true });
            if (typeof body?.acknowledged !== 'boolean') { await destroy(); throw Error('Invalid input acknowledgement'); }
            actions.setInput(!!enabled && body.acknowledged === true);
            uiDirty = true; setLinkState({});
            return body.acknowledged === true;
        },
        destroy
    };
    // Read-only hook for the browser checks (verify-stream-view.cjs): the provider's counters.
    host.__streamStats = instance.stats;
    // Read-only hook for the browser checks: where the animated scenery currently stands on the canvas.
    host.__streamLocPoints = () => locPlaced.map(p => ({ key: p.key, frame: cycleFrame(p.kind.delays, p.kind.total, performance.now() / CYCLE_MS + p.phase), frames: p.kind.delays.length, point: renderer?.project(p.x, p.m[13] + 0.6, p.z) })).filter(p => p.point);
    host.__streamPickTargets = () => placed.map(p => {
        const point = renderer.project(p.cx,p.y+(p.top || 1.5)*0.5,p.cz);
        return {key:p.e.k,point};
    });
    // Read-only hook for the browser checks (verify-stream-interface.cjs): the game's option
    // list at a game-window point, exactly what a right click there would show. Sends nothing.
    host.__streamMenuAt = async (gx, gy) => {
        if (gx > 4 && gy > 4 && gx < 516 && gy < 338 && state.scene?.ui?.main === -1) {
            const r = canvas.getBoundingClientRect();
            return ((await actions.worldMenu((gx - 4) / 512 * r.width, (gy - 4) / 334 * r.height)) || []).map(i => ({ label: i.label, unavailable: false, local: false }));
        }
        return actions.interfaceMenuAt(gx, gy);
    };
    return instance;
}
