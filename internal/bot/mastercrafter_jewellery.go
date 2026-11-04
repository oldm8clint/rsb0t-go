package bot

// MasterCrafter's jewellery round (a user request; the rule, the content and the order are
// mastercraftplan/jewellery.go and jewelleryround.go). When the mule's published bank holds no Games
// necklace and no Ring of dueling of any charge, and gold bars, cut Sapphires and runes enough for
// some, the main account asks it for a batch (default 50), makes the Sapphire necklaces at the Falador furnace,
// enchants each into a Games necklace(8), and hands them to the mule: the supply the fleet's run
// stalling takes from it (every ScarabSwarm account rubs a carried Games necklace or Ring of dueling).
//
// What it reuses rather than rewrites: MasterCrafter's own ask (askMuleWith, the bundled kit order),
// walk (masterWalkTo, the wolf-free route) and the Falador west bank (chestOpenFaladorBank), the chest
// round's way of owing the mule by quantity (a ledger of what was made, drawn as notes at the post),
// its teleport check (mastercraftplan.CanCastWith) and the hand-over trade (masterDeliver).
//
// The round is a state machine like the chest's (mastercraftplan.NextJewelleryStep): every pass reads
// where the account is and what it holds and takes the next step, so a step that fails is tried again
// from where it left the account, and a round a restart interrupted carries on from the pack and the
// bank. A bank that holds Sapphire necklaces and no gold bars needs no furnace: the enchants are made
// where the account stands.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// masterJewelleryLongWalk bounds a long leg between Catherby and Falador on the wolf-free route (the
// ships, then the spirit trees), the chest round's own bound.
const masterJewelleryLongWalk = masterChestLongWalk

// masterJewelleryLegWalk bounds a walk inside Falador, bank to furnace or back.
const masterJewelleryLegWalk = 5 * time.Minute

// masterJewelleryStepFails is how many times in a row one step may fail before the round gives up on
// it (the work steps wrap the round up, the others end it).
const masterJewelleryStepFails = 3

// jewelleryRetryWait is the wait before a failed step is tried again (a variable so a test need not
// sleep through it).
var jewelleryRetryWait = 3 * time.Second

// masterJewelleryRepeats is how many times in a row one step may run, each time "working", before it
// counts as a failure (a load takes the bank, the furnace and the enchants, so nothing legitimately
// repeats).
const masterJewelleryRepeats = 8

// masterJewelleryMaxSteps bounds one round's steps whatever happens: a batch is about six steps a
// load of ten, and the setting's cap is a thousand.
const masterJewelleryMaxSteps = 1500

// masterJewelleryBankRadius is how close to the work bank's stand counts as at it, and
// masterJewelleryShopRadius how close to a shop keeper's spawn counts as at the shop.
const (
	masterJewelleryBankRadius = 8
	masterJewelleryShopRadius = 5
)

// The round's heavy I/O, as package variables a test replaces (like masterWalkTo and masterDeliver): the
// furnace found, approached and used, the spell cast, the mould bought and the teleport sent.
var (
	jewelleryFind     = mastercraftrunner.FindFurnace
	jewelleryApproach = mastercraftrunner.ApproachFurnace
	jewelleryReached  = mastercraftrunner.FurnaceReached
	jewelleryMake     = mastercraftrunner.MakeJewellery
	jewelleryEnchant  = mastercraftrunner.CastEnchantKind
	jewelleryBuy      = mastercraftrunner.BuyMouldFor
	jewelleryTeleport = func(ctx *Context, spell string) (bool, error) { return ctx.Teleport(spell, 0) }
	// jewelleryWalkTake ends a walk's enchanting and reads what it cast (scarabrunner.Travel.EnchantWalkTake).
	jewelleryWalkTake = func(t *fleetrunner.Travel, d *fleetrunner.Deps) fleetrunner.EnchantWalkResult {
		return t.EnchantWalkTake(d)
	}
)

// masterJewellery is one jewellery round's running state.
type masterJewellery struct {
	b    *masterCrafterBot
	ctx  *Context
	d    *fleetrunner.Deps
	ld   *lootchestfisherrunner.Deps
	post tile.Tile
	hold func() bool

	magic    int
	crafting int
	paid     map[string]bool
	view     mastercraftplan.JewelleryView
	// kind is what the batch makes and sp its items (mastercraftplan.JewelleryKind).
	kind mastercraftplan.JewelleryKind
	sp   mastercraftplan.KindSpec
	// bank is the bank's round items of both kinds as last read (a kit visit reads it and takes off what
	// it drew); the view reads it As the batch's kind.
	bank mastercraftplan.JewelleryHeld

	furn     locindex.Match
	haveFurn bool
	shops    []mastercraftplan.MouldShop

	asked, handedOver, futile, shopFailed bool
	craftOK, enchantOK                    bool
	// fresh is set by the ask: the next bank visit reads what arrived and sizes the batch to it.
	fresh bool
	// holdReason is what the round's hold said when it stopped a step.
	holdReason string
	// askFailed says the ask brought nothing for a passing reason (not "the mule has none"); transient
	// that something failed for a passing reason (a mould that could not be bought) which backs the
	// round's mode off if it made nothing.
	askFailed, transient bool

	crafted, enchanted, handed, visits, bought int
	started                                    time.Time
	// travel is the walks' Travel when they enchant (nil with craftWalkEnchant off); walkOff says the walks'
	// enchanting stalled and the rest of the round enchants standing. walks is the walks that carried pieces,
	// walkSent and walkCast the casts those walks sent and landed (enchanted counts them and the standing ones).
	travel                    *fleetrunner.Travel
	walkOff                   bool
	walks, walkSent, walkCast int
	steps                     map[mastercraftplan.JewelleryStep]int
	last                      mastercraftplan.JewelleryStep
}

// jewelleryRound is the jewellery round: see the file comment.
func (b *masterCrafterBot) jewelleryRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeJewellery}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return b.jewelleryWaits(ctx, out, dec.Resume)
	}
	led := &b.ledger.Jewellery
	// What the round makes: a new batch's kind, or the kind of the batch under way (a hand-over of owed
	// pieces carries no kind of its own), or the kind of the own-bank pieces a resume enchants.
	kind := dec.Kind
	if dec.Resume && led.Under() {
		kind = led.Kind
	}
	c := &masterJewellery{
		b: b, ctx: ctx, d: d, ld: b.chestDeps(ctx), post: post, started: started,
		magic: fleetrunner.MagicLevel(d), crafting: masterCraftingLevel(ctx), shops: mastercraftplan.MouldShops(),
		steps: map[mastercraftplan.JewelleryStep]int{}, kind: kind, sp: kind.Spec(),
	}
	c.paid = mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(d), b.set.MembersWorld)
	if b.set.WalkEnchant {
		// Every walk of the round carries a poll that can cast the enchant on the pieces the account holds
		// (a user request: "ring enchanting should be done while walking instead of while standing at the
		// furnace"); it does nothing until a walk arms it (walk below).
		c.travel = &fleetrunner.Travel{Members: b.set.MembersWorld}
		prev := d.Travel
		d.Travel = c.travel
		defer func() { d.Travel = prev }()
	}
	ok, jwhy := mastercraftplan.JewelleryOKFor(kind, b.set, c.magic, c.crafting)
	c.craftOK, c.enchantOK = ok, ok
	c.hold = b.roundHold(ctx, d, started, &c.holdReason)
	c.bank, _ = b.ownJewelleryHeld(ctx)
	// The batch: a new decision starts one; a resume carries the ledger's on, or makes one of the
	// Sapphire necklaces (Emerald rings) the bank holds, or only hands over what is owed.
	switch {
	case !dec.Resume:
		led.StartKindBatch(kind, dec.Jewellery.N)
		b.logf(ctx, "jewellery: a batch of %d %s: %s", dec.Jewellery.N, c.sp.Plural, dec.Why)
	default:
		c.asked = true
		if !led.Under() && c.bank.As(kind).Necklaces > 0 {
			led.StartKindBatch(kind, c.bank.As(kind).Necklaces)
		}
		b.logf(ctx, "jewellery: resuming (%s): batch %d %s, %d crafted and %d enchanted so far, %d owed to %s; from %s",
			dec.Why, led.Target, c.sp.Plural, led.Crafted, led.Enchanted, led.OwedTotal(), b.mule, fleetrunner.HereIs(d))
	}
	if !ok {
		b.logf(ctx, "jewellery: nothing can be made: %s", jwhy)
	}
	led.Rounds++
	fal, home := mastercraftplan.JewelleryLegSpell(mastercraftplan.LegToFalador, c.magic), mastercraftplan.JewelleryLegSpell(mastercraftplan.LegHome, c.magic)
	b.logf(ctx, "jewellery: round starts at Magic %d, Crafting %d: to Falador %s, home %s; a worn staff pays for %s",
		c.magic, c.crafting, spellWord(fal), spellWord(home), paidWord(c.paid))
	if kind != mastercraftplan.KindNecklace {
		b.logf(ctx, "jewellery: this batch makes %s: %s from %s and %s, enchanted with %s (%d %s and a Cosmic rune a cast)",
			c.sp.Plural, c.sp.Base, c.sp.Mould, c.sp.Gem, c.sp.Spell, c.sp.SecondPer, c.sp.Second)
	}

	fails, repeats := 0, 0
	reason := ""
loop:
	for step := 0; step < masterJewelleryMaxSteps; step++ {
		if ctx.Stopped() {
			reason = "stopped"
			break
		}
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			reason = "stopped"
			break
		}
		c.read(started)
		next := mastercraftplan.NextJewelleryStep(c.view)
		if next == mastercraftplan.JewelleryStepDone {
			break
		}
		if next != c.last {
			fails, repeats = 0, 0
		} else {
			repeats++
		}
		c.last = next
		c.steps[next]++
		ok, end, why := c.do(next)
		if ok && repeats >= masterJewelleryRepeats {
			ok, why = false, fmt.Sprintf("%s ran %d times in a row without the round moving on", next, repeats+1)
		}
		if end != "" {
			reason = end
			out.Transient = out.Transient || why == "transient"
			break
		}
		if ok {
			fails = 0
			continue
		}
		fails++
		b.logf(ctx, "jewellery: %s failed (%d in a row): %s", next, fails, why)
		if fails < masterJewelleryStepFails {
			ctx.Delay(jewelleryRetryWait)
			continue
		}
		out.Transient = true
		switch {
		case c.view.Working() && next != mastercraftplan.JewelleryStepHome && next != mastercraftplan.JewelleryStepHandover:
			// A step of the work failed too often: wrap the round up with what there is.
			c.futile, fails = true, 0
			b.logf(ctx, "jewellery: giving up on the rest of the batch after %s failed %d times — taking what there is home", next, masterJewelleryStepFails)
			continue loop
		default:
			reason = fmt.Sprintf("%s failed %d times in a row: %s", next, fails, why)
			break loop
		}
	}
	if reason == "" {
		reason = "the batch is made and handed over"
		switch {
		case c.futile:
			reason = "the rest of the batch was given up, what was made handed over"
		case !c.enchantOK || !c.craftOK:
			reason = "nothing could be made: " + jwhy
		case led.Left() > 0:
			reason = fmt.Sprintf("%d of the batch were not made (nothing left to make them from, or no way to)", led.Left())
		}
	}
	// A batch that is made, or has nothing left to be made from (the pack and the bank hold no Sapphire
	// necklace and no pair to make one of), is over.
	// The ledger keeps the bank as this round last read it, so that a restart starts from the same
	// picture (a round that opened no bank leaves the session's own reading in it).
	led.Bank = c.bank
	pack := mastercraftrunner.JewelleryPack(d).As(kind)
	bk := c.bank.As(kind)
	pipeline := bk.Necklaces + bk.Pairs() + pack.Necklaces + pack.Pairs()
	if !ctx.Stopped() && (!led.Under() || pipeline == 0) {
		led.ClearBatch()
	}
	out.Transient = out.Transient || c.transient || c.askFailed
	out.Produced, out.Handed = c.enchanted, c.handed
	out.HandoverTried = c.handedOver
	out.HandoverFailed = c.handedOver && led.OwedTotal() > 0
	out.Why = fmt.Sprintf("%d %s(s) made (%d %s(s) crafted), %d handed to %s, %d still owed, %d mould(s) bought (%s)",
		c.enchanted, c.sp.Product, c.crafted, c.sp.Base, c.handed, b.mule, led.OwedTotal(), c.bought, reason)
	if c.walks > 0 {
		b.logf(ctx, "jewellery: walk enchant totals: %d of the round's %d enchant(s) cast on %d walk(s) (%d cast(s) sent), %d cast standing",
			c.walkCast, c.enchanted, c.walks, c.walkSent, c.enchanted-c.walkCast)
	}
	b.logf(ctx, "jewellery: round over: %s; %d bank visit(s), steps %s", out.Why, c.visits, stepCounts(c.steps))
	b.saveLedger(ctx)
	return b.jewelleryWaits(ctx, out, dec.Resume)
}

func spellWord(spell string) string {
	if spell == "" {
		return "by walking"
	}
	return "by " + spell
}

func paidWord(paid map[string]bool) string {
	var names []string
	for n, ok := range paid {
		if ok {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "no rune"
	}
	sort.Strings(names)
	return strings.Join(names, " and ")
}

func stepCounts(m map[mastercraftplan.JewelleryStep]int) string {
	keys := make([]string, 0, len(m))
	for s := range m {
		keys = append(keys, string(s))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[mastercraftplan.JewelleryStep(k)]))
	}
	return strings.Join(parts, ", ")
}

// jewelleryWaits sets what a jewellery round earned: it made or handed over something, so any wait is
// cleared; it failed for a passing reason, so it backs off; or it found nothing to do, so a round of a
// new batch (the mule's bank was stale, or it could not fund one) is paused for
// craftFruitlessPauseMinutes. A resumed round that found nothing (a batch the ledger remembered that is
// not there) pauses nothing: the batch is cleared, so nothing resumes again, and a pause would hold off
// the next batch the empty mule calls for. A Stop sets nothing. The hand-over's own backoff is
// afterRound's (recordWaits).
func (b *masterCrafterBot) jewelleryWaits(ctx *Context, out masterRoundOutcome, resumed bool) masterRoundOutcome {
	if ctx.Stopped() {
		return out
	}
	now := time.Now()
	led := &b.ledger
	switch {
	case out.Produced > 0 || out.Handed > 0:
		led.Succeed(mastercraftplan.ModeJewellery)
		led.Jewellery.PauseUntil = time.Time{}
	case out.Transient:
		b.failMode(ctx, mastercraftplan.ModeJewellery, now, out.Why)
	case resumed:
		// nothing to pause (see above)
	default:
		led.Jewellery.PauseUntil = mastercraftplan.PauseAfter(0, now, b.set.FruitlessPause)
	}
	return out
}

// subtractDrew takes what a bank visit drew (by item name, as the pack showed it) off a holding.
func subtractDrew(h mastercraftplan.JewelleryHeld, drew map[string]int) mastercraftplan.JewelleryHeld {
	h.Bars -= drew[mastercraftplan.GoldBar]
	h.Sapphires -= drew[mastercraftplan.CutSapphire]
	h.Necklaces -= drew[mastercraftplan.SapphireNecklace]
	h.Mould -= drew[mastercraftplan.NecklaceMould]
	h.Cosmic -= drew[mastercraftplan.RuneCosmic]
	h.Water -= drew[mastercraftplan.RuneWater]
	h.Coins -= drew[mastercraftplan.CoinsName]
	h.Emeralds -= drew[mastercraftplan.CutEmerald]
	h.EmeraldRings -= drew[mastercraftplan.EmeraldRing]
	h.RingMould -= drew[mastercraftplan.RingMould]
	h.Air -= drew[mastercraftplan.RuneAir]
	return h
}

// ownJewelleryHeld is this account's bank's round items as the session last saw it, else as the ledger
// last saw them (a restarted session has not opened the bank yet); known says the session has.
func (b *masterCrafterBot) ownJewelleryHeld(ctx *Context) (held mastercraftplan.JewelleryHeld, known bool) {
	if items := ownBankItems(ctx); items != nil {
		return mastercraftrunner.JewelleryOwn(items), true
	}
	return b.ledger.Jewellery.Bank, false
}

// ---- reading the account ----

// bankByName is the bank rows' count of the unnoted items called name (the teleport's runes).
func (c *masterJewellery) bankByName(banked []gamestate.SlotItem, name string) int {
	n := 0
	for _, id := range c.unnotedIDs(name) {
		n += int(actions.CountByID(banked, int32(id)))
	}
	return n
}

// unnotedIDs is the object ids called name that are not notes.
func (c *masterJewellery) unnotedIDs(name string) []int {
	var out []int
	if c.ctx.Config == nil {
		return out
	}
	for _, id := range c.ctx.Config.ObjIDs(name) {
		if o, ok := c.ctx.Config.Obj(id); ok && !o.Noted() {
			out = append(out, id)
		}
	}
	return out
}

// here is the account's tile (zero before the first position).
func (c *masterJewellery) here() tile.Tile {
	x, z, level, _ := c.ctx.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// ensureFurnace finds the Falador furnace by name and option through the loc index, once.
func (c *masterJewellery) ensureFurnace() bool {
	if c.haveFurn {
		return true
	}
	c.furn, c.haveFurn = jewelleryFind(c.d, mastercraftplan.JewelleryWorkBank, mastercraftplan.JewelleryFurnaceRadius)
	return c.haveFurn
}

// read refreshes the view from the account.
func (c *masterJewellery) read(started time.Time) {
	led := &c.b.ledger.Jewellery
	v := &c.view
	here := c.here()
	v.NearWork = lootchestfisherrunner.OnFaladorSide(here)
	if v.NearWork {
		c.ensureFurnace()
	}
	switch {
	case here.Level == c.post.Level && tile.Chebyshev(here, c.post) <= 10:
		v.Where = mastercraftplan.JewelleryAtPost
	case c.haveFurn && jewelleryReached(c.d, c.furn):
		v.Where = mastercraftplan.JewelleryAtFurnace
	case here.Level == mastercraftplan.JewelleryWorkBank.Level && tile.Chebyshev(here, mastercraftplan.JewelleryWorkBank) <= masterJewelleryBankRadius:
		v.Where = mastercraftplan.JewelleryAtWorkBank
	case c.atShop(here):
		v.Where = mastercraftplan.JewelleryAtShop
	default:
		v.Where = mastercraftplan.JewelleryAtOther
	}
	v.Kind = c.kind
	v.Asked, v.HandedOver, v.Futile, v.ShopFailed = c.asked, c.handedOver, c.futile, c.shopFailed
	v.Expired = mastercraftplan.RoundExpired(started, time.Now(), c.b.set.MaxRound)
	v.CraftOK, v.EnchantOK, v.WaterPaid = c.craftOK, c.enchantOK, c.paid[c.sp.Second]
	v.WalkEnchant = c.travel != nil && !c.walkOff
	v.Left, v.Owed = led.Left(), led.OwedTotal()
	v.Pack = mastercraftrunner.JewelleryPack(c.d).As(c.kind)
	v.PackOther = mastercraftrunner.JewelleryPackOtherFor(c.d, c.kind)
	v.Bank = c.bank.As(c.kind)
	if v.Where == mastercraftplan.JewelleryAtWorkBank {
		v.KeepSecond = mastercraftplan.JewelleryTravelRunes(mastercraftplan.LegHome, c.magic, c.paid)[c.sp.Second]
	} else {
		v.KeepSecond = 0
	}
}

// atShop says the account stands by the keeper of one of the mould shops.
func (c *masterJewellery) atShop(here tile.Tile) bool {
	for _, s := range c.shops {
		if here.Level == s.Stand.Level && tile.Chebyshev(here, s.Stand) <= masterJewelleryShopRadius {
			return true
		}
	}
	return false
}

// do runs one step. ok false is a failure to retry; end non-empty ends the round (why "transient"
// marks it a passing failure).
func (c *masterJewellery) do(step mastercraftplan.JewelleryStep) (ok bool, end, why string) {
	c.ctx.SetPhase("mastercraft/jewellery/" + string(step))
	// A level-up's dialog left open keeps a bank from opening and a use from being read (the Crafting
	// and Magic XP of a batch can cross a level): every step starts with it closed.
	masterDismissChat(c.ctx)
	switch step {
	case mastercraftplan.JewelleryStepAsk:
		return c.ask()
	case mastercraftplan.JewelleryStepBank:
		return c.bankVisit()
	case mastercraftplan.JewelleryStepToWork:
		return c.toWork()
	case mastercraftplan.JewelleryStepToFurnace:
		return c.toFurnace()
	case mastercraftplan.JewelleryStepCraft:
		return c.craft()
	case mastercraftplan.JewelleryStepEnchant:
		return c.enchant()
	case mastercraftplan.JewelleryStepShop:
		return c.shop()
	case mastercraftplan.JewelleryStepHome:
		return c.home()
	case mastercraftplan.JewelleryStepHandover:
		return c.handover()
	}
	return false, "unknown step " + string(step), ""
}

// ---- the steps ----

// ask banks the pack at the post, reads the bank and asks the mule for the batch's ingredients.
func (c *masterJewellery) ask() (bool, string, string) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	v := mastercraftrunner.DepositAll(c.d, c.post)
	if !v.Opened {
		return false, "", "the bank at the post: " + v.Err
	}
	rawOwn := mastercraftrunner.JewelleryOwn(v.Banked)
	c.bank = rawOwn
	own := rawOwn.As(c.kind)
	ownOf := func(item string) int { return c.bankByName(v.Banked, item) }
	var stock mastercraftplan.Stock
	if f := muleBankCount(ctx, b.mule); f != nil {
		stock = f
	}
	lines := mastercraftplan.PlanKindAsk(c.kind, led.Target, c.magic, c.paid, stock, own, ownOf, own.Mould > 0)
	c.fresh = true
	if len(lines) == 0 {
		b.logf(ctx, "jewellery: nothing to ask %s for (the batch is %d, the bank holds %d bars and %d %ss)", b.mule, led.Target, own.Bars, own.Sapphires, c.sp.Gem)
		c.asked = true
		return true, "", ""
	}
	// Everything as notes (one pack slot a line), the pack having just been banked.
	a := b.askMuleWith(ctx, c.d, lines, masterAskOpts{noted: true, banked: true})
	b.logf(ctx, "jewellery: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
	if a.Stopped {
		return false, "stopped", ""
	}
	if a.Traded == 0 {
		c.askFailed = !a.declinedEmpty()
		b.logf(ctx, "jewellery: nothing arrived: %s", a.Why)
	}
	c.asked = true
	led.Bank = rawOwn
	b.saveLedger(ctx)
	return true, "", ""
}

// bankVisit is a kit visit, at the post or the Falador west bank: the pack is banked whole and the
// next load drawn (mastercraftrunner.JewelleryKit, mastercraftplan.PlanJewelleryDraws).
func (c *masterJewellery) bankVisit() (bool, string, string) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	start := time.Now()
	atPost := c.view.Where == mastercraftplan.JewelleryAtPost
	var bank *actions.Bank
	var ok bool
	if atPost {
		bank, ok = mastercraftrunner.OpenBank(c.d, c.post)
	} else {
		bank, ok = chestOpenFaladorBank(c.ld, mastercraftplan.JewelleryWorkBank, 20)
	}
	if !ok || bank == nil {
		return false, "", "the bank would not open"
	}
	opened := time.Since(start)
	var planned []mastercraftplan.JewelleryDraw
	futile := false
	plan := func(banked []gamestate.SlotItem) []mastercraftplan.JewelleryDraw {
		after := mastercraftrunner.JewelleryOwn(banked).As(c.kind)
		if c.fresh {
			// The visit right after the ask reads what the mule's answer came to, and the batch is sized to it.
			c.fresh = false
			if pipeline := after.Necklaces + after.Pairs(); led.Enchanted+pipeline < led.Target {
				b.logf(ctx, "jewellery: the batch is cut from %d to %d: the bank holds %d %s(s) and %d bar-and-%s pair(s)", led.Target, led.Enchanted+pipeline, after.Necklaces, c.sp.Base, after.Pairs(), c.sp.Gem)
				led.Target = led.Enchanted + pipeline
			}
		}
		v := c.view
		v.Pack, v.PackOther, v.Bank = mastercraftplan.JewelleryHeld{}, 0, after
		v.Left, v.Owed = led.Left(), led.OwedTotal()
		in := mastercraftplan.JewelleryVisitFor(v, after, func(item string) int { return c.bankByName(banked, item) }, c.magic, c.paid)
		draws := mastercraftplan.PlanJewelleryDraws(in)
		if v.Working() && !mastercraftplan.JewelleryLoaded(draws) {
			futile = true
			return nil // nothing to load for the work that is left: no teleport runes either
		}
		for i := range draws {
			if draws[i].ID == 0 {
				if ids := c.unnotedIDs(draws[i].Name); len(ids) > 0 {
					draws[i].ID = int32(ids[0])
				}
			}
		}
		planned = draws
		return draws
	}
	res := mastercraftrunner.JewelleryKit(c.d, bank, plan)
	if res.Banked == nil && res.Err != "" {
		return false, "", res.Err
	}
	if res.Err != "" {
		b.logf(ctx, "jewellery: the bank visit said %s", res.Err)
	}
	// The bank as it now stands: what the deposit made it, less what the pack showed drawn.
	held := mastercraftrunner.JewelleryOwn(res.Banked)
	held = subtractDrew(held, res.Drew)
	c.bank = held
	led.Bank = held
	c.visits++
	where := "Falador"
	if atPost {
		where = "the post"
	}
	hk := held.As(c.kind)
	b.logf(ctx, "jewellery: banked at %s in %s (bank open in %s, kit %s): drew %s; the bank holds %d %s(s), %d %s(s), %d bar(s), %d %s(s), %d mould(s); %d of the batch left",
		where, time.Since(start).Round(100*time.Millisecond), opened.Round(100*time.Millisecond), res.Took.Round(100*time.Millisecond),
		mastercraftplan.JewelleryDrawCounts(planned), hk.Games, c.sp.Product, hk.Necklaces, c.sp.Base, hk.Bars, hk.Sapphires, c.sp.Gem, hk.Mould, led.Left())
	if futile {
		c.futile = true
		b.logf(ctx, "jewellery: the bank held nothing to load for the %d %s(s) of the batch that are left (no mould or coins for one, no runes, nothing to craft): wrapping up", led.Left(), c.sp.Product)
	}
	b.saveLedger(ctx)
	return true, "", ""
}

// ---- travel ----

// walk goes to dest on ScarabSwarm's wolf-free route (the ships and the spirit trees between Catherby
// and Falador), running.
//
// A walk that carries pieces to enchant (and the runes for them) casts the enchant on them as it goes
// (scarabrunner.Travel's walk poll; scarabplan/walkenchant.go has the gates), and what it cast is counted here,
// whether or not the walk arrived. Whatever is left is cast standing, by the enchant step at the other end.
func (c *masterJewellery) walk(dest tile.Tile, radius int, timeout time.Duration) bool {
	fleetrunner.EnsureRun(c.d)
	armed := c.armWalkEnchant()
	ok := masterWalkTo(c.d, dest, radius, timeout)
	if armed {
		c.takeWalkEnchant(dest)
	}
	return ok
}

// armWalkEnchant arms the walk's enchant when the pack holds pieces and the runes to cast on them; it reports
// whether it did.
func (c *masterJewellery) armWalkEnchant() bool {
	if c.travel == nil || c.walkOff || !c.enchantOK {
		return false
	}
	held := mastercraftrunner.JewelleryPack(c.d).As(c.kind)
	if held.Necklaces <= 0 || c.kind.Casts(held, c.paid[c.sp.Second]) <= 0 {
		return false
	}
	c.travel.ArmEnchant(c.d, fleetrunner.EnchantWalk{
		Spell: c.sp.Spell, BaseID: c.sp.BaseID, FinalID: c.sp.FinalID, Refusal: mastercraftplan.EnchantRefusal,
	})
	return true
}

// takeWalkEnchant ends the walk's enchanting, books what it cast against the ledger (the same books the standing
// enchant step keeps: what is made is owed to the mule at once) and logs the walk.
func (c *masterJewellery) takeWalkEnchant(dest tile.Tile) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	r := jewelleryWalkTake(c.travel, c.d)
	c.walks++
	c.walkSent += r.Sent
	c.walkCast += r.Landed
	c.enchanted += r.Landed
	led.Enchanted += r.Landed
	led.Made += r.Landed
	led.AddOwed(int(c.sp.FinalID), r.Landed)
	b.saveLedger(ctx)
	b.logf(ctx, "jewellery: walk enchant: %d cast(s) on the walk to %s (%d sent, +%d Magic XP over %d tick(s)), %d left to cast there (%s)",
		r.Landed, c.placeWord(dest), r.Sent, r.XP, r.Ticks, r.Left, firstNonEmpty(r.Reason, "walk ended"))
	switch r.Reason {
	case "level", "members", "item":
		c.enchantOK = false
		b.logf(ctx, "jewellery: %s was refused (%s): no more enchants this round", c.sp.Spell, r.Reason)
	case "stalled":
		c.walkOff = true
		b.logf(ctx, "jewellery: the casts on the walk never showed: enchanting standing for the rest of the round")
	}
}

// placeWord names dest for a log line.
func (c *masterJewellery) placeWord(dest tile.Tile) string {
	switch {
	case tile.Chebyshev(dest, mastercraftplan.JewelleryWorkBank) <= masterJewelleryBankRadius && dest.Level == mastercraftplan.JewelleryWorkBank.Level:
		return "the Falador west bank"
	case tile.Chebyshev(dest, c.post) <= 10 && dest.Level == c.post.Level:
		return "the post"
	case c.haveFurn && tile.Chebyshev(dest, tile.Tile{X: c.furn.X, Z: c.furn.Z, Level: c.furn.Level}) <= 3:
		return "the furnace"
	}
	return fmt.Sprintf("%d,%d", dest.X, dest.Z)
}

// legTimeout is the time a walk to dest has: the long bound when the account is not on the Falador
// side or dest is not, the short one for a leg inside it.
func (c *masterJewellery) legTimeout(dest tile.Tile) time.Duration {
	if lootchestfisherrunner.OnFaladorSide(c.here()) && lootchestfisherrunner.OnFaladorSide(dest) {
		return masterJewelleryLegWalk
	}
	return masterJewelleryLongWalk
}

// teleport casts spell when Magic and the pack's runes (and a worn staff) allow it; false when it was
// not cast or did not move the account. The staff is read afresh: it can change between visits.
func (c *masterJewellery) teleport(spell string) bool {
	if spell == "" {
		return false
	}
	c.paid = mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(c.d), c.b.set.MembersWorld)
	held := func(name string) int { return mastercraftrunner.PackCountNamed(c.d, name) }
	if !mastercraftplan.CanCastWith(spell, c.magic, c.paid, held) {
		return false
	}
	arrived, err := jewelleryTeleport(c.ctx, spell)
	if err != nil || !arrived {
		c.b.logf(c.ctx, "jewellery: %s did not land (%v) — walking instead", spell, err)
		return false
	}
	c.b.logf(c.ctx, "jewellery: cast %s, now at %s", spell, fleetrunner.HereIs(c.d))
	return true
}

// toWork goes to the Falador west bank: a Falador Teleport from anywhere else when one is castable and
// paid for, then the walk.
func (c *masterJewellery) toWork() (bool, string, string) {
	if !lootchestfisherrunner.OnFaladorSide(c.here()) {
		c.teleport(mastercraftplan.JewelleryLegSpell(mastercraftplan.LegToFalador, c.magic))
	}
	dest := mastercraftplan.JewelleryWorkBank
	if !c.walk(dest, 3, c.legTimeout(dest)) {
		return false, "", "could not reach the Falador west bank from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// toFurnace goes to the furnace: the Falador Teleport when one is castable and paid for, then the walk
// onto a tile the engine accepts a use from.
func (c *masterJewellery) toFurnace() (bool, string, string) {
	if !c.ensureFurnace() {
		return false, "", fmt.Sprintf("no %q with a %q option within %d tiles of the Falador west bank in the loc index",
			mastercraftrunner.FurnaceName, mastercraftrunner.FurnaceOp, mastercraftplan.JewelleryFurnaceRadius)
	}
	if !lootchestfisherrunner.OnFaladorSide(c.here()) {
		c.teleport(mastercraftplan.JewelleryLegSpell(mastercraftplan.LegToFalador, c.magic))
	}
	dest := tile.Tile{X: c.furn.X, Z: c.furn.Z, Level: c.furn.Level}
	if !jewelleryApproach(c.d, c.furn, c.walk, c.legTimeout(dest)) {
		return false, "", "could not stand where the engine accepts a use on the furnace, from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// craft makes the Sapphire necklaces of the pack's gold bars and Sapphires at the furnace.
func (c *masterJewellery) craft() (bool, string, string) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	if !c.ensureFurnace() {
		return false, "", "no furnace in the loc index near the Falador west bank"
	}
	if !jewelleryReached(c.d, c.furn) {
		if !jewelleryApproach(c.d, c.furn, c.walk, masterJewelleryLegWalk) {
			return false, "", "could not stand where the engine accepts a use on the furnace, from " + fleetrunner.HereIs(c.d)
		}
	}
	masterDismissChat(ctx) // a level-up left open stops the use (mastercrafter_chat.go)
	r := jewelleryMake(c.d, c.kind, c.furn, c.hold)
	b.logf(ctx, "jewellery: crafting: %s", r.Describe())
	c.crafted += r.Made
	led.Crafted += r.Made
	b.saveLedger(ctx)
	if ctx.Stopped() {
		return false, "stopped", ""
	}
	switch {
	case r.Made > 0 || r.Reason == "held" || r.Reason == "done":
		return true, "", ""
	case r.Reason == "no-mould":
		return false, "", "the pack holds no " + c.sp.Mould
	}
	return false, "", "no " + c.sp.Base + " was made (" + r.Reason + ")"
}

// enchant casts Lvl-1 Enchant on every Sapphire necklace in the pack. What it makes is owed to the mule
// at once, by quantity.
func (c *masterJewellery) enchant() (bool, string, string) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	masterDismissChat(ctx)
	r := jewelleryEnchant(c.d, c.kind, c.hold)
	b.logf(ctx, "jewellery: enchanting: %s", r.Describe())
	c.enchanted += r.Casts
	led.Enchanted += r.Casts
	led.Made += r.Casts
	led.AddOwed(int(c.sp.FinalID), r.Casts)
	b.saveLedger(ctx)
	if ctx.Stopped() {
		return false, "stopped", ""
	}
	switch r.Reason {
	case "level", "members", "item":
		c.enchantOK = false
		b.logf(ctx, "jewellery: %s was refused (%s): no more enchants this round", c.sp.Spell, r.Reason)
	case "runes":
		if r.Casts == 0 {
			// The view counts runes the server says are not there: wrap up rather than ask again.
			c.futile = true
			b.logf(ctx, "jewellery: the server says the runes are short although the pack and the bank count them: wrapping up")
		}
	}
	if r.Casts > 0 || r.Reason == "held" || r.Reason == "done" || !c.enchantOK {
		return true, "", ""
	}
	return false, "", "no " + c.sp.Base + " was enchanted (" + r.Reason + ")"
}

// shop buys a Necklace mould from the first shop that sells one, with the coins in the pack. A trip
// that buys none is not a failed step but a fact the round carries on from (ShopFailed): nothing is
// crafted without a mould.
func (c *masterJewellery) shop() (bool, string, string) {
	b, ctx := c.b, c.ctx
	led := &b.ledger.Jewellery
	var whys []string
	for _, s := range c.shops {
		if ctx.Stopped() {
			return false, "stopped", ""
		}
		if !lootchestfisherrunner.OnFaladorSide(c.here()) {
			c.teleport(mastercraftplan.JewelleryLegSpell(mastercraftplan.LegToFalador, c.magic))
		}
		if !c.walk(s.Stand, 3, c.legTimeout(s.Stand)) {
			whys = append(whys, s.Name+": could not walk there from "+fleetrunner.HereIs(c.d))
			continue
		}
		bought, why := jewelleryBuy(c.d, c.kind, s)
		if bought {
			led.MouldsBought++
			c.bought++
			b.logf(ctx, "jewellery: bought a %s at %s", c.sp.Mould, s.Name)
			b.saveLedger(ctx)
			return true, "", ""
		}
		whys = append(whys, s.Name+": "+why)
	}
	c.shopFailed = true
	c.transient = true
	b.logf(ctx, "jewellery: could not buy a %s: %s", c.sp.Mould, strings.Join(whys, "; "))
	return true, "", ""
}

// home goes back to the mule's post: from the Falador side the teleport home (Camelot's when Magic
// allows it, else Varrock's) when castable and paid for, and the walk.
func (c *masterJewellery) home() (bool, string, string) {
	if lootchestfisherrunner.OnFaladorSide(c.here()) {
		c.teleport(mastercraftplan.JewelleryLegSpell(mastercraftplan.LegHome, c.magic))
	}
	if !c.walk(c.post, 3, masterJewelleryLongWalk) {
		return false, "", "could not reach " + c.b.mule + "'s post from " + fleetrunner.HereIs(c.d)
	}
	return true, "", ""
}

// handover hands the owed Games necklaces to the mule; the round ends after it either way.
func (c *masterJewellery) handover() (bool, string, string) {
	handed, failed, why := c.b.handOverJewellery(c.ctx, c.d, c.post)
	c.bank = c.b.ledger.Jewellery.Bank // the hand-over banked what was left and read the bank as it then stood
	c.handed += handed
	c.handedOver = true
	if failed {
		c.b.logf(c.ctx, "jewellery: the hand-over left %d piece(s) owed: %s", c.b.ledger.Jewellery.OwedTotal(), why)
	}
	return true, "", ""
}

// handOverJewellery draws the owed Games necklaces out of the bank as notes, by quantity (the account's
// own necklace for its run stalling stays its own), and trades them to the mule at its post
// (masterDeliver), in up to masterHandoverPasses loads; then banks what did not cross and settles the
// owed necklaces against what crossed and what the bank still holds. It is handOverChest's, over the
// jewellery ledger.
func (b *masterCrafterBot) handOverJewellery(ctx *Context, d *fleetrunner.Deps, post tile.Tile) (handed int, failed bool, why string) {
	led := &b.ledger.Jewellery
	if led.OwedTotal() == 0 {
		return 0, false, ""
	}
	handedByID := map[int]int{}
	p := scarabMulePresenceOf(ctx, b.mule)
	switch {
	case ctx.Bus == nil || ctx.Username == "":
		why = "no wall bus to hand over on"
	case !p.Heard || !p.SameWorld:
		why = "the mule is not here: " + p.Why
	default:
		for pass := 0; pass < masterHandoverPasses && !ctx.Stopped(); pass++ {
			masterClearDead(ctx)
			ids, owed := led.OwedIDs()
			var wants []mastercraftrunner.LootWant
			var names []string
			seen := map[string]bool{}
			for _, id := range ids {
				left := owed[id] - handedByID[id]
				if left <= 0 {
					continue
				}
				name := masterObjName(ctx, id)
				wants = append(wants, mastercraftrunner.LootWant{ID: int32(id), Name: name, Qty: left})
				if !seen[strings.ToLower(name)] {
					seen[strings.ToLower(name)] = true
					names = append(names, name)
				}
			}
			if len(wants) == 0 {
				break
			}
			h, gotByID := mastercraftrunner.DrawLoot(d, post, wants)
			if !h.Opened || h.Err != "" && h.Rows == 0 {
				why = "drawing the necklaces: " + h.Err
				break
			}
			if h.Rows == 0 {
				break // none of it in the bank
			}
			rd := fleetrunner.MuleDeps(d, ctx.Bus, ctx.Username)
			WireDonorWorlds(ctx, rd)
			rd.HomeWorld = 0 // gated on the mule being on this world: no hop
			r := masterDeliver(d, rd, b.mule, post, h.Rows, h.Slots, names)
			result := "traded"
			switch {
			case r.Hopping:
				result = "hopping"
			case r.GaveUp:
				result = "gave-up"
			case !r.Traded:
				result = "not-traded"
			}
			b.logf(ctx, "HANDOVER jewellery mule=%s drawn=%s delivered=%s result=%s reason=%q pass=%d",
				b.mule, masterDescribe(h.Got), masterDescribe(r.Delivered), result, r.Reason, pass+1)
			creditByName(r.Delivered, wants, gotByID, handedByID)
			if r.Hopping {
				why = "the session is hopping worlds"
				break
			}
			if !r.Traded {
				why = fmt.Sprintf("the trade did not happen (%s): %s", result, r.Reason)
				break
			}
		}
	}
	for _, n := range handedByID {
		handed += n
	}
	led.Handed += handed
	v := mastercraftrunner.DepositAll(d, post)
	if !v.Opened {
		led.Settle(handedByID, nil)
		b.saveLedger(ctx)
		return handed, true, firstNonEmpty(why, "the bank would not open to bank what was left: "+v.Err)
	}
	led.Settle(handedByID, func(id int) int { return int(actions.CountByID(v.Banked, int32(id))) })
	led.Bank = mastercraftrunner.JewelleryOwn(v.Banked)
	b.saveLedger(ctx)
	return handed, led.OwedTotal() > 0, why
}
