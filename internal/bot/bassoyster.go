package bot

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
)

// BassOysterCaskets (owner, 2026-10-04) is ProgressiveFisher narrowed to the most Raw bass,
// Oyster pearls, caskets and casket loot per hour: the Big net at the Fishing Guild, with everything
// the net brings up that is not worth the space dropped, opened or cut while the account fishes.
//
// It IS ProgressiveFisher: the registry builds the same fisherBot with bassOyster set, from the
// same settings map, so an account switched over keeps its world hop, mule hand-off (caskets and
// oysters opened, key halves joined, then the trade: fisherplan.HandoffPrepOrder), random events,
// survival, Guild doors and tool spawn exactly as they are. What differs:
//
//   - target is always the Big net, pinned (fisherplan.BassOysterTarget); the target and
//     autoSharks settings are ignored.
//   - fishingGuild is always on; an account below Fishing 68 nets at Catherby until it can enter.
//   - The DROP CYCLE (fisherplan/cycle.go, fisherrunner/cycle.go; a user request): once the pack
//     has fisherplan.JunkDropFreeSlots or fewer free slots and the cycle has something to do, the
//     hold ends and one cycle runs, five clicks a game tick: the caskets are opened (coins, gems and a
//     key half stay), the key halves joined into a crystal key, the oysters opened, the Oyster pearls
//     cut into Pearl bolttips when Fletching is 41 and a chisel is carried, and the junk dropped with
//     the Empty oysters (Raw mackerel, Raw cod, Leather boots, Leather gloves, Seaweed, Empty oyster),
//     then the spot is clicked again. The same cycle runs inside a walk long enough to hold it and at
//     the bank before the booth is clicked, so the pack is tidy by the time it is banked and the bank
//     trip no longer opens anything on the way.
//
// Settings (all optional, read once at OnStart; every one defaults on):
//
//	openCaskets  bool  default true  -- open caskets in the cycle and join the key halves they give.
//	openOysters  bool  default true  -- open oysters in the cycle; the Empty oyster is dropped.
//	cutPearls    bool  default true  -- carry a chisel and cut Oyster pearls into Pearl bolttips in the
//	                                     cycle once Fletching is 41 (a pearl below that is banked).
//	cycleOnWalks bool  default true  -- also run the cycle in the middle of a walk that is long enough.
//	fletchTo41   bool  default true  -- train Fletching to 41 from the mule's logs (plain logs into arrow
//	                                     shafts, oak and willow into unstrung bows) so pearls can be cut;
//	                                     bassoyster_fletch.go.
func init() {
	Register("BassOysterCaskets", func() Bot { return &fisherBot{bassOyster: true} })
}

// cycleRetryAfter is the wait after a cycle that did nothing before the Loop tries again.
const cycleRetryAfter = 30 * time.Second

// applyBassOysterSettings overrides the two ProgressiveFisher settings this script fixes, and reads the
// drop cycle's own.
func (b *fisherBot) applyBassOysterSettings(ctx *Context) {
	b.target = fisherplan.BassOysterTarget()
	b.guild.enabled = true
	b.dropCycle = &fisherrunner.Cycle{
		Opts: fisherplan.CycleOptions{
			OpenCaskets: BoolSetting(ctx.Settings, "openCaskets", true),
			OpenOysters: BoolSetting(ctx.Settings, "openOysters", true),
			CutPearls:   BoolSetting(ctx.Settings, "cutPearls", true),
		},
		OnWalks: BoolSetting(ctx.Settings, "cycleOnWalks", true),
	}
	b.fletch.on = BoolSetting(ctx.Settings, "fletchTo41", true)
	o := b.dropCycle.Opts
	ctx.logf("fisher: BassOysterCaskets — Big net only (pinned), Fishing Guild on; drop cycle when the pack is nearly full (%d or fewer free): open caskets %v and join key halves, open oysters %v, cut pearls into tips at Fletching %d+ %v (train Fletching to %d from the mule's logs %v), on walks %v, dropping %v",
		fisherplan.JunkDropFreeSlots, o.OpenCaskets, o.OpenOysters, fisherplan.FletchGoal, o.CutPearls, fisherplan.FletchGoal, b.fletch.on, b.dropCycle.OnWalks, fisherplan.BassOysterJunk)
}

// dropCycleStep runs the drop cycle at a Loop boundary when it is due, and reports whether it took this
// Loop call. FishUntil runs it during a session itself; this catches a pack that filled some other
// way (a session that ended on its target, a relog) before the phase machine would bank it as it is.
func (b *fisherBot) dropCycleStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) bool {
	if s.MainModal != gamestate.NoModal || randomevents.Busy(ctx.Session) || time.Now().Before(b.dropCycleRetryAt) {
		return false
	}
	if !fisherrunner.CycleDue(d) {
		return false
	}
	ctx.SetPhase(phaselog.Fishing)
	if res := fisherrunner.RunCycle(d, "between fishing holds"); !res.Progress() {
		b.dropCycleRetryAt = time.Now().Add(cycleRetryAfter)
	}
	return true
}

// logDropCycleTotals is the stop line's account of what the drop cycle did over the run.
func (b *fisherBot) logDropCycleTotals(ctx *Context) {
	c := b.dropCycle
	if c == nil || c.Runs == 0 {
		return
	}
	ctx.logf("fisher: drop cycle totals — %d cycle(s): %d casket(s) opened, %d oyster(s) opened (%d pearl(s), %d empty shell(s) dropped), %d key half pair(s) joined, %d pearl(s) cut into %d Pearl bolttips, %d slot(s) dropped",
		c.Runs, c.Caskets, c.Oysters, c.Pearls, c.Empties, c.Joins, c.Cuts, c.Tips, c.Dropped)
}
