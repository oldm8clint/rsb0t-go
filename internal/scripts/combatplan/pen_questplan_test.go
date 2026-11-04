package combatplan_test

// The trainer's cows rung and the Lumbridge quest farms share one pen. combatplan cannot import questplan (questplan imports it), so
// the pen's figures are written down in both places and this external test, which can import both, holds them equal: a change to
// either one that is not made to the other fails here.

import (
	"slices"
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestTheCowsRungIsTheQuestFarmsPen(t *testing.T) {
	pen := combatplan.CowPen
	if !slices.Equal(pen.Gates, questplan.CowPastureGates) {
		t.Errorf("gates: the trainer has %v, the quest farms %v", pen.Gates, questplan.CowPastureGates)
	}
	if pen.Enter != questplan.CowPastureStand {
		t.Errorf("entry: the trainer walks in to %v, the quest farms to %v", pen.Enter, questplan.CowPastureStand)
	}
	if pen.EnterRadius != questplan.CowPastureEnterRadius {
		t.Errorf("entry radius: the trainer's is %d, the quest farms' %d", pen.EnterRadius, questplan.CowPastureEnterRadius)
	}
	if !slices.Equal(pen.Beat, questplan.CowPastureSweep) {
		t.Errorf("beat: the trainer walks %v, the quest farms %v", pen.Beat, questplan.CowPastureSweep)
	}

	r, ok := combatplan.RungByKey(combatplan.MeleeLadder(), "cows")
	if !ok {
		t.Fatal("no cows rung")
	}
	less := func(a, b tile.Tile) int {
		if a.X != b.X {
			return a.X - b.X
		}
		return a.Z - b.Z
	}
	got, want := slices.Clone(r.Spawns), slices.Clone(questplan.CowPastureSpawns)
	slices.SortFunc(got, less)
	slices.SortFunc(want, less)
	if !slices.Equal(got, want) {
		t.Errorf("spawns: the cows rung lists %v, the quest farms' pen %v", got, want)
	}
}
