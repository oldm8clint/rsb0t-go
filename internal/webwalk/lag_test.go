package webwalk

import (
	"strings"
	"testing"
	"time"
)

// lagWorld is a fakeWorld whose picture of the player freezes for a stretch of game time, as a hung connection's does:
// the real player keeps walking whatever path the last click gave it, the walker sees the tile it saw when the lag began,
// and ViewLagging says so. Every click is counted, with whether the view was lagging when it was sent.
type lagWorld struct {
	*fakeWorld
	lagFrom, lagTo time.Time
	frozen         Point
	frozenSet      bool
	sent           int
	sentLagging    int
	lastPath       []Point
}

func (l *lagWorld) isLagging() bool { return !l.now.Before(l.lagFrom) && l.now.Before(l.lagTo) }

func (l *lagWorld) Tile() (Point, bool) {
	if l.isLagging() {
		if !l.frozenSet {
			l.frozen, l.frozenSet = l.fakeWorld.me, true
		}
		return l.frozen, true
	}
	return l.fakeWorld.me, true
}

func (l *lagWorld) ViewLagging() (bool, string) { return l.isLagging(), "no game tick for 12.0s" }

func (l *lagWorld) Walk(path []Point, run bool) error {
	l.sent++
	if l.isLagging() {
		l.sentLagging++
	}
	l.lastPath = append([]Point(nil), path...)
	return l.fakeWorld.Walk(path, run)
}

// strip is a flat three-tile-wide road along x, 0 to 100.
func strip(t *testing.T, me Point) (*lagWorld, *Walker, *[]string) {
	t.Helper()
	col := newGrid().fill(0, 0, 100, 2, 0)
	f := newFakeWorld(t, col, me)
	l := &lagWorld{fakeWorld: f}
	w := NewWalker(NewPlanner(col, nil), l)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	return l, w, &logs
}

func joined(logs *[]string) string { return strings.Join(*logs, "\n") }

// 17 Oct 12:21:27, the first half: a minute of silence. The walk neither clicks nor stalls nor learns that a step is blocked,
// and goes on from the real tile when the view is back.
func TestAWalkWaitsOutALaggingViewWithoutAClickAStallOrALearnedBlock(t *testing.T) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	l.lagFrom, l.lagTo = l.now.Add(6*time.Second), l.now.Add(66*time.Second)
	res := w.WalkTo(Point{X: 90, Z: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	if l.sentLagging != 0 {
		t.Errorf("%d clicks were sent while the view was lagging", l.sentLagging)
	}
	if l.sent == 0 {
		t.Fatal("the walk sent no clicks at all")
	}
	all := joined(logs)
	for _, bad := range []string{"stalled at", "teleported", "repathing", "no path"} {
		if strings.Contains(all, bad) {
			t.Errorf("the log has %q, which a lagging view must not cause:\n%s", bad, all)
		}
	}
	if len(w.learned) != 0 {
		t.Errorf("the walk learned %d blocked steps from a frozen view: %v", len(w.learned), w.learned)
	}
	if !strings.Contains(all, "view lagging (no game tick for 12.0s)") || !strings.Contains(all, "view current again after") {
		t.Errorf("the hold is not in the log:\n%s", all)
	}
}

// The view can jump when it comes back: the real player walked on, and the first tile it reads is far from the last. That is
// a catch-up, not a teleport, and it must not spend one of the walk's five repaths.
func TestAViewThatJumpsAfterALagIsNotATeleport(t *testing.T) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	l.lagFrom, l.lagTo = l.now.Add(12*time.Second), l.now.Add(40*time.Second)
	res := w.WalkTo(Point{X: 95, Z: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	if strings.Contains(joined(logs), "teleported") {
		t.Errorf("a catch-up jump was read as a teleport:\n%s", joined(logs))
	}
}

// A view that never comes back is given up on after lagHoldMax, with no click sent from it and no stop click either (the tile
// it has may be behind the player, and a click to it would walk the player back).
func TestAWalkGivesUpOnAViewThatNeverComesBack(t *testing.T) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	l.lagFrom, l.lagTo = l.now.Add(6*time.Second), l.now.Add(time.Hour)
	start := l.now
	res := w.WalkTo(Point{X: 90, Z: 1}, WalkOptions{Timeout: 20 * time.Minute})
	if res.Outcome != OutcomeFailed {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	if l.sentLagging != 0 {
		t.Errorf("%d clicks were sent while the view was lagging, the stop included", l.sentLagging)
	}
	if held := l.now.Sub(start); held < lagHoldMax || held > lagHoldMax+time.Minute {
		t.Errorf("the walk waited %v for the view, want about %v", held, lagHoldMax)
	}
	if !strings.Contains(joined(logs), "giving the walk up without a click") {
		t.Errorf("the give-up is not in the log:\n%s", joined(logs))
	}
}

// The lag is not walking: a walk with a 90-second budget that waits out a 60-second lag still arrives.
func TestTheWaitForAViewIsNotCountedAgainstTheWalksDeadline(t *testing.T) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	l.lagFrom, l.lagTo = l.now.Add(6*time.Second), l.now.Add(66*time.Second)
	res := w.WalkTo(Point{X: 90, Z: 1}, WalkOptions{Timeout: 90 * time.Second})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
}

// blockedStrip is a road the planner thinks is open and the server has walled off at x 10.
func blockedStrip(t *testing.T) (*lagWorld, *Walker, *[]string) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	for z := 0; z <= 2; z++ {
		l.move.wall(Point{X: 10, Z: z}, Point{X: 11, Z: z})
	}
	return l, w, logs
}

// 17 Oct, the second half: the walk ended unreachable and the last click's path stayed live on the server. An abandoned walk
// stops the account where it stands.
func TestAnAbandonedWalkStopsTheAccountWhereItStands(t *testing.T) {
	keep, wk, _ := blockedStrip(t)
	if res := wk.WalkTo(Point{X: 50, Z: 1}, WalkOptions{KeepPath: true}); res.Arrived() {
		t.Fatal("setup: the walled road was walked")
	}
	l, w, logs := blockedStrip(t)
	res := w.WalkTo(Point{X: 50, Z: 1}, WalkOptions{})
	if res.Arrived() {
		t.Fatal("setup: the walled road was walked")
	}
	if l.sent != keep.sent+1 {
		t.Fatalf("the abandoned walk sent %d clicks against %d with KeepPath: want exactly the stop more", l.sent, keep.sent)
	}
	if len(l.lastPath) != 1 || l.lastPath[0] != l.me {
		t.Errorf("the last click was %v at %s, want a one-tile path on the player's own tile", l.lastPath, l.me)
	}
	if !strings.Contains(joined(logs), "stopped the account on") {
		t.Errorf("the stop is not in the log:\n%s", joined(logs))
	}
}

func TestAWalkThatNeverClickedHasNothingToStop(t *testing.T) {
	l, w, _ := strip(t, Point{X: 0, Z: 1})
	l.plan.fill(30, 30, 32, 32, 0) // a sealed pocket: the first walk ends unreachable after its ladder's clicks
	if res := w.WalkTo(Point{X: 31, Z: 31}, WalkOptions{}); res.Outcome != OutcomeUnreachable {
		t.Fatalf("setup: result = %+v", res)
	}
	before := l.sent
	if res := w.WalkTo(Point{X: 31, Z: 31}, WalkOptions{}); res.Outcome != OutcomeUnreachable {
		t.Fatalf("setup: the repeat = %+v, want the unreachable memo", res)
	}
	if l.sent != before {
		t.Errorf("a walk refused by the unreachable memo (no click of its own) sent %d clicks, a stop among them", l.sent-before)
	}
}

func TestAWalkThatArrivedSendsNoStop(t *testing.T) {
	l, w, logs := strip(t, Point{X: 0, Z: 1})
	if res := w.WalkTo(Point{X: 30, Z: 1}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	k, wk, _ := strip(t, Point{X: 0, Z: 1})
	wk.WalkTo(Point{X: 30, Z: 1}, WalkOptions{KeepPath: true})
	if l.sent != k.sent {
		t.Errorf("an arrived walk sent %d clicks against %d with KeepPath: a stop went out", l.sent, k.sent)
	}
	if strings.Contains(joined(logs), "stopped the account") {
		t.Errorf("an arrived walk stopped the account:\n%s", joined(logs))
	}
}

// The ladder's blind rungs are refused where NoBlind says so, and the walk still ends as it did.
func TestNoBlindKeepsTheLadderFromClickingStraightAtTheDestination(t *testing.T) {
	f, w, _ := house(t, Point{X: 9, Z: 2})
	f.plan.fill(30, 30, 32, 32, 0) // a sealed pocket: the baked plan fails and the ladder runs
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	res := w.WalkTo(Point{X: 31, Z: 31}, WalkOptions{NoBlind: func(Point) bool { return true }})
	if res.Outcome != OutcomeUnreachable {
		t.Fatalf("result = %+v", res)
	}
	if f.walks != 0 {
		t.Errorf("the ladder sent %d blind clicks from ground NoBlind forbids", f.walks)
	}
	if n := strings.Count(strings.Join(logs, "\n"), "no blind clicks from"); n != 1 {
		t.Errorf("the refusal is in the log %d times, want once a walk:\n%s", n, strings.Join(logs, "\n"))
	}
	// And the same walk without the hook does click blind, so the test above proves the hook and not an idle ladder.
	g, wg, _ := house(t, Point{X: 9, Z: 2})
	g.plan.fill(30, 30, 32, 32, 0)
	wg.WalkTo(Point{X: 31, Z: 31}, WalkOptions{})
	if g.walks == 0 {
		t.Error("setup: the ladder without NoBlind sent no click either")
	}
}

// Watch runs at every wait of the walk, the ladder's included: eating must not wait for a straight leg's poll.
func TestWatchRunsAtEveryWaitOfTheLadder(t *testing.T) {
	f, w, _ := house(t, Point{X: 9, Z: 2})
	f.plan.fill(30, 30, 32, 32, 0)
	watched := 0
	w.WalkTo(Point{X: 31, Z: 31}, WalkOptions{Watch: func() { watched++ }})
	if f.delays == 0 || watched != f.delays {
		t.Errorf("Watch ran %d times for %d waits", watched, f.delays)
	}
	// The Walker's own Watch is the default, and a walk's overrides it.
	ff, ww, _ := house(t, Point{X: 9, Z: 2})
	ff.plan.fill(30, 30, 32, 32, 0)
	own, mine := 0, 0
	ww.Watch = func() { own++ }
	ww.WalkTo(Point{X: 31, Z: 31}, WalkOptions{})
	if own == 0 {
		t.Error("the Walker's Watch never ran")
	}
	ww.WalkTo(Point{X: 31, Z: 31}, WalkOptions{Watch: func() { mine++ }})
	if mine == 0 {
		t.Error("the walk's own Watch never ran")
	}
}

// A StepCost is a soft avoid: two equal roads, the dear one loses, and a road that is the only way is still walked.
func TestStepCostPrefersTheCheaperOfTwoEqualRoads(t *testing.T) {
	// Two rooms joined by a north corridor (z 6) and a south one (z 0), both the same length.
	col := newGrid().fill(0, 0, 0, 6, 0).fill(10, 0, 10, 6, 0).fill(1, 0, 9, 0, 0).fill(1, 6, 9, 6, 0)
	p := NewPlanner(col, nil)
	from, to := Point{X: 0, Z: 3}, Point{X: 10, Z: 3}
	plain := p.Plan(from, to, Options{WalkOnly: true})
	if !plain.OK {
		t.Fatalf("setup: %s", plain.Reason)
	}
	southDear := func(x, z, level int) int {
		if z < 3 && x > 0 && x < 10 {
			return 20
		}
		return 0
	}
	northDear := func(x, z, level int) int {
		if z > 3 && x > 0 && x < 10 {
			return 20
		}
		return 0
	}
	viaNorth := func(r Route) bool {
		for _, s := range r.Waypoints {
			if s.Z == 6 {
				return true
			}
		}
		return false
	}
	r1 := p.Plan(from, to, Options{WalkOnly: true, StepCost: southDear})
	r2 := p.Plan(from, to, Options{WalkOnly: true, StepCost: northDear})
	if !r1.OK || !r2.OK || !viaNorth(r1) || viaNorth(r2) {
		t.Fatalf("south dear: north=%v, north dear: north=%v (want true, false)", viaNorth(r1), viaNorth(r2))
	}
	// A road that is the only way is walked at its price.
	only := func(x, z, level int) int { return 5 }
	if r := p.Plan(from, to, Options{WalkOnly: true, StepCost: only}); !r.OK || r.Cost <= plain.Cost {
		t.Errorf("a uniformly dear road: ok=%v cost=%d against %d plain", r.OK, r.Cost, plain.Cost)
	}
}
