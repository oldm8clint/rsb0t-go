package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/mastercraftplan"
)

// A levelling round that ends with the account STRANDED inside a Runecrafting temple or the essence mine is not followed
// by ScarabSwarm starting there (no bank, nothing for it to do, until the next check): switchTo holds the account while
// the exit is tried again, and starts ScarabSwarm once it is out -- or, as a last resort, after masterRescueTries tries.

func strandedRig(t *testing.T) *masterRig {
	t.Helper()
	old := masterRescuePause
	t.Cleanup(func() { masterRescuePause = old })
	masterRescuePause = 0
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft, Why: "test"}
	r.outcome = masterRoundOutcome{Transient: true, Stranded: true, Why: "Runecrafting 19 -> 19 (+0 xp): STRANDED inside the mine"}
	return r
}

func TestAStrandedRoundIsRescuedBeforeScarabSwarmStarts(t *testing.T) {
	r := strandedRig(t)
	tries := 0
	r.b.rescue = func(*Context) (string, bool) {
		tries++
		r.events = append(r.events, "rescue")
		return "left the mine", tries < 3 // the third try gets the account out
	}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:runecraft,rescue,rescue,rescue,start" {
		t.Fatalf("events %q: ScarabSwarm starts only after the third rescue got the account out", got)
	}
	for _, want := range []string{
		"ScarabSwarm is not started from there",
		"still stranded after rescue try 1",
		"the account is out again after rescue try 3",
	} {
		if !r.logged(want) {
			t.Errorf("log lacks %q: %s", want, strings.Join(r.logs, " | "))
		}
	}
}

// An account that cannot be got out is tried masterRescueTries times, then ScarabSwarm starts from where it stands, loudly.
func TestAStrandedAccountThatNeverLeavesGetsScarabSwarmAsALastResort(t *testing.T) {
	r := strandedRig(t)
	r.b.rescue = func(*Context) (string, bool) {
		r.events = append(r.events, "rescue")
		return "STRANDED inside the temple", true
	}
	r.loop(t)
	want := "stop,round:runecraft," + strings.Repeat("rescue,", masterRescueTries) + "start"
	if got := strings.Join(r.events, ","); got != want {
		t.Fatalf("events %q, want %q", got, want)
	}
	if !r.logged("ScarabSwarm starts from here as a last resort") {
		t.Errorf("log: %s", strings.Join(r.logs, " | "))
	}
}

// A stop during the rescue ends it: nothing is started.
func TestAStopDuringTheRescueStartsNothing(t *testing.T) {
	r := strandedRig(t)
	r.b.rescue = func(*Context) (string, bool) {
		r.events = append(r.events, "rescue")
		r.ctx.requestStop()
		return "STRANDED inside the temple", true
	}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:runecraft,rescue" {
		t.Fatalf("events %q", got)
	}
}

// A round that failed for a passing reason but left the account outside is not rescued: ScarabSwarm starts at once.
func TestARoundThatEndedOutsideIsNotRescued(t *testing.T) {
	r := strandedRig(t)
	r.outcome = masterRoundOutcome{Transient: true, Why: "the walk did not arrive"}
	r.b.rescue = func(*Context) (string, bool) { r.events = append(r.events, "rescue"); return "", false }
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:runecraft,start" {
		t.Fatalf("events %q", got)
	}
}
