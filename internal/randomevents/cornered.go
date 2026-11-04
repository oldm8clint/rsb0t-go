package randomevents

// A cornered account fights back (scarabe2e, 2026-09-26).
//
// LIVE DEATH, 2026-09-26 21:44:36-21:45:04 on c1ff7c2f (acct125, ScarabSwarm, Thieving 40s): a
// thieving random event's Watchman (content macro_watchman2, type 432; macro events/scripts/
// thieving/macro_event_watchman.rs2) spawned beside the account inside the East Ardougne guard
// house at 2668,3320, a room whose two doors the live overlay still reads as shut. HandleEvade found
// no escape on the live pack, and its evadeThroughDoor found no door either, so it did what its
// no-escape branch does: sleepWatchingCombat -- which returns at once, because the account was
// being hit -- and "no safe reachable local escape". The guardian re-ran it every half second for
// 28 seconds (56 "handler error" lines) while the Watchman hit the account, and the script, held
// by Busy(), never ate the cakes in its pack. It died standing still.
//
// So when there is nowhere to go AND the threat is attacking, the account fights back (every
// hostile event monster's op2 is Attack in content's antimacro.npc) and the guardian steps aside
// for corneredFightWindow: the event is deferred (retryUntil), which releases Busy() so the
// script's own loop -- its eat step above all -- runs; the guardian's generic "in combat with no
// tracked event" flee and EmergencyFlee's in-combat flee both stand down for the window (they
// have nowhere to go either and would only take the account back). When the window ends the
// ordinary evade runs again: a door may be open by then, or the account fights on for another
// window. A threat that is not attacking (only in view) keeps the old behaviour exactly.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
)

// corneredFightWindow is how long a cornered account fights back with its script free to eat
// before the evade handler looks for a way out again. Long enough to be several of its own attacks
// and a couple of bites, short enough that an opened door is found soon.
const corneredFightWindow = 15 * time.Second

// corneredAttackOp is the Attack option of every hostile event monster (content antimacro.npc
// sets op2=Attack on all 31 of them).
const corneredAttackOp = 2

// threatAttacking reports whether ev's NPC is attacking this account now: in view, within two
// tiles, and either facing the account or the account taking hits. Pure over the state.
func threatAttacking(state *gamestate.State, ev *Event) bool {
	if state == nil || ev == nil || ev.NpcIndex < 0 {
		return false
	}
	x, z, _, ok := state.Tile()
	if !ok {
		return false
	}
	self := state.Self()
	for _, n := range state.NPCs() {
		if n.Index != ev.NpcIndex {
			continue
		}
		dx, dz := n.X-x, n.Z-z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		if max(dx, dz) > 2 {
			return false
		}
		return (self >= 0 && n.TargetsPlayer(self)) || state.InCombat()
	}
	return false
}

// corneredCanFight is whether attacking ev's NPC is worth anything. A Shade is level 120 for an owner of combat
// 61 to 90 and a Strange plant has defence 99 and 200 hitpoints: the only thing attacking either does is keep
// the account facing it, in melee, with its own script (which would eat, fight the herd it came to fight or
// break off for the bank) held out of the way. Every other event monster is an opponent an account can hurt.
func corneredCanFight(npcType int) bool {
	return npcType != angryPlantType && !(npcType >= 425 && npcType <= 430)
}

// corneredFed reports whether the account has anything to eat. A session with no Guardian, or no name resolver to
// read its pack with, reads as fed: nothing is known, and the cornered fight-back keeps the behaviour it had.
func corneredFed(s actions.Session) bool {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil || s.State() == nil {
		return true
	}
	if _, ok := currentResolver().(holdGroundResolver); !ok {
		return true
	}
	_, has := g.strandFood()
	return has
}

// fightBackCornered attacks ev's NPC where that is worth doing (corneredCanFight) and the account has food to
// carry the fight, and hands the account to its script for corneredFightWindow (see the file comment). With NOTHING
// to eat it does not attack: the account cannot win against an event monster it has not been able to leave (a Swarm
// has more hitpoints than anything a ScarabSwarm account kills in a useful time), and a fight with nothing to heal
// with is only a death on a timer. Live 17 Oct 2026, the Wizards' Tower top room: forty "fighting back" passes in
// ten minutes at food 0, 54 hitpoints to 0. It reports whether it handed the account over.
func fightBackCornered(s actions.Session, ev *Event, log func(string)) bool {
	fed := corneredFed(s)
	fight := corneredCanFight(ev.NpcType) && fed
	if fight {
		if err := actions.OpNpc(s, corneredAttackOp, ev.NpcIndex); err != nil {
			if log != nil {
				log("cornered: attacking the threat failed: " + err.Error())
			}
			return false
		}
	}
	markCornered(s, ev, time.Now().Add(corneredFightWindow))
	if log != nil {
		x, z, level, _ := s.State().Tile()
		if fight {
			log(fmt.Sprintf("cornered at %d,%d,%d with no escape and npc %d type %d attacking: fighting back, and the script may eat, for %s",
				x, z, level, ev.NpcIndex, ev.NpcType, corneredFightWindow))
		} else if !fed && corneredCanFight(ev.NpcType) {
			log(fmt.Sprintf("cornered at %d,%d,%d with no escape and npc %d type %d attacking, and nothing to eat: not attacking it (a fight the account cannot win only keeps it in melee), so the script may break off for %s",
				x, z, level, ev.NpcIndex, ev.NpcType, corneredFightWindow))
		} else {
			log(fmt.Sprintf("cornered at %d,%d,%d with no escape and npc %d type %d attacking: not worth attacking, so the script may eat or break off for %s",
				x, z, level, ev.NpcIndex, ev.NpcType, corneredFightWindow))
		}
	}
	return true
}

// corneredStandDown is the end of an evade that could not move the account: the walker found nowhere to go
// (fleeWalking's stuck), whatever the plan before it said. When the account is hurt enough to need one it
// casts the teleport it can pay for (lastResortExit); otherwise it takes the cornered path, which hands the
// account to its script for corneredFightWindow, fighting back first where that is worth anything. Reports
// whether it did either; false leaves the caller's own wait in place.
func corneredStandDown(s actions.Session, ev *Event, log func(string)) bool {
	return corneredStandDownBecause(s, ev, "evade: stuck", log)
}

// corneredStandDownBecause is corneredStandDown with its own first words for the teleport's log line: HandleEvade calls
// it when its plan found nowhere to go ("evade: cornered"), the walker when it was stuck ("evade: stuck").
func corneredStandDownBecause(s actions.Session, ev *Event, why string, log func(string)) bool {
	if lastResortExit(s, why, log) {
		return true
	}
	if fightBackCornered(s, ev, log) {
		return true
	}
	return false
}

// fleeCornered is what a flee that has nowhere to walk does about being hit: the teleport it can pay for when
// hurt enough to need one (lastResortExit), else eat or fight back (fleeHoldGround).
func fleeCornered(s actions.Session, log func(string)) {
	if lastResortExit(s, "combat flee: nowhere to walk", log) {
		return
	}
	fleeHoldGround(s, log)
}

// markCornered records the fight-back window on session's Guardian and defers ev until it ends.
func markCornered(session actions.Session, ev *Event, until time.Time) {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return
	}
	g.mu.Lock()
	g.corneredUntil = until
	sig := eventSignature(ev)
	g.retryUntil[sig] = until
	g.trackedEvents[sig] = *ev
	g.mu.Unlock()
}

// cornered reports whether this Guardian is inside a fight-back window.
func (g *Guardian) cornered() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Now().Before(g.corneredUntil)
}
