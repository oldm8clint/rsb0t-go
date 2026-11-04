// Throwaway script: exercises the REAL vendored Packet.ts to capture known-answer
// vectors for the Go port's primitives, including the boundary-sensitive ones
// (g3's odd read order, gsmart/gsmarts's 0x80 threshold, the bit reader's cross-byte
// accumulation) that are easy to get subtly wrong in a port.
import Packet from '../rsb0t-main/src/client/io/Packet.js';

// ---- write primitives: build a buffer, capture the resulting bytes ----
const w = new Packet(new Uint8Array(64));
w.p1(0xab);
w.p2(0x1234);
w.ip2(0x1234);
w.p3(0x123456);
w.p4(-1); // exercises the signed int32 write path
w.ip4(-1);
w.p8(0x0102030405060708n);
w.pjstr('hi');
const writtenLen = w.pos;
const writtenBytes = Array.from(w.data.subarray(0, writtenLen));

// ---- gsmart / gsmarts boundary cases: value 0x7f (1-byte form) and 0x80 (2-byte form) ----
function smartBytes(fn: (p: Packet, v: number) => void, v: number): number[] {
    const p = new Packet(new Uint8Array(8));
    fn(p, v);
    return Array.from(p.data.subarray(0, p.pos));
}
// gsmart reads back what a real server would send for small (<0x80, 1 byte) and large
// (>=0x80, 2 bytes offset -0x8000) values — construct the raw bytes by hand since the
// class has no smart *writer*, only server-side encoders do; we just need read-side truth.
function readGSmart(bytes: number[]): number {
    const p = new Packet(new Uint8Array(bytes));
    return p.gsmart();
}
function readGSmarts(bytes: number[]): number {
    const p = new Packet(new Uint8Array(bytes));
    return p.gsmarts();
}
const gsmartCases = [
    { bytes: [0x00], want: readGSmart([0x00]) },
    { bytes: [0x7f], want: readGSmart([0x7f]) },
    { bytes: [0x80, 0x00], want: readGSmart([0x80, 0x00]) }, // 2-byte form, high bit forces it
    { bytes: [0xff, 0xff], want: readGSmart([0xff, 0xff]) }
];
const gsmartsCases = [
    { bytes: [0x40], want: readGSmarts([0x40]) }, // 1-byte form: 0x40 - 0x40 = 0
    { bytes: [0x00], want: readGSmarts([0x00]) }, // 1-byte form: 0 - 0x40 = -64
    { bytes: [0x80, 0x00], want: readGSmarts([0x80, 0x00]) },
    { bytes: [0xc0, 0x00], want: readGSmarts([0xc0, 0x00]) }
];

// ---- CRC32: the standard IEEE check value for the ASCII bytes "123456789" is the
// well-known 0xCBF43926, independent of this codebase entirely — a strong external
// cross-check that Packet.getcrc really is standard reflected CRC-32. ----
const crcInput = new TextEncoder().encode('123456789');
const crc = Packet.getcrc(crcInput, 0, crcInput.length) >>> 0; // unsigned view

// ---- bit reader: pack a few odd-width fields across a byte boundary and read them back ----
const bitBuf = new Uint8Array([0b10110100, 0b11001010, 0b00001111]);
const br = new Packet(bitBuf);
br.gBitStart();
const bits = [br.gBit(3), br.gBit(5), br.gBit(7), br.gBit(9)]; // 3+5+7+9=24 bits = 3 bytes exactly
br.gBitEnd();
const bitEndPos = br.pos;

console.log(JSON.stringify({
    written: { bytes: writtenBytes, len: writtenLen },
    gsmart: gsmartCases,
    gsmarts: gsmartsCases,
    crc32_of_123456789: crc,
    bits: { input: Array.from(bitBuf), values: bits, endPos: bitEndPos }
}, null, 2));
