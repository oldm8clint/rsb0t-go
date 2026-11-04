package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ElementalWorkshop is the Elemental Workshop ready for the engine. The bank, the mule and the shops (the kit), the deposits and the
// provisioning rounds are the engine's generic steps; the customs are the legs the content makes special: the bookcase and the book, the
// knife through its spine, the key on the smithy's wall and the stairs (teleports into a pocket), the three crates, the water wheel's two
// valves and lever, the bellows, the lava, the Pile of Rock with its earth elemental (the one fight), the furnace (and the air lever
// when it asks for more heat), the workbench, and the way up.
func ElementalWorkshop() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.ElementalWorkshop,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomEwBook:  ewBook,
			questplan.CustomEwRead:  ewRead,
			questplan.CustomEwSlash: ewSlash,
			questplan.CustomEwEnter: ewEnter,
			questplan.CustomEwLeave: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return ewLeaveAll(e.Deps())
			},
			questplan.CustomEwSupplies: ewSupplies,
			questplan.CustomEwWater:    ewWater,
			questplan.CustomEwBellows:  ewBellows,
			questplan.CustomEwLava:     ewLava,
			questplan.CustomEwOre:      ewOre,
			questplan.CustomEwSmelt:    ewSmelt,
			questplan.CustomEwShield:   ewShield,
		},
	}
}

const (
	// ewArriveWait is how long a wall, a staircase or a ladder gets to move the account somewhere else.
	ewArriveWait = 15 * time.Second
	// ewMsgWait is how long a loc op gets to answer in the chat.
	ewMsgWait = 8 * time.Second
	// ewLeverReset is how long a lever stays a different loc after a pull ([elemental_workshop_lever_pulled], three ticks) and then some.
	ewLeverReset = 4 * time.Second
	// ewSpawnWait is how long the rock gets to put its earth elemental in view.
	ewSpawnWait = 12 * time.Second
	// ewFightBudget bounds the fight with the earth elemental (35 hitpoints, a hit of up to 4 every 6 ticks): the model's kill takes a
	// minute or two at the levels the odds accept, so four minutes is a fight that is not being won.
	ewFightBudget = 4 * time.Minute
	// ewNoHitAfter is how long into the fight the elemental may stand untouched before the fight is given up.
	ewNoHitAfter = 40 * time.Second
	// ewSmeltWait is how long the furnace gets to hand over a bar (a three-tick animation and the chat).
	ewSmeltWait = 10 * time.Second
	// ewSmelts bounds the tries at the furnace: the first finds out whether the air is pumping, the second follows the lever.
	ewSmelts = 3
	// ewWaters bounds the passes at the water wheel: a pass the gates reset leaves the clean state the next one starts from.
	ewWaters = 3
)

// ewSettle closes whatever message box or book a script left open, so that the next interaction is not dropped.
func ewSettle(d *questrunner.Deps) {
	for i := 0; i < 3; i++ {
		if !questrunner.ClearLeftoverModal(d) {
			return
		}
	}
}

// ewRegion is where the account stands relative to the workshop.
func ewRegion(d *questrunner.Deps) questplan.EwRegion {
	return questplan.EwRegionOf(questrunner.CurrentTile(d))
}

// ewLoc finds a loc by id near a tile with the op it must offer, waiting a few seconds for it to be its usable form (a lever is another loc
// for three ticks after a pull; a wall is another for three after a crossing), and says so in the log when it is not there.
func ewLoc(d *questrunner.Deps, id int, op string, near tile.Tile, radius int) (locindex.Match, bool) {
	m, ok := questrunner.FindLoc(d, id, "", op, near, radius)
	if ok {
		return m, true
	}
	waitUntil(d, ewLeverReset, func() bool { _, again := questrunner.FindLoc(d, id, "", op, near, radius); return again })
	if m, ok = questrunner.FindLoc(d, id, "", op, near, radius); ok {
		return m, true
	}
	logf(d, "workshop: no loc %d offering %q near %d,%d,%d", id, op, near.X, near.Z, near.Level)
	return m, false
}

// ewBook searches the bookcase for the Battered book: the content gives one whenever the pack holds none and there is room.
func ewBook(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwBook) > 0 {
		return true
	}
	m, ok := ewLoc(d, questplan.LocEwBookcase, "Search", questplan.EwBookcaseAt, 3)
	if !ok {
		return false
	}
	mark := time.Now()
	logf(d, "workshop: searching the bookcase for the Battered book")
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "workshop: the bookcase: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait, func() bool {
		return questrunner.HeldID(d, questplan.ObjEwBook) > 0 || cookSaw(d, mark, "nothing of interest", "enough room")
	})
	if questrunner.HeldID(d, questplan.ObjEwBook) == 0 {
		logf(d, "workshop: the bookcase gave no book [%s]", huntSaid(d, mark))
		return false
	}
	return true
}

// ewRead reads the book, which starts the quest (the bit and the tab's colour are set before the book opens, so the book is closed
// at once).
func ewRead(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwBook) == 0 {
		logf(d, "workshop: no Battered book to read")
		return false
	}
	logf(d, "workshop: reading the Battered book")
	if err := questrunner.OpHeldByName(d, questplan.ObjEwBook, "Read"); err != nil {
		logf(d, "workshop: reading the book: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait, func() bool {
		main, _, chat := d.Session.State().Modals()
		return main != gamestate.NoModal || chat != gamestate.NoModal
	})
	ewSettle(d)
	started := waitUntil(d, ewMsgWait, func() bool {
		return questrunner.ReadStatus(d, questplan.ElementalWorkshopName) == questplan.StatusInProgress
	})
	if !started {
		logf(d, "workshop: the quest tab did not turn yellow after the book was read")
	}
	return started
}

// ewSlash cuts the key out of the book's spine with a knife (opheldu on the book: the key is added when the pack holds none).
func ewSlash(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwKey) > 0 {
		return true
	}
	mark := time.Now()
	logf(d, "workshop: cutting the book's spine for the key")
	if err := questrunner.UseItemOnItem(d, questplan.ObjKnife, questplan.ObjEwBook); err != nil {
		logf(d, "workshop: the knife on the book: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait, func() bool {
		return questrunner.HeldID(d, questplan.ObjEwKey) > 0 || cookSaw(d, mark, "don't want to damage")
	})
	if questrunner.HeldID(d, questplan.ObjEwKey) == 0 {
		logf(d, "workshop: the book gave no key [%s]", huntSaid(d, mark))
		return false
	}
	return true
}

// ewWall finds one half of the smithy's double wall offering op, in whatever form another account's crossing has left it.
func ewWall(d *questrunner.Deps, op string) (locindex.Match, bool) {
	for _, id := range []int{questplan.LocEwWallRight, questplan.LocEwWallLeft} {
		if m, ok := questrunner.FindLoc(d, id, "", op, questplan.EwWallAt, 3); ok {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// ewThroughWall uses the key on the smithy's Odd looking wall from outside and waits for the secret room. (A Push from outside answers
// "You see a small hole in the wall but no way to open it.": the wall opens from outside only to the key, and from inside to a Push.)
func ewThroughWall(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjEwKey) == 0 {
		logf(d, "workshop: no Battered key for the smithy's wall")
		return false
	}
	if !questrunner.Travel(d, questplan.EwWallOutside, 0) && !atTile(d, questplan.EwWallOutside) {
		logf(d, "workshop: the smithy's wall (outside %v) was not reached from %v", questplan.EwWallOutside, questrunner.CurrentTile(d))
		return false
	}
	m, ok := ewWall(d, "Push")
	if !ok {
		waitUntil(d, ewLeverReset, func() bool { _, again := ewWall(d, "Push"); return again })
		if m, ok = ewWall(d, "Push"); !ok {
			logf(d, "workshop: no Odd looking wall (loc %d, %d) offering Push near %v", questplan.LocEwWallRight, questplan.LocEwWallLeft, questplan.EwWallAt)
			return false
		}
	}
	logf(d, "workshop: the Battered key on the smithy's wall")
	if err := questrunner.UseItemOnLoc(d, questplan.ObjEwKey, m); err != nil {
		logf(d, "workshop: the key on the wall: %v", err)
		return false
	}
	if !waitUntil(d, ewArriveWait, func() bool { return ewRegion(d) == questplan.EwSecretRoom }) {
		logf(d, "workshop: the wall let the account nowhere (at %v)", questrunner.CurrentTile(d))
		return false
	}
	ewSettle(d)
	return true
}

// ewStairsDown climbs the spiral stairs in the secret room into the workshop.
func ewStairsDown(d *questrunner.Deps) bool {
	m, ok := ewLoc(d, questplan.LocEwStairsDown, "Climb-down", questplan.EwStairsDownAt, 3)
	if !ok {
		return false
	}
	logf(d, "workshop: down the spiral stairs")
	if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
		logf(d, "workshop: the stairs: %v", err)
		return false
	}
	if !waitUntil(d, ewArriveWait, func() bool { return ewRegion(d) == questplan.EwWorkshop }) {
		logf(d, "workshop: the stairs did not take the account down (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1500 * time.Millisecond) // the workshop's scene loads
	ewSettle(d)
	return true
}

// ewEnter takes the account into the workshop from wherever it stands: through the wall when outside, down the stairs from the secret
// room.
func ewEnter(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	switch ewRegion(d) {
	case questplan.EwWorkshop:
		return true
	case questplan.EwSurface:
		if !ewThroughWall(d) {
			return false
		}
	}
	return ewStairsDown(d)
}

// ewLeaveAll takes the account out of the workshop: the stairs up and the wall pushed from inside. It is the way out for the quest itself
// and, as the pocket's Leave, for every other quest's walks.
func ewLeaveAll(d *questrunner.Deps) bool {
	for round := 0; round < 3; round++ {
		switch ewRegion(d) {
		case questplan.EwSurface:
			return true
		case questplan.EwWorkshop:
			m, ok := ewLoc(d, questplan.LocEwStairsUp, "Climb-up", questplan.EwStairsUpAt, 3)
			if !ok {
				return false
			}
			logf(d, "workshop: up the spiral stairs")
			if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
				logf(d, "workshop: the stairs up: %v", err)
				return false
			}
			if !waitUntil(d, ewArriveWait, func() bool { return ewRegion(d) != questplan.EwWorkshop }) {
				logf(d, "workshop: the stairs up did not take the account out (at %v)", questrunner.CurrentTile(d))
				return false
			}
			ewSettle(d)
		case questplan.EwSecretRoom:
			m, ok := ewWall(d, "Push")
			if !ok {
				waitUntil(d, ewLeverReset, func() bool { _, again := ewWall(d, "Push"); return again })
				if m, ok = ewWall(d, "Push"); !ok {
					logf(d, "workshop: no Odd looking wall offering Push near %v", questplan.EwWallAt)
					return false
				}
			}
			if err := questrunner.OperateLoc(d, m, "Push"); err != nil {
				logf(d, "workshop: pushing the wall from inside: %v", err)
				return false
			}
			if !waitUntil(d, ewArriveWait, func() bool { return ewRegion(d) == questplan.EwSurface }) {
				logf(d, "workshop: the wall let the account out nowhere (at %v)", questrunner.CurrentTile(d))
				return false
			}
			ewSettle(d)
		}
	}
	return ewRegion(d) == questplan.EwSurface
}

// ewSupplies searches one of the workshop's crates for what the step names (its ItemID): the stone bowl (whenever the pack holds no empty
// one), the needle or the leather (once per account, ever).
func ewSupplies(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	box, at, item := questplan.LocEwBoxBowl, questplan.EwBoxBowlAt, step.ItemID
	switch item {
	case questplan.ObjEwNeedle:
		box, at = questplan.LocEwBoxNeedle, questplan.EwBoxNeedleAt
	case questplan.ObjEwLeather:
		box, at = questplan.LocEwBoxLeather, questplan.EwBoxLeatherAt
	default:
		item = questplan.ObjEwBowl
	}
	if questrunner.HeldID(d, item) > 0 {
		return true
	}
	m, ok := ewLoc(d, box, "Search", at, 3)
	if !ok {
		return false
	}
	mark := time.Now()
	logf(d, "workshop: searching the crate at %d,%d for %s", at.X, at.Z, questrunner.ObjName(d, item))
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "workshop: the crate: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait, func() bool { return questrunner.HeldID(d, item) > 0 || cookSaw(d, mark, "it's empty") })
	if questrunner.HeldID(d, item) == 0 {
		logf(d, "workshop: the crate gave no %s [%s]", questrunner.ObjName(d, item), huntSaid(d, mark))
		return false
	}
	return true
}

// ewTurn turns one of the water wheel's valves and reports "ok", "locked" (the wheel runs: "the valve seems locked off") or "failed".
func ewTurn(d *questrunner.Deps, id int, at tile.Tile) string {
	m, ok := ewLoc(d, id, "Turn", at, 3)
	if !ok {
		return "failed"
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Turn"); err != nil {
		logf(d, "workshop: the valve at %d,%d: %v", at.X, at.Z, err)
		return "failed"
	}
	waitUntil(d, ewMsgWait, func() bool { return cookSaw(d, mark, "you turn the handle", "locked off") })
	switch {
	case cookSaw(d, mark, "locked off"):
		return "locked"
	case cookSaw(d, mark, "you turn the handle"):
		d.Delay(1200 * time.Millisecond)
		return "ok"
	}
	logf(d, "workshop: the valve at %d,%d answered nothing", at.X, at.Z)
	return "failed"
}

// ewPull pulls a lever and reports what the chat said first of the needles, "" for nothing.
func ewPull(d *questrunner.Deps, id int, at tile.Tile, needles ...string) string {
	m, ok := ewLoc(d, id, "Pull", at, 3)
	if !ok {
		return ""
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Pull"); err != nil {
		logf(d, "workshop: the lever at %d,%d: %v", at.X, at.Z, err)
		return ""
	}
	waitUntil(d, ewMsgWait, func() bool { return cookSaw(d, mark, needles...) })
	for _, n := range needles {
		if cookSaw(d, mark, n) {
			return n
		}
	}
	return ""
}

// ewWater starts the water wheel: the first valve, the second valve, the lever. The first valve sets its bit only while the second is off
// and the lever starts the wheel only when both are set, otherwise it clears both, so a pass that ends in "the flow gates resetting" leaves
// the state the next pass works from. A lever that stops a wheel which already runs is pulled again.
func ewWater(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	const started, stopped, reset = "water wheel starting up", "coming to a standstill", "flow gates resetting"
	for pass := 1; pass <= ewWaters; pass++ {
		first := ewTurn(d, questplan.LocEwValveFirst, questplan.EwValveFirstAt)
		if first == "locked" {
			logf(d, "workshop: the valves are locked off: the wheel already runs")
			return true
		}
		if first != "ok" {
			return false
		}
		second := ewTurn(d, questplan.LocEwValveSecond, questplan.EwValveSecondAt)
		if second == "locked" {
			return true
		}
		if second != "ok" {
			return false
		}
		switch ewPull(d, questplan.LocEwWaterLever, questplan.EwWaterLeverAt, started, stopped, reset) {
		case started:
			logf(d, "workshop: the water wheel runs (pass %d)", pass)
			return true
		case stopped:
			// The wheel was running: the lever stopped it and the gates are still set, so the lever again starts it.
			d.Delay(ewLeverReset)
			if ewPull(d, questplan.LocEwWaterLever, questplan.EwWaterLeverAt, started, stopped, reset) == started {
				return true
			}
		case reset:
			logf(d, "workshop: the flow gates reset on pass %d: again from the clean state", pass)
			d.Delay(ewLeverReset)
		default:
			logf(d, "workshop: the water lever answered nothing")
			return false
		}
	}
	logf(d, "workshop: the water wheel did not start in %d passes", ewWaters)
	return false
}

// ewBellows stitches the leather over the bellows (Crafting 20, a needle, a leather and a thread; the leather and the thread are used up).
func ewBellows(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for _, id := range []int{questplan.ObjEwNeedle, questplan.ObjEwLeather, questplan.ObjEwThread} {
		if questrunner.HeldID(d, id) == 0 {
			logf(d, "workshop: no %s to mend the bellows with", questrunner.ObjName(d, id))
			return false
		}
	}
	m, ok := ewLoc(d, questplan.LocEwBellowsL, "Fix", questplan.EwBellowsAt, 3)
	if !ok {
		return false
	}
	mark := time.Now()
	logf(d, "workshop: stitching the leather over the bellows")
	if err := questrunner.OperateLoc(d, m, "Fix"); err != nil {
		logf(d, "workshop: the bellows: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait, func() bool {
		return cookSaw(d, mark, "you stitch the leather", "already look fixed", "crafting level of 20", "piece of leather")
	})
	switch {
	case cookSaw(d, mark, "you stitch the leather"), cookSaw(d, mark, "already look fixed"):
		return true
	case cookSaw(d, mark, "crafting level of 20"):
		e.RequestPark("Crafting is under 20: the bellows will not be mended", questplan.EwFightParkFor)
	}
	logf(d, "workshop: the bellows were not mended [%s]", huntSaid(d, mark))
	return false
}

// ewNearestLoc finds the nearest of several locs of an id (the lava trough has two, the workbench two) to the account, with no op wanted.
func ewNearestLoc(d *questrunner.Deps, id int, near tile.Tile, radius int) (locindex.Match, bool) {
	m, ok := questrunner.FindLoc(d, id, "", "", near, radius)
	if !ok {
		logf(d, "workshop: no loc %d near %d,%d,%d", id, near.X, near.Z, near.Level)
	}
	return m, ok
}

// ewLava lights the furnace: the empty stone bowl is filled at the lava trough and emptied on the furnace ("The furnace bursts in to
// life." four ticks later, once).
func ewLava(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwBowlFull) == 0 {
		if questrunner.HeldID(d, questplan.ObjEwBowl) == 0 {
			logf(d, "workshop: no stone bowl to fill")
			return false
		}
		trough, ok := ewNearestLoc(d, questplan.LocEwTrough, questplan.EwTroughAt, 4)
		if !ok {
			return false
		}
		logf(d, "workshop: filling the stone bowl at the lava trough")
		if err := questrunner.UseItemOnLoc(d, questplan.ObjEwBowl, trough); err != nil {
			logf(d, "workshop: the bowl on the trough: %v", err)
			return false
		}
		if !waitUntil(d, ewMsgWait, func() bool { return questrunner.HeldID(d, questplan.ObjEwBowlFull) > 0 }) {
			logf(d, "workshop: the trough filled no bowl")
			return false
		}
	}
	furnace, ok := ewNearestLoc(d, questplan.LocEwFurnace, questplan.EwFurnaceAt, 4)
	if !ok {
		return false
	}
	mark := time.Now()
	logf(d, "workshop: emptying the lava onto the furnace")
	if err := questrunner.UseItemOnLoc(d, questplan.ObjEwBowlFull, furnace); err != nil {
		logf(d, "workshop: the lava on the furnace: %v", err)
		return false
	}
	waitUntil(d, ewMsgWait+4*time.Second, func() bool {
		return cookSaw(d, mark, "bursts in to life", "makes little difference")
	})
	return questrunner.HeldID(d, questplan.ObjEwBowlFull) == 0 && cookSaw(d, mark, "bursts in to life", "makes little difference")
}

// ewEarth is the rock's earth elemental in view, if one stands: the one the Mine op set on the account (id 1023); the standing earth
// elementals of the chamber (id 1020) are never looked at.
func ewEarth(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(8).
		Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcEwEarthRock }).Nearest()
}

// ewGiveUp says why a fight with the earth elemental (cur: as it stands now), begun at began, is not worth going on with, "" when it is.
func ewGiveUp(cur entityquery.Entity, began, now time.Time) string {
	if now.Sub(began) >= ewNoHitAfter && cur.TotalHealth == 0 {
		return "no blow has landed on it"
	}
	return ""
}

// ewOre mines the Pile of Rock, kills the earth elemental the rock sets on the account and takes the ore it drops. The fight is the shared
// fight step's (the eat line from the foe's hardest hit, the top-up before the first blow, the race that gives up a fight being lost); the
// odds were judged by questplan.EwElementalReady before the account came to the rock, a fight lost anyway is a near-death the engine
// counts, and one death here parks the quest for a day (StepDeathCaps).
func ewOre(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwOre) > 0 {
		return true
	}
	if questplan.EwPickaxeFor(questrunner.PackCounts(d), questrunner.WornCounts(d), questrunner.BaseLevels(d)["mining"]) == 0 {
		logf(d, "workshop: no pickaxe the Mining level can use")
		return false
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "workshop: no free slot for the ore")
		return false
	}
	// An ore left on the floor by an earlier fight (a restart in the middle of one) is taken first.
	if items := questrunner.GroundItemsNear(d, questplan.ObjEwOre, questplan.EwRockAt, 14); len(items) > 0 {
		here := tile.Tile{X: items[0].X, Z: items[0].Z, Level: items[0].Level}
		if questrunner.TakeGround(d, questplan.ObjEwOre, here, 1) {
			return true
		}
	}
	m, ok := questrunner.FindLoc(d, questplan.LocEwRock, "", "Mine", questplan.EwRockAt, 4)
	if !ok {
		logf(d, "workshop: no Pile of Rock (loc %d) offering Mine near %v", questplan.LocEwRock, questplan.EwRockAt)
		return false
	}
	mark := time.Now()
	logf(d, "workshop: mining the Pile of Rock at %d,%d: its earth elemental comes", m.X, m.Z)
	if err := questrunner.OperateLoc(d, m, "Mine"); err != nil {
		logf(d, "workshop: the rock: %v", err)
		return false
	}
	var elemental entityquery.Entity
	if !waitUntil(d, ewSpawnWait, func() bool {
		var found bool
		elemental, found = ewEarth(d)
		return found
	}) {
		ewSettle(d) // "You do not have a pickaxe which you have the Mining level to use."
		logf(d, "workshop: the rock set no earth elemental on the account [%s]", huntSaid(d, mark))
		return false
	}
	began := time.Now()
	why := ""
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(12).
			Where(func(n entityquery.Entity) bool { return n.Index == elemental.Index && n.ID == elemental.ID }).Nearest()
		if !ok {
			return false
		}
		why = ewGiveUp(cur, began, time.Now())
		return why != ""
	}
	logf(d, "workshop: fighting the earth elemental")
	switch questrunner.FightChosen(d, elemental, questplan.ElementalWorkshop.Sustain, ewFightBudget, giveUp) {
	case questrunner.FightWon:
		logf(d, "workshop: the earth elemental is down")
	case questrunner.FightGaveUp:
		logf(d, "workshop: gave up on the earth elemental after %s: %s", time.Since(began).Round(time.Second), why)
		e.RequestPark("the earth elemental could not be hurt", time.Hour)
		return false
	case questrunner.FightTimedOut:
		logf(d, "workshop: the earth elemental is still standing after %s", ewFightBudget)
		e.RequestPark("the earth elemental was still standing after the fight's budget", time.Hour)
		return false
	default:
		return false // lost (the engine counts the near-death) or stopped
	}
	// The ore drops where the elemental fell, a tick or two after it goes (its death script adds the drops), for the account that did most
	// of the damage.
	here := questrunner.CurrentTile(d)
	if !waitUntil(d, ewMsgWait, func() bool { return len(questrunner.GroundItemsNear(d, questplan.ObjEwOre, here, 6)) > 0 }) {
		logf(d, "workshop: the earth elemental died and left no elemental ore within 6 tiles of %v", here)
		return false
	}
	return questrunner.TakeGround(d, questplan.ObjEwOre, here, 6) || questrunner.HeldID(d, questplan.ObjEwOre) > 0
}

// ewAir pulls the air lever and reports how the bellows stand: "on", "off" (a pump that was on is stopped), "nothing" (the wheel or the
// bellows are not ready: "Nothing happens; the lever resets itself.") or "" for no answer.
func ewAir(d *questrunner.Deps) string {
	switch ewPull(d, questplan.LocEwAirLever, questplan.EwAirLeverAt, "bellows pump air", "bellows stop pumping", "nothing happens") {
	case "bellows pump air":
		return "on"
	case "bellows stop pumping":
		return "off"
	case "nothing happens":
		return "nothing"
	}
	return ""
}

// ewSmelt puts the ore and four coal into the furnace. The air is not in the journal and the lever toggles, so the furnace is tried first
// and the lever pulled only when it asks for more heat ("The furnace needs to be hotter to be of any use.").
func ewSmelt(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwBar) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjEwOre) == 0 || questrunner.HeldID(d, questplan.ObjEwCoal) < questplan.EwCoalNeeded {
		logf(d, "workshop: the ore or the four coal are not in the pack")
		return false
	}
	furnace, ok := ewNearestLoc(d, questplan.LocEwFurnace, questplan.EwFurnaceAt, 4)
	if !ok {
		return false
	}
	for try := 1; try <= ewSmelts; try++ {
		mark := time.Now()
		logf(d, "workshop: the ore and four coal into the furnace")
		if err := questrunner.UseItemOnLoc(d, questplan.ObjEwOre, furnace); err != nil {
			logf(d, "workshop: the ore on the furnace: %v", err)
			return false
		}
		waitUntil(d, ewSmeltWait, func() bool {
			return questrunner.HeldID(d, questplan.ObjEwBar) > 0 ||
				cookSaw(d, mark, "needs to be hotter", "four heaps of coal", "cold and useless")
		})
		switch {
		case questrunner.HeldID(d, questplan.ObjEwBar) > 0:
			return true
		case cookSaw(d, mark, "needs to be hotter"):
			logf(d, "workshop: the furnace wants more heat: the air lever")
			switch ewAir(d) {
			case "on":
			case "off":
				// A pump that was on is stopped by the pull: once more to start it again.
				d.Delay(ewLeverReset)
				if ewAir(d) != "on" {
					return false
				}
			default:
				logf(d, "workshop: the air lever did not start the bellows (the wheel or the bellows are not ready)")
				return false
			}
		case cookSaw(d, mark, "four heaps of coal"):
			logf(d, "workshop: the furnace wants four coal")
			return false
		case cookSaw(d, mark, "cold and useless"):
			logf(d, "workshop: the furnace is cold: it is not lit")
			return false
		default:
			logf(d, "workshop: the furnace answered nothing [%s]", huntSaid(d, mark))
		}
	}
	return questrunner.HeldID(d, questplan.ObjEwBar) > 0
}

// ewShield works the bar into the shield on the workbench (a hammer, Smithing 20 and the Battered book in the pack): the quest is queued
// by the same script.
func ewShield(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjEwShield) > 0 {
		return true
	}
	for _, id := range []int{questplan.ObjEwBar, questplan.ObjHammer, questplan.ObjEwBook} {
		if questrunner.HeldID(d, id) == 0 {
			logf(d, "workshop: no %s for the workbench", questrunner.ObjName(d, id))
			return false
		}
	}
	bench, ok := ewNearestLoc(d, questplan.LocEwWorkbench, questplan.EwWorkbenchAt, 8)
	if !ok {
		return false
	}
	mark := time.Now()
	logf(d, "workshop: the bar onto the workbench")
	if err := questrunner.UseItemOnLoc(d, questplan.ObjEwBar, bench); err != nil {
		logf(d, "workshop: the bar on the workbench: %v", err)
		return false
	}
	complete := func() bool {
		return questrunner.HeldID(d, questplan.ObjEwShield) > 0 ||
			questrunner.ReadStatus(d, questplan.ElementalWorkshopName) == questplan.StatusComplete
	}
	if !waitUntil(d, ewSmeltWait+5*time.Second, complete) {
		ewSettle(d) // "This workbench is too complicated. You need instructions to follow."
		logf(d, "workshop: the workbench made no shield [%s]", huntSaid(d, mark))
		return false
	}
	ewSettle(d) // the quest scroll
	return true
}

// The workshop and the secret room are a pocket: the stairs and the wall are teleports the walk graph has no edge for.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Elemental Workshop", In: questplan.InEwPocket, Leave: ewLeaveAll})
}
