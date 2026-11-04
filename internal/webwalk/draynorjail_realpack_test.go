package webwalk

import "testing"

// The Draynor jail guards (content draynor.npc [jailguard]: level 26, huntrange 5, huntmode aggressive_melee,
// and that mode has check_nottoostrong=off in all.hunt) hunt every account that comes within five tiles, whatever
// its combat level. The catalog zone for them stopped at combat 50. On 17 Oct 2026 a combat-63 RuneCarrier on
// the Port Sarim to Draynor bank walk was killed at 3107,3234, three tiles from the guard that spawns at
// 3109,3237 (HP 55 to 0, no food, nothing in the code reacting to it).

// jailGuardSpawns are the four guards' spawn tiles (content m48_50): 3109,3237 and three more to the east.
var jailGuardSpawns = []Point{{X: 3109, Z: 3237}, {X: 3120, Z: 3238}, {X: 3121, Z: 3249}, {X: 3127, Z: 3248}}

// jailGuardHunt is a guard's huntrange; a route must stay out of it.
const jailGuardHunt = 5

func routeTilesOf(r Route) []Point {
	var out []Point
	for _, leg := range r.Legs() {
		for i := 1; i < len(leg.Tiles); i++ {
			cx, cz := leg.Tiles[i-1].X, leg.Tiles[i-1].Z
			lvl := leg.Tiles[i].Level
			for cx != leg.Tiles[i].X || cz != leg.Tiles[i].Z {
				if cx < leg.Tiles[i].X {
					cx++
				} else if cx > leg.Tiles[i].X {
					cx--
				}
				if cz < leg.Tiles[i].Z {
					cz++
				} else if cz > leg.Tiles[i].Z {
					cz--
				}
				out = append(out, Point{X: cx, Z: cz, Level: lvl})
			}
		}
	}
	return out
}

func cheb(a, b Point) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return max(dx, dz)
}

// A combat-63 account walking between Port Sarim's houses, Rimmington or Lumbridge and the Draynor bank is
// routed clear of the jail compound, and still has a route.
func TestAHighCombatAccountIsRoutedClearOfTheDraynorJailGuards(t *testing.T) {
	p := realPack(t)
	bank := Point{X: 3093, Z: 3243}
	for _, from := range []Point{{X: 3109, Z: 3166}, {X: 3104, Z: 3163}, {X: 3222, Z: 3218}} {
		start := from
		zones := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 63, CombatKnown: true, Start: &start, Dest: &bank})
		r := p.Plan(from, bank, Options{AvoidZones: zones})
		if !r.OK {
			t.Errorf("from %s: no route to the Draynor bank for combat 63 with the jail avoided: %s", from, r.Reason)
			continue
		}
		worst := 1 << 20
		for _, tile := range routeTilesOf(r) {
			for _, g := range jailGuardSpawns {
				if tile.Level == 0 {
					worst = min(worst, cheb(tile, g))
				}
			}
		}
		t.Logf("from %s: cost %d, nearest the route comes to a guard's spawn: %d", from, r.Cost, worst)
		if worst <= jailGuardHunt {
			t.Errorf("from %s: the route passes %d tiles from a Jail guard's spawn, inside its hunt range of %d", from, worst, jailGuardHunt)
		}
	}
	// Control, the rule as it was: with the zone dropped above combat 50 (the old AvoidAtOrBelowCombat) the same walk
	// goes by the jail, which is how the live account came to be at 3107,3234.
	from := Point{X: 3109, Z: 3166}
	old := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 63, CombatKnown: true, Start: &from, Dest: &bank})
	var withoutJail []Rect
	jail, _ := KnownDangerZoneByID("draynor-jail-guards")
	for _, z := range old {
		inJail := false
		for _, j := range jail.Rects {
			if z == j {
				inJail = true
			}
		}
		if !inJail {
			withoutJail = append(withoutJail, z)
		}
	}
	r := p.Plan(from, bank, Options{AvoidZones: withoutJail})
	if r.OK {
		near := 1 << 20
		for _, tile := range routeTilesOf(r) {
			for _, g := range jailGuardSpawns {
				near = min(near, cheb(tile, g))
			}
		}
		t.Logf("control, jail zone dropped: route passes %d tiles from a guard", near)
	}
}

// The jail zone still lets a walk that starts or ends inside it (the quest at the jail, the cell) plan.
func TestADestinationInsideTheDraynorJailStillPlans(t *testing.T) {
	p := realPack(t)
	from, inside := Point{X: 3093, Z: 3243}, Point{X: 3110, Z: 3240}
	zones := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 63, CombatKnown: true, Start: &from, Dest: &inside})
	if r := p.Plan(from, inside, Options{AvoidZones: zones}); !r.OK {
		t.Fatalf("no route into the jail compound: %s", r.Reason)
	}
}
