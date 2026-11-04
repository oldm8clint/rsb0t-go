package actions

import (
	"errors"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// Using a held item on something: InvItem.useOn from the TS scripting API. Upstream's Input
// selects the item (USEHELD_START, client-only, no packet) and then fires the USEHELD_ONNPC,
// USEHELD_ONHELD, USEHELD_ONOBJ or USEHELD_ONLOC menu action, whose packets are ported here.
// Every payload ends with the selected item's objId, slot and inventory component, the same
// triple OPHELDn sends. Byte layouts come from Client.ts's doAction and Engine-TS 289's
// OpNpcU/OpHeldU/OpObjU/OpLocU decoders; test/fixtures/useon_vector.json records both.
// Like OpNpc, OpObj and OpLoc, none of these walk. Stand next to the target first: the local
// engine runs with clientRoutefinder on, so it does not path the player to an out-of-reach
// target (found live by AJ "livecheck", docs/script_api_coverage.md).

// ErrUseWhileBanking is returned when a use is attempted while the bank is open. Upstream's
// InvItem.useOn refuses then too: the bank's side backpack only has Deposit buttons.
var ErrUseWhileBanking = errors.New("actions: cannot use an item while the bank is open")

// ErrNoOtherItem is returned by UseOnHeld when the target item is only the used item itself.
var ErrNoOtherItem = errors.New("actions: no other backpack slot holds the target item")

// OpNpcU ports USEHELD_ONNPC: p2(npc index) p2(useObj) p2(useSlot) p2(useCom).
func OpNpcU(s Session, npcIndex int, useObj int32, useSlot, useCom int) error {
	if interactionHeld(s) {
		return nil
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(byte(clientprot.OPNPCU))
		w.P2(uint16(npcIndex))
		w.P2(uint16(useObj))
		w.P2(uint16(useSlot))
		w.P2(uint16(useCom))
	})
}

// OpHeldU ports USEHELD_ONHELD: the target item's p2(obj) p2(slot) p2(com), then the used
// item's p2(useObj) p2(useSlot) p2(useCom).
func OpHeldU(s Session, obj int32, slot, com int, useObj int32, useSlot, useCom int) error {
	if interactionHeld(s) {
		return nil
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(byte(clientprot.OPHELDU))
		w.P2(uint16(obj))
		w.P2(uint16(slot))
		w.P2(uint16(com))
		w.P2(uint16(useObj))
		w.P2(uint16(useSlot))
		w.P2(uint16(useCom))
	})
}

// OpObjU ports USEHELD_ONOBJ: absolute p2(x) p2(z), the ground item's p2(obj), then
// p2(useObj) p2(useSlot) p2(useCom).
func OpObjU(s Session, x, z int, obj int, useObj int32, useSlot, useCom int) error {
	if interactionHeld(s) {
		return nil
	}
	return s.Send(func(w *packet.Packet) {
		w.P1Enc(byte(clientprot.OPOBJU))
		w.P2(uint16(x))
		w.P2(uint16(z))
		w.P2(uint16(obj))
		w.P2(uint16(useObj))
		w.P2(uint16(useSlot))
		w.P2(uint16(useCom))
	})
}

// heldSlot finds the first backpack slot holding objID, with the backpack's component id.
func (e *Equipment) heldSlot(objID int32) (gamestate.SlotItem, int, error) {
	main, _, _ := e.s.State().Modals()
	if main == BankMainModal {
		return gamestate.SlotItem{}, -1, ErrUseWhileBanking
	}
	comID := e.inventoryComponent()
	if comID == -1 {
		return gamestate.SlotItem{}, -1, ErrNotOnScreen
	}
	for _, it := range e.Inventory() {
		if it.ID == objID {
			return it, comID, nil
		}
	}
	return gamestate.SlotItem{}, comID, ErrItemNotFound
}

// UseOnNpc uses the first backpack objID on the NPC at npcIndex (OPNPCU).
func (e *Equipment) UseOnNpc(objID int32, npcIndex int) error {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return err
	}
	return OpNpcU(e.s, npcIndex, use.ID, use.Slot, comID)
}

// UseOnLoc uses the first backpack objID on the loc of type locType at absolute (x, z)
// (OPLOCU).
func (e *Equipment) UseOnLoc(objID int32, x, z, locType int) error {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return err
	}
	return OpLocU(e.s, x, z, locType, use.ID, use.Slot, comID)
}

// UseOnGroundItem uses the first backpack objID on a ground item (OPOBJU).
func (e *Equipment) UseOnGroundItem(objID int32, item gamestate.GroundItem) error {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return err
	}
	return OpObjU(e.s, item.X, item.Z, item.ID, use.ID, use.Slot, comID)
}

// UseOnHeld uses the first backpack objID on the first other backpack slot holding targetID
// (OPHELDU), for example a knife on logs or a tinderbox on logs. When both ids are the same,
// the target is the next slot holding it.
func (e *Equipment) UseOnHeld(objID, targetID int32) error {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return err
	}
	for _, it := range e.Inventory() {
		if it.ID == targetID && it.Slot != use.Slot {
			return OpHeldU(e.s, it.ID, it.Slot, comID, use.ID, use.Slot, comID)
		}
	}
	return ErrNoOtherItem
}

// UseOnHeldMany sends up to n uses of the first backpack objID (one tool: a chisel on uncut gems)
// on n DIFFERENT backpack slots holding targetID, in one burst, and reports how many it sent. The
// pack the client sees does not change until the next tick, so a burst that re-read "the first
// slot" each time (UseOnHeld n times) would name the same, by then emptied, slot n times and the
// engine would drop all but the first. The slots go out lowest first: content's cut removes the
// lowest slot holding the gem (inv_del) and puts the product in the lowest empty slot, so use i
// finds the slot it named still holding the gem. The engine reads five user events a player a tick
// (ClientGameProtCategory.USER_EVENT) and a cut has no delay, so n = 5 is five gems a tick.
func (e *Equipment) UseOnHeldMany(objID, targetID int32, n int) (int, error) {
	slots, err := e.UseOnHeldManyExcept(objID, targetID, n, nil)
	return len(slots), err
}

// UseOnHeldManyExcept is UseOnHeldMany that leaves out the slots in skip, and reports the slots it
// named. It is what lets the bursts be PIPELINED, one a game tick, without waiting for each to show
// in the pack: the pack the client sees lags the server by a tick or two, so a burst sent before the
// last one has shown would otherwise name the same slots again. Skipping the slots already named
// sends the next burst to gems no earlier use touched, and every named slot still holds its gem when
// the engine reaches it: the engine reads each player's queued packets in order, five user events a
// tick (engine/src/engine/entity/NetworkPlayer.ts decodeIn), and content's cut removes the lowest
// slot holding the gem (skill_crafting/scripts/gem/uncut_gem.rs2 inv_del), which in ascending order
// is the slot the use named.
func (e *Equipment) UseOnHeldManyExcept(objID, targetID int32, n int, skip map[int]bool) ([]int, error) {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return nil, err
	}
	var sent []int
	for _, it := range e.Inventory() {
		if len(sent) >= n {
			break
		}
		if it.ID != targetID || it.Slot == use.Slot || skip[it.Slot] {
			continue
		}
		if err := OpHeldU(e.s, it.ID, it.Slot, comID, use.ID, use.Slot, comID); err != nil {
			return sent, err
		}
		sent = append(sent, it.Slot)
	}
	if len(sent) == 0 {
		return nil, ErrNoOtherItem
	}
	return sent, nil
}

// UseOnHeldStack sends n uses of the first backpack objID on the first backpack targetID, naming
// the SAME two slots every time, and reports how many it sent. It is for two STACKS (bolt tips on
// bolts): a use takes some of each stack and leaves both in their slots, so unlike a slot of uncut
// gems, the slot is still valid for the next use in the same tick. The engine validates each use
// against the slot (engine OpHeldUHandler: inv.hasAt), so a use sent after a stack ran out is
// dropped, not misapplied.
func (e *Equipment) UseOnHeldStack(objID, targetID int32, n int) (int, error) {
	use, comID, err := e.heldSlot(objID)
	if err != nil {
		return 0, err
	}
	for _, it := range e.Inventory() {
		if it.ID != targetID || it.Slot == use.Slot {
			continue
		}
		for i := 0; i < n; i++ {
			if err := OpHeldU(e.s, it.ID, it.Slot, comID, use.ID, use.Slot, comID); err != nil {
				return i, err
			}
		}
		return n, nil
	}
	return 0, ErrNoOtherItem
}

// UseOnHeldPairs sends up to n uses of objID on targetID in one burst, each pairing a DIFFERENT
// objID slot with a different targetID slot, and reports how many it sent. The pack the client
// sees does not change until the next tick, so a burst that re-read "the first slot" each time
// would name the same (by then emptied) slots n times. The engine reads up to five user events a
// player a tick (ClientGameProtCategory.USER_EVENT), and an OPHELDU script with no delay runs at
// once, so five pairs a tick is five actions a tick.
func (e *Equipment) UseOnHeldPairs(objID, targetID int32, n int) (int, error) {
	_, comID, err := e.heldSlot(objID)
	if err != nil {
		return 0, err
	}
	var uses, targets []int
	for _, it := range e.Inventory() {
		switch it.ID {
		case objID:
			uses = append(uses, it.Slot)
		case targetID:
			targets = append(targets, it.Slot)
		}
	}
	sent := 0
	for sent < n && sent < len(uses) && sent < len(targets) {
		if err := OpHeldU(e.s, targetID, targets[sent], comID, objID, uses[sent], comID); err != nil {
			return sent, err
		}
		sent++
	}
	if sent == 0 {
		return 0, ErrNoOtherItem
	}
	return sent, nil
}
