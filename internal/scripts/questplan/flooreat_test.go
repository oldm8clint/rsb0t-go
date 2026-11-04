package questplan

import (
	"testing"
	"time"
)

// The live deaths: 1 to 3 of 34 to 39 hitpoints with food carried. A bite is due at or under half the hitpoints, never above,
// never with an unread maximum or a dead account, and not twice inside the eat delay.
func TestFloorEatDueAtOrUnderHalf(t *testing.T) {
	for _, c := range []struct {
		hp, max int
		since   time.Duration
		want    bool
		name    string
	}{
		{2, 34, time.Hour, true, "the poisoned account at 2 of 34"},
		{9, 34, time.Hour, true, "9 of 34, the hitpoints a Grail give-up lands at"},
		{17, 34, time.Hour, true, "exactly half"},
		{18, 34, time.Hour, false, "just over half"},
		{34, 34, time.Hour, false, "full"},
		{0, 34, time.Hour, false, "a dead account (a stat packet not yet read)"},
		{5, 0, time.Hour, false, "an unread maximum"},
		{9, 34, FloorEatGap - time.Millisecond, false, "inside the eat delay"},
		{9, 34, FloorEatGap, true, "a bite after the gap"},
	} {
		if got := FloorEatDue(c.hp, c.max, c.since); got != c.want {
			t.Errorf("%s: FloorEatDue(%d, %d, %v) = %v, want %v", c.name, c.hp, c.max, c.since, got, c.want)
		}
	}
}

// What a floor bite eats: any food by name, the biggest heal first, never a quest tool, never what is not food.
func TestRankFloorFoodsEatsAnyCarriedFoodButNoQuestTool(t *testing.T) {
	const bread, trout, shark, cake, logs, coins, raw = 2309, 333, 385, 1891, 1511, 995, 317
	carried := []FloorFood{
		{ID: coins, Name: "Coins", Count: 5000},
		{ID: bread, Name: "Bread", Count: 15},
		{ID: raw, Name: "Raw shrimps", Count: 3},
		{ID: shark, Name: "Shark", Count: 2},
		{ID: logs, Name: "Logs", Count: 4},
		{ID: cake, Name: "2/3 cake", Count: 1},
		{ID: trout, Name: "Trout", Count: 0},
	}
	got := RankFloorFoods(carried, nil)
	var ids []int
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	// Shark 20, 2/3 cake 4 (a cake bite), Bread 4: the sharks first, then the pack's own order among equal heals.
	if want := []int{shark, bread, cake}; len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] || ids[2] != want[2] {
		t.Fatalf("ranked %v, want %v (coins, raw shrimps, logs and an empty stack are not food)", ids, want)
	}

	// Merlin's bread is a quest item (merlinTools): it is left to the quest, the sharks and the cake are not.
	got = RankFloorFoods(carried, func(id int) bool { return id == bread })
	ids = ids[:0]
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	if len(ids) != 2 || ids[0] != shark || ids[1] != cake {
		t.Errorf("with bread a tool: ranked %v, want the shark then the cake", ids)
	}

	// Nothing edible, or everything a tool: nothing to eat.
	if got := RankFloorFoods(carried[:1], nil); len(got) != 0 {
		t.Errorf("coins ranked as food: %v", got)
	}
	if got := RankFloorFoods(carried, func(int) bool { return true }); len(got) != 0 {
		t.Errorf("every stack a tool, yet ranked %v", got)
	}
}
