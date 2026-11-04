package mastercraftplan

import (
	"encoding/json"
	"testing"
	"time"
)

// tradesEvery records a trade every step from start for span, and returns the time of the last.
func tradesEvery(g *GateLedger, start time.Time, step, span time.Duration) time.Time {
	var last time.Time
	for at := start; !at.After(start.Add(span)); at = at.Add(step) {
		g.NoteTrade(24, at)
		last = at
	}
	return last
}

// A run of trades that lasted an hour and ended a few minutes ago is a good history; a short one, or one that ended long ago, is not.
func TestGateGoodHistoryIsAnHourOfTradesThatEndedRecently(t *testing.T) {
	var g GateLedger
	if g.GoodHistory(t0) {
		t.Fatal("an empty ledger has a good history")
	}
	last := tradesEvery(&g, t0, 30*time.Second, 59*time.Minute)
	if g.GoodHistory(last.Add(time.Minute)) {
		t.Error("59 minutes of trades is not an hour")
	}
	last = tradesEvery(&g, last.Add(30*time.Second), 30*time.Second, 2*time.Minute)
	if !g.GoodHistory(last.Add(time.Minute)) {
		t.Errorf("an hour of trades, the last one a minute ago, is a good history: run since %v, last %v", g.RunSince, g.LastTrade)
	}
	if !g.GoodHistory(last.Add(GateGoodFresh)) {
		t.Error("45 minutes after the last trade is still fresh")
	}
	if g.GoodHistory(last.Add(GateGoodFresh + time.Second)) {
		t.Error("a history that ended more than 45 minutes ago says nothing of a failure now")
	}
}

// A silence longer than GateRunGap starts the run again; a restart's ramp (eight minutes) does not.
func TestGateRunSurvivesARestartAndNotALongSilence(t *testing.T) {
	var g GateLedger
	last := tradesEvery(&g, t0, time.Minute, 50*time.Minute)
	last = tradesEvery(&g, last.Add(8*time.Minute), time.Minute, 15*time.Minute) // a relaunch in the middle
	if !g.RunSince.Equal(t0) || !g.GoodHistory(last.Add(time.Minute)) {
		t.Fatalf("a restart's ramp broke the run: since %v, last %v", g.RunSince, g.LastTrade)
	}
	resume := last.Add(GateRunGap + time.Minute)
	g.NoteTrade(24, resume)
	if !g.RunSince.Equal(resume) || g.GoodHistory(resume.Add(time.Minute)) {
		t.Errorf("a silence of %s did not start a new run: since %v", GateRunGap+time.Minute, g.RunSince)
	}
}

// A ledger written before RunSince existed (a LastTrade and no RunSince) counts its run from its first trade under this build, and
// loads without it.
func TestGateLedgerWithoutARunSinceLoads(t *testing.T) {
	var g GateLedger
	if err := json.Unmarshal([]byte(`{"trades":5,"lastTrade":"2026-10-03T11:59:00Z"}`), &g); err != nil {
		t.Fatal(err)
	}
	if !g.RunSince.IsZero() || g.GoodHistory(t0) {
		t.Fatalf("an old ledger has a history it never recorded: %+v", g)
	}
	g.NoteTrade(24, t0)
	if !g.RunSince.Equal(t0) {
		t.Errorf("run since %v, want the first trade under this build", g.RunSince)
	}
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back GateLedger
	if err := json.Unmarshal(data, &back); err != nil || !back.RunSince.Equal(t0) {
		t.Fatalf("RunSince did not survive the ledger file: %v %+v", err, back)
	}
}

// The failed round of a gate with a good history waits 5 minutes, then 10, 20, 40; a gate without one waits 30, 60, 120 as it always did.
func TestNatureBackoffIsShortForAGateWithAGoodHistory(t *testing.T) {
	set := orderSettings()
	var good Ledger
	last := tradesEvery(&good.Rune.Gate, t0, time.Minute, 90*time.Minute)
	now := last.Add(2 * time.Minute)
	var got []time.Duration
	for i := 0; i < 4; i++ {
		until := good.Fail(ModeNature, now, set)
		got = append(got, until.Sub(now))
	}
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("failure %d of a gate with a good history: %s, want %s", i+1, got[i], want[i])
		}
	}
	if good.Fails(ModeNature) != 4 {
		t.Errorf("%d failures counted", good.Fails(ModeNature))
	}
	var bad Ledger
	bad.Rune.Gate.NoteTrade(24, t0) // one trade is not an hour of them
	for i, w := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour} {
		until := bad.Fail(ModeNature, t0.Add(time.Minute), set)
		if until.Sub(t0.Add(time.Minute)) != w {
			t.Errorf("failure %d of a gate with no history: %s, want %s", i+1, until.Sub(t0.Add(time.Minute)), w)
		}
	}
	// Another mode keeps its wait whatever the gate's history.
	var other Ledger
	tradesEvery(&other.Rune.Gate, t0, time.Minute, 90*time.Minute)
	if until := other.Fail(ModeGems, now, set); until.Sub(now) != 30*time.Minute {
		t.Errorf("gems waited %s: the gate's history is the gate's", until.Sub(now))
	}
}

// The short wait is never longer than the operator's: a backoff set to 2 minutes stays 2.
func TestGateBackoffNeverExceedsTheConfiguredOne(t *testing.T) {
	set := orderSettings()
	set.Backoff, set.BackoffMax = 2*time.Minute, 6*time.Minute
	for fails, want := range map[int]time.Duration{0: 0, 1: 2 * time.Minute, 2: 4 * time.Minute, 3: 6 * time.Minute, 5: 6 * time.Minute} {
		if got := GateBackoffFor(fails, set); got != want {
			t.Errorf("after %d failure(s): %s, want %s", fails, got, want)
		}
	}
	// Past the cap the short wait follows the long one up to it and no further.
	set = orderSettings()
	for fails := 1; fails <= 8; fails++ {
		if short, long := GateBackoffFor(fails, set), BackoffFor(fails, set); short > long || short > set.BackoffMax {
			t.Errorf("after %d failure(s) the short wait %s exceeds the long %s", fails, short, long)
		}
	}
}

// The wait is reported with the reason it is the short one, for the log.
func TestLedgerWaitForSaysWhenItIsShort(t *testing.T) {
	set := orderSettings()
	var l Ledger
	last := tradesEvery(&l.Rune.Gate, t0, time.Minute, 90*time.Minute)
	if wait, short := l.WaitFor(ModeNature, 1, last.Add(time.Minute), set); !short || wait != 5*time.Minute {
		t.Errorf("%s %v", wait, short)
	}
	if wait, short := l.WaitFor(ModeNature, 1, last.Add(3*time.Hour), set); short || wait != 30*time.Minute {
		t.Errorf("a stale history: %s %v", wait, short)
	}
	var nilLed *Ledger
	if wait, short := nilLed.WaitFor(ModeNature, 2, t0, set); short || wait != time.Hour {
		t.Errorf("a nil ledger: %s %v", wait, short)
	}
}
