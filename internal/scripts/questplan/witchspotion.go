package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Witch's Potion (content id `hetty`, %hetty, not transmitted), ported from defs/hetty.ts and
// checked against content 289 by the quest-chain study
// (private evidence notes). Every ingredient is
// matched by EXACT id: Onion, Eye of newt, Burnt meat, Raw beef and Cooked meat all have noted
// forms with the same display name, and Hetty accepts only the unnoted ones, from the pack.
//
// The traps the study found, all handled here:
//   - a Rat's tail sitting in the BANK blocks every future drop (rat.rs2 checks inv and bank),
//     so a banked tail is withdrawn, never farmed for;
//   - the tail drops only at stage 1 or 2, so the quest is started first;
//   - Wydin keeps ONE raw beef, restocking slowly; a failed purchase is simply chosen again on
//     the next pass, and the engine's watchdog parks the quest rather than let it loop;
//   - the range in Hetty's house (loc 2728) approaches from the west only, which the engine's
//     locreach stand handles.

// WitchsPotionName is the quest tab's spelling (internal/quests: component 7348).
const WitchsPotionName = "Witch's Potion"

// WitchsPotionID is the engine's id.
const WitchsPotionID = "hetty"

// Ids from content/pack.
const (
	NpcHetty      = 307
	NpcRat        = 47
	NpcBetty      = 583
	NpcWydin      = 557
	ObjRatsTail   = 300
	ObjOnion      = 1957
	ObjEyeOfNewt  = 221
	ObjBurntMeat  = 2146
	ObjCookedMeat = 2142
	ObjRawBeef    = 2132
	LocCauldron   = 2024
	LocHettyRange = 2728
	LocOnionPlant = 3366
)

// Tiles from content maps (m46_50, m47_50).
var (
	HettyAnchor    = tile.Tile{X: 2968, Z: 3206}
	HettyCauldron  = tile.Tile{X: 2967, Z: 3205}
	HettyRangeTile = tile.Tile{X: 2970, Z: 3209}
	// RimmingtonRats is inside the rat house (m46_50: walls x 2953-2960, z 3201-3205; the rats
	// spawn at 2953,3204, 2955,3202, 2959,3202 and 2960,3205 inside it).
	RimmingtonRats  = tile.Tile{X: 2956, Z: 3203}
	RimmingtonOnion = tile.Tile{X: 2950, Z: 3251}
	BettyShop       = ShopRef{Npc: "Betty", NpcID: NpcBetty, Anchor: tile.Tile{X: 3012, Z: 3259}}
	WydinShop       = ShopRef{Npc: "Wydin", NpcID: NpcWydin, Anchor: tile.Tile{X: 3014, Z: 3204}}
)

// InRatHouse reports a tile inside the Rimmington rat house.
func InRatHouse(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2953 && t.X <= 2960 && t.Z >= 3201 && t.Z <= 3205
}

// Custom step names (internal/scripts/questmods).
const (
	CustomHettyHandIn = "hetty-hand-in"
	CustomHettyRat    = "hetty-rat"
	// CustomHettyDrink drinks and CONTINUES the mesbox: quest_hetty.rs2 queues the completion
	// only after the mesbox's pause returns, so a bare loc op leaves the quest at stage 2 (the
	// second Witch's Potion EMPTY lab run parked there for an hour).
	CustomHettyDrink = "hetty-drink"
)

// Kit names.
const (
	KitHetty     = "hetty"
	KitHettyNewt = "hetty-newt"
	KitHettyBeef = "hetty-beef"
)

// HettyStop starts the quest (hetty.rs2's two menus).
var HettyStop = NpcStop{Npc: "Hetty", ID: NpcHetty, Anchor: HettyAnchor, Leash: 6,
	Prefer: []string{"I am in search of a quest.", "Yes help me become one with my darker side."}}

// Stages (quest_hetty.constant): 0 not started, 1 started, 2 objects given, 3 complete.
const (
	HettyStarted      = 1
	HettyObjectsGiven = 2
	HettyComplete     = 3
)

// ParseWitchsPotionJournal reads the stage from hetty_journal.rs2's rendered text.
func ParseWitchsPotionJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return HettyComplete, true
	case strings.Contains(text, "i should drink from the cauldron"):
		return HettyObjectsGiven, true
	case strings.Contains(text, "hetty needs me to bring her the following"):
		return HettyStarted, true
	case strings.Contains(text, "i can start this quest by speaking to hetty"):
		return 0, true
	}
	return 0, false
}

// hettyKit is the provision pass: the eye of newt is the only line a shop can fill here; the
// rest are taken from the bank (or the mule) when there and otherwise made in the quest's own
// steps (picked, burnt, looted), so they are optional in the kit.
func hettyKit() []KitLine {
	return []KitLine{
		{ID: ObjEyeOfNewt, Name: "Eye of newt", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: 3}}},
		{ID: ObjOnion, Name: "Onion", Qty: 1, Optional: true},
		{ID: ObjBurntMeat, Name: "Burnt meat", Qty: 1, Optional: true},
		{ID: ObjRatsTail, Name: "Rat's tail", Qty: 1, Optional: true, Untradeable: true},
	}
}

var hettyKeep = []int{ObjRatsTail, ObjOnion, ObjEyeOfNewt, ObjBurntMeat, ObjCookedMeat, ObjRawBeef, CoinsID}

// DecideWitchsPotion is the whole quest.
func DecideWitchsPotion(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: HettyStop}
	}
	if s.StageKnown && s.Stage == HettyObjectsGiven {
		return Step{Kind: StepCustom, Custom: CustomHettyDrink, Reason: "drink from the cauldron and continue its message"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if held(ObjRatsTail) && held(ObjOnion) && held(ObjEyeOfNewt) && held(ObjBurntMeat) {
		return Step{Kind: StepCustom, Custom: CustomHettyHandIn, Reason: "all four ingredients held"}
	}
	if !s.Ran["provision:"+KitHetty] && !s.Failed["provision:"+KitHetty] {
		return Step{Kind: StepProvision, KitName: KitHetty, Kit: hettyKit(), KeepIDs: hettyKeep,
			Bank: DraynorBank, BankSet: true, Reason: "the ingredients the bank, the mule or Betty can give"}
	}
	if n, known := s.BankedID(ObjRatsTail); known && n > 0 && !held(ObjRatsTail) {
		// A banked tail blocks every drop: take it out rather than farm for another.
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Rat's tail", ID: ObjRatsTail, Qty: 1}}, Bank: DraynorBank, BankSet: true}
	}
	if s.FreeSlots < 2 {
		return Step{Kind: StepDeposit, KeepIDs: hettyKeep, Bank: DraynorBank, BankSet: true}
	}
	switch {
	// Shop items come only through the provisioner (the item rule's shop rung), never a bare
	// buy: a bypass would let a quest pass with its provisioner disabled, which is exactly what
	// the lab's CONTROL arm must not be able to do.
	case !held(ObjEyeOfNewt):
		if s.Failed["provision:"+KitHettyNewt] {
			return Step{Kind: StepWait, Reason: "no eye of newt from the bank, the mule or Betty"}
		}
		return Step{Kind: StepProvision, KitName: KitHettyNewt, KeepIDs: hettyKeep, Bank: DraynorBank, BankSet: true,
			Kit:    []KitLine{{ID: ObjEyeOfNewt, Name: "Eye of newt", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: BettyShop, Price: 3}}}},
			Reason: "an eye of newt"}
	case !held(ObjOnion):
		return Step{Kind: StepPickLoc, LocID: LocOnionPlant, Loc: "Onion", Op: "Pick", Anchor: RimmingtonOnion,
			ExpectItemID: ObjOnion, ExpectItem: "Onion"}
	case !held(ObjBurntMeat):
		for _, raw := range []int{ObjCookedMeat, ObjRawBeef} {
			if held(raw) {
				// Cooked meat on a range always burns; raw beef burns about half the time at low
				// Cooking and otherwise cooks, which the next pass burns.
				return Step{Kind: StepUseOn, ItemID: raw, Item: "meat", TargetKind: TargetLoc,
					TargetID: LocHettyRange, Target: "Range", Anchor: HettyRangeTile}
			}
		}
		if s.Failed["provision:"+KitHettyBeef] {
			return Step{Kind: StepWait, Reason: "no meat to burn from the bank, the mule or Wydin"}
		}
		return Step{Kind: StepProvision, KitName: KitHettyBeef, KeepIDs: hettyKeep, Bank: DraynorBank, BankSet: true,
			Kit:    []KitLine{{ID: ObjRawBeef, Name: "Raw beef", Qty: 1, Sources: []Source{{Kind: SourceShop, Shop: WydinShop, Price: 1}}}},
			Reason: "raw beef to burn"}
	}
	return Step{Kind: StepCustom, Custom: CustomHettyRat, Reason: "a Rimmington rat for its tail"}
}

// WitchsPotionRecord is the quest's identity (no server gates).
var WitchsPotionRecord = Record{ID: WitchsPotionID, Name: WitchsPotionName, QuestPoints: 1}

// WitchsPotion is the engine module.
var WitchsPotion = Module{
	Record: WitchsPotionRecord, Bank: DraynorBank, BankSet: true, Anchor: HettyAnchor,
	ToolIDs:   hettyKeep,
	ReadStage: ParseWitchsPotionJournal,
	Decide:    DecideWitchsPotion,
}
