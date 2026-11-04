package combatplan

import "testing"

func TestWorthTrainLoot(t *testing.T) {
	cases := []struct {
		d    TrainLootDrop
		free int
		want bool
	}{
		{TrainLootDrop{Name: "Feather", Stackable: true}, 1, true},
		{TrainLootDrop{Name: "Nature rune", Stackable: true, Stacks: true}, 0, true}, // a full pack still stacks
		{TrainLootDrop{Name: "Law rune", Stackable: true}, 0, false},                 // no slot for a new stack
		{TrainLootDrop{Name: "Big bones", Cost: 1}, 5, false},                        // the bone pass's
		{TrainLootDrop{Name: "Steel med helm", Cost: 300}, 1, true},
		{TrainLootDrop{Name: "Steel med helm", Cost: 300}, 0, false}, // food is never dropped for it
		{TrainLootDrop{Name: "Raw chicken", Cost: 1}, 10, false},     // not worth a slot
	}
	for _, c := range cases {
		if got := WorthTrainLoot(c.d, c.free); got != c.want {
			t.Errorf("%+v with %d free: got %v, want %v", c.d, c.free, got, c.want)
		}
	}
}

// Limpwurt roots from the giants (a user request): worth 7 coins, taken into spare slots only.
func TestWorthTrainLootTakesLimpwurtRootsIntoSpareSlots(t *testing.T) {
	root := TrainLootDrop{Name: "Limpwurt root", Cost: 7}
	for free, want := range map[int]bool{0: false, 1: false, 2: false, 3: true, 10: true} {
		if got := WorthTrainLoot(root, free); got != want {
			t.Errorf("a limpwurt root with %d free slots: got %v, want %v", free, got, want)
		}
	}
	if WorthTrainLoot(TrainLootDrop{Name: "Grimy thing", Cost: 7}, 10) {
		t.Error("another 7-coin drop was taken")
	}
}
