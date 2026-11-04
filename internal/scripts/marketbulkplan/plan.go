// Package marketbulkplan is the pure half of MarketBulkBuy (internal/bot/marketbulk.go): a
// one-shot, operator-triggered bulk purchase from the Seers player market by the mule, to stock
// the fleet with chaos runes (design ruling, 2026-09-24: the Seers Market held 732,139 chaos at
// 95 gp, with a 5,000,000-coins-per-trade limit; one full trade is 52,450 chaos for 4,982,750).
//
// The shape is the owner's route ruling, and every step of it is here or in the bot:
//
//  1. walk from the Catherby post to Seers carrying NOTHING valuable (the pack is banked first);
//  2. withdraw exactly ONE trade's coins at the Seers bank, beside the stall -- the coins never
//     cross the Catherby-Seers road;
//  3. buy at the stall through woodcutrunner.BuyAtMarket, the live-proven market path;
//  4. deposit the item and any unspent coins at the Seers bank (the bank is the account's, so
//     Catherby kits can use it);
//  5. repeat 2-4 per trade when the quantity needs more than one trade;
//  6. walk back to the post and stop, so the operator restarts MuleReceiver.
//
// What this package decides, with no client and no I/O: whether a request may run at all
// (Validate), how a quantity splits under the per-trade coin limit (PlanTrades), whether the fleet
// budget has room (CheckBudget), whether a stall quote is acceptable (CheckQuote), and what one
// trade's before/after readings mean (JudgeTrade).
package marketbulkplan

import (
	"errors"
	"fmt"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// DefaultSeller is the Seers stall's account name.
const DefaultSeller = "Seers Market"

// DefaultItem is what users ruled the fleet should buy.
const DefaultItem = "Chaos rune"

// DefaultTradeCoinLimit is the Seers Market's own per-trade ceiling (owner, 2026-09-24).
const DefaultTradeCoinLimit = 5_000_000

// MaxTrades bounds one run: a request that would need more trades than this is almost certainly
// a typo in the quantity, and refusing it costs one re-entry of the settings.
const MaxTrades = 10

// SeersBank is the Seers' Village bank floor (woodcutplan.Seers), which the stall stands beside.
var SeersBank = tile.Tile{X: 2725, Z: 3493}

// CatherbyPost is the Catherby bank floor the live mule works from (mulereceiverplan's
// CatherbyBankTile).
var CatherbyPost = tile.Tile{X: 2809, Z: 3441}

// Settings is one bulk purchase as the operator configured it.
type Settings struct {
	Item string
	// Quantity is how many to buy in the whole run. Required.
	Quantity int
	// MaxUnitPrice is the most any unit may cost. Required.
	MaxUnitPrice int
	// UnitPrice is what is offered per unit; 0 means MaxUnitPrice. Never above MaxUnitPrice.
	UnitPrice int
	// Budget is the fleet-wide market budget the ledger enforces (scarabplan.DefaultMarketBudget
	// unless raised explicitly).
	Budget int
	// TradeCoinLimit is the most coins one trade may carry.
	TradeCoinLimit int
	Seller         string
	// QuoteOnly walks to the stall, asks, reports the quote and buys nothing.
	QuoteOnly bool
	// Post is where the mule is walked back to; Bank the bank beside the stall.
	Post, Bank tile.Tile
}

// Unit is the per-unit price actually offered.
func (s Settings) Unit() int {
	if s.UnitPrice > 0 {
		return s.UnitPrice
	}
	return s.MaxUnitPrice
}

// Normalised fills the defaults that are safe to default. Quantity and MaxUnitPrice are never
// defaulted: without them there is nothing safe to buy.
func (s Settings) Normalised() Settings {
	s.Item = strings.TrimSpace(s.Item)
	if s.Item == "" {
		s.Item = DefaultItem
	}
	s.Seller = strings.TrimSpace(s.Seller)
	if s.Seller == "" {
		s.Seller = DefaultSeller
	}
	if s.TradeCoinLimit <= 0 {
		s.TradeCoinLimit = DefaultTradeCoinLimit
	}
	if s.Post == (tile.Tile{}) {
		s.Post = CatherbyPost
	}
	if s.Bank == (tile.Tile{}) {
		s.Bank = SeersBank
	}
	return s
}

// The refusals, as values so the tests and the bot's log name the same thing.
var (
	ErrNoQuantity    = errors.New("no quantity configured: set an explicit quantity")
	ErrNoPriceCap    = errors.New("no price cap configured: set maxUnitPrice")
	ErrOverCap       = errors.New("the offered unit price is above the configured cap")
	ErrNoBudget      = errors.New("no fleet market budget configured")
	ErrBudgetSpent   = errors.New("the fleet market budget does not have room for this purchase")
	ErrTooManyTrades = errors.New("the quantity needs more trades than one run allows")
	ErrUnitOverLimit = errors.New("one unit costs more than the per-trade coin limit")
	ErrQuoteOverCap  = errors.New("the stall quoted a unit price above the cap")
)

// Validate refuses a request that has nothing safe to buy with. QuoteOnly still needs a quantity
// (it is what is asked for) but no price cap, since nothing is paid.
func (s Settings) Validate() error {
	if s.Quantity <= 0 {
		return ErrNoQuantity
	}
	if s.QuoteOnly {
		return nil
	}
	if s.MaxUnitPrice <= 0 {
		return ErrNoPriceCap
	}
	if s.UnitPrice > s.MaxUnitPrice {
		return fmt.Errorf("%w (%d > %d)", ErrOverCap, s.UnitPrice, s.MaxUnitPrice)
	}
	if s.Budget <= 0 {
		return ErrNoBudget
	}
	return nil
}

// Trade is one trade of a run.
type Trade struct {
	Qty, Unit, Coins int
}

// PlanTrades splits qty at unit per item into trades of at most limit coins each, as few as
// possible and as even as possible (so no tail trade is a handful of runes that costs a whole
// round trip at the stall for nothing).
func PlanTrades(qty, unit, limit int) ([]Trade, error) {
	if qty <= 0 {
		return nil, ErrNoQuantity
	}
	if unit <= 0 {
		return nil, ErrNoPriceCap
	}
	if limit <= 0 {
		limit = DefaultTradeCoinLimit
	}
	per := limit / unit
	if per < 1 {
		return nil, ErrUnitOverLimit
	}
	n := (qty + per - 1) / per
	if n > MaxTrades {
		return nil, fmt.Errorf("%w (%d trades, at most %d)", ErrTooManyTrades, n, MaxTrades)
	}
	out := make([]Trade, 0, n)
	base, extra := qty/n, qty%n
	for i := 0; i < n; i++ {
		q := base
		if i < extra {
			q++
		}
		out = append(out, Trade{Qty: q, Unit: unit, Coins: q * unit})
	}
	return out, nil
}

// TotalCoins is what a plan costs at most.
func TotalCoins(trades []Trade) int {
	n := 0
	for _, t := range trades {
		n += t.Coins
	}
	return n
}

// CheckBudget refuses a trade the fleet budget has no room for. remaining is the ledger's
// Remaining for this account (budget less spent less others' holds).
func CheckBudget(coins, remaining int) error {
	if coins > remaining {
		return fmt.Errorf("%w (%d coins wanted, %d left)", ErrBudgetSpent, coins, remaining)
	}
	return nil
}

// CheckQuote refuses a stall quote above the cap. A quote is informational for a buy (the buyer
// offers exactly its own price and declines any other screen), but quote-only mode reports it,
// and a quote over the cap is the clearest possible "do not run the real buy".
func CheckQuote(unit, maxUnit int) error {
	if maxUnit > 0 && unit > maxUnit {
		return fmt.Errorf("%w (%d > %d)", ErrQuoteOverCap, unit, maxUnit)
	}
	return nil
}

// Outcome is what one trade's readings mean.
type Outcome int

const (
	// Bought: exactly the quantity arrived for exactly the coins.
	Bought Outcome = iota
	// Nothing: nothing arrived and nothing was paid (a refusal, a decline, a stall away).
	Nothing
	// Partial: something arrived, but not exactly what was agreed. The run stops.
	Partial
	// Lost: coins left and nothing arrived. Should be impossible through BuyAtMarket's guards;
	// the run stops and the operator must look.
	Lost
)

func (o Outcome) String() string {
	switch o {
	case Bought:
		return "bought"
	case Nothing:
		return "nothing crossed"
	case Partial:
		return "PARTIAL"
	case Lost:
		return "COINS LOST"
	}
	return "unknown"
}

// JudgeTrade reads one trade from the account's own pack: got is the item count gained, paid the
// coins that left.
func JudgeTrade(t Trade, got, paid int) Outcome {
	switch {
	case got == t.Qty && paid == t.Coins:
		return Bought
	case got <= 0 && paid <= 0:
		return Nothing
	case got <= 0 && paid > 0:
		return Lost
	default:
		return Partial
	}
}

// Continue reports whether a run may start its next trade after an outcome: only a clean buy.
func Continue(o Outcome) bool { return o == Bought }
