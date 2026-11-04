// node tools/stream/measure-sparkle.cjs PANEL ACCOUNT [QUERY] [PNG]  (set OLD_GL=path to serve another scene-gl.js)
// Sparkle: pixels whose brightness jumps by more than 150 (of 765) between two frames drawn a
// 0.01 rad camera step apart, zoomed out to 45 tiles. Local lab panel only.
const fs = require('fs');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
(async () => {
    const [base, account, query, out] = process.argv.slice(2);
    const browser = await chromium.launch({ executablePath: 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    const context = await browser.newContext({ viewport: { width: 1600, height: 1300 } });
    const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: 12, h: 14, accounts: [account], settings: { fps: '30' } }] };
    await context.addInitScript(v => { if (!sessionStorage.getItem('s')) { localStorage.setItem('rsb0t-go.dashboard.layout', v); sessionStorage.setItem('s', 1); } }, JSON.stringify(layout));
    if (process.env.OLD_GL) await context.route('**/stream/scene-gl.js', r => r.fulfill({ contentType: 'text/javascript', body: fs.readFileSync(process.env.OLD_GL, 'utf8') }));
    const page = await context.newPage();
    await page.goto(base + '/?layout=session' + query);
    await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { polling: 250 });
    await page.locator('section.window[data-id="stream"]').getByRole('button', { name: 'Load stream client' }).click();
    await page.waitForFunction(() => /triangles/.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || '') && /cache textures/.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || ''), null, { polling: 500, timeout: 120000 });
    const canvas = page.locator('section.window[data-id="stream"] canvas').first();
    const b = await canvas.boundingBox();
    await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
    for (let i = 0; i < 6; i++) { await page.mouse.wheel(0, 600); await page.waitForTimeout(150); }
    await page.waitForTimeout(2500);
    const r = await page.evaluate(() => {
        const c = document.querySelector('section.window[data-id="stream"] canvas');
        const gl = c.getContext('webgl2') || c.getContext('webgl');
        c.setPointerCapture = () => {};
        const grab = dx => {
            c.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 100, button: 0, bubbles: true }));
            window.dispatchEvent(new PointerEvent('pointermove', { clientX: 100 + dx, clientY: 100 }));
            const px = new Uint8Array(c.width * c.height * 4);
            gl.readPixels(0, 0, c.width, c.height, gl.RGBA, gl.UNSIGNED_BYTE, px);
            window.dispatchEvent(new PointerEvent('pointerup', { button: 0 }));
            return px;
        };
        const a = grab(1), b = grab(1);
        let flips = 0;
        for (let i = 0; i < a.length; i += 4) if (Math.abs((a[i] + a[i + 1] + a[i + 2]) - (b[i] + b[i + 1] + b[i + 2])) > 150) flips++;
        let el = c; while (el && !el.__streamStats) el = el.parentElement;
        const st = el.__streamStats();
        return { ctx2: !!c.getContext('webgl2'), ctxIs2: gl instanceof WebGL2RenderingContext, search: location.search, flipsPerMillion: Math.round(flips / (c.width * c.height) * 1e6), size: [c.width, c.height], webgl: st.gl.webgl, textureMode: st.gl.textureMode, glError: gl.getError(), distance: st.camera.distance };
    });
    if (out) await page.locator('section.window[data-id="stream"] canvas').first().screenshot({ path: out });
    console.log(JSON.stringify(r));
    await page.locator('section.window[data-id="stream"]').getByRole('button', { name: 'Close client' }).click();
    await browser.close();
})().catch(e => { console.error(e.message); process.exitCode = 1; });
