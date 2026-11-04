package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// A step that gives a fight up (Deps.noteRetreat, which retreatFromFight calls) is a near-death: the
// engine parks the quest, whatever the step returns, and counts it in the store.
func nearDeathQuest(id string, max int, returns bool, runs *int) Quest {
	return Quest{
		Module: questplan.Module{
			Record:        questplan.Record{ID: id, Name: id},
			MaxNearDeaths: max,
			Decide: func(questplan.Snapshot) questplan.Step {
				return questplan.Step{Kind: questplan.StepCustom, Custom: "fight"}
			},
		},
		Customs: map[string]CustomFunc{"fight": func(e *Engine, _ questplan.Step) bool {
			*runs++
			e.D.noteRetreat("Monk of Zamorak: ate 6 times and the target's health has not gone down")
			return returns
		}},
	}
}

func TestALostFightParksTheQuestForTheShortBackoff(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	runs := 0
	res := e.Run(nearDeathQuest("fightquest", 0, false, &runs), time.Minute)
	if res.Outcome != OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "lost a fight") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, sink.lines)
	}
	if !sink.has("LOST A FIGHT during") || !sink.has("near-death 1, the cap is 2") {
		t.Errorf("the log does not say plainly what happened: %v", sink.lines)
	}
	r := e.Store.Record("fightquest")
	if len(r.NearDeaths) != 1 || r.Deaths != 0 || r.Parks != 1 {
		t.Fatalf("record %+v", r)
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(4*time.Minute)); !parked {
		t.Fatal("not parked for the first backoff")
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(6*time.Minute)); parked {
		t.Fatal("the first lost fight parked past the 5 minute backoff: only the cap parks for a day")
	}
}

func TestTheSecondLostFightInADaySkipsTheQuestForADay(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.Store.NoteNearDeath("fightquest", time.Now().Add(-3*time.Hour), questplan.NearDeathWindow)
	runs := 0
	res := e.Run(nearDeathQuest("fightquest", 0, true, &runs), time.Minute)
	if res.Outcome != OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 2") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(23*time.Hour)); !parked {
		t.Fatal("not parked for the day")
	}
	if parked, _ := e.Store.Parked("fightquest", time.Now().Add(MaxDeathsSkip+time.Minute)); parked {
		t.Fatal("parked past the skip")
	}
}

// A lost fight from before the window is forgotten.
func TestAnOldLostFightDoesNotCount(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	e.Store.NoteNearDeath("fightquest", time.Now().Add(-questplan.NearDeathWindow-time.Hour), questplan.NearDeathWindow)
	runs := 0
	res := e.Run(nearDeathQuest("fightquest", 0, true, &runs), time.Minute)
	if res.Outcome != OutcomeParked || strings.Contains(res.Reason, "the quest's cap") {
		t.Fatalf("outcome %s: %q", res.Outcome, res.Reason)
	}
	if n := len(e.Store.Record("fightquest").NearDeaths); n != 1 {
		t.Fatalf("%d near-deaths kept, the expired one should be gone", n)
	}
}

// A module can lower the cap: its fight needs more luck than the default allows.
func TestAModuleCanLowerTheNearDeathCap(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	runs := 0
	res := e.Run(nearDeathQuest("fightquest", 1, false, &runs), time.Minute)
	if res.Outcome != OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, sink.lines)
	}
}

// The engine points a lost fight at the quest's bank, else the nearest one.
func TestRetreatGoesToTheQuestsBankElseTheNearest(t *testing.T) {
	d := &Deps{}
	here := tile.Tile{X: 3410, Z: 3488}
	got, name := d.retreatDest(here)
	if name == "the quest's bank" || got != questplan.NearestBank(here).Tile {
		t.Fatalf("no quest bank: %v %q", got, name)
	}
	d.retreatBank, d.retreatBankSet = tile.Tile{X: 3253, Z: 3420}, true
	if got, name = d.retreatDest(here); got != (tile.Tile{X: 3253, Z: 3420}) || name != "the quest's bank" {
		t.Fatalf("quest bank: %v %q", got, name)
	}
}

// Retreats are taken once.
func TestRetreatsAreTakenOnce(t *testing.T) {
	d := &Deps{}
	d.noteRetreat("a")
	d.noteRetreat("b")
	if n, why := d.takeRetreats(); n != 2 || why != "b" {
		t.Fatalf("%d %q", n, why)
	}
	if n, _ := d.takeRetreats(); n != 0 {
		t.Fatalf("%d left", n)
	}
}
