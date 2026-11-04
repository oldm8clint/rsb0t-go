package randomevents

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/serverprot"
)

// The live failure, on the real map. 15 Oct 2026, 07:53-07:59: an account at the Asgarnian ice
// dungeon's rune pen (the safespot 3041,9588, a pocket among eleven Ice warriors) had a Swarm land
// beside it, and for more than five minutes every flee ran dozens of failing full-component searches
// and then nothing (see flee_plan.go for the chain). These tests hold that spot to a bound, and to a
// sane answer.
//
// They need RSB0T_COLLISION_PACK and RSB0T_CONFIG_JAG (the cache's npc names and options: the Ice
// warrior is only "something that attacks" because its config says Attack).

var (
	iceCfgOnce sync.Once
	iceCfgVal  *cachecfg.Config
)

func realNpcConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	iceCfgOnce.Do(func() {
		if cfg, err := cachecfg.LoadFile(path); err == nil {
			iceCfgVal = cfg
		}
	})
	if iceCfgVal == nil {
		t.Fatal("cache config did not load")
	}
	return iceCfgVal
}

const (
	iceWarriorType = 125
	rockGolemType  = 413
)

// iceWarriorSpawns are the herd's spawn tiles (content m47_149, scarabplan.IceWarriorThreats) that an
// account at the safespot sees: the rest are more than the 15 tiles of its view away.
var iceWarriorSpawns = [][2]int{{3044, 9581}, {3046, 9575}, {3048, 9583}, {3049, 9590}, {3053, 9576}}

// setExactTile places the local player on any tile (setTile only reaches multiples of 8): the build
// area starts at the tile's own 8x8 zone and the player's local coordinates are the remainder.
func setExactTile(s *gamestate.State, x, z, level int) {
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(x>>3 + 6))
		p.P2(uint16(z>>3 + 6))
	}))
	s.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1}, bitField{2, 3}, bitField{2, uint32(level)},
		bitField{7, uint32(x & 7)}, bitField{7, uint32(z & 7)}, bitField{1, 0}, bitField{1, 0}))
}

// icePen is the account at the safespot 3041,9588 and what it sees.
type icePen struct {
	s      *overlapSession
	pack   *pathfind.Pack
	origin pathfind.NavPoint
	swarm  pathfind.NavPoint // where the monster stands
}

func newIcePen(t *testing.T, monsterType int) icePen {
	t.Helper()
	pack := realCollisionPack(t)
	withResolver(t, realNpcConfig(t))
	st := overlapState()
	pen := icePen{
		s:      &overlapSession{state: st},
		pack:   pack,
		origin: pathfind.NavPoint{X: 3041, Z: 9588},
		swarm:  pathfind.NavPoint{X: 3040, Z: 9588},
	}
	setExactTile(st, pen.origin.X, pen.origin.Z, 0)
	if x, z, _, _ := st.Tile(); x != pen.origin.X || z != pen.origin.Z {
		t.Fatalf("the scene put the account at %d,%d, not %v", x, z, pen.origin)
	}
	specs := []facingNPC{{npcSpec{1, monsterType, pen.swarm.X - pen.origin.X, pen.swarm.Z - pen.origin.Z}, 32768 + st.Self()}}
	for i, w := range iceWarriorSpawns {
		specs = append(specs, facingNPC{npcSpec{10 + i, iceWarriorType, w[0] - pen.origin.X, w[1] - pen.origin.Z}, -1})
	}
	addFacingNPCs(st, specs...)
	return pen
}

// routeWalkable holds a corner-compressed route to the pack: every step it takes is one the exit masks
// allow.
func routeWalkable(t *testing.T, pack *pathfind.Pack, route []pathfind.NavPoint) {
	t.Helper()
	for i := 1; i < len(route); i++ {
		cur, end := route[i-1], route[i]
		sx, sz := signum(end.X-cur.X), signum(end.Z-cur.Z)
		if (sx != 0 && sz != 0 && abs(end.X-cur.X) != abs(end.Z-cur.Z)) || stepDir(sx, sz) < 0 {
			t.Fatalf("leg %v -> %v is not a straight or diagonal line", cur, end)
		}
		for cur.X != end.X || cur.Z != end.Z {
			if pack.ExitMask(cur.X, cur.Z, cur.Level)&(1<<uint(stepDir(sx, sz))) == 0 {
				t.Fatalf("the route steps %d,%d -> %d,%d, which the collision pack does not allow", cur.X, cur.Z, cur.X+sx, cur.Z+sz)
			}
			cur.X, cur.Z = cur.X+sx, cur.Z+sz
		}
	}
}

// routeTiles lists every tile after the first that a corner-compressed route walks.
func routeTiles(route []pathfind.NavPoint) []pathfind.NavPoint {
	var out []pathfind.NavPoint
	for i := 1; i < len(route); i++ {
		cx, cz := route[i-1].X, route[i-1].Z
		for cx != route[i].X || cz != route[i].Z {
			cx += signum(route[i].X - cx)
			cz += signum(route[i].Z - cz)
			out = append(out, pathfind.NavPoint{X: cx, Z: cz, Level: route[i].Level})
		}
	}
	return out
}

// searchesAndExpansions runs f and reports the A* searches and expanded tiles it cost.
func searchesAndExpansions(f func()) (searches, expanded int64) {
	s0, e0 := pathfind.SearchStats()
	f()
	s1, e1 := pathfind.SearchStats()
	return s1 - s0, e1 - e0
}

// The Swarm beside the safespot: the plan is the shortest walk out of its leash along the pocket's
// way west, clear of the herd, for one route search where the live plan ran 298 (57,000 expansions).
func TestSwarmFleePlanIsBoundedAtTheIceDungeonPen(t *testing.T) {
	pen := newIcePen(t, swarmNpcType)
	swarm, ok := threateningLeashed(pen.s.state, pen.origin.X, pen.origin.Z)
	if !ok {
		t.Fatal("the Swarm is not seen as the threat")
	}
	// The plan is made under the herd predicate the walker holds the ring flee to, which boxes the pocket in:
	// the leash walk has its own margin from the herd and ignores it.
	avoid := visibleWolfAvoidance(pen.s, pen.origin)
	if avoid == nil {
		t.Fatal("control: the visible Ice warriors must put the herd predicate in force, as they did live")
	}
	var plan escapePlan
	var how, stats string
	var found bool
	var took time.Duration
	searches, expanded := searchesAndExpansions(func() {
		started := time.Now()
		plan, how, found, stats = planFleeLeg(pen.s, pen.pack, pen.origin, swarm.X, swarm.Z, 20, 1, avoid, avoid, nil)
		took = time.Since(started)
	})
	t.Logf("%v in %s, %d searches, %d expanded: %s [%s]", plan.Route, took, searches, expanded, how, stats)
	if !found {
		t.Fatalf("no way out of the pocket: %s", how)
	}
	if !strings.HasPrefix(how, "event leash") {
		t.Fatalf("a Swarm was fled with %q, not by leaving its leash", how)
	}
	// Bounded: the live plan was 298 searches and 57,408 expansions.
	if searches > 3 || expanded > 2000 {
		t.Fatalf("the plan cost %d searches and %d expansions, want at most 3 and 2000", searches, expanded)
	}
	// Sane: a real route from the account's tile, over ground the pack allows, ending outside the leash.
	if plan.Route[0] != pen.origin {
		t.Fatalf("the route starts at %v, not the account's tile %v", plan.Route[0], pen.origin)
	}
	routeWalkable(t, pen.pack, plan.Route)
	if d := chebyshev(plan.Dest.X, plan.Dest.Z, pen.swarm.X, pen.swarm.Z); d < leashBreakFor(swarmLeash, 1) {
		t.Fatalf("the walk ends %d tiles from the Swarm's spawn, short of %d", d, leashBreakFor(swarmLeash, 1))
	}
	if steps := routeSteps(pen.origin, plan.Route[1:]); steps < 17 || steps > 40 {
		t.Fatalf("the walk is %d steps: the leash is 17 tiles out, and a longer walk than 40 is not the shortest way", steps)
	}
	// And away from the herd: no tile of the walk nearer an Ice warrior than the account stands now,
	// and never within 3 of one (the narrowest margin offered).
	for _, p := range routeTiles(plan.Route) {
		for _, w := range iceWarriorSpawns {
			d0 := chebyshev(pen.origin.X, pen.origin.Z, w[0], w[1])
			if d := chebyshev(p.X, p.Z, w[0], w[1]); d < min(d0, 3) {
				t.Fatalf("the walk passes %d tiles from the Ice warrior at %d,%d (the account stands %d away)", d, w[0], w[1], d0)
			}
		}
	}
	// Planned again with the herd out of view it is the same walk or shorter, never a longer one.
	if res, ok := leashPlan(pen.pack, pen.origin, pen.swarm.X, pen.swarm.Z, pen.swarm.X, pen.swarm.Z, leashBreakFor(swarmLeash, 1), nil, nil); !ok || routeSteps(pen.origin, res.Route[1:]) > routeSteps(pen.origin, plan.Route[1:]) {
		t.Fatalf("with no other attacker in view the walk is %v (ok %v)", res.Route, ok)
	}
}

// HandleEvade's probe finds the same walk from the event's own record of the monster, and only for one with a leash.
func TestEvadeProbeLeashesAnEventMonsterWithALeashAndNothingElse(t *testing.T) {
	pen := newIcePen(t, swarmNpcType)
	ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: swarmNpcType, ThreatX: pen.swarm.X, ThreatZ: pen.swarm.Z}
	res, ok := evadeLeash(pen.s, pen.pack, pen.origin, ev, 1)
	if !ok || chebyshev(res.Dest.X, res.Dest.Z, pen.swarm.X, pen.swarm.Z) < leashBreakFor(swarmLeash, 1) {
		t.Fatalf("probe = %v %v", res.Dest, ok)
	}
	// A Swarm that has gone out of view is still planned from the event's record of it.
	gone := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 99, NpcType: swarmNpcType, ThreatX: pen.swarm.X, ThreatZ: pen.swarm.Z}
	if _, ok := evadeLeash(pen.s, pen.pack, pen.origin, gone, 1); !ok {
		t.Fatal("no walk for a Swarm out of view")
	}
	// A Rock Golem (maxrange 5) is leashed as well; the Drunken Dwarf (maxrange 10000) cannot be.
	golem := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: rockGolemType, ThreatX: pen.swarm.X, ThreatZ: pen.swarm.Z}
	if res, ok := evadeLeash(pen.s, pen.pack, pen.origin, golem, 1); !ok || chebyshev(res.Dest.X, res.Dest.Z, pen.swarm.X, pen.swarm.Z) < leashBreakFor(5, 1) {
		t.Fatalf("a Rock Golem probe = %v %v, want a walk %d from its spawn", res.Dest, ok, leashBreakFor(5, 1))
	}
	dwarf := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: 956, ThreatX: pen.swarm.X, ThreatZ: pen.swarm.Z}
	if _, ok := evadeLeash(pen.s, pen.pack, pen.origin, dwarf, 1); ok {
		t.Fatal("a Drunken Dwarf was leashed")
	}
	// The second attempt aims farther out than the first.
	second, ok := evadeLeash(pen.s, pen.pack, pen.origin, ev, 2)
	if !ok || chebyshev(second.Dest.X, second.Dest.Z, pen.swarm.X, pen.swarm.Z) < leashBreakFor(swarmLeash, 2) {
		t.Fatalf("second attempt = %v %v, want at least %d from the spawn", second.Dest, ok, leashBreakFor(swarmLeash, 2))
	}
}

// The same pocket against a Rock Golem. Live, the ordinary flee was held to the visible-wolf predicate and
// found nothing in a pocket among eleven Ice warriors (4 reachable tiles, 298 searches a plan). A golem
// (maxrange 5) is left by its leash like the Swarm instead, clear of the herd, for a bounded number of
// searches, whatever the herd predicate says.
func TestOtherEventMonsterFleeAtTheIceDungeonPenIsBoundedAndKeepsOffTheHerd(t *testing.T) {
	pen := newIcePen(t, rockGolemType)
	live := visibleWolfAvoidance(pen.s, pen.origin)
	if live == nil {
		t.Fatal("control: the visible Ice warriors must put the herd predicate in force")
	}
	var plan escapePlan
	var how, stats string
	var found bool
	searches, expanded := searchesAndExpansions(func() {
		plan, how, found, stats = planFleeLeg(pen.s, pen.pack, pen.origin, pen.swarm.X, pen.swarm.Z, 20, 1, live, live, nil)
	})
	t.Logf("found %v: %s [%s], %d searches, %d expanded", found, how, stats, searches, expanded)
	if !found {
		t.Fatalf("no way out of the pocket from a Rock Golem: %s", how)
	}
	if !strings.HasPrefix(how, "event leash") {
		t.Fatalf("a Rock Golem was fled with %q, not by leaving its leash", how)
	}
	if searches > fleeRouteBudget || expanded > 40000 {
		t.Fatalf("the plan cost %d searches and %d expansions (live: 298 and 57,408)", searches, expanded)
	}
	routeWalkable(t, pen.pack, plan.Route)
	if d := chebyshev(plan.Dest.X, plan.Dest.Z, pen.swarm.X, pen.swarm.Z); d < leashBreakFor(5, 1) {
		t.Fatalf("the walk ends %d tiles from the golem's spawn, short of %d", d, leashBreakFor(5, 1))
	}
	// Control: under the live predicate the ordinary flee finds nothing, which is the reproduction.
	pl := newFleePlanner(pen.pack, pen.origin, pen.swarm.X, pen.swarm.Z, live, CatherbyBankRef)
	if p, ok := pl.escape(20); ok {
		if _, held := pl.heldTo(p, live); held {
			t.Fatal("control: the live predicate no longer boxes the pocket in; the reproduction does not hold")
		}
	}
}

// The flood is for ground where candidates fail. On open ground the first candidate is the answer
// and one search is the whole plan: no flood, and the same landing the old search chose (Lumbridge
// pickpocketing spot, TestCatherbyFleePicksUnchanged's last case).
func TestOpenGroundPlanIsOneSearchAndNoFlood(t *testing.T) {
	pack := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 3222, Z: 3218}
	var plan escapePlan
	var ok bool
	var stats string
	searches, _ := searchesAndExpansions(func() {
		pl := newFleePlanner(pack, origin, origin.X, origin.Z, nil, CatherbyBankRef)
		plan, ok = pl.escape(20)
		stats = pl.Stats()
	})
	if !ok || plan.Dest != (pathfind.NavPoint{X: 3222, Z: 3238}) {
		t.Fatalf("plan = %v %v, want 3222,3238", plan.Dest, ok)
	}
	if searches != 1 || !strings.Contains(stats, "no flood needed") {
		t.Fatalf("%d searches, %q: open ground should cost one search and no flood", searches, stats)
	}
	// And on cut-off ground the flood is built the moment a search fails, so the rest are screened.
	pen := newIcePen(t, rockGolemType)
	pl := newFleePlanner(pen.pack, pen.origin, pen.swarm.X, pen.swarm.Z, visibleWolfAvoidance(pen.s, pen.origin), CatherbyBankRef)
	pl.escape(20)
	if !strings.Contains(pl.Stats(), "reachable tiles") {
		t.Fatalf("the ice pocket's plan built no flood: %q", pl.Stats())
	}
}

// A plan with nowhere to go costs one search to find that out, however many candidates there are:
// the first search that fails floods the room and every other candidate is screened against the
// flood without a search. The Waterfall dungeon's fire-giant room, shut in by its door (lane Q's death,
// 2026-09-24), has no tile 15 from the giant inside it; the old plan ran a search for every ring
// candidate to learn that, across the preferred distance and four shorter rungs.
func TestHopelessPlanCostsOneFailedSearch(t *testing.T) {
	pack := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2565, Z: 9880}
	var ok bool
	var routes int
	searches, expanded := searchesAndExpansions(func() {
		pl := newFleePlanner(pack, origin, 2564, 9882, nil, origin)
		_, _, ok = pl.ladder(20)
		routes = pl.routes
	})
	if ok {
		t.Fatal("control: a flee was found from the shut fire-giant room, the reproduction no longer holds")
	}
	if routes != 1 || searches != 1 || expanded > 500 {
		t.Fatalf("a hopeless plan counted %d route searches and ran %d (%d expansions), want one failed search in a ~100-tile room", routes, searches, expanded)
	}
}

// The walking flee as the live accounts ran it, end to end on the real map with the clocks shortened:
// a stationary account (nothing moves it, as when the server ignores the walk) beside a monster it
// cannot flee. The old walker re-ran its whole plan in a loop with no sleep and no log; this one
// plans once, says so once, holds still, and SAYS IT IS STUCK so the evade handler can stand down
// instead of waiting for another walk. The place is the Waterfall dungeon's shut fire-giant room (lane Q's
// death, 2026-09-24), which has no tile 15 from a monster inside it and nothing to leave a leash by.
func TestStuckWalkerPlansOnceAndDoesNotSpin(t *testing.T) {
	sc := newBareScene(t, pathfind.NavPoint{X: 2565, Z: 9880}, rockGolemType)
	prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 200*time.Millisecond, 1500*time.Millisecond
	t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })
	forgetNoEscape(sc.s)
	t.Cleanup(func() { forgetNoEscape(sc.s) })

	var lines []string
	var stuck bool
	searches, expanded := searchesAndExpansions(func() {
		var err error
		if stuck, err = fleeWalking(sc.s, sc.pack, 20, func(m string) { lines = append(lines, m) }); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("%d searches, %d expanded, %d log lines, stuck %v:\n%s", searches, expanded, len(lines), stuck, strings.Join(lines, "\n"))
	if searches > fleeRouteBudget || expanded > 40000 {
		t.Fatalf("the stuck walker ran %d searches and %d expansions in %s", searches, expanded, fleeWalkDeadline)
	}
	if !stuck {
		t.Fatal("a walker that found nowhere to go and never moved did not say it was stuck")
	}
	plans := 0
	for _, l := range lines {
		if strings.Contains(l, "no reachable flee candidate") || strings.Contains(l, "heading to") {
			plans++
		}
	}
	// One plan per 8 s of memory (here: one), however many times the loop came round.
	if plans > 2 {
		t.Fatalf("%d plans in %s", plans, fleeWalkDeadline)
	}
	if len(lines) > 6 {
		t.Fatalf("%d log lines in %s: the loop is spinning", len(lines), fleeWalkDeadline)
	}
}

// A Swarm at the same pocket, stationary account: the walker sends the leash route and keeps to it,
// logging the plan once.
func TestSwarmWalkerSendsTheLeashRoute(t *testing.T) {
	pen := newIcePen(t, swarmNpcType)
	prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 400*time.Millisecond, 900*time.Millisecond
	t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })

	sends := 0
	pen.s.onSend = func(byte) { sends++ }
	var lines []string
	searches, expanded := searchesAndExpansions(func() {
		if err := fleeFromCombatWalking(pen.s, pen.pack, 20, func(m string) { lines = append(lines, m) }); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("%d sends, %d searches, %d expanded:\n%s", sends, searches, expanded, strings.Join(lines, "\n"))
	if sends == 0 {
		t.Fatal("the walker sent nothing")
	}
	if searches > 20 || expanded > 8000 {
		t.Fatalf("%d searches and %d expansions for a Swarm walk in %s", searches, expanded, fleeWalkDeadline)
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "heading to") && strings.Contains(l, "event leash") {
			found = true
		}
	}
	if !found {
		t.Fatal("no \"heading to ... swarm leash\" line was logged")
	}
}
