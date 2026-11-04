package controlpanel

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
)

// countingPlayersSource is countingFleetSource (snapshotcost_test.go) with the seen-players
// read counted too. Counted, not timed, for the same reason: one read takes every
// connected session's gamestate lock once, and how many reads we ask for is what matters.
func countingPlayersSource(t *testing.T) (*FleetSource, *int64, *int64, *time.Time) {
	t.Helper()
	src, snapshots := countingFleetSource(t, liveRoster)
	var reads int64
	src.seen.read = func() fleet.SeenPlayers {
		atomic.AddInt64(&reads, 1)
		return fleet.SeenPlayers{Players: []fleet.SeenPlayer{{Name: "Stranger", X: 2810, Z: 3440, SeenBy: 3}}, Observers: liveRoster}
	}
	now := time.Unix(1_700_000_000, 0)
	src.now = func() time.Time { return now }
	return src, snapshots, &reads, &now
}

// One /api/players costs ONE pass over the fleet's player views and ZERO status snapshots,
// and repeated requests inside the window share it.
func TestSeenPlayersTakesOneReadAndNoSnapshot(t *testing.T) {
	src, snapshots, reads, now := countingPlayersSource(t)
	h := newHarness(t, Config{}, testOptions(), src)
	atomic.StoreInt64(snapshots, 0)

	var body struct {
		Players   []SeenPlayer `json:"players"`
		Observers int          `json:"observers"`
		Coverage  string       `json:"coverage"`
	}
	decode(t, h.get("/api/players"), &body)
	if len(body.Players) != 1 || body.Players[0].Name != "Stranger" || body.Observers != liveRoster || body.Coverage == "" {
		t.Fatalf("/api/players = %+v", body)
	}
	if got := atomic.LoadInt64(reads); got != 1 {
		t.Errorf("/api/players read the fleet's player views %d times, want 1", got)
	}
	if got := atomic.LoadInt64(snapshots); got != 0 {
		t.Errorf("/api/players took %d whole-fleet status snapshots, want 0 — each takes every live session's lock ~8 times", got)
	}

	// Several map panels / tabs polling inside the window share one read.
	for i := 0; i < 5; i++ {
		if res := h.get("/api/players"); res.StatusCode != http.StatusOK {
			t.Fatalf("/api/players = %d", res.StatusCode)
		}
	}
	if got := atomic.LoadInt64(reads); got != 1 {
		t.Errorf("6 requests inside the coalescing window read the fleet %d times, want 1", got)
	}
	*now = now.Add(defaultSeenPlayersTTL)
	h.get("/api/players")
	if got := atomic.LoadInt64(reads); got != 2 {
		t.Errorf("a request after the window read the fleet %d times in total, want 2", got)
	}
}

// /api/status must not pay for the players layer at all: it stays at one snapshot and no
// player reads.
func TestStatusDoesNotReadSeenPlayers(t *testing.T) {
	src, snapshots, reads, _ := countingPlayersSource(t)
	h := newHarness(t, Config{}, testOptions(), src)
	atomic.StoreInt64(snapshots, 0)
	if res := h.get("/api/status"); res.StatusCode != http.StatusOK {
		t.Fatalf("/api/status = %d", res.StatusCode)
	}
	if got := atomic.LoadInt64(reads); got != 0 {
		t.Errorf("/api/status read player views %d times, want 0", got)
	}
	if got := atomic.LoadInt64(snapshots); got != 1 {
		t.Errorf("/api/status took %d snapshots, want 1", got)
	}
}

func TestSeenPlayersIs501WithoutASource(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	if res := h.get("/api/players"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("/api/players on a source without player views = %d, want 501", res.StatusCode)
	}
}
