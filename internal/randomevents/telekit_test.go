package randomevents

import (
	"reflect"
	"testing"
)

// The ten kinds of Stage 5 quest-hold account that could not pay for a teleport on 17 Oct 2026 (/api/inventory, GET only), and the
// ones that could.
func TestTeleportShortfall(t *testing.T) {
	for _, c := range []struct {
		name      string
		magic     int
		pack      map[string]int
		worn      []string
		wantSpell string
		want      []KitLine
	}{
		{"a full kit with a staff of air", 66, map[string]int{"Law rune": 22, "Air rune": 139, "Fire rune": 600}, []string{"Staff of air"}, "varrock", nil},
		{"a fire staff pays the element", 66, map[string]int{"Law rune": 27, "Air rune": 78}, []string{"Staff of fire"}, "varrock", nil},
		{"no Law rune (the Wizards' Tower death)", 66, map[string]int{"Air rune": 150}, []string{"Staff of fire"}, "varrock", []KitLine{{"Law rune", 1}}},
		{"nothing at all", 66, map[string]int{}, nil, "varrock", []KitLine{{"Law rune", 1}, {"Fire rune", 1}, {"Air rune", 3}}},
		{"no air and a staff of fire", 66, map[string]int{"Law rune": 30}, []string{"Staff of fire"}, "varrock", []KitLine{{"Air rune", 3}}},
		{"two air short", 66, map[string]int{"Law rune": 5, "Air rune": 1, "Fire rune": 2}, nil, "varrock", []KitLine{{"Air rune", 2}}},
		{"a staff of air and no element", 66, map[string]int{"Law rune": 34, "Air rune": 150}, []string{"Staff of air"}, "varrock", []KitLine{{"Fire rune", 1}}},
		{"earth runes make Lumbridge the cheaper set to finish", 66, map[string]int{"Law rune": 3, "Earth rune": 4}, nil, "lumbridge", []KitLine{{"Air rune", 3}}},
		{"Magic under every teleport", 20, map[string]int{}, nil, "", nil},
		{"Magic for Varrock only", 30, map[string]int{"Law rune": 1, "Air rune": 3}, nil, "varrock", []KitLine{{"Fire rune", 1}}},
	} {
		spell, short := teleportShortfall(c.magic, c.pack, c.worn)
		if spell != c.wantSpell || !reflect.DeepEqual(short, c.want) {
			t.Errorf("%s: teleportShortfall = (%q, %v), want (%q, %v)", c.name, spell, short, c.wantSpell, c.want)
		}
	}
}

// The session reader agrees with PickTeleport: an account TeleportShortfall calls complete is one strandSpell can cast.
func TestTeleportShortfallAgreesWithPickTeleport(t *testing.T) {
	for _, pack := range []map[string]int{
		{"Law rune": 1, "Air rune": 3, "Fire rune": 1},
		{"Law rune": 1, "Air rune": 3},
		{"Law rune": 1, "Air rune": 2, "Fire rune": 1},
		{"Law rune": 2, "Earth rune": 1, "Air rune": 9},
	} {
		for _, worn := range [][]string{nil, {"Staff of fire"}, {"Staff of air"}} {
			spell, short := teleportShortfall(66, pack, worn)
			picked, _ := PickTeleport(66, true, pack, worn, nil)
			if (len(short) == 0) != (picked != "") {
				t.Errorf("pack %v worn %v: shortfall (%q, %v) but PickTeleport = %q", pack, worn, spell, short, picked)
			}
			if len(short) == 0 && picked != "" && spell == "" {
				t.Errorf("pack %v worn %v: a spell is castable and the shortfall names none", pack, worn)
			}
		}
	}
}
