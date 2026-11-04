package zonetrigger

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Lumbridge's map square 0_50_50 (x and z 3200-3263) plays its music by region
// (music/scripts/move.rs2: [mapzone,0_50_50] @music_playbyregion), so stepping into it fires; a
// step inside one zone fires nothing.
func TestEnteringAMusicSquareFires(t *testing.T) {
	if !Fires(tile.Tile{X: 3199, Z: 3220}, tile.Tile{X: 3200, Z: 3220}) {
		t.Fatal("stepping into map square 0_50_50 did not fire")
	}
	if Fires(tile.Tile{X: 3201, Z: 3201}, tile.Tile{X: 3202, Z: 3201}) {
		t.Fatal("a step inside one zone fired")
	}
}

// [zone,0_45_54_48_56] is the Wilderness warning north of Edgeville: the zone at 2928,3512 on level
// 0. Entering it fires; the same step a level up does not, and neither does one inside it.
func TestEnteringATriggeredZoneFires(t *testing.T) {
	if !Fires(tile.Tile{X: 2927, Z: 3512}, tile.Tile{X: 2928, Z: 3512}) {
		t.Fatal("stepping into the Wilderness warning zone did not fire")
	}
	if Fires(tile.Tile{X: 2927, Z: 3512, Level: 1}, tile.Tile{X: 2928, Z: 3512, Level: 1}) {
		t.Fatal("the level-0 zone fired on level 1")
	}
	if Fires(tile.Tile{X: 2928, Z: 3512}, tile.Tile{X: 2930, Z: 3514}) {
		t.Fatal("a step inside the zone fired")
	}
}

// FirstFiring is measured from the path's first tile and looks no further than it is asked to.
func TestFirstFiringCountsTilesToTheCrossing(t *testing.T) {
	var path []tile.Tile
	for x := 3190; x <= 3215; x++ {
		path = append(path, tile.Tile{X: x, Z: 3220})
	}
	if got := FirstFiring(path, 12); got != 10 {
		t.Fatalf("crossing 3199 -> 3200 from 3190: got %d, want 10", got)
	}
	if got := FirstFiring(path, 9); got != 0 {
		t.Fatalf("looking 9 tiles ahead: got %d, want 0", got)
	}
	// Running, the walker's route skips no zone: a two-tile step over the boundary still fires.
	if got := FirstFiring([]tile.Tile{{X: 3197, Z: 3220}, {X: 3199, Z: 3220}, {X: 3201, Z: 3220}}, 10); got != 4 {
		t.Fatalf("two-tile steps: got %d, want 4", got)
	}
	if got := FirstFiring(nil, 10); got != 0 {
		t.Fatalf("no path: got %d", got)
	}
}
