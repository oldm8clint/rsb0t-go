package freshstartrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

func TestRangeDoorSidesWithCollisionPack(t *testing.T) {
	path := os.Getenv("RS2BOT_TEST_COLLISION_PACK")
	if path == "" {
		t.Skip("set RS2BOT_TEST_COLLISION_PACK to check the real range room")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	room := tile.Tile{X: 2818, Z: 3442, Level: 0}
	bank := tile.Tile{X: 2809, Z: 3441, Level: 0}
	for _, tc := range []struct{ from, dest, near, far tile.Tile }{
		{room, bank, rangeDoorInside, rangeDoorOutside},
		{bank, room, rangeDoorOutside, rangeDoorInside},
	} {
		near, far, ok := rangeDoorSides(pk, tc.from, tc.dest)
		if !ok || near != tc.near || far != tc.far {
			t.Fatalf("%v -> %v: near=%v far=%v ok=%v", tc.from, tc.dest, near, far, ok)
		}
	}
	if _, _, ok := rangeDoorSides(pk, tile.Tile{X: 2818, Z: 3442, Level: 1}, bank); ok {
		t.Fatal("wrong level accepted")
	}
	if _, _, ok := rangeDoorSides(pk, tile.Tile{X: 3100, Z: 3100}, bank); ok {
		t.Fatal("unrelated room accepted")
	}
}
