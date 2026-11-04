package actions

import (
	"time"

	"rsb0t-go/internal/gamestate"
)

// The rest of upstream's Bank.ts surface: withdrawX/withdrawXById, setNoteMode,
// depositInventory and depositAllMatching. bank.go's one-click withdraw and deposit stay as
// they are; everything here confirms its own result through a Waiter.
//
// Ids are from content 289: bank_main:com_93 (5386) and com_94 (5387) are the Note and Item
// buttons (`[if_button,bank_main:com_93] %bankcert = 1;` in interface_bank/scripts/bank.rs2),
// and varp 115 is bankcert (pack/varp.pack). The bank inventory's option5 is "Withdraw X" and
// option4 "Withdraw All"; the side backpack's option4 is "Deposit All" (bank_main.if,
// bank_side.if).
const (
	BankNoteButton = 5386 // bank_main:com_93, withdraw as notes
	BankItemButton = 5387 // bank_main:com_94, withdraw as items
	BankCertVarp   = 115  // bankcert: 1 while note mode is on

	bankDepositAll = 4

	bankConfirmTimeout    = 4 * time.Second
	bankCountDialogWait   = 3 * time.Second
	bankDepositSlotWait   = 2 * time.Second
	bankSideReadyWait     = 1200 * time.Millisecond
	bankDepositGuard      = 32
	bankNoteConfirmWindow = 3 * time.Second
)

// NoteMode reports whether withdrawals come out as notes (varp bankcert is 1).
func (b *Bank) NoteMode() bool {
	return b.s.State().Varp(BankCertVarp) == 1
}

// SetNoteMode ports Bank.setNoteMode: the Note or Item button, confirmed by the bankcert varp.
// Opening a bank resets it to Item, so set it after opening. When the varp is already known to
// hold the wanted mode nothing is sent.
func (b *Bank) SetNoteMode(on bool, w Waiter) error {
	w = waiterOr(w)
	if !b.IsOpen() {
		return ErrNotOnScreen
	}
	want := 0
	button := BankItemButton
	if on {
		want, button = 1, BankNoteButton
	}
	if v, known := b.s.State().VarpKnown(BankCertVarp); known && v == want {
		return nil
	}
	if err := IfButton(b.s, button); err != nil {
		return err
	}
	if !w.DelayUntil(func() bool { return b.s.State().Varp(BankCertVarp) == want }, bankNoteConfirmWindow) {
		return ErrNotConfirmed
	}
	return nil
}

// WithdrawX ports Bank.withdrawX: take up to n of objID from the bank and wait until they are in
// the backpack. Exactly 1, 5 or 10 use those buttons (no count dialog, as upstream); any other
// amount uses Withdraw X and answers the count dialog. A request larger than the bank holds takes
// what is there. The wait also ends when the backpack fills after some arrived.
func (b *Bank) WithdrawX(objID, n int32, w Waiter) error {
	return b.withdrawX(objID, n, objID, true, w)
}

// WithdrawXAs ports Bank.withdrawXById with landsAsId: in note mode the backpack receives the
// noted object, whose id differs, so the confirmation counts landsAsID. It always uses
// Withdraw X, as upstream does.
func (b *Bank) WithdrawXAs(objID, n, landsAsID int32, w Waiter) error {
	return b.withdrawX(objID, n, landsAsID, false, w)
}

func (b *Bank) withdrawX(objID, n, landsAsID int32, fixedButtons bool, w Waiter) error {
	w = waiterOr(w)
	if n <= 0 {
		return nil
	}
	if !b.IsOpen() {
		return ErrNotOnScreen
	}
	if b.depositComponent() == -1 {
		return ErrNotOnScreen
	}
	slot, available, found := slotOf(b.Items(), objID)
	if !found || available <= 0 {
		return ErrItemNotFound
	}
	take := n
	if available < take {
		take = available
	}
	held := func() int32 { return CountByID(b.Inventory(), landsAsID) }
	before := held()
	target := before + take
	arrived := func() bool {
		now := held()
		return now >= target || (now > before && IsFull(b.Inventory()))
	}

	if fixedButtons {
		option := 0
		switch take {
		case 1:
			option = bankWithdrawOne
		case 5:
			option = bankWithdrawFive
		case 10:
			option = bankWithdrawTen
		}
		if option != 0 {
			if err := InvButton(b.s, option, objID, slot, BankWithdraw); err != nil {
				return err
			}
			if !w.DelayUntil(arrived, bankConfirmTimeout) {
				return ErrNotConfirmed
			}
			return nil
		}
	}

	if err := InvButton(b.s, bankWithdrawX, objID, slot, BankWithdraw); err != nil {
		return err
	}
	if !w.DelayUntil(func() bool { return b.s.State().CountDialogOpen() }, bankCountDialogWait) {
		return ErrNotConfirmed
	}
	if err := AnswerCountDialog(b.s, take); err != nil {
		return err
	}
	if !w.DelayUntil(arrived, bankConfirmTimeout) {
		return ErrNotConfirmed
	}
	return nil
}

// DepositInventory ports Bank.depositInventory: Deposit All on every backpack stack.
func (b *Bank) DepositInventory(w Waiter) error {
	return b.DepositAllMatching(func(gamestate.SlotItem) bool { return true }, w)
}

// DepositAllMatching ports Bank.depositAllMatching: Deposit All on each backpack stack match
// accepts, one at a time, waiting for its slot to empty before the next (at most 32 rounds, as
// upstream). An item that stays put is not retried forever: the round limit ends the loop. It
// returns nil once nothing left matches.
func (b *Bank) DepositAllMatching(match func(gamestate.SlotItem) bool, w Waiter) error {
	w = waiterOr(w)
	for guard := 0; guard < bankDepositGuard; guard++ {
		if !b.IsOpen() {
			return ErrNotOnScreen
		}
		comID := b.depositComponent()
		if comID == -1 {
			return ErrNotOnScreen
		}
		inv := b.Inventory()
		if len(inv) == 0 {
			// The side backpack can arrive a moment after the bank opens.
			w.DelayUntil(func() bool { return len(b.Inventory()) > 0 || !b.IsOpen() }, bankSideReadyWait)
			inv = b.Inventory()
		}
		var pick gamestate.SlotItem
		found := false
		for _, it := range inv {
			if match(it) {
				pick, found = it, true
				break
			}
		}
		if !found {
			return nil
		}
		if err := InvButton(b.s, bankDepositAll, pick.ID, pick.Slot, comID); err != nil {
			return err
		}
		w.DelayUntil(func() bool {
			for _, it := range b.Inventory() {
				if it.Slot == pick.Slot && it.ID == pick.ID {
					return false
				}
			}
			return true
		}, bankDepositSlotWait)
	}
	return nil
}
