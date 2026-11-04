package donorrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

func slot(id, count int32) gamestate.SlotItem {
	return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: count}}
}

// The kit-ask hand-over's room: a stack given whole frees its slot (every slot, for an unnoted item); a stack
// given in part (coins drawn onto the purse the account keeps) frees none.
func TestGiveFreedCountsOnlyWholeStacks(t *testing.T) {
	pack := []gamestate.SlotItem{
		slot(995, 80000),             // coins: 20,000 purse + 60,000 drawn to give
		slot(533, 40),                // big bone notes, all given
		slot(1965, 1), slot(1965, 1), // two unnoted cabbages, both given
		slot(554, 300), // fire runes kept
	}
	give := []GiveLine{{ID: 995, Qty: 60000}, {ID: 533, Qty: 40}, {ID: 1965, Qty: 2}}
	if got := GiveFreed(pack, give); got != 3 {
		t.Fatalf("freed %d, want 3 (the bone notes and both cabbages; the purse stays)", got)
	}
}

// A hand-over line the pack does not hold (in full) is cut to what it holds.
func TestGiveHeldCutsToThePack(t *testing.T) {
	pack := []gamestate.SlotItem{slot(995, 500), slot(533, 10)}
	got := GiveHeld(pack, []GiveLine{{ID: 995, Qty: 1000}, {ID: 533, Qty: 10}, {ID: 7, Qty: 3}})
	if len(got) != 2 || got[0] != (GiveLine{ID: 995, Qty: 500}) || got[1] != (GiveLine{ID: 533, Qty: 10}) {
		t.Fatalf("got %+v", got)
	}
}

// Offer-All only when the pack holds no more of it than is given.
func TestGiveTradeLinesOfferAllOnlyWholeStacks(t *testing.T) {
	pack := []gamestate.SlotItem{slot(995, 80000), slot(533, 40)}
	lines := giveTradeLines(pack, []GiveLine{{ID: 995, Qty: 60000}, {ID: 533, Qty: 40}})
	if lines[0].All || !lines[1].All {
		t.Fatalf("lines %+v: coins must be Offer-X (the purse stays), the notes Offer-All", lines)
	}
}
