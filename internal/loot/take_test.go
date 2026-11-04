package loot

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// packOf is a pack reading with the given contents, one slot per id.
func packOf(c Counts) *gamestate.Inventory {
	inv := &gamestate.Inventory{}
	for id, n := range c {
		inv.Slots = append(inv.Slots, gamestate.Item{ID: int32(id), Count: int32(n)})
	}
	return inv
}

func noDelay(time.Duration) bool { return true }

// TestTakeGainedIsWhatArrivedLessAnEventsOwnItem: a Genie's lamp waiting in the pack while the
// account takes a bone is not the bone's, and must not end the wait for it either.
func TestTakeGainedIsWhatArrivedLessAnEventsOwnItem(t *testing.T) {
	const bones = 526
	reads := 0
	read := func() *gamestate.Inventory {
		reads++
		if reads == 1 {
			return packOf(Counts{objLamp: 1, objStrangeBox: 1}) // only the event's items so far
		}
		return packOf(Counts{objLamp: 1, objStrangeBox: 1, bones: 1})
	}
	got, discarded := TakeGained(read, &Reading{Counts: Counts{}}, noDelay, time.Second, nil)
	if discarded || !reflect.DeepEqual(got, Counts{bones: 1}) {
		t.Fatalf("TakeGained = %v, discarded %v; want the Bones alone (the lamp and the box are an event's)", got, discarded)
	}
	if reads < 2 {
		t.Fatalf("read the pack %d time(s): an event's own item ended the wait before the bone arrived", reads)
	}
}

// TestTakeGainedThrowsAwayATakeARandomEventRanInside is the Cow card's Feather x5,014: the pack is
// read, a Maze pays out while the account is held, and the take that follows would have claimed
// the lot. Nothing is attributed, and the answer says so, so a caller can tell it from "nothing
// arrived".
func TestTakeGainedThrowsAwayATakeARandomEventRanInside(t *testing.T) {
	const bones, feather = 526, 314
	asked := 0
	before := &Reading{Counts: Counts{}, Disturbed: func() bool { asked++; return true }}
	read := func() *gamestate.Inventory { return packOf(Counts{bones: 1, feather: 80}) }
	got, discarded := TakeGained(read, before, noDelay, 0, nil)
	if !discarded || got != nil {
		t.Fatalf("TakeGained = %v, discarded %v; want nothing attributed and the discard reported", got, discarded)
	}
	if asked != 1 {
		t.Fatalf("Disturbed asked %d times, want once", asked)
	}
	// The same take with no event in it is what it always was.
	before.Disturbed = func() bool { return false }
	if got, discarded := TakeGained(read, before, noDelay, 0, nil); discarded || !reflect.DeepEqual(got, Counts{bones: 1, feather: 80}) {
		t.Fatalf("an undisturbed take = %v, discarded %v", got, discarded)
	}
	// A take that gained nothing is not a discard, whatever ran: there was nothing to throw away.
	before.Disturbed = func() bool { return true }
	if got, discarded := TakeGained(func() *gamestate.Inventory { return packOf(Counts{}) }, before, noDelay, 0, nil); discarded || len(got) != 0 {
		t.Fatalf("an empty take = %v, discarded %v", got, discarded)
	}
}

// TestTakeGainedAsksAfterTheWait: an event that begins while the pickup is still arriving is the
// one that matters, so the question is asked once the wait is over, not before it.
func TestTakeGainedAsksAfterTheWait(t *testing.T) {
	reads := 0
	read := func() *gamestate.Inventory { reads++; return packOf(Counts{526: 1}) }
	before := &Reading{Counts: Counts{}, Disturbed: func() bool { return reads > 0 }}
	if _, discarded := TakeGained(read, before, noDelay, 0, nil); !discarded {
		t.Fatal("an event that began during the wait was not seen: Disturbed ran before the pack was read")
	}
}

// TestTakeGainedWithNothingToMeasureAgainst: a nil reading is a script that records no loot.
func TestTakeGainedWithNothingToMeasureAgainst(t *testing.T) {
	got, discarded := TakeGained(func() *gamestate.Inventory { t.Fatal("read the pack with nothing to measure against"); return nil }, nil, noDelay, 0, nil)
	if got != nil || discarded {
		t.Fatalf("TakeGained(nil) = %v, %v", got, discarded)
	}
}

// TestTakeGainedSkipsWhatTheCallerNames: the caller's own skip still applies beside the event items.
func TestTakeGainedSkipsWhatTheCallerNames(t *testing.T) {
	read := func() *gamestate.Inventory { return packOf(Counts{526: 1, 1891: 3}) }
	got, _ := TakeGained(read, &Reading{Counts: Counts{}}, noDelay, 0, func(id int) bool { return id == 1891 })
	if !reflect.DeepEqual(got, Counts{526: 1}) {
		t.Fatalf("TakeGained = %v, want the Bones alone", got)
	}
}
