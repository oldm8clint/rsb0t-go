// Throwaway script: generates known-answer RSA encryption vectors from the REAL
// vendored JsUtil.ts (bytesToBigInt/bigIntModPow/bigIntToBytes — the exact primitives
// Packet.rsaenc composes), so the Go port can be checked against ground truth.
import { bigIntModPow, bigIntToBytes, bytesToBigInt } from '../rsb0t-main/src/client/util/JsUtil.js';

function rsaEncBytes(plaintext: Uint8Array, mod: bigint, exp: bigint): Uint8Array {
    const raw = bytesToBigInt(plaintext);
    const enc = bigIntModPow(raw, exp, mod);
    return bigIntToBytes(enc);
}

// Case 1: small toy keypair (p=61, q=53 -> n=3233, e=17, d=2753 — the textbook RSA
// example), so the vector is also independently checkable by hand/other tools.
const toyMod = 3233n;
const toyExp = 17n;
const toyDec = 2753n; // private exponent, for round-trip sanity in this script itself
const toyPlain = new Uint8Array([65]); // 'A' — single byte, must be < mod
const toyEnc = rsaEncBytes(toyPlain, toyMod, toyExp);
// sanity: decrypt with the private exponent and confirm we get 65 back, before trusting the vector
const decrypted = bigIntModPow(bytesToBigInt(toyEnc), toyDec, toyMod);
if (decrypted !== 65n) throw new Error(`toy round-trip failed: got ${decrypted}`);

// Case 2: the actual local engine's real key (large modulus, large non-65537 exponent —
// the genuinely relevant case per docs/how-to/run-locally.md's warning about stock
// engines), with a realistic multi-byte plaintext shaped like a real login block prefix.
const localMod = 7162900525229798032761816791230527296329313291232324290237849263501208207972894053929065636522363163621000728841182238772712427862772219676577293600221789n;
const localExp = 58778699976184461502525193738213253649000149147835990136706041084440742975821n;
const localPlain = new TextEncoder().encode('hello rsb0t-go');
const localEnc = rsaEncBytes(localPlain, localMod, localExp);

console.log(JSON.stringify({
    toy: { mod: toyMod.toString(), exp: toyExp.toString(), plain: Array.from(toyPlain), enc: Array.from(toyEnc) },
    local: { mod: localMod.toString(), exp: localExp.toString(), plain: Array.from(localPlain), enc: Array.from(localEnc) }
}, null, 2));
