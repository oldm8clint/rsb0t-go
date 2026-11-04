package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/tile"
)

// The order of attack: whatever is on the account first, then free monsters nearest first, then
// monsters another player is fighting; in a known single-way area those last are left out (the
// Giants: "Someone else is fighting that.").
func TestRankTargets(t *testing.T) {
	const self = 5
	face := func(slot int) int { return 32768 + slot }
	here := tile.Tile{X: 100, Z: 100}
	onUs := gamestate.NPC{Index: 1, X: 108, Z: 100, FaceEntity: face(self), InCombat: true}
	freeFar := gamestate.NPC{Index: 2, X: 106, Z: 100, FaceEntity: -1}
	freeNear := gamestate.NPC{Index: 3, X: 102, Z: 100, FaceEntity: -1}
	busyNear := gamestate.NPC{Index: 4, X: 101, Z: 100, FaceEntity: face(9), InCombat: true}
	hitByOther := gamestate.NPC{Index: 6, X: 101, Z: 101, FaceEntity: -1, InCombat: true}
	all := []gamestate.NPC{busyNear, freeFar, onUs, hitByOther, freeNear}
	idx := func(ns []gamestate.NPC) []int {
		out := make([]int, len(ns))
		for i, n := range ns {
			out[i] = n.Index
		}
		return out
	}
	eq := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if got := idx(rankTargets(all, here, self, false, true)); !eq(got, []int{1, 3, 2}) {
		t.Errorf("single-way: %v, want [1 3 2] (on us, then free nearest first, busy left out)", got)
	}
	if got := idx(rankTargets(all, here, self, true, true)); !eq(got, []int{1, 3, 2, 4, 6}) {
		t.Errorf("multiway: %v, want [1 3 2 4 6] (busy last, nearest first)", got)
	}
	if got := idx(rankTargets(all, here, self, false, false)); !eq(got, []int{1, 3, 2, 4, 6}) {
		t.Errorf("area not known: %v, want busy kept last", got)
	}
	if got := rankTargets(nil, here, self, false, true); len(got) != 0 {
		t.Errorf("no candidates: %v", got)
	}
}
