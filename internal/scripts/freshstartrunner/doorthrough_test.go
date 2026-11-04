package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// buildDoorPack builds a minimal synthetic LCNV pack (the same v1 wire format
// pathfind.Pack.Parse decodes — see internal/pathfind/pack_test.go's own syntheticPack for the
// reference layout this mirrors) holding exactly one mapsquare (0,0), level 0, with only the
// given tiles marked walkable and every ExitMask left at 0 (no exits at all). Tests that need
// flood-fill connectivity add it themselves via exit bits; the through-tile picker under test
// here only cares about Walkable() and about which tiles a flood fill from a given start can
// reach, so an isolated single-tile "reachable" set is enough to exercise it.
func buildDoorPack(t *testing.T, walkable ...[3]int) *pathfind.Pack {
	t.Helper()
	const exitBytes, walkBytes = 4096, 512
	exit := make([]byte, exitBytes)
	walk := make([]byte, walkBytes)
	for _, xz := range walkable {
		x, z := xz[0], xz[1]
		idx := (x&0x3f)*64 + (z & 0x3f)
		walk[idx>>3] |= 1 << uint(idx&7)
	}
	raw := append([]byte{'L', 'C', 'N', 'V', 1, 1, 0, 0, 1, 0}, []byte{0, 0, 0b0001}...)
	raw = append(raw, exit...)
	raw = append(raw, walk...)
	p, err := pathfind.Parse(raw)
	if err != nil {
		t.Fatalf("buildDoorPack: %v", err)
	}
	return p
}

// TestPickFloodFillThroughPrefersDestWhenBothSidesAreExcluded guards the real live bug found
// 2026-09-16 across a 30-account production fleet (see pickFloodFillThrough's own doc comment
// for the full incident): tryOpenDoor's flood-fill through-tile strategy used to walk its 4
// orthogonal candidates in a FIXED order and take the first one the flood fill (run fresh from
// wherever the account happens to be standing THIS call) couldn't reach — with no regard for
// which one actually leads toward dest. When a door sits in a tight vestibule where BOTH
// candidates across it are equally "not reachable from here" (confirmed live: two production
// doors, both reporting the identical "pocket size 29" from either side), the fixed order always
// won, and because "from" flips to whichever tile got picked last call, the two candidates took
// turns being "the chosen one" — the account walked back and forth between them forever. This
// test reproduces exactly that shape (both the north and south neighbour of the door are
// walkable and excluded from the tiny reachable set the flood fill finds) and checks that the
// candidate actually closer to dest wins regardless of doorApproach's own fixed iteration order —
// which the old code could not do, since it always returned the north candidate (first in
// doorApproach) no matter where dest was.
func TestPickFloodFillThroughPrefersDestWhenBothSidesAreExcluded(t *testing.T) {
	// found sits at (10,5,0); its 4 orthogonal neighbours are (10,6) north, (10,4) south,
	// (11,5) east, (9,5) west. usedOff points east (11,5) — "the tile we approached from" —
	// which pickFloodFillThrough must always exclude regardless of walkability or reachability.
	// West (9,5) is left unwalkable so only north/south remain as real candidates, matching the
	// real incident's own two live candidates.
	found := tile.Tile{X: 10, Z: 5, Level: 0}
	usedOff := struct{ dx, dz int }{1, 0} // east: (11,5)
	from := tile.Tile{X: 11, Z: 5, Level: 0}
	pk := buildDoorPack(t,
		[3]int{10, 4, 0}, // south candidate
		[3]int{10, 6, 0}, // north candidate
		[3]int{11, 5, 0}, // from (the approach tile) — walkable but isolated: no exit bits at
		// all, so the flood fill from here reaches only itself, leaving BOTH north and south
		// "not reachable" — the exact double-exclusion the real incident hit.
	)

	cases := []struct {
		name string
		dest tile.Tile
		want tile.Tile
	}{
		{"dest south picks the south candidate", tile.Tile{X: 10, Z: 0, Level: 0}, tile.Tile{X: 10, Z: 4, Level: 0}},
		{"dest north picks the north candidate", tile.Tile{X: 10, Z: 20, Level: 0}, tile.Tile{X: 10, Z: 6, Level: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, pocketSize, ok := pickFloodFillThrough(pk, found, usedOff, from, c.dest)
			if !ok {
				t.Fatal("pickFloodFillThrough found no candidate")
			}
			if got != c.want {
				t.Errorf("picked %+v, want %+v (dest %+v)", got, c.want, c.dest)
			}
			if pocketSize != 1 {
				t.Errorf("pocket size = %d, want 1 (from is isolated)", pocketSize)
			}
		})
	}
}

// TestPickFloodFillThroughSkipsUnwalkableAndApproachTile guards the two earlier, already-fixed
// real bugs pickFloodFillThrough's own doc comment/tryOpenDoor's history describes: an unwalkable
// candidate must never be picked just because it happens to come first in doorApproach's fixed
// order (found live 2026-09-15, door "Door" 3107,3162, pocket size 15), and the tile the account
// just approached from must never be re-offered as its own "through" target.
func TestPickFloodFillThroughSkipsUnwalkableAndApproachTile(t *testing.T) {
	found := tile.Tile{X: 10, Z: 5, Level: 0}
	usedOff := struct{ dx, dz int }{0, -1} // approached from the south: (10,4)
	from := tile.Tile{X: 10, Z: 4, Level: 0}
	// North (10,6) is walkable and isolated (a real exit); south (10,4) is the approach tile
	// (must be skipped); east/west are simply never marked walkable at all.
	pk := buildDoorPack(t, [3]int{10, 4, 0}, [3]int{10, 6, 0})

	got, _, ok := pickFloodFillThrough(pk, found, usedOff, from, tile.Tile{X: 100, Z: 100, Level: 0})
	if !ok {
		t.Fatal("pickFloodFillThrough found no candidate")
	}
	want := tile.Tile{X: 10, Z: 6, Level: 0}
	if got != want {
		t.Errorf("picked %+v, want %+v (the only walkable, non-approach candidate)", got, want)
	}
}

// TestPickFloodFillThroughNoneQualify checks the "found nothing" path callers rely on to fall
// back to the mechanical-opposite guess: when every neighbour is either the approach tile,
// unwalkable, or already reachable from "from", there is no discriminating candidate at all.
func TestPickFloodFillThroughNoneQualify(t *testing.T) {
	found := tile.Tile{X: 10, Z: 5, Level: 0}
	usedOff := struct{ dx, dz int }{0, -1}
	from := tile.Tile{X: 10, Z: 4, Level: 0}
	// No tiles marked walkable at all, so every candidate fails the walkability check.
	pk := buildDoorPack(t)

	if _, _, ok := pickFloodFillThrough(pk, found, usedOff, from, tile.Tile{X: 0, Z: 0, Level: 0}); ok {
		t.Fatal("expected no candidate when nothing nearby is walkable")
	}
}
