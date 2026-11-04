package actions

import (
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
)

// Bank interface ids for this content build, pinned once via internal/archive's dynamic
// resolution (see cmd/archivetest) exactly the way trade's ids were pinned by hand in
// rsb0t-main originally — 5292 is a long-documented classic-RS bank interface id,
// which this session's archive parse independently reproduced from the live engine.
const (
	BankMainModal = 5292 // opens via IF_OPENMAIN_SIDE.main when a booth is used
	BankWithdraw  = 5382 // trademain-style inv component; option1..5 = Withdraw 1/5/10/All/X

	bankWithdrawOne  = 1
	bankWithdrawFive = 2
	bankWithdrawTen  = 3
	bankWithdrawAll  = 4
	bankWithdrawX    = 5
)

// Bank is the Banking/Bank.* surface Phase 2 needs, bound to a session. Unlike Trade,
// the deposit-side component isn't a fixed literal — IF_OPENMAIN_SIDE's side id varies
// with which sidebar layout is active, so it's read from live state and resolved
// against the parsed interface archive each time the bank opens (cheap: a cached map
// lookup, not a re-parse).
type Bank struct {
	s        Session
	archive  archive.Components
	sideComp int // resolved lazily once IF_OPENMAIN_SIDE's side id is known
}

// NewBank binds a Bank to a session using an already-parsed interface archive (see
// archive.ParseInterfaces via cmd/archivetest's fetch-and-parse sequence, which a real
// client wires in once at startup — the archive doesn't change while the server is up).
func NewBank(s Session, components archive.Components) *Bank {
	return &Bank{s: s, archive: components, sideComp: -1}
}

func (b *Bank) IsOpen() bool {
	main, _, _ := b.s.State().Modals()
	return main == BankMainModal
}

// depositComponent resolves the deposit-from-inventory component the same way
// ClientAdapter does: findInvComponentIn on the current sideModalId, looking for an
// iop containing "deposit". Cached per bank session since sideModalId doesn't change
// while the bank stays open.
func (b *Bank) depositComponent() int {
	_, side, _ := b.s.State().Modals()
	if side == -1 {
		return -1
	}
	if b.sideComp != -1 {
		return b.sideComp
	}
	b.sideComp = archive.FindInvComponentIn(b.archive, side, func(c *archive.Component) bool {
		return c.Iop[0] != "" && containsFold(c.Iop[0], "deposit")
	})
	if b.sideComp >= 0 {
		lastBankSide.Store(int64(b.sideComp)) // MarkBankOpen (fastbank.go) watches it
	}
	return b.sideComp
}

func containsFold(s, substr string) bool {
	sl, subl := len(s), len(substr)
	if subl == 0 {
		return true
	}
	for i := 0; i+subl <= sl; i++ {
		if equalFold(s[i:i+subl], substr) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// Items returns the bank's own contents (what BankWithdraw currently holds).
func (b *Bank) Items() []gamestate.SlotItem { return items(b.s.State(), BankWithdraw) }

// Inventory returns what's shown in the deposit-side panel while the bank is open —
// this is the account's own inventory, sourced from the side modal, not a separate read.
func (b *Bank) Inventory() []gamestate.SlotItem {
	comID := b.depositComponent()
	if comID == -1 {
		return nil
	}
	return items(b.s.State(), comID)
}

// InventorySnapshot identifies the live deposit component and its packet generation.
func (b *Bank) InventorySnapshot() (int, *gamestate.Inventory) {
	id := b.depositComponent()
	if id < 0 {
		return id, nil
	}
	return id, b.s.State().Inventory(id)
}

// WithdrawOne/Five/Ten/All withdraw from a bank slot holding objID, ported from the
// bank interface's option1..4 (see content/scripts/interface_bank/interfaces/bank_main.if).
func (b *Bank) withdraw(objID int32, option int) error {
	if !b.IsOpen() {
		return ErrNotOnScreen
	}
	for _, it := range b.Items() {
		if it.ID == objID {
			return InvButton(b.s, option, it.ID, it.Slot, BankWithdraw)
		}
	}
	return ErrItemNotFound
}

func (b *Bank) WithdrawOne(objID int32) error  { return b.withdraw(objID, bankWithdrawOne) }
func (b *Bank) WithdrawFive(objID int32) error { return b.withdraw(objID, bankWithdrawFive) }
func (b *Bank) WithdrawTen(objID int32) error  { return b.withdraw(objID, bankWithdrawTen) }
func (b *Bank) WithdrawAll(objID int32) error  { return b.withdraw(objID, bankWithdrawAll) }

// DepositOne/All deposit from the player's own inventory (the side panel while banking)
// into the bank, via the same option1(deposit-1)/option4(deposit-all) convention.
func (b *Bank) deposit(objID int32, option int) error {
	if !b.IsOpen() {
		return ErrNotOnScreen
	}
	comID := b.depositComponent()
	if comID == -1 {
		return ErrNotOnScreen
	}
	for _, it := range b.Inventory() {
		if it.ID == objID {
			return InvButton(b.s, option, it.ID, it.Slot, comID)
		}
	}
	return ErrItemNotFound
}

func (b *Bank) DepositOne(objID int32) error { return b.deposit(objID, bankWithdrawOne) }
func (b *Bank) DepositAll(objID int32) error { return b.deposit(objID, bankWithdrawAll) }

// Close ports the generic close-modal path; the bank has no dedicated close button
// beyond the usual interface close.
func (b *Bank) Close() error { return CloseModal(b.s) }
