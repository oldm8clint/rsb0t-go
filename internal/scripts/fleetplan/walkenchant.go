package fleetplan

// Enchanting while walking (a user request: "ring enchanting should be done while walking instead of while
// standing at the furnace"). The decision; the casting is scarabrunner/travelenchant.go, the walk hook is the same
// webwalk.WalkOptions.Aside High Level Alchemy rides (walkalch.go has the engine facts, which hold here too).
//
// What differs from alchemy, read from content 289 (skill_magic/scripts/spells/enchant.rs2):
//
//   - the label ends in p_delay(1), not p_delay(3): delayedUntil = tick + 2, so one cast is accepted every
//     three ticks (WalkEnchantCastTicks; mastercraftplan.EnchantTicks is the same three), not five;
//   - the cast replaces the item in its own slot (inv_setslot), so the pack keeps its shape and the piece that
//     was cast on is the one that changed: the cast is confirmed by the enchanted piece's count rising, which is
//     also what mastercraftrunner.CastEnchantKind waits for, and a cast the engine read inside the delay (it
//     drops one without a word) is told by that count not rising;
//   - nothing is paid back by the pack but runes, so there is no coin or allow-list rule.

import "time"

const (
	// WalkEnchantCastTicks is the ticks from one accepted enchant to the next: p_delay(1) plus the tick the
	// delay ends on.
	WalkEnchantCastTicks = 3
	// WalkEnchantGapTicks is how many client ticks must have been seen after a cast landed before the next is
	// sent, so that the engine reads it on the third tick, the first it takes one (mastercraftrunner.CastEnchantKind's
	// gap, proven live). A cast that does not land widens it by a tick.
	WalkEnchantGapTicks = WalkEnchantCastTicks - 1
	// WalkEnchantEndAhead is the least tiles to a leg's end for a cast to go: a cast sent as the account arrives
	// is worth nothing to the walk, the standing step does it at once. The walker sends no click after the
	// leg's last one, so no more distance is needed than this.
	WalkEnchantEndAhead = 3
)

// WalkEnchantAhead is the least distance (tiles) the walk's click target must still be ahead for a cast to fit
// before the walker's next click (WalkCastAhead with the enchant's cast ticks); a click target that ends the leg
// has no next click and needs only WalkEnchantEndAhead.
func WalkEnchantAhead(running, legEnd bool) int {
	if legEnd {
		return WalkEnchantEndAhead
	}
	return WalkCastAhead(running, WalkEnchantCastTicks)
}

// WalkEnchantPoll is one walk poll as the enchant decision sees it.
type WalkEnchantPoll struct {
	// SinceClick is how long ago the walk's last movement click went (WalkAlchAfterClick keeps a cast out of the
	// click's tick).
	SinceClick time.Duration
	// Pending is a cast sent and not yet seen to land; TicksSinceLand the client ticks since the last one did (a
	// large value when none has), GapTicks the ticks that must pass (at least WalkEnchantGapTicks).
	Pending        bool
	TicksSinceLand int
	GapTicks       int
	// Ahead is tiles from the account to the click's target; LegEnd that the target ends the leg; Running the
	// account's run toggle.
	Ahead   int
	LegEnd  bool
	Running bool
	// InCombat, Wilderness (level > 0) and Modal (an interface is open) each stop a cast; HP and MaxHP leave a
	// low account free to eat (a cast's p_delay refuses the eat).
	InCombat   bool
	Wilderness int
	Modal      bool
	HP, MaxHP  int
	// NoFood is the pack holding nothing to eat: the hitpoints gate stands down (StallStandAside).
	NoFood bool
	// Pieces is the pieces in the pack still to enchant.
	Pieces int
}

// ShouldWalkEnchant decides one poll. Cheap: every gate is a comparison on what the poll read.
func ShouldWalkEnchant(p WalkEnchantPoll) WalkAlchDecision {
	no := func(why string) WalkAlchDecision { return WalkAlchDecision{Why: why} }
	gap := p.GapTicks
	if gap < WalkEnchantGapTicks {
		gap = WalkEnchantGapTicks
	}
	switch {
	case p.Pieces < 1:
		return no("nothing left to enchant")
	case p.InCombat:
		return no("in combat")
	case StallStandAside(p.HP, p.MaxHP, p.NoFood):
		return no("hitpoints low: the account must be free to eat")
	case p.Wilderness > 0:
		return no("in the Wilderness")
	case p.Modal:
		return no("an interface is open")
	case p.Pending:
		return no("the last cast has not shown yet")
	case p.SinceClick < WalkAlchAfterClick:
		return no("a movement click went out this tick")
	case p.TicksSinceLand < gap:
		return no("the last cast's delay is not over")
	case p.Ahead < WalkEnchantAhead(p.Running, p.LegEnd):
		return no("the walk will click again inside the cast's delay")
	}
	return WalkAlchDecision{Cast: true}
}
