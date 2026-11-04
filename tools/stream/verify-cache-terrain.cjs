// node tools/stream/verify-cache-terrain.cjs [OUTPUT_PNG]
// Explicit SYNTHETIC transport/decoder fixture for tools/stream/cache-terrain-client.js. A fake
// origin serves the provider, its sibling modules, the dashboard's item icon modules, a fake
// capture adapter (a small invented height grid, not the cache) and a scripted bridge. No
// server, login, game or live claim: it proves the provider's own rules only.
//   A. scene-delta: one whole scene then deltas confirmed by `have`; a 1-tile step does not
//      re-decode terrain, a 6-tile move does; markers, labels, minimap and backpack are drawn;
//      eased movement repaints between observations; an epoch change closes the lease.
//   B. a delta on a base the provider does not hold makes it ask for a whole scene (have=0).
//   C. a server that only speaks full-snapshot still gets the same view.
//   D. abort during decoding uploads nothing late and cleans up.
const fs = require('fs'), path = require('path'), assert = require('assert/strict');
const { chromium } = require('../../../rsb0t-main/node_modules/playwright-core');
const web = path.join(__dirname, '../../internal/controlpanel/web');

const adapter = `export async function terrainGpuMesh(s, radius){
    window.decodes=(window.decodes||0)+1; window.decodedAt=(window.decodedAt||[]).concat(s.x);
    if(window.delay) await new Promise(r=>setTimeout(r,window.delay));
    const v=[]; const h=(x,z)=>-(Math.sin((s.x+x)*0.7)+Math.cos((s.z+z)*0.5))*0.6;
    const c=(x,z)=>[x,h(x,z),z]; const col=(x,z)=>((s.x+x+s.z+z)&1)?[0.25,0.45,0.2,1]:[0.3,0.5,0.22,1];
    for(let x=-radius;x<=radius;x++)for(let z=-radius;z<=radius;z++){
        const a=c(x-0.5,z-0.5),b=c(x+0.5,z-0.5),d=c(x+0.5,z+0.5),e=c(x-0.5,z+0.5),k=col(x,z);
        for(const p of [a,b,d,a,d,e]) v.push(...p,...k);
    }
    return {vertices:v,triangles:v.length/21,tiles:(2*radius+1)**2,texturedFaces:3};
}
export async function textureAverages(){ return Array.from({length:50},()=>0x406080); }
// A synthetic 'model': a coloured box, 10 floats per vertex. npc type 2000 never loads, to
// exercise the marker fallback and the pending/retry path.
export async function entityGpuMeshes(reqs){
    window.modelReqs=(window.modelReqs||0)+reqs.length;
    const meshes={};
    for(const r of reqs){
        if(r.kind==='npc'&&r.type===2000) continue;
        const v=[], h=r.kind==='obj'?0.2:1.2, w=0.3, c=r.kind==='player'?[0.2,0.3,0.9]:r.kind==='obj'?[0.9,0.8,0.1]:[0.8,0.2,0.2];
        const q=(a,b,cc,d)=>{for(const p of [a,b,cc,a,cc,d]) v.push(...p,...c,1,0,0,-1);};
        q([-w,h,-w],[w,h,-w],[w,h,w],[-w,h,w]); q([-w,0,-w],[w,0,-w],[w,h,-w],[-w,h,-w]); q([-w,0,w],[w,0,w],[w,h,w],[-w,h,w]);
        q([-w,0,-w],[-w,0,w],[-w,h,w],[-w,h,-w]); q([w,0,-w],[w,0,w],[w,h,w],[w,h,-w]);
        meshes[r.key]={vertices:v,alpha:[],triangles:v.length/30,minY:h,texturedFaces:0,anims:r.kind==='obj'?null:{ready:7,walk:8,run:9,turn:-1}};
    }
    return {meshes,pending:reqs.filter(r=>!meshes[r.key]).map(r=>r.key),floats:10};
}
// Synthetic sequences: two frames, the second raised by 0.5 tiles; 3 cycles (60 ms) each.
export async function animationGpuFrames(r, seq){
    window.animReqs=(window.animReqs||[]).concat(r.kind+':'+seq);
    const m=(await entityGpuMeshes([{...r,key:'x'}])).meshes.x;
    const xyz=[],up=[];
    for(let i=0;i<m.vertices.length;i+=10){xyz.push(m.vertices[i],m.vertices[i+1],m.vertices[i+2]);up.push(m.vertices[i],m.vertices[i+1]+0.5,m.vertices[i+2]);}
    return {seq,frameCount:2,delays:[3,3],loops:-1,maxloops:99,base:m.vertices,baseAlpha:[],positions:[xyz,up],alphaPositions:[[],[]],floats:10,triangles:m.triangles};
}`;

(async () => {
    const output = process.argv[2];
    const browser = await chromium.launch({ executablePath: process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless: true });
    try {
        const page = await browser.newPage({ viewport: { width: 1000, height: 700 } });
        let opened = 0, closed = 0, requests = 0, sockets = 0, haves = [], mode = 'scene-delta', badBaseAt = 0, epochBumpAt = 8;
        let lastSent = null, lastSeq = 0;
        const pageErrors = [];
        page.on('websocket', () => sockets++);
        page.on('pageerror', e => pageErrors.push(String(e.message || e)));
        const sceneFor = n => ({
            tick: 100 + n,
            self: { x: n < 3 ? 3200 : n < 5 ? 3201 : 3206, z: 3200, level: 0, centreX: 400, centreZ: 400, slot: 1, name: 'fixture' },
            entities: [
                { k: 'n1', x: 3202 + (n % 2), z: 3201, t: 316, n: 'Fishing spot' },
                { k: 'n2', x: 3198, z: 3203, t: 2000, s: 2 },
                { k: 'p5', x: 3199, z: 3199 - (n % 3), n: 'Neighbour' }
            ],
            inv: Array.from({ length: 28 }, (_, i) => (i === 0 ? [303, 1] : i <= n ? [317, 1] : i === 27 ? [995, 123456] : [0, 0])),
            chat: [{ type: 0, text: `You catch some shrimps. (${n})` }],
            status: { bot: 'ProgressiveFisher', activity: 'fishing', run: 90 - n },
            looks: { self: 'AAEC', p5: 'CQgH' },
            objs: [[3201, 3201, 995, 5]]
        });
        const diff = (a, b, base) => {
            const d = { base };
            if (JSON.stringify(a.self) !== JSON.stringify(b.self)) d.self = b.self;
            const old = new Map(a.entities.map(e => [e.k, JSON.stringify(e)]));
            d.upsert = b.entities.filter(e => old.get(e.k) !== JSON.stringify(e));
            d.inv = b.inv.map((s, i) => [i, s[0], s[1]]).filter(([i]) => JSON.stringify(a.inv[i]) !== JSON.stringify(b.inv[i]));
            if (JSON.stringify(a.chat) !== JSON.stringify(b.chat)) d.chat = b.chat;
            if (JSON.stringify(a.status) !== JSON.stringify(b.status)) d.status = b.status;
            if (JSON.stringify(a.looks) !== JSON.stringify(b.looks)) d.looks = b.looks;
            if (JSON.stringify(a.objs) !== JSON.stringify(b.objs)) d.objs = b.objs;
            return d;
        };
        const snapshotOf = sc => ({ x: sc.self.x, z: sc.self.z, level: 0, centreX: 400, centreZ: 400, selfSlot: 1, appearance: sc.looks.self,
            objs: sc.objs.map(([X, Z, ID, Count]) => ({ X, Z, ID, Count, Level: 0 })),
            npcs: sc.entities.filter(e => e.k[0] === 'n').map(e => ({ Index: +e.k.slice(1), Type: e.t, X: e.x, Z: e.z })),
            players: [{ index: 1, x: sc.self.x, z: sc.self.z }, ...sc.entities.filter(e => e.k[0] === 'p').map(e => ({ index: +e.k.slice(1), name: e.n, x: e.x, z: e.z, appearance: sc.looks[e.k] }))],
            ui: { version: 1, name: 'fixture', tick: sc.tick, inventories: { 3214: { Slots: sc.inv.map(([ID, Count]) => ({ ID, Count })) } }, chat: [{ type: 0, username: '', text: sc.chat[0].text }] } });
        await page.route('http://terrain.test/**', async route => {
            const url = new URL(route.request().url()), p = url.pathname;
            const js = file => route.fulfill({ contentType: 'application/javascript', body: fs.readFileSync(file, 'utf8') });
            if (p === '/provider.js') return js(path.join(__dirname, 'cache-terrain-client.js'));
            if (/^\/scene-(state|gl|chrome|input|view)\.js$/.test(p)) return js(path.join(__dirname, p.slice(1)));
            if (p === '/itemicons.js' || p === '/itemiconmanifest.js') return js(path.join(web, p.slice(1)));
            if (p.startsWith('/itemicons_cache/')) {
                const f = path.join(web, 'itemicons_cache', path.basename(p));
                return fs.existsSync(f) ? route.fulfill({ contentType: 'image/png', body: fs.readFileSync(f) }) : route.fulfill({ status: 404, body: '' });
            }
            if (p === '/capture/client.js') return route.fulfill({ contentType: 'application/javascript', body: adapter });
            if (p === '/api/stream/open') { opened++; requests = 0; lastSent = null; lastSeq = 0; const body = JSON.parse(route.request().postData()); return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ session: 'fixture-' + opened, transport: mode === 'scene-delta' ? body.transport : 'full-snapshot', fps: 5, maxFps: 5 }) }); }
            if (p === '/api/stream/close') { closed++; return route.fulfill({ contentType: 'application/json', body: '{}' }); }
            if (p === '/api/stream/frame') {
                requests++; const have = Number(url.searchParams.get('have')); haves.push(have);
                const epoch = requests >= epochBumpAt ? 2 : 1, sc = sceneFor(requests), seq = requests;
                let frame;
                if (mode !== 'scene-delta') frame = { seq, epoch, transport: 'full-snapshot', snapshot: snapshotOf(sc) };
                else if (badBaseAt === requests) frame = { seq, epoch, transport: 'scene-delta', tick: sc.tick, delta: { base: 999, remove: ['n1'] } };
                else if (lastSent && have === lastSeq) frame = { seq, epoch, transport: 'scene-delta', tick: sc.tick, delta: diff(lastSent, sc, lastSeq) };
                else frame = { seq, epoch, transport: 'scene-delta', tick: sc.tick, scene: sc };
                if (badBaseAt !== requests) { lastSent = sc; lastSeq = seq; }
                return route.fulfill({ contentType: 'application/json', body: JSON.stringify(frame) });
            }
            return route.fulfill({ contentType: 'text/html', body: '<div id="host" style="position:relative;width:765px;height:503px"></div>' });
        });
        await page.goto('http://terrain.test/');
        const mount = opts => page.evaluate(async o => { window.provider ||= await import('/provider.js'); window.view = await provider.mount(document.querySelector('#host'), { account: 'fixture', fps: 60, ...o }); }, opts || {});

        // A. scene-delta
        await mount();
        await page.waitForFunction(() => view.stats().frames >= 6, null, { timeout: 30000 });
        const a = await page.evaluate(() => {
            const s = view.stats();
            const gl = document.querySelector('#host canvas').getContext('webgl2') || document.querySelector('#host canvas').getContext('webgl');
            const grab = c => { const g = c.getContext('2d'), d = g.getImageData(0, 0, c.width, c.height).data; let n = 0; for (let i = 3; i < d.length; i += 4) if (d[i] && (d[i - 3] || d[i - 2] || d[i - 1])) n++; return n; };
            const [scene, labels, minimap] = document.querySelectorAll('#host canvas');
            const slots = [...document.querySelectorAll('#host [data-slot]')];
            return { s, glError: gl.getError(), labelPixels: grab(labels), minimapPixels: grab(minimap),
                filledSlots: slots.filter(x => x.querySelector('img,svg')).length, slotCount: slots.length,
                coinsText: slots[27]?.textContent, chat: document.querySelector('#host [aria-label="Status and recent chat"]')?.textContent,
                canvases: document.querySelectorAll('canvas').length };
        });
        assert.equal(a.s.transport, 'scene-delta');
        assert.equal(a.s.fullFrames, 1, 'exactly one whole scene, then deltas');
        assert.ok(a.s.deltaFrames >= 5, 'deltas confirmed by have');
        assert.ok(a.s.deltaBytes / a.s.deltaFrames < a.s.fullBytes, 'a delta is smaller than the whole scene');
        assert.deepEqual(haves.slice(0, 3), [0, 1, 2], 'have names the last applied seq');
        assert.deepEqual(await page.evaluate(() => window.decodedAt), [3200, 3206], 'a 1-tile step reuses the terrain; a 6-tile move re-decodes');
        assert.equal(a.s.entities, 3);
        assert.equal(a.s.gl.instances, 4, 'self, n1, p5 and the ground item drawn as models');
        assert.equal(a.s.gl.markerTriangles, 12, 'npc type 2000 never loads: one marker pillar stands in');
        assert.equal(a.s.gl.models, 4, 'one GPU buffer per distinct model');
        assert.equal(a.s.gl.drawCalls, 6, 'terrain + 4 instances + one marker batch');
        assert.ok(a.s.animatedInstances >= 2, `ready/walk sequences drawn on the self and p5 models (${a.s.animatedInstances})`);
        assert.ok(a.s.animationsLoaded >= 2, 'sequences exported once per model and sequence');
        const animReqs = await page.evaluate(() => window.animReqs);
        assert.ok(animReqs.includes('player:7') && animReqs.some(r => r.startsWith('npc:')) && animReqs.includes('player:8'), `ready and walk sequences requested (${animReqs})`);
        // Idle but animated: the view keeps painting between observations.
        const before = await page.evaluate(() => view.stats().paints);
        await page.waitForTimeout(500);
        assert.ok((await page.evaluate(() => view.stats().paints)) - before >= 5, 'an animated model repaints without new observations');
        assert.equal(a.glError, 0);
        assert.ok(a.labelPixels > 50 && a.minimapPixels > 500, `labels ${a.labelPixels} px, minimap ${a.minimapPixels} px`);
        assert.equal(a.slotCount, 28);
        assert.ok(a.filledSlots >= 7, `backpack slots drawn: ${a.filledSlots}`);
        assert.equal(a.coinsText, '123K', 'stack count in the game\'s own K format');
        assert.match(a.chat, /ProgressiveFisher · fishing/);
        assert.ok(a.s.paints > a.s.frames, 'eased movement repaints between observations');
        assert.equal(a.canvases, 4, 'scene, labels, minimap and the hidden decoder canvas');
        if (output) {
            const png = await page.locator('#host').screenshot();
            fs.mkdirSync(path.dirname(path.resolve(output)), { recursive: true }); fs.writeFileSync(output, png);
        }
        await page.waitForFunction(() => view.stats().lastError.includes('epoch'), null, { timeout: 30000 });
        await page.waitForFunction(() => !document.querySelector('#canvas'));
        assert.equal(closed, 1);
        assert.equal(await page.evaluate(() => document.querySelectorAll('#host canvas').length), 0);

        // B. a delta on the wrong base -> whole scene requested
        haves = []; badBaseAt = 3; epochBumpAt = 1e9;
        await mount();
        await page.waitForFunction(() => view.stats().frames >= 5, null, { timeout: 30000 });
        const b = await page.evaluate(() => view.stats());
        assert.equal(haves[3], 0, `after a bad base the provider asks for a whole scene (haves ${haves})`);
        assert.equal(b.resyncs, 1);
        assert.equal(b.fullFrames, 2);
        await page.evaluate(() => view.destroy());
        assert.equal(closed, 2);

        // C. old server: full-snapshot only
        mode = 'full-snapshot'; badBaseAt = 0;
        await mount();
        await page.waitForFunction(() => view.stats().frames >= 3, null, { timeout: 30000 });
        const c = await page.evaluate(() => view.stats());
        assert.equal(c.transport, 'full-snapshot');
        assert.equal(c.entities, 3);
        assert.equal(c.gl.instances, 4, 'the full-snapshot fallback builds the same models from snapshot appearances');
        assert.ok(c.inventorySlots >= 3);
        await page.evaluate(() => view.destroy());
        assert.equal(closed, 3);

        // D. abort while decoding: no late mesh upload, lease and DOM cleaned up.
        mode = 'scene-delta';
        await page.waitForFunction(() => !document.querySelector('#canvas'));
        await page.evaluate(() => { window.delay = 400; window.decodes = 0; window.cancel = new AbortController(); });
        await mount({ signal: undefined });
        await page.evaluate(async () => { await view.destroy(); window.cancel = new AbortController(); window.view = await provider.mount(document.querySelector('#host'), { account: 'fixture', fps: 15, signal: cancel.signal }); });
        await page.waitForFunction(() => window.decodes >= 1);
        await page.evaluate(() => cancel.abort());
        await page.waitForFunction(() => !document.querySelector('#canvas'));
        const d = await page.evaluate(() => view.stats());
        assert.equal(d.uploads, 0);
        assert.equal(opened, closed, 'every lease opened was closed exactly once');
        assert.equal(sockets, 0);
        assert.deepEqual(pageErrors, []);
        console.log(JSON.stringify({ fixture: 'synthetic', sceneDelta: { fullFrames: a.s.fullFrames, fullBytes: a.s.fullBytes, deltaFrames: a.s.deltaFrames, avgDeltaBytes: Math.round(a.s.deltaBytes / a.s.deltaFrames),
            paints: a.s.paints, frames: a.s.frames, triangles: a.s.gl.triangles, modelTriangles: a.s.gl.modelTriangles, markerTriangles: a.s.gl.markerTriangles, drawCalls: a.s.gl.drawCalls, instances: a.s.gl.instances, maxPaintMs: a.s.maxPaintMs },
            terrainReusedFor1TileStep: true, badBaseResyncs: b.resyncs, fullSnapshotFallback: true, abortDuringDecodeCleans: true, opened, closed, gameSockets: sockets }));
    } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
