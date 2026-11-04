package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// Live 2026-09-22, production: eight ProgressiveFisher accounts sat at 2780,3214 --
// "Brimhaven, northern dock", one of the engine's own catalogued fail-teleport destinations
// and on Karamja, an island -- for half an hour at 0 xp. Each had already logged "phase:
// sailing to Catherby", so its own across-water recovery was under way; EmergencyFlee's
// drift check then took the action lock on every 500ms poll and issued a walk home that
// cannot exist, cancelling the sail each time. 96 "recovering immediately" lines, no other
// line from the script, no movement.
//
// The drift check's stationary gate cannot catch this on its own: it assumes an account is
// stationary BECAUSE nothing is moving it, and an account waiting at a gangplank is
// stationary while something very much is.

// TestDriftCanWalkHomeRefusesWhenNoRouteExists is the decision itself. An empty pack has
// nothing walkable, so every FindPath fails -- which is the same answer the real pack gives
// from Karamja, and the one that must make the guardian stand down.
func TestDriftCanWalkHomeRefusesWhenNoRouteExists(t *testing.T) {
	s := &safeRefSession{state: gamestate.New()}
	home := pathfind.NavPoint{X: 2809, Z: 3441}
	if driftCanWalkHome(s, packWithNothingWalkable(), 2780, 3214, 0, home) {
		t.Fatal("reported a walking route home through a pack in which nothing is walkable")
	}
}

// The companion, so the test above cannot pass by always answering false. Without this a
// gate that stood the guardian down everywhere would look correct, and the drift safety net
// would be silently dead for the whole fleet.
func TestDriftCanWalkHomeAllowsAnOrdinaryWalk(t *testing.T) {
	pk := realCollisionPack(t)
	s := &safeRefSession{state: gamestate.New()}
	catherbyBank := pathfind.NavPoint{X: 2809, Z: 3441}
	// A tile out in the Catherby fishing camp: far enough to be drift, plainly walkable home.
	if !driftCanWalkHome(s, pk, 2845, 3429, 0, catherbyBank) {
		t.Fatal("the Catherby fishing camp cannot walk to the Catherby bank -- the gate would " +
			"disarm drift recovery for ordinary displacement, which is what it exists for")
	}
}

// The live case end to end, against real collision data: the dock the eight accounts were
// stranded on really has no walking route to their home, so the guardian really must stand
// down there. Skipped without RSB0T_COLLISION_PACK, like every other real-pack test here.
func TestBrimhavenDockHasNoWalkHomeToCatherby(t *testing.T) {
	pk := realCollisionPack(t)
	s := &safeRefSession{state: gamestate.New()}
	catherbyBank := pathfind.NavPoint{X: 2809, Z: 3441}
	if driftCanWalkHome(s, pk, 2780, 3214, 0, catherbyBank) {
		t.Fatal("Brimhaven's northern dock reports a walking route to Catherby -- it is on " +
			"Karamja and only a boat gets home")
	}
	if name, ok := KnownFailTeleportPocket(2780, 3214, 0); !ok || name == "" {
		t.Fatalf("2780,3214 is not catalogued as a fail-teleport destination (%q, %v) -- the "+
			"stand-down line names the location from this table", name, ok)
	}
}

// The memo has to be keyed on the tile AND the home, or an account that genuinely walks
// somewhere new keeps the old verdict. A stranded account sits still by definition, so the
// cache is what keeps a failing A* (21,951 expansions for this pair) off every 500ms poll;
// a cache that never invalidates would be the worse bug.
func TestDriftReachMemoIsKeyedOnTileAndHome(t *testing.T) {
	pk := realCollisionPack(t)
	s := &safeRefSession{state: gamestate.New()}
	catherbyBank := pathfind.NavPoint{X: 2809, Z: 3441}

	if driftCanWalkHome(s, pk, 2780, 3214, 0, catherbyBank) {
		t.Fatal("Brimhaven dock should have no walk home")
	}
	// Same session, different tile: the cached "no" must not carry over.
	if !driftCanWalkHome(s, pk, 2845, 3429, 0, catherbyBank) {
		t.Fatal("the memo answered for a tile it was not asked about -- an account that walked " +
			"somewhere reachable would stay written off")
	}
	// Same session and tile, different home: likewise.
	ardougneBank := pathfind.NavPoint{X: 2615, Z: 3332}
	if !driftCanWalkHome(s, pk, 2845, 3429, 0, ardougneBank) {
		t.Fatal("the memo ignored a changed home reference -- fisher.go re-points driftHome " +
			"between phases, so the same tile legitimately has two answers")
	}
}

// Per account, like every other piece of drift bookkeeping in this file. One stranded
// account must not disarm another's drift recovery.
func TestDriftReachMemoIsPerSession(t *testing.T) {
	stranded := &safeRefSession{state: gamestate.New()}
	working := &safeRefSession{state: gamestate.New()}
	home := pathfind.NavPoint{X: 2809, Z: 3441}
	empty := packWithNothingWalkable()

	if driftCanWalkHome(stranded, empty, 2780, 3214, 0, home) {
		t.Fatal("expected no route through an empty pack")
	}
	if _, ok := driftReachable.Load(working); ok {
		t.Fatal("one account's reachability verdict was stored against another session")
	}
}
