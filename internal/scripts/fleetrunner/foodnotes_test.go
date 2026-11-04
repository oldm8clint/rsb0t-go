package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// Bank notes are not food: a bundled kit's noted cakes must not count as cakes the account can eat.
func TestWithoutNotesDropsBankNotes(t *testing.T) {
	const cake, cakeNote = 1891, 1892
	slots := []gamestate.SlotItem{{Item: gamestate.Item{ID: cake, Count: 2}}, {Item: gamestate.Item{ID: cakeNote, Count: 16}}}
	got := WithoutNotes(slots, func(id int32) bool { return id == cakeNote })
	if len(got) != 1 || got[0].ID != cake || got[0].Count != 2 {
		t.Fatalf("withoutNotes kept %v", got)
	}
	// No config (nothing known to be a note): everything stays.
	if all := PackUnnoted(&Deps{Session: &packSession{st: gamestate.New()}}); len(all) != 0 {
		t.Fatalf("an empty pack: %v", all)
	}
}
