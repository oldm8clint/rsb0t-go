package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Doric's Quest shares Rimmington's two clay rocks and its one pickaxe spawn, and Gertrude's Cat the five doogle leaf spawns and
// the farmhouse bucket: each admits a handful of accounts at once (6 and 8), and the next one waits until a slot is released.
func TestDoricAndGertrudesCatAdmitTheirCrowdsInSlots(t *testing.T) {
	for id, capacity := range map[string]int{questplan.DoricsQuestID: 6, questplan.GertrudesCatID: 8} {
		if !exclusiveQuests[id] || capacityOf(id) != capacity {
			t.Errorf("%s: exclusive %v capacity %d, want true and %d", id, exclusiveQuests[id], capacityOf(id), capacity)
			continue
		}
		now := time.Now()
		ds := make([]*Deps, capacity+2)
		for i := range ds {
			ds[i] = &Deps{}
			defer releaseQuest(id, ds[i])
		}
		for i := 0; i < capacity; i++ {
			if !tryHoldQuest(id, ds[i], now) {
				t.Fatalf("%s: holder %d refused", id, i)
			}
		}
		if tryHoldQuest(id, ds[capacity], now) {
			t.Errorf("%s: account %d must wait", id, capacity+1)
		}
		if !tryHoldQuest(id, ds[0], now) {
			t.Errorf("%s: a holder re-claiming its own slot must succeed", id)
		}
		releaseQuest(id, ds[1])
		if !tryHoldQuest(id, ds[capacity], now) {
			t.Errorf("%s: a release must let the next account in", id)
		}
		if tryHoldQuest(id, ds[capacity+1], now) {
			t.Errorf("%s: one slot freed, one account in", id)
		}
		// Neither is a repeating step: a finished run is no proof of anything about the next, and neither is in slotRepeats.
		if slotRepeats[id] {
			t.Errorf("%s is in slotRepeats", id)
		}
	}
}
