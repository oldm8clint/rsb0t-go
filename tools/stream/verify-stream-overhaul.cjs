// Read-only capture fixture. All stream leases, input and actions are intercepted.
// node tools/stream/verify-stream-overhaul.cjs PANEL ACCOUNT [OUTPUT_DIR]
const fs = require('fs');
const path = require('path');
const assert = require('assert/strict');
// RSB0T_PLAYWRIGHT: path of a playwright-core install (the default is the sibling rsb0t-main checkout's,
// which a git worktree sits one directory too deep to find).
const { chromium } = require(process.env.RSB0T_PLAYWRIGHT || '../../../rsb0t-main/node_modules/playwright-core');
const root = path.resolve(__dirname, '../..');
const verifyUpgrades = process.argv.includes('--upgrades') || !!process.env.VERIFY_UPGRADES;
const snapshotFile = process.argv.find(v=>v.startsWith('--snapshot='))?.slice(11) || process.env.FIXTURE_SNAPSHOT;
(async () => {
    const [base = 'http://127.0.0.1:7300', account = 'main', output = path.join(root, 'artifacts/stream-overhaul')] = process.argv.slice(2);
    let snapshot;
    if (snapshotFile) snapshot = JSON.parse(fs.readFileSync(snapshotFile,'utf8'));
    else {
        const capture = await fetch(base + '/api/capture', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({account}) });
        assert(capture.ok, 'Read-only capture must be available');
        ({snapshot} = await capture.json());
    }
    fs.mkdirSync(output, { recursive:true });
    fs.writeFileSync(path.join(output,'snapshot.json'),JSON.stringify(snapshot));
    const source = fs.readFileSync(path.join(root,'internal/controlpanel/streamview.go'),'utf8');
    const constant = name => source.match(new RegExp('const '+name+' = `([\\s\\S]*?)`'))[1];
    const browser = await chromium.launch({ executablePath:process.env.CAPTURE_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', headless:true, args:process.env.HARDWARE_GPU ? ['--use-angle=d3d11','--ignore-gpu-blocklist','--enable-gpu'] : [] });
    try {
        const page = await browser.newPage({ viewport:{width:1434,height:943} });
        const errors = [], inputRequests = [], actions = [];
        page.on('pageerror',e=>errors.push(e.message));
        if(process.argv.includes('--debug')){
            page.on('console',m=>{if(m.type()==='error'||m.type()==='warning')console.error(m.text());});
            page.on('requestfailed',r=>console.error(r.url(),r.failure()));
        }
        if(process.argv.includes('--offline')){
            const {cacheEntry}=require('./cache-fixture.cjs');
            const webRoot=path.join(root,'internal/controlpanel/web');
            await page.route('**/*',r=>{
                const file=path.resolve(webRoot,'.'+new URL(r.request().url()).pathname);
                if(file.startsWith(webRoot+path.sep)&&fs.existsSync(file)&&fs.statSync(file).isFile()){
                    const contentType=file.endsWith('.js')?'text/javascript':file.endsWith('.png')?'image/png':'application/octet-stream';
                    return r.fulfill({contentType,body:fs.readFileSync(file)});
                }
                return r.fulfill({status:404,body:'Offline fixture'});
            });
            await page.route('**/capture/cache/*/*',r=>{
                const parts=new URL(r.request().url()).pathname.split('/');
                return r.fulfill({contentType:'application/octet-stream',body:cacheEntry(Number(parts.at(-2)),Number(parts.at(-1)))});
            });
            await page.route('**/api/enginetick',r=>r.fulfill({contentType:'application/json',body:JSON.stringify({available:true,tickRateMs:600})}));
        }
        let seq=0, begin=Date.now(), deny=true, leases=0, failFrame=false;
        await page.route('**/stream/**', async r => {
            const url=new URL(r.request().url()), name=path.basename(url.pathname);
            if (name==='view') return r.fulfill({contentType:'text/html',body:constant('streamViewPage')});
            if (name==='view.js') return r.fulfill({contentType:'text/javascript',body:constant('streamViewScript')});
            if (name==='view.css') return r.fulfill({contentType:'text/css',body:constant('streamViewStyle')});
            const file=path.join(root,'tools/stream',name==='client.js'?'cache-terrain-client.js':name);
            if (fs.existsSync(file)) return r.fulfill({contentType:'text/javascript',body:fs.readFileSync(name==='scene-gl.js' && process.env.STREAM_GL ? process.env.STREAM_GL : file)});
            return r.continue();
        });
        await page.route('**/capture/client.js*',r=>r.fulfill({contentType:'text/javascript',body:fs.readFileSync(process.env.CAPTURE_RENDERER || path.join(root,'run/capture/client.js'))}));
        await page.route('**/api/stream/**',async r=>{
            const endpoint=new URL(r.request().url()).pathname;
            let body={}, status=200;
            if (endpoint.endsWith('/open')) { leases++; body={session:'fixture',epoch:1,transport:'full-snapshot',fps:5,maxFps:5}; }
            if (endpoint.endsWith('/frame')) {
                if (failFrame) { failFrame=false; return r.fulfill({status:409,contentType:'application/json',body:JSON.stringify({error:'Fixture lease lost'})}); }
                const s=structuredClone(snapshot), tick=s.ui.tick+Math.floor((Date.now()-begin)/600);
                s.ui.tick=tick;
                const combat={health:7,total:10,tick,seen:true,hits:[{damage:3,type:1,tick,seen:true}],spot:{seen:false}};
                s.combat=combat;
                if (verifyUpgrades) {
                    s.ui.playerOps = ['Follow','Attack','','Trade with',''];
                    s.ui.playerOpPriority = [true,false,false,false,false];
                    s.players = [{index:42,name:'Fixture player',x:s.x+1,z:s.z,appearance:s.appearance,anim:-1,faceEntity:-1}];
                }
                if (s.npcs[0]) s.npcs[0].Combat=combat;
                s.effects=[{id:10,tick,level:s.level,x:s.x-2,z:s.z,toX:s.x+2,toZ:s.z,height:64,endHeight:32,start:0,end:30,slope:16,arc:16,target:0,projectile:true}];
                body={session:'fixture',epoch:1,seq:++seq,capturedAt:Date.now(),snapshot:s};
            }
            if (endpoint.endsWith('/input')) {
                inputRequests.push(r.request().postDataJSON());
                if (deny) {status=409;body={error:'Fixture control denied'};} else body={ok:true,acknowledged:r.request().postDataJSON().enabled};
            }
            if (endpoint.endsWith('/action')) {
                assert(verifyUpgrades,'Fixture must not send game actions');
                actions.push(r.request().postDataJSON());body={ok:true,label:'Fixture action'};
            }
            return r.fulfill({status,contentType:'application/json',body:JSON.stringify(body)});
        });
        await page.goto(base+'/stream/view?account='+encodeURIComponent(account)+'&fps=60&clean=1'+(process.argv.includes('--webgl1')?'&webgl=1':''));
        await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().terrain?.triangles>0 || /shader|uniform|precision|rebuild|error|failed/i.test(document.querySelector('#viewstatus').textContent),null,{timeout:180000});
        assert(await page.evaluate(()=>document.querySelector('#host').__streamStats?.().terrain?.triangles>0),await page.locator('#viewstatus').textContent());
        console.log('Terrain ready');
        await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().interfaceFrames>0,null,{timeout:120000});
        console.log('Interfaces ready');
        await page.mouse.move(5,5);
        await page.locator('#viewtools').click();
        await page.locator('[data-preset="loot"]').click();
        assert(await page.evaluate(() => { const s=document.querySelector('#host').__streamStats(); return s.camera.mode==='top-down' && s.toggles.items && s.toggles.grid; }));
        await page.locator('#viewfps').selectOption('15');
        assert.equal(await page.evaluate(()=>document.querySelector('#host').__streamStats().renderFps),15);
        const downloadPromise=page.waitForEvent('download');
        await page.locator('#viewscreenshot').click();
        const download=await downloadPromise;
        await download.saveAs(path.join(output,'gameplay-tools.png'));
        const png=fs.readFileSync(path.join(output,'gameplay-tools.png'));
        assert.equal(png.subarray(1,4).toString(),'PNG');
        await page.locator('[data-preset="combat"]').click();
        assert(await page.evaluate(()=>document.querySelector('#host').__streamStats().toggles.names));
        await page.locator('[data-preset="reset"]').click();
        await page.locator('#viewfps').selectOption('60');
        await page.locator('#viewtools').click();
        console.log('Gameplay presets, FPS and PNG capture passed');
        if (process.argv.includes('--next')) {
            await page.locator('#viewtools').click();
            await page.locator('[data-toggle="distances"]').check();
            assert(await page.evaluate(()=>document.querySelector('#host').__streamStats().toggles.distances));
            await page.locator('[data-preset="combat"]').click();
            await page.locator('#viewsavecamera').click();
            await page.locator('[data-preset="overview"]').click();
            await page.locator('#viewrestorecamera').click();
            assert.equal(await page.evaluate(()=>document.querySelector('#host').__streamStats().camera.mode),'follow');
            assert.equal(await page.evaluate(()=>document.querySelector('#host').__streamStats().camera.targetDistance),12);
            await page.locator('[data-quality="low"]').click();
            assert(await page.evaluate(()=>{const s=document.querySelector('#host').__streamStats();return s.renderFps===15&&s.renderDistance===16&&s.resolution===.75;}));
            await page.locator('[data-quality="balanced"]').click();
            assert(await page.evaluate(()=>{const s=document.querySelector('#host').__streamStats();return s.renderFps===30&&s.renderDistance===32&&s.resolution===1;}));
            const diagnosticDownload=page.waitForEvent('download');
            await page.locator('#viewdiagnostics').click();
            const diagnostic=await diagnosticDownload;
            await diagnostic.saveAs(path.join(output,'diagnostics.json'));
            const data=JSON.parse(fs.readFileSync(path.join(output,'diagnostics.json'),'utf8'));
            assert(data.stats.gl.webgl>=1&&data.capturedAt);
            await page.locator('[data-preset="reset"]').click();
            await page.locator('#viewfps').selectOption('60');
            await page.locator('#viewtools').click();
            console.log('Distance labels, camera bookmark, quality presets and diagnostics passed');
        }
        await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().modelsLoaded>0);
        await page.mouse.move(5,5);
        await page.locator('#viewinput').click();
        await page.waitForFunction(()=>document.querySelector('#viewstatus').textContent.includes('Fixture control denied'));
        assert.equal(await page.locator('#viewinput').getAttribute('aria-pressed'),'false');
        deny=false;
        await page.mouse.move(5,5);
        await page.locator('#viewinput').click();
        await page.waitForFunction(()=>document.querySelector('#viewinput').getAttribute('aria-pressed')==='true');
        if (verifyUpgrades) {
            await page.waitForFunction(()=>document.querySelector('#host').__streamPickTargets().some(p=>p.key==='p42'&&p.point));
            const target=await page.evaluate(()=>{
                const host=document.querySelector('#host'),p=host.__streamPickTargets().find(p=>p.key==='p42'),r=host.querySelector('canvas').getBoundingClientRect();
                return {x:r.left+p.point[0],y:r.top+p.point[1]};
            });
            await page.mouse.click(target.x,target.y,{button:'right'});
            await page.getByRole('menuitem',{name:'Trade with Fixture player',exact:true}).click();
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats().inputLog.some(l=>l.label==='Trade with Fixture player'));
            assert.deepEqual(actions[0].action,{kind:'player',index:42,op:4});
            await page.locator('#viewtools').click();
            await page.locator('#viewdistance').selectOption('48');
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats().terrain.radius===48,null,{timeout:120000});
            await page.locator('#viewresolution').selectOption('1.5');
            await page.locator('#viewroofs').selectOption('hide');
            await page.waitForFunction(()=>{const s=document.querySelector('#host').__streamStats();return s.resolution===1.5&&s.roofs==='hide'&&s.drawnTop===s.self.level;});
            await page.locator('#viewdistance').selectOption('32');
            await page.locator('#viewresolution').selectOption('1');
            await page.locator('#viewroofs').selectOption('auto');
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats().terrain.radius===32,null,{timeout:120000});
            await page.locator('#viewtools').click();
            console.log('Player right-click/action, 48-tile decode, resolution and roof settings passed');
        }
        await page.mouse.move(5,5);
        await page.locator('#viewinput').click();
        await page.waitForFunction(()=>document.querySelector('#viewinput').getAttribute('aria-pressed')==='false');
        const canvas=page.locator('#host canvas').first();
        await canvas.click({button:'right',position:{x:300,y:200}});
        await page.waitForSelector('[role="menu"] canvas');
        await page.screenshot({path:path.join(output,'menu.png')});
        await page.locator('[role="menuitem"][aria-label="Cancel"]').click();
        await page.mouse.move(100,100); await page.mouse.down();
        const stats=await page.evaluate(()=>{
            const host=document.querySelector('#host'),canvas=host.querySelector('canvas');
            window.dispatchEvent(new PointerEvent('pointermove',{clientX:100,clientY:100}));
            const out=document.createElement('canvas');out.width=canvas.width;out.height=canvas.height;
            const ctx=out.getContext('2d');ctx.drawImage(canvas,0,0);
            window.dispatchEvent(new PointerEvent('pointerup',{button:0,clientX:100,clientY:100}));
            const px=ctx.getImageData(0,0,out.width,out.height).data,W=out.width,H=out.height,seen=new Uint8Array(W*H);
            const dark=i=>px[i*4]+px[i*4+1]+px[i*4+2]<=24;
            let specks=0,darkPixels=0;
            for(let i=0;i<W*H;i++) if(!seen[i]&&dark(i)) {
                const stack=[i];seen[i]=1;let n=0,edge=false;
                while(stack.length) { const at=stack.pop(),x=at%W,y=Math.floor(at/W);n++;edge ||= x===0||y===0||x===W-1||y===H-1;
                    for(const next of [x>0?at-1:-1,x<W-1?at+1:-1,y>0?at-W:-1,y<H-1?at+W:-1]) if(next>=0&&!seen[next]&&dark(next)){seen[next]=1;stack.push(next);} }
                darkPixels+=n;if(n<=4&&!edge)specks++;
            }
            const gl=canvas.getContext('webgl2')||canvas.getContext('webgl'),ext=gl.getExtension('WEBGL_debug_renderer_info');
            return {...host.__streamStats(),specks,darkPixels,gpu:ext?gl.getParameter(ext.UNMASKED_RENDERER_WEBGL):gl.getParameter(gl.RENDERER)};
        });
        await page.mouse.up();
        await page.screenshot({path:path.join(output,'combat.png')});
        if (process.argv.includes('--strict-assets') || (!process.env.CAPTURE_RENDERER && !process.env.STREAM_GL)) {
            assert(stats.hitSprites>0,'Native hit sprites loaded');
            assert(stats.nativeFonts.includes('b12'),'Native menu font loaded');
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().effectsLoaded>0,null,{timeout:30000});
            await page.evaluate(()=>{const c=document.querySelector('#host canvas'),gl=c.getContext('webgl2')||c.getContext('webgl');window.fixtureLoss=gl.getExtension('WEBGL_lose_context');window.fixtureLoss?.loseContext();});
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().gl.contextLosses>0);
            await page.evaluate(()=>window.fixtureLoss.restoreContext());
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats?.().gl.contextRestores>0);
        }
        assert.equal(errors.length,0,errors.join('\n'));
        assert.deepEqual(inputRequests.map(r=>r.enabled),[true,true,false]);
        await page.mouse.move(5,5);
        await page.locator('#viewtools').click();
        await page.locator('[data-preset="loot"]').click();
        await page.locator('#viewtools').click();
        failFrame=true;
        // A lost lease no longer tears the view down: the provider reopens it by itself, on the same
        // renderer, and keeps painting.
        await page.waitForFunction(()=>{const s=document.querySelector('#host').__streamStats?.();return s && s.leases>=2 && !s.stopped && s.phase==='live';},null,{timeout:90000});
        assert(leases>=2,'The view reopens a lost view lease without being torn down');
        assert(await page.evaluate(()=>{const s=document.querySelector('#host').__streamStats();return s.camera.mode==='top-down' && s.toggles.items && s.toggles.grid;}),'Gameplay preferences restored after reconnect');
        assert.equal(await page.locator('#viewinput').getAttribute('aria-pressed'),'false','Reconnect remains view-only');
        assert.equal(inputRequests.length,3,'Reconnect must not reacquire manual input');
        let rotationFps;
        if (process.argv.includes('--bench')) {
            const profiler=process.argv.includes('--profile')?await page.context().newCDPSession(page):null;
            if(profiler){await profiler.send('Profiler.enable');await profiler.send('Profiler.start');}
            await page.waitForFunction(()=>document.querySelector('#host').__streamStats()?.terrain?.radius===32);
            await page.evaluate(()=>document.querySelector('#host').focus());
            const start=await page.evaluate(()=>({paints:document.querySelector('#host').__streamStats().paints,time:performance.now()}));
            await page.keyboard.down('ArrowLeft');
            await page.waitForTimeout(3000);
            await page.keyboard.up('ArrowLeft');
            rotationFps=await page.evaluate(start=>{const s=document.querySelector('#host').__streamStats();return Math.round((s.paints-start.paints)*10000/(performance.now()-start.time))/10;},start);
            if(profiler){const {profile}=await profiler.send('Profiler.stop');fs.writeFileSync(path.join(output,'rotation.cpuprofile'),JSON.stringify(profile));await profiler.detach();}
        }
        assert.equal(await page.evaluate(()=>{const c=document.querySelector('#host canvas'),gl=c.getContext('webgl2')||c.getContext('webgl');return gl.getError();}),0,'No WebGL errors after rendering/restoration');
        const result={frames:stats.frames,gpu:stats.gpu,specks:stats.specks,darkPixels:stats.darkPixels,triangles:stats.gl.triangles,drawCalls:stats.gl.drawCalls,culled:stats.gl.culled,gpuBytes:stats.gl.gpuBytes,avgPaintMs:stats.avgPaintMs,maxPaintMs:stats.maxPaintMs,decodeMs:stats.decodeMs,rotationFps,models:stats.modelsLoaded,interfaceFrames:stats.interfaceFrames,errors,inputRequests:inputRequests.length,actions:actions.length,reconnected:true,output};
        fs.writeFileSync(path.join(output,'results.json'),JSON.stringify(result,null,2));
        console.log(JSON.stringify(result));
    } finally { await browser.close(); }
})().catch(e=>{console.error(e);process.exitCode=1;});
