package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// The market lot's handler over the simulated bank and pack: the plan, the one ask to the mule for the lot's coins, the
// stall purchase (a seam), the ledger, the bank.

const lotCoins = 995

// lotRig is a the main account account at the post with the coins the mule's ask will have given already in the pack (the
// simulator's pack stacks nothing; one row of 300,000 coins stands for them), a mule publishing coins and a fake kit
// bundle that records the ask.
func lotRig(t *testing.T, muleCoins, budget int) (*masterRig, *[]fleetplan.ItemCount) {
	t.Helper()
	r := newSimRig(t, []actionsim.Stack{{ID: lotCoins, Count: 300_000}}, nil, map[string]int{"Coins": muleCoins})
	r.b.set.Rune.BuyBudget = budget
	asked := &[]fleetplan.ItemCount{}
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		*asked = append(*asked, it...)
		return fleetrunner.KitResult{Got: map[string]int{"Coins": 300_000}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	return r, asked
}

// fakeStalls replaces the stall purchase: it sells got essence for paid coins, takes the coins off the pack and banks the
// essence the way the Seers bank's deposit would.
func fakeStalls(t *testing.T, r *masterRig, got, paid int, why string) *int {
	t.Helper()
	calls := new(int)
	old := runeLotExec
	t.Cleanup(func() { runeLotExec = old })
	runeLotExec = func(_ *Context, _ *fleetrunner.Deps, plan essenceplan.LotPlan, coins int, logf func(string, ...any)) runeLot {
		*calls++
		logf("a fake stall: %d coins held, up to %d essence", coins, plan.Qty)
		if got <= 0 {
			return runeLot{Why: why}
		}
		r.sim.Remove(lotCoins, paid)
		r.sim.OpenBank(append(r.sim.Bank(), actionsim.Stack{ID: runecraftplan.EssenceID, Count: got}))
		return runeLot{Got: got, Paid: paid, Stall: "Seers Market", Why: why}
	}
	return calls
}

// A lot: the mule holds 2M so 1M is spare; the 300,000 budget buys 2,000 at the 150 ceiling; ONE ask for 300,000 coins;
// the stall sells 1,900 for 190,000; the ledger counts it for good and the bank has the essence.
func TestTheMarketLotAsksForCoinsBuysAndBanksTheEssence(t *testing.T) {
	r, asked := lotRig(t, 2_000_000, 300_000)
	calls := fakeStalls(t, r, 1900, 190_000, "")
	w := newRuneWorld(t, r, nil)
	res := w.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000})
	if !res.OK || *calls != 1 || w.buyFailed {
		t.Fatalf("%+v calls %d failed %v\n%v", res, *calls, w.buyFailed, r.logs)
	}
	if len(*asked) != 1 || (*asked)[0].Item != "Coins" || (*asked)[0].Count != 300_000 {
		t.Errorf("the ask: %v, want one line of 300,000 coins", *asked)
	}
	if l := r.b.ledger.Rune; l.MarketSpent != 190_000 || l.MarketBought != 1900 || l.MarketLots != 1 {
		t.Errorf("ledger %+v", l)
	}
	if !w.bankKnown || w.bankEssence != 1900 {
		t.Errorf("the bank as the world sees it: %v %d", w.bankKnown, w.bankEssence)
	}
	for _, want := range []string{
		"market lot: budget 300,000 coins, 0 spent in 0 lot(s) so far; buying up to 2,000 essence at no more than 150 each (300,000 coins at most)",
		"market lot: asked mule for 300,000 coins",
		"MARKET LOT: 1,900 Rune essence from Seers Market for 190,000 coins; the budget has spent 190,000 of 300,000 in 1 lot(s), 1,900 essence in all",
	} {
		if !r.logged(want) {
			t.Errorf("log lacks %q:\n%s", want, strings.Join(r.logs, "\n"))
		}
	}
	// The budget is spent once, across rounds: a second lot may only use what is left of it.
	plan, why := r.b.runeLotPlan(r.ctx, w.set, 5000)
	if why != "" || plan.Qty != 733 || plan.MaxCoins != 109_950 { // 110,000 left / 150
		t.Errorf("second lot %+v %q", plan, why)
	}
}

// The mule under a million coins funds nothing: no ask is made, nothing is bought, the round does not try again.
func TestTheMarketLotIsRefusedWhileTheMuleIsUnderAMillion(t *testing.T) {
	r, asked := lotRig(t, 773_226, 300_000)
	calls := fakeStalls(t, r, 1900, 190_000, "")
	w := newRuneWorld(t, r, nil)
	res := w.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000})
	if res.OK || !w.buyFailed || *calls != 0 || len(*asked) != 0 || !strings.Contains(res.Why, "floor") {
		t.Fatalf("%+v asked %v calls %d", res, *asked, *calls)
	}
	if r.b.ledger.Rune.MarketSpent != 0 {
		t.Errorf("ledger %+v", r.b.ledger.Rune)
	}
}

// A budget of 0 is off; a spent budget is off; neither asks the mule for a coin.
func TestTheMarketLotOffAndSpent(t *testing.T) {
	r, asked := lotRig(t, 5_000_000, 0)
	calls := fakeStalls(t, r, 100, 10_000, "")
	w := newRuneWorld(t, r, nil)
	if res := w.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000}); res.OK || !strings.Contains(res.Why, "off") {
		t.Fatalf("budget 0: %+v", res)
	}
	r2, asked2 := lotRig(t, 5_000_000, 300_000)
	r2.b.ledger.Rune.MarketSpent = 300_000
	w2 := newRuneWorld(t, r2, nil)
	if res := w2.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000}); res.OK || !strings.Contains(res.Why, "spent") {
		t.Fatalf("spent: %+v", res)
	}
	if *calls != 0 || len(*asked) != 0 || len(*asked2) != 0 {
		t.Errorf("nothing may be asked or bought: %d %v %v", *calls, *asked, *asked2)
	}
}

// A lot that buys nothing is a failure that spends nothing, and the pack is still banked by the stall path.
func TestTheMarketLotThatBuysNothingSpendsNothing(t *testing.T) {
	r, _ := lotRig(t, 2_000_000, 300_000)
	calls := fakeStalls(t, r, 0, 0, "Seers Market: no essence listed")
	w := newRuneWorld(t, r, nil)
	res := w.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000})
	if res.OK || !w.buyFailed || *calls != 1 || !strings.Contains(res.Why, "no essence listed") {
		t.Fatalf("%+v", res)
	}
	if l := r.b.ledger.Rune; l.MarketSpent != 0 || l.MarketLots != 0 {
		t.Errorf("ledger %+v", l)
	}
}

// A mule that gives no coins ends the lot before any stall is walked to.
func TestTheMarketLotNeedsTheMulesCoins(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Coins": 2_000_000})
	r.b.set.Rune.BuyBudget = 300_000
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{"Coins": "declined: busy"}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	calls := fakeStalls(t, r, 1900, 190_000, "")
	w := newRuneWorld(t, r, nil)
	res := w.Buy(runecraftplan.Step{Stage: runecraftplan.StageBuy, Goal: 2000})
	if res.OK || *calls != 0 || !strings.Contains(res.Why, "no coins") || !w.buyFailed {
		t.Fatalf("%+v calls %d", res, *calls)
	}
}

// Read() reports whether a lot is allowed, so the ladder can reach it; with the budget off it reports nothing at all.
func TestTheReadingOffersALotOnlyOnTheLadder(t *testing.T) {
	r, _ := lotRig(t, 2_000_000, 300_000)
	w := newRuneWorld(t, r, nil)
	r.sim.StandAt(2809, 3441)
	mindHoldStat(r.sim.State(), runecraftrunner.Stat, 0, 1) // Runecrafting 1
	snap := w.Read()
	if !snap.CanBuy || snap.BuyQty != 2000 {
		t.Fatalf("a lot is allowed: %+v", snap)
	}
	w.set.Essence = runecraftplan.SourceMine
	if snap := w.Read(); snap.CanBuy {
		t.Errorf("a named source never buys: %+v", snap)
	}
	w.set.Essence, w.set.BuyBudget = runecraftplan.SourceAuto, 0
	if snap := w.Read(); snap.CanBuy || snap.BuyQty != 0 {
		t.Errorf("budget 0: %+v", snap)
	}
	w.set.BuyBudget = 300_000
	w.buyFailed = true
	if snap := w.Read(); !snap.BuyFailed {
		t.Errorf("a failed lot is reported: %+v", snap)
	}
}
