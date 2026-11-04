package fleetplan

import (
	"testing"
)

// mustSpell is SpellByName for a table that names a spell the ladder is known to hold.
func mustSpell(name string) Spell {
	sp, ok := SpellByName(name)
	if !ok {
		panic("no spell " + name)
	}
	return sp
}

func spellNamed(t *testing.T, name string) Spell {
	t.Helper()
	return mustSpell(name)
}
