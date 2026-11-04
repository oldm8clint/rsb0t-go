package cachecfg

import (
	"fmt"
	"strings"
)

// The decoders below are ports of the vendored client's ObjType.decode, NpcType.decode
// and LocType.decode (rsb0t/src/client/config). Every opcode consumes exactly the bytes
// the TypeScript does, even for fields this package throws away, and an unknown opcode
// consumes nothing, as the non-strict TypeScript does.

// centrepieceStraight is LocShape.CENTREPIECE_STRAIGHT, which LocType.decode uses to
// decide whether a loc with no explicit "active" flag is interactive.
const centrepieceStraight = 10

type rawObj struct {
	name, desc             string
	hasName                bool
	members, stackable     bool
	cost                   int32
	certlink, certtemplate int
	ops, iops              [5]string
}

type rawNpc struct {
	name     string
	size     int
	vislevel int
	ops      [5]string
}

type rawLoc struct {
	name                  string
	width, length         int
	blockwalk, blockrange bool
	active                bool
	forceapproach         int
	ops                   [5]string
}

// hiddenOp ports the `op[i]?.toLowerCase() === 'hidden'` rule: a hidden option is absent.
func hiddenOp(s string) string {
	if strings.ToLower(s) == "hidden" {
		return ""
	}
	return s
}

func decodeObj(r *reader) rawObj {
	o := rawObj{cost: 1, certlink: -1, certtemplate: -1}
	for r.err == nil {
		code := r.g1()
		switch {
		case code == 0:
			return o
		case code == 1, code == 4, code == 5, code == 6, code == 7, code == 8, code == 10,
			code == 24, code == 26, code == 78, code == 79, code >= 90 && code <= 93, code == 95,
			code >= 110 && code <= 112:
			r.skip(2)
		case code == 2:
			o.name, o.hasName = r.gjstr(), true
		case code == 3:
			o.desc = r.gjstr()
		case code == 11:
			o.stackable = true
		case code == 12:
			o.cost = r.g4()
		case code == 16:
			o.members = true
		case code == 23, code == 25:
			r.skip(3)
		case code >= 30 && code < 35:
			o.ops[code-30] = hiddenOp(r.gjstr())
		case code >= 35 && code < 40:
			o.iops[code-35] = r.gjstr()
		case code == 40:
			r.skip(r.g1() * 4)
		case code == 97:
			o.certlink = r.g2()
		case code == 98:
			o.certtemplate = r.g2()
		case code >= 100 && code < 110:
			r.skip(4)
		case code >= 113 && code <= 115:
			r.skip(1)
		}
	}
	return o
}

func decodeNpc(r *reader) rawNpc {
	n := rawNpc{size: 1, vislevel: -1}
	for r.err == nil {
		code := r.g1()
		switch {
		case code == 0:
			return n
		case code == 1, code == 60:
			r.skip(r.g1() * 2)
		case code == 2:
			n.name = r.gjstr()
		case code == 3:
			r.gjstr()
		case code == 12:
			n.size = r.g1b()
		case code == 13, code == 14, code >= 90 && code <= 92, code == 97, code == 98, code == 102, code == 103:
			r.skip(2)
		case code == 17:
			r.skip(8)
		case code >= 30 && code < 40:
			// Why: the TS op array has five slots but opcodes run to 39; indices past 4 are
			// never read by the client's menus, so they are consumed and dropped.
			if op := hiddenOp(r.gjstr()); code-30 < 5 {
				n.ops[code-30] = op
			}
		case code == 40:
			r.skip(r.g1() * 4)
		case code == 95:
			n.vislevel = r.g2()
		case code == 100, code == 101:
			r.skip(1)
		}
	}
	return n
}

func decodeLoc(r *reader) rawLoc {
	l := rawLoc{width: 1, length: 1, blockwalk: true, blockrange: true}
	active := -1
	var hasModel, hasShape, shapeStraight, hasOps, breakRoutefinding bool
	for r.err == nil {
		code := r.g1()
		switch {
		case code == 0:
			if active == -1 {
				l.active = hasModel && (!hasShape || shapeStraight) || hasOps
			}
			if breakRoutefinding {
				l.blockwalk, l.blockrange = false, false
			}
			return l
		case code == 1:
			count := r.g1()
			hasModel, hasShape, shapeStraight = true, true, false
			for i := 0; i < count; i++ {
				r.skip(2)
				if shape := r.g1(); i == 0 {
					shapeStraight = shape == centrepieceStraight
				}
			}
		case code == 2:
			l.name = r.gjstr()
		case code == 3:
			r.gjstr()
		case code == 5:
			hasModel, hasShape, shapeStraight = true, false, false
			r.skip(r.g1() * 2)
		case code == 14:
			l.width = r.g1()
		case code == 15:
			l.length = r.g1()
		case code == 17:
			l.blockwalk = false
		case code == 18:
			l.blockrange = false
		case code == 19:
			active = r.g1()
			if active == 1 {
				l.active = true
			}
		case code == 21, code == 22, code == 23, code == 62, code == 64, code == 73:
		case code == 24, code == 60, code >= 65 && code <= 68, code >= 70 && code <= 72:
			r.skip(2)
		case code == 28, code == 29, code == 39, code == 75:
			r.skip(1)
		case code >= 30 && code < 39:
			hasOps = true
			if op := hiddenOp(r.gjstr()); code-30 < 5 {
				l.ops[code-30] = op
			}
		case code == 40:
			r.skip(r.g1() * 4)
		case code == 69:
			l.forceapproach = r.g1()
		case code == 74:
			breakRoutefinding = true
		case code == 77:
			r.skip(2)
			r.skip((r.g1() + 1) * 2)
		}
	}
	return l
}

// records reads a .idx/.dat pair: a g2 count, then one g2 length per record, with the
// first record starting at offset 2 of the .dat file.
func records(name string, idx, dat []byte, decode func(r *reader)) (int, error) {
	if len(idx) < 2 {
		return 0, fmt.Errorf("cachecfg: %s.idx is too short", name)
	}
	ir := &reader{data: idx}
	count := ir.g2()
	offset := 2
	for id := 0; id < count; id++ {
		length := ir.g2()
		if ir.err != nil {
			return 0, fmt.Errorf("cachecfg: %s.idx ends before record %d of %d", name, id, count)
		}
		r := &reader{data: dat, pos: offset}
		decode(r)
		if r.err != nil {
			return 0, fmt.Errorf("cachecfg: %s record %d: %w", name, id, r.err)
		}
		offset += length
	}
	return count, nil
}
