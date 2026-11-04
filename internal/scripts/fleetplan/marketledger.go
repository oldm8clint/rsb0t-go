package fleetplan

import (
	"time"
)

// DefaultMarketBudget is the owner's fleet-wide ceiling on coins spent on market runes.
const DefaultMarketBudget = 5_000_000

// MarketHoldTTL is how long a reservation stands without a commit: a walk to the stall, the
// stall's own fetch from its bank and the trade fit well inside it, and a crashed purchase frees
// its hold after it.
const MarketHoldTTL = 10 * time.Minute

// MarketPurchase is one market purchase as the ledger records it.
type MarketPurchase struct {
	Account string    `json:"account"`
	Market  string    `json:"market"`
	Item    string    `json:"item"`
	Qty     int       `json:"qty"`
	Paid    int       `json:"paid"`
	At      time.Time `json:"at"`
	// SpentAfter is the fleet's running total once this purchase was counted.
	SpentAfter int `json:"spentAfter"`
}

// MarketLedgerState is what the bot persists.
type MarketLedgerState struct {
	Spent  int              `json:"spent"`
	Buys   int              `json:"buys"`
	Recent []MarketPurchase `json:"recent,omitempty"`
}

// MarketOnWorld reports whether a player market can be traded with from world: markets lists the
// worlds the market stalls stand on, and an empty list means "any world" (the old, world-blind
// rule). A world of 0 or less is unknown and allowed, since a one-world deployment reports none.
//
// WHY (the owner's world-2 ruling, 2026-09-23: "the Scarab cohort runs on world 2 and hops to
// world 1 to trade with the mule"). The Seers Market is a real player on world 1. From world 2 an
// account walks to Seers, calls out an order nobody on its world can hear, waits out the reply
// window, and -- because the woodcutters' stall turn lock is process-wide and keyed by the stall's
// name alone -- holds world 1's woodcutters out of the stall while it does.
func MarketOnWorld(world int, markets []int) bool {
	if world <= 0 || len(markets) == 0 {
		return true
	}
	for _, w := range markets {
		if w == world {
			return true
		}
	}
	return false
}

// DefaultMarketWorlds is where the player markets this script buys from stand: the Seers Market
// and the other stalls on the fleet's price page were heard on world 1, the only world the fleet
// ran on when they were (2026-09-21).
func DefaultMarketWorlds() []int { return []int{1} }
