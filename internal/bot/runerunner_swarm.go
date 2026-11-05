package bot

// RuneRunner's ScarabSwarm phase: what a rotating runner does while the shelves regrow
// (runerunnerplan/swarm.go has the rule and the owner's words).
//
// It is MasterCrafter's pattern (mastercrafter.go) the other way round: there ScarabSwarm is the
// default and a craft round the interruption; here the circuit is the default and ScarabSwarm
// fills the wait. ScarabSwarm runs whole, as scarabSwarmBot -- OnStart resumes it from its own
// checkpoint, Loop runs it, OnStop saves it -- with ctx.Settings swapped to the swarmSettings
// object for as long as it runs (RuneRunner reads its own settings only in OnStart, and the
// swap is undone when ScarabSwarm stops). It is stopped only at a safe point, by MasterCrafter's
// rules (mastercraftplan.SafePoint), with the place rule widened to the east side's surface,
// where the circuit itself works.
//
// The phase is persisted (State.Swarm), so a restart -- a ScarabSwarm world hop, a fleet restart
// -- comes back to ScarabSwarm, and the restock check keeps its clock (State.SwarmChecked).

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// runeRunnerSwarmWaitSay is how often a due return to the circuit says it is waiting for a safe
// point.
const runeRunnerSwarmWaitSay = 5 * time.Minute

// circuitWorth is what a whole circuit would buy now under the caps, from the board's estimate.
func (b *runeRunnerBot) circuitWorth(ctx *Context) int {
	if b.circuitWorthSeam != nil {
		return b.circuitWorthSeam(ctx)
	}
	pairs := runerunnerplan.RotationCircuit(b.shopsNow(ctx), b.set.Worlds, b.phase(ctx))
	return runerunnerplan.CircuitWorth(pairs, func(site string, world int) int { return b.worth(ctx, site, world) })
}

// circuitWorthAt is circuitWorth projected `after` from now by the content's restock rule from the board's freshest sighting.
func (b *runeRunnerBot) circuitWorthAt(ctx *Context, after time.Duration) int {
	if b.circuitWorthAtSeam != nil {
		return b.circuitWorthAtSeam(ctx, after)
	}
	at := time.Now().Add(after)
	pairs := runerunnerplan.RotationCircuit(b.shopsNow(ctx), b.set.Worlds, b.phase(ctx))
	return runerunnerplan.CircuitWorth(pairs, func(siteKey string, world int) int {
		site, ok := runerunnerplan.Site(siteKey)
		if !ok || !b.set.Enabled(siteKey) {
			return 0
		}
		if off, _ := b.pairGated(siteKey, world, at); off {
			return 0
		}
		return runerunnerplan.Worth(site.Shop, b.estimateAt(site, world, at), b.set.RuneList, b.set.CapTable)
	})
}

// fullShelfHorizon is how far on the full-shelf worth is projected: every shelf of the circuit is back at its base long before.
const fullShelfHorizon = 48 * time.Hour

// restockLine says how restocked the circuit is and when it will be worth walking.
func (b *runeRunnerBot) restockLine(ctx *Context, c runerunnerplan.SwarmCheck) string {
	target := c.Target()
	if c.CircuitWorth >= target {
		return fmt.Sprintf("restock: circuit worth %d of %d at full shelves (target %d): ready", c.CircuitWorth, c.FullWorth, target)
	}
	wait, ok := runerunnerplan.RestockWait(func(d time.Duration) int { return b.circuitWorthAt(ctx, d) }, target, 5*time.Minute, 12*time.Hour)
	if !ok {
		return fmt.Sprintf("restock: circuit worth %d of %d at full shelves (target %d): not ready within 12h", c.CircuitWorth, c.FullWorth, target)
	}
	return fmt.Sprintf("restock: circuit worth %d of %d at full shelves (target %d): ready in about %s", c.CircuitWorth, c.FullWorth, target, wait.Round(time.Minute))
}

// swarmCheck is the switch rules' input now.
func (b *runeRunnerBot) swarmCheck(ctx *Context) runerunnerplan.SwarmCheck {
	return runerunnerplan.SwarmCheck{
		On: b.set.SwarmWhileWaiting, Rotation: b.set.Rotation,
		CircuitWorth: b.circuitWorth(ctx), Resume: b.set.SwarmResumeRunes,
		FullWorth: b.circuitWorthAt(ctx, fullShelfHorizon), ReadyPercent: b.set.SwarmReadyPercent, MuleShort: b.muleShort,
	}
}

// afterRoundSwarm runs at the end of every round (endRound) and reports whether it took over:
// the delivery-only round before the switch has ended (switch now), or a rotation round has ended
// and the next circuit is not worth walking (plan the delivery-only round first).
func (b *runeRunnerBot) afterRoundSwarm(ctx *Context) bool {
	if b.st.SwarmDue {
		b.st.SwarmDue = false
		b.enterSwarm(ctx)
		return true
	}
	if !b.set.SwarmOn() || !b.st.Rotation {
		return false
	}
	check := b.swarmCheck(ctx)
	ok, why := runerunnerplan.SwarmAfterRound(check)
	b.logf(ctx, "%s", b.restockLine(ctx, check))
	if !ok {
		b.verbosef(ctx, "circuit done: staying on the circuit: %s", why)
		return false
	}
	m, _ := b.muleWorld(ctx)
	b.logf(ctx, "circuit done: %s; handing everything held to %s on world %d, then ScarabSwarm", why, b.set.Mule, m)
	b.st.StartRound(runerunnerplan.DeliveryRing(m), 0, time.Now())
	b.st.Rotation = true
	b.st.SwarmDue = true
	b.resumed = true
	b.save(ctx)
	return true
}

// enterSwarm switches to ScarabSwarm (started by the next Loop).
func (b *runeRunnerBot) enterSwarm(ctx *Context) {
	now := time.Now()
	b.st.Swarm, b.st.SwarmSince, b.st.SwarmChecked = true, now, now
	b.st.Ring, b.st.Cursor = nil, 0
	b.swarmBack = ""
	b.save(ctx)
	b.logf(ctx, "SWARM on: running ScarabSwarm while the shops restock; checking every %s, back to the circuit at %d runes",
		b.set.SwarmEvery(), b.set.SwarmResumeRunes)
}

// startSwarm starts a fresh ScarabSwarm on its own settings; it resumes from its checkpoint.
func (b *runeRunnerBot) startSwarm(ctx *Context) error {
	mk := b.newSwarm
	if mk == nil {
		mk = func() masterSwarm { return idleSwarm(nil) }
	}
	sw := mk()
	b.runnerSettings = ctx.Settings
	m := b.set.SwarmSettingsMap()
	b.noteRanged(ctx)
	m, added := runerunnerplan.WithRangedTraining(m, b.ranged, b.set.RangedTo40)
	if len(added) > 0 {
		b.sayEvery(ctx, "rangedtrain", time.Hour, "ranged %d: ScarabSwarm runs the Ranged stint to %d (%s switched on)", b.ranged, runerunnerplan.RangingGuildRanged, strings.Join(added, ","))
	}
	ctx.Settings = m
	if err := sw.OnStart(ctx); err != nil {
		ctx.Settings, b.runnerSettings = b.runnerSettings, nil
		return err
	}
	b.swarm = sw
	return nil
}

// stopSwarm ends ScarabSwarm's run (its OnStop saves its checkpoint) and gives ctx back its own
// settings.
func (b *runeRunnerBot) stopSwarm(ctx *Context) {
	if b.swarm == nil {
		return
	}
	b.swarm.OnStop(ctx)
	b.swarm = nil
	if b.runnerSettings != nil {
		ctx.Settings, b.runnerSettings = b.runnerSettings, nil
	}
}

// leaveSwarm stops ScarabSwarm and starts a rotation round from where the account stands.
func (b *runeRunnerBot) leaveSwarm(ctx *Context, why string) {
	since := time.Duration(0)
	if !b.st.SwarmSince.IsZero() {
		since = time.Since(b.st.SwarmSince).Round(time.Minute)
	}
	b.logf(ctx, "SWARM off after %s: %s; ScarabSwarm stopped at %s", since, why, fleetrunner.HereIs(b.deps(ctx)))
	b.stopSwarm(ctx)
	b.st.Swarm, b.st.SwarmDue = false, false
	b.swarmBack = ""
	b.resumed = false
	b.newRound(ctx, runerunnerplan.AreaOf(fleetrunner.Here(b.deps(ctx))), true)
}

// swarmLoop is one Loop of the ScarabSwarm phase: the restock check when due, the return to the
// circuit at a safe point when it said so, else one ScarabSwarm Loop.
func (b *runeRunnerBot) swarmLoop(ctx *Context) error {
	if b.swarm == nil {
		if err := b.startSwarm(ctx); err != nil {
			b.logf(ctx, "ScarabSwarm would not start (%v): back to the circuit", err)
			b.st.Swarm = false
			b.save(ctx)
			ctx.Delay(time.Minute)
			return nil
		}
	}
	now := time.Now()
	if b.swarmBack == "" && runerunnerplan.SwarmCheckDue(b.st.SwarmChecked, now, b.set.SwarmEvery()) {
		b.st.SwarmChecked = now
		check := b.swarmCheck(ctx)
		back, why := runerunnerplan.SwarmResume(check)
		b.logf(ctx, "%s", b.restockLine(ctx, check))
		if back {
			b.swarmBack = why
			b.swarmWaitSaid = time.Time{}
			b.logf(ctx, "restock check: %s — back to the circuit at the next safe point", why)
		} else {
			b.logf(ctx, "restock check: staying on ScarabSwarm — %s", why)
		}
		b.save(ctx)
	}
	if b.swarmBack != "" {
		in := b.swarm.safeInput(ctx)
		in.FarFromMule = b.farFromCircuit(ctx)
		if safe, why := mastercraftplan.SafePoint(in, mastercraftplan.DefaultSafeHpPercent); safe {
			b.leaveSwarm(ctx, b.swarmBack)
			return nil
		} else if time.Since(b.swarmWaitSaid) >= runeRunnerSwarmWaitSay {
			b.swarmWaitSaid = time.Now()
			b.logf(ctx, "back to the circuit is due (%s) but ScarabSwarm is not at a safe point: %s — carrying on", b.swarmBack, why)
		}
	}
	return b.swarm.Loop(ctx)
}

// farFromCircuit is the safe point's place rule for the return to the circuit: MasterCrafter's
// (the surface west of White Wolf Mountain, off the Wilderness and the Mage Bank, or a short walk
// from the mule's post), widened to the east side's surface, where Betty and Aubury are.
func (b *runeRunnerBot) farFromCircuit(ctx *Context) bool {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return true
	}
	t := tile.Tile{X: x, Z: z, Level: level}
	if consolidateSafePlace(&t) {
		return false
	}
	if t.Level == 0 && t.Z < masterUndergroundZ && fleetplan.WildernessLevel(t) == 0 && runerunnerplan.AreaOf(&t) == runerunnerplan.AreaEast {
		return false
	}
	post := b.mulePost(ctx)
	return t.Level != post.Level || tile.Chebyshev(t, post) > masterNearPost
}

// swarmStatus is the start line's word on the phase.
func (b *runeRunnerBot) swarmStatus() string {
	switch {
	case b.st.Swarm:
		return fmt.Sprintf("in the ScarabSwarm phase since %s", b.st.SwarmSince.Format("15:04"))
	case b.set.SwarmOn():
		return fmt.Sprintf("ScarabSwarm between circuits worth under %d runes, checked every %s", b.set.SwarmResumeRunes, b.set.SwarmEvery())
	}
	return "no ScarabSwarm between circuits"
}
