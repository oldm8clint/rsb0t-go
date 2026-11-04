package fleetplan

import (
	"testing"
)

func TestMarketOnWorld(t *testing.T) {
	cases := []struct {
		world   int
		markets []int
		want    bool
	}{
		{1, DefaultMarketWorlds(), true},
		{2, DefaultMarketWorlds(), false}, // the world-2 cohort: the Seers stall is on world 1
		{3, DefaultMarketWorlds(), false},
		{0, DefaultMarketWorlds(), true}, // unknown: a one-world deployment
		{2, nil, true},                   // no list: any world
		{2, []int{1, 2}, true},
	}
	for _, c := range cases {
		if got := MarketOnWorld(c.world, c.markets); got != c.want {
			t.Errorf("MarketOnWorld(%d, %v) = %v, want %v", c.world, c.markets, got, c.want)
		}
	}
}
