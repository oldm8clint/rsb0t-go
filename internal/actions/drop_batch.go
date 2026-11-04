package actions

// DROP FIVE A TICK. [opheld5,_] ~dropslot(last_slot) (content player/scripts/drop.rs2) drops the clicked slot
// with no p_delay and no %action_delay, so the engine accepts the next Drop in the same tick, up to the five user
// events it reads a player a tick. A caller that drops one, waits for the pack to lose the slot and drops the
// next spends a round trip (one to two ticks) a slot; DropMany names every slot from one reading of the pack
// (the pack does not close up when a slot empties), sends them five to a tick and confirms once.
//
// A drop runs only where the game allows it (not in the duel arena, the trawler wreck, the games room before the
// warning is answered): a slot the server refused is still in the pack at the confirm and is dropped again one at
// a time, as before.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/gamestate"
)

// dropConfirmTimeout is how long the one confirm waits for every named slot to empty.
const dropConfirmTimeout = 3 * time.Second

// DropBatch is what one DropMany did.
type DropBatch struct {
	// Wanted is the slots asked for; Sent those whose Drop click went out.
	Wanted, Sent int
	// Gone is the slots the pack then showed empty (or holding something else); Retried the slots the
	// one-at-a-time drop emptied; Left the slots that still held their item.
	Gone, Retried, Left int
	// Bursts is the ticks the clicks took (1 for up to five slots).
	Bursts int
	Took   time.Duration
	Err    error
}

// Dropped is how many slots are empty now.
func (b DropBatch) Dropped() int { return b.Gone + b.Retried }

// Log is the one-line account of a drop for a script's log.
func (b DropBatch) Log() string {
	s := fmt.Sprintf("drop: %d of %d slot(s) in %d tick(s) (%d click(s) in one burst, %s)",
		b.Dropped(), b.Wanted, b.Bursts, b.Sent, b.Took.Round(time.Millisecond))
	if b.Retried > 0 {
		s += fmt.Sprintf("; %d on the one-at-a-time retry", b.Retried)
	}
	if b.Left > 0 {
		s += fmt.Sprintf("; %d still in the pack", b.Left)
	}
	if b.Err != nil {
		s += "; " + b.Err.Error()
	}
	return s
}

func (e *Equipment) slotHolds(slot int, id int32) bool {
	for _, it := range e.Inventory() {
		if it.Slot == slot && it.ID == id {
			return true
		}
	}
	return false
}

// DropMany drops each of the given backpack slots (a reading of the pack: ID and Slot) with its Drop option,
// five to a tick, then confirms once that none still holds its item. A slot the pack no longer holds as named is
// skipped.
func (e *Equipment) DropMany(types ObjTypes, slots []gamestate.SlotItem, w Waiter) (out DropBatch) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	comID := e.inventoryComponent()
	if comID == -1 {
		out.Err = ErrNotOnScreen
		return out
	}
	seen := map[int]bool{}
	var todo []gamestate.SlotItem
	for _, it := range slots {
		if seen[it.Slot] {
			continue
		}
		seen[it.Slot] = true
		todo = append(todo, it)
	}
	out.Wanted = len(todo)
	var packets []BurstPacket
	var sent []gamestate.SlotItem
	optionOf := map[int]int{}
	for _, it := range todo {
		if !e.slotHolds(it.Slot, it.ID) {
			out.Gone++ // already gone: nothing to click
			continue
		}
		option := 5
		if types != nil {
			if obj, ok := types.Obj(int(it.ID)); ok {
				if option = OptionIndex(HeldOps(obj.IOps), "Drop"); option == 0 {
					out.Left++
					continue
				}
			}
		}
		it, option := it, option
		optionOf[it.Slot] = option
		sent = append(sent, it)
		packets = append(packets, BurstPacket{Events: 1, Send: func() error { return OpHeld(e.s, option, it.ID, it.Slot, comID) }})
	}
	out.Sent = len(sent)
	allGone := func() bool {
		for _, it := range sent {
			if e.slotHolds(it.Slot, it.ID) {
				return false
			}
		}
		return true
	}
	res := SendBurst(e.s, w, BankMaxPerTick, allGone, packets)
	out.Bursts = res.Bursts
	if res.Err != nil {
		out.Err = res.Err
		return out
	}
	if len(packets) > 0 {
		w.DelayUntil(allGone, dropConfirmTimeout)
	}
	for _, it := range sent {
		if !e.slotHolds(it.Slot, it.ID) {
			out.Gone++
			continue
		}
		// Not gone: drop it again the old way, one click and a wait for the slot.
		if err := OpHeld(e.s, optionOf[it.Slot], it.ID, it.Slot, comID); err == nil && w.DelayUntil(func() bool { return !e.slotHolds(it.Slot, it.ID) }, dropConfirmTimeout) {
			out.Retried++
		} else {
			out.Left++
		}
	}
	return out
}
