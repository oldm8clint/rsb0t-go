package actions

import (
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/protocol/clientprot"
)

// depositSideComponents builds a minimal interface tree for a bank's side panel: a
// layout root containing one TYPE_INV component whose iop[0] names "Deposit-1" — the
// shape depositComponent's findInvComponentIn scan looks for.
func depositSideComponents(sideRoot, invComID int) archive.Components {
	return archive.Components{
		sideRoot: {ID: sideRoot, Type: archive.TypeLayer, Children: []int{invComID}},
		invComID: {ID: invComID, Type: archive.TypeInv, Iop: [5]string{"Deposit-1", "", "", "", ""}},
	}
}

func TestBankIsOpen(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	if b.IsOpen() {
		t.Fatal("bank reported open with nothing open")
	}
	openMainSide(s.state, BankMainModal, 5063)
	if !b.IsOpen() {
		t.Fatal("bank should be open once BankMainModal is the main modal")
	}
}

func TestBankItemsReadsWithdrawComponent(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	setFullInventory(s.state, BankWithdraw, [2]int32{995, 5_000_000}, [2]int32{0, 0}, [2]int32{1777, 40})
	items := b.Items()
	if len(items) != 2 || items[0].ID != 995 || items[0].Count != 5_000_000 || items[1].ID != 1777 {
		t.Fatalf("Items() = %+v", items)
	}
}

func TestBankInventoryResolvesDepositComponentOnce(t *testing.T) {
	s := newFakeSession()
	components := depositSideComponents(5063, 5064)
	b := NewBank(s, components)
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, 5064, [2]int32{1779, 28})

	inv := b.Inventory()
	if len(inv) != 1 || inv[0].ID != 1779 {
		t.Fatalf("Inventory() = %+v", inv)
	}
	// sideComp should now be cached; changing the archive out from under it must not
	// matter — depositComponent() must not re-resolve while the bank stays open.
	if b.sideComp != 5064 {
		t.Fatalf("sideComp cache = %d, want 5064", b.sideComp)
	}
}

func TestBankInventoryEmptyWithoutASideModal(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, depositSideComponents(5063, 5064))
	if inv := b.Inventory(); inv != nil {
		t.Fatalf("Inventory() with no side modal open = %+v, want nil", inv)
	}
}

func TestBankWithdrawVariants(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	if err := b.WithdrawOne(995); err != ErrNotOnScreen {
		t.Fatalf("WithdrawOne while closed: err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, BankWithdraw, [2]int32{995, 5_000_000})

	if err := b.WithdrawFive(1779); err != ErrItemNotFound {
		t.Fatalf("WithdrawFive(missing): err = %v, want ErrItemNotFound", err)
	}

	cases := []struct {
		call   func() error
		option byte
	}{
		{func() error { return b.WithdrawOne(995) }, clientprot.INV_BUTTON1},
		{func() error { return b.WithdrawFive(995) }, clientprot.INV_BUTTON2},
		{func() error { return b.WithdrawTen(995) }, clientprot.INV_BUTTON3},
		{func() error { return b.WithdrawAll(995) }, clientprot.INV_BUTTON4},
	}
	for _, tc := range cases {
		if err := tc.call(); err != nil {
			t.Fatalf("withdraw call: %v", err)
		}
		want := []byte{tc.option, 0x03, 0xE3, 0, 0, 0x15, 0x06} // 995, slot0, BankWithdraw=5382=0x1506
		if got := s.lastSent(); string(got) != string(want) {
			t.Fatalf("withdraw wire bytes = % x, want % x", got, want)
		}
	}
}

func TestBankDepositVariants(t *testing.T) {
	s := newFakeSession()
	components := depositSideComponents(5063, 5064)
	b := NewBank(s, components)
	if err := b.DepositOne(995); err != ErrNotOnScreen {
		t.Fatalf("DepositOne while closed: err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, 5064, [2]int32{995, 12})

	if err := b.DepositOne(1779); err != ErrItemNotFound {
		t.Fatalf("DepositOne(missing): err = %v, want ErrItemNotFound", err)
	}
	if err := b.DepositAll(995); err != nil {
		t.Fatalf("DepositAll: %v", err)
	}
	want := []byte{clientprot.INV_BUTTON4, 0x03, 0xE3, 0, 0, 0x13, 0xC8} // 995, slot0, comID 5064=0x13C8
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("DepositAll = % x, want % x", got, want)
	}
}

func TestBankDepositWithoutResolvableComponent(t *testing.T) {
	s := newFakeSession()
	// Components with no TYPE_INV descendant at all: depositComponent must return -1.
	components := archive.Components{5063: {ID: 5063, Type: archive.TypeLayer}}
	b := NewBank(s, components)
	openMainSide(s.state, BankMainModal, 5063)
	if err := b.DepositOne(995); err != ErrNotOnScreen {
		t.Fatalf("DepositOne with no deposit component: err = %v, want ErrNotOnScreen", err)
	}
}

func TestBankClose(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	openMainSide(s.state, BankMainModal, 5063)
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := s.lastSent(); len(got) != 1 || got[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("Close() = % x, want CLOSE_MODAL", got)
	}
	if b.IsOpen() {
		t.Fatal("bank still reports open after Close")
	}
}
