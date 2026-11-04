package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Tai Bwo Wannai Trio runs under a fleet-wide slot of three: the monkeys, the Jogre pen, Zambo's rum shelf, the Holy Lake's spots and
// the cutscene of Tamayu's hunt are shared, and a run is long. The fourth account waits and a release lets it in. The slot is held to
// the end of the run: the quest names no ExclusiveDone and does not repeat.
func TestTaiBwoWannaiTrioRunsUnderASlotOfThree(t *testing.T) {
	id := questplan.TaiBwoID
	if !exclusiveQuests[id] || capacityOf(id) != 3 {
		t.Fatalf("exclusive %v capacity %d, want true and 3", exclusiveQuests[id], capacityOf(id))
	}
	if questplan.TaiBwoTrio.ExclusiveDone != nil || slotRepeats[id] {
		t.Errorf("the quest names an early end to its slot (%v) or repeats it (%v)", questplan.TaiBwoTrio.ExclusiveDone != nil, slotRepeats[id])
	}
	now := time.Now()
	ds := []*Deps{{}, {}, {}, {}, {}}
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
		t.Error("a released slot was not given to the waiting account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}
