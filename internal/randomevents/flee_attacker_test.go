package randomevents

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// Live 2026-09-19 (spare 21 20:17): the combat record named the Guild fishing spot the
// account was netting (type 313) as an attacker beside the real Swarm (type 411). A fishing
// spot never leaves view, so the early lift of the spot block could never fire.
func TestIdentifyAttackersSkipsAnNPCThatCannotFight(t *testing.T) {
	withResolver(t, fakeResolver{
		npcNames: map[int]string{313: "Fishing spot", 1234: "Guard"},
		npcOps:   map[int][5]string{313: {"Net", "Bait"}, 1234: {"Talk-to", "Attack"}},
	})
	s := combatSession()
	addNPCFacing(s.state, 60, 313, 1, 0, playerFaceBase+testSelfSlot)
	if got := identifyAttackers(s.state); len(got) != 0 {
		t.Fatalf("a fishing spot was identified as an attacker: %+v", got)
	}
	addNPCFacing(s.state, 61, 1234, 1, 0, playerFaceBase+testSelfSlot)
	if got := identifyAttackers(s.state); len(got) != 1 || got[0].typ != 1234 {
		t.Fatalf("an NPC with Attack facing the player was not identified: %+v", got)
	}
	addNPCFacing(s.state, 62, 411, 1, 0, playerFaceBase+testSelfSlot)
	if got := identifyAttackers(s.state); len(got) != 1 || got[0].typ != 411 {
		t.Fatalf("an event monster was not identified whatever its ops: %+v", got)
	}
}

// Without a resolver nothing is known about ops, and every facing NPC still counts, exactly
// as before the filter existed.
func TestIdentifyAttackersWithoutResolverKeepsEveryFacingNPC(t *testing.T) {
	withResolver(t, nil)
	s := combatSession()
	addNPCFacing(s.state, 60, 313, 1, 0, playerFaceBase+testSelfSlot)
	if got := identifyAttackers(s.state); len(got) != 1 {
		t.Fatalf("with no resolver a facing NPC must still count: %+v", got)
	}
}

// Live 2026-09-19 (herring, 23:55-23:57): each flee ended the moment InCombat lapsed and the
// Strange box handler reopened a box beside the same attacker. A recorded attacker still
// facing the account from close by keeps the fight open.
func TestRecordedAttackerStillFacingKeepsTheFightOpen(t *testing.T) {
	withResolver(t, nil)
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 2, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)
	t.Cleanup(func() { recentCombat.Delete(s) })
	if s.state.InCombat() {
		t.Fatal("fixture: no hitmark was sent, InCombat must be false")
	}
	if npc, ok := pursuingAttacker(s); !ok || npc.Index != 77 {
		t.Fatalf("a recorded attacker two tiles away and facing the account is not pursuing: %+v %v", npc, ok)
	}
	if !underAttack(s) || !stillThreatened(s, 3200, 3200) {
		t.Fatal("an attacker still closing in between hits must keep the account under attack")
	}

	// It turns away (faces nobody): the fight is over.
	addNPCFacing(s.state, 77, 1234, 2, 0, -1)
	if _, ok := pursuingAttacker(s); ok {
		t.Fatal("an attacker no longer facing the account still counts as pursuing")
	}
	if underAttack(s) {
		t.Fatal("underAttack stayed true with no hit and no pursuer")
	}
}

// A pursuer only counts within pursuitRadius: one that has fallen far behind is not a reason
// to keep running.
func TestRecordedAttackerFarAwayIsNotPursuing(t *testing.T) {
	withResolver(t, nil)
	s := combatSession()
	addNPCFacing(s.state, 77, 1234, 1, 0, playerFaceBase+testSelfSlot)
	noteCombatFlee(s, 3200, 3200, 0)
	t.Cleanup(func() { recentCombat.Delete(s) })
	addNPCFacing(s.state, 77, 1234, pursuitRadius+2, 0, playerFaceBase+testSelfSlot)
	if _, ok := pursuingAttacker(s); ok {
		t.Fatalf("an attacker %d tiles away counted as pursuing", pursuitRadius+2)
	}
}

// An unrelated NPC facing the account that never took part in the recorded fight is not a
// pursuer: only recorded attackers are.
func TestUnrecordedFacingNPCIsNotAPursuer(t *testing.T) {
	withResolver(t, nil)
	s := combatSession()
	addNPCFacing(s.state, 90, 555, 1, 0, playerFaceBase+testSelfSlot)
	if _, ok := pursuingAttacker(s); ok {
		t.Fatal("no combat was recorded, yet an NPC counted as pursuing")
	}
}

func TestCurrentAttackerPrefersTheEventMonster(t *testing.T) {
	withResolver(t, fakeResolver{
		npcNames: map[int]string{411: "Swarm"},
		npcOps:   map[int][5]string{411: {"", "Attack"}},
	})
	s := combatSession()
	addNPCFacing(s.state, 8345, 411, 1, 0, playerFaceBase+testSelfSlot)
	npc, ok := currentAttacker(s.state)
	if !ok || npc.Index != 8345 {
		t.Fatalf("currentAttacker = %+v %v, want the Swarm", npc, ok)
	}
	if got, want := attackerDiag(s.state), `attacker npc 8345 type 411 "Swarm" at 3201,3200 dist 1`; got != want {
		t.Fatalf("attackerDiag = %q, want %q", got, want)
	}
}

func TestWithoutBadDestsRefusesRoutesNearAFailedDestination(t *testing.T) {
	origin := pathfind.NavPoint{X: 100, Z: 100}
	bad := []pathfind.NavPoint{{X: 120, Z: 100}}
	avoid := withoutBadDests(nil, bad, origin)
	if !avoid(121, 101, 0) || !avoid(117, 100, 0) {
		t.Fatal("tiles within badDestRadius of a failed destination must be avoided")
	}
	if avoid(116, 100, 0) || avoid(80, 100, 0) {
		t.Fatal("tiles away from the failed destination must stay open")
	}
	if avoid(120, 100, 1) {
		t.Fatal("a failed destination on another level must not block this one")
	}
	// The account's own neighbourhood is never excluded, even beside a failed destination.
	near := withoutBadDests(nil, []pathfind.NavPoint{{X: 102, Z: 100}}, origin)
	if near(101, 100, 0) {
		t.Fatal("a tile beside the account itself was excluded")
	}
	if withoutBadDests(nil, nil, origin) != nil {
		t.Fatal("no failed destinations must leave avoid unchanged")
	}
}

// Live 2026-09-19 (spare102, herring, acct98 after deaths): drift recovery could not get
// the action lock for minutes while Strange boxes were solved one after another. A failed
// attempt now asks box/lamp handling to step aside for a short window.
func TestDriftYieldIsPerAccountAndExpires(t *testing.T) {
	a, b := combatSession(), combatSession()
	ga, gb := NewGuardian(a, nil), NewGuardian(b, nil)
	t.Cleanup(func() { ga.unregister(); gb.unregister() })
	if driftRecoveryWaiting(a) {
		t.Fatal("no drift recovery was requested yet")
	}
	requestDriftYield(a)
	if !driftRecoveryWaiting(a) || !ga.driftRecoveryWanted() {
		t.Fatal("a failed drift-recovery lock attempt was not recorded")
	}
	if driftRecoveryWaiting(b) {
		t.Fatal("one account's drift recovery made another yield")
	}
	ga.mu.Lock()
	ga.driftWantedUntil = ga.driftWantedUntil.Add(-2 * driftYieldWindow)
	ga.mu.Unlock()
	if driftRecoveryWaiting(a) {
		t.Fatal("the yield never expires")
	}
	var unregistered overlapSession
	requestDriftYield(&unregistered) // no Guardian: must be a harmless no-op
	if driftRecoveryWaiting(&unregistered) {
		t.Fatal("a session with no Guardian reported a waiting drift recovery")
	}
}
