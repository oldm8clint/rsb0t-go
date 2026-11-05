package bot

// What MasterCrafter does between rounds when craftIdle is "oaks" (a user request: two-tick oaks on master "between trawler
// blocks"): RetaliateChopper (retaliatechopper.go), the two-tick oak chopper at the Falador farm, runs whole from the same
// settings map. A chopper that stops (no kit, rats that cannot be phased, hitpoints under its floor) hands the time to
// ScarabSwarm, as craftIdle "swarm" would.
//
// THE SAFE POINT. The rats hitting the account are the method, not a fight, so a hit does not hold a round back; hitpoints
// still do (craftSafeHpPercent). Falador is east of White Wolf Mountain, which the safe point's place rule refuses for
// ScarabSwarm's accounts; master walks from the oaks to any round himself, so the rule does not hold him there.

import (
	"time"

	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/retaliateplan"
	"rsb0t-go/internal/scripts/thieverrunner"
)

// masterOaksBow is the bow the oaks round asks the mule for when master holds none he can wield: a plain Shortbow needs no
// Ranged level, and the chop's rolls do not depend on the bow (5 Nov 2026: master, Ranged 1, held only a Magic shortbow, so
// the chopper stopped at once with "no shortbow the account's Ranged level can wield").
const masterOaksBow = "Shortbow"

// masterOaksIdle is the between-rounds RetaliateChopper, with ScarabSwarm behind it.
type masterOaksIdle struct {
	b     *masterCrafterBot
	chop  *retaliateChopperBot
	swarm masterSwarm
}

func (o *masterOaksIdle) OnStart(ctx *Context) error {
	o.fetchBow(ctx)
	o.chop = &retaliateChopperBot{}
	return o.chop.OnStart(ctx)
}

// fetchBow gets master a STRUNG shortbow he can wield (a user request: he strings his own; mastercraftplan/oakbow.go has the plan):
// one worn or in the pack ends it; the unstrung Shortbow and a Bow string in the pack are strung; else they are drawn from this
// account's own bank; only then is the mule asked, for the unstrung bow by its qualified name (a plain "Shortbow" gave the unstrung
// obj 50 where the strung one is 841). The chopper's own gear step wields the result.
func (o *masterOaksIdle) fetchBow(ctx *Context) {
	d := o.b.deps(ctx)
	if d.Config == nil {
		return
	}
	ranged, _ := skillLevel(ctx, 4)
	for _, n := range mastercraftrunner.WornNames(d) {
		if retaliateplan.IsShortbow(n) && retaliateplan.BowRanged(n) <= ranged {
			return
		}
	}
	for _, n := range []string{"Shortbow", "Oak shortbow", "Willow shortbow", "Maple shortbow", "Yew shortbow", "Magic shortbow"} {
		if ids, err := bowstringrunner.ResolveIDs(d.Config, n); err == nil && retaliateplan.BowRanged(n) <= ranged && bowstringrunner.PackCount(d, ids.Strung) > 0 {
			return
		}
	}
	ids, err := bowstringrunner.ResolveIDs(d.Config, masterOaksBow)
	if err != nil {
		o.b.logf(ctx, "between rounds: no shortbow for Ranged %d and the %s ids are unknown (%v)", ranged, masterOaksBow, err)
		return
	}
	// The pack, the bank and the mule as the plan reads them.
	h := mastercraftplan.OakBowHeld{
		UnstrungPack: bowstringrunner.PackCount(d, ids.Unstrung), StringPack: bowstringrunner.PackCount(d, ids.String),
	}
	askName := bowstringplan.AskName(masterOaksBow, int(ids.Unstrung))
	if bank := ownBankItems(ctx); bank != nil {
		h.BankKnown = true
		for _, it := range bank {
			switch it.ID {
			case ids.Unstrung:
				h.UnstrungBank += int(it.Count)
			case ids.String:
				h.StringBank += int(it.Count)
			}
		}
	}
	if stock := o.b.muleStock(ctx); stock != nil {
		h.UnstrungMule, _ = stock(askName)
		h.StringMule, _ = stock(bowstringplan.BowString)
	}
	step := mastercraftplan.PlanOakBow(h)
	o.b.logf(ctx, "between rounds: no strung shortbow for Ranged %d: %s (pack %d bow, %d string; bank %d, %d; %s %d, %d)",
		ranged, step, h.UnstrungPack, h.StringPack, h.UnstrungBank, h.StringBank, o.b.mule, h.UnstrungMule, h.StringMule)
	switch step {
	case mastercraftplan.OakBowBank:
		o.drawFromBank(ctx, d, ids, h)
	case mastercraftplan.OakBowAsk:
		// The kit asks the mule once; what the own bank holds is not asked for. The axes stay in the pack through the kit's deposit.
		keep := []string{"Bronze axe", "Iron axe", "Steel axe", "Black axe", "Mithril axe", "Adamant axe", "Rune axe", "Dragon axe"}
		if lines := mastercraftplan.OakBowAskLines(h, askName, bowstringplan.BowString); len(lines) > 0 {
			a := o.b.askMuleWith(ctx, d, lines, masterAskOpts{keep: keep})
			o.b.logf(ctx, "between rounds: asked %s for %s: %s", o.b.mule, describeLines(lines), masterDescribe(a.Got))
		}
		if bowstringrunner.PackCount(d, ids.Unstrung) == 0 || bowstringrunner.PackCount(d, ids.String) == 0 {
			o.drawFromBank(ctx, d, ids, h)
		}
	case mastercraftplan.OakBowNone:
		o.b.logf(ctx, "between rounds: no unstrung %s and Bow string in the pack, the bank or %s's bank: the chopper will stop for want of a bow", masterOaksBow, o.b.mule)
		return
	}
	if bowstringrunner.PackCount(d, ids.Unstrung) > 0 && bowstringrunner.PackCount(d, ids.String) > 0 {
		r := bowstringrunner.StringAll(d, ids)
		o.b.logf(ctx, "between rounds: strung %d %s (%s)", r.Strung, masterOaksBow, r.Reason)
	}
}

// drawFromBank takes the unstrung bow and the string the pack lacks out of this account's bank at the mule's post.
func (o *masterOaksIdle) drawFromBank(ctx *Context, d *fleetrunner.Deps, ids bowstringrunner.IDs, h mastercraftplan.OakBowHeld) {
	post, why, ok := o.b.muleHere(ctx)
	if !ok {
		o.b.logf(ctx, "between rounds: the bank for the bow parts: %s", why)
		return
	}
	if why, ok := o.b.arrive(ctx, d, post); !ok {
		o.b.logf(ctx, "between rounds: the bank for the bow parts: %s", why)
		return
	}
	bank, ok := mastercraftrunner.OpenBank(d, post)
	if !ok || bank == nil {
		o.b.logf(ctx, "between rounds: the bank for the bow parts would not open")
		return
	}
	for _, p := range []struct {
		id   int32
		have int
	}{{ids.Unstrung, h.UnstrungPack}, {ids.String, h.StringPack}} {
		if p.have > 0 {
			continue
		}
		before := bowstringrunner.PackCount(d, p.id)
		if err := bank.WithdrawOne(p.id); err == nil {
			thieverrunner.Waiter(fleetrunner.Thiev(d)).DelayUntil(func() bool { return bowstringrunner.PackCount(d, p.id) > before }, 3*time.Second)
		}
	}
	_ = bank.Close()
	d.Delay(600 * time.Millisecond)
}

// chopping reports whether the chopper is still the one at work.
func (o *masterOaksIdle) chopping() bool {
	return o.swarm == nil && o.chop != nil && o.chop.stopped == "" && (o.chop.run == nil || o.chop.run.Stopped() == "")
}

func (o *masterOaksIdle) Loop(ctx *Context) error {
	if o.swarm == nil && !o.chopping() {
		why := ""
		if o.chop != nil {
			why = o.chop.stopped
			if why == "" && o.chop.run != nil {
				why = o.chop.run.Stopped()
			}
			o.chop.OnStop(ctx)
		}
		o.endCombatExpected(ctx)
		o.b.logf(ctx, "between rounds: the oak chopper stopped (%s): ScarabSwarm instead", why)
		o.swarm = idleSwarm(o.b.storeKeep)
		if err := o.swarm.OnStart(ctx); err != nil {
			return err
		}
	}
	if o.swarm != nil {
		return o.swarm.Loop(ctx)
	}
	return o.chop.Loop(ctx)
}

func (o *masterOaksIdle) OnStop(ctx *Context) {
	if o.swarm != nil {
		o.swarm.OnStop(ctx)
		return
	}
	if o.chop != nil {
		o.chop.OnStop(ctx)
	}
	o.endCombatExpected(ctx)
}

// endCombatExpected gives the guardian back its fleeing: the chopper told it the rats' hits are expected.
func (o *masterOaksIdle) endCombatExpected(ctx *Context) {
	if ctx.Guardian != nil {
		ctx.Guardian.CombatExpected = false
	}
}

// safeInput is the generic reading less the rats' hits while the chopper works (see THE SAFE POINT).
func (o *masterOaksIdle) safeInput(ctx *Context) mastercraftplan.SafeInput {
	if o.swarm != nil {
		return o.swarm.safeInput(ctx)
	}
	in := masterSafeBase(ctx)
	in.InCombat, in.Hurt = false, false
	return in
}

// masterDailyOaks is craftIdle "oaks-daily" (a user request: "2-tick oaks: 2 hours every 24 hours"; ScarabSwarm fills the rest): the
// oak chopper runs while the day's allowance (craftOakMinutes of every craftOakEveryMinutes, mastercraftplan.ScheduleLedger.OaksDue) is
// not used up, and ScarabSwarm otherwise. The chopping done is written to the ledger as it goes, so a restart does not give the day's
// two hours again.
type masterDailyOaks struct {
	b       *masterCrafterBot
	oaks    *masterOaksIdle
	swarm   masterSwarm
	since   time.Time // when the stint now under way began
	flushed time.Time // how far the stint is written to the ledger
}

// masterOaksFlush is how often chopping time is written to the ledger.
const masterOaksFlush = 5 * time.Minute

func (o *masterDailyOaks) OnStart(ctx *Context) error { return o.pick(ctx) }

// chopping reports whether the oak chopper is the one at work.
func (o *masterDailyOaks) chopping() bool { return o.oaks != nil && o.oaks.chopping() }

// pick starts the chopper when the allowance is not used up, ScarabSwarm otherwise.
func (o *masterDailyOaks) pick(ctx *Context) error {
	now := time.Now()
	led := &o.b.ledger.Sched
	if led.OaksDue(o.b.set, now, 0) {
		o.b.logf(ctx, "between rounds: the day's oak allowance is open (%s of %s used in the window since %s): 2-tick oaks", led.OakUsed.Round(time.Minute), o.b.set.Sched.OakWindow, led.OakDay.Format("15:04 2 Jan"))
		o.oaks, o.swarm = &masterOaksIdle{b: o.b}, nil
		o.since, o.flushed = now, now
		return o.oaks.OnStart(ctx)
	}
	o.oaks, o.swarm = nil, idleSwarm(o.b.storeKeep)
	return o.swarm.OnStart(ctx)
}

// stopStint ends the stint at hand, writing the chopping to the ledger.
func (o *masterDailyOaks) stopStint(ctx *Context) {
	if o.oaks != nil {
		o.flush(ctx, time.Now())
		o.oaks.OnStop(ctx)
		o.oaks = nil
	}
	if o.swarm != nil {
		o.swarm.OnStop(ctx)
		o.swarm = nil
	}
}

// flush writes the chopping since the last flush into the ledger's window.
func (o *masterDailyOaks) flush(ctx *Context, now time.Time) {
	if o.oaks == nil || !now.After(o.flushed) {
		return
	}
	o.b.ledger.Sched.NoteOaks(o.b.set, o.since, now.Sub(o.flushed))
	o.flushed = now
	o.b.saveLedger(ctx)
}

func (o *masterDailyOaks) Loop(ctx *Context) error {
	now := time.Now()
	led := &o.b.ledger.Sched
	if o.oaks != nil {
		if now.Sub(o.flushed) >= masterOaksFlush {
			o.flush(ctx, now)
		}
		if !led.OaksDue(o.b.set, now, 0) {
			o.b.logf(ctx, "between rounds: the day's oak allowance is used up (%s): ScarabSwarm", led.OakUsed.Round(time.Minute))
			o.stopStint(ctx)
			return o.pick(ctx)
		}
		return o.oaks.Loop(ctx)
	}
	if led.OaksDue(o.b.set, now, 0) {
		o.stopStint(ctx)
		return o.pick(ctx)
	}
	return o.swarm.Loop(ctx)
}

func (o *masterDailyOaks) OnStop(ctx *Context) { o.stopStint(ctx) }

func (o *masterDailyOaks) safeInput(ctx *Context) mastercraftplan.SafeInput {
	if o.oaks != nil {
		return o.oaks.safeInput(ctx)
	}
	if o.swarm != nil {
		return o.swarm.safeInput(ctx)
	}
	return masterSafeBase(ctx)
}
