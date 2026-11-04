package bot

// MarketBulkBuy: a one-shot bulk purchase from the Seers player market, run ON THE MULE on the
// operator's say-so only (never started by anything automatic). The pure rules are in
// internal/scripts/marketbulkplan, whose package doc carries the owner's route ruling.
//
// How it is meant to be used (the operator's steps, in order):
//
//  1. stop MuleReceiver on the mule;
//  2. start MarketBulkBuy on the mule with explicit settings (below) -- first quoteOnly, then the
//     smallest real buy, then the full one;
//  3. wait for its "MARKETBULK DONE" line (it idles at the post after it);
//  4. stop it and start MuleReceiver again.
//
// THE PURCHASE ITSELF IS woodcutrunner.BuyAtMarket, the market path proven live on 2026-09-21
// (rune axes for 150,000). This file only calls it: it guarantees the coins are in the pack
// before it is called, it bounds what may leave at exactly one planned trade, and it judges the
// result from this account's own pack, not from the buyer's verdict.
//
// THE BUDGET IS THE FLEET'S. The spend is committed into the same ledger the ScarabSwarm
// per-account market path reads (scarabswarm_marketledger.go; data/scarabswarm-checkpoints/
// _fleet/ScarabMarketLedger.json), through the same in-process instance, so the owner's
// 5,000,000-coin fleet ceiling covers both. A full bulk trade leaves ScarabSwarm's per-account
// buyer almost nothing; raising stage3MarketBudget (and this bot's budget) is an explicit
// operator decision afterwards.
//
// Settings:
//
//	quantity       int     REQUIRED: how many to buy in the whole run
//	maxUnitPrice   int     REQUIRED (not for quoteOnly): the most any unit may cost
//	unitPrice      int     default maxUnitPrice: what is offered per unit (never above the cap)
//	item           string  default "Chaos rune"
//	seller         string  default "Seers Market"
//	budget         int     default 5,000,000: the fleet-wide market budget (must match
//	                       ScarabSwarm's stage3MarketBudget)
//	tradeCoinLimit int     default 5,000,000: the stall's per-trade ceiling
//	quoteOnly      bool    default false: walk to the stall, ask, report the quote, buy nothing;
//	                       a value that is not a JSON boolean is refused, never read as false
//	runId          string  REQUIRED for a real buy: any run with a runId already started refuses
//	                       (marketbulk_runs.go), so a reconnect or a panel restart never buys twice
//	checkpointDir  string  default data/scarabswarm-checkpoints: where the fleet ledger lives
//	post           string  default "2809,3441" (Catherby bank): where it walks back to
//	bank           string  default "2725,3493" (Seers bank): the bank beside the stall

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/marketbulkplan"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("MarketBulkBuy", func() Bot { return &marketBulkBot{} })
}

// marketBulkQuoteWait is how long quote-only mode listens for the stall's quote after asking.
const marketBulkQuoteWait = 20 * time.Second

type marketBulkBot struct {
	set    marketbulkplan.Settings
	ledger *scarabMarketLedger
	walker *webwalk.Walker
	me     string
	// done is set once the run has finished, for good or not; Loop then idles.
	done   string
	reason string
	start  time.Time
	bought int
	paid   int
}

func (b *marketBulkBot) deps(ctx *Context) *woodcutrunner.Deps {
	return &woodcutrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Walker: b.walker, World: ctx.World(),
	}
}

func (b *marketBulkBot) thiev(ctx *Context) *thieverrunner.Deps {
	return &thieverrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: thieverrunner.Delayer(ctx.Delay), Log: ctx.Log,
	}
}

// marketBulkSettings reads the settings map. Pure apart from the map, so it is unit-tested.
func marketBulkSettings(m map[string]interface{}) marketbulkplan.Settings {
	s := marketbulkplan.Settings{
		Item:           StringSetting(m, "item", ""),
		Quantity:       IntSetting(m, "quantity", 0),
		MaxUnitPrice:   IntSetting(m, "maxUnitPrice", 0),
		UnitPrice:      IntSetting(m, "unitPrice", 0),
		Budget:         IntSetting(m, "budget", fleetplan.DefaultMarketBudget),
		TradeCoinLimit: IntSetting(m, "tradeCoinLimit", marketbulkplan.DefaultTradeCoinLimit),
		Seller:         StringSetting(m, "seller", ""),
		QuoteOnly:      BoolSetting(m, "quoteOnly", false),
	}
	if t, ok := mulereceiverplan.ParsePostTile(StringSetting(m, "post", "")); ok {
		s.Post = t
	}
	if t, ok := mulereceiverplan.ParsePostTile(StringSetting(m, "bank", "")); ok {
		s.Bank = t
	}
	return s.Normalised()
}

func (b *marketBulkBot) finish(ctx *Context, format string, args ...any) {
	b.done = fmt.Sprintf(format, args...)
	ctx.logf("marketbulk: MARKETBULK DONE after %s — %s (bought %d %s for %d coins this run)",
		time.Since(b.start).Round(time.Second), b.done, b.bought, strings.ToLower(b.set.Item), b.paid)
}

func (b *marketBulkBot) OnStart(ctx *Context) error {
	b.start = time.Now()
	b.me = ctx.Username
	b.set = marketBulkSettings(ctx.Settings)
	if v, present := ctx.Settings["quoteOnly"]; present {
		if _, isBool := v.(bool); !isBool {
			// BoolSetting reads "true" (a string) as its default, false: a quote request would
			// have become a real buy.
			b.finish(ctx, "REFUSED: quoteOnly is %v (%T), not a JSON true or false", v, v)
			return nil
		}
	}
	if err := b.set.Validate(); err != nil {
		b.finish(ctx, "REFUSED: %v", err)
		return nil
	}
	if !b.set.QuoteOnly {
		runID := strings.TrimSpace(StringSetting(ctx.Settings, "runId", ""))
		if runID == "" {
			b.finish(ctx, "REFUSED: a real buy needs a runId (any new name), so a restart can never repeat it")
			return nil
		}

		trades, err := marketbulkplan.PlanTrades(b.set.Quantity, b.set.Unit(), b.set.TradeCoinLimit)
		if err != nil {
			b.finish(ctx, "REFUSED: %v", err)
			return nil
		}
		b.ledger = scarabLedgerFor(settings.NewStore(StringSetting(ctx.Settings, "checkpointDir", scarabSwarmCheckpointDir)))
		if b.ledger == nil || b.ledger.loadErr != nil {
			b.finish(ctx, "REFUSED: the fleet market ledger could not be read (%v)", b.ledger.loadErrOrNil())
			return nil
		}
		// The whole run must fit before anything moves: a run that can pay for its first trade
		// and not its second would strand half a plan.
		left := b.ledger.Remaining(b.set.Budget, b.me, time.Now())
		if err := marketbulkplan.CheckBudget(marketbulkplan.TotalCoins(trades), left); err != nil {
			b.finish(ctx, "REFUSED: %v", err)
			return nil
		}
		// Last of the refusals, so a refused request never uses its runId up.
		store := settings.NewStore(StringSetting(ctx.Settings, "checkpointDir", scarabSwarmCheckpointDir))
		if at, err := claimMarketBulkRun(store, runID, time.Now()); err != nil {
			b.finish(ctx, "REFUSED: %v", err)
			return nil
		} else if at != "" {
			b.finish(ctx, "REFUSED: run %q already started at %s; nothing was spent this time. Give a new runId to buy again", runID, at)
			return nil
		}
		ctx.logf("marketbulk: %d %s from %q at %d each (cap %d) in %d trade(s), %d coins; fleet budget %d, %d left",
			b.set.Quantity, strings.ToLower(b.set.Item), b.set.Seller, b.set.Unit(), b.set.MaxUnitPrice,
			len(trades), marketbulkplan.TotalCoins(trades), b.set.Budget, left)
	} else {
		ctx.logf("marketbulk: QUOTE ONLY — asking %q for %d %s, buying nothing", b.set.Seller, b.set.Quantity, strings.ToLower(b.set.Item))
	}
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("marketbulk: no webwalk (%v) — walking with the plain pathfinder", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{Client: w.Client, Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Log: ctx.Log}))
		}
		b.walker = w
	}
	return nil
}

func (l *scarabMarketLedger) loadErrOrNil() error {
	if l == nil {
		return fmt.Errorf("no ledger")
	}
	return l.loadErr
}

func (b *marketBulkBot) OnStop(ctx *Context) {
	if b.ledger != nil {
		b.ledger.Release(b.me)
	}
}

func (b *marketBulkBot) Loop(ctx *Context) error {
	if b.done != "" {
		ctx.Delay(5 * time.Second)
		return nil
	}
	b.run(ctx)
	if b.done == "" {
		b.finish(ctx, "stopped (%s)", b.reason)
	}
	return nil
}

// run is the whole purchase, once. MARKETBULK DONE is logged only at the very end, once the
// account is back at the post (or could not get there), so an operator who waits for it never
// stops the bot mid-walk with the pack in an unknown state.
func (b *marketBulkBot) run(ctx *Context) {
	d := b.deps(ctx)
	if !woodcutrunner.WaitForGuardian(d) {
		return
	}
	// 1. Nothing valuable on the road: bank the whole pack first, at the NEARER of the post's
	// bank and the Seers bank -- a restart at Seers with the trade's coins in the pack must not
	// walk them to Catherby to bank them.
	first := b.set.Post
	if here := mbHere(d); tile.Chebyshev(here, b.set.Bank) < tile.Chebyshev(here, b.set.Post) {
		first = b.set.Bank
	}
	if len(woodcutrunner.PackView(d)) > 0 && !b.bankPack(ctx, d, first, "before leaving") {
		b.finish(ctx, "FAILED: could not bank the pack before walking to Seers; nothing was spent")
		return
	}
	legOut := time.Now()
	if woodcutrunner.TravelTo(d, b.set.Bank, 3) {
		ctx.logf("marketbulk: reached the Seers bank in %s", time.Since(legOut).Round(time.Second))
		if b.set.QuoteOnly {
			b.quote(ctx, d)
		} else {
			b.buyAll(ctx, d)
		}
	} else {
		b.stopWith("FAILED: could not reach the Seers bank; nothing was spent")
	}
	// 6. Back to the post, whatever happened (the item and coins are banked by now, or the
	// run stopped with its reason logged).
	legBack := time.Now()
	if len(woodcutrunner.PackView(d)) > 0 && !b.bankPack(ctx, d, b.set.Bank, "before walking back") &&
		!b.bankPack(ctx, d, b.set.Bank, "before walking back (second try)") {
		// Whatever is in the pack now is the trade's coins or what they bought: never across the
		// road with it (design ruling 6). Stay at Seers and say so.
		b.stopWith("STOPPED AT SEERS: the bank would not take the pack (%v); it was not walked home", woodcutrunner.PackView(d))
		b.finish(ctx, "%s", b.reason)
		return
	}
	if ctx.Stopped() {
		return
	}
	if woodcutrunner.TravelTo(d, b.set.Post, 3) {
		ctx.logf("marketbulk: back at the post in %s", time.Since(legBack).Round(time.Second))
	} else {
		ctx.logf("marketbulk: WARNING: could not walk back to the post (at %d,%d)", mbHere(d).X, mbHere(d).Z)
	}
	if b.reason == "" {
		b.reason = "complete"
	}
	b.finish(ctx, "%s", b.reason)
}

// stopWith records why the run is ending early; the first reason wins.
func (b *marketBulkBot) stopWith(format string, args ...any) {
	if b.reason == "" {
		b.reason = fmt.Sprintf(format, args...)
	}
}

// marketBulkBusyTries is how many times one planned trade asks for the stall's turn before it is
// given up, and marketBulkBusyPause the pause between tries. See buyWaitingAtStall.
const (
	marketBulkBusyTries = 4
	marketBulkBusyPause = 20 * time.Second
)

// buyWaitingAtStall runs buy, and when it only lost the stall's turn (Busy) waits pause and tries
// again, up to tries attempts in all. onBusy is told each wait, numbered from 1, and ends the
// waiting by returning false.
//
// WAIT AT THE STALL, DO NOT WALK HOME. Live 2026-09-25 20:40-21:05, the mule's MarketBulkBuy failed
// seven times in eight with "another account is buying from Seers Market right now" -- which is
// this fleet's own turn lock (woodcutrunner/marketlease.go), never the stall's words -- and every
// failure banked the coins, walked 106 tiles back to Catherby, and was restarted by hand to walk
// 106 tiles out again. The holders were ProgressiveWCFletch rune-axe trips every time (acct159,
// acct167, Acct168 then Acct211, acct135 then acct136). The turn now gives this buyer
// priority over axe buyers; this covers the turn being held by ScarabSwarm, or by a trade that is
// already on the screen when it arrives.
func buyWaitingAtStall(delay func(time.Duration) bool, tries int, pause time.Duration,
	buy func() woodcutrunner.MarketBuyResult, onBusy func(n int, r woodcutrunner.MarketBuyResult) bool) woodcutrunner.MarketBuyResult {
	if tries < 1 {
		tries = 1
	}
	res := buy()
	for n := 1; n < tries && res.Busy && !res.Stopped && !res.Got; n++ {
		if onBusy != nil && !onBusy(n, res) {
			return res
		}
		if delay != nil && !delay(pause) {
			res.Stopped = true
			return res
		}
		res = buy()
	}
	return res
}

// buyAll runs every planned trade: withdraw exactly its coins, buy, bank the result.
func (b *marketBulkBot) buyAll(ctx *Context, d *woodcutrunner.Deps) {
	trades, _ := marketbulkplan.PlanTrades(b.set.Quantity, b.set.Unit(), b.set.TradeCoinLimit)
	for i, t := range trades {
		if ctx.Stopped() {
			return
		}
		left := b.ledger.Remaining(b.set.Budget, b.me, time.Now())
		if err := marketbulkplan.CheckBudget(t.Coins, left); err != nil {
			b.stopWith("REFUSED before trade %d of %d: %v", i+1, len(trades), err)
			return
		}
		if got := b.ledger.Reserve(b.me, t.Coins, b.set.Budget, time.Now(), fleetplan.MarketHoldTTL); got < t.Coins {
			b.ledger.Release(b.me)
			b.stopWith("REFUSED before trade %d of %d: the ledger granted %d of %d coins", i+1, len(trades), got, t.Coins)
			return
		}
		// 2. Exactly this trade's coins, at the bank beside the stall.
		if !b.withdrawCoins(ctx, d, t.Coins) {
			b.ledger.Release(b.me)
			b.bankPack(ctx, d, b.set.Bank, "after a failed withdrawal")
			b.stopWith("FAILED at trade %d of %d: could not withdraw exactly %d coins; nothing was spent", i+1, len(trades), t.Coins)
			return
		}
		// 3. The proven buyer, listening for the stall's quote alongside.
		itemBefore, coinsBefore := mbPackNamed(d, b.set.Item), woodcutrunner.CoinsHeld(d)
		stopQuotes, lastQuote := b.listenQuotes(ctx)
		tradeStart := time.Now()
		res := buyWaitingAtStall(ctx.Delay, marketBulkBusyTries, marketBulkBusyPause, func() woodcutrunner.MarketBuyResult {
			return woodcutrunner.BuyAtMarket(d, woodcutrunner.MarketBuyOptions{
				Stand: b.set.Bank, Item: b.set.Item, Qty: t.Qty, Price: t.Unit, MaxCoins: t.Coins,
				Seller: b.set.Seller, Me: b.me,
			})
		}, func(n int, r woodcutrunner.MarketBuyResult) bool {
			// Waiting can outlast the ledger hold (scarabplan.MarketHoldTTL), so renew it; a
			// budget that no longer covers the trade ends the wait instead.
			if got := b.ledger.Reserve(b.me, t.Coins, b.set.Budget, time.Now(), fleetplan.MarketHoldTTL); got < t.Coins {
				ctx.logf("marketbulk: trade %d of %d — the ledger now grants %d of %d coins; not waiting for the stall", i+1, len(trades), got, t.Coins)
				return false
			}
			ctx.logf("marketbulk: trade %d of %d — %s; waiting at the stall with the coins (%d of %d)",
				i+1, len(trades), r.Reason, n, marketBulkBusyTries-1)
			return true
		})
		stopQuotes()
		if q, ok := lastQuote(); ok {
			ctx.logf("marketbulk: the stall quoted %d x %s at %d each (%d total)", q.Qty, q.Item, q.Unit, q.Total)
		}
		// Judged by the pack: what arrived, what left.
		mbWait(ctx, 3*time.Second, func() bool { return mbPackNamed(d, b.set.Item) > itemBefore })
		got := mbPackNamed(d, b.set.Item) - itemBefore
		paid := coinsBefore - woodcutrunner.CoinsHeld(d)
		outcome := marketbulkplan.JudgeTrade(t, got, paid)
		ctx.logf("marketbulk: trade %d of %d — %s: +%d %s for %d coins in %s (buyer said: got=%v %s)",
			i+1, len(trades), outcome, got, strings.ToLower(b.set.Item), paid, time.Since(tradeStart).Round(time.Second), res.Got, res.Reason)
		if paid > 0 || got > 0 {
			st, p := b.ledger.Commit(fleetplan.MarketPurchase{Account: b.me, Market: b.set.Seller, Item: b.set.Item, Qty: got, Paid: paid, At: time.Now()})
			if err := b.ledger.save(st); err != nil {
				ctx.logf("marketbulk: WARNING: the fleet ledger could not be saved: %v", err)
			}
			ctx.logf("marketbulk: MARKET RUNE LEDGER — %d %s from %s for %d coins; the fleet has spent %d of its %d market budget in %d purchases",
				got, b.set.Item, b.set.Seller, paid, p.SpentAfter, b.set.Budget, st.Buys)
			b.bought += got
			b.paid += paid
		} else {
			b.ledger.Release(b.me)
		}
		// 4. The item and the change go straight into the bank here.
		if !b.bankPack(ctx, d, b.set.Bank, "after the trade") {
			ctx.logf("marketbulk: WARNING: could not bank after trade %d; it stays in the pack", i+1)
		}
		if !marketbulkplan.Continue(outcome) {
			b.stopWith("STOPPED at trade %d of %d: %s (%s)", i+1, len(trades), outcome, res.Reason)
			return
		}
	}
}

// quote asks the stall and reports what it says, trading nothing.
func (b *marketBulkBot) quote(ctx *Context, d *woodcutrunner.Deps) {
	if at, ok := mbSellerAt(d, b.set.Seller); ok {
		ctx.logf("marketbulk: %q is at %d,%d (%d tiles away)", b.set.Seller, at.X, at.Z, tile.Chebyshev(mbHere(d), at))
		if tile.Chebyshev(mbHere(d), at) > 2 {
			woodcutrunner.WalkNear(d, at, 2)
		}
	} else {
		ctx.logf("marketbulk: %q is not among the visible players", b.set.Seller)
	}
	stop, last := b.listenQuotes(ctx)
	defer stop()
	line := woodcutplan.MarketAsk(b.set.Item, b.set.Quantity)
	if err := actions.SayPublic(ctx.Session, line); err != nil {
		b.stopWith("QUOTE FAILED: could not say %q: %v", line, err)
		return
	}
	ctx.logf("marketbulk: said %q; listening %s", line, marketBulkQuoteWait)
	ctx.DelayUntil(func() bool { _, ok := last(); return ok }, marketBulkQuoteWait)
	q, ok := last()
	if !ok {
		b.stopWith("QUOTE: %q gave no quote within %s", b.set.Seller, marketBulkQuoteWait)
		return
	}
	verdict := "within the cap"
	if err := marketbulkplan.CheckQuote(q.Unit, b.set.MaxUnitPrice); err != nil {
		verdict = err.Error()
	} else if b.set.MaxUnitPrice <= 0 {
		verdict = "no cap configured"
	}
	b.stopWith("QUOTE: %d x %s at %d each = %d coins (%s); nothing was traded", q.Qty, q.Item, q.Unit, q.Total, verdict)
}

// listenQuotes records the stall's quotes until the returned stop is called.
func (b *marketBulkBot) listenQuotes(ctx *Context) (stop func(), last func() (fleetplan.MarketQuote, bool)) {
	var mu sync.Mutex
	var q fleetplan.MarketQuote
	heard := false
	seller := strings.ToLower(strings.TrimSpace(b.set.Seller))
	cancel := ctx.Session.State().Subscribe(func(ev gamestate.Event) {
		if ev.Kind != gamestate.EventChatMessage || (ev.ChatType != gamestate.ChatPublic && ev.ChatType != gamestate.ChatPublicMod) {
			return
		}
		if strings.ToLower(strings.TrimSpace(ev.Username)) != seller {
			return
		}
		ctx.logf("marketbulk: %s said %q", b.set.Seller, ev.Text)
		if got, ok := fleetplan.ParseMarketQuote(ev.Text, b.set.Item); ok {
			mu.Lock()
			q, heard = got, true
			mu.Unlock()
		}
	})
	return cancel, func() (fleetplan.MarketQuote, bool) {
		mu.Lock()
		defer mu.Unlock()
		return q, heard
	}
}

// withdrawCoins opens the bank beside the stall and draws exactly n coins into an empty pack.
func (b *marketBulkBot) withdrawCoins(ctx *Context, d *woodcutrunner.Deps, n int) bool {
	bank, ok := thieverrunner.OpenBankAt(b.thiev(ctx), b.set.Bank)
	if !ok {
		return false
	}
	defer func() { _ = bank.Close(); ctx.Delay(600 * time.Millisecond) }()
	w := woodcutrunner.Waiter(d)
	if len(woodcutrunner.PackView(d)) > 0 {
		_ = bank.BatchDepositInventory(w)
		mbWait(ctx, 5*time.Second, func() bool { return len(woodcutrunner.PackView(d)) == 0 })
	}
	var coinsID int32 = -1
	have := 0
	for _, it := range bank.Items() {
		if o, ok := ctx.Config.Obj(int(it.ID)); ok && !o.Noted() && strings.EqualFold(o.Name, freshstart.Coins) {
			coinsID, have = it.ID, int(it.Count)
		}
	}
	if coinsID < 0 || have < n {
		ctx.logf("marketbulk: the bank holds %d coins, %d needed", have, n)
		return false
	}
	if err := bank.WithdrawX(coinsID, int32(n), w); err != nil {
		ctx.logf("marketbulk: withdrawing %d coins: %v", n, err)
	}
	mbWait(ctx, 5*time.Second, func() bool { return woodcutrunner.CoinsHeld(d) >= n })
	if held := woodcutrunner.CoinsHeld(d); held != n {
		ctx.logf("marketbulk: the pack holds %d coins, not exactly %d — refusing to trade", held, n)
		return false
	}
	ctx.logf("marketbulk: withdrew exactly %d coins at the Seers bank", n)
	return true
}

// bankPack deposits the whole pack at the bank on tile at and confirms it is empty.
func (b *marketBulkBot) bankPack(ctx *Context, d *woodcutrunner.Deps, at tile.Tile, why string) bool {
	if tile.Chebyshev(mbHere(d), at) > 6 && !woodcutrunner.TravelTo(d, at, 3) {
		return false
	}
	bank, ok := thieverrunner.OpenBankAt(b.thiev(ctx), at)
	if !ok {
		return false
	}
	_ = bank.BatchDepositInventory(woodcutrunner.Waiter(d))
	empty := mbWait(ctx, 5*time.Second, func() bool { return len(woodcutrunner.PackView(d)) == 0 })
	_ = bank.Close()
	ctx.Delay(600 * time.Millisecond)
	if empty {
		ctx.logf("marketbulk: banked the pack at %d,%d %s", at.X, at.Z, why)
	}
	return empty
}

func mbWait(ctx *Context, timeout time.Duration, cond func() bool) bool {
	return ctx.DelayUntil(cond, timeout)
}

func mbHere(d *woodcutrunner.Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// packNamed counts an item in the pack by name, notes included.
func mbPackNamed(d *woodcutrunner.Deps, name string) int {
	n := 0
	for _, it := range woodcutrunner.PackView(d) {
		if strings.EqualFold(strings.TrimSpace(it.Name), strings.TrimSpace(name)) {
			n += it.Count
		}
	}
	return n
}

func mbSellerAt(d *woodcutrunner.Deps, name string) (tile.Tile, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, p := range d.Session.State().Players() {
		if strings.ToLower(strings.TrimSpace(p.Name)) == want {
			return tile.Tile{X: p.X, Z: p.Z, Level: p.Level}, true
		}
	}
	return tile.Tile{}, false
}
