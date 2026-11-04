package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestTrainFloorHP(t *testing.T) {
	cases := []struct{ maxHP, eat, want int }{
		{10, 6, 5},  // half of ten; the eat setting less one is also 5
		{13, 6, 7},  // half, rounded up
		{40, 6, 20}, // a Stage 5 account keeps half its health
		{10, 9, 8},  // a higher eat setting raises the floor
		{4, 6, 3},   // never at or above MaxHP
		{5, 0, 3},   // eat 0 (never eat) still gets a floor
		{1, 6, 0},   // no room for a floor
		{0, 6, 0},   // unknown MaxHP
	}
	for _, c := range cases {
		if got := TrainFloorHP(c.maxHP, c.eat); got != c.want {
			t.Errorf("TrainFloorHP(%d, %d) = %d, want %d", c.maxHP, c.eat, got, c.want)
		}
	}
}

// TestStage3TrainingEatsAtTheFightFloor: a fight broken off at the floor is followed by a bite,
// never by the next fight. Live 26b2816f: the decider ate only below 6 on a 13-HP account, so the
// sixty-second fight began at 6 and ran to 0.
func TestStage3TrainingEatsAtTheFightFloor(t *testing.T) {
	s := Stage3Snapshot{
		Tile: &tile.Tile{X: TrainStand.X, Z: TrainStand.Z}, HP: 7, MaxHP: 13, EatBelowHP: 6,
		MagicLevel: 11, MagicTarget: 41, HaveStaff: true, TrainCasts: 200, Food: 5, FoodWanted: 8,
	}
	if got := DecideStage3Phase(s); got != Phase3Eat {
		t.Fatalf("a 13-HP account at 7 (the floor) chose %s, want %s", got, Phase3Eat)
	}
	s.HP = 8
	if got := DecideStage3Phase(s); got != Phase3Train {
		t.Fatalf("a 13-HP account at 8 chose %s, want %s", got, Phase3Train)
	}
	if e := s.EatEmergencyHP(); e < s.TrainEatBelowHP() {
		t.Fatalf("EatEmergencyHP %d under the training eat line %d: ChooseFood could decline the bite the decider asked for", e, s.TrainEatBelowHP())
	}
}

// TestStage3NeverTrainsOnAnEmptyPack is the owner's rule: no food, no fight. The mule when it is
// due, the food phase when it may run, else Phase3Hungry (the runner thieves for food at the stall).
func TestStage3NeverTrainsOnAnEmptyPack(t *testing.T) {
	base := Stage3Snapshot{
		Tile: &tile.Tile{X: TrainStand.X, Z: TrainStand.Z}, HP: 13, MaxHP: 13, EatBelowHP: 6,
		MagicLevel: 11, MagicTarget: 41, HaveStaff: true, TrainCasts: 200, Food: 0,
	}
	cases := []struct {
		name   string
		mule   bool
		wanted int
		want   Phase3
	}{
		{"mule due", true, 8, Phase3Mule},
		{"food phase may run", false, 8, Phase3Food},
		{"food latched short", false, 0, Phase3Hungry},
	}
	for _, c := range cases {
		s := base
		s.MuleFoodAskDue, s.FoodWanted = c.mule, c.wanted
		if got := DecideStage3Phase(s); got != c.want {
			t.Errorf("%s: chose %s, want %s", c.name, got, c.want)
		}
	}
	// A non-combat mode (alch, teleport) needs no food.
	alch := base
	alch.TrainWithoutFight = true
	if got := DecideStage3Phase(alch); got != Phase3Train {
		t.Fatalf("an alching account with no food chose %s, want %s", got, Phase3Train)
	}
	// One cake is food: the account trains on it (and eats it at the floor).
	fed := base
	fed.Food = 1
	if got := DecideStage3Phase(fed); got != Phase3Train {
		t.Fatalf("an account holding food chose %s, want %s", got, Phase3Train)
	}
}
