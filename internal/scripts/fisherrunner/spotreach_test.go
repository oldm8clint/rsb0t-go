package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// reachableExcept is a stand-in for the real pathfinder: every tile has a route except the
// ones named. Enough to exercise the rule, since Pack.ReachableWithin has its own tests
// (internal/pathfind/reach_test.go) including the real Al Kharid palace geometry.
//
// It reports its route lengths as -1, "unknown", deliberately: these tests are about which
// candidates survive the filter, so they let pickSpot fall back to camp distance and stay
// readable. Route length as a SELECTION rule is tested separately, against a stub that models
// it, in routeStepsOf's own tests below.
func reachableExcept(blocked ...tile.Tile) func(tile.Tile) (bool, int) {
	return func(t tile.Tile) (bool, int) {
		for _, b := range blocked {
			if b == t {
				return false, -1
			}
		}
		return true, -1
	}
}

// routeStepsOf is the other stand-in: every named tile has a route of exactly that many steps,
// and anything unnamed has none. Where reachableExcept models the gate, this models the
// measurement the gate now hands back.
func routeStepsOf(steps map[tile.Tile]int) func(tile.Tile) (bool, int) {
	return func(t tile.Tile) (bool, int) {
		n, ok := steps[t]
		if !ok {
			return false, -1
		}
		return true, n
	}
}

func spotAt(index, x, z int) gamestate.NPC {
	return gamestate.NPC{Index: index, X: x, Z: z}
}

// TestRoutableSpotsDropsTheSpotWithNoRoute is the plain case: a candidate the account cannot
// walk to is not offered to pickSpot at all, and is counted so the caller can say so.
func TestRoutableSpotsDropsTheSpotWithNoRoute(t *testing.T) {
	near := spotAt(100, 2836, 3431) // offshore: no walkable tile touches it
	far := spotAt(200, 2853, 3423)

	kept, noRoute := routableSpots(
		[]spotCandidate{{NPC: near, Dist: 4, Here: 4, Crowd: 0}, {NPC: far, Dist: 12, Here: 12, Crowd: 0}},
		reachableExcept(tile.Tile{X: near.X, Z: near.Z}))

	if noRoute != 1 {
		t.Fatalf("noRoute = %d, want 1", noRoute)
	}
	if len(kept) != 1 || kept[0].NPC.Index != far.Index {
		t.Fatalf("kept %v, want only the far spot (200)", kept)
	}
}

// TestFilteredPickSkipsTheNearerSpotItCannotWalkTo is the fix as FindSpot composes it: the
// nearest-to-camp spot is the one with no route, and the selection takes the next-nearest one
// with a route rather than clicking across the water at the nearer one.
//
// Before the filter existed, pickSpot took the nearest by camp distance whatever was in
// between: the server accepts OPNPC on it, the player walks as close as the water allows,
// inOperableDistance is never satisfied, and no fishing trigger ever runs.
func TestFilteredPickSkipsTheNearerSpotItCannotWalkTo(t *testing.T) {
	walled := spotAt(100, 2836, 3431)
	open := spotAt(200, 2840, 3430)
	candidates := []spotCandidate{{NPC: walled, Dist: 4, Here: 4, Crowd: 0}, {NPC: open, Dist: 9, Here: 9, Crowd: 0}}

	if got, ok := pickSpot(candidates, gamestate.NPC{}, false, nil); !ok || got.Index != walled.Index {
		t.Fatalf("control: the unfiltered rule should still take the nearer walled spot, got %d (ok=%v)", got.Index, ok)
	}

	kept, noRoute := routableSpots(candidates, reachableExcept(tile.Tile{X: walled.X, Z: walled.Z}))
	got, ok := pickSpot(kept, gamestate.NPC{}, false, nil)
	if !ok || got.Index != open.Index {
		t.Fatalf("got index %d (ok=%v), want the reachable spot (200); noRoute=%d", got.Index, ok, noRoute)
	}
}

// TestFilteringRevalidatesTheHeldSpotEveryCall is the one that matters, and the reason the
// filter runs BEFORE pickSpot rather than after.
//
// pickSpot returns the held `current` spot outright whenever it is still among the candidates,
// with no comparison against anything — that stability is deliberate (see its own doc comment)
// and it is also exactly how AdvancedFighter kept re-attacking a walled-in warrior every 8
// seconds for a whole window: the selection gate was never the half that mattered, because the
// held target never went back through it. Here the held spot is dropped from the candidate list
// before pickSpot ever sees it, so a spot that becomes unreachable — the account was pushed off
// the shore by a flee, a random event, or a knockback — is let go on the very next call.
func TestFilteringRevalidatesTheHeldSpotEveryCall(t *testing.T) {
	held := spotAt(100, 2836, 3431)
	other := spotAt(200, 2840, 3430)
	candidates := []spotCandidate{{NPC: held, Dist: 4, Here: 4, Crowd: 0}, {NPC: other, Dist: 9, Here: 9, Crowd: 0}}

	// While it still has a route, the held spot keeps winning, unchanged behaviour.
	kept, _ := routableSpots(candidates, reachableExcept())
	if got, ok := pickSpot(kept, held, true, nil); !ok || got.Index != held.Index {
		t.Fatalf("with a route: got index %d (ok=%v), want the held spot (100) to stay selected", got.Index, ok)
	}

	// The account is now somewhere the held spot cannot be reached from. Without the filter
	// pickSpot would hand it straight back, forever.
	if got, ok := pickSpot(candidates, held, true, nil); !ok || got.Index != held.Index {
		t.Fatalf("control: unfiltered, the held spot should still be handed back, got %d (ok=%v)", got.Index, ok)
	}
	kept, noRoute := routableSpots(candidates, reachableExcept(tile.Tile{X: held.X, Z: held.Z}))
	got, ok := pickSpot(kept, held, true, nil)
	if !ok || got.Index != other.Index {
		t.Fatalf("got index %d (ok=%v), want it to let the held spot go for the reachable one (200)", got.Index, ok)
	}
	if noRoute != 1 {
		t.Fatalf("noRoute = %d, want 1 so the caller can report what it passed over", noRoute)
	}
}

// TestRoutableSpotsWithEveryRouteGoneKeepsNothing pins the state FishUntil's own recovery
// depends on: nothing kept and a non-zero count, which is "walk back to the camp", not "wait
// for a hazard to pass". That is why noRoute is not part of spotSafety.blocked().
func TestRoutableSpotsWithEveryRouteGoneKeepsNothing(t *testing.T) {
	a, b := spotAt(100, 2836, 3431), spotAt(200, 2840, 3430)
	kept, noRoute := routableSpots(
		[]spotCandidate{{NPC: a, Dist: 4, Here: 4, Crowd: 0}, {NPC: b, Dist: 9, Here: 9, Crowd: 0}},
		reachableExcept(tile.Tile{X: a.X, Z: a.Z}, tile.Tile{X: b.X, Z: b.Z}))
	if len(kept) != 0 || noRoute != 2 {
		t.Fatalf("kept %d candidate(s) with noRoute %d, want 0 and 2", len(kept), noRoute)
	}
	if _, ok := pickSpot(kept, a, true, nil); ok {
		t.Fatalf("expected no spot to be found once every route is gone, even with one held")
	}
}

// TestRoutableSpotsWithoutAPredicateKeepsEverything is the fail-open path: Reachable answers
// "reachable" when there is no collision pack, and a caller that passes no predicate at all
// must see exactly today's behaviour rather than an empty camp.
func TestRoutableSpotsWithoutAPredicateKeepsEverything(t *testing.T) {
	in := []spotCandidate{
		{NPC: spotAt(100, 2836, 3431), Dist: 4, Here: 4},
		{NPC: spotAt(200, 2840, 3430), Dist: 9, Here: 9},
	}
	kept, noRoute := routableSpots(in, nil)
	if len(kept) != 2 || noRoute != 0 {
		t.Fatalf("kept %d with noRoute %d, want all 2 kept and 0 passed over", len(kept), noRoute)
	}
}

// TestReachableFailsOpenWithNoPack pins the asymmetry the three sibling runners already
// document: without collision data this must not start refusing spots.
func TestReachableFailsOpenWithNoPack(t *testing.T) {
	ok, why := Reachable(&Deps{}, tile.Tile{X: 2836, Z: 3431})
	if !ok {
		t.Fatalf("Reachable with no pack = false (%s), want it to fail open", why)
	}
	if ok, why := Reachable(nil, tile.Tile{}); !ok {
		t.Fatalf("Reachable(nil) = false (%s), want it to fail open", why)
	}
}

// TestRoutableSpotsRecordsHowFarEachOneActuallyIs is the measurement half of the filter: the
// search that answers "is there a route" already knows how long that route is, and nothing was
// keeping it. Without this, pickSpot has only a straight line to score with.
func TestRoutableSpotsRecordsHowFarEachOneActuallyIs(t *testing.T) {
	round := spotAt(100, 2604, 3416) // five tiles away, thirty-eight steps round the bay
	across := spotAt(200, 2598, 3412)
	kept, noRoute := routableSpots(
		[]spotCandidate{{NPC: round, Dist: 5}, {NPC: across, Dist: 11}},
		routeStepsOf(map[tile.Tile]int{
			{X: round.X, Z: round.Z}:   38,
			{X: across.X, Z: across.Z}: 12,
		}))
	if noRoute != 0 || len(kept) != 2 {
		t.Fatalf("kept %d with noRoute %d, want both kept", len(kept), noRoute)
	}
	if !kept[0].HaveSteps || !kept[1].HaveSteps || kept[0].Steps != 38 || kept[1].Steps != 12 {
		t.Fatalf("recorded steps %d and %d, want 38 and 12", kept[0].Steps, kept[1].Steps)
	}
}

// TestRoutableSpotsLeavesAFailOpenAnswerUnmeasured: reachableSteps answers (true, -1) when it
// cannot search at all. That candidate must stay kept but unmeasured, or pickSpot would score a
// -1-step walk and prefer it over every real one.
func TestRoutableSpotsLeavesAFailOpenAnswerUnmeasured(t *testing.T) {
	kept, noRoute := routableSpots(
		[]spotCandidate{{NPC: spotAt(1, 2600, 3410), Dist: 5, Here: 5}},
		func(tile.Tile) (bool, int) { return true, -1 })
	if noRoute != 0 || len(kept) != 1 {
		t.Fatalf("kept %d with noRoute %d, want the candidate kept", len(kept), noRoute)
	}
	if kept[0].HaveSteps {
		t.Fatalf("a fail-open answer was recorded as a measured %d-step route", kept[0].Steps)
	}
}

// TestPickSpotTakesTheShorterWALKNotTheShorterStraightLine is the live Fishing Guild case, from
// run/fleet.log on build f505d78 at 06:03:17: an account at 2604,3421 re-found a spot five tiles
// due south at 2604,3416 and walked "cost 38, 16 waypoints" to get to it, because the water is
// between them and the only way across is round. A spot twice as far in a straight line, but on
// this side of the bay, is a third of the walk.
//
// Measured across the whole fleet, this is not an edge case: the guild's median approach leg was
// 20 steps against Catherby's 11, and approach legs overall had a route-to-straight-line ratio
// of 1.60 median and 4.00 at p90. The fix is not a new search — routableSpots already ran it.
func TestPickSpotTakesTheShorterWALKNotTheShorterStraightLine(t *testing.T) {
	roundTheBay := spotAt(100, 2604, 3416)
	thisSide := spotAt(200, 2598, 3412)
	candidates := []spotCandidate{
		{NPC: roundTheBay, Dist: 5, Here: 5, Steps: 38, HaveSteps: true},
		{NPC: thisSide, Dist: 11, Here: 11, Steps: 12, HaveSteps: true},
	}

	// The control is the rule without a measured route: score the straight line, take the spot
	// that looks nearer and is not.
	byStraightLine := append([]spotCandidate(nil), candidates...)
	for i := range byStraightLine {
		byStraightLine[i].HaveSteps = false
	}
	if got, ok := pickSpot(byStraightLine, gamestate.NPC{}, false, nil); !ok || got.Index != roundTheBay.Index {
		t.Fatalf("control: the straight-line rule should take the spot round the bay (100), got %d (ok=%v)", got.Index, ok)
	}

	got, ok := pickSpot(candidates, gamestate.NPC{}, false, nil)
	if !ok || got.Index != thisSide.Index {
		t.Fatalf("got index %d (ok=%v), want the spot on this side of the water (200): 12 steps against 38", got.Index, ok)
	}
}

// TestRouteScoringOffIsTheStraightLineControl pins the lab A/B switch: with RouteScoring off,
// a measured route is ignored and the straight-line spot wins, as it did before fisherwalk.
func TestRouteScoringOffIsTheStraightLineControl(t *testing.T) {
	defer func(was bool) { RouteScoring = was }(RouteScoring)
	roundTheBay, thisSide := spotAt(100, 2604, 3416), spotAt(200, 2598, 3412)
	candidates := []spotCandidate{
		{NPC: roundTheBay, Dist: 5, Here: 5, Steps: 38, HaveSteps: true},
		{NPC: thisSide, Dist: 11, Here: 11, Steps: 12, HaveSteps: true},
	}
	RouteScoring = false
	if got, _ := pickSpot(candidates, gamestate.NPC{}, false, nil); got.Index != roundTheBay.Index {
		t.Fatalf("RouteScoring off: got %d, want the straight-line spot (100)", got.Index)
	}
	RouteScoring = true
	if got, _ := pickSpot(candidates, gamestate.NPC{}, false, nil); got.Index != thisSide.Index {
		t.Fatalf("RouteScoring on: got %d, want the shorter walk (200)", got.Index)
	}
}

// TestPickSpotStillSpreadsACrowdWhenScoringByRoute keeps the crowd term the netrecovery agent
// added working in the same units as before. crowdWeight and spotJitter are both quoted in
// tiles, and a route length is tiles too, so swapping which distance is scored must not quietly
// re-tune either one: twelve fishers at a spot six steps away still lose to one fisher at a spot
// twenty steps away, exactly as they did when the six and the twenty were straight lines.
func TestPickSpotStillSpreadsACrowdWhenScoringByRoute(t *testing.T) {
	near, far := spotAt(1, 2855, 3423), spotAt(2, 2838, 3431)
	got, _ := pickSpot([]spotCandidate{
		{NPC: near, Dist: 3, Steps: 6, HaveSteps: true, Crowd: 0},
		{NPC: far, Dist: 9, Steps: 20, HaveSteps: true, Crowd: 0},
	}, gamestate.NPC{}, false, nil)
	if got.Index != near.Index {
		t.Fatalf("with nobody at either, want the shorter walk (1), got %d", got.Index)
	}
	got, _ = pickSpot([]spotCandidate{
		{NPC: near, Dist: 3, Steps: 6, HaveSteps: true, Crowd: 12},
		{NPC: far, Dist: 9, Steps: 20, HaveSteps: true, Crowd: 1},
	}, gamestate.NPC{}, false, nil)
	if got.Index != far.Index {
		t.Fatalf("with twelve at the near spot and one at the far, want the far spot (2), got %d", got.Index)
	}
}

// TestPickSpotFallsBackToStraightLineWhenTheRouteIsUnknown is the no-collision-pack path.
// Reachable fails open there, so there is no route to measure and HaveSteps stays false.
// Scoring the zero-value Steps would make every spot look adjacent and the choice arbitrary;
// the rule instead scores the straight line from the account (Here), as it did before.
func TestPickSpotFallsBackToStraightLineWhenTheRouteIsUnknown(t *testing.T) {
	nearCamp, farCamp := spotAt(100, 2836, 3431), spotAt(200, 2853, 3423)
	got, ok := pickSpot([]spotCandidate{
		{NPC: farCamp, Dist: 15, Here: 15},
		{NPC: nearCamp, Dist: 4, Here: 4},
	}, gamestate.NPC{}, false, nil)
	if !ok || got.Index != nearCamp.Index {
		t.Fatalf("got index %d (ok=%v), want the camp-nearest spot (100) when no route length is known", got.Index, ok)
	}
}

// TestPickSpotMixesAKnownRouteWithAnUnknownOne covers the half-and-half state routableSpots can
// genuinely produce: a nil predicate leaves every candidate unmeasured, but a live one can only
// measure the candidates it kept. A known short walk must still beat an unmeasured candidate
// that merely looks further, and an unmeasured one must not score as a zero-step walk.
func TestPickSpotMixesAKnownRouteWithAnUnknownOne(t *testing.T) {
	measured, unmeasured := spotAt(100, 2840, 3430), spotAt(200, 2836, 3431)
	got, ok := pickSpot([]spotCandidate{
		{NPC: measured, Dist: 9, Here: 9, Steps: 3, HaveSteps: true},
		{NPC: unmeasured, Dist: 4, Here: 4},
	}, gamestate.NPC{}, false, nil)
	if !ok || got.Index != measured.Index {
		t.Fatalf("got index %d (ok=%v), want the spot with a measured 3-step route (100)", got.Index, ok)
	}
}

// TestRouteStepsIgnoresARouteThatStopsShort: a route that ends more than a tile from the spot
// came from the pathfinder's within-five fallback, so its length is not the walk to the spot.
func TestRouteStepsIgnoresARouteThatStopsShort(t *testing.T) {
	spot := tile.Tile{X: 2604, Z: 3416}
	beside := pathfind.PathResult{OK: true, Waypoints: []pathfind.NavPoint{{X: 2604, Z: 3430}, {X: 2604, Z: 3417}}}
	if got := routeSteps(beside, spot); got != 13 {
		t.Fatalf("route ending beside the spot: %d steps, want 13", got)
	}
	short := pathfind.PathResult{OK: true, Waypoints: []pathfind.NavPoint{{X: 2604, Z: 3430}, {X: 2604, Z: 3421}}}
	if got := routeSteps(short, spot); got != -1 {
		t.Fatalf("route stopping 5 short: %d steps, want -1 (unknown)", got)
	}
	if got := routeSteps(pathfind.PathResult{OK: true}, spot); got != -1 {
		t.Fatalf("no waypoints: %d, want -1", got)
	}
}
