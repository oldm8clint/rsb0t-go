package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestUnequipAndWaitConfirmsTheWornPanel(t *testing.T) {
	f, e := equipmentSession() // leather body 1129 worn in slot 3 of 1688
	f.afterSend = func(f *fakeSession) {
		if sentOpcode(f) == clientprot.INV_BUTTON1 {
			setFullInventory(f.state, 1688, [2]int32{0, 0})
		}
	}
	if err := e.UnequipByName(testObjs, "LEATHER body", bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if e.IsWorn(1129) || f.sendCount() != 1 {
		t.Fatalf("still worn = %v after %d sends", e.IsWorn(1129), f.sendCount())
	}
	if err := e.UnequipAndWait(1129, bankTestWaiter{}); err != nil || f.sendCount() != 1 {
		t.Fatalf("not worn any more: err = %v, sends = %d", err, f.sendCount())
	}

	f2, e2 := equipmentSession()
	if err := e2.UnequipAndWait(1129, bankTestWaiter{}); err != ErrNotConfirmed {
		t.Fatalf("no server reply: err = %v", err)
	}
	if f2.sendCount() != 1 {
		t.Fatalf("sends = %d, want the one Remove", f2.sendCount())
	}
}

func TestEquipItemAndWaitConfirmsTheWornPanel(t *testing.T) {
	f, e := equipmentSession() // bronze dagger 1205 in backpack slot 1
	f.afterSend = func(f *fakeSession) {
		if sentOpcode(f) == clientprot.OPHELD2 {
			setFullInventory(f.state, 1688, [2]int32{0, 0}, [2]int32{0, 0}, [2]int32{0, 0}, [2]int32{1129, 1}, [2]int32{1205, 1})
		}
	}
	if err := e.EquipItemAndWait(testObjs, 1205, bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if !e.IsWorn(1205) || f.sendCount() != 1 {
		t.Fatalf("worn = %v after %d sends", e.IsWorn(1205), f.sendCount())
	}
	if err := e.EquipItemAndWait(testObjs, 1205, bankTestWaiter{}); err != nil || f.sendCount() != 1 {
		t.Fatalf("already worn: err = %v, sends = %d", err, f.sendCount())
	}
	if err := e.EquipItemAndWait(testObjs, 590, bankTestWaiter{}); err != ErrNoSuchAction {
		t.Fatalf("a tinderbox: err = %v", err)
	}
}
