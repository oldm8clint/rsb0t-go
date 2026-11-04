package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func tileSet(ts []tile.Tile) map[tile.Tile]bool {
	m := map[tile.Tile]bool{}
	for _, t := range ts {
		m[t] = true
	}
	return m
}

// A 1x1 ladder keeps the four orthogonal neighbours tryChangeLevel always used.
func TestFootprintApproachesOneByOne(t *testing.T) {
	got := footprintApproaches(3229, 3224, 1, 1, 1, 0, tile.Tile{X: 3229, Z: 3223, Level: 1})
	want := []tile.Tile{{X: 3229, Z: 3223, Level: 1}, {X: 3228, Z: 3224, Level: 1}, {X: 3230, Z: 3224, Level: 1}, {X: 3229, Z: 3225, Level: 1}}
	if len(got) != 4 {
		t.Fatalf("got %d tiles: %v", len(got), got)
	}
	if got[0] != want[0] {
		t.Fatalf("nearest first: got %v, want %v", got[0], want[0])
	}
	set := tileSet(got)
	for _, w := range want {
		if !set[w] {
			t.Fatalf("missing %v in %v", w, got)
		}
	}
}

// The 2x2 stairstop at Morgan's house (3100,3266,1): no approach tile may sit inside the
// footprint, which the old origin-only offsets did for (3101,3266) and (3100,3267).
func TestFootprintApproachesTwoByTwoExcludesFootprint(t *testing.T) {
	got := footprintApproaches(3100, 3266, 1, 2, 2, 1, tile.Tile{X: 3102, Z: 3270, Level: 1})
	if len(got) != 8 {
		t.Fatalf("got %d tiles, want 8: %v", len(got), got)
	}
	for _, a := range got {
		if a.X >= 3100 && a.X <= 3101 && a.Z >= 3266 && a.Z <= 3267 {
			t.Fatalf("%v is inside the footprint", a)
		}
		dx := axisOutside(a.X, 3100, 3101)
		dz := axisOutside(a.Z, 3266, 3267)
		if dx+dz != 1 {
			t.Fatalf("%v is not orthogonally adjacent to the footprint", a)
		}
	}
	if got[0] != (tile.Tile{X: 3101, Z: 3268, Level: 1}) && got[0] != (tile.Tile{X: 3102, Z: 3267, Level: 1}) {
		t.Fatalf("nearest-first ordering picked %v", got[0])
	}
}

func TestFootprintApproachesRotationSwapsSides(t *testing.T) {
	// width 2, length 3: angle 0 spans x 0..1, z 0..2; angle 1 spans x 0..2, z 0..1.
	a0 := tileSet(footprintApproaches(0, 0, 0, 2, 3, 0, tile.Tile{}))
	a1 := tileSet(footprintApproaches(0, 0, 0, 2, 3, 1, tile.Tile{}))
	if !a0[tile.Tile{X: 0, Z: 3}] || a0[tile.Tile{X: 3, Z: 0}] {
		t.Fatalf("angle 0 footprint wrong: %v", a0)
	}
	if !a1[tile.Tile{X: 3, Z: 0}] || a1[tile.Tile{X: 0, Z: 3}] {
		t.Fatalf("angle 1 footprint wrong: %v", a1)
	}
	if len(a0) != 10 || len(a1) != 10 {
		t.Fatalf("sizes %d %d, want 10", len(a0), len(a1))
	}
}

func TestFootprintApproachesClampsZeroSize(t *testing.T) {
	if got := footprintApproaches(5, 5, 0, 0, 0, 0, tile.Tile{X: 5, Z: 4}); len(got) != 4 {
		t.Fatalf("zero-size loc gave %d tiles", len(got))
	}
}

func axisOutside(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo - v
	case v > hi:
		return v - hi
	}
	return 0
}
