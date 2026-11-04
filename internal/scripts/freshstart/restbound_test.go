package freshstart

import (
	"testing"
	"time"
)

func TestShouldRestBoundedStartsAndHoldsLikeShouldRestWith(t *testing.T) {
	// Not resting: the start mark decides.
	if rest, gaveUp := ShouldRestBounded(0.4, false, 0, RestBelowHP, ResumeAtHP, RestMax); !rest || gaveUp {
		t.Errorf("40%% did not start a rest: %v %v", rest, gaveUp)
	}
	if rest, _ := ShouldRestBounded(0.41, false, 0, RestBelowHP, ResumeAtHP, RestMax); rest {
		t.Error("41% started a rest")
	}
	// Resting: the resume mark decides, inside the bound.
	if rest, _ := ShouldRestBounded(0.6, true, time.Minute, RestBelowHP, ResumeAtHP, RestMax); !rest {
		t.Error("60% ended a rest")
	}
	if rest, gaveUp := ShouldRestBounded(0.9, true, time.Minute, RestBelowHP, ResumeAtHP, RestMax); rest || gaveUp {
		t.Errorf("90%% did not end the rest cleanly: %v %v", rest, gaveUp)
	}
}

// The bound: a rest that has lasted RestMax is given up, whatever health says, and it says so.
func TestShouldRestBoundedGivesUpAtTheBound(t *testing.T) {
	if rest, gaveUp := ShouldRestBounded(0.4, true, RestMax-time.Second, RestBelowHP, ResumeAtHP, RestMax); !rest || gaveUp {
		t.Errorf("a second short of the bound: %v %v", rest, gaveUp)
	}
	if rest, gaveUp := ShouldRestBounded(0.4, true, RestMax, RestBelowHP, ResumeAtHP, RestMax); rest || !gaveUp {
		t.Errorf("at the bound: %v %v", rest, gaveUp)
	}
	if rest, gaveUp := ShouldRestBounded(0.4, true, 74*time.Minute, RestBelowHP, ResumeAtHP, RestMax); rest || !gaveUp {
		t.Errorf("Howdy Folk's 1h14m: %v %v", rest, gaveUp)
	}
	// Health back over the resume mark at the very bound is an ordinary end, not a give-up.
	if rest, gaveUp := ShouldRestBounded(0.95, true, RestMax+time.Minute, RestBelowHP, ResumeAtHP, RestMax); rest || gaveUp {
		t.Errorf("recovered: %v %v", rest, gaveUp)
	}
	// No bound (0) is the old unbounded rest.
	if rest, gaveUp := ShouldRestBounded(0.4, true, 24*time.Hour, RestBelowHP, ResumeAtHP, 0); !rest || gaveUp {
		t.Errorf("no bound: %v %v", rest, gaveUp)
	}
}

// The bound is longer than health needs to come back: a starting account (10 hitpoints) rests from 4
// to the resume mark of 9, five points at a point a minute.
func TestRestMaxOutlastsAStartingAccountsRegeneration(t *testing.T) {
	const startingHP, startAt, regenPerMinute = 10, 4, 1
	need := time.Duration(int(ResumeAtHP*startingHP)-startAt) * time.Minute / regenPerMinute
	if RestMax <= need {
		t.Errorf("RestMax %s does not outlast the %s a starting account needs", RestMax, need)
	}
	if RestCooldown <= RestMax {
		t.Errorf("RestCooldown %s must outlast the rest it follows (%s)", RestCooldown, RestMax)
	}
}
