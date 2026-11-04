package loot

import "rsb0t-go/internal/cachecfg"

// THE FISHING TRAWLER on the loot panel (a user request: "everything the trawler yields must show on the loot tracker").
//
//   - The anchor's claim (MasterCrafter's trawler round): every item it moves out of the trawler_catch window into its pack is
//     a catch of the "Fishing Trawler" row on the Fishing card (KindGathering, Skill Fishing), one event per claim. What it
//     leaves in the window and dumps is not loot: it never reached an account.
//   - A crew member picking a dumped fish off the ground near the net records it on the same row with no event (the claim
//     counted the event).
//   - Swamp paste used on a leak is a cost of that row (Drop.Spent, one paste a leak), so the row reads net of the paste.
//   - A mended net is a production event on the Crafting card ("mend", keyed on Rope: Source.Consumes nets one Rope a mend,
//     the rope content's [label,trawler_fix_net] deletes).
//   - The Port Khazard store's paste, rope and buckets are recorded by the shop watch on the Shop purchases card like every
//     shop buy (shop.go); nothing here reports them.
//
// Fishing experience is not loot: the experience watch hands pending Fishing XP to the claim's drop as it does to any Fishing
// drop (lootxp.go), and refuses the claim's one big rise (thousands of rolls in one tick) as above its cap.

// The trawler's names and ids (content 289).
const (
	// FishingTrawlerName names the row.
	FishingTrawlerName = "Fishing Trawler"
	// TrawlerNetLocID is trawlernetland, the net the catch is claimed at: the row's id and picture.
	TrawlerNetLocID = 2166
	// TrawlerRopeID and TrawlerPasteID are rope and swamppaste.
	TrawlerRopeID  = 954
	TrawlerPasteID = 1941
)

// The trawler's actions, for the panel's verbs.
const (
	ActionClaim = "claim"
	ActionMend  = "mend"
)

// FishingTrawler is the Fishing card's Fishing Trawler row.
func FishingTrawler() Source {
	return Gathering(SkillFishing, ActionClaim, ModelLoc, TrawlerNetLocID, FishingTrawlerName)
}

// TrawlerClaim is the drop of one claim: what moved from the reward window into the pack. Nothing (no event, no item) when
// nothing moved, so a claim the window refused records nothing.
func TrawlerClaim(cfg *cachecfg.Config, taken Counts) (Drop, bool) {
	items := Items(cfg, taken)
	if len(items) == 0 {
		return Drop{}, false
	}
	return Drop{Source: FishingTrawler(), Events: 1, Items: items}, true
}

// TrawlerLeak is the cost of n leaks filled: n Swamp paste spent on the Fishing Trawler row.
func TrawlerLeak(cfg *cachecfg.Config, n int) (Drop, bool) {
	if n <= 0 {
		return Drop{}, false
	}
	return Drop{Source: FishingTrawler(), Spent: Items(cfg, Counts{TrawlerPasteID: int64(n)})}, true
}

// TrawlerMend is the Crafting card's row for the trawler's net: one event a mend, one Rope consumed each.
func TrawlerMend(cfg *cachecfg.Config) Source {
	return Production(SkillCrafting, ActionMend, TrawlerRopeID, objName(cfg, TrawlerRopeID, "Rope"))
}

// TrawlerMended is the drop of n nets mended.
func TrawlerMended(cfg *cachecfg.Config, n int) (Drop, bool) {
	if n <= 0 {
		return Drop{}, false
	}
	return Drop{Source: TrawlerMend(cfg), Events: n}, true
}
