package questplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// Who can reach the tile the shed fight is cast from, on the real collision pack (skipped without
// RSB0T_COLLISION_PACK). The threat model is the Black Demon's (grandtree_pack_test.go): a form keeps a melee
// target only while a tile its body can stand on is orthogonally beside it, and its body positions are every
// one a breadth-first search over the pack's steps reaches from its spawn, which is MORE than the engine's
// naively pathing NPCs reach.
//
// witchshouse.go says the glob is cast at from outside its range and the rest from the one-front tile
// 2936,3459, "where the size-2 bear freezes at 2936,3461". WitchShedLoss prices the spider and the wolf
// (size 1) at that tile and the bear too (pessimistically); this pins which of them can in fact stand beside it.
func TestShedFormsAndTheOneFrontTile(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// ^ball_experiment_spawn_coord 0_45_54_55_6; the forms' maxrange is the default 7, and a body is kept
	// within maxrange+1 of its spawn (Npc.targetWithinMaxRange) plus its own size.
	spawnX, spawnZ := 2880+55, 3456+6
	front := WitchOneFront
	beside := func(size int) (reach int) {
		for p := range gtBodyPositions(pk, spawnX, spawnZ, size, 8+size) {
			if gtMeleeBeside(pk, p, size, front) {
				reach++
			}
		}
		return reach
	}
	if n := beside(1); n == 0 {
		t.Errorf("a size-1 form cannot stand beside %v: the spider and the wolf never reach the account, so the cast tile is a safespot the file comment does not know about", front)
	} else {
		t.Logf("size-1 forms stand beside %v from %d positions", front, n)
	}
	bear := beside(2)
	t.Logf("the size-2 bear stands beside %v from %d positions", front, bear)
	if bear != 0 {
		// The loss model counts the bear in full, so this is a finding and not a failure of the gate; the comment
		// in witchshouse.go ("the size-2 bear freezes at 2936,3461") is what would be wrong.
		t.Errorf("the bear can reach %v from %d positions: update witchshouse.go's comment on WitchOneFront", front, bear)
	}
	// The corridor tile 2936,3460 is the way in for a size-1 body, one tile wide for the bear.
	if !pk.Walkable(2936, 3460, 0) {
		t.Fatal("the corridor tile 2936,3460 is not walkable")
	}
	for _, p := range [][2]int{{2936, 3460}, {2935, 3460}} {
		ok := true
		for dx := 0; dx < 2; dx++ {
			for dz := 0; dz < 2; dz++ {
				if !pk.Walkable(p[0]+dx, p[1]+dz, 0) {
					ok = false
				}
			}
		}
		if ok {
			t.Logf("a 2x2 footprint at %v is open", p)
		}
	}
	_ = tile.Tile{}
}
