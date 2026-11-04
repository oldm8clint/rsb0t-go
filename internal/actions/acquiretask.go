package actions

import (
	"fmt"
	"time"

	"rsb0t-go/internal/acquiretask"
	"rsb0t-go/internal/gamestate"
)

// AcquireTask is the live execution layer for internal/acquiretask's pure planner: it
// reads how many of an item are already held and how much an open bank/shop can supply,
// asks acquiretask.Plan what to do, and executes each step through the already-landed
// Bank/Shop primitives (InvButton-based withdraw/buy — no new protocol work). It does
// not open a bank or shop, or walk anywhere — a caller (a script, or a cmd/*test tool)
// is responsible for getting to one and opening it first, the same way every other live
// tool in this repo already does its own walking before handing off to Bank/Shop.
type AcquireTask struct {
	bank *Bank // nil, or simply not open, is treated as "no bank stock available"
	shop *Shop // nil, or simply not open, is treated as "no shop stock available"
}

// NewAcquireTask binds the Bank/Shop primitives an AcquireTask composes. Either may be
// nil if that source genuinely isn't available to the caller right now (e.g. a script
// that only ever restocks from a shop never needs to construct a Bank at all) — Ensure
// treats a nil primitive exactly like a closed one, per bankStock/shopStock below.
func NewAcquireTask(bank *Bank, shop *Shop) *AcquireTask {
	return &AcquireTask{bank: bank, shop: shop}
}

// held reads the player's own current count of objID from whichever of the bank's or
// shop's side panel is currently open — both already mirror the live backpack while
// their respective screen is up (Bank.Inventory, Shop.Pack), so this needs no separate
// inventory-component resolution of its own. If neither is open there is no live pack
// view to read from at all; callers open one of the two before calling Ensure, the same
// precondition Bank/Shop's own methods already carry (ErrNotOnScreen otherwise).
func (a *AcquireTask) held(objID int32) int32 {
	var pack []gamestate.SlotItem
	switch {
	case a.bank != nil && a.bank.IsOpen():
		pack = a.bank.Inventory()
	case a.shop != nil && a.shop.IsOpen():
		pack = a.shop.Pack()
	}
	return sumCount(pack, objID)
}

func (a *AcquireTask) bankStock(objID int32) int32 {
	if a.bank == nil || !a.bank.IsOpen() {
		return 0
	}
	return sumCount(a.bank.Items(), objID)
}

func (a *AcquireTask) shopStock(objID int32) int32 {
	if a.shop == nil || !a.shop.IsOpen() {
		return 0
	}
	return sumCount(a.shop.Stock(), objID)
}

func sumCount(items []gamestate.SlotItem, objID int32) int32 {
	var total int32
	for _, it := range items {
		if it.ID == objID {
			total += it.Count
		}
	}
	return total
}

// Ensure resolves need.Count of objID (the obj id the caller already resolved
// need.Item to, e.g. via internal/cachecfg.ObjIDs — this layer works in ids, same as
// Bank/Shop themselves). It reads held/bank/shop counts fresh, asks acquiretask.Plan
// for the steps, and executes bank withdrawals / shop purchases in the plan's own
// order (bank before shop, per Plan's own doc comment).
//
// Returns the plan that was computed (so a caller can inspect a trailing SourceGather
// step, or log what happened even on success) and an error only if an execution step
// itself failed — a SourceGather step in a nil-error result is the honest "can't
// resolve automatically from here" signal Plan documents, not a failure of Ensure.
func (a *AcquireTask) Ensure(objID int32, need acquiretask.Need) ([]acquiretask.Step, error) {
	plan := acquiretask.Plan(a.held(objID), need, a.bankStock(objID), a.shopStock(objID))
	for _, step := range plan {
		switch step.Source {
		case acquiretask.SourceBank:
			if err := a.withdrawExact(objID, step.Count); err != nil {
				return plan, fmt.Errorf("acquiretask: withdraw %d of %s: %w", step.Count, need.Item, err)
			}
		case acquiretask.SourceShop:
			if err := a.shop.Buy(objID, step.Count); err != nil {
				return plan, fmt.Errorf("acquiretask: buy %d of %s: %w", step.Count, need.Item, err)
			}
		case acquiretask.SourceGather:
			// Nothing to execute here — this is Plan's honest "still short" signal;
			// the caller decides what gathering (or failing the task) means.
		}
	}
	return plan, nil
}

// withdrawExact withdraws exactly n of objID from the bank, batched into the same
// Withdraw-10/5/1 clicks Shop.trade already uses for its own Buy-10/5/1 options. The
// bank interface has no arbitrary-amount ("Withdraw-X") wire path landed in bank.go yet
// (see its unused bankWithdrawX constant — that option opens a count-entry dialog this
// project hasn't ported), so this composes what's already proven rather than adding a
// new protocol path, exactly like Shop.trade's own batching.
func (a *AcquireTask) withdrawExact(objID, n int32) error {
	steps := []struct {
		qty int32
		fn  func(int32) error
	}{
		{10, a.bank.WithdrawTen},
		{5, a.bank.WithdrawFive},
		{1, a.bank.WithdrawOne},
	}
	remaining := n
	for remaining > 0 {
		clicked := false
		for _, st := range steps {
			if st.qty > remaining {
				continue
			}
			if err := st.fn(objID); err != nil {
				return err
			}
			remaining -= st.qty
			clicked = true
			break
		}
		if !clicked {
			break // remaining < 1 never happens; guards a future steps table without it
		}
		time.Sleep(150 * time.Millisecond) // let the tick settle before rechecking slots
	}
	return nil
}
