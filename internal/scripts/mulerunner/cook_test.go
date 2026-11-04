package mulerunner

import (
	"testing"

	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
)

// The way home from a cook round crosses the door only from the range's side of it. Every tile
// the live 2026-09-20 log left the mule on in the range room counts; the door's own tile, the
// bank post and the room east of the second door do not.
func TestOnRangeSide(t *testing.T) {
	in := []tile.Tile{
		catherbyDoorInside,
		{X: 2817, Z: 3443, Level: 0}, // where every live yield left the mule
		{X: 2818, Z: 3442, Level: 0}, // labfleettest travel's range-room start
	}
	for _, p := range in {
		if !onRangeSide(p) {
			t.Errorf("%v: want range side", p)
		}
	}
	out := []tile.Tile{
		catherbyDoorTile,
		mulereceiverplan.CatherbyBankTile,
		{X: 2809, Z: 3441, Level: 0},
		{X: 2828, Z: 3441, Level: 0},
		{X: 2817, Z: 3443, Level: 1},
	}
	for _, p := range out {
		if onRangeSide(p) {
			t.Errorf("%v: want bank side", p)
		}
	}
}
