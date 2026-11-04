// Throwaway generator for internal/webwalk: runs the REAL TS webwalk code and prints either
// the compiled transport graph internal/webwalk embeds, or the routes the TS PathFinder plans
// over the real collision pack, which the Go planner's tests compare against.
// Not part of the build.
//
//   RSB0T_TS=<rsb0t TS checkout> bun webwalk_reference_vector.ts graph > internal/webwalk/graph.json
//   RSB0T_TS=<rsb0t TS checkout> RSB0T_COLLISION_PACK=<collision.lcnav.gz> \
//       bun webwalk_reference_vector.ts routes > test/fixtures/webwalk_routes.json
//
// "graph" is exactly what PathFinder.addEdges compiles from doors.json, transports.json (after the
// state-aware activations), the curated travel catalog and stairEdges.json, with every edge kept
// (walkable filtering is left to the Go planner, which has the pack) and each edge's
// plan-time requires and special crossing resolved the way PathFinder and WalkExecutor do.
import { readFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
import { pathToFileURL } from 'node:url';
import { execSync } from 'node:child_process';

const ts = process.env.RSB0T_TS;
if (!ts) {
    throw new Error('set RSB0T_TS to the rsb0t TS checkout');
}
const mod = (p: string): Promise<any> => import(pathToFileURL(`${ts}/src/bot/event/webwalk/${p}`).href);

const { PathFinder } = await mod('PathFinder.ts');
const { loadDefaultNavEdges } = await mod('loadTransportGraph.ts');
const { specialCrossingForTransport } = await mod('data/specialCrossings.ts');

const nodeX = (id: number): number => (id >> 14) & 0x3fff;
const nodeZ = (id: number): number => id & 0x3fff;
const nodeLevel = (id: number): number => (id >> 28) & 0x3;

function commit(): string {
    try {
        return execSync('git rev-parse HEAD', { cwd: ts }).toString().trim();
    } catch {
        return 'unknown';
    }
}

function graph(): void {
    // An empty pack plus an always-walkable override keeps every edge; the Go planner filters.
    const empty = new Uint8Array([0x4c, 0x43, 0x4e, 0x56, 2, 1, 0, 0, 0, 0]);
    const finder = new PathFinder(empty);
    finder.walkable = (): boolean => true;
    loadDefaultNavEdges(finder);
    const rows: string[] = [];
    const keys = [...finder.edges.keys()].sort((a: number, b: number) => a - b);
    for (const key of keys) {
        for (const e of finder.edges.get(key)) {
            const from = { x: nodeX(key), z: nodeZ(key), level: nodeLevel(key) };
            const to = { x: nodeX(e.to), z: nodeZ(e.to), level: nodeLevel(e.to) };
            const t = e.transport;
            const out: Record<string, unknown> = {
                from,
                to,
                kind: e.kind ?? t.kind ?? 'other',
                cost: e.cost,
                locName: t.locName,
                action: t.action,
                locX: t.locX,
                locZ: t.locZ
            };
            if (t.locId !== undefined) out.locId = t.locId;
            if (t.openLocId !== undefined) out.openLocId = t.openLocId;
            if (t.toTile !== undefined) out.landsOnTile = true;
            if (t.acceptAnyLanding) out.acceptAnyLanding = true;
            if (e.requires) out.requires = e.requires;
            const sc = specialCrossingForTransport(t, from, to);
            if (sc) {
                const special: Record<string, unknown> = { label: sc.label };
                if (sc.npc) special.npc = sc.npc;
                if (sc.dialogue?.choose) special.choose = sc.dialogue.choose;
                if (sc.useItem) special.useItem = sc.useItem.name;
                if (sc.exactApproach) special.exactApproach = true;
                out.special = special;
            }
            rows.push(JSON.stringify(out));
        }
    }
    console.log(`{"source":${JSON.stringify(`rsb0t ${commit()} webwalk PathFinder.addEdges`)},"edges":[\n${rows.join(',\n')}\n]}`);
}

// Routes the Go tests plan too. Keep in step with internal/webwalk/realpack_test.go.
export const ROUTES: { name: string; from: number[]; to: number[] }[] = [
    { name: 'catherby bank to fishing', from: [2809, 3441, 0], to: [2837, 3432, 0] },
    { name: 'catherby fishing to bank', from: [2845, 3431, 0], to: [2809, 3441, 0] },
    { name: 'seers field to wheel', from: [2741, 3444, 0], to: [2711, 3471, 1] },
    { name: 'seers wheel to bank', from: [2711, 3471, 1], to: [2725, 3493, 0] },
    { name: 'seers bank to field', from: [2725, 3493, 0], to: [2741, 3444, 0] },
    { name: 'lumbridge to draynor', from: [3222, 3218, 0], to: [3093, 3244, 0] },
    { name: 'port sarim to karamja', from: [3029, 3217, 0], to: [2924, 3152, 0] },
    { name: 'ardougne dock deck to catherby', from: [2683, 3268, 1], to: [2809, 3441, 0] },
    { name: 'ardougne gap 2615,3292,1', from: [2615, 3292, 1], to: [2809, 3441, 0] },
    { name: 'ardougne gap 2537,3089,0', from: [2537, 3089, 0], to: [2809, 3441, 0] },
    { name: 'ardougne gap 2595,3440,0', from: [2595, 3440, 0], to: [2809, 3441, 0] },
    { name: 'ardougne gap 2617,3207,1', from: [2617, 3207, 1], to: [2809, 3441, 0] },
    { name: 'large door 2656,3161', from: [2656, 3161, 0], to: [2809, 3441, 0] },
    { name: 'port sarim ladder top', from: [2955, 3390, 2], to: [3029, 3217, 0] },
    { name: 'brimhaven pier area', from: [2976, 3325, 0], to: [3029, 3217, 0] }
];

function routes(): void {
    const packPath = process.env.RSB0T_COLLISION_PACK;
    if (!packPath) {
        throw new Error('set RSB0T_COLLISION_PACK');
    }
    let bytes = new Uint8Array(readFileSync(packPath));
    if (bytes[0] === 0x1f && bytes[1] === 0x8b) {
        bytes = new Uint8Array(gunzipSync(bytes));
    }
    const finder = new PathFinder(bytes);
    loadDefaultNavEdges(finder);
    const out = ROUTES.map(r => {
        const from = { x: r.from[0], z: r.from[1], level: r.from[2] };
        const to = { x: r.to[0], z: r.to[1], level: r.to[2] };
        const res = finder.findPath(from, to);
        if (!res.ok) {
            return { name: r.name, from, to, ok: false, reason: res.reason };
        }
        const last = res.waypoints[res.waypoints.length - 1];
        return {
            name: r.name,
            from,
            to,
            ok: true,
            cost: res.cost,
            end: { x: last.x, z: last.z, level: last.level },
            hops: res.hops.map((h: any) => ({ kind: h.kind, action: h.action, locName: h.locName, locId: h.locId, from: h.from, to: h.to }))
        };
    });
    console.log(JSON.stringify({ source: `rsb0t ${commit()} PathFinder.findPath, no WorldState, no policy`, routes: out }, null, 1));
}

const mode = process.argv[2];
if (mode === 'graph') {
    graph();
} else if (mode === 'routes') {
    routes();
} else {
    throw new Error('usage: bun webwalk_reference_vector.ts graph|routes');
}
