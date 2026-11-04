package questrunner

import "rsb0t-go/internal/scripts/questplan"

// Did a run that ended in a timeout move its quest on?
//
// A timeout is two different things. The Waterfall run that went strip, kit, raft, falls, pebble
// and was cut off at the tomb is a quest that needed a longer slot; the Priest in Peril run that
// bought no essence and re-provisioned it for twenty minutes is a quest that spun in place. The
// ladder parked both the same way (5, 10, 20, 40 minutes, then 2 hours for ever), so after the
// fifth timeout every quest sat out two hours between tries however far each try had got.

// ProgressSteps is how many different steps a run must have gone through to count as having moved
// when the journal cannot say (a module with no stage reading): a step that failed and was
// retried is one step, a walk, a talk and a loc each add one.
const ProgressSteps = 4

// ProgressTrainXP is how much experience a training rung (Engine.NoteTrained) must have gained in a run for the run
// to have moved: a rung has no journal, and a step it repeats for the whole slot is one step, so the run that killed
// cows for twenty minutes and the run that spun in place at the bank door read the same. A slot of cow kills was 58 xp
// on 16 Oct 2026 (175 xp an hour, 26 accounts), which is under it; a pack of big bones is 405.
const ProgressTrainXP = 100

// RunProgress is what a run reads about itself as it goes.
type RunProgress struct {
	// Started and Ended are the journal colour at the first and the last decide pass.
	Started, Ended questplan.Status
	// StartStage and EndStage are the module's stage readings, valid with the Known flags.
	StartStage, EndStage int
	StartKnown, EndKnown bool
	// Steps are the different non-wait steps the run described.
	Steps int
	// TrainedXP is the experience a training rung's steps reported gaining (Engine.NoteTrained).
	TrainedXP int
}

// Moved reports whether the run advanced the quest.
func (p RunProgress) Moved() bool {
	if p.StartKnown && p.EndKnown && p.EndStage > p.StartStage {
		return true
	}
	// Started and finished are the colour moves; an uncoloured tab (StatusUnknown) at either end
	// says nothing, so a tab that had not loaded yet is not a quest that was started.
	switch {
	case p.Started == questplan.StatusNotStarted && (p.Ended == questplan.StatusInProgress || p.Ended == questplan.StatusComplete):
		return true
	case p.Started == questplan.StatusInProgress && p.Ended == questplan.StatusComplete:
		return true
	}
	return p.Steps >= ProgressSteps || p.TrainedXP >= ProgressTrainXP
}

// progressTracker accumulates a RunProgress over one Run's decide passes.
type progressTracker struct {
	RunProgress
	seen    map[string]bool
	started bool
}

// note records one decide pass: the snapshot it read, the step it chose and the step's text.
func (t *progressTracker) note(snap questplan.Snapshot, step questplan.Step, desc string) {
	if !t.started {
		t.started = true
		t.Started = snap.Journal
	}
	t.Ended = snap.Journal
	if snap.StageKnown {
		if !t.StartKnown {
			t.StartStage, t.StartKnown = snap.Stage, true
		}
		t.EndStage, t.EndKnown = snap.Stage, true
	}
	if step.Kind == questplan.StepWait || step.Kind == questplan.StepDone {
		return
	}
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	if !t.seen[desc] {
		t.seen[desc] = true
		t.Steps++
	}
}
