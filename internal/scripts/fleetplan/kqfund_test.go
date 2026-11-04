package fleetplan

import (
	"testing"
)

func TestWestFundRung(t *testing.T) {
	if m, ok := WestFundRung(10); ok {
		t.Fatalf("Thieving 10 opens no coin rung in the west, got %s", m.Label)
	}
	if m, ok := WestFundRung(25); !ok || m.Label != "Warrior woman" {
		t.Fatalf("Thieving 25: got %q ok=%v, want Warrior woman", m.Label, ok)
	}
	if m, ok := WestFundRung(55); !ok || m.Label != "Knight of Ardougne" {
		t.Fatalf("Thieving 55: got %q ok=%v, want Knight of Ardougne", m.Label, ok)
	}
	for level := 25; level <= 99; level++ {
		m, ok := WestFundRung(level)
		if !ok {
			t.Fatalf("Thieving %d: no rung", level)
		}
		if m.Label == "Watchman" || m.Label == "Gnome" || m.Label == "Man" || m.Label == "Farmer" {
			t.Fatalf("Thieving %d chose %s, which is not a walk from the East Ardougne market", level, m.Label)
		}
		if m.Level > level {
			t.Fatalf("Thieving %d chose %s at %d", level, m.Label, m.Level)
		}
	}
}
