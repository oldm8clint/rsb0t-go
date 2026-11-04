package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Man and Farmer rungs are home ground for Stage 2 and Stage 3. Before this, a pocket from
// beside a northern Farmer read "elsewhere" (Stage 3 walked ~80 tiles home after it) and a Man
// pocket read "not arrived" (Stage 2 sailed back): both lie outside ArdougneReach and every pen.
func TestTheLowRungsAreHomeGround(t *testing.T) {
	spawns := []tile.Tile{
		{X: 2614, Z: 3318}, {X: 2621, Z: 3293}, {X: 2612, Z: 3316}, {X: 2647, Z: 3357}, // Men and Women
		{X: 2645, Z: 3367}, {X: 2646, Z: 3364}, {X: 2652, Z: 3351}, // Farmers
	}
	for _, sp := range spawns {
		for _, dx := range []int{-1, 0, 1} {
			for _, dz := range []int{-1, 0, 1} {
				at := tile.Tile{X: sp.X + dx, Z: sp.Z + dz}
				if r := RegionOf(&at); r != Region3Ardougne {
					t.Errorf("beside the spawn at %d,%d: %d,%d is %s, want ardougne", sp.X, sp.Z, at.X, at.Z, r)
				}
				if !InArdougneReach(at) {
					t.Errorf("beside the spawn at %d,%d: %d,%d is not in Ardougne reach", sp.X, sp.Z, at.X, at.Z)
				}
			}
		}
	}
	// Controls: Lumbridge is not Ardougne, and a floor above the rung is not the rung.
	lum := tile.Tile{X: 3222, Z: 3218}
	if OnArdougneLowRung(lum) || RegionOf(&lum) == Region3Ardougne {
		t.Error("Lumbridge counted as an Ardougne rung")
	}
	if OnArdougneLowRung(tile.Tile{X: 2645, Z: 3367, Level: 1}) {
		t.Error("a floor above the Farmers counted as their rung")
	}
}
