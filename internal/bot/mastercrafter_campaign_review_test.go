package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The review of the P4 commit (14 Oct 2026): the trigger sized the buy-out with the last campaign's total, and the refill committed to
// asking the mule for its whole stack before anything said a stall could sell. Both are tested here through the live seams.

func clearRestingStalls() {
	questMarketFailed.Lock()
	questMarketFailed.at = map[string]time.Time{}
	questMarketFailed.Unlock()
}

func stallRows(market string, stock, lastSold int) fleetplan.MarketListing {
	return fleetplan.MarketListing{Market: market, Item: "Rune essence", Stock: stock, LastSold: lastSold}
}

// ---- the trigger sizes the buy-out for the campaign it would start, not for the last one ----

// THE REVIEW'S FINDING: a campaign that finished at its target leaves Crafted at 100,000 in the ledger. With the mule out of Natures again
// and an essence row of 100, the trigger took the next campaign's need as 100,000 - 100,000 = 0 ("no essence is short") and refused to
// start unless the essence was already there. A campaign that is not under way starts from zero.
func TestCampaignTriggerRearmsAfterAFinishedCampaignWithABuyOut(t *testing.T) {
	setup := func(led mastercraftplan.CampaignLedger) *masterRig {
		r := newCampaignRig(t, nil, []actionsim.Stack{{ID: natTal, Count: 1}}, map[string]int{"Nature rune": 72, "Rune essence": 100, "Coins": 11_229_284, "Nature talisman": 7})
		r.sim.StandAt(2809, 3441)
		mindHoldStat(r.sim.State(), runecraftrunner.Stat, 55649, 44)
		for _, name := range []string{"acct1", "acct2", "acct3"} {
			r.ctx.Bus.Publish(mulebus.StandbyMessage(name, "main", tile.Tile{}))
		}
		r.b.ledger.Rune.Campaign = led
		return r
	}
	// The last campaign finished at the target (its Crafted is still its total) and the mule is out of Natures again.
	r := setup(mastercraftplan.CampaignLedger{Number: 1, Done: 1, Disarmed: true, Crafted: 100000, Rounds: 4, LastEnd: string(runecraftplan.EndTarget), LastWhy: "100,000 crafted"})
	d := r.b.decideLive(r.ctx)
	if d.Mode != mastercraftplan.ModeNature || !d.Campaign || d.Continue || !strings.Contains(d.Why, "99,900 more by a buy-out") {
		t.Fatalf("a finished campaign's Crafted must not size the next: %s (%s)", d.Mode, d.Why)
	}
	// A campaign that finished because the mule was full (62,000 crafted of 100,000): the same.
	r = setup(mastercraftplan.CampaignLedger{Number: 1, Done: 1, Disarmed: true, Crafted: 62000, LastEnd: string(runecraftplan.EndMuleFull)})
	if d := r.b.decideLive(r.ctx); d.Mode != mastercraftplan.ModeNature || !strings.Contains(d.Why, "99,900 more by a buy-out") {
		t.Fatalf("%s (%s)", d.Mode, d.Why)
	}
}

// The snapshot reads the campaign's own total only while it is under way: the paused campaign that is 40,000 in lacks 59,900 more.
func TestCampaignSnapshotCountsOnlyTheCampaignUnderWay(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 100, "Coins": 11_229_284})
	stock, _ := muleStockOf(r)
	read := func() mastercraftplan.Snapshot {
		snap := mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
		r.b.campaignSnapshot(r.ctx, &snap)
		return snap
	}
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Crafted: 100000, Disarmed: true}
	if s := read(); s.Rune.BuyOut != 99900 || s.Rune.BuyOutWhy != "" {
		t.Fatalf("not under way: %+v", s.Rune)
	}
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 40000}
	if s := read(); s.Rune.BuyOut != 59900 {
		t.Fatalf("under way, 40,000 crafted and 100 on the mule: %+v", s.Rune)
	}
	// An active campaign that has already crafted the target needs nothing (it ends at its next round).
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 100000}
	if s := read(); s.Rune.BuyOut != 0 || !strings.Contains(s.Rune.BuyOutWhy, "no essence is short") {
		t.Fatalf("%+v", s.Rune)
	}
}

// The trigger fetches the price page only when a buy-out could matter: not with essence on the mule, not for a campaign that cannot
// start (the mule's Natures are enough, the campaign is paused or given up).
func TestCampaignSnapshotReadsThePriceOnlyWhenABuyOutCouldMatter(t *testing.T) {
	fetches := 0
	read := func(r *masterRig) mastercraftplan.Snapshot {
		stock, _ := muleStockOf(r)
		snap := mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
		r.b.campaignSnapshot(r.ctx, &snap)
		return snap
	}
	count := func(r *masterRig) {
		masterMarketListings = func(time.Duration, func(string)) []fleetplan.MarketListing { fetches++; return campaignPage }
	}
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 12400, "Coins": 11_229_284})
	count(r)
	if s := read(r); fetches != 0 || s.Rune.BuyOut != 0 {
		t.Fatalf("essence on the mule: %d fetches %+v", fetches, s.Rune)
	}
	r = newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 5000, "Rune essence": 100, "Coins": 11_229_284})
	count(r)
	if read(r); fetches != 0 {
		t.Fatalf("the mule holds Natures enough: %d fetches", fetches)
	}
	r = newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 100, "Coins": 11_229_284})
	count(r)
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 1000, PausedUntil: time.Now().Add(30 * time.Minute)}
	if read(r); fetches != 0 {
		t.Fatalf("paused: %d fetches", fetches)
	}
	r.b.ledger.Rune.Campaign.PausedUntil = time.Time{}
	if s := read(r); fetches != 1 || s.Rune.BuyOut == 0 {
		t.Fatalf("a campaign that could start reads the page once: %d fetches %+v", fetches, s.Rune)
	}
}

// ---- the refill commits nothing before it knows something can be bought ----

// With no stall listing Rune essence (they were bought out, or never stocked) nothing is asked of the mule and nothing is walked: the
// mule holds under a shift's worth, so the round says why it cannot go on.
func TestCampaignSupplyAsksNothingWhenNoStallListsEssence(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	campaignPage = []fleetplan.MarketListing{
		{Market: "Seers Market", Item: "Nature rune", Stock: 40000, LastSold: 200},
		{Market: "Tradevault", Item: "Law rune", Stock: 3000},
		{Market: "Sil Stuff", Item: "Chaos rune", Stock: 900},
	}
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "no stall to buy essence from") || !strings.Contains(why, "its page lists no rune essence") {
		t.Fatalf("%v %q", ok, why)
	}
	if len(rr.asks) != 0 || len(rr.buys) != 0 || len(rr.handed) != 0 {
		t.Fatalf("nothing is asked, bought or handed: asks %v buys %d handed %v", rr.asks, len(rr.buys), rr.handed)
	}
	if rr.r.b.ledger.Rune.Campaign.BuyOuts != 0 || !rr.r.b.ledger.Rune.Campaign.NoStockUntil.IsZero() {
		t.Fatalf("a refill that never started leaves no memory: %+v", rr.r.b.ledger.Rune.Campaign)
	}
	// With a shift's worth on the mule the round goes on, still asking nothing.
	rr = newRefillRig(t, 11_229_284, 1200, nil)
	campaignPage = nil
	if why, ok := rr.w.campaignSupply(); !ok || why != "" || len(rr.asks) != 0 {
		t.Fatalf("%v %q %v", ok, why, rr.asks)
	}
}

// A price page that could not be read is no reading: the whole stack does not go on the road blind.
func TestCampaignSupplyAsksNothingWhenThePageCannotBeRead(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	campaignPage = nil
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "price page could not be read") || len(rr.asks) != 0 || len(rr.buys) != 0 {
		t.Fatalf("%v %q asks %v", ok, why, rr.asks)
	}
}

// Stalls that failed this account's purchase a few minutes ago are resting: with all three resting there is nothing to ask for.
func TestCampaignSupplyHonoursRestingStalls(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	for _, m := range []string{"Seers Market", "Tradevault", "Sil Stuff"} {
		questMarketMarkFailed(rr.r.ctx.Username, m, time.Now())
	}
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "asked already or resting after a failure") || len(rr.asks) != 0 {
		t.Fatalf("%v %q asks %v", ok, why, rr.asks)
	}
	// One of them rested and the other two not: those two are asked, the one is not counted in what is listed.
	clearRestingStalls()
	questMarketMarkFailed(rr.r.ctx.Username, "Seers Market", time.Now())
	campaignPage = []fleetplan.MarketListing{stallRows("Seers Market", 298249, 100), stallRows("Tradevault", 30000, 100), stallRows("Sil Stuff", 20000, 100)}
	plan, why := rr.r.b.campaignBuyPlan(rr.r.ctx, 99900, 0, 0, 0)
	if why != "" || plan.Qty != 50000 || plan.Ask != 50000*essenceplan.MarketUnitCeiling {
		t.Fatalf("only what the two stalls list: %+v %q", plan, why)
	}
}

// The ask goes no higher than the stalls list: 8,000 essence at one stall is 1,200,000 coins at the ceiling, not the mule's whole stack.
func TestCampaignSupplyAsksNoMoreThanTheStallsList(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	campaignPage = []fleetplan.MarketListing{
		stallRows("Seers Market", 8000, 100),
		{Market: "Tradevault", Item: "Law rune", Stock: 3000},
		{Market: "Sil Stuff", Item: "Chaos rune", Stock: 900},
	}
	why, ok := rr.w.campaignSupply()
	if !ok || why != "" {
		t.Fatalf("%v %q\n%s", ok, why, strings.Join(rr.r.logs, "\n"))
	}
	if len(rr.asks) != 1 || rr.asks[0] != 8000*essenceplan.MarketUnitCeiling {
		t.Fatalf("the ask is what the 8,000 listed cost at the ceiling: %v", rr.asks)
	}
	if rr.sold != 8000 || len(rr.handed) != 1 || rr.handed[0]["Rune essence"] != 8000 || rr.handed[0]["Coins"] != 8000*essenceplan.MarketUnitCeiling-800_000 {
		t.Fatalf("sold %d handed %v", rr.sold, rr.handed)
	}
	if !rr.r.logged("the price page lists it at Seers Market (8,000 listed in all)") {
		t.Fatalf("log:\n%s", strings.Join(rr.r.logs, "\n"))
	}
	// Under the least worth a trip: no ask at all, and the figure is said.
	rr = newRefillRig(t, 11_229_284, 100, nil)
	campaignPage = []fleetplan.MarketListing{stallRows("Seers Market", 800, 100)}
	why, ok = rr.w.campaignSupply()
	if ok || !strings.Contains(why, "the stalls list only 800 essence in all") || len(rr.asks) != 0 {
		t.Fatalf("%v %q %v", ok, why, rr.asks)
	}
}

// ---- a buy-out that came up short is remembered ----

// A buy-out that reached the stalls and bought under the least worth a trip holds the next one off for hours: the trigger, the dry-end
// refill and the supply all say so, and nothing is asked of the mule. When the memory lapses the buy-out is tried again, and one that
// bought enough clears it.
func TestCampaignBuyOutThatCameUpShortIsRemembered(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) { rr.stallStock = 300 })
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "the mule holds only 400 Rune essence after the buy-out") {
		t.Fatalf("%v %q", ok, why)
	}
	if len(rr.asks) != 1 || rr.sold != 300 {
		t.Fatalf("asks %v sold %d", rr.asks, rr.sold)
	}
	c := &rr.r.b.ledger.Rune.Campaign
	if c.NoStockUntil.Before(time.Now().Add(essenceplan.BuyOutNoStockWait-time.Minute)) || !rr.r.logged("BUY-OUT came up short - the stalls sold 300 Rune essence of the 99,900 wanted (under the 1,000 worth a trip)") {
		t.Fatalf("%+v\n%s", c, strings.Join(rr.r.logs, "\n"))
	}
	// The next round's refill asks for nothing.
	why, ok = rr.w.campaignSupply()
	if ok || !strings.Contains(why, "not asked again until") || len(rr.asks) != 1 {
		t.Fatalf("%v %q asks %v", ok, why, rr.asks)
	}
	// The trigger does not count a buy-out, and says why; a dry end does not call a refill possible.
	stock, _ := muleStockOf(rr.r)
	snap := mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
	rr.r.b.campaignSnapshot(rr.r.ctx, &snap)
	if snap.Rune.BuyOut != 0 || !strings.Contains(snap.Rune.BuyOutWhy, "not asked again until") {
		t.Fatalf("%+v", snap.Rune)
	}
	if cont, whyNot := rr.r.b.campaignRefillPossible(rr.r.ctx); cont || !strings.Contains(whyNot, "not asked again until") {
		t.Fatalf("%v %q", cont, whyNot)
	}
	// A new campaign remembers it too (the stalls do not restock for it).
	c.Begin(time.Now(), 72)
	if _, quiet := c.NoStock(time.Now()); !quiet {
		t.Fatal("kept through Begin")
	}
	// It lapses: asked again, and a buy-out that bought what it should clears the memory.
	c.NoStockUntil = time.Now().Add(-time.Minute)
	rr.stallStock = 1 << 30
	why, ok = rr.w.campaignSupply()
	if !ok || why != "" || len(rr.asks) != 2 || rr.sold != 300+99900 {
		t.Fatalf("%v %q asks %v sold %d", ok, why, rr.asks, rr.sold)
	}
	if !c.NoStockUntil.IsZero() {
		t.Fatalf("a buy-out that bought what it should clears the memory: %v", c.NoStockUntil)
	}
}

// A trip that never reached the stalls (the walk failed) says nothing about their stock: no memory.
func TestCampaignBuyOutThatNeverReachedTheStallsIsNotRemembered(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) {
		masterBuyOutExec = func(*Context, *fleetrunner.Deps, essenceplan.BuyOutPlan, int, func(string, ...any)) buyOutResult {
			return buyOutResult{Why: "the walk to the Seers stalls did not arrive (at somewhere)"}
		}
	})
	why, ok := rr.w.campaignSupply()
	if ok || len(rr.asks) != 1 {
		t.Fatalf("%v %q asks %v", ok, why, rr.asks)
	}
	if c := rr.r.b.ledger.Rune.Campaign; !c.NoStockUntil.IsZero() {
		t.Fatalf("a walk that failed is not an empty stall: %+v", c)
	}
}

// A stop in the middle of the buying says nothing about the stalls either.
func TestCampaignBuyOutStoppedAtTheStallsIsNotRemembered(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	masterBuyOutExec = func(*Context, *fleetrunner.Deps, essenceplan.BuyOutPlan, int, func(string, ...any)) buyOutResult {
		rr.r.ctx.requestStop()
		return buyOutResult{Reached: true, Stopped: true}
	}
	if why, ok := rr.w.campaignSupply(); ok || len(rr.asks) != 1 {
		t.Fatalf("%v %q asks %v", ok, why, rr.asks)
	}
	if c := rr.r.b.ledger.Rune.Campaign; !c.NoStockUntil.IsZero() {
		t.Fatalf("a stopped buy-out is not an empty stall: %+v", c)
	}
}

// ---- a gate that runs dry is refilled at once only once ----

// The round that follows a pause-free refill and runs dry again WITHOUT A SINGLE TRADE pauses the campaign instead of refilling again;
// one that traded earns another.
func TestCampaignDryEndRefillsOnceThenPauses(t *testing.T) {
	now := time.Now()
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 700, "Rune essence": 100, "Coins": 11_229_284})
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Started: now.Add(-6 * time.Hour), Crafted: 20000}
	end := func(trades int) {
		w := newCampaignWorld(t, r, nil)
		w.trades = trades
		w.camp.latch(runecraftplan.EndDry, "no essence came")
		if !w.campaignRoundEnd(natureLoopResult{Done: true}) {
			t.Fatal("a round that ended by a campaign rule is decided by it")
		}
	}
	c := &r.b.ledger.Rune.Campaign
	// The first dry end: refilled at once.
	end(0)
	if c.PausedUntil.After(time.Now()) || c.DryRefills != 1 || !r.logged("the next round refills at once") {
		t.Fatalf("%+v\n%s", *c, strings.Join(r.logs, "\n"))
	}
	// The round after it ran dry again with no trade: a pause.
	end(0)
	if c.PausedUntil.Before(now.Add(44*time.Minute)) || c.DryRefills != 1 || !r.logged("no second one without a pause") {
		t.Fatalf("%+v\n%s", *c, strings.Join(r.logs, "\n"))
	}
	// A round that traded before it ran dry is not that: it earns the one refill again.
	end(3)
	if c.PausedUntil.After(time.Now()) || c.DryRefills != 1 {
		t.Fatalf("%+v", *c)
	}
	// A round that ended another way clears the count.
	w := newCampaignWorld(t, r, nil)
	w.trades = 2
	w.camp.latch(runecraftplan.EndTime, "eight hours")
	w.campaignRoundEnd(natureLoopResult{Done: true})
	if c.DryRefills != 0 {
		t.Fatalf("%+v", *c)
	}
}

// ---- the whole chain, as the live run would meet it ----

// After a buy-out has emptied the stalls (the price page lists none), a gate that ran dry is not refilled: it pauses, and the mule is
// not asked for a coin in the round after.
func TestCampaignDryEndWithEmptyStallsPausesAndAsksNothing(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	r := rr.r
	campaignPage = []fleetplan.MarketListing{{Market: "Seers Market", Item: "Law rune", Stock: 3000}}
	r.b.ledger.Rune.Campaign.Crafted = 20000
	w := newCampaignWorld(t, r, nil)
	w.camp.latch(runecraftplan.EndDry, "no essence came")
	w.campaignRoundEnd(natureLoopResult{Done: true})
	c := r.b.ledger.Rune.Campaign
	if c.PausedUntil.Before(time.Now().Add(44*time.Minute)) || c.DryRefills != 0 || !r.logged("no refill: no stall to buy essence from") {
		t.Fatalf("%+v\n%s", c, strings.Join(r.logs, "\n"))
	}
	if len(rr.asks) != 0 {
		t.Fatalf("asks %v", rr.asks)
	}
}
