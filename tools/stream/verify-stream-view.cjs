// node tools/stream/verify-stream-view.cjs PANEL_URL ACCOUNT [OUT_PREFIX]
//
// Browser check of the stream client's VIEW side (nothing here sends anything to the game):
// no scrollbars and fit-to-panel at a small and a large panel size, render resolution growing
// with the panel, wheel zoom, middle-drag and arrow-key rotation (keys only while the view has
// focus), camera modes, overlay toggles, the help overlay, the screenshot button, painting
// stopping while the panel is not visible, and page CPU/heap/GPU numbers idle, viewing and
// moving the camera. Uses ?layout=session, so a panel's saved layout is never touched. It
// takes and releases the single stream view lease.
const fs = require('fs');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';
const step = m => { if (process.env.DEBUG) console.error(new Date().toISOString().slice(11, 19), m); };

async function openView(browser, base, account, w, h) {
    const context = await browser.newContext({ viewport: { width: 1600, height: 1300 }, acceptDownloads: true });
    const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w, h, accounts: [account], settings: { fps: '30' } }] };
    await context.addInitScript(([key, value]) => { try { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } } catch (_) { } }, [LAYOUT_KEY, JSON.stringify(layout)]);
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(String(e.message || e)));
    await page.goto(new URL('/?layout=session', base).href);
    const panel = page.locator('section.window[data-id="stream"]');
    // A view that closes at once (the single lease still held by an earlier viewer for up to
    // its 15 s expiry, or the account briefly out of game) is retried.
    let outcome = 'closed';
    for (let attempt = 0; attempt < 4 && outcome !== 'frame'; attempt++) {
        if (attempt) await page.waitForTimeout(16000);
        step(`attempt ${attempt}: waiting for Load`);
        await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000, polling: 500 });
        await panel.getByRole('button', { name: 'Load stream client' }).click();
        step('clicked Load');
        outcome = await (await page.waitForFunction(() => {
            const s = document.querySelector('section.window[data-id="stream"]');
            if (/triangles/.test(s.querySelector('div[role="status"]')?.textContent || '')) return 'frame';
            const load = [...s.querySelectorAll('button')].find(x => x.textContent === 'Load stream client');
            if (!s.querySelector('canvas') && load && !load.disabled) return 'closed';
            return false;
        }, null, { timeout: 120000, polling: 500 })).jsonValue();
    }
    step('outcome ' + outcome);
    if (outcome !== 'frame') throw Error('the view never produced a frame: ' + await panel.locator('p[role="status"]').textContent());
    await page.waitForTimeout(2500);
    return { context, page, panel, errors };
}

const stats = page => page.evaluate(() => {
    const c = document.querySelector('section.window[data-id="stream"] canvas');
    let el = c; while (el && !el.__streamStats) el = el.parentElement;
    return el ? el.__streamStats() : null;
});
const layoutInfo = page => page.evaluate(() => {
    const s = document.querySelector('section.window[data-id="stream"]');
    const body = s.querySelector('.body');
    const c = s.querySelector('canvas');
    let root = c; while (root && !/inset|left: \d/.test(root.getAttribute('style') || '')) root = root.parentElement;
    if (!c || !root) return { error: 'no view mounted' };
    const r = root.getBoundingClientRect(), host = root.parentElement.getBoundingClientRect();
    return { scrollbars: body.scrollHeight > body.clientHeight + 1 || body.scrollWidth > body.clientWidth + 1, aspect: r.width / r.height,
        inside: r.left >= host.left - 1 && r.right <= host.right + 1 && r.top >= host.top - 1 && r.bottom <= host.bottom + 1,
        fill: Math.max(r.width / host.width, r.height / host.height), width: Math.round(r.width), height: Math.round(r.height), glWidth: c.width, glHeight: c.height };
});

(async () => {
    const [base, account, out] = process.argv.slice(2);
    if (!base || !account) throw Error('usage: verify-stream-view.cjs PANEL_URL ACCOUNT [OUT_PREFIX]');
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true, args: ['--enable-precise-memory-info'] });
    const result = {};
    try {
        // Small (the default panel size) and large.
        for (const [name, w, h] of [['small', 8, 5], ['large', 12, 14]]) {
            const v = await openView(browser, base, account, w, h);
            result[name] = await layoutInfo(v.page);
            if (out) await v.panel.screenshot({ path: `${out}-${name}.png` });
            if (name === 'small') { await v.context.close(); continue; }
            const { page } = v;
            const canvas = page.locator('section.window[data-id="stream"] canvas').first();
            const box = await canvas.boundingBox();
            const cx = box.x + box.width / 2, cy = box.y + box.height / 2;
            const cdp = await v.context.newCDPSession(page);
            await cdp.send('Performance.enable');
            const metrics = async () => Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map(m => [m.name, m.value]));
            const window = async (ms, during) => {
                const a = await metrics(), s0 = await stats(page);
                const t0 = Date.now(); await (during ? during() : page.waitForTimeout(ms)); const dt = (Date.now() - t0) / 1000;
                const b = await metrics(), s1 = await stats(page);
                return { cpuPct: Math.round((b.TaskDuration - a.TaskDuration) / dt * 1000) / 10, heapMB: Math.round(b.JSHeapUsedSize / 1048576), paintsPerSec: Math.round((s1.paints - s0.paints) / dt), gpuMB: Math.round(s1.gl.gpuBytes / 1048576 * 10) / 10 };
            };
            result.viewing = await window(5000);
            // Wheel zoom.
            const d0 = (await stats(page)).camera.distance;
            await page.mouse.move(cx, cy); await page.mouse.wheel(0, -400); await page.waitForTimeout(1000);
            result.wheelZoom = [Math.round(d0 * 10) / 10, Math.round((await stats(page)).camera.distance * 10) / 10];
            // Middle-drag rotation.
            const y0 = (await stats(page)).camera.yaw;
            await page.mouse.move(cx, cy); await page.mouse.down({ button: 'middle' }); await page.mouse.move(cx + 120, cy + 30, { steps: 6 }); await page.mouse.up({ button: 'middle' });
            result.middleDragYaw = [Math.round(y0 * 100) / 100, Math.round((await stats(page)).camera.yaw * 100) / 100];
            // Arrow keys: ignored without focus, rotate with it.
            await page.evaluate(() => document.activeElement?.blur());
            let y1 = (await stats(page)).camera.yaw;
            await page.keyboard.down('ArrowLeft'); await page.waitForTimeout(400); await page.keyboard.up('ArrowLeft');
            result.arrowWithoutFocus = Math.round(((await stats(page)).camera.yaw - y1) * 100) / 100;
            await canvas.click({ button: 'middle', position: { x: 5, y: 5 } }); // focus the view without a game click
            y1 = (await stats(page)).camera.yaw;
            result.movingCamera = await window(0, async () => { await page.keyboard.down('ArrowLeft'); await page.waitForTimeout(3000); await page.keyboard.up('ArrowLeft'); });
            result.arrowWithFocus = Math.round(((await stats(page)).camera.yaw - y1) * 100) / 100;
            const p0 = (await stats(page)).camera.pitch;
            await page.keyboard.down('ArrowUp'); await page.waitForTimeout(3000); await page.keyboard.up('ArrowUp');
            result.pitchClampedAt = Math.round((await stats(page)).camera.pitch * 1000) / 1000;
            result.pitchStart = Math.round(p0 * 1000) / 1000;
            // Modes and toggles.
            for (const k of ['c', 'g', 'n', 'i']) await page.keyboard.press(k);
            await page.waitForTimeout(500);
            const st = await stats(page);
            result.modeAfterC = st.camera.mode; result.toggles = st.toggles;
            await page.keyboard.press('?');
            result.helpShown = await page.locator('[aria-label="Stream client shortcuts"]').count() === 1;
            if (out) await page.locator('section.window[data-id="stream"]').screenshot({ path: `${out}-overlays.png` });
            await page.keyboard.press('?');
            const download = page.waitForEvent('download', { timeout: 10000 }).catch(() => null);
            await page.keyboard.press('p');
            const dl = await download;
            result.screenshot = dl ? dl.suggestedFilename().replace(/^[^-]+/, '[account]') : null;
            for (const k of ['c', 'c', 'g', 'n', 'i']) await page.keyboard.press(k); // back to follow, overlays off
            // Not visible: painting stops, observations slow.
            await page.evaluate(() => { document.querySelector('section.window[data-id="stream"] .body').style.display = 'none'; });
            await page.waitForTimeout(800);
            result.hidden = await window(5000);
            await page.evaluate(() => { document.querySelector('section.window[data-id="stream"] .body').style.display = ''; });
            await page.waitForTimeout(1500);
            result.visibleAgain = (await stats(page)).showing;
            result.pageErrors = v.errors.length;
            await page.locator('section.window[data-id="stream"]').getByRole('button', { name: 'Close client' }).click();
            await v.context.close();
        }
        console.log(JSON.stringify(result, null, 1));
        const ok = !result.small.scrollbars && !result.large.scrollbars && result.small.inside && result.large.inside && result.large.glWidth > result.small.glWidth &&
            result.wheelZoom[1] < result.wheelZoom[0] && result.middleDragYaw[0] !== result.middleDragYaw[1] && result.arrowWithoutFocus === 0 && result.arrowWithFocus !== 0 &&
            result.helpShown && result.screenshot && result.hidden.paintsPerSec === 0 && result.visibleAgain && !result.pageErrors;
        if (!ok) process.exitCode = 1;
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
