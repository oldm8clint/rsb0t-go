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
	if id, ok := pickBankedID([]int{617, 995}, never, bank, nil); !ok || id != 995 {
		t.Fatalf("coins resolved to %d, want the banked 995", id)
	}
	if id, ok := pickBankedID([]int{617, 995}, never, nil, nil); !ok || id != 617 {
		t.Fatalf("with nothing banked the first usable id should win as before, got %d", id)
	}
	noted := func(id int) bool { return id == 302 }
	if id, ok := pickBankedID([]int{301, 302}, noted, bank, nil); !ok || id != 301 {
		t.Fatalf("a single un-noted id must resolve unchanged, got %d", id)
	}
	if _, ok := pickBankedID([]int{302}, noted, bank, nil); ok {
		t.Fatal("a name with only a noted id must not resolve")
	}
}

// "Shortbow" names the unstrung bow (50) and the strung one (841): a plain name hands over the one
// that can be wielded, whichever order the ids come in, and falls back to what the bank holds.
func TestPickBankedIDPrefersTheWearableForm(t *testing.T) {
	never := func(int) bool { return false }
	wear := func(id int) bool { return id == 841 }
	both := []gamestate.SlotItem{{Item: gamestate.Item{ID: 50, Count: 9}}, {Item: gamestate.Item{ID: 841, Count: 4}}}
	if id, ok := pickBankedID([]int{50, 841}, never, both, wear); !ok || id != 841 {
		t.Fatalf("both held: resolved to %d, want the strung 841", id)
	}
	onlyUnstrung := []gamestate.SlotItem{{Item: gamestate.Item{ID: 50, Count: 9}}}
	if id, _ := pickBankedID([]int{50, 841}, never, onlyUnstrung, wear); id != 50 {
		t.Fatalf("only the unstrung bow held: resolved to %d, want 50 as before", id)
	}
	if id, _ := pickBankedID([]int{50, 841}, never, nil, wear); id != 841 {
		t.Fatalf("nothing held: resolved to %d, want the wearable 841", id)
	}
	if !Wearable([]string{"", "Wield", "", "", "Drop"}) || Wearable([]string{"Use", "", "", "", "Drop"}) {
		t.Fatal("Wearable must read Wield/Wear only")
	}
}
