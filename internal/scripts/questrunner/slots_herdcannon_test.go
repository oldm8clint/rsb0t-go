package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Sheep Herder and Dwarf Cannon (19 Oct 2026) run under fleet-wide slots. Sheep Herder is one account at a time: its four sheep are world npcs
// with three map copies each, the sheep the herd sends over the fence is a 150 tick npc_add at a fixed tile that a second account herding the
// same sheep would feed and kill from under the first, and the barn holds one prod. Dwarf Cannon admits three: the remains are one ground
// spawn on the watchtower's top floor, the dwarf child one npc the crate adds for 120 ticks.
func TestSheepHerderRunsAloneAndDwarfCannonUnderASlotOfThree(t *testing.T) {
	for _, c := range []struct {
		id       string
		capacity int
	}{{questplan.SheepHerderID, 1}, {questplan.DwarfCannonID, 3}} {
		id := c.id
		if !exclusiveQuests[id] || capacityOf(id) != c.capacity {
			t.Errorf("%s: exclusive %v capacity %d, want true and %d", id, exclusiveQuests[id], capacityOf(id), c.capacity)
		}
		now := time.Now()
		ds := make([]*Deps, c.capacity+2)
		for i := range ds {
			ds[i] = &Deps{}
		}
		for i := 0; i < c.capacity; i++ {
			if !tryHoldQuest(id, ds[i], now) {
				t.Fatalf("%s: account %d of %d was refused", id, i+1, c.capacity)
			}
		}
		if tryHoldQuest(id, ds[c.capacity], now) {
			t.Errorf("%s: account %d was admitted", id, c.capacity+1)
		}
		releaseQuest(id, ds[0])
		if !tryHoldQuest(id, ds[c.capacity], now) {
			t.Errorf("%s: a released slot was not given to the next account", id)
		}
		for _, d := range ds {
			releaseQuest(id, d)
		}
		if _, held := slots[id]; held {
			t.Errorf("%s: slots left behind: %v", id, slots[id])
		}
		// Neither slot repeats: once past the shared steps the account never queues for them again.
		if slotRepeats[id] {
			t.Errorf("%s is a repeating slot", id)
		}
	}
}

// The engine gives the slot up once the module says the account is past the shared steps.
func TestSheepHerderAndDwarfCannonReleaseTheirSlotsAtTheirSharedSteps(t *testing.T) {
	herd := questplan.SheepHerder
	if herd.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.HerdStageWith([4]int{3, 3, 3, 2}), StageKnown: true}) ||
		!herd.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.HerdAllBurnt, StageKnown: true}) {
		t.Error("Sheep Herder's slot is not released at the fourth sheep")
	}
	cannon := questplan.DwarfCannon
	if cannon.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.McFindChild, StageKnown: true}) ||
		!cannon.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.McChildRescued, StageKnown: true}) {
		t.Error("Dwarf Cannon's slot is not released at the child")
	}
}
