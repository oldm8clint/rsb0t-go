package controlpanel

import (
	"path/filepath"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestStatsAggregatorAccumulatesEdgesNotLevels proves every counter is edge-triggered
// (counts distinct occurrences) rather than mirroring a current/live value — the whole
// point of an *accumulating* stat is that it does not reset just because the underlying
// state flips back.
func TestStatsAggregatorAccumulatesEdgesNotLevels(t *testing.T) {
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))

	// Death, then alive again, then dead again: two edges, not one, not three.
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, base)
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, base.Add(10*time.Second)) // still dead: no new edge
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: false}}, base.Add(20*time.Second))
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, base.Add(30*time.Second))

	rec := a.accounts["acct1"]
	if rec == nil {
		t.Fatal("acct1 not tracked")
	}
	if rec.Deaths != 2 {
		t.Fatalf("Deaths = %d, want 2", rec.Deaths)
	}
}

// TestStatsAggregatorCountsReconnectEpisodesNotAttempts proves Reconnects counts distinct
// reconnect EPISODES (an edge into StateReconnecting) rather than every sample where the
// account happens to still be reconnecting — a flapping connection retried 5 times before
// succeeding is 1 episode, not 5.
func TestStatsAggregatorCountsReconnectEpisodesNotAttempts(t *testing.T) {
	base := time.Now()
	a := newStatsAggregator("", nil, fixedNow(base))

	states := []State{StateIngame, StateReconnecting, StateReconnecting, StateReconnecting, StateIngame, StateReconnecting, StateIngame}
	for i, st := range states {
		a.recordSample([]AccountStatus{{Name: "acct1", State: st}}, base.Add(time.Duration(i)*time.Second))
	}
	if got := a.accounts["acct1"].Reconnects; got != 2 {
		t.Fatalf("Reconnects = %d, want 2 (two distinct episodes)", got)
	}
}

// TestStatsAggregatorRandomEventsDedupeByTimestamp proves a random event already counted
// (by its own At timestamp) is never double-counted on a later poll even though
// AccountStatus.RandomEvents is a rolling window that keeps re-reporting recent entries
// every poll — and that each outcome lands in its own bucket.
//
// This test used to assert that a "still_present" outcome incremented RandomsFailed, which
// is the behaviour that made the dashboard report 17,802 failed random events against 6,963
// cleared (2026-09-20). "Still present after handling" is the guardian releasing the account
// to retry the SAME event instance — one live mime logged it 13 times before clearing — so
// it is now counted as a retry, and only a handler error or a final give-up is a failure.
func TestStatsAggregatorRandomEventsDedupeByTimestamp(t *testing.T) {
	base := time.Now()
	a := newStatsAggregator("", nil, fixedNow(base))

	events := []RandomEventOutcome{
		{Name: "Strange Plant", Outcome: "cleared", At: "2026-09-16T10:00:00Z"},
	}
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, RandomEvents: events}}, base)
	// Same window reported again next poll (newest-first, unchanged) plus one genuinely new entry.
	events2 := []RandomEventOutcome{
		{Name: "Mime", Outcome: "error", At: "2026-09-16T10:15:00Z"},
		{Name: "Genie", Outcome: "not_ours", At: "2026-09-16T10:10:00Z"},
		{Name: "Genie", Outcome: "retry", At: "2026-09-16T10:05:00Z"},
		{Name: "Strange Plant", Outcome: "cleared", At: "2026-09-16T10:00:00Z"},
	}
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, RandomEvents: events2}}, base.Add(time.Second))

	rec := a.accounts["acct1"]
	if rec.RandomsCleared != 1 {
		t.Fatalf("RandomsCleared = %d, want 1 (not double-counted)", rec.RandomsCleared)
	}
	if rec.RandomsFailed != 1 {
		t.Fatalf("RandomsFailed = %d, want 1 (only the handler error is a failure)", rec.RandomsFailed)
	}
	if rec.RandomsRetried != 1 {
		t.Fatalf("RandomsRetried = %d, want 1 (a retry must not be counted as a failure)", rec.RandomsRetried)
	}
	if rec.RandomsNotOurs != 1 {
		t.Fatalf("RandomsNotOurs = %d, want 1 (another player's event is not our failure)", rec.RandomsNotOurs)
	}
}

// TestStatsAggregatorXPBaselineSurvivesRestart is this feature's core promise: an
// account's first-ever observed XP is persisted once as a floor, so "XP gained" stays
// correct (current minus that floor) across a restart instead of re-baselining to
// whatever XP happens to be current the moment tracking resumes and silently discarding
// everything earned before that restart.
func TestStatsAggregatorXPBaselineSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Now()

	a1 := newStatsAggregator(path, nil, fixedNow(base))
	a1.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Skills: map[string]SkillCounter{"Firemaking": {Level: 27, XP: 10_000}}}}, base)
	a1.close() // flushes to disk, same as Server.Shutdown calling it

	// A fresh aggregator over the same file, simulating a process restart. The account
	// now reports more XP than at first observation, as it genuinely would have earned
	// more in between.
	a2 := newStatsAggregator(path, nil, fixedNow(base))
	a2.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Skills: map[string]SkillCounter{"Firemaking": {Level: 40, XP: 250_000}}}}, base.Add(time.Hour))

	got := a2.snapshot()
	if len(got.Accounts) != 1 {
		t.Fatalf("Accounts = %+v, want 1 entry", got.Accounts)
	}
	view := got.Accounts[0]
	const wantGained = int64(250_000 - 10_000)
	if view.XPGained != wantGained {
		t.Fatalf("XPGained after restart = %d, want %d (baseline must survive the restart, not reset to 250000-250000=0)", view.XPGained, wantGained)
	}
	if view.LevelsGained != 40-27 {
		t.Fatalf("LevelsGained after restart = %d, want %d", view.LevelsGained, 40-27)
	}
}

// TestStatsAggregatorSinceRestartIsIndependentOfLifetime proves the second half of the
// "both persisted lifetime AND a since-restart figure" design: after a simulated
// restart, Lifetime keeps everything earned before it, while SinceRestart starts back at
// (approximately) zero for this new process instance rather than inheriting the old
// totals — an operator watching "since restart" after a deploy should see this
// process's own contribution, not the whole fleet's history.
func TestStatsAggregatorSinceRestartIsIndependentOfLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	base := time.Now()

	a1 := newStatsAggregator(path, nil, fixedNow(base))
	for i := 0; i < 3; i++ {
		a1.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, base.Add(time.Duration(i)*time.Second))
		a1.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: false}}, base.Add(time.Duration(i)*time.Second+500*time.Millisecond))
	}
	a1.close()
	lifetimeBefore := a1.snapshot().Lifetime
	if lifetimeBefore.Deaths != 3 {
		t.Fatalf("setup: Deaths before restart = %d, want 3", lifetimeBefore.Deaths)
	}

	a2 := newStatsAggregator(path, nil, fixedNow(base.Add(time.Hour)))
	a2.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, base.Add(time.Hour))

	got := a2.snapshot()
	if got.Lifetime.Deaths != 4 {
		t.Fatalf("Lifetime.Deaths after restart + 1 new death = %d, want 4 (3 carried over + 1)", got.Lifetime.Deaths)
	}
	if got.SinceRestart.Deaths != 1 {
		t.Fatalf("SinceRestart.Deaths = %d, want 1 (only this process's own death, not the 3 from before the restart)", got.SinceRestart.Deaths)
	}
}

// TestStatsAggregatorMemoryOnlyWhenPathEmpty proves the "empty path disables the file"
// convention this package borrows from fleet.bankStore/layoutStore: no path means no
// panic, no error, just an aggregator that accumulates for this process's own lifetime
// and nothing survives a restart, same as those two existing features' own memory-only
// fallback.
func TestStatsAggregatorMemoryOnlyWhenPathEmpty(t *testing.T) {
	a := newStatsAggregator("", nil, fixedNow(time.Now()))
	a.recordSample([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true}}, time.Now())
	a.save()  // must not panic or error out with no path configured
	a.close() // ditto
	if got := a.snapshot().Lifetime.Deaths; got != 1 {
		t.Fatalf("Lifetime.Deaths = %d, want 1 (memory-only still accumulates within one process)", got)
	}
}

// TestStatsAggregatorLoadFromMissingFileStartsEmpty mirrors
// TestBankStoreLoadFromMissingFileStartsEmptyNotError's own coverage for the identical
// convention here: a configured path that doesn't exist yet (first run) starts empty,
// not erroring.
func TestStatsAggregatorLoadFromMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	a := newStatsAggregator(path, nil, fixedNow(time.Now()))
	if got := a.snapshot(); len(got.Accounts) != 0 {
		t.Fatalf("Accounts = %+v, want none", got.Accounts)
	}
}
