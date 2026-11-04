package actions

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

var opObjOpcodes = [5]int{
	clientprot.OPOBJ1,
	clientprot.OPOBJ2,
	clientprot.OPOBJ3,
	clientprot.OPOBJ4,
	clientprot.OPOBJ5,
}

// OpObj ports the OP_OBJ1..5 branch of Client.ts's doAction (~line 8769): absolute tile
// plus the ground item's type id, option 1-based. A ground item's default "Take" option
// (no custom op1-4 defined on the item type) is OP_OBJ3 — Client.ts's menu builder falls
// back to it explicitly (~line 9640) — so callers picking up a plain item without a
// bespoke interaction should pass option 3. Like OpLoc, the client-side tryMove/approach is
// not ported; callers should already be adjacent. Options 1, 4 and 5 carry Client.ts's
// anti-cheat side-packets (oplogic7/8/3), which need the build area's base to turn the world
// tile back into the scene-local one the client counts; before the first REBUILD_NORMAL (no
// base yet) they are not advanced, since a real client has no scene to click in either.
func OpObj(s Session, option int, x, z, itemType int) error {
	if option < 1 || option > 5 {
		return errBadOption
	}
	if interactionHeld(s) {
		return nil // the Guardian holds the account's input while it handles an event (movehold.go)
	}
	baseX, baseZ, based := s.State().MapBuildBase()
	based = based && inBuildArea(x-baseX, z-baseZ)
	return s.Send(func(w *packet.Packet) {
		if based {
			switch option {
			case 1:
				s.Cheats().BeforeOpObj1(w, int32(x-baseX))
			case 4:
				s.Cheats().BeforeOpObj4(w, int32(z-baseZ))
			case 5:
				s.Cheats().BeforeOpObj5(w, int32(baseZ))
			}
		}
		w.P1Enc(byte(opObjOpcodes[option-1]))
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(itemType))
	})
}

// buildAreaSize is the client's scene width in tiles (104).
const buildAreaSize = 104

// inBuildArea reports whether a scene-local tile is inside the build area. A real client can
// only click inside it, so a tile outside it (a negative or too-large local coordinate) must not
// move a counter that sums local coordinates.
func inBuildArea(localX, localZ int) bool {
	return localX >= 0 && localX < buildAreaSize && localZ >= 0 && localZ < buildAreaSize
}
