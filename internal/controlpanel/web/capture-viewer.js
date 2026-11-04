const account = new URL(location.href).searchParams.get('account');
const status = document.querySelector('#status');
const preview = document.querySelector('#preview');
const refresh = document.querySelector('#refresh');
let renderer;
// GPU presentation is optional. The authentic client rasterizer remains CPU-based.
const gl = preview.getContext('webgl', { alpha: false, preserveDrawingBuffer: true, powerPreference: 'low-power' });
let program, texture;
if (gl) {
    const shader = (kind, source) => {
        const s = gl.createShader(kind); gl.shaderSource(s, source); gl.compileShader(s);
        if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s));
        return s;
    };
    program = gl.createProgram();
    gl.attachShader(program, shader(gl.VERTEX_SHADER, 'attribute vec2 p; varying vec2 uv; void main(){uv=vec2((p.x+1.0)/2.0,(1.0-p.y)/2.0);gl_Position=vec4(p,0.0,1.0);}'));
    gl.attachShader(program, shader(gl.FRAGMENT_SHADER, 'precision mediump float; varying vec2 uv; uniform sampler2D image; void main(){gl_FragColor=texture2D(image,uv);}'));
    gl.linkProgram(program); gl.useProgram(program);
    const buffer = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER,buffer);
    gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),gl.STATIC_DRAW);
    const position = gl.getAttribLocation(program,'p'); gl.enableVertexAttribArray(position); gl.vertexAttribPointer(position,2,gl.FLOAT,false,0,0);
    texture = gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D,texture);
    gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.NEAREST); gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_S,gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_T,gl.CLAMP_TO_EDGE);
}
document.querySelector('#backend').textContent = gl ? 'WebGL presentation available. 3D scene rasterization uses the authentic CPU renderer; browser GPU acceleration depends on your browser settings.' : 'Canvas presentation. WebGL is unavailable in this browser.';
async function capture() {
    refresh.disabled = true;
    try {
        status.textContent = `Capturing ${account}…`;
        const response = await fetch('/api/capture', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({account}) });
        const data = await response.json();
        if (!response.ok) throw new Error(data.error || data.problem || 'Account capture unavailable');
        renderer ||= await import('/capture/client.js');
        const image = await renderer.renderSnapshot(data.snapshot);
        preview.width = image.width; preview.height = image.height;
        if (gl && !gl.isContextLost()) {
            gl.viewport(0,0,preview.width,preview.height);
            gl.texImage2D(gl.TEXTURE_2D,0,gl.RGBA,gl.RGBA,gl.UNSIGNED_BYTE,image);
            gl.drawArrays(gl.TRIANGLE_STRIP,0,4);
        } else if (!gl) preview.getContext('2d').drawImage(image,0,0);
        else throw new Error('WebGL context lost; reopen the capture window');
        const png = image.toDataURL('image/png');
        const link = document.querySelector('#download'); link.href = png; link.download = `${account}-${data.capturedAt}.png`; link.hidden = false;
        status.textContent = `${account}: ${data.snapshot.x}, ${data.snapshot.z}, level ${data.snapshot.level} — ${new Date(data.capturedAt).toLocaleString()} — ${image.width === 765 ? 'game window' : 'scene only (older fleet build)'}`;
        if (window.opener) window.opener.postMessage({type:'account-capture',account,png,capturedAt:data.capturedAt},location.origin);
    } catch (error) { status.textContent = error.message; }
    finally { refresh.disabled = false; }
}
refresh.addEventListener('click',capture);
capture();
