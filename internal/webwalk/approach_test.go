package webwalk

import "testing"

// Every override must still pick a real edge. An override whose loc id, placement or action
// drifts out of the baked graph stops applying silently, and the stand quietly goes back to
// the one live testing already disproved.
func TestApproachOverridesAllMatchAnEdge(t *testing.T) {
	fresh, err := ParseGraph(defaultGraphJSON)
	if err != nil {
		t.Fatal(err)
	}
	fresh.ApplyCorrections(EdgeCorrections)
	hits := fresh.ApplyOverrides(ApproachOverrides)
	for i, n := range hits {
		if n == 0 {
			o := ApproachOverrides[i]
			t.Errorf("override %d (loc %d at %d,%d level %d %q) matched no edge", i, o.LocID, o.LocX, o.LocZ, o.Level, o.Action)
		}
	}
}

// The Asgarnian ice dungeon ladder is the one entrance of the three that the baked graph got
// wrong: loc 1759 is forceapproach=13 at placement angle 0, so east is the only side the
// engine accepts an op from, and the baked stand was the tile north of it. A live run climbed
// down from 3009,3150 after seven "I can't reach that!" refusals from 3008,3151.
func TestIceDungeonLadderStandsEastOfTheLadder(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	want := Point{X: 3009, Z: 3150, Level: 0}
	found := 0
	for _, e := range g.Edges {
		if e.LocID != 1759 || e.LocX != 3008 || e.LocZ != 3150 || e.Action != "Climb-Down" {
			continue
		}
		found++
		if e.From != want {
			t.Errorf("ice dungeon ladder stands at %s, want the east tile %s", e.From, want)
		}
		if e.To != (Point{X: 3008, Z: 9551, Level: 0}) {
			t.Errorf("ice dungeon ladder lands at %s, want 3008,9551,0", e.To)
		}
		if e.Override == "" {
			t.Error("the ice dungeon ladder edge carries no override evidence")
		}
	}
	if found == 0 {
		t.Fatal("no Climb-Down edge for loc 1759 at 3008,3150 in the graph")
	}
}

// The other two entrances are shut fixtures that must be opened before they can be used, so
// each edge has to carry the open-state id the walker switches to after the Open op.
func TestDungeonEntrancesCarryTheirOpenStateIDs(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name               string
		locID, x, z        int
		action             string
		wantOpen           int
		wantTo             Point
		wantLandsOnTile    bool
		shouldBeInTheGraph bool
	}{
		{"Edgeville trapdoor", 1568, 3097, 3468, "Climb-down", 1570, Point{X: 3096, Z: 9868}, true, true},
		{"Varrock manhole", 881, 3237, 3458, "Climb down", 882, Point{X: 3237, Z: 9859}, true, true},
	}
	for _, c := range cases {
		found := false
		for _, e := range g.Edges {
			if e.LocID != c.locID || e.LocX != c.x || e.LocZ != c.z || e.Action != c.action {
				continue
			}
			found = true
			if e.OpenLocID != c.wantOpen {
				t.Errorf("%s: OpenLocID = %d, want %d (the closed loc offers only Open)", c.name, e.OpenLocID, c.wantOpen)
			}
			if e.To != c.wantTo {
				t.Errorf("%s: lands at %s, want %s", c.name, e.To, c.wantTo)
			}
			if e.LandsOnTile != c.wantLandsOnTile {
				t.Errorf("%s: LandsOnTile = %v, want %v", c.name, e.LandsOnTile, c.wantLandsOnTile)
			}
		}
		if found != c.shouldBeInTheGraph {
			t.Errorf("%s: present in the graph = %v, want %v", c.name, found, c.shouldBeInTheGraph)
		}
	}
}
