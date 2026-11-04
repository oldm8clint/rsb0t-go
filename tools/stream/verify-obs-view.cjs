// node tools/stream/verify-obs-view.cjs PANEL_URL ACCOUNT [OUTPUT_PNG]
//
// Loads /stream/view (internal/controlpanel/streamview.go) exactly as an OBS Browser
// Source would: a bare URL, a 1920x1080 viewport, no dashboard, no saved layout and no
// seeded storage. It proves the page paints the real session's view, opens no game
// WebSocket, shows no scrollbars, and frees the single stream VIEW lease the moment the
// source closes.
//
// Read-only with respect to the game: it takes and releases the view lease and sends
// nothing. Point it at a -fake panel or at a local-engine panel you own, never at the live
// fleet panel without the owner's go-ahead. Prints counters only; the optional PNG is the
// full browser-source frame — keep it local and out of Git.
const fs = require('fs');
const path = require('path');

// The reference checkout sits beside the repo, but an agent working in a git worktree is
// one directory deeper, so a fixed "../../../rsb0t-main" resolves to nothing there. Walk
// up instead and say plainly what was looked for when it is missing.
function playwright() {
    for (let dir = __dirname; ; dir = path.dirname(dir)) {
        const candidate = path.join(dir, 'rsb0t-main', 'node_modules', 'playwright-core');
        if (fs.existsSync(candidate)) return require(candidate);
        if (path.dirname(dir) === dir) break;
    }
    throw Error('playwright-core not found: expected <some parent>/rsb0t-main/node_modules/playwright-core');
}

(async () => {
    const [base, account, output] = process.argv.slice(2);
    if (!base || !account) throw Error('Provide the panel URL and an in-game account');
    const { chromium } = playwright();
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    try {
        const context = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
        const page = await context.newPage();
        let sockets = 0;
        const errors = [];
        page.on('websocket', () => sockets++);
        page.on('pageerror', e => errors.push(String(e.message || e)));
        // The reference client's shell tries to set an inline style the CSP blocks, and OBS
        // (like any browser) asks for a favicon this build does not serve. Neither affects
        // the picture, so both are counted apart rather than reported as page errors.
        let blockedInlineStyles = 0, faviconMisses = 0;
        page.on('response', r => { if (r.status() === 404 && /favicon/.test(r.url())) faviconMisses++; });
        page.on('console', m => {
            if (m.type() !== 'error') return;
            const text = m.text();
            if (/Applying inline style violates/.test(text)) blockedInlineStyles++;
            else if (/404 \(Not Found\)/.test(text)) faviconMisses++;
            else errors.push('console: ' + text);
        });
        // STREAM_SKIN adds the era skin (internal/controlpanel/streamskin.go) to the URL,
        // so this check covers the OBS path for a chosen era, not only the default. An
        // undeclared skin is a 400 from the handler and never reaches the page — that
        // refusal is asserted by tools/stream/verify-stream-skin.cjs.
        const skin = process.env.STREAM_SKIN ? `&skin=${encodeURIComponent(process.env.STREAM_SKIN)}` : '';
        const url = new URL(`/stream/view?account=${encodeURIComponent(account)}&fps=30&status=1${skin}`, base).href;
        const started = Date.now();
        await page.goto(url);
        await page.waitForFunction(() => document.querySelectorAll('#host canvas').length > 0, null, { timeout: 180000, polling: 250 });

        // Liveness is judged from a COMPOSITED screenshot of the scene canvas, never from
        // gl.readPixels: the provider's context has no preserveDrawingBuffer, so a readback
        // taken outside the drawing frame comes back all zeroes and would report a working
        // view as black. A black PNG this size compresses to a couple of KB; a drawn scene
        // is hundreds.
        const SCENE_DRAWN_BYTES = 20000;
        let sceneBytes = 0;
        for (let i = 0; i < 90 && sceneBytes < SCENE_DRAWN_BYTES; i++) {
            sceneBytes = (await page.locator('#host canvas').first().screenshot()).length;
            if (sceneBytes < SCENE_DRAWN_BYTES) await page.waitForTimeout(2000);
        }
        const firstSceneMs = Date.now() - started;

        // THE CHROME, waited for rather than sampled once.
        //
        // This check used to read the interface canvas the instant the scene crossed its
        // byte threshold, and report whatever it found. That is how it passed a view with
        // no minimap, inventory or chatbox in it: the interface canvas is created early
        // and filled LATER, so a single read taken while the adapter is still decoding the
        // cache finds it present and empty, scores 0, and says PASS because nothing here
        // asserted on it. Under load the gap is tens of seconds — measured on this machine
        // at 100% CPU, the scene drew at 26s and the chrome with it, against ~7s idle.
        //
        // So: poll until the interface canvas has real pixels, time how long that took, and
        // FAIL if it never happens. "The scene drew" is not the same claim as "the game
        // window is there", and the owner looks at the second one.
        const CHROME_TIMEOUT_MS = Number(process.env.STREAM_CHROME_TIMEOUT_MS || 120000);
        // The chrome canvas is the era's OWN window size, not always 2004's: the Classic
        // skin draws a 512x346 one (scene-chrome-classic.js). This used to look for 765x503
        // alone, so a WORKING Classic view scored -1 ("not created yet") for the full two
        // minutes and the harness reported the interfaces missing when they were on screen.
        // Both sizes are named here rather than "the last canvas", because which canvas is
        // the chrome is the thing being asserted.
        const CHROME_SIZES = [[765, 503], [512, 346]];
        const litInterface = () => page.evaluate(sizes => {
            const cs = [...document.querySelectorAll('#host canvas')];
            const c = cs.find(x => sizes.some(([w, h]) => x.width === w && x.height === h));
            if (!c) return -1; // not created yet
            const d = c.getContext('2d', { willReadFrequently: true }).getImageData(0, 0, c.width, c.height).data;
            let n = 0;
            for (let i = 3; i < d.length; i += 4) if (d[i] > 10) n++;
            return n;
        }, CHROME_SIZES);
        const chromeStart = Date.now();
        let chromePixels = await litInterface();
        while (chromePixels <= 0 && Date.now() - chromeStart < CHROME_TIMEOUT_MS) {
            await page.waitForTimeout(1000);
            chromePixels = await litInterface();
        }
        const chromeMs = Date.now() - started;

        const view = await page.evaluate(sizes => {
            const cs = [...document.querySelectorAll('#host canvas')];
            const frame = cs.find(c => sizes.some(([w, h]) => c.width === w && c.height === h));
            let framePixels = 0;
            if (frame) { const d = frame.getContext('2d', { willReadFrequently: true }).getImageData(0, 0, frame.width, frame.height).data; for (let i = 3; i < d.length; i += 4) if (d[i] > 10) framePixels++; }
            return { canvases: cs.length, sceneCanvas: [cs[0].width, cs[0].height], gameFramePixels: framePixels,
                gameFrameSize: frame ? [frame.width, frame.height] : null,
                overflow: [document.documentElement.scrollWidth - innerWidth, document.documentElement.scrollHeight - innerHeight],
                background: getComputedStyle(document.body).backgroundColor };
        }, CHROME_SIZES);
        if (output) { fs.mkdirSync(path.dirname(path.resolve(output)), { recursive: true }); fs.writeFileSync(output, await page.screenshot()); }

        // Closing the source must free the one view lease at once, or the dashboard panel
        // could not show anything again until the lease timed out.
        await page.close();
        const probe = await context.newPage();
        await probe.goto(new URL('/stream/view', base).href); // same origin, so fetch is allowed
        const lease = await probe.evaluate(async ([b, a]) => {
            const r = await fetch(new URL('/api/stream/open', b).href, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account: a }) });
            const j = await r.json();
            if (r.ok) await fetch(new URL('/api/stream/close', b).href, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: j.session }) });
            return { status: r.status, error: j.error || null };
        }, [base, account]);

        const result = { url, ...view, sceneScreenshotBytes: sceneBytes, sceneDrawn: sceneBytes >= SCENE_DRAWN_BYTES, firstSceneMs,
            // chromeMs is time from page load to the game's own interfaces being DRAWN,
            // which is what an operator opening this URL is actually waiting for.
            chromeMs, chromeDrawn: chromePixels > 0,
            gameSockets: sockets, blockedInlineStyles, faviconMisses, pageErrors: errors.slice(0, 5), leaseFreedAfterClose: lease };
        console.log(JSON.stringify(result));
        const failures = [];
        if (!result.sceneDrawn) failures.push(`the scene canvas never drew (screenshot ${sceneBytes} B)`);
        // The game window is the deliverable, not the scene alone: a view with the minimap,
        // inventory and chatbox missing is broken however good the 3D looks.
        if (!result.chromeDrawn) failures.push(`the game's interfaces (minimap, inventory, chatbox) never drew in ${Math.round(CHROME_TIMEOUT_MS / 1000)}s — the canvas held ${chromePixels} lit pixels`);
        if (result.gameSockets) failures.push(`${result.gameSockets} game WebSocket(s) opened; this page must only observe`);
        if (result.overflow[0] > 0 || result.overflow[1] > 0) failures.push(`the page overflows its viewport by ${result.overflow}`);
        if (lease.status !== 200) failures.push(`the view lease was not freed on close (open returned ${lease.status}: ${lease.error})`);
        if (errors.length) failures.push(`page errors: ${errors.slice(0, 3).join(' | ')}`);
        if (failures.length) { console.error('FAIL: ' + failures.join('; ')); process.exitCode = 1; }
        else console.error(`PASS: ${result.canvases} canvases, scene ${result.sceneCanvas.join('x')} drawn in ${Math.round(firstSceneMs / 100) / 10}s, interfaces (${result.gameFramePixels} px) in ${Math.round(chromeMs / 100) / 10}s, lease freed`);
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
