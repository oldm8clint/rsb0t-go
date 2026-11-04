package gamestate

import "rsb0t-go/internal/packet"

// Item is one inventory slot as the server reports it: an obj id and a stack count.
// A zero ID means an empty slot (the client zeroes untransmitted slots the same way).
type Item struct {
	ID    int32
	Count int32
}

// Inventory mirrors what Client.ts keeps per interface component in
// IfType.linkObjType/linkObjNumber plus its invUpdateState bookkeeping: the slots, and
// generation counters so callers can tell "has a full snapshot arrived yet" from "is
// this just partial deltas on top of nothing".
type Inventory struct {
	Slots          []Item
	Generation     int // bumped on every FULL or PARTIAL update
	FullGeneration int // generation at the last FULL update; 0 = never received a full snapshot
}

// readCount ports the shared count encoding: one byte, or 255 followed by a 4-byte count.
func readCount(r *packet.Packet) int32 {
	count := int32(r.G1())
	if count == 255 {
		count = r.G4()
	}
	return count
}

// readObjID ports the wire's item-id convention (UpdateInvFullEncoder.ts /
// UpdateInvPartialEncoder.ts): ids are transmitted as objId+1 so 0 can mean "empty
// slot" without colliding with the real obj id 0. Only non-empty ids get the -1.
func readObjID(r *packet.Packet) int32 {
	id := int32(r.G2())
	if id == 0 {
		return 0
	}
	return id - 1
}

// applyFull ports the UPDATE_INV_FULL handler: [comId already consumed] size:u16, then
// size × (id:u16, count:u8|255+u32). Slots beyond `size` are zeroed, matching the
// client zeroing the rest of linkObjType/linkObjNumber.
//
// comID is the inventory's own component id, needed only to stamp it onto any
// returned EventInventoryChanged (the Inventory itself doesn't otherwise know its
// component id — see State.inventory's map key). Returns one Event per slot whose id
// or count actually changed, in ascending slot order; a full resync that reports the
// same contents back (common right after a partial update, or a no-op refresh)
// returns nil. See events.go's package doc for why this is a separate, smaller diff
// than internal/statlog's own (gains-only, externally-snapshotted) one.
func (inv *Inventory) applyFull(comID int, r *packet.Packet) []Event {
	size := int(r.G2())
	if size > len(inv.Slots) {
		inv.Slots = append(inv.Slots, make([]Item, size-len(inv.Slots))...)
	}
	var events []Event
	for i, prev := range inv.Slots {
		var cur Item // slots omitted from the full snapshot are cleared
		if i < size {
			cur.ID = readObjID(r)
			cur.Count = readCount(r)
		}
		inv.Slots[i] = cur
		if prev == cur {
			continue
		}
		events = append(events, Event{
			Kind: EventInventoryChanged, Comp: comID, Slot: i,
			ItemID: cur.ID, PreviousID: prev.ID,
			Count: cur.Count, PreviousCount: prev.Count,
		})
	}
	inv.Generation++
	inv.FullGeneration = inv.Generation
	return events
}

// applyPartial ports the UPDATE_INV_PARTIAL handler: [comId already consumed] then
// repeated (slot:gsmart, id:u16, count:u8|255+u32) until the payload is exhausted.
// comID and the returned events mirror applyFull's — see its doc comment.
func (inv *Inventory) applyPartial(comID int, r *packet.Packet) []Event {
	var events []Event
	for r.Available() > 0 {
		slot := int(r.GSmart())
		id := readObjID(r)
		count := readCount(r)
		if slot < 0 {
			continue
		}
		if slot >= len(inv.Slots) {
			inv.Slots = append(inv.Slots, make([]Item, slot+1-len(inv.Slots))...)
		}
		prev := inv.Slots[slot]
		inv.Slots[slot] = Item{ID: id, Count: count}
		if prev.ID != id || prev.Count != count {
			events = append(events, Event{
				Kind: EventInventoryChanged, Comp: comID, Slot: slot,
				ItemID: id, PreviousID: prev.ID,
				Count: count, PreviousCount: prev.Count,
			})
		}
	}
	inv.Generation++
	return events
}

// NonEmpty returns the occupied slots with their indices, the shape trade/bank code
// wants when deciding what to offer or withdraw.
func (inv *Inventory) NonEmpty() []SlotItem {
	count := 0
	for _, it := range inv.Slots {
		if it.ID != 0 {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	out := make([]SlotItem, 0, count)
	for i, it := range inv.Slots {
		if it.ID != 0 {
			out = append(out, SlotItem{Slot: i, Item: it})
		}
	}
	return out
}

// SlotItem is an Item together with its slot index.
type SlotItem struct {
	Slot int
	Item
}

// InventoryGenerations is an inventory's update counters, read WITHOUT copying its slots: generation is
// bumped by every full or partial update, fullGeneration is 0 until a full snapshot has arrived, and ok
// says whether anything has been received for the component at all. A caller that remembers a reading
// of a big inventory (the bank's hundreds of slots) compares these to learn whether it changed, instead
// of copying the whole inventory to look.
func (s *State) InventoryGenerations(comID int) (generation, fullGeneration int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.inventories[comID]
	if !ok {
		return 0, 0, false
	}
	return inv.Generation, inv.FullGeneration, true
}
