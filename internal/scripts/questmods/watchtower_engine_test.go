package questmods

// The Watch Tower through the real engine (Engine.Run) on a simulated session, in the manner of the other quests' engine tests: the module's own Decide
// runs against snapshots built from a fake pack, the quest tab's colour, the position and the journal state, and every step it asks for is performed by a
// stub that changes that world the way the content does (quest_itwatchtower's scripts). What is proven is the state machine under the engine's
// bookkeeping: the order of the legs, the items each hand-in takes, a healthy run never parked, a lost key or crystal asked for again, and a quest short of
// a level, a supply or a price parked at the first step it cannot do, with the reason. Each custom's own live execution is the real-data tests' and the
// logs'.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Skill indices of the protocol's stat table the quest reads.
const (
	wtsMagic    = 6
	wtsMining   = 14
	wtsHerblore = 15
	wtsAgility  = 16
	wtsThieving = 17
)

// wtRig is the quest's state on the rig: st is %itwatchtower and its bits as the journal tells them, and the rig's stage follows it.
type wtRig struct {
	*csRig
	st         questplan.WtState
	partsGiven int
	// wizardHas is the relic pieces the wizard holds (the content's bits relic1 to relic3); asked is how often each giver's lost-piece menu was used.
	wizardHas map[int]bool
	asked     map[string]int
}

func newWtRig(t *testing.T) *wtRig { return newWtRigAs(t, 99, 99, 99, 99) }

// newWtRigAs is the rig with an account of these combat levels (the hitpoints are seeded once: the simulated session keeps the highest it was given as the
// account's maximum, so a rig that seeds them twice is a hurt account that never mends).
func newWtRigAs(t *testing.T, attack, strength, defence, hp int) *wtRig {
	r := &wtRig{csRig: newCSRig(t, questplan.WtBank), st: questplan.WtState{Stage: questplan.WtNotStarted, Left: -1}, wizardHas: map[int]bool{}, asked: map[string]int{}}
	r.stageKnown = true
	// An account the dragons' nest can be asked of, with every level the quest asks (the tests lower the one they are about): Defence 99, 99 hitpoints and an
	// anti-dragon shield worn, which the simulated session does not track and the hook adds to the snapshot. Sixteen sharks come from the food round.
	osSeed(r.csRig, osAttack, attack)
	osSeed(r.csRig, osStrength, strength)
	osSeed(r.csRig, osDefence, defence)
	osSeed(r.csRig, gamestate.HitpointsStat, hp)
	for stat, lv := range map[int]int{wtsMagic: 70, wtsMining: 60, wtsHerblore: 50, wtsAgility: 40, wtsThieving: 77} {
		osSeed(r.csRig, stat, lv)
	}
	r.snapHook = func(s *questplan.Snapshot) {
		s.WornIDs = map[int]int{questplan.WtObjAntiDragonShield: 1}
	}
	r.sync()
	return r
}

// sync tells the rig's snapshot what the journal and the tab would say.
func (r *wtRig) sync() {
	r.stage = r.st.Pack()
	switch {
	case r.st.Stage >= questplan.WtComplete:
		r.setTab(questplan.WatchtowerName, quests.Complete)
	case r.st.Stage == questplan.WtNotStarted:
		r.setTab(questplan.WatchtowerName, quests.NotStarted)
	default:
		r.setTab(questplan.WatchtowerName, quests.InProgress)
	}
}

func (r *wtRig) stand(t tile.Tile) { r.sim.StandAtLevel(t.X, t.Z, t.Level) }

func (r *wtRig) setStage(n int) { r.st.Stage = n; r.sync() }

func (r *wtRig) flag(f uint32) { r.st.Flags |= f; r.sync() }

func (r *wtRig) packIDs() map[int]int {
	out := map[int]int{}
	for _, s := range r.sim.Pack() {
		out[int(s.ID)] += s.Count
	}
	return out
}

// fill is a provisioning round that found everything: the bank, the mule and the shops hold every line the kit asks for.
func (r *wtRig) fill(kit []questplan.KitLine) {
	for _, l := range kit {
		if l.Optional {
			continue
		}
		if need := l.Needs(r.packIDs(), nil); need > 0 {
			r.give(l.ID, need)
		}
	}
}

// apply performs a step the way the content does.
func (r *wtRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		r.fill(step.Kit)
		if step.KitName == questplan.KitWtFood {
			r.give(questplan.WtObjShark, 13-r.held(questplan.WtObjShark))
		}
		return true
	case questplan.StepDeposit, questplan.StepScanBank, questplan.StepWithdraw:
		return true
	case questplan.StepOpHeld:
		r.take(step.ItemID, 1)
		return true
	case questplan.StepTalk:
		r.stand(step.Stop.Anchor)
		return r.talk(step.Stop.Npc)
	case questplan.StepWalk:
		// The walk out of a hunted pocket before a park (the engine's walk leaves the pocket by its own way first).
		r.stand(step.Anchor)
		return true
	}
	return r.custom(step)
}

func (r *wtRig) talk(npc string) bool {
	has := func(id int) bool { return r.held(id) > 0 }
	switch npc {
	case "Watchtower wizard":
		switch r.st.Stage {
		case questplan.WtNotStarted:
			r.setStage(questplan.WtStarted)
		case questplan.WtStarted:
			if !has(questplan.WtObjFingernails) {
				r.t.Errorf("the wizard was shown nothing")
				return false
			}
			r.take(questplan.WtObjFingernails, 1)
			r.setStage(questplan.WtGivenFingernails)
		case questplan.WtFedNightshade:
			r.setStage(questplan.WtLearnedPotion)
		default:
			r.t.Errorf("a talk with the wizard at stage %d", r.st.Stage)
			return false
		}
	case "Og":
		switch {
		case !r.st.Has(questplan.WtOgSpoken):
			r.flag(questplan.WtOgSpoken)
			r.give(questplan.WtObjTobanKey, 1)
		case has(questplan.WtObjStolenGold):
			r.take(questplan.WtObjStolenGold, 1)
			r.give(questplan.WtObjPart1, 1)
			r.flag(questplan.WtOgHelped)
		case !r.st.Has(questplan.WtOgHelped) && !has(questplan.WtObjTobanKey):
			// "I have lost the key!"
			r.give(questplan.WtObjTobanKey, 1)
		default:
			r.t.Errorf("a pointless talk with Og")
			return false
		}
	case "Grew":
		switch {
		case !r.st.Has(questplan.WtGrewSpoken):
			r.flag(questplan.WtGrewSpoken)
		case has(questplan.WtObjTooth):
			r.take(questplan.WtObjTooth, 1)
			r.give(questplan.WtObjPart2, 1)
			r.give(questplan.WtObjCrystal1, 1)
			r.flag(questplan.WtGrewHelped)
		case r.st.Has(questplan.WtGrewHelped) && !has(questplan.WtObjCrystal1):
			// "I've lost the crystal you gave me."
			r.give(questplan.WtObjCrystal1, 1)
		default:
			r.t.Errorf("a pointless talk with Grew")
			return false
		}
	case "Toban":
		switch {
		case !r.st.Has(questplan.WtTobanSpoken):
			r.flag(questplan.WtTobanSpoken)
		case has(questplan.WtObjDragonBones):
			r.take(questplan.WtObjDragonBones, 1)
			r.give(questplan.WtObjPart3, 1)
			r.flag(questplan.WtTobanHelped)
		default:
			r.t.Errorf("a pointless talk with Toban")
			return false
		}
	case "City guard":
		if r.st.Stage != questplan.WtGivenRelic {
			r.t.Errorf("a talk with the City guard at stage %d", r.st.Stage)
			return false
		}
		r.setStage(questplan.WtGivenRiddle)
	default:
		r.t.Errorf("a talk with %s", npc)
		return false
	}
	return true
}

func (r *wtRig) custom(step questplan.Step) bool {
	has := func(id int) bool { return r.held(id) > 0 }
	switch step.Custom {
	case questplan.CustomWtClimb:
		r.stand(questplan.WtWizardAnchor)
	case questplan.CustomWtDown:
		r.stand(tile.Tile{X: 2544, Z: 3112})
	case questplan.CustomWtBush:
		r.stand(questplan.WtBushStand)
		r.give(questplan.WtObjFingernails, 1)
	case questplan.WtGivePartName(questplan.WtObjPart1), questplan.WtGivePartName(questplan.WtObjPart2), questplan.WtGivePartName(questplan.WtObjPart3):
		if !has(step.Qty) {
			r.t.Errorf("a part the pack does not hold (%d) was given", step.Qty)
			return false
		}
		r.stand(questplan.WtWizardAnchor)
		if r.wizardHas[step.Qty] {
			// "I already have that part..." The piece stays in the pack.
			return false
		}
		r.take(step.Qty, 1)
		r.wizardHas[step.Qty] = true
		r.partsGiven++
		if r.partsGiven == 3 {
			r.give(questplan.WtObjRelic, 1)
		}
	case questplan.CustomWtRelicBack:
		// The wizard's lost-relic menu is offered at stage 3 (the relic made) only; at stage 2 he says there is nothing he can do.
		r.stand(questplan.WtWizardAnchor)
		r.asked["wizard"]++
		if r.partsGiven == 3 && !has(questplan.WtObjRelic) {
			r.give(questplan.WtObjRelic, 1)
			return true
		}
		return false
	case questplan.CustomWtAskOg, questplan.CustomWtAskGrew, questplan.CustomWtAskToban:
		part := map[string]int{questplan.CustomWtAskOg: questplan.WtObjPart1, questplan.CustomWtAskGrew: questplan.WtObjPart2, questplan.CustomWtAskToban: questplan.WtObjPart3}[step.Custom]
		r.asked[step.Custom]++
		if has(part) {
			r.t.Errorf("%s was asked for a piece the pack holds", step.Custom)
			return false
		}
		// Grew's menu hides the option once the wizard holds his piece; Og's and Toban's offer it whatever the wizard holds.
		if step.Custom == questplan.CustomWtAskGrew && r.wizardHas[part] {
			return false
		}
		r.give(part, 1)
	case questplan.CustomWtIslandIn:
		if !has(questplan.WtObjRope) {
			r.t.Errorf("the swing was made without a rope")
			return false
		}
		r.take(questplan.WtObjRope, 1)
		r.stand(questplan.WtGrewAnchor)
	case questplan.CustomWtCampIn:
		r.stand(questplan.WtCampArrival)
	case questplan.CustomWtChest:
		if !has(questplan.WtObjTobanKey) {
			r.t.Errorf("the chest was opened without its key")
			return false
		}
		r.give(questplan.WtObjStolenGold, 1)
	case questplan.CustomWtGorad:
		r.give(questplan.WtObjTooth, 1)
	case questplan.CustomWtRelicGate:
		r.take(questplan.WtObjRelic, 1)
		r.setStage(questplan.WtGivenRelic)
		r.stand(questplan.WtRelicGateInside)
	case questplan.CustomWtCake:
		r.give(questplan.WtObjRockCake, 1)
		r.stand(questplan.WtStallStand)
	case questplan.CustomWtBattlement:
		r.take(questplan.WtObjRockCake, 1)
		r.flag(questplan.WtMarketAsked | questplan.WtMarketPaid)
		r.stand(tile.Tile{X: 2510, Z: 3012})
	case questplan.CustomWtJump:
		r.take(questplan.WtObjCoins, questplan.WtChasmToll)
		r.stand(questplan.WtJumpBackStand)
	case questplan.CustomWtRiddleGive:
		r.take(questplan.WtObjDeathRune, 1)
		r.give(questplan.WtObjMap, 1)
		r.setStage(questplan.WtSolvedRiddle)
	case questplan.CustomWtCave:
		switch {
		case step.Qty == 5:
			r.flag(questplan.WtLearning)
		case step.Qty >= 1 && step.Qty <= 4:
			r.flag(questplan.WtCaveFlag[step.Qty-1])
		default:
			r.t.Errorf("cave %d is not a talker's", step.Qty)
			return false
		}
		r.stand(questplan.WtCaveExits[step.Qty-1])
	case questplan.CustomWtMad:
		if step.Qty == 0 {
			r.take(questplan.WtObjGoldBar, 1)
		}
		r.give(questplan.WtObjCrystal2, 1)
		if r.st.Stage == questplan.WtSolvedRiddle {
			r.setStage(questplan.WtSkavidCrystal)
		}
		r.stand(questplan.WtEastGateStand)
	case questplan.CustomWtNightshade:
		r.give(questplan.WtObjNightshade, 1)
		r.stand(questplan.WtCaveExits[2])
	case questplan.CustomWtEnclaveIn:
		r.take(questplan.WtObjNightshade, 1)
		if r.st.Stage == questplan.WtSkavidCrystal {
			r.setStage(questplan.WtFedNightshade)
		}
		r.stand(questplan.WtEnclaveArrive)
	case questplan.CustomWtEnclaveOut:
		r.stand(questplan.WtEnclaveGuard)
	case questplan.CustomWtBrew:
		for _, id := range []int{questplan.WtObjGuamLeaf, questplan.WtObjVialWater, questplan.WtObjJangerberries, questplan.WtObjBatBones} {
			if !has(id) {
				r.t.Errorf("the brew was started without item %d", id)
				return false
			}
			r.take(id, 1)
		}
		r.give(questplan.WtObjOgrePotion, 1)
	case questplan.CustomWtBerries:
		if !has(questplan.WtObjRope) {
			r.t.Errorf("the swing was made without a rope")
			return false
		}
		r.take(questplan.WtObjRope, 1)
		r.give(questplan.WtObjJangerberries, step.Qty)
		r.stand(questplan.WtSwingLanding)
	case questplan.CustomWtInfuse:
		r.take(questplan.WtObjOgrePotion, 1)
		r.give(questplan.WtObjMagicPotion, 1)
		r.st.Left = questplan.WtShamans
		r.setStage(questplan.WtMadePotion)
		r.stand(questplan.WtWizardAnchor)
	case questplan.CustomWtDissolve:
		if !has(questplan.WtObjMagicPotion) {
			r.t.Errorf("the shamans were dissolved without the potion")
			return false
		}
		r.take(questplan.WtObjMagicPotion, 1)
		if r.held(questplan.WtObjNightshade) > 0 {
			r.take(questplan.WtObjNightshade, 1)
		}
		r.give(questplan.WtObjCrystal3, 1)
		r.st.Left = 0
		r.sync()
		r.stand(questplan.WtRockStand)
	case questplan.CustomWtRock:
		if !has(questplan.WtObjPickaxe) {
			r.t.Errorf("the rock was mined without a pickaxe")
			return false
		}
		r.give(questplan.WtObjCrystal4, 1)
		r.flag(questplan.WtMinedRock)
	case questplan.CustomWtCrystals:
		r.setStage(questplan.WtFoundAll)
		r.stand(questplan.WtWizardAnchor)
	case questplan.CustomWtLever:
		r.give(questplan.WtObjSpellScroll, 1)
		for _, c := range []int{questplan.WtObjCrystal1, questplan.WtObjCrystal2, questplan.WtObjCrystal3, questplan.WtObjCrystal4} {
			r.take(c, 1)
		}
		r.setStage(questplan.WtComplete)
		r.stand(questplan.WtMirrorLanding)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// wtHappyPath is the distinct steps of a healthy run, a repeat of the same description collapsed (csRig.run).
var wtHappyPath = []string{
	"wt-climb", "talk to Watchtower wizard", // the start, by the wall
	"wt-bush", "wt-climb", "talk to Watchtower wizard", // the evidence
	"provision wt-bones", "provision wt-food", "talk to Og", // the bones before any risk, then Og's key
	"provision wt-island-kit", "wt-island-in", "talk to Grew", // Grew's task
	"wt-camp-in", "wt-chest", "wt-gorad", "talk to Toban", // the camp: Gorad before Toban, so that Toban's piece is not in the pack while the fight is on
	"provision wt-island-kit", "wt-island-in", "talk to Grew", // the tooth, over the swing again with a second rope
	"talk to Og",                                                                   // the gold
	"wt-give-part-2373", "wt-give-part-2374", "wt-give-part-2375", "wt-relic-gate", // the relic (pieces 1 to 3 by object id)
	"wt-cake", "wt-battlement", "provision wt-city", "wt-jump", "talk to City guard", "wt-riddle-give",
	"provision wt-caves", "wt-cave", "wt-cave", "wt-cave", "wt-cave", "wt-cave", // the scared skavid, then the four talkers
	"provision wt-bar", "wt-mad", "wt-nightshade", "wt-enclave-in", "wt-climb", "talk to Watchtower wizard",
	"provision wt-potion-scarce", "provision wt-island-kit", "wt-berries", "provision wt-potion", "wt-brew", "wt-infuse",
	"provision wt-rock-kit", "wt-nightshade", "wt-dissolve", "wt-rock", "wt-enclave-out", "wt-crystals", "wt-lever",
	"Read Spell scroll", "wt-down",
}

// wtWantSteps compares the distinct steps with a list of prefixes, naming the first mismatch.
func (r *wtRig) wtWantSteps(want []string) {
	r.t.Helper()
	got := r.described()
	for i, w := range want {
		if i >= len(got) || !strings.HasPrefix(got[i], w) {
			at := "the end of the run"
			if i < len(got) {
				at = got[i]
			}
			r.t.Fatalf("step %d is %q, want a step starting %q\nall:\n  %s", i, at, w, strings.Join(got, "\n  "))
		}
	}
	if len(got) != len(want) {
		r.t.Fatalf("%d steps, want %d\nall:\n  %s", len(got), len(want), strings.Join(got, "\n  "))
	}
}

func (r *wtRig) ranStep(prefix string) bool {
	for _, s := range r.described() {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// The whole quest in the content's order, every executor a stub.
func TestWatchtowerReachesCompleteThroughTheEngine(t *testing.T) {
	r := newWtRig(t)
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): last step %q\n%v\nsteps:\n  %s", res.Outcome, res.Reason, res.LastStep, r.log, strings.Join(r.described(), "\n  "))
	}
	r.wtWantSteps(wtHappyPath)
	if r.st.Stage != questplan.WtComplete {
		t.Errorf("the quest stands at stage %d", r.st.Stage)
	}
	if r.partsGiven != 3 {
		t.Errorf("the wizard was given %d parts", r.partsGiven)
	}
	if r.held(questplan.WtObjSpellScroll) != 0 || r.held(questplan.WtObjRelic) != 0 {
		t.Error("the scroll was not read or the relic was kept")
	}
	if got := r.e.Store.Record(questplan.WatchtowerID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// Every executor is a stub here, so nothing was asked of the engine's parking or of a death.
	if rec := r.e.Store.Record(questplan.WatchtowerID); rec.Parks != 0 || rec.Deaths != 0 {
		t.Errorf("record %+v: a healthy run is never parked", rec)
	}
}

// An account that lacks Agility 18 does nothing at all: the wall is the first step.
func TestWatchtowerParksAtTheWallWithoutAgility(t *testing.T) {
	r := newWtRig(t)
	osSeed(r.csRig, wtsAgility, 12)
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "agility 18") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	if len(r.steps) != 0 {
		t.Errorf("steps %v: nothing should have been done", r.described())
	}
}

// The fleet's account on 19 Oct 2026 (Agility 35, Thieving 78, nobody holds dragon bones) starts the quest, brings the evidence and parks at the bones
// before the first risk, with the supply in the reason.
func TestWatchtowerBuildsUpAndParksOnTheBonesBeforeAnyRisk(t *testing.T) {
	r := newWtRig(t)
	osSeed(r.csRig, wtsAgility, 35)
	osSeed(r.csRig, wtsHerblore, 1)
	osSeed(r.csRig, wtsMining, 3)
	// The provisioning round for the bones finds none: the stub fails it as the real round does (nothing in the bank or the mule, no shop).
	apply := func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitWtBones {
			return false
		}
		return r.apply(step)
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "dragon bones") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	r.wtWantSteps([]string{"wt-climb", "talk to Watchtower wizard", "wt-bush", "wt-climb", "talk to Watchtower wizard", "provision wt-bones"})
	if r.st.Stage != questplan.WtGivenFingernails {
		t.Errorf("the quest stands at stage %d, want 2 (started, evidence handed in)", r.st.Stage)
	}
}

// With the bones and the mule stocked, the level of each step is that step's gate: the run builds up to the first one the account lacks.
func TestWatchtowerParksAtTheFirstStepALevelForbids(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stat  int
		level int
		words string
		// done is what must have happened before the park; stage is where the quest stands.
		done  []string
		stage int
	}{
		{"herblore", wtsHerblore, 3, "herblore 14", []string{"wt-gorad", "wt-relic-gate", "wt-jump", "wt-mad", "wt-enclave-in"}, questplan.WtLearnedPotion},
		{"magic", wtsMagic, 10, "magic 14", []string{"wt-brew", "wt-infuse"}, questplan.WtMadePotion},
		{"thieving", wtsThieving, 9, "thieving 15", []string{"wt-gorad", "wt-relic-gate"}, questplan.WtGivenRelic},
		// Agility 20 climbs the wall (18) and swings (10) and fails the chasm (25).
		{"agility", wtsAgility, 20, "agility 25", []string{"wt-gorad", "wt-relic-gate", "wt-cake", "wt-battlement"}, questplan.WtGivenRelic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWtRig(t)
			osSeed(r.csRig, tc.stat, tc.level)
			res := r.run(Watchtower(), r.apply)
			if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), tc.words) {
				t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
			}
			if r.st.Stage != tc.stage {
				t.Errorf("the quest stands at stage %d, want %d", r.st.Stage, tc.stage)
			}
			for _, want := range tc.done {
				if !r.ranStep(want) {
					t.Errorf("%s never ran before the park\nsteps:\n  %s", want, strings.Join(r.described(), "\n  "))
				}
			}
		})
	}
}

// Mining 40 is the rock's, the last of the gates: the shamans are all dissolved first, and the park is at the rock.
func TestWatchtowerParksAtTheRockOnlyAfterTheShamans(t *testing.T) {
	r := newWtRig(t)
	osSeed(r.csRig, wtsMining, 12)
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "mining 40") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	if r.st.Left != 0 || r.held(questplan.WtObjCrystal3) == 0 || r.held(questplan.WtObjCrystal4) != 0 {
		t.Errorf("state %s: the six shamans are gone and the rock is not mined", r.st)
	}
	if r.ranStep("wt-rock") || !r.ranStep("wt-dissolve") {
		t.Errorf("steps %v: the shamans come first and the rock is not asked for at Mining 12", r.described())
	}
	// The park is made OUTSIDE the nest: a level found missing with the account among the dragons walks it out first (the last step is the way out), and an
	// account parked inside would be hit until it was killed.
	if got := r.described(); !strings.HasPrefix(got[len(got)-1], "wt-enclave-out") {
		t.Errorf("the last step before the park is %q, want the way out of the enclave", got[len(got)-1])
	}
}

// A weak account is refused by the model before it walks into ogre country: the first leg parks with the price, and the bones were asked for first.
func TestWatchtowerParksAWeakAccountBeforeTheOgres(t *testing.T) {
	r := newWtRigAs(t, 40, 40, 40, 40)
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "ogre") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	for _, s := range []string{"talk to Og", "wt-island-in", "wt-camp-in"} {
		if r.ranStep(s) {
			t.Errorf("a weak account went into ogre country: %s", s)
		}
	}
	if !r.ranStep("provision wt-bones") || r.st.Stage != questplan.WtGivenFingernails {
		t.Errorf("stage %d, steps %v", r.st.Stage, r.described())
	}
}

// An account with no food and none to be had is not sent in: the quest parks with that reason.
func TestWatchtowerParksWithoutFood(t *testing.T) {
	r := newWtRig(t)
	apply := func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitWtFood {
			return true // the round came back with nothing: the line is optional
		}
		return r.apply(step)
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "no food") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	if r.ranStep("talk to Og") {
		t.Error("an account with no food went to Og")
	}
}

// A key lost between Og and the chest is asked of Og again ("I have lost the key!") and the run goes on to Complete.
func TestWatchtowerAsksOgForALostKey(t *testing.T) {
	r := newWtRig(t)
	lost := false
	apply := func(step questplan.Step) bool {
		ok := r.apply(step)
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomWtIslandIn && !lost {
			lost = true
			r.take(questplan.WtObjTobanKey, 1)
		}
		return ok
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	og := 0
	for _, s := range r.described() {
		if strings.HasPrefix(s, "talk to Og") {
			og++
		}
	}
	if og != 3 {
		t.Errorf("Og was talked to %d times, want 3 (the key, the key again, the gold)\nsteps:\n  %s", og, strings.Join(r.described(), "\n  "))
	}
}

// A crystal lost after the shamans is asked of its giver again, over the swing with a fresh rope, and the run goes on to Complete.
func TestWatchtowerAsksGrewForALostCrystal(t *testing.T) {
	r := newWtRig(t)
	lost := false
	apply := func(step questplan.Step) bool {
		ok := r.apply(step)
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomWtRock && !lost {
			lost = true
			r.take(questplan.WtObjCrystal1, 1)
		}
		return ok
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	got := r.described()
	rockAt, lastGrew := -1, -1
	for i, s := range got {
		if strings.HasPrefix(s, "wt-rock") {
			rockAt = i
		}
		if strings.HasPrefix(s, "talk to Grew") {
			lastGrew = i
		}
	}
	if rockAt < 0 || lastGrew < rockAt {
		t.Errorf("Grew was not asked again after the rock\nsteps:\n  %s", strings.Join(got, "\n  "))
	}
}

// A crystal of the mad skavid's lost is asked of him again (Qty 1: he gives it for nothing now).
func TestWatchtowerAsksTheMadSkavidForALostCrystal(t *testing.T) {
	r := newWtRig(t)
	lost := false
	apply := func(step questplan.Step) bool {
		ok := r.apply(step)
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomWtRock && !lost {
			lost = true
			r.take(questplan.WtObjCrystal2, 1)
		}
		return ok
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	mad := 0
	for _, st := range r.steps {
		if st.Kind == questplan.StepCustom && st.Custom == questplan.CustomWtMad {
			mad++
		}
	}
	if mad != 2 {
		t.Errorf("the mad skavid was visited %d times, want 2", mad)
	}
}

// A custom that fails parks the quest for its leg rather than looping on it.
func TestWatchtowerParksAFailedLegInsteadOfLooping(t *testing.T) {
	r := newWtRig(t)
	apply := func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomWtBush {
			return false
		}
		return r.apply(step)
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, questplan.CustomWtBush) {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	bush := 0
	for _, s := range r.described() {
		if strings.HasPrefix(s, "wt-bush") {
			bush++
		}
	}
	if bush != 1 {
		t.Errorf("the bush was searched %d times", bush)
	}
}

// Every custom the decision can ask for has an executor, and every executor is asked for by the decision (the pockets' own ways out are not customs).
func TestWatchtowerEveryCustomHasAnExecutor(t *testing.T) {
	q := Watchtower()
	asked := map[string]bool{}
	inv := func(ids ...int) map[int]int {
		m := map[int]int{questplan.WtObjShark: 10}
		for _, id := range ids {
			m[id] = 1
		}
		return m
	}
	everything := inv(questplan.WtObjDragonBones, questplan.WtObjRope, questplan.WtObjTobanKey, questplan.WtObjStolenGold, questplan.WtObjTooth, questplan.WtObjRelic,
		questplan.WtObjRockCake, questplan.WtObjDeathRune, questplan.WtObjMap, questplan.WtObjLitCandle, questplan.WtObjGoldBar, questplan.WtObjNightshade,
		questplan.WtObjMagicPotion, questplan.WtObjPickaxe, questplan.WtObjOgrePotion, questplan.WtObjGuamLeaf, questplan.WtObjBatBones, questplan.WtObjVialWater,
		questplan.WtObjPestle, questplan.WtObjJangerberries, questplan.WtObjFingernails, questplan.WtObjPart1, questplan.WtObjSpellScroll)
	everything[questplan.WtObjCoins] = 200
	invs := []map[int]int{
		inv(), everything,
		inv(questplan.WtObjCrystal4, questplan.WtObjMap, questplan.WtObjLitCandle),
		inv(questplan.WtObjCrystal3, questplan.WtObjCrystal4),
		inv(questplan.WtObjTobanKey, questplan.WtObjDragonBones, questplan.WtObjRope),
		inv(questplan.WtObjGuamLeaf, questplan.WtObjBatBones, questplan.WtObjRope),
		inv(questplan.WtObjGuamLeaf, questplan.WtObjBatBones, questplan.WtObjJangerberries, questplan.WtObjVialWater, questplan.WtObjPestle),
		inv(questplan.WtObjPickaxe),
		inv(questplan.WtObjCrystal1, questplan.WtObjCrystal2, questplan.WtObjCrystal3, questplan.WtObjCrystal4),
		inv(questplan.WtObjPart1), inv(questplan.WtObjPart2), inv(questplan.WtObjPart3),
	}
	// What the engine has seen fail this run: the lost-piece remedies are asked in turn, each once.
	flagSets2 := []map[string]bool{
		{}, {questplan.CustomWtRelicBack: true}, {questplan.CustomWtRelicBack: true, questplan.CustomWtAskOg: true},
		{questplan.CustomWtRelicBack: true, questplan.CustomWtAskOg: true, questplan.CustomWtAskToban: true},
	}
	allTribes := questplan.WtOgSpoken | questplan.WtOgHelped | questplan.WtGrewSpoken | questplan.WtGrewHelped | questplan.WtTobanSpoken | questplan.WtTobanHelped
	flagSets := []uint32{
		0, questplan.WtOgSpoken, questplan.WtOgSpoken | questplan.WtGrewSpoken | questplan.WtTobanHelped,
		questplan.WtOgSpoken | questplan.WtOgHelped | questplan.WtGrewSpoken | questplan.WtTobanSpoken | questplan.WtTobanHelped,
		allTribes, questplan.WtMarketPaid,
		questplan.WtLearning | questplan.WtLearnedNod | questplan.WtLearnedIg | questplan.WtLearnedAr | questplan.WtLearnedCur,
	}
	spots := []tile.Tile{questplan.WtBank, questplan.WtWizardAnchor, questplan.WtGrewAnchor, questplan.WtCampArrival, questplan.WtJumpBackStand, questplan.WtEnclaveArrive,
		questplan.WtMirrorLanding, questplan.WtStallStand, {X: 2510, Z: 3012}, questplan.WtCityGuardAnchor}
	for stage := 0; stage <= 14; stage++ {
		for _, flags := range flagSets {
			for _, left := range []int{-1, 0, 4} {
				for _, pos := range spots {
					for _, held := range invs {
						for _, journal := range []questplan.Status{questplan.StatusNotStarted, questplan.StatusInProgress, questplan.StatusComplete} {
							for _, failed := range flagSets2 {
								snap := questplan.Snapshot{Journal: journal, Stage: questplan.WtState{Stage: stage, Flags: flags, Left: left}.Pack(), StageKnown: true,
									Tile: pos, TileKnown: true, InvIDs: held, WornIDs: map[int]int{questplan.WtObjAntiDragonShield: 1}, Inv: map[string]int{}, BankIDs: map[int]int{},
									BankKnown: true, FreeSlots: 14, HP: 99, MaxHP: 99, Failed: failed, Ran: map[string]bool{}, Blocked: map[string]bool{},
									Levels: map[string]int{"attack": 99, "strength": 99, "defence": 99, "hitpoints": 99, "magic": 70, "agility": 40, "thieving": 77, "herblore": 50, "mining": 60}}
								if st := q.Module.Decide(snap); st.Kind == questplan.StepCustom {
									asked[st.Custom] = true
									if q.Customs[st.Custom] == nil {
										t.Errorf("stage %d asks for %q, which has no executor", stage, st.Custom)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	for name := range q.Customs {
		if !asked[name] {
			t.Errorf("executor %q is never asked for by the decision", name)
		}
	}
	if q.Module.Record.ID != questplan.WatchtowerID || len(q.Module.ToolIDs) != len(questplan.WtToolIDs()) {
		t.Error("the module is not the plan's")
	}
}

// The fleet's typical account (Attack 52, Strength 43, Defence 50, 53 hitpoints) is refused at Og, the first ogre leg, by the count of ogres round his stand and the
// twenty ticks of his talk in which nothing is eaten, before it walks anywhere near them; the bones were asked for first.
func TestWatchtowerParksTheCanaryAtOg(t *testing.T) {
	r := newWtRigAs(t, 52, 43, 50, 53)
	res := r.run(Watchtower(), r.apply)
	reason := strings.ToLower(res.Reason)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(reason, "og's stand") || !strings.Contains(reason, "4 ogres") {
		t.Fatalf("the run ended %s (%s)", res.Outcome, res.Reason)
	}
	for _, s := range []string{"talk to Og", "wt-island-in", "wt-camp-in"} {
		if r.ranStep(s) {
			t.Errorf("the canary went into ogre country: %s", s)
		}
	}
}

// The dragons' nest is priced before the nightshade is fetched: an account that has done everything up to the enclave and whose bites cannot stay above the fire
// (60 hitpoints: it eats at 45, the fire reaches 50) is parked at stage 7 with the dragons in the reason, and no walk towards the guard is made.
func TestWatchtowerParksBeforeTheNightshadeWhenTheFireWouldFinishIt(t *testing.T) {
	r := newWtRigAs(t, 99, 99, 99, 60)
	r.snapHook = nil
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "blue dragons") {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	if r.ranStep("wt-nightshade") || r.ranStep("wt-enclave-in") {
		t.Errorf("the nightshade was fetched or the guard fed for an account the nest would kill\nsteps:\n  %s", strings.Join(r.described(), "\n  "))
	}
	if r.st.Stage != questplan.WtSkavidCrystal {
		t.Errorf("the quest stands at stage %d, want 7 (the caves done, the enclave not entered)", r.st.Stage)
	}
}

// A maxed account without the shield can walk out of the nest (the hop of stage 7 is priced as that walk and passes), is let in once, and is parked at the six
// shamans: the whole tour is not a price it can pay, and the reason says what it is.
func TestWatchtowerParksAtTheShamansWithoutAShield(t *testing.T) {
	r := newWtRig(t)
	r.snapHook = nil
	res := r.run(Watchtower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "six shamans") {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	if !r.ranStep("wt-enclave-in") || r.ranStep("wt-dissolve") {
		t.Errorf("the account should have been let into the enclave once (stage 7) and not asked for the shamans\nsteps:\n  %s", strings.Join(r.described(), "\n  "))
	}
	if r.st.Stage != questplan.WtMadePotion || r.held(questplan.WtObjMagicPotion) == 0 {
		t.Errorf("stage %d: the potion is infused and unused", r.st.Stage)
	}
}

// A relic piece lost after its tribe was helped is asked of its giver, one at a time and once each; a spare the wizard refuses ("I already have that part") is kept
// and never given again, and the run goes on to Complete. Toban's piece is the one lost here, as it was lost at Gorad in the first order of the camp.
func TestWatchtowerAsksTheGiversForALostPiece(t *testing.T) {
	r := newWtRig(t)
	dropped := false
	apply := func(step questplan.Step) bool {
		ok := r.apply(step)
		if step.Kind == questplan.StepTalk && step.Stop.Npc == "Toban" && !dropped && r.held(questplan.WtObjPart3) > 0 {
			dropped = true
			r.take(questplan.WtObjPart3, 1)
		}
		return ok
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	if !dropped || r.asked[questplan.CustomWtAskToban] != 1 || r.asked[questplan.CustomWtAskOg] != 1 || r.asked["wizard"] != 1 || r.asked[questplan.CustomWtAskGrew] != 0 {
		t.Errorf("the asks %v: the wizard, then Og (the spare he gives is refused), then Toban (whose piece completes the relic), never Grew", r.asked)
	}
	if r.held(questplan.WtObjPart1) != 1 {
		t.Errorf("the refused spare of Og's piece is kept: %d held", r.held(questplan.WtObjPart1))
	}
	gives := 0
	for _, st := range r.steps {
		if st.Kind == questplan.StepCustom && st.Custom == questplan.WtGivePartName(questplan.WtObjPart1) {
			gives++
		}
	}
	if gives != 2 {
		t.Errorf("Og's piece was given %d times, want 2 (once for the quest, once as the refused spare)", gives)
	}
}

// When every remedy has been tried and the relic is still short, the quest parks for a day with the reason, and nothing is asked a second time on the way.
func TestWatchtowerParksADayWhenAPieceCannotBeReplaced(t *testing.T) {
	r := newWtRig(t)
	dropped := false
	apply := func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && (step.Custom == questplan.CustomWtAskOg || step.Custom == questplan.CustomWtAskGrew || step.Custom == questplan.CustomWtAskToban) {
			r.asked[step.Custom]++
			return false // the giver's menu gave nothing
		}
		ok := r.apply(step)
		if step.Kind == questplan.StepTalk && step.Stop.Npc == "Toban" && !dropped && r.held(questplan.WtObjPart3) > 0 {
			dropped = true
			r.take(questplan.WtObjPart3, 1)
		}
		return ok
	}
	res := r.run(Watchtower(), apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(strings.ToLower(res.Reason), "relic") {
		t.Fatalf("the run ended %s (%s)\nsteps:\n  %s", res.Outcome, res.Reason, strings.Join(r.described(), "\n  "))
	}
	for name, n := range r.asked {
		if n != 1 {
			t.Errorf("%s was asked %d times, want once", name, n)
		}
	}
	if len(r.asked) != 4 {
		t.Errorf("asked %v: the wizard and the three givers, each once", r.asked)
	}
	if rec := r.e.Store.Record(questplan.WatchtowerID); time.Duration(rec.BackoffUntil-rec.UpdatedAt)*time.Millisecond < 23*time.Hour {
		t.Errorf("record %+v: parked for a day", rec)
	}
}
