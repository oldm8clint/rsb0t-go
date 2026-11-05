package loot

import "strings"

// The Magic card (a user request): spells cast on items, netted of everything a cast uses.
//
//   - Superheat Item rows: one event a cast, one bar made. Inputs: the bar's ores (the primary ore
//     and its coal or tin: smelting.struct), one Nature rune, and four Fire runes unless a fire
//     staff was wielded (magic_spells.dbrow [magic_spell_superheat] runesrequired
//     naturerune,1,firerune,4; magic.rs2 ~staff_runes).
//   - High and Low Level Alchemy rows: one event a cast. Item: the coins it paid. Inputs: the item
//     alched, one Nature rune, and five (high) or three (low) Fire runes unless a fire staff was
//     wielded (alchplan.HighAlch / LowAlch).
//
// WHICH CASE APPLIED is recorded per cast in the ACTION: "superheat" and "alch" are casts with a
// fire staff wielded, "superheat-runes" and "alch-runes" casts that paid Fire runes, so the two are
// separate rows (PartKey carries the action) with their own cost.

// SkillMagic titles the Magic card.
const SkillMagic = "Magic"

// The Magic card's actions (see above for the "-runes" forms).
const (
	ActionSuperheat = "superheat"
	ActionAlch      = "alch"
	ActionLowAlch   = "lowalch"
	runesSuffix     = "-runes"
)

// Rune names and ids (content pack/obj.pack).
const (
	natureRuneName = "Nature rune"
	fireRuneName   = "Fire rune"
	fireRuneID     = 554
)

func magicAction(action string, staff bool) string {
	if staff {
		return action
	}
	return action + runesSuffix
}

// SuperheatCast is a superheat source for one bar (its obj id) cast on oreName, with or without a
// fire staff wielded.
func SuperheatCast(barID int, oreName string, staff bool) Source {
	// On the Smithing card: the owner looks for bars there (2 Nov 2026: "superheating gold bars doesnt appear anywhere on
	// the loot tracker should probably appear in smithing panel"). Its inputs are the same either way (cost.go).
	return Production(SkillSmithing, magicAction(ActionSuperheat, staff), barID, oreName)
}

// Alchemy is an alchemy source for the item alched (its obj id and name), high or low, with or
// without a fire staff wielded.
func Alchemy(itemID int, itemName string, high, staff bool) Source {
	a := ActionLowAlch
	if high {
		a = ActionAlch
	}
	return Production(SkillMagic, magicAction(a, staff), itemID, itemName)
}

// magicInputs is what one Magic-card event uses beyond its first input (Consumes: the ore or the
// item): the bar's second ingredient for a superheat, the Nature rune, and the Fire runes when the
// action says no staff paid them.
func magicInputs(s Source) []Cost {
	base := strings.TrimSuffix(s.Action, runesSuffix)
	runes := strings.HasSuffix(s.Action, runesSuffix)
	var out []Cost
	fire := int64(0)
	switch base {
	case ActionSuperheat:
		out = append(out, barSecond[s.ID]...)
		fire = 4
	case ActionAlch:
		fire = 5
	case ActionLowAlch:
		fire = 3
	default:
		return nil
	}
	out = append(out, Cost{ID: natureRuneID, Name: natureRuneName, Per: 1})
	if runes {
		out = append(out, Cost{ID: fireRuneID, Name: fireRuneName, Per: fire})
	}
	return out
}

// barSecond is each bar's second ingredient a bar (skill_smithing/configs/smelting/smelting.struct
// ingredient_secondary / ingredient_secondary_count), keyed on the bar's obj id.
var barSecond = map[int][]Cost{
	2349: {{ID: 438, Name: "Tin ore", Per: 1}}, // bronze
	2353: {{ID: 453, Name: "Coal", Per: 2}},    // steel
	2359: {{ID: 453, Name: "Coal", Per: 4}},    // mithril
	2361: {{ID: 453, Name: "Coal", Per: 6}},    // adamantite
	2363: {{ID: 453, Name: "Coal", Per: 8}},    // runite
}

// ActionCombat is the Magic card's offensive-spell row: the runes cast at npcs, whatever the spell.
const ActionCombat = "combat"

// combatSpellsName and combatSpellsIcon are the row's name and the obj it is drawn with (a Death rune).
const (
	combatSpellsName = "Combat spells"
	combatSpellsIcon = 560
)

// CombatSpells is the Magic card's row for every offensive spell cast (a user request: "the Magic
// panel counts the cost of ALL offensive spells"). Its Events are the kills made with spells and its
// Spent the measured runes: the same bill the npc rows are netted of, which Tally.Add copies here
// whenever a kill carries Spent. It is a MIRROR (MirrorsCost), so nothing subtracts it twice.
func CombatSpells() Source {
	return Production(SkillMagic, ActionCombat, combatSpellsIcon, combatSpellsName)
}

// MirrorsCost reports whether a source's Spent only repeats a bill another source already carries, so
// a total over every source must not subtract it again: the Combat spells row.
func (s Source) MirrorsCost() bool {
	return s.Kind == KindProduction && s.Skill == SkillMagic && (s.Action == ActionCombat || s.Action == ActionSuperheatCast)
}

// ActionSuperheatCast is the Magic card's Superheat Item row: the casts, their measured bill and the
// Magic experience they paid.
//
// a user request: "superheating should show in the magic panel". The bars stay on the Smithing card
// (SuperheatCast), where the owner looks for them; the cast itself is ALSO a Magic card row, with its
// cost and XP. It is a MIRROR like the Combat spells row (MirrorsCost): the bars it made are already
// valued on Smithing, so the row carries no Items, and its events, runes and ore are not added to a
// total a second time.
const ActionSuperheatCast = "superheatcast"

// superheatSpellName and superheatSpellIcon are the row's name and the obj it is drawn with (a Nature rune).
const (
	superheatSpellName = "Superheat Item"
	superheatSpellIcon = natureRuneID
)

// SuperheatSpell is the Magic card's row for every Superheat Item cast.
func SuperheatSpell() Source {
	return Production(SkillMagic, ActionSuperheatCast, superheatSpellIcon, superheatSpellName)
}

// barOre is the primary ore of each bar a superheat makes, keyed on the bar's obj id (smelting.struct).
var barOre = map[int]int{
	2349: 436, // bronze: copper ore (and tin, barSecond)
	2351: 440, // iron: iron ore
	2353: 440, // steel: iron ore (and coal)
	2355: 442, // silver: silver ore
	2357: 444, // gold: gold ore
	2359: 447, // mithril: mithril ore
	2361: 449, // adamantite: adamantite ore
	2363: 451, // runite: runite ore
}

// SuperheatCastDrop is the Magic card's record of n casts that made barID from oreName: the events,
// and the ore, second ingredient, Nature runes and (unless a fire staff was wielded) Fire runes they
// used as measured Spent. Experience is attached by the caller's experience watch (ClaimsExperience).
func SuperheatCastDrop(barID int, oreName string, n int, staff bool) Drop {
	if n <= 0 {
		return Drop{}
	}
	oreID, ok := barOre[barID]
	if !ok {
		oreID = barID
	}
	nn := int64(n)
	spent := []Item{{ID: oreID, Name: oreName, Count: nn}}
	for _, c := range barSecond[barID] {
		spent = append(spent, Item{ID: c.ID, Name: c.Name, Count: c.Per * nn})
	}
	spent = append(spent, Item{ID: natureRuneID, Name: natureRuneName, Count: nn})
	if !staff {
		spent = append(spent, Item{ID: fireRuneID, Name: fireRuneName, Count: 4 * nn})
	}
	return Drop{Source: SuperheatSpell(), Events: n, Spent: spent}
}
