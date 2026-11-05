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
