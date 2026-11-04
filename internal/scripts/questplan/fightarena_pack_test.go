package questplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// Why 2602,3153 is not a safespot, on the real collision pack (skipped without RSB0T_COLLISION_PACK). The threat
// model is the Black Demon's (grandtree_pack_test.go): a hostile body keeps a melee target only while a tile its body
// can stand on is orthogonally beside it with no wall between, and its body positions are every one a breadth-first
// search over the pack's steps reaches from where it starts.
//
// The study's claim (fightarena.go's old header) was that a size-2 beast coming from the north "freezes at 2602,3155
// behind the static Jeremy and can never reach it". That holds only while Jeremy's own tile, 2602,3154, is what the
// beast needs. It is not: the stand is open floor, and both of its sides are free for a body of either size.
// Production shows the rest: Bouncer on Jeremy's tile on 12 Oct, the ogre and the scorpion on it on 16 Oct.
func TestTheArenaStandHasFreeSidesBesideJeremy(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stand, jeremy := ArenaSafespot, tile.Tile{X: 2602, Z: 3154}
	for _, tl := range []tile.Tile{stand, jeremy, {X: 2601, Z: 3153}, {X: 2603, Z: 3153}} {
		if !pk.Walkable(tl.X, tl.Z, 0) {
			t.Fatalf("%d,%d is not walkable on the pack: the premise moved", tl.X, tl.Z)
		}
	}
	// A beast released from the Bouncer pen (2606,3162) wanders the floor within its maxrange of 35: bound the search by
	// the floor.
	for _, size := range []int{1, 2} {
		pos := gtBodyPositions(pk, 2606, 3162, size, 20)
		var beside, free []([2]int)
		for p := range pos {
			if !gtMeleeBeside(pk, p, size, stand) {
				continue
			}
			beside = append(beside, p)
			overlapsJeremy := false
			for i := 0; i < size; i++ {
				for j := 0; j < size; j++ {
					if p[0]+i == jeremy.X && p[1]+j == jeremy.Z {
						overlapsJeremy = true
					}
				}
			}
			if !overlapsJeremy {
				free = append(free, p)
			}
		}
		if len(beside) == 0 {
			t.Errorf("size %d: no body position stands beside the stand: it IS a safespot, and the premise of this file moved", size)
			continue
		}
		if len(free) == 0 {
			t.Errorf("size %d: every body position beside the stand overlaps Jeremy's tile (%d of them): Jeremy's occupancy is what holds it, and production says that does not hold", size, len(beside))
		}
		// Both sides of the stand, west and east of it on its row, are among them.
		west, east := false, false
		for _, p := range free {
			west = west || p[0] < stand.X
			east = east || p[0] > stand.X
		}
		if !west || !east {
			t.Errorf("size %d: free positions west=%v east=%v of the stand: %v", size, west, east, free)
		}
		t.Logf("size %d: %d of %d reachable body positions stand beside the stand, %d without touching Jeremy's tile: %v", size, len(beside), len(pos), len(free), free)
	}
}
