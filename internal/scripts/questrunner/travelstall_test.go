package questrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

const strandedWhy = "stranded in the Wizards' Tower west room: the Guardian's escape ladder owns the way out, not this walk"

var mizgog = tile.Tile{X: 3103, Z: 3163, Level: 2}

func TestStrandedWalksParkAfterThree(t *testing.T) {
	var s travelStalls
	for i := 1; i < StrandedTravelParkRounds; i++ {
		s.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy)
		if why := s.parkReason(); why != "" {
			t.Fatalf("parked after %d stranded walks: %q", i, why)
		}
	}
	// A different target counts too: the account is stranded whatever it was walking to.
	s.note(tile.Tile{X: 3093, Z: 3243}, webwalk.OutcomeUnreachable, strandedWhy)
	why := s.parkReason()
	if !strings.Contains(why, "stranded") || !strings.Contains(why, "3 walks") {
		t.Fatalf("not parked after %d stranded walks: %q", StrandedTravelParkRounds, why)
	}
}

func TestOnlyUnreachableWalksCount(t *testing.T) {
	var s travelStalls
	for i := 0; i < 20; i++ {
		s.note(mizgog, webwalk.OutcomeBudget, "out of time")
		s.note(mizgog, webwalk.OutcomeStopped, "stop requested")
	}
	if why := s.parkReason(); why != "" {
		t.Fatalf("timeouts and stops parked the quest: %q", why)
	}
}

func TestTheSameUnreachableTargetParksAfterFive(t *testing.T) {
	var s travelStalls
	for i := 1; i < SameTargetUnreachableParkRounds; i++ {
		s.note(mizgog, webwalk.OutcomeUnreachable, "unreachable from here (best 12 tiles)")
		if why := s.parkReason(); why != "" {
			t.Fatalf("parked after %d walks: %q", i, why)
		}
	}
	// Other targets do not add up to one.
	s.note(tile.Tile{X: 1, Z: 1}, webwalk.OutcomeUnreachable, "unreachable from here (best 3 tiles)")
	if why := s.parkReason(); why != "" {
		t.Fatalf("a different target parked the quest: %q", why)
	}
	s.note(mizgog, webwalk.OutcomeUnreachable, "unreachable from here (best 12 tiles)")
	if why := s.parkReason(); !strings.Contains(why, "3103,3163,2") {
		t.Fatalf("not parked after %d walks to one tile: %q", SameTargetUnreachableParkRounds, why)
	}
}

func TestResetForgetsAFinishedRun(t *testing.T) {
	var s travelStalls
	for i := 0; i < StrandedTravelParkRounds; i++ {
		s.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy)
	}
	s.reset()
	if why := s.parkReason(); why != "" {
		t.Fatalf("still parked after reset: %q", why)
	}
	s.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy)
	if why := s.parkReason(); why != "" {
		t.Fatalf("the old run's walks were remembered: %q", why)
	}
}

// The engine parks a quest whose custom step keeps ending in a stranded walk, for TravelParkFor and with a log line,
// instead of running the step for the whole slot (Spare 25, 32 laps of the Wizards' Tower door).
func TestAQuestWhoseWalksEndStrandedIsParked(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	runs := 0
	q := Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: "imp", Name: "Imp Catcher"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "talk"}
			},
		},
		Customs: map[string]CustomFunc{"talk": func(e *Engine, _ questplan.Step) bool {
			runs++
			e.D.stalls.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy) // what Travel does when the walk ends so
			return false
		}},
	}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || runs != StrandedTravelParkRounds {
		t.Fatalf("outcome %s after %d runs (want parked after %d): %q (%v)", res.Outcome, runs, StrandedTravelParkRounds, res.Reason, sink.lines)
	}
	if !sink.has("parking for 20m0s: its walks cannot get there") {
		t.Errorf("the park is not logged with its reason: %v", sink.lines)
	}
	if parked, _ := e.Store.Parked("imp", time.Now().Add(TravelParkFor-time.Minute)); !parked {
		t.Fatal("not parked for the skip")
	}
	if parked, _ := e.Store.Parked("imp", time.Now().Add(TravelParkFor+time.Minute)); parked {
		t.Fatal("parked past the skip")
	}
}

// Live 16 Oct 2026: an Imp Catcher account in the mime's room walked to its field five times in a minute, each ended
// unreachable, the event ended, and fourteen minutes later the engine read the latched verdict and parked a run that had
// three of its four beads. A walk that arrives at the tile takes the verdict about that tile back.
func TestAnArrivalAtTheTargetTakesTheUnreachableVerdictBack(t *testing.T) {
	var s travelStalls
	field := tile.Tile{X: 2629, Z: 3217}
	for i := 0; i < SameTargetUnreachableParkRounds; i++ {
		s.note(field, webwalk.OutcomeUnreachable, "unreachable from here (best 1542 tiles)")
	}
	if s.parkReason() == "" {
		t.Fatal("the walks did not reach the verdict the test starts from")
	}
	s.arrived(mizgog) // some other tile: nothing is taken back
	if s.parkReason() == "" {
		t.Fatal("an arrival elsewhere took the verdict back")
	}
	s.arrived(field)
	if why := s.parkReason(); why != "" {
		t.Fatalf("the verdict stands after a walk arrived at the tile: %q", why)
	}
	// The count starts again: four more unreachable walks do not park, a fifth does.
	for i := 1; i < SameTargetUnreachableParkRounds; i++ {
		s.note(field, webwalk.OutcomeUnreachable, "unreachable from here (best 12 tiles)")
	}
	if why := s.parkReason(); why != "" {
		t.Fatalf("parked after %d walks since the arrival: %q", SameTargetUnreachableParkRounds-1, why)
	}
	s.note(field, webwalk.OutcomeUnreachable, "unreachable from here (best 12 tiles)")
	if s.parkReason() == "" {
		t.Fatal("not parked after five unreachable walks since the arrival")
	}
}

// An arrival is no answer to the Guardian owning the account: the stranded count and its verdict stand, because the
// Wizards' Tower loop of 7 Oct alternated walks that arrived (the bank) with walks that ended stranded (Mizgog).
func TestAnArrivalDoesNotForgiveStrandedWalks(t *testing.T) {
	var s travelStalls
	for i := 0; i < StrandedTravelParkRounds; i++ {
		s.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy)
	}
	s.arrived(mizgog)
	s.arrived(tile.Tile{X: 3093, Z: 3243})
	if !strings.Contains(s.parkReason(), "stranded") {
		t.Fatalf("an arrival took a stranded verdict back: %q", s.parkReason())
	}
	// And arrivals between stranded walks do not reset their count.
	var u travelStalls
	for i := 0; i < StrandedTravelParkRounds; i++ {
		u.note(mizgog, webwalk.OutcomeUnreachable, strandedWhy)
		if i < StrandedTravelParkRounds-1 {
			u.arrived(tile.Tile{X: 3093, Z: 3243})
		}
	}
	if !strings.Contains(u.parkReason(), "stranded") {
		t.Fatalf("arrivals between stranded walks reset their count: %q", u.parkReason())
	}
}

// offTheMap tells a walk the Guardian owns (a random event's room has no ground in the pack) from one the map can judge.
func TestOffTheMapIsWhereThePackHasNoGround(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := &Deps{Pack: pk}
	mime := tile.Tile{X: 2008, Z: 4764} // where the mime event set an account down on 16 Oct 2026
	field := tile.Tile{X: 2629, Z: 3217}
	if !pk.Walkable(field.X, field.Z, field.Level) || pk.Walkable(mime.X, mime.Z, mime.Level) {
		t.Fatal("the field is not ground or the mime's room is: the case proves nothing")
	}
	if d.offTheMap(field, true, field) {
		t.Error("a walk from the field to the field is off the map")
	}
	if !d.offTheMap(mime, true, field) {
		t.Error("a walk that began in the mime's room is on the map")
	}
	if !d.offTheMap(field, true, mime) {
		t.Error("a walk that ended in the mime's room is on the map")
	}
	if d.offTheMap(mime, false, tile.Tile{}) {
		t.Error("an unread start and no end was judged off the map")
	}
	if (&Deps{}).offTheMap(mime, true, mime) {
		t.Error("with no pack loaded nothing is off the map")
	}
}
