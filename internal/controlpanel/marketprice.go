package controlpanel

// What an item is actually worth, from the market's own completed trades.
//
// The Loot panel used to value everything at the cache's unit cost (obj "cost"), which is the
// shop price the content ships with and has little to do with what a thing trades for on this
// server. Users asked for real market values, falling back to high alchemy where an item
// has never traded.
//
// The source is https://example.invalid/market/trades — completed coins-for-items trades, newest
// first, paginated. It is the same site and the same row shape as market.go's inventory pages,
// so the parsing follows that file's conventions deliberately.
//
// Three things this is careful about, because a wrong price is worse than no price:
//
//   - A missing price is UNKNOWN, never zero. Same rule market.go already states for
//     LastSold/LastBought.
//   - The figure is the MEDIAN unit price, not a mean. A mean cannot be defended here, volume
//     weighted or not: weighting by units still puts the coins in the numerator, so one
//     person moving a single item at a silly price drags the answer with it. Measured on the
//     obvious case — 10,000 units at 100 coins alongside 1 unit at 1,000,000 — the
//     volume-weighted mean is 199, nearly double the real price, while the median is 100.
//     Trades and Units are reported alongside so a consumer can see how thin the sample is.
//   - Both sides count. A completed trade is a completed trade whether the market was the
//     buyer or the seller; taking only one side would bias the figure in whichever direction
//     the market happens to lean.

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tradesBase is the completed-trades listing. Page 1 is the newest.
const tradesBase = "https://example.invalid/market/trades"

// DefaultTradePages bounds how many pages back to read. The site paginates at 40 rows a page and
// the read stops at the first empty page, so this is a ceiling, not a count: the whole history is
// read (85 pages, about 3,400 trades, on 7 Oct 2026, up from 32 when this was first written),
// which is what makes the per-item medians worth anything. Reading fewer would bias toward
// whatever happened to trade in the last hour.
const DefaultTradePages = 250

// tradesFetchLimit bounds one page's body, as market.go bounds an inventory page.
const tradesFetchLimit = 4 << 20

// minConfidentTrades is how many completed trades an item needs before its median is trusted
// over the weaker sources below it.
//
// A median of one sample IS that sample, which is how "Uncut emerald" came to be priced at
// 335,000 coins — one trade, 152 units, no other evidence anywhere, against a cache cost of
// 50 and a ruby (a strictly better gem) trading at 2,800. Measured on the live table the day
// this was added: ruby 5 trades, sapphire 6, leather 4, raw lobster 22, all agreeing with the
// markets' own listings where a listing existed. Emerald was the only one-sample item among
// them, and the only one that was obviously wrong.
//
// Two is deliberately low. It is not a claim that two trades make a reliable price; it is the
// smallest bar that excludes "a single number nobody corroborated", which is the failure that
// actually happened. Raising it would discard genuinely agreeing pairs.
const minConfidentTrades = 2

// maxCoherentSpread is how far an item's own trades may disagree with each other before the
// median stops being a price at all.
//
// minConfidentTrades above rejects a price nobody corroborated. This rejects the other shape:
// a handful of trades that DO corroborate each other into two different answers. The median
// picks one of them by counting trades, and a trade is a trade whether it moved 1 unit or
// 1,000 — so three matching trades of 100 units outvote one of 1,000, and the fleet's 91,000
// cakes get valued at the price of the smaller side.
//
// That is the live case, spotted by the fleet owner on 2026-09-21: Cake priced at 11,092 each
// against 91,000 held, roughly a billion coins of fictional bank value. Its seven trades are
// 1,000 units at 53 and three of 100 units at 11,092-13,484, all three sold by one market
// inside two minutes fifteen days ago. p25 376, p75 13,484 -- a 36x spread on ONE item.
//
// Ten is measured, not guessed. Over the live table's 1,280 trades in 204 items, exactly TWO
// exceed it: Cake at 35.9x and Black sq shield at 21.4x (median 9,315 against a p25 of 435
// across 1,455 units -- the same shape). The worst legitimate spread is Rune scimitar at 5.5x,
// then Diamond at 4.2x, so nothing real sits anywhere near the line.
//
// A unit-weighted median was the obvious alternative and it is NOT better: measured on the
// same table it fixes Cake (11,290 -> 53) but breaks Snape grass (105 -> 11,111), Rune
// chainbody (18,900 -> 455,555) and Rune platelegs (67,200 -> 350,000), because weighting by
// units simply hands the vote to whoever moved the biggest stack. Refusing to answer is the
// honest response to a sample that contradicts itself, and the ladder below has real
// alternatives: a market's own listing first, then high alchemy.
const maxCoherentSpread = 10

// Coherent reports whether an item's own trades agree with each other closely enough for the
// median to mean anything. See maxCoherentSpread.
//
// A sample too small to have a spread is not incoherent -- minConfidentTrades is what guards
// that -- so a missing Low or High answers true and lets the rest of the ladder decide.
func (m MarketPrice) Coherent() bool {
	if m.Low <= 0 || m.High <= 0 {
		return true
	}
	return m.High <= m.Low*maxCoherentSpread
}

// headRefreshTTL is how often the NEWEST page of trades is re-read.
//
// Users asked for prices that "frequently update with the site to accurately represent
// new medians and price ranges". A full rebuild is 32 page fetches and cannot politely be run
// every couple of minutes, but page 1 alone carries the newest 40 trades — which is where any
// change to a price actually appears first. So the table is kept fresh by re-reading that one
// page often and merging in only the trades whose ids are new, and rebuilt from all 32 pages
// on the slower cadence below to let old trades age out of the window.
//
// One page every two minutes is ~30 requests an hour against someone else's site, against 32
// requests per full rebuild. That is a deliberate trade: far fresher prices for roughly the
// same total traffic.
const headRefreshTTL = 2 * time.Minute

// tradePricesTTL is how long a computed price table is reused. Deliberately long: these are
// averages over ~1,300 historical trades, so they barely move minute to minute, and 32 page
// fetches is not something to repeat because a panel refreshed.
const tradePricesTTL = 30 * time.Minute

// A trade row, e.g.
//
//	<tr data-id="49719" ...><td class="mk-col-item"><a href="/market/item/oak_logs" title="Oak logs">
//	  ...<td class="mk-col-price">...<span class="mk-badge-buy" ...>[B]</span>
//	  <span title="300">300</span> ... <span class="mk-coin" title="15 coins">...
//
// data-id on THIS page is the trade's id, not the item's — unlike the inventory pages, where
// it is the item id. So trades are keyed by name and slug, and an item id has to come from
// somewhere else (the inventory pages carry both).
var (
	tradeRowPattern  = regexp.MustCompile(`(?s)<tr[^>]*data-id="(\d+)"[^>]*>(.*?)</tr>`)
	tradeItemPattern = regexp.MustCompile(`<a href="/market/item/([^"]*)" title="([^"]*)"`)
	tradeQtyPattern  = regexp.MustCompile(`<span title="([\d,]+)">`)
	// Whole numbers only, DELIBERATELY. A bulk row states its unit price with decimals
	// ("672.22 coins" for 3,124 Big bones) while the total stays whole, so a decimal unit
	// price does not match here and the row falls through to the total-derivation path in
	// parseTradePage — which divides the total by the quantity and gets the same answer.
	//
	// Measured over 1,278 live rows: 163 (13%) carry a decimal unit price, NONE of them lack
	// a whole-number total, and the derived figures match the stated ones (Logs 150 against
	// 150.94, Yew shortbow 105 against 105.19).
	//
	// Do not "fix" this to accept decimals without reading that path first. Before the
	// total-derivation existed, a decimal row's FIRST match was the total, recorded as the
	// price of a single item — that is how Big bones came to be priced at 2,100,000 each.
	tradeCoinPattern = regexp.MustCompile(`class="mk-coin" title="([\d,]+) coins"`)
	// Which side the MARKET was on. "[B]" is the market BUYING -- a player sold to it, so
	// that is what the fleet could actually realise for the same item. "[S]" is the market
	// SELLING, which is its asking price and always the higher of the two.
	tradeSidePattern = regexp.MustCompile(`mk-badge-(buy|sell)`)
)

// MarketPrice is one item's average traded price.
type MarketPrice struct {
	// Name is the item's display name, exactly as the market spells it.
	Name string `json:"name"`
	// Slug is the site's own item-page slug, for a link out.
	Slug string `json:"slug,omitempty"`
	// Coins is the median price per unit, in coins. Median rather than mean on purpose — see
	// this file's own header for the measurement that settled it.
	Coins int64 `json:"coins"`
	// Trades and Units say how thin the sample is. One trade of three items is not evidence
	// of a market price, and a consumer that cares should look before trusting Coins.
	Trades int   `json:"trades"`
	Units  int64 `json:"units"`
	// Low and High are the 25th and 75th percentile unit price — where prices actually sit,
	// not the extremes.
	//
	// The true min and max are worse than useless here, because real single-unit anomalies
	// exist and define the range on their own: one raw lobster genuinely changed hands for
	// 5,100,800 coins and one rune axe for 7,982,733, against medians of 200 and 150,000.
	// Reporting 125-5,100,800 as a lobster's "price range" tells a reader nothing. A median
	// deserves a spread measured the same robust way it is.
	Low  int64 `json:"low"`
	High int64 `json:"high"`
	// Buy and Sell are the two sides of the market, kept apart because they are two different
	// questions and lumping them together answers neither.
	//
	// Buy is the median price the market PAID a player -- what the fleet could realise for
	// the same item. Sell is the median it CHARGED, which is its asking price. The gap is the
	// market's margin and it is not small: measured on the live table a Rune axe buys at
	// 50,000 and sells at 150,000, a Raw shark 900 against 1,200, a Law rune 250 against 300.
	// Buy is below Sell on essentially every item that has both.
	//
	// Zero means that side has no trades at all. 117 of 204 items are sell-only and 7 are
	// buy-only, so both cases are ordinary rather than exceptional.
	Buy  int64 `json:"buy,omitempty"`
	Sell int64 `json:"sell,omitempty"`
	// BuyTrades and SellTrades are how many completed trades each side rests on.
	BuyTrades  int `json:"buyTrades,omitempty"`
	SellTrades int `json:"sellTrades,omitempty"`
	// LastTradeID is the newest trade this price was computed from, so a caller can tell
	// whether a refresh actually saw anything new.
	LastTradeID int64 `json:"lastTradeId,omitempty"`
}

// tradeRow is one parsed row before averaging.
type tradeRow struct {
	// id is the site's own trade id (the row's data-id). It is what lets a cheap re-read of
	// page 1 merge only the trades that are genuinely new, instead of double-counting every
	// trade still sitting on that page.
	id         int64
	name, slug string
	units      int64
	unitCoins  int64
	// marketBought is true when the market was the BUYER, i.e. a player sold to it.
	marketBought bool
}

func tradesURL(page int) string {
	if page <= 1 {
		return tradesBase
	}
	return fmt.Sprintf("%s?page=%d", tradesBase, page)
}

// parseCoins reads "4,500" as 4500. The site writes every figure this way in the title
// attribute, which is why the titles are read rather than the visible text — the visible text
// is abbreviated ("4.5K") and would lose precision.
func parseCoins(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseTradePage pulls every usable row out of one page's HTML. A row missing any of the
// fields that make it meaningful is skipped rather than guessed at.
func parseTradePage(body string) []tradeRow {
	var out []tradeRow
	for _, m := range tradeRowPattern.FindAllStringSubmatch(body, -1) {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		row := m[2]
		item := tradeItemPattern.FindStringSubmatch(row)
		if item == nil {
			continue
		}
		qty := tradeQtyPattern.FindStringSubmatch(row)
		if qty == nil {
			continue
		}
		units, ok := parseCoins(qty[1])
		if !ok || units <= 0 {
			continue
		}
		// Coin figures on a row, and which one is the UNIT price.
		//
		// The usual row shows two: "<qty> for <unit> ea. = <total>", so the first is the unit
		// price and taking it directly beats re-deriving it from a rounded total. But a row
		// can carry only ONE — the total, with no "ea." breakdown at all. Reading that as a
		// unit price is how a raw lobster came to be priced at 2,315,461 coins: that was the
		// total for 5,010 of them, or 462 each (found 2026-09-20 by checking the reported
		// price RANGE, which is exactly what a range is for).
		//
		// So: two or more figures means the first is the unit price; exactly one means it is
		// the total and the unit price is derived. The two-figure case is checked rather than
		// trusted — if unit x units does not come back to the stated total, the total is the
		// figure to believe, because it is what actually changed hands.
		coins := tradeCoinPattern.FindAllStringSubmatch(row, -1)
		if len(coins) == 0 {
			continue
		}
		var unit int64
		if len(coins) >= 2 {
			first, okFirst := parseCoins(coins[0][1])
			total, okTotal := parseCoins(coins[1][1])
			switch {
			case okFirst && okTotal && first*units == total:
				unit = first
			case okTotal && total > 0:
				unit = total / units
			case okFirst:
				unit = first
			}
		} else if total, ok := parseCoins(coins[0][1]); ok {
			unit = total / units
		}
		if unit <= 0 {
			continue
		}
		side := tradeSidePattern.FindStringSubmatch(row)
		out = append(out, tradeRow{
			id: id, name: item[2], slug: item[1], units: units, unitCoins: unit,
			marketBought: side != nil && side[1] == "buy",
		})
	}
	return out
}

// averageTrades folds rows into one price per item, keyed by lower-cased name so a caller
// holding a game item name can look it up without worrying about the market's capitalisation.
func averageTrades(rows []tradeRow) map[string]MarketPrice {
	type acc struct {
		name, slug string
		unit       []int64
		buy, sell  []int64
		units      int64
		trades     int
		newest     int64
	}
	by := map[string]*acc{}
	for _, r := range rows {
		k := strings.ToLower(strings.TrimSpace(r.name))
		if k == "" || r.units <= 0 {
			continue
		}
		a := by[k]
		if a == nil {
			a = &acc{name: r.name, slug: r.slug}
			by[k] = a
		}
		a.unit = append(a.unit, r.unitCoins)
		if r.marketBought {
			a.buy = append(a.buy, r.unitCoins)
		} else {
			a.sell = append(a.sell, r.unitCoins)
		}
		a.units += r.units
		a.trades++
		if r.id > a.newest {
			a.newest = r.id
		}
	}
	out := make(map[string]MarketPrice, len(by))
	for k, a := range by {
		if len(a.unit) == 0 {
			continue
		}
		sort.Slice(a.unit, func(i, j int) bool { return a.unit[i] < a.unit[j] })
		a.unit = withoutSelfTransfers(a.unit)
		buy, sell := sideMedian(a.buy), sideMedian(a.sell)
		coins := buy
		if coins <= 0 {
			// The owner's rule (2026-09-21): "median for buy and sell, if no buy and has
			// sell, just sell price". Buy first because it is what the fleet could actually
			// get for the thing; sell is the fallback, and it OVERSTATES, which is worth
			// knowing rather than hiding -- Buy and Sell are both reported so a reader can
			// see which one answered.
			coins = sell
		}
		if coins <= 0 {
			coins = a.unit[(len(a.unit)-1)/2]
		}
		out[k] = MarketPrice{
			Name: a.name, Slug: a.slug,
			Coins:      coins,
			Buy:        buy,
			Sell:       sell,
			BuyTrades:  len(a.buy),
			SellTrades: len(a.sell),
			Trades:     a.trades, Units: a.units,
			Low:         percentileOf(a.unit, 25),
			High:        percentileOf(a.unit, 75),
			LastTradeID: a.newest,
		}
	}
	return out
}

// sideMedian is one side's median unit price, with the coin transfers stripped out the same
// way the combined figure strips them. Zero when that side has no trades.
func sideMedian(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	sorted := append([]int64(nil), v...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	sorted = withoutSelfTransfers(sorted)
	return sorted[(len(sorted)-1)/2]
}

// medianOf is the middle unit price; with an even count it is the lower of the two middles,
// which keeps the answer an actual observed price rather than an average of two.
func medianOf(v []int64) int64 {
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[(len(v)-1)/2]
}

// selfTransferMultiple is how far above the median a unit price has to sit before it is
// treated as not a sale at all.
//
// The fleet owner identified what these actually are (2026-09-20): a market's owner moving
// coins to their own market account by "selling" it one item for an enormous sum. They are
// not prices and must not be counted as any part of one. The signature is unmistakable —
// one raw lobster for 5,100,800 against a median of 200 is 25,504x, and one rune axe for
// 7,982,733 against 150,000 is 53x.
//
// Ten is chosen to sit far above real variation and far below these. Measured on the live
// table: an uncut ruby's genuine spread is 2,200-3,225 around a median of 2,800 (1.15x), yew
// logs 180-585 around 315, flax 50-100 around 95. Nothing legitimate came close to 10x.
//
// Filtering is one-sided on purpose. A transfer is always far ABOVE the median; a cheap sale
// is an ordinary thing and stays.
const selfTransferMultiple = 10

// withoutSelfTransfers drops the coin transfers described above from an ALREADY SORTED slice
// of unit prices. The median is taken first and is itself robust to them, which is what makes
// it safe to use as the reference for deciding what to discard.
func withoutSelfTransfers(sorted []int64) []int64 {
	if len(sorted) < 3 {
		// Too few to tell an anomaly from the market. Leave them alone and let
		// minConfidentTrades decide whether this price is trusted at all.
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
	// Never discard everything: if a run of them somehow dominates, keep what is there rather
	// than reporting no price at all.
	if cut == 0 {
		return sorted
	}
	return sorted[:cut]
}

// percentileOf picks the nearest observed value at the given percentile of an ALREADY SORTED
// slice. Always a price somebody actually paid, never an interpolation between two.
func percentileOf(sorted []int64, pct int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted) - 1) * pct / 100
	return sorted[i]
}

// TradePrices reads pages of completed trades and returns the per-item averages. A page that
// fails is skipped rather than failing the lot: a price table built from 30 of 32 pages is
// still far better than the cache cost it replaces. An error comes back only when NO page
// could be read, so a caller can tell "the site is down" from "one page hiccuped".
func TradePrices(client *http.Client, pages int) (map[string]MarketPrice, error) {
	if pages <= 0 {
		pages = DefaultTradePages
	}
	var rows []tradeRow
	var firstErr error
	got := 0
	for page := 1; page <= pages; page++ {
		body, err := fetchTradePage(client, page)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		got++
		parsed := parseTradePage(body)
		// The listing ends before the requested page count on a quiet market; an empty page
		// means there is nothing further back to read.
		if len(parsed) == 0 {
			break
		}
		rows = append(rows, parsed...)
	}
	if got == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("controlpanel: no market trade page could be read")
	}
	return averageTrades(rows), nil
}

func fetchTradePage(client *http.Client, page int) (string, error) {
	resp, err := client.Get(tradesURL(page))
	if err != nil {
		return "", fmt.Errorf("controlpanel: fetching market trades: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("controlpanel: the market site returned %s for trades page %d", resp.Status, page)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, tradesFetchLimit))
	if err != nil {
		return "", fmt.Errorf("controlpanel: reading market trades: %w", err)
	}
	return string(body), nil
}

// PriceBook answers "what is this item worth" for the Loot panel, and says where the answer
// came from. It is safe for concurrent use and refreshes itself at most once per TTL.
//
// The fallback order is the owner's: the market's own average, then high alchemy, then the
// cache's unit cost. High alchemy is 60% of that cost, which is the game's own rule, so it is
// a real in-game figure rather than an invented discount.
type PriceBook struct {
	mu     sync.Mutex
	client *http.Client
	pages  int
	now    func() time.Time
	prices map[string]MarketPrice
	listed map[string]int64
	// rows is every trade behind the current table, keyed by the site's own trade id. Keeping
	// them is what makes an incremental head refresh possible at all: a median cannot be
	// updated from another median, only recomputed from the samples.
	rows       map[int64]tradeRow
	fetched    time.Time
	headAt     time.Time
	lastErr    error
	refreshing bool
}

// ValueSource names where a price came from, so the panel can be honest about it.
type ValueSource string

const (
	ValueFromMarket ValueSource = "market"
	// ValueFromListing is a market's own last-sold or last-bought figure from its inventory
	// page — broader coverage than completed trades, but a weaker signal, so it ranks below.
	ValueFromListing ValueSource = "listing"
	// ValueFromParts is derived from what the item is made of, for something the market has
	// never traded but whose components it trades constantly. See madeOf.
	ValueFromParts ValueSource = "parts"
	ValueFromAlch  ValueSource = "alch"
	ValueFromCost  ValueSource = "cost"
)

// HighAlchOf is the game's own high-alchemy value for an item of the given cache cost: 60% of
// it, rounded down. Kept here beside its only consumer rather than in the alching script,
// which is about casting the spell, not about pricing an item.
func HighAlchOf(cost int64) int64 {
	if cost <= 0 {
		return 0
	}
	return cost * 6 / 10
}

func NewPriceBook(client *http.Client, pages int) *PriceBook {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &PriceBook{client: client, pages: pages, now: time.Now}
}

// Refresh reloads the table in the BACKGROUND if it is older than the TTL, and returns
// immediately.
//
// It has to be non-blocking: it is called from the Loot panel's own request path, and a
// refresh is 32 page fetches against someone else's website. Doing that inline would hang the
// panel for however long the site takes, and hang it again every time the site is slow. So
// the first call after the TTL kicks off a goroutine and every caller keeps serving the
// previous table meanwhile — the panel shows slightly stale prices instead of stalling, which
// for a median over ~1,300 historical trades is no loss at all.
//
// A failed refresh keeps the previous table rather than dropping every price, and records the
// error for Stats so the panel can say so.
func (p *PriceBook) Refresh() {
	if p == nil {
		return
	}
	p.mu.Lock()
	// A book built by hand rather than through NewPriceBook (every test does this, and so
	// would any future caller assembling a fixed table) has no clock and no HTTP client.
	// Treat that as "this table is not refreshable" instead of dereferencing a nil func:
	// Value still answers from whatever it holds, which is the whole point of such a book.
	if p.now == nil || p.client == nil {
		p.mu.Unlock()
		return
	}
	if p.refreshing {
		p.mu.Unlock()
		return
	}
	now := p.now()
	full := p.rows == nil || now.Sub(p.fetched) >= tradePricesTTL
	head := !full && now.Sub(p.headAt) >= headRefreshTTL
	if !full && !head {
		p.mu.Unlock()
		return
	}
	p.refreshing = true
	client, pages := p.client, p.pages
	p.mu.Unlock()

	go func() {
		if full {
			rows, err := tradeRowsFrom(client, pages)
			listed := listedPrices(client)
			p.mu.Lock()
			defer p.mu.Unlock()
			p.refreshing = false
			p.fetched, p.headAt = p.now(), p.now()
			p.lastErr = err
			if err == nil {
				p.rows = rows
				p.prices = averageTrades(valuesOf(rows))
			}
			if len(listed) > 0 {
				p.listed = listed
			}
			return
		}
		// Head refresh: one page, and only the trades we have not already counted.
		body, err := fetchTradePage(client, 1)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.refreshing = false
		p.headAt = p.now()
		p.lastErr = err
		if err != nil {
			return
		}
		added := 0
		for _, r := range parseTradePage(body) {
			if _, seen := p.rows[r.id]; seen {
				continue
			}
			p.rows[r.id] = r
			added++
		}
		// Recomputing costs nothing worth measuring (a few thousand rows) and is far safer
		// than trying to nudge a median in place.
		if added > 0 {
			p.prices = averageTrades(valuesOf(p.rows))
		}
	}()
}

func valuesOf(m map[int64]tradeRow) []tradeRow {
	out := make([]tradeRow, 0, len(m))
	for _, r := range m {
		out = append(out, r)
	}
	return out
}

// tradeRowsFrom is TradePrices' own fetch, kept as rows rather than folded straight into
// prices, so an incremental refresh has something to merge into.
func tradeRowsFrom(client *http.Client, pages int) (map[int64]tradeRow, error) {
	if pages <= 0 {
		pages = DefaultTradePages
	}
	out := map[int64]tradeRow{}
	var firstErr error
	got := 0
	for page := 1; page <= pages; page++ {
		body, err := fetchTradePage(client, page)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		got++
		parsed := parseTradePage(body)
		if len(parsed) == 0 {
			break
		}
		for _, r := range parsed {
			out[r.id] = r
		}
	}
	if got == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("controlpanel: no market trade page could be read")
	}
	return out, nil
}

// listedPrices is the SECOND price source: each market's own last-sold (else last-bought)
// figure from its bank-inventory page.
//
// It exists because the completed-trades listing only covers what has actually changed hands
// recently — a few hundred items — while the five inventory pages carry a price for anything
// the markets have ever traded, which is far broader. Users asked for both after noticing
// items priced from neither (2026-09-20).
//
// It is a weaker signal than a completed trade, so it ranks below one: a listing is what a
// market last did, not what the market will do, and last-bought in particular is the price
// somebody paid the market rather than what it asks. Hence last-sold first.
//
// Best effort throughout. A market that cannot be read is skipped; the trades table and the
// alchemy fallback still answer.
func listedPrices(client *http.Client) map[string]int64 {
	out := map[string]int64{}
	for _, m := range KnownMarkets {
		res, err := fetchMarket(client, m.Character)
		if err != nil {
			continue
		}
		for _, it := range res.Items {
			price := int64(it.LastSold)
			if price <= 0 {
				price = int64(it.LastBought)
			}
			if price <= 0 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(it.Name))
			// Several markets list the same item. Keep the lowest, which is the most
			// defensible reading of "what is this worth": it is the one a buyer could
			// actually have got it for.
			if have, ok := out[k]; !ok || price < have {
				out[k] = price
			}
		}
	}
	return out
}

// madeOf answers "this item is N of that one", for items the market has never traded but whose
// components it trades constantly.
//
// Deliberately tiny and deliberately not a crafting system. Every entry has to be a genuine
// identity on this server -- something the fleet itself assembles or splits -- not a guess at
// what an item "ought" to be worth relative to another. One entry today:
//
//   - A Crystal key is two key halves. Content pays the halves out separately (the crystal
//     chest's own drop table is "750 coins and a key half"), and internal/scripts/mulerunner's
//     JoinKeyHalvesRound turns two into one. The fleet does this thousands of times a day.
//
// Note the halves BOTH display as "Half of a key" (obj 985 and 987 are indistinguishable by
// name, which mulerunner's own comments record), so the market's single "Half of a key" row is
// the right thing to price against -- there is no per-half distinction to lose.
// The uncut dragonstone's price rule (Value): the cut gem's price plus a fixed premium. The cut gem's
// cache cost (content 289 obj.pack) is what Value falls back to through alchemy when the market has
// no price for it.
const (
	uncutDragonstoneName    = "Uncut dragonstone"
	dragonstoneName         = "Dragonstone"
	dragonstoneCacheCost    = 10000
	uncutDragonstonePremium = 10000
)

// TRAWLER FISH ARE PRICED FROM THE RAW SHARK (a user request: "value of raw sea turtle and raw manta ray should be 1k+
// higher depending on heal relative to raw shark price"). The market has hardly traded either, so both fell to their alchemy
// (300) below a Raw shark; each is now the Raw shark's value plus rawFishHealPremium a hitpoint it heals above a shark's 20
// (fleetplan/foodchoice.go: a Sea turtle heals 21, a Manta ray 22), whatever the market says of the fish itself.
const (
	rawSharkName       = "Raw shark"
	rawSharkCacheCost  = 300
	rawSharkHeal       = 20
	rawFishHealPremium = 1000
)

var rawFishHeals = map[string]int{"raw sea turtle": 21, "raw manta ray": 22}

func madeOf(key string) (n int, from string, ok bool) {
	switch key {
	case "crystal key":
		return 2, "half of a key", true
	}
	return 0, "", false
}

// Value resolves one item's unit value and says which source answered. A nil PriceBook (the
// feature switched off) still answers, from alchemy and then cost, so the caller needs no
// special case.
func (p *PriceBook) Value(name string, cacheCost int64) (int64, ValueSource) {
	// A coin is worth a coin. Stated rather than left to chance: high alchemy of a 1-coin
	// item rounds to 0, so coins only fell through to their cost by accident, and any change
	// to the rounding would silently have valued the fleet's coin pile at nothing.
	if strings.EqualFold(strings.TrimSpace(name), "Coins") {
		return 1, ValueFromCost
	}
	// AN UNCUT DRAGONSTONE IS THE CUT ONE PLUS 10,000 (a user request: "uncut dragonstone price
	// should be 10k more than the cut price"), whatever the market says of the uncut gem itself.
	if strings.EqualFold(strings.TrimSpace(name), uncutDragonstoneName) {
		cut, _ := p.Value(dragonstoneName, dragonstoneCacheCost)
		return cut + uncutDragonstonePremium, ValueFromParts
	}
	if heal, ok := rawFishHeals[strings.ToLower(strings.TrimSpace(name))]; ok {
		shark, _ := p.Value(rawSharkName, rawSharkCacheCost)
		return shark + int64(heal-rawSharkHeal)*rawFishHealPremium, ValueFromParts
	}
	if p != nil {
		k := strings.ToLower(strings.TrimSpace(name))
		p.mu.Lock()
		price, traded := p.prices[k]
		listed, isListed := p.listed[k]
		p.mu.Unlock()
		// A completed trade outranks a market's listing: one is what the item changed hands
		// for, the other is what a market last did with it. But only once more than one
		// trade agrees — see minConfidentTrades for the emerald that made this necessary.
		if traded && price.Coins > 0 && price.Trades >= minConfidentTrades && price.Coherent() {
			return price.Coins, ValueFromMarket
		}
		if isListed && listed > 0 {
			return listed, ValueFromListing
		}
		// A single uncorroborated trade is still better than nothing when the item has a
		// cache cost of zero, where alchemy and cost both answer 0 and the item would
		// otherwise read as worthless.
		if traded && price.Coins > 0 && cacheCost <= 0 {
			return price.Coins, ValueFromMarket
		}

		// NOTHING TRADED? ASK WHAT IT IS MADE OF.
		//
		// A Crystal key has never once been traded on this market -- not a thin sample, none
		// at all -- so it fell all the way to its cache cost and the panel showed the fleet's
		// keys at 1 coin each. Its two halves trade constantly and coherently (13 trades, a
		// 115,000-125,000 range), and two halves ARE a key: the mule joins them itself.
		//
		// The fleet owner's suggestion, and it is the right one: "could have the price of a
		// key the price of 2 key halves or close." Derived rather than invented, and only
		// ever from a component price the ladder above would itself have trusted.
		if n, from, ok := madeOf(k); ok {
			if part, partTraded := p.prices[from]; partTraded && part.Coins > 0 &&
				part.Trades >= minConfidentTrades && part.Coherent() {
				return part.Coins * int64(n), ValueFromParts
			}
			if listedPart, ok := p.listed[from]; ok && listedPart > 0 {
				return listedPart * int64(n), ValueFromParts
			}
		}
	}
	if alch := HighAlchOf(cacheCost); alch > 0 {
		return alch, ValueFromAlch
	}
	return cacheCost, ValueFromCost
}

// Lookup returns the whole market record for one item name, so a caller can show how THIN the
// sample behind a price is rather than only the price itself. Value answers "what is it
// worth"; this answers "and how much evidence is that". False when the market has never seen
// the item, which is not the same as a price of zero — this file's own first rule.
func (p *PriceBook) Lookup(name string) (MarketPrice, bool) {
	if p == nil {
		return MarketPrice{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	price, ok := p.prices[strings.ToLower(strings.TrimSpace(name))]
	return price, ok
}

// Stats reports what the book currently holds, for the panel's own "where these numbers came
// from" line.
func (p *PriceBook) Stats() (items int, fetched time.Time, err error) {
	if p == nil {
		return 0, time.Time{}, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.prices), p.fetched, p.lastErr
}

// Snapshot is every price the book holds, for the panel. Returns a copy: the caller must not
// be able to mutate the table another request is reading.
func (p *PriceBook) Snapshot() (prices map[string]MarketPrice, listed map[string]int64, fullAt, headAt time.Time, trades int, err error) {
	if p == nil {
		return nil, nil, time.Time{}, time.Time{}, 0, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	prices = make(map[string]MarketPrice, len(p.prices))
	for k, v := range p.prices {
		prices[k] = v
	}
	listed = make(map[string]int64, len(p.listed))
	for k, v := range p.listed {
		listed[k] = v
	}
	return prices, listed, p.fetched, p.headAt, len(p.rows), p.lastErr
}
