package questmods

// Hazeel Cult through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (obsslug_engine_test.go): the
// module's own Decide runs against snapshots built from a fake pack, the account's tile and the quest tab's colour, and every step it asks
// for is performed by a stub that changes that world the way the server would. What is proven is the state machine under the engine's
// bookkeeping (Ran and Failed, the watchdog, the park requests, the death cap and the completion record): the cult's road reaches Complete
// in the order the plan intends, a healthy run is never parked, and a leg that cannot be done parks the quest with its reason. What is not
// proven here is each custom's own live execution (the cave, the raft, the valves, the wall, the chest): those are the real-data tests and
// the live logs.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// hzRig is Hazeel Cult's state on the rig: r.stage is %hazeelcultquest (plus 100 on the Carnillean side) and the tab follows it.
type hzRig struct {
	*csRig
	chests, rituals int
}

func newHzRig(t *testing.T, stage int) *hzRig {
	r := &hzRig{csRig: newCSRig(t, questplan.HzCerilAnchor)}
	r.stageKnown = true
	r.setStage(stage)
	return r
}

func (r *hzRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.HzNotStarted:
		status = quests.NotStarted
	case questplan.HzComplete:
		status = quests.Complete
	}
	r.setTab(questplan.HazeelCultName, status)
}

func (r *hzRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

func (r *hzRig) here() tile.Tile {
	x, z, level, _ := r.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// apply plays the content's side of the quest: the talks, the poison, the crate's key, the chest's scroll, the ritual.
func (r *hzRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepDeposit:
		keep := map[int]bool{}
		for _, id := range step.KeepIDs {
			keep[id] = true
		}
		for _, s := range r.sim.Pack() {
			if !keep[int(s.ID)] {
				r.sim.Remove(s.ID, s.Count)
			}
		}
		return true
	case questplan.StepTalk:
		switch step.Stop.ID {
		case questplan.NpcHzCeril:
			r.setStage(questplan.HzStarted)
		case questplan.NpcHzClivet:
			if !questplan.InHzLanding(r.here()) {
				r.t.Errorf("a talk with Clivet away from the landing, at %v", r.here())
			}
			switch r.stage {
			case questplan.HzStarted, questplan.HzSpokenClivet:
				r.give(questplan.ObjPoison, 1)
				r.setStage(questplan.HzClivetDecision)
			case questplan.HzClivetDecision:
				if r.held(questplan.ObjPoison) == 0 {
					r.give(questplan.ObjPoison, 1)
				}
			}
		default:
			r.t.Errorf("a talk with npc %d", step.Stop.ID)
		}
		return true
	}
	switch step.Custom {
	case questplan.CustomHzDown:
		r.at(questplan.HzLanding)
	case questplan.CustomHzPour:
		r.take(questplan.ObjPoison, 1)
		r.setStage(questplan.HzPoured)
		r.at(questplan.HzRangeStand)
	case questplan.CustomHzAlomone:
		r.setStage(questplan.HzFinishedSide)
		r.at(questplan.HzHideoutStand)
	case questplan.CustomHzKey:
		if r.held(questplan.ObjHzKey) == 0 {
			r.give(questplan.ObjHzKey, 1)
		}
		r.at(questplan.HzCrateStand)
	case questplan.CustomHzChest:
		r.chests++
		if r.held(questplan.ObjHzScroll) == 0 {
			r.give(questplan.ObjHzScroll, 1)
			if r.stage == questplan.HzFinishedSide {
				r.setStage(questplan.HzScroll)
			}
		}
		r.at(questplan.HzWallSouth)
	case questplan.CustomHzRitual:
		r.rituals++
		r.take(questplan.ObjHzScroll, 1)
		r.setStage(questplan.HzComplete)
		r.at(questplan.HzHideoutStand)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole cult road, every executor a stub, in the content's order.
func TestHazeelCultReachesCompleteThroughTheEngine(t *testing.T) {
	r := newHzRig(t, questplan.HzNotStarted)
	res := r.run(HazeelCult(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Ceril Carnillean", questplan.CustomHzDown, "talk to Clivet", questplan.CustomHzPour, questplan.CustomHzAlomone, questplan.CustomHzKey,
		questplan.CustomHzChest, questplan.CustomHzRitual)
	if r.chests != 1 || r.rituals != 1 {
		t.Errorf("%d chests, %d rituals", r.chests, r.rituals)
	}
	if got := r.e.Store.Record(questplan.HazeelCultID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjPoison, questplan.ObjHzScroll} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the ritual", id)
		}
	}
}

// A quest begun elsewhere resumes from where the journal says: each stage's own first step, and a healthy run is never parked.
func TestHazeelCultResumesThroughTheEngineFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		give  []int
		want  []string
	}{
		{"started", questplan.HzStarted, nil, []string{questplan.CustomHzDown, "talk to Clivet", questplan.CustomHzPour, questplan.CustomHzAlomone, questplan.CustomHzKey,
			questplan.CustomHzChest, questplan.CustomHzRitual}},
		{"the vial in hand", questplan.HzClivetDecision, []int{questplan.ObjPoison}, []string{questplan.CustomHzPour, questplan.CustomHzAlomone, questplan.CustomHzKey,
			questplan.CustomHzChest, questplan.CustomHzRitual}},
		{"Alomone has spoken", questplan.HzFinishedSide, nil, []string{questplan.CustomHzKey, questplan.CustomHzChest, questplan.CustomHzRitual}},
		{"the scroll found", questplan.HzScroll, []int{questplan.ObjHzScroll}, []string{questplan.CustomHzRitual}},
	} {
		r := newHzRig(t, c.stage)
		for _, id := range c.give {
			r.give(id, 1)
		}
		res := r.run(HazeelCult(), r.apply)
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s): %v", c.name, res.Outcome, res.Reason, r.log)
			continue
		}
		got := r.described()
		if len(got) != len(c.want) {
			t.Errorf("%s: steps %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range c.want {
			if !strings.HasPrefix(got[i], c.want[i]) {
				t.Errorf("%s: step %d is %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// An account on the Carnillean side is parked for a day with the reason said and walked down no road at all.
func TestTheEngineParksAnAccountOnTheCarnilleanSideForADay(t *testing.T) {
	r := newHzRig(t, questplan.HzGoodSide+questplan.HzFinishedSide)
	res := r.run(HazeelCult(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Carnillean side") || len(r.steps) != 0 {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.HazeelCultID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
	if parked, _ := r.e.Store.Parked(questplan.HazeelCultID, time.Now().Add(25*time.Hour)); parked {
		t.Error("parked past the day")
	}
}

// A raft that will not take the account to the hideout asks for a park of three hours, and the leg is run once.
func TestARaftThatWillNotMoveParksHazeelCultForThreeHours(t *testing.T) {
	r := newHzRig(t, questplan.HzPoured)
	runs := 0
	res := r.run(HazeelCult(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHzAlomone {
			runs++
			r.e.RequestPark("the raft would not take the account to the hideout after the valves were set", 3*time.Hour)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "raft") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.HazeelCultID, time.Now().Add(179*time.Minute)); !parked {
		t.Error("not parked for the three hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.HazeelCultID, time.Now().Add(181*time.Minute)); parked {
		t.Error("parked past the three hours")
	}
}

// A leg that never works does not loop: the engine's watchdog parks the quest.
func TestAHazeelLegThatNeverWorksParks(t *testing.T) {
	r := newHzRig(t, questplan.HzFinishedSide)
	res := r.run(HazeelCult(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHzKey {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A death on any leg parks the quest for a day (the cap is one: nothing on the cult's road should kill an account).
func TestADeathParksHazeelCultForADay(t *testing.T) {
	r := newHzRig(t, questplan.HzClivetDecision)
	r.e.D.Chat = chatlog.New()
	r.give(questplan.ObjPoison, 1)
	res := r.run(HazeelCult(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHzPour {
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.HazeelCultID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A full pack banks before Clivet's vial and the crate's key are handed over, and the quest items stay.
func TestAFullPackIsBankedBeforeHazeelCultHandsItemsOver(t *testing.T) {
	r := newHzRig(t, questplan.HzClivetDecision)
	r.at(questplan.HzLanding)
	for i := 0; i < 27; i++ {
		r.give(10000+i, 1)
	}
	r.give(questplan.ObjPoison, 1)
	// The vial is held, so the pour goes first and frees a slot; a quest at stage 6 with a full pack banks before the crate.
	res := r.run(HazeelCult(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	deposits := 0
	for _, st := range r.steps {
		if st.Kind == questplan.StepDeposit {
			deposits++
			if !slices.Contains(st.KeepIDs, questplan.ObjHzKey) || !slices.Contains(st.KeepIDs, questplan.ObjPoison) {
				t.Errorf("the deposit keeps %v", st.KeepIDs)
			}
		}
	}
	if deposits == 0 {
		t.Errorf("a full pack was never banked: %v", r.described())
	}
}
