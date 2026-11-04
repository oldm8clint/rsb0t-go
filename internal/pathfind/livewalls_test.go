package pathfind

import "testing"

func TestLiveWallRotationLeavesSharedPackAndOtherWallsIntact(t *testing.T) {
	p := &Pack{Version: 2}
	row := new([256]uint16)
	row[0] = 1
	p.index[0][0] = row
	slot := levelSlot{exit: make([]byte, 4096), walk: make([]byte, 512), wall: make([]byte, 2048)}
	for i := range slot.exit {
		slot.exit[i] = 255
	}
	for i := range slot.walk {
		slot.walk[i] = 255
	}
	p.slots = []levelSlot{slot}
	closed := p.WithWalls([]WallEdit{{X: 10, Z: 10, Shape: 0, Angle: 1, Add: true}, {X: 20, Z: 20, Shape: 0, Angle: 0, Add: true}})
	if closed.ExitMask(10, 10, 0)&1 != 0 || closed.ExitMask(10, 11, 0)&4 != 0 {
		t.Fatal("closed north wall did not block both faces")
	}
	open := closed.WithWalls([]WallEdit{{X: 10, Z: 10, Shape: 0, Angle: 1}, {X: 10, Z: 11, Shape: 0, Angle: 0, Add: true}})
	if open.ExitMask(10, 10, 0)&1 == 0 || open.ExitMask(10, 11, 0)&4 == 0 {
		t.Fatal("old crossing did not reopen")
	}
	if open.ExitMask(10, 11, 0)&8 != 0 || open.ExitMask(9, 11, 0)&2 != 0 {
		t.Fatal("rotated wall did not block new crossing")
	}
	if open.ExitMask(20, 20, 0)&8 != 0 {
		t.Fatal("unrelated wall was lost")
	}
	if closed.ExitMask(10, 10, 0)&1 != 0 || p.ExitMask(10, 10, 0) != 255 {
		t.Fatal("shared collision was modified")
	}
	if open.ExitMask(10, 10, 0)&128 != 0 {
		t.Fatal("diagonal cuts rotated wall corner")
	}
	if !open.FindPath(NavPoint{X: 10, Z: 10}, NavPoint{X: 10, Z: 12}).OK {
		t.Fatal("opened crossing is not routable")
	}
}
