package actions

// Bank.Visit is Bank.Batch with the old one-at-a-time path behind it, for the scripts whose bank visit used to be
// a string of "send one, wait for it to show, send the next" (a deposit, then a tool, then the knife, then the
// logs: one round trip, one to two game ticks, each). Every operation of the visit goes out together, five user
// events a tick, the visit is confirmed ONCE, and what fell short is done again the old way (WithdrawX /
// WithdrawXAs for a count, WithdrawAll for the rest, DepositAllMatching for a deposit), so a refused click costs
// what it cost before and nothing more. The engine and content facts are fastbank.go's.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// visitRetryWait is how long a retried deposit waits for its slot to clear.
const visitRetryWait = 3 * time.Second

// Visit runs ops through Batch and retries what fell short one at a time. The returned batch lists only the
// operations that are STILL short after the retries; Retried says how many the old path landed. opt.Close
// closes the bank after the retries, not before.
func (b *Bank) Visit(ops []BankOp, w Waiter, opt BatchOptions) (out BankBatch, retried int) {
	w = waiterOr(w)
	closeAfter := opt.Close
	opt.Close = false
	out = b.Batch(ops, w, opt)
	if out.Err == nil && len(out.Short) > 0 {
		var still []BankShort
		for _, sh := range out.Short {
			if b.retryShort(sh, out.Pack, w) {
				retried++
				continue
			}
			still = append(still, sh)
		}
		out.Short = still
		if retried > 0 {
			out.Pack, out.Bank = b.Inventory(), b.Items()
		}
	}
	if closeAfter && out.Err == nil {
		if err := b.Close(); err == nil {
			out.Closed = true
		}
	}
	return out, retried
}

// retryShort does one shortfall the old way and reports whether it now holds.
func (b *Bank) retryShort(sh BankShort, pack []gamestate.SlotItem, w Waiter) bool {
	if !b.IsOpen() {
		return false
	}
	switch sh.Op.Kind {
	case BankOpDeposit:
		// ONE attempt, not DepositAllMatching's 32 rounds of two seconds: a kind the server refuses (an unbankable
		// item, a full bank) must cost what the old per-kind loops cost, a click and a short wait.
		id := sh.Op.ID
		if err := b.DepositAll(id); err != nil {
			return false
		}
		return w.DelayUntil(func() bool { return CountByID(b.Inventory(), id) == 0 }, visitRetryWait)
	case BankOpWithdraw:
		if sh.Why == "the bank holds none" || sh.Why == "the pack filled" || IsFull(pack) {
			return false
		}
		lands := sh.Op.landsAs()
		before := CountByID(b.Inventory(), lands)
		rest := int32(sh.Want - sh.Got)
		var err error
		switch {
		case sh.Op.Count == 0:
			if err = b.WithdrawAll(sh.Op.ID); err == nil {
				return w.DelayUntil(func() bool { return CountByID(b.Inventory(), lands) > before }, bankConfirmTimeout)
			}
			return false
		case sh.Op.LandsAs != 0 && sh.Op.LandsAs != sh.Op.ID:
			err = b.WithdrawXAs(sh.Op.ID, rest, sh.Op.LandsAs, w)
		default:
			err = b.WithdrawX(sh.Op.ID, rest, w)
		}
		return err == nil
	}
	return false
}

// VisitLog is the one-line account of a visit for a script's log: how many operations went out in how many
// ticks, and what fell short.
func VisitLog(label string, ops int, r BankBatch, retried int) string {
	s := fmt.Sprintf("bank: %s: %d operation(s), %d user event(s) in %d tick(s), confirmed in %s (%s in all)",
		label, ops, r.Sent, r.Chunks, r.Confirm.Round(time.Millisecond), r.Took.Round(time.Millisecond))
	if retried > 0 {
		s += fmt.Sprintf("; %d landed on the one-at-a-time retry", retried)
	}
	for _, sh := range r.Short {
		s += fmt.Sprintf("; %s short (%d of %d): %s", sh.Op, sh.Got, sh.Want, sh.Why)
	}
	if r.Err != nil {
		s += "; " + r.Err.Error()
	}
	return s
}

// ---- a hand-off's note draw ----

// NoteResolver is the note of an item for DrawAllAsNotes: cfg's un-noted object whose note links back to it, or the
// item itself when it has none (a Withdraw-All of such an item lands as the item even in note mode).
func NoteResolver(cfg *cachecfg.Config) func(id int32) int32 {
	return func(id int32) int32 {
		if cfg == nil {
			return id
		}
		o, ok := cfg.Obj(int(id))
		if !ok || o.Noted() {
			return id
		}
		for _, nid := range cfg.ObjIDs(o.Name) {
			if n, ok := cfg.Obj(nid); ok && n.Noted() && n.CertLink == int(id) {
				return int32(nid)
			}
		}
		return id
	}
}

// RowIDsByName is the open bank's first un-noted row holding each of names (case-insensitive), in the order of
// names, each row once; a name the bank holds none of is skipped.
func (b *Bank) RowIDsByName(types ObjTypes, names []string) []int32 {
	if types == nil {
		return nil
	}
	var out []int32
	seen := map[int32]bool{}
	for _, name := range names {
		for _, it := range b.Items() {
			if it.Count <= 0 || seen[it.ID] {
				continue
			}
			if obj, ok := types.Obj(int(it.ID)); ok && !obj.Noted() && equalFold(strings.TrimSpace(obj.Name), strings.TrimSpace(name)) {
				seen[it.ID] = true
				out = append(out, it.ID)
				break
			}
		}
	}
	return out
}

// DrawAllAsNotes withdraws every one of the given bank rows as bank notes in ONE visit: note mode on, a
// Withdraw-All per row (at most maxRows of them: a note takes one pack slot, and the old loop stopped when the pack
// had none free), note mode off again, five user events a tick and one confirm (twopertick-1012: the hand-off draw
// was a Withdraw-All and a wait per row, between two note-mode switches that each waited on the varp). A row the
// burst did not draw is drawn again the old way, in note mode, one at a time. It reports the batch and how many rows
// were asked for.
func (b *Bank) DrawAllAsNotes(ids []int32, noteOf func(id int32) int32, maxRows int, w Waiter) (BankBatch, int) {
	w = waiterOr(w)
	if maxRows >= 0 && len(ids) > maxRows {
		ids = ids[:maxRows]
	}
	if len(ids) == 0 {
		return BankBatch{}, 0
	}
	ops := []BankOp{NoteModeOp(true)}
	for _, id := range ids {
		lands := id
		if noteOf != nil {
			lands = noteOf(id)
		}
		ops = append(ops, WithdrawAsOp(id, 0, lands))
	}
	ops = append(ops, NoteModeOp(false))
	r := b.Batch(ops, w, BatchOptions{})
	if r.Err != nil {
		_ = b.SetNoteMode(false, w)
		return r, len(ids)
	}
	var again []BankShort
	var still []BankShort
	for _, sh := range r.Short {
		if sh.Op.Kind != BankOpWithdraw || sh.Why == "the bank holds none" || sh.Why == "the pack filled" || IsFull(r.Pack) {
			still = append(still, sh)
			continue
		}
		again = append(again, sh)
	}
	r.Short = still
	if len(again) > 0 {
		if err := b.SetNoteMode(true, w); err == nil {
			for _, sh := range again {
				lands := sh.Op.landsAs()
				before := CountByID(b.Inventory(), lands)
				if b.WithdrawAll(sh.Op.ID) == nil && w.DelayUntil(func() bool { return CountByID(b.Inventory(), lands) > before }, bankConfirmTimeout) {
					continue
				}
				r.Short = append(r.Short, sh)
			}
		} else {
			r.Short = append(r.Short, again...)
		}
		_ = b.SetNoteMode(false, w)
	}
	r.Pack, r.Bank = b.Inventory(), b.Items()
	return r, len(ids)
}

// DepositKinds banks every one of each of ids (Deposit-All per kind) in one burst, five a tick, confirmed once, with the
// one-at-a-time path behind it (twopertick-1012: the runner packages DepositAllExcept loops sent one, waited for the pack to
// lose a slot, and sent the next). It reports which kinds left the pack and which are stuck.
func (b *Bank) DepositKinds(ids []int32, w Waiter) (deposited, stuck []int32, r BankBatch, retried int) {
	if len(ids) == 0 {
		return nil, nil, BankBatch{}, 0
	}
	ops := make([]BankOp, 0, len(ids))
	for _, id := range ids {
		ops = append(ops, DepositAllOp(id))
	}
	r, retried = b.Visit(ops, w, BatchOptions{})
	if r.Err != nil {
		// The visit could not run or was cut short: what is still in the pack is stuck, what left it is banked.
		pack := items(b.s.State(), PlayerPack) // the backpack itself: the bank's side panel is gone when the bank is
		for _, id := range ids {
			if CountByID(pack, id) > 0 {
				stuck = append(stuck, id)
			} else {
				deposited = append(deposited, id)
			}
		}
		return deposited, stuck, r, retried
	}
	short := map[int32]bool{}
	for _, sh := range r.Short {
		if sh.Op.Kind == BankOpDeposit {
			short[sh.Op.ID] = true
		}
	}
	for _, id := range ids {
		if short[id] {
			stuck = append(stuck, id)
		} else {
			deposited = append(deposited, id)
		}
	}
	return deposited, stuck, r, retried
}
