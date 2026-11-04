package controlpanel

import (
	"time"

	"rsb0t-go/internal/fleet"
)

// A fleet snapshot is expensive, and the dashboard was taking far more of them than it
// needed. This file makes repeated snapshots inside one short window share a single pass.
//
// What a snapshot actually costs: fleet.Manager.Snapshot walks every account, and for each
// CONNECTED one takes that session's gamestate lock about eight separate times (Self,
// Modals, Tile, KnownInventories, HP, MaxHP, IsDead, Skills) while copying its inventories,
// its skills, and its last 50 log and 50 chat lines. That lock is the same one the
// account's own read loop holds while decoding packets, so the cost is paid by the live
// fleet, not only by the dashboard.
//
// Measured before this existed (see snapshotcost_test.go, which asserts the call counts
// rather than wall-clock so it stays meaningful on any machine): one /api/status took 2
// snapshots — Accounts() and Logins() each took their own — and one /api/bank-all took
// **267** at the live 266-account roster, because it asked for every account and then
// called Bank(name) per account, each of which re-snapshotted the whole fleet to find one
// name. The combined-bank panel refreshes every five seconds. That is the starvation
// behind CLAUDE.md's "status requests intermittently time out": /api/status needs the same
// locks and simply queued behind it.
//
// The window is deliberately much shorter than anything that reads through it, so this can
// only ever coalesce work that was already redundant — it cannot make the dashboard show
// staler data than it already did. The hub samples status on its own tick (1s by default)
// and the panels poll in seconds; 250ms is well inside both.
//
// This is a coalescing window, NOT a background refresher: nothing is snapshotted unless a
// caller actually asks. A dashboard with no browser open still costs nothing.
const defaultSnapshotTTL = 250 * time.Millisecond

func (f *FleetSource) snapshotTTL() time.Duration {
	if f.snapTTL > 0 {
		return f.snapTTL
	}
	return defaultSnapshotTTL
}

// cachedSnapshot returns a fleet snapshot no older than the coalescing window, taking a
// fresh one only when the last is too old.
//
// The returned slice is shared by every caller inside the window and must be treated as
// read-only — every existing caller already only reads it, copying out what it needs.
// Deliberately not a defensive copy: copying 266 AccountStatus values (each with its own
// inventory maps, skills map and two 50-line string slices) per call would give back a
// large part of what this is here to save.
func (f *FleetSource) cachedSnapshot() []fleet.AccountStatus {
	now := f.now()
	f.snapMu.Lock()
	if f.snapAt.IsZero() || now.Sub(f.snapAt) >= f.snapshotTTL() {
		// Released around the snapshot itself: Manager.Snapshot takes per-account and
		// per-session locks, and holding this mutex across it would serialize every
		// dashboard reader behind the slowest account's read loop — trading one
		// bottleneck for another. Two callers racing here can both snapshot; that is
		// strictly no worse than the uncached behaviour, and the window makes it rare.
		f.snapMu.Unlock()
		snap := f.snapshot()
		taken := f.now()
		f.snapMu.Lock()
		// Keep whichever snapshot is newer: a slower concurrent caller must not install
		// an older snapshot over a fresher one.
		if f.snapAt.IsZero() || taken.After(f.snapAt) {
			f.snap, f.snapAt = snap, taken
		}
	}
	out := f.snap
	f.snapMu.Unlock()
	return out
}

// invalidateSnapshot drops the cached snapshot so the very next read takes a fresh one.
//
// Every operator action goes through it (FleetSource's own StartBot/StopBot/Disconnect/
// ConnectExisting/AddAccount/RequestHandoff/SetMuleAt all defer it), because a button press
// must be visible in the panel immediately, not whenever the coalescing window happens to
// lapse. A pre-existing test caught exactly that: POST /api/fleet/connect followed by a
// status read returned the state from *before* the reconnect was requested.
//
// Deliberately on FleetSource's mutating methods rather than in the HTTP handlers: these
// are precisely the operations that change what a snapshot would say, so a new endpoint
// that calls one of them cannot forget to invalidate.
func (f *FleetSource) invalidateSnapshot() {
	f.snapMu.Lock()
	f.snap, f.snapAt = nil, time.Time{}
	f.snapMu.Unlock()
}
