package questmods

// The Knight's Sword through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers'
// (workshop_engine_test.go): the module's own Decide runs against snapshots built from a fake pack, the account's tile and levels and the
// quest tab's colour, and every step it asks for is performed by a stub that changes that world the way the server would. What is proven is
// the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and the completion
// record): the quest reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that cannot be done parks
// the quest with its reason. What is not proven here is each custom's own live execution (the sink, the range, the cupboard, the cave):
// those are the real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// sqRig is the Knight's Sword's state on the rig: r.stage is %squire and the tab follows it.
type sqRig struct {
	*csRig
	bank, mule map[int]int
	// supply are the ids the ground and the shops can give.
	supply map[int]bool
	// cooks are the range's outcomes in order (true a pie); a run past them is a pie.
	cooks  []bool
	cooked int
	// refusals are the searches Sir Vyvin refuses before one lands; trips counts the visits to the dungeon.
	refusals, searches, trips int
}

func newSqRig(t *testing.T, stage int) *sqRig { return newSqRigWith(t, stage, 62) }

// newSqRigWith is newSqRig for an account of hp hitpoints (a stat read never lowers the maximum the rig has seen, so it is seeded once).
func newSqRigWith(t *testing.T, stage, hp int) *sqRig {
	r := &sqRig{csRig: newCSRig(t, questplan.SqSquireAnchor), bank: map[int]int{}, mule: map[int]int{}, supply: map[int]bool{}}
	r.stageKnown = true
	r.e.Prov.Bank.Counts = r.bank
	r.setStage(stage)
	for stat, lvl := range map[int]int{osAttack: 60, osStrength: 60, osDefence: 40, gamestate.HitpointsStat: hp, sqMining: 12, sqCooking: 14} {
		osSeed(r.csRig, stat, lvl)
	}
	r.give(questplan.CoinsID, 100)
	return r
}

// Skill indices of the protocol's stat table (gamestate.SkillNames).
const (
	sqCooking = 7
	sqMining  = 14
)

func (r *sqRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.SqNotStarted:
		status = quests.NotStarted
	case questplan.SqComplete:
		status = quests.Complete
	}
	r.setTab(questplan.KnightsSwordName, status)
}

func (r *sqRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

func (r *sqRig) here() tile.Tile {
	x, z, level, _ := r.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// stock fills the bank with everything the quest can ask of it.
func (r *sqRig) stock() {
	for id, n := range map[int]int{questplan.ObjSqPieDish: 2, questplan.ObjSqRedberries: 6, questplan.ObjSqPotOfFlour: 6, questplan.ObjSqBucket: 1,
		questplan.ObjSqIronBar: 4, questplan.ObjShark: 30, questplan.ObjBronzePickaxe: 1} {
		r.bank[id] = n
	}
}

// provision plays the real planner over the rig: the bank's lines are drawn, the mule's bundle arrives with its one ask, a ground or shop
// task's quantity arrives if the rig supplies it; a source the rig does not have is skipped for the round, and a kit nothing fills fails.
func (r *sqRig) provision(kit []questplan.KitLine) bool {
	exhausted := map[string]bool{}
	asked := false
	for pass := 0; pass < 24; pass++ {
		pack := map[int]int{}
		for _, row := range r.sim.Pack() {
			pack[int(row.ID)] += row.Count
		}
		plan := questplan.PlanProvision(kit, questplan.ProvisionState{Pack: pack, Bank: r.bank, BankKnown: true, Mule: r.mule, MuleKnown: true, MuleAsked: asked,
			FreeSlots: 28 - len(r.sim.Pack()), Levels: map[string]int{"mining": 12}, Exhausted: exhausted})
		switch plan.Phase {
		case questplan.PhaseSatisfied:
			return true
		case questplan.PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := min(c.Count, r.bank[c.ID])
				if c.ID == questplan.CoinsID {
					n = c.Count
				}
				if n > 0 {
					r.bank[c.ID] -= n
					r.give(c.ID, n)
					moved = true
				}
			}
			if !moved && len(plan.Withdraw) > 0 {
				return false
			}
		case questplan.PhaseMule:
			asked = true
			for _, c := range plan.MuleAsk {
				n := min(c.Count, r.mule[c.ID])
				r.mule[c.ID] -= n
				if n > 0 {
					r.give(c.ID, n)
				}
			}
		case questplan.PhaseSource:
			task := plan.Tasks[0]
			if !r.supply[task.Line.ID] {
				exhausted[questplan.SourceKey(task.Line, task.Source)] = true
				continue
			}
			r.give(task.Line.ID, task.Qty)
		default:
			return false
		}
	}
	return false
}

// apply plays the content's side of the quest: the keepers' talks, the pie's rungs, the cupboard, the dungeon.
func (r *sqRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepTalk:
		r.at(step.Stop.Anchor)
		switch step.Stop.ID {
		case questplan.NpcSquire:
			switch {
			case r.stage == questplan.SqNotStarted:
				r.setStage(questplan.SqStarted)
			case r.stage == questplan.SqSpokenThurgo:
				r.setStage(questplan.SqLookingPortrait)
			case r.stage == questplan.SqLookingBlurite && r.held(questplan.ObjSqSword) > 0:
				r.take(questplan.ObjSqSword, 1)
				r.setStage(questplan.SqComplete)
			default:
				r.t.Errorf("a talk with the Squire at stage %d", r.stage)
				return false
			}
		case questplan.NpcReldo:
			r.setStage(questplan.SqSpokenReldo)
		case questplan.NpcThurgo:
			switch {
			case r.stage == questplan.SqSpokenReldo && r.held(questplan.ObjSqRedberryPie) > 0:
				r.take(questplan.ObjSqRedberryPie, 1)
				r.setStage(questplan.SqGivenPie)
			case r.stage == questplan.SqGivenPie:
				r.setStage(questplan.SqSpokenThurgo)
			case r.stage == questplan.SqLookingPortrait && r.held(questplan.ObjSqPortrait) > 0:
				r.take(questplan.ObjSqPortrait, 1)
				r.setStage(questplan.SqLookingBlurite)
			case r.stage == questplan.SqLookingBlurite && r.held(questplan.ObjSqBluriteOre) > 0 && r.held(questplan.ObjSqIronBar) >= 2:
				r.take(questplan.ObjSqBluriteOre, 1)
				r.take(questplan.ObjSqIronBar, 2)
				r.give(questplan.ObjSqSword, 1)
			default:
				r.t.Errorf("a talk with Thurgo at stage %d", r.stage)
				return false
			}
		}
		return true
	case questplan.StepProvision:
		return r.provision(step.Kit)
	case questplan.StepDeposit, questplan.StepScanBank:
		return true
	case questplan.StepWithdraw:
		for _, it := range step.Items {
			if r.bank[it.ID] < it.Qty {
				r.t.Errorf("withdrawing %d of %d from a bank holding %d", it.Qty, it.ID, r.bank[it.ID])
				return false
			}
			r.bank[it.ID] -= it.Qty
			r.give(it.ID, it.Qty)
		}
		return true
	case questplan.StepWalk:
		r.at(step.Anchor)
		return true
	}
	switch step.Custom {
	case questplan.CustomSquireFill:
		r.take(questplan.ObjSqBucket, 1)
		r.give(questplan.ObjSqBucketWater, 1)
		w := questplan.SqNearestWater(r.here())
		r.at(w.Loc)
	case questplan.CustomSquireDough:
		r.take(questplan.ObjSqPotOfFlour, 1)
		r.take(questplan.ObjSqBucketWater, 1)
		r.give(questplan.ObjSqEmptyPot, 1)
		r.give(questplan.ObjSqBucket, 1)
		r.give(questplan.ObjSqPastryDough, 1)
	case questplan.CustomSquireShell:
		r.take(step.ItemID, 1)
		r.take(step.TargetID, 1)
		r.give(step.ExpectItemID, 1)
	case questplan.CustomSquireBerries:
		r.take(step.ItemID, 1)
		r.take(step.TargetID, 1)
		r.give(step.ExpectItemID, 1)
	case questplan.CustomSquireCook:
		r.take(questplan.ObjSqUncookedPie, 1)
		pie := r.cooked >= len(r.cooks) || r.cooks[r.cooked]
		r.cooked++
		if pie {
			r.give(questplan.ObjSqRedberryPie, 1)
		} else {
			r.give(questplan.ObjSqBurntPie, 1)
		}
	case questplan.CustomSquireEmpty:
		r.take(questplan.ObjSqBurntPie, 1)
		r.give(questplan.ObjSqPieDish, 1)
	case questplan.CustomSquirePortrait:
		r.searches++
		if r.searches <= r.refusals {
			return false
		}
		r.give(questplan.ObjSqPortrait, 1)
		r.at(questplan.SqVyvinApproaches[0])
	case questplan.CustomSquireBlurite:
		r.trips++
		r.give(questplan.ObjSqBluriteOre, 1)
		r.at(questplan.SqLadderTop)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole quest in the content's order, every executor a stub.
func TestTheKnightsSwordReachesCompleteThroughTheEngine(t *testing.T) {
	r := newSqRig(t, questplan.SqNotStarted)
	r.stock()
	res := r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	// Thurgo's pie talk and his sword talk (stage 2 to 3 and 3 to 4) are the same description twice in a row, which the rig collapses.
	r.wantSteps("talk to Squire", "talk to Reldo", "provision squire-pie", questplan.CustomSquireFill, questplan.CustomSquireDough, questplan.CustomSquireShell,
		questplan.CustomSquireBerries, questplan.CustomSquireCook, "talk to Thurgo", "talk to Squire", questplan.CustomSquirePortrait, "talk to Thurgo",
		"provision squire-ore", questplan.CustomSquireBlurite, "talk to Thurgo", "talk to Squire")
	if got := r.e.Store.Record(questplan.KnightsSwordID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjSqSword, questplan.ObjSqPortrait, questplan.ObjSqBluriteOre, questplan.ObjSqRedberryPie} {
		if r.held(id) != 0 {
			t.Errorf("item %d is still held after the hand-in", id)
		}
	}
	if r.trips != 1 || r.cooked != 1 {
		t.Errorf("%d visits to the dungeon and %d pies cooked", r.trips, r.cooked)
	}
	// The pie's kit was one provisioning round, and the dungeon's another.
	if k := r.stepNamed("provision squire-pie"); len(k.Kit) < 4 {
		t.Errorf("the pie's kit is %+v", k.Kit)
	}
}

// A quest begun elsewhere resumes from where the journal and the pack say, and a healthy run is never parked.
func TestTheKnightsSwordResumesThroughTheEngineFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		at    tile.Tile
		give  map[int]int
		first string
	}{
		{"started", questplan.SqStarted, questplan.SqSquireAnchor, nil, "talk to Reldo"},
		{"spoken to Reldo with the pie", questplan.SqSpokenReldo, questplan.SqReldoAnchor, map[int]int{questplan.ObjSqRedberryPie: 1}, "talk to Thurgo"},
		{"the dough made", questplan.SqSpokenReldo, questplan.WydinShop.Anchor, map[int]int{questplan.ObjSqPastryDough: 1, questplan.ObjSqPieDish: 1, questplan.ObjSqRedberries: 1}, questplan.CustomSquireShell},
		{"the pie burnt", questplan.SqSpokenReldo, questplan.SqRangeSpots[0].Loc, map[int]int{questplan.ObjSqBurntPie: 1}, questplan.CustomSquireEmpty},
		{"given the pie", questplan.SqGivenPie, questplan.SqThurgoAnchor, nil, "talk to Thurgo"},
		{"the picture asked for", questplan.SqSpokenThurgo, questplan.SqThurgoAnchor, nil, "talk to Squire"},
		{"looking for the portrait", questplan.SqLookingPortrait, questplan.SqSquireAnchor, nil, questplan.CustomSquirePortrait},
		{"the portrait in hand", questplan.SqLookingPortrait, questplan.SqVyvinApproaches[0], map[int]int{questplan.ObjSqPortrait: 1}, "talk to Thurgo"},
		{"the materials wanted", questplan.SqLookingBlurite, questplan.SqThurgoAnchor, nil, "provision squire-ore"},
		{"the ore in hand", questplan.SqLookingBlurite, questplan.SqLadderTop, map[int]int{questplan.ObjSqBluriteOre: 1, questplan.ObjSqIronBar: 2}, "talk to Thurgo"},
		{"the sword in hand", questplan.SqLookingBlurite, questplan.SqThurgoAnchor, map[int]int{questplan.ObjSqSword: 1}, "talk to Squire"},
	} {
		r := newSqRig(t, c.stage)
		r.stock()
		r.supply[questplan.ObjSqPotOfFlour], r.supply[questplan.ObjSqRedberries], r.supply[questplan.ObjSqBucket] = true, true, true
		r.at(c.at)
		for id, n := range c.give {
			r.give(id, n)
		}
		res := r.run(KnightsSword(), r.apply)
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s): %v", c.name, res.Outcome, res.Reason, r.log)
			continue
		}
		if got := r.described(); len(got) == 0 || !strings.HasPrefix(got[0], c.first) {
			t.Errorf("%s: the first step is %v, want %q", c.name, got, c.first)
		}
	}
}

// The range burns the first two pies: the engine's bookkeeping carries the dish back and the chain is made again, and the quest completes.
func TestABurntPieDoesNotParkTheKnightsSword(t *testing.T) {
	r := newSqRig(t, questplan.SqSpokenReldo)
	r.stock()
	r.cooks = []bool{false, false, true}
	res := r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.cooked != 3 {
		t.Fatalf("the run ended %s (%s) after %d bakes: %v", res.Outcome, res.Reason, r.cooked, r.log)
	}
	empties := 0
	for _, st := range r.steps {
		if st.Custom == questplan.CustomSquireEmpty {
			empties++
		}
	}
	if empties == 0 {
		t.Errorf("no burnt dish was emptied: %v", r.described())
	}
}

// Under the quest's levels the engine parks it at once with the reason said, before any step; the park is six hours.
func TestTheKnightsSwordParksAnAccountUnderItsLevels(t *testing.T) {
	for _, c := range []struct {
		name string
		stat int
		lvl  int
		say  string
	}{{"Mining 9", sqMining, 9, "Mining 9/10"}, {"Cooking 9", sqCooking, 9, "Cooking 9/10"}, {"Hitpoints 55", gamestate.HitpointsStat, 55, "Hitpoints 55/60"}} {
		hp := 62
		if c.stat == gamestate.HitpointsStat {
			hp = c.lvl
		}
		r := newSqRigWith(t, questplan.SqNotStarted, hp)
		r.stock()
		if c.stat != gamestate.HitpointsStat {
			osSeed(r.csRig, c.stat, c.lvl)
		}
		res := r.run(KnightsSword(), r.apply)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, c.say) || len(r.steps) != 0 {
			t.Errorf("%s: the run ended %s (%s) after %v", c.name, res.Outcome, res.Reason, r.described())
			continue
		}
		if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(5*time.Hour)); !parked {
			t.Errorf("%s: not parked for the six hours", c.name)
		}
		if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(7*time.Hour)); parked {
			t.Errorf("%s: parked past the six hours", c.name)
		}
	}
}

// No bars, no food and an empty visit each park the quest with a reason, and none walks an unprepared account into the dungeon.
func TestTheKnightsSwordParksBeforeTheDungeonUnlessPrepared(t *testing.T) {
	// No iron bars from anywhere.
	r := newSqRig(t, questplan.SqLookingBlurite)
	r.at(questplan.SqThurgoAnchor)
	r.bank[questplan.ObjShark], r.bank[questplan.ObjBronzePickaxe] = 30, 1
	res := r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "iron bar") || r.trips != 0 {
		t.Fatalf("no bars: the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, r.trips, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(2*time.Hour)); !parked {
		t.Error("no bars: not parked for the three hours")
	}
	// No food anywhere.
	r = newSqRig(t, questplan.SqLookingBlurite)
	r.at(questplan.SqThurgoAnchor)
	r.bank[questplan.ObjSqIronBar], r.bank[questplan.ObjBronzePickaxe] = 4, 1
	res = r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "hitpoints of food") || r.trips != 0 {
		t.Fatalf("no food: the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, r.trips, r.log)
	}
	// The mule holds the bars and the sharks: the account is supplied, goes down once and finishes.
	r = newSqRig(t, questplan.SqLookingBlurite)
	r.at(questplan.SqThurgoAnchor)
	r.bank[questplan.ObjBronzePickaxe] = 1
	r.mule[questplan.ObjSqIronBar], r.mule[questplan.ObjShark] = 14000, 80000
	res = r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.trips != 1 {
		t.Fatalf("a mule-supplied account: the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, r.trips, r.log)
	}
	if r.mule[questplan.ObjSqIronBar] != 13998 || r.held(questplan.ObjShark) < 6 {
		t.Errorf("the mule gave %d bars and the pack holds %d sharks", 14000-r.mule[questplan.ObjSqIronBar], r.held(questplan.ObjShark))
	}
}

// Sir Vyvin refusing the search is the executor's failure, not a loop: the quest is parked for the retry park with the reason, and the
// next run takes the portrait.
func TestSirVyvinsRefusalParksTheKnightsSwordThroughTheEngine(t *testing.T) {
	r := newSqRig(t, questplan.SqLookingPortrait)
	r.stock()
	r.refusals = 1
	res := r.run(KnightsSword(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Sir Vyvin") || r.searches != 1 {
		t.Fatalf("the run ended %s (%s) after %d searches: %v", res.Outcome, res.Reason, r.searches, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(31*time.Minute)); parked {
		t.Error("parked past the half hour")
	}
}

// A death in the dungeon parks the quest for three days at once (StepDeathCaps), not after the escalating backoff and not for a day.
func TestADeathInTheIceDungeonParksTheKnightsSwordForThreeDays(t *testing.T) {
	r := newSqRig(t, questplan.SqLookingBlurite)
	r.stock()
	r.e.D.Chat = chatlog.New()
	r.at(questplan.SqThurgoAnchor)
	visits := 0
	res := r.run(KnightsSword(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomSquireBlurite {
			visits++
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || visits != 1 || !strings.Contains(res.Reason, "cap is 1") {
		t.Fatalf("the run ended %s (%s) after %d visits: %v", res.Outcome, res.Reason, visits, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(71*time.Hour)); !parked {
		t.Error("not parked for the three days")
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(73*time.Hour)); parked {
		t.Error("parked past the three days")
	}
}

// A visit that comes out empty asks for the hour's park; the leg is run once.
func TestAnEmptyVisitParksTheKnightsSwordForAnHour(t *testing.T) {
	r := newSqRig(t, questplan.SqLookingBlurite)
	r.stock()
	r.at(questplan.SqThurgoAnchor)
	visits := 0
	res := r.run(KnightsSword(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomSquireBlurite {
			visits++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || visits != 1 || !strings.Contains(res.Reason, "no ore") {
		t.Fatalf("outcome %s after %d visits: %q (%v)", res.Outcome, visits, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(59*time.Minute)); !parked {
		t.Error("not parked for the hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.KnightsSwordID, time.Now().Add(61*time.Minute)); parked {
		t.Error("parked past the hour")
	}
}

// A kit that cannot be filled does not loop: the engine's watchdog parks the quest.
func TestAPieKitThatNeverFillsParksTheKnightsSword(t *testing.T) {
	r := newSqRig(t, questplan.SqSpokenReldo)
	res := r.run(KnightsSword(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}
