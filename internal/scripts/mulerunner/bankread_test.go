package mulerunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// TestBankReadIsNilSafe pins the contract Deps.OnBankRead is added under: it is a REPORTING
// hook, so a mule running without one (every standalone tool, every existing caller, and any
// fleet with no bank directory) must behave exactly as this package always has.
//
// The hook exists for the fleet owner's 2026-09-22 report — "woodcutters are asking the mule for
// axes they dont even have instock ... they should beable to know whats in their bank to
// request" — and a donor cannot read a mule's bank any other way, since another session's
// gamestate is unreachable from a script.
func TestBankReadIsNilSafe(t *testing.T) {
	items := []gamestate.SlotItem{{Item: gamestate.Item{ID: 1357, Count: 1}}}

	// A nil Deps, a Deps with no hook, and a real hook handed nothing must all be silent.
	var nilDeps *Deps
	nilDeps.bankRead(items) // must not panic

	(&Deps{}).bankRead(items) // must not panic

	called := 0
	d := &Deps{OnBankRead: func([]gamestate.SlotItem) { called++ }}
	d.bankRead(nil)
	if called != 0 {
		t.Errorf("OnBankRead was called %d time(s) for an EMPTY bank reading, want 0: an empty slice is a bank that could not be read, and publishing it would tell the fleet the mule holds nothing", called)
	}

	d.bankRead(items)
	if called != 1 {
		t.Errorf("OnBankRead was called %d time(s) for a real reading, want exactly 1", called)
	}
}

// TestBankReadPassesTheItemsThrough: the hook is what internal/bot/mule.go turns into a
// peerbank publication, so the items it receives have to be the bank's own, untouched.
func TestBankReadPassesTheItemsThrough(t *testing.T) {
	// The real the mule bank rows the 2026-09-22 diagnosis turned on: one Adamant axe (which is
	// NOT on orderplan.AxeHandouts) and a Steel battleaxe, which does not chop.
	items := []gamestate.SlotItem{{Item: gamestate.Item{ID: 1357, Count: 1}}, {Item: gamestate.Item{ID: 1365, Count: 4}}, {Item: gamestate.Item{ID: 995, Count: 2331324}}}
	var got []gamestate.SlotItem
	d := &Deps{OnBankRead: func(in []gamestate.SlotItem) { got = in }}
	d.bankRead(items)

	if len(got) != len(items) {
		t.Fatalf("the hook received %d rows, want all %d: a donor deciding what to ask for needs the whole bank, not a filtered view", len(got), len(items))
	}
	for i := range items {
		if got[i] != items[i] {
			t.Fatalf("row %d = %+v, want %+v unchanged", i, got[i], items[i])
		}
	}
}
