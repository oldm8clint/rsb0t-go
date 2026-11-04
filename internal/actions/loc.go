package actions

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

var opLocOpcodes = [5]int{
	clientprot.OPLOC1,
	clientprot.OPLOC2,
	clientprot.OPLOC3,
	clientprot.OPLOC4,
	clientprot.OPLOC5,
}

// OpLoc ports the OP_LOC1..5 branch (Client.ts's interactWithLoc, ~line 5703): absolute
// tile plus the loc's type id, option 1-based. The client-side approach/tryMove is not
// ported; callers should already be adjacent. The anti-cheat side-packets are: OPLOGIC1 on
// option 2 (it counts the scene-local z, so it waits for the first REBUILD_NORMAL like
// OpObj's), OPLOGIC2 on option 3, and interactWithLoc's CYCLELOGIC2 on every option.
func OpLoc(s Session, option int, x, z, locType int) error {
	if option < 1 || option > 5 {
		return errBadOption
	}
	if movementHeld(s) {
		return nil // another caller holds this account's movement (movehold.go)
	}
	baseX, baseZ, based := s.State().MapBuildBase()
	based = based && inBuildArea(x-baseX, z-baseZ)
	return s.Send(func(w *packet.Packet) {
		switch {
		case option == 2 && based:
			s.Cheats().BeforeOpLoc2(w, int32(z-baseZ))
		case option == 3:
			s.Cheats().BeforeOpLoc3(w)
		}
		s.Cheats().BeforeInteractWithLoc(w)
		w.P1Enc(byte(opLocOpcodes[option-1]))
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(locType))
	})
}

// OpLocU ports OPLOCU (using an inventory item on a loc — dragging flax onto a spinning
// wheel, dough onto a range, etc.): a fixed 12-byte payload, p2(x) p2(z) p2(locType)
// p2(useObj) p2(useSlot) p2(useCom), confirmed against the engine's own decoder
// (network/game/client/codec/OpLocUDecoder.ts: `new ClientGameProt(184, 12)`, decoding
// x,z,loc,useObj,useSlot,useCom in that order — the same objId/slot/comId triple
// InvButton/opHeld already send for a panel-relative item, here paired with the loc
// being used on). useCom is the inventory panel's own component id (e.g. the backpack's
// 3214, see fisherrunner.PlayerPack/crafterrunner.PlayerPack) the item's slot is read
// against. First added and confirmed live (three real flax spun into bow strings against
// the local engine) by the ProgressiveCrafter port — this is the first "use item on X"
// wire action in this codebase.
func OpLocU(s Session, x, z, locType int, useObj int32, useSlot int, useCom int) error {
	if interactionHeld(s) {
		return nil // the Guardian holds the account's input while it handles an event (movehold.go)
	}
	return s.Send(func(w *packet.Packet) {
		s.Cheats().BeforeInteractWithLoc(w)
		w.P1Enc(byte(clientprot.OPLOCU))
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(locType))
		w.P2(uint16(useObj))
		w.P2(uint16(useSlot))
		w.P2(uint16(useCom))
	})
}
