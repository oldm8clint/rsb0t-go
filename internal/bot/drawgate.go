package bot

import (
	"fmt"
	"math/rand"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
)

// drawGate is a donor script's check of the mule's queue BEFORE it draws a hand-off load: when
// the mule already expects more donors than it can serve within donorplan.DrawGateWait, the
// script keeps working and asks again later instead of drawing, walking over and standing. See
// donorplan.DeferDraw for the measured reason.
//
// It reads only the mule's own heartbeat (donorrunner.MuleQueue), so a fleet whose mule is older
// than the queue field, or has no bus, decides exactly as it did before the gate.
type drawGate struct {
	// deferred counts deferrals in a row; after donorplan.DrawGateMaxDeferrals the next check
	// lets the donor go regardless, so nobody is starved of a turn by an always-long queue.
	deferred int
	// force skips the next check: an operator's manual hand-off request is not second-guessed.
	force bool
}

// check reports whether to hold the draw back, for how long, and a log line saying why.
func (g *drawGate) check(ctx *Context, mule string) (wait time.Duration, why string, hold bool) {
	if g.force {
		g.force, g.deferred = false, 0
		return 0, "", false
	}
	queue, cycle, ok := donorrunner.MuleQueue(ctx.Bus, mule, time.Now())
	if !ok {
		return 0, "", false
	}
	d, deferIt := donorplan.DeferDraw(queue, cycle)
	if !deferIt {
		g.deferred = 0
		return 0, "", false
	}
	if g.deferred >= donorplan.DrawGateMaxDeferrals {
		g.deferred = 0
		return 0, "", false
	}
	g.deferred++
	// Spread the retries so a crowd deferred together does not come back together.
	d += time.Duration(rand.Int63n(int64(d/4) + 1))
	return d, fmt.Sprintf("%q already expects %d donor(s) at about %s each, more than %s of queue",
		mule, queue, cycle.Round(time.Second), donorplan.DrawGateWait), true
}
