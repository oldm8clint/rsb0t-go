package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Big Chompy Bird Hunting runs under a fleet-wide slot of two: Rantz shoots and misses for one baited chompy at a time, and the pool's toads and its one
// reachable bubble, the cave's chest and the Feldip wolves are shared. A third account waits and a release lets it in. The slot is held to the end of the
// run: the quest names no ExclusiveDone and does not repeat.
func TestBigChompyBirdHuntingRunsUnderASlotOfTwo(t *testing.T) {
	id := questplan.ChompyID
	if !exclusiveQuests[id] || capacityOf(id) != 2 {
		t.Fatalf("exclusive %v capacity %d, want true and 2", exclusiveQuests[id], capacityOf(id))
	}
	if questplan.BigChompy.ExclusiveDone != nil || slotRepeats[id] {
		t.Errorf("the quest names an early end to its slot (%v) or repeats it (%v)", questplan.BigChompy.ExclusiveDone != nil, slotRepeats[id])
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
		t.Error("a released slot was not given to the waiting account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
}
