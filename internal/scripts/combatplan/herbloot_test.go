package combatplan

import "testing"

func TestHerbsAreLootedByIDRanarrFirst(t *testing.T) {
	herb := func(id int) TrainLootDrop { return TrainLootDrop{Name: "Herb", Cost: 1, ID: id} }
	if !WorthTrainLoot(herb(RanarrHerbID), 1) {
		t.Fatal("a ranarr takes the last free slot")
	}
	if WorthTrainLoot(herb(199), 1) || WorthTrainLoot(herb(199), 3) || !WorthTrainLoot(herb(199), 4) {
		t.Fatal("a guam needs four free slots")
	}
	if WorthTrainLoot(herb(211), 1) || !WorthTrainLoot(herb(211), 2) {
		t.Fatal("an avantoe needs two")
	}
	if WorthTrainLoot(herb(RanarrHerbID), 0) {
		t.Fatal("no free slot, no herb")
	}
	// Without the id the herb is a cost-1 item like any other, below TrainLootMinCost.
	if WorthTrainLoot(TrainLootDrop{Name: "Herb", Cost: 1}, 10) {
		t.Fatal("without an id the herb rule cannot apply")
	}
	if r, ok := HerbRank(RanarrHerbID); !ok || r != 0 || len(HerbIDs) != 14 {
		t.Fatal("ranarr ranks first of the 14 grimy herbs")
	}
	if _, ok := HerbRank(208); ok {
		t.Fatal("the noted ranarr (208) is not a ground herb id")
	}
}
