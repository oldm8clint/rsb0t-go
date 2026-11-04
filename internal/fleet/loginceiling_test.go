package fleet

import (
	"testing"
	"time"

	"rsb0t-go/internal/client"
)

// jitterBounds is the range jitterFraction can produce for d — see its own doc comment for
// why every wait in this package is randomized.
func jitterBounds(d time.Duration) (time.Duration, time.Duration) {
	f := float64(d)
	return time.Duration(f * 0.7), time.Duration(f * 1.3)
}

// observeFirstRetry drives one account through a single failed login and reports the wait
// the reconnect loop chose for its next attempt, plus whether the shared fleet-wide gate
// was put into a quiet window. Stubs connectLogin (so no server is needed) and retryWait
// (so nothing actually sleeps, and the loop stops after the one wait under test).
func observeFirstRetry(t *testing.T, loginErr error) (wait time.Duration, fleetPaused bool) {
	t.Helper()
	m := New(Config{})
	defer m.CloseAll()

	m.connectLogin = func(client.Config) (*client.Session, error) { return nil, loginErr }
	waits := make(chan time.Duration, 1)
	m.retryWait = func(done <-chan struct{}, d time.Duration) bool {
		select {
		case waits <- d:
		default:
		}
		return false // end the retry loop: one wait is all this test needs to see
	}

	m.Connect("acct58", "unused-by-the-stub")
	select {
	case wait = <-waits:
	case <-time.After(10 * time.Second):
		t.Fatal("the reconnect loop never chose a wait")
	}
	m.loginRate.mu.Lock()
	fleetPaused = !m.loginRate.penalizedUntil.IsZero() && time.Now().Before(m.loginRate.penalizedUntil)
	m.loginRate.mu.Unlock()
	return wait, fleetPaused
}

// The behaviour this whole change exists for, asserted end to end rather than only at the
// predicate level: a connection-ceiling refusal must NOT stop the rest of the fleet logging
// in, and the refused account must fall back to the slow ceiling schedule.
//
// Why it matters, from the live evidence in COLLAB.md: with 176 accounts pinned against a
// ceiling of 90, the old behaviour put the shared gate into a two-minute quiet window
// (extending on each further refusal) essentially continuously. An account that had been
// happily in game and simply dropped — whose slot WAS free and WAS claimable — had to wait
// that blackout out, for the sake of a pause that could not free a slot for anybody.
func TestConnectionCeilingRefusalDoesNotPauseTheFleet(t *testing.T) {
	wait, paused := observeFirstRetry(t, rejected(connectionCeilingResponse))
	if paused {
		t.Error("a connection-ceiling refusal put the whole fleet into a login quiet window; waiting cannot free another session's slot, and this blocks accounts whose slot just came free")
	}
	low, high := jitterBounds(connectionCeilingBackoffStart)
	if wait < low || wait > high {
		t.Errorf("next attempt in %s, want the jittered ceiling schedule %s..%s — the ordinary schedule would spend the fleet's shared login budget on an attempt that cannot be admitted", wait, low, high)
	}
}

// The other half, unchanged on purpose: a genuine RATE refusal still halts every account.
// The engine counts login attempts per address in a TTL cache whose entry is re-set on each
// further attempt, so only a genuinely quiet window lets the counter lapse — a trickle
// would keep it alive indefinitely. That is what loginRateGate.Penalize is for.
func TestRateRefusalStillPausesTheWholeFleet(t *testing.T) {
	wait, paused := observeFirstRetry(t, rejected(client.LoginAttemptsExceededResponse))
	if !paused {
		t.Error("a login-rate refusal no longer pauses the fleet; the shared quiet window is the only thing that lets the server's own per-address counter lapse")
	}
	low, high := jitterBounds(reconnectBackoffStart)
	if wait < low || wait > high {
		t.Errorf("next attempt in %s, want the ordinary jittered schedule %s..%s — a rate refusal is not a ceiling and must not be parked on the slow one", wait, low, high)
	}
}
