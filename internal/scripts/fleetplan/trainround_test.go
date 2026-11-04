package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Paladin thief's walk: six chaos in the pack (three pockets), a 100-cast minimum, a Paladin
// wait rung. Before the round rule this walked ~120 tiles to the pen to cast six Wind Bolts.
func TestStage3SmallRoundThievesOnWhenTheRungPaysTheRune(t *testing.T) {
	at := func(tt tile.Tile) *tile.Tile { return &tt }
	s := Stage3Snapshot{Tile: at(TrainStand), HP: 20, MaxHP: 20, MagicLevel: 37, MagicTarget: 66,
		HaveStaff: true, TrainCasts: 6, MinTrainCasts: 100, CastsToTarget: 23000, WaitRungPaysTrainRune: true, Food: 8}
	if got := DecideStage3Phase(s); got != Phase3Thieve {
		t.Fatalf("6 casts held, a 100-cast round, a Paladin rung: %s, want %s (thieve on and walk once, full)", got, Phase3Thieve)
	}
	s.TrainCasts = 100
	if got := DecideStage3Phase(s); got != Phase3Train {
		t.Fatalf("a full round: %s, want %s", got, Phase3Train)
	}
	// Part-way through a round the account finishes it rather than walking back to thieve.
	s.TrainCasts, s.Training = 6, true
	if got := DecideStage3Phase(s); got != Phase3Train {
		t.Fatalf("mid-round with 6 left: %s, want %s", got, Phase3Train)
	}
}

// A small holding goes to the supply ladder FIRST (a bigger round), and never idles when nothing
// is due and thieving pays nothing towards the spell.
func TestStage3SmallRoundFillsFromTheLadderThenCastsRatherThanIdle(t *testing.T) {
	at := func(tt tile.Tile) *tile.Tile { return &tt }
	base := Stage3Snapshot{Tile: at(TrainStand), HP: 20, MaxHP: 20, MagicLevel: 30, MagicTarget: 66,
		HaveStaff: true, TrainCasts: 30, MinTrainCasts: 100, CastsToTarget: 24000, Food: 8}
	cases := []struct {
		name string
		mod  func(*Stage3Snapshot)
		want Phase3
	}{
		{"market due: buy before the walk", func(s *Stage3Snapshot) { s.MarketDue = true }, Phase3Market},
		{"mule due with a market lot wanted", func(s *Stage3Snapshot) { s.MarketDue, s.MuleAskDue = true, true }, Phase3Mule},
		{"Lundail trip due", func(s *Stage3Snapshot) { s.TripDue = true }, Phase3Kit},
		{"Aubury due", func(s *Stage3Snapshot) { s.AuburyDue = true }, Phase3Aubury},
		{"nothing due, a coin rung: cast what is held", func(s *Stage3Snapshot) {}, Phase3Train},
		{"nothing due, a coin rung, hungry: food first", func(s *Stage3Snapshot) { s.FoodWanted, s.Food = 8, 0 }, Phase3Food},
		{"the last casts to the target are a round", func(s *Stage3Snapshot) { s.CastsToTarget, s.MarketDue = 25, true }, Phase3Train},
		{"minimum off: the old any-cast rule", func(s *Stage3Snapshot) { s.MinTrainCasts, s.MarketDue = 0, true }, Phase3Train},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		if got := DecideStage3Phase(s); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestTrainRoundReady(t *testing.T) {
	cases := []struct {
		held, min, left int
		training, want  bool
	}{
		{0, 100, 5000, false, false},
		{0, 100, 5000, true, false},
		{99, 100, 5000, false, false},
		{100, 100, 5000, false, true},
		{6, 100, 5000, true, true},
		{6, 0, 5000, false, true},
		{6, 100, 6, false, true},
		{6, 100, 0, false, false}, // unknown distance to the target: the minimum stands
	}
	for _, c := range cases {
		s := Stage3Snapshot{TrainCasts: c.held, MinTrainCasts: c.min, CastsToTarget: c.left, Training: c.training}
		if got := TrainRoundReady(s); got != c.want {
			t.Errorf("held %d, min %d, left %d, training %v: %v, want %v", c.held, c.min, c.left, c.training, got, c.want)
		}
	}
}

func BoltSpell(t *testing.T, name string) Spell {
	t.Helper()
	sp, ok := SpellByName(name)
	if !ok {
		t.Fatalf("no spell %q", name)
	}
	return sp
}
