package bot

// RuneRunner's answer to the Runner's trawler crew (runner_trawler.go; a user request: "acct should only join trawler
// if they have completed thier full shopping route to ensure shops are empty before going to the trawler"). The decision
// is runerunnerplan.TrawlerReady; this file adds the run's own state (a hop asked for, the ScarabSwarm phase) and records
// the join so the stint's clock survives the restarts every trawler game ends in.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// TrawlerReady implements trawlerGate.
func (b *runeRunnerBot) TrawlerReady(ctx *Context) (bool, string) {
	if !b.hopAsked.IsZero() {
		return false, fmt.Sprintf("a hop to world %d is under way", b.hopTo)
	}
	if b.st.Swarm {
		// Between circuits: the last circuit's shops are done and its runes with the mule.
		return true, "on ScarabSwarm between circuits"
	}
	if ctx.Session != nil {
		if x, z, level, ok := ctx.Session.State().Tile(); !ok || mulereceiverplan.IsWilderness(tile.Tile{X: x, Z: z, Level: level}) {
			// Never leave for the trawler from the Wilderness (a forced join out of a stalled round included): the round
			// walks out first.
			return false, "in the Wilderness, or the position is not known yet"
		}
	}
	now := time.Now()
	if b.trawlerHeld.IsZero() || b.trawlerHeldRound != b.st.Round {
		b.trawlerHeld, b.trawlerHeldRound = now, b.st.Round
	}
	v := runerunnerplan.TrawlerReady(b.st, now, b.set.TrawlerStint(), b.set.TrawlerRoundHoldMax(), b.trawlerHeld)
	if v.Ready {
		b.trawlerHeld = time.Time{}
	}
	return v.Ready, v.Why
}

// TrawlerJoined implements trawlerJoined: the crew has the account, so the stint's clock starts (saved, as every trawler
// game ends in a restart).
func (b *runeRunnerBot) TrawlerJoined(ctx *Context) {
	if b.st.NoteTrawlerJoin(time.Now(), b.set.TrawlerStint()) {
		b.save(ctx)
	}
}

// TrawlerBack implements trawlerHandBack: the crew's OnStop put the fleet's default hop bound back, and the account
// stands wherever the trawler left it.
func (b *runeRunnerBot) TrawlerBack(ctx *Context) {
	if b.set.Rotation && ctx.SetHopBudget != nil {
		n, window := runerunnerplan.FleetHopBudget(b.set.MaxHopsPerHour)
		if err := ctx.SetHopBudget(n, window); err != nil {
			b.logf(ctx, "hop budget not raised: %v", err)
		}
	}
	b.resumed = false
}
