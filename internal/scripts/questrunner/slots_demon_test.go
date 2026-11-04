package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Demon Slayer (18 Oct 2026) runs under a fleet-wide slot of three, held for the whole run: the drain key is a public ground item in the
// sewer for three minutes after a pour and Delrith is one npc that single combat gives to one account at a time. The fourth account
// waits and a release lets it in. It names no ExclusiveDone, so there is no narrow release to test: the slot is the run's.
func TestDemonSlayerRunsUnderASlotOfThree(t *testing.T) {
	id := questplan.DemonSlayerID
	if !exclusiveQuests[id] || capacityOf(id) != 3 {
		t.Errorf("exclusive %v capacity %d, want true and 3", exclusiveQuests[id], capacityOf(id))
	}
	if questplan.DemonSlayer.ExclusiveDone != nil {
		t.Error("the module names an ExclusiveDone: the slot would be released before the circle")
	}
	now := time.Now()
	ds := make([]*Deps, 5)
	for i := range ds {
		ds[i] = &Deps{}
	}
	for i := 0; i < 3; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("account %d of 3 was refused", i+1)
		}
	}
	if tryHoldQuest(id, ds[3], now) {
		t.Error("a fourth account was admitted")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[3], now) {
		t.Error("a released slot was not given to the fourth account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}

// A run's slot is shorter than the 90 minutes after which a holder that never released is dropped (slotStale), or a second account would be
// let into a slot a live run still holds.
func TestDemonSlayersSlotIsShorterThanTheStaleLimit(t *testing.T) {
	if got := questplan.QuestSlot(questplan.DemonSlayerID, 20*time.Minute); got >= slotStale {
		t.Errorf("the quest's slot %s reaches the stale limit %s", got, slotStale)
	}
}
