package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// pursuitRadius is how close a recorded attacker still facing this account must be to count
// as pursuing it after InCombat's hit window has lapsed. A chaser between its attack ticks
// stands a tile or two away; one that has turned back to wander is soon further than this.
const pursuitRadius = 10

// canAttack reports whether npc is something that can fight: an event monster, or an NPC
// whose type offers "Attack". Unknown when no name resolver is registered, in which case
// every NPC counts, the behaviour this package had before the check existed.
//
// Live 2026-09-19 (spare 21 20:17, and the same shape for spare102 22:12): the combat
// record named "npc 6144 type 313" — the Guild fishing spot the account was netting, which
// it faces while fishing — as an attacker beside the real one (type 411, a Swarm). A fishing
// spot never leaves view, so RecentlyAttackedNear's early lift could not fire, and the flee
// log could not say what was actually hitting the account.
func canAttack(npc gamestate.NPC) bool {
	if hostileEventNpcIDs[npc.Type] {
		return true
	}
	res := currentResolver()
	if res == nil {
		return true
	}
	info, ok := res.Npc(npc.Type)
	if !ok {
		return true
	}
	return cachecfg.OpIndex(info.Ops, "Attack") != 0
}

// currentAttacker names the NPC most likely to be hitting this account right now: among the
// NPCs that can fight and are either facing this account or the one auto retaliate turned it
// toward, an event monster first, then one facing the account, then the nearest.
func currentAttacker(state *gamestate.State) (gamestate.NPC, bool) {
	if state == nil {
		return gamestate.NPC{}, false
	}
	x, z, _, ok := state.Tile()
	if !ok {
		return gamestate.NPC{}, false
	}
	self, face := state.Self(), state.SelfFaceEntity()
	var best gamestate.NPC
	bestRank, found := 0, false
	for _, n := range state.NPCs() {
		targets := self >= 0 && n.TargetsPlayer(self)
		if !targets && n.Index != face {
			continue
		}
		dist := chebyshev(n.X, n.Z, x, z)
		if dist > hostileEngageDist || !canAttack(n) {
			continue
		}
		// Lower is better: event monster, then facing us, then distance breaks ties.
		rank := dist
		if !targets {
			rank += 100
		}
		if !hostileEventNpcIDs[n.Type] {
			rank += 1000
		}
		if !found || rank < bestRank {
			best, bestRank, found = n, rank, true
		}
	}
	return best, found
}

// describeNPC is one NPC as a log fragment: index, type, cache name, tile and distance.
func describeNPC(npc gamestate.NPC, x, z int) string {
	name := ""
	if res := currentResolver(); res != nil {
		if info, ok := res.Npc(npc.Type); ok && info.Name != "" {
			name = fmt.Sprintf(" %q", info.Name)
		}
	}
	return fmt.Sprintf("npc %d type %d%s at %d,%d dist %d", npc.Index, npc.Type, name, npc.X, npc.Z, chebyshev(npc.X, npc.Z, x, z))
}

// attackerDiag is currentAttacker for a log line.
func attackerDiag(state *gamestate.State) string {
	x, z, _, _ := state.Tile()
	if npc, ok := currentAttacker(state); ok {
		return "attacker " + describeNPC(npc, x, z)
	}
	return "attacker unidentified"
}

// pursuingAttacker returns an NPC recorded as this account's attacker in its most recent
// combat flee (see noteCombatFleeFrom) that is still facing the account from within
// pursuitRadius. InCombat is only "hit in the last few ticks", so between two attacks by a
// chaser it reads false; this is the evidence that the fight is not over.
//
// Live 2026-09-19 (herring, Catherby, 23:55-23:57): each combat flee returned as soon as
// InCombat lapsed, the Strange box handler reopened a box on the spot, and the same attacker
// was hitting again within ~15s — five times, until the account died.
func pursuingAttacker(s actions.Session) (gamestate.NPC, bool) {
	v, ok := recentCombat.Load(s)
	if !ok {
		return gamestate.NPC{}, false
	}
	spot := v.(recentCombatSpot)
	if time.Now().After(spot.until) || len(spot.attackers) == 0 {
		return gamestate.NPC{}, false
	}
	state := s.State()
	if state == nil {
		return gamestate.NPC{}, false
	}
	x, z, _, ok := state.Tile()
	self := state.Self()
	if !ok || self < 0 {
		return gamestate.NPC{}, false
	}
	for _, n := range state.NPCs() {
		for _, a := range spot.attackers {
			if n.Index == a.index && n.Type == a.typ && n.TargetsPlayer(self) && chebyshev(n.X, n.Z, x, z) <= pursuitRadius {
				return n, true
			}
		}
	}
	return gamestate.NPC{}, false
}

// underAttack is the combat branch's trigger: hit recently, or a recorded attacker is still
// closing in (pursuingAttacker).
func underAttack(s actions.Session) bool {
	if s.State().InCombat() {
		return true
	}
	_, ok := pursuingAttacker(s)
	return ok
}

// stillThreatened is the walking flee's "not separated yet" test at x,z: hit recently, an
// event monster still threatening the account, or a recorded attacker still pursuing it.
func stillThreatened(s actions.Session, x, z int) bool {
	if s.State().InCombat() || HostileEventNpcNearby(s.State(), x, z, hostileEngageDist) {
		return true
	}
	_, ok := pursuingAttacker(s)
	return ok
}

// walkerThreatened is the walking flee's "not separated yet" test. For an account whose combat is expected (a
// trainer standing in an aggressive herd, a thief among the market guards) being in combat is its ordinary
// state, not a threat: the flee it is running is from an event monster, and it is over when none remains near
// (HostileEventNpcNearby). stillThreatened counts every hit and every recorded attacker, which for such an
// account never lapses, so the walker went on "going farther" for its whole 20 s after the monster was gone and
// walked the trainer out of its stand (a Strange plant that lost its target at the Ice warrior cave).
func walkerThreatened(s actions.Session, x, z int) bool {
	if combatExpectedFor(s) {
		return HostileEventNpcNearby(s.State(), x, z, hostileEngageDist)
	}
	return stillThreatened(s, x, z)
}
