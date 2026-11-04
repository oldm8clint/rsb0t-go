package randomevents

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// FLEEING WHAT HITS THE ACCOUNT, ON A SCRIPT'S REQUEST (lagescape-1018, 18 Oct 2026).
//
// kick() flees an account that is being hit with no tracked event ("in combat with no tracked event - fleeing immediately"), and
// EmergencyFlee does the same from a script's wait loop, but both stand down for an account whose combat is expected
// (Guardian.CombatExpected, true for every ScarabSwarm stage: a trainer in a herd, a thief among guards, a magic account at a pen),
// where a hit is an ordinary cost and the stage's own answer to it is a bite. On a server running its bites five seconds late that
// answer fails (the 18 Oct 15:14 death: sixteen bites sent in a minute, none of them ahead of a volley of sixteen), and the stage has
// judged the hit one it cannot out-eat (scarabplan.DecideEscape). FleeAttackers is the flee it then asks for: the same walking flee
// kick() runs, with the flag clear for its length, because walkerThreatened counts a hit as a threat only for an account that does
// not expect one.

// fleeAttackersFlee is the flee FleeAttackers runs; a variable only so a test can say what it did without a pack to plan on.
var fleeAttackersFlee = fleeFromCombat

// clearCombatExpected clears the session's Guardian.CombatExpected and returns the function that puts it back as it was. Nothing for a
// session with no Guardian.
func clearCombatExpected(s actions.Session) (restore func()) {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil {
		return func() {}
	}
	prev := g.CombatExpected
	g.CombatExpected = false
	return func() { g.CombatExpected = prev }
}

// FleeAttackers walks the account away from whatever is hitting it, whatever its script expects, and reports whether it did. It takes
// the guardian's action lock without waiting (a flee the guardian is already running is left alone), does nothing for an account the
// strand ladder is working or one that nothing has hit lately, and runs the walking flee (fleeFromCombat: the ring search away from the
// attacker, with the cornered path's teleport and bite when it finds nowhere to go) for at most its own 20 s.
func FleeAttackers(s actions.Session, pack *pathfind.Pack, log func(string)) bool {
	if s == nil || s.State() == nil || strandHolds(s) {
		return false
	}
	unlock, ok := TryLock(s)
	if !ok {
		return false
	}
	defer unlock()
	defer clearCombatExpected(s)()
	if !underAttack(s) {
		return false
	}
	fleeAttackersFlee(s, pack, baseFleeDist, log)
	return true
}
