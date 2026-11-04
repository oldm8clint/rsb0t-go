package woodcutrunner

import (
	"testing"

	"strings"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Live 2026-09-22: acct155 logged
//
//	woodcutter: sold the starter kit: 0 items for 0 gp (24 coins)
//	walk to 3209,3247,0 avoids 28 zone rectangle(s)
//
// alternately, 200 times in the log tail at about one round every two seconds, for its whole
// run at 0 xp. Its sell queue held Arrow shafts, Leather gloves, Leather boots and Seaweed;
// the Lumbridge general store's stock at that moment was Pot, Jug, Shears, Bucket, Tinderbox,
// Chisel, Hammer and Newcomer map. StoreTrip's "the shop does not stock this" branch dropped
// each line silently without recording it, so Sellable never fell and DecideWoodPhase went on
// answering PhaseStore for ever.
//
// These tests pin the loop and its exit in the pure layer, where no session is needed: the
// refused map is the only thing standing between PhaseStore and the rest of the ladder.

// theLivePack is acct155's own sell queue, by name.
func theLivePack() []freshstart.PackItem {
	return []freshstart.PackItem{
		{Name: "Arrow shaft", Count: 1245},
		{Name: "Leather gloves", Count: 96},
		{Name: "Leather boots", Count: 165},
		{Name: "Seaweed", Count: 189},
	}
}

func inLumbridge() *tile.Tile {
	// The tile acct155 was walking to, in the Lumbridge general store's own radius.
	t := tile.Tile{X: 3209, Z: 3247, Level: 0}
	return &t
}

// TestUnstockedLinesLeaveTheStorePhaseOnceRefused is the livelock and its exit in one test.
func TestUnstockedLinesLeaveTheStorePhaseOnceRefused(t *testing.T) {
	refused := map[string]bool{}
	queue := freshstart.SellQueue(theLivePack(), nil, refused)
	if len(queue) == 0 {
		t.Fatal("the live pack produced an empty sell queue -- the fixture no longer reproduces the bug")
	}

	// Before: Sellable > 0, so the phase is PhaseStore and StoreTrip will sell nothing,
	// because none of these names is in the shop's stock.
	s := woodcutplan.WoodSnapshot{Tile: inLumbridge(), Sellable: len(queue), Bright: true}
	if got := woodcutplan.DecideWoodPhase(s); got != woodcutplan.PhaseStore {
		t.Fatalf("DecideWoodPhase = %q, want PhaseStore -- without this the test below proves nothing", got)
	}

	// StoreTrip's not-in-stock branch now records each line, which is the one-line fix.
	for _, line := range queue {
		refused[strings.ToLower(line.Name)] = true
	}

	after := freshstart.SellQueue(theLivePack(), nil, refused)
	if len(after) != 0 {
		t.Fatalf("SellQueue still offers %d line(s) after every one was refused: %v", len(after), after)
	}
	s.Sellable = len(after)
	if got := woodcutplan.DecideWoodPhase(s); got == woodcutplan.PhaseStore {
		t.Fatal("still PhaseStore with nothing left to sell -- this is the live livelock: " +
			"open the shop, sell 0 items, walk away, repeat for ever")
	}
}

// The guard on the other side. An empty shop pane means "the stock has not loaded yet", not
// "it stocks nothing", so StoreTrip must not write the queue off on one. Without this the fix
// turns a livelock into a silent refusal ever to sell anything -- the same bug facing the
// other way, and strictly worse because it would be quiet.
func TestAnEmptyShopPaneDoesNotRefuseAnything(t *testing.T) {
	if storeShouldRecordRefusal(0) {
		t.Fatal("an empty shop pane refused a sell line -- the stock had not loaded yet")
	}
	if !storeShouldRecordRefusal(8) {
		t.Fatal("a loaded shop pane did not record the refusal, so Sellable never falls")
	}
}
