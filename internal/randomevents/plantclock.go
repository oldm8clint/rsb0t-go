package randomevents

import (
	"time"

	"rsb0t-go/internal/gamestate"
)

// THE STRANGE PLANT'S CLOCK (plantfisher-1018, from the 18 Oct 2026 20:03:36 death).
//
// Content 289 (scripts/macro events/scripts/general/macro_event_triffid.rs2 and configs/antimacro.npc): the seed is added
// with ai_timer 45 and runs the same timer twice. The first fire, 45 ticks after it was added, only ends the growing
// phase (the pick answers "isn't ready" until then); the second, 90 ticks after it was added, changes its type to the
// angry one (npc_changetype_keepall) and sets it on its owner (opplayer2). In between it can be picked, with no check on
// the pack: inv_add drops what does not fit at the player's feet (Engine-TS InvOps INV_ADD), the message and the
// despawn still run. An angry plant follows its target only while the target is within maxrange 3 (+1) of the tile it
// was added at (Npc.validateTarget), and is deleted by its next timer once it has none.
//
// So the whole danger is one number. An account that picks before tick 90, or is more than a leash away from the
// seed's tile at tick 90, never takes a hit; one that is inside the leash at 90 takes a poisoning hit every few
// seconds that it cannot out-eat (a Kebab is 4 hitpoints against about 1.3 a second). The Guardian used to
// judge a pick by a fixed two seconds of wall time and try again every four, with no notion of how late it was.
// 18 Oct 20:01:51 to 20:03:06: about twenty clicks, not one reply, the account standing beside the seed the whole 97 ticks.
//
// So the Guardian counts the plant's age in ticks (NPC.VisibleTicks, plus what a silent or backlogged view is hiding),
// picks while it can, and when the pick has not landed by plantLeaveAtTicks (less the lag it has seen) it walks out of the
// leash while the plant is still passive and stays out until the plant is gone.
const (
	// plantGrowTicks is when the seed can first be picked.
	plantGrowTicks = 45
	// plantTurnTicks is when the seed turns on its owner.
	plantTurnTicks = 90
	// plantLeaveAtTicks is the age at which an unpicked seed is left instead: 24 ticks (14 s) before it turns, enough for
	// a six-step walk, a click that lands a few ticks late and a lag the view has not yet admitted to.
	plantLeaveAtTicks = 66
	// plantLagCap bounds how much a recent view lag moves the deadline forward: a minute of silence must not make the
	// account leave a plant at tick 20 and give up the fruit.
	plantLagCap = 20
	// plantBusyFromTicks is when the seed starts holding the account's script: nine ticks before it can be picked, so a
	// view that is a few ticks behind never leaves a pickable seed unsupervised.
	plantBusyFromTicks = plantGrowTicks - 9
	// plantClearTicks is when the Guardian stops treating a plant it engaged as live: an angry one with nobody to
	// follow is deleted by its next timer, 20 ticks after it turns.
	plantClearTicks = plantTurnTicks + 25
)

// plantLagAllowance is the ticks a recent view lag moves the leave deadline forward by.
func plantLagAllowance(lagTicks int) int { return min(max(lagTicks, 0), plantLagCap) }

// plantMustLeave reports whether an unpicked seed of this age (ticks, as the view counts them) has to be left now: it
// would turn before a walk could finish. lagTicks is the most ticks one recent lag episode hid.
func plantMustLeave(age, lagTicks int) bool {
	return age >= 0 && age+plantLagAllowance(lagTicks) >= plantLeaveAtTicks
}

// plantBusyWindow reports whether a plant of this age holds the account's script.
func plantBusyWindow(age int) bool { return age >= plantBusyFromTicks && age < plantClearTicks }

// plantReplyWaitFor is how long a pick click is waited on for its reply: the two seconds a tick-perfect link needs, plus
// the delay the route is known to have. A reply that comes after the wait is read on the next try (it is in the chat log
// and the pack by then), so the wait only decides how soon the guardian clicks again.
func plantReplyWaitFor(base time.Duration, lagTicks int, biteLag time.Duration, maxWait time.Duration) time.Duration {
	wait := base
	if lagTicks > 0 {
		if lagged := base + time.Duration(min(lagTicks, plantLagCap))*gamestate.ViewTick; lagged > wait {
			wait = lagged
		}
	}
	if biteLag > wait {
		wait = biteLag
	}
	if wait > maxWait {
		wait = maxWait
	}
	return wait
}

// plantAgeTicks is the plant's age in ticks as far as the view can tell: the ticks it has been in view, plus the ticks
// a silent view is not showing (the server keeps counting) or a backlog still has to deliver. ok is false when the plant
// is not in view.
func plantAgeTicks(state *gamestate.State, npcIndex int) (age int, ok bool) {
	if state == nil {
		return 0, false
	}
	for _, n := range state.NPCs() {
		if n.Index != npcIndex {
			continue
		}
		return n.VisibleTicks + hiddenViewTicks(state), true
	}
	return 0, false
}

// hiddenViewTicks is how many server ticks the view is behind the server now: the silence it has not ended yet, or the
// worst recent episode while a backlog is still being read.
func hiddenViewTicks(state *gamestate.State) int {
	lag := state.ViewLag()
	switch {
	case lag.Silent:
		return int(lag.Age / gamestate.ViewTick)
	case lag.CatchingUp:
		return lag.Recent.Ticks
	}
	return 0
}
