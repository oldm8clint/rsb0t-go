package cachecfg

import "errors"

var errShort = errors.New("cachecfg: record runs past the end of its .dat file")

// reader is a bounds-checked cursor over a .dat file. The config decoders need two things
// internal/packet does not give them: a read past the end must become an error rather
// than a panic (the archive comes off the network), and strings must decode as Latin-1
// the way Packet.gjstr's String.fromCharCode does, so a name with a byte >= 0x80 matches
// the TypeScript client's.
type reader struct {
	data []byte
	pos  int
	err  error
}

func (r *reader) ok(n int) bool {
	if r.err != nil {
		return false
	}
	if r.pos < 0 || r.pos+n > len(r.data) {
		r.err = errShort
		return false
	}
	return true
}

func (r *reader) g1() int {
	if !r.ok(1) {
		return 0
	}
	v := r.data[r.pos]
	r.pos++
	return int(v)
}

func (r *reader) g1b() int { return int(int8(r.g1())) }

func (r *reader) g2() int {
	if !r.ok(2) {
		return 0
	}
	v := int(r.data[r.pos])<<8 | int(r.data[r.pos+1])
	r.pos += 2
	return v
}

func (r *reader) g4() int32 {
	if !r.ok(4) {
		return 0
	}
	d := r.data[r.pos:]
	r.pos += 4
	return int32(uint32(d[0])<<24 | uint32(d[1])<<16 | uint32(d[2])<<8 | uint32(d[3]))
}

func (r *reader) skip(n int) {
	if r.ok(n) {
		r.pos += n
	}
}

// gjstr ports Packet.gjstr, including its exit order: a string that reaches the end of
// the buffer without a newline drops its last byte.
func (r *reader) gjstr() string {
	if !r.ok(1) {
		return ""
	}
	start, end, ascii := r.pos, r.pos, true
	for r.pos < len(r.data) {
		b := r.data[r.pos]
		r.pos++
		if b == '\n' || r.pos >= len(r.data) {
			break
		}
		if b >= 0x80 {
			ascii = false
		}
		end = r.pos
	}
	raw := r.data[start:end]
	if ascii {
		return string(raw)
	}
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = rune(b)
	}
	return string(runes)
}
