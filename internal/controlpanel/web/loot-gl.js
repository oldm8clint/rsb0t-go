// The Loot panel's model stage: every source card's real cache model on a stone turntable,
// drawn by ONE WebGL context for the whole panel.
//
// One canvas lies over the panel's scrolling card list (pointer-events: none). Each painted
// frame clears it to transparent, then for every card whose stage is on screen sets the
// viewport and scissor to that stage's rectangle and draws the pedestal, its soft shadow and
// the model into it. So the number of contexts never grows with the number of sources (a
// page gets ~16 WebGL contexts before the browser starts dropping the oldest), and every
// model is uploaded once and drawn as often as needed.
//
// Geometry comes from the capture adapter (tools/capture/client.ts, entityGpuMeshes and
// animationGpuFrames) in the stream client's vertex layout, so this is the same approach as
// tools/stream/scene-gl.js, cut down to what a turntable needs: 10 floats per vertex (x y z
// r g b a u v tex), tiles, y up. Untextured faces carry the cache's own lit colour (the
// software renderer's lighting is already applied); a textured face carries a brightness
// that is multiplied by the texture's average colour, as scene-gl does without its atlas.
// Faces with alpha go in a second, blended pass.
//
// Animation: each sequence frame is a positions-only buffer beside frame 0's full vertices,
// as scene-gl stores them. The game steps frames; this tweens between the current and next
// frame (mix in the vertex shader), which reads smoother on a slow spin. TWEEN = false gives
// the game's own stepping.
//
// Without WebGL (or with only a software one: failIfMajorPerformanceCaveat, the same rule
// gpu-text.js, map-webgl.js and heat-render.js follow) drawModelSprite paints a still pose
// with Canvas 2D: flat-shaded triangles, back faces culled, sorted far to near.

export const FLOATS = 10;
const STRIDE = FLOATS * 4;
export const TWEEN = true;
// The reference client's animation clock: sequence delays count 20 ms cycles.
const CYCLE_MS = 20;
const FOV = 0.55;
const PITCH = 0.3; // the camera looks down a little, as the game camera does
const PEDESTAL_DEPTH = 0.3; // pedestal height below the ground plane, in pedestal radii

/**
 * Where to frame one model: its footprint's CENTRE and radius, and its height, from its bind pose.
 *
 * The centre is load-bearing. Nothing guarantees a cache model is built about its own origin in
 * x/z any more than it is in y (see baseLift, which fixed only the vertical axis). Measured in
 * the panel 2026-09-20: the Moss giant's footprint centre sits 0.40 tiles north of its origin
 * and the Strange box's 0.25 tiles west of it, so both were drawn off to one side of their
 * plinth — and because `foot` was the radius about the ORIGIN, the Moss giant also got a 1.046
 * tile plinth where its own footprint only needs 0.697.
 *
 * `reach` is how far the model ever gets from that centre horizontally. It starts equal to
 * `foot` and animationBounds widens it, because a walk cycle swings a figure's arms well
 * outside its standing footprint (up to 1.54x, measured on npc 1). The plinth is sized from
 * `foot` so it stays the figure's own footprint; the camera frames `reach` so nothing a
 * sequence throws outwards leaves the stage.
 */
export function modelBounds(vertices, alpha) {
    let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity, top = 0, bottom = 0;
    for (const v of [vertices, alpha]) {
        for (let i = 0; i < v.length; i += FLOATS) {
            const x = v[i], y = v[i + 1], z = v[i + 2];
            if (x < x0) x0 = x;
            if (x > x1) x1 = x;
            if (z < z0) z0 = z;
            if (z > z1) z1 = z;
            if (y > top) top = y;
            if (y < bottom) bottom = y;
        }
    }
    if (!(x0 <= x1)) { x0 = x1 = z0 = z1 = 0; }
    const cx = (x0 + x1) / 2, cz = (z0 + z1) / 2;
    let foot = 0;
    for (const v of [vertices, alpha]) {
        for (let i = 0; i < v.length; i += FLOATS) {
            const r = Math.hypot(v[i] - cx, v[i + 2] - cz);
            if (r > foot) foot = r;
        }
    }
    foot = Math.max(0.05, foot);
    return { cx, cz, foot, reach: foot, top: Math.max(0.15, top), bottom };
}

/**
 * The same, over an animation's own frames (positions-only buffers, 3 floats per vertex, as
 * loot-models.js keeps them), so a stage that PLAYS a sequence is framed on what it draws.
 *
 * A cache model's bind pose is not its playing pose. Measured 2026-09-20: the Strange plant's
 * bind pose is 2.50 tiles tall but its readyanim never rises above 1.35, so a camera framed on
 * the bind pose drew the plant at 54% of the height it had reserved for it — which is the
 * owner's "the strange plant is too small for its card", and why no scale factor was the answer.
 * Every other npc here has a playing height within 1% of its bind pose, which is why only the
 * plant looked wrong.
 */
export function animationBounds(frameSets) {
    let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity, top = 0, bottom = 0, any = false;
    for (const frames of frameSets) {
        for (const p of frames) {
            for (let i = 0; i < p.length; i += 3) {
                const x = p[i], y = p[i + 1], z = p[i + 2];
                any = true;
                if (x < x0) x0 = x;
                if (x > x1) x1 = x;
                if (z < z0) z0 = z;
                if (z > z1) z1 = z;
                if (y > top) top = y;
                if (y < bottom) bottom = y;
            }
        }
    }
    if (!any) return null;
    const cx = (x0 + x1) / 2, cz = (z0 + z1) / 2;
    let foot = 0;
    for (const frames of frameSets) {
        for (const p of frames) {
            for (let i = 0; i < p.length; i += 3) {
                const r = Math.hypot(p[i] - cx, p[i + 2] - cz);
                if (r > foot) foot = r;
            }
        }
    }
    foot = Math.max(0.05, foot);
    return { cx, cz, foot, reach: foot, top: Math.max(0.15, top), bottom };
}

/** Pedestal radius (tiles) for a model's footprint: a person gets a tile-wide plinth. */
function pedestalRadius(b) {
    return Math.max(0.34, b.foot * 1.05);
}

/** How long one pass of a sequence takes, in ms. 0 when it has no frames. */
export function loopLength(delays) {
    let total = 0;
    for (const d of delays ?? []) total += Math.max(1, d || 1);
    return total * CYCLE_MS;
}

/**
 * The frame of a looping sequence at elapsedMs, as the client plays a ready/walk loop, plus
 * how far it is towards the next frame (0..1) for tweening.
 */
export function loopFrame(delays, elapsedMs) {
    const n = delays.length;
    if (!n) return { frame: 0, next: 0, mix: 0 };
    let total = 0;
    for (const d of delays) total += Math.max(1, d || 1);
    let cycles = (elapsedMs / CYCLE_MS) % total;
    for (let f = 0; f < n; f++) {
        const d = Math.max(1, delays[f] || 1);
        if (cycles < d) return { frame: f, next: (f + 1) % n, mix: TWEEN ? cycles / d : 0 };
        cycles -= d;
    }
    return { frame: 0, next: 0, mix: 0 };
}

// ---- matrices (column-major, for uniformMatrix4fv) ----

function perspective(fovY, aspect, near, far) {
    const f = 1 / Math.tan(fovY / 2), nf = 1 / (near - far);
    return [f / aspect, 0, 0, 0, 0, f, 0, 0, 0, 0, (far + near) * nf, -1, 0, 0, 2 * far * near * nf, 0];
}

function multiply(a, b, out = new Float32Array(16)) {
    for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
        out[c * 4 + r] = a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] + a[12 + r] * b[c * 4 + 3];
    }
    return out;
}

function lookFrom(cy, d, aspect, reach) {
    const ey = cy + d * Math.sin(PITCH), ez = d * Math.cos(PITCH);
    // lookAt from (0, ey, ez) to (0, cy, 0), up y. With f = (0, fy, fz) the side vector
    // f x up is (1, 0, 0) and up is side x f = (0, -fz, fy).
    let fy = cy - ey, fz = -ez;
    const fl = Math.hypot(fy, fz); fy /= fl; fz /= fl;
    const uy = -fz, uz = fy;
    const V = [1, 0, 0, 0, 0, uy, -fy, 0, 0, uz, -fz, 0, 0, -(uy * ey + uz * ez), fy * ey + fz * ez, 1];
    const near = Math.max(0.02, d - reach * 3), far = d + reach * 3 + 2;
    return multiply(perspective(FOV, aspect, near, far), V);
}

/**
 * The camera for one stage: world (x east, y up, z north) -> clip space. The camera stands
 * south of the model, a little above it, looking north: npc models face south in the cache,
 * so the model faces the viewer at yaw 0. The world z flip into GL's right-handed space is
 * folded into the model matrix, as scene-gl.js does; with it front faces wind CCW on screen.
 *
 * Framing: whatever the spin, the model stays inside a cylinder (its `reach` radius about its
 * footprint centre, its height) standing on the pedestal, whose own radius R is passed in
 * rather than derived here — a stage playing an animation is framed on the ANIMATION's bounds
 * while the plinth stays sized to the model's standing footprint, so the plinth does not jump
 * the moment a sequence finishes downloading. The camera is moved in and re-aimed until that
 * cylinder's silhouette fills ~94% of the stage, so a tall knight and a flat casket both
 * fill their stage. Cached per bounds, stage shape and plinth.
 */
function stageCamera(b, aspect, R) {
    const key = `${aspect.toFixed(3)}|${R.toFixed(3)}`;
    b.cameras ||= new Map();
    const hit = b.cameras.get(key);
    if (hit) return hit;
    const bottom = -PEDESTAL_DEPTH * R, rim = R * 1.07;
    // The model is drawn lifted so its lowest vertex rests on the plinth (see baseLift), so
    // frame its lifted height. Reading b.top alone was correct only while every model
    // happened to have its origin at its base.
    const height = b.top + baseLift(b);
    const pts = [];
    // 24 samples rather than 12: a circle's silhouette read off 12 points under-measures its
    // true width by up to 3.4%, which the 0.94 fill then spends straight back on overflow.
    // The model's own radius is sampled at three heights, not only the top, because the widest
    // part of a figure is usually its middle.
    for (let i = 0; i < 24; i++) {
        const a = (i / 24) * Math.PI * 2, c = Math.cos(a), s = Math.sin(a);
        pts.push([rim * c, 0, rim * s], [rim * c, bottom, rim * s],
            [b.reach * c, height, b.reach * s], [b.reach * c, height / 2, b.reach * s], [b.reach * c, 0, b.reach * s]);
    }
    const reach = Math.max(rim, b.reach);
    let cy = (height + bottom) / 2, d = (height - bottom + 2 * reach) / Math.tan(FOV / 2);
    let M = null;
    for (let pass = 0; pass < 4; pass++) {
        M = lookFrom(cy, d, aspect, reach);
        let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
        for (const [x, y, z] of pts) {
            const w = M[3] * x + M[7] * y - M[11] * z + M[15];
            const nx = (M[0] * x + M[4] * y - M[8] * z + M[12]) / w, ny = (M[1] * x + M[5] * y - M[9] * z + M[13]) / w;
            x0 = Math.min(x0, nx); x1 = Math.max(x1, nx); y0 = Math.min(y0, ny); y1 = Math.max(y1, ny);
        }
        const extent = Math.max(x1, -x0, (y1 - y0) / 2);
        // NDC y is roughly view-space y over d tan(fov/2): re-aim at the silhouette's middle.
        cy += ((y1 + y0) / 2) * d * Math.tan(FOV / 2);
        d *= extent / 0.94;
    }
    M = lookFrom(cy, d, aspect, reach);
    b.cameras.set(key, M);
    return M;
}

/**
 * world -> GL: the model moved onto the plinth's axis, z flipped, then the spin about y.
 *
 * (cx, cz) is the model's own footprint centre (see modelBounds). Subtracting it BEFORE the
 * rotation is what makes an off-centre model turn about itself on the middle of the plinth
 * instead of orbiting its own origin off to one side — the owner's "some npcs aren't centred".
 */
function spinFlip(yaw, lift = 0, cx = 0, cz = 0, out = new Float32Array(16)) {
    const c = Math.cos(yaw), s = Math.sin(yaw);
    out.set([c, 0, -s, 0, 0, 1, 0, 0, -s, 0, -c, 0, -(c * cx - s * cz), lift, s * cx + c * cz, 1]);
    return out;
}

/**
 * spinFlip for ONE model standing somewhere on a SHARED plinth: scaled by `scale`, stood at
 * (ox, oz) on the turntable, and turned with it.
 *
 * A point goes p -> R * (offset + scale * (p - centre)) + (0, lift, 0), where R is the same
 * yaw-plus-z-flip spinFlip applies. Because the offset is inside R, a satellite orbits with the
 * plinth instead of sliding across a turning one. At scale 1 with no offset this is spinFlip
 * exactly, which is what every single-model card still uses.
 */
function placeFlip(yaw, lift, cx, cz, scale, ox, oz, out = new Float32Array(16)) {
    const c = Math.cos(yaw), s = Math.sin(yaw);
    // R applied to (offset - scale * centre), which is the whole translation bar the lift.
    const vx = ox - scale * cx, vz = oz - scale * cz;
    out.set([
        c * scale, 0, -s * scale, 0,
        0, scale, 0, 0,
        -s * scale, 0, -c * scale, 0,
        c * vx - s * vz, lift, -s * vx - c * vz, 1
    ]);
    return out;
}

/**
 * The cylinder a whole arrangement occupies, so one plinth can be sized for it and one camera
 * can frame it.
 *
 * Each placement rests its own scaled base on the plinth top, so the group's floor is 0 and its
 * ceiling is the tallest lifted model. Its radius is how far the furthest placement reaches from
 * the middle: its own distance out plus its own scaled reach.
 */
function groupBounds(places, boundsOf) {
    let reach = 0.05, top = 0.15;
    for (const p of places) {
        const b = boundsOf(p);
        if (!b) continue;
        const s = p.scale ?? 1;
        const r = Math.hypot(p.ox ?? 0, p.oz ?? 0) + s * b.reach;
        if (r > reach) reach = r;
        const t = s * b.top - Math.min(0, s * b.bottom);
        if (t > top) top = t;
    }
    return { cx: 0, cz: 0, foot: reach, reach, top, bottom: 0 };
}

/**
 * How far to raise a model so it RESTS on the plinth instead of intersecting it.
 *
 * Nothing guarantees a cache model's origin is at its base. An npc stands on its own
 * origin, so it needs no lift, but an item model is built about its middle — which is why
 * the Strange box sank through the pedestal instead of sitting on it (owner, 2026-09-20).
 * modelBounds seeds bottom at 0 and only ever lowers it, so this is 0 for a model that
 * already rests correctly and positive for one that would sink: safe to apply to every
 * model rather than special-casing the ones noticed so far.
 */
function baseLift(b) {
    return -Math.min(0, b.bottom || 0);
}

// How finely the turn is sampled when working out which part of it a model survives, the grid
// its silhouette is measured on, and how much of its own best silhouette it must still show to
// count as facing the viewer.
//
// The floor has a wide margin behind it. Measured across the sixteen models this panel draws,
// the worst silhouette any HEALTHY model falls to is 0.48 of its own best (the Cow, seen
// end-on); the Strange box falls to 0.15. Anything from about 0.25 to 0.45 separates them, so
// 0.3 sits in the middle of a gap more than three times wide.
const ARC_SAMPLES = 24;
const ARC_GRID = 28;
const ARC_FLOOR = 0.3;

/**
 * How much of a small grid a model covers under one matrix: its SILHOUETTE, not the area of its
 * triangles.
 *
 * The difference matters and cost an attempt to get right. Summing the area of the triangles
 * that survive culling counts overlapping faces twice, so a model made of big flat planes reads
 * as collapsing when it is merely edge-on to some of them: it fired on the Kalphite Queen's
 * flying form, whose silhouette never drops below 0.68 of its best. Filling the triangles into a
 * coverage grid and counting cells measures what is actually on screen, which is the question.
 *
 * Front faces only, decided the same way drawModelSprite's collect decides (a front face
 * projects to a negative signed area once the model matrix has flipped z).
 */
function frontCoverage(datas, M, grid, seen) {
    seen.fill(0);
    for (const data of datas) {
        for (let i = 0; i < data.length; i += FLOATS * 3) {
            const o1 = i, o2 = i + FLOATS, o3 = i + FLOATS * 2;
            const a = project(M, data[o1], data[o1 + 1], data[o1 + 2], grid, grid);
            const b = project(M, data[o2], data[o2 + 1], data[o2 + 2], grid, grid);
            const c = project(M, data[o3], data[o3 + 1], data[o3 + 2], grid, grid);
            const area = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
            if (area >= 0) continue; // back face: culled, so it is not on screen
            const x0 = Math.max(0, Math.floor(Math.min(a[0], b[0], c[0])));
            const x1 = Math.min(grid - 1, Math.ceil(Math.max(a[0], b[0], c[0])));
            const y0 = Math.max(0, Math.floor(Math.min(a[1], b[1], c[1])));
            const y1 = Math.min(grid - 1, Math.ceil(Math.max(a[1], b[1], c[1])));
            for (let y = y0; y <= y1; y++) {
                for (let x = x0; x <= x1; x++) {
                    const px = x + 0.5, py = y + 0.5;
                    const w0 = (b[0] - a[0]) * (py - a[1]) - (b[1] - a[1]) * (px - a[0]);
                    const w1 = (c[0] - b[0]) * (py - b[1]) - (c[1] - b[1]) * (px - b[0]);
                    const w2 = (a[0] - c[0]) * (py - c[1]) - (a[1] - c[1]) * (px - c[0]);
                    if (w0 <= 0 && w1 <= 0 && w2 <= 0) seen[y * grid + x] = 1;
                }
            }
        }
    }
    let n = 0;
    for (let i = 0; i < seen.length; i++) n += seen[i];
    return n;
}

/** The longest run of samples at or above `floor`, as a yaw centre and half-width. */
function longestArc(cover, floor) {
    const n = cover.length, ok = cover.map(c => c >= floor);
    if (ok.every(Boolean) || ok.every(v => !v)) return null;
    let bestStart = 0, bestLen = 0, start = 0, len = 0;
    for (let i = 0; i < n * 2; i++) {
        if (ok[i % n]) {
            if (len === 0) start = i;
            len++;
            if (len > bestLen) { bestLen = len; bestStart = start; }
        } else len = 0;
    }
    if (bestLen <= 1 || bestLen >= n) return null;
    const step = (Math.PI * 2) / n;
    return { centre: (bestStart + (bestLen - 1) / 2) * step, half: ((bestLen - 1) / 2) * step };
}

/**
 * The arc of yaw a model can be shown through, or null for one that survives a whole turn.
 *
 * Some cache models are authored for a fixed-angle inventory icon and are simply not there from
 * behind. The Strange box (obj 3062) is the one the owner noticed: measured over 16 yaws, it
 * holds 57-100% of its largest silhouette from yaw 5.5 round through 0 to 2.36, and collapses to
 * 15-17% from 2.75 to 5.11 — for about 40% of every turn there is almost nothing there, which he
 * read as "still shows transparency on some sides as it spins". No winding, mirroring or culling
 * change survives a full rotation, because the faces genuinely are not in the file; an earlier
 * attempt to mirror them closed made it measurably worse and was reverted.
 *
 * So the fix is presentational, and derived rather than listed: this turns the model through
 * ARC_SAMPLES yaws, measures the silhouette culling leaves at each, and returns the longest run
 * still showing ARC_FLOOR of its best. A model that never drops that far — every other model
 * this panel draws — gets null and keeps turning as it always has. Nothing here names a model,
 * and a content change that fixes or breaks one is picked up on its own.
 *
 * A few milliseconds of CPU once per mesh, cached on the bounds object beside the camera cache.
 */
export function viewableArc(src) {
    const b = src.bounds;
    if (b.arc !== undefined) return b.arc;
    b.arc = null;
    const R = pedestalRadius(b), lift = baseLift(b);
    // A square grid: the aspect scales x and y alike, so it cannot change which samples clear
    // the floor relative to one another.
    const cam = stageCamera(b, 1, R);
    const model = new Float32Array(16), mvp = new Float32Array(16);
    const seen = new Uint8Array(ARC_GRID * ARC_GRID);
    const datas = [src.vertices, src.alpha];
    const cover = [];
    for (let i = 0; i < ARC_SAMPLES; i++) {
        const M = multiply(cam, spinFlip((i / ARC_SAMPLES) * Math.PI * 2, lift, b.cx, b.cz, model), mvp);
        cover.push(frontCoverage(datas, M, ARC_GRID, seen));
    }
    const max = Math.max(...cover);
    if (max > 0 && Math.min(...cover) < max * ARC_FLOOR) b.arc = longestArc(cover, max * ARC_FLOOR);
    return b.arc;
}

// THE STRANGE BOX (obj 3062) IS A PARTIAL SHELL, AND NOTHING HERE CAN CLOSE IT.
//
// Measured 2026-09-20, after a first attempt at fixing it made it worse and had to be reverted.
// Recording the numbers so the next person does not spend the same evening on it:
//
//   * Its 65 triangles sit on a unit cube. The x-, y+ and z- planes carry faces covering 55%,
//     57% and 58% of their plane; the x+, y- and z+ planes carry ZERO triangles. Every one of
//     the 35 triangles on the faces that exist winds the same way, so this is NOT a winding or
//     culling bug, whatever it looks like.
//   * Spun through 16 yaws and its painted body measured per frame, the cache model holds 57%
//     to 100% of its largest silhouette from yaw 5.5 round through 0 to 2.36 — and collapses to
//     15-17% from 2.75 to 5.11. For about 40% of every turn there is almost nothing there.
//     That is the owner's "the strange box still shows transparency on some sides as it spins".
//   * Reflecting the three empty planes through the box centre to close it (tried, shipped,
//     reverted) took the holes INSIDE the silhouette from a mean of 1.0% to 17.3%, worst 35.9%:
//     the reflected faces are culled from the far side, so it gained real see-through gaps on
//     top of the thin ones. Reflecting without reversing the winding was worse still (22.7%).
//   * Drawing it double-sided removes every hole by that metric, but only because the model
//     collapses to flat plates edge-on: from behind it reads as two panels standing open, not
//     as a box.
//
// The geometry is authored for an inventory icon seen from one fixed angle and there is no
// version of it that survives a full turn. The fix is presentational, not geometric: keep this
// model inside the arc its own geometry supports (roughly yaw -0.8 to +2.4) and oscillate
// there rather than spinning it through the dead half. That is not built yet.

function scaleFlip(sx, sy, sz, lift, out = new Float32Array(16)) {
    out.set([sx, 0, 0, 0, 0, sy, 0, 0, 0, 0, -sz, 0, 0, lift, 0, 1]);
    return out;
}

// ---- the pedestal: a round stone plinth, lit from the camera's upper left ----

const LIGHT = (() => { const l = [-0.55, 0.75, -0.45], n = Math.hypot(...l); return l.map(x => x / n); })();
const lit = (nx, ny, nz, base) => {
    const n = Math.hypot(nx, ny, nz), d = Math.max(0, (nx * LIGHT[0] + ny * LIGHT[1] + nz * LIGHT[2]) / n);
    const k = 0.42 + 0.58 * d;
    return base.map(c => c * k);
};
const STONE_TOP = [0.53, 0.49, 0.42];
const STONE_RIM = [0.47, 0.43, 0.36];
const STONE_SIDE = [0.36, 0.32, 0.27];
const STONE_LIP = [0.26, 0.23, 0.19];

/** A unit-radius plinth, top face at y = 0, as FLOATS-layout triangles (no texture). */
export function pedestalMesh(segments = 32) {
    const out = [];
    const push = (x, y, z, c) => out.push(x, y, z, c[0], c[1], c[2], 1, 0, 0, -1);
    const ring = [
        // [radius, y] down the profile
        [0.88, 0], [1, -0.06], [1, -0.24], [1.07, -0.27], [1.07, -PEDESTAL_DEPTH]
    ];
    for (let i = 0; i < segments; i++) {
        const a0 = (i / segments) * Math.PI * 2, a1 = ((i + 1) / segments) * Math.PI * 2;
        const c0 = Math.cos(a0), s0 = Math.sin(a0), c1 = Math.cos(a1), s1 = Math.sin(a1);
        const am = (a0 + a1) / 2, cm = Math.cos(am), sm = Math.sin(am);
        // Top: a faint radial falloff, brighter towards the middle like a worn step.
        const centre = STONE_TOP.map(c => c * 1.08);
        push(0, 0, 0, centre);
        push(ring[0][0] * c0, 0, ring[0][0] * s0, STONE_TOP);
        push(ring[0][0] * c1, 0, ring[0][0] * s1, STONE_TOP);
        for (let k = 0; k < ring.length - 1; k++) {
            const [r0, y0] = ring[k], [r1, y1] = ring[k + 1];
            const dr = r1 - r0, dy = y1 - y0;
            // Outward normal of this band: perpendicular to its profile.
            const ny = dr === 0 ? 0 : -dr / Math.hypot(dr, dy) * Math.sign(dy);
            const nr = dr === 0 ? 1 : Math.abs(dy) / Math.hypot(dr, dy);
            const base = k === 0 ? STONE_RIM : k === 2 ? STONE_RIM : k === 3 ? STONE_LIP : STONE_SIDE;
            const c = lit(nr * cm, ny || 0.0001, nr * sm, base);
            push(r0 * c0, y0, r0 * s0, c); push(r1 * c0, y1, r1 * s0, c); push(r1 * c1, y1, r1 * s1, c);
            push(r0 * c0, y0, r0 * s0, c); push(r1 * c1, y1, r1 * s1, c); push(r0 * c1, y0, r0 * s1, c);
        }
    }
    return new Float32Array(out);
}

/** A unit disc whose alpha falls from `centre` to 0 at the rim: the model's contact shadow. */
function shadowMesh(segments = 32, centre = 0.55) {
    const out = [];
    const push = (x, z, a) => out.push(x, 0, z, 0, 0, 0, a, 0, 0, -1);
    const rings = [[0, centre], [0.45, centre * 0.8], [0.75, centre * 0.4], [1, 0]];
    for (let i = 0; i < segments; i++) {
        const a0 = (i / segments) * Math.PI * 2, a1 = ((i + 1) / segments) * Math.PI * 2;
        for (let k = 0; k < rings.length - 1; k++) {
            const [r0, al0] = rings[k], [r1, al1] = rings[k + 1];
            push(r0 * Math.cos(a0), r0 * Math.sin(a0), al0); push(r1 * Math.cos(a0), r1 * Math.sin(a0), al1); push(r1 * Math.cos(a1), r1 * Math.sin(a1), al1);
            if (r0 > 0) { push(r0 * Math.cos(a0), r0 * Math.sin(a0), al0); push(r1 * Math.cos(a1), r1 * Math.sin(a1), al1); push(r0 * Math.cos(a1), r0 * Math.sin(a1), al0); }
        }
    }
    return new Float32Array(out);
}

/**
 * The shared WebGL renderer. Throws when WebGL (hardware) is unavailable; the caller then
 * uses drawModelSprite. models are {vertices, alpha, bounds}; animations {base, baseAlpha,
 * positions[], alphaPositions[], delays}, all Float32Arrays kept by the caller so a lost
 * context can re-upload them.
 */
export function createLootRenderer(canvas) {
    const gl = canvas.getContext('webgl', { alpha: true, premultipliedAlpha: true, antialias: true, depth: true, stencil: false, powerPreference: 'low-power', failIfMajorPerformanceCaveat: true });
    if (!gl) throw Error('WebGL is unavailable');
    let program = null, u = {}, a = {};
    let destroyed = false, lost = false;
    const models = new Map(); // key -> { src, opaque, alpha }
    const animations = new Map(); // key -> { src, base, baseAlpha, frames: [{opaque, alpha}] }
    const fixed = { pedestal: { data: pedestalMesh() }, shadow: { data: shadowMesh() } };
    const averages = new Float32Array(150).fill(0.5);
    const stats = { frames: 0, drawCalls: 0, triangles: 0, stages: 0, models: 0, animations: 0, gpuBytes: 0, uploads: 0, lost: 0, restored: 0 };
    const mvp = new Float32Array(16), model = new Float32Array(16);

    function shader(type, source) {
        const s = gl.createShader(type); gl.shaderSource(s, source); gl.compileShader(s);
        if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) { const m = gl.getShaderInfoLog(s); gl.deleteShader(s); throw Error(m); }
        return s;
    }
    function upload(data) {
        if (!data || !data.length) return null;
        const b = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, b); gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
        stats.uploads++;
        return b;
    }
    function uploadModel(m) {
        m.opaque = upload(m.src.vertices); m.alpha = upload(m.src.alpha);
    }
    function uploadAnimation(an) {
        an.base = upload(an.src.base); an.baseAlpha = upload(an.src.baseAlpha);
        an.frames = an.src.positions.map((p, i) => ({ opaque: upload(p), alpha: upload(an.src.alphaPositions[i]) }));
    }
    function initialize() {
        const v = shader(gl.VERTEX_SHADER, `
            attribute vec3 aPos; attribute vec3 aPos2; attribute vec4 aColour; attribute float aTex;
            uniform mat4 uMVP; uniform float uMix; uniform vec3 uTexAvg[50];
            varying vec4 vColour;
            void main() {
                gl_Position = uMVP * vec4(mix(aPos, aPos2, uMix), 1.0);
                vec3 c = aColour.rgb;
                if (aTex >= 0.0) c *= uTexAvg[int(aTex + 0.5)];
                vColour = vec4(c, aColour.a);
            }`);
        // Premultiplied out, to match the canvas (premultipliedAlpha) and blendFunc(ONE, ...).
        // Scaling a premultiplied (rgb*a, a) by uFade gives (rgb*(a*uFade), a*uFade), which is
        // the same colour at a lower alpha — exactly what a dissolve wants.
        const f = shader(gl.FRAGMENT_SHADER, `precision mediump float; varying vec4 vColour;
            uniform float uFade;
            void main() { gl_FragColor = vec4(vColour.rgb * vColour.a, vColour.a) * uFade; }`);
        program = gl.createProgram(); gl.attachShader(program, v); gl.attachShader(program, f); gl.linkProgram(program);
        gl.deleteShader(v); gl.deleteShader(f);
        if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw Error(gl.getProgramInfoLog(program));
        for (const n of ['uMVP', 'uMix', 'uTexAvg', 'uFade']) u[n] = gl.getUniformLocation(program, n);
        for (const n of ['aPos', 'aPos2', 'aColour', 'aTex']) a[n] = gl.getAttribLocation(program, n);
        for (const p of Object.values(fixed)) p.buffer = upload(p.data);
        for (const m of models.values()) uploadModel(m);
        for (const an of animations.values()) uploadAnimation(an);
    }
    function recount() {
        let bytes = 0;
        for (const m of models.values()) bytes += m.src.vertices.byteLength + m.src.alpha.byteLength;
        for (const an of animations.values()) {
            bytes += an.src.base.byteLength + an.src.baseAlpha.byteLength;
            for (let i = 0; i < an.src.positions.length; i++) bytes += an.src.positions[i].byteLength + an.src.alphaPositions[i].byteLength;
        }
        stats.gpuBytes = bytes; stats.models = models.size; stats.animations = animations.size;
    }
    function lostHandler(e) { e.preventDefault(); lost = true; stats.lost++; }
    function restoredHandler() {
        if (destroyed) return;
        lost = false; stats.restored++;
        // Every GL object died with the context; the source arrays are still here.
        initialize();
        onRestore?.();
    }
    let onRestore = null;
    canvas.addEventListener('webglcontextlost', lostHandler);
    canvas.addEventListener('webglcontextrestored', restoredHandler);
    initialize();

    // Bind colour/tex from `full` (stride 40) and positions from pos/pos2 (stride 12 when
    // they are position-only frame buffers, else the full buffer's own positions).
    function bind(full, pos, pos2) {
        gl.bindBuffer(gl.ARRAY_BUFFER, full);
        gl.vertexAttribPointer(a.aColour, 4, gl.FLOAT, false, STRIDE, 12);
        gl.vertexAttribPointer(a.aTex, 1, gl.FLOAT, false, STRIDE, 36);
        if (pos) { gl.bindBuffer(gl.ARRAY_BUFFER, pos); gl.vertexAttribPointer(a.aPos, 3, gl.FLOAT, false, 12, 0); }
        else gl.vertexAttribPointer(a.aPos, 3, gl.FLOAT, false, STRIDE, 0);
        if (pos2) { gl.bindBuffer(gl.ARRAY_BUFFER, pos2); gl.vertexAttribPointer(a.aPos2, 3, gl.FLOAT, false, 12, 0); }
        else { gl.bindBuffer(gl.ARRAY_BUFFER, pos || full); gl.vertexAttribPointer(a.aPos2, 3, gl.FLOAT, false, pos ? 12 : STRIDE, 0); }
    }

    return {
        kind: 'webgl',
        stats,
        get lost() { return lost || gl.isContextLost(); },
        set onRestore(fn) { onRestore = fn; },
        hasModel: key => models.has(key),
        hasAnimation: key => animations.has(key),
        setTextureAverages(rgbs) {
            for (let i = 0; i < 50; i++) { const c = rgbs[i] | 0; averages[i * 3] = ((c >> 16) & 255) / 255; averages[i * 3 + 1] = ((c >> 8) & 255) / 255; averages[i * 3 + 2] = (c & 255) / 255; }
        },
        setModel(key, src) {
            if (destroyed || models.has(key)) return;
            const m = { src, opaque: null, alpha: null };
            if (!lost) uploadModel(m);
            models.set(key, m); recount();
        },
        setAnimation(key, src) {
            if (destroyed || animations.has(key)) return;
            const an = { src, base: null, baseAlpha: null, frames: [] };
            if (!lost) uploadAnimation(an);
            animations.set(key, an); recount();
        },
        /**
         * Paint one frame. stages: [{model, anim, yaw, rect: {x, y, w, h} (CSS px, canvas-relative),
         * elapsed (ms of animation clock)}]. Returns false when nothing could be drawn.
         */
        draw(stages, dpr) {
            if (destroyed || lost || gl.isContextLost()) return false;
            const W = canvas.width, H = canvas.height;
            gl.viewport(0, 0, W, H);
            gl.disable(gl.SCISSOR_TEST);
            gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);
            gl.useProgram(program);
            gl.uniform3fv(u.uTexAvg, averages);
            gl.uniform1f(u.uFade, 1);
            gl.enableVertexAttribArray(a.aPos); gl.enableVertexAttribArray(a.aPos2);
            gl.enableVertexAttribArray(a.aColour); gl.enableVertexAttribArray(a.aTex);
            gl.enable(gl.SCISSOR_TEST); gl.enable(gl.DEPTH_TEST); gl.depthFunc(gl.LEQUAL);
            gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
            let calls = 0, tris = 0, drawn = 0;
            for (const st of stages) {
                // One stage, one pedestal, however many models stand on it (owner, 2026-09-20:
                // "when displaying multiple models on one panel, have them only sit on 1
                // pedestel"). A stage names either a single model or a list of placements; the
                // single case is normalised into a list of one at scale 1 in the middle, which
                // is exactly what it drew before.
                const places = (st.place ?? [{ model: st.model, anim: st.anim }])
                    .map(p => ({ ...p, m: models.get(p.model) }))
                    .filter(p => p.m);
                if (!places.length) continue;
                const x = Math.round(st.rect.x * dpr), w = Math.round(st.rect.w * dpr), h = Math.round(st.rect.h * dpr);
                const y = H - Math.round(st.rect.y * dpr) - h;
                if (w < 2 || h < 2 || x >= W || y >= H || x + w <= 0 || y + h <= 0) continue;
                gl.viewport(x, y, w, h); gl.scissor(x, y, w, h);
                gl.depthMask(true); gl.clear(gl.DEPTH_BUFFER_BIT);
                // Frame what this stage actually draws: each placement's sequence bounds while
                // one is playing, its bind pose otherwise. The plinth is sized from the bind
                // poses either way, so it does not resize the moment a sequence finishes
                // loading.
                for (const p of places) {
                    const an = p.anim ? animations.get(p.anim) : null;
                    p.an = an && an.frames.length && an.base ? an : null;
                    p.bind = p.m.src.bounds;
                    p.b = p.an && p.an.src.bounds ? p.an.src.bounds : p.bind;
                }
                const R = pedestalRadius(groupBounds(places, p => p.bind));
                const gb = groupBounds(places, p => p.b);
                const cam = stageCamera(gb, w / h, R);
                // Pedestal and its shadow stay still; only what stands on them turns.
                gl.disable(gl.CULL_FACE); gl.disable(gl.BLEND); gl.uniform1f(u.uMix, 0);
                gl.uniformMatrix4fv(u.uMVP, false, multiply(cam, scaleFlip(R, R, R, 0, model), mvp));
                bind(fixed.pedestal.buffer); gl.drawArrays(gl.TRIANGLES, 0, fixed.pedestal.data.length / FLOATS);
                calls++; tris += fixed.pedestal.data.length / FLOATS / 3;
                gl.enable(gl.BLEND); gl.depthMask(false);
                const sr = Math.min(R * 0.98, gb.foot * 1.15 + 0.08);
                gl.uniformMatrix4fv(u.uMVP, false, multiply(cam, scaleFlip(sr, 1, sr, 0.004, model), mvp));
                bind(fixed.shadow.buffer); gl.drawArrays(gl.TRIANGLES, 0, fixed.shadow.data.length / FLOATS);
                calls++; tris += fixed.shadow.data.length / FLOATS / 3;
                gl.disable(gl.BLEND); gl.depthMask(true);
                // st.fade dissolves the MODELS only; the plinth they stand on stays solid, so a
                // card cross-fading between two forms looks like one turntable changing what is
                // on it rather than the whole stage blinking. 1 (or absent) is the normal path,
                // byte for byte what it always did.
                const fade = st.fade === undefined ? 1 : st.fade;
                // Back faces culled, as the game's rasterizer culls them.
                gl.enable(gl.CULL_FACE); gl.cullFace(gl.BACK); gl.frontFace(gl.CCW);
                for (const p of places) {
                    const m = p.m, an = p.an, b = p.b, s = p.scale ?? 1;
                    gl.uniformMatrix4fv(u.uMVP, false,
                        multiply(cam, placeFlip(st.yaw, -Math.min(0, s * b.bottom), b.cx, b.cz, s, p.ox ?? 0, p.oz ?? 0, model), mvp));
                    let alphaFull = null, alphaPos = null, alphaPos2 = null, alphaCount = 0;
                    let opaqueBuf = null, opaquePos = null, opaquePos2 = null, opaqueCount = 0;
                    if (an) {
                        // A placement may carry its own clock: a card cycling an npc through its
                        // sequences restarts the clock at each one, so the new sequence begins at
                        // its first frame instead of wherever the card's own clock happens to be.
                        const f = loopFrame(an.src.delays, p.elapsed === undefined ? st.elapsed : p.elapsed);
                        const fr = an.frames[f.frame], nx = an.frames[f.next];
                        gl.uniform1f(u.uMix, f.mix);
                        opaqueBuf = an.base; opaquePos = fr.opaque; opaquePos2 = nx.opaque;
                        opaqueCount = an.src.base.length / FLOATS;
                        if (an.baseAlpha) { alphaFull = an.baseAlpha; alphaPos = fr.alpha; alphaPos2 = nx.alpha; alphaCount = an.src.baseAlpha.length / FLOATS; }
                    } else {
                        gl.uniform1f(u.uMix, 0);
                        if (m.opaque) { opaqueBuf = m.opaque; opaqueCount = m.src.vertices.length / FLOATS; }
                        if (m.alpha) { alphaFull = m.alpha; alphaCount = m.src.alpha.length / FLOATS; }
                    }
                    const drawOpaque = () => {
                        if (!opaqueBuf || !opaqueCount) return;
                        bind(opaqueBuf, opaquePos, opaquePos2); gl.drawArrays(gl.TRIANGLES, 0, opaqueCount);
                        calls++; tris += opaqueCount / 3;
                    };
                    if (fade >= 0.996) {
                        drawOpaque();
                    } else if (fade > 0.004) {
                        // A depth prepass with colour masked off, then the blended pass at
                        // depthFunc EQUAL: every visible fragment is drawn exactly once, so a
                        // concave model does not blend into itself and the dissolve stays clean.
                        // Two draws, and only while a card is actually mid-transition.
                        gl.colorMask(false, false, false, false);
                        drawOpaque();
                        gl.colorMask(true, true, true, true);
                        gl.depthFunc(gl.EQUAL); gl.depthMask(false); gl.enable(gl.BLEND);
                        gl.uniform1f(u.uFade, fade);
                        drawOpaque();
                        gl.uniform1f(u.uFade, 1);
                        gl.disable(gl.BLEND); gl.depthMask(true); gl.depthFunc(gl.LEQUAL);
                    }
                    if (alphaFull && alphaCount && fade > 0.004) {
                        gl.enable(gl.BLEND); gl.depthMask(false);
                        if (fade < 0.996) gl.uniform1f(u.uFade, fade);
                        bind(alphaFull, alphaPos, alphaPos2); gl.drawArrays(gl.TRIANGLES, 0, alphaCount);
                        calls++; tris += alphaCount / 3;
                        gl.uniform1f(u.uFade, 1);
                        gl.disable(gl.BLEND); gl.depthMask(true);
                    }
                }
                drawn++;
            }
            gl.disable(gl.SCISSOR_TEST);
            stats.frames++; stats.drawCalls = calls; stats.triangles = tris; stats.stages = drawn;
            return true;
        },
        /** Clear to transparent (nothing on screen, or everything switched to icons). */
        clear() {
            if (destroyed || lost || gl.isContextLost()) return;
            gl.disable(gl.SCISSOR_TEST); gl.viewport(0, 0, canvas.width, canvas.height);
            gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);
        },
        destroy() {
            if (destroyed) return;
            destroyed = true;
            canvas.removeEventListener('webglcontextlost', lostHandler);
            canvas.removeEventListener('webglcontextrestored', restoredHandler);
            if (!gl.isContextLost()) {
                const del = b => { if (b) gl.deleteBuffer(b); };
                for (const p of Object.values(fixed)) del(p.buffer);
                for (const m of models.values()) { del(m.opaque); del(m.alpha); }
                for (const an of animations.values()) { del(an.base); del(an.baseAlpha); for (const fr of an.frames) { del(fr.opaque); del(fr.alpha); } }
                gl.deleteProgram(program);
            }
            models.clear(); animations.clear();
            // Hand the context back now rather than waiting for garbage collection: the page's
            // other GL panels share the browser's context budget.
            gl.getExtension('WEBGL_lose_context')?.loseContext();
        }
    };
}

// ---- Canvas 2D fallback: one still pose, painted once into a card's own small canvas ----

function project(M, x, y, z, W, H) {
    const cx = M[0] * x + M[4] * y + M[8] * z + M[12], cy = M[1] * x + M[5] * y + M[9] * z + M[13];
    const cz = M[2] * x + M[6] * y + M[10] * z + M[14], cw = M[3] * x + M[7] * y + M[11] * z + M[15];
    return [(cx / cw * 0.5 + 0.5) * W, (1 - (cy / cw * 0.5 + 0.5)) * H, cz / cw];
}

function rgb(r, g, b, a = 1) {
    return `rgba(${Math.round(Math.min(1, r) * 255)},${Math.round(Math.min(1, g) * 255)},${Math.round(Math.min(1, b) * 255)},${a})`;
}

/**
 * Paint one model on its pedestal into a 2D canvas (sized by the caller to CSS size x dpr).
 * Flat shading from the three vertex colours' average, back faces culled by screen winding,
 * triangles sorted far to near (painter's algorithm). textureAverages: 50 packed RGB ints.
 */
export function drawModelSprite(canvas, src, yaw, textureAverages) {
    const ctx = canvas.getContext('2d');
    if (!ctx) return 0;
    const W = canvas.width, H = canvas.height;
    ctx.clearRect(0, 0, W, H);
    const b = src.bounds, R = pedestalRadius(b), cam = stageCamera(b, W / H, R);
    const tex = i => { const c = textureAverages?.[i] ?? 0x808080; return [((c >> 16) & 255) / 255, ((c >> 8) & 255) / 255, (c & 255) / 255]; };
    const tris = [];
    const collect = (data, M, layer, cull) => {
        for (let i = 0; i < data.length; i += FLOATS * 3) {
            const p = [0, 1, 2].map(k => project(M, data[i + k * FLOATS], data[i + k * FLOATS + 1], data[i + k * FLOATS + 2], W, H));
            // Screen winding, y down: the flip makes a front face clockwise here.
            const area = (p[1][0] - p[0][0]) * (p[2][1] - p[0][1]) - (p[1][1] - p[0][1]) * (p[2][0] - p[0][0]);
            if (cull && area >= 0) continue;
            let r = 0, g = 0, bl = 0, al = 0;
            for (let k = 0; k < 3; k++) {
                const o = i + k * FLOATS, t = data[o + 9];
                const c = t >= 0 ? tex(t | 0) : [1, 1, 1];
                r += data[o + 3] * c[0]; g += data[o + 4] * c[1]; bl += data[o + 5] * c[2]; al += data[o + 6];
            }
            tris.push({ p, z: (p[0][2] + p[1][2] + p[2][2]) / 3, layer, fill: rgb(r / 3, g / 3, bl / 3, al / 3) });
        }
    };
    collect(pedestalMesh(24), multiply(cam, scaleFlip(R, R, R, 0)), 0, false);
    // The contact shadow as one soft ellipse, from the projected disc's centre and rim.
    const sr = Math.min(R * 0.98, b.foot * 1.15 + 0.08);
    const S = multiply(cam, scaleFlip(1, 1, 1, 0.004));
    const c0 = project(S, 0, 0, 0, W, H), cx = project(S, sr, 0, 0, W, H), cz = project(S, 0, 0, sr, W, H);
    collect(src.vertices, multiply(cam, spinFlip(yaw, baseLift(b), b.cx, b.cz)), 2, true);
    collect(src.alpha, multiply(cam, spinFlip(yaw, baseLift(b), b.cx, b.cz)), 2, true);
    tris.sort((p, q) => p.layer - q.layer || q.z - p.z);
    let shadowDone = false;
    for (const t of tris) {
        if (t.layer === 2 && !shadowDone) {
            shadowDone = true;
            const rx = Math.abs(cx[0] - c0[0]), ry = Math.max(1, Math.abs(cz[1] - c0[1]));
            ctx.save(); ctx.translate(c0[0], c0[1]); ctx.scale(1, ry / Math.max(1, rx));
            const g = ctx.createRadialGradient(0, 0, 0, 0, 0, rx);
            g.addColorStop(0, 'rgba(0,0,0,0.5)'); g.addColorStop(0.6, 'rgba(0,0,0,0.25)'); g.addColorStop(1, 'rgba(0,0,0,0)');
            ctx.fillStyle = g; ctx.beginPath(); ctx.arc(0, 0, rx, 0, Math.PI * 2); ctx.fill(); ctx.restore();
        }
        ctx.beginPath(); ctx.moveTo(t.p[0][0], t.p[0][1]); ctx.lineTo(t.p[1][0], t.p[1][1]); ctx.lineTo(t.p[2][0], t.p[2][1]); ctx.closePath();
        ctx.fillStyle = t.fill; ctx.fill();
        // A hairline of the same colour closes the anti-aliasing seams between neighbours.
        ctx.strokeStyle = t.fill; ctx.lineWidth = 0.6; ctx.stroke();
    }
    return tris.length;
}
