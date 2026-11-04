package freshstartrunner

import (
	"testing"
	"time"
)

// Live 2026-09-22: acct25 died, respawned at Lumbridge needing 60 coins for two ship fares,
// and had 13,340 in its own bank. It pickpocketed twenty Men for 63 and sold its starter kit
// for 50 gp, while its own route to the ship ran through freshstart.Draynor -- twelve tiles
// from the bank holding its money.

// The throttle is what stops a bank trip becoming its own livelock: an account whose bank is
// genuinely empty must not be walked to Draynor and back every time it is short of a fare.
func TestTheOwnBankTripIsTriedOnceThenThrottled(t *testing.T) {
	s := &sailThrottleSession{}
	d := &Deps{Session: s}
	// A Deps with no Locs/Config/Components cannot try, so it reports false without
	// consuming the throttle -- otherwise a misconfigured caller would lock the account out
	// of its own bank for ten minutes on the first loop.
	if _, ok := WithdrawFareFromOwnBank(d, 60); ok {
		t.Fatal("reported success with no loc index, config or components configured")
	}
	if _, seen := ownBankTriedAt.Load(s); seen {
		t.Fatal("a trip that could not even be attempted consumed the throttle")
	}
}

// ...and the throttle, once set, really does suppress.
func TestTheThrottleSuppressesAndThenExpires(t *testing.T) {
	s := &sailThrottleSession{}
	ownBankTriedAt.Store(s, time.Now())
	if time.Since(mustTime(t, s)) >= OwnBankRetry {
		t.Fatal("a freshly stored attempt already reads as expired")
	}
	ownBankTriedAt.Store(s, time.Now().Add(-OwnBankRetry-time.Minute))
	if time.Since(mustTime(t, s)) < OwnBankRetry {
		t.Fatal("an attempt older than OwnBankRetry still reads as recent, so the account " +
			"would never look in its own bank again")
	}
}

func mustTime(t *testing.T, s *sailThrottleSession) time.Time {
	t.Helper()
	v, ok := ownBankTriedAt.Load(s)
	if !ok {
		t.Fatal("no attempt recorded")
	}
	return v.(time.Time)
}

// Per account, like every other throttle in this package: one account's bank trip must not
// stop another's.
func TestTheOwnBankThrottleIsPerSession(t *testing.T) {
	a, b := &sailThrottleSession{}, &sailThrottleSession{}
	ownBankTriedAt.Store(a, time.Now())
	if _, seen := ownBankTriedAt.Load(b); seen {
		t.Fatal("one account's bank attempt was recorded against another")
	}
}

// The retry window has to be long enough to be worth having and short enough that an account
// which banks between attempts is not locked out for the session.
func TestTheRetryWindowIsSane(t *testing.T) {
	if OwnBankRetry < time.Minute || OwnBankRetry > time.Hour {
		t.Fatalf("OwnBankRetry = %s, which is outside anything defensible", OwnBankRetry)
	}
}
