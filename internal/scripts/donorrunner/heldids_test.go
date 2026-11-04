package donorrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// A hand-out of coins arrives as whichever coins obj the mule's bank held; the receipt count must
// see it whichever of the name's un-noted ids it is.
func TestCountOfIDsSumsEveryIDOfTheName(t *testing.T) {
	pack := []gamestate.SlotItem{{Item: gamestate.Item{ID: 995, Count: 1000}}, {Item: gamestate.Item{ID: 301, Count: 1}}}
	if got := countOfIDs(pack, []int32{617, 995}); got != 1000 {
		t.Fatalf("coins held = %d, want 1000", got)
	}
	if got := countOfIDs(pack, []int32{301}); got != 1 {
		t.Fatalf("a single-id item = %d, want 1", got)
	}
	if got := countOfIDs(pack, nil); got != 0 {
		t.Fatalf("no ids = %d", got)
	}
}
