package fleetplan

import (
	"math"
)

// THREE FAILURES, NOT TWO (deathsweep-1017, 17 Oct 2026). The line below was two failures plus one, and three accounts died
// above it on 14 Oct, 20:09:24, 20:13:19 and 20:13:21, two Paladin thieves at 53 and 54 maximum hitpoints and a Hero at 51: each
// had a bite in the air (the Hero's "took 3.2s to show", the second Paladin's "took 3.8s to show", the first one's three bites
// "timed out waiting to register" with one landing fourteen seconds late) when a failed pickpocket's 3 or 4 landed on hitpoints
// of 3 and 4. All three came inside fleet-wide windows when 6 to 15 accounts a minute logged bite timeouts against a baseline of
// one to four: with the route that slow, the hitpoints the account reads and the failure that is already on its way are both a
// few seconds older than its decision. So the line covers THREE failures in a row, and never sits below 40% of the maximum,
// where a Hero's 4 needs five failures in the air to kill. Eating there costs no more food than eating at 7: the
// damage taken is the damage healed whatever the line.
const (
	// ThieveFailuresCovered is how many failed steals in a row the eating line survives.
	ThieveFailuresCovered = 3
	// ThieveEatFraction is the share of maximum hitpoints the eating line never sits under.
	ThieveEatFraction = 0.40
)

// ThieveEatBelow is the thieving eat threshold: HP below it, with food carried, eats before the
// next pickpocket. It is the setting (stage3EatBelowHp, default 6) or, when that is lower, one
// more than ThieveFailuresCovered failures on the rung (3 x stun + 1), so no run of consecutive
// failures can take a fed account from above the threshold to zero -- a Hero's 4 needs 13, a Knight's
// or Paladin's 3 needs 10, a Guard's 2 needs 7. eatBelow 0 keeps "never eat".
func ThieveEatBelow(eatBelow, stun int) int {
	if eatBelow <= 0 {
		return 0
	}
	if floor := ThieveFailuresCovered*stun + 1; floor > eatBelow {
		return floor
	}
	return eatBelow
}

// ThieveEatBelowHP is ThieveEatBelow for this snapshot, raised to ThieveEatFraction of MaxHP and never above MaxHP: an account
// whose whole health is under the threshold eats only once it is actually hurt, not every loop at full health.
func (s Stage3Snapshot) ThieveEatBelowHP() int {
	eat := ThieveEatBelow(s.EatBelowHP, s.ThieveStun)
	if eat > 0 && s.MaxHP > 0 {
		if share := int(math.Ceil(ThieveEatFraction * float64(s.MaxHP))); share > eat {
			eat = share
		}
	}
	if s.MaxHP > 0 && eat > s.MaxHP {
		eat = s.MaxHP
	}
	// A thief that has lagged lately also keeps what a steal can raise for as long as an episode can hide it: the Watchman event and
	// the rung's failure, and whatever already faces it (lageat.go).
	return s.lagEatBelow(eat, SumHits(ThievingHits(s.CombatLevel, s.ThieveStun)...)+s.LagFacing)
}

// MuleAskFoodHungry keys a hungry account's food ask: its own timer, separate from the regular
// MuleAskFood one, so the regular 25-minute spacing (and its restart slot) never holds up an
// account that has nothing at all to eat.
const MuleAskFoodHungry = "food-hungry"
