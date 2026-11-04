package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// TestTheCatherbyPostTouchesItsBoothsCornerToCorner pins the geometry behind WalkToUse at the tile
// a ScarabSwarm hand-over leaves an account on (the mule's Catherby post, 12 October 2026): from
// 2808,3441 the booths at 2807,3442 and 2809,3442 are diagonals the engine refuses, and each is
// usable from the tile straight below it.
func TestTheCatherbyPostTouchesItsBoothsCornerToCorner(t *testing.T) {
	pk := realRoutePack(t)
	post := pathfind.NavPoint{X: 2808, Z: 3441}
	for _, booth := range []pathfind.NavPoint{{X: 2807, Z: 3442}, {X: 2809, Z: 3442}} {
		if pk.CanInteractFrom(post, booth) {
			t.Errorf("the booth at %d,%d answers from %d,%d, a diagonal of it", booth.X, booth.Z, post.X, post.Z)
		}
		below := pathfind.NavPoint{X: booth.X, Z: booth.Z - 1}
		if !pk.CanInteractFrom(below, booth) {
			t.Errorf("the booth at %d,%d does not answer from %d,%d below it (interact tiles %v)", booth.X, booth.Z, below.X, below.Z, pk.InteractTiles(booth))
		}
	}
}

// The keeper's side of the Catherby counter is not tried: from the post only the customer-side stands are kept, and a
// loc with no reachable stand keeps them all (the old behaviour).
func TestWalkToUseKeepsOnlyTheStandsTheAccountCanReach(t *testing.T) {
	d := &Deps{Pack: realRoutePack(t)}
	post := tile.Tile{X: 2808, Z: 3441}
	booth := tile.Tile{X: 2809, Z: 3442}
	all := d.Pack.InteractTiles(pathfind.NavPoint{X: booth.X, Z: booth.Z})
	kept := reachableStands(d, post, booth, all)
	if len(kept) == 0 || len(kept) >= len(all) {
		t.Fatalf("kept %v of %v: want the customer side only", kept, all)
	}
	for _, s := range kept {
		if s.Z > booth.Z {
			t.Errorf("kept %d,%d, the keeper's side of the counter", s.X, s.Z)
		}
	}
	walledIn := []pathfind.NavPoint{{X: 2809, Z: 3443}}
	if got := reachableStands(d, post, booth, walledIn); len(got) != 1 {
		t.Errorf("with no reachable stand the list should stay as it was, got %v", got)
	}
}
