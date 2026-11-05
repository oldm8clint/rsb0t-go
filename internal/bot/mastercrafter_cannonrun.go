package bot

// The cannon run round is not part of this build: it rides on a cannon rig that lives in a script this build leaves out. The
// plan never picks the round (its snapshot is left empty, so the planner reports "cannon run off"); the stub keeps the schedule's
// switch complete, and a decision that did name it ends the round at once instead of failing.

import (
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// cannonRunSnapshot would fill the round's part of a decision snapshot; here it leaves the round off.
func (b *masterCrafterBot) cannonRunSnapshot(ctx *Context, s *mastercraftplan.Snapshot) {}

// cannonRunRound ends at once: the cannon run is not in this build.
func (b *masterCrafterBot) cannonRunRound(ctx *Context, d *fleetrunner.Deps, started time.Time, dec mastercraftplan.Decision) masterRoundOutcome {
	return masterRoundOutcome{Mode: mastercraftplan.ModeCannonRun, Transient: true, Why: "the cannon run is not in this build"}
}
