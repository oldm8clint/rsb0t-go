package marketstockplan

// The trade book: what a stall charged, from example.invalid/market/trades (completed trades, newest
// first, 40 a page). This is a small port of internal/controlpanel/marketprice.go's parser and
// medians, which this package cannot import (controlpanel imports bot). Only the SELL side is read
// here -- what a stall charged a player, which is the price a buyer pays -- and the rules that
// file measured are kept: a median, not a mean; whole-number unit prices only, deriving the unit
// from the total when a row states a decimal; coin transfers (a unit price over ten times the
// median, a stall's owner moving coins to their own account) dropped; and an item whose own sell
// trades disagree by more than ten times between the 25th and 75th percentile has no price.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TradesURL is the completed-trades listing; page 1 is the newest.
const TradesURL = "https://example.invalid/market/trades"

// TradesPageURL is page n of the listing.
func TradesPageURL(page int) string {
	if page <= 1 {
		return TradesURL
	}
	return TradesURL + "?page=" + strconv.Itoa(page)
}

var (
	tradeRowRe  = regexp.MustCompile(`(?s)<tr[^>]*data-id="(\d+)"[^>]*>(.*?)</tr>`)
	tradeItemRe = regexp.MustCompile(`<a href="/market/item/([^"]*)" title="([^"]*)"`)
	tradeQtyRe  = regexp.MustCompile(`<span title="([\d,]+)">`)
	tradeCoinRe = regexp.MustCompile(`class="mk-coin" title="([\d,]+) coins"`)
	tradeSideRe = regexp.MustCompile(`mk-badge-(buy|sell)`)
)

// TradeRow is one completed trade.
type TradeRow struct {
	Item  string
	Units int64
	Unit  int64
	// MarketSold is true when the stall SOLD (the "[S]" badge): a player bought from it.
	MarketSold bool
}

func parseNum(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// ParseTradePage reads one page of the trades listing. A row missing a field that makes it
// meaningful is skipped.
func ParseTradePage(body string) []TradeRow {
	var out []TradeRow
	for _, m := range tradeRowRe.FindAllStringSubmatch(body, -1) {
		row := m[2]
		item := tradeItemRe.FindStringSubmatch(row)
		qty := tradeQtyRe.FindStringSubmatch(row)
		if item == nil || qty == nil {
			continue
		}
		units, ok := parseNum(qty[1])
		if !ok || units <= 0 {
			continue
		}
		coins := tradeCoinRe.FindAllStringSubmatch(row, -1)
		if len(coins) == 0 {
			continue
		}
		var unit int64
		if len(coins) >= 2 {
			first, okFirst := parseNum(coins[0][1])
			total, okTotal := parseNum(coins[1][1])
			switch {
			case okFirst && okTotal && first*units == total:
				unit = first
			case okTotal && total > 0:
				unit = total / units
			case okFirst:
				unit = first
			}
		} else if total, ok := parseNum(coins[0][1]); ok {
			unit = total / units
		}
		if unit <= 0 {
			continue
		}
		side := tradeSideRe.FindStringSubmatch(row)
		out = append(out, TradeRow{Item: strings.TrimSpace(item[2]), Units: units, Unit: unit, MarketSold: side != nil && side[1] == "sell"})
	}
	return out
}

// selfTransferMultiple is how far above the median a unit price must sit to be a coin transfer,
// not a sale (controlpanel/marketprice.go measured ten).
const selfTransferMultiple = 10

// maxSpread is how far apart the 25th and 75th percentile of an item's sell prices may sit.
const maxSpread = 10

// BookFromTrades is the sell-side book per item, keyed by lower-cased name.
func BookFromTrades(rows []TradeRow) map[string]Book {
	sells := map[string][]int64{}
	for _, r := range rows {
		if !r.MarketSold || r.Unit <= 0 {
			continue
		}
		k := strings.ToLower(r.Item)
		sells[k] = append(sells[k], r.Unit)
	}
	out := make(map[string]Book, len(sells))
	for k, v := range sells {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		v = withoutTransfers(v)
		lo, hi := v[(len(v)-1)*25/100], v[(len(v)-1)*75/100]
		out[k] = Book{
			Sell:       int(v[(len(v)-1)/2]),
			SellTrades: len(v),
			Coherent:   lo <= 0 || hi <= lo*maxSpread,
		}
	}
	return out
}

// withoutTransfers drops unit prices over selfTransferMultiple times the median from a sorted
// slice (never everything, and never from fewer than three).
func withoutTransfers(sorted []int64) []int64 {
	if len(sorted) < 3 {
		return sorted
	}
	median := sorted[(len(sorted)-1)/2]
	if median <= 0 {
		return sorted
	}
	limit := median * selfTransferMultiple
	cut := len(sorted)
	for cut > 0 && sorted[cut-1] > limit {
		cut--
	}
	if cut == 0 {
		return sorted
	}
	return sorted[:cut]
}
