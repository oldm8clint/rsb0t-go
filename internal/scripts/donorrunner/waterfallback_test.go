package donorrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// TestStrandedAcrossWaterMatchesRealIncident reproduces the real, live-caught production
// incident this fallback exists for (account "acct", ProgressiveFisher, 2026-09-16):
// stuck at 3220,3259,0, repeatedly logging "donor: no path to 2809,3441,0: unreachable"
// while trying to reach its own configured muleAtTile (2809,3441,0 — Catherby's bank,
// fisherplan.Catherby().Bank). Ground-truthed independently against the real collision pack
// (rsb0t-main/out/collision.lcnav.gz) via pack.FindPath({3220,3259,0}, {2809,3441,0}):
// OK=false, reason="unreachable", expanded=216661 (the entire mainland component) — a genuine
// cross-water case, not a transient pathing hiccup, confirming strandedAcrossWater must
// return true here so Run's own MeetWalk loop stops re-trying a walk that can never succeed
// and instead falls back to crossWaterTo.
func TestStrandedAcrossWaterMatchesRealIncident(t *testing.T) {
	stuck := tile.Tile{X: 3220, Z: 3259, Level: 0}
	if !strandedAcrossWater(stuck, catherbyStand) {
		t.Errorf("strandedAcrossWater(%+v) = false, want true — this is the exact live-caught stuck tile that never reaches 2809,3441,0 by plain walking", stuck)
	}
}

// TestStrandedAcrossWaterFalseOncePastTheShips guards against over-triggering: once an
// account is already on the far side of either boat leg, a plain walk really can finish the
// trip (ground-truthed: pack.FindPath from 2683,3271,0 — the tile freshstart.ArdougneGangplank
// drops an account at — to 2809,3441,0 succeeds in 68 waypoints), so strandedAcrossWater must
// say false there — Run should keep retrying the ordinary walk, not restart a 20-minute boat
// detour every time a single walkNear call happens to fail for some other, unrelated reason.
func TestStrandedAcrossWaterFalseOncePastTheShips(t *testing.T) {
	cases := []struct {
		name string
		here tile.Tile
	}{
		{"just off the Ardougne-side gangplank", tile.Tile{X: 2683, Z: 3271, Level: 0}},
		{"just inside the Ardougne-side boundary (ArdougneSideMaxX)", tile.Tile{X: 2739, Z: 3300, Level: 0}},
	}
	for _, tc := range cases {
		if strandedAcrossWater(tc.here, catherbyStand) {
			t.Errorf("%s: strandedAcrossWater(%+v) = true, want false — this tile is already on the destination side of the water", tc.name, tc.here)
		}
	}
}

// TestStrandedAcrossWaterFalseOnKaramjaSide checks the Karamja mid-leg case: an account
// between the two ships (e.g. freshstart.BrimhavenPier, 2772,3234,0) still can't reach a
// Catherby-area dest by plain walking (ground-truthed: pack.FindPath from there to
// 2809,3441,0 is unreachable too, needing the second ship) but freshstart.TravelLeg
// classifies it as "go to destination" rather than "go to the pier", the same distinction
// fisherrunner.ReturnToCatherby's own identical check already relies on — SailLeg (driven by
// crossWaterTo's PhaseTravel branch) resolves which physical leg is next from the live tile
// itself, so this is intentionally not "false" from strandedAcrossWater's own point of view:
// the account is mid-journey, not falsely flagged as needing to restart the whole crossing.
func TestStrandedAcrossWaterOnKaramjaSideNeedsDestinationLeg(t *testing.T) {
	brimhavenPier := tile.Tile{X: 2772, Z: 3234, Level: 0}
	if strandedAcrossWater(brimhavenPier, catherbyStand) {
		t.Errorf("strandedAcrossWater(%+v) = true, want false — Karamja-side tiles are classified as needing the destination leg (still handled by crossWaterTo's PhaseTravel), not restarted from the pier", brimhavenPier)
	}
}
