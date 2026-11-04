// Package packet ports rsb0t-main's src/client/io/Packet.ts.
//
// The TS original wraps a fixed-size Uint8Array/DataView and an object-pool cache
// (Packet.alloc/release) sized for a browser's GC pressure under a real-time render
// loop. This port keeps the read/write primitives faithful but drops the pooling —
// Go's GC and a bot client's much lower allocation rate don't need it; NewReader wraps
// an already-received byte slice, NewWriter starts an auto-growing buffer.
package packet

import (
	"encoding/binary"
	"hash/crc32"

	"rsb0t-go/internal/isaac"
	"rsb0t-go/internal/rsa"

	"math/big"
)

// bitmask ports Packet.bitmask: bitmask[i] = (1<<i)-1 for i in 0..31, bitmask[32] = all-ones.
// Used only by Bit, for the same bit-level reads (movement masks etc.) the TS client uses it for.
var bitmask = func() [33]uint32 {
	var m [33]uint32
	for i := 0; i < 32; i++ {
		m[i] = (uint32(1) << i) - 1
	}
	m[32] = 0xffffffff
	return m
}()

// Packet is a read/write cursor over a byte buffer, matching Packet.ts's single class
// that serves both directions. Random is the ISAAC cipher used by P1Enc for outgoing
// opcode obfuscation — see isaaccipher/ (not yet ported) for how it's wired up for
// incoming opcode de-obfuscation, which is the caller's responsibility, not this type's.
type Packet struct {
	Data   []byte
	Pos    int
	BitPos int
	Random *isaac.Cipher
}

// NewReader wraps an already-populated byte slice for reading — mirrors `new Packet(src)`.
func NewReader(data []byte) *Packet {
	return &Packet{Data: data}
}

// NewWriter starts an empty, auto-growing buffer for building an outgoing packet.
// capacity is just a hint (like TS's Packet.alloc size classes), not a hard limit.
func NewWriter(capacity int) *Packet {
	return &Packet{Data: make([]byte, 0, capacity)}
}

// Available mirrors the `available` getter: bytes left to read.
func (p *Packet) Available() int {
	return len(p.Data) - p.Pos
}

// ensure grows Data (for writers) so indices [Pos, Pos+n) are valid to write into,
// zero-filling any newly added bytes. Read-only callers never need this — their Data is
// already fully populated — so this is only invoked from write methods.
func (p *Packet) ensure(n int) {
	need := p.Pos + n
	if need > len(p.Data) {
		p.Data = append(p.Data, make([]byte, need-len(p.Data))...)
	}
}

// ---- reads ----

// G1 reads an unsigned byte. (g1)
func (p *Packet) G1() uint8 {
	v := p.Data[p.Pos]
	p.Pos++
	return v
}

// G1b reads a signed byte. (g1b)
func (p *Packet) G1b() int8 {
	v := int8(p.Data[p.Pos])
	p.Pos++
	return v
}

// G2 reads a big-endian unsigned 16-bit value. (g2)
func (p *Packet) G2() uint16 {
	v := binary.BigEndian.Uint16(p.Data[p.Pos:])
	p.Pos += 2
	return v
}

// G2b reads a big-endian signed 16-bit value. (g2b)
func (p *Packet) G2b() int16 {
	return int16(p.G2())
}

// G3 reads a big-endian 24-bit value: one byte then a big-endian uint16, combined as
// (byte0<<16)|uint16 — ported exactly from g3's slightly unusual read order (the byte
// read advances Pos first, then the uint16 read happens at that already-advanced
// position, and only afterward does Pos advance past the uint16).
func (p *Packet) G3() uint32 {
	b0 := uint32(p.Data[p.Pos])
	p.Pos++
	rest := uint32(binary.BigEndian.Uint16(p.Data[p.Pos:]))
	p.Pos += 2
	return (b0 << 16) | rest
}

// G4 reads a big-endian signed 32-bit value. (g4)
func (p *Packet) G4() int32 {
	v := int32(binary.BigEndian.Uint32(p.Data[p.Pos:]))
	p.Pos += 4
	return v
}

// G8 reads a big-endian signed 64-bit value. (g8)
func (p *Packet) G8() int64 {
	v := int64(binary.BigEndian.Uint64(p.Data[p.Pos:]))
	p.Pos += 8
	return v
}

// GSmarts reads a RuneScript "smart" signed varint: peeks the next byte — if it's under
// 0x80, consume 1 byte and subtract 0x40; otherwise consume 2 bytes and subtract 0xc000.
// (gsmarts)
func (p *Packet) GSmarts() int32 {
	if p.Data[p.Pos] < 0x80 {
		return int32(p.G1()) - 0x40
	}
	return int32(p.G2()) - 0xc000
}

// GSmart reads the unsigned counterpart: 1 byte as-is, or 2 bytes minus 0x8000. (gsmart)
func (p *Packet) GSmart() int32 {
	if p.Data[p.Pos] < 0x80 {
		return int32(p.G1())
	}
	return int32(p.G2()) - 0x8000
}

// GJStr reads a newline (0x0A)-terminated string, one byte per character (Latin-1-style,
// not UTF-8 decoded) — matching the client's raw RS string encoding. (gjstr)
func (p *Packet) GJStr() string {
	length := len(p.Data)
	buf := make([]byte, 0, 16)
	for p.Pos < length {
		b := p.Data[p.Pos]
		p.Pos++
		if b == 10 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

// Skip advances Pos by n bytes without reading them — for fixed-size fields a reader
// has no use for but must still consume to stay aligned with the writer.
func (p *Packet) Skip(n int) {
	p.Pos += n
}

// GData copies length bytes starting at Pos into dest[offset:], advancing Pos. (gdata)
func (p *Packet) GData(length, offset int, dest []byte) {
	copy(dest[offset:offset+length], p.Data[p.Pos:p.Pos+length])
	p.Pos += length
}

// ---- writes ----

// P1Enc ISAAC-obfuscates and writes an outgoing opcode byte: wire = (opcode + isaac.NextInt) & 0xff.
// Converting the int32 sum to byte truncates to the low 8 bits, which is exactly what
// `& 0xff` does in the original — see isaaccipher/ for why only the opcode byte, never
// the payload, goes through this. (p1Enc)
func (p *Packet) P1Enc(opcode byte) {
	var next int32
	if p.Random != nil {
		next = p.Random.NextInt()
	}
	p.ensure(1)
	p.Data[p.Pos] = byte(int32(opcode) + next)
	p.Pos++
}

// P1 writes a raw byte. (p1)
func (p *Packet) P1(value byte) {
	p.ensure(1)
	p.Data[p.Pos] = value
	p.Pos++
}

// P2 writes a big-endian uint16. (p2)
func (p *Packet) P2(value uint16) {
	p.ensure(2)
	binary.BigEndian.PutUint16(p.Data[p.Pos:], value)
	p.Pos += 2
}

// IP2 writes a little-endian uint16. (ip2)
func (p *Packet) IP2(value uint16) {
	p.ensure(2)
	binary.LittleEndian.PutUint16(p.Data[p.Pos:], value)
	p.Pos += 2
}

// P3 writes a big-endian 24-bit value: high byte then big-endian uint16. (p3)
func (p *Packet) P3(value uint32) {
	p.ensure(3)
	p.Data[p.Pos] = byte(value >> 16)
	p.Pos++
	binary.BigEndian.PutUint16(p.Data[p.Pos:], uint16(value))
	p.Pos += 2
}

// P4 writes a big-endian int32. (p4)
func (p *Packet) P4(value int32) {
	p.ensure(4)
	binary.BigEndian.PutUint32(p.Data[p.Pos:], uint32(value))
	p.Pos += 4
}

// IP4 writes a little-endian int32. (ip4)
func (p *Packet) IP4(value int32) {
	p.ensure(4)
	binary.LittleEndian.PutUint32(p.Data[p.Pos:], uint32(value))
	p.Pos += 4
}

// P8 writes a big-endian int64. (p8)
func (p *Packet) P8(value int64) {
	p.ensure(8)
	binary.BigEndian.PutUint64(p.Data[p.Pos:], uint64(value))
	p.Pos += 8
}

// PJStr writes str one byte per character (Latin-1-style — callers must only pass
// strings whose characters are all <= 0xff, true for the ASCII usernames/passwords this
// is used for) followed by a trailing newline (0x0A) terminator. (pjstr)
func (p *Packet) PJStr(str string) {
	p.ensure(len(str) + 1)
	for i := 0; i < len(str); i++ {
		p.Data[p.Pos] = str[i]
		p.Pos++
	}
	p.Data[p.Pos] = 10
	p.Pos++
}

// PData copies src[offset:offset+length] to Pos, advancing Pos. (pdata)
func (p *Packet) PData(src []byte, offset, length int) {
	p.ensure(length)
	copy(p.Data[p.Pos:], src[offset:offset+length])
	p.Pos += length
}

// PSize1 backpatches a 1-byte length placeholder written `size` bytes + 1 byte ago —
// i.e. at Pos-size-1 — with the actual size, once it's known. Used for VARBYTE-framed
// outgoing packets: write a placeholder byte, write the payload, then call this. (psize1)
func (p *Packet) PSize1(size int) {
	p.Data[p.Pos-size-1] = byte(size)
}

// ---- bit-level reads ----

// GBitStart mirrors gBitStart: seed the bit cursor from the current byte position.
func (p *Packet) GBitStart() {
	p.BitPos = p.Pos << 3
}

// GBitEnd mirrors gBitEnd: round the byte position back up from the bit cursor.
func (p *Packet) GBitEnd() {
	p.Pos = (p.BitPos + 7) >> 3
}

// GBit reads n bits (n <= 32) starting at the current bit cursor, ported byte-for-byte
// from gBit's cross-byte-boundary accumulation loop. (gBit)
func (p *Packet) GBit(n int) uint32 {
	bytePos := p.BitPos >> 3
	remaining := 8 - (p.BitPos & 7)
	var value uint32
	p.BitPos += n

	for n > remaining {
		value += (uint32(p.Data[bytePos]) & bitmask[remaining]) << uint(n-remaining)
		bytePos++
		n -= remaining
		remaining = 8
	}

	if n == remaining {
		value += uint32(p.Data[bytePos]) & bitmask[remaining]
	} else {
		value += (uint32(p.Data[bytePos]) >> uint(remaining-n)) & bitmask[n]
	}

	return value
}

// ---- CRC32 ----

// GetCRC ports Packet.getcrc: the standard reflected IEEE CRC-32 (poly 0xedb88320,
// init 0xffffffff, final complement) over src[offset:length] — note "length" here is an
// exclusive end INDEX, not a byte count, matching the original's (confusingly named)
// parameter. Cross-checked in packet_test.go against Go's stdlib crc32.ChecksumIEEE,
// which uses the identical algorithm — kept as an explicit, from-scratch port rather
// than a stdlib call so this protocol implementation stays self-contained and auditable.
func GetCRC(src []byte, offset, length int) uint32 {
	crc := uint32(0xffffffff)
	for i := offset; i < length; i++ {
		crc = (crc >> 8) ^ crc32.IEEETable[(crc^uint32(src[i]))&0xff]
	}
	return ^crc
}

// CheckCRC ports Packet.checkcrc.
func CheckCRC(src []byte, offset, length int, expected uint32) bool {
	return GetCRC(src, offset, length) == expected
}

// ---- RSA ----

// RSAEncrypt ports Packet.rsaenc: take everything written so far (bytes [0, Pos)),
// raw-RSA-encrypt it (see internal/rsa — no padding scheme, math/big modPow), reset to
// the start, and rewrite as a 1-byte length prefix followed by the ciphertext. Called
// once, after building the plaintext login block and before wrapping it in the outer
// login packet — see login.go.
func (p *Packet) RSAEncrypt(mod, exp *big.Int) {
	length := p.Pos
	plaintext := make([]byte, length)
	copy(plaintext, p.Data[:length])

	encrypted := rsa.Encrypt(plaintext, mod, exp)

	p.Pos = 0
	p.P1(byte(len(encrypted)))
	p.PData(encrypted, 0, len(encrypted))
}
