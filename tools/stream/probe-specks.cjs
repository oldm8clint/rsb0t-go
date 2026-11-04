// Frozen cache geometry probe. No leases, input or game actions.
const fs = require('fs'), path = require('path');
const assert = require('assert/strict');
const {chromium} = require('../../../rsb0t-main/node_modules/playwright-core');
const root = path.resolve(__dirname,'../..');
const {cacheEntry}=require('./cache-fixture.cjs');
(async()=>{
    const output = path.resolve(process.argv[3] || 'artifacts/specks-probe');
    fs.mkdirSync(output,{recursive:true});
    const snapshot = JSON.parse(fs.readFileSync(process.argv[2] || 'artifacts/stream-next/snapshot.json'));
    const browser = await chromium.launch({executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',headless:true,args:['--use-angle=d3d11','--ignore-gpu-blocklist','--enable-gpu']});
    try {
        const page=await browser.newPage({viewport:{width:960,height:640}});
        page.on('pageerror',e=>console.error(e.message));
        page.on('console',m=>{if(m.type()==='warning'||m.type()==='error')console.error(m.text());});
        await page.route('**/*',r=>r.fulfill({status:404,body:'Offline fixture: network unavailable'}));
        await page.route('**/capture/cache/*/*',r=>{
            const parts=new URL(r.request().url()).pathname.split('/');
            return r.fulfill({contentType:'application/octet-stream',body:cacheEntry(Number(parts.at(-2)),Number(parts.at(-1)))});
        });
        await page.route('**/speck-probe*',r=>r.fulfill({contentType:'text/html',body:'<style>body{margin:0}#world{width:960px;height:640px}</style><canvas id="canvas" width="765" height="503" hidden></canvas><div id="status" hidden></div><canvas id="world"></canvas>'}));
        await page.route('**/stream/scene-gl.js',r=>r.fulfill({contentType:'text/javascript',body:fs.readFileSync(process.env.STREAM_GL || path.join(root,'tools/stream/scene-gl.js'),'utf8')}));
        await page.route('**/capture/client.js*',r=>r.fulfill({contentType:'text/javascript',body:fs.readFileSync(process.env.CAPTURE_RENDERER || path.join(root,'run/capture/client.js'))}));
        const query=new URLSearchParams();
        if(process.argv.includes('--webgl1'))query.set('webgl','1');
        if(process.argv.includes('--debuggl'))query.set('debuggl','1');
        await page.goto('http://127.0.0.1:7300/speck-probe?'+query);
        await page.evaluate(async snapshot=>{
            // Cache lighting has random gamma jitter; freeze that too.
            Math.random=()=>.5;
            const adapter=await import('/capture/client.js?probe=1');
            adapter.configure({highDetail:true});
            const mesh=await adapter.sceneGpuMesh(snapshot,32,true);
            const atlas=await adapter.textureAtlas();
            const {createSceneRenderer,modelMatrix}=await import('/stream/scene-gl.js');
            const renderer=createSceneRenderer(document.querySelector('#world'));
            const gl=renderer.gl, clearColor=gl.clearColor.bind(gl);
            if(typeof location!=='undefined'&&location.search.includes('debuggl'))for(const name of ['texImage2D','copyTexSubImage2D','drawArrays']){
                const call=gl[name].bind(gl);gl[name]=(...args)=>{const value=call(...args),error=gl.getError();if(error)throw Error(name+' GL '+error);return value;};
            }
            // Magenta background distinguishes missing geometry from dark surfaces.
            gl.clearColor=()=>clearColor(1,0,1,1);
            renderer.setTextureAverages(await adapter.textureAverages());
            const instances=[];
            const add=(key,vertices,alpha=[])=>{
                renderer.setModel(key,new Float32Array(vertices),new Float32Array(alpha));
                instances.push({key,m:modelMatrix(0,0,0,0)});
            };
            for(let l=0;l<=snapshot.level;l++) add('terrain:'+l,mesh.terrain[l]);
            mesh.chunks.forEach((c,i)=>{if(c.level<=snapshot.level)add('chunk:'+i,c.vertices,c.alpha);});
            const corners=mesh.heightsSide, centre=32;
            const ground=mesh.heights[centre*corners+centre];
            window.probe={renderer,atlas,mesh,instances,clearColor,adapter,snapshot,camera:{x:0,y:ground,z:0,yaw:0,pitch:.95,distance:20}};
        },snapshot);
        const result=await page.evaluate(()=>{
            const {renderer,mesh,instances}=probe;
            const darkVertices=data=>{
                let dark=0,untextured=0;
                for(let i=0;i<data.length;i+=10)if(data[i+3]+data[i+4]+data[i+5]<=24/255){dark++;if(data[i+9]<0)untextured++;}
                return {dark,untextured};
            };
            return {terrain:mesh.terrain.map(darkVertices),scenery:mesh.chunks.map(c=>darkVertices(c.vertices)),triangles:mesh.terrainTriangles,origin:mesh.mapBase,ground:probe.camera.y};
        });
        result.frames=[];
        for(const textures of [false,true])for(const yaw of [0,.8,1.6,2.4,3.2,4,4.8,5.6]) {
            const shot=await page.evaluate(({textures,yaw})=>{
                const {renderer,instances,atlas}=probe;
                if(textures&&!probe.texturesInstalled){renderer.setTextures(atlas);probe.texturesInstalled=true;}
                renderer.draw({...probe.camera,yaw},instances);
                const gl=renderer.gl,W=gl.drawingBufferWidth,H=gl.drawingBufferHeight;
                const px=new Uint8Array(W*H*4);gl.readPixels(0,0,W,H,gl.RGBA,gl.UNSIGNED_BYTE,px);
                const dark=i=>px[i*4]+px[i*4+1]+px[i*4+2]<=24;
                const seen=new Uint8Array(W*H),components=[];
                for(let i=0;i<W*H;i++)if(!seen[i]&&dark(i)){
                    let stack=[i],n=0,edge=false,xsum=0,ysum=0;seen[i]=1;
                    while(stack.length){const at=stack.pop(),x=at%W,y=Math.floor(at/W);n++;xsum+=x;ysum+=y;edge ||= x===0||y===0||x===W-1||y===H-1;
                        for(const next of[x>0?at-1:-1,x<W-1?at+1:-1,y>0?at-W:-1,y<H-1?at+W:-1])if(next>=0&&!seen[next]&&dark(next)){seen[next]=1;stack.push(next);}}
                    if(!edge&&n<=8)components.push({n,x:Math.round(xsum/n),y:H-1-Math.round(ysum/n)});
                }
                return {textures,yaw,components,png:document.querySelector('#world').toDataURL(),glError:gl.getError()};
            },{textures,yaw});
            fs.writeFileSync(path.join(output,`${textures?'textured':'average'}-${yaw}.png`),Buffer.from(shot.png.split(',')[1],'base64'));
            delete shot.png;result.frames.push(shot);
            assert.equal(shot.glError,0,'No GPU errors');
            if(process.argv.includes('--require-zero'))assert.equal(shot.components.length,0,`Specks remain at yaw=${yaw}, textures=${textures}`);
        }
        if(process.argv.includes('--interfaces')){
            result.interfaces=[];
            for(const [name,main,overlay,side] of [['open',-1,-1,-1],['bank',5292,-1,2005],['overlay',-1,5292,-1],['closed',-1,-1,-1]]){
                const shot=await page.evaluate(async({main,overlay,side})=>{
                    const snapshot=structuredClone(probe.snapshot);Object.assign(snapshot.ui,{main,overlay,side});
                    const frame=await probe.adapter.interfaceFrame(snapshot,{yaw:0});
                    return {png:frame.canvas.toDataURL(),modalPixels:frame.modalPixels,viewportReadback:frame.viewportReadback};
                },{main,overlay,side});
                fs.writeFileSync(path.join(output,'interface-'+name+'.png'),Buffer.from(shot.png.split(',')[1],'base64'));
                delete shot.png;result.interfaces.push({name,...shot});
                if(main===-1&&overlay===-1)assert.equal(shot.modalPixels,0);
                else assert(shot.modalPixels>0,'Main modal/overlay must remain visible');
            }
        }
        if(process.argv.includes('--bench')) {
            if(process.argv.includes('--interfaces')){
                result.interfaceBenchmark=await page.evaluate(async()=>{
                    const {adapter}=probe,windows=[];
                    const prototype=CanvasRenderingContext2D.prototype,original=prototype.getImageData;
                    let readbacks=0;
                    prototype.getImageData=function(...args){
                        if(this.canvas.width===765&&this.canvas.height===503&&args[0]===4&&args[1]===4&&args[2]===512&&args[3]===334)readbacks++;
                        return original.apply(this,args);
                    };
                    try{
                        for(const main of [-1,5292]){
                            const snapshot=structuredClone(probe.snapshot);Object.assign(snapshot.ui,{main,overlay:-1,side:main===-1?-1:2005});
                            for(let i=0;i<4;i++)await adapter.interfaceFrame(snapshot,{yaw:i*17});
                            for(let window=0;window<3;window++){
                                const before=readbacks,start=performance.now();
                                for(let i=0;i<24;i++)await adapter.interfaceFrame(snapshot,{yaw:i*37});
                                windows.push({main,ms:(performance.now()-start)/24,readbacks:readbacks-before});
                            }
                        }
                    }finally{prototype.getImageData=original;}
                    return windows;
                });
                console.log(JSON.stringify({interfaceBenchmark:result.interfaceBenchmark}));
            }
            result.benchmark=await page.evaluate(()=>{
                const {renderer,instances,camera}=probe,gl=renderer.gl,windows=[],pixel=new Uint8Array(4);
                for(const resolution of [1,2]){
                    renderer.setResolution(resolution);
                    for(let i=0;i<8;i++){renderer.draw({...camera,yaw:i*.17},instances);gl.finish();}
                    for(let window=0;window<3;window++){
                        let cpu=0,total=0;
                        for(let i=0;i<40;i++){
                            const start=performance.now();renderer.draw({...camera,yaw:i*Math.PI/20},instances);
                            cpu+=performance.now()-start;gl.readPixels(0,0,1,1,gl.RGBA,gl.UNSIGNED_BYTE,pixel);total+=performance.now()-start;
                        }
                        windows.push({resolution,cpuMs:cpu/40,totalMs:total/40});
                    }
                }
                renderer.setResolution(1);
                return windows;
            });
            console.log(JSON.stringify({benchmark:result.benchmark}));
        }
        if(process.argv.includes('--require-zero')) {
            result.synthetic=await page.evaluate(()=>{
                const {renderer,clearColor}=probe,gl=renderer.gl,canvas=document.querySelector('#world');
                canvas.style.width='128px';canvas.style.height='128px';
                const copy=gl.copyTexSubImage2D.bind(gl),allowed=new Set();let before;
                gl.copyTexSubImage2D=(...args)=>{
                    gl.enable(gl.SCISSOR_TEST);
                    const rectangle=(x,y,w,h,colour)=>{gl.scissor(x,y,w,h);clearColor(...colour,1);gl.clear(gl.COLOR_BUFFER_BIT);};
                    rectangle(0,0,128,128,[64/255,128/255,32/255]);
                    for(let n=1;n<=8;n++){
                        rectangle(10,10+n*8,n,1,[0,0,0]);
                        for(let x=10;x<10+n;x++)allowed.add((10+n*8)*128+x);
                    }
                    rectangle(50,30,9,1,[0,0,0]); // Larger thin detail must survive.
                    rectangle(50,50,10,10,[0,0,0]);
                    rectangle(0,100,4,1,[0,0,0]); // Screen-edge component must survive.
                    rectangle(80,80,30,30,[8/255,8/255,9/255]);
                    rectangle(90,90,1,1,[0,0,0]);allowed.add(90*128+90);
                    gl.disable(gl.SCISSOR_TEST);
                    before=new Uint8Array(128*128*4);gl.readPixels(0,0,128,128,gl.RGBA,gl.UNSIGNED_BYTE,before);
                    copy(...args);
                };
                renderer.draw(probe.camera,[]);gl.copyTexSubImage2D=copy;
                const after=new Uint8Array(before.length);gl.readPixels(0,0,128,128,gl.RGBA,gl.UNSIGNED_BYTE,after);
                for(let i=0;i<128*128;i++){
                    if(allowed.has(i)){if(after[i*4]+after[i*4+1]+after[i*4+2]<=24)throw Error('Synthetic speck remains');}
                    else for(let channel=0;channel<4;channel++)if(before[i*4+channel]!==after[i*4+channel])throw Error('Larger detail or boundary changed');
                }
                const texture=gl.getParameter(gl.TEXTURE_BINDING_2D),program=gl.getParameter(gl.CURRENT_PROGRAM);
                const filterBytes=renderer.stats.speckFilterBytes;
                renderer.destroy();
                if(gl.isTexture(texture)||gl.isProgram(program))throw Error('Filter resources leaked');
                return {removedPixels:allowed.size,preservedPixels:128*128-allowed.size,filterBytes,glError:gl.getError()};
            });
            assert.equal(result.synthetic.glError,0);
            assert.equal(result.synthetic.filterBytes,128*128*4);
        }
        fs.writeFileSync(path.join(output,'results.json'),JSON.stringify(result,null,2));
        console.log(JSON.stringify({origin:result.origin,frames:result.frames.map(f=>({textures:f.textures,yaw:f.yaw,specks:f.components.length})),darkScenery:result.scenery.reduce((n,c)=>n+c.dark,0),darkTerrain:result.terrain.reduce((n,c)=>n+c.dark,0)}));
    } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
