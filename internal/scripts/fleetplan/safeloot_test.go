package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The cast must still reach the target after its next step: a target whose line of sight one step
// sideways runs through a projectile blocker is refused, though it is in reach where it stands.
func TestSafeCastableMovingRefusesATargetOneStepFromCover(t *testing.T) {
	spot := tile.Tile{X: 100, Z: 100}
	at := tile.Tile{X: 106, Z: 100}
	if !SafeCastableMoving(spot, at, 1, nil) {
		t.Fatal("an open-field target six tiles off is refused")
	}
	// A blocker that hides the tile one step north-east of the target from the spot.
	cover := func(x, z int) Sight {
		if x == 104 && z == 101 {
			return SightFull
		}
		return 0
	}
	if !SafeCastable(spot, at, 1, cover) {
		t.Fatal("the target itself is hidden; the test needs it in sight")
	}
	if SafeCastableMoving(spot, at, 1, cover) {
		t.Fatal("a target one step from cover passed the moving check")
	}
	// Range: a size-2 body whose near edge is 9 off may step to 10, never past.
	if !SafeCastableMoving(spot, tile.Tile{X: 109, Z: 100}, 2, nil) {
		t.Error("near edge 9: refused")
	}
	if SafeCastableMoving(spot, tile.Tile{X: 110, Z: 100}, 2, nil) {
		t.Error("near edge 10: passed (no margin left for its step)")
	}
}
