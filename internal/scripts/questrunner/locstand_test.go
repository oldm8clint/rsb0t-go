package questrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

func TestWalledFromRectJudgesTheFaceTowardTheLoc(t *testing.T) {
	// A 1x1 loc at 10,10; each neighbour is walled only when its face toward the loc is.
	cases := []struct {
		name string
		at   tile.Tile
		wall byte
		want bool
	}{
		{"east neighbour, west face walled", tile.Tile{X: 11, Z: 10}, 1 << 3, true},
		{"east neighbour, east face walled", tile.Tile{X: 11, Z: 10}, 1 << 1, false},
		{"west neighbour, east face walled", tile.Tile{X: 9, Z: 10}, 1 << 1, true},
		{"north neighbour, south face walled", tile.Tile{X: 10, Z: 11}, 1 << 2, true},
		{"south neighbour, north face walled", tile.Tile{X: 10, Z: 9}, 1 << 0, true},
		{"south neighbour, south face walled", tile.Tile{X: 10, Z: 9}, 1 << 2, false},
		{"diagonal is not judged", tile.Tile{X: 11, Z: 11}, 0xf, false},
	}
	for _, c := range cases {
		if got := walledFromRect(c.wall, c.at, 10, 10, 1, 1); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// A 2x3 footprint at 10,10: the east side is x 12, z 10..12.
	if !walledFromRect(1<<3, tile.Tile{X: 12, Z: 12}, 10, 10, 2, 3) {
		t.Error("the east side of a 2x3 footprint was not judged")
	}
	if walledFromRect(1<<3, tile.Tile{X: 12, Z: 13}, 10, 10, 2, 3) {
		t.Error("a corner diagonal of a 2x3 footprint was judged")
	}
}

// On the fleet's own pack (skipped without RSB0T_COLLISION_PACK): Murder Mystery's flour barrel
// at 2735,3582 is against the Sinclair kitchen's east wall, so the tile east of it is walled off
// and the one south of it, inside the kitchen, is not (murder-empty-2's "I can't reach that!").
func TestMurderFlourBarrelEastTileIsWalledOnThePack(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	east := tile.Tile{X: 2736, Z: 3582}
	south := tile.Tile{X: 2735, Z: 3581}
	t.Logf("east 2736,3582 walkable %v wall %04b; south 2735,3581 walkable %v wall %04b",
		pk.Walkable(east.X, east.Z, 0), pk.WallMask(east.X, east.Z, 0), pk.Walkable(south.X, south.Z, 0), pk.WallMask(south.X, south.Z, 0))
	if pk.Walkable(east.X, east.Z, 0) && !walledFromRect(pk.WallMask(east.X, east.Z, 0), east, 2735, 3582, 1, 1) {
		t.Errorf("2736,3582 reads as a stand for the barrel (wall nibble %04b)", pk.WallMask(east.X, east.Z, 0))
	}
	if !pk.Walkable(south.X, south.Z, 0) || walledFromRect(pk.WallMask(south.X, south.Z, 0), south, 2735, 3582, 1, 1) {
		t.Errorf("2735,3581 is not a stand for the barrel (walkable %v, wall nibble %04b)",
			pk.Walkable(south.X, south.Z, 0), pk.WallMask(south.X, south.Z, 0))
	}
}
