package quests

import "testing"

func TestQuestTableCoversTheWholeTab(t *testing.T) {
	if len(questRows) != 68 {
		t.Fatalf("questRows has %d rows, want the 68 of content 289's questlist.if", len(questRows))
	}
	free := 0
	ids := map[int]string{}
	names := map[string]bool{}
	for _, row := range questRows {
		if !row.Members {
			free++
		}
		if other, dup := ids[row.ComID]; dup {
			t.Fatalf("%q and %q share component %d", row.Label, other, row.ComID)
		}
		ids[row.ComID] = row.Label
		if names[normalise(row.Label)] {
			t.Fatalf("duplicate quest name %q", row.Label)
		}
		names[normalise(row.Label)] = true
	}
	if free != 18 {
		t.Fatalf("%d free quests, want 18", free)
	}

	// The nine ids first confirmed live with cmd/questtest -dump.
	live := map[string]int{
		"cook's assistant": 7333, "black knight's fortress": 7332, "demon slayer": 7334,
		"the restless ghost": 7337, "imp catcher": 7340, "pirate's treasure": 7341,
		"romeo & juliet": 7343, "sheep shearer": 7344, "vampire slayer": 7347,
	}
	for name, want := range live {
		if got, ok := ComponentID(name); !ok || got != want {
			t.Errorf("ComponentID(%q) = %d,%v, want %d", name, got, ok, want)
		}
	}
	if id, ok := ComponentID("Dragon Slayer"); !ok || id != 7383 {
		t.Fatalf("Dragon Slayer = %d,%v, want 7383", id, ok)
	}
	if id, ok := ComponentID("haunted mine"); !ok || id != 11857 {
		t.Fatalf("Haunted Mine (questlist:com_90) = %d,%v, want 11857", id, ok)
	}
}

func TestAllCarriesLabelsAndMembership(t *testing.T) {
	for _, e := range All(emptyState()) {
		if e.Label == "" || e.ComID == 0 {
			t.Fatalf("entry without a label or component: %+v", e)
		}
		if e.Name == "dragon slayer" && (!e.Members || e.Label != "Dragon Slayer") {
			t.Fatalf("dragon slayer entry = %+v", e)
		}
		if e.Name == "cook's assistant" && e.Members {
			t.Fatal("Cook's Assistant is a free quest")
		}
	}
}
