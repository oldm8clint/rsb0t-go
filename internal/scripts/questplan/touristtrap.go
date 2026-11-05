package questplan

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Tourist Trap (content id `desertrescue`, %desertrescue and %desertrescue_map_mechanisms, 2 quest points, Smithing 20 and Fletching
// 10), ported from the TS def (defs/touristtrap.ts, 2,487 lines) and checked against content 289: quests/quest_desertrescue
// (quest_desertrescue.rs2, irena.rs2, mercenary_captain.rs2, mercenary.rs2, camp_guard.rs2, mining_slave.rs2, al_shabim.rs2,
// bedabin_nomad_guard.rs2, captain_siad.rs2, mine_cart_driver.rs2, ana.rs2, desertrescue_journal.rs2 and the constants), areas/area_desert
// (desert_heat.rs2, the zone tables), areas/area_alkharid/shantay_pass.rs2 and the maps m49_47, m51_47, m51_48 and m51_147.
//
// THE REWARD. Irena pays 4,650 experience twice, in Fletching, Agility, Smithing or Thieving (stat_advance(x, 46500), the same skill may be
// picked twice). There is no combat and no Magic on offer. The module takes Thieving both times: it is the stat the camp's own slave lock
// wants (stat_random(thieving, 20, 250)), the one the Tribal Totem (21) and the Digsite Quest (25) are gated on and no rung trains, and the one
// the fleet's endgame loop spends its hours on; 9,300 experience is level 26 from scratch (TtRewardPick).
//
// THE STAGES, in the order they run (%desertrescue; the varp is not transmitted, so the stage is read off the journal, which the content
// writes cumulatively and the module reads newest-needle-first, as the TS def does; TtMarkers).
//
//	0  Irena stands south of the Shantay Pass (3303,3113): "What's the matter?", "Is there a reward if I get her back?", "Okay Irena, calm
//	   down. I'll get your daughter back for you." (1). The pass costs a Shantay pass (5 coins, a stack) a time, the first time with a
//	   disclaimer from the guard; it is free going north. South of it the desert's heat is on.
//	1  The Mercenary Captain (level 47, 80 hitpoints, a hit of up to 5) stands outside the camp's gate at (3270,3029). Talking to him
//	   sets stage 3; "Wow! A real captain!", "I'd love to work for a tough guy like you!", "Can't I do something for a strong Captain like
//	   you?", "Sorry Sir, I don't think I can do that.", "It's a funny captain who can't fight his own battles!" ends with npc_retaliate:
//	   he attacks the account and the guards stand by. A cold Attack on him is the one thing NOT to click: while he is not fighting the
//	   account it asks "are you sure" and the answer yes has the guards throw the account into the desert and its water away.
//	4  His death hands over the Metal key ("defeat_merc_capt": only to a killer who has none in the pack and none in the bank).
//	5  The key opens the camp's gate (3273,3028/9) from outside. Inside, any weapon but a pickaxe or any armour (shield, helmet, gloves,
//	   body or leg armour; the desert and slave clothes are none) is found by a guard within four tiles at the door and by a timer every
//	   25 to 100 ticks, and the account is searched and thrown in the cell (3285,3034). Leaving by the gate in slave clothes with a guard
//	   within four tiles is the same.
//	6/7 A male slave (825, five of them) asks the account to pick his chains: stat_random(thieving, 20, 250) a try (8% at Thieving 1,
//	   30% at 25), and from the third failure on a guard within ten tiles of the slave spots the account and it is searched and jailed.
//	8  The slave trades his clothes for desert clothes (shirt, robe, boots, carried or worn: a carried piece is converted first, a worn
//	   one in place).
//	9  Slave clothes open the mine doors (3301/2,3036): the account lands in the hall (3278,9426) of the mine underneath.
//	10 The cave guard (829, in the hall) wants a Tenti pineapple: "I'd like to mine in a different area.", "Yes sir, you're quite right
//	   sir.", "Yes sir, we understand each other perfectly." (the mine door, the cave guard, nothing else is needed for 10).
//	11 Al Shabim, in the Bedabin camp (3171,3025), asks for the plans in Captain Siad's chest and gives a copy of the key to it.
//	12 Siad's room is upstairs in the camp (3290,3036): the bookcase searched, "You seem to have a lot of books!", "So, you're interested in
//	   sailing?", "I could tell by the cut of your jib." distracts him (the bit is cleared by the next chest try), the chest opened with the
//	   Bedobin key gives the Technical plans (the message box is raised BEFORE the plans are added: continued, never closed).
//	13 The plans shown to Al ("Yes, I'm very interested.", "Yes, I'm kind of curious."): a bronze bar, ten feathers and a hammer.
//	14 The Bedabin guard (834) lets the account into the tent (3169,3046) when shown the plans; the experimental anvil (3171,3048) takes the
//	   bar at Smithing 20 (stat_random(smithing, 61, 245): 38% at 20, the bar is spent either way) for a Prototype dart tip.
//	15 Ten feathers on the tip at Fletching 10 (stat_random(fletching, 61, 254): 31% at 10, the ten feathers are spent either way).
//	16 The dart to Al: six bronze darts, the key taken back, a Tenti pineapple.
//	17 The pineapple to the cave guard; the cave (3281,9415) now lets the slave through to the lower mine.
//	18 The lower mine's empty barrel (3303,9415) and the cart (3303,9417): stat_random(agility, 100, 250) to squeeze in (39% at Agility 1, a
//	   miss costs 2 hitpoints) to the deep mine (3302,9417 -> 3319,9431).
//	19 Ana (3302,9466) is caught with the empty barrel. Stages 20 to 26 are hidden from the journal (it reads "get Ana's barrel out of this mine
//	   camp" for all of them): the barrel is put in the deep cart, the account rides it to the lower mine, finds Ana among the barrels there,
//	   puts her on the lift (the winch guard: "Yes please.", "I said you were very gregarious!"), leaves by the cave and the mine doors,
//	   pulls the surface winch, finds her in the full barrel at (3278,3017), puts her on the surface cart (3287,3023), bribes the driver for
//	   100 coins ("Nice cart.", "One wagon wheel says to the other, 'I'll see you around'.", ... "A hundred it is!") and boards: stage 26.
//	   The cart sets the account down in the desert at (3258,3029) with Ana in a barrel, to be walked to Irena.
//	27 "Hey, great you've found Ana!" and the two reward menus, ending the quest.
//
// WHAT CAN HURT, and how each is handled.
//
//   - THE DESERT'S HEAT. In the desert zone (TtInDesertZone, not the camp and not the Bedabin camp) a timer of 150 ticks, 190 in the full desert
//     outfit (10 for the shirt, 20 for the robe, 10 for the boots; armour takes 10 to 40 off), sips a dose from the lowest waterskin that has
//     one; with none it says so and deals 1 to 10 damage. The module never enters the desert holding fewer than TtWaterEntry doses, never
//     starts a leg across it holding fewer than TtWaterLeg, turns for water (the Bedabin camp's shop, else the pass) when it falls under
//     TtWaterFloor, and a guard's toss into the desert (which takes the water away) is a walk to the nearest water at once.
//   - THE CAPTAIN is priced before the account crosses (MeleeChain on the account's levels and the food in its pack, TtCaptainFit) and
//     again before the taunt; a loss in the model is a 3 hour park, a death a day (Module.MaxDeaths).
//   - THE ROWDY GUARDS (842, level 43, hunt combat 86 and under within a tile) stand in the camp and the mine; the walks keep out of
//     rectangles round their posts where the pack has a way round, and an account they set on is fought back by the engine.
//   - THE SEARCHES. Every way of being caught ends in a cell: the surface cell (a window bar bent with a Strength roll, four rocks climbed
//     with Agility rolls, 7 hitpoints a fall) or the punishment mine (fifteen "Rock" from the cell's rocks, and a gate). Both are walked out of by
//     the module; a count of the cells in a run parks the quest when the slave's lock keeps costing them.
//   - THE EQUIPMENT. Everything but the clothes comes off at the camp's gate (and goes back on when the quest ends, the demon quest's way:
//     the ids are remembered in the quest's store). It comes off into the pack, one slot a piece, so the first kit is planned against the pack
//     less the gear worn (ttSpareSlots) and a strip with no room parks the quest naming the slots. A restock's bank visit banks what the strip
//     left in the pack (the provisioner deposits first everything that is neither kit nor kept), and then the gear is in the account's bank for
//     whatever script fetches its kit next, not in the pack for ttFinish.
//   - THE FOOD. The strip leaves the account bare-handed for the nine rowdy guards, so the pack's food (judged as one pool by the hitpoints it
//     restores) must reach TtFoodEntryHeal to cross and TtFoodRestockHeal to stay south, a rowdy guard is priced before the gate
//     and the mine doors (TtCampFit), and the hitpoints a fall costs are rested for before the attempt (questmods ttRestAbove).
//
// THE POCKETS. The camp (414 tiles, behind the gate), the hall (441), the lower mine (332), the deep mine (639) and their cells are separate
// pieces of ground on the pack; the gate, the mine doors, the cave and the carts join them and the module crosses each by its own custom.

// TouristTrapName is the quest tab's spelling (component 7379, `questlist:desertrescue`) and TouristTrapID the engine's id, content's own.
const (
	TouristTrapName = "The Tourist Trap"
	TouristTrapID   = "desertrescue"
)

// The %desertrescue values the journal tells apart (desertrescue.constant). Stages 19 to 26 and 27 to 29 read the same on the page.
const (
	TtNotStarted = 0
	TtStarted    = 1
	TtApproached = 3
	TtKilledCapt = 4
	TtEnteredCmp = 5
	TtSpokenSlv  = 6
	TtFreedSlave = 7
	TtTraded     = 8
	TtEnteredMin = 9
	TtFindingPin = 10
	TtBedobinKey = 11
	TtGotPlans   = 12
	TtShownPlans = 13
	TtMadeTip    = 14
	TtMadeDart   = 15
	TtLearned    = 16
	TtGivenPine  = 17
	TtUsedCart   = 18
	TtRescue     = 19 // 19 to 26
	TtReward     = 27 // 27 to 29
	TtComplete   = 30
)

// Npc ids (checked against the production config by TestTouristTrapIDsMatchTheProductionConfig).
const (
	NpcTtAna         = 822
	NpcTtSlave       = 825 // [mining_slave_male], five in the camp
	NpcTtEscaped     = 826 // [escaped_slave]: the same slave for 200 ticks after the trade
	NpcTtRowdySlave  = 827
	NpcTtMerc        = 828 // [desertminingmercenary1]
	NpcTtCaveGuard   = 829 // [desertminingmercenary2], the guards in the mine
	NpcTtCaptain     = 830
	NpcTtSiad        = 831
	NpcTtAlShabim    = 832
	NpcTtBedabin     = 833 // the Bedabin camp's shop
	NpcTtBedabinGrd  = 834
	NpcTtIrena       = 835
	NpcTtDriver      = 841
	NpcTtRowdyGuard  = 842
	NpcTtDesertWolf  = 839
	NpcTtUgthanki    = 840
	LocTtPass        = 4031
	LocTtGateSouth   = 2673 // [miningcampgateclosedl]
	LocTtGateNorth   = 2674
	LocTtMineDoorL   = 2675 // [thttmineentrancel]
	LocTtMineDoorR   = 2676
	LocTtExitDoorL   = 2690 // [thttmineexitl]
	LocTtExitDoorR   = 2691
	LocTtCaveIn      = 2699 // [thminecavel] at 3281,9415: the hall's side
	LocTtCaveOut     = 2698 // [thminecaver] at 3283,9415: the lower mine's side
	LocTtLadderUp    = 1747
	LocTtLadderDown  = 1746
	LocTtBookcase    = 2678
	LocTtDesk        = 2679
	LocTtChest       = 2677
	LocTtWindow      = 2683
	LocTtRock1       = 2694
	LocTtRock2       = 2695
	LocTtRock3       = 2696
	LocTtRock4       = 2697
	LocTtWinch       = 2667
	LocTtFullBarrel  = 2680
	LocTtEmptyBarrel = 2681
	LocTtSurfaceCart = 2682
	LocTtLiftBucket  = 2668
	LocTtMineCart    = 2684
	LocTtCellGateL   = 2685
	LocTtCellGateR   = 2686
	LocTtPunishRock  = 2704
	LocTtAnvil       = 2672
	LocTtTentDoor    = 2700
)

// Object ids (the same test).
const (
	ObjTtSkin4        = 1823
	ObjTtSkin3        = 1825
	ObjTtSkin2        = 1827
	ObjTtSkin1        = 1829
	ObjTtSkin0        = 1831
	ObjTtShirt        = 1833
	ObjTtRobe         = 1835
	ObjTtBoots        = 1837
	ObjTtMetalKey     = 1839
	ObjTtCellKey      = 1840
	ObjTtBarrel       = 1841 // [thminebarrel_empty]
	ObjTtAnaBarrel    = 1842 // [thanainabarrel]
	ObjTtGoodMineKey  = 1843
	ObjTtSlaveShirt   = 1844
	ObjTtSlaveRobe    = 1845
	ObjTtSlaveBoots   = 1846
	ObjTtDisclaimer   = 1848
	ObjTtDart         = 1849
	ObjTtPlans        = 1850
	ObjTtPineapple    = 1851
	ObjTtBedobinKey   = 1852
	ObjTtDartTip      = 1853
	ObjTtPass         = 1854
	ObjTtPunishRock   = 1855
	ObjTtFeather      = 314
	ObjTtBronzeDart   = 806
	ObjTtKebab        = 1971
	ObjTtBronzePickax = 1265
)

// Skills the anvil and the dart want, and the hitpoints the module will not start a desert trip under.
const (
	TtSmithing  = 20
	TtFletching = 10
	// TtMinHitpoints is the base Hitpoints below which no trip into the desert is begun: the wolves (a hit of 3) and the Ugthanki (5) that wander
	// it hunt a combat level 54 or 84 and under, and a rowdy guard's hit is 5.
	TtMinHitpoints = 25
)

// The dose and ration policy. Doses are the waterskin's: Waterskin(4) holds four, one sip a heat tick.
const (
	// TtWaterEntry is the doses the account holds when it crosses south: three full skins. TtWaterFloor is the doses under which it turns for water
	// in the heat, and the least it starts any leg across the open desert with (the camp to the Bedabin camp is about 110 tiles, under a minute and a
	// half; a dose lasts 190 ticks). TtSkins is the full skins a restock asks for.
	TtWaterEntry = 12
	TtWaterFloor = 3
	TtSkins      = 3
	// TtCoinFloor is the coins the account keeps from the first crossing on: the driver's 100, two tolls of the Al Kharid gate and a pass
	// and a skin or two for a restock. TtCoinStart is what the first kit draws: the shops' prices (a second outfit, three skins, a pass, the bars, the
	// feathers and a hammer come to about 500 when the mule has none of them) and the floor on top.
	TtCoinFloor = 200
	TtCoinStart = 800
	// TtFeathers is the feathers asked for (each failed try spends ten: 31% at Fletching 10), TtFeatherMin the least that starts the dart;
	// TtBars the bronze bars (the anvil spends one a try: 38% at Smithing 20), TtBarMin the least.
	TtFeathers   = 60
	TtFeatherMin = 20
	TtBars       = 4
	TtBarMin     = 2
)

// The pack and the food. The camp's strip takes every weapon and piece of armour off and into the pack (ttGearPieces: one slot each, up to about
// eight), so the first kit is planned against the pack less that gear and a little slack (ttSpareSlots); what does not fit is fetched later. The
// strip also leaves the account bare-handed for the nine rowdy guards (a hit of 5), the wolves and the Ugthanki: food is a floor, not a wish.
const (
	// TtPackSlots is the pack's size; TtSlotSlack the room the first kit leaves for what the quest hands over on the way (the plans, a barrel, the
	// pineapple, the tip).
	TtPackSlots = 28
	TtSlotSlack = 3
	// TtFoodEntryHeal is the hitpoints the pack's food must restore when the account crosses the pass (three sharks), TtFoodRestockHeal what it must
	// still hold south of it (one shark): under that the account turns for the bank, and one that cannot be fed there parks.
	TtFoodEntryHeal   = 60
	TtFoodRestockHeal = 20
	// TtFoodWant is the pieces a kit asks for (the same before and after the captain's fight: the guards are priced on the pack's food, and an
	// account that passes with six sharks may not with three); TtFoodMin the fewest pieces of the best food that satisfy a kit.
	TtFoodWant = 6
	TtFoodMin  = 3
)

// Parks, in how long an account that cannot go on is skipped.
const (
	// TtCaptainPark: the captain beats the model; the account's levels do not change in an hour.
	TtCaptainPark = 3 * time.Hour
	// TtItemPark: nothing the bank, the mule and the shop can give covers what the quest wants; the mule's stock changes by the hour.
	TtItemPark = 3 * time.Hour
	// TtLevelPark: Smithing 20 and Fletching 10 are the record's, parked when they are not there.
	TtLevelPark = 6 * time.Hour
	// TtJailPark: the slave's lock has cost TtJailsToPark cells in one run.
	TtJailPark = 6 * time.Hour
	// TtLostPark: a quest item that cannot be had again (the slave outfit and no desert clothes to trade, the rescue's coins).
	TtLostPark = 24 * time.Hour
)

// TtJailsToPark is the cells (surface or punishment mine) one run may cost before the quest is parked for TtJailPark.
const TtJailsToPark = 12

// Tiles from the content maps, the loc index and the collision pack.
var (
	// TtIrena is Irena (m51_48 "0 39 41: 835"), TtShantayShop Shantay's anchor (ShantayAnchor).
	TtIrena = tile.Tile{X: 3303, Z: 3113}
	// TtPassNorth and TtPassSouth are the stands either side of the pass (the planner's own: the north one is on Shantay's side).
	TtPassNorth = tile.Tile{X: 3304, Z: 3118}
	TtPassSouth = tile.Tile{X: 3304, Z: 3114}
	// TtCaptainAt is the Mercenary Captain's post (m51_47 "0 6 21: 830"); the gate's outside tile TtGateOut and the inside one TtGateIn.
	TtCaptainAt = tile.Tile{X: 3270, Z: 3029}
	TtGateOut   = tile.Tile{X: 3273, Z: 3029}
	TtGateIn    = tile.Tile{X: 3274, Z: 3029}
	// TtSlaveAt is the male slave the module talks to (m51_47 "0 38 8: 825").
	TtSlaveAt = tile.Tile{X: 3302, Z: 3016}
	// TtMineDoor is the camp-side stand of the mine doors; TtHall the tile they land the account on (0_51_147_14_18 = 3278,9426) and
	// TtHallExit the stand inside the hall of the exit doors.
	TtMineDoor = tile.Tile{X: 3301, Z: 3036}
	TtHallExit = tile.Tile{X: 3278, Z: 9427}
	// TtCaveHall and TtCaveLower are the tiles either side of the guarded cave (the teleports land on them: 0_51_147_14_7 and 0_51_147_22_7).
	TtCaveHall  = tile.Tile{X: 3278, Z: 9415}
	TtCaveLower = tile.Tile{X: 3286, Z: 9415}
	// TtGuardAt is where the hall's guards stand (m51_147 "0 15 6: 829", "0 15 8: 829").
	TtGuardAt = tile.Tile{X: 3279, Z: 9415}
	// TtAlAt is Al Shabim (m49_47 "0 35 17: 832"); TtTentApproach the tile south of the tent door (the interior is a sealed piece of ground) and
	// TtTentIn the first tile inside; TtAnvil the anvil.
	TtAlAt        = tile.Tile{X: 3171, Z: 3025}
	TtTentOutside = tile.Tile{X: 3169, Z: 3045}
	TtTentInside  = tile.Tile{X: 3169, Z: 3046}
	TtAnvil       = tile.Tile{X: 3171, Z: 3048}
	// TtLadder is the camp's ladder (loc 1747 at 3290,3036); Siad's room is upstairs: the bookcase TtBookcase, the desk TtDesk, the chest TtChest
	// (forced approach: its east side), and TtSiadAt where he stands.
	TtLadder   = tile.Tile{X: 3290, Z: 3036}
	TtBookcase = tile.Tile{X: 3284, Z: 3032, Level: 1}
	TtDesk     = tile.Tile{X: 3290, Z: 3033, Level: 1}
	TtChest    = tile.Tile{X: 3292, Z: 3033, Level: 1}
	TtSiadAt   = tile.Tile{X: 3291, Z: 3032, Level: 1}
	// The surface cell: the window (3284,3032), the account's tile inside (3285,3034) and the rocks, the landing tiles after each of which are TtJailRocks.
	TtCellAt   = tile.Tile{X: 3285, Z: 3034}
	TtWindowAt = tile.Tile{X: 3284, Z: 3032}
	// TtLowerBarrel is the empty barrel by the lower cart (3303,9415), TtLowerCart the cart (3303,9417), TtDeepCart the deep one (3318,9430).
	TtLowerBarrel = tile.Tile{X: 3303, Z: 9415}
	TtLowerCart   = tile.Tile{X: 3303, Z: 9417}
	TtDeepCart    = tile.Tile{X: 3318, Z: 9430}
	// TtLiftAt is the winch bucket (3292,9423), TtAnaAt Ana (m51_147 "0 38 58: 822"), TtRowdyAt a rowdy slave.
	TtLiftAt = tile.Tile{X: 3292, Z: 9423}
	TtAnaAt  = tile.Tile{X: 3302, Z: 9466}
	// TtWinchAt is the surface winch (3279,3017), TtTopBarrel the full barrel it brings Ana up in (3278,3017), TtSurfaceCart the cart (3287,3023).
	TtWinchAt     = tile.Tile{X: 3279, Z: 3017}
	TtTopBarrel   = tile.Tile{X: 3278, Z: 3017}
	TtSurfaceCart = tile.Tile{X: 3287, Z: 3023}
	// TtDriverAt is the driver (m51_47 "0 23 14: 841"); TtCartLanding where the cart sets the account down.
	TtDriverAt    = tile.Tile{X: 3287, Z: 3022}
	TtCartLanding = tile.Tile{X: 3258, Z: 3029}
	// TtCellGate is the punishment mine's gate (3292,9434/5); TtCellPick the two pickaxes lying in the cell.
	TtCellGate = tile.Tile{X: 3292, Z: 9434}
	TtCellPick = []tile.Tile{{X: 3288, Z: 9431}, {X: 3288, Z: 9442}}
	// TtBedabinShop is the Bedabin camp's shop (npc 833, Trade): Waterskin(4) at five a visit.
	TtBedabinShop = ShopRef{Npc: "Bedabin Nomad", NpcID: NpcTtBedabin, Anchor: tile.Tile{X: 3159, Z: 3031}}
)

// TtJailRocks are the landing tiles of the surface cell's way out: the one after the window (telejump west by one), then one per rock.
var TtJailRocks = []tile.Tile{{X: 3283, Z: 3032}, {X: 3282, Z: 3037}, {X: 3279, Z: 3038}, {X: 3277, Z: 3039}, {X: 3275, Z: 3040}}

// TtRowdyPosts are the rowdy guards' posts (m51_47 "0 16 18", "0 23 22", "0 32 18" and m51_147 "0 4 9", "0 13 44", "0 22 58", "0 23 6", "0 34 24",
// "0 53 33": npc 842). The walks keep out of the square round each of the camp's and the hall's; the cave's landing (3287,9414) is one that
// no walk can avoid (the teleport lands the account beside it).
var TtRowdyPosts = []tile.Tile{
	{X: 3280, Z: 3026}, {X: 3287, Z: 3030}, {X: 3296, Z: 3026},
	{X: 3268, Z: 9417}, {X: 3277, Z: 9452}, {X: 3286, Z: 9466}, {X: 3287, Z: 9414}, {X: 3298, Z: 9432}, {X: 3317, Z: 9441},
}

// TtRowdyReach is how far from its post a rowdy guard is found (wanderrange 3, huntrange 1).
const TtRowdyReach = 4

// ---------------------------------------------------------------------------------------------------------------
// Areas.
// ---------------------------------------------------------------------------------------------------------------

// TtArea is the piece of the world the account stands in, the TS def's touristTrapArea made exact on the real pack.
type TtArea int

const (
	TtAreaUnknown TtArea = iota
	TtAreaMainland
	TtAreaShantayNorth // Shantay's shop and the pass, north of the gate
	TtAreaIrena        // just south of the pass
	TtAreaDesert       // the open desert, the captain's post, the gate's outside
	TtAreaBedabin      // the Bedabin camp and its surroundings
	TtAreaTent         // inside the Bedabin experimental tent (a sealed piece of ground)
	TtAreaCamp         // the mining camp's yard
	TtAreaCampUpper    // Captain Siad's room, upstairs
	TtAreaSurfaceCell  // the cell and the rocks out of it
	TtAreaHall         // behind the mine doors up to the guarded cave
	TtAreaLower        // the lower mine: the lift, the barrels, the lower cart
	TtAreaDeep         // the deep mine: Ana, the deep cart
	TtAreaMineCell     // the punishment mine
)

// TtZone is the chain of places the quest walks, from the shop to the deep mine. Every crossing joins two neighbours.
type TtZone int

const (
	TtZoneNone TtZone = iota
	TtZoneNorth
	TtZoneOpen
	TtZoneCamp
	TtZoneHall
	TtZoneLower
	TtZoneDeep
)

// ZoneOf is the zone an area is in.
func (a TtArea) ZoneOf() TtZone {
	switch a {
	case TtAreaMainland, TtAreaShantayNorth:
		return TtZoneNorth
	case TtAreaIrena, TtAreaDesert, TtAreaBedabin, TtAreaTent:
		return TtZoneOpen
	case TtAreaCamp, TtAreaCampUpper:
		return TtZoneCamp
	case TtAreaHall:
		return TtZoneHall
	case TtAreaLower:
		return TtZoneLower
	case TtAreaDeep:
		return TtZoneDeep
	}
	return TtZoneNone
}

// South reports the areas on the far side of the pass.
func (a TtArea) South() bool {
	return a != TtAreaUnknown && a != TtAreaMainland && a != TtAreaShantayNorth
}

// desertRects are content's desert_zones (desert_heat_zones.dbrow) as tile rectangles, all level 0.
var desertRects = []Rect{
	{MinX: 3136, MaxX: 3199, MinZ: 2944, MaxZ: 3071}, // 0_49_46_0_0 to 0_49_47_63_63
	{MinX: 3200, MaxX: 3263, MinZ: 2944, MaxZ: 3135}, // 0_50_46_0_0 to 0_50_48_63_63
	{MinX: 3264, MaxX: 3294, MinZ: 2944, MaxZ: 3135}, // 0_51_46_0_0 to 0_51_48_30_63
	{MinX: 3294, MaxX: 3317, MinZ: 2944, MaxZ: 3117}, // 0_51_46_30_0 to 0_51_48_53_45
	{MinX: 3317, MaxX: 3391, MinZ: 2944, MaxZ: 3135}, // 0_51_46_53_0 to 0_52_48_63_63
}

// heatFreeRects are content's desertheat_disabled_zones: the Bedabin camp (three overlapping rectangles) and the mining camp.
var heatFreeRects = []Rect{
	{MinX: 3156, MaxX: 3186, MinZ: 3019, MaxZ: 3048},
	{MinX: 3155, MaxX: 3181, MinZ: 3030, MaxZ: 3053},
	{MinX: 3154, MaxX: 3167, MinZ: 3031, MaxZ: 3055},
	{MinX: 3274, MaxX: 3306, MinZ: 3011, MaxZ: 3043},
}

// TtInDesertZone reports a tile in content's desert zones, where the heat timer runs.
func TtInDesertZone(t tile.Tile) bool {
	for _, r := range desertRects {
		if r.Contains(t) {
			return true
		}
	}
	return false
}

// TtHeat reports a tile where the heat deals with the account's water: in the desert zones and out of the camps that switch it off.
func TtHeat(t tile.Tile) bool {
	if !TtInDesertZone(t) {
		return false
	}
	for _, r := range heatFreeRects {
		if r.Contains(t) {
			return false
		}
	}
	return true
}

// TtAreaOf is the area of a tile.
func TtAreaOf(t tile.Tile) TtArea {
	switch {
	case t.Level == 0 && t.X >= 3284 && t.X <= 3287 && t.Z >= 3032 && t.Z <= 3037:
		// The cell is the 18 walkable tiles of that box on the real pack (the prison door's column and one tile north of it are inside).
		return TtAreaSurfaceCell
	case t.Level == 0 && tileIn(t, TtJailRocks):
		return TtAreaSurfaceCell
	case t.X >= 3274 && t.X <= 3306 && t.Z >= 3011 && t.Z <= 3043 && (t.Level == 0 || t.Level == 1):
		if t.Level == 1 {
			return TtAreaCampUpper
		}
		return TtAreaCamp
	case t.Level == 0 && t.X >= 3264 && t.X <= 3327 && t.Z >= 9408 && t.Z <= 9471:
		// The order checked against every walkable tile of m51_147: the cave wall, the punishment gate and the carts each split adjacent coordinates.
		switch {
		case t.X <= 3282:
			return TtAreaHall
		case t.X >= 3285 && t.X <= 3292 && t.Z >= 9429 && t.Z <= 9452:
			return TtAreaMineCell
		case t.X >= 3315 || t.Z >= 9428:
			return TtAreaDeep
		}
		return TtAreaLower
	case t.Level == 0 && t.X >= 3167 && t.X <= 3172 && t.Z >= 3046 && t.Z <= 3049:
		// The sealed piece of ground inside the Bedabin experimental tent: 18 tiles of that box on the real pack; everything else near it is desert.
		return TtAreaTent
	case t.Level == 0 && t.X >= 3300 && t.X <= 3314 && t.Z >= 3109 && t.Z <= 3116:
		return TtAreaIrena
	case t.Level == 0 && ((t.X >= 3291 && t.X <= 3316 && t.Z >= 3118 && t.Z <= 3140) || (t.Z == 3117 && t.X >= 3302 && t.X <= 3305)):
		// Shantay's side of the wall. The row just north of the wall is two stands of the pass (3303,3117 and 3304,3117) and desert elsewhere; the
		// desert reaches z 3119 at x 3288 to 3290, which the box leaves out.
		return TtAreaShantayNorth
	case t.Level == 0 && t.X >= 3150 && t.X <= 3200 && t.Z >= 2990 && t.Z <= 3070:
		return TtAreaBedabin
	case t.Level == 0 && TtInDesertZone(t):
		return TtAreaDesert
	}
	return TtAreaMainland
}

func tileIn(t tile.Tile, ts []tile.Tile) bool {
	for _, c := range ts {
		if c.X == t.X && c.Z == t.Z {
			return true
		}
	}
	return false
}

// TtHeatFree reports the open-desert areas where the heat is off: the Bedabin camp proper.
func TtHeatFree(t tile.Tile) bool { return TtInDesertZone(t) && !TtHeat(t) }

// ---------------------------------------------------------------------------------------------------------------
// The journal.
// ---------------------------------------------------------------------------------------------------------------

// ttMarkers are the journal's markers, newest stage first: desertrescue_journal.rs2 keeps every earlier paragraph on the page, so the newest
// one decides. The TS def's needles, kept in its order, each checked against the script by TestTouristTrapJournalNeedlesAreTheScripts.
var ttMarkers = []struct {
	stage int
	all   []string
}{
	{TtComplete, []string{"quest complete!"}},
	{TtReward, []string{"talk to irena", "for my reward"}},
	{TtRescue, []string{"get ana", "her barrel", "out of this mine camp"}},
	{TtUsedCart, []string{"find some way of getting ana out"}},
	{TtGivenPine, []string{"i am sure ana is down here"}},
	{TtLearned, []string{"now i have a pineapple"}},
	{TtLearned, []string{"misplaced it", "another one"}},
	{TtMadeDart, []string{"made a prototype dart"}},
	{TtMadeTip, []string{"made the prototype dart tip"}},
	{TtShownPlans, []string{"if i make him a", "prototype"}},
	{TtGotPlans, []string{"captain siad", "stolen the plans"}},
	{TtBedobinKey, []string{"if i can steal the", "plans"}},
	{TtFindingPin, []string{"get the mine guard a 'tenti' pineapple"}},
	{TtEnteredMin, []string{"head deeper into the mine"}},
	{TtTraded, []string{"head into the mine", "look for ana"}},
	{TtFreedSlave, []string{"trade my desert robes"}},
	{TtSpokenSlv, []string{"undo the locks on the slave"}},
	{TtEnteredCmp, []string{"look around this desert mining camp"}},
	{TtKilledCapt, []string{"inside a desert mining camp"}},
	{TtApproached, []string{"feeling ana must be around here"}},
	{TtStarted, []string{"head into the desert", "search for ana"}},
	{TtNotStarted, []string{"start this quest by speaking to irena"}},
}

// ParseTouristTrapJournal reads %desertrescue from the rendered journal, the TS def's parseTouristTrapJournal. Stages 19 to 26 read 19 and 27 to 29
// read 27. ok is false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseTouristTrapJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, m := range ttMarkers {
		hit := true
		for _, n := range m.all {
			if !strings.Contains(text, n) {
				hit = false
				break
			}
		}
		if hit {
			return m.stage, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------------------------------------------
// The foes.
// ---------------------------------------------------------------------------------------------------------------

// The foes of the quest, from desertrescue.npc and area_desert/configs/desert.npc (TestQuestFoesMatchTheContent checks them against the
// content). The Captain is the one the module fights; the others are priced into the HP floor and the eat line of an account they set on.
var (
	// FoeMercenaryCaptain is [desertminingcaptain]: level 47, attack 32, defence 32, strength 29, 80 hitpoints, attackbonus 9, strengthbonus 14,
	// slash 15: a maximum hit of 5 every 4 ticks.
	FoeMercenaryCaptain = MeleeFoe{Name: "Mercenary Captain", Attack: 32, Strength: 29, Defence: 32, Magic: 0, Hitpoints: 80,
		AttackTicks: 4, AttackBonus: 9, StrengthBonus: 14, DefenceBonus: 15}
	// FoeRowdyGuard is [rowdymineguard]: level 43, 60 hitpoints, the same bonuses; huntmode cowardly with huntrange 1.
	FoeRowdyGuard = MeleeFoe{Name: "Rowdy Guard", Attack: 32, Strength: 32, Defence: 32, Magic: 0, Hitpoints: 60,
		AttackTicks: 4, AttackBonus: 9, StrengthBonus: 14, DefenceBonus: 15}
	// FoeDesertWolf is [desert_wolf] (level 27, 34 hitpoints) and FoeUgthanki [ugthanki] (level 42, 45 hitpoints, a stab): the desert's wanderers.
	FoeDesertWolf = MeleeFoe{Name: "Desert Wolf", Attack: 20, Strength: 21, Defence: 22, Magic: 0, Hitpoints: 34, AttackTicks: 4}
	FoeUgthanki   = MeleeFoe{Name: "Ugthanki", Attack: 35, Strength: 35, Defence: 35, Magic: 0, Hitpoints: 45, AttackTicks: 4}
)

// ttFoodIDs are the foods the account carries, best first (the fight tables know each).
var ttFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// TtCaptainFit prices the Captain on the account's levels and the food in its pack: MeleeChain on the shared tables, which tops up from the bites
// before the first blow and judges the account's hitpoints at the worst of the fight against his hardest hit. ok is true for an account whose
// levels are not read yet (nothing to judge).
func TtCaptainFit(s Snapshot) (bool, string) {
	odds := MeleeChain(s, []MeleeFoe{FoeMercenaryCaptain}, Sustain{FoodIDs: ttFoodIDs, EatAt: 15})
	return odds.OK, odds.Why
}

// TtCampFit prices one of the camp's and the mines' rowdy guards (60 hitpoints, a hit of 5; five of the ten legs of the camp and mine cannot keep
// out of a guard's square) on the account's levels and the food in its pack. The account meets it with its weapon and armour in the pack (the
// strip), so the levels are all it has, which is how MeleeChain prices anyway, and the model keeps a tenth of the bites for the way out. ok is true
// for an account whose levels are not read yet; an account with no food is never fit (the model keeps a bite back, and there is none).
func TtCampFit(s Snapshot) (bool, string) {
	odds := MeleeChain(s, []MeleeFoe{FoeRowdyGuard}, Sustain{FoodIDs: ttFoodIDs, EatAt: 15})
	return odds.OK, odds.Why
}
