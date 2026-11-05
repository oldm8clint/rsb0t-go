package consolidateplan

import "testing"

func TestScaleKitAskMins(t *testing.T) {
	s := Settings{MinCoins: KitAskMinCoins, MinRunes: KitAskMinRunes, MinFood: KitAskMinFood, MinLootValue: KitAskMinLootValue, MinLootRows: KitAskMinLootRows}
	got := ScaleKitAskMins(s, 5, 100, 250)
	if got.MinCoins != 100_000 || got.MinRunes != 500 || got.MinFood != 50 || got.MinLootValue != 25_000 || got.MinLootRows != 25 {
		t.Fatalf("a roomy bank scales every minimum: %+v", got)
	}
	if got := ScaleKitAskMins(s, 5, 250, 250); got.MinCoins != s.MinCoins || got.MinRunes != s.MinRunes {
		t.Fatalf("a near-full bank keeps the lowered minimums: %+v", got)
	}
	if got := ScaleKitAskMins(s, 1, 10, 250); got.MinCoins != s.MinCoins || got.MinRunes != s.MinRunes {
		t.Fatalf("scale 1 is off: %+v", got)
	}
	if got := ScaleKitAskMins(s, 5, -1, 250); got.MinCoins != 100_000 {
		t.Fatalf("an unread bank counts as not full: %+v", got)
	}
}
