package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// World 2's Aubury is not world 1's: a reading on one world must never move the other's view.
func TestEachWorldHasItsOwnShopBoard(t *testing.T) {
	if ShopsFor(0) != Shops || ShopsFor(1) != Shops || ShopsFor(-3) != Shops {
		t.Fatal("the default and unknown worlds must share the process board every single-world caller uses")
	}
	w2, w3 := ShopsFor(2), ShopsFor(3)
	if w2 == Shops || w3 == Shops || w2 == w3 {
		t.Fatal("worlds 2 and 3 need boards of their own")
	}
	if ShopsFor(2) != w2 {
		t.Fatal("ShopsFor(2) is not stable across calls")
	}
	now := time.Now()
	tick := 600 * time.Millisecond
	w1Before := ShopsFor(1).Estimate(fleetplan.Aubury, fleetplan.MindRune, now, tick)
	// A world-2 account empties world 2's Aubury.
	w2.Observe(fleetplan.Aubury, fleetplan.MindRune, 0, now, "w2account")
	if got := w2.Estimate(fleetplan.Aubury, fleetplan.MindRune, now, tick); got != 0 {
		t.Fatalf("world 2's board after its own reading: %d", got)
	}
	// World 1's board never saw it.
	if got := ShopsFor(1).Estimate(fleetplan.Aubury, fleetplan.MindRune, now, tick); got != w1Before {
		t.Fatalf("world 2's empty Aubury moved world 1's board: %d -> %d", w1Before, got)
	}
	// World 3's board has nobody's reading: the content stock, not world 2's empty shelf.
	if got := w3.Estimate(fleetplan.Aubury, fleetplan.MindRune, now, tick); got == 0 {
		t.Fatal("world 2's empty Aubury leaked into world 3's board")
	}
}
