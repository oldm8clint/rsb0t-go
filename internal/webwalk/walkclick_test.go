package webwalk

import (
	"testing"
	"time"
)

// A walk click starts at its route's first corner, never on the player's tile (actions.Walk says
// why), and aims legClickSteps along the route.
func TestWalkClicksNeverStartOnThePlayersTile(t *testing.T) {
	col := newGrid().fill(0, 0, 200, 6, 0)
	f := newFakeWorld(t, col, Point{X: 2, Z: 3})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	res := w.WalkTo(Point{X: 162, Z: 3}, WalkOptions{Timeout: 10 * time.Minute})
	if !res.Arrived() {
		t.Fatalf("result %+v", res)
	}
	if f.ownStarts != 0 {
		t.Errorf("%d of %d clicks started on the player's own tile", f.ownStarts, f.walks)
	}
	// 160 tiles in clicks of legClickSteps, re-clicked arriveRadius short of each target.
	if want := 160/(legClickSteps-arriveRadius) + 2; f.walks > want {
		t.Errorf("%d clicks for 160 straight tiles, want at most %d", f.walks, want)
	}
}

// Against the engine's own movement (engineSim): a start the player stands on costs the tick's step,
// and one the server has walked past walks the player back. With the start dropped, 160 open tiles
// running take about the 80 ticks their steps need however the polls fall across the ticks, at
// round trips from 40 to 500 ms; every click used to cost its tick (about 8 for this walk), and a
// late one walked the player back as well (claude/tickmanip-1005: 82-90 tiles walked for 70).
func TestWalkClicksCostNoTickUnderLatency(t *testing.T) {
	col := newGrid().fill(0, 0, 200, 6, 0)
	for _, lat := range []time.Duration{20 * time.Millisecond, 150 * time.Millisecond, 250 * time.Millisecond} {
		for phase := time.Duration(0); phase < 600*time.Millisecond; phase += 100 * time.Millisecond {
			s := newEngineSim(col, Point{X: 2, Z: 3}, phase, lat, lat, 40*time.Millisecond, 7, true)
			dest := Point{X: 162, Z: 3}
			s.stopAt = dest
			w := NewWalker(NewPlanner(col, &Graph{}), s)
			res := w.WalkTo(dest, WalkOptions{Timeout: 10 * time.Minute})
			if !s.finished {
				t.Fatalf("latency %v phase %v: never arrived: %+v", lat, phase, res)
			}
			// 80 ticks of running, the first click's own trip, and a tick of slack.
			if s.ticks > 83 {
				t.Errorf("latency %v phase %v: %d ticks for 160 tiles running (%d clicks), want at most 83", lat, phase, s.ticks, s.clicks)
			}
		}
	}
}
