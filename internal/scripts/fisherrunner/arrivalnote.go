package fisherrunner

import (
	"fmt"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the arrival test that reports success without going anywhere ----
//
// OBSERVABILITY ONLY. Nothing in this file changes what any caller does: every function here
// either returns a string or writes a log line, and no caller branches on either.
//
// The shape, found by "netrescue" and confirmed by "eventstrand" (COLLAB.md, 2026-09-20).
// fisherplan.InCatherby is Chebyshev <= 60 from the Catherby bank, and cmd/pocketscan counted
// 1,488 walkable tiles inside that radius, in 75 sealed components, that cannot walk to the bank
// at all. From one of them — 2844,3427 is 35 tiles out — ReturnToCatherby's very first statement
// reports success without taking a step; the caller resets its retry streak and the account loops
// forever writing NOTHING. The same 60-tile radius stands in for reachability at seven places the
// fishers and the mule run on every recovery.
//
// Why this is a log line and not the fix. eventstrand enumerated all 43 content-289 displacement
// destinations and none lands in a pocket (nearest 74 tiles away, intersection zero), so no
// account can currently arrive in one and the bug is latent. The real fix changes what those
// seven checks decide, for 44 live fishers and the mule, so it wants its own claim, its own live
// arm and a green fleet window. Until then the cheap, safe half is this: if it ever does fire in
// production, the log explains itself instead of looking like a stall.
//
// Why it does not become noise on 45 accounts. The line needs BOTH halves: an arrival test that
// has already decided it is home, AND no walkable route from that tile to the place it just
// claimed to have reached. An account standing at the camp when something asks it to go home has
// a route, so it says nothing. That second half is the discriminator, not the distance — a tile
// 36 tiles from the bank is an ordinary fishing camp, and only connectivity separates it from a
// sealed pocket.

// arrivalNote is the pure half: the line to write, or "" when there is nothing to say.
//
// start is where the account stood when the recovery began, here is where it stands now, and dest
// is what it has just claimed to have reached. routeToDest is what the pathfinder says about
// here -> dest; when it is true this is an ordinary, correct arrival and there is nothing to
// report.
func arrivalNote(label string, start, here, dest tile.Tile, routeToDest bool) string {
	if routeToDest {
		return ""
	}
	movement := "no movement occurred"
	if here != start {
		movement = fmt.Sprintf("having moved from %d,%d,%d", start.X, start.Z, start.Level)
	}
	return fmt.Sprintf("WARNING: %s reported arrival at %d,%d,%d while %s. It is %d tiles from %d,%d,%d and there is NO walkable route from here to it, so the radius test passed on ground the account cannot leave. The caller will treat this as a successful recovery and reset its retry streak. See internal/scripts/fisherrunner/arrivalnote.go.",
		label, here.X, here.Z, here.Level, movement,
		tile.Chebyshev(here, dest), dest.X, dest.Z, dest.Level)
}

// NoteArrival writes arrivalNote's line, when there is one, for an arrival test that has just
// decided the account is home.
//
// Exported because internal/bot's MuleReceiver makes the same radius-for-reachability substitution
// against the same ground (mule.go's ensureHome skips the whole walk home on InCatherby, and
// mulehome.go's muleLocalWalkSafe calls a local walk safe on a 12-tile radius without asking
// whether a route exists).
//
// The pathfinder is asked only at the moment a check has ALREADY reported arrival, so a healthy
// run pays for one search that succeeds immediately from the tile it is standing on. The failing
// search is the expensive one and it only ever runs on ground that is genuinely sealed: netrescue
// measured 2,584 expansions outward from 2844,3427, against the 445,667 the opposite direction
// costs. A nil pack means no opinion, and silence is the right answer then.
func NoteArrival(pk *pathfind.Pack, log func(string), label string, start, here, dest tile.Tile) {
	if pk == nil || log == nil {
		return
	}
	if pk.FindPath(arrivalNav(here), arrivalNav(dest)).OK {
		return
	}
	if line := arrivalNote(label, start, here, dest, false); line != "" {
		log(line)
	}
}

// noteArrival is NoteArrival for this package's own Deps.
func noteArrival(d *Deps, label string, start, here, dest tile.Tile) {
	if d == nil {
		return
	}
	NoteArrival(d.Pack, d.Log, label, start, here, dest)
}

func arrivalNav(t tile.Tile) pathfind.NavPoint {
	return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}
}
