package questplan

import (
	"fmt"
	"time"
)

// The pure half of a quest fight in progress: when the account must send its attack again, and
// when the fight is being lost and the account must walk away from it.
//
// WHY THIS EXISTS (live, 5 Oct 2026, Priest in Peril's "pip-monk"). Unforgivable fought a Monk of
// Zamorak for 11 minutes and ate 15 sharks and lobsters at 7-10 hitpoints without once killing
// it, then died; acct12, acct162, spare27 and acct50 did the same. The same step, run at
// full health, killed the same monk in about a minute with no food eaten (spare28 32/32, spare8
// 33/33, acct22 36/36, shrimp, and Unforgivable's own second try after its death). Every failed
// fight started at 19-29 hitpoints, so the account had to eat mid-fight; every success needed
// no bite.
//
// The engine explains it. OpHeldHandler.handle runs player.clearPendingAction() for every item
// op that is not on the main modal, and that is clearInteraction(): eating throws the attack away.
// The account answers a hit only if auto-retaliate is on, and ScarabSwarm's cow pen leaves it OFF
// (scarabrunner.RetreatFromCows turns it off on its way out, and the quest ladder starts from
// there; no live account's varp was read, so that half is the code's account, not a measurement).
// execFight attacked ONCE, so after its first bite the account stood in front of the monk being
// hit (about 0.4 hitpoints a second from a level-30 monk: 1 in 4 attacks is a zap of up to 8) and
// ate until the food ran out. The monk lost one hitpoint a minute to regeneration.
//
// So a fight loop has two jobs the old one did not do: send the attack again when it has stopped
// (DecideFight's FightReengage), and stop fighting what it is not beating (FightRetreat) instead of
// feeding it every piece of food the account carries.
const (
	// FightStallEats is how many bites the account may eat without the target's health bar
	// falling before the fight is called lost. A fight the account is winning lowers the bar
	// every few seconds; six bites with no change is a stalemate it is paying for.
	FightStallEats = 6

	// FightClickMin is the shortest gap between two attack clicks: a click closes any open modal
	// and asks the server to path again, so it is never sent faster than this.
	FightClickMin = 1500 * time.Millisecond
	// FightClickIdle is how long the target may go unhit (no hitmark on it inside the client's
	// combat window) before the attack is sent again.
	FightClickIdle = 3 * time.Second
	// FightClickAny is the longest the attack goes unsent even while hits are landing: a
	// cheap guard against an interaction the server dropped between two hitmarks.
	FightClickAny = 8 * time.Second
)

// FightReading is what one poll of a fight sees.
type FightReading struct {
	// HP and MaxHP are the account's hitpoints, EatAt the sustain's eat line (0 never eats).
	HP, MaxHP, EatAt int
	// Food is how many pieces of the sustain's food the pack still holds.
	Food int
	// EatsSinceProgress is how many bites the account has eaten since the target's health bar last
	// fell (or since the fight began, when it never has).
	EatsSinceProgress int
	// SinceClick is how long ago the attack was last sent.
	SinceClick time.Duration
	// AteSinceClick is whether a bite was eaten after that click: eating clears the interaction.
	AteSinceClick bool
	// TargetInCombat is whether a hitmark landed on the target inside the client's combat window.
	TargetInCombat bool
	// Race is the fight's pace so far (fightrace.go); the zero value, with no time elapsed, judges nothing.
	Race RaceReading
	// Scripted is whether the target is a foe the content scripts the account to lose to (ScriptedLoss): no
	// retreat rule is read for it, only the attack is sent again when it has stopped.
	Scripted bool
}

// FightAction is what a poll decides.
type FightAction int

const (
	// FightOn keeps fighting.
	FightOn FightAction = iota
	// FightReengage sends the attack again.
	FightReengage
	// FightRetreat gives the fight up and walks away.
	FightRetreat
)

// FightVerdict is DecideFight's answer: the action and, for a retreat, why in one line.
type FightVerdict struct {
	Action FightAction
	Why    string
}

// DecideFight judges one poll. Retreat comes first: a fight that is being lost is not made better
// by clicking again.
//
//   - Down to the last food (none or one piece) with hitpoints at or under the eat line: the next
//     bite buys 30 seconds and the one after is not there. The account walks away with its last
//     piece, which the eat loop uses on the way.
//   - FightStallEats bites eaten with the target's health bar never falling: it is not being hurt.
//   - The race (RaceLost): the pace so far says the rest of the fight costs more than the account's hitpoints
//     and food hold, or the target's bar has not moved while the account has lost a third of what it can
//     spend.
//
// None of the three applies to a scripted loss (FightReading.Scripted): there the hitpoints reaching 0 is the
// pass, and the only thing the poll decides is when to send the attack again.
//
// Otherwise the attack goes out again when eating has cleared it, when the target has gone unhit
// for FightClickIdle, or at the latest every FightClickAny; never faster than FightClickMin.
func DecideFight(r FightReading) FightVerdict {
	if !r.Scripted {
		if r.EatAt > 0 && r.MaxHP > 0 && r.HP <= r.EatAt && r.Food <= 1 {
			return FightVerdict{FightRetreat, fmt.Sprintf("hitpoints %d/%d are at the eat line (%d) with %d food left", r.HP, r.MaxHP, r.EatAt, r.Food)}
		}
		if r.EatsSinceProgress >= FightStallEats {
			return FightVerdict{FightRetreat, fmt.Sprintf("ate %d times and the target's health has not gone down", r.EatsSinceProgress)}
		}
		if lost, why := RaceLost(r.Race); lost {
			return FightVerdict{FightRetreat, why}
		}
	}
	if r.SinceClick < FightClickMin {
		return FightVerdict{Action: FightOn}
	}
	switch {
	case r.AteSinceClick:
		return FightVerdict{Action: FightReengage}
	case !r.TargetInCombat && r.SinceClick >= FightClickIdle:
		return FightVerdict{Action: FightReengage}
	case r.SinceClick >= FightClickAny:
		return FightVerdict{Action: FightReengage}
	}
	return FightVerdict{Action: FightOn}
}

// FightHealth is a target's health bar as the last hitmark on it drew it: Total is 0 until one has
// arrived.
type FightHealth struct{ Health, Total int }

// HealthFell reports whether the bar went down between two readings: the first hit that took
// something off a bar read for the first time counts, and a bar that went up (a respawned
// monster, a new target) does not.
func HealthFell(prev, cur FightHealth) bool {
	if cur.Total <= 0 {
		return false
	}
	if prev.Total <= 0 {
		return cur.Health < cur.Total
	}
	return cur.Health < prev.Health
}

// NearDeathsToPark is how many lost fights (retreats) inside NearDeathWindow park a quest for a
// day, as MaxDeaths does for deaths: a quest that keeps losing parks instead of killing the account.
const NearDeathsToPark = 2

// NearDeathWindow is the span NearDeathsToPark is counted over.
const NearDeathWindow = 24 * time.Hour
