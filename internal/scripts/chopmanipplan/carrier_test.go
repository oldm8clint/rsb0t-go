package chopmanipplan

import (
	"testing"

	"rsb0t-go/internal/scripts/woodcutplan"
)

func TestPlainLogsAlwaysCarry(t *testing.T) {
	c, ok := PickCarrier(map[string]int{"Logs": 1, "Oak logs": 5}, 1)
	if !ok || c.Logs != "Logs" || c.MakeComp != Make1Shafts {
		t.Fatalf("got %+v %v, want plain logs and the shaft button", c, ok)
	}
	// Whatever the Fletching level: arrow shafts need level 1.
	if _, ok := PickCarrier(map[string]int{"logs": 1}, 0); !ok {
		t.Fatal("plain logs refused at Fletching 0")
	}
}

func TestOtherLogsNeedTheirShortbowLevel(t *testing.T) {
	cases := []struct {
		held      map[string]int
		fletching int
		want      string
		ok        bool
	}{
		{map[string]int{"Oak logs": 3}, 19, "", false},
		{map[string]int{"Oak logs": 3}, 20, "Oak logs", true},
		{map[string]int{"Willow logs": 1}, 34, "", false},
		{map[string]int{"Willow logs": 1}, 35, "Willow logs", true},
		{map[string]int{"Maple logs": 1}, 50, "Maple logs", true},
		{map[string]int{"Yew logs": 1}, 64, "", false},
		{map[string]int{"Yew logs": 1}, 65, "Yew logs", true},
		{map[string]int{"Magic logs": 1}, 79, "", false},
		{map[string]int{"Magic logs": 1}, 99, "Magic logs", true},
		{map[string]int{"Oak logs": 0}, 99, "", false},
		{map[string]int{}, 99, "", false},
		// The cheapest qualifying species carries, so a split batch never fletches the dearest.
		{map[string]int{"Yew logs": 2, "Oak logs": 1, "Willow logs": 1}, 99, "Oak logs", true},
		{map[string]int{"Yew logs": 2, "Oak logs": 1}, 30, "Oak logs", true},
		{map[string]int{"Yew logs": 2, "Willow logs": 1}, 30, "", false},
	}
	for _, c := range cases {
		got, ok := PickCarrier(c.held, c.fletching)
		if ok != c.ok || (ok && (got.Logs != c.want || got.MakeComp != Make1Bow)) {
			t.Errorf("held %v Fletching %d: got %+v %v, want %q %v with the bow button", c.held, c.fletching, got, ok, c.want, c.ok)
		}
	}
}

func TestShortbowLevelsCoverEveryTierLog(t *testing.T) {
	// A tier whose logs are missing from the table would silently never manipulate.
	for _, tier := range woodcutplan.TIERS {
		if tier.Logs == PlainLogs {
			continue
		}
		if _, ok := ShortbowLevel[tier.Logs]; !ok {
			t.Errorf("woodcutplan tier %q logs %q have no ShortbowLevel", tier.Label, tier.Logs)
		}
	}
}
