package fleet

import (
	"errors"
	"time"

	"rsb0t-go/internal/client"
)

// This file separates the two completely different things the login server can mean when
// it turns an account away, because conflating them cost the live fleet real recovery time.
//
// **Response 16 — "Login attempts exceeded".** A RATE rejection: too many login attempts
// from this address inside the server's own rolling window. Waiting is exactly the right
// answer, and waiting QUIETLY matters: the engine counts attempts in a TTL cache keyed by
// address whose entry is re-set (and so its expiry pushed out) on every further attempt, so
// a steady trickle of attempts can keep the counter alive indefinitely and never let it
// lapse. That is what loginRateGate.Penalize is for, and response 16 should keep using it.
//
// **Response 9 — "Login limit exceeded / Too many connections from your address".** A
// CONCURRENCY rejection: the address already holds as many simultaneous connections as the
// server will allow. This is the one that was being handled wrongly. Nothing the client
// does — waiting, pausing, backing off — can free a slot; only an existing session ending
// can. Treating it like response 16 therefore bought nothing, and cost two real things:
//
//  1. loginRateGate.Penalize halts logins FLEET-WIDE for two minutes (extending on each
//     further rejection, up to maxLoginPenalty). With a large surplus of accounts pinned
//     against the ceiling, rejections never stop arriving, so the gate spent most of its
//     life closed — including against accounts whose own slot HAD just come free and which
//     could have got straight back in.
//  2. Every attempt, doomed or not, still counts against the address's rate budget in the
//     engine's TTL cache described above. A large surplus retrying on the ordinary
//     reconnect schedule (capped at 60s) therefore burns the shared budget on attempts
//     that cannot possibly be admitted, starving the accounts that can.
//
// The evidence this is built on is recorded in COLLAB.md: across the archived fleet logs
// and the watcher's own per-minute counts, the fleet genuinely held 265 accounts in game
// simultaneously for roughly ten hours, then — with no roster change — pinned at exactly
// 89-90 in game from 2026-09-18 04:31 UTC onward, with every further account rejected with
// response 9. A concurrency ceiling that moved, not a roster that was always too big, and
// not a rate the fleet was exceeding.
//
// So: response 16 keeps the fleet-wide quiet window; response 9 gets a long, per-account,
// jittered "wait for a slot" backoff instead and no blackout at all.

// isLoginRateRejection reports whether a rejection is the login server's own flood
// protection (response 16), the only case that should trip loginRateGate's fleet-wide
// quiet window. Kept as its own predicate rather than inlined so the distinction from
// isConnectionCeilingRejection below is explicit at every call site.
func isLoginRateRejection(err error) bool {
	var rejected *client.LoginRejectedError
	return errors.As(err, &rejected) && rejected.Response == client.LoginAttemptsExceededResponse
}

// isConnectionCeilingRejection reports whether a rejection is the per-address concurrent
// connection ceiling (response 9) — see this file's own doc comment.
func isConnectionCeilingRejection(err error) bool {
	var rejected *client.LoginRejectedError
	return errors.As(err, &rejected) && rejected.Response == connectionCeilingResponse
}

// connectionCeilingResponse is the login server's "Login limit exceeded / Too many
// connections from your address" code, as the reference client itself renders it
// (rsb0t-main/src/client/shell/Client.ts's own response-9 branch).
const connectionCeilingResponse = 9

// The ceiling backoff schedule. Deliberately much slower than reconnectBackoff, and
// deliberately still bounded rather than "give up": the ceiling is a moving server-side
// number (it was above 265 the day before it was 90), so an account parked against it must
// keep probing occasionally or the fleet would never grow back on its own when the limit
// is raised again.
//
// The numbers are chosen against the gate's own budget rather than picked for feel.
// loginRateInterval hands out one token every 2.5s, i.e. 24 login attempts a minute for
// the whole fleet. With the ordinary schedule, a surplus of S accounts pinned at the
// 60s cap demands S attempts a minute: at the observed S=176 that is ~176/min against a
// 24/min budget, oversubscribing it more than sevenfold with attempts that cannot
// succeed. At connectionCeilingBackoffMax the same surplus demands S/10 per minute —
// ~18/min at S=176 — which fits inside the budget and, more importantly, leaves the
// token channel mostly UNCONTENDED: an account sleeping out its backoff is not blocked
// on the gate, so an account whose slot genuinely just freed finds a token waiting for it
// instead of queueing behind a crowd of doomed ones.
//
// connectionCeilingBackoffStart must stay strictly slower than reconnectBackoffMax, or the
// first ceiling probe costs the shared budget exactly as much as an ordinary retry and the
// separate schedule buys nothing on the attempt that matters most — the one right after an
// account first hits the ceiling, when a whole cohort is arriving at once.
// TestCeilingBackoffFitsTheSharedLoginBudget guards that relationship rather than the
// individual numbers, and caught precisely this: the first draft had both at 60s.
const (
	connectionCeilingBackoffStart = 90 * time.Second
	connectionCeilingBackoffMax   = 10 * time.Minute
)

// connectionCeilingBackoff is the wait before the ceilingAttempt-th consecutive probe of a
// full connection ceiling, doubling from connectionCeilingBackoffStart and capped at
// connectionCeilingBackoffMax. Caller applies jitterFraction, exactly as the ordinary
// schedule does — see jitterFraction's own doc comment for the live lockstep problem that
// exists for.
func connectionCeilingBackoff(ceilingAttempt int) time.Duration {
	if ceilingAttempt < 1 {
		ceilingAttempt = 1
	}
	d := connectionCeilingBackoffStart
	for i := 1; i < ceilingAttempt; i++ {
		d *= 2
		if d >= connectionCeilingBackoffMax {
			return connectionCeilingBackoffMax
		}
	}
	return d
}
