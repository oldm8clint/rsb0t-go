package questplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// The stronghold's row tables against the real collision pack (RSB0T_COLLISION_PACK): the three places the walls
// close are exactly the 49 + 28 + 37 tiles the tables name, so a pocket built on them neither leaves an account
// standing in the tower "outside" nor sends one in the courtyard to the ladder.
func TestKhazardStrongholdTablesAreTheRealPacksRooms(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rooms := []struct {
		name string
		seed tile.Tile
		want int
		in   func(tile.Tile) bool
	}{
		{"hall", KhazardHallLanding, 49, InKhazardHall},
		{"ladder room", KhazardLadderStand, 28, InKhazardLadderRoom},
		{"chest floor", KhazardLadderTopStand, 37, InKhazardChestFloor},
	}
	for _, r := range rooms {
		flood := pk.Flood(pathfind.NavPoint{X: r.seed.X, Z: r.seed.Z, Level: r.seed.Level}, 40)
		if len(flood) != r.want {
			t.Errorf("%s: the walls close %d tiles, want %d", r.name, len(flood), r.want)
		}
		// Every tile of the room is in the table...
		for xz := range flood {
			if at := (tile.Tile{X: xz[0], Z: xz[1], Level: r.seed.Level}); !r.in(at) || !InKhazardStronghold(at) {
				t.Errorf("%s: the room's tile %v is not in the table", r.name, at)
			}
		}
		// ...and every walkable tile the table names is in the room (a row that over-claims a walkable tile of
		// the courtyard or of another room would walk an account out of the tower early).
		for x := 2490; x <= 2520; x++ {
			for z := 3240; z <= 3270; z++ {
				at := tile.Tile{X: x, Z: z, Level: r.seed.Level}
				if r.in(at) && pk.Walkable(x, z, r.seed.Level) && !flood[[2]int{x, z}] {
					t.Errorf("%s: the table names the walkable tile %v, which the room's walls do not reach", r.name, at)
				}
			}
		}
	}
	// The doors' stands and landings are on the right sides of their walls.
	for _, c := range []struct {
		at   tile.Tile
		want bool
		why  string
	}{
		{KhazardHallLanding, true, "the wall's landing"}, {KhazardInnerDoorStand, true, "the inner door's hall side"},
		{tile.Tile{X: 2505, Z: 3255}, true, "the inner door's ladder room stand"}, {tile.Tile{X: 2504, Z: 3255}, true, "the inner door's landing"},
		{KhazardFrontDoorStand, true, "the front door's inside"}, {KhazardDoorTile, false, "the front door's outside"},
		{KhazardWallSouth, false, "the wall's south stand"}, {KhazardChestStand, true, "the chest's stand"},
	} {
		if !pk.Walkable(c.at.X, c.at.Z, c.at.Level) {
			t.Errorf("%s %v is not walkable in the pack", c.why, c.at)
		}
		if InKhazardStronghold(c.at) != c.want {
			t.Errorf("%s %v: in the stronghold %v, want %v", c.why, c.at, InKhazardStronghold(c.at), c.want)
		}
	}
}
