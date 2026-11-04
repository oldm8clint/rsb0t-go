package packtest

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

func TestGroundIsWalkableOnlyWhereTheTestSaysAndNeverCutsACorner(t *testing.T) {
	pk, err := Ground(10, 10, 30, 30, func(x, z int) bool { return x == 20 && z == 20 })
	if err != nil {
		t.Fatal(err)
	}
	if !pk.Walkable(19, 20, 0) || pk.Walkable(20, 20, 0) {
		t.Fatal("walkability does not follow the predicate")
	}
	// Outside the rectangle (the same mapsquare) is solid.
	if pk.Walkable(9, 10, 0) || pk.Walkable(31, 31, 0) {
		t.Fatal("ground outside the rectangle is walkable")
	}
	// A diagonal past the solid tile's corner is refused: (19,19) to (20,20) is into it, (19,21) to (20,20) too, and (19,20) to (20,21)
	// would cut the corner of (20,20).
	if mask := pk.ExitMask(19, 20, 0); mask&(1<<4) != 0 {
		t.Fatal("a diagonal step round the corner of a solid tile is legal")
	}
	if mask := pk.ExitMask(19, 21, 0); mask&(1<<1) == 0 {
		t.Fatal("the step east along the row above the solid tile is not legal")
	}
	if p := pk.FindPath(pathfind.NavPoint{X: 18, Z: 20}, pathfind.NavPoint{X: 22, Z: 20}); !p.OK {
		t.Fatalf("no route round the solid tile: %+v", p)
	}
}

func TestNatureGateHasThePillarAndTheStone(t *testing.T) {
	pk, err := NatureGate()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		x, z int
		walk bool
	}{{2865, 3020, false}, {2866, 3022, false}, {2864, 3020, true}, {2867, 3020, true}, {2867, 3021, true}, {2865, 3022, true}} {
		if got := pk.Walkable(c.x, c.z, 0); got != c.walk {
			t.Errorf("%d,%d walkable %v, want %v", c.x, c.z, got, c.walk)
		}
	}
}
