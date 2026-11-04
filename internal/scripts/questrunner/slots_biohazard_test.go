package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Biohazard (19 Oct 2026) runs under a fleet-wide slot of three: the rotten apples are one ground spawn, the pigeon cages three, the
// sick mourners two (an account that talks to one turns it on itself and fights it, and the key drops to the account he is aggressive
// to) and the priest's gown and robe a shelf of three at Thessalia's. The fourth account waits and a release lets it in. The run is far
// shorter than a slot's life (the ladder facts' estimate is under slotStale), so a holder that finishes frees the slot long before it
// would be dropped as abandoned; and the module gives the slot up once Elena has the distillator, which is where the crowded steps end.
func TestBiohazardRunsUnderSlotsOfThree(t *testing.T) {
	id := questplan.BiohazardID
	if !exclusiveQuests[id] || capacityOf(id) != 3 || slotRepeats[id] {
		t.Fatalf("%s: exclusive %v capacity %d repeats %v, want true, 3 and false", id, exclusiveQuests[id], capacityOf(id), slotRepeats[id])
	}
	if est := time.Duration(questplan.LadderFacts[id].EstMinutes) * time.Minute; est <= 0 || est >= slotStale {
		t.Errorf("the quest's estimate %s is not within a slot's life of %s", est, slotStale)
	}
	if questplan.Biohazard.ExclusiveDone == nil {
		t.Error("the module never gives its slot up before the end of the run")
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
	// A holder that never released (a relog dropped its Deps) is dropped after slotStale, and the fifth account gets in.
	if tryHoldQuest(id, ds[4], now) {
		t.Error("a fifth account was admitted")
	}
	if !tryHoldQuest(id, ds[4], now.Add(slotStale+time.Minute)) {
		t.Error("an abandoned slot was not given up after slotStale")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}
