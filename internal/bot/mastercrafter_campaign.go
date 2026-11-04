package bot

// The Nature campaign's live half (runecraft P4, docs/runecraft_p4_2026-10-14.md). The decisions are pure and tested apart:
// runecraftplan.DecideCampaignStart / DecideCampaignEnd / OwnTripDue, mastercraftplan's decideCampaign and CampaignLedger,
// essenceplan.PlanBuyOut. This file reads the world for them, acts on what they say and logs it.
//
//	trigger     mastercraftplan.Decide, every CheckEvery: the mule's PUBLISHED Nature rune under craftRuneStartBelow, carriers ready
//	            (the list, or those at work and standing by on the bus), essence on the mule or a buy-out to fund it
//	the start   the round that starts a campaign begins it in the ledger (Begin); the next rounds of it carry on (Continue)
//	the refill  at the start of a round, with the mule holding under craftRuneMinEssence: the main account's own bank essence first, then the
//	            mule's WHOLE coin stack (less craftRuneMuleCoinFloor) asked for and spent at every stall that lists Rune essence,
//	            the essence and the coins left handed back to the mule as notes (a user request: "request mules entire cash
//	            stack and spend it on everything instead of just running out"). It commits nothing before it knows something can
//	            be bought: the price page is read first (campaignStalls: a stall must list the essence, the ask goes no higher than
//	            the stalls list), a buy-out that reached the stalls and bought under essenceplan.BuyOutMin is remembered for hours
//	            (CampaignLedger.NoStockUntil), and a gate that runs dry is refilled at once only once (AllowDryRefill)
//	the gate    P3's round, with the campaign's end rule: the target, the mule full, the round's time, no carriers, no essence, and
//	            the main account's own hand-over trip when no courier is coming
//	the end     the surplus beyond the keep handed to the mule (the bank counted with the pack), the ledger told how it ended
//	            (finished, paused, or going straight on), a line that says what the campaign made
//
// Every number the log prints is the ledger's, so a restart reads the same campaign.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// campaignStockEvery is how long the mule's published bank is read once and kept (the directory copy is the costly part).
	campaignStockEvery = 20 * time.Second
	// campaignSampleEvery is how often the crafted total is sampled for the rate, campaignLineEvery how often the progress line is said.
	campaignSampleEvery = 20 * time.Second
	campaignLineEvery   = 10 * time.Minute
	// supplyBankMin is the essence in the main account's own bank worth a hand-over to the mule: under it the walk and the trade are not worth the load.
	supplyBankMin = 500
	// supplyWalk bounds the walk between the mule's post and the Seers stalls (about 100 tiles).
	supplyWalk = runeLotWalk
)

// masterAskCoins is the whole-stack ask for coins (the RuneRunner's top-up path: orders sized by the mule's per-order cap), unless a
// test replaced it.
var masterAskCoins = runerunner.AskMuleForCoins

// masterMarketListings is the market price page (fleet-wide cache): the live one unless a test replaced it.
var masterMarketListings = fleetrunner.MarketListings

// masterBuyOutExec is the stall purchase of a buy-out: the live one unless a test replaced it. It is given the trip's plan and the coins
// in the pack, walks to the Seers stalls, buys from every stall in the owner's order until the coins or the want are gone, and leaves
// the essence in the pack as notes.
var masterBuyOutExec = masterBuyOutAtSeers

// natureCampaignState is the campaign's part of a gate round.
type natureCampaignState struct {
	// on is craftRuneCampaign; started that campaignRoundStart ran.
	on, started bool
	// The end rule's memory: how long the carriers have been few, the rate window, when each was last sampled and said.
	watch          runecraftplan.CarrierWatch
	rate           runecraftplan.RateWindow
	rateAt, lineAt time.Time
	// endKind and endWhy are the end the rule found (latched: once the round is ending it stays ending); forceEnd ends the round at
	// once for a reason that is not a campaign's.
	endKind  runecraftplan.EndKind
	endWhy   string
	forceEnd string
	// lastCourier is when a courier last took Natures this round; openedAt when the gate opened (the walk to it done, or the round
	// resumed standing there): the end rule's warm-up and dry checks run from it, not from the round's start, which includes the refill.
	lastCourier, openedAt time.Time
	// drySaid is the dry checks already said.
	drySaid int
	// stock is the mule's published bank as last read.
	stock struct {
		at                     time.Time
		nature, essence, coins int
		known                  bool
	}
}

// latch fixes the round's end.
func (s *natureCampaignState) latch(kind runecraftplan.EndKind, why string) {
	if s.endKind == runecraftplan.EndNone {
		s.endKind, s.endWhy = kind, why
	}
}

// muleNumbers is the mule's published Nature runes, Rune essence and coins, read at most every campaignStockEvery; known is false
// when it has not published (or the row is stale), which is not the same as publishing none.
func (w *masterNatureWorld) muleNumbers() (nature, essence, coins int, known bool) {
	s := &w.camp.stock
	now := time.Now()
	if !s.at.IsZero() && now.Sub(s.at) < campaignStockEvery {
		return s.nature, s.essence, s.coins, s.known
	}
	s.at = now
	s.nature, s.essence, s.coins, s.known = 0, 0, 0, false
	if count := muleBankCount(w.ctx, w.b.mule); count != nil {
		s.known = true
		s.nature, _ = count("Nature rune")
		s.essence, _ = count(runecraftplan.EssenceName)
		s.coins, _ = count("Coins")
	}
	return s.nature, s.essence, s.coins, s.known
}

func (w *masterNatureWorld) noteCampaignCraft(n int) {
	if w.camp.on {
		w.b.ledger.Rune.Campaign.NoteCraft(n)
	}
}

func (w *masterNatureWorld) noteCampaignTrade(essence int) {
	if w.camp.on {
		w.b.ledger.Rune.Campaign.NoteTrade(essence)
	}
}

func (w *masterNatureWorld) noteCampaignCourier(natures int, now time.Time) {
	w.camp.lastCourier = now
	if w.camp.on {
		w.b.ledger.Rune.Campaign.NoteCourier(natures)
	}
}

// noteCampaignHandOver records what the main account handed the mule: his own trip when the round ended for it (EndHandOver).
func (w *masterNatureWorld) noteCampaignHandOver(n int) {
	if w.camp.on && n > 0 {
		w.b.ledger.Rune.Campaign.NoteHandOver(n, w.camp.endKind == runecraftplan.EndHandOver)
	}
}

// campaignRoundStart begins the campaign in the ledger (or carries it on) and says so. A round resumed where the account stands, with
// a campaign that already finished, ends at once: there is nothing to craft for.
func (w *masterNatureWorld) campaignRoundStart(dec mastercraftplan.Decision) {
	if !w.camp.on {
		return
	}
	b := w.b
	c := &b.ledger.Rune.Campaign
	now := time.Now()
	nature, essence, _, known := w.muleNumbers()
	switch {
	case c.Active:
		w.logf("CAMPAIGN goes on - round %d of campaign %d, crafted %s of %s Nature runes in %s; the mule holds %s Nature runes and %s essence%s",
			c.Rounds+1, c.Number, runecraftplan.Thousands(c.Crafted), runecraftplan.Thousands(w.set.Target), now.Sub(c.Started).Round(time.Minute),
			muleWord(nature, known), muleWord(essence, known), campaignLastEndWord(c))
	case dec.Resume && c.Disarmed:
		w.camp.forceEnd = "the last campaign finished and the mule's Natures have not fallen under " + runecraftplan.Thousands(w.set.StartBelow) + " since: nothing to craft for"
		w.logf("campaign: resumed at the gate after a finished campaign (%s); the round ends and walks home", c.LastWhy)
		return
	default:
		c.Begin(now, nature)
		w.logf("CAMPAIGN START - campaign %d: the mule holds %s Nature runes (under %s), %s Rune essence; target %s Nature runes, stops when the mule holds %s; Runecrafting %d; %d carrier(s) at work, %d standing by, %d listed; keep %s, own trip at %s beyond it when no courier has come for %s",
			c.Number, muleWord(nature, known), runecraftplan.Thousands(w.set.StartBelow), muleWord(essence, known), runecraftplan.Thousands(w.set.Target), runecraftplan.Thousands(w.set.StopAbove),
			w.level0, len(w.carrierViews()), len(mulebus.ReadStandby(w.ctx.Bus, w.ctx.Username, now, 0)), len(w.set.Runners),
			runecraftplan.Thousands(w.set.Keep), runecraftplan.Thousands(w.set.HandOverAt), w.set.CourierWait)
	}
	c.NoteRound()
	w.camp.started = true
	w.camp.lineAt = now
	if dec.Resume {
		w.camp.openedAt = now // standing at the gate already
	}
	b.saveLedger(w.ctx)
}

func muleWord(n int, known bool) string {
	if !known {
		return "? (not published)"
	}
	return runecraftplan.Thousands(n)
}

func campaignLastEndWord(c *mastercraftplan.CampaignLedger) string {
	if c.LastEnd == "" {
		return ""
	}
	return "; the last round ended: " + runecraftplan.EndKind(c.LastEnd).Describe() + " (" + c.LastWhy + ")"
}

// campaignEnd is the campaign's end rule over the live world; it returns why the round must end now, "" while it goes on.
func (w *masterNatureWorld) campaignEnd() string {
	if w.camp.endKind != runecraftplan.EndNone {
		return w.camp.endWhy
	}
	now := time.Now()
	carriers := w.carrierViews()
	low := w.camp.watch.Note(now, len(carriers), runecraftplan.CampaignMinCarriers)
	nature, _, _, known := w.muleNumbers()
	c := &w.b.ledger.Rune.Campaign
	end, kind, why := runecraftplan.DecideCampaignEnd(runecraftplan.CampaignEndView{
		Now: now, Started: w.started, Opened: w.camp.openedAt, MaxRound: w.set.MaxHours,
		Target: w.set.Target, Crafted: c.Crafted, StopAbove: w.set.StopAbove, MuleNature: nature, MuleKnown: known,
		Carriers: len(carriers), LowSince: low, LastTrade: w.lastTrade,
		Held: runecraftrunner.Natures(w.d), Keep: w.set.Keep, HandOverAt: w.set.HandOverAt, HandOverMin: w.set.HandOverMin,
		CourierOn: w.set.Courier, LastCourier: w.camp.lastCourier, CourierWait: w.set.CourierWait,
	})
	if !end {
		w.sayDryCheck(now)
		return ""
	}
	w.camp.latch(kind, why)
	w.logf("campaign: the round ends - %s (%s)", kind.Describe(), why)
	return why
}

// sayDryCheck says each dry check once: the first of the two is the warning, the second ends the round.
func (w *masterNatureWorld) sayDryCheck(now time.Time) {
	opened := w.camp.openedAt
	if opened.IsZero() || opened.Before(w.started) {
		opened = w.started
	}
	checks := runecraftplan.DryChecks(now, opened, w.lastTrade)
	if checks == 0 {
		w.camp.drySaid = 0
		return
	}
	if checks > w.camp.drySaid && checks < runecraftplan.CampaignDryChecks && now.Sub(opened) >= runecraftplan.GateWarmUp {
		w.camp.drySaid = checks
		nature, essence, _, known := w.muleNumbers()
		w.logf("WARNING: campaign: essence dry - no trade for %s (check %d of %d); the mule holds %s essence and %s Nature runes, %d carrier(s) at work",
			time.Duration(checks)*runecraftplan.CampaignDryCheck, checks, runecraftplan.CampaignDryChecks, muleWord(essence, known), muleWord(nature, known), len(w.carrierViews()))
	}
}

// campaignTick samples the crafted total for the rate and says the progress line every campaignLineEvery.
func (w *masterNatureWorld) campaignTick() {
	if !w.camp.on {
		return
	}
	now := time.Now()
	c := &w.b.ledger.Rune.Campaign
	if now.Sub(w.camp.rateAt) >= campaignSampleEvery {
		w.camp.rateAt = now
		w.camp.rate.Note(now, c.Crafted)
	}
	if w.camp.lineAt.IsZero() {
		w.camp.lineAt = now // the first reading starts the clock: the line is said ten minutes into the round
		return
	}
	if now.Sub(w.camp.lineAt) < campaignLineEvery {
		return
	}
	w.camp.lineAt = now
	nature, essence, _, _ := w.muleNumbers()
	w.logf("%s", runecraftplan.CampaignLine(c.Crafted, w.set.Target, w.camp.rate.PerHour(now), len(w.carrierViews()), essence, nature, runecraftrunner.Natures(w.d), w.set.Keep))
}

// campaignRoundEnd tells the ledger how the round ended: a hand-over's result, then the end the rule found. It reports whether the
// campaign's own ledger has decided what the account waits for now (the round ended by one of its rules): the gate round's fruitless
// pause is then not added to it (a gate that ran dry and is to be refilled at once must not also sit out three hours).
func (w *masterNatureWorld) campaignRoundEnd(res natureLoopResult) (decided bool) {
	if !w.camp.on {
		return false
	}
	b := w.b
	c := &b.ledger.Rune.Campaign
	now := time.Now()
	if w.handOverTried {
		c.NoteHandOverResult(w.handOverFailed)
		if w.handOverFailed {
			w.logf("WARNING: campaign: the hand-over of Nature runes to %s failed (%d in a row, given up at %d)", b.mule, c.HandOverFails, b.set.MaxHandoverFailures)
		}
	}
	kind, why := w.camp.endKind, w.camp.endWhy
	if !res.Done || kind == runecraftplan.EndNone {
		// Stopped, stuck, a failed stage: the campaign keeps its standing and the mode's own backoff or pause holds it.
		b.saveLedger(w.ctx)
		return false
	}
	pause := w.set.Pause
	note := ""
	if kind != runecraftplan.EndDry || w.trades > 0 {
		c.ResetDryRefills() // a round that traded, or ended another way, was not left dry by the refill before it
	}
	if kind == runecraftplan.EndDry {
		if cont, whyNot := b.campaignRefillPossible(w.ctx); !cont {
			if whyNot != "" {
				note = "; no refill: " + whyNot
			}
		} else if c.AllowDryRefill(essenceplan.MaxDryRefills) {
			pause, note = 0, "; the mule's essence is short and a buy-out can fund it: the next round refills at once"
		} else {
			// The round that followed a pause-free refill ran dry again without a single trade: another would only repeat it.
			note = "; the refill before this round did not get the gate trading, so no second one without a pause"
		}
	}
	c.End(kind, why, now, pause)
	switch {
	case kind.Finishes():
		w.logf("CAMPAIGN DONE - %s", b.campaignSummary(c, now))
	case kind.Continues():
		w.logf("campaign: the round ended for an own hand-over trip (%s); %s Nature runes crafted so far, the campaign goes straight on", why, runecraftplan.Thousands(c.Crafted))
	default:
		until := ""
		if !c.PausedUntil.After(now) {
			until = "no pause"
		} else {
			until = "paused until " + c.PausedUntil.Format("15:04")
		}
		w.logf("campaign: the round ended: %s (%s); %s%s; %s Nature runes crafted so far", kind.Describe(), why, until, note, runecraftplan.Thousands(c.Crafted))
	}
	b.saveLedger(w.ctx)
	return true
}

// campaignSummary is the one line of what a campaign made.
func (b *masterCrafterBot) campaignSummary(c *mastercraftplan.CampaignLedger, now time.Time) string {
	r := b.set.Rune
	return fmt.Sprintf("campaign %d crafted %s of %s Nature runes in %s over %d round(s) from %s essence traded; the main account handed the mule %s in %d own trip(s) and at the ends of rounds, couriers carried %s; %d buy-out(s) brought %s essence for %s coins; it re-arms when the mule's Nature runes fall under %s",
		c.Number, runecraftplan.Thousands(c.Crafted), runecraftplan.Thousands(r.Target), now.Sub(c.Started).Round(time.Minute), c.Rounds, runecraftplan.Thousands(c.EssenceTaken),
		runecraftplan.Thousands(c.HandedOver), c.OwnTrips, runecraftplan.Thousands(c.CourierNatures), c.BuyOuts, runecraftplan.Thousands(c.Bought), runecraftplan.Thousands(c.BoughtSpent),
		runecraftplan.Thousands(r.StartBelow))
}

// ---- the plan of a buy-out, and the essence the campaign has to work with ----

// campaignBuyPlan is essenceplan.PlanBuyOut over the live figures: the mule's published coins, the world, the settings. Before it
// says there is a buy-out it checks the two things that decide whether anything can be bought at all, BEFORE the mule is asked for a
// coin (review of 14 Oct 2026): the last buy-out's no-stock memory, and the price page (campaignStalls: a stall must list Rune essence,
// and the ask goes no higher than the stalls list). sold is the essence this buy-out's earlier trips already bought, counted off what
// the page lists.
func (b *masterCrafterBot) campaignBuyPlan(ctx *Context, want, asked, held, sold int) (essenceplan.BuyOutPlan, string) {
	r := b.set.Rune
	if !r.BuyAll {
		return essenceplan.BuyOutPlan{}, "craftRuneBuyAll is off"
	}
	t := essenceplan.BuyOutTerms{
		Want: want, Floor: r.MuleCoinFloor, Held: held, Asked: asked, Trip: r.BuyTrip,
		OnMarketWorld: fleetplan.MarketOnWorld(ctx.World(), fleetplan.DefaultMarketWorlds()),
	}
	if stock := muleBankCount(ctx, b.mule); stock != nil {
		n, _ := stock("Coins")
		t.MuleKnown, t.MuleCoins = true, n
	}
	plan, why := essenceplan.PlanBuyOut(t)
	if why != "" {
		return plan, why
	}
	if until, quiet := b.ledger.Rune.Campaign.NoStock(time.Now()); quiet {
		return essenceplan.BuyOutPlan{}, "the last buy-out found the stalls short of essence: not asked again until " + until.Format("15:04")
	}
	sup, why := b.campaignStalls(ctx)
	if why != "" {
		return essenceplan.BuyOutPlan{}, "no stall to buy essence from: " + why
	}
	if !sup.Known {
		return plan, "" // a stall does not show its stock: the stalls are asked, uncapped
	}
	if sup.Listed-sold <= 0 {
		return essenceplan.BuyOutPlan{}, "the essence the stalls list is all bought"
	}
	t.Listed = sup.Listed - sold
	return essenceplan.PlanBuyOut(t)
}

// campaignStalls is the price page read for a buy-out: which stalls could sell Rune essence now (not resting after a failure of this
// account's) and how much they list. A page that could not be read is no stall to buy from.
func (b *masterCrafterBot) campaignStalls(ctx *Context) (questplan.BuyOutSupply, string) {
	return questplan.BuyOutStalls(questplan.MarketTerms{
		Order: questplan.PipEssenceMarkets, Item: essenceplan.Essence,
		Listings: questMarketStocks(masterMarketListings(scarabSwarmMarketMaxAge, ctx.Log)),
		Skip:     questMarketResting(ctx.Username, questplan.PipEssenceMarkets, time.Now()),
		Fallback: questplan.PipEssenceUnitFallback, Ceiling: min(questplan.PipEssenceUnitCeiling, essenceplan.MarketUnitCeiling),
	})
}

// stallsWord says what campaignStalls found, for the log.
func stallsWord(s questplan.BuyOutSupply) string {
	if len(s.Stalls) == 0 {
		return "no stall"
	}
	list := strings.Join(s.Stalls, ", ")
	if !s.Known {
		return list + " (stock not all shown)"
	}
	return fmt.Sprintf("%s (%s listed in all)", list, runecraftplan.Thousands(s.Listed))
}

// campaignRefillPossible says whether the next round could refill the mule's essence: it is under craftRuneMinEssence and a buy-out
// could fund some.
func (b *masterCrafterBot) campaignRefillPossible(ctx *Context) (bool, string) {
	r := b.set.Rune
	stock := muleBankCount(ctx, b.mule)
	if stock == nil {
		return false, "the mule has not published its bank"
	}
	essence, _ := stock(runecraftplan.EssenceName)
	if essence >= r.MinEssence {
		return false, fmt.Sprintf("the mule holds %s essence, enough: the carriers are the trouble, not the supply", runecraftplan.Thousands(essence))
	}
	need := runecraftplan.CampaignNeed(r.Target, b.ledger.Rune.Campaign.CraftedNow(), essence, 0, 0)
	if _, why := b.campaignBuyPlan(ctx, need, 0, 0, 0); why != "" {
		return false, why
	}
	return true, ""
}

// campaignSnapshot adds what the trigger reads that only the bot knows: the carriers standing by on the bus and what a buy-out could fund.
func (b *masterCrafterBot) campaignSnapshot(ctx *Context, snap *mastercraftplan.Snapshot) {
	r := b.set.Rune
	if !r.Campaign {
		return
	}
	in := &snap.Rune
	if ctx.Bus != nil && ctx.Username != "" {
		in.Standby = len(mulebus.ReadStandby(ctx.Bus, ctx.Username, time.Now(), 0))
	}
	if snap.Stock == nil {
		in.BuyOutWhy = "the mule has not published its bank"
		return
	}
	essence, _ := snap.Stock(runecraftplan.EssenceName)
	// The trigger reads a buy-out only when it could matter: the mule short of essence and a campaign that could start or go on. The
	// price page is not fetched for any other check.
	c := &b.ledger.Rune.Campaign
	if essence >= r.MinEssence || c.GivenUp(b.set) {
		return
	}
	if _, paused := c.Paused(time.Now()); paused {
		return
	}
	if nature, _ := snap.Stock("Nature rune"); !c.Active && nature >= r.StartBelow {
		return
	}
	// A campaign that is not under way starts from zero: the last one's Crafted is not this one's (the review's finding).
	need := runecraftplan.CampaignNeed(r.Target, c.CraftedNow(), essence, 0, 0)
	plan, why := b.campaignBuyPlan(ctx, need, 0, 0, 0)
	if why != "" {
		in.BuyOutWhy = why
		return
	}
	in.BuyOut = plan.Qty
}

// campaignReconcile settles a campaign that was active while no round ran (a restart, a pause): finished at the target or the full
// mule, a finished one re-armed when the mule's Natures fall under the start level.
func (b *masterCrafterBot) campaignReconcile(ctx *Context, snap mastercraftplan.Snapshot) {
	r := b.set.Rune
	if !r.On || !r.Campaign {
		return
	}
	nature, known := 0, snap.Stock != nil
	if known {
		nature, _ = snap.Stock("Nature rune")
	}
	c := &b.ledger.Rune.Campaign
	if msg := c.Reconcile(r, nature, known, snap.Now); msg != "" {
		if !c.Active && !c.Disarmed {
			b.logf(ctx, "campaign: %s (the mule holds %d Nature runes)", msg, nature)
		} else {
			b.logf(ctx, "CAMPAIGN DONE (settled between rounds) - %s", b.campaignSummary(c, snap.Now))
		}
		b.saveLedger(ctx)
	}
}

// clearCampaignGiveUp is what a restart does to a campaign given up on failed hand-overs of Natures: the count starts again.
func (b *masterCrafterBot) clearCampaignGiveUp(ctx *Context) {
	c := &b.ledger.Rune.Campaign
	if c.GivenUp(b.set) {
		b.logf(ctx, "WARNING: the Nature campaign was given up because %d hand-overs of Nature runes to %s failed in a row; this restart tries again", c.HandOverFails, b.mule)
		c.HandOverFails = 0
		b.saveLedger(ctx)
	}
}

// campaignBanner says at the start what the campaign is set to do and where it stands.
func (b *masterCrafterBot) campaignBanner(ctx *Context) {
	r := b.set.Rune
	c := &b.ledger.Rune.Campaign
	if !r.Campaign {
		b.logf(ctx, "runecraft campaign: off (%s false)", mastercraftplan.KeyRuneCampaign)
		return
	}
	buy := "buy-out off (craftRuneBuyAll false)"
	if r.BuyAll {
		trip := "in one trip"
		if r.BuyTrip > 0 {
			trip = fmt.Sprintf("in trips of %s coins", runecraftplan.Thousands(r.BuyTrip))
		}
		buy = fmt.Sprintf("when the mule holds under %s essence the whole coin stack less %s is asked of it %s and spent on every stall listing Rune essence", runecraftplan.Thousands(r.MinEssence), runecraftplan.Thousands(r.MuleCoinFloor), trip)
	}
	state := "no campaign yet"
	switch {
	case c.Active:
		state = fmt.Sprintf("campaign %d is ACTIVE since %s: %s of %s Nature runes crafted", c.Number, c.Started.Format("Jan 2 15:04"), runecraftplan.Thousands(c.Crafted), runecraftplan.Thousands(r.Target))
		if until, paused := c.Paused(time.Now()); paused {
			state += ", paused until " + until.Format("15:04") + " (" + c.LastWhy + ")"
		}
	case c.Disarmed:
		state = fmt.Sprintf("campaign %d finished (%s), disarmed until the mule's Nature runes fall under %s", c.Number, c.LastWhy, runecraftplan.Thousands(r.StartBelow))
	case c.Number > 0:
		state = fmt.Sprintf("%d campaign(s) so far", c.Number)
	}
	b.logf(ctx, "runecraft campaign: ON (%s) - starts when the mule's published Nature runes are under %s and Runecrafting is 44 on a members world, with %s essence on the mule (or fundable) and %d carriers ready; crafts toward %s Nature runes and stops when the mule holds %s; keep %s, an own hand-over trip at %s beyond it when no courier has come for %s; a round's time, too few carriers or no essence pauses it %s; %s; %s",
		mastercraftplan.KeyRuneCampaign, runecraftplan.Thousands(r.StartBelow), runecraftplan.Thousands(r.MinEssence), r.MinRunners, runecraftplan.Thousands(r.Target), runecraftplan.Thousands(r.StopAbove),
		runecraftplan.Thousands(r.Keep), runecraftplan.Thousands(r.HandOverAt), r.CourierWait, r.Pause, buy, state)
}

// ---- the refill ----

// handToMule hands the mule what the pack holds of items (a trade at its post): the pack's rows are counted before and after, so what
// it reports is what really left. rows and slots describe how the load got into the pack (a bank draw, or a purchase).
func (b *masterCrafterBot) handToMule(ctx *Context, d *fleetrunner.Deps, post tile.Tile, rows, slots int, items []string, tag string) (map[string]int, string) {
	p := scarabMulePresenceOf(ctx, b.mule)
	switch {
	case ctx.Bus == nil || ctx.Username == "":
		return nil, "no wall bus to hand over on"
	case !p.Heard || !p.SameWorld:
		return nil, "the mule is not here: " + p.Why
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	rd.HomeWorld = 0 // gated on the mule being on this world: no hop
	r := masterDeliver(d, rd, b.mule, post, rows, slots, items)
	result := "traded"
	switch {
	case r.Hopping:
		result = "hopping"
	case r.GaveUp:
		result = "gave-up"
	case !r.Traded:
		result = "not-traded"
	}
	b.logf(ctx, "HANDOVER %s mule=%s delivered=%s result=%s reason=%q", tag, b.mule, masterDescribe(r.Delivered), result, r.Reason)
	if !r.Traded {
		return r.Delivered, fmt.Sprintf("the trade did not happen (%s): %s", result, r.Reason)
	}
	return r.Delivered, ""
}

// deliverBankEssence draws the main account's own bank's Rune essence as one note stack and hands it to the mule: the levelling round's
// leftovers, a refill whose delivery failed. He has no use for it (Runecrafting is past 44), the carriers' kits are made of it.
func (w *masterNatureWorld) deliverBankEssence() (delivered int, why string) {
	b, ctx, d := w.b, w.ctx, w.d
	n := w.bankEssence
	h, _ := mastercraftrunner.DrawLoot(d, w.post, []mastercraftrunner.LootWant{{ID: int32(runecraftplan.EssenceID), Name: runecraftplan.EssenceName, Qty: n}})
	if !h.Opened || h.Err != "" && h.Rows == 0 {
		return 0, "drawing the essence: " + h.Err
	}
	if h.Rows == 0 {
		return 0, "none of the " + runecraftplan.Thousands(n) + " essence was in the bank to draw"
	}
	got, why := b.handToMule(ctx, d, w.post, h.Rows, h.Slots, []string{runecraftplan.EssenceName}, "essence")
	delivered = got[runecraftplan.EssenceName]
	// What the trade left in the pack goes back into the bank.
	if v := mastercraftrunner.DepositAll(d, w.post); v.Opened {
		ess, tal, kit := runecraftrunner.Counts(v.Banked)
		w.noteBank(ess, tal, kit)
	}
	return delivered, why
}

// campaignSupply is the refill at the start of a campaign round, at the mule's post: the main account's own bank essence goes to the mule, and
// with the mule still under craftRuneMinEssence a buy-out spends its whole coin stack on essence. It reports why the round cannot go on
// (the mule holds under a shift's worth and nothing could be bought).
func (w *masterNatureWorld) campaignSupply() (why string, ok bool) {
	b, ctx, d := w.b, w.ctx, w.d
	r := w.set
	c := &b.ledger.Rune.Campaign
	_, muleEss, _, known := w.muleNumbers()
	delivered := 0
	if w.bankKnown && w.bankEssence >= supplyBankMin {
		w.logf("supply: the main account's bank holds %s Rune essence of his own, no use to him now: handing it to %s as one note stack", runecraftplan.Thousands(w.bankEssence), b.mule)
		n, whyNot := w.deliverBankEssence()
		delivered += n
		if whyNot != "" {
			w.logf("WARNING: supply: the bank's essence did not cross: %s", whyNot)
		} else {
			w.logf("supply: %s Rune essence from the main account's bank crossed to %s", runecraftplan.Thousands(n), b.mule)
		}
	}
	have := muleEss + delivered
	if known && have >= r.MinEssence {
		return "", true
	}
	if !known {
		w.logf("supply: the mule has not published its bank: the essence it holds is not known, no refill is made")
		return "", true
	}
	// What the carriers can draw is the mule's: the main account's own bank holds none that counts (what it held has just crossed, or could not).
	need := runecraftplan.CampaignNeed(r.Target, c.CraftedNow(), have, 0, 0)
	plan, whyNot := b.campaignBuyPlan(ctx, need, 0, runecraftrunner.Coins(d), 0)
	if whyNot != "" {
		w.logf("supply: the mule holds %s Rune essence, under craftRuneMinEssence %s, and no buy-out: %s", runecraftplan.Thousands(have), runecraftplan.Thousands(r.MinEssence), whyNot)
		if have >= runecraftplan.ShiftMinEssence {
			return "", true // the carriers can still start shifts on it
		}
		return fmt.Sprintf("the mule holds only %s Rune essence and no buy-out can add to it: %s", runecraftplan.Thousands(have), whyNot), false
	}
	sup, _ := b.campaignStalls(ctx) // the same cached page the plan was made from
	w.logf("BUY-OUT - the mule holds %s Rune essence (under %s) and the campaign lacks %s: asking it for its whole coin stack less %s (%s coins asked at most, %d trip(s) at most) to spend on every stall listing Rune essence, up to %s essence; the price page lists it at %s",
		runecraftplan.Thousands(have), runecraftplan.Thousands(r.MinEssence), runecraftplan.Thousands(need), runecraftplan.Thousands(r.MuleCoinFloor),
		runecraftplan.Thousands(plan.Whole), plan.Trips, runecraftplan.Thousands(plan.Qty), stallsWord(sup))
	want0 := need
	spent, bought, asked, sold, reached := 0, 0, 0, 0, false
	for trip := 1; trip <= essenceplan.BuyOutMaxTrips; trip++ {
		t := w.buyOutTrip(trip, plan)
		bought += t.Essence
		spent += t.Paid
		asked += t.Asked
		sold += t.Sold
		reached = reached || t.Reached
		if t.Stop {
			why = t.Why
			break
		}
		need = runecraftplan.CampaignNeed(r.Target, c.CraftedNow(), have+bought, 0, 0)
		if t.Why != "" {
			w.logf("supply: trip %d: %s", trip, t.Why)
			break
		}
		plan, whyNot = b.campaignBuyPlan(ctx, need, asked, runecraftrunner.Coins(d), sold)
		if whyNot != "" || need < essenceplan.BuyOutMin || t.Essence <= 0 {
			break
		}
	}
	// A buy-out that reached the stalls and bought under the least worth a trip is not asked for again for hours: the next attempt
	// would ask the mule for its whole stack, walk it to Seers and back, and find the same empty stalls. A stop in the middle of the
	// buying says nothing about the stalls.
	switch {
	case !reached || ctx.Stopped():
	case essenceplan.BuyOutShort(sold, want0):
		c.NoteNoStock(time.Now(), essenceplan.BuyOutNoStockWait)
		w.logf("WARNING: BUY-OUT came up short - the stalls sold %s Rune essence of the %s wanted (under the %s worth a trip): no buy-out again until %s",
			runecraftplan.Thousands(sold), runecraftplan.Thousands(want0), runecraftplan.Thousands(min(essenceplan.BuyOutMin, want0)), c.NoStockUntil.Format("15:04"))
		b.saveLedger(ctx)
	case !c.NoStockUntil.IsZero():
		c.NoteStock()
		b.saveLedger(ctx)
	}
	if bought > 0 || spent > 0 {
		c.NoteBuyOut(bought, spent)
		word := "BUY-OUT DONE"
		if why != "" {
			word = "BUY-OUT STOPPED"
		}
		w.logf("%s - %s Rune essence handed to %s for %s coins; the mule should now hold about %s essence", word, runecraftplan.Thousands(bought), b.mule, runecraftplan.Thousands(spent), runecraftplan.Thousands(have+bought))
		b.saveLedger(ctx)
	}
	switch {
	case why != "":
		w.logf("WARNING: supply: the buy-out stopped: %s", why)
	case bought <= 0:
		w.logf("WARNING: supply: the buy-out bought nothing")
	}
	if have+bought >= runecraftplan.ShiftMinEssence {
		return "", true
	}
	if why == "" {
		why = "the buy-out bought nothing"
	}
	return fmt.Sprintf("the mule holds only %s Rune essence after the buy-out: %s", runecraftplan.Thousands(have+bought), why), false
}

// tripResult is what one trip of a buy-out came to: the essence that crossed to the mule, the coins spent and the coins the mule gave,
// Stop that the refill must end (the bot was stopped, or something the next trip would only repeat) and Why how the trip fell short.
// Sold is the essence the stalls sold (it may not have crossed), Reached says the walk to the stalls arrived: a trip that never got
// there says nothing about what the stalls had.
type tripResult struct {
	Essence, Paid, Asked int
	Sold                 int
	Reached              bool
	Stop                 bool
	Why                  string
}

// buyOutTrip is one trip of a buy-out: the pack banked, the coins asked of the mule, the walk to the stalls and the buying, the walk
// back, and the essence and what is left of the coins handed to the mule.
func (w *masterNatureWorld) buyOutTrip(trip int, plan essenceplan.BuyOutPlan) (res tripResult) {
	b, ctx, d := w.b, w.ctx, w.d
	fail := func(stop bool, why string) tripResult { res.Stop, res.Why = stop, why; return res }
	// Room first: the pack is banked, the Natures with it (the gate pack is drawn after the refill).
	if v := mastercraftrunner.DepositAll(d, w.post); !v.Opened {
		return fail(true, "the bank at the mule's post would not open: "+v.Err)
	}
	rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
	WireDonorWorlds(ctx, rd)
	rd.HomeWorld = 0
	a := masterAskCoins(d, rd, b.mule, ctx.Username, plan.Ask)
	if a.Stopped {
		return fail(true, "stopped")
	}
	res.Asked = a.Got
	coins := runecraftrunner.Coins(d)
	w.logf("BUY-OUT trip %d: asked %s for %s coins in %d order(s): got %s (%s coins in the pack)%s", trip, b.mule, runecraftplan.Thousands(plan.Ask), a.Orders,
		runecraftplan.Thousands(a.Got), runecraftplan.Thousands(coins), whySuffix(a.Reason))
	if coins < essenceplan.MarketUnitCeiling {
		return fail(true, fmt.Sprintf("the mule gave no coins for the buy-out (%d held)%s", coins, whySuffix(a.Reason)))
	}
	buy := plan
	buy.MaxCoins = coins
	lot := masterBuyOutExec(ctx, d, buy, coins, func(f string, args ...any) { w.logf("buy-out: "+f, args...) })
	res.Reached, res.Sold = lot.Reached, lot.Got
	if lot.Stopped {
		return fail(true, "stopped")
	}
	res.Paid = lot.Paid
	w.logf("buy-out: trip %d bought %s Rune essence for %s coins in %d lot(s) (%s)%s", trip, runecraftplan.Thousands(lot.Got), runecraftplan.Thousands(lot.Paid), lot.Lots,
		strings.Join(lot.Stalls, ", "), whySuffix(lot.Why))
	// Back to the post with the essence and the coins that are left, and hand them over.
	if !masterWalkTo(d, w.post, 3, supplyWalk) {
		return fail(true, fmt.Sprintf("the walk back to %s's post did not arrive (at %s) with %s essence notes and %s coins", b.mule, fleetrunner.HereIs(d),
			runecraftplan.Thousands(runecraftrunner.Notes(d)), runecraftplan.Thousands(runecraftrunner.Coins(d))))
	}
	notes, left := runecraftrunner.Notes(d), runecraftrunner.Coins(d)
	if notes <= 0 && left <= 0 {
		return fail(false, "nothing came back to hand over: "+firstNonEmpty(lot.Why, "no stall sold any"))
	}
	slots := 0
	for _, n := range []int{notes, left} {
		if n > 0 {
			slots++
		}
	}
	delivered, whyNot := b.handToMule(ctx, d, w.post, 1, slots, []string{runecraftplan.EssenceName, "Coins"}, "buyout")
	res.Essence = delivered[runecraftplan.EssenceName]
	w.logf("buy-out: trip %d handed %s Rune essence and %s coins (the unspent rest) to %s", trip, runecraftplan.Thousands(res.Essence), runecraftplan.Thousands(delivered["Coins"]), b.mule)
	if whyNot != "" {
		return fail(true, "the hand-over to the mule failed: "+whyNot+" (the essence goes into the main account's bank at the next visit and crosses the next time)")
	}
	if lot.Got <= 0 {
		return fail(false, firstNonEmpty(lot.Why, "no stall sold any essence"))
	}
	return res
}

// ---- the stalls ----

// buyOutResult is what a trip's stall purchases came to.
type buyOutResult struct {
	// Got is the essence that arrived (as notes), Paid the coins it cost, Lots how many purchases bought something and Stalls where from.
	Got, Paid, Lots int
	Stalls          []string
	Why             string
	Stopped         bool
	// Reached says the walk to the stalls arrived: only then does what they sold (or did not) say anything about their stock.
	Reached bool
}

// masterBuyOutAtSeers is the live buy-out: the walk to the Seers stalls, then every stall that lists Rune essence in the owner's order
// (Seers Market, Tradevault, Sil Stuff), a lot at a time (essenceplan.BuyOutLot), a stall that lists less than the want bought out and
// left, until the coins or the want are gone. The woodcutters' live-proven stall buyer is the one that trades; the price page's rows
// come off the sold stock as the lots land. The essence is left in the pack as notes: this trip's end is the mule, not a bank.
func masterBuyOutAtSeers(ctx *Context, d *fleetrunner.Deps, plan essenceplan.BuyOutPlan, coins int, logf func(string, ...any)) (res buyOutResult) {
	stand := questplan.SeersMarketStand
	if !masterWalkTo(d, stand, 4, supplyWalk) {
		res.Why = "the walk to the Seers stalls did not arrive (at " + fleetrunner.HereIs(d) + ")"
		return res
	}
	res.Reached = true
	tried := questMarketResting(ctx.Username, questplan.PipEssenceMarkets, time.Now())
	stocks := questMarketStocks(masterMarketListings(scarabSwarmMarketMaxAge, ctx.Log))
	want := plan.Qty
	for lots := 0; lots < essenceplan.BuyOutMaxLots && want > 0; lots++ {
		if ctx.Stopped() {
			res.Stopped = true
			return res
		}
		held := runecraftrunner.Coins(d)
		pick, why := questplan.PickBuyOut(questplan.MarketTerms{
			Order: questplan.PipEssenceMarkets, Item: essenceplan.Essence, Want: want, Listings: stocks, Skip: tried,
			Coins: min(held, plan.MaxCoins-res.Paid), Fallback: questplan.PipEssenceUnitFallback, Ceiling: min(questplan.PipEssenceUnitCeiling, essenceplan.MarketUnitCeiling),
		}, essenceplan.BuyOutLot)
		if pick.Market == "" {
			res.Why = firstNonEmpty(res.Why, "no stall left to buy "+runecraftplan.Thousands(want)+" Rune essence from: "+why)
			if res.Got > 0 {
				res.Why = "" // every stall that could sell has: the buy-out is done, not failed
			}
			return res
		}
		logf("asking %s for %s Rune essence at %d each (at most %d each, %s coins in all; %s coins held, %s still wanted)", pick.Market, runecraftplan.Thousands(pick.Qty), pick.Unit,
			pick.UnitCap, runecraftplan.Thousands(pick.MaxTotal), runecraftplan.Thousands(held), runecraftplan.Thousands(want))
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
		paid := held - runecraftrunner.Coins(d)
		if r.Got > 0 || paid > 0 || r.Paid > 0 {
			res.Got += r.Got
			res.Paid += max(paid, r.Paid)
			res.Lots++
			res.Why = ""
			res.Stalls = append(res.Stalls, fmt.Sprintf("%s %s", pick.Market, runecraftplan.Thousands(r.Got)))
			want -= r.Got
			// Take what was bought off the price page's row; a stall that sold less than asked (its limit, its stock) is not asked again.
			soldOut := r.Got < pick.Qty
			for i := range stocks {
				if strings.EqualFold(stocks[i].Market, pick.Market) && strings.EqualFold(stocks[i].Item, essenceplan.Essence) && stocks[i].Stock > 0 {
					stocks[i].Stock -= r.Got
					soldOut = soldOut || stocks[i].Stock <= 0
				}
			}
			if soldOut {
				tried[pick.Market] = true
			}
			continue
		}
		logf("%s sold no essence: %s", pick.Market, r.Reason)
		questMarketMarkFailed(ctx.Username, pick.Market, time.Now())
		tried[pick.Market] = true
		res.Why = strings.TrimSpace(firstNonEmpty(r.Reason, res.Why))
	}
	return res
}
