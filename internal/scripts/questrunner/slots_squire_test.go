package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Knight's Sword (20 Oct 2026) runs under a fleet-wide slot of two: the pie dish is a ground spawn in the Varrock palace kitchen, Wydin's
// shelf keeps three pots of flour and one redberry, the Rimmington house has one sink and one range, Sir Vyvin's cupboard is one loc, and
// the blurite rocks of the Asgarnian Ice Dungeon are a pair under the same two Ice warriors. The third account waits and a release lets it
// in. The slot is held for the whole run (there is no step it can be released at that the rest does not need).
func TestTheKnightsSwordRunsUnderASlotOfTwo(t *testing.T) {
	id := questplan.KnightsSwordID
	if !exclusiveQuests[id] || capacityOf(id) != 2 {
		t.Fatalf("%s: exclusive %v capacity %d, want true and 2", id, exclusiveQuests[id], capacityOf(id))
	}
	now := time.Now()
	ds := make([]*Deps, 4)
	for i := range ds {
		ds[i] = &Deps{}
	}
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
		t.Error("a released slot was not given to the waiting account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
	if slotRepeats[id] || questplan.KnightsSword.ExclusiveDone != nil {
		t.Errorf("the quest repeats its slot (%v) or names an early end to it (%v)", slotRepeats[id], questplan.KnightsSword.ExclusiveDone != nil)
	}
}
