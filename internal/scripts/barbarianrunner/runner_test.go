package barbarianrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/barbarianplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// strandedAcrossWater is pure tile arithmetic (freshstart.TravelLeg underneath), so it's
// unit-tested directly like ResyncTo above — TravelToCourse's own live half (the fallback it
// gates, travelViaFreshStart) needs a live *Deps and is proven live instead, per this package's
// established convention.
func TestStrandedAcrossWaterMatchesTheRealDeathRespawnAndNothingElse(t *testing.T) {
	// A death respawns near Lumbridge — the exact production-blocking case this fallback was
	// built for (see TravelToCourse's own doc comment and COLLAB.md).
	if !strandedAcrossWater(freshstart.LumbridgeCentre) {
		t.Fatal("Lumbridge should read as stranded across water from the outpost")
	}
	// Already on Karamja, or already past the second ship (west of ArdougneSideMaxX) — no
	// boat is ahead, so this must NOT trigger the fallback.
	if strandedAcrossWater(tile.Tile{X: 2850, Z: 3150}) { // Musa Point, Karamja
		t.Fatal("Karamja should not read as stranded across water")
	}
	// Near the outpost itself (an ordinary same-level pathfind hiccup, not a real
	// displacement) must not spuriously trigger the boat/pickpocket fallback.
	if strandedAcrossWater(barbarianplan.PipeNorth) {
		t.Fatal("the outpost's own tile should not read as stranded across water")
	}
}

// ResyncTo is the one piece of the resync port (mirroring BarbarianAgility.ts's own
// resyncToNearest/resyncTo) that needs no live session — pure lap/tile arithmetic — so it's
// unit-tested directly. ResyncToNearest itself needs a live *Deps (FindObstacle reads
// d.Session/d.Locs/d.Config), matching this package's existing convention of proving its
// I/O-touching half live rather than mocking a session, per COLLAB.md.
func TestResyncToPicksTheNearestOccurrenceOfARepeatedName(t *testing.T) {
	lap := barbarianplan.DefaultLap // ..., "Crumbling wall", "Crumbling wall", "Crumbling wall"

	// The three crumbling walls stand at x=2535, 2538, 2541 (z=3553, level 0) in that lap
	// order. Standing right next to the middle one should resync to it, not the first.
	here := tile.Tile{X: 2538, Z: 3553, Level: 0}
	idx, ok := ResyncTo(lap, "Crumbling wall", here)
	if !ok {
		t.Fatal("ResyncTo returned ok=false for a name that is in the lap")
	}
	wantIdx, _ := firstOccurrenceAt(lap, "Crumbling wall", 1) // the 2nd of the 3 walls
	if idx != wantIdx {
		t.Fatalf("ResyncTo(..., middle wall's tile) = step %d, want %d (the middle occurrence)", idx, wantIdx)
	}

	// Standing next to the last wall resyncs to the last occurrence, not the first.
	here = tile.Tile{X: 2541, Z: 3553, Level: 0}
	idx, ok = ResyncTo(lap, "Crumbling wall", here)
	if !ok {
		t.Fatal("ResyncTo returned ok=false for a name that is in the lap")
	}
	wantIdx, _ = firstOccurrenceAt(lap, "Crumbling wall", 2) // the 3rd of the 3 walls
	if idx != wantIdx {
		t.Fatalf("ResyncTo(..., last wall's tile) = step %d, want %d (the last occurrence)", idx, wantIdx)
	}
}

func TestResyncToIsCaseInsensitiveAndReportsUnknownNames(t *testing.T) {
	lap := barbarianplan.DefaultLap
	here := tile.Tile{X: 2551, Z: 3554, Level: 0} // Ropeswing's own start tile

	if _, ok := ResyncTo(lap, "ROPESWING", here); !ok {
		t.Fatal("ResyncTo should match a lap name case-insensitively")
	}
	if _, ok := ResyncTo(lap, "Not an obstacle", here); ok {
		t.Fatal("ResyncTo should report ok=false for a name that isn't in the lap")
	}
}

// firstOccurrenceAt returns the lap index of the (occurrence)th step named name, in lap order
// — a small test-only helper mirroring what barbarianplan.OccurrenceOf/StartTileFor already
// use internally, so this test's expectations are derived the same way ResyncTo itself is.
func firstOccurrenceAt(lap []string, name string, occurrence int) (int, bool) {
	seen := 0
	for i, n := range lap {
		if n != name {
			continue
		}
		if seen == occurrence {
			return i, true
		}
		seen++
	}
	return 0, false
}
