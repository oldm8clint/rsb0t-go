package questmods

import (
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The camp: the captain, the gate, Siad's desk and the slave, the mine doors, and the two cells the guards put an account in.

// ---------------------------------------------------------------------------------------------------------
// The captain.
// ---------------------------------------------------------------------------------------------------------

// ttCaptainAttacker reports the captain when he is the one fighting the account. A cold Attack on him asks "are you sure" and the answer yes has the
// guards throw the account into the desert and its water away, so no attack is clicked until this holds.
func ttCaptainAttacker(d *questrunner.Deps) (entityquery.Entity, bool) {
	npc, ok := questrunner.Attacker(d)
	if ok && npc.ID == questplan.NpcTtCaptain {
		return npc, true
	}
	return entityquery.Entity{}, false
}

// ttCaptainStep taunts the Mercenary Captain ("It's a funny captain who can't fight his own battles!" ends in npc_retaliate with the guards standing
// by), fights him with the shared fight loop and waits for the Metal key he drops. The fight is priced by the plan (TtCaptainFit) before this runs.
func ttCaptainStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stop := questplan.TtCaptainStop
	npc, attacked := ttCaptainAttacker(d)
	if !attacked {
		if !questrunner.GotoNpc(d, stop, nil) {
			logf(d, "tourist trap: the Mercenary Captain was not reached near %v (at %v)", stop.Anchor, ttHere(d))
			return false
		}
		if !questrunner.OpenDialogue(d, stop) {
			logf(d, "tourist trap: the Mercenary Captain opened no conversation")
			return false
		}
		driveScript(d, stop.Prefer, func() bool { _, ok := ttCaptainAttacker(d); return ok }, 90*time.Second)
		ttSettle(d)
		waitUntil(d, 8*time.Second, func() bool { _, ok := ttCaptainAttacker(d); return ok })
		if npc, attacked = ttCaptainAttacker(d); !attacked {
			logf(d, "tourist trap: the Mercenary Captain did not enter the duel (the taunt's menus ended without it)")
			return false
		}
	}
	logf(d, "tourist trap: fighting the Mercenary Captain (%d of %d hitpoints)", npc.Health, npc.TotalHealth)
	switch questrunner.FightChosen(d, npc, questplan.TouristTrap.Sustain, ttFightBudget, nil) {
	case questrunner.FightWon:
	case questrunner.FightTimedOut:
		e.RequestPark("the Mercenary Captain was still standing after the fight's budget", questplan.TtCaptainPark)
		return false
	default:
		return false
	}
	if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTtMetalKey) > 0 }) {
		logf(d, "tourist trap: the captain is down and dropped no Metal key (one in the bank is the reason the content gives)")
		return false
	}
	logf(d, "tourist trap: the Mercenary Captain is down; the Metal key is in the pack")
	return true
}

// ---------------------------------------------------------------------------------------------------------
// The gate.
// ---------------------------------------------------------------------------------------------------------

var ttGateIDs = []int{questplan.LocTtGateSouth, questplan.LocTtGateNorth}

// ttGateIn unlocks the camp's gate from outside with the Metal key: the account is walked through ("You manage to sneak past the guards!").
func ttGateIn(d *questrunner.Deps) bool {
	if ttAreaNow(d).ZoneOf() == questplan.TtZoneCamp {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjTtMetalKey) == 0 {
		logf(d, "tourist trap: no Metal key for the gate")
		return false
	}
	if !ttTravel(d, questplan.TtGateOut, 0) {
		logf(d, "tourist trap: the gate's outside %v was not reached from %v", questplan.TtGateOut, ttHere(d))
		return false
	}
	if !ttOperate(d, "the camp's gate", ttGateIDs, "Open", questplan.TtGateOut, 3) {
		return false
	}
	inside := func() bool { return ttAreaNow(d).ZoneOf() == questplan.TtZoneCamp }
	driveScript(d, nil, inside, 30*time.Second)
	if !inside() {
		questrunner.WalkNear(d, questplan.TtGateIn, 0)
		waitUntil(d, 4*time.Second, inside)
	}
	ttSettle(d)
	return inside()
}

func ttGateInStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return ttGateIn(e.Deps())
}

// ttGateOut leaves the camp by the gate, which wants the Metal key going out as well and refuses the slave's clothes within four tiles of a guard.
func ttGateOut(d *questrunner.Deps) bool {
	if ttAreaNow(d).ZoneOf() == questplan.TtZoneOpen {
		return true
	}
	if !ttEnsureSurface(d) {
		return false
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneCamp {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjTtMetalKey) == 0 {
		logf(d, "tourist trap: no Metal key for the gate going out")
		return false
	}
	if worn := questrunner.WornCounts(d); ttAnyWorn(worn, ttSlaveIDs) {
		logf(d, "tourist trap: slave clothes on at the gate: a guard within four tiles would search the account")
		return false
	}
	if !ttTravelRound(d, questplan.TtGateIn, 0) {
		logf(d, "tourist trap: the gate's inside %v was not reached from %v", questplan.TtGateIn, ttHere(d))
		return false
	}
	if !ttOperate(d, "the camp's gate", ttGateIDs, "Open", questplan.TtGateIn, 3) {
		return false
	}
	outside := func() bool { return ttAreaNow(d).ZoneOf() == questplan.TtZoneOpen }
	driveScript(d, nil, outside, 30*time.Second)
	if !outside() {
		questrunner.WalkNear(d, questplan.TtGateOut, 0)
		waitUntil(d, 4*time.Second, outside)
	}
	ttSettle(d)
	return outside()
}

func ttGateOutStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	switch ttAreaNow(d).ZoneOf() {
	case questplan.TtZoneNone:
		return ttLeaveAll(d)
	}
	return ttGateOut(d)
}

// ---------------------------------------------------------------------------------------------------------
// Siad's desk.
// ---------------------------------------------------------------------------------------------------------

// ttKeysStep searches the captain's desk upstairs: a cell door key and the Metal key, each only when the account holds none ("You search the
// captains desk while he's not looking..."). Each key is an object box raised BEFORE the key is added: pressed on, never closed.
func ttKeysStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtMetalKey) > 0 {
		return true
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneCamp {
		logf(d, "tourist trap: the desk is inside the camp, not at %v", ttHere(d))
		return false
	}
	if questrunner.FreeSlots(d) < 2 && !ttMakeRoom(d, 2) {
		logf(d, "tourist trap: no room for the two keys the desk gives")
		return false
	}
	desk := questplan.TtDesk
	stand := tile.Tile{X: 3289, Z: 3033, Level: 1}
	if !ttTravelRound(d, stand, 0) {
		logf(d, "tourist trap: the desk's stand %v was not reached from %v", stand, ttHere(d))
		return false
	}
	if !ttOperate(d, "Captain Siad's desk", []int{questplan.LocTtDesk}, "Search", desk, 4) {
		return false
	}
	have := func() bool { return questrunner.HeldID(d, questplan.ObjTtMetalKey) > 0 }
	driveScript(d, nil, have, 25*time.Second)
	ttSettle(d)
	if !have() {
		logf(d, "tourist trap: the desk gave no Metal key")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------------------------------------
// The slave.
// ---------------------------------------------------------------------------------------------------------

func ttHasSlaveOutfit(d *questrunner.Deps) bool {
	pack, worn := questrunner.PackCounts(d), questrunner.WornCounts(d)
	return ttHasSet(pack, worn, ttSlaveIDs)
}

// ttSlaveStep frees a male slave of his chains (a lockpick a try, the Thieving roll) and trades his clothes for desert ones. The whole of it is one
// conversation the content drives with menus; a failed pick offers another go, and from the third failure a guard within ten tiles of the slave
// spots the account, which is searched and put in the cell: counted, and the cell's own step takes over.
func ttSlaveStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !ttEnsureSurface(d) {
		return false
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneCamp {
		logf(d, "tourist trap: the slaves are in the camp, not at %v", ttHere(d))
		return false
	}
	if !ttTravelRound(d, questplan.TtSlaveAt, 3) {
		logf(d, "tourist trap: the slave at %v was not reached from %v", questplan.TtSlaveAt, ttHere(d))
		return false
	}
	var stop questplan.NpcStop
	found := false
	for _, id := range []int{questplan.NpcTtSlave, questplan.NpcTtEscaped} {
		s := questplan.TtSlaveStop
		s.ID = id
		if _, ok := questrunner.FindNpc(d, s, s.Leash); ok {
			stop, found = s, true
			break
		}
	}
	if !found {
		logf(d, "tourist trap: no male slave within %d tiles of %v", questplan.TtSlaveStop.Leash, ttHere(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		logf(d, "tourist trap: the slave opened no conversation")
		return false
	}
	caged := func() bool { return ttAreaNow(d) == questplan.TtAreaSurfaceCell }
	driveScript(d, stop.Prefer, func() bool { return ttHasSlaveOutfit(d) || caged() }, ttTalkBudget)
	ttSettle(d)
	if caged() {
		ttNoteJail(e, "the slave's lock")
		return true
	}
	if !ttHasSlaveOutfit(d) {
		logf(d, "tourist trap: the talk ended with no slave clothes")
		return false
	}
	logf(d, "tourist trap: traded for the slave's clothes")
	return true
}

// ---------------------------------------------------------------------------------------------------------
// The mine doors.
// ---------------------------------------------------------------------------------------------------------

var (
	ttMineDoorIDs = []int{questplan.LocTtMineDoorL, questplan.LocTtMineDoorR}
	ttExitDoorIDs = []int{questplan.LocTtExitDoorL, questplan.LocTtExitDoorR}
)

// ttMineIn pushes the big doors in the camp's wall: slave clothes open them and set the account down in the hall behind (0_51_147_14_18).
func ttMineIn(d *questrunner.Deps) bool {
	if ttAreaNow(d) == questplan.TtAreaHall {
		return true
	}
	if !ttEnsureSurface(d) {
		return false
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneCamp {
		return false
	}
	if worn := questrunner.WornCounts(d); !ttAllWorn(worn, ttSlaveIDs) {
		logf(d, "tourist trap: the mine doors open for slave clothes only")
		return false
	}
	if !ttTravelRound(d, questplan.TtMineDoor, 0) {
		logf(d, "tourist trap: the mine doors' stand %v was not reached from %v", questplan.TtMineDoor, ttHere(d))
		return false
	}
	if !ttOperate(d, "the mine doors", ttMineDoorIDs, "Open", questplan.TtMineDoor, 3) {
		return false
	}
	in := func() bool { return ttAreaNow(d) == questplan.TtAreaHall }
	driveScript(d, nil, in, 30*time.Second)
	ttSettle(d)
	return in() || waitUntil(d, ttArriveWait, in)
}

func ttMineInStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return ttMineIn(e.Deps())
}

// ttMineOut is the hall's exit doors, back to the camp (0_51_47_37_28, the doorway).
func ttMineOut(d *questrunner.Deps) bool {
	if ttAreaNow(d).ZoneOf() == questplan.TtZoneCamp {
		return true
	}
	if ttAreaNow(d) != questplan.TtAreaHall {
		return false
	}
	if worn := questrunner.WornCounts(d); !ttAllWorn(worn, ttSlaveIDs) {
		logf(d, "tourist trap: the exit doors let a slave through: a guard would catch the account in other clothes")
		return false
	}
	if !ttTravelRound(d, questplan.TtHallExit, 0) {
		logf(d, "tourist trap: the exit doors' stand %v was not reached from %v", questplan.TtHallExit, ttHere(d))
		return false
	}
	if !ttOperate(d, "the exit doors", ttExitDoorIDs, "Open", tile.Tile{X: 3278, Z: 9426}, 3) {
		return false
	}
	out := func() bool { return ttAreaNow(d).ZoneOf() == questplan.TtZoneCamp }
	driveScript(d, nil, out, 30*time.Second)
	ttSettle(d)
	return out() || waitUntil(d, ttArriveWait, out)
}

func ttMineOutStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return ttMineOut(e.Deps())
}

// ---------------------------------------------------------------------------------------------------------
// Siad's plans.
// ---------------------------------------------------------------------------------------------------------

// ttPlansStep gets the technical plans out of Siad's chest: the bookcase searched (the sailing bit), Siad talked into looking out of his window
// ("I could tell by the cut of your jib."), and the chest opened with Al Shabim's key before the next thing the account does (the bit is kept
// until the chest is tried). The box that shows the plans is raised BEFORE they are added: continued, never closed.
func ttPlansStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtPlans) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjTtBedobinKey) == 0 {
		logf(d, "tourist trap: no Bedobin key for Siad's chest")
		return false
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneCamp {
		logf(d, "tourist trap: Siad's room is in the camp, not at %v", ttHere(d))
		return false
	}
	if questrunner.FreeSlots(d) < 1 && !ttMakeRoom(d, 1) {
		logf(d, "tourist trap: no room for the plans")
		return false
	}
	up := tile.Tile{X: 3285, Z: 3032, Level: 1}
	for attempt := 1; attempt <= 3; attempt++ {
		if !ttTravelRound(d, up, 1) {
			logf(d, "tourist trap: the bookcase's stand %v was not reached from %v", up, ttHere(d))
			return false
		}
		if ttOperate(d, "the bookcase", []int{questplan.LocTtBookcase}, "Search", questplan.TtBookcase, 4) {
			driveScript(d, nil, nil, 8*time.Second)
			ttSettle(d)
		}
		stop := questplan.TtSiadStop
		if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
			logf(d, "tourist trap: Captain Siad opened no conversation (attempt %d of 3)", attempt)
			continue
		}
		driveScript(d, stop.Prefer, nil, 90*time.Second)
		ttSettle(d)
		// The chest's east side is its only stand (forceapproach): walk there and open it at once.
		if !ttTravelRound(d, tile.Tile{X: 3293, Z: 3033, Level: 1}, 0) {
			continue
		}
		if !ttOperate(d, "Siad's chest", []int{questplan.LocTtChest}, "Open", questplan.TtChest, 3) {
			continue
		}
		have := func() bool { return questrunner.HeldID(d, questplan.ObjTtPlans) > 0 }
		driveScript(d, nil, have, 25*time.Second)
		ttSettle(d)
		if have() {
			logf(d, "tourist trap: the technical plans are in the pack")
			return true
		}
		logf(d, "tourist trap: the chest gave no plans (attempt %d of 3: Siad was not distracted, or the chest is empty)", attempt)
	}
	return false
}

// ---------------------------------------------------------------------------------------------------------
// The cells.
// ---------------------------------------------------------------------------------------------------------

func ttInSurfaceCell(t tile.Tile) bool {
	return questplan.TtAreaOf(t) == questplan.TtAreaSurfaceCell && !tileIsRock(t)
}

// tileIsRock reports one of the landing tiles of the cell's way out (outside the cell proper).
func tileIsRock(t tile.Tile) bool {
	for _, r := range questplan.TtJailRocks {
		if r.X == t.X && r.Z == t.Z {
			return true
		}
	}
	return false
}

// ttJailCellStep is the way out of the surface cell: the window's bar (Search, "Yes, I'll bend the bar.", a Strength roll a try) lands the account
// outside, then four rocks west are climbed (Climb-to, "Yes, I'll give it a try." on the first, an Agility roll a rock, 7 hitpoints and back down on
// a miss) and the fourth lets it drop to the desert. The rocks' stands are not walkable ground, so the ops are sent from where the account stands
// (the content's aploc scripts walk it up to them); each is checked by the tile it should move the account to.
func ttJailCellStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	ok := ttJailCell(e.Deps())
	if !ok {
		ttWeakPark(e, ttFallDamage+ttHPMargin, "a rock in the cell")
	}
	return ok
}

// ttJailCell is the surface cell's way out, whoever asks.
func ttJailCell(d *questrunner.Deps) bool {
	for attempt := 0; attempt < 40; attempt++ {
		if ttAreaNow(d) != questplan.TtAreaSurfaceCell {
			return true
		}
		questrunner.EatIfNeeded(d, questplan.TouristTrap.Sustain.FoodIDs, 18)
		here := ttHere(d)
		if ttInSurfaceCell(here) {
			if !ttOperate(d, "the cell's window", []int{questplan.LocTtWindow}, "Search", questplan.TtWindowAt, 3) {
				return false
			}
			driveScript(d, []string{"Yes, I'll bend the bar.", "Yes, I'll bend the bar again."}, func() bool { return !ttInSurfaceCell(ttHere(d)) }, 2*time.Minute)
			ttSettle(d)
			continue
		}
		if !ttClimbNextRock(d, here) {
			return false
		}
	}
	logf(d, "tourist trap: the cell's way out took 40 tries; at %v", ttHere(d))
	return ttAreaNow(d) != questplan.TtAreaSurfaceCell
}

// ttClimbNextRock sends the climb op for the nearest rock west of the account and waits for it to move the account.
func ttClimbNextRock(d *questrunner.Deps, here tile.Tile) bool {
	type rock struct{ id, x, z int }
	rocks := []rock{{questplan.LocTtRock1, 3280, 3038}, {questplan.LocTtRock2, 3278, 3039}, {questplan.LocTtRock3, 3276, 3040}, {questplan.LocTtRock4, 3274, 3039}}
	var next *rock
	for i := range rocks {
		if rocks[i].x < here.X || (rocks[i].x == here.X && rocks[i].z != here.Z) {
			if next == nil || rocks[i].x > next.x {
				next = &rocks[i]
			}
		}
	}
	if next == nil {
		logf(d, "tourist trap: no rock west of %v to climb", here)
		return false
	}
	// A slip deals 7 and can kill: the climb waits until the hitpoints are above the fall and a margin.
	if !ttRestAbove(d, ttFallDamage+ttHPMargin, ttRestLimit) {
		logf(d, "tourist trap: not climbing rock %d: the hitpoints stay under %d", next.id, ttFallDamage+ttHPMargin+1)
		return false
	}
	op := "Climb-to"
	if next.id == questplan.LocTtRock4 {
		op = "Climb down"
	}
	typ, ok := d.Config.Loc(next.id)
	if !ok {
		return false
	}
	n := 0
	for i, o := range typ.Ops {
		if o == op {
			n = i + 1
		}
	}
	if n == 0 {
		logf(d, "tourist trap: rock %d offers %v, not %q", next.id, typ.Ops, op)
		return false
	}
	ttSettle(d)
	if err := actions.OpLoc(d.Session, n, next.x, next.z, next.id); err != nil {
		logf(d, "tourist trap: rock %d: %v", next.id, err)
		return false
	}
	moved := func() bool { return ttHere(d) != here }
	driveScript(d, []string{"Yes, I'll give it a try."}, moved, 15*time.Second)
	ttSettle(d)
	return true
}

// ttJailMineStep is the way out of the punishment mine: fifteen "Rock" from the cell's rocks (any Mining level, a pickaxe from the cell's floor) in
// one go, handed to the guard at the gate.
func ttJailMineStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	ttNoteJailOnce(e)
	return ttJailMine(e.Deps())
}

// ttJailMine is the punishment mine's way out, whoever asks.
func ttJailMine(d *questrunner.Deps) bool {
	for round := 0; round < 60; round++ {
		if ttAreaNow(d) != questplan.TtAreaMineCell {
			return true
		}
		questrunner.EatIfNeeded(d, questplan.TouristTrap.Sustain.FoodIDs, 18)
		if !ttHasPickaxe(d) {
			if !ttTakeCellPickaxe(d) {
				logf(d, "tourist trap: no pickaxe on the punishment mine's floor")
				return false
			}
		}
		have := questrunner.HeldID(d, questplan.ObjTtPunishRock)
		if have >= 15 {
			return ttCellGate(d)
		}
		if need := 15 - have; questrunner.FreeSlots(d) < need && !ttMakeRoom(d, need) {
			logf(d, "tourist trap: %d free slots for %d more rocks", questrunner.FreeSlots(d), need)
			return false
		}
		if !ttMineOneRock(d, have) {
			d.Delay(2 * time.Second)
		}
	}
	return ttAreaNow(d) != questplan.TtAreaMineCell
}

// ttJailMineSeen is when the punishment mine was last counted: one entry is many ticks of the same step.
var (
	ttJailMineMu   sync.Mutex
	ttJailMineSeen = map[*questrunner.Deps]time.Time{}
)

// ttNoteJailOnce counts the punishment mine once per entry (an entry that has gone five minutes without a step is a new one).
func ttNoteJailOnce(e *questrunner.Engine) {
	d := e.Deps()
	ttJailMineMu.Lock()
	last := ttJailMineSeen[d]
	ttJailMineSeen[d] = time.Now()
	ttJailMineMu.Unlock()
	if time.Since(last) < 5*time.Minute {
		return
	}
	ttNoteJail(e, "the punishment mine")
}

var ttPickaxeIDs = []int{1265, 1267, 1269, 1271, 1273, 1275}

func ttHasPickaxe(d *questrunner.Deps) bool {
	pack, worn := questrunner.PackCounts(d), questrunner.WornCounts(d)
	for _, id := range ttPickaxeIDs {
		if pack[id]+worn[id] > 0 {
			return true
		}
	}
	return false
}

func ttTakeCellPickaxe(d *questrunner.Deps) bool {
	for _, at := range questplan.TtCellPick {
		if questrunner.TakeGround(d, questplan.ObjTtBronzePickax, at, 20) {
			return true
		}
	}
	return questrunner.TakeGround(d, questplan.ObjTtBronzePickax, ttHere(d), 30)
}

// ttMineOneRock mines the nearest of the cell's rocks that the account can reach, and waits for the rock to arrive in the pack.
func ttMineOneRock(d *questrunner.Deps, have int) bool {
	here := ttHere(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	ms := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 12, IDs: []int{questplan.LocTtPunishRock}, Op: "Mine"}, live)
	for i, m := range ms {
		if i >= 6 {
			break
		}
		if len(questrunner.LocStands(d, m, here)) == 0 {
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Mine"); err != nil {
			continue
		}
		if waitUntil(d, 25*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTtPunishRock) > have }) {
			return true
		}
	}
	return false
}

// ttCellGate hands the fifteen rocks to the guard at the gate.
func ttCellGate(d *questrunner.Deps) bool {
	inside := tile.Tile{X: 3292, Z: 9434}
	if !ttTravel(d, inside, 0) {
		logf(d, "tourist trap: the cell gate's inside %v was not reached from %v", inside, ttHere(d))
		return false
	}
	if !ttOperate(d, "the cell's gate", []int{questplan.LocTtCellGateL, questplan.LocTtCellGateR}, "Open", questplan.TtCellGate, 3) {
		return false
	}
	out := func() bool { return ttAreaNow(d) != questplan.TtAreaMineCell }
	driveScript(d, nil, out, 45*time.Second)
	ttSettle(d)
	return out() || waitUntil(d, ttArriveWait, out)
}

func ttTentOutStep(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return ttTentExit(e.Deps())
}
