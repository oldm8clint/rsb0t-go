package fleet

import (
	"errors"
	"sync"
	"time"

	"rsb0t-go/internal/client"
)

// The server's world-hop timer, learned once and then respected. Login response 21 means "this
// account logged out of a DIFFERENT world too recently"; the server states the seconds left in
// the response's own payload byte. Two defects spent that budget:
//
//  1. reconnectLoop ignored the stated seconds and retried on the ordinary backoff. Proven live
//     2026-09-22 on acct155's failover to world 2 (run/logs/fleet-20260922-162017-560-35116.log):
//     "15s to wait", a retry 3.2s later, "8s to wait". Every early retry is a login attempt the
//     per-address counter (30 in a rolling 60s, re-armed on every attempt) counts, for nothing.
//  2. hopLeg settles 5s after the logout and then dials, but the timer (45s by default, counted
//     from the logout) is still running, so every production hop spent one attempt learning
//     what the previous hop had already been told.
//
// So: the first response 21 a hop meets teaches the fleet the timer's full length (the time
// since that account's session closed plus the seconds the server stated), and every later hop
// waits it out after its own logout before dialling. A server that never sends 21 (a lab whose
// accounts are staff, who are exempt) never teaches anything, and nothing waits. The stated
// seconds always win over the learned value: hopTime is configurable, and the engine's fallback
// for it is read from NODE_MAX_NPCS by an upstream bug, so no constant can be trusted.

// hopTimerMax bounds a learned timer: a stated 255 (the byte's ceiling) on a misconfigured server
// must not make every hop wait four minutes on a guess.
const hopTimerMax = 2 * time.Minute

// hopTimerMargin is added to every wait: the stated seconds are truncated to whole seconds.
const hopTimerMargin = time.Second

// hopTimerState is the fleet-wide learned timer. Embedded in worldFields.
type hopTimerState struct {
	mu      sync.Mutex
	learned time.Duration
}

// learnHopTimer records a response 21 met sinceGone after this account's session closed, with
// stated left on it. The latest observation wins, so the learned timer can shrink as well as
// grow: it used to keep only the largest ever seen, and one observation measured from a stale
// logout time (a retried leg keeps its first hopGoneAt) pinned every later hop to the two-minute
// cap for the life of the process (review revW). An observation whose reference is older than
// the cap cannot be the timer at all -- the server would have let the login through -- so it is
// not learned from.
func (m *Manager) learnHopTimer(sinceGone, stated time.Duration) {
	if stated <= 0 || sinceGone < 0 || sinceGone > hopTimerMax {
		return
	}
	full := sinceGone + stated
	if full > hopTimerMax {
		full = hopTimerMax
	}
	t := &m.worldFields.hopTimer
	t.mu.Lock()
	t.learned = full
	t.mu.Unlock()
}

// hopTimerPrewait is how much longer a hop whose old session closed at goneAt should wait before
// dialling the other world: 0 until a response 21 has taught the fleet the timer.
func (m *Manager) hopTimerPrewait(goneAt time.Time) time.Duration {
	t := &m.worldFields.hopTimer
	t.mu.Lock()
	learned := t.learned
	t.mu.Unlock()
	if learned <= 0 || goneAt.IsZero() {
		return 0
	}
	if left := learned + hopTimerMargin - time.Since(goneAt); left > 0 {
		return left
	}
	return 0
}

// hopTimerRetry reports the wait a response 21 stated, plus the margin, or 0 for any other error.
func hopTimerRetry(err error) time.Duration {
	var rejected *client.LoginRejectedError
	if errors.As(err, &rejected) && rejected.Response == client.HopTimerResponse && rejected.RetrySeconds > 0 {
		return time.Duration(rejected.RetrySeconds)*time.Second + hopTimerMargin
	}
	return 0
}

// noteRetryHopTimer is retryAccount's hook after a failed login: a response 21 makes the NEXT
// retry wait exactly what the server stated instead of the ordinary backoff.
func (m *Manager) noteRetryHopTimer(a *account, err error) {
	w := hopTimerRetry(err)
	if w <= 0 {
		return
	}
	a.mu.Lock()
	a.hopRetryAfter = w
	a.mu.Unlock()
}

// retryWaitFor is retryAccount's hook before it sleeps: the stated hop-timer wait when the last
// attempt met one (consumed once), else wait unchanged.
func (m *Manager) retryWaitFor(a *account, wait time.Duration) time.Duration {
	a.mu.Lock()
	stated := a.hopRetryAfter
	a.hopRetryAfter = 0
	a.mu.Unlock()
	if stated <= 0 {
		return wait
	}
	m.alogf(a, "[%s] the server's world-hop timer said to wait — retrying in %s, not %s", a.username, stated, wait)
	return stated
}
