// node tools/stream/verify-stream-walk.cjs PANEL_URL ACCOUNT X,Z [X,Z ...] --out PREFIX
//
// LOCAL LAB / -fake ONLY (refuses non-loopback panels): with a panel started with -stream-input,
// load the account's stream view, take input control (force), walk it through the given tiles
// using the real /api/stream/action route (the session id, epoch and seq are the page's own,
// read from its bridge responses), and after each arrival record which levels the view draws
// (the roof rule) and save a screenshot. Releases control at the end (the bot, if any, resumes).
const fs = require('fs');
const path = require('path');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';
const step = m => { if (process.env.DEBUG) console.error(new Date().toISOString().slice(11, 19), m); };

(async () => {
    const args = process.argv.slice(2);
    const o = args.indexOf('--out');
    const out = o >= 0 ? args.splice(o, 2)[1] : null;
    const [base, account, ...tiles] = args;
    if (!base || !account || !tiles.length) throw Error('usage: verify-stream-walk.cjs PANEL ACCOUNT X,Z [X,Z ...] [--out PREFIX]');
    const host = new URL(base).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') throw Error('loopback panels only');
    const status = async () => (await (await fetch(new URL('/api/status', base))).json()).accounts.find(a => a.name === account);
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    const steps = [];
    try {
        const context = await browser.newContext({ viewport: { width: 1400, height: 1100 } });
        const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: 12, h: 11, accounts: [account], settings: { fps: '30' } }] };
        await context.addInitScript(([key, value]) => { try { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } } catch (_) { } }, [LAYOUT_KEY, JSON.stringify(layout)]);
        const page = await context.newPage();
        const bridge = { session: null, epoch: null, seq: 0 };
        page.on('response', async r => {
            const p = new URL(r.url()).pathname;
            try {
                if (p === '/api/stream/open') bridge.session = (await r.json()).session;
                if (p === '/api/stream/frame') { const b = await r.json(); if (b.seq) { bridge.seq = b.seq; bridge.epoch = b.epoch; } }
            } catch (_) { }
        });
        await page.goto(new URL('/?layout=session', base).href);
        step('loaded page');
        const panel = page.locator('section.window[data-id="stream"]');
        await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000, polling: 500 });
        await panel.getByRole('button', { name: 'Load stream client' }).click();
        step('clicked load');
        await page.waitForFunction(() => /triangles/.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || ''), null, { timeout: 120000, polling: 500 });
        await page.waitForTimeout(2500);
        const overlay = () => page.evaluate(() => document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || '');
        const levels = async () => (/levels 0-\d drawn[^;)]*/.exec(await overlay()) || [''])[0];
        const shot = async name => {
            if (!out) return;
            const root = panel.locator('canvas').first().locator('xpath=ancestor::div[contains(@style,"inset: 0")][1]');
            fs.mkdirSync(path.dirname(path.resolve(out)), { recursive: true });
            await root.screenshot({ path: `${out}-${name}.png` });
        };
        let here = (await status()).pos;
        steps.push({ at: here, drawn: await levels() });
        await shot('0-start');
        await panel.locator('input[type="checkbox"]').check();
        step('viewing; taking control');
        await panel.getByRole('button', { name: 'Take input control' }).click();
        await page.waitForFunction(() => /^Controlling/.test(document.querySelector('section.window[data-id="stream"] p[role="status"]')?.textContent || ''), null, { timeout: 15000, polling: 250 });
        let i = 0;
        step('controlling');
        for (const t of tiles) {
            const [x, z] = t.split(',').map(Number);
            const res = await page.evaluate(async ({ b, x, z }) => {
                const r = await fetch('/api/stream/action', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: b.session, epoch: b.epoch, seq: b.seq, action: { kind: 'walk', x, z } }) });
                return [r.status, (await r.json()).label || ''];
            }, { b: bridge, x, z });
            for (let k = 0; k < 40; k++) {
                await page.waitForTimeout(500);
                here = (await status()).pos;
                if (here.x === x && here.z === z) break;
            }
            await page.waitForTimeout(1500); // the view eases and redraws
            steps.push({ walkTo: [x, z], sent: res, at: here, drawn: await levels() });
            await shot(`${++i}-${x}-${z}`);
        }
        await panel.getByRole('button', { name: 'Release input control' }).click();
        await page.waitForTimeout(1500);
        await panel.getByRole('button', { name: 'Close client' }).click();
        console.log(JSON.stringify({ account: '[measured]', steps }, null, 1));
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
