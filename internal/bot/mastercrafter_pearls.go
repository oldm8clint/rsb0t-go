package bot

// MasterCrafter's pearl round (a user request): when the mule holds craftPearlThreshold (100,000)
// oyster pearls or more, ask it for ALL of them and ALL its Bolts, cut every pearl into Pearl
// bolttips with the chisel as fast as the engine allows (the gem round's quick bank and pipelined
// bursts), then put the tips on the Bolts, fifty Pearl bolts a tick, and hand the Pearl bolts back.
// The content facts are internal/scripts/pearlplan's; the visits and the work
// internal/scripts/mastercraftrunner's (pearls.go, quick.go).
//
//	1. a full bank visit at the mule's post (deposit all but the chisel, read the bank)
//	2. the ask: every pearl of both kinds and every Bolts the mule publishes, a chisel if none
//	3. tips: load by load, 27 pearls (Oyster pearls first: 24 tips a use against 6), cut five a
//	   tick, the next booth op behind the last burst; when the bank has no pearls left and the
//	   mule still publishes some (its anyMaxPerOrder clamped the first ask), ask again
//	4. bolts: draw every tip and every Bolts (two stacks), five uses a tick, ten bolts a use; when
//	   tips are left and Bolts are not, ask the mule for its Bolts again
//	5. hand the Pearl bolts to the mule; once no tips are left, the Bolts the tips did not need go
//	   back too. Tips with no Bolts to go on stay in this account's bank, and the next decision
//	   resumes the round when the mule has Bolts again.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/pearlplan"
)

// masterPearlAsks bounds how many kit asks one pearl round makes (the first, and the top-ups when
// the mule's caps clamped a line).
const masterPearlAsks = 4

// pearlRound is the pearl round: see the file comment.
func (b *masterCrafterBot) pearlRound(ctx *Context, d *fleetrunner.Deps, started time.Time) masterRoundOutcome {
	out := masterRoundOutcome{Mode: mastercraftplan.ModePearls}
	post, why, ok := b.muleHere(ctx)
	if !ok {
		out.Transient, out.Why = true, why
		return out
	}
	if why, ok := b.arrive(ctx, d, post); !ok {
		out.Transient, out.Why = true, why
		return out
	}
	gd := mastercraftrunner.GemDeps(d)
	var qb mastercraftrunner.QuickBank

	// 1. Bank the pack, read the bank.
	v := mastercraftrunner.FullVisit(d, post, &qb, mastercraftrunner.KeepChisel, nil, false)
	if !v.Opened || v.Err != "" {
		out.Transient, out.Why = true, "the bank at the mule's post: "+v.Err
		return out
	}
	own := mastercraftrunner.PearlHeld(v.Banked)
	chisel := v.ChiselHeld || v.Count(gemplan.ChiselID) > 0

	// 2. Ask the mule for every pearl and every Bolts it publishes.
	asks := 0
	ask := func(why string) (got int, stopped bool, transient bool, reason string) {
		var stock mastercraftplan.Stock
		if f := muleBankCount(ctx, b.mule); f != nil {
			stock = f
		}
		a := mastercraftplan.PlanPearlAsk(stock, own, chisel)
		if len(a.Lines) == 0 {
			return 0, false, false, a.Why
		}
		asks++
		r := b.askMule(ctx, d, a.Lines)
		b.logf(ctx, "pearls: asked %s (%s) for %d oyster pearl(s) and %d Bolts: %s", b.mule, why, a.Pearls, a.Bolts, masterDescribe(r.Got))
		if r.Stopped {
			return 0, true, false, "stopped"
		}
		if r.Traded == 0 {
			return 0, false, !r.declinedEmpty(), r.Why
		}
		return r.Traded, false, false, ""
	}
	if got, stopped, transient, reason := ask("the round's first ask"); stopped {
		out.Why = "stopped"
		return out
	} else if got == 0 && own.PearlCount() == 0 && !(own.Tips > 0 && own.Bolts > 0) {
		out.Transient, out.Why = transient, reason
		return out
	}

	// 3. Tips: cut every pearl, load by load.
	var pace masterPace
	var pearlsCut, tipsMade, boltsMade, fails int
	reason := ""
	// hold runs before every load and every tick of tipping bolts: the stop, the round's budget,
	// and (twice a second, as every script's own guardian wait polls) a random event, which must
	// not have uses sent under it: a use clears the pending action (engine OpHeldUHandler).
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
	// visit makes a load visit: quick when it can, else a full one.
	visit := func(draw func(bank []gamestate.SlotItem) []int32, wantChisel bool) (mastercraftrunner.LoadVisit, time.Duration, bool) {
		at := time.Now()
		if qb.Ready(d) && (!wantChisel || gemrunner.HasChisel(gd)) {
			qv := qb.Visit(d, mastercraftrunner.KeepChisel, draw)
			if qv.Done {
				lv := mastercraftrunner.LoadVisit{Drew: qv.Drew, ChiselHeld: gemrunner.HasChisel(gd)}
				lv.Opened, lv.Banked = true, qv.AtOpen
				return lv, time.Since(at), true
			}
			b.logf(ctx, "pearls: quick bank visit fell short (%s); making a full one", qv.Err)
			pace.fallbacks++
		}
		return mastercraftrunner.FullVisit(d, post, &qb, mastercraftrunner.KeepChisel, draw, wantChisel), time.Since(at), false
	}
	// The six-tick runs (mastercraftrunner.FastPearls): on while craftFastPearls is and they keep in
	// step; fastKind is the pearl a full load of was just cut with the booth op primed behind it.
	fastOn, fastDesyncs := b.set.FastPearls, 0
	var fastKind pearlplan.Kind
	fastHold := func() bool {
		return !ctx.Stopped() && !mastercraftplan.RoundExpired(started, time.Now(), b.set.MaxRound) &&
			!ctx.Session.State().IsDead() && !randomevents.Busy(ctx.Session)
	}
tips:
	for hold() {
		masterDismissChat(ctx) // a level-up left open stops the uses and the bank alike (mastercrafter_chat.go)
		var lv mastercraftrunner.LoadVisit
		var bankTook time.Duration
		quick, visited := false, false
		if fastOn && fastKind.ID != 0 && qb.Primed() && qb.Ready(d) {
			fr := mastercraftrunner.FastPearls(d, &qb, fastKind, mastercraftrunner.FastOptions{MaxLoads: masterFastMaxLoads, Hold: fastHold})
			ran := fastKind
			fastKind = pearlplan.Kind{}
			if fr.Cuts > 0 {
				pearlsCut, tipsMade = pearlsCut+fr.Cuts, tipsMade+fr.Cuts*ran.Tips
				lootTips(ctx, ran, fr.Cuts, fr.Cuts*ran.Tips)
				pace.addFast(fr.Loads, fr.Took, fr.Ticks, fr.XP)
			}
			if fr.Reason != "not-fast" {
				b.logf(ctx, "pearls: %s: %s; %s", ran.Name, fr.Describe(), pace.say())
			}
			switch fr.Reason {
			case "done":
				fastDesyncs = 0
				continue // the pack holds a load's tips: the next visit is the 7-tick one
			case "not-fast":
				if l := fr.Load; l != nil && l.Opened && l.Err == "" {
					lv, quick, visited = *l, true, true
				}
			case "desync":
				pace.desyncs++
				if fastDesyncs++; fastDesyncs >= masterFastMaxDesyncs {
					fastOn = false
					b.logf(ctx, "pearls: %d six-tick runs in a row fell out of step; the 7-tick loads carry on for the rest of the round", fastDesyncs)
				}
				continue // the next visit resyncs from whatever the pack holds
			case "refused", "members":
				reason = "the chisel on " + ran.Name + " was refused (" + fr.Reason + ")"
				if fr.RefusedLevel > 0 {
					reason += fmt.Sprintf(": Fletching %d is needed", fr.RefusedLevel)
				}
				break tips
			case "no-open":
				pace.fallbacks++
				b.logf(ctx, "pearls: the bank did not open for the six-tick run (%s); the 7-tick visit tries again", fr.Why)
			default: // held, stopped: hold() at the top of the loop sees to it
				continue
			}
		}
		if !visited {
			lv, bankTook, quick = visit(mastercraftrunner.PearlDraw, true)
		}
		if !lv.Opened || (lv.Err != "" && len(lv.Drew) == 0) {
			if fails++; fails >= 3 {
				out.Transient, reason = true, "the bank would not open or draw: "+lv.Err
				break
			}
			ctx.Delay(3 * time.Second)
			continue
		}
		fails = 0
		if len(lv.Drew) == 0 {
			// No pearls left in the bank: ask again when the mule still publishes some (the first
			// ask was clamped by its caps), else the tips are all made.
			if mule := pearlplan.HeldIn(pearlplan.Stock(muleBankCount(ctx, b.mule))); mule.PearlCount() > 0 && asks < masterPearlAsks {
				if got, stopped, _, _ := ask("pearls the first ask left behind"); stopped {
					reason = "stopped"
					break
				} else if got > 0 {
					continue
				}
			}
			break
		}
		if !lv.ChiselHeld {
			reason = "no chisel in the pack or the bank"
			break
		}
		kind, _ := pearlplan.KindByID(lv.Drew[0])
		workAt := time.Now()
		r := mastercraftrunner.CutPearls(gd, kind, func() {
			if qb.Ready(d) {
				qb.Prime(d)
			}
		})
		pearlsCut, tipsMade = pearlsCut+r.Cut, tipsMade+r.Tips
		lootTips(ctx, kind, r.Cut, r.Tips)
		pace.add(quick, bankTook, time.Since(workAt), r.Ticks, r.XP)
		if r.Reason == "done" && r.Cut == mastercraftplan.FastLoadUses {
			fastKind = kind // a full load cut, the booth op primed: the next can be a six-tick run
		}
		if r.Reason != "done" || pace.due() {
			b.logf(ctx, "pearls: %s; %s", r.Describe(), pace.say())
		}
		switch r.Reason {
		case "refused", "members", "no-tool":
			reason = "the chisel on " + kind.Name + " was refused (" + r.Reason + ")"
			if r.RefusedLevel > 0 {
				reason += fmt.Sprintf(": Fletching %d is needed", r.RefusedLevel)
			}
			break tips
		case "stopped":
			reason = "stopped"
			break tips
		}
	}
	if pace.loads > 0 {
		b.logf(ctx, "pearls: tips over: %d pearl(s) cut into %d Pearl bolttips; %s", pearlsCut, tipsMade, pace.say())
	}

	// 4. Bolts: every tip on a Bolts, fifty a tick.
	if reason == "" {
		for hold() {
			lv, _, _ := visit(mastercraftrunner.BoltDraw, false)
			if !lv.Opened || (lv.Err != "" && len(lv.Drew) == 0) {
				if fails++; fails >= 3 {
					out.Transient, reason = true, "the bank would not open or draw: "+lv.Err
					break
				}
				ctx.Delay(3 * time.Second)
				continue
			}
			fails = 0
			if len(lv.Drew) == 0 {
				bank := mastercraftrunner.PearlHeld(lv.Banked)
				mule := pearlplan.HeldIn(pearlplan.Stock(muleBankCount(ctx, b.mule)))
				if bank.Tips > 0 && bank.Bolts == 0 && mule.Bolts > 0 && asks < masterPearlAsks {
					if got, stopped, _, _ := ask("Bolts for the tips left"); stopped {
						reason = "stopped"
						break
					} else if got > 0 {
						continue
					}
				}
				break // no tips, or no Bolts to put them on
			}
			r := mastercraftrunner.MakeBolts(gd, hold)
			boltsMade += r.Made
			lootPearlBolts(ctx, r.Made)
			b.logf(ctx, "pearls: %s", r.Describe())
			if r.Reason == "refused" || r.Reason == "members" {
				reason = fmt.Sprintf("tipping bolts was refused (%s, Fletching %d)", r.Reason, r.RefusedLevel)
				break
			}
			if r.Reason == "held" || r.Reason == "stopped" {
				break
			}
		}
	}
	out.Produced = pearlsCut + boltsMade
	b.ledger.PearlsCut += pearlsCut
	b.ledger.PearlBolts += boltsMade
	out.Why = fmt.Sprintf("%d oyster pearl(s) cut into %d Pearl bolttips, %d Pearl bolts made (%s)", pearlsCut, tipsMade, boltsMade, firstNonEmpty(reason, "every pearl and tip used"))

	// 5. Hand the Pearl bolts back; the Bolts too once no tips are left.
	ho := b.handOver(ctx, d, post, mastercraftrunner.PearlProducts())
	out.Handed = mastercraftrunner.Total(ho.Handed)
	b.ledger.PearlBoltsHanded += out.Handed
	out.HandoverTried, out.HandoverFailed = true, ho.failed()
	b.ledger.PendingDeliver = b.ledger.PendingDeliver || out.HandoverFailed
	if ho.Why != "" {
		out.Why += "; hand-over: " + ho.Why
	}
	if !out.HandoverFailed && ho.Opened && mastercraftrunner.PearlHeld(ho.Banked).Tips == 0 {
		back := b.handOver(ctx, d, post, mastercraftrunner.PearlLeftovers())
		if n := mastercraftrunner.Total(back.Handed); n > 0 {
			b.logf(ctx, "pearls: handed back %s the mule: %s", b.mule, masterDescribe(back.Handed))
			out.Handed += n
		}
		if back.failed() {
			out.HandoverFailed = true
			b.ledger.PendingDeliver = true
		}
	}
	return out
}
