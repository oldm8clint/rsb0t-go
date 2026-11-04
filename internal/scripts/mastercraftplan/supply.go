package mastercraftplan

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/shopplan"
)

// THE SHOP TRIP (a user request: "to get more bolts to make pearl bolts, the main account could grab coins
// from the mule to buy out the various shops stocking bolts"). the mule held 514k oyster pearls and about
// 24k Bolts, so tips pile up with nothing to go on. When this account's bank holds Pearl bolttips
// and neither it nor the mule holds enough Bolts, a shop round asks the mule for coins, buys every
// chosen supply shop's Bolts out under a price cap (shopplan.SupplyShops: Hickton in Catherby, Lowe
// in Varrock, and the Biohazard-gated camp store when chosen), banks them at the mule's post, and
// the pearl round that follows (Chain: own tips and own Bolts are a Resume) tips them.
//
// The same trip buys Steel arrowtips at a shop that stocks them (Hickton) when the arrow round has
// headless arrows waiting for tips (arrows.go).
//
// WHAT RATE-LIMITS IT is the shelves, not a timer: a shop is worth the walk only once its estimated
// shelf (shopplan.EstimateShelf, from what the last visit left and the engine's restock clock) is
// back to ShopMinShelfPercent of its baseline. Hickton refills 200 Bolts in 20 minutes, Lowe 1,500 in
// two and a half hours, ten a minute each. That is the whole supply the shops give: about 1,200
// Bolts an hour with both shops kept bought out, against ~3M tips 514k pearls make. Shops are a
// trickle; see the COLLAB entry for what else could supply Bolts.

// ShopShelf is what this account last saw on one shop's shelf of one item.
type ShopShelf struct {
	Seen time.Time `json:"seen"`
	Left int32     `json:"left"`
}

// ShelfKey is a Ledger.Shelves key: the shop's inv key and the item's obj id.
func ShelfKey(shop shopplan.Shop, it shopplan.Item) string {
	return shop.Key + ":" + strconv.Itoa(int(it.ID))
}

// ShopWants is which supply items a shop trip is for.
type ShopWants struct {
	Bolts, ArrowTips bool
}

// Any reports whether the trip wants anything.
func (w ShopWants) Any() bool { return w.Bolts || w.ArrowTips }

// Has reports whether w covers obj id.
func (w ShopWants) Has(id int32) bool {
	switch id {
	case pearlplan.BoltID:
		return w.Bolts
	case ArrowTipsID:
		return w.ArrowTips
	}
	return false
}

// ShopStop is one shop a trip visits, and what it expects to buy there.
type ShopStop struct {
	Shop shopplan.Shop
	// Items are the wanted items the shelf estimate says are worth buying, with the units the cap
	// allows from the estimated shelf and what they would cost.
	Items []ShopLine
}

// ShopLine is one item of a stop.
type ShopLine struct {
	Item  shopplan.Item
	Shelf int32
	Units int32
	Cost  int32
}

// Coins is what the stop's lines are estimated to cost.
func (s ShopStop) Coins() int32 {
	var n int32
	for _, l := range s.Items {
		n += l.Cost
	}
	return n
}

// Units is the units the stop expects to buy.
func (s ShopStop) Units() int32 {
	var n int32
	for _, l := range s.Items {
		n += l.Units
	}
	return n
}

// PlanShopTrip is the stops of a shop trip at now: every chosen shop (set.ShopNames, in that order)
// with a wanted item whose estimated shelf is at least set.ShopMinShelfPercent of its baseline, each
// line the units set.ShopMaxPrice allows from that shelf. Nothing when nothing is wanted.
func PlanShopTrip(set Settings, want ShopWants, shelves map[string]ShopShelf, now time.Time) []ShopStop {
	if !want.Any() {
		return nil
	}
	var out []ShopStop
	for _, name := range set.ShopNames {
		shop, ok := shopplan.SupplyShopByName(name)
		if !ok {
			continue
		}
		stop := ShopStop{Shop: shop}
		for _, it := range shop.Items {
			if !want.Has(it.ID) {
				continue
			}
			sh, seen := shelves[ShelfKey(shop, it)]
			est := shopplan.EstimateShelf(it, sh.Left, now.Sub(sh.Seen), seen)
			if int64(est)*100 < int64(it.Baseline)*int64(set.ShopMinShelfPercent) || est <= 0 {
				continue
			}
			units, cost := shopplan.UnitsUnderCap(it, shop, est, int32(set.ShopMaxPrice), -1)
			if units <= 0 {
				continue
			}
			stop.Items = append(stop.Items, ShopLine{Item: it, Shelf: est, Units: units, Cost: cost})
		}
		if len(stop.Items) > 0 {
			out = append(out, stop)
		}
	}
	return out
}

// TripCoins is the coins a trip should carry: every stop's estimated cost with a tenth on top (the
// shelf may be a unit or two fuller than estimated, and another player may have bought some, which
// raises the price of the rest), rounded up to a thousand.
func TripCoins(stops []ShopStop) int {
	var n int64
	for _, s := range stops {
		n += int64(s.Coins())
	}
	n += n / 10
	return int((n + 999) / 1000 * 1000)
}

// DescribeTrip is a one-line account of a planned trip, for the log.
func DescribeTrip(stops []ShopStop) string {
	if len(stops) == 0 {
		return "no shop worth the walk"
	}
	parts := make([]string, 0, len(stops))
	for _, s := range stops {
		var items []string
		for _, l := range s.Items {
			items = append(items, strconv.Itoa(int(l.Units))+" "+l.Item.Name+" (~"+strconv.Itoa(int(l.Cost))+" gp)")
		}
		parts = append(parts, s.Shop.Keeper+": "+strings.Join(items, ", "))
	}
	return strings.Join(parts, "; ")
}

// BoltsWanted says whether the pearl round is short of Bolts: this account's bank holds at least
// set.ShopMinWaiting Pearl bolttips while its own Bolts and the mule's together are under
// set.ShopBuyBelow. mule is the mule's published bank (its Bolts count only).
func BoltsWanted(set Settings, own, mule pearlplan.Held) (bool, string) {
	switch {
	case !set.BuyBolts:
		return false, "bolt buying is off (" + KeyBuyBolts + ")"
	case own.Tips < set.ShopMinWaiting:
		return false, "this account's bank holds " + strconv.Itoa(own.Tips) + " Pearl bolttips, under " + strconv.Itoa(set.ShopMinWaiting)
	case own.Bolts+mule.Bolts >= set.ShopBuyBelow:
		return false, "there are " + strconv.Itoa(own.Bolts+mule.Bolts) + " Bolts to tip already"
	}
	return true, strconv.Itoa(own.Tips) + " Pearl bolttips wait and only " + strconv.Itoa(own.Bolts+mule.Bolts) + " Bolts are held"
}

// ShopReady decides a shop trip: wanted items, and a chosen shop whose shelf is worth the walk.
func ShopReady(set Settings, want ShopWants, shelves map[string]ShopShelf, now time.Time) (stops []ShopStop, ok bool, why string) {
	if !want.Any() {
		return nil, false, "nothing to buy"
	}
	stops = PlanShopTrip(set, want, shelves, now)
	if len(stops) == 0 {
		return nil, false, "no chosen shop has restocked to " + strconv.Itoa(set.ShopMinShelfPercent) + "% of its shelf yet"
	}
	return stops, true, DescribeTrip(stops)
}

// NoteShelf records what a visit left on a shelf.
func (l *Ledger) NoteShelf(shop shopplan.Shop, it shopplan.Item, left int32, at time.Time) {
	if l.Shelves == nil {
		l.Shelves = map[string]ShopShelf{}
	}
	l.Shelves[ShelfKey(shop, it)] = ShopShelf{Seen: at, Left: max(0, left)}
}

// ShelfKeys lists the ledger's shelves, sorted, for the log.
func (l *Ledger) ShelfKeys() []string {
	out := make([]string, 0, len(l.Shelves))
	for k := range l.Shelves {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
