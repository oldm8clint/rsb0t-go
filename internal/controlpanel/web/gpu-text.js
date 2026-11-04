// A small 2D drawing surface for dashboard panels: filled rectangles, dots and text runs.
//
// Two backends behind one API, chosen at creation:
//   'webgl'  — every rect, dot and glyph is a textured quad in ONE vertex buffer, drawn with
//              one drawArrays(TRIANGLES) per frame. Glyphs come from an atlas texture the
//              browser's own text rasteriser fills once per (font, character), so the page's
//              real fonts (the cache-derived 'Lost City' TTFs) are what is drawn.
//   'canvas' — the same calls replayed onto a CanvasRenderingContext2D, for a browser with
//              WebGL disabled or unavailable. Same layout, same pixels to within anti-aliasing.
//
// Coordinates are CSS pixels; the surface scales by devicePixelRatio itself and snaps glyphs
// to whole device pixels so text stays as crisp as the DOM's. No animation loop: the owner of
// the surface calls begin()/.../end() when something changed.

const ATLAS = 1024;

function parseColor(css) {
    // getComputedStyle always answers rgb()/rgba(); anything else is treated as opaque white.
    const m = /rgba?\(([^)]+)\)/.exec(css || '');
    if (!m) return [1, 1, 1, 1];
    const p = m[1].split(/[\s,/]+/).filter(Boolean).map(Number);
    return [p[0] / 255, p[1] / 255, p[2] / 255, p.length > 3 ? p[3] : 1];
}

export { parseColor };

function webglBackend(canvas) {
    // failIfMajorPerformanceCaveat: a browser without GPU acceleration hands out WebGL on a
    // software rasteriser (SwiftShader), which measured SLOWER than Canvas 2D for this job
    // (docs/control_panel_gpu.md). Refusing it makes such a browser take the Canvas path.
    const gl = canvas.getContext('webgl', { alpha: true, antialias: false, premultipliedAlpha: true, preserveDrawingBuffer: false, failIfMajorPerformanceCaveat: true });
    if (!gl) return null;
    const atlasCanvas = document.createElement('canvas');
    atlasCanvas.width = atlasCanvas.height = ATLAS;
    const actx = atlasCanvas.getContext('2d', { willReadFrequently: false });
    let texture = null, program = null, buffer = null, loc = null, dirtyAtlas = true, lost = false;
    let glyphs = new Map(), penX = 0, penY = 0, rowH = 0, dpr = 1;
    const WHITE = { u0: 1 / ATLAS, v0: 1 / ATLAS, u1: 3 / ATLAS, v1: 3 / ATLAS };
    let DOT = null;

    function resetAtlas() {
        actx.clearRect(0, 0, ATLAS, ATLAS);
        glyphs = new Map();
        actx.fillStyle = '#fff';
        actx.fillRect(0, 0, 4, 4); // solid texels for rectangles
        // An anti-aliased disc for dots, 32 device px, sampled linearly when scaled.
        actx.beginPath(); actx.arc(4 + 2 + 16, 2 + 16, 15.5, 0, Math.PI * 2); actx.fill();
        DOT = { u0: 6 / ATLAS, v0: 2 / ATLAS, u1: 38 / ATLAS, v1: 34 / ATLAS };
        penX = 40; penY = 0; rowH = 36;
        dirtyAtlas = true;
    }

    function setup() {
        const vs = 'attribute vec2 p;attribute vec2 t;attribute vec4 c;uniform vec2 r;varying vec2 vt;varying vec4 vc;void main(){gl_Position=vec4(p.x/r.x*2.-1.,1.-p.y/r.y*2.,0.,1.);vt=t;vc=c;}';
        const fs = 'precision mediump float;uniform sampler2D s;varying vec2 vt;varying vec4 vc;void main(){float a=texture2D(s,vt).a*vc.a;gl_FragColor=vec4(vc.rgb*a,a);}';
        program = gl.createProgram();
        for (const [kind, src] of [[gl.VERTEX_SHADER, vs], [gl.FRAGMENT_SHADER, fs]]) {
            const sh = gl.createShader(kind); gl.shaderSource(sh, src); gl.compileShader(sh);
            if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) throw Error(gl.getShaderInfoLog(sh));
            gl.attachShader(program, sh); gl.deleteShader(sh);
        }
        gl.linkProgram(program);
        if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw Error(gl.getProgramInfoLog(program));
        loc = { p: gl.getAttribLocation(program, 'p'), t: gl.getAttribLocation(program, 't'), c: gl.getAttribLocation(program, 'c'), r: gl.getUniformLocation(program, 'r') };
        buffer = gl.createBuffer();
        texture = gl.createTexture();
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        dirtyAtlas = true;
    }
    try { setup(); } catch (_) { return null; }
    resetAtlas();

    // One glyph at the current device scale. Advance is the browser's own measurement.
    function glyph(font, ch) {
        const key = font + '\u0000' + ch;
        let g = glyphs.get(key);
        if (g) return g;
        actx.font = font; actx.textBaseline = 'alphabetic';
        const m = actx.measureText(ch);
        const ascent = Math.ceil(m.fontBoundingBoxAscent ?? m.actualBoundingBoxAscent ?? 12);
        const descent = Math.ceil(m.fontBoundingBoxDescent ?? m.actualBoundingBoxDescent ?? 4);
        const left = Math.max(0, Math.ceil(m.actualBoundingBoxLeft || 0));
        const w = Math.ceil(Math.max(m.width, (m.actualBoundingBoxRight || 0) + left)) + left + 2;
        const h = ascent + descent + 2;
        if (penX + w > ATLAS) { penX = 0; penY += rowH; rowH = 0; }
        if (penY + h > ATLAS) { resetAtlas(); return glyph(font, ch); } // full: start over, rare
        actx.fillStyle = '#fff';
        actx.fillText(ch, penX + left + 1, penY + ascent + 1);
        g = { u0: penX / ATLAS, v0: penY / ATLAS, u1: (penX + w) / ATLAS, v1: (penY + h) / ATLAS, w, h, ox: -(left + 1), oy: -(ascent + 1), advance: m.width };
        glyphs.set(key, g);
        penX += w + 1; rowH = Math.max(rowH, h + 1);
        dirtyAtlas = true;
        return g;
    }

    // One reused, growable vertex array: a scroll step writes a few thousand quads, and a
    // fresh JS array per frame measured as most of the frame's script time plus GC.
    let verts = new Float32Array(48 * 4096), n = 0;
    function vertex(x, y, u, v, c) { verts[n++] = x; verts[n++] = y; verts[n++] = u; verts[n++] = v; verts[n++] = c[0]; verts[n++] = c[1]; verts[n++] = c[2]; verts[n++] = c[3]; }
    function quad(x0, y0, x1, y1, uv, c) {
        if (n + 48 > verts.length) { const grown = new Float32Array(verts.length * 2); grown.set(verts); verts = grown; }
        vertex(x0, y0, uv.u0, uv.v0, c); vertex(x1, y0, uv.u1, uv.v0, c); vertex(x0, y1, uv.u0, uv.v1, c);
        vertex(x0, y1, uv.u0, uv.v1, c); vertex(x1, y0, uv.u1, uv.v0, c); vertex(x1, y1, uv.u1, uv.v1, c);
    }

    canvas.addEventListener('webglcontextlost', e => { e.preventDefault(); lost = true; });
    canvas.addEventListener('webglcontextrestored', () => { lost = false; try { setup(); resetAtlas(); } catch (_) { lost = true; } });

    return {
        kind: 'webgl',
        get lost() { return lost; },
        setScale(scale) { if (scale !== dpr) { dpr = scale; resetAtlas(); } },
        begin() { n = 0; },
        rect(x, y, w, h, c) { quad(Math.round(x * dpr), Math.round(y * dpr), Math.round((x + w) * dpr), Math.round((y + h) * dpr), WHITE, c); },
        dot(cx, cy, r, c) { quad((cx - r) * dpr, (cy - r) * dpr, (cx + r) * dpr, (cy + r) * dpr, DOT, c); },
        advance(font, ch) { return glyph(font, ch).advance / dpr; },
        glyphRun(font, text, x, baseline, c) {
            let pen = Math.round(x * dpr); const by = Math.round(baseline * dpr);
            for (const ch of text) {
                const g = glyph(font, ch);
                if (ch !== ' ') quad(Math.round(pen + g.ox), by + g.oy, Math.round(pen + g.ox) + g.w, by + g.oy + g.h, g, c);
                pen += g.advance;
            }
        },
        end(width, height) {
            if (lost) return 0;
            gl.viewport(0, 0, width, height);
            gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);
            if (!n) return 0;
            gl.useProgram(program);
            gl.bindTexture(gl.TEXTURE_2D, texture);
            if (dirtyAtlas) { gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false); gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, atlasCanvas); dirtyAtlas = false; }
            gl.enable(gl.BLEND); gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
            gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
            gl.bufferData(gl.ARRAY_BUFFER, verts.subarray(0, n), gl.DYNAMIC_DRAW);
            gl.uniform2f(loc.r, width, height);
            gl.enableVertexAttribArray(loc.p); gl.vertexAttribPointer(loc.p, 2, gl.FLOAT, false, 32, 0);
            gl.enableVertexAttribArray(loc.t); gl.vertexAttribPointer(loc.t, 2, gl.FLOAT, false, 32, 8);
            gl.enableVertexAttribArray(loc.c); gl.vertexAttribPointer(loc.c, 4, gl.FLOAT, false, 32, 16);
            const count = n / 8;
            gl.drawArrays(gl.TRIANGLES, 0, count);
            return count / 3;
        },
        destroy() {
            if (!lost) { gl.deleteBuffer(buffer); gl.deleteTexture(texture); gl.deleteProgram(program); }
            const ext = gl.getExtension('WEBGL_lose_context'); if (ext) ext.loseContext();
        }
    };
}

function canvasBackend(canvas) {
    const ctx = canvas.getContext('2d');
    if (!ctx) return null;
    let dpr = 1; const widths = new Map();
    const css = c => `rgba(${Math.round(c[0] * 255)},${Math.round(c[1] * 255)},${Math.round(c[2] * 255)},${c[3]})`;
    return {
        kind: 'canvas',
        lost: false,
        setScale(scale) { if (scale !== dpr) { dpr = scale; widths.clear(); } },
        begin() { ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.clearRect(0, 0, canvas.width, canvas.height); },
        rect(x, y, w, h, c) { ctx.fillStyle = css(c); ctx.fillRect(Math.round(x * dpr), Math.round(y * dpr), Math.round((x + w) * dpr) - Math.round(x * dpr), Math.round((y + h) * dpr) - Math.round(y * dpr)); },
        dot(cx, cy, r, c) { ctx.fillStyle = css(c); ctx.beginPath(); ctx.arc(cx * dpr, cy * dpr, r * dpr, 0, Math.PI * 2); ctx.fill(); },
        advance(font, ch) {
            const key = font + '\u0000' + ch;
            let w = widths.get(key);
            if (w === undefined) { ctx.font = font; w = ctx.measureText(ch).width; widths.set(key, w); }
            return w / dpr;
        },
        glyphRun(font, text, x, baseline, c) {
            ctx.font = font; ctx.fillStyle = css(c); ctx.textBaseline = 'alphabetic';
            ctx.fillText(text, Math.round(x * dpr), Math.round(baseline * dpr));
        },
        end() { return 0; },
        destroy() { /* nothing held beyond the canvas */ }
    };
}

/** createSurface(canvas, prefer) -> surface. prefer 'webgl' falls back to 'canvas'. */
export function createSurface(canvas, prefer = 'webgl') {
    const backend = (prefer === 'webgl' && webglBackend(canvas)) || canvasBackend(canvas);
    let dpr = 1;
    const fontCache = new Map();
    // Whole-string widths, so a table re-measuring the same cell text every second or on
    // every scroll step pays one Map lookup instead of a per-character walk. Bounded.
    let widthCache = new Map();
    function width(text, df) {
        const key = df + '\u0000' + text;
        let w = widthCache.get(key);
        if (w === undefined) {
            w = 0; for (const ch of text) w += backend.advance(df, ch);
            if (widthCache.size > 50000) widthCache = new Map();
            widthCache.set(key, w);
        }
        return w;
    }
    // Fonts are given as {size, weight, family} and turned into device-pixel CSS fonts once.
    function deviceFont(f) {
        const key = `${f.weight}|${f.size}|${f.family}|${dpr}`;
        let s = fontCache.get(key);
        if (!s) { s = `${f.weight} ${f.size * dpr}px ${f.family}`; fontCache.set(key, s); }
        return s;
    }
    return {
        get kind() { return backend.kind; },
        get lost() { return backend.lost; },
        resize(cssW, cssH, scale) {
            if (scale !== dpr) widthCache = new Map();
            dpr = scale; backend.setScale(scale);
            const w = Math.max(1, Math.round(cssW * scale)), h = Math.max(1, Math.round(cssH * scale));
            if (canvas.width !== w || canvas.height !== h) { canvas.width = w; canvas.height = h; }
            canvas.style.width = cssW + 'px'; canvas.style.height = cssH + 'px';
        },
        begin() { backend.begin(); },
        rect(x, y, w, h, c) { backend.rect(x, y, w, h, c); },
        dot(cx, cy, r, c) { backend.dot(cx, cy, r, c); },
        /** Width of text in CSS px. */
        measure(text, font) { return width(text, deviceFont(font)); },
        /** Draws text left-aligned at (x, baseline), cut with an ellipsis to maxWidth; returns the width drawn. */
        text(text, x, baseline, font, color, maxWidth = Infinity) {
            const df = deviceFont(font);
            let w = width(text, df);
            if (w > maxWidth) {
                const ell = backend.advance(df, '…');
                let cut = 0, acc = 0;
                for (const ch of text) { const a = backend.advance(df, ch); if (acc + a + ell > maxWidth) break; acc += a; cut += ch.length; }
                text = text.slice(0, cut) + '…'; w = acc + ell;
            }
            backend.glyphRun(df, text, x, baseline, color);
            return w;
        },
        end() { return backend.end(canvas.width, canvas.height); },
        destroy() { backend.destroy(); }
    };
}
