package woodcutplan

import "testing"

func TestAxeHeadSlotsNeeded(t *testing.T) {
	for _, c := range []struct {
		headHeld, handleWorn bool
		want                 int
	}{{false, false, 1}, {true, false, 0}, {false, true, 2}, {true, true, 1}} {
		if got := AxeHeadSlotsNeeded(c.headHeld, c.handleWorn); got != c.want {
			t.Errorf("AxeHeadSlotsNeeded(head held %v, handle worn %v) = %d, want %d", c.headHeld, c.handleWorn, got, c.want)
		}
	}
}

func TestSpareForAxeHead(t *testing.T) {
	for name, want := range map[string]bool{
		"Logs": true, "Yew logs": true, "Maple longbow": true, "Oak shortbow": true, "Longbow": true,
		"Knife": false, "Coins": false, "Rune axe": false, "Axe handle": false, "Axe head": false,
		"Arrow shaft": false, "Bow string": false,
	} {
		if got := SpareForAxeHead(name); got != want {
			t.Errorf("SpareForAxeHead(%q) = %v, want %v", name, got, want)
		}
	}
}
