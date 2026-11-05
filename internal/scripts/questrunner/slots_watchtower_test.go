package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Watch Tower (19 Oct 2026) runs under a fleet-wide slot of two, held for the whole run: its shamans are six npcs that one account's potion removes for
// every player for 102 ticks, Gorad is one npc (back 60 ticks after a death), the rock cake stall is empty for twelve ticks after each theft and minded by one
// trader, and every leg is through hunters. There is no step after which the account is clear of the shared ground, so the module releases nothing early.
func TestWatchtowerRunsUnderASlotOfTwo(t *testing.T) {
	id := questplan.WatchtowerID
	if !exclusiveQuests[id] || capacityOf(id) != 2 {
		t.Fatalf("%s: exclusive %v capacity %d, want true and 2", id, exclusiveQuests[id], capacityOf(id))
	}
	now := time.Now()
	ds := []*Deps{{}, {}, {}, {}}
	for i := 0; i < 2; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("account %d of 2 was refused", i+1)
		}
	}
	if tryHoldQuest(id, ds[2], now) {
		t.Error("a third account was admitted")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[2], now) {
		t.Error("a released slot was not given to the next account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
	if slotRepeats[id] {
		t.Error("the slot repeats: it is held for the whole run and never remembered as passed")
	}
	if questplan.Watchtower.ExclusiveDone != nil {
		t.Error("the module releases its slot early")
	}
}
