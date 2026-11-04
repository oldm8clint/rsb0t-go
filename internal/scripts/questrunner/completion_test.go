package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// workThenDone is a quest that takes one custom step and then reports Done.
func workThenDone(id string) Quest {
	worked := false
	return Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: id, Name: id},
			Decide: func(questplan.Snapshot) questplan.Step {
				if worked {
					return questplan.Step{Kind: questplan.StepDone}
				}
				return questplan.Step{Kind: questplan.StepCustom, Custom: "work"}
			},
		},
		Customs: map[string]CustomFunc{"work": func(*Engine, questplan.Step) bool { worked = true; return true }},
	}
}

// A quest that ends complete leaves the evidence cmd/questpromote counts: the store says which build and
// account finished it and after how many decisions, and the log carries the same line on its own.
func TestACompletionIsRecordedWithItsBuildAndAccount(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	build := strings.Repeat("c", 40)
	e.Account, e.Build = "Test Player", build
	before := time.Now().UnixMilli()

	if res := e.Run(workThenDone("ghost"), time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s (%v)", res.Outcome, sink.lines)
	}
	cs := e.Store.Record("ghost").Completions
	if len(cs) != 1 {
		t.Fatalf("completions %+v, want exactly one", cs)
	}
	c := cs[0]
	if c.Account != "Test Player" || c.Build != build || c.Ticks != 2 || c.At < before || !c.Earned() {
		t.Fatalf("completion %+v", c)
	}
	want := "[ghost] COMPLETED build=" + build + " ticks=2 account=Test Player"
	if !sink.has(want) {
		t.Fatalf("the log has no %q: %v", want, sink.lines)
	}
}

// A quest that is already done when the run starts reports Done on its first decision. It is recorded
// (the run did end complete) but marked as having done no work, so it is not counted as evidence.
func TestAQuestDoneBeforeTheRunDidNoWork(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.Account, e.Build = "x", "unknown"
	q := instantQuest("imp", map[string]bool{})
	if res := e.Run(q, time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s (%v)", res.Outcome, sink.lines)
	}
	cs := e.Store.Record("imp").Completions
	if len(cs) != 1 || cs[0].Ticks != 1 || cs[0].Earned() {
		t.Fatalf("completions %+v, want one that did no work", cs)
	}
}

// A lab harness has no account and no build: the completion is still recorded and logged.
func TestACompletionWithoutAnIdentityIsStillRecorded(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	if res := e.Run(workThenDone("hetty"), time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if cs := e.Store.Record("hetty").Completions; len(cs) != 1 || cs[0].Build != "" {
		t.Fatalf("completions %+v", cs)
	}
	if !sink.has("[hetty] COMPLETED build=unknown ticks=2 account=") {
		t.Fatalf("log: %v", sink.lines)
	}
}

// Fight Arena's way-out-only pass returns Done while the journal still reads in progress: that run got a
// trapped account out and finished nothing, so it is no completion. Rungs and miniquests have no tab.
func TestOnlyADoneThatEndsTheQuestIsACompletion(t *testing.T) {
	for j, want := range map[questplan.Status]bool{
		questplan.StatusComplete:   true,
		questplan.StatusUnknown:    true,
		questplan.StatusInProgress: false,
		questplan.StatusNotStarted: false,
	} {
		if got := endsTheQuest(j); got != want {
			t.Errorf("endsTheQuest(%s) = %v, want %v", j, got, want)
		}
	}
}
