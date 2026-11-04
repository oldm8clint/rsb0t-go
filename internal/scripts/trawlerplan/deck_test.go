package trawlerplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The tiles crew were left on when a block ended (live 5 Nov 2026): the docked deck and the wreck's water.
func TestOnDeckAndAtSea(t *testing.T) {
	cases := []struct {
		at          tile.Tile
		deck, atSea bool
	}{
		{tile.Tile{X: 2673, Z: 3172, Level: 1}, true, false},
		{tile.Tile{X: 2672, Z: 3171, Level: 1}, true, false},
		{tile.Tile{X: 2671, Z: 3177, Level: 0}, true, false},
		{tile.Tile{X: 2676, Z: 3170, Level: 0}, false, false}, // the pier
		{tile.Tile{X: 1955, Z: 4823, Level: 0}, false, true},  // the wreck
		{tile.Tile{X: 1885, Z: 4826, Level: 0}, false, true},  // the boat
		{tile.Tile{X: 2655, Z: 3283, Level: 0}, false, false},
	}
	for _, c := range cases {
		if OnDeck(c.at) != c.deck || AtSea(c.at) != c.atSea {
			t.Errorf("%v: OnDeck %v AtSea %v, want %v %v", c.at, OnDeck(c.at), AtSea(c.at), c.deck, c.atSea)
		}
	}
}
