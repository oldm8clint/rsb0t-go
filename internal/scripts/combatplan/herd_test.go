package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestHerdKeyAndMatch(t *testing.T) {
	if got := HerdKey("Barbarian", " Barbarian woman ", ""); got != "Barbarian|Barbarian woman" {
		t.Fatalf("HerdKey = %q", got)
	}
	cases := []struct {
		key, name string
		want      bool
	}{
		{"Barbarian|Barbarian woman", "barbarian WOMAN", true},
		{"Barbarian|Barbarian woman", "Barbarian", true},
		{"Barbarian|Barbarian woman", "Gunthor the Brave", false},
		{"Cow", "cow", true}, // a plain name is a herd of one, as every caller before herds passed
		{"Cow", "Calf", false},
		{"", "Cow", false}, // an empty key matches nothing, not everything
		{"Cow", "", false},
	}
	for _, c := range cases {
		if got := HerdMatch(c.key, c.name); got != c.want {
			t.Errorf("HerdMatch(%q, %q) = %v, want %v", c.key, c.name, got, c.want)
		}
	}
	if got := HerdNames(" Ogre | |Ogre "); len(got) != 2 || got[0] != "Ogre" {
		t.Errorf("HerdNames = %v", got)
	}
}

// a user request: "accounts training at barbarians dont consider attacking the barbarian women". The
// rung's monster is the herd of both, and the five women's spawns (content m48_53, npc 17) are in its supply,
// every one inside the rung's leash of its stand so a target search from the stand finds them.
func TestBarbariansRungIncludesTheWomen(t *testing.T) {
	m := MonsterBarbarian
	if !m.Is("Barbarian woman") || !m.Is("Barbarian") || m.Is("Gunthor the Brave") {
		t.Fatalf("herd = %v", m.Names())
	}
	if got := m.HerdKey(); got != "Barbarian|Barbarian woman" {
		t.Fatalf("HerdKey = %q", got)
	}
	var rung Rung
	for _, r := range SpreadLadder() {
		if r.Key == "barbarians" {
			rung = r
		}
	}
	if rung.Key == "" {
		t.Fatal("no barbarians rung in the spread ladder")
	}
	if rung.SpawnCount() != 16 {
		t.Fatalf("%d spawns, want 11 Barbarians + 5 women = 16", rung.SpawnCount())
	}
	for _, sp := range rung.Spawns {
		if d := tile.Chebyshev(sp, rung.Stand); d > rung.Leash {
			t.Errorf("spawn %v is %d tiles from the stand, leash %d", sp, d, rung.Leash)
		}
	}
	// The women have the Barbarian's hitpoints and respawn (content: 14 and 50), so one supply formula
	// serves the whole herd.
	if m.Hitpoints != 14 || m.RespawnTicks != 50 {
		t.Fatalf("the herd's numbers moved: %+v", m)
	}
	// Every other monster is a herd of one.
	if got := MonsterOgre.HerdKey(); got != "Ogre" || MonsterOgre.Is("Barbarian") {
		t.Fatalf("Ogre HerdKey = %q", got)
	}
}
