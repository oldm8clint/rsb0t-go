package randomevents

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// Live 2026-09-23 15:15:35 (build f3017f87): the mule cooking at the Catherby range, 2817,3443, a
// Swarm spawned at 2816,3442, and "evade: no safe reachable local escape from 2817,3443,0" came
// back about twice a second for two and a half minutes. On the STATIC pack the range room is a
// sealed 13-tile component -- its only exit, the Door (loc 1530, shape 0, angle 1) at 2816,3438,
// is baked shut -- so the planner could find nowhere at any distance. The mule had walked in
// through that door, open, twelve seconds earlier.
//
// CONTROL: the static pack reproduces the live failure exactly. The fix is that HandleEvade plans
// on fleePack's live overlay, which removes a door wall the session has seen opened; with that
// door's wall removed the same planner finds a way out of the room.
func TestTheRangeRoomSwarmHasAWayOutOnceTheOpenDoorIsSeen(t *testing.T) {
	pk := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2817, Z: 3443, Level: 0}
	const threatX, threatZ = 2816, 3442

	if _, found, _ := evadeEscape(pk, origin, threatX, threatZ, 1, CatherbyBankRef); found {
		t.Fatal("control: on the static pack the range room should have no escape (the live failure)")
	}

	opened := pk.WithWalls([]pathfind.WallEdit{{X: 2816, Z: 3438, Level: 0, Shape: 0, Angle: 1}})
	dest, found, _ := evadeEscape(opened, origin, threatX, threatZ, 1, CatherbyBankRef)
	if !found {
		t.Fatal("with the range room's door open there must be an escape out of the room")
	}
	if onRangeRoomSide(dest) {
		t.Fatalf("the escape %v is still inside the range room", dest)
	}
	// The retries the guardian makes ask for more separation; they must still find one.
	for attempt := 2; attempt <= 6; attempt++ {
		if _, ok, _ := evadeEscape(opened, origin, threatX, threatZ, attempt, CatherbyBankRef); !ok {
			t.Fatalf("attempt %d found no escape through the open door", attempt)
		}
	}
}

// onRangeRoomSide is mulerunner's onRangeSide, restated: the 13-tile component cmd/doordiag reports.
func onRangeRoomSide(p pathfind.NavPoint) bool {
	return p.Level == 0 && p.X >= 2815 && p.X <= 2818 && p.Z >= 3439 && p.Z <= 3443
}
