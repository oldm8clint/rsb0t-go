// Genuine triangle rendering prototype, separate from CPU scene captures.
// Static opaque colour meshes only. No game session, picking or full scene yet.
export function createGpuModel(canvas) {
    const gl = canvas.getContext('webgl', { alpha: false, antialias: false });
    if (!gl) throw Error('WebGL is unavailable');
    let program, buffer, data = null, count = 0, center = [0, 0, 0], radius = 1;
    let destroyed = false, lost = false, yaw = 0.6, pitch = -0.25;
    function shader(type, source) {
        const result = gl.createShader(type); gl.shaderSource(result, source); gl.compileShader(result);
        if (!gl.getShaderParameter(result, gl.COMPILE_STATUS)) {
            const message = gl.getShaderInfoLog(result); gl.deleteShader(result); throw Error(message);
        }
        return result;
    }
    function initialize() {
        const vertex = shader(gl.VERTEX_SHADER, `
            attribute vec3 aPosition; attribute vec4 aColour;
            uniform vec3 uCenter; uniform float uRadius, uYaw, uPitch, uAspect;
            varying vec4 colour;
            void main() {
                vec3 p=(aPosition-uCenter)/uRadius;
                p=vec3(p.x*cos(uYaw)+p.z*sin(uYaw),p.y,-p.x*sin(uYaw)+p.z*cos(uYaw));
                p=vec3(p.x,p.y*cos(uPitch)-p.z*sin(uPitch),p.y*sin(uPitch)+p.z*cos(uPitch));
                p.z+=3.6;
                gl_Position=vec4(p.x*2.4/uAspect,p.y*2.4,1.002002*p.z-0.2002002,p.z);
                colour=aColour;
            }`);
        const fragment = shader(gl.FRAGMENT_SHADER, `precision mediump float; varying vec4 colour; void main(){gl_FragColor=colour;}`);
        program = gl.createProgram(); gl.attachShader(program, vertex); gl.attachShader(program, fragment); gl.linkProgram(program);
        gl.deleteShader(vertex); gl.deleteShader(fragment);
        if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw Error(gl.getProgramInfoLog(program));
        buffer = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
        if (data) gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
    }
    function draw(nextYaw = yaw, nextPitch = pitch) {
        yaw = nextYaw; pitch = nextPitch;
        if (destroyed || lost) return;
        const scale = Math.min(devicePixelRatio || 1, 2);
        const width = Math.max(1, Math.round(canvas.clientWidth * scale));
        const height = Math.max(1, Math.round(canvas.clientHeight * scale));
        if (canvas.width !== width || canvas.height !== height) { canvas.width = width; canvas.height = height; }
        gl.viewport(0, 0, width, height); gl.clearColor(0.07, 0.06, 0.04, 1);
        gl.enable(gl.DEPTH_TEST); gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
        if (!data || !count) return;
        gl.useProgram(program); gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
        const position = gl.getAttribLocation(program, 'aPosition'), colour = gl.getAttribLocation(program, 'aColour');
        gl.enableVertexAttribArray(position); gl.vertexAttribPointer(position, 3, gl.FLOAT, false, 28, 0);
        gl.enableVertexAttribArray(colour); gl.vertexAttribPointer(colour, 4, gl.FLOAT, false, 28, 12);
        gl.uniform3fv(gl.getUniformLocation(program, 'uCenter'), center);
        for (const [key, value] of [['uRadius', radius], ['uYaw', yaw], ['uPitch', pitch], ['uAspect', width / height]]) gl.uniform1f(gl.getUniformLocation(program, key), value);
        gl.drawArrays(gl.TRIANGLES, 0, count);
    }
    function contextLost(event) { event.preventDefault(); lost = true; }
    function contextRestored() { if (destroyed) return; lost = false; initialize(); draw(); }
    canvas.addEventListener('webglcontextlost', contextLost);
    canvas.addEventListener('webglcontextrestored', contextRestored);
    initialize();
    return {
        setMesh(mesh) {
            if (destroyed) throw Error('Renderer is closed');
            if (!Array.isArray(mesh.vertices) || mesh.vertices.length % 21 || mesh.vertices.length > 2520000 || !mesh.vertices.every(Number.isFinite)) throw Error('Invalid or oversized triangle mesh');
            data = new Float32Array(mesh.vertices); count = data.length / 7;
            const min = [Infinity, Infinity, Infinity], max = [-Infinity, -Infinity, -Infinity];
            for (let i = 0; i < data.length; i += 7) for (let j = 0; j < 3; j++) { min[j] = Math.min(min[j], data[i + j]); max[j] = Math.max(max[j], data[i + j]); }
            center = count ? min.map((value, j) => (value + max[j]) / 2) : [0, 0, 0];
            radius = count ? Math.max(1, ...max.map((value, j) => value - min[j])) / 2 : 1;
            if (!lost) { gl.bindBuffer(gl.ARRAY_BUFFER, buffer); gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW); }
            draw();
            return { triangles: count / 3, path: 'WebGL triangle geometry' };
        },
        draw,
        destroy() {
            if (destroyed) return; destroyed = true;
            canvas.removeEventListener('webglcontextlost', contextLost); canvas.removeEventListener('webglcontextrestored', contextRestored);
            gl.deleteBuffer(buffer); gl.deleteProgram(program); data = null; count = 0;
        }
    };
}
