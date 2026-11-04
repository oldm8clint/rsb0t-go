// node tools/stream/verify-stream-skin.cjs PANEL_URL ACCOUNT
//
// Proof of the ERA SKIN mechanism (internal/controlpanel/streamskin.go), end to end
// through the real dashboard, the real Load button and the real OBS view URL. Run from
// the repo root against a panel started with -stream-client (plus the capture flags for
// the cache-terrain provider).
//
// What it asserts, and why each one is the thing that actually matters:
//   1. /api/meta advertises the skins the INSTALLED provider's manifest declares, and a
//      default among them. (Nothing else in the panel is allowed to invent that list.)
//   2. Loading the declared skin mounts it, and the id the panel records is the one the
//      PROVIDER returned (host.dataset.skin), not the one that was requested.
//   3. Asking for a skin the provider does NOT declare leaves Load disabled with the real
//      installed list in the message, takes no view lease, and draws nothing. This is the
//      whole point: a skin with no assets must refuse, never quietly draw another era.
//   4. /stream/view answers 200 for a declared skin (and for no skin at all) and 400 for
//      an undeclared one, so an OBS Browser Source reports a bad URL once instead of
//      retrying a black source forever.
//
// Read-only with respect to the game: it seeds a one-panel layout into THIS headless
// browser's own storage, takes and releases the single stream VIEW lease, opens no
// WebSocket and sends no game action.
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');

const LAYOUT_KEY = 'rsb0t-go.dashboard.layout';
const UNDECLARED = process.env.STREAM_BAD_SKIN || '2011';

(async () => {
    const [base, account] = process.argv.slice(2);
    if (!base || !account) throw Error('Provide the panel URL and an in-game account');
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    const failures = [];
    const check = (ok, what) => { if (!ok) failures.push(what); return ok; };
    try {
        const open = async skin => {
            const context = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
            const layout = { v: 1, panels: [{ id: 'stream', type: 'stream', title: '', col: 0, row: 0, w: 12, h: 8, accounts: [account], settings: { fps: '30', skin } }] };
            await context.addInitScript(([key, value]) => { try { localStorage.setItem(key, value); } catch (_) { /* storage refused */ } }, [LAYOUT_KEY, JSON.stringify(layout)]);
            const page = await context.newPage();
            let sockets = 0; const errors = [];
            page.on('websocket', () => sockets++);
            page.on('pageerror', e => errors.push(String(e.message || e)));
            // Two kinds of console error are this fixture's own doing and must not be
            // counted as page errors: the reference client's shell attempts one inline
            // style the panel CSP blocks, and every refusal this fixture deliberately
            // provokes (a 400 from /stream/view, a 409 from a held lease) is logged by the
            // browser as a failed resource load. Anything else is a real page error.
            page.on('console', m => { if (m.type() !== 'error') return; const t = m.text();
                if (/Applying inline style violates/.test(t) || /Failed to load resource/.test(t)) return;
                errors.push('console: ' + t); });
            await page.goto(new URL('/?layout=session', base).href);
            await page.locator('section.window[data-id="stream"]').waitFor({ timeout: 30000 });
            return { context, page, sockets: () => sockets, errors };
        };
        const panelState = page => page.evaluate(() => {
            const s = document.querySelector('section.window[data-id="stream"]');
            const load = [...s.querySelectorAll('button')].find(b => b.textContent === 'Load stream client');
            return {
                loadDisabled: !!load?.disabled,
                message: s.querySelector('p[role="status"]')?.textContent || '',
                era: (() => { const dt = [...s.querySelectorAll('dt')].find(d => d.textContent === 'Era skin'); return dt?.nextElementSibling?.textContent || ''; })(),
                canvases: s.querySelectorAll('canvas').length,
                mountedSkin: [...s.querySelectorAll('[data-skin]')].map(n => n.dataset.skin)[0] || ''
            };
        });

        // The UNDECLARED skin goes first, deliberately: it must take no view lease, and
        // the only way to prove a lease is free is to ask for one before anything has held
        // it. A closed browser context releases the panel's lease, but not synchronously —
        // running this second would have tested the 15s lease TTL, not the refusal.

        // 1. what the build advertises, read from the panel's own /api/meta
        const bad = await open(UNDECLARED);
        const meta = await bad.page.evaluate(async () => (await (await fetch('/api/meta', { cache: 'no-store' })).json()).streamClient);
        const ids = (meta?.skins || []).map(s => s.id);
        check(ids.length > 0, 'the panel advertises no era skins at all');
        check(!!meta.skin && ids.includes(meta.skin), `default skin ${JSON.stringify(meta.skin)} is not one of ${JSON.stringify(ids)}`);
        check(!ids.includes(UNDECLARED), `this provider declares ${UNDECLARED}, so it cannot be used as the undeclared case; set STREAM_BAD_SKIN`);

        // 2. the undeclared skin refuses, and takes nothing
        await bad.page.waitForTimeout(2500); // let a status poll land, so Load would be enabled if it were going to be
        const refused = await panelState(bad.page);
        check(refused.loadDisabled, `Load was enabled for the undeclared skin ${UNDECLARED}`);
        check(refused.canvases === 0, 'the undeclared skin drew a canvas');
        check(refused.mountedSkin === '', `the undeclared skin recorded a mounted skin ${JSON.stringify(refused.mountedSkin)}`);
        for (const id of ids) check(refused.message.includes(id), `the refusal does not name the installed skin ${id}: ${JSON.stringify(refused.message)}`);
        // It must not have taken the single view lease either: an open must still succeed.
        const lease = await bad.page.evaluate(async name => {
            const res = await fetch('/api/stream/open', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ account: name }) });
            const body = await res.json().catch(() => ({}));
            if (res.ok && body.session) await fetch('/api/stream/close', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: body.session }) });
            return { status: res.status, error: body.error || '' };
        }, account);
        check(lease.status === 200, `the view lease was not free after a refused skin: /api/stream/open = ${lease.status} ${lease.error}`);

        // 3. the OBS view URL
        const view = await bad.page.evaluate(async ([name, badSkin, goodSkin]) => {
            const at = async q => (await fetch('/stream/view?account=' + name + q, { cache: 'no-store' })).status;
            return { none: await at(''), good: await at('&skin=' + goodSkin), bad: await at('&skin=' + badSkin), nonsense: await at('&skin=not-an-era') };
        }, [account, UNDECLARED, meta.skin]);
        check(view.none === 200 && view.good === 200, `/stream/view refused a valid URL: ${JSON.stringify(view)}`);
        check(view.bad === 400 && view.nonsense === 400, `/stream/view accepted an undeclared skin: ${JSON.stringify(view)}`);
        check(bad.errors.length === 0, `page errors: ${bad.errors.join(' | ')}`);
        await bad.context.close();

        // 4. the declared skin loads, and the id recorded is the PROVIDER's own answer
        const first = await open(meta.skin);
        await first.page.waitForFunction(() => { const b = [...document.querySelectorAll('section.window[data-id="stream"] button')].find(x => x.textContent === 'Load stream client'); return b && !b.disabled; }, null, { timeout: 120000 });
        await first.page.getByRole('button', { name: 'Load stream client' }).click();
        await first.page.waitForFunction(() => /triangles|seq/i.test(document.querySelector('section.window[data-id="stream"] div[role="status"]')?.textContent || ''), null, { timeout: 120000, polling: 250 });
        const loaded = await panelState(first.page);
        check(loaded.mountedSkin === meta.skin, `the provider mounted ${JSON.stringify(loaded.mountedSkin)}, want ${JSON.stringify(meta.skin)}`);
        check(loaded.canvases > 0, 'the declared skin drew no canvas');
        check(first.sockets() === 0, `the page opened ${first.sockets()} game WebSockets`);
        check(first.errors.length === 0, `page errors while loaded: ${first.errors.join(' | ')}`);
        await first.context.close();

        console.log(JSON.stringify({ advertised: ids, defaultSkin: meta.skin, mountedSkin: loaded.mountedSkin, era: loaded.era,
            undeclared: UNDECLARED, loadDisabled: refused.loadDisabled, refusal: refused.message, leaseAfterRefusal: lease.status, view,
            gameSockets: first.sockets() + bad.sockets(), failures }));
        if (failures.length) { console.error(failures.join('\n')); process.exitCode = 1; }
    } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
