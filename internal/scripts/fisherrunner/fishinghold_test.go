package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func fishingAnimation(s *gamestate.State, seq int) {
	p := packet.NewWriter(16)
	p.PData([]byte{0x80, 0x1f, 0xfc}, 0, 3) // stationary self, extended block, no other players
	p.P1(2)
	p.P2(uint16(seq))
	p.P1(0)
	s.Handle(serverprot.PLAYER_INFO, p.Data[:p.Pos])
}

func TestFishingDryStreakRemainsActive(t *testing.T) {
	s := gamestate.New()
	// A minute of unsuccessful rolls still refreshes the fishing animation.
	for tick := 0; tick < 100; tick++ {
		if tick%5 == 0 {
			fishingAnimation(s, 621)
		} else {
			s.Handle(serverprot.PLAYER_INFO, []byte{0, 0x7f, 0xf0}) // no self update, no players
		}
		if !fishingActive(s) {
			t.Fatalf("dry streak incorrectly ended at tick %d", tick)
		}
	}
	fishingAnimation(s, 65535)
	if fishingActive(s) {
		t.Fatal("cancelled animation still considered fishing")
	}
	fishingAnimation(s, 808)
	if fishingActive(s) {
		t.Fatal("non-fishing animation considered fishing")
	}
}

func TestFishingActivityExpiresWithoutRefresh(t *testing.T) {
	s := gamestate.New()
	fishingAnimation(s, 620)
	for i := 0; i < 12; i++ {
		s.Handle(serverprot.PLAYER_INFO, []byte{0, 0x7f, 0xf0})
	}
	if fishingActive(s) {
		t.Fatal("stale fishing animation did not expire")
	}
}

func TestFishingHoldWaitsForStartupButEndsOnServerCancellation(t *testing.T) {
	s := gamestate.New()
	if fishingHoldEnded(false, fishingActive(s)) {
		t.Fatal("fresh click lost startup grace")
	}
	fishingAnimation(s, 621)
	if fishingHoldEnded(true, fishingActive(s)) {
		t.Fatal("active dry streak ended hold")
	}
	fishingAnimation(s, 65535)
	if !fishingHoldEnded(true, fishingActive(s)) {
		t.Fatal("server cancellation retained the old twelve-second idle")
	}
}
