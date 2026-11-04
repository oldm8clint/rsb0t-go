package toadcollectplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

func base() Snapshot {
	return Snapshot{
		Here: Post, FreeSlots: 28, MuleName: "mule", HandOffAt: 24, HandOffEvery: 30 * time.Minute,
		HandOffRetryOK: true,
	}
}

func TestDecideOrder(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Snapshot)
		want StepKind
	}{
		{"kit first", func(s *Snapshot) { s.Kit = true; s.HeldToads = 2 }, StepClear},
		{"skin before pick", func(s *Snapshot) { s.HeldToads = 1; s.Visible = []Spot{Spawns[0]} }, StepSkin},
		{"hand off at count", func(s *Snapshot) { s.HeldLegs = 24 }, StepHandOff},
		{"count includes bank", func(s *Snapshot) { s.HeldLegs = 4; s.BankLegs = 20 }, StepHandOff},
		{"hand off on timer", func(s *Snapshot) { s.HeldLegs = 3; s.SinceHandOff = 31 * time.Minute }, StepHandOff},
		{"no mule never hands", func(s *Snapshot) { s.MuleName = " "; s.HeldLegs = 28; s.FreeSlots = 0 }, StepBank},
		{"cooldown banks full pack", func(s *Snapshot) { s.HeldLegs = 28; s.FreeSlots = 0; s.HandOffRetryOK = false }, StepBank},
		{"full of other waits", func(s *Snapshot) { s.FreeSlots = 0 }, StepWait},
		{"pick visible", func(s *Snapshot) { s.Visible = []Spot{Spawns[3]} }, StepPick},
		{"travel back", func(s *Snapshot) { s.Here = tile.Tile{X: 2300, Z: 3500} }, StepTravel},
		{"wrong level travels", func(s *Snapshot) { s.Here.Level = 1 }, StepTravel},
		{"wait at post", func(s *Snapshot) {}, StepWait},
	}
	for _, c := range cases {
		s := base()
		c.mod(&s)
		if got := Decide(s).Kind; got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestPickNearestFirst(t *testing.T) {
	s := base()
	s.Here = tile.Tile{X: 2428, Z: 3510}
	s.Visible = []Spot{Spawns[0], Spawns[17], Spawns[8]}
	if got := Decide(s).Spot; got != Spawns[17] {
		t.Fatalf("got %+v", got)
	}
}

func TestSpawnsAndPost(t *testing.T) {
	if len(Spawns) != 18 {
		t.Fatalf("spawns %d", len(Spawns))
	}
	seen := map[tile.Tile]bool{}
	for _, sp := range Spawns {
		if seen[sp.Tile] {
			t.Errorf("duplicate %v", sp.Tile)
		}
		seen[sp.Tile] = true
		if tile.Chebyshev(sp.Tile, Post) > TravelRadius {
			t.Errorf("%v outside travel radius", sp.Tile)
		}
	}
}

func TestMuleLoadOnlyLegs(t *testing.T) {
	pack := []donorplan.ItemRow{
		{Name: "Toad's legs", ID: 2152, Count: 1}, {Name: "Toad's legs", ID: 2152, Count: 1},
		{Name: "Swamp toad", ID: 2150, Count: 1}, {Name: "Coins", ID: 995, Count: 10},
	}
	if got := MuleHaul(pack); got != 2 {
		t.Fatalf("haul %d", got)
	}
	legs, toads := Count(pack)
	if legs != 2 || toads != 1 {
		t.Fatalf("count %d %d", legs, toads)
	}
}

func TestDecodeSettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{"handOffAt": "10", "mule": "mule1"})
	if err != nil || s.HandOffAt != 10 || s.Mule != "mule1" || s.HandOffMinutes != 30 {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := DecodeSettings(map[string]interface{}{"bogus": 1}); err == nil {
		t.Error("unknown key accepted")
	}
	if _, err := DecodeSettings(map[string]interface{}{"handOffAt": 29}); err == nil {
		t.Error("29 accepted")
	}
	if _, err := DecodeSettings(map[string]interface{}{"logDetail": "loud"}); err == nil {
		t.Error("bad logDetail accepted")
	}
	if s, err := DecodeSettings(map[string]interface{}{"mule": ""}); err != nil || s.Mule != "" {
		t.Errorf("blank mule: %+v %v", s, err)
	}
}
