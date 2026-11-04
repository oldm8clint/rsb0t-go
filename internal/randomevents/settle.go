package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// Leaving combat before a logout (deathsweep-1017, 17 Oct 2026).
//
// A graceful stop of the fleet closes every account's socket, and a closed socket does not take an account out of a fight.
// The server holds a logout while the player is in combat (content's combat_preventlogout: 16 ticks, 9.6 seconds, after the
// last combat action) and forces one only after a full minute without a packet from the client (World.TIMEOUT_NO_RESPONSE,
// 100 ticks). For that minute the account stands where it was, in the fight, with no script, no Guardian and nobody to eat:
// a minute of an ogre's or a giant's hits. Deaths while disconnected cannot be logged, so what shows is the account that logs
// back in hurt, and the one that is dead before it can act: 14 Oct 11:03, an account stopped in the North Ardougne Moss
// giants' pen logged back in at 11/54; 15 Oct 00:49:07, an account stopped at 35 hitpoints in the Yanille ogres (a Lobster
// every 25 seconds while it fought) logged back in at 00:56:28 at 5/53, with 15 food, and was dead two seconds later.
//
// So a stop that finds an account in combat gets it out of it first, by the means the Guardian already has: a teleport
// when hurt enough to need one and able to pay for it (lastResortExit), a bite when under 60% (fleeEatIfLow), and the
// walking flee from whatever is hitting it (fleeFromCombat), until it has been out of combat for longer than the
// server's hold. It is bounded: an account that cannot get out in settleMaxWait is logged out anyway, as before.

const (
	// settleQuietFor is how long an account must have been out of combat before the logout: the server's 16 ticks
	// (9.6 s) and a margin, so the logout is taken at once instead of held.
	settleQuietFor = 12 * time.Second

	// settleFleeGap spaces the walking flees, so a bite and a cast are not starved by a flee that blocks for up to 20 s.
	settleFleeGap = 3 * time.Second
)

// The settle's clocks; variables only so a test can shorten them.
var (
	// settleMaxWait bounds the whole settle. The stop script waits two minutes for the logouts.
	settleMaxWait = 45 * time.Second
	settlePoll    = 500 * time.Millisecond
	settleQuiet   = settleQuietFor
)

// stillFighting is the settle's "not out of it yet": hit recently, or a recorded attacker still pursuing.
func stillFighting(s actions.Session) bool {
	return s.State().InCombat() || underAttack(s)
}

// SettleBeforeLogout takes an account that is in combat out of it before the caller logs it out, and reports whether it
// was out of combat for settleQuiet when it returned (always true for an account that was not in combat to begin with,
// at once). Call it with the account's script stopped and its Guardian still running: the Guardian's last-resort teleport
// and its eating are this function's means, and its own flee is kept off the account's back by the action lock for as
// long as this function acts. log may be nil.
func SettleBeforeLogout(s actions.Session, pack *pathfind.Pack, log func(string)) bool {
	if s == nil || s.State() == nil || !stillFighting(s) {
		return true
	}
	say := func(format string, a ...any) {
		if log != nil {
			log(fmt.Sprintf(format, a...))
		}
	}
	// A ScarabSwarm account's combat is "expected" for the whole of its run (applySafeRef), which makes the walking flee
	// stand down at once ("combat is its ordinary state", walkerThreatened). The script has stopped: nothing is expected.
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g != nil {
		g.CombatExpected = false
	}
	hp, _ := s.State().HP()
	max, _ := s.State().MaxHP()
	say("stop: in combat at %d/%d hitpoints (%s): eating, fleeing or casting out of it before the logout, for up to %s", hp, max, attackerDiag(s.State()), settleMaxWait)
	deadline := time.Now().Add(settleMaxWait)
	var quietSince, lastEat, lastFlee time.Time
	for time.Now().Before(deadline) {
		if !stillFighting(s) {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= settleQuiet {
				say("stop: out of combat for %s — logging out", settleQuiet)
				return true
			}
			time.Sleep(settlePoll)
			continue
		}
		quietSince = time.Time{}
		unlock, ok := TryLock(s)
		if !ok {
			time.Sleep(settlePoll) // the Guardian is acting on the account (a flee, an event): let it
			continue
		}
		acted := settleStep(s, pack, &lastEat, &lastFlee, log)
		unlock()
		if !acted {
			time.Sleep(settlePoll)
		}
	}
	hp, _ = s.State().HP()
	say("stop: still in combat at %d/%d hitpoints after %s (%s): logging out anyway", hp, max, settleMaxWait, attackerDiag(s.State()))
	return false
}

// settleStep is one action toward leaving combat, in the order of what is most urgent, and reports whether it did one.
func settleStep(s actions.Session, pack *pathfind.Pack, lastEat, lastFlee *time.Time, log func(string)) bool {
	if lastResortExit(s, "stop: in combat", log) {
		return true
	}
	if time.Since(*lastEat) >= fleeHoldGroundEvery && fleeEatIfLow(s, log) {
		*lastEat = time.Now()
		return true
	}
	if time.Since(*lastFlee) >= settleFleeGap {
		*lastFlee = time.Now()
		fleeFromCombat(s, pack, baseFleeDist, log)
		return true
	}
	return false
}
