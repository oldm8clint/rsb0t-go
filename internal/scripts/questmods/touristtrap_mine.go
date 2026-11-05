package questmods

import (
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The mines: the guarded cave, the barrels, the carts, Ana, the lift, the winch and the cart out. The content's hidden stages 20 to 26 are
// %desertrescue values and bits the client never sees; every leg here is therefore checked by what the pack holds and where the account stands.

// ttSlaveWorn reports the whole slave outfit on.
func ttSlaveWorn(d *questrunner.Deps) bool { return ttAllWorn(questrunner.WornCounts(d), ttSlaveIDs) }

// ttCaveCross takes the guarded cave: in (the hall to the lower mine, 0_51_147_22_7) or out (0_51_147_14_7). It wants slave clothes, and going out
// it must not carry Ana in her barrel: a guard within six tiles takes the barrel and throws the account in a cell.
func ttCaveCross(d *questrunner.Deps, in bool) bool {
	want, from, stand, near, id := questplan.TtAreaLower, questplan.TtAreaHall, tile.Tile{X: 3280, Z: 9415}, tile.Tile{X: 3281, Z: 9415}, questplan.LocTtCaveIn
	if !in {
		want, from, stand, near, id = questplan.TtAreaHall, questplan.TtAreaLower, tile.Tile{X: 3284, Z: 9415}, tile.Tile{X: 3283, Z: 9415}, questplan.LocTtCaveOut
	}
	if ttAreaNow(d) == want {
		return true
	}
	if ttAreaNow(d) != from {
		logf(d, "tourist trap: the cave is crossed from %d, not from %v", from, ttHere(d))
		return false
	}
	if !ttSlaveWorn(d) {
		logf(d, "tourist trap: the guarded cave lets slaves through: the guards would search the account")
		return false
	}
	if !in && questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		logf(d, "tourist trap: not through the cave with Ana in her barrel: the guards take it")
		return false
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if !ttTravelRound(d, stand, 0) {
			logf(d, "tourist trap: the cave's stand %v was not reached from %v", stand, ttHere(d))
			continue
		}
		if !ttOperate(d, "the guarded cave", []int{id}, "Walk through", near, 3) {
			continue
		}
		driveScript(d, nil, func() bool { return ttAreaNow(d) == want }, 30*time.Second)
		ttSettle(d)
		if ttAreaNow(d) == want || waitUntil(d, ttArriveWait, func() bool { return ttAreaNow(d) == want }) {
			d.Delay(1200 * time.Millisecond) // the scene loads
			return true
		}
		logf(d, "tourist trap: the cave did not take the account across (attempt %d of 3, at %v)", attempt, ttHere(d))
	}
	return false
}

func ttCaveStep(e *questrunner.Engine, step questplan.Step) bool {
	defer e.InvalidateStage()
	return ttCaveCross(e.Deps(), step.Qty == 1)
}

// ttBarrelStep takes an empty barrel (Qty 1: "Yeah, cool!", the barrel is taken off the loc for 100 ticks, so the barrels by the cart are tried in
// turn) or looks among the barrels beside the lower cart for the one Ana is in ("You search the barrels and find Ana.": the content gives her only to
// an account whose cart ride put her there, and offers an ordinary barrel to every other, which "No thanks." leaves alone).
func ttBarrelStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if ttAreaNow(d) != questplan.TtAreaLower {
		logf(d, "tourist trap: the barrels are in the lower mine, not at %v", ttHere(d))
		return false
	}
	if step.Qty == 1 {
		return ttTakeBarrel(d)
	}
	return ttProbeBarrels(d)
}

// ttBarrelsNear lists the empty barrels within r of a tile, nearest first.
func ttBarrelsNear(d *questrunner.Deps, near tile.Tile, r int) []locindex.Match {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Find(d.Config, locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: r, IDs: []int{questplan.LocTtEmptyBarrel}, Op: "Search"}, live)
}

func ttTakeBarrel(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjTtBarrel) > 0 || questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 1 && !ttMakeRoom(d, 1) {
		logf(d, "tourist trap: no room for a barrel")
		return false
	}
	have := func() bool {
		return questrunner.HeldID(d, questplan.ObjTtBarrel) > 0 || questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0
	}
	for i, m := range ttBarrelsNear(d, questplan.TtLowerBarrel, 5) {
		if i >= 4 {
			break
		}
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			logf(d, "tourist trap: the barrel at %d,%d: %v", m.X, m.Z, err)
			continue
		}
		driveScript(d, []string{"Yeah, cool!"}, have, 40*time.Second)
		ttSettle(d)
		if have() {
			logf(d, "tourist trap: an empty barrel is in the pack")
			return true
		}
	}
	logf(d, "tourist trap: no barrel could be taken near %v", questplan.TtLowerBarrel)
	return false
}

func ttProbeBarrels(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 1 && !ttMakeRoom(d, 1) {
		logf(d, "tourist trap: no room for Ana in her barrel")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocTtEmptyBarrel, "", "Search", questplan.TtLowerBarrel, 2)
	if !ok {
		logf(d, "tourist trap: no barrel at %v", questplan.TtLowerBarrel)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "tourist trap: the barrel: %v", err)
		return false
	}
	found := func() bool {
		return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 || ttSaw(d, mark, "decide not to take the barrel", "plenty of barrels")
	}
	driveScript(d, []string{"No thanks."}, found, 40*time.Second)
	ttSettle(d)
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		logf(d, "tourist trap: Ana is found in the barrels by the lower cart")
		return true
	}
	logf(d, "tourist trap: Ana is not among the lower cart's barrels")
	return false
}

// ttCartRide squeezes into a mine cart (Search, "Yes, of course.", an Agility roll: 39% at Agility 1, a miss costs 2 hitpoints and the cart goes
// without the account) and rides it. up: from the lower cart to the deep mine, else back.
func ttCartRide(d *questrunner.Deps, up bool) bool {
	from, to, near := questplan.TtAreaLower, questplan.TtAreaDeep, questplan.TtLowerCart
	if !up {
		from, to, near = questplan.TtAreaDeep, questplan.TtAreaLower, questplan.TtDeepCart
	}
	if ttAreaNow(d) == to {
		return true
	}
	if ttAreaNow(d) != from {
		logf(d, "tourist trap: this cart is ridden from %d, not from %v", from, ttHere(d))
		return false
	}
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		logf(d, "tourist trap: no room in the cart for the account and Ana's barrel")
		return false
	}
	for attempt := 1; attempt <= 12; attempt++ {
		questrunner.EatIfNeeded(d, questplan.TouristTrap.Sustain.FoodIDs, 12)
		// A miss deals 2 and can kill: the squeeze waits until the hitpoints are above the fall and a margin.
		if !ttRestAbove(d, ttCartDamage+ttHPMargin, ttRestLimit) {
			logf(d, "tourist trap: not squeezing into the cart: the hitpoints stay under %d", ttCartDamage+ttHPMargin+1)
			return false
		}
		mark := time.Now()
		if !ttOperate(d, "the mine cart", []int{questplan.LocTtMineCart}, "Search", near, 3) {
			return false
		}
		arrived := func() bool { return ttAreaNow(d) == to || ttSaw(d, mark, "fail to fit yourself") }
		driveScript(d, []string{"Yes, of course."}, arrived, 40*time.Second)
		ttSettle(d)
		if ttAreaNow(d) == to || waitUntil(d, ttArriveWait, func() bool { return ttAreaNow(d) == to }) {
			d.Delay(1200 * time.Millisecond)
			logf(d, "tourist trap: the cart carried the account (attempt %d)", attempt)
			return true
		}
		logf(d, "tourist trap: the Agility roll failed (attempt %d of 12)", attempt)
	}
	return false
}

func ttCartStep(e *questrunner.Engine, step questplan.Step) bool {
	defer e.InvalidateStage()
	ok := ttCartRide(e.Deps(), step.Qty == 1)
	if !ok {
		ttWeakPark(e, ttCartDamage+ttHPMargin, "the mine cart")
	}
	return ok
}

// ttCatchStep puts the empty barrel on Ana ([opnpcu,ana] with the barrel: "You manage to squeeze Ana into the barrel"; it clears every stale
// transport bit, so it is also the replay's reset).
func ttCatchStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjTtBarrel) == 0 {
		logf(d, "tourist trap: no empty barrel for Ana")
		return false
	}
	if ttAreaNow(d) != questplan.TtAreaDeep {
		logf(d, "tourist trap: Ana is in the deep mine, not at %v", ttHere(d))
		return false
	}
	if !ttTravelRound(d, questplan.TtAnaAt, 2) {
		logf(d, "tourist trap: Ana at %v was not reached from %v", questplan.TtAnaAt, ttHere(d))
		return false
	}
	ana, ok := questrunner.FindNpcByID(d, questplan.NpcTtAna, "", 14)
	if !ok {
		logf(d, "tourist trap: no Ana within 14 tiles of %v", ttHere(d))
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.ObjTtBarrel, ana); err != nil {
		logf(d, "tourist trap: the barrel on Ana: %v", err)
		return false
	}
	have := func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 }
	driveScript(d, nil, have, 90*time.Second)
	ttSettle(d)
	if have() {
		logf(d, "tourist trap: Ana is in the barrel")
	}
	return have()
}

// ttDeepStep is the deep mine's checkpoint: Ana's barrel put in the deep cart (which sends the cart's barrels to the lower mine and clears the
// barrel from the pack), then the ride to the lower mine, where the barrels are searched for her.
func ttDeepStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if ttAreaNow(d) != questplan.TtAreaDeep {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		m, ok := questrunner.FindLoc(d, questplan.LocTtMineCart, "", "Search", questplan.TtDeepCart, 3)
		if !ok {
			logf(d, "tourist trap: no deep cart near %v", questplan.TtDeepCart)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjTtAnaBarrel, m); err != nil {
			logf(d, "tourist trap: Ana's barrel on the deep cart: %v", err)
			return false
		}
		gone := func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) == 0 }
		driveScript(d, nil, gone, 45*time.Second)
		ttSettle(d)
		if !gone() {
			logf(d, "tourist trap: the deep cart kept Ana's barrel out of its load")
			return false
		}
	}
	ok := ttCartRide(d, false)
	if !ok {
		ttWeakPark(e, ttCartDamage+ttHPMargin, "the mine cart")
	}
	return ok
}

// ttLiftStep puts Ana's barrel on the lift under the winch guard's eye: the loc's use-on script runs only with a guard within five tiles, who
// offers a hand ("Yes please.") and is told "I said you were very gregarious!" when he hears her.
func ttLiftStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) == 0 {
		return true
	}
	if ttAreaNow(d) != questplan.TtAreaLower {
		logf(d, "tourist trap: the lift is in the lower mine, not at %v", ttHere(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocTtLiftBucket, "", "Use", questplan.TtLiftAt, 3)
	if !ok {
		logf(d, "tourist trap: no winch bucket near %v", questplan.TtLiftAt)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjTtAnaBarrel, m); err != nil {
		logf(d, "tourist trap: Ana's barrel on the lift: %v", err)
		return false
	}
	gone := func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) == 0 }
	driveScript(d, questplan.TtLiftGuardAnswers, gone, 75*time.Second)
	ttSettle(d)
	if gone() {
		logf(d, "tourist trap: Ana is on the lift")
	}
	return gone()
}

// ttWinchStep pulls the surface winch and searches the full barrel at the top for Ana ("Get me OUT OF HERE!").
func ttWinchStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		return true
	}
	if !ttEnsureSurface(d) || ttAreaNow(d) != questplan.TtAreaCamp {
		logf(d, "tourist trap: the winch is in the camp's yard, not at %v", ttHere(d))
		return false
	}
	if questrunner.FreeSlots(d) < 1 && !ttMakeRoom(d, 1) {
		logf(d, "tourist trap: no room for Ana in her barrel")
		return false
	}
	if !ttTravelRound(d, tile.Tile{X: 3280, Z: 3018}, 0) {
		logf(d, "tourist trap: the winch's stand was not reached from %v", ttHere(d))
		return false
	}
	if !ttOperate(d, "the surface winch", []int{questplan.LocTtWinch}, "Use", questplan.TtWinchAt, 3) {
		return false
	}
	driveScript(d, nil, nil, 20*time.Second)
	ttSettle(d)
	if !ttOperate(d, "the full barrel at the top", []int{questplan.LocTtFullBarrel}, "Search", questplan.TtTopBarrel, 1) {
		return false
	}
	have := func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 }
	driveScript(d, nil, have, 45*time.Second)
	ttSettle(d)
	if have() {
		logf(d, "tourist trap: Ana came up the winch")
	}
	return have()
}

// ttEscapeStep puts Ana's barrel on the surface cart, bribes the driver (100 coins; with fewer, the prison riot appeal) and rides it out:
// "Yes, I'll get on." sets the account down in the desert at (3258,3029) with Ana in a barrel again. The driver's chat sets the ready bit only
// when it ends, so it is driven to its end, never closed.
func ttEscapeStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !ttEnsureSurface(d) || ttAreaNow(d) != questplan.TtAreaCamp {
		logf(d, "tourist trap: the cart is in the camp's yard, not at %v", ttHere(d))
		return false
	}
	out := func() bool { return ttAreaNow(d).ZoneOf() == questplan.TtZoneOpen }
	board := func() bool {
		if !ttOperate(d, "the surface cart", []int{questplan.LocTtSurfaceCart}, "Search", questplan.TtSurfaceCart, 3) {
			return false
		}
		driveScript(d, []string{"Yes, I'll get on."}, out, 30*time.Second)
		ttSettle(d)
		return out() || waitUntil(d, 10*time.Second, out)
	}
	for round := 1; round <= 3; round++ {
		if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
			m, ok := questrunner.FindLoc(d, questplan.LocTtSurfaceCart, "", "Search", questplan.TtSurfaceCart, 3)
			if !ok {
				return false
			}
			if err := questrunner.UseItemOnLoc(d, questplan.ObjTtAnaBarrel, m); err != nil {
				logf(d, "tourist trap: Ana's barrel on the cart: %v", err)
				return false
			}
			gone := func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) == 0 }
			driveScript(d, nil, gone, 45*time.Second)
			ttSettle(d)
			if !gone() {
				logf(d, "tourist trap: the cart took no barrel")
				return false
			}
		}
		// A reload can land after the driver was paid and before the cart was boarded: the ready bit is set, and the cart takes the account.
		if board() {
			return ttCartArrived(d)
		}
		prefer := questplan.TtDriverStop.Prefer
		coins := questrunner.HeldID(d, questplan.CoinsID)
		if coins < 100 {
			prefer = questplan.TtDriverRiotAnswers
		}
		stop := questplan.TtDriverStop
		if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
			logf(d, "tourist trap: the cart driver opened no conversation (round %d of 3)", round)
			continue
		}
		driveScript(d, prefer, nil, 3*time.Minute)
		ttSettle(d)
		if coins >= 100 && questrunner.HeldID(d, questplan.CoinsID) != coins-100 {
			logf(d, "tourist trap: the driver took no 100 coins (%d before, %d after)", coins, questrunner.HeldID(d, questplan.CoinsID))
		}
		d.Delay(1200 * time.Millisecond)
		if board() {
			return ttCartArrived(d)
		}
		logf(d, "tourist trap: the cart did not take the account (round %d of 3)", round)
	}
	return false
}

// ttCartArrived confirms the ride: the account is in the desert and Ana is in its pack.
func ttCartArrived(d *questrunner.Deps) bool {
	if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 }) {
		logf(d, "tourist trap: the cart ride gave the account no Ana in a barrel")
		return false
	}
	logf(d, "tourist trap: out of the camp on the cart with Ana, at %v", ttHere(d))
	return true
}

// ttRecaptureStep is the canonical replay: the lift or the cart no longer holds Ana (a guard took her, the account was caught, the bits are stale), so
// the account goes back down with a new barrel, catches her again (which clears every transport bit) and runs the checkpoints from the top, to the
// winch: its success leaves Ana in the pack at the camp's yard. The engine keeps a failure flag for the whole run (it clears one only when that same
// step later succeeds), so a plan that waited for the winch's or the cart's own step to be retried after the replay would see the old flag and replay
// again for ever: the replay ends with the winch, and a replay that succeeds forgets the winch's and the cart's flags, so that what fails after it is a
// new failure (questplan.ttRescueCamp parks on that).
func ttRecaptureStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	logf(d, "tourist trap: the replay: back down for a new barrel and Ana")
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) > 0 {
		logf(d, "tourist trap: the replay starts without Ana in the pack")
		return false
	}
	if !ttEnsureSurface(d) {
		return false
	}
	steps := []struct {
		what string
		run  func() bool
	}{
		{"the slave's clothes", func() bool { return ttSlaveWorn(d) || ttDressApply(d, e, questplan.TtDressSlave) }},
		{"the mine doors", func() bool { return ttMineIn(d) }},
		{"the guarded cave", func() bool { return ttCaveCross(d, true) }},
		{"a new barrel", func() bool { return ttTakeBarrel(d) }},
		{"the cart down", func() bool { return ttCartRide(d, true) }},
		{"Ana", func() bool { return ttCatchStep(e, questplan.Step{}) }},
		{"the deep cart", func() bool { return ttDeepStep(e, questplan.Step{}) }},
		{"the barrels", func() bool { return ttProbeBarrels(d) }},
		{"the lift", func() bool { return ttLiftStep(e, questplan.Step{}) }},
		{"the cave back", func() bool { return ttCaveCross(d, false) }},
		{"the exit doors", func() bool { return ttMineOut(d) }},
		{"the winch", func() bool { return ttWinchStep(e, questplan.Step{}) }},
	}
	for _, s := range steps {
		if ttAreaNow(d).ZoneOf() == questplan.TtZoneCamp && s.what == "the slave's clothes" && ttSlaveWorn(d) {
			continue
		}
		if !s.run() {
			logf(d, "tourist trap: the replay stopped at %s (at %v)", s.what, ttHere(d))
			ttWeakPark(e, ttCartDamage+ttHPMargin, "the mine cart")
			return false
		}
	}
	if questrunner.HeldID(d, questplan.ObjTtAnaBarrel) == 0 {
		return false
	}
	e.ForgetFailure(questplan.CustomTtWinch, questplan.CustomTtEscape)
	return true
}

// ---------------------------------------------------------------------------------------------------------
// The pocket's way out.
// ---------------------------------------------------------------------------------------------------------

// ttLeaveAll takes the account out of the camp and the mines to the open desert, whatever part of them it stands in: the cells' ways out, the
// carts and the cave and the doors back, the gate. It is the pocket's Leave, so a walk that starts inside and ends outside runs it first.
func ttLeaveAll(d *questrunner.Deps) bool {
	for round := 0; round < 16; round++ {
		a := ttAreaNow(d)
		if !ttInPocket(ttHere(d)) {
			return true
		}
		ok := false
		switch a {
		case questplan.TtAreaDeep:
			ok = ttCartRide(d, false)
		case questplan.TtAreaLower:
			ok = ttCaveCross(d, false)
		case questplan.TtAreaHall:
			ok = ttMineOut(d)
		case questplan.TtAreaMineCell:
			ok = ttJailMine(d)
		case questplan.TtAreaSurfaceCell:
			ok = ttJailCell(d)
		case questplan.TtAreaTent:
			ok = ttTentExit(d)
		case questplan.TtAreaCamp, questplan.TtAreaCampUpper:
			if questrunner.HeldID(d, questplan.ObjTtMetalKey) == 0 {
				ok = false
				break
			}
			if ttAnyWorn(questrunner.WornCounts(d), ttSlaveIDs) {
				ttDressApply(d, nil, questplan.TtDressPlain)
			}
			ok = ttGateOut(d)
		}
		if !ok {
			logf(d, "tourist trap: leaving the camp and the mines stopped at %v", ttHere(d))
			return false
		}
	}
	return !ttInPocket(ttHere(d))
}
