import { createGpuModel } from './gpu-model.js';
const status = document.querySelector('#status'), load = document.querySelector('#load');
let renderer = null;
load.addEventListener('click', async () => {
    load.disabled = true;
    try {
        const adapter = await import('/capture/client.js');
        if (!adapter.itemGpuMesh) throw Error('Rebuild the optional capture adapter to enable GPU geometry export.');
        const mesh = await adapter.itemGpuMesh(Number(document.querySelector('#item').value));
        renderer ||= createGpuModel(document.querySelector('#gpu'));
        const result = renderer.setMesh(mesh);
        status.textContent = `${mesh.name}: ${result.triangles} GPU triangles; omitted ${mesh.texturedFaces} textured and ${mesh.alphaFaces} alpha faces. Hardware acceleration depends on the browser GPU driver.`;
    } catch (error) { status.textContent = `Model unavailable: ${error.message || error}`; }
    finally { load.disabled = false; }
});
document.querySelector('#yaw').addEventListener('input', event => renderer?.draw(Number(event.target.value)));
window.addEventListener('resize', () => renderer?.draw());
window.addEventListener('pagehide', () => { renderer?.destroy(); renderer = null; });
