package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func teleOn() TeleTravelSettings { return TeleTravelSettings{Enabled: true} }

func plenty() RuneStock {
	return RuneStock{"Law rune": 20, "Fire rune": 20, "Earth rune": 20, "Water rune": 20}
}

func TestTravelTeleportsMatchContent(t *testing.T) {
	want := map[string][3]int{ // level, landing x, landing z (magic_spells.dbrow tele_coord)
		"Varrock Teleport": {25, 3213, 3424}, "Lumbridge Teleport": {31, 3221, 3218},
		"Falador Teleport": {37, 2965, 3378}, "Camelot Teleport": {45, 2757, 3478},
		"Ardougne Teleport": {51, 2661, 3301},
	}
	got := TravelTeleports()
	if len(got) != len(want) {
		t.Fatalf("%d travel teleports, want %d", len(got), len(want))
	}
	for _, tp := range got {
		w, ok := want[tp.Spell.Name]
		if !ok || tp.Spell.Level != w[0] || tp.DestX != w[1] || tp.DestZ != w[2] {
			t.Errorf("%s: level %d at %d,%d, want %v", tp.Spell.Name, tp.Spell.Level, tp.DestX, tp.DestZ, w)
		}
	}
	if ArdougneTeleport.Quest != "Plague City" {
		t.Error("Ardougne Teleport lost its Plague City gate")
	}
	// The training mode's own list must not grow Ardougne (it has no quest gate there).
	for _, tp := range Teleports {
		if tp.Spell.Name == ArdougneTeleport.Spell.Name {
			t.Error("Ardougne Teleport leaked into Teleports")
		}
	}
}

func lineFor(lines []AskLine, item string) (AskLine, bool) {
	for _, l := range lines {
		if l.Choices[0] == item {
			return l, true
		}
	}
	return AskLine{}, false
}

// A Rune Mysteries leg from Ardougne to Duke Horacio upstairs in Lumbridge Castle (level 1): no
// walk joins Kandarin to Asgarnia while wolf territory is avoided, so the leg must start with a
// teleport that lands in Lumbridge.
func TestChooseTravelTeleportArdougneToDukeHoracio(t *testing.T) {
	c, ok := ChooseTravelTeleport(teleOn(), TeleTravelInput{Here: tile.Tile{X: 2662, Z: 3305},
		Dest: tile.Tile{X: 3212, Z: 3220, Level: 1}, Magic: 45, Members: true, Pack: plenty(), Staff: StaffOfAir})
	if !ok || c.Teleport.Spell.Name != "Lumbridge Teleport" {
		t.Fatalf("got %+v ok=%v, want Lumbridge Teleport", c, ok)
	}
}
