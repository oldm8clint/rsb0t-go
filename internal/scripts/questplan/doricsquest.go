package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Doric's Quest (content id `doric`, %doricquest 0 not started, 10 started, 100 complete; not transmitted), ported from the TS
// def (defs/doric.ts) and checked against content 289: quests/quest_doric/scripts/quest_doric.rs2 and doric_journal.rs2.
//
// Doric, the dwarf smith north of Falador, wants 6 clay, 4 copper ore and 2 iron ore (all three in the PACK at the hand-in:
// doric_materials checks inv_total only, no menu) and pays 180 coins, 1300 Mining XP (stat_advance(mining, 13000)) and 1 quest
// point. There is no fight, no skill gate on the quest and nothing it hands out, so no death is possible by design.
//
// The quest is decided from the quest tab's colour and the pack, never from the journal's stage: %doricquest is 0 or 10 in every
// state a decision depends on, and the tab says which (red, then yellow, then green). The module still reads the stage
// (Module.ReadStage, ParseDoricsQuestJournal) so the engine's log line names it and cmd/questlanetest's journal check has a
// reader; Decide does not branch on it.
//
// Where the ores come from, in the owner's item rule (provision.go): the account's own bank, then the mule, then the world.
//   - The mule publishes copper ore (5,334 on 18 Oct 2026) and iron ore (11,811) and no clay and no pickaxe, so one bundled pass
//     (KitDoricOres) answers for the copper and the iron of an account whose trainer banked none, and for the clay of an account
//     that kept some.
//   - What is left is mined, in Rimmington's mine south of Falador (content m46_50: two clay rocks at 2986,3239 and 2987,3240,
//     copper at 2976-2979,3245-3248, iron at 2968-2982,3233-3242, all within twenty tiles): CustomDoricMine, with the best
//     pickaxe the account owns at its Mining level. Clay and copper need Mining 1 and iron needs 15 (iron_rock_table). An account
//     below 15 whose bank and mule hold no iron cannot mine it and the quest parks for DoricIronParkFor with the reason said; it
//     is never trained to 15 for 2 ores (138 copper ores at 17.5 xp), the ladder holds no Mining rung and the Doric reward is
//     itself Mining XP.
//   - The pickaxe: an owned one (pack, worn, or the bank when it was read), else the free Bronze pickaxe on the table south of
//     the mine (content m46_50 obj 1265 at 2963,3216, one spawn that comes back after 100 ticks), else Bob's Axes in Lumbridge,
//     which stocks five at 1 gp. Mining levels: bronze and iron 0, steel 6, mithril 21, adamant 31, rune 41 (pickaxes.obj).
//
// The walks stay in the settled south-west: Rimmington, Port Sarim, Falador and Doric's smithy north of it are all free-to-play
// ground, none is near the Wilderness (the first row of which is z 3520; Doric stands at z 3451), and the walker keeps clear of
// White Wolf Mountain (x 2828-2878) on the road to Taverley.

// DoricsQuestName is the quest tab's spelling (component 7336, `questlist:doric`). The journal's own title is spelt "Dorics' Quest".
const DoricsQuestName = "Doric's Quest"

// DoricsQuestID is the engine's id.
const DoricsQuestID = "doric"

// The %doricquest stages (quest_doric.rs2).
const (
	DoricNotStarted = 0
	DoricStarted    = 10
	DoricComplete   = 100
)

// Ids from content/pack (checked against the production config by TestDoricFluffsIDsMatchTheProductionConfig).
const (
	NpcDoric       = 284 // [doric], the smithy north of Falador
	DoricClay      = 434 // [clay]
	DoricCopperOre = 436 // [copper_ore]
	DoricIronOre   = 440 // [iron_ore]
)

// Rock locs of the Rimmington mine (content skill_mining/configs/mine.dbrow and rocks.loc): every ore rock is named "Rocks" with
// the op "Mine", so the type id is what tells them apart. Each ore has two variants of the model.
var (
	DoricClayRocks   = []int{2108, 2109} // [clayrock1], [clayrock2]
	DoricCopperRocks = []int{2090, 2091} // [copperrock1], [copperrock2]
	DoricIronRocks   = []int{2092, 2093} // [ironrock1], [ironrock2]
)

// How much Doric wants, and the Mining level iron needs.
const (
	DoricClayQty   = 6
	DoricCopperQty = 4
	DoricIronQty   = 2
	DoricIronLevel = 15
)

// DoricIronParkFor is how long the quest skips an account that lacks iron ore and the Mining level to mine it: its level does not
// change in an hour and the mule's stock can come back, so it is looked at again at the next ladder pass, not every few minutes.
const DoricIronParkFor = 6 * time.Hour

// Tiles from content maps (m46_50 and the loc and obj indexes) and the keepers.
var (
	// DoricAnchor is Doric (m46_52, 2952,3451), inside his smithy.
	DoricAnchor = tile.Tile{X: 2952, Z: 3451}
	// RimmingtonMine is the middle of Rimmington's mine: the iron rocks to its west, the copper to the north and the clay to the
	// east are all inside twelve tiles of it.
	RimmingtonMine = tile.Tile{X: 2977, Z: 3241}
	// RimmingtonClay, RimmingtonCopper and RimmingtonIron are a stand beside each ore's rocks, for the walk there.
	RimmingtonClay   = tile.Tile{X: 2985, Z: 3240}
	RimmingtonCopper = tile.Tile{X: 2977, Z: 3244}
	RimmingtonIron   = tile.Tile{X: 2970, Z: 3241}
	// RimmingtonPickaxe is the free Bronze pickaxe's spawn on the table south of the mine (m46_50 obj 1265 at local 19,16).
	RimmingtonPickaxe = tile.Tile{X: 2963, Z: 3216}
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomDoricMine = "doric-mine"

	KitDoricOres = "doric-ores"
	KitDoricPick = "doric-pick"
)

// DoricStop starts the quest at the first of its two menus and takes the ores. The first menu is "I wanted to use your anvils." /
// "Mind your own business, shortstuff!" / "I was just checking out the landscape." / "What do you make here?", the second
// "Yes, I will get you materials." / "No, hitting rocks is for the boring people, sorry."; the hand-in has no menu.
var DoricStop = NpcStop{Npc: "Doric", ID: NpcDoric, Anchor: DoricAnchor, Leash: 6,
	Prefer: []string{"I wanted to use your anvils.", "Yes, I will get you materials."}}

// pickaxeRung is one pickaxe and the Mining level the server wants to use it (pickaxe_checker.rs2).
type pickaxeRung struct {
	ID    int
	Name  string
	Level int
}

// doricPickaxes are the pickaxes best first, with the Mining level each needs.
var doricPickaxes = []pickaxeRung{
	{1275, "Rune pickaxe", 41},
	{1271, "Adamant pickaxe", 31},
	{1273, "Mithril pickaxe", 21},
	{1269, "Steel pickaxe", 6},
	{1267, "Iron pickaxe", 0},
	{ObjBronzePickaxe, "Bronze pickaxe", 0},
}

// doricKeep is what a bank visit of this quest leaves in the pack: the ores, every pickaxe and the coins.
var doricKeep = func() []int {
	keep := []int{DoricClay, DoricCopperOre, DoricIronOre, CoinsID}
	for _, p := range doricPickaxes {
		keep = append(keep, p.ID)
	}
	return keep
}()

// DoricKeepIDs returns the keep list (a copy).
func DoricKeepIDs() []int { return append([]int(nil), doricKeep...) }

// ParseDoricsQuestJournal reads %doricquest from doric_journal.rs2's rendered text. In progress the journal also lists the
// three ingredients (struck through once the pack holds them), which says nothing of the stage.
func ParseDoricsQuestJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return DoricComplete, true
	case strings.Contains(text, "i need to collect some items"):
		return DoricStarted, true
	case strings.Contains(text, "i can start this quest by talking to") && strings.Contains(text, "doric"):
		return DoricNotStarted, true
	}
	return 0, false
}

// DoricUsablePickaxe is the id of the best pickaxe the account holds (pack or worn) and can use at its Mining level, 0 for none.
func DoricUsablePickaxe(s Snapshot) int {
	lvl := s.Level("mining")
	for _, p := range doricPickaxes {
		if lvl >= p.Level && (s.HeldID(p.ID) > 0 || s.WornIDs[p.ID] > 0) {
			return p.ID
		}
	}
	return 0
}

// DoricBankedPickaxe is the best usable pickaxe the last bank read saw, 0 for none or an unread bank.
func DoricBankedPickaxe(s Snapshot) int {
	lvl := s.Level("mining")
	for _, p := range doricPickaxes {
		if n, known := s.BankedID(p.ID); known && n > 0 && lvl >= p.Level {
			return p.ID
		}
	}
	return 0
}

// doricBank is the bank a visit uses: the one nearest the account, else Falador's east bank.
func doricBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return NearestBank(DoricAnchor).Tile
}

// doricOreKit asks the bank and the mule for all three ores. The lines are optional and carry no source of their own: what
// neither holds is mined (CustomDoricMine), so a short bank never blocks the quest.
func doricOreKit() []KitLine {
	return []KitLine{
		{ID: DoricClay, Name: "Clay", Qty: DoricClayQty, Optional: true},
		{ID: DoricCopperOre, Name: "Copper ore", Qty: DoricCopperQty, Optional: true},
		{ID: DoricIronOre, Name: "Iron ore", Qty: DoricIronQty, Optional: true},
	}
}

// doricPickKit is the pickaxe for the mine: the best usable one the bank holds, else the Bronze pickaxe from the table at the
// mine, else Bob's Axes.
func doricPickKit(s Snapshot) []KitLine {
	if id := DoricBankedPickaxe(s); id != 0 {
		for _, p := range doricPickaxes {
			if p.ID == id {
				return []KitLine{{ID: p.ID, Name: p.Name, Qty: 1}}
			}
		}
	}
	return []KitLine{{ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: RimmingtonPickaxe, PerSpawn: 1, RespawnTicks: 100, Note: "the table south of Rimmington's mine"},
		{Kind: SourceShop, Shop: BobsAxes, Price: 1, Note: "Bob keeps five, Lumbridge"},
	}}}
}

// DecideDoricsQuest is the whole quest.
func DecideDoricsQuest(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: DoricStop}
	}
	clay, copper, iron := s.HeldID(DoricClay), s.HeldID(DoricCopperOre), s.HeldID(DoricIronOre)
	if clay >= DoricClayQty && copper >= DoricCopperQty && iron >= DoricIronQty {
		return Step{Kind: StepTalk, Stop: DoricStop}
	}
	// The bank and the mule answer for all three at once, at no walk to a rock: the mule holds copper and iron ore by the
	// thousand. One pass, before anything is mined.
	if !s.Ran["provision:"+KitDoricOres] && !s.Failed["provision:"+KitDoricOres] {
		return Step{Kind: StepProvision, KitName: KitDoricOres, Kit: doricOreKit(), KeepIDs: doricKeep,
			Bank: doricBank(s), BankSet: true, Reason: "the clay and ores the bank or the mule can give"}
	}
	needClay, needCopper, needIron := max(DoricClayQty-clay, 0), max(DoricCopperQty-copper, 0), max(DoricIronQty-iron, 0)
	mineIron := s.Level("mining") >= DoricIronLevel
	if needClay == 0 && needCopper == 0 && !mineIron {
		// Only iron is missing and the account cannot mine it: a level, not a thing to retry in five minutes.
		return Step{Kind: StepWait, ParkFor: DoricIronParkFor,
			Reason: "2 iron ore needed: the bank and the mule hold none and Mining is below 15, which iron rocks need"}
	}
	pick := DoricUsablePickaxe(s)
	slots := needClay + needCopper
	if mineIron {
		slots += needIron
	}
	if pick == 0 {
		slots++
	}
	if s.FreeSlots < slots {
		return Step{Kind: StepDeposit, KeepIDs: doricKeep, Bank: doricBank(s), BankSet: true,
			Reason: "room for the ores and the pickaxe"}
	}
	if pick == 0 {
		if s.Failed["provision:"+KitDoricPick] {
			return Step{Kind: StepWait, Reason: "no pickaxe from the bank, the mule, the table at the mine or Bob's Axes"}
		}
		return Step{Kind: StepProvision, KitName: KitDoricPick, Kit: doricPickKit(s), KeepIDs: doricKeep,
			Bank: doricBank(s), BankSet: true, Reason: "a pickaxe for the Rimmington rocks"}
	}
	return Step{Kind: StepCustom, Custom: CustomDoricMine, Reason: "clay, copper and iron from Rimmington's mine"}
}

// DoricsQuestRecord is the quest's identity (no server gates).
var DoricsQuestRecord = Record{ID: DoricsQuestID, Name: DoricsQuestName, QuestPoints: 1}

// DoricsQuest is the engine module. MaxDeaths 1: nothing in it can kill an account, so the first death anywhere on its walks
// parks the quest for a day instead of walking back into whatever did it.
var DoricsQuest = Module{
	Record: DoricsQuestRecord, Bank: NearestBank(DoricAnchor).Tile, BankSet: true, Anchor: DoricAnchor,
	ToolIDs:   doricKeep,
	ReadStage: ParseDoricsQuestJournal,
	MaxDeaths: 1,
	Decide:    DecideDoricsQuest,
}
