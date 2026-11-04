package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Nature campaign over the simulated account: the end rule through the live world, the ledger it keeps, the keep and the hand-over,
// and the refill that spends the mule's whole coin stack. The stalls, the mule's coin orders and the trade are the seams.

var notedEss = int32(runecraftplan.NotedEssenceID)

// campaignPage is the market price page the campaign rigs read: the three stalls as the page showed them on 13 Oct 2026 (Seers Market
// 298,249 Rune essence at 100, Tradevault 1,213,889, Sil Stuff 262,411 with no price). A test sets it to what the stalls hold.
var campaignPage []fleetplan.MarketListing

func essencePage() []fleetplan.MarketListing {
	return []fleetplan.MarketListing{
		{Market: "Seers Market", Item: "Rune essence", Stock: 298249, LastSold: 100},
		{Market: "Tradevault", Item: "Rune essence", Stock: 1213889, LastSold: 100},
		{Market: "Sil Stuff", Item: "Rune essence", Stock: 262411},
	}
}

// newCampaignRig is a MasterCrafter at the mule's post with the campaign on and the mule publishing stock. The market price page is the
// stub campaignPage (never the network), and no stall is resting.
func newCampaignRig(t *testing.T, pack, bank []actionsim.Stack, stock map[string]int) *masterRig {
	t.Helper()
	stackable(t, natRune, coinsObj, notedEss)
	r := newSimRig(t, pack, bank, stock)
	campaignPage = essencePage()
	oldList := masterMarketListings
	masterMarketListings = func(time.Duration, func(string)) []fleetplan.MarketListing { return campaignPage }
	clearRestingStalls()
	t.Cleanup(func() { masterMarketListings = oldList; campaignPage = nil; clearRestingStalls() })
	r.ctx.Pack = &pathfind.Pack{Members: true}
	r.b.set.Rune.On, r.b.set.Rune.Manual, r.b.set.Rune.Campaign = true, false, true
	withRuneBank(t, r)
	return r
}

// newCampaignWorld is the gate round's world with the campaign on.
func newCampaignWorld(t *testing.T, r *masterRig, mod func(w *masterNatureWorld)) *masterNatureWorld {
	t.Helper()
	oldWait := natureWait
	natureWait = 150 * time.Millisecond
	t.Cleanup(func() { natureWait = oldWait })
	rw := newRuneWorld(t, r, nil)
	rw.post, rw.postKnown = tile.Tile{X: 2809, Z: 3441}, true
	w := &masterNatureWorld{masterRuneWorld: rw, watch: runecraftrunner.NewGateWatcher(rw.d), strangerSaid: map[string]time.Time{}, carrierSeen: time.Now()}
	w.camp.on = true
	if mod != nil {
		mod(w)
	}
	return w
}

// publishMule republishes the mule's bank (the world reads it at most every campaignStockEvery: the cache is cleared).
func republishMule(r *masterRig, w *masterNatureWorld, stock map[string]int) {
	var hs []peerbank.Holding
	for k, v := range stock {
		hs = append(hs, peerbank.Holding{Item: k, Count: v})
	}
	r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, hs, nil, false)
	if w != nil {
		w.camp.stock.at = time.Time{}
	}
}

func carriersAtWork(r *masterRig, n int) {
	for i := 0; i < n; i++ {
		r.ctx.Bus.Publish(mulebus.CarrierMessage("swarm"+string(rune('1'+i)), "main", tile.Tile{}, mulebus.CarrierWaiting, 25))
	}
}

// ---- the start and the ledger ----

func TestCampaignRoundStartBeginsAndCarriesOn(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 12400, "Nature talisman": 7})
	w := newCampaignWorld(t, r, nil)
	w.campaignRoundStart(mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true})
	c := r.b.ledger.Rune.Campaign
	if !c.Active || c.Number != 1 || c.Rounds != 1 || c.MuleAtStart != 72 || c.Started.IsZero() || c.Crafted != 0 {
		t.Fatalf("%+v", c)
	}
	if !r.logged("CAMPAIGN START - campaign 1: the mule holds 72 Nature runes (under 1,000), 12,400 Rune essence; target 100,000 Nature runes, stops when the mule holds 60,000") {
		t.Fatalf("log:\n%s", strings.Join(r.logs, "\n"))
	}
	// The next round of it carries on and does not begin another.
	r.b.ledger.Rune.Campaign.Crafted = 31025
	w2 := newCampaignWorld(t, r, nil)
	w2.campaignRoundStart(mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Continue: true})
	c = r.b.ledger.Rune.Campaign
	if c.Number != 1 || c.Rounds != 2 || c.Crafted != 31025 {
		t.Fatalf("%+v", c)
	}
	if !r.logged("CAMPAIGN goes on - round 2 of campaign 1, crafted 31,025 of 100,000 Nature runes") {
		t.Fatalf("log:\n%s", strings.Join(r.logs, "\n"))
	}
}

// A restart that finds the account at the gate after the campaign finished ends the round at once: there is nothing to craft for.
func TestCampaignResumedAfterItFinishedEndsAtOnce(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 59000})
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Disarmed: true, Number: 2, Done: 2, LastWhy: "100,000 crafted"}
	w := newCampaignWorld(t, r, nil)
	w.campaignRoundStart(mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Resume: true})
	if r.b.ledger.Rune.Campaign.Active || !strings.Contains(w.endReason(), "the last campaign finished") {
		t.Fatalf("%+v end %q", r.b.ledger.Rune.Campaign, w.endReason())
	}
	// With no campaign recorded at all (a lost ledger), the resumed round adopts one.
	r2 := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 300})
	w2 := newCampaignWorld(t, r2, nil)
	w2.campaignRoundStart(mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Resume: true})
	if c := r2.b.ledger.Rune.Campaign; !c.Active || c.Number != 1 || w2.camp.forceEnd != "" {
		t.Fatalf("%+v", c)
	}
}

// ---- the end rule over the live world ----

func TestCampaignEndRuleOverTheLiveWorld(t *testing.T) {
	now := time.Now()
	newW := func(mod func(r *masterRig, w *masterNatureWorld)) (*masterRig, *masterNatureWorld) {
		r := newCampaignRig(t, []actionsim.Stack{{ID: natRune, Count: 6000}}, nil, map[string]int{"Nature rune": 700, "Rune essence": 9000})
		w := newCampaignWorld(t, r, func(w *masterNatureWorld) {
			w.started = now.Add(-time.Hour)
			w.lastTrade = now.Add(-time.Minute)
		})
		r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Started: now.Add(-time.Hour), Crafted: 12000}
		carriersAtWork(r, 6)
		if mod != nil {
			mod(r, w)
		}
		return r, w
	}
	// A quiet gate runs on.
	if _, w := newW(nil); w.endReason() != "" || w.camp.endKind != runecraftplan.EndNone {
		t.Fatalf("%q %q", w.endReason(), w.camp.endKind)
	}
	// The target: the craft that reaches it ends the round, and the end stays.
	r, w := newW(nil)
	r.b.ledger.Rune.Campaign.Crafted = 99990
	w.noteCampaignCraft(10)
	if why := w.endReason(); !strings.Contains(why, "craftRuneTarget 100,000") || w.camp.endKind != runecraftplan.EndTarget {
		t.Fatalf("%q %q", why, w.camp.endKind)
	}
	r.b.ledger.Rune.Campaign.Crafted = 5
	if w.endReason() == "" || w.camp.endKind != runecraftplan.EndTarget {
		t.Fatal("an end found is latched: the round does not change its mind")
	}
	// The mule holds the stop level.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) {
		republishMule(r, w, map[string]int{"Nature rune": 61200, "Rune essence": 9000})
	})
	if why := w.endReason(); !strings.Contains(why, "holds 61,200 Nature runes") || w.camp.endKind != runecraftplan.EndMuleFull {
		t.Fatalf("%q %q", why, w.camp.endKind)
	}
	// ...but a mule that has not published stops nothing.
	r, w = newW(nil)
	r.ctx.Peers = nil
	if why := w.endReason(); why != "" {
		t.Fatalf("an unpublished mule must not end a campaign: %q", why)
	}
	// Fewer than two carriers for half an hour.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) {
		r.ctx.Bus = mulebus.NewBus()
		carriersAtWork(r, 1)
		w.started = now.Add(-45 * time.Minute)
		w.camp.watch.Since = now.Add(-31 * time.Minute)
	})
	if why := w.endReason(); !strings.Contains(why, "fewer than 2 carriers") || w.camp.endKind != runecraftplan.EndCarriers {
		t.Fatalf("%q %q", why, w.camp.endKind)
	}
	// No essence for 35 minutes with carriers about: two dry checks.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) { w.lastTrade = now.Add(-35 * time.Minute) })
	if why := w.endReason(); !strings.Contains(why, "2 of 2 dry checks") || w.camp.endKind != runecraftplan.EndDry {
		t.Fatalf("%q %q", why, w.camp.endKind)
	}
	// The round's eight hours.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) { w.started = now.Add(-9 * time.Hour) })
	if w.endReason() == "" || w.camp.endKind != runecraftplan.EndTime {
		t.Fatalf("%q", w.camp.endKind)
	}
	// Own trip: 24,300 held (keep 4,000), the last courier three hours ago.
	r, w = newW(func(r *masterRig, w *masterNatureWorld) {
		r.sim.Give(natRune, 18300)
		w.started = now.Add(-5 * time.Hour)
		w.camp.lastCourier = now.Add(-3 * time.Hour)
		w.lastTrade = now.Add(-time.Minute)
	})
	if why := w.endReason(); !strings.Contains(why, "20,300 Nature runes") || w.camp.endKind != runecraftplan.EndHandOver {
		t.Fatalf("%q %q", why, w.camp.endKind)
	}
	// A courier a few minutes ago: 24,300 is no trip yet.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) {
		r.sim.Give(natRune, 18300)
		w.started = now.Add(-5 * time.Hour)
		w.camp.lastCourier = now.Add(-10 * time.Minute)
	})
	if why := w.endReason(); why != "" {
		t.Fatalf("a courier is coming: %q", why)
	}
	// Couriers off: at the trip level it goes.
	_, w = newW(func(r *masterRig, w *masterNatureWorld) {
		r.sim.Give(natRune, 12000)
		w.set.Courier = false
	})
	if why := w.endReason(); !strings.Contains(why, "couriers are off") {
		t.Fatalf("%q", why)
	}
}

// The first dry check is a warning in the log, said once; the second ends the round.
func TestCampaignSaysTheFirstDryCheckOnce(t *testing.T) {
	now := time.Now()
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 700, "Rune essence": 9000})
	w := newCampaignWorld(t, r, func(w *masterNatureWorld) {
		w.started = now.Add(-time.Hour)
		w.lastTrade = now.Add(-16 * time.Minute)
	})
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 100}
	carriersAtWork(r, 4)
	for i := 0; i < 4; i++ {
		if why := w.endReason(); why != "" {
			t.Fatalf("one dry check ends nothing: %q", why)
		}
	}
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, "essence dry - no trade for 15m0s (check 1 of 2)") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("said %d times:\n%s", n, strings.Join(r.logs, "\n"))
	}
	// A trade clears the count: the next dry spell warns again.
	w.lastTrade = now
	w.endReason()
	if w.camp.drySaid != 0 {
		t.Fatal("a trade resets the warning")
	}
}

// The progress line every ten minutes: crafted, rate, carriers, the mule's essence and Natures, ETA.
func TestCampaignProgressLine(t *testing.T) {
	r := newCampaignRig(t, []actionsim.Stack{{ID: natRune, Count: 6300}}, nil, map[string]int{"Nature rune": 72, "Rune essence": 8200})
	w := newCampaignWorld(t, r, nil)
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 12000}
	carriersAtWork(r, 6)
	w.campaignTick()
	if r.logged("campaign: crafted") {
		t.Fatalf("no line before ten minutes:\n%v", r.logs)
	}
	w.camp.lineAt = time.Now().Add(-11 * time.Minute)
	w.camp.rate.Note(time.Now().Add(-10*time.Minute), 9000)
	w.camp.rateAt = time.Time{}
	w.campaignTick()
	if !r.logged("campaign: crafted 12,000 of 100,000 Nature runes") || !r.logged("6 carrier(s) at work, the mule holds 8,200 essence and 72 Nature runes, the main account holds 6,300 (keep 4,000)") {
		t.Fatalf("log:\n%s", strings.Join(r.logs, "\n"))
	}
	if !r.logged("/h over the last 10m0s") {
		t.Fatalf("the rate window is said:\n%s", strings.Join(r.logs, "\n"))
	}
}

// ---- how a round ends in the ledger ----

func TestCampaignRoundEndTellsTheLedger(t *testing.T) {
	now := time.Now()
	run := func(kind runecraftplan.EndKind, why string, mod func(r *masterRig, w *masterNatureWorld)) (*masterRig, *masterNatureWorld) {
		r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 61200, "Rune essence": 100, "Coins": 11_229_284})
		w := newCampaignWorld(t, r, nil)
		r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Started: now.Add(-6 * time.Hour), Crafted: 100000, Rounds: 3, OwnTrips: 1, HandedOver: 20300}
		if mod != nil {
			mod(r, w)
		}
		w.camp.latch(kind, why)
		if !w.campaignRoundEnd(natureLoopResult{Done: true}) {
			t.Errorf("%s: a round that ended by a campaign rule is decided by it", kind)
		}
		return r, w
	}
	// The target finishes it: not active, disarmed, one done, a line that says what it made.
	r, _ := run(runecraftplan.EndTarget, "100,000 crafted", nil)
	c := r.b.ledger.Rune.Campaign
	if c.Active || !c.Disarmed || c.Done != 1 || !r.logged("CAMPAIGN DONE - campaign 1 crafted 100,000 of 100,000 Nature runes in 6h0m0s over 3 round(s)") {
		t.Fatalf("%+v\n%s", c, strings.Join(r.logs, "\n"))
	}
	if !r.logged("it re-arms when the mule's Nature runes fall under 1,000") {
		t.Fatalf("log:\n%s", strings.Join(r.logs, "\n"))
	}
	// The round's time pauses it for 45 minutes.
	r, _ = run(runecraftplan.EndTime, "eight hours", nil)
	if c := r.b.ledger.Rune.Campaign; !c.Active || c.PausedUntil.Before(now.Add(44*time.Minute)) || c.PausedUntil.After(now.Add(46*time.Minute)) {
		t.Fatalf("%+v", c)
	}
	// A hand-over trip goes straight on.
	r, _ = run(runecraftplan.EndHandOver, "own trip", nil)
	if c := r.b.ledger.Rune.Campaign; !c.Active || !c.PausedUntil.IsZero() || !r.logged("the campaign goes straight on") {
		t.Fatalf("%+v", c)
	}
	// The gate ran dry with the mule short of essence and its coins to buy: no pause, the next round refills.
	lacking := func(r *masterRig, w *masterNatureWorld) { r.b.ledger.Rune.Campaign.Crafted = 20000 }
	r, _ = run(runecraftplan.EndDry, "no essence", lacking)
	if c := r.b.ledger.Rune.Campaign; !c.Active || c.PausedUntil.After(time.Now()) || !r.logged("the next round refills at once") {
		t.Fatalf("%+v\n%s", c, strings.Join(r.logs, "\n"))
	}
	// Dry with the mule holding essence enough: the carriers are the trouble, a pause (a refill would only buy more of what is not used).
	enough := func(r *masterRig, w *masterNatureWorld) {
		lacking(r, w)
		republishMule(r, w, map[string]int{"Nature rune": 700, "Rune essence": 90000, "Coins": 11_229_284})
	}
	r, _ = run(runecraftplan.EndDry, "no essence", enough)
	if c := r.b.ledger.Rune.Campaign; c.PausedUntil.Before(now.Add(44*time.Minute)) || !r.logged("no refill: the mule holds 90,000 essence, enough") {
		t.Fatalf("%+v\n%s", c, strings.Join(r.logs, "\n"))
	}
	// Dry with nothing to buy with (a poor mule): a pause as well.
	poor := func(r *masterRig, w *masterNatureWorld) {
		lacking(r, w)
		republishMule(r, w, map[string]int{"Nature rune": 700, "Rune essence": 100, "Coins": 8000})
	}
	r, _ = run(runecraftplan.EndDry, "no essence", poor)
	if c := r.b.ledger.Rune.Campaign; c.PausedUntil.Before(now.Add(44*time.Minute)) || !r.logged("no refill:") {
		t.Fatalf("%+v\n%s", c, strings.Join(r.logs, "\n"))
	}
	// A round that did not end by the rule (stopped, stuck, a failed stage) leaves the campaign as it was.
	r = newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 700})
	w := newCampaignWorld(t, r, nil)
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 500}
	if w.campaignRoundEnd(natureLoopResult{Stuck: true, Reason: "no talisman"}) {
		t.Fatal("a stuck round is not decided by a campaign rule")
	}
	w.camp.latch(runecraftplan.EndTarget, "x")
	if w.campaignRoundEnd(natureLoopResult{Transient: true}) {
		t.Fatal("a failed round is not decided by a campaign rule")
	}
	w.camp.endKind = runecraftplan.EndNone
	if c := r.b.ledger.Rune.Campaign; !c.Active || c.Disarmed || c.LastEnd != "" {
		t.Fatalf("%+v", c)
	}
}

// A hand-over that failed counts in a row, one that worked clears the count, and the campaign gives up at craftMaxHandoverFailures.
func TestCampaignGivesUpOnFailingHandOvers(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 700, "Rune essence": 9000, "Nature talisman": 7})
	r.b.set.MaxHandoverFailures = 3
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1}
	for i := 1; i <= 3; i++ {
		w := newCampaignWorld(t, r, nil)
		w.handOverTried, w.handOverFailed = true, true
		w.campaignRoundEnd(natureLoopResult{})
		if got := r.b.ledger.Rune.Campaign.HandOverFails; got != i {
			t.Fatalf("after %d failures: %d", i, got)
		}
	}
	if !r.b.ledger.Rune.Campaign.GivenUp(r.b.set) {
		t.Fatal("three in a row give it up")
	}
	// A restart tries again.
	r.b.clearCampaignGiveUp(r.ctx)
	if r.b.ledger.Rune.Campaign.HandOverFails != 0 || !r.logged("this restart tries again") {
		t.Fatalf("%+v\n%v", r.b.ledger.Rune.Campaign, r.logs)
	}
	// One that worked clears the count.
	r.b.ledger.Rune.Campaign.HandOverFails = 2
	w := newCampaignWorld(t, r, nil)
	w.handOverTried, w.handOverFailed = true, false
	w.campaignRoundEnd(natureLoopResult{})
	if r.b.ledger.Rune.Campaign.HandOverFails != 0 {
		t.Fatal("a good hand-over clears the count")
	}
}

// ---- the keep and the hand-over ----

// The gate pack is drawn with the keep: the Natures the bank holds are topped into the pack up to craftRuneKeep.
func TestCampaignProvisionDrawsTheKeep(t *testing.T) {
	r := newCampaignRig(t, nil, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsObj, Count: 80000}, {ID: natRune, Count: 9000}}, map[string]int{"Nature talisman": 7})
	w := newCampaignWorld(t, r, nil)
	if why, ok := w.provision(); !ok {
		t.Fatalf("%s\n%v", why, r.logs)
	}
	p := r.sim.Pack()
	if simCount(p, natRune) != 4000 || simCount(p, natTal) != 1 || simCount(p, coinsObj) != runecraftplan.GateFare || len(p) != 3 {
		t.Fatalf("the gate pack is the talisman, the keep and the fare: %v", p)
	}
	if b := r.sim.Bank(); simCount(b, natRune) != 5000 {
		t.Fatalf("bank %v", b)
	}
	if !r.logged("4,000 Natures kept (keep 4,000; 5,000 more in the bank)") {
		t.Fatalf("log: %v", r.logs)
	}
}

// The end of a round counts the pack and the bank together: a keep banked by an earlier round is not kept twice. Pack 5,000 and bank
// 3,000 are 8,000, 4,000 beyond the keep, which is what crosses.
func TestFinishNatureHandsOverTheStockBeyondTheKeepOnce(t *testing.T) {
	r := newCampaignRig(t, []actionsim.Stack{{ID: natRune, Count: 5000}, {ID: natTal, Count: 1}, {ID: coinsObj, Count: 100}},
		[]actionsim.Stack{{ID: natRune, Count: 3000}}, map[string]int{"Nature rune": 700, "Rune essence": 9000})
	r.sim.StandAt(2809, 3441)
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1}
	w := newCampaignWorld(t, r, nil)
	w.camp.latch(runecraftplan.EndHandOver, "own trip")
	var delivered int
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, mule string, _ tile.Tile, rows, slots int, items []string) runerunner.Delivery {
		delivered = simCount(r.sim.Pack(), natRune)
		r.sim.Remove(natRune, delivered)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: 1}, Delivered: map[string]int{"Nature rune": delivered}}
	}
	what, stranded := w.finishNature()
	if stranded || delivered != 4000 || w.handed != 4000 || w.handOverFailed || !w.handOverTried {
		t.Fatalf("%q stranded %v, delivered %d, handed %d\n%s", what, stranded, delivered, w.handed, strings.Join(r.logs, "\n"))
	}
	if b := r.sim.Bank(); simCount(b, natRune) != 4000 || simCount(b, natTal) != 1 {
		t.Fatalf("the keep stays banked once, with the talisman: %v", b)
	}
	if len(r.sim.Pack()) != 0 {
		t.Fatalf("the pack is banked: %v", r.sim.Pack())
	}
	c := r.b.ledger.Rune.Campaign
	if c.HandedOver != 4000 || c.OwnTrips != 1 {
		t.Fatalf("an own trip handed 4,000: %+v", c)
	}
	if g := r.b.ledger.Rune.Gate.HandedAtEnd; g != 4000 {
		t.Fatalf("the gate ledger: %d", g)
	}
	// With the stock at the keep or under it nothing is handed over.
	r2 := newCampaignRig(t, []actionsim.Stack{{ID: natRune, Count: 1000}}, []actionsim.Stack{{ID: natRune, Count: 3000}}, map[string]int{"Nature rune": 700})
	w2 := newCampaignWorld(t, r2, nil)
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		t.Errorf("nothing beyond the keep: no trade")
		return runerunner.Delivery{}
	}
	if _, stranded := w2.finishNature(); stranded || w2.handOverTried || w2.handed != 0 {
		t.Fatalf("%+v", w2)
	}
	if b := r2.sim.Bank(); simCount(b, natRune) != 4000 {
		t.Fatalf("bank %v", b)
	}
}

// A mule that is not there leaves the Natures banked and counts as a failed hand-over.
func TestFinishNatureWithTheMuleAwayFailsTheHandOver(t *testing.T) {
	r := newCampaignRig(t, []actionsim.Stack{{ID: natRune, Count: 9000}}, nil, map[string]int{"Nature rune": 700})
	r.ctx.Bus = mulebus.NewBus() // nobody is there
	w := newCampaignWorld(t, r, nil)
	what, _ := w.finishNature()
	if !w.handOverTried || !w.handOverFailed || w.handed != 0 || !strings.Contains(what, "not here") {
		t.Fatalf("%q tried %v failed %v handed %d", what, w.handOverTried, w.handOverFailed, w.handed)
	}
	if b := r.sim.Bank(); simCount(b, natRune) != 9000 {
		t.Fatalf("everything stays banked: %v", b)
	}
}

// ---- the refill: the mule's whole coin stack spent on essence ----

// refillRig is a campaign rig whose mule publishes `coins` and `essence`, with the coin orders, the stalls and the trade as fakes that
// move the simulated pack the way the real ones would.
type refillRig struct {
	r                *masterRig
	w                *masterNatureWorld
	asks             []int
	buys             []essenceplan.BuyOutPlan
	gave, sold, paid int
	handed           []map[string]int
	stallStock       int
	muleGives        int // coins the mule gives an ask (0: all asked)
}

func newRefillRig(t *testing.T, coins, essence int, mod func(rr *refillRig)) *refillRig {
	t.Helper()
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": essence, "Coins": coins, "Nature talisman": 7})
	rr := &refillRig{r: r, stallStock: 1 << 30}
	rr.w = newCampaignWorld(t, r, nil)
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1}
	oldAsk, oldBuy, oldDeliver := masterAskCoins, masterBuyOutExec, masterDeliver
	t.Cleanup(func() { masterAskCoins, masterBuyOutExec, masterDeliver = oldAsk, oldBuy, oldDeliver })
	masterAskCoins = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _, _ string, ask int) runerunner.TopUpResult {
		rr.asks = append(rr.asks, ask)
		n := ask
		if rr.muleGives > 0 {
			n = min(ask, rr.muleGives)
		}
		r.sim.Give(coinsObj, n)
		return runerunner.TopUpResult{Got: n, Orders: (n + 999_999) / 1_000_000}
	}
	masterBuyOutExec = func(_ *Context, _ *fleetrunner.Deps, plan essenceplan.BuyOutPlan, coins int, logf func(string, ...any)) buyOutResult {
		rr.buys = append(rr.buys, plan)
		// Every stall at 100 coins an essence, until the coins or the want or the stock are gone.
		got := min(plan.Qty, coins/100, rr.stallStock)
		rr.stallStock -= got
		rr.sold += got
		rr.paid += got * 100
		r.sim.Remove(coinsObj, got*100)
		r.sim.Give(notedEss, got)
		logf("a fake stall sold %d for %d", got, got*100)
		return buyOutResult{Got: got, Paid: got * 100, Lots: (got + essenceplan.BuyOutLot - 1) / essenceplan.BuyOutLot, Stalls: []string{"Seers Market"}, Reached: true}
	}
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _ string, _ tile.Tile, rows, slots int, items []string) runerunner.Delivery {
		out := map[string]int{}
		for _, it := range items {
			switch it {
			case "Rune essence":
				n := simCount(r.sim.Pack(), notedEss) + simCount(r.sim.Pack(), int32(runecraftplan.EssenceID))
				r.sim.Remove(notedEss, simCount(r.sim.Pack(), notedEss))
				out[it] = n
			case "Coins":
				n := simCount(r.sim.Pack(), coinsObj)
				r.sim.Remove(coinsObj, n)
				out[it] = n
			}
		}
		rr.handed = append(rr.handed, out)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: len(out)}, Delivered: out}
	}
	if mod != nil {
		mod(rr)
	}
	return rr
}

// THE OWNER'S WORDS: "request mules entire cash stack and spend it on everything instead of just running out". The mule holds 11,229,284
// coins and almost no essence; the campaign lacks 100,000: the ask is the whole stack less the 10,000 floor, the stalls take what it buys, the
// essence and the unspent coins go back to the mule in one hand-over.
func TestCampaignSupplyAsksTheWholeStackAndSpendsIt(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	why, ok := rr.w.campaignSupply()
	if !ok || why != "" {
		t.Fatalf("%v %q\n%s", ok, why, strings.Join(rr.r.logs, "\n"))
	}
	if len(rr.asks) != 1 || rr.asks[0] != 11_219_284 {
		t.Fatalf("the ask must be the whole stack less the floor in one trip: %v", rr.asks)
	}
	if rr.sold != 99900 || rr.paid != 9_990_000 {
		t.Fatalf("what the campaign lacks (100,000 less the 100 the mule holds) for 9,990,000: sold %d paid %d", rr.sold, rr.paid)
	}
	if len(rr.handed) != 1 || rr.handed[0]["Rune essence"] != 99900 || rr.handed[0]["Coins"] != 1_229_284 {
		t.Fatalf("the essence and the unspent coins go to the mule: %v", rr.handed)
	}
	if simCount(rr.r.sim.Pack(), coinsObj) != 0 || simCount(rr.r.sim.Pack(), notedEss) != 0 {
		t.Fatalf("nothing is left in the pack: %v", rr.r.sim.Pack())
	}
	c := rr.r.b.ledger.Rune.Campaign
	if c.BuyOuts != 1 || c.Bought != 99900 || c.BoughtSpent != 9_990_000 {
		t.Fatalf("%+v", c)
	}
	for _, want := range []string{
		"BUY-OUT - the mule holds 100 Rune essence (under 5,000) and the campaign lacks 99,900: asking it for its whole coin stack less 10,000",
		"BUY-OUT trip 1: asked mule for 11,219,284 coins in 12 order(s): got 11,219,284",
		"HANDOVER buyout mule=mule",
		"BUY-OUT DONE - 99,900 Rune essence handed to mule for 9,990,000 coins",
	} {
		if !rr.r.logged(want) {
			t.Errorf("log lacks %q:\n%s", want, strings.Join(rr.r.logs, "\n"))
		}
	}
}

// With the mule holding essence enough nothing is asked and nothing bought.
func TestCampaignSupplyDoesNothingWhenTheMuleHasEssence(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 12400, nil)
	if why, ok := rr.w.campaignSupply(); !ok || why != "" || len(rr.asks) != 0 || len(rr.buys) != 0 {
		t.Fatalf("%v %q asks %v buys %d", ok, why, rr.asks, len(rr.buys))
	}
}

// craftRuneBuyAll off: the mule is not asked for a coin; with a shift's worth on the mule the round goes on, with less it does not.
func TestCampaignSupplyWithBuyAllOff(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 3000, func(rr *refillRig) { rr.w.set.BuyAll = false; rr.r.b.set.Rune.BuyAll = false })
	if why, ok := rr.w.campaignSupply(); !ok || why != "" || len(rr.asks) != 0 {
		t.Fatalf("3,000 is a shift's worth: %v %q %v", ok, why, rr.asks)
	}
	rr = newRefillRig(t, 11_229_284, 300, func(rr *refillRig) { rr.w.set.BuyAll = false; rr.r.b.set.Rune.BuyAll = false })
	if why, ok := rr.w.campaignSupply(); ok || !strings.Contains(why, "craftRuneBuyAll is off") || len(rr.asks) != 0 {
		t.Fatalf("300 is no shift: %v %q %v", ok, why, rr.asks)
	}
}

// A poor mule funds no buy-out: under the floor, and what is short is said.
func TestCampaignSupplyWithAPoorMule(t *testing.T) {
	rr := newRefillRig(t, 8000, 300, nil)
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "only 300 Rune essence") || !strings.Contains(why, "not above its 10000 floor") || len(rr.asks) != 0 {
		t.Fatalf("%v %q %v", ok, why, rr.asks)
	}
	rr = newRefillRig(t, 8000, 1200, nil) // a shift's worth is there: the carriers can work on it
	if why, ok := rr.w.campaignSupply(); !ok || why != "" {
		t.Fatalf("%v %q", ok, why)
	}
}

// Trips: craftRuneBuyTrip splits the stack into trips; each trip asks for what is left, buys, and hands over.
func TestCampaignSupplyInTrips(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) { rr.r.b.set.Rune.BuyTrip = 3_000_000; rr.w.set.BuyTrip = 3_000_000 })
	if why, ok := rr.w.campaignSupply(); !ok {
		t.Fatalf("%q\n%s", why, strings.Join(rr.r.logs, "\n"))
	}
	if len(rr.asks) < 4 || rr.asks[0] != 3_000_000 || rr.asks[1] != 3_000_000 || rr.asks[2] != 3_000_000 {
		t.Fatalf("trips of 3,000,000: %v", rr.asks)
	}
	if rr.sold != 99900 {
		t.Fatalf("the whole want is bought over the trips: %d (asks %v)", rr.sold, rr.asks)
	}
	if len(rr.handed) != len(rr.asks) {
		t.Fatalf("each trip hands over: %d hand-overs, %d asks", len(rr.handed), len(rr.asks))
	}
	total := 0
	for _, h := range rr.handed {
		total += h["Rune essence"]
	}
	if total != 99900 {
		t.Fatalf("handed %d", total)
	}
}

// A mule that gives less than asked (its per-order caps): the next trip asks for the rest of the stack.
func TestCampaignSupplyAsksTheRestWhenTheMuleGaveLess(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) { rr.muleGives = 6_000_000 })
	if why, ok := rr.w.campaignSupply(); !ok {
		t.Fatalf("%q\n%s", why, strings.Join(rr.r.logs, "\n"))
	}
	if len(rr.asks) < 2 || rr.asks[0] != 11_219_284 || rr.asks[1] != 5_219_284 {
		t.Fatalf("the second ask is the rest of the stack: %v", rr.asks)
	}
	if rr.sold != 99900 {
		t.Fatalf("sold %d", rr.sold)
	}
}

// The stalls run out: the buy-out takes what they have, hands it over with the coins left, and says so.
func TestCampaignSupplyWhenTheStallsRunOut(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) { rr.stallStock = 7000 })
	why, ok := rr.w.campaignSupply()
	if !ok || why != "" {
		t.Fatalf("7,000 on the mule is a shift's worth: %v %q", ok, why)
	}
	if rr.sold != 7000 || len(rr.handed) == 0 || rr.handed[0]["Rune essence"] != 7000 || rr.handed[0]["Coins"] != 11_219_284-700_000 {
		t.Fatalf("sold %d handed %v", rr.sold, rr.handed)
	}
	// Nothing sold at all: no essence, and the round says why it cannot go on.
	rr = newRefillRig(t, 11_229_284, 100, func(rr *refillRig) { rr.stallStock = 0 })
	why, ok = rr.w.campaignSupply()
	if ok || !strings.Contains(why, "only 100 Rune essence after the buy-out") {
		t.Fatalf("%v %q", ok, why)
	}
	if rr.r.b.ledger.Rune.Campaign.BuyOuts != 0 {
		t.Fatalf("nothing bought, nothing recorded: %+v", rr.r.b.ledger.Rune.Campaign)
	}
}

// A mule that gives no coins ends the refill before any stall is walked to.
func TestCampaignSupplyWithAMuleThatGivesNoCoins(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, func(rr *refillRig) {
		masterAskCoins = func(*fleetrunner.Deps, *donorrunner.Deps, string, string, int) runerunner.TopUpResult {
			return runerunner.TopUpResult{Declined: true, Reason: "none in the bank"}
		}
	})
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "the mule gave no coins for the buy-out") || len(rr.buys) != 0 {
		t.Fatalf("%v %q buys %d", ok, why, len(rr.buys))
	}
}

// A hand-over to the mule that fails after the purchase says where the essence is.
func TestCampaignSupplyWhenTheHandOverFails(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		return runerunner.Delivery{Result: donorrunner.Result{GaveUp: true, Reason: "the mule never met me"}}
	}
	why, ok := rr.w.campaignSupply()
	if ok || !strings.Contains(why, "the hand-over to the mule failed") || !strings.Contains(why, "never met") {
		t.Fatalf("%v %q", ok, why)
	}
	// The coins were spent whatever happened to the essence: the ledger counts them, and the essence that crossed (none).
	if c := rr.r.b.ledger.Rune.Campaign; c.BuyOuts != 1 || c.Bought != 0 || c.BoughtSpent != 9_990_000 || !rr.r.logged("BUY-OUT STOPPED - 0 Rune essence handed to mule for 9,990,000 coins") {
		t.Fatalf("%+v\n%s", c, strings.Join(rr.r.logs, "\n"))
	}
}

// The main account's own bank essence goes to the mule first, with no coins asked for it when that is enough.
func TestCampaignSupplyDeliversTheBanksEssenceFirst(t *testing.T) {
	stackable(t, int32(runecraftplan.EssenceID)) // the simulated pack would otherwise hold 28 of a 6,000 draw
	rr := newRefillRig(t, 11_229_284, 100, nil)
	rr.r.sim.OpenBank([]actionsim.Stack{{ID: int32(runecraftplan.EssenceID), Count: 6000}})
	rr.w.bankKnown, rr.w.bankEssence = true, 6000
	why, ok := rr.w.campaignSupply()
	if !ok || why != "" {
		t.Fatalf("%v %q\n%s", ok, why, strings.Join(rr.r.logs, "\n"))
	}
	if len(rr.asks) != 0 || len(rr.handed) != 1 || rr.handed[0]["Rune essence"] != 6000 {
		t.Fatalf("asks %v handed %v\n%s", rr.asks, rr.handed, strings.Join(rr.r.logs, "\n"))
	}
	if !rr.r.logged("supply: 6,000 Rune essence from the main account's bank crossed to mule") {
		t.Fatalf("log:\n%s", strings.Join(rr.r.logs, "\n"))
	}
}

// The buy-out's plan is the whole stack in the mule's coins and the campaign's remaining want, on the market's world only.
func TestCampaignBuyPlanReadsTheMulesPublishedCoins(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Coins": 11_229_284})
	plan, why := r.b.campaignBuyPlan(r.ctx, 100000, 0, 0, 0)
	if why != "" || plan.Ask != 11_219_284 || plan.Qty != 100000 {
		t.Fatalf("%+v %q", plan, why)
	}
	r.ctx.CurrentWorld = 2
	if _, why := r.b.campaignBuyPlan(r.ctx, 100000, 0, 0, 0); !strings.Contains(why, "do not stand on this world") {
		t.Fatalf("%q", why)
	}
	r.ctx.CurrentWorld = 1
	r.ctx.Peers = nil
	if _, why := r.b.campaignBuyPlan(r.ctx, 100000, 0, 0, 0); !strings.Contains(why, "not published") {
		t.Fatalf("%q", why)
	}
}

// ---- what the trigger reads ----

func TestCampaignSnapshotReadsStandbyAndTheBuyOut(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 100, "Coins": 11_229_284})
	for _, name := range []string{"acct1", "acct2", "acct3"} {
		r.ctx.Bus.Publish(mulebus.StandbyMessage(name, "main", tile.Tile{}))
	}
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct4", "main", tile.Tile{}, mulebus.CarrierWaiting, 25))
	stock, _ := muleStockOf(r)
	snap := mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
	r.b.runeSnapshot(r.ctx, &snap)
	if snap.Rune.Standby != 3 || snap.Rune.Carriers != 1 {
		t.Fatalf("%+v", snap.Rune)
	}
	if snap.Rune.BuyOut != 99900 || snap.Rune.BuyOutWhy != "" {
		t.Fatalf("a buy-out funds the whole want: %+v", snap.Rune)
	}
	// A mule that is poor: none, and why.
	r2 := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 72, "Rune essence": 100, "Coins": 5000})
	stock, _ = muleStockOf(r2)
	snap = mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
	r2.b.campaignSnapshot(r2.ctx, &snap)
	if snap.Rune.BuyOut != 0 || !strings.Contains(snap.Rune.BuyOutWhy, "floor") {
		t.Fatalf("%+v", snap.Rune)
	}
	// The campaign off: nothing is read.
	r2.b.set.Rune.Campaign = false
	snap = mastercraftplan.Snapshot{Now: time.Now(), Stock: stock}
	r2.b.campaignSnapshot(r2.ctx, &snap)
	if snap.Rune.BuyOut != 0 || snap.Rune.Standby != 0 {
		t.Fatalf("%+v", snap.Rune)
	}
}

func muleStockOf(r *masterRig) (mastercraftplan.Stock, bool) {
	if f := muleBankCount(r.ctx, r.b.mule); f != nil {
		return f, true
	}
	return nil, false
}

// Between rounds a campaign that reached the target while the account was away is settled by the check, and a finished one is re-armed.
func TestCampaignReconcileAtTheCheck(t *testing.T) {
	r := newCampaignRig(t, nil, nil, map[string]int{"Nature rune": 61200})
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Number: 1, Crafted: 40000, Started: time.Now().Add(-5 * time.Hour)}
	stock, _ := muleStockOf(r)
	r.b.campaignReconcile(r.ctx, mastercraftplan.Snapshot{Now: time.Now(), Stock: stock})
	c := r.b.ledger.Rune.Campaign
	if c.Active || !c.Disarmed || !r.logged("CAMPAIGN DONE (settled between rounds)") {
		t.Fatalf("%+v\n%v", c, r.logs)
	}
	// The mule's Natures fall under the start level: re-armed.
	republishMule(r, nil, map[string]int{"Nature rune": 300})
	stock, _ = muleStockOf(r)
	r.b.campaignReconcile(r.ctx, mastercraftplan.Snapshot{Now: time.Now(), Stock: stock})
	if c := r.b.ledger.Rune.Campaign; c.Disarmed || !r.logged("re-armed") {
		t.Fatalf("%+v\n%v", c, r.logs)
	}
	// With the campaign off the check reads nothing.
	r.b.set.Rune.Campaign = false
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{Active: true, Crafted: 999999}
	r.b.campaignReconcile(r.ctx, mastercraftplan.Snapshot{Now: time.Now(), Stock: stock})
	if !r.b.ledger.Rune.Campaign.Active {
		t.Fatal("an off campaign is not settled")
	}
}

// The decision through the live check: the mule out of Natures, carriers standing by on the bus, a campaign round is due and it is
// the campaign's.
func TestCampaignDecidedByTheLiveCheck(t *testing.T) {
	r := newCampaignRig(t, nil, []actionsim.Stack{{ID: natTal, Count: 1}}, map[string]int{"Nature rune": 72, "Rune essence": 12400, "Nature talisman": 7})
	r.sim.StandAt(2809, 3441)
	mindHoldStat(r.sim.State(), runecraftrunner.Stat, 55649, 44)
	for _, name := range []string{"acct1", "acct2", "acct3"} {
		r.ctx.Bus.Publish(mulebus.StandbyMessage(name, "main", tile.Tile{}))
	}
	d := r.b.decideLive(r.ctx)
	if d.Mode != mastercraftplan.ModeNature || !d.Campaign || d.Continue || !strings.Contains(d.Why, "nature campaign: start") {
		t.Fatalf("%s (%s)", d.Mode, d.Why)
	}
	// With the mule holding 5,000 Natures it is not.
	republishMule(r, nil, map[string]int{"Nature rune": 5000, "Rune essence": 12400, "Nature talisman": 7})
	if d := r.b.decideLive(r.ctx); d.Mode == mastercraftplan.ModeNature {
		t.Fatalf("%s (%s)", d.Mode, d.Why)
	}
}

func TestCampaignBannerSaysWhatItWillDo(t *testing.T) {
	r := newCampaignRig(t, nil, nil, nil)
	r.b.campaignBanner(r.ctx)
	for _, want := range []string{
		"runecraft campaign: ON (craftRuneCampaign) - starts when the mule's published Nature runes are under 1,000",
		"crafts toward 100,000 Nature runes and stops when the mule holds 60,000",
		"keep 4,000, an own hand-over trip at 14,000 beyond it when no courier has come for 2h0m0s",
		"the whole coin stack less 10,000 is asked of it in one trip and spent on every stall listing Rune essence",
		"no campaign yet",
	} {
		if !r.logged(want) {
			t.Errorf("banner lacks %q:\n%s", want, strings.Join(r.logs, "\n"))
		}
	}
	r.b.set.Rune.Campaign = false
	r.b.campaignBanner(r.ctx)
	if !r.logged("runecraft campaign: off (craftRuneCampaign false)") {
		t.Fatal(r.logs)
	}
}

// ---- the rounds, chained and wired ----

// A campaign's own next round (after an own hand-over trip, a refill) chains straight on, however many rounds that is: the chain limit
// of eight is for unrelated rounds in a row, not for one campaign's. A new gate round decided on the mule's bank (no Continue) waits.
func TestMasterCrafterChainsACampaignThroughItsRounds(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Why: "nature campaign: start"}
	left := 12
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		if len(r.rounds) == 0 {
			return r.decision
		}
		if left > 0 {
			left--
			return mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Continue: true, Why: "nature campaign: go on"}
		}
		return mastercraftplan.Decision{Mode: mastercraftplan.ModeSwarm}
	}
	r.outcome = masterRoundOutcome{Produced: 14000, Handed: 10000}
	r.loop(t)
	rounds := strings.Count(strings.Join(r.events, ","), "round:nature")
	if rounds != 13 || r.events[0] != "stop" || r.events[len(r.events)-1] != "start" {
		t.Fatalf("%d campaign rounds in a row: %v", rounds, r.events)
	}
	// Without Continue the same kind of round waits for the next check.
	r = newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true}
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		return r.decision
	}
	r.outcome = masterRoundOutcome{Produced: 14000}
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:nature,start" {
		t.Fatalf("%q", got)
	}
	// A hand-over trip that crafted nothing but handed the mule the Natures it held is a round that did its work: no pause, no backoff.
	r = newMasterRig(t)
	r.b.set.Rune.On = true
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, Handed: 14000}, time.Now())
	if !r.b.ledger.Rune.Gate.PauseUntil.IsZero() || r.b.ledger.Fails(mastercraftplan.ModeNature) != 0 {
		t.Fatalf("%+v", r.b.ledger.Rune.Gate)
	}
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature}, time.Now())
	if r.b.ledger.Rune.Gate.PauseUntil.IsZero() {
		t.Fatal("a round that made and handed nothing is paused")
	}
}

// A round that ended by one of the campaign's rules (the gate ran dry and a refill is to follow at once) is not also paused as a fruitless gate
// round: the campaign's ledger set what the account waits for.
func TestCampaignDecidedRoundIsNotAlsoPausedAsFruitless(t *testing.T) {
	r := newMasterRig(t)
	r.b.set.Rune.On = true
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, CampaignDecided: true, Why: "no essence came"}, time.Now())
	if !r.b.ledger.Rune.Gate.PauseUntil.IsZero() || r.b.ledger.Fails(mastercraftplan.ModeNature) != 0 {
		t.Fatalf("%+v", r.b.ledger.Rune.Gate)
	}
	// Without a campaign's word, a round that made and handed nothing is paused as before; a STUCK one is paused whatever decided.
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, Why: "no carrier came"}, time.Now())
	if r.b.ledger.Rune.Gate.PauseUntil.IsZero() {
		t.Fatal("a fruitless gate round is paused")
	}
	r.b.ledger.Rune.Gate.PauseUntil = time.Time{}
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, CampaignDecided: true, Stuck: true, Why: "no talisman"}, time.Now())
	if r.b.ledger.Rune.Gate.PauseUntil.IsZero() {
		t.Fatal("a stuck round is paused whatever the campaign decided")
	}
}

// The round of a campaign in order: the campaign begins, the refill spends the mule's whole coin stack, the gate pack is provisioned, and
// only then is the gate walked to. Here the walk to the gate fails, so the round ends where it can be seen.
func TestNatureRoundOfACampaignRefillsThenProvisionsThenWalks(t *testing.T) {
	rr := newRefillRig(t, 11_229_284, 100, nil)
	r := rr.r
	r.sim.StandAt(2809, 3441)
	mindHoldStat(r.sim.State(), runecraftrunner.Stat, 55649, 44)
	r.sim.OpenBank([]actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsObj, Count: 80000}})
	oldKar := masterWalkKaramja
	t.Cleanup(func() { masterWalkKaramja = oldKar })
	var walked []tile.Tile
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		walked = append(walked, dest)
		return false
	}
	r.b.ledger.Rune.Campaign = mastercraftplan.CampaignLedger{} // not started yet
	out := r.b.natureRound(r.ctx, r.b.deps(r.ctx), time.Now(), mastercraftplan.Decision{Mode: mastercraftplan.ModeNature, Campaign: true, Why: "nature campaign: start"})
	if !out.Transient || !strings.Contains(out.Why, "could not walk to the gate") || len(walked) == 0 {
		t.Fatalf("%+v walked %v\n%s", out, walked, strings.Join(r.logs, "\n"))
	}
	c := r.b.ledger.Rune.Campaign
	if !c.Active || c.Number != 1 || c.Rounds != 1 || c.BuyOuts != 1 || c.Bought != 99900 {
		t.Fatalf("%+v", c)
	}
	order := func(sub string) int {
		for i, l := range r.logs {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	start, buy, done, prov := order("CAMPAIGN START"), order("BUY-OUT trip 1"), order("BUY-OUT DONE"), order("provisioned at")
	if start < 0 || buy < start || done < buy || prov < done {
		t.Fatalf("campaign %d, buy-out %d, done %d, provisioned %d:\n%s", start, buy, done, prov, strings.Join(r.logs, "\n"))
	}
	if len(rr.asks) != 1 || rr.asks[0] != 11_219_284 {
		t.Fatalf("one ask for the whole stack: %v", rr.asks)
	}
	// The gate pack was loaded before the walk: the talisman and the fare.
	if p := r.sim.Pack(); simCount(p, natTal) != 0 && simCount(p, natTal) != 1 {
		t.Fatalf("%v", p)
	}
	// A round that failed on the road does not end the campaign or pause it: it backs off through the mode.
	if c.PausedUntil.After(time.Now()) || !c.PausedUntil.IsZero() || !c.Active {
		t.Fatalf("%+v", c)
	}
}

// The chest round's alchemy ask counts the Natures the main account keeps: held in the pack (a gate round that ended early, a keep drawn and not
// banked) or banked at the end of a campaign round, they are his own stock and the mule is not asked for more (owner: "keep some instead of
// trading all then asking back"). With none anywhere the ask is the 2,000 it always was.
func TestChestAskCountsTheNaturesMasterKeeps(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	nat := itemID(t, cfg, "Nature rune", false)
	stock := map[string]int{mastercraftplan.ChestKeyItem: 313, "Nature rune": 90000, "Ruby": 5000}
	ask := func(pack, bank []actionsim.Stack) map[string]int {
		bank = append([]actionsim.Stack{{ID: key, Count: 12}}, bank...)
		r, _ := chestRig(t, pack, bank, stock)
		var items []fleetplan.ItemCount
		scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
			items = it
			return fleetrunner.KitResult{Got: map[string]int{mastercraftplan.ChestKeyItem: 313}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
		}
		c := r.chest(t, cfg, atThePost)
		c.view.Where = mastercraftplan.ChestAtPost
		c.magic = 60
		c.opt = mastercraftplan.ChestKitOptions{Alch: true}
		if ok, _, why := c.ask(); !ok {
			t.Fatalf("the ask failed: %s", why)
		}
		out := map[string]int{}
		for _, it := range items {
			out[it.Item] = it.Count
		}
		return out
	}
	if got := ask(nil, nil); got["Nature rune"] != mastercraftplan.ChestNaturesAsk {
		t.Fatalf("with none anywhere the ask is %d: %v", mastercraftplan.ChestNaturesAsk, got)
	}
	if got := ask([]actionsim.Stack{{ID: nat, Count: 4000}}, nil); got["Nature rune"] != 0 {
		t.Fatalf("4,000 Natures in the PACK (banked first by the ask): no Natures asked of the mule: %v", got)
	}
	if got := ask(nil, []actionsim.Stack{{ID: nat, Count: 4000}}); got["Nature rune"] != 0 {
		t.Fatalf("4,000 Natures in the bank: %v", got)
	}
	if got := ask([]actionsim.Stack{{ID: nat, Count: 1500}}, []actionsim.Stack{{ID: nat, Count: 400}}); got["Nature rune"] != 0 {
		t.Fatalf("1,900 held between the pack and the bank is over the four visits' worth the ask waits for: %v", got)
	}
	// Under four visits' worth (160) the pack's and the bank's together are what the ask tops up from.
	if got := ask([]actionsim.Stack{{ID: nat, Count: 60}}, []actionsim.Stack{{ID: nat, Count: 40}}); got["Nature rune"] != mastercraftplan.ChestNaturesAsk-100 {
		t.Fatalf("100 held between the pack and the bank: %v", got)
	}
}
