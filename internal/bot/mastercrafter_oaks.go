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
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/retaliateplan"
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

// fetchBow asks the mule for a Shortbow when no shortbow master can wield is worn or in the pack. The trade lands in the
// pack, where the chopper's gear step wields it. A refused ask is logged; the chopper's own stop then says why.
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
		if retaliateplan.BowRanged(n) <= ranged && mastercraftrunner.PackCountNamed(d, n) > 0 {
			return
		}
	}
	// The kit's deposit keeps the axes: the chop needs one carried while the bow is wielded.
	keep := []string{"Bronze axe", "Iron axe", "Steel axe", "Black axe", "Mithril axe", "Adamant axe", "Rune axe", "Dragon axe"}
	a := o.b.askMuleWith(ctx, d, []mastercraftplan.Line{{Item: masterOaksBow, Qty: 1}}, masterAskOpts{keep: keep})
	o.b.logf(ctx, "between rounds: no shortbow for Ranged %d: asked %s for a %s: %s", ranged, o.b.mule, masterOaksBow, masterDescribe(a.Got))
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
