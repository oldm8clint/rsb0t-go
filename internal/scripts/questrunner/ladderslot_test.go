package questrunner

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
)

// A quest the test completes at once: its module's Decide says done, and Done reads a flag Finish
// sets (the quest tab of a fake session never turns green).
func instantQuest(id string, done map[string]bool) Quest {
	return Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: id, Name: id},
			Decide: func(questplan.Snapshot) questplan.Step { return questplan.Step{Kind: questplan.StepDone} },
		},
		Done:   func(*Engine) bool { return done[id] },
		Finish: func(*Engine) { done[id] = true },
	}
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *logSink) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// fakeEngine is an engine whose account meets every level gate (laddergate_test.go's fakeEngineAt
// with no levels given): these tests are about slots, parks and prerequisites.
func fakeEngine(sink *logSink) *Engine { return fakeEngineAt(sink, nil) }

func fakeEngine0(sink *logSink) *Engine {
	d := &Deps{
		Session: fakeSession{gamestate.New()},
		Delay:   func(time.Duration) bool { return true },
		Log:     sink.add,
	}
	return &Engine{D: d, Store: questplan.NewStore()}
}

// Each quest runs under its own slot (SlotFor), not one number for all: the log line says which.
func TestRunLadderGivesEachQuestItsOwnSlot(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.SlotFor = func(id string) time.Duration { return questplan.QuestSlot(id, 20*time.Minute) }
	done := map[string]bool{}
	res := e.RunLadder([]Quest{instantQuest("waterfall", done), instantQuest("hetty", done)}, 20*time.Minute, time.Hour)
	if res.Outcome != LadderDone || !res.Progressed {
		t.Fatalf("outcome %s progressed %v, want done and progressed (%v)", res.Outcome, res.Progressed, sink.lines)
	}
	if !sink.has("running waterfall (0/2 complete, slot 50m0s)") {
		t.Errorf("Waterfall should run in a 50 minute slot: %v", sink.lines)
	}
	if !sink.has("running hetty (1/2 complete, slot 31m15s)") {
		t.Errorf("Hetty (25 minutes live) gets 31m15s: %v", sink.lines)
	}
}

// Without SlotFor the ladder behaves as before: perQuest for every quest.
func TestRunLadderWithoutSlotForUsesPerQuest(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}
	res := e.RunLadder([]Quest{instantQuest("waterfall", done)}, 33*time.Minute, time.Hour)
	if res.Outcome != LadderDone {
		t.Fatalf("outcome %s (%v)", res.Outcome, sink.lines)
	}
	if !sink.has("slot 33m0s") {
		t.Errorf("perQuest should be the slot: %v", sink.lines)
	}
}

// A quest whose slot does not fit half of what is left is not started and releases nothing it
// does not hold; the pass ends as out of budget with the first quest done. The first quest of a
// pass always runs, whatever its slot against the budget.
func TestRunLadderDoesNotStartAQuestThatCannotFitHalfItsSlot(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.SlotFor = func(id string) time.Duration {
		if id == "pip" {
			return 100 * time.Minute
		}
		return 10 * time.Minute
	}
	done := map[string]bool{}
	res := e.RunLadder([]Quest{instantQuest("hetty", done), instantQuest("pip", done)}, 20*time.Minute, 30*time.Minute)
	if res.Outcome != LadderBudget {
		t.Fatalf("outcome %s, want the pass out of budget (%v)", res.Outcome, sink.lines)
	}
	if !done["hetty"] || done["pip"] {
		t.Errorf("hetty should have run and pip not: %v", done)
	}
	if !sink.has("not starting pip") {
		t.Errorf("the refusal should be logged: %v", sink.lines)
	}

	// The same slot as the FIRST quest of a pass runs, clipped to the budget.
	sink2 := &logSink{}
	e2 := fakeEngine(sink2)
	e2.SlotFor = e.SlotFor
	done2 := map[string]bool{}
	res2 := e2.RunLadder([]Quest{instantQuest("pip", done2)}, 20*time.Minute, 30*time.Minute)
	if res2.Outcome != LadderDone || !done2["pip"] {
		t.Fatalf("a first quest must run even when its slot is over the budget: %s (%v)", res2.Outcome, sink2.lines)
	}
}
