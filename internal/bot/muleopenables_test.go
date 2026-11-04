package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// Live 2026-09-20: a pass that hit its round cap cleared the openables job with caskets and
// oysters still banked. The bank read that decides whether the job is done counts every
// casket and oyster stack, and nothing else.
func TestOpenableCountCountsCasketsAndOystersOnly(t *testing.T) {
	names := map[int]string{405: "Casket", 407: "Oyster", 406: "Casket", 363: "Raw bass", 411: "Oyster pearl", 413: "Empty oyster"}
	noted := map[int]bool{406: true}
	nameOf := func(id int) (string, bool) { return names[id], noted[id] }
	items := []gamestate.SlotItem{
		{Item: gamestate.Item{ID: 405, Count: 300}},
		{Item: gamestate.Item{ID: 407, Count: 41}},
		{Item: gamestate.Item{ID: 406, Count: 9}}, // a note cannot be opened
		{Item: gamestate.Item{ID: 363, Count: 900}},
		{Item: gamestate.Item{ID: 411, Count: 3}},
		{Item: gamestate.Item{ID: 413, Count: 5}},
	}
	if got := openableCount(items, nameOf); got != 341 {
		t.Fatalf("openable count = %d, want 341 (300 caskets + 41 oysters)", got)
	}
	if got := openableCount(nil, nameOf); got != 0 {
		t.Fatalf("empty bank: %d, want 0", got)
	}
	if got := bankedOpenableCount(nil, items); got != 0 {
		t.Fatalf("no config: %d, want 0", got)
	}
}
