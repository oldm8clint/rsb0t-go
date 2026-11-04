package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// DruidicRitual is Druidic Ritual ready for the engine. Its talks (Kaqemeex, Sanfew), its bank pass and Wydin's shelf (the
// provisioner's kit) are generic steps; the customs are the parts the content makes special: one hunt for each meat the world has
// to give (a kill and the drop taken from the ground), the way down the Taverley dungeon through the door whose suits of armour
// come to life, and the dipping of the raw meats in the Cauldron of Thunder.
func DruidicRitual() questrunner.Quest {
	customs := map[string]questrunner.CustomFunc{
		questplan.CustomDruidEnter: druidEnter,
		questplan.CustomDruidDip:   druidDip,
	}
	for _, m := range questplan.DruidMeats {
		customs[questplan.CustomDruidHunt(m)] = func(e *questrunner.Engine, _ questplan.Step) bool { return druidHunt(e, m) }
	}
	return questrunner.Quest{Module: questplan.DruidicRitual, Customs: customs}
}

const (
	// druidHuntBudget bounds one hunt: the animals are certain drops, and an hour of the fleet's goblin farm says a kill takes a
	// minute of walking and fighting at the worst; this is several times a bear's.
	druidHuntBudget = 12 * time.Minute
	// druidFightBudget bounds one fight (a bear has 27 hitpoints, the rest fewer); one still standing after this is one the account
	// is not reaching.
	druidFightBudget = 90 * time.Second
	// druidNoHitAfter is how long a fight may go without a single hit landing (a health bar drawn) before the account gives it up:
	// an animal behind a fence never gets one.
	druidNoHitAfter = 30 * time.Second
	// druidBusyAfter is how long into a fight an animal that another player is fighting is given up.
	druidBusyAfter = 4 * time.Second
	// druidSkipFor and druidSkipBusy are how long an animal given up on is left alone (by NPC index), and one another player was
	// fighting.
	druidSkipFor  = 2 * time.Minute
	druidSkipBusy = 20 * time.Second
	// druidSearch is how far from the account an animal may be and still be chosen; druidStray how far beyond the field's radius it
	// may stand (a bear that has wandered towards the Draynor jail is left).
	druidSearch = 24
	druidStray  = 6
	// druidReachWindow is the square (half its side) the open-ground flood covers round the account; druidReachEvery the longest a
	// flood is reused while the account stands where it was taken.
	druidReachWindow = 34
	druidReachEvery  = 2 * time.Second
	// druidIdleLooks is how many looks in a row may find no animal before the hunt walks back to its field.
	druidIdleLooks = 8
	// druidSayEvery paces the hunt's progress line.
	druidSayEvery = 3 * time.Minute
	// druidDoorTries is how many times the Prison door is opened (two suits, then the door itself) and the suits fought between.
	druidDoorTries = 5
	// druidDoorSpare is how many fights with the corridor's skeletons, beyond the one for each of the skeletons the price counts
	// (questplan.DruidCorridorSkeletons), the account may fight at the door before it is called stuck: the ones that followed it down
	// the hall come one at a time (the dungeon is single-combat) and none of those fights is a try at the door.
	druidDoorSpare = 2
	// druidSuitBudget bounds a fight with one suit of armour (29 hitpoints, a hit of 3).
	druidSuitBudget = 2 * time.Minute
	// druidDipWait is how long a dip gets to turn the meat.
	druidDipWait = 8 * time.Second
)

// meatHunt is one hunt's state.
type meatHunt struct {
	d *questrunner.Deps
	m questplan.DruidMeat
	// skip are animals given up on (by NPC index), until when.
	skip map[int]time.Time
	// reach is the open ground round reachFrom, taken at reachAt; the flood is reused while the account stands still.
	reach     *pathfind.Reach
	reachFrom tile.Tile
	reachAt   time.Time
	idle      int
	// The hunt's tally, for its closing line.
	won, gaveUp, timedOut, nothing int
}

// have reports the meat in the pack in either form.
func (h *meatHunt) have() bool {
	return questrunner.HeldID(h.d, h.m.Raw) > 0 || questrunner.HeldID(h.d, h.m.Enchanted) > 0
}

// floodFrom is the open ground round here, nil when the pack is not loaded or the account stands off it (the callers then fall
// back to distance).
func (h *meatHunt) floodFrom(here tile.Tile) *pathfind.Reach {
	if h.d.Pack == nil {
		return nil
	}
	if h.reach != nil && h.reachFrom == here && time.Since(h.reachAt) < druidReachEvery {
		return h.reach
	}
	reach := h.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, druidReachWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	h.reach, h.reachFrom, h.reachAt = reach, here, time.Now()
	return reach
}

// stepsTo is the steps from here to a tile the engine accepts a melee attack on whatever stands at t from, over open ground (-1
// for none; see interactSteps).
func (h *meatHunt) stepsTo(here, t tile.Tile) int {
	return interactSteps(h.d.Pack, h.floodFrom(here), here, t)
}

// pick is the animal to fight now, among those in view: one of the meat's kind that no other player is fighting, not given up on,
// not strayed from the field, and a walk over open ground away. The nearest by that walk wins, and between equals the lower NPC
// index, so two accounts looking at the same field do not oscillate.
func (h *meatHunt) pick() (entityquery.Entity, bool) {
	d := h.d
	ids := map[int]bool{}
	for _, id := range h.m.Npcs {
		ids[id] = true
	}
	now, self, here := time.Now(), d.Session.State().Self(), questrunner.CurrentTile(d)
	var best entityquery.Entity
	bestSteps, found := 0, false
	for _, e := range entityquery.Npcs(d.Session.State(), d.Config).Within(druidSearch).Results() {
		at := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
		if !ids[e.ID] || !now.After(h.skip[e.Index]) || goblinBusy(e, self) || tile.Chebyshev(at, h.m.Field) > h.m.Radius+druidStray {
			continue
		}
		n := h.stepsTo(here, at)
		if n < 0 {
			continue
		}
		if !found || n < bestSteps || (n == bestSteps && e.Index < best.Index) {
			best, bestSteps, found = e, n, true
		}
	}
	return best, found
}

// giveUp says why a fight with cur (the animal as it stands now), begun at began, is not worth going on with, "" when it is, and
// how long the animal is then left alone. steps is the walk to it over open ground now (-1 for none).
func (h *meatHunt) giveUp(cur entityquery.Entity, self int, began, now time.Time, steps int) (why string, skip time.Duration) {
	since := now.Sub(began)
	switch {
	case tile.Chebyshev(tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}, h.m.Field) > h.m.Radius+2*druidStray:
		return "it has wandered off the field", druidSkipFor
	case steps < 0:
		return "no walk over open ground reaches it", druidSkipFor
	case since >= druidBusyAfter && goblinBusy(cur, self):
		return "another player is fighting it", druidSkipBusy
	case since >= druidNoHitAfter && cur.TotalHealth == 0:
		return "no blow has landed on it", druidSkipFor
	}
	return "", 0
}

// fight fights one animal and takes the meat it dropped.
func (h *meatHunt) fight(e entityquery.Entity) questrunner.FightResult {
	d := h.d
	began := time.Now()
	why, skip := "", druidSkipFor
	last := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
	giveUp := func() bool {
		cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(druidSearch + 4).
			Where(func(n entityquery.Entity) bool { return n.Index == e.Index && n.ID == e.ID }).Nearest()
		if !ok {
			return false
		}
		last = tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
		w, s := h.giveUp(cur, d.Session.State().Self(), began, time.Now(), h.stepsTo(questrunner.CurrentTile(d), last))
		if w != "" {
			why, skip = w, s
		}
		return w != ""
	}
	res := questrunner.FightChosen(d, e, questplan.DruidicRitual.Sustain, druidFightBudget, giveUp)
	switch res {
	case questrunner.FightWon:
		h.won++
		// The animal drops its meat where it dies, which is where it was last seen.
		if questrunner.TakeGround(d, h.m.Raw, last, 3) || h.have() {
			logf(d, "druid: took the raw %s meat from a kill at %d,%d", h.m.Name, last.X, last.Z)
		} else {
			h.nothing++
			logf(d, "druid: the %s at %d,%d died and no %s was found beside it", h.m.Name, last.X, last.Z, h.m.RawName)
		}
	case questrunner.FightGaveUp:
		h.gaveUp++
		h.skip[e.Index] = time.Now().Add(skip)
		logf(d, "druid: gave up on the %s at %d,%d after %s: %s", h.m.Name, e.X, e.Z, time.Since(began).Round(time.Second), why)
	case questrunner.FightTimedOut:
		h.timedOut++
		h.skip[e.Index] = time.Now().Add(druidSkipFor)
		logf(d, "druid: the %s at %d,%d is still standing after %s; leaving it for %s", h.m.Name, e.X, e.Z, druidFightBudget, druidSkipFor)
	}
	return res
}

// druidHunt kills the animals of one meat, in the field the plan names, until the pack holds the meat (or the budget runs out),
// and reports whether it does. The foe is chosen by a walk over open ground to a tile the engine accepts an attack from, never by
// distance (a chicken in the coop, a cow behind the dairy fence, a bear across the river are in view and out of reach); a fight is
// a minute and a half at most and is given up when no blow has landed in thirty seconds or when another player is the one fighting
// the animal; a lost fight ends the hunt (the engine counts it against the quest and walks the account to a bank). The Decide has
// priced the bear and refused the hunt when the model loses.
func druidHunt(e *questrunner.Engine, m questplan.DruidMeat) bool {
	d := e.Deps()
	h := &meatHunt{d: d, m: m, skip: map[int]time.Time{}}
	if h.have() {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "druid: no room in the pack for a raw %s meat", m.Name)
		return false
	}
	if !questrunner.Travel(d, m.Field, 6) {
		logf(d, "druid: the %s field at %v was not reached from %v", m.Name, m.Field, questrunner.CurrentTile(d))
		return false
	}
	t0 := time.Now()
	deadline := t0.Add(druidHuntBudget)
	saidAt := t0
	logf(d, "druid: hunting for a raw %s meat in the field at %v, up to %s", m.Name, m.Field, druidHuntBudget)
	for !h.have() && time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		if time.Since(saidAt) >= druidSayEvery {
			saidAt = time.Now()
			logf(d, "druid: %s in: %d %s killed, %d given up on, %d timed out, %d drops missed", time.Since(t0).Round(time.Second), h.won, m.Name, h.gaveUp, h.timedOut, h.nothing)
		}
		// Meat lying about from a kill whose pickup missed it (ours, or another account's: it is the same item).
		if questrunner.TakeGround(d, m.Raw, questrunner.CurrentTile(d), 6) {
			continue
		}
		target, ok := h.pick()
		if !ok {
			h.idle++
			if h.idle == 1 || h.idle%20 == 0 {
				logf(d, "druid: no %s in reach of %v; looking again", m.Name, questrunner.CurrentTile(d))
			}
			if h.idle%druidIdleLooks == 0 || tile.Chebyshev(questrunner.CurrentTile(d), m.Field) > m.Radius {
				questrunner.Travel(d, m.Field, 6)
			}
			if !d.Delay(3 * time.Second) {
				break
			}
			continue
		}
		h.idle = 0
		switch h.fight(target) {
		case questrunner.FightStopped:
			return h.have()
		case questrunner.FightLost:
			logf(d, "druid: the fight with a %s was lost; ending the hunt", m.Name)
			return h.have()
		}
	}
	logf(d, "druid: the hunt for the %s ends after %s with %d killed, %d given up on, %d timed out: held %v", m.Name, time.Since(t0).Round(time.Second),
		h.won, h.gaveUp, h.timedOut, h.have())
	return h.have()
}

// druidSuit is the animated suit of armour that is fighting the account, if any: the door raises a suit for whoever opens it and
// sets it on that account (~npc_retaliate), so a suit in view that is not facing the account is another account's (the door's suits
// are the world's, and a second account within five hundred ticks of the first has the second suit), and attacking it would be
// refused ("Someone else is fighting that") while the account's own suit hit it.
func druidSuit(d *questrunner.Deps) (entityquery.Entity, bool) {
	npc, hit := questrunner.Attacker(d)
	return npc, hit && npc.ID == questplan.NpcSuitOfArmour
}

// druidFightFollower says whether an NPC that is attacking the account at the cauldron's door is one to fight back: not the door's
// suit of armour (the door loop fights that one itself, on its own budget), not one past the cap on these fights, and not one the
// account's combat level is not within ten of (the poison spider, level 64, is walked away from). An unread level on either side is no.
func druidFightFollower(fights, maxFights, npcID, npcCombat, own int, ownKnown bool) bool {
	return fights < maxFights && npcID != questplan.NpcSuitOfArmour && ownKnown && npcCombat > 0 && npcCombat <= own+10
}

// druidEnter takes the account down the Taverley dungeon and through the Prison door to the Cauldron of Thunder's room, and
// reports whether it stands in it. The top-up comes first (the food the model counted is eaten to the level a fight starts at); the
// walk is the planner's (the ladder, the hall of skeletons the price counts: questplan.DruidDungeonOdds), a plain Travel that runs when
// attacked and eats by the floor, not a cautious one (a cautious walk plans round each attacker's square, which a hall the only way
// through cannot allow, and a walk bent to the wall passes a poison spider instead); the skeletons that followed it to the door are
// fought back one at a time, none of those fights a try at the door; the door is its own step, because opening it from the corridor
// raises a suit of armour that attacks the account and leaves the door shut (prison_doors.rs2): the suit is fought through the shared
// fight step, and the door is tried again, up to twice, until it opens. A door that cannot be opened in druidDoorTries asks for a park.
func druidEnter(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
		return true
	}
	sus := questplan.DruidicRitual.Sustain
	if _, max := questrunner.Hitpoints(d); max > 0 {
		questrunner.TopUp(d, sus.FoodIDs, questplan.FightStartHP(max, questplan.FoeSuitOfArmour.PeakHit()))
	}
	if !questrunner.Travel(d, questplan.CauldronDoorOutside, 0) {
		logf(d, "druid: the cauldron's door (%v) was not reached from %v", questplan.CauldronDoorOutside, questrunner.CurrentTile(d))
		return false
	}
	fights, maxFights := 0, len(questplan.DruidCorridorSkeletons)+druidDoorSpare
	for try := 1; try <= druidDoorTries; try++ {
		if questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
			return true
		}
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		// Whatever is hitting the account while it stands at the door is dealt with before the door is touched, unless it is a foe
		// the model did not price (a poison spider is walked away from, not fought). The skeletons that followed the account down the
		// hall come one at a time; each is fought to its end here and does not use up a try at the door, within the number the price
		// counts and a little over.
		for {
			npc, hit := questrunner.Attacker(d)
			own, ownKnown := d.Session.State().CombatLevel()
			if !hit || !druidFightFollower(fights, maxFights, npc.ID, npc.CombatLevel, own, ownKnown) {
				break
			}
			fights++
			logf(d, "druid: %s is attacking at the cauldron's door: fighting back (fight %d of at most %d)", npc.Name, fights, maxFights)
			questrunner.FightBack(d, npc, sus)
			if !questrunner.WaitForGuardian(d) {
				return false
			}
		}
		if suit, ok := druidSuit(d); ok {
			logf(d, "druid: a suit of armour has come to life (try %d of %d): fighting it", try, druidDoorTries)
			switch questrunner.FightChosen(d, suit, sus, druidSuitBudget, nil) {
			case questrunner.FightWon:
				continue
			default:
				e.RequestPark("the suit of armour at the cauldron's door was not beaten", questplan.DruidDungeonPark)
				return false
			}
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCauldronDoor, "", "Open", questplan.CauldronDoorOutside, 3)
		if !ok {
			m, ok = questrunner.FindLoc(d, questplan.LocCauldronDoorLeft, "", "Open", questplan.CauldronDoorOutside, 3)
		}
		if !ok {
			// The leaf is not the shut one any more: another account's opening has it standing open. Walk in.
			if questrunner.Travel(d, questplan.CauldronStand, 1) && questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
				return true
			}
			logf(d, "druid: no shut Prison door near %v and the room was not walked into", questplan.CauldronDoorOutside)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "druid: opening the Prison door: %v", err)
			return false
		}
		// The door either lets the account in (the script moves it through) or raises a suit that attacks it at once.
		waitUntil(d, 8*time.Second, func() bool {
			if questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
				return true
			}
			_, onUs := druidSuit(d)
			return onUs
		})
	}
	if questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
		return true
	}
	e.RequestPark("the Prison door of the Cauldron of Thunder did not open after the suits of armour were fought", questplan.DruidDungeonPark)
	return false
}

// druidDip dips every raw meat the pack holds in the Cauldron of Thunder (used on it, one at a time: the content turns one meat
// for each use, and only at Sanfew's stage) and reports whether every meat is now in its enchanted form. It stands where the
// plan put it (the room); the walk out is the next step's, the door opening from inside with no suit to raise.
func druidDip(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InCauldronRoom(questrunner.CurrentTile(d)) {
		logf(d, "druid: not in the cauldron's room (at %v)", questrunner.CurrentTile(d))
		return false
	}
	cauldron, ok := questrunner.FindLoc(d, questplan.LocCauldronOfThunder, "", "", questplan.CauldronStand, 6)
	if !ok {
		logf(d, "druid: no Cauldron of Thunder near %v", questplan.CauldronStand)
		return false
	}
	all := true
	for _, m := range questplan.DruidMeats {
		if questrunner.HeldID(d, m.Enchanted) > 0 || questrunner.HeldID(d, m.Raw) == 0 {
			all = all && questrunner.HeldID(d, m.Enchanted) > 0
			continue
		}
		done := false
		for try := 1; try <= 3 && !done; try++ {
			if err := questrunner.UseItemOnLoc(d, m.Raw, cauldron); err != nil {
				logf(d, "druid: the raw %s meat on the cauldron: %v", m.Name, err)
				break
			}
			done = waitUntil(d, druidDipWait, func() bool { return questrunner.HeldID(d, m.Enchanted) > 0 })
			questrunner.ClearLeftoverModal(d)
		}
		if done {
			logf(d, "druid: the %s meat is enchanted", m.Name)
		}
		all = all && done
	}
	return all
}
