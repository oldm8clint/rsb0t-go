package questmods

// Sheep Herder through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (obsslug_engine_test.go):
// the module's own Decide runs against snapshots built from a fake pack, the account's worn panel and the quest tab's colour, and every step it
// asks for is performed by a stub that changes that world the way the server would. What is proven is the state machine under the engine's
// bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and the completion record): the quest reaches Complete in the
// order the plan intends, a healthy run is never parked, and a leg that cannot be done parks the quest with its reason. What is not proven here is
// each custom's own live execution (the gate, the pushes, the feed, the incinerator): the herding loop has its own simulation against the real
// pack (sheepherder_sim_test.go) and the rest is the real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// herdRig is Sheep Herder's state on the rig: the sheep's states give the stage, and the tab follows it.
type herdRig struct {
	*csRig
	states   [4]int
	suit     bool // the suit has been bought
	herded   []int
	burnt    []int
	fetches  int
	provided map[string]func()
}

func newHerdRig(t *testing.T) *herdRig {
	r := &herdRig{csRig: newCSRig(t, questplan.HalgriveAnchor), provided: map[string]func(){}}
	r.stageKnown = true
	r.set(questplan.HerdNotStarted)
	return r
}

// set puts the quest at a stage (and the sheep's states, when it is a disposing one).
func (r *herdRig) set(stage int) {
	r.stage = stage
	status := quests.InProgress
	switch {
	case stage == questplan.HerdNotStarted:
		status = quests.NotStarted
	case stage == questplan.HerdComplete:
		status = quests.Complete
	case stage >= questplan.HerdDisposing:
		r.suit = true
		for n := 1; n <= 4; n++ {
			r.states[n-1] = questplan.HerdSheepState(stage, n)
		}
	}
	r.setTab(questplan.SheepHerderName, status)
}

// restage recomputes the disposing stage from the sheep's states (all four burnt is the page that says to return).
func (r *herdRig) restage() { r.set(questplan.HerdStageWith(r.states)) }

// gear dresses the account as the plan wants it before the sheep: the suit and the prod worn, a feed in the pack.
func (r *herdRig) gear() {
	r.sim.Wear(4, questplan.ObjPlagueJacket)
	r.sim.Wear(7, questplan.ObjPlagueTrousers)
	r.sim.Wear(3, questplan.ObjCattleProd)
	r.give(questplan.ObjSheepFeed, 1)
}

var herdWornSlot = map[int]int{questplan.ObjPlagueJacket: 4, questplan.ObjPlagueTrousers: 7, questplan.ObjCattleProd: 3}

func (r *herdRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		if f, ok := r.provided[step.KitName]; ok {
			f()
			return true
		}
		switch step.KitName {
		case questplan.KitHerdCoins:
			r.give(questplan.CoinsID, 100)
		case questplan.KitHerdGear:
		default:
			r.t.Errorf("unexpected kit %q", step.KitName)
			return false
		}
		return true
	case questplan.StepDeposit, questplan.StepScanBank, questplan.StepWithdraw:
		// A deposit empties the pack of everything the plan does not keep.
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
	case questplan.StepEquip:
		slot, ok := herdWornSlot[step.ItemID]
		if !ok || r.held(step.ItemID) == 0 {
			r.t.Errorf("equip %s with %d held", step.Describe(), r.held(step.ItemID))
			return false
		}
		r.take(step.ItemID, 1)
		r.sim.Wear(slot, int32(step.ItemID))
		return true
	case questplan.StepTalk:
		switch step.Stop.Npc {
		case "Councillor Halgrive":
			switch {
			case r.stage == questplan.HerdNotStarted:
				r.give(questplan.ObjSheepFeed, 1)
				r.set(questplan.HerdNeedSuit)
			case r.stage == questplan.HerdAllBurnt:
				r.give(questplan.CoinsID, 3100)
				r.set(questplan.HerdComplete)
			case r.held(questplan.ObjSheepFeed) == 0:
				r.give(questplan.ObjSheepFeed, 1)
			default:
				r.t.Errorf("a pointless talk with Halgrive at stage %d", r.stage)
				return false
			}
		case "Doctor Orbon":
			if r.held(questplan.CoinsID) < 100 {
				r.t.Errorf("Orbon talked to without the coins")
				return false
			}
			r.take(questplan.CoinsID, 100)
			r.give(questplan.ObjPlagueJacket, 1)
			r.give(questplan.ObjPlagueTrousers, 1)
			if r.stage == questplan.HerdNeedSuit {
				r.set(questplan.HerdDisposing)
			}
		default:
			r.t.Errorf("a talk with %s", step.Stop.Npc)
			return false
		}
		return true
	}
	switch step.Custom {
	case questplan.CustomHerdProd:
		r.fetches++
		r.give(questplan.ObjCattleProd, 1)
	case questplan.CustomHerdSheep:
		n := step.Qty
		r.herded = append(r.herded, n)
		r.states[n-1] = questplan.HerdSheepKilled
		r.give(questplan.ObjSheepBones(n), 1)
		r.restage()
	case questplan.CustomHerdBurn:
		n := step.Qty
		r.burnt = append(r.burnt, n)
		r.take(questplan.ObjSheepBones(n), 1)
		r.states[n-1] = questplan.HerdSheepBurnt
		r.restage()
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole quest in the content's order, every executor a stub: Halgrive, the coins, Orbon's suit, the gear round, the suit and the prod put
// on, the prod fetched, then each sheep herded and its remains burnt, and the reward.
func TestSheepHerderReachesCompleteThroughTheEngine(t *testing.T) {
	r := newHerdRig(t)
	res := r.run(SheepHerder(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Councillor Halgrive", "provision herd-coins", "talk to Doctor Orbon", "provision herd-gear", "equip Plague jacket", "equip Plague trousers",
		questplan.CustomHerdProd, "equip Prod", questplan.CustomHerdSheep, questplan.CustomHerdBurn, questplan.CustomHerdSheep, questplan.CustomHerdBurn,
		questplan.CustomHerdSheep, questplan.CustomHerdBurn, questplan.CustomHerdSheep, questplan.CustomHerdBurn, "talk to Councillor Halgrive")
	if len(r.herded) != 4 || len(r.burnt) != 4 || r.fetches != 1 {
		t.Errorf("herded %v, burnt %v, %d prod fetches", r.herded, r.burnt, r.fetches)
	}
	for i := 0; i < 4; i++ {
		if r.herded[i] != i+1 || r.burnt[i] != i+1 {
			t.Errorf("the sheep were done out of order: herded %v, burnt %v", r.herded, r.burnt)
		}
	}
	if got := r.e.Store.Record(questplan.SheepHerderID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	if r.held(questplan.CoinsID) != 3100 {
		t.Errorf("%d coins after the reward", r.held(questplan.CoinsID))
	}
}

// A quest begun elsewhere is picked up where it stands: two sheep burnt, the gear on, and the rest follows without a second purchase.
func TestSheepHerderResumesAtTheThirdSheep(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdStageWith([4]int{3, 3, 0, 0}))
	r.gear()
	res := r.run(SheepHerder(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps(questplan.CustomHerdSheep, questplan.CustomHerdBurn, questplan.CustomHerdSheep, questplan.CustomHerdBurn, "talk to Councillor Halgrive")
	if len(r.herded) != 2 || r.herded[0] != 3 || r.herded[1] != 4 || r.fetches != 0 {
		t.Errorf("herded %v with %d prod fetches", r.herded, r.fetches)
	}
}

// A sheep killed whose remains are still in the pack is burnt before anything else is touched.
func TestSheepHerderBurnsRemainsHeldFirst(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdStageWith([4]int{3, 2, 0, 0}))
	r.gear()
	r.give(questplan.ObjSheepBones(2), 1)
	res := r.run(SheepHerder(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps(questplan.CustomHerdBurn, questplan.CustomHerdSheep, questplan.CustomHerdBurn, questplan.CustomHerdSheep, questplan.CustomHerdBurn, "talk to Councillor Halgrive")
	if r.burnt[0] != 2 {
		t.Errorf("burnt %v: the held remains of sheep 2 come first", r.burnt)
	}
}

// A pack with no room for what the suit and the prod replace is emptied at the bank first, keeping the quest's own items.
func TestSheepHerderMakesRoomBeforeDressing(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdDisposing)
	r.give(questplan.ObjPlagueJacket, 1)
	r.give(questplan.ObjPlagueTrousers, 1)
	r.give(questplan.ObjSheepFeed, 1)
	r.give(questplan.ObjCattleProd, 1)
	for id := 1000; id < 1021; id++ {
		r.give(id, 1)
	}
	res := r.run(SheepHerder(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantHead("bank all but", "equip Plague jacket", "equip Plague trousers", "equip Prod", questplan.CustomHerdSheep)
	if r.held(1000) != 0 || r.held(questplan.ObjSheepFeed) != 1 {
		t.Errorf("the bank visit emptied the wrong things: junk %d, feed %d", r.held(1000), r.held(questplan.ObjSheepFeed))
	}
}

// A herd that fails is not looped on: the engine parks the quest for half an hour with the reason, and the leg was run once.
func TestAFailedHerdParksSheepHerderForHalfAnHour(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdDisposing)
	r.gear()
	runs := 0
	res := r.run(SheepHerder(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHerdSheep {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "sheep 1 would not be herded") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.SheepHerderID, time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.SheepHerderID, time.Now().Add(31*time.Minute)); parked {
		t.Error("parked past the half hour")
	}
}

// A prod that is not on the barn floor parks the quest the same way, once.
func TestAMissingProdParksSheepHerder(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdDisposing)
	r.sim.Wear(4, questplan.ObjPlagueJacket)
	r.sim.Wear(7, questplan.ObjPlagueTrousers)
	r.give(questplan.ObjSheepFeed, 1)
	runs := 0
	res := r.run(SheepHerder(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHerdProd {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "no prod") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
}

// A death anywhere on the walks parks the quest for a day (MaxDeaths 1), at once.
func TestADeathParksSheepHerderForADay(t *testing.T) {
	r := newHerdRig(t)
	r.e.D.Chat = chatlog.New()
	r.set(questplan.HerdDisposing)
	r.gear()
	res := r.run(SheepHerder(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomHerdSheep {
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.SheepHerderID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A kit that never fills does not loop: the engine's watchdog parks the quest.
func TestASheepHerderKitThatNeverFillsParks(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdNeedSuit)
	res := r.run(SheepHerder(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitHerdCoins {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A lost half of the suit is bought again, and the account is never sent into the enclosure half dressed.
func TestALostHalfOfTheSuitIsBoughtAgain(t *testing.T) {
	r := newHerdRig(t)
	r.set(questplan.HerdStageWith([4]int{3, 0, 0, 0}))
	r.sim.Wear(4, questplan.ObjPlagueJacket)
	r.sim.Wear(3, questplan.ObjCattleProd)
	r.give(questplan.ObjSheepFeed, 1)
	r.give(questplan.CoinsID, 100)
	res := r.run(SheepHerder(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	// The bank's gear is drawn once first (a half-suit may be banked), then Orbon, then the trousers on.
	r.wantHead("provision herd-gear", "talk to Doctor Orbon", "equip Plague trousers", questplan.CustomHerdSheep)
}

// The pen is where the ladder leaves an account that parks inside it: a walk out of it goes through the pocket's exit, which the engine's
// Travel finds on its own (the registration is the pack test's).
func TestSheepHerderModuleCustomsAreAllRegistered(t *testing.T) {
	q := SheepHerder()
	for _, name := range []string{questplan.CustomHerdProd, questplan.CustomHerdSheep, questplan.CustomHerdBurn} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if q.Module.Record.ID != questplan.SheepHerderID {
		t.Errorf("module %q", q.Module.Record.ID)
	}
}

// wantHead asserts the first steps of the run.
func (r *herdRig) wantHead(want ...string) {
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
