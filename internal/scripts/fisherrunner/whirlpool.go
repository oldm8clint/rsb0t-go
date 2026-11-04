package fisherrunner

import (
	"math/rand"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// How a whirlpool takes a tool (content macro_event_whirlpool.rs2, fishing.rs2, Engine-TS
// Player.validateTarget): a fishing spot's whirlpool is the same NPC changetyped for 60 ticks,
// still named "Fishing spot" with the same ops. A changetype clears the interaction of anyone
// already fishing it, so the danger is only a click the server receives while the spot is a
// whirlpool — a click that crossed the change in flight. From then on every roll (every 5
// ticks) adds one to %macro_whirlpool_counter and the fifth deletes the tool. The counter is not
// perm: it resets on logout but otherwise accumulates across the whole session, across every
// whirlpool, whoever spawned it.
//
// Live 2026-09-20 00:48: ~139 fishers paused on "all matching spots unsafe" were released at
// once when one whirlpool reverted, all walked to the one remaining spot (2855,3423), clicked it
// over six seconds, it turned into a whirlpool among them, and 72 nets were deleted in five
// seconds. The accounts that clicked in the race then sat idle ("fishing paused") while the
// server kept rolling on the whirlpool for them.

// WhirlpoolGuard switches on everything added for this (the step-off below, the re-check of the
// spot right before a click, the crowd-weighted jittered spot choice and the stagger). It exists
// for the lab A/B in cmd/labfleettest (-mode whirlpool-roll), which turns it off for the "old"
// arm; production never changes it.
var WhirlpoolGuard = true

// clickFastPoll is the hold loop's poll interval just after a click: the first roll lands 4
// ticks (2.4s live) after the click, so seeing a whirlpool and stepping off inside that window
// costs no roll at all.
const clickFastPoll = 150 * time.Millisecond

// clickFastWindow is how long after a click the hold loop polls at clickFastPoll.
const clickFastWindow = 3 * time.Second

// clickedSpotWindow is how long after a click a whirlpool at the clicked spot's index counts as
// ours even before our face-entity update shows it.
const clickedSpotWindow = 5 * time.Second

// herdStaggerMax bounds the random wait before committing to a new spot after the last one
// vanished or every spot was blocked, so a released crowd does not click one spot in unison.
const herdStaggerMax = 3 * time.Second

// crowdWeight is how many tiles of extra walking one other fisher at a spot is worth when
// choosing a new spot.
const crowdWeight = 2

// spotJitter is the random tie-spread added to each candidate's score, in tiles.
const spotJitter = 4

func npcByIndex(state *gamestate.State, index int) (gamestate.NPC, bool) {
	for _, n := range state.NPCs() {
		if n.Index == index {
			return n, true
		}
	}
	return gamestate.NPC{}, false
}

// rollingOnWhirlpool reports the whirlpool this account may be fishing: the NPC it faces is a
// whirlpool, or the spot it clicked moments ago is one now (the face update can trail the type
// change by a tick).
func rollingOnWhirlpool(state *gamestate.State, clicked gamestate.NPC, clickedAt time.Time) (gamestate.NPC, bool) {
	if face := state.SelfFaceEntity(); face >= 0 && face < 32768 {
		if n, ok := npcByIndex(state, face); ok && fisherplan.IsWhirlpool(n.Type) {
			return n, true
		}
	}
	if !clickedAt.IsZero() && time.Since(clickedAt) < clickedSpotWindow {
		if n, ok := npcByIndex(state, clicked.Index); ok && fisherplan.IsWhirlpool(n.Type) {
			return n, true
		}
	}
	return gamestate.NPC{}, false
}

// stepOffTile picks where to step so the server drops a fishing interaction: a walkable tile
// one or two steps from here, as far from the whirlpool as possible. Pure, for the unit test.
func stepOffTile(here, whirl tile.Tile, walkable func(tile.Tile) bool) (tile.Tile, bool) {
	best := tile.Tile{}
	bestDist := -1
	for r := 1; r <= 2; r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if max(abs(dx), abs(dz)) != r {
					continue
				}
				t := tile.Tile{X: here.X + dx, Z: here.Z + dz, Level: here.Level}
				dist := tile.Chebyshev(t, whirl)
				if dist <= bestDist || !walkable(t) {
					continue
				}
				best, bestDist = t, dist
			}
		}
		if bestDist > tile.Chebyshev(here, whirl) {
			return best, true
		}
	}
	return best, bestDist >= 0
}

// StepOffWhirlpool sends one short walk away from w. Any walk ends the server-side fishing
// interaction, so no further roll lands on the whirlpool.
func StepOffWhirlpool(d *Deps, w gamestate.NPC) bool {
	here := currentTile(d)
	whirl := tile.Tile{X: w.X, Z: w.Z, Level: w.Level}
	walkable := func(t tile.Tile) bool {
		if d.Pack == nil {
			return true
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level})
		return res.OK && len(res.Waypoints) > 0 && len(res.Waypoints) <= 3
	}
	dest, ok := stepOffTile(here, whirl, walkable)
	if !ok {
		return false
	}
	path := []pathfind.NavPoint{{X: dest.X, Z: dest.Z, Level: dest.Level}}
	if d.Pack != nil {
		if res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, path[0]); res.OK && len(res.Waypoints) > 0 {
			path = res.Waypoints
		}
	}
	if err := actions.Walk(d.Session, path, false); err != nil {
		d.logf("stepping off the whirlpool: %v", err)
		return false
	}
	d.logf("whirlpool: the spot at %d,%d (index=%d) turned into a whirlpool under our click — stepped to %d,%d before the next roll", w.X, w.Z, w.Index, dest.X, dest.Z)
	return true
}

// guardWhirlpool steps off when the account may be rolling on a whirlpool. True when it did.
func guardWhirlpool(d *Deps, clicked gamestate.NPC, clickedAt time.Time) bool {
	if !WhirlpoolGuard {
		return false
	}
	w, ok := rollingOnWhirlpool(d.Session.State(), clicked, clickedAt)
	if !ok {
		return false
	}
	return StepOffWhirlpool(d, w)
}

// crowdAt counts the other players fishing a spot: facing it, or standing beside it.
func crowdAt(state *gamestate.State, spot gamestate.NPC) int {
	n := 0
	self := state.Self()
	for _, p := range state.Players() {
		if p.Index == self || p.Level != spot.Level {
			continue
		}
		if p.FaceEntity == spot.Index || tile.Chebyshev(tile.Tile{X: p.X, Z: p.Z}, tile.Tile{X: spot.X, Z: spot.Z}) <= 1 {
			n++
		}
	}
	return n
}

// herdStagger is the random wait before re-choosing after a spot vanished or unblocked.
func herdStagger() time.Duration {
	return time.Duration(rand.Int63n(int64(herdStaggerMax)))
}

// randomJitter is pickSpot's production jitter source.
func randomJitter(n int) int { return rand.Intn(n) }
