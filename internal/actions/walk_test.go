package actions

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestWalkSingleWaypointWalking(t *testing.T) {
	s := newFakeSession()
	wp := []pathfind.NavPoint{{X: 3200, Z: 3200, Level: 0}}
	if err := Walk(s, wp, false); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	// size = 2*len(waypoints)+3 = 5; ctrlHeld=0 (walking); start tile as two big-endian
	// uint16s; no further corners.
	want := []byte{clientprot.MOVE_GAMECLICK, 5, 0, 0x0C, 0x80, 0x0C, 0x80} // 3200 = 0x0C80
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Walk(1 waypoint) = % x, want % x", got, want)
	}
}

func TestWalkRunFlagAndRelativeCorners(t *testing.T) {
	s := newFakeSession()
	wp := []pathfind.NavPoint{
		{X: 3200, Z: 3200, Level: 0},
		{X: 3205, Z: 3198, Level: 0}, // +5, -2 relative to start
	}
	if err := Walk(s, wp, true); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	size := 2*len(wp) + 3
	want := []byte{
		clientprot.MOVE_GAMECLICK, byte(size), 1, // ctrlHeld=1 for running
		0x0C, 0x80, 0x0C, 0x80, // start 3200,3200
		5,    // +5 as a signed byte
		0xFE, // -2 as a signed byte (two's complement)
	}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Walk(run, 2 waypoints) = % x, want % x", got, want)
	}
}

func TestWalkRejectsEmptyWaypoints(t *testing.T) {
	s := newFakeSession()
	if err := Walk(s, nil, false); err != errNoWaypoints {
		t.Fatalf("Walk(nil): err = %v, want errNoWaypoints", err)
	}
	if s.sendCount() != 0 {
		t.Fatal("Walk(nil) should never call Send")
	}
}

func TestWalkTruncatesToMaxWaypoints(t *testing.T) {
	s := newFakeSession()
	wp := make([]pathfind.NavPoint, 30)
	for i := range wp {
		wp[i] = pathfind.NavPoint{X: 3200 + i, Z: 3200, Level: 0}
	}
	if err := Walk(s, wp, false); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := s.lastSent()
	// Truncated to maxWaypoints(25): size = 2*25+3 = 53, and only 24 corner pairs
	// follow the start tile (24 relative-offset pairs = 48 bytes) — anything beyond
	// waypoint 25 must never appear on the wire.
	wantSize := byte(2*maxWaypoints + 3)
	if got[1] != wantSize {
		t.Fatalf("Walk(30 waypoints): size byte = %d, want %d (truncated to %d waypoints)", got[1], wantSize, maxWaypoints)
	}
	wantLen := 2 + 1 + 4 + 2*(maxWaypoints-1) // opcode+size, ctrlHeld, start(4 bytes), corners
	if len(got) != wantLen {
		t.Fatalf("Walk(30 waypoints): total wire length = %d, want %d", len(got), wantLen)
	}
	// The last corner byte pair must correspond to waypoint index 24 (the 25th),
	// relative offset +24 from the start tile — waypoint 25..29 must not appear.
	lastDX := int8(got[len(got)-2])
	if int(lastDX) != maxWaypoints-1 {
		t.Fatalf("Walk(30 waypoints): last corner dx = %d, want %d (waypoint 25's offset)", lastDX, maxWaypoints-1)
	}
}

// WalkOpClick is Walk's packet under MOVE_OPCLICK's opcode, byte for byte: the server reads both
// with one decoder (Engine-TS MoveClickDecoder) and only the opcode tells it not to close the
// player's dialogue.
func TestWalkOpClickDiffersFromWalkOnlyByOpcode(t *testing.T) {
	wp := []pathfind.NavPoint{
		{X: 3200, Z: 3200, Level: 0},
		{X: 3205, Z: 3198, Level: 0},
		{X: 3205, Z: 3190, Level: 0},
	}
	for _, run := range []bool{false, true} {
		a, b := newFakeSession(), newFakeSession()
		if err := Walk(a, wp, run); err != nil {
			t.Fatal(err)
		}
		if err := WalkOpClick(b, wp, run); err != nil {
			t.Fatal(err)
		}
		ga, gb := a.lastSent(), b.lastSent()
		if len(ga) != len(gb) || len(ga) == 0 {
			t.Fatalf("lengths differ: %d vs %d", len(ga), len(gb))
		}
		if ga[0] != clientprot.MOVE_GAMECLICK || gb[0] != clientprot.MOVE_OPCLICK {
			t.Errorf("opcodes = %d and %d, want %d and %d", ga[0], gb[0], clientprot.MOVE_GAMECLICK, clientprot.MOVE_OPCLICK)
		}
		if string(ga[1:]) != string(gb[1:]) {
			t.Errorf("payloads differ (run=%v): % x vs % x", run, ga[1:], gb[1:])
		}
	}
}

func TestWalkOpClickRejectsEmptyWaypoints(t *testing.T) {
	s := newFakeSession()
	if err := WalkOpClick(s, nil, false); err != errNoWaypoints {
		t.Fatalf("WalkOpClick(nil): err = %v, want errNoWaypoints", err)
	}
	if s.sendCount() != 0 {
		t.Fatal("WalkOpClick(nil) should never call Send")
	}
}

func TestWalkOpClickIsHeldLikeWalk(t *testing.T) {
	s := newFakeSession()
	hold := HoldMovement(s, nil)
	defer hold.Release()
	wp := []pathfind.NavPoint{{X: 3200, Z: 3200, Level: 0}}
	if err := WalkOpClick(s, wp, false); err != nil {
		t.Fatal(err)
	}
	if s.sendCount() != 0 {
		t.Fatal("a held account's WalkOpClick must be dropped")
	}
	if err := WalkOpClick(hold.Owner, wp, false); err != nil {
		t.Fatal(err)
	}
	if s.sendCount() != 1 {
		t.Fatalf("the holder's own WalkOpClick must go out; sends = %d", s.sendCount())
	}
}
