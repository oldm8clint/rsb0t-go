// Combat effects drawn over the GPU scene on the labels canvas: the health bar, hitsplats that
// rise and fade, and the blood (or block / poison) spray of a landed hit. Two dimensional, in the
// native game-window pixels (512 wide; the caller scales its 2D context to the view); the caller has projected the world points. No DOM, no globals, no per-frame
// allocation: the particles live in fixed typed arrays and targets are plain scalars, so it runs
// at the stream view's frame rate beside a large fleet (docs/control_panel_cpu.md).
//
// What is real and what is made up. Hits, their damage and type, and health come from the game's
// own PLAYER_INFO/NPC_INFO hitmark blocks (internal/gamestate/effects.go). The rising and fading, the
// pop-in and the particles are this view's own flourish: the 2004 client draws the splat in place
// for 70 client cycles and nothing else.

const POOL = 128;
const HIT_CYCLES = 70;          // how long a hitsplat lives (the game's own figure)
const BAR_CYCLES = 300;         // and a health bar after the last hit
const FRESH_CYCLES = 12;        // a hit seen this recently is new: it sprays
const GRAVITY = 380;            // native px/s^2
const KIND_COLOUR = ['#c4121a', '#5aa0ff', '#4ec24e', '#e8e8e8'];  // blood, block, poison, other
const OFFSETS = [[0, 0], [0, -20], [-15, -10], [15, -10]];          // the game's four hit slots

// The kind of spray a hit type makes: 1 damage is blood, 0 a block (blue), 2 poison (green).
const kindOf = (type, damage) => (damage > 0 && type === 1 ? 0 : type === 2 ? 2 : damage > 0 ? 3 : 1);

export class CombatFx {
    constructor() {
        this.x = new Float32Array(POOL); this.y = new Float32Array(POOL);
        this.vx = new Float32Array(POOL); this.vy = new Float32Array(POOL);
        this.age = new Float32Array(POOL); this.life = new Float32Array(POOL); this.size = new Float32Array(POOL);
        this.kind = new Uint8Array(POOL);
        this.count = 0;
        this.lastMs = 0;
    }

    // Per-target memory, kept on the entity's motion record: the last tick each hit slot was seen
    // (to spray once per hit) and the smoothed health bar.
    static state() { return { ticks: [0, 0, 0, 0], bar: -1 }; }

    active() { return this.count > 0; }
    clear() { this.count = 0; }

    spawn(x, y, kind, damage) {
        const n = kind === 0 ? 9 + Math.min(damage, 10) : kind === 1 ? 8 : 7;
        for (let i = 0; i < n && this.count < POOL; i++) {
            const k = this.count++;
            const angle = -Math.PI / 2 + (Math.random() - 0.5) * 2.1;       // a fan, upwards
            const speed = (35 + Math.random() * 90) * (kind === 1 ? 0.7 : 1);
            this.x[k] = x + (Math.random() - 0.5) * 6; this.y[k] = y;
            this.vx[k] = Math.cos(angle) * speed; this.vy[k] = Math.sin(angle) * speed;
            this.age[k] = 0; this.life[k] = 380 + Math.random() * 320;
            this.size[k] = kind === 1 ? 1.5 : 1.8 + Math.random() * 1.8; this.kind[k] = kind;
        }
    }

    // begin starts a frame: it advances the particles to nowMs.
    begin(nowMs) {
        const dt = this.lastMs ? Math.min(0.05, (nowMs - this.lastMs) / 1000) : 0;
        this.lastMs = nowMs;
        for (let i = 0; i < this.count;) {
            this.age[i] += dt * 1000;
            if (this.age[i] >= this.life[i]) { this.remove(i); continue; }
            this.vy[i] += GRAVITY * dt;
            this.x[i] += this.vx[i] * dt; this.y[i] += this.vy[i] * dt;
            i++;
        }
        if (!this.count) this.lastMs = 0;
    }

    remove(i) {
        const last = --this.count;
        if (i === last) return;
        for (const a of [this.x, this.y, this.vx, this.vy, this.age, this.life, this.size, this.kind]) a[i] = a[last];
    }

    // target draws one entity's health bar and hitsplats. head and centre are projected points
    // [x, y] in canvas pixels; cycles(tick) is the age in client cycles of something that happened on
    // that game tick; sprites/font are the game's own hitmark art and bitmap font.
    target(g, combat, st, headX, headY, centreX, centreY, cycles, sprites, font) {
        if (!combat) return;
        const age = cycles(combat.tick);
        if (combat.seen && age < BAR_CYCLES && combat.total > 0 && headY === headY) {
            const want = Math.max(0, Math.min(1, combat.health / combat.total));
            st.bar = st.bar < 0 ? want : st.bar + (want - st.bar) * 0.35;
            const w = 32, h = 5, x = Math.round(headX - w / 2), y = Math.round(headY - h / 2), fill = Math.round(w * st.bar);
            g.fillStyle = 'rgba(0,0,0,.85)'; g.fillRect(x - 1, y - 1, w + 2, h + 2);
            g.fillStyle = '#c01414'; g.fillRect(x, y, w, h);
            g.fillStyle = '#26d426'; g.fillRect(x, y, fill, h);
            g.fillStyle = 'rgba(255,255,255,.28)'; g.fillRect(x, y, fill, 1);
        }
        const hits = combat.hits;
        if (!hits || centreY !== centreY) return;
        for (let i = 0; i < hits.length; i++) {
            const hit = hits[i];
            if (!hit.seen) continue;
            const t = cycles(hit.tick);
            if (t >= HIT_CYCLES) continue;
            const ox = OFFSETS[i][0], oy = OFFSETS[i][1];
            if (hit.tick !== st.ticks[i]) {
                st.ticks[i] = hit.tick;
                if (t < FRESH_CYCLES) this.spawn(centreX + ox, centreY + oy, kindOf(hit.type, hit.damage), hit.damage);
            }
            const sprite = sprites[hit.type];
            if (!sprite) continue;
            // Rise a few pixels, pop in slightly large, and fade over the last 20 cycles.
            const rise = Math.min(t, 40) / 40 * 10, pop = 1 + Math.max(0, 8 - t) / 8 * 0.3;
            const alpha = t > 50 ? (HIT_CYCLES - t) / 20 : 1;
            const w = sprite.width * pop, h = sprite.height * pop, cx = centreX + ox, cy = centreY + oy - rise;
            g.globalAlpha = alpha;
            g.drawImage(sprite, cx - w / 2, cy - h / 2, w, h);
            if (font) {
                g.save(); g.translate(cx, cy); g.scale(pop, pop);
                const text = String(hit.damage), left = -Math.floor(font.width(text) / 2);
                font.draw(g, text, left, 4, '#000'); font.draw(g, text, left - 1, 3);
                g.restore();
            }
            g.globalAlpha = 1;
        }
    }

    // flush draws the particles, one path per kind.
    flush(g) {
        if (!this.count) return;
        for (let kind = 0; kind < KIND_COLOUR.length; kind++) {
            g.fillStyle = KIND_COLOUR[kind]; g.beginPath();
            let any = false;
            for (let i = 0; i < this.count; i++) {
                if (this.kind[i] !== kind) continue;
                const r = this.size[i] * (1 - 0.55 * this.age[i] / this.life[i]);
                g.moveTo(this.x[i] + r, this.y[i]); g.arc(this.x[i], this.y[i], r, 0, 6.2832); any = true;
            }
            if (any) g.fill();
        }
    }
}
