// Pure scene state for the stream client: no DOM, no WebGL, no fetch. Node-importable so
// the apply/interpolate/height rules are testable without a browser
// (tools/stream/verify-scene-state.mjs).
//
// A scene is exactly what internal/controlpanel/streamscene.go projects server-side:
//   { tick, self:{x,z,level,centreX,centreZ,slot,name}, entities:[{k,x,z,t,s,n}],
//     inv:[[id,count],...], chat:[{type,user,text}], status:{bot,activity,hp,maxHp,run,...} }
// It arrives whole (frame.scene) or as a delta (frame.delta) on the "scene-delta"
// transport. An older server that only knows "full-snapshot" sends a whole
// gamestate.RenderSnapshot per frame instead; sceneFromSnapshot projects that the same way
// the server would, minus npc names (the server's config archive is not in the browser).

export const BACKPACK = 3214;
const CHAT_LINES = 5;
const MAX_ENTITIES = 255;

// Movement follows Client.ts routeMove: each observed tile joins the entity's route queue and
// the entity walks along it at 4 fine units (of 128 per tile) per 20 ms client cycle — one
// tile per 640 ms, STEP_MS — faster when the queue backs up (6 with more than two queued
// points, 8 with more than three) and twice as fast on a run step. The protocol's run flag is
// not kept by the session, so a step is taken to be a run when the account advanced two tiles
// within about one game tick (a missed observation that also shows two tiles, but over a
// longer gap, stays a walk).
export const STEP_MS = 640;
// A move longer than this in one observation, and longer than two tiles per tick elapsed,
// is a teleport (or a re-entry into view); it snaps instead of walking across the map.
export const SNAP_TILES = 4;
const TURN_SPEED = 32; // yaw units per cycle (ClientEntity.turnspeed default)
// Playout delay (at the live tick). The game client receives a position every tick in step
// with the server; this view polls, so observations arrive unevenly (a slow reply, a missed
// tick). Each observed tile is played at its SERVER capture time plus the smallest recent
// capture-to-arrival delay plus a hold, which puts the tiles back on the server's even
// rhythm before routeMove walks them, instead of letting the queue run dry mid-walk and
// flash the ready pose. The hold is HOLD_MS (scaled by the tick) or the observed arrival
// spread, whichever is longer. It delays the view by about half a second, not the account.
export const HOLD_MS = 500;

// sceneFromSnapshot projects a full-snapshot frame's gamestate.RenderSnapshot. NPC and
// LocInstance have no json tags in internal/gamestate (X/Z), RenderSnapshot/RenderPlayer
// do (x/z); both casings are read.
export function sceneFromSnapshot(s) {
    const self = { x: s.x, z: s.z, level: s.level, centreX: s.centreX, centreZ: s.centreZ, slot: s.selfSlot, name: s.ui?.name || '', combat: s.combat };
    const entities = [];
    for (const n of s.npcs || []) entities.push({ k: 'n' + (n.Index ?? n.index), x: n.X ?? n.x, z: n.Z ?? n.z, t: n.Type ?? n.type, combat: n.Combat ?? n.combat });
    for (const p of s.players || []) if (p.index !== s.selfSlot) entities.push({ k: 'p' + p.index, x: p.x, z: p.z, n: p.name || '', combat: p.combat });
    if (entities.length > MAX_ENTITIES) {
        const d = e => Math.max(Math.abs(e.x - s.x), Math.abs(e.z - s.z));
        entities.sort((a, b) => d(a) - d(b) || (a.k < b.k ? -1 : 1)).length = MAX_ENTITIES;
    }
    entities.sort((a, b) => (a.k < b.k ? -1 : a.k > b.k ? 1 : 0));
    const slots = s.ui?.inventories?.[BACKPACK]?.Slots || [];
    const chat = (s.ui?.chat || []).slice(-CHAT_LINES).map(l => ({ type: l.type, user: l.username || '', text: l.text }));
    // Go encodes []byte as base64 in JSON, which is exactly the scene-delta looks format.
    const looks = {};
    if (s.appearance) looks.self = s.appearance;
    const kept = new Set(entities.map(e => e.k));
    for (const p of s.players || []) if (p.appearance && kept.has('p' + p.index)) looks['p' + p.index] = p.appearance;
    const objs = (s.objs || []).filter(o => (o.Level ?? 0) === s.level && o.Count > 0).map(o => [o.X, o.Z, o.ID, o.Count]).sort((a, b) => a[0] - b[0] || a[1] - b[1] || a[2] - b[2] || a[3] - b[3]);
    return { tick: s.ui?.tick || 0, self, entities, inv: slots.map(it => (it.Count > 0 ? [it.ID, it.Count] : [0, 0])), chat,
        status: { run: s.ui?.runEnergy, weight: s.ui?.weight }, looks, objs, effects: s.effects || [], ui: s.ui || null };
}

function sameSelf(a, b) {
    return a && b && a.x === b.x && a.z === b.z && a.level === b.level && a.centreX === b.centreX && a.centreZ === b.centreZ;
}

// SceneState holds the current scene, applies whole scenes and deltas, and keeps each
// entity's eased position between observed tiles.
export class SceneState {
    constructor() {
        this.tickScale = 1;
        this.scene = null;
        this.seq = 0;
        // key -> the motion record newMotion() below builds: {e, x, z, obsX, obsZ, route, ...}.
        // x/z are the EASED tile position (what a renderer draws and what the game's own
        // minimap dot should use); obsX/obsZ are the last tile the server actually reported.
        // There is no toX/toZ and never has been — reading one yields undefined, which is
        // how every entity in the adapter's interface snapshot ended up on scene tile 0,0.
        this.entities = new Map();
        this.selfMotion = null;
        this.invDirty = new Set();
        this.invAll = true;
        this.chatDirty = true;
        this.statusDirty = true;
    }

    // applyFrame applies one observation. Returns what changed. Throws on a delta whose base
    // this state does not hold — the caller must then ask for a whole scene (have=0).
    applyFrame(frame, now) {
        this.clock(frame.scene?.tick ?? frame.tick ?? frame.snapshot?.ui?.tick, frame.capturedAt, now);
        if (frame.scene) return this.applyScene(frame.scene, frame.seq, now);
        if (frame.delta) {
            if (!this.scene || frame.delta.base !== this.seq) throw Error(`delta on base ${frame.delta.base}, holding ${this.seq}`);
            return this.applyDelta(frame.delta, frame.seq, frame.tick, now);
        }
        if (frame.snapshot) return this.applyScene(sceneFromSnapshot(frame.snapshot), frame.seq, now);
        throw Error('frame carries no scene, delta or snapshot');
    }

    // clock sets frameAt, the server time an observation belongs to: its game tick (the
    // session's PLAYER_INFO count) times the tick length when the frame carries one, else the
    // server capture time, else its arrival. Movement is played on that clock (observe), so
    // tiles walk on the server's even rhythm however unevenly the polls arrive, and the speed
    // that decides a run is measured on it. offset maps it to the local clock: the smallest
    // arrival delay over the last ~30 observations (the least delayed is the closest).
    clock(tick, capturedAt, now) {
        const tickMs = 600 / (this.tickScale || 1);
        let at = Number.isFinite(capturedAt) ? capturedAt : now, kind = 'capture';
        if (Number.isFinite(tick) && tick > 0) { at = tick * tickMs; kind = 'tick' + tickMs; }
        // A new clock basis (the tick length became known, a reconnect restarted the tick
        // count) starts the offset estimate afresh.
        if (kind !== this.clockKind || (kind !== 'capture' && at < this.frameAt)) this.offsets = [];
        this.clockKind = kind;
        this.frameAt = at;
        // Only the first observation of a tick measures its delay (later polls in the same
        // tick see the same positions, later).
        if (kind === 'capture' || at !== this.lastAt || !this.offsets?.length) {
            this.offsets = (this.offsets || []).slice(-29); this.offsets.push(now - at);
            const sorted = [...this.offsets].sort((a, b) => a - b);
            this.offset = sorted[0];
            // Hold: long enough for nine in ten observations to arrive before they play.
            const spread = sorted[Math.floor(sorted.length * 0.9)] - sorted[0];
            // It grows by at most 20 ms (one client cycle) per tick, so a longer hold never
            // opens a visible gap in a route already walking.
            const want = Math.max(HOLD_MS / (this.tickScale || 1), spread + 50);
            this.hold = this.hold === undefined || want < this.hold ? want : Math.min(want, this.hold + 20);
        }
        this.lastAt = at;
    }

    applyScene(scene, seq, now) {
        const prevSelf = this.scene?.self;
        const seen = new Set();
        for (const e of scene.entities) { this.moveEntity(e, now); seen.add(e.k); }
        for (const k of [...this.entities.keys()]) if (!seen.has(k)) this.entities.delete(k);
        this.moveSelf(scene.self, now);
        this.scene = { ...scene, entities: undefined, looks: { ...(scene.looks || {}) }, objs: scene.objs || [] };
        this.seq = seq;
        this.invAll = true; this.chatDirty = true; this.statusDirty = true;
        return { full: true, self: !sameSelf(prevSelf, scene.self) };
    }

    applyDelta(d, seq, tick, now) {
        const s = this.scene;
        let self = false;
        if (d.self) { self = !sameSelf(s.self, d.self); s.self = d.self; this.moveSelf(d.self, now); }
        for (const e of d.upsert || []) this.moveEntity(e, now);
        for (const k of d.remove || []) { this.entities.delete(k); delete s.looks[k]; }
        if (d.looks) Object.assign(s.looks, d.looks);
        if (d.ui) s.ui = applyUiDelta(s.ui, d.ui);
        if (d.objs) s.objs = d.objs;
        if (d.effects) s.effects = d.effects;
        if (d.invLen !== undefined && d.invLen !== null) {
            s.inv = s.inv.slice(0, d.invLen);
            while (s.inv.length < d.invLen) s.inv.push([0, 0]);
            this.invAll = true;
        }
        for (const [slot, id, count] of d.inv || []) {
            if (slot < 0 || slot >= s.inv.length) throw Error(`inventory slot ${slot} outside ${s.inv.length}`);
            s.inv[slot] = count > 0 ? [id, count] : [0, 0];
            this.invDirty.add(slot);
        }
        if (d.chat) { s.chat = d.chat; this.chatDirty = true; }
        if (d.status) { s.status = d.status; this.statusDirty = true; }
        if (tick) s.tick = tick;
        this.seq = seq;
        return { full: false, self };
    }

    moveEntity(e, now) {
        // The server omits a zero npc type (json omitempty): npc type 0 is a real npc (Hans).
        if (e.k[0] === 'n' && e.t === undefined) e.t = 0;
        const had = this.entities.get(e.k);
        if (!had) { this.entities.set(e.k, newMotion(e.x, e.z, now, e)); return; }
        if ((e.a || 0) !== (had.e.a || 0) || (e.at || 0) !== (had.e.at || 0)) had.primaryStart = now;
        had.tickScale = this.tickScale;
        observe(had, e.x, e.z, now, this.frameAt ?? now, (this.offset ?? 0) + (this.hold ?? HOLD_MS / (this.tickScale || 1)));
        had.e = e;
    }

    moveSelf(self, now) {
        const m = this.selfMotion;
        if (!m) { this.selfMotion = Object.assign(newMotion(self.x, self.z, now, null), { a: self.a || 0, at: self.at || 0 }); return; }
        if ((self.a || 0) !== m.a || (self.at || 0) !== m.at) { m.primaryStart = now; m.a = self.a || 0; m.at = self.at || 0; }
        m.tickScale = this.tickScale;
        observe(m, self.x, self.z, now, this.frameAt ?? now, (this.offset ?? 0) + (this.hold ?? HOLD_MS / (this.tickScale || 1)));
    }

    // advance runs every entity's route forward to `now` (the client's per-cycle routeMove
    // and turning). faceOf(motion) may return a yaw the entity should face while it moves or
    // stands (its interaction target), as Client.ts's entityFace does.
    advance(now, faceOf) {
        if (this.selfMotion) step(this.selfMotion, now, faceOf);
        for (const m of this.entities.values()) step(m, now, faceOf);
    }

    // selfAt/entitiesAt return positions in TILE coordinates (a tile's own coordinate, not its
    // centre), after advancing to `now`.
    selfAt(now) { if (!this.selfMotion) return [0, 0]; step(this.selfMotion, now); return [this.selfMotion.x, this.selfMotion.z]; }

    // animating is true while anything is walking or turning: the only time the view needs
    // painting without a new observation or a camera change.
    animating(now) {
        if (this.selfMotion && busy(this.selfMotion)) return true;
        for (const m of this.entities.values()) if (busy(m)) return true;
        return false;
    }

    *entitiesAt(now) {
        for (const m of this.entities.values()) {
            step(m, now);
            yield { e: m.e, x: m.x, z: m.z, yaw: m.yaw, motion: m };
        }
    }

    takeInvChanges() {
        const all = this.invAll, slots = [...this.invDirty];
        this.invAll = false; this.invDirty.clear();
        return { all, slots };
    }
}

// applyUiDelta merges an interface delta (internal/controlpanel/streamui.go): scalar fields
// replace, map sections merge per key with null deleting the key.
export function applyUiDelta(ui, d) {
    const out = { ...(ui || {}) };
    for (const k of ['version', 'name', 'main', 'side', 'chatModal', 'skills', 'chat', 'runEnergy', 'weight', 'countDialog', 'overlay', 'playerOps', 'playerOpPriority']) if (d[k] !== undefined) out[k] = d[k];
    for (const k of ['inventories', 'texts', 'colours', 'varps', 'sideIcons', 'objects',
                     'heads', 'anims', 'hidden', 'scrollPos', 'positions']) {
        if (!d[k]) continue;
        const m = { ...(out[k] || {}) };
        for (const [key, v] of Object.entries(d[k])) { if (v === null) delete m[key]; else m[key] = v; }
        out[k] = m;
    }
    return out;
}

// roofTop ports Client.ts's roofCheck: the highest level to draw. 3 (everything) unless the
// account's own tile, or — when the camera is lower than pitch 310 — the camera's tile or any
// tile on the client's stepped line from the camera to the account, carries RemoveRoof
// (0x4) on the account's level; then only levels up to the account's are drawn, which hides
// roofs and upper floors when the account is inside. flags: 104 x 104, index x * 104 + z in
// the region's local tiles from base. rsPitch is the camera pitch in client units (2048 per
// turn).
export function roofTop(flags, base, level, selfX, selfZ, eyeX, eyeZ, rsPitch) {
    if (!flags) return 3;
    const f = (x, z) => {
        const lx = x - base.x, lz = z - base.z;
        return lx >= 0 && lz >= 0 && lx < 104 && lz < 104 && flags[lx * 104 + lz] !== 0;
    };
    let top = 3;
    if (rsPitch < 310) {
        let cx = Math.floor(eyeX), cz = Math.floor(eyeZ);
        const px = selfX, pz = selfZ;
        if (f(cx, cz)) top = level;
        const dx = Math.abs(px - cx), dz = Math.abs(pz - cz);
        if (dx > dz) {
            const delta = Math.floor((dz * 65536) / dx);
            let acc = 32768;
            while (cx !== px) {
                cx += cx < px ? 1 : -1;
                if (f(cx, cz)) top = level;
                acc += delta;
                if (acc >= 65536) {
                    acc -= 65536;
                    if (cz < pz) cz++; else if (cz > pz) cz--;
                    if (f(cx, cz)) top = level;
                }
            }
        } else if (dz > 0) {
            const delta = Math.floor((dx * 65536) / dz);
            let acc = 32768;
            while (cz !== pz) {
                cz += cz < pz ? 1 : -1;
                if (f(cx, cz)) top = level;
                acc += delta;
                if (acc >= 65536) {
                    acc -= 65536;
                    if (cx < px) cx++; else if (cx > px) cx--;
                    if (f(cx, cz)) top = level;
                }
            }
        }
    }
    if (f(selfX, selfZ)) top = level;
    return top;
}

// facing turns an observed step into the client's yaw (0..2047, 0 = facing south, 512 west,
// 1024 north, 1536 east), the direction an entity faces while walking. No step keeps the
// previous facing. The game also turns entities toward what they interact with; that target
// is not in the scene yet, so an idle entity keeps facing the way it last walked.
export function facing(dx, dz, previous = 0) {
    const sx = Math.sign(dx), sz = Math.sign(dz);
    if (!sx && !sz) return previous;
    return { '0,-1': 0, '-1,-1': 256, '-1,0': 512, '-1,1': 768, '0,1': 1024, '1,1': 1280, '1,0': 1536, '1,-1': 1792 }[sx + ',' + sz];
}

// isMoving: the entity still has route to walk (the game shows walk/run then, ready otherwise).
export function isMoving(m) { return !!m && m.route.length > 0; }

// faceYaw turns a face target into the client's yaw: dstYaw = atan2(dx, dz) * 325.949 & 0x7ff
// with dx/dz from the target TO the entity, as Client.ts computes it for faceEntity.
export function faceYaw(fromX, fromZ, targetX, targetZ, previous = 0) {
    const dx = fromX - targetX, dz = fromZ - targetZ;
    if (!dx && !dz) return previous;
    return Math.round(Math.atan2(dx, dz) * 325.949) & 0x7ff;
}

// CYCLE_MS is the reference client's animation clock: sequence frame delays count 20 ms cycles.
export const CYCLE_MS = 20;

// primaryFrame replays Client.ts's primary-animation advance from the moment the sequence
// (re)started: frames step when their delays (in cycles) run out; past the last frame the
// sequence steps back `loops` frames (loops -1, the default, ends it) and ends after
// `maxloops` loops. Returns the frame index, or -1 once the sequence has finished (the
// entity then falls back to its ready/walk sequence).
export function primaryFrame(seq, elapsedMs) {
    const n = seq.delays.length;
    if (!n) return -1;
    let cycles = Math.floor(elapsedMs / CYCLE_MS), frame = 0, loop = 0;
    for (let guard = 0; guard < 100000; guard++) {
        const d = Math.max(1, seq.delays[frame] || 1);
        if (cycles < d) return frame;
        cycles -= d; frame++;
        if (frame >= n) {
            frame -= seq.loops;
            loop++;
            if (loop >= (seq.maxloops ?? 99) || frame < 0 || frame >= n) return -1;
        }
    }
    return -1;
}

// cycleFrame: the frame a repeating sequence is on, for sequence delays summing to total client
// cycles, at the given cycle count. Allocation-free, for the many scenery animations drawn every paint.
export function cycleFrame(delays, total, cycles) {
    if (!(total > 0)) return 0;
    cycles = ((cycles % total) + total) % total;
    for (let f = 0; f < delays.length; f++) {
        if (cycles < delays[f]) return f;
        cycles -= delays[f];
    }
    return 0;
}

// loopFrame: a ready/walk/run sequence repeats for as long as it is the one being shown.
export function loopFrame(seq, elapsedMs) {
    const n = seq.delays.length;
    if (!n) return 0;
    const total = seq.delays.reduce((t, d) => t + Math.max(1, d || 1), 0);
    let cycles = Math.floor(elapsedMs / CYCLE_MS) % total;
    for (let f = 0; f < n; f++) {
        const d = Math.max(1, seq.delays[f] || 1);
        if (cycles < d) return f;
        cycles -= d;
    }
    return 0;
}

function newMotion(x, z, now, e) {
    return { e, x, z, route: [], obsX: x, obsZ: z, obsT: now, lastStep: now, yaw: 0, dstYaw: 0, turnspeed: TURN_SPEED, primaryStart: now, walk: 'ready', speed: 0 };
}

// observe queues a newly observed tile: the intermediate tile of a two-tile step first (the
// client receives both), each flagged run when the move was that fast.
function observe(m, x, z, now, at = now, delay = 0) {
    // Server time since it was last seen where it was: a move longer than it could have run
    // in that many ticks (two tiles a tick) is a teleport; shorter ones are walked, even when
    // polls missed ticks.
    const since = at - (m.seenAt ?? at);
    m.seenAt = at;
    if (x === m.obsX && z === m.obsZ) return;
    const dx = x - m.obsX, dz = z - m.obsZ, dist = Math.max(Math.abs(dx), Math.abs(dz));
    m.obsX = x; m.obsZ = z; m.obsT = now;
    const ticks = Math.max(1, Math.round(since / (600 / (m.tickScale || 1))));
    if (dist > Math.max(SNAP_TILES, 2 * ticks)) { m.route.length = 0; m.x = x; m.z = z; return; }
    // Walking covers one tile per 600 ms tick (1.7 tiles/s), running two (3.3 tiles/s). The
    // session does not keep the protocol's run flag, so the speed over the last ~1.5 s of
    // server capture time decides; a lone two-tile step (a re-routed click) is not a run.
    // tickScale is 600 / the server's tick in ms (1 on the live 600 ms server; a lab engine
    // at 200 ms moves three times as fast and the thresholds scale with it).
    const k = m.tickScale || 1;
    m.hist = (m.hist || []).filter(h => at - h.t <= 1500 / k);
    m.hist.push({ t: at, d: dist });
    const span = at - m.hist[0].t, tiles = m.hist.slice(1).reduce((n, h) => n + h.d, 0);
    const run = span >= 450 / k && tiles / span * 1000 >= 2.6 * k;
    // Once a run is confirmed, the steps still queued from the first fast step were runs too.
    if (run) for (const r of m.route) r.run = true;
    // When this tile plays (local clock): never before it arrived, never before the tile
    // queued ahead of it.
    const last = m.route[m.route.length - 1];
    const play = Math.max(now, at + delay, last ? last.play : -Infinity);
    let px = x - dx, pz = z - dz;
    while (px !== x || pz !== z) {
        px += Math.sign(x - px); pz += Math.sign(z - pz);
        m.route.push({ x: px, z: pz, run, play });
    }
    if (m.route.length > 12) { const last = m.route[m.route.length - 1]; m.route.length = 0; m.x = last.x; m.z = last.z; }
}

// step: Client.ts routeMove plus turning, over the cycles since the last step.
function step(m, now, faceOf) {
    if (faceOf) { const f = faceOf(m); if (f !== null && f !== undefined) m.faceYaw = f; else m.faceYaw = null; }
    if (now <= m.lastStep) return;
    let cycles = Math.min(250, (now - m.lastStep) / 20);
    let t = now - cycles * 20; // the simulated time within this window
    m.lastStep = now;
    const turn = n => {
        let d = (m.dstYaw - m.yaw) & 0x7ff;
        if (d > 1024) d -= 2048;
        const t = Math.min(Math.abs(d), m.turnspeed * n);
        m.yaw = (m.yaw + Math.sign(d) * t) & 0x7ff;
    };
    m.walk = 'ready';
    while (cycles > 0 && m.route.length) {
        const target = m.route[0];
        if (target.play > t) {
            // Not due yet: stand (as the client does with an empty route) until it is.
            const wait = (target.play - t) / 20;
            m.walk = 'ready';
            if (wait >= cycles) { cycles = 0; break; }
            cycles -= wait; t = target.play;
            continue;
        }
        let due = 0;
        for (const r of m.route) { if (r.play > t) break; due++; }
        const tdx = target.x - m.x, tdz = target.z - m.z;
        m.dstYaw = m.faceYaw ?? facing(tdx, tdz, m.dstYaw);
        const moveYaw = facing(tdx, tdz, m.dstYaw);
        const kind = () => {
            let d = (moveYaw - m.yaw) & 0x7ff;
            if (d > 1024) d -= 2048;
            return d >= -256 && d <= 256 ? 'walk' : d >= 256 && d < 768 ? 'walk_r' : d >= -768 && d <= -256 ? 'walk_l' : 'walk_b';
        };
        m.walk = kind();
        let speed = 4;
        if (m.yaw !== m.dstYaw && m.faceYaw == null) speed = 2;
        if (due > 2) speed = 6;
        if (due > 3) speed = 8;
        if (target.run) speed <<= 1;
        // Client.ts plays run at speed 8 and above; here the queue can also back up from
        // observation jitter, so only an actual run step (two tiles within a tick) plays run.
        if (target.run && m.walk === 'walk') m.walk = 'run';
        m.speed = speed;
        // Speeds are the client's per-20ms-cycle units, scaled to the server's tick.
        speed *= m.tickScale || 1;
        const units = Math.max(Math.abs(tdx), Math.abs(tdz)) * 128;
        const need = units / speed;
        const used = Math.min(cycles, need);
        const f = units ? (used * speed) / 128 : 0;
        m.x += Math.sign(tdx) * Math.min(Math.abs(tdx), f);
        m.z += Math.sign(tdz) * Math.min(Math.abs(tdz), f);
        turn(used);
        // The sequence shown is the one for the state this window ends in.
        m.walk = kind();
        if (target.run && m.walk === 'walk') m.walk = 'run';
        cycles -= used; t += used * 20;
        if (used >= need - 1e-9) { m.x = target.x; m.z = target.z; m.route.shift(); }
    }
    if (!m.route.length) {
        m.walk = 'ready';
        if (m.faceYaw != null) m.dstYaw = m.faceYaw;
        const before = m.yaw;
        turn(cycles);
        if (m.yaw !== before || m.yaw !== m.dstYaw) m.walk = 'turn';
    }
}

// busy: walking, or turning toward its target.
function busy(m) { return m.route.length > 0 || m.yaw !== m.dstYaw; }

// terrainHeights indexes a terrainGpuMesh result (7 floats per vertex, x/y/z relative to
// the tile the mesh was decoded around) by absolute tile CORNER, so an entity can stand on
// the decoded ground. originX/originZ is the account's tile when the mesh was decoded.
// Returns heightAt(x, z) for a point in absolute tile units (a tile's centre is X+0.5),
// bilinear over the four surrounding corners. Corners under omitted (textured) faces are
// missing; the nearest decoded corners around the point stand in, and null means none.
export function terrainHeights(vertices, originX, originZ, floats = 7) {
    const heights = new Map();
    for (let i = 0; i + 6 < vertices.length; i += floats) {
        const cx = vertices[i] + originX + 0.5, cz = vertices[i + 2] + originZ + 0.5;
        const rx = Math.round(cx), rz = Math.round(cz);
        if (Math.abs(cx - rx) > 1e-3 || Math.abs(cz - rz) > 1e-3) continue;
        heights.set(rx * 65536 + rz, vertices[i + 1]);
    }
    const at = (x, z) => heights.get(x * 65536 + z);
    return function heightAt(x, z) {
        const x0 = Math.floor(x), z0 = Math.floor(z), fx = x - x0, fz = z - z0;
        const c = [at(x0, z0), at(x0 + 1, z0), at(x0, z0 + 1), at(x0 + 1, z0 + 1)];
        if (c.every(v => v !== undefined)) return (c[0] * (1 - fx) + c[1] * fx) * (1 - fz) + (c[2] * (1 - fx) + c[3] * fx) * fz;
        for (let r = 0; r <= 2; r++) {
            let sum = 0, n = 0;
            for (let dx = -r; dx <= r + 1; dx++) for (let dz = -r; dz <= r + 1; dz++) {
                const v = at(x0 + dx, z0 + dz);
                if (v !== undefined) { sum += v; n++; }
            }
            if (n) return sum / n;
        }
        return null;
    };
}

// gridHeights wraps the adapter's authoritative corner-height grid (sceneGpuMesh.heights:
// the client's own groundh for the account's level, bridges included) as heightAt(x, z) for
// absolute tile coordinates, bilinear between corners. originX/originZ is the account's tile
// at decode time; corner (i, j) is absolute (originX - radius + i, originZ - radius + j).
export function gridHeights(heights, side, originX, originZ, radius) {
    const x0 = originX - radius, z0 = originZ - radius;
    const at = (i, j) => heights[Math.max(0, Math.min(side - 1, i)) * side + Math.max(0, Math.min(side - 1, j))];
    return function heightAt(x, z) {
        const fx = x - x0, fz = z - z0, i = Math.floor(fx), j = Math.floor(fz), tx = fx - i, tz = fz - j;
        return (at(i, j) * (1 - tx) + at(i + 1, j) * tx) * (1 - tz) + (at(i, j + 1) * (1 - tx) + at(i + 1, j + 1) * tx) * tz;
    };
}
