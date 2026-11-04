// Reference stream client provider: the FIRST end-to-end join of the control panel's
// stream client contract to the existing-session observation bridge and to real WebGL
// triangles.
//
// Install it explicitly, the same way the capture renderer is installed:
//
//     go run ./cmd/controlpanel -fake 8 -stream-client tools/stream/tile-client.js
//
// The server reads tile-client.js.manifest.json beside this file, checks the renderer
// identity it declares, advertises exactly that in /api/meta.streamClient, and serves
// this module at the fixed same-origin path /stream/client.js. It is not embedded in the
// dashboard binary on purpose: a provider is an installed artifact, and a build that has
// not had one installed must report that it has none.
//
// WHAT THIS ACTUALLY DRAWS, stated plainly because it is easy to overclaim:
//
//   * Real GPU triangles. Vertex buffers, a perspective vertex shader, depth testing and
//     gl.drawArrays(gl.TRIANGLES, ...) — it reuses /gpu-model.js, the same renderer whose
//     geometry path was measured in a browser on the owner's GTX 1650.
//   * Real live data. Every tile, marker and position comes from an observation taken off
//     the account's ALREADY-CONNECTED Go session through /api/stream/frame. This module
//     never opens a game socket and cannot log anything in.
//   * It is NOT the authentic 2004 game window. There is no terrain height, no map
//     texture, no cache model, no animation, no interface, no minimap and no chat. A loc
//     is a coloured block, an NPC is a coloured marker. That is why it declares the
//     renderer identity "webgl-tile-geometry" and not "webgl3d": docs/stream_client_panel.md
//     reserves webgl3d for authentic terrain, models, animation, transparency and picking,
//     and none of that is here.
//   * Input does nothing. setInputEnabled always reports the server's answer, and the
//     server answers 501 on every build in this repository because no Source implements
//     StreamInputSink. This module has no code path that sends an action.
//
// Observation rate is the SERVER's, not the panel's frame-rate preference: the bridge
// hands out whole snapshots and caps itself (see DefaultStreamMaxFPS). This module paces
// itself off the retry hint the server sends and redraws when a frame arrives; it does
// not interpolate, so movement steps rather than glides. That is a real gap, not a
// setting.

import { createGpuModel } from '/gpu-model.js';

// RADIUS is how far either side of the account the ground patch extends, in tiles. Fixed
// on purpose: the underlying renderer re-centres on the mesh bounding box each upload, so
// a constant-size patch centred on the account gives a stable follow camera instead of a
// camera that lurches whenever a loc appears at the edge of the observation.
const RADIUS = 12;
const TILE = 1;
// Flat ground: a headless session's snapshot carries no terrain height at all, so every
// ground vertex sits at y=0 and relief comes only from the blocks below. Saying so here
// rather than quietly drawing a plane that looks like terrain.
const GROUND_Y = 0;
const LOC_HEIGHT = 1.4;
const NPC_HEIGHT = 1.9;
const PLAYER_HEIGHT = 2.4;
const SELF_HEIGHT = 3.0;

const COLOURS = {
    groundEven: [0.16, 0.20, 0.13],
    groundOdd: [0.19, 0.24, 0.15],
    loc: [0.42, 0.33, 0.20],
    npc: [0.78, 0.66, 0.24],
    player: [0.31, 0.47, 0.72],
    self: [0.85, 0.28, 0.24]
};

async function postJSON(path, body, signal) {
    const res = await fetch(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal
    });
    let parsed = null;
    try { parsed = await res.json(); } catch (_) { parsed = null; }
    if (!res.ok) throw Error(parsed?.error || `HTTP ${res.status}`);
    return parsed;
}

// quad pushes two triangles for one axis-aligned rectangle in the y plane.
function quad(out, x0, z0, x1, z1, y, colour) {
    const [r, g, b] = colour;
    const push = (x, z) => out.push(x, y, z, r, g, b, 1);
    push(x0, z0); push(x1, z0); push(x1, z1);
    push(x0, z0); push(x1, z1); push(x0, z1);
}

// block pushes a flat-topped box: the top face plus four sides, so the shape reads as
// solid under the renderer's depth test rather than as a floating rectangle.
function block(out, cx, cz, half, height, colour) {
    const [r, g, b] = colour;
    const x0 = cx - half, x1 = cx + half, z0 = cz - half, z1 = cz + half;
    quad(out, x0, z0, x1, z1, height, colour);
    const side = (ax, az, bx, bz, shade) => {
        const push = (x, y, z) => out.push(x, y, z, r * shade, g * shade, b * shade, 1);
        push(ax, GROUND_Y, az); push(bx, GROUND_Y, bz); push(bx, height, bz);
        push(ax, GROUND_Y, az); push(bx, height, bz); push(ax, height, az);
    };
    side(x0, z0, x1, z0, 0.72);
    side(x1, z0, x1, z1, 0.60);
    side(x1, z1, x0, z1, 0.72);
    side(x0, z1, x0, z0, 0.60);
}

// tile reads one entity's absolute world coordinates out of an observation.
//
// The casing really is mixed, and it is not a mistake to paper over silently: in
// internal/gamestate, RenderSnapshot and RenderPlayer carry json tags (so they arrive as
// "x"/"z"), while NPC and LocInstance do not (so they arrive as Go field names, "X"/"Z").
// A provider that assumed one casing produced NaN coordinates and the renderer refused
// the whole mesh — found in a browser fixture, which is exactly what fixtures are for.
// Reading both is the honest fix here: changing the tags on gamestate's own types would
// change the /api/capture body every existing capture consumer already reads.
function tile(entity) {
    const x = entity.X !== undefined ? entity.X : entity.x;
    const z = entity.Z !== undefined ? entity.Z : entity.z;
    return Number.isFinite(x) && Number.isFinite(z) ? { x, z } : null;
}

// buildMesh turns ONE observation into triangle vertices. Every coordinate here is the
// snapshot's own absolute world tile, re-based to the account so the camera follows it.
function buildMesh(snapshot) {
    const out = [];
    const baseX = snapshot.x, baseZ = snapshot.z;
    for (let dx = -RADIUS; dx <= RADIUS; dx++) {
        for (let dz = -RADIUS; dz <= RADIUS; dz++) {
            const colour = (dx + dz) % 2 === 0 ? COLOURS.groundEven : COLOURS.groundOdd;
            quad(out, dx * TILE, dz * TILE, (dx + 1) * TILE, (dz + 1) * TILE, GROUND_Y, colour);
        }
    }
    const counts = { locs: 0, npcs: 0, players: 0, skipped: 0 };
    const place = (entity, half, height, colour, key) => {
        const at = tile(entity);
        if (!at) { counts.skipped++; return; }
        const dx = at.x - baseX, dz = at.z - baseZ;
        if (Math.abs(dx) > RADIUS || Math.abs(dz) > RADIUS) return;
        block(out, dx + 0.5, dz + 0.5, half, height, colour);
        counts[key]++;
    };
    for (const loc of snapshot.locs || []) {
        if (loc.Deleted || loc.deleted) continue;
        place(loc, 0.5, LOC_HEIGHT, COLOURS.loc, 'locs');
    }
    for (const npc of snapshot.npcs || []) place(npc, 0.3, NPC_HEIGHT, COLOURS.npc, 'npcs');
    for (const player of snapshot.players || []) place(player, 0.3, PLAYER_HEIGHT, COLOURS.player, 'players');
    block(out, 0.5, 0.5, 0.34, SELF_HEIGHT, COLOURS.self);
    return { vertices: out, triangles: out.length / 21, ...counts };
}

export async function mount(host, { account, fps, signal }) {
    if (signal?.aborted) throw Error('Aborted before the stream client started.');

    const canvas = document.createElement('canvas');
    canvas.style.cssText = 'width:100%;height:100%;display:block;touch-action:none';
    // Real text in real DOM, over the canvas — the dashboard's own rule: WebGL for the
    // dense visual, DOM for anything anybody has to read, select or hear read out.
    const overlay = document.createElement('div');
    overlay.style.cssText = 'position:absolute;left:0;bottom:0;right:0;padding:4px 6px;font:11px/1.4 monospace;background:rgba(0,0,0,.55);color:#e8e2d0;white-space:pre-wrap';
    overlay.setAttribute('role', 'status');
    host.replaceChildren(canvas, overlay);

    let renderer = null, session = null, stopped = false, timer = 0, yaw = 0.7, pitch = -0.55;
    let frames = 0, lastSeq = 0, lastError = '';

    const stopEverything = () => {
        stopped = true;
        if (timer) { clearTimeout(timer); timer = 0; }
    };
    // destroy is idempotent and safe on a partially initialized instance: every step
    // below is individually guarded, because an abort can land between any two awaits.
    const destroy = async () => {
        stopEverything();
        canvas.removeEventListener('pointerdown', onPointerDown);
        window.removeEventListener('pointermove', onPointerMove);
        window.removeEventListener('pointerup', onPointerUp);
        const closing = session;
        session = null;
        try { renderer?.destroy(); } catch (_) { /* cleanup continues */ }
        renderer = null;
        try { host.replaceChildren(); } catch (_) { /* cleanup continues */ }
        if (closing) {
            // Release the server's single view lease promptly rather than making the next
            // operator wait out its TTL. Never allowed to throw out of destroy().
            try { await postJSON('/api/stream/close', { session: closing }, undefined); } catch (_) { /* the lease expires on its own */ }
        }
    };
    const abort = () => { destroy(); };
    signal?.addEventListener('abort', abort, { once: true });

    let dragging = false, lastX = 0, lastY = 0;
    function onPointerDown(ev) { dragging = true; lastX = ev.clientX; lastY = ev.clientY; canvas.setPointerCapture?.(ev.pointerId); }
    function onPointerMove(ev) {
        if (!dragging || !renderer) return;
        yaw += (ev.clientX - lastX) * 0.01;
        pitch = Math.max(-1.4, Math.min(-0.1, pitch + (ev.clientY - lastY) * 0.006));
        lastX = ev.clientX; lastY = ev.clientY;
        renderer.draw(yaw, pitch);
    }
    function onPointerUp() { dragging = false; }
    canvas.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);

    try {
        renderer = createGpuModel(canvas);
        // The FPS preference is the panel's render preference. The server decides how
        // often it will actually take an observation and says so in its reply; this asks
        // for what the operator picked and accepts whatever it is given.
        session = (await postJSON('/api/stream/open', { account, fps }, signal))?.session;
        if (!session) throw Error('The session bridge did not return a view lease.');
    } catch (error) {
        await destroy();
        throw error;
    }
    if (signal?.aborted || stopped) { await destroy(); throw Error('Aborted while the stream client was starting.'); }

    const openedSession = session;
    const pump = async () => {
        if (stopped || session !== openedSession) return;
        let wait = Math.max(50, Math.round(1000 / Math.max(1, fps || 5)));
        try {
            const res = await fetch(`/api/stream/frame?session=${encodeURIComponent(openedSession)}`, { signal, cache: 'no-store' });
            const body = await res.json().catch(() => null);
            if (stopped || session !== openedSession) return;
            if (!res.ok) {
                // 409/410 mean the observed session changed or the lease is gone. Neither
                // is recoverable by retrying with the same lease, and a provider that kept
                // painting would be showing a session that no longer exists.
                lastError = body?.error || `HTTP ${res.status}`;
                overlay.textContent = `stream ended: ${lastError}`;
                stopEverything();
                return;
            }
            if (body?.throttled) {
                wait = Math.max(20, body.retryInMs || wait);
            } else if (body?.snapshot) {
                const mesh = buildMesh(body.snapshot);
                const drawn = renderer.setMesh(mesh);
                renderer.draw(yaw, pitch);
                frames++; lastSeq = body.seq;
                overlay.textContent =
                    `${account}  tile ${body.snapshot.x},${body.snapshot.z} level ${body.snapshot.level}  ` +
                    `seq ${body.seq} epoch ${body.epoch}  frames ${frames}\n` +
                    `${drawn.triangles} GPU triangles  ${mesh.locs} locs  ${mesh.npcs} npcs  ${mesh.players} players` +
                    (mesh.skipped ? `  ${mesh.skipped} entities skipped (no usable coordinates)` : '') + '  ' +
                    `${body.transport} frames, not deltas — diagnostic tile geometry, not the authentic 2004 scene`;
            }
        } catch (error) {
            if (stopped || signal?.aborted) return;
            lastError = error?.message || String(error);
            overlay.textContent = `stream error: ${lastError}`;
        }
        if (stopped || session !== openedSession) return;
        timer = setTimeout(pump, wait);
    };
    pump();

    return {
        account,
        // Declared identity must match what the server advertises from this module's own
        // manifest, or the panel rejects and destroys this instance. Keep the two in step.
        sceneRenderer: 'webgl-tile-geometry',
        // Reported so a fixture or an operator can read the real numbers rather than
        // taking a screenshot's word for it.
        stats: () => ({ frames, lastSeq, lastError, session: openedSession }),
        async setInputEnabled(enabled) {
            // No local decision: whatever the server acknowledges is the answer. On every
            // build in this repository that is a 501, and the panel then releases the
            // whole instance rather than leaving ownership ambiguous.
            const body = await postJSON('/api/stream/input', { session: openedSession, enabled }, signal);
            return body?.acknowledged === true;
        },
        destroy
    };
}
