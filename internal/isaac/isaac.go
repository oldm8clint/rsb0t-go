// Package isaac ports rsb0t-main's src/client/io/Isaac.ts line-for-line.
//
// The original is a straight port of Bob Jenkins' ISAAC PRNG, used by the client to
// obfuscate each packet's opcode byte (see rsa/loginkey.go and the anticheat/protocol
// packages for how the two Cipher instances — outgoing and incoming — are seeded and
// used). Every arithmetic op here uses Go's native int32 wraparound, which is bit-for-bit
// equivalent to the TS source's use of Int32Array storage plus JS's ToInt32 coercion on
// every bitwise operator — see the doc comment on Cipher for why that equivalence holds.
package isaac

// Cipher is a straight port of the vendored client's Isaac class.
//
// The TS source stores rsl/mem in Int32Array (auto-truncating on every write) but keeps
// a, b, c as plain JS `number` fields. That looks like a hazard — a plain `+=` on a JS
// number is not truncated to 32 bits the way an Int32Array write is — but every place
// those fields are later read, they're read through a bitwise operator (^, <<, >>>) or
// written into an Int32Array slot, and JS bitwise operators always coerce their operand
// to Int32 before use regardless of the operand's actual magnitude. So the "untruncated"
// intermediate value in JS is never observed except through an operation that truncates
// it anyway — using genuine int32 fields throughout in Go (which wrap on overflow the
// same way) produces identical results without needing to replicate that subtlety.
type Cipher struct {
	count   int
	rsl     [256]int32
	mem     [256]int32
	a, b, c int32
}

// NewCipher seeds a Cipher the way `new Isaac(seed)` does: copy as many seed words as
// given into rsl (zero-padding the rest), then run the standard ISAAC init mix.
func NewCipher(seed []int32) *Cipher {
	c := &Cipher{}
	copy(c.rsl[:], seed)
	c.init()
	return c
}

// NextInt is the `nextInt` getter: pop a pre-generated value, regenerating the block
// every 256 pulls. The order here matters — JS's `count-- === 0` compares the OLD value
// then decrements unconditionally, which is what old/dec below replicates.
func (c *Cipher) NextInt() int32 {
	old := c.count
	c.count--
	if old == 0 {
		c.mix()
		c.count = 255
	}
	return c.rsl[c.count]
}

// ushr replicates JS's `>>>` (logical/unsigned right shift) on a 32-bit value; Go's `>>`
// on a signed int32 is arithmetic (sign-extending), so shifts ported from `>>>` must go
// through uint32 instead.
func ushr(x int32, n uint) int32 {
	return int32(uint32(x) >> n)
}

// goldenRatio is 0x9e3779b9 as a signed 32-bit two's complement value. Go's constant
// conversions check representability (unlike JS's implicit ToInt32 wraparound on every
// bitwise op), so the literal hex form overflows int32 at compile time — this is its
// wrapped equivalent, the value ISAAC actually operates on either way.
const goldenRatio int32 = -1640531527

func (c *Cipher) init() {
	var a, b, cc, d, e, f, g, h int32 = goldenRatio, goldenRatio, goldenRatio, goldenRatio, goldenRatio, goldenRatio, goldenRatio, goldenRatio

	mix := func() {
		a ^= b << 11
		d += a
		b += cc
		b ^= ushr(cc, 2)
		e += b
		cc += d
		cc ^= d << 8
		f += cc
		d += e
		d ^= ushr(e, 16)
		g += d
		e += f
		e ^= f << 10
		h += e
		f += g
		f ^= ushr(g, 4)
		a += f
		g += h
		g ^= h << 8
		b += g
		h += a
		h ^= ushr(a, 9)
		cc += h
		a += b
	}

	for i := 0; i < 4; i++ {
		mix()
	}

	for i := 0; i < 256; i += 8 {
		a += c.rsl[i]
		b += c.rsl[i+1]
		cc += c.rsl[i+2]
		d += c.rsl[i+3]
		e += c.rsl[i+4]
		f += c.rsl[i+5]
		g += c.rsl[i+6]
		h += c.rsl[i+7]

		mix()

		c.mem[i] = a
		c.mem[i+1] = b
		c.mem[i+2] = cc
		c.mem[i+3] = d
		c.mem[i+4] = e
		c.mem[i+5] = f
		c.mem[i+6] = g
		c.mem[i+7] = h
	}

	for i := 0; i < 256; i += 8 {
		a += c.mem[i]
		b += c.mem[i+1]
		cc += c.mem[i+2]
		d += c.mem[i+3]
		e += c.mem[i+4]
		f += c.mem[i+5]
		g += c.mem[i+6]
		h += c.mem[i+7]

		mix()

		c.mem[i] = a
		c.mem[i+1] = b
		c.mem[i+2] = cc
		c.mem[i+3] = d
		c.mem[i+4] = e
		c.mem[i+5] = f
		c.mem[i+6] = g
		c.mem[i+7] = h
	}

	c.mix()
	c.count = 256
}

// mix is the `isaac()` method: one full 256-word shuffle, refilling rsl.
func (c *Cipher) mix() {
	c.c++
	c.b += c.c

	for i := 0; i < 256; i++ {
		x := c.mem[i]

		switch i & 3 {
		case 0:
			c.a ^= c.a << 13
		case 1:
			c.a ^= ushr(c.a, 6)
		case 2:
			c.a ^= c.a << 2
		case 3:
			c.a ^= ushr(c.a, 16)
		}

		c.a += c.mem[(i+128)&0xff]

		y := c.mem[ushr(x, 2)&0xff] + c.a + c.b
		c.mem[i] = y
		c.b = c.mem[ushr(ushr(y, 8), 2)&0xff] + x
		c.rsl[i] = c.b
	}
}
