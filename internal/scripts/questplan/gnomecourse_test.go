package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Each obstacle's landing tile (gnome_course.rs2's movecoords) places the account in front of the
// next obstacle, and the pipe's far end in front of the log again: a lap resyncs from any tile.
func TestGnomeNextFollowsTheLap(t *testing.T) {
	cases := []struct {
		at   tile.Tile
		want int
	}{
		{GnomeStart, 0},
		{tile.Tile{X: 2474, Z: 3429}, 1},           // the log's landing
		{tile.Tile{X: 2473, Z: 3424, Level: 1}, 2}, // over the first net
		{tile.Tile{X: 2473, Z: 3420, Level: 2}, 3}, // up the branch
		{tile.Tile{X: 2483, Z: 3420, Level: 2}, 4}, // across the rope
		{tile.Tile{X: 2487, Z: 3420}, 5},           // down the far branch
		{tile.Tile{X: 2485, Z: 3427}, 6},           // over the second net
		{tile.Tile{X: 2484, Z: 3437}, 0},           // out of the pipe
		{tile.Tile{X: 2400, Z: 3400}, 0},           // off the course: walk to the start
	}
	for _, c := range cases {
		if got := GnomeNext(c.at); got != c.want {
			t.Errorf("%v: obstacle %d, want %d", c.at, got, c.want)
		}
	}
	if len(GnomeCourse) != 7 {
		t.Fatalf("the lap has %d obstacles", len(GnomeCourse))
	}
}

// Each obstacle sets the account down in front of the next one: every tile of every landing box reads as the next obstacle,
// and a lap resyncs from the pipe's end to the log.
func TestGnomeLandingsAreInFrontOfTheNextObstacle(t *testing.T) {
	for i, ob := range GnomeCourse {
		l := ob.Land
		if l.MinX > l.MaxX {
			t.Fatalf("obstacle %d: empty landing box %+v", i, l)
		}
		for x := l.MinX; x <= l.MaxX; x++ {
			at := tile.Tile{X: x, Z: l.Z, Level: l.Level}
			if !GnomeLanded(i, at) {
				t.Errorf("obstacle %d (%s): %v is in its own landing box and is not Landed", i, ob.Name, at)
			}
			if got, want := GnomeNext(at), (i+1)%len(GnomeCourse); got != want {
				t.Errorf("obstacle %d (%s): landing %v reads as obstacle %d, want %d", i, ob.Name, at, got, want)
			}
		}
		if GnomeLanded(i, ob.Stand) {
			t.Errorf("obstacle %d (%s): its own stand %v is a landing", i, ob.Name, ob.Stand)
		}
	}
	if GnomeLanded(-1, GnomeStart) || GnomeLanded(len(GnomeCourse), GnomeStart) {
		t.Error("an obstacle index out of range landed somewhere")
	}
}

// The rope and the pipe walk the account across tiles that GnomeNext already reads as the NEXT obstacle (the rope's far half,
// the pipe's second half): they are not landings, which is the whole reason there is a landing. The live walks from 2481,3420,2
// and 2484,3434,0 on 16 Oct 2026 found no path.
func TestGnomeTransitTilesAreNotLandings(t *testing.T) {
	for _, c := range []struct {
		i  int
		at tile.Tile
	}{
		{3, tile.Tile{X: 2481, Z: 3420, Level: 2}}, {3, tile.Tile{X: 2482, Z: 3420, Level: 2}},
		{6, tile.Tile{X: 2484, Z: 3434}}, {6, tile.Tile{X: 2484, Z: 3436}},
	} {
		if GnomeLanded(c.i, c.at) {
			t.Errorf("%v is a landing of obstacle %d", c.at, c.i)
		}
		if GnomeNext(c.at) == c.i {
			t.Errorf("%v still reads as obstacle %d: the test names a tile that does not show the premature reading", c.at, c.i)
		}
	}
}

// The upper floors are the level-1 platform and the level-2 platforms, and no tile of the ground.
func TestInGnomeUpper(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{tile.Tile{X: 2473, Z: 3424, Level: 1}, true},  // after the net
		{tile.Tile{X: 2473, Z: 3423, Level: 1}, true},  // the branch's stand
		{tile.Tile{X: 2473, Z: 3420, Level: 2}, true},  // after the branch
		{tile.Tile{X: 2477, Z: 3420, Level: 2}, true},  // the rope's stand
		{tile.Tile{X: 2481, Z: 3420, Level: 2}, true},  // mid-rope
		{tile.Tile{X: 2483, Z: 3420, Level: 2}, true},  // after the rope
		{tile.Tile{X: 2485, Z: 3419, Level: 2}, true},  // the far branch's stand
		{tile.Tile{X: 2487, Z: 3420}, false},           // the ground it lands on
		{tile.Tile{X: 2474, Z: 3429}, false},           // after the log
		{tile.Tile{X: 2484, Z: 3437}, false},           // after the pipe
		{tile.Tile{X: 2473, Z: 3426}, false},           // the net's stand
		{tile.Tile{X: 2474, Z: 3433, Level: 1}, false}, // the platform north of the log is not the course's
		{tile.Tile{X: 2400, Z: 3400, Level: 2}, false},
	} {
		if got := InGnomeUpper(c.at); got != c.want {
			t.Errorf("%v: InGnomeUpper = %v, want %v", c.at, got, c.want)
		}
	}
	for i, ob := range GnomeCourse {
		if want := ob.Stand.Level > 0; InGnomeUpper(ob.Stand) != want {
			t.Errorf("obstacle %d (%s): stand %v upper = %v, want %v", i, ob.Name, ob.Stand, InGnomeUpper(ob.Stand), want)
		}
	}
}
