package fleetrunner

// Buying runes from a player market stall (the Seers Market) through the woodcutters' buyer,
// which is the one market path in this repo proven LIVE: run/fleet.log, 2026-09-21 21:25-21:26,
// "bought 1 rune axe for 150000 coins" (Acct170, Acct176).
//
// WHY NOT BuyFromMarket (market.go). That buyer was written before anyone had heard a stall
// speak, and it contradicts the protocol the stalls turned out to announce once the fleet could
// hear public chat (COLLAB_ARCHIVE, 82101276; woodcutrunner/market.go's header): it waits for the
// SELLER to open the trade, and it waits for the item before putting coins up. The stall does
// neither -- "To buy from me: say 'buying 100 iron ore', then trade me and put up coins" -- so
// both sides wait for the other. Those are exactly the two deadlocks woodcutrunner fixed on
// 2026-09-21 (c6054ef5, 5bd4e576). BuyFromMarket stays for the lab's scripted market account in
// cmd/scarabstage3test, which speaks the old imagined protocol.
//
// woodcutrunner.BuyAtMarket is used READ-ONLY: its file is claimed by the fleet owner's own
// session, so nothing here changes it. What this wrapper adds is only what a rune purchase needs
// that an axe purchase did not:
//
//   - THE PRICE COMES FROM THE PAGE, THEN FROM THE STALL. BuyAtMarket offers exactly Price x Qty
//     and declines any screen that differs, which is right, but the page's last-sold price need
//     not be what the stall quotes today. So this listens to the stall's public reply while the
//     buyer runs, and if the purchase failed and the stall quoted a different unit price at or
//     under the operator's cap, it tries ONCE more at the stall's own number
//     (scarabplan.DecideRequote). It never chases a price a second time.
//   - A LOST TURN IS NOT A REFUSAL. BuyAtMarket reports Busy when another account held the
//     stall's turn (the stall serves one buyer at a time, and its turn lock is shared with the
//     woodcutters); the caller backs off minutes, not the half hour a real refusal earns.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// MarketRuneOrder is one rune purchase at a stall.
type MarketRuneOrder struct {
	// Market is the stall's account name, which is also the only partner a trade may be
	// completed with.
	Market string
	// Stand is where the stall stands (scarabplan.DefaultMarketPlaces).
	Stand tile.Tile
	Item  string
	Qty   int
	// Unit is the per-unit price to offer first (the page's last sold); UnitCap the most a
	// re-offer at the stall's own quote may pay; MaxCoins the hard ceiling on the whole lot.
	Unit, UnitCap, MaxCoins int
	// Me is this account's own name, so the stall's "Trade me, <name>." is recognised as ours.
	Me string
}

// MarketRuneResult is what a purchase did, judged by this account's own pack.
type MarketRuneResult struct {
	Got     int
	Paid    int
	Unit    int
	Reason  string
	Busy    bool
	Stopped bool
	// Quote is the stall's own quote when one was heard.
	Quote      fleetplan.MarketQuote
	QuoteHeard bool
	Retried    bool
}

// woodDeps adapts d to woodcutrunner.Deps, field for field.
func woodDeps(d *Deps) *woodcutrunner.Deps {
	return &woodcutrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: woodcutrunner.Delayer(d.Delay),
		Walker: d.Walker, Log: d.Log,
	}
}

// BuyRunesAtMarket buys o.Qty of o.Item from the stall o.Market. The account must already be
// carrying the coins (BuyAtMarket refuses before walking otherwise).
func BuyRunesAtMarket(d *Deps, o MarketRuneOrder) MarketRuneResult {
	var mu sync.Mutex
	var quote fleetplan.MarketQuote
	heard := false
	// said keeps the stall's own lines (a few), so an order that ends with no quote heard can say
	// what the stall DID say -- the round-2 budget run's re-offer never happened and nothing
	// showed why.
	var said []string
	cancel := d.Session.State().Subscribe(func(ev gamestate.Event) {
		if ev.Kind != gamestate.EventChatMessage || (ev.ChatType != gamestate.ChatPublic && ev.ChatType != gamestate.ChatPublicMod) {
			return
		}
		if !MarketNameMatch(ev.Username, o.Market) {
			return
		}
		mu.Lock()
		if len(said) < 8 {
			said = append(said, ev.Text)
		}
		mu.Unlock()
		if q, ok := fleetplan.ParseMarketQuote(ev.Text, o.Item); ok {
			mu.Lock()
			quote, heard = q, true
			mu.Unlock()
		}
	})
	defer cancel()

	attempt := func(unit int) woodcutrunner.MarketBuyResult {
		return woodcutrunner.BuyAtMarket(woodDeps(d), woodcutrunner.MarketBuyOptions{
			Stand: o.Stand, Item: o.Item, Qty: o.Qty, Price: unit, MaxCoins: o.MaxCoins,
			Seller: o.Market, Me: o.Me,
		})
	}

	itemBefore, coinsBefore := CountPackNamed(d, o.Item), CoinsHeld(d)
	out := MarketRuneResult{Unit: o.Unit}
	res := attempt(o.Unit)
	if !res.Got && !res.Stopped && !res.Busy {
		mu.Lock()
		q, h := quote, heard
		mu.Unlock()
		if dec := fleetplan.DecideRequote(o.Unit, q, h, o.Qty, o.UnitCap); dec.Retry && dec.Unit*o.Qty <= o.MaxCoins && dec.Unit*o.Qty <= CoinsHeld(d) {
			d.Logf("market: %s (%s)", dec.Why, res.Reason)
			out.Retried, out.Unit = true, dec.Unit
			res = attempt(dec.Unit)
		} else if h {
			d.Logf("market: not re-offering: %s (quote %d x %s at %d each; %d coins held, ceiling %d)", dec.Why, q.Qty, q.Item, q.Unit, CoinsHeld(d), o.MaxCoins)
		} else {
			mu.Lock()
			lines := append([]string(nil), said...)
			mu.Unlock()
			d.Logf("market: no quote heard from %s for %s; it said %q", o.Market, strings.ToLower(o.Item), lines)
		}
	}
	mu.Lock()
	out.Quote, out.QuoteHeard = quote, heard
	mu.Unlock()
	// Judged by the pack, never by the buyer's own verdict alone: a noted or partial arrival
	// still counts what arrived, and a "Got" with nothing new in the pack counts nothing.
	WaitFor(d, "the market purchase to land", 3*time.Second, func() bool { return CountPackNamed(d, o.Item) > itemBefore })
	out.Got = CountPackNamed(d, o.Item) - itemBefore
	out.Paid = coinsBefore - CoinsHeld(d)
	out.Busy, out.Stopped = res.Busy, res.Stopped
	if out.Got <= 0 {
		out.Reason = res.Reason
		if out.Reason == "" {
			out.Reason = "nothing arrived"
		}
	}
	return out
}
