package webwalk

import "testing"

// Every EdgeAddition stands and lands on a walkable tile of the real pack, so NewPlanner keeps it.
func TestEdgeAdditionsSurviveThePack(t *testing.T) {
	p := realPack(t)
	for _, add := range EdgeAdditions {
		found := false
		for _, e := range p.EdgesFrom(add.From) {
			if e.LocID == add.LocID && e.LocX == add.LocX && e.LocZ == add.LocZ && e.To == add.To {
				found = true
			}
		}
		if !found {
			t.Errorf("added edge %s was dropped: its stand or landing is not walkable in the pack", add.String())
		}
	}
}

// Live 2026-09-25: an account inside the sheep pen north of East Ardougne could plan no web walk at
// all, because the pen's only way out, its stile, was not in the graph. With EdgeAdditions it walks
// out over the stile to the bank and to the Farmer rung's yard; the baked graph alone (the control)
// still cannot.
func TestTheArdougneSheepPenHasAWayOut(t *testing.T) {
	p := realPack(t)
	pen := []Point{{X: 2659, Z: 3362}, {X: 2650, Z: 3374}} // acct57's and Acct225's tiles
	for _, from := range pen {
		for _, to := range []Point{{X: 2655, Z: 3283}, {X: 2643, Z: 3370}} { // the bank; the rung's anchor
			r := p.Plan(from, to, Options{})
			if !r.OK {
				t.Errorf("%s -> %s: no route (%s)", from, to, r.Reason)
				continue
			}
			if hopWith(&r, 993, "Climb-over") == nil {
				t.Errorf("%s -> %s: routed without the stile, hops %v", from, to, r.Hops())
			}
		}
	}
	baked, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	baked.ApplyCorrections(EdgeCorrections)
	if r := NewPlanner(p.col, baked).Plan(pen[0], Point{X: 2655, Z: 3283}, Options{}); r.OK {
		t.Errorf("control: the baked graph already walks out of the pen (%d hops), so the addition is not what fixes it", len(r.Hops()))
	}
	// And nothing on the outside detours through the pen to get to the yard.
	if r := p.Plan(Point{X: 2655, Z: 3283}, Point{X: 2643, Z: 3370}, Options{}); !r.OK || hopWith(&r, 993, "Climb-over") != nil {
		t.Errorf("bank -> yard: ok=%v hops %v, want a route that never climbs the pen stile", r.OK, r.Hops())
	}
}
