package questmods

// The Observatory Quest and the Sea Slug Quest through the real engine (Engine.Run) on a simulated session, in the manner of the other
// fillers' (monkcog_engine_test.go): the module's own Decide runs against snapshots built from a fake pack, the account's tile and
// levels and the quest tab's colour, and every step it asks for is performed by a stub that changes that world the way the server would.
// What is proven is the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and
// the completion record): each scenario reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that
// cannot be done parks the quest with its reason. What is not proven here is each custom's own live execution (the ladder, the gate, the
// guard, the boat): those are the real-data tests and the live logs.

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
	"rsb0t-go/internal/scripts/tile"
)

// osSeed gives the simulated session one skill at a level, as the server's UPDATE_STAT does.
func osSeed(r *csRig, stat, level int) {
	p := packet.NewWriter(6)
	p.P1(byte(stat))
	p.P4(int32(levels.XPAtLevel(level)))
	p.P1(byte(level))
	r.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

// Skill indices of the protocol's stat table (gamestate.SkillNames).
const (
	osAttack     = 0
	osDefence    = 1
	osStrength   = 2
	osFiremaking = 11
)

// osFighter seeds a melee account.
func osFighter(r *csRig, level, hp int) {
	osSeed(r, osAttack, level)
	osSeed(r, osStrength, level)
	osSeed(r, osDefence, level)
	osSeed(r, gamestate.HitpointsStat, hp)
}

// ---------------------------------------------------------------------------------------------------------------------
// The Observatory Quest
// ---------------------------------------------------------------------------------------------------------------------

// obsRig is the Observatory Quest's state on the rig: r.stage is %itgronigen and the tab follows it.
type obsRig struct {
	*csRig
	trips   int
	scopes  int
	ashes   int
	provide map[string]func()
}

func newObsRig(t *testing.T, stage int) *obsRig {
	r := &obsRig{csRig: newCSRig(t, questplan.EastArdougneNorth), provide: map[string]func(){}}
	r.stageKnown = true
	r.setStage(stage)
	osFighter(r.csRig, 45, 55)
	return r
}

func (r *obsRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.ObsNotStarted:
		status = quests.NotStarted
	case questplan.ObsComplete:
		status = quests.Complete
	}
	r.setTab(questplan.ObservatoryName, status)
}

// apply plays the content's side of the quest: the professor's hand-overs, the furnace and the mould-on-glass, the customs' effects.
func (r *obsRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		if f, ok := r.provide[step.KitName]; ok {
			f()
			return true
		}
		if step.KitName == questplan.KitObsBundle || step.KitName == questplan.KitObsFood {
			return true
		}
		r.t.Errorf("unexpected kit %q", step.KitName)
		return false
	case questplan.StepScanBank, questplan.StepDeposit, questplan.StepWithdraw:
		return true
	case questplan.StepTalk:
		switch r.stage {
		case questplan.ObsNotStarted:
			r.setStage(questplan.ObsStarted)
		case questplan.ObsStarted:
			if r.held(questplan.ObjPlank) >= 3 {
				r.take(questplan.ObjPlank, 3)
				r.setStage(questplan.ObsGivenPlanks)
			}
		case questplan.ObsGivenPlanks:
			if r.held(questplan.ObjBronzeBar) > 0 {
				r.take(questplan.ObjBronzeBar, 1)
				r.setStage(questplan.ObsGivenBronze)
			}
		case questplan.ObsGivenBronze:
			if r.held(questplan.ObjMoltenGlass) > 0 {
				r.take(questplan.ObjMoltenGlass, 1)
				r.setStage(questplan.ObsGivenGlass)
			}
		case questplan.ObsGivenGlass:
			if r.held(questplan.ObjLensMould) > 0 {
				r.give(questplan.ObjMoltenGlass, 1)
				r.setStage(questplan.ObsGivenMould)
			}
		case questplan.ObsGivenMould:
			if r.held(questplan.ObjLens) > 0 {
				r.take(questplan.ObjLens, 1)
				r.take(questplan.ObjLensMould, 1)
				r.setStage(questplan.ObsSentTelescope)
			}
		}
		return true
	case questplan.StepUseOn:
		switch {
		case step.ItemID == questplan.ObjCopperOre:
			r.take(questplan.ObjCopperOre, 1)
			r.take(questplan.ObjTinOre, 1)
			r.give(questplan.ObjBronzeBar, 1)
		case step.ItemID == questplan.ObjBucketOfSand:
			r.take(questplan.ObjBucketOfSand, 1)
			r.take(questplan.ObjSodaAsh, 1)
			r.give(questplan.ObjMoltenGlass, 1)
		case step.ItemID == questplan.ObjLensMould:
			r.take(questplan.ObjMoltenGlass, 1)
			r.give(questplan.ObjLens, 1)
		default:
			r.t.Errorf("unexpected use-on %s", step.Describe())
		}
		return true
	}
	switch step.Custom {
	case questplan.CustomObsMould:
		r.trips++
		r.give(questplan.ObjLensMould, 1)
		return true
	case questplan.CustomObsAsh:
		r.ashes++
		r.take(questplan.ObjSeaweed, 1)
		r.give(questplan.ObjSodaAsh, 1)
		return true
	case questplan.CustomObsTelescope:
		r.scopes++
		r.setStage(questplan.ObsComplete)
		return true
	case questplan.CustomObsLeave:
		return true
	}
	r.t.Errorf("no answer for the step %q", step.Custom)
	return false
}

// The whole quest in the content's order, every executor a stub: the planks from the ground, the ores smelted, the sand and the
// seaweed made into glass, the mould from the dungeon, the lens, the telescope.
func TestTheObservatoryReachesCompleteThroughTheEngine(t *testing.T) {
	r := newObsRig(t, questplan.ObsNotStarted)
	r.provide[questplan.KitObsPlanks] = func() { r.give(questplan.ObjPlank, 3) }
	r.provide[questplan.KitObsSand] = func() { r.give(questplan.ObjBucketOfSand, 1) }
	r.provide[questplan.KitObsBundle] = func() {
		r.give(questplan.ObjCopperOre, 1)
		r.give(questplan.ObjTinOre, 1)
		r.give(questplan.ObjSeaweed, 1)
	}
	r.provide[questplan.KitObsFood] = func() { r.give(questplan.ObjShark, 8) }
	res := r.run(Observatory(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Observatory professor", "provision obs-bundle", "provision obs-planks", "talk to Observatory professor",
		"use Copper ore on Furnace", "talk to Observatory professor", "provision obs-sand", questplan.CustomObsAsh, "use Bucket of sand on Furnace",
		"talk to Observatory professor", "provision obs-food", questplan.CustomObsMould, "talk to Observatory professor",
		"use Lens mould on Molten glass", "talk to Observatory professor", questplan.CustomObsTelescope)
	if r.trips != 1 || r.scopes != 1 || r.ashes != 1 {
		t.Errorf("%d dungeon trips, %d telescopes, %d ashes", r.trips, r.scopes, r.ashes)
	}
	if got := r.e.Store.Record(questplan.ObservatoryID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjPlank, questplan.ObjBronzeBar, questplan.ObjMoltenGlass, questplan.ObjLensMould, questplan.ObjLens} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the telescope", id)
		}
	}
}

// A weak account is not sent to the goblin guard: the engine parks the quest for the guard's six hours with the model's reason, and the
// side store has it.
func TestTheObservatoryParksAWeakAccountAtTheGuard(t *testing.T) {
	r := newObsRig(t, questplan.ObsGivenGlass)
	osFighter(r.csRig, 10, 20)
	r.give(questplan.ObjShark, 8)
	res := r.run(Observatory(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "not going down to the goblin guard") || r.trips != 0 {
		t.Fatalf("the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, r.trips, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ObservatoryID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.ObservatoryID, time.Now().Add(7*time.Hour)); parked {
		t.Error("parked past the six hours")
	}
}

// The same account with food and the levels goes down once.
func TestTheObservatorySendsAStrongAccountToTheGuardOnce(t *testing.T) {
	r := newObsRig(t, questplan.ObsGivenGlass)
	r.give(questplan.ObjShark, 8)
	r.provide[questplan.KitObsBundle] = func() {}
	res := r.run(Observatory(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.trips != 1 {
		t.Fatalf("the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, r.trips, r.log)
	}
}

// A death on the mould trip parks the quest for a day (StepDeathCaps), at once, not after the escalating backoff.
func TestADeathOnTheMouldTripParksTheObservatoryForADay(t *testing.T) {
	r := newObsRig(t, questplan.ObsGivenGlass)
	r.e.D.Chat = chatlog.New()
	r.give(questplan.ObjShark, 8)
	trips := 0
	r.provide[questplan.KitObsBundle] = func() {}
	res := r.run(Observatory(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomObsMould {
			trips++
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || trips != 1 || !strings.Contains(res.Reason, "its cap is 1") {
		t.Fatalf("the run ended %s (%s) after %d trips: %v", res.Outcome, res.Reason, trips, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ObservatoryID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// A guard still standing after the fight's budget asks for a park of an hour, and the leg is run once.
func TestAGuardThatWillNotFallParksTheObservatoryForAnHour(t *testing.T) {
	r := newObsRig(t, questplan.ObsGivenGlass)
	r.give(questplan.ObjShark, 8)
	r.provide[questplan.KitObsBundle] = func() {}
	runs := 0
	res := r.run(Observatory(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomObsMould {
			runs++
			r.e.RequestPark("the goblin guard was still standing after the fight's budget", time.Hour)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "goblin guard") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ObservatoryID, time.Now().Add(59*time.Minute)); !parked {
		t.Error("not parked for the hour")
	}
	if parked, _ := r.e.Store.Parked(questplan.ObservatoryID, time.Now().Add(61*time.Minute)); parked {
		t.Error("parked past the hour")
	}
}

// A kit that cannot be filled does not loop: the engine's watchdog parks the quest.
func TestAnObservatoryKitThatNeverFillsParks(t *testing.T) {
	r := newObsRig(t, questplan.ObsStarted)
	res := r.run(Observatory(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitObsPlanks {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
}

// The telescope alone finishes a quest begun elsewhere (stage 6), and the module reads the stage from the journal on a started tab.
func TestTheObservatoryFinishesAtTheTelescope(t *testing.T) {
	r := newObsRig(t, questplan.ObsSentTelescope)
	res := r.run(Observatory(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete || r.scopes != 1 {
		t.Fatalf("the run ended %s (%s) after %d telescopes: %v", res.Outcome, res.Reason, r.scopes, r.log)
	}
	if !slices.Equal(r.described(), []string{questplan.CustomObsTelescope + " (through the dungeon and the dome to the telescope)"}) {
		t.Errorf("steps %v", r.described())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Sea Slug Quest
// ---------------------------------------------------------------------------------------------------------------------

// slugRig is the Sea Slug Quest's state on the rig: r.stage is %seaslugquest, the account's tile gives the region and the floor.
type slugRig struct {
	*csRig
	rubs int
	trip []string
}

func newSlugRig(t *testing.T, stage int, region questplan.SlugRegion) *slugRig {
	r := &slugRig{csRig: newCSRig(t, questplan.ArdougneSouthBank)}
	r.stageKnown = true
	r.setStage(stage)
	r.setRegion(region)
	osSeed(r.csRig, osFiremaking, 40)
	return r
}

func (r *slugRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.SlugNotStarted:
		status = quests.NotStarted
	case questplan.SlugComplete:
		status = quests.Complete
	}
	r.setTab(questplan.SeaSlugName, status)
}

func (r *slugRig) setRegion(region questplan.SlugRegion) {
	var at tile.Tile
	switch region {
	case questplan.SlugMainland:
		at = questplan.ArdougneSouthBank
	case questplan.SlugPlatform:
		at = questplan.SlugPlatformArrive
	default:
		at = questplan.SlugIslandArrive
	}
	r.sim.StandAtLevel(at.X, at.Z, 0)
}

func (r *slugRig) region() questplan.SlugRegion {
	x, z, level, _ := r.sim.State().Tile()
	return questplan.SlugRegionOf(tile.Tile{X: x, Z: z, Level: level})
}

func (r *slugRig) level() int {
	_, _, level, _ := r.sim.State().Tile()
	return level
}

// board is the boat: the torch goes out on the crossing from the shore.
func (r *slugRig) board(to questplan.SlugRegion) {
	if r.region() == questplan.SlugMainland && r.held(questplan.ObjTorchLit) > 0 {
		r.take(questplan.ObjTorchLit, 1)
		r.give(questplan.ObjTorchUnlit, 1)
	}
	r.setRegion(to)
}

func (r *slugRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		if step.KitName != questplan.KitSlugPaste {
			r.t.Errorf("unexpected kit %q", step.KitName)
			return false
		}
		r.give(questplan.ObjSwampPaste, 1)
		return true
	case questplan.StepGrabGround:
		r.give(step.ItemID, 1)
		return true
	case questplan.StepUseOn:
		r.take(questplan.ObjDampSticks, 1)
		r.give(questplan.ObjDrySticks, 1)
		return true
	case questplan.StepTalk:
		switch step.Stop.ID {
		case questplan.NpcCaroline:
			switch r.stage {
			case questplan.SlugNotStarted:
				r.setStage(questplan.SlugStarted)
			case questplan.SlugSavedKennith:
				r.setStage(questplan.SlugComplete)
			}
		case questplan.NpcHolgartLand:
			switch r.stage {
			case questplan.SlugStarted:
				r.setStage(questplan.SlugSpokenHolgart)
			case questplan.SlugSpokenHolgart:
				if r.held(questplan.ObjSwampPaste) > 0 {
					r.take(questplan.ObjSwampPaste, 1)
					r.setStage(questplan.SlugBoatRepaired)
					r.board(questplan.SlugPlatform)
				}
			default:
				r.board(questplan.SlugPlatform)
			}
		case questplan.NpcBailey:
			if r.held(questplan.ObjTorchLit)+r.held(questplan.ObjTorchUnlit) == 0 {
				r.give(questplan.ObjTorchUnlit, 1)
			}
		default:
			r.t.Errorf("a talk with npc %d", step.Stop.ID)
		}
		r.trip = append(r.trip, step.Stop.Npc)
		return true
	}
	r.trip = append(r.trip, step.Custom)
	switch step.Custom {
	case questplan.CustomSlugToPlatform:
		r.board(questplan.SlugPlatform)
	case questplan.CustomSlugToIsland:
		r.board(questplan.SlugIsland)
		if r.stage == questplan.SlugSpokenKennith {
			r.setStage(questplan.SlugSailedKent)
		}
	case questplan.CustomSlugToMainland:
		r.board(questplan.SlugMainland)
	case questplan.CustomSlugDown:
		r.sim.StandAtLevel(questplan.SlugLadderFoot.X, questplan.SlugLadderFoot.Z, 0)
	case questplan.CustomSlugKennith:
		switch r.stage {
		case questplan.SlugBoatRepaired:
			r.setStage(questplan.SlugSpokenKennith)
		case questplan.SlugLitTorch:
			if r.held(questplan.ObjTorchLit) == 0 {
				r.t.Errorf("up the ladder with no lit torch")
			}
			r.setStage(questplan.SlugKennithNeedEscape)
		}
	case questplan.CustomSlugKent:
		r.setStage(questplan.SlugSpokenKent)
	case questplan.CustomSlugRub:
		r.rubs++
		r.take(questplan.ObjTorchUnlit, 1)
		r.give(questplan.ObjTorchLit, 1)
		if r.stage == questplan.SlugSpokenKent {
			r.setStage(questplan.SlugLitTorch)
		}
	case questplan.CustomSlugPanel:
		r.setStage(questplan.SlugNeedKennithPath)
	case questplan.CustomSlugCrane:
		r.setStage(questplan.SlugSavedKennith)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// The whole quest in the content's order, every executor a stub, from the shore: the paste from the shop, the platform, Kennith, the
// island, Kent, the torch (a stick and a piece of glass off the deck), the panel, the crane, home.
func TestTheSeaSlugReachesCompleteThroughTheEngine(t *testing.T) {
	r := newSlugRig(t, questplan.SlugNotStarted, questplan.SlugMainland)
	res := r.run(SeaSlug(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	want := []string{"talk to Caroline", "talk to Holgart", "provision slug-paste", "talk to Holgart", questplan.CustomSlugKennith, questplan.CustomSlugToIsland,
		questplan.CustomSlugKent, questplan.CustomSlugToPlatform, "talk to Bailey", "grab Damp sticks", "grab Broken glass", "use Broken glass on Damp sticks",
		questplan.CustomSlugRub, questplan.CustomSlugKennith, questplan.CustomSlugPanel, questplan.CustomSlugCrane, questplan.CustomSlugToMainland, "talk to Caroline"}
	r.wantSteps(want...)
	if r.rubs != 1 {
		t.Errorf("%d rubs", r.rubs)
	}
	if got := r.e.Store.Record(questplan.SeaSlugID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
}

// An account that dies on the platform is respawned on the mainland with no torch: from stage 7 it sails back and asks Bailey for another.
func TestASeaSlugAccountThatLostItsTorchSailsBackAndAsksBaileyAgain(t *testing.T) {
	r := newSlugRig(t, questplan.SlugLitTorch, questplan.SlugMainland)
	r.give(questplan.ObjDrySticks, 1) // the sticks are not used up by a rub
	res := r.run(SeaSlug(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	head := r.described()[:4]
	if !strings.HasPrefix(head[0], questplan.CustomSlugToPlatform) || head[1] != "talk to Bailey" || !strings.HasPrefix(head[2], questplan.CustomSlugRub) ||
		!strings.HasPrefix(head[3], questplan.CustomSlugKennith) {
		t.Errorf("the run began %v", head)
	}
}

// Below Firemaking 30 the sticks never catch: the quest parks at the torch with the level said, once, and is not tried again that day.
func TestTheSeaSlugParksBelowFiremaking30(t *testing.T) {
	r := newSlugRig(t, questplan.SlugSpokenKent, questplan.SlugPlatform)
	osSeed(r.csRig, osFiremaking, 29)
	res := r.run(SeaSlug(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Firemaking 29") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.SeaSlugID, time.Now().Add(2*time.Hour)); !parked {
		t.Error("not parked")
	}
}

// A rub that asks for a park (the content refused the level) is run once, and a death anywhere parks the quest for the day.
func TestTheSeaSlugParksOnADeathAndOnARequest(t *testing.T) {
	r := newSlugRig(t, questplan.SlugStarted, questplan.SlugMainland)
	r.e.D.Chat = chatlog.New()
	res := r.run(SeaSlug(), func(step questplan.Step) bool {
		r.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 1") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.SeaSlugID, time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
	r = newSlugRig(t, questplan.SlugSpokenKent, questplan.SlugPlatform)
	runs := 0
	res = r.run(SeaSlug(), func(step questplan.Step) bool {
		switch {
		case step.Kind == questplan.StepTalk:
			return r.apply(step)
		case step.Kind == questplan.StepGrabGround || step.Kind == questplan.StepUseOn:
			return r.apply(step)
		case step.Custom == questplan.CustomSlugRub:
			runs++
			r.e.RequestPark("Firemaking is under 30: the dry sticks never catch", questplan.SlugParkFor)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "Firemaking") {
		t.Fatalf("outcome %s after %d rubs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
}
