package bot

// MasterCrafter's later rounds (a user request), after gems, pearls and bows and before the
// ScarabSwarm fallback: the shop trip (Bolts, and Steel arrowtips, from the supply shops with the
// mule's coins), superheating the mule's ores into bars, steel arrows from the mule's parts, and
// cleaning the mule's grimy herbs. The decisions and the content facts are mastercraftplan's
// (supply.go, superheat.go, arrows.go, herbs.go); the I/O mastercraftrunner's.
//
// THE PACE of each, all without streaming packets ahead of the server: the superheat round one
// cast every third tick (the spell's own p_delay(1) and the engine dropping a cast while delayed);
// the arrow round five uses a tick on two stacks, 75 arrows a tick; the herb round five Identify
// clicks a tick, the gem round's pace.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/tile"
)

// masterSupplyAsks bounds the kit asks one round makes (the first, and top-ups the mule's caps
// left).
const masterSupplyAsks = 4

// ownBankItems is this account's bank as the session last saw it (nil when never seen).
func ownBankItems(ctx *Context) []gamestate.SlotItem {
	if ctx.Session == nil {
		return nil
	}
	inv := ctx.Session.State().Inventory(actions.BankWithdraw)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// skillLevel is the account's level in stat, 0 until the server has sent it.
func skillLevel(ctx *Context, stat int) (int, bool) {
	if ctx.Session == nil {
		return 0, false
	}
	if sk, ok := ctx.Session.State().Skill(stat); ok && sk.Generation > 0 {
		return sk.Level, true
	}
	return 0, false
}

// supplySnapshot fills the later rounds' part of a decision snapshot.
func (b *masterCrafterBot) supplySnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	bank := ownBankItems(ctx)
	s.MagicLevel, _ = skillLevel(ctx, mastercraftrunner.MagicStat)
	s.SmithingLevel, _ = skillLevel(ctx, mastercraftrunner.SmithingStat)
	s.HerbloreLevel, s.HerbloreKnown = skillLevel(ctx, herbStat)
	d := b.deps(ctx)
	if ctx.Config != nil && ctx.Components != nil {
		s.FireStaff = mastercraftrunner.FireStaffWorn(d) || mastercraftrunner.FireStaffIn(d, bank) != 0
	}
	if bank != nil {
		s.OwnSuperheat = mastercraftrunner.SuperheatOwn(bank)
		s.OwnArrows = mastercraftrunner.ArrowOwn(bank)
		s.OwnHerbs = mastercraftrunner.HerbCounts(bank)
	}
	// The jewellery round's own bank (mastercrafter_jewellery.go): the session's reading, else the
	// ledger's, and whether the session has read it; and whether a worn Staff of water pays the Water.
	s.OwnJewellery, s.OwnJewelleryKnown = b.ownJewelleryHeld(ctx)
	if ctx.Config != nil && ctx.Components != nil {
		paid := mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(d), b.set.MembersWorld)
		s.WaterPaid, s.AirPaid = paid[mastercraftplan.RuneWater], paid[mastercraftplan.RuneAir]
	}
}

// herbStat is UPDATE_STAT's Herblore index.
const herbStat = 15

// roundHold is the check a later round makes before every load and burst: the stop, the round's
// budget, the dead latch, and a random event (no use may go out under one).
func (b *masterCrafterBot) roundHold(ctx *Context, d *fleetrunner.Deps, started time.Time, reason *string) func() bool {
	var guarded time.Time
	return func() bool {
		if ctx.Stopped() {
			*reason = "stopped"
			return false
		}
		if mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) {
			*reason = "the round's time (craftMaxRoundMinutes) is used up"
			return false
		}
		if time.Since(guarded) < 500*time.Millisecond {
			return !randomevents.Busy(ctx.Session)
		}
		guarded = time.Now()
		if b.trawlerWaiting(ctx, guarded) {
			*reason = "the trawler crew is waiting in the lobby"
			return false
		}
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			*reason = "stopped"
			return false
		}
		return true
	}
}

// muleStock is the mule's published bank, nil when it has not published.
func (b *masterCrafterBot) muleStock(ctx *Context) mastercraftplan.Stock {
	if f := muleBankCount(ctx, b.mule); f != nil {
		return f
	}
	return nil
}

// ---- the shop trip ----

// shopRound is the shop trip: coins, the shops, the bank at the mule's post.
func (b *masterCrafterBot) shopRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeShop}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	stops := dec.Shops
	if len(stops) == 0 {
		stops = mastercraftplan.PlanShopTrip(b.set, dec.Wants, b.ledger.Shelves, time.Now())
	}
	if len(stops) == 0 {
		out.Why = "no shop worth the walk"
		return out
	}
	need := mastercraftplan.TripCoins(stops)
	// 1. Coins: this account's own first, the mule's for the rest.
	if v := mastercraftrunner.DepositAll(d, post); !v.Opened {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	if held, _ := fleetrunner.WithdrawCoinsTo(d, post, need); held < need {
		short := (need - held + 999) / 1000 * 1000
		a := b.askMule(ctx, d, []mastercraftplan.Line{{Item: "Coins", Qty: short}})
		b.logf(ctx, "shop: asked %s for %d coins (the trip needs about %d, %d held): %s", b.mule, short, need, held, masterDescribe(a.Got))
		if a.Stopped {
			out.Why = "stopped"
			return out
		}
	}
	coins0 := fleetrunner.CoinsHeld(d)
	if coins0 <= 0 {
		out.Transient, out.Why = true, "no coins for the shops"
		return out
	}
	b.logf(ctx, "shop: %d coins for %s", coins0, mastercraftplan.DescribeTrip(stops))
	// 2. The shops, in order.
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)
	bought := map[int32]int{}
	spent, failed := 0, 0
	for _, stop := range stops {
		if !hold() {
			break
		}
		if fleetrunner.CoinsHeld(d) <= 0 {
			reason = "the coins ran out"
			break
		}
		v := mastercraftrunner.BuyAtShop(d, stop.Shop, dec.Wants.Has, int32(b.set.ShopMaxPrice))
		if !v.Opened {
			failed++
			b.logf(ctx, "shop: %s", v.Why)
			continue
		}
		now := time.Now()
		for _, it := range stop.Shop.Items {
			if left, ok := v.Left[it.ID]; ok {
				b.ledger.NoteShelf(stop.Shop, it, left, now)
			}
		}
		for id, n := range v.Bought {
			bought[id] += n
		}
		spent += v.Spent
	}
	// 3. Back to the post, everything banked: the pearl and arrow rounds draw from there.
	if !masterWalkTo(d, post, 3, scarabConsolidateWalk) {
		b.logf(ctx, "shop: could not walk back to %s's post from %s; the next round's walk brings the purchases", b.mule, fleetrunner.HereIs(d))
	} else {
		mastercraftrunner.DepositAll(d, post)
	}
	total := 0
	for _, n := range bought {
		total += n
	}
	b.ledger.ShopBought += total
	b.ledger.ShopSpent += spent
	out.Produced = total
	out.Transient = total == 0 && failed > 0
	out.Why = fmt.Sprintf("bought %d Bolts and %d Steel arrowtips for %d coins (%s)", bought[pearlplan.BoltID], bought[mastercraftplan.ArrowTipsID], spent, firstNonEmpty(reason, "every shop visited"))
	return out
}

// ---- superheating ----

// superheatRound is the superheat round: the fire staff, the ores, the casts, the bars to the mule.
func (b *masterCrafterBot) superheatRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeSuperheat}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	// 1. The pack banked, the bank read.
	var qb mastercraftrunner.QuickBank
	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	// 2. A Staff of fire wielded (owner: own bank, then the mule, then the Seers market or Zaff).
	staff, previous, swhy := b.fireStaff(ctx, d, post, v.Banked)
	if swhy != "" {
		b.logf(ctx, "superheat: %s — casting with Fire runes", swhy)
	}
	defer func() {
		if previous != 0 && staff != 0 {
			mastercraftrunner.Rewield(d, previous)
		}
	}()
	worn := mastercraftrunner.FireStaffWorn(d)
	smithing, _ := skillLevel(ctx, mastercraftrunner.SmithingStat)
	// 2b. Gold bars give 2.5x the Smithing XP with the goldsmith gauntlets worn (mastercraftplan/gauntlets.go).
	b.goldGauntlets(ctx, d, post, v.Banked, smithing)
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)
	asks, bars := 0, 0
	own := mastercraftrunner.SuperheatOwn(v.Banked)
	ask := func() bool {
		if asks >= masterSupplyAsks {
			return false
		}
		mule := b.muleStock(ctx)
		p := mastercraftplan.PlanSuperheat(b.set, smithing, func(item string) int {
			n := own(item)
			if mule != nil {
				if m, ok := mule(item); ok {
					n += m
				}
			}
			return n
		}, worn)
		lines := mastercraftplan.PlanSuperheatAsk(p, mule, own, worn)
		if len(lines) == 0 {
			return false
		}
		asks++
		a := b.askMule(ctx, d, lines)
		b.logf(ctx, "superheat: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
		return a.Traded > 0 && !a.Stopped
	}
	if !ask() && mastercraftplan.PlanSuperheat(b.set, smithing, own, worn).Casts == 0 {
		out.Why = "nothing to superheat: no ore and runes in the bank, and the mule gave none"
		return out
	}
	// 3. The casts (mastercraftrunner.SuperheatRun): the bank one tick a load, a cast every three
	// ticks, the highest bar the ore allows first. The mule is asked again only when this account's
	// own bank and pack can make nothing more (before 6 Oct 2026 the Nature runes drawn into the pack
	// read as none, so every load asked the mule and the round ended after two loads).
	pick := func(stock mastercraftplan.SuperheatStock) (mastercraftplan.Bar, bool) {
		p := mastercraftplan.PlanSuperheat(b.set, smithing, stock, worn)
		return p.Bar, p.Casts > 0
	}
	keep := func(id int32) bool { return previous != 0 && id == previous }
	usedUp, fails := false, 0
	for hold() {
		run := mastercraftrunner.SuperheatRun(d, post, mastercraftrunner.HeatOptions{Pick: pick, Keep: keep, Staff: worn, Hold: hold, Quick: &qb})
		b.logf(ctx, "superheat: %s", run.Describe())
		for _, m := range run.Made {
			lootSuperheat(ctx, m.Bar, m.Casts, worn)
			bars += m.Casts
		}
		if run.Casts > 0 {
			fails = 0
		}
		switch run.Reason {
		case "done":
			if ask() {
				continue
			}
			usedUp = true
		case "no-open", "stalled":
			if fails++; fails < 3 {
				continue
			}
			reason, out.Transient = "the superheat run: "+run.Why, true
		case "held", "stopped":
		default:
			reason = "the cast was refused (" + run.Reason + ")"
		}
		break
	}
	b.ledger.BarsMade += bars
	out.Produced = bars
	if previous != 0 && staff != 0 {
		mastercraftrunner.Rewield(d, previous)
		previous = 0
	}
	if !usedUp {
		// The owner (6 Oct 2026): the bars go to the mule when the ore is used up, not after every
		// round; the next superheat round carries on from this account's bank.
		out.Why = fmt.Sprintf("%d bars superheated (%s); the bars stay in the bank until the ore is used up", bars, firstNonEmpty(reason, "stopped"))
		return out
	}
	out.Why = fmt.Sprintf("%d bars superheated (the ore is used up)", bars)
	ho := b.handOver(ctx, d, post, mastercraftrunner.BarProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	return out
}

// goldGauntlets wears the goldsmith gauntlets when gold ore may be superheated (craftSuperheatGold on, Smithing
// 40): worn already, from the pack, or from the account's own bank, and says what a gold bar will give, or
// what is missing (they are Family Crest's reward, and the quest is not built). It returns whether they are
// worn after.
func (b *masterCrafterBot) goldGauntlets(ctx *Context, d *fleetrunner.Deps, post tile.Tile, bank []gamestate.SlotItem, smithing int) bool {
	inPlay := mastercraftplan.GoldInPlay(b.set, smithing)
	worn, inPack, inBank := false, 0, 0
	if inPlay {
		worn, inPack, inBank = mastercraftrunner.GauntletState(d, bank)
	}
	act := mastercraftplan.GauntletStep(inPlay, worn, inPack, inBank)
	switch act {
	case mastercraftplan.GauntletsNotNeeded:
		return false
	case mastercraftplan.GauntletsWorn:
		b.logf(ctx, "superheat: %s", mastercraftplan.GauntletLine(act, nil))
		return true
	case mastercraftplan.GauntletsMissing:
		levels := map[string]int{}
		for name, stat := range map[string]int{"Mining": essencerunner.MiningStat, "Crafting": gemrunner.CraftingStat, "Smithing": mastercraftrunner.SmithingStat, "Magic": mastercraftrunner.MagicStat} {
			levels[name], _ = skillLevel(ctx, stat)
		}
		b.logf(ctx, "superheat: %s", mastercraftplan.GauntletLine(act, mastercraftplan.FamilyCrestShort(levels)))
		return false
	}
	b.logf(ctx, "superheat: %s", mastercraftplan.GauntletLine(act, nil))
	if why := mastercraftrunner.WearGauntlets(d, post); why != "" {
		b.logf(ctx, "superheat: gold: the gauntlets are not worn: %s (the gold bars give the plain Smithing XP)", why)
		return false
	}
	return true
}

// fireStaff wields a Staff of fire: one already worn, else from the bank, else from the mule, else
// bought at the Seers market or from Zaff. It returns the staff and the weapon it replaced, or why
// none is wielded.
func (b *masterCrafterBot) fireStaff(ctx *Context, d *fleetrunner.Deps, post tile.Tile, bank []gamestate.SlotItem) (staff, previous int32, why string) {
	return b.fireStaffOpt(ctx, d, post, bank, b.set.BuyFireStaff)
}

// fireStaffOpt is fireStaff with the buying switched by the caller: the chest round wields a staff it can get from
// its own bank or the mule and does not walk to a shop for one.
func (b *masterCrafterBot) fireStaffOpt(ctx *Context, d *fleetrunner.Deps, post tile.Tile, bank []gamestate.SlotItem, buy bool) (staff, previous int32, why string) {
	if mastercraftrunner.FireStaffWorn(d) {
		return 0, 0, ""
	}
	if mastercraftrunner.FireStaffIn(d, bank) == 0 {
		if mule := b.muleStock(ctx); mule != nil {
			if n, _ := mule(mastercraftplan.StaffOfFire); n > 0 {
				a := b.askMule(ctx, d, []mastercraftplan.Line{{Item: mastercraftplan.StaffOfFire, Qty: 1}})
				b.logf(ctx, "superheat: asked %s for a %s: %s", b.mule, mastercraftplan.StaffOfFire, masterDescribe(a.Got))
			}
		}
	}
	staff, previous, why = mastercraftrunner.WieldFireStaff(d, post)
	if why == "" {
		return staff, previous, ""
	}
	if !buy {
		return 0, 0, why
	}
	want := max(b.set.FireStaffMaxPrice, mastercraftrunner.FireStaffCoins)
	if held, _ := fleetrunner.WithdrawCoinsTo(d, post, want); held < want {
		a := b.askMule(ctx, d, []mastercraftplan.Line{{Item: "Coins", Qty: (want - held + 999) / 1000 * 1000}})
		b.logf(ctx, "superheat: asked %s for coins for a %s: %s", b.mule, mastercraftplan.StaffOfFire, masterDescribe(a.Got))
	}
	got, bwhy := mastercraftrunner.BuyFireStaff(d, b.set.FireStaffMaxPrice)
	b.logf(ctx, "superheat: buying a %s: %s", mastercraftplan.StaffOfFire, firstNonEmpty(bwhy, got))
	if got == "" {
		return 0, 0, why + "; buying one: " + bwhy
	}
	if !masterWalkTo(d, post, 3, scarabConsolidateWalk) {
		return 0, 0, "could not walk back to the mule's post with the staff"
	}
	return mastercraftrunner.WieldFireStaff(d, post)
}

// lootSuperheat reports bars superheated on the Magic card, the row saying whether a fire staff paid
// the Fire runes (loot/magic.go). Not on the Smithing card as well: the panel sums every card, and
// one bar must be counted once.
func lootSuperheat(ctx *Context, bar mastercraftplan.Bar, n int, staff bool) {
	lootMade(ctx, loot.SuperheatCast(int(bar.BarID), bar.Ore, staff), n, loot.Product(int(bar.BarID), bar.Name, n))
}

// describeLines is a kit's lines for a log.
func describeLines(lines []mastercraftplan.Line) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, fmt.Sprintf("%d %s", l.Qty, l.Item))
	}
	return strings.Join(parts, ", ")
}

// ---- steel arrows ----

// arrowRound is the arrow round: the parts from the mule, headless arrows, steel arrows, the arrows
// to the mule.
func (b *masterCrafterBot) arrowRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeArrows}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	var qb mastercraftrunner.QuickBank
	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	if lines := mastercraftplan.PlanArrowAsk(b.set, mastercraftplan.ArrowHeldIn(b.muleStock(ctx)), mastercraftrunner.ArrowOwn(v.Banked)); len(lines) > 0 {
		a := b.askMule(ctx, d, lines)
		b.logf(ctx, "arrows: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
		if a.Stopped {
			out.Why = "stopped"
			return out
		}
	}
	fletching, _ := skillLevel(ctx, bowstringrunner.FletchingStat)
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)
	gd := mastercraftrunner.GemDeps(d)
	headless, arrows := 0, 0
	type step struct {
		draw            func([]gamestate.SlotItem) []int32
		use, with, made int32
		src             loot.Source
		name            string
	}
	steps := []step{
		{mastercraftrunner.HeadlessDraw, mastercraftplan.FeatherID, mastercraftplan.ArrowShaftID, mastercraftplan.HeadlessID,
			loot.Fletching(nil, int(mastercraftplan.ArrowShaftID), mastercraftplan.ArrowShaft), mastercraftplan.Headless},
	}
	if fletching >= mastercraftplan.SteelArrowLvl {
		steps = append(steps, step{mastercraftrunner.SteelDraw, mastercraftplan.ArrowTipsID, mastercraftplan.HeadlessID, mastercraftplan.SteelArrowID,
			loot.Fletching(nil, int(mastercraftplan.ArrowTipsID), mastercraftplan.ArrowTips), mastercraftplan.SteelArrow})
	}
steps:
	for _, s := range steps {
		for hold() {
			lv := mastercraftrunner.FullVisit(d, post, &qb, nil, s.draw, false)
			if !lv.Opened || (lv.Err != "" && len(lv.Drew) == 0) {
				reason, out.Transient = "the bank: "+lv.Err, true
				break steps
			}
			if len(lv.Drew) < 2 {
				break // one of the two stacks is used up
			}
			r := mastercraftrunner.MakeStack(gd, mastercraftrunner.StackOptions{
				Use: s.use, With: s.with, Product: s.made, PerUse: mastercraftplan.ArrowsPerUse,
				Refusal: pearlplan.Refusal, Hold: hold,
			})
			b.logf(ctx, "arrows: %s: %s", s.name, r.Describe())
			lootMade(ctx, s.src, r.Made, loot.Product(int(s.made), s.name, r.Made))
			if s.made == mastercraftplan.HeadlessID {
				headless += r.Made
			} else {
				arrows += r.Made
			}
			if r.Reason != "done" {
				if r.Reason != "held" && r.Reason != "stopped" {
					reason = "the uses were refused or stalled (" + r.Reason + ")"
				}
				break steps
			}
		}
	}
	b.ledger.HeadlessMade += headless
	b.ledger.ArrowsMade += arrows
	out.Produced = headless + arrows
	out.Why = fmt.Sprintf("%d headless arrows and %d steel arrows made (%s)", headless, arrows, firstNonEmpty(reason, "the parts are used up"))
	ho := b.handOver(ctx, d, post, mastercraftrunner.ArrowProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	// Shafts or feathers the other ran out against make nothing here: back to the mule.
	if !out.HandoverFailed && ho.Opened && mastercraftrunner.ArrowOwn(ho.Banked).HeadlessToMake() == 0 {
		back := b.handOver(ctx, d, post, mastercraftrunner.ArrowLeftovers())
		if n := mastercraftrunner.Total(back.Handed); n > 0 {
			b.logf(ctx, "arrows: handed back %s the mule: %s", b.mule, masterDescribe(back.Handed))
		}
	}
	return out
}

// ---- herbs ----

// herbRound is the herb round: every grimy herb Herblore can clean from the mule, cleaned five a
// tick, the clean herbs to the mule.
func (b *masterCrafterBot) herbRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeHerbs}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	level, _ := skillLevel(ctx, herbStat)
	var qb mastercraftrunner.QuickBank
	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	asks := 0
	ask := func() bool {
		if asks >= masterSupplyAsks {
			return false
		}
		lines := mastercraftplan.PlanHerbAsk(b.set, level, b.muleStock(ctx))
		if len(lines) == 0 {
			return false
		}
		asks++
		a := b.askMule(ctx, d, lines)
		b.logf(ctx, "herbs: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
		return a.Traded > 0 && !a.Stopped
	}
	ask()
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)
	denied := map[int32]bool{}
	cleaned := 0
	for hold() {
		var herbs []herbplan.Herb
		for _, h := range mastercraftplan.HerbsCleanable(level) {
			if !denied[h.UnidID] {
				herbs = append(herbs, h)
			}
		}
		var lv mastercraftrunner.LoadVisit
		if qb.Ready(d) {
			qv := qb.Visit(d, nil, mastercraftrunner.HerbDraw(herbs))
			if qv.Done {
				lv = mastercraftrunner.LoadVisit{Drew: qv.Drew}
				lv.Opened, lv.Banked = true, qv.AtOpen
			}
		}
		if !lv.Opened {
			lv = mastercraftrunner.FullVisit(d, post, &qb, nil, mastercraftrunner.HerbDraw(herbs), false)
		}
		if !lv.Opened || (lv.Err != "" && len(lv.Drew) == 0) {
			reason, out.Transient = "the bank: "+lv.Err, true
			break
		}
		if len(lv.Drew) == 0 {
			if ask() {
				continue
			}
			break // every herb this level can clean is clean
		}
		h, _ := herbplan.HerbByUnidID(lv.Drew[0])
		r := mastercraftrunner.CleanAll(d, h, func() {
			if qb.Ready(d) {
				qb.Prime(d)
			}
		})
		cleaned += r.Cleaned
		lootMade(ctx, loot.Cleaning(int(h.UnidID), h.Name), r.Cleaned, loot.Product(int(h.CleanID), h.Name, r.Cleaned))
		if r.Reason != "done" {
			b.logf(ctx, "herbs: %s", r.Describe())
		}
		switch r.Reason {
		case "refused", "members":
			denied[h.UnidID] = true
		case "stalled", "stopped":
			reason = "cleaning " + r.Reason
		}
		if reason != "" {
			break
		}
	}
	if qb.Primed() {
		qb.Forget()
	}
	b.ledger.HerbsCleaned += cleaned
	out.Produced = cleaned
	out.Why = fmt.Sprintf("%d herbs cleaned (%s)", cleaned, firstNonEmpty(reason, "every grimy herb is clean"))
	ho := b.handOver(ctx, d, post, mastercraftrunner.HerbProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	return out
}
