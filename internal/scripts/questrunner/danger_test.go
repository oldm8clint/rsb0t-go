package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

func TestDangerWatchWalksThenParksOnFallingHitpoints(t *testing.T) {
	// The 6 Nov East Ardougne run: 25, 20, 18, 16, 15 of 38 with a stalled quest and no food.
	var w dangerWatch
	got := []dangerVerdict{}
	for _, hp := range []int{25, 20, 18, 16, 15} {
		got = append(got, w.Note(hp, 38, true))
	}
	walked := false
	for _, v := range got {
		if v == dangerWalkAway {
			walked = true
		}
		if v == dangerPark {
			t.Fatalf("parked before walking away: %v", got)
		}
	}
	if !walked {
		t.Fatalf("never walked away while falling to half: %v", got)
	}
	// Still falling after the walk: park.
	parked := false
	for _, hp := range []int{11, 6, 5, 4} {
		if w.Note(hp, 38, true) == dangerPark {
			parked = true
		}
	}
	if !parked {
		t.Fatal("kept standing in the danger instead of parking")
	}
}

func TestDangerWatchIgnoresProgressAndSteadyHitpoints(t *testing.T) {
	var w dangerWatch
	for i := 0; i < 30; i++ {
		if v := w.Note(10, 38, true); v != dangerNone {
			t.Fatalf("steady low hitpoints with no drop gave %v", v)
		}
	}
	var p dangerWatch
	for _, hp := range []int{30, 25, 20, 15, 10} {
		if v := p.Note(hp, 38, false); v != dangerNone {
			t.Fatalf("a progressing quest was judged: %v at %d", v, hp)
		}
	}
	if v := p.Note(0, 38, true); v != dangerNone {
		t.Fatal("an unknown reading was judged")
	}
}

func TestDangerWatchResetsWalksWhenHealthy(t *testing.T) {
	var w dangerWatch
	for _, hp := range []int{10, 8, 6} {
		w.Note(hp, 38, true)
	}
	w.Note(38, 38, false)
	if w.walks != 0 {
		t.Fatal("walk count survived a recovery")
	}
}

// Two wait reasons that alternate never reach WaitPark on one key; the any-reason streak parks them.
func TestAlternatingWaitsParkAndFallingHitpointsStopTheRun(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	flip := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "alt", Name: "alt"},
		Decide: func(questplan.Snapshot) questplan.Step {
			flip++
			return questplan.Step{Kind: questplan.StepWait, Reason: []string{"journal not read", "kit short"}[flip%2]}
		},
	}}
	res := e.Run(q, 5*time.Minute)
	if res.Outcome != OutcomeParked || !strings.Contains(res.Reason, "changing reasons") {
		t.Fatalf("outcome %s: %q", res.Outcome, res.Reason)
	}
}
