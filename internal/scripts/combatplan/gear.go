// Package combatplan holds the pure decision logic a combat script needs: what gear the
// account may wear, whether a target is dead, what is worth looting, what to re-equip after
// a death, and when a run is finished.
//
// Pure means what it means everywhere else in internal/scripts: no client, actions, fleet or
// bot import, no I/O, no clock beyond values a caller passes in — so every rule here is
// unit-tested rather than only observed live. internal/scripts/combatrunner is the I/O half.
//
// This package is milestone 1 of docs/combat_trainer_plan.md (the primitives), NOT the
// training ladder — no rung schedule, no gear-buying loop and no bot registration lives here
// yet, deliberately.
package combatplan

import "strings"

// Gear requirements, and which shops actually sell what.
//
// Why this table exists at all: the item config this client decodes (internal/cachecfg)
// carries no wear slot and no level requirement. In content branch 289 those requirements are
// not data — they are scripts. levelrequire/scripts/tier*.rs2 registers one [opheld2,<item>]
// trigger per item that calls @levelrequire_attack(N), @levelrequire_defence(N),
// @levelrequire_ranged(N) and so on, and the level lives in that call. So no decoder can
// supply it and a hand-built table is the only option; every number below was read out of
// those tier files, not assumed from memory of the live game.
//
// The metal tiers, confirmed across tier1/5/10/20/30/40/60.rs2: bronze and iron 1, steel 5,
// black 10, mithril 20, adamant 30, rune 40, dragon 60 — Attack for weapons,
// Defence for armour.
//
// Ranged is its own ladder (same files): bows are shortbow/longbow 1, oak 5, willow 20,
// maple 30, yew 40, magic 50.
//
// Arrows are the one case that is NOT a wear requirement. No tier file registers an arrow at
// all. Instead skill_combat/scripts/player/player_ranged.rs2's [proc,player_ranged_check_ammo]
// compares the two items' own `levelrequire` obj params at the moment of the shot:
//
//	if (oc_param($ammo, levelrequire) > oc_param($rhand, levelrequire)) {
//	    mes("Your bow isn't powerful enough for those arrows.");
//
// so an arrow is limited by the BOW, not by the player's Ranged level. Those params are
// bronze/iron 1, steel 5, mithril 20, adamant 30, rune 40 for arrows, and the same numbers as
// the bows' own requirements. AmmoTier below carries that param and ArrowFitsBow applies the
// comparison, so a caller never has to guess the pairing.

// Skill names the stat a requirement is measured in.
type Skill string

const (
	SkillAttack   Skill = "attack"
	SkillStrength Skill = "strength"
	SkillDefence  Skill = "defence"
	SkillRanged   Skill = "ranged"
	SkillMagic    Skill = "magic"
)

// Slot is the equipment slot an item occupies, as a training script thinks of them. This is
// the script's own classification, not a value the cache supplies.
type Slot string

const (
	SlotWeapon Slot = "weapon"
	SlotHelm   Slot = "helm"
	SlotBody   Slot = "body"
	SlotLegs   Slot = "legs"
	SlotShield Slot = "shield"
	SlotAmmo   Slot = "ammo"
	SlotBow    Slot = "bow"
	// SlotStaff is the Magic discipline's weapon. It is its own slot rather than SlotWeapon so
	// that adding a staff to the table cannot disturb any melee plan: BuyPlan and EquipPlan walk
	// MeleeSlots, which names SlotWeapon and never this.
	SlotStaff Slot = "staff"
)

// Levels is the account's base levels, the values the server's own stat_base checks compare
// against (not boosted or drained current levels).
type Levels struct {
	Attack   int
	Strength int
	Defence  int
	Ranged   int
	Magic    int
}

// Of returns the level for one skill.
func (l Levels) Of(s Skill) int {
	switch s {
	case SkillAttack:
		return l.Attack
	case SkillStrength:
		return l.Strength
	case SkillDefence:
		return l.Defence
	case SkillRanged:
		return l.Ranged
	case SkillMagic:
		return l.Magic
	}
	return 0
}

// Item is one wearable piece: what it needs, and where it can be bought.
type Item struct {
	// Name is the item's display name exactly as its obj config spells it, which is what
	// cachecfg.Obj.Name returns — so a runner can resolve it without a second mapping.
	Name string
	// Tier is the metal or bow material, for grouping and for upgrade decisions.
	Tier string
	Slot Slot
	// Skill and Level are the wear requirement the content script enforces.
	Skill Skill
	Level int
	// Quest names the quest that gates wearing this item, empty when none. Confirmed from
	// the tier files' own quest-specific labels (dragon_slayer_quest_defence and friends).
	Quest string
	// Shop is where a shop actually stocks it, empty when nothing does. ShopNote carries
	// the gate on reaching that shop.
	Shop     string
	ShopNote string
	// AmmoTier is the `levelrequire` obj param player_ranged_check_ammo compares for bows
	// and arrows; 0 for everything else.
	AmmoTier int
}

// Quest gates that appear in the tier files.
const (
	QuestDragonSlayer = "Dragon Slayer"
	QuestLegends      = "Legends' Quest"
	QuestLostCity     = "Lost City"
	QuestHeroes       = "Heroes' Quest"
)

// Shops, named as a script would report them.
const (
	ShopVarrockSword     = "Varrock sword shop"
	ShopVarrockArmour    = "Varrock armour shop (Horvik)"
	ShopVarrockArchery   = "Varrock archery shop (Lowe)"
	ShopBarbarianHelmet  = "Barbarian Village helmet shop"
	ShopAlKharidScimitar = "Al Kharid scimitar shop"
	ShopAlKharidLegs     = "Al Kharid platelegs shop"
	ShopAlKharidSkirt    = "Al Kharid plateskirt shop"
	ShopFaladorShield    = "Falador shield shop"
	ShopFaladorChain     = "Falador chainmail shop"
	ShopFaladorMace      = "Falador mace shop"
	ShopChampionsGuild   = "Champions' Guild champion shop"
	ShopRuniteGuild      = "Champions' Guild runite shop"
	ShopEdgevilleOziach  = "Edgeville (Oziach)"
	ShopRangingGuild     = "Ranging Guild bow shop"
	ShopVarrockRune      = "Varrock rune shop (Aubury)"
	ShopVarrockStaff     = "Varrock staff shop (Zaff)"
)

// noteQuestPoints is the Champions' Guild door gate: champions_guild.rs2 refuses entry with
// "The door won't open - you need at least 32 Quest Points." So both guild shops, which are
// the ONLY source of any rune melee gear in this content build, sit behind 32 quest points.
const noteQuestPoints = "32 Quest Points to enter the guild"

// noteRangingGuild is the Ranging Guild's own gate, and it keeps that shop out of every plan
// this trainer makes.
//
// Two separate reasons, either of which is enough. Its door refuses entry below Ranged 40
// (minigames/game_ranging/scripts/ranging_guild_door.rs2: "You must be a level 40 ranger to
// enter"), so a run below that level cannot get in at all. And no route to it has ever been
// walked with this client, which is the standard the ladder's TravelProven flag already applies
// to a rung: the members rungs are refused outright rather than walked at on the strength of a
// map reading, and a shop deserves the same treatment.
//
// This matters because the Ranging Guild is the ONLY source of willow bows and of mithril,
// adamant and rune arrows. Without this gate a Ranged plan reads Lowe's oak ceiling as
// temporary and sends a Ranged 40 account across the map to a door it has never opened, three
// times over, before the slot retires. With it, the ceiling is stated once and the run trains on.
const noteRangingGuild = "Ranged 40 to enter the guild; no verified bank or route yet"

// metalTier is one armour/weapon material and the level its pieces need.
type metalTier struct {
	Name  string
	Level int
}

// The metal ladder, from levelrequire/scripts/tier*.rs2.
var metalTiers = []metalTier{
	{"Bronze", 1}, {"Iron", 1}, {"Steel", 5}, {"Black", 10},
	{"Mithril", 20}, {"Adamant", 30}, {"Rune", 40}, {"Dragon", 60},
}

// piece is one kind of item a metal tier comes in. Suffix is the display-name tail, so
// "Rune" + " " + "scimitar" is "Rune scimitar" — the spelling confirmed against every obj
// config's own name= line.
type piece struct {
	Suffix string
	Slot   Slot
	Skill  Skill
}

var metalPieces = []piece{
	{"scimitar", SlotWeapon, SkillAttack},
	{"longsword", SlotWeapon, SkillAttack},
	{"dagger", SlotWeapon, SkillAttack},
	{"mace", SlotWeapon, SkillAttack},
	{"battleaxe", SlotWeapon, SkillAttack},
	{"sword", SlotWeapon, SkillAttack},
	{"med helm", SlotHelm, SkillDefence},
	{"full helm", SlotHelm, SkillDefence},
	{"chainbody", SlotBody, SkillDefence},
	{"platebody", SlotBody, SkillDefence},
	{"platelegs", SlotLegs, SkillDefence},
	{"plateskirt", SlotLegs, SkillDefence},
	{"sq shield", SlotShield, SkillDefence},
	{"kiteshield", SlotShield, SkillDefence},
}

// dragonPieces are the only Dragon items that exist at all in this content build — there is
// no dragon scimitar, platebody, full helm, kiteshield or plateskirt to buy or be given.
// Checked against every obj config's name= line, not assumed from the live game.
var dragonPieces = map[string]bool{
	"dagger": true, "longsword": true, "mace": true, "battleaxe": true,
	"med helm": true, "chainbody": true, "platelegs": true, "sq shield": true,
}

// questGates names the items whose tier file entry is a quest-specific label rather than a
// plain level check.
var questGates = map[string]string{
	"Rune platebody":   QuestDragonSlayer,
	"Dragon sq shield": QuestLegends,
	"Dragon dagger":    QuestLostCity,
	"Dragon longsword": QuestLostCity,
	"Dragon mace":      QuestHeroes,
	"Dragon battleaxe": QuestHeroes,
}

// shopOf maps an item's display name to the shop that stocks it. Every entry was read from
// the shop's own .inv stock lines in content/scripts/areas, so an item missing here is one
// no shop sells — which for most rune gear is the whole point.
var shopOf = map[string]Item{
	// Varrock sword shop (swordshop): longswords and daggers up to adamant.
	"Bronze longsword": {Shop: ShopVarrockSword}, "Iron longsword": {Shop: ShopVarrockSword},
	"Steel longsword": {Shop: ShopVarrockSword}, "Black longsword": {Shop: ShopVarrockSword},
	"Mithril longsword": {Shop: ShopVarrockSword}, "Adamant longsword": {Shop: ShopVarrockSword},
	"Bronze dagger": {Shop: ShopVarrockSword}, "Iron dagger": {Shop: ShopVarrockSword},
	"Steel dagger": {Shop: ShopVarrockSword}, "Black dagger": {Shop: ShopVarrockSword},
	"Mithril dagger": {Shop: ShopVarrockSword}, "Adamant dagger": {Shop: ShopVarrockSword},
	"Bronze sword": {Shop: ShopVarrockSword}, "Iron sword": {Shop: ShopVarrockSword},
	"Steel sword": {Shop: ShopVarrockSword}, "Black sword": {Shop: ShopVarrockSword},
	"Mithril sword": {Shop: ShopVarrockSword}, "Adamant sword": {Shop: ShopVarrockSword},

	// Varrock armour shop (armourshop, Horvik).
	"Bronze chainbody": {Shop: ShopVarrockArmour}, "Iron chainbody": {Shop: ShopVarrockArmour},
	"Steel chainbody": {Shop: ShopVarrockArmour}, "Mithril chainbody": {Shop: ShopVarrockArmour},
	"Bronze platebody": {Shop: ShopVarrockArmour}, "Iron platebody": {Shop: ShopVarrockArmour},
	"Steel platebody": {Shop: ShopVarrockArmour}, "Mithril platebody": {Shop: ShopVarrockArmour},
	"Black platebody": {Shop: ShopVarrockArmour},

	// Barbarian Village helmet shop.
	"Bronze med helm": {Shop: ShopBarbarianHelmet}, "Iron med helm": {Shop: ShopBarbarianHelmet},
	"Steel med helm": {Shop: ShopBarbarianHelmet}, "Mithril med helm": {Shop: ShopBarbarianHelmet},
	"Adamant med helm": {Shop: ShopBarbarianHelmet},
	"Bronze full helm": {Shop: ShopBarbarianHelmet}, "Iron full helm": {Shop: ShopBarbarianHelmet},
	"Steel full helm": {Shop: ShopBarbarianHelmet}, "Mithril full helm": {Shop: ShopBarbarianHelmet},
	"Adamant full helm": {Shop: ShopBarbarianHelmet},

	// Al Kharid: scimitars stop at mithril, legs and skirts reach adamant.
	"Bronze scimitar": {Shop: ShopAlKharidScimitar}, "Iron scimitar": {Shop: ShopAlKharidScimitar},
	"Steel scimitar": {Shop: ShopAlKharidScimitar}, "Mithril scimitar": {Shop: ShopAlKharidScimitar},
	"Bronze platelegs": {Shop: ShopAlKharidLegs}, "Iron platelegs": {Shop: ShopAlKharidLegs},
	"Steel platelegs": {Shop: ShopAlKharidLegs}, "Black platelegs": {Shop: ShopAlKharidLegs},
	"Mithril platelegs": {Shop: ShopAlKharidLegs}, "Adamant platelegs": {Shop: ShopAlKharidLegs},
	"Bronze plateskirt": {Shop: ShopAlKharidSkirt}, "Iron plateskirt": {Shop: ShopAlKharidSkirt},
	"Steel plateskirt": {Shop: ShopAlKharidSkirt}, "Black plateskirt": {Shop: ShopAlKharidSkirt},
	"Mithril plateskirt": {Shop: ShopAlKharidSkirt}, "Adamant plateskirt": {Shop: ShopAlKharidSkirt},

	// Falador. The shield shop's best kiteshield is STEEL — it stocks mithril only as a
	// square shield, which the plan this table was built for had wrong.
	"Bronze sq shield": {Shop: ShopFaladorShield}, "Iron sq shield": {Shop: ShopFaladorShield},
	"Steel sq shield": {Shop: ShopFaladorShield}, "Mithril sq shield": {Shop: ShopFaladorShield},
	"Bronze kiteshield": {Shop: ShopFaladorShield}, "Iron kiteshield": {Shop: ShopFaladorShield},
	"Steel kiteshield": {Shop: ShopFaladorShield},
	"Black chainbody":  {Shop: ShopFaladorChain}, "Adamant chainbody": {Shop: ShopFaladorChain},
	"Bronze mace": {Shop: ShopFaladorMace}, "Iron mace": {Shop: ShopFaladorMace},
	"Steel mace": {Shop: ShopFaladorMace}, "Mithril mace": {Shop: ShopFaladorMace},
	"Adamant mace": {Shop: ShopFaladorMace},

	// Champions' Guild, behind 32 quest points — the only rune melee gear sold anywhere.
	"Black full helm":   {Shop: ShopChampionsGuild, ShopNote: noteQuestPoints},
	"Adamant platebody": {Shop: ShopChampionsGuild, ShopNote: noteQuestPoints},
	"Rune longsword":    {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},
	"Rune sword":        {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},
	"Rune mace":         {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},
	"Rune chainbody":    {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},
	"Rune platelegs":    {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},
	"Rune plateskirt":   {Shop: ShopRuniteGuild, ShopNote: noteQuestPoints},

	// Oziach sells the one rune platebody, which also needs Dragon Slayer to wear.
	"Rune platebody": {Shop: ShopEdgevilleOziach, ShopNote: "Dragon Slayer to wear it"},
}

// bowSpec is one bow: its Ranged requirement, which equals the `levelrequire` param its
// ammo check compares against.
type bowSpec struct {
	Name  string
	Tier  string
	Level int
	Shop  string
	Note  string
}

var bowSpecs = []bowSpec{
	{"Shortbow", "Normal", 1, ShopVarrockArchery, ""},
	{"Longbow", "Normal", 1, ShopVarrockArchery, ""},
	{"Oak shortbow", "Oak", 5, ShopVarrockArchery, ""},
	{"Oak longbow", "Oak", 5, ShopVarrockArchery, ""},
	{"Willow shortbow", "Willow", 20, ShopRangingGuild, "Ranging Guild"},
	{"Willow longbow", "Willow", 20, ShopRangingGuild, "Ranging Guild"},
	{"Maple shortbow", "Maple", 30, "", ""},
	{"Maple longbow", "Maple", 30, "", ""},
	{"Yew shortbow", "Yew", 40, "", ""},
	{"Yew longbow", "Yew", 40, "", ""},
	{"Magic shortbow", "Magic", 50, "", ""},
	{"Magic longbow", "Magic", 50, "", ""},
}

// staffSpec is one elemental staff: which rune it supplies for free, and where to buy it.
//
// The substitution is the reason a staff is worth a trip at all. magic_staff.dbrow maps each
// staff to one rune and [proc,staff_runes] in skill_magic/scripts/magic.rs2 applies it, so a
// Fire Blast autocaster holding a staff of fire pays a death rune and four air runes instead of
// a death rune, five fire runes and four air runes. Over the thousands of casts between Magic 59
// and 75 that is the difference between a rune trip per bank run and several.
//
// Level is 1 for all four: no tier file in content/scripts/levelrequire registers a staff at
// all, so nothing above Magic 1 is required to hold one. Zaff stocks two of each at 1000 coins
// (varrock.inv [staffshop]).
type staffSpec struct {
	Name string
	// Rune is the display name of the rune this staff supplies for free.
	Rune string
	Shop string
}

var staffSpecs = []staffSpec{
	{"Staff of air", "Air rune", ShopVarrockStaff},
	{"Staff of water", "Water rune", ShopVarrockStaff},
	{"Staff of earth", "Earth rune", ShopVarrockStaff},
	{"Staff of fire", "Fire rune", ShopVarrockStaff},
}

// StaffRune returns the rune an elemental staff supplies for free, empty for anything else.
func StaffRune(staffName string) string {
	for _, s := range staffSpecs {
		if strings.EqualFold(s.Name, staffName) {
			return s.Rune
		}
	}
	return ""
}

// arrowSpec is one arrow tier and its `levelrequire` obj param.
type arrowSpec struct {
	Name string
	Tier string
	// Param is the value player_ranged_check_ammo compares against the bow's own.
	Param int
	Shop  string
	Note  string
}

var arrowSpecs = []arrowSpec{
	{"Bronze arrow", "Bronze", 1, ShopVarrockArchery, ""},
	{"Iron arrow", "Iron", 1, ShopVarrockArchery, ""},
	{"Steel arrow", "Steel", 5, ShopVarrockArchery, ""},
	{"Mithril arrow", "Mithril", 20, ShopRangingGuild, "Ranging Guild"},
	{"Adamant arrow", "Adamant", 30, ShopRangingGuild, "Ranging Guild"},
	{"Rune arrow", "Rune", 40, ShopRangingGuild, "Ranging Guild"},
}

// allItems is built once from the tables above.
var allItems = buildItems()

func buildItems() []Item {
	var out []Item
	for _, tier := range metalTiers {
		for _, p := range metalPieces {
			if tier.Name == "Dragon" && !dragonPieces[p.Suffix] {
				continue
			}
			name := tier.Name + " " + p.Suffix
			it := Item{
				Name:  name,
				Tier:  tier.Name,
				Slot:  p.Slot,
				Skill: p.Skill,
				Level: tier.Level,
				Quest: questGates[name],
			}
			if s, ok := shopOf[name]; ok {
				it.Shop, it.ShopNote = s.Shop, s.ShopNote
			}
			out = append(out, it)
		}
	}
	for _, b := range bowSpecs {
		out = append(out, Item{
			Name: b.Name, Tier: b.Tier, Slot: SlotBow, Skill: SkillRanged,
			Level: b.Level, Shop: b.Shop, ShopNote: b.Note, AmmoTier: b.Level,
		})
	}
	for _, st := range staffSpecs {
		out = append(out, Item{
			Name: st.Name, Tier: "Staff", Slot: SlotStaff, Skill: SkillMagic,
			Level: 1, Shop: st.Shop,
		})
	}
	for _, a := range arrowSpecs {
		// Arrows have no wear requirement of their own — see this file's doc comment.
		out = append(out, Item{
			Name: a.Name, Tier: a.Tier, Slot: SlotAmmo, Skill: SkillRanged,
			Level: 0, Shop: a.Shop, ShopNote: a.Note, AmmoTier: a.Param,
		})
	}
	return out
}

// Items returns every item in the table.
func Items() []Item {
	return append([]Item(nil), allItems...)
}

// ItemByName finds an item by display name, case-insensitively.
func ItemByName(name string) (Item, bool) {
	for _, it := range allItems {
		if strings.EqualFold(it.Name, name) {
			return it, true
		}
	}
	return Item{}, false
}

// Wearable reports whether the account meets an item's own wear requirement. questsDone may
// be nil, in which case a quest-gated item is refused — the safe direction, since wearing it
// would simply fail in game.
func Wearable(it Item, l Levels, questsDone map[string]bool) bool {
	if l.Of(it.Skill) < it.Level {
		return false
	}
	if it.Quest != "" && !questsDone[it.Quest] {
		return false
	}
	return true
}

// BestWearable returns the highest-requirement item in a slot the account can actually wear.
// Ties (bronze and iron both need 1) resolve to the first in table order, which is the
// cheaper metal. ok is false when nothing in the slot qualifies.
func BestWearable(slot Slot, l Levels, questsDone map[string]bool) (Item, bool) {
	best := Item{}
	found := false
	for _, it := range allItems {
		if it.Slot != slot || !Wearable(it, l, questsDone) {
			continue
		}
		if !found || it.Level > best.Level {
			best, found = it, true
		}
	}
	return best, found
}

// BestBuyable is BestWearable restricted to items a shop actually stocks — the realistic
// upgrade for a script with coins but no mule. ok is false when nothing in the slot is both
// wearable and sold.
func BestBuyable(slot Slot, l Levels, questsDone map[string]bool) (Item, bool) {
	best := Item{}
	found := false
	for _, it := range allItems {
		if it.Slot != slot || it.Shop == "" || !Wearable(it, l, questsDone) {
			continue
		}
		if !found || it.Level > best.Level {
			best, found = it, true
		}
	}
	return best, found
}

// ArrowFitsBow applies player_ranged_check_ammo's own comparison: an arrow whose levelrequire
// param exceeds the bow's is refused with "Your bow isn't powerful enough for those arrows."
// The player's own Ranged level does not enter into it.
func ArrowFitsBow(arrow, bow Item) bool {
	if arrow.Slot != SlotAmmo || bow.Slot != SlotBow {
		return false
	}
	return arrow.AmmoTier <= bow.AmmoTier
}

// BestArrowForBow returns the strongest arrow the bow can actually fire, preferring one a
// shop sells when buyable is true.
func BestArrowForBow(bow Item, buyable bool) (Item, bool) {
	best := Item{}
	found := false
	for _, it := range allItems {
		if it.Slot != SlotAmmo || !ArrowFitsBow(it, bow) {
			continue
		}
		if buyable && it.Shop == "" {
			continue
		}
		if !found || it.AmmoTier > best.AmmoTier {
			best, found = it, true
		}
	}
	return best, found
}

// MetalTierLevel returns the level a metal tier's pieces require, and whether the tier is
// one this content build has.
func MetalTierLevel(tier string) (int, bool) {
	for _, t := range metalTiers {
		if strings.EqualFold(t.Name, tier) {
			return t.Level, true
		}
	}
	return 0, false
}
