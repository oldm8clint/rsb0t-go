package questmods

// Dwarf Cannon through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (obsslug_engine_test.go): the
// module's own Decide runs against snapshots built from a fake pack, the account's tile, levels and hitpoints and the quest tab's colour, and
// every step it asks for is performed by a stub that changes that world the way the server would. What is proven is the state machine under the
// engine's bookkeeping: the quest reaches Complete in the order the plan intends, a healthy run is never parked, an account the bats hunt
// is refused when the price is too high and a leg that cannot be done parks the quest with its reason. What is not proven here is each custom's own
// live execution (the railings' rolls, the ladders, the crate, the cannon): those are the real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// cannonRig is Dwarf Cannon's state on the rig: r.stage is the journal reading and the tab follows it.
type cannonRig struct {
	*csRig
	railings, towers, children, outs, repairs, reports int
	hand                                               []string
}

func newCannonRig(t *testing.T, stage int) *cannonRig { return newCannonRigWith(t, stage, 70, 60) }

// newCannonRigWith is the rig with the melee levels and hitpoints given: 70 and 60 is a strong fighter, whom the bats (combat 54 and under)
// do not hunt.
func newCannonRigWith(t *testing.T, stage, melee, hp int) *cannonRig {
	r := &cannonRig{csRig: newCSRig(t, questplan.CannonCommanderAnchor)}
	r.stageKnown = true
	r.set(stage)
	osSeed(r.csRig, osAttack, melee)
	osSeed(r.csRig, osStrength, melee)
	osSeed(r.csRig, osDefence, melee)
	osSeed(r.csRig, gamestate.HitpointsStat, hp)
	return r
}

func (r *cannonRig) set(stage int) {
	r.stage = stage
	status := quests.InProgress
	switch stage {
	case questplan.McNotStarted:
		status = quests.NotStarted
	case questplan.McComplete:
		status = quests.Complete
	}
	r.setTab(questplan.DwarfCannonName, status)
}

func (r *cannonRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		if step.KitName != questplan.KitCannonFood {
			r.t.Errorf("unexpected kit %q", step.KitName)
			return false
		}
		r.give(questplan.ObjShark, 4)
		return true
	case questplan.StepDeposit, questplan.StepScanBank, questplan.StepWithdraw:
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
		r.hand = append(r.hand, step.Stop.Npc)
		switch step.Stop.Npc {
		case "Dwarf Commander":
			switch r.stage {
			case questplan.McNotStarted:
				r.give(questplan.ObjCannonRailing, 6)
				r.set(questplan.McRailings)
			case questplan.McRailings:
				r.give(questplan.ObjCannonRailing, 1)
			case questplan.McRailingsDone:
				r.set(questplan.McTower)
			case questplan.McTowerRemains:
				r.set(questplan.McGoblinCave)
			case questplan.McChildRescued:
				r.give(questplan.ObjCannonToolkit, 1)
				r.set(questplan.McFixCannon)
			case questplan.McFixCannon:
				if r.held(questplan.ObjCannonToolkit) == 0 {
					r.give(questplan.ObjCannonToolkit, 1)
				} else {
					r.t.Errorf("a pointless talk with the Commander at the tool kit stage")
					return false
				}
			case questplan.McReturnNotes:
				if r.held(questplan.ObjCannonNotes) == 0 || r.held(questplan.ObjCannonMould) == 0 {
					r.t.Errorf("the Commander asked for the mould and the notes without them")
					return false
				}
				r.take(questplan.ObjCannonNotes, 1)
				r.take(questplan.ObjCannonMould, 1)
				r.set(questplan.McComplete)
			default:
				r.t.Errorf("a talk with the Commander at stage %d", r.stage)
				return false
			}
		case "Nulodion":
			switch r.stage {
			case questplan.McSeeNulodion:
				r.give(questplan.ObjCannonNotes, 1)
				r.give(questplan.ObjCannonMould, 1)
				r.set(questplan.McReturnNotes)
			case questplan.McReturnNotes:
				if r.held(questplan.ObjCannonNotes) == 0 {
					r.give(questplan.ObjCannonNotes, 1)
				}
				if r.held(questplan.ObjCannonMould) == 0 {
					r.give(questplan.ObjCannonMould, 1)
				}
			default:
				r.t.Errorf("a talk with Nulodion at stage %d", r.stage)
				return false
			}
		default:
			r.t.Errorf("a talk with %s", step.Stop.Npc)
			return false
		}
		return true
	}
	switch step.Custom {
	case questplan.CustomCannonRailings:
		r.railings++
		r.take(questplan.ObjCannonRailing, r.held(questplan.ObjCannonRailing))
		r.set(questplan.McRailingsDone)
	case questplan.CustomCannonTower:
		r.towers++
		r.set(questplan.McTowerRemains)
	case questplan.CustomCannonChild:
		r.children++
		r.sim.StandAt(questplan.CannonCrate.X, questplan.CannonCrate.Z)
		r.set(questplan.McChildRescued)
	case questplan.CustomCannonOut:
		r.outs++
		r.sim.StandAt(questplan.CannonCaveExit.X, questplan.CannonCaveExit.Z)
	case questplan.CustomCannonRepair:
		r.repairs++
		r.set(questplan.McCannonFixed)
	case questplan.CustomCannonReport:
		r.reports++
		r.set(questplan.McSeeNulodion)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole quest in the content's order, every executor a stub.
func TestDwarfCannonReachesCompleteThroughTheEngine(t *testing.T) {
	r := newCannonRig(t, questplan.McNotStarted)
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("provision cannon-food", "talk to Dwarf Commander", questplan.CustomCannonRailings, "talk to Dwarf Commander", questplan.CustomCannonTower,
		"talk to Dwarf Commander", questplan.CustomCannonChild, questplan.CustomCannonOut, "talk to Dwarf Commander", questplan.CustomCannonRepair,
		questplan.CustomCannonReport, "talk to Nulodion", "talk to Dwarf Commander")
	if r.railings != 1 || r.towers != 1 || r.children != 1 || r.outs != 1 || r.repairs != 1 || r.reports != 1 {
		t.Errorf("railings %d, towers %d, children %d, outs %d, repairs %d, reports %d", r.railings, r.towers, r.children, r.outs, r.repairs, r.reports)
	}
	if got := r.e.Store.Record(questplan.DwarfCannonID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	if r.held(questplan.ObjCannonNotes) != 0 || r.held(questplan.ObjCannonMould) != 0 {
		t.Error("the notes or the mould are still held after the hand-in")
	}
}

// An account that is in the cave when the child is out leaves by the mud pile before it talks to anybody.
func TestDwarfCannonLeavesTheCaveBeforeAnythingElse(t *testing.T) {
	r := newCannonRig(t, questplan.McChildRescued)
	r.sim.StandAt(questplan.CannonCrate.X, questplan.CannonCrate.Z)
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantHead(questplan.CustomCannonOut, "talk to Dwarf Commander", questplan.CustomCannonRepair)
}

// A lost tool kit is asked of the Commander again, and the repair is not started without one.
func TestDwarfCannonAsksForAnotherToolKit(t *testing.T) {
	r := newCannonRig(t, questplan.McFixCannon)
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantHead("talk to Dwarf Commander", questplan.CustomCannonRepair, questplan.CustomCannonReport)
}

// A lost mould or lost notes are asked of Nulodion again.
func TestDwarfCannonAsksNulodionForALostMould(t *testing.T) {
	r := newCannonRig(t, questplan.McReturnNotes)
	r.give(questplan.ObjCannonNotes, 1)
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Nulodion", "talk to Dwarf Commander")
}

// An account too weak for the railings and the bats is not started: the engine parks it for three hours with the reason.
func TestDwarfCannonParksAWeakAccountAtTheStart(t *testing.T) {
	r := newCannonRigWith(t, questplan.McNotStarted, 70, 15)
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "not doing Dwarf Cannon yet") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.DwarfCannonID, time.Now().Add(2*time.Hour+50*time.Minute)); !parked {
		t.Error("not parked for the three hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.DwarfCannonID, time.Now().Add(3*time.Hour+10*time.Minute)); parked {
		t.Error("parked past the three hours")
	}
	if r.held(questplan.ObjCannonRailing) != 0 {
		t.Error("the Commander was talked to")
	}
}

// An account the bats hunt (combat 54 and under) with no food from the bank or the mule is priced out; with food it goes.
func TestDwarfCannonPricesAHuntedAccountOnItsFood(t *testing.T) {
	// The round found nothing: the account carries no food, and the engine parks it.
	r := newCannonRigWith(t, questplan.McNotStarted, 40, 45)
	res := r.run(DwarfCannon(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision {
			return true // the bank and the mule had no food
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "giant bats") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.held(questplan.ObjCannonRailing) != 0 {
		t.Error("the Commander was talked to")
	}
	// With the sharks the round brings, the same account is started.
	r = newCannonRigWith(t, questplan.McNotStarted, 40, 45)
	res = r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A leg that fails is run once and parks the quest for half an hour with its reason.
func TestDwarfCannonParksAFailedLegForHalfAnHour(t *testing.T) {
	for _, c := range []struct {
		stage  int
		custom string
		reason string
	}{
		{questplan.McRailings, questplan.CustomCannonRailings, "railings were not all replaced"},
		{questplan.McTower, questplan.CustomCannonTower, "no dwarf remains"},
		{questplan.McFindChild, questplan.CustomCannonChild, "dwarf child"},
		{questplan.McFixCannon, questplan.CustomCannonRepair, "multicannon would not be repaired"},
		{questplan.McCannonFixed, questplan.CustomCannonReport, "did not run to its end"},
	} {
		r := newCannonRig(t, c.stage)
		r.give(questplan.ObjShark, 4)
		r.give(questplan.ObjCannonRailing, 6)
		r.give(questplan.ObjCannonToolkit, 1)
		runs := 0
		res := r.run(DwarfCannon(), func(step questplan.Step) bool {
			if step.Kind == questplan.StepCustom && step.Custom == c.custom {
				runs++
				return false
			}
			return r.apply(step)
		})
		if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, c.reason) {
			t.Errorf("%s: outcome %s after %d runs: %q (%v)", c.custom, res.Outcome, runs, res.Reason, r.log)
			continue
		}
		if parked, _ := r.e.Store.Parked(questplan.DwarfCannonID, time.Now().Add(29*time.Minute)); !parked {
			t.Errorf("%s: not parked for the half hour", c.custom)
		}
		if parked, _ := r.e.Store.Parked(questplan.DwarfCannonID, time.Now().Add(31*time.Minute)); parked {
			t.Errorf("%s: parked past the half hour", c.custom)
		}
	}
}

// A death in the cave parks the quest for a day (MaxDeaths 1), at once.
func TestADeathInTheCaveParksDwarfCannonForADay(t *testing.T) {
	r := newCannonRig(t, questplan.McFindChild)
	r.e.D.Chat = chatlog.New()
	r.give(questplan.ObjShark, 4)
	res := r.run(DwarfCannon(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomCannonChild {
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.DwarfCannonID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A full pack at the start is emptied at the bank first, keeping the quest's own items, so the six railings fit.
func TestDwarfCannonMakesRoomForTheRailings(t *testing.T) {
	r := newCannonRig(t, questplan.McNotStarted)
	r.give(questplan.ObjShark, 4)
	for id := 1000; id < 1021; id++ {
		r.give(id, 1)
	}
	res := r.run(DwarfCannon(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantHead("bank all but", "talk to Dwarf Commander", questplan.CustomCannonRailings)
	if r.held(1000) != 0 {
		t.Error("the junk is still in the pack")
	}
}

// wantHead asserts the first steps of the run.
func (r *cannonRig) wantHead(want ...string) {
	r.t.Helper()
	got := r.described()
	if len(got) < len(want) {
		r.t.Fatalf("steps %v: want them to begin %v", got, want)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			r.t.Fatalf("step %d is %q, want a step starting %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestDwarfCannonCustomsAreAllRegistered(t *testing.T) {
	q := DwarfCannon()
	for _, name := range []string{questplan.CustomCannonRailings, questplan.CustomCannonTower, questplan.CustomCannonChild, questplan.CustomCannonOut,
		questplan.CustomCannonRepair, questplan.CustomCannonReport} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if q.Module.Record.ID != questplan.DwarfCannonID {
		t.Errorf("module %q", q.Module.Record.ID)
	}
}
