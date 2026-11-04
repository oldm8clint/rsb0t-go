package questmods

// The Digsite Quest through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (obsslug_engine_test.go):
// the module's own Decide runs against snapshots built from a fake pack, the account's tile and levels and the quest tab's colour, and every
// step it asks for is performed by a stub that changes that world the way the content does (itexam_*.rs2, area_digsite.rs2 and the NPC
// scripts). What is proven is the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death
// caps, the completion record): every stage reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that
// cannot be done parks the quest with its reason. What is not proven here is each custom's own live execution (the winch, the pocket, the dig,
// the blast): those are the real-data tests and the live logs.

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

// Skill indices of the protocol's stat table the Digsite reads.
const (
	dgHitpoints = 3
	dgHerblore  = 15
	dgAgility   = 16
	dgThieving  = 17
)

// dgRig is the Digsite Quest's state on the rig. The stage and the errands are %itexamlevel and %itexam_errands; the bits are the ones of
// %itexam_bits the plan cannot see (the tea given to the panning guide, the ropes on the two winches, the barrel's lid, the talisman shown).
type dgRig struct {
	*csRig
	stage      int
	errand     [4]int // by the script's own numbering: 1 the green student, 2 the purple one, 3 the orange one
	tea        bool
	ropeEast   bool
	ropeWest   bool
	barrelOpen bool
	shown      bool
	counts     map[string]int
	// talismanAfter is how many digs the talisman takes (the stub's dice); 0 gives it at once.
	talismanFails int
	// pocketFood is how many sharks the pocket food kit finds in the bank (the rig's default is none).
	pocketFood int
}

func newDgRig(t *testing.T, stage int) *dgRig {
	r := &dgRig{csRig: newCSRig(t, questplan.DigExaminerAnchor), counts: map[string]int{}}
	r.stageKnown = true
	r.setStage(stage)
	osFighter(r.csRig, 60, 70)
	osSeed(r.csRig, dgThieving, 40)
	osSeed(r.csRig, dgAgility, 30)
	osSeed(r.csRig, dgHerblore, 25)
	return r
}

// setStage moves %itexamlevel and the tab, and recomputes what the journal says (flags and all).
func (r *dgRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.DigNotStarted:
		status = quests.NotStarted
	case questplan.DigComplete:
		status = quests.Complete
	}
	r.setTab(questplan.DigsiteName, status)
	r.sync()
}

// raw is what the journal's page reads as (questplan.ParseDigsiteJournal over itexam_journal.rs2, as the questplan tests prove): the stage, the
// first exam's answered errands, the ready line, the third exam's opal.
func (r *dgRig) raw() int {
	flags := 0
	ready := r.errand[1] == 2 && r.errand[2] == 2 && r.errand[3] == 2
	if r.stage == questplan.DigFirstExam {
		if r.errand[1] == 2 {
			flags |= questplan.DigFlagGreen
		}
		if r.errand[2] == 2 {
			flags |= questplan.DigFlagPurple
		}
		if r.errand[3] == 2 {
			flags |= questplan.DigFlagOrange
		}
	}
	if r.stage >= questplan.DigFirstExam && r.stage <= questplan.DigThirdExam && ready {
		flags |= questplan.DigFlagReady
	}
	if r.stage == questplan.DigThirdExam && r.errand[2] == 1 {
		flags |= questplan.DigFlagOpal
	}
	return questplan.DigRaw(r.stage, flags)
}

func (r *dgRig) sync() { r.csRig.stage = r.raw() }

func (r *dgRig) at(t tile.Tile) { r.sim.StandAt(t.X, t.Z) }

func (r *dgRig) region() questplan.DigRegion {
	return questplan.DigRegionOf(questrunner.CurrentTile(r.e.D))
}

func (r *dgRig) setErrand(n, v int) { r.errand[n] = v; r.sync() }

func (r *dgRig) resetErrands() { r.errand = [4]int{}; r.sync() }

func (r *dgRig) ensure(id int) {
	if r.held(id) == 0 {
		r.give(id, 1)
	}
}

// apply plays the content's side of the quest: the NPCs' hand-overs and the customs' effects.
func (r *dgRig) apply(step questplan.Step) bool {
	defer r.sync()
	switch step.Kind {
	case questplan.StepProvision:
		return r.provision(step)
	case questplan.StepScanBank, questplan.StepDeposit, questplan.StepWithdraw:
		return true
	case questplan.StepTalk:
		return r.talk(step)
	}
	r.counts[step.Custom]++
	switch step.Custom {
	case questplan.CustomDigPickpocket:
		return r.pickpocket(step)
	case questplan.CustomDigBush:
		if r.stage == questplan.DigFirstExam {
			r.ensure(questplan.DigObjSamplePurple)
		}
	case questplan.CustomDigSacks:
		r.ensure(questplan.DigObjSpecimenJar)
	case questplan.CustomDigTray:
		r.ensure(questplan.DigObjTrayEmpty)
	case questplan.CustomDigPan:
		if !r.tea {
			if r.held(questplan.DigObjTea) == 0 {
				return false // "Hey! You can't pan yet!": he wants a cup of tea
			}
			r.take(questplan.DigObjTea, 1)
			r.tea = true
		}
		r.give(step.ItemID, 1)
	case questplan.CustomDigGiveSample:
		r.take(step.ItemID, 1)
		switch step.TargetID {
		case questplan.DigNpcGreen:
			r.setErrand(1, 2)
		case questplan.DigNpcPurple:
			r.setErrand(2, 2)
		case questplan.DigNpcOrange:
			r.setErrand(3, 2)
		}
	case questplan.CustomDigStudentGreen, questplan.CustomDigStudentGreen3:
		r.setErrand(1, 2)
	case questplan.CustomDigStudentOrange, questplan.CustomDigStudentOrange3:
		r.setErrand(3, 2)
	case questplan.CustomDigStudentPurple, questplan.CustomDigStudentPurple3:
		switch {
		case r.stage == questplan.DigThirdExam && r.errand[2] == 0:
			r.setErrand(2, 1)
		case r.stage == questplan.DigThirdExam && r.held(questplan.DigObjOpal) > 0:
			r.take(questplan.DigObjOpal, 1)
			r.setErrand(2, 2)
		case r.stage == questplan.DigThirdExam:
		default:
			r.setErrand(2, 2)
		}
	case questplan.CustomDigCutOpal:
		r.take(questplan.DigObjUncutOpal, 1)
		r.give(questplan.DigObjOpal, 1)
	case questplan.CustomDigTalisman:
		if r.talismanFails > 0 {
			r.talismanFails--
			return r.talismanFails > 0 // the long dig that finds nothing reports failure
		}
		if !r.shown {
			r.give(questplan.DigObjTalisman, 1)
		}
	case questplan.CustomDigCharcoal:
		r.ensure(questplan.DigObjCharcoal)
	case questplan.CustomDigUseOnExpert:
		return r.useOnExpert(step.ItemID)
	case questplan.CustomDigLostLetter:
		if r.shown && r.held(questplan.DigObjExpertScroll) == 0 {
			r.give(questplan.DigObjExpertScroll, 1)
		}
	case questplan.CustomDigGiveScroll:
		r.take(questplan.DigObjExpertScroll, 1)
		r.setStage(questplan.DigMineshaftPermit)
	case questplan.CustomDigWest:
		return r.west()
	case questplan.CustomDigChest:
		r.take(questplan.DigObjChestKey, 1)
		r.give(questplan.DigObjPowder, 1)
	case questplan.CustomDigBarrel:
		r.barrelOpen = true
		r.take(questplan.DigObjVial, 1)
		r.give(questplan.DigObjLiquid, 1)
	case questplan.CustomDigMix:
		return r.mix(step.ItemID, step.TargetID)
	case questplan.CustomDigBlast:
		return r.blast()
	case questplan.CustomDigTablet:
		r.give(questplan.DigObjStoneTablet, 1)
		r.at(questplan.DigMainWinch)
	case questplan.CustomDigLeave:
		r.at(questplan.DigStudentsAnchor)
	default:
		r.t.Errorf("no answer for the step %q (%s)", step.Custom, step.Describe())
		return false
	}
	return true
}

func (r *dgRig) provision(step questplan.Step) bool {
	switch {
	case strings.HasPrefix(step.KitName, questplan.KitDigBundle):
		return true
	case step.KitName == questplan.KitDigChem || step.KitName == questplan.KitDigBox || step.KitName == questplan.KitDigChisel || step.KitName == questplan.KitDigTea:
		for _, l := range step.Kit {
			if r.held(l.ID) < l.Qty {
				r.give(l.ID, l.Qty-r.held(l.ID))
			}
		}
		return true
	case step.KitName == questplan.KitDigFood:
		r.give(questplan.ObjShark, 8)
		return true
	case step.KitName == questplan.KitDigPocketFood:
		if r.pocketFood > 0 {
			r.give(questplan.ObjShark, r.pocketFood)
		}
		return true
	}
	r.t.Errorf("unexpected kit %q", step.KitName)
	return false
}

func (r *dgRig) talk(step questplan.Step) bool {
	switch step.Stop.ID {
	case questplan.DigNpcCurator:
		if r.held(questplan.DigObjPlainLetter) > 0 {
			r.take(questplan.DigObjPlainLetter, 1)
			r.give(questplan.DigObjStampedLetter, 1)
		}
	case questplan.DigNpcExaminer:
		p := step.Stop.Prefer[0]
		switch {
		case p == "Can I take an exam?":
			r.give(questplan.DigObjPlainLetter, 1)
			r.setStage(questplan.DigStamping)
		case p == "I have lost the letter you gave me.":
			if r.held(questplan.DigObjPlainLetter) == 0 {
				r.give(questplan.DigObjPlainLetter, 1)
			}
		case p == "I have lost my trowel.":
			if r.held(questplan.DigObjTrowel) == 0 {
				r.give(questplan.DigObjTrowel, 1)
			}
		case r.stage == questplan.DigStamping && r.held(questplan.DigObjStampedLetter) > 0:
			r.take(questplan.DigObjStampedLetter, 1)
			r.setStage(questplan.DigFirstExam) // and the first exam goes badly: the errands are open
		case r.stage >= questplan.DigFirstExam && r.stage <= questplan.DigThirdExam:
			if r.errand[1] == 2 && r.errand[2] == 2 && r.errand[3] == 2 {
				// A pass gives a certificate (and the first a trowel): the pack changes, which the engine's watchdog reads as progress (on the
				// live engine the journal's stage is in its signature too, which the rig's injected stage is not).
				r.resetErrands()
				if r.stage == questplan.DigFirstExam {
					r.give(questplan.DigObjTrowel, 1)
				}
				r.give(questplan.DigObjCert1+r.stage-questplan.DigFirstExam, 1)
				r.setStage(r.stage + 1)
			}
		}
	default:
		r.t.Errorf("unexpected talk %s", step.Describe())
		return false
	}
	return true
}

func (r *dgRig) pickpocket(step questplan.Step) bool {
	if t := r.skill("thieving"); t > 0 && t < questplan.DigThieving {
		r.e.RequestPark("the workmen's pockets need Thieving 25", questplan.DigSkillParkFor)
		return false
	}
	qty := step.Qty
	if qty < 1 {
		qty = 1
	}
	r.give(step.ItemID, qty-r.held(step.ItemID))
	return true
}

// skill is a base level the engine reads off the simulated stat table.
func (r *dgRig) skill(name string) int { return questrunner.BaseLevels(r.e.D)[name] }

func (r *dgRig) useOnExpert(item int) bool {
	switch item {
	case questplan.DigObjTalisman:
		r.take(questplan.DigObjTalisman, 1)
		r.give(questplan.DigObjExpertScroll, 1)
		r.shown = true
	case questplan.DigObjPowder:
		r.take(questplan.DigObjPowder, 1)
		r.give(questplan.DigObjNitrate, 1)
	case questplan.DigObjLiquid:
		r.take(questplan.DigObjLiquid, 1)
		r.give(questplan.DigObjNitro, 1)
	case questplan.DigObjStoneTablet:
		r.take(questplan.DigObjStoneTablet, 1)
		r.setStage(questplan.DigComplete)
	default:
		r.t.Errorf("unexpected item %d shown to the expert", item)
		return false
	}
	return true
}

// west is the private winch's trip: a rope on the winch the first time (pickpocketed when the pack has none), the key and the root.
func (r *dgRig) west() bool {
	if r.region() != questplan.DigShaftWest {
		if !r.ropeWest {
			r.ensure(questplan.DigObjRope)
			r.take(questplan.DigObjRope, 1)
			r.ropeWest = true
		}
		r.at(questplan.DigWestLanding)
	}
	if r.held(questplan.DigObjChestKey) == 0 && r.held(questplan.DigObjPowder) == 0 && r.held(questplan.DigObjNitrate) == 0 {
		r.give(questplan.DigObjChestKey, 1)
	}
	if r.held(questplan.DigObjArceniaRoot) == 0 && r.held(questplan.DigObjCompound) == 0 {
		r.give(questplan.DigObjArceniaRoot, 1)
	}
	r.at(questplan.DigPrivateWinch)
	return true
}

func (r *dgRig) mix(a, b int) bool {
	recipes := []struct{ a, b, product, herblore int }{
		{questplan.DigObjCharcoal, questplan.DigObjPestle, questplan.DigObjGroundCharcoal, 0},
		{questplan.DigObjNitrate, questplan.DigObjNitro, questplan.DigObjPreMix, 0},
		{questplan.DigObjPreMix, questplan.DigObjGroundCharcoal, questplan.DigObjPostMix, 10},
		{questplan.DigObjPostMix, questplan.DigObjArceniaRoot, questplan.DigObjCompound, 10},
	}
	for _, rc := range recipes {
		if (rc.a == a && rc.b == b) || (rc.a == b && rc.b == a) {
			if rc.herblore > r.skill("herblore") {
				r.t.Errorf("mixed %d with %d at Herblore %d", a, b, r.skill("herblore"))
				return false
			}
			// The pestle is a tool; every other ingredient is used up.
			for _, in := range []int{rc.a, rc.b} {
				if in != questplan.DigObjPestle {
					r.take(in, 1)
				}
			}
			r.give(rc.product, 1)
			return true
		}
	}
	r.t.Errorf("no recipe for %d and %d", a, b)
	return false
}

// blast is the pour and the light: the main winch's rope the first time, the compound, the tinderbox, and the run to the altar cave.
func (r *dgRig) blast() bool {
	if !r.ropeEast {
		r.ensure(questplan.DigObjRope)
		r.take(questplan.DigObjRope, 1)
		r.ropeEast = true
	}
	if r.held(questplan.DigObjCompound) > 0 {
		r.take(questplan.DigObjCompound, 1)
		r.setStage(questplan.DigPouredCompound)
	}
	if r.stage != questplan.DigPouredCompound || r.held(questplan.DigObjTinderbox) == 0 {
		r.t.Errorf("the blast at stage %d with %d tinderboxes", r.stage, r.held(questplan.DigObjTinderbox))
		return false
	}
	r.setStage(questplan.DigRemovedBlockage)
	r.at(questplan.DigAltarLanding)
	return true
}

// everything the pack may hold that is dangerous or used up when the quest is over.
var dgGone = []int{questplan.DigObjLiquid, questplan.DigObjNitro, questplan.DigObjPreMix, questplan.DigObjPostMix, questplan.DigObjCompound, questplan.DigObjStoneTablet,
	questplan.DigObjPowder, questplan.DigObjNitrate, questplan.DigObjTalisman, questplan.DigObjExpertScroll}

// The whole quest in the content's order, every executor a stub: the letter stamped, three exams with the errands of each, the talisman, the
// shafts, the chemicals, the blast and the tablet.
func TestTheDigsiteReachesCompleteThroughTheEngine(t *testing.T) {
	r := newDgRig(t, questplan.DigNotStarted)
	res := r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	r.wantSteps(
		// The start, and the letter: the Curator stamps it, and the Examiner begins the first exam on the spot.
		"talk to Examiner", "provision dig-bundle-1", "talk to Curator", "talk to Examiner",
		// The first exam: the three samples collected (a pocket, the bush, the river with the panning guide's tea), then handed over in one trip.
		questplan.CustomDigPickpocket, questplan.CustomDigBush, questplan.CustomDigTray, questplan.CustomDigPan, "provision dig-tea", questplan.CustomDigPan,
		questplan.CustomDigGiveSample, questplan.CustomDigGiveSample, questplan.CustomDigGiveSample, "talk to Examiner",
		// The second exam: every student once.
		"provision dig-bundle-3", questplan.CustomDigStudentGreen, questplan.CustomDigStudentOrange, questplan.CustomDigStudentPurple, "talk to Examiner",
		// The third: the purple student's opal in between her two talks.
		"provision dig-bundle-4", questplan.CustomDigStudentGreen3, questplan.CustomDigStudentOrange3, questplan.CustomDigStudentPurple3,
		questplan.CustomDigPan, "provision dig-chisel", questplan.CustomDigCutOpal, questplan.CustomDigStudentPurple3, "talk to Examiner",
		// The find for the expert, and his letter for a workman.
		"provision dig-bundle-5", questplan.CustomDigSacks, questplan.CustomDigPickpocket, questplan.CustomDigTalisman, questplan.CustomDigUseOnExpert,
		questplan.CustomDigGiveScroll,
		// The chemistry: the shopping, the west shaft, the charcoal dug and ground before any explosive item exists, the chest, the barrel, the mixes.
		"provision dig-bundle-6", "provision dig-chem", questplan.CustomDigWest, questplan.CustomDigCharcoal, questplan.CustomDigMix,
		questplan.CustomDigChest, questplan.CustomDigUseOnExpert, questplan.CustomDigBarrel, questplan.CustomDigUseOnExpert,
		questplan.CustomDigMix, questplan.CustomDigMix, questplan.CustomDigMix,
		// The blast, the tablet, the expert.
		questplan.CustomDigBlast, questplan.CustomDigTablet, questplan.CustomDigUseOnExpert)
	if got := r.e.Store.Record(questplan.DigsiteID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range dgGone {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the quest", id)
		}
	}
	if r.counts[questplan.CustomDigBlast] != 1 || r.counts[questplan.CustomDigTablet] != 1 || r.counts[questplan.CustomDigWest] != 1 {
		t.Errorf("customs ran %v", r.counts)
	}
}

// Every stage resumes from its own page of the journal with whatever the pack holds and finishes: a run restarted at any point of the quest
// re-derives where it is.
func TestTheDigsiteResumesFromEveryStage(t *testing.T) {
	type start struct {
		name  string
		stage int
		setup func(r *dgRig)
	}
	for _, s := range []start{
		{"stage 1 with the plain letter", questplan.DigStamping, func(r *dgRig) { r.give(questplan.DigObjPlainLetter, 1) }},
		{"stage 1 with the stamped one", questplan.DigStamping, func(r *dgRig) { r.give(questplan.DigObjStampedLetter, 1) }},
		{"stage 1 with none", questplan.DigStamping, func(r *dgRig) {}},
		{"stage 2", questplan.DigFirstExam, func(r *dgRig) {}},
		{"stage 2 with the green student answered", questplan.DigFirstExam, func(r *dgRig) { r.setErrand(1, 2) }},
		{"stage 2 with the samples in the pack", questplan.DigFirstExam, func(r *dgRig) {
			r.give(questplan.DigObjSampleGreen, 1)
			r.give(questplan.DigObjSamplePurple, 1)
			r.give(questplan.DigObjSampleOrange, 1)
		}},
		{"stage 2 ready", questplan.DigFirstExam, func(r *dgRig) { r.errand = [4]int{0, 2, 2, 2}; r.sync() }},
		{"stage 3", questplan.DigSecondExam, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1) }},
		{"stage 4", questplan.DigThirdExam, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1) }},
		{"stage 4 with an opal", questplan.DigThirdExam, func(r *dgRig) { r.give(questplan.DigObjOpal, 1) }},
		{"stage 4, the opal asked for", questplan.DigThirdExam, func(r *dgRig) { r.setErrand(2, 1) }},
		{"stage 5", questplan.DigImpressExpert, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1) }},
		{"stage 5 with the talisman", questplan.DigImpressExpert, func(r *dgRig) { r.give(questplan.DigObjTalisman, 1) }},
		{"stage 5 with the letter", questplan.DigImpressExpert, func(r *dgRig) { r.give(questplan.DigObjExpertScroll, 1) }},
		{"stage 6", questplan.DigMineshaftPermit, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1) }},
		{"stage 6 with the ropes tied", questplan.DigMineshaftPermit, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1); r.ropeEast, r.ropeWest = true, true }},
		{"stage 6 with the powder", questplan.DigMineshaftPermit, func(r *dgRig) {
			r.give(questplan.DigObjTrowel, 1)
			r.give(questplan.DigObjPowder, 1)
			r.give(questplan.DigObjArceniaRoot, 1)
		}},
		{"stage 6 with the premix", questplan.DigMineshaftPermit, func(r *dgRig) {
			r.give(questplan.DigObjTrowel, 1)
			r.give(questplan.DigObjPreMix, 1)
			r.give(questplan.DigObjGroundCharcoal, 1)
			r.give(questplan.DigObjArceniaRoot, 1)
			r.give(questplan.DigObjTinderbox, 1)
		}},
		{"stage 6 with the compound", questplan.DigMineshaftPermit, func(r *dgRig) { r.give(questplan.DigObjCompound, 1); r.give(questplan.DigObjTinderbox, 1) }},
		{"stage 7", questplan.DigPouredCompound, func(r *dgRig) { r.give(questplan.DigObjTinderbox, 1) }},
		{"stage 7 in the east shaft", questplan.DigPouredCompound, func(r *dgRig) { r.give(questplan.DigObjTinderbox, 1); r.at(questplan.DigEastLanding) }},
		{"stage 8", questplan.DigRemovedBlockage, func(r *dgRig) {}},
		{"stage 8 in the altar cave", questplan.DigRemovedBlockage, func(r *dgRig) { r.at(questplan.DigAltarLanding) }},
		{"stage 8 with the tablet", questplan.DigRemovedBlockage, func(r *dgRig) { r.give(questplan.DigObjStoneTablet, 1) }},
	} {
		t.Run(s.name, func(t *testing.T) {
			r := newDgRig(t, s.stage)
			s.setup(r)
			r.sync()
			res := r.run(Digsite(), r.apply)
			if res.Outcome != questrunner.OutcomeComplete {
				t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
			}
			for _, id := range dgGone {
				if r.held(id) != 0 {
					t.Errorf("item %d still held after the quest", id)
				}
			}
		})
	}
}

// A shaft the account is found in is left, unless the next step is the shaft's own.
func TestTheDigsiteLeavesAShaftItIsFoundIn(t *testing.T) {
	r := newDgRig(t, questplan.DigFirstExam)
	r.at(questplan.DigWestLanding)
	res := r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if first := r.described()[0]; !strings.HasPrefix(first, questplan.CustomDigLeave) {
		t.Errorf("the first step is %q, want the way out of the shaft", first)
	}
}

// The levels the content enforces park the quest at the step that needs them, with the reason, for hours and not for the escalating backoff.
func TestTheDigsiteParksBelowItsLevels(t *testing.T) {
	for _, c := range []struct {
		name   string
		stage  int
		stat   int
		level  int
		setup  func(r *dgRig)
		reason string
	}{
		{"Thieving 24 at the green sample", questplan.DigFirstExam, dgThieving, 24, func(r *dgRig) {}, "Thieving 25"},
		{"Thieving 24 at the brush", questplan.DigImpressExpert, dgThieving, 24, func(r *dgRig) {
			r.give(questplan.DigObjTrowel, 1)
			r.give(questplan.DigObjSpecimenJar, 1)
		}, "Thieving 25"},
		{"Herblore 9 before the chemicals", questplan.DigMineshaftPermit, dgHerblore, 9, func(r *dgRig) { r.give(questplan.DigObjTrowel, 1) }, "Herblore 10"},
		{"Agility 9 at the west winch", questplan.DigMineshaftPermit, dgAgility, 9, func(r *dgRig) {
			r.give(questplan.DigObjTrowel, 1)
			r.give(questplan.DigObjVial, 1)
			r.give(questplan.DigObjPestle, 1)
			r.give(questplan.DigObjTinderbox, 1)
		}, "Agility 10"},
		{"Agility 9 at the tablet", questplan.DigRemovedBlockage, dgAgility, 9, func(r *dgRig) {}, "Agility 10"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newDgRig(t, c.stage)
			osSeed(r.csRig, c.stat, c.level)
			c.setup(r)
			r.sync()
			res := r.run(Digsite(), r.apply)
			if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, c.reason) {
				t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
			}
			if parked, _ := r.e.Store.Parked(questplan.DigsiteID, time.Now().Add(5*time.Hour)); !parked {
				t.Error("not parked for the hours")
			}
			if parked, _ := r.e.Store.Parked(questplan.DigsiteID, time.Now().Add(7*time.Hour)); parked {
				t.Error("parked past the six hours")
			}
		})
	}
}

// An account the skeletons hunt, with the pack to match, is refused the cave (after one food kit, which the bank does not fill), and the same
// account with food goes.
func TestTheDigsiteParksAWeakAccountAtTheAltarCave(t *testing.T) {
	r := newDgRig(t, questplan.DigRemovedBlockage)
	osFighter(r.csRig, 20, 25)
	osSeed(r.csRig, dgHitpoints, 25)
	food := 0
	res := r.run(Digsite(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitDigFood {
			food++
			return true // the bank and the mule had none
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "altar cave") || food != 1 || r.counts[questplan.CustomDigTablet] != 0 {
		t.Fatalf("the run ended %s (%s) after %d food kits and %d tablet trips: %v", res.Outcome, res.Reason, food, r.counts[questplan.CustomDigTablet], r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.DigsiteID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the hours")
	}

	r = newDgRig(t, questplan.DigRemovedBlockage)
	osFighter(r.csRig, 20, 25)
	osSeed(r.csRig, dgHitpoints, 25)
	r.give(questplan.ObjShark, 8)
	res = r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("with eight sharks the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// A weak account is also refused the pour: the blast ends in the cave, so the price is asked before the compound goes on the bricks.
func TestTheDigsiteParksAWeakAccountAtThePour(t *testing.T) {
	r := newDgRig(t, questplan.DigMineshaftPermit)
	osFighter(r.csRig, 20, 25)
	osSeed(r.csRig, dgHitpoints, 25)
	r.give(questplan.DigObjCompound, 1)
	r.give(questplan.DigObjTinderbox, 1)
	res := r.run(Digsite(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitDigFood {
			return true
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || r.counts[questplan.CustomDigBlast] != 0 || r.held(questplan.DigObjCompound) != 1 {
		t.Fatalf("the run ended %s (%s) after %d blasts: %v", res.Outcome, res.Reason, r.counts[questplan.CustomDigBlast], r.log)
	}
}

// The panning guide's gate: a pan that he forbids with no tea in the pack sends the account for a cup of tea once (the third exam's pan, in the
// same run, finds the guide satisfied and buys nothing), and an account whose guide has had his tea buys none at all.
func TestTheDigsiteBuysTheTeaThePanningGuideAsksFor(t *testing.T) {
	teaKits := func(r *dgRig) int {
		n := 0
		for _, st := range r.steps {
			if st.Kind == questplan.StepProvision && st.KitName == questplan.KitDigTea {
				n++
			}
		}
		return n
	}
	r := newDgRig(t, questplan.DigFirstExam)
	r.setErrand(1, 2)
	r.setErrand(2, 2)
	r.give(questplan.DigObjTrayEmpty, 1)
	res := r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	// The refused pan, the cup of tea, the pan that went through; then the third exam's pan for the opal (the guide is content by then).
	d := r.described()
	var pans []int
	for i, step := range d {
		if strings.HasPrefix(step, questplan.CustomDigPan) {
			pans = append(pans, i)
		}
	}
	if len(pans) != 3 || !strings.HasPrefix(d[pans[0]+1], "provision "+questplan.KitDigTea) || !strings.HasPrefix(d[pans[1]-1], "provision "+questplan.KitDigTea) {
		t.Errorf("the pans and the tea came as %v (pans at %v), want a pan, the tea, a pan, and the opal's pan later", d, pans)
	}
	if teaKits(r) != 1 || r.counts[questplan.CustomDigPan] != 3 || !r.tea || r.held(questplan.DigObjTea) != 0 {
		t.Errorf("%d tea kits, %d pans, given %v, held %d", teaKits(r), r.counts[questplan.CustomDigPan], r.tea, r.held(questplan.DigObjTea))
	}
	// The same account after the guide has had his tea pans at once and buys nothing.
	r = newDgRig(t, questplan.DigFirstExam)
	r.tea = true
	r.setErrand(1, 2)
	r.setErrand(2, 2)
	r.give(questplan.DigObjTrayEmpty, 1)
	res = r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.counts[questplan.CustomDigPan] != 2 || teaKits(r) != 0 {
		t.Fatalf("the run ended %s (%s) after %d pans and %d tea kits: %v", res.Outcome, res.Reason, r.counts[questplan.CustomDigPan], teaKits(r), r.log)
	}
}

// A level 3 dig that finds nothing for a long while (the talisman is found once ever, and the account may have shown it already) asks the
// expert for his letter again, once, and the quest goes on.
func TestTheDigsiteAsksTheExpertForALostLetter(t *testing.T) {
	r := newDgRig(t, questplan.DigImpressExpert)
	r.shown = true
	r.talismanFails = 1 // the dig reports its long failure once
	r.give(questplan.DigObjTrowel, 1)
	r.give(questplan.DigObjSpecimenJar, 1)
	r.give(questplan.DigObjSpecimenBrush, 1)
	res := r.run(Digsite(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
	}
	want := []string{questplan.CustomDigTalisman, questplan.CustomDigLostLetter, questplan.CustomDigGiveScroll}
	var got []string
	for _, d := range r.described() {
		for _, w := range want {
			if strings.HasPrefix(d, w) {
				got = append(got, w)
			}
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("the dig, the letter and the workman came in the order %v, want %v (all %v)", got, want, r.described())
	}
	if r.counts[questplan.CustomDigLostLetter] != 1 {
		t.Errorf("the expert was asked %d times", r.counts[questplan.CustomDigLostLetter])
	}
}

// A death in the cave parks the quest for a day (StepDeathCaps), at once, not after the escalating backoff.
func TestADeathAtTheTabletParksTheDigsiteForADay(t *testing.T) {
	r := newDgRig(t, questplan.DigRemovedBlockage)
	r.e.D.Chat = chatlog.New()
	trips := 0
	res := r.run(Digsite(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomDigTablet {
			trips++
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || trips != 1 || !strings.Contains(res.Reason, "its cap is 1") {
		t.Fatalf("the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, trips, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.DigsiteID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A kit that cannot be filled does not loop: the engine's watchdog parks the quest, and a kit whose every source is dead parks it for hours.
func TestADigsiteKitThatNeverFillsParks(t *testing.T) {
	r := newDgRig(t, questplan.DigMineshaftPermit)
	r.give(questplan.DigObjTrowel, 1)
	res := r.run(Digsite(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitDigChem {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// The slot, the module wiring the engine reads and the factory's customs.
func TestTheDigsiteQuestIsWired(t *testing.T) {
	q := Digsite()
	if q.ID() != "itexam" || q.Module.Record.QuestPoints != 2 {
		t.Fatalf("built %q %d QP", q.ID(), q.Module.Record.QuestPoints)
	}
	for _, name := range []string{questplan.CustomDigPickpocket, questplan.CustomDigBush, questplan.CustomDigSacks, questplan.CustomDigTray, questplan.CustomDigPan,
		questplan.CustomDigGiveSample, questplan.CustomDigStudentGreen, questplan.CustomDigStudentOrange, questplan.CustomDigStudentPurple,
		questplan.CustomDigStudentGreen3, questplan.CustomDigStudentOrange3, questplan.CustomDigStudentPurple3, questplan.CustomDigCutOpal,
		questplan.CustomDigTalisman, questplan.CustomDigCharcoal, questplan.CustomDigUseOnExpert, questplan.CustomDigLostLetter, questplan.CustomDigGiveScroll,
		questplan.CustomDigWest, questplan.CustomDigChest, questplan.CustomDigBarrel, questplan.CustomDigMix, questplan.CustomDigBlast, questplan.CustomDigTablet,
		questplan.CustomDigLeave} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if len(q.Customs) != 25 {
		t.Errorf("%d customs registered, want 25", len(q.Customs))
	}
}
