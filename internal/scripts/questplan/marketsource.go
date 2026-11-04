package questplan

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Buying a kit line from the Seers' Village player stalls: the pure half of MethodMarketEssence.
// The live half (internal/bot/scarabswarm_questmarket.go) walks there, asks each stall in turn
// through the woodcutters' live-proven stall buyer, and un-notes what arrives at the Seers bank.
//
// PRICES, MEASURED (the control panel's /api/market, 2026-09-28 ~20:30): every stall listing
// Rune essence that shows a price last sold it at 100 gp each (Seers Market 13,567 in stock, last
// bought at 50; Tradevault 430,526). Sil Stuff lists 285,191 with no price; Edge Market 115,908
// with none, and it is not in Seers. That is not the "few gp" a shop charges, which is why the
// ceiling below is 150 each, not 50: a 50 gp cap would decline every stall.

// MethodMarketEssence is the market source executor for Rune essence.
const MethodMarketEssence = "market-essence"

// SeersMarketStand is the Seers' Village bank floor, where the Seers Market and Tradevault stalls
// stand (scarabplan.DefaultMarketPlaces; COLLAB "woodspec": Tradevault is "at the SAME Seers
// bank"). Sil Stuff's post is not recorded anywhere in this repo; the owner names it among the
// Seers stalls, and the buyer closes on any named stall it can see from here.
var SeersMarketStand = tile.Tile{X: 2725, Z: 3493}

// PipEssenceMarkets are the stalls asked for essence, in order (owner, 2026-09-28).
var PipEssenceMarkets = []string{"Seers Market", "Tradevault", "Sil Stuff"}

const (
	// PipEssenceUnitFallback is the unit price offered when a stall's listing shows none: the
	// last-sold price every priced listing showed on 2026-09-28.
	PipEssenceUnitFallback = 100
	// PipEssenceUnitCeiling is the most one essence may cost, whatever a listing or a stall
	// says. It is also what the provisioner budgets the coins at.
	PipEssenceUnitCeiling = 150
	// MarketUnitSlackPct is how far over the listed price a stall's own quote may be taken: the
	// same 25% scarabplan.MarketSlack allows for runes.
	MarketUnitSlackPct = 25
)

// MarketStock is one stall's listing of one item (scarabplan.MarketListing, without the import).
type MarketStock struct {
	Market   string
	Item     string
	Stock    int
	LastSold int
}

// MarketPick is the stall to ask and on what terms.
type MarketPick struct {
	Market string
	// Unit is the price offered first; UnitCap the most a re-offer at the stall's own quote may
	// pay; Qty how many to ask for; MaxTotal the ceiling on the whole lot (UnitCap x Qty).
	Unit, UnitCap, Qty, MaxTotal int
}

// MarketTerms are PickMarket's inputs.
type MarketTerms struct {
	// Order is the stalls to try, first first.
	Order []string
	Item  string
	// Want is how many the kit is short.
	Want int
	// Listings are the price page's rows for every stall (any item); empty when the page could
	// not be read, in which case every stall is tried at the fallback price.
	Listings []MarketStock
	// Skip are stalls not to ask now (tried this pass, or failed recently).
	Skip map[string]bool
	// Coins are what the pack holds for the purchase.
	Coins int
	// Fallback and Ceiling are the unit price with no listed price, and the hard unit ceiling.
	Fallback, Ceiling int
}

// PickMarket chooses the next stall to ask. A stall is passed over when it is skipped, when its
// page lists items but not this one (it has none), when it lists fewer than wanted, or when its
// last-sold price is over the ceiling. The lot is cut to what the coins pay for at UnitCap, so a
// re-offer at the stall's own quote is always affordable. why says what ruled every stall out.
func PickMarket(t MarketTerms) (MarketPick, string) {
	if t.Want <= 0 {
		return MarketPick{}, "nothing is short"
	}
	if t.Ceiling <= 0 || t.Fallback <= 0 || t.Fallback > t.Ceiling {
		return MarketPick{}, fmt.Sprintf("no sane price (fallback %d, ceiling %d)", t.Fallback, t.Ceiling)
	}
	listed := map[string]bool{}
	for _, l := range t.Listings {
		listed[strings.ToLower(l.Market)] = true
	}
	var why []string
	for _, m := range t.Order {
		if t.Skip[m] {
			why = append(why, m+": asked already or resting after a failure")
			continue
		}
		unit := t.Fallback
		if listed[strings.ToLower(m)] {
			row, ok := findStock(t.Listings, m, t.Item)
			switch {
			case !ok:
				why = append(why, m+": its page lists no "+strings.ToLower(t.Item))
				continue
			case row.Stock > 0 && row.Stock < t.Want:
				why = append(why, fmt.Sprintf("%s: lists only %d", m, row.Stock))
				continue
			case row.LastSold > t.Ceiling:
				why = append(why, fmt.Sprintf("%s: last sold at %d each, over the ceiling of %d", m, row.LastSold, t.Ceiling))
				continue
			case row.LastSold > 0:
				unit = row.LastSold
			}
		}
		unitCap := unit + unit*MarketUnitSlackPct/100
		if unitCap > t.Ceiling {
			unitCap = t.Ceiling
		}
		qty := t.Want
		if t.Coins < unitCap*qty {
			qty = t.Coins / unitCap
		}
		if qty < 1 {
			why = append(why, fmt.Sprintf("%s: %d coins do not pay for one at up to %d", m, t.Coins, unitCap))
			continue
		}
		return MarketPick{Market: m, Unit: unit, UnitCap: unitCap, Qty: qty, MaxTotal: unitCap * qty}, ""
	}
	if len(why) == 0 {
		return MarketPick{}, "no stalls to ask"
	}
	return MarketPick{}, strings.Join(why, "; ")
}

func findStock(rows []MarketStock, market, item string) (MarketStock, bool) {
	for _, r := range rows {
		if strings.EqualFold(r.Market, market) && strings.EqualFold(r.Item, item) {
			return r, true
		}
	}
	return MarketStock{}, false
}
