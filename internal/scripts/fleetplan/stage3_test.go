package fleetplan

import (
	"testing"
)

// The ladder is the whole point of stage3.go, so it gets checked against the content's own
// numbers rather than trusted. Spot values are quoted from
// content/scripts/skill_combat/configs/magic/magic_combat_spells.dbrow.
func TestCombatSpellsMatchTheContent(t *testing.T) {
	want := []struct {
		name    string
		level   int
		maxHit  int
		members bool
		runes   []Rune
	}{
		{"Wind Strike", 1, 2, false, []Rune{{"Mind rune", 1}, {"Air rune", 1}}},
		{"Fire Strike", 13, 8, false, []Rune{{"Mind rune", 1}, {"Fire rune", 3}, {"Air rune", 2}}},
		{"Wind Bolt", 17, 9, false, []Rune{{"Chaos rune", 1}, {"Air rune", 2}}},
		{"Wind Blast", 41, 13, false, []Rune{{"Death rune", 1}, {"Air rune", 3}}},
		{"Fire Wave", 75, 20, true, []Rune{{"Blood rune", 1}, {"Fire rune", 7}, {"Air rune", 5}}},
	}
	for _, w := range want {
		s, ok := SpellByName(w.name)
		if !ok {
			t.Errorf("%s is missing from the ladder", w.name)
			continue
		}
		if s.Level != w.level || s.MaxHit != w.maxHit || s.Members != w.members {
			t.Errorf("%s = level %d, max hit %d, members %v; want %d / %d / %v",
				w.name, s.Level, s.MaxHit, s.Members, w.level, w.maxHit, w.members)
		}
		if len(s.Runes) != len(w.runes) {
			t.Errorf("%s has %d rune lines, want %d", w.name, len(s.Runes), len(w.runes))
			continue
		}
		for i, r := range w.runes {
			if s.Runes[i] != r {
				t.Errorf("%s rune %d = %+v, want %+v", w.name, i, s.Runes[i], r)
			}
		}
	}
	// The ladder must stay ordered by level, because everything else reads it that way.
	for i := 1; i < len(CombatSpells); i++ {
		if CombatSpells[i].Level < CombatSpells[i-1].Level {
			t.Fatalf("the ladder is out of level order at %s", CombatSpells[i].Name)
		}
	}
	// Every wave spell is members-only on this content, and nothing below is.
	for _, s := range CombatSpells {
		if s.Members && s.Level < 62 {
			t.Errorf("%s is flagged members at level %d; only the wave tier (62+) is", s.Name, s.Level)
		}
	}
}

func TestCanCastAndCastsAffordable(t *testing.T) {
	fire, _ := SpellByName("Fire Strike") // 1 mind + 3 fire + 2 air
	full := RuneStock{"Mind rune": 10, "Fire rune": 30, "Air rune": 20}
	if !fire.CanCast(full, nil) {
		t.Error("Fire Strike is not castable with a full stock")
	}
	// 10 mind / 1, 30 fire / 3, 20 air / 2 -> min(10, 10, 10) = 10
	if got := fire.CastsAffordable(full, nil); got != 10 {
		t.Errorf("CastsAffordable = %d, want 10 (the binding rune)", got)
	}
	// One rune short of any line makes it uncastable.
	short := RuneStock{"Mind rune": 10, "Fire rune": 2, "Air rune": 20}
	if fire.CanCast(short, nil) {
		t.Error("Fire Strike is castable with 2 fire runes and a cost of 3")
	}
	if got := fire.CastsAffordable(short, nil); got != 0 {
		t.Errorf("CastsAffordable with 2 fire runes = %d, want 0", got)
	}
	// A staff of fire makes the fire line free, so the air runes become binding.
	staff := StaffProvides("Staff of fire")
	if got := fire.CastsAffordable(RuneStock{"Mind rune": 100, "Fire rune": 0, "Air rune": 20}, staff); got != 10 {
		t.Errorf("with a staff of fire and no fire runes, CastsAffordable = %d, want 10 (air-bound)", got)
	}
	if !fire.CanCast(RuneStock{"Mind rune": 1, "Fire rune": 0, "Air rune": 2}, staff) {
		t.Error("a staff of fire does not cover the fire rune line")
	}
	// An unknown staff supplies nothing.
	if len(StaffProvides("Plain staff")) != 0 || len(StaffProvides("")) != 0 {
		t.Error("a plain staff should supply no runes")
	}
}

func TestRunesForCastsIsAShoppingList(t *testing.T) {
	fire, _ := SpellByName("Fire Strike")
	list := RunesForCasts(fire, 100, nil)
	want := map[string]int{"Mind rune": 100, "Fire rune": 300, "Air rune": 200}
	if len(list) != len(want) {
		t.Fatalf("shopping list has %d lines, want %d", len(list), len(want))
	}
	for _, r := range list {
		if want[r.Name] != r.Count {
			t.Errorf("%s: %d, want %d", r.Name, r.Count, want[r.Name])
		}
	}
	// The staff's own rune drops out of the list entirely.
	withStaff := RunesForCasts(fire, 100, StaffProvides("Staff of air"))
	for _, r := range withStaff {
		if r.Name == "Air rune" {
			t.Error("a staff of air still appears on the shopping list")
		}
	}
	if len(withStaff) != 2 {
		t.Errorf("with a staff of air the list has %d lines, want 2", len(withStaff))
	}
	// Nonsense counts must not produce negative quantities.
	for _, r := range RunesForCasts(fire, -5, nil) {
		if r.Count < 0 {
			t.Errorf("negative cast count produced %d %s", r.Count, r.Name)
		}
	}
}

func TestScarceRunesAreTheOnesTheSharedShopsCannotSupply(t *testing.T) {
	// Chaos and death are shared stock of 1000 for the whole world; blood is not stocked at all.
	for _, name := range []string{"Chaos rune", "Death rune", "Blood rune"} {
		if !ScarceRunes[name] {
			t.Errorf("%s is not marked scarce", name)
		}
	}
	// The elemental and mind runes are stocked in the thousands and must not be marked scarce,
	// or the preference would have nothing to prefer.
	for _, name := range []string{"Air rune", "Water rune", "Earth rune", "Fire rune", "Mind rune"} {
		if ScarceRunes[name] {
			t.Errorf("%s is marked scarce; Aubury stocks it in the thousands", name)
		}
	}
	// Every strike spell is payable from plentiful runes; every bolt and above is not.
	for _, s := range CombatSpells {
		scarce := s.NeedsScarceRune()
		if s.Level <= 13 && scarce {
			t.Errorf("%s (level %d) needs a scarce rune; the strike tier should not", s.Name, s.Level)
		}
		if s.Level >= 17 && !scarce {
			t.Errorf("%s (level %d) needs no scarce rune; the bolt tier and above should", s.Name, s.Level)
		}
	}
}
