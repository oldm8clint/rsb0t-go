package questplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Knight's Sword (content id `squire`, %squire 0 to 7, not transmitted: the quest tab's colour and the journal are what the client
// sees), ported from the TS def (defs/knightssword/*.ts) and checked against content 289: quests/quest_squire/{configs,scripts},
// areas/area_falador/scripts/squire.rs2, areas/area_varrock/scripts/reldo.rs2, areas/area_port_sarim/scripts/thurgo.rs2,
// general_use/scripts/{cupboards,water_sources}.rs2, general_use/configs/cupboards.loc, skill_cooking (pies, dough, cooking.rs2),
// skill_mining (mining.rs2, mine.dbrow) and the maps (m46_52, m46_149 to m48_149). One quest point and 12,725 Smithing experience
// (stat_advance(smithing, 127250)).
//
// The story, with the stage each step moves %squire to:
//
//	0   The Squire (the White Knights' Castle courtyard, 2977,3342) has lost Sir Vyvin's sword: "And how is life as a squire?" /
//	    "I can make a new sword if you like..." / "So would these dwarves make another one?" / "Ok, I'll give it a go." (1).
//	1   Reldo (the Varrock palace library, 3209,3495): "What do you know about the Imcando dwarves?" (2): they live on the Asgarnian
//	    peninsula and love redberry pie.
//	2   Thurgo (3001,3144, Mudskipper Point): "Would you like a redberry pie?" takes a redberry pie from the pack (3). Anything else
//	    he says at this stage leaves the stage alone.
//	3   Thurgo again: "Can you make me a special sword?" wants a picture of the sword (4).
//	4   The Squire: "Sir Vyvin keeps a portrait in a cupboard in his room" (5).
//	5   The portrait: Sir Vyvin's cupboard (loc 2271 at 2984,3336, level 2), opened and searched. At stage 5 or later, and only while
//	    Sir Vyvin is not within one tile of the player (vyvin_distracted), the search gives a portrait to an account holding none in
//	    the pack or the bank. Thurgo takes it: "About that sword..." (6).
//	6   Two iron bars and a blurite ore (the only rocks are in the Asgarnian Ice Dungeon under Thurgo's cliff) make the sword,
//	    which Thurgo hands over. The Squire takes it from the PACK (a worn one is refused, a banked one he sends for) and pays (7).
//
// What the content does that a script must plan for:
//   - The pie is the longest part. A redberry pie wants a pie shell (pastry dough on a pie dish), redberries and Cooking 10 twice: the
//     redberries go into the shell only at Cooking 10 (make_uncooked_pie) and the range cooks the pie only at Cooking 10 (the
//     struct and cooking_generic_redberry_pie both say 10). The range burns about one pie in two at that level (successchance 98,452:
//     131 in 256 at Cooking 10), and a burnt pie keeps the dish ("Empty Dish") but loses the dough and the berries, so the chain is
//     decided from the pack, backwards from the pie, and the flour and berries are asked for in threes.
//   - The dough is a menu. A pot of flour used on a bucket (or jug, or bowl) of water asks "What sort of dough do you wish to make?"
//     (Bread, Pastry, Pizza, Pitta) and the pot, the bucket and the dough come back; only the pastry dough makes a shell. A bucket
//     is filled at a sink, a fountain or a well (_watersource, _well).
//   - Everything but the shell's dish is cheap and near. Rimmington's house at 2965-2970,3209 holds a sink and a range four tiles
//     apart and is a walk of 65 steps from Wydin's food store (a shelf of three pots of flour and one redberry, restocking one a minute),
//     so the chain is made there; the pie dish is a ground spawn in the Varrock palace kitchen and the mule holds 267 of them.
//   - Sir Vyvin wanders ten tiles round his room, which is barely wider, and the cupboard may be searched only from its south side
//     (forceapproach=east at angle 1, so east in the loc's frame is south in the world): 2985,3335 or 2984,3335. A search with Sir Vyvin
//     within one tile is answered by a dialogue and a message box that must be continued. The cupboard stays open 300 ticks.
//   - The blurite rocks are four (loc 2110 at 3049,9566, 3059,9564, 3067,9581 and 3067,9582), Mining 10, a roll every 7 ticks with a
//     bronze pickaxe (rock_successchance 95,350), and every one of them has Ice warriors (level 57, hits up to 6, huntrange 5, wander 3)
//     or Ice giants (level 53, up to 7) within reach: the dungeon killed more than twenty-five accounts in October 2026. See
//     knightsdungeon.go for the price of the visit.
//
// The item rule: the bars come from the bank or the mule and are never smelted (Smithing is 1 on the fleet's accounts and the iron
// bar wants 15), the food comes from the same two, and the pie's shop items from Wydin; the rest is the kit's own sources. The quest
// never fights: the module turns auto-retaliate off and the dungeon is walked through, priced and eaten through, never fought.

// KnightsSwordName is the quest tab's spelling (component 7346, `questlist:squire`).
const KnightsSwordName = "The Knight's Sword"

// KnightsSwordID is the engine's id.
const KnightsSwordID = "squire"

// The %squire stages (quest_squire.constant, general/configs/quest.constant).
const (
	SqNotStarted      = 0
	SqStarted         = 1
	SqSpokenReldo     = 2
	SqGivenPie        = 3
	SqSpokenThurgo    = 4
	SqLookingPortrait = 5
	SqLookingBlurite  = 6
	SqComplete        = 7
)

// Ids from content/pack (checked against the production config by TestSquireIdsMatchTheProductionConfig).
const (
	NpcSquire = 606 // [squire]
	NpcVyvin  = 605 // [sir_vyvin], wanderrange 10
	NpcReldo  = 647 // [reldo]
	NpcThurgo = 604 // [thurgo]

	ObjSqPortrait     = 666  // [knights_portrait]
	ObjSqSword        = 667  // [faladian_sword], "Blurite sword"
	ObjSqBluriteOre   = 668  // [blurite_ore]
	ObjSqPieDish      = 2313 // [piedish]
	ObjSqPieShell     = 2315 // [pie_shell]
	ObjSqUncookedPie  = 2321 // [uncooked_redberry_pie], "Uncooked berry pie"
	ObjSqRedberryPie  = 2325 // [redberry_pie], op Eat: a tool, never food, to the floor bite
	ObjSqBurntPie     = 2329 // [burnt_pie], op Empty Dish
	ObjSqPastryDough  = 1953 // [pastry_dough]
	ObjSqRedberries   = 1951 // [redberries], op Eat: a tool too
	ObjSqPotOfFlour   = 1933 // [pot_flour]
	ObjSqEmptyPot     = 1931 // [pot_empty]
	ObjSqBucket       = 1925 // [bucket_empty]
	ObjSqBucketWater  = 1929 // [bucket_water]
	ObjSqIronBar      = 2351 // [iron_bar]
	LocSqCupboardShut = 2271 // [vyvincupboardshut], op Open
	LocSqCupboardOpen = 2272 // [vyvincupboardopen], op Search (Shut)
	LocSqBluriteRock  = 2110 // [bluriterock], "Rocks", op Mine
	LocSqRange        = 2728 // the cooking ranges ("Range")
	LocSqSink         = 873  // [sink], "Sink"
	LocSqSinkAlt      = 874  // [sink2], "Sink"
	LocSqFountain     = 879  // [fountain]
	LocSqWell         = 884  // [well], a bucket only
)

// The levels. SqMining and SqCooking are the content's (the rock and the redberry pie); SqMinHitpoints and SqMinCombat are this module's:
// the Asgarnian Ice Dungeon is priced in knightsdungeon.go and the account is refused below them (the Ice warrior rung of the
// Stage 5 trainer is gated at the same 60 hitpoints for the same dungeon).
const (
	SqMining       = 10
	SqCooking      = 10
	SqMinHitpoints = 60
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomSquireFill     = "squire-fill"     // a bucket filled at a sink, a well or a fountain
	CustomSquireDough    = "squire-dough"    // flour on the bucket of water: pastry dough
	CustomSquireShell    = "squire-shell"    // pastry dough on the pie dish: a pie shell
	CustomSquireBerries  = "squire-berries"  // redberries on the shell: an uncooked pie
	CustomSquireCook     = "squire-cook"     // the uncooked pie on a range
	CustomSquireEmpty    = "squire-empty"    // the burnt pie emptied: the dish back
	CustomSquirePortrait = "squire-portrait" // Sir Vyvin's cupboard
	CustomSquireBlurite  = "squire-blurite"  // the ice dungeon: one ore
	CustomSquireUnwield  = "squire-unwield"  // the sword taken off

	KitSquirePie     = "squire-pie"     // the pie's raw ingredients
	KitSquireBerries = "squire-berries" // redberries from the ground, when the shelf and the mule have none
	KitSquireOre     = "squire-ore"     // bars, a pickaxe and food for the dungeon
	KitSquireFood2   = "squire-food2"   // the second kind of food, when the first did not cover the trip
)

// Park times: a level does not change in an hour, the mule's stock changes by the hour, and a leg that failed on its last run is looked at
// again after a short while.
const (
	SqLevelParkFor = 6 * time.Hour
	SqItemParkFor  = 3 * time.Hour
	SqRetryParkFor = 30 * time.Minute
	// SqTripRetryFor is how long a dungeon visit that came out with no ore (the budget ran out, the stand was not reached, the account was
	// hurt and left) is not tried again.
	SqTripRetryFor = time.Hour
)

// Tiles from content maps (m46_52, m47_50 and the loc index) and the keepers.
var (
	// SqSquireAnchor is the Squire (m46_52, 2977,3342), in the castle's courtyard.
	SqSquireAnchor = tile.Tile{X: 2977, Z: 3342}
	// SqReldoAnchor is Reldo (m50_55, 3209,3495), in the palace library.
	SqReldoAnchor = tile.Tile{X: 3209, Z: 3495}
	// SqThurgoAnchor is Thurgo (m47_49, 3001,3144).
	SqThurgoAnchor = tile.Tile{X: 3001, Z: 3144}
	// SqLadderTop is the Asgarnian Ice Dungeon's ladder (loc 1759 at 3008,3150) and its only legal stand, the tile east of it
	// (forceapproach, internal/webwalk graph.go); the dungeon's landing is SqLanding.
	SqLadderTop = tile.Tile{X: 3009, Z: 3150}
	SqLanding   = tile.Tile{X: 3008, Z: 9551}
	// SqVyvinApproaches are the two tiles the cupboard is operable from (south of its two tiles, level 2); SqVyvinRetreat is a tile of
	// the room the account stands on to give Sir Vyvin both approaches to wander off.
	SqVyvinApproaches = []tile.Tile{{X: 2985, Z: 3335, Level: 2}, {X: 2984, Z: 3335, Level: 2}}
	SqVyvinRetreat    = tile.Tile{X: 2980, Z: 3340, Level: 2}
	SqCupboardAt      = tile.Tile{X: 2984, Z: 3336, Level: 2}
)

// SqWaterSpot is a place a bucket is filled at: the loc, where it stands, and the loc's ids.
type SqWaterSpot struct {
	Name string
	Loc  tile.Tile
	IDs  []int
}

// SqRangeSpot is a range worth cooking at.
type SqRangeSpot struct {
	Name string
	Loc  tile.Tile
}

// SqWaterSpots are the water sources the module fills its bucket at, from the loc index: Rimmington's sink and well (the house with the
// range), Falador's fountain beside the general store, and the Varrock palace kitchen's sink beside the pie dish spawn. The module walks to
// the one nearest the account (SqNearestWater).
var SqWaterSpots = []SqWaterSpot{
	{"Rimmington's sink", tile.Tile{X: 2966, Z: 3209}, []int{LocSqSink, LocSqSinkAlt}},
	{"Rimmington's well", tile.Tile{X: 2956, Z: 3212}, []int{LocSqWell}},
	{"Falador's fountain", tile.Tile{X: 2949, Z: 3381}, []int{LocSqFountain}},
	{"the Varrock palace kitchen's sink", tile.Tile{X: 3224, Z: 3494}, []int{LocSqSink, LocSqSinkAlt}},
}

// SqRangeSpots are the ranges, from the loc index: Rimmington's house, Port Sarim's, Falador's east side and the Varrock palace kitchen's.
var SqRangeSpots = []SqRangeSpot{
	{"Rimmington's range", tile.Tile{X: 2970, Z: 3209}},
	{"Port Sarim's range", tile.Tile{X: 3019, Z: 3237}},
	{"Falador's range", tile.Tile{X: 2988, Z: 3365}},
	{"the Varrock palace kitchen's range", tile.Tile{X: 3221, Z: 3497}},
}

// SqNearestWater is the water source nearest to t on the same side of the underground line, by Chebyshev distance.
func SqNearestWater(t tile.Tile) SqWaterSpot {
	best := SqWaterSpots[0]
	for _, w := range SqWaterSpots[1:] {
		if tile.Chebyshev(t, w.Loc) < tile.Chebyshev(t, best.Loc) {
			best = w
		}
	}
	return best
}

// SqNearestRange is the range nearest to t.
func SqNearestRange(t tile.Tile) SqRangeSpot {
	best := SqRangeSpots[0]
	for _, r := range SqRangeSpots[1:] {
		if tile.Chebyshev(t, r.Loc) < tile.Chebyshev(t, best.Loc) {
			best = r
		}
	}
	return best
}

// The stops. The Squire's four menus are the start's (the in-progress talks have none), Reldo's one, Thurgo's the pie's menu (the other
// stages' talks have none: thurgo_special_sword_post_pie and thurgo_about_sword are straight runs of dialogue).
var (
	SqSquireStop = NpcStop{Npc: "Squire", ID: NpcSquire, Anchor: SqSquireAnchor, Leash: 6,
		Prefer: []string{"And how is life as a squire?", "I can make a new sword if you like", "So would these dwarves make another one?", "Ok, I'll give it a go."}}
	SqReldoStop = NpcStop{Npc: "Reldo", ID: NpcReldo, Anchor: SqReldoAnchor, Leash: 6,
		Prefer: []string{"What do you know about the Imcando dwarves?"}}
	SqThurgoStop = NpcStop{Npc: "Thurgo", ID: NpcThurgo, Anchor: SqThurgoAnchor, Leash: 6,
		Prefer: []string{"Would you like a redberry pie?", "Would you like some redberry pie?", "About that sword...", "Can you make me a special sword?", "Hello. Are you an Imcando dwarf?"}}
)

// sqFoodIDs are the foods the quest eats, best first; Module.Sustain and the kit's food count read them.
var sqFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 1891, 1893, 1895, 2309}

// SqFoodIDs returns the foods the quest eats (a copy).
func SqFoodIDs() []int { return append([]int(nil), sqFoodIDs...) }

// SqEatAt is the hitpoints at or under which the quest eats between and beside its steps: two Ice warrior hits (6) and the three-tick
// delay of a bite fit well inside it, and it is a bite at 40 percent of the 60 hitpoints the quest asks for.
const SqEatAt = 24

// sqSustain is the quest's survival policy.
var sqSustain = Sustain{FoodIDs: sqFoodIDs, EatAt: SqEatAt}

// sqTools are the quest's own items: what the floor bite never eats (the pie and the berries have an Eat op) and every bank step of the
// quest leaves in the pack. sqKeep adds the food, which is no tool.
var sqTools = func() []int {
	tools := []int{ObjSqPortrait, ObjSqSword, ObjSqBluriteOre, ObjSqIronBar, ObjSqPieDish, ObjSqPieShell, ObjSqUncookedPie, ObjSqRedberryPie,
		ObjSqBurntPie, ObjSqPastryDough, ObjSqRedberries, ObjSqPotOfFlour, ObjSqEmptyPot, ObjSqBucket, ObjSqBucketWater, CoinsID}
	for _, p := range doricPickaxes {
		tools = append(tools, p.ID)
	}
	return tools
}()

var sqKeep = append(append([]int(nil), sqTools...), sqFoodIDs...)

// SqKeepIDs returns the keep list (a copy).
func SqKeepIDs() []int { return append([]int(nil), sqKeep...) }

// SqToolIDs returns the module's tools (a copy).
func SqToolIDs() []int { return append([]int(nil), sqTools...) }

// sqMarkers are the journal's markers for the part of the quest reached, newest first (squire_journal.rs2 keeps every earlier paragraph
// on the page, struck through, so the newest decides): written against NormaliseLines, which turns a colour tag into a space.
var sqMarkers = []struct {
	stage int
	text  string
}{
	{SqComplete, "quest complete!"},
	{SqLookingBlurite, "thurgo has now smithed me a replica"},
	{SqLookingBlurite, "according to thurgo to make a replica sword"},
	{SqLookingPortrait, "the squire told me about a"},
	{SqSpokenThurgo, "needs a picture of the sword before he can help"},
	{SqGivenPie, "gained his trust through"},
	{SqSpokenReldo, "reldo couldn't give me much information"},
	{SqStarted, "the squire suggests i speak to"},
	{SqNotStarted, "i can start this quest by talking to the"},
}

// ParseKnightsSwordJournal reads %squire from squire_journal.rs2's rendered text. ok is false for an empty or unrecognised page, which is
// unknown, never stage 0.
func ParseKnightsSwordJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, m := range sqMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// sqBank is the bank a visit uses: the one nearest the account on the surface, the Falador bank beside the Squire for an account that is
// not (underground the map's z means nothing).
func sqBank(s Snapshot) tile.Tile {
	if s.TileKnown && !IsUnderground(s.Tile) {
		return NearestBank(s.Tile).Tile
	}
	return NearestBank(SqSquireAnchor).Tile
}

func sqCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

func sqWait(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, ParkFor: d, Reason: reason}
}

func sqDeposit(s Snapshot, reason string) Step {
	return Step{Kind: StepDeposit, KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true, Reason: reason}
}

func sqWithdraw(s Snapshot, id int, name, reason string) Step {
	return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: name, Qty: 1, ID: id}}, Bank: sqBank(s), BankSet: true, Reason: reason}
}

// sqBanked reports an item the last bank read saw.
func sqBanked(s Snapshot, id int) bool {
	n, known := s.BankedID(id)
	return known && n > 0
}

// SquireLevelsShort says which of the skills the quest asks for is under its level, "" for none; Cooking is asked for only while the pie is
// still to be made (withCooking). The hitpoints and combat halves are the dungeon's (knightsdungeon.go); a stat the snapshot has not read is
// not judged.
func SquireLevelsShort(s Snapshot, withCooking bool) string {
	var short []string
	if lvl := s.Level("mining"); lvl > 0 && lvl < SqMining {
		short = append(short, fmt.Sprintf("Mining %d/%d (the blurite rock)", lvl, SqMining))
	}
	if lvl := s.Level("cooking"); withCooking && lvl > 0 && lvl < SqCooking {
		short = append(short, fmt.Sprintf("Cooking %d/%d (the redberry pie)", lvl, SqCooking))
	}
	if s.MaxHP > 0 && s.MaxHP < SqMinHitpoints {
		short = append(short, fmt.Sprintf("Hitpoints %d/%d (the Asgarnian Ice Dungeon)", s.MaxHP, SqMinHitpoints))
	}
	if c := DemonCombat(s); c > 0 && c < SqMinCombat {
		short = append(short, fmt.Sprintf("combat %d/%d (the dungeon's hobgoblins and pirates hunt anything under it)", c, SqMinCombat))
	}
	return strings.Join(short, ", ")
}

// sqStart is stage 0: the levels, then the Squire.
func sqStart(s Snapshot) Step {
	if why := SquireLevelsShort(s, true); why != "" {
		return sqWait(SqLevelParkFor, "not starting the Knight's Sword: it wants "+why)
	}
	return Step{Kind: StepTalk, Stop: SqSquireStop}
}

// ---------------------------------------------------------------------------------------------------------
// The pie.
// ---------------------------------------------------------------------------------------------------------

// sqPieKitLines is the raw ingredients the rest of the chain wants, from where the chain stands: a shell wants the berries, dough wants a
// dish and the berries, and a chain not begun wants a dish, water (a bucket of water or the bucket to fill), flour and berries. The flour
// and the berries are asked for in threes with one the least (the range burns about one pie in two), the shelf at Wydin's keeps three
// pots and one berry.
func sqPieKitLines(s Snapshot) []KitLine {
	var kit []KitLine
	berries := KitLine{ID: ObjSqRedberries, Name: "Redberries", Qty: 3, Min: 1, Sources: []Source{
		{Kind: SourceShop, Shop: WydinShop, Price: 1, Note: "Wydin's food store keeps one, restocking in a hundred ticks"}}}
	dish := KitLine{ID: ObjSqPieDish, Name: "Pie dish", Qty: 1, Sources: []Source{
		{Kind: SourceGround, Tile: tile.Tile{X: 3222, Z: 3494}, PerSpawn: 1, RespawnTicks: 100, Note: "the Varrock palace kitchen floor"}}}
	switch {
	case s.HeldID(ObjSqPieShell) > 0:
		kit = append(kit, berries)
	case s.HeldID(ObjSqPastryDough) > 0:
		if s.HeldID(ObjSqPieDish) == 0 {
			kit = append(kit, dish)
		}
		kit = append(kit, berries)
	default:
		if s.HeldID(ObjSqPieDish) == 0 {
			kit = append(kit, dish)
		}
		if s.HeldID(ObjSqBucketWater) == 0 {
			kit = append(kit, demonBucketKit()...)
		}
		kit = append(kit,
			KitLine{ID: ObjSqPotOfFlour, Name: "Pot of flour", Qty: 3, Min: 1, Sources: []Source{
				{Kind: SourceShop, Shop: WydinShop, Price: 3, Note: "Wydin's food store keeps three, restocking one a minute"}}},
			berries)
	}
	// The shops are paid from the pack: an account that holds under SqPieCoins asks the bank and the mule for them too (the line is optional,
	// so a poor bank never blocks the kit; the shops' own coin check does).
	if s.HeldID(CoinsID) < SqPieCoins && len(kit) > 0 {
		kit = append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: SqPieCoins, Stackable: true, Optional: true})
	}
	return kit
}

// sqBerryFallbackKit is the redberries from the ground: the three spawns beside the cadava patch south-east of Varrock, for an account
// whose bank, mule and Wydin's shelf had none.
func sqBerryFallbackKit() []KitLine {
	var src []Source
	for _, t := range RedberrySpawns {
		src = append(src, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "the redberry bushes beside the cadava patch"})
	}
	return []KitLine{{ID: ObjSqRedberries, Name: "Redberries", Qty: 2, Min: 1, Sources: src}}
}

// sqShort lists the required lines of a kit the pack is short of.
func sqShort(s Snapshot, kit []KitLine) []KitLine {
	var short []KitLine
	for _, l := range kit {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			short = append(short, l)
		}
	}
	return short
}

func sqNames(lines []KitLine) string {
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		names = append(names, strings.ToLower(l.Name))
	}
	return strings.Join(names, ", ")
}

// sqCookingShort is the pie's own level gate, in case the quest was begun by hand below it.
func sqCookingShort(s Snapshot) bool {
	lvl := s.Level("cooking")
	return lvl > 0 && lvl < SqCooking
}

// sqPie is the next rung of the redberry-pie chain at stage 2: read backwards from the pie, so a part-built chain resumes at the right
// rung.
func sqPie(s Snapshot) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if held(ObjSqBurntPie) {
		return sqRung(s, Step{Kind: StepCustom, Custom: CustomSquireEmpty, ItemID: ObjSqBurntPie, Reason: "the burnt pie emptied: the dish comes back"},
			"emptying the burnt pie")
	}
	if sqCookingShort(s) && (held(ObjSqUncookedPie) || held(ObjSqPieShell)) {
		return sqWait(SqLevelParkFor, fmt.Sprintf("the redberry pie wants Cooking %d (the account has %d)", SqCooking, s.Level("cooking")))
	}
	if held(ObjSqUncookedPie) {
		return sqRung(s, sqCustom(CustomSquireCook, "the uncooked pie on a range"), "the uncooked pie on a range")
	}
	// A pie in the bank is the shortest way to one; the bank is read before the chain is begun so that one is noticed.
	if !s.BankKnown && !sqChainBegun(s) {
		return Step{Kind: StepScanBank, Bank: sqBank(s), BankSet: true, Reason: "a redberry pie or its ingredients may be in the bank"}
	}
	if sqBanked(s, ObjSqRedberryPie) && !held(ObjSqRedberryPie) {
		return sqWithdraw(s, ObjSqRedberryPie, "Redberry pie", "a redberry pie is in the bank")
	}
	// The raw ingredients the rest of the chain wants, once; a round with every source dead parks the quest.
	kit := sqPieKitLines(s)
	if short := sqShort(s, kit); len(short) > 0 {
		if s.Blocked["provision:"+KitSquirePie] {
			if sqBerriesOnly(short) && !s.Ran["provision:"+KitSquireBerries] && !s.Failed["provision:"+KitSquireBerries] {
				return Step{Kind: StepProvision, KitName: KitSquireBerries, Kit: sqBerryFallbackKit(), KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true,
					Reason: "redberries from the bushes: the bank, the mule and Wydin's shelf had none"}
			}
			return sqWait(SqItemParkFor, "no "+sqNames(short)+" from the bank, the mule, the world or Wydin's shelf for the redberry pie")
		}
		if s.FreeSlots < SqPieSlots {
			return sqDeposit(s, "room for the pie's ingredients")
		}
		return Step{Kind: StepProvision, KitName: KitSquirePie, Kit: kit, KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true,
			Reason: "the redberry pie's ingredients (" + sqNames(short) + ")"}
	}
	switch {
	case held(ObjSqPieShell):
		return sqRung(s, Step{Kind: StepCustom, Custom: CustomSquireBerries, ItemID: ObjSqRedberries, TargetID: ObjSqPieShell, ExpectItemID: ObjSqUncookedPie,
			Reason: "the redberries into the pie shell"}, "the redberries into the pie shell")
	case held(ObjSqPastryDough):
		return sqRung(s, Step{Kind: StepCustom, Custom: CustomSquireShell, ItemID: ObjSqPastryDough, TargetID: ObjSqPieDish, ExpectItemID: ObjSqPieShell,
			Reason: "the pastry dough into the pie dish"}, "the pastry dough into the pie dish")
	}
	if !held(ObjSqBucketWater) {
		return sqRung(s, sqCustom(CustomSquireFill, "the bucket filled at the nearest sink, well or fountain"), "filling the bucket at a sink, a well or a fountain")
	}
	return sqRung(s, sqCustom(CustomSquireDough, "the flour on the bucket of water: pastry dough"), "the flour and the water into pastry dough")
}

// sqRung is a custom step of the chain, or the quest parked with the reason when that step failed on its last try: a rung is never retried in
// a loop (the engine forgets the failure when the park ends, and the next visit begins it afresh).
func sqRung(s Snapshot, step Step, what string) Step {
	if s.Failed[step.Custom] {
		return sqWait(SqRetryParkFor, what+" failed on its last try")
	}
	return step
}

// sqChainBegun reports that something of the pie's chain is in the pack already.
func sqChainBegun(s Snapshot) bool {
	for _, id := range []int{ObjSqPieDish, ObjSqPieShell, ObjSqPastryDough, ObjSqBucketWater, ObjSqBucket, ObjSqPotOfFlour, ObjSqRedberries, ObjSqUncookedPie} {
		if s.HeldID(id) > 0 {
			return true
		}
	}
	return false
}

// SqPieCoins is the coins the pie's shopping wants in the pack: a bucket's two, three pots of flour at three and three berries at one, and a margin.
const SqPieCoins = 60

// SqPieSlots is the pack room the pie's raw ingredients want before a round: a dish, a bucket, a pot of flour and a berry at the least.
const SqPieSlots = 4

// sqBerriesOnly reports that the redberries are all that is short.
func sqBerriesOnly(short []KitLine) bool {
	return len(short) == 1 && short[0].ID == ObjSqRedberries
}

// ---------------------------------------------------------------------------------------------------------
// The portrait.
// ---------------------------------------------------------------------------------------------------------

// sqPortrait is stage 5: the portrait in the pack goes to Thurgo, one in the bank is fetched (the cupboard gives none to a player whose
// bank holds one), and otherwise Sir Vyvin's cupboard is searched.
func sqPortrait(s Snapshot) Step {
	if s.HeldID(ObjSqPortrait) > 0 {
		return Step{Kind: StepTalk, Stop: SqThurgoStop}
	}
	if sqBanked(s, ObjSqPortrait) {
		return sqWithdraw(s, ObjSqPortrait, "Portrait", "the portrait is in the bank, and the cupboard gives none while it is")
	}
	if s.Failed[CustomSquirePortrait] {
		return sqWait(SqRetryParkFor, "Sir Vyvin's cupboard gave no portrait on its last try (Sir Vyvin kept near it)")
	}
	return sqCustom(CustomSquirePortrait, "Sir Vyvin's cupboard, while Sir Vyvin is not beside it")
}

// SqVyvinTooClose reports Sir Vyvin within one tile of the account: [proc,vyvin_distracted] is npc_find(coord, sir_vyvin, 1, 0) on the
// player's own coordinate, so only proximity counts.
func SqVyvinTooClose(here, vyvin tile.Tile) bool {
	return here.Level == vyvin.Level && tile.Chebyshev(here, vyvin) <= 1
}

// SqBestApproach is the cupboard's approach tile farthest from Sir Vyvin (the first when his tile is unknown): with him to one side only
// the far tile is clear, and with him south of both neither is, and only time helps.
func SqBestApproach(vyvin tile.Tile, known bool) tile.Tile {
	if !known {
		return SqVyvinApproaches[0]
	}
	best := SqVyvinApproaches[0]
	for _, a := range SqVyvinApproaches[1:] {
		if tile.Chebyshev(a, vyvin) > tile.Chebyshev(best, vyvin) {
			best = a
		}
	}
	return best
}

// SqMaxSkips is how many passes the search is put off for Sir Vyvin's position alone before it is tried regardless (the position is read a
// tick before the click and the engine checks again after the walk), and SqRefusalsBeforeRetreat how many refused searches in a row make the
// account step away from the cupboard to give him room to wander.
const (
	SqMaxSkips              = 4
	SqRefusalsBeforeRetreat = 3
	SqPortraitAttempts      = 60
)

// SqShouldWaitOut reports a pass to put off: Sir Vyvin is too close and fewer than SqMaxSkips passes have been.
func SqShouldWaitOut(skips int, here, vyvin tile.Tile, known bool) bool {
	return skips < SqMaxSkips && known && SqVyvinTooClose(here, vyvin)
}

// ---------------------------------------------------------------------------------------------------------
// The materials.
// ---------------------------------------------------------------------------------------------------------

// sqOreKitLines is the kit the dungeon wants: two iron bars (the bank and the mule; the module never smelts), a pickaxe the Mining level can
// use, and the food the trip is priced at (sharks; the second kind is its own round).
func sqOreKitLines(s Snapshot, trip SqTrip, sharks bool) []KitLine {
	var kit []KitLine
	if sharks {
		if s.HeldID(ObjSqIronBar) < 2 {
			kit = append(kit, KitLine{ID: ObjSqIronBar, Name: "Iron bar", Qty: 2})
		}
		if s.HeldID(ObjSqBluriteOre) == 0 && DoricUsablePickaxe(s) == 0 {
			kit = append(kit, doricPickKit(s)...)
		}
	}
	if s.HeldID(ObjSqBluriteOre) == 0 {
		need := max(trip.FoodWanted-FoodPoolHeal(s.InvIDs, sqFoodIDs), 0)
		if need > 0 {
			if sharks {
				kit = append(kit, KitLine{ID: ObjShark, Name: "Shark", Qty: min(ceilDiv(need, 20), SqMaxFoodItems), Optional: true})
			} else {
				kit = append(kit, KitLine{ID: ObjBass, Name: "Bass", Qty: min(ceilDiv(need, 13), SqMaxFoodItems), Optional: true})
			}
		}
	}
	return kit
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// sqMaterials is stage 6: the sword where it is, the bars, then the ore.
func sqMaterials(s Snapshot) Step {
	if s.WornIDs[ObjSqSword] > 0 {
		return sqRung(s, sqCustom(CustomSquireUnwield, "the sword taken off: the Squire wants it in the pack"), "taking the sword off")
	}
	if s.HeldID(ObjSqSword) > 0 {
		return Step{Kind: StepTalk, Stop: SqSquireStop}
	}
	if sqBanked(s, ObjSqSword) {
		return sqWithdraw(s, ObjSqSword, "Blurite sword", "the sword is in the bank")
	}
	if s.HeldID(ObjSqBluriteOre) > 0 && s.HeldID(ObjSqIronBar) >= 2 {
		return Step{Kind: StepTalk, Stop: SqThurgoStop}
	}
	inDungeon := s.TileKnown && InSquireDungeon(s.Tile)
	if inDungeon && s.HeldID(ObjSqBluriteOre) == 0 {
		// The visit is resumed (or ended) by its own step, which takes its own measure of hitpoints and food.
		if s.Failed[CustomSquireBlurite] {
			return Step{Kind: StepWalk, Anchor: SqLadderTop, Radius: 2, Reason: "out of the Asgarnian Ice Dungeon"}
		}
		return sqCustom(CustomSquireBlurite, "the ice dungeon: one blurite ore, then out")
	}
	if inDungeon {
		return Step{Kind: StepWalk, Anchor: SqLadderTop, Radius: 2, Reason: "out of the Asgarnian Ice Dungeon with the ore"}
	}
	if s.HeldID(ObjSqBluriteOre) > 0 && s.HeldID(ObjSqIronBar) < 2 {
		return sqBars(s)
	}
	if why := SquireLevelsShort(s, false); why != "" {
		return sqWait(SqLevelParkFor, "not going down to the Asgarnian Ice Dungeon: it wants "+why)
	}
	trip := SquireTripPrice(s)
	if !trip.OK {
		return sqWait(SqLevelParkFor, trip.Why)
	}
	if st, ok := sqOreKitStep(s, trip); ok {
		return st
	}
	if s.FreeSlots < 1 {
		return sqDeposit(s, "room for the ore")
	}
	if s.Failed[CustomSquireBlurite] {
		return sqWait(SqTripRetryFor, "the last visit to the ice dungeon came out with no ore")
	}
	return sqCustom(CustomSquireBlurite, "the ice dungeon: one blurite ore, then out")
}

// sqBars is the bars on their own, when the ore is already in hand.
func sqBars(s Snapshot) Step {
	if s.Blocked["provision:"+KitSquireOre] || s.Failed["provision:"+KitSquireOre] {
		return sqWait(SqItemParkFor, "no iron bars from the bank or the mule for Thurgo's sword (the quest does not smelt them)")
	}
	return Step{Kind: StepProvision, KitName: KitSquireOre, Kit: []KitLine{{ID: ObjSqIronBar, Name: "Iron bar", Qty: 2}}, KeepIDs: sqKeep,
		Bank: sqBank(s), BankSet: true, Reason: "two iron bars for Thurgo's sword"}
}

// sqOreKitStep is the provisioning round the dungeon still wants, if any: the bars, the pickaxe and the sharks first, the second kind of
// food when the sharks did not cover the trip; a round with every source dead, or a food pool still short after both, parks the quest.
func sqOreKitStep(s Snapshot, trip SqTrip) (Step, bool) {
	needBars := s.HeldID(ObjSqIronBar) < 2
	needPick := DoricUsablePickaxe(s) == 0
	pool := FoodPoolHeal(s.InvIDs, sqFoodIDs)
	if !needBars && !needPick && pool >= trip.FoodWanted {
		return Step{}, false
	}
	kit := sqOreKitLines(s, trip, true)
	if short := sqShort(s, kit); len(short) > 0 {
		if s.Blocked["provision:"+KitSquireOre] || s.Failed["provision:"+KitSquireOre] {
			return sqWait(SqItemParkFor, "no "+sqNames(short)+" from the bank, the mule or the shops (iron bars are never smelted by this quest)"), true
		}
		return Step{Kind: StepProvision, KitName: KitSquireOre, Kit: kit, KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true,
			Reason: "the dungeon's kit (" + sqNames(short) + ")"}, true
	}
	if pool < trip.FoodWanted {
		if !s.Ran["provision:"+KitSquireOre] && !s.Failed["provision:"+KitSquireOre] && len(kit) > 0 {
			return Step{Kind: StepProvision, KitName: KitSquireOre, Kit: kit, KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true,
				Reason: fmt.Sprintf("sharks for the ice dungeon (%d hitpoints of food wanted, %d held)", trip.FoodWanted, pool)}, true
		}
		if !s.Ran["provision:"+KitSquireFood2] && !s.Failed["provision:"+KitSquireFood2] {
			if second := sqOreKitLines(s, trip, false); len(second) > 0 {
				return Step{Kind: StepProvision, KitName: KitSquireFood2, Kit: second, KeepIDs: sqKeep, Bank: sqBank(s), BankSet: true,
					Reason: fmt.Sprintf("bass for the ice dungeon (%d hitpoints of food wanted, %d held)", trip.FoodWanted, pool)}, true
			}
		}
		return sqWait(SqItemParkFor, fmt.Sprintf("not going down to the Asgarnian Ice Dungeon with %d hitpoints of food: the trip is priced at %d hitpoints taken and wants twice that in the pack",
			pool, int(trip.Damage+0.5))), true
	}
	return Step{}, false
}

// ---------------------------------------------------------------------------------------------------------
// The quest.
// ---------------------------------------------------------------------------------------------------------

// DecideKnightsSword is the whole quest, read from the quest tab, the journal's stage, the account's tile and the pack.
func DecideKnightsSword(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return sqStart(s)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case SqNotStarted:
		return sqStart(s)
	case SqStarted:
		return Step{Kind: StepTalk, Stop: SqReldoStop}
	case SqSpokenReldo:
		if s.HeldID(ObjSqRedberryPie) > 0 {
			return Step{Kind: StepTalk, Stop: SqThurgoStop}
		}
		return sqPie(s)
	case SqGivenPie:
		return Step{Kind: StepTalk, Stop: SqThurgoStop}
	case SqSpokenThurgo:
		return Step{Kind: StepTalk, Stop: SqSquireStop}
	case SqLookingPortrait:
		return sqPortrait(s)
	case SqLookingBlurite:
		return sqMaterials(s)
	case SqComplete:
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Knight's Sword stage " + strconv.Itoa(s.Stage)}
}

// KnightsSwordRecord is the quest's identity. The levels are the content's (Mining 10 for the rock, Cooking 10 for the pie) and the
// module's (60 hitpoints for the dungeon) and the Record states them so the ladder does not start an account that has none of them: no
// rung trains Mining or Cooking for one filler, and an advised ladder gate (the row's) inserts nothing.
var KnightsSwordRecord = Record{ID: KnightsSwordID, Name: KnightsSwordName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "mining", Level: SqMining}, {Skill: "cooking", Level: SqCooking}, {Skill: "hitpoints", Level: SqMinHitpoints}}}}

// KnightsSword is the engine module. It never fights (NoRetaliate: the dungeon's Ice warriors are walked through, priced and eaten
// through), and one death anywhere parks it for a day, a death in the dungeon for three (StepDeathCaps).
var KnightsSword = Module{
	Record: KnightsSwordRecord, Bank: NearestBank(SqSquireAnchor).Tile, BankSet: true, Anchor: SqSquireAnchor,
	ToolIDs:     sqTools,
	ReadStage:   ParseKnightsSwordJournal,
	Sustain:     sqSustain,
	NoRetaliate: true,
	MaxDeaths:   1,
	StepDeathCaps: map[string]StepDeathCap{
		CustomSquireBlurite: {Max: 1, Window: 24 * time.Hour, Skip: 72 * time.Hour},
	},
	Decide: DecideKnightsSword,
}
