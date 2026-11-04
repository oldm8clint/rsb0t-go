package mastercraftplan

// THE GOLDSMITH GAUNTLETS (a user request: "Gold: superheat gold ore into gold bars. At Magic 59 with
// Crafting/Smithing/Mining 40, Family Crest gives Goldsmith gauntlets ... record the quest requirement
// and build the gauntlets-worn check + wear").
//
// THE CONTENT (content/scripts, branch 289):
//
//   - Superheat Item on gold ore (skill_magic/scripts/spells/superheat.rs2): the gold bar's Smithing XP is
//     scaled 5/2 when gauntlets_of_goldsmithing are WORN (inv_total(worn, gauntlets_of_goldsmithing)):
//     22.5 becomes 56.25 a bar, with the Magic XP of the cast (53) unchanged. A furnace does the same
//     (smelting.rs2). Gold takes one ore and no coal, so a load is 28 casts.
//   - The gauntlets are the Family Crest quest's reward (quests/quest_crest): steel gauntlets at the end
//     of the quest (crest_complete), which Avan improves into the goldsmith ones when he is asked
//     (crest_man.rs2 [label,avan_gauntlets]: the choice is made once, goldsmithing/cooking/chaos, and the
//     steel gauntlets must be carried). "Goldsmith gauntlet" is obj 776, members, tradeable=no, and
//     content gives them back from Dimintheis when lost (crest_dimintheis.rs2).
//   - The quest's own journal (crest_journal.rs2) lists what it needs: 40 Mining, 40 Crafting, 40
//     Smithing and 59 Magic, and the ability to defeat a level 170 demon: Chronozon under the Air obelisk
//     in the Wilderness dies only once hit with each of Wind, Water, Earth and Fire Blast (Magic 59 is
//     Fire Blast's level). Its other legs: Dimintheis in East Varrock, Caleb the chef in Catherby (a
//     cooked Swordfish, Bass, Tuna, Salmon and Shrimp), Avan in the desert north of Al Kharid, Boot the
//     dwarf, a ruby ring and necklace of 'perfect gold' from underground east of Ardougne, and
//     Johnathon at the Jolly Boar Inn (an antipoison potion), then Chronozon.
//
// NOT BUILT: Family Crest is not in the quest ladder (internal/scripts/questplan has no step for it), so
// nothing in the fleet completes it. The round below wears the gauntlets when the account holds them
// and says what is missing when it does not. An account that completes the quest by hand (and has the
// gauntlets in its pack or bank) gets the 2.5x with no further change.

import (
	"fmt"
	"strings"
)

// Goldsmith gauntlets, by name and object id (content pack/obj.pack: 776=gauntlets_of_goldsmithing).
const (
	GoldGauntlets         = "Goldsmith gauntlet"
	GoldGauntletsID int32 = 776
	// GoldBarName is the bar the gauntlets scale.
	GoldBarName = "Gold bar"
	// GoldGauntletsNum and GoldGauntletsDen are the scale content applies to the gold bar's Smithing XP
	// (scale(5, 2, xp)).
	GoldGauntletsNum, GoldGauntletsDen = 5, 2
)

// QuestRequirement is one skill a quest needs.
type QuestRequirement struct {
	Skill string
	Level int
}

// FamilyCrestRequirements are the levels the quest's journal lists (crest_journal.rs2), the account's
// Magic 59 being what the four Blast spells Chronozon needs ask for (Fire Blast).
var FamilyCrestRequirements = []QuestRequirement{
	{"Mining", 40}, {"Crafting", 40}, {"Smithing", 40}, {"Magic", 59},
}

// FamilyCrestShort lists the requirements the levels (by skill name) do not meet, in words ("Mining 37 of
// 40"), empty when all are met.
func FamilyCrestShort(levels map[string]int) []string {
	var out []string
	for _, r := range FamilyCrestRequirements {
		if have := levels[r.Skill]; have < r.Level {
			out = append(out, fmt.Sprintf("%s %d of %d", r.Skill, have, r.Level))
		}
	}
	return out
}

// XPTenthsWith is the Smithing XP in content's tenths one cast of the bar gives: the gold bar's scaled
// 5/2 with the goldsmith gauntlets worn (22.5 to 56.25), every other bar's as it is.
func (b Bar) XPTenthsWith(gauntlets bool) int {
	if b.Gold && gauntlets {
		return b.XPTenths * GoldGauntletsNum / GoldGauntletsDen
	}
	return b.XPTenths
}

// GoldInPlay is whether the superheat round may cast gold ore at this Smithing level: craftSuperheatGold
// is on, the bar list allows it and Smithing is 40.
func GoldInPlay(set Settings, smithing int) bool {
	for _, b := range SuperheatBars(set, smithing) {
		if b.Gold {
			return true
		}
	}
	return false
}

// GauntletAction is what the round does about the gauntlets.
type GauntletAction string

// The actions.
const (
	// GauntletsNotNeeded: gold is not in play, nothing to do.
	GauntletsNotNeeded GauntletAction = "not-needed"
	// GauntletsWorn: already worn, the 2.5x applies.
	GauntletsWorn GauntletAction = "worn"
	// GauntletsWear: in the pack, wear them.
	GauntletsWear GauntletAction = "wear"
	// GauntletsDraw: in the bank, draw one and wear it.
	GauntletsDraw GauntletAction = "draw"
	// GauntletsMissing: neither worn nor held, the gold bars give the plain XP.
	GauntletsMissing GauntletAction = "missing"
)

// GauntletStep decides what to do about the gauntlets: gold in play (GoldInPlay), the gauntlets worn,
// and how many the pack and the bank hold.
func GauntletStep(goldInPlay, worn bool, inPack, inBank int) GauntletAction {
	switch {
	case !goldInPlay:
		return GauntletsNotNeeded
	case worn:
		return GauntletsWorn
	case inPack > 0:
		return GauntletsWear
	case inBank > 0:
		return GauntletsDraw
	}
	return GauntletsMissing
}

// GauntletLine is the log line for an action: what the gold bars will give, or what is missing and why
// the quest is not built. short is FamilyCrestShort of the account's levels.
func GauntletLine(a GauntletAction, short []string) string {
	plain := GoldBarXPString(false)
	switch a {
	case GauntletsWorn:
		return "gold: Goldsmith gauntlets worn: a gold bar gives " + GoldBarXPString(true) + " Smithing XP, not " + plain
	case GauntletsWear:
		return "gold: wearing the Goldsmith gauntlets from the pack (" + GoldBarXPString(true) + " Smithing XP a gold bar, not " + plain + ")"
	case GauntletsDraw:
		return "gold: drawing the Goldsmith gauntlets from the bank and wearing them (" + GoldBarXPString(true) + " Smithing XP a gold bar, not " + plain + ")"
	case GauntletsMissing:
		s := "gold: no Goldsmith gauntlets worn, in the pack or in the bank, so " + plain + " Smithing XP a gold bar; they are Family Crest's reward (Mining 40, Crafting 40, Smithing 40, Magic 59 and a level 170 Chronozon), and the quest is NOT in the quest ladder (not built)"
		if len(short) > 0 {
			s += "; this account is short of " + strings.Join(short, ", ")
		}
		return s
	}
	return ""
}

// GoldBarXPString is the gold bar's Smithing XP as the client shows it ("22.5", "56.2": content's
// scale(5, 2, 225) is integer arithmetic in tenths, 562, where the comment in superheat.rs2 says 56.25).
func GoldBarXPString(gauntlets bool) string {
	for _, b := range Bars {
		if b.Gold {
			tenths := b.XPTenthsWith(gauntlets)
			if tenths%10 == 0 {
				return fmt.Sprintf("%d", tenths/10)
			}
			return fmt.Sprintf("%d.%d", tenths/10, tenths%10)
		}
	}
	return ""
}
