package mulerunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/orderplan"
)

// The mechanical half of "hand out anything the bank holds", asserted against the LIVE the mule
// bank as read on 2026-09-22 by a read-only GET /api/bank?account=the mule.
//
// orderplan's own tests prove the DECISION is now made; these prove the two steps that turn that
// decision into items on a trade screen still work for an item nobody catalogued. They are the
// steps that would fail silently: a name that resolves to a bank note would hand a woodcutter
// something it cannot chop with, and a quantity rule that asks the wrong question already broke
// coins once (see mustUseNotes' own doc comment for that live incident).

// liveAdamantAxe is the one chopping axe in that bank: obj 1357, count 1, slot 180. There is no
// mithril, steel, iron or bronze axe in it at all — which is every rung of orderplan.AxeHandouts.
const liveAdamantAxe = 1357

// TestOpenHandoutResolvesTheItemTheBankReallyHolds. The id a hand-out draws must be the one the
// bank has, not merely the first id the name resolves to, and it must never be a bank note: a
// note of an axe is not an axe, and the account would arrive at a tree still blocked.
func TestOpenHandoutResolvesTheItemTheBankReallyHolds(t *testing.T) {
	// The live bank row, plus a hypothetical noted form of the same name, since ObjIDs returns
	// both for most tradeable items and picking the note is the silent failure.
	const notedAdamantAxe = liveAdamantAxe + 1
	noted := func(id int) bool { return id == notedAdamantAxe }
	bank := []gamestate.SlotItem{{Item: gamestate.Item{ID: liveAdamantAxe, Count: 1}}}

	id, ok := pickBankedID([]int{liveAdamantAxe, notedAdamantAxe}, noted, bank, nil)
	if !ok {
		t.Fatal("the Adamant axe the live mule holds did not resolve to a drawable id — the widened decision would then be declined 'I cannot identify that item'")
	}
	if id != liveAdamantAxe {
		t.Fatalf("resolved to %d, want %d: the un-noted axe, because a note of an axe cannot chop", id, liveAdamantAxe)
	}
	if n := countByID(bank, id); n != 1 {
		t.Fatalf("counted %d of it, want the 1 the live bank holds", n)
	}
}

// TestOpenHandoutOfOneItemDoesNotBecomeABankNote. notesAbove exists for an order that genuinely
// cannot fit in a backpack (500 crystal keys against 28 slots). Widening the catalogue means
// far more items reach this rule, so the boundary is pinned for the two shapes that now matter:
// the single tool users asked about, and a food load a combat account asks for.
func TestOpenHandoutOfOneItemDoesNotBecomeABankNote(t *testing.T) {
	for _, tc := range []struct {
		what      string
		qty       int
		stackable bool
		want      bool
		why       string
	}{
		{"the live mule's one Adamant axe", 1, false, false,
			"one item fits in a pack, and a woodcutter needs the axe rather than a note of it"},
		{"a full pack of Sharks", 28, false, false,
			"28 is exactly a backpack; the boundary is strictly above, and an account eats sharks rather than notes"},
		{"a thousand Air runes", 1000, true, false,
			"runes stack, so any quantity is one slot — this is the question coins got wrong live, which declined every rune-axe funding order"},
		{"a hundred Bread", 100, false, true,
			"a hundred un-noted loaves cannot fit in 28 slots, so notes are the only way they can cross"},
	} {
		if got := mustUseNotes(tc.qty, tc.stackable); got != tc.want {
			t.Errorf("%s: mustUseNotes(%d, stackable=%v) = %v, want %v — %s",
				tc.what, tc.qty, tc.stackable, got, tc.want, tc.why)
		}
	}
}

// TestHandoutPrescreenAcceptsAnUncataloguedItemBeforeAnyBankTrip. Handout's first act is
// orderplan.Prescreen, deliberately, so an order that cannot possibly be served costs a log line
// rather than a walk to a booth. The widening has to be visible THERE too, or the mule would
// refuse an item it holds without ever opening its bank to look.
func TestHandoutPrescreenAcceptsAnUncataloguedItemBeforeAnyBankTrip(t *testing.T) {
	pol := orderplan.DefaultPolicy([]string{"acct134"})
	led := orderplan.NewLedger(orderplan.DefaultWindow)

	d := orderplan.Prescreen(time.Now(), "acct134", orderplan.Format("Adamant axe", 1), pol, led)
	if d.Verdict != orderplan.Fulfil {
		t.Fatalf("Prescreen declined an Adamant axe (%q) — Handout returns there and never opens the bank, so the live mule's one axe stays in it forever", d.Reason)
	}

	// And the fail-closed side is unchanged: a name off the allow list never reaches a bank,
	// whatever the catalogue now allows.
	if d := orderplan.Prescreen(time.Now(), "someone_else", orderplan.Format("Adamant axe", 1), pol, led); d.Verdict != orderplan.Decline {
		t.Fatalf("an unknown name was prescreened as fulfillable: %+v — the allow list is the only authentication the bus has, and widening the catalogue must not have touched it", d)
	}
}
