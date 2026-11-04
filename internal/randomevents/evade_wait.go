package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
)

// What HandleEvade does while an event monster is STILL on an account whose combat is expected, after the
// walk that was meant to shake it.
//
// WHAT THE WAIT USED TO DO. HandleEvade ends every walk with a wait of up to 45 s for the monster to lose the
// account, holding the action lock the whole time. For an account whose combat is not expected, being hit
// during the wait meant the walk had not worked and the generic flee ran. For one whose combat IS expected (every
// ScarabSwarm account, in every phase, and every fighter) that flee is the opposite of what is wanted, so it
// was skipped, and nothing took its place: if the leash walk had not shaken the monster, the handler idled
// for the full 45 s with a Shade or a River troll on the account (8 to 12 a hit against a pool of 50), while the
// script that would eat, break off for the bank or cast a teleport (survivalTick, the trainer's food step) sat
// behind the guardian's lock and could do none of it.
//
// WHAT IT DOES NOW, every half second while the account is in combat and the monster still threatens it:
//
//   - A teleport when hurt enough to need one (lastResortExit: under 30% hitpoints with food, under 50%
//     without), then nothing more is asked of the handler.
//   - A bite of food on the walking flee's own rule (fleeEatIfLow: under 60%), because the handler holds the
//     lock and so the account's eating is the handler's to do.
//   - Below carryOnFromHP (the cut the step-aside rule uses) it stands down at once: the script has the lock
//     back, fights on or banks as it sees fit, and the event is retried when the cornered window ends.
//   - After evadeHeldPatience (the leash is lost beyond maxrange+1 of the spawn, so a walk that worked shows
//     in seconds) it hands the lock back with the event still pending: the guardian's next attempt (attempt+1)
//     plans again from where the account now stands, clear of the monster where it now is, and aims four tiles
//     farther past the leash (fleeWalkingFrom passes the attempt to the walker, leashBreakFor adds the tiles).
//     From the maxAttempts'th attempt on it stands down instead, so a monster that cannot be shaken ends with
//     the account in its script's hands, not in a loop of walks.

// evadeHeldPatience is how long the monster may keep the account after the walk before the handler hands
// back. A variable only so a test can shorten it.
var evadeHeldPatience = 10 * time.Second

// heldAction is what the wait does now.
type heldAction int

const (
	// heldWait keeps waiting.
	heldWait heldAction = iota
	// heldHandBack ends the handler with the event still pending, so the guardian's next attempt plans again.
	heldHandBack
	// heldStandDown ends it by handing the account to its script (corneredStandDown).
	heldStandDown
)

// evadeHeldAction is the decision, pure. held is how long the monster has been on the account since the walk;
// attempt is the guardian's count for the event. Unknown hitpoints stand down, as combatReactionFor does: the
// safe answer is to let the script act.
func evadeHeldAction(hp, max int, hpKnown bool, held time.Duration, attempt int) heldAction {
	if !hpKnown || max <= 0 || float64(hp) < carryOnFromHP*float64(max) {
		return heldStandDown
	}
	if held < evadeHeldPatience {
		return heldWait
	}
	if attempt >= maxAttempts {
		return heldStandDown
	}
	return heldHandBack
}

// evadeHeld is one pass of the wait for an account that is in combat with the event monster still on it, and
// reports whether the handler must return now. since is when the monster was first found still on the account;
// lastHold throttles the bite as fleeWalking's leg loop does.
func evadeHeld(s actions.Session, ev *Event, attempt int, since time.Time, lastHold *time.Time, log func(string)) bool {
	if lastResortExit(s, "evade: still hit after the walk", log) {
		return true
	}
	if time.Since(*lastHold) >= fleeHoldGroundEvery && fleeEatIfLow(s, log) {
		*lastHold = time.Now()
		// The hitpoints on file are from before the bite: judge the next pass, not this one.
		return false
	}
	hp, okHP := s.State().HP()
	max, okMax := s.State().MaxHP()
	held := time.Since(since)
	switch evadeHeldAction(hp, max, okHP && okMax, held, attempt) {
	case heldHandBack:
		if log != nil {
			log(fmt.Sprintf("evade: %s is still on the account %s after the walk at %d/%d hitpoints — handing the lock back, and attempt %d walks again from here, farther out",
				ev.Name, held.Round(time.Second), hp, max, attempt+1))
		}
		return true
	case heldStandDown:
		if log != nil {
			log(fmt.Sprintf("evade: %s is still on the account after the walk (%d/%d hitpoints, %s, attempt %d) — standing down for the script",
				ev.Name, hp, max, held.Round(time.Second), attempt))
		}
		if !corneredStandDown(s, ev, log) {
			// The stand-down's own attack failed: the lock is still given back, never held for nothing.
			deferEvent(s, ev, combatEventDefer)
		}
		return true
	}
	return false
}
