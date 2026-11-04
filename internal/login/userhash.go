package login

import "strings"

// toUserhash ports JString.toUserhash (src/client/datastruct/JString.ts): a base-37
// encoding of up to the first 12 characters (A-Z/a-z -> 1-26, 0-9 -> 27-36, else 0),
// with trailing "unmapped" factors of 37 divided back out. Only used here to derive the
// 5-bit loginServer selector from (hash>>16)&0x1f — see performLogin's session-init step.
//
// The TS original uses bigint; the result provably fits in uint64 (max possible value
// for 12 base-37 digits is 37^12-1 ≈ 6.58e18, well under uint64's ~1.84e19 ceiling), so
// plain uint64 arithmetic is exact here without needing math/big.
func toUserhash(s string) uint64 {
	s = strings.TrimSpace(s)
	var l uint64
	for i := 0; i < len(s) && i < 12; i++ {
		c := s[i]
		l *= 37
		switch {
		case c >= 0x41 && c <= 0x5a: // A-Z
			l += uint64(c) + 1 - 0x41
		case c >= 0x61 && c <= 0x7a: // a-z
			l += uint64(c) + 1 - 0x61
		case c >= 0x30 && c <= 0x39: // 0-9
			l += uint64(c) + 27 - 0x30
		}
	}
	for l != 0 && l%37 == 0 {
		l /= 37
	}
	return l
}

// loginServer derives the session-init selector byte from a username: (hash>>16)&0x1f.
func loginServer(username string) byte {
	return byte((toUserhash(username) >> 16) & 0x1f)
}
