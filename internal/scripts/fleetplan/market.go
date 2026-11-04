package fleetplan

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// MarketCharacter is one market account the page lists.
type MarketCharacter struct {
	ID   int
	Name string
}

// MarketListing is one item row of one market account's bank.
type MarketListing struct {
	Market string
	Item   string
	Stock  int
	// LastSold and LastBought are the page's per-unit prices in coins, 0 when blank.
	LastSold, LastBought int
}

var (
	marketOptionRe = regexp.MustCompile(`<option value="(\d+)"( selected)?>([^<]+)</option>`)
	marketRowRe    = regexp.MustCompile(`(?s)<tr data-id="\d+">(.*?)</tr>`)
	marketItemRe   = regexp.MustCompile(`<a href="/market/item/[^"]*" title="([^"]+)"`)
	marketQtyRe    = regexp.MustCompile(`<td class="mk-col-num">([\d,]+)</td>`)
	marketPriceRe  = regexp.MustCompile(`(?s)<td class="mk-col-price[^"]*">(.*?)</td>`)
	marketCoinRe   = regexp.MustCompile(`title="([\d,]+) coins"`)
)

// ParseMarketPage reads example.invalid/market/inventory (or ?character=<id>): every market
// character in the selector, which one is shown, and the shown one's rows.
func ParseMarketPage(page string) (chars []MarketCharacter, shown string, rows []MarketListing) {
	for _, m := range marketOptionRe.FindAllStringSubmatch(page, -1) {
		id, _ := strconv.Atoi(m[1])
		name := html.UnescapeString(strings.TrimSpace(m[3]))
		chars = append(chars, MarketCharacter{ID: id, Name: name})
		if m[2] != "" {
			shown = name
		}
	}
	for _, r := range marketRowRe.FindAllStringSubmatch(page, -1) {
		body := r[1]
		item := marketItemRe.FindStringSubmatch(body)
		qty := marketQtyRe.FindStringSubmatch(body)
		if item == nil || qty == nil {
			continue
		}
		l := MarketListing{Market: shown, Item: html.UnescapeString(item[1]), Stock: atoiCommas(qty[1])}
		prices := marketPriceRe.FindAllStringSubmatch(body, -1)
		if len(prices) > 0 {
			if c := marketCoinRe.FindStringSubmatch(prices[0][1]); c != nil {
				l.LastSold = atoiCommas(c[1])
			}
		}
		if len(prices) > 1 {
			if c := marketCoinRe.FindStringSubmatch(prices[1][1]); c != nil {
				l.LastBought = atoiCommas(c[1])
			}
		}
		rows = append(rows, l)
	}
	return chars, shown, rows
}

func atoiCommas(s string) int {
	n, _ := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	return n
}

// MarketOrderText is the public-chat order, in the owner's words: "buying <amount> <item>".
func MarketOrderText(item string, qty int) string {
	return "buying " + strconv.Itoa(qty) + " " + strings.ToLower(strings.TrimSpace(item))
}

var marketPriceTextRe = regexp.MustCompile(`(?i)(\d[\d,]*(?:\.\d+)?)\s*([km])?\s*(gp|coins?|g)\b`)

// ParseMarketPrice reads a coin amount out of a market's chat reply: the first
// "<number>[k|m] (gp|coins)" in it. ok is false when there is none. See this file's HONEST STATUS.
func ParseMarketPrice(text string) (int, bool) {
	m := marketPriceTextRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || f <= 0 {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "k":
		f *= 1000
	case "m":
		f *= 1000000
	}
	return int(f + 0.5), true
}

// MarketScreen is what one trade screen with a market shows, by name.
type MarketScreen struct {
	Confirm bool
	Partner string
	Market  string
	// Theirs and Mine are the two offers, as named counts.
	Theirs, Mine []ItemCount
	// Item, Qty and Price are what was ordered and agreed.
	Item  string
	Qty   int
	Price int
	// MaxPrice is the most this account will pay for the whole order.
	MaxPrice int
}

// MarketAction is what to do with a market trade screen.
type MarketAction string

const (
	MarketDecline MarketAction = "decline"
	MarketOffer   MarketAction = "offer-coins"
	MarketWait    MarketAction = "wait"
	MarketAccept  MarketAction = "accept"
)

// DecideMarketScreen is the rule that stands between the swarm's coins and a market account:
// coins go on the screen only when the market's side is EXACTLY the ordered item and amount, the
// partner is the market spoken to, and the price is the agreed one within the cap; and the
// confirm screen is accepted only when both sides still read exactly that. Anything else is
// declined — the same care the mule's own confirm-screen checks take.
func DecideMarketScreen(s MarketScreen) (MarketAction, string) {
	if !namesMatch(s.Partner, s.Market) {
		return MarketDecline, "the trade is with " + s.Partner + ", not " + s.Market
	}
	if s.Price <= 0 || (s.MaxPrice > 0 && s.Price > s.MaxPrice) {
		return MarketDecline, "the price " + strconv.Itoa(s.Price) + " is over the cap " + strconv.Itoa(s.MaxPrice)
	}
	theirsOK := len(s.Theirs) == 1 && strings.EqualFold(s.Theirs[0].Item, s.Item) && s.Theirs[0].Count == s.Qty
	coins := 0
	mineOther := false
	for _, m := range s.Mine {
		if strings.EqualFold(m.Item, "Coins") {
			coins += m.Count
		} else {
			mineOther = true
		}
	}
	if mineOther || coins > s.Price {
		return MarketDecline, "my side is not exactly the agreed coins"
	}
	if !theirsOK {
		if s.Confirm || len(s.Theirs) > 1 || (len(s.Theirs) == 1 && (!strings.EqualFold(s.Theirs[0].Item, s.Item) || s.Theirs[0].Count > s.Qty)) {
			return MarketDecline, "their side is not exactly " + strconv.Itoa(s.Qty) + " " + s.Item
		}
		return MarketWait, "waiting for the market to put up the full order"
	}
	if coins == 0 {
		if s.Confirm {
			return MarketDecline, "a confirm screen with no coins on it"
		}
		return MarketOffer, ""
	}
	if coins != s.Price {
		return MarketWait, "my coins are still going on"
	}
	return MarketAccept, ""
}

func namesMatch(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " "))
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// DefaultMarketPlaces are where the market accounts stand, as far as the owner has said
// (2026-09-19): Seers Market "at the Seers' Village bank", Edge Market "at the Edgeville bank".
// Constructor, Sil Stuff and Tradevault have no known place yet (the owner expects the page to
// add them); until then they are not bought from, and a "marketPlaces" setting can add any.
// The tiles are the banks' own floors, where an account would stand anyway.
var DefaultMarketPlaces = map[string]tile.Tile{
	"Seers Market": {X: 2725, Z: 3493},
	"Edge Market":  {X: 3094, Z: 3493},
}
