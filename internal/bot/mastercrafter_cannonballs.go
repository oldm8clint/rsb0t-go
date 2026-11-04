package bot

// The cannonball round (a user request: "master should make cannonballs ... 2k cannon balls are needed per account"; the plan of 31
// Oct, docs/master_ranged_cannon_2026-10-31.md). mastercraftplan/cannonballs.go has the decision and every content fact.
//
//	trigger     the cannonballs on hand (the mule's published bank and this account's bank) are under craftCannonballTarget, the two
//	            banks hold at least craftCannonballMinBars Steel bars (mastercraftplan.CannonballsReady), Smithing is 35 and under the
//	            200 million cap, a members world, Dwarf Cannon is not known unfinished, and an Ammo mould is in the pack or the bank.
//	            A round ranks after the superheat round (which makes the Steel bars) and BEFORE the anvil round (which would turn
//	            them into arrowtips); the skill goal does not hold it (it is a supply round).
//	the mould   is untradeable (quest_mcannon.obj), so the mule cannot supply it: a round reads this account's bank at the mule's post
//	            and, when no Ammo mould is in it, ends there with that reason in the log and the round paused
//	            (craftFruitlessPauseMinutes). Nothing is smelted blind.
//	ask         a new round walks to the mule's post, banks its pack, and asks for the Steel bars the round smelts (as notes, up to
//	            craftCannonballBatch) that the own bank does not hold. Ledger.Cannon.Active is set and saved.
//	trips       Falador west bank <-> the Falador furnace (the jewellery round's pair, found by name and option through the loc index):
//	            the bank visit banks the pack but the mould and draws a full pack of 27 bars, the furnace is walked to, and each bar is
//	            one use of it (mastercraftrunner.SmeltCannonballs): four balls and 37.5 Smithing XP, ten ticks a bar. A trip is 270
//	            ticks.
//	hand-over   when the batch is smelted or the bank holds no bar, and also when a round is cut short (the balls made are the mule's),
//	            the cannonballs go to the mule. Ledger.Cannon.Active stays set only while bars were drawn and not smelted: the next check
//	            carries the round on from this account's own bank, with no trigger.
//
// SETTINGS (mastercraftplan.DefaultSettings, docs/mastercrafter_settings_reference.md):
//
//	craftCannonballTarget   int  default 60000  cannonballs the fleet should hold (mule + own bank); 0 turns the round off
//	craftCannonballBatch    int  default 500    most Steel bars one round smelts (2,000 balls)
//	craftCannonballMinBars  int  default 100    Steel bars (both banks) needed to start a round
//
// Nothing here is lab-proven (owner rule, 6 Oct 2026): the facts are content 289's, the tests are unit and engine-simulator ones, and
// the log lines (grep "mastercrafter: cannonballs:") are what to read live.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/lootchestfisherrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The seams of the trip loop: a test plays the bank, the furnace and the walk.
var (
	// masterCannonKitVisit opens the Falador west bank and runs the kit visit; opened is false when the bank could not be reached.
	masterCannonKitVisit = func(d *fleetrunner.Deps, remaining int) (res mastercraftrunner.CannonKitResult, opened bool) {
		bank, ok := mastercraftrunner.OpenBank(d, mastercraftplan.JewelleryWorkBank)
		if !ok || bank == nil {
			return res, false
		}
		return mastercraftrunner.CannonKit(d, bank, remaining), true
	}
	masterCannonFindFurnace = mastercraftrunner.FindFurnace
	masterCannonApproach    = mastercraftrunner.ApproachFurnace
	masterCannonSmelt       = mastercraftrunner.SmeltCannonballs
)

// masterCannonFails is how many trips in a row may fail before the round gives up for now, and masterCannonRetryWait the pause between
// them (a var: a test makes it 0).
const masterCannonFails = 3

var masterCannonRetryWait = 3 * time.Second

// cannonSnapshot fills the cannonball round's part of a decision snapshot.
func (b *masterCrafterBot) cannonSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	in := &s.Cannon
	if ctx.Session == nil {
		return
	}
	st := ctx.Session.State()
	in.SmithingXP = masterSkillXP(ctx, mastercraftrunner.SmithingStat)
	if status, ok := quests.Status(st, mastercraftplan.DwarfCannonQuest); ok {
		in.QuestKnown, in.QuestDone = true, status == quests.Complete
	}
	if inv := st.Inventory(actions.BankWithdraw); inv != nil {
		in.OwnKnown = true
		in.OwnSteel, in.OwnBalls, in.Mould = mastercraftrunner.CannonOwn(inv.NonEmpty())
	}
	if inv := st.Inventory(actions.PlayerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if it.ID == mastercraftplan.AmmoMouldID {
				in.Mould = true
			}
		}
	}
}

// cannonKeepsBars says the Steel bars in this account's bank are a cannonball round's stock: the deliver round does not hand them to the
// mule.
func (b *masterCrafterBot) cannonKeepsBars() bool { return b.ledger.Cannon.Active }

// cannonRun is one cannonball round's trip loop and its running totals.
type cannonRun struct {
	b    *masterCrafterBot
	ctx  *Context
	d    *fleetrunner.Deps
	plan mastercraftplan.CannonballPlan
	hold func() bool

	trips, bars, balls, xp, fails int
	// finished says the batch is smelted or the bank holds no bar: the round is complete, not cut short. refused says the furnace or the
	// bank refused for a reason that will not pass (no mould, the level, a free world); transient a passing failure.
	finished, refused, transient bool
	reason, lastFailedWhy        string
}

// failed notes a trip that came to nothing and says whether the round gives up now.
func (r *cannonRun) failed(why string) bool {
	r.fails++
	r.lastFailedWhy = why
	r.b.logf(r.ctx, "cannonballs: trip failed (%d in a row): %s", r.fails, why)
	if r.fails >= masterCannonFails {
		r.reason, r.transient = fmt.Sprintf("%d trips in a row failed: %s", r.fails, why), true
		return true
	}
	r.ctx.Delay(masterCannonRetryWait)
	return false
}

// loop is the trips, until the batch is smelted, the bank holds no bar, the hold ends the round or trips keep failing.
func (r *cannonRun) loop() {
	for {
		if !r.hold() {
			return
		}
		remaining := r.plan.Bars - r.bars
		if remaining <= 0 {
			r.finished, r.reason = true, fmt.Sprintf("the batch of %d bars is smelted", r.plan.Bars)
			return
		}
		kit, opened := masterCannonKitVisit(r.d, remaining)
		switch {
		case !opened:
			if r.failed("the Falador west bank would not open") {
				return
			}
			continue
		case kit.NoMould:
			r.refused, r.reason = true, "no Ammo mould in the pack or the bank (it cannot be traded: Nulodion's shop sells one after Dwarf Cannon)"
			return
		case kit.Err != "" && kit.Drew == 0:
			if r.failed("the bank visit: " + kit.Err) {
				return
			}
			continue
		case kit.Drew == 0:
			r.finished, r.reason = true, "no Steel bar left in the bank to smelt"
			return
		}
		r.trips++
		furn, ok := masterCannonFindFurnace(r.d, mastercraftplan.JewelleryWorkBank, mastercraftplan.JewelleryFurnaceRadius)
		if !ok {
			if r.failed(fmt.Sprintf("no %q with a %q option within %d tiles of the Falador west bank in the loc index", mastercraftrunner.FurnaceName, mastercraftrunner.FurnaceOp, mastercraftplan.JewelleryFurnaceRadius)) {
				return
			}
			continue
		}
		if !masterCannonApproach(r.d, furn, func(dest tile.Tile, radius int, timeout time.Duration) bool {
			return masterWalkTo(r.d, dest, radius, timeout)
		}, 90*time.Second) {
			if r.failed("could not reach a tile the furnace accepts a use from (at " + fleetrunner.HereIs(r.d) + ")") {
				return
			}
			continue
		}
		res := masterCannonSmelt(r.d, furn, r.hold)
		if res.Bars > 0 {
			r.bars += res.Bars
			r.balls += res.Balls
			r.xp += res.XP
			lootMade(r.ctx, loot.Forging(int(mastercraftplan.SteelBarID), mastercraftplan.SteelBar), res.Bars,
				loot.Product(int(mastercraftplan.CannonballID), mastercraftplan.Cannonball, res.Balls))
		}
		r.b.logf(r.ctx, "cannonballs: trip %d: %s; round so far: %d of %d bars -> %d cannonballs, +%d Smithing XP",
			r.trips, res.Describe(), r.bars, r.plan.Bars, r.balls, r.xp)
		switch res.Reason {
		case "held", "stopped":
			r.reason = "stopped"
			return
		case "members", "level", "no-mould":
			r.refused, r.reason = true, "the furnace refused: "+res.Reason
			return
		}
		if res.Bars == 0 {
			if r.failed("the furnace made nothing (" + res.Reason + ")") {
				return
			}
			continue
		}
		r.fails = 0
	}
}

// cannonballRound is the cannonball round: see the file comment.
func (b *masterCrafterBot) cannonballRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeCannonballs}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	plan := dec.Cannon
	smithing, _ := skillLevel(ctx, mastercraftrunner.SmithingStat)
	b.logf(ctx, "cannonballs: trigger: %s; resume=%v; Smithing %d; target %d, batch %d bars", dec.Why, dec.Resume, smithing, b.set.CannonballTarget, b.set.CannonballBatch)
	reason := ""
	hold := b.roundHold(ctx, d, started, &reason)

	// 1. A new round: the bank at the mule's post is read for the mould (never smelt blind), then the Steel bars are asked for.
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
		steel, _, mould := mastercraftrunner.CannonOwn(v.Banked)
		if !mould {
			out.Why = "no " + mastercraftplan.AmmoMould + " in this account's bank (read at the mule's post): it is untradeable, the mule cannot hand one over, and Nulodion's shop sells it only after " + mastercraftplan.DwarfCannonQuest + " — no round starts without it"
			b.logf(ctx, "cannonballs: NOT STARTING: %s", out.Why)
			return out
		}
		if status, ok := quests.Status(ctx.Session.State(), mastercraftplan.DwarfCannonQuest); ok && status != quests.Complete {
			out.Why = mastercraftplan.DwarfCannonQuest + " is not complete in the quest tab: no round starts"
			b.logf(ctx, "cannonballs: NOT STARTING: %s", out.Why)
			return out
		}
		lines := mastercraftplan.PlanCannonAsk(plan, b.muleStock(ctx), steel)
		if len(lines) > 0 {
			a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: true})
			b.logf(ctx, "cannonballs: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
			if a.Stopped {
				out.Why = "stopped"
				return out
			}
			if a.Traded == 0 && steel == 0 {
				out.Transient = !a.declinedEmpty()
				out.Why = a.Why
				return out
			}
		} else if steel == 0 {
			out.Why = "no Steel bar in the bank, and the mule's published bank holds none"
			return out
		}
	}
	if !b.ledger.Cannon.Active {
		b.ledger.Cannon.Active = true
		b.saveLedger(ctx)
	}
	b.logf(ctx, "cannonballs: round started: %d bars -> %d cannonballs (%d a bar, ten ticks a bar, 27 bars a trip)", plan.Bars, plan.Bars*mastercraftplan.CannonballsPerBar, mastercraftplan.CannonballsPerBar)

	// 2. The trips.
	run := &cannonRun{b: b, ctx: ctx, d: d, plan: plan, hold: hold}
	run.loop()
	if run.reason == "" {
		run.reason = reason
	}
	b.ledger.Cannon.Rounds++
	b.ledger.Cannon.Bars += run.bars
	b.ledger.Cannon.Balls += run.balls
	out.Produced = run.balls
	out.Transient = run.transient
	// Active stays set only for a round cut short with bars drawn and not smelted.
	if run.finished || run.refused {
		b.ledger.Cannon.Active = false
	}
	out.Why = fmt.Sprintf("%d bars smelted in %d trip(s) -> %d cannonballs (%s)", run.bars, run.trips, run.balls, firstNonEmpty(run.reason, "stopped"))
	b.logf(ctx, "cannonballs: round over: %s; +%d Smithing XP", out.Why, run.xp)
	b.saveLedger(ctx)
	if ctx.Stopped() {
		return out
	}

	// 3. The balls to the mule, whether or not the round finished: they are the fleet's.
	ho := b.handOver(ctx, d, post, mastercraftrunner.CannonProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	b.ledger.Cannon.Handed += out.Handed
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	b.logf(ctx, "cannonballs: handed %s %s", b.mule, masterDescribe(ho.Handed))
	return out
}

// cannonResumesHere says a resumed cannonball round carries on where the account stands when that is the Falador side: the bars are in
// this account's own bank, and the walk to the mule's post is for the hand-over at the end.
func (b *masterCrafterBot) cannonResumesHere(ctx *Context) bool {
	if ctx.Session == nil {
		return false
	}
	x, z, level, ok := ctx.Session.State().Tile()
	return ok && lootchestfisherrunner.OnFaladorSide(tile.Tile{X: x, Z: z, Level: level})
}
