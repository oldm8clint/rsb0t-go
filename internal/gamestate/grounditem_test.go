package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func itemAt(s *State, x, z int) []GroundItem {
	var out []GroundItem
	for _, g := range s.GroundItems() {
		if g.X == x && g.Z == z {
			out = append(out, g)
		}
	}
	return out
}

func TestObjAddThenObjDelRemovesTheStack(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	// Tile offset 2,3 in the zone: 3152+48+2, 3152+48+3.
	s.Handle(serverprot.OBJ_ADD, payload(func(p *packet.Packet) { p.P1(2<<4 | 3); p.P2(995); p.P2(10) }))
	items := itemAt(s, 3202, 3203)
	if len(items) != 1 || items[0].ID != 995 || items[0].Count != 10 || items[0].Level != 0 {
		t.Fatalf("added item = %+v", items)
	}
	s.Handle(serverprot.OBJ_DEL, payload(func(p *packet.Packet) { p.P1(2<<4 | 3); p.P2(995) }))
	if items := itemAt(s, 3202, 3203); len(items) != 0 {
		t.Fatalf("GroundItems still lists a deleted stack: %+v", items)
	}
}

func TestObjDelMatchesOnlyTheMaskedID(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	s.Handle(serverprot.OBJ_ADD, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(1) }))
	// The high bit is masked off when the server addresses a stack for deletion, mirroring
	// Client.ts's `obj.id === (type & 0x7fff)`.
	s.Handle(serverprot.OBJ_DEL, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995 | 0x8000) }))
	if items := itemAt(s, 3200, 3200); len(items) != 0 {
		t.Fatalf("masked OBJ_DEL did not remove the stack: %+v", items)
	}
}

func TestObjCountUpdatesTheMatchingStackByOldCount(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	s.Handle(serverprot.OBJ_ADD, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(10) }))
	s.Handle(serverprot.OBJ_ADD, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(25) }))
	// Addresses the (id=995, count=10) stack specifically, leaving the count=25 stack alone.
	s.Handle(serverprot.OBJ_COUNT, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(10); p.P2(50) }))
	items := itemAt(s, 3200, 3200)
	if len(items) != 2 {
		t.Fatalf("expected 2 stacks, got %+v", items)
	}
	var sawUpdated, sawUntouched bool
	for _, it := range items {
		if it.Count == 50 {
			sawUpdated = true
		}
		if it.Count == 25 {
			sawUntouched = true
		}
	}
	if !sawUpdated || !sawUntouched {
		t.Fatalf("OBJ_COUNT did not update the right stack: %+v", items)
	}
}

func TestObjRevealSkipsWhenPidIsSelf(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(7); p.P1(0) }))
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	s.Handle(serverprot.OBJ_REVEAL, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(1); p.P2(7) }))
	if items := itemAt(s, 3200, 3200); len(items) != 0 {
		t.Fatalf("OBJ_REVEAL from ourselves should be skipped: %+v", items)
	}
	s.Handle(serverprot.OBJ_REVEAL, payload(func(p *packet.Packet) { p.P1(0x00); p.P2(995); p.P2(1); p.P2(99) }))
	if items := itemAt(s, 3200, 3200); len(items) != 1 || items[0].ID != 995 {
		t.Fatalf("OBJ_REVEAL from another player should add the stack: %+v", items)
	}
}

func TestZoneEnclosedDecodesObjSubPackets(t *testing.T) {
	s := newZoneState()
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, payload(func(p *packet.Packet) {
		p.P1(48)
		p.P1(56)
		p.P1(serverprot.OBJ_ADD)
		p.P1(0x45)
		p.P2(995)
		p.P2(3)
	}))
	items := itemAt(s, 3152+48+4, 3152+56+5)
	if len(items) != 1 || items[0].ID != 995 || items[0].Count != 3 {
		t.Fatalf("enclosed OBJ_ADD = %+v", items)
	}
}

func TestFullFollowsClearsGroundItemsInItsZone(t *testing.T) {
	s := newZoneState()
	for _, zone := range []byte{48, 56} {
		s.Handle(serverprot.UPDATE_ZONE_PARTIAL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(zone); p.P1(48) }))
		s.Handle(serverprot.OBJ_ADD, payload(func(p *packet.Packet) { p.P1(0x11); p.P2(995); p.P2(1) }))
	}
	if len(s.GroundItems()) != 2 {
		t.Fatalf("setup: %+v", s.GroundItems())
	}
	s.Handle(serverprot.UPDATE_ZONE_FULL_FOLLOWS, payload(func(p *packet.Packet) { p.P1(48); p.P1(48) }))
	if items := itemAt(s, 3201, 3201); len(items) != 0 {
		t.Error("full follows left a ground item in its own zone")
	}
	if items := itemAt(s, 3209, 3201); len(items) != 1 {
		t.Error("full follows cleared a neighbouring zone's ground item")
	}
}
