package bot

// The Fire battlestaff round (a user request: "fire battlestaff making should trigger when the mule hold 5k battlestaff and 5k
// fire orbs to attach, attaching and banking as fast as possible"). The rules and the content facts are
// mastercraftplan/staves.go's; the run is mastercraftrunner.FastStaves, the bow-stringing stream on a Battlestaff and a Fire orb:
// 14 staffs a load in four ticks, the booth op last in its tick, the next load's bank events behind it.
//
//	trigger   the mule's published bank holds at least craftFireBattlestaveMinStaves Battlestaff and craftFireBattlestaveMinOrbs Fire
//	          orb (5,000 each), Crafting 62 or above, a members world; a round cut short resumes from this account's own bank.
//	where     the mule's post, its bank at Catherby: the same booth the bow and pearl rounds use.
//	asks      craftKitSize (the mule's per-ask cap, 1,000 by default) of each, as notes into the bank, again each time the bank
//	          holds less than a load, so 5,000 are worked in as many asks; the pack never holds more than one load.
//	end       no pair left in the bank and none at the mule, the hold, or the round's time; the Fire battlestaves go to the mule
//	          by the usual hand-over (it banks the pack first).
//
// Nothing here is lab-proven (owner rule): grep "mastercrafter: staves:" in the log.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
)

// masterStavesAsks bounds the kit asks one round makes: 5,000 pairs at the mule's cap of 1,000 a line, with room for the top-ups
// a capped ask leaves.
const masterStavesAsks = 40

// stavesSnapshot fills the round's part of a decision snapshot: this account's bank's Battlestaff and Fire orb.
func (b *masterCrafterBot) stavesSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {
	if bank := ownBankItems(ctx); bank != nil {
		s.OwnStaves, s.OwnOrbs = mastercraftrunner.StavesOwn(bank)
	}
}

// stavesRound is the Fire battlestaff round: see the file comment.
func (b *masterCrafterBot) stavesRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModeStaves}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	crafting := masterCraftingLevel(ctx)
	if crafting < mastercraftplan.StavesCraftingLevel {
		out.Why = fmt.Sprintf("Crafting %d is under the %d a Fire battlestaff needs", crafting, mastercraftplan.StavesCraftingLevel)
		return out
	}
	b.logf(ctx, "staves: trigger: %s; resume=%v; Crafting %d", dec.Why, dec.Resume, crafting)
	var qb mastercraftrunner.QuickBank

	// 1. Bank the pack, read the bank.
	v := mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	staves, orbs := mastercraftrunner.StavesOwn(v.Banked)
	asks, made, fails, desyncs := 0, 0, 0, 0
	reason := ""
	fastHold := func() bool {
		return !ctx.Stopped() && !mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) &&
			!ctx.Session.State().IsDead() && !randomevents.Busy(ctx.Session)
	}
	var guarded time.Time
	hold := func() bool {
		if ctx.Stopped() {
			reason = "stopped"
			return false
		}
		if mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) {
			reason = "the round's time (craftMaxRoundMinutes) is used up"
			return false
		}
		if time.Since(guarded) < 500*time.Millisecond {
			return true
		}
		guarded = time.Now()
		masterClearDead(ctx)
		if !fleetrunner.WaitForGuardian(d) {
			reason = "stopped"
			return false
		}
		return true
	}
	packBanked := true // the pack was banked whole at the last full visit
	var pace masterPace
	for hold() {
		masterDismissChat(ctx) // a level-up left open stops the uses and the bank alike (mastercrafter_chat.go)
		// Less than a load in the bank: ask the mule for the next kit (the cap's worth of each), or end when it holds none.
		if min(staves, orbs) < mastercraftplan.StavesLoad {
			lines := mastercraftplan.PlanStavesAsk(b.set, b.muleStock(ctx))
			if len(lines) == 0 || asks >= masterStavesAsks {
				reason = "no pair of Battlestaff and Fire orb left in the bank or at the mule"
				break
			}
			asks++
			a := b.askMuleWith(ctx, d, lines, masterAskOpts{noted: true, banked: packBanked})
			b.logf(ctx, "staves: asked %s for %s: %s", b.mule, describeLines(lines), masterDescribe(a.Got))
			if a.Stopped {
				reason = "stopped"
				break
			}
			if a.Traded == 0 {
				out.Transient = !a.declinedEmpty()
				reason = a.Why
				break
			}
			// The kit's notes go into the bank at a full visit, which also reads what it now holds.
			if v = mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false); !v.Opened || v.Err != "" {
				out.Transient, reason = true, "the bank at the mule's post: "+v.Err
				break
			}
			staves, orbs = mastercraftrunner.StavesOwn(v.Banked)
			packBanked = true
			continue
		}
		if !qb.Ready(d) {
			if v = mastercraftrunner.FullVisit(d, post, &qb, nil, nil, false); !v.Opened || v.Err != "" || !qb.Ready(d) {
				if fails++; fails >= 3 {
					out.Transient, reason = true, "no quick bank at the mule's post: "+v.Err
					break
				}
				ctx.Delay(3 * time.Second)
				continue
			}
			packBanked = true
		}
		if !qb.Primed() && !qb.Prime(d) {
			qb.Forget()
			if fails++; fails >= 3 {
				out.Transient, reason = true, "the quick bank would not prime"
				break
			}
			continue
		}
		fr := mastercraftrunner.FastStaves(d, &qb, mastercraftrunner.FastOptions{MaxLoads: masterFastMaxLoads, Hold: fastHold})
		packBanked = false
		if fr.Cuts > 0 {
			made += fr.Cuts
			staves, orbs = staves-fr.Cuts, orbs-fr.Cuts
			pace.addFast(fr.Loads, fr.Took, fr.Ticks, fr.XP)
			lootMade(ctx, loot.Crafting(ctx.Config, int(mastercraftplan.BattlestaffID), mastercraftplan.BattlestaffName), fr.Cuts,
				loot.Product(int(mastercraftplan.FireBattlestaffID), mastercraftplan.FireBattlestaff, fr.Cuts))
		}
		if fr.Reason != "not-fast" {
			b.logf(ctx, "staves: %s; %s", fr.Describe(), pace.say())
		}
		switch fr.Reason {
		case "done":
			fails, desyncs = 0, 0
		case "not-fast":
			// The run could not start: the visit banked the whole pack. Short says the bank holds less than a load.
			packBanked = true
			if fr.Short {
				staves, orbs = 0, 0
			}
		case "desync":
			pace.desyncs++
			if desyncs++; desyncs >= masterFastMaxDesyncs {
				reason = fmt.Sprintf("%d four-tick runs in a row fell out of step", desyncs)
				out.Transient = true
			}
		case "refused", "members":
			reason = "attaching the orb was refused (" + fr.Reason + ")"
		case "no-open":
			qb.Forget()
			if fails++; fails >= 3 {
				out.Transient, reason = true, "the bank did not open for the fast run: "+fr.Why
			}
		}
		if reason != "" {
			break
		}
	}
	b.ledger.Staves.Rounds++
	b.ledger.Staves.Made += made
	out.Produced = made
	out.Why = fmt.Sprintf("%d Fire battlestaves made in %d ask(s) (%s)", made, asks, firstNonEmpty(reason, "stopped"))
	b.logf(ctx, "staves: round over: %s; %s", out.Why, pace.say())

	// 2. The Fire battlestaves to the mule (the hand-over banks the pack first); any Battlestaff and Fire orb left stay in the bank.
	ho := b.handOver(ctx, d, post, mastercraftrunner.StavesProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	b.logf(ctx, "staves: handed %s %s", b.mule, masterDescribe(ho.Handed))
	return out
}
