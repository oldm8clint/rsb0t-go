package questmods

// Tribal Totem and the Fishing Contest through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers'
// (monkcog_engine_test.go): the module's own Decide runs against snapshots built from a fake pack and the quest tab's colour, and every
// step it asks for is performed by a stub that changes that world the way the server would. What is proven is the state machine under
// the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and the completion record): each scenario
// reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that cannot be done parks the quest with its
// reason. What is not proven here is each custom's own live execution (the lock's dials, the stairs, the gate, the casts): those are the
// real-data tests and the live logs.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// tfSeed sets one skill of the rig's account to a level, as the server's UPDATE_STAT does.
func tfSeed(r *csRig, skill string, level int) {
	r.t.Helper()
	for stat, name := range gamestate.SkillNames {
		if strings.EqualFold(name, skill) {
			p := packet.NewWriter(6)
			p.P1(byte(stat))
			p.P4(int32(levels.XPAtLevel(level)))
			p.P1(byte(level))
			r.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
			return
		}
	}
	r.t.Fatalf("no skill %q", skill)
}

// tfWant fails the test unless the steps the module asked for start with the wanted prefixes, in order and no more.
func tfWant(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: steps %v, want %v", name, got, want)
		return
	}
	for i := range got {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("%s: step %d is %q, want %q (all %v)", name, i, got[i], want[i], got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Tribal Totem
// ---------------------------------------------------------------------------------------------------------------------

// ttRig is Tribal Totem as the content runs it on the rig: r.stage is %totemquest (41 for stage 4 with the lock solved), the tab follows.
type ttRig struct {
	*csRig
	steals int
}

func newTTRig(t *testing.T) *ttRig {
	r := &ttRig{csRig: newCSRig(t, questplan.ArdougneSouthBank)}
	r.stageKnown = true
	tfSeed(r.csRig, "thieving", 40)
	r.set(questplan.TotemNotStarted)
	return r
}

func (r *ttRig) set(stage int) {
	r.stage = stage
	status := quests.InProgress
	switch stage {
	case questplan.TotemNotStarted:
		status = quests.NotStarted
	case questplan.TotemComplete:
		status = quests.Complete
	}
	r.setTab(questplan.TribalTotemName, status)
}

// apply performs the talks and the provisioning as the content does and hands the customs to the scenario's stubs.
func (r *ttRig) apply(answers map[string]func() bool) func(step questplan.Step) bool {
	return func(step questplan.Step) bool {
		switch step.Kind {
		case questplan.StepTalk:
			switch step.Stop.ID {
			case questplan.NpcKangaiMau:
				switch r.stage {
				case questplan.TotemNotStarted:
					r.set(questplan.TotemStarted)
				case questplan.TotemTeleported, questplan.TotemUnlocked:
					if r.held(questplan.ObjTribalTotem) > 0 {
						r.take(questplan.ObjTribalTotem, 1)
						r.set(questplan.TotemComplete)
					}
				}
			case questplan.NpcRPDTEmployee:
				if r.stage == questplan.TotemCrateMarked {
					r.set(questplan.TotemCrateDelivered)
				}
			}
			return true
		case questplan.StepProvision:
			if step.KitName == questplan.KitTotemCoins {
				r.give(questplan.CoinsID, questplan.TotemCoinsQty)
			}
			return true
		case questplan.StepScanBank, questplan.StepDeposit:
			return true
		}
		f, ok := answers[step.Custom]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", step.Describe())
			return false
		}
		return f()
	}
}

// happy are the customs of a quest that goes as the content says.
func (r *ttRig) happy() map[string]func() bool {
	return map[string]func() bool{
		questplan.CustomTotemLabel: func() bool { r.give(questplan.ObjTotemLabel, 1); return true },
		questplan.CustomTotemMark: func() bool {
			r.take(questplan.ObjTotemLabel, 1)
			r.set(questplan.TotemCrateMarked)
			return true
		},
		questplan.CustomTotemTeleport: func() bool {
			r.sim.StandAt(questplan.MansionArrival.X, questplan.MansionArrival.Z)
			if r.stage == questplan.TotemCrateDelivered {
				r.set(questplan.TotemTeleported)
			}
			return true
		},
		questplan.CustomTotemLock: func() bool { r.set(questplan.TotemUnlocked); return true },
		questplan.CustomTotemSteal: func() bool {
			r.steals++
			r.give(questplan.ObjTribalTotem, 1)
			return true
		},
	}
}

// The whole quest in the content's order, every executor a stub: the coins once, Kangai Mau, the label peeled and used, the employee,
// the Wizard's teleport, the lock, the stairs and the chest, and the totem to Kangai Mau.
func TestTribalTotemReachesCompleteThroughTheEngine(t *testing.T) {
	r := newTTRig(t)
	res := r.run(TribalTotem(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("provision totem-coins", "talk to Kangai Mau", "totem-label", "totem-mark", "talk to RPDT employee", "totem-teleport", "totem-lock",
		"totem-steal", "talk to Kangai Mau")
	if got := r.e.Store.Record("totem").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjTotemLabel, questplan.ObjTribalTotem} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the hand-in", id)
		}
	}
	if r.steals != 1 {
		t.Errorf("the chest was robbed %d times", r.steals)
	}
}

// A resumed run finds the account anywhere in the quest: each stage picks up where the content says it is.
func TestTribalTotemResumesFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		at    [2]int
		have  int
		want  []string
	}{
		{"started", questplan.TotemStarted, [2]int{2791, 3182}, 0, []string{"totem-label", "totem-mark", "talk to RPDT employee", "totem-teleport", "totem-lock", "totem-steal", "talk to Kangai Mau"}},
		{"label in hand", questplan.TotemStarted, [2]int{2650, 3272}, questplan.ObjTotemLabel, []string{"totem-mark", "talk to RPDT employee", "totem-teleport", "totem-lock", "totem-steal", "talk to Kangai Mau"}},
		{"delivered", questplan.TotemCrateDelivered, [2]int{2650, 3272}, 0, []string{"totem-teleport", "totem-lock", "totem-steal", "talk to Kangai Mau"}},
		{"in the mansion, locked", questplan.TotemTeleported, [2]int{2638, 3321}, 0, []string{"totem-lock", "totem-steal", "talk to Kangai Mau"}},
		{"in the mansion, unlocked", questplan.TotemUnlocked, [2]int{2635, 3323}, 0, []string{"totem-steal", "talk to Kangai Mau"}},
		{"outside the mansion, unlocked", questplan.TotemUnlocked, [2]int{2655, 3286}, 0, []string{"totem-teleport", "totem-steal", "talk to Kangai Mau"}},
		{"the totem in hand", questplan.TotemUnlocked, [2]int{2631, 3321}, questplan.ObjTribalTotem, []string{"talk to Kangai Mau"}},
	} {
		r := newTTRig(t)
		r.sim.StandAt(c.at[0], c.at[1])
		r.give(questplan.CoinsID, 500)
		r.set(c.stage)
		if c.have != 0 {
			r.give(c.have, 1)
		}
		res := r.run(TribalTotem(), r.apply(r.happy()))
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s): %v", c.name, res.Outcome, res.Reason, r.log)
			continue
		}
		tfWant(t, c.name, r.described(), c.want)
	}
}

// An account under Thieving 21 is parked for hours before it walks anywhere; its level is the content's gate at the stairs.
func TestTribalTotemUnderThieving21ParksBeforeItWalks(t *testing.T) {
	r := newTTRig(t)
	tfSeed(r.csRig, "thieving", 20)
	r.give(questplan.CoinsID, 500)
	res := r.run(TribalTotem(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Thieving 21") || len(r.steps) != 0 {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked("totem", time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
}

// A teleport that does not land, a label that does not come and a lock that does not take each park the quest with a reason and a
// timeout, and are run once.
func TestTribalTotemFailedLegsPark(t *testing.T) {
	for _, c := range []struct {
		name   string
		stage  int
		at     [2]int
		custom string
		want   string
	}{
		{"the label", questplan.TotemStarted, [2]int{2650, 3272}, questplan.CustomTotemLabel, "address label"},
		{"the teleport", questplan.TotemCrateDelivered, [2]int{2650, 3272}, questplan.CustomTotemTeleport, "teleport"},
		{"the lock", questplan.TotemTeleported, [2]int{2638, 3321}, questplan.CustomTotemLock, "combination"},
		{"the chest", questplan.TotemUnlocked, [2]int{2635, 3323}, questplan.CustomTotemSteal, "chest gave no totem"},
	} {
		r := newTTRig(t)
		r.sim.StandAt(c.at[0], c.at[1])
		r.give(questplan.CoinsID, 500)
		r.set(c.stage)
		runs := 0
		ans := r.happy()
		ans[c.custom] = func() bool { runs++; return false }
		res := r.run(TribalTotem(), r.apply(ans))
		if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, c.want) {
			t.Errorf("%s: outcome %s after %d run(s): %q (%v)", c.name, res.Outcome, runs, res.Reason, r.log)
			continue
		}
		if parked, _ := r.e.Store.Parked("totem", time.Now().Add(29*time.Minute)); !parked {
			t.Errorf("%s: not parked for the half hour", c.name)
		}
		if parked, _ := r.e.Store.Parked("totem", time.Now().Add(31*time.Minute)); parked {
			t.Errorf("%s: parked past the half hour", c.name)
		}
	}
}

// A custom that knows the way will not mend (the stairs' trap needs a level the account lacks) asks for a long park and is run once.
func TestTribalTotemARequestedParkRunsTheLegOnce(t *testing.T) {
	r := newTTRig(t)
	r.sim.StandAt(2635, 3323)
	r.set(questplan.TotemUnlocked)
	runs := 0
	ans := r.happy()
	ans[questplan.CustomTotemSteal] = func() bool {
		runs++
		r.e.RequestPark("the stairs' trap dropped the account into the Ardougne sewers", time.Hour)
		return false
	}
	res := r.run(TribalTotem(), r.apply(ans))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "sewers") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("totem", time.Now().Add(59*time.Minute)); !parked {
		t.Error("not parked for the hour")
	}
	if parked, _ := r.e.Store.Parked("totem", time.Now().Add(61*time.Minute)); parked {
		t.Error("parked past the hour")
	}
}

// One death anywhere in the quest parks it for a day (MaxDeaths 1).
func TestTribalTotemOneDeathParksItForADay(t *testing.T) {
	r := newTTRig(t)
	r.e.D.Chat = chatlog.New()
	r.sim.StandAt(2635, 3323)
	r.give(questplan.CoinsID, 500)
	r.set(questplan.TotemUnlocked)
	ans := r.happy()
	ans[questplan.CustomTotemSteal] = func() bool {
		r.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	}
	res := r.run(TribalTotem(), r.apply(ans))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("totem", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Fishing Contest
// ---------------------------------------------------------------------------------------------------------------------

// fcRig is the Fishing Contest as the content runs it on the rig: r.stage is %fishingcompo, the tab follows.
type fcRig struct {
	*csRig
	pipe         bool
	strangerAway int
	digs, pays   int
	// noGarlic makes the supplies pass find no garlic in the bank or the mule.
	noGarlic bool
}

func newFCRig(t *testing.T) *fcRig {
	r := &fcRig{csRig: newCSRig(t, questplan.FcSeersBank)}
	r.stageKnown = true
	tfSeed(r.csRig, "fishing", 40)
	r.set(questplan.FcNotStarted)
	return r
}

func (r *fcRig) set(stage int) {
	r.stage = stage
	status := quests.InProgress
	switch stage {
	case questplan.FcNotStarted:
		status = quests.NotStarted
	case questplan.FcComplete:
		status = quests.Complete
	}
	r.setTab(questplan.FishingContestName, status)
}

// pack puts the contest's kit in the pack.
func (r *fcRig) pack(ids ...int) {
	for _, id := range ids {
		r.give(id, 1)
	}
}

func (r *fcRig) apply(answers map[string]func() bool) func(step questplan.Step) bool {
	return func(step questplan.Step) bool {
		switch step.Kind {
		case questplan.StepTalk:
			switch {
			case r.stage == questplan.FcNotStarted:
				r.set(questplan.FcStarted)
				r.give(questplan.ObjFishingPass, 1)
			case step.UntilItemID == questplan.ObjFishingPass:
				r.give(questplan.ObjFishingPass, 1)
			case r.stage == questplan.FcWonComp && r.held(questplan.ObjFishingTrophy) > 0:
				r.take(questplan.ObjFishingTrophy, 1)
				r.set(questplan.FcComplete)
			}
			return true
		case questplan.StepProvision:
			switch step.KitName {
			case questplan.KitFishSupplies:
				if r.held(questplan.ObjFishingRod) == 0 {
					r.give(questplan.ObjFishingRod, 1)
				}
				if r.held(questplan.ObjGarlic) == 0 && !r.noGarlic {
					r.give(questplan.ObjGarlic, 1)
				}
				if r.held(questplan.CoinsID) < questplan.FcCoinsFloor {
					r.give(questplan.CoinsID, questplan.FcCoinsQty)
				}
			case questplan.KitFishSpade:
				r.give(questplan.ObjSpade, 1)
			case questplan.KitFishFood:
			}
			return true
		case questplan.StepScanBank, questplan.StepDeposit, questplan.StepWithdraw:
			return true
		}
		f, ok := answers[step.Custom]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", step.Describe())
			return false
		}
		return f()
	}
}

func (r *fcRig) happy() map[string]func() bool {
	return map[string]func() bool{
		questplan.CustomFishGarlic: func() bool { r.give(questplan.ObjGarlic, 1); return true },
		questplan.CustomFishDig: func() bool {
			r.digs++
			r.give(questplan.ObjRedVineWorm, 3)
			return true
		},
		questplan.CustomFishStash: func() bool {
			r.take(questplan.ObjGarlic, 1)
			r.pipe = true
			if r.stage == questplan.FcInComp {
				r.set(questplan.FcGarlicComp)
			}
			return true
		},
		questplan.CustomFishPay: func() bool {
			r.pays++
			switch {
			case r.pipe && r.strangerAway > 0:
				r.strangerAway--
			case r.pipe:
				r.set(questplan.FcGarlicComp)
			default:
				r.set(questplan.FcInComp)
			}
			return true
		},
		questplan.CustomFishLeave: func() bool {
			if r.stage == questplan.FcInComp || r.stage == questplan.FcGarlicComp {
				r.set(questplan.FcStarted)
			}
			return true
		},
		questplan.CustomFishFish: func() bool {
			r.take(questplan.ObjRedVineWorm, 3)
			r.give(questplan.ObjFishingTrophy, 1)
			r.set(questplan.FcWonComp)
			return true
		},
		questplan.CustomFishSpare: func() bool { r.give(questplan.ObjFishingTrophy, 1); return true },
	}
}

// The whole quest in the content's order, every executor a stub: the coins once, the dwarf, the spade, the worms, the garlic in the
// pipe before the fee, the fee and the seat, the casts, and the trophy back to the dwarf.
func TestTheFishingContestReachesCompleteThroughTheEngine(t *testing.T) {
	r := newFCRig(t)
	res := r.run(FishingContest(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("provision fishing-supplies", "talk to Mountain Dwarf", "provision fishing-spade", "fishing-dig", "fishing-stash", "fishing-pay", "fishing-fish",
		"talk to Mountain Dwarf")
	if got := r.e.Store.Record("fishingcompo").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjFishingTrophy, questplan.ObjRedVineWorm, questplan.ObjGarlic} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the dwarf", id)
		}
	}
}

// A seat that is not given leaves the fee paid: Bonzo is talked to again, and no second clove is fetched or stashed.
func TestTheFishingContestASeatThatFailsIsRetried(t *testing.T) {
	r := newFCRig(t)
	r.strangerAway = 2
	res := r.run(FishingContest(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.pays != 3 || r.digs != 1 {
		t.Errorf("%d seatings and %d trips into the wood, want 3 and 1: %v", r.pays, r.digs, r.described())
	}
	if n := strings.Count(strings.Join(r.described(), "\n"), "fishing-stash"); n != 1 {
		t.Errorf("the garlic was stashed %d times: %v", n, r.described())
	}
}

// A resumed run finds the account anywhere in the contest.
func TestTheFishingContestResumesFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		setup func(r *fcRig)
		want  []string
	}{
		{"seated at the willow with garlic", questplan.FcInComp, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod, questplan.ObjGarlic)
			r.give(questplan.ObjRedVineWorm, 3)
			r.give(questplan.CoinsID, 300)
		}, []string{"fishing-stash", "fishing-fish", "talk to Mountain Dwarf"}},
		// The pipe was stashed by an earlier run, which this run does not know: it fetches a clove and stashes it again, harmlessly.
		{"seated at the willow without garlic", questplan.FcInComp, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod)
			r.give(questplan.ObjRedVineWorm, 3)
			r.give(questplan.CoinsID, 300)
			r.pipe = true
		}, []string{"fishing-leave", "provision fishing-supplies", "fishing-stash", "fishing-pay", "fishing-fish", "talk to Mountain Dwarf"}},
		{"seated at the pipes", questplan.FcGarlicComp, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod)
			r.give(questplan.ObjRedVineWorm, 3)
		}, []string{"fishing-fish", "talk to Mountain Dwarf"}},
		{"seated at the pipes without worms", questplan.FcGarlicComp, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod)
			r.give(questplan.CoinsID, 300)
			r.pipe = true
		}, []string{"fishing-leave", "provision fishing-supplies", "provision fishing-spade", "fishing-dig", "fishing-stash", "fishing-pay", "fishing-fish",
			"talk to Mountain Dwarf"}},
		{"won, the trophy in hand", questplan.FcWonComp, func(r *fcRig) { r.pack(questplan.ObjFishingPass, questplan.ObjFishingTrophy) },
			[]string{"talk to Mountain Dwarf"}},
		{"won, the trophy lost", questplan.FcWonComp, func(r *fcRig) { r.pack(questplan.ObjFishingPass) },
			[]string{"fishing-spare", "talk to Mountain Dwarf"}},
	} {
		r := newFCRig(t)
		c.setup(r)
		r.set(c.stage)
		res := r.run(FishingContest(), r.apply(r.happy()))
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s): %v", c.name, res.Outcome, res.Reason, r.log)
			continue
		}
		tfWant(t, c.name, r.described(), c.want)
	}
}

// Under Fishing 10 the account is parked for hours before it walks anywhere.
func TestTheFishingContestUnderFishing10ParksBeforeItWalks(t *testing.T) {
	r := newFCRig(t)
	tfSeed(r.csRig, "fishing", 9)
	r.give(questplan.CoinsID, 500)
	res := r.run(FishingContest(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Fishing 10") || len(r.steps) != 0 {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked("fishingcompo", time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
}

// The wood is priced: an account the dogs can kill goes in with food or not at all. Here a 30-hitpoint account at Defence 1 with no food
// asks for food once, is parked for hours with the reason, and never enters the wood; with five sharks it goes in.
func TestTheFishingContestThePricedWoodIsNeverEnteredWithoutFood(t *testing.T) {
	setup := func(sharks int) *fcRig {
		r := newFCRig(t)
		tfSeed(r.csRig, "hitpoints", 30)
		tfSeed(r.csRig, "defence", 1)
		r.give(questplan.CoinsID, 500)
		r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod, questplan.ObjGarlic, questplan.ObjSpade)
		if sharks > 0 {
			r.give(385, sharks)
		}
		r.set(questplan.FcStarted)
		return r
	}
	r := setup(0)
	res := r.run(FishingContest(), r.apply(r.happy()))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "McGrubor") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	for _, s := range r.described() {
		if strings.HasPrefix(s, "fishing-dig") {
			t.Errorf("the wood was entered: %v", r.described())
		}
	}
	if !slices.ContainsFunc(r.described(), func(s string) bool { return strings.HasPrefix(s, "provision fishing-food") }) {
		t.Errorf("no food was asked for: %v", r.described())
	}
	if parked, _ := r.e.Store.Parked("fishingcompo", time.Now().Add(2*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
	r = setup(5)
	if res = r.run(FishingContest(), r.apply(r.happy())); res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("with five sharks the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A death on the worm trip parks the quest for a day by the step's own cap.
func TestTheFishingContestADeathInTheWoodParksItForADay(t *testing.T) {
	r := newFCRig(t)
	r.e.D.Chat = chatlog.New()
	r.give(questplan.CoinsID, 500)
	r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod, questplan.ObjGarlic, questplan.ObjSpade)
	r.set(questplan.FcStarted)
	ans := r.happy()
	ans[questplan.CustomFishDig] = func() bool {
		r.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	}
	res := r.run(FishingContest(), r.apply(ans))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "fishing-dig") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("fishingcompo", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A step that cannot be done parks with its reason and a timeout, and is run once: the garlic with no cupboard, the casts, the spare.
func TestTheFishingContestFailedLegsPark(t *testing.T) {
	for _, c := range []struct {
		name   string
		stage  int
		setup  func(r *fcRig)
		custom string
		want   string
	}{
		{"the garlic", questplan.FcStarted, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod)
			r.give(questplan.CoinsID, 500)
			r.give(questplan.ObjRedVineWorm, 3)
			r.noGarlic = true
		}, questplan.CustomFishGarlic, "garlic"},
		{"the casts", questplan.FcGarlicComp, func(r *fcRig) {
			r.pack(questplan.ObjFishingPass, questplan.ObjFishingRod)
			r.give(questplan.ObjRedVineWorm, 3)
		}, questplan.CustomFishFish, "fishing spot"},
		{"the spare trophy", questplan.FcWonComp, func(r *fcRig) { r.pack(questplan.ObjFishingPass) }, questplan.CustomFishSpare, "spare trophy"},
	} {
		r := newFCRig(t)
		c.setup(r)
		r.set(c.stage)
		runs := 0
		ans := r.happy()
		ans[c.custom] = func() bool { runs++; return false }
		res := r.run(FishingContest(), r.apply(ans))
		if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, c.want) {
			t.Errorf("%s: outcome %s after %d run(s): %q (%v)", c.name, res.Outcome, runs, res.Reason, r.log)
			continue
		}
		if parked, _ := r.e.Store.Parked("fishingcompo", time.Now().Add(29*time.Minute)); !parked {
			t.Errorf("%s: not parked for the half hour", c.name)
		}
	}
}
