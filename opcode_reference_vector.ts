// Throwaway script: parses the ALREADY-COMMITTED, already-verified-correct
// ClientProt.ts/ServerProt.ts enum text directly (not re-running gen-prot.ts) to produce
// a ground-truth name->value fixture for the Go generator's own output to be checked
// against — avoiding manual transcription of 151 opcode values into a fixture by hand.
import { readFileSync } from 'node:fs';

function parseEnum(source: string): Record<string, number> {
    const out: Record<string, number> = {};
    const re = /^\s*([A-Z0-9_]+)\s*=\s*(-?\d+),?\s*$/gm;
    for (const m of source.matchAll(re)) {
        out[m[1]] = Number(m[2]);
    }
    return out;
}

const clientSrc = readFileSync('../rsb0t-main/src/client/io/ClientProt.ts', 'utf8');
const serverSrc = readFileSync('../rsb0t-main/src/client/io/ServerProt.ts', 'utf8');

const client = parseEnum(clientSrc);
const server = parseEnum(serverSrc);

// Import the real committed array directly rather than re-parsing its text by regex —
// this is the actual TS values, verbatim, no transcription risk either way.
const { ServerProtSizes } = await import('../rsb0t-main/src/client/io/ServerProt.js');

console.log(JSON.stringify({
    clientCount: Object.keys(client).length,
    serverCount: Object.keys(server).length,
    client,
    server,
    serverProtSizes: ServerProtSizes
}, null, 2));
