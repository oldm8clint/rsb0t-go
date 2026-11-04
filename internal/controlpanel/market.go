// The Seers Market inventory, fetched and parsed server-side from example.invalid so the control
// panel can show what the market is actually holding.
//
// Why this is a Go-side endpoint rather than a fetch from the panel's own page: the dashboard
// serves itself with a Content-Security-Policy whose `connect-src` is `'self'`, so the browser
// cannot reach example.invalid at all. Widening the CSP for one panel would weaken it for every
// panel, so this follows the path internal/controlpanel/hiscores.go already established for the
// same site — fetch and parse here, serve our own JSON — rather than changing the policy.
//
// WHAT THIS DATA IS, stated plainly because the panel must not imply otherwise: it is a
// PERIODIC SNAPSHOT of one market character's bank, not live stock. The page says so itself in
// its own header ("bank inventory, as of <span title="2026-09-20 07:41 UTC">6m ago</span>"), and
// both halves of that are parsed out and carried through to the panel so an operator sees the
// snapshot's real age rather than assuming a number is current.
//
// WHY THE PANEL IS WORTH HAVING AT ALL: the Seers Market is a PLAYER-run shop, not a shop
// interface. There is no `openshop` anywhere in Seers' Village in this content build — a fact
// confirmed by reading the content, not assumed (`content/scripts/areas/area_seers/` has no
// `.inv` file, no `param=owned_shop` on any of its four NPCs, and no `openshop` call). So
// nothing in game can query this stock: an account buys by saying `buying <n> <item>` in public
// chat and waiting for the operator on the other side to tally it and open a trade. This panel
// is therefore the ONLY way to know whether a trip to the market is worth making.
//
// Everything parsed out of this page is DATA, never instruction: item names and numbers are
// rendered as text by the panel and are never evaluated, and a parse that finds nothing is
// reported as a failure rather than served as an empty market.
package controlpanel

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// marketBase is example.invalid's own market-inventory URL. The `character` query parameter
// identifies WHICH market, so it is a caller-supplied number rather than baked in; Seers is 795
// (supplied by the fleet owner and confirmed live — the page's own header says "Seers Market").
const marketBase = "https://example.invalid/market/inventory?character="

// DefaultMarketCharacter is the Seers Market. Callers that do not name one get this.
const DefaultMarketCharacter = 795

// KnownMarket is one player-run market the inventory page can be read for.
type KnownMarket struct {
	Character int    `json:"character"`
	Name      string `json:"name"`
}

// KnownMarkets are the markets the upstream page's own character selector lists. These ids are
// READ FROM THAT SELECTOR, not guessed:
//
//	<option value="864">Constructor</option><option value="941">Edge Market</option>
//	<option value="795" selected>Seers Market</option><option value="2148">Sil Stuff</option>
//	<option value="2169">Tradevault</option>
//
// All five were fetched and parsed on 2026-09-20 before being listed here, and each returned
// HTTP 200 with a header matching its selector label and a readable row table (64, 40, 201, 12
// and 38 rows respectively) — so none of them is a hopeful guess.
//
// Worth knowing when deciding where to send an account: on that same reading, the Seers Market
// was the ONLY one stocking a chopping axe above bronze (black 15, iron 172, mithril 10, rune
// 93, steel 1). Constructor, Edge Market and Tradevault each held exactly one bronze axe, and
// Sil Stuff held none. Tradevault is in Seers' Village too, so checking it costs no travel —
// but on today's stock it is not an axe source.
var KnownMarkets = []KnownMarket{
	{Character: 795, Name: "Seers Market"},
	{Character: 2169, Name: "Tradevault"},
	{Character: 864, Name: "Constructor"},
	{Character: 941, Name: "Edge Market"},
	{Character: 2148, Name: "Sil Stuff"},
}

// marketRowPattern matches one item row. The page gives every row the item's real object id in
// a `data-id` attribute — the SAME id the game config uses (1359 is `rune_axe` in
// content/pack/obj.pack), which is what makes a row cross-checkable against the cache rather
// than matched on a display name alone.
var marketRowPattern = regexp.MustCompile(`(?s)<tr data-id="(\d+)">(.*?)</tr>`)

// marketItemPattern pulls the slug and display name out of a row's first cell.
var marketItemPattern = regexp.MustCompile(`<a href="/market/item/([^"]*)" title="([^"]*)"`)

// marketCellPattern matches each of a row's `mk-col-*` cells in document order: item, stock,
// last sold, last bought.
var marketCellPattern = regexp.MustCompile(`(?s)<td class="mk-col-([a-z]+)[^"]*"[^>]*>(.*?)</td>`)

// marketCoinPattern reads an exact coin amount out of a price cell's own tooltip. The visible
// text is rounded ("150K"); the `title` attribute carries the real figure ("150,000 coins"), so
// the exact number is taken from there and the rounded text is never parsed.
var marketCoinPattern = regexp.MustCompile(`title="([\d,]+) coins"`)

// marketHeadPattern reads the market's own name and the snapshot's age out of the table header.
// Both the absolute timestamp (the `title`) and the relative text are captured, because the
// panel shows the relative one and needs the absolute one to be honest about it.
var marketHeadPattern = regexp.MustCompile(`<b>([^<]*)</b>[^<]*&middot;[^<]*bank inventory, as of <span title="([^"]*)">([^<]*)</span>`)

var marketTagPattern = regexp.MustCompile(`<[^>]*>`)

// MarketItem is one row of the market's inventory.
type MarketItem struct {
	// ID is the game object id, straight off the row's own data-id.
	ID int `json:"id"`
	// Name is the item's display name.
	Name string `json:"name"`
	// Slug is the site's own item-page slug, for a link out.
	Slug string `json:"slug,omitempty"`
	// Stock is how many the market's bank held at the snapshot.
	Stock int `json:"stock"`
	// LastSold and LastBought are the market's own last per-unit prices in coins. Both are
	// frequently absent — an item can sit in stock having never traded — so both are omitted
	// rather than reported as zero, and a consumer must treat a missing price as "unknown",
	// never as "free".
	LastSold   int `json:"lastSold,omitempty"`
	LastBought int `json:"lastBought,omitempty"`
}

// MarketResult is one parsed market-inventory page.
type MarketResult struct {
	// Market is the market's own name as the page gives it, e.g. "Seers Market".
	Market string `json:"market"`
	// Character is the market character id this came from.
	Character int `json:"character"`
	// URL is the page this was read from, for a link out.
	URL string `json:"url"`
	// SnapshotAt is the snapshot's own absolute timestamp, exactly as the page states it
	// (e.g. "2026-09-20 07:41 UTC"). Empty when the page's header could not be read.
	SnapshotAt string `json:"snapshotAt,omitempty"`
	// SnapshotAge is the page's own relative wording for the same thing, e.g. "6m ago".
	SnapshotAge string `json:"snapshotAge,omitempty"`
	// FetchedAt is when THIS server read the page — distinct from the snapshot's own age, and
	// both are shown, because a cached fetch of an already-stale snapshot is twice removed
	// from live.
	FetchedAt time.Time    `json:"fetchedAt"`
	Items     []MarketItem `json:"items"`
}

func marketURL(character int) string {
	return marketBase + strconv.Itoa(character)
}

// parseMarketCount reads a plain count cell: digits with optional thousands commas. Anything
// else reads as absent rather than as zero.
func parseMarketCount(s string) (int, bool) {
	s = strings.TrimSpace(marketTagPattern.ReplaceAllString(s, ""))
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseMarketPrice reads the exact coin figure out of a price cell's tooltip.
func parseMarketPrice(s string) (int, bool) {
	m := marketCoinPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// parseMarket turns a fetched page into a MarketResult. Separated from the fetch so it can be
// unit-tested against a saved copy of the real page with no network at all.
//
// Deliberately strict about one thing only: a page with no recognisable rows is an ERROR, never
// an empty market. The two read identically in JSON and mean opposite things to an operator
// deciding whether to send an account across the map.
func parseMarket(body []byte, character int) (MarketResult, error) {
	res := MarketResult{Character: character, URL: marketURL(character), FetchedAt: time.Now().UTC()}
	if head := marketHeadPattern.FindSubmatch(body); head != nil {
		res.Market = strings.TrimSpace(string(head[1]))
		res.SnapshotAt = strings.TrimSpace(string(head[2]))
		res.SnapshotAge = strings.TrimSpace(string(head[3]))
	}
	rows := marketRowPattern.FindAllSubmatch(body, -1)
	if len(rows) == 0 {
		return MarketResult{}, fmt.Errorf("controlpanel: the market page had no recognisable item rows")
	}
	res.Items = make([]MarketItem, 0, len(rows))
	for _, row := range rows {
		id, err := strconv.Atoi(string(row[1]))
		if err != nil {
			continue
		}
		item := MarketItem{ID: id}
		if m := marketItemPattern.FindSubmatch(row[2]); m != nil {
			item.Slug = string(m[1])
			item.Name = strings.TrimSpace(string(m[2]))
		}
		if item.Name == "" {
			// A row whose name cannot be read is dropped rather than shown as a blank line:
			// an unnamed row is not information, and the id alone is not actionable.
			continue
		}
		cells := marketCellPattern.FindAllSubmatch(row[2], -1)
		// Cells are positional (item, stock, last sold, last bought), so a page that changes
		// shape yields fewer cells and the missing fields stay absent rather than shifting
		// a price into the stock column.
		if len(cells) > 1 {
			if n, ok := parseMarketCount(string(cells[1][2])); ok {
				item.Stock = n
			}
		}
		if len(cells) > 2 {
			if n, ok := parseMarketPrice(string(cells[2][2])); ok {
				item.LastSold = n
			}
		}
		if len(cells) > 3 {
			if n, ok := parseMarketPrice(string(cells[3][2])); ok {
				item.LastBought = n
			}
		}
		res.Items = append(res.Items, item)
	}
	if len(res.Items) == 0 {
		return MarketResult{}, fmt.Errorf("controlpanel: the market page's rows had no readable items")
	}
	return res, nil
}

// marketFetchLimit bounds how much of someone else's page is read into memory. The real Seers
// page measured about 109 KB with roughly 200 rows, so 4 MB is a very generous ceiling that
// still refuses to read an unbounded body.
const marketFetchLimit = 4 << 20

func fetchMarket(client *http.Client, character int) (MarketResult, error) {
	url := marketURL(character)
	resp, err := client.Get(url)
	if err != nil {
		return MarketResult{}, fmt.Errorf("controlpanel: fetching the market inventory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return MarketResult{}, fmt.Errorf("controlpanel: the market site returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, marketFetchLimit))
	if err != nil {
		return MarketResult{}, fmt.Errorf("controlpanel: reading the market page: %w", err)
	}
	return parseMarket(body, character)
}

// marketCacheTTL bounds how often the same market page is re-fetched. Deliberately much longer
// than hiscoresCacheTTL's 30s: the underlying data is itself only a periodic snapshot (the live
// page said "6m ago" when this was written), so polling it faster than it changes would only
// load someone else's site for identical bytes.
const marketCacheTTL = 2 * time.Minute

type marketCacheEntry struct {
	result MarketResult
	err    error
	at     time.Time
}

// marketCache is a small TTL cache in front of fetchMarket, keyed by character id.
type marketCache struct {
	client *http.Client
	mu     sync.Mutex
	data   map[int]marketCacheEntry
}

func newMarketCache() *marketCache {
	return &marketCache{client: &http.Client{Timeout: 12 * time.Second}, data: make(map[int]marketCacheEntry)}
}

// MarketsResult is every known market in one answer, so the panel can show them together
// without the browser making five requests.
type MarketsResult struct {
	Markets []MarketEntry `json:"markets"`
}

// MarketEntry is one market's result, or the reason it could not be read. Exactly one of Result
// and Error is set, so a market that is down is reported as down rather than as empty.
type MarketEntry struct {
	Character int           `json:"character"`
	Name      string        `json:"name"`
	Result    *MarketResult `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// all reads every known market, concurrently. One market being unreadable never fails the
// others: its entry carries the error instead, because "Tradevault is down" is useful and
// "everything is down" would not be true.
func (c *marketCache) all() MarketsResult {
	out := MarketsResult{Markets: make([]MarketEntry, len(KnownMarkets))}
	var wg sync.WaitGroup
	for i, m := range KnownMarkets {
		wg.Add(1)
		go func(i int, m KnownMarket) {
			defer wg.Done()
			entry := MarketEntry{Character: m.Character, Name: m.Name}
			if res, err := c.get(m.Character); err != nil {
				entry.Error = err.Error()
			} else {
				entry.Result = &res
			}
			out.Markets[i] = entry
		}(i, m)
	}
	wg.Wait()
	return out
}

func (c *marketCache) get(character int) (MarketResult, error) {
	if character <= 0 {
		character = DefaultMarketCharacter
	}
	c.mu.Lock()
	if e, ok := c.data[character]; ok && time.Since(e.at) < marketCacheTTL {
		c.mu.Unlock()
		return e.result, e.err
	}
	c.mu.Unlock()

	result, err := fetchMarket(c.client, character)

	c.mu.Lock()
	c.data[character] = marketCacheEntry{result: result, err: err, at: time.Now()}
	c.mu.Unlock()
	return result, err
}
