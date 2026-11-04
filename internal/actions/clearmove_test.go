package actions

import (
	"bytes"
	"testing"
)

// The packet is an OPHELDU like any other, naming the item at a slot that does not hold it: the last slot the pack does
// not hold the item in, however full the rest of the pack is.
func TestClearMoveRequestNamesTheItemAtASlotThatDoesNotHoldIt(t *testing.T) {
	const bow, com = int32(859), 3214
	f := newFakeSession()
	holds := func(slot int) bool { return slot == 27 || slot == 26 || slot == 3 } // the bows, some noted and some not
	if err := ClearMoveRequest(f, bow, com, holds); err != nil {
		t.Fatal(err)
	}
	want := newFakeSession()
	_ = OpHeldU(want, bow, 25, com, bow, 25, com)
	if !bytes.Equal(f.lastSent(), want.lastSent()) || f.sendCount() != 1 {
		t.Fatalf("sent % x in %d sends, want one OPHELDU naming slot 25: % x", f.lastSent(), f.sendCount(), want.lastSent())
	}
}

func TestClearMoveRequestNeverNamesASlotThatHoldsTheItem(t *testing.T) {
	for held := 0; held < backpackSlots; held++ {
		held := held
		f := newFakeSession()
		err := ClearMoveRequest(f, 7, 1, func(slot int) bool { return slot != held })
		// Every slot but one holds it: the one that does not is the one named.
		if err != nil {
			t.Fatalf("slot %d free: %v", held, err)
		}
		want := newFakeSession()
		_ = OpHeldU(want, 7, held, 1, 7, held, 1)
		if !bytes.Equal(f.lastSent(), want.lastSent()) {
			t.Errorf("slot %d free: sent % x, want % x", held, f.lastSent(), want.lastSent())
		}
	}
}

// With the item in every slot there is no slot to name wrongly: nothing is sent, because naming a right one would be a
// real use of an item on itself.
func TestClearMoveRequestSendsNothingWhenEverySlotHoldsTheItem(t *testing.T) {
	f := newFakeSession()
	if err := ClearMoveRequest(f, 7, 1, func(int) bool { return true }); err != ErrNoEmptyNaming || f.sendCount() != 0 {
		t.Fatalf("err %v, %d sends", err, f.sendCount())
	}
}
