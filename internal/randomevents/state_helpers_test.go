package randomevents

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// payload builds a raw packet payload the same way internal/gamestate's own tests do
// (see internal/gamestate/loc_test.go's identical helper) — Detect() only has access to
// gamestate.State's exported API, so synthetic NPCs/locs/tile for these tests are built
// by feeding State.Handle real, hand-built server packets, not by poking private fields.
func payload(build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(64)
	build(p)
	return p.Data[:p.Pos]
}

// hitSelf lands a hitmark of damage on the local player this tick: InCombat is true for the next ten ticks of
// PLAYER_INFO, and no more arrive in a test, so it stays true.
func hitSelf(s *gamestate.State, damage int) {
	bits := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
	s.Handle(serverprot.PLAYER_INFO, append(bits, payload(func(p *packet.Packet) {
		p.P1(0x10) // playerUpdateHitmark
		p.P1(byte(damage))
		p.P1(1)
		p.P1(7)
		p.P1(10)
	})...))
}

// bitField is one (width, value) pair for bitPack.
type bitField struct {
	n int
	v uint32
}

// bitPack MSB-first bit-packs fields into a totalBytes-long buffer, the write-side
// mirror of internal/packet.Packet.GBit's own read algorithm (there is no exported
// bit-level writer in that package — PLAYER_INFO/NPC_INFO are server->client-only
// wire formats production code never needs to emit). totalBytes must be large enough
// to hold every field plus any lookahead a reader checks before its terminator.
func bitPack(totalBytes int, fields ...bitField) []byte {
	buf := make([]byte, totalBytes)
	pos := 0
	for _, f := range fields {
		n, v := f.n, f.v
		for n > 0 {
			bytePos := pos >> 3
			bitOff := pos & 7
			free := 8 - bitOff
			take := n
			if take > free {
				take = free
			}
			shift := n - take
			chunk := (v >> uint(shift)) & ((1 << uint(take)) - 1)
			buf[bytePos] |= byte(chunk << uint(free-take))
			pos += take
			n -= take
		}
	}
	return buf
}

// setTile teleports the local player to an absolute tile via REBUILD_NORMAL +
// PLAYER_INFO(teleport), mirroring applyRebuildNormal/applyPlayerInfoLocal. x and z
// must be multiples of 8 so the build-area origin lands exactly on the requested tile
// with a zero local offset (keeps the arithmetic trivial and exact for test fixtures).
func setTile(s *gamestate.State, x, z, level int) {
	if x%8 != 0 || z%8 != 0 {
		panic("setTile: x and z must be multiples of 8")
	}
	zoneX := x/8 + 6
	zoneZ := z/8 + 6
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(zoneX))
		p.P2(uint16(zoneZ))
	}))
	// PLAYER_INFO local-player teleport branch: hasUpdate(1)=1, kind(2)=3 (teleport),
	// level(2), localX(7)=0, localZ(7)=0, jump(1)=0, extendedInfo(1)=0 — 21 bits, well
	// within a 3-byte buffer.
	s.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1},
		bitField{2, 3},
		bitField{2, uint32(level)},
		bitField{7, 0},
		bitField{7, 0},
		bitField{1, 0},
		bitField{1, 0},
	))
}

// addLoc adds one loc via a minimal UPDATE_ZONE_PARTIAL_FOLLOWS + LOC_ADD_CHANGE pair,
// the same shape internal/gamestate/loc_test.go uses. Detect()'s hazard matching only
// checks loc.Type, not its tile, so the tile is fixed at the zone's own origin.
func addLoc(s *gamestate.State, typeID, shape int) {
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) {
		p.P1(0)
		p.P1(0)
	}))
	s.Handle(serverprot.LOC_ADD_CHANGE, payload(func(p *packet.Packet) {
		p.P1(0)
		p.P1(byte(shape << 2))
		p.P2(uint16(typeID))
	}))
}

// setFullInventory mirrors UPDATE_INV_FULL for the given component: each item is
// (objID, count); objID 0 means an empty slot, matching the wire protocol and
// internal/actions's own identically-named test helper (internal/actions/testhelpers_test.go)
// — duplicated here rather than shared since it's a small, unexported test-only helper
// and the two packages' test files don't otherwise depend on each other.
func setFullInventory(s *gamestate.State, comID int, items ...[2]int32) {
	s.Handle(serverprot.UPDATE_INV_FULL, payload(func(p *packet.Packet) {
		p.P2(uint16(comID))
		p.P2(uint16(len(items)))
		for _, it := range items {
			id, count := it[0], it[1]
			if id != 0 {
				p.P2(uint16(id + 1))
			} else {
				p.P2(0)
			}
			if count >= 255 {
				p.P1(255)
				p.P4(count)
			} else {
				p.P1(byte(count))
			}
		}
	}))
}

// addNPC adds one NPC via NPC_INFO, positioned dx,dz tiles from the local player's own
// current local coordinates (set beforehand by setTile) — exactly the offset Detect()'s
// Chebyshev distance check needs, without having to reason about the build-area origin
// a second time. dx/dz must be in [-16,15] (the wire format's 5-bit signed range).
func addNPC(s *gamestate.State, index, typeID, dx, dz int) {
	s.Handle(serverprot.NPC_INFO, bitPack(9,
		bitField{8, 0}, // no previously-tracked npcs
		bitField{14, uint32(index)},
		bitField{11, uint32(typeID)},
		bitField{5, uint32(dx & 0x1F)},
		bitField{5, uint32(dz & 0x1F)},
		bitField{1, 0},      // jump flag, unused
		bitField{1, 0},      // extendedInfo flag, unused
		bitField{14, 16383}, // terminator (NPC_INFO's "no more entries" sentinel)
	))
}

// npcSpec is one entry for addNPCs.
type npcSpec struct {
	index, typeID, dx, dz int
}

// addNPCs is addNPC generalized to N simultaneous NPCs in one NPC_INFO packet — addNPC
// itself always claims "no previously-tracked npcs" (bitField{8,0}), so two separate
// addNPC calls do NOT accumulate: applyNpcInfo's own end-of-packet bookkeeping
// (`s.npcIDs = newIDs`, then deleting anything not in the fresh `keep` set) drops
// whichever NPC the FIRST call added the moment the second packet arrives, since it
// never mentions that index. Confirmed live while writing the nearest-dialog-npc test
// below (a naive two-call version silently left only one NPC tracked, no test failure,
// a false pass). Bits per entry match addNPC's own layout exactly (14+11+5+5+1+1=37),
// followed by the one shared 14-bit terminator.
func addNPCs(s *gamestate.State, specs ...npcSpec) {
	fields := []bitField{{8, 0}} // no previously-tracked npcs
	for _, sp := range specs {
		fields = append(fields,
			bitField{14, uint32(sp.index)},
			bitField{11, uint32(sp.typeID)},
			bitField{5, uint32(sp.dx & 0x1F)},
			bitField{5, uint32(sp.dz & 0x1F)},
			bitField{1, 0}, // jump flag, unused
			bitField{1, 0}, // extendedInfo flag, unused
		)
	}
	fields = append(fields, bitField{14, 16383}) // terminator
	totalBits := 8 + 37*len(specs) + 14
	totalBytes := (totalBits + 7) / 8
	s.Handle(serverprot.NPC_INFO, bitPack(totalBytes, fields...))
}

// addNPCFacing is addNPC plus a real FaceEntity, for the Detect()/TargetsAnotherPlayer
// filter — a hostile "event monster" that's confirmed facing a DIFFERENT player must not
// raise KindEvade for this account (see detect.go's own doc comment on that check).
// faceEntity is the raw wire value: playerFaceBase+slot for a player, an NPC index below
// playerFaceBase, or -1 for nobody.
//
// The main NPC_INFO block uses bitPack(8, ...) here, not addNPC's own bitPack(9, ...):
// 8+14+11+5+5+1+1+14 = 59 bits fits exactly in 8 bytes (with 5 bits of trailing padding
// GBitEnd rounds past), and GBitEnd/applyNpcInfoExtended's byte cursor picks up at
// EXACTLY that rounded-up byte offset (packet.Packet.GBitEnd: `Pos = (BitPos+7)>>3`) —
// addNPC's own extra 9th byte would land one byte short of where the extended-info mask
// byte appended right after this function's own bitPack call needs to start.
func addNPCFacing(s *gamestate.State, index, typeID, dx, dz, faceEntity int) {
	main := bitPack(8,
		bitField{8, 0}, // no previously-tracked npcs
		bitField{14, uint32(index)},
		bitField{11, uint32(typeID)},
		bitField{5, uint32(dx & 0x1F)},
		bitField{5, uint32(dz & 0x1F)},
		bitField{1, 0},      // jump flag, unused
		bitField{1, 1},      // extendedInfo flag: this NPC has an extended-info block below
		bitField{14, 16383}, // terminator (NPC_INFO's "no more entries" sentinel)
	)
	face := uint16(faceEntity)
	if faceEntity < 0 {
		face = 65535
	}
	const npcUpdateFaceEntityMask = 0x4 // gamestate's own npcUpdateFaceEntity — unexported there, mirrored here
	ext := []byte{
		npcUpdateFaceEntityMask,
		byte(face >> 8), byte(face),
	}
	s.Handle(serverprot.NPC_INFO, append(main, ext...))
}
