// Package rsa ports the raw textbook-RSA math rsb0t-main's client uses for its login
// block (Packet.rsaenc in src/client/io/Packet.ts, composed from bytesToBigInt/
// bigIntModPow/bigIntToBytes in src/client/util/JsUtil.ts).
//
// Deliberately not Go's stdlib crypto/rsa: that package's Encrypt/Verify helpers assume
// a padding scheme (OAEP/PKCS1v15) and, per docs/how-to/run-locally.md, reject a stock
// LostCityRS engine's key outright because its exponent isn't a small standard value —
// the exact reason the original TS client bypasses Node/Bun's crypto module too. This
// does plain modPow via math/big, no padding beyond the one conditional leading 0x00
// byte the original adds.
package rsa

import "math/big"

// Encrypt reproduces Packet.rsaenc's math: interpret plaintext as a big-endian unsigned
// integer, raw-RSA it (no padding scheme), and re-emit as the minimal big-endian byte
// encoding — with one leading 0x00 byte prepended if the top byte's high bit is set, so
// a byte-oriented reader downstream never misreads the value as a two's-complement
// negative number. The 1-byte *length* of this result (not a fixed 2-byte length) is
// what the login packet prefixes it with — see login.go, not this package, since that's
// a login-packet-framing concern, not an RSA one.
func Encrypt(plaintext []byte, mod, exp *big.Int) []byte {
	raw := new(big.Int).SetBytes(plaintext)
	enc := new(big.Int).Exp(raw, exp, mod)
	out := enc.Bytes() // minimal big-endian, no leading zero for a nonzero value — matches bigIntToBytes' natural output
	if len(out) > 0 && out[0]&0x80 != 0 {
		padded := make([]byte, len(out)+1)
		copy(padded[1:], out)
		out = padded
	}
	return out
}
