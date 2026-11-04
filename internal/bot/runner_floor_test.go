package bot

import (
	"testing"
	"time"
)

// The Runner calls Loop back to back, and a Loop that returns at once (a phase re-derived from live state, a chat handed back for
// the next pass) used to be called again at once: CPU speed, and, when each pass sent a click, a queue of packets at the server
// (docs/click_flood_audit_2026-10-15.md). loopFloor pads a short pass.

func TestARunnerCallsALoopThatReturnsAtOnceAtMostAboutSevenTimesASecond(t *testing.T) {
	fb := &fakeBot{}
	r := NewRunner("Fake", fb, newTestContext())
	r.Start()
	time.Sleep(1200 * time.Millisecond)
	r.Stop()
	r.Wait()
	_, _, calls := fb.snapshot()
	want := int(1200*time.Millisecond/loopFloor) + 2
	if calls > want {
		t.Errorf("Loop ran %d times in 1.2 s; a pass that returns at once may not be repeated faster than every %v (at most %d)", calls, loopFloor, want)
	}
	if calls < 4 {
		t.Errorf("Loop ran only %d times in 1.2 s; the floor must not make a pass slower than %v", calls, loopFloor)
	}
}

func TestARunnerDoesNotSlowALoopThatTakesLongerThanTheFloor(t *testing.T) {
	fb := &fakeBot{}
	fb.onLoop = func(ctx *Context) { ctx.Delay(250 * time.Millisecond) }
	r := NewRunner("Fake", fb, newTestContext())
	start := time.Now()
	r.Start()
	time.Sleep(1100 * time.Millisecond)
	r.Stop()
	r.Wait()
	_, _, calls := fb.snapshot()
	if calls < 4 { // 250 ms a pass: four or five in 1.1 s; a floor added on top of a slow pass would make it three
		t.Errorf("Loop ran %d times in %v; a pass that already took %v must not be padded further", calls, time.Since(start).Round(time.Millisecond), 250*time.Millisecond)
	}
}

func TestAStopDuringTheFloorEndsTheRunAtOnce(t *testing.T) {
	fb := &fakeBot{}
	r := NewRunner("Fake", fb, newTestContext())
	r.Start()
	time.Sleep(20 * time.Millisecond) // inside the first pad
	t0 := time.Now()
	r.Stop()
	r.Wait()
	if took := time.Since(t0); took > loopFloor {
		t.Errorf("the run took %v to end after Stop; the floor's wait must be cancellable", took)
	}
	if got := r.Status().State; got != StateStopped {
		t.Errorf("state %q, want stopped", got)
	}
}
