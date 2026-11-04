package questmods

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ClockTower is the Clock Tower ready for the engine. Kojo's talks, the bucket (the provisioner's kit and a generic use on the
// well) and the rat poison (a generic grab) are the engine's; the customs are the five legs the content makes special: one fetch
// per cog and the fitting of a cog on its spindle.
func ClockTower() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.ClockTower,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomCogBlue:  cogBlue,
			questplan.CustomCogBlack: cogBlack,
			questplan.CustomCogWhite: cogWhite,
			questplan.CustomCogRed:   cogRed,
			questplan.CustomCogPlace: cogPlace,
		},
	}
}

const (
	// cogSpawnWait bounds the wait for a cog another account has just taken: every cog is a single ground spawn that comes back
	// 100 ticks (a minute) after a pickup, and the quest's slot admits four accounts.
	cogSpawnWait = 100 * time.Second
	// cogOgreWait bounds the wait at the ogre cave's mouth for the ogres to stand off the cog.
	cogOgreWait = 2 * time.Minute
	// cogGateWait is how long the lever, the gate and the rat gate get to answer.
	cogGateWait = 8 * time.Second
)

// cogTake takes colour c's cog from its spawn, waiting for a respawn when another account has just taken it.
func cogTake(d *questrunner.Deps, c questplan.CogColour) bool {
	deadline := time.Now().Add(cogSpawnWait)
	for time.Now().Before(deadline) {
		if questrunner.HeldID(d, c.Obj) > 0 {
			return true
		}
		if questrunner.TakeGround(d, c.Obj, c.Spawn, 3) {
			logf(d, "cog: took the %s cog at %v", c.Name, c.Spawn)
			return true
		}
		// A refusal arrives as a message box, which holds every later click.
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(1500 * time.Millisecond) {
			return false
		}
	}
	return questrunner.HeldID(d, c.Obj) > 0
}

// cogPlace fits the cog in the pack on its spindle: up or down to the spindle's floor, the use of the cog on the loc by id (the
// look-alikes beside it refuse every cog), retried from a standstill. It reports whether the cog left the pack.
func cogPlace(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	c, ok := questplan.CogByObj(step.ItemID)
	if !ok || questrunner.HeldID(d, c.Obj) == 0 {
		logf(d, "cog: no cog of object %d in the pack to fit", step.ItemID)
		return false
	}
	if !questrunner.Travel(d, c.Stand, 1) {
		logf(d, "cog: the %s spindle's stand %v was not reached from %v", c.Name, c.Stand, questrunner.CurrentTile(d))
		return false
	}
	m, found := questrunner.FindLoc(d, c.Spindle, "", "", c.Stand, 5)
	if !found {
		logf(d, "cog: no %s spindle (loc %d) near %v", c.Name, c.Spindle, c.Stand)
		return false
	}
	for try := 1; try <= 3; try++ {
		if err := questrunner.UseItemOnLoc(d, c.Obj, m); err != nil {
			logf(d, "cog: the %s cog on its spindle: %v", c.Name, err)
			return false
		}
		if waitUntil(d, 8*time.Second, func() bool { return questrunner.HeldID(d, c.Obj) == 0 }) {
			logf(d, "cog: the %s cog fits its spindle at %d,%d,%d", c.Name, m.X, m.Z, m.Level)
			return true
		}
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	return false
}

// cogBlue fetches the blue cog from the sealed room east of the cellar: down the ladder at 2621,3262 (the walker's own route to
// the corridor east of the secret wall), the wall pushed from 2576,9631, the cog off the floor. The room's own ladder is the way
// out, and the next step's walk takes it.
func cogBlue(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	c, _ := questplan.CogByName("blue")
	if questrunner.HeldID(d, c.Obj) > 0 {
		return true
	}
	if !questplan.InCogBlueRoom(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.CogSecretWallStand, 0) {
			logf(d, "cog: the secret wall's stand %v was not reached from %v", questplan.CogSecretWallStand, questrunner.CurrentTile(d))
			return false
		}
		if !cogPushWall(d) {
			e.RequestPark("the secret wall would not let the account into the blue cog's room", 30*time.Minute)
			return false
		}
	}
	if !cogTake(d, c) {
		e.RequestPark("no blue cog on the floor of its room", 20*time.Minute)
		return false
	}
	return true
}

// cogPushWall pushes the secret wall from its east side and waits for the blue room.
func cogPushWall(d *questrunner.Deps) bool {
	m, ok := questrunner.FindLoc(d, questplan.LocCogSecretWall, "", "Push", questplan.CogSecretWallStand, 3)
	if !ok {
		logf(d, "cog: no secret wall (loc %d) near %v", questplan.LocCogSecretWall, questplan.CogSecretWallStand)
		return false
	}
	for try := 1; try <= 3; try++ {
		if err := questrunner.OperateLoc(d, m, "Push"); err != nil {
			logf(d, "cog: pushing the secret wall: %v", err)
			return false
		}
		if waitUntil(d, cogGateWait, func() bool { return questplan.InCogBlueRoom(questrunner.CurrentTile(d)) }) {
			logf(d, "cog: through the secret wall into the blue cog's room")
			return true
		}
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	logf(d, "cog: the secret wall did not let the account in (at %v)", questrunner.CurrentTile(d))
	return false
}

// cogBlack fetches the black cog: the walk to its spawn, the take, and when the take is refused ("The cog is red hot from
// flames.") the bucket of water poured on it ([opobju,blackcog]: "You pour water over the cog.") and the take again. The cooled
// bit is perm, so a cog taken after the first time is taken at once; the Take is tried first for that.
func cogBlack(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	c, _ := questplan.CogByName("black")
	if questrunner.HeldID(d, c.Obj) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjBucketOfWater) == 0 {
		logf(d, "cog: no bucket of water to cool the black cog with")
		return false
	}
	if !questrunner.Travel(d, c.Spawn, 2) {
		logf(d, "cog: the black cog's spawn %v was not reached from %v", c.Spawn, questrunner.CurrentTile(d))
		return false
	}
	// The cog may have been taken a moment ago: wait for it to be on the floor.
	if !waitUntil(d, cogSpawnWait, func() bool { return len(questrunner.GroundItemsNear(d, c.Obj, c.Spawn, 3)) > 0 }) {
		logf(d, "cog: no black cog on the floor at %v after %s", c.Spawn, cogSpawnWait)
		e.RequestPark("no black cog on the floor beside the spiders", 20*time.Minute)
		return false
	}
	if questrunner.TakeGround(d, c.Obj, c.Spawn, 3) {
		return true
	}
	questrunner.ClearLeftoverModal(d)
	items := questrunner.GroundItemsNear(d, c.Obj, c.Spawn, 3)
	if len(items) == 0 {
		return questrunner.HeldID(d, c.Obj) > 0
	}
	if !questrunner.PackVisible(d) {
		return false
	}
	logf(d, "cog: pouring the bucket of water over the black cog")
	before := questrunner.HeldID(d, questplan.ObjBucketOfWater)
	if err := actions.NewEquipment(d.Session, d.Components).UseOnGroundItem(int32(questplan.ObjBucketOfWater), items[0]); err != nil {
		logf(d, "cog: the bucket of water on the black cog: %v", err)
		return false
	}
	waitUntil(d, 8*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjBucketOfWater) < before })
	questrunner.ClearLeftoverModal(d)
	if !questrunner.TakeGround(d, c.Obj, c.Spawn, 3) {
		logf(d, "cog: the black cog was not taken after the water")
		return false
	}
	return true
}

// cogRed fetches the red cog from the ogres' cave. Nothing in it is fought (Module.NoRetaliate); the account waits at the cave's
// mouth, 6 or more tiles from every ogre, until none stands within OgreCalmNear of the cog, goes in for the cog, takes it and
// comes back out at once. The Decide has seen to the food (two sharks' worth) and the Record to the hitpoints (30): an ogre hits
// for up to 7 every 6 ticks, one at a time (a player in combat is skipped by the others, all.hunt check_notcombat), and the trip
// is some 25 ticks. The wait that never ends parks the quest for half an hour; a death here parks it for a day
// (Module.StepDeathCaps).
func cogRed(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	c, _ := questplan.CogByName("red")
	if questrunner.HeldID(d, c.Obj) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.RedCogStage, 2) {
		logf(d, "cog: the ogres' cave's mouth %v was not reached from %v", questplan.RedCogStage, questrunner.CurrentTile(d))
		return false
	}
	deadline := time.Now().Add(cogOgreWait)
	for {
		ogres := cogOgresInView(d)
		if questplan.OgreCalm(ogres, c.Spawn, questplan.OgreCalmNear) {
			logf(d, "cog: %d ogre(s) in view and none within %d of the red cog: going in", len(ogres), questplan.OgreCalmNear)
			break
		}
		if !time.Now().Before(deadline) {
			logf(d, "cog: an ogre still stands within %d of the red cog after %s: %v", questplan.OgreCalmNear, cogOgreWait, ogres)
			e.RequestPark("ogres stand by the red cog", 30*time.Minute)
			return false
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 && hp*2 < max {
			logf(d, "cog: %d/%d hitpoints before the ogres' cave: not going in", hp, max)
			e.RequestPark("too few hitpoints for the ogres' cave", 30*time.Minute)
			return false
		}
		if !d.Delay(time.Second) {
			return false
		}
	}
	if !questrunner.Travel(d, c.Spawn, 1) {
		logf(d, "cog: the red cog's spawn %v was not reached from %v", c.Spawn, questrunner.CurrentTile(d))
		questrunner.Travel(d, questplan.RedCogStage, 2)
		return false
	}
	got := questrunner.TakeGround(d, c.Obj, c.Spawn, 3)
	if got {
		logf(d, "cog: took the red cog; out of the ogres' cave")
	}
	questrunner.Travel(d, questplan.RedCogStage, 2)
	return got || questrunner.HeldID(d, c.Obj) > 0
}

// cogOgresInView is where the ogres in the scene stand (their south-west tiles).
func cogOgresInView(d *questrunner.Deps) []tile.Tile {
	var out []tile.Tile
	for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcOgre }).Results() {
		out = append(out, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
	}
	return out
}

// cogWhite fetches the white cog through the rat pen, from wherever the account stands: outside, the lever and the gate and the
// walk in; in the pen, the white room's gate (refused until the rats are poisoned), the poison on the food trough, the gate
// again; in the room, the cog. The poison is in the pack (the Decide grabs it first: the cellar it lies in cannot be walked back
// to from the pen). The room's ladder is the way out, and the next step's walk takes it. A gate that stays shut after the lever
// and a room with no cog in it park the quest for twenty minutes, with the reason.
func cogWhite(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	c, _ := questplan.CogByName("white")
	for round := 0; round < 4; round++ {
		if questrunner.HeldID(d, c.Obj) > 0 {
			return true
		}
		here := questrunner.CurrentTile(d)
		switch {
		case questplan.InCogWhiteRoom(here):
			if !cogTake(d, c) {
				e.RequestPark("no white cog on the floor of its room", 20*time.Minute)
				return false
			}
			return true
		case questplan.InCogPen(here):
			if cogCrossRatGate(d) {
				continue
			}
			if questrunner.HeldID(d, questplan.ObjRatPoison) == 0 {
				logf(d, "cog: the rat gate is shut and the pack holds no poison: leaving the pen for some")
				cogLeavePen(d)
				return false
			}
			if !cogPoisonTrough(d) {
				return false
			}
			if !cogCrossRatGate(d) {
				return false
			}
		default:
			if entered, stuck := cogEnterPen(d); !entered {
				if stuck {
					e.RequestPark("the rat pen's gate stays shut after its lever was pulled", 20*time.Minute)
				}
				return false
			}
		}
	}
	return questrunner.HeldID(d, c.Obj) > 0
}

// cogGateShut reports the pen's gate standing (loc 37 at its tile): the world's, so another account's open gate counts.
func cogGateShut(d *questrunner.Deps) bool {
	_, shut := questrunner.FindLoc(d, questplan.LocPenGate, "", "", questplan.PenGateTile, 0)
	return shut
}

// cogEnterPen opens the pen's gate with the lever and walks in; stuck reports a gate that stood shut through three pulls. Lever A
// (loc 33) opens it; lever B beside it does nothing, and the lever once pulled (loc 35) shuts it again, so only loc 33 is ever
// pulled.
func cogEnterPen(d *questrunner.Deps) (entered, stuck bool) {
	if cogGateShut(d) {
		// A gate seen standing, or not yet seen at all (the server tells the scene only what is near): the lever's corridor has
		// both the lever and the gate in view.
		if !questrunner.Travel(d, questplan.CogLeverTile, 1) {
			logf(d, "cog: the lever's corridor %v was not reached from %v", questplan.CogLeverTile, questrunner.CurrentTile(d))
			return false, false
		}
	}
	for try := 1; cogGateShut(d); try++ {
		if try > 3 {
			logf(d, "cog: the pen's gate stays shut after the lever was pulled three times")
			return false, true
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCogLever, "", "Pull", questplan.CogLeverTile, 3)
		if !ok {
			// The lever reads pulled (loc 35) while the gate still stands: the world is mid-change.
			if !waitUntil(d, cogGateWait, func() bool { return !cogGateShut(d) }) {
				logf(d, "cog: neither lever A nor an open gate near %v", questplan.CogLeverTile)
				return false, false
			}
			break
		}
		logf(d, "cog: pulling the lever that opens the rat pen's gate")
		if err := questrunner.OperateLoc(d, m, "Pull"); err != nil {
			logf(d, "cog: pulling the lever: %v", err)
			return false, false
		}
		waitUntil(d, cogGateWait, func() bool { return !cogGateShut(d) })
	}
	// The gate stays open 500 ticks. The tile outside it is on the planner's graph and the step west through it is one plain move
	// click (the planner knows the gate only as the wall the map placed); the walker, which plans the step from the walls the server
	// has changed, is the fallback, and not the first try because a walk it cannot make counts toward the quest's travel park.
	if !questplan.InCogPen(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.PenGateOutside, 0) || !cogStep(d, questplan.PenGateTile) {
			if !questrunner.Travel(d, questplan.PenGateTile, 0) && !questplan.InCogPen(questrunner.CurrentTile(d)) {
				logf(d, "cog: the open gate was not walked through (at %v)", questrunner.CurrentTile(d))
				return false, false
			}
		}
	}
	logf(d, "cog: in the rat pen")
	return questplan.InCogPen(questrunner.CurrentTile(d)), false
}

// cogStep takes one step to an adjacent tile with a plain move click and waits for the account to be on it.
func cogStep(d *questrunner.Deps, to tile.Tile) bool {
	if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: to.X, Z: to.Z, Level: to.Level}}, false); err != nil {
		logf(d, "cog: the step to %v: %v", to, err)
		return false
	}
	return waitUntil(d, 4*time.Second, func() bool { return questrunner.CurrentTile(d) == to })
}

// cogPoisonTrough pours the rat poison on the food trough in the pen, which kills the rats and sets the rat-door bit
// (quest_cog_food_trough.rs2). It reports whether the poison left the pack.
func cogPoisonTrough(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.TroughStand, 1) {
		logf(d, "cog: the food trough's stand %v was not reached from %v", questplan.TroughStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocFoodTrough, "", "", questplan.TroughStand, 4)
	if !ok {
		logf(d, "cog: no food trough near %v", questplan.TroughStand)
		return false
	}
	for try := 1; try <= 3; try++ {
		if err := questrunner.UseItemOnLoc(d, questplan.ObjRatPoison, m); err != nil {
			logf(d, "cog: the rat poison on the food trough: %v", err)
			return false
		}
		if waitUntil(d, cogGateWait, func() bool { return questrunner.HeldID(d, questplan.ObjRatPoison) == 0 }) {
			logf(d, "cog: the rats have eaten the poison")
			return true
		}
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	return false
}

// cogRatGateRefusal is what the gate says (a game message, no box) until the rat-door bit is set.
const cogRatGateRefusal = "does not seem to be openable"

// cogCrossRatGate goes through the white room's gate from the pen's side and waits for the room. The gate answers "This door
// does not seem to be openable." until the rat-door bit is set; once it is, [oploc1,ctratgatec] raises a message box ("The death
// throes of the rats seem to have shaken the door loose of its hinges...") and only opens the door and moves the account when
// that box has been continued. ~mesbox ends in p_pausebutton, so the box is answered with its Continue, never with CLOSE_MODAL:
// the engine's closeModal() drops a script paused on its button (Player.closeModal sets activeScript to null at PAUSEBUTTON),
// and the door would never open. The box arrives a few ticks after the op (the account walks to the gate first), so nothing is
// cleared right after the op either: OperateLoc has cleared what was up before it sent the op, and the engine clears between
// steps only once this has returned.
func cogCrossRatGate(d *questrunner.Deps) bool {
	m, ok := questrunner.FindLoc(d, questplan.LocRatGate, "", "Go-through", questplan.RatGateTile, 3)
	if !ok {
		logf(d, "cog: no rat gate (loc %d) near %v", questplan.LocRatGate, questplan.RatGateTile)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Go-through"); err != nil {
		logf(d, "cog: the rat gate: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	inRoom := func() bool { return questplan.InCogWhiteRoom(questrunner.CurrentTile(d)) }
	refused := func() bool { return cookSaw(d, mark, cogRatGateRefusal) }
	if !waitUntil(d, cogGateWait, func() bool { return dl.Ready() || inRoom() || refused() }) {
		logf(d, "cog: the rat gate answered nothing within %s (at %v)", cogGateWait, questrunner.CurrentTile(d))
		return false
	}
	if refused() {
		logf(d, "cog: the rat gate is shut: the rats are not dead yet")
		return false
	}
	if dl.Ready() {
		// No menu here: Drive only continues pages.
		dl.Drive(nil)
	}
	if waitUntil(d, cogGateWait, inRoom) {
		logf(d, "cog: through the rat gate into the white cog's room")
		return true
	}
	logf(d, "cog: the rat gate's box was continued and the account is still outside the room (at %v)", questrunner.CurrentTile(d))
	return false
}

// cogLeavePen walks the account out of the rat pen: opening the gate from inside when it has shut (Open answers from the gate's
// own tile only), then through it. It is also the pen's pocket: no walk leaves a pen whose gate has shut.
func cogLeavePen(d *questrunner.Deps) bool {
	if !questplan.InCogPen(questrunner.CurrentTile(d)) {
		return true
	}
	if m, shut := questrunner.FindLoc(d, questplan.LocPenGate, "", "Open", questplan.PenGateTile, 0); shut {
		if !questrunner.Travel(d, questplan.PenGateTile, 0) {
			logf(d, "cog: the pen's gate tile was not reached from %v", questrunner.CurrentTile(d))
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "cog: opening the pen's gate from inside: %v", err)
			return false
		}
		waitUntil(d, cogGateWait, func() bool { return !cogGateShut(d) })
	}
	if !questrunner.Travel(d, questplan.PenGateOutside, 0) {
		cogStep(d, questplan.PenGateOutside)
	}
	out := waitUntil(d, 6*time.Second, func() bool { return !questplan.InCogPen(questrunner.CurrentTile(d)) })
	if out {
		logf(d, "cog: out of the rat pen")
	}
	return out
}

// The rat pen is a pocket for every quest's walks: its gate shuts 500 ticks after it opens.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Clock Tower's rat pen", In: questplan.InCogPen, Leave: cogLeavePen})
}
