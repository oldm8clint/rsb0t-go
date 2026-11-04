// Throwaway generator, not part of the build: runs the REAL vendored ObjType/NpcType/LocType
// decoders over a config archive and writes test/fixtures/cachecfg_vector.json, the ground
// truth internal/cachecfg is checked against.
//
// Run from an rsb0t checkout (so its bunfig DOM preload applies):
//   cd <rsb0t> && RSB0T_TS_ROOT=<rsb0t> RSB0T_CONFIG_JAG=<config archive> \
//     bun test <this repo>/cachecfg_reference_vector.test.ts
// The archive is the engine's GET /config<crc> (crc = /crc0 index 2).
import { mock, test } from 'bun:test';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

const TS_ROOT = process.env.RSB0T_TS_ROOT ?? '';
const CONFIG = process.env.RSB0T_CONFIG_JAG ?? '';
const CLIENT = path.join(TS_ROOT, 'src', 'client');
const OUT = path.join(import.meta.dir, 'test', 'fixtures', 'cachecfg_vector.json');

mock.module(path.join(CLIENT, '3rdparty', 'audio.js'), () => ({ playWave: async () => {}, setWaveVolume: () => {} }));
mock.module(path.join(CLIENT, '3rdparty', 'tinymidipcm.js'), () => ({ playMidi: () => {}, setMidiVolume: () => {}, stopMidi: () => {} }));

const ops = (a: (string | null)[] | null): string => (a ? Array.from({ length: 5 }, (_, i) => a[i] ?? '').join('|') : '||||');
const b = (v: boolean): string => (v ? '1' : '0');

test('cachecfg reference vector', async () => {
    if (!TS_ROOT || !CONFIG) {
        throw new Error('set RSB0T_TS_ROOT and RSB0T_CONFIG_JAG');
    }
    const { default: JagFile } = await import(path.join(CLIENT, 'io', 'JagFile.ts'));
    const { default: ObjType } = await import(path.join(CLIENT, 'config', 'ObjType.ts'));
    const { default: NpcType } = await import(path.join(CLIENT, 'config', 'NpcType.ts'));
    const { default: LocType } = await import(path.join(CLIENT, 'config', 'LocType.ts'));

    const raw = fs.readFileSync(CONFIG);
    const jag = new JagFile(new Uint8Array(raw));
    ObjType.init(jag, true);
    NpcType.init(jag);
    LocType.init(jag);

    const lines = (count: number, line: (id: number) => string): string[] => Array.from({ length: count }, (_, id) => line(id));
    const objs = lines(ObjType.numDefinitions, id => {
        const o = ObjType.list(id);
        return [id, o.name ?? '', b(o.members), b(o.stackable), o.cost, o.certlink, o.certtemplate, ops(o.op), ops(o.iop)].join('\t');
    });
    const npcs = lines(NpcType.numDefinitions, id => {
        const n = NpcType.list(id);
        return [id, n.name ?? '', n.size, n.vislevel, ops(n.op)].join('\t');
    });
    const locs = lines(LocType.numDefinitions, id => {
        const l = LocType.list(id);
        return [id, l.name ?? '', l.width, l.length, b(l.blockwalk), b(l.blockrange), b(l.active), l.forceapproach, ops(l.op)].join('\t');
    });

    const digest = (all: string[]): string => crypto.createHash('sha256').update(all.join('\n') + '\n', 'utf8').digest('hex');
    const sample = (all: string[]): string[] => all.filter((_, id) => id < 5 || id % 97 === 0);
    const named = (all: string[], names: string[]): Record<string, number[]> =>
        Object.fromEntries(names.map(name => [name, all.filter(l => l.split('\t')[1].toLowerCase() === name.toLowerCase()).map(l => Number(l.split('\t')[0]))]));

    const vector = {
        archiveSHA256: crypto.createHash('sha256').update(raw).digest('hex'),
        obj: { count: objs.length, digest: digest(objs), sample: sample(objs), named: named(objs, ['Coins', 'Flax', 'Bow string', 'Shears', 'Ball of wool']) },
        npc: { count: npcs.length, digest: digest(npcs), sample: sample(npcs), named: named(npcs, ['Man', 'Banker', 'Sheep', 'Thessalia']) },
        loc: { count: locs.length, digest: digest(locs), sample: sample(locs), named: named(locs, ['Bank booth', 'Flax', 'Spinning wheel', 'Ladder']) }
    };
    fs.mkdirSync(path.dirname(OUT), { recursive: true });
    fs.writeFileSync(OUT, JSON.stringify(vector, null, 1) + '\n');
});
