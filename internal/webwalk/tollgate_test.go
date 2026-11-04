package webwalk

import "testing"

// The toll gate's four edges must land one tile BEYOND the gate square rather than on it. See
// tollGateWhy: landing on 3268 puts the player inside a three-tick inviswall box, on the middle
// tile of a shuttle that paying again simply reverses.
func TestTollGateEdgesLandBeyondTheGate(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	want := map[Point]Point{
		{X: 3267, Z: 3227}: {X: 3269, Z: 3227},
		{X: 3268, Z: 3227}: {X: 3266, Z: 3227},
		{X: 3267, Z: 3228}: {X: 3269, Z: 3228},
		{X: 3268, Z: 3228}: {X: 3266, Z: 3228},
	}
	seen := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.LocID != 2882 && e.LocID != 2883 {
			continue
		}
		exp, ok := want[e.From]
		if !ok {
			t.Errorf("unexpected toll edge from %s", e.From)
			continue
		}
		if e.To != exp {
			t.Errorf("toll edge %s -> %s, want landing %s (on the gate square it shuttles back and forth)", e.From, e.To, exp)
		}
		if e.Special == nil {
			t.Errorf("toll edge %s lost its special crossing", e.From)
		}
		seen++
	}
	if seen != len(want) {
		t.Errorf("matched %d toll edges, want %d — a correction that matches nothing is a silent no-op", seen, len(want))
	}
}

// The landing tiles have to be walkable, or the planner drops the edge outright (plan.go's
// unwalkable-ends filter) and the gate becomes uncrossable rather than merely expensive. Without
// a collision pack there is nothing to check against, so this asserts the weaker but still
// useful property: no toll edge lands on the gate square itself.
func TestTollGateEdgesNeverLandOnTheGateSquare(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.LocID != 2882 && e.LocID != 2883 {
			continue
		}
		if e.To.X == 3268 {
			t.Errorf("toll edge %s still lands on the gate square %s", e.From, e.To)
		}
	}
}

// FromTile is what tells the two directions of one barrier apart. Without it a correction
// written for the eastbound leaf would also rewrite the westbound one, since they share a loc
// id, a placement and an action.
func TestCorrectionFromTileSelectsOneDirection(t *testing.T) {
	east := Correction{LocID: 2882, LocX: 3268, LocZ: 3227, Level: 0, Action: "Open", FromTile: pt(3267, 3227)}
	fwd := Edge{LocID: 2882, LocX: 3268, LocZ: 3227, Action: "Open", From: Point{X: 3267, Z: 3227}}
	rev := Edge{LocID: 2882, LocX: 3268, LocZ: 3227, Action: "Open", From: Point{X: 3268, Z: 3227}}
	if !east.matches(&fwd) {
		t.Error("the eastbound correction did not match its own edge")
	}
	if east.matches(&rev) {
		t.Error("the eastbound correction also matched the westbound edge; FromTile is not selecting")
	}
}
