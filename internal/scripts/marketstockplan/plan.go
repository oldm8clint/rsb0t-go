// Package marketstockplan is the pure half of RuneRunner's market station: keeping the mule
// stocked with the scarce runes (Nature first) by buying them from the Seers' Village player
// stalls with coins, when a stall actually lists them.
//
// WHY THIS EXISTS (7 October 2026). The mule held 109 Nature runes, no Death, and the shops give
// about 36 Natures an hour (Lundail), so walk alchemy, fight alchemy, the endgame stun alchemy and
// MasterCrafter's chest alchemy all stalled. The mule held 16 million coins. A player stall had
// sold the fleet about 10,700 Nature runes at 600 each the night before (S15, the market page's
// trades list), so the market is a real source -- when a stall has stock. On the day this was
// written the player stalls listed 17 Nature runes between them, so the station is built to cost
// nothing until a stall lists a worthwhile lot: one cached page read per check.
//
// What this package decides, with no client and no I/O: which rune is short (target stock less the
// mule's published count less what the runner already carries or banks for it), the price ceiling
// for it (a multiple of the trade book's recent median SELL price, never above a fixed hard
// ceiling), which stall to ask (cheapest listed price at or under the ceiling, enough stock for a
// worthwhile lot, not resting), and how a lot splits under the stall's per-trade coin limit. The
// purchase itself is scarabrunner.BuyRunesAtMarket, the live-proven stall path.
package marketstockplan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Defaults and bounds.
const (
	// DefaultPricePercent is the price ceiling as a percentage of the book's recent median SELL
	// price (the price a stall charged): 150 is "up to 1.5 times the median".
	DefaultPricePercent = 150
	MinPricePercent     = 100
	MaxPricePercent     = 400
	// DefaultCoins is the most coins one market station may spend, drawn from the account's own
	// bank at the Seers bank.
	DefaultCoins = 1000000
	// MaxCoins is 200M since 2 Nov 2026 (owner: "spend as much coins as we need on essence and other stock").
	MaxCoins = 200000000
	// DefaultCheckMinutes is the least time between two looks at the market: rounds are minutes
	// apart, the page is a snapshot that moves slowly, and a trip is long.
	DefaultCheckMinutes = 15
	MaxCheckMinutes     = 240
	// DefaultMinLot is the smallest purchase worth the walk to Seers, for a rune whose shortfall
	// is at least this big (a smaller shortfall is bought whole).
	DefaultMinLot = 100
	MaxMinLot     = 100000
	// DefaultTradeCoins is a stall's per-trade coin ceiling. S15's trades on 6 October were
	// 1.67K Nature at 600 = 999.6K, so it is 1,000,000; the Seers Market's own is 5,000,000.
	DefaultTradeCoins = 1000000
	MaxTradeCoins     = 5000000
	// SlackPercent is how far above its listed price a stall's own quote may be taken (the same
	// 25% as scarabplan.MarketSlack), always under the ceiling.
	SlackPercent = 125
	// MinBookTrades is how many completed SELL trades make a book median worth trusting: the same
	// bar controlpanel/marketprice.go keeps (two corroborating trades).
	MinBookTrades = 2
	// MaxTradesPerStation bounds one station's trades, so a bug cannot spend a bank in one visit.
	// It is 30 since 2 Nov 2026 (owner: keep buying "until they dont have anything left to sell"): a stall's
	// whole stock in one visit, still bounded.
	MaxTradesPerStation = 30
)

// DefaultTargets are the stock the mule should hold, "Rune=count". Nature is the bottleneck;
// Death (Wind Blast), Fire (alchemy, five a cast) and Chaos (Wind Bolt) are bought only where a
// stall lists them cheaply.
func DefaultTargets() []string {
	return []string{"Nature rune=20000", "Death rune=6000", "Fire rune=10000", "Chaos rune=8000"}
}

// DefaultStalls are the stalls asked, in order. The first three stand at the Seers bank (the
// quest market code's own list); S15 sold the fleet Nature runes on 6 October, and where it
// stands is unrecorded, so the station asks it only when it is among the visible players.
func DefaultStalls() []string {
	return []string{"Seers Market", "Tradevault", "Sil Stuff", "S15"}
}

// HardCeilings are the most one rune may cost, whatever the book says: a guard against a book
// median skewed by a handful of silly trades. They sit a little above 1.5 times the prices seen
// on 6-7 October (Nature 600, Death 300, Fire 45, Chaos 150).
var HardCeilings = map[string]int{
	"Nature rune": 1000,
	"Death rune":  600,
	"Fire rune":   100,
	"Chaos rune":  300,
	"Law rune":    600,
	"Blood rune":  2000,
	"Cosmic rune": 400,
	"Air rune":    80,
	"Water rune":  80,
	"Earth rune":  80,
	"Mind rune":   150,
	"Body rune":   80,
	// 2 Nov 2026 (owner: "buying all rune essence" at the Seers bank before runecrafting): the stalls sold
	// it at about 100 (mastercrafter_runemarket.go).
	"Rune essence": 150,
}

var runeNames = func() map[string]string {
	m := map[string]string{}
	for k := range HardCeilings {
		m[strings.ToLower(k)] = k
	}
	return m
}()

// CanonicalRune is the rune's display name ("nature rune", "Nature" -> "Nature rune").
func CanonicalRune(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if c, ok := runeNames[n]; ok {
		return c, true
	}
	if !strings.HasSuffix(n, " rune") {
		n += " rune"
	}
	c, ok := runeNames[n]
	return c, ok
}

// Target is one rune's stock goal on the mule.
type Target struct {
	Rune  string
	Stock int
}

// ParseTargets reads "Rune=count" entries, in priority order. No entries gives DefaultTargets.
func ParseTargets(entries []string) ([]Target, error) {
	if len(entries) == 0 {
		entries = DefaultTargets()
	}
	var out []Target
	seen := map[string]bool{}
	for _, e := range entries {
		eq := strings.LastIndex(e, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("%q is not Rune=count", e)
		}
		name, ok := CanonicalRune(e[:eq])
		if !ok {
			return nil, fmt.Errorf("%q: %q is not a rune", e, strings.TrimSpace(e[:eq]))
		}
		n, err := strconv.Atoi(strings.TrimSpace(e[eq+1:]))
		if err != nil || n < 1 || n > 10000000 {
			return nil, fmt.Errorf("%q: the stock must be a whole number from 1 to 10000000", e)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s is listed twice", name)
		}
		seen[name] = true
		out = append(out, Target{Rune: name, Stock: n})
	}
	return out, nil
}

// ParseStalls checks the stall names: non-empty, no duplicates (case-insensitive). No entries
// gives DefaultStalls.
func ParseStalls(entries []string) ([]string, error) {
	if len(entries) == 0 {
		return DefaultStalls(), nil
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range entries {
		n := strings.TrimSpace(e)
		if n == "" {
			return nil, fmt.Errorf("a stall name is empty")
		}
		if seen[strings.ToLower(n)] {
			return nil, fmt.Errorf("%s is listed twice", n)
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return out, nil
}

// Book is what the trade book says about one item: the median SELL price (what a stall charged),
// how many sell trades it rests on, and whether the item's own trades agree (controlpanel's
// Coherent).
type Book struct {
	Sell       int
	SellTrades int
	Coherent   bool
}

// Trusted reports whether the book's sell median is worth using.
func (b Book) Trusted() bool { return b.Sell > 0 && b.SellTrades >= MinBookTrades && b.Coherent }

// Ceiling is the most one unit of rune may cost: percent of the book's sell median when the book
// is trusted, never above the rune's hard ceiling; the hard ceiling alone when it is not. why says
// which answered.
func Ceiling(rune string, book Book, known bool, percent int) (int, string) {
	hard := HardCeilings[rune]
	if hard <= 0 {
		return 0, "no hard ceiling for " + rune
	}
	if percent < MinPricePercent {
		percent = DefaultPricePercent
	}
	if known && book.Trusted() {
		c := (book.Sell*percent + 99) / 100
		if c > hard {
			return hard, fmt.Sprintf("book median %d x %d%% = %d, held to the hard ceiling %d", book.Sell, percent, c, hard)
		}
		return c, fmt.Sprintf("book median %d (%d trades) x %d%%", book.Sell, book.SellTrades, percent)
	}
	return hard, "no trusted book median; the hard ceiling"
}

// Listing is one stall's line for one item: stock and its listed price (0 when the page shows
// none).
type Listing struct {
	Market string
	Item   string
	Stock  int
	Ask    int
}

// Input is one planning pass.
type Input struct {
	Targets []Target
	// Held is how many of a rune the mule's published bank holds; ok is false when it published
	// none, and that rune is then left alone (nothing can be said to be short).
	Held func(rune string) (int, bool)
	// Pending is what this runner already carries or has banked for the mule, by rune.
	Pending map[string]int
	// Deliverable limits the plan to runes the runner hands to the mule (its rune list); nil means
	// every rune.
	Deliverable func(rune string) bool
	Listings    []Listing
	// Book answers for an item by its display name.
	Book func(item string) (Book, bool)
	// Stalls are the stalls that may be asked; on a price tie the earlier one wins.
	Stalls []string
	// Resting are stalls not to ask for an item now, keyed by RestKey.
	Resting map[string]bool
	// Coins is what the station may spend.
	Coins        int
	PricePercent int
	MinLot       int
	TradeCoins   int
}

// Trade is one purchase: Qty of Item from Market, offered at Unit each and re-offered at the
// stall's own quote only up to UnitCap each, never spending more than MaxCoins.
type Trade struct {
	Market  string
	Item    string
	Qty     int
	Unit    int
	UnitCap int
	// MaxCoins is Qty x UnitCap.
	MaxCoins int
	// Ceiling is the rune's price ceiling this trade was planned under.
	Ceiling int
	// Short is how many the mule was short of its target when planned.
	Short int
}

// RestKey is Resting's key for a stall and an item.
func RestKey(market, item string) string {
	return strings.ToLower(strings.TrimSpace(market)) + "\x00" + strings.ToLower(strings.TrimSpace(item))
}

// Plan is the answer: Trades in the order to make them, and one line per rune saying why it has
// none.
type Plan struct {
	Trades []Trade
	Why    []string
}

// Reason is the plan's single log line when it has no trades.
func (p Plan) Reason() string {
	if len(p.Why) == 0 {
		return "nothing to buy"
	}
	return strings.Join(p.Why, "; ")
}

// MakePlan chooses the purchases. For each target in order that the mule is short of, it picks the
// stall with the lowest listed price at or under the ceiling that has a worthwhile lot, and splits
// the lot under the per-trade coin limit. Coins are shared out in target order.
func MakePlan(in Input) Plan {
	var p Plan
	pct := in.PricePercent
	if pct == 0 {
		pct = DefaultPricePercent
	}
	minLot := in.MinLot
	if minLot <= 0 {
		minLot = DefaultMinLot
	}
	tradeCoins := in.TradeCoins
	if tradeCoins <= 0 {
		tradeCoins = DefaultTradeCoins
	}
	allowed := map[string]int{}
	for i, s := range in.Stalls {
		allowed[strings.ToLower(strings.TrimSpace(s))] = i
	}
	coins := in.Coins
	for _, t := range in.Targets {
		if len(p.Trades) >= MaxTradesPerStation {
			break
		}
		if in.Deliverable != nil && !in.Deliverable(t.Rune) {
			p.Why = append(p.Why, t.Rune+": not a rune this runner delivers")
			continue
		}
		held, known := 0, false
		if in.Held != nil {
			held, known = in.Held(t.Rune)
		}
		if !known {
			p.Why = append(p.Why, t.Rune+": the mule has published no bank")
			continue
		}
		short := t.Stock - held - in.Pending[t.Rune]
		if short <= 0 {
			p.Why = append(p.Why, fmt.Sprintf("%s: the mule holds %d (+%d on the way) of %d", t.Rune, held, in.Pending[t.Rune], t.Stock))
			continue
		}
		var book Book
		var bookKnown bool
		if in.Book != nil {
			book, bookKnown = in.Book(t.Rune)
		}
		ceiling, ceilWhy := Ceiling(t.Rune, book, bookKnown, pct)
		if ceiling <= 0 {
			p.Why = append(p.Why, t.Rune+": "+ceilWhy)
			continue
		}
		best, bestWhy := pickStall(in, t.Rune, short, ceiling, book, bookKnown, allowed, coins, minLot)
		if best == nil {
			p.Why = append(p.Why, fmt.Sprintf("%s: short %d, ceiling %d (%s): %s", t.Rune, short, ceiling, ceilWhy, bestWhy))
			continue
		}
		for _, tr := range splitLot(*best, tradeCoins, MaxTradesPerStation-len(p.Trades)) {
			tr.Ceiling, tr.Short = ceiling, short
			p.Trades = append(p.Trades, tr)
			coins -= tr.MaxCoins
		}
	}
	return p
}

type lot struct {
	Market  string
	Item    string
	Qty     int
	Unit    int
	UnitCap int
}

func pickStall(in Input, rune string, short, ceiling int, book Book, bookKnown bool, allowed map[string]int, coins, minLot int) (*lot, string) {
	var why []string
	var best *lot
	bestRank := 0
	for _, l := range in.Listings {
		if !strings.EqualFold(strings.TrimSpace(l.Item), rune) {
			continue
		}
		rank, ok := allowed[strings.ToLower(strings.TrimSpace(l.Market))]
		if !ok {
			continue
		}
		if in.Resting[RestKey(l.Market, rune)] {
			why = append(why, l.Market+" is resting")
			continue
		}
		if l.Stock <= 0 {
			continue
		}
		unit := l.Ask
		if unit <= 0 && bookKnown && book.Sell > 0 {
			// The page shows no price for the line: offer the book's sell median, which the
			// stall's own quote corrects (BuyRunesAtMarket re-offers once at its number).
			unit = book.Sell
		}
		if unit <= 0 {
			why = append(why, l.Market+" lists it with no price and the book has none")
			continue
		}
		if unit > ceiling {
			why = append(why, fmt.Sprintf("%s asks %d, over the ceiling", l.Market, unit))
			continue
		}
		unitCap := (unit*SlackPercent + 99) / 100
		if unitCap > ceiling {
			unitCap = ceiling
		}
		if unitCap < unit {
			unitCap = unit
		}
		qty := short
		if l.Stock < qty {
			qty = l.Stock
		}
		if afford := coins / unitCap; afford < qty {
			qty = afford
		}
		need := minLot
		if short < need {
			need = short
		}
		if qty < need {
			why = append(why, fmt.Sprintf("%s: only %d can be bought (stock %d, coins %d), under the lot of %d", l.Market, qty, l.Stock, coins, need))
			continue
		}
		cand := &lot{Market: l.Market, Item: rune, Qty: qty, Unit: unit, UnitCap: unitCap}
		if best == nil || unit < best.Unit || (unit == best.Unit && (qty > best.Qty || (qty == best.Qty && rank < bestRank))) {
			best, bestRank = cand, rank
		}
	}
	if best == nil {
		if len(why) == 0 {
			return nil, "no stall lists it"
		}
		sort.Strings(why)
		return nil, strings.Join(why, ", ")
	}
	return best, ""
}

// splitLot cuts a lot into trades each under tradeCoins at the unit cap, at most maxTrades.
func splitLot(l lot, tradeCoins, maxTrades int) []Trade {
	if maxTrades < 1 {
		return nil
	}
	per := tradeCoins / l.UnitCap
	if per < 1 {
		return nil
	}
	var out []Trade
	left := l.Qty
	for left > 0 && len(out) < maxTrades {
		q := left
		if q > per {
			q = per
		}
		out = append(out, Trade{Market: l.Market, Item: l.Item, Qty: q, Unit: l.Unit, UnitCap: l.UnitCap, MaxCoins: q * l.UnitCap})
		left -= q
	}
	return out
}

// BoughtLine is the log line a completed purchase writes.
func BoughtLine(n int, item string, paid, muleNow int) string {
	each := 0
	if n > 0 {
		each = (paid + n/2) / n
	}
	return fmt.Sprintf("market: bought %d %s at %d each (mule now %d)", n, item, each, muleNow)
}
