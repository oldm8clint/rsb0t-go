// node tools/stream/verify-stream-input.cjs PANEL_URL ACCOUNT [OUTPUT_PNG]
//
// End-to-end check of the OPT-IN stream client input through the dashboard's own Stream client
// panel, on a panel YOU run with -stream-input: the -fake fleet or the LOCAL LAB ENGINE only.
// Never point it at a production fleet: it takes control of the account (stopping its bot)
// and sends a real walk to the game.
//
// Steps: Load the view; Take input control (first WITHOUT force: if a bank/trade is open or
// the account is a mule the server must refuse, then with force); left-click the ground a few
// tiles from the account; wait for the observed tile to change; right-click and read the
// option menu; Release input control and confirm the bot state is what it was; print the
// server's audit entries for this run. Counters only; the PNG stays local.
const fs = require('fs');
const path = require('path');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';

(async () => {
    const [base, account, output] = process.argv.slice(2);
    if (!base || !account) throw Error('usage: verify-stream-input.cjs PANEL_URL ACCOUNT [PNG]');
    const host = new URL(base).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') throw Error('loopback panels only');
    const status = async () => (await (await fetch(new URL('/api/status', base))).json()).accounts.find(a => a.name === account);
    const before = await status();
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    try {
        const context = await browser.newContext({ viewport: { width: 1400, height: 1100 } });
        const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: 12, h: 11, accounts: [account], settings: { fps: '30' } }] };
        await context.addInitScript(([key, value]) => { try { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } } catch (_) { } }, [LAYOUT_KEY, JSON.stringify(layout)]);
        const page = await context.newPage();
        let sockets = 0; const errors = [];
        page.on('websocket', () => sockets++);
        page.on('pageerror', e => errors.push(String(e.message || e)));
        await page.goto(new URL('/?layout=session', base).href); // never the server's saved layout
        const meta = await page.evaluate(async () => (await (await fetch('/api/meta', { cache: 'no-store' })).json()).streamClient);
        if (!meta?.sessionInput) throw Error('this panel does not advertise sessionInput (start it with -stream-input)');
        const panel = page.locator('section.window[data-id="stream"]');
        await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000 });
        await panel.getByRole('button', { name: 'Load stream client' }).click();
        await page.waitForFunction(() => /triangles/.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || ''), null, { timeout: 120000 });
        await page.waitForTimeout(2000);
        const message = () => panel.locator('p[role="status"]').textContent();
        // Take control without force.
        await panel.getByRole('button', { name: 'Take input control' }).click();
        await page.waitForTimeout(3500);
        // A refusal closes the view (the panel's own rule for an unacknowledged lease); the
        // reason is in the server's audit log.
        const refusedWithoutForce = (await panel.locator('canvas').count()) === 0;
        const refusal = refusedWithoutForce ? (await (await fetch(new URL('/api/stream/audit', base))).json()).entries.filter(e => e.account === account).pop()?.result : null;
        if (refusedWithoutForce) {
            // The refusal closes the view (an unacknowledged lease is ambiguous): reload, force.
            await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 60000 });
            await panel.getByRole('button', { name: 'Load stream client' }).click();
            await page.waitForFunction(() => /triangles/.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || ''), null, { timeout: 120000 });
            await page.waitForTimeout(2000);
            await panel.locator('input[type="checkbox"]').check();
            await panel.getByRole('button', { name: 'Take input control' }).click();
            await page.waitForTimeout(3500);
        }
        const controlling = /^Controlling/.test(await message());
        const during = await status();
        // Left-click the ground a few tiles below the viewport centre (south of the account).
        const canvas = panel.locator('canvas').first();
        const box = await canvas.boundingBox();
        const startTile = during.pos;
        await canvas.click({ position: { x: box.width * 0.5, y: box.height * 0.78 } });
        let moved = null;
        for (let i = 0; i < 20 && !moved; i++) {
            await page.waitForTimeout(500);
            const now = await status();
            if (now.pos && (now.pos.x !== startTile.x || now.pos.z !== startTile.z)) moved = now.pos;
        }
        // Right-click: the option menu.
        await canvas.click({ button: 'right', position: { x: box.width * 0.5, y: box.height * 0.5 } });
        await page.waitForTimeout(1500);
        const menu = await page.evaluate(() => [...document.querySelectorAll('[role="menuitem"]')].map(x => x.textContent));
        if (menu.length) await page.locator('[role="menuitem"]', { hasText: 'Cancel' }).click();
        if (output) {
            const root = canvas.locator('xpath=ancestor::div[contains(@style,"inset: 0")][1]');
            fs.mkdirSync(path.dirname(path.resolve(output)), { recursive: true });
            if (await root.count()) await root.screenshot({ path: output });
        }
        await panel.getByRole('button', { name: 'Release input control' }).click();
        await page.waitForTimeout(3000);
        const after = await status();
        await panel.getByRole('button', { name: 'Close client' }).click();
        const audit = (await (await fetch(new URL('/api/stream/audit', base))).json()).entries.filter(e => e.account === account || !e.account).slice(-12);
        const result = {
            account: '[measured]', refusedWithoutForce, refusal, controlling, botBefore: [before.bot, before.botState], botDuring: [during.bot, during.botState],
            botAfter: [after.bot, after.botState], walkedFrom: startTile, walkedTo: moved, menu, gameSockets: sockets, pageErrors: errors.length,
            audit: audit.map(e => `${e.request} ${e.detail || ''} -> ${e.result}`)
        };
        console.log(JSON.stringify(result, null, 1));
        if (!controlling || !moved || sockets || errors.length) process.exitCode = 1;
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
