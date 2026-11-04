package questrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// A goblin that wanders north of z 3519 is in the Wilderness: an account in Goblin Village does not chase it,
// while an account already inside the Wilderness may still fight what stands there.
func TestWildernessChase(t *testing.T) {
	village := tile.Tile{X: 2956, Z: 3505}
	for _, c := range []struct {
		name  string
		here  tile.Tile
		x, z  int
		chase bool
	}{
		{"village goblin", village, 2958, 3510, false},
		{"goblin over the line", village, 2958, 3521, true},
		{"both inside", tile.Tile{X: 2958, Z: 3525}, 2960, 3530, false},
		{"far from the Wilderness", tile.Tile{X: 3222, Z: 3218}, 3225, 3220, false},
	} {
		if got := wildernessChase(c.here, c.x, c.z, 0); got != c.chase {
			t.Errorf("%s: chase %v, want %v", c.name, got, c.chase)
		}
	}
}
