package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
)

// fakeStickySession is a bare actions.Session used as a distinct, comparable map key by
// stationaryFor/RecentlyAttackedNear/noteCombatFlee.
//
// Since 2026-09-19 noteCombatFlee DOES read the session's state — it records which NPC was
// attacking (identifyAttackers) so RecentlyAttackedNear can lift the block early once that
// NPC has gone. This fake therefore returns an EMPTY real state rather than panicking: with
// no NPCs in view no attacker can be identified, which is precisely the case the change is
// designed to leave untouched (full window, full radius). So the tests below that use this
// fake still pin the original behaviour exactly; the attacker-aware behaviour is pinned
// separately in combatattacker_test.go against real decoded NPC_INFO state.
type fakeStickySession struct{}

func (fakeStickySession) Send(func(w *packet.Packet)) error { panic("not used by this test") }
func (fakeStickySession) State() *gamestate.State           { return gamestate.New() }
func (fakeStickySession) Cheats() *anticheat.Counters       { panic("not used by this test") }

func TestStationaryForResetsOnMovementAndFiresAfterWindow(t *testing.T) {
	s := &fakeStickySession{}
	const win = 50 * time.Millisecond

	if stationaryFor(s, 100, 100, 0, win) {
		t.Fatal("first observation must report false — nothing to compare against yet")
	}
	if stationaryFor(s, 100, 100, 0, win) {
		t.Fatal("same tile immediately after the first observation must still be false — window hasn't elapsed")
	}
	// A different tile is real movement: resets the clock, still false immediately after.
	if stationaryFor(s, 101, 100, 0, win) {
		t.Fatal("a changed tile must reset the clock and report false")
	}
	time.Sleep(win + 20*time.Millisecond)
	if !stationaryFor(s, 101, 100, 0, win) {
		t.Fatal("same tile held past the window must report true")
	}
}

func TestStationaryForIsPerSession(t *testing.T) {
	// Fresh pointers each call — sync.Map keys on session identity, so these are
	// guaranteed-distinct keys with no prior state to worry about.
	a, b := &fakeStickySession{}, &fakeStickySession{}
	const win = 20 * time.Millisecond
	stationaryFor(a, 5, 5, 0, win)
	// b has never been observed at this tile — must not inherit a's clock.
	if stationaryFor(b, 5, 5, 0, win) {
		t.Fatal("a different session must not share stationaryFor's tracked observation")
	}
}

func TestRecentlyAttackedNearBeforeAnyFlee(t *testing.T) {
	s := &fakeStickySession{}
	if RecentlyAttackedNear(s, 0, 0, 0, combatAvoidRadius) {
		t.Fatal("a session with no recorded flee must never report recently attacked")
	}
}

func TestRecentlyAttackedNearRadiusAndExpiry(t *testing.T) {
	s := &fakeStickySession{}
	noteCombatFlee(s, 2845, 3431, 0)

	if !RecentlyAttackedNear(s, 2845, 3431, 0, combatAvoidRadius) {
		t.Fatal("the exact flee tile must read as recently attacked")
	}
	if !RecentlyAttackedNear(s, 2845+combatAvoidRadius, 3431, 0, combatAvoidRadius) {
		t.Fatal("a tile exactly at the avoid radius must still read as recently attacked")
	}
	if RecentlyAttackedNear(s, 2845+combatAvoidRadius+1, 3431, 0, combatAvoidRadius) {
		t.Fatal("a tile just outside the avoid radius must not read as recently attacked")
	}
	if RecentlyAttackedNear(s, 2845, 3431, 1, combatAvoidRadius) {
		t.Fatal("the same x,z on a different level must not read as recently attacked")
	}
}

func TestRecentlyAttackedNearExpiresAfterWindow(t *testing.T) {
	s := &fakeStickySession{}
	recentCombat.Store(s, recentCombatSpot{x: 10, z: 10, level: 0, until: time.Now().Add(-time.Second)})
	if RecentlyAttackedNear(s, 10, 10, 0, combatAvoidRadius) {
		t.Fatal("a flee whose avoidance window already elapsed must not still exclude the spot")
	}
}
