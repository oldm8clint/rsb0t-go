package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/serverprot"
)

// Live 2026-09-19: combatAvoidRadius (15) covers every fishing spot in Catherby's ~24-tile camp,
// so any fight blanked every spot for the full two-minute window. One 1h46m run logged 8,266
// "fishing paused: all matching spots unsafe" lines across 114 accounts — all "recent combat",
// none whirlpool — with a median longest continuous pause of 140s. The owner saw it as "a lot of
// accounts will stand around".
//
// The rule exists to stop an account walking back into the thing that just hit it (the
// 2026-09-16 flee -> walk back -> get hit -> flee death loop, eatFood off). These tests pin the
// narrowed rule: the block is lifted early ONLY on positive evidence the attacker has left
// view, and is otherwise exactly what it was.
//
// All state below is real decoded NPC_INFO / UPDATE_PID packets through gamestate, not a stubbed
// NPC list. overlapState puts the local player in slot 5 at 3200,3200.

const testSelfSlot = 5

// playerFaceBase mirrors gamestate's unexported constant: an NPC's FaceEntity is a player's slot
// plus 32768 when it faces a player.
const playerFaceBase = 32768

// clearNPCs sends an NPC_INFO that tracks no NPCs, so every previously-visible NPC is dropped —
// the decoded equivalent of the attacker despawning or walking out of view.
func clearNPCs(s *gamestate.State) {
	s.Handle(serverprot.NPC_INFO, bitPack(3,
		bitField{8, 0},      // no previously-tracked npcs
		bitField{14, 16383}, // terminator
	))
}

func combatSession() *overlapSession { return &overlapSession{state: overlapState()} }

// The core safety property must survive: while the attacker is still in view, the spot it
// attacked from stays blocked.
func TestCombatBlockHoldsWhileTheAttackerIsStillInView(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)

	spot := recentCombatFor(t, s)
	if len(spot.attackers) != 1 || spot.attackers[0].index != 77 || spot.attackers[0].typ != 1234 {
		t.Fatalf("attacker not identified from an NPC facing the player: %+v", spot.attackers)
	}
	if !RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("the attacker is still in view but the spot was unblocked — this reopens the 2026-09-16 flee/walk-back death loop")
	}
}

// The fix: once the attacker has gone from view there is nothing left to walk back into, so
// the spot is usable again immediately instead of after the full two minutes.
func TestCombatBlockLiftsOnceTheAttackerHasLeftView(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)
	clearNPCs(s.state)

	if RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("the attacker has left view but the spot is still blocked — accounts will keep standing idle for the full window")
	}
}

// NPC indices are reused by the server. A different NPC type now holding the attacker's old
// index must NOT be mistaken for the attacker still being here.
func TestCombatBlockTreatsAReusedIndexAsTheAttackerGone(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)
	addNPC(s.state, 77, 999, 1, 1) // same index, different type — a new, unrelated NPC

	if RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("an unrelated NPC reusing the attacker's index kept the spot blocked")
	}
}

// When no attacker could be identified, behaviour is exactly what it was before the change:
// the full window applies. This is what keeps the change from ever being LESS safe.
func TestCombatBlockKeepsTheFullWindowWhenNoAttackerWasIdentified(t *testing.T) {
	s := combatSession()
	addNPC(s.state, 40, 555, 3, 3) // in view, but not facing us: not identifiable as the attacker
	noteCombatFlee(s, 3200, 3200, 0)
	if spot := recentCombatFor(t, s); len(spot.attackers) != 0 {
		t.Fatalf("an NPC not facing the player was recorded as an attacker: %+v", spot.attackers)
	}
	clearNPCs(s.state)

	if !RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("no attacker was identified, yet the block was lifted early — the unidentified case must keep the full window")
	}
}

// Radius and level still gate, attacker or not.
func TestCombatBlockStillRespectsRadiusAndLevel(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)

	if RecentlyAttackedNear(s, 3200+combatAvoidRadius+1, 3200, 0, combatAvoidRadius) {
		t.Fatal("a spot outside the radius was blocked")
	}
	if RecentlyAttackedNear(s, 3200, 3200, 1, combatAvoidRadius) {
		t.Fatal("a spot on another level was blocked")
	}
}

// An NPC facing ANOTHER player is someone else's fight, not ours, and must not be recorded.
func TestCombatBlockIgnoresAnNPCFightingSomeoneElse(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot+1)
	noteCombatFlee(s, 3200, 3200, 0)
	if spot := recentCombatFor(t, s); len(spot.attackers) != 0 {
		t.Fatalf("an NPC facing a different player was recorded as this account's attacker: %+v", spot.attackers)
	}
}

func recentCombatFor(t *testing.T, s *overlapSession) recentCombatSpot {
	t.Helper()
	v, ok := recentCombat.Load(s)
	if !ok {
		t.Fatal("noteCombatFlee stored nothing")
	}
	return v.(recentCombatSpot)
}

// Live 2026-09-19 (one 1.6h run on f1abfe3): event monsters are cleared a median 6s after first
// sighting, yet accounts waited a median 120s after "cleared" before the next catch — the full
// combatAvoidWindow. HandleEvade flees BEFORE the monster attacks, so the monster is not yet
// facing the player and identifyAttackers finds nobody. The known threat must be recorded
// directly, and the block must then lift the moment that monster leaves view.
func TestKnownThreatIsRecordedEvenBeforeItAttacks(t *testing.T) {
	s := combatSession()
	addNPC(s.state, 88, 411, 2, 0) // an event monster in view, NOT facing us yet
	noteCombatFleeFrom(s, 3200, 3200, 0, 88, 411)

	spot := recentCombatFor(t, s)
	if len(spot.attackers) != 1 || spot.attackers[0] != (combatAttacker{index: 88, typ: 411}) {
		t.Fatalf("the known pre-attack threat was not recorded: %+v", spot.attackers)
	}
	if !RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("the threat is still in view but the spot was unblocked")
	}
	clearNPCs(s.state) // despawned — the live median is ~6s
	if RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("the event monster is gone but the spot stays blocked — this is the ~120s idle after every event monster")
	}
}

// A second, generic flee moments later must not drop the known threat by replacing the record
// with an empty attacker list — that would silently restore the full two-minute window.
func TestALaterFleeMergesRatherThanDroppingTheKnownThreat(t *testing.T) {
	s := combatSession()
	addNPC(s.state, 88, 411, 2, 0)
	noteCombatFleeFrom(s, 3200, 3200, 0, 88, 411)
	noteCombatFlee(s, 3201, 3200, 0) // facing identifies nobody this time

	spot := recentCombatFor(t, s)
	found := false
	for _, a := range spot.attackers {
		if a == (combatAttacker{index: 88, typ: 411}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("a later flee replaced the record and lost the known threat: %+v", spot.attackers)
	}
	clearNPCs(s.state)
	if RecentlyAttackedNear(s, 3201, 3200, 0, combatAvoidRadius) {
		t.Fatal("the known threat is gone but the merged record kept the spot blocked")
	}
}

// Unknown threat (-1) behaves exactly like plain noteCombatFlee: nothing identified, full window.
func TestUnknownThreatKeepsTheFullWindow(t *testing.T) {
	s := combatSession()
	noteCombatFleeFrom(s, 3200, 3200, 0, -1, -1)
	if spot := recentCombatFor(t, s); len(spot.attackers) != 0 {
		t.Fatalf("an unknown threat produced attackers: %+v", spot.attackers)
	}
	if !RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("with no attacker identified the full window must apply")
	}
}
