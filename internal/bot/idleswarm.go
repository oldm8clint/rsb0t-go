package bot

// The idle swarm: what MasterCrafter, RuneCarrier and RuneRunner run between their own work. In this
// repository it is ScarabSwarm, which registers itself in newIdleSwarm (scarabswarm_idle.go). A build
// without ScarabSwarm's files (the public client) leaves newIdleSwarm nil, and each script waits where it
// stands instead (waitIdle).

import (
	"time"

	"rsb0t-go/internal/scripts/mastercraftplan"
)

// newIdleSwarm builds the idle swarm; keep names items its loot hand-off must keep (nil: none). nil when
// ScarabSwarm is not built in.
var newIdleSwarm func(keep func() []string) masterSwarm

// idleSwarm is newIdleSwarm's swarm, or waitIdle without one.
func idleSwarm(keep func() []string) masterSwarm {
	if newIdleSwarm == nil {
		return &waitIdle{}
	}
	return newIdleSwarm(keep)
}

// waitIdle is the masterSwarm that does nothing: it waits in place between its script's rounds.
type waitIdle struct{}

func (w *waitIdle) OnStart(*Context) error { return nil }

func (w *waitIdle) OnStop(*Context) {}

func (w *waitIdle) Loop(ctx *Context) error {
	ctx.Delay(5 * time.Second)
	return nil
}

func (w *waitIdle) safeInput(ctx *Context) mastercraftplan.SafeInput { return masterSafeBase(ctx) }
