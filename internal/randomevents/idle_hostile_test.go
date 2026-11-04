package randomevents

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/serverprot"
	"testing"
)

func TestIdleHostileDoesNotBlockBankWork(t *testing.T) {
	s := gamestate.New()
	s.SelfSlot = 1
	setTile(s, 2808, 3440, 0)
	addNPCFacing(s, 3, 411, 1, 0, -1)
	for i := 0; i < 8; i++ {
		s.Handle(serverprot.PLAYER_INFO, bitPack(3, bitField{1, 0}, bitField{8, 0}, bitField{11, 2047}))
	}
	if ev := Detect(s); ev != nil {
		t.Fatalf("idle unassigned hostile blocks bank: %+v", ev)
	}
	if HostileEventNpcNearby(s, 2808, 3440, 15) {
		t.Fatal("idle hostile falsely reports aggression")
	}
}

func TestOwnedHostileStillPreemptsBankWork(t *testing.T) {
	s := gamestate.New()
	s.SelfSlot = 1
	setTile(s, 2808, 3440, 0)
	addNPCFacing(s, 3, 411, 10, 0, 32769)
	if ev := Detect(s); ev == nil || ev.Kind != KindEvade {
		t.Fatalf("owned hostile missed: %+v", ev)
	}
}
