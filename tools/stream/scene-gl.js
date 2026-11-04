// WebGL scene renderer for the stream client.
//
// Geometry lives in static GPU buffers, uploaded once per distinct mesh: the decoded cache
// terrain, and one buffer per cache model (an npc type, a player's appearance, a ground
// item). Each painted frame draws the terrain once and every visible entity as an INSTANCE
// of its model's buffer with a model matrix (tile position, ground height, yaw), so moving
// an entity costs one uniform, not a re-upload. Entities whose model is not available yet
// (still downloading, or an adapter that predates entityGpuMeshes) are drawn as a coloured
// marker pillar from a small dynamic buffer — a marker, not a model.
//
// Vertex format (10 floats): x y z  r g b a  u v tex. For an untextured face rgb is its lit
// colour. For a textured face (tex >= 0) rgb is the software rasterizer's brightness for
// that vertex's shade and the colour comes from the texture: sampled from the texture atlas
// when one is installed (setTextures), else the texture's average colour.
//
// World units are tiles: x east, y up, z north. Faces with alpha go in a second, blended
// pass drawn after all opaque geometry with depth writes off.

export const FLOATS = 10;
const STRIDE = FLOATS * 4;

export const MARKER = {
    npc: { colour: [0.95, 0.85, 0.2], height: 1.5, half: 0.28 },
    player: { colour: [0.95, 0.95, 0.95], height: 1.8, half: 0.26 },
    self: { colour: [0.35, 0.8, 1.0], height: 2.0, half: 0.3 },
};

const IDENTITY = new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

// GPU bytes kept for animation frames; the least recently drawn sequence is evicted past it
// (and re-requested if it is needed again).
export const ANIMATION_BUDGET = 96 * 1048576;

// One conservative sphere per spatial batch, computed only when geometry is uploaded.
export function meshBounds(...parts) {
    const min = [Infinity, Infinity, Infinity], max = [-Infinity, -Infinity, -Infinity];
    for (const data of parts) for (let i = 0; i < data.length; i += FLOATS) for (let j = 0; j < 3; j++) {
        min[j] = Math.min(min[j], data[i+j]); max[j] = Math.max(max[j], data[i+j]);
    }
    if (!Number.isFinite(min[0])) return null;
    return {x:(min[0]+max[0])/2,y:(min[1]+max[1])/2,z:(min[2]+max[2])/2,r:Math.hypot(max[0]-min[0],max[1]-min[1],max[2]-min[2])/2};
}

export function frustumPlanes(m) {
    const planes = [];
    for (let row = 0; row < 3; row++) for (const sign of [-1, 1]) {
        const p = [m[3]+sign*m[row],m[7]+sign*m[4+row],m[11]+sign*m[8+row],m[15]+sign*m[12+row]];
        const length = Math.hypot(p[0],p[1],p[2]);
        planes.push(p.map(v => v / length));
    }
    return planes;
}

export function boundsVisible(bounds, model, planes) {
    if (!bounds) return false;
    const x = model[0]*bounds.x+model[4]*bounds.y+model[8]*bounds.z+model[12];
    const y = model[1]*bounds.x+model[5]*bounds.y+model[9]*bounds.z+model[13];
    const z = model[2]*bounds.x+model[6]*bounds.y+model[10]*bounds.z+model[14];
    return planes.every(p => p[0]*x+p[1]*y+p[2]*z+p[3] >= -bounds.r);
}

export function createSceneRenderer(canvas) {
    // WebGL2 when available: cache textures then live in a mipmapped TEXTURE_2D_ARRAY with
    // hardware wrapping and anisotropic filtering, so distant textured ground and foliage do
    // not sparkle. WebGL1 keeps a nearest-neighbour atlas.
    const forceGl1 = typeof location !== 'undefined' && /[?&]webgl=1/.test(location.search);
    const gl = (!forceGl1 && canvas.getContext('webgl2', { alpha: false, antialias: true })) || canvas.getContext('webgl', { alpha: false, antialias: true });
    const gl2 = typeof WebGL2RenderingContext !== 'undefined' && gl instanceof WebGL2RenderingContext;
    if (!gl) throw Error('WebGL is unavailable');
    let program, u = {}, a = {};
    let speckProgram, speckTexture, speckBuffer, speckLayout, speckPosition, speckImage, speckSize;
    let speckWidth = 0, speckHeight = 0;
    const relative = new Float32Array(16);
    let terrain = null, terrainBuffer = null, markerBuffer = null, atlasTexture = null;
    let markers = new Float32Array(0), markerCount = 0, averages = new Float32Array(150), atlas = null;
    const models = new Map(); // key -> {opaque:{data,buffer,count}, alpha:{...}, bytes}
    const layouts = new Map(); // WebGL2 vertex-array state, one per geometry buffer
    // Animations: "model|seq" -> {base, baseAlpha (full vertices of frame 0), frames: [{opaque, alpha}
    // position-only buffers], bytes, used}. Bounded by ANIMATION_BUDGET bytes, least recently
    // drawn evicted first.
    const animations = new Map();
    let animationBytes = 0;
    let destroyed = false, lost = false, matrix = null, cssWidth = 1, cssHeight = 1;
    let resolution = 1;
    // Distance fog (setFog): the decoded map ends at a hard edge, which fog turns into a horizon; the
    // fog colour is also the clear colour. Off, the range is pushed out of reach and the clear is black.
    let fog = { on: false, radius: 32, colour: [0.34, 0.41, 0.52] };
    // Soft ground shadows under actors (setShadows): one fan of triangles each, built into a reused array.
    let shadows = new Float32Array(0), shadowCount = 0, shadowGeometry = new Float32Array(0), shadowBuffer = null;
    const stats = { webgl: 0, frames: 0, drawCalls: 0, triangles: 0, terrainTriangles: 0, modelTriangles: 0, markerTriangles: 0, instances: 0, models: 0, gpuBytes: 0, uploads: 0 };

    function shader(type, source) {
        const s = gl.createShader(type); gl.shaderSource(s, source); gl.compileShader(s);
        if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) { const m = gl.getShaderInfoLog(s); gl.deleteShader(s); throw Error(m); }
        return s;
    }
    function upload(data) {
        const b = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, b); gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW); return b;
    }
    function initialize() {
        const precision = gl.getShaderPrecisionFormat(gl.FRAGMENT_SHADER,gl.HIGH_FLOAT)?.precision ? 'highp' : 'mediump';
        stats.fragmentPrecision = precision;
        const v = shader(gl.VERTEX_SHADER, gl2 ? `#version 300 es
            in vec3 aPosition; in vec4 aColour; in vec2 aUv; in float aTex;
            uniform mat4 uMatrix; uniform mat4 uModel; uniform vec3 uTexAvg[50]; uniform mediump float uAtlas; uniform vec2 uFogRange;
            out vec4 vColour; out vec2 vUv; out float vTex; out float vFog;
            void main() {
                gl_Position = uMatrix * uModel * vec4(aPosition, 1.0);
                vFog = clamp((gl_Position.w - uFogRange.x) / (uFogRange.y - uFogRange.x), 0.0, 1.0);
                vTex = aTex; vUv = aUv;
                if (aTex >= 0.0 && uAtlas < 0.5) vColour = vec4(uTexAvg[int(aTex + 0.5)] * aColour.rgb, aColour.a);
                else vColour = aColour;
            }` : `
            attribute vec3 aPosition; attribute vec4 aColour; attribute vec2 aUv; attribute float aTex;
            uniform mat4 uMatrix; uniform mat4 uModel; uniform vec3 uTexAvg[50]; uniform mediump float uAtlas; uniform vec2 uFogRange;
            varying vec4 vColour; varying vec2 vUv; varying float vTex; varying float vFog;
            void main() {
                gl_Position = uMatrix * uModel * vec4(aPosition, 1.0);
                vFog = clamp((gl_Position.w - uFogRange.x) / (uFogRange.y - uFogRange.x), 0.0, 1.0);
                vTex = aTex; vUv = aUv;
                if (aTex >= 0.0 && uAtlas < 0.5) vColour = vec4(uTexAvg[int(aTex + 0.5)] * aColour.rgb, aColour.a);
                else vColour = aColour;
            }`);
        const f = shader(gl.FRAGMENT_SHADER, gl2 ? `#version 300 es
            precision ${precision} float; precision mediump sampler2DArray;
            in vec4 vColour; in vec2 vUv; in float vTex; in float vFog;
            uniform sampler2DArray uAtlasTex; uniform mediump float uAtlas; uniform vec2 uCells; uniform vec3 uFogColour;
            out vec4 outColour;
            void main() {
                vec4 c = vColour;
                if (vTex >= 0.0 && uAtlas > 0.5) {
                    vec4 t = texture(uAtlasTex, vec3(vUv, floor(vTex + 0.5)));
                    if (t.a < 0.5) discard;
                    c = vec4(t.rgb * vColour.rgb, vColour.a);
                }
                outColour = vec4(mix(c.rgb, uFogColour, vFog), c.a);
            }` : `precision ${precision} float;
            varying vec4 vColour; varying vec2 vUv; varying float vTex; varying float vFog;
            uniform sampler2D uAtlasTex; uniform mediump float uAtlas; uniform vec2 uCells; uniform vec3 uFogColour;
            void main() {
                vec4 c = vColour;
                if (vTex >= 0.0 && uAtlas > 0.5) {
                    float id = floor(vTex + 0.5);
                    vec2 cell = vec2(mod(id, uCells.x), floor(id / uCells.x));
                    vec2 uv = (cell + clamp(fract(vUv), 0.004, 0.996)) / uCells;
                    vec4 t = texture2D(uAtlasTex, uv);
                    if (t.a < 0.5) discard;
                    c = vec4(t.rgb * vColour.rgb, vColour.a);
                }
                gl_FragColor = vec4(mix(c.rgb, uFogColour, vFog), c.a);
            }`);
        program = gl.createProgram(); gl.attachShader(program, v); gl.attachShader(program, f); gl.linkProgram(program);
        gl.deleteShader(v); gl.deleteShader(f);
        if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw Error(gl.getProgramInfoLog(program));
        for (const n of ['uMatrix', 'uModel', 'uTexAvg', 'uAtlas', 'uAtlasTex', 'uCells', 'uFogRange', 'uFogColour']) u[n] = gl.getUniformLocation(program, n);
        for (const n of ['aPosition', 'aColour', 'aUv', 'aTex']) a[n] = gl.getAttribLocation(program, n);
        markerBuffer = gl.createBuffer(); shadowBuffer = gl.createBuffer();
        terrainBuffer = terrain ? upload(terrain) : null;
        for (const m of models.values()) for (const part of [m.opaque, m.alpha]) if (part.count) part.buffer = upload(part.data);
        for (const an of animations.values()) {
            for (const part of [an.base, an.baseAlpha]) if (part.count) part.buffer = upload(part.data);
            for (const fr of an.frames) for (const part of [fr.opaque, fr.alpha]) if (part.data.length) part.buffer = upload(part.data);
        }
        if (atlas) installAtlas();
        initializeSpeckFilter(precision);
    }
    function initializeSpeckFilter(precision) {
        const vertex = shader(gl.VERTEX_SHADER, gl2 ? `#version 300 es
            in vec2 aPosition; void main(){gl_Position=vec4(aPosition,0.0,1.0);}` :
            `attribute vec2 aPosition; void main(){gl_Position=vec4(aPosition,0.0,1.0);}`);
        const fragment = shader(gl.FRAGMENT_SHADER, `${gl2 ? '#version 300 es' : ''}
            precision ${precision} float;
            uniform sampler2D uImage; uniform vec2 uSize;
            ${gl2 ? 'out vec4 outColour;' : ''}
            vec4 pixel(vec2 p){return ${gl2 ? 'texture' : 'texture2D'}(uImage,(p+0.5)/uSize);}
            bool dark(vec4 c){return c.r+c.g+c.b <= 24.01/255.0;}
            vec4 filtered(){
                vec2 origin=floor(gl_FragCoord.xy); vec4 source=pixel(origin);
                if(!dark(source))return source;
                // Nine neighbouring dark pixels already prove a larger component.
                // Skip the flood fill for interiors of shadows and black background.
                bool solid=true;
                for(int y=-1;y<=1;y++){
                    for(int x=-1;x<=1;x++){
                        if(x==0&&y==0)continue;
                        if(!dark(pixel(origin+vec2(float(x),float(y))))){solid=false;break;}
                    }
                    if(!solid)break;
                }
                if(solid)return source;
                // A bounded flood fill identifies actual isolated components, rather
                // than blurring edges or lifting every black surface. Nine connected
                // pixels prove a larger feature and leave its original colour intact.
                vec2 queue[8]; queue[0]=origin; int count=1;
                vec3 boundary=vec3(0.0); float samples=0.0;
                for(int head=0;head<8;head++){
                    if(head>=count)break;
                    vec2 p=queue[head];
                    for(int direction=0;direction<4;direction++){
                        vec2 offset=direction==0?vec2(-1.0,0.0):direction==1?vec2(1.0,0.0):direction==2?vec2(0.0,-1.0):vec2(0.0,1.0);
                        vec2 next=p+offset;
                        if(next.x<0.0||next.y<0.0||next.x>=uSize.x||next.y>=uSize.y)return source;
                        vec4 colour=pixel(next);
                        if(!dark(colour)){boundary+=colour.rgb;samples+=1.0;continue;}
                        bool seen=false;
                        for(int k=0;k<8;k++){
                            if(k>=count)break;
                            if(all(equal(queue[k],next))){seen=true;break;}
                        }
                        if(!seen){if(count==8)return source;queue[count]=next;count++;}
                    }
                }
                if(samples==0.0)return source;
                vec3 fill=boundary/samples;
                // Keep rounding to RGBA8 from turning a just-above-threshold average
                // back into a dark pixel. This only touches the isolated component.
                fill*=max(1.0,(27.0/255.0)/(fill.r+fill.g+fill.b));
                return vec4(fill,source.a);
            }
            void main(){${gl2 ? 'outColour' : 'gl_FragColor'}=filtered();}`);
        speckProgram=gl.createProgram(); gl.attachShader(speckProgram,vertex); gl.attachShader(speckProgram,fragment); gl.linkProgram(speckProgram);
        gl.deleteShader(vertex); gl.deleteShader(fragment);
        if(!gl.getProgramParameter(speckProgram,gl.LINK_STATUS))throw Error(gl.getProgramInfoLog(speckProgram));
        speckPosition=gl.getAttribLocation(speckProgram,'aPosition');
        speckImage=gl.getUniformLocation(speckProgram,'uImage'); speckSize=gl.getUniformLocation(speckProgram,'uSize');
        speckBuffer=upload(new Float32Array([-1,-1,1,-1,-1,1,-1,1,1,-1,1,1]));
        speckTexture=gl.createTexture(); speckWidth=0; speckHeight=0;
        if(gl2){speckLayout=gl.createVertexArray();gl.bindVertexArray(speckLayout);}
        gl.bindBuffer(gl.ARRAY_BUFFER,speckBuffer); gl.enableVertexAttribArray(speckPosition); gl.vertexAttribPointer(speckPosition,2,gl.FLOAT,false,0,0);
        if(gl2)gl.bindVertexArray(null);
    }
    const SHADOW_SEGMENTS = 10, SHADOW_ALPHA = 0.42;
    const shadowCos = Float32Array.from({ length: SHADOW_SEGMENTS + 1 }, (_, i) => Math.cos(i / SHADOW_SEGMENTS * 2 * Math.PI));
    const shadowSin = Float32Array.from({ length: SHADOW_SEGMENTS + 1 }, (_, i) => Math.sin(i / SHADOW_SEGMENTS * 2 * Math.PI));
    // drawShadows: each shadow is a fan that fades from SHADOW_ALPHA at its centre to nothing at its
    // rim, blended over the ground without writing depth. Built into one reused array.
    function drawShadows() {
        const need = shadowCount * SHADOW_SEGMENTS * 3 * FLOATS;
        if (shadowGeometry.length < need) shadowGeometry = new Float32Array(Math.max(need, shadowGeometry.length * 2));
        const g = shadowGeometry;
        let o = 0;
        const vertex = (x, y, z, alpha) => { g[o] = x; g[o + 1] = y; g[o + 2] = z; g[o + 3] = 0; g[o + 4] = 0; g[o + 5] = 0; g[o + 6] = alpha; g[o + 7] = 0; g[o + 8] = 0; g[o + 9] = -1; o += FLOATS; };
        for (let i = 0; i < shadowCount; i++) {
            const x = shadows[i * 4], y = shadows[i * 4 + 1] + 0.025, z = shadows[i * 4 + 2], r = shadows[i * 4 + 3];
            for (let s = 0; s < SHADOW_SEGMENTS; s++) {
                vertex(x, y, z, SHADOW_ALPHA);
                vertex(x + shadowCos[s] * r, y, z + shadowSin[s] * r, 0);
                vertex(x + shadowCos[s + 1] * r, y, z + shadowSin[s + 1] * r, 0);
            }
        }
        gl.disable(gl.CULL_FACE); gl.enable(gl.BLEND); gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA); gl.depthMask(false);
        gl.enable(gl.POLYGON_OFFSET_FILL); gl.polygonOffset(-2, -2);
        bind(shadowBuffer); gl.bufferData(gl.ARRAY_BUFFER, g.subarray(0, o), gl.DYNAMIC_DRAW);
        gl.drawArrays(gl.TRIANGLES, 0, o / FLOATS);
        gl.disable(gl.POLYGON_OFFSET_FILL); gl.depthMask(true); gl.disable(gl.BLEND);
        return o / FLOATS;
    }
    function filterSpecks(width,height) {
        // Copy the resolved default framebuffer on the GPU, retaining hardware MSAA.
        // Interfaces and labels are separate canvases and never pass through this filter.
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D,speckTexture);
        if(speckWidth!==width||speckHeight!==height){
            // alpha:false gives an RGB default framebuffer. Matching its format is
            // required by copyTexSubImage2D on both WebGL versions.
            gl.texImage2D(gl.TEXTURE_2D,0,gl.RGB,width,height,0,gl.RGB,gl.UNSIGNED_BYTE,null);
            gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.NEAREST); gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.NEAREST);
            gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_S,gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_T,gl.CLAMP_TO_EDGE);
            speckWidth=width;speckHeight=height;stats.speckFilterBytes=width*height*4;recount();
        }
        gl.copyTexSubImage2D(gl.TEXTURE_2D,0,0,0,0,0,width,height);
        gl.disable(gl.DEPTH_TEST);gl.disable(gl.CULL_FACE);gl.disable(gl.BLEND);
        gl.useProgram(speckProgram);gl.uniform1i(speckImage,0);gl.uniform2f(speckSize,width,height);
        if(gl2)gl.bindVertexArray(speckLayout);
        else{gl.bindBuffer(gl.ARRAY_BUFFER,speckBuffer);gl.enableVertexAttribArray(speckPosition);gl.vertexAttribPointer(speckPosition,2,gl.FLOAT,false,0,0);}
        gl.drawArrays(gl.TRIANGLES,0,6);
        if(gl2)gl.bindVertexArray(null);
        stats.speckFilter='isolated dark components, 1–8 pixels';
    }
    function installAtlas() {
        if (atlasTexture) gl.deleteTexture(atlasTexture);
        atlasTexture = gl.createTexture();
        if (gl2) {
            // One 128 x 128 layer per cache texture, REPEAT-wrapped, mipmapped. Transparent texels
            // take the texture's average opaque colour first, so mip levels do not darken edges.
            const size = 128, layers = atlas.cells * atlas.rows, px = new Uint8Array(size * size * 4 * layers);
            for (let id = 0; id < layers; id++) {
                const cx = (id % atlas.cells) * size, cy = Math.floor(id / atlas.cells) * size;
                let r = 0, g = 0, b = 0, n = 0;
                for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) {
                    const o = ((cy + y) * atlas.width + cx + x) * 4;
                    if (atlas.pixels[o + 3]) { r += atlas.pixels[o]; g += atlas.pixels[o + 1]; b += atlas.pixels[o + 2]; n++; }
                }
                if (n) { r /= n; g /= n; b /= n; }
                for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) {
                    const o = ((cy + y) * atlas.width + cx + x) * 4, d = ((id * size + y) * size + x) * 4;
                    const solid = atlas.pixels[o + 3] !== 0;
                    px[d] = solid ? atlas.pixels[o] : r; px[d + 1] = solid ? atlas.pixels[o + 1] : g; px[d + 2] = solid ? atlas.pixels[o + 2] : b; px[d + 3] = atlas.pixels[o + 3];
                }
            }
            gl.bindTexture(gl.TEXTURE_2D_ARRAY, atlasTexture);
            gl.texImage3D(gl.TEXTURE_2D_ARRAY, 0, gl.RGBA, size, size, layers, 0, gl.RGBA, gl.UNSIGNED_BYTE, px);
            gl.generateMipmap(gl.TEXTURE_2D_ARRAY);
            gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
            gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MAG_FILTER, gl.NEAREST); // texels stay crisp up close, as in the game
            gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_S, gl.REPEAT);
            gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_T, gl.REPEAT);
            const aniso = gl.getExtension('EXT_texture_filter_anisotropic');
            if (aniso) gl.texParameterf(gl.TEXTURE_2D_ARRAY, aniso.TEXTURE_MAX_ANISOTROPY_EXT, Math.min(8, gl.getParameter(aniso.MAX_TEXTURE_MAX_ANISOTROPY_EXT)));
            stats.textureMode = aniso ? 'webgl2 array, mipmapped, anisotropic' : 'webgl2 array, mipmapped';
            stats.textureBytes = Math.round(px.byteLength * 4 / 3);
            return;
        }
        gl.bindTexture(gl.TEXTURE_2D, atlasTexture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, atlas.width, atlas.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, atlas.pixels);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        stats.textureMode = 'webgl1 atlas, nearest';
        stats.textureBytes = atlas.pixels.byteLength;
    }
    function lostHandler(e) { e.preventDefault(); lost = true; layouts.clear(); stats.contextLosses = (stats.contextLosses || 0) + 1; }
    function restoredHandler() {
        if (destroyed) return;
        lost = false; atlasTexture = null; matrix = null;
        initialize(); stats.contextRestores = (stats.contextRestores || 0) + 1;
    }
    canvas.addEventListener('webglcontextlost', lostHandler);
    canvas.addEventListener('webglcontextrestored', restoredHandler);
    initialize();

    function bind(buffer) {
        gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
        if (gl2) {
            const cached = layouts.get(buffer);
            if (cached) { gl.bindVertexArray(cached); return; }
            const layout = gl.createVertexArray(); layouts.set(buffer,layout); gl.bindVertexArray(layout);
        }
        gl.enableVertexAttribArray(a.aPosition); gl.vertexAttribPointer(a.aPosition, 3, gl.FLOAT, false, STRIDE, 0);
        gl.enableVertexAttribArray(a.aColour); gl.vertexAttribPointer(a.aColour, 4, gl.FLOAT, false, STRIDE, 12);
        gl.enableVertexAttribArray(a.aUv); gl.vertexAttribPointer(a.aUv, 2, gl.FLOAT, false, STRIDE, 28);
        gl.enableVertexAttribArray(a.aTex); gl.vertexAttribPointer(a.aTex, 1, gl.FLOAT, false, STRIDE, 36);
    }
    function deleteBuffer(buffer) {
        const layout = layouts.get(buffer);
        if (layout) { gl.deleteVertexArray(layout); layouts.delete(buffer); }
        gl.deleteBuffer(buffer);
    }
    function recount() {
        let bytes = terrain ? terrain.byteLength : 0;
        for (const m of models.values()) bytes += m.bytes;
        stats.gpuBytes = bytes + animationBytes + (atlas ? (stats.textureBytes || atlas.pixels.byteLength) : 0) + (stats.speckFilterBytes || 0); stats.models = models.size;
        stats.animations = animations.size; stats.animationBytes = animationBytes;
    }

    stats.webgl = gl2 ? 2 : 1;
    return {
        gl, stats,
        needsRedraw() { return !lost && !matrix; },
        setTerrain(vertices) {
            if (destroyed) throw Error('Renderer is closed');
            if (!(vertices instanceof Float32Array) || vertices.length % (FLOATS * 3)) throw Error('Invalid terrain mesh');
            terrain = vertices; stats.uploads++;
            if (!lost) { if (terrainBuffer) deleteBuffer(terrainBuffer); terrainBuffer = upload(terrain); }
            recount();
        },
        hasModel(key) { return models.has(key); },
        // setModel uploads one model's opaque and alpha vertex arrays once.
        setModel(key, opaque, alpha) {
            if (destroyed || models.has(key)) return;
            const part = data => ({ data, count: data.length / FLOATS, buffer: !lost && data.length ? upload(data) : null });
            models.set(key, { opaque: part(opaque), alpha: part(alpha), bounds: meshBounds(opaque,alpha), bytes: opaque.byteLength + alpha.byteLength, used: performance.now() });
            stats.uploads++; recount();
        },
        dropModel(key) {
            const m = models.get(key); if (!m) return;
            for (const p of [m.opaque, m.alpha]) if (p.buffer) deleteBuffer(p.buffer);
            models.delete(key); recount();
        },
        modelKeys() { return [...models.keys()]; },
        // evictModels drops models not drawn for idleMs (except keep(key)); returns their keys.
        evictModels(idleMs, keep) {
            const now = performance.now(), gone = [];
            for (const [k, m] of models) if (now - m.used > idleMs && !keep(k)) { this.dropModel(k); gone.push(k); }
            for (const [k, a] of animations) if (now - a.used > idleMs && !keep(k)) this.dropAnimation(k);
            stats.evicted = (stats.evicted || 0) + gone.length;
            return gone;
        },
        hasAnimation(key) { return animations.has(key); },
        // setAnimation uploads one sequence on one model: frame 0 as full vertices (its
        // colours, u/v and texture ids serve every frame) plus each frame's positions.
        setAnimation(key, base, baseAlpha, positions, alphaPositions) {
            if (destroyed || animations.has(key)) return;
            const part = data => ({ data, count: data.length / FLOATS, buffer: !lost && data.length ? upload(data) : null });
            const pos = data => ({ data, buffer: !lost && data.length ? upload(data) : null });
            const an = { base: part(base), baseAlpha: part(baseAlpha), frames: positions.map((p, i) => ({ opaque: pos(p), alpha: pos(alphaPositions[i]) })), bytes: 0, used: performance.now() };
            an.bytes = base.byteLength + baseAlpha.byteLength + positions.reduce((n, p, i) => n + p.byteLength + alphaPositions[i].byteLength, 0);
            animations.set(key, an); animationBytes += an.bytes; stats.uploads++;
            while (animationBytes > ANIMATION_BUDGET && animations.size > 1) {
                let oldest = null;
                for (const [k, v] of animations) if (k !== key && (!oldest || v.used < oldest[1].used)) oldest = [k, v];
                if (!oldest) break;
                this.dropAnimation(oldest[0]); stats.evictions = (stats.evictions || 0) + 1;
            }
            recount();
        },
        dropAnimation(key) {
            const an = animations.get(key); if (!an) return;
            for (const part of [an.base, an.baseAlpha]) if (part.buffer) deleteBuffer(part.buffer);
            for (const fr of an.frames) for (const part of [fr.opaque, fr.alpha]) if (part.buffer) deleteBuffer(part.buffer);
            animationBytes -= an.bytes; animations.delete(key); recount();
        },
        // setTextureAverages: 50 packed RGB ints, used for textured faces without an atlas.
        setTextureAverages(rgbs) {
            for (let i = 0; i < 50; i++) { const c = rgbs[i] | 0; averages[i * 3] = ((c >> 16) & 255) / 255; averages[i * 3 + 1] = ((c >> 8) & 255) / 255; averages[i * 3 + 2] = (c & 255) / 255; }
        },
        // setTextures: an RGBA atlas of cells x rows texture cells (alpha 0 = transparent texel).
        setTextures(next) {
            atlas = next; if (!lost) installAtlas(); recount();
        },
        setMarkers(vertices, count) { markers = vertices; markerCount = count; },
        // draw paints one frame. instances: [{key, m: Float32Array(16)}].
        setResolution(value) { if ([0.75,1,1.5,2].includes(value)) resolution = value; },
        setFog(next) { fog = { ...fog, ...next }; matrix = null; },
        // shadows: [x, y, z, radius] per shadow, ground height y, in tiles.
        setShadows(data, count) { shadows = data; shadowCount = count; },
        draw(camera, instances) {
            if (destroyed || lost) return false;
            const now = performance.now();
            const scale = Math.min((devicePixelRatio || 1) * resolution, 2);
            cssWidth = Math.max(1, canvas.clientWidth); cssHeight = Math.max(1, canvas.clientHeight);
            const w = Math.round(cssWidth * scale), h = Math.round(cssHeight * scale);
            if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
            gl.viewport(0, 0, w, h);
            gl.clearColor(fog.on ? fog.colour[0] : 0, fog.on ? fog.colour[1] : 0, fog.on ? fog.colour[2] : 0, 1); gl.enable(gl.DEPTH_TEST); gl.depthMask(true); gl.disable(gl.BLEND);
            gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
            matrix = cameraMatrix(camera, w / h);
            const planes = frustumPlanes(matrix);
            stats.culled = 0; stats.resolution = scale;
            gl.useProgram(program);
            // Render near the camera origin. Subtract large map coordinates on the CPU
            // rather than cancelling them in vertex shader floats (which can create seams).
            gl.uniformMatrix4fv(u.uMatrix, false, cameraMatrix({...camera,x:0,z:0},w/h));
            const modelUniform = m => {
                relative.set(m); relative[12] -= camera.x; relative[14] -= camera.z;
                gl.uniformMatrix4fv(u.uModel,false,relative);
            };
            gl.uniform3fv(u.uTexAvg, averages);
            // Fog starts a little past the middle of the decoded map and is complete at its edge,
            // measured from the camera, which sits camera.distance behind the target.
            if (fog.on) { gl.uniform2f(u.uFogRange, camera.distance + fog.radius * 0.5, camera.distance + fog.radius * 0.97); gl.uniform3fv(u.uFogColour, fog.colour); }
            else { gl.uniform2f(u.uFogRange, 1e8, 2e8); gl.uniform3f(u.uFogColour, 0, 0, 0); }
            gl.uniform1f(u.uAtlas, atlas ? 1 : 0);
            if (atlas) { gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl2 ? gl.TEXTURE_2D_ARRAY : gl.TEXTURE_2D, atlasTexture); gl.uniform1i(u.uAtlasTex, 0); gl.uniform2f(u.uCells, atlas.cells, atlas.rows); }
            let calls = 0, tris = 0, modelTris = 0, drawn = 0;
            modelUniform(IDENTITY);
            // Back faces are culled, as the game's rasterizer does for every model and ground
            // triangle (it draws a face only when its screen winding is clockwise with y down,
            // i.e. counter-clockwise in GL window space). Without this, the inside of a tree
            // showed through the transparent texels of its leaves as black specks.
            gl.enable(gl.CULL_FACE); gl.cullFace(gl.BACK); gl.frontFace(gl.CCW);
            if (terrain && terrainBuffer) { bind(terrainBuffer); gl.drawArrays(gl.TRIANGLES, 0, terrain.length / FLOATS); calls++; tris += terrain.length / FLOATS / 3; }
            const alphaQueue = [];
            let animated = 0;
            for (const inst of instances) {
                const an = inst.anim !== undefined ? animations.get(inst.anim) : null;
                const fr = an ? an.frames[inst.frame] : null;
                if (an && fr) {
                    an.used = now;
                    modelUniform(inst.m);
                    if (an.base.count && an.base.buffer && fr.opaque.buffer) {
                        bind(an.base.buffer);
                        gl.bindBuffer(gl.ARRAY_BUFFER, fr.opaque.buffer); gl.vertexAttribPointer(a.aPosition, 3, gl.FLOAT, false, 12, 0);
                        gl.drawArrays(gl.TRIANGLES, 0, an.base.count); calls++; modelTris += an.base.count / 3;
                    }
                    if (an.baseAlpha.count && an.baseAlpha.buffer && fr.alpha.buffer) alphaQueue.push([an.baseAlpha, inst.m, fr.alpha.buffer]);
                    drawn++; animated++;
                    continue;
                }
                const m = models.get(inst.key); if (!m) continue;
                if ((inst.key.startsWith('chunk:') || inst.key.startsWith('terrain:')) && !boundsVisible(m.bounds,inst.m,planes)) { stats.culled++; continue; }
                m.used = now;
                modelUniform(inst.m);
                // Ground and decorations can be coplanar. Bias only terrain depth so grass,
                // paths and floor details cannot flicker against it as the camera moves.
                const ground = inst.key.startsWith('terrain:');
                if (ground) { gl.enable(gl.POLYGON_OFFSET_FILL); gl.polygonOffset(1,1); }
                if (m.opaque.count && m.opaque.buffer) { bind(m.opaque.buffer); gl.drawArrays(gl.TRIANGLES, 0, m.opaque.count); calls++; modelTris += m.opaque.count / 3; }
                if (ground) gl.disable(gl.POLYGON_OFFSET_FILL);
                if (m.alpha.count && m.alpha.buffer) alphaQueue.push([m.alpha, inst.m]);
                drawn++;
            }
            stats.animatedInstances = animated;
            gl.disable(gl.CULL_FACE); // marker pillars are not game geometry and have no winding rule
            if (markerCount) {
                modelUniform(IDENTITY);
                bind(markerBuffer); gl.bufferData(gl.ARRAY_BUFFER, markers.subarray(0, markerCount * FLOATS), gl.DYNAMIC_DRAW);
                gl.drawArrays(gl.TRIANGLES, 0, markerCount); calls++; tris += markerCount / 3;
            }
            if (shadowCount) { modelUniform(IDENTITY); calls++; tris += drawShadows() / 3; }
            if (alphaQueue.length) {
                // Blended actors/items must be composited far to near, with depth writes off.
                alphaQueue.sort((a,b) => {
                    const depth = m => matrix[3]*m[12]+matrix[7]*m[13]+matrix[11]*m[14]+matrix[15];
                    return depth(b[1])-depth(a[1]);
                });
                gl.enable(gl.CULL_FACE);
                gl.enable(gl.BLEND); gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA); gl.depthMask(false);
                for (const [part, m, positions] of alphaQueue) {
                    modelUniform(m); bind(part.buffer);
                    if (positions) { gl.bindBuffer(gl.ARRAY_BUFFER, positions); gl.vertexAttribPointer(a.aPosition, 3, gl.FLOAT, false, 12, 0); }
                    gl.drawArrays(gl.TRIANGLES, 0, part.count); calls++; modelTris += part.count / 3;
                }
                gl.depthMask(true); gl.disable(gl.BLEND);
            }
            stats.frames++; stats.drawCalls = calls + 1; stats.triangles = tris + modelTris + 2; stats.modelTriangles = modelTris; stats.instances = drawn;
            stats.terrainTriangles = terrain ? terrain.length / FLOATS / 3 : 0; stats.markerTriangles = markerCount / 3;
            filterSpecks(w,h);
            return true;
        },
        // projectInto is project without the allocation: it writes (x, y, depth) into out.
        projectInto(out, x, y, z) {
            if (!matrix) return false;
            const m = matrix;
            const cx = m[0] * x + m[4] * y + m[8] * z + m[12], cy = m[1] * x + m[5] * y + m[9] * z + m[13];
            const cw = m[3] * x + m[7] * y + m[11] * z + m[15];
            if (cw <= 0.01) return false;
            out[0] = (cx / cw * 0.5 + 0.5) * cssWidth; out[1] = (1 - (cy / cw * 0.5 + 0.5)) * cssHeight; out[2] = cw;
            return true;
        },
        // project maps a world point to CSS pixels in the canvas using the last drawn camera.
        project(x, y, z) {
            if (!matrix) return null;
            const m = matrix;
            const cx = m[0] * x + m[4] * y + m[8] * z + m[12], cy = m[1] * x + m[5] * y + m[9] * z + m[13];
            const cw = m[3] * x + m[7] * y + m[11] * z + m[15];
            if (cw <= 0.01) return null;
            return [(cx / cw * 0.5 + 0.5) * cssWidth, (1 - (cy / cw * 0.5 + 0.5)) * cssHeight, cw];
        },
        destroy() {
            if (destroyed) return; destroyed = true;
            gl.useProgram(null);
            canvas.removeEventListener('webglcontextlost', lostHandler); canvas.removeEventListener('webglcontextrestored', restoredHandler);
            for (const k of [...models.keys()]) this.dropModel(k);
            for (const k of [...animations.keys()]) this.dropAnimation(k);
            if (terrainBuffer) deleteBuffer(terrainBuffer);
            deleteBuffer(markerBuffer); deleteBuffer(shadowBuffer); if (atlasTexture) gl.deleteTexture(atlasTexture); gl.deleteProgram(program);
            gl.deleteTexture(speckTexture);gl.deleteBuffer(speckBuffer);gl.deleteProgram(speckProgram);
            if(gl2&&speckLayout)gl.deleteVertexArray(speckLayout);
            terrain = null; markers = new Float32Array(0); markerCount = 0; atlas = null;
        }
    };
}

// modelMatrix places a model at world (x, y, z) tiles, rotated by a client yaw (0..2047;
// 0 faces south), the rotation Model.worldRender applies: x' = z sin + x cos, z' = z cos - x sin.
export function modelMatrix(x, y, z, yaw, out = new Float32Array(16)) {
    const t = (yaw || 0) * Math.PI / 1024, s = Math.sin(t), c = Math.cos(t);
    out[0] = c; out[1] = 0; out[2] = -s; out[3] = 0;
    out[4] = 0; out[5] = 1; out[6] = 0; out[7] = 0;
    out[8] = s; out[9] = 0; out[10] = c; out[11] = 0;
    out[12] = x; out[13] = y; out[14] = z; out[15] = 1;
    return out;
}

// cameraMatrix = perspective * lookAt * flipZ, column-major for uniformMatrix4fv.
export function cameraMatrix(c, aspect) {
    const fov = 0.8, near = 0.5, far = 400, f = 1 / Math.tan(fov / 2);
    const P = [f / aspect, 0, 0, 0, 0, f, 0, 0, 0, 0, (far + near) / (near - far), -1, 0, 0, 2 * far * near / (near - far), 0];
    const tx = c.x, ty = c.y, tz = -c.z; // target in GL space (z flipped)
    const cp = Math.cos(c.pitch);
    const ex = tx + c.distance * Math.sin(c.yaw) * cp, ey = ty + c.distance * Math.sin(c.pitch), ez = tz + c.distance * Math.cos(c.yaw) * cp;
    let fx = tx - ex, fy = ty - ey, fz = tz - ez; const fl = Math.hypot(fx, fy, fz); fx /= fl; fy /= fl; fz /= fl;
    let sx = -fz, sy = 0, sz = fx; const sl = Math.hypot(sx, sy, sz); sx /= sl; sy /= sl; sz /= sl;
    const ux = sy * fz - sz * fy, uy = sz * fx - sx * fz, uz = sx * fy - sy * fx;
    const V = [sx, ux, -fx, 0, sy, uy, -fy, 0, sz, uz, -fz, 0,
        -(sx * ex + sy * ey + sz * ez), -(ux * ex + uy * ey + uz * ez), fx * ex + fy * ey + fz * ez, 1];
    const PV = multiply(P, V);
    // Fold the z flip into the matrix: column 2 negated.
    PV[8] = -PV[8]; PV[9] = -PV[9]; PV[10] = -PV[10]; PV[11] = -PV[11];
    return new Float32Array(PV);
}

function multiply(a, b) {
    const out = new Array(16);
    for (let col = 0; col < 4; col++) for (let row = 0; row < 4; row++) {
        let s = 0;
        for (let k = 0; k < 4; k++) s += a[k * 4 + row] * b[col * 4 + k];
        out[col * 4 + row] = s;
    }
    return out;
}

// MarkerMesh builds fallback marker pillars into one reused Float32Array.
export class MarkerMesh {
    constructor(maxMarkers) { this.data = new Float32Array(maxMarkers * 36 * FLOATS); this.count = 0; this.max = maxMarkers; }
    reset() { this.count = 0; }
    push(x, y, z, r, g, b) {
        const i = this.count * FLOATS, d = this.data;
        d[i] = x; d[i + 1] = y; d[i + 2] = z; d[i + 3] = r; d[i + 4] = g; d[i + 5] = b; d[i + 6] = 1; d[i + 7] = 0; d[i + 8] = 0; d[i + 9] = -1;
        this.count++;
    }
    // marker: a pillar (top + four shaded sides) on a flat tile square, 12 triangles.
    marker(cx, cz, ground, kind, size = 1) {
        if (this.count + 36 > this.max * 36) return false;
        const { colour: [r, g, b], height } = kind, half = kind.half * size, top = ground + height;
        const x0 = cx - half, x1 = cx + half, z0 = cz - half, z1 = cz + half;
        const quad = (ax, ay, az, bx, by, bz, qx, qy, qz, dx, dy, dz, s) => {
            this.push(ax, ay, az, r * s, g * s, b * s); this.push(bx, by, bz, r * s, g * s, b * s); this.push(qx, qy, qz, r * s, g * s, b * s);
            this.push(ax, ay, az, r * s, g * s, b * s); this.push(qx, qy, qz, r * s, g * s, b * s); this.push(dx, dy, dz, r * s, g * s, b * s);
        };
        quad(x0, top, z0, x1, top, z0, x1, top, z1, x0, top, z1, 1);
        quad(x0, ground, z0, x1, ground, z0, x1, top, z0, x0, top, z0, 0.75);
        quad(x1, ground, z0, x1, ground, z1, x1, top, z1, x1, top, z0, 0.6);
        quad(x1, ground, z1, x0, ground, z1, x0, top, z1, x1, top, z1, 0.75);
        quad(x0, ground, z1, x0, ground, z0, x0, top, z0, x0, top, z1, 0.6);
        // A darker filled square just above the ground marks the observed tile itself.
        const t = size / 2, lift = ground + 0.03;
        quad(cx - t, lift, cz - t, cx + t, lift, cz - t, cx + t, lift, cz + t, cx - t, lift, cz + t, 0.35);
        return true;
    }
}

// widenTerrain converts the 7-float terrain export (x y z r g b a) to this renderer's
// 10-float format, offset into absolute tiles. A newer adapter that already emits 10 floats
// (textured terrain) passes through with the offset applied.
export function widenTerrain(vertices, floats, offX, offZ) {
    const n = vertices.length / floats, out = new Float32Array(n * FLOATS);
    for (let i = 0; i < n; i++) {
        const s = i * floats, d = i * FLOATS;
        out[d] = vertices[s] + offX; out[d + 1] = vertices[s + 1]; out[d + 2] = vertices[s + 2] + offZ;
        out[d + 3] = vertices[s + 3]; out[d + 4] = vertices[s + 4]; out[d + 5] = vertices[s + 5]; out[d + 6] = vertices[s + 6];
        if (floats >= 10) { out[d + 7] = vertices[s + 7]; out[d + 8] = vertices[s + 8]; out[d + 9] = vertices[s + 9]; } else out[d + 9] = -1;
    }
    return out;
}
