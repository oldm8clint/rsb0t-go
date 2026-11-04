package locreach

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func tilesOf(ss []DecorStand) []tile.Tile {
	var out []tile.Tile
	for _, s := range ss {
		out = append(out, s.Tile)
	}
	return out
}

// engine ReachStrategy.reachWallDecor1: for the straight decorations (shapes 4 and 5) only the loc's own tile reaches it.
func TestAStraightWallDecorationIsReachedFromItsOwnTileOnly(t *testing.T) {
	for _, shape := range []int{4, 5} {
		for angle := 0; angle < 4; angle++ {
			got := WallDecorStands(Loc{X: 2663, Z: 3674, Level: 1, Angle: angle, Shape: shape})
			want := []DecorStand{{Tile: tile.Tile{X: 2663, Z: 3674, Level: 1}}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("shape %d angle %d: stands %v, want only the loc's own tile", shape, angle, got)
			}
		}
	}
}

func TestOnlyShapesFourToEightAreWallDecorations(t *testing.T) {
	for shape := -1; shape < 24; shape++ {
		want := shape >= 4 && shape <= 8
		if DecorShape(shape) != want {
			t.Errorf("DecorShape(%d) = %v", shape, !want)
		}
		if (WallDecorStands(Loc{X: 5, Z: 5, Shape: shape}) != nil) != want {
			t.Errorf("WallDecorStands for shape %d: nil-ness is wrong", shape)
		}
	}
}

// The diagonal decorations add the neighbours reachWallDecor1 names, each with the wall face that must be open.
func TestADiagonalWallDecorationAddsItsNeighboursWithTheirWalls(t *testing.T) {
	at := func(dx, dz int) tile.Tile { return tile.Tile{X: 10 + dx, Z: 20 + dz} }
	cases := []struct {
		name         string
		shape, angle int
		want         []DecorStand
	}{
		{"offset, west", 6, 0, []DecorStand{{at(0, 0), 0}, {at(1, 0), WallWest}, {at(0, -1), WallNorth}}},
		{"offset, north", 6, 1, []DecorStand{{at(0, 0), 0}, {at(-1, 0), WallEast}, {at(0, -1), WallNorth}}},
		{"offset, east", 6, 2, []DecorStand{{at(0, 0), 0}, {at(-1, 0), WallEast}, {at(0, 1), WallSouth}}},
		{"offset, south", 6, 3, []DecorStand{{at(0, 0), 0}, {at(1, 0), WallWest}, {at(0, 1), WallSouth}}},
		// Shape 7 is read two quarter turns on (alteredRotation).
		{"no offset, west reads east", 7, 0, []DecorStand{{at(0, 0), 0}, {at(-1, 0), WallEast}, {at(0, 1), WallSouth}}},
		{"no offset, north reads south", 7, 1, []DecorStand{{at(0, 0), 0}, {at(1, 0), WallWest}, {at(0, 1), WallSouth}}},
		{"both", 8, 2, []DecorStand{{at(0, 0), 0}, {at(0, 1), WallSouth}, {at(0, -1), WallNorth}, {at(-1, 0), WallEast}, {at(1, 0), WallWest}}},
	}
	for _, c := range cases {
		got := WallDecorStands(Loc{X: 10, Z: 20, Shape: c.shape, Angle: c.angle})
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: stands %v, want %v", c.name, got, c.want)
		}
	}
	if got := tilesOf(WallDecorStands(Loc{X: 10, Z: 20, Shape: 8})); len(got) != 5 {
		t.Errorf("a decoration on both diagonals has %d stands, want 5", len(got))
	}
}
