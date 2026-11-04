package questrunner

import "testing"

// A cake's config names only "Eat"; the client supplies "Drop" in the fifth slot, and the
// server accepts op 5 as Drop. Live 2026-09-26: Waterfall's freeSlots failed "2/3 cake has no
// "Drop" op (it has [Eat    ])" at full hitpoints and looped.
func TestHeldOpIndexAppliesTheClientDropDefault(t *testing.T) {
	cake := [5]string{"Eat"}
	if got := heldOpIndex(cake, "Drop"); got != 5 {
		t.Fatalf("Drop on a cake = op %d, want 5", got)
	}
	if got := heldOpIndex(cake, "eat"); got != 1 {
		t.Fatalf("Eat on a cake = op %d, want 1", got)
	}
	if got := heldOpIndex(cake, "Wield"); got != 0 {
		t.Fatalf("Wield on a cake = op %d, want 0", got)
	}
	// A type that names its own fifth op keeps it; no Drop is invented.
	rune5 := [5]string{"", "", "", "", "Destroy"}
	if got := heldOpIndex(rune5, "Drop"); got != 0 {
		t.Fatalf("Drop on a Destroy item = op %d, want 0", got)
	}
}
