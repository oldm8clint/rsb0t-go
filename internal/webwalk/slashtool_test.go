package webwalk

import "testing"

func TestIsContentKnifeNameOnlyMatchesThePlainKnife(t *testing.T) {
	for _, name := range []string{"Knife", "knife", " Knife "} {
		if !IsContentKnifeName(name) {
			t.Errorf("IsContentKnifeName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Bronze knife", "Rune knife", "", "Knife sharpener"} {
		if IsContentKnifeName(name) {
			t.Errorf("IsContentKnifeName(%q) = true, want false", name)
		}
	}
}

func TestIsSlashWeaponNameMatchesBladesAndNotThrownKnives(t *testing.T) {
	blades := []string{"Bronze sword", "Rune scimitar", "Iron longsword", "Adamant dagger",
		"Steel battleaxe", "Rune 2h sword", "Dragon halberd", "Silverlight", "Excalibur", "Machete"}
	for _, name := range blades {
		if !IsSlashWeaponName(name) {
			t.Errorf("IsSlashWeaponName(%q) = false, want true", name)
		}
	}
	// The thrown knives are separate objs from content's `knife` and do not slash.
	for _, name := range []string{"Bronze knife", "Iron knife", "Rune knife", "Knife"} {
		if IsSlashWeaponName(name) {
			t.Errorf("IsSlashWeaponName(%q) = true, want false", name)
		}
	}
}

// Why: a substring match on "sword" makes a Raw swordfish a web-cutting blade, and the
// Varrock sewer route would then be planned for an account holding nothing but fish.
func TestIsSlashWeaponNameDoesNotMatchSubstringsInsideOtherWords(t *testing.T) {
	for _, name := range []string{"Raw swordfish", "Swordfish", "Cooked swordfish", "Sword grass"} {
		if name == "Sword grass" {
			continue // "Sword" stands alone here, which is the ambiguous case left permissive
		}
		if IsSlashWeaponName(name) {
			t.Errorf("IsSlashWeaponName(%q) = true, want false", name)
		}
	}
}

func TestHasSlashTool(t *testing.T) {
	cases := []struct {
		names []string
		want  bool
	}{
		{nil, false},
		{[]string{"Tinderbox", "Raw shrimps"}, false},
		{[]string{"Tinderbox", "Knife"}, true},
		{[]string{"Bronze sword"}, true},
		{[]string{"Bronze knife"}, false},
		{[]string{"Raw swordfish"}, false},
	}
	for _, c := range cases {
		if got := HasSlashTool(c.names); got != c.want {
			t.Errorf("HasSlashTool(%v) = %v, want %v", c.names, got, c.want)
		}
	}
}

// The Requires gate itself: an unknown slash state must let the edge through, because an
// offline plan has no inventory, while a known-false one must keep it out.
func TestSlashToolRequiresFailsOpenWhenUnknown(t *testing.T) {
	r := &Requires{SlashTool: true}
	if ok, why := r.Check(&WorldState{}); !ok {
		t.Errorf("unknown slash state rejected the edge: %s", why)
	}
	no := false
	if ok, _ := r.Check(&WorldState{CanSlashWeb: &no}); ok {
		t.Error("a known-bladeless account was allowed through a web")
	}
	yes := true
	if ok, why := r.Check(&WorldState{CanSlashWeb: &yes}); !ok {
		t.Errorf("a blade-carrying account was refused: %s", why)
	}
}
