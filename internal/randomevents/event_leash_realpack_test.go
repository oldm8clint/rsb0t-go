package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// The Asgarnian ice dungeon's Ice warrior cave on the real map, where five ScarabSwarm accounts died between
// 15 and 17 Oct 2026 with a random event monster at their side and no step taken. Each log line of those
// minutes read "no reachable flee candidate ... the only landing needs a route closer to a visible wolf, Ice
// warrior or giant than the account stands (1 reachable tiles)": the wolf predicate (type 125 = Ice warrior,
// added for White Wolf Mountain) refused every tile nearer a warrior than the account stood, and with eleven
// of them about there was none.
//
// The fix is NOT to drop that predicate for the cave. A Shade, a Strange plant or a Zombie is left by walking out
// of its leash (event_leash.go), which has its own graded margin from the herd and is tried first. The ring
// flee keeps the predicate, because without it a threat that has no leash (the Drunken Dwarf) is fled through
// the herd: the rune pen's safespot among the warriors is in live use, and a route from it to the open cave is
// 24 steps with 23 of them inside five tiles of a warrior's spawn (TestUnleashedThreatInTheIceCave...).
//
// These tests need RSB0T_COLLISION_PACK and RSB0T_CONFIG_JAG.

// iceCaveWarriors are the eleven Ice warrior spawns of the cave (content m47_149; the "icewarriors" rung of
// combatplan's spread ladder).
var iceCaveWarriors = [][2]int{
	{3044, 9581}, {3046, 9575}, {3048, 9583}, {3049, 9590}, {3052, 9568}, {3053, 9576},
	{3056, 9583}, {3056, 9587}, {3057, 9566}, {3062, 9576}, {3062, 9581},
}

// newCaveScene is an account at origin among every warrior it can see, with an event monster of
// monsterType at monster targeting it.
func newCaveScene(t *testing.T, origin pathfind.NavPoint, monsterType int, monster pathfind.NavPoint) icePen {
	t.Helper()
	pack := realCollisionPack(t)
	withResolver(t, realNpcConfig(t))
	st := overlapState()
	sc := icePen{s: &overlapSession{state: st}, pack: pack, origin: origin, swarm: monster}
	setExactTile(st, origin.X, origin.Z, 0)
	if x, z, _, _ := st.Tile(); x != origin.X || z != origin.Z {
		t.Fatalf("the scene put the account at %d,%d, not %v", x, z, origin)
	}
	specs := []facingNPC{{npcSpec{1, monsterType, monster.X - origin.X, monster.Z - origin.Z}, 32768 + st.Self()}}
	for i, w := range iceCaveWarriors {
		dx, dz := w[0]-origin.X, w[1]-origin.Z
		if dx < -16 || dx > 15 || dz < -16 || dz > 15 {
			continue // out of the 15 tiles the client sees
		}
		specs = append(specs, facingNPC{npcSpec{10 + i, iceWarriorType, dx, dz}, -1})
	}
	addFacingNPCs(st, specs...)
	return sc
}

// The three live spots: the Strange plant that poisoned its owner at the rung's stand (death 1, 16 Oct), the
// stand a Strange box was cycled at for seven minutes (death 2, 17 Oct 01:22; the leashed stand-in is a
// Zombie, maxrange 5), and the Shade a prayer event spawned beside the account (death 3, 17 Oct 02:50).
var caveCases = []struct {
	name         string
	origin       pathfind.NavPoint
	monsterType  int
	monster      pathfind.NavPoint
	maxrange     int
	oldFloodMost int // the predicate the fleet ran left no more ground than this
}{
	{"the stand at 3054,9578 and an angry Strange plant", pathfind.NavPoint{X: 3054, Z: 9578}, angryPlantType, pathfind.NavPoint{X: 3053, Z: 9578}, 3, 5},
	{"the stand at 3059,9577 and a Zombie", pathfind.NavPoint{X: 3059, Z: 9577}, 421, pathfind.NavPoint{X: 3060, Z: 9577}, 5, 5},
	{"the stand at 3062,9580 and a Shade", pathfind.NavPoint{X: 3062, Z: 9580}, 429, pathfind.NavPoint{X: 3063, Z: 9580}, 3, 60},
}

func TestCaveFleeHadNowhereToGoAndNowLeavesTheLeash(t *testing.T) {
	for _, c := range caveCases {
		t.Run(c.name, func(t *testing.T) {
			sc := newCaveScene(t, c.origin, c.monsterType, c.monster)

			// The reproduction: the predicate the fleet ran leaves a handful of tiles of a window that is
			// otherwise open ground.
			herd := visibleWolfAvoidance(sc.s, sc.origin)
			if herd == nil {
				t.Fatal("control: the cave's Ice warriors must put the herd predicate in force")
			}
			boxed := sc.pack.ReachFrom(sc.origin, fleeWindowRadius, herd).Size()
			open := sc.pack.ReachFrom(sc.origin, fleeWindowRadius, func(x, z, level int) bool {
				return level != sc.origin.Level || chebyshev(x, z, sc.origin.X, sc.origin.Z) > fleeWindowRadius
			}).Size()
			t.Logf("reachable: %d under the live predicate, %d open", boxed, open)
			if boxed > c.oldFloodMost {
				t.Fatalf("control: the live predicate left %d tiles at %v, the reproduction (<= %d) no longer holds", boxed, sc.origin, c.oldFloodMost)
			}
			if open < 800 {
				t.Fatalf("control: only %d tiles are open within the window", open)
			}

			// The fix: the walker plans a leash walk, a few steps long, ending clear of the leash and the herd,
			// whatever the herd predicate (still in force for the ring flee) says.
			monster, ok := threateningLeashed(sc.s.state, sc.origin.X, sc.origin.Z)
			if !ok {
				t.Fatal("the monster is not seen as a leashed threat")
			}
			if got, _ := eventLeashOf(monster.Type); got != c.maxrange {
				t.Fatalf("leash of type %d = %d, want %d", monster.Type, got, c.maxrange)
			}
			var plan escapePlan
			var how string
			var found bool
			searches, expanded := searchesAndExpansions(func() {
				plan, how, found, _ = planFleeLeg(sc.s, sc.pack, sc.origin, c.monster.X, c.monster.Z, 20, 1, herd, herd, nil)
			})
			if !found || !strings.HasPrefix(how, "event leash") {
				t.Fatalf("plan = %v found %v by %q, want an event leash walk", plan.Route, found, how)
			}
			t.Logf("%s; %d searches, %d expanded: %v", how, searches, expanded, plan.Route)
			if searches > 3 || expanded > 4000 {
				t.Fatalf("the plan cost %d searches and %d expansions", searches, expanded)
			}
			routeWalkable(t, sc.pack, plan.Route)
			if plan.Route[0] != sc.origin {
				t.Fatalf("the route starts at %v, not the account's tile %v", plan.Route[0], sc.origin)
			}
			want := leashBreakFor(c.maxrange, 1)
			if d := chebyshev(plan.Dest.X, plan.Dest.Z, c.monster.X, c.monster.Z); d < want {
				t.Fatalf("the walk ends %d tiles from the monster's spawn, short of %d (the engine drops the target beyond %d)", d, want, c.maxrange+1)
			}
			if steps := routeSteps(sc.origin, plan.Route[1:]); steps < 4 || steps > 12 {
				t.Fatalf("the walk is %d steps: the leash is %d tiles out, so a handful, and nowhere near the 97 to the ladder", steps, want)
			}
			// Never nearer an Ice warrior than the account stands now, and never within 3 of one.
			for _, p := range routeTiles(plan.Route) {
				for _, w := range iceCaveWarriors {
					d0 := chebyshev(sc.origin.X, sc.origin.Z, w[0], w[1])
					if d := chebyshev(p.X, p.Z, w[0], w[1]); d < min(d0, 3) {
						t.Fatalf("the walk passes %d tiles from the Ice warrior at %d,%d (the account stands %d away)", d, w[0], w[1], d0)
					}
				}
			}

			// One planner one answer: HandleEvade's probe is the walker's plan.
			ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: c.monsterType, ThreatX: c.monster.X, ThreatZ: c.monster.Z}
			probe, ok := evadeLeash(sc.s, sc.pack, sc.origin, ev, 1)
			if !ok || probe.Dest != plan.Dest {
				t.Fatalf("HandleEvade's probe = %v %v, the walker's plan lands at %v", probe.Dest, ok, plan.Dest)
			}
		})
	}
}

// The walker as the live accounts ran it, end to end, with the clocks shortened: it sends the leash route
// and says what it is doing (the live one planned nothing and logged "1 reachable tiles" twenty times).
func TestPlantWalkerSendsTheLeashRouteInTheCave(t *testing.T) {
	c := caveCases[0]
	sc := newCaveScene(t, c.origin, c.monsterType, c.monster)
	prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 400*time.Millisecond, 900*time.Millisecond
	t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })
	forgetNoEscape(sc.s)
	t.Cleanup(func() { forgetNoEscape(sc.s) })

	sends := 0
	sc.s.onSend = func(byte) { sends++ }
	var lines []string
	if _, err := fleeWalking(sc.s, sc.pack, 20, func(m string) { lines = append(lines, m) }); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d sends:\n%s", sends, strings.Join(lines, "\n"))
	if sends == 0 {
		t.Fatal("the walker sent nothing")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "heading to") && strings.Contains(l, "event leash") {
			found = true
		}
	}
	if !found {
		t.Fatal("no \"heading to ... event leash\" line was logged")
	}
}

// HandleEvade end to end for an account that cannot be moved (a walk the server ignores, as in the live
// deaths): it does not wait out its 45 s for another walk, it takes the cornered path, which hands the account
// back to its script, and it does NOT attack a Strange plant (defence 99, 200 hitpoints) or a Shade.
func TestEvadeThatCannotMoveTheAccountStandsDownWithoutAttackingAPlantOrShade(t *testing.T) {
	for _, c := range []int{0, 2} {
		cc := caveCases[c]
		t.Run(cc.name, func(t *testing.T) {
			sc := newCaveScene(t, cc.origin, cc.monsterType, cc.monster)
			prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
			fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 150*time.Millisecond, 700*time.Millisecond
			t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })
			forgetNoEscape(sc.s)
			t.Cleanup(func() { forgetNoEscape(sc.s) })
			g := NewGuardian(sc.s, sc.pack)
			g.CombatExpected = true
			defer g.Stop()

			var ops []byte
			sc.s.onSend = func(op byte) { ops = append(ops, op) }
			ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: cc.monsterType, ThreatX: cc.monster.X, ThreatZ: cc.monster.Z}
			var lines []string
			started := time.Now()
			if err := HandleEvade(sc.s, sc.pack, ev, 1, func(m string) { lines = append(lines, m) }); err != nil {
				t.Fatal(err)
			}
			took := time.Since(started)
			t.Logf("returned in %s:\n%s", took.Round(time.Millisecond), strings.Join(lines, "\n"))
			if took > 10*time.Second {
				t.Fatalf("HandleEvade held the account for %s: it waited out its 45 s for a walk that was not moving it", took)
			}
			if !g.cornered() {
				t.Fatal("the account was not handed back to its script (no cornered window)")
			}
			for _, op := range ops {
				if op == clientprot.OPNPC2 {
					t.Fatal("the account attacked an event monster that is not worth attacking")
				}
			}
		})
	}
}
