package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestDecideStage3PhaseTrainsFirst(t *testing.T) {
	at := func(tt tile.Tile) *tile.Tile { return &tt }
	base := Stage3Snapshot{Tile: at(TrainStand), HP: 10, MaxHP: 10, MagicLevel: 5, MagicTarget: 13, Food: 5}
	cases := []struct {
		name string
		mod  func(*Stage3Snapshot)
		want Phase3
	}{
		{"no staff, mule due", func(s *Stage3Snapshot) { s.MuleAskDue = true }, Phase3Mule},
		{"no staff", func(s *Stage3Snapshot) {}, Phase3Staff},
		{"staff and runes: train", func(s *Stage3Snapshot) { s.HaveStaff, s.TrainCasts = true, 50 }, Phase3Train},
		{"training and hurt: eat", func(s *Stage3Snapshot) { s.HaveStaff, s.TrainCasts, s.HP, s.EatBelowHP, s.Food = true, 50, 3, 6, 2 }, Phase3Eat},
		{"staff, no runes, trip due", func(s *Stage3Snapshot) { s.HaveStaff, s.TripDue = true, true }, Phase3Kit},
		{"staff, no runes, market cheaper", func(s *Stage3Snapshot) { s.HaveStaff, s.MarketDue, s.TripDue = true, true, true }, Phase3Market},
		{"coins short with a mule", func(s *Stage3Snapshot) { s.HaveStaff, s.CoinsShort, s.MuleAskDue = true, true, true }, Phase3Mule},
		{"trained: stock the KQ spell", func(s *Stage3Snapshot) { s.MagicLevel, s.TripDue = 13, true }, Phase3Kit},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		if got := DecideStage3Phase(s); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if RegionOf(at(TrainStand)) != Region3Ardougne {
		t.Fatal("the cow field is home ground")
	}
}
