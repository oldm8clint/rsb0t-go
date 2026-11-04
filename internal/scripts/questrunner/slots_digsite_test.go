package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Digsite Quest (19 Oct 2026) runs under a fleet-wide slot of three: its panning tray is one ground spawn, its stone tablet another in a cave
// of ten skeletons, its cave workman one npc in a closed shaft, and its chest, bush, sacks and pockets one site. The fourth account waits and a
// release lets it in. The slot is held for the whole run (the tray, the cave workman and the tablet are needed at different stages), and the
// quest's later runs queue for it again.
func TestTheDigsiteRunsUnderASlotOfThree(t *testing.T) {
	id := questplan.DigsiteID
	if !exclusiveQuests[id] || capacityOf(id) != 3 {
		t.Fatalf("%s: exclusive %v capacity %d, want true and 3", id, exclusiveQuests[id], capacityOf(id))
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
	if slotRepeats[id] {
		t.Error("the Digsite is a repeating slot: its shared steps are not met again after a run that passed them")
	}
	if questplan.Digsite.ExclusiveDone != nil {
		t.Error("the Digsite releases its slot early: the module holds it for the whole run")
	}
}
