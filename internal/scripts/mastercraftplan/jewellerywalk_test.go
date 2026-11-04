package mastercraftplan

import "testing"

// With the walking enchants on, the pieces made at the furnace are taken to the bank (the next walk of the round)
// and cast on the way; the step at the bank casts what is left; a bank, or anywhere off the Falador side, casts where
// the account stands (the pieces there came out of the bank itself).
func TestWalkEnchantTakesThePiecesForAWalk(t *testing.T) {
	carrying := JewelleryHeld{Necklaces: 10, Cosmic: 10, Water: 10}
	for _, tc := range []struct {
		name string
		v    JewelleryView
		want JewelleryStep
	}{
		{"at the furnace with the walk on: to the bank, enchanting on the way", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork, v.WalkEnchant = carrying, JewelleryHeld{}, JewelleryAtFurnace, true, true
		}), JewelleryStepToWork},
		{"the same with the walk off: enchant standing, as before", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork = carrying, JewelleryHeld{}, JewelleryAtFurnace, true
		}), JewelleryStepEnchant},
		{"on the Falador side off the road, walk on: to the bank", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork, v.WalkEnchant = carrying, JewelleryHeld{}, JewelleryAtOther, true, true
		}), JewelleryStepToWork},
		{"at the work bank, walk on: enchant what is left before the deposit", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork, v.WalkEnchant = carrying, JewelleryHeld{}, JewelleryAtWorkBank, true, true
		}), JewelleryStepEnchant},
		{"at the post, walk on: nowhere to walk, enchant", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.WalkEnchant = carrying, JewelleryHeld{}, true
		}), JewelleryStepEnchant},
		{"off the Falador side, walk on: enchant where it stands", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.WalkEnchant = carrying, JewelleryHeld{}, JewelleryAtOther, true
		}), JewelleryStepEnchant},
		{"no Cosmic runes in the pack, walk on: nothing to cast, so the wrap-up walk to the bank as before", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork, v.WalkEnchant = JewelleryHeld{Necklaces: 10, Water: 10}, JewelleryHeld{}, JewelleryAtFurnace, true, true
		}), JewelleryStepToWork},
		{"bars still to craft come first", jview(func(v *JewelleryView) {
			v.Pack, v.Bank, v.Where, v.NearWork, v.WalkEnchant = JewelleryHeld{Mould: 1, Bars: 5, Sapphires: 5, Necklaces: 5, Cosmic: 10, Water: 10}, JewelleryHeld{}, JewelleryAtFurnace, true, true
		}), JewelleryStepCraft},
	} {
		if got := NextJewelleryStep(tc.v); got != tc.want {
			t.Errorf("%s: step %s, want %s", tc.name, got, tc.want)
		}
	}
}

// A load's whole order with the walk on: craft at the furnace, walk to the bank (enchanting), enchant what is left at
// the bank, bank. The enchant step is still there, at the bank, for the pieces the walk did not reach.
func TestWalkEnchantOrderOfALoad(t *testing.T) {
	v := jview(func(v *JewelleryView) {
		v.WalkEnchant, v.NearWork, v.Where = true, true, JewelleryAtFurnace
		v.Pack = JewelleryHeld{Mould: 1, Bars: 10, Sapphires: 10, Cosmic: 10, Water: 10}
		v.Bank = JewelleryHeld{}
	})
	if got := NextJewelleryStep(v); got != JewelleryStepCraft {
		t.Fatalf("loaded at the furnace: %s", got)
	}
	v.Pack = JewelleryHeld{Mould: 1, Necklaces: 10, Cosmic: 10, Water: 10} // crafted
	if got := NextJewelleryStep(v); got != JewelleryStepToWork {
		t.Fatalf("crafted: %s", got)
	}
	v.Where = JewelleryAtWorkBank
	v.Pack = JewelleryHeld{Mould: 1, Necklaces: 6, Cosmic: 6, Water: 6} // four cast on the walk
	if got := NextJewelleryStep(v); got != JewelleryStepEnchant {
		t.Fatalf("arrived with six left: %s", got)
	}
	v.Pack = JewelleryHeld{Mould: 1, Games: 10}
	if got := NextJewelleryStep(v); got != JewelleryStepBank {
		t.Fatalf("all cast: %s", got)
	}
}
