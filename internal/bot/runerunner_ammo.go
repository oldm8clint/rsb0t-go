package bot

// RuneRunner's ammo stations (runerunnerplan/ammo.go): Hickton in Catherby, Lowe in Varrock and Gerrant in Port
// Sarim, visited on the way round the rune shops to buy the arrowtips, bolts and feathers users asked for
// (13 October 2026: "runerunner should be buying all arrowtips steel and up + feathers + arrowshafts + bolts
// from all available shops on every world while they do their shopping runs for runes").
//
// An ammo station is the ring's lowest priority, below Zaff's staffs: it spends only what the pack holds beyond
// the fare and the WHOLE pack-paid need of the rune and staff stations still ahead (a short purse buys runes
// first and ammo nothing), within the round's ammoCoins budget, and it never decides a hop, a wait or the
// swarm: the idle wait, the hop's worth and the circuit's worth read the rune stations only. The ammo estimate
// is outside the round's need (RoundNeed.PackNeed/Est/Total), so it never sizes the mule ask, the market float
// or a bank top-up; the coin draw adds it only from coins left over once the runes are covered (bankDraw).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// ammoMule is the mule's published bank as the ammo gate reads it, nil when none was published lately.
func (b *runeRunnerBot) ammoMule(ctx *Context) runerunnerplan.MuleStock {
	f := muleBankCount(ctx, b.set.Mule)
	if f == nil {
		return nil
	}
	return runerunnerplan.MuleStock(f)
}

// ammoItemsAt are the listed ammo parts a site sells, in the setting's priority order. The trawler crew's shelf (Port Khazard) gives
// up only its Rope with khazardRopeOnly: the paste and the buckets on it are the crew's own kit.
func (b *runeRunnerBot) ammoItemsAt(site runerunnerplan.AmmoSite) []string {
	var out []string
	for _, it := range b.set.AmmoItemList {
		if site.Shop.Sells(it) {
			out = append(out, it)
		}
	}
	if site.CrewShelf && b.set.KhazardRopeOnly {
		out = runerunnerplan.RopeOnly(out)
	}
	return out
}

// trawlerBlockWorld is the world of the anchor's Fishing Trawler block while it runs (mulebus.TrawlerBlock), false when none does or
// no bus is heard.
func (b *runeRunnerBot) trawlerBlockWorld(ctx *Context) (int, bool) {
	if b.trawlerBlockSeam != nil {
		return b.trawlerBlockSeam()
	}
	if ctx == nil || ctx.Bus == nil {
		return 0, false
	}
	return mulebus.TrawlerBlock(mulebus.ReadTrawler(ctx.Bus, time.Now(), mulebus.TrawlerBlockTTL), trawlerCrewAnchor)
}

// ammoWanted is the site's listed parts left after the mule-stock gate (ammoStopAbove), and the parts it left
// out with their reasons. pack is the runner's pack, whose ammo counts toward the gate (it is on its way to the
// mule).
func (b *runeRunnerBot) ammoWanted(ctx *Context, site runerunnerplan.AmmoSite, pack map[string]int) ([]string, []runerunnerplan.AmmoSkip) {
	return runerunnerplan.AmmoWanted(b.ammoItemsAt(site), b.set.AmmoStop, b.ammoMule(ctx), func(item string) int { return pack[item] })
}

// ammoPackState is what the pack holds that an ammo stop decides from: its coins, its free slots, and the count
// of every listed ammo part. A test replaces the reading (ammoPackSeam: a rig has no item config to name coins).
func (b *runeRunnerBot) ammoPackState(d *fleetrunner.Deps) (coins, free int, ammo map[string]int) {
	if b.ammoPackSeam != nil {
		return b.ammoPackSeam()
	}
	return fleetrunner.CoinsHeld(d), runerunner.FreeSlots(d), runerunner.PackCounts(d, b.set.AmmoItemList)
}

// ammoEstimate is the board's stock for each listed part a site sells on one world, projected to `at` by the
// content's restock rule from the freshest sighting any visit made (the base when nobody has looked).
func (b *runeRunnerBot) ammoEstimate(site runerunnerplan.AmmoSite, world int, at time.Time) map[string]int {
	board := fleetrunner.ShopsFor(world)
	out := map[string]int{}
	for _, it := range b.ammoItemsAt(site) {
		out[it] = board.Estimate(site.Shop, it, at, b.set.Tick())
	}
	return out
}

// ammoStationCost is what an ammo station is expected to cost when the round reaches it (the purse's
// runerunnerplan.PurseInput.AmmoCost): the wanted parts off the projected shelf under their caps. The mule's
// stock gates the estimate as it gates the visit; the pack's own ammo is read by the station itself.
func (b *runeRunnerBot) ammoStationCost(ctx *Context, s runerunnerplan.Station, at time.Time) int {
	site, ok := runerunnerplan.AmmoSiteFor(s.Site)
	if !ok || !site.Built {
		return 0
	}
	want, _ := runerunnerplan.AmmoWanted(b.ammoItemsAt(site), b.set.AmmoStop, b.ammoMule(ctx), nil)
	return runerunnerplan.AmmoStationCost(site.Shop, b.ammoEstimate(site, s.World, at), want, b.set.AmmoCapTable, 0)
}

// ammoHold is the coins an ammo stop leaves in the pack beyond the fare: the pack-paid need (RoundNeed.PackNeed:
// the shops' estimate, its margin and Zaff's staffs) of every rune and staff station after this one in the ring,
// LESS what the bank can add for them (runerunnerplan.HoldAhead). It is the whole uncovered figure, not cut to half
// the purse: the cap is how a rune visit stays an order of preference instead of a veto, and ammo has no right to the
// other half. Nothing is left to hold at the stop that closes the round, and nothing when the bank holds the runes
// ahead: those stations draw it at their own shops (a user request: spend it on everything).
func (b *runeRunnerBot) ammoHold(ctx *Context) int {
	hold, _, _ := b.holdAhead(ctx)
	return hold
}

// holdAhead is ammoHold with its parts for the skip line: the hold, the pack-paid need ahead and the bank's spare
// toward it (the bank's last reading less the reserve and Lundail's keep, 0 when no reading was taken).
func (b *runeRunnerBot) holdAhead(ctx *Context) (hold, need, bankSpare int) {
	rn := b.roundNeed(ctx, b.st.Cursor+1)
	need = rn.PackNeed()
	bankRead := !b.st.BankReadAt.IsZero()
	if bankRead {
		bankSpare = max(b.st.BankCoins-b.set.CoinReserve-rn.KeepNow(b.st.LundailKeep), 0)
	}
	return runerunnerplan.HoldAhead(need, bankRead, bankSpare), need, bankSpare
}

// ammoStation buys the wanted ammo parts at one site: Hickton, Lowe or Gerrant.
func (b *runeRunnerBot) ammoStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station) {
	now := time.Now()
	key := s.Key()
	site, ok := runerunnerplan.AmmoSiteFor(s.Site)
	switch {
	case !ok || !site.Built:
		b.finish(ctx, skipped("unknown ammo shop %q", s.Site))
		return
	case !b.set.AmmoOn():
		b.finish(ctx, skipped("ammo is off (ammoBuy %v, rotation %v)", b.set.AmmoBuy, b.set.Rotation))
		return
	case !containsFold(b.set.AmmoSiteList, site.Key):
		// A ring saved before the setting changed (the operator took the shop out of ammoShops mid-round).
		b.finish(ctx, skipped("%s is no longer in ammoShops", site.Key))
		return
	case ctx.World() != s.World:
		b.finish(ctx, skipped("on world %d; the station is for world %d", ctx.World(), s.World))
		return
	case b.noCoins:
		b.finish(ctx, skipped("no coins: the draw found the bank empty"))
		return
	}
	if off, why := b.st.PairOff(key, now); off {
		b.finish(ctx, skipped("%s", why))
		return
	}
	// The trawler crew shops this world's store for its kit while the anchor's block runs: the shelf is theirs then.
	if site.CrewShelf {
		if world, on := b.trawlerBlockWorld(ctx); on && world == s.World {
			b.finish(ctx, skipped("%s's Fishing Trawler block is on world %d: its crew buys this world's %s shelf", trawlerCrewAnchor, world, site.Key))
			return
		}
	}
	if site.Dialogue {
		b.nedStation(ctx, d, s, site)
		return
	}
	limit, budgetOK := runerunnerplan.AmmoLeft(b.set.AmmoCoins, runerunnerplan.AmmoSpentThisRound(b.st.Ring, b.st.Done))
	if !budgetOK {
		b.finish(ctx, skipped("ammoCoins %d is spent this round", b.set.AmmoCoins))
		return
	}
	// A round that has already left runes on a shelf, or passed a station, for coins is short: the runes come first.
	if waits, why := runerunnerplan.AmmoWaitsForRunes(b.st); waits {
		b.finish(ctx, skipped("%s", why))
		return
	}

	// What is wanted: the listed parts the shop sells, less those the mule already holds enough of.
	coins, freeSlots, pack := b.ammoPackState(d)
	want, gated := b.ammoWanted(ctx, site, pack)
	for _, g := range gated {
		b.logf(ctx, "ammo-skip item=%s reason=%q", g.Item, g.Reason)
	}
	if len(want) == 0 {
		b.finish(ctx, skipped("nothing wanted at %s: %s", site.Key, ammoSkipText(gated)))
		return
	}
	est := b.ammoEstimate(site, s.World, now)
	estCost := runerunnerplan.AmmoStationCost(site.Shop, est, want, b.set.AmmoCapTable, limit)
	if ok, why := runerunnerplan.AmmoWorthVisiting(estCost, site.MinVisit(b.set.AmmoMinVisitCoins)); !ok {
		b.logf(ctx, "ammo-skip station=%s reason=%q", key, why)
		b.finish(ctx, skipped("%s", why))
		return
	}

	// Coins: ammo is bought after the runes. The fare stays in the pack and so does the part of the pack-paid
	// need of the rune and staff stations still ahead (margin included) that the bank cannot fund, NOT cut to half
	// the purse as a rune visit's reserve is: a purse short of the runes ahead, bank and pack together, buys no ammo.
	hold, aheadNeed, bankSpare := b.holdAhead(ctx)
	spendable := coins - b.set.FareCoins - hold
	if spendable <= 0 {
		b.finish(ctx, skipped("%d coins in the pack, %d for the fare and %d held back for the runes ahead (they need %d and the bank adds %d): nothing to spend on ammo",
			coins, b.set.FareCoins, hold, aheadNeed, bankSpare))
		return
	}
	// Room: a new stack needs a free slot beyond the reserve for the runes still to be bought.
	free := freeSlots - b.set.AmmoSlotReserve
	stacked := map[string]bool{}
	for item, n := range pack {
		stacked[item] = n > 0
	}
	if free <= 0 {
		anyStacked := false
		for _, w := range want {
			anyStacked = anyStacked || stacked[w]
		}
		if !anyStacked {
			b.finish(ctx, skipped("%d free pack slots with %d kept for the runes: no room for an ammo stack", freeSlots, b.set.AmmoSlotReserve))
			return
		}
	}

	var plan runerunnerplan.AmmoVisitPlan
	var visitSpendable int
	planFor := func(seen map[string]int, packCoins int) fleetplan.PurchasePlan {
		visitSpendable = packCoins - b.set.FareCoins - hold
		plan = runerunnerplan.PlanAmmoVisit(runerunnerplan.AmmoVisitInput{
			Shop: site.Shop, Items: want, Seen: seen, Caps: b.set.AmmoCapTable,
			Spendable: visitSpendable, Limit: limit, FreeSlots: free, Stacked: stacked,
		})
		return plan.PurchasePlan
	}
	v := b.buyAmmo(d, site, planFor)
	at := time.Now()
	if v.Walked {
		fleetrunner.ObserveVisit(fleetrunner.ShopsFor(s.World), site.Shop, v.ShopVisit, at, ctx.Username)
	}
	switch {
	case !v.Walked:
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeWalkFailed, Reason: b.strikeWhy(key, at, fmt.Sprintf("could not reach %s", site.Key))})
		return
	case v.OpenFailed:
		b.logf(ctx, "%s", v.Reason)
		b.finish(ctx, runerunnerplan.StationResult{Outcome: runerunnerplan.OutcomeAborted, Reason: b.strikeWhy(key, at, v.Reason)})
		return
	}
	b.st.ClearStrikes(key)
	res := b.ammoResult(ctx, site, s.World, key, want, v.ShopVisit, plan, visitSpendable, limit)
	// A pack of single Ropes would ride into the next stop and crowd out its purchase: bank it first (AmmoSite.BankAfter).
	if runerunnerplan.BankAfterWanted(site, res.Bought) {
		b.logf(ctx, "ammo-bank station=%s bought=%s: banking the pack at %s %d,%d", key, runerunnerplan.AmmoBoughtLine(res.Bought), site.BankName, site.Bank.X, site.Bank.Z)
		b.bankPack(ctx, d, site.Bank)
	}
	b.finish(ctx, res)
}

// nedStation is Ned's stop (runerunnerplan.NedShop): the part of ropeFloor that the mule's published bank and this runner's own Rope
// leave short, bought one conversation at a time (scarabrunner.BuyRopesFromNed: 15 coins each, drawn at the Draynor bank) and banked
// there. He has no shelf to wait on, so he is the part of the supply that does not depend on a restock clock.
func (b *runeRunnerBot) nedStation(ctx *Context, d *fleetrunner.Deps, s runerunnerplan.Station, site runerunnerplan.AmmoSite) {
	key := s.Key()
	_, freeSlots, pack := b.ammoPackState(d)
	mule, known := 0, false
	if f := b.ammoMule(ctx); f != nil {
		mule, known = f(runerunnerplan.AmmoRope)
	}
	want, why := runerunnerplan.NedRopesWanted(b.set.RopeFloor, mule, known, pack[runerunnerplan.AmmoRope], freeSlots-b.set.AmmoSlotReserve)
	if want <= 0 {
		b.logf(ctx, "ammo-skip station=%s reason=%q", key, why)
		b.finish(ctx, skipped("%s", why))
		return
	}
	b.logf(ctx, "ned station=%s buying %d Rope: %s", key, want, why)
	got, bwhy := b.buyNedRopes(d, want)
	r := runerunnerplan.StationResult{Bought: map[string]int{}, Spent: got * runerunnerplan.NedRopeCost, Reason: bwhy, At: time.Now()}
	switch {
	case got > 0:
		r.Outcome = runerunnerplan.OutcomeBought
		b.st.ClearStrikes(key)
		r.Bought[runerunnerplan.AmmoRope] = got
		b.logf(ctx, "buy shop=%s world=%d item=%s bought=%d spent=%d%s", site.Key, s.World, runerunnerplan.AmmoRope, got, r.Spent, ifReason(bwhy))
		if runerunnerplan.BankAfterWanted(site, r.Bought) {
			b.bankPack(ctx, d, site.Bank)
		}
	default:
		r.Outcome = runerunnerplan.OutcomeAborted
		r.Reason = b.strikeWhy(key, time.Now(), "Ned sold no Rope: "+bwhy)
		b.logf(ctx, "ned station=%s bought nothing: %s", key, bwhy)
	}
	b.finish(ctx, r)
}

// buyNedRopes is scarabrunner.BuyRopesFromNed, or the test seam's purchase.
func (b *runeRunnerBot) buyNedRopes(d *fleetrunner.Deps, want int) (int, string) {
	if b.nedBuySeam != nil {
		return b.nedBuySeam(want)
	}
	return 0, "buying ropes from Ned is not in this build"
}

// ifReason is ": why" for a log line, nothing for an empty reason.
func ifReason(why string) string {
	if why == "" {
		return ""
	}
	return ": " + why
}

// buyAmmo is runerunner.BuyAmmoAt, or the test seam's visit.
func (b *runeRunnerBot) buyAmmo(d *fleetrunner.Deps, site runerunnerplan.AmmoSite, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
	if b.ammoBuySeam != nil {
		return b.ammoBuySeam(site, planFor)
	}
	return runerunner.BuyAmmoAt(d, site, planFor)
}

// ammoResult logs one visit line by line, runs the price check and says how the station ended. The lines are
// the rune visit's (visitResult): "buy" for a part the shelf held and the plan bought from, "capped" for one it
// left whole, "empty", and "left N <item>: <reason>" for what a bought line left on the shelf. items are the parts
// the visit wanted; the ones the mule-stock gate left out were logged as ammo-skip before the walk.
func (b *runeRunnerBot) ammoResult(ctx *Context, site runerunnerplan.AmmoSite, world int, key string, items []string, v fleetrunner.ShopVisit, plan runerunnerplan.AmmoVisitPlan, spendable, limit int) runerunnerplan.StationResult {
	r := runerunnerplan.StationResult{Bought: map[string]int{}, Spent: v.Spent, Reason: v.Reason, At: time.Now()}
	anyStock, anyBought := false, false
	planned := map[string]int{}
	for _, l := range plan.Lines {
		planned[l.Rune] = l.Count
	}
	for _, item := range items {
		seen, got, pl := v.Seen[item], v.Bought[item], planned[item]
		capGP := b.set.AmmoCapTable.For(item)
		switch {
		case seen <= 0:
			b.logf(ctx, "empty shop=%s world=%d item=%s", site.Key, world, item)
		case got <= 0 && pl == 0:
			anyStock = true
			b.logf(ctx, "capped shop=%s world=%d item=%s price=%d cap=%d stock=%d: %s", site.Key, world, item,
				site.Shop.PriceAt(item, seen), capGP, seen, skipReason(plan.Skips, item))
		default:
			anyStock = true
			b.logf(ctx, "buy shop=%s world=%d item=%s seen=%d bought=%d spent=%d left=%d cap=%d", site.Key, world, item, seen, got,
				site.Shop.CostToBuy(item, seen, got), seen-got, capGP)
		}
		if got > 0 {
			anyBought = true
			r.Bought[item] = got
		}
		if left := seen - got; left > 0 && (got > 0 || pl > 0) {
			var why string
			switch next := site.Shop.PriceAt(item, left); {
			case got < pl:
				why = fmt.Sprintf("bought %d of %d planned", got, pl)
				if v.Reason != "" {
					why += ": " + v.Reason
				}
			case capGP > 0 && next > capGP:
				why = fmt.Sprintf("price %d over cap %d", next, capGP)
			case limit > 0 && v.Spent+next > limit:
				why = fmt.Sprintf("ammoCoins: the round's budget of %d left %d for this visit, which spent %d, next unit %d", b.set.AmmoCoins, limit, v.Spent, next)
			default:
				why = fmt.Sprintf("coins: %d spendable after the fare and the runes ahead, next unit %d", spendable, next)
			}
			b.logf(ctx, "left %d %s shop=%s world=%d: %s", left, item, site.Key, world, why)
		}
	}
	predicted := runerunnerplan.PredictedSpend(site.Shop, plan.PurchasePlan, v.Bought)
	if mismatch, disabled := b.st.NotePrice(key, predicted, v.Spent); mismatch {
		b.logf(ctx, "PRICE MODEL MISMATCH shop=%s world=%d predicted=%d spent=%d disabled=%v", site.Key, world, predicted, v.Spent, disabled)
	}
	switch {
	case anyBought:
		r.Outcome = runerunnerplan.OutcomeBought
	case anyStock:
		r.Outcome = runerunnerplan.OutcomeCapped
	default:
		r.Outcome = runerunnerplan.OutcomeEmpty
	}
	return r
}

// skipReason is the plan's reason for leaving an item, "" when it has none.
func skipReason(skips []runerunnerplan.AmmoSkip, item string) string {
	for _, s := range skips {
		if strings.EqualFold(s.Item, item) {
			return s.Reason
		}
	}
	return ""
}

// withoutFold is list less every entry that equals one of drop, ignoring case.
func withoutFold(list, drop []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !containsFold(drop, s) {
			out = append(out, s)
		}
	}
	return out
}

// containsFold reports whether the list holds s, ignoring case.
func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// ammoSkipText joins the gate's reasons for a log line.
func ammoSkipText(gated []runerunnerplan.AmmoSkip) string {
	parts := make([]string, len(gated))
	for i, g := range gated {
		parts[i] = g.Item + ": " + g.Reason
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// ammoRoundLine is the round's ammo summary, logged when the round closes with ammo on.
func (b *runeRunnerBot) ammoRoundLine(ctx *Context) {
	if !b.set.AmmoOn() {
		return
	}
	bought, spent, stops := map[string]int{}, 0, 0
	for _, s := range b.st.Ring {
		if !runerunnerplan.IsAmmoStation(s) {
			continue
		}
		stops++
		r := b.st.Done[s.Key()]
		spent += r.Spent
		for k, n := range r.Bought {
			bought[k] += n
		}
	}
	if stops == 0 {
		return // a delivery-only round
	}
	b.logf(ctx, "ammo round=%d stops=%d spent=%d budget=%d bought=%s", b.st.Round, stops, spent, b.set.AmmoCoins, runerunnerplan.FormatCounts(bought))
}

// ammoStartLine is the ammo status for the start log: what is placed, what is left out and why, and the rules.
func (b *runeRunnerBot) ammoStartLine() string {
	if !b.set.AmmoBuy {
		return "ammo: off (ammoBuy false)"
	}
	if !b.set.Rotation {
		return "ammo: ammoBuy is on but this runner does not rotate: the classic ring has no ammo stations"
	}
	placed, left := runerunnerplan.SplitAmmoSites(b.set.AmmoSiteList, b.set.ShopList)
	line := fmt.Sprintf("ammo: on sites=%s items=%s buy_all=%v coins=%d min_visit=%d slot_reserve=%d stop_above=%s",
		strings.Join(placed, ","), strings.Join(b.set.AmmoItemList, ","), b.set.AmmoBuyAll, b.set.AmmoCoins, b.set.AmmoMinVisitCoins,
		b.set.AmmoSlotReserve, runerunnerplan.FormatCounts(b.set.AmmoStop))
	if len(left) > 0 {
		line += " left_out=" + strings.Join(left, "; ")
	}
	for _, it := range b.set.AmmoItemList {
		sold := false
		for _, k := range placed {
			if site, ok := runerunnerplan.AmmoSiteFor(k); ok && site.Shop.Sells(it) {
				sold = true
			}
		}
		if !sold {
			line += fmt.Sprintf("; %s is sold by no shop on this circuit (the Seers market stall or fletching are its sources)", it)
		}
	}
	return line
}
