package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Plague City (18 Oct 2026) runs under a fleet-wide slot of four: the bucket by Brother Cedric's cart, the spade and the picture on
// Edmond's floor, the single chocolate bar in Wydin's shop and the snape grass spawns on the hobgoblins' trail are each a handful of
// items that a crowd of accounts would race for, and the plague house's door needs a mourner within earshot. The fifth account waits
// and a release lets it in. The run is far shorter than a slot's life (the ladder facts' estimate is under slotStale), so a holder that
// finishes frees the slot long before it would be dropped as abandoned.
func TestPlagueCityRunsUnderSlotsOfFour(t *testing.T) {
	id := questplan.PlagueCityID
	if !exclusiveQuests[id] || capacityOf(id) != 4 || slotRepeats[id] {
		t.Fatalf("%s: exclusive %v capacity %d repeats %v, want true, 4 and false", id, exclusiveQuests[id], capacityOf(id), slotRepeats[id])
	}
	if est := time.Duration(questplan.LadderFacts[id].EstMinutes) * time.Minute; est <= 0 || est >= slotStale {
		t.Errorf("the quest's estimate %s is not within a slot's life of %s", est, slotStale)
	}
	now := time.Now()
	ds := make([]*Deps, 6)
	for i := range ds {
		ds[i] = &Deps{}
	}
	for i := 0; i < 4; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("account %d of 4 was refused", i+1)
		}
	}
	if tryHoldQuest(id, ds[4], now) {
		t.Error("a fifth account was admitted")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[4], now) {
		t.Error("a released slot was not given to the fifth account")
	}
	// A holder that never released (a relog dropped its Deps) is dropped after slotStale, and the sixth account gets in.
	if tryHoldQuest(id, ds[5], now) {
		t.Error("a sixth account was admitted")
	}
	if !tryHoldQuest(id, ds[5], now.Add(slotStale+time.Minute)) {
		t.Error("an abandoned slot was not given up after slotStale")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}
