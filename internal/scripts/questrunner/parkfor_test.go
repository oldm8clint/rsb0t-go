package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// A wait that carries ParkFor (a stats gate that will not open for hours) parks the quest at once for
// that long: not after WaitPark ticks, and not for the escalating 5 minute Backoff.
func TestAWaitWithParkForParksAtOnceForThatLong(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	decides := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "gated", Name: "gated"},
		Decide: func(questplan.Snapshot) questplan.Step {
			decides++
			return questplan.Step{Kind: questplan.StepWait, Reason: "not doing it: the demon", ParkFor: 24 * time.Hour}
		},
	}}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || res.Reason != "not doing it: the demon" {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
	if decides != 1 {
		t.Fatalf("decide ran %d times: it waited instead of parking", decides)
	}
	if !sink.has("parking for 24h0m0s: not doing it: the demon") {
		t.Errorf("the log does not say why: %v", sink.lines)
	}
	if parked, why := e.Store.Parked("gated", time.Now().Add(23*time.Hour)); !parked || !strings.Contains(why, "the demon") {
		t.Fatalf("not parked for the day: %v %q", parked, why)
	}
	if parked, _ := e.Store.Parked("gated", time.Now().Add(25*time.Hour)); parked {
		t.Fatal("parked past the day")
	}
}

// An ordinary wait still waits WaitPark ticks first.
func TestAnOrdinaryWaitStillWaits(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	decides := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "waits", Name: "waits"},
		Decide: func(questplan.Snapshot) questplan.Step {
			decides++
			return questplan.Step{Kind: questplan.StepWait, Reason: "nothing yet"}
		},
	}}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || decides != WaitPark {
		t.Fatalf("outcome %s after %d decides (want %d): %q", res.Outcome, decides, WaitPark, res.Reason)
	}
}
