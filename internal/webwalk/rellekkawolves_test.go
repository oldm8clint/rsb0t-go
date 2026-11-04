package webwalk

import "testing"

// rellekkaWolves is content's spawns and hunt profile for npc 95 and 1330 (see
// rellekkawolves.go for the sources), in seershunters_test.go's hunterSpawn form so the same
// reach rule is checked.
func rellekkaWolves() []hunterSpawn {
	var out []hunterSpawn
	for _, s := range rellekkaWolfSpawns {
		out = append(out, hunterSpawn{"wolf", s[0], s[1], 64, 6, 8, 2, 2, true})
	}
	for _, s := range vikingWolfSpawns {
		out = append(out, hunterSpawn{"viking_wolf", s[0], s[1], 64, 6, 8, 1, 2, true})
	}
	return out
}

// TestRellekkaWolvesAreAvoidedAtLowCombat: every tile either wolf can notice a player from is
// in a zone at combat 64 and below, and none of them is at 65, the owner's "low combat" line.
func TestRellekkaWolvesAreAvoidedAtLowCombat(t *testing.T) {
	far := Point{X: 3222, Z: 3218}
	low := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 64, CombatKnown: true, Start: &far, Dest: &far})
	high := ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 65, CombatKnown: true, Start: &far, Dest: &far})
	for _, h := range rellekkaWolves() {
		r := h.reach()
		for x := h.x - r; x <= h.x+r; x++ {
			for z := h.z - r; z <= h.z+r; z++ {
				if !InZones(Point{X: x, Z: z}, low) {
					t.Fatalf("%s at %d,%d: %d,%d is within its reach %d but no combat-64 zone covers it", h.name, h.x, h.z, x, z, r)
				}
			}
		}
	}
	for _, id := range []string{"rellekka-road-wolves", "viking-wolves"} {
		z, ok := KnownDangerZoneByID(id)
		if !ok || !z.Automatic || !z.AllowWhenEndpointInside || z.AvoidAtOrBelowCombat != 64 || z.FromTS {
			t.Fatalf("%s = %+v", id, z)
		}
		for _, r := range z.Rects {
			if InZones(Point{X: r.MinX, Z: r.MinZ}, high) && !InZones(Point{X: r.MinX, Z: r.MinZ}, zonesWithout(id, far, far, 65)) {
				t.Fatalf("%s still applies at combat 65", id)
			}
		}
	}
	// A walk starting beside a wolf may leave: the endpoint exemption.
	spawn := Point{X: 2680, Z: 3585}
	if InZones(spawn, ResolveDangerZones(nil, nil, ZoneContext{IncludeAutomatic: true, CombatLevel: 3, CombatKnown: true, Start: &spawn, Dest: &far})) {
		t.Error("a walk out of the wolves' zone still avoids it")
	}
}

// TestRellekkaWolvesLeaveTheSeersFleetAlone: no tile a Seers or Catherby script stands on is
// inside the new zones, so no existing route loses its endpoint.
func TestRellekkaWolvesLeaveTheSeersFleetAlone(t *testing.T) {
	var rects []Rect
	for _, id := range []string{"rellekka-road-wolves", "viking-wolves"} {
		z, _ := KnownDangerZoneByID(id)
		rects = append(rects, z.Rects...)
	}
	for name, p := range fleetPlaces() {
		if InZones(pointOf(p), rects) {
			t.Errorf("%s (%d,%d) is inside a Rellekka wolf zone", name, p.X, p.Z)
		}
	}
}
