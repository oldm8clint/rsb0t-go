package webwalk

import (
	"strings"
	"testing"
	"time"
)

// beforeFollowRig is a long corridor, a walker over it and a log of what the walker said.
type beforeFollowRig struct {
	f    *fakeWorld
	w    *Walker
	logs []string
}

func newBeforeFollowRig(t *testing.T, col *grid, me Point) *beforeFollowRig {
	t.Helper()
	f := newFakeWorld(t, col, me)
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	r := &beforeFollowRig{f: f, w: w}
	w.Log = func(s string) { r.logs = append(r.logs, s) }
	return r
}

func (r *beforeFollowRig) count(sub string) int {
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// The hook is asked once a walk, right after the first plan; when it moves the player the walk plans again from the landing
// (its own "route" line) without spending a repath.
func TestBeforeFollowRunsOncePerWalkAndReplansWithoutSpendingARepath(t *testing.T) {
	col := newGrid().fill(0, 0, 420, 6, 0)
	r := newBeforeFollowRig(t, col, Point{X: 2, Z: 3})
	calls := 0
	var seen FollowQuery
	r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		calls++
		seen = q
		r.f.me = Point{X: 300, Z: 3} // the cast
		return BeforeFollowResult{Moved: true, Planned: true}
	}
	res := r.w.WalkTo(Point{X: 402, Z: 3}, WalkOptions{Radius: 1, Timeout: 30 * time.Minute})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if calls != 1 {
		t.Errorf("the hook was asked %d times, want once: the move ends its allowance", calls)
	}
	if !seen.Route.OK || seen.Here != (Point{X: 2, Z: 3}) || seen.Dest != (Point{X: 402, Z: 3}) {
		t.Errorf("the hook saw %+v", seen)
	}
	if n := r.count("route "); n != 2 {
		t.Errorf("%d route lines, want 2 (from here, then from the landing)", n)
	}
	if n := r.count("repathing"); n != 0 {
		t.Errorf("%d repaths: a move must not spend one", n)
	}
	if r.f.me.X < 400 {
		t.Errorf("ended at %s", r.f.me)
	}
}

// A plan that failed is shown to the hook too, whether it found no path or ran out of budget: that is where a cast helps most.
func TestBeforeFollowSeesTheFailedFirstPlanAndTheBudgetPlan(t *testing.T) {
	col := newGrid().fill(0, 0, 20, 5, 0).fill(100, 0, 102, 5, 0) // two islands
	r := newBeforeFollowRig(t, col, Point{X: 2, Z: 3})
	var seen []Route
	r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		seen = append(seen, q.Route)
		r.f.me = Point{X: 100, Z: 3}
		return BeforeFollowResult{Moved: true, Planned: true}
	}
	if res := r.w.WalkTo(Point{X: 101, Z: 3}, WalkOptions{Radius: 1, Timeout: 5 * time.Minute}); !res.Arrived() {
		t.Fatalf("no path: result = %+v", res)
	}
	if len(seen) != 1 || seen[0].OK {
		t.Fatalf("the hook saw %+v, want one failed route", seen)
	}

	// The search ran out of its expansion budget on a long corridor.
	col = newGrid().fill(0, 0, 420, 6, 0)
	r = newBeforeFollowRig(t, col, Point{X: 2, Z: 3})
	seen = nil
	r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		seen = append(seen, q.Route)
		r.f.me = Point{X: 400, Z: 3}
		return BeforeFollowResult{Moved: true, Planned: true}
	}
	res := r.w.WalkTo(Point{X: 402, Z: 3}, WalkOptions{Radius: 1, Timeout: 30 * time.Minute, Search: Options{MaxExpansions: 60}})
	if !res.Arrived() {
		t.Fatalf("budget: result = %+v", res)
	}
	if len(seen) != 1 || seen[0].OK || !strings.Contains(seen[0].Reason, "budget") {
		t.Fatalf("the hook saw %+v, want a route that ran out of budget", seen)
	}
}

// NoTeleport, a pocket route and a walk that has already moved never ask the hook.
func TestBeforeFollowSkippedForPocketRoutesAndNoTeleport(t *testing.T) {
	col := newGrid().fill(0, 0, 40, 6, 0)
	r := newBeforeFollowRig(t, col, Point{X: 2, Z: 3})
	calls := 0
	r.w.BeforeFollow = func(FollowQuery) BeforeFollowResult {
		calls++
		return BeforeFollowResult{}
	}
	dest := Point{X: 30, Z: 3}
	if res := r.w.WalkTo(dest, WalkOptions{Radius: 1, NoTeleport: true}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if calls != 0 {
		t.Errorf("a NoTeleport walk asked the hook %d times", calls)
	}
	route := Route{OK: true}
	if r.w.beforeFollow(Point{X: 2, Z: 3}, dest, WalkOptions{}, nil, route, true) || calls != 0 {
		t.Errorf("a pocket route asked the hook (%d calls)", calls)
	}
	if r.w.beforeFollow(Point{X: 2, Z: 3}, dest, WalkOptions{}, nil, route, false); calls != 1 {
		t.Errorf("an ordinary route asked the hook %d times, want 1", calls)
	}
	r.w.walk.teleMoved = true
	if r.w.beforeFollow(Point{X: 2, Z: 3}, dest, WalkOptions{}, nil, route, false) || calls != 1 {
		t.Errorf("a walk that already moved asked again (%d calls)", calls)
	}
	// A Walker with no hook is untouched.
	r.w.BeforeFollow = nil
	r.w.walk.teleMoved = false
	if r.w.beforeFollow(Point{X: 2, Z: 3}, dest, WalkOptions{}, nil, route, false) {
		t.Error("a nil hook moved the player")
	}
}

// Only decisions that planned something count against the walk's allowance of three: the cheap declines of a short walk never
// use it up, and a destination the ladder keeps retrying cannot make the hook price landings without end.
func TestBeforeFollowCountsOnlyPlanningDecisionsUpToThree(t *testing.T) {
	run := func(planned bool) (calls int) {
		col := newGrid().fill(0, 0, 20, 5, 0).fill(100, 0, 102, 5, 0) // no way across
		r := newBeforeFollowRig(t, col, Point{X: 2, Z: 3})
		r.w.BeforeFollow = func(FollowQuery) BeforeFollowResult {
			calls++
			return BeforeFollowResult{Planned: planned}
		}
		r.w.WalkTo(Point{X: 101, Z: 3}, WalkOptions{Radius: 1, Timeout: 10 * time.Minute})
		return calls
	}
	planning, cheap := run(true), run(false)
	if planning != maxTeleDecisions {
		t.Errorf("a hook that planned every time was asked %d times, want %d", planning, maxTeleDecisions)
	}
	if cheap <= planning {
		t.Errorf("a hook that only declined cheaply was asked %d times, want more than the %d of one that planned", cheap, planning)
	}
}

// A landing is priced with the danger zones resolved from the landing: the Draynor jail guards allow a walk that STARTS inside
// them, and that exemption belongs to the account's own tile, not to a landing across the map.
func TestPlanFromResolvesZonesFromTheLanding(t *testing.T) {
	col := newGrid().fill(3080, 3215, 3160, 3270, 0)
	r := newBeforeFollowRig(t, col, Point{X: 3100, Z: 3230}) // inside the jail guards' zone
	dest := Point{X: 3150, Z: 3243}
	landing := Point{X: 3085, Z: 3243} // outside the zone, west of it
	var fromHere, fromLanding Route
	r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		fromHere = q.Route
		fromLanding = q.PlanFrom(landing)
		return BeforeFollowResult{Planned: true}
	}
	r.w.WalkTo(dest, WalkOptions{Radius: 1, Timeout: 5 * time.Minute})
	if !fromHere.OK || !fromLanding.OK {
		t.Fatalf("routes: here %+v landing %+v", fromHere, fromLanding)
	}
	if straight := Chebyshev(landing, dest); fromLanding.Cost <= straight {
		t.Errorf("the walk from the landing costs %d, no more than the straight line %d: the zone exemption leaked from the start tile", fromLanding.Cost, straight)
	}
	if straight := Chebyshev(Point{X: 3100, Z: 3230}, dest); fromHere.Cost != straight {
		t.Errorf("the walk from inside the zone costs %d, want the straight line %d", fromHere.Cost, straight)
	}
}

// The walk's own ad-hoc rectangles apply to a landing's price too.
func TestPlanFromUsesTheWalksOwnAvoidRects(t *testing.T) {
	col := newGrid().fill(0, 0, 100, 40, 0)
	r := newBeforeFollowRig(t, col, Point{X: 2, Z: 20})
	dest := Point{X: 45, Z: 35}
	landing := Point{X: 45, Z: 5}
	var plain, avoided Route
	r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		avoided = q.PlanFrom(landing)
		return BeforeFollowResult{Planned: true}
	}
	// Between the landing and the destination, so the route has to go round it (a diagonal costs the same as a straight
	// step, so a detour only costs when the rectangle sits across the line).
	wall := Rect{MinX: 30, MaxX: 60, MinZ: 15, MaxZ: 25, Level: 0}
	r.w.WalkTo(dest, WalkOptions{Radius: 1, Timeout: 5 * time.Minute, AvoidRects: []Rect{wall}})
	r2 := newBeforeFollowRig(t, col, Point{X: 2, Z: 20})
	r2.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
		plain = q.PlanFrom(landing)
		return BeforeFollowResult{Planned: true}
	}
	r2.w.WalkTo(dest, WalkOptions{Radius: 1, Timeout: 5 * time.Minute})
	if !plain.OK || !avoided.OK {
		t.Fatalf("plain %+v avoided %+v", plain, avoided)
	}
	if avoided.Cost <= plain.Cost {
		t.Errorf("the landing's price ignored the walk's AvoidRects: %d with the wall, %d without", avoided.Cost, plain.Cost)
	}
}

// Walker.PlanFrom from the player's own tile is Walker.Plan, and from another tile is the walk from there.
func TestWalkerPlanFromMatchesPlanFromHere(t *testing.T) {
	col := newGrid().fill(0, 0, 100, 40, 0)
	r := newBeforeFollowRig(t, col, Point{X: 2, Z: 20})
	dest := Point{X: 90, Z: 20}
	o := WalkOptions{Radius: 1}
	here, from := r.w.Plan(dest, o), r.w.PlanFrom(Point{X: 2, Z: 20}, dest, o)
	if !here.OK || here.Cost != from.Cost || len(here.Waypoints) != len(from.Waypoints) {
		t.Errorf("Plan %+v, PlanFrom(here) %+v", here, from)
	}
	elsewhere := r.w.PlanFrom(Point{X: 60, Z: 20}, dest, o)
	if !elsewhere.OK || elsewhere.Cost != 30 {
		t.Errorf("PlanFrom(60,20) = %+v, want a 30-tile route", elsewhere)
	}
	if r.f.me != (Point{X: 2, Z: 20}) {
		t.Errorf("planning moved the player to %s", r.f.me)
	}
}

// The key a caller remembers a landing's price under changes with whatever changes the price, and with nothing else.
func TestPlanKeyFollowsWhatDecidesALandingsPrice(t *testing.T) {
	col := newGrid().fill(0, 0, 100, 40, 0)
	key := func(dest Point, o WalkOptions, avoid map[Point]bool) uint64 {
		r := newBeforeFollowRig(t, col, Point{X: 2, Z: 20})
		var k uint64
		r.w.BeforeFollow = func(q FollowQuery) BeforeFollowResult {
			k = q.PlanKey()
			return BeforeFollowResult{}
		}
		route := Route{OK: true}
		r.w.beforeFollow(Point{X: 2, Z: 20}, dest, o, avoid, route, false)
		return k
	}
	dest := Point{X: 90, Z: 20}
	base := key(dest, WalkOptions{}, nil)
	if again := key(dest, WalkOptions{Radius: 3, Timeout: time.Hour}, nil); again != base {
		t.Error("a radius and a timeout changed the key: they do not change a price")
	}
	for name, other := range map[string]uint64{
		"another destination":  key(Point{X: 91, Z: 20}, WalkOptions{}, nil),
		"an avoided rectangle": key(dest, WalkOptions{AvoidRects: []Rect{{MinX: 5, MaxX: 6, MinZ: 5, MaxZ: 6}}}, nil),
		"a named zone":         key(dest, WalkOptions{AvoidZones: []string{"white-wolf-mountain"}}, nil),
		"the automatic zones":  key(dest, WalkOptions{NoAutomaticZones: true}, nil),
		"an avoided barrier":   key(dest, WalkOptions{}, map[Point]bool{{X: 4, Z: 2}: true}),
	} {
		if other == base {
			t.Errorf("%s left the key unchanged", name)
		}
	}
}
