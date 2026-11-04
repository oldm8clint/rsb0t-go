package webwalk

import (
	"testing"

	"rsb0t-go/internal/randomevents"
)

// scarabAvoids is what a ScarabSwarm Stage 3 walk keeps out of: the White Wolf Mountain dens
// (scarabrunner.wolfRects, the same content spawn list wolfdens.go makes automatic) and the
// Brown bears east of the Ardougne cow field (scarabrunner.ArdougneBears).
func scarabAvoids() []Rect {
	squares := randomevents.WolfTerritorySquares(wolfDenMargin)
	out := make([]Rect, 0, len(squares)+1)
	for _, s := range squares {
		out = append(out, Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	return append(out, Rect{MinX: 2685, MaxX: 2724, MinZ: 3324, MaxZ: 3350, Level: 0})
}

// TestArdougneToVarrockTakesTheShipsAndNotTheWolves is the owner's own route, in one check:
// "they should take the boat to brimhaven walk to musa point take that boat". A Stage 3 account
// in East Ardougne is combat 3, White Wolf Mountain is an automatic danger zone for every walk
// (a combat-3 Scarab account died on the pass on the way to Zaff), and the ships are therefore
// the only way east. This pins that the plan exists, uses those ships, and enters no wolf den.
//
// It also pins the fare: every one of those edges requires 30 Coins, and the planner drops an
// edge whose requires the account cannot meet, so an account that reaches the pier with 29
// coins is told the whole of Misthalin is unreachable rather than that it is short of a fare.
func TestArdougneToVarrockTakesTheShipsAndNotTheWolves(t *testing.T) {
	p := realPack(t)
	from := Point{X: 2653, Z: 3282} // where the stranded Stage 3 accounts actually stood
	dests := map[string]Point{
		"Aubury's rune shop": {X: 3253, Z: 3402},
		"Zaff's staff shop":  {X: 3203, Z: 3433},
		"Lumbridge":          {X: 3222, Z: 3218},
	}
	avoid := scarabAvoids()
	rich := func(int) int { return 2000 } // what the mule hands a Stage 3 account

	for name, dest := range dests {
		zones := ResolveDangerZones(nil, avoid, ZoneContext{
			IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &from, Dest: &dest,
		})
		opt := Options{AllowNPC: true, AllowSpecial: true, AvoidZones: zones,
			State: &WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
				ItemCount: func(n string) int { return rich(0) }, WornCount: func(string) int { return 0 }}}
		r := p.Plan(from, dest, opt)
		if !r.OK {
			t.Errorf("%s: no route from East Ardougne (%s): %s", name, from, r.Reason)
			continue
		}
		if h := hopWith(&r, -1, "Pay-fare"); h == nil {
			t.Errorf("%s: the route pays no ship fare, so it is not the boat chain users asked for: %v", name, r.Hops())
		}
		for _, w := range r.Waypoints {
			if InZones(w.Point, avoid) {
				t.Errorf("%s: the route steps on %s, inside the White Wolf Mountain dens or the Ardougne bears", name, w.Point)
				break
			}
		}
	}
}

// TestTheShipChainIsFourPlannableLegs names each leg the owner described, so a break in one of
// them says which one rather than only "Varrock is unreachable". Brimhaven to Musa Point is a
// WALK through the Karamja gates (loc 1596/1597 at 2816,3182-3183), not a ship, which is the
// answer to "is Brimhaven<->Musa Point an edge in the graph?" — it does not need to be.
func TestTheShipChainIsFourPlannableLegs(t *testing.T) {
	p := realPack(t)
	legs := []struct {
		name     string
		from, to Point
		fare     bool
	}{
		{"East Ardougne to Captain Barnaby's pier", Point{X: 2653, Z: 3282}, Point{X: 2683, Z: 3272}, false},
		{"Ardougne to Brimhaven by ship", Point{X: 2683, Z: 3272}, Point{X: 2773, Z: 3234}, true},
		{"Brimhaven to Musa Point on foot", Point{X: 2773, Z: 3234}, Point{X: 2956, Z: 3146}, false},
		{"Musa Point to Port Sarim by ship", Point{X: 2956, Z: 3146}, Point{X: 3027, Z: 3217}, true},
		{"Port Sarim to Varrock overland", Point{X: 3027, Z: 3217}, Point{X: 3253, Z: 3402}, false},
	}
	for _, leg := range legs {
		zones := ResolveDangerZones(nil, scarabAvoids(), ZoneContext{
			IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &leg.from, Dest: &leg.to,
		})
		opt := Options{AllowNPC: true, AllowSpecial: true, AvoidZones: zones,
			State: &WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
				ItemCount: func(string) int { return 2000 }, WornCount: func(string) int { return 0 }}}
		r := p.Plan(leg.from, leg.to, opt)
		if !r.OK {
			t.Errorf("%s: %s", leg.name, r.Reason)
			continue
		}
		if got := hopWith(&r, -1, "Pay-fare") != nil; got != leg.fare {
			t.Errorf("%s: pays a fare = %v, want %v (hops %v)", leg.name, got, leg.fare, r.Hops())
		}
	}
}

// TestAShipFareIsRefusedWithoutTheCoins is the control for the two tests above: they hand the
// planner 2000 coins, so they would pass on a graph that had no fare requirement at all. An
// account with 29 coins must NOT be routed onto a 30-coin ship.
func TestAShipFareIsRefusedWithoutTheCoins(t *testing.T) {
	p := realPack(t)
	from, dest := Point{X: 2683, Z: 3272}, Point{X: 2773, Z: 3234}
	for _, coins := range []int{0, 29} {
		opt := Options{AllowNPC: true, AllowSpecial: true,
			State: &WorldState{Members: true, Skills: map[string]int{}, FreeSlots: 20,
				ItemCount: func(string) int { return coins }, WornCount: func(string) int { return 0 }}}
		if r := p.Plan(from, dest, opt); r.OK {
			t.Errorf("with %d coins the planner still boarded the 30-coin Brimhaven ship, so the fare gate is not being checked and the tests above prove less than they claim", coins)
		}
	}
}
