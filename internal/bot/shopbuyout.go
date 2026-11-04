package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/shopplan"
	"rsb0t-go/internal/scripts/shoprunner"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("ShopBuyout", func() Bot { return &shopBuyoutBot{} })
}

// ShopBuyoutSettings is the script's declared settings schema, resolved through
// internal/settings so a value arriving as a bare string from a panel is typed, options are
// matched ignoring case, and a missing key takes the default. The keys mirror the TS
// manifest's own SHOPBUYOUT_SETTINGS (shop, budgetGp, perTripGp, stopFloorGp, buyItems,
// recheckSeconds) plus this codebase's own bank-booth and verbosity conventions.
var ShopBuyoutSettings = settings.Schema{
	"shop": {
		Type:    settings.TypeString,
		Default: shopplan.Shops[0].Label,
		Options: shopplan.ShopLabels(),
		Label:   "Shop",
		Help:    "the shop to buy out — its keeper, trade tile and bank are baked into the preset",
	},
	"buyItems": {
		Type:    settings.TypeList,
		Default: []string{},
		Options: shopplan.AllItemNames(),
		Label:   "Items to buy (empty = all stock)",
		Help:    "most valuable first under the coin budget; leave empty to buy the whole shop out. Names the chosen shop does not stock are reported and dropped",
	},
	"budgetGp": {
		Type:    settings.TypeNumber,
		Default: float64(shopplan.DefaultBudgetGp),
		Min:     settings.Float(100),
		Label:   "Total gp to spend",
		Help:    "the session budget; the run stops cleanly once roughly this much has been spent",
	},
	"perTripGp": {
		Type:    settings.TypeNumber,
		Default: float64(shopplan.DefaultPerTripGp),
		Min:     settings.Float(100),
		Label:   "Gp per bank trip",
	},
	"stopFloorGp": {
		Type:    settings.TypeNumber,
		Default: float64(shopplan.DefaultStopFloorGp),
		Min:     settings.Float(0),
		Label:   "Stop below bank gp",
	},
	"recheckSeconds": {
		Type:    settings.TypeNumber,
		Default: float64(shopplan.DefaultRecheckSeconds),
		Min:     settings.Float(5),
		Max:     settings.Float(600),
		Label:   "Restock recheck (s)",
		Help:    "how long to wait at the keeper once the chosen stock is drained. The engine restocks one unit per item on every world tick divisible by that item's rate, so a slow row is genuinely slow",
	},
	"bankBooth": {
		Type:    settings.TypeString,
		Default: shopplan.BoothName,
		Label:   "Bank booth",
		Help:    "the bank loc's display name",
	},
	"logDetail": {
		Type:    settings.TypeString,
		Default: "Normal",
		Options: []string{"Normal", "Verbose"},
		Label:   "Log detail",
	},
}

// shopBuyoutBot ports rsb0t's public-catalogue ShopBuyout.ts
// (src/bot/scripts/ShopBuyout/) to this codebase's three-layer recipe — see
// docs/script_porting_framework.md. Every Loop call re-derives the stage from live state
// (shopplan.DecideStage) rather than trusting the TS class's own remembered `phase` field;
// the only things carried across calls are what that class also kept: the session spend, the
// haul tally and the trip counters.
//
// No mule: confirmed by reading the whole TS source — ShopBuyout.ts imports no donorPlan or
// muleProtocol, and the bank is the only destination for every trip.
//
// No primary skill entry in skills.go, deliberately: buying from a shop trains nothing, so
// this script belongs with MuleReceiver in having no single trained skill rather than a
// guessed one.
type shopBuyoutBot struct {
	settings shopplan.Settings
	shop     shopplan.Shop
	chosen   map[string]bool
	// minUnit is the cheapest chosen item's price at baseline stock: what the run must be
	// carrying for a trip to the keeper to be able to buy anything at all.
	minUnit int32

	// walker takes the long way home when the same-level pathfinder cannot plan a route —
	// after a death or a fail-teleport. Nil if webwalk could not be built; the script then
	// degrades to same-level pathfinding rather than failing to start.
	walker *webwalk.Walker

	// undepositable are object ids the bank refused to take, remembered so a bank trip stops
	// re-trying them. Owned here rather than in the runner so it survives across trips, the
	// same way donorrunner's KnownUntradable is owned by its bot. See the HerbCleaner port's
	// findings for the live incident that made this necessary.
	undepositable map[int32]bool

	// needCoins is set when the keeper's shelf was stocked but the purse could not afford a
	// single unit at its CURRENT price — see shopplan.Snapshot.NeedCoins for the live failure
	// this prevents (a run stood at the shelf waiting for a restock that would not have
	// helped, because the problem was the 7gp it had left, not the stock). A bank trip that
	// actually draws coins clears it.
	needCoins bool

	sessionSpent int32
	haul         map[int32]int32
	trips        int
	stage        string
	startedAt    time.Time
	warnedAt     map[string]time.Time
}

func (b *shopBuyoutBot) deps(ctx *Context) *shoprunner.Deps {
	return &shoprunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Walker:     b.walker,
	}
}

func (b *shopBuyoutBot) budgetLeft() int32 {
	left := b.settings.BudgetGp - b.sessionSpent
	if left < 0 {
		return 0
	}
	return left
}

func (b *shopBuyoutBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("shopbuyout: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	bag := ctx.ResolveSettings(ShopBuyoutSettings)
	b.settings = shopplan.Settings{
		Shop:           bag.Str("shop", shopplan.Shops[0].Label),
		BuyItems:       bag.List("buyItems", nil),
		BudgetGp:       int32(bag.Num("budgetGp", float64(shopplan.DefaultBudgetGp))),
		PerTripGp:      int32(bag.Num("perTripGp", float64(shopplan.DefaultPerTripGp))),
		StopFloorGp:    int32(bag.Num("stopFloorGp", float64(shopplan.DefaultStopFloorGp))),
		RecheckSeconds: bag.Int("recheckSeconds", shopplan.DefaultRecheckSeconds),
		BankBooth:      bag.Str("bankBooth", shopplan.BoothName),
		LogDetail:      bag.Str("logDetail", "Normal"),
	}.Normalised()

	shop, ok := shopplan.ShopByLabel(b.settings.Shop)
	if !ok {
		return fmt.Errorf("shopbuyout: %q is not a known shop preset", b.settings.Shop)
	}
	b.shop = shop

	// An item name the run did not actually take is worth saying out loud, because the silent
	// outcome is a run far WIDER and more expensive than the operator asked for: an empty
	// selection means "buy the whole shop out". Two ways a name is lost and this catches both
	// — a plain typo, and a real item that belongs to a DIFFERENT preset than the one chosen.
	if dropped := shopplan.DroppedItems(shop, ctx.SettingsBag().List("buyItems", nil)); len(dropped) > 0 {
		ctx.logf("shopbuyout: WARNING: %s does not stock %s — ignoring it; this shop sells: %s",
			shop.Keeper, strings.Join(dropped, ", "), strings.Join(shop.ItemNames(), ", "))
	}

	b.chosen = b.settings.Chosen()
	b.minUnit = shopplan.MinUnitPrice(shop, b.chosen)

	// The same construction barbarian.go/gemcutter.go/herbcleaner.go already use. A failure
	// here is not fatal: the script still works walking between its own bank and shop, it just
	// cannot get home from across the map.
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("shopbuyout: no webwalk (%v) — a long walk back will be limited to same-level pathfinding", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	b.undepositable = map[int32]bool{}
	b.haul = map[int32]int32{}
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()

	which := "every item it stocks"
	if len(b.settings.BuyItems) > 0 {
		which = strings.Join(b.settings.BuyItems, ", ")
	}
	ctx.logf("shopbuyout: buying %s from %s — budget %dgp, %dgp per trip, stopping below %dgp in the bank",
		which, b.shop.Keeper, b.settings.BudgetGp, b.settings.PerTripGp, b.settings.StopFloorGp)
	return nil
}

func (b *shopBuyoutBot) vlog(ctx *Context, format string, args ...interface{}) {
	if b.settings.Verbose() {
		ctx.logf("shopbuyout: "+format, args...)
	}
}

func (b *shopBuyoutBot) warn(ctx *Context, key string, every time.Duration, format string, args ...interface{}) {
	if time.Since(b.warnedAt[key]) < every {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("shopbuyout: WARNING: "+format, args...)
}

func (b *shopBuyoutBot) setStage(ctx *Context, stage shopplan.Stage) {
	if string(stage) != b.stage {
		b.stage = string(stage)
		b.vlog(ctx, "-> %s", shopplan.DescribeStage(stage))
	}
}

func (b *shopBuyoutBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !shoprunner.WaitForGuardian(d) {
		return nil
	}
	// A death here drops the account near Lumbridge and the ordinary bank/travel dispatch
	// walks back, the same reasoning every sibling script's doc comment gives. Still clear the
	// one-way dead latch so a dashboard does not show dead=true forever.
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	stage := shopplan.DecideStage(shopplan.Snapshot{
		BudgetLeft:   b.budgetLeft(),
		CoinsHeld:    shoprunner.CoinsHeld(d),
		PackFull:     shoprunner.PackFull(d),
		MinUnitPrice: b.minUnit,
		NeedCoins:    b.needCoins,
	})
	b.setStage(ctx, stage)

	switch stage {
	case shopplan.StageBank:
		return b.bankStep(ctx, d)
	case shopplan.StageBuy:
		b.buyStep(ctx, d)
	}
	return nil
}

// bankStep deposits the haul, then asks shopplan whether the run should stop or draw more
// coins. The order matters and is the TypeScript's own: a run that has spent its budget still
// banks what it bought before it stops.
func (b *shopBuyoutBot) bankStep(ctx *Context, d *shoprunner.Deps) error {
	bank, ok := shoprunner.OpenBank(d, b.shop, b.settings.BankBooth)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return nil
	}
	defer func() { _ = bank.Close() }()

	// Coins are kept back deliberately, matching ShopBuyout.ts's own
	// depositAllMatching(name => name !== 'Coins'): depositing them would mean withdrawing
	// them again two lines later.
	keep := map[int32]bool{shopplan.CoinsID: true}
	for id := range b.undepositable {
		keep[id] = true
	}
	deposited, stuck := shoprunner.DepositAllExceptIDs(d, bank, keep)
	for _, id := range stuck {
		if b.undepositable[id] {
			continue
		}
		b.undepositable[id] = true
		ctx.logf("shopbuyout: %s will not leave the pack — not retrying it on later trips", shoprunner.DescribeItem(d, id))
	}

	bankCoins := shoprunner.BankCount(bank, shopplan.CoinsID)
	coinsHeld := shoprunner.CoinsHeld(d)
	decision := shopplan.DecideBank(bankCoins, coinsHeld, b.settings.PerTripGp, b.budgetLeft(), b.settings.StopFloorGp)

	if deposited > 0 {
		b.trips++
		ctx.logf("shopbuyout: bank trip %d — deposited %d item kind(s); bank holds %dgp, carrying %dgp",
			b.trips, deposited, bankCoins, coinsHeld)
	}
	if decision.Stop {
		// Returning an error is how a Bot ends its own run in this codebase; the Runner
		// reports it as the stop reason.
		return fmt.Errorf("shopbuyout: %s — spent %dgp of a %dgp budget over %d bank trip(s)",
			decision.Reason, b.sessionSpent, b.settings.BudgetGp, b.trips)
	}
	if decision.Withdraw > 0 {
		got := shoprunner.WithdrawCoins(d, bank, decision.Withdraw)
		if got == 0 {
			b.warn(ctx, "coin-withdraw", 60*time.Second, "asked the bank for %dgp and got none — retrying", decision.Withdraw)
			ctx.Delay(2 * time.Second)
			return nil
		}
		// The purse is funded again, so the keeper is worth another visit.
		b.needCoins = false
		b.vlog(ctx, "withdrew %dgp (%dgp of the budget left)", got, b.budgetLeft())
	}
	return nil
}

// buyStep walks to the keeper, plans against the live shelf and buys, measuring every line's
// real coin delta rather than trusting the plan's estimate.
func (b *shopBuyoutBot) buyStep(ctx *Context, d *shoprunner.Deps) {
	sh, ok := shoprunner.OpenKeeperShop(d, b.shop)
	if !ok {
		b.warn(ctx, "shop-open", 60*time.Second, "could not open %s's shop — retrying", b.shop.Keeper)
		ctx.Delay(3 * time.Second)
		return
	}
	defer func() { _ = sh.Close() }()

	coins := shoprunner.CoinsHeld(d)
	if left := b.budgetLeft(); coins > left {
		coins = left
	}
	stock := shoprunner.StockByID(sh, b.shop)
	plan := shopplan.BuyoutPlan(b.shop, stock, coins, b.chosen)
	if len(plan) == 0 {
		// Two different reasons, needing opposite responses. A drained shelf is worth waiting
		// at. An empty purse is not: waiting there was a real live hang, where a run with 7gp
		// logged "nothing to buy — waiting 10s for restock" indefinitely against a fully
		// stocked keeper that was simply charging more than 7gp.
		if shopplan.AnyChosenInStock(b.shop, stock, b.chosen) && !shopplan.CanAffordAnything(b.shop, stock, coins, b.chosen) {
			b.needCoins = true
			b.vlog(ctx, "carrying only %dgp, which does not reach the current price of anything %s still stocks — going back to the bank",
				coins, b.shop.Keeper)
			return
		}
		b.vlog(ctx, "nothing to buy at %s right now (carrying %dgp) — waiting %ds for restock",
			b.shop.Keeper, coins, b.settings.RecheckSeconds)
		ctx.Delay(time.Duration(b.settings.RecheckSeconds) * time.Second)
		return
	}

	bought := int32(0)
	for _, want := range plan {
		// An unstackable line cannot exceed the free slots, so cap it rather than sending
		// clicks the server will refuse with "You don't have enough inventory space." The TS
		// leaves this to the server; capping only avoids wasted clicks and never buys more.
		if !want.Item.Stackable {
			if free := int32(shoprunner.FreeSlots(d)); want.Units > free {
				want.Units = free
			}
		}
		if want.Units <= 0 {
			continue
		}
		res := shoprunner.BuyWant(d, sh, want)
		switch res.Reason {
		case "bought":
			b.haul[res.Item.ID] += res.Bought
			b.sessionSpent += res.Spent
			bought += res.Bought
			ctx.logf("shopbuyout: bought %d %s for %dgp (session %d/%dgp)",
				res.Bought, res.Item.Name, res.Spent, b.sessionSpent, b.settings.BudgetGp)
		case "no-coins":
			b.vlog(ctx, "out of coins at %s", b.shop.Keeper)
		default:
			b.vlog(ctx, "bought none of the %d %s asked for (%s)", want.Units, res.Item.Name, res.Reason)
		}
		if b.budgetLeft() <= 0 || shoprunner.PackFull(d) {
			break
		}
	}

	// Buying drains the shelf and raises its price, so the purse that funded this pass may no
	// longer reach anything. Deciding that here, while the shop is still open and the real
	// stock is readable, is what stops the next Loop walking back to a keeper it cannot buy
	// from.
	afterCoins := shoprunner.CoinsHeld(d)
	if left := b.budgetLeft(); afterCoins > left {
		afterCoins = left
	}
	afterStock := shoprunner.StockByID(sh, b.shop)
	if shopplan.AnyChosenInStock(b.shop, afterStock, b.chosen) && !shopplan.CanAffordAnything(b.shop, afterStock, afterCoins, b.chosen) {
		b.needCoins = true
		b.vlog(ctx, "down to %dgp, which no longer reaches %s's prices — banking for more", afterCoins, b.shop.Keeper)
	}

	if bought == 0 && !b.needCoins {
		// The shelf looked stocked but nothing moved; do not spin on it.
		b.vlog(ctx, "a buying pass bought nothing — waiting %ds before looking again", b.settings.RecheckSeconds)
		ctx.Delay(time.Duration(b.settings.RecheckSeconds) * time.Second)
	}
}

func (b *shopBuyoutBot) OnStop(ctx *Context) {
	var parts []string
	for id, n := range b.haul {
		name := fmt.Sprintf("object %d", id)
		if it, ok := b.shop.ItemByID(id); ok {
			name = it.Name
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, name))
	}
	haul := "nothing"
	if len(parts) > 0 {
		haul = strings.Join(parts, ", ")
	}
	ctx.logf("shopbuyout: stopped — bought %s for %dgp over %d bank trip(s) in %s",
		haul, b.sessionSpent, b.trips, time.Since(b.startedAt).Round(time.Second))
}
