package bot

// Every script's trawler crew (a user request: "while master is doing a fishing trawler round we should have every account
// running any script there at the trawler, more people, higher catches, more exp per hour"). trawler_catch_fish rolls once
// for every player aboard, and the crew's end-of-game logouts leave the whole catch to the anchor, so each account aboard
// adds to it. While the anchor's two-hour block is on (mulebus.TrawlerBlock), the Runner runs the FishingTrawler crew loop
// between its script's passes instead of the script, and hands back to the script once the block is over: a Loop pass is
// the point every script re-derives its state from, so the script carries on from where the account then stands.
//
// ScarabSwarm accounts with trawlerCrew set do this inside ScarabSwarm (scarabswarm_trawler.go); the scripts that are the
// trawler or its supply (trawlerCrewSkip) and the anchor and the mule (trawlerCrewNever) never join.
//
// A script may say when the crew may have its account (trawlerGate): RuneRunner joins only between two shop rounds (owner,
// 4 Nov 2026: "acct should only join trawler if they have completed thier full shopping route to ensure shops are empty
// before going to the trawler"; runerunner_trawler.go). The gate is asked before every join, which includes the first pass
// after every restart; the crew's end-of-game leave hop restarts the script, so it is asked again at every game boundary.

import (
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
	"rsb0t-go/internal/scripts/trawlerrunner"
)

// trawlerCrewSkip are the scripts the Runner never sends to the trawler.
var trawlerCrewSkip = map[string]bool{"MasterCrafter": true, "MuleReceiver": true, "FishingTrawler": true}

// trawlerCrewNever are the accounts that never join: the anchor and the mule.
var trawlerCrewNever = map[string]bool{"master": true, "mule1": true}

// trawlerCrewAnchor is the anchor whose block the Runner's crew follows.
const trawlerCrewAnchor = "master"

// trawlerGate is a script that says when the Runner's trawler crew may have its account. ready false keeps the account on
// its script; why is logged. A script that does not implement it joins whenever the block is on.
type trawlerGate interface {
	TrawlerReady(ctx *Context) (ready bool, why string)
}

// trawlerJoined is a gated script told when the crew has actually taken its account (after the crew's OnStart).
type trawlerJoined interface {
	TrawlerJoined(ctx *Context)
}

// trawlerHandBack is a script told when the crew hands its account back without a restart (the block over, stranded, an
// error), to put back what the crew changed.
type trawlerHandBack interface {
	TrawlerBack(ctx *Context)
}

// swarmTrawlerEvery spaces the crew's reads of the anchor's block, and its checks that the block still runs.
const swarmTrawlerEvery = 30 * time.Second

// runnerTrawler is the Runner's trawler crew state.
type runnerTrawler struct {
	checked time.Time
	// on and world are the anchor's block as last read (every swarmTrawlerEvery), so a gate's one-pass window between two
	// rounds is not lost to the read's timer.
	on    bool
	world int
	crew  *fishingTrawlerBot
	// skipUntil holds the account on its script after it was stranded on the way to the trawler (trawlerCrewStrandedSkip).
	skipUntil time.Time
	// heldSaid is when the gate's "not ready" line was last logged (trawlerGateSayEvery).
	heldSaid time.Time
}

// trawlerCrewStrandedSkip is how long an account that could not get back to the trawler stays on its own script.
const trawlerCrewStrandedSkip = 30 * time.Minute

// trawlerGateSayEvery bounds the gate's "not ready" line.
const trawlerGateSayEvery = 5 * time.Minute

// trawlerEligible reports whether this Runner's account and script join the anchor's blocks.
func (r *Runner) trawlerEligible() bool {
	ctx := r.ctx
	if ctx == nil || ctx.Bus == nil || trawlerCrewSkip[r.script] {
		return false
	}
	if trawlerCrewNever[strings.ToLower(strings.TrimSpace(ctx.Username))] {
		return false
	}
	// ScarabSwarm's own crew loop already takes these (and the scripts that run ScarabSwarm inside them).
	return !BoolSetting(r.settings, "trawlerCrew", false)
}

// trawlerGateAllows asks the script's gate, when it has one, and logs its answer: the hold at most every
// trawlerGateSayEvery, the join after a hold every time.
func (r *Runner) trawlerGateAllows(now time.Time) bool {
	g, ok := r.b.(trawlerGate)
	if !ok {
		return true
	}
	t := &r.trawler
	ready, why := g.TrawlerReady(r.ctx)
	if !ready {
		if t.heldSaid.IsZero() || now.Sub(t.heldSaid) >= trawlerGateSayEvery {
			t.heldSaid = now
			r.ctx.logf("trawler crew: %s's block is on (world %d) but %s is not ready: %s; carrying on", trawlerCrewAnchor, t.world, r.script, why)
		}
		return false
	}
	t.heldSaid = time.Time{}
	r.ctx.logf("trawler crew: %s: joining the trawler crew", why)
	return true
}

// trawlerOffDeck crosses an account standing on the docked boat's deck back to the pier by the gangplank, and reports
// whether it was on the deck. No walk leaves the deck, so a script handed the account there walks for nothing.
func trawlerOffDeck(ctx *Context) bool {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok || !trawlerplan.OnDeck(tile.Tile{X: x, Z: z, Level: level}) {
		return false
	}
	ctx.logf("trawler crew: on the docked boat's deck at %d,%d with no block to play: crossing the gangplank to the pier", x, z)
	trawlerrunner.LeaveDeck((&fishingTrawlerBot{}).deps(ctx))
	return true
}

// trawlerHandBack gives the account back to the script after the crew's OnStop.
func (r *Runner) trawlerHandBack() {
	r.trawler.crew = nil
	if h, ok := r.b.(trawlerHandBack); ok {
		h.TrawlerBack(r.ctx)
	}
}

// trawlerTick runs the crew loop while the anchor's block is on; handled is false when the script's own Loop should run.
func (r *Runner) trawlerTick() (handled bool) {
	t := &r.trawler
	ctx := r.ctx
	if ctx.Session == nil || !r.trawlerEligible() {
		return false
	}
	now := time.Now()
	fresh := false
	if now.Sub(t.checked) >= swarmTrawlerEvery {
		t.checked, fresh = now, true
		t.world, t.on = mulebus.TrawlerBlock(mulebus.ReadTrawler(ctx.Bus, now, mulebus.TrawlerBlockTTL), trawlerCrewAnchor)
	}
	if t.crew != nil {
		if fresh {
			if t.on {
				t.crew.settings.World = t.world
			} else if x, z, level, ok := ctx.Session.State().Tile(); ok && trawlerplan.AtSea(tile.Tile{X: x, Z: z, Level: level}) {
				// In a game: the crew loop plays it out (see scarabswarm_trawler.go).
			} else if trawlerOffDeck(ctx) {
				t.checked = time.Time{}
				return true
			} else {
				ctx.logf("trawler crew: %s's block is over: back to %s", trawlerCrewAnchor, r.script)
				t.crew.OnStop(ctx)
				r.trawlerHandBack()
				return false
			}
		}
		if t.crew.Stranded() {
			ctx.logf("trawler crew: stranded on the way to Port Khazard: back to %s for %s", r.script, trawlerCrewStrandedSkip)
			t.crew.OnStop(ctx)
			r.trawlerHandBack()
			t.skipUntil = now.Add(trawlerCrewStrandedSkip)
			return false
		}
		if err := t.crew.Loop(ctx); err != nil {
			ctx.logf("WARNING: trawler crew: %v: back to %s", err, r.script)
			t.crew.OnStop(ctx)
			r.trawlerHandBack()
			t.on = false // the next read of the block decides, not this pass
		}
		return true
	}
	if !t.on && fresh && trawlerOffDeck(ctx) {
		return true
	}
	if !t.on || now.Before(t.skipUntil) {
		return false
	}
	world := t.world
	// Not out of a fight or at low health: the script's own pass finishes that first.
	st := ctx.Session.State()
	if hp, ok := st.HP(); ok {
		if max, mok := st.MaxHP(); mok && max > 0 && hp*2 < max {
			return false
		}
	}
	if st.InCombat() {
		return false
	}
	if !r.trawlerGateAllows(now) {
		return false
	}
	crew := &fishingTrawlerBot{kitOptional: true}
	saved := ctx.Settings
	ctx.Settings = map[string]interface{}{"anchor": trawlerCrewAnchor, "world": world, "alch": true, "mule": StringSetting(saved, "mule", "mule1")}
	err := crew.OnStart(ctx)
	ctx.Settings = saved
	if err != nil {
		ctx.logf("WARNING: trawler crew: could not start the crew loop: %v", err)
		t.on = false
		return false
	}
	ctx.logf("trawler crew: %s's block is on (world %d): leaving %s between passes for the Fishing Trawler", trawlerCrewAnchor, world, r.script)
	t.crew = crew
	if j, ok := r.b.(trawlerJoined); ok {
		j.TrawlerJoined(ctx)
	}
	return true
}
