package freshstartrunner

import (
	"fmt"
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/webwalk"
)

// Guard spawn tiles from content maps/m48_50.jm2 (npc 917, jailguard).
var jailGuards = []pathfind.NavPoint{{X: 3109, Z: 3237}, {X: 3120, Z: 3238}, {X: 3121, Z: 3249}, {X: 3127, Z: 3248}}

// The Highwayman's spawn (content maps/m48_51.jm2, npc 180) and how far he reaches from it:
// wanderrange 8 plus huntrange 5.
var highwayman, highwaymanReach = pathfind.NavPoint{X: 3110, Z: 3295}, 8 + 5

func TestZoneAvoidKeepsLowCombatOutOfTheDraynorJail(t *testing.T) {
	from, to := pathfind.NavPoint{X: 3219, Z: 3254}, pathfind.NavPoint{X: 3093, Z: 3243}
	avoid := zoneAvoid(nil, from, to) // no session: combat unknown, the zone applies
	for _, g := range jailGuards {
		if !avoid(g.X, g.Z, 0) {
			t.Errorf("guard spawn %v is not avoided at unknown combat", g)
		}
	}
	if avoid(3150, 3264, 0) || avoid(3093, 3243, 0) {
		t.Error("the road north of the compound and the destination must stay open")
	}
	// A walk that starts or ends inside the compound may still leave or reach it.
	inside := zoneAvoid(nil, pathfind.NavPoint{X: 3120, Z: 3245}, to)
	if inside(3121, 3249, 0) {
		t.Error("a walk starting inside the compound was refused its own way out")
	}
}

func TestLowCombatZonesAreCatalogZones(t *testing.T) {
	for _, id := range lowCombatZoneIDs {
		if _, ok := webwalk.KnownDangerZoneByID(id); !ok {
			t.Errorf("%q is not in webwalk.KnownDangerZones", id)
		}
	}
}

// With the real pack, the Lumbridge -> Draynor walk herring died on still has a route, and it
// passes no guard within its hunt range.
func TestJailAvoidingRouteExistsOnTheRealPack(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, walk := range [][2]pathfind.NavPoint{
		{{X: 3219, Z: 3254}, {X: 3093, Z: 3243}},
		{{X: 3142, Z: 3259}, {X: 3093, Z: 3243}},
		{{X: 3222, Z: 3218}, {X: 3093, Z: 3243}},
		{{X: 3222, Z: 3218}, {X: 3029, Z: 3217}}, // Lumbridge respawn to the Port Sarim ship
	} {
		from, to := walk[0], walk[1]
		res := pk.FindPathAvoiding(from, to, zoneAvoid(nil, from, to))
		if !res.OK {
			t.Fatalf("%v -> %v: no route around the jail (%s)", from, to, res.Reason)
		}
		prev := from
		for _, c := range res.Waypoints {
			for prev != c {
				prev.X += sign(c.X - prev.X)
				prev.Z += sign(c.Z - prev.Z)
				for _, g := range jailGuards {
					if tileDist(prev, g) <= 5 {
						t.Fatalf("%v -> %v passes %v, within a guard's 5-tile hunt range of %v", from, to, prev, g)
					}
				}
				if tileDist(prev, highwayman) <= highwaymanReach {
					t.Fatalf("%v -> %v passes %v, within the Highwayman's reach", from, to, prev)
				}
			}
		}
	}
}

func tileDist(a, b pathfind.NavPoint) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// knownCombat is the zone context walkNearCore builds for a session whose combat level is known.
func knownCombat(combat int, from, to pathfind.NavPoint) webwalk.ZoneContext {
	return webwalk.ZoneContext{
		CombatLevel: combat, CombatKnown: true,
		Start: &webwalk.Point{X: from.X, Z: from.Z, Level: from.Level},
		Dest:  &webwalk.Point{X: to.X, Z: to.Z, Level: to.Level},
	}
}

// Two endpoints outside every zone, so none is exempted: the Catherby and Seers banks.
var catherbyBank, seersBank = pathfind.NavPoint{X: 2809, Z: 3441}, pathfind.NavPoint{X: 2725, Z: 3493}

// everyTileInReach calls f for every tile within h.reach of h.at.
func everyTileInReach(h hunter, f func(x, z int)) {
	for x := h.at.X - h.reach; x <= h.at.X+h.reach; x++ {
		for z := h.at.Z - h.reach; z <= h.at.Z+h.reach; z++ {
			f(x, z)
		}
	}
}

// The owner's ruling, 2026-09-23: avoid White Wolf Mountain always, especially for low levels.
// So the wolves' reach is off this router's routes at every combat level, known or not, which
// is what webwalk's walks have had since 2026-09-19 (wcrg13's 7-tile evergreen walk was sent
// past the sentries at 2842,3450 and 2847,3474 because this router alone did not have it).
func TestZoneAvoidKeepsEveryLevelOffTheWolfDens(t *testing.T) {
	avoids := map[string]func(x, z, level int) bool{"unknown combat": zoneAvoid(nil, catherbyBank, seersBank)}
	for _, c := range []int{3, 10, 11, 126} {
		avoids[fmt.Sprintf("combat %d", c)] = zoneAvoidWith(lowCombatZoneIDs, knownCombat(c, catherbyBank, seersBank))
	}
	for label, avoid := range avoids {
		for _, h := range fleetHunters {
			if h.maxCombat > 0 {
				continue
			}
			everyTileInReach(h, func(x, z int) {
				if !avoid(x, z, 0) {
					t.Fatalf("%s: %d,%d is within %d of the %s at %d,%d and not avoided", label, x, z, h.reach, h.name, h.at.X, h.at.Z)
				}
			})
		}
	}
}

// The two Highwaymen north of Seers hunt combat 10 and below (2 x their level 5): their reach is
// off the route at those levels and at an unknown level (fail safe), and back on it at 11.
func TestZoneAvoidKeepsLowCombatOffTheSeersHighwaymen(t *testing.T) {
	for _, c := range []struct {
		label string
		avoid func(x, z, level int) bool
		want  bool
	}{
		{"unknown combat", zoneAvoid(nil, seersBank, catherbyBank), true},
		{"combat 3", zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, seersBank, catherbyBank)), true},
		{"combat 10", zoneAvoidWith(lowCombatZoneIDs, knownCombat(10, seersBank, catherbyBank)), true},
		{"combat 11", zoneAvoidWith(lowCombatZoneIDs, knownCombat(11, seersBank, catherbyBank)), false},
	} {
		for _, h := range fleetHunters {
			if h.name != "highwayman" {
				continue
			}
			if c.avoid(h.at.X, h.at.Z, 0) != c.want {
				t.Errorf("%s: the Highwayman's spawn %d,%d avoided = %v, want %v", c.label, h.at.X, h.at.Z, !c.want, c.want)
			}
			if !c.want {
				continue
			}
			everyTileInReach(h, func(x, z int) {
				if !c.avoid(x, z, 0) {
					t.Fatalf("%s: %d,%d is within the Highwayman at %d,%d's reach and not avoided", c.label, x, z, h.at.X, h.at.Z)
				}
			})
		}
	}
}

// A walk that starts or ends inside a zone may still leave or reach it: an account a flee or a
// random event left among the wolves must be able to walk out, and a walk to the river spot
// between the Highwaymen is the caller's own choice.
func TestZoneAvoidLetsAWalkLeaveOrReachTheNewZones(t *testing.T) {
	sentry, riverSpot := pathfind.NavPoint{X: 2842, Z: 3450}, pathfind.NavPoint{X: 2727, Z: 3524}
	if zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, sentry, catherbyBank))(2842, 3451, 0) {
		t.Error("a walk starting beside a White wolf sentry was refused its way out")
	}
	if zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, catherbyBank, sentry))(2842, 3451, 0) {
		t.Error("a walk ending inside the wolf dens was refused its destination")
	}
	if zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, seersBank, riverSpot))(2735, 3521, 0) {
		t.Error("a walk to a tile inside the Seers Highwaymen zone was refused it")
	}
	if zoneAvoidWith(lowCombatZoneIDs, knownCombat(3, riverSpot, seersBank))(2735, 3521, 0) {
		t.Error("a walk out of the Seers Highwaymen zone was refused its way out")
	}
}

// Every tile the Catherby fishers and the Seers woodcutters stand on is outside both new zones at
// combat 3, so neither can exempt itself away (an endpoint inside a zone drops that whole zone)
// or refuse a leg its own destination. The nearest is Harry, two tiles west of the 2842,3450
// sentry's square; widening that square by two would put him inside.
func TestTheFleetsOwnTilesAreOutsideTheNewZones(t *testing.T) {
	c := fisherplan.Catherby()
	tiles := map[string]pathfind.NavPoint{
		"Catherby bank": np(c.Bank), "Catherby camp": np(c.Camp), "Harry": np(c.HarryStand),
		"Arhein": np(c.ArheinStand), "Catherby range": np(mulereceiverplan.CatherbyRange),
		"Catherby camp east end": {X: 2865, Z: 3428},
	}
	for _, s := range catherbySpots {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				tiles[fmt.Sprintf("beside Catherby spot %d,%d", s.X, s.Z)] = pathfind.NavPoint{X: s.X + dx, Z: s.Z + dz}
			}
		}
	}
	for name, p := range seersPlaces() {
		if name == "fail-teleport Camelot topiary" {
			continue // a fail-teleport lands where it lands; only walking home from it matters
		}
		tiles[name] = np(p)
	}
	for _, ids := range [][]string{{"white-wolf-dens"}, {"seers-north-highwaymen"}} {
		rects := webwalk.ResolveDangerZones(ids, nil, webwalk.ZoneContext{CombatLevel: 3, CombatKnown: true})
		if len(rects) == 0 {
			t.Fatalf("%v resolved to nothing at combat 3", ids)
		}
		for name, p := range tiles {
			if webwalk.InZones(webwalk.Point{X: p.X, Z: p.Z, Level: p.Level}, rects) {
				t.Errorf("%s (%d,%d) is inside %v", name, p.X, p.Z, ids)
			}
		}
	}
}

// "seers-south-bats" stays out of this router's list on purpose: it is the giant-bat zone the
// dangerZones comment in runner.go records as cutting Catherby off, and switching this router to
// every automatic zone (webwalk's IncludeAutomatic) would bring it in.
func TestLowCombatZonesLeaveOutTheSeersSouthBats(t *testing.T) {
	for _, id := range lowCombatZoneIDs {
		if id == "seers-south-bats" {
			t.Fatal("seers-south-bats is in lowCombatZoneIDs; see the dangerZones comment for the Catherby cut-off")
		}
	}
}

// zoneBlocked tells a walk the zones alone refuse (the pack has a route, through a zone) from one
// nothing could route (a sealed pocket), so walkNearCore can hand the first back to its caller
// instead of running door and STRANDED recovery on it, and keep that recovery for the second.
func TestZoneBlockedTellsAZoneFromASealedPocket(t *testing.T) {
	// A corridor 10..14,10 whose middle tile is "in a zone", and a sealed two-tile pocket at 20..21,20.
	pk := buildConnectedDoorPack(t,
		[][3]int{{10, 10, 0}, {11, 10, 0}, {12, 10, 0}, {13, 10, 0}, {14, 10, 0}, {20, 20, 0}, {21, 20, 0}},
		[][2][3]int{
			{{10, 10, 0}, {11, 10, 0}}, {{11, 10, 0}, {12, 10, 0}}, {{12, 10, 0}, {13, 10, 0}}, {{13, 10, 0}, {14, 10, 0}},
			{{20, 20, 0}, {21, 20, 0}},
		})
	zone := func(x, z, level int) bool { return x == 12 && z == 10 }
	from, to := pathfind.NavPoint{X: 10, Z: 10}, pathfind.NavPoint{X: 14, Z: 10}

	res := pk.FindPathAvoiding(from, to, zone)
	if res.OK {
		t.Fatal("the zone did not cut the corridor; the fixture is wrong")
	}
	if !zoneBlocked(pk, from, to, res) {
		t.Error("a corridor cut only by a zone was not reported zone-blocked")
	}
	if ok := pk.FindPathAvoiding(from, to, nil); !ok.OK || zoneBlocked(pk, from, to, ok) {
		t.Error("a walk that planned is zone-blocked")
	}
	pocket := pathfind.NavPoint{X: 20, Z: 20}
	if sealed := pk.FindPathAvoiding(pocket, to, zone); sealed.OK || zoneBlocked(pk, pocket, to, sealed) {
		t.Error("a sealed pocket was reported zone-blocked, which would skip the recovery it needs")
	}
	upstairs := pathfind.NavPoint{X: 14, Z: 10, Level: 1}
	if zoneBlocked(pk, from, upstairs, pathfind.PathResult{Reason: "unreachable"}) {
		t.Error("a level change was reported zone-blocked; that is tryChangeLevel's")
	}
	if zoneBlocked(pk, from, to, pathfind.PathResult{Reason: "start not walkable"}) {
		t.Error("a position desync was reported zone-blocked")
	}
	if zoneBlocked(nil, from, to, res) {
		t.Error("no pack, and still zone-blocked")
	}
}

// planAvoidingZones walks an account out of a pocket the zones wall in, and only out of one: a
// 64x40 open field (2,560 tiles, more than zonePocketTiles) joined by a one-tile corridor, whose
// middle is "in a zone", to a 3x3 pocket. From the pocket the only way anywhere crosses the
// zone, so it is taken; from the field, a walk into the pocket is still refused.
func TestPlanAvoidingZonesLeavesAPocketButKeepsTheFieldOut(t *testing.T) {
	var walk [][3]int
	var edges [][2][3]int
	add := func(x, z int) { walk = append(walk, [3]int{x, z, 0}) }
	link := func(ax, az, bx, bz int) { edges = append(edges, [2][3]int{{ax, az, 0}, {bx, bz, 0}}) }
	for x := 0; x < 64; x++ {
		for z := 0; z < 40; z++ {
			add(x, z)
			if x > 0 {
				link(x-1, z, x, z)
			}
			if z > 0 {
				link(x, z-1, x, z)
			}
		}
	}
	for z := 40; z < 50; z++ { // the corridor, x=1
		add(1, z)
		link(1, z-1, 1, z)
	}
	for x := 0; x < 3; x++ { // the pocket, 0..2,50..52
		for z := 50; z < 53; z++ {
			add(x, z)
			if x > 0 {
				link(x-1, z, x, z)
			}
			if z > 50 {
				link(x, z-1, x, z)
			}
		}
	}
	link(1, 49, 1, 50)
	pk := buildConnectedDoorPack(t, walk, edges)
	zone := func(x, z, level int) bool { return x == 1 && z >= 43 && z <= 46 }
	pocket, field := pathfind.NavPoint{X: 1, Z: 51}, pathfind.NavPoint{X: 30, Z: 20}

	out, escaped := planAvoidingZones(pk, pocket, field, zone)
	if !out.OK || !escaped {
		t.Fatalf("from the pocket: ok=%v escaped=%v (%s); the only way out crosses the zone and was refused", out.OK, escaped, out.Reason)
	}
	if plain := pk.FindPath(pocket, field); routeSteps(out) != routeSteps(plain) {
		t.Errorf("the way out is %d tiles, the plain route %d", routeSteps(out), routeSteps(plain))
	}
	in, escaped := planAvoidingZones(pk, field, pocket, zone)
	if in.OK || escaped {
		t.Fatalf("from the field into the pocket: ok=%v escaped=%v; a start with the whole field to stand in is not walled in", in.OK, escaped)
	}
	if !zoneBlocked(pk, field, pocket, in) {
		t.Error("the refused walk into the pocket is not zone-blocked, so walkNearCore would run door and STRANDED recovery on it")
	}
	if r, escaped := planAvoidingZones(pk, field, pathfind.NavPoint{X: 60, Z: 35}, zone); !r.OK || escaped {
		t.Errorf("an ordinary walk across the field: ok=%v escaped=%v", r.OK, escaped)
	}
	sealed := buildConnectedDoorPack(t, [][3]int{{5, 5, 0}, {6, 5, 0}, {20, 20, 0}}, [][2][3]int{{{5, 5, 0}, {6, 5, 0}}})
	if r, escaped := planAvoidingZones(sealed, pathfind.NavPoint{X: 5, Z: 5}, pathfind.NavPoint{X: 20, Z: 20}, zone); r.OK || escaped {
		t.Error("a pocket the plain pack cannot leave either was reported escaped; it needs walkNearCore's recovery")
	}
}
