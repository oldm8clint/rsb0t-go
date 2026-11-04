package fisherrunner

import (
	"testing"
	"time"
)

// campWalkRun runs walkToCampUntilSpot with a walk that polls its until function polls times
// and a spot that comes into view on poll inViewAt (0 = never), and reports every wait.
func campWalkRun(polls, inViewAt int) (waits []time.Duration, ok bool) {
	d := &Deps{Delay: func(dur time.Duration) bool { waits = append(waits, dur); return true }}
	n := 0
	inView := func() bool { return inViewAt > 0 && n >= inViewAt }
	ok = walkToCampUntilSpot(d, func(until func() bool) {
		for i := 0; i < polls; i++ {
			n++
			if until() {
				return
			}
		}
	}, inView)
	return waits, ok
}

// The fixed one-second wait after the camp walk is for a walk that ended with no spot in view.
// When the walk stopped because a spot came into view, the next look happens at once. Live on
// build e35420b6 that step ran 59,009 times in 37 hours, each paying the second regardless.
func TestCampWalkDoesNotWaitOnceASpotIsInView(t *testing.T) {
	waits, ok := campWalkRun(10, 3)
	if !ok || len(waits) != 0 {
		t.Fatalf("spot came into view during the walk: waits=%v ok=%v, want no wait", waits, ok)
	}
	// The walk ended (arrived, or gave up) without polling, but a spot is in view now: no wait
	// either.
	var slept []time.Duration
	d := &Deps{Delay: func(dur time.Duration) bool { slept = append(slept, dur); return true }}
	if !walkToCampUntilSpot(d, func(func() bool) {}, func() bool { return true }) || len(slept) != 0 {
		t.Fatalf("spot in view after the walk: waits=%v, want none", slept)
	}
}

func TestCampWalkWaitsASecondWhenNothingCameIntoView(t *testing.T) {
	waits, ok := campWalkRun(10, 0)
	if !ok || len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("waits=%v ok=%v, want one 1s wait", waits, ok)
	}
	d := &Deps{Delay: func(time.Duration) bool { return false }}
	if walkToCampUntilSpot(d, func(func() bool) {}, func() bool { return false }) {
		t.Fatal("a stop requested during the wait must be reported")
	}
}
