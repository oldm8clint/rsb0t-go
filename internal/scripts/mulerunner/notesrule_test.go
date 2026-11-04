package mulerunner

import "testing"

// TestMustUseNotes pins the rule that decides whether a hand-out crosses as bank notes.
//
// The stackable half is a regression test for a live failure on 2026-09-21. Every woodcutter
// asking the mule for the 150,000 coins a rune axe costs was declined with "that many will not
// fit in a pack and it has no bank note", because the rule tested QUANTITY alone: coins stack
// natively, so they have no noted form, and the notes branch had no way to succeed. A stackable
// item occupies one slot at any quantity and must never take that branch.
func TestMustUseNotes(t *testing.T) {
	for _, c := range []struct {
		name      string
		qty       int
		stackable bool
		want      bool
	}{
		{"one unstackable tool", 1, false, false},
		{"a full pack of an unstackable item", notesAbove, false, false},
		{"one past a full pack must be noted", notesAbove + 1, false, true},
		{"500 crystal keys, the case notes exist for", 500, false, true},

		// Stackable: never notes, however large. 150,000 is the live rune-axe price.
		{"a few coins", 5, true, false},
		{"a full pack of coins", notesAbove, true, false},
		{"one past a full pack of coins still stacks", notesAbove + 1, true, false},
		{"a rune axe's worth of coins", 150_000, true, false},
	} {
		if got := mustUseNotes(c.qty, c.stackable); got != c.want {
			t.Errorf("%s: mustUseNotes(%d, stackable=%v) = %v, want %v",
				c.name, c.qty, c.stackable, got, c.want)
		}
	}
}
