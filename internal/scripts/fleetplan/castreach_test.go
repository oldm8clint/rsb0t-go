package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// walls is a test sight grid: faces and full blockers by tile.
type walls map[[2]int]Sight

func (w walls) at(x, z int) Sight { return w[[2]int{x, z}] }

func TestLineOfSightOverOpenGroundAndWater(t *testing.T) {
	from := tile.Tile{X: 2590, Z: 3438}
	for _, to := range []tile.Tile{{X: 2599, Z: 3438}, {X: 2599, Z: 3447}, {X: 2595, Z: 3430}, {X: 2581, Z: 3441}, from} {
		if !LineOfSight(from, to, walls{}.at) {
			t.Errorf("open ground %v -> %v: no line of sight", from, to)
		}
	}
	if !LineOfSight(from, tile.Tile{X: 2599, Z: 3438}, nil) {
		t.Error("a nil grid sees through everything")
	}
	if LineOfSight(from, tile.Tile{X: 2595, Z: 3438, Level: 1}, nil) {
		t.Error("another level is never in sight")
	}
}

// The engine's ray enters each tile through one face: a wall on that face stops it, a wall on the
// far face of the same tile does not, and the destination's own full blocker never counts.
func TestLineOfSightStopsAtWallsTheRayCrosses(t *testing.T) {
	from, to := tile.Tile{X: 10, Z: 10}, tile.Tile{X: 16, Z: 10}
	if LineOfSight(from, to, walls{{13, 10}: SightWest}.at) {
		t.Error("a wall on the west face of a tile the eastward ray enters must block it")
	}
	if !LineOfSight(from, to, walls{{13, 10}: SightEast}.at) {
		// SightEast on 13 is the face the ray leaves by, not enters by; the engine checks the
		// entered tile's near face only (a wall between 13 and 14 is ALSO 14's west face).
		t.Error("the far face of a tile is not the face the ray enters by")
	}
	if !LineOfSight(to, from, walls{{13, 10}: SightWest}.at) {
		// Westward the ray enters 13 through its east face. (The engine marks a wall on both
		// sides, 13's west AND 12's east; this grid holds only the one face on purpose.)
		t.Error("westward, 13's west face is the one the ray leaves by")
	}
	if LineOfSight(to, from, walls{{13, 10}: SightEast}.at) {
		t.Error("westward, 13's east face is the one entered")
	}
	if LineOfSight(from, to, walls{{13, 10}: SightFull}.at) {
		t.Error("a projectile blocker filling a tile on the way blocks the ray")
	}
	if !LineOfSight(from, to, walls{{16, 10}: SightFull}.at) {
		t.Error("the destination's own full blocker does not hide it (engine: flagProj cleared at the end tile)")
	}
	if LineOfSight(from, to, walls{{16, 10}: SightWest}.at) {
		t.Error("a wall on the destination's near face does block")
	}
	// North-south and diagonal rays use the z faces.
	if LineOfSight(tile.Tile{X: 5, Z: 5}, tile.Tile{X: 5, Z: 12}, walls{{5, 8}: SightSouth}.at) {
		t.Error("northward ray, a south-face wall on the way")
	}
	if LineOfSight(tile.Tile{X: 5, Z: 5}, tile.Tile{X: 10, Z: 10}, walls{{7, 7}: SightFull}.at) {
		t.Error("a diagonal ray through a full blocker")
	}
	if !LineOfSight(tile.Tile{X: 5, Z: 5}, tile.Tile{X: 10, Z: 10}, walls{{7, 9}: SightFull}.at) {
		t.Error("a diagonal ray is not blocked by a tile off its line")
	}
}

// Content: autocast's player_attackrange is 10, a hand cast's apRange is 10, Chebyshev. The choice
// keeps one tile of margin for a Duck that moves before the cast is judged.
func TestCastableFromIsTenTilesLessTheMargin(t *testing.T) {
	from := tile.Tile{X: 2590, Z: 3438}
	if CastRange != 10 {
		t.Fatalf("CastRange %d: content's player_attackrange with autocast on is 10", CastRange)
	}
	if !CastableFrom(from, tile.Tile{X: 2599, Z: 3447}, nil) {
		t.Error("9 tiles diagonally: in reach")
	}
	if CastableFrom(from, tile.Tile{X: 2600, Z: 3438}, nil) {
		t.Error("10 tiles: outside the margin")
	}
	if CastableFrom(from, tile.Tile{X: 2595, Z: 3438}, walls{{2593, 3438}: SightWest}.at) {
		t.Error("in range but behind a wall")
	}
}

func TestCastTileStaysPutWhenItReachesAndOtherwiseTakesTheNearestShoreTile(t *testing.T) {
	pen := LakeDucksWest
	duck := tile.Tile{X: 2597, Z: 3443}
	here := tile.Tile{X: 2590, Z: 3438}
	if got, ok := pen.CastTile(&here, duck, nil); !ok || got != here {
		t.Fatalf("in reach from where it stands: stay (%v), got %v %v", here, got, ok)
	}
	// Out of reach where it stands: the nearest cast tile that reaches.
	far := tile.Tile{X: 2582, Z: 3435}
	got, ok := pen.CastTile(&far, tile.Tile{X: 2599, Z: 3434}, nil)
	if !ok || got != (tile.Tile{X: 2590, Z: 3438}) {
		t.Fatalf("from %v at the Duck on 2599,3434: 2590,3438 (8 away, 9 from the Duck) before 2591,3431 (9 away), got %v %v", far, got, ok)
	}
	// A Duck on 2600,3430 is 10 from 2590,3438: only 2591,3431 and 2593,3426 reach it.
	if got, ok := pen.CastTile(&far, tile.Tile{X: 2600, Z: 3430}, nil); !ok || got != (tile.Tile{X: 2591, Z: 3431}) {
		t.Fatalf("from %v at a Duck on 2600,3430: 2591,3431, got %v %v", far, got, ok)
	}
	// Nothing on this shore reaches a mid-lake Duck.
	if got, ok := pen.CastTile(&far, tile.Tile{X: 2610, Z: 3440}, nil); ok {
		t.Fatalf("a Duck at 2610,3440 is 17+ tiles from every west-shore cast tile, got %v", got)
	}
	// A wall between every cast tile and the Duck: none reaches.
	blind := func(x, z int) Sight { return SightWest | SightEast | SightNorth | SightSouth }
	if _, ok := pen.CastTile(nil, duck, blind); ok {
		t.Fatal("walls on every face: nothing is in sight")
	}
}

func TestCastTileNeverStandsOnAHazard(t *testing.T) {
	pen := LakeDucksNorthEast
	onDog := tile.Tile{X: 2625, Z: 3465} // inside the Guard dog's rectangle
	if !GuardDogNorthOfTheLake.Contains(onDog) {
		t.Fatal("test tile must be on the hazard")
	}
	got, ok := pen.CastTile(&onDog, tile.Tile{X: 2619, Z: 3456}, nil)
	if !ok || got == onDog || !pen.SafeTarget(got) {
		t.Fatalf("standing on the hazard: cast from a safe shore tile instead, got %v %v", got, ok)
	}
	for _, c := range pen.CastFrom {
		if !pen.SafeTarget(c) {
			t.Errorf("cast tile %v is on the pen's hazard", c)
		}
	}
}
