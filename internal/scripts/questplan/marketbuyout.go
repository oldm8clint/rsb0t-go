package questplan

import (
	"fmt"
	"strings"
)

// PickBuyOut is PickMarket for a BUY-OUT (runecraft P4: the Nature campaign spends the mule's whole coin stack on Rune essence):
// the next stall to ask and the lot, where a stall that lists FEWER than wanted is not passed over but bought out - the lot is cut
// to what it lists - and the next call (the stall just asked is then in Skip or lists less) goes on to the next. PickMarket's
// rule of passing over a short stall is right for a kit that needs all of its line from one seller, and wrong for a purchase that
// takes everything every stall has.
//
// The lot is at most lotMax and at most what the coins pay for at UnitCap; a stall that lists no essence at all (its page lists
// the market but not the item), one whose last-sold price is over the ceiling, one in Skip, and one the coins do not pay for one at
// are passed over, and why says what ruled every stall out. A stall whose page could not be read (not in Listings) is asked at the
// fallback price for the whole lot.
func PickBuyOut(t MarketTerms, lotMax int) (MarketPick, string) {
	if t.Want <= 0 {
		return MarketPick{}, "nothing is short"
	}
	if lotMax <= 0 {
		return MarketPick{}, "no lot size"
	}
	if t.Ceiling <= 0 || t.Fallback <= 0 || t.Fallback > t.Ceiling {
		return MarketPick{}, fmt.Sprintf("no sane price (fallback %d, ceiling %d)", t.Fallback, t.Ceiling)
	}
	listed := listedMarkets(t.Listings)
	var why []string
	for _, m := range t.Order {
		st := stallOf(t, listed, m)
		if st.Why != "" {
			why = append(why, st.Why)
			continue
		}
		unit := st.Unit
		qty := min(t.Want, lotMax)
		if st.Stock > 0 {
			qty = min(qty, st.Stock)
		}
		unitCap := unit + unit*MarketUnitSlackPct/100
		if unitCap > t.Ceiling {
			unitCap = t.Ceiling
		}
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

// listedMarkets is the stalls the price page has a row for (any item): a stall with none could not be read, which is not the same as a
// stall that has nothing.
func listedMarkets(rows []MarketStock) map[string]bool {
	listed := map[string]bool{}
	for _, l := range rows {
		listed[strings.ToLower(l.Market)] = true
	}
	return listed
}

// stall is what the price page says about asking one stall for the item now.
type stall struct {
	// Unit is the price to offer first; Stock what the stall lists (0: not shown, or its page was not read); Found says the page has a
	// row for the item at this stall. Why is set when the stall is ruled out.
	Unit, Stock int
	Found       bool
	Why         string
}

// stallOf reads one stall's row: skipped or resting, a page that lists no such item, a last-sold price over the ceiling; otherwise the
// price to offer and the stock listed.
func stallOf(t MarketTerms, listed map[string]bool, m string) stall {
	if t.Skip[m] {
		return stall{Why: m + ": asked already or resting after a failure"}
	}
	st := stall{Unit: t.Fallback}
	if !listed[strings.ToLower(m)] {
		return st // its page was not read: asked at the fallback price
	}
	row, ok := findStock(t.Listings, m, t.Item)
	switch {
	case !ok:
		return stall{Why: m + ": its page lists no " + strings.ToLower(t.Item)}
	case row.LastSold > t.Ceiling:
		return stall{Why: fmt.Sprintf("%s: last sold at %d each, over the ceiling of %d", m, row.LastSold, t.Ceiling)}
	}
	st.Found, st.Stock = true, row.Stock
	if row.LastSold > 0 {
		st.Unit = row.LastSold
	}
	return st
}

// BuyOutSupply is what the price page says the stalls could sell to a buy-out now, read BEFORE the mule is asked for a coin.
type BuyOutSupply struct {
	// Stalls are the stalls the page shows a row of the item for, at or under the ceiling and not skipped, in the order they would be
	// asked. A stall whose own page was not read is not among them: the buy-out is sized on what the page confirms, never on a guess.
	Stalls []string
	// Listed is the essence they list in all, counted only when every one of them shows its stock: Known says so. A stall whose stock
	// is not shown (a row with no quantity) leaves it unknown, and the buy-out uncapped, not empty.
	Listed int
	Known  bool
}

// BuyOutStalls says whether any stall could sell to a buy-out: at least one stall must be one the page shows a row of the item for, at
// or under the ceiling, and not skipped or resting. why says what ruled every stall out when none can; a price page that could not be
// read at all (no rows) is one of them, because a buy-out spends the mule's whole coin stack and does not go in blind.
func BuyOutStalls(t MarketTerms) (BuyOutSupply, string) {
	if t.Ceiling <= 0 || t.Fallback <= 0 || t.Fallback > t.Ceiling {
		return BuyOutSupply{}, fmt.Sprintf("no sane price (fallback %d, ceiling %d)", t.Fallback, t.Ceiling)
	}
	if len(t.Listings) == 0 {
		return BuyOutSupply{}, "the price page could not be read, so no stall is known to list " + strings.ToLower(t.Item)
	}
	listed := listedMarkets(t.Listings)
	s := BuyOutSupply{Known: true}
	var why []string
	for _, m := range t.Order {
		st := stallOf(t, listed, m)
		switch {
		case st.Why != "":
			why = append(why, st.Why)
		case !st.Found:
			why = append(why, m+": its page lists nothing (empty, or not read)")
		default:
			s.Stalls = append(s.Stalls, m)
			if st.Stock > 0 {
				s.Listed += st.Stock
			} else {
				s.Known = false
			}
		}
	}
	if len(s.Stalls) == 0 {
		if len(why) == 0 {
			why = append(why, "no stalls to ask")
		}
		return BuyOutSupply{}, strings.Join(why, "; ")
	}
	if !s.Known {
		s.Listed = 0
	}
	return s, ""
}
