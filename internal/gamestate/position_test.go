package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// rebuild sends REBUILD_NORMAL for the given centre zone (build origin (zone-6)*8).
func rebuild(s *State, zoneX, zoneZ int) {
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) { p.P2(uint16(zoneX)); p.P2(uint16(zoneZ)) }))
}

// teleportLocal sends a PLAYER_INFO whose local update is op 3 to a build-local tile, with
// no other players.
func teleportLocal(s *State, level, localX, localZ int) {
	s.Handle(serverprot.PLAYER_INFO, bitPack(5,
		bitField{1, 1}, bitField{2, 3},
		bitField{2, uint32(level)}, bitField{7, uint32(localX)}, bitField{7, uint32(localZ)},
		bitField{1, 0}, bitField{1, 0}, // jump, extendedInfo
		bitField{8, 0}, bitField{11, 2047},
	))
}

func wantTile(t *testing.T, s *State, x, z int) {
	t.Helper()
	gx, gz, _, ok := s.Tile()
	if !ok || gx != x || gz != z {
		t.Fatalf("Tile() = (%d,%d) ok=%v, want (%d,%d)", gx, gz, ok, x, z)
	}
}

// TestRebuildMidWalkKeepsTheAbsoluteTile reproduces the "position drifts while running"
// finding: Engine-TS sends REBUILD_NORMAL then a plain run step (no teleport) when a walk
// crosses the build area's reload edge.
func TestRebuildMidWalkKeepsTheAbsoluteTile(t *testing.T) {
	s := New()
	rebuild(s, 400, 400) // origin 3152,3152
	teleportLocal(s, 0, 60, 60)
	wantTile(t, s, 3212, 3212)

	rebuild(s, 404, 400) // origin 3184,3152: 32 tiles east
	wantTile(t, s, 3212, 3212)

	// Run two steps east (direction 4 is x+1).
	s.Handle(serverprot.PLAYER_INFO, bitPack(4,
		bitField{1, 1}, bitField{2, 2}, bitField{3, 4}, bitField{3, 4}, bitField{1, 0},
		bitField{8, 0}, bitField{11, 2047},
	))
	wantTile(t, s, 3214, 3212)

	// A rebuild to the same centre zone changes nothing.
	rebuild(s, 404, 400)
	wantTile(t, s, 3214, 3212)
}

// TestRebuildKeepsNpcsAndPlayersInPlace checks the same shift for other entities, which
// Client.ts applies to every NPC and player on REBUILD_NORMAL.
func TestRebuildKeepsNpcsAndPlayersInPlace(t *testing.T) {
	s := New()
	rebuild(s, 400, 400)
	teleportLocal(s, 0, 60, 60) // 3212,3212

	// One new NPC, index 5, type 100, at dx=3 dz=-2 from the local player.
	s.Handle(serverprot.NPC_INFO, bitPack(8,
		bitField{8, 0},
		bitField{14, 5}, bitField{11, 100}, bitField{5, 3}, bitField{5, 30}, bitField{1, 0}, bitField{1, 0},
		bitField{14, 16383},
	))
	// No local update, one new player, index 9, at dx=2 dz=1.
	s.Handle(serverprot.PLAYER_INFO, bitPack(6,
		bitField{1, 0},
		bitField{8, 0},
		bitField{11, 9}, bitField{5, 2}, bitField{5, 1}, bitField{1, 0}, bitField{1, 0},
		bitField{11, 2047},
	))

	check := func(when string) {
		t.Helper()
		npcs := s.NPCs()
		if len(npcs) != 1 || npcs[0].X != 3215 || npcs[0].Z != 3210 {
			t.Fatalf("%s: NPCs() = %+v, want one at 3215,3210", when, npcs)
		}
		players := s.Players()
		if len(players) != 1 || players[0].X != 3214 || players[0].Z != 3213 {
			t.Fatalf("%s: Players() = %+v, want one at 3214,3213", when, players)
		}
	}
	check("before rebuild")
	rebuild(s, 396, 405) // origin moves 32 west and 40 north
	wantTile(t, s, 3212, 3212)
	check("after rebuild")
}
