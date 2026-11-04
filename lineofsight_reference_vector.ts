// Throwaway script: generates a known-answer vector from the REAL Engine-TS line of sight
// (src/engine/routefinder/LineValidator.ts hasLineOfSight, the check Player.inApproachDistance
// makes before a spell is cast) over a seeded random grid of projectile-blocking walls and full
// blockers, so scarabplan.LineOfSight can be checked against it tile for tile.
// Not part of the build; kept here only as the source of test/fixtures/lineofsight_vector.json.
//   bun lineofsight_reference_vector.ts > test/fixtures/lineofsight_vector.json
import CollisionEngine from '../engine/src/engine/routefinder/CollisionEngine.js';
import { CollisionFlag } from '../engine/src/engine/routefinder/flags.js';
import { hasLineOfSight } from '../engine/src/engine/routefinder/LineValidator.js';

let state = 0x2c9277b5;
function rand(n: number): number {
    // xorshift32, deterministic across runs
    state ^= state << 13;
    state >>>= 0;
    state ^= state >>> 17;
    state ^= state << 5;
    state >>>= 0;
    return state % n;
}

const X0 = 3200, Z0 = 3200, SIZE = 32;
const flags = new CollisionEngine();
for (let x = X0 - 8; x < X0 + SIZE + 8; x++) {
    for (let z = Z0 - 8; z < Z0 + SIZE + 8; z++) {
        flags.allocateIfAbsent(x, z, 0);
    }
}
// Sight bits as scarabplan.Sight numbers them: 1 north, 2 east, 4 south, 8 west, 16 full.
const grid: number[][] = [];
for (let x = X0; x < X0 + SIZE; x++) {
    for (let z = Z0; z < Z0 + SIZE; z++) {
        let sight = 0;
        let f = 0;
        const r = rand(100);
        if (r < 6) {
            f |= CollisionFlag.LOC_PROJ_BLOCKER;
            sight |= 16;
        } else if (r < 16) {
            const side = rand(4);
            f |= [CollisionFlag.WALL_NORTH_PROJ_BLOCKER, CollisionFlag.WALL_EAST_PROJ_BLOCKER, CollisionFlag.WALL_SOUTH_PROJ_BLOCKER, CollisionFlag.WALL_WEST_PROJ_BLOCKER][side];
            sight |= 1 << side;
        }
        if (f !== 0) {
            flags.add(x, z, 0, f);
            grid.push([x, z, sight]);
        }
    }
}
const cases: number[][] = [];
for (let i = 0; i < 4000; i++) {
    const sx = X0 + rand(SIZE), sz = Z0 + rand(SIZE);
    const dx = X0 + rand(SIZE), dz = Z0 + rand(SIZE);
    const los = hasLineOfSight(flags, 0, sx, sz, dx, dz, 1, 1, 1, 1, 0);
    cases.push([sx, sz, dx, dz, los ? 1 : 0]);
}
console.log(JSON.stringify({ grid, cases }));
