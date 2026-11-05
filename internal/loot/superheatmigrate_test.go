package loot

import "testing"

func TestMigrateMovesOldSuperheatRowsToSmithing(t *testing.T) {
	for _, action := range []string{ActionSuperheat, ActionSuperheat + runesSuffix} {
		old := Source{Kind: KindProduction, Action: action, Name: "Adamantite ore", ID: 2361, Skill: SkillMagic}
		got := Migrate(old)
		if got.Skill != SkillSmithing {
			t.Errorf("%s stayed on %q", action, got.Skill)
		}
		if got.Key() != old.Key() {
			t.Errorf("%s changed key to %q", action, got.Key())
		}
	}
	alch := Source{Kind: KindProduction, Action: ActionAlch, Name: "Yew longbow", Skill: SkillMagic}
	if got := Migrate(alch); got.Skill != SkillMagic {
		t.Errorf("alchemy moved to %q", got.Skill)
	}
}

func TestSuperheatCastDropIsAMagicMirrorWithTheBill(t *testing.T) {
	d := SuperheatCastDrop(2361, "Adamantite ore", 10, false)
	if d.Events != 10 || d.Source.Skill != SkillMagic || !d.Source.MirrorsCost() || len(d.Items) != 0 {
		t.Fatalf("drop %+v", d)
	}
	got := map[string]int64{}
	for _, it := range d.Spent {
		got[it.Name] = it.Count
	}
	if got["Adamantite ore"] != 10 || got["Coal"] != 60 || got["Nature rune"] != 10 || got["Fire rune"] != 40 {
		t.Errorf("spent %v", got)
	}
	staff := SuperheatCastDrop(2361, "Adamantite ore", 10, true)
	for _, it := range staff.Spent {
		if it.Name == "Fire rune" {
			t.Error("fire staff paid, but Fire runes counted")
		}
	}
	if got := SuperheatCastDrop(2361, "x", 0, true); got.Events != 0 {
		t.Errorf("zero casts: %+v", got)
	}
}

func TestSuperheatMirrorClaimsMagicXPButCombatMirrorDoesNot(t *testing.T) {
	if stat, ok := ClaimsExperience(SuperheatSpell()); !ok || stat != 6 {
		t.Errorf("superheat mirror claims %d %v", stat, ok)
	}
	if _, ok := ClaimsExperience(CombatSpells()); ok {
		t.Error("combat mirror claims experience")
	}
	if SuperheatSpell().Key() == SuperheatCast(2361, "Adamantite ore", true).Key() {
		t.Error("mirror shares the Smithing row's key")
	}
	if got := Migrate(SuperheatSpell()); got.Skill != SkillMagic {
		t.Errorf("mirror migrated to %q", got.Skill)
	}
	if _, ok := SuperheatSpell().Consumes(); ok {
		t.Error("mirror consumes a fixed input")
	}
}
