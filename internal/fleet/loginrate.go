package fleet

import (
	"sync"
	"time"
)

// loginRateGate caps how often ANY account (a fresh Connect or a reconnectLoop retry alike)
// may actually attempt a login, globally across the whole Manager. Live-found 2026-09-16,
// this codebase's first real production run, ramping the full 179-account roster: per-
// account exponential backoff + jitter (reconnectLoop, already proven for the "an
// established session drops" case) does NOT, by itself, stop a large cohort of accounts
// from collectively re-triggering the login server's own flood limiter (response 16, ~29
// logins/minute — see client.LoginAttemptsExceededResponse's own doc comment) — each
// account paces ITSELF, but nothing paces the GROUP. Confirmed live: with ~96 accounts
// simultaneously in reconnectLoop, each backing off on its own schedule, the aggregate
// attempt rate across all of them stayed well over the server's budget for 5+ minutes
// straight with NOT ONE successful retry — a thundering herd that jitter alone cannot fix
// once the herd is this large relative to the shared budget.
//
// A steady trickle alone (the gate's original shape, before Penalize existed) turned out
// not to be enough either — the account owner's own live hypothesis (2026-09-16): if the
// login server's own limiter resets/extends its cooldown on every new attempt while already
// tripped (a common flood-protection shape — "any attempt during the penalty restarts it"),
// then continuing to trickle a FEW attempts through during the window, however slowly,
// could keep the whole fleet penalized indefinitely rather than ever letting it lapse. This
// gate now tests that hypothesis directly and defends against it either way: Penalize (see
// its own doc comment) drains every outstanding token and blocks ALL attempts, from EVERY
// account, for a full cooldown window the moment any single one comes back rejected — not
// merely slowed down. If another rejection lands before that window elapses (very possible
// right after the first hit, from the handful of requests already in flight), the window
// extends again rather than resuming early. Only once a clean window passes with zero
// rejections does the gate let a login through at all.
type loginRateGate struct {
	tokens chan struct{}
	stop   chan struct{}

	mu             sync.Mutex
	penalizedUntil time.Time
	// penaltyStreakStart is when the CURRENT run of back-to-back Penalize calls began —
	// see maxLoginPenalty's own doc comment for why this exists.
	penaltyStreakStart time.Time
}

// loginRateInterval is one token every this long during NORMAL (non-penalized) operation —
// ~24 logins/minute, comfortably under the ~29/minute empirical threshold (see this type's
// own doc comment and cmd/memprobe's -login-delay flag help text) with margin for clock
// jitter and the fact that a REJECTED attempt still counts against the server's own budget
// just as much as an accepted one.
const loginRateInterval = 2500 * time.Millisecond

// loginRateBurst is the bucket's capacity — small on purpose; see loginRateInterval's own
// doc comment for why a large burst would defeat this gate's whole point.
const loginRateBurst = 2

// loginPenalty is how long EVERY login attempt, fleet-wide, halts after any one of them
// comes back rejected by the login server's own flood protection — the tuning knob for the
// live experiment described in this type's own doc comment. Chosen generously (well past
// every earlier ad-hoc "wait ~60-90s and manually reconnect" that worked before this gate
// existed) so the first real test has a real chance of landing on a genuinely clean window
// rather than just re-triggering the same uncertainty at a smaller scale. Revisit this
// constant once a real fleet run confirms (or refutes) the reset-on-attempt hypothesis.
const loginPenalty = 2 * time.Minute

// maxLoginPenalty caps how long a single BACK-TO-BACK run of Penalize calls can extend the
// window for, measured from the first rejection in that run — a real live-lock found the
// same night this gate first ran at full 179-account scale: with dozens of accounts each
// independently retrying on their own reconnectLoop backoff schedule (2s doubling to a
// jittered 60s), a rejection landed somewhere in the crowd roughly every 1-2 seconds on
// average, so Penalize's own "extend, don't resume early" rule (see its doc comment) never
// found the clean, rejection-free window it was waiting for — the fleet sat stuck at
// 116/179 accounts for 5+ minutes with the gate fully, correctly, and PERMANENTLY closed.
// Extending forever was only ever safe for the small-stragglers case Penalize was designed
// around; at large contention it becomes a genuine deadlock, not just a longer wait. Capping
// the total extension guarantees the gate reopens eventually regardless of how many accounts
// are still mid-retry, trading a small chance of one more rejection right at the cap for a
// hard guarantee of forward progress. 5 minutes is comfortably longer than every clean
// recovery observed live (the biggest was ~3.5 minutes across a ~90-account backlog), so
// this should rarely if ever actually bind — it exists purely as a backstop.
const maxLoginPenalty = 5 * time.Minute

func newLoginRateGate() *loginRateGate {
	g := &loginRateGate{
		tokens: make(chan struct{}, loginRateBurst),
		stop:   make(chan struct{}),
	}
	for i := 0; i < loginRateBurst; i++ {
		g.tokens <- struct{}{}
	}
	go g.refill()
	return g
}

func (g *loginRateGate) refill() {
	t := time.NewTicker(loginRateInterval)
	defer t.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-t.C:
			g.mu.Lock()
			if time.Now().Before(g.penalizedUntil) {
				g.mu.Unlock()
				continue
			}
			select {
			case g.tokens <- struct{}{}:
			default:
				// Bucket already full — a token was never spent since the last tick,
				// meaning nothing is currently waiting on this gate. Dropping the refill
				// rather than blocking keeps refill() itself from ever stalling.
			}
			g.mu.Unlock()
		}
	}
}

// Wait blocks until a token is available or done fires (e.g. the Manager's own m.stop, so a
// login attempt mid-wait doesn't outlive a shutdown), returning false in the latter case.
// Checks the penalty window first, in a loop — a Penalize call landing while this is
// already waiting out an earlier one must extend the wait, not be missed.
func (g *loginRateGate) Wait(done <-chan struct{}) bool {
	for {
		g.mu.Lock()
		wait := time.Until(g.penalizedUntil)
		if wait <= 0 {
			// The penalty (if any) has genuinely elapsed — end this streak so the next
			// rejection, whenever it comes, starts maxLoginPenalty's own cap counting
			// fresh from zero rather than staying pinned to a run that already ended.
			g.penaltyStreakStart = time.Time{}
		}
		g.mu.Unlock()
		if wait > 0 {
			select {
			case <-time.After(wait):
				continue
			case <-done:
				return false
			case <-g.stop:
				return false
			}
		}
		select {
		case <-g.tokens:
			// A rejection can arrive after the check above while this caller
			// waits for a token. Recheck under the same lock as Penalize: an
			// already queued retry must not break the fleet's quiet window.
			g.mu.Lock()
			allowed := !time.Now().Before(g.penalizedUntil)
			g.mu.Unlock()
			if allowed {
				return true
			}
		case <-done:
			return false
		case <-g.stop:
			return false
		}
	}
}

// Penalize halts every future Wait call, fleet-wide, until loginPenalty has elapsed with no
// further Penalize call — see this type's own doc comment for why. Extends rather than
// replaces an already-running penalty (later of the two), so a rejection landing near the
// end of one window pushes it out rather than letting a near-simultaneous second rejection
// slip through a window that's about to close — but never past maxLoginPenalty from the
// first rejection in the current back-to-back streak (see that constant's own doc comment
// for the live deadlock this backstop closes). Also drains every token currently sitting in
// the bucket, so a Wait call already past the penalty check (i.e. already holding a spot in
// line for a token) doesn't complete during what should be a total quiet period.
func (g *loginRateGate) Penalize() {
	g.mu.Lock()
	now := time.Now()
	if g.penaltyStreakStart.IsZero() {
		g.penaltyStreakStart = now
	}
	streakDeadline := g.penaltyStreakStart.Add(maxLoginPenalty)
	until := now.Add(loginPenalty)
	if until.After(streakDeadline) {
		until = streakDeadline
	}
	if until.After(g.penalizedUntil) {
		g.penalizedUntil = until
	}
	defer g.mu.Unlock()
	for {
		select {
		case <-g.tokens:
		default:
			return
		}
	}
}

func (g *loginRateGate) Close() {
	close(g.stop)
}
