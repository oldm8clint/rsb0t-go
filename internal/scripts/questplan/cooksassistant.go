package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Cook's Assistant (content id `cook`, %cookquest varp 0 not started, 1 started, 2 complete, not transmitted),
// ported from the TS def (defs/cooksassistant.ts) and checked against content 289:
// content/scripts/quests/quest_cook/scripts/quest_cook.rs2, cook_journal.rs2 and areas/area_lumbridge/scripts/cook.rs2.
// The Cook asks for a bucket of milk, an egg and a pot of flour (one of each, any order, all three in the PACK at the
// hand-in: cooks_assistant_inprogress checks inv_total only) and pays 300 Cooking XP and 1 quest point. There is no
// fight, no skill gate and no item the quest hands out, so no death is possible by design.
//
// The quest is decided from the quest tab's colour and the pack, never from the journal's stage: %cookquest is 0 or 1 in
// every state a decision depends on, and the tab says which (red, then yellow, then green). The module still reads the
// stage (Module.ReadStage, ParseCooksAssistantJournal) so the engine's log line names it and cmd/questlanetest's journal
// check has a reader; Decide does not branch on it.
//
// Where each ingredient comes from, in the owner's item rule (provision.go): the account's own bank, then the mule, then
// the world. The egg lies on the Lumbridge farm's chicken coop floor (two static spawns, 3226,3301 and 3229,3299). The
// flour is a SHOP item where a shop is on the way (Wydin's food store in Port Sarim sells it, stock 3 restocking one a
// minute, content port_sarim.inv [wydinstore]; ShopOnTheWay says when it is worth the walk) and the windmill north-west of
// Lumbridge otherwise: a longer chain (an empty pot, a grain from the wheat field, the hopper, its lever, the flour bin)
// the module walks itself, per player (%hopper_full and %flour are perm varps, so no account's grinding is seen by
// another). The milk has no shop in reach (only the Gnome Stronghold stocks it): a bucket (the farmhouse spawn at
// 3225,3294, else the Lumbridge general store) used on a dairy cow (cow_milking.rs2: [opnpcu,cow] with bucket_empty).

// CooksAssistantName is the quest tab's spelling (component 7333, `questlist:cook`).
const CooksAssistantName = "Cook's Assistant"

// CooksAssistantID is the engine's id.
const CooksAssistantID = "cook"

// The %cookquest stages (general/configs/quest.constant).
const (
	CookStarted  = 1
	CookComplete = 2
)

// ParseCooksAssistantJournal reads %cookquest from cook_journal.rs2's rendered text.
func ParseCooksAssistantJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return CookComplete, true
	case strings.Contains(text, "bring him the following ingredients"):
		return CookStarted, true
	case strings.Contains(text, "i can start this quest by speaking to the cook"):
		return 0, true
	}
	return 0, false
}

// Ids from content/pack (checked against the production config by TestCookSheepIdsMatchTheProductionConfig).
const (
	NpcCook         = 278  // [cook], the Lumbridge castle kitchen
	NpcDairyCow     = 81   // [cow]: [opnpcu,cow] with a bucket_empty milks it (cow2 397 and cow_beef 955 do too but are not spawned here)
	ObjEgg          = 1944 // [egg]
	ObjBucketOfMilk = 1927 // [bucket_milk]
	ObjGrain        = 1947 // [grain]
	LocWheat        = 313  // [wheat], op2 Pick, west of the Lumbridge windmill
	LocHopperContr  = 2718 // [hopperlevers1], "Hopper controls", op1 Operate, on the mill's top floor
	LocMillHopper   = 2714 // [hopper_lumbridge], "Hopper", on the mill's top floor
	LocFlourBin     = 1781 // [millbase], "Flour bin", op1 Empty (1782 [millbase_flour] while the bin holds flour)
)

// Tiles from content maps (m50_50, m50_51, m49_51) and the loc index.
var (
	// CookAnchor is the Cook (m50_50, 3209,3215).
	CookAnchor = tile.Tile{X: 3209, Z: 3215}
	// CookEggSpawns are the two egg spawns on the chicken coop floor (m50_51), tried in this order.
	CookEggSpawns = []tile.Tile{{X: 3226, Z: 3301}, {X: 3229, Z: 3299}}
	// FarmBucketSpawn is the empty bucket on the farmhouse floor (m50_51), twenty tiles from the dairy cows.
	FarmBucketSpawn = tile.Tile{X: 3225, Z: 3294}
	// DairyField is the middle of the cow field east of the farm (cow spawns 3243-3261 x 3278-3295, m50_51 and m51_51).
	DairyField = tile.Tile{X: 3252, Z: 3286}
	// KitchenPotSpawn is the empty pot on the castle kitchen floor beside the Cook (m50_50, 3210,3212).
	KitchenPotSpawn = tile.Tile{X: 3210, Z: 3212}
	// MillWheat is the wheat field west of the mill (seven stalks on x 3160, z 3300-3305).
	MillWheat = tile.Tile{X: 3160, Z: 3302}
	// MillTop is the floor tile beside the hopper and its controls on the mill's top floor: orthogonally between the hopper's
	// west stand (3165,3307) and the controls' (3165,3305), one step from the ladder. (3166,3306), between the two locs, is
	// not a floor tile on the real pack.
	MillTop = tile.Tile{X: 3165, Z: 3306, Level: 2}
	// MillBin is the flour bin on the mill's ground floor (a 2x2 loc at 3166,3307).
	MillBin = tile.Tile{X: 3166, Z: 3307}
	// LumbridgeStore is the Lumbridge general store's keeper (npc 520, m50_50 3209,3247): empty pots, buckets and shears.
	LumbridgeStore = ShopRef{Npc: "Shop keeper", NpcID: 520, Anchor: tile.Tile{X: 3209, Z: 3247}}
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomCookMilk = "cook-milk"
	CustomCookMill = "cook-mill"

	KitCook       = "cook"
	KitCookBucket = "cook-bucket"
	KitCookPot    = "cook-pot"
)

// cookFreeSlots is the pack room the gathering steps want before they start: a bucket or pot, a grain and the
// ingredient itself.
const cookFreeSlots = 3

// CookStop starts the quest and takes the ingredients (cook.rs2 and quest_cook.rs2). The first menu is
// "What's wrong?" / "Well you could give me all your money!" / "You don't look very happy." / "Nice hat!", the
// second "Yes, I'll help you." / "No, I don't feel like it. Maybe later."; the in-progress hand-in has no menu.
var CookStop = NpcStop{Npc: "Cook", ID: NpcCook, Anchor: CookAnchor, Leash: 6,
	Prefer: []string{"What's wrong?", "Yes, I'll help you."}}

// cookKeep is what a bank visit of this quest leaves in the pack: the ingredients and what makes them, and the coins.
var cookKeep = []int{ObjEgg, ObjBucketOfMilk, ObjPotOfFlour, ObjBucket, ObjEmptyPot, ObjGrain, CoinsID}

// CookKeepIDs returns the keep list (a copy).
func CookKeepIDs() []int { return append([]int(nil), cookKeep...) }

// cookKit is the one bundled provisioning pass: whatever the bank, the mule, the egg coop's floor and, for an account
// that stands nearer to Port Sarim than to the Cook, Wydin's shelf can give. Every line is optional, because the quest's
// own steps (the cow, the mill) make what these cannot, so a short bank or an empty shelf never blocks the quest.
func cookKit(s Snapshot) []KitLine {
	var eggs []Source
	for _, t := range CookEggSpawns {
		eggs = append(eggs, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "the Lumbridge farm's chicken coop"})
	}
	var flour []Source
	if ShopOnTheWay(s, WydinShop, CookAnchor) {
		flour = append(flour, Source{Kind: SourceShop, Shop: WydinShop, Price: 1, Note: "Wydin's food store keeps 3, restocking one a minute"})
	}
	return []KitLine{
		{ID: ObjEgg, Name: "Egg", Qty: 1, Optional: true, Sources: eggs},
		{ID: ObjBucketOfMilk, Name: "Bucket of milk", Qty: 1, Optional: true},
		{ID: ObjPotOfFlour, Name: "Pot of flour", Qty: 1, Optional: true, Sources: flour},
	}
}

// cookBucketKit is an empty bucket for the cow: the farmhouse floor, else the general store.
func cookBucketKit() []KitLine {
	return []KitLine{{ID: ObjBucket, Name: "Bucket", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: FarmBucketSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the farmhouse floor"},
		{Kind: SourceShop, Shop: LumbridgeStore, Price: 2},
	}}}
}

// cookPotKit is an empty pot for the flour: the castle kitchen's floor, else the general store.
func cookPotKit() []KitLine {
	return []KitLine{{ID: ObjEmptyPot, Name: "Pot", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: KitchenPotSpawn, PerSpawn: 1, RespawnTicks: 100, Note: "the castle kitchen floor"},
		{Kind: SourceShop, Shop: LumbridgeStore, Price: 1},
	}}}
}

// cookToolStep provisions one tool (the bucket, the pot) by the item rule, and waits once that failed: the quest does
// not walk on to a cow it cannot milk.
func cookToolStep(s Snapshot, kitName string, kit []KitLine, what, forWhat string) Step {
	if s.Failed["provision:"+kitName] {
		return Step{Kind: StepWait, Reason: "no " + what + " from the bank, the mule, the world or the general store"}
	}
	return Step{Kind: StepProvision, KitName: kitName, Kit: kit, KeepIDs: cookKeep, Bank: DraynorBank, BankSet: true,
		Reason: "a " + what + " for the " + forWhat}
}

// DecideCooksAssistant is the whole quest.
func DecideCooksAssistant(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: CookStop}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if held(ObjEgg) && held(ObjBucketOfMilk) && held(ObjPotOfFlour) {
		return Step{Kind: StepTalk, Stop: CookStop}
	}
	// One bundled pass first: the bank and the mule answer for all three at once, and the coop and Wydin's shelf are
	// what the item rule tries before the account's own work.
	if !s.Ran["provision:"+KitCook] && !s.Failed["provision:"+KitCook] {
		return Step{Kind: StepProvision, KitName: KitCook, Kit: cookKit(s), KeepIDs: cookKeep, Bank: DraynorBank, BankSet: true,
			Reason: "the egg, the milk and the flour the bank, the mule, the coop or Wydin can give"}
	}
	if s.FreeSlots < cookFreeSlots {
		return Step{Kind: StepDeposit, KeepIDs: cookKeep, Bank: DraynorBank, BankSet: true}
	}
	switch {
	case !held(ObjEgg):
		// The coop's spawns come back a minute after a pickup, and other accounts take them: wait for one.
		return Step{Kind: StepGrabGround, ItemID: ObjEgg, Item: "Egg", Anchor: CookEggSpawns[0], Radius: 90}
	case !held(ObjBucketOfMilk):
		if !held(ObjBucket) {
			return cookToolStep(s, KitCookBucket, cookBucketKit(), "bucket", "cow")
		}
		return Step{Kind: StepCustom, Custom: CustomCookMilk, Reason: "milk a cow in the dairy field"}
	}
	// The flour: the windmill, when Wydin's shelf gave none.
	switch {
	case !held(ObjEmptyPot):
		return cookToolStep(s, KitCookPot, cookPotKit(), "pot", "flour")
	case !held(ObjGrain):
		return Step{Kind: StepPickLoc, LocID: LocWheat, Loc: "Wheat", Op: "Pick", Anchor: MillWheat,
			ExpectItemID: ObjGrain, ExpectItem: "Grain"}
	}
	return Step{Kind: StepCustom, Custom: CustomCookMill, Reason: "grain into the hopper, the lever, the flour bin into the pot"}
}

// CooksAssistantRecord is the quest's identity (no server gates).
var CooksAssistantRecord = Record{ID: CooksAssistantID, Name: CooksAssistantName, QuestPoints: 1}

// CooksAssistant is the engine module. MaxDeaths 1: nothing in it can kill an account, so the first death anywhere on
// its walks parks the quest for a day instead of walking back into whatever did it.
var CooksAssistant = Module{
	Record: CooksAssistantRecord, Bank: DraynorBank, BankSet: true, Anchor: CookAnchor,
	ToolIDs:   cookKeep,
	ReadStage: ParseCooksAssistantJournal,
	MaxDeaths: 1,
	Decide:    DecideCooksAssistant,
}
