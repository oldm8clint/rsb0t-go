package combatplan

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestMagicBonusIsContentGroundTruth pins values read from the objects' own param=magicattack
// lines (skill_combat/configs/melee/*.obj, magic/*.obj, enchanted_jewelry.obj).
func TestMagicBonusIsContentGroundTruth(t *testing.T) {
	cases := []struct {
		name  string
		bonus int
		slot  Slot
	}{
		{"Iron platebody", -30, SlotBody},
		{"Rune platebody", -30, SlotBody},
		{"Adamant chainbody", -15, SlotBody},
		{"Mithril platelegs", -21, SlotLegs},
		{"Adamant plateskirt", -21, SlotLegs},
		{"Steel full helm", -6, SlotHelm},
		{"Iron med helm", -3, SlotHelm},
		{"Rune kiteshield", -8, SlotShield},
		{"Steel sq shield", -6, SlotShield},
		{"Wizards robe", 3, SlotBody},
		{"Wizards hat", 2, SlotHelm},
		{"Amulet of magic", 10, SlotAmulet},
		{"Staff of air", 10, SlotWeapon},
		{"Staff", 4, SlotWeapon},
		{"Bronze 2h sword", -4, SlotWeapon},
		{"  adamant PLATEBODY ", -30, SlotBody}, // case and spaces do not matter
	}
	for _, c := range cases {
		got, ok := MagicBonus(c.name)
		if !ok || got != c.bonus || MagicSlot(c.name) != c.slot {
			t.Fatalf("%q: bonus %d ok %v slot %q, want %d %q", c.name, got, ok, MagicSlot(c.name), c.bonus, c.slot)
		}
	}
	for _, none := range []string{"Adamant scimitar", "Cake", "Wooden shield", "Coins", ""} {
		if _, ok := MagicBonus(none); ok {
			t.Fatalf("%q has no magicattack param, got a table row", none)
		}
	}
}

// Every armour piece the combat trainer can buy or wear (the metal tiers' helm, body, legs and
// shield pieces) must read negative: that is the whole reason for the swap, and a piece missing
// from the table would silently stay on.
func TestEveryTrainerArmourPieceCostsMagic(t *testing.T) {
	armour := 0
	for _, it := range Items() {
		switch it.Slot {
		case SlotHelm, SlotBody, SlotLegs, SlotShield:
		default:
			continue
		}
		armour++
		b, ok := MagicBonus(it.Name)
		if !ok || b >= 0 {
			// Rune and dragon pieces the content does not define as objects, and quest ones, are
			// the only tolerated gaps: name them so a new tier cannot slip through.
			t.Logf("armour %q (%s %s) reads magic %d (in table: %v)", it.Name, it.Tier, it.Slot, b, ok)
			if it.Tier != "Rune" && it.Tier != "Dragon" {
				t.Fatalf("armour %q is not a negative magic piece in the table", it.Name)
			}
		}
	}
	if armour < 20 {
		t.Fatalf("only %d armour items checked", armour)
	}
}

func TestMagicSwapRemovalsTakesArmourOffAndLeavesTheWeapon(t *testing.T) {
	worn := []string{"Adamant full helm", "Adamant platebody", "Adamant platelegs", "Adamant kiteshield", "Adamant scimitar", "Amulet of power", "Leather boots"}
	got := MagicSwapRemovals(worn)
	want := []string{"Adamant full helm", "Adamant platebody", "Adamant platelegs", "Adamant kiteshield"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removals %v, want %v", got, want)
	}
	// A two-hander is negative but the weapon slot is the staff's, not this swap's.
	if got := MagicSwapRemovals([]string{"Bronze 2h sword", "Staff of air"}); len(got) != 0 {
		t.Fatalf("removed a weapon: %v", got)
	}
	// Already magic-friendly: nothing to do (idempotent).
	if got := MagicSwapRemovals([]string{"Wizards hat", "Wizards robe", "Staff of air", "Amulet of magic"}); len(got) != 0 {
		t.Fatalf("robes are not armour to remove: %v", got)
	}
	if got := MagicSwapRemovals(nil); len(got) != 0 {
		t.Fatalf("nothing worn: %v", got)
	}
}

func TestMagicWearAddsOnlyPositiveUpgrades(t *testing.T) {
	got := MagicWearAdds(nil, []string{"Iron platebody", "Wizards hat", "Blue hat", "Wizards robe", "Amulet of magic", "Staff of air", "Cake"})
	want := []string{"Wizards hat", "Wizards robe", "Amulet of magic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adds %v, want %v (the iron platebody is negative, the staff is the weapon slot's)", got, want)
	}
	// Worn pieces as good already: no churn.
	if got := MagicWearAdds([]string{"Wizards hat", "Black robe"}, []string{"Wizards hat", "Wizards robe"}); len(got) != 0 {
		t.Fatalf("re-wore an equal piece: %v", got)
	}
	// A positive piece replaces a worse worn one in its slot.
	if got := MagicWearAdds([]string{"Amulet of power"}, []string{"Amulet of magic"}); !reflect.DeepEqual(got, []string{"Amulet of magic"}) {
		t.Fatalf("amulet of magic should beat the amulet of power, got %v", got)
	}
	if got := MagicWearAdds(nil, nil); len(got) != 0 {
		t.Fatalf("holding nothing: %v", got)
	}
}

func TestMeleeRegearDrawsOnlyWhatTheBankHasAndThePackDoesNot(t *testing.T) {
	l := Levels{Attack: 40, Strength: 40, Defence: 40}
	bank := []string{"Adamant platebody", "Adamant platelegs", "Mithril kiteshield", "Adamant full helm", "Cake", "Adamant scimitar"}
	got := MeleeRegear(l, nil, nil, bank)
	names := map[string]bool{}
	for _, it := range got {
		names[it.Name] = true
		if it.Slot == SlotWeapon {
			t.Fatalf("the weapon is the blade logic's, got %q", it.Name)
		}
	}
	for _, n := range []string{"Adamant platebody", "Adamant platelegs", "Adamant full helm"} {
		if !names[n] {
			t.Fatalf("regear %v lacks %q", got, n)
		}
	}
	// Something the pack already holds is the trainer's per-pass EquipPlan's to wear, not a bank draw.
	got = MeleeRegear(l, nil, []string{"Adamant platebody"}, bank)
	for _, it := range got {
		if it.Name == "Adamant platebody" {
			t.Fatalf("drew a platebody the pack already holds: %v", got)
		}
	}
	// Already wearing as good: nothing.
	if got := MeleeRegear(l, []string{"Adamant platebody", "Adamant platelegs", "Adamant full helm", "Adamant kiteshield"}, nil, bank); len(got) != 0 {
		t.Fatalf("regear %v although the account wears its best", got)
	}
	// Levels too low for the tier: the plan only offers what the account may wear.
	if got := MeleeRegear(Levels{Attack: 1, Defence: 1}, nil, nil, []string{"Adamant platebody", "Iron platebody"}); len(got) != 1 || got[0].Name != "Iron platebody" {
		t.Fatalf("at defence 1 only the iron platebody is wearable, got %v", got)
	}
}

// The switch from melee to Magic training ("scarabpens", 7 Oct 2026): what is taken off and what the
// pack's melee kit is, so the kit pass banks it on the same visit.
func TestMagicSwitchKitBanksTheMeleeKit(t *testing.T) {
	worn := []string{"Rune platebody", "Rune platelegs", "Rune kiteshield", "Wizards hat", "Amulet of magic", "Rune scimitar"}
	pack := []string{"Staff of fire", "Shark", "Rune scimitar", "Fire rune", "Mithril chainbody", "Wizards robe"}
	takeOff, bank := MagicSwitchKit(worn, pack)
	if got, want := strings.Join(takeOff, ","), "Rune platebody,Rune platelegs,Rune kiteshield"; got != want {
		t.Errorf("take off %q, want %q", got, want)
	}
	for _, name := range []string{"Rune platebody", "Rune platelegs", "Rune kiteshield", "Rune scimitar", "Mithril chainbody"} {
		if !slices.Contains(bank, name) {
			t.Errorf("%s is not banked: %v", name, bank)
		}
	}
	for _, name := range []string{"Staff of fire", "Shark", "Fire rune", "Wizards robe", "Wizards hat", "Amulet of magic"} {
		if slices.Contains(bank, name) {
			t.Errorf("%s is banked, a caster keeps it: %v", name, bank)
		}
	}
	if len(bank) != 5 {
		t.Errorf("banked %v, each name once", bank)
	}
}

func TestMagicSwitchKitIsEmptyForACaster(t *testing.T) {
	takeOff, bank := MagicSwitchKit([]string{"Wizards hat", "Wizards robe", "Staff of air"}, []string{"Shark", "Law rune", "Yew shortbow"})
	if len(takeOff) != 0 || len(bank) != 0 {
		t.Fatalf("a caster with nothing to bank: take off %v, bank %v", takeOff, bank)
	}
}
