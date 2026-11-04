package fleetplan

import (
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// WestFundReach bounds how far from the East Ardougne market a western fund rung may be (the
// Paladins at West Ardougne's 2580,3297 are 75 tiles out and count).
const WestFundReach = 80

// WestFundRung is the pickpocket a western Stage 6 account earns coins at: of the rungs its
// Thieving level opens on this side of the ships (LadderRungs, the same locality rule Stage 2's
// ladder obeys), the one with the most coins per tick once stuns and eating are counted. ok is
// false when the level opens none (below Warrior woman's 25), and the caller falls back.
//
// Coins per tick, not XP per tick: this is a fare being raised, and a Guard at 40 (30 coins) is
// out-earned by nothing below Knights, while the XP ladder might prefer something else.
//
// Only rungs within WestFundReach of the East Ardougne market count: the Watchmen (Yanille, up a
// floor) and the Gnomes (the Stronghold) are on this side of the ships but are a trip in their own
// right for a purse of about a hundred coins.
func WestFundRung(level int) (thieverplan.Method, bool) {
	var best RungRate
	found := false
	market := tile.Tile{X: thieverplan.ArdougneMarket.X, Z: thieverplan.ArdougneMarket.Z}
	for _, m := range LadderRungs() {
		if m.Level > level || PickpocketCoins[m.Label] <= 0 {
			continue
		}
		if m.Anchor.Level != 0 || tile.Chebyshev(tile.Tile{X: m.Anchor.X, Z: m.Anchor.Z}, market) > WestFundReach {
			continue
		}
		r := RateRung(m, level, DefaultLadderCosts())
		if !found || r.CoinsPerTick > best.CoinsPerTick {
			best, found = r, true
		}
	}
	return best.Method, found
}
