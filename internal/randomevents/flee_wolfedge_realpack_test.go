package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/pathfind"
)

// The planner on White Wolf Mountain's edge, where the first version of flee_plan.go went wrong.
//
// Review of 15 Oct 2026 compared the pre-change search (every ring candidate routed with an A*, no
// budget) with the planner on the real map. They agreed except on three origins at the mountain edge,
// where one 80-search budget shared by the main passes, the least-exposed scan and every shorter rung
// was spent on candidates whose routes cross wolf territory, so the fallbacks never ran:
//
//	Catherby town 2830,3467, HandleEvade's second attempt: the old flee walked to 2810,3447 (exposure
//	  4); the planner found nowhere and the account stood and fought;
//	the mountain foot 2864,3445, second attempt: 2866,3420; likewise;
//	2838,3464 at distance 40: 2791,3427 (exposure 8) degraded to 2797,3504 (exposure 35).
//
// These tests hold the picks to the old search's own answers and the work to a bound, and the new
// screens to the property that makes them safe: they never refuse what the old search accepted.
// They need RSB0T_COLLISION_PACK.

// eastThreat is the threat the comparison harness used: the monster one tile east of the account.
func eastThreat(o pathfind.NavPoint) (int, int) { return o.X + 1, o.Z }

func TestMountainEdgeFleePicksMatchTheUnboundedSearch(t *testing.T) {
	pack := realCollisionPack(t)
	for _, c := range []struct {
		name    string
		origin  pathfind.NavPoint
		attempt int // HandleEvade's attempt (distance 20, 24, 26...), or 0 to use dist
		dist    int
		want    pathfind.NavPoint
	}{
		{"Catherby town, second attempt", pathfind.NavPoint{X: 2830, Z: 3467}, 2, 0, pathfind.NavPoint{X: 2810, Z: 3447}},
		{"mountain foot, second attempt", pathfind.NavPoint{X: 2864, Z: 3445}, 2, 0, pathfind.NavPoint{X: 2866, Z: 3420}},
		{"Catherby town at distance 40", pathfind.NavPoint{X: 2838, Z: 3464}, 0, 40, pathfind.NavPoint{X: 2791, Z: 3427}},
		{"mountain slope, first attempt", pathfind.NavPoint{X: 2861, Z: 3460}, 1, 0, pathfind.NavPoint{X: 2866, Z: 3420}},
		{"Taverley foot, first attempt", pathfind.NavPoint{X: 2875, Z: 3428}, 1, 0, pathfind.NavPoint{X: 2875, Z: 3408}},
	} {
		tx, tz := eastThreat(c.origin)
		var got pathfind.NavPoint
		var ok bool
		searches, expanded := searchesAndExpansions(func() {
			if c.attempt > 0 {
				got, ok, _ = evadeEscape(pack, c.origin, tx, tz, c.attempt, CatherbyBankRef)
			} else {
				got, ok = localEscape(pack, c.origin, tx, tz, c.dist, CatherbyBankRef)
			}
		})
		t.Logf("%s: %v %v, %d searches, %d expanded", c.name, got, ok, searches, expanded)
		if !ok || got != c.want {
			t.Errorf("%s: got %v %v, want %v (the pre-change search's own pick)", c.name, got, ok, c.want)
		}
		// The old search ran 153 to 252 route searches on each of these.
		if searches > 100 {
			t.Errorf("%s: %d route searches", c.name, searches)
		}
	}
}

// A main pass that spends its budget must leave the least-exposed fallback its own: with the main
// passes allowed two searches the second attempt at Catherby town still gets 2810,3447, which only
// the fallback finds (its route spends 4 tiles in territory, so the main passes refuse it).
func TestSpentMainPassDoesNotStarveTheLeastExposedFallback(t *testing.T) {
	pack := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2830, Z: 3467}
	tx, tz := eastThreat(origin)
	pl := newFleePlanner(pack, origin, tx, tz, nil, CatherbyBankRef)
	pl.mainBudget = 2
	plan, ok := pl.escape(evadeDistance(2))
	if !ok || plan.Dest != (pathfind.NavPoint{X: 2810, Z: 3447}) {
		t.Fatalf("with a two-search main pass the plan is %v %v, want 2810,3447 from the fallback", plan.Dest, ok)
	}
	if e := routeExposure(origin, plan.Route); e == 0 || e > 8 {
		t.Fatalf("the fallback's route has exposure %d, want a small non-zero one (it is the fallback because the main passes refuse exposure)", e)
	}
}

func TestPhasesBeginWithTheirOwnBudget(t *testing.T) {
	pl := &fleePlanner{}
	pl.beginPhase(2)
	pl.phaseRoutes = 2
	if !pl.spent() {
		t.Fatal("a phase that used its budget is not spent")
	}
	pl.beginPhase(2)
	if pl.spent() {
		t.Fatal("the next phase began already spent")
	}
	pl.beginPhase(0)
	if !pl.spent() {
		t.Fatal("a phase with no budget is not spent")
	}
}

// The territory-free flood is the exact test for "some route to this candidate crosses no wolf
// territory". Safe means it never refuses a candidate the old search would have accepted: for every
// ring candidate, if the shortest route under the local predicate lands clear and has no exposure,
// the flood reaches the candidate within the detour bound the planner screens by.
func TestDryFloodNeverRefusesWhatTheOldSearchAccepted(t *testing.T) {
	pack := realCollisionPack(t)
	accepted, refused := 0, 0
	for _, origin := range []pathfind.NavPoint{{X: 2830, Z: 3467}, {X: 2864, Z: 3445}, {X: 2838, Z: 3464}, {X: 2875, Z: 3428}, {X: 2861, Z: 3460}, {X: 2856, Z: 3468}, {X: 2809, Z: 3441}, {X: 2846, Z: 3430}} {
		if inWolfTerritory(origin) {
			t.Fatalf("%v is inside territory: the main passes never run from there", origin)
		}
		tx, tz := eastThreat(origin)
		pl := newFleePlanner(pack, origin, tx, tz, nil, CatherbyBankRef)
		if !pl.territoryInWindow() {
			t.Fatalf("%v: no wolf territory in the window", origin)
		}
		dry := pl.dryReach()
		dry.Each(func(x, z, level, steps int) bool {
			if steps > 0 && inWolfTerritory(pathfind.NavPoint{X: x, Z: z, Level: level}) {
				t.Fatalf("%v: the territory-free flood reached %d,%d, which is in territory", origin, x, z)
			}
			return true
		})
		local := pl.localReach()
		if dry.Size() > local.Size() {
			t.Fatalf("%v: the territory-free flood (%d) is bigger than the local one (%d)", origin, dry.Size(), local.Size())
		}
		for _, radius := range []int{20, 28} {
			biased, rest := pl.ring(radius, 20)
			for _, dest := range append(append([]pathfind.NavPoint(nil), biased...), rest...) {
				if inDangerZone(dest) {
					continue
				}
				route := pack.FindPathAvoiding(origin, dest, pl.localAvoid)
				takes := route.OK && len(route.Waypoints) > 0 &&
					fleeLandingOK(origin, route.Waypoints[len(route.Waypoints)-1], tx, tz, route.Waypoints) &&
					routeExposure(origin, route.Waypoints) == 0
				screened := pl.worthRouting(dry, dest)
				if takes {
					accepted++
					if !screened {
						t.Errorf("%v -> %v: the old search takes it (route %v) but the flood refuses it", origin, dest, route.Waypoints)
					}
				} else if !screened {
					refused++
				}
			}
		}
	}
	t.Logf("%d candidates the old search takes, all passed; %d it refuses were screened out without a search", accepted, refused)
	if accepted == 0 || refused == 0 {
		t.Fatalf("a vacuous comparison: %d accepted, %d screened", accepted, refused)
	}
}

// A walkable candidate in territory is refused without a search, and no flood is built for it.
func TestCandidateInsideTerritoryIsRefusedWithoutASearch(t *testing.T) {
	pack := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2830, Z: 3467}
	pl := newFleePlanner(pack, origin, origin.X+1, origin.Z, nil, CatherbyBankRef)
	var inside pathfind.NavPoint
	found := false
	for x := 2840; x <= 2848 && !found; x++ {
		for z := 3496; z <= 3506 && !found; z++ {
			if p := (pathfind.NavPoint{X: x, Z: z}); pack.Walkable(x, z, 0) && inWolfTerritory(p) {
				inside, found = p, true
			}
		}
	}
	if !found {
		t.Fatal("no walkable tile in the pack-leader's territory")
	}
	if !pl.certainlyInTerritory(inside) {
		t.Fatalf("%v is walkable and in territory", inside)
	}
	if pl.certainlyInTerritory(pathfind.NavPoint{X: 3222, Z: 3218}) {
		t.Fatal("Lumbridge is not in wolf territory")
	}
	searches, _ := searchesAndExpansions(func() {
		if v := pl.mainVerdict(inside); v.ok {
			t.Errorf("a tile in territory was taken: %v", v.plan)
		}
		if v := pl.exposedVerdict(inside); v.ok {
			t.Errorf("a tile in territory was taken by the scan: %v", v.plan)
		}
	})
	if searches != 0 || pl.routes != 0 {
		t.Fatalf("%d searches and %d counted for a tile that cannot be taken", searches, pl.routes)
	}
}

// The shorter rungs ask for most of the preferred distance's candidates again; a candidate is
// searched once per plan, so a ladder costs no more than its distinct candidates and reaches the
// same answer a rung asked alone would.
func TestLadderRungsReuseTheirCandidates(t *testing.T) {
	pack := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2830, Z: 3467}
	tx, tz := eastThreat(origin)
	fresh := newFleePlanner(pack, origin, tx, tz, nil, CatherbyBankRef)
	alone, aloneOK := fresh.escape(12)
	aloneRoutes := fresh.routes

	shared := newFleePlanner(pack, origin, tx, tz, nil, CatherbyBankRef)
	if _, ok := shared.escape(20); !ok {
		t.Fatal("control: no escape at 20")
	}
	before := shared.routes
	got, ok := shared.escape(12)
	if ok != aloneOK || got.Dest != alone.Dest {
		t.Fatalf("rung 12 after rung 20 = %v %v, asked alone = %v %v", got.Dest, ok, alone.Dest, aloneOK)
	}
	if spent := shared.routes - before; spent >= aloneRoutes {
		t.Fatalf("the rung after the preferred distance spent %d searches, no fewer than the %d it costs alone", spent, aloneRoutes)
	}
}

// bareScene is an account at origin with a monster of monsterType one tile west, targeting it, and
// nothing else in view.
func newBareScene(t *testing.T, origin pathfind.NavPoint, monsterType int) icePen {
	t.Helper()
	pack := realCollisionPack(t)
	withResolver(t, realNpcConfig(t))
	st := overlapState()
	sc := icePen{
		s:      &overlapSession{state: st},
		pack:   pack,
		origin: origin,
		swarm:  pathfind.NavPoint{X: origin.X - 1, Z: origin.Z},
	}
	setExactTile(st, origin.X, origin.Z, origin.Level)
	if x, z, _, _ := st.Tile(); x != origin.X || z != origin.Z {
		t.Fatalf("the scene put the account at %d,%d, not %v", x, z, origin)
	}
	addFacingNPCs(st, facingNPC{npcSpec{1, monsterType, -1, 0}, 32768 + st.Self()})
	return sc
}

// A Swarm beside an account inside the mountain's wolf territory is not left by the leash, which
// ends wherever the leash is lost (32 of 54 sampled origins: inside the territory), but by the
// ordinary least-exposed flee, whose landing is never in territory. The picks are the pre-change
// search's own.
func TestSwarmInWolfTerritoryIsFledLikeAnyOtherMonster(t *testing.T) {
	for _, c := range []struct {
		origin, want pathfind.NavPoint
	}{
		{pathfind.NavPoint{X: 2862, Z: 3476}, pathfind.NavPoint{X: 2906, Z: 3432}},
		{pathfind.NavPoint{X: 2857, Z: 3486}, pathfind.NavPoint{X: 2909, Z: 3434}},
		{pathfind.NavPoint{X: 2859, Z: 3485}, pathfind.NavPoint{X: 2911, Z: 3433}},
	} {
		sc := newBareScene(t, c.origin, swarmNpcType)
		if !inWolfTerritory(c.origin) {
			t.Fatalf("control: %v is not in wolf territory", c.origin)
		}
		swarm, ok := threateningLeashed(sc.s.state, c.origin.X, c.origin.Z)
		if !ok {
			t.Fatalf("%v: the Swarm is not seen as the threat", c.origin)
		}
		if res, ok := leashFor(sc.s, sc.pack, c.origin, swarm, 1, nil); ok {
			t.Errorf("%v: a leash walk was planned inside wolf territory: %v", c.origin, res.Route)
		}
		ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: swarmNpcType, ThreatX: sc.swarm.X, ThreatZ: sc.swarm.Z}
		if _, ok := evadeLeash(sc.s, sc.pack, c.origin, ev, 1); ok {
			t.Errorf("%v: HandleEvade's probe offered a leash inside wolf territory", c.origin)
		}
		plan, how, found, stats := planFleeLeg(sc.s, sc.pack, c.origin, swarm.X, swarm.Z, 20, 1, nil, nil, nil)
		t.Logf("%v: %v (%s) [%s]", c.origin, plan.Dest, how, stats)
		if !found {
			t.Errorf("%v: no flee at all: %s", c.origin, how)
			continue
		}
		if strings.HasPrefix(how, "event leash") {
			t.Errorf("%v: fled by %q", c.origin, how)
		}
		if inWolfTerritory(plan.Dest) || inDangerZone(plan.Dest) {
			t.Errorf("%v: the flee lands at %v, in the wolves' reach", c.origin, plan.Dest)
		}
		if plan.Dest != c.want {
			t.Errorf("%v: flee lands at %v, the pre-change flee's pick was %v", c.origin, plan.Dest, c.want)
		}
		routeWalkable(t, sc.pack, plan.Route)
	}
}

// Just outside the territory the Swarm is still left by the leash, over ground that never enters it.
func TestSwarmJustOutsideWolfTerritoryIsStillLeashed(t *testing.T) {
	origin := pathfind.NavPoint{X: 2875, Z: 3428}
	sc := newBareScene(t, origin, swarmNpcType)
	if inWolfTerritory(origin) {
		t.Fatalf("control: %v is in wolf territory", origin)
	}
	swarm, ok := threateningLeashed(sc.s.state, origin.X, origin.Z)
	if !ok {
		t.Fatal("the Swarm is not seen as the threat")
	}
	res, ok := leashFor(sc.s, sc.pack, origin, swarm, 1, nil)
	if !ok {
		t.Fatal("no leash walk just outside the territory")
	}
	if d := chebyshev(res.Dest.X, res.Dest.Z, sc.swarm.X, sc.swarm.Z); d < leashBreakFor(swarmLeash, 1) {
		t.Fatalf("the walk ends %d tiles from the Swarm's spawn, short of %d", d, leashBreakFor(swarmLeash, 1))
	}
	if e := routeExposure(origin, res.Route); e != 0 || inWolfTerritory(res.Dest) {
		t.Fatalf("the leash walk %v spends %d tiles in wolf territory (lands in it: %v)", res.Route, e, inWolfTerritory(res.Dest))
	}
	routeWalkable(t, sc.pack, res.Route)
}
