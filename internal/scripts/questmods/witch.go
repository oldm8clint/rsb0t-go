package questmods

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// WitchsHouse is Witch's House ready for the engine.
func WitchsHouse() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.WitchsHouse,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomWitchPot:      witchPot,
			questplan.CustomWitchMagnet:   witchMagnet,
			questplan.CustomWitchDropMag:  witchDropMagnet,
			questplan.CustomWitchMouse:    witchMouse,
			questplan.CustomWitchDiary:    witchDiary,
			questplan.CustomWitchFountain: witchFountain,
			questplan.CustomWitchShed:     witchShed,
			questplan.CustomWitchBall:     witchBall,
			questplan.CustomWitchOut:      witchOut,
		},
	}
}

func held(d *questrunner.Deps, id int) bool { return questrunner.HeldID(d, id) > 0 }

// witchPot looks under the potted plant west of the front door for the door key.
func witchPot(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjWitchDoorKey) {
		return true
	}
	if !questrunner.Travel(d, questplan.WitchPotStand, 1) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWitchPot, "", "Look-under", questplan.WitchPotTile, 3)
	if !ok {
		logf(d, "the witch's potted plant is not in the loc index")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Look-under"); err != nil {
		logf(d, "look under the potted plant: %v", err)
		return false
	}
	// quest_ball.rs2 shows a mesbox and adds the key only once it is continued: waiting and
	// then closing the box (witch-empty-1 and -2) aborted the script with no key and no message.
	questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjWitchDoorKey) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	if !held(d, questplan.ObjWitchDoorKey) {
		// The first questproof EMPTY run (witch-empty-1) parked here with no game message at all.
		at := questrunner.CurrentTile(d)
		logf(d, "no door key after Look-under from %d,%d,%d (plant at %d,%d)", at.X, at.Z, at.Level, m.X, m.Z)
	}
	return held(d, questplan.ObjWitchDoorKey)
}

// A ~mesbox is a p_pausebutton and the item after it arrives only once it is continued; closing
// it (ClearLeftoverModal) drops the script. Every "wait for the item" here drives the dialogue.

// witchMagnet goes down to the cellar (the walker opens the front door with the key held and the
// electrified gate with the gloves worn) and opens, then searches, the cupboard.
func witchMagnet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjWitchMagnet) {
		return true
	}
	if !questrunner.Travel(d, questplan.WitchCupStand, 1) {
		logf(d, "the cellar cupboard is unreachable (the gate passes only with the gloves worn)")
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocWitchCupShut, "", "Open", questplan.WitchCupTile, 2); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		waitUntil(d, 6*time.Second, func() bool {
			_, open := questrunner.FindLoc(d, questplan.LocWitchCupOpen, "", "Search", questplan.WitchCupTile, 2)
			return open
		})
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWitchCupOpen, "", "Search", questplan.WitchCupTile, 2)
	if !ok {
		logf(d, "the cellar cupboard did not open")
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjWitchMagnet) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	if !held(d, questplan.ObjWitchMagnet) && d.Chat != nil && strings.Contains(strings.ToLower(d.Chat.Since(mark)), "anything interesting") {
		// magnetcbopen counts the bank (obj_gettotal). The decision draws a banked magnet when the bank has been read
		// this run; when it has not (the kit's bank visit failed), the next run's visit will.
		logf(d, "the cupboard is empty: a magnet exists somewhere (the pack, worn, or the bank)")
		e.RequestPark("the magnet is in the bank, which the cupboard counts: the next run draws it", witchBankParkFor)
	}
	e.InvalidateStage()
	return held(d, questplan.ObjWitchMagnet)
}

// witchDropMagnet drops a magnet that stops the cupboard issuing the right one.
func witchDropMagnet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if err := questrunner.OpHeldByName(d, questplan.ObjWitchMagnet, "Drop"); err != nil {
		logf(d, "dropping the stale magnet: %v", err)
		return false
	}
	return waitUntil(d, 6*time.Second, func() bool { return !held(d, questplan.ObjWitchMagnet) })
}

// witchMouse lures the mouse with cheese from the porch and fits the magnet to its harness.
func witchMouse(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.WitchPorch, 0) {
		logf(d, "the back porch is unreachable (the front door needs the key in the pack)")
		return false
	}
	mouse := func() (entityquery.Entity, bool) { return questrunner.FindNpcByID(d, questplan.NpcWitchMouse, "", 4) }
	if _, ok := mouse(); !ok {
		if m, found := questrunner.FindLoc(d, questplan.LocWitchHole, "", "", questplan.WitchHoleTile, 2); found {
			if err := questrunner.UseItemOnLoc(d, questplan.ObjCheese, m); err != nil {
				logf(d, "cheese on the mouse hole: %v", err)
			}
			questrunner.NewDialogue(d).DriveUntil(func() bool { _, ok := mouse(); return ok }, nil, 8*time.Second)
		}
		// Dropping cheese on the porch runs the same lure label (opheld5,cheese).
		if _, ok := mouse(); !ok && held(d, questplan.ObjCheese) {
			_ = questrunner.OpHeldByName(d, questplan.ObjCheese, "Drop")
			questrunner.NewDialogue(d).DriveUntil(func() bool { _, ok := mouse(); return ok }, nil, 8*time.Second)
		}
		questrunner.ClearLeftoverModal(d)
	}
	npc, ok := mouse()
	if !ok {
		logf(d, "no mouse came out of the hole")
		return false
	}
	questrunner.WalkNear(d, questplan.WitchPorch, 0)
	if err := questrunner.UseItemOnNpc(d, questplan.ObjWitchMagnet, npc); err != nil {
		logf(d, "magnet on the mouse: %v", err)
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return !held(d, questplan.ObjWitchMagnet) }, nil, 12*time.Second)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return !held(d, questplan.ObjWitchMagnet)
}

// witchDiary takes the diary from the upstairs bedroom and reads it: at stage 3 that sets
// stage 5, after which a catch by the witch costs only the shed key.
func witchDiary(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !held(d, questplan.ObjWitchDiary) {
		if !questrunner.Travel(d, questplan.WitchDiaryStand, 1) {
			return false
		}
		diaryAt := tile.Tile{X: 2900, Z: 3473, Level: 1}
		if !waitUntil(d, 25*time.Second, func() bool { return len(questrunner.GroundItemsNear(d, questplan.ObjWitchDiary, diaryAt, 2)) > 0 }) {
			logf(d, "no diary on the bedroom floor (it respawns in 30 ticks)")
			return false
		}
		if !questrunner.TakeGround(d, questplan.ObjWitchDiary, diaryAt, 2) {
			return false
		}
	}
	if err := questrunner.OpHeldByName(d, questplan.ObjWitchDiary, "Read"); err != nil {
		logf(d, "reading the diary: %v", err)
		return false
	}
	d.Delay(2 * time.Second)
	questrunner.ClearLeftoverModal(d)
	return true
}

// ---------------------------------------------------------------------------
// The garden
// ---------------------------------------------------------------------------

// gardenEnter crosses the back door from the porch (the walker opens it from stage 3).
func gardenEnter(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if questplan.InWitchGarden(here) {
		return true
	}
	return questrunner.Travel(d, questplan.GardenRing()[0], 0) && questplan.InWitchGarden(questrunner.CurrentTile(d))
}

// witchAt is the witch's x on her lane and the way she is walking (0 when unknown), or ok false
// when she is out of view (more than fifteen tiles away, which no gap window reaches).
func witchAt(d *questrunner.Deps) (x, dx int, ok bool) {
	w, found := questrunner.FindNpcByID(d, questplan.NpcNoraHagg, "", 20)
	if !found {
		return 0, 0, false
	}
	d.Delay(700 * time.Millisecond)
	w2, found := questrunner.FindNpcByID(d, questplan.NpcNoraHagg, "", 20)
	if !found {
		return w.X, 0, true
	}
	switch {
	case w2.X > w.X:
		dx = 1
	case w2.X < w.X:
		dx = -1
	}
	return w2.X, dx, true
}

// gardenWalk walks the ring to dest, crossing each exposed gap only while the witch is clear of
// its catch window. A catch teleports the account out beside the boy; that ends the walk.
func gardenWalk(d *questrunner.Deps, dest tile.Tile) bool {
	for _, hop := range questplan.PlanGardenWalk(questrunner.CurrentTile(d), dest) {
		if !questplan.InWitchGarden(questrunner.CurrentTile(d)) {
			logf(d, "left the garden mid-walk (caught by the witch?) at %v", questrunner.CurrentTile(d))
			return false
		}
		if !questrunner.WalkNear(d, hop.Stand, 0) {
			logf(d, "the garden walk could not stand on %v from %v", hop.Stand, questrunner.CurrentTile(d))
			return false
		}
		if hop.Gap < 0 {
			continue
		}
		g := questplan.GardenGaps[hop.Gap]
		clear := func() bool {
			x, dx, seen := witchAt(d)
			return !seen || questplan.WitchClear(g, x, dx, hop.Tiles)
		}
		if !waitUntil(d, 120*time.Second, clear) {
			logf(d, "the witch never cleared the gap at x %d-%d, z %d", g.X0, g.X1, g.Z)
			return false
		}
		if !questrunner.WalkNear(d, hop.Cross, 0) {
			return false
		}
	}
	here := questrunner.CurrentTile(d)
	return here.X == dest.X && here.Z == dest.Z
}

// witchFountain walks the garden to the fountain and checks its compartment for the shed key.
func witchFountain(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !gardenEnter(d) || !gardenWalk(d, questplan.WitchFountStand) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWitchFount, "", "Check", questplan.WitchFountTile, 3)
	if !ok {
		logf(d, "the fountain is not in the loc index")
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Check"); err != nil {
		logf(d, "check the fountain from %v: %v", questrunner.CurrentTile(d), err)
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjWitchShedKey) }, nil, 10*time.Second)
	questrunner.ClearLeftoverModal(d)
	if held(d, questplan.ObjWitchShedKey) {
		return true
	}
	// Live 6 to 15 Oct 2026: 262 runs ended here with no line saying why. [oploc2,witchfountain] answers "There is
	// nothing in the fountain." and gives nothing to an account that owns the key ANYWHERE (obj_gettotal: pack, worn
	// and bank), and every one of those accounts held it in the bank, where a Stage 5 deposit-all had put it.
	said := ""
	if d.Chat != nil {
		said = strings.ToLower(d.Chat.Since(mark))
	}
	switch {
	case strings.Contains(said, "nothing in the fountain"):
		logf(d, "the fountain says it holds nothing: the shed key is already owned, so it is in the bank (the engine counts the bank)")
		e.RequestPark("the shed key is in the bank, which the fountain counts: the next run draws it", witchBankParkFor)
	case questrunner.FreeSlots(d) == 0:
		logf(d, "the pack is full: the fountain's key had no slot")
	default:
		logf(d, "no shed key from the fountain at %v (heard %q)", questrunner.CurrentTile(d), said)
	}
	return false
}

// witchBankParkFor is how long a quest parks when an item it needs is in the bank and the bank was not
// read this run: the next run's kit visit reads it.
const witchBankParkFor = 20 * time.Minute

// enterShed crosses the shed door: with the key used on it before stage 6 (which also spawns
// the glob), with a plain Open after.
func enterShed(d *questrunner.Deps, useKey bool) bool {
	if questplan.InWitchShed(questrunner.CurrentTile(d)) {
		return true
	}
	if !gardenEnter(d) || !gardenWalk(d, questplan.WitchShedStand) {
		return false
	}
	op := "Open"
	if useKey {
		op = ""
	}
	m, ok := questrunner.FindLoc(d, questplan.LocWitchShed, "", op, questplan.WitchShedDoor, 2)
	if !ok {
		logf(d, "the shed door is not in the loc index")
		return false
	}
	var err error
	if useKey {
		err = questrunner.UseItemOnLoc(d, questplan.ObjWitchShedKey, m)
	} else {
		err = questrunner.OperateLoc(d, m, "Open")
	}
	if err != nil {
		logf(d, "the shed door: %v", err)
		return false
	}
	ok = questrunner.NewDialogue(d).DriveUntil(func() bool { return questplan.InWitchShed(questrunner.CurrentTile(d)) }, nil, 12*time.Second)
	questrunner.ClearLeftoverModal(d)
	if !ok {
		logf(d, "not let into the shed (someone inside?)")
	}
	return ok
}

// form is the shapeshifter's current form in the shed.
func form(d *questrunner.Deps) (entityquery.Entity, bool) {
	var best entityquery.Entity
	found := false
	for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(12).Results() {
		for _, id := range questplan.WitchForms {
			if n.ID == id && (!found || n.Distance < best.Distance) {
				best, found = n, true
			}
		}
	}
	return best, found
}

// witchShed enters the shed with the key and casts the shapeshifter through its four forms:
// the glob from outside its maxrange, the rest from the one-front tile below the corridor.
func witchShed(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	// Never into the shed without a spell to cast: an account that parks inside cannot open its
	// door again (live 27 Sep, runebatch14: acct68 parked in the shed with no runes, then "Door at
	// 2934,3463 did not open" 392 times, and every other account got "not let into the shed").
	// Outside, the park is harmless and the kit step restocks the runes on the next attempt.
	if _, ok := questrunner.BestCastableSpell(d, 1); !ok {
		if questplan.InWitchShed(questrunner.CurrentTile(d)) {
			logf(d, "in the shed with no castable combat spell: leaving it")
			leaveShed(d)
		} else {
			logf(d, "no castable combat spell carried: not entering the shed")
		}
		return false
	}
	// And not with a pack that cannot finish it. The decision judges the same thing before the garden walk; this
	// is the pack as it stands at the door, and the only judge of an account that arrives here another way. Live
	// 6 to 15 Oct 2026: five deaths in here, each after a failed kit (Wind Strike's 142 casts, no food, 11 casts of
	// runes). An account already inside is not judged by it: its fight has its own rules (DecideWitchShed).
	if !questplan.InWitchShed(questrunner.CurrentTile(d)) {
		if v := questplan.WitchShedReady(shedSnapshot(d)); !v.OK {
			logf(d, "not entering the shed: %s", v.Why)
			return false
		}
	}
	if !enterShed(d, true) {
		return false
	}
	mark := time.Now()
	killed := func() bool {
		return d.Chat != nil && strings.Contains(strings.ToLower(d.Chat.Since(mark)), "once and for all")
	}
	sus := questplan.WitchsHouse.Sustain
	deadline := time.Now().Add(8 * time.Minute)
	idleSince := time.Time{}
	casts := 0
	for time.Now().Before(deadline) && !killed() {
		if questrunner.DiedSince(d, mark) {
			logf(d, "died in the shed after %d casts", casts)
			return false
		}
		if !questplan.InWitchShed(questrunner.CurrentTile(d)) {
			logf(d, "out of the shed mid-fight at %v", questrunner.CurrentTile(d))
			return false
		}
		// The fight is given up while the account can still walk out, not when it is dead: one account cast on with
		// 6 hitpoints and no food, two more with 14 and with 9, and another with no spell left.
		hp, maxHP := questrunner.Hitpoints(d)
		_, canCast := questrunner.BestCastableSpell(d, 1)
		if v := questplan.DecideWitchShed(questplan.WitchShedReading{HP: hp, MaxHP: maxHP, EatAt: sus.EatAt,
			Food: sus.FoodHeld(questrunner.PackCounts(d)), Casts: casts, CanCast: canCast}); v.Action == questplan.FightRetreat {
			questrunner.RetreatFromFight(d, "the Witch's House shapeshifter", v.Why)
			return false
		}
		npc, ok := form(d)
		if !ok {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) > 15*time.Second {
				// With no form present, touching the ball summons a fresh glob.
				logf(d, "no shapeshifter in the shed: touching the ball to summon one")
				questrunner.WalkNear(d, questplan.WitchBallStand, 0)
				questrunner.TakeGround(d, questplan.ObjWitchBall, tile.Tile{X: 2935, Z: 3460}, 1)
				questrunner.ClearLeftoverModal(d)
				idleSince = time.Now()
			}
			d.Delay(600 * time.Millisecond)
			continue
		}
		idleSince = time.Time{}
		hold := questplan.WitchOneFront
		if npc.ID == questplan.WitchForms[0] {
			hold = questplan.WitchGlobSpot
		}
		if here := questrunner.CurrentTile(d); here.X != hold.X || here.Z != hold.Z {
			questrunner.WalkNear(d, hold, 0)
		}
		questrunner.EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
		spell, ok := questrunner.BestCastableSpell(d, 1)
		if !ok {
			logf(d, "no castable combat spell left in the shed after %d casts", casts)
			return false
		}
		if err := actions.CastOnNpc(d.Session, spell.Name, npc.Index); err != nil {
			logf(d, "cast %s: %v", spell.Name, err)
			return false
		}
		casts++
		if !questrunner.WaitCast(d) {
			return false
		}
	}
	e.InvalidateStage()
	if !killed() {
		logf(d, "the shapeshifter outlived the fight after %d casts", casts)
		// Out of time with a form on the account: walk out, count it as a lost fight.
		questrunner.RetreatFromFight(d, "the Witch's House shapeshifter", fmt.Sprintf("the forms are still standing after %d casts", casts))
		return false
	}
	logf(d, "the shapeshifter is dead after %d casts", casts)
	return takeBall(d)
}

// shedSnapshot is the pack as the shed's gate (questplan.WitchShedReady) reads it.
func shedSnapshot(d *questrunner.Deps) questplan.Snapshot {
	s := questrunner.BuildSnapshot(d, questplan.WitchsHouseName, 0)
	s.HP, s.MaxHP = questrunner.Hitpoints(d)
	s.Levels = questrunner.BaseLevels(d)
	return s
}

// takeBall picks the ball off the crate once the shapeshifter is dead.
func takeBall(d *questrunner.Deps) bool {
	if held(d, questplan.ObjWitchBall) {
		return true
	}
	questrunner.WalkNear(d, questplan.WitchBallStand, 0)
	at := tile.Tile{X: 2935, Z: 3460}
	if !waitUntil(d, 30*time.Second, func() bool { return len(questrunner.GroundItemsNear(d, questplan.ObjWitchBall, at, 1)) > 0 }) {
		logf(d, "no ball on the crate (it respawns on its map timer)")
		return false
	}
	questrunner.TakeGround(d, questplan.ObjWitchBall, at, 1)
	questrunner.ClearLeftoverModal(d)
	return waitUntil(d, 6*time.Second, func() bool { return held(d, questplan.ObjWitchBall) })
}

// witchBall goes back into the shed after stage 6 (a plain Open now) for the ball.
func witchBall(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !enterShed(d, false) {
		return false
	}
	return takeBall(d)
}

// witchOut leaves the shed and walks the garden back to the back door with the ball; the long
// walk to the boy then leaves the house by its doors.
// witchBackDoorInside is the house tile past the garden's back door (loc 2862 at 2901,3465).
var witchBackDoorInside = tile.Tile{X: 2901, Z: 3466}

// LeaveWitchShed walks an account that stands in the Witch's House shed out through its door, and
// reports whether it is out. Every Stage 5 pass calls it first: live 27 Sep, an account trapped in
// the shed ran the NEXT quest (Murder Mystery), whose generic walk cannot open the shed door from
// inside ("Door at 2934,3463 did not open", 392 times), so the Witch's House step's own exit never ran.
func LeaveWitchShed(d *questrunner.Deps) bool {
	if !questplan.InWitchShed(questrunner.CurrentTile(d)) {
		return true
	}
	logf(d, "standing in the Witch's House shed: leaving it first")
	return leaveShed(d)
}

// leaveShed opens the shed door from inside and waits to be out of the shed.
func leaveShed(d *questrunner.Deps) bool {
	questrunner.WalkNear(d, questplan.WitchShedDoor, 0)
	m, ok := questrunner.FindLoc(d, questplan.LocWitchShed, "", "Open", questplan.WitchShedDoor, 2)
	if !ok {
		logf(d, "no shut shed door (loc %d) near %v from %v", questplan.LocWitchShed, questplan.WitchShedDoor, questrunner.CurrentTile(d))
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		logf(d, "opening the shed door from %v: %v", questrunner.CurrentTile(d), err)
		return false
	}
	if waitUntil(d, 10*time.Second, func() bool { return !questplan.InWitchShed(questrunner.CurrentTile(d)) }) {
		return true
	}
	// Content (quest_ball.rs2): Open answers "The shed door is locked." until the experiment is
	// beaten; before that the shed key used on the door is the way through, both ways. Live 7 Oct
	// 2026, Psycho sat inside for hours retrying Open.
	if questrunner.HeldID(d, questplan.ObjWitchShedKey) > 0 {
		logf(d, "the shed door stays locked from %v: using the shed key on it", questrunner.CurrentTile(d))
		if err := questrunner.UseItemOnLoc(d, questplan.ObjWitchShedKey, m); err != nil {
			logf(d, "using the shed key on the door: %v", err)
			return false
		}
		if waitUntil(d, 10*time.Second, func() bool { return !questplan.InWitchShed(questrunner.CurrentTile(d)) }) {
			return true
		}
	}
	logf(d, "still in the shed after opening its door, at %v (shed key held: %v)", questrunner.CurrentTile(d), questrunner.HeldID(d, questplan.ObjWitchShedKey) > 0)
	return false
}

func witchOut(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	// witch-empty-4 parked here after the shapeshifter with no log line: every failure now says
	// where the account stood.
	if questplan.InWitchShed(questrunner.CurrentTile(d)) && !leaveShed(d) {
		return false
	}
	if !gardenWalk(d, questplan.GardenRing()[0]) {
		logf(d, "the garden walk out did not reach %v; at %v", questplan.GardenRing()[0], questrunner.CurrentTile(d))
		return false
	}
	// The ring's first tile is still the garden: the way out is the house's back door on it,
	// which the webwalker opens (as gardenEnter crosses it inward). witch-empty-6 stood on that
	// tile and "left" the garden eight times without going through the door.
	if !questrunner.Travel(d, witchBackDoorInside, 0) || questplan.InWitchGarden(questrunner.CurrentTile(d)) {
		logf(d, "could not go back through the house's back door; at %v", questrunner.CurrentTile(d))
		return false
	}
	return true
}

// The shed is a pocket for every quest walk too: its door opens only from the shed's own step.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Witch's House shed", In: questplan.InWitchShed, Leave: leaveShed})
}
