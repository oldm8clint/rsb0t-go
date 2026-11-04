package bot

// What MasterCrafter does between rounds when craftIdle is "post" (the default on 5 and 6 Oct 2026,
// ScarabSwarm again since the thresholds went to 20,000): stand at the mule's post and wait for the
// next check, instead of running ScarabSwarm.
//
// ScarabSwarm's Stage 3 walks to East Ardougne to train Magic, so every round began with the walk
// back to Catherby and every gap ended with the walk out again (a user request: "the main account has
// wandered all the way to ardougne"). Rounds are due every few minutes now (pearls from the 3M
// tips in its own bank, bows, gems, storage), so the account earns more at the post. craftIdle
// "swarm" brings ScarabSwarm back.

import (
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// masterIdleRadius is how far from the post the account may stand while it waits.
	masterIdleRadius = 6
	// masterIdleWait is one wait at the post between looks at the check clock.
	masterIdleWait = 3 * time.Second
	// masterIdleSay spaces the line saying why it cannot wait at the post.
	masterIdleSay = 5 * time.Minute
)

// masterPostIdle is the masterSwarm that waits at the mule's post.
type masterPostIdle struct {
	b      *masterCrafterBot
	saidAt time.Time
}

func (p *masterPostIdle) OnStart(*Context) error { return nil }

func (p *masterPostIdle) OnStop(*Context) {}

func (p *masterPostIdle) Loop(ctx *Context) error {
	post, why, ok := p.b.muleHere(ctx)
	if !ok {
		if time.Since(p.saidAt) >= masterIdleSay {
			p.saidAt = time.Now()
			p.b.logf(ctx, "between rounds: not at the post: %s", why)
		}
		ctx.Delay(masterIdleWait)
		return nil
	}
	if p.b.resumesHere(ctx, p.b.pending) {
		// A chest round under way is waiting for its safe point on the Falador side: walking to the post
		// now would only be walked back (mastercrafter_resume.go).
		ctx.Delay(masterIdleWait)
		return nil
	}
	d := p.b.deps(ctx)
	if here := fleetrunner.Here(d); here == nil || here.Level != post.Level || tile.Chebyshev(*here, post) > masterIdleRadius {
		if !masterWalkTo(d, post, 3, scarabConsolidateWalk) {
			ctx.Delay(masterIdleWait)
		}
		return nil
	}
	ctx.Delay(masterIdleWait)
	return nil
}

// safeInput is the generic reading: nothing of ScarabSwarm's (errands, trips) applies at the post.
func (p *masterPostIdle) safeInput(ctx *Context) mastercraftplan.SafeInput {
	return masterSafeBase(ctx)
}
