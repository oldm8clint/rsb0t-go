package essenceplan

// The PICKAXE UPGRADE (runecraft P2, opt-in `buyPickaxe`): the pickaxe is the miner's real limit - a bronze one makes
// 620 essence an hour at the Guild and a rune one 1,290 - and a miner earns the levels for the better ones as it works
// (5 Mining experience an essence: Mining 6 for steel after about 100 essence, 21 for mithril after about 1,000 - an hour
// and a half at bronze - 31 for adamant after about 3,000, 41 for rune after about 8,200). Nurmof's shop in the Dwarven
// Mine under Falador sells every tier (pickaxeshop in
// area_falador/configs/dwarven_mine.inv) but in tiny stock that restocks slowly:
//
//	bronze 6 (a new one every 50 ticks), iron 5 (100), steel 4 (200), mithril 3 (300), adamant 2 (500), rune 1 (700)
//
// so a fleet of miners can take about one rune pickaxe every seven minutes between them, and a trip that finds the
// shelf empty falls to the next tier down. The list prices are PickaxeShopCost's (1 / 140 / 500 / 1,300 / 3,200 /
// 32,000 coins). At the essence's 100 coins in the stalls a rune pickaxe pays for itself in about half an hour of the
// extra 670 essence an hour.

import (
	"fmt"
	"strings"
	"time"
)

// Upgrade numbers.
const (
	// UpgradeCoinMargin is the percentage of a pickaxe's list price the trip carries on top of it (the shop's price rises
	// as its shelf empties): 25.
	UpgradeCoinMargin = 25
	// NurmofFareCoins is what the trip carries for its fares on top of the pickaxe's coins. From the Guild's bank at Yanille the
	// walk to the Dwarven Mine crosses by ship (White Wolf Mountain is avoided below combat 70), and a walk planned with the
	// pickaxe's 1 coin in the pack has no fare to pay: on 20 Oct 2026 every miner's bronze pickaxe trip ended at once with
	// "the walk to the Dwarven Mine trapdoor did not arrive", the walker answering "no path: unreachable".
	NurmofFareCoins = 1_000
	// UpgradeRetry is how long a failed trip (the shelf was empty, the shop would not open) waits before the next.
	UpgradeRetry = 30 * time.Minute
)

// UpgradeView is what DecideUpgrade reads.
type UpgradeView struct {
	Now time.Time
	// Selection is the pickaxe setting (BestAvailable or a tier name) and MiningLevel the account's Mining level.
	Selection   string
	MiningLevel int
	// Owned is every pickaxe item name held, worn or banked.
	Owned []string
	// Coins is the account's coins, pack and bank.
	Coins int
	// NextTry is the earliest a trip is made after a failed one.
	NextTry time.Time
}

// UpgradeOffer is one pickaxe worth fetching: an upgrade on what is owned that the level allows and the coins pay for.
type UpgradeOffer struct {
	Item string
	Tier string
	// Cost is the list price and Carry the coins the trip takes (Cost plus the margin).
	Cost, Carry int
	// Rate is the essence an hour the tier makes.
	Rate float64
}

// UpgradeDecision is DecideUpgrade's answer.
type UpgradeDecision struct {
	// Buy says make the trip; Offers are the pickaxes to try at the shop, best first (a shelf that is out falls to the
	// next).
	Buy    bool
	Offers []UpgradeOffer
	// Target is the best tier the Mining level allows; OwnedRate the essence an hour the best owned pickaxe makes
	// (0 for none).
	Target    PickTier
	OwnedRate float64
	// Wait says an upgrade is affordable but a failed trip's wait has not run out: look again, do not give up on it.
	Wait bool
	Why  string
}

// ownedPeriod is the ticks between essence of the best pickaxe in owned the level can use (0 for none).
func ownedPeriod(owned []string, level int) int {
	best := 0
	for _, o := range owned {
		for _, t := range PickTiers {
			if strings.EqualFold(t.Item, strings.TrimSpace(o)) && level >= t.Level {
				if p, ok := PickPeriod(t.Item); ok && (best == 0 || p < best) {
					best = p
				}
			}
		}
	}
	return best
}

// DecideUpgrade is the trip to Nurmof the miner should make, if any: the tiers above the best pickaxe it owns that its
// Mining level allows and its coins pay for, best first. A named tier (the pickaxe setting) is the only one wanted.
func DecideUpgrade(v UpgradeView) UpgradeDecision {
	var d UpgradeDecision
	target, ok := DesiredPickaxe(v.Selection, v.MiningLevel)
	if !ok {
		d.Why = fmt.Sprintf("no pickaxe tier the selection %q allows at Mining %d", v.Selection, v.MiningLevel)
		return d
	}
	d.Target = target
	have := ownedPeriod(v.Owned, v.MiningLevel)
	if have > 0 {
		for _, t := range PickTiers {
			if p, _ := PickPeriod(t.Item); p == have {
				d.OwnedRate = EssencePerHour(t.Item)
			}
		}
	}
	if tp, _ := PickPeriod(target.Item); have > 0 && have <= tp {
		d.Why = fmt.Sprintf("the %s the level allows is already owned", strings.ToLower(target.Item))
		return d
	}
	for _, t := range PickTiers { // best first
		if v.MiningLevel < t.Level {
			continue
		}
		if !strings.EqualFold(v.Selection, BestAvailable) && !strings.EqualFold(v.Selection, t.Tier) {
			continue
		}
		p, _ := PickPeriod(t.Item)
		if have > 0 && p >= have {
			continue
		}
		cost, known := PickaxeCost(t.Item)
		if !known {
			continue
		}
		carry := cost + cost*UpgradeCoinMargin/100
		if v.Coins < carry {
			continue
		}
		d.Offers = append(d.Offers, UpgradeOffer{Item: t.Item, Tier: t.Tier, Cost: cost, Carry: carry, Rate: EssencePerHour(t.Item)})
	}
	if len(d.Offers) == 0 {
		cost, _ := PickaxeCost(target.Item)
		d.Why = fmt.Sprintf("a %s costs %d and the account holds %d coins: no affordable upgrade", strings.ToLower(target.Item), cost, v.Coins)
		return d
	}
	if v.Now.Before(v.NextTry) {
		d.Why = fmt.Sprintf("a %s is affordable; the next trip to Nurmof is not before %s", strings.ToLower(d.Offers[0].Item), v.NextTry.Format("15:04"))
		d.Offers, d.Wait = nil, true
		return d
	}
	d.Buy = true
	d.Why = fmt.Sprintf("a %s (%d coins) would make %.0f essence/h against %.0f", strings.ToLower(d.Offers[0].Item), d.Offers[0].Cost, d.Offers[0].Rate, d.OwnedRate)
	return d
}
