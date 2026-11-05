package bot

// The platebody round (a user request: "after the adamant is done i want master to teleport to varrock, travel to varrock west
// and use the anvils to make platebodies starting from the highest level bar available saving our steel bars for cannonball
// making ... this round should last for an hour regardless if all the bars get used or not. the platebodies are to be used for
// alching amongst the fleet"). The rules are mastercraftplan/platebody.go's; this is the I/O.
//
//	trigger     no Adamantite ore in this account's bank or the mule's, and bars for platebodies (never Steel) in the two
//	            banks (mastercraftplan.PlatebodyReady). Decide places it right after the superheat round.
//	start       a new round walks to the mule's post, banks the pack, asks for the bars (the highest first, up to
//	            craftPlatebodyKit) and a hammer, and the runes of one Varrock Teleport when the mule holds them; it casts the
//	            teleport (a walk when the runes or the Magic level are missing), and the Varrock west bank is next.
//	trips       the anvil round's: smithRun (mastercrafter_smith.go) with the platebody products, a full pack of the highest bar
//	            a trip, Make 10 / 5 / 1 at the anvil, every walk run-stalled and alching (smithTravel).
//	stop        the round's own hour (craftPlatebodyRoundMinutes, "regardless if all the bars get used or not"), or no bar left.
//	            Then the platebodies go to the mule by the usual hand-over (it banks the pack at the post first, so the leftover
//	            bars are in the bank), and the mode is paused craftRoundLimitPauseMinutes (roundlimit.go).
//
// Nothing here is lab-proven (owner rule): grep "mastercrafter: platebody:" in the log.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
)

// plateKeepsBars says the bars in this account's bank are a platebody round's stock: the deliver round does not hand them
// to the mule while a round is under way.
func (b *masterCrafterBot) plateKeepsBars() bool { return b.ledger.Plate.Active }

// plateRound is the platebody round: see the file comment.
func (b *masterCrafterBot) plateRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModePlatebody}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	// The walks rub or alch (High Level Alchemy only where a cast pays its runes, never on the round's own platebodies:
	// mastercraftplan/platealch.go); the allow-list is re-made once a fire staff is wielded below.
	travel := smithTravel(b.set)
	travel.Alch = mastercraftplan.PlateWalkAlch(mastercraftrunner.FireStaffWorn(d))
	d.Travel = travel
	defer func() { d.Travel = nil }()
	smithing, _ := skillLevel(ctx, mastercraftrunner.SmithingStat)
	products := mastercraftplan.PlateProductsFor(smithing)
	if len(products) == 0 {
		out.Why = fmt.Sprintf("Smithing %d allows no platebody", smithing)
		return out
	}
	plan := dec.Plate
	// The round's clock: a resumed round keeps the start it had.
	begin := started
	if dec.Resume && !plan.Started.IsZero() {
		begin = plan.Started
	}
	stalls, stallWhy := travel.StallMode(d)
	b.logf(ctx, "platebody: trigger: %s; resume=%v; Smithing %d; platebodies %s; the hour began %s; walks run-stalled: %v (%s)",
		plan.Why, dec.Resume, smithing, smithProductWord(products), begin.Format("15:04"), stalls, stallWhy)
	reason, limited := "", false
	baseHold := b.roundHold(ctx, d, started, &reason)
	hold := func() bool {
		if !baseHold() {
			return false
		}
		if mastercraftplan.LimitReached(b.set, mastercraftplan.ModePlatebody, "", begin, time.Now()) {
			limited = true
			reason = "the round's hour (craftPlatebodyRoundMinutes) is used up"
			return false
		}
		return true
	}

	// 1. A new round: the mule's bars (the highest first) banked here, and the runes of one Varrock Teleport in the pack.
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
		// A fire staff in hand (own bank, then the mule) is wielded for the walks' alchemy; with none, the allow-list below
		// asks 40 coins a cast instead of 20 (five Fire runes), and the weapon is put back at the round's end.
		staff, previous, swhy := b.fireStaffOpt(ctx, d, post, v.Banked, false)
		if swhy != "" {
			b.logf(ctx, "platebody: %s: alching on the walks pays over the Fire runes' cost too", swhy)
		}
		defer func() {
			if previous != 0 && staff != 0 {
				mastercraftrunner.Rewield(d, previous)
			}
		}()
		travel.Alch = mastercraftplan.PlateWalkAlch(mastercraftrunner.FireStaffWorn(d))
		own := mastercraftrunner.SmithOwn(v.Banked)
		lines := mastercraftplan.PlanPlateAsk(b.set, products, b.muleStock(ctx), own, false)
		if len(lines) > 0 {
			a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: true})
			b.logf(ctx, "platebody: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
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
			out.Why = "no bar for a platebody in the bank, and the mule's published bank holds none"
			return out
		}
		b.plateTeleport(ctx, d)
	}
	if !b.ledger.Plate.Active || !dec.Resume {
		b.ledger.Plate.Active, b.ledger.Plate.StartedAt = true, begin
		b.ledger.Plate.Rounds++
		b.saveLedger(ctx)
	}

	// 2. The trips: the anvil round's, on the platebody products.
	run := &smithRun{b: b, ctx: ctx, d: d, products: products, travel: travel, hold: hold, bars: map[string]int{}, started: begin}
	run.loop()
	if run.reason == "" {
		run.reason = reason
	}
	b.ledger.Plate.Bars += run.barsTotal()
	b.ledger.Plate.Bodies += run.bodies
	b.ledger.SmithBars += run.barsTotal()
	b.ledger.PlatebodiesMade += run.bodies
	out.Produced = run.barsTotal()
	out.Transient = run.transient
	out.Why = fmt.Sprintf("%d bars smithed in %d trip(s) -> %d platebodies (%s)", run.barsTotal(), run.trips, run.bodies, firstNonEmpty(run.reason, "stopped"))
	b.logf(ctx, "platebody: round over: %s; +%d Smithing XP", out.Why, run.xp)
	if ctx.Stopped() || (!run.usedUp && !limited) {
		b.saveLedger(ctx)
		return out // cut short: Active stays set and the next check carries on inside the hour
	}

	// 3. The hour is up, or no bar is left: the round is over, the platebodies go to the mule (the hand-over banks the pack
	// first, so the leftover bars are in the bank), and the mode rests.
	b.ledger.Plate.Active = false
	if limited {
		out.LimitReached = true
		b.logf(ctx, "platebody: %s", mastercraftplan.LimitWhy(mastercraftplan.ModePlatebody, b.set.PlatebodyRound, b.set.RoundLimitPause))
	}
	ho := b.handOver(ctx, d, post, mastercraftrunner.PlateProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	b.logf(ctx, "platebody: handed %s %s", b.mule, masterDescribe(ho.Handed))
	return out
}

// plateTeleport casts Varrock Teleport from the mule's post when the Magic level and the pack's runes (or a worn staff) allow
// it, asking the mule for the runes first; otherwise the round walks to Varrock west. It never fails the round.
func (b *masterCrafterBot) plateTeleport(ctx *Context, d *fleetrunner.Deps) {
	magic := fleetrunner.MagicLevel(d)
	paid := mastercraftplan.RunesPaidByStaff(mastercraftrunner.WornNames(d), b.set.MembersWorld)
	pack := func(name string) int { return mastercraftrunner.PackCountNamed(d, name) }
	if lines := mastercraftplan.PlateTeleportKit(magic, paid, pack, b.muleStock(ctx)); len(lines) > 0 {
		a := b.askMule(ctx, d, lines)
		b.logf(ctx, "platebody: asked %s for the runes of %s: %s", b.mule, mastercraftplan.PlateTeleportSpell, masterDescribe(a.Got))
	}
	if !mastercraftplan.CanCastWith(mastercraftplan.PlateTeleportSpell, magic, paid, pack) {
		b.logf(ctx, "platebody: no %s (Magic %d, the runes are not in the pack): walking to Varrock west", mastercraftplan.PlateTeleportSpell, magic)
		return
	}
	arrived, err := ctx.Teleport(mastercraftplan.PlateTeleportSpell, 0)
	if err != nil || !arrived {
		b.logf(ctx, "platebody: %s did not land (%v): walking to Varrock west", mastercraftplan.PlateTeleportSpell, err)
		return
	}
	b.logf(ctx, "platebody: cast %s, now at %s", mastercraftplan.PlateTeleportSpell, fleetrunner.HereIs(d))
}
