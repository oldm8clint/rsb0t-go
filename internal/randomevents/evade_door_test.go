package randomevents

import (
	"os"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

func realLocData(t *testing.T) (*locindex.Index, *cachecfg.Config) {
	t.Helper()
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return ix, cfg
}

// TestEvadeOpensTheKitchenDoorWhenShutIn replays the mule on build e35420b6 (2026-09-24 18:21:09):
// just up the ladder from the Lumbridge basement into the castle kitchen at 3209,3217, its Swarm
// at 3208,3216, the kitchen doors shut. The control reproduces the 196 "no safe reachable local
// escape" answers of the next twelve minutes; the fallback must name a closed door the account
// can reach and open, on the way to an escape.
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestEvadeOpensTheKitchenDoorWhenShutIn(t *testing.T) {
	pack := realCollisionPack(t)
	ix, cfg := realLocData(t)
	origin := pathfind.NavPoint{X: 3209, Z: 3217}
	const threatX, threatZ = 3208, 3216
	toward := pathfind.NavPoint{X: 3222, Z: 3218} // the Lumbridge pickpocket spot it later walked to
	if _, found, _ := evadeEscape(pack, origin, threatX, threatZ, 1, toward); found {
		t.Fatal("control: the kitchen is no longer sealed on the pack — the reproduction does not hold")
	}
	// Each attempt opens the first closed door on the way out; the next attempt plans on the
	// overlay with it open. The castle has a chain of them (the mule's own walk out at 18:33 went
	// through the kitchen Door at 3207,3217 and two Large doors), so this takes a few attempts.
	live := pack
	var opened []pathfind.WallEdit
	for attempt := 1; ; attempt++ {
		if _, found, _ := evadeEscape(live, origin, threatX, threatZ, 1, toward); found {
			if attempt == 1 {
				t.Fatal("unreachable")
			}
			t.Logf("escape found after opening %d door(s)", attempt-1)
			break
		}
		if attempt > 4 {
			t.Fatalf("still shut in after opening %d doors", attempt-1)
		}
		door, near, ok := evadeDoorOut(live, ix, cfg, nil, origin, threatX, threatZ, 1, toward)
		if !ok {
			t.Fatalf("attempt %d: no door out of the kitchen was planned", attempt)
		}
		t.Logf("attempt %d: door %q id %d at %d,%d angle %d, opened from %d,%d (op %d)", attempt, door.Name, door.ID, door.X, door.Z, door.Angle, near.X, near.Z, door.Op)
		if door.Op == 0 || chebyshev(door.X, door.Z, origin.X, origin.Z) > evadeDoorRadius {
			t.Fatalf("bad door %+v", door)
		}
		if near != origin && !live.FindPath(origin, near).OK {
			t.Fatalf("the side of the door it opens from, %v, is not reachable from %v", near, origin)
		}
		if !doorCrossing(door, near.X, near.Z, near.X+1, near.Z) && !doorCrossing(door, near.X, near.Z, near.X-1, near.Z) &&
			!doorCrossing(door, near.X, near.Z, near.X, near.Z+1) && !doorCrossing(door, near.X, near.Z, near.X, near.Z-1) {
			t.Fatalf("%v does not stand at the door's wall", near)
		}
		if attempt == 1 && (door.X != 3207 || door.Z != 3217) {
			t.Errorf("first door %d,%d, want the kitchen door at 3207,3217 the mule left by", door.X, door.Z)
		}
		opened = append(opened, pathfind.WallEdit{X: door.X, Z: door.Z, Level: door.Level, Shape: door.Shape, Angle: door.Angle})
		live = pack.WithWalls(opened)
	}
}

func TestDoorCrossingFollowsTheWallAngle(t *testing.T) {
	north := locindex.Match{Loc: locindex.Loc{X: 10, Z: 10, Shape: 0, Angle: 1}}
	if !doorCrossing(north, 10, 10, 10, 11) || !doorCrossing(north, 10, 11, 10, 10) {
		t.Fatal("a north wall separates its tile from the one north of it")
	}
	if doorCrossing(north, 10, 10, 11, 10) || doorCrossing(north, 10, 10, 10, 9) {
		t.Fatal("a north wall does not block east or south")
	}
	corner := locindex.Match{Loc: locindex.Loc{X: 10, Z: 10, Shape: 2, Angle: 0}}
	if !doorCrossing(corner, 10, 10, 9, 10) || !doorCrossing(corner, 10, 10, 10, 11) || doorCrossing(corner, 10, 10, 11, 10) {
		t.Fatal("a corner at angle 0 walls west and north only")
	}
}

// TestHandleEvadeOpensTheDoorInsteadOfReportingNoEscape drives HandleEvade itself, standing on
// the kitchen door's tile (so no walk is needed first) with the Swarm beside it. Before the
// fallback it only slept and answered "no safe reachable local escape"; now it clicks Open on the
// door (the session plays content's loc_del back on the click) and, in the same call, plans again
// and heads for the next closed door on the way out, the castle's Large door. This fake session
// never moves, so that second walk cannot arrive and the call ends in the ordinary error.
func TestHandleEvadeOpensTheDoorInsteadOfReportingNoEscape(t *testing.T) {
	pack := realCollisionPack(t)
	ix, cfg := realLocData(t)
	withResolver(t, cfg)
	prevIx := currentLocIndex()
	SetLocIndex(ix)
	t.Cleanup(func() { SetLocIndex(prevIx) })

	const baseX, baseZ = 3200, 3208
	state := gamestate.New()
	setTileAt(state, baseX, baseZ, 7, 9, 0) // 3207,3217: the kitchen door's tile
	state.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(5); p.P1(0) }))
	s := &overlapSession{state: state}
	opened := false
	s.onSend = func(op byte) {
		if op == clientprot.OPLOC1 && !opened {
			opened = true
			liveLocChange(state, baseX, baseZ, 3207, 3217, -1, 0, 1)
		}
	}
	g := NewGuardian(s, pack)
	defer g.Stop()
	ev := newEvent(KindEvade, "event monster")
	ev.NpcIndex, ev.NpcType, ev.ThreatX, ev.ThreatZ = 7, 411, 3208, 3216
	var lines []string
	err := HandleEvade(s, pack, ev, 1, func(l string) { lines = append(lines, l) })
	if !opened {
		t.Fatalf("no Open was sent on the door (err=%v):\n%s", err, strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "opening the Large door at 3213,3220") || !sentMove(s.sent) {
		t.Fatalf("after the kitchen door the same call must head for the next door out (err=%v):\n%s", err, joined)
	}
}
