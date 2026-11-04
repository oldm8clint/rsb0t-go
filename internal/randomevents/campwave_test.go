package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// Live 2026-09-19 (build 27eb4d8, 89 accounts at the Catherby camp): "event monster — handling"
// arrived in waves of 70-110 accounts within one minute, roughly every five minutes. One account's
// own event monster, visible but not yet locked onto its owner, counted as a threat to everyone
// within 15 tiles. Content npc_add's every hostile event exactly one tile from its owner, so an
// unassigned new monster further than freshOwnerRadius away belongs to somebody else.

func campState(self int) *gamestate.State {
	s := gamestate.New()
	s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(uint16(self)); p.P1(0) }))
	setTile(s, 2840, 3432, 0) // Catherby fishing camp
	return s
}

func TestFreshUnassignedEventMonsterAcrossTheCampIsNotOurs(t *testing.T) {
	for _, dist := range []int{freshOwnerRadius + 1, 8, hostileEngageDist} {
		s := campState(5)
		addNPCFacing(s, 3, 391, dist, 0, -1) // River troll, just spawned, no target yet
		if got := Detect(s); got != nil {
			t.Errorf("fresh unassigned troll %d tiles away sent this account fleeing: %+v", dist, got)
		}
	}
}

func TestFreshUnassignedEventMonsterBesideUsStillTriggers(t *testing.T) {
	for _, dist := range []int{1, 2, freshOwnerRadius} {
		s := campState(5)
		addNPCFacing(s, 3, 391, dist, 0, -1)
		if got := Detect(s); got == nil || got.Kind != KindEvade || got.NpcIndex != 3 {
			t.Errorf("our own troll spawning %d tiles away was missed: %+v", dist, got)
		}
	}
}

func TestEventMonsterLockedOnUsTriggersAtAnyEngageDistance(t *testing.T) {
	s := campState(5)
	addNPCFacing(s, 3, 391, hostileEngageDist, 0, playerFaceBase+5)
	if got := Detect(s); got == nil || got.Kind != KindEvade {
		t.Fatalf("troll attacking us 15 tiles away was missed: %+v", got)
	}
}

// After a flee, the spot block must not hold for a monster that has since locked onto its
// real owner: it is chasing them, not us.
func TestCombatBlockLiftsOnceTheThreatChasesAnotherPlayer(t *testing.T) {
	s := combatSession()
	addNPCFacing(s.state, 77, 391, 2, 0, -1)
	noteCombatFleeFrom(s, 3200, 3200, 0, 77, 391)
	if !RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("unassigned threat still beside the spot should keep it blocked")
	}
	addNPCFacing(s.state, 77, 391, 2, 0, playerFaceBase+9)
	if RecentlyAttackedNear(s, 3200, 3200, 0, combatAvoidRadius) {
		t.Fatal("threat now chasing another player kept the spot blocked")
	}
}

// addOtherPlayer adds another visible player at dx,dz from the local tile (PLAYER_INFO NewVis).
func addOtherPlayer(s *gamestate.State, index, dx, dz int) {
	s.Handle(serverprot.PLAYER_INFO, bitPack(6,
		bitField{1, 0}, // local: no update
		bitField{8, 0}, // no previously-tracked players
		bitField{11, uint32(index)},
		bitField{5, uint32(dx & 0x1F)},
		bitField{5, uint32(dz & 0x1F)},
		bitField{1, 0},
		bitField{1, 0},
		bitField{11, 2047},
	))
}

// Live 2026-09-19 on 1515cb3: fishers stack on the tiles beside a busy spot, and 53 of them
// stood one tile from one account's brand-new swarm (face -1, seen 0 ticks). Every one fled.
// While another player is as close as we are, wait for the monster's first target update.
func TestNewUnassignedMonsterInAStackWaitsForItsTarget(t *testing.T) {
	s := campState(5)
	addOtherPlayer(s, 9, 1, 1)
	addNPCFacing(s, 3, 411, 1, 0, -1)
	if got := Detect(s); got != nil {
		t.Fatalf("stacked account fled a swarm that another player is equally close to: %+v", got)
	}
	addNPCFacing(s, 3, 411, 1, 0, playerFaceBase+5)
	if got := Detect(s); got == nil || got.Kind != KindEvade {
		t.Fatalf("once the swarm targets us it must be evaded: %+v", got)
	}
}

func TestNewUnassignedMonsterBesideALoneAccountStillTriggers(t *testing.T) {
	s := campState(5)
	addNPCFacing(s, 3, 411, 1, 0, -1)
	if got := Detect(s); got == nil || got.Kind != KindEvade {
		t.Fatalf("alone, a new swarm beside us is ours: %+v", got)
	}
	s = campState(5)
	addOtherPlayer(s, 9, 6, 0) // five tiles from the swarm, we are one
	addNPCFacing(s, 3, 411, 1, 0, -1)
	if got := Detect(s); got == nil || got.Kind != KindEvade {
		t.Fatalf("nearest to a new swarm, it is ours: %+v", got)
	}
}
