package questmods

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// BlackKnightsFortress is Black Knights' Fortress ready for the engine. Its module's talks (Sir Amik), kit (the bank, the mule, the
// shops), equips (the iron chainbody and the bronze med helm) and cabbage (the field south of Falador) are the generic steps and
// the provisioner's; the three executors are the walks inside the fortress, which the long-distance walker cannot be trusted
// with (questplan/blackknightsroute.go says why): bk-spy takes the account to the grill and listens, bk-sabotage to the hole
// and drops the cabbage, bk-leave out of the fortress again. All three run the same loop, bkRoute: find the region of the
// route the account stands in, take the next crossing of questplan.BKPlan, check it landed, repeat.
func BlackKnightsFortress() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.BlackKnightsFortress,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomBKSpy:      bkSpy,
			questplan.CustomBKSabotage: bkSabotage,
			questplan.CustomBKLeave:    bkLeave,
		},
	}
}

const (
	// bkCrossTries is how many times a crossing is sent before the step gives up: a click can be dropped (a knight's hit, a
	// random event) and the engine's next decision walks the account back to where it stands.
	bkCrossTries = 3
	// bkLandWait is how long a crossing gets to land the account: the walk onto the door's tile, a climb's two or three ticks, and
	// the warning guard's dialogue when there is one.
	bkLandWait = 10 * time.Second
	// bkDialogueWait is how long a loc gets to open its dialogue.
	bkDialogueWait = 8 * time.Second
	// bkMaxLegs bounds one step's crossings: the longest plan has thirteen.
	bkMaxLegs = 40
	// bkAbortHP is the hitpoints at or under which an account with no food left stops what it is doing and goes out the nearest way.
	bkAbortHP = 10
	// bkEntryMargin is the least of the run an account needs left to go in at the front door: the walk to the hall and back out is
	// about eight minutes without trouble, and a run that ends with the account inside leaves it among the knights for whatever the
	// Stage 5 trainer does next from a place the walker's graph cannot leave.
	bkEntryMargin = 15 * time.Minute
	// bkLeaveMargin is how little of the run an account inside may have left before it turns for the door.
	bkLeaveMargin = 4 * time.Minute
)

// bkResult is how a walk along the route ended.
type bkResult int

const (
	// bkFailed: a crossing would not land, the account died, got lost, or turned back for want of food (the quest is parked for that).
	bkFailed bkResult = iota
	// bkArrived: the account is in the region asked for.
	bkArrived
	// bkYielded: not enough of the run was left to go in or stay in, and the account is out; the run has been ended and the quest
	// is not marked failed: the next pass tries again.
	bkYielded
)

// bkSentryAvoid is the knights' open ground as the walker's rectangle.
func bkSentryAvoid() []webwalk.Rect {
	r := questplan.BKSentryGround
	return []webwalk.Rect{{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level}}
}

// bkWhere says how a tile relates to the route.
type bkWhere int

const (
	// bkAt: in a region of the route (the outside included).
	bkAt bkWhere = iota
	// bkFar: nowhere near the fortress.
	bkFar
	// bkLost: inside the fortress but on floor the route does not name (a staircase room, the witch's, the roof).
	bkLost
)

// bkLocate is the region of the route a tile belongs to: outside for any tile near the fortress that is not inside it, else the
// region whose floor the tile can be walked to on foot (the pack's static collision, every door shut, which is what the table's
// regions are).
func bkLocate(d *questrunner.Deps, here tile.Tile) (string, bkWhere) {
	if !questplan.BKInside(here) {
		if questplan.BKNearFortress(here) {
			return questplan.BKOutside, bkAt
		}
		return "", bkFar
	}
	if d.Pack == nil {
		return "", bkLost
	}
	flood := d.Pack.Flood(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, 70)
	regions := questplan.BKRegionTiles()
	names := make([]string, 0, len(regions))
	for r := range regions {
		names = append(names, r)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, t := range regions[name] {
			if t.Level == here.Level && flood[[2]int{t.X, t.Z}] {
				return name, bkAt
			}
		}
	}
	return "", bkLost
}

// bkDisguiseWorn reports the iron chainbody and the bronze med helm both worn.
func bkDisguiseWorn(d *questrunner.Deps) bool {
	worn := questrunner.WornCounts(d)
	return worn[questplan.ObjIronChainbody] > 0 && worn[questplan.ObjBronzeMedHelm] > 0
}

// bkFoodLeft is the hitpoints of food the pack holds.
func bkFoodLeft(d *questrunner.Deps) int {
	return questplan.FoodPoolHeal(questrunner.PackCounts(d), questplan.BKFoods)
}

// bkCross runs one crossing: onto its stand, the op on its loc, the dialogue it opens if any, and the wait for the tile it lands on. A
// crossing that has not landed the account after bkCrossTries sends is reported failed. It never fights: the knights that
// follow are left behind by the ladder or the door.
func bkCross(d *questrunner.Deps, leg questplan.BKLeg) bool {
	if leg.Disguise && !bkDisguiseWorn(d) {
		logf(d, "bk: %s needs the iron chainbody and the bronze med helm worn, and they are not", leg.Name)
		return false
	}
	if leg.Opens {
		return bkOpenAndStep(d, leg)
	}
	for try := 1; try <= bkCrossTries; try++ {
		if at := questrunner.CurrentTile(d); at == leg.Land {
			return true // an earlier send landed late
		} else if at != leg.Stand && !questrunner.WalkNear(d, leg.Stand, 0) {
			logf(d, "bk: could not stand on %d,%d,%d for %s (try %d)", leg.Stand.X, leg.Stand.Z, leg.Stand.Level, leg.Name, try)
			continue
		}
		m, ok := questrunner.FindLoc(d, leg.Loc.ID, "", leg.Op, leg.Loc.Tile(), 0)
		if !ok || m.X != leg.Loc.X || m.Z != leg.Loc.Z || m.Level != leg.Loc.Level {
			// A door another account has open has another id for a few ticks: wait a moment and look again.
			d.Delay(1500 * time.Millisecond)
			continue
		}
		typ, ok := d.Config.Loc(m.ID)
		n := cachecfg.OpIndex(typ.Ops, leg.Op)
		if !ok || n == 0 {
			logf(d, "bk: loc %d offers %v, not %q", m.ID, typ.Ops, leg.Op)
			return false
		}
		questrunner.ClearLeftoverModal(d)
		logf(d, "bk: %s (%s %s at %d,%d,%d from %d,%d,%d)", leg.Name, leg.Op, typ.Name, m.X, m.Z, m.Level, leg.Stand.X, leg.Stand.Z, leg.Stand.Level)
		if err := actions.OpLoc(d.Session, n, m.X, m.Z, m.ID); err != nil {
			logf(d, "bk: %s: %v", leg.Name, err)
			continue
		}
		if len(leg.Dialogue) > 0 {
			bkDrive(d, leg.Dialogue)
		}
		if waitUntil(d, bkLandWait, func() bool { return questrunner.CurrentTile(d) == leg.Land }) {
			return true
		}
		at := questrunner.CurrentTile(d)
		logf(d, "bk: %s did not land on %d,%d,%d (standing at %d,%d,%d, try %d)", leg.Name, leg.Land.X, leg.Land.Z, leg.Land.Level, at.X, at.Z, at.Level, try)
	}
	return false
}

// bkOpenAndStep crosses a plain door: the op swings it open for 500 ticks and the account walks the one step through. A door that is
// not shut (another account opened it, or this one did a few minutes ago: an open door is another loc) needs no op. The walk is a
// single click on the landing tile, which is what the long-distance walker's own door crossing sends: the pack still draws the
// door as a wall, so the local pathfinder is no use for the step.
func bkOpenAndStep(d *questrunner.Deps, leg questplan.BKLeg) bool {
	shut := func() bool {
		m, ok := questrunner.FindLoc(d, leg.Loc.ID, "", leg.Op, leg.Loc.Tile(), 0)
		return ok && m.X == leg.Loc.X && m.Z == leg.Loc.Z && m.Level == leg.Loc.Level
	}
	for try := 1; try <= bkCrossTries; try++ {
		if at := questrunner.CurrentTile(d); at == leg.Land {
			return true
		} else if at != leg.Stand && !questrunner.WalkNear(d, leg.Stand, 0) {
			logf(d, "bk: could not stand on %d,%d,%d for %s (try %d)", leg.Stand.X, leg.Stand.Z, leg.Stand.Level, leg.Name, try)
			continue
		}
		if shut() {
			m, _ := questrunner.FindLoc(d, leg.Loc.ID, "", leg.Op, leg.Loc.Tile(), 0)
			typ, ok := d.Config.Loc(m.ID)
			n := cachecfg.OpIndex(typ.Ops, leg.Op)
			if !ok || n == 0 {
				logf(d, "bk: loc %d offers %v, not %q", m.ID, typ.Ops, leg.Op)
				return false
			}
			questrunner.ClearLeftoverModal(d)
			logf(d, "bk: %s (%s %s at %d,%d,%d from %d,%d,%d)", leg.Name, leg.Op, typ.Name, m.X, m.Z, m.Level, leg.Stand.X, leg.Stand.Z, leg.Stand.Level)
			if err := actions.OpLoc(d.Session, n, m.X, m.Z, m.ID); err != nil {
				logf(d, "bk: %s: %v", leg.Name, err)
				continue
			}
			if !waitUntil(d, bkLandWait, func() bool { return !shut() }) {
				logf(d, "bk: %s did not open (try %d)", leg.Name, try)
				continue
			}
		}
		if err := actions.Walk(d.Session, []pathfind.NavPoint{{X: leg.Land.X, Z: leg.Land.Z, Level: leg.Land.Level}}, true); err != nil {
			logf(d, "bk: %s: the step through: %v", leg.Name, err)
			continue
		}
		if waitUntil(d, bkLandWait, func() bool { return questrunner.CurrentTile(d) == leg.Land }) {
			return true
		}
		at := questrunner.CurrentTile(d)
		logf(d, "bk: %s: did not step onto %d,%d,%d (standing at %d,%d,%d, try %d)", leg.Name, leg.Land.X, leg.Land.Z, leg.Land.Level, at.X, at.Z, at.Level, try)
	}
	return false
}

// bkDrive waits for a dialogue to open and drives it to its close, choosing from prefer.
func bkDrive(d *questrunner.Deps, prefer []string) bool {
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, bkDialogueWait, dl.Ready) {
		return false
	}
	ok := dl.Drive(prefer)
	questrunner.ClearLeftoverModal(d)
	return ok
}

// bkWorld is what a walk along the route asks of the account and the session, so the loop that plans and takes the crossings can
// be tested without either.
type bkWorld interface {
	// Tile is where the account stands.
	Tile() tile.Tile
	// Locate is the region of the route a tile belongs to.
	Locate(tile.Tile) (string, bkWhere)
	// Cross runs one crossing and reports whether it landed.
	Cross(questplan.BKLeg) bool
	// Approach walks the account from far away to the front door's approach, round the knights' open ground.
	Approach() bool
	// Waypoint walks an account standing on the open ground by the fortress to the waypoint west of it, round the knights' open
	// ground, and reports whether it is there.
	Waypoint() bool
	// Died reports the account dead since the walk began.
	Died() bool
	// Out reports the account at or under bkAbortHP with no food left.
	Out() bool
	// Late reports less than bkLeaveMargin of the engine's run left, and Short less than bkEntryMargin.
	Late() bool
	Short() bool
	// Yield ends the engine's run now.
	Yield()
	// Park asks the engine to park the quest.
	Park(reason string)
	Logf(format string, args ...any)
}

// bkRoute takes the account along the route to a region. From far away it first walks to the front door's approach; from inside it
// takes the crossings of questplan.BKPlan one at a time, so a step interrupted anywhere (a random event, a restart) resumes from the
// room the account stands in. An account that is out of food and hitpoints turns round for the nearest way out and parks the quest on
// arrival; one that has too little of the run left to go in does not go, and one inside with the run nearly over goes out, both
// ending the run (bkYielded) and leaving the quest unmarked. Every one of those ends, and a crossing that fails at the front door, that
// leaves the account on the open ground by the door walks it to the waypoint west of the fortress.
func bkRoute(w bkWorld, goal string) bkResult {
	res := bkWalk(w, goal)
	if w.Died() {
		return res
	}
	// Whatever way the walk ended, an account it left on the open ground by the front door goes west before anything else sends it
	// anywhere: the front door is the end of a walk out, of a retreat for want of food or time, of a run that is over before the
	// account goes in, and of a first crossing that would not land, and the walker's own way from there to Falador or a bank runs
	// south past the three knights in the open ground (the next module and the stage loop do not carry the rectangle).
	if region, where := w.Locate(w.Tile()); where != bkAt || region != questplan.BKOutside {
		return res
	}
	if !w.Waypoint() {
		w.Logf("bk: could not walk west to %d,%d, clear of the knights' open ground", questplan.BKWestWaypoint.X, questplan.BKWestWaypoint.Z)
		if res == bkArrived {
			return bkFailed
		}
	}
	return res
}

// bkWalk is the walk bkRoute makes.
func bkWalk(w bkWorld, goal string) bkResult {
	retreat, late := false, false
	for step := 0; step < bkMaxLegs; step++ {
		if w.Died() {
			w.Logf("bk: the account died on the way")
			return bkFailed
		}
		here := w.Tile()
		region, where := w.Locate(here)
		if where != bkLost && goal != questplan.BKOutside && !retreat {
			switch {
			case (where == bkFar || region == questplan.BKOutside) && w.Out():
				// Not on the way in: too weak to go near the knights at all.
				w.Logf("bk: at or under %d hitpoints and no food left: not going to the fortress", bkAbortHP)
				return bkFailed
			case where == bkAt && region != questplan.BKOutside && w.Late():
				w.Logf("bk: under %s of the run left and inside the fortress: going out", bkLeaveMargin)
				goal, retreat, late = questplan.BKOutside, true, true
			case where == bkAt && region != questplan.BKOutside && w.Out():
				w.Logf("bk: at or under %d hitpoints and no food left: going out the nearest way", bkAbortHP)
				goal, retreat = questplan.BKOutside, true
			case where == bkAt && region == questplan.BKOutside && w.Short():
				w.Logf("bk: under %s of the run left: not going in now", bkEntryMargin)
				w.Yield()
				return bkYielded
			}
		}
		switch where {
		case bkFar:
			if goal == questplan.BKOutside {
				if retreat && late {
					w.Yield()
					return bkYielded
				}
				if retreat {
					return bkFailed
				}
				return bkArrived
			}
			if w.Short() {
				w.Logf("bk: under %s of the run left: not walking to the fortress now", bkEntryMargin)
				w.Yield()
				return bkYielded
			}
			if !w.Approach() {
				return bkFailed
			}
			continue
		case bkLost:
			w.Logf("bk: %d,%d,%d is inside the fortress but on no part of the route", here.X, here.Z, here.Level)
			return bkFailed
		}
		if region == goal {
			switch {
			case retreat && late:
				w.Yield()
				return bkYielded
			case retreat:
				w.Park("out of food in the Black Knights' Fortress with the knights about")
				return bkFailed
			}
			return bkArrived
		}
		leg, ok := questplan.BKLegNext(region, goal)
		if !ok {
			w.Logf("bk: no way from %q to %q", region, goal)
			return bkFailed
		}
		if !w.Cross(leg) {
			return bkFailed
		}
	}
	w.Logf("bk: %d crossings and still not at %q", bkMaxLegs, goal)
	return bkFailed
}

// bkLive is the route's world: a quest account.
type bkLive struct {
	e    *questrunner.Engine
	d    *questrunner.Deps
	mark time.Time
}

func newBKLive(e *questrunner.Engine) *bkLive { return &bkLive{e: e, d: e.Deps(), mark: time.Now()} }

func (w *bkLive) Tile() tile.Tile                      { return questrunner.CurrentTile(w.d) }
func (w *bkLive) Locate(t tile.Tile) (string, bkWhere) { return bkLocate(w.d, t) }
func (w *bkLive) Cross(leg questplan.BKLeg) bool       { return bkCross(w.d, leg) }
func (w *bkLive) Approach() bool {
	return questrunner.TravelAvoiding(w.d, questplan.BKDoorApproach, 1, bkSentryAvoid())
}
func (w *bkLive) Waypoint() bool {
	return questrunner.TravelAvoiding(w.d, questplan.BKWestWaypoint, questplan.BKWaypointRadius, bkSentryAvoid())
}
func (w *bkLive) Died() bool { return questrunner.DiedSince(w.d, w.mark) }
func (w *bkLive) Out() bool {
	hp, _ := questrunner.Hitpoints(w.d)
	return hp <= bkAbortHP && bkFoodLeft(w.d) == 0
}
func (w *bkLive) Late() bool                 { return w.e.RunLeft() < bkLeaveMargin }
func (w *bkLive) Short() bool                { return w.e.RunLeft() < bkEntryMargin }
func (w *bkLive) Yield()                     { w.e.YieldRun() }
func (w *bkLive) Park(reason string)         { w.e.RequestPark(reason, questplan.BKFitRetry) }
func (w *bkLive) Logf(f string, args ...any) { logf(w.d, f, args...) }

// bkAdvance is bkRoute over the engine's account.
func bkAdvance(e *questrunner.Engine, goal string) bkResult { return bkRoute(newBKLive(e), goal) }

// bkSpy is stage 1: to the grill's own tile and a listen at it. The grill speaks nine pages and the stage rises after the last, so the
// dialogue is driven to its close; a grill that says nothing (the stage is not 1) gives no dialogue and the step reports failure.
func bkSpy(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	switch bkAdvance(e, questplan.BKGrillShaft) {
	case bkFailed:
		return false
	case bkYielded:
		return true
	}
	if !questrunner.WalkNear(d, questplan.BKGrillStand, 0) {
		logf(d, "bk: could not walk to the grill's tile")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocBKGrill, "", "Listen-at", questplan.BKGrill.Tile(), 0)
	if !ok {
		logf(d, "bk: no grill at %d,%d,%d", questplan.BKGrill.X, questplan.BKGrill.Z, questplan.BKGrill.Level)
		return false
	}
	questrunner.ClearLeftoverModal(d)
	if err := questrunner.OperateLoc(d, m, "Listen-at"); err != nil {
		logf(d, "bk: the grill: %v", err)
		return false
	}
	if !bkDrive(d, nil) {
		logf(d, "bk: the grill gave no conversation, or one that did not close")
		return false
	}
	logf(d, "bk: listened at the grill")
	return true
}

// bkSabotage is stage 2: to the hole above the witch's cauldron and the cabbage down it. The cabbage leaves the pack on the script's first
// line, a few ticks of messages follow and the stage rises before the account's closing line, which is driven to its close.
func bkSabotage(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjCabbage) == 0 {
		logf(d, "bk: no cabbage for the hole")
		return false
	}
	switch bkAdvance(e, questplan.BKWarehouse) {
	case bkFailed:
		return false
	case bkYielded:
		return true
	}
	stood := false
	for _, s := range questplan.BKHoleStands {
		if questrunner.WalkNear(d, s, 0) {
			stood = true
			break
		}
	}
	if !stood {
		logf(d, "bk: could not walk to a tile beside the hole")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocBKHole, "", "", questplan.BKHole.Tile(), 0)
	if !ok {
		logf(d, "bk: no hole at %d,%d,%d", questplan.BKHole.X, questplan.BKHole.Z, questplan.BKHole.Level)
		return false
	}
	before := questrunner.HeldID(d, questplan.ObjCabbage)
	questrunner.ClearLeftoverModal(d)
	if err := questrunner.UseItemOnLoc(d, questplan.ObjCabbage, m); err != nil {
		logf(d, "bk: the cabbage on the hole: %v", err)
		return false
	}
	if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjCabbage) < before }) {
		logf(d, "bk: the hole took no cabbage (the potion may already be ruined, or this is not the plain cabbage)")
		return false
	}
	// "You drop a cabbage down the hole." ... "You hear the witch groan in dismay.": four messages, two ticks apart, then the line.
	bkDriveLong(d)
	logf(d, "bk: the cabbage is down the hole")
	return true
}

// bkDriveLong drives the closing dialogue of a script that talks after a run of messages: it waits up to twenty seconds for the first page.
func bkDriveLong(d *questrunner.Deps) {
	dl := questrunner.NewDialogue(d)
	if waitUntil(d, 20*time.Second, dl.Ready) {
		dl.Drive(nil)
	}
	questrunner.ClearLeftoverModal(d)
}

// bkLeave is stage 3's way out, and the way out from anywhere inside: to the open ground, then (bkRoute's last step) to the waypoint
// west of the fortress, out of the knights' sight, from which the walker's route to Sir Amik runs west and not south past them. An
// account already on the open ground is only walked to the waypoint.
func bkLeave(e *questrunner.Engine, _ questplan.Step) bool {
	return bkAdvance(e, questplan.BKOutside) != bkFailed
}
