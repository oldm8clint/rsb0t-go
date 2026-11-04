package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
)

func deathLine(s *State) {
	p := packet.NewWriter(64)
	p.PJStr("Oh dear you are dead!")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])
}

// A death is an event, and the context read at it holds the pack, the NPCs facing the account, the ones near it and the
// hitpoints of the fight.
func TestDeathContextHoldsThePackTheAttackersAndTheHitpoints(t *testing.T) {
	s := New()
	var got []Event
	s.Subscribe(collect(&got))

	xp := int32(levels.XPAtLevel(50))
	for _, hp := range []int{50, 41, 30, 18, 7, 0} {
		s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, xp, hp))
		s.scene.tick++
	}
	inv := packet.NewWriter(32)
	inv.P2(packComponentID)
	inv.P2(2)
	inv.P2(858 + 1) // wire id is the obj id plus one
	inv.P1(255)
	inv.P4(721)
	inv.P2(386 + 1)
	inv.P1(4)
	s.Handle(serverprot.UPDATE_INV_FULL, inv.Data[:inv.Pos])
	worn := packet.NewWriter(16)
	worn.P2(wornComponentID)
	worn.P2(1)
	worn.P2(1333 + 1)
	worn.P1(1)
	s.Handle(serverprot.UPDATE_INV_FULL, worn.Data[:worn.Pos])

	s.SelfSlot = 7
	s.haveTile, s.mapBuildBaseX, s.mapBuildBaseZ, s.localX, s.localZ, s.level = true, 2500, 3000, 8, 97, 0
	s.npcs[10] = &npcEntry{typeID: 111, localX: 9, localZ: 97, faceEntity: playerFaceBase + 7}
	s.npcs[11] = &npcEntry{typeID: 112, localX: 10, localZ: 99, faceEntity: -1}
	s.npcs[12] = &npcEntry{typeID: 113, localX: 40, localZ: 97, faceEntity: playerFaceBase + 7} // far, but it faces us
	s.npcs[13] = &npcEntry{typeID: 114, localX: 60, localZ: 97, faceEntity: -1}                 // far and idle
	s.players[3] = &playerEntry{faceEntity: playerFaceBase + 7}

	deathLine(s)

	deaths := 0
	for _, ev := range got {
		if ev.Kind == EventDeath {
			deaths++
		}
	}
	if deaths != 1 {
		t.Fatalf("%d EventDeath in %d events, want one", deaths, len(got))
	}
	dc, ok := s.LastDeath()
	if !ok {
		t.Fatal("no death context after the death line")
	}
	if !dc.HaveTile || dc.X != 2508 || dc.Z != 3097 || dc.Level != 0 {
		t.Errorf("tile = %v %d,%d,%d, want 2508,3097,0", dc.HaveTile, dc.X, dc.Z, dc.Level)
	}
	if dc.HPPeak != 50 || dc.HPLast != 7 || dc.MaxHP != 50 {
		t.Errorf("hitpoints peak/last/max = %d/%d/%d, want 50/7/50", dc.HPPeak, dc.HPLast, dc.MaxHP)
	}
	if len(dc.Attackers) != 2 {
		t.Errorf("attackers = %+v, want the two NPCs facing the account", dc.Attackers)
	}
	if len(dc.Nearby) != 1 || dc.Nearby[0].Type != 112 {
		t.Errorf("nearby = %+v, want the idle NPC two tiles away", dc.Nearby)
	}
	if dc.Players != 1 {
		t.Errorf("players = %d, want 1", dc.Players)
	}
	if len(dc.Pack) != 2 || dc.Pack[0].ID != 858 || dc.Pack[0].Count != 721 {
		t.Errorf("pack = %+v, want 721 of obj 858 first", dc.Pack)
	}
	if len(dc.Worn) != 1 || dc.Worn[0].ID != 1333 || dc.Worn[0].Count != 1 {
		t.Errorf("worn = %+v, want the one worn item (obj 1333)", dc.Worn)
	}
}

// With no NPC facing the account and no poison, the killer is unknown, and the context says so by being empty.
func TestDeathContextWithNothingFacingTheAccount(t *testing.T) {
	s := New()
	deathLine(s)
	dc, ok := s.LastDeath()
	if !ok || len(dc.Attackers) != 0 || dc.Poisoned || dc.Players != 0 || dc.HaveTile {
		t.Fatalf("context = %+v ok=%v, want an empty one", dc, ok)
	}
}

// The ring keeps the last readings only, and a heal that outlives the window is not the fight's peak.
func TestDeathHPPeakIsTheFightsNotTheSessions(t *testing.T) {
	var d deathTracker
	d.noteHP(0, 50)
	d.noteHP(100, 30)
	d.noteHP(105, 12)
	d.noteHP(110, 0)
	peak, last := d.peakAndLast(110)
	if peak != 30 || last != 12 {
		t.Fatalf("peak/last = %d/%d, want 30/12 (the 50 was %d ticks before)", peak, last, 110)
	}
	for i := 0; i < 20; i++ {
		d.noteHP(200+i, 9)
	}
	if peak, last = d.peakAndLast(219); peak != 9 || last != 9 {
		t.Fatalf("after the ring wrapped: peak/last = %d/%d, want 9/9", peak, last)
	}
}
