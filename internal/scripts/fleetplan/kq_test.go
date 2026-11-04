package fleetplan

import (
	"testing"
)

func TestKitCoinsCoverTheWalk(t *testing.T) {
	st := DefaultKQSettings()
	water := st.Waterskins * (WaterskinCost + 5)
	if got := KitCoins(KQRegionAlKharid, st); got != ShantayPassCost+st.SpareCoins+water {
		t.Errorf("Al Kharid kit coins = %d", got)
	}
	if got := KitCoins(KQRegionElsewhere, st); got != ShantayPassCost+st.SpareCoins+water+TollGateCost {
		t.Errorf("Lumbridge-side kit coins = %d", got)
	}
	if got := KitCoins(KQRegionWest, st); got != ShantayPassCost+st.SpareCoins+water+TollGateCost+ShipFares {
		t.Errorf("Ardougne kit coins = %d", got)
	}
}

// The Queen's spell defaults to Stage 3's own target spell: kqSpell unset means stage3Spell, so an
// account casts at her exactly what it stocked for.
func TestKQSpellDefaultsToTheStage3Spell(t *testing.T) {
	kq, st := DefaultKQSettings(), DefaultStage3Settings()
	if kq.Spell != "" {
		t.Fatalf("DefaultKQSettings().Spell = %q, want empty (follow stage3Spell)", kq.Spell)
	}
	for _, name := range []string{"Fire Strike", "Wind Bolt"} {
		st.Spell = name
		if got := kq.SpellFor(st); got.Name != st.TargetSpell().Name {
			t.Fatalf("stage3Spell %s: the Queen's spell is %s", name, got.Name)
		}
	}
}
