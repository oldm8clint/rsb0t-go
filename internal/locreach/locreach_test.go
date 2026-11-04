package locreach

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// catherbyRange is the loc the whole "silent OpLocU" gotcha turned out to be about, with
// the exact values the engine printed for it on 2026-09-20:
//
//	gate3 ok loc type=2728 at=2817,3444 shape=10 angle=1 w=1 l=2 ... forceapproach=13
//
// and the exact live outcome from each tile around it, cooking raw shrimps:
//
//	2817,3444 (on tile)  cooked
//	2818,3444 (east)     cooked
//	2817,3443 (south)    cooked
//	2816,3444 (west)     "I can't reach that!"
//	2817,3445 (north)    "I can't reach that!"
var catherbyRange = Loc{X: 2817, Z: 3444, Width: 1, Length: 2, Angle: 1, Shape: 10, ForceApproach: 13}

func TestReachedMatchesTheLiveCatherbyRange(t *testing.T) {
	cases := []struct {
		name string
		x, z int
		want bool
	}{
		{"its own origin tile", 2817, 3444, true},
		{"the east half of its own 1x2 footprint, rotated", 2818, 3444, true},
		{"due south", 2817, 3443, true},
		{"due south of the east half", 2818, 3443, true},
		{"west, which the live engine refused", 2816, 3444, false},
		{"north, which the live engine refused", 2817, 3445, false},
		{"east of the footprint, which forceapproach blocks", 2819, 3444, false},
		{"diagonal", 2816, 3443, false},
	}
	for _, c := range cases {
		got, ok := Reached(c.x, c.z, catherbyRange)
		if !ok {
			t.Fatalf("%s: shape 10 must be modelled", c.name)
		}
		if got != c.want {
			t.Errorf("Reached(%d,%d) = %v, want %v (%s)", c.x, c.z, got, c.want, c.name)
		}
	}
}

func TestTilesForTheRangeAreExactlyTheOnesThatCooked(t *testing.T) {
	got := Tiles(catherbyRange)
	want := []tile.Tile{
		{X: 2817, Z: 3443}, {X: 2818, Z: 3443}, // the south side, the only open side
		{X: 2817, Z: 3444}, {X: 2818, Z: 3444}, // the footprint, last
	}
	if len(got) != len(want) {
		t.Fatalf("Tiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tiles = %v, want %v", got, want)
		}
	}
	for _, tl := range got {
		if reached, _ := Reached(tl.X, tl.Z, catherbyRange); !reached {
			t.Errorf("Tiles offered %v but Reached refuses it", tl)
		}
	}
}

// The banana crate is the other half of the same investigation: 1x1, no forceapproach, so
// every orthogonal neighbour AND its own tile work — which is what the live engine did, and
// is why the Karamja fare's failure was never about reach at all.
func TestAnUnrestricted1x1LocIsReachableFromEverySide(t *testing.T) {
	crate := Loc{X: 2943, Z: 3151, Width: 1, Length: 1, Angle: 0, Shape: 10}
	for _, c := range []tile.Tile{
		{X: 2943, Z: 3151}, {X: 2942, Z: 3151}, {X: 2944, Z: 3151},
		{X: 2943, Z: 3150}, {X: 2943, Z: 3152},
	} {
		if reached, ok := Reached(c.X, c.Z, crate); !ok || !reached {
			t.Errorf("Reached(%v) = %v (modelled=%v), want true", c, reached, ok)
		}
	}
	if len(Tiles(crate)) != 5 {
		t.Errorf("Tiles = %v, want the four sides plus the loc's own tile", Tiles(crate))
	}
}

func TestRotateFlagsMatchesTheEngine(t *testing.T) {
	// Rotation.rotateFlags: ((flags << angle) & 0xf) | (flags >>> (4 - angle)).
	// 13 = 0b1101 = north|south|west blocked. At angle 1 that becomes 0b1011 =
	// north|east|west, leaving south the only open side — the live result for the range.
	if got := rotateFlags(1, 13); got != 0b1011 {
		t.Fatalf("rotateFlags(1, 13) = %04b, want 1011", got)
	}
	if got := rotateFlags(0, 13); got != 13 {
		t.Fatalf("rotateFlags(0, 13) = %d, want 13", got)
	}
	if got := rotateFlags(2, 1); got != 0b0100 {
		t.Fatalf("rotateFlags(2, 1) = %04b, want 0100", got)
	}
}

func TestFootprintRotatesWithAnOddAngle(t *testing.T) {
	x, z, w, h := catherbyRange.Footprint()
	if x != 2817 || z != 3444 || w != 2 || h != 1 {
		t.Fatalf("Footprint = %d,%d %dx%d, want 2817,3444 2x1", x, z, w, h)
	}
	even := Loc{X: 10, Z: 20, Width: 1, Length: 2, Angle: 0, Shape: 10}
	if _, _, w, h := even.Footprint(); w != 1 || h != 2 {
		t.Fatalf("Footprint at angle 0 = %dx%d, want 1x2", w, h)
	}
}

// A shape this package does not model must say so rather than answer confidently, and must
// still hand the caller the neighbour list the call sites used before.
func TestAnUnmodelledShapeIsReportedRatherThanGuessed(t *testing.T) {
	door := Loc{X: 5, Z: 5, Width: 1, Length: 1, Angle: 0, Shape: 0}
	if _, ok := Reached(5, 6, door); ok {
		t.Error("shape 0 is a wall; Reached must report that it is not modelled")
	}
	if got := len(Tiles(door)); got != 4 {
		t.Errorf("Tiles for an unmodelled shape = %d tiles, want the 4 orthogonal neighbours", got)
	}
}

func TestNearestPicksTheClosestLegalTile(t *testing.T) {
	// Walking in from the Catherby bank, which is west of the range.
	from := tile.Tile{X: 2810, Z: 3441}
	got, ok := Nearest(from, catherbyRange)
	if !ok {
		t.Fatal("Nearest found nothing")
	}
	if want := (tile.Tile{X: 2817, Z: 3443}); got != want {
		t.Errorf("Nearest = %v, want %v", got, want)
	}
}
