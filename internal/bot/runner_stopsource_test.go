package bot

import (
	"errors"
	"testing"
	"time"
)

// internal/fleet's watchdog leaves a bot alone that ended itself and starts again one that was
// stopped from outside (watchdog_orphan.go). What tells them apart is the runner: Stop is the only
// way in from outside, and a script reaches its own stop channel without it.

func TestRunnerTellsAScriptsOwnStopFromOneCalledFromOutside(t *testing.T) {
	// A script that finishes its job: its Loop asks its own Context to stop.
	own := NewRunner("own", &fakeBot{onLoop: func(ctx *Context) { ctx.requestStop() }}, newTestContext())
	own.Start()
	own.Wait()
	if st := own.Status(); st.State != StateStopped || st.StoppedAt.IsZero() {
		t.Fatalf("own stop: %+v, want stopped with a stop time", st)
	}
	if own.StoppedFromOutside() {
		t.Fatal("a script ending itself is not a stop from outside")
	}
	if !own.Finished() {
		t.Fatal("Wait returned, so the run must be finished")
	}

	// The same script stopped by an operator, the watchdog, a hop or a dropped connection.
	outside := NewRunner("outside", &fakeBot{}, newTestContext())
	outside.Start()
	if outside.Finished() || outside.StoppedFromOutside() {
		t.Fatal("a running bot is neither finished nor stopped from outside")
	}
	before := time.Now()
	outside.Stop()
	select {
	case <-outside.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the run")
	}
	if st := outside.Status(); st.State != StateStopped || st.StoppedAt.Before(before) {
		t.Fatalf("outside stop: %+v", st)
	}
	if !outside.StoppedFromOutside() {
		t.Fatal("Stop must be recorded as a stop from outside")
	}

	// A script that fails ends itself too, with an error.
	failed := NewRunner("failed", &fakeBot{loopErr: errors.New("boom"), loopErrAfter: 1}, newTestContext())
	failed.Start()
	failed.Wait()
	if st := failed.Status(); st.State != StateError || st.StoppedAt.IsZero() || failed.StoppedFromOutside() {
		t.Fatalf("failed: %+v outside=%v", st, failed.StoppedFromOutside())
	}
}

// TestRunnerStoppedAtIsTheFirstEnd: a later finish (OnStop panicking after a clean stop) must not
// move the time the run ended, which the safety net counts from.
func TestRunnerStoppedAtIsTheFirstEnd(t *testing.T) {
	r := NewRunner("p", &panicBot{panicInOnStop: true}, newTestContext())
	r.Start()
	r.Stop()
	r.Wait()
	st := r.Status()
	if st.StoppedAt.IsZero() {
		t.Fatalf("no stop time: %+v", st)
	}
	first := st.StoppedAt
	r.finish(StateError, "again")
	if got := r.Status().StoppedAt; !got.Equal(first) {
		t.Fatalf("StoppedAt moved from %v to %v on a second finish", first, got)
	}
}
