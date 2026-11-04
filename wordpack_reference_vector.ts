// Throwaway script: known-answer vectors for WordPack, the nibble text compression public chat
// (PLAYER_INFO's CHAT mask) and private messages (MESSAGE_PRIVATE) carry. For each input the
// server side is Engine-TS 289's own WordPack.pack (what MessagePublicHandler and
// MessagePrivateEncoder write), and the text is what the vendored client's WordPack.unpack reads
// back (Client.ts's CHAT mask ~8132 and MESSAGE_PRIVATE ~6717). The client's own pack output is
// recorded too.
//
//   RSB0T_TS=<rsb0t TS checkout> RSB0T_ENGINE=<Engine-TS 289 checkout> \
//     bun wordpack_reference_vector.ts > test/fixtures/wordpack_vector.json
const TS = process.env.RSB0T_TS;
const ENGINE = process.env.RSB0T_ENGINE;
if (!TS || !ENGINE) {
    throw new Error('set RSB0T_TS and RSB0T_ENGINE');
}

const ClientPacket = (await import(TS + '/src/client/io/Packet.ts')).default;
const ClientWordPack = (await import(TS + '/src/client/wordfilter/WordPack.ts')).default;
const EnginePacket = (await import(ENGINE + '/src/io/Packet.ts')).default;
const EngineWordPack = (await import(ENGINE + '/src/wordenc/WordPack.ts')).default;

const inputs = [
    'hello',
    'Hello World',
    'buying lobsters 250 ea',
    'wow. that is great! yes? ok',
    'selling 1k coins (cheap) - pm me @ lumbridge #1 +/=',
    "don't \\ \"quote\" [me] 50% $5 & *",
    'price is £5',
    'unknown ~ chars ^ map to space',
    'qqqq zzzz xxxx jjjj',
    'a'.repeat(90),
    ''
];

function bytesOf(packet: { data: Uint8Array; pos: number }): number[] {
    return Array.from(packet.data.subarray(0, packet.pos));
}

const out = [];
for (const input of inputs) {
    const engine = new EnginePacket(new Uint8Array(256));
    EngineWordPack.pack(engine, input);
    const packed = bytesOf(engine);

    const client = new ClientPacket(new Uint8Array(256));
    ClientWordPack.pack(client, input);
    const clientPacked = bytesOf(client);

    const reader = new ClientPacket(new Uint8Array(packed));
    const unpacked = ClientWordPack.unpack(reader, packed.length);

    out.push({ input, packed, clientPacked, unpacked });
}

console.log(JSON.stringify(out, null, 2));
