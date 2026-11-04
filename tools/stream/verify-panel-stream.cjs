// node tools/stream/verify-panel-stream.cjs PANEL_URL ACCOUNT [OUTPUT_PNG] [--preinstall PROVIDER ADAPTER]
//
// End-to-end check of the INSTALLED stream client provider through the dashboard's own
// Stream client panel and its real "Load stream client" button — the path the owner
// uses. Run from the repo root. Works against a -fake panel or a real fleet panel that
// was started with -stream-client (plus -capture-renderer/-capture-cache for the
// cache-terrain provider).
//
// Read-only with respect to the game: it seeds a one-panel layout into THIS headless
// browser's own storage (never the server's saved layout, never the operator's browser),
// takes and releases the single stream VIEW lease, opens no WebSocket and sends no game
// action. Prints counters only — no chat, inventory or appearance data. The optional PNG
// is the provider's own GPU canvas; keep it local and out of Git.
//
// --preinstall PROVIDER ADAPTER is a PRE-DEPLOY check against a panel that is already
// running WITHOUT a provider (e.g. the live fleet before its restart): inside this one
// headless browser only, it serves PROVIDER at /stream/client.js and ADAPTER at
// /capture/client.js, advertises PROVIDER's own manifest identity in /api/meta, and adds
// the blob: image allowance a -stream-client build sends on the dashboard page. The
// observation bridge (/api/stream/open|frame|close) and the snapshot data stay the
// running panel's own, unmodified. It proves the provider renders that fleet's real
// data; it does not prove the panel binary itself was started with the right flags.
const fs = require('fs');
const path = require('path');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');

const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';

(async () => {
    const args = process.argv.slice(2);
    const pre = args.indexOf('--preinstall');
    const preinstall = pre >= 0 ? { provider: args[pre + 1], adapter: args[pre + 2] } : null;
    if (pre >= 0) args.splice(pre, 3);
    const [base, account, output] = args;
    if (preinstall && (!preinstall.provider || !preinstall.adapter)) throw Error('--preinstall needs PROVIDER and ADAPTER paths');
    if (!base || !account) throw Error('Provide the panel URL and an in-game account');
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    try {
        const context = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
        const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: Number(process.env.STREAM_W || 12), h: Number(process.env.STREAM_H || 8), accounts: [account], settings: { fps: process.env.STREAM_FPS || '30', skin: process.env.STREAM_SKIN || '2004' } }] };
        await context.addInitScript(([key, value]) => { try { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } } catch (_) { /* storage refused */ } }, [LAYOUT_KEY, JSON.stringify(layout)]);
        if (preinstall) {
            const manifest = JSON.parse(fs.readFileSync(preinstall.provider + '.manifest.json', 'utf8'));
            await context.route('**/stream/client.js', route => route.fulfill({ contentType: 'text/javascript', body: fs.readFileSync(preinstall.provider, 'utf8') }));
            // The provider's sibling modules (scene-state.js, ...) are served beside it.
            await context.route(url => { const p = new URL(url).pathname; return p.startsWith('/stream/') && p !== '/stream/client.js'; }, route => {
                const file = path.join(path.dirname(preinstall.provider), path.basename(new URL(route.request().url()).pathname));
                return fs.existsSync(file) ? route.fulfill({ contentType: 'text/javascript', body: fs.readFileSync(file, 'utf8') }) : route.fulfill({ status: 404, body: '' });
            });
            await context.route('**/capture/client.js', route => route.fulfill({ contentType: 'text/javascript', body: fs.readFileSync(preinstall.adapter, 'utf8') }));
            await context.route('**/api/meta', async route => {
                const response = await route.fetch(); const body = await response.json();
                // The era skins the PROVIDER's own manifest declares travel with it, the
                // same way its renderer identity does: a preinstall check against a panel
                // whose binary predates skins must still exercise the skin the provider
                // being checked says it draws (internal/controlpanel/streamskin.go).
                body.streamClient = { ...(body.streamClient || {}), sceneRenderer: manifest.sceneRenderer, description: manifest.description,
                    ...(manifest.skins ? { skins: manifest.skins, skin: (manifest.skins.find(s => s.default) || manifest.skins[0] || {}).id } : {}) };
                await route.fulfill({ response, json: body });
            });
            await context.route(url => new URL(url).pathname === '/', async route => {
                const response = await route.fetch(); const headers = { ...response.headers() };
                headers['content-security-policy'] = (headers['content-security-policy'] || '').replace("img-src 'self' data:", "img-src 'self' data: blob:");
                await route.fulfill({ response, headers });
            });
        }
        const page = await context.newPage();
        let sockets = 0; const errors = [];
        page.on('websocket', () => sockets++);
        page.on('pageerror', e => errors.push(String(e.message || e)));
        // The reference client's shell tries to add an inline style the panel CSP blocks; the
        // attempt is harmless (the adapter never shows that shell), so it is counted apart.
        let blockedInlineStyles = 0;
        page.on('console', m => { if (m.type() !== 'error') return; if (/Applying inline style violates/.test(m.text())) blockedInlineStyles++; else errors.push('console: ' + m.text()); });
        const describe = () => page.evaluate(() => { const s = document.querySelector('section.window[data-id="stream"]'); return { message: s?.querySelector('p[role="status"]')?.textContent, overlay: s?.querySelector('canvas')?.parentElement?.querySelector('[role="status"]')?.textContent }; }).catch(() => ({}));
        await page.goto(new URL('/?layout=session' + (process.env.STREAM_QUERY || ''), base).href); // never the server's saved layout
        const meta = await page.evaluate(async () => (await (await fetch('/api/meta', { cache: 'no-store' })).json()).streamClient);
        if (!meta?.sceneRenderer) throw Error('This panel advertises no installed stream client provider (/api/meta.streamClient.sceneRenderer is empty)');
        const panel = page.locator('section.window[data-id="stream"]');
        const load = panel.getByRole('button', { name: 'Load stream client' });
        await load.waitFor({ timeout: 30000 });
        // Click Load and wait for the provider's first drawn frame. 'closed' means the panel
        // itself released the view because the account left game or reconnected (its
        // session epoch changed) — correct behaviour, and routine on a -fake fleet, whose
        // simulated accounts drop every ~45s — so that alone is retried.
        let clicked = 0;
        const loadOnce = async () => {
            await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000 });
            await load.click();
            clicked = Date.now();
            const handle = await page.waitForFunction(() => {
                const s = document.querySelector('section.window[data-id="stream"]');
                const status = s.querySelector('div[role="status"]')?.textContent || '';
                const message = s.querySelector('p[role="status"]')?.textContent || '';
                if (/triangles|seq/i.test(status)) return 'frame';
                if (/unavailable/i.test(message)) return 'unavailable';
                if (/^Installed provider|^Load the selected/.test(message)) return 'closed';
                return false;
            }, null, { timeout: 120000, polling: 250 }).catch(async e => { throw Error(e.message + ' ' + JSON.stringify(await describe()) + ' ' + errors.join(' | ')); });
            return handle.jsonValue();
        };
        let outcome = 'closed', retriedAfterStateChange = 0;
        for (let attempt = 0; attempt < 5 && outcome === 'closed'; attempt++) {
            outcome = await loadOnce();
            if (outcome === 'closed') retriedAfterStateChange++;
        }
        const firstFrameMs = Date.now() - clicked; // click to first drawn frame, includes adapter cache init
        if (outcome !== 'frame') throw Error('Panel reported: ' + JSON.stringify(await describe()) + (errors.length ? ' | page errors: ' + errors.join('; ') : ''));
        // Let a few observations flow, recording what the provider says, then read its GPU
        // canvas back. A provider that ended itself in that time is reported, not hidden.
        await page.evaluate(() => {
            window.__streamLog = [];
            const s = document.querySelector('section.window[data-id="stream"]');
            new MutationObserver(() => {
                const text = (s.querySelector('div[role="status"]')?.textContent || '') + ' | ' + (s.querySelector('p[role="status"]')?.textContent || '');
                if (window.__streamLog[window.__streamLog.length - 1] !== text) window.__streamLog.push(text);
            }).observe(s, { subtree: true, childList: true, characterData: true });
        });
        await page.waitForTimeout(3000);
        if (!(await panel.locator('canvas').count())) throw Error('The provider ended within 3s of its first frame: ' + JSON.stringify(await page.evaluate(() => window.__streamLog)) + ' ' + errors.join(' | '));
        // The renderer's drawing buffer is not preserved, so redraw and read it back in the
        // SAME task: a one-pixel camera drag makes the provider call draw() synchronously.
        const proof = await page.evaluate(() => {
            const canvas = document.querySelector('section.window[data-id="stream"] canvas');
            const gl = (canvas.getContext('webgl2') || canvas.getContext('webgl'));
            const ext = gl && gl.getExtension('WEBGL_debug_renderer_info');
            const overlay = document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || '';
            canvas.setPointerCapture = () => {}; // a synthetic event has no active pointer to capture
            canvas.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 100, bubbles: true }));
            window.dispatchEvent(new PointerEvent('pointermove', { clientX: 101, clientY: 100 }));
            const pixels = new Uint8Array(canvas.width * canvas.height * 4);
            gl.readPixels(0, 0, canvas.width, canvas.height, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
            let visiblePixels = 0;
            for (let i = 0; i < pixels.length; i += 4) if (pixels[i] !== pixels[0] || pixels[i + 1] !== pixels[1] || pixels[i + 2] !== pixels[2]) visiblePixels++;
            // Specks: the owner's complaint is "a lot of black specks", so what is counted is
            // what that looks like — tiny ISLANDS of dark pixels inside drawn geometry.
            //
            // The first version of this required a near-black pixel's four orthogonal
            // neighbours to be clearly lit. That is a check that cannot fail on the real
            // thing: measured against a live Catherby session it reported 0 while the same
            // frame held 111 dark blobs of one or two pixels, because a speck that is 2 px
            // wide, or that sits on darker grass or water, never has four lit neighbours.
            // It now counts 4-connected dark components and reports the small ones, which is
            // a number that moves when the rendering changes.
            const W = canvas.width, H = canvas.height;
            const at = (x, y) => { const o = ((H - 1 - y) * W + x) * 4; return pixels[o] + pixels[o + 1] + pixels[o + 2]; };
            const DARK = 60, dark = new Uint8Array(W * H);
            let darkPixels = 0;
            for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) if (at(x, y) <= DARK) { dark[y * W + x] = 1; darkPixels++; }
            const seen = new Uint8Array(W * H), specks = [], sizes = [], stack = [];
            let darkBlobs = 0;
            for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
                const i0 = y * W + x;
                if (!dark[i0] || seen[i0]) continue;
                stack.length = 0; stack.push(i0); seen[i0] = 1;
                let n = 0, touchesEdge = false;
                while (stack.length) {
                    const i = stack.pop(); n++;
                    const ix = i % W, iy = (i / W) | 0;
                    if (ix === 0 || iy === 0 || ix === W - 1 || iy === H - 1) touchesEdge = true;
                    if (ix > 0 && dark[i - 1] && !seen[i - 1]) { seen[i - 1] = 1; stack.push(i - 1); }
                    if (ix < W - 1 && dark[i + 1] && !seen[i + 1]) { seen[i + 1] = 1; stack.push(i + 1); }
                    if (iy > 0 && dark[i - W] && !seen[i - W]) { seen[i - W] = 1; stack.push(i - W); }
                    if (iy < H - 1 && dark[i + W] && !seen[i + W]) { seen[i + W] = 1; stack.push(i + W); }
                }
                darkBlobs++;
                // An island of at most 4 px that does not run off the edge of the viewport is a
                // speck; a big dark area (a shadow, a doorway, the sky past the map) is not.
                if (n <= 4 && !touchesEdge) { sizes.push(n); if (specks.length < 12) specks.push([x, y]); }
            }
            const speckCount = sizes.length;
            window.__specks = specks;
            const png = canvas.toDataURL('image/png');
            window.dispatchEvent(new PointerEvent('pointerup', {}));
            return { specks: speckCount, speckSample: specks.slice(0, 12), darkPixels, darkBlobs, width: canvas.width, height: canvas.height, gpu: ext ? gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER), glError: gl.getError(), overlay, png, visiblePixels };
        });
        const visiblePixels = proof.visiblePixels;
        if (output) { fs.mkdirSync(path.dirname(path.resolve(output)), { recursive: true }); fs.writeFileSync(output, Buffer.from(proof.png.split(',')[1], 'base64')); }
        // Chrome beside the scene, when the provider draws one (cache-terrain does; the tile
        // provider does not): backpack slots with an icon, minimap pixels, status/chat text.
        const chrome = await page.evaluate(() => {
            const s = document.querySelector('section.window[data-id="stream"]');
            const slots = [...s.querySelectorAll('[data-slot]')];
            const map = s.querySelector('canvas[aria-label^="Minimap"]');
            let mapPixels = 0;
            if (map) { const d = map.getContext('2d').getImageData(0, 0, map.width, map.height).data; for (let i = 0; i < d.length; i += 4) if (d[i] || d[i + 1] || d[i + 2]) mapPixels++; }
            const chat = s.querySelector('[aria-label="Status and recent chat"]');
            const frame = s.querySelector('canvas[aria-label^="Game interfaces"]');
            let interfacePixels = 0;
            if (frame) { const d = frame.getContext('2d').getImageData(0, 0, frame.width, frame.height).data; for (let i = 3; i < d.length; i += 4) if (d[i]) interfacePixels++; }
            return { gameFrame: !!frame, interfacePixels, backpackSlots: slots.length, backpackItems: slots.filter(x => x.querySelector('img,svg')).length, minimapPixels: mapPixels, statusLines: chat ? chat.querySelectorAll('div').length : 0, jsHeapMB: performance.memory ? Math.round(performance.memory.usedJSHeapSize / 1048576) : null };
        });
        // The era skin actually mounted. The panel writes its status line from the value the
        // PROVIDER returned, not from what was requested, and it refuses the mount outright
        // when the two differ — so reading it here is reading the provider's own answer.
        const mountedSkin = await page.evaluate(() => {
            const s = document.querySelector('section.window[data-id="stream"]');
            return [...s.querySelectorAll('[data-skin]')].map(n => n.dataset.skin)[0] || '';
        });
        if (output && process.env.STREAM_DRAG) {
            // Rotate the camera by a drag and let the interfaces redraw: the minimap must turn with the scene.
            await page.evaluate(dx => {
                const c = document.querySelector('section.window[data-id="stream"] canvas'); c.setPointerCapture = () => {};
                c.dispatchEvent(new PointerEvent('pointerdown', { clientX: 300, clientY: 200, bubbles: true }));
                window.dispatchEvent(new PointerEvent('pointermove', { clientX: 300 + dx, clientY: 200 }));
                window.dispatchEvent(new PointerEvent('pointerup', {}));
            }, Number(process.env.STREAM_DRAG));
            await page.waitForTimeout(1500);
            const root = panel.locator('canvas').first().locator('xpath=ancestor::div[contains(@style,"grid-template-columns") or contains(@style,"inset: 0")][1]');
            if (await root.count()) await root.screenshot({ path: output.replace(/\.png$/i, '') + '-dragged.png' });
        }
        if (output) { const host = panel.locator('canvas').first().locator('xpath=ancestor::div[contains(@style,"grid-template-columns") or contains(@style,"inset: 0")][1]'); if (await host.count()) await host.screenshot({ path: output.replace(/\.png$/i, '') + '-window.png' }); }
        // Close must free the server's single view lease at once: a second Load succeeds.
        await panel.getByRole('button', { name: 'Close client' }).click();
        const canvasesAfterClose = await panel.locator('canvas').count();
        let reopened = 'closed';
        for (let attempt = 0; attempt < 5 && reopened === 'closed'; attempt++) reopened = await loadOnce();
        const closeButton = panel.getByRole('button', { name: 'Close client' });
        if (await closeButton.isEnabled()) await closeButton.click();
        const inputDisabled = await panel.getByRole('button', { name: /input control/ }).isDisabled();
        const result = {
            sceneRenderer: meta.sceneRenderer, sessionInput: meta.sessionInput, maxFps: meta.maxFps, frameTransport: meta.frameTransport,
            // Era skins: what the build ADVERTISES, what this run ASKED for, and what the
            // panel says it actually mounted. A provider that quietly drew a different era
            // would show up as requestedSkin != mountedSkin (the panel refuses that mount,
            // so in practice this run would have failed at Load instead).
            advertisedSkins: (meta.skins || []).map(s => s.id), defaultSkin: meta.skin || '',
            requestedSkin: process.env.STREAM_SKIN || '2004', mountedSkin,
            firstFrameMs, specks: proof.specks, speckSample: proof.speckSample, darkPixels: proof.darkPixels, darkBlobs: proof.darkBlobs, width: proof.width, height: proof.height, gpu: proof.gpu, glError: proof.glError, visiblePixels,
            overlay: proof.overlay.replace(/\s+/g, ' ').trim(), canvasesAfterClose, reopened: reopened === 'frame', retriedAfterStateChange,
            inputDisabled, gameSockets: sockets, pageErrors: errors.length, blockedInlineStyles, preinstalled: !!preinstall, chrome
        };
        console.log(JSON.stringify(result));
        // A provider that draws the game's own window must actually have drawn it. This
        // fixture reported chrome.interfacePixels from the start but never asserted on it,
        // which is how a view with no minimap, inventory or chatbox could still print a
        // healthy-looking line. It is only required of a provider that claims a game frame;
        // the tile provider draws no chrome and is not held to it.
        const chromeMissing = chrome.gameFrame && !chrome.interfacePixels;
        if (chromeMissing) console.error('the provider built a game frame but drew no interfaces in it (minimap, inventory, chatbox)');
        if (sockets || proof.glError || !visiblePixels || canvasesAfterClose || !result.reopened || !inputDisabled || errors.length || chromeMissing) {
            if (errors.length) console.error(errors.join('\n'));
            process.exitCode = 1;
        }
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
