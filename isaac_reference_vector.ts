// Throwaway script: generates a known-answer vector from the REAL vendored Isaac.ts
// for a fixed test seed, so the Go port can be checked against ground truth.
// Not part of the build; kept here only as the source of test/fixtures/isaac_vector.json.
import Isaac from '../rsb0t-main/src/client/io/Isaac.js';

const seed = new Int32Array([1, 2, 3, 4]);
const isaac = new Isaac(seed);

const values: number[] = [];
for (let i = 0; i < 20; i++) {
    values.push(isaac.nextInt);
}

console.log(JSON.stringify({ seed: Array.from(seed), values }, null, 2));
