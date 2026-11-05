package questmods

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// KnightsSword is The Knight's Sword ready for the engine. Its talks, its withdrawals and its provisioning rounds are the engine's generic
// steps; the pie's rungs (the bucket at a sink, the dough and its menu, the shell, the berries, the range, the burnt dish), Sir Vyvin's
// cupboard and the visit to the Asgarnian Ice Dungeon are the nine executors below.
func KnightsSword() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.KnightsSword,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomSquireFill:     squireFill,
			questplan.CustomSquireDough:    squireDough,
			questplan.CustomSquireShell:    squireCombine,
			questplan.CustomSquireBerries:  squireCombine,
			questplan.CustomSquireCook:     squireCook,
			questplan.CustomSquireEmpty:    squireEmpty,
			questplan.CustomSquirePortrait: squirePortrait,
			questplan.CustomSquireBlurite:  squireBlurite,
			questplan.CustomSquireUnwield:  squireUnwield,
		},
	}
}

func squireHeld(d *questrunner.Deps, id int) bool { return questrunner.HeldID(d, id) > 0 }

// squireNearest orders tiles by their distance from the account.
func squireNearest[T any](from tile.Tile, items []T, at func(T) tile.Tile) []T {
	out := append([]T(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return tile.Chebyshev(from, at(out[i])) < tile.Chebyshev(from, at(out[j])) })
	return out
}

// squireFill fills the bucket in the pack at the water source nearest the account that works. The use is retried from a standstill: sent
// the moment the walk ends, it lands inside the arrival window and is dropped (monkJug's note).
func squireFill(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if squireHeld(d, questplan.ObjSqBucketWater) {
		return true
	}
	if !squireHeld(d, questplan.ObjSqBucket) {
		logf(d, "squire: no bucket in the pack to fill")
		return false
	}
	for _, spot := range squireNearest(questrunner.CurrentTile(d), questplan.SqWaterSpots, func(w questplan.SqWaterSpot) tile.Tile { return w.Loc }) {
		if !questrunner.Travel(d, spot.Loc, 3) {
			logf(d, "squire: %s (%v) was not reached from %v", spot.Name, spot.Loc, questrunner.CurrentTile(d))
			continue
		}
		var m locindex.Match
		found := false
		for _, id := range spot.IDs {
			if m, found = questrunner.FindLoc(d, id, "", "", spot.Loc, 4); found {
				break
			}
		}
		if !found {
			logf(d, "squire: no water source near %v", spot.Loc)
			continue
		}
		for try := 1; try <= 3; try++ {
			if err := questrunner.UseItemOnLoc(d, questplan.ObjSqBucket, m); err != nil {
				logf(d, "squire: the bucket on %s: %v", spot.Name, err)
				break
			}
			if waitUntil(d, 8*time.Second, func() bool { return squireHeld(d, questplan.ObjSqBucketWater) }) {
				logf(d, "squire: filled the bucket at %s", spot.Name)
				return true
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
	}
	return false
}

// squireDough uses the pot of flour on the bucket of water and answers the dough menu with the pastry dough (a bread, pizza or pitta dough
// would use the flour and the water for nothing). The menu is a chat dialogue; the result is the pastry dough in the pack.
func squireDough(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if squireHeld(d, questplan.ObjSqPastryDough) {
		return true
	}
	if err := questrunner.UseItemOnItem(d, questplan.ObjSqPotOfFlour, questplan.ObjSqBucketWater); err != nil {
		logf(d, "squire: the flour on the bucket of water: %v", err)
		return false
	}
	ok := questrunner.NewDialogue(d).DriveUntil(func() bool { return squireHeld(d, questplan.ObjSqPastryDough) }, []string{"Pastry dough"}, 15*time.Second)
	if !ok {
		questrunner.ClearLeftoverModal(d)
		logf(d, "squire: the dough menu gave no pastry dough")
	}
	return ok
}

// squireCombineWait is how long a use of one item on another is given to make its product (a variable so that a test need not wait it out).
var squireCombineWait = 6 * time.Second

// squireCombine uses one held item on another and waits for the product the step names: the pastry dough on the dish (a shell) and the
// redberries on the shell (an uncooked pie, which wants Cooking 10: the refusal is a message box, cleared).
func squireCombine(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	if step.ItemID == 0 || step.TargetID == 0 || step.ExpectItemID == 0 {
		logf(d, "squire: a combine step with no item, target or product: %s", step.Describe())
		return false
	}
	if squireHeld(d, step.ExpectItemID) {
		return true
	}
	for try := 1; try <= 3; try++ {
		if err := questrunner.UseItemOnItem(d, step.ItemID, step.TargetID); err != nil {
			logf(d, "squire: %s on %s: %v", questrunner.ObjName(d, step.ItemID), questrunner.ObjName(d, step.TargetID), err)
			return false
		}
		if waitUntil(d, squireCombineWait, func() bool { return squireHeld(d, step.ExpectItemID) }) {
			return true
		}
		questrunner.ClearLeftoverModal(d)
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	logf(d, "squire: %s on %s made no %s", questrunner.ObjName(d, step.ItemID), questrunner.ObjName(d, step.TargetID), questrunner.ObjName(d, step.ExpectItemID))
	return false
}

// squireCook uses the uncooked pie on the range nearest the account that works and waits for the pie or the burnt pie. A burnt pie is the
// step's success: the next decision empties the dish and begins a pie again.
func squireCook(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if squireHeld(d, questplan.ObjSqRedberryPie) || squireHeld(d, questplan.ObjSqBurntPie) {
		return true
	}
	if !squireHeld(d, questplan.ObjSqUncookedPie) {
		logf(d, "squire: no uncooked pie in the pack to cook")
		return false
	}
	done := func() bool {
		return squireHeld(d, questplan.ObjSqRedberryPie) || squireHeld(d, questplan.ObjSqBurntPie)
	}
	for _, spot := range squireNearest(questrunner.CurrentTile(d), questplan.SqRangeSpots, func(r questplan.SqRangeSpot) tile.Tile { return r.Loc }) {
		if !questrunner.Travel(d, spot.Loc, 3) {
			logf(d, "squire: %s (%v) was not reached from %v", spot.Name, spot.Loc, questrunner.CurrentTile(d))
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocSqRange, "", "", spot.Loc, 3)
		if !ok {
			logf(d, "squire: no range near %v", spot.Loc)
			continue
		}
		for try := 1; try <= 3; try++ {
			if err := questrunner.UseItemOnLoc(d, questplan.ObjSqUncookedPie, m); err != nil {
				logf(d, "squire: the pie on %s: %v", spot.Name, err)
				break
			}
			if waitUntil(d, 12*time.Second, done) {
				if squireHeld(d, questplan.ObjSqRedberryPie) {
					logf(d, "squire: the pie came out of %s", spot.Name)
				} else {
					logf(d, "squire: the pie burnt on %s", spot.Name)
				}
				return true
			}
			questrunner.ClearLeftoverModal(d)
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
	}
	return false
}

// squireEmpty empties the burnt pie: the dish comes back, the dough and the berries do not.
func squireEmpty(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !squireHeld(d, questplan.ObjSqBurntPie) {
		return true
	}
	if err := questrunner.OpHeldByName(d, questplan.ObjSqBurntPie, "Empty Dish"); err != nil {
		logf(d, "squire: emptying the burnt pie: %v", err)
		return false
	}
	return waitUntil(d, 8*time.Second, func() bool { return squireHeld(d, questplan.ObjSqPieDish) })
}

// squireUnwield takes the sword off: the Squire wants it in the pack, and says so ("un-equip it and hand it over").
func squireUnwield(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.WornCounts(d)[questplan.ObjSqSword] == 0 {
		return true
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.UnequipAndWait(int32(questplan.ObjSqSword), actions.DelayWaiter(d.Delay)); err != nil {
		logf(d, "squire: taking the sword off: %v", err)
		return false
	}
	return squireHeld(d, questplan.ObjSqSword)
}

// ---------------------------------------------------------------------------------------------------------
// Sir Vyvin's cupboard.
// ---------------------------------------------------------------------------------------------------------

// squireVyvin is Sir Vyvin's tile, when he is in view on the account's floor.
func squireVyvin(d *questrunner.Deps) (tile.Tile, bool) {
	npc, ok := questrunner.FindNpcByID(d, questplan.NpcVyvin, "", 14)
	if !ok {
		return tile.Tile{}, false
	}
	return tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, true
}

// squireDismiss continues the dialogue and the message box a refused search leaves up; they have to go before the next click.
func squireDismiss(d *questrunner.Deps) {
	dl := questrunner.NewDialogue(d)
	if dl.Ready() {
		dl.Drive(nil)
	}
	questrunner.ClearLeftoverModal(d)
}

// squirePortraitWait is how long a search is given to hand over the portrait while its message box is continued (a variable so that a test need
// not wait it out).
var squirePortraitWait = 6 * time.Second

// squirePortrait opens Sir Vyvin's cupboard and searches it while he is not beside it. The cupboard is operable from the two tiles south of
// it only (the room's other tiles drop the op in silence), the one farthest from Sir Vyvin is the one used, a search with him within a tile
// is refused by a dialogue ("HEY! Just WHAT do you THINK you are DOING???"), and he wanders ten tiles round a room barely wider, so the
// account waits a few passes for him to move off, and after three refusals steps away to give him room.
func squirePortrait(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if squireHeld(d, questplan.ObjSqPortrait) {
		return true
	}
	if !questrunner.Travel(d, questplan.SqVyvinApproaches[0], 1) {
		logf(d, "squire: Sir Vyvin's room (%v) was not reached from %v", questplan.SqVyvinApproaches[0], questrunner.CurrentTile(d))
		return false
	}
	skips, refusals := 0, 0
	for pass := 0; pass < questplan.SqPortraitAttempts; pass++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if squireHeld(d, questplan.ObjSqPortrait) {
			return true
		}
		if refusals >= questplan.SqRefusalsBeforeRetreat {
			refusals = 0
			logf(d, "squire: stepping away to give Sir Vyvin room to wander")
			questrunner.Travel(d, questplan.SqVyvinRetreat, 1)
			if !d.Delay(6 * time.Second) {
				return false
			}
		}
		vyvin, known := squireVyvin(d)
		stand := questplan.SqBestApproach(vyvin, known)
		if here := questrunner.CurrentTile(d); here != stand {
			questrunner.WalkNear(d, stand, 0)
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
		squireDismiss(d)
		if m, ok := questrunner.FindLoc(d, questplan.LocSqCupboardShut, "", "Open", questplan.SqCupboardAt, 4); ok {
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "squire: opening the cupboard: %v", err)
			}
			if !d.Delay(1800 * time.Millisecond) {
				return false
			}
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocSqCupboardOpen, "", "Search", questplan.SqCupboardAt, 4)
		if !ok {
			logf(d, "squire: no cupboard in reach of the stand %v", stand)
			if !d.Delay(1800 * time.Millisecond) {
				return false
			}
			continue
		}
		here := questrunner.CurrentTile(d)
		vyvin, known = squireVyvin(d)
		if questplan.SqShouldWaitOut(skips, here, vyvin, known) {
			skips++
			if !d.Delay(1800 * time.Millisecond) {
				return false
			}
			continue
		}
		skips = 0
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			logf(d, "squire: searching the cupboard: %v", err)
		}
		// The portrait is added AFTER the message box ("You find a small portrait in here which you take.") is continued
		// (cupboards.rs2 search_cupboard: ~mesbox, then inv_add), and a refusal is a dialogue and a message box: both are continued, never
		// closed, and the portrait is waited for as the dialogue is driven.
		if questrunner.NewDialogue(d).DriveUntil(func() bool { return squireHeld(d, questplan.ObjSqPortrait) }, nil, squirePortraitWait) {
			logf(d, "squire: the portrait is in the pack")
			return true
		}
		squireDismiss(d)
		refusals++
		beside := ""
		if known && questplan.SqVyvinTooClose(here, vyvin) {
			beside = ", Sir Vyvin beside it"
		}
		logf(d, "squire: the search did not land (pass %d, at %v%s)", pass, here, beside)
		if !d.Delay(2400 * time.Millisecond) {
			return false
		}
	}
	logf(d, "squire: never got a clear look at the cupboard in %d passes", questplan.SqPortraitAttempts)
	return false
}

// ---------------------------------------------------------------------------------------------------------
// The Asgarnian Ice Dungeon.
// ---------------------------------------------------------------------------------------------------------

const (
	// squireWalkBudget bounds each leg of the visit: the surface to the ladder, the ladder to the stand, the stand to the ladder.
	squireWalkBudget = 5 * time.Minute
	// squireClickGap is how long the Mine click is left alone: the swing goes on by itself, and a bite eaten clears it.
	squireClickGap = 6 * time.Second
	// squireTopUpBites is the most bites eaten to reach the start line on the surface.
	squireTopUpBites = 8
)

// squireFoodHeal is the hitpoints of food in the pack.
func squireFoodHeal(d *questrunner.Deps) int {
	return questplan.FoodPoolHeal(questrunner.PackCounts(d), questplan.SqFoodIDs())
}

// squireHurt is the break-off test for a walk: under a third of the hitpoints with nothing left to eat.
func squireHurt(d *questrunner.Deps) bool {
	hp, max := questrunner.Hitpoints(d)
	return questplan.SqBreakOff(hp, max, squireFoodHeal(d))
}

// squireTopUp eats on the surface until the account is at the hitpoints it goes down with.
func squireTopUp(d *questrunner.Deps) {
	for bite := 0; bite < squireTopUpBites; bite++ {
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || hp >= questplan.SqStartHP(max) {
			return
		}
		if !questrunner.EatIfNeededVs(d, questplan.SqFoodIDs(), hp, 0) {
			return
		}
	}
}

// squireLeave walks out of the dungeon by its ladder, to the tile beside the ladder's top.
func squireLeave(d *questrunner.Deps) bool {
	if !questplan.InSquireDungeon(questrunner.CurrentTile(d)) {
		return true
	}
	if questrunner.TravelUntil(d, questplan.SqLadderTop, 1, squireWalkBudget, nil) {
		logf(d, "squire: up the ladder, out of the Asgarnian Ice Dungeon")
		return true
	}
	logf(d, "squire: the ladder's top (%v) was not reached from %v", questplan.SqLadderTop, questrunner.CurrentTile(d))
	return false
}

// squireRock is the east rock that is not mined out, the stand's own first (a mined-out rock is another loc until it comes back).
func squireRock(d *questrunner.Deps, st questplan.SqStand) (locindex.Match, bool) {
	if m, ok := questrunner.FindLoc(d, questplan.LocSqBluriteRock, "", "Mine", st.Rock, 1); ok {
		return m, true
	}
	for _, other := range questplan.SqStands {
		if other.Rock == st.Rock {
			continue
		}
		if m, ok := questrunner.FindLoc(d, questplan.LocSqBluriteRock, "", "Mine", other.Rock, 1); ok {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// squireMine mines at the stand until the pack holds the ore, the time the visit is priced for has passed, or the account is hurt with
// nothing to eat. The account never strikes (the module's auto-retaliate is off); it eats beside the swing, and clicks the rock again
// when a bite or a miss has stopped it. It reports whether the ore is in the pack.
func squireMine(d *questrunner.Deps, st questplan.SqStand, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	var lastClick time.Time
	peak := questplan.FoeIceWarrior.PeakHit()
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if squireHeld(d, questplan.ObjSqBluriteOre) {
			return true
		}
		if squireHurt(d) {
			hp, max := questrunner.Hitpoints(d)
			logf(d, "squire: %d of %d hitpoints and nothing to eat: leaving the rock", hp, max)
			return false
		}
		if questrunner.EatIfNeededVs(d, questplan.SqFoodIDs(), questplan.SqEatAt, peak) {
			lastClick = time.Time{} // a bite stops the swing
		}
		if time.Since(lastClick) >= squireClickGap {
			rock, ok := squireRock(d, st)
			if !ok {
				// Both rocks are mined out (another account's ore): they come back in eighty ticks.
				d.Delay(1200 * time.Millisecond)
				continue
			}
			if err := questrunner.OperateLoc(d, rock, "Mine"); err != nil {
				logf(d, "squire: mining the rock at %d,%d: %v", rock.X, rock.Z, err)
				d.Delay(1200 * time.Millisecond)
				continue
			}
			lastClick = time.Now()
		}
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
	}
	logf(d, "squire: no blurite ore in %s at the rock", budget.Round(time.Second))
	return false
}

// squireBlurite is one visit to the Asgarnian Ice Dungeon for one blurite ore: the food checked against the price and the account topped up
// to the hitpoints it goes down with on the surface, the ladder, the shortest walk to the east rocks (the walk breaks off if the account is
// hurt with nothing to eat), the ore, and the way out. It reports whether the ore is in the pack. An account found in the dungeon (a restart
// mid-visit) takes up the visit where it stands.
func squireBlurite(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if squireHeld(d, questplan.ObjSqBluriteOre) {
		squireLeave(d)
		return true
	}
	snap := e.Snapshot(0)
	trip := questplan.SquireTripPrice(snap)
	inside := questplan.InSquireDungeon(questrunner.CurrentTile(d))
	if !inside {
		if !trip.OK {
			logf(d, "squire: %s", trip.Why)
			return false
		}
		if questplan.DoricUsablePickaxe(snap) == 0 {
			logf(d, "squire: no pickaxe in the pack that Mining %d can use", snap.Level("mining"))
			return false
		}
		// The top-up comes first: the bites it eats are food the visit does not have, so the pack is measured after it.
		squireTopUp(d)
		if held := squireFoodHeal(d); held < trip.FoodWanted {
			logf(d, "squire: %d hitpoints of food in the pack after the top-up and the visit wants %d (priced at %.0f hitpoints taken)", held, trip.FoodWanted, trip.Damage)
			return false
		}
		if !questrunner.TravelCautious(d, questplan.SqLadderTop, 1) {
			logf(d, "squire: the ladder's top (%v) was not reached from %v", questplan.SqLadderTop, questrunner.CurrentTile(d))
			return false
		}
		logf(d, "squire: down into the Asgarnian Ice Dungeon with %d hitpoints of food (priced at %.0f taken: %d held tiles each way and %d ticks at the rock, %.1f a warrior's attack)",
			squireFoodHeal(d), trip.Damage, trip.WalkTicks, trip.MineTicks, trip.PerAttack)
	}
	st := questplan.SqStands[0]
	if !questrunner.TravelUntil(d, st.Stand, 0, squireWalkBudget, func() bool { return squireHurt(d) }) {
		logf(d, "squire: the rock's stand (%v) was not reached from %v", st.Stand, questrunner.CurrentTile(d))
		squireLeave(d)
		return squireHeld(d, questplan.ObjSqBluriteOre)
	}
	budget := time.Duration(trip.MineTicks)*600*time.Millisecond + 15*time.Second
	got := squireMine(d, st, budget)
	squireLeave(d)
	return got || squireHeld(d, questplan.ObjSqBluriteOre)
}
