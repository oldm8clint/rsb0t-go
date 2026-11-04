package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Jungle Potion (content id `junglepotion`, %junglepotion, perm and not transmitted), ported from the TS def
// (defs/junglepotion.ts) and checked against content 289: quests/quest_junglepotion/{configs,scripts}, areas/area_karamja/scripts/
// trufitus.rs2, skill_herblore/configs/herbs.obj and the maps (m43_46 to m46_48, m44_148 and m44_149). One quest point and 775
// Herblore experience (stat_advance 7750 at 10x). It needs Druidic Ritual complete (the start dialogue refuses otherwise:
// trufitus.rs2 start_junglepotion) and Herblore 3 (every herb is identified at level 3, herbs.obj identified_herb_level; Druidic
// Ritual pays 250 experience, which is level 3).
//
// The story, with the stage each step moves %junglepotion to. Trufitus (the village hut in Tai Bwo Wannai, Karamja) asks for five
// herbs in turn, each picked fresh from its own loc, cleaned (Identify) and handed over; every hand-in tells where the next is:
//
//	0   "It's a nice village, where is everyone?", "Me? How can I help?", "It sounds like just the challenge for me.": 1.
//	1,3,5,7,9   get_X: the herb is to be picked. A pick is searched from the loc (op2 Search) and ADDS the unidentified herb; the stage
//	            moves to found_X (the next even number) only when the pick is made at get_X: junglepotion_pick_herb checks
//	            "if (%junglepotion = ^junglepotion_get_X)". A herb picked before its stage, or a clean one bought, is refused
//	            by Trufitus ("That's not fresh ...", trufitus_accept_herb at a get_ stage).
//	2,4,6,8,10  found_X: "Of course!" gives the CLEAN herb (the unidentified one is refused: "so dirty that I can't even tell
//	            whether it is fresh") and moves to the next get_.
//	11  found_all_herbs: Trufitus shows the techniques (a message box) and the quest completes in a queue: 775 Herblore.
//
// What the content does that a script must plan for:
//   - The journal reads the pack. Each herb's page has a "pick" line, a "give" line, or (only for the first herb, with the
//     unidentified one in the pack) nothing; a found_X with the unidentified herb in the pack reads as the get_X page (junglepotion_
//     journal.rs2). So the stage is read as the herb Trufitus wants and whether the journal says pick or give, and the pack decides
//     the rest: an unidentified herb of the current one is cleaned first, a clean one is handed over when the journal says give.
//   - Four of the five locs are shared: a pick changes the loc to its empty form for 100 ticks for everyone (loc_change(..., 100)):
//     the vine, the palm, the scorched earth and the rock. The wall in the cave (rogues purse) is not changed. A pick that finds the
//     empty form waits for the full one.
//   - The cave is a pocket: the Rocks (loc 2584 at 2824,3118) raise a message box, a menu and another message box and set the
//     account down at 2830,9520; the Hand holds (loc 2585 at 2830,9522) climb back out to 2823,3120. Neither is a graph edge.
//   - Karamja has no bank and the ships cost: the account goes with coins (the ships are 30 each way) and the food it needs.
//
// The jungle's hostiles, from the maps (docs: the exploration is in TestJungleHostilesAreTheMapsOwn):
//   - Jungle spiders (npc 62, level 44, 50 hitpoints, a maximum hit of 5, hunt range 1, wander 3) and tribesmen (npc 191, level 32,
//     39 hitpoints, a hit of 4 and poison, hunt range 1, default wander 5) stand about the village and across the swamp the vine
//     lies in. They hunt only an account that comes within a tile of them, and the herbs lie outside the ground they wander over,
//     so every walk on the island keeps out of the rectangles round their spawns (JungleAvoid): the planner finds a route to
//     every stand and to Trufitus round all of them (checked on the real pack).
//   - The cave under the Rocks holds twelve jogres (npc 113, level 53, 60 hitpoints, a maximum hit of 7, an attack every six
//     ticks, hunt range 2): the way from the Hand holds to the rogues purse passes within two tiles of four of their spawns and within
//     four of six, and there is no way round. It is priced (JungleCaveOdds) on the hitpoints and the food the account goes with,
//     before the account leaves the mainland, and run through, not fought.

// JunglePotionName is the quest tab's spelling (component 7365, `questlist:junglepotion`).
const JunglePotionName = "Jungle Potion"

// JunglePotionID is the engine's id.
const JunglePotionID = "junglepotion"

// The %junglepotion stages (quest_junglepotion.constant, general/configs/quest.constant). Complete is 12; 13 is the same page.
const (
	JungleNotStarted     = 0
	JungleFoundAllHerbs  = 11
	JungleComplete       = 12
	jungleHerbCount      = 5
	jungleStageFirstHerb = 1
)

// Ids from content/pack (checked against the production config by TestDruidJungleIdsMatchTheProductionConfig).
const (
	NpcTrufitus     = 740 // [trufitus], the hut in Tai Bwo Wannai
	NpcJogre        = 113 // [jogre], the cave's beasts
	NpcJungleSpider = 62  // [jungle_spider]
	NpcTribesman    = 191 // [tribesman]

	LocJunglePothole   = 2584 // [pothole_cave_entrance], "Rocks", op2 Search
	LocJungleHandHolds = 2585 // [jp_caverocksout], "Hand holds", op1 Climb
)

// JungleHerb is one of the five herbs, in the order Trufitus asks for them.
type JungleHerb struct {
	// Name is the herb as the quest's pages and Trufitus spell it ("Snake weed" is "Snakeweed" in the journal's pick line).
	Name string
	// Unid and Clean are the object ids of the unidentified and the identified herb.
	Unid, Clean int
	// Loc is the full loc's id, At its tile and Stand the tile a walk aims at to search it.
	Loc   int
	At    tile.Tile
	Stand tile.Tile
	// Underground says the loc is in the cave under the Rocks.
	Underground bool
	// Shared says the loc changes to its empty form for a hundred ticks after any pick (everything but the cave's wall).
	Shared bool
	// pick and give are the journal needles of the herb's two lines (lower case, after NormaliseLines).
	pick, give string
}

// JungleHerbs are the five herbs, snake weed to rogues purse.
var JungleHerbs = [jungleHerbCount]JungleHerb{
	{Name: "Snake weed", Unid: 1525, Clean: 1526, Loc: 2575, At: tile.Tile{X: 2760, Z: 3014}, Stand: tile.Tile{X: 2759, Z: 3014}, Shared: true,
		pick: "i need to pick some fresh snakeweed", give: "i need to give the snake weed"},
	{Name: "Ardrigal", Unid: 1527, Clean: 1528, Loc: 2577, At: tile.Tile{X: 2870, Z: 3115}, Stand: tile.Tile{X: 2869, Z: 3115}, Shared: true,
		pick: "i need to pick some fresh ardrigal", give: "i need to give the ardrigal"},
	{Name: "Sito foil", Unid: 1529, Clean: 1530, Loc: 2579, At: tile.Tile{X: 2784, Z: 3067}, Stand: tile.Tile{X: 2785, Z: 3067}, Shared: true,
		pick: "i need to pick some fresh sito foil", give: "i need to give the sito foil"},
	{Name: "Volencia moss", Unid: 1531, Clean: 1532, Loc: 2581, At: tile.Tile{X: 2850, Z: 3035}, Stand: tile.Tile{X: 2851, Z: 3035}, Shared: true,
		pick: "i need to pick some fresh volencia moss", give: "i need to give the volencia moss"},
	{Name: "Rogues purse", Unid: 1533, Clean: 1534, Loc: 2583, At: tile.Tile{X: 2850, Z: 9476}, Stand: tile.Tile{X: 2850, Z: 9476}, Underground: true,
		pick: "i need to pick some fresh rogues purse", give: "i need to give the rogue purse"},
}

// Tiles from content maps and the loc index.
var (
	// TrufitusAnchor is Trufitus's spawn (m43_48, in the village's main hut).
	TrufitusAnchor = tile.Tile{X: 2809, Z: 3086}
	// JunglePotholeStand is a tile the Rocks can be searched from; JungleCaveArrival is where the Rocks set the account down
	// (p_telejump 0_44_148_14_48), JungleHandHoldsStand the only tile the Hand holds are climbed from, and JungleCaveSurface where
	// they set it down (0_44_48_7_48).
	JunglePotholeStand   = tile.Tile{X: 2823, Z: 3118}
	JungleCaveArrival    = tile.Tile{X: 2830, Z: 9520}
	JungleHandHoldsStand = tile.Tile{X: 2830, Z: 9522}
	JungleCaveSurface    = tile.Tile{X: 2823, Z: 3120}
)

// InJungleCave reports a tile in the cave under the Rocks: the 1,276 tiles the planner reaches on foot from where the Rocks set the
// account down (x 2822-2876, z 9476-9532) with a margin, no part of which the surface graph reaches. Its neighbours underground are
// out of it: the Brimhaven dungeon's secret wall is at z 9599 and the lesser demons' rooms at z 9552.
func InJungleCave(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2815 && t.X <= 2880 && t.Z >= 9470 && t.Z <= 9540
}

// JungleHostile is one spawn the walks keep out of: an npc id and its tile.
type JungleHostile struct {
	NPC  int
	Tile tile.Tile
}

// jungleSpiders and jungleTribesmen are the jungle spiders' and tribesmen's spawns (maps m43_46 to m45_49) on the ground the
// quest walks: west of x 2900 and north of z 2975, the swamp the vine lies in to the Shilo road. The east side of the island (the
// jogres of the Shilo road and the plantation) is not on any leg.
var jungleSpiders = []tile.Tile{
	{X: 2777, Z: 3116}, {X: 2780, Z: 3029}, {X: 2780, Z: 3031}, {X: 2796, Z: 3104}, {X: 2796, Z: 3116}, {X: 2797, Z: 3112}, {X: 2798, Z: 3107},
	{X: 2799, Z: 3115}, {X: 2810, Z: 2981}, {X: 2810, Z: 2987}, {X: 2812, Z: 2999}, {X: 2814, Z: 2996}, {X: 2844, Z: 3061}, {X: 2850, Z: 3063},
	{X: 2853, Z: 3054}, {X: 2855, Z: 3085}, {X: 2855, Z: 3089}, {X: 2857, Z: 3085}, {X: 2858, Z: 3083}, {X: 2858, Z: 3092}, {X: 2861, Z: 3082},
	{X: 2861, Z: 3088}, {X: 2862, Z: 3081}, {X: 2864, Z: 3084}, {X: 2864, Z: 3088}, {X: 2866, Z: 3081}, {X: 2867, Z: 3086}, {X: 2868, Z: 3084},
	{X: 2883, Z: 2996}, {X: 2884, Z: 3005}, {X: 2887, Z: 3006}, {X: 2888, Z: 2998}, {X: 2890, Z: 3007}, {X: 2891, Z: 2994}, {X: 2892, Z: 2997},
	{X: 2892, Z: 3000}, {X: 2894, Z: 3006}, {X: 2896, Z: 3000},
}

var jungleTribesmen = []tile.Tile{
	{X: 2771, Z: 3014}, {X: 2773, Z: 3017}, {X: 2777, Z: 3068}, {X: 2801, Z: 3067}, {X: 2842, Z: 3017}, {X: 2866, Z: 3061}, {X: 2868, Z: 3054},
}

// The ground a hunter covers: its wander range, its hunt range and a tile of slack. Jungle spiders wander 3 (content wanderrange)
// and tribesmen 5 (the engine's default, NpcType.wanderrange), both hunt within 1.
const (
	JungleSpiderPad    = 3 + 1 + 1
	JungleTribesmanPad = 5 + 1 + 1
)

// JungleHostiles lists the spawns JungleAvoid fences.
func JungleHostiles() []JungleHostile {
	out := make([]JungleHostile, 0, len(jungleSpiders)+len(jungleTribesmen))
	for _, t := range jungleSpiders {
		out = append(out, JungleHostile{NPC: NpcJungleSpider, Tile: t})
	}
	for _, t := range jungleTribesmen {
		out = append(out, JungleHostile{NPC: NpcTribesman, Tile: t})
	}
	return out
}

// JungleAvoid is the rectangles every walk on the island keeps out of: a square round each spider's and tribesman's spawn as wide
// as the ground it wanders over and hunts across.
func JungleAvoid() []Rect {
	var out []Rect
	for _, h := range JungleHostiles() {
		pad := JungleSpiderPad
		if h.NPC == NpcTribesman {
			pad = JungleTribesmanPad
		}
		out = append(out, Rect{MinX: h.Tile.X - pad, MaxX: h.Tile.X + pad, MinZ: h.Tile.Z - pad, MaxZ: h.Tile.Z + pad, Level: h.Tile.Level})
	}
	return out
}

// FoeJogre is [jogre] (npc 113, scripts/_unpack/225/all.npc): attack, strength and defence 43, 60 hitpoints, attackrate 6, attack
// bonus 22, strength bonus 20 (a maximum hit of 7), no defence params. Huntmode cowardly with hunt range 2 and size 2.
var FoeJogre = MeleeFoe{Name: "Jogre", Attack: 43, Strength: 43, Defence: 43, Magic: 1, Hitpoints: 60, AttackTicks: 6, AttackBonus: 22, StrengthBonus: 20}

// The cave's price. The planned way from the Hand holds to the rogues purse passes within two tiles of four jogre spawns and
// within four of six (TestJungleCaveRouteMeetsTheJogres); a jogre that notices the account swings once as it passes in, once
// as the account stops at the wall and once as it passes out, which is three swings for each of four jogres.
const (
	JungleCaveJogres = 4
	JungleCaveSwings = 3
)

// The quest's gates and supplies.
const (
	// JungleMinHitpoints is the module's own floor.
	JungleMinHitpoints = 35
	// JungleCoinsQty is what the supplies pass draws, JungleCoinsMin the least the account sets out with: the ships are 30 each way,
	// and a fare is all the island's walks to the mainland ask.
	JungleCoinsQty = 250
	JungleCoinsMin = 120
	// JungleFreeSlots is the pack room the trip wants before it starts: the herbs, and the food the cave eats from.
	JungleFreeSlots = 5
	// JungleOddsPark, JungleSupplyPark and JungleLegPark are the parks of a refused price, of no coins for the ships and of a leg
	// that would not walk.
	JungleOddsPark   = 3 * time.Hour
	JungleSupplyPark = time.Hour
	JungleLegPark    = 30 * time.Minute
)

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomJungleStart  = "jungle-start"
	CustomJunglePick   = "jungle-pick"
	CustomJungleHandIn = "jungle-handin"
	CustomJungleFinish = "jungle-finish"

	KitJungleSupplies = "jungle-supplies"
)

// jungleFoodIDs are the foods the quest carries and eats, best first.
var jungleFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// jungleSustain: the cave's jogres hit up to 7, so the line is high enough to eat on the way out of a swing.
var jungleSustain = Sustain{FoodIDs: jungleFoodIDs, EatAt: 22}

// jungleTools are the quest's own items: the coins, and the herbs in both forms.
var jungleTools = func() []int {
	out := []int{CoinsID}
	for _, h := range JungleHerbs {
		out = append(out, h.Unid, h.Clean)
	}
	return out
}()

// jungleKeep is a bank visit's keep list: the tools and the food.
func jungleKeep() []int { return append(append([]int(nil), jungleTools...), jungleFoodIDs...) }

// JungleKeepIDs returns the keep list (a copy).
func JungleKeepIDs() []int { return jungleKeep() }

// The stops.
var (
	// TrufitusStartStop starts the quest: three menus ("What does Bwana mean?" / "Tai Bwo Wannai? What does that mean?" / "It's a
	// nice village, where is everyone?", then "Me? How can I help?" / "I am sorry, but I am very busy.", then "Hmmm, sounds
	// difficult, ..." / "It sounds like just the challenge for me.").
	TrufitusStartStop = NpcStop{Npc: "Trufitus", ID: NpcTrufitus, Anchor: TrufitusAnchor, Leash: 6,
		Prefer: []string{"It's a nice village, where is everyone?", "Me? How can I help?", "It sounds like just the challenge for me."}}
	// TrufitusHandInStop gives a herb: "Of course!" / "Not yet, sorry, what's the clue again?". The finishing talk (stage 11) has
	// no menu.
	TrufitusHandInStop = NpcStop{Npc: "Trufitus", ID: NpcTrufitus, Anchor: TrufitusAnchor, Leash: 6,
		Prefer: []string{"Of course!"}}
)

// ParseJunglePotionJournal reads the stage from junglepotion_journal.rs2's rendered text: 0 not started, 11 for all herbs in, 12
// complete, and for a herb index h in 0 to 4 either 2h+1 (the page says to pick it) or 2h+2 (it says to give it, or, for the first
// herb, says nothing: the unidentified one is in the pack). The pack settles get_X from found_X where the pages cannot (see the
// file's comment); ok is false for an empty or unrecognised journal, which is unknown, never stage 0.
func ParseJunglePotionJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	switch {
	case text == "":
		return 0, false
	case strings.Contains(text, "quest complete"):
		return JungleComplete, true
	case strings.Contains(text, "i can start this quest"):
		return JungleNotStarted, true
	case strings.Contains(text, "trufitus needs to commune with the gods"):
		return JungleFoundAllHerbs, true
	}
	for h, herb := range JungleHerbs {
		if strings.Contains(text, herb.give) || strings.Contains(text, strings.Replace(herb.pick, "need to pick", "picked", 1)) {
			return 2*h + 2, true
		}
		if strings.Contains(text, herb.pick) {
			return 2*h + 1, true
		}
	}
	// The first herb's page with the unidentified one in the pack has the header and nothing else.
	if strings.Contains(text, "he's asked me to help him by collecting herbs") && !strings.Contains(text, "i've given") {
		return 2, true
	}
	return 0, false
}

// JungleHerbOf is the herb a stage of the journal reading is about, and whether the page says to give it. ok is false outside the
// five herbs' stages.
func JungleHerbOf(stage int) (herb JungleHerb, index int, give, ok bool) {
	if stage < jungleStageFirstHerb || stage >= JungleFoundAllHerbs {
		return JungleHerb{}, 0, false, false
	}
	index = (stage - jungleStageFirstHerb) / 2
	return JungleHerbs[index], index, stage%2 == 0, true
}

// jungleAway reports an account that is on the mainland (or wherever it is not on Karamja): its next Karamja errand starts with
// the supplies. The cave is Karamja too.
func jungleAway(s Snapshot) bool {
	return s.TileKnown && !OnKaramja(s.Tile) && !InJungleCave(s.Tile)
}

// jungleSuppliesKit is the coins for the ships and the food: the coins a flexible line (an account holding the floor is topped up
// only from a bank that is open anyway), the sharks an optional line the odds judge (one kind only: see druidFoodLines).
func jungleSuppliesKit() []KitLine {
	return []KitLine{
		{ID: CoinsID, Name: "Coins", Qty: JungleCoinsQty, Min: JungleCoinsMin, Stackable: true},
		{ID: ObjShark, Name: "Shark", Qty: 6, Optional: true},
	}
}

// JungleCaveOdds prices the run through the cave on what the account goes with: four jogres, three swings each, at the rate a
// jogre hits an account with this Defence (FoePerAttack, with the same quarter on top the quest fights use). The account's
// capacity is its hitpoints and half its food (a bite costs the ticks the account is running for), and what the run is expected to
// take must leave three of the jogre's hardest hits. Nothing is judged for an account whose hitpoints are unread.
func JungleCaveOdds(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	f := FoeJogre
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(JungleCaveJogres*JungleCaveSwings) * per
	hp := s.HP
	if hp <= 0 || hp > s.MaxHP {
		hp = s.MaxHP
	}
	pool := FoodPoolHeal(s.InvIDs, jungleFoodIDs)
	capacity := float64(hp) + float64(pool)/2
	margin := float64(3 * f.PeakHit())
	if capacity-dmg < margin {
		return false, fmt.Sprintf("the cave's jogres (level 53, hits up to %d) cost an account at Defence %d about %.0f hitpoints on the run to the rogues purse and back; it goes in with %d of %d and %d in food (half of it counted), and the run must leave %.0f",
			f.PeakHit(), s.Level("defence"), dmg, hp, s.MaxHP, pool, margin)
	}
	return true, ""
}

// jungleDepart is the pass before the account leaves for the island: room, the supplies (coins for the ships, food), and the
// price of the cave. It reports false when the account is ready, or is on the island already (there is no bank on it).
func jungleDepart(s Snapshot) (Step, bool) {
	if !jungleAway(s) {
		return Step{}, false
	}
	bank := cookSheepBank(s)
	if s.FreeSlots < JungleFreeSlots {
		return Step{Kind: StepDeposit, KeepIDs: jungleKeep(), Bank: bank, BankSet: true, Reason: "room for the herbs and the food"}, true
	}
	supplied := s.HeldID(CoinsID) >= JungleCoinsMin && FoodPoolHeal(s.InvIDs, jungleFoodIDs) > 0
	if !supplied && !s.Ran["provision:"+KitJungleSupplies] && !s.Failed["provision:"+KitJungleSupplies] {
		return Step{Kind: StepProvision, KitName: KitJungleSupplies, Kit: jungleSuppliesKit(), KeepIDs: jungleKeep(), Bank: bank, BankSet: true,
			Reason: "coins for the ships and food for the cave"}, true
	}
	if s.HeldID(CoinsID) < JungleCoinsMin {
		return Step{Kind: StepWait, ParkFor: JungleSupplyPark,
			Reason: fmt.Sprintf("no %d coins for the ships from the bank or the mule (the pack holds %d): Karamja has no bank", JungleCoinsMin, s.HeldID(CoinsID))}, true
	}
	if ok, why := JungleCaveOdds(s); !ok {
		return Step{Kind: StepWait, ParkFor: JungleOddsPark, Reason: "not going to Karamja: " + why}, true
	}
	return Step{}, false
}

// jungleErrand is a custom step on the island, after the supplies pass.
func jungleErrand(s Snapshot, custom string, qty int, reason string) Step {
	if st, ok := jungleDepart(s); ok {
		return st
	}
	if s.Failed[custom] {
		return Step{Kind: StepWait, ParkFor: JungleLegPark, Reason: fmt.Sprintf("the %s errand could not be done (a walk round the spiders and tribesmen, or a talk that would not take): %s", custom, reason)}
	}
	return Step{Kind: StepCustom, Custom: custom, Qty: qty, Reason: reason}
}

// DecideJunglePotion is the whole quest, read from the journal stage and the pack.
func DecideJunglePotion(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return jungleErrand(s, CustomJungleStart, 0, "ask Trufitus for the herb-gathering job")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch {
	case s.Stage >= JungleComplete:
		return Step{Kind: StepDone}
	case s.Stage == JungleNotStarted:
		return jungleErrand(s, CustomJungleStart, 0, "ask Trufitus for the herb-gathering job")
	case s.Stage >= JungleFoundAllHerbs:
		return jungleErrand(s, CustomJungleFinish, 0, "let Trufitus commune with the gods")
	}
	herb, index, give, ok := JungleHerbOf(s.Stage)
	if !ok {
		return Step{Kind: StepWait, Reason: "unrecognised Jungle Potion stage"}
	}
	// A carried unidentified herb outranks the page: it only exists once picked, which the page cannot always say, and Trufitus
	// takes only the clean one.
	if s.HeldID(herb.Unid) > 0 {
		return Step{Kind: StepOpHeld, ItemID: herb.Unid, Item: "Unidentified " + strings.ToLower(herb.Name), Op: "Identify", Reason: "clean the " + herb.Name}
	}
	if give && s.HeldID(herb.Clean) > 0 {
		return jungleErrand(s, CustomJungleHandIn, index, "give the "+herb.Name+" to Trufitus")
	}
	if s.FreeSlots < 1 {
		return Step{Kind: StepWait, ParkFor: JungleLegPark, Reason: "no room in the pack for the " + herb.Name}
	}
	// The cave's price is paid again on the island, where the food of the departure may have been eaten on the way and there is no
	// bank to draw more from: an account that would now lose to the jogres waits out the hitpoints and does not go in. (An account
	// already inside the cave goes out by the Hand holds and is not stopped.)
	if herb.Underground && s.TileKnown && OnKaramja(s.Tile) {
		if ok, why := JungleCaveOdds(s); !ok {
			return Step{Kind: StepWait, ParkFor: JungleOddsPark, Reason: "not going into the cave of the jogres: " + why}
		}
	}
	return jungleErrand(s, CustomJunglePick, index, "pick the "+herb.Name)
}

// JunglePotionRecord is the quest's identity. The server gates it on Druidic Ritual and the herbs on Herblore 3 (which that quest
// pays); Hitpoints 35 is the module's own floor for the cave.
var JunglePotionRecord = Record{ID: JunglePotionID, Name: JunglePotionName, QuestPoints: 1,
	Requirements: Requirements{Quests: []string{DruidicRitualID}, Skills: []SkillReq{{Skill: "herblore", Level: 3}, {Skill: "hitpoints", Level: JungleMinHitpoints}}}}

// JunglePotion is the engine module. It never fights (NoRetaliate): the spiders and tribesmen are walked round, the jogres run
// past. One death parks it for a day: the cave is priced, so a death means the price was wrong for this account.
var JunglePotion = Module{
	Record: JunglePotionRecord, Bank: DraynorBank, BankSet: true, Anchor: TrufitusAnchor,
	ToolIDs:     jungleTools,
	ReadStage:   ParseJunglePotionJournal,
	Sustain:     jungleSustain,
	NoRetaliate: true,
	MaxDeaths:   1,
	Decide:      DecideJunglePotion,
}
