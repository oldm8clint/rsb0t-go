package controlpanel

import (
	"errors"
	"os"
	"testing"
	"time"
)

// testdata/market_seers.html is a real excerpt of
// https://example.invalid/market/inventory?character=795, fetched 2026-09-20: its own table header
// plus eight verbatim item rows (every axe the market listed, plus two ordinary items as
// controls). It is a genuine capture, not a hand-written mock, so this test fails if the real
// page's shape changes rather than only if our own parser changes.
func loadMarketFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/market_seers.html")
	if err != nil {
		t.Fatalf("reading the market fixture: %v", err)
	}
	return body
}

func TestParseMarketReadsTheRealPage(t *testing.T) {
	res, err := parseMarket(loadMarketFixture(t), DefaultMarketCharacter)
	if err != nil {
		t.Fatalf("parseMarket: %v", err)
	}
	if res.Market != "Seers Market" {
		t.Errorf("Market = %q, want %q", res.Market, "Seers Market")
	}
	// The snapshot's own age is the whole reason the panel can be honest about not being
	// live, so an empty one is a real failure, not a cosmetic one.
	if res.SnapshotAt == "" || res.SnapshotAge == "" {
		t.Errorf("the snapshot stamp did not parse: at=%q age=%q", res.SnapshotAt, res.SnapshotAge)
	}
	if res.Character != DefaultMarketCharacter || res.URL == "" {
		t.Errorf("character/url not carried through: %d %q", res.Character, res.URL)
	}
	if len(res.Items) != 8 {
		t.Fatalf("parsed %d items, want 8", len(res.Items))
	}
}

func TestParseMarketReadsIdsStockAndExactPrices(t *testing.T) {
	res, err := parseMarket(loadMarketFixture(t), DefaultMarketCharacter)
	if err != nil {
		t.Fatalf("parseMarket: %v", err)
	}
	byName := map[string]MarketItem{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}

	// The rune axe is the row that matters most: it carries an id, a stock, and BOTH prices,
	// and its price is the one that decides whether a mule can ever fund the purchase.
	rune, ok := byName["Rune axe"]
	if !ok {
		t.Fatal("no Rune axe row")
	}
	// 1359 is rune_axe in the game's own content/pack/obj.pack, so this also pins that the
	// page's data-id really is the game object id and not some site-internal key.
	if rune.ID != 1359 {
		t.Errorf("Rune axe id = %d, want 1359", rune.ID)
	}
	if rune.Stock != 93 {
		t.Errorf("Rune axe stock = %d, want 93", rune.Stock)
	}
	// The visible text says "150K"; the exact figure lives only in the tooltip. Parsing the
	// rounded text instead would silently under-report by up to 500 coins per unit.
	if rune.LastSold != 150000 {
		t.Errorf("Rune axe lastSold = %d, want 150000 (from the tooltip, not the rounded text)", rune.LastSold)
	}
	if rune.LastBought != 50000 {
		t.Errorf("Rune axe lastBought = %d, want 50000", rune.LastBought)
	}
	if rune.Slug != "rune_axe" {
		t.Errorf("Rune axe slug = %q", rune.Slug)
	}

	// Mithril is the cheap rung and the one a mule can realistically fund, so its price is
	// pinned too.
	if mith := byName["Mithril axe"]; mith.Stock != 10 || mith.LastSold != 600 {
		t.Errorf("Mithril axe = stock %d lastSold %d, want 10 / 600", mith.Stock, mith.LastSold)
	}

	// A row with stock but no recorded price must come back with the price ABSENT, never as
	// zero — a consumer reading 0 as "free" would send an account to buy something it cannot
	// pay for.
	iron, ok := byName["Iron axe"]
	if !ok {
		t.Fatal("no Iron axe row")
	}
	if iron.Stock != 172 {
		t.Errorf("Iron axe stock = %d, want 172", iron.Stock)
	}
	if iron.LastSold != 0 || iron.LastBought != 0 {
		t.Errorf("Iron axe has no recorded prices, got sold=%d bought=%d", iron.LastSold, iron.LastBought)
	}

	// Thousands separators in a stock count must survive.
	if anch := byName["Anchovies"]; anch.Stock != 5201 {
		t.Errorf("Anchovies stock = %d, want 5201 (comma-separated in the page)", anch.Stock)
	}
}

// TestParseMarketRefusesAPageItCannotRead pins the one strictness that matters: an unreadable
// page must be an error, never an empty market. The two are identical in JSON and mean opposite
// things to anyone deciding whether to send an account across the map for an axe.
func TestParseMarketRefusesAPageItCannotRead(t *testing.T) {
	for _, body := range []string{
		"",
		"<html><body>site is down</body></html>",
		// Shaped like the real page but with no rows at all.
		`<div class="mk-tablebox"><div class="mk-tablehead"><b>Seers Market</b> &middot; bank inventory, as of <span title="2026-09-20 07:41 UTC">6m ago</span></div><table><tbody></tbody></table>`,
		// A row whose name cell is unreadable: dropped, and with nothing left it is an error.
		`<tr data-id="1359"><td class="mk-col-item"></td><td class="mk-col-num">3</td></tr>`,
	} {
		if _, err := parseMarket([]byte(body), DefaultMarketCharacter); err == nil {
			t.Errorf("parseMarket(%.40q) returned no error", body)
		}
	}
}

func TestMarketURLNamesTheCharacter(t *testing.T) {
	if got := marketURL(795); got != "https://example.invalid/market/inventory?character=795" {
		t.Errorf("marketURL(795) = %q", got)
	}
}

// TestMarketCacheDefaultsTheCharacter keeps a caller that forgot the query parameter pointed at
// Seers rather than at character 0, which is not a market.
func TestMarketCacheDefaultsTheCharacter(t *testing.T) {
	c := newMarketCache()
	if c.client == nil || c.data == nil {
		t.Fatal("newMarketCache left a nil field")
	}
	// Prime the cache under the default id and confirm a 0 request reads the same entry
	// rather than fetching anything.
	want := MarketResult{Market: "Seers Market", Character: DefaultMarketCharacter}
	c.data[DefaultMarketCharacter] = marketCacheEntry{result: want, at: time.Now()}
	got, err := c.get(0)
	if err != nil {
		t.Fatalf("get(0): %v", err)
	}
	if got.Market != want.Market {
		t.Errorf("get(0) did not fall back to the default character, got %+v", got)
	}
}

// TestKnownMarketsAreTheOnesTheSelectorLists pins the market ids against the upstream page's
// own character selector, which reads:
//
//	<option value="864">Constructor</option><option value="941">Edge Market</option>
//	<option value="795" selected>Seers Market</option><option value="2148">Sil Stuff</option>
//	<option value="2169">Tradevault</option>
//
// Every one of these was fetched and parsed on 2026-09-20 before being listed, so none is a
// guess. Seers Market and Tradevault come first because both are in Seers' Village, at the same
// bank, and are therefore the two an account can reach with a single public-chat line.
func TestKnownMarketsAreTheOnesTheSelectorLists(t *testing.T) {
	want := []KnownMarket{
		{795, "Seers Market"},
		{2169, "Tradevault"},
		{864, "Constructor"},
		{941, "Edge Market"},
		{2148, "Sil Stuff"},
	}
	if len(KnownMarkets) != len(want) {
		t.Fatalf("KnownMarkets = %+v, want %d entries", KnownMarkets, len(want))
	}
	for i, m := range want {
		if KnownMarkets[i] != m {
			t.Errorf("KnownMarkets[%d] = %+v, want %+v", i, KnownMarkets[i], m)
		}
	}
	if KnownMarkets[0].Character != DefaultMarketCharacter {
		t.Errorf("the default character %d should be the first listed market", DefaultMarketCharacter)
	}
	// Ids must be distinct, or two selector entries would collide on one cache key.
	seen := map[int]string{}
	for _, m := range KnownMarkets {
		if prev, dup := seen[m.Character]; dup {
			t.Errorf("character %d is listed twice (%s and %s)", m.Character, prev, m.Name)
		}
		seen[m.Character] = m.Name
	}
}

// TestMarketsAllReportsAFailureAsAFailure is the same rule as the single-market parse, applied
// across markets: one market being unreadable must be reported as unreadable and must never
// look like a market that is simply out of stock. Getting this wrong would send an account to a
// market that is actually down.
func TestMarketsAllReportsAFailureAsAFailure(t *testing.T) {
	c := newMarketCache()
	// Prime every entry so nothing reaches the network: the first as a real result, the rest
	// as errors.
	good, err := parseMarket(loadMarketFixture(t), KnownMarkets[0].Character)
	if err != nil {
		t.Fatalf("parseMarket: %v", err)
	}
	c.data[KnownMarkets[0].Character] = marketCacheEntry{result: good, at: time.Now()}
	for _, m := range KnownMarkets[1:] {
		c.data[m.Character] = marketCacheEntry{err: errors.New("market is down"), at: time.Now()}
	}

	all := c.all()
	if len(all.Markets) != len(KnownMarkets) {
		t.Fatalf("all() returned %d markets, want %d", len(all.Markets), len(KnownMarkets))
	}
	// Order must follow KnownMarkets, so the panel's first column is stable between refreshes.
	for i, m := range all.Markets {
		if m.Character != KnownMarkets[i].Character || m.Name != KnownMarkets[i].Name {
			t.Errorf("all()[%d] = %+v, want %+v", i, m, KnownMarkets[i])
		}
	}
	if all.Markets[0].Result == nil || all.Markets[0].Error != "" {
		t.Errorf("the readable market should carry a result, got %+v", all.Markets[0])
	}
	if len(all.Markets[0].Result.Items) != 8 {
		t.Errorf("the readable market lost its items: %d", len(all.Markets[0].Result.Items))
	}
	for _, m := range all.Markets[1:] {
		if m.Result != nil {
			t.Errorf("%s failed but carries a result — that reads as an empty market", m.Name)
		}
		if m.Error == "" {
			t.Errorf("%s failed but carries no reason", m.Name)
		}
	}
}
