package randomevents

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestLegClockJudgesInTheServersTimeAsWellAsTheWalls(t *testing.T) {
	st := overlapState()
	interval := 100 * time.Millisecond
	t0 := time.Now()
	clock := newLegClock(st, t0)

	if clock.judgeable(st, t0.Add(interval/2), interval) {
		t.Fatal("judged before the interval")
	}
	// Ticks are not arriving at all (a client that does not count them, or a view about to be called silent): the wall decides.
	if !clock.judgeable(st, t0.Add(interval), interval) {
		t.Fatal("a leg with no tick at all was never judged")
	}
	// Ticks are arriving, slowly: two ticks in the interval is a slow server, not a refused step.
	advanceTicks(st, 2)
	if clock.judgeable(st, t0.Add(interval), interval) || clock.judgeable(st, t0.Add(2*interval), interval) {
		t.Fatal("judged a leg that the server had read for two ticks")
	}
	advanceTicks(st, fleeLegMinTicks-2)
	if !clock.judgeable(st, t0.Add(interval), interval) {
		t.Fatalf("not judged after %d ticks", fleeLegMinTicks)
	}
	// The valve: whatever the ticks say, three intervals is a judgement.
	slow := newLegClock(st, t0)
	advanceTicks(st, 1)
	if slow.judgeable(st, t0.Add(2*interval), interval) || !slow.judgeable(st, t0.Add(fleeLegValve*interval), interval) {
		t.Fatal("the valve is not fleeLegValve intervals")
	}
}

// walkerScene is the account on the stand tile with the angry plant on it, to drive fleeWalkingFrom (the session never moves).
func walkerScene(t *testing.T) *incidentScene {
	t.Helper()
	sc := newIncidentScene(t, 408, 97)
	hitSelf(sc.s.state, 4)
	sc.s.state.OverrideViewLag(&gamestate.ViewLag{})
	return sc
}

// The walks of the 18 Oct flee were judged in three seconds of wall time on a server that was ticking slowly, thrown away and sent
// again as another walk (which replaces the queue the server had not yet started). Under a slow server the leg is not judged before
// it has had fleeLegMinTicks game ticks.
func TestFleeWalkerDoesNotJudgeALegBeforeTheServerHasHadItsTicks(t *testing.T) {
	sc := walkerScene(t)
	st := sc.s.state
	fleeWalkLegInterval, fleeWalkDeadline = 60*time.Millisecond, 1200*time.Millisecond
	var stop atomic.Bool
	go func() { // a server that ticks every 25 ms: the 60 ms leg holds under three ticks, the valve (180 ms) seven
		for !stop.Load() {
			time.Sleep(25 * time.Millisecond)
			advanceTicks(st, 1)
			st.OverrideViewLag(&gamestate.ViewLag{})
		}
	}()
	defer stop.Store(true)

	var headTick, judgedTick int
	var headAt, judgedAt time.Time
	log := func(m string) {
		switch {
		case strings.Contains(m, "heading to") && headTick == 0:
			headTick, headAt = st.Tick(), time.Now()
		case strings.Contains(m, "moved 0 tile(s)") && judgedTick == 0:
			judgedTick, judgedAt = st.Tick(), time.Now()
		}
	}
	if _, err := fleeWalkingFrom(sc.s, sc.pack, 20, 1, log); err != nil {
		t.Fatal(err)
	}
	if headTick == 0 || judgedTick == 0 {
		t.Fatalf("the walker never headed out or never judged a leg (head %d, judged %d)", headTick, judgedTick)
	}
	// Judged on the ticks, or (a machine too busy to tick on time) on the valve; never sooner than both.
	if got, waited := judgedTick-headTick, judgedAt.Sub(headAt); got < fleeLegMinTicks && waited < fleeLegValve*fleeWalkLegInterval {
		t.Fatalf("the first leg was judged after %d game tick(s) and %s, before the server had its %d ticks or the valve's %s", got, waited, fleeLegMinTicks, fleeLegValve*fleeWalkLegInterval)
	}
}

// With no tick arriving at all the wall decides, as before.
func TestFleeWalkerJudgesOnTheWallWhenNoTicksArrive(t *testing.T) {
	sc := walkerScene(t)
	fleeWalkLegInterval, fleeWalkDeadline = 80*time.Millisecond, 600*time.Millisecond
	var lines []string
	if _, err := fleeWalkingFrom(sc.s, sc.pack, 20, 1, func(m string) { lines = append(lines, m) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "moved 0 tile(s)") {
		t.Fatalf("a stalled flee on a client whose ticks do not count was never judged:\n%s", strings.Join(lines, "\n"))
	}
}

// A view that is silent is not a judgement at all: no verdict on a tile that may be minutes old, no second walk, no bad destination.
func TestFleeWalkerHoldsEverythingWhileTheViewIsSilent(t *testing.T) {
	sc := walkerScene(t)
	sc.s.state.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 20 * time.Second})
	fleeWalkLegInterval, fleeWalkDeadline, fleeLagHoldMax = 60*time.Millisecond, 300*time.Millisecond, 700*time.Millisecond
	t.Cleanup(func() { fleeLagHoldMax = 40 * time.Second })
	var lines []string
	start := time.Now()
	if _, err := fleeWalkingFrom(sc.s, sc.pack, 20, 1, func(m string) { lines = append(lines, m) }); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "moved") || strings.Contains(text, "marking it bad") {
		t.Fatalf("a leg was judged while the view was silent:\n%s", text)
	}
	if sc.s.count(clientprot.MOVE_GAMECLICK) != 1 {
		t.Fatalf("%d walks went out, want the one", sc.s.count(clientprot.MOVE_GAMECLICK))
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Fatalf("the flee gave up after %s: the deadline is pushed out by the time spent waiting on a lagging view", time.Since(start))
	}
}
