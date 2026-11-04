package webwalk

import "testing"

// The Fremennik Trials are held for Agility 35 (questplan.LadderFacts' module gate): the places they need inside Rellekka
// are reached over the basalt rocks, whose northbound dice rock keeps the graph's Agility 35, and below it every walk there
// ends "unreachable" (17 parks on 14 Oct 2026, "5 walks in this run to 2738,3638,0 ended unreachable").
func TestTheTrialsPlacesInRellekkaNeedAgility35FromTheMainland(t *testing.T) {
	p := realPack(t)
	places := map[string]Point{
		"the longhall's drain pipe (Manni)": {X: 2663, Z: 3674},
		"the swaying tree (Olaf)":           {X: 2738, Z: 3638},
		"the Fossegrimen altar (Olaf)":      {X: 2626, Z: 3598},
	}
	for name, to := range places {
		for _, from := range []Point{seersBank, ardougneBank} {
			if r := basaltPlan(p, from, to, 34); r.OK {
				t.Errorf("%s: a route from %s at Agility 34 (cost %d): the account would be sent over the rocks and left on the dice", name, from, r.Cost)
			}
			r := basaltPlan(p, from, to, 35)
			if !r.OK {
				t.Errorf("%s: no route from %s at Agility 35 (%s)", name, from, r.Reason)
				continue
			}
			if got := rockIDs(r); !equalInts(got, northboundRock) {
				t.Errorf("%s: %s -> jumps %v, want the five northbound rocks %v", name, from, got, northboundRock)
			}
		}
	}
}
