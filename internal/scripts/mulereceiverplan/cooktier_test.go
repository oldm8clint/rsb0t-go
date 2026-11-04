package mulereceiverplan

import "testing"

func TestHighestTierInBankPicksHighestLevelPresent(t *testing.T) {
	bank := map[string]int{"raw lobster": 40, "raw shark": 5, "raw cod": 12}
	got, ok := HighestTierInBank(bank, 99)
	if !ok {
		t.Fatal("expected a tier, got none")
	}
	if got.Raw != "Raw shark" {
		t.Errorf("HighestTierInBank = %q, want %q", got.Raw, "Raw shark")
	}
}

func TestHighestTierInBankTieBreaksAnchoviesBeforeShrimps(t *testing.T) {
	// Both level 1; scanning FishTiers in reverse (highest level first) reaches anchovies
	// (the later entry in the table) before shrimps.
	bank := map[string]int{"raw anchovies": 3, "raw shrimps": 1}
	got, ok := HighestTierInBank(bank, 99)
	if !ok || got.Raw != "Raw anchovies" {
		t.Errorf("HighestTierInBank = %+v, ok=%v, want Raw anchovies", got, ok)
	}
}

func TestHighestTierInBankEmptyBank(t *testing.T) {
	if _, ok := HighestTierInBank(map[string]int{}, 99); ok {
		t.Error("expected no tier for an empty bank")
	}
	if _, ok := HighestTierInBank(nil, 99); ok {
		t.Error("expected no tier for a nil bank map")
	}
}

func TestHighestTierInBankZeroCountDoesNotCount(t *testing.T) {
	bank := map[string]int{"raw shark": 0, "raw cod": 5}
	got, ok := HighestTierInBank(bank, 99)
	if !ok || got.Raw != "Raw cod" {
		t.Errorf("HighestTierInBank = %+v, ok=%v, want Raw cod (shark at 0 should be skipped)", got, ok)
	}
}

func TestHighestTierInBankLowerCaseKeyedMap(t *testing.T) {
	// HighestTierInBank's own contract (see its doc comment): bankRaw is keyed by
	// lower-cased item name, the shape mulerunner's live bank read builds — not
	// case-insensitive matching on the caller's own map, which Go maps can't do anyway.
	bank := map[string]int{"raw shark": 2}
	got, ok := HighestTierInBank(bank, 99)
	if !ok || got.Raw != "Raw shark" {
		t.Errorf("HighestTierInBank = %+v, ok=%v, want Raw shark", got, ok)
	}
}

func TestHighestTierInBankGatedByCookingLevel(t *testing.T) {
	// Only cod (level 18) and shark (level 80) are banked; a level-20 mule can cook cod
	// but must not attempt shark, even though shark would otherwise win as the highest tier.
	bank := map[string]int{"raw cod": 10, "raw shark": 10}
	got, ok := HighestTierInBank(bank, 20)
	if !ok || got.Raw != "Raw cod" {
		t.Errorf("HighestTierInBank = %+v, ok=%v, want Raw cod", got, ok)
	}
}

func TestHighestTierInBankNoTierWithinLevel(t *testing.T) {
	// Only shark (level 80) is banked; a level-1 mule cannot cook it, so there is nothing
	// within reach even though the bank is not empty.
	bank := map[string]int{"raw shark": 10}
	if _, ok := HighestTierInBank(bank, 1); ok {
		t.Error("expected no tier: the only banked fish is above the mule's own Cooking level")
	}
}

func TestHighestTierInBankSkipsAboveLevelEvenWhenBanked(t *testing.T) {
	// Bank holds both shark (level 80) and shrimps (level 1); a low-level mule should get
	// shrimps, not be blocked (or given nothing) simply because a higher tier also has stock
	// it cannot yet cook.
	bank := map[string]int{"raw shark": 3, "raw shrimps": 8}
	got, ok := HighestTierInBank(bank, 5)
	if !ok || got.Raw != "Raw shrimps" {
		t.Errorf("HighestTierInBank = %+v, ok=%v, want Raw shrimps", got, ok)
	}
}

func TestFishTiersLevelOrder(t *testing.T) {
	for i := 1; i < len(FishTiers); i++ {
		if FishTiers[i].Level < FishTiers[i-1].Level {
			t.Errorf("FishTiers not in level order at index %d: %+v before %+v", i, FishTiers[i-1], FishTiers[i])
		}
	}
}

func TestFishTiersMatchRealCookingLevels(t *testing.T) {
	// Ground-truthed against this content build's own real data (content/scripts/
	// skill_cooking/configs/cooking_source/cooking_generic.dbrow's levelrequired field),
	// not assumed — see cooktier.go's own doc comment.
	want := map[string]int{
		"Raw shrimps":    1,
		"Raw anchovies":  1,
		"Raw mackerel":   10,
		"Raw cod":        18,
		"Raw tuna":       30,
		"Raw lobster":    40,
		"Raw bass":       43,
		"Raw swordfish":  45,
		"Raw shark":      80,
		"Raw sea turtle": 82,
		"Raw manta ray":  91,
	}
	if len(want) != len(FishTiers) {
		t.Fatalf("FishTiers has %d entries, want %d", len(FishTiers), len(want))
	}
	for _, tier := range FishTiers {
		lvl, ok := want[tier.Raw]
		if !ok {
			t.Errorf("unexpected tier %q in FishTiers", tier.Raw)
			continue
		}
		if tier.Level != lvl {
			t.Errorf("FishTiers[%q].Level = %d, want %d", tier.Raw, tier.Level, lvl)
		}
	}
}
