package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestStepOffTileMovesAwayFromTheWhirlpool(t *testing.T) {
	here := tile.Tile{X: 2855, Z: 3424}
	whirl := tile.Tile{X: 2855, Z: 3423}
	// The shore: water (z <= 3423) is not walkable.
	walkable := func(t tile.Tile) bool { return t.Z > 3423 }
	got, ok := stepOffTile(here, whirl, walkable)
	if !ok {
		t.Fatal("no step found on an open shore")
	}
	if tile.Chebyshev(got, whirl) <= tile.Chebyshev(here, whirl) {
		t.Fatalf("stepped to %v, no farther from the whirlpool at %v than %v", got, whirl, here)
	}
	if !walkable(got) {
		t.Fatalf("stepped into the water at %v", got)
	}
	// Boxed in: nothing walkable, no step.
	if _, ok := stepOffTile(here, whirl, func(tile.Tile) bool { return false }); ok {
		t.Fatal("stepped with nowhere walkable")
	}
}
