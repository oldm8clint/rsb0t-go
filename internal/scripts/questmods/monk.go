package questmods

import (
	"sort"
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MonksFriend is Monk's Friend ready for the engine. Its talks (Omad, Cedric), its jug (the provisioner's kit), its log (the
// provisioner's chop-logs source) and its sink are generic steps and customs; the three customs below are the parts the content
// makes special: the cave behind a ladder that exists only while the account stands near it, the jug filled at a sink, and the
// party that must be waited out.
func MonksFriend() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.MonksFriend,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomMonkBlanket: monkBlanket,
			questplan.CustomMonkJug:     monkJug,
			questplan.CustomMonkParty:   monkParty,
		},
	}
}

const (
	// monkLadderWait is how long the ladder under the ring of stones gets to appear: the timer adds it the tick after the account
	// is within 2 tiles, unless one stands already (another account's, which is the world's).
	monkLadderWait = 20 * time.Second
	// monkBlanketWait bounds the wait for the blanket in the cave: it is one ground spawn that comes back 50 ticks (30 s) after
	// another account took it, and the quest's slot admits four.
	monkBlanketWait = 3 * time.Minute
	// monkPartyBudget bounds the wait for the party: about 23 ticks of delay, then the quest completes in a closing queue.
	monkPartyBudget = 90 * time.Second
)

// monkBlanket fetches the child's blanket: down the ladder the ring of stones grows, to the thieves' room, take the blanket, back
// up. It reports whether the pack holds the blanket. The account is never fought for it (Module.NoRetaliate): the thieves hit for
// 2 and 3, and half of the account's hitpoints gone ends the wait and walks out.
func monkBlanket(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjChildsBlanket) > 0 {
		monkLeaveCave(d)
		return true
	}
	if !monkEnterCave(e, d) {
		return false
	}
	if !questrunner.Travel(d, questplan.MonkBlanketSpawn, 1) {
		logf(d, "monk: the blanket's tile was not reached from the cave's foot, at %v", questrunner.CurrentTile(d))
		monkLeaveCave(d)
		return false
	}
	got, hurt := monkTakeBlanket(d)
	if !got {
		logf(d, "monk: no blanket taken from the thieves' room (at %v)", questrunner.CurrentTile(d))
		// Neither mends in a minute: the blanket's spawn is empty for minutes only when the cave is crowded, and the hitpoints
		// the thieves took come back by the hour.
		if hurt {
			e.RequestPark("the thieves took half the account's hitpoints in the cave", 30*time.Minute)
		} else {
			e.RequestPark("no child's blanket on the thieves' room floor", 20*time.Minute)
		}
	}
	monkLeaveCave(d)
	return got
}

// monkTakeBlanket takes the blanket from its spawn, waiting for a respawn when another account has just taken it, and gives up
// at monkBlanketWait or when the thieves have taken half the account's hitpoints (hurt).
func monkTakeBlanket(d *questrunner.Deps) (got, hurt bool) {
	deadline := time.Now().Add(monkBlanketWait)
	for time.Now().Before(deadline) {
		if questrunner.TakeGround(d, questplan.ObjChildsBlanket, questplan.MonkBlanketSpawn, 3) {
			logf(d, "monk: took the child's blanket")
			return true, false
		}
		if questrunner.HeldID(d, questplan.ObjChildsBlanket) > 0 {
			return true, false
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 && hp*2 < max {
			logf(d, "monk: %d/%d hitpoints in the thieves' room: leaving without the blanket", hp, max)
			return false, true
		}
		if !d.Delay(1500 * time.Millisecond) {
			return false, false
		}
	}
	return questrunner.HeldID(d, questplan.ObjChildsBlanket) > 0, false
}

// monkEnterCave takes the ladder under the ring of stones and reports whether the account is in the cave. The ladder is not in
// the map: the player's blanket_ladder timer adds it for 200 ticks while the account stands within 2 tiles (quest_drunkmonk.rs2),
// so the account stands next to its tile and waits for it. A ladder that never appears parks the quest for half an hour.
func monkEnterCave(e *questrunner.Engine, d *questrunner.Deps) bool {
	if questplan.InMonkCave(questrunner.CurrentTile(d)) {
		return true
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if !questrunner.Travel(d, questplan.MonkRingStand, 1) {
			logf(d, "monk: the ring of stones (%v) was not reached from %v", questplan.MonkRingStand, questrunner.CurrentTile(d))
			return false
		}
		var ladder locindex.Match
		found := waitUntil(d, monkLadderWait, func() bool {
			m, ok := questrunner.FindLoc(d, questplan.LocMonkRingLadder, "", "Climb-down", questplan.MonkRingLadder, 3)
			if ok {
				ladder = m
			}
			return ok
		})
		if !found {
			logf(d, "monk: no ladder appeared under the ring of stones in %s (attempt %d of 3)", monkLadderWait, attempt)
			continue
		}
		if err := questrunner.OperateLoc(d, ladder, "Climb-down"); err != nil {
			logf(d, "monk: climbing down the ring's ladder: %v", err)
			continue
		}
		if waitUntil(d, 12*time.Second, func() bool { return questplan.InMonkCave(questrunner.CurrentTile(d)) }) {
			logf(d, "monk: down the ladder into the thieves' cave")
			return true
		}
		logf(d, "monk: the ring's ladder did not take the account down (at %v)", questrunner.CurrentTile(d))
	}
	e.RequestPark("the ladder under the ring of stones never took the account into the thieves' cave", 30*time.Minute)
	return false
}

// monkLeaveCave climbs the cave's ladder from its foot and reports whether the account is out. It is also the cave's pocket: no
// walk leaves the cave, because the graph's edge for this ladder stands on a tile the cave does not reach.
func monkLeaveCave(d *questrunner.Deps) bool {
	if !questplan.InMonkCave(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.Travel(d, questplan.MonkCaveFoot, 0) {
		logf(d, "monk: the cave's foot (%v) was not reached from %v", questplan.MonkCaveFoot, questrunner.CurrentTile(d))
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCaveLadderUp, "", "Climb-up", questplan.MonkCaveLadder, 2)
	if !ok {
		logf(d, "monk: no way up near %v", questplan.MonkCaveLadder)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
		logf(d, "monk: climbing the cave's ladder: %v", err)
		return false
	}
	out := waitUntil(d, 12*time.Second, func() bool { return !questplan.InMonkCave(questrunner.CurrentTile(d)) })
	if out {
		logf(d, "monk: up the ladder, out of the thieves' cave")
	}
	return out
}

// The thieves' cave is a pocket for every quest's walks.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the thieves' cave under the ring of stones", In: questplan.InMonkCave, Leave: monkLeaveCave})
}

// monkSinkOrder is the sinks nearest first from where the account stands.
func monkSinkOrder(from tile.Tile) []tile.Tile {
	out := append([]tile.Tile(nil), questplan.MonkSinks...)
	sort.SliceStable(out, func(i, j int) bool { return tile.Chebyshev(from, out[i]) < tile.Chebyshev(from, out[j]) })
	return out
}

// monkJug fills the jug in the pack at the nearest sink that works. The use is retried from a standstill: sent the moment the
// walk ends, it lands inside the arrival window and is dropped (the TS def's note).
func monkJug(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.HeldID(d, questplan.ObjJugOfWater) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjJug) == 0 {
		logf(d, "monk: no jug in the pack to fill")
		return false
	}
	for _, spot := range monkSinkOrder(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, spot, 3) {
			logf(d, "monk: the sink at %v was not reached", spot)
			continue
		}
		m, ok := questrunner.FindLoc(d, 0, "Sink", "", spot, 5)
		if !ok {
			logf(d, "monk: no sink near %v", spot)
			continue
		}
		for try := 1; try <= 3; try++ {
			if err := questrunner.UseItemOnLoc(d, questplan.ObjJug, m); err != nil {
				logf(d, "monk: the jug on the sink at %d,%d: %v", m.X, m.Z, err)
				break
			}
			if waitUntil(d, 8*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjJugOfWater) > 0 }) {
				logf(d, "monk: filled the jug at the sink at %d,%d", m.X, m.Z)
				return true
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
	}
	return false
}

// monkParty tells Omad that Cedric is on his way and waits out the party. The script moves %drunkmonkquest to 80 only in the
// queue it ends with, some 23 ticks after the dialogue closes, so a talk that returned with the dialogue would find stage 70 and
// start a second party; this keeps the conversation and waits until the quest tab turns green.
func monkParty(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	complete := func() bool { return questrunner.ReadStatus(d, questplan.MonksFriendName) == questplan.StatusComplete }
	if complete() {
		return true
	}
	if !questrunner.GotoNpc(d, questplan.OmadPartyStop, nil) {
		return false
	}
	if !questrunner.OpenDialogue(d, questplan.OmadPartyStop) {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(complete, questplan.OmadPartyStop.Prefer, monkPartyBudget)
	questrunner.ClearLeftoverModal(d)
	done := complete()
	logf(d, "monk: the party is over (quest tab complete: %v)", done)
	return done
}
