package webwalk

import (
	"testing"

	"rsb0t-go/internal/randomevents"
)

// TestStrandEdgesAgainstTheRealPack plans out of each place the server drops cohorts into, on the real 289
// collision pack and the Default graph (the TS graph plus ExtraEdges). Before ExtraEdges every one of
// these was "unreachable" after 141 (castle) or 430 (lair) expansions, which is what the planner said to
// the eight accounts that spent twelve hours on Rogues' Castle's roof and to the 224 that died in the
// dragon's lair. The castle is the exception now: its way out is the Guardian's plan, not a graph route.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestStrandEdgesAgainstTheRealPack(t *testing.T) {
	p := realPack(t)

	// Every extra edge survived the planner's own "both ends walkable" filter.
	for _, x := range ExtraEdges {
		found := false
		for _, e := range p.EdgesFrom(x.Edge.From) {
			found = found || (e.LocID == x.Edge.LocID && e.To == x.Edge.To && e.Action == x.Edge.Action)
		}
		if !found {
			t.Errorf("extra edge %s was dropped by the planner: an end of it is not walkable in the pack", x.Edge.String())
		}
	}

	keep := Point{X: 3158, Z: 3950, Level: 0}
	// The castle: the graph has none of its doors or staircases, so from every landing tile the planner finds no
	// route at all, after settling the few tiles of the room it is in. (The Walker does not even ask: a walk from
	// these tiles is refused, strand_test.go. The Guardian plans the way out, internal/doorescape.)
	for _, from := range []Point{
		{X: 3282, Z: 3936, Level: 3}, {X: 3282, Z: 3937, Level: 3}, {X: 3281, Z: 3937, Level: 3}, {X: 3281, Z: 3936, Level: 3},
		{X: 3282, Z: 3936, Level: 2}, {X: 3279, Z: 3936, Level: 2},
		{X: 3282, Z: 3936, Level: 1}, {X: 3279, Z: 3936, Level: 1},
		{X: 3282, Z: 3936, Level: 0}, {X: 3279, Z: 3936, Level: 0},
	} {
		if _, planned := randomevents.StrandPlanned(from.X, from.Z, from.Level); !planned {
			t.Errorf("%s is not a tile the Guardian plans", from)
		}
		r := p.Plan(from, keep, Options{})
		if r.OK {
			t.Errorf("a route from the castle tile %s to the Keep, though the castle is not in the graph: %s", from, r.String())
			continue
		}
		if r.Expanded > 500 {
			t.Errorf("the plan from %s settled %d nodes before it gave up, want a few: something outside the castle is reachable from it", from, r.Expanded)
		}
	}

	// The lair: lever, ladder, then the surface.
	lair := Point{X: 2718, Z: 9816, Level: 0}
	for _, to := range []Point{{X: 3016, Z: 3852, Level: 0}, {X: 2655, Z: 3283, Level: 0}} {
		r := p.Plan(lair, to, Options{})
		if !r.OK {
			t.Errorf("no route from the lair arrival to %s: %s", to, r.Reason)
			continue
		}
		hops := r.Hops()
		if len(hops) < 2 || hops[0].LocID != 1817 || hops[1].LocID != 1766 {
			t.Errorf("from the lair to %s the route starts %s, want the lever 1817 then the ladder 1766", to, r.String())
		}
	}

	// From the open ground around the castle, and from its courtyards, no route to the Keep or to Lumbridge uses a
	// castle door or staircase or stands on a tile the Guardian plans: the castle is not in the graph and its rooms
	// are walled.
	for _, from := range []Point{
		{X: 3279, Z: 3945, Level: 0}, {X: 3299, Z: 3930, Level: 0}, {X: 3270, Z: 3930, Level: 0}, {X: 3285, Z: 3922, Level: 0},
		{X: 3276, Z: 3937, Level: 0}, {X: 3294, Z: 3936, Level: 0},
	} {
		for _, to := range []Point{keep, {X: 3222, Z: 3218, Level: 0}} {
			r := p.Plan(from, to, Options{})
			if !r.OK {
				t.Errorf("no route from %s to %s: %s", from, to, r.Reason)
				continue
			}
			for _, h := range r.Hops() {
				if edgeInCastle(*h) {
					t.Errorf("the route from %s to %s uses the castle edge %s", from, to, h.String())
				}
			}
			for _, w := range r.Waypoints {
				if site, planned := randomevents.StrandPlanned(w.X, w.Z, w.Level); planned {
					t.Errorf("the route from %s to %s passes %s, a tile of %s", from, to, w.Point, site)
				}
			}
		}
	}

	// The Mage Bank: home by the Keep lever, not a thousand tiles of overland route. (Without the zones:
	// the Walker drops them for a pocket start, tested with a fake world in strand_test.go.)
	bank := Point{X: 2543, Z: 4713, Level: 0}
	r := p.Plan(bank, Point{X: 2655, Z: 3283, Level: 0}, Options{})
	if !r.OK {
		t.Fatalf("no route from the Mage Bank home: %s", r.Reason)
	}
	usesLever := false
	for _, h := range r.Hops() {
		usesLever = usesLever || h.LocID == 1815
	}
	if !usesLever || r.Cost > 900 {
		t.Errorf("Mage Bank to the Ardougne bank: cost %d, Keep lever used %v; want the lever and under 900 (it was 1180 by overland route)\n%s", r.Cost, usesLever, r.String())
	}
}

// TestRoguesCastleGroundLeavesThroughTheGate plans from the three tiles where accounts sat STRANDED on
// 6 Oct 2026 (craft south of the castle, Acct194 north of it, acct113 by its gate) to Draynor and to
// Edgeville. Every route opens the Gate (loc 1596) at 3225,3904, the way out of the fence the castle
// stands in: freshstartrunner's STRANDED rescue and Stage 3's walk south both hand the walk to the
// Walker for it, since the raw pack and the scripts' door searches (15 and 48 tiles) never reach it.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestRoguesCastleGroundLeavesThroughTheGate(t *testing.T) {
	p := realPack(t)
	gate := Point{X: 3225, Z: 3904, Level: 0}
	for _, from := range []Point{{X: 3288, Z: 3923, Level: 0}, {X: 3278, Z: 3940, Level: 0}, {X: 3277, Z: 3926, Level: 0}} {
		for _, to := range []Point{{X: 3093, Z: 3243, Level: 0}, {X: 3094, Z: 3491, Level: 0}} {
			r := p.Plan(from, to, Options{})
			if !r.OK {
				t.Errorf("no route from %s to %s: %s", from, to, r.Reason)
				continue
			}
			opens := false
			for _, h := range r.Hops() {
				opens = opens || (h.LocID == 1596 && h.From == gate)
			}
			if !opens {
				t.Errorf("the route from %s to %s does not open the Gate at %s:\n%s", from, to, gate, r.String())
			}
		}
	}
}
