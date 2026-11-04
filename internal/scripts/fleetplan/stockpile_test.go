package fleetplan

import (
	"testing"
)

func TestChooseFoodHealsInFullAndSavesSharks(t *testing.T) {
	pack := []CarriedFood{{"Shark", 5}, {"Lobster", 5}, {"Shrimps", 5}}
	cases := []struct {
		name      string
		hp, max   int
		pack      []CarriedFood
		want      string
		emergency bool
		none      bool
	}{
		{"full hp eats nothing", 60, 60, pack, "", false, true},
		{"2 missing: even a shrimp would overheal", 58, 60, pack, "", false, true},
		{"3 missing: a shrimp heals in full", 57, 60, pack, "Shrimps", false, false},
		{"15 missing: lobster, not the shark that would overheal", 45, 60, pack, "Lobster", false, false},
		{"20 missing: now the shark heals in full", 40, 60, pack, "Shark", false, false},
		{"12 missing, only sharks: wait", 48, 60, []CarriedFood{{"Shark", 3}}, "", false, true},
		{"emergency: biggest heal whatever the waste", 4, 60, pack, "Shark", true, false},
		{"emergency on a small account", 3, 10, pack, "Shark", true, false},
		{"no food", 20, 60, nil, "", false, true},
	}
	for _, c := range cases {
		got, ok := ChooseFood(c.pack, c.hp, c.max, 0)
		if c.none {
			if ok {
				t.Errorf("%s: ate %s", c.name, got.Food)
			}
			continue
		}
		if !ok || got.Food != c.want || got.Emergency != c.emergency {
			t.Errorf("%s: got %+v ok=%v, want %s (emergency %v)", c.name, got, ok, c.want, c.emergency)
		}
	}
	// The heals themselves are the content's.
	for food, heal := range map[string]int{"Shark": 20, "Swordfish": 14, "Bass": 13, "Lobster": 12, "Tuna": 10, "Cod": 7, "Cake": 4} {
		got, ok := ChooseFood([]CarriedFood{{food, 1}}, 99-heal, 99, 0)
		if !ok || got.Heal != heal {
			t.Errorf("%s heals %d, want %d", food, got.Heal, heal)
		}
	}
}
