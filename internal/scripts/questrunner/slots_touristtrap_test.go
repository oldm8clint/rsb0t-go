package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Tourist Trap holds an exclusive slot, two accounts at a time: the Mercenary Captain, the camp's slaves, Captain Siad's chest, Al Shabim,
// the cave guards, the carts and Ana are single things a crowd of accounts would fight over. The slot is held for the whole run (no step releases it
// early) and does not repeat.
func TestTouristTrapHoldsAnExclusiveSlotOfTwo(t *testing.T) {
	id := questplan.TouristTrapID
	if !exclusiveQuests[id] || capacityOf(id) != 2 || slotRepeats[id] {
		t.Fatalf("exclusive %v capacity %d repeats %v", exclusiveQuests[id], capacityOf(id), slotRepeats[id])
	}
	a, b, c := &Deps{}, &Deps{}, &Deps{}
	now := time.Now()
	if !tryHoldQuest(id, a, now) || !tryHoldQuest(id, b, now) {
		t.Fatal("the first two accounts did not get a slot")
	}
	if tryHoldQuest(id, c, now) {
		t.Error("a third account got a slot: the capacity is two")
	}
	releaseQuest(id, a)
	if !tryHoldQuest(id, c, now) {
		t.Error("the slot a holder gave up was not free")
	}
	releaseQuest(id, b)
	releaseQuest(id, c)
}
