// Throwaway comparison script (Phase 0's opcode-drift check): calls the exported
// generateProt() from rsb0t-main's tools/gen-prot.ts against the freshly cloned local
// 289 engine, and diffs the result against the ALREADY-COMMITTED ClientProt.ts/
// ServerProt.ts — without writing anything, unlike running gen-prot.ts directly as a
// script (which overwrites those files in place).
import { generateProt } from '../rsb0t-main/tools/gen-prot.js';
import { readFileSync } from 'node:fs';

const engineDir = '../engine';
const generated = generateProt(engineDir);

const committedClient = readFileSync('../rsb0t-main/src/client/io/ClientProt.ts', 'utf8');
const committedServer = readFileSync('../rsb0t-main/src/client/io/ServerProt.ts', 'utf8');

console.log(`local engine revision: ${generated.revision}`);
console.log(`ClientProt.ts matches committed: ${generated.client === committedClient}`);
console.log(`ServerProt.ts matches committed: ${generated.server === committedServer}`);

function diffLines(label: string, a: string, b: string): void {
    const aLines = a.split('\n');
    const bLines = b.split('\n');
    const max = Math.max(aLines.length, bLines.length);
    let diffCount = 0;
    for (let i = 0; i < max; i++) {
        if (aLines[i] !== bLines[i]) {
            diffCount++;
            if (diffCount <= 15) {
                console.log(`  ${label} line ${i + 1}:\n    committed: ${aLines[i] ?? '(missing)'}\n    local-gen: ${bLines[i] ?? '(missing)'}`);
            }
        }
    }
    console.log(`  ${label}: ${diffCount} differing line(s)${diffCount > 15 ? ' (showing first 15)' : ''}`);
}

if (generated.client !== committedClient) {
    diffLines('ClientProt', committedClient, generated.client);
}
if (generated.server !== committedServer) {
    diffLines('ServerProt', committedServer, generated.server);
}
