package actions

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// bankTestWaiter evaluates each condition once instead of sleeping: the fake session's
// afterSend has already applied the server's reply by the time a wait starts.
type bankTestWaiter struct{}

func (bankTestWaiter) Delay(time.Duration) bool                          { return true }
func (bankTestWaiter) DelayUntil(cond func() bool, _ time.Duration) bool { return cond() }
func (bankTestWaiter) DelayTicks(int) bool                               { return true }

// openBankSession opens a bank with side panel 5063 -> 5064.
func openBankSession() (*fakeSession, *Bank) {
	f := newFakeSession()
	b := NewBank(f, depositSideComponents(5063, 5064))
	openMainSide(f.state, BankMainModal, 5063)
	return f, b
}

func sentOpcode(f *fakeSession) int {
	if last := f.lastSent(); len(last) > 0 {
		return int(last[0])
	}
	return -1
}

func setVarp(s *gamestate.State, id, value int) {
	s.Handle(serverprot.VARP_SMALL, payload(func(p *packet.Packet) { p.P2(uint16(id)); p.P1(byte(value)) }))
}

func TestSetNoteModeClicksAndConfirmsByVarp(t *testing.T) {
	f, b := openBankSession()
	f.afterSend = func(f *fakeSession) {
		last := f.lastSent()
		if int(last[0]) != clientprot.IF_BUTTON {
			return
		}
		com := int(last[1])<<8 | int(last[2])
		if com == BankNoteButton {
			setVarp(f.state, BankCertVarp, 1)
		} else if com == BankItemButton {
			setVarp(f.state, BankCertVarp, 0)
		}
	}
	if err := b.SetNoteMode(true, bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if !b.NoteMode() || f.sendCount() != 1 {
		t.Fatalf("note mode = %v after %d sends", b.NoteMode(), f.sendCount())
	}
	if err := b.SetNoteMode(true, bankTestWaiter{}); err != nil || f.sendCount() != 1 {
		t.Fatalf("already in note mode: err = %v, sends = %d", err, f.sendCount())
	}
	if err := b.SetNoteMode(false, bankTestWaiter{}); err != nil || b.NoteMode() {
		t.Fatalf("back to items: err = %v, note mode = %v", err, b.NoteMode())
	}

	f.afterSend = nil
	if err := b.SetNoteMode(true, bankTestWaiter{}); err != ErrNotConfirmed {
		t.Fatalf("no varp reply: err = %v", err)
	}
	closeModal(f.state)
	if err := b.SetNoteMode(true, bankTestWaiter{}); err != ErrNotOnScreen {
		t.Fatalf("bank closed: err = %v", err)
	}
}

func TestWithdrawXUsesTheCountDialog(t *testing.T) {
	f, b := openBankSession()
	setFullInventory(f.state, BankWithdraw, [2]int32{995, 100}, [2]int32{1511, 300})
	setFullInventory(f.state, 5064, [2]int32{590, 1})
	f.afterSend = func(f *fakeSession) {
		switch sentOpcode(f) {
		case clientprot.INV_BUTTON5:
			openCountDialog(f.state)
		case clientprot.RESUME_P_COUNTDIALOG:
			last := f.lastSent()
			n := int32(last[1])<<24 | int32(last[2])<<16 | int32(last[3])<<8 | int32(last[4])
			setFullInventory(f.state, 5064, [2]int32{590, 1}, [2]int32{1512, n})
		}
	}
	// Noted logs land as 1512.
	if err := b.WithdrawXAs(1511, 27, 1512, bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 2 {
		t.Fatalf("sends = %d, want Withdraw X then the count", f.sendCount())
	}
	first := f.sent[0]
	if int(first[0]) != clientprot.INV_BUTTON5 || int(first[3])<<8|int(first[4]) != 1 || int(first[5])<<8|int(first[6]) != BankWithdraw {
		t.Fatalf("Withdraw X packet %x, want slot 1 of the bank inventory", first)
	}
	if got := CountByID(b.Inventory(), 1512); got != 27 {
		t.Fatalf("backpack has %d noted logs", got)
	}
}

func TestWithdrawXClampsToTheBankAndUsesFixedButtons(t *testing.T) {
	f, b := openBankSession()
	setFullInventory(f.state, BankWithdraw, [2]int32{995, 5})
	f.afterSend = func(f *fakeSession) {
		if sentOpcode(f) == clientprot.INV_BUTTON2 {
			setFullInventory(f.state, 5064, [2]int32{995, 5})
		}
	}
	// Asking for 50 when the bank holds 5 takes 5, which is the Withdraw 5 button.
	if err := b.WithdrawX(995, 50, bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 1 || sentOpcode(f) != clientprot.INV_BUTTON2 {
		t.Fatalf("sent %d packets, last opcode %d; want one Withdraw 5", f.sendCount(), sentOpcode(f))
	}

	f.afterSend = nil
	if err := b.WithdrawX(995, 0, bankTestWaiter{}); err != nil || f.sendCount() != 1 {
		t.Fatalf("n = 0 must be a no-op: err = %v", err)
	}
	if err := b.WithdrawX(4151, 3, bankTestWaiter{}); err != ErrItemNotFound {
		t.Fatalf("not banked: err = %v", err)
	}
	if err := b.WithdrawX(995, 3, bankTestWaiter{}); err != ErrNotConfirmed {
		t.Fatalf("no count dialog: err = %v", err)
	}
}

func TestDepositInventoryEmptiesEachStack(t *testing.T) {
	f, b := openBankSession()
	setFullInventory(f.state, 5064, [2]int32{590, 1}, [2]int32{0, 0}, [2]int32{995, 1000}, [2]int32{1511, 1})
	f.afterSend = func(f *fakeSession) {
		last := f.lastSent()
		if int(last[0]) != clientprot.INV_BUTTON4 {
			return
		}
		slot := int(last[3])<<8 | int(last[4])
		// Remove that slot from the side backpack.
		var rows [][2]int32
		for i := 0; i < 4; i++ {
			rows = append(rows, [2]int32{0, 0})
		}
		for _, it := range b.Inventory() {
			if it.Slot != slot {
				rows[it.Slot] = [2]int32{it.ID, it.Count}
			}
		}
		setFullInventory(f.state, 5064, rows...)
	}
	keepTinderbox := func(it gamestate.SlotItem) bool { return it.ID != 590 }
	if err := b.DepositAllMatching(keepTinderbox, bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if inv := b.Inventory(); len(inv) != 1 || inv[0].ID != 590 || f.sendCount() != 2 {
		t.Fatalf("after depositing all but the tinderbox: %+v, %d sends", inv, f.sendCount())
	}
	if err := b.DepositInventory(bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if inv := b.Inventory(); len(inv) != 0 || f.sendCount() != 3 {
		t.Fatalf("after DepositInventory: %+v, %d sends", inv, f.sendCount())
	}
}

func TestDepositAllMatchingStopsAtTheRoundLimit(t *testing.T) {
	f, b := openBankSession()
	setFullInventory(f.state, 5064, [2]int32{590, 1})
	// The server never removes it.
	if err := b.DepositInventory(bankTestWaiter{}); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != bankDepositGuard {
		t.Fatalf("sends = %d, want the %d-round limit", f.sendCount(), bankDepositGuard)
	}
}
