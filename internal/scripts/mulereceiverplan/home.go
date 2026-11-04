package mulereceiverplan

import "rsb0t-go/internal/scripts/tile"

// HomeStep is what MuleReceiver does next on its way back to its bank post.
type HomeStep string

const (
	// HomeArrived: standing on the post.
	HomeArrived HomeStep = "arrived"
	// HomeWalkLocal: already on Catherby's side of White Wolf Mountain, where the plain,
	// door-aware local walk reaches the post without going near a wolf. This is the walk the
	// mule has always used for the short trip back from the range room or the beach.
	HomeWalkLocal HomeStep = "walk-local"
	// HomeWalk: the webwalk planner found a route home that stays out of wolf territory
	// (ships and their fares included). Walk it.
	HomeWalk HomeStep = "walk"
	// HomeToFundSpot: no wolf-free route with the coins held, and nowhere near the Lumbridge
	// Men that pay the fare. Walk there first (also wolf-free) rather than waiting for a Man.
	HomeToFundSpot HomeStep = "to-fund-spot"
	// HomeFund: no wolf-free route with the coins held. Earn the fare where the account
	// stands: pickpocketing at Lumbridge, or Luthas's banana crates on Karamja.
	HomeFund HomeStep = "fund"
)

// HomeView is the live state DecideHome reads.
type HomeView struct {
	AtHome bool
	// LocalSafe: on Catherby's side of the mountain, and a plain walk home stays out of wolf
	// territory (see PlainRouteSafe).
	LocalSafe bool
	// SafeRoute: the webwalk planner, with wolf territory as no-go zones and the coins held,
	// found a route to the post. RouteFare is the total of every fare on that route.
	SafeRoute bool
	RouteFare int
	// Coins held, and Fare still ahead of the account from where it stands (both ships from
	// the mainland, one from Karamja, none from the Ardougne side).
	Coins, Fare int
	OnKaramja   bool
	// FarFromFundSpot: on the mainland and far from the Lumbridge pickpocket spot.
	FarFromFundSpot bool
}

// DecideHome picks the next step home. The one thing it never picks is a walk whose route
// crosses White Wolf Mountain: an account that cannot get home without the pass earns a boat
// fare instead, because a combat-3 mule with 10 hitpoints does not survive the pass (live
// 2026-09-19: `mule` was attacked on the eastern foothills over and over, the combat flee
// pushed it further east each time, it died twice and completed no trades for hours).
func DecideHome(v HomeView) HomeStep {
	switch {
	case v.AtHome:
		return HomeArrived
	case v.LocalSafe:
		return HomeWalkLocal
	case v.SafeRoute && v.Coins >= v.RouteFare:
		// The planner checks each fare against the coins held on its own, so 30 coins
		// "afford" a route with two 30-coin ships. Setting off would strand the mule on
		// Karamja with nothing for the second, so a route is only walked once its whole
		// fare is on hand; until then it earns the rest below.
		return HomeWalk
	case v.OnKaramja:
		// The banana fare is earned on the island, never by walking to a mainland spot
		// that is itself across a paid ship.
		return HomeFund
	case !v.SafeRoute && v.Coins >= v.Fare:
		// The fare is on hand yet the planner found no way home. Walking still lets the
		// walker's own ladder (scene walk, unstick, door retries) try; it plans with the
		// same wolf zones, so the worst it can do is fail and come back here.
		return HomeWalk
	case v.FarFromFundSpot:
		return HomeToFundSpot
	}
	return HomeFund
}

// PlainRouteSafe reports whether a corner-compressed route from from stays out of danger on
// every tile it steps through, the start tile excluded (an account may be standing at the
// edge of a zone it is about to walk away from). A route that is not OK is not safe.
func PlainRouteSafe(from tile.Tile, ok bool, corners []tile.Tile, danger func(tile.Tile) bool) bool {
	if !ok {
		return false
	}
	cur := from
	for _, c := range corners {
		if c.Level != cur.Level {
			cur = c
			if danger(cur) {
				return false
			}
			continue
		}
		for guard := 0; (cur.X != c.X || cur.Z != c.Z) && guard < 4096; guard++ {
			cur.X += sign(c.X - cur.X)
			cur.Z += sign(c.Z - cur.Z)
			if danger(cur) {
				return false
			}
		}
	}
	return true
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
