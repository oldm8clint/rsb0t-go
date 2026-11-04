package combatplan

import (
	"strings"
	"testing"
)

// A shop LISTING a rune and a shop HAVING one are different facts, and only the run finds out
// which. shopRunes is read off the .inv and is right about the line; it says nothing about the
// shelf, and a drained Aubury sells nothing at all.
func TestShopRunesExceptStrikesTheEmptyShelf(t *testing.T) {
	out := ShopRunesExcept(map[string]bool{"death rune": true})
	if out["death rune"] {
		t.Fatal("a struck line came back buyable")
	}
	if !out["chaos rune"] || !out["air rune"] {
		t.Fatal("striking one line must not disturb the others")
	}
	// The shelf handed back is a copy, so one run's discovery must not poison the next caller.
	if !ShopRunes()["death rune"] {
		t.Fatal("ShopRunesExcept mutated the package's own shelf")
	}
	// Only a true entry is a strike. combattrainer's unavailable map is written by observation,
	// and a false value means "never seen empty", not "empty".
	if !ShopRunesExcept(map[string]bool{"death rune": false})["death rune"] {
		t.Fatal("a false entry should not strike the line")
	}
}

// A run that has learned nothing must behave exactly as it did before this existed.
func TestShopRunesExceptWithNothingStruckMatchesShopRunes(t *testing.T) {
	base, out := ShopRunes(), ShopRunesExcept(nil)
	if len(base) != len(out) {
		t.Fatalf("ShopRunes has %d lines, ShopRunesExcept(nil) has %d", len(base), len(out))
	}
	for k := range base {
		if base[k] != out[k] {
			t.Fatalf("line %q differs with nothing struck", k)
		}
	}
}

// The regression this whole change exists for. A death-rune stockout used to leave the run
// choosing Fire Blast forever: the server refuses the autocast at SET time, pointStaff returns
// false, Loop returns without fighting, and the 10-minute shop cadence retries it for the rest
// of the session. It must step DOWN the spell ladder instead, because slower training beats
// none at all.
func TestPlanMagicSpellDegradesWhenTheShelfIsEmpty(t *testing.T) {
	spells := contentSpells()

	full, ok := PlanMagicSpell(75, false, spells, "Fire rune", ShopRunes())
	if !ok || full.Name != "Fire Blast" {
		t.Fatalf("with a full shelf: got %q %v, want Fire Blast", full.Name, ok)
	}

	// Death runes gone. Fire Blast (59) and Wind Blast (41) both need one, so the best spell
	// still in runes is Fire Bolt on a chaos rune.
	noDeath := ShopRunesExcept(map[string]bool{"death rune": true})
	got, ok := PlanMagicSpell(75, false, spells, "Fire rune", noDeath)
	if !ok || got.Name != "Fire Bolt" {
		t.Fatalf("with no death rune: got %q %v, want Fire Bolt", got.Name, ok)
	}

	// Chaos runes gone too: down to Fire Strike on a mind rune rather than to nothing.
	noChaos := ShopRunesExcept(map[string]bool{"death rune": true, "chaos rune": true})
	got, ok = PlanMagicSpell(75, false, spells, "Fire rune", noChaos)
	if !ok || got.Name != "Fire Strike" {
		t.Fatalf("with no death or chaos rune: got %q %v, want Fire Strike", got.Name, ok)
	}
}

// The two magic ceilings have different causes and must not borrow each other's explanation.
// Sending whoever reads the log to the Magic Guild's door when the truth is "Aubury sold out"
// throws away the one piece of information the run actually had.
func TestCheckMagicCeilingExceptBlamesTheStockoutNotTheMagicGuild(t *testing.T) {
	spells := contentSpells()

	c := CheckMagicCeilingExcept(75, false, spells, "Fire rune", map[string]bool{"death rune": true})
	if !c.Stalled {
		t.Fatal("Fire Blast is castable at 75 and its death rune is struck, so the ceiling should be announced")
	}
	if !strings.Contains(c.Reason, "death rune") {
		t.Fatalf("the reason must name the struck rune: %s", c.Reason)
	}
	if strings.Contains(c.Reason, "Magic Guild") {
		t.Fatalf("an observed stockout must not be blamed on the Magic Guild's door: %s", c.Reason)
	}
	if c.BestWeaponTier != "Fire Bolt" {
		t.Fatalf("the suppliable spell = %q, want Fire Bolt", c.BestWeaponTier)
	}

	// With nothing struck, the static-shelf explanation is still the correct one.
	plain := CheckMagicCeilingExcept(62, true, spells, "Fire rune", nil)
	if !plain.Stalled || !strings.Contains(plain.Reason, "Magic Guild") {
		t.Fatalf("the blood-rune ceiling should still name the Magic Guild: %+v", plain)
	}
}
