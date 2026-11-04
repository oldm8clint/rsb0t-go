package essencerunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/essenceplan"
)

// StackDraw is what DrawNoteStack came to.
type StackDraw struct {
	// Drawn is the notes that arrived in the pack (the stack's count), NoteID their object id.
	Drawn  int
	NoteID int32
	// Held is the Rune essence the bank held when the draw was asked for, Asked how many were asked.
	Held, Asked int
	// Why is the reason when fewer than asked arrived.
	Why  string
	Took time.Duration
}

// DrawNoteStack withdraws up to n of the open bank's Rune essence as ONE bank-note stack: note mode on, the
// withdrawal (Withdraw-All when n covers what the bank holds, else Withdraw-X) and note mode off in one burst,
// the way a hand-off's note draw goes (actions.Bank.Batch), with the one-at-a-time path behind it when the burst
// fell short. A note is one pack slot however many it counts, so a stack of 5,000 crosses to the mule in one
// Offer-All. The pack needs one free slot; the bank is left in item mode.
func DrawNoteStack(d *Deps, bank *actions.Bank, n int) (res StackDraw) {
	started := time.Now()
	defer func() { res.Took = time.Since(started) }()
	res.Asked = n
	// Only one essence item exists, blankrune 1436, and one note of it, cert_blankrune 1437 (content pack obj.pack).
	id, noteID := int32(essenceplan.EssenceID), int32(essenceplan.NotedEssenceID)
	res.Held = int(actions.CountByID(bank.Items(), id))
	res.NoteID = noteID
	if n <= 0 || res.Held <= 0 {
		res.Why = "nothing to draw"
		return res
	}
	if FreeSlots(d) < 1 {
		res.Why = "the pack has no free slot for the note stack"
		return res
	}
	take := min(n, res.Held)
	count := int32(take)
	if take >= res.Held {
		count = 0 // Withdraw-All: no count dialog
	}
	before := int(actions.CountByID(bank.Inventory(), noteID))
	got := func() int { return int(actions.CountByID(bank.Inventory(), noteID)) - before }
	w := Waiter(d)
	r := bank.Batch([]actions.BankOp{actions.NoteModeOp(true), actions.WithdrawAsOp(id, count, noteID), actions.NoteModeOp(false)}, w, actions.BatchOptions{})
	if r.Err != nil || got() < take {
		// The burst fell short: the same draw one step at a time.
		if err := bank.SetNoteMode(true, w); err != nil {
			res.Why = fmt.Sprintf("note mode would not switch on: %v", err)
			_ = bank.SetNoteMode(false, w)
			res.Drawn = got()
			return res
		}
		need := take - got()
		if need > 0 {
			var err error
			if need >= res.Held-got() {
				before2 := got()
				err = bank.WithdrawAll(id)
				if err == nil {
					w.DelayUntil(func() bool { return got() > before2 }, 4*time.Second)
				}
			} else {
				err = bank.WithdrawXAs(id, int32(need), noteID, w)
			}
			if err != nil {
				res.Why = fmt.Sprintf("the withdrawal failed: %v", err)
			}
		}
		_ = bank.SetNoteMode(false, w)
	}
	res.Drawn = got()
	if res.Drawn < take && res.Why == "" {
		res.Why = fmt.Sprintf("only %d of %d notes arrived", res.Drawn, take)
	}
	return res
}
