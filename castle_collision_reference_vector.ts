// Throwaway generator for internal/doorescape: runs the REAL Engine-TS collision code over the 289 map
// data of Rogues' Castle (mapsquare 51,61 and its eight neighbours) and writes
//
//   1. internal/doorescape/castle_collision.json: the castle region's static collision flags, doors
//      closed, as the server holds them after GameMap.init (the movement-relevant bits only), which the
//      package embeds, and
//   2. test/fixtures/castle_collision_golden.json: known answers the Go port is checked against, tile for
//      tile and door state by door state: the doors and stairs as the map places them, the approach tiles
//      the engine's own ReachStrategy accepts for each, and, for a few hundred door states, a digest of
//      every tile's eight-direction step mask (StepValidator.canTravel) and of every reach answer.
//
// Not part of the build; kept only as the source of those two files.
//
//   RSB0T_ENGINE=<Engine-TS 289 checkout> bun castle_collision_reference_vector.ts \
//       internal/doorescape/castle_collision.json test/fixtures/castle_collision_golden.json
//
// What it does NOT run: the RuneScript door procs (content/scripts/doors/scripts/*.rs2). Their effect is
// ported by hand below (doorOpen, doorClose, the leaf's angle) and by hand again in doorescape/door.go;
// the live fleet log's own door sightings (Door at 3293,3931, 3294,3931 and 3284,3931 reported open after
// the closed leaf at 3292,3931, 3294,3932 and 3283,3931 was operated) agree with it.
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

const engine = process.env.RSB0T_ENGINE;
if (!engine) {
    throw new Error('set RSB0T_ENGINE to the Engine-TS checkout (the one holding data/pack/.cache/maps-server.zip)');
}
const flagsOut = process.argv[2];
const goldenOut = process.argv[3];
if (!flagsOut || !goldenOut) {
    throw new Error('usage: bun castle_collision_reference_vector.ts <flags json> <golden json>');
}
const imp = (p: string): Promise<any> => import(pathToFileURL(`${engine}/src/${p}`).href);
const { unzipSync } = await import(pathToFileURL(`${engine}/node_modules/fflate/esm/index.mjs`).href);
const { default: Packet } = await imp('io/Packet.ts');
const { default: LocType } = await imp('cache/config/LocType.ts');
const rf = await imp('engine/routefinder/index.ts');
const { CollisionFlag, CollisionType, LocAngle, LocLayer, locShapeLayer } = rf;
const { canTravel } = await imp('engine/routefinder/StepValidator.ts');
const { default: ReachStrategy } = await imp('engine/routefinder/ReachStrategy.ts');

LocType.load(`${engine}/data/pack`);
const col = new rf.RouteFinder();
const zip = unzipSync(fs.readFileSync(`${engine}/data/pack/.cache/maps-server.zip`));

// The region the fixture covers: the castle (3275-3298 x 3925-3941) with a margin of open ground.
const X0 = 3262, X1 = 3310, Z0 = 3910, Z1 = 3950;
const LEVELS = 4;
// Doors and stairs found in the map inside this box are the catalogue.
const BOX = { x0: 3274, x1: 3298, z0: 3922, z1: 3942 };
const DOOR_IDS = new Set([1530, 1539]);
const STAIR_IDS = new Set([1738, 1739, 1740]);

// ---- GameMap.loadGround / loadLocations, for the castle's squares -----------------------------------
const OPEN = 0, BLOCK_MAP_SQUARE = 1, LINK_BELOW = 2, REMOVE_ROOFS = 4;
const pack = (x: number, z: number, level: number) => (z & 0x3f) | ((x & 0x3f) << 6) | ((level & 0x3) << 12);
const unpack = (p: number) => ({ x: (p >> 6) & 0x3f, z: p & 0x3f, level: (p >> 12) & 3 });

function changeLocCollision(shape: number, angle: number, blockrange: boolean, length: number, width: number, active: number, x: number, z: number, level: number, add: boolean) {
    const layer = locShapeLayer(shape);
    if (layer === LocLayer.WALL) {
        col.changeWall(x, z, level, angle, shape, blockrange, add);
    } else if (layer === LocLayer.GROUND) {
        if (angle === LocAngle.NORTH || angle === LocAngle.SOUTH) {
            col.changeLoc(x, z, level, length, width, blockrange, add);
        } else {
            col.changeLoc(x, z, level, width, length, blockrange, add);
        }
    } else if (layer === LocLayer.GROUND_DECOR) {
        if (active === 1) {
            col.changeFloor(x, z, level, add);
        }
    }
}

type Placed = { id: number; x: number; z: number; level: number; angle: number; shape: number };
const found: Placed[] = [];
// A tile with an empty record in the map file has no ground: on level 0 there is none of those, above it the
// castle's floors are the only tiles with a record. The engine leaves such a tile's flags at zero (open air),
// the collision pack calls it not walkable, and so does every walker of the fleet, so the fixture does too:
// FLOOR is set on every tile above level 0 that has no ground (see the end of loadSquare's caller below).
const hasGround = new Set<string>();

function loadSquare(mx: number, mz: number) {
    const mapsquareX = mx << 6, mapsquareZ = mz << 6;
    const m = zip[`m${mx}_${mz}`];
    const l = zip[`l${mx}_${mz}`];
    if (!m) return;
    const lands = new Int8Array(4 * 64 * 64);
    const packet = new Packet(m);
    for (let level = 0; level < 4; level++) for (let x = 0; x < 64; x++) for (let z = 0; z < 64; z++) {
        while (true) {
            const opcode = packet.g1();
            if (opcode === 0) break;
            hasGround.add(`${level},${mapsquareX + x},${mapsquareZ + z}`);
            if (opcode === 1) { packet.pos++; break; }
            if (opcode <= 49) packet.pos++;
            else if (opcode <= 81) lands[pack(x, z, level)] = opcode - 49;
        }
    }
    for (let level = 0; level < 4; level++) for (let x = 0; x < 64; x++) {
        const ax = x + mapsquareX;
        for (let z = 0; z < 64; z++) {
            const az = z + mapsquareZ;
            if (x % 7 === 0 && z % 7 === 0) col.allocateIfAbsent(ax, az, level);
            const land = lands[pack(x, z, level)];
            if ((land & REMOVE_ROOFS) !== OPEN) col.changeRoof(ax, az, level, true);
            if ((land & BLOCK_MAP_SQUARE) !== BLOCK_MAP_SQUARE) continue;
            const bridged = (level === 1 ? land & LINK_BELOW : lands[pack(x, z, 1)] & LINK_BELOW) === LINK_BELOW;
            const actual = bridged ? level - 1 : level;
            if (actual < 0) continue;
            col.changeFloor(ax, az, actual, true);
        }
    }
    if (!l) return;
    const lp = new Packet(l);
    let locId = -1;
    let off = lp.gsmarts();
    while (off !== 0) {
        locId += off;
        let coord = 0;
        let coffs = lp.gsmarts();
        while (coffs !== 0) {
            const { x, z, level } = unpack((coord += coffs - 1));
            const info = lp.g1();
            coffs = lp.gsmarts();
            const ax = x + mapsquareX, az = z + mapsquareZ;
            const bridged = (level === 1 ? lands[coord] & LINK_BELOW : lands[pack(x, z, 1)] & LINK_BELOW) === LINK_BELOW;
            const actual = bridged ? level - 1 : level;
            if (actual < 0) continue;
            const type = LocType.get(locId);
            const width = type.width, length = type.length;
            const shape = info >> 2, angle = info & 3;
            if (type.blockwalk) changeLocCollision(shape, angle, type.blockrange, length, width, type.active, ax, az, actual, true);
            if ((DOOR_IDS.has(locId) || STAIR_IDS.has(locId)) && ax >= BOX.x0 && ax <= BOX.x1 && az >= BOX.z0 && az <= BOX.z1) {
                found.push({ id: locId, x: ax, z: az, level: actual, angle, shape });
            }
        }
        off = lp.gsmarts();
    }
}

for (let mx = 50; mx <= 52; mx++) for (let mz = 60; mz <= 62; mz++) loadSquare(mx, mz);
for (let level = 1; level < 4; level++) for (let x = X0; x <= X1; x++) for (let z = Z0; z <= Z1; z++) {
    if (!hasGround.has(`${level},${x},${z}`)) col.changeFloor(x, z, level, true);
}

// ---- the region's static flags -------------------------------------------------------------------------
// Movement reads only these bits (CollisionFlag.BLOCK_*: the eight wall faces, LOC, FLOOR, FLOOR_DECORATION);
// the projectile blockers, the roof bit and the entity-occupancy bits never change a step of a player.
const MOVE_MASK = (0xff | CollisionFlag.LOC | CollisionFlag.FLOOR | CollisionFlag.FLOOR_DECORATION) >>> 0;
const W = X1 - X0 + 1, H = Z1 - Z0 + 1;
const baseline: number[][][] = [];
const rawBaseline: number[][][] = [];
for (let level = 0; level < LEVELS; level++) {
    const rows: number[][] = [];
    const raw: number[][] = [];
    for (let z = Z0; z <= Z1; z++) {
        const row: number[] = [];
        const rrow: number[] = [];
        for (let x = X0; x <= X1; x++) {
            const f = col.collisionFlags.get(x, z, level) >>> 0;
            rrow.push(f);
            row.push((f & MOVE_MASK) >>> 0);
        }
        rows.push(row);
        raw.push(rrow);
    }
    baseline.push(rows);
    rawBaseline.push(raw);
}
fs.writeFileSync(flagsOut, JSON.stringify({
    source: 'castle_collision_reference_vector.ts over Engine-TS 289 data/pack/.cache/maps-server.zip (mapsquares 50-52,60-62); movement bits only',
    x0: X0, z0: Z0, width: W, height: H, levels: LEVELS, moveMask: MOVE_MASK,
    // flags[level][z - z0][x - x0]
    flags: baseline,
}));

function restore() {
    for (let level = 0; level < LEVELS; level++) {
        for (let z = Z0; z <= Z1; z++) {
            for (let x = X0; x <= X1; x++) col.collisionFlags.set(x, z, level, rawBaseline[level][z - Z0][x - X0]);
        }
    }
}

// ---- the doors' RuneScript, ported by hand (content/scripts/doors/scripts/door_procs.rs2) ---------------
// door_open / door_close return the offset from the loc's own tile to where the leaf goes; the leaf's
// angle becomes (angle + 1) % 4 on opening and (angle + 3) % 4 on closing; the id is loc_param(next_loc_stage).
const doorOpen = (angle: number, shape: number): [number, number] => (shape === 0
    ? [[-1, 0], [0, 1], [1, 0], [0, -1]][angle]
    : [[0, 1], [1, 0], [0, -1], [-1, 0]][angle]) as [number, number];
const NEXT: Record<number, number> = { 1530: 1531, 1539: 1540 };

const doors: Placed[] = found.filter(p => DOOR_IDS.has(p.id)).sort((a, b) => a.level - b.level || a.x - b.x || a.z - b.z);
const stairs: Placed[] = found.filter(p => STAIR_IDS.has(p.id)).sort((a, b) => a.level - b.level || a.x - b.x || a.z - b.z);
const openLeaf = (d: Placed): Placed => {
    const [dx, dz] = doorOpen(d.angle, d.shape);
    return { id: NEXT[d.id], x: d.x + dx, z: d.z + dz, level: d.level, angle: (d.angle + 1) % 4, shape: d.shape };
};
const apply = (p: Placed, add: boolean) => {
    const t = LocType.get(p.id);
    if (t.blockwalk) changeLocCollision(p.shape, p.angle, t.blockrange, t.length, t.width, t.active, p.x, p.z, p.level, add);
};

// ---- digests -----------------------------------------------------------------------------------------
class Fnv {
    h = 0x811c9dc5;
    byte(b: number) { this.h ^= b & 0xff; this.h = Math.imul(this.h, 0x01000193) >>> 0; }
    hex() { return this.h.toString(16).padStart(8, '0'); }
}
const DIRS: [number, number][] = [[0, 1], [1, 0], [0, -1], [-1, 0], [1, 1], [1, -1], [-1, -1], [-1, 1]];
const stepMask = (level: number, x: number, z: number): number => {
    let m = 0;
    for (let d = 0; d < 8; d++) {
        if (canTravel(col.collisionFlags, level, x, z, DIRS[d][0], DIRS[d][1], 1, CollisionFlag.BLOCK_NPC_AND_PLAYERS, CollisionType.NORMAL)) m |= 1 << d;
    }
    return m;
};
// The digest covers the region without its outer ring of tiles, whose neighbours the fixture does not hold.
function exitsDigest(): string {
    const h = new Fnv();
    for (let level = 0; level < LEVELS; level++) {
        for (let z = Z0 + 1; z <= Z1 - 1; z++) for (let x = X0 + 1; x <= X1 - 1; x++) h.byte(stepMask(level, x, z));
    }
    return h.hex();
}
const reachOf = (p: Placed, level: number, sx: number, sz: number): boolean => {
    const t = LocType.get(p.id);
    return ReachStrategy.reached(col.collisionFlags, level, sx, sz, p.x, p.z, t.width, t.length, 1, p.angle, p.shape, t.forceapproach);
};
// Every placement a reach question is asked about: each door's closed and open leaf, and the stairs.
const targets: Placed[] = [];
for (const d of doors) { targets.push(d); targets.push(openLeaf(d)); }
for (const s of stairs) targets.push(s);
const RADIUS = 3;
function reachBits(): { digest: string; tiles: string[][] } {
    const h = new Fnv();
    const tiles: string[][] = [];
    for (const p of targets) {
        const ok: string[] = [];
        for (let dz = -RADIUS; dz <= RADIUS; dz++) for (let dx = -RADIUS; dx <= RADIUS; dx++) {
            const r = reachOf(p, p.level, p.x + dx, p.z + dz);
            h.byte(r ? 1 : 0);
            if (r) ok.push(`${p.x + dx},${p.z + dz}`);
        }
        tiles.push(ok);
    }
    return { digest: h.hex(), tiles };
}

// ---- door states: each door has a closed leaf present or not and an open leaf present or not ----------
type State = number[]; // [closed0, open0, closed1, open1, ...]
function applyState(s: State) {
    restore();
    for (let i = 0; i < doors.length; i++) {
        if (!s[2 * i]) apply(doors[i], false);
        if (s[2 * i + 1]) apply(openLeaf(doors[i]), true);
    }
}
let seed = 0x1badd00d;
const rand = (n: number) => { seed ^= seed << 13; seed >>>= 0; seed ^= seed >>> 17; seed ^= seed << 5; seed >>>= 0; return seed % n; };
const states: State[] = [];
const closedAll: State = doors.flatMap(() => [1, 0]);
const openAll: State = doors.flatMap(() => [0, 1]);
states.push(closedAll, openAll);
for (let i = 0; i < doors.length; i++) {
    const s = closedAll.slice(); s[2 * i] = 0; s[2 * i + 1] = 1; states.push(s);
}
for (let i = 0; i < doors.length; i++) for (let j = i + 1; j < doors.length; j++) {
    const s = closedAll.slice(); s[2 * i] = 0; s[2 * i + 1] = 1; s[2 * j] = 0; s[2 * j + 1] = 1; states.push(s);
}
for (let k = 0; k < 120; k++) {
    // Mostly the two honest states per door (closed leaf alone, open leaf alone), with the odd one where
    // both leaves or neither are there, which the server reaches after a close races a respawn.
    states.push(doors.flatMap(() => { const r = rand(10); return r < 4 ? [1, 0] : r < 8 ? [0, 1] : r < 9 ? [1, 1] : [0, 0]; }));
}
const entries: any[] = [];
for (const s of states) {
    applyState(s);
    const reach = reachBits();
    entries.push({ leaves: s.join(''), exits: exitsDigest(), reach: reach.digest });
}
// Readable approach tables for the two uniform states.
const approach: Record<string, string[][]> = {};
for (const [name, s] of [['closed', closedAll], ['open', openAll]] as [string, State][]) {
    applyState(s);
    approach[name] = reachBits().tiles;
}
restore();

const typeInfo: Record<string, any> = {};
for (const id of [1530, 1531, 1539, 1540, 1738, 1739, 1740]) {
    const t = LocType.get(id);
    typeInfo[id] = { name: t.name, width: t.width, length: t.length, blockwalk: !!t.blockwalk, blockrange: !!t.blockrange, active: t.active, forceapproach: t.forceapproach };
}
fs.writeFileSync(goldenOut, JSON.stringify({
    source: 'castle_collision_reference_vector.ts over Engine-TS 289 (StepValidator.canTravel, ReachStrategy.reached, CollisionEngine.changeWall/changeLoc)',
    region: { x0: X0, z0: Z0, x1: X1, z1: Z1, levels: LEVELS },
    digestRegion: { x0: X0 + 1, z0: Z0 + 1, x1: X1 - 1, z1: Z1 - 1 },
    reachRadius: RADIUS,
    doors, stairs, locTypes: typeInfo,
    targets: targets.map(t => ({ id: t.id, x: t.x, z: t.z, level: t.level, angle: t.angle, shape: t.shape })),
    approach,
    states: entries,
}, null, 0));
console.error(`flags ${W}x${H}x${LEVELS}, ${doors.length} doors, ${stairs.length} stairs, ${entries.length} door states`);
