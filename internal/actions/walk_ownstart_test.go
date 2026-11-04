package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// placeSelf puts the session's own player on a tile: a REBUILD_NORMAL for the build area, then a
// PLAYER_INFO whose local update places the player (op 3) at local 48,48 on level 0.
func placeSelf(t *testing.T, s *fakeSession) pathfind.NavPoint {
	t.Helper()
	s.state.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) { p.P2(400); p.P2(400) }))
	s.state.Handle(serverprot.PLAYER_INFO, msbBits(
		[2]uint32{1, 1}, [2]uint32{2, 3}, // a local update, placed
		[2]uint32{2, 0}, [2]uint32{7, 48}, [2]uint32{7, 48}, [2]uint32{1, 1}, [2]uint32{1, 0},
		[2]uint32{8, 0},     // no other players
		[2]uint32{11, 2047}, // no new ones
	))
	x, z, level, ok := s.state.Tile()
	if !ok {
		t.Fatal("the PLAYER_INFO placed no tile")
	}
	return pathfind.NavPoint{X: x, Z: z, Level: level}
}

// A click whose first waypoint is the player's own tile goes out starting at the next corner, as the
// real client's do; a lone own-tile click is a stop and goes out as it is.
func TestWalkDropsTheOwnTileStart(t *testing.T) {
	s := newFakeSession()
	me := placeSelf(t, s)
	corner := pathfind.NavPoint{X: me.X + 5, Z: me.Z - 2, Level: me.Level}
	far := pathfind.NavPoint{X: me.X + 9, Z: me.Z - 2, Level: me.Level}
	if err := Walk(s, []pathfind.NavPoint{me, corner, far}, true); err != nil {
		t.Fatal(err)
	}
	want := []byte{clientprot.MOVE_GAMECLICK, 2*2 + 3, 1, byte(corner.X >> 8), byte(corner.X), byte(corner.Z >> 8), byte(corner.Z), 4, 0}
	if got := s.lastSent(); !bytes.Equal(got, want) {
		t.Fatalf("Walk(me, corner, far) = % x, want % x (starting at the corner)", got, want)
	}
	if err := Walk(s, []pathfind.NavPoint{me}, false); err != nil {
		t.Fatal(err)
	}
	stop := []byte{clientprot.MOVE_GAMECLICK, 5, 0, byte(me.X >> 8), byte(me.X), byte(me.Z >> 8), byte(me.Z)}
	if got := s.lastSent(); !bytes.Equal(got, stop) {
		t.Fatalf("Walk(me) = % x, want the stop on the own tile % x", got, stop)
	}
	// A start that is not the player's tile is sent unchanged.
	if err := Walk(s, []pathfind.NavPoint{corner, far}, false); err != nil {
		t.Fatal(err)
	}
	if got := s.lastSent(); got[3] != byte(corner.X>>8) || got[4] != byte(corner.X) || got[1] != 2*2+3 {
		t.Fatalf("Walk(corner, far) = % x, want it starting at the corner", got)
	}
}
