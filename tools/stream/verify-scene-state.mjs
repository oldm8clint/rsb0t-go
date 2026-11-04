// node tools/stream/verify-scene-state.mjs — pure checks of scene-state.js (no browser,
// server or game). The same apply rule is checked from the Go side by
// internal/controlpanel's TestDiffStreamSceneSendsOnlyWhatChanged.
import assert from 'node:assert/strict';
import { fitBox, clampPitch, clampZoom, easeToward, PITCH_MIN, PITCH_MAX } from './scene-view.js';
import { SceneState, sceneFromSnapshot, terrainHeights, STEP_MS, HOLD_MS, primaryFrame, loopFrame, faceYaw, facing, applyUiDelta, roofTop } from './scene-state.js';

const scene = {
    tick: 1, self: { x: 100, z: 200, level: 0, centreX: 12, centreZ: 25, slot: 7, name: 'me' },
    entities: [{ k: 'n1', x: 101, z: 200, t: 316, n: 'Fishing spot' }, { k: 'p9', x: 99, z: 201, n: 'friend' }],
    inv: [[303, 1], [0, 0], [0, 0]], chat: [{ type: 0, text: 'hello' }], status: { activity: 'fishing' }
};

// Whole scene, then a delta on the right base.
const s = new SceneState();
let t = 1000;
assert.deepEqual(s.applyFrame({ seq: 1, scene: structuredClone(scene) }, t), { full: true, self: true });
assert.equal(s.entities.size, 2);
assert.deepEqual(s.takeInvChanges(), { all: true, slots: [] });
s.applyFrame({ seq: 2, tick: 2, delta: { base: 1, upsert: [{ k: 'n1', x: 102, z: 200, t: 316, n: 'Fishing spot' }], remove: ['p9'], inv: [[1, 317, 1]] } }, t);
assert.equal(s.seq, 2);
assert.equal(s.entities.size, 1);
assert.deepEqual(s.scene.inv[1], [317, 1]);
assert.deepEqual(s.takeInvChanges(), { all: false, slots: [1] });
assert.equal(s.scene.tick, 2);

// Movement (Client.ts routeMove): a one-tile step is walked along the route, not snapped;
// the entity arrives within the client's walk time (640 ms plus turning), shows 'walk' on the
// way and 'ready' after; a long jump snaps. A route from rest waits HOLD_MS first (a jitter
// buffer for late observations).
const at = now => [...s.entitiesAt(now)][0];
const play = [...s.entities.values()][0].route[0].play;
assert.ok(play >= t + HOLD_MS, 'a tile plays at least HOLD_MS after it was observed');
assert.equal(at(t).x, 101);
assert.equal(at(play - 50).x, 101, 'held before walking');
const mid = at(play + 400).x;
assert.ok(mid > 101 && mid < 102, `mid-step position ${mid}`);
assert.equal([...s.entities.values()][0].walk, 'walk');
assert.ok(s.animating(play + 400));
assert.equal(at(play + 1200).x, 102);
assert.equal([...s.entities.values()][0].walk, 'ready');
assert.ok(!s.animating(play + 1300));
t += 2000;
s.applyFrame({ seq: 3, delta: { base: 2, upsert: [{ k: 'n1', x: 150, z: 200, t: 316 }] } }, t);
assert.equal(at(t).x, 150, 'a teleport-length move snaps rather than sliding');
// Moving at running speed (two tiles a tick) is a run; queued steps are marked run.
t += 2000; at(t);
for (let i = 1; i <= 4; i++) s.applyFrame({ seq: 3 + i, delta: { base: 2 + i, upsert: [{ k: 'n1', x: 150 + 2 * i, z: 200, t: 316 }] } }, t + i * 300);
at(t + 1250);
assert.ok([...s.entities.values()][0].route.some(r => r.run), 'running speed is queued as a run');
assert.equal(at(t + 5000).x, 158);
s.seq = 7;
// Two tiles over a long gap (a missed observation) stays a walk.
t += 6000; at(t);
s.applyFrame({ seq: 8, delta: { base: 7, upsert: [{ k: 'n1', x: 160, z: 200, t: 316 }] } }, t + 1500);
at(t + 1500 + HOLD_MS + 50);
assert.equal([...s.entities.values()][0].walk, 'walk');
at(t + 5000);
s.seq = 3; // the checks below apply deltas on base 3

// Uneven polling (replies 20-450 ms late, a tick missed now and then) still walks and runs
// without dropping to the ready pose mid-route: tiles play on the frame's game tick.
for (const [tilesPerTick, want] of [[1, 'walk'], [2, 'run']]) {
    const st = new SceneState(); let seed = 3;
    const rnd = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648;
    const obs = [];
    for (let c = 0; c < 14 * 600;) {
        const lat = 20 + rnd() * 80 + (rnd() < 0.1 ? 350 : 0);
        obs.push({ tick: Math.floor(c / 600) + 1, arrive: c + lat, x: 3200 + Math.min(Math.floor(c / 600), 10) * tilesPerTick });
        c += lat + 200;
    }
    const kinds = []; let i = 0;
    for (let now = 0; now <= 10000; now += 50) {
        while (i < obs.length && obs[i].arrive <= now) {
            const o = obs[i++];
            st.applyFrame({ seq: i, tick: o.tick, scene: { tick: o.tick, self: { x: o.x, z: 3200, level: 0 }, entities: [] } }, o.arrive);
        }
        st.advance(now, null);
        kinds.push(st.selfMotion?.walk || 'ready');
    }
    const first = kinds.findIndex(k => k !== 'ready'), last = kinds.findLastIndex(k => k !== 'ready');
    const between = kinds.slice(first, last + 1);
    assert.ok(!between.includes('ready'), `${want}: no ready pose mid-route (${between.join(',')})`);
    assert.ok(between.filter(k => k === want).length > between.length * 0.8, `${want}: mostly ${want}`);
    if (want === 'walk') assert.ok(!between.includes('run'), 'walking never shows run');
    assert.equal(st.selfMotion.x, 3200 + 10 * tilesPerTick);
}

// A delta on a base this state does not hold is refused, never half-applied.
assert.throws(() => s.applyFrame({ seq: 5, delta: { base: 4, remove: ['n1'] } }, t), /base 4/);
assert.equal(s.entities.size, 1);
assert.throws(() => s.applyFrame({ seq: 6, delta: { base: 3, inv: [[99, 1, 1]] } }, t), /slot 99/);

// Self moves along its route like any entity.
s.applyFrame({ seq: 7, scene: { ...structuredClone(scene), self: { ...scene.self, x: 101 } } }, t);
const selfMid = s.selfAt(t + HOLD_MS + 400)[0];
assert.ok(selfMid > 100 && selfMid < 101, `self mid-step ${selfMid}`);
assert.equal(s.selfAt(t + HOLD_MS + 1500)[0], 101);

// full-snapshot fallback: both json casings, self excluded, backpack projected.
const projected = sceneFromSnapshot({ x: 10, z: 20, level: 0, centreX: 1, centreZ: 2, selfSlot: 3,
    npcs: [{ Index: 4, Type: 5, X: 11, Z: 20 }], players: [{ index: 3, x: 10, z: 20 }, { index: 8, name: 'other', x: 9, z: 20 }],
    ui: { name: 'me', tick: 9, inventories: { 3214: { Slots: [{ ID: 1, Count: 2 }, { ID: 0, Count: 0 }] } }, chat: [{ type: 2, username: 'u', text: 'x' }] } });
assert.deepEqual(projected.entities.map(e => e.k), ['n4', 'p8']);
assert.deepEqual(projected.inv, [[1, 2], [0, 0]]);
assert.equal(projected.chat[0].user, 'u');

// Terrain heights: one quick tile decoded around tile (100, 200); corners at relative -0.5/+0.5.
const v = [];
const corner = (rx, rz, y) => v.push(rx, y, rz, 0, 0, 0, 1);
corner(-0.5, -0.5, 1); corner(0.5, -0.5, 2); corner(-0.5, 0.5, 3);
corner(0.5, -0.5, 2); corner(0.5, 0.5, 4); corner(-0.5, 0.5, 3);
const h = terrainHeights(v, 100, 200);
assert.equal(h(100, 200), 1);
assert.equal(h(100.5, 200.5), 2.5, 'tile centre is the mean of its corners');
assert.equal(h(110, 210), null, 'no decoded corner anywhere near: unknown, not zero');
assert.equal(typeof h(101.5, 200.5), 'number', 'a missing corner falls back to the nearest decoded ones');
// Sequence timing, as Client.ts advances it (20 ms cycles).
const seq = { delays: [3, 3, 4], loops: -1, maxloops: 99 };
assert.equal(primaryFrame(seq, 0), 0);
assert.equal(primaryFrame(seq, 59), 0);
assert.equal(primaryFrame(seq, 60), 1);
assert.equal(primaryFrame(seq, 199), 2);
assert.equal(primaryFrame(seq, 200), -1, 'loops -1: the primary sequence ends after its last frame');
const looping = { delays: [2, 2, 2], loops: 2, maxloops: 3 };
assert.equal(primaryFrame(looping, 120), 1, 'past the end it steps back `loops` frames');
assert.equal(primaryFrame(looping, 10000), -1, 'and ends after maxloops');
assert.equal(loopFrame(seq, 200), 0);
assert.equal(loopFrame(seq, 260), 1);
// Facing: a target to the north makes the entity face north (1024), as Client.ts's atan2.
assert.equal(faceYaw(10, 10, 10, 15), 1024);
assert.equal(faceYaw(10, 10, 5, 10), 512, 'target to the west');
assert.equal(facing(0, -1), 0);
assert.equal(faceYaw(0, 0, 1, 1), facing(1, 1), 'step and face rules agree');
// Interface deltas: scalars replace, map keys merge, null deletes.
const ui = applyUiDelta({ main: -1, texts: { 1: 'a', 2: 'b' }, inventories: { 3214: { Slots: [] } } }, { main: 5292, texts: { 2: null, 3: 'c' }, inventories: { 5382: { Slots: [{ ID: 1, Count: 2 }] } } });
assert.deepEqual(ui, { main: 5292, texts: { 1: 'a', 3: 'c' }, inventories: { 3214: { Slots: [] }, 5382: { Slots: [{ ID: 1, Count: 2 }] } } });
// Npc type 0 arrives without "t" (omitempty): it must become 0, never undefined (undefined
// sent the adapter's config decoder into an endless loop).
const zero = new SceneState();
zero.applyFrame({ seq: 1, scene: { ...structuredClone(scene), entities: [{ k: 'n4', x: 100, z: 200 }] } }, 0);
assert.equal(zero.entities.get('n4').e.t, 0);
// Roofs (Client.ts roofCheck): a building covering tiles x 10..14, z 10..14 of a region based at 1000,2000.
const flags = new Array(104 * 104).fill(0);
for (let x = 10; x <= 14; x++) for (let z = 10; z <= 14; z++) flags[x * 104 + z] = 1;
const base = { x: 1000, z: 2000 };
assert.equal(roofTop(flags, base, 0, 1012, 2012, 1012, 2030, 250), 0, 'inside: roofs removed');
assert.equal(roofTop(flags, base, 1, 1012, 2012, 1012, 2030, 250), 1, 'inside on the upper floor: levels up to it');
assert.equal(roofTop(flags, base, 0, 1020, 2020, 1030, 2030, 250), 3, 'outside, camera looking away: everything drawn');
assert.equal(roofTop(flags, base, 0, 1012, 2020, 1012, 2005, 250), 0, 'outside, low camera looking across the building: removed');
assert.equal(roofTop(flags, base, 0, 1012, 2020, 1012, 2005, 330), 3, 'the same with a high camera (pitch >= 310): drawn');
assert.equal(roofTop(null, base, 0, 1012, 2012, 1012, 2030, 250), 3, 'no flags from an old adapter: everything drawn');
// View: fit keeps 765:503, centred, whole pixels; the game's pitch limits; zoom bounds; easing.
const b1 = fitBox(1000, 500);
assert.equal(b1.height, 500); assert.equal(b1.width, Math.floor(765 * 500 / 503)); assert.equal(b1.left, Math.floor((1000 - b1.width) / 2)); assert.equal(b1.top, 0);
const b2 = fitBox(765, 2000);
assert.equal(b2.width, 765); assert.equal(b2.height, 503); assert.equal(b2.top, Math.floor((2000 - 503) / 2));
assert.equal(clampPitch(0), PITCH_MIN); assert.equal(clampPitch(3), PITCH_MAX);
assert.ok(Math.abs(PITCH_MIN - 0.3927) < 1e-3 && Math.abs(PITCH_MAX - 1.1750) < 1e-3, 'Client.ts orbitCameraPitch 128..383');
assert.equal(clampZoom(1), 6); assert.equal(clampZoom(100), 45);
let d = 20; for (let i = 0; i < 60; i++) d = easeToward(d, 10, 1 / 60);
assert.equal(d, 10, 'zoom easing settles within a second');
assert.equal(easeToward(20, 10, 0), 20, 'no time, no movement');
console.log(JSON.stringify({ sceneState: 'pass' }));
