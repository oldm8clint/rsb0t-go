package mulereceiverplan

// What a mule does with the time between trades, once the fleet owner's own progression is
// taken into account.
//
// The rule, in the owner's words (2026-09-21): "for mule receiver: at 200m exp in cooking,
// start progressive fishing until 200m exp in fishing but stay at catherby as catherby bank is
// the mule spot and its a long walk from the fishing guild."
//
// Pure on purpose. The decision is three integer comparisons and nothing else, so it is worth
// pinning here rather than leaving inline in internal/bot/mule.go where it would only ever be
// exercised by an account that has actually banked 200,000,000 cooking experience.

// MaxSkillXP is the experience ceiling for one skill. 200,000,000 is the cap, and reaching it
// is the point at which further work in that skill earns nothing at all — which is what makes
// it the right moment to move, rather than a preference.
const MaxSkillXP = 200_000_000

// IdleWork is what the mule should be doing while no trade needs it.
type IdleWork string

const (
	// IdleNone is standing by: either idle work is switched off, or there is nothing left
	// worth doing.
	IdleNone IdleWork = "none"
	// IdleCook is the existing behaviour — cooking the fleet's banked raw fish at the
	// Catherby range.
	IdleCook IdleWork = "cook"
	// IdleFish is fishing AT CATHERBY. Never the Fishing Guild: the mule's post and the bank
	// it trades at are Catherby's, and the Guild is a long walk away, so a mule that wandered
	// there would be absent exactly when a donor arrived. That constraint is the owner's and
	// it is the reason this is a distinct value rather than "run the fisher".
	IdleFish IdleWork = "fish"
)

// DecideIdleWork picks the mule's between-trades work from its own experience totals.
//
// Cooking first while it still pays, then fishing, then nothing. Note what this does NOT do:
// it never interrupts a trade, and it never runs at all unless the caller has already decided
// the stand is quiet — that judgement stays with internal/bot/mule.go, which owns the trade
// state. This answers only "given there is idle time, what is worth doing with it?".
func DecideIdleWork(idleWorkEnabled bool, cookingXP, fishingXP int) IdleWork {
	if !idleWorkEnabled {
		return IdleNone
	}
	if cookingXP < MaxSkillXP {
		return IdleCook
	}
	if fishingXP < MaxSkillXP {
		return IdleFish
	}
	return IdleNone
}
