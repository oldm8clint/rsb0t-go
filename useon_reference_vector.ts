// Throwaway script: known-answer vectors for the "use held item on X" packets
// (OPNPCU, OPHELDU, OPOBJU, OPLOCU). Each payload is written with the vendored client's
// real Packet.ts in the exact field order Client.ts's doAction uses (USEHELD_ONNPC ~8960,
// USEHELD_ONHELD ~9279, USEHELD_ONOBJ ~8874, USEHELD_ONLOC ~9021 via interactWithLoc),
// then decoded by Engine-TS 289's own client-message decoders, so the fixture records both
// the bytes and what the server reads out of them.
//
// Paths differ between machines, so both checkouts come from the environment:
//   RSB0T_TS=<rsb0t TS checkout> RSB0T_ENGINE=<Engine-TS 289 checkout> \
//     bun useon_reference_vector.ts > test/fixtures/useon_vector.json
const TS = process.env.RSB0T_TS;
const ENGINE = process.env.RSB0T_ENGINE;
if (!TS || !ENGINE) {
    throw new Error('set RSB0T_TS and RSB0T_ENGINE');
}

const ClientPacket = (await import(TS + '/src/client/io/Packet.ts')).default;
const EnginePacket = (await import(ENGINE + '/src/io/Packet.ts')).default;
const ClientGameProt = (await import(ENGINE + '/src/network/game/client/ClientGameProt.ts')).default;

async function decoder(name: string) {
    const Dec = (await import(`${ENGINE}/src/network/game/client/codec/${name}Decoder.ts`)).default;
    return new Dec();
}

type Case = { name: string; fields: Record<string, number>; order: string[] };

const cases: Case[] = [
    // Client.ts: p2(npc index) p2(objComId) p2(objSelectedSlot) p2(objSelectedComId)
    { name: 'OPNPCU', fields: { npc: 1234, useObj: 1925, useSlot: 3, useCom: 3214 }, order: ['npc', 'useObj', 'useSlot', 'useCom'] },
    // Client.ts: p2(target obj) p2(target slot) p2(target com) p2(objComId) p2(objSelectedSlot) p2(objSelectedComId)
    { name: 'OPHELDU', fields: { obj: 590, slot: 27, com: 3214, useObj: 1511, useSlot: 0, useCom: 3214 }, order: ['obj', 'slot', 'com', 'useObj', 'useSlot', 'useCom'] },
    // Client.ts: p2(x) p2(z) p2(ground obj) p2(objComId) p2(objSelectedSlot) p2(objSelectedComId)
    { name: 'OPOBJU', fields: { x: 3222, z: 3218, obj: 1511, useObj: 590, useSlot: 5, useCom: 3214 }, order: ['x', 'z', 'obj', 'useObj', 'useSlot', 'useCom'] },
    // interactWithLoc: p2(x) p2(z) p2(loc id), then p2(objComId) p2(objSelectedSlot) p2(objSelectedComId)
    { name: 'OPLOCU', fields: { x: 2711, z: 3471, loc: 2644, useObj: 1779, useSlot: 12, useCom: 3214 }, order: ['x', 'z', 'loc', 'useObj', 'useSlot', 'useCom'] }
];

const decoders: Record<string, string> = { OPNPCU: 'OpNpcU', OPHELDU: 'OpHeldU', OPOBJU: 'OpObjU', OPLOCU: 'OpLocU' };

const out = [];
for (const c of cases) {
    const w = new ClientPacket(new Uint8Array(32));
    for (const key of c.order) {
        w.p2(c.fields[key]);
    }
    const bytes = Array.from(w.data.subarray(0, w.pos));
    const prot = ClientGameProt[c.name];
    const decoded = (await decoder(decoders[c.name])).decode(new EnginePacket(new Uint8Array(bytes)));
    out.push({ name: c.name, opcode: prot.id, length: prot.length, fields: c.fields, bytes, decoded: { ...decoded } });
}

console.log(JSON.stringify(out, null, 2));
