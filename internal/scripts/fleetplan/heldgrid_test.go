package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The bitmap answers exactly what scanning the threats does, over the pen and a margin beyond it.
func TestHeldGridAgreesWithScanningTheThreats(t *testing.T) {
	for _, pen := range []TrainPen{YanilleWestOgres, FeldipOgresWest, FeldipOgresEast, MossGiantsNorthArdougne} {
		g := NewHeldGrid(pen.Threats)
		held := 0
		for x := pen.Stand.X - pen.Leash - 40; x <= pen.Stand.X+pen.Leash+40; x++ {
			for z := pen.Stand.Z - pen.Leash - 40; z <= pen.Stand.Z+pen.Leash+40; z++ {
				want := false
				for _, th := range pen.Threats {
					if th.Holds(tile.Tile{X: x, Z: z}) {
						want = true
						break
					}
				}
				if got := g.Holds(x, z, 0); got != want {
					t.Fatalf("%s: Holds(%d,%d) = %v, scanning the threats says %v", pen.Name, x, z, got, want)
				}
				if want {
					held++
				}
				if g.Holds(x, z, 1) {
					t.Fatalf("%s: %d,%d is held on level 1", pen.Name, x, z)
				}
			}
		}
		if held == 0 {
			t.Errorf("%s: no tile is held", pen.Name)
		}
	}
}

func TestHeldGridCornersAreNotHeldAndNilHoldsNothing(t *testing.T) {
	th := PenThreat{At: tile.Tile{X: 100, Z: 100}, Size: 2, Cut: 6, Name: "Ogre", Herd: true}
	g := NewHeldGrid([]PenThreat{th})
	if !g.Holds(106, 100, 0) || !g.Holds(100, 94, 0) {
		t.Error("the edge of the chase range is not held")
	}
	if g.Holds(106, 106, 0) {
		t.Error("the exact corner is held (Npc.targetWithinMaxRange excludes it)")
	}
	if g.Holds(107, 100, 0) {
		t.Error("a tile past the chase range is held")
	}
	var none *HeldGrid
	if none.Holds(100, 100, 0) || NewHeldGrid(nil).Holds(100, 100, 0) {
		t.Error("an empty grid holds a tile")
	}
	if n := g.Count([]tile.Tile{{X: 100, Z: 100}, {X: 200, Z: 200}, {X: 103, Z: 100}}); n != 2 {
		t.Errorf("Count = %d, want 2", n)
	}
}
