package lootchestfisherrunner

// Emptying the account's own bank into the mule, and counting the keys a hand-out really
// delivered. Both are I/O halves of decisions that live in lootchestfisherplan.
//
// WHY THE BANK HAS TO BE DRAINED AT ALL, since it is not obvious from the loop: the chest's
// haul never goes to the mule from the pack. A trip ends at the FALADOR bank
// (bankLootStep), because the walk home crosses water and a death with a full pack loses the
// lot — so by the time the account is standing in front of the mule, everything it collected
// is in a bank 150 tiles away. The bank is shared across the whole game, which is what makes
// this work: the same rows come back out at Catherby. Users asked for it directly
// (2026-09-21): "make sure it hands over everything in the bank besides 500gp and
// untradeables once it has looted all keys".
//
// UNTRADEABLES ARE NOT A HAND-WRITTEN LIST. donorplan.Untradeable answers from
// untradeable_gen.go, which tools/gen-untradeable.py generates from Lost City content's own
// `tradeable=no` flag (963 obj ids). It is reached through donorplan.Rules.Stays, which
// fisherplan.MuleRows already applies, so this file gets content's real answer for free and
// adds no list of its own. cachecfg.Obj carries no tradeability field, so the cache cannot
// answer this and the generated table is the authority — that is worth stating because
// "resolve it from the cache" is the instinct and it does not work here.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
)

// BankItemRows is an open bank as the named rows donorplan and fisherplan reason about.
//
// One row per bank row, carrying the obj id as well as the name, because donorplan.Stays
// needs the ID to consult content's untradeable table and a name alone cannot answer it.
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	if bank == nil {
		return nil
	}
	items := bank.Items()
	out := make([]donorplan.ItemRow, 0, len(items))
	for _, it := range items {
		name := ""
		if d.Config != nil {
			if obj, ok := d.Config.Obj(int(it.ID)); ok {
				name = obj.Name
			}
		}
		if strings.TrimSpace(name) == "" {
			// A row this client cannot name is a row it must not offer: the trade screen
			// would stall on it exactly as an untradeable does, and there is no way to check
			// it against the keep list or the untradeable table without a name.
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: c})
	}
	return out
}

// MuleBankRows is everything in the bank that is destined for the mule: the haul, and nothing
// else.
//
// keep is this script's own list on top of the ones fisherplan already enforces — the fishing
// tools and fisherplan.NeverHandOver, which is where "Crystal key" lives. Passing the crystal
// key through that list rather than filtering it here is deliberate: the key's NOTED form
// carries the identical display name, so one name-based rule covers both, where an id-based
// one would have to know two ids.
func MuleBankRows(d *Deps, bank *actions.Bank, keep []string) []donorplan.ItemRow {
	return fisherplan.MuleRows(BankItemRows(d, bank), keep)
}

// drawSettle is how long to let one withdrawal land before reading the pack again. The bank's
// own side panel updates a tick behind the click, and reading through it immediately reports
// the row unchanged.
const drawSettle = 600 * time.Millisecond

// DrawHaulForMule withdraws as much of rows as the pack will hold, AS BANK NOTES, and reports
// how many pack slots it actually filled.
//
// Notes, for the same reason ProgressiveFisher's own hand-off draws notes (see fisher.go's
// comment on loadHandoff): raw fish and most chest loot do not stack, so an items-mode draw
// comes out at one slot per unit and a bank of 300 rows becomes a dozen round trips. A note
// stacks, so each ROW costs one slot however many are in it.
//
// It stops on the first full pack rather than on the end of rows, and that is the whole
// contract — the caller hands over what it drew and comes back for the rest, which is what
// makes this safe to call repeatedly against a bank far larger than a backpack.
//
// Reports slots filled, measured from the pack, not rows attempted: a withdrawal that silently
// does nothing (a row the bank no longer holds, a note mode that would not switch) must read as
// zero progress so the caller's own give-up counter can see it.
func DrawHaulForMule(d *Deps, bank *actions.Bank, rows []donorplan.ItemRow) int {
	if bank == nil || len(rows) == 0 {
		return 0
	}
	before := len(packItems(d))
	if err := bank.SetNoteMode(true, Waiter(d)); err != nil {
		// Not fatal: an items-mode draw still moves the haul, just fewer rows per trip. Said
		// out loud because it changes how many trips the drain takes, which an operator
		// watching the log would otherwise have to infer.
		d.logf("lootchest: could not switch the bank to notes for the hand-over (%v) — drawing items instead", err)
	}
	for _, row := range rows {
		if PackFree(d) <= 0 {
			break
		}
		if row.ID <= 0 {
			continue
		}
		if err := bank.WithdrawAll(int32(row.ID)); err != nil {
			d.logf("lootchest: withdrawing %q for the mule: %v", row.Name, err)
			continue
		}
		d.Delay(drawSettle)
	}
	if err := bank.SetNoteMode(false, Waiter(d)); err != nil {
		d.logf("lootchest: could not switch the bank back off notes: %v", err)
	}
	after := len(packItems(d))
	if after <= before {
		return 0
	}
	return after - before
}

// KeysHeld is every crystal key in the pack, ORDINARY AND NOTED together.
//
// It exists to be the receipt for a key order, and the two forms have to be counted together
// for that to work. donorrunner.Await proves a hand-out landed by watching this account's pack
// gain the item, but it counts only UN-NOTED ids (donorrunner/handout.go's own unnotedIDs) —
// so the moment an order is large enough for the mule to send bank notes instead, which is
// every order above 28 (mulerunner's notesAbove), Await's receipt can never be satisfied and
// it reports "the trade closed with nothing arriving" while the notes sit in the pack.
//
// Reading this before and after the ask, and believing the delta rather than the Result field,
// is the fix, and it is the rule docs/trustworthy_harnesses.md states for exactly this class
// of mistake: read the state twice, and assert on what was gained.
func KeysHeld(d *Deps, ids KeyIDs) int {
	return KeysInPack(d, ids) + NotedKeysInPack(d, ids)
}

// BankHaulRowCount is how many bank rows are waiting for the mule, for a caller that wants the
// number without holding the rows.
func BankHaulRowCount(d *Deps, bank *actions.Bank, keep []string) int {
	return len(MuleBankRows(d, bank, keep))
}

// packSlotItems is the pack's own rows, for callers outside this package that need them in the
// donorplan shape (the trade-screen plan).
func packSlotItems(d *Deps) []gamestate.SlotItem { return packItems(d) }

// PackSlotRows is the pack as one donorplan row per slot — the shape MuleLoad/MuleHaul take.
// One row per SLOT, never merged: a merged row would count 27 unstackable fish as one slot and
// the trade-screen plan would under-fill every offer.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	items := packSlotItems(d)
	out := make([]donorplan.ItemRow, 0, len(items))
	for _, it := range items {
		name := ""
		if d.Config != nil {
			if obj, ok := d.Config.Obj(int(it.ID)); ok {
				name = obj.Name
			}
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: c})
	}
	return out
}
