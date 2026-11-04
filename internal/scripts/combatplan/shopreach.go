package combatplan

import (
	"strings"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Gear shops across the water.
//
// Every shop in shopSites stands in Misthalin or Asgarnia, joined to Kandarin only by White Wolf
// Mountain, which no walk crosses (webwalk's white-wolf-dens zone). An account standing in Ardougne
// therefore reaches any of them only by the Port Sarim - Karamja - Brimhaven - Ardougne ships, and
// the walker plans those only while the pack holds their fares. Live 7 Oct 2026: acct89, at
// Ardougne with a few coins, chose Cassie's shield in Falador and Peksa's helm in Barbarian Village;
// each took three "could not reach the keeper" trips, two minutes apart, and bought nothing.

// SkippedShop is a shop a plan leaves out because the account cannot pay the ships to it.
type SkippedShop struct {
	Shop, Keeper string
	// Fares is the coins the round trip across the water costs; Coins is what the pack held.
	Fares, Coins int
}

// CrossingFares is the coins for the ships to the keeper and back from where the account stands:
// 0 when the shop is on the account's own landmass.
func CrossingFares(here *tile.Tile, site ShopSite) int {
	stand := site.Stand
	return 2 * freshstart.FareToCross(here, &stand)
}

// PlanReachable is BuyPlanExcept limited to shops the account can get to with the coins it holds:
// a slot whose best item is sold across the water, with fewer coins than the round trip's fares,
// falls back to the best item a shop on this side (or an affordable crossing) sells, or drops out
// of the plan. The shops left out are returned so the caller can say so once.
func PlanReachable(l Levels, questsDone map[string]bool, worn, held []string, unavailable map[string]bool, here *tile.Tile, coins int) ([]Purchase, []SkippedShop) {
	excluded := map[string]bool{}
	for k, v := range unavailable {
		excluded[k] = v
	}
	var skipped []SkippedShop
	seen := map[string]bool{}
	for {
		plan := BuyPlanExcept(l, questsDone, worn, held, excluded)
		clean := true
		for _, p := range plan {
			fares := CrossingFares(here, p.Site)
			if fares == 0 || coins >= fares {
				continue
			}
			clean = false
			if !seen[p.Site.Shop] {
				seen[p.Site.Shop] = true
				skipped = append(skipped, SkippedShop{Shop: p.Site.Shop, Keeper: p.Site.Keeper, Fares: fares, Coins: coins})
			}
			for _, it := range Items() {
				if it.Shop == p.Site.Shop {
					excluded[strings.ToLower(it.Name)] = true
				}
			}
		}
		if clean {
			return plan, skipped
		}
	}
}
