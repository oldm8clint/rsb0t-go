package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// A training rung has no journal and runs one step for its whole slot, so the progress rule that reads steps and stages
// called every timed-out slot of the Prayer rungs "spun" and parked it 5, 10, 20, 40 minutes and then 2 hours for ever (the
// store of every account that ran prayerkq on 16 Oct 2026: attempts 7, parks 6, backoff 2 hours). What a rung can say is
// the experience it gained (Engine.NoteTrained).

// trainingQuest is a rung whose one step reports xp experience gained on its first call and records what the run had left.
func trainingQuest(xp int, calls *int, left *time.Duration) Quest {
	return Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: "rung", Name: "rung"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "train", Reason: "train"}
			},
		},
		Customs: map[string]CustomFunc{"train": func(e *Engine, _ questplan.Step) bool {
			*calls++
			if *calls == 1 {
				*left = e.RunLeft()
				e.NoteTrained(xp)
			}
			return true
		}},
	}
}

func TestARungThatTrainedTimesOutHavingProgressed(t *testing.T) {
	for _, c := range []struct {
		xp   int
		want bool
	}{{ProgressTrainXP, true}, {405, true}, {ProgressTrainXP - 1, false}, {55, false}, {0, false}} {
		sink := &logSink{}
		e := fakeEngine(sink)
		e.D.Delay = func(time.Duration) bool { time.Sleep(15 * time.Millisecond); return true }
		var calls int
		var left time.Duration
		r := e.Run(trainingQuest(c.xp, &calls, &left), 80*time.Millisecond)
		if r.Outcome != OutcomeTimeout {
			t.Fatalf("%d xp: outcome %s (%s), want a timeout: %v", c.xp, r.Outcome, r.Reason, sink.lines)
		}
		if r.Progressed != c.want {
			t.Errorf("a rung that gained %d xp: Progressed %v, want %v", c.xp, r.Progressed, c.want)
		}
		if left <= 0 || left > 80*time.Millisecond {
			t.Errorf("RunLeft inside the run was %s, want a share of its 80ms", left)
		}
		// What the ladder then writes: a progressed rung waits the shortest backoff and its park count does not rise.
		e.noteSlotEnd("rung", r, false)
		if rec := e.Store.Record("rung"); c.want && rec.Parks != 0 || !c.want && rec.Parks != 1 {
			t.Errorf("%d xp: Parks %d after the timeout", c.xp, rec.Parks)
		}
	}
}

// The experience of one run is not carried into the next.
func TestTrainedExperienceIsPerRun(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.D.Delay = func(time.Duration) bool { time.Sleep(15 * time.Millisecond); return true }
	var calls int
	var left time.Duration
	if r := e.Run(trainingQuest(500, &calls, &left), 60*time.Millisecond); !r.Progressed {
		t.Fatal("the first run gained 500 xp and did not progress")
	}
	calls = 0
	if r := e.Run(trainingQuest(0, &calls, &left), 60*time.Millisecond); r.Progressed {
		t.Error("the second run gained nothing and inherited the first one's progress")
	}
}

func TestRunLeftIsZeroOutsideARun(t *testing.T) {
	e := &Engine{}
	if got := e.RunLeft(); got != 0 {
		t.Errorf("RunLeft with no run = %s", got)
	}
	e.runDeadline = time.Now().Add(-time.Minute)
	if got := e.RunLeft(); got != 0 {
		t.Errorf("RunLeft after the deadline = %s, want 0", got)
	}
	e.runDeadline = time.Now().Add(time.Hour)
	if got := e.RunLeft(); got < 59*time.Minute || got > time.Hour {
		t.Errorf("RunLeft an hour out = %s", got)
	}
	e.NoteTrained(-5)
	if e.trained != 0 {
		t.Error("a negative amount was counted")
	}
}

func TestProgressCountsTrainedExperience(t *testing.T) {
	p := RunProgress{Steps: 1, TrainedXP: ProgressTrainXP - 1}
	if p.Moved() {
		t.Error("99 xp is not a rung that moved")
	}
	p.TrainedXP = ProgressTrainXP
	if !p.Moved() {
		t.Error("100 xp is")
	}
}

// A rung with nothing left to do ends the run now, as a timeout, and the step is not called again until the deadline.
func TestAnEngineRunEndsAtOnceWhenAStepYields(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.D.Delay = func(time.Duration) bool { time.Sleep(5 * time.Millisecond); return true }
	calls := 0
	q := Quest{
		Module: questplan.Module{Record: questplan.Record{ID: "rung", Name: "rung"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "idle", Reason: "idle"}
			}},
		Customs: map[string]CustomFunc{"idle": func(e *Engine, _ questplan.Step) bool {
			calls++
			e.YieldRun()
			return true
		}},
	}
	start := time.Now()
	r := e.Run(q, 30*time.Second)
	if r.Outcome != OutcomeTimeout || calls != 1 || time.Since(start) > 5*time.Second {
		t.Errorf("outcome %s after %s with %d call(s): want an immediate timeout from one call", r.Outcome, time.Since(start), calls)
	}
	// A yield is for one run only.
	calls = 0
	e.Run(q, 20*time.Millisecond)
	if calls != 1 {
		t.Errorf("the next run called the step %d times", calls)
	}
}
