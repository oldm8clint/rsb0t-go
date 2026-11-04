package fleetplan

import (
	"testing"
)

func spellOrDie(t *testing.T, name string) Spell {
	t.Helper()
	sp, ok := SpellByName(name)
	if !ok {
		t.Fatalf("no spell %q", name)
	}
	return sp
}

// guildCapFor is the per-rune cap the plan applies (stage3MaxPriceMultiple times the base cost).
func guildCapFor(st Stage3Settings, rune string) int {
	l, _ := WizardsGuild.Line(rune)
	return l.Cost * st.Normalised().MaxPriceMultiple
}
