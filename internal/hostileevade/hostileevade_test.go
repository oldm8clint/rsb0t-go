package hostileevade

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/hostileavoid"
)

// The budget is the one thing standing between "break off from the wolf" and "never finish the
// travel leg", so its state machine is asserted rather than trusted.
func TestBudgetAllowsExactlyBudgetBreakOffsPerWindow(t *testing.T) {
	st := &state{}
	now := time.Unix(10000, 0)
	for i := 0; i < Budget; i++ {
		ok, exhausted := st.claim(now)
		if !ok || exhausted {
			t.Fatalf("break-off %d/%d refused (ok=%v exhausted=%v)", i+1, Budget, ok, exhausted)
		}
		now = now.Add(time.Second)
	}
	ok, exhausted := st.claim(now)
	if ok {
		t.Fatal("a break-off past the budget was allowed")
	}
	if !exhausted {
		t.Fatal("the budget ran out without saying so, so nothing would be logged")
	}
}

// Exhausting the budget must stand evasion down for Rest and then let it resume: standing down
// forever would trade a death for a permanent stall, which is the worse of the two.
func TestBudgetStandsDownForRestThenResumes(t *testing.T) {
	st := &state{}
	now := time.Unix(10000, 0)
	for i := 0; i <= Budget; i++ {
		st.claim(now)
	}
	if ok, _ := st.claim(now.Add(Rest / 2)); ok {
		t.Error("a break-off was allowed during the rest period")
	}
	if ok, _ := st.claim(now.Add(Rest + time.Second)); !ok {
		t.Error("evasion never resumed after the rest period")
	}
}

// A quiet window must not accumulate: an account that broke off twice an hour ago is not close
// to its limit now.
func TestBudgetResetsAfterAQuietWindow(t *testing.T) {
	st := &state{}
	now := time.Unix(10000, 0)
	for i := 0; i < Budget-1; i++ {
		st.claim(now)
	}
	later := now.Add(Window + time.Minute)
	for i := 0; i < Budget; i++ {
		if ok, _ := st.claim(later); !ok {
			t.Fatalf("break-off %d refused after a quiet window", i+1)
		}
		later = later.Add(time.Second)
	}
}

// The sighting diagnostic is rate-limited because an unthrottled one logs on every poll for as
// long as a wolf is in view — this project has already produced a 6.7 GB fleet log that way.
func TestSightingLoggingIsThrottled(t *testing.T) {
	st := &state{}
	now := time.Unix(10000, 0)
	if !st.noteSighting(now) {
		t.Fatal("the first sighting was not logged")
	}
	if st.noteSighting(now.Add(sightingLogEvery / 2)) {
		t.Error("a second sighting inside the throttle window was logged")
	}
	if !st.noteSighting(now.Add(sightingLogEvery + time.Second)) {
		t.Error("a sighting past the throttle window was not logged")
	}
}

// Cfg.Enabled false must turn the whole thing off, which is what the live A/B harness flips and
// what the zero value of Deps gives a caller that has not thought about its margins.
func TestBreakOffDoesNothingWhenDisabledOrUnwired(t *testing.T) {
	if moved, _ := BreakOff(Deps{}); moved {
		t.Error("the zero Deps broke off from something")
	}
	if moved, _ := BreakOff(Deps{Cfg: hostileavoid.Config{Enabled: true}}); moved {
		t.Error("BreakOff moved an account with no session and no pack")
	}
}

// The poll interval is per-caller because the two walk loops that share this package poll at
// different rates; a caller that names none must still get a sane one rather than a busy loop.
func TestPollFallsBackToTheDefault(t *testing.T) {
	if got := (Deps{}).poll(); got != DefaultPoll {
		t.Errorf("default poll = %v, want %v", got, DefaultPoll)
	}
	if got := (Deps{Poll: 50 * time.Millisecond}).poll(); got != 50*time.Millisecond {
		t.Errorf("poll override = %v, want 50ms", got)
	}
}
