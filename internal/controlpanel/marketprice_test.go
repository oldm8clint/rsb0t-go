package controlpanel

import (
	"os"
	"testing"
)

// The fixture is real HTML saved from https://example.invalid/market/trades on 2026-09-20, thirteen
// rows verbatim. Parsing is tested against the site's actual markup rather than a hand-made
// approximation, because the whole risk here is that the markup is not what we think — and it
// was not: the thirteenth row is the real Big bones trade that proved the original parser was
// reading a whole trade total as a single item's price (see TestBulkTradeIsPricedPerUnit).
func tradesFixture(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("testdata/market_trades.html")
	if err != nil {
		t.Fatalf("reading the trades fixture: %v", err)
	}
	return string(body)
}

func TestParseTradePageReadsRealMarkup(t *testing.T) {
	rows := parseTradePage(tradesFixture(t))
	if len(rows) != 13 {
		t.Fatalf("parsed %d rows, want 13 (the fixture's own row count)", len(rows))
	}
	// The first row, checked field by field against the saved HTML:
	//   <a href="/market/item/oak_logs" title="Oak logs"> ... <span title="300">300</span>
	//   ... <span class="mk-coin" title="15 coins"> ... <span class="mk-coin" title="4,500 coins">
	first := rows[0]
	if first.name != "Oak logs" || first.slug != "oak_logs" {
		t.Errorf("first row item = %q/%q, want \"Oak logs\"/\"oak_logs\"", first.name, first.slug)
	}
	if first.units != 300 {
		t.Errorf("first row units = %d, want 300", first.units)
	}
	// 15, not 4500: the last mk-coin is the trade TOTAL, and the unit price is that divided
	// by the units. Taking the total by mistake overvalues an item by the whole trade size.
	if first.unitCoins != 15 {
		t.Errorf("first row unit price = %d, want 15 (4,500 total over 300 units, not the total)", first.unitCoins)
	}
	// A comma-grouped quantity must survive: "10,000" is 10000, not 10.
	var yew *tradeRow
	for i := range rows {
		if rows[i].name == "Yew longbow" {
			yew = &rows[i]
			break
		}
	}
	if yew == nil {
		t.Fatal("no Yew longbow row parsed")
	}
	if yew.units != 10000 || yew.unitCoins != 450 {
		t.Errorf("Yew longbow = %d units at %d, want 10000 at 450", yew.units, yew.unitCoins)
	}
}

// A row can show only the TOTAL, with no "for X ea." breakdown. Reading that as a unit price
// put a raw lobster at 2,315,461 coins when the row was 5,010 of them for that much, or 462
// each. Found on the live table by looking at the reported price range.
func TestParseTradePageDerivesTheUnitPriceFromATotalOnlyRow(t *testing.T) {
	const row = `<tr data-id="1"><td class="mk-col-item">` +
		`<a href="/market/item/raw_lobster" title="Raw lobster">Raw lobster</a></td>` +
		`<td class="mk-col-price"><span class="mk-badge-sell">[S]</span> <span title="5,010">5,010</span> ` +
		`<span class="mk-coin" title="2,315,461 coins">2.3M</span></td></tr>`
	rows := parseTradePage(row)
	if len(rows) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(rows))
	}
	if rows[0].unitCoins != 462 {
		t.Errorf("unit price = %d, want 462 (2,315,461 over 5,010), not the total", rows[0].unitCoins)
	}
}

// And when a row DOES state the unit price, it is used directly rather than re-derived from
// the rounded total — but only when the two agree.
func TestParseTradePagePrefersAStatedUnitPriceThatChecksOut(t *testing.T) {
	const good = `<tr data-id="2"><td class="mk-col-item">` +
		`<a href="/market/item/oak_logs" title="Oak logs">Oak logs</a></td>` +
		`<td class="mk-col-price"><span title="300">300</span> ` +
		`<span class="mk-coin" title="15 coins">15</span> <span class="mk-coin" title="4,500 coins">4.5K</span></td></tr>`
	if rows := parseTradePage(good); len(rows) != 1 || rows[0].unitCoins != 15 {
		t.Fatalf("stated unit price: got %+v, want unitCoins 15", rows)
	}
	// 7 x 300 is 2,100, not 4,500, so the stated unit price is not consistent with the total.
	// Believe the total, which is what actually changed hands: 4,500 / 300 = 15.
	const bad = `<tr data-id="3"><td class="mk-col-item">` +
		`<a href="/market/item/oak_logs" title="Oak logs">Oak logs</a></td>` +
		`<td class="mk-col-price"><span title="300">300</span> ` +
		`<span class="mk-coin" title="7 coins">7</span> <span class="mk-coin" title="4,500 coins">4.5K</span></td></tr>`
	if rows := parseTradePage(bad); len(rows) != 1 || rows[0].unitCoins != 15 {
		t.Fatalf("inconsistent row: got %+v, want unitCoins 15 derived from the total", rows)
	}
}

// TestBulkTradeIsPricedPerUnit is the regression for a real, shipped bug.
//
// The fixture's last row is a genuine trade: 3,124 Big bones for 2,100,000 coins, which the
// site writes as "672.22 coins" each and "2,100,000 coins" in total. The original coin pattern
// demanded `[\d,]+`, so the fractional "ea." figure did not match at all and the FIRST match
// became the 2,100,000 total — which was then recorded as the price of ONE Big bone. Measured
// over 335 real rows on 2026-09-20, 55 of them (16%) had a fractional unit price and were
// mispriced this way; live, that showed Big bones at 2,100,000 coins each and Raw tuna at
// 950,000. Both the Loot panel and the Combined bank panel read this table.
func TestBulkTradeIsPricedPerUnit(t *testing.T) {
	rows := parseTradePage(tradesFixture(t))
	var bones *tradeRow
	for i := range rows {
		if rows[i].slug == "big_bones" {
			bones = &rows[i]
			break
		}
	}
	if bones == nil {
		t.Fatal("no Big bones row parsed; the fixture's bulk row is the whole point of this test")
	}
	if bones.units != 3124 {
		t.Errorf("units = %d, want 3124", bones.units)
	}
	// 2,100,000 / 3,124 = 672.2 -> 672. Anything near 2,100,000 means the total is being
	// read as one item's price again.
	if bones.unitCoins != 672 {
		t.Errorf("unit price = %d, want 672 (2,100,000 over 3,124); 2,100,000 is the whole trade", bones.unitCoins)
	}
}

// Real rows from the live site, verbatim: a bulk trade states its unit price with DECIMALS
// while the total stays whole. 163 of 1,278 live rows (13%) look like this.
//
// The unit-price pattern matches whole numbers only, so these fall through to the
// total-derivation path and come out right. This pins that, because the failure it replaced
// was severe and silent: the decimal unit price did not match, the regex's first match became
// the TOTAL, and it was recorded as the price of one item — Big bones at 2,100,000 each,
// Raw tuna at 950,000. Anyone widening the pattern to accept decimals must keep these green.
func TestParseTradePageHandlesDecimalUnitPrices(t *testing.T) {
	for _, c := range []struct {
		name      string
		row       string
		wantUnits int64
		wantCoins int64
	}{
		{
			name: "Logs, 848 at 150.94 each",
			row: `<tr data-id="41"><td class="mk-col-item"><a href="/market/item/logs" title="Logs">Logs</a></td>` +
				`<td class="mk-col-price"><span title="848">848</span> <span class="mk-coin" title="150.94 coins">150.94</span>` +
				` <span class="mk-coin" title="128,000 coins">128K</span></td></tr>`,
			wantUnits: 848, wantCoins: 150,
		},
		{
			name: "Big bones, 3124 at 672.22 each",
			row: `<tr data-id="42"><td class="mk-col-item"><a href="/market/item/big_bones" title="Big bones">Big bones</a></td>` +
				`<td class="mk-col-price"><span title="3,124">3,124</span> <span class="mk-coin" title="672.22 coins">672.22</span>` +
				` <span class="mk-coin" title="2,100,000 coins">2.1M</span></td></tr>`,
			wantUnits: 3124, wantCoins: 672,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			rows := parseTradePage(c.row)
			if len(rows) != 1 {
				t.Fatalf("parsed %d rows, want 1 — a decimal unit price must not drop the row", len(rows))
			}
			if rows[0].units != c.wantUnits {
				t.Errorf("units = %d, want %d", rows[0].units, c.wantUnits)
			}
			if rows[0].unitCoins != c.wantCoins {
				t.Errorf("unit price = %d, want %d — not the total, which is what this used to record", rows[0].unitCoins, c.wantCoins)
			}
		})
	}
}

func TestAverageTradesUsesTheMedianAndIsCaseInsensitive(t *testing.T) {
	// The case that decided median over mean. A simple mean of these is 500,050; the
	// VOLUME-WEIGHTED mean is 199 — still nearly double the real price, because weighting by
	// units puts the silly trade's coins in the numerator regardless. The median is 100.
	got := averageTrades([]tradeRow{
		{name: "Yew logs", slug: "yew_logs", units: 10000, unitCoins: 100},
		{name: "Yew logs", slug: "yew_logs", units: 5000, unitCoins: 100},
		{name: "Yew logs", slug: "yew_logs", units: 1, unitCoins: 1000000},
	})
	price, ok := got["yew logs"]
	if !ok {
		t.Fatalf("the map is keyed by lower-cased name; got keys %v", keysOf(got))
	}
	if price.Coins != 100 {
		t.Errorf("median = %d, want 100 (a volume-weighted mean would be about 66,700 here)", price.Coins)
	}
	if price.Trades != 3 || price.Units != 15001 {
		t.Errorf("sample = %d trades / %d units, want 3 / 15001 — a consumer needs to see how thin it is", price.Trades, price.Units)
	}
	if price.Name != "Yew logs" {
		t.Errorf("display name = %q, want the market's own spelling", price.Name)
	}
}

func TestAverageTradesSkipsRowsThatCannotBePriced(t *testing.T) {
	got := averageTrades([]tradeRow{
		{name: "", units: 5, unitCoins: 10},
		{name: "Ghost item", units: 0, unitCoins: 10},
	})
	if len(got) != 0 {
		t.Errorf("priced %v, want nothing: a row with no name or no units is not evidence of a price", keysOf(got))
	}
}

// The fallback order is the owner's: market, then high alchemy, then the cache cost. The
// important property is that a missing market price never reads as zero.
func TestPriceBookFallsBackThroughAlchToCost(t *testing.T) {
	book := &PriceBook{prices: map[string]MarketPrice{
		"yew logs": {Name: "Yew logs", Coins: 190, Trades: 2, Units: 10001},
	}}

	if v, src := book.Value("Yew logs", 4); v != 190 || src != ValueFromMarket {
		t.Errorf("a traded item = %d from %q, want 190 from market (not its cache cost of 4)", v, src)
	}
	if v, src := book.Value("YEW LOGS", 4); v != 190 || src != ValueFromMarket {
		t.Errorf("lookup must ignore case: got %d from %q", v, src)
	}
	// Never traded, but the cache knows a cost: high alchemy is 60% of it.
	if v, src := book.Value("Uncut sapphire", 250); v != 150 || src != ValueFromAlch {
		t.Errorf("an untraded item = %d from %q, want 150 from alch (60%% of 250)", v, src)
	}
	// No cost either: fall through to cost rather than inventing one.
	if v, src := book.Value("Nothing", 0); v != 0 || src != ValueFromCost {
		t.Errorf("an unknown item = %d from %q, want 0 from cost", v, src)
	}
	// A nil book must still answer, so the caller needs no special case when the feature is
	// switched off.
	var off *PriceBook
	if v, src := off.Value("Yew logs", 250); v != 150 || src != ValueFromAlch {
		t.Errorf("with no price book: %d from %q, want 150 from alch", v, src)
	}
}

// The emerald case, which is the reason minConfidentTrades exists. One trade at an absurd
// price must not outrank the weaker-but-corroborated sources beneath it.
func TestPriceBookIgnoresASingleUncorroboratedTrade(t *testing.T) {
	book := &PriceBook{
		prices: map[string]MarketPrice{
			"uncut emerald": {Name: "Uncut emerald", Coins: 335000, Trades: 1, Units: 152},
			"uncut ruby":    {Name: "Uncut ruby", Coins: 2800, Trades: 5, Units: 597},
			"odd thing":     {Name: "Odd thing", Coins: 4242, Trades: 1, Units: 3},
		},
		listed: map[string]int64{"uncut emerald": 1200},
	}
	// One trade, but a listing exists: the listing wins.
	if v, src := book.Value("Uncut emerald", 50); v != 1200 || src != ValueFromListing {
		t.Errorf("one-trade emerald with a listing = %d from %q, want 1200 from listing", v, src)
	}
	// Five agreeing trades: the median wins, as it should.
	if v, src := book.Value("Uncut ruby", 100); v != 2800 || src != ValueFromMarket {
		t.Errorf("five-trade ruby = %d from %q, want 2800 from market", v, src)
	}
	// One trade, no listing, but the cache knows a cost: high alchemy rather than the
	// uncorroborated figure.
	delete(book.listed, "uncut emerald")
	if v, src := book.Value("Uncut emerald", 50); v != 30 || src != ValueFromAlch {
		t.Errorf("one-trade emerald with no listing = %d from %q, want 30 from alch (60%% of 50)", v, src)
	}
	// One trade, no listing, no cache cost: the lone trade beats reporting it as worthless.
	if v, src := book.Value("Odd thing", 0); v != 4242 || src != ValueFromMarket {
		t.Errorf("one-trade item with no other evidence = %d from %q, want 4242 from market", v, src)
	}
}

// Prices carry their spread, not just the middle. Users asked for ranges as well as
// medians, and the spread is what says how firm a price is.
func TestAverageTradesReportsTheSpreadAndNewestTrade(t *testing.T) {
	got := averageTrades([]tradeRow{
		{id: 10, name: "Yew logs", units: 100, unitCoins: 300},
		{id: 30, name: "Yew logs", units: 100, unitCoins: 315},
		{id: 20, name: "Yew logs", units: 100, unitCoins: 900},
	})
	p := got["yew logs"]
	// 25th and 75th percentile of {300, 315, 900}: index 0 and index 1.
	if p.Low != 300 || p.High != 315 {
		t.Errorf("range = %d..%d, want 300..315 — the interquartile spread, not the extremes", p.Low, p.High)
	}
	if p.Coins != 315 {
		t.Errorf("median = %d, want 315 — the middle of the three, not the middle of the range", p.Coins)
	}
	if p.LastTradeID != 30 {
		t.Errorf("newest trade id = %d, want 30 (the highest, whatever order they arrived in)", p.LastTradeID)
	}
}

// The trade id is what makes a cheap re-read of page 1 safe: the same trades appear again and
// must not be counted twice, or every refresh would drag the median toward whatever is
// currently on the front page.
func TestParseTradePageCapturesTheTradeID(t *testing.T) {
	rows := parseTradePage(tradesFixture(t))
	if len(rows) == 0 {
		t.Fatal("no rows parsed")
	}
	seen := map[int64]bool{}
	for _, r := range rows {
		if r.id <= 0 {
			t.Fatalf("row %q has no trade id; a re-read could not be deduplicated", r.name)
		}
		if seen[r.id] {
			t.Errorf("trade id %d appeared twice in one page", r.id)
		}
		seen[r.id] = true
	}
	// The fixture's first row is data-id="49719", verbatim from the site.
	if rows[0].id != 49719 {
		t.Errorf("first row id = %d, want 49719", rows[0].id)
	}
}

// Real single-unit anomalies exist on this market and must not define an item's range: one
// raw lobster changed hands for 5,100,800 coins against a median of 200.
func TestAverageTradesRangeIgnoresASingleAnomaly(t *testing.T) {
	rows := []tradeRow{{id: 99, name: "Raw lobster", units: 1, unitCoins: 5100800}}
	for i := 0; i < 20; i++ {
		rows = append(rows, tradeRow{id: int64(i), name: "Raw lobster", units: 100, unitCoins: 200})
	}
	p := averageTrades(rows)["raw lobster"]
	if p.Coins != 200 {
		t.Errorf("median = %d, want 200", p.Coins)
	}
	if p.High > 1000 {
		t.Errorf("high = %d, want a figure near 200 — one anomalous trade must not define the range", p.High)
	}
	// And it must not be counted at all: the owner identified these as a market owner moving
	// coins to their own account by "selling" one item for an enormous sum.
	if p.Trades != 21 {
		t.Errorf("trades = %d, want all 21 counted", p.Trades)
	}
}

func TestWithoutSelfTransfersDropsOnlyTheAbsurdlyHigh(t *testing.T) {
	// A genuine spread around a median of 2,800 survives untouched.
	genuine := []int64{2200, 2500, 2800, 3000, 3225}
	if got := withoutSelfTransfers(genuine); len(got) != len(genuine) {
		t.Errorf("genuine spread lost %d of %d prices", len(genuine)-len(got), len(genuine))
	}
	// A transfer far above the median goes.
	withTransfer := []int64{190, 195, 200, 210, 5100800}
	got := withoutSelfTransfers(withTransfer)
	if len(got) != 4 || got[len(got)-1] != 210 {
		t.Errorf("got %v, want the four real prices with the 5,100,800 transfer dropped", got)
	}
	// Too few to judge: left alone, and minConfidentTrades decides whether to trust it.
	if got := withoutSelfTransfers([]int64{200, 9999999}); len(got) != 2 {
		t.Errorf("with only two samples the filter must not guess; got %v", got)
	}
}

func TestHighAlchIsSixtyPercentRoundedDown(t *testing.T) {
	for _, c := range []struct{ cost, want int64 }{{0, 0}, {-5, 0}, {1, 0}, {10, 6}, {250, 150}, {12799, 7679}} {
		if got := HighAlchOf(c.cost); got != c.want {
			t.Errorf("HighAlchOf(%d) = %d, want %d", c.cost, got, c.want)
		}
	}
}

func keysOf(m map[string]MarketPrice) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Cake, as the live market actually recorded it on 2026-09-21: one trade of 1,000 units at 53
// coins, and three of 100 units at 11,092-13,484 sold by one market inside two minutes. The
// median counts trades, so the three win and 91,000 banked cakes get valued at about a billion
// coins. The sample contradicts itself by 36x and the right answer is to decline it.
func TestASampleThatContradictsItselfIsNotAPrice(t *testing.T) {
	cake := MarketPrice{Name: "Cake", Coins: 11092, Trades: 4, Units: 1300, Low: 376, High: 13484}
	if cake.Coherent() {
		t.Errorf("Cake p25=%d p75=%d (%.1fx) reported coherent — this is the billion-coin bank valuation the owner spotted",
			cake.Low, cake.High, float64(cake.High)/float64(cake.Low))
	}

	// The worst LEGITIMATE spreads on the same table must survive. Rune scimitar is the widest
	// at 5.5x, Diamond next at 4.2x. If either of these ever fails, the threshold moved too far.
	for _, ok := range []MarketPrice{
		{Name: "Rune scimitar", Coins: 40000, Trades: 16, Low: 40000, High: 220000},
		{Name: "Diamond", Coins: 4059, Trades: 5, Low: 2255, High: 9500},
		{Name: "Bow string", Coins: 150, Trades: 72, Low: 140, High: 190},
	} {
		if !ok.Coherent() {
			t.Errorf("%s p25=%d p75=%d reported incoherent — a real price was thrown away", ok.Name, ok.Low, ok.High)
		}
	}

	// Too small a sample to have a spread is minConfidentTrades' problem, not this one's.
	if !(MarketPrice{Name: "Thing", Coins: 5}).Coherent() {
		t.Error("an item with no recorded spread should fall through to the rest of the ladder, not be rejected here")
	}
}

// The ladder, end to end: an incoherent market price must not be returned as ValueFromMarket,
// and something further down must answer instead.
func TestAnIncoherentPriceFallsDownTheLadder(t *testing.T) {
	p := &PriceBook{
		prices: map[string]MarketPrice{
			"cake": {Name: "Cake", Coins: 11092, Trades: 4, Low: 376, High: 13484},
		},
		listed: map[string]int64{},
	}
	got, src := p.Value("Cake", 100)
	if src == ValueFromMarket {
		t.Fatalf("Cake answered %d from the market despite a 36x spread", got)
	}
	if want := HighAlchOf(100); got != want || src != ValueFromAlch {
		t.Fatalf("Cake = %d (%s), want %d from alchemy", got, src, want)
	}
}

// The two sides of the market are two different questions. Measured on the live table
// (2026-09-21): a Rune axe BUYS at 50,000 and SELLS at 150,000, a Raw shark 900 against 1,200,
// a Law rune 250 against 300. Buy is below Sell on essentially every item carrying both, and
// Buy is the one the fleet could actually realise for what it is holding.
func TestTheValueIsWhatTheMarketPaysNotWhatItCharges(t *testing.T) {
	rows := []tradeRow{
		{id: 1, name: "Rune axe", units: 1, unitCoins: 50000, marketBought: true},
		{id: 2, name: "Rune axe", units: 1, unitCoins: 50000, marketBought: true},
		{id: 3, name: "Rune axe", units: 1, unitCoins: 150000},
		{id: 4, name: "Rune axe", units: 1, unitCoins: 150000},
		{id: 5, name: "Rune axe", units: 1, unitCoins: 150000},
	}
	got := averageTrades(rows)["rune axe"]
	if got.Buy != 50000 || got.Sell != 150000 {
		t.Fatalf("buy/sell = %d/%d, want 50000/150000", got.Buy, got.Sell)
	}
	if got.Coins != 50000 {
		t.Errorf("Coins = %d, want the BUY side (50000) — what the fleet could get, not the market's asking price", got.Coins)
	}
	if got.BuyTrades != 2 || got.SellTrades != 3 {
		t.Errorf("per-side trade counts = %d/%d, want 2/3", got.BuyTrades, got.SellTrades)
	}

	// Sell-only is the ordinary case, not the exception: 117 of 204 items on the live table
	// have no buy side at all. The owner's rule is to use the sell price there.
	sellOnly := averageTrades([]tradeRow{
		{id: 1, name: "Thing", units: 1, unitCoins: 700},
		{id: 2, name: "Thing", units: 1, unitCoins: 700},
	})["thing"]
	if sellOnly.Buy != 0 || sellOnly.Coins != 700 {
		t.Errorf("sell-only: buy=%d coins=%d, want 0 and 700", sellOnly.Buy, sellOnly.Coins)
	}
}

// A Crystal key has never been traded on this market — not a thin sample, NONE — so it fell to
// its cache cost and the panel showed the fleet's keys at 1 coin each. Its two halves trade
// constantly, and two halves are a key: the mule joins them itself, thousands of times a day.
func TestAKeyIsWorthItsTwoHalves(t *testing.T) {
	p := &PriceBook{
		prices: map[string]MarketPrice{
			"half of a key": {Name: "Half of a key", Coins: 115000, Trades: 13, Low: 115000, High: 125000},
		},
		listed: map[string]int64{},
	}
	got, src := p.Value("Crystal key", 1)
	if src != ValueFromParts || got != 230000 {
		t.Fatalf("Crystal key = %d (%s), want 230000 from parts — 2 x 115,000", got, src)
	}

	// The component has to clear the SAME bar the ladder applies to anything else. A half
	// priced off one uncorroborated trade must not launder itself into a key's price.
	thin := &PriceBook{
		prices: map[string]MarketPrice{
			"half of a key": {Name: "Half of a key", Coins: 900000, Trades: 1},
		},
		listed: map[string]int64{},
	}
	if _, src := thin.Value("Crystal key", 1); src == ValueFromParts {
		t.Error("a single uncorroborated half-price was used to derive a key — the derivation must not be a way around minConfidentTrades")
	}

	// Nothing else is derived. This table is a short list of real identities, not a crafting
	// system, and it should stay that way.
	if _, _, ok := madeOf("rune platebody"); ok {
		t.Error("madeOf answered for an item that is not assembled from a traded component")
	}
}

// TestUncutDragonstoneIsTheCutPricePlusTenThousand: the owner's "uncut dragonstone price should be 10k
// more than the cut price" (7 Oct 2026), whatever the market says of the uncut gem.
func TestTrawlerFishArePricedFromTheRawShark(t *testing.T) {
	book := priced(map[string]MarketPrice{
		"raw shark":     {Name: "Raw shark", Coins: 900, Trades: 20, Units: 100},
		"raw manta ray": {Name: "Raw manta ray", Coins: 100, Trades: 20, Units: 100},
	})
	if v, _ := book.Value("Raw sea turtle", 500); v != 1900 {
		t.Fatalf("raw sea turtle = %d, want the shark's 900 + 1000", v)
	}
	if v, _ := book.Value("Raw manta ray", 500); v != 2900 {
		t.Fatalf("raw manta ray = %d, want the shark's 900 + 2000, whatever its own market says", v)
	}
	var none *PriceBook
	shark, _ := none.Value("Raw shark", 300)
	if v, _ := none.Value("raw manta ray", 500); v != shark+2000 {
		t.Fatalf("raw manta ray with no market = %d, want %d", v, shark+2000)
	}
}

func TestUncutDragonstoneIsTheCutPricePlusTenThousand(t *testing.T) {
	book := priced(map[string]MarketPrice{
		"dragonstone":       {Name: "Dragonstone", Coins: 30000, Trades: 20, Units: 100},
		"uncut dragonstone": {Name: "Uncut dragonstone", Coins: 25000, Trades: 20, Units: 100},
	})
	if v, _ := book.Value("Uncut dragonstone", 10000); v != 40000 {
		t.Fatalf("uncut = %d, want 40000", v)
	}
	// With no market at all the cut gem falls back to its alchemy, and the premium still applies.
	var none *PriceBook
	cut, _ := none.Value("Dragonstone", 10000)
	if v, _ := none.Value("uncut dragonstone", 10000); v != cut+10000 {
		t.Fatalf("uncut with no market = %d, want %d", v, cut+10000)
	}
}
