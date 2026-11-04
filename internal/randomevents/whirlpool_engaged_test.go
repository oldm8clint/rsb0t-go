package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// setSelfFace decodes a real PLAYER_INFO local-player face-entity update (mask 0x4).
func setSelfFace(t *testing.T, s *gamestate.State, face int) {
	t.Helper()
	main := bitPack(3, bitField{1, 1}, bitField{2, 0}, bitField{8, 0}, bitField{11, 2047})
	ext := payload(func(p *packet.Packet) { p.P1(0x4); p.P2(uint16(face)) })
	s.Handle(serverprot.PLAYER_INFO, append(main, ext...))
	if got := s.SelfFaceEntity(); got != face {
		t.Fatalf("self face = %d, want %d", got, face)
	}
}

func whirlpoolEvent(index, x, z int) *Event {
	ev := newEvent(KindHazard, "whirlpool")
	ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = index, 403, x, z
	return ev
}

func sentMove(ops []byte) bool {
	for _, op := range ops {
		if op == clientprot.MOVE_GAMECLICK {
			return true
		}
	}
	return false
}

// Live on build e35420b6: 20,128 of 29,100 whirlpool handles were for a whirlpool the account's
// own fisher had not stepped off in the 5 seconds before — somebody else's, within Detect's
// 3-tile reach — and every one walked the account 4 tiles off the spot it was fishing. A
// whirlpool only rolls for a player whose click reached it (fisherrunner/whirlpool.go), so the
// handler must leave an account that is neither facing it nor beside it where it is.
func TestWhirlpoolNobodyIsFishingIsNotWalkedAwayFrom(t *testing.T) {
	s := &overlapSession{state: overlapState()} // at 3200,3200
	g := NewGuardian(s, nil)
	defer g.Stop()
	ev := whirlpoolEvent(9, 3203, 3200)
	var lines []string
	if err := HandleHazard(s, &pathfind.Pack{}, ev, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "stepped clear") || !strings.Contains(joined, "nothing to step away from") {
		t.Fatalf("a whirlpool 3 tiles away that the account is not fishing was handled as a step-off:\n%s", joined)
	}
	if sentMove(s.sent) {
		t.Fatal("walked away from a whirlpool the account never clicked")
	}
	g.mu.Lock()
	until := g.retryUntil[eventSignature(ev)]
	g.mu.Unlock()
	if time.Until(until) < whirlpoolLifetime-5*time.Second {
		t.Fatalf("whirlpool not deferred for its lifetime: retry in %s", time.Until(until))
	}
}

func TestWhirlpoolEngagedOnlyWhenFacingOrBeside(t *testing.T) {
	state := overlapState() // at 3200,3200
	if whirlpoolEngaged(state, whirlpoolEvent(9, 3202, 3200)) {
		t.Fatal("two tiles away and not facing it: not engaged")
	}
	if !whirlpoolEngaged(state, whirlpoolEvent(9, 3201, 3201)) {
		t.Fatal("diagonally beside it: a click may have crossed the change, so engaged")
	}
	setSelfFace(t, state, 9)
	if !whirlpoolEngaged(state, whirlpoolEvent(9, 3203, 3200)) {
		t.Fatal("facing the whirlpool: engaged at any distance")
	}
	if whirlpoolEngaged(state, whirlpoolEvent(10, 3203, 3200)) {
		t.Fatal("facing a different npc: not engaged with this whirlpool")
	}
}

// TestWhirlpoolStepOffOnTheRealPack runs both arms on the real pack in the Catherby bank, where a
// step-aside can actually be planned (the shore's candidates all fall in a danger zone, so no
// walk is ever sent there, which would make the control arm prove nothing): facing the
// whirlpool the handler still walks away, and three tiles from someone else's it sends nothing.
// Skipped without RSB0T_COLLISION_PACK.
func TestWhirlpoolStepOffOnTheRealPack(t *testing.T) {
	pack := realCollisionPack(t)
	const x, z = 2808, 3440
	if !pack.Walkable(x, z, 0) {
		t.Fatalf("%d,%d is not walkable on this pack; pick another tile", x, z)
	}
	for _, arm := range []struct {
		name   string
		facing bool
		walks  bool
	}{{"facing it", true, true}, {"someone else's, 3 tiles away", false, false}} {
		state := gamestate.New()
		setTile(state, x, z, 0)
		state.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
		if arm.facing {
			setSelfFace(t, state, 9)
		}
		s := &overlapSession{state: state}
		g := NewGuardian(s, pack)
		if err := HandleHazard(s, pack, whirlpoolEvent(9, x+3, z), nil); err != nil {
			t.Fatal(err)
		}
		g.Stop()
		if got := sentMove(s.sent); got != arm.walks {
			t.Errorf("%s: walked=%v, want %v", arm.name, got, arm.walks)
		}
	}
}
