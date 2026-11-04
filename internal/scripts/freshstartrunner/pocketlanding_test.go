package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// TestEscapedToOpenGroundRejectsADeadEndClimb is the Ardougne Monastery shape in miniature
// (mapsquare 40,50, ground-truthed with cmd/doordiag against the real pack and a freshly
// baked loc index — see escapedToOpenGround's own doc comment for the exact rooms and ids):
// the only fixture stairOpsUp can find in the sealed ground-floor room climbs into a room
// that is just as sealed, one level up and in the same mapsquare. leftPocket says yes to that
// because the level changed, and saying yes is what let the account bounce between the two
// floors instead of ever reaching the door chain that actually leaves the building.
//
// The fake pack carries level 0 only, so the landing tile one level up has a one-tile
// component — exactly the "another sealed room" reading the real 24-tile bedroom produces
// against a threshold of 2000.
func TestEscapedToOpenGroundRejectsADeadEndClimb(t *testing.T) {
	const threshold = 5
	// A real corridor on level 0, so the "landed somewhere open" case below has somewhere to
	// land. Tiles are mapsquare-0 coordinates because buildConnectedDoorPack masks to one
	// mapsquare; the Monastery's own numbers are in escapedToOpenGround's doc comment.
	var walkable [][3]int
	var edges [][2][3]int
	for z := 10; z <= 17; z++ {
		walkable = append(walkable, [3]int{10, z, 0})
		if z > 10 {
			edges = append(edges, [2][3]int{{10, z - 1, 0}, {10, z, 0}})
		}
	}
	pk := buildConnectedDoorPack(t, walkable, edges)

	ground := tile.Tile{X: 10, Z: 12, Level: 0}
	upstairs := tile.Tile{X: 10, Z: 12, Level: 1}

	if escapedToOpenGround(pk, ground, upstairs, threshold) {
		t.Error("climbing into a sealed room one level up counted as an escape — this is the Monastery oscillation")
	}
	if !leftPocket(ground, upstairs) {
		t.Fatal("leftPocket should still see the level change; escapedToOpenGround must be the stricter test, not a rename")
	}
	if !escapedToOpenGround(pk, upstairs, ground, threshold) {
		t.Error("descending into a real, connected area should count as an escape")
	}
}

// TestEscapedToOpenGroundKeepsTheBasementPocketJump pins the case the original mapsquare-only
// check was built for: the "lubmy basement" pocket (mapsquare 50,150) climbs a Ladder and
// lands beside a Trapdoor 6400 tiles away in z, on ordinary Lumbridge-area surface. A
// mapsquare change is still an escape on its own — the component check must not be allowed to
// veto it, since the landing is open ground by construction and a threshold-sized flood fill
// there is pure cost.
func TestEscapedToOpenGroundKeepsTheBasementPocketJump(t *testing.T) {
	const threshold = 5
	pk := buildConnectedDoorPack(t, [][3]int{{10, 10, 0}}, nil)
	before := tile.Tile{X: 3212, Z: 9615, Level: 0}
	after := tile.Tile{X: 3209, Z: 3216, Level: 0}
	if !escapedToOpenGround(pk, before, after, threshold) {
		t.Error("the real lubmy-basement-to-surface jump must still count as an escape")
	}
}

// TestEscapedToOpenGroundRejectsOrdinaryMovement keeps the one thing this check has always had
// to reject: a single OpLoc that moved the account a tile or two inside the room it is already
// stuck in is not a rescue, however it is measured.
func TestEscapedToOpenGroundRejectsOrdinaryMovement(t *testing.T) {
	const threshold = 5
	pk := buildConnectedDoorPack(t, [][3]int{{10, 10, 0}, {10, 11, 0}}, [][2][3]int{{{10, 10, 0}, {10, 11, 0}}})
	before := tile.Tile{X: 10, Z: 10, Level: 0}
	for _, after := range []tile.Tile{before, {X: 10, Z: 11, Level: 0}} {
		if escapedToOpenGround(pk, before, after, threshold) {
			t.Errorf("moving to %d,%d,%d inside the same room is not an escape", after.X, after.Z, after.Level)
		}
	}
}

// TestEscapedToOpenGroundWithoutAPackDegradesToLeftPocket guards the nil-Pack path. Deps.Pack
// is set by every caller in this repo today, but escapedToOpenGround sits on the rescue path
// of last resort and must not turn a missing pack into "no escape is ever possible".
func TestEscapedToOpenGroundWithoutAPackDegradesToLeftPocket(t *testing.T) {
	before := tile.Tile{X: 10, Z: 10, Level: 0}
	if !escapedToOpenGround(nil, before, tile.Tile{X: 10, Z: 10, Level: 1}, largeComponentThreshold) {
		t.Error("with no pack to measure, a level change should still read as an escape")
	}
	if escapedToOpenGround(nil, before, before, largeComponentThreshold) {
		t.Error("with no pack, standing still must still not read as an escape")
	}
}
