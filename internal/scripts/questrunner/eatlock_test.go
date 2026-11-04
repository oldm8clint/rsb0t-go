package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
)

// fakeSession is a session that sends nothing and has a fresh game state (no hitpoints known,
// so EatIfNeeded takes its lock, reads, and returns without eating).
type fakeSession struct{ st *gamestate.State }

func (f fakeSession) Send(func(w *packet.Packet)) error { return nil }
func (f fakeSession) State() *gamestate.State           { return f.st }
func (f fakeSession) Cheats() *anticheat.Counters       { return nil }

func eatReturns(d *Deps, within time.Duration) bool {
	done := make(chan struct{})
	go func() { EatIfNeeded(d, []int{385}, 10); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(within):
		return false
	}
}

// EatIfNeeded takes its own account's lock (Deps.eatMu) and no other: while account A's lock is
// held, A's EatIfNeeded waits and B's runs at once; releasing A's lets A's through.
func TestEatIfNeededTakesOnlyItsOwnAccountsLock(t *testing.T) {
	a := &Deps{Session: fakeSession{gamestate.New()}}
	b := &Deps{Session: fakeSession{gamestate.New()}}
	a.eatMu.Lock()
	if !eatReturns(b, time.Second) {
		a.eatMu.Unlock()
		t.Fatal("account B's EatIfNeeded waited on account A's eating lock")
	}
	done := make(chan struct{})
	go func() { EatIfNeeded(a, []int{385}, 10); close(done) }()
	select {
	case <-done:
		a.eatMu.Unlock()
		t.Fatal("account A's EatIfNeeded ran while A's own eating lock was held: it does not take Deps.eatMu")
	case <-time.After(200 * time.Millisecond):
	}
	a.eatMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("account A's EatIfNeeded never ran after its lock was released")
	}
}
