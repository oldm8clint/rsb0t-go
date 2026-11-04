package loot

import (
	"strings"

	"rsb0t-go/internal/cachecfg"
)

// RUNECRAFTING AND THE ESSENCE SUPPLY ON THE LOOT PANEL (a user request: "runecrafting loot panel is
// needed"). MasterCrafter binds Rune essence into Air, Body and Nature runes at the altars (runecraftrunner.Craft),
// and the EssenceMiner accounts mine essence and hand it to the mule in note stacks (internal/bot's
// essence_supply.go). Neither reported a Drop, so the Loot panel saw the essence MINED (the Mining card) and
// nothing of what became of it.
//
// THE RUNECRAFT CARD, a production card like Fletching and Crafting (production.go):
//
//   - one row per ALTAR (Source.Name "Air altar", Source.ID the rune it makes, so the row is drawn with the rune
//     itself);
//   - one EVENT is one CRAFT, the altar click that binds the whole pack at once, not one essence: a craft's
//     essence is a different number every time (27 a trip, whatever the Nature gate was handed) and the count of
//     essence is what is measured, so it is Drop.Spent, which the panel prices as a cost exactly as it prices the
//     runes cast at an npc (lootcost.go spentCost). That is why Consumes says no for this card: there is no fixed
//     input per event to multiply;
//   - Items are the runes the craft measurably put in the pack, valued at the market like any loot;
//   - XP is the experience the craft measurably paid (the stat reading rising, never the table's figure);
//   - Source.Level is the Runecraft LEVEL the account read after the craft. It is the one field a source already
//     carries as a "latest reading" (an npc's combat level), so it rides the same path (Tally.recordLocked keeps
//     the last non-zero, the control panel's lootRec keeps it, the view takes the highest of the accounts). It
//     does not change the source's identity: Key and GroupKey ignore it.
//
// NOT DOUBLE-COUNTED. The essence is counted ONCE as income, when it is mined (the Mining card's Rune essence
// item); binding it nets it off here as a spend, so the two cards add up to what the essence became. The runes
// are counted once, when the altar makes them. Nothing that moves them afterwards (a trade to the mule, a courier
// haul) is loot.
//
// THE ESSENCE SUPPLY CARD (KindSupply), the other half of the owner's ask: how much essence the miners hand to
// the mule. A hand-over MOVES essence that was already counted when it was mined, so it must not be loot a second
// time: the Drop carries the essence only as Events (the essence crossed), with no Items, and the card shows a
// count and a rate and no gp. The mule's own stock is not here: it is a reading, not an event, and the control
// panel reads it from the mule's cached bank (lootrunecraft.go).

// SkillRunecraft is spelled as gamestate.SkillNames spells it.
const SkillRunecraft = "Runecraft"

// ActionRunecraft is one craft: every essence in the pack bound at an altar.
const ActionRunecraft = "runecraft"

// The essence objs (content pack/obj.pack): the unnoted Rune essence (blankrune) and its note (cert_blankrune).
const (
	ObjRuneEssence      = 1436
	ObjNotedRuneEssence = 1437
	// RuneEssenceName is the name the cache gives both.
	RuneEssenceName = "Rune essence"
)

// IsRuneEssence reports whether an obj id is Rune essence, noted or not.
func IsRuneEssence(id int) bool { return id == ObjRuneEssence || id == ObjNotedRuneEssence }

// Runecrafting is the source of the crafts made at one altar: altar is the rune's short name ("Air"), runeID the rune
// it makes, which is what the row is drawn with.
func Runecrafting(altar string, runeID int) Source {
	altar = strings.TrimSpace(altar)
	if altar == "" {
		altar = "Rune"
	}
	return Production(SkillRunecraft, ActionRunecraft, runeID, altar+" altar")
}

// IsRunecraft reports whether a source is a row of the Runecraft card.
func IsRunecraft(s Source) bool { return s.Kind == KindProduction && s.Skill == SkillRunecraft }

// RuneCrafted reports one VERIFIED craft: essence bound, runes the pack gained, the experience the stat gained and the
// level it read afterwards. A nil sink, a craft with no essence, or one that measurably came to neither a rune nor
// experience, reports nothing. A craft whose essence went and whose experience came but whose runes the pack did not
// show (the inventory update read late) is still reported, without the runes: the essence really was spent, and
// nothing here guesses what the table would have given. Names and the cache's unit cost come from cfg when it is
// there; the caller's name stands otherwise.
func RuneCrafted(sink func(Drop), cfg *cachecfg.Config, altar string, runeID int, runeName string, essence, runes, xp, level int) {
	if sink == nil || essence <= 0 || (runes <= 0 && xp <= 0) {
		return
	}
	src := Runecrafting(altar, runeID)
	if level > 0 {
		src.Level = level
	}
	d := Drop{
		Source: src,
		Events: 1,
		XP:     max(0, xp),
		Spent:  []Item{costed(cfg, Item{ID: ObjRuneEssence, Name: RuneEssenceName, Count: int64(essence)})},
	}
	if runes > 0 {
		d.Items = []Item{costed(cfg, Item{ID: runeID, Name: runeName, Count: int64(runes)})}
	}
	sink(d)
}

// costed names an item through the cache and gives it the cache's unit cost, leaving the caller's name where the cache
// has none.
func costed(cfg *cachecfg.Config, it Item) Item {
	if cfg == nil {
		return it
	}
	if o, ok := cfg.Obj(it.ID); ok {
		if o.Name != "" {
			it.Name = o.Name
		}
		it.Value = int64(o.Cost)
	}
	return it
}

// KindSupply groups the essence supply on one card. Titled by Skill, like the Teleports and Shop purchases cards.
const KindSupply = "supply"

// SkillEssenceSupply titles the Essence supply card; it is not a game skill, only the card's name.
const SkillEssenceSupply = "Essence supply"

// ActionHandover is one essence handed to the mule, for the panel's verb: an event is an ESSENCE (so the card's count
// and its hourly rate are essence, the figure users asked for), not a trade.
const ActionHandover = "handover"

// EssenceHandover is the source of every hand-over: the card's one row, drawn with the essence itself.
func EssenceHandover() Source {
	return Source{Kind: KindSupply, Skill: SkillEssenceSupply, Action: ActionHandover, Name: RuneEssenceName, ID: ObjRuneEssence, Model: ModelObj}
}

// IsEssenceSupply reports whether a source is the Essence supply card's.
func IsEssenceSupply(s Source) bool { return s.Kind == KindSupply && s.Skill == SkillEssenceSupply }

// EssenceHandedOver reports essence that crossed to the mule (the trade was seen to take it). A nil sink or no essence
// reports nothing. It carries no Items on purpose: see the header.
func EssenceHandedOver(sink func(Drop), essence int) {
	if sink == nil || essence <= 0 {
		return
	}
	sink(Drop{Source: EssenceHandover(), Events: essence})
}
