package runerunnerplan

import "testing"

func TestHopWorthy(t *testing.T) {
	aemad := AmmoHopStop{Key: "Aemad@1", Coins: 2900, MinVisit: 500}
	khazard := AmmoHopStop{Key: "PortKhazard@1", Coins: 400, MinVisit: 1000}
	cases := []struct {
		name  string
		runes int
		ammo  []AmmoHopStop
		want  bool
	}{
		{"runes alone", 300, nil, true},
		{"the 5 Nov block: no Lundail runes, Aemad's shelf", 0, []AmmoHopStop{khazard, aemad}, true},
		{"a stop under its own minimum does not count", 0, []AmmoHopStop{khazard}, false},
		{"small ammo adds up only from stops that would be made", 0, []AmmoHopStop{{Key: "a", Coins: 1200, MinVisit: 500}, {Key: "b", Coins: 900, MinVisit: 500}}, true},
		{"too little of everything", 100, []AmmoHopStop{{Key: "a", Coins: 800, MinVisit: 500}}, false},
	}
	for _, c := range cases {
		got, why := HopWorthy(c.runes, 200, c.ammo)
		if got != c.want {
			t.Errorf("%s: HopWorthy = %v (%s), want %v", c.name, got, why, c.want)
		}
	}
}
