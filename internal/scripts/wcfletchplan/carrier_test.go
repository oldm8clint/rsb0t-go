package wcfletchplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestCarrierReserveKeepsBackTheOneLogTheKnifeWouldUse(t *testing.T) {
	cases := []struct {
		name     string
		manip    bool
		held     map[string]int
		rung     string
		fletch   int
		reserved int
	}{
		{"manipulation off keeps nothing back", false, map[string]int{"Oak logs": 9}, "Oak logs", 99, 0},
		{"the rung's own logs are the carrier", true, map[string]int{"Oak logs": 9}, "Oak logs", 99, 1},
		{"a plain log is the carrier, so every oak log may be cut", true, map[string]int{"Oak logs": 9, "Logs": 1}, "Oak logs", 99, 0},
		{"on the plain rung the carrier is one of the logs it cuts", true, map[string]int{"Logs": 20}, "Logs", 1, 1},
		{"yew logs the account cannot make a shortbow of carry nothing", true, map[string]int{"Yew logs": 4}, "Yew logs", 60, 0},
		{"yew logs at the level are the carrier", true, map[string]int{"Yew logs": 4}, "Yew logs", 65, 1},
		{"a plain log carries a yew rung that cannot use its own", true, map[string]int{"Yew logs": 4, "Logs": 1}, "Yew logs", 60, 0},
		{"no logs, nothing to keep", true, map[string]int{}, "Oak logs", 99, 0},
	}
	for _, c := range cases {
		if got := CarrierReserve(c.manip, c.held, c.rung, c.fletch); got != c.reserved {
			t.Errorf("%s: reserve %d, want %d", c.name, got, c.reserved)
		}
	}
}

func TestNeedsCarrier(t *testing.T) {
	if NeedsCarrier(false, nil, 99) {
		t.Error("manipulation off needs no carrier")
	}
	if !NeedsCarrier(true, map[string]int{}, 99) {
		t.Error("an empty pack needs one")
	}
	if NeedsCarrier(true, map[string]int{"Willow logs": 1}, 35) {
		t.Error("a willow log at Fletching 35 is a carrier")
	}
	if !NeedsCarrier(true, map[string]int{"Willow logs": 1}, 34) {
		t.Error("a willow log at Fletching 34 is not: the level test would refuse the shortbow")
	}
}

func TestCarrierFromBankTakesAPlainLogFirstThenTheCheapestUsableOne(t *testing.T) {
	for _, c := range []struct {
		name       string
		manip      bool
		pack, bank map[string]int
		fletch     int
		want       string
		ok         bool
	}{
		{"a plain log is banked", true, nil, map[string]int{"Logs": 3, "Oak logs": 40}, 99, "Logs", true},
		{"only own-tier logs are banked", true, map[string]int{}, map[string]int{"Willow logs": 5, "Oak logs": 40}, 99, "Oak logs", true},
		{"a log the level cannot use is left", true, nil, map[string]int{"Yew logs": 12}, 60, "", false},
		{"nothing banked", true, nil, map[string]int{}, 99, "", false},
		{"the pack already has one", true, map[string]int{"Oak logs": 1}, map[string]int{"Logs": 3}, 99, "", false},
		{"manipulation off", false, nil, map[string]int{"Logs": 3}, 99, "", false},
	} {
		got, ok := CarrierFromBank(c.manip, c.pack, c.bank, c.fletch)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestNearestPlainPatchIsTheDeadTreesAtTheChurchYewsAndTheNormalTreesBesideTheOaks(t *testing.T) {
	church, ok := NearestPlainPatch(tile.Tile{X: 2709, Z: 3462})
	if !ok || church.Loc != "Dead tree" {
		t.Fatalf("at the church yews: %+v %v, want the dead trees beside them", church, ok)
	}
	oaks, ok := NearestPlainPatch(tile.Tile{X: 2724, Z: 3488})
	if !ok || oaks.Loc != "Tree" || !oaks.Home {
		t.Fatalf("at the oaks: %+v %v, want the ladder's own normal trees", oaks, ok)
	}
	if p, ok := NearestPlainPatch(tile.Tile{X: 2711, Z: 3508}); ok {
		t.Fatalf("at the willows a plain patch %q was named: they are %d tiles from the nearest, a walk that costs more than the willow's own first log", p.Name, CarrierPatchReach)
	}
	if _, ok := NearestPlainPatch(tile.Tile{X: 2709, Z: 3462, Level: 1}); ok {
		t.Fatal("a patch on another level was named")
	}
}
