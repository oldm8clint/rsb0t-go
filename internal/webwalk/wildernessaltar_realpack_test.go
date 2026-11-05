package webwalk

import "testing"

// Merlin's Crystal sends every account to the Wilderness chaos altar at 3239,3607. The altar's hut is entered from the north: the
// planned route reaches z 3634 (27 rows past the altar) before it turns back south. The Wilderness zone's first margin of 16 rows
// left that entrance fenced off, so 138 walks in one run ended "unreachable" (5 Nov 2026). With the zones a walk really resolves,
// the altar has a route from Edgeville and from inside the Wilderness, and the deep land (the Ardougne lever's landing) stays avoided.
func TestChaosAltarIsReachableWithTheAutomaticZones(t *testing.T) {
	p := realPack(t)
	alt := Point{X: 3239, Z: 3607}
	for _, from := range []Point{{X: 3094, Z: 3489}, {X: 3200, Z: 3500}, {X: 3239, Z: 3590}} {
		start := from
		zones := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 65, CombatKnown: true, Start: &start, Dest: &alt})
		r := p.Plan(from, alt, Options{AvoidZones: zones})
		if !r.OK {
			t.Errorf("from %s: no route to the chaos altar: %s", from, r.Reason)
		}
	}
}
