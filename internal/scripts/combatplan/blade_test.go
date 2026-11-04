package combatplan

import "testing"

// The regression this file exists for: the mime event nulls the backpack and worn tabs, so both
// containers read empty, and the two-answer version of this rule ended a 23-minute run on that
// reading. An unreadable pass must never be evidence, and must never move the counter.
func TestBladeGateNeverStopsOnAnUnreadableContainer(t *testing.T) {
	verdict, fails := BladeGate(false, false, false, 0, BladeFailLimit)
	if verdict != BladeWait {
		t.Errorf("an unreadable container gave %s, want wait", verdict)
	}
	if fails != 0 {
		t.Errorf("an unreadable container moved the failure count to %d, want it left at 0", fails)
	}

	// Even a run already one pass from the limit cannot be pushed over it by a reading that
	// proves nothing. This is the whole point: a mime event holds the tabs away for minutes.
	verdict, fails = BladeGate(false, false, false, BladeFailLimit-1, BladeFailLimit)
	if verdict != BladeWait {
		t.Errorf("an unreadable container at the edge of the limit gave %s, want wait", verdict)
	}
	if fails != BladeFailLimit-1 {
		t.Errorf("an unreadable container at the edge moved the count to %d, want %d", fails, BladeFailLimit-1)
	}

	// A thousand unreadable passes in a row still cannot stop a run.
	fails = 0
	for i := 0; i < 1000; i++ {
		verdict, fails = BladeGate(false, false, false, fails, BladeFailLimit)
		if verdict == BladeStop {
			t.Fatalf("unreadable pass %d stopped the run", i)
		}
	}
}

func TestBladeGateIsReadyWhenTheBladeIsOnOrGoesOn(t *testing.T) {
	for _, c := range []struct {
		name                     string
		readable, wielding, wore bool
	}{
		{"already wielding it", true, true, false},
		{"put one on from the pack", true, false, true},
		// Wielding is a positive reading even when the panels are mid-refresh: something was
		// seen in the right hand, and seeing it is not something an empty read can do.
		{"wielding it while the containers are not readable", false, true, false},
	} {
		verdict, fails := BladeGate(c.readable, c.wielding, c.wore, 7, BladeFailLimit)
		if verdict != BladeReady {
			t.Errorf("%s: got %s, want ready", c.name, verdict)
		}
		if fails != 0 {
			t.Errorf("%s: failure count %d, want it reset to 0", c.name, fails)
		}
	}
}

// The other half: a readable account that genuinely owns no blade is a real, permanent
// condition, and a run that can never cross the sewer web has to end rather than spin.
func TestBladeGateStopsOnlyAfterTheLimitOfReadableFailures(t *testing.T) {
	fails := 0
	var verdict BladeVerdict
	for i := 1; i < BladeFailLimit; i++ {
		verdict, fails = BladeGate(true, false, false, fails, BladeFailLimit)
		if verdict != BladeWait {
			t.Fatalf("readable failure %d gave %s, want wait until the limit", i, verdict)
		}
		if fails != i {
			t.Fatalf("readable failure %d counted %d", i, fails)
		}
	}
	verdict, fails = BladeGate(true, false, false, fails, BladeFailLimit)
	if verdict != BladeStop {
		t.Errorf("readable failure %d gave %s, want stop", BladeFailLimit, verdict)
	}
	if fails != BladeFailLimit {
		t.Errorf("the stopping pass counted %d, want %d", fails, BladeFailLimit)
	}
}

// A success anywhere in a losing streak clears it, so a run that recovers is not carrying credit
// toward a stop it no longer deserves.
func TestBladeGateResetsTheCountOnRecovery(t *testing.T) {
	fails := 0
	var verdict BladeVerdict
	for i := 0; i < 5; i++ {
		verdict, fails = BladeGate(true, false, false, fails, BladeFailLimit)
	}
	if fails != 5 {
		t.Fatalf("expected a streak of 5, got %d", fails)
	}
	if verdict, fails = BladeGate(true, false, true, fails, BladeFailLimit); verdict != BladeReady || fails != 0 {
		t.Errorf("recovery gave %s with count %d, want ready with 0", verdict, fails)
	}
}
