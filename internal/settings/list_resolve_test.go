package settings

import "testing"

// A list setting resolved through a Schema must be readable back off the Bag. It was not:
// Schema.resolve stores TypeList as []string while Bag.List only accepted []interface{}, so
// every schema-resolved list silently read back as the caller's fallback. Found by the
// GemCutter port, whose gem selection resolved correctly and then came back empty, which
// widened the run to every gem instead of the chosen ones.
func TestBagListReadsASchemaResolvedList(t *testing.T) {
	schema := Schema{
		"gems": {
			Type:    TypeList,
			Default: []string{},
			Options: []string{"Sapphire", "Emerald", "Ruby"},
		},
	}

	// The shape a JSON-decoded settings file actually produces.
	bag := schema.Resolve(map[string]interface{}{"gems": []interface{}{"ruby", "SAPPHIRE"}})
	got := bag.List("gems", nil)
	if len(got) != 2 || got[0] != "Ruby" || got[1] != "Sapphire" {
		t.Errorf("List after Resolve = %v, want the canonical [Ruby Sapphire]", got)
	}

	// The shape a Go caller passes directly.
	bag = schema.Resolve(map[string]interface{}{"gems": []string{"Emerald"}})
	if got := bag.List("gems", nil); len(got) != 1 || got[0] != "Emerald" {
		t.Errorf("List for a []string value = %v, want [Emerald]", got)
	}

	// A missing key resolves to the schema default, which must also read back.
	bag = schema.Resolve(map[string]interface{}{})
	if got := bag.List("gems", []string{"fallback"}); len(got) != 0 {
		t.Errorf("an unset list should resolve to the empty default, got %v", got)
	}
}

func TestBagListFallsBackForANonList(t *testing.T) {
	bag := NewBagFrom(map[string]interface{}{"gems": 7, "mixed": []interface{}{"a", 3}})
	if got := bag.List("gems", []string{"fallback"}); len(got) != 1 || got[0] != "fallback" {
		t.Errorf("a number is not a list; want the fallback, got %v", got)
	}
	if got := bag.List("mixed", []string{"fallback"}); len(got) != 1 || got[0] != "fallback" {
		t.Errorf("a list with a non-string element should fall back, got %v", got)
	}
	if got := bag.List("absent", []string{"fallback"}); len(got) != 1 {
		t.Errorf("an absent key should fall back, got %v", got)
	}
}

// The returned slice must not alias the Bag's own storage, or a caller could mutate another
// caller's settings through it.
func TestBagListReturnsACopy(t *testing.T) {
	bag := NewBagFrom(map[string]interface{}{"gems": []string{"Ruby"}})
	got := bag.List("gems", nil)
	got[0] = "clobbered"
	if again := bag.List("gems", nil); again[0] != "Ruby" {
		t.Errorf("mutating a returned list changed the Bag: %v", again)
	}
}
