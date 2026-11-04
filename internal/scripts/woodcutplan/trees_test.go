package woodcutplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func at(x, z int) tile.Tile { return tile.Tile{X: x, Z: z} }

func TestTreeBlocksExpireOnTheirOwn(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var b TreeBlocks
	tree := at(3091, 3346)

	if b.Blocked(tree, now) {
		t.Error("a fresh blocklist blocks nothing")
	}
	b.Block(tree, now)
	if !b.Blocked(tree, now) {
		t.Error("a just-blocked tree should be blocked")
	}
	if !b.Blocked(tree, now.Add(EntBlockFor-time.Second)) {
		t.Error("still blocked one second before expiry")
	}
	// The expiry is what makes this self-healing: an account that walked away cannot observe
	// the Ent leaving, so a block that only ever cleared on observation would accumulate until
	// the camp had no trees left.
	if b.Blocked(tree, now.Add(EntBlockFor)) {
		t.Error("the block should have expired exactly at its deadline")
	}
	// Another tree is unaffected.
	if b.Blocked(at(3092, 3346), now) {
		t.Error("blocking one tree must not block another")
	}
}

// TestTreeBlocksAreKeyedByTileNotByLoc pins the reason the key is a tile: a felled tree
// respawns as a DIFFERENT loc at the same place, so an id-keyed block would be dropped exactly
// when the Ent is most likely to still be standing there.
func TestTreeBlocksAreKeyedByTileNotByLoc(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var b TreeBlocks
	b.Block(at(3091, 3346), now)
	// The same tile on a different level is a different tree.
	upstairs := tile.Tile{X: 3091, Z: 3346, Level: 1}
	if b.Blocked(upstairs, now) {
		t.Error("a tile on another level must not share a block")
	}
}

func TestTreeBlocksSweepAndLen(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var b TreeBlocks
	b.Block(at(1, 1), now)
	b.Block(at(2, 2), now)
	if got := b.Len(now); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
	later := now.Add(EntBlockFor + time.Second)
	if got := b.Len(later); got != 0 {
		t.Errorf("Len after expiry = %d, want 0", got)
	}
	b.Sweep(later)
	if len(b.until) != 0 {
		t.Errorf("Sweep left %d entries behind — the map would grow over a long run", len(b.until))
	}
	// Sweeping must not drop a block that is still live.
	b.Block(at(3, 3), later)
	b.Sweep(later)
	if !b.Blocked(at(3, 3), later) {
		t.Error("Sweep dropped a live block")
	}
}

func TestPickTreeTakesTheNearestUnblockedOne(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	near, mid, far := at(10, 10), at(20, 20), at(30, 30)
	candidates := []TreeCandidate{
		{At: far, Distance: 9},
		{At: near, Distance: 1},
		{At: mid, Distance: 4},
	}

	got, ok := PickTree(candidates, nil, now)
	if !ok || got.At != near {
		t.Errorf("with nothing blocked PickTree = %+v ok=%v, want the nearest", got, ok)
	}

	var b TreeBlocks
	b.Block(near, now)
	got, ok = PickTree(candidates, &b, now)
	if !ok || got.At != mid {
		t.Errorf("with the nearest blocked PickTree = %+v ok=%v, want the next nearest", got, ok)
	}

	// THE CASE THAT MATTERS: every tree blocked must report "none", never "the nearest
	// anyway". Chopping a blocked tree is precisely what breaks the axe and buys a cross-map
	// repair trip to Bob's.
	b.Block(mid, now)
	b.Block(far, now)
	if got, ok := PickTree(candidates, &b, now); ok {
		t.Errorf("every tree is blocked but PickTree returned %+v — that would break the axe", got)
	}

	// And once the blocks lapse, the camp is usable again with no observation needed.
	if got, ok := PickTree(candidates, &b, now.Add(EntBlockFor)); !ok || got.At != near {
		t.Errorf("after expiry PickTree = %+v ok=%v, want the nearest again", got, ok)
	}

	// No candidates at all is "none", not a zero-value tree.
	if _, ok := PickTree(nil, &b, now); ok {
		t.Error("PickTree(nil) should report none")
	}
}

// TestBlockForNeverShortensALongerExclusion pins the one rule that makes two exclusions with
// different lifetimes safe to share a set.
//
// The Ent block errs long on purpose — the cost of blocking a tree too long is one step to a
// neighbour, the cost of un-blocking too early is a broken axe and a cross-map repair trip. The
// unreachable block is 30s, because "the tiles round this tree were blocked a moment ago" is a
// passing state at a dense camp. If the short one could overwrite the long one, an Ent block
// would silently become a 30-second block the moment the same tree also read as unreachable —
// which is exactly when it would, since an Ent standing on a tree blocks the tiles around it.
func TestBlockForNeverShortensALongerExclusion(t *testing.T) {
	now := time.Now()
	at := tile.Tile{X: 3090, Z: 3345}
	var b TreeBlocks

	b.Block(at, now) // the Ent block: EntBlockFor
	b.BlockFor(at, now, UnreachableBlockFor)
	if !b.Blocked(at, now.Add(UnreachableBlockFor+time.Second)) {
		t.Error("a short block shortened an Ent block")
	}
	if !b.Blocked(at, now.Add(EntBlockFor-time.Second)) {
		t.Error("the Ent block should still hold just before its own expiry")
	}
	if b.Blocked(at, now.Add(EntBlockFor+time.Second)) {
		t.Error("the Ent block should have expired")
	}

	// The other way round it DOES extend, which is what a caller wants: an Ent seen at a tree
	// already excluded as unreachable is the more serious of the two.
	var c TreeBlocks
	c.BlockFor(at, now, UnreachableBlockFor)
	c.Block(at, now)
	if !c.Blocked(at, now.Add(UnreachableBlockFor+time.Second)) {
		t.Error("an Ent block should extend a shorter one")
	}

	// And the short block on its own really is short.
	var s TreeBlocks
	s.BlockFor(at, now, UnreachableBlockFor)
	if !s.Blocked(at, now.Add(UnreachableBlockFor-time.Second)) {
		t.Error("the unreachable block should hold for its own duration")
	}
	if s.Blocked(at, now.Add(UnreachableBlockFor+time.Second)) {
		t.Error("the unreachable block should expire on its own")
	}
	if UnreachableBlockFor >= EntBlockFor {
		t.Errorf("UnreachableBlockFor (%s) must be shorter than EntBlockFor (%s)", UnreachableBlockFor, EntBlockFor)
	}
}
