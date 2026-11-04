package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// DwarfCannon is Dwarf Cannon ready for the engine. The Commander's and Nulodion's talks, the food (the provisioner's kit) and the room
// are the engine's generic steps; the customs are the legs the content makes special: six railings mended by a Crafting roll behind a
// hitpoint floor, the watchtower's ladders and the remains on its top floor (the remains are obj id 0, which no snapshot sees), the goblins'
// cave entered by a telejump and left by the mud pile (a pocket for every other quest's walks) with the dwarf child in its crate, the
// broken cannon's four components, and the Commander's conversation about the mended cannon, which has a gap in the middle.
func DwarfCannon() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.DwarfCannon,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomCannonRailings: cannonRailings,
			questplan.CustomCannonTower:    cannonTower,
			questplan.CustomCannonChild:    cannonChild,
			questplan.CustomCannonOut: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return cannonLeaveCave(e.Deps())
			},
			questplan.CustomCannonRepair: cannonRepair,
			questplan.CustomCannonReport: cannonReport,
		},
	}
}

const (
	// cannonRailingBudget bounds one railings run: twenty-odd rolls of a few seconds each, and the regeneration the cuts need when the
	// pack has no food (one hitpoint in 100 ticks), within the run's own time.
	cannonRailingBudget = 14 * time.Minute
	// cannonRegenWait is the longest one wait for the hitpoints to come back to the floor.
	cannonRegenWait = 5 * time.Minute
	// cannonRepairBudget bounds the repair of the four components: each takes about two Inspects in four to land.
	cannonRepairBudget = 5 * time.Minute
	// cannonClimbWait is how long a ladder gets to change the account's floor.
	cannonClimbWait = 12 * time.Second
	// cannonTelejumpWait is how long the cave's entrance and the mud pile get to move the account (a tick's delay and a telejump).
	cannonTelejumpWait = 15 * time.Second
	// cannonRemainsWait is how long the top floor is given to show the remains when another account has just taken them (a ground spawn
	// is back a hundred ticks after a pickup).
	cannonRemainsWait = 90 * time.Second
	// cannonChildWait is the 120 ticks the child another account freed stays in the crate's room, and the wait after "you find nothing".
	cannonChildWait = 80 * time.Second
	// cannonQuietAfter is how long the cannon's chat may be quiet, with no answer in the game messages, before an Inspect counts as over.
	cannonQuietAfter = 5 * time.Second
	// cannonQuiet is how long the Commander's conversation about the mended cannon may be quiet before it counts as over: he leaves the
	// chat to look at the cannon between two pages.
	cannonQuiet = 12 * time.Second
	// cannonTimeLeft is the run time below which a long leg stops starting attempts.
	cannonTimeLeft = 25 * time.Second
)

// stageReader is the part of the engine's journal reading the stage oracles use, so their order of calls can be tested without a session.
type stageReader interface {
	InvalidateStage()
	StageIs(n int) bool
}

// stageNow reports whether the journal says stage n NOW: it makes the engine forget its reading first. Engine.StageIs only re-reads the
// journal when the reading is stale, and the reading Engine.Snapshot took at the start of the tick is not stale, so inside a custom a
// bare StageIs answers with the stage from before the step: the remains taken, the child freed, the cannon mended or the Commander told
// would all read as still to do, and the custom would go on (or keep waiting) for a result it already had.
func stageNow(r stageReader, n int) bool {
	r.InvalidateStage()
	return r.StageIs(n)
}

// cannonStageNow is the oracle for the outcome of something the custom has just done: it opens the journal again. Use it after every
// action whose result is a stage change.
func cannonStageNow(e *questrunner.Engine, n int) bool { return stageNow(e, n) }

// cannonStageCached is the stage as of the last reading, for a custom's opening question (the snapshot has just read it) and for a
// wait that no action of the custom's own can have changed: it costs no journal open when the reading is fresh and still reads again
// when the last read failed (a failed read leaves the reading stale).
func cannonStageCached(e *questrunner.Engine, n int) bool { return e.StageIs(n) }

// cannonRunLow reports that the engine's run is nearly out of time.
func cannonRunLow(e *questrunner.Engine) bool {
	left := e.RunLeft()
	return left > 0 && left < cannonTimeLeft
}

// cannonDriveAll drives every chat page that appears, choosing from prefer, until none has for quiet (or budget runs out): a quest script
// that closes the chat to do something and opens it again is a lull, not an end. A menu none of prefer matches is closed and ends it.
func cannonDriveAll(d *questrunner.Deps, prefer []string, quiet, budget time.Duration) {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	last := time.Now()
	for time.Now().Before(deadline) {
		if dl.Ready() {
			if !dl.Drive(prefer) && len(dl.OptionTexts()) > 0 {
				logf(d, "dwarfcannon: an unexpected menu [%s]: closing it", strings.Join(dl.OptionTexts(), " | "))
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

// ---------------------------------------------------------------------------------------------------------------------------------
// The railings
// ---------------------------------------------------------------------------------------------------------------------------------

// A railing's three answers (railings.rs2), as the game chat carries them.
const (
	railingAlready = "already fixed this railing"
	railingFixed   = "replace the railing with no problems"
	railingFailed  = "fail and cut yourself trying"
)

// cannonRailingTry is one go at railing i: Inspect it, take the offer to replace it, and read the outcome. fixed is true for a railing
// that is mended now or was before; cut says a failed roll hurt the account; ok is false when the loc could not be reached or answered
// nothing.
func cannonRailingTry(d *questrunner.Deps, i int) (fixed, cut, ok bool) {
	spot := questplan.CannonRailingSpots[i]
	m, found := questrunner.FindLoc(d, questplan.CannonRailingLoc(i), "", "Inspect", spot, 3)
	if !found {
		logf(d, "dwarfcannon: no railing (loc %d) near %v", questplan.CannonRailingLoc(i), spot)
		return false, false, false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Inspect"); err != nil {
		logf(d, "dwarfcannon: the railing at %v: %v", spot, err)
		return false, false, false
	}
	// "You search the railing...", "One railing is broken ...", then the offer; or at once "You have already fixed this railing.".
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 14*time.Second, func() bool {
		return dl.Ready() || cookSaw(d, mark, railingAlready, railingFixed, railingFailed)
	})
	if dl.Ready() {
		dl.Drive([]string{"Try to replace the railing."})
	}
	waitUntil(d, 10*time.Second, func() bool { return cookSaw(d, mark, railingAlready, railingFixed, railingFailed) })
	clearChat(d)
	switch {
	case cookSaw(d, mark, railingFixed, railingAlready):
		return true, false, true
	case cookSaw(d, mark, railingFailed):
		return false, true, true
	}
	logf(d, "dwarfcannon: railing %d answered nothing we know", i+1)
	return false, false, false
}

// cannonHitpoints makes sure the account stands at or above the railings' floor before a roll that may cut it: it eats if it has food,
// and otherwise waits for the hitpoints to come back (one in a hundred ticks). It reports whether the floor is met.
func cannonHitpoints(e *questrunner.Engine, d *questrunner.Deps, deadline time.Time) bool {
	waitEnd := time.Now().Add(cannonRegenWait)
	for {
		hp, maxHP := questrunner.Hitpoints(d)
		if maxHP == 0 {
			return true // not read yet: the engine's own sustain and the floor rule cover it
		}
		floor := questplan.CannonRailingFloor(maxHP)
		if hp >= floor {
			return true
		}
		if questrunner.EatIfNeeded(d, questplan.CannonFoodIDs, floor-1) {
			d.Delay(1800 * time.Millisecond)
			continue
		}
		if time.Now().After(waitEnd) || time.Now().After(deadline) || cannonRunLow(e) {
			logf(d, "dwarfcannon: %d/%d hitpoints, under the railings' floor of %d, with no food to eat", hp, maxHP, floor)
			return false
		}
		if !d.Delay(5 * time.Second) {
			return false
		}
	}
}

// cannonRailings mends the six broken railings in order. The journal cannot say which are done until all are, so each is Inspected and
// the game's answer says ("You have already fixed this railing." costs nothing). A roll is made only at or above the hitpoint floor.
func cannonRailings(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	deadline := time.Now().Add(cannonRailingBudget)
	fixes := 0
	for i := 0; i < questplan.CannonRailings; i++ {
		for tries := 0; ; tries++ {
			if cannonRunLow(e) {
				// The slot is nearly out: the run ends as a timeout, without the half hour's park (the bits stay set).
				logf(d, "dwarfcannon: the run is nearly out at railing %d (%d mended this run)", i+1, fixes)
				e.YieldRun()
				return true
			}
			if time.Now().After(deadline) {
				logf(d, "dwarfcannon: out of time at railing %d (%d mended this run)", i+1, fixes)
				return false
			}
			if questrunner.HeldID(d, questplan.ObjCannonRailing) == 0 {
				// A railing is used up by a mend; with none left the Commander is asked for another.
				logf(d, "dwarfcannon: no railing left in the pack at railing %d", i+1)
				return true
			}
			if !cannonHitpoints(e, d, deadline) {
				if cannonRunLow(e) {
					e.YieldRun()
					return true
				}
				return false
			}
			fixed, cut, ok := cannonRailingTry(d, i)
			if !ok {
				if tries >= 3 {
					return false
				}
				d.Delay(1200 * time.Millisecond)
				continue
			}
			if fixed {
				fixes++
				break
			}
			if cut {
				continue
			}
		}
	}
	logf(d, "dwarfcannon: all six railings answered (%d mended this run)", fixes)
	return true
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The watchtower
// ---------------------------------------------------------------------------------------------------------------------------------

// cannonClimb takes the tower's ladder from the stand between them and waits for the floor to change. The landing is the account's own
// tile one floor up or down (~climb_ladder with movecoord(coord(), 0, 1, 0)), which is walkable on every floor.
func cannonClimb(d *questrunner.Deps, from int, up bool) bool {
	stand := questplan.CannonTowerStand
	stand.Level = from
	want, op, id := from-1, "Climb-down", questplan.LocCannonLadderDown
	if up {
		want, op, id = from+1, "Climb-up", questplan.LocCannonLadderUp
	}
	if questrunner.CurrentTile(d).Level == want {
		return true
	}
	if !questrunner.Travel(d, stand, 0) && questrunner.CurrentTile(d) != stand {
		logf(d, "dwarfcannon: the tower's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, id, "", op, stand, 3)
	if !ok {
		logf(d, "dwarfcannon: no ladder offering %q (loc %d) near %v", op, id, stand)
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "dwarfcannon: the ladder: %v", err)
		return false
	}
	if !waitUntil(d, cannonClimbWait, func() bool { return questrunner.CurrentTile(d).Level == want }) {
		logf(d, "dwarfcannon: the ladder did not change the account's floor (at %v): the trapdoor opens from stage 2 only", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(900 * time.Millisecond)
	return true
}

// cannonTowerDown takes the account down to the ground, from whichever floor it stands on.
func cannonTowerDown(d *questrunner.Deps) bool {
	for questrunner.CurrentTile(d).Level > 0 {
		if !cannonClimb(d, questrunner.CurrentTile(d).Level, false) {
			return false
		}
	}
	return true
}

// cannonTakeRemains takes the dwarf remains off the top floor. They are obj id 0, which the pack never shows, so the oracles are the
// ground item going and the journal page that says they are held.
func cannonTakeRemains(e *questrunner.Engine, d *questrunner.Deps) bool {
	spawn := questplan.CannonRemainsSpawn
	// The config names no ground option for any object (the client's default Take is option 3, [opobj3,mcannonremains]).
	op := 3
	if d.Config != nil {
		if obj, ok := d.Config.Obj(questplan.ObjCannonRemains); ok {
			if n := cachecfg.OpIndex(obj.Ops, "Take"); n != 0 {
				op = n
			}
		}
	}
	deadline := time.Now().Add(cannonRemainsWait)
	for time.Now().Before(deadline) {
		items := questrunner.GroundItemsNear(d, questplan.ObjCannonRemains, spawn, 6)
		if len(items) == 0 {
			// Only this call's own take can have moved the stage since the tick's reading, and the check after the click below has already
			// opened the journal again; this one costs nothing unless that read failed.
			if cannonStageCached(e, questplan.McTowerRemains) {
				return true
			}
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		g := items[0]
		at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if questrunner.CurrentTile(d) != at && !questrunner.WalkNear(d, at, 0) {
			questrunner.Travel(d, at, 1)
		}
		if err := actions.OpObj(d.Session, op, g.X, g.Z, int(g.ID)); err != nil {
			logf(d, "dwarfcannon: taking the remains: %v", err)
			return false
		}
		waitUntil(d, 6*time.Second, func() bool {
			return len(questrunner.GroundItemsNear(d, questplan.ObjCannonRemains, spawn, 6)) == 0
		})
		clearChat(d)
		if cannonStageNow(e, questplan.McTowerRemains) {
			return true
		}
	}
	logf(d, "dwarfcannon: no dwarf remains on the top floor at %v in %s", spawn, cannonRemainsWait)
	return false
}

// cannonTower climbs the watchtower, takes the remains and comes back down.
func cannonTower(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if cannonStageCached(e, questplan.McTowerRemains) {
		return cannonTowerDown(d)
	}
	for questrunner.CurrentTile(d).Level < 2 {
		if !cannonClimb(d, questrunner.CurrentTile(d).Level, true) {
			cannonTowerDown(d)
			return false
		}
	}
	if !questrunner.Travel(d, questplan.CannonRemainsSpawn, 1) {
		logf(d, "dwarfcannon: the remains at %v were not reached from %v", questplan.CannonRemainsSpawn, questrunner.CurrentTile(d))
		cannonTowerDown(d)
		return false
	}
	took := cannonTakeRemains(e, d)
	down := cannonTowerDown(d)
	return took && down
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The goblins' cave
// ---------------------------------------------------------------------------------------------------------------------------------

// cannonEnterCave takes the Cave Entrance, a telejump the walk graph does not carry, and waits for the cave.
func cannonEnterCave(d *questrunner.Deps) bool {
	if questplan.InCannonCave(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.CannonCaveStand, 0) && questrunner.CurrentTile(d) != questplan.CannonCaveStand {
		logf(d, "dwarfcannon: the Cave Entrance's stand %v was not reached from %v", questplan.CannonCaveStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCannonCave, "", "Enter", questplan.CannonCaveEntrance, 4)
	if !ok {
		logf(d, "dwarfcannon: no Cave Entrance (loc %d) near %v", questplan.LocCannonCave, questplan.CannonCaveEntrance)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Enter"); err != nil {
		logf(d, "dwarfcannon: the Cave Entrance: %v", err)
		return false
	}
	if !waitUntil(d, cannonTelejumpWait, func() bool { return questplan.InCannonCave(questrunner.CurrentTile(d)) }) {
		logf(d, "dwarfcannon: the Cave Entrance did not take the account in (at %v)", questrunner.CurrentTile(d))
		return false
	}
	clearChat(d)
	return true
}

// cannonLeaveCave climbs the mud pile at the cave's mouth, the way out the walk graph does not carry.
func cannonLeaveCave(d *questrunner.Deps) bool {
	if !questplan.InCannonCave(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.CannonCaveArrive, 1) {
		logf(d, "dwarfcannon: the mud pile at %v was not reached from %v", questplan.CannonMudPile, questrunner.CurrentTile(d))
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCannonMudPile, "", "Climb-over", questplan.CannonMudPile, 5)
	if !ok {
		logf(d, "dwarfcannon: no mud pile (loc %d) near %v", questplan.LocCannonMudPile, questplan.CannonMudPile)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-over"); err != nil {
		logf(d, "dwarfcannon: the mud pile: %v", err)
		return false
	}
	if !waitUntil(d, cannonTelejumpWait, func() bool { return !questplan.InCannonCave(questrunner.CurrentTile(d)) }) {
		logf(d, "dwarfcannon: the mud pile did not take the account out (at %v)", questrunner.CurrentTile(d))
		return false
	}
	return true
}

// cannonChild goes into the cave, to the crate at its far end and frees Gilob's son. The crate adds the child for 120 ticks and refuses
// ("You search the crate but find nothing.") while one is in the room, which another account's visit a minute ago leaves; the search is
// tried again after the wait. The page that says the child is rescued is the oracle.
func cannonChild(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !cannonEnterCave(d) {
		return false
	}
	for attempt := 0; attempt < 3; attempt++ {
		if cannonRunLow(e) {
			e.YieldRun()
			return true
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCannonCrate, "", "Search", questplan.CannonCrate, 4)
		if !ok {
			logf(d, "dwarfcannon: no crate (loc %d) near %v", questplan.LocCannonCrate, questplan.CannonCrate)
			return false
		}
		stands := questrunner.LocStands(d, m, questrunner.CurrentTile(d))
		reached := false
		for _, s := range stands {
			if questrunner.Travel(d, s, 0) || questrunner.CurrentTile(d) == s {
				reached = true
				break
			}
		}
		if !reached {
			logf(d, "dwarfcannon: no stand beside the crate was reached from %v", questrunner.CurrentTile(d))
			return false
		}
		mark := time.Now()
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			logf(d, "dwarfcannon: the crate: %v", err)
			return false
		}
		// "You search the crate..." and the child's chat, or "You search the crate but find nothing.".
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 12*time.Second, func() bool { return dl.Ready() || cookSaw(d, mark, "find nothing") })
		cannonDriveAll(d, nil, 6*time.Second, 60*time.Second)
		clearChat(d)
		if cannonStageNow(e, questplan.McChildRescued) {
			return true
		}
		if cookSaw(d, mark, "find nothing") {
			logf(d, "dwarfcannon: another dwarf child is in the room (a recent rescue): waiting %s", cannonChildWait)
			d.Delay(cannonChildWait)
		}
	}
	return false
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The broken cannon
// ---------------------------------------------------------------------------------------------------------------------------------

// The cannon's answers (mcannon_broken_cannon.rs2), as the game chat carries them.
const (
	cannonPartFixed   = "manage to fix it"
	cannonPartAlready = "already fixed this part"
	cannonPartFailed  = "too hard you fail to fix"
	cannonNoProblem   = "can't quite find the problem"
	cannonNotNow      = "strange dwarf contraption"
	cannonNoKit       = "need a tool kit"
)

var cannonCycle = []string{cannonPartFixed, cannonPartAlready, cannonPartFailed, cannonNoProblem, cannonNotNow, cannonNoKit}

// cannonInspect Inspects the cannon once and answers the repair menu for part ("" answers "None", which closes it). It returns the needle
// that ended the cycle, "" when none came.
func cannonInspect(d *questrunner.Deps, part string) string {
	m, ok := questrunner.FindLoc(d, questplan.LocCannonBroken, "", "Inspect", questplan.CannonShedCannon, 4)
	if !ok {
		logf(d, "dwarfcannon: no broken multicannon (loc %d) near %v", questplan.LocCannonBroken, questplan.CannonShedCannon)
		return ""
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Inspect"); err != nil {
		logf(d, "dwarfcannon: the cannon: %v", err)
		return ""
	}
	prefer := []string{"None"}
	if part != "" {
		prefer = []string{part}
	}
	dl := questrunner.NewDialogue(d)
	end := time.Now().Add(14 * time.Second)
	// A chat that was driven and then fell quiet with no answer is an Inspect with no message to read (the first one at stage 6, the one that
	// closes the stage): it ends after cannonQuietAfter, not after the whole wait.
	var driven time.Time
	for time.Now().Before(end) {
		if dl.Ready() {
			dl.Drive(prefer)
			driven = time.Now()
			continue
		}
		for _, n := range cannonCycle {
			if cookSaw(d, mark, n) {
				clearChat(d)
				return n
			}
		}
		if !driven.IsZero() && time.Since(driven) > cannonQuietAfter {
			break
		}
		if !d.Delay(300 * time.Millisecond) {
			break
		}
	}
	clearChat(d)
	return ""
}

// cannonRepair mends the four components in turn, then Inspects once more to close the stage. The repair menu offers all five lines on
// every Inspect (one Inspect resolves at most one component), the bits stay set from run to run, and a component that is mended already
// answers so at once.
func cannonRepair(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjCannonToolkit) == 0 {
		logf(d, "dwarfcannon: no tool kit in the pack")
		return false
	}
	if !questrunner.Travel(d, questplan.CannonShedStand, 1) {
		logf(d, "dwarfcannon: the shed at %v was not reached from %v", questplan.CannonShedStand, questrunner.CurrentTile(d))
		return false
	}
	deadline := time.Now().Add(cannonRepairBudget)
	var done [4]bool
	for time.Now().Before(deadline) && !cannonRunLow(e) {
		next := -1
		for i, ok := range done {
			if !ok {
				next = i
				break
			}
		}
		if next < 0 {
			// All four are mended: this Inspect finds the four bits set and closes the stage (a chat line, no game message).
			cannonInspect(d, "")
			if cannonStageNow(e, questplan.McCannonFixed) {
				return true
			}
			continue
		}
		switch cannonInspect(d, questplan.CannonParts[next]) {
		case cannonPartFixed, cannonPartAlready:
			done[next] = true
		case cannonNotNow:
			// Not stage 6 or 7 any more: the journal says which.
			if cannonStageNow(e, questplan.McCannonFixed) {
				return true
			}
		case cannonNoKit:
			return false
		}
	}
	// One reading for both questions: the loop ended on its budget or the run's time, and the last Inspect may have been the one that closed
	// the stage.
	if cannonStageNow(e, questplan.McCannonFixed) {
		return true
	}
	if cannonRunLow(e) {
		e.YieldRun()
		return true
	}
	return false
}

// cannonReport tells the Commander the cannon works. He walks to the shed to look between two pages, so the conversation is driven
// through the lull and the menu after it ("Ok then, just for you!").
func cannonReport(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stop := questplan.CannonCommanderStop
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "dwarfcannon: the Commander was not reached (at %v)", questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	cannonDriveAll(d, stop.Prefer, cannonQuiet, 90*time.Second)
	clearChat(d)
	return cannonStageNow(e, questplan.McSeeNulodion)
}

// The goblins' cave is a pocket: the Cave Entrance and the mud pile are telejumps the walk graph does not carry, so a walk from inside
// to anywhere outside comes back unreachable however often it is tried.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the goblins' cave under the Black Guard camp", In: questplan.InCannonCave, Leave: cannonLeaveCave})
}
