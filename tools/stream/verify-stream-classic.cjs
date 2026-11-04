// node tools/stream/verify-stream-classic.cjs PANEL_URL ACCOUNT [OUT_DIR]
//
// Checks the RuneScape Classic era skin end to end against a panel YOU run on loopback with
// -era-assets and the cache-terrain provider installed. It opens /stream/view with
// &skin=classic, waits for the chrome to be drawn, and asserts things that can only be true
// if Classic's OWN art really reached the page:
//
//   * /api/meta advertises the classic skin as available, with what was found on disk.
//   * /stream/era/classic/manifest.json lists the chrome sprites, and every PNG it names
//     decodes at exactly the size the manifest claims.
//   * the view reports skin "classic" — the id the PROVIDER returned, not the one requested.
//   * the 512x346 Classic chrome canvas exists (the 2004 skin's is 765x503) and carries the
//     tab strip's own pixels: the six 32x32 buttons of the decoded inv1 sheet, checked by
//     comparing the drawn strip against the PNG this process served.
//   * each of the six Classic tabs opens a panel that draws something.
//   * THE DECLARED 2004 FALLBACK ACTUALLY HAPPENS. Name a second account with a main modal
//     open (STREAM_MODAL_ACCOUNT, e.g. a -fake panel's banking account) and it asserts that
//     the skin composites the reference client's own window instead of drawing nothing: the
//     adapter reports a real modal id, the chrome canvas carries a composited image where the
//     world viewport was, and Classic's tab strip STANDS DOWN rather than covering it. The
//     owner reported "there's no bank screen"; nothing here could tell that apart from a
//     working one, because every check above looks at chrome the fallback deliberately hides.
//   * no game WebSocket is opened by the page, and there are no page errors.
//
// It does not check that the layout matches mudclient's own: that source is not in this
// checkout and scene-chrome-classic.js says so. It checks that the art is Classic's.
const fs = require('fs');
const path = require('path');
// The reference checkout sits beside the repo, but an agent working in a git worktree is one
// directory deeper, so the fixed '../../../rsb0t-main' this used resolved to nothing there
// and the harness could not run at all from a worktree. Walk up instead (same helper as
// verify-obs-view.cjs) and say what was looked for when it is missing.
function playwright() {
    for (let dir = __dirname; ; dir = path.dirname(dir)) {
        const candidate = path.join(dir, 'rsb0t-main', 'node_modules', 'playwright-core');
        if (fs.existsSync(candidate)) return require(candidate);
        if (path.dirname(dir) === dir) break;
    }
    throw Error('playwright-core not found: expected <some parent>/rsb0t-main/node_modules/playwright-core');
}
const { chromium } = playwright();

// The tab strip, from the decoded inv1 sheet: 197x32 of six 32x32 buttons 33px apart,
// drawn at (512-197-3, 3) of the 512x346 Classic window (scene-chrome-classic.js).
const W = 512, H = 346, STRIP_W = 197, STEP = 33, BUTTON = 32, COUNT = 6;
const STRIP_X = W - STRIP_W - 3, STRIP_Y = 3;
const TABS = ['options', 'friends', 'spells', 'stats', 'map', 'inventory'];

(async () => {
    const [base, account, outDir] = process.argv.slice(2);
    if (!base || !account) throw Error('usage: verify-stream-classic.cjs PANEL_URL ACCOUNT [OUT_DIR]');
    const host = new URL(base).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') throw Error('loopback panels only');
    let failures = 0;
    const check = (ok, what, detail) => { console.log(`${ok ? 'PASS' : 'FAIL'}  ${what}${detail === undefined ? '' : ' — ' + detail}`); if (!ok) failures++; };

    const meta = await (await fetch(new URL('/api/meta', base))).json();
    const skins = meta.streamClient?.skins || [];
    const classic = skins.find(s => s.id === 'classic');
    check(!!classic, 'the installed provider declares the classic skin', JSON.stringify(skins.map(s => s.id)));
    check(!!classic?.available, 'the classic skin is available on this machine', classic?.found || classic?.missing || '(nothing said)');
    if (!classic?.available) { console.log('cannot continue without the Classic assets'); process.exitCode = 1; return; }

    const manifest = await (await fetch(new URL('/stream/era/classic/manifest.json', base))).json();
    check((manifest.sprites || []).length > 0, 'the era manifest lists chrome sprites', `${manifest.sprites.length} from ${manifest.source}`);

    const browser = await chromium.launch({
        executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
        headless: true, args: ['--use-angle=d3d11'],
    });
    try {
        const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
        const page = await ctx.newPage();
        const errors = []; let sockets = 0;
        page.on('pageerror', e => errors.push(String(e.message || e)));
        page.on('websocket', w => { if (!w.url().includes(new URL(base).port)) sockets++; });

        const t0 = Date.now();
        await page.goto(new URL(`/stream/view?account=${encodeURIComponent(account)}&fps=5&skin=classic`, base).href);
        const stats = await page.waitForFunction(() => {
            const h = [...document.querySelectorAll('*')].find(e => e.__streamStats);
            const s = h && h.__streamStats();
            return s && s.interfaceFrames > 0 ? s : null;
        }, null, { timeout: 240000 }).then(h => h.jsonValue());
        check(stats.skin === 'classic', 'the view reports the skin the provider returned', String(stats.skin));
        await page.waitForTimeout(2000);

        // Every served PNG decodes at the size the manifest claims. Run in the loaded page
        // so relative urls resolve and the bytes go through a real image decoder rather
        // than a content-length check.
        const sizes = await page.evaluate(async sprites => {
            const out = [];
            for (const sp of sprites) {
                const img = new Image();
                img.src = new URL(sp.url, location.href).href;
                try { await img.decode(); } catch (e) { out.push({ name: sp.name, frame: sp.frame, error: String(e) }); continue; }
                out.push({ name: sp.name, frame: sp.frame, want: [sp.width, sp.height], got: [img.naturalWidth, img.naturalHeight] });
            }
            return out;
        }, manifest.sprites);
        const bad = sizes.filter(s => s.error || s.got[0] !== s.want[0] || s.got[1] !== s.want[1]);
        check(sizes.length === manifest.sprites.length && bad.length === 0,
            'every served Classic sprite decodes at its manifest size', bad.length ? JSON.stringify(bad.slice(0, 3)) : `${sizes.length} frames`);

        const chrome = page.locator('canvas[aria-label^="RuneScape Classic"]');
        check(await chrome.count() === 1, 'the Classic chrome canvas exists');
        const dims = await chrome.evaluate(c => [c.width, c.height]);
        check(dims[0] === W && dims[1] === H, `the chrome canvas is Classic's own ${W}x${H} window`, dims.join('x'));

        // The tab strip must be the decoded inv1 sheet's own pixels, not something drawn to
        // look like it: compare the strip region against the PNG this process served.
        const strip = manifest.sprites.find(s => s.name === 'inv1');
        const diff = await page.evaluate(async ([url, x, y, w, h]) => {
            const img = new Image(); img.src = new URL(url, location.href).href; await img.decode();
            const ref = document.createElement('canvas'); ref.width = w; ref.height = h;
            ref.getContext('2d').drawImage(img, 0, 0);
            const c = [...document.querySelectorAll('canvas')].find(c => c.width === 512 && c.height === 346);
            const a = ref.getContext('2d').getImageData(0, 0, w, h).data;
            const b = c.getContext('2d').getImageData(x, y, w, h).data;
            let same = 0, opaque = 0;
            for (let i = 0; i < a.length; i += 4) {
                if (a[i + 3] === 0) continue;
                opaque++;
                if (Math.abs(a[i] - b[i]) < 6 && Math.abs(a[i + 1] - b[i + 1]) < 6 && Math.abs(a[i + 2] - b[i + 2]) < 6) same++;
            }
            return { opaque, same };
        }, [strip.url, STRIP_X, STRIP_Y, strip.width, strip.height]);
        check(diff.opaque > 1000 && diff.same / diff.opaque > 0.97,
            'the tab strip on screen is the decoded Classic inv1 sheet', `${diff.same}/${diff.opaque} pixels identical`);

        const box = await chrome.locator('xpath=..').boundingBox();
        for (let i = 0; i < COUNT; i++) {
            await page.mouse.click(box.x + (STRIP_X + i * STEP + BUTTON / 2) / W * box.width,
                box.y + (STRIP_Y + BUTTON / 2) / H * box.height);
            await page.waitForTimeout(1200);
            const drawn = await page.evaluate(() => {
                const c = [...document.querySelectorAll('canvas')].find(c => c.width === 512 && c.height === 346);
                // The panel body, under the 33px header at (512-245-3, 37).
                const d = c.getContext('2d').getImageData(512 - 245 - 3, 37 + 33, 245, 204).data;
                const colours = new Set();
                for (let i = 0; i < d.length; i += 4) if (d[i + 3] > 0) colours.add((d[i] << 16) | (d[i + 1] << 8) | d[i + 2]);
                const inv = document.querySelector('[aria-label="Backpack"]');
                return { colours: colours.size, invSlots: inv && inv.style.display !== 'none' ? inv.children.length : 0 };
            });
            check(drawn.colours > 3 || drawn.invSlots > 0, `the ${TABS[i]} panel draws something`,
                `${drawn.colours} colours, ${drawn.invSlots} inventory slots`);
            if (outDir) {
                fs.mkdirSync(outDir, { recursive: true });
                await page.screenshot({ path: path.join(outDir, `classic-${TABS[i]}.png`) });
            }
        }
        // The declared 2004 fallback, measured on a SECOND account that has a main modal
        // open. Two facts are asserted together, because either alone can be satisfied by a
        // broken view: the reference client really drew a modal (the adapter reports its id),
        // and the Classic window really shows that composite (a rich image where the world
        // viewport was, with Classic's own tab strip absent from over the top of it).
        const modalAccount = process.env.STREAM_MODAL_ACCOUNT || '';
        if (modalAccount) {
            // The first view is closed first. A view holds a lease, and a second one opened
            // beside it competes for it — which shows up as a string of 409s and a view that
            // takes half a minute to draw, i.e. as a harness that measures its own contention.
            await page.close();
            const page2 = await ctx.newPage();
            page2.on('pageerror', e => errors.push(String(e.message || e)));
            await page2.goto(new URL(`/stream/view?account=${encodeURIComponent(modalAccount)}&fps=5&skin=classic`, base).href);
            const s2 = await page2.waitForFunction(() => {
                const h = [...document.querySelectorAll('*')].find(e => e.__streamStats);
                const s = h && h.__streamStats();
                return s && s.interfaceFrames > 0 && s.interface ? s : null;
            }, null, { timeout: 240000 }).then(h => h.jsonValue());
            await page2.waitForTimeout(1500);
            check(s2.interface.modal !== undefined && s2.interface.modal !== -1,
                `${modalAccount} really has a main interface open`, `modal ${s2.interface.modal}, ${s2.interface.modalPixels} drawn pixels`);
            const fb = await page2.evaluate(async ([url, x, y, w, h]) => {
                const c = [...document.querySelectorAll('canvas')].find(c => c.width === 512 && c.height === 346);
                if (!c) return { error: 'no Classic chrome canvas' };
                const g = c.getContext('2d', { willReadFrequently: true });
                // The world viewport's own area of the Classic window. A composited 2004
                // window is a photograph of an interface: hundreds of distinct colours.
                const d = g.getImageData(0, 0, 512, 300).data;
                const colours = new Set();
                let lit = 0;
                for (let i = 0; i < d.length; i += 4) { if (d[i + 3] <= 10) continue; lit++; colours.add((d[i] << 16) | (d[i + 1] << 8) | d[i + 2]); }
                // And the tab strip must NOT be drawn over it: same pixel comparison the
                // no-modal check above makes, expected to FAIL here.
                const img = new Image(); img.src = new URL(url, location.href).href; await img.decode();
                const ref = document.createElement('canvas'); ref.width = w; ref.height = h;
                ref.getContext('2d').drawImage(img, 0, 0);
                const a = ref.getContext('2d').getImageData(0, 0, w, h).data;
                const b = g.getImageData(x, y, w, h).data;
                let same = 0, opaque = 0;
                for (let i = 0; i < a.length; i += 4) {
                    if (a[i + 3] === 0) continue;
                    opaque++;
                    if (Math.abs(a[i] - b[i]) < 6 && Math.abs(a[i + 1] - b[i + 1]) < 6 && Math.abs(a[i + 2] - b[i + 2]) < 6) same++;
                }
                return { colours: colours.size, lit, stripSame: same / opaque };
            }, [strip.url, STRIP_X, STRIP_Y, strip.width, strip.height]);
            check(!fb.error && fb.lit > 100000 && fb.colours > 200,
                'an open modal is composited into the Classic window (the declared 2004 fallback)',
                fb.error || `${fb.lit} lit pixels, ${fb.colours} colours`);
            check(!fb.error && fb.stripSame < 0.9,
                "Classic's tab strip stands down for it rather than covering it",
                fb.error || `${Math.round((fb.stripSame || 0) * 100)}% of the strip's pixels still match inv1`);
            if (outDir) {
                fs.mkdirSync(outDir, { recursive: true });
                await page2.screenshot({ path: path.join(outDir, 'classic-modal-fallback.png') });
            }
            await page2.close();
        } else {
            console.log('SKIP  the 2004 modal fallback — set STREAM_MODAL_ACCOUNT to an account with a bank/shop/trade open');
        }
        check(sockets === 0, 'the page opened no game WebSocket', String(sockets));
        check(errors.length === 0, 'no page errors', JSON.stringify(errors.slice(0, 2)));
        console.log(JSON.stringify({ account, chromeMs: Date.now() - t0, frames: stats.frames, interfaceFrames: stats.interfaceFrames, failures }, null, 1));
    } finally { await browser.close(); }
    if (failures) process.exitCode = 1;
})().catch(e => { console.error(String(e).slice(0, 2000)); process.exitCode = 1; });
