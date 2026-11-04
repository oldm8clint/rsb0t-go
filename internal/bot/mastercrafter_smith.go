package bot

// The anvil round (a user request: "mastercrafter should make steel bars and up only, add anvil smithing at
// varrock west, making arrow tips with the bars starting with best bar first, must have over 8000 steel bars
// to trigger it, iron bars can be turned into iron platebodies, must use jewellery stall running back and
// forth, iron platebodies to add to alch list"; then: "it should get all bars out when there are 8000 not
// just small amounts").
//
//	trigger     more than craftSmithMinSteelBars (8000) Steel bars in this account's bank and the mule's
//	            together (mastercraftplan.SmithReady). That STARTS a round; it is not what ends one.
//	ask         a new round walks to the mule's post, banks its pack, and asks for EVERY Runite, Adamantite,
//	            Mithril, Steel and Iron bar the mule holds (as notes, up to craftSmithKit of a bar) and a
//	            hammer when none is held. Ledger.SmithActive is set and saved at once.
//	trips       Varrock west bank <-> the nearest Varrock west anvil, over and over: the bank visit banks the
//	            products and keeps the hammer and the rub jewellery, then draws a FULL pack of bars, the best
//	            bar first (mastercraftplan.PlanSmithDraws); the anvil is walked to, a bar used on it and Make 10
//	            (Make 5, Make 1 for the rest) pressed per batch (mastercraftrunner.SmithAtAnvil). A make is five
//	            ticks (smithing.rs2 smithing_anvil). Runite, Adamantite, Mithril and Steel bars become
//	            arrowtips (15 a bar), Iron bars Iron platebodies (5 a platebody), until the bank holds none of
//	            a kind it can smith: NOT until Steel drops under the threshold.
//	run staller every walk of the round (to the mule, to the bank, to the anvil and back) carries a
//	            scarabrunner.Travel with the rub on (walkRub): a Games necklace or Ring of dueling rubbed every
//	            third tick of a run. The bank visit draws one from the bank when the pack has none. The hop
//	            between the bank and the anvil is only 10 to 15 tiles, under the least walk a rub fits in
//	            (scarabplan.RubAhead); the long walks, to and from the mule, are where it bites.
//	hand-over   when the bank holds no bar left to smith, the arrowtips and Iron platebodies go to the mule
//	            (the arrow round takes the Steel arrowtips from there; the Iron platebodies are on the alch
//	            list), and SmithActive is cleared. A round the time budget or a restart cut short leaves it set:
//	            the next check resumes from this account's own bank, with no threshold (mastercraftplan.SmithReady).

// SETTINGS (all in MasterCrafter's craft* keys, defaults in mastercraftplan.DefaultSettings, every one in
// docs/mastercrafter_settings_reference.md):
//
//	craftSmith                bool   default true    the anvil round on
//	craftSmithMinSteelBars    int    default 8000    Steel bars (both banks) that must be EXCEEDED to start a round
//	craftSmithArrowtips       bool   default true    arrowtips from Runite, Adamantite, Mithril and Steel bars (members world)
//	craftSmithIronPlatebodies bool   default true    Iron platebodies from Iron bars (5 each, Smithing 33)
//	craftSmithKit             int    default 100000  most of one bar a new round's kit line asks the mule for
//	craftSuperheatLowBars     bool   default false   superheat Bronze, Iron and Silver bars too (Steel and up only otherwise)
//
// Nothing here is lab-proven (owner rule, 8 Oct 2026): the facts are content 289's, the tests are unit and
// engine-simulator ones, and the log lines above (grep "mastercrafter: smith:") are what to read live.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The seams of the trip loop: a test plays the bank, the anvil and the walk.
var (
	// masterSmithKitVisit opens the Varrock west bank and runs the kit visit; opened is false when the bank
	// could not be reached.
	masterSmithKitVisit = func(d *fleetrunner.Deps, products []mastercraftplan.SmithProduct, travel *fleetrunner.Travel) (res mastercraftrunner.SmithKitResult, opened bool) {
		bank, ok := mastercraftrunner.OpenBank(d, mastercraftrunner.VarrockWestBank)
		if !ok || bank == nil {
			return res, false
		}
		return mastercraftrunner.SmithKit(d, bank, products, travel), true
	}
	masterFindAnvil     = mastercraftrunner.FindAnvil
	masterApproachAnvil = mastercraftrunner.ApproachAnvil
	masterSmithAnvil    = mastercraftrunner.SmithAtAnvil
)

// masterSmithFails is how many trips in a row may fail before the round gives up for now, and
// masterSmithRetryWait the pause between them (a var: a test makes it 0).
const masterSmithFails = 3

var masterSmithRetryWait = 3 * time.Second

// smithRun is one anvil round's trip loop and its running totals.
type smithRun struct {
	b        *masterCrafterBot
	ctx      *Context
	d        *fleetrunner.Deps
	products []mastercraftplan.SmithProduct
	travel   *fleetrunner.Travel
	hold     func() bool

	trips int
	// bars is the bars smithed per bar name, tips the arrowtips and bodies the Iron platebodies made.
	bars          map[string]int
	tips, bodies  int
	xp            int
	fails         int
	usedUp        bool
	transient     bool
	reason        string
	left          mastercraftplan.SmithStock
	started       time.Time
	lastFailedWhy string
}

func (r *smithRun) barsTotal() int {
	n := 0
	for _, v := range r.bars {
		n += v
	}
	return n
}

// failed notes a trip that came to nothing and says whether the round gives up now.
func (r *smithRun) failed(why string) bool {
	r.fails++
	r.lastFailedWhy = why
	r.b.logf(r.ctx, "smith: trip failed (%d in a row): %s", r.fails, why)
	if r.fails >= masterSmithFails {
		r.reason, r.transient = fmt.Sprintf("%d trips in a row failed: %s", r.fails, why), true
		return true
	}
	r.ctx.Delay(masterSmithRetryWait)
	return false
}

// leftWord is the bars still in the bank in words: the owner's "S steel, M mithril ..." line.
func smithLeftWord(left mastercraftplan.SmithStock) string {
	s, m, a, ru, i := mastercraftplan.SmithTotals(left)
	return fmt.Sprintf("%d steel, %d mithril, %d adamant, %d rune, %d iron bars", s, m, a, ru, i)
}

// loop is the trips, until the bank holds no bar to smith, the hold ends the round, or trips keep failing.
func (r *smithRun) loop() {
	for {
		if !r.hold() {
			return
		}
		kit, opened := masterSmithKitVisit(r.d, r.products, r.travel)
		switch {
		case !opened:
			if r.failed("the Varrock west bank would not open") {
				return
			}
			continue
		case kit.NoHammer:
			r.reason, r.transient = "no hammer in the pack or the bank (the mule gave none)", true
			return
		case kit.Err != "" && len(kit.Draws) == 0 && len(kit.Banked) == 0:
			if r.failed("the bank visit: " + kit.Err) {
				return
			}
			continue
		}
		// What the bank holds after this trip's draw.
		draw := map[string]int{}
		for _, dr := range kit.Draws {
			draw[dr.Product.Bar] += dr.N
		}
		inBank := mastercraftrunner.SmithOwn(kit.Banked)
		r.left = func(item string) int { return max(0, inBank(item)-draw[item]) }
		if len(kit.Draws) == 0 {
			r.usedUp = true
			r.reason = "no bar left in the bank to smith"
			return
		}
		r.trips++
		anvil, ok := masterFindAnvil(r.d, mastercraftrunner.VarrockWestBank, 20)
		if !ok {
			if r.failed("no anvil in the loc index near the Varrock west bank") {
				return
			}
			continue
		}
		if !masterApproachAnvil(r.d, anvil, func(dest tile.Tile, radius int, timeout time.Duration) bool {
			return masterWalkTo(r.d, dest, radius, timeout)
		}, 90*time.Second) {
			if r.failed("could not reach a tile the anvil accepts a use from (at " + fleetrunner.HereIs(r.d) + ")") {
				return
			}
			continue
		}
		var parts []string
		tripBars, tripFailed := 0, ""
		for _, dr := range kit.Draws {
			res := masterSmithAnvil(r.d, anvil, dr.Product, r.hold)
			if res.Bars > 0 {
				tripBars += res.Bars
				r.bars[dr.Product.Bar] += res.Bars
				units := res.Makes * dr.Product.Per
				if dr.Product.Kind == mastercraftplan.SmithArrowtipsKind {
					r.tips += units
				} else {
					r.bodies += units
				}
				r.xp += res.XP
				lootMade(r.ctx, loot.Forging(int(dr.Product.BarID), dr.Product.Bar), res.Bars, loot.Product(int(dr.Product.ID), dr.Product.Name, units))
				parts = append(parts, fmt.Sprintf("%d %s -> %d %s", res.Bars, dr.Product.Bar, units, dr.Product.Name))
			}
			switch res.Reason {
			case "done":
			case "held", "stopped":
				r.reason = "stopped"
				if r.ctx.Stopped() {
					return
				}
			case "level", "members":
				r.b.logf(r.ctx, "smith: %s was refused (%s) — leaving it for the rest of the round", dr.Product.Name, res.Reason)
				r.products = withoutProduct(r.products, dr.Product)
			default:
				tripFailed = dr.Product.Name + ": " + res.Reason
			}
		}
		// The round's progress, the trip's own line first.
		tipsNow := r.tips
		r.b.logf(r.ctx, "smith: trip %d: %s (%d bars); bank now holds %s; round so far: %d bars -> %d arrowtips, %d Iron platebodies, +%d Smithing XP",
			r.trips, strings.Join(parts, ", "), tripBars, smithLeftWord(r.left), r.barsTotal(), tipsNow, r.bodies, r.xp)
		if tripBars == 0 && tripFailed != "" {
			if r.failed("the anvil made nothing (" + tripFailed + ")") {
				return
			}
			continue
		}
		r.fails = 0
		if len(r.products) == 0 {
			r.reason = "every product was refused"
			r.transient = true
			return
		}
	}
}

func withoutProduct(ps []mastercraftplan.SmithProduct, drop mastercraftplan.SmithProduct) []mastercraftplan.SmithProduct {
	out := make([]mastercraftplan.SmithProduct, 0, len(ps))
	for _, p := range ps {
		if p.ID != drop.ID {
			out = append(out, p)
		}
	}
	return out
}

// smithKeepsBars says the bars in this account's bank are an anvil round's stock: the deliver round does not
// hand them to the mule.
func (b *masterCrafterBot) smithKeepsBars() bool { return b.ledger.SmithActive }

// ownRubBanked reports whether this account's own bank, as last read, holds an unnoted rub item.
func ownRubBanked(ctx *Context) bool {
	if ctx.Config == nil {
		return false
	}
	for _, it := range ownBankItems(ctx) {
		if o, ok := ctx.Config.Obj(int(it.ID)); ok && !o.Noted() && fleetplan.IsRubItem(o.Name) {
			return true
		}
	}
	return false
}

// smithTravel is the run staller the round's walks carry: the jewellery rub (a Games necklace or Ring of
// dueling rubbed every third tick of a run), no walk alchemy.
func smithTravel(set mastercraftplan.Settings) *fleetrunner.Travel {
	// High Level Alchemy on every walk when the account carries the kit, the ring's rub otherwise, and run kept
	// on with Strange fruit (a user request: the main account walked rubbing a ring with everything for alchemy in the
	// pack, Strange fruit held and 29% run energy).
	alch := fleetplan.WalkAlchSettings{Enabled: true, MinMagic: 55, Items: fleetplan.DefaultAlchItems, MinValue: 1}
	return &fleetrunner.Travel{Alch: alch.Normalised(), Rub: true, KeepRunning: true, Members: set.MembersWorld}
}

// smithSnapshot fills the anvil round's part of a decision snapshot: this account's bank's bars (nil until the
// session has read the bank).
func (b *masterCrafterBot) smithSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	if bank := ownBankItems(ctx); bank != nil {
		s.OwnSmith = mastercraftrunner.SmithOwn(bank)
	}
}

// smithRound is the anvil round: see the file comment.
func (b *masterCrafterBot) smithRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeSmith}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	travel := smithTravel(b.set)
	d.Travel = travel // every walk of the round (to the mule, the bank, the anvil, home) is run-stalled
	defer func() { d.Travel = nil }()
	smithing, _ := skillLevel(ctx, mastercraftrunner.SmithingStat)
	products := mastercraftplan.SmithProductsFor(b.set, smithing)
	if len(products) == 0 {
		out.Why = fmt.Sprintf("Smithing %d allows no product", smithing)
		return out
	}
	plan := dec.Smith
	stalls, stallWhy := travel.StallMode(d)
	b.logf(ctx, "smith: trigger: bank holds %d steel bars (need > %d); resume=%v; Smithing %d; products %s; walks run-stalled: %v (%s)",
		plan.Steel, b.set.SmithMinSteelBars, dec.Resume, smithing, smithProductWord(products), stalls, stallWhy)
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)

	// 1. A new round: the mule's bars, all of them, banked here.
	if !dec.Resume {
		if why, ok := b.arrive(ctx, d, post); !ok {
			out.Transient, out.Why = true, why
			return out
		}
		v := mastercraftrunner.DepositAll(d, post)
		if !v.Opened || v.Err != "" {
			out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
			return out
		}
		own := mastercraftrunner.SmithOwn(v.Banked)
		lines := mastercraftplan.PlanSmithAsk(b.set, products, b.muleStock(ctx), own, false)
		if len(lines) > 0 {
			a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: true})
			b.logf(ctx, "smith: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
			if a.Stopped {
				out.Why = "stopped"
				return out
			}
			if a.Traded == 0 && mastercraftplan.SmithMakes(products, own) == 0 {
				out.Transient = !a.declinedEmpty()
				out.Why = a.Why
				return out
			}
		} else if mastercraftplan.SmithMakes(products, own) == 0 {
			out.Why = "no bar to smith in the bank, and the mule's published bank holds none"
			return out
		}
	}
	// The round has started: remembered through a restart until the bank holds nothing to smith.
	if !b.ledger.SmithActive {
		b.ledger.SmithActive = true
		b.saveLedger(ctx)
	}
	b.logf(ctx, "smith: round started: %s — smithing all of them (Rune, Adamant, Mithril, Steel into arrowtips best first, Iron into platebodies) until none is left, not just down to %d steel",
		plan.Describe(), b.set.SmithMinSteelBars)

	// A resumed round with no rub jewellery anywhere fetches one first: every walk of the round rubs (owner, 8
	// Oct 2026: "must use jewellery stall running back and forth"). A new round's ask carries it already.
	// The bank's own rub item first: the first bank visit draws it (live 8 Oct 2026 the main account kept one Ring of
	// dueling back from a jewellery hand-over, then waited three minutes on the mule for another).
	if dec.Resume && !stalls && !ownRubBanked(ctx) {
		if name, ok := mastercraftplan.MuleRubItem(b.muleStock(ctx)); ok {
			if why, ok := b.arrive(ctx, d, post); ok {
				a := b.askMuleWith(ctx, d, []mastercraftplan.Line{{Item: name, Qty: 1}}, masterAskOpts{})
				b.logf(ctx, "smith: asked %s for a %s for the walks' rubs: %s", b.mule, name, masterDescribe(a.Got))
			} else {
				b.logf(ctx, "smith: no rub jewellery and the mule's post is out of reach (%s): walking without the rub", why)
			}
		}
	}

	// 2. The trips.
	run := &smithRun{b: b, ctx: ctx, d: d, products: products, travel: travel, hold: hold, bars: map[string]int{}, started: started}
	run.loop()
	if run.reason == "" {
		run.reason = reason
	}
	b.ledger.SmithBars += run.barsTotal()
	b.ledger.ArrowtipsMade += run.tips
	b.ledger.PlatebodiesMade += run.bodies
	out.Produced = run.barsTotal()
	out.Transient = run.transient
	out.Why = fmt.Sprintf("%d bars smithed in %d trip(s) -> %d arrowtips, %d Iron platebodies (%s)", run.barsTotal(), run.trips, run.tips, run.bodies, firstNonEmpty(run.reason, "stopped"))
	b.logf(ctx, "smith: round over: %s; +%d Smithing XP", out.Why, run.xp)
	if !run.usedUp {
		b.saveLedger(ctx)
		return out // cut short: SmithActive stays set and the next check carries on from the bank
	}

	// 3. Nothing left to smith: the products to the mule, and the round is finished.
	b.ledger.SmithActive = false
	ho := b.handOver(ctx, d, post, mastercraftrunner.SmithProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	b.logf(ctx, "smith: handed %s %s", b.mule, masterDescribe(ho.Handed))
	return out
}

func smithProductWord(ps []mastercraftplan.SmithProduct) string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}
