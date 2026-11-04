package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func payload(build func(p *packet.Packet)) []byte {
	p := packet.NewWriter(64)
	build(p)
	return p.Data[:p.Pos]
}

// newZoneState has a build area whose origin is 3152,3152 (REBUILD_NORMAL zone 400,400).
func newZoneState() *State {
	s := New()
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) { p.P2(400); p.P2(400) }))
	return s
}

func locAt(s *State, x, z int) (LocInstance, bool) {
	for _, l := range s.LocChanges() {
		if l.X == x && l.Z == z {
			return l, true
		}
	}
	return LocInstance{}, false
}

func TestLocDelLeavesATombstone(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	// Tile offset 2,3 in the zone: 3152+48+2, 3152+48+3.
	s.Handle(serverprot.LOC_ADD_CHANGE, payload(func(p *packet.Packet) { p.P1(2<<4 | 3); p.P1(10<<2 | 1); p.P2(2646) }))
	if locs := s.Locs(); len(locs) != 1 || locs[0].Type != 2646 || locs[0].X != 3202 || locs[0].Z != 3203 || locs[0].Rotate != 1 {
		t.Fatalf("added loc = %+v", locs)
	}
	s.Handle(serverprot.LOC_DEL, payload(func(p *packet.Packet) { p.P1(2<<4 | 3); p.P1(10 << 2) }))
	if locs := s.Locs(); len(locs) != 0 {
		t.Fatalf("Locs still lists a deleted loc: %+v", locs)
	}
	l, ok := locAt(s, 3202, 3203)
	if !ok || !l.Deleted || l.Type != -1 || l.Shape != 10 {
		t.Fatalf("tombstone = %+v ok=%v", l, ok)
	}
}

func TestFullFollowsClearsItsZoneInWorldCoordinates(t *testing.T) {
	s := newZoneState()
	for _, zone := range []byte{48, 56} {
		s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(zone); p.P1(48) }))
		s.Handle(serverprot.LOC_DEL, payload(func(p *packet.Packet) { p.P1(0x11); p.P1(10 << 2) }))
	}
	if len(s.LocChanges()) != 2 {
		t.Fatalf("setup: %+v", s.LocChanges())
	}
	s.Handle(serverprot.UPDATE_ZONE_FULL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	if _, ok := locAt(s, 3201, 3201); ok {
		t.Error("full follows left a record in its own zone")
	}
	if _, ok := locAt(s, 3209, 3201); !ok {
		t.Error("full follows cleared a neighbouring zone")
	}
}

func TestPartialEnclosedAppliesLocChangesAndSkipsTheRest(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, payload(func(p *packet.Packet) {
		p.P1(48)
		p.P1(56)
		p.P1(serverprot.OBJ_ADD)
		p.P1(0x00)
		p.P2(995)
		p.P2(10)
		p.P1(serverprot.LOC_DEL)
		p.P1(0x45)
		p.P1(10 << 2)
		p.P1(serverprot.MAP_ANIM)
		p.Skip(6)
		p.P1(serverprot.LOC_ADD_CHANGE)
		p.P1(0x70)
		p.P1(0<<2 | 2)
		p.P2(1531)
	}))
	del, ok := locAt(s, 3152+48+4, 3152+56+5)
	if !ok || !del.Deleted {
		t.Fatalf("enclosed LOC_DEL = %+v ok=%v", del, ok)
	}
	door, ok := locAt(s, 3152+48+7, 3152+56)
	if !ok || door.Type != 1531 || door.Shape != 0 || door.Rotate != 2 {
		t.Fatalf("enclosed LOC_ADD_CHANGE = %+v ok=%v", door, ok)
	}

	// An opcode that cannot appear in a zone stops decoding instead of misreading.
	s2 := newZoneState()
	s2.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, payload(func(p *packet.Packet) {
		p.P1(0)
		p.P1(0)
		p.P1(serverprot.PLAYER_INFO)
		p.P1(serverprot.LOC_DEL)
		p.P1(0)
		p.P1(0)
	}))
	if len(s2.LocChanges()) != 0 {
		t.Fatalf("decoded past an unknown sub-opcode: %+v", s2.LocChanges())
	}
}
