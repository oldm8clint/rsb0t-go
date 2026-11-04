// node tools/stream/verify-stream-interface.cjs PANEL_URL ACCOUNT PLAN_JSON [OUT_DIR]
//
// Drives the OPT-IN stream client input on the game's own interfaces through the dashboard's
// Stream client panel, step by step from a plan, on a panel YOU run with -stream-input against
// the LOCAL LAB ENGINE only (it refuses a non-loopback panel). It takes control of the account
// (stopping its bot, resumed on release) and really clicks: every action goes through the
// panel's validated /api/stream/action path and is printed from the server's audit log.
//
// Coordinates are the game's own 765 x 503 window. Plan steps (a JSON array), run in order:
//   {"tab": 4}                               click a side-tab icon (local, as in the game)
//   {"probe": "side"|"chat"|"main"}          print the game's option lists found on that area
//   {"find": "Wield Wooden shield", "area": "side", "left": false}
//                                            find the point whose option list has that entry,
//                                            then right-click there and choose it (or left-click
//                                            when it is the first entry and "left" is true)
//   {"click": [x, y], "right": false, "choose": "text"}
//   {"type": "hello", "enter": true}         keys to the focused view (the game's chat)
//   {"wheel": [x, y, dy]}   {"wait": ms}   {"shot": "name"}   {"ui": true} (print ui summary)
//   {"expect": {"inv": [3214, slot, id|0]}}  check an observed inventory slot (after waiting)
//   {"expect": {"invHas": [3214, id, count?]}} / {"expect": {"invLacks": [3214, id]}}
//   {"varpsNow": true} then {"expect": {"varpChanged": 43}}   {"blurType": "abc"} (keys outside the view)
//   {"expect": {"chatModal": -1}} / {"expect": {"main": 5292}} / {"expect": {"chat": "text"}}
const fs = require('fs');
const path = require('path');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';
const TAB_BOXES = [
    [539, 573, 169, 205, 0], [569, 599, 168, 205, 1], [597, 627, 168, 205, 2], [625, 669, 168, 203, 3],
    [666, 696, 168, 205, 4], [694, 724, 168, 205, 5], [722, 756, 169, 205, 6],
    [540, 574, 466, 502, 7], [572, 602, 466, 503, 8], [599, 629, 466, 503, 9], [627, 671, 467, 502, 10],
    [669, 699, 466, 503, 11], [696, 726, 466, 503, 12], [724, 758, 466, 502, 13]];
const AREAS = { main: [8, 8, 512, 334], side: [556, 208, 740, 463], chat: [20, 360, 493, 450] };

(async () => {
    const [base, account, planPath, outDir] = process.argv.slice(2);
    if (!base || !account || !planPath) throw Error('usage: verify-stream-interface.cjs PANEL_URL ACCOUNT PLAN_JSON [OUT_DIR]');
    const host = new URL(base).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') throw Error('loopback panels only');
    const plan = JSON.parse(fs.readFileSync(planPath, 'utf8'));
    const status = async () => (await (await fetch(new URL('/api/status', base))).json()).accounts.find(a => a.name === account);
    const audit = async () => ((await (await fetch(new URL('/api/stream/audit', base))).json()).entries || []).filter(e => e.account === account);
    const auditStart = (await audit()).length;
    const before = await status();
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    const log = (...a) => console.log(...a);
    let failures = 0;
    try {
        const context = await browser.newContext({ viewport: { width: 1400, height: 1400 } });
        const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: 12, h: 12, accounts: [account], settings: { fps: '30' } }] };
        await context.addInitScript(([key, value]) => { try { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, value); sessionStorage.setItem('seeded', '1'); } } catch (_) { } }, [LAYOUT_KEY, JSON.stringify(layout)]);
        const page = await context.newPage();
        let sockets = 0; const errors = [];
        page.on('websocket', () => sockets++);
        page.on('pageerror', e => errors.push(String(e.message || e)));
        await page.goto(new URL('/?layout=session', base).href);
        const panel = page.locator('section.window[data-id="stream"]');
        await page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000 });
        await panel.getByRole('button', { name: 'Load stream client' }).click();
        try {
            await page.waitForFunction(() => { const h = [...document.querySelectorAll('*')].find(e => e.__streamStats); return h && h.__streamStats().interfaceFrames > 0; }, null, { timeout: 120000 });
        } catch (e) {
            const s = await page.evaluate(() => { const h = [...document.querySelectorAll('*')].find(e => e.__streamStats); const s = h?.__streamStats(); return s && { frames: s.frames, lastError: s.lastError, lastModelError: s.lastModelError, terrain: s.terrain }; });
            const text = await panel.textContent();
            throw Error(`the view never drew its interfaces: ${JSON.stringify(s)} page errors ${JSON.stringify(errors)} panel "${text.slice(0, 300)}"`);
        }
        await page.waitForTimeout(1500);
        await panel.locator('input[type="checkbox"]').check();
        await panel.getByRole('button', { name: 'Take input control' }).click();
        await page.waitForFunction(() => { const h = [...document.querySelectorAll('*')].find(e => e.__streamStats); return h && h.__streamStats().input; }, null, { timeout: 15000 });
        const stats = () => page.evaluate(() => [...document.querySelectorAll('*')].find(e => e.__streamStats).__streamStats());
        const menuAt = (x, y) => page.evaluate(([x, y]) => [...document.querySelectorAll('*')].find(e => e.__streamMenuAt).__streamMenuAt(x, y), [x, y]);
        const rootBox = async () => {
            const root = page.locator('canvas[aria-label^="GPU scene"]').locator('xpath=../..');
            return { root, box: await root.boundingBox() };
        };
        const toPage = async (x, y) => { const { box } = await rootBox(); return [box.x + (x + 0.5) / 765 * box.width, box.y + (y + 0.5) / 503 * box.height]; };
        const click = async (x, y, right) => {
            const [px, py] = await toPage(x, y);
            if (process.env.VERIFY_DEBUG) log('  target:', await page.evaluate(([px, py]) => { const e = document.elementFromPoint(px, py); return e ? `${e.tagName} ${e.getAttribute('aria-label') || ''} ${e.getAttribute('role') || ''}` : null; }, [px, py]));
            await page.mouse.click(px, py, { button: right ? 'right' : 'left' });
        };
        const choose = async text => {
            await page.waitForSelector('[role="menuitem"]', { timeout: 5000 });
            const items = await page.evaluate(() => [...document.querySelectorAll('[role="menuitem"]')].map(x => x.textContent));
            const hit = page.locator('[role="menuitem"]', { hasText: text }).first();
            if (!(await hit.count())) { log(`  menu had no "${text}": ${JSON.stringify(items)}`); failures++; await page.locator('[role="menuitem"]', { hasText: 'Cancel' }).first().click(); return; }
            log(`  menu: ${JSON.stringify(items)} -> "${text}"`);
            await hit.click();
        };
        const scan = async (area, step = 6) => {
            const [x0, y0, x1, y1] = AREAS[area], seen = new Map();
            for (let y = y0; y < y1; y += step) for (let x = x0; x < x1; x += step) {
                const m = await menuAt(x, y);
                if (!m.length) continue;
                const key = m.map(i => i.label).join(' | ');
                if (!seen.has(key)) seen.set(key, [x, y]);
            }
            return seen;
        };
        let n = 0, varpsBefore = {};
        for (const step of plan) {
            n++;
            if (step.tab !== undefined) {
                const b = TAB_BOXES.find(t => t[4] === step.tab);
                await click((b[0] + b[1]) >> 1, (b[2] + b[3]) >> 1, false);
                await page.waitForTimeout(600);
                log(`${n}. tab ${step.tab} -> view shows tab ${(await stats()).uiTab}`);
            } else if (step.probe) {
                const seen = await scan(step.probe, step.step || 6);
                log(`${n}. options on ${step.probe}:`);
                for (const [k, at] of seen) log(`  at ${at}: ${k}`);
            } else if (step.find) {
                const seen = await scan(step.area || 'side', step.step || 4);
                const hit = [...seen].find(([k]) => k.split(' | ').some(l => l.includes(step.find)));
                if (!hit) { log(`${n}. "${step.find}" not found on ${step.area}`); failures++; continue; }
                const [x, y] = hit[1];
                if (step.left) { await click(x, y, false); log(`${n}. left-click at ${x},${y}: first option of [${hit[0]}]`); }
                else { log(`${n}. right-click at ${x},${y}`); await click(x, y, true); await choose(step.find); }
                await page.waitForTimeout(step.after || 1500);
            } else if (step.click) {
                const [x, y] = step.click;
                log(`${n}. ${step.right ? 'right' : 'left'}-click at ${x},${y}`);
                await click(x, y, !!step.right);
                if (step.choose) await choose(step.choose);
                await page.waitForTimeout(step.after || 1500);
            } else if (step.type !== undefined) {
                await page.keyboard.type(step.type, { delay: 40 });
                await page.waitForTimeout(700);
                const typed = (await stats()).typed;
                log(`${n}. typed "${step.type}" -> view's chat line "${typed}"`);
                if (step.shot) await shot(step.shot);
                if (step.enter) { await page.keyboard.press('Enter'); await page.waitForTimeout(1500); }
            } else if (step.varpsNow) {
                varpsBefore = { ...((await stats()).ui?.varps || {}) };
                log(`${n}. remembered ${Object.keys(varpsBefore).length} varps`);
            } else if (step.blurType !== undefined) {
                // Keys typed while the view does NOT have focus must never reach it.
                const before = (await stats()).typed, actionsBefore = (await stats()).inputActions;
                await page.locator('header, h1, body').first().click({ position: { x: 5, y: 5 } });
                await page.keyboard.type(step.blurType, { delay: 30 }); await page.keyboard.press('Enter');
                await page.waitForTimeout(800);
                const st = await stats();
                const ok = st.typed === before && st.inputActions === actionsBefore;
                log(`${n}. typed "${step.blurType}" with focus outside the view (${await page.evaluate(() => document.activeElement?.tagName)}): ${ok ? 'PASS' : 'FAIL'} (view line "${st.typed}", actions ${actionsBefore} -> ${st.inputActions})`);
                if (!ok) failures++;
            } else if (step.wheel) {
                const [x, y, dy] = step.wheel;
                const [px, py] = await toPage(x, y);
                await page.mouse.move(px, py); await page.mouse.wheel(0, dy);
                await page.waitForTimeout(800);
                log(`${n}. wheel ${dy} at ${x},${y}`);
            } else if (step.wait) {
                await page.waitForTimeout(step.wait);
            } else if (step.ui) {
                log(`${n}. ui ${JSON.stringify((await stats()).ui)}`);
            } else if (step.expect) {
                const e = step.expect;
                let ok = false, got;
                for (let i = 0; i < 20 && !ok; i++) {
                    const ui = (await stats()).ui;
                    if (e.inv) { const [com, slot, id] = e.inv; const s = ui.inventories?.[com]?.[slot]; got = s; ok = id === 0 ? !s : (s && s[0] === id); }
                    else if (e.invHas) { const [com, id, count] = e.invHas; got = (ui.inventories?.[com] || []).filter(Boolean); ok = got.some(s => s[0] === id && (count === undefined || s[1] === count)); }
                    else if (e.invLacks) { const [com, id] = e.invLacks; got = (ui.inventories?.[com] || []).filter(Boolean); ok = !got.some(s => s[0] === id); }
                    else if (e.varpChanged !== undefined) { got = ui.varps?.[e.varpChanged]; ok = got !== varpsBefore[e.varpChanged]; }
                    else if (e.chatModal !== undefined) { got = ui.chatModal; ok = got === e.chatModal; }
                    else if (e.notChatModal !== undefined) { got = ui.chatModal; ok = got !== e.notChatModal; }
                    else if (e.main !== undefined) { got = ui.main; ok = got === e.main; }
                    else if (e.countDialog !== undefined) { got = ui.countDialog; ok = got === e.countDialog; }
                    else if (e.chat) { got = ui.chat; ok = ui.chat.some(t => t.includes(e.chat)); }
                    if (!ok) await page.waitForTimeout(500);
                }
                log(`${n}. expect ${JSON.stringify(e)}: ${ok ? 'PASS' : 'FAIL'} (observed ${JSON.stringify(got)})`);
                if (!ok) failures++;
            }
            if (step.shot && step.type === undefined) await shot(step.shot);
        }
        async function shot(name) {
            if (!outDir) return;
            fs.mkdirSync(outDir, { recursive: true });
            const { root } = await rootBox();
            await root.screenshot({ path: path.join(outDir, `${name}.png`) });
        }
        await panel.getByRole('button', { name: 'Release input control' }).click();
        await page.waitForTimeout(2500);
        const after = await status();
        const st = await stats();
        await panel.getByRole('button', { name: 'Close client' }).click();
        const entries = (await audit()).slice(auditStart);
        log('audit:');
        for (const e of entries) log(`  ${e.request} ${e.detail || ''} -> ${e.result}`);
        log(JSON.stringify({ botBefore: [before.bot, before.botState], botAfter: [after.bot, after.botState], inputActions: st.inputActions,
            inputLog: st.inputLog.map(l => `${l.label}: ${l.result}`), gameSockets: sockets, pageErrors: errors, failures }, null, 1));
        if (failures || sockets || errors.length) process.exitCode = 1;
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
