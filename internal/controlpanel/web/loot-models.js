// Real cache models for the Loot panel, from the optional capture adapter (/capture/client.js,
// cmd/controlpanel -capture-renderer; source tools/capture/client.ts).
//
// Loaded lazily and once per page: the first Loot panel that is on screen and has sources to
// show imports the adapter, whose initialization decodes the whole cache (seconds, and tens of
// MB of heap). Every mesh and animation it exports is kept here, at module level, so a panel
// recreated by the customize dialog (or a second Loot panel) never decodes the same model
// twice. The Float32Arrays are also what a renderer re-uploads after a lost WebGL context.
//
// Sharing the page with the stream client (tools/stream/cache-terrain-client.js), which runs
// the same adapter module in this same page:
//   * The reference client reads `#canvas` once, when the module is first evaluated
//     (src/client/graphics/Canvas.ts), and writes load progress into `#status`. The stream
//     client refuses to start while either id exists, so this module creates them only if
//     absent and removes its own again as soon as the adapter has initialized: the stream
//     view can be opened later, and if it was first, its elements (and its decoder) are used.
//     If the stream view is opened during those few seconds of initialization it reports the
//     ids in use; opening it again afterwards works.
//   * The adapter's detail level is fixed by whoever initializes it first. The stream view
//     wants the high-detail build, so this asks for the same whenever a stream client is
//     installed; otherwise it keeps the low-memory build.
//   * The adapter decodes JPEGs into blob: images while initializing. The dashboard's CSP only
//     allows blob: images when a stream client AND the capture adapter are installed
//     (internal/controlpanel/auth.go, dashboardBlobImages); without that the decoder would
//     wait forever. So models are attempted only when meta says the adapter is usable (see
//     modelsUsable), and every wait for initialization is bounded.

import { animationBounds, modelBounds } from './loot-gl.js';

const INIT_DEADLINE_MS = 20000;

let adapterPromise = null;
let texturePromise = null;
const meshes = new Map(); // key -> { vertices, alpha, bounds, anims } | { failed: true } | { retryAt }
const anims = new Map(); // "key|seq" -> { base, baseAlpha, positions, alphaPositions, delays } | { failed } | { retryAt }
let queue = Promise.resolve(); // adapter calls, one at a time: they share the reference client

/**
 * Whether this page can load cache models at all. meta.captureRenderer is an explicit flag a
 * newer server may send; until then the cache-terrain stream client is the proof the adapter
 * is installed (it refuses to start without it) and that the page may load blob: images. A
 * server that sends captureRenderer must only send true when the page's CSP allows blob:.
 */
export function modelsUsable(meta) {
    if (!meta) return false;
    if (typeof meta.captureRenderer === 'boolean') return meta.captureRenderer;
    return meta.streamClient?.sceneRenderer === 'webgl-cache-terrain';
}

function withDeadline(promise, ms, what) {
    let timer = 0;
    return Promise.race([
        promise.finally(() => clearTimeout(timer)),
        new Promise((_, reject) => { timer = setTimeout(() => reject(Object.assign(Error(`${what} is taking longer than ${ms / 1000}s`), { slow: true })), ms); })
    ]);
}

/**
 * The adapter module, initialized. The initialization itself runs once per page and its
 * failure is final; each caller's wait is bounded, and a wait that runs out (error.slow)
 * leaves the initialization running for a later call to pick up.
 */
export function adapter(meta) {
    adapterPromise ||= (async () => {
        const mine = [];
        if (!document.getElementById('canvas')) {
            const c = Object.assign(document.createElement('canvas'), { id: 'canvas', width: 765, height: 503, hidden: true });
            document.body.append(c); mine.push(c);
        }
        if (!document.getElementById('status')) {
            const s = Object.assign(document.createElement('div'), { id: 'status', hidden: true });
            document.body.append(s); mine.push(s);
        }
        try {
            const mod = await import('/capture/client.js');
            if (typeof mod.entityGpuMeshes !== 'function') throw Error('this capture adapter predates entityGpuMeshes; rebuild it');
            if (typeof mod.configure === 'function') mod.configure({ highDetail: !!meta?.streamClient?.sceneRenderer });
            // The first real call initializes the reference client (the cache decode).
            await (mod.textureAverages ? mod.textureAverages() : mod.entityGpuMeshes([], 1));
            return mod;
        } finally {
            // The module keeps its reference to #canvas; the ids themselves are only needed while
            // initializing, and the stream client refuses to start while they are taken.
            for (const node of mine) node.remove();
        }
    })();
    return withDeadline(adapterPromise, INIT_DEADLINE_MS, 'decoding the game cache');
}

/** The 50 cache textures' average colours, for textured faces drawn untextured. */
export async function textureAverages(meta) {
    const mod = await adapter(meta);
    texturePromise ||= mod.textureAverages ? mod.textureAverages() : Promise.resolve(Array(50).fill(0x808080));
    return texturePromise;
}

function serial(task) {
    const run = queue.then(task, task);
    queue = run.catch(() => {});
    return run;
}

export function cachedMesh(key) {
    const m = meshes.get(key);
    return m && m.vertices ? m : null;
}

export function cachedAnimation(key) {
    const a = anims.get(key);
    return a && a.positions ? a : null;
}

/** Keys still worth asking the adapter for right now. */
function wanted(map, keys) {
    const now = performance.now();
    return keys.filter(k => { const m = map.get(k); return !m || (m.retryAt !== undefined && m.retryAt <= now); });
}

/**
 * Load the bind-pose meshes for requests [{key, kind:'npc', type} | {key, kind:'obj', id}].
 * Resolves to the keys that became available. A model whose cache files are still
 * downloading is retried on a later call; one that cannot be built is not asked for again.
 */
export async function loadMeshes(meta, requests) {
    const want = new Set(wanted(meshes, requests.map(r => r.key)));
    const ask = requests.filter(r => want.has(r.key));
    if (!ask.length) return [];
    const mod = await adapter(meta);
    const tried = new Map(ask.map(r => [r.key, meshes.get(r.key)?.tries || 0]));
    for (const r of ask) meshes.set(r.key, { retryAt: Infinity }); // in flight
    let res;
    try {
        // Each request is rebuilt rather than forwarded, so only the fields the adapter reads
        // are sent. A loc (a tree, a rock) MUST keep its own kind: this used to fall through to
        // the obj branch, which quietly fetched the ITEM with the same id and produced a small
        // flat mesh instead of a tree.
        res = await serial(() => mod.entityGpuMeshes(ask.map(r => {
            if (r.kind === 'npc') return { key: r.key, kind: 'npc', type: r.type };
            if (r.kind === 'loc') return { key: r.key, kind: 'loc', id: r.id, shape: r.shape };
            return { key: r.key, kind: 'obj', id: r.id, count: 1 };
        }), 8000));
    } catch (error) {
        for (const r of ask) meshes.set(r.key, { retryAt: performance.now() + 15000, tries: tried.get(r.key) });
        throw error;
    }
    const done = [];
    const pending = new Set(res.pending || []);
    for (const r of ask) {
        const m = res.meshes?.[r.key];
        if (m && Array.isArray(m.vertices) && (m.floats || 10) === 10) {
            const vertices = new Float32Array(m.vertices), alpha = new Float32Array(m.alpha || []);
            if (!vertices.length && !alpha.length) { meshes.set(r.key, { failed: true }); continue; }
            meshes.set(r.key, { vertices, alpha, bounds: modelBounds(vertices, alpha), anims: m.anims || null });
            done.push(r.key);
        } else if (pending.has(r.key)) {
            const tries = tried.get(r.key) + 1;
            meshes.set(r.key, tries >= 4 ? { failed: true } : { retryAt: performance.now() + 5000, tries });
        } else {
            meshes.set(r.key, { failed: true });
        }
    }
    return done;
}

/** One looping sequence (readyanim/walkanim) on one npc type. Resolves to true when available. */
export async function loadAnimation(meta, key, type, seq) {
    const ak = `${key}|${seq}`;
    if (cachedAnimation(ak)) return true;
    if (!wanted(anims, [ak]).length) return false;
    const mod = await adapter(meta);
    if (typeof mod.animationGpuFrames !== 'function') { anims.set(ak, { failed: true }); return false; }
    anims.set(ak, { retryAt: Infinity });
    try {
        const res = await serial(() => mod.animationGpuFrames({ kind: 'npc', type }, seq, 4000));
        if (!res.frameCount) {
            // Still downloading: try again later. A frame whose face layout differs from the
            // first (layoutChanged) cannot share colours, so that sequence stays unanimated.
            anims.set(ak, res.pending ? { retryAt: performance.now() + 5000 } : { failed: true });
            return false;
        }
        const positions = res.positions.map(p => new Float32Array(p));
        const alphaPositions = res.alphaPositions.map(p => new Float32Array(p));
        anims.set(ak, {
            base: new Float32Array(res.base), baseAlpha: new Float32Array(res.baseAlpha || []),
            positions, alphaPositions,
            delays: res.delays,
            // What this sequence actually occupies, which is not what the bind pose occupies:
            // the renderer frames a playing stage on these rather than on the mesh's bounds.
            bounds: animationBounds([positions, alphaPositions])
        });
        return true;
    } catch (error) {
        anims.set(ak, { retryAt: performance.now() + 15000 });
        throw error;
    }
}

/** Whether asking the adapter for this mesh now could help (not loaded, failed, in flight or waiting to retry). */
export function meshWanted(key) {
    return wanted(meshes, [key]).length > 0;
}

/** The same for one "key|seq" animation. */
export function animationWanted(ak) {
    return wanted(anims, [ak]).length > 0;
}
