package combatplan

import "testing"

// The top bands run on to the ceiling, so a lowest melee level of 75 to 99 has a rung and a goal of 99 is finishable; the
// ladders themselves are not touched.
func TestExtendTopBandsGivesTheTopRungsALevelTo99(t *testing.T) {
	for name, ladder := range map[string][]Rung{"melee": MeleeLadder(), "spread": SpreadLadder(), "spread+wilderness": SpreadLadderWith(SpreadOptions{Wilderness: true})} {
		before := make([]int, len(ladder))
		for i, r := range ladder {
			before[i] = r.ToLevel
		}
		if got := MaxTrainableGoal(ladder, true); got != 75 {
			t.Fatalf("%s: the ladder's ceiling is %d, want 75", name, got)
		}
		ext := ExtendTopBands(ladder, ExtendedCeiling)
		if got := MaxTrainableGoal(ext, true); got != ExtendedCeiling {
			t.Errorf("%s: extended ceiling %d, want %d", name, got, ExtendedCeiling)
		}
		if got := MaxTrainableGoal(ladder, true); got != 75 {
			t.Errorf("%s: the original ladder was changed: ceiling %d", name, got)
		}
		for i, r := range ext {
			switch {
			case before[i] == 75 && r.ToLevel != ExtendedCeiling:
				t.Errorf("%s: rung %s ended at 75 and now ends at %d", name, r.Key, r.ToLevel)
			case before[i] != 75 && r.ToLevel != before[i]:
				t.Errorf("%s: rung %s is below the top and must not move: %d -> %d", name, r.Key, before[i], r.ToLevel)
			case r.FromLevel != ladder[i].FromLevel || r.Key != ladder[i].Key:
				t.Errorf("%s: rung %d changed shape", name, i)
			}
		}
		// Every level to 98 (the highest a goal of 99 asks a rung for) has a usable rung for a strong account.
		for lvl := 60; lvl < 99; lvl++ {
			if !PickRung(ext, lvl, 126, true).OK {
				t.Errorf("%s: no rung for lowest melee %d on the extended ladder", name, lvl)
			}
		}
		if PickRung(ladder, 80, 126, true).OK {
			t.Errorf("%s: the plain ladder has no rung at 80", name)
		}
	}
}

// A ladder already past the ceiling, and an empty one, come back as plain copies.
func TestExtendTopBandsLeavesAHigherOrEmptyLadderAlone(t *testing.T) {
	if got := ExtendTopBands(nil, ExtendedCeiling); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
	ext := ExtendTopBands(MeleeLadder(), ExtendedCeiling)
	again := ExtendTopBands(ext, ExtendedCeiling)
	for i := range ext {
		if again[i].ToLevel != ext[i].ToLevel {
			t.Fatalf("rung %s moved on a second extension", ext[i].Key)
		}
	}
	if &again[0] == &ext[0] {
		t.Fatal("a copy is returned, never the argument")
	}
}
