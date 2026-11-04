package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

func custom(name string) questplan.Step {
	return questplan.Step{Kind: questplan.StepCustom, Custom: name}
}

func snapAt(j questplan.Status, stage int, known bool) questplan.Snapshot {
	return questplan.Snapshot{Journal: j, Stage: stage, StageKnown: known}
}

// The Waterfall run that went strip, kit, raft, falls and was cut off had moved (many different
// steps, and the journal went from not started); the Priest in Peril run that re-provisioned the
// same essence for twenty minutes had not (one step, the stage the same).
func TestARunThatSpunInPlaceDidNotProgress(t *testing.T) {
	var spin progressTracker
	for i := 0; i < 40; i++ {
		spin.note(snapAt(questplan.StatusInProgress, 10, true), custom("provision pip-essence"), "provision pip-essence (2 line(s))")
	}
	if spin.Moved() {
		t.Fatalf("40 passes of one failing step read as progress: %+v", spin.RunProgress)
	}

	var wf progressTracker
	steps := []string{"wf-strip", "provision waterfall-tomb", "use pebble", "wf-chest", "wf-raft"}
	for _, s := range steps {
		wf.note(snapAt(questplan.StatusInProgress, 0, false), custom(s), s)
	}
	if !wf.Moved() {
		t.Fatalf("five different steps did not read as progress: %+v", wf.RunProgress)
	}
}

// Stage and colour moves count; an uncoloured tab at the start is not a quest that was started.
func TestProgressReadsTheJournalStageAndColour(t *testing.T) {
	rose := progressTracker{}
	rose.note(snapAt(questplan.StatusInProgress, 5, true), custom("a"), "a")
	rose.note(snapAt(questplan.StatusInProgress, 6, true), custom("a"), "a")
	if !rose.Moved() {
		t.Error("stage 5 -> 6 is progress")
	}
	same := progressTracker{}
	same.note(snapAt(questplan.StatusInProgress, 5, true), custom("a"), "a")
	same.note(snapAt(questplan.StatusInProgress, 5, true), custom("b"), "b")
	if same.Moved() {
		t.Error("two steps and the same stage is not progress")
	}
	started := progressTracker{}
	started.note(snapAt(questplan.StatusNotStarted, 0, false), custom("a"), "a")
	started.note(snapAt(questplan.StatusInProgress, 1, true), custom("a"), "a")
	if !started.Moved() {
		t.Error("not started -> in progress is progress")
	}
	unloaded := progressTracker{}
	unloaded.note(snapAt(questplan.StatusUnknown, 0, false), custom("a"), "a")
	unloaded.note(snapAt(questplan.StatusInProgress, 1, true), custom("a"), "a")
	if unloaded.Moved() {
		t.Error("a tab that was not coloured yet is not a quest that was started")
	}
	// A stage first read late is the baseline, not a rise from zero.
	late := progressTracker{}
	late.note(snapAt(questplan.StatusInProgress, 0, false), custom("a"), "a")
	late.note(snapAt(questplan.StatusInProgress, 10, true), custom("a"), "a")
	late.note(snapAt(questplan.StatusInProgress, 10, true), custom("a"), "a")
	if late.Moved() {
		t.Error("a first stage reading of 10 is a baseline, not a rise")
	}
	// Waits are not steps.
	waits := progressTracker{}
	for i := 0; i < 20; i++ {
		waits.note(snapAt(questplan.StatusInProgress, 3, true), questplan.Step{Kind: questplan.StepWait, Reason: "w"}, "wait")
	}
	if waits.Steps != 0 || waits.Moved() {
		t.Errorf("waiting is not progress: %+v", waits.RunProgress)
	}
}

// How a timeout is remembered: a slot the pass budget cut short leaves no mark; a slot the quest
// ran whole with progress parks it for five minutes only and never raises Parks; one that spun
// parks like any failure, so the backoff still escalates for a quest that is really wedged.
func TestSlotEndsAreRememberedByWhatHappened(t *testing.T) {
	now := time.Now()
	e := &Engine{Store: questplan.NewStore()}

	e.noteSlotEnd("clipped", Result{Outcome: OutcomeTimeout, LastStep: "x", Progressed: true}, true)
	if parked, _ := e.Store.Parked("clipped", now); parked || e.Store.Record("clipped").Parks != 0 {
		t.Error("a clipped slot must leave no mark")
	}

	for i := 0; i < 6; i++ {
		e.noteSlotEnd("moved", Result{Outcome: OutcomeTimeout, LastStep: "use pebble", Progressed: true}, false)
	}
	rec := e.Store.Record("moved")
	if rec.Parks != 0 {
		t.Errorf("six progressed timeouts raised Parks to %d", rec.Parks)
	}
	if parked, why := e.Store.Parked("moved", now); !parked || why != "budget ran out: use pebble" {
		t.Errorf("a progressed timeout should still hold the quest briefly: parked %v %q", parked, why)
	}
	if until := time.UnixMilli(rec.BackoffUntil); until.Sub(now) > questplan.Backoff(1)+time.Minute {
		t.Errorf("a progressed timeout waits only the shortest backoff, got until %v", until.Sub(now))
	}

	for i := 0; i < 5; i++ {
		e.noteSlotEnd("spun", Result{Outcome: OutcomeTimeout, LastStep: "provision pip-essence"}, false)
	}
	rec = e.Store.Record("spun")
	if rec.Parks != 5 {
		t.Errorf("five spun timeouts should be five parks, got %d", rec.Parks)
	}
	if time.UnixMilli(rec.BackoffUntil).Sub(now) < 100*time.Minute {
		t.Errorf("the fifth park of a quest that spins is the two hour backoff, got %v", time.UnixMilli(rec.BackoffUntil).Sub(now))
	}

	// No store: nothing to do, nothing to panic over.
	(&Engine{}).noteSlotEnd("x", Result{Outcome: OutcomeTimeout}, false)
}
