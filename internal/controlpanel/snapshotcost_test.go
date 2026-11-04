package controlpanel

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
)

// countingFleetSource is a FleetSource whose underlying fleet snapshot is counted.
//
// The count is the thing worth measuring here, not wall-clock time: one
// fleet.Manager.Snapshot walks every account, and for every CONNECTED one takes the
// session's gamestate lock about eight separate times (Self, Modals, Tile,
// KnownInventories, HP, MaxHP, IsDead, Skills) while copying its inventories, skills, and
// its last 50 log and 50 chat lines. That lock is the same one the account's own read loop
// holds while decoding packets, so the cost lands on the live fleet, not just on the
// dashboard. How long that takes depends on the machine; how many times we ask for it does
// not, which is why these tests assert call counts.
func countingFleetSource(t *testing.T, accounts int) (*FleetSource, *int64) {
	t.Helper()
	snap := make([]fleet.AccountStatus, accounts)
	roster := make([]string, accounts)
	for i := range snap {
		name := fmt.Sprintf("swarm%03d", i)
		snap[i] = fleet.AccountStatus{Username: name, Connected: true}
		roster[i] = name
	}
	var calls int64
	src := newFleetSource(func() []fleet.AccountStatus {
		atomic.AddInt64(&calls, 1)
		return snap
	}, roster, time.Second)
	src.poll = 0
	return src, &calls
}

// The live roster this was measured against.
const liveRoster = 266

// One /api/status must cost ONE fleet snapshot.
//
// It used to cost two: Accounts() and Logins() each took their own, and handleStatus calls
// both. At the live roster that is a second full pass over 266 accounts, and the hub takes
// one of these every second while any browser is open.
//
// The clock is the source's own, stepped 10ms per read rather than the wall clock. On the
// wall clock this failed under heavy CPU load (3 in 124 loaded runs of the package): the
// first Accounts() over 266 accounts also records every first sighting and emits it to the
// hub, and a descheduled goroutine could take longer than the 250ms window before Logins()
// asked, so Logins() correctly took a fresh snapshot. What this asserts is that the two
// reads of ONE request share a pass, not how fast this machine runs them. Stepping the
// clock (rather than freezing it) keeps the check honest about the window: a request whose
// handful of clock reads spans tens of milliseconds must still fit inside it.
func TestStatusTakesOneFleetSnapshot(t *testing.T) {
	src, calls := countingFleetSource(t, liveRoster)
	var clockMu sync.Mutex
	now := time.Unix(1_700_000_000, 0)
	src.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		now = now.Add(10 * time.Millisecond)
		return now
	}
	h := newHarness(t, Config{}, testOptions(), src)

	atomic.StoreInt64(calls, 0)
	res := h.get("/api/status")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/status = %d", res.StatusCode)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Errorf("/api/status took %d fleet snapshots, want 1 — each one walks all %d accounts and takes every live session's gamestate lock ~8 times", got, liveRoster)
	}
}

// One /api/bank-all must cost ONE fleet snapshot, not one per account.
//
// This is the measurement that explains CLAUDE.md's "status requests intermittently time
// out". handleBankAll asked the Source for every account (one snapshot) and then called
// Bank(name) for each of them — and FleetSource.Bank answered by taking ANOTHER whole-fleet
// snapshot and scanning it for that one name. At the live roster that is 1 + 266 = 267 full
// snapshots for a single request, roughly seventy thousand account visits, from a panel
// that refreshes every five seconds. /api/status needs the same locks and simply queued
// behind it.
func TestBankAllTakesOneFleetSnapshot(t *testing.T) {
	src, calls := countingFleetSource(t, liveRoster)
	// The clock jumps a full window on every read. This is what a REAL fleet looks like
	// to the coalescing window: a snapshot over 266 live sessions is not free, so a loop
	// that takes one per account outruns the window and keeps re-snapshotting. Without
	// this the test would pass purely because the in-memory fake finishes 266 iterations
	// inside 250ms, and would say nothing about production.
	now := time.Unix(1_700_000_000, 0)
	src.now = func() time.Time {
		now = now.Add(defaultSnapshotTTL)
		return now
	}
	h := newHarness(t, Config{}, testOptions(), src)

	atomic.StoreInt64(calls, 0)
	res := h.get("/api/bank-all")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/bank-all = %d", res.StatusCode)
	}
	got := atomic.LoadInt64(calls)
	if got > 1 {
		t.Errorf("/api/bank-all took %d fleet snapshots for %d accounts, want 1 — this is the O(accounts^2) refresh that starves /api/status of the same locks", got, liveRoster)
	}
}

// A per-account read must not pay for the whole fleet either. Same bug, smaller blast
// radius: opening one account's bank or inventory panel scanned all 266.
func TestPerAccountReadsTakeOneFleetSnapshot(t *testing.T) {
	src, calls := countingFleetSource(t, liveRoster)
	h := newHarness(t, Config{}, testOptions(), src)

	for _, path := range []string{"/api/bank?account=swarm004", "/api/inventory?account=swarm004"} {
		atomic.StoreInt64(calls, 0)
		if res := h.get(path); res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", path, res.StatusCode)
		}
		// At most one: zero is correct and expected when an earlier request in the same
		// coalescing window already took one.
		if got := atomic.LoadInt64(calls); got > 1 {
			t.Errorf("GET %s took %d fleet snapshots, want at most 1", path, got)
		}
	}
}

// The coalescing window must genuinely expire. A cache that never refreshed would make the
// dashboard show a frozen fleet, which is a far worse bug than the one it replaced — so
// this asserts both halves: repeated reads inside the window share one pass, and a read
// after it has elapsed takes a fresh one.
func TestSnapshotWindowCoalescesThenExpires(t *testing.T) {
	src, calls := countingFleetSource(t, 8)
	now := time.Unix(1_700_000_000, 0)
	src.now = func() time.Time { return now }
	src.snapTTL = 250 * time.Millisecond

	atomic.StoreInt64(calls, 0)
	for i := 0; i < 20; i++ {
		src.Accounts(nil)
		src.Logins()
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Errorf("40 reads inside one window took %d snapshots, want 1", got)
	}

	now = now.Add(src.snapTTL)
	src.Accounts(nil)
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("a read after the window elapsed took a total of %d snapshots, want 2 — the window must expire or the dashboard freezes", got)
	}
}

// The window must be short enough that it can only ever collapse work that was already
// redundant. If it ever grew past the hub's own sampling tick it would start hiding real
// changes from the event stream rather than just saving duplicate passes.
func TestSnapshotWindowIsShorterThanTheHubTick(t *testing.T) {
	if defaultSnapshotTTL >= defaultOptions().tick {
		t.Errorf("snapshot window %s is not shorter than the hub's %s sampling tick — it would start making the dashboard stale rather than only removing duplicate work",
			defaultSnapshotTTL, defaultOptions().tick)
	}
}
