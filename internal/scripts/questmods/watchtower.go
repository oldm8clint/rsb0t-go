package questmods

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Watchtower is the Watch Tower ready for the engine (questplan/watchtower.go has the story and the stages, watchtowerdecide.go the order). The generic talk
// step covers the plain conversations (the wizard on his floor, Og, Grew on his island, Toban in his camp, the City guard); everything else is a custom, in
// three files: this one (the helpers, the tower, the tribes and the pockets' ways out), watchtowercity.go (Gu'Tanoth: the stall, the battlement, the chasm, the
// caves, the east gate) and watchtowerenclave.go (the nightshade, the enclave, the potion, the shamans, the crystals and the lever).
//
// Every custom is restartable: it begins by asking whether its goal is already met, walks to its own stand (leaving any pocket the account is in first: the
// pockets are registered at the foot of this file with their own ways out) and reports what it can prove from the pack, the area and the journal.
func Watchtower() questrunner.Quest {
	q := questrunner.Quest{
		Module: questplan.Watchtower,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomWtClimb: wtClimbStep,
			questplan.CustomWtDown: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return wtTowerDown(e.Deps())
			},
			questplan.CustomWtBush:       wtBushStep,
			questplan.CustomWtRelicBack:  wtRelicBackStep,
			questplan.CustomWtAskOg:      wtAskOgStep,
			questplan.CustomWtAskGrew:    wtAskGrewStep,
			questplan.CustomWtAskToban:   wtAskTobanStep,
			questplan.CustomWtCampIn:     func(e *questrunner.Engine, _ questplan.Step) bool { return wtCampIn(e.Deps()) },
			questplan.CustomWtChest:      wtChestStep,
			questplan.CustomWtGorad:      wtGoradStep,
			questplan.CustomWtIslandIn:   func(e *questrunner.Engine, _ questplan.Step) bool { return wtIslandIn(e.Deps()) },
			questplan.CustomWtBerries:    wtBerriesStep,
			questplan.CustomWtRelicGate:  wtRelicGateStep,
			questplan.CustomWtCake:       wtCakeStep,
			questplan.CustomWtBattlement: wtBattlementStep,
			questplan.CustomWtJump:       wtJumpStep,
			questplan.CustomWtRiddleGive: wtRiddleGiveStep,
			questplan.CustomWtCave:       wtCaveStep,
			questplan.CustomWtMad:        wtMadStep,
			questplan.CustomWtNightshade: wtNightshadeStep,
			questplan.CustomWtEnclaveIn: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return wtEnclaveIn(e.Deps())
			},
			questplan.CustomWtEnclaveOut: func(e *questrunner.Engine, _ questplan.Step) bool { return wtEnclaveOut(e.Deps()) },
			questplan.CustomWtBrew:       wtBrewStep,
			questplan.CustomWtInfuse:     wtInfuseStep,
			questplan.CustomWtDissolve:   wtDissolveStep,
			questplan.CustomWtRock:       wtRockStep,
			questplan.CustomWtRobe:       wtRobeStep,
			questplan.CustomWtCrystals:   wtCrystalsStep,
			questplan.CustomWtLever:      wtLeverStep,
		},
	}
	// One name for each piece of the relic, so that the wizard's refusal of one fails only that piece's custom.
	for _, part := range []int{questplan.WtObjPart1, questplan.WtObjPart2, questplan.WtObjPart3} {
		q.Customs[questplan.WtGivePartName(part)] = wtGivePartStep
	}
	return q
}

const (
	// wtChat is the quiet that ends an ordinary conversation: the longest pause in these scripts is p_delay(3), two seconds, and a message box between two pages
	// is raised and lowered inside it.
	wtChat = 6 * time.Second
	// wtLongChat is the infusion's: the wizard closes the chat to mutter over the potion and speaks again three ticks later.
	wtLongChat = 9 * time.Second
	// wtClimbWait is how long a ladder or the rocks get to change the account's floor; wtTeleWait a cave mouth or a swing to move it.
	wtClimbWait = 12 * time.Second
	wtTeleWait  = 15 * time.Second
	// wtItemWait is how long an op gets to put its item in the pack.
	wtItemWait = 8 * time.Second
	// wtTimeLeft is the run time below which a long leg stops starting attempts.
	wtTimeLeft = 25 * time.Second
	// wtGoradBudget bounds the fight with Gorad (80 hitpoints; the model says a few hundred ticks for the fleet's swing).
	wtGoradBudget = 7 * time.Minute
	// wtGoradWait is how long Gorad is waited for when another account has just killed him (he is back 60 ticks later).
	wtGoradWait = 90 * time.Second
	// wtCakeBudget bounds the stall: the trader has to be away from it, and it is empty for twelve ticks after each theft.
	wtCakeBudget = 5 * time.Minute
	// wtShamanBudget bounds the dissolving: six potions' worth of walking and the 102 ticks a dissolved shaman takes to come back.
	wtShamanBudget = 9 * time.Minute
)

// wtRunLow reports that the engine's run is nearly out of time.
func wtRunLow(e *questrunner.Engine) bool {
	left := e.RunLeft()
	return left > 0 && left < wtTimeLeft
}

// wtAvoidRects are questplan.WtAvoid as the walker's rectangles, made once.
var wtAvoidRects = sync.OnceValue(func() []webwalk.Rect {
	var out []webwalk.Rect
	for _, r := range questplan.WtAvoid() {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
})

// wtWalk walks to within radius of dest. The surface walks keep out of the surface chieftains' ground where a route round it exists, and take the plain walk
// where none does (the stand is then priced by the decision); a pocket's walk is the engine's, which leaves the pocket the account is in first.
func wtWalk(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if here, ok := questrunner.CurrentTileOK(d); ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if dest.Level == 0 && dest.Z < 4000 {
		if questrunner.TravelAvoiding(d, dest, radius, wtAvoidRects()) {
			return true
		}
		logf(d, "watchtower: no walk to %v round the chieftains: the plain walk", dest)
	}
	return questrunner.Travel(d, dest, radius)
}

// wtReadState is the journal's reading of the quest, for the executors that need to know whether a conversation moved it. A var, so that a test can
// answer it without a quest tab.
var wtReadState = func(d *questrunner.Deps) (questplan.WtState, bool) {
	lines, err := questrunner.ReadJournal(d, questplan.WatchtowerName)
	if err != nil {
		logf(d, "watchtower: reading the journal: %v", err)
		return questplan.WtState{}, false
	}
	n, ok := questplan.ParseWatchtowerJournal(lines)
	return questplan.WtStateOf(n), ok
}

// wtDriveAll drives every chat page that appears, choosing from prefer, until none has for quiet (or budget runs out): a quest script that closes the chat to
// do something and opens it again is a lull, not an end. A menu none of prefer matches is closed and ends it, and so is a box the driver cannot continue.
func wtDriveAll(d *questrunner.Deps, prefer []string, quiet, budget time.Duration) {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	last := time.Now()
	for time.Now().Before(deadline) {
		if dl.Ready() {
			if !dl.Drive(prefer) && len(dl.OptionTexts()) > 0 {
				logf(d, "watchtower: an unexpected menu [%s]: closing it", strings.Join(dl.OptionTexts(), " | "))
				questrunner.ClearLeftoverModal(d)
				return
			}
			last = time.Now()
			continue
		}
		if questrunner.ClearLeftoverModal(d) {
			last = time.Now()
			continue
		}
		if time.Since(last) >= quiet {
			return
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
}

// wtOperate operates the loc id near near with op and waits for done, driving any conversation the op raises (prefer chooses its menus).
func wtOperate(d *questrunner.Deps, id int, op string, near tile.Tile, radius int, prefer []string, done func() bool, wait time.Duration) bool {
	m, ok := questrunner.FindLoc(d, id, "", op, near, radius)
	if !ok {
		logf(d, "watchtower: no loc %d offering %q near %v", id, op, near)
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "watchtower: loc %d (%s): %v", id, op, err)
		return false
	}
	return wtAwait(d, prefer, done, wait)
}

// wtAwait waits for done, driving the conversations and clearing the boxes that come up meanwhile.
func wtAwait(d *questrunner.Deps, prefer []string, done func() bool, wait time.Duration) bool {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		if dl.Ready() {
			if !dl.Drive(prefer) && len(dl.OptionTexts()) > 0 {
				questrunner.ClearLeftoverModal(d)
			}
			continue
		}
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
	}
	return done()
}

func wtArea(d *questrunner.Deps) questplan.WtArea {
	return questplan.WtAreaOf(questrunner.CurrentTile(d))
}

func wtLevel(d *questrunner.Deps) int { return questrunner.CurrentTile(d).Level }

func wtHeld(d *questrunner.Deps, id int) bool { return questrunner.HeldID(d, id) > 0 }

// ---------------------------------------------------------------------------------------------------------------------------------
// The tower
// ---------------------------------------------------------------------------------------------------------------------------------

// wtClimbStep climbs to the wizard's floor: by the Climbing rocks on the north wall while the quest is not started (Qty 1: the tower guard turns the ground
// ladder away until then, and the walk graph does not know), by the ground ladder and the first floor's after.
func wtClimbStep(e *questrunner.Engine, step questplan.Step) bool {
	defer e.InvalidateStage()
	return wtTowerUp(e.Deps(), step.Qty == 1)
}

func wtTowerUp(d *questrunner.Deps, byWall bool) bool {
	for try := 0; try < 4; try++ {
		here := questrunner.CurrentTile(d)
		area := questplan.WtAreaOf(here)
		switch {
		case area == questplan.WtAreaTower && here.Level == 2:
			return true
		case area == questplan.WtAreaTower && here.Level == 1:
			if !wtWalk(d, questplan.WtFirstFloorStand, 0) && questrunner.CurrentTile(d) != questplan.WtFirstFloorStand {
				logf(d, "watchtower: the first floor's ladder stand %v was not reached from %v", questplan.WtFirstFloorStand, questrunner.CurrentTile(d))
				return false
			}
			if !wtOperate(d, questplan.LocWtLadderUp, "Climb-up", questplan.WtFirstFloorStand, 3, nil, func() bool { return wtLevel(d) == 2 }, wtClimbWait) {
				logf(d, "watchtower: the ladder to the wizard's floor did not take the account up (at %v)", questrunner.CurrentTile(d))
				return false
			}
			d.Delay(900 * time.Millisecond)
		case byWall:
			if !wtWalk(d, questplan.WtWallStand, 0) && questrunner.CurrentTile(d) != questplan.WtWallStand {
				logf(d, "watchtower: the wall's stand %v was not reached from %v", questplan.WtWallStand, questrunner.CurrentTile(d))
				return false
			}
			if !wtOperate(d, questplan.LocWtClimbRocks, "Climb-up", tile.Tile{X: 2548, Z: 3119}, 4, nil, func() bool { return wtLevel(d) == 1 }, wtClimbWait) {
				logf(d, "watchtower: the Climbing rocks did not take the account up (Agility 18 is wanted; at %v)", questrunner.CurrentTile(d))
				return false
			}
			d.Delay(900 * time.Millisecond)
		default:
			if !wtWalk(d, questplan.WtTowerLadderStand, 0) && questrunner.CurrentTile(d) != questplan.WtTowerLadderStand {
				logf(d, "watchtower: the tower ladder's stand %v was not reached from %v", questplan.WtTowerLadderStand, questrunner.CurrentTile(d))
				return false
			}
			// The guard at the foot says his piece before the ladder is climbed.
			if !wtOperate(d, questplan.LocWtTowerLadder, "Climb-up", tile.Tile{X: 2544, Z: 3111}, 4, nil, func() bool { return wtLevel(d) == 1 }, wtClimbWait+8*time.Second) {
				logf(d, "watchtower: the tower's ladder did not take the account up (at %v)", questrunner.CurrentTile(d))
				return false
			}
			d.Delay(900 * time.Millisecond)
		}
	}
	return false
}

// wtTowerDown takes the account down from the wizard's floor, or from the mirror tower the lever sends it to, to the ground by the two ladders. It is the way out
// of both pockets.
func wtTowerDown(d *questrunner.Deps) bool {
	for try := 0; try < 4; try++ {
		here := questrunner.CurrentTile(d)
		area := questplan.WtAreaOf(here)
		if area != questplan.WtAreaTower && area != questplan.WtAreaMirror {
			return true
		}
		switch here.Level {
		case 2:
			if !wtOperate(d, questplan.LocWtLadderDown, "Climb-down", here, 12, nil, func() bool { return wtLevel(d) == 1 }, wtClimbWait) {
				logf(d, "watchtower: no ladder down from %v", here)
				return false
			}
			d.Delay(900 * time.Millisecond)
		case 1:
			if !wtOperate(d, questplan.LocWtLadderTop, "Climb-down", here, 12, nil, func() bool { return wtLevel(d) == 0 }, wtClimbWait) {
				logf(d, "watchtower: no ladder down from the first floor at %v", here)
				return false
			}
			d.Delay(900 * time.Millisecond)
		default:
			return false
		}
	}
	return wtArea(d) != questplan.WtAreaTower && wtArea(d) != questplan.WtAreaMirror
}

// wtToWizardFloor brings the account to the wizard's floor for a custom that talks to him or pulls his lever.
func wtToWizardFloor(d *questrunner.Deps) bool {
	if a := wtArea(d); a == questplan.WtAreaTower && wtLevel(d) == 2 {
		return true
	}
	return wtTowerUp(d, false)
}

// wtBushStep searches the bush that holds the fingernails (loc 2799; its forty look-alikes are 2798 and 2800 to 2803, which hold nothing or a decoy).
func wtBushStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjFingernails) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for the fingernails")
		return false
	}
	if !wtWalk(d, questplan.WtBushStand, 1) {
		logf(d, "watchtower: the bush's stand %v was not reached from %v", questplan.WtBushStand, questrunner.CurrentTile(d))
		return false
	}
	if !wtOperate(d, questplan.LocWtNailBush, "Search", questplan.WtBush, 4, nil, func() bool { return wtHeld(d, questplan.WtObjFingernails) }, wtItemWait) {
		return false
	}
	wtDriveAll(d, nil, 3*time.Second, 15*time.Second)
	return wtHeld(d, questplan.WtObjFingernails)
}

// wtUseOnWizard climbs to the wizard, uses a held item on him and drives his answers.
func wtUseOnWizard(d *questrunner.Deps, item int, quiet time.Duration) bool {
	if !wtToWizardFloor(d) {
		logf(d, "watchtower: the wizard's floor was not reached from %v", questrunner.CurrentTile(d))
		return false
	}
	npc, ok := questrunner.FindNpcByID(d, questplan.WtNpcWizard, "", 14)
	if !ok {
		logf(d, "watchtower: no wizard within 14 tiles of %v", questrunner.CurrentTile(d))
		return false
	}
	if err := questrunner.UseItemOnNpc(d, item, npc); err != nil {
		logf(d, "watchtower: using %s on the wizard: %v", questrunner.ObjName(d, item), err)
		return false
	}
	wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
	wtDriveAll(d, nil, quiet, 60*time.Second)
	return true
}

// wtGivePartStep hands the wizard a piece of the relic (Qty is its object id). The third makes him assemble the relic.
func wtGivePartStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	id := step.Qty
	if !wtHeld(d, id) {
		return true
	}
	if !wtUseOnWizard(d, id, wtChat) {
		return false
	}
	return !wtHeld(d, id)
}

// wtRelicBackStep asks the wizard for the relic he assembled and the account lost ("I have lost the relic you gave me.", offered at stage 3 only).
func wtRelicBackStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjRelic) {
		return true
	}
	if !wtToWizardFloor(d) {
		return false
	}
	stop := questplan.WtWizardStop("I have lost the relic you gave me.")
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	wtDriveAll(d, stop.Prefer, wtChat, 45*time.Second)
	return wtHeld(d, questplan.WtObjRelic)
}

// wtAskPart asks a helped tribe's giver for the relic piece he gave and the account lost: his "lost the relic part" menu, which Og's, Toban's and (while the
// wizard lacks that piece) Grew's talks offer. approach brings the account to the giver's side (the island, the camp). It reports whether the piece is in the
// pack afterwards; a conversation that gave nothing (Grew's menu without the option: the wizard holds his piece) is a failed ask, which the decision counts as
// asked and goes on.
func wtAskPart(e *questrunner.Engine, d *questrunner.Deps, giver string, stop questplan.NpcStop, part int, approach func() bool) bool {
	defer e.InvalidateStage()
	if wtHeld(d, part) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for the relic piece %s would give", giver)
		return false
	}
	if !approach() {
		return false
	}
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: stop, Reason: "ask " + giver + " for the relic piece the account lost"}) {
		logf(d, "watchtower: %s's lost-piece menu was not driven to its end", giver)
	}
	wtDriveAll(d, nil, 3*time.Second, 12*time.Second)
	return wtHeld(d, part)
}

func wtAskOgStep(e *questrunner.Engine, _ questplan.Step) bool {
	return wtAskPart(e, e.Deps(), "Og", questplan.WtOgLostPartStop, questplan.WtObjPart1, func() bool { return true })
}

func wtAskTobanStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	return wtAskPart(e, d, "Toban", questplan.WtTobanLostPartStop, questplan.WtObjPart3, func() bool { return wtCampIn(d) })
}

func wtAskGrewStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	return wtAskPart(e, d, "Grew", questplan.WtGrewLostPartStop, questplan.WtObjPart2, func() bool { return wtIslandIn(d) })
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Toban's camp
// ---------------------------------------------------------------------------------------------------------------------------------

// wtCampIn takes the cave at 2499,2989, which puts the account in the camp.
func wtCampIn(d *questrunner.Deps) bool {
	if wtArea(d) == questplan.WtAreaCamp {
		return true
	}
	if !wtWalk(d, questplan.WtCampCaveStand, 0) && questrunner.CurrentTile(d) != questplan.WtCampCaveStand {
		logf(d, "watchtower: the camp cave's stand %v was not reached from %v", questplan.WtCampCaveStand, questrunner.CurrentTile(d))
		return false
	}
	if !wtOperate(d, questplan.LocWtTobanCave, "Enter", questplan.WtCampCave, 4, nil, func() bool { return wtArea(d) == questplan.WtAreaCamp }, wtTeleWait) {
		logf(d, "watchtower: the cave did not take the account into the camp (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(900 * time.Millisecond)
	return true
}

// wtCampOut climbs the ladder out of the camp (the pocket's way out).
func wtCampOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaCamp {
		return true
	}
	if !wtOperate(d, questplan.LocWtTobanLadder, "Climb-down", questplan.WtCampLadder, 6, nil, func() bool { return wtArea(d) != questplan.WtAreaCamp }, wtTeleWait) {
		logf(d, "watchtower: the camp's ladder did not take the account out (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(900 * time.Millisecond)
	return true
}

// wtChestStep opens Toban's chest by using Og's key on it (the key is not used up that way; "Open" eats it).
func wtChestStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjStolenGold) {
		return true
	}
	if !wtHeld(d, questplan.WtObjTobanKey) {
		logf(d, "watchtower: no key for Toban's chest")
		return false
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for the gold in Toban's chest")
		return false
	}
	if !wtCampIn(d) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWtTobanChest, "", "", questplan.WtCampChest, 4)
	if !ok {
		logf(d, "watchtower: no chest (loc %d) near %v", questplan.LocWtTobanChest, questplan.WtCampChest)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.WtObjTobanKey, m); err != nil {
		logf(d, "watchtower: the key on the chest: %v", err)
		return false
	}
	// The gold arrives before the box that shows it.
	if !wtAwait(d, nil, func() bool { return wtHeld(d, questplan.WtObjStolenGold) }, wtItemWait) {
		return false
	}
	wtDriveAll(d, nil, 3*time.Second, 12*time.Second)
	return wtHeld(d, questplan.WtObjStolenGold)
}

// wtGoradStep fights Gorad for his tooth, by an explicit attack; he is level 68 with 80 hitpoints and no hunt mode, so nothing else starts it. The camp's ogres
// may join: the decision priced the pair. The tooth is added by his death (a free slot is wanted) and a box shows it.
func wtGoradStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjTooth) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for Gorad's tooth")
		return false
	}
	if !wtCampIn(d) {
		return false
	}
	deadline := time.Now().Add(wtGoradWait)
	for {
		npc, ok := questrunner.FindNpcByID(d, questplan.WtNpcGorad, "", 20)
		if ok {
			sus := questplan.Watchtower.Sustain
			switch questrunner.FightChosen(d, npc, sus, wtGoradBudget, nil) {
			case questrunner.FightWon:
				if !wtAwait(d, nil, func() bool { return wtHeld(d, questplan.WtObjTooth) }, wtItemWait) {
					logf(d, "watchtower: Gorad is gone and the pack has no tooth (another account was his hero?)")
					return false
				}
				wtDriveAll(d, nil, 3*time.Second, 12*time.Second)
				return true
			case questrunner.FightStopped:
				return false
			default:
				logf(d, "watchtower: the fight with Gorad was not won")
				return false
			}
		}
		if time.Now().After(deadline) || wtRunLow(e) {
			logf(d, "watchtower: no Gorad in the camp within %s", wtGoradWait)
			return false
		}
		if !d.Delay(3 * time.Second) {
			return false
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// Grew's island
// ---------------------------------------------------------------------------------------------------------------------------------

// wtIslandIn ties the rope to the branch at 2499,3087 (the rope is used up) and swings across.
func wtIslandIn(d *questrunner.Deps) bool {
	if wtArea(d) == questplan.WtAreaIsland {
		return true
	}
	if !wtHeld(d, questplan.WtObjRope) {
		logf(d, "watchtower: no rope for the branch")
		return false
	}
	if !wtWalk(d, questplan.WtBranchStand, 1) {
		logf(d, "watchtower: the branch's stand %v was not reached from %v", questplan.WtBranchStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWtBranch, "", "", questplan.WtBranch, 4)
	if !ok {
		logf(d, "watchtower: no branch (loc %d) near %v", questplan.LocWtBranch, questplan.WtBranch)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.WtObjRope, m); err != nil {
		logf(d, "watchtower: the rope on the branch: %v", err)
		return false
	}
	if !wtAwait(d, nil, func() bool { return wtArea(d) == questplan.WtAreaIsland }, wtTeleWait) {
		logf(d, "watchtower: the swing did not take the account across (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	return true
}

// wtIslandOut swings back off the island by the rope that hangs from the island's tree (Agility 10: the content checks it for this swing).
func wtIslandOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaIsland {
		return true
	}
	if have := questrunner.CurrentLevel(d, "agility"); have > 0 && have < questplan.WtAgilitySwing {
		logf(d, "watchtower: Agility %d is under the %d the swing back off the island wants", have, questplan.WtAgilitySwing)
		return false
	}
	for try := 0; try < 3; try++ {
		if !questrunner.Travel(d, questplan.WtSwingStand, 0) && questrunner.CurrentTile(d) != questplan.WtSwingStand {
			logf(d, "watchtower: the swing's stand %v was not reached from %v", questplan.WtSwingStand, questrunner.CurrentTile(d))
			return false
		}
		if wtOperate(d, questplan.LocWtSwingBack, "Swing-on", tile.Tile{X: 2511, Z: 3090}, 4, nil, func() bool { return wtArea(d) != questplan.WtAreaIsland }, wtTeleWait) {
			d.Delay(1200 * time.Millisecond)
			return true
		}
		// "The rope swing is being used" for four ticks: try again.
		questrunner.ClearLeftoverModal(d)
		d.Delay(2400 * time.Millisecond)
	}
	return false
}

// wtBerriesStep picks the jangerberries on the island's four ground spawns (Qty of them) and swings back.
func wtBerriesStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	want := step.Qty
	if want <= 0 {
		want = questplan.WtJangerberryQty
	}
	if questrunner.HeldID(d, questplan.WtObjJangerberries) >= want {
		return true
	}
	if !wtIslandIn(d) {
		return false
	}
	for _, spot := range questplan.WtBerries {
		if questrunner.HeldID(d, questplan.WtObjJangerberries) >= want {
			break
		}
		if !questrunner.Travel(d, spot, 1) {
			continue
		}
		questrunner.TakeGround(d, questplan.WtObjJangerberries, spot, 3)
	}
	got := questrunner.HeldID(d, questplan.WtObjJangerberries)
	out := wtIslandOut(d)
	return got > 0 && out
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The pockets
// ---------------------------------------------------------------------------------------------------------------------------------

func wtInArea(a questplan.WtArea) func(tile.Tile) bool {
	return func(t tile.Tile) bool { return questplan.WtAreaOf(t) == a }
}

// The pockets: the walk graph carries none of these ladders, ropes, mouths and rocks, so a walk from inside to anywhere outside comes back unreachable
// however often it is tried. Each is left by the same way the quest's own legs use.
func init() {
	for _, p := range []questrunner.Pocket{
		{Name: "the Watchtower's upper floors", In: wtInArea(questplan.WtAreaTower), Leave: wtTowerDown},
		{Name: "the Watchtower's mirror copy", In: wtInArea(questplan.WtAreaMirror), Leave: wtTowerDown},
		{Name: "Grew's island", In: wtInArea(questplan.WtAreaIsland), Leave: wtIslandOut},
		{Name: "Toban's camp", In: wtInArea(questplan.WtAreaCamp), Leave: wtCampOut},
		{Name: "Gu'Tanoth's lower city", In: wtInArea(questplan.WtAreaLowerCity), Leave: wtLowerOut},
		{Name: "the City guard's side of the chasm", In: wtInArea(questplan.WtAreaCityGuard), Leave: wtJumpOut},
		{Name: "the ground behind Gu'Tanoth's east gate", In: wtInArea(questplan.WtAreaEastInside), Leave: wtEastOut},
		{Name: "the skavid caves", In: wtInArea(questplan.WtAreaCaves), Leave: wtCaveOut},
		{Name: "the shamans' enclave", In: wtInArea(questplan.WtAreaEnclave), Leave: wtEnclaveOut},
	} {
		questrunner.RegisterPocket(p)
	}
}

// wtNpcIn returns the nearest npc of id within radius, for the executors that pick among several.
func wtNpcIn(d *questrunner.Deps, id, radius int) (entityquery.Entity, bool) {
	return questrunner.FindNpcByID(d, id, "", radius)
}
