package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// A quest with no quest tab (the Barcrawl miniquest) is complete by its own Done, which reads the side store's
// SeenComplete flag its custom step sets once the guard has taken the card. The engine's StepDone used to write
// SeenComplete back from the quest TAB (always "unknown" for such a quest), erasing what the step had just recorded: the
// tour could finish, the hand-in could succeed and the ladder would still read "barcrawl not complete", so Scorpion
// Catcher and Horror from the Deep (whose prerequisite it is) stayed held back for good.
func TestAStepDoneKeepsWhatAQuestWithItsOwnDoneRecorded(t *testing.T) {
	const id = "minitour"
	sink := &logSink{}
	e := fakeEngine(sink)
	q := Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: id, Name: "A miniquest with no tab"},
			Decide: func(s questplan.Snapshot) questplan.Step {
				if s.Ran["tour"] {
					return questplan.Step{Kind: questplan.StepDone}
				}
				return questplan.Step{Kind: questplan.StepCustom, Custom: "tour", Reason: "the whole tour"}
			},
		},
		Customs: map[string]CustomFunc{"tour": func(e *Engine, _ questplan.Step) bool {
			e.Store.Record(id).SeenComplete = true // what barcrawlEnsure records once the guard has the card
			return true
		}},
		Done: func(e *Engine) bool { return e.Store != nil && e.Store.Record(id).SeenComplete },
	}

	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeComplete {
		t.Fatalf("the run ended %s (%s), want complete: %v", res.Outcome, res.Reason, sink.lines)
	}
	if !q.Done(e) {
		t.Fatalf("the quest's own Done reads false after its tour was handed in and the run ended complete: StepDone wiped SeenComplete (%v)", sink.lines)
	}
	if done := e.completedQuests([]Quest{q}); !done[id] {
		t.Errorf("the ladder reads %v after the run, want the miniquest complete", done)
	}
}

// The other side of the same line: a tab quest's StepDone still records what the tab said, and a quest with its own Done
// that never recorded anything is not made complete by reaching StepDone.
func TestAStepDoneStillRecordsTheTabForAQuestWithoutADone(t *testing.T) {
	e := fakeEngine(&logSink{})
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "tabbed", Name: "tabbed"},
		Decide: func(questplan.Snapshot) questplan.Step { return questplan.Step{Kind: questplan.StepDone} },
	}}
	e.Store.Record("tabbed").SeenComplete = true // a stale hint from an earlier run
	if res := e.Run(q, time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if e.Store.Record("tabbed").SeenComplete {
		t.Error("a tab quest whose tab does not read complete kept a stale SeenComplete after StepDone")
	}

	e = fakeEngine(&logSink{})
	q.Done = func(*Engine) bool { return false } // its own Done, and nothing ever recorded
	if res := e.Run(q, time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if e.Store.Record("tabbed").SeenComplete {
		t.Error("reaching StepDone made a quest with its own Done 'seen complete' without anything having recorded it")
	}
}
