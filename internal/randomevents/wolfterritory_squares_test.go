package randomevents

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// With no margin the squares cover exactly the tiles inWolfTerritory reports, so a planner
// avoiding them and the flee avoiding territory agree on where the wolves reach.
func TestWolfTerritorySquaresMatchTerritory(t *testing.T) {
	squares := WolfTerritorySquares(0)
	in := func(x, z int) bool {
		for _, s := range squares {
			if x >= s.MinX && x <= s.MaxX && z >= s.MinZ && z <= s.MaxZ {
				return true
			}
		}
		return false
	}
	for x := 2780; x <= 2900; x++ {
		for z := 3400; z <= 3560; z++ {
			want := inWolfTerritory(pathfind.NavPoint{X: x, Z: z, Level: 0})
			if got := in(x, z); got != want {
				t.Fatalf("%d,%d: squares say %v, inWolfTerritory says %v", x, z, got, want)
			}
		}
	}
}

func TestWolfTerritorySquaresMargin(t *testing.T) {
	plain, wide := WolfTerritorySquares(0), WolfTerritorySquares(2)
	if len(plain) != len(wolfDens) || len(wide) != len(plain) {
		t.Fatalf("got %d and %d squares for %d dens", len(plain), len(wide), len(wolfDens))
	}
	for i := range plain {
		p, w := plain[i], wide[i]
		if w.MinX != p.MinX-2 || w.MaxX != p.MaxX+2 || w.MinZ != p.MinZ-2 || w.MaxZ != p.MaxZ+2 {
			t.Fatalf("square %d: margin 2 gave %+v from %+v", i, w, p)
		}
	}
	if neg := WolfTerritorySquares(-3); neg[0] != plain[0] {
		t.Fatalf("a negative margin must not shrink territory: %+v vs %+v", neg[0], plain[0])
	}
	// The eastern foothills where the live mule was attacked (2869,3438 and around) are inside.
	for _, p := range [][2]int{{2869, 3438}, {2873, 3434}, {2872, 3448}} {
		found := false
		for _, s := range plain {
			if p[0] >= s.MinX && p[0] <= s.MaxX && p[1] >= s.MinZ && p[1] <= s.MaxZ {
				found = true
			}
		}
		if !found {
			t.Fatalf("%d,%d on the eastern foothills is outside wolf territory", p[0], p[1])
		}
	}
}
