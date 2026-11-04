package questmods

// The Elemental Workshop through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers'
// (obsslug_engine_test.go): the module's own Decide runs against snapshots built from a fake pack, the account's tile, its levels and the
// quest tab's colour, and every step it asks for is performed by a stub that changes that world the way the server would. What is proven
// is the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and the completion
// record): the quest reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that cannot be done parks
// the quest with its reason. What is not proven here is each custom's own live execution (the wall, the valves, the rock, the furnace):
// those are the real-data tests and the live logs.

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

// Skill indices of the protocol's stat table (gamestate.SkillNames).
const (
	ewCrafting = 12
	ewSmithing = 13
	ewMining   = 14
)

// ewRig is the Elemental Workshop's state on the rig: r.stage is the journal parser's stage (the book and the key in the low bits, the
// machinery in the high) and the tab follows it.
type ewRig struct {
	*csRig
	fights, ores, smelts      int
	needleGiven, leatherGiven bool
}

func newEwRig(t *testing.T, stage int) *ewRig {
	r := &ewRig{csRig: newCSRig(t, questplan.SeersBank)}
	r.stageKnown = true
	r.setStage(stage)
	for _, s := range []int{ewCrafting, ewSmithing, ewMining} {
		osSeed(r.csRig, s, 30)
	}
	osFighter(r.csRig, 45, 55)
	return r
}

func (r *ewRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch questplan.EwStageOf(n) {
	case questplan.EwNotStarted:
		status = quests.NotStarted
	case questplan.EwComplete:
		status = quests.Complete
	}
	r.setTab(questplan.ElementalWorkshopName, status)
}

func (r *ewRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

func (r *ewRig) here() tile.Tile {
	x, z, level, _ := r.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// kit gives the pack everything a full kit round would.
func (r *ewRig) kit() {
	for id, n := range map[int]int{questplan.ObjHammer: 1, questplan.ObjBronzePickaxe: 1, questplan.ObjEwCoal: 4, questplan.ObjEwThread: 1, questplan.ObjShark: 6} {
		if r.held(id) < n {
			r.give(id, n-r.held(id))
		}
	}
}

// apply plays the content's side of the quest: the book, the key, the machinery's bits, the crates' once-only gifts, the rock's elemental,
// the furnace and the bench.
func (r *ewRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		switch step.KitName {
		case questplan.KitEwKnife:
			r.give(questplan.ObjKnife, 1)
		case questplan.KitEwKit:
			r.kit()
		default:
			r.t.Errorf("unexpected kit %q", step.KitName)
			return false
		}
		return true
	case questplan.StepDeposit:
		return true
	}
	flags := r.stage &^ questplan.EwStageMask
	switch step.Custom {
	case questplan.CustomEwBook:
		r.give(questplan.ObjEwBook, 1)
	case questplan.CustomEwRead:
		r.setStage(questplan.EwRead)
	case questplan.CustomEwSlash:
		r.give(questplan.ObjEwKey, 1)
		r.setStage(flags | max(questplan.EwStageOf(r.stage), questplan.EwSlashed))
	case questplan.CustomEwEnter:
		r.at(questplan.EwArrive)
		r.setStage(flags | questplan.EwEntered)
	case questplan.CustomEwLeave:
		r.at(questplan.EwWallOutside)
	case questplan.CustomEwSupplies:
		switch step.ItemID {
		case questplan.ObjEwNeedle:
			if r.needleGiven || r.held(questplan.ObjEwNeedle) > 0 {
				return false
			}
			r.needleGiven = true
			r.give(questplan.ObjEwNeedle, 1)
		case questplan.ObjEwLeather:
			if r.leatherGiven || r.held(questplan.ObjEwLeather) > 0 {
				return false
			}
			r.leatherGiven = true
			r.give(questplan.ObjEwLeather, 1)
		default:
			r.give(questplan.ObjEwBowl, 1)
		}
	case questplan.CustomEwWater:
		r.setStage(r.stage | questplan.EwWater)
	case questplan.CustomEwBellows:
		r.take(questplan.ObjEwLeather, 1)
		r.take(questplan.ObjEwThread, 1)
		r.setStage(r.stage | questplan.EwBellows)
	case questplan.CustomEwLava:
		r.setStage(r.stage | questplan.EwFurnace)
	case questplan.CustomEwOre:
		r.fights++
		r.ores++
		r.give(questplan.ObjEwOre, 1)
	case questplan.CustomEwSmelt:
		r.smelts++
		r.take(questplan.ObjEwOre, 1)
		r.take(questplan.ObjEwCoal, questplan.EwCoalNeeded)
		r.give(questplan.ObjEwBar, 1)
		r.setStage(r.stage | questplan.EwBar)
	case questplan.CustomEwShield:
		r.take(questplan.ObjEwBar, 1)
		r.give(questplan.ObjEwShield, 1)
		r.setStage(questplan.EwComplete)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole quest in the content's order, every executor a stub.
func TestTheElementalWorkshopReachesCompleteThroughTheEngine(t *testing.T) {
	r := newEwRig(t, questplan.EwNotStarted)
	res := r.run(ElementalWorkshop(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps(questplan.CustomEwBook, questplan.CustomEwRead, "provision "+questplan.KitEwKnife, questplan.CustomEwSlash, "provision "+questplan.KitEwKit,
		questplan.CustomEwEnter, questplan.CustomEwWater, questplan.CustomEwSupplies, questplan.CustomEwSupplies, questplan.CustomEwBellows,
		questplan.CustomEwSupplies, questplan.CustomEwLava, questplan.CustomEwOre, questplan.CustomEwSmelt, questplan.CustomEwShield)
	if r.fights != 1 || r.smelts != 1 {
		t.Errorf("%d fights, %d smelts", r.fights, r.smelts)
	}
	if got := r.e.Store.Record(questplan.ElementalWorkshopID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// The supplies steps name their items: the needle, the leather, the bowl.
	var items []int
	for _, st := range r.steps {
		if st.Custom == questplan.CustomEwSupplies {
			items = append(items, st.ItemID)
		}
	}
	if !slices.Equal(items, []int{questplan.ObjEwNeedle, questplan.ObjEwLeather, questplan.ObjEwBowl}) {
		t.Errorf("the supplies asked for %v", items)
	}
	if r.held(questplan.ObjEwShield) != 1 || r.held(questplan.ObjEwBar) != 0 || r.held(questplan.ObjEwOre) != 0 {
		t.Errorf("shield %d, bar %d, ore %d", r.held(questplan.ObjEwShield), r.held(questplan.ObjEwBar), r.held(questplan.ObjEwOre))
	}
}

// A quest begun elsewhere resumes from where the journal and the pack say, and a healthy run is never parked.
func TestTheElementalWorkshopResumesThroughTheEngineFromEveryStage(t *testing.T) {
	flags := questplan.EwEntered | questplan.EwWater | questplan.EwBellows | questplan.EwFurnace
	for _, c := range []struct {
		name  string
		stage int
		at    tile.Tile
		give  map[int]int
		want  []string
	}{
		{"the book read", questplan.EwRead, questplan.SeersBank, map[int]int{questplan.ObjEwBook: 1}, []string{"provision " + questplan.KitEwKnife, questplan.CustomEwSlash,
			"provision " + questplan.KitEwKit, questplan.CustomEwEnter, questplan.CustomEwWater, questplan.CustomEwSupplies, questplan.CustomEwSupplies, questplan.CustomEwBellows,
			questplan.CustomEwSupplies, questplan.CustomEwLava, questplan.CustomEwOre, questplan.CustomEwSmelt, questplan.CustomEwShield}},
		{"the machinery done, at the rock", flags, questplan.EwArrive, map[int]int{questplan.ObjEwBook: 1, questplan.ObjEwKey: 1, questplan.ObjHammer: 1, questplan.ObjEwCoal: 4,
			questplan.ObjBronzePickaxe: 1, questplan.ObjShark: 6}, []string{questplan.CustomEwOre, questplan.CustomEwSmelt, questplan.CustomEwShield}},
		{"the bar made, in the village", flags | questplan.EwBar, questplan.SeersBank, map[int]int{questplan.ObjEwBook: 1, questplan.ObjEwKey: 1, questplan.ObjHammer: 1,
			questplan.ObjEwBar: 1}, []string{questplan.CustomEwEnter, questplan.CustomEwShield}},
	} {
		r := newEwRig(t, c.stage)
		r.at(c.at)
		for id, n := range c.give {
			r.give(id, n)
		}
		res := r.run(ElementalWorkshop(), r.apply)
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

// A weak account is not sent to the rock: the engine parks the quest for the fight's six hours with the model's reason, and the side
// store has it. A strong account goes down and fights once.
func TestTheElementalWorkshopParksAWeakAccountBeforeTheDescent(t *testing.T) {
	r := newEwRig(t, questplan.EwSlashed)
	osFighter(r.csRig, 10, 20)
	r.give(questplan.ObjEwKey, 1)
	r.give(questplan.ObjEwBook, 1)
	res := r.run(ElementalWorkshop(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "not going down to the rock's earth elemental") || r.fights != 0 {
		t.Fatalf("the run ended %s (%s) after %d fights: %v", res.Outcome, res.Reason, r.fights, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(7*time.Hour)); parked {
		t.Error("parked past the six hours")
	}
	// The strong one of the same quest state goes down and fights once.
	r = newEwRig(t, questplan.EwSlashed)
	r.give(questplan.ObjEwKey, 1)
	r.give(questplan.ObjEwBook, 1)
	r.needleGiven, r.leatherGiven = true, true
	r.give(questplan.ObjEwNeedle, 1)
	r.give(questplan.ObjEwLeather, 1)
	res = r.run(ElementalWorkshop(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.fights != 1 {
		t.Fatalf("the run ended %s (%s) after %d fights: %v", res.Outcome, res.Reason, r.fights, r.log)
	}
}

// Under 20 in any of the three skills the quest is parked at once with the reason said (the record keeps the ladder from starting it; this
// is the module's own net for an account that falls under, which levels never do, or whose levels were misread).
func TestTheElementalWorkshopParksAnAccountUnderItsSkills(t *testing.T) {
	r := newEwRig(t, questplan.EwSlashed)
	osSeed(r.csRig, ewSmithing, 19)
	r.give(questplan.ObjEwKey, 1)
	res := r.run(ElementalWorkshop(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Smithing 19/20") || len(r.steps) != 0 {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
}

// A death at the rock parks the quest for a day at once (StepDeathCaps), not after the escalating backoff.
func TestADeathAtTheRockParksTheElementalWorkshopForADay(t *testing.T) {
	r := newEwRig(t, questplan.EwEntered|questplan.EwWater|questplan.EwBellows|questplan.EwFurnace)
	r.e.D.Chat = chatlog.New()
	r.at(questplan.EwArrive)
	for id, n := range map[int]int{questplan.ObjEwBook: 1, questplan.ObjEwKey: 1, questplan.ObjHammer: 1, questplan.ObjEwCoal: 4, questplan.ObjBronzePickaxe: 1, questplan.ObjShark: 6} {
		r.give(id, n)
	}
	fights := 0
	res := r.run(ElementalWorkshop(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomEwOre {
			fights++
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || fights != 1 || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s) after %d fights: %v", res.Outcome, res.Reason, fights, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// An elemental still standing after the fight's budget asks for a park of an hour, and the leg is run once.
func TestAnElementalThatWillNotFallParksTheWorkshopForAnHour(t *testing.T) {
	r := newEwRig(t, questplan.EwEntered|questplan.EwWater|questplan.EwBellows|questplan.EwFurnace)
	r.at(questplan.EwArrive)
	for id, n := range map[int]int{questplan.ObjEwBook: 1, questplan.ObjEwKey: 1, questplan.ObjHammer: 1, questplan.ObjEwCoal: 4, questplan.ObjBronzePickaxe: 1, questplan.ObjShark: 6} {
		r.give(id, n)
	}
	runs := 0
	res := r.run(ElementalWorkshop(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomEwOre {
			runs++
			r.e.RequestPark("the earth elemental was still standing after the fight's budget", time.Hour)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "earth elemental") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(59*time.Minute)); !parked {
		t.Error("not parked for the hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(61*time.Minute)); parked {
		t.Error("parked past the hour")
	}
}

// A kit that cannot be filled does not loop: the engine's watchdog parks the quest.
func TestAWorkshopKitThatNeverFillsParks(t *testing.T) {
	r := newEwRig(t, questplan.EwSlashed)
	r.give(questplan.ObjEwKey, 1)
	r.give(questplan.ObjEwBook, 1)
	res := r.run(ElementalWorkshop(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitEwKit {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A crate that has given its once-only needle and leather to an account that lost them parks the quest for the day, with the reason said,
// instead of searching crates for ever.
func TestACrateThatHasNothingLeftParksTheWorkshopForADay(t *testing.T) {
	r := newEwRig(t, questplan.EwEntered|questplan.EwWater)
	r.at(questplan.EwArrive)
	r.needleGiven, r.leatherGiven = true, true
	for id, n := range map[int]int{questplan.ObjEwBook: 1, questplan.ObjEwKey: 1, questplan.ObjHammer: 1, questplan.ObjEwCoal: 4, questplan.ObjBronzePickaxe: 1, questplan.ObjShark: 6,
		questplan.ObjEwThread: 1} {
		r.give(id, n)
	}
	res := r.run(ElementalWorkshop(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no needle or leather left") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ElementalWorkshopID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
	if len(r.steps) != 1 || r.steps[0].Custom != questplan.CustomEwSupplies {
		t.Errorf("steps %v: the crates are searched once", r.described())
	}
}
