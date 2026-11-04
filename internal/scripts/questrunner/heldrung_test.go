package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// THE HOLD: an account the quest-point fillers carried to 36 quest points, at level 99 in every skill,
// meets every requirement Dragon Slayer states and every gate the table names, and the ladder still does
// not start it. The module has never run live (questplan.HeldRungs, W19).
func TestRunLadderDoesNotStartAHeldRungThatMeetsEveryRequirement(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, nil)
	seedQuestPoints(e.D.Session.State(), 36)
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.DragonSlayer, done)}, time.Hour, time.Hour)
	if done["dragon"] || sink.has("running dragon") {
		t.Fatalf("Dragon Slayer started on an account at 36 quest points: %v", sink.lines)
	}
	if res.Outcome != LadderStuck {
		t.Errorf("outcome %s, want stuck (the only quest is held)", res.Outcome)
	}
	if got := strings.Join(res.Blocked["dragon"], " | "); !strings.Contains(got, "held back: ") || !strings.Contains(got, "W19") {
		t.Errorf("blocked reasons %q do not say the rung is held", got)
	}
	if !sink.has("[ladder] dragon is held back, not run: ") {
		t.Errorf("the log does not say why Dragon Slayer did not start: %v", sink.lines)
	}
	// No slot was taken for it: a held rung must not occupy the one-at-a-time quest slot of the fleet.
	if sink.has("slot") {
		t.Errorf("a slot was taken for a held rung: %v", sink.lines)
	}
}

// The control: with the entry lifted, the same account runs it. So the hold is the only thing in the way,
// and the test above fails when the hold is gone.
func TestRunLadderStartsTheRungOnceItsHoldIsLifted(t *testing.T) {
	was := questplan.HeldRungs
	t.Cleanup(func() { questplan.HeldRungs = was })
	questplan.HeldRungs = map[string]string{}

	sink := &logSink{}
	e := fakeEngineAt(sink, nil)
	seedQuestPoints(e.D.Session.State(), 36)
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.DragonSlayer, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done["dragon"] || sink.has("held back") {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
}

// Short of the 32 the log reads as it always did, naming the quest points and not a hold: the held line
// belongs to the accounts the quest would otherwise have started on.
func TestRunLadderReadsHeldRungShortOfItsQuestPointsAsBefore(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, nil)
	seedQuestPoints(e.D.Session.State(), 20)
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.DragonSlayer, done)}, time.Hour, time.Hour)
	if done["dragon"] || res.Outcome != LadderStuck {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
	got := strings.Join(res.Blocked["dragon"], " | ")
	if !strings.Contains(got, "needs 32 quest points (have 20)") || strings.Contains(got, "held back") {
		t.Errorf("blocked reasons %q", got)
	}
	if sink.has("dragon is held back, not run: its module") {
		t.Errorf("the hold line appeared for an account short of the quest points: %v", sink.lines)
	}
}

// A held rung is said once a call however many passes the loop makes, and it does not stop the quests
// behind it from running.
func TestRunLadderSaysAHeldRungOnceAndRunsTheOthers(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, nil)
	seedQuestPoints(e.D.Session.State(), 36)
	done := map[string]bool{}
	qs := []Quest{
		recordQuest(questplan.DragonSlayer, done),
		instantQuest("hetty", done),
		instantQuest("imp", done),
	}
	res := e.RunLadder(qs, time.Hour, time.Hour)
	if res.Outcome != LadderStuck || done["dragon"] || !done["hetty"] || !done["imp"] {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
	n := 0
	for _, l := range sink.lines {
		if strings.Contains(l, "dragon is held back") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the hold was logged %d times, want once: %v", n, sink.lines)
	}
}
