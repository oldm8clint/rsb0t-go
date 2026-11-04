package questmods

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Khazard's building is registered as a pocket for every quest's walks (questrunner.Travel), so
// the live 4 Oct pilots left in the drunk guard's room by Fight Arena walk out through door1
// whatever quest the ladder runs next. The arena floor and the cell are not walked out of: no
// door1 is reachable from them (questplan.InArenaTrap; the quest itself is forced there).
func TestKhazardBuildingIsAPocket(t *testing.T) {
	for _, at := range []tile.Tile{{X: 2617, Z: 3141}, {X: 2618, Z: 3142}, {X: 2615, Z: 3141}, questplan.ArenaDoor1In, arenaDoor1West} {
		if name, ok := questrunner.PocketAt(at); !ok || name != "Khazard's building" {
			t.Errorf("%v: pocket %q %v", at, name, ok)
		}
	}
	for _, at := range []tile.Tile{{X: 2606, Z: 3166}, questplan.ArenaSafespot, questplan.HengradAnchor,
		{X: 2617, Z: 3172}, {X: 2584, Z: 3141}, questplan.KhazardBarAnchor, questplan.YanilleBank} {
		if name, ok := questrunner.PocketAt(at); ok {
			t.Errorf("%v is not in Khazard's building's pocket, got %q", at, name)
		}
	}
}

// The door1s the exit uses: the west one (loc 81 at 2585,3141, a west wall) opens from its own
// column, the north one (2617,3171, a north wall) from its own row (check_axis), and both inside
// tiles are in the building with their outside neighbour out of it.
func TestArenaDoor1InsideTiles(t *testing.T) {
	for _, c := range []struct{ in, out tile.Tile }{
		{arenaDoor1West, tile.Tile{X: 2584, Z: 3141}},
		{questplan.ArenaDoor1In, questplan.ArenaDoor1Out},
	} {
		if !questplan.InKhazardPocket(c.in) || questplan.InArenaBuilding(c.out) {
			t.Errorf("door1 inside %v / outside %v", c.in, c.out)
		}
	}
}
