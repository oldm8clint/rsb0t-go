package webwalk

import "testing"

// Until ends a walk, as arrived, the moment its condition holds: a fisher returning from the
// bank stops once a fishing spot is in view instead of walking on to the camp's centre.
func TestWalkToStopsWhenUntilHolds(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 0})
	res := w.WalkTo(Point{X: 5, Z: 4}, WalkOptions{Until: func() bool { return f.me.X <= 10 }})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if f.me.X > 10 || f.me.X <= 5 {
		t.Fatalf("stopped at %s, want just past x 10, well short of the destination x 5", f.me)
	}
	if w.until != nil {
		t.Fatal("Until leaked past the walk it belonged to")
	}
}

func TestWalkToWithoutUntilStillWalksToTheDestination(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 0})
	if res := w.WalkTo(Point{X: 5, Z: 4}, WalkOptions{}); !res.Arrived() || f.me != (Point{X: 5, Z: 4}) {
		t.Fatalf("result = %+v, ended at %s", res, f.me)
	}
}
