package actions

import (
	"bytes"
	"testing"
)

// A chisel on a pack of uncut gems: one use per DIFFERENT gem slot, lowest first, so five gems are
// cut in one tick (content cuts with no delay and removes the lowest slot holding the gem).
func TestUseOnHeldManyNamesDistinctTargetSlots(t *testing.T) {
	f, e := equipmentSession()
	const chisel, sapphire = 1755, 1623
	setFullInventory(f.state, 3214,
		[2]int32{chisel, 1}, [2]int32{sapphire, 1}, [2]int32{sapphire, 1}, [2]int32{0, 0}, [2]int32{sapphire, 1},
		[2]int32{1607, 1}, [2]int32{sapphire, 1}, [2]int32{sapphire, 1}, [2]int32{sapphire, 1}, [2]int32{sapphire, 1})
	sent, err := e.UseOnHeldMany(chisel, sapphire, 5)
	if err != nil || sent != 5 {
		t.Fatalf("sent %d, err %v", sent, err)
	}
	if f.sendCount() != 5 {
		t.Fatalf("%d packets, want 5", f.sendCount())
	}
	// The gems sit in slots 1, 2, 4, 6, 7, 8, 9 (slot 3 is empty, slot 5 a cut sapphire).
	want := []int{1, 2, 4, 6, 7}
	ref := newFakeSession()
	for i, slot := range want {
		if err := OpHeldU(ref, sapphire, slot, 3214, chisel, 0, 3214); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(f.sent[i], ref.sent[i]) {
			t.Fatalf("packet %d = %x, want %x (target slot %d, chisel slot 0)", i, f.sent[i], ref.sent[i], slot)
		}
	}
}

func TestUseOnHeldManyFewerTargetsThanAsked(t *testing.T) {
	f, e := equipmentSession()
	setFullInventory(f.state, 3214, [2]int32{1755, 1}, [2]int32{1623, 1}, [2]int32{1623, 1})
	sent, err := e.UseOnHeldMany(1755, 1623, 5)
	if err != nil || sent != 2 || f.sendCount() != 2 {
		t.Fatalf("sent %d (%d packets), err %v; want the two gems held", sent, f.sendCount(), err)
	}
}

func TestUseOnHeldManyRefusals(t *testing.T) {
	f, e := equipmentSession()
	setFullInventory(f.state, 3214, [2]int32{1755, 1}, [2]int32{1607, 1})
	if sent, err := e.UseOnHeldMany(1755, 1623, 5); sent != 0 || err != ErrNoOtherItem {
		t.Fatalf("no gem held: sent %d err %v", sent, err)
	}
	if sent, err := e.UseOnHeldMany(4151, 1623, 5); sent != 0 || err != ErrItemNotFound {
		t.Fatalf("no chisel held: sent %d err %v", sent, err)
	}
	if sent, err := e.UseOnHeldMany(1755, 1607, 0); sent != 0 || err != ErrNoOtherItem {
		t.Fatalf("n = 0: sent %d err %v", sent, err)
	}
	openMainSide(f.state, BankMainModal, 2005)
	if sent, err := e.UseOnHeldMany(1755, 1607, 5); sent != 0 || err != ErrUseWhileBanking {
		t.Fatalf("bank open: sent %d err %v", sent, err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("a refused burst sent %d packets", f.sendCount())
	}
}
