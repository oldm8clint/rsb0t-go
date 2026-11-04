package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/scripts/questplan"
)

// A custom step with a death cap (Module.StepDeathCaps) that kills the account for the cap-th
// time inside the window parks the whole quest for the cap's skip, not the short escalating
// backoff that sent accounts back into the Khazard warlord four times on 4 Oct.
func TestAStepDeathCapParksTheQuestForItsSkip(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.D.Chat = chatlog.New()
	now := time.Now()
	e.Store.NoteStepDeath("fightquest", "fight", now.Add(-time.Hour), 24*time.Hour)
	runs := 0
	q := Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: "fightquest", Name: "fightquest"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "fight"}
			},
			MaxDeaths: 5,
			StepDeathCaps: map[string]questplan.StepDeathCap{
				"fight": {Max: 2, Window: 24 * time.Hour, Skip: 24 * time.Hour},
			},
		},
		Customs: map[string]CustomFunc{"fight": func(e *Engine, _ questplan.Step) bool {
			runs++
			e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}},
	}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || runs != 1 || !strings.Contains(res.Reason, `2 death(s) on "fight"`) {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, sink.lines)
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(23*time.Hour)); !parked {
		t.Fatal("not parked for the day")
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(24*time.Hour+time.Minute)); parked {
		t.Fatal("parked past the skip")
	}
	if r := e.Store.Record("fightquest"); r.Deaths != 1 || len(r.StepDeaths["fight"]) != 2 {
		t.Fatalf("record %+v", r)
	}
}

// A quest at its MaxDeaths is skipped for a day after the death that reaches it, not for the
// 5-40 minute Backoff that sent mackerel back to the Black Demon eight minutes after its cap.
func TestMaxDeathsParksTheQuestForADay(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.D.Chat = chatlog.New()
	e.Store.NoteDeath("capquest", time.Now().Add(-time.Hour))
	q := Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: "capquest", Name: "capquest"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "fight"}
			},
			MaxDeaths: 2,
		},
		Customs: map[string]CustomFunc{"fight": func(e *Engine, _ questplan.Step) bool {
			e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}},
	}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 2") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
	if parked, _ := e.Store.Parked("capquest", time.Now().Add(23*time.Hour)); !parked {
		t.Fatal("not parked for the day")
	}
	if parked, _ := e.Store.Parked("capquest", time.Now().Add(MaxDeathsSkip+time.Minute)); parked {
		t.Fatal("parked past the skip")
	}
}

// A custom step that has learned trying again changes nothing (the Black Knight Titan that did not fall in 25 minutes,
// 6 Oct 2026) asks the engine to park the quest for a long skip: parked in the side store, the step never run twice.
func TestRequestParkParksTheQuestForItsSkipAndRunsTheStepOnce(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	runs := 0
	q := Quest{
		Module: questplan.Module{
			Record: questplan.Record{ID: "titanquest", Name: "titanquest"},
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "titan"}
			},
		},
		Customs: map[string]CustomFunc{"titan": func(e *Engine, _ questplan.Step) bool {
			runs++
			e.RequestPark("the Titan did not fall", 12*time.Hour)
			return false
		}},
	}
	res := e.Run(q, time.Minute)
	if res.Outcome != OutcomeParked || runs != 1 || res.Reason != "the Titan did not fall" {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, sink.lines)
	}
	if parked, why := e.Store.Parked("titanquest", time.Now().Add(11*time.Hour)); !parked || why != "the Titan did not fall" {
		t.Fatalf("not parked for the skip: %v %q", parked, why)
	}
	if parked, _ := e.Store.Parked("titanquest", time.Now().Add(12*time.Hour+time.Minute)); parked {
		t.Fatal("parked past the skip")
	}
	if !strings.Contains(strings.Join(sink.lines, "\n"), "parking for 12h0m0s: the Titan did not fall") {
		t.Fatalf("the park was not logged: %v", sink.lines)
	}
}
