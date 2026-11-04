package runerunnerplan

// What a shop PAYS for an item: the model that answers the owner's Karamja question, and nothing more.
//
// a user request: "i believe we can sell items with a good high alchemy price to the karamja store and
// get close to alch price just by selling them so we can fund the shop runs while we are short on coins".
//
// The answer is in the content's own rule (scripts/shop/scripts/shop.rs2, read, not recalled):
//
//	sell_item            loops i = 0..n-1 and pays adjusted_item_cost_selling(oc_uncert(obj), shop_total + i),
//	                     where shop_total is the shop's count of the item BEFORE the sale
//	adjusted_item_cost_selling = calc_shop_value(oc_cost, shop_delta, shop_buy_multiplier, price_mod)
//	price_mod            stock minus inv_stockbase(shop, obj), or the stock alone when the shop does not list
//	                     the item (stockbase -1: every item a general store takes that it does not stock)
//	calc_shop_value      int5 = min(1000, max(-5000, price_mod * delta)); int5 = max(100, buyMult - int5);
//	                     value = int5 * cost / 1000 (truncated)
//
// So the i-th unit pays cost * (buyMult - delta * i) / 1000 at a general store -- never under a tenth of the
// cost -- and a general store (buyMult 400) pays 40% of cost for the FIRST unit, which is 66.7% of High Level
// Alchemy (60% of cost), then delta/1000 of the cost less for every unit it already holds. The Karamja General
// Store is generalshop7, priced like Varrock's and Catherby's Arhein's (400 / 30): it has no advantage and
// costs a ship each way. Only a shelf below its stock base pays more (the gem stall, 800 / 30, pays 133% of
// alch for the first Diamond), and a listed line recovers one unit per its rate while an unlisted item loses
// one unit a minute (World.ts), so a visit 40 minutes later starts again from empty.
//
// A Dragonstone (cost 10,000, alch 6,000) at the general stores: 4,000, 3,700, 3,400, 3,100, 2,800, 2,500 ...
// down to the 1,000 floor; six of them at a 40%-of-alch stop (2,400 a unit) pay 19,500, an average of 3,250
// against the 5,980 a Nature rune cast on one returns (alchemy.rs2: profit = max(scale(6, 10, cost), 1)) with
// 65 Magic xp. That is why scarabplan.DefaultAlchItems gained the Dragonstone and the Runite bar and RuneRunner
// has no sell station: selling is the fallback for what the Natures cannot absorb, and the mule's bank
// (5.0 million coins on 13 Oct against 2.97 million at 18:05) did not call for it. This file is the price rule,
// pure and tested against those figures, ready for a station if the coins ever do.

import "fmt"

// SalePrice is what a shop pays for ONE unit of an item with `stockBefore` of it on the shelf already: cost is
// the obj's oc_cost, buyMult and delta the keeper's shop_buy_multiplier and shop_delta, base the line's
// stock base in the shop (-1 when the shop does not list the item).
func SalePrice(cost, buyMult, delta, base, stockBefore int) int {
	diff := stockBefore
	if base != -1 {
		diff = stockBefore - base
	}
	int5 := diff * delta
	if int5 > 1000 {
		int5 = 1000
	}
	if int5 < -5000 {
		int5 = -5000
	}
	int5 = buyMult - int5
	if int5 < 100 {
		int5 = 100
	}
	return int5 * cost / 1000
}

// SaleTotal is what n units pay in all, each priced at the shelf count before it.
func SaleTotal(cost, buyMult, delta, base, stockBefore, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += SalePrice(cost, buyMult, delta, base, stockBefore+i)
	}
	return total
}

// AlchValue is what one High Level Alchemy cast pays on an item of this cost: 60% of it, at least one coin.
func AlchValue(cost int) int {
	if v := cost * 6 / 10; v > 1 {
		return v
	}
	return 1
}

// PlanSaleMaxUnits bounds a plan that was given no bound of its own: a stop under a general store's floor price
// (a tenth of the cost) never trips, and the caller's pack is the real limit.
const PlanSaleMaxUnits = 100000

// PlanSale is how many units to sell and what they pay: units while the next one still pays at least
// minPercent percent of the item's alchemy value, up to maxUnits (0 = PlanSaleMaxUnits).
func PlanSale(cost, buyMult, delta, base, stockBefore, minPercent, maxUnits int) (n, total int) {
	if maxUnits <= 0 || maxUnits > PlanSaleMaxUnits {
		maxUnits = PlanSaleMaxUnits
	}
	floor := AlchValue(cost) * minPercent / 100
	for n < maxUnits {
		p := SalePrice(cost, buyMult, delta, base, stockBefore+n)
		if p < floor || p <= 0 {
			break
		}
		total += p
		n++
	}
	return n, total
}

// SellOutlet is a shop that buys items from a player, with the keeper's selling-side params.
type SellOutlet struct {
	Key, Label string
	// BuyMult and Delta are shop_buy_multiplier and shop_delta; Base is the line's stock base for the item
	// the table is read for (-1 in a general store, which lists nothing it takes).
	BuyMult, Delta int
	// AllStock is true for a general store: it takes any tradable item, noted or not.
	AllStock bool
	// Where is the keeper's place, from the content maps, and how far off the circuit it lies.
	Where string
}

// SellOutlets are the outlets the owner's question and the design walked, content 289 values.
var SellOutlets = []SellOutlet{
	{"VarrockGeneral", "Varrock General Store (generalshop2)", 400, 30, true, "3218,3415, between the Varrock Teleport landing and Aubury"},
	{"Arhein", "Arhein's Store, Catherby (arheinstore)", 400, 30, true, "2803,3430, 11 tiles from the mule's post"},
	{"KaramjaGeneral", "Karamja General Store (generalshop7)", 400, 30, true, "2902,3146, Musa Point: a Port Sarim ship each way"},
	{"Aemad", "Aemad's Adventuring Supplies, East Ardougne (adventurershop)", 400, 20, true, "2613,3294, 42 tiles from the Ardougne bank"},
	{"Khazard", "Port Khazard General Store (khazardshop)", 400, 10, true, "2655,3152, the deepest general store"},
	{"Jiminua", "Jiminua's Jungle Store, Tai Bwo Wannai (junglestore)", 500, 20, true, "2767,3122: a ship and 190 tiles inland"},
	{"GemStall", "Ardougne gem stall (ardougnegemstall)", 800, 30, false, "2669,3303: Sapphire, Emerald, Ruby and Diamond only"},
}

// SellOutletFor finds an outlet by key.
func SellOutletFor(key string) (SellOutlet, bool) {
	for _, o := range SellOutlets {
		if o.Key == key {
			return o, true
		}
	}
	return SellOutlet{}, false
}

// SaleLine is the outlet's answer for an item as a log line: "VarrockGeneral Dragonstone: 6 for 19500 ...".
func (o SellOutlet) SaleLine(item string, cost, base, stock, minPercent int) string {
	n, total := PlanSale(cost, o.BuyMult, o.Delta, base, stock, minPercent, 0)
	return fmt.Sprintf("%s %s: %d for %d (first %d of alch %d)", o.Key, item, n, total, SalePrice(cost, o.BuyMult, o.Delta, base, stock), AlchValue(cost))
}
