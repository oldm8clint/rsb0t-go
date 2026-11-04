package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Observatory Quest and the Sea Slug Quest (18 Oct 2026) run under fleet-wide slots of three: the Observatory's four plank spawns,
// its one sand spawn, its key chest (shut again 300 ticks after a use), its goblin guard (one npc, back 500 ticks after his death) and its
// sack are shared, and the Sea Slug Quest's damp sticks and broken glass are single ground spawns on the one platform. The fourth account
// waits and a release lets it in.
func TestTheObservatoryAndTheSeaSlugRunUnderSlotsOfThree(t *testing.T) {
	for _, id := range []string{questplan.ObservatoryID, questplan.SeaSlugID} {
		if !exclusiveQuests[id] || capacityOf(id) != 3 {
			t.Errorf("%s: exclusive %v capacity %d, want true and 3", id, exclusiveQuests[id], capacityOf(id))
		}
		now := time.Now()
		ds := make([]*Deps, 5)
		for i := range ds {
			ds[i] = &Deps{}
		}
		for i := 0; i < 3; i++ {
			if !tryHoldQuest(id, ds[i], now) {
				t.Fatalf("%s: account %d of 3 was refused", id, i+1)
			}
		}
		if tryHoldQuest(id, ds[3], now) {
			t.Errorf("%s: a fourth account was admitted", id)
		}
		releaseQuest(id, ds[0])
		if !tryHoldQuest(id, ds[3], now) {
			t.Errorf("%s: a released slot was not given to the fourth account", id)
		}
		for _, d := range ds {
			releaseQuest(id, d)
		}
		if _, held := slots[id]; held {
			t.Errorf("%s: slots left behind: %v", id, slots[id])
		}
	}
	// Neither slot repeats: once past the crowded steps the account never queues for them again (slotRepeats is Holy Grail's alone).
	for _, id := range []string{questplan.ObservatoryID, questplan.SeaSlugID} {
		if slotRepeats[id] {
			t.Errorf("%s is a repeating slot", id)
		}
	}
}

// The engine gives the slot up once the module says the account is past the shared steps: the quest's own ExclusiveDone is what it asks.
func TestTheObservatoryAndTheSeaSlugReleaseTheirSlotsAtTheirSharedSteps(t *testing.T) {
	obs := questplan.Observatory
	if obs.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.ObsGivenGlass, StageKnown: true}) ||
		!obs.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.ObsGivenMould, StageKnown: true}) {
		t.Error("the Observatory's slot is not released at the lens")
	}
	slug := questplan.SeaSlug
	if slug.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.SlugSpokenKent, StageKnown: true}) ||
		!slug.ExclusiveDone(questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.SlugLitTorch, StageKnown: true}) {
		t.Error("the Sea Slug Quest's slot is not released at the lit torch")
	}
}
