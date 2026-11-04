package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Observatory is the Observatory Quest ready for the engine. The professor's talks, the hand-overs, the bank and the mule, the
// planks and the sand off the ground, the bronze bar and the molten glass at the furnace and the lens from the mould are the engine's
// generic steps; the customs are the four legs the content makes special: the dungeon trip for the lens mould (the ladder behind the
// assistant's warning, the goblin guard, the keep gate and its key, the sack), the soda ash on the range, the way out of the dungeon,
// and the trip to the telescope.
func Observatory() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Observatory,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomObsMould:     obsMould,
			questplan.CustomObsTelescope: obsTelescope,
			questplan.CustomObsAsh:       obsAsh,
			questplan.CustomObsLeave:     func(e *questrunner.Engine, _ questplan.Step) bool { return obsLeaveAll(e.Deps()) },
		},
	}
}

const (
	// obsLadderBudget bounds the climb down: the assistant's two lines of warning, then the teleport.
	obsLadderBudget = 25 * time.Second
	// obsGateWait is how long the gate gets to move the account one tile (a p_teleport) or say it is locked.
	obsGateWait = 8 * time.Second
	// obsFightBudget bounds the fight with the goblin guard (43 hitpoints, a hit of up to 5 every 4 ticks): the model's kill takes a
	// minute or two at the levels the odds accept, so four minutes is a fight that is not being won.
	obsFightBudget = 4 * time.Minute
	// obsGuardBusyAfter is how long into the fight a guard that another player holds is given up: the attack on him is refused
	// ("Someone else is fighting that") however often it is sent.
	obsGuardBusyAfter = 6 * time.Second
	// obsGuardNoHitAfter is how long a fight may go without a blow landing on the guard before it is given up.
	obsGuardNoHitAfter = 40 * time.Second
	// obsGuardSearch is how far from the account the guard may be and still be looked at.
	obsGuardSearch = 24
	// obsGuardRetry is the pause after a fight given up because another player held the guard.
	obsGuardRetry = 15 * time.Second
	// obsRoundWait is the wait for a chest or a loc another account has just changed to show itself.
	obsRoundWait = 8 * time.Second
	// obsTelescopeBudget bounds the conversation at the telescope: the professor's chat, the chart, the constellation's chats and
	// the reward, a minute and a half at the slowest.
	obsTelescopeBudget = 90 * time.Second
)

// atTile reports the account standing exactly on t.
func atTile(d *questrunner.Deps, t tile.Tile) bool { return questrunner.CurrentTile(d) == t }

// obsGuard is the goblin guard in view, if one stands.
func obsGuard(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(obsGuardSearch).
		Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcGoblinGuard }).Nearest()
}

// obsDescend climbs the reception's ladder down into the goblin cavern, answering the assistant on the way: the ladder is a
// p_telejump behind two lines of his warning (continue pages, no menu), so the climb is driven until the account is underground.
func obsDescend(d *questrunner.Deps) bool {
	if questplan.IsUnderground(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.ObsReceptionLadderStand, 1) {
		logf(d, "observatory: the reception ladder's stand %v was not reached from %v", questplan.ObsReceptionLadderStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsReceptionLadder, "", "Climb-down", questplan.ObsReceptionLadderStand, 4)
	if !ok {
		logf(d, "observatory: no ladder down (loc %d) near %v", questplan.LocObsReceptionLadder, questplan.ObsReceptionLadderStand)
		return false
	}
	logf(d, "observatory: climbing down into the goblin cavern")
	if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
		logf(d, "observatory: the ladder down: %v", err)
		return false
	}
	underground := func() bool { return questplan.IsUnderground(questrunner.CurrentTile(d)) }
	if !questrunner.NewDialogue(d).DriveUntil(underground, nil, obsLadderBudget) {
		logf(d, "observatory: the ladder down did not take the account underground (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1500 * time.Millisecond) // the scene of the cavern loads
	return true
}

// obsAscend climbs the cavern's ladder up to the reception (the account is out of the keep).
func obsAscend(d *questrunner.Deps) bool {
	if !questplan.IsUnderground(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.ObsCavernLanding, 1) {
		logf(d, "observatory: the cavern's way up (landing %v) was not reached from %v", questplan.ObsCavernLanding, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsCavernExit, "", "Climb-up", questplan.ObsCavernLanding, 4)
	if !ok {
		logf(d, "observatory: no ladder up (loc %d) near %v", questplan.LocObsCavernExit, questplan.ObsCavernLanding)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
		logf(d, "observatory: the ladder up: %v", err)
		return false
	}
	if !waitUntil(d, 15*time.Second, func() bool { return !questplan.IsUnderground(questrunner.CurrentTile(d)) }) {
		logf(d, "observatory: the ladder up did not take the account to the surface (at %v)", questrunner.CurrentTile(d))
		return false
	}
	logf(d, "observatory: back at the reception")
	return true
}

// obsGate is a half of the keep gate near a tile that offers Open.
func obsGate(d *questrunner.Deps, near tile.Tile) (locindex.Match, bool) {
	for _, id := range []int{questplan.LocObsGateLeft, questplan.LocObsGateRight} {
		if m, ok := questrunner.FindLoc(d, id, "", "Open", near, 4); ok {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// obsLeaveKeep opens the gate from the south (it always opens that way) and waits for the account to be one tile north of it.
func obsLeaveKeep(d *questrunner.Deps) bool {
	for try := 1; try <= 3; try++ {
		if !questplan.InObsKeep(questrunner.CurrentTile(d)) {
			return true
		}
		if !questrunner.Travel(d, questplan.ObsGateSouth, 0) && !atTile(d, questplan.ObsGateSouth) {
			logf(d, "observatory: the gate's south side %v was not reached from %v", questplan.ObsGateSouth, questrunner.CurrentTile(d))
			continue
		}
		m, ok := obsGate(d, questplan.ObsGateSouth)
		if !ok {
			// The gate is open for the three ticks another account's crossing leaves it so.
			waitUntil(d, obsRoundWait, func() bool { _, again := obsGate(d, questplan.ObsGateSouth); return again })
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "observatory: opening the gate from the keep: %v", err)
			continue
		}
		if waitUntil(d, obsGateWait, func() bool { return !questplan.InObsKeep(questrunner.CurrentTile(d)) }) {
			logf(d, "observatory: out of the keep")
			return true
		}
	}
	return !questplan.InObsKeep(questrunner.CurrentTile(d))
}

// obsFetchKey opens the one chest of the cavern that holds a key and searches it. Every lookup is by id: six of the cavern's chests
// look the same and spawn a poisonous spider when searched. The chest toggles shut and open for 300 ticks at a time, and another
// account's visit may have left it either way.
func obsFetchKey(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjObsKeepKey) > 0 {
		return true
	}
	stand := questplan.ObsKeyChestStand
	if !questrunner.Travel(d, stand, 1) {
		logf(d, "observatory: the key chest's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	for try := 1; try <= 3; try++ {
		if m, shut := questrunner.FindLoc(d, questplan.LocObsKeyChestShut, "", "Open", stand, 4); shut {
			logf(d, "observatory: opening the keep-key chest")
			if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "observatory: opening the chest: %v", err)
				return false
			}
			waitUntil(d, obsRoundWait, func() bool {
				_, open := questrunner.FindLoc(d, questplan.LocObsKeyChestOpen, "", "Search", stand, 4)
				return open
			})
		}
		open, ok := questrunner.FindLoc(d, questplan.LocObsKeyChestOpen, "", "Search", stand, 4)
		if !ok {
			logf(d, "observatory: neither the shut nor the open key chest (loc %d, %d) near %v", questplan.LocObsKeyChestShut, questplan.LocObsKeyChestOpen, stand)
			if !d.Delay(2 * time.Second) {
				return false
			}
			continue
		}
		mark := time.Now()
		if err := questrunner.OperateLoc(d, open, "Search"); err != nil {
			logf(d, "observatory: searching the chest: %v", err)
			return false
		}
		// The find is an objbox (~objbox(keep_key, "You find a keep key.")) that holds the script on its Continue: the key is added, and the
		// chest shut again, only after the box is continued, so closing it would drop the script and the key with it.
		dl := questrunner.NewDialogue(d)
		saidHeld := func() bool { return cookSaw(d, mark, "you already have a keep key") }
		waitUntil(d, obsRoundWait, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.ObjObsKeepKey) > 0 || saidHeld() })
		if dl.Ready() {
			dl.Drive(nil)
		}
		got := waitUntil(d, obsRoundWait, func() bool { return questrunner.HeldID(d, questplan.ObjObsKeepKey) > 0 || saidHeld() })
		questrunner.ClearLeftoverModal(d)
		if questrunner.HeldID(d, questplan.ObjObsKeepKey) > 0 {
			logf(d, "observatory: the keep key")
			return true
		}
		if got {
			logf(d, "observatory: the chest says the account already has a keep key (the bank holds one), and the pack has none")
			return false
		}
	}
	return false
}

// obsCrossGate opens the gate from its north side with the key when the pack holds one and without it when not, and waits for the
// account to be inside the keep. A locked gate ("The gate is locked.") sends the account to the key chest, and the gate is tried
// again with the key. It reports whether the account is in the keep.
func obsCrossGate(d *questrunner.Deps) bool {
	for round := 1; round <= 2; round++ {
		if questplan.InObsKeep(questrunner.CurrentTile(d)) {
			return true
		}
		if !questrunner.Travel(d, questplan.ObsGateNorth, 0) && !atTile(d, questplan.ObsGateNorth) {
			logf(d, "observatory: the gate's north side %v was not reached from %v", questplan.ObsGateNorth, questrunner.CurrentTile(d))
			return false
		}
		m, ok := obsGate(d, questplan.ObsGateNorth)
		if !ok {
			logf(d, "observatory: no keep gate (loc %d, %d) near %v", questplan.LocObsGateLeft, questplan.LocObsGateRight, questplan.ObsGateNorth)
			return false
		}
		mark := time.Now()
		var err error
		if questrunner.HeldID(d, questplan.ObjObsKeepKey) > 0 {
			logf(d, "observatory: unlocking the keep gate with the keep key")
			err = questrunner.UseItemOnLoc(d, questplan.ObjObsKeepKey, m)
		} else {
			logf(d, "observatory: trying the keep gate without a key")
			err = questrunner.OperateLoc(d, m, "Open")
		}
		if err != nil {
			logf(d, "observatory: the keep gate: %v", err)
			return false
		}
		if waitUntil(d, obsGateWait, func() bool {
			return questplan.InObsKeep(questrunner.CurrentTile(d)) || cookSaw(d, mark, "the gate is locked")
		}) && questplan.InObsKeep(questrunner.CurrentTile(d)) {
			logf(d, "observatory: through the keep gate")
			// Crossing south raises "I'd better be quick, there may be more guards about." (a chat to continue), which holds every op after it.
			if dl := questrunner.NewDialogue(d); waitUntil(d, 3*time.Second, dl.Ready) {
				dl.Drive(nil)
			}
			return true
		}
		if !cookSaw(d, mark, "the gate is locked") {
			logf(d, "observatory: the keep gate moved nobody and said nothing (at %v)", questrunner.CurrentTile(d))
			return false
		}
		logf(d, "observatory: the keep gate is locked: fetching the keep key")
		if !obsFetchKey(d) {
			return false
		}
	}
	return questplan.InObsKeep(questrunner.CurrentTile(d))
}

// obsGuardGiveUp says why a fight with the goblin guard (cur: the guard as it stands now), begun at began, is not worth going on
// with, "" when it is. steps is the walk to a tile a melee attack reaches him from over open ground now (-1 for none). He is a
// single-combat npc: the attack on one another player holds is refused however often it is sent.
func obsGuardGiveUp(cur entityquery.Entity, self int, began, now time.Time, steps int) string {
	since := now.Sub(began)
	switch {
	case steps < 0:
		return "no walk over open ground reaches a tile to strike him from"
	case since >= obsGuardBusyAfter && goblinBusy(cur, self):
		return "another player is fighting him"
	case since >= obsGuardNoHitAfter && cur.TotalHealth == 0:
		return "no blow has landed on him"
	}
	return ""
}

// obsClearGuard kills the goblin guard at the gate, which the content sets on the account the moment the gate moves, through the shared
// fight step (the eat line from his hardest hit, the top-up before the first blow, the race that gives up a fight being lost) and
// only if a walk over open ground reaches him. Decide has judged the odds (questplan.ObsGuardReady); a fight lost anyway is a
// near-death the engine counts, and one death at this step parks the quest for a day. It reports whether no guard stands.
func obsClearGuard(e *questrunner.Engine, d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.ObsGateNorth, 2) {
		logf(d, "observatory: the gate's north side %v was not reached from %v", questplan.ObsGateNorth, questrunner.CurrentTile(d))
		return false
	}
	r := &goblinRun{d: d}
	for round := 1; round <= 3; round++ {
		g, ok := obsGuard(d)
		if !ok {
			return true // dead, or respawning: the gate will not stir anyone
		}
		began := time.Now()
		why := ""
		giveUp := func() bool {
			cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(obsGuardSearch + 4).
				Where(func(n entityquery.Entity) bool { return n.Index == g.Index && n.ID == g.ID }).Nearest()
			if !ok {
				return false
			}
			here := questrunner.CurrentTile(d)
			why = obsGuardGiveUp(cur, d.Session.State().Self(), began, time.Now(), r.stepsTo(here, tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}))
			return why != ""
		}
		logf(d, "observatory: fighting the goblin guard at %d,%d (round %d)", g.X, g.Z, round)
		switch questrunner.FightChosen(d, g, questplan.Observatory.Sustain, obsFightBudget, giveUp) {
		case questrunner.FightWon:
			logf(d, "observatory: the goblin guard is down")
		case questrunner.FightGaveUp:
			logf(d, "observatory: gave up on the goblin guard after %s: %s", time.Since(began).Round(time.Second), why)
			if !d.Delay(obsGuardRetry) {
				return false
			}
		case questrunner.FightTimedOut:
			logf(d, "observatory: the goblin guard is still standing after %s", obsFightBudget)
			e.RequestPark("the goblin guard was still standing after the fight's budget", time.Hour)
			return false
		default:
			return false // lost (the engine counts the near-death) or stopped
		}
	}
	_, still := obsGuard(d)
	return !still
}

// obsSearchSack searches the sack in the keep for the lens mould. The content refuses it when the pack, the bank or the worn slots
// already hold one, so a refusal is the bank's mould (Decide draws it from the bank before the trip) and is not tried again.
func obsSearchSack(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjLensMould) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.ObsSackStand, 0) && !atTile(d, questplan.ObsSackStand) {
		logf(d, "observatory: the sack's stand %v was not reached from %v", questplan.ObsSackStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsSack, "", "Search", questplan.ObsSackStand, 3)
	if !ok {
		logf(d, "observatory: no sack (loc %d) near %v", questplan.LocObsSack, questplan.ObsSackStand)
		return false
	}
	mark := time.Now()
	logf(d, "observatory: searching the sack for the lens mould")
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "observatory: searching the sack: %v", err)
		return false
	}
	waitUntil(d, obsRoundWait, func() bool {
		return questrunner.HeldID(d, questplan.ObjLensMould) > 0 || cookSaw(d, mark, "you already have this lens mould")
	})
	if questrunner.HeldID(d, questplan.ObjLensMould) == 0 {
		logf(d, "observatory: the sack gave no mould [%s]", huntSaid(d, mark))
		return false
	}
	return true
}

// obsMould fetches the lens mould and brings it out: down the ladder, the guard, the gate (and the key when it is locked), the sack,
// out through the gate and up the ladder. Each stage is tried where the account stands, so a run that began half way (in the keep,
// in the cavern) finishes the same trip.
func obsMould(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjLensMould) == 0 {
		if !questplan.InObsKeep(questrunner.CurrentTile(d)) {
			if !obsDescend(d) {
				return false
			}
			if !obsClearGuard(e, d) {
				return false
			}
			if !obsCrossGate(d) {
				return false
			}
		}
		if !obsSearchSack(d) {
			obsLeaveKeep(d) // never leave the account in the keep
			return false
		}
	}
	if !obsLeaveKeep(d) {
		return false
	}
	return obsAscend(d) && questrunner.HeldID(d, questplan.ObjLensMould) > 0
}

// obsDomeLadderDown climbs the dome's ladder back down to the cavern.
func obsDomeLadderDown(d *questrunner.Deps) bool {
	if !questplan.InObsDome(questrunner.CurrentTile(d)) {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsDomeLadderDown, "", "Climb-down", questplan.ObsDomeArrive, 6)
	if !ok {
		logf(d, "observatory: no ladder down (loc %d) in the dome", questplan.LocObsDomeLadderDown)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
		logf(d, "observatory: the dome's ladder down: %v", err)
		return false
	}
	return waitUntil(d, 12*time.Second, func() bool { return !questplan.InObsDome(questrunner.CurrentTile(d)) })
}

// obsLeaveAll walks the account out of wherever the quest left it: the dome by its ladder, the keep by its gate, the cavern by its
// ladder up. It is the way out of the quest's dungeon for the quest itself and, as a pocket's Leave, for every other quest's walks.
func obsLeaveAll(d *questrunner.Deps) bool {
	if questplan.InObsDome(questrunner.CurrentTile(d)) && !obsDomeLadderDown(d) {
		return false
	}
	if !obsLeaveKeep(d) {
		return false
	}
	return obsAscend(d)
}

// obsAsh burns a seaweed to soda ash on the range (cooking_generic_seaweed always succeeds and burns to the same ash). A make menu,
// which a stack of seaweed raises, is answered with the ash.
func obsAsh(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjSodaAsh) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjSeaweed) == 0 {
		logf(d, "observatory: no seaweed to burn")
		return false
	}
	if !questrunner.Travel(d, questplan.ObsRangeAt, 3) {
		logf(d, "observatory: the range %v was not reached from %v", questplan.ObsRangeAt, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsRange, "", "", questplan.ObsRangeAt, 6)
	if !ok {
		logf(d, "observatory: no range (loc %d) near %v", questplan.LocObsRange, questplan.ObsRangeAt)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjSeaweed, m); err != nil {
		logf(d, "observatory: the seaweed on the range: %v", err)
		return false
	}
	if waitUntil(d, 6*time.Second, func() bool {
		return questrunner.HeldID(d, questplan.ObjSodaAsh) > 0 || questrunner.IsMakeMenu(d)
	}) && questrunner.IsMakeMenu(d) {
		_ = questrunner.MakeOne(d, "Soda ash")
	}
	if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjSodaAsh) > 0 }) {
		logf(d, "observatory: the range made no soda ash")
		return false
	}
	return true
}

// obsTelescope climbs the cavern's ladder into the dome, looks through the telescope with the dome's professor within seven tiles
// of the account, and drives what follows: his "Well done" chat, the star chart on the main modal (closed, because the queued
// constellation conversation cannot run while it is up) and the conversation itself (either option), to the quest scroll.
func obsTelescope(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InObsDome(questrunner.CurrentTile(d)) {
		if !obsDescend(d) {
			return false
		}
		if !questrunner.Travel(d, questplan.ObsDomeLadderFoot, 1) {
			logf(d, "observatory: the dome ladder's foot %v was not reached from %v", questplan.ObsDomeLadderFoot, questrunner.CurrentTile(d))
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocObsDomeLadderUp, "", "Climb-up", questplan.ObsDomeLadderFoot, 4)
		if !ok {
			logf(d, "observatory: no ladder up to the dome (loc %d) near %v", questplan.LocObsDomeLadderUp, questplan.ObsDomeLadderFoot)
			return false
		}
		logf(d, "observatory: climbing into the observatory dome")
		if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
			logf(d, "observatory: the ladder to the dome: %v", err)
			return false
		}
		if !waitUntil(d, 12*time.Second, func() bool { return questplan.InObsDome(questrunner.CurrentTile(d)) }) {
			logf(d, "observatory: the ladder did not take the account into the dome (at %v)", questrunner.CurrentTile(d))
			return false
		}
		d.Delay(1500 * time.Millisecond)
	}
	if _, near := entityquery.Npcs(d.Session.State(), d.Config).Within(14).
		Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcObsProfessorDome }).Nearest(); !near {
		logf(d, "observatory: the dome's professor is not in view: the telescope would not answer")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocObsTelescope, "", "Use", questplan.ObsTelescopeAt, 6)
	if !ok {
		logf(d, "observatory: no telescope (loc %d) near %v", questplan.LocObsTelescope, questplan.ObsTelescopeAt)
		return false
	}
	logf(d, "observatory: looking through the telescope")
	if err := questrunner.OperateLoc(d, m, "Use"); err != nil {
		logf(d, "observatory: the telescope: %v", err)
		return false
	}
	complete := func() bool {
		return questrunner.ReadStatus(d, questplan.ObservatoryName) == questplan.StatusComplete
	}
	done := questrunner.NewDialogue(d).DriveUntil(complete, []string{"I can see a constellation.", "What am I looking at?"}, obsTelescopeBudget)
	questrunner.ClearLeftoverModal(d) // the quest scroll
	e.InvalidateStage()
	if !done {
		logf(d, "observatory: the telescope's conversation did not complete the quest in %s", obsTelescopeBudget)
	}
	return done
}

// The Observatory's dome and keep are pockets for every quest's walks: the dome is walled off from the surface (its only door is the
// ladder down), and the keep is behind a gate the walk graph has no edge for.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Observatory dome", In: questplan.InObsDome, Leave: obsLeaveAll})
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Observatory keep", In: questplan.InObsKeep, Leave: obsLeaveAll})
}
