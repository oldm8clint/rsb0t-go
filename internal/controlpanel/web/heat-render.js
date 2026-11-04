// Draws a heat layer: a set of weighted points, each spread as a soft round kernel, summed,
// then coloured through one ramp. Two implementations behind one interface:
//
//   WebGL: pass 1 draws every point as a point sprite with additive blending into an
//   offscreen density texture (half-float or float where the GPU can render to it, 8-bit
//   otherwise); pass 2 draws one full-screen quad that maps density through the colour ramp.
//   Canvas: the classic 2D technique — stamp a blurred alpha disc per point, then recolour
//   every pixel's accumulated alpha through the same ramp.
//
// createHeatRenderer(canvas, { prefer: 'gpu' | 'canvas' }) -> {
//   kind            'webgl-float' | 'webgl-half' | 'webgl-8bit' | 'canvas'
//   draw(points, radius, intensity)
//       points: Float32Array of x, y, weight triples in CSS pixels, weight in 0..1
//       radius: kernel radius in CSS pixels; intensity: a multiplier (1 = default)
//   resize(width, height, dpr)
//   destroy()
//   onchange        set to a callback fired when a lost GL context falls back or recovers
// }

// A single-hue ramp (orange, dark and transparent to light and opaque): heat is a magnitude,
// so it gets one hue that gets lighter, never a rainbow. Stops are [t, r, g, b, alpha].
const RAMP_STOPS = [
    [0.00, 110, 35, 0, 0.00],
    [0.12, 160, 52, 6, 0.38],
    [0.35, 214, 92, 18, 0.66],
    [0.65, 248, 156, 48, 0.86],
    [1.00, 255, 238, 186, 0.96]
];

/** 256 RGBA bytes, straight (not premultiplied) alpha. */
export const RAMP = (() => {
    const out = new Uint8Array(256 * 4);
    for (let i = 0; i < 256; i++) {
        const t = i / 255;
        let j = 0;
        while (j < RAMP_STOPS.length - 2 && t > RAMP_STOPS[j + 1][0]) j++;
        const a = RAMP_STOPS[j], b = RAMP_STOPS[j + 1];
        const f = Math.min(1, Math.max(0, (t - a[0]) / (b[0] - a[0])));
        for (let c = 0; c < 4; c++) {
            const v = a[c + 1] + (b[c + 1] - a[c + 1]) * f;
            out[i * 4 + c] = c === 3 ? Math.round(v * 255) : Math.round(v);
        }
    }
    return out;
})();

/** The ramp as a CSS linear-gradient, for the legend. */
export function rampGradient() {
    const parts = RAMP_STOPS.map(([t, r, g, b, a]) => `rgba(${r},${g},${b},${a}) ${Math.round(t * 100)}%`);
    return `linear-gradient(to right, ${parts.join(', ')})`;
}

// Density d (summed kernel weights times intensity) to a ramp position: soft saturation, so
// one busy tile does not flatten everything else and overlapping kernels never clip hard.
const SATURATE = 1.6;

export function createHeatRenderer(canvas, { prefer = 'gpu' } = {}) {
    let impl = prefer === 'canvas' ? null : glRenderer(canvas);
    let fallbackCanvas = null;
    const api = {
        kind: 'canvas',
        onchange: null,
        draw(points, radius, intensity) { current().draw(points, radius, intensity); },
        resize(w, h, dpr) {
            impl?.resize(w, h, dpr);
            if (!impl || impl.lost) current();
            fallbackCanvas?.resize(w, h, dpr);
        },
        destroy() { impl?.destroy(); fallbackCanvas?.destroy(); }
    };
    function current() {
        if (impl && !impl.lost) return impl;
        if (!fallbackCanvas) fallbackCanvas = canvasRenderer(impl ? impl.shadow() : canvas);
        return fallbackCanvas;
    }
    if (impl) {
        api.kind = impl.kind;
        impl.onlost = () => { api.kind = 'canvas (GPU context lost)'; api.onchange?.(); };
        impl.onrestored = () => { api.kind = impl.kind; api.onchange?.(); };
    }
    return api;
}

function glRenderer(canvas) {
    const gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: true, antialias: false, depth: false, stencil: false, powerPreference: 'low-power', failIfMajorPerformanceCaveat: true });
    if (!gl) return null;
    let res = null;
    let width = 1, height = 1, dpr = 1;
    let last = null; // [points, radius, intensity] for a redraw after restore
    // A 2D canvas stacked over the GL one, used only while the GL context is lost.
    let shadowCanvas = null;
    const self = {
        kind: '',
        lost: false,
        onlost: null,
        onrestored: null,
        draw,
        resize(w, h, r) {
            width = w; height = h; dpr = r;
            canvas.width = Math.max(1, Math.round(w * r));
            canvas.height = Math.max(1, Math.round(h * r));
            canvas.style.width = w + 'px'; canvas.style.height = h + 'px';
            if (shadowCanvas) { shadowCanvas.width = canvas.width; shadowCanvas.height = canvas.height; shadowCanvas.style.width = w + 'px'; shadowCanvas.style.height = h + 'px'; }
            if (res) sizeTarget();
        },
        shadow() {
            if (!shadowCanvas) {
                shadowCanvas = document.createElement('canvas');
                shadowCanvas.className = canvas.className;
                shadowCanvas.style.cssText = canvas.style.cssText;
                shadowCanvas.width = canvas.width; shadowCanvas.height = canvas.height;
                canvas.after(shadowCanvas);
            }
            return shadowCanvas;
        },
        destroy() {
            canvas.removeEventListener('webglcontextlost', onLost);
            canvas.removeEventListener('webglcontextrestored', onRestored);
            shadowCanvas?.remove();
            if (res && !self.lost) {
                for (const p of [res.splat, res.colour]) gl.deleteProgram(p);
                for (const b of [res.points, res.quad]) gl.deleteBuffer(b);
                for (const t of [res.density, res.ramp]) gl.deleteTexture(t);
                gl.deleteFramebuffer(res.fbo);
            }
            gl.getExtension('WEBGL_lose_context')?.loseContext();
        }
    };
    function onLost(e) { e.preventDefault(); self.lost = true; res = null; self.onlost?.(); }
    function onRestored() {
        try { res = setup(); } catch { return; }
        self.lost = false;
        if (shadowCanvas) shadowCanvas.getContext('2d').clearRect(0, 0, shadowCanvas.width, shadowCanvas.height);
        self.onrestored?.();
        if (last) draw(...last);
    }
    canvas.addEventListener('webglcontextlost', onLost);
    canvas.addEventListener('webglcontextrestored', onRestored);

    function program(vs, fs) {
        const p = gl.createProgram();
        for (const [kind, src] of [[gl.VERTEX_SHADER, vs], [gl.FRAGMENT_SHADER, fs]]) {
            const s = gl.createShader(kind);
            gl.shaderSource(s, src); gl.compileShader(s);
            if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s));
            gl.attachShader(p, s); gl.deleteShader(s);
        }
        gl.linkProgram(p);
        if (!gl.getProgramParameter(p, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(p));
        return p;
    }
    // Pick the best texture type this GPU can render into for the density pass.
    function pickTarget() {
        const half = gl.getExtension('OES_texture_half_float');
        gl.getExtension('EXT_color_buffer_half_float');
        const full = gl.getExtension('OES_texture_float');
        gl.getExtension('WEBGL_color_buffer_float');
        const candidates = [];
        if (half) candidates.push(['webgl-half', half.HALF_FLOAT_OES, 1]);
        if (full) candidates.push(['webgl-float', gl.FLOAT, 1]);
        candidates.push(['webgl-8bit', gl.UNSIGNED_BYTE, 0.25]);
        for (const [kind, type, gain] of candidates) {
            const tex = gl.createTexture();
            gl.bindTexture(gl.TEXTURE_2D, tex);
            gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 4, 4, 0, gl.RGBA, type, null);
            const fbo = gl.createFramebuffer();
            gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
            gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
            const ok = gl.checkFramebufferStatus(gl.FRAMEBUFFER) === gl.FRAMEBUFFER_COMPLETE;
            gl.bindFramebuffer(gl.FRAMEBUFFER, null);
            gl.deleteFramebuffer(fbo); gl.deleteTexture(tex);
            gl.getError();
            if (ok) return { kind, type, gain };
        }
        throw new Error('no renderable texture type');
    }
    function setup() {
        const target = pickTarget();
        const splat = program(
            'attribute vec3 p;uniform vec2 res;uniform float size;uniform float gain;varying float w;' +
            'void main(){vec2 q=p.xy/res;gl_Position=vec4(q.x*2.-1.,1.-q.y*2.,0.,1.);gl_PointSize=size;w=p.z*gain;}',
            'precision mediump float;varying float w;' +
            'void main(){vec2 d=gl_PointCoord*2.-1.;float r2=dot(d,d);if(r2>1.)discard;' +
            'float k=(exp(-r2*3.)-0.049787)/0.950213;gl_FragColor=vec4(w*k,0.,0.,1.);}');
        const colour = program(
            'attribute vec2 c;varying vec2 uv;void main(){uv=c*.5+.5;gl_Position=vec4(c,0.,1.);}',
            'precision mediump float;varying vec2 uv;uniform sampler2D dens;uniform sampler2D ramp;uniform float scale;' +
            'void main(){float d=texture2D(dens,uv).r*scale;float t=1.-exp(-' + SATURATE.toFixed(2) + '*d);' +
            'if(t<.004)discard;vec4 c=texture2D(ramp,vec2(t,.5));gl_FragColor=vec4(c.rgb*c.a,c.a);}');
        const quad = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, quad);
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, -1, 1, 1, -1, 1, 1]), gl.STATIC_DRAW);
        const ramp = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, ramp);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 256, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, RAMP);
        for (const [k, v] of [[gl.TEXTURE_MIN_FILTER, gl.LINEAR], [gl.TEXTURE_MAG_FILTER, gl.LINEAR], [gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE], [gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE]]) gl.texParameteri(gl.TEXTURE_2D, k, v);
        const r = { target, splat, colour, quad, ramp, points: gl.createBuffer(), density: gl.createTexture(), fbo: gl.createFramebuffer(), maxPoint: gl.getParameter(gl.ALIASED_POINT_SIZE_RANGE)[1] };
        self.kind = target.kind;
        res = r;
        sizeTarget();
        return r;
    }
    function sizeTarget() {
        gl.bindTexture(gl.TEXTURE_2D, res.density);
        // NEAREST: float textures are not linearly filterable everywhere, and pass 2 samples
        // the density texture 1:1 with the canvas anyway.
        for (const [k, v] of [[gl.TEXTURE_MIN_FILTER, gl.NEAREST], [gl.TEXTURE_MAG_FILTER, gl.NEAREST], [gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE], [gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE]]) gl.texParameteri(gl.TEXTURE_2D, k, v);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, canvas.width, canvas.height, 0, gl.RGBA, res.target.type, null);
        gl.bindFramebuffer(gl.FRAMEBUFFER, res.fbo);
        gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, res.density, 0);
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    }
    function draw(points, radius, intensity) {
        last = [points, radius, intensity];
        if (self.lost || !res) return;
        const n = points.length / 3;
        // pass 1: density
        gl.bindFramebuffer(gl.FRAMEBUFFER, res.fbo);
        gl.viewport(0, 0, canvas.width, canvas.height);
        gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);
        if (n > 0) {
            gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE);
            gl.useProgram(res.splat);
            gl.bindBuffer(gl.ARRAY_BUFFER, res.points);
            gl.bufferData(gl.ARRAY_BUFFER, points, gl.DYNAMIC_DRAW);
            const loc = gl.getAttribLocation(res.splat, 'p');
            gl.enableVertexAttribArray(loc);
            gl.vertexAttribPointer(loc, 3, gl.FLOAT, false, 12, 0);
            gl.uniform2f(gl.getUniformLocation(res.splat, 'res'), width, height);
            gl.uniform1f(gl.getUniformLocation(res.splat, 'size'), Math.min(res.maxPoint, Math.max(1, radius * 2 * dpr)));
            gl.uniform1f(gl.getUniformLocation(res.splat, 'gain'), res.target.gain);
            gl.drawArrays(gl.POINTS, 0, n);
            gl.disableVertexAttribArray(loc);
        }
        // pass 2: colour
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
        gl.viewport(0, 0, canvas.width, canvas.height);
        gl.clear(gl.COLOR_BUFFER_BIT);
        if (n === 0) return;
        gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
        gl.useProgram(res.colour);
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, res.density);
        gl.activeTexture(gl.TEXTURE1); gl.bindTexture(gl.TEXTURE_2D, res.ramp);
        gl.uniform1i(gl.getUniformLocation(res.colour, 'dens'), 0);
        gl.uniform1i(gl.getUniformLocation(res.colour, 'ramp'), 1);
        gl.uniform1f(gl.getUniformLocation(res.colour, 'scale'), intensity / res.target.gain);
        gl.bindBuffer(gl.ARRAY_BUFFER, res.quad);
        const c = gl.getAttribLocation(res.colour, 'c');
        gl.enableVertexAttribArray(c);
        gl.vertexAttribPointer(c, 2, gl.FLOAT, false, 8, 0);
        gl.drawArrays(gl.TRIANGLES, 0, 6);
        gl.disableVertexAttribArray(c);
        gl.activeTexture(gl.TEXTURE0);
        canvas.dataset.points = String(n);
        canvas.dataset.glError = String(gl.getError());
    }
    try { setup(); } catch { gl.getExtension('WEBGL_lose_context')?.loseContext(); return null; }
    return self;
}

function canvasRenderer(canvas) {
    const ctx = canvas.getContext('2d');
    let width = 1, height = 1, dpr = 1;
    let stamp = null, stampRadius = -1;
    function makeStamp(r) {
        const size = Math.max(2, Math.ceil(r * 2));
        const s = document.createElement('canvas');
        s.width = s.height = size;
        const g = s.getContext('2d');
        const grad = g.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
        grad.addColorStop(0, 'rgba(0,0,0,1)');
        grad.addColorStop(0.45, 'rgba(0,0,0,0.5)');
        grad.addColorStop(1, 'rgba(0,0,0,0)');
        g.fillStyle = grad;
        g.fillRect(0, 0, size, size);
        return s;
    }
    return {
        kind: 'canvas',
        resize(w, h, r) {
            width = w; height = h; dpr = r;
            canvas.width = Math.max(1, Math.round(w * r));
            canvas.height = Math.max(1, Math.round(h * r));
            canvas.style.width = w + 'px'; canvas.style.height = h + 'px';
        },
        draw(points, radius, intensity) {
            ctx.setTransform(1, 0, 0, 1, 0, 0);
            ctx.clearRect(0, 0, canvas.width, canvas.height);
            const n = points.length / 3;
            if (!n) return;
            const r = Math.max(1, radius * dpr);
            if (r !== stampRadius) { stamp = makeStamp(r); stampRadius = r; }
            for (let i = 0; i < points.length; i += 3) {
                ctx.globalAlpha = Math.min(1, 1 - Math.exp(-SATURATE * points[i + 2] * intensity * 0.6));
                ctx.drawImage(stamp, points[i] * dpr - r, points[i + 1] * dpr - r, r * 2, r * 2);
            }
            ctx.globalAlpha = 1;
            const img = ctx.getImageData(0, 0, canvas.width, canvas.height);
            const px = img.data;
            for (let i = 3; i < px.length; i += 4) {
                const a = px[i];
                if (!a) continue;
                const j = a * 4;
                px[i - 3] = RAMP[j]; px[i - 2] = RAMP[j + 1]; px[i - 1] = RAMP[j + 2]; px[i] = RAMP[j + 3];
            }
            ctx.putImageData(img, 0, 0);
            canvas.dataset.points = String(n);
        },
        destroy() {}
    };
}
