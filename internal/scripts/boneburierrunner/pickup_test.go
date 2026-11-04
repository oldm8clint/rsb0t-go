package boneburierrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The engine takes a ground item only from its own tile or an orthogonal neighbour with no wall
// between (Player.inOperableDistance), and Take carries no route. Collect used to send it from
// anywhere within Chebyshev 1 — diagonals and the far side of a pen fence included — and 421
// "timed out waiting for Bones picked up" followed on 2026-09-25. These pin where Collect now
// makes the account stand first.
func TestPickupStandsOnTheDropTile(t *testing.T) {
	drop := tile.Tile{X: 2662, Z: 3348}
	cases := []struct {
		name       string
		here       tile.Tile
		walkable   bool
		wantWalk   bool
		wantRadius int
	}{
		// THE LIVE FAULT: one tile away diagonally. The old rule (Chebyshev <= 1, no walk) clicked
		// from here, and the engine refuses a diagonal.
		{"diagonal neighbour", tile.Tile{X: 2661, Z: 3347}, true, true, 0},
		// Orthogonal, but at a cow pen the neighbour can be across the fence; the walk onto the
		// tile is what routes round it (or reports no path and the drop is skipped, fast).
		{"orthogonal neighbour", tile.Tile{X: 2661, Z: 3348}, true, true, 0},
		{"four tiles off", tile.Tile{X: 2658, Z: 3348}, true, true, 0},
		{"already on it", drop, true, false, 0},
		// A drop on a tile nobody can stand on is still taken from beside it, as before.
		{"unwalkable drop, orthogonal", tile.Tile{X: 2661, Z: 3348}, false, false, 1},
		{"unwalkable drop, diagonal", tile.Tile{X: 2661, Z: 3347}, false, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			radius, walk := pickupApproach(tc.here, drop, tc.walkable)
			if walk != tc.wantWalk || (walk && radius != tc.wantRadius) {
				t.Errorf("pickupApproach(%v -> %v, walkable=%v) = radius %d walk %v, want radius %d walk %v",
					tc.here, drop, tc.walkable, radius, walk, tc.wantRadius, tc.wantWalk)
			}
		})
	}
}
