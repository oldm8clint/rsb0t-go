package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// The ring flee and the Ice warrior cave's herd, on the real map.
//
// A threat with no leash (the Drunken Dwarf: maxrange 10000, strength 1, 1-damage stones) cannot be left by
// distance, so it is fled, if at all, by the ring search. That search must not walk the account through eleven
// aggressive level-57 warriors to do it: the rune pen's safespot (3041,9588) is a pocket among them, and an
// account standing in it is better off holding its ground. Review of 17 Oct 2026, after the first version of the
// cave fix had confined the herd predicate to White Wolf Mountain: from the pen, a dwarf's ring flee became
// "ring 20 -> 3061,9569", 24 steps with 23 of them inside five tiles of a warrior's spawn.
//
// These tests need RSB0T_COLLISION_PACK and RSB0T_CONFIG_JAG.

// drunkenDwarfType is the Drunken Dwarf, the one event NPC with no leash (event_leash.go).
const drunkenDwarfType = 956

var (
	cavePenSafespot = pathfind.NavPoint{X: 3041, Z: 9588}
	cavePenMonster  = pathfind.NavPoint{X: 3040, Z: 9588}
)

// caveSpots are where the rule is held to: the rune pen's safespot, and the three stands the live deaths
// happened at.
func caveSpots() []struct {
	name            string
	origin, monster pathfind.NavPoint
} {
	spots := []struct {
		name            string
		origin, monster pathfind.NavPoint
	}{{"the rune pen safespot at 3041,9588", cavePenSafespot, cavePenMonster}}
	for _, c := range caveCases {
		spots = append(spots, struct {
			name            string
			origin, monster pathfind.NavPoint
		}{c.name, c.origin, c.monster})
	}
	return spots
}

// visibleWarriors lists the Ice warriors the scene's account can see.
func visibleWarriors(sc icePen) [][2]int {
	var out [][2]int
	for _, npc := range sc.s.State().NPCs() {
		if npc.Type == iceWarriorType {
			out = append(out, [2]int{npc.X, npc.Z})
		}
	}
	return out
}

// tilesNearWarriors counts the tiles of a route that lie within r of a warrior.
func tilesNearWarriors(route []pathfind.NavPoint, warriors [][2]int, r int) int {
	near := 0
	for _, p := range routeTiles(route) {
		for _, w := range warriors {
			if chebyshev(p.X, p.Z, w[0], w[1]) <= r {
				near++
				break
			}
		}
	}
	return near
}

func TestUnleashedThreatInTheIceCaveIsNeverFledThroughTheHerd(t *testing.T) {
	for _, sp := range caveSpots() {
		t.Run(sp.name, func(t *testing.T) {
			sc := newCaveScene(t, sp.origin, drunkenDwarfType, sp.monster)
			if _, leashed := threateningLeashed(sc.s.state, sp.origin.X, sp.origin.Z); leashed {
				t.Fatal("the Drunken Dwarf was seen as a leashed threat")
			}
			warriors := visibleWarriors(sc)
			if len(warriors) < 5 {
				t.Fatalf("control: only %d Ice warriors in view", len(warriors))
			}
			avoid := visibleWolfAvoidance(sc.s, sp.origin)
			if avoid == nil {
				t.Fatal("no herd predicate for the cave's Ice warriors")
			}

			// The walker's own call: the herd predicate for the plan, and the same with no bad destinations yet.
			var plan escapePlan
			var how, stats string
			var found bool
			searches, expanded := searchesAndExpansions(func() {
				plan, how, found, stats = planFleeLeg(sc.s, sc.pack, sp.origin, sp.monster.X, sp.monster.Z, 20, 1, avoid, withoutBadDests(avoid, nil, sp.origin), nil)
			})
			t.Logf("found %v: %s [%s], %d searches, %d expanded", found, how, stats, searches, expanded)
			if searches > fleeRouteBudget || expanded > 40000 {
				t.Fatalf("the plan cost %d searches and %d expansions", searches, expanded)
			}
			if strings.HasPrefix(how, "event leash") {
				t.Fatalf("a Drunken Dwarf was fled with %q: it has no leash", how)
			}
			if found {
				routeWalkable(t, sc.pack, plan.Route)
				for _, p := range routeTiles(plan.Route) {
					for _, w := range warriors {
						d0 := chebyshev(sp.origin.X, sp.origin.Z, w[0], w[1])
						if d := chebyshev(p.X, p.Z, w[0], w[1]); d < min(d0, 12) {
							t.Fatalf("the flee passes %d tiles from the Ice warrior at %d,%d, nearer than the account's own %d (route %v)", d, w[0], w[1], d0, plan.Route)
						}
					}
				}
			}

			// The reproduction, at the pen: with no herd rule the ordinary flee walks through the warriors.
			if sp.origin == cavePenSafespot {
				raw, rawHow, rawFound, _ := planFleeLeg(sc.s, sc.pack, sp.origin, sp.monster.X, sp.monster.Z, 20, 1, nil, nil, nil)
				if !rawFound {
					t.Fatalf("control: without a herd rule the pen has no ordinary flee either (%s)", rawHow)
				}
				if near := tilesNearWarriors(raw.Route, warriors, 5); near < 10 {
					t.Fatalf("control: the flee without a herd rule spends only %d tiles within 5 of a warrior (%v), the reproduction no longer holds", near, raw.Route)
				} else {
					t.Logf("control: without the herd rule the flee is %s, %d tiles within 5 of a warrior", rawHow, near)
				}
				if found {
					t.Fatalf("the herd rule let the pen's flee through: %v", plan.Route)
				}
			}
		})
	}
}

// The walker end to end at the pen against a threat with no leash, an account that cannot be moved: it sends
// no walk (there is nowhere to go that is not through the herd), says it is stuck, and holds the ground.
func TestWalkerHoldsGroundAtTheIcePenAgainstAnUnleashedThreat(t *testing.T) {
	sc := newCaveScene(t, cavePenSafespot, drunkenDwarfType, cavePenMonster)
	prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 200*time.Millisecond, 1200*time.Millisecond
	t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })
	forgetNoEscape(sc.s)
	t.Cleanup(func() { forgetNoEscape(sc.s) })
	hitSelf(sc.s.state, 1) // the dwarf's stone: the account is under attack

	walks := 0
	sc.s.onSend = func(op byte) {
		if op == clientprot.MOVE_GAMECLICK {
			walks++
		}
	}
	var lines []string
	stuck, err := fleeWalking(sc.s, sc.pack, 20, func(m string) { lines = append(lines, m) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stuck %v, %d walks:\n%s", stuck, walks, strings.Join(lines, "\n"))
	if walks != 0 {
		t.Fatalf("the account was walked %d times from a pocket whose only way out is through the herd", walks)
	}
	if !stuck {
		t.Fatal("a walker that found nowhere to go did not say it was stuck")
	}
}
