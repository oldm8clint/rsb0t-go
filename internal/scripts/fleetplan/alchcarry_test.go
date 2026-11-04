package fleetplan

import (
	"strings"
	"testing"
)

func TestStockKeepSlotsKeepsTheBestSlotsWithinTheBudget(t *testing.T) {
	slots := []StockSlot{
		{Slot: 3, Count: 721, Rank: 4}, // the yew shortbows: far over any carry
		{Slot: 5, Count: 40, Rank: 1},  // a dearer item
		{Slot: 6, Count: 70, Rank: 0},  // the dearest
		{Slot: 9, Count: 225, Rank: 2}, // the yew longbows
		{Slot: 11, Count: 30, Rank: 3},
	}
	keep, kept := StockKeepSlots(slots, 120)
	if kept != 110 || !keep[6] || !keep[5] || keep[3] || keep[9] || keep[11] {
		t.Fatalf("keep = %v kept = %d, want the 70 and the 40 (110 casts)", keep, kept)
	}
	// A slot too big for the rest of the budget is skipped, and a smaller one behind it still fits.
	keep, kept = StockKeepSlots(slots, 100)
	if kept != 100 || !keep[6] || !keep[11] || keep[5] {
		t.Fatalf("keep = %v kept = %d, want 70 + 30 (the 40 does not fit after the 70)", keep, kept)
	}
}

func TestStockKeepSlotsWithNoBudgetKeepsNothing(t *testing.T) {
	for _, budget := range []int{0, -5} {
		if keep, kept := StockKeepSlots([]StockSlot{{Slot: 1, Count: 1}}, budget); len(keep) != 0 || kept != 0 {
			t.Fatalf("budget %d kept %v (%d)", budget, keep, kept)
		}
	}
	if keep, kept := StockKeepSlots(nil, 50); len(keep) != 0 || kept != 0 {
		t.Fatalf("an empty pack kept %v (%d)", keep, kept)
	}
}

func TestCarryLineSaysWhatWasKeptAndWhy(t *testing.T) {
	if got := CarryLine("a safespot pen", 60, 946, 60); !strings.Contains(got, "carrying 60 of 946") || !strings.Contains(got, "the rest stays banked") {
		t.Fatalf("line = %q", got)
	}
	if got := CarryLine("the melee trainer", 0, 946, 0); !strings.Contains(got, "carrying 0 of 946") || !strings.Contains(got, "stays banked") {
		t.Fatalf("line = %q", got)
	}
}
