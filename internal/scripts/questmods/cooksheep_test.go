package questmods

// Cook's Assistant and Sheep Shearer through the real engine (Engine.Run) on a simulated session: the module's own Decide
// runs against snapshots built from a fake pack and the quest tab's colour, and every step it asks for is performed by a
// stub that changes that world the way the server would (a talk starts the quest, a provisioning round fills the pack, the
// hand-in takes the items and turns the tab green). What is proven is the state machine: that each scenario reaches
// Complete in the order the plan intends, that the watchdog never parks a healthy run, and that a failing kit or a missing
// source parks the quest instead of looping. What is not proven here is each step's own live execution (walking, shearing,
// the mill): those are the customs, covered by the real-data tests and the live logs.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// csRig is a simulated account and the engine that runs a quest on it.
type csRig struct {
	t   *testing.T
	sim *actionsim.Session
	e   *questrunner.Engine
	log []string
	// steps are the real steps the module asked for, a repeat of the same description collapsed.
	steps []questplan.Step
	// stage is the journal stage injected into the snapshot (the engine's journal read needs a real interface).
	stage      int
	stageKnown bool
	// snapHook, when set, changes the snapshot the module's Decide is given (what the account wears, which the simulated session does not track).
	snapHook func(*questplan.Snapshot)
}

func newCSRig(t *testing.T, at tile.Tile) *csRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return id == questplan.CoinsID }}, nil)
	sim.StandAt(at.X, at.Z)
	r := &csRig{t: t, sim: sim}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { r.log = append(r.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	r.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	return r
}

// setTab colours a quest's tab entry as the server's IF_SETCOLOUR does.
func (r *csRig) setTab(name string, status quests.QuestStatus) {
	r.t.Helper()
	comID, ok := quests.ComponentID(name)
	if !ok {
		r.t.Fatalf("no quest tab entry for %q", name)
	}
	var packed uint16
	switch status {
	case quests.NotStarted:
		packed = 0x1f << 10
	case quests.InProgress:
		packed = 0x1f<<10 | 0x1f<<5
	case quests.Complete:
		packed = 0x1f << 5
	}
	w := packet.NewWriter(4)
	w.P2(uint16(comID))
	w.P2(packed)
	r.sim.State().Handle(serverprot.IF_SETCOLOUR, w.Data[:w.Pos])
}

func (r *csRig) held(id int) int {
	n := 0
	for _, s := range r.sim.Pack() {
		if int(s.ID) == id {
			n += s.Count
		}
	}
	return n
}

func (r *csRig) give(id, n int) {
	if got := r.sim.Give(int32(id), n); got != n {
		r.t.Fatalf("the pack took %d of %d of item %d", got, n, id)
	}
}

func (r *csRig) take(id, n int) {
	if got := r.sim.Remove(int32(id), n); got != n {
		r.t.Fatalf("the pack lost %d of %d of item %d", got, n, id)
	}
}

// run wraps q so that every step its Decide asks for goes to apply (the real step, which reports whether it worked) and
// runs it to the end. Provisioning rounds keep the engine's own bookkeeping (a custom step named "provision:<kit>" is
// recorded in Snapshot.Ran and Snapshot.Failed under that name, as the real step is), and a module that reads its stage
// from the journal gets the rig's stage injected instead.
func (r *csRig) run(q questrunner.Quest, apply func(step questplan.Step) bool) questrunner.Result {
	real := q.Module.Decide
	m := q.Module
	m.ReadStage = nil
	customs := map[string]questrunner.CustomFunc{}
	var pending questplan.Step
	m.Decide = func(s questplan.Snapshot) questplan.Step {
		if r.stageKnown {
			s.Stage, s.StageKnown = r.stage, true
		}
		if r.snapHook != nil {
			r.snapHook(&s)
		}
		st := real(s)
		if st.Kind == questplan.StepDone || st.Kind == questplan.StepWait {
			return st
		}
		if why := questplan.ValidateStep(st); why != "" {
			r.t.Fatalf("the module asked for a malformed step: %s (%s)", why, st.Describe())
		}
		name := "step:" + string(st.Kind)
		switch st.Kind {
		case questplan.StepCustom:
			name = st.Custom
		case questplan.StepProvision:
			name = "provision:" + st.KitName
		}
		pending = st
		customs[name] = func(*questrunner.Engine, questplan.Step) bool { return apply(pending) }
		if n := len(r.steps); n == 0 || r.steps[n-1].Describe() != st.Describe() {
			r.steps = append(r.steps, st)
		}
		return questplan.Step{Kind: questplan.StepCustom, Custom: name, Reason: st.Describe()}
	}
	q.Module = m
	q.Customs = customs
	return r.e.Run(q, time.Minute)
}

// described is the distinct steps in order, for an assertion on the whole run.
func (r *csRig) described() []string {
	var out []string
	for _, st := range r.steps {
		out = append(out, st.Describe())
	}
	return out
}

func (r *csRig) wantSteps(want ...string) {
	r.t.Helper()
	got := r.described()
	if len(got) != len(want) {
		r.t.Fatalf("steps\n  %s\nwant\n  %s\nlog %v", strings.Join(got, "\n  "), strings.Join(want, "\n  "), r.log)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			r.t.Fatalf("step %d is %q, want a step starting %q\nall:\n  %s", i, got[i], want[i], strings.Join(got, "\n  "))
		}
	}
}

func (r *csRig) stepNamed(prefix string) questplan.Step {
	r.t.Helper()
	for _, st := range r.steps {
		if strings.HasPrefix(st.Describe(), prefix) {
			return st
		}
	}
	r.t.Fatalf("no step starting %q in %v", prefix, r.described())
	return questplan.Step{}
}

var csAtCook = tile.Tile{X: 3210, Z: 3216}

// ---------------------------------------------------------------------------------------------------------------------
// Cook's Assistant
// ---------------------------------------------------------------------------------------------------------------------

// cookApply is the server's side of the quest: the Cook starts it and takes the three ingredients; the rest is up to the
// scenario's kit and gathering stubs, which are given as per-step answers.
func cookApply(r *csRig, give map[string]func() bool) func(step questplan.Step) bool {
	return func(step questplan.Step) bool {
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepPickLoc, questplan.StepGrabGround:
			key = string(step.Kind)
		case questplan.StepTalk:
			if r.held(questplan.ObjEgg) > 0 && r.held(questplan.ObjBucketOfMilk) > 0 && r.held(questplan.ObjPotOfFlour) > 0 {
				r.take(questplan.ObjEgg, 1)
				r.take(questplan.ObjBucketOfMilk, 1)
				r.take(questplan.ObjPotOfFlour, 1)
				r.setTab(questplan.CooksAssistantName, quests.Complete)
				return true
			}
			if st, _ := quests.Status(r.sim.State(), questplan.CooksAssistantName); st == quests.NotStarted {
				r.setTab(questplan.CooksAssistantName, quests.InProgress)
				return true
			}
			r.t.Errorf("a talk with the Cook that neither starts the quest nor hands the ingredients in")
			return false
		}
		f, ok := give[key]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", key)
			return false
		}
		return f()
	}
}

// The long way: nothing in the bank or the mule, the kit gives only the egg, and the bucket, the cow, the pot, the wheat and
// the mill make the rest. Every step is one the plan intends, in order, and the run ends complete without a park.
func TestCooksAssistantTheLongWayReachesComplete(t *testing.T) {
	r := newCSRig(t, csAtCook)
	r.setTab(questplan.CooksAssistantName, quests.NotStarted)
	res := r.run(CooksAssistant(), cookApply(r, map[string]func() bool{
		"provision:" + questplan.KitCook:       func() bool { r.give(questplan.ObjEgg, 1); return true },
		"provision:" + questplan.KitCookBucket: func() bool { r.give(questplan.ObjBucket, 1); return true },
		questplan.CustomCookMilk: func() bool {
			r.take(questplan.ObjBucket, 1)
			r.give(questplan.ObjBucketOfMilk, 1)
			return true
		},
		"provision:" + questplan.KitCookPot: func() bool { r.give(questplan.ObjEmptyPot, 1); return true },
		string(questplan.StepPickLoc):       func() bool { r.give(questplan.ObjGrain, 1); return true },
		questplan.CustomCookMill: func() bool {
			r.take(questplan.ObjEmptyPot, 1)
			r.take(questplan.ObjGrain, 1)
			r.give(questplan.ObjPotOfFlour, 1)
			return true
		},
	}))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Cook", "provision cook ", "provision cook-bucket", "cook-milk", "provision cook-pot", "Pick Wheat", "cook-mill", "talk to Cook")
	if got := r.e.Store.Record("cook").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// The pack is empty of ingredients and the bundled pass was asked with the item rule's keep list.
	for _, id := range []int{questplan.ObjEgg, questplan.ObjBucketOfMilk, questplan.ObjPotOfFlour} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the hand-in", id)
		}
	}
	kit := r.stepNamed("provision cook ")
	if len(kit.Kit) != 3 || kit.KeepIDs == nil || !kit.BankSet || kit.Bank != questplan.DraynorBank {
		t.Errorf("the bundled pass %+v", kit)
	}
	// At the Cook, the windmill is nearer than Wydin's shelf: the kit lists no shop for the flour.
	for _, l := range kit.Kit {
		if l.ID == questplan.ObjPotOfFlour && len(l.Sources) != 0 {
			t.Errorf("flour sources %+v for an account at the Cook", l.Sources)
		}
	}
}

// The bank (or the mule) held everything: one pass, the hand-in, nothing else.
func TestCooksAssistantFromAFullBankIsThreeSteps(t *testing.T) {
	r := newCSRig(t, csAtCook)
	r.setTab(questplan.CooksAssistantName, quests.NotStarted)
	res := r.run(CooksAssistant(), cookApply(r, map[string]func() bool{
		"provision:" + questplan.KitCook: func() bool {
			r.give(questplan.ObjEgg, 1)
			r.give(questplan.ObjBucketOfMilk, 1)
			r.give(questplan.ObjPotOfFlour, 1)
			return true
		},
	}))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Cook", "provision cook ", "talk to Cook")
}

// A bundled pass that failed on the way (a walk that timed out) is not asked again: the quest's own steps go on.
func TestCooksAssistantAFailedPassFallsThroughToTheOwnSteps(t *testing.T) {
	r := newCSRig(t, csAtCook)
	r.setTab(questplan.CooksAssistantName, quests.InProgress)
	passes := 0
	res := r.run(CooksAssistant(), cookApply(r, map[string]func() bool{
		"provision:" + questplan.KitCook: func() bool { passes++; return false },
		string(questplan.StepGrabGround): func() bool { r.give(questplan.ObjEgg, 1); return true },
		"provision:" + questplan.KitCookBucket: func() bool {
			r.give(questplan.ObjBucket, 1)
			return true
		},
		questplan.CustomCookMilk:            func() bool { r.take(questplan.ObjBucket, 1); r.give(questplan.ObjBucketOfMilk, 1); return true },
		"provision:" + questplan.KitCookPot: func() bool { r.give(questplan.ObjEmptyPot, 1); return true },
		string(questplan.StepPickLoc):       func() bool { r.give(questplan.ObjGrain, 1); return true },
		questplan.CustomCookMill: func() bool {
			r.take(questplan.ObjEmptyPot, 1)
			r.take(questplan.ObjGrain, 1)
			r.give(questplan.ObjPotOfFlour, 1)
			return true
		},
	}))
	if res.Outcome != questrunner.OutcomeComplete || passes != 1 {
		t.Fatalf("the run ended %s (%s) after %d passes: %v", res.Outcome, res.Reason, passes, r.log)
	}
	r.wantSteps("provision cook ", "grab Egg", "provision cook-bucket", "cook-milk", "provision cook-pot", "Pick Wheat", "cook-mill", "talk to Cook")
}

// A milk that fails again and again is the quest's own cow, and the engine parks it rather than milking for ever.
func TestCooksAssistantAMilkThatNeverComesParks(t *testing.T) {
	r := newCSRig(t, csAtCook)
	r.setTab(questplan.CooksAssistantName, quests.InProgress)
	r.give(questplan.ObjEgg, 1)
	r.give(questplan.ObjBucket, 1)
	res := r.run(CooksAssistant(), cookApply(r, map[string]func() bool{
		"provision:" + questplan.KitCook: func() bool { return true },
		questplan.CustomCookMilk:         func() bool { return false },
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s), want a park on no progress: %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("cook", time.Now()); !parked {
		t.Error("the park is not in the side store")
	}
}

// With no bucket anywhere (the floor, the store and the bank all failed) the quest waits and parks; it does not walk to a cow.
func TestCooksAssistantWithNoBucketWaitsAndParks(t *testing.T) {
	r := newCSRig(t, csAtCook)
	r.setTab(questplan.CooksAssistantName, quests.InProgress)
	r.give(questplan.ObjEgg, 1)
	res := r.run(CooksAssistant(), cookApply(r, map[string]func() bool{
		"provision:" + questplan.KitCook:       func() bool { return true },
		"provision:" + questplan.KitCookBucket: func() bool { return false },
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no bucket") {
		t.Fatalf("the run ended %s (%s), want a park on the missing bucket: %v", res.Outcome, res.Reason, r.log)
	}
	for _, d := range r.described() {
		if strings.HasPrefix(d, "cook-milk") {
			t.Errorf("the quest went to the cow without a bucket: %v", r.described())
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Sheep Shearer
// ---------------------------------------------------------------------------------------------------------------------

// sheepApply is the server's side: Fred starts the quest (stage 1) and takes balls one by one (the hand-in takes all it is
// given, the 20th completes). The scenario answers the provisioning and gathering steps.
func sheepApply(r *csRig, handed *int, give map[string]func(step questplan.Step) bool) func(step questplan.Step) bool {
	return func(step questplan.Step) bool {
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepTalk:
			if st, _ := quests.Status(r.sim.State(), questplan.SheepShearerName); st != quests.NotStarted {
				r.t.Errorf("a plain talk with Fred after the start: the hand-in is its own step")
				return false
			}
			r.setTab(questplan.SheepShearerName, quests.InProgress)
			r.stage, r.stageKnown = questplan.SheepStarted+*handed, true
			return true
		case questplan.StepCustom:
			if step.Custom == questplan.CustomSheepHandIn {
				for r.held(questplan.ObjBallOfWool) > 0 && *handed < questplan.SheepBalls {
					r.take(questplan.ObjBallOfWool, 1)
					*handed++
				}
				r.stage = questplan.SheepStarted + *handed
				if *handed >= questplan.SheepBalls {
					r.setTab(questplan.SheepShearerName, quests.Complete)
				}
				return true
			}
		}
		f, ok := give[key]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", key)
			return false
		}
		return f(step)
	}
}

var (
	csInArdougne = tile.Tile{X: 2640, Z: 3290}
	csAtFred     = tile.Tile{X: 3190, Z: 3275}
)

// An account in Ardougne: the first pass runs before the quest is even started, lists Aemad's shelf, buys the 20 balls where
// it stands, and Fred takes them: no shears, no pen, no wheel.
func TestSheepShearerFromArdougneBuysItsBallsFirst(t *testing.T) {
	r := newCSRig(t, csInArdougne)
	r.setTab(questplan.SheepShearerName, quests.NotStarted)
	handed := 0
	res := r.run(SheepShearer(), sheepApply(r, &handed, map[string]func(questplan.Step) bool{
		"provision:" + questplan.KitSheepBalls: func(questplan.Step) bool { r.give(questplan.ObjBallOfWool, 20); return true },
	}))
	if res.Outcome != questrunner.OutcomeComplete || handed != 20 {
		t.Fatalf("the run ended %s (%s), Fred took %d: %v", res.Outcome, res.Reason, handed, r.log)
	}
	r.wantSteps("provision sheep-balls", "talk to Fred the Farmer", "sheep-hand-in")
	kit := r.stepNamed("provision sheep-balls")
	if len(kit.Kit) != 1 || kit.Kit[0].Qty != 20 || len(kit.Kit[0].Sources) != 1 || kit.Kit[0].Sources[0].Shop != questplan.AemadStore {
		t.Errorf("the pass %+v, want 20 balls with Aemad's shelf as the source", kit.Kit)
	}
	if kit.Bank != questplan.NearestBank(csInArdougne).Tile {
		t.Errorf("the pass banks at %v, want the Ardougne bank the account stands by", kit.Bank)
	}
}

// An account at Fred's: the shelf is across the map, so the pass is the bank and the mule only; when they hold none the quest
// is started, then the shears, the pen and the wheel make the balls, and Fred takes them.
func TestSheepShearerAtFredShearsItsOwn(t *testing.T) {
	r := newCSRig(t, csAtFred)
	r.setTab(questplan.SheepShearerName, quests.NotStarted)
	handed := 0
	var wanted int
	res := r.run(SheepShearer(), sheepApply(r, &handed, map[string]func(questplan.Step) bool{
		"provision:" + questplan.KitSheepBalls:  func(questplan.Step) bool { return true },
		"provision:" + questplan.KitSheepShears: func(questplan.Step) bool { r.give(questplan.ObjShears, 1); return true },
		questplan.CustomSheepWool: func(st questplan.Step) bool {
			wanted = st.Qty
			r.give(questplan.ObjBallOfWool, st.Qty)
			return true
		},
	}))
	if res.Outcome != questrunner.OutcomeComplete || handed != 20 || wanted != 20 {
		t.Fatalf("the run ended %s (%s), Fred took %d, the shearing was asked for %d: %v", res.Outcome, res.Reason, handed, wanted, r.log)
	}
	r.wantSteps("provision sheep-balls", "talk to Fred the Farmer", "provision sheep-shears", "sheep-wool", "sheep-hand-in")
	if kit := r.stepNamed("provision sheep-balls"); len(kit.Kit[0].Sources) != 0 {
		t.Errorf("sources %+v: Aemad's shelf is 850 steps from Fred", kit.Kit[0].Sources)
	}
	if r.held(questplan.ObjShears) != 1 {
		t.Errorf("the shears are not in the pack at the end: they are the account's to keep")
	}
}

// Fred has taken 13 already (the journal says 7 owed), the mule gives 4, the shearing is asked for the other 3, and the
// hand-in of 7 finishes the quest: a run that ended part way picks up where it stopped.
func TestSheepShearerResumesAPartialDelivery(t *testing.T) {
	r := newCSRig(t, csAtFred)
	r.setTab(questplan.SheepShearerName, quests.InProgress)
	handed := 13
	r.stage, r.stageKnown = questplan.SheepStarted+handed, true
	r.give(questplan.ObjShears, 1)
	var wanted int
	res := r.run(SheepShearer(), sheepApply(r, &handed, map[string]func(questplan.Step) bool{
		"provision:" + questplan.KitSheepBalls: func(st questplan.Step) bool {
			if st.Kit[0].Qty != 7 {
				t.Errorf("the pass asked for %d balls, want the 7 Fred still owes", st.Kit[0].Qty)
			}
			r.give(questplan.ObjBallOfWool, 4)
			return true
		},
		questplan.CustomSheepWool: func(st questplan.Step) bool {
			wanted = st.Qty
			r.give(questplan.ObjBallOfWool, st.Qty)
			return true
		},
	}))
	if res.Outcome != questrunner.OutcomeComplete || handed != 20 || wanted != 3 {
		t.Fatalf("the run ended %s (%s), Fred has %d, the shearing was asked for %d: %v", res.Outcome, res.Reason, handed, wanted, r.log)
	}
	r.wantSteps("provision sheep-balls", "sheep-wool", "sheep-hand-in")
}

// The shears are nowhere: the quest waits and parks rather than walking to a pen it cannot shear.
func TestSheepShearerWithNoShearsWaitsAndParks(t *testing.T) {
	r := newCSRig(t, csAtFred)
	r.setTab(questplan.SheepShearerName, quests.InProgress)
	handed := 0
	r.stage, r.stageKnown = questplan.SheepStarted, true
	res := r.run(SheepShearer(), sheepApply(r, &handed, map[string]func(questplan.Step) bool{
		"provision:" + questplan.KitSheepBalls:  func(questplan.Step) bool { return true },
		"provision:" + questplan.KitSheepShears: func(questplan.Step) bool { return false },
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no shears") {
		t.Fatalf("the run ended %s (%s), want a park on the missing shears: %v", res.Outcome, res.Reason, r.log)
	}
	for _, d := range r.described() {
		if strings.HasPrefix(d, "sheep-wool") {
			t.Errorf("the quest went to the pen without shears: %v", r.described())
		}
	}
}

// The shearing that gains no ball parks the quest through the watchdog instead of shearing for the whole slot.
func TestSheepShearerAShearingThatGainsNothingParks(t *testing.T) {
	r := newCSRig(t, csAtFred)
	r.setTab(questplan.SheepShearerName, quests.InProgress)
	handed := 0
	r.stage, r.stageKnown = questplan.SheepStarted, true
	r.give(questplan.ObjShears, 1)
	res := r.run(SheepShearer(), sheepApply(r, &handed, map[string]func(questplan.Step) bool{
		"provision:" + questplan.KitSheepBalls: func(questplan.Step) bool { return true },
		questplan.CustomSheepWool:              func(questplan.Step) bool { return false },
	}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s), want a park on no progress: %v", res.Outcome, res.Reason, r.log)
	}
}

// Both quests are registered with the customs their plans name, and no others.
func TestCookSheepRegisterExactlyTheirCustoms(t *testing.T) {
	for _, c := range []struct {
		q    questrunner.Quest
		id   string
		want []string
	}{
		{CooksAssistant(), "cook", []string{questplan.CustomCookMilk, questplan.CustomCookMill}},
		{SheepShearer(), "sheep", []string{questplan.CustomSheepWool, questplan.CustomSheepHandIn}},
	} {
		if c.q.ID() != c.id || len(c.q.Customs) != len(c.want) || c.q.Done != nil || c.q.Finish != nil {
			t.Errorf("%s: id %q, customs %d, done %v, finish %v", c.id, c.q.ID(), len(c.q.Customs), c.q.Done != nil, c.q.Finish != nil)
		}
		for _, name := range c.want {
			if c.q.Customs[name] == nil {
				t.Errorf("%s has no executor for %q", c.id, name)
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Every state a decision can be asked in
// ---------------------------------------------------------------------------------------------------------------------

// csCheckEveryStep calls decide on every combination of the given dimensions and checks what any step may be: well formed,
// a registered custom, a provision with its kit lines valid and the quest's whole keep list, a done only on a complete
// tab, a wait only with a reason and never a long park.
func csCheckEveryStep(t *testing.T, name string, q questrunner.Quest, keep []int, each func(emit func(questplan.Snapshot))) {
	t.Helper()
	n := 0
	kinds := map[questplan.StepKind]int{}
	each(func(s questplan.Snapshot) {
		n++
		st := q.Module.Decide(s)
		kinds[st.Kind]++
		if why := questplan.ValidateStep(st); why != "" {
			t.Fatalf("%s: malformed step %s for %+v", name, why, s)
		}
		switch st.Kind {
		case questplan.StepDone:
			if s.Journal != questplan.StatusComplete {
				t.Fatalf("%s: done on a %s tab", name, s.Journal)
			}
		case questplan.StepCustom:
			if q.Customs[st.Custom] == nil {
				t.Fatalf("%s: custom step %q has no executor", name, st.Custom)
			}
		case questplan.StepProvision, questplan.StepDeposit:
			for _, id := range keep {
				if !slices.Contains(st.KeepIDs, id) {
					t.Fatalf("%s: %s step %q does not keep item %d", name, st.Kind, st.KitName, id)
				}
			}
			if !st.BankSet {
				t.Fatalf("%s: %s step names no bank", name, st.Kind)
			}
		case questplan.StepWait:
			if st.ParkFor != 0 {
				t.Fatalf("%s: a %s wait that parks for a day: %s", name, s.Journal, st.Reason)
			}
		case questplan.StepTalk:
			if st.UntilItemID != 0 {
				t.Fatalf("%s: a talk that drives until an item", name)
			}
		}
	})
	if n < 1000 {
		t.Fatalf("%s: only %d states checked", name, n)
	}
	t.Logf("%s: %d states, steps %v", name, n, kinds)
}

func csSnap(journal questplan.Status, at tile.Tile, known bool, free int) questplan.Snapshot {
	return questplan.Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: free,
		BankKnown: true, BankIDs: map[int]int{}, Tile: at, TileKnown: known, Failed: map[string]bool{}, Ran: map[string]bool{}}
}

func TestCooksAssistantEveryStateYieldsARunnableStep(t *testing.T) {
	items := []int{questplan.ObjEgg, questplan.ObjBucketOfMilk, questplan.ObjPotOfFlour, questplan.ObjBucket, questplan.ObjEmptyPot, questplan.ObjGrain}
	csCheckEveryStep(t, "cook", CooksAssistant(), questplan.CookKeepIDs(), func(emit func(questplan.Snapshot)) {
		for _, journal := range []questplan.Status{questplan.StatusComplete, questplan.StatusUnknown, questplan.StatusNotStarted, questplan.StatusInProgress} {
			for _, at := range []tile.Tile{csAtCook, {X: 3020, Z: 3215}, {X: 2640, Z: 3290}} {
				for _, free := range []int{0, 2, 20} {
					for mask := 0; mask < 1<<len(items); mask++ {
						for kit := 0; kit < 3; kit++ { // untried, ran, failed
							for fail := 0; fail < 4; fail++ { // the bucket kit and the pot kit failed or not
								s := csSnap(journal, at, true, free)
								for i, id := range items {
									if mask&(1<<i) != 0 {
										s.InvIDs[id] = 1
									}
								}
								switch kit {
								case 1:
									s.Ran["provision:"+questplan.KitCook] = true
								case 2:
									s.Failed["provision:"+questplan.KitCook] = true
								}
								s.Failed["provision:"+questplan.KitCookBucket] = fail&1 != 0
								s.Failed["provision:"+questplan.KitCookPot] = fail&2 != 0
								emit(s)
							}
						}
					}
				}
			}
		}
	})
}

func TestSheepShearerEveryStateYieldsARunnableStep(t *testing.T) {
	csCheckEveryStep(t, "sheep", SheepShearer(), questplan.SheepKeepIDs(), func(emit func(questplan.Snapshot)) {
		for _, journal := range []questplan.Status{questplan.StatusComplete, questplan.StatusUnknown, questplan.StatusNotStarted, questplan.StatusInProgress} {
			for _, at := range []tile.Tile{csAtFred, csInArdougne, {}} {
				for _, free := range []int{0, 5, 20, 28} {
					for _, balls := range []int{0, 1, 7, 19, 20, 21} {
						for _, wool := range []int{0, 3, 25} {
							for shears := 0; shears < 2; shears++ {
								for _, stage := range []int{-1, 1, 14, 20} { // -1: the journal is unread
									for kit := 0; kit < 3; kit++ {
										for fail := 0; fail < 2; fail++ {
											s := csSnap(journal, at, at != (tile.Tile{}), free)
											s.InvIDs[questplan.ObjBallOfWool] = balls
											s.InvIDs[questplan.ObjWool] = wool
											s.InvIDs[questplan.ObjShears] = shears
											if stage > 0 {
												s.Stage, s.StageKnown = stage, true
											}
											switch kit {
											case 1:
												s.Ran["provision:"+questplan.KitSheepBalls] = true
											case 2:
												s.Failed["provision:"+questplan.KitSheepBalls] = true
											}
											s.Failed["provision:"+questplan.KitSheepShears] = fail == 1
											emit(s)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	})
}
