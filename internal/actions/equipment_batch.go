package actions

// WEAR AND REMOVE SEVERAL PIECES IN ONE TICK. EquipItemAndWait sends one Wield/Wear and waits up to three
// seconds for the worn panel to show it before the caller sends the next, so a death re-gear of five pieces,
// a mode switch's armour swap or a quest kit cost a round trip (one to two ticks) a piece. EquipMany and
// UnequipMany send them all in the one tick (BankMaxPerTick user events a burst) and confirm ONCE.
//
// What content and the engine allow (content 289 player/scripts/equip.rs2, engine OpHeldHandler):
//
//   - [opheld2,_] ~equip(last_slot) and [inv_button1,wornitems:wear] ~unequip(last_slot) run in the
//     handler, with no p_delay and no %action_delay, so the next packet of the same tick is accepted.
//     (Only an elemental shield, a gree-gree or the staff of Iban add a spot anim or a p_delay(0), and
//     none of them is a piece a script wears.)
//   - ~equip swaps the clicked slot with the worn one (inv_movetoslot) and then moves the displaced piece to
//     the FIRST EMPTY slot (inv_movefromslot): it never lands on, or shifts, a slot that holds another piece,
//     so every slot named from one pack reading still holds its piece when the engine reaches it. The same is
//     true of an unequip: the worn panel never closes up, so each worn slot keeps its index.
//   - A refusal (a level, a full pack, the duel arena) is a chat line and nothing else; the batch confirms
//     once at the end and the pieces that did not show are tried again one at a time, as before.
//
// Two pieces that fight for a wear position (two weapons) must not be named in one batch: the second
// swap would put the first back. Callers pass one piece a position.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
)

// EquipBatch is what one EquipMany or UnequipMany did.
type EquipBatch struct {
	// Wanted is how many distinct pieces were asked for.
	Wanted int
	// Sent is the pieces whose click went out in the burst, Done the pieces the panel then showed (worn for
	// EquipMany, off for UnequipMany), Already the pieces that needed nothing.
	Sent, Done, Already []int32
	// Missing is the pieces the pack (or the worn panel) did not hold, Unusable those whose config has no
	// Wield/Wear/Equip option. Nothing was sent for either.
	Missing, Unusable []int32
	// Retried is the pieces the burst did not land that the one-at-a-time path then did; Left are those it
	// did not either (the server refused them).
	Retried, Left []int32
	// Bursts is the ticks the clicks took (1 for up to five pieces).
	Bursts int
	// Took is the whole call.
	Took time.Duration
	// Err is a click that could not be written or a stop.
	Err error
}

// OK reports whether every piece asked for is now as wanted.
func (b EquipBatch) OK() bool {
	return b.Err == nil && len(b.Missing) == 0 && len(b.Unusable) == 0 && len(b.Left) == 0
}

// Log is the one-line account of a batch for a script's log: what went out in how many ticks.
func (b EquipBatch) Log(verb string) string {
	s := fmt.Sprintf("equipment: %s %d of %d piece(s) in %d tick(s) (%d click(s) in one burst, %s)",
		verb, len(b.Done)+len(b.Retried)+len(b.Already), b.Wanted, b.Bursts, len(b.Sent), b.Took.Round(time.Millisecond))
	if len(b.Retried) > 0 {
		s += fmt.Sprintf("; %d landed on the one-at-a-time retry", len(b.Retried))
	}
	if len(b.Left) > 0 {
		s += fmt.Sprintf("; %d refused", len(b.Left))
	}
	if len(b.Missing) > 0 {
		s += fmt.Sprintf("; %d not held", len(b.Missing))
	}
	if b.Err != nil {
		s += "; " + b.Err.Error()
	}
	return s
}

func dedupeIDs(ids []int32) []int32 {
	seen := map[int32]bool{}
	var out []int32
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// EquipMany wears each of ids from the backpack in one burst: one Wield/Wear click a piece, each naming a
// DIFFERENT backpack slot read from one snapshot, five to a tick, then one confirm on the worn panel. A
// piece already worn is not clicked. Whatever the burst did not land is worn again with EquipItemAndWait, one
// at a time. It never returns an error for a refused piece: read the batch.
func (e *Equipment) EquipMany(types ObjTypes, ids []int32, w Waiter) (out EquipBatch) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	ids = dedupeIDs(ids)
	out.Wanted = len(ids)
	if len(ids) == 0 {
		return out
	}
	comID := e.inventoryComponent()
	if comID == -1 {
		out.Err = ErrNotOnScreen
		return out
	}
	pack := e.Inventory()
	used := map[int]bool{}
	var packets []BurstPacket
	for _, id := range ids {
		if e.IsWorn(id) {
			out.Already = append(out.Already, id)
			continue
		}
		option := wearOption
		if types != nil {
			if obj, ok := types.Obj(int(id)); ok {
				if option = EquipOption(obj.IOps); option == 0 {
					out.Unusable = append(out.Unusable, id)
					continue
				}
			}
		}
		slot := -1
		for _, it := range pack {
			if it.ID == id && !used[it.Slot] {
				slot = it.Slot
				break
			}
		}
		if slot < 0 {
			out.Missing = append(out.Missing, id)
			continue
		}
		used[slot] = true
		id, slot, option := id, slot, option
		out.Sent = append(out.Sent, id)
		packets = append(packets, BurstPacket{Events: 1, Send: func() error { return OpHeld(e.s, option, id, slot, comID) }})
	}
	allWorn := func() bool {
		for _, id := range out.Sent {
			if !e.IsWorn(id) {
				return false
			}
		}
		return true
	}
	res := SendBurst(e.s, w, BankMaxPerTick, allWorn, packets)
	out.Bursts = res.Bursts
	if res.Err != nil {
		out.Err = res.Err
		return out
	}
	if len(packets) > 0 {
		w.DelayUntil(allWorn, equipConfirmTimeout)
	}
	for _, id := range out.Sent {
		switch {
		case e.IsWorn(id):
			out.Done = append(out.Done, id)
		case e.EquipItemAndWait(types, id, w) == nil:
			out.Retried = append(out.Retried, id)
		default:
			out.Left = append(out.Left, id)
		}
	}
	return out
}

// UnequipMany takes each of ids off in one burst (Remove on its worn slot, read from one snapshot of the worn
// panel), then confirms once on the panel. A piece that is not worn needs nothing. The ones the burst did not
// land (a full pack refuses the rest) are tried again with UnequipAndWait, one at a time.
func (e *Equipment) UnequipMany(ids []int32, w Waiter) (out EquipBatch) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	ids = dedupeIDs(ids)
	out.Wanted = len(ids)
	if len(ids) == 0 {
		return out
	}
	comID := e.wornComponent()
	if comID == -1 {
		out.Err = ErrNotOnScreen
		return out
	}
	worn := e.Worn()
	var packets []BurstPacket
	for _, id := range ids {
		var at *gamestate.SlotItem
		for i := range worn {
			if worn[i].ID == id {
				at = &worn[i]
				break
			}
		}
		if at == nil {
			out.Already = append(out.Already, id)
			continue
		}
		id, slot := id, at.Slot
		out.Sent = append(out.Sent, id)
		packets = append(packets, BurstPacket{Events: 1, Send: func() error { return InvButton(e.s, removeOption, id, slot, comID) }})
	}
	allOff := func() bool {
		for _, id := range out.Sent {
			if e.IsWorn(id) {
				return false
			}
		}
		return true
	}
	res := SendBurst(e.s, w, BankMaxPerTick, allOff, packets)
	out.Bursts = res.Bursts
	if res.Err != nil {
		out.Err = res.Err
		return out
	}
	if len(packets) > 0 {
		w.DelayUntil(allOff, equipConfirmTimeout)
	}
	for _, id := range out.Sent {
		switch {
		case !e.IsWorn(id):
			out.Done = append(out.Done, id)
		case e.UnequipAndWait(id, w) == nil:
			out.Retried = append(out.Retried, id)
		default:
			out.Left = append(out.Left, id)
		}
	}
	return out
}

// UnequipManyByName is UnequipMany for the worn pieces whose config names equal names, ignoring case. A name
// nothing worn carries is skipped.
func (e *Equipment) UnequipManyByName(types ObjTypes, names []string, w Waiter) EquipBatch {
	if types == nil {
		return EquipBatch{Wanted: len(names)}
	}
	var ids []int32
	for _, name := range names {
		for _, it := range e.Worn() {
			if obj, ok := types.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
				ids = append(ids, it.ID)
				break
			}
		}
	}
	return e.UnequipMany(ids, w)
}
