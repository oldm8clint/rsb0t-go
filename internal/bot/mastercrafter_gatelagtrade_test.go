package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// CR-6 of docs/xp_rate_plan_2026-10-19.md. 18 Oct 2026 19:46-19:52 ten trades in a row failed while the view went silent every few
// seconds; the gate stage failed three times, the round ended, the crafter walked home and the nature rounds backed off for an hour.
// A trade that failed in a lagging view is not counted toward ending the round (up to natureLagTrades in a row); one that failed in a
// current view is, as it always was.

func lagTradeWorld(t *testing.T, next *runecraftrunner.GateResult) (*masterRig, *masterNatureWorld) {
	t.Helper()
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, nil)
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return *next
	}
	t.Cleanup(func() { masterGateStep = runecraftrunner.GateStep })
	return r, w
}

func TestNatureGateTradesFailedInALaggingViewAreNotCounted(t *testing.T) {
	next := runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "the confirm screen does not match the offer", Lag: true}
	r, w := lagTradeWorld(t, &next)
	for i := 1; i <= natureLagTrades; i++ {
		if res := w.Gate(); !res.OK {
			t.Fatalf("lagging failure %d ended the stage: %+v", i, res)
		}
		if w.failedTrades != 0 || w.lagTrades != i {
			t.Fatalf("after %d: %d counted, %d excused", i, w.failedTrades, w.lagTrades)
		}
	}
	if !r.logged("failed in a lagging view (1 of 30 excused, not counted toward ending the round)") || r.logged("WARNING: trade with") {
		t.Fatalf("log: %v", r.logs)
	}
	// A view that never recovers still ends the round, a little later: the excuses run out and the failures count.
	for i := 1; i < natureTradeFails; i++ {
		if res := w.Gate(); !res.OK || w.failedTrades != i || w.lagTrades != natureLagTrades {
			t.Fatalf("past the excuses, failure %d: %+v (%d counted, %d excused)", i, res, w.failedTrades, w.lagTrades)
		}
	}
	if res := w.Gate(); res.OK || !strings.Contains(res.Why, "trades in a row failed") {
		t.Fatalf("the failure after the excuses ran out should end the stage: %+v", res)
	}
}

// A failure in a current view is counted beside the excused ones and is not hidden by them.
func TestNatureGateCountsTheFailuresALagDidNotCause(t *testing.T) {
	next := runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "the trade never closed"}
	_, w := lagTradeWorld(t, &next)
	for i := 1; i < natureTradeFails; i++ {
		next.Lag = false
		if res := w.Gate(); !res.OK || w.failedTrades != i {
			t.Fatalf("failure %d: %+v (%d counted)", i, res, w.failedTrades)
		}
		next.Lag = true
		if res := w.Gate(); !res.OK || w.failedTrades != i {
			t.Fatalf("lagging failure after %d: %+v (%d counted)", i, res, w.failedTrades)
		}
	}
	next.Lag = false
	if res := w.Gate(); res.OK {
		t.Fatalf("the sixth failure in a current view should end the stage: %+v", res)
	}
}

// A trade that worked clears both counts: the next lag has its excuses again.
func TestNatureGateATradeThatWorkedClearsTheExcuses(t *testing.T) {
	next := runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "x", Lag: true}
	_, w := lagTradeWorld(t, &next)
	for i := 0; i < 5; i++ {
		w.Gate()
	}
	if w.lagTrades != 5 {
		t.Fatalf("%d excused", w.lagTrades)
	}
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25}
	if res := w.Gate(); !res.OK || w.lagTrades != 0 || w.failedTrades != 0 {
		t.Fatalf("%+v: %d excused, %d counted", res, w.lagTrades, w.failedTrades)
	}
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateGave, Partner: "Acct 2", Natures: 3000}
	w.lagTrades, w.failedTrades = 7, 3
	if res := w.Gate(); !res.OK || w.lagTrades != 0 || w.failedTrades != 0 {
		t.Fatalf("a courier trade: %+v: %d excused, %d counted", res, w.lagTrades, w.failedTrades)
	}
}

// The walk back to the gate carries the view's lag, like every other stage that moves the account.
func TestNatureGateWalkBackThatFailsInALaggingViewSaysSo(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}}, nil, nil)
	r.sim.StandAt(3222, 3218) // Lumbridge
	w := newNatureWorld(t, r, nil)
	masterWalkKaramja = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { return false }
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	if res := w.Gate(); res.OK || res.Lag {
		t.Fatalf("a failed walk back in a current view: %+v", res)
	}
	r.sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
	if res := w.Gate(); res.OK || !res.Lag {
		t.Fatalf("a failed walk back in a lagging view: %+v", res)
	}
}

// ---- the short backoff of a gate with a good history ----

// The round that failed after an hour of trades waits five minutes and says why; with no history it waits as it always did.
func TestFailModeIsShortForAGateWithAGoodHistory(t *testing.T) {
	r := newMasterRig(t)
	now := time.Now()
	for at := now.Add(-100 * time.Minute); at.Before(now.Add(-3 * time.Minute)); at = at.Add(time.Minute) {
		r.b.ledger.Rune.Gate.NoteTrade(24, at)
	}
	r.b.failMode(r.ctx, mastercraftplan.ModeNature, now, "trade failed 3 times in a row")
	until, backed := r.b.ledger.BackedOff(mastercraftplan.ModeNature, now)
	if !backed || until.Sub(now) != 5*time.Minute {
		t.Fatalf("backed off until %v (%v), want 5 minutes", until.Sub(now), backed)
	}
	if !r.logged("backing off until") || !r.logged("(5m0s) [the gate traded for") || !r.logged("the short backoff, not 30m0s") {
		t.Fatalf("log: %v", r.logs)
	}
	// A second failure in a row waits ten.
	r.b.failMode(r.ctx, mastercraftplan.ModeNature, now, "again")
	if until, _ := r.b.ledger.BackedOff(mastercraftplan.ModeNature, now); until.Sub(now) != 10*time.Minute {
		t.Fatalf("second failure waits %v", until.Sub(now))
	}

	r2 := newMasterRig(t)
	r2.b.failMode(r2.ctx, mastercraftplan.ModeNature, now, "no history")
	if until, _ := r2.b.ledger.BackedOff(mastercraftplan.ModeNature, now); until.Sub(now) != 30*time.Minute {
		t.Fatalf("no history: waits %v", until.Sub(now))
	}
	if !r2.logged("(30m0s) —") || r2.logged("the short backoff") {
		t.Fatalf("log: %v", r2.logs)
	}
}
