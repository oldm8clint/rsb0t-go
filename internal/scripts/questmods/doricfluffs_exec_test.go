package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The executors' paths that need no map: the seasoning on a simulated pack, and the early returns that must not send an account
// anywhere.

type execWorld struct {
	sim *actionsim.Session
	e   *questrunner.Engine
	log *[]string
}

func newExecWorld(pack ...actionsim.Stack) *execWorld {
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, pack)
	sim.ShowTabs()
	var lines []string
	d := &questrunner.Deps{Session: sim, Components: actionsim.TabComponents(),
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		Log:   func(s string) { lines = append(lines, s) }}
	return &execWorld{sim: sim, e: &questrunner.Engine{D: d}, log: &lines}
}

func (w *execWorld) held(id int) int {
	n := 0
	for _, r := range w.sim.Pack() {
		if int(r.ID) == id {
			n += r.Count
		}
	}
	return n
}

func TestFluffsSeasonRubsTheLeavesOverTheSardine(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.FluffsDoogleLeaves, Count: 1}, actionsim.Stack{ID: questplan.FluffsRawSardine, Count: 1},
		actionsim.Stack{ID: questplan.ObjBucketOfMilk, Count: 1})
	w.sim.Combine(questplan.FluffsDoogleLeaves, questplan.FluffsRawSardine, questplan.FluffsSeasonedSardine)
	if !fluffsSeason(w.e, questplan.Step{}) {
		t.Fatalf("the seasoning failed: %v", *w.log)
	}
	if w.held(questplan.FluffsSeasonedSardine) != 1 || w.held(questplan.FluffsDoogleLeaves) != 0 || w.held(questplan.FluffsRawSardine) != 0 ||
		w.held(questplan.ObjBucketOfMilk) != 1 {
		t.Errorf("pack %+v, want the seasoned sardine, the milk and nothing else", w.sim.Pack())
	}
	// Seasoned already: nothing to do and nothing spent.
	if !fluffsSeason(w.e, questplan.Step{}) || w.held(questplan.FluffsSeasonedSardine) != 1 {
		t.Error("a second seasoning disturbed the pack")
	}
}

func TestFluffsSeasonWithoutBothPartsFailsAtOnce(t *testing.T) {
	for name, pack := range map[string][]actionsim.Stack{
		"no sardine": {{ID: questplan.FluffsDoogleLeaves, Count: 1}},
		"no leaves":  {{ID: questplan.FluffsRawSardine, Count: 1}},
		"empty":      nil,
	} {
		w := newExecWorld(pack...)
		w.sim.Combine(questplan.FluffsDoogleLeaves, questplan.FluffsRawSardine, questplan.FluffsSeasonedSardine)
		if fluffsSeason(w.e, questplan.Step{}) {
			t.Errorf("%s: the seasoning succeeded", name)
		}
		if !strings.Contains(strings.Join(*w.log, "\n"), "nothing to season") {
			t.Errorf("%s: the log does not say why: %v", name, *w.log)
		}
	}
}

// An offer whose gift has already gone (a run that gave it, then lost the engine's tick) is done: it does not walk to the yard.
func TestFluffsOfferOfAGiftAlreadyGivenIsDoneAtOnce(t *testing.T) {
	w := newExecWorld()
	if !fluffsOffer(w.e, questplan.Step{ItemID: questplan.FluffsKitten}) {
		t.Errorf("an offer of a kitten the pack does not hold failed: %v", *w.log)
	}
}

// The kitten's crate search is already done when the pack holds the kitten.
func TestFluffsCratesWithTheKittenInHandIsDoneAtOnce(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.FluffsKitten, Count: 1})
	if !fluffsCrates(w.e, questplan.Step{}) {
		t.Errorf("the crate search failed with the kitten in the pack: %v", *w.log)
	}
}

// The miner refuses an account without a pickaxe it can use, and one with nothing owed, without a step of walking.
func TestDoricMineRefusesWithoutAUsablePickaxeOrAnythingOwed(t *testing.T) {
	w := newExecWorld()
	if doricMine(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no pickaxe") {
		t.Errorf("an empty pack mined: %v", *w.log)
	}
	// A pickaxe and every ore owed, but the simulated session reports no Mining level: nothing is mineable, and the step says so
	// and returns without a step of walking.
	w = newExecWorld(actionsim.Stack{ID: 1265, Count: 1})
	if doricMine(w.e, questplan.Step{}) {
		t.Errorf("mined without a Mining level the session could report: %v", *w.log)
	}
}
