package questmods

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// HazeelCult is Hazeel Cult ready for the engine, on the cult's side (questplan/hazeelcult.go says why). Ceril's and Clivet's talks and the
// bank are the engine's generic steps; the customs are the legs the content makes special: the cave into the sewer (a teleport), the poison
// on the range, the five valves and the raft to the hideout (the raft is a teleport that moves only when the valves are right), Alomone's
// talks, the crate's key, the secret wall, the attic and the chest's scroll.
func HazeelCult() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.HazeelCult,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomHzDown: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return hzDown(e.Deps())
			},
			questplan.CustomHzPour:    hzPour,
			questplan.CustomHzAlomone: hzAlomone,
			questplan.CustomHzKey:     hzKey,
			questplan.CustomHzChest:   hzChest,
			questplan.CustomHzRitual:  hzRitual,
		},
		// The ritual leaves the account in the hideout, a pocket: best-effort tidying, the pocket's own Leave runs before any walk anyway.
		Finish: func(e *questrunner.Engine) { hzLeaveSewer(e.Deps()) },
	}
}

const (
	// hzArriveWait is how long a cave, a set of stairs or a raft gets to move the account to the other side.
	hzArriveWait = 15 * time.Second
	// hzBoxWait is how long a loc op gets to put its message box on the screen.
	hzBoxWait = 8 * time.Second
	// hzRounds bounds the rides of one trip to the hideout: the first finds out whether the valves are set, the second follows them being
	// set, the third is a retry.
	hzRounds = 3
	// hzValveTries bounds the turns of one valve.
	hzValveTries = 3
	// hzTalkBudget bounds one of Alomone's conversations; the ritual's is the longest quest chat there is (about thirty pages and a
	// five-tick pause) and ends with the quest's own queue.
	hzTalkBudget = 150 * time.Second
	// hzChestWait is how long the chest gets to show its opened form after the key.
	hzChestWait = 10 * time.Second
	// hzRaftParkFor is how long the quest skips an account whose raft will not take it to the hideout whatever it does to the valves.
	hzRaftParkFor = 3 * time.Hour
)

// hzIslandRafts are the rafts of the four islands the raft stops at when the valves are wrong (m40_151 "0 18 22", "0 31 30", "0 38 47",
// "0 54 61"): each sends the account back to the landing.
var hzIslandRafts = [4]tile.Tile{{X: 2578, Z: 9686}, {X: 2591, Z: 9694}, {X: 2598, Z: 9711}, {X: 2614, Z: 9725}}

// driveScript carries a script's messages to their end: pages are continued and menus answered from prefer by questrunner's Dialogue (which
// also presses the continue on a message box, the chat interface a ~mesbox opens), and a box the dialogue does not see (a main modal) is
// closed. With done it runs until done holds or the budget ends and reports done; without it, it runs until the chat and the modals have
// stayed shut for three seconds and reports whether the script raised anything at all.
func driveScript(d *questrunner.Deps, prefer []string, done func() bool, budget time.Duration) bool {
	const quiet = 3 * time.Second
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	var shutSince time.Time
	spoke := false
	settle := func() {
		for i := 0; i < 3; i++ {
			if !questrunner.ClearLeftoverModal(d) {
				return
			}
		}
	}
	for time.Now().Before(deadline) {
		if done != nil && done() {
			settle()
			return true
		}
		if dl.Ready() {
			spoke = true
			shutSince = time.Time{}
			if !dl.Drive(prefer) {
				break // no option to choose, or the page budget ran out
			}
			continue
		}
		if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
			spoke = true
			shutSince = time.Time{}
			questrunner.ClearLeftoverModal(d)
			continue
		}
		if done == nil {
			if shutSince.IsZero() {
				shutSince = time.Now()
			}
			if time.Since(shutSince) >= quiet {
				break
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
	}
	settle()
	if done != nil {
		return done()
	}
	return spoke
}

// hzSettle closes whatever message box a script left open, so that the next interaction is not dropped.
func hzSettle(d *questrunner.Deps) {
	for i := 0; i < 3; i++ {
		if !questrunner.ClearLeftoverModal(d) {
			return
		}
	}
}

// hzSurface reports a tile that is neither in the sewer nor underground: the forest, the mansion's floors and the rest of the map the
// walker reaches.
func hzSurface(t tile.Tile) bool { return !questplan.InHzSewer(t) && !questplan.IsUnderground(t) }

// hzDown takes the account to the landing under the cave, from wherever it stands: by the cave's mouth from the surface (or the kitchen
// cellar, whose ladder the walker takes), by the raft from the hideout or an island.
func hzDown(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if questplan.InHzLanding(here) {
		return true
	}
	if questplan.InHzSewer(here) {
		return hzRaftBack(d) && questplan.InHzLanding(questrunner.CurrentTile(d))
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHzCave, "", "Enter", questplan.HzCaveAt, 6)
	if !ok {
		logf(d, "hazeel: no cave (loc %d) offering Enter near %v", questplan.LocHzCave, questplan.HzCaveAt)
		return false
	}
	logf(d, "hazeel: down the cave to the sewer")
	if err := questrunner.OperateLoc(d, m, "Enter"); err != nil {
		logf(d, "hazeel: the cave: %v", err)
		return false
	}
	if !waitUntil(d, hzArriveWait, func() bool { return questplan.InHzLanding(questrunner.CurrentTile(d)) }) {
		logf(d, "hazeel: the cave did not set the account down at the landing (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond) // the sewer's scene loads
	hzSettle(d)
	return true
}

// hzUp climbs the stairs at the landing to the forest.
func hzUp(d *questrunner.Deps) bool {
	if !questplan.InHzLanding(questrunner.CurrentTile(d)) {
		return hzSurface(questrunner.CurrentTile(d))
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHzStairs, "", "Climb-up", questplan.HzStairsAt, 6)
	if !ok {
		logf(d, "hazeel: no stairs (loc %d) offering Climb-up near %v", questplan.LocHzStairs, questplan.HzStairsAt)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
		logf(d, "hazeel: the stairs: %v", err)
		return false
	}
	if !waitUntil(d, hzArriveWait, func() bool { return hzSurface(questrunner.CurrentTile(d)) }) {
		logf(d, "hazeel: the stairs did not set the account down in the forest (at %v)", questrunner.CurrentTile(d))
		return false
	}
	hzSettle(d)
	return true
}

// hzRaftHere is the raft of the piece of ground the account stands on: the hideout's, an island's or the landing's.
func hzRaftHere(t tile.Tile) (tile.Tile, bool) {
	switch {
	case questplan.InHzHideout(t):
		return questplan.HzHideoutRaft, true
	case questplan.HzIslandOf(t) != 0:
		return hzIslandRafts[questplan.HzIslandOf(t)-1], true
	case questplan.InHzLanding(t):
		return questplan.HzRaftMouth, true
	}
	return tile.Tile{}, false
}

// hzRaftBack boards the raft of the hideout or of an island: every raft but the landing's sends the account to the landing ("The raft
// flows with the current back to the cave entrance.", p_teleport(0_40_151_7_16)).
func hzRaftBack(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	if questplan.InHzLanding(here) || !questplan.InHzSewer(here) {
		return true
	}
	at, ok := hzRaftHere(here)
	if !ok {
		return false
	}
	m, found := questrunner.FindLoc(d, questplan.LocHzRaft, "", "Board", at, 3)
	if !found {
		logf(d, "hazeel: no raft (loc %d) offering Board near %v", questplan.LocHzRaft, at)
		return false
	}
	logf(d, "hazeel: riding the raft back to the landing")
	if err := questrunner.OperateLoc(d, m, "Board"); err != nil {
		logf(d, "hazeel: the raft: %v", err)
		return false
	}
	if !waitUntil(d, hzArriveWait, func() bool { return questplan.InHzLanding(questrunner.CurrentTile(d)) }) {
		logf(d, "hazeel: the raft did not take the account back to the landing (at %v)", questrunner.CurrentTile(d))
		return false
	}
	hzSettle(d) // "The raft flows with the current back to the cave entrance." is raised after the teleport
	return true
}

// hzLeaveSewer is the sewer's way out for every walk: the raft back to the landing, then the stairs up.
func hzLeaveSewer(d *questrunner.Deps) bool {
	for round := 0; round < 3; round++ {
		here := questrunner.CurrentTile(d)
		if !questplan.InHzSewer(here) {
			return true
		}
		if questplan.InHzLanding(here) {
			if !hzUp(d) {
				return false
			}
			continue
		}
		if !hzRaftBack(d) {
			return false
		}
	}
	return !questplan.InHzSewer(questrunner.CurrentTile(d))
}

// hzRide is how a ride on the landing's raft ended.
type hzRide int

const (
	hzRideFailed  hzRide = iota // the op could not be sent
	hzRodeIn                    // the hideout
	hzRideRefused               // the raft would not move: no valve is set
	hzRideShort                 // an island: the first valves are set, not all five
)

// hzBoard boards the landing's raft once and reports where it went. The raft's answer is the only oracle for the valves (the varp is
// not transmitted).
func hzBoard(d *questrunner.Deps) hzRide {
	m, ok := questrunner.FindLoc(d, questplan.LocHzRaft, "", "Board", questplan.HzRaftMouth, 3)
	if !ok {
		logf(d, "hazeel: no raft (loc %d) offering Board near %v", questplan.LocHzRaft, questplan.HzRaftMouth)
		return hzRideFailed
	}
	mark := time.Now()
	logf(d, "hazeel: boarding the raft")
	if err := questrunner.OperateLoc(d, m, "Board"); err != nil {
		logf(d, "hazeel: the raft: %v", err)
		return hzRideFailed
	}
	waitUntil(d, hzArriveWait, func() bool {
		return !questplan.InHzLanding(questrunner.CurrentTile(d)) || cookSaw(d, mark, "will not move")
	})
	here := questrunner.CurrentTile(d)
	hzSettle(d)
	switch {
	case questplan.InHzHideout(here):
		logf(d, "hazeel: the raft reached the hideout")
		return hzRodeIn
	case questplan.HzIslandOf(here) != 0:
		logf(d, "hazeel: the raft stopped at island %d: the valves are not all set", questplan.HzIslandOf(here))
		return hzRideShort
	}
	logf(d, "hazeel: the raft would not move: the valves are not set")
	return hzRideRefused
}

// hzValves sets the five sewer valves in the forest, in order. Each op sets its bit whatever it was (oploc1 and oploc2 of
// _hazeelcult_valve write the bit, they do not toggle it), so the pass is the same for an account with none set and one with some.
func hzValves(d *questrunner.Deps) bool {
	dl := questrunner.NewDialogue(d)
	for i, v := range questplan.HzValves {
		turned := false
		for try := 1; try <= hzValveTries && !turned; try++ {
			m, ok := questrunner.FindLoc(d, v.Loc, "", v.Op, v.At, 3)
			if !ok {
				logf(d, "hazeel: no sewer valve %d (loc %d) offering %q near %v", i+1, v.Loc, v.Op, v.At)
				return false
			}
			if err := questrunner.OperateLoc(d, m, v.Op); err != nil {
				logf(d, "hazeel: valve %d: %v", i+1, err)
				continue
			}
			// The valve's script changes the bit and then raises a message box ("You turn the large metal valve to the left..."): the box
			// is the proof that the script ran.
			turned = waitUntil(d, hzBoxWait, dl.Ready)
			hzSettle(d)
		}
		if !turned {
			logf(d, "hazeel: sewer valve %d at %v would not turn", i+1, v.At)
			return false
		}
	}
	logf(d, "hazeel: all five sewer valves are set")
	return true
}

// hzIntoHideout takes the account to the hideout over the raft, setting the valves only when the raft says they are not set.
func hzIntoHideout(e *questrunner.Engine, d *questrunner.Deps) bool {
	for round := 1; round <= hzRounds; round++ {
		if questplan.InHzHideout(questrunner.CurrentTile(d)) {
			return true
		}
		if !hzDown(d) {
			return false
		}
		switch hzBoard(d) {
		case hzRodeIn:
			return true
		case hzRideFailed:
			return false
		}
		logf(d, "hazeel: setting the valves (round %d of %d)", round, hzRounds)
		if !hzLeaveSewer(d) || !hzValves(d) {
			return false
		}
	}
	if questplan.InHzHideout(questrunner.CurrentTile(d)) {
		return true
	}
	e.RequestPark("the raft would not take the account to the hideout after the valves were set", hzRaftParkFor)
	return false
}

// hzTalkAlomone walks to Alomone in the hideout and opens his conversation.
func hzTalkAlomone(d *questrunner.Deps) bool {
	stop := questplan.HzAlomoneStop
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "hazeel: Alomone was not reached in the hideout (at %v)", questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		logf(d, "hazeel: Alomone opened no conversation")
		return false
	}
	return true
}

// hzAlomone rides to the hideout and talks to Alomone: seven pages with no menu, the last of which sets the stage (the journal is read
// again by the engine).
func hzAlomone(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !hzIntoHideout(e, d) || !hzTalkAlomone(d) {
		return false
	}
	return driveScript(d, questplan.HzAlomoneStop.Prefer, nil, hzTalkBudget)
}

// hzRitual rides to the hideout with the scroll and hands it to Alomone: the ritual is a long chat that Hazeel's own queue ends with the
// quest's reward, so it is driven until the quest tab reads complete.
func hzRitual(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjHzScroll) == 0 {
		logf(d, "hazeel: no scroll of Hazeel in the pack to hand over")
		return false
	}
	if !hzIntoHideout(e, d) || !hzTalkAlomone(d) {
		return false
	}
	complete := func() bool { return questrunner.ReadStatus(d, questplan.HazeelCultName) == questplan.StatusComplete }
	done := driveScript(d, questplan.HzAlomoneStop.Prefer, complete, hzTalkBudget)
	hzSettle(d) // the quest scroll
	if !done {
		logf(d, "hazeel: the ritual did not complete the quest in %s", hzTalkBudget)
	}
	return done
}

// hzPour pours Clivet's poison into the mansion's soup: it is used on the range in the kitchen cellar (stage 4 on the cult side
// only, anything else is "Random murder isn't good"). The box that follows is raised after the stage is set, so closing it is safe.
func hzPour(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjPoison) == 0 {
		logf(d, "hazeel: no poison in the pack to pour")
		return false
	}
	if !questrunner.Travel(d, questplan.HzRangeStand, 0) && !atTile(d, questplan.HzRangeStand) {
		logf(d, "hazeel: the range's stand %v was not reached from %v", questplan.HzRangeStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHzRange, "", "", questplan.HzRangeAt, 4)
	if !ok {
		logf(d, "hazeel: no range (loc %d) near %v", questplan.LocHzRange, questplan.HzRangeAt)
		return false
	}
	logf(d, "hazeel: pouring the poison into the range")
	if err := questrunner.UseItemOnLoc(d, questplan.ObjPoison, m); err != nil {
		logf(d, "hazeel: the poison on the range: %v", err)
		return false
	}
	gone := waitUntil(d, hzBoxWait, func() bool { return questrunner.HeldID(d, questplan.ObjPoison) == 0 })
	hzSettle(d)
	if !gone {
		logf(d, "hazeel: the range kept the poison (the quest is not at the stage that takes it)")
	}
	return gone
}

// hzKey searches the crate in the kitchen cellar for the rusty key. The message box ("Under various food package you find an old rusty
// key.") comes BEFORE the key is added, so it is continued, never closed.
func hzKey(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjHzKey) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.HzCrateStand, 0) && !atTile(d, questplan.HzCrateStand) {
		logf(d, "hazeel: the crate's stand %v was not reached from %v", questplan.HzCrateStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHzCrate, "", "Search", questplan.HzCrateAt, 3)
	if !ok {
		logf(d, "hazeel: no crate (loc %d) offering Search near %v", questplan.LocHzCrate, questplan.HzCrateAt)
		return false
	}
	logf(d, "hazeel: searching the crate for the key")
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "hazeel: the crate: %v", err)
		return false
	}
	got := driveScript(d, nil, func() bool { return questrunner.HeldID(d, questplan.ObjHzKey) > 0 }, hzBoxWait*2)
	if !got {
		logf(d, "hazeel: the crate gave no key")
	}
	return got
}

// hzEnterAttic knocks on the secret wall from the bedroom's side and waits for the ladder room (stage 6 and 7 on the cult side only; at
// other stages the wall answers "You can hear there is a hollow behind this wall.").
func hzEnterAttic(d *questrunner.Deps) bool {
	if questplan.InHzAttic(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.HzWallSouth, 0) && !atTile(d, questplan.HzWallSouth) {
		logf(d, "hazeel: the secret wall's bedroom side %v was not reached from %v", questplan.HzWallSouth, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3)
	if !ok {
		// Another account's knock leaves the wall a different loc for three ticks.
		waitUntil(d, 4*time.Second, func() bool {
			_, again := questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3)
			return again
		})
		if m, ok = questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3); !ok {
			logf(d, "hazeel: no secret wall (loc %d) offering Knock-at near %v", questplan.LocHzSecretWall, questplan.HzWallAt)
			return false
		}
	}
	logf(d, "hazeel: knocking on the secret wall")
	if err := questrunner.OperateLoc(d, m, "Knock-at"); err != nil {
		logf(d, "hazeel: the secret wall: %v", err)
		return false
	}
	if !waitUntil(d, hzArriveWait, func() bool { return questplan.InHzAttic(questrunner.CurrentTile(d)) }) {
		logf(d, "hazeel: the secret wall let nobody through (at %v)", questrunner.CurrentTile(d))
		return false
	}
	hzSettle(d)
	return true
}

// hzLeaveAttic climbs down from the attic and knocks the wall from the ladder room's side, which sets the account in the bedroom.
func hzLeaveAttic(d *questrunner.Deps) bool {
	for round := 0; round < 3; round++ {
		here := questrunner.CurrentTile(d)
		if !questplan.InHzAttic(here) {
			return true
		}
		if here.Level == 2 {
			m, ok := questrunner.FindLoc(d, questplan.LocHzAtticLadderDn, "", "Climb-down", tile.Tile{X: 2573, Z: 3271, Level: 2}, 3)
			if !ok {
				logf(d, "hazeel: no attic ladder (loc %d) offering Climb-down", questplan.LocHzAtticLadderDn)
				return false
			}
			if err := questrunner.OperateLoc(d, m, "Climb-down"); err != nil {
				logf(d, "hazeel: the attic ladder: %v", err)
				return false
			}
			if !waitUntil(d, hzArriveWait, func() bool { return questrunner.CurrentTile(d).Level == 1 }) {
				logf(d, "hazeel: the attic ladder did not take the account down (at %v)", questrunner.CurrentTile(d))
				return false
			}
			continue
		}
		m, ok := questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3)
		if !ok {
			waitUntil(d, 4*time.Second, func() bool {
				_, again := questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3)
				return again
			})
			if m, ok = questrunner.FindLoc(d, questplan.LocHzSecretWall, "", "Knock-at", questplan.HzWallAt, 3); !ok {
				logf(d, "hazeel: no secret wall (loc %d) offering Knock-at from the ladder room", questplan.LocHzSecretWall)
				return false
			}
		}
		if err := questrunner.OperateLoc(d, m, "Knock-at"); err != nil {
			logf(d, "hazeel: the secret wall from the ladder room: %v", err)
			return false
		}
		if !waitUntil(d, hzArriveWait, func() bool { return !questplan.InHzAttic(questrunner.CurrentTile(d)) }) {
			logf(d, "hazeel: the secret wall let the account out nowhere (at %v)", questrunner.CurrentTile(d))
			return false
		}
		hzSettle(d)
	}
	return !questplan.InHzAttic(questrunner.CurrentTile(d))
}

// hzChestOf finds the chest in whichever form another account's use has left it.
func hzChestOf(d *questrunner.Deps) (shut, open locindex.Match, shutOK, openOK bool) {
	shut, shutOK = questrunner.FindLoc(d, questplan.LocHzChestShut, "", "Open", questplan.HzChestAt, 3)
	open, openOK = questrunner.FindLoc(d, questplan.LocHzChestOpen, "", "Search", questplan.HzChestAt, 3)
	return
}

// hzChest fetches the scroll of Hazeel: through the secret wall, up the ladder, the key on the chest and a search of it. The chest shuts
// itself 300 ticks after it is opened, and another account's visit may have left it either way. The search's message box comes BEFORE
// the scroll is added, so it is continued, never closed. The account is brought back down to the bedroom before the step ends.
func hzChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjHzScroll) > 0 {
		hzLeaveAttic(d)
		return true
	}
	if questrunner.HeldID(d, questplan.ObjHzKey) == 0 {
		logf(d, "hazeel: no key in the pack for the chest")
		return false
	}
	if !hzEnterAttic(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.HzChestStand, 0) && !atTile(d, questplan.HzChestStand) {
		logf(d, "hazeel: the chest's stand %v was not reached from %v", questplan.HzChestStand, questrunner.CurrentTile(d))
		hzLeaveAttic(d)
		return false
	}
	got := false
	for try := 1; try <= 3 && !got; try++ {
		shut, open, shutOK, openOK := hzChestOf(d)
		if !openOK && shutOK {
			logf(d, "hazeel: unlocking the chest with the key")
			if err := questrunner.UseItemOnLoc(d, questplan.ObjHzKey, shut); err != nil {
				logf(d, "hazeel: the key on the chest: %v", err)
				break
			}
			hzSettle(d)
			waitUntil(d, hzChestWait, func() bool { _, _, _, o := hzChestOf(d); return o })
			shut, open, shutOK, openOK = hzChestOf(d)
		}
		if !openOK {
			logf(d, "hazeel: neither the shut nor the open chest (loc %d, %d) near %v", questplan.LocHzChestShut, questplan.LocHzChestOpen, questplan.HzChestAt)
			d.Delay(2 * time.Second)
			continue
		}
		logf(d, "hazeel: searching the chest")
		if err := questrunner.OperateLoc(d, open, "Search"); err != nil {
			logf(d, "hazeel: the chest: %v", err)
			break
		}
		got = driveScript(d, nil, func() bool { return questrunner.HeldID(d, questplan.ObjHzScroll) > 0 }, hzBoxWait*2)
	}
	if !got {
		logf(d, "hazeel: the chest gave no scroll")
	}
	hzLeaveAttic(d)
	return got
}

// The cult's sewer (the landing, the hideout and the islands) and the mansion's ladder room and attic are pockets: no walk leaves them.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Hazeel cult's sewer", In: questplan.InHzSewer, Leave: hzLeaveSewer})
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Carnillean ladder room and attic", In: questplan.InHzAttic, Leave: hzLeaveAttic})
}
