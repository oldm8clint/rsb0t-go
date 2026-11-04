package woodcutplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// mapleTrees are the loc 1307 placements the content puts around the Seers bank
// (content/maps/m42_54.jm2, local coords 33/39/42,45 and 45,43). Every one is 2x2.
var mapleTrees = []tile.Tile{
	{X: 2721, Z: 3501}, {X: 2727, Z: 3501}, {X: 2730, Z: 3501}, {X: 2733, Z: 3499},
}

// The Maple anchor is pinned because its old value, 2727,3496, was walkable and looked fine:
// it sat inside a sealed 22-tile strip the Seers bank cannot reach, and five tiles from any
// maple. An anchor no tree is within leash of chops nothing, whatever the pathfinder says.
func TestMapleAnchorHasMapleTreesInsideItsLeash(t *testing.T) {
	maple := TargetTier("Maple")
	if maple.Anchor.Z <= 3497 {
		t.Fatalf("Maple anchor is %v: z 3495-3497 is the sealed strip north of the Seers bank, not the maple patch", maple.Anchor)
	}
	for _, tree := range mapleTrees {
		if d := tile.Chebyshev(maple.Anchor, tree); d > maple.Leash {
			t.Errorf("maple at %v is %d tiles from the anchor %v, past the leash of %d", tree, d, maple.Anchor, maple.Leash)
		}
	}
}

// Every anchor sits within seventy tiles of the Seers bank, which the package doc states as a
// fact about the ladder. It is also what would have caught a stray tier anchor.
func TestEveryTierAnchorIsNearSeers(t *testing.T) {
	for _, tr := range TIERS {
		if d := tile.Chebyshev(tr.Anchor, Seers); d > 70 {
			t.Errorf("%s anchor %v is %d tiles from the Seers bank", tr.Label, tr.Anchor, d)
		}
		if tr.Anchor.Level != 0 {
			t.Errorf("%s anchor %v is not on the ground level", tr.Label, tr.Anchor)
		}
	}
}

// TestNormalTreeCampIsTheAwkwardOne records what an offline pack measurement found when a
// production stall report came in (2026-09-21), and it is worth reading the retraction first.
//
// THE REPORT'S CAUSAL CLAIM WAS WITHDRAWN BY ITS OWN AUTHOR within the hour. It said this camp
// was pathing-bound rather than tree-bound, on two accounts showing +200 xp with 33 stalls
// against +950 with 5. Nine more minutes showed both accounts ACCELERATING hard (to roughly
// 3,300 and 7,500 xp/h) while the fleet stall counter rose 182 -> 221, and the gap narrowing
// from 4.8x to 2.7x and still closing. Stalls rise WITH throughput, because chopping faster
// means more tree-to-tree walks and every walk can stall: they are frequent but cheap, a retry
// rather than a wedge. **The tree-contention analysis this ladder's cohort guidance rests on is
// NOT displaced by anything that was measured.**
//
// What survives is narrow and offline: against the real collision pack, the Seers normal camp is
// the only tier where an accepted stand tile has no route from the anchor (1 of 18; every other
// tier: 0). That is cmd/woodanchortest's measurement to make, not this package's — a plan
// package has no pack. What this test pins is the one thing it CAN check, and the reason the
// camp matters at all: every swapped account lands here.
func TestNormalTreeCampIsTheAwkwardOne(t *testing.T) {
	normal := TIERS[0]
	if normal.Label != "Normal tree" {
		t.Fatalf("the ladder's first rung is %q — this test names the wrong one", normal.Label)
	}
	// Every swapped account starts here, whatever its target, because CurrentTier gates on
	// StartAt and a swapped fisher arrives at Woodcutting 1.
	for _, target := range TargetOptions {
		if got := CurrentTier(1, target); got.Label != normal.Label {
			t.Errorf("a Woodcutting 1 account targeting %q starts on %q, not the normal tree", target, got.Label)
		}
	}
	// NOT asserted here, and the first draft of this test got it wrong by guessing: the normal
	// tree does NOT have the widest leash on the ladder (Oak's is 14 against its 10). What the
	// pack measurement actually found is that it has the MOST TREES inside its leash — 12,
	// against Oak 5, Willow 6, Maple 4, Yew 3, Magic 3 — which is what makes it dense enough
	// for one tree to sit behind the others. That count comes from the loc index, so it is
	// cmd/woodanchortest's to assert and not this package's; it is recorded here because the
	// number is the reason this camp is the awkward one and a reader should not have to guess
	// at it the way this test first did.
}
