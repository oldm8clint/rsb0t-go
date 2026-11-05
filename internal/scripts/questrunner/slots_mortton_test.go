package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Shades of Mort'ton runs under a fleet-wide slot of one: the two villagers change type for everyone for 200 ticks after a dose of serum, the temple's walls, its
// altar and the funeral pyres are locs changed for the whole world, and the shades hunt whoever stands near. A second account waits and a release lets it in. The slot is
// held to the end of the run: the quest names no ExclusiveDone and does not repeat.
func TestShadesOfMorttonRunsUnderASlotOfOne(t *testing.T) {
	id := questplan.MorttonID
	if !exclusiveQuests[id] || capacityOf(id) != 1 {
		t.Fatalf("exclusive %v capacity %d, want true and 1", exclusiveQuests[id], capacityOf(id))
	}
	if questplan.MorttonShades.ExclusiveDone != nil || slotRepeats[id] {
		t.Errorf("the quest names an early end to its slot (%v) or repeats it (%v)", questplan.MorttonShades.ExclusiveDone != nil, slotRepeats[id])
	}
	now := time.Now()
	ds := []*Deps{{}, {}}
	if !tryHoldQuest(id, ds[0], now) {
		t.Fatal("the first account was refused")
	}
	if tryHoldQuest(id, ds[1], now) {
		t.Error("a second account was admitted")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[1], now) {
		t.Error("a released slot was not given to the waiting account")
	}
	releaseQuest(id, ds[1])
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}
