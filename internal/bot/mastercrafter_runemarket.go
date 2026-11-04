package bot

// The bounded MARKET LOT of Rune essence (runecraft P2, docs/runecraft_p2_2026-10-13.md): the last rung of the
// levelling round's essence ladder, after the bank, the mule's stock and mining. Off unless the owner sets
// craftRuneBuyBudget (default 0); 300,000 coins buys about 3,000 essence at the stalls' 100 each.
//
//	the gate    essenceplan.PlanMarketLot: the budget not spent, the stalls on this world (world 1 only), the mule's
//	            published coins ABOVE ONE MILLION (a mule under it funds nothing), at no more than 150 an essence
//	the funds   one ask to the mule for the lot's coins (the lot is paid from the mule's side, never from the main account's own)
//	the buy     the Seers stalls in the owner's order (Seers Market, Tradevault, Sil Stuff), questplan.PickMarket's
//	            choice of stall and price, the woodcutters' live-proven stall buyer (scarabrunner.BuyRunesAtMarket: the
//	            fleet-wide one-buyer-per-stall turn, the stall's own quote, one re-offer at or under the unit cap)
//	the bank    what arrived (notes) is banked at the Seers bank, which un-notes it: the next bank visit draws it
//
// The stall path is the quest provisioner's (scarabswarm_questmarket.go questMarketEssence), reused, not changed: its pure
// choice and its shared helpers. It was NEVER PROVEN LIVE OR IN THE LAB (the lab has no market characters); the first lot
// logs every step. What was spent is counted in the ledger for good (RuneLedger.MarketSpent), so the budget is spent once.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// runeCoinsID is the Coins object.
const runeCoinsID = 995

// runeLotWalk bounds the walk from the mule's post to the Seers bank (about 100 tiles).
const runeLotWalk = 10 * time.Minute

// runeLotPlan is essenceplan.PlanMarketLot over the live figures: the ledger's spent coins, the mule's published coins
// and the world.
func (b *masterCrafterBot) runeLotPlan(ctx *Context, set mastercraftplan.RuneSettings, want int) (essenceplan.LotPlan, string) {
	t := essenceplan.LotTerms{
		Budget: set.BuyBudget, Spent: b.ledger.Rune.MarketSpent, Want: want,
		OnMarketWorld: fleetplan.MarketOnWorld(ctx.World(), fleetplan.DefaultMarketWorlds()),
	}
	if stock := muleBankCount(ctx, b.mule); stock != nil {
		n, _ := stock("Coins")
		t.MuleKnown, t.MuleCoins = true, n
	}
	return essenceplan.PlanMarketLot(t)
}

// runeLot is what a stall purchase came to.
type runeLot struct {
	// Got is the essence that arrived (as notes), Paid the coins it cost, Stall the stall that sold it.
	Got, Paid int
	Stall     string
	Why       string
	Stopped   bool
}

// runeLotExec is the stall purchase: the live one unless a test replaced it. It is given the lot's plan and the
// coins in the pack, walks to the Seers bank, buys, and banks the pack there.
var runeLotExec = masterBuyEssenceAtSeers

// Buy is the market lot stage.
func (w *masterRuneWorld) Buy(st runecraftplan.Step) runeStepResult {
	b, ctx := w.b, w.ctx
	plan, why := b.runeLotPlan(ctx, w.set, st.Goal)
	if why != "" {
		w.buyFailed = true
		return runeFail("market lot: " + why)
	}
	ledger := &b.ledger.Rune
	w.logf("market lot: budget %s coins, %s spent in %d lot(s) so far; buying up to %s essence at no more than %d each (%s coins at most), funded by %s",
		runecraftplan.Thousands(w.set.BuyBudget), runecraftplan.Thousands(ledger.MarketSpent), ledger.MarketLots,
		runecraftplan.Thousands(plan.Qty), essenceplan.MarketUnitCeiling, runecraftplan.Thousands(plan.MaxCoins), b.mule)
	// The funds: one ask to the mule at its post, for the lot's coins and nothing else.
	a := b.askMuleWith(ctx, w.d, []mastercraftplan.Line{{Item: "Coins", Qty: plan.Ask}}, masterAskOpts{})
	if a.Stopped {
		return runeStopped()
	}
	w.logf("market lot: asked %s for %s coins: got %s%s", b.mule, runecraftplan.Thousands(plan.Ask), masterDescribe(a.Got), whySuffix(a.Why))
	held := runecraftrunner.PackCount(w.d, runeCoinsID)
	if held < essenceplan.MarketUnitCeiling {
		w.buyFailed = true
		return runeFail(fmt.Sprintf("the mule gave no coins for the lot (%d held)%s", held, whySuffix(a.Why)))
	}
	res := runeLotExec(ctx, w.d, plan, held, func(f string, args ...any) { w.logf("market lot: "+f, args...) })
	if res.Stopped {
		return runeStopped()
	}
	// Whatever happened, the pack was banked at the Seers bank (the leftover coins too): read it.
	if res.Got > 0 || res.Paid > 0 {
		ledger.NoteMarket(res.Paid, res.Got)
		w.logf("MARKET LOT: %s Rune essence from %s for %s coins; the budget has spent %s of %s in %d lot(s), %s essence in all",
			runecraftplan.Thousands(res.Got), res.Stall, runecraftplan.Thousands(res.Paid), runecraftplan.Thousands(ledger.MarketSpent),
			runecraftplan.Thousands(w.set.BuyBudget), ledger.MarketLots, runecraftplan.Thousands(ledger.MarketBought))
	}
	w.readLiveBank()
	b.saveLedger(ctx)
	if res.Got <= 0 {
		w.buyFailed = true
		return runeFail("the market lot brought nothing: " + firstNonEmpty(res.Why, "no stall sold any"))
	}
	return runeOK()
}

// masterBuyEssenceAtSeers is the live stall purchase.
func masterBuyEssenceAtSeers(ctx *Context, d *fleetrunner.Deps, plan essenceplan.LotPlan, coins int, logf func(string, ...any)) (res runeLot) {
	stand := questplan.SeersMarketStand
	// The pack is banked at the Seers bank whatever comes of the buying: the leftover coins and any notes.
	defer func() {
		if v := mastercraftrunner.DepositAll(d, stand); !v.Opened {
			logf("could not bank the pack at the Seers bank (%s); the next bank visit will", v.Err)
		}
	}()
	if !masterWalkTo(d, stand, 4, runeLotWalk) {
		res.Why = "the walk to the Seers bank did not arrive (at " + fleetrunner.HereIs(d) + ")"
		return res
	}
	tried := questMarketResting(ctx.Username, questplan.PipEssenceMarkets, time.Now())
	stocks := questMarketStocks(fleetrunner.MarketListings(scarabSwarmMarketMaxAge, ctx.Log))
	for {
		if ctx.Stopped() {
			res.Stopped = true
			return res
		}
		held := runecraftrunner.PackCount(d, runeCoinsID)
		pick, why := questplan.PickMarket(questplan.MarketTerms{
			Order: questplan.PipEssenceMarkets, Item: essenceplan.Essence, Want: plan.Qty, Listings: stocks, Skip: tried,
			Coins: min(held, plan.MaxCoins), Fallback: questplan.PipEssenceUnitFallback, Ceiling: min(questplan.PipEssenceUnitCeiling, essenceplan.MarketUnitCeiling),
		})
		if pick.Market == "" {
			res.Why = "no stall to buy " + runecraftplan.Thousands(plan.Qty) + " Rune essence from: " + why
			return res
		}
		logf("asking %s for %d Rune essence at %d each (at most %d each, %d in all; %d coins held)", pick.Market, pick.Qty, pick.Unit, pick.UnitCap, pick.MaxTotal, held)
		order := fleetrunner.MarketRuneOrder{
			Market: pick.Market, Stand: stand, Item: essenceplan.Essence, Qty: pick.Qty,
			Unit: pick.Unit, UnitCap: pick.UnitCap, MaxCoins: pick.MaxTotal, Me: ctx.Username,
		}
		var r fleetrunner.MarketRuneResult
		for attempt := 0; attempt < marketBusyRetries; attempt++ {
			r = fleetrunner.BuyRunesAtMarket(d, order)
			if !r.Busy || r.Stopped {
				break
			}
			logf("%s is serving another account; waiting at the stall (%d of %d)", pick.Market, attempt+1, marketBusyRetries)
			if !ctx.Delay(20 * time.Second) {
				break
			}
		}
		if r.Stopped {
			res.Stopped = true
			return res
		}
		if r.QuoteHeard {
			logf("%s quoted %d %s at %d each", pick.Market, r.Quote.Qty, r.Quote.Item, r.Quote.Unit)
		}
		paid := held - runecraftrunner.PackCount(d, runeCoinsID)
		if r.Got > 0 || paid > 0 || r.Paid > 0 {
			res.Got, res.Paid, res.Stall = r.Got, max(paid, r.Paid), pick.Market
			res.Why = r.Reason
			return res
		}
		logf("%s sold no essence: %s", pick.Market, r.Reason)
		questMarketMarkFailed(ctx.Username, pick.Market, time.Now())
		tried[pick.Market] = true
		res.Why = strings.TrimSpace(firstNonEmpty(r.Reason, res.Why))
	}
}
