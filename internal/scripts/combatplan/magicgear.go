package combatplan

import "strings"

// Magic attack bonus, and what an account training Magic should not be wearing (a user request:
// "when an account trains Magic ... it must not wear melee armour that gives negative magic attack
// bonus. Swap to magic-friendly gear before casting, and swap back to melee gear for melee slices").
//
// The numbers are not recalled, they are the `param=magicattack,N` line of each object's own
// config under content/scripts (skill_combat/configs/melee/*.obj, magic/*.obj,
// areas/**/enchanted_jewelry.obj and the rest; 240 objects carry one, the other items have none and
// read as 0). A bronze platebody is -30, a platelegs or plateskirt -21, a chainbody -15, a full helm
// -6, a med helm -3, a kiteshield -8, a square shield -6, a Wizards robe +3, a Wizards hat +2, an
// Amulet of magic +10 and a staff +4 to +10. Ordinary swords and scimitars carry none; two-handers
// and halberds are -4 and are not touched here, because a Magic run wields its staff in that slot
// anyway (scarabrunner.PrepareTraining). A table row is by display name, the key every wear and
// bank call here uses; `legs` rows cover plateskirts too.
//
// The rule is deliberately small: take off every worn ARMOUR piece with a negative bonus, put on any
// held piece with a positive one for an emptied slot. Nothing else about the kit changes.

// Slots the magic table needs beyond the melee ones.
const (
	SlotGloves Slot = "gloves"
	SlotBoots  Slot = "boots"
	SlotAmulet Slot = "amulet"
	SlotCape   Slot = "cape"
	SlotOther  Slot = "other"
)

type magicRow struct {
	Name  string
	Slot  Slot
	Bonus int
}

// magicRows is every object with a non-zero magicattack param, from the content configs.
var magicRows = []magicRow{
	{"Cape of guthix", SlotCape, 10},
	{"Cape of saradomin", SlotCape, 10},
	{"Cape of zamorak", SlotCape, 10},
	{"Ranger boots", SlotBoots, -10},
	{"Wizard's boots", SlotBoots, 4},
	{"Amulet of glory", SlotAmulet, 10},
	{"Amulet of glory(1)", SlotAmulet, 10},
	{"Amulet of glory(2)", SlotAmulet, 10},
	{"Amulet of glory(3)", SlotAmulet, 10},
	{"Amulet of glory(4)", SlotAmulet, 10},
	{"Amulet of magic", SlotAmulet, 10},
	{"Amulet of power", SlotAmulet, 6},
	{"Beads of the dead", SlotAmulet, 1},
	{"Unholy symbol", SlotAmulet, 2},
	{"Dragon vambraces", SlotGloves, -10},
	{"Adam full helm (g)", SlotHelm, -6},
	{"Adam full helm (t)", SlotHelm, -6},
	{"Adamant full helm", SlotHelm, -6},
	{"Adamant med helm", SlotHelm, -3},
	{"Archer helm", SlotHelm, -5},
	{"Berserker helm", SlotHelm, -5},
	{"Black full helm", SlotHelm, -6},
	{"Black full helm (g)", SlotHelm, -6},
	{"Black full helm (t)", SlotHelm, -6},
	{"Black med helm", SlotHelm, -3},
	{"Blood'n'tar snelm", SlotHelm, -3},
	{"Blue hat", SlotHelm, 2},
	{"Broken bark snelm", SlotHelm, -3},
	{"Bronze full helm", SlotHelm, -6},
	{"Bronze med helm", SlotHelm, -3},
	{"Bruise blue snelm", SlotHelm, -3},
	{"Coif", SlotHelm, -1},
	{"Dragon med helm", SlotHelm, -3},
	{"Farseer helm", SlotHelm, 6},
	{"Fremennik hat", SlotHelm, 3},
	{"Fremennik helm", SlotHelm, -6},
	{"Gilded full helm", SlotHelm, -6},
	{"Guthix full helm", SlotHelm, -6},
	{"Hat", SlotHelm, 3},
	{"Iron full helm", SlotHelm, -6},
	{"Iron med helm", SlotHelm, -3},
	{"Mithril full helm", SlotHelm, -6},
	{"Mithril med helm", SlotHelm, -3},
	{"Myre snelm", SlotHelm, -3},
	{"Ochre snelm", SlotHelm, -3},
	{"Robin hood hat", SlotHelm, -10},
	{"Rune full helm", SlotHelm, -6},
	{"Rune full helm (g)", SlotHelm, -6},
	{"Rune full helm (t)", SlotHelm, -6},
	{"Rune med helm", SlotHelm, -3},
	{"Saradomin full", SlotHelm, -6},
	{"Steel full helm", SlotHelm, -6},
	{"Steel med helm", SlotHelm, -3},
	{"Warrior helm", SlotHelm, -5},
	{"Wizards hat", SlotHelm, 2},
	{"Zamorak full helm", SlotHelm, -6},
	{"Adam kiteshield (g)", SlotShield, -8},
	{"Adam kiteshield (t)", SlotShield, -8},
	{"Adamant kiteshield", SlotShield, -8},
	{"Adamant sq shield", SlotShield, -6},
	{"Black kiteshield", SlotShield, -8},
	{"Black kiteshield (g)", SlotShield, -8},
	{"Black kiteshield (t)", SlotShield, -8},
	{"Black sq shield", SlotShield, -6},
	{"Book of balance", SlotShield, 4},
	{"Bronze kiteshield", SlotShield, -8},
	{"Bronze sq shield", SlotShield, -6},
	{"Dragon sq shield", SlotShield, -6},
	{"Fremennik shield", SlotShield, -8},
	{"Gilded kiteshield", SlotShield, -8},
	{"Granite shield", SlotShield, -12},
	{"Guthix kiteshield", SlotShield, -8},
	{"Iron kiteshield", SlotShield, -8},
	{"Iron sq shield", SlotShield, -6},
	{"Mithril kiteshield", SlotShield, -8},
	{"Mithril sq shield", SlotShield, -6},
	{"Rune kiteshield", SlotShield, -8},
	{"Rune kiteshield (g)", SlotShield, -8},
	{"Rune kiteshield (t)", SlotShield, -8},
	{"Rune sq shield", SlotShield, -6},
	{"Saradomin kite", SlotShield, -8},
	{"Steel kiteshield", SlotShield, -8},
	{"Steel sq shield", SlotShield, -6},
	{"Unholy book", SlotShield, 8},
	{"Zamorak kiteshield", SlotShield, -8},
	{"Adam platelegs (g)", SlotLegs, -21},
	{"Adam platelegs (t)", SlotLegs, -21},
	{"Adam plateskirt (g)", SlotLegs, -21},
	{"Adam plateskirt (t)", SlotLegs, -21},
	{"Adamant platelegs", SlotLegs, -21},
	{"Adamant plateskirt", SlotLegs, -21},
	{"Black platelegs", SlotLegs, -21},
	{"Black platelegs (g)", SlotLegs, -21},
	{"Black platelegs (t)", SlotLegs, -21},
	{"Black plateskirt", SlotLegs, -21},
	{"Black plateskirt (g)", SlotLegs, -21},
	{"Black plateskirt (t)", SlotLegs, -21},
	{"Bronze platelegs", SlotLegs, -21},
	{"Bronze plateskirt", SlotLegs, -21},
	{"Dragon platelegs", SlotLegs, -21},
	{"Dragonhide chaps", SlotLegs, -10},
	{"Gilded platelegs", SlotLegs, -21},
	{"Gilded plateskirt", SlotLegs, -21},
	{"Guthix platelegs", SlotLegs, -21},
	{"Guthix plateskirt", SlotLegs, -21},
	{"Iron platelegs", SlotLegs, -21},
	{"Iron plateskirt", SlotLegs, -21},
	{"Mithril platelegs", SlotLegs, -21},
	{"Mithril plateskirt", SlotLegs, -21},
	{"Rune platelegs", SlotLegs, -21},
	{"Rune platelegs (g)", SlotLegs, -21},
	{"Rune platelegs (t)", SlotLegs, -21},
	{"Rune plateskirt", SlotLegs, -21},
	{"Rune plateskirt (g)", SlotLegs, -21},
	{"Rune plateskirt (t)", SlotLegs, -21},
	{"Saradomin legs", SlotLegs, -21},
	{"Saradomin skirt", SlotLegs, -21},
	{"Steel platelegs", SlotLegs, -21},
	{"Steel plateskirt", SlotLegs, -21},
	{"Studded chaps", SlotLegs, -5},
	{"Zamorak platelegs", SlotLegs, -21},
	{"Zamorak plateskirt", SlotLegs, -21},
	{"Adamant 2h sword", SlotWeapon, -4},
	{"Adamant dagger", SlotWeapon, 1},
	{"Adamant dagger(p)", SlotWeapon, 1},
	{"Adamant halberd", SlotWeapon, -4},
	{"Adamnt warhammer", SlotWeapon, -4},
	{"Air battlestaff", SlotWeapon, 10},
	{"Battlestaff", SlotWeapon, 10},
	{"Black 2h sword", SlotWeapon, -4},
	{"Black dagger", SlotWeapon, 1},
	{"Black dagger(p)", SlotWeapon, 1},
	{"Black halberd", SlotWeapon, -4},
	{"Black warhammer", SlotWeapon, -4},
	{"Bronze 2h sword", SlotWeapon, -4},
	{"Bronze dagger", SlotWeapon, 1},
	{"Bronze dagger(p)", SlotWeapon, 1},
	{"Bronze halberd", SlotWeapon, -4},
	{"Bronze warhammer", SlotWeapon, -4},
	{"Dragon dagger", SlotWeapon, 1},
	{"Dragon dagger(p)", SlotWeapon, 1},
	{"Dragon halberd", SlotWeapon, -4},
	{"Dramen staff", SlotWeapon, 10},
	{"Earth battlestaff", SlotWeapon, 10},
	{"Fire battlestaff", SlotWeapon, 10},
	{"Glowing dagger", SlotWeapon, 1},
	{"Iron 2h sword", SlotWeapon, -4},
	{"Iron dagger", SlotWeapon, 1},
	{"Iron dagger(p)", SlotWeapon, 1},
	{"Iron halberd", SlotWeapon, -4},
	{"Iron warhammer", SlotWeapon, -4},
	{"Lava battlestaff", SlotWeapon, 10},
	{"Magic staff", SlotWeapon, 10},
	{"Mithril 2h sword", SlotWeapon, -4},
	{"Mithril dagger", SlotWeapon, 1},
	{"Mithril dagger(p)", SlotWeapon, 1},
	{"Mithril halberd", SlotWeapon, -4},
	{"Mithril warhammer", SlotWeapon, -4},
	{"Mystic air staff", SlotWeapon, 10},
	{"Mystic earth staff", SlotWeapon, 10},
	{"Mystic fire staff", SlotWeapon, 10},
	{"Mystic lava staff", SlotWeapon, 10},
	{"Mystic water staff", SlotWeapon, 10},
	{"Prod", SlotWeapon, 1},
	{"Rune 2h sword", SlotWeapon, -4},
	{"Rune dagger", SlotWeapon, 1},
	{"Rune dagger(p)", SlotWeapon, 1},
	{"Rune halberd", SlotWeapon, -4},
	{"Rune warhammer", SlotWeapon, -4},
	{"Staff", SlotWeapon, 4},
	{"Staff of air", SlotWeapon, 10},
	{"Staff of earth", SlotWeapon, 10},
	{"Staff of fire", SlotWeapon, 10},
	{"Staff of guthix", SlotWeapon, 6},
	{"Staff of saradomin", SlotWeapon, 6},
	{"Staff of water", SlotWeapon, 10},
	{"Staff of zamorak", SlotWeapon, 6},
	{"Steel 2h sword", SlotWeapon, -4},
	{"Steel dagger", SlotWeapon, 1},
	{"Steel dagger(p)", SlotWeapon, 1},
	{"Steel halberd", SlotWeapon, -4},
	{"Steel warhammer", SlotWeapon, -4},
	{"Water battlestaff", SlotWeapon, 10},
	{"Wolfbane", SlotWeapon, 1},
	{"Adam platebody (g)", SlotBody, -30},
	{"Adam platebody (t)", SlotBody, -30},
	{"Adamant chainbody", SlotBody, -15},
	{"Adamant platebody", SlotBody, -30},
	{"Black chainbody", SlotBody, -15},
	{"Black platebody", SlotBody, -30},
	{"Black platebody (g)", SlotBody, -30},
	{"Black platebody (t)", SlotBody, -30},
	{"Black robe", SlotBody, 3},
	{"Bronze chainbody", SlotBody, -15},
	{"Bronze platebody", SlotBody, -30},
	{"Dragon chainbody", SlotBody, -15},
	{"Dragonhide body", SlotBody, -15},
	{"Gilded platebody", SlotBody, -30},
	{"Guthix platebody", SlotBody, -30},
	{"Hardleather body", SlotBody, -4},
	{"Iron chainbody", SlotBody, -15},
	{"Iron platebody", SlotBody, -30},
	{"Leather body", SlotBody, -2},
	{"Mithril chainbody", SlotBody, -15},
	{"Mithril platebody", SlotBody, -30},
	{"Rune chainbody", SlotBody, -15},
	{"Rune platebody", SlotBody, -30},
	{"Rune platebody (g)", SlotBody, -30},
	{"Rune platebody (t)", SlotBody, -30},
	{"Saradomin plate", SlotBody, -30},
	{"Steel chainbody", SlotBody, -15},
	{"Steel platebody", SlotBody, -30},
	{"Studded body", SlotBody, -4},
	{"Wizards robe", SlotBody, 3},
	{"Zamorak platebody", SlotBody, -30},
}

var magicByName = func() map[string]magicRow {
	m := make(map[string]magicRow, len(magicRows))
	for _, r := range magicRows {
		m[strings.ToLower(r.Name)] = r
	}
	return m
}()

// MagicBonus is an item's magic attack bonus by display name (case-insensitive); ok is false for
// an item with none, which is a bonus of 0.
func MagicBonus(name string) (bonus int, ok bool) {
	r, ok := magicByName[strings.ToLower(strings.TrimSpace(name))]
	return r.Bonus, ok
}

// MagicSlot is the slot a table item occupies, "" for an item that is not in the table.
func MagicSlot(name string) Slot {
	return magicByName[strings.ToLower(strings.TrimSpace(name))].Slot
}

// isArmourSlot is the slots the swap takes armour off: the weapon slot is the staff's, and the
// amulet and cape have no negative rows.
func isArmourSlot(s Slot) bool {
	switch s {
	case SlotHelm, SlotBody, SlotLegs, SlotShield, SlotGloves, SlotBoots:
		return true
	}
	return false
}

// MagicSwapRemovals is the worn armour a Magic run takes off: every piece whose magic attack bonus
// is negative, in the order they are worn. The weapon is left alone (the staff pass owns it).
func MagicSwapRemovals(worn []string) []string {
	var out []string
	for _, name := range worn {
		if b, ok := MagicBonus(name); ok && b < 0 && isArmourSlot(MagicSlot(name)) {
			out = append(out, name)
		}
	}
	return out
}

// IsMeleeBlade reports whether name is a melee weapon of the gear tables (a scimitar, a longsword, a
// mace: the SlotWeapon rows; never a staff, a bow or an arrow).
func IsMeleeBlade(name string) bool {
	it, ok := ItemByName(strings.TrimSpace(name))
	return ok && it.Slot == SlotWeapon
}

// MagicSwitchKit is the decision at a switch from melee to Magic training ("scarabpens", 7 Oct 2026;
// owner: "when switching from melee training to magic training, ScarabSwarm doesn't bank the armor it
// wore when training melee"). From what is worn and what the pack holds:
//
//   - takeOff is the worn armour to unequip: every piece with a negative magic attack bonus
//     (MagicSwapRemovals);
//   - bank is everything in the pack the Magic run has no use for, which is the pieces just taken off,
//     any armour with a negative bonus already carried, and any melee blade (the weapon the staff
//     displaces lands in the pack). Each name once, in the order met.
//
// What a caster keeps is not here: a Wizard's hat or robe, an Amulet of magic, a cape, a staff, runes,
// food. The caller passes the pack WITHOUT bank notes.
func MagicSwitchKit(worn, pack []string) (takeOff, bank []string) {
	takeOff = MagicSwapRemovals(worn)
	seen := map[string]bool{}
	add := func(name string) {
		if k := strings.ToLower(strings.TrimSpace(name)); k != "" && !seen[k] {
			seen[k] = true
			bank = append(bank, name)
		}
	}
	for _, n := range takeOff {
		add(n)
	}
	for _, n := range pack {
		if b, ok := MagicBonus(n); ok && b < 0 && isArmourSlot(MagicSlot(n)) {
			add(n)
		} else if IsMeleeBlade(n) {
			add(n)
		}
	}
	return takeOff, bank
}

// MagicWearAdds is what a Magic run puts on from what it holds: for every slot (armour slots, the
// amulet and the cape), the held piece with the best POSITIVE bonus when it beats what is worn
// there, the worn piece counting by its table bonus (0 for an item the table lacks, nothing for an
// empty slot). Wizard robes and hat, an Amulet of magic, Wizard's boots, a cape of a god. Never
// the weapon.
func MagicWearAdds(worn, held []string) []string {
	wornBySlot := map[Slot]int{}
	for _, name := range worn {
		if s := MagicSlot(name); s != "" && s != SlotWeapon {
			b, _ := MagicBonus(name)
			wornBySlot[s] = b
		}
	}
	best := map[Slot]magicRow{}
	for _, name := range held {
		r, ok := magicByName[strings.ToLower(strings.TrimSpace(name))]
		if !ok || r.Bonus <= 0 || r.Slot == SlotWeapon || r.Slot == SlotOther {
			continue
		}
		if cur, seen := best[r.Slot]; seen && cur.Bonus >= r.Bonus {
			continue
		}
		best[r.Slot] = r
	}
	var out []string
	for _, slot := range []Slot{SlotHelm, SlotBody, SlotLegs, SlotShield, SlotGloves, SlotBoots, SlotAmulet, SlotCape} {
		r, ok := best[slot]
		if !ok {
			continue
		}
		if cur, wearing := wornBySlot[slot]; wearing && cur >= r.Bonus {
			continue
		}
		out = append(out, r.Name)
	}
	return out
}

// MeleeRegear is the armour a melee run should draw from the bank and wear: what EquipPlan would
// put on if the bank's contents were in the pack, restricted to the armour slots and to items the
// pack does not already hold (those the trainer's own per-pass EquipPlan wears). It is stateless on
// purpose: after a Magic slice's swap, a death or a restart, "what would I wear if I had my bank"
// is the same question.
func MeleeRegear(l Levels, worn, pack, bank []string) []Item {
	inPack := map[string]bool{}
	for _, n := range pack {
		inPack[strings.ToLower(strings.TrimSpace(n))] = true
	}
	held := append(append([]string{}, pack...), bank...)
	var out []Item
	for _, it := range EquipPlan(l, nil, worn, held) {
		if it.Slot == SlotWeapon || inPack[strings.ToLower(it.Name)] {
			continue
		}
		out = append(out, it)
	}
	return out
}
