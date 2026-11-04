package donorrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// catherbyStand is the live fleet's mule post, the stand every ProgressiveWCFletch donor walks to.
var catherbyStand = tile.Tile{X: 2809, Z: 3441, Level: 0}

// seersCatherbyRoad is every tile a live donor was standing on when it logged "stranded across
// water heading for the mule stand at 2809,3441,0" (2026-09-22/23, the 26 distinct tiles the
// donors themselves then reported over the mule bus), plus 2740,3481 (where the plain Seers ->
// Catherby FindPath first crosses freshstart.ArdougneSideMaxX), the Seers bank the walk starts
// from and the stand itself. FindPath from every one of them to the stand is OK on the fleet's
// own pack: none of them is stranded.
var seersCatherbyRoad = []tile.Tile{
	{X: 2789, Z: 3457}, {X: 2790, Z: 3456}, {X: 2791, Z: 3456}, {X: 2792, Z: 3455}, {X: 2793, Z: 3455},
	{X: 2794, Z: 3455}, {X: 2795, Z: 3454}, {X: 2796, Z: 3453}, {X: 2797, Z: 3452}, {X: 2799, Z: 3450},
	{X: 2800, Z: 3449}, {X: 2801, Z: 3448}, {X: 2802, Z: 3447}, {X: 2803, Z: 3446}, {X: 2805, Z: 3444},
	{X: 2805, Z: 3443}, {X: 2805, Z: 3442}, {X: 2805, Z: 3441}, {X: 2805, Z: 3439}, {X: 2805, Z: 3438},
	{X: 2805, Z: 3437}, {X: 2806, Z: 3436}, {X: 2807, Z: 3436}, {X: 2808, Z: 3436}, {X: 2808, Z: 3437},
	{X: 2808, Z: 3438},
	{X: 2740, Z: 3481}, // the first tile the old classifier flipped on
	{X: 2725, Z: 3493}, // the Seers bank
	{X: 2809, Z: 3441}, // the stand
}

// 99 of the 102 live "stranded" lines were this road. The old classifier ignored the stand and
// answered LegPier for every tile with X >= 2740, the stand included; the CONTROL half of this
// test asserts exactly that, so it shows the case the fix changes rather than one it never saw.
func TestStrandedAcrossWaterIsFalseOnTheSeersCatherbyRoad(t *testing.T) {
	oldFlagged := 0
	for _, here := range seersCatherbyRoad {
		if strandedAcrossWater(here, catherbyStand) {
			t.Errorf("strandedAcrossWater(%+v, stand %+v) = true: a donor on the road the pack routes to the stand is not stranded", here, catherbyStand)
		}
		if freshstart.TravelLeg(here) == freshstart.LegPier {
			oldFlagged++
		}
	}
	// Control: the old stand-blind rule flags every tile here except the Seers bank itself.
	if want := len(seersCatherbyRoad) - 1; oldFlagged != want {
		t.Fatalf("the old classifier flagged %d of %d road tiles, want %d — the control no longer describes the live false positive", oldFlagged, len(seersCatherbyRoad), want)
	}
}

// The maze instance lies east of 2740 too (mapsquare 45,71). acct102 and acct167 logged
// "stranded" from 2891,4597 on 2026-09-23; the event sends the account back by itself.
func TestStrandedAcrossWaterIsFalseInsideTheMaze(t *testing.T) {
	maze := tile.Tile{X: 2891, Z: 4597}
	if freshstart.TravelLeg(maze) != freshstart.LegPier {
		t.Fatalf("control: the old rule must read the maze as LegPier for this test to mean anything")
	}
	if strandedAcrossWater(maze, catherbyStand) {
		t.Error("a donor inside the maze instance is not stranded across water")
	}
}

// Every live true positive is kept.
func TestStrandedAcrossWaterKeepsTheRealStrandings(t *testing.T) {
	for _, c := range []struct {
		name string
		here tile.Tile
	}{
		{"Acct173's Lumbridge respawn, 2026-09-23 01:43", tile.Tile{X: 3222, Z: 3217}},
		{"the \"catherby\" incident, 2026-09-16", tile.Tile{X: 3220, Z: 3259}},
		{"\"spare28\" at Varrock, 2026-09-18", tile.Tile{X: 3278, Z: 3500, Level: 1}},
	} {
		if !strandedAcrossWater(c.here, catherbyStand) {
			t.Errorf("%s: strandedAcrossWater(%+v) = false, want true", c.name, c.here)
		}
	}
}

// A destination WEST of 2740, the barbarian outpost's pipe, keeps the old meaning: Lumbridge is
// across the water from it, the pipe itself is not.
func TestStrandedAcrossWaterForAWesternStand(t *testing.T) {
	pipe := tile.Tile{X: 2552, Z: 3559}
	if !strandedAcrossWater(tile.Tile{X: 3222, Z: 3217}, pipe) {
		t.Error("Lumbridge must still read as stranded from the barbarian outpost")
	}
	if strandedAcrossWater(pipe, pipe) {
		t.Error("the stand itself is never stranded from itself")
	}
}

// A walk that ran out of time must never count toward the water check: that was the other half
// of the false alarm (three 75-second overruns in a row on a reachable road). Only the pack's own
// "no path" answer does.
func TestOnlyANoPathCountsTowardTheWaterCheck(t *testing.T) {
	n := 0
	for i := 0; i < 5; i++ {
		n = nextStandUnreachable(n, walkTimedOut)
	}
	if n >= standUnreachableBeforeCrossing {
		t.Errorf("five timeouts reached the water check (count %d); a timeout is not an unreachable", n)
	}
	n = 0
	for i := 0; i < standUnreachableBeforeCrossing; i++ {
		n = nextStandUnreachable(n, walkNoPath)
	}
	if n < standUnreachableBeforeCrossing {
		t.Errorf("%d consecutive no-path walks gave count %d; the water check must still be reachable", standUnreachableBeforeCrossing, n)
	}
	if got := nextStandUnreachable(n, walkReached); got != 0 {
		t.Errorf("arriving must reset the count, got %d", got)
	}
	if got := nextStandUnreachable(2, walkFailed); got != 0 {
		t.Errorf("a failed walk packet is not an unreachable either, got %d", got)
	}
}

// On the fleet's own pack (skipped without RSB0T_COLLISION_PACK): every road tile above really
// does route to the stand, and the Lumbridge true positive really does not. This is the
// ground truth the two pure tests above encode.
func TestTheSeersCatherbyRoadRoutesOnTheRealPack(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	to := pathfind.NavPoint{X: catherbyStand.X, Z: catherbyStand.Z, Level: catherbyStand.Level}
	for _, here := range seersCatherbyRoad {
		if res := pk.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, to); !res.OK {
			t.Errorf("FindPath %+v -> stand: %s — the road tile is not routable after all", here, res.Reason)
		}
	}
	if res := pk.FindPath(pathfind.NavPoint{X: 3222, Z: 3217}, to); res.OK {
		t.Error("FindPath Lumbridge -> Catherby stand succeeded; the true-positive case is not across water on this pack")
	}
}
