package questmods

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

func tileOf(a [3]int) tile.Tile { return tile.Tile{X: a[0], Z: a[1], Level: a[2]} }

// Ernest the Chicken through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module which
// legs ran, the wait-and-park, the completion record), with every step's EXECUTOR stubbed to play the content's own rule for it
// on a simulated pack. The module's Decide is the real one; only the quest tab's colour and the journal stage, which no
// simulated interface can supply, are read from the rig. The legs' own live halves (the maze, the closet, the fountain) have
// their tests beside this one (ernestmaze_test.go) and in the real-data tests.

type ernestRig struct {
	*fillerRig
	poisoned bool
	bites    int
	// fail names the custom steps whose stub reports failure.
	fail map[string]bool
}

func newErnestRig(t *testing.T, stage int, pack, bank map[int]int) *ernestRig {
	return &ernestRig{fillerRig: newFillerRig(t, stage, pack, bank), fail: map[string]bool{}}
}

// wrap is fillerRig.wrap with the module's own custom steps kept under their names: a talk, a use-on, a deposit and the
// provisioner's kit become the rig's stubs, every custom step is itself.
func (r *ernestRig) wrap(q questrunner.Quest, stubs map[string]questrunner.CustomFunc) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case r.stage == 0:
			s.Journal, s.StageKnown = questplan.StatusNotStarted, false
		case r.stage >= questplan.ErnestComplete:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, r.stage, true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait, questplan.StepCustom:
			return st
		}
		st.Reason = st.Describe()
		if st.Kind == questplan.StepProvision {
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		} else {
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

func (r *ernestRig) stubs() map[string]questrunner.CustomFunc {
	stubs := r.provisionStub(questplan.KitErnest)
	play := func(name string, f func() bool) {
		stubs[name] = func(e *questrunner.Engine, st questplan.Step) bool {
			ok := !r.fail[name] && f()
			r.stubLog("%s -> %v", name, ok)
			return ok
		}
	}
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.Stop.ID {
		case questplan.NpcVeronica:
			if r.stage == questplan.ErnestNotStarted {
				r.stage = questplan.ErnestStarted
			}
		case questplan.NpcOddenstein:
			if r.stage == questplan.ErnestStarted {
				r.stage = questplan.ErnestSpokeOddenstein
			}
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:useOn"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(questplan.ObjPoison, 1) || !r.take(questplan.ObjFishFood, 1) {
			return false
		}
		r.give(questplan.ObjPoisonedFishFood, 1)
		return true
	}
	stubs["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	give := func(id int) func() bool { return func() bool { r.give(id, 1); return true } }
	play(questplan.CustomErnestFishFood, give(questplan.ObjFishFood))
	play(questplan.CustomErnestPoison, give(questplan.ObjPoison))
	play(questplan.CustomErnestSpade, give(questplan.ObjSpade))
	play(questplan.CustomErnestCan, give(questplan.ObjOilCan))
	play(questplan.CustomErnestProbe, func() bool {
		if r.poisoned {
			r.give(questplan.ObjPressureGauge, 1)
		} else {
			r.bites++
		}
		return true
	})
	play(questplan.CustomErnestPour, func() bool {
		if !r.take(questplan.ObjPoisonedFishFood, 1) {
			return false
		}
		r.poisoned = true
		return true
	})
	play(questplan.CustomErnestGauge, func() bool {
		if !r.poisoned {
			return false
		}
		r.give(questplan.ObjPressureGauge, 1)
		return true
	})
	play(questplan.CustomErnestKey, func() bool {
		if r.held(questplan.ObjSpade) == 0 || r.held(questplan.ObjClosetKey) > 0 {
			return false
		}
		r.give(questplan.ObjClosetKey, 1)
		return true
	})
	play(questplan.CustomErnestTube, func() bool {
		if r.held(questplan.ObjClosetKey) == 0 {
			return false
		}
		r.give(questplan.ObjRubberTube, 1)
		return true
	})
	play(questplan.CustomErnestHandIn, func() bool {
		if r.held(questplan.ObjRubberTube) == 0 || r.held(questplan.ObjOilCan) == 0 || r.held(questplan.ObjPressureGauge) == 0 {
			return false
		}
		r.take(questplan.ObjRubberTube, 1)
		r.take(questplan.ObjOilCan, 1)
		r.take(questplan.ObjPressureGauge, 1)
		r.stage = questplan.ErnestComplete
		return true
	})
	return stubs
}

func TestErnestTheChickenRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newErnestRig(t, 0, map[int]int{questplan.CoinsID: 10, 385: 4}, nil)
	q := r.wrap(ErnestTheChicken(), r.stubs())
	if q.ID() != questplan.ErnestChickenID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.ErnestComplete || r.bites != 1 || !r.poisoned {
		t.Errorf("stage %d, %d bites, poisoned %v", r.stage, r.bites, r.poisoned)
	}
	cs := e.Store.Record(questplan.ErnestChickenID).Completions
	if len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	want := []string{"provision ernest", "talk to Veronica", "talk to Professor Oddenstein", questplan.CustomErnestFishFood, questplan.CustomErnestPoison,
		"use Poison on Fish food", questplan.CustomErnestCan, questplan.CustomErnestSpade, questplan.CustomErnestProbe, questplan.CustomErnestPour,
		questplan.CustomErnestGauge, questplan.CustomErnestKey, questplan.CustomErnestTube, questplan.CustomErnestHandIn, "[haunted] COMPLETED"}
	last := -1
	for _, w := range want {
		i := strings.Index(joined[last+1:], w)
		if i < 0 {
			t.Fatalf("the log has no %q after position %d:\n%s", w, last, joined)
		}
		last += 1 + i
	}
	if r.held(385) != 4 || r.held(questplan.CoinsID) != 10 {
		t.Errorf("the food and the coins were touched: %v", r.pack())
	}
}

// Parts the trainer banked are drawn at the one bank visit and no leg is walked for them.
func TestErnestTheChickenDrawsBankedPartsThroughTheEngine(t *testing.T) {
	r := newErnestRig(t, questplan.ErnestSpokeOddenstein, nil, map[int]int{questplan.ObjOilCan: 1, questplan.ObjPressureGauge: 1, questplan.ObjClosetKey: 1})
	q := r.wrap(ErnestTheChicken(), r.stubs())
	res := r.engine().Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	for _, absent := range []string{questplan.CustomErnestCan, questplan.CustomErnestProbe, questplan.CustomErnestKey, questplan.CustomErnestSpade, "talk to Veronica"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q, a leg the bank made unnecessary:\n%s", absent, joined)
		}
	}
	if !strings.Contains(joined, questplan.CustomErnestTube) || !strings.Contains(joined, questplan.CustomErnestHandIn) {
		t.Errorf("the log lacks the tube or the hand-in:\n%s", joined)
	}
}

// A leg that fails parks the quest with the leg's name in the reason, rather than looping.
func TestErnestTheChickenParksOnALegThatFails(t *testing.T) {
	for _, leg := range []string{questplan.CustomErnestCan, questplan.CustomErnestProbe, questplan.CustomErnestKey, questplan.CustomErnestTube} {
		r := newErnestRig(t, questplan.ErnestSpokeOddenstein, map[int]int{questplan.ObjOilCan: 1, questplan.ObjPressureGauge: 1, questplan.ObjRubberTube: 1}, nil)
		// Take away what the leg under test would otherwise be skipped for.
		switch leg {
		case questplan.CustomErnestCan:
			r.take(questplan.ObjOilCan, 1)
		case questplan.CustomErnestProbe:
			r.take(questplan.ObjPressureGauge, 1)
		case questplan.CustomErnestKey, questplan.CustomErnestTube:
			r.take(questplan.ObjRubberTube, 1)
		}
		r.fail[leg] = true
		q := r.wrap(ErnestTheChicken(), r.stubs())
		res := r.engine().Run(q, 30*time.Second)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, leg) {
			t.Errorf("%s fails: the run ended %s (%s), want a park naming the leg: %v", leg, res.Outcome, res.Reason, r.log)
		}
		if n := strings.Count(strings.Join(r.log, "\n"), "stub "+leg+" -> false"); n != 1 {
			t.Errorf("%s was tried %d times, want once: %v", leg, n, r.log)
		}
	}
}

// ---- the registry ----

func TestEveryErnestCustomStepHasAnExecutorAndNoneIsExtra(t *testing.T) {
	q := ErnestTheChicken()
	var have []string
	for name, fn := range q.Customs {
		if fn == nil {
			t.Errorf("custom step %s has a nil executor", name)
		}
		have = append(have, name)
	}
	slices.Sort(have)
	want := append([]string(nil), questplan.ErnestCustomNames()...)
	slices.Sort(want)
	if !slices.Equal(have, want) {
		t.Errorf("executors %v, the plan's steps %v", have, want)
	}
	if q.Module.Record.ID != questplan.ErnestChickenID || q.Done != nil {
		t.Errorf("quest %+v", q.Module.Record)
	}
}

func TestTheManorsSealedPlacesArePocketsForEveryQuestsWalks(t *testing.T) {
	for name, at := range map[string][3]int{
		"the Draynor Manor closet":        {questplan.ErnestClosetInside.X, questplan.ErnestClosetInside.Z, 0},
		"the Draynor Manor ladder alcove": {questplan.ErnestAlcoveLever.X, questplan.ErnestAlcoveLever.Z, 0},
		"the Draynor Manor basement":      {questplan.ErnestBasementLanding.X, questplan.ErnestBasementLanding.Z, 0},
	} {
		got, ok := questrunner.PocketAt(tileOf(at))
		if !ok || got != name {
			t.Errorf("PocketAt(%v) = %q %v, want %q", at, got, ok, name)
		}
	}
	for _, at := range [][3]int{{3098, 3358, 0}, {3107, 3367, 0}, {3110, 3367, 2}, {3093, 3243, 0}} {
		if name, ok := questrunner.PocketAt(tileOf(at)); ok {
			t.Errorf("PocketAt(%v) = %q: not a pocket", at, name)
		}
	}
}
