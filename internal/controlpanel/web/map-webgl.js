// Leaflet retains its projection, controls, tooltips and hit testing. Only path
// drawing is replaced: circles and wall segments share two GPU batches.
export function webglRenderer(L, options = {}) {
    const canvas = document.createElement('canvas');
    const gl = canvas.getContext('webgl', { alpha: true, antialias: true, depth: false, powerPreference: 'low-power', failIfMajorPerformanceCaveat: true });
    if (!gl) return null;
    let gpu;
    function program(vertex, fragment) {
        const result = gl.createProgram();
        for (const [kind, source] of [[gl.VERTEX_SHADER, vertex], [gl.FRAGMENT_SHADER, fragment]]) {
            const shader = gl.createShader(kind);
            gl.shaderSource(shader, source); gl.compileShader(shader);
            if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(shader));
            gl.attachShader(result, shader); gl.deleteShader(shader);
        }
        gl.linkProgram(result);
        if (!gl.getProgramParameter(result, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(result));
        return result;
    }
    function setup() {
        const position = 'uniform vec2 origin;uniform vec2 resolution;vec4 project(vec2 p){vec2 q=(p-origin)/resolution;return vec4(q.x*2.-1.,1.-q.y*2.,0.,1.);}';
        return {
            circles: program(position + 'attribute vec2 p;attribute float size;attribute vec4 fill;attribute vec4 stroke;attribute float border;uniform float dpr;varying vec4 f;varying vec4 s;varying float b;varying float aa;void main(){gl_Position=project(p);gl_PointSize=size*dpr;f=fill;s=stroke;b=border;aa=2./(size*dpr);}',
                'precision mediump float;varying vec4 f;varying vec4 s;varying float b;varying float aa;void main(){float r=length(gl_PointCoord-vec2(.5))*2.;if(r>1.)discard;vec4 c=r>1.-b?s:f;c.a*=1.-smoothstep(1.-aa,1.,r);gl_FragColor=c;}'),
            lines: program(position + 'attribute vec2 p;attribute vec4 color;varying vec4 tint;void main(){gl_Position=project(p);tint=color;}',
                'precision mediump float;varying vec4 tint;void main(){gl_FragColor=tint;}'),
            circleBuffer: gl.createBuffer(), lineBuffer: gl.createBuffer()
        };
    }
    try { gpu = setup(); } catch { return null; }
    const colors = new Map();
    function color(value, alpha) {
        if (!colors.has(value)) {
            let hex = String(value || '#ffffff').replace('#', '');
            if (hex.length === 3) hex = [...hex].map(c => c + c).join('');
            const n = /^[0-9a-f]{6}$/i.test(hex) ? parseInt(hex, 16) : 0xffffff;
            colors.set(value, [(n >> 16 & 255) / 255, (n >> 8 & 255) / 255, (n & 255) / 255]);
        }
        return [...colors.get(value), alpha];
    }
    function triangleLine(out, a, b, width, tint) {
        const dx = b.x - a.x, dy = b.y - a.y, length = Math.hypot(dx, dy);
        if (!length) return;
        const x = -dy / length * width / 2, y = dx / length * width / 2;
        for (const [px, py] of [[a.x+x,a.y+y],[a.x-x,a.y-y],[b.x+x,b.y+y],[b.x+x,b.y+y],[a.x-x,a.y-y],[b.x-x,b.y-y]]) out.push(px, py, ...tint);
    }
    const Renderer = L.Canvas.extend({
        _gpu: true,
        _initContainer() {
            this._container = canvas;
            canvas.classList.add('map-webgl-paths');
            L.DomEvent.on(canvas, 'mousemove', this._onMouseMove, this);
            L.DomEvent.on(canvas, 'click dblclick mousedown mouseup contextmenu', this._onClick, this);
            L.DomEvent.on(canvas, 'mouseout', this._handleMouseOut, this);
            canvas._leaflet_disable_events = true;
            this._visible = () => { if (!document.hidden) this._redraw(); };
            document.addEventListener('visibilitychange', this._visible);
            canvas.addEventListener('webglcontextlost', event => { event.preventDefault(); this._lost = true; this.fire('gpulost'); });
            canvas.addEventListener('webglcontextrestored', () => { gpu = setup(); this._lost = false; this._redraw(); this.fire('gpurestored'); });
        },
        _update() {
            if (this._map._animatingZoom && this._bounds) return;
            L.Renderer.prototype._update.call(this);
            const size = this._bounds.getSize();
            this._dpr = Math.min(devicePixelRatio || 1, 2);
            L.DomUtil.setPosition(canvas, this._bounds.min);
            canvas.width = Math.max(1, Math.round(size.x * this._dpr));
            canvas.height = Math.max(1, Math.round(size.y * this._dpr));
            canvas.style.width = size.x + 'px'; canvas.style.height = size.y + 'px';
            this.fire('update');
        },
        _updateCircle(layer) { this._requestRedraw(layer); },
        _updatePoly(layer) { this._requestRedraw(layer); },
        _redraw() { this._redrawRequest = null; this._draw(); this._redrawBounds = null; },
        _draw() {
            if (!this._bounds || this._lost || document.hidden || !canvas.width) return;
            const circles = [], lines = [];
            for (let order = this._drawFirst; order; order = order.next) {
                const layer = order.layer, style = layer.options;
                if (layer._point && layer._radius !== undefined) {
                    if (layer._empty()) continue;
                    const weight = style.stroke ? style.weight || 0 : 0;
                    const diameter = Math.max(2, layer._radius * 2 + weight);
                    circles.push(layer._point.x, layer._point.y, diameter,
                        ...color(style.fillColor || style.color, style.fill ? style.fillOpacity ?? 1 : 0),
                        ...color(style.color, style.stroke ? style.opacity ?? 1 : 0), weight * 2 / diameter);
                } else if (style.stroke) {
                    const tint = color(style.color, style.opacity ?? 1);
                    for (const part of layer._parts || []) for (let i = 1; i < part.length; i++) triangleLine(lines, part[i-1], part[i], style.weight || 1, tint);
                }
            }
            gl.viewport(0, 0, canvas.width, canvas.height);
            gl.clearColor(0,0,0,0); gl.clear(gl.COLOR_BUFFER_BIT);
            gl.enable(gl.BLEND); gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
            const size = this._bounds.getSize();
            function draw(program, buffer, values, attributes, stride, mode) {
                if (!values.length) return;
                gl.useProgram(program); gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
                gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(values), gl.DYNAMIC_DRAW);
                gl.uniform2f(gl.getUniformLocation(program,'origin'), this._bounds.min.x, this._bounds.min.y);
                gl.uniform2f(gl.getUniformLocation(program,'resolution'), size.x, size.y);
                gl.uniform1f(gl.getUniformLocation(program,'dpr'), this._dpr);
                const enabled = [];
                for (const [name, length, offset] of attributes) {
                    const location = gl.getAttribLocation(program,name);
                    gl.enableVertexAttribArray(location); gl.vertexAttribPointer(location,length,gl.FLOAT,false,stride*4,offset*4); enabled.push(location);
                }
                gl.drawArrays(mode,0,values.length/stride);
                for (const location of enabled) gl.disableVertexAttribArray(location);
            }
            draw.call(this,gpu.lines,gpu.lineBuffer,lines,[['p',2,0],['color',4,2]],6,gl.TRIANGLES);
            draw.call(this,gpu.circles,gpu.circleBuffer,circles,[['p',2,0],['size',1,2],['fill',4,3],['stroke',4,7],['border',1,11]],12,gl.POINTS);
            canvas.dataset.circles = String(circles.length / 12);
            canvas.dataset.wallVertices = String(lines.length / 6);
        },
        _destroyContainer() {
            L.Util.cancelAnimFrame(this._redrawRequest);
            document.removeEventListener('visibilitychange', this._visible);
            L.DomEvent.off(canvas); canvas.remove();
            for (const name of ['circles','lines']) gl.deleteProgram(gpu[name]);
            for (const name of ['circleBuffer','lineBuffer']) gl.deleteBuffer(gpu[name]);
            delete this._container;
        }
    });
    return new Renderer(options);
}
