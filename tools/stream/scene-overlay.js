// The 2D layer over the GPU scene (the labels canvas): the tile grid, ground item labels, combat
// effects (health bars, hitsplats, spray: scene-combat.js) and the name tags. One call per paint.
// It knows nothing of the provider: everything it reads comes in the `frame` object the caller
// owns and refreshes (no allocation per paint beyond the sort of the labelled names).

import { CombatFx } from './scene-combat.js';

const MAX_LABELS = 24;   // nearest named entities labelled per frame
const NAME_COLOURS = { npc: '#ffff00', player: '#ffffff', self: '#5ad0ff' };

// A second view of the world per call: points are projected into these (x, y, depth) scratch arrays.
const head = new Float64Array(3), centre = new Float64Array(3), point = new Float64Array(3);

export function createOverlay() {
    const fx = new CombatFx();
    const labelled = [];
    return {
        fx,
        // draw paints one frame. frame: {
        //   canvas, dpr, now, scale (canvas px per native px; 1 outside the game window),
        //   project(out, x, y, z) -> bool, cycles(tick) -> client cycles since a game tick,
        //   placed [{e, cx, cz, y, top, kind, motion}] and placedCount, self {x, z, y, top, name, combat, motion},
        //   objs, itemNames, heightAt, toggles {grid, items, names, distances}, gameFrame,
        //   sprites, font (the game's hitmark art and p11 font), maxLabels }
        draw(frame) {
            const c = frame.canvas, dpr = frame.dpr;
            const w = Math.round(c.clientWidth * dpr), h = Math.round(c.clientHeight * dpr);
            if (c.width !== w || c.height !== h) { c.width = w; c.height = h; }
            const g = c.getContext('2d');
            g.setTransform(dpr, 0, 0, dpr, 0, 0); g.clearRect(0, 0, c.clientWidth, c.clientHeight);
            const tg = frame.toggles || {}, project = frame.project;
            if (tg.grid && frame.heightAt) drawGrid(g, frame);
            if (tg.items) drawItems(g, frame, tg);
            // Combat overlays stay visible independently of the diagnostic name tags. They draw in the
            // game window's native units, so the context is scaled and projected points scaled back.
            const scale = frame.scale, s = frame.self;
            fx.begin(frame.now);
            g.save(); g.scale(scale, scale); g.imageSmoothingEnabled = false;
            for (let i = 0; i < frame.placedCount; i++) {
                const p = frame.placed[i];
                combatOf(g, frame, p.e.combat, p.motion, p.cx, p.y, p.cz, p.top || p.kind.height);
            }
            combatOf(g, frame, s.combat, s.motion, s.x + 0.5, s.y, s.z + 0.5, s.top);
            fx.flush(g);
            g.restore();
            // The game window shows no name tags over entities: only on request (N), or in the DOM layout.
            if (frame.gameFrame && !tg.names) return;
            g.font = '11px Arial, sans-serif'; g.textAlign = 'center'; g.lineWidth = 3; g.strokeStyle = '#000';
            labelled.length = 0;
            for (let i = 0; i < frame.placedCount; i++) { const p = frame.placed[i]; p.d = Math.hypot(p.cx - s.x - 0.5, p.cz - s.z - 0.5); labelled.push(p); }
            labelled.sort((a, b) => a.d - b.d); labelled.length = Math.min(labelled.length, frame.maxLabels || MAX_LABELS);
            for (const p of labelled) {
                const text = (p.e.n || (p.e.k[0] === 'n' ? `npc ${p.e.t}` : 'player')) + (tg.distances ? ` · ${Math.ceil(Math.max(Math.abs(p.cx - s.x - .5), Math.abs(p.cz - s.z - .5)))} tiles` : '');
                if (!project(point, p.cx, p.y + (p.top || p.kind.height) + 0.35, p.cz)) continue;
                g.fillStyle = p.kind.npc ? NAME_COLOURS.npc : NAME_COLOURS.player;
                g.strokeText(text, point[0], point[1]); g.fillText(text, point[0], point[1]);
            }
            if (project(point, s.x + 0.5, s.y + s.top + 0.35, s.z + 0.5)) {
                g.fillStyle = NAME_COLOURS.self; g.strokeText(s.name, point[0], point[1]); g.fillText(s.name, point[0], point[1]);
            }
        },
    };

    // One entity's combat effects: the head and centre points projected, drawn in native units.
    function combatOf(g, frame, combat, motion, x, y, z, top) {
        if (!combat || !motion) return;
        const st = motion.fx ||= CombatFx.state(), project = frame.project, k = 1 / frame.scale;
        const ok = project(head, x, y + top + 15 / 128, z), ok2 = project(centre, x, y + top / 2, z);
        fx.target(g, combat, st, ok ? head[0] * k : NaN, ok ? head[1] * k : NaN, ok2 ? centre[0] * k : NaN, ok2 ? centre[1] * k : NaN, frame.cycles, frame.sprites, frame.font);
    }
}

function drawGrid(g, f) {
    // Tile grid on the decoded ground, 12 tiles around the account.
    g.strokeStyle = 'rgba(255,255,255,.28)'; g.lineWidth = 1; g.beginPath();
    const cx = Math.round(f.self.x), cz = Math.round(f.self.z), R = 12;
    for (let i = -R; i <= R + 1; i++) {
        for (let axis = 0; axis < 2; axis++) {
            let prev = false, px = 0, py = 0;
            for (let t = 0; t <= 1.0001; t += 1 / (2 * R + 1)) {
                const x = axis ? cx - R + t * (2 * R + 1) : cx + i, z = axis ? cz + i : cz - R + t * (2 * R + 1);
                const ok = f.project(point, x, (f.heightAt(x, z) ?? 0) + 0.02, z);
                if (ok && prev) { g.moveTo(px, py); g.lineTo(point[0], point[1]); }
                prev = ok; px = point[0]; py = point[1];
            }
        }
    }
    g.stroke();
}

function drawItems(g, f, tg) {
    g.font = '10px Arial, sans-serif'; g.textAlign = 'center'; g.fillStyle = '#ff9040'; g.lineWidth = 3; g.strokeStyle = '#000';
    const objs = f.objs || [], n = Math.min(objs.length, MAX_LABELS);
    for (let i = 0; i < n; i++) {
        const [x, z, id, count] = objs[i];
        if (!f.project(point, x + 0.5, (f.heightAt?.(x + 0.5, z + 0.5) ?? 0) + 0.3, z + 0.5)) continue;
        const text = (f.itemNames?.[id] || `item ${id}`) + (count > 1 ? ` (${count})` : '') + (tg.distances ? ` · ${Math.ceil(Math.max(Math.abs(x - f.self.x), Math.abs(z - f.self.z)))} tiles` : '');
        g.strokeText(text, point[0], point[1]); g.fillText(text, point[0], point[1]);
    }
}
