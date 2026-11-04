package fleet

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/client"
)

func rejected(response int) error {
	return fmt.Errorf("connect: %w", &client.LoginRejectedError{Response: response})
}

// The fleet-wide quiet window belongs to RATE rejections only.
//
// This test used to assert the opposite for response 9, on the reasoning that pausing
// could not hurt. The archived live evidence (see loginceiling.go's own doc comment and
// COLLAB.md) showed it did hurt: response 9 is a concurrent-connection ceiling, no amount
// of waiting frees somebody else's slot, and halting the WHOLE fleet for two minutes every
// time one of a large stuck cohort was refused also blocked the accounts whose own slot had
// just come free. Flipped deliberately, with the reasoning recorded, rather than quietly.
func TestOnlyRateRejectionsPauseTheWholeFleet(t *testing.T) {
	cases := []struct {
		response  int
		wantRate  bool
		wantFloor bool
		why       string
	}{
		{16, true, false, "LoginAttemptsExceededResponse — the server's own flood protection"},
		{9, false, true, "connection ceiling — waiting cannot free another session's slot"},
		{2, false, false, "success never reaches this check, but must stay false either way"},
		{6, false, false, "bad key/checksum — not flood-shaped, must not pause the fleet"},
		{3, false, false, "invalid credentials — same"},
	}
	for _, c := range cases {
		err := rejected(c.response)
		if got := isLoginRateRejection(err); got != c.wantRate {
			t.Errorf("isLoginRateRejection(response %d) = %v, want %v (%s)", c.response, got, c.wantRate, c.why)
		}
		if got := isConnectionCeilingRejection(err); got != c.wantFloor {
			t.Errorf("isConnectionCeilingRejection(response %d) = %v, want %v (%s)", c.response, got, c.wantFloor, c.why)
		}
	}
	// Neither predicate may fire on an error that is not a login rejection at all — a
	// dropped socket mid-handshake must never be mistaken for either server verdict.
	plain := errors.New("dial tcp: connection reset by peer")
	if isLoginRateRejection(plain) || isConnectionCeilingRejection(plain) {
		t.Error("a plain transport error was read as a login-server verdict")
	}
}

// The ceiling schedule has to be slow enough to fit inside the shared login budget while a
// large cohort is parked against the ceiling — that is the whole point of it existing
// separately from reconnectBackoff. This guards the RELATIONSHIP between the constants
// rather than any single value, so raising one without thinking about the other fails here
// instead of live.
func TestCeilingBackoffFitsTheSharedLoginBudget(t *testing.T) {
	// The live cohort that produced this fix: 266 accounts against a ceiling of 90.
	const surplus = 176
	budgetPerMinute := float64(time.Minute) / float64(loginRateInterval)

	ordinary := float64(surplus) / reconnectBackoffMax.Minutes()
	if ordinary <= budgetPerMinute {
		t.Fatalf("premise no longer holds: the ordinary schedule demands %.1f attempts/min, which already fits the %.1f/min budget", ordinary, budgetPerMinute)
	}
	ceiling := float64(surplus) / connectionCeilingBackoffMax.Minutes()
	if ceiling > budgetPerMinute {
		t.Errorf("a %d-account cohort on the ceiling schedule still demands %.1f attempts/min, over the %.1f/min the gate hands out — it would keep starving accounts that can actually get in",
			surplus, ceiling, budgetPerMinute)
	}
	if connectionCeilingBackoffStart <= reconnectBackoffMax {
		t.Errorf("connectionCeilingBackoffStart (%s) must be slower than the ordinary schedule's cap (%s), or a ceiling probe is no cheaper than an ordinary retry",
			connectionCeilingBackoffStart, reconnectBackoffMax)
	}
}

func TestCeilingBackoffDoublesAndCaps(t *testing.T) {
	for _, c := range []struct {
		attempt int
		want    time.Duration
	}{
		{0, connectionCeilingBackoffStart}, // defensive: never a zero wait
		{1, connectionCeilingBackoffStart},
		{2, 3 * time.Minute},
		{3, 6 * time.Minute},
		{4, connectionCeilingBackoffMax},
		{50, connectionCeilingBackoffMax},
	} {
		if got := connectionCeilingBackoff(c.attempt); got != c.want {
			t.Errorf("connectionCeilingBackoff(%d) = %s, want %s", c.attempt, got, c.want)
		}
	}
}

// noteLoginOutcome is what decides which schedule retryAccount uses next, so the streak
// bookkeeping matters: a ceiling streak must survive consecutive refusals and must end the
// moment anything else happens, because the ceiling is a moving server-side number rather
// than a permanent property of the account.
func TestCeilingStreakTracksOnlyConsecutiveCeilingRefusals(t *testing.T) {
	a := &account{username: "acct58"}

	noteLoginOutcome(a, rejected(connectionCeilingResponse))
	noteLoginOutcome(a, rejected(connectionCeilingResponse))
	noteLoginOutcome(a, rejected(connectionCeilingResponse))
	if a.ceilingAttempt != 3 {
		t.Fatalf("after three consecutive ceiling refusals ceilingAttempt = %d, want 3", a.ceilingAttempt)
	}

	// A rate rejection is a different verdict: the ceiling streak ends, so the account
	// goes back to the ordinary schedule while the shared gate's own quiet window runs.
	noteLoginOutcome(a, rejected(client.LoginAttemptsExceededResponse))
	if a.ceilingAttempt != 0 {
		t.Errorf("a rate rejection left ceilingAttempt = %d, want the streak ended", a.ceilingAttempt)
	}

	noteLoginOutcome(a, rejected(connectionCeilingResponse))
	if a.ceilingAttempt != 1 {
		t.Fatalf("a fresh ceiling refusal did not restart the streak: %d", a.ceilingAttempt)
	}
	// Getting in is the real proof the address is no longer full.
	noteLoginOutcome(a, nil)
	if a.ceilingAttempt != 0 {
		t.Errorf("a successful login left ceilingAttempt = %d, want 0", a.ceilingAttempt)
	}
}
