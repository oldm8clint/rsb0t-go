package fleetrunner

import (
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

type bitField struct {
	n int
	v uint32
}

func bitPack(totalBytes int, fields ...bitField) []byte {
	buf := make([]byte, totalBytes)
	pos := 0
	for _, f := range fields {
		n, v := f.n, f.v
		for n > 0 {
			bytePos, bitOff := pos>>3, pos&7
			free := 8 - bitOff
			take := min(n, free)
			shift := n - take
			chunk := (v >> uint(shift)) & ((1 << uint(take)) - 1)
			buf[bytePos] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

func simPayload(build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(64)
	build(p)
	return p.Data[:p.Pos]
}

// simPlace puts the local player on x,z (level 0) with slot 5.
func simPlace(s *gamestate.State, x, z int) {
	baseX, baseZ := (x/8-6)*8, (z/8-6)*8
	s.Handle(serverprot.REBUILD_NORMAL, simPayload(func(p *packet.Packet) { p.P2(uint16(baseX/8 + 6)); p.P2(uint16(baseZ/8 + 6)) }))
	s.Handle(serverprot.UPDATE_PID, simPayload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	s.Handle(serverprot.PLAYER_INFO, bitPack(3, bitField{1, 1}, bitField{2, 3}, bitField{2, 0},
		bitField{7, uint32(x - baseX)}, bitField{7, uint32(z - baseZ)}, bitField{1, 0}, bitField{1, 0}))
}

type simNPC struct{ index, typeID, dx, dz int }

func simNPCs(s *gamestate.State, npcs ...simNPC) {
	fields := []bitField{{8, 0}}
	for _, n := range npcs {
		fields = append(fields, bitField{14, uint32(n.index)}, bitField{11, uint32(n.typeID)},
			bitField{5, uint32(n.dx & 0x1f)}, bitField{5, uint32(n.dz & 0x1f)}, bitField{1, 0}, bitField{1, 0})
	}
	fields = append(fields, bitField{14, 16383})
	s.Handle(serverprot.NPC_INFO, bitPack((8+37*len(npcs)+14+7)/8, fields...))
}

const mossGiantType = 112

// simObj drops a ground item at the absolute tile x,z for a player simPlace'd at px,pz.
func simObj(s *gamestate.State, px, pz, x, z, id, count int) {
	baseX, baseZ := (px/8-6)*8, (pz/8-6)*8
	lx, lz := x-baseX, z-baseZ
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, simPayload(func(p *packet.Packet) { p.P1(byte(lx &^ 7)); p.P1(byte(lz &^ 7)) }))
	s.Handle(serverprot.OBJ_ADD, simPayload(func(p *packet.Packet) { p.P1(byte((lx&7)<<4 | lz&7)); p.P2(uint16(id)); p.P2(uint16(count)) }))
}

func sentOps(sim *actionsim.Session, ops ...int) [][]byte {
	var out [][]byte
	for _, b := range sim.Sent() {
		for _, op := range ops {
			if len(b) > 0 && int(b[0]) == op {
				out = append(out, b)
			}
		}
	}
	return out
}
