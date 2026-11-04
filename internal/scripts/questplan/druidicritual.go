package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Druidic Ritual (content id `druid`, %druidquest, perm and not transmitted), ported from the TS def (defs/druidicritual.ts) and
// checked against content 289: quests/quest_druid/{configs,scripts}, areas/area_taverly/scripts/{kaqemeex,sanfew}.rs2,
// areas/area_taverly/dungeon/scripts/prison_doors.rs2, the drop tables of the four animals and the maps (m49_50, m49_51, m50_50,
// m50_51, m45_153). Four quest points, 250 Herblore experience (stat_advance 2500 at 10x: Herblore level 3, which is what
// Jungle Potion asks for).
//
// The story, with the stage each step moves %druidquest to:
//
//	0  Kaqemeex (the druids' circle north of Taverley): "I'm in search of a quest." then "Ok, I will try and help.": 1.
//	1  Sanfew (the Taverley herblore house, upstairs): "I've been sent to help purify the Varrock stone circle.", then
//	   "Ok, I'll do that then.": 2. He wants the raw meat of four animals, each dipped in the Cauldron of Thunder.
//	2  The cauldron ([oplocu,cauldron_of_thunder]) turns a raw rat meat, beef, bear meat or chicken into its enchanted form,
//	   and only at this stage ("nothing interesting happens" otherwise). Sanfew takes the four enchanted meats in one go,
//	   without a menu, as soon as the pack holds all of them: 3.
//	3  Kaqemeex again, no menu: the quest completes in a queue (250 Herblore experience, the quest complete scroll, then
//	   the fundamentals of Herblore as a chat).
//
// What the content does that a script must plan for:
//   - The meats. All four animals drop theirs every time (bear.rs2, giant_rat.rs2, chicken.rs2, cow.rs2: obj_add of the raw meat
//     whatever the roll): the Lumbridge region has all of them within a hundred tiles. Wydin's food store sells a raw beef and a
//     raw chicken (port_sarim.inv [wydinstore], one of each, restocking every hundred ticks) and the Canifis meat shop sells all four
//     (across the swamp, not walked here); the mule holds none. The bank and the mule come first, then Wydin's for an account
//     nearer Port Sarim than the field, then the account's own work, as the item rule says.
//   - The enchanted forms are untradeable (quest_druid.obj) and the Stage 5 trainer banks what it does not keep between two
//     runs, so a half-finished run's meats are in the bank: they are drawn back first.
//   - The cauldron's door. Opening the Prison door of the Taverley dungeon from either side but the doorway itself (prison_doors.rs2) first looks for a
//     suit of armour standing in the corridor (loc 818 at 2887,9832 and 2887,9829): the first one it finds is deleted for 500 ticks
//     and an npc suit of armour (npc 453, level 19, 29 hitpoints, a maximum hit of 3) takes its place, attacks the account, and the
//     door does not open. The next opening finds the other suit; the third opens. So the door costs two fights of a level 19 foe
//     that hits for 3 every five ticks (priced by MeleeChain), and another account's suit within the last 500 ticks removes one.
//   - The dungeon. The ladder at 2884,3397 leads into the Taverley dungeon, where the way north to the cauldron room is one hall,
//     nine tiles wide (x 2880 to 2888). The planner's lines up it and back down run within two tiles of the spawns of five unarmed
//     skeletons, over four of the five (DruidCorridorSkeletons: level 25, 17 hitpoints, a hit of 3, hunt range 1, wander range 8,
//     respawn in 140 ticks); a sixth stands three tiles off the line and an armed one (level 22) five tiles from the door. They
//     hunt any account of combat level 50 or less and, the dungeon being single-combat, one at a time as each finds the account out
//     of combat for eight ticks. The line is walked twice, in and out, with no bank between and no eating inside a chat, so the
//     trip is priced as every one of those fights to the death (DruidDungeonFoes: the skeletons ahead, the door's two suits, the
//     skeletons again on the way out) with the food the account goes down with, and refused (a park of hours) when the model says
//     it loses. A poison spider (level 64, a hit of 7) wanders ten tiles round a spawn nine tiles off the line: it is not priced,
//     and the walk, which is not cautious, outruns it. The walk is not bent round the skeletons either: the hall's west wall is
//     four tiles from that spawn, which is the worse company.
//
// The item rule for what the quest needs (provision.go): the bank and the mule first (enchanted meats are drawn back from the
// bank, raw ones too), Wydin's two for an account nearer Port Sarim than the field, the account's own kills last.

// DruidicRitualName is the quest tab's spelling (component 7355, `questlist:druid`).
const DruidicRitualName = "Druidic Ritual"

// DruidicRitualID is the engine's id.
const DruidicRitualID = "druid"

// The %druidquest stages (quest_druid.constant, general/configs/quest.constant).
const (
	DruidNotStarted       = 0
	DruidStarted          = 1
	DruidSpokenSanfew     = 2
	DruidGivenIngredients = 3
	DruidComplete         = 4
)

// Ids from content/pack (checked against the production config by TestDruidJungleIdsMatchTheProductionConfig). The raw beef is
// ObjRawBeef of witchspotion.go, the dairy cow NpcDairyCow of cooksassistant.go and the shark ObjShark of horror.go: one name for
// one object in the package.
const (
	NpcKaqemeex     = 455 // [kaqemeex], the druids' circle
	NpcSanfew       = 454 // [sanfew], the herblore house
	NpcSuitOfArmour = 453 // [suit_of_armour], the animated suit of the cauldron's door
	NpcBrownBear    = 105 // [brownbear], "Bear", level 21
	NpcGiantRat     = 87  // [giantrat1], level 6
	NpcGiantRatCub  = 86  // [giantrat], level 3 (category giantrat: the same drop)
	NpcChicken      = 41  // [chicken], level 1

	ObjRawChicken        = 2138 // [raw_chicken]
	ObjRawRatMeat        = 2134 // [raw_rat_meat]
	ObjRawBearMeat       = 2136 // [raw_bear_meat]
	ObjEnchantedBeef     = 522  // [enchanted_beef], untradeable
	ObjEnchantedRatMeat  = 523  // [enchanted_rat_meat], untradeable
	ObjEnchantedBearMeat = 524  // [enchanted_bear_meat], untradeable
	ObjEnchantedChicken  = 525  // [enchanted_chicken], untradeable

	LocCauldronOfThunder = 2142 // [cauldron_of_thunder], used with a raw meat
	LocCauldronDoor      = 2143 // [cauldrondoor], op1 Open, "Prison door" (right leaf)
	LocCauldronDoorLeft  = 2144 // [cauldrondoor_l], op1 Open, "Prison door" (left leaf)
	LocSuitOfArmourLoc   = 818  // [suitofarmour_darkknight], the loc the door animates
)

// Tiles from content maps and the loc index.
var (
	// KaqemeexAnchor and SanfewAnchor are the two druids' spawns (m45_54 and m45_53; Sanfew is on the upper floor of his house).
	KaqemeexAnchor = tile.Tile{X: 2924, Z: 3486}
	SanfewAnchor   = tile.Tile{X: 2897, Z: 3426, Level: 1}
	// DruidDungeonSurface is where the Taverley dungeon's ladder (loc 1759 at 2884,3397) is climbed from, DruidDungeonFoot the
	// stand of the way up (loc 1755 at 2884,9797).
	DruidDungeonSurface = tile.Tile{X: 2885, Z: 3397}
	DruidDungeonFoot    = tile.Tile{X: 2884, Z: 9796}
	// CauldronDoorOutside is the corridor tile the Prison door is opened from, CauldronDoorThreshold the door's own tile, where the
	// door script puts the account (and where it is opened from to leave: from there it walks the account out to the corridor),
	// CauldronDoorInside the tile just inside it and CauldronStand a tile beside the cauldron (loc 2142 at 2893,9831).
	CauldronDoorOutside   = tile.Tile{X: 2888, Z: 9831}
	CauldronDoorThreshold = tile.Tile{X: 2889, Z: 9831}
	CauldronDoorInside    = tile.Tile{X: 2890, Z: 9831}
	CauldronStand         = tile.Tile{X: 2892, Z: 9831}
	// SuitNorth and SuitSouth are the two corridor tiles the door's suits stand on (prison_doors.rs2: 0_45_153_7_40 and _37).
	SuitNorth = tile.Tile{X: 2887, Z: 9832}
	SuitSouth = tile.Tile{X: 2887, Z: 9829}
	// DruidHuntHome is the middle of the hunting grounds (the Lumbridge region west and north of the castle): the place a shop is
	// measured against (ShopOnTheWay).
	DruidHuntHome = tile.Tile{X: 3190, Z: 3240}
)

// InCauldronRoom reports a tile in the room the Prison door shuts in: the 55 tiles the planner reaches on foot from the cauldron
// (x 2889-2897, z 9826-9836). Its first column, x 2889, is the door's own: opened from the corridor, the door puts the account on
// the threshold (CauldronDoorThreshold) and not a step further in (open_and_close_double_door2 with $entering false teleports to the
// loc's own tile), so a tile there is the room, and the stand the door is opened from to leave it.
func InCauldronRoom(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2889 && t.X <= 2897 && t.Z >= 9826 && t.Z <= 9836
}

// InTaverleyDungeon reports a tile in the part of the Taverley dungeon the quest walks (its ladder at 2884,9797 to the cauldron
// room, with the rooms either side of the corridor), by the map squares m44_153 to m45_154.
func InTaverleyDungeon(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2816 && t.X <= 2950 && t.Z >= 9728 && t.Z <= 9900
}

// The foes of the quest, stats from content (the .npc files named at each).
var (
	// FoeSuitOfArmour is [suit_of_armour] (npc 453, scripts/_unpack/225/all.npc): attack 16, strength 14, defence 9, 29 hitpoints,
	// attackrate 5, attack bonus 8, strength bonus 10 (a maximum hit of 3), defence bonuses 46/50/45. It has no hunt mode: the
	// door's script sets it on the account that opened the door (~npc_retaliate), and it stays 500 ticks.
	FoeSuitOfArmour = MeleeFoe{Name: "Suit of armour", Attack: 16, Strength: 14, Defence: 9, Magic: 1, Hitpoints: 29,
		AttackTicks: 5, AttackBonus: 8, StrengthBonus: 10, DefenceBonus: 45}
	// FoeBrownBear is [brownbear] (npc 105): attack 17, strength 18, defence 15, 27 hitpoints, no attackrate (4) and no bonus
	// params (a maximum hit of 3). Huntmode cowardly with hunt range 1: it hunts an account of combat 42 or less only when one
	// stands beside it, and fights back whoever attacks it.
	FoeBrownBear = MeleeFoe{Name: "Bear", Attack: 17, Strength: 18, Defence: 15, Magic: 1, Hitpoints: 27, AttackTicks: 4}
)

// DruidMeat is one of the ritual's four meats.
type DruidMeat struct {
	// Name is "bear", "rat", "chicken" or "beef", the word the quest's logs and custom steps use.
	Name string
	// Raw and Enchanted are the two object ids; RawName and EnchantedName their display names.
	Raw, Enchanted         int
	RawName, EnchantedName string
	// Npcs are the npc ids whose death drops Raw (content drop tables: every kill).
	Npcs []int
	// Field is where the hunt starts and Radius how far from it the spawns lie; Spawns are the spawn tiles round it (content maps).
	Field  tile.Tile
	Radius int
	Spawns []tile.Tile
	// Wydin says Wydin's food store sells the raw meat.
	Wydin bool
}

// DruidMeats are the four meats in the order a hunt visits them: bear, rat, chicken and beef lie on one road from the
// Lumbridge woods west of the castle, past its south-east swamp, up to the farm and the dairy field.
var DruidMeats = []DruidMeat{
	{Name: "bear", Raw: ObjRawBearMeat, Enchanted: ObjEnchantedBearMeat, RawName: "Raw bear meat", EnchantedName: "Enchanted bear", Npcs: []int{NpcBrownBear},
		Field: tile.Tile{X: 3167, Z: 3228}, Radius: 14, Spawns: []tile.Tile{{X: 3159, Z: 3233}, {X: 3176, Z: 3223}}},
	{Name: "rat", Raw: ObjRawRatMeat, Enchanted: ObjEnchantedRatMeat, RawName: "Raw rat meat", EnchantedName: "Enchanted rat", Npcs: []int{NpcGiantRatCub, NpcGiantRat},
		Field: tile.Tile{X: 3195, Z: 3205}, Radius: 10, Spawns: []tile.Tile{{X: 3192, Z: 3207}, {X: 3194, Z: 3203}, {X: 3195, Z: 3207}, {X: 3197, Z: 3201}}},
	{Name: "chicken", Raw: ObjRawChicken, Enchanted: ObjEnchantedChicken, RawName: "Raw chicken", EnchantedName: "Enchanted chicken", Npcs: []int{NpcChicken}, Wydin: true,
		Field: tile.Tile{X: 3188, Z: 3278}, Radius: 8, Spawns: []tile.Tile{{X: 3185, Z: 3277}, {X: 3186, Z: 3279}, {X: 3187, Z: 3278}, {X: 3189, Z: 3278}, {X: 3191, Z: 3277}}},
	{Name: "beef", Raw: ObjRawBeef, Enchanted: ObjEnchantedBeef, RawName: "Raw beef", EnchantedName: "Enchanted beef", Npcs: []int{NpcDairyCow}, Wydin: true,
		Field: tile.Tile{X: 3252, Z: 3286}, Radius: 14},
}

// DruidMeatByRaw is the meat whose raw form has this object id.
func DruidMeatByRaw(id int) (DruidMeat, bool) {
	for _, m := range DruidMeats {
		if m.Raw == id {
			return m, true
		}
	}
	return DruidMeat{}, false
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomDruidEnter = "druid-enter"
	CustomDruidDip   = "druid-dip"

	KitDruidMeats = "druid-meats"
)

// CustomDruidHunt is the custom step that kills for m's raw meat: one name per meat, so a hunt that failed is told from one that
// did not (Snapshot.Failed is keyed by the step's name).
func CustomDruidHunt(m DruidMeat) string { return "druid-hunt-" + m.Name }

// DruidFoodIDs are the foods the quest carries and eats, best first.
var DruidFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// druidSustain is the quest's survival policy: its foods, and an eat line the fights raise to the foe's (Sustain.VsFoe).
var druidSustain = Sustain{FoodIDs: DruidFoodIDs, EatAt: 15}

// druidTools are the quest's own items, which every bank visit of the quest leaves in the pack: the coins of the shop, every
// form of the four meats and the food.
var druidTools = func() []int {
	out := []int{CoinsID}
	for _, m := range DruidMeats {
		out = append(out, m.Raw, m.Enchanted)
	}
	return out
}()

// druidKeep is a bank visit's keep list: the tools and the food.
func druidKeep() []int { return append(append([]int(nil), druidTools...), DruidFoodIDs...) }

// DruidKeepIDs returns the keep list (a copy).
func DruidKeepIDs() []int { return druidKeep() }

// The quest's gates and prices.
const (
	// DruidMinHitpoints is the module's own floor: the dungeon's corridor holds skeletons and poison spiders, and the model
	// (DruidDungeonOdds) judges the rest. The server gates nothing.
	DruidMinHitpoints = 30
	// DruidOddsPark is how long the quest is parked when the model says the account loses to the bear or to the door's suits
	// even with the food it could draw: the hitpoints do not mend in an hour.
	DruidOddsPark = 3 * time.Hour
	// DruidHuntPark is how long a hunt that found nothing to kill (or could not reach it) parks the quest.
	DruidHuntPark = 30 * time.Minute
	// DruidDungeonPark is how long a trip to the cauldron that did not get through the door parks the quest.
	DruidDungeonPark = 30 * time.Minute
)

// The stops. A talk step must name a preference, so the talks with no menu carry the first line of an earlier menu of the
// same NPC, which an unexpected menu would simply not match.
var (
	// KaqemeexStartStop starts the quest: two menus (the first: "Who are you?", "I'm in search of a quest.", "Did you build this?";
	// the second: "Ok, I will try and help.", "No, that doesn't sound very interesting.", "So... is there anything in this for me?").
	KaqemeexStartStop = NpcStop{Npc: "Kaqemeex", ID: NpcKaqemeex, Anchor: KaqemeexAnchor, Leash: 8,
		Prefer: []string{"I'm in search of a quest.", "Ok, I will try and help."}}
	// KaqemeexRewardStop finishes the quest: no menu at stage 3 (druid_completion).
	KaqemeexRewardStop = NpcStop{Npc: "Kaqemeex", ID: NpcKaqemeex, Anchor: KaqemeexAnchor, Leash: 8,
		Prefer: []string{"I'm in search of a quest."}}
	// SanfewBriefStop takes the job (stage 1): "I've been sent to help purify the Varrock stone circle." then "Ok, I'll do that then."
	SanfewBriefStop = NpcStop{Npc: "Sanfew", ID: NpcSanfew, Anchor: SanfewAnchor, Leash: 8,
		Prefer: []string{"I've been sent to help purify the Varrock stone circle.", "Ok, I'll do that then."}}
	// SanfewHandInStop gives him the four enchanted meats (stage 2): no menu when the pack holds them all. Were one missing, the
	// menu he would raise ("What was I meant to be doing again?" / "I'll get on with it.") ends with the second line.
	SanfewHandInStop = NpcStop{Npc: "Sanfew", ID: NpcSanfew, Anchor: SanfewAnchor, Leash: 8,
		Prefer: []string{"I'll get on with it."}}
)

// druidHops are the Taverley dungeon's ladders, the fallback for a level change the walk graph does not bake (it bakes both).
var druidHops = []LadderHop{
	{Stand: DruidDungeonSurface, LocName: "Ladder", Op: "Climb-down", Arrive: tile.Tile{X: 2884, Z: 9798}},
	{Stand: DruidDungeonFoot, LocName: "Ladder", Op: "Climb-up", Arrive: tile.Tile{X: 2885, Z: 3397}},
}

// ParseDruidicRitualJournal reads %druidquest from druid_journal.rs2's rendered text, newest page first (the later pages keep the
// earlier lines, struck through); ok is false for an empty or unrecognised journal, which is unknown, never stage 0.
func ParseDruidicRitualJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return DruidComplete, true
	case strings.Contains(text, "claim my reward"):
		return DruidGivenIngredients, true
	case strings.Contains(text, "sanfew told me for the ritual"):
		return DruidSpokenSanfew, true
	case strings.Contains(text, "i should speak to sanfew"):
		return DruidStarted, true
	case strings.Contains(text, "i can start this quest by speaking to kaqemeex"):
		return DruidNotStarted, true
	}
	return 0, false
}

// DruidMeatsHeld says which of the four meats the pack holds in some form (raw or enchanted) and which in the enchanted form.
func DruidMeatsHeld(s Snapshot) (held, enchanted int) {
	for _, m := range DruidMeats {
		if s.HeldID(m.Enchanted) > 0 {
			enchanted++
			held++
		} else if s.HeldID(m.Raw) > 0 {
			held++
		}
	}
	return held, enchanted
}

// DruidPastTheHunt reports the account past the quest's shared steps (the animals and Wydin's shelf): every meat is in the pack in
// some form, or the hand-in is behind it. The fleet-wide slot is held until then.
func DruidPastTheHunt(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	if s.Journal != StatusInProgress || !s.StageKnown {
		return false
	}
	if s.Stage >= DruidGivenIngredients {
		return true
	}
	if s.Stage == DruidSpokenSanfew {
		held, _ := DruidMeatsHeld(s)
		return held == len(DruidMeats)
	}
	return false
}

// DruidSharks is the food a pass asks the bank or the mule for. The corridor's gauntlet is priced as up to twelve fights on one pack
// (DruidDungeonFoes) and an account of 40/40/40 with 30 hitpoints is carried through them by eight sharks and not by five
// (TestDruidDungeonIsPricedOnWhatTheRouteMeets); the mule holds tens of thousands.
const DruidSharks = 8

// druidFoodLines is the food a pass asks the bank or the mule for: DruidSharks sharks, optional, so a bank and a mule with none do not
// block the quest: the odds below decide whether the account goes without. One kind only: a line for each of several foods is a line
// the bank fills each, and the pack has the room for the meats and no more.
func druidFoodLines() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: DruidSharks, Optional: true}}
}

// druidMeatKit is the one bundled pass at the start of the meat stage, for the meats the pack holds in no form: the bank (the
// enchanted forms a half-finished run banked, and any raw one), the mule, Wydin's raw beef and raw chicken for an account nearer
// Port Sarim than the hunting grounds, and the food. Every line is optional: the quest's own hunts make what these cannot, so a
// short bank or an empty shelf never blocks the quest.
func druidMeatKit(s Snapshot, missing []DruidMeat) []KitLine {
	var kit []KitLine
	for _, m := range missing {
		kit = append(kit, KitLine{ID: m.Enchanted, Name: m.EnchantedName, Qty: 1, Untradeable: true, Optional: true})
		line := KitLine{ID: m.Raw, Name: m.RawName, Qty: 1, Optional: true, OnlyIfShort: m.Enchanted}
		if m.Wydin && ShopOnTheWay(s, WydinShop, DruidHuntHome) {
			line.Sources = []Source{{Kind: SourceShop, Shop: WydinShop, Price: 3, Note: "Wydin's food store keeps one raw beef and one raw chicken, restocking in a hundred ticks"}}
		}
		kit = append(kit, line)
	}
	return append(kit, druidFoodLines()...)
}

// DruidBearOdds prices the bear hunt on the pack: the bear (level 21, 27 hitpoints, a hit of 3) fought with the account's own
// Attack, Strength and Defence, from the hitpoints it has, with the food it carries (an account carrying none is refused: the
// model keeps a bite for the way out).
func DruidBearOdds(s Snapshot) (bool, string) {
	odds := MeleeChain(s, []MeleeFoe{FoeBrownBear}, druidSustain)
	return odds.OK, odds.Why
}

// DruidCorridorSkeletons are the spawn tiles of the unarmed skeletons (npc 90, [skeleton_unarmed]) the planned way down the
// Taverley dungeon's hall passes within DruidCorridorReach tiles of, in the order it meets them (maps/m45_153.jm2). The planner's
// line runs up x 2885 from the ladder's foot and back down x 2886, so that between them the two ways pass over four of these tiles
// and within two of the fifth, and each way within two of all five (TestDruidDungeonRouteMeetsTheSkeletons walks them on the real
// pack). They are priced as engaged, every one, on each pass: a skeleton wanders eight tiles round its spawn, so the whole hall
// (nine tiles wide) is its ground, and a walk bent to the west wall would keep four tiles from the nearest spawn and as many from
// the poison spider's. The sixth unarmed skeleton (2882,9826) is three tiles off the line, and the armed one (npc 92, level 22,
// 29 hitpoints, at 2884,9836) five from the door: both are left out, as the cave's jogres beyond two tiles are (JungleCaveJogres).
var DruidCorridorSkeletons = []tile.Tile{{X: 2886, Z: 9812}, {X: 2886, Z: 9816}, {X: 2885, Z: 9819}, {X: 2887, Z: 9821}, {X: 2885, Z: 9823}}

// DruidCorridorReach is how near a skeleton's spawn the planned line must pass for the skeleton to count as engaged.
const DruidCorridorReach = 2

// DruidDungeonFoes is the run of fights the rest of the trip to the cauldron and back is priced as, from where the account stands:
// the skeletons still ahead on the way in (all of them from the surface or the ladder's foot, those not yet passed from inside the
// corridor), the door's two suits of armour, and every skeleton again on the way out. The ones killed on the way in are priced as
// back by then (the suits, the door and the dips take about as long as their 140 ticks), and nothing can be bought or banked
// between. A tile that is not known counts as the surface.
func DruidDungeonFoes(at tile.Tile, known bool) []MeleeFoe {
	ahead := len(DruidCorridorSkeletons)
	if known && InTaverleyDungeon(at) {
		ahead = 0
		for _, sp := range DruidCorridorSkeletons {
			if sp.Z >= at.Z-DruidCorridorReach {
				ahead++
			}
		}
	}
	var foes []MeleeFoe
	for i := 0; i < ahead; i++ {
		foes = append(foes, FoeManorSkeleton)
	}
	foes = append(foes, FoeSuitOfArmour, FoeSuitOfArmour)
	for range DruidCorridorSkeletons {
		foes = append(foes, FoeManorSkeleton)
	}
	return foes
}

// DruidDungeonOdds prices the trip to the cauldron's door and back (DruidDungeonFoes) in the shared fight model: each fight to the
// death, with the top-up the fight step takes before its first blow, on the hitpoints and the food the account has now. FoeManorSkeleton
// is [skeleton_unarmed] (TestQuestFoesMatchTheContent pins it to the content under npc 90).
func DruidDungeonOdds(s Snapshot) (bool, string) {
	foes := DruidDungeonFoes(s.Tile, s.TileKnown)
	odds := MeleeChain(s, foes, druidSustain)
	if odds.OK {
		return true, ""
	}
	return false, fmt.Sprintf("the way to the cauldron and back is %d fights (the corridor's skeletons both ways, the door's two suits between): %s", len(foes), odds.Why)
}

// druidBank is the bank a pack-making visit uses: the nearest to the account, Draynor's for an account in Lumbridge (which has
// none) and Al Kharid's toll gate (cookSheepBank).
func druidBank(s Snapshot) tile.Tile { return cookSheepBank(s) }

// druidMissing are the meats the pack holds in no form, in hunting order.
func druidMissing(s Snapshot) []DruidMeat {
	var out []DruidMeat
	for _, m := range DruidMeats {
		if s.HeldID(m.Enchanted) == 0 && s.HeldID(m.Raw) == 0 {
			out = append(out, m)
		}
	}
	return out
}

// druidOddsPark is the wait that parks the quest when a priced step is refused.
func druidOddsPark(what, why string) Step {
	return Step{Kind: StepWait, ParkFor: DruidOddsPark, Reason: fmt.Sprintf("not %s: %s", what, why)}
}

// decideDruidMeats is stage 2: the four meats, then the cauldron, then Sanfew.
func decideDruidMeats(s Snapshot) Step {
	_, enchanted := DruidMeatsHeld(s)
	if enchanted == len(DruidMeats) {
		return Step{Kind: StepTalk, Stop: SanfewHandInStop}
	}
	missing := druidMissing(s)
	bank := druidBank(s)
	// Room for what is still to be carried (a meat each) and a spare slot.
	if s.FreeSlots < len(missing)+1 {
		return Step{Kind: StepDeposit, KeepIDs: druidKeep(), Bank: bank, BankSet: true, Reason: "room for the ritual meats"}
	}
	if len(missing) > 0 {
		// The bank, the mule, Wydin's shelf and the food in one pass, then the account's own hunts.
		if !s.Ran["provision:"+KitDruidMeats] && !s.Failed["provision:"+KitDruidMeats] {
			return Step{Kind: StepProvision, KitName: KitDruidMeats, Kit: druidMeatKit(s, missing), KeepIDs: druidKeep(), Bank: bank, BankSet: true,
				Reason: "the meats the bank, the mule or Wydin's shelf can give, and food"}
		}
		m := missing[0]
		if m.Name == "bear" {
			if ok, why := DruidBearOdds(s); !ok {
				return druidOddsPark("hunting the bear", why)
			}
		}
		if s.Failed[CustomDruidHunt(m)] {
			return Step{Kind: StepWait, ParkFor: DruidHuntPark, Reason: fmt.Sprintf("the hunt for the %s found nothing to kill or could not reach it", m.Name)}
		}
		return Step{Kind: StepCustom, Custom: CustomDruidHunt(m), ItemID: m.Raw, Reason: fmt.Sprintf("kill a %s for its raw meat", m.Name)}
	}
	// Every meat is in the pack, at least one of them raw: the cauldron.
	if InCauldronRoom(s.Tile) {
		if s.Failed[CustomDruidDip] {
			return Step{Kind: StepWait, ParkFor: DruidDungeonPark, Reason: "the raw meats would not take the Cauldron of Thunder"}
		}
		return Step{Kind: StepCustom, Custom: CustomDruidDip, Reason: "dip the raw meats in the Cauldron of Thunder"}
	}
	// Priced from wherever the account stands, inside the dungeon too: what is left of the trip is the skeletons not yet passed,
	// the suits and the way out, and an account that came down on food it has since eaten is not sent on to the door.
	if ok, why := DruidDungeonOdds(s); !ok {
		return druidOddsPark("going down the Taverley dungeon to the cauldron", why)
	}
	if s.Failed[CustomDruidEnter] {
		return Step{Kind: StepWait, ParkFor: DruidDungeonPark, Reason: "the way down the Taverley dungeon to the cauldron's room did not open"}
	}
	return Step{Kind: StepCustom, Custom: CustomDruidEnter, Reason: "down the Taverley dungeon, through the door of the suits of armour, to the Cauldron of Thunder"}
}

// DecideDruidicRitual is the whole quest, read from the journal stage and the pack.
func DecideDruidicRitual(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: KaqemeexStartStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case DruidNotStarted:
		return Step{Kind: StepTalk, Stop: KaqemeexStartStop}
	case DruidStarted:
		return Step{Kind: StepTalk, Stop: SanfewBriefStop}
	case DruidSpokenSanfew:
		return decideDruidMeats(s)
	case DruidGivenIngredients:
		return Step{Kind: StepTalk, Stop: KaqemeexRewardStop}
	case DruidComplete:
		return Step{Kind: StepDone}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Druidic Ritual stage"}
}

// DruidicRitualRecord is the quest's identity. The server gates nothing; Hitpoints 30 is the module's own floor (the corridor of
// the Taverley dungeon).
var DruidicRitualRecord = Record{ID: DruidicRitualID, Name: DruidicRitualName, QuestPoints: 4,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "hitpoints", Level: DruidMinHitpoints}}}}

// DruidicRitual is the engine module. One death parks it for a day: the dungeon and the bear are priced, so a death means the price
// was wrong for this account, and the meats it carried are gone.
var DruidicRitual = Module{
	Record: DruidicRitualRecord, Hops: druidHops, Bank: FaladorWestBank, BankSet: true, Anchor: SanfewAnchor,
	ToolIDs:       druidTools,
	ReadStage:     ParseDruidicRitualJournal,
	Sustain:       druidSustain,
	MaxDeaths:     1,
	ExclusiveDone: DruidPastTheHunt,
	Decide:        DecideDruidicRitual,
}
