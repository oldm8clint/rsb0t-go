package questmods

// Druidic Ritual and Jungle Potion through the real engine (Engine.Run) on a simulated session, in the manner of the fillers'
// (cooksheep_test.go): the module's own Decide runs against snapshots built from a fake pack, the quest tab's colour and the
// position, and every step it asks for is performed by a stub that changes that world the way the server would. What is proven is
// the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death cap and the
// completion record): each scenario reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that
// cannot be done parks the quest with its reason. What is not proven here is each custom's own live execution (the hunts, the door
// and its suits, the pick at a shared loc, the cave): those are the real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

func standAt(r *csRig, at tile.Tile) { r.sim.StandAt(at.X, at.Z) }

// ---------------------------------------------------------------------------------------------------------------------
// Druidic Ritual
// ---------------------------------------------------------------------------------------------------------------------

// drApply plays the content's side of Druidic Ritual on the rig: r.stage is %druidquest, the tab follows it.
func drApply(r *csRig, answers map[string]func() bool) func(step questplan.Step) bool {
	setStage := func(n int) {
		r.stage = n
		status := quests.InProgress
		switch n {
		case questplan.DruidNotStarted:
			status = quests.NotStarted
		case questplan.DruidComplete:
			status = quests.Complete
		}
		r.setTab(questplan.DruidicRitualName, status)
	}
	return func(step questplan.Step) bool {
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepTalk:
			switch step.Stop.ID {
			case questplan.NpcKaqemeex:
				switch r.stage {
				case questplan.DruidNotStarted:
					setStage(questplan.DruidStarted)
				case questplan.DruidGivenIngredients:
					setStage(questplan.DruidComplete)
				}
			case questplan.NpcSanfew:
				switch r.stage {
				case questplan.DruidStarted:
					setStage(questplan.DruidSpokenSanfew)
				case questplan.DruidSpokenSanfew:
					all := true
					for _, m := range questplan.DruidMeats {
						all = all && r.held(m.Enchanted) > 0
					}
					if all {
						for _, m := range questplan.DruidMeats {
							r.take(m.Enchanted, 1)
						}
						setStage(questplan.DruidGivenIngredients)
					}
				}
			}
			return true
		case questplan.StepDeposit:
			return true
		}
		f, ok := answers[key]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", key)
			return false
		}
		return f()
	}
}

// drAnswers are the executors of a healthy world: the kit gives the food, each hunt a raw meat, the way down the room and the dip
// the enchanted forms.
func drAnswers(r *csRig) map[string]func() bool {
	a := map[string]func() bool{
		"provision:" + questplan.KitDruidMeats: func() bool { r.give(385, 5); return true },
		questplan.CustomDruidEnter: func() bool {
			standAt(r, questplan.CauldronStand)
			return true
		},
		questplan.CustomDruidDip: func() bool {
			for _, m := range questplan.DruidMeats {
				if r.held(m.Raw) > 0 {
					r.take(m.Raw, 1)
					r.give(m.Enchanted, 1)
				}
			}
			return true
		},
	}
	for _, m := range questplan.DruidMeats {
		a[questplan.CustomDruidHunt(m)] = func() bool { r.give(m.Raw, 1); return true }
	}
	return a
}

// The whole quest in the content's order, every executor a stub.
func TestDruidicRitualReachesCompleteThroughTheEngine(t *testing.T) {
	r := newCSRig(t, questplan.FaladorWestBank)
	r.setTab(questplan.DruidicRitualName, quests.NotStarted)
	r.stageKnown = true
	res := r.run(DruidicRitual(), drApply(r, drAnswers(r)))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Kaqemeex", "talk to Sanfew", "provision druid-meats", "druid-hunt-bear", "druid-hunt-rat", "druid-hunt-chicken", "druid-hunt-beef",
		"druid-enter", "druid-dip", "talk to Sanfew", "talk to Kaqemeex")
	if got := r.e.Store.Record("druid").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, m := range questplan.DruidMeats {
		if r.held(m.Raw) != 0 || r.held(m.Enchanted) != 0 {
			t.Errorf("the %s meat is still held after the quest", m.Name)
		}
	}
}

// A hunt that finds nothing is the quest's own: the engine parks it on the Decide's wait, with the reason, in the side store, and
// the hunt is run once.
func TestDruidicRitualAHuntThatFindsNothingParksTheQuest(t *testing.T) {
	r := newCSRig(t, questplan.DraynorBank)
	r.setTab(questplan.DruidicRitualName, quests.InProgress)
	r.stage, r.stageKnown = questplan.DruidSpokenSanfew, true
	a := drAnswers(r)
	runs := 0
	a[questplan.CustomDruidHunt(questplan.DruidMeats[0])] = func() bool { runs++; return false }
	res := r.run(DruidicRitual(), drApply(r, a))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "bear") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("druid", time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
	if parked, _ := r.e.Store.Parked("druid", time.Now().Add(31*time.Minute)); parked {
		t.Error("parked past the half hour")
	}
}

// A door that does not open parks the quest, and the executor's own request (the suit not beaten) is served by the engine as the
// executor asked.
func TestDruidicRitualADoorThatWillNotOpenParksTheQuest(t *testing.T) {
	r := newCSRig(t, questplan.FaladorWestBank)
	r.setTab(questplan.DruidicRitualName, quests.InProgress)
	r.stage, r.stageKnown = questplan.DruidSpokenSanfew, true
	for _, m := range questplan.DruidMeats {
		r.give(m.Raw, 1)
	}
	a := drAnswers(r)
	runs := 0
	a[questplan.CustomDruidEnter] = func() bool {
		runs++
		r.e.RequestPark("the Prison door of the Cauldron of Thunder did not open after the suits of armour were fought", questplan.DruidDungeonPark)
		return false
	}
	res := r.run(DruidicRitual(), drApply(r, a))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "Prison door") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
}

// One death parks the quest for a day (MaxDeaths 1): the meats the account carried are gone and the price was wrong for it.
func TestDruidicRitualOneDeathParksItForADay(t *testing.T) {
	r := newCSRig(t, questplan.DraynorBank)
	r.e.D.Chat = chatlog.New()
	r.setTab(questplan.DruidicRitualName, quests.InProgress)
	r.stage, r.stageKnown = questplan.DruidSpokenSanfew, true
	a := drAnswers(r)
	a[questplan.CustomDruidHunt(questplan.DruidMeats[0])] = func() bool {
		r.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	}
	res := r.run(DruidicRitual(), drApply(r, a))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("druid", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Jungle Potion
// ---------------------------------------------------------------------------------------------------------------------

// jpRig is Jungle Potion's state on the rig: c is %junglepotion; r.stage is what the journal would render for the pack.
type jpRig struct {
	*csRig
	c int
}

// render sets the rig's stage the way junglepotion_journal.rs2 reads the pack, and the tab with it.
func (j *jpRig) render() {
	j.stage = j.c
	if j.c >= 1 && j.c < questplan.JungleFoundAllHerbs && j.c%2 == 0 {
		h := (j.c - 2) / 2
		herb := questplan.JungleHerbs[h]
		switch {
		case j.held(herb.Clean) > 0:
		case j.held(herb.Unid) > 0 && h == 0:
			j.stage = 2
		default:
			j.stage = j.c - 1
		}
	}
	status := quests.InProgress
	switch {
	case j.c == questplan.JungleNotStarted:
		status = quests.NotStarted
	case j.c >= questplan.JungleComplete:
		status = quests.Complete
	}
	j.setTab(questplan.JunglePotionName, status)
}

func jpApply(j *jpRig, answers map[string]func() bool) func(step questplan.Step) bool {
	return func(step questplan.Step) bool {
		defer j.render()
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepOpHeld:
			for _, h := range questplan.JungleHerbs {
				if h.Unid == step.ItemID && j.held(h.Unid) > 0 {
					j.take(h.Unid, 1)
					j.give(h.Clean, 1)
				}
			}
			return true
		case questplan.StepDeposit:
			return true
		}
		f, ok := answers[key]
		if !ok {
			j.t.Errorf("the scenario has no answer for the step %q", key)
			return false
		}
		return f()
	}
}

func jpAnswers(j *jpRig) (map[string]func() bool, *int) {
	picks := 0
	return map[string]func() bool{
		"provision:" + questplan.KitJungleSupplies: func() bool { j.give(questplan.CoinsID, 300); j.give(385, 6); return true },
		questplan.CustomJungleStart: func() bool {
			standAt(j.csRig, questplan.TrufitusAnchor)
			if j.c == questplan.JungleNotStarted {
				j.c = 1
			}
			return true
		},
		questplan.CustomJunglePick: func() bool {
			// The step's herb is the one the content wants now (the Decide's Qty).
			h := (j.c - 1) / 2
			herb := questplan.JungleHerbs[h]
			standAt(j.csRig, herb.Stand)
			picks++
			j.give(herb.Unid, 1)
			if j.c == 2*h+1 {
				j.c++
			}
			return true
		},
		questplan.CustomJungleHandIn: func() bool {
			standAt(j.csRig, questplan.TrufitusAnchor)
			h := (j.c - 2) / 2
			herb := questplan.JungleHerbs[h]
			if j.held(herb.Clean) > 0 {
				j.take(herb.Clean, 1)
				j.c++
				if h == len(questplan.JungleHerbs)-1 {
					j.c = questplan.JungleFoundAllHerbs
				}
			}
			return true
		},
		questplan.CustomJungleFinish: func() bool {
			standAt(j.csRig, questplan.TrufitusAnchor)
			j.c = questplan.JungleComplete
			return true
		},
	}, &picks
}

func newJPRig(t *testing.T, at tile.Tile, c int) *jpRig {
	j := &jpRig{csRig: newCSRig(t, at), c: c}
	j.stageKnown = true
	j.render()
	return j
}

// The whole quest in the content's order, every executor a stub: the supplies pass at the mainland bank, the start, five herbs each
// picked, cleaned and handed over, the finish.
func TestJunglePotionReachesCompleteThroughTheEngine(t *testing.T) {
	j := newJPRig(t, questplan.FaladorWestBank, questplan.JungleNotStarted)
	answers, picks := jpAnswers(j)
	res := j.run(JunglePotion(), jpApply(j, answers))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, j.log)
	}
	want := []string{"provision jungle-supplies", "jungle-start"}
	for range questplan.JungleHerbs {
		want = append(want, "jungle-pick", "Identify", "jungle-handin")
	}
	want = append(want, "jungle-finish")
	j.wantSteps(want...)
	if *picks != 5 {
		t.Errorf("%d picks, want five", *picks)
	}
	if got := j.e.Store.Record("junglepotion").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, h := range questplan.JungleHerbs {
		if j.held(h.Unid) != 0 || j.held(h.Clean) != 0 {
			t.Errorf("the %s is still held after the quest", h.Name)
		}
	}
}

// A pick that cannot be done (the executor asks for the park: the loc never grew its herb back) parks the quest with the reason,
// and is run once.
func TestJunglePotionAPickThatFindsNothingParksTheQuest(t *testing.T) {
	j := newJPRig(t, questplan.TrufitusAnchor, 1)
	j.give(questplan.CoinsID, 300)
	j.give(385, 6)
	answers, _ := jpAnswers(j)
	runs := 0
	answers[questplan.CustomJunglePick] = func() bool {
		runs++
		j.e.RequestPark("no Snake weed to search for in 3m0s", questplan.JungleLegPark)
		return false
	}
	res := j.run(JunglePotion(), jpApply(j, answers))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "Snake weed") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, j.log)
	}
	if parked, _ := j.e.Store.Parked("junglepotion", time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
}

// An errand that failed without asking for a park is not repeated: the Decide's own wait parks the quest on it.
func TestJunglePotionAFailedHandInParksOnTheDecidesWait(t *testing.T) {
	j := newJPRig(t, questplan.TrufitusAnchor, 2)
	j.give(questplan.CoinsID, 300)
	j.give(385, 6)
	j.give(questplan.JungleHerbs[0].Clean, 1)
	j.render()
	answers, _ := jpAnswers(j)
	runs := 0
	answers[questplan.CustomJungleHandIn] = func() bool { runs++; return false }
	res := j.run(JunglePotion(), jpApply(j, answers))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, questplan.CustomJungleHandIn) {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, j.log)
	}
}

// One death parks the quest for a day (MaxDeaths 1).
func TestJunglePotionOneDeathParksItForADay(t *testing.T) {
	j := newJPRig(t, questplan.TrufitusAnchor, 9)
	j.e.D.Chat = chatlog.New()
	j.give(questplan.CoinsID, 300)
	j.give(385, 6)
	answers, _ := jpAnswers(j)
	answers[questplan.CustomJunglePick] = func() bool {
		j.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	}
	res := j.run(JunglePotion(), jpApply(j, answers))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, j.log)
	}
	if parked, _ := j.e.Store.Parked("junglepotion", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// The factories build what the plan names: every custom the Decide can ask for has an executor.
func TestDruidAndJunglePotionRegisterTheirExecutors(t *testing.T) {
	dr, jp := DruidicRitual(), JunglePotion()
	want := []string{questplan.CustomDruidEnter, questplan.CustomDruidDip}
	for _, m := range questplan.DruidMeats {
		want = append(want, questplan.CustomDruidHunt(m))
	}
	for _, name := range want {
		if dr.Customs[name] == nil {
			t.Errorf("Druidic Ritual has no executor for %q", name)
		}
	}
	if len(dr.Customs) != len(want) {
		t.Errorf("Druidic Ritual registers %d executors, the plan names %d", len(dr.Customs), len(want))
	}
	for _, name := range []string{questplan.CustomJungleStart, questplan.CustomJunglePick, questplan.CustomJungleHandIn, questplan.CustomJungleFinish} {
		if jp.Customs[name] == nil {
			t.Errorf("Jungle Potion has no executor for %q", name)
		}
	}
	if len(jp.Customs) != 4 {
		t.Errorf("Jungle Potion registers %d executors", len(jp.Customs))
	}
	if dr.ID() != "druid" || jp.ID() != "junglepotion" {
		t.Errorf("ids %q %q", dr.ID(), jp.ID())
	}
	// The cave is a pocket for every quest's walks.
	if name, ok := questrunner.PocketAt(questplan.JungleCaveArrival); !ok || !strings.Contains(name, "cave") {
		t.Errorf("the cave's arrival is in the pocket %q (%v)", name, ok)
	}
	if _, ok := questrunner.PocketAt(questplan.TrufitusAnchor); ok {
		t.Error("Trufitus's hut is in a pocket")
	}
}
