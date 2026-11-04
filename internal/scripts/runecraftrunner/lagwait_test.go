package runecraftrunner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// lagFor puts the view in the state the test names for a stretch of real time, then makes it current again: Lagging (silent) from
// `from` to `to` after it was called.
func lagFor(st *gamestate.State, from, to time.Duration) {
	go func() {
		time.Sleep(from)
		st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 5 * time.Second})
		time.Sleep(to - from)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
}

// 18 Oct 2026 05:30: three crafts in a row were "not seen" six seconds after the click, in a view that went silent for ten seconds and
// read backlogs for the next minute. A craft that lands late in a view that lags is a craft, not a failure, and the click is not sent
// a second time to meet an empty pack.
func TestACraftThatLandsLateInALaggingViewIsNotAFailure(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.mu.Lock()
	e.craftLateTicks = 52 // 1.3 s: twice the 0.6 s a click is waited for
	e.mu.Unlock()
	lagFor(e.sim.State(), 100*time.Millisecond, 1000*time.Millisecond)
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Essence != 27 || res.Left != 0 || res.XPGain != 135 {
		t.Fatalf("a craft that landed late: %+v", res)
	}
	if !res.Lag {
		t.Error("the result does not say the view lagged")
	}
	e.mu.Lock()
	clicks := e.altarClicks
	e.mu.Unlock()
	if res.Clicks != 1 || clicks != 1 {
		t.Errorf("the altar was clicked %d times (%d read): a late answer is not a reason to click again", res.Clicks, clicks)
	}
}

// The same late craft in a view that is current is the failure it always was, and the click is sent again once.
func TestTheSameLateCraftInACurrentViewStillFails(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.mu.Lock()
	e.craftLateTicks = 200 // 5 s
	e.mu.Unlock()
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if res.OK() || !strings.Contains(res.Reason, "still in the pack") || res.Lag {
		t.Fatalf("a craft that did not land in a current view: %+v", res)
	}
	if strings.Contains(res.Reason, "view was lagging") {
		t.Errorf("a failure in a current view blames the view: %q", res.Reason)
	}
	if res.Clicks != 2 {
		t.Errorf("clicks = %d, want the resend once", res.Clicks)
	}
}

// A view that went silent a few seconds ago is shaky: its waits are three times as long, whatever it reports now.
func TestAShakyViewWaitsThreeTimesAsLong(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), runecraftplan.Air.Enter, 1)
	e.mu.Lock()
	e.craftLateTicks = 60 // 1.5 s: past craftWait/2 (0.6 s), inside three times that (1.8 s)
	e.mu.Unlock()
	e.sim.State().OverrideViewLag(&gamestate.ViewLag{Silence: 8 * time.Second, SilenceEnded: time.Now().Add(-10 * time.Second)})
	if !Shaky(d) {
		t.Fatal("setup: a view that was silent ten seconds ago is not shaky")
	}
	res := Craft(d, runecraftplan.Air, airAltarMatch())
	if !res.OK() || res.Clicks != 1 || !res.Lag {
		t.Fatalf("a craft that landed inside a shaky view's wait: %+v", res)
	}
}

func TestShakyIsLaggingNowOrSilentInTheLastMinute(t *testing.T) {
	_, d := newEngine(t, runecraftplan.Air, nil, runecraftplan.Air.Enter, 1)
	st := d.Session.State()
	for _, c := range []struct {
		name  string
		l     gamestate.ViewLag
		shaky bool
	}{
		{"current, never silent", gamestate.ViewLag{}, false},
		{"lagging now", gamestate.ViewLag{Lagging: true, CatchingUp: true, InWindow: 14}, true},
		{"silent 10 s ago", gamestate.ViewLag{Silence: 6 * time.Second, SilenceEnded: time.Now().Add(-10 * time.Second)}, true},
		{"silent 2 minutes ago", gamestate.ViewLag{Silence: 6 * time.Second, SilenceEnded: time.Now().Add(-2 * time.Minute)}, false},
	} {
		l := c.l
		st.OverrideViewLag(&l)
		if got := Shaky(d); got != c.shaky {
			t.Errorf("%s: Shaky = %v, want %v", c.name, got, c.shaky)
		}
	}
}

// The talisman's teleport lands late in a view that lags: the use is not sent twice and the result says why the wait was long.
func TestATalismanThatLandsLateInALaggingViewIsNotAFailure(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	e.mu.Lock()
	e.teleportLateTicks = 80 // 2 s past the usual two
	e.mu.Unlock()
	lagFor(e.sim.State(), 100*time.Millisecond, 1200*time.Millisecond)
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if !res.Arrived || res.At != runecraftplan.Air.Enter || res.Clicks != 1 || !res.Lag {
		t.Fatalf("%+v", res)
	}
}

// A failure out of a view that lags carries it, so the stage above can excuse it: nothing is said about the click.
func TestAFailedTalismanInALaggingViewSaysSo(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	e.mu.Lock()
	e.teleportLateTicks = 400 // 10 s: never inside the waits
	e.mu.Unlock()
	lagFor(e.sim.State(), 50*time.Millisecond, 800*time.Millisecond)
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if res.Arrived || !res.Lag || !strings.Contains(res.Reason, "not seen inside") || !strings.Contains(res.Reason, "view was lagging") {
		t.Fatalf("%+v", res)
	}
	if res.Clicks != 1 {
		t.Errorf("the use was sent %d times: the first one's result was not seen in a view that lags", res.Clicks)
	}
}

// ---- waitSeen itself ----

func drivenDeps(t *testing.T) (*actionsim.Session, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(objs(), nil)
	return sim, &fleetrunner.Deps{Session: sim, Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
}

// tickEvery ticks the session every period until the test ends.
func tickEvery(t *testing.T, sim *actionsim.Session, period time.Duration) {
	t.Helper()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk := time.NewTicker(period)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				sim.Tick()
			}
		}
	}()
	t.Cleanup(func() { close(done); wg.Wait() })
}

// A server that ticks slowly is not silent (no gap of three seconds), but its budget of seconds is a few ticks of game time: the wait
// goes on until the ticks it was worth have come.
func TestAWaitIsNotOverUntilTheTicksItWasWorthHaveCome(t *testing.T) {
	old := tickPace
	tickPace = 100 * time.Millisecond // a 1.2 s budget is worth 11 ticks
	t.Cleanup(func() { tickPace = old })
	sim, d := drivenDeps(t)
	tickEvery(t, sim, 250*time.Millisecond) // four ticks a second
	st := sim.State()
	tick0 := st.Tick()
	ok, note := waitSeen(d, func() bool { return st.Tick()-tick0 >= 6 }, 1200*time.Millisecond)
	if !ok {
		t.Fatal("the wait ended at its wall-clock budget with the server's ticks still to come")
	}
	if !note.lagged {
		t.Error("a wait stretched for slow ticks does not say the view was slow")
	}
	// The same wait on a server that keeps its pace ends at the budget.
	sim2, d2 := drivenDeps(t)
	tickEvery(t, sim2, 25*time.Millisecond)
	started := time.Now()
	ok, note = waitSeen(d2, func() bool { return false }, 1200*time.Millisecond)
	if ok || note.any() {
		t.Errorf("a wait that nothing satisfied on a healthy view: ok %v note %+v", ok, note)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("a healthy view's wait took %v for a 1.2 s budget", took)
	}
}

// A session that sees no ticks at all is not waited for past the budget (a session that is not driven, or a connection that is gone
// and has not yet been called silent).
func TestAWaitOnASessionWithoutTicksEndsAtItsBudget(t *testing.T) {
	_, d := drivenDeps(t)
	started := time.Now()
	ok, _ := waitSeen(d, func() bool { return false }, 300*time.Millisecond)
	if ok {
		t.Fatal("nothing satisfied the wait")
	}
	if took := time.Since(started); took > time.Second {
		t.Errorf("took %v for a 0.3 s budget", took)
	}
}

// A view that stays silent is waited for, but not for ever.
func TestAWaitForAViewThatNeverComesBackIsBounded(t *testing.T) {
	old := lagWaitMax
	lagWaitMax = 400 * time.Millisecond
	t.Cleanup(func() { lagWaitMax = old })
	sim, d := drivenDeps(t)
	sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: time.Minute})
	started := time.Now()
	ok, note := waitSeen(d, func() bool { return false }, 200*time.Millisecond)
	took := time.Since(started)
	if ok || !note.lagged {
		t.Errorf("ok %v note %+v", ok, note)
	}
	if took < 500*time.Millisecond || took > 2*time.Second {
		t.Errorf("took %v, want the budget plus lagWaitMax (0.6 s)", took)
	}
}

// The seconds a view spends lagging are not charged to the wait: the budget starts when it is current.
func TestTheSecondsOfALagAreNotChargedToTheWait(t *testing.T) {
	sim, d := drivenDeps(t)
	lagFor(sim.State(), 0, 800*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	landed := time.Now().Add(1100 * time.Millisecond)
	started := time.Now()
	ok, note := waitSeen(d, func() bool { return time.Now().After(landed) }, 600*time.Millisecond)
	if !ok || !note.lagged {
		t.Fatalf("a result 1.1 s after the click, 0.8 s of it lagging, with a 0.6 s budget: ok %v note %+v after %v", ok, note, time.Since(started))
	}
}

func TestAwaitViewWaitsForALaggingViewAndOnlyThen(t *testing.T) {
	sim, d := drivenDeps(t)
	if AwaitView(d) {
		t.Error("a current view was waited for")
	}
	sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, CatchingUp: true, InWindow: 12})
	go func() {
		time.Sleep(200 * time.Millisecond)
		sim.State().OverrideViewLag(&gamestate.ViewLag{})
	}()
	started := time.Now()
	if !AwaitView(d) {
		t.Error("a lagging view was not waited for")
	}
	if took := time.Since(started); took < 150*time.Millisecond || took > 2*time.Second {
		t.Errorf("waited %v for a view that came back after 0.2 s", took)
	}
}

// A use whose arrival was not seen in a view that lags may still land, and the Guardian is still told to expect the landing; a use
// that failed in a current view announces nothing.
func TestALandingThatIsNotSeenInALaggingViewStaysAnnounced(t *testing.T) {
	e, d := newEngine(t, runecraftplan.Air, airKit(27), airRuinsStand, 1)
	e.mu.Lock()
	e.teleportLateTicks = 400
	e.mu.Unlock()
	lagFor(e.sim.State(), 50*time.Millisecond, 800*time.Millisecond)
	res := UseTalisman(d, runecraftplan.Air, airRuinsMatch())
	if res.Arrived || !res.Lag {
		t.Fatalf("setup: %+v", res)
	}
	if got := randomevents.ExpectedJumps(d.Session); len(got) != 1 {
		t.Errorf("announced jumps after a use that may still land = %v, want the one", got)
	}
	_, d2 := newEngine(t, runecraftplan.Air, airKit(27), airRuinsBad, 1)
	if res := UseTalisman(d2, runecraftplan.Air, airRuinsMatch()); res.Arrived || res.Lag {
		t.Fatalf("setup: %+v", res)
	}
	if got := randomevents.ExpectedJumps(d2.Session); len(got) != 0 {
		t.Errorf("announced jumps after a refused use = %v, want none", got)
	}
}
