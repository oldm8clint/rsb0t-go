package vialplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The west bank tile is pinned because the value it replaced looked entirely reasonable and was
// wrong for months: 2946,3367 is the bank booth loc itself, unwalkable in the baked collision
// pack, so every leg out of the west bank planned as unreachable after 39 expansions. Anything
// on the counter row (z 3367, x 2945-2949) is the same bug again.
func TestFaladorWestBankStandsOffTheCounter(t *testing.T) {
	if FaladorWestBank.Z == 3367 {
		t.Fatalf("FaladorWestBank is %v: z 3367 is the bank booth row itself, which is unwalkable; stand north of it", FaladorWestBank)
	}
	want := tile.Tile{X: 2946, Z: 3368, Level: 0}
	if FaladorWestBank != want {
		t.Errorf("FaladorWestBank = %v, want %v (the street tile north of the counter, proven live 2026-09-18)", FaladorWestBank, want)
	}
	if FaladorWestBank.Level != 0 || FaladorEastBank.Level != 0 {
		t.Errorf("both Falador banks are ground level: west %v, east %v", FaladorWestBank, FaladorEastBank)
	}
}
