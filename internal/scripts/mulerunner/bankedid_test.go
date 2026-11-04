package mulerunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// "Coins" names two un-noted objs in this config (617 and 995); a hand-out must draw the one the
// bank actually holds, while every single-id name resolves exactly as nonNotedID always did.
func TestPickBankedIDPrefersTheIDTheBankHolds(t *testing.T) {
	never := func(int) bool { return false }
	bank := []gamestate.SlotItem{{Item: gamestate.Item{ID: 995, Count: 1000}}, {Item: gamestate.Item{ID: 301, Count: 3}}}
	if id, ok := pickBankedID([]int{617, 995}, never, bank); !ok || id != 995 {
		t.Fatalf("coins resolved to %d, want the banked 995", id)
	}
	if id, ok := pickBankedID([]int{617, 995}, never, nil); !ok || id != 617 {
		t.Fatalf("with nothing banked the first usable id should win as before, got %d", id)
	}
	noted := func(id int) bool { return id == 302 }
	if id, ok := pickBankedID([]int{301, 302}, noted, bank); !ok || id != 301 {
		t.Fatalf("a single un-noted id must resolve unchanged, got %d", id)
	}
	if _, ok := pickBankedID([]int{302}, noted, bank); ok {
		t.Fatal("a name with only a noted id must not resolve")
	}
}
