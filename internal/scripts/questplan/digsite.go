package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Digsite Quest (content id `itexam`, varp %itexamlevel, 2 quest points, Agility 10, Herblore 10 and Thieving 25), ported from the
// TS def (defs/digsite/*.ts) and checked against content 289: quests/quest_itexam (the constants, quest_itexam.rs2, examiner.rs2,
// student1-3.rs2, area_digsite.rs2, digsite_workman.rs2, panning_guide.rs2, arcaeological_expert.rs2, private_dig_worker.rs2,
// itexam_chemistry.rs2, itexam_journal.rs2), areas/area_varrock/scripts/curator.rs2 and maps m52_52, m52_53, m52_152, m52_153.
//
// THE QUEST, in the order the stages run (%itexamlevel bits 0..3; the varp is not transmitted, so the stage is read off the journal).
//
//	1  The Examiner (Exam Centre) hands over a letter; the Curator in Varrock's museum stamps it the moment the talk opens; back to the
//	   Examiner, who starts the first exam on the spot (the account knows nothing yet and answers it badly: harmless).
//	2  The first exam. Each of the three students will help for a rock sample: the green one's is only ever a pickpocket from a Digsite
//	   workman (Thieving 25; 3 successes in 11 give it, while the stage is the first exam), the purple one's lies in one bush (3357,3372)
//	   and the orange one's is panned from the river (3 in 40, same stage rule). Used on its student, a sample answers the errand
//	   whatever its state (the three opnpcu scripts). With all three answered the journal says so, the Examiner is asked and the answers
//	   offered are the right ones: each question offers a wrong-only menu until the errand it belongs to is answered.
//	3  The second exam: a talk with each student sets the errand (no sample, no menu). Trowel and a certificate on passing.
//	4  The third exam: green and orange as before; the purple student wants an opal (her talk starts the errand, a talk with an opal in
//	   the pack ends it). The opal is a panned uncut opal cut with a chisel (Crafting 1, a coin flip at that level: opal_cutting 128,250).
//	5  A find to impress the expert: the level 3 site (x 3350..3357, z 3404..3412) wants a trowel, a specimen jar (the sacks at
//	   3359,3398) and a specimen brush (pickpocketed), and gives the strange talisman one dig in 20 (once). The expert gives a letter for
//	   it, and a workman accepts the letter (the stage moves to 6).
//	6  The shafts. Two winches (the main one at 3352,3417, the private one at 3370,3428) each need a rope tied to their bucket once and
//	   Agility 10 to squeeze down. The private (west) shaft holds the cave workman, who gives a chest key after four pleas, and an
//	   arcenia root on the floor. The chest at 3374,3378 gives an unidentified powder, the expert names it ammonium nitrate; the barrel at
//	   3364,3378 is levered open with the trowel and a vial dipped in it gives an unidentified liquid, which the expert names
//	   nitroglycerin; a charcoal is dug at the training site (1 dig in 13) and ground with a pestle and mortar; four mixes (the second and
//	   third want Herblore 10) end in the chemical compound.
//	7  The compound is poured over the bricks of the main (east) shaft, from (3379,9826) and nowhere else: that is the tile the blast's
//	   run sequence checks ("Unexpected player coord!").
//	8  The tinderbox lights it, a scripted run lands the account in the altar cave, where the Zarosian tablet lies (3374,9747); the
//	   tablet, shown to the expert, completes the quest: 15,300 Mining XP, 2,000 Herblore XP, 2 gold bars (content's stat_advance units
//	   are tenths).
//
// WHAT CAN HURT, and how each is handled.
//   - THE CHEMICALS EXPLODE ON DROP: the liquid, the nitroglycerin and both mixtures answer Drop (opheld5) with 25, 35, 45, 55 and 65
//     damage, and the compound 65. No drop in this module is by exclusion: only the ids in digJunk (the dig and pocket loot, checked
//     against the pack by a test) are ever dropped, and the order of stage 6 digs the charcoal and grinds it BEFORE the first volatile
//     item exists, so no dig is made holding one.
//   - The altar cave holds ten level 25 skeletons (npc 90, the Ernest closet's [skeleton_unarmed]: hit up to 3, a hit every 4 ticks,
//     hunt mode cowardly with huntrange 1). They hunt only an account of combat 50 or less (Npc.ts: combatLevel > vislevel*2 skips), and
//     the tablet lies two tiles from one of them. The pre-blast shaft has none in reach: the same npc rows sit in rock south of its 189
//     tiles. So the stage 8 visit is priced (DigAltarFit) on the account's levels, its hitpoints and its food for two skeletons over the
//     visit, and refused (parked) below it; the module never fights (NoRetaliate), it walks.
//   - The workmen hit for 1 on a failed pocket and stun for 8 ticks; nothing else attacks.
//
// THE POCKETS. Neither winch's shaft joins the walk graph: the east shaft (189 tiles), the west shaft (52), the altar cave (426) and
// its west copy (52) are four closed rectangles of rock that only the winches reach. Each has its own rope out, and each is registered
// (internal/scripts/questmods) so that a walk from inside climbs out first.
//
// WHAT THE CLIENT CANNOT SEE. The rope on each winch, the barrel's lid, the panning guide's tea, the talisman shown to the expert and
// the three errands past the first exam are varp bits the client never receives; the module tries the thing and reads the answer
// (a winch that does not reach the bottom, a pan that the guide forbids, a dig that finds nothing) instead of remembering.

// DigsiteID is the engine's id and DigsiteName the quest tab's spelling.
const (
	DigsiteID   = "itexam"
	DigsiteName = "Digsite Quest"
)

// The %itexamlevel values (quest_itexam.constant, quest.constant).
const (
	DigNotStarted      = 0
	DigStamping        = 1
	DigFirstExam       = 2
	DigSecondExam      = 3
	DigThirdExam       = 4
	DigImpressExpert   = 5
	DigMineshaftPermit = 6
	DigPouredCompound  = 7
	DigRemovedBlockage = 8
	DigComplete        = 9
)

// The levels the content enforces: the workman's pocket (Thieving 25, digworkman1_pickpocket), the winches (Agility 10,
// digsite_winch_teleport) and the second and third mixes (Herblore 10, opheldu postcharcoalmixture and precharcoalmixture).
const (
	DigThieving = 25
	DigAgility  = 10
	DigHerblore = 10
)

// Objects from content/pack (checked against the production config by TestDigsiteIDsMatchTheProductionConfig in questmods).
const (
	DigObjSpecimenJar    = 669 // [specimen_jar]
	DigObjSpecimenBrush  = 670 // [specimen_brush]
	DigObjSampleGreen    = 671 // [rock_sample1]: the green student's, a workman's pocket
	DigObjSampleOrange   = 672 // [rock_sample2]: the orange student's, panned
	DigObjSamplePurple   = 673 // [rock_sample3]: the purple student's, the bush
	DigObjTrowel         = 676 // [trowel]
	DigObjTrayEmpty      = 677 // [tray_empty]
	DigObjTrayMud        = 679 // [tray_mud]
	DigObjTalisman       = 681 // [digtalisman]
	DigObjPlainLetter    = 682 // [digplainletter]
	DigObjStampedLetter  = 683 // [recommendedletter]
	DigObjCert1          = 691
	DigObjCert2          = 692
	DigObjCert3          = 693
	DigObjExpertScroll   = 696 // [digexpertscroll], "Invitation letter"
	DigObjStoneTablet    = 699 // [zarosstonetablet]
	DigObjPowder         = 700 // [unidentified_powder]
	DigObjNitrate        = 701 // [ammonium_nitrate]
	DigObjLiquid         = 702 // [unidentified_liquid]: explodes on Drop
	DigObjNitro          = 703 // [nitroglycerin]: explodes on Drop
	DigObjGroundCharcoal = 704
	DigObjPreMix         = 705 // [precharcoalmixture]: explodes on Drop
	DigObjPostMix        = 706 // [postcharcoalmixture]: explodes on Drop
	DigObjCompound       = 707 // [digcompound]: explodes on Drop
	DigObjArceniaRoot    = 708
	DigObjChestKey       = 709 // [digchestkey]
	DigObjVial           = 229 // [vial_empty]
	DigObjPestle         = 233 // [pestle_and_mortar]
	DigObjTinderbox      = 590
	DigObjCharcoal       = 973
	DigObjRope           = 954
	DigObjChisel         = 1755
	DigObjOpal           = 1609
	DigObjUncutOpal      = 1625
	DigObjTea            = 1978 // [cup_of_tea], op Drink
)

// Npcs, locs and shops from content/pack.
const (
	DigNpcWorkman     = 613 // [digworkman1]: the site's workmen (Talk-to, Steal-from)
	DigNpcCaveWorkman = 614 // [digworkman2]: the west shaft's, with the chest key
	DigNpcGreen       = 615 // [student1]: green top, errand 1, rock_sample1
	DigNpcOrange      = 616 // [student3]: orange top, rock_sample2
	DigNpcPurple      = 617 // [student2]: purple shirt, rock_sample3, the third exam's opal
	DigNpcExaminer    = 618
	DigNpcExpert      = 619 // [archaeological_expert]
	DigNpcGuide       = 620 // [panning_guide]
	DigNpcCurator     = 646
	DigNpcTeaSeller   = 595 // [tea_seller]

	DigLocMainWinch    = 2350 // [digwinch1]: Operate; the east shaft
	DigLocPrivateWinch = 2351 // [digwinch2]: Operate; the west shaft
	DigLocRopeWest     = 2352 // [winchladder1]: Climb-up, the west shaft (and its altar copy) out beside the private winch
	DigLocRopeEast     = 2353 // [winchladder2]: Climb-up, the east shaft (and the altar cave) out beside the main winch
	DigLocSacks        = 2356 // [digsamplesacks]: Search, a specimen jar
	DigLocSampleBush   = 2358 // [digsitebushsample]: Search, the purple student's sample
	DigLocBarrel       = 2359 // [digbarrelclosed]: op1 Open, op2 Search; the trowel levers it, a vial fills
	DigLocChestOpen    = 2360 // [digchestopen]: Search, the powder
	DigLocChestShut    = 2361 // [digchestclosed]: the key opens it
	DigLocBrick        = 2362 // [digblastbrick]: the compound is poured on it, the tinderbox lights it
	DigLocPanning      = 2363 // [panning_point]: op1 Look
	DigLocSoil1        = 2376 // [digdugupsoil1..3]: the trowel's targets
	DigLocSoil2        = 2377
	DigLocSoil3        = 2378
)

// Tiles from content maps (m52_52, m52_53, m52_152, m52_153, m50_53, m51_53) and the loc index.
var (
	// DigExaminerAnchor is the first of the Examiner's three spawns (m52_52 "0 32 15: 618"; the others are 3364,3339 and 3364,3342).
	DigExaminerAnchor = tile.Tile{X: 3360, Z: 3343}
	// DigExpertAnchor is Terry Balando's spawn ("0 27 5: 619") and DigGuideAnchor the panning guide's ("0 48 49: 620").
	DigExpertAnchor = tile.Tile{X: 3355, Z: 3333}
	DigGuideAnchor  = tile.Tile{X: 3376, Z: 3377}
	// DigCuratorAnchor is the Curator's spawn in Varrock's museum (m50_53 "0 55 53: 646").
	DigCuratorAnchor = tile.Tile{X: 3255, Z: 3445}
	// DigTeaSellerAnchor is the tea seller's spawn (m51_53 "0 7 19: 595").
	DigTeaSellerAnchor = tile.Tile{X: 3271, Z: 3411}
	// DigStudentsAnchor is the middle of the student camp, where all three students spawn (four spawns each, from 3348,3397 to 3373,3432),
	// and DigWorkmenAnchor the workmen's, a tile from the soil of the level 3 site.
	DigStudentsAnchor = tile.Tile{X: 3358, Z: 3410}
	DigWorkmenAnchor  = tile.Tile{X: 3357, Z: 3407}
	// DigBushAnchor is the sample bush (loc 2358), DigSacksAnchor the sample sacks (loc 2356), DigChestAnchor the chest (loc 2361), DigBarrelAnchor
	// the barrel the quest levers (loc 2359; two more stand on either side of it).
	DigBushAnchor   = tile.Tile{X: 3357, Z: 3372}
	DigSacksAnchor  = tile.Tile{X: 3359, Z: 3398}
	DigChestAnchor  = tile.Tile{X: 3374, Z: 3378}
	DigBarrelAnchor = tile.Tile{X: 3364, Z: 3378}
	// DigTraySpawn is the panning tray's single spawn (m52_52 "0 41 50: 677"), DigPanAnchor the bank tile beside the panning point at 3379,3377
	// (the point itself is water; loc 2363 stands in the river at six places, all within 10 tiles of the guide, so every pan meets his gate).
	DigTraySpawn = tile.Tile{X: 3369, Z: 3378}
	DigPanAnchor = tile.Tile{X: 3379, Z: 3378}
	// DigTrayStand is the tile the tray is taken from: the spawn's own tile is not walkable (a fence or a rock stands on it), and the pack's
	// interact tiles are the four beside it; the pickup is sent from the east one, where the walker can put the account.
	DigTrayStand = tile.Tile{X: 3370, Z: 3378}
	// DigMainWinch and DigPrivateWinch are the winches (each 1x2: the main one covers 3352..3353 by 3417, the private one 3370 by
	// 3428..3429).
	DigMainWinch    = tile.Tile{X: 3352, Z: 3417}
	DigPrivateWinch = tile.Tile{X: 3370, Z: 3428}
	// DigEastLanding and DigWestLanding are where the winches set the account down (0_52_153_42_36 and 0_52_153_25_26); DigAltarLanding is
	// where the blast leaves it (0_52_152_40_39) and DigAltarEastLanding and DigAltarWestLanding where the winches put an account that has
	// passed the blast (0_52_152_42_36 and 0_52_152_25_26).
	DigEastLanding       = tile.Tile{X: 3370, Z: 9828}
	DigWestLanding       = tile.Tile{X: 3353, Z: 9818}
	DigAltarLanding      = tile.Tile{X: 3368, Z: 9767}
	DigAltarEastLanding  = tile.Tile{X: 3370, Z: 9764}
	DigAltarWestLanding  = tile.Tile{X: 3353, Z: 9754}
	DigCaveWorkmanAnchor = tile.Tile{X: 3352, Z: 9821}
	// DigRootSpawns are the west shaft's two arcenia roots (m52_153 "0 20 27: 708" and "0 23 32: 708").
	DigRootSpawns = []tile.Tile{{X: 3348, Z: 9819}, {X: 3351, Z: 9824}}
	// DigBrickStand is the one tile the blast accepts, north of the bricks (loc 2362 at 3379,9825): digsite_blockage_run_sequence compares the
	// coordinate (0_52_153_51_34) before it does anything.
	DigBrickStand = tile.Tile{X: 3379, Z: 9826}
	// DigTabletSpawn is the stone tablet (m52_152 "0 46 19: 699").
	DigTabletSpawn = tile.Tile{X: 3374, Z: 9747}
	// DigAltarRope is the altar cave's rope out (loc 2353 at 3370,9763), beside the landing of the main winch.
	DigAltarRope = tile.Tile{X: 3370, Z: 9763}
)

// DigZone is a dig site: the soil locs of its rectangle, and the tile to stand on to start.
type DigZone struct {
	MinX, MaxX, MinZ, MaxZ int
	Stand                  tile.Tile
}

// Contains reports a tile of the zone.
func (z DigZone) Contains(t tile.Tile) bool {
	return t.Level == 0 && t.X >= z.MinX && t.X <= z.MaxX && t.Z >= z.MinZ && t.Z <= z.MaxZ
}

// The zones are the content's own: area_digsite.rs2 picks the exam level from inzone() on the soil's coordinate (0_52_53_22_12 to
// 0_52_53_29_20 is level 3, 0_52_53_24_4 to 0_52_53_29_8 the training site).
var (
	DigLevel3Zone   = DigZone{MinX: 3350, MaxX: 3357, MinZ: 3404, MaxZ: 3412, Stand: tile.Tile{X: 3353, Z: 3408}}
	DigTrainingZone = DigZone{MinX: 3352, MaxX: 3357, MinZ: 3396, MaxZ: 3400, Stand: tile.Tile{X: 3354, Z: 3397}}
)

// DigRegion is where the account stands relative to the shafts.
type DigRegion string

const (
	// DigSurface is anywhere the walker reaches.
	DigSurface DigRegion = "surface"
	// DigShaftEast is the main winch's shaft before the blast (189 tiles; the bricks).
	DigShaftEast DigRegion = "east shaft"
	// DigShaftWest is the private winch's shaft (52 tiles; the cave workman and the roots).
	DigShaftWest DigRegion = "west shaft"
	// DigAltarEast is the cave the blast opens (426 tiles; the tablet and ten skeletons).
	DigAltarEast DigRegion = "altar cave"
	// DigAltarWest is the private winch's shaft after the blast (52 tiles).
	DigAltarWest DigRegion = "altar cave's west shaft"
)

// The rectangles are tight on the real pack (a flood from each landing: 189, 52, 426 and 52 tiles, the first spanning x 3364..3390 by
// z 9826..9853), and disjoint.
func inDigRect(t tile.Tile, minX, maxX, minZ, maxZ int) bool {
	return t.Level == 0 && t.X >= minX && t.X <= maxX && t.Z >= minZ && t.Z <= maxZ
}

// InDigShaftEast reports a tile of the east shaft before the blast.
func InDigShaftEast(t tile.Tile) bool { return inDigRect(t, 3361, 3394, 9820, 9856) }

// InDigShaftWest reports a tile of the west shaft before the blast.
func InDigShaftWest(t tile.Tile) bool { return inDigRect(t, 3344, 3360, 9812, 9828) }

// InDigAltarEast reports a tile of the altar cave.
func InDigAltarEast(t tile.Tile) bool { return inDigRect(t, 3361, 3394, 9736, 9792) }

// InDigAltarWest reports a tile of the west shaft after the blast.
func InDigAltarWest(t tile.Tile) bool { return inDigRect(t, 3344, 3360, 9746, 9764) }

// InDigCave reports a tile of any of the four.
func InDigCave(t tile.Tile) bool {
	return InDigShaftEast(t) || InDigShaftWest(t) || InDigAltarEast(t) || InDigAltarWest(t)
}

// DigRegionOf is the region a tile is in.
func DigRegionOf(t tile.Tile) DigRegion {
	switch {
	case InDigShaftEast(t):
		return DigShaftEast
	case InDigShaftWest(t):
		return DigShaftWest
	case InDigAltarEast(t):
		return DigAltarEast
	case InDigAltarWest(t):
		return DigAltarWest
	}
	return DigSurface
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomDigPickpocket    = "dig-pickpocket"     // Item: what to steal toward, Qty: how many
	CustomDigBush          = "dig-bush"           // the purple student's sample
	CustomDigSacks         = "dig-sacks"          // the specimen jar
	CustomDigTray          = "dig-tray"           // the panning tray
	CustomDigPan           = "dig-pan"            // Item: the find to pan for
	CustomDigGiveSample    = "dig-give-sample"    // Item: the sample, Target: the student
	CustomDigStudentGreen  = "dig-student-green"  // a talk with the green student, for the second exam
	CustomDigStudentOrange = "dig-student-orange" // a talk with the orange student, for the second exam
	CustomDigStudentPurple = "dig-student-purple" // a talk with the purple student, for the second exam
	// The third exam's talks are their own steps: the engine remembers a step that ran by its name for the whole run, and the second exam's
	// talks of the same run (the errands are reset when an exam is passed) must not stand for these.
	CustomDigStudentGreen3  = "dig-student-green-3"
	CustomDigStudentOrange3 = "dig-student-orange-3"
	CustomDigStudentPurple3 = "dig-student-purple-3"
	CustomDigCutOpal        = "dig-cut-opal"
	CustomDigTalisman       = "dig-talisman" // dig the level 3 site for the talisman
	CustomDigCharcoal       = "dig-charcoal" // dig the training site for charcoal
	CustomDigUseOnExpert    = "dig-expert"   // Item: the thing the expert is shown
	CustomDigLostLetter     = "dig-lost-letter"
	CustomDigGiveScroll     = "dig-give-scroll" // the invitation letter to a workman
	CustomDigWest           = "dig-west"        // the private winch, the cave workman and the root
	CustomDigChest          = "dig-chest"
	CustomDigBarrel         = "dig-barrel"
	CustomDigMix            = "dig-mix" // Item and Target: the two ingredients
	CustomDigBlast          = "dig-blast"
	CustomDigTablet         = "dig-tablet"
	CustomDigLeave          = "dig-leave"

	KitDigBundle = "dig-bundle" // plus the stage
	KitDigChem   = "dig-chem"
	KitDigTea    = "dig-tea"
	KitDigChisel = "dig-chisel"
	KitDigBox    = "dig-tinderbox"
	KitDigFood   = "dig-food"
	// KitDigPocketFood is the food for the workmen's pockets (digPocketFood), apart from the altar cave's.
	KitDigPocketFood = "dig-pocket-food"
)

// Parks: how long the quest skips an account that cannot go on and has no way to mend it by trying again.
const (
	// DigSkillParkFor is a level the content gates on (Thieving, Agility, Herblore): levels do not change in an hour.
	DigSkillParkFor = 6 * time.Hour
	// DigItemParkFor is a kit no source covers (the mule's stock changes by the hour, a shop's by the minute).
	DigItemParkFor = 3 * time.Hour
	// DigAltarParkFor is a cave the model refuses.
	DigAltarParkFor = 6 * time.Hour
)

// digFoodIDs are the foods the quest carries and eats (the Sustain's), best first.
var digFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// digSustain is the quest's survival policy: the foods it carries and the line its eat loop never goes under. The only thing in the
// quest that bites is the altar cave's skeletons (hit up to 3), so the line is four of their hits.
var digSustain = Sustain{FoodIDs: digFoodIDs, EatAt: 12}

// digVolatile are the objects that explode when dropped (opheld5 in area_digsite.rs2); they are never dropped, banked by a
// deposit that does not name them, or used on anything but their one recipe.
var digVolatile = []int{DigObjLiquid, DigObjNitro, DigObjPreMix, DigObjPostMix, DigObjCompound}

// DigVolatileIDs returns the explosive objects (a copy).
func DigVolatileIDs() []int { return append([]int(nil), digVolatile...) }

// digTools are the quest's own items and its coins: what the floor bite never eats and every bank visit of the quest leaves in the pack.
var digTools = []int{
	CoinsID, DigObjSpecimenJar, DigObjSpecimenBrush, DigObjSampleGreen, DigObjSampleOrange, DigObjSamplePurple, DigObjTrowel,
	DigObjTrayEmpty, DigObjTrayMud, DigObjTalisman, DigObjPlainLetter, DigObjStampedLetter, DigObjCert1, DigObjCert2, DigObjCert3,
	DigObjExpertScroll, DigObjStoneTablet, DigObjPowder, DigObjNitrate, DigObjLiquid, DigObjNitro, DigObjGroundCharcoal, DigObjPreMix,
	DigObjPostMix, DigObjCompound, DigObjArceniaRoot, DigObjChestKey, DigObjVial, DigObjPestle, DigObjTinderbox, DigObjCharcoal,
	DigObjRope, DigObjChisel, DigObjOpal, DigObjUncutOpal, DigObjTea,
}

// digKeep is what every bank step of the quest leaves in the pack: the tools and the food.
var digKeep = append(append([]int(nil), digTools...), digFoodIDs...)

// DigKeepIDs returns the keep list (a copy).
func DigKeepIDs() []int { return append([]int(nil), digKeep...) }

// DigToolIDs returns the module's tools (a copy).
func DigToolIDs() []int { return append([]int(nil), digTools...) }

// digJunk are the ids the module may drop to make room, and nothing else: the pocket's and the dig's loot (area_digsite.rs2's tables and
// digworkman1's), none of it a tool of any plan. See digVolatile for why the list is allow, not deny.
var digJunk = []int{
	952,  // spade
	1059, // leather gloves
	1061, // leather boots
	1925, // bucket
	526,  // bones
	687,  // broken arrow
	694,  // pottery
	688,  // buttons
	710,  // old vase
	690,  // broken glass
	686,  // rusty sword
	689,  // broken staff
	697,  // old armour
	698,  // old armour
	684,  // belt buckle
	695,  // old tooth
	685,  // old boot
	1984, // rotten apple
	436,  // copper ore
	434,  // clay
	1627, // uncut jade
	407,  // oyster shell
	1151, // black medium helm
	1237, // bronze spear
	863,  // iron knife
	1733, // needle
	2313, // pie dish
	1203, // iron dagger
	1923, // bowl
	1935, // jug
	1931, // pot
	674,  // cracked sample
	680,  // gold nuggets
	1633, // crushed gemstone
	444,  // gold ore
}

// DigJunkIDs returns the ids the module may drop (a copy).
func DigJunkIDs() []int { return append([]int(nil), digJunk...) }

// DigKeepLimit is how many of an object the module keeps; one more than this is surplus a pocket's pickings leave (a pocket turns up five
// specimen brushes and three ropes on the way to one sample).
var DigKeepLimit = map[int]int{DigObjSpecimenBrush: 1, DigObjSpecimenJar: 1, DigObjTrowel: 1, DigObjRope: 2}

// DigRopesNeeded is the ropes the two winches take (one tie each, once: the bit is the account's for good).
const DigRopesNeeded = 2

// The NPC stops. A talk step must name a preference, so the stops with no menu carry a placeholder.
var (
	// DigExaminerStartStop starts the quest: "Can I take an exam?" / "Interesting...".
	DigExaminerStartStop = NpcStop{Npc: "Examiner", ID: DigNpcExaminer, Anchor: DigExaminerAnchor, Leash: 10,
		Prefer: []string{"Can I take an exam?"}}
	// DigExaminerLostLetterStop asks for another letter at stage 1 ("I have lost the letter you gave me." / "Alright I'll try and get it.").
	DigExaminerLostLetterStop = NpcStop{Npc: "Examiner", ID: DigNpcExaminer, Anchor: DigExaminerAnchor, Leash: 10,
		Prefer: []string{"I have lost the letter you gave me."}}
	// DigExaminerLetterStop hands over the stamped letter: no menu, then the first exam starts at once with every errand unanswered, so
	// each question offers only wrong answers and the stop takes the first (digExamFallbacks).
	DigExaminerLetterStop = NpcStop{Npc: "Examiner", ID: DigNpcExaminer, Anchor: DigExaminerAnchor, Leash: 10,
		Prefer: append(append([]string(nil), DigExamAnswers...), digExamFallbacks...)}
	// DigExaminerExamStop sits an exam: "Yes, I certainly am." (stage 2), "I am ready for the next exam." (3), "I am ready for the last
	// exam..." (4), then the nine questions.
	DigExaminerExamStop = NpcStop{Npc: "Examiner", ID: DigNpcExaminer, Anchor: DigExaminerAnchor, Leash: 10,
		Prefer: append(append([]string{"Yes, I certainly am.", "I am ready for the next exam.", "I am ready for the last exam"}, DigExamAnswers...), digExamFallbacks...)}
	// DigExaminerTrowelStop asks for another trowel: "I have lost my trowel." is the fourth line of the stage 3 and 4 menus; from stage 5
	// on the Examiner offers one with no menu when the pack holds none.
	DigExaminerTrowelStop = NpcStop{Npc: "Examiner", ID: DigNpcExaminer, Anchor: DigExaminerAnchor, Leash: 10,
		Prefer: []string{"I have lost my trowel."}}
	// DigCuratorStop stamps the plain letter the moment the talk opens (curator_stamp_letter runs before the menu): no menu.
	DigCuratorStop = NpcStop{Npc: "Curator", ID: DigNpcCurator, Anchor: DigCuratorAnchor, Leash: 8, Prefer: []string{"Have you any interesting news?"}}
	// DigGreenStop, DigOrangeStop and DigPurpleStop are the students: none of their talks has a menu.
	DigGreenStop  = NpcStop{Npc: "Student", ID: DigNpcGreen, Anchor: DigStudentsAnchor, Leash: 16, Prefer: []string{"Hello there."}}
	DigOrangeStop = NpcStop{Npc: "Student", ID: DigNpcOrange, Anchor: DigStudentsAnchor, Leash: 16, Prefer: []string{"Hello there."}}
	DigPurpleStop = NpcStop{Npc: "Student", ID: DigNpcPurple, Anchor: DigStudentsAnchor, Leash: 16, Prefer: []string{"Hello there."}}
	// DigGuideStop is the panning guide's gate: "OK, forget it." / "So how do I become invited then?" after a refused pan.
	DigGuideStop = NpcStop{Npc: "Panning guide", ID: DigNpcGuide, Anchor: DigGuideAnchor, Leash: 8,
		Prefer: []string{"So how do I become invited then?", "Thank you!"}}
	// DigExpertLetterStop is the expert's menu with the lost letter's line, which exists only while the account has been shown to him and holds
	// no letter ("No thanks." closes the other cases).
	DigExpertLetterStop = NpcStop{Npc: "Archaeological expert", ID: DigNpcExpert, Anchor: DigExpertAnchor, Leash: 8,
		Prefer: []string{"I lost the letter you gave me.", "No thanks."}}
)

// DigKeyPleas are the cave workman's menus in the order the content gives them (digworkman2_cave_default and digworkman1_usekey .. please3):
// he says he "needs" the key, and gives it after four pleas. The menus offer a wrong line beside each ("Can I buy it from you?", "Hey that's my
// key!"), and none of these is a substring of a wrong one.
var DigKeyPleas = []string{
	"I have been invited to research here.", "Do you know where to find a chest key?", "I don't suppose I could use it?", "Aww... go on.",
	"Pretty please with sugar on top!", "Pretty please!", "Please?",
}

// DigExamAnswers are the right answers of the nine questions, in the content's own words (examiner.rs2). A question offers its right
// answer only once the errand it belongs to is answered: exam 1 takes the green student's, the orange student's and the purple student's
// errand for its three questions in that order of the script (student1, student3, student2); exam 2 the same three; exam 3 the purple, the
// green and the orange student's.
var DigExamAnswers = []string{
	// Earth Sciences level 1.
	"The study of the earth, its contents, and history.",
	"All that have passed the appropriate Earth Sciences exam.",
	"Gloves and boots to be worn at all times; proper tools must be used.",
	// Earth Sciences level 2.
	"Samples taken in rough form; kept only in sealed containers.",
	"Finds must be carefully handled, and gloves worn.",
	"Always handle with care; strike cleanly on its cleaving point.",
	// Earth Sciences level 3.
	"Samples cleaned, and carried only in specimen jars.",
	"Brush carefully and slowly using short strokes.",
	"Handle bones very carefully and keep them away from other samples.",
}

// digExamFallbacks are one option of every question's wrong-only menu (the menu an unanswered errand shows): taken after the right
// answers, so that an exam sat too early, or begun at stage 1 by the stamped letter, is finished and not left half answered at a menu.
// No right answer is offered by a wrong-only menu, so none of these ever pre-empts one.
var digExamFallbacks = []string{
	"The study of gardening", "Magic users, miners and their escorts.", "Heat-resistant clothing",
	"Samples cut and cleaned before transportation.", "Finds must not be handled by anyone.", "Strike rock repeatedly until powdered.",
	"Samples may be mixed together safely.", "Brush quickly using a wet brush.", "Bones must not be taken from the digsite.",
}

// The journal flags of a snapshot's Stage (see DigRaw).
const (
	// DigFlagGreen, DigFlagPurple and DigFlagOrange: the errand of that student is answered (stage 2's page says so; later stages' pages
	// print the answered and unanswered lines alike once the colour tags are gone, so the flags are only set at stage 2).
	DigFlagGreen = 1 << iota
	DigFlagPurple
	DigFlagOrange
	// DigFlagReady: "I should talk to the Examiner to take my ... exam" is on the page, which only prints once all three errands are answered.
	DigFlagReady
	// DigFlagOpal: the purple student has asked for an opal (stage 4) and has not been given it.
	DigFlagOpal
)

// DigRaw packs a stage and its flags into the integer ReadStage returns (the engine compares and prints it): the stage in the low four
// bits, the flags above.
func DigRaw(stage, flags int) int { return flags<<4 | stage }

// DigStageOf is the stage of a snapshot's Stage.
func DigStageOf(raw int) int { return raw & 15 }

// DigFlagsOf is the flags of a snapshot's Stage.
func DigFlagsOf(raw int) int { return raw >> 4 }

// digMarkers are the journal's stage markers, newest stage first (itexam_journal.rs2 keeps every earlier paragraph on the page, struck
// through, so the newest marker present decides).
var digMarkers = []struct {
	stage int
	text  string
}{
	{DigComplete, "quest complete!"},
	{DigRemovedBlockage, "interesting find in the secret room"},
	{DigPouredCompound, "ignite the explosive compound"},
	{DigMineshaftPermit, "move the rocks blocking the way"},
	{DigImpressExpert, "letter to a workman near a"},
	{DigThirdExam, "study for my third exam"},
	{DigSecondExam, "study for my second exam"},
	{DigFirstExam, "study for my first exam"},
	{DigStamping, "curator of the varrock museum"},
	{DigNotStarted, "i can start this quest by speaking to"},
}

// digFlagMarkers are the flags' lines. The green and orange students' answered lines read alike, so each needle carries the line above
// it; the purple student's is "she gave me", which "he gave me" is a part of, so it is looked for on its own.
var digFlagMarkers = []struct {
	flag int
	text string
}{
	{DigFlagReady, "i should talk to the examiner to take my"},
	{DigFlagGreen, "green top about the exams. he gave me an answer"},
	{DigFlagOrange, "orange top about the exams. he gave me an answer"},
	{DigFlagPurple, "she gave me an answer"},
	{DigFlagOpal, "i need to bring her an opal"},
}

// ParseDigsiteJournal reads %itexamlevel and the errands' flags from the rendered journal (DigRaw). ok is false for an empty or
// unrecognised page, which is unknown, never stage 0.
func ParseDigsiteJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range digMarkers {
		if !strings.Contains(text, m.text) {
			continue
		}
		flags := 0
		// The flags are only meaningful while an exam is being studied for; after that the page keeps the old lines struck through.
		if m.stage >= DigFirstExam && m.stage <= DigThirdExam {
			for _, f := range digFlagMarkers {
				if strings.Contains(text, f.text) {
					flags |= f.flag
				}
			}
			if m.stage != DigFirstExam {
				flags &^= DigFlagGreen | DigFlagPurple | DigFlagOrange
			}
			if m.stage != DigThirdExam {
				flags &^= DigFlagOpal
			}
		}
		return DigRaw(m.stage, flags), true
	}
	return 0, false
}

// digSnap is a snapshot's stage and flags.
func digSnap(s Snapshot) (stage, flags int) { return DigStageOf(s.Stage), DigFlagsOf(s.Stage) }

// DigAltarFit prices the altar cave: ten skeletons (level 25, hit up to 3 every 4 ticks) in a 426-tile cave, hunting an account of combat 50
// or less within a tile, two of them taken to be on the account for the DigAltarTicks of the visit. An account above the ceiling is not
// hunted and goes in as it is. Below it the visit is refused unless the hitpoints above two hits of each plus the food in the pack
// cover twice the priced damage (food is eaten in a cave: there is no chat in the visit but the tablet's pickup, which has none). An
// unread level passes, as every other gate here treats an unread stat.
func DigAltarFit(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 || !demonHunted(s, DigSkeletonCeiling) {
		return true, ""
	}
	f := FoeManorSkeleton
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(DigSkeletonsEngaged) * float64(DigAltarTicks) / float64(f.AttackTicks) * per
	room := float64(s.MaxHP - DigSkeletonsEngaged*2*f.PeakHit())
	pool := float64(FoodPoolHeal(s.InvIDs, digFoodIDs))
	if room+pool < 2*dmg {
		return false, fmt.Sprintf("not going into the altar cave with this pack: its ten skeletons (level 25, hits up to %d) hunt every account of combat %d and under, this account is combat %d, "+
			"%d of them over a visit of %d ticks cost an account at Defence %d about %.0f hitpoints, and %d hitpoints less %d hits plus %.0f of food is %.0f, short of twice that",
			f.PeakHit(), DigSkeletonCeiling, DemonCombat(s), DigSkeletonsEngaged, DigAltarTicks, s.Level("defence"), dmg, s.MaxHP, DigSkeletonsEngaged*2, pool, room+pool)
	}
	return true, ""
}

// The altar cave's pricing: the skeletons' ceiling is twice their level (25) and the combat level of the account must be over it; two are
// taken to be on the account at once for the visit's ticks (the landing is 26 tiles from the tablet and the rope 24 from it, a pickup and
// the walks between, with a margin).
const (
	DigSkeletonCeiling  = 2 * 25
	DigSkeletonsEngaged = 2
	DigAltarTicks       = 80
)

// The pocket's price in hitpoints. A miss on a workman costs one hitpoint (pickpocket_digworkman1: damage_self(1), and damage_self can kill)
// and stuns for eight ticks, and the hitpoint timer (health_regen, 100 ticks) gives one back a minute, so the pocket loop is a steady drain
// it cannot out-wait. At Thieving 25 a pocket is picked about one time in two, and a rope is one in eleven of the picks: a rope costs ten
// misses on average and thirty one time in twenty. The loop therefore never steals at or under a floor of its hitpoints (it eats there if it
// has food, and parks the quest to regain them if it has none), and an account whose hitpoints do not cover a bad rope is given a little food
// before the pocket is picked (digPocketFood).
const (
	// DigPocketMinHP is the fewest hitpoints the pocket loop steals at: the floor is never lower, so a miss at the floor leaves three.
	DigPocketMinHP = 4
	// DigPocketFloorPercent is the floor as a share of the maximum hitpoints (rounded up), when that is more than DigPocketMinHP.
	DigPocketFloorPercent = 25
	// DigPocketResumePercent is how much of the maximum an account without food starts a stretch of pocketing with after a rest (rounded
	// up), and DigPocketResumeMargin the least it is over the floor: the hysteresis that keeps a rested account from parking again at once.
	DigPocketResumePercent = 50
	DigPocketResumeMargin  = 3
	// DigPocketCover is the misses an account's hitpoints over the floor must cover for it to pick pockets without food: a rope is thirty
	// misses one time in twenty.
	DigPocketCover = 30
	// DigPocketRegen is the time the engine's health_regen timer takes to give one hitpoint back (100 ticks).
	DigPocketRegen = time.Minute
	// DigPocketRestMin and DigPocketRestMax bound the park of an account that rests to regain hitpoints.
	DigPocketRestMin = 5 * time.Minute
	DigPocketRestMax = 30 * time.Minute
)

// DigPocketFloor is the hitpoints at or under which the pocket loop does not steal: a quarter of the maximum, at least DigPocketMinHP,
// and always under the maximum. 0 for an unread maximum.
func DigPocketFloor(maxHP int) int {
	if maxHP <= 0 {
		return 0
	}
	floor := max(DigPocketMinHP, (maxHP*DigPocketFloorPercent+99)/100)
	return min(floor, maxHP-1)
}

// DigPocketResume is the hitpoints an account without food starts a stretch of pocketing at, after a rest: half the maximum, at least
// DigPocketResumeMargin over the floor, and never over the maximum. 0 for an unread maximum.
func DigPocketResume(maxHP int) int {
	if maxHP <= 0 {
		return 0
	}
	return min(maxHP, max(DigPocketFloor(maxHP)+DigPocketResumeMargin, (maxHP*DigPocketResumePercent+99)/100))
}

// DigPocketCovered reports that an account's hitpoints over the floor take a bad rope's misses without food (DigPocketCover). An unread
// maximum passes, as every other gate here treats an unread stat.
func DigPocketCovered(maxHP int) bool {
	return maxHP <= 0 || maxHP-DigPocketFloor(maxHP) >= DigPocketCover
}

// DigPocketAdvice is what the pocket loop does about its hitpoints.
type DigPocketAdvice int

const (
	// DigPocketGo steals on.
	DigPocketGo DigPocketAdvice = iota
	// DigPocketEat eats a bite of food first.
	DigPocketEat
	// DigPocketRest parks the quest to regain hitpoints: the account has no food to eat.
	DigPocketRest
)

func (a DigPocketAdvice) String() string {
	switch a {
	case DigPocketGo:
		return "go"
	case DigPocketEat:
		return "eat"
	case DigPocketRest:
		return "rest"
	}
	return fmt.Sprintf("DigPocketAdvice(%d)", int(a))
}

// DigPocketJudge decides the pocket loop's next move from its hitpoints, its maximum and what its food heals (FoodPoolHeal of the pack).
// At or under the floor it eats, or rests when it has nothing to eat. entering is the start of a stretch: an account with no food starts
// one only from DigPocketResume up, so that the rest that ended a stretch is not undone by a single hitpoint. An unread maximum goes on.
func DigPocketJudge(hp, maxHP, foodHeal int, entering bool) DigPocketAdvice {
	if maxHP <= 0 {
		return DigPocketGo
	}
	if hp > DigPocketFloor(maxHP) {
		if !entering || hp >= DigPocketResume(maxHP) || foodHeal > 0 {
			return DigPocketGo
		}
		return DigPocketRest
	}
	if foodHeal > 0 {
		return DigPocketEat
	}
	return DigPocketRest
}

// DigPocketRestFor is how long a resting account is parked for: the hitpoints it lacks to DigPocketResume at a minute each, and one more
// minute, between DigPocketRestMin and DigPocketRestMax.
func DigPocketRestFor(hp, maxHP int) time.Duration {
	need := max(DigPocketResume(maxHP)-hp, 1)
	return min(DigPocketRestMax, max(DigPocketRestMin, time.Duration(need+1)*DigPocketRegen))
}

// digPocketFoodKit is a little food for the workmen's pockets: up to three sharks and three bass, whatever the bank and the mule have,
// optional so that a short bank blocks nothing (the pocket loop's own floor does the rest).
func digPocketFoodKit() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: 3, Optional: true}, {ID: ObjBass, Name: "Bass", Qty: 3, Optional: true}}
}

// digPocketFood puts a food kit (once a run) in front of a step that picks pockets when the account's hitpoints do not cover a bad rope
// and it carries nothing to eat: the pocket step itself, and the three that go down a winch (a rope is a pocket's loot, tied once, and the
// plan cannot see whether it already was). The kit is its own, so the altar cave's later food kit (KitDigFood) is not spent by it.
func digPocketFood(s Snapshot, step Step, region DigRegion) (Step, bool) {
	if region != DigSurface || step.Kind != StepCustom || DigPocketCovered(s.MaxHP) {
		return Step{}, false
	}
	switch step.Custom {
	case CustomDigPickpocket:
	case CustomDigWest, CustomDigBlast, CustomDigTablet:
		if s.HeldID(DigObjRope) > 0 {
			return Step{}, false
		}
	default:
		return Step{}, false
	}
	if FoodPoolHeal(s.InvIDs, digFoodIDs) > 0 {
		return Step{}, false
	}
	return digOnce(s, KitDigPocketFood, digPocketFoodKit(), fmt.Sprintf("food for the workmen's pockets: a miss costs a hitpoint and this account has %d", s.MaxHP))
}

// digTalk is a talk step.
func digTalk(stop NpcStop, reason string) Step {
	return Step{Kind: StepTalk, Stop: stop, Reason: reason}
}

// digCustom is a custom step.
func digCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

// digCustomItem is a custom step about one object.
func digCustomItem(name string, item, qty int, reason string) Step {
	return Step{Kind: StepCustom, Custom: name, ItemID: item, Qty: qty, Reason: reason}
}

func digWait(format string, args ...any) Step {
	return Step{Kind: StepWait, Reason: fmt.Sprintf(format, args...)}
}

func digPark(d time.Duration, format string, args ...any) Step {
	return Step{Kind: StepWait, ParkFor: d, Reason: fmt.Sprintf(format, args...)}
}

// digBundle is the first pass of a stage: everything the stage will want that the bank or the mule can give, in one visit and one ask.
// Every line is optional and carries no source of its own (a source on an optional line is worked), so a short bank never blocks it. The
// quest items are untradeable (never asked of the mule); the Stage 5 trainer banks what it does not keep between two runs, so this is how
// a letter, a certificate or a half-made chemical comes back into the pack.
func digBundle(s Snapshot, stage int) []KitLine {
	var kit []KitLine
	add := func(id int, name string, qty int, untradeable bool) {
		if s.HeldID(id) >= qty {
			return
		}
		kit = append(kit, KitLine{ID: id, Name: name, Qty: qty, Optional: true, Untradeable: untradeable})
	}
	switch stage {
	case DigStamping:
		add(DigObjStampedLetter, "Stamped letter", 1, true)
		add(DigObjPlainLetter, "Unstamped letter", 1, true)
	case DigSecondExam, DigThirdExam:
		add(DigObjTrowel, "Trowel", 1, true)
		add(DigObjChisel, "Chisel", 1, false)
		add(DigObjOpal, "Opal", 1, false)
		add(DigObjUncutOpal, "Uncut opal", 1, false)
	case DigImpressExpert:
		add(DigObjTrowel, "Trowel", 1, true)
		add(DigObjSpecimenJar, "Specimen jar", 1, false)
		add(DigObjSpecimenBrush, "Specimen brush", 1, false)
		add(DigObjTalisman, "Talisman of zaros", 1, true)
		add(DigObjExpertScroll, "Invitation letter", 1, true)
	case DigMineshaftPermit:
		add(DigObjTrowel, "Trowel", 1, true)
		add(DigObjRope, "Rope", DigRopesNeeded, false)
		add(DigObjVial, "Vial", 1, false)
		add(DigObjPestle, "Pestle and mortar", 1, false)
		add(DigObjTinderbox, "Tinderbox", 1, false)
		add(DigObjCharcoal, "Charcoal", 1, false)
		add(DigObjGroundCharcoal, "Ground charcoal", 1, false)
		add(DigObjArceniaRoot, "Arcenia root", 1, true)
		add(DigObjChestKey, "Chest key", 1, true)
		add(DigObjPowder, "Chemical powder", 1, true)
		add(DigObjNitrate, "Ammonium nitrate", 1, true)
		add(DigObjLiquid, "Unidentified liquid", 1, true)
		add(DigObjNitro, "Nitroglycerin", 1, true)
		add(DigObjPreMix, "Mixed chemicals", 1, true)
		add(DigObjPostMix, "Mixed chemicals", 1, true)
		add(DigObjCompound, "Chemical compound", 1, true)
	case DigPouredCompound:
		add(DigObjTinderbox, "Tinderbox", 1, false)
		add(DigObjRope, "Rope", 1, false)
	case DigRemovedBlockage:
		add(DigObjStoneTablet, "Stone tablet", 1, true)
	}
	return kit
}

// digShop is a shop source at a shop.
func digShop(shop ShopRef, price int, note string) []Source {
	return []Source{{Kind: SourceShop, Shop: shop, Price: price, Note: note}}
}

// DigJatixShop is Jatix's Herblore Store in Taverley (vials, 800 on the shelf, and the pestle and mortar, 3): the nearest counter to Varrock
// that stocks either (Entrana, Karamja, Shilo and the Fremennik village are farther).
var DigJatixShop = PlagueJatixShop

// DigTeaShop is the tea seller's stall in Varrock (20 cups, one more every 50 ticks).
var DigTeaShop = ShopRef{Npc: "Tea seller", NpcID: DigNpcTeaSeller, Anchor: tile.Tile{X: 3271, Z: 3412}}

// digTeaKit is the cup of tea the panning guide asks for.
func digTeaKit() []KitLine {
	return []KitLine{{ID: DigObjTea, Name: "Cup of tea", Qty: 1, Sources: digShop(DigTeaShop, 10, "the tea seller in Varrock keeps 20")}}
}

// digChiselKit is the chisel the opal is cut with.
func digChiselKit() []KitLine {
	return []KitLine{{ID: DigObjChisel, Name: "Chisel", Qty: 1, Sources: digShop(VarrockGenStore, 2, "Varrock's general store keeps 2")}}
}

// digBoxKit is the tinderbox the blast is lit with (the mule keeps a few).
func digBoxKit() []KitLine {
	return []KitLine{{ID: DigObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: digShop(VarrockGenStore, 2, "Varrock's general store keeps 2")}}
}

// digChemKit is what stage 6 buys: the vial the barrel is dipped with and the pestle and mortar the charcoal is ground with (Jatix's
// counter has both, so one trip), and the tinderbox. Each line only while the chemistry still needs it.
func digChemKit(s Snapshot) []KitLine {
	var kit []KitLine
	if digNeedsVial(s) && s.HeldID(DigObjVial) == 0 {
		kit = append(kit, KitLine{ID: DigObjVial, Name: "Vial", Qty: 1, Sources: digShop(DigJatixShop, 2, "Jatix's Herblore Store in Taverley keeps 800")})
	}
	if digNeedsPestle(s) && s.HeldID(DigObjPestle) == 0 {
		kit = append(kit, KitLine{ID: DigObjPestle, Name: "Pestle and mortar", Qty: 1, Sources: digShop(DigJatixShop, 4, "Jatix's Herblore Store in Taverley keeps 3")})
	}
	if s.HeldID(DigObjTinderbox) == 0 {
		kit = append(kit, KitLine{ID: DigObjTinderbox, Name: "Tinderbox", Qty: 1, Sources: digShop(VarrockGenStore, 2, "Varrock's general store keeps 2")})
	}
	return kit
}

// digFoodKit is a little food for the altar cave: the bank and the mule only.
func digFoodKit() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: 6, Optional: true}, {ID: 373, Name: "Swordfish", Qty: 6, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 6, Optional: true}, {ID: 365, Name: "Bass", Qty: 6, Optional: true}}
}

// digOnce provisions a kit once a run: never again after it has run or failed (a failed kit is not retried at once; a blocked one parks).
func digOnce(s Snapshot, name string, kit []KitLine, reason string) (Step, bool) {
	if len(kit) == 0 || s.Ran["provision:"+name] || s.Failed["provision:"+name] {
		return Step{}, false
	}
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: digKeep, Bank: VarrockEastBank, BankSet: true, Reason: reason}, true
}

// digKitOrPark provisions a required kit, and parks (for DigItemParkFor) once every source of it is dead.
func digKitOrPark(s Snapshot, name string, kit []KitLine, reason, because string) Step {
	if s.Blocked["provision:"+name] {
		return digPark(DigItemParkFor, "%s", because)
	}
	// A kit that failed on the way (a walk that timed out) is tried again; the watchdog parks it if it keeps failing.
	return Step{Kind: StepProvision, KitName: name, Kit: kit, KeepIDs: digKeep, Bank: VarrockEastBank, BankSet: true, Reason: reason}
}

// digTrowelStep is the trowel, from the bank when the bank holds one and else the Examiner's spare.
func digTrowelStep(s Snapshot) Step {
	if n, _ := s.BankedID(DigObjTrowel); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Trowel", Qty: 1, ID: DigObjTrowel}}, KeepIDs: digKeep, Bank: VarrockEastBank, BankSet: true,
			Reason: "the trowel the bank holds"}
	}
	return digTalk(DigExaminerTrowelStop, "the Examiner's spare trowel")
}

// Skills and recipes: which of the chemistry's parts the pack still needs a tool for.

func digMixed1(s Snapshot) bool {
	return s.HeldID(DigObjPreMix) > 0 || s.HeldID(DigObjPostMix) > 0 || s.HeldID(DigObjCompound) > 0
}

func digMixed2(s Snapshot) bool { return s.HeldID(DigObjPostMix) > 0 || s.HeldID(DigObjCompound) > 0 }

func digNitrateDone(s Snapshot) bool { return s.HeldID(DigObjNitrate) > 0 || digMixed1(s) }

func digNitroDone(s Snapshot) bool { return s.HeldID(DigObjNitro) > 0 || digMixed1(s) }

func digGroundDone(s Snapshot) bool { return s.HeldID(DigObjGroundCharcoal) > 0 || digMixed2(s) }

func digRootDone(s Snapshot) bool {
	return s.HeldID(DigObjArceniaRoot) > 0 || s.HeldID(DigObjCompound) > 0
}

// digNeedsVial: the barrel is still to be dipped.
func digNeedsVial(s Snapshot) bool { return !digNitroDone(s) && s.HeldID(DigObjLiquid) == 0 }

// digNeedsPestle: the charcoal is still to be ground.
func digNeedsPestle(s Snapshot) bool { return !digGroundDone(s) }

// digNeedsKey: the chest is still shut to this account.
func digNeedsKey(s Snapshot) bool {
	return s.HeldID(DigObjChestKey) == 0 && s.HeldID(DigObjPowder) == 0 && !digNitrateDone(s)
}

// digMixStep is the use of one ingredient on the other (the executor tries both orders: opheldu is declared on one of the pair).
func digMixStep(a, b int, reason string) Step {
	return Step{Kind: StepCustom, Custom: CustomDigMix, ItemID: a, TargetID: b, Reason: reason}
}

// digChemistry is stage 6: the order is the one that keeps every dig before the first explosive item exists (the charcoal is dug and ground
// first), and the west shaft first of all (it holds the key and the root). See the file comment.
func digChemistry(s Snapshot, region DigRegion) Step {
	if s.HeldID(DigObjCompound) > 0 {
		return digBlastStep(s, region, "pour the compound over the bricks and light it")
	}
	// Herblore first: every mix after the first wants level 10, and the first makes the explosive mixture that cannot be undone.
	if s.Level("herblore") > 0 && s.Level("herblore") < DigHerblore && !digMixed2(s) {
		return digPark(DigSkillParkFor, "the second and third mixes need Herblore %d (this account has %d): not starting the chemicals", DigHerblore, s.Level("herblore"))
	}
	if kit := digChemKit(s); len(kit) > 0 {
		return digKitOrPark(s, KitDigChem, kit, "the vial, the pestle and the tinderbox for the chemicals",
			"no vial, pestle or tinderbox from the bank, the mule, Jatix or Varrock's general store")
	}
	if digNeedsKey(s) || !digRootDone(s) {
		if s.Level("agility") > 0 && s.Level("agility") < DigAgility {
			return digPark(DigSkillParkFor, "the winch needs Agility %d to squeeze down the shaft (this account has %d)", DigAgility, s.Level("agility"))
		}
		return digCustom(CustomDigWest, "the west shaft: the chest key and an arcenia root")
	}
	if (digNeedsPestle(s) || digNeedsVial(s)) && s.HeldID(DigObjTrowel) == 0 {
		return digTrowelStep(s)
	}
	if !digGroundDone(s) {
		if s.HeldID(DigObjCharcoal) == 0 {
			return digCustom(CustomDigCharcoal, "dig the training site for charcoal")
		}
		return digMixStep(DigObjCharcoal, DigObjPestle, "grind the charcoal to a powder")
	}
	if !digNitrateDone(s) {
		if s.HeldID(DigObjPowder) == 0 {
			return digCustom(CustomDigChest, "unlock and search the chest for the powder")
		}
		return digCustomItem(CustomDigUseOnExpert, DigObjPowder, 1, "have the expert name the powder")
	}
	if !digNitroDone(s) {
		if s.HeldID(DigObjLiquid) == 0 {
			return digCustom(CustomDigBarrel, "lever the barrel open and fill a vial")
		}
		return digCustomItem(CustomDigUseOnExpert, DigObjLiquid, 1, "have the expert name the liquid")
	}
	if !digMixed1(s) {
		return digMixStep(DigObjNitrate, DigObjNitro, "mix the nitrate into the nitroglycerin")
	}
	if !digMixed2(s) {
		return digMixStep(DigObjPreMix, DigObjGroundCharcoal, "mix the ground charcoal in")
	}
	return digMixStep(DigObjPostMix, DigObjArceniaRoot, "mix the arcenia root in")
}

// digAltarGate is the altar cave's pricing as a step: nil when the pack can take the cave, a food kit once a run when more food would help,
// and a park when it would not. Underground, it is the way out first.
func digAltarGate(s Snapshot, region DigRegion) (Step, bool) {
	ok, why := DigAltarFit(s)
	if ok {
		return Step{}, false
	}
	if region != DigSurface {
		return digCustom(CustomDigLeave, "the pack cannot take the altar cave's skeletons: "+why), true
	}
	if st, once := digOnce(s, KitDigFood, digFoodKit(), "food for the altar cave's skeletons: "+why); once {
		return st, true
	}
	return digPark(DigAltarParkFor, "%s", why), true
}

// digBlastStep is the pour and the light (stage 6 with the compound, and 7): the altar cave the blast ends in is priced first, then the
// tinderbox, the winch's Agility, then the executor.
func digBlastStep(s Snapshot, region DigRegion, reason string) Step {
	if st, gated := digAltarGate(s, region); gated {
		return st
	}
	if s.HeldID(DigObjTinderbox) == 0 {
		if region != DigSurface {
			// Down the shaft without a tinderbox: the pour can be made, the light cannot; the executor climbs out for it if it must.
			return digCustom(CustomDigLeave, "up out of the shaft for a tinderbox")
		}
		return digKitOrPark(s, KitDigBox, digBoxKit(), "a tinderbox to light the compound", "no tinderbox from the bank, the mule or Varrock's general store")
	}
	if s.Level("agility") > 0 && s.Level("agility") < DigAgility && region == DigSurface {
		return digPark(DigSkillParkFor, "the winch needs Agility %d to squeeze down the shaft (this account has %d)", DigAgility, s.Level("agility"))
	}
	return digCustom(CustomDigBlast, reason)
}

// digPanStep is the river: the tray, then the pans, and the tea the guide asks for when his gate refused a pan.
func digPanStep(s Snapshot, goal int, reason string) Step {
	if s.HeldID(DigObjTrayEmpty)+s.HeldID(DigObjTrayMud) == 0 {
		return digCustom(CustomDigTray, "the panning tray")
	}
	if s.Failed[CustomDigPan] && s.HeldID(DigObjTea) == 0 {
		return digKitOrPark(s, KitDigTea, digTeaKit(), "a cup of tea: the panning guide refused the pan until he has one",
			"no cup of tea from the bank, the mule or the tea seller")
	}
	return digCustomItem(CustomDigPan, goal, 1, reason)
}

// digPickpocketStep steals toward an item. The content gates the pocket on Thieving 25 and says so only when it is tried.
func digPickpocketStep(s Snapshot, item, qty int, reason string) Step {
	if s.Level("thieving") > 0 && s.Level("thieving") < DigThieving {
		return digPark(DigSkillParkFor, "the workmen's pockets need Thieving %d (this account has %d)", DigThieving, s.Level("thieving"))
	}
	return digCustomItem(CustomDigPickpocket, item, qty, reason)
}

// digFirstExam is stage 2: the three samples are collected, then handed over in one trip to the camp, then the exam.
func digFirstExam(s Snapshot, flags int) Step {
	if flags&DigFlagReady != 0 {
		return digTalk(DigExaminerExamStop, "sit the first Earth Sciences exam")
	}
	type errand struct {
		flag    int
		item    int
		student int
		name    string
	}
	errands := []errand{
		{DigFlagGreen, DigObjSampleGreen, DigNpcGreen, "green"},
		{DigFlagPurple, DigObjSamplePurple, DigNpcPurple, "purple"},
		{DigFlagOrange, DigObjSampleOrange, DigNpcOrange, "orange"},
	}
	// Collect: nothing is handed over until every sample still wanted is in the pack, so the camp is walked to once.
	for _, e := range errands {
		if flags&e.flag != 0 || s.HeldID(e.item) > 0 {
			continue
		}
		switch e.item {
		case DigObjSampleGreen:
			return digPickpocketStep(s, e.item, 1, "the green student's rock sample is a pickpocket from a workman")
		case DigObjSamplePurple:
			return digCustom(CustomDigBush, "the purple student's rock sample lies in a bush")
		default:
			return digPanStep(s, e.item, "pan the river for the orange student's rock sample")
		}
	}
	for _, e := range errands {
		if flags&e.flag == 0 && s.HeldID(e.item) > 0 {
			return Step{Kind: StepCustom, Custom: CustomDigGiveSample, ItemID: e.item, TargetID: e.student, Reason: "return the " + e.name + " student's rock sample"}
		}
	}
	return digWait("the first exam's errands are answered and the journal does not say the exam is ready")
}

// digStudents is stages 3 and 4: a talk with each student once a run (every talk sets that student's errand), then the exam. At stage 4 the
// purple student wants an opal, which is fetched between her two talks. Each exam's talks have their own step names (the engine remembers a step
// that ran for the whole run, and the errands are reset when an exam is passed).
func digStudents(s Snapshot, stage, flags int) Step {
	if flags&DigFlagReady != 0 {
		if stage == DigSecondExam {
			return digTalk(DigExaminerExamStop, "sit the second Earth Sciences exam")
		}
		return digTalk(DigExaminerExamStop, "sit the third Earth Sciences exam")
	}
	green, orange, purple := CustomDigStudentGreen, CustomDigStudentOrange, CustomDigStudentPurple
	if stage == DigThirdExam {
		green, orange, purple = CustomDigStudentGreen3, CustomDigStudentOrange3, CustomDigStudentPurple3
	}
	if !s.Ran[green] {
		return digCustom(green, "revise with the green student")
	}
	if !s.Ran[orange] {
		return digCustom(orange, "revise with the orange student")
	}
	if stage == DigThirdExam {
		return digOpalErrand(s, flags)
	}
	if !s.Ran[purple] {
		return digCustom(purple, "revise with the purple student")
	}
	return digWait("all three students were asked and the journal does not say the second exam is ready")
}

// digOpalErrand is the purple student's: her first talk starts the errand, the opal ends it.
func digOpalErrand(s Snapshot, flags int) Step {
	started := flags&DigFlagOpal != 0
	asked := s.Ran[CustomDigStudentPurple3]
	if s.HeldID(DigObjOpal) > 0 {
		// A cut opal is delivered by the talk: the errand is started first when it has not been (her first talk only asks).
		if started || asked {
			return digCustom(CustomDigStudentPurple3, "give the purple student her opal")
		}
		return digCustom(CustomDigStudentPurple3, "ask the purple student what she wants")
	}
	if !started && !asked {
		return digCustom(CustomDigStudentPurple3, "ask the purple student what she wants")
	}
	if s.HeldID(DigObjUncutOpal) > 0 {
		if s.HeldID(DigObjChisel) == 0 {
			return digKitOrPark(s, KitDigChisel, digChiselKit(), "a chisel to cut the opal", "no chisel from the bank, the mule or Varrock's general store")
		}
		return digCustom(CustomDigCutOpal, "cut the uncut opal")
	}
	return digPanStep(s, DigObjUncutOpal, "pan the river for an uncut opal")
}

// digImpress is stage 5: a find from the level 3 site for the expert, and the expert's letter for a workman.
func digImpress(s Snapshot, region DigRegion) Step {
	switch {
	case s.HeldID(DigObjExpertScroll) > 0:
		return digCustom(CustomDigGiveScroll, "show the invitation letter to a workman")
	case s.HeldID(DigObjTalisman) > 0:
		return digCustomItem(CustomDigUseOnExpert, DigObjTalisman, 1, "show the talisman to the expert")
	case s.HeldID(DigObjTrowel) == 0:
		return digTrowelStep(s)
	case s.HeldID(DigObjSpecimenJar) == 0:
		return digCustom(CustomDigSacks, "a specimen jar from the sacks")
	case s.HeldID(DigObjSpecimenBrush) == 0:
		return digPickpocketStep(s, DigObjSpecimenBrush, 1, "a specimen brush is a pickpocket from a workman")
	}
	if s.Failed[CustomDigTalisman] && !s.Ran[CustomDigLostLetter] {
		// A long dig that found no talisman: it is found once, and the expert's letter for one already shown is a line of his menu.
		return digCustom(CustomDigLostLetter, "the expert may already have seen the talisman: ask him for the letter again")
	}
	return digCustom(CustomDigTalisman, "dig the level 3 site for a find")
}

// digAfterBlast is stage 8: the tablet, then the expert.
func digAfterBlast(s Snapshot, region DigRegion) Step {
	if s.HeldID(DigObjStoneTablet) > 0 {
		return digCustomItem(CustomDigUseOnExpert, DigObjStoneTablet, 1, "show the stone tablet to the expert")
	}
	if st, gated := digAltarGate(s, region); gated {
		return st
	}
	if s.Level("agility") > 0 && s.Level("agility") < DigAgility && region == DigSurface {
		return digPark(DigSkillParkFor, "the winch needs Agility %d to squeeze down the shaft (this account has %d)", DigAgility, s.Level("agility"))
	}
	if s.FreeSlots < 1 && s.TileKnown {
		return Step{Kind: StepDeposit, KeepIDs: digKeep, Bank: VarrockEastBank, BankSet: true, Reason: "room for the stone tablet"}
	}
	return digCustom(CustomDigTablet, "take the Zarosian tablet from the altar cave")
}

// digWorksIn reports that a step is one the account may take where it stands underground: the west shaft's trip in the west shaft, the
// blast in the east shaft, the tablet in the altar cave. Every other step starts on the surface.
func digWorksIn(step Step, region DigRegion) bool {
	if step.Kind != StepCustom {
		return false
	}
	switch step.Custom {
	case CustomDigWest:
		return region == DigShaftWest
	case CustomDigBlast:
		return region == DigShaftEast
	case CustomDigTablet:
		return region == DigAltarEast
	case CustomDigLeave:
		return true
	}
	return false
}

// DecideDigsite is the whole quest, read from the journal's stage, the account's region and the pack.
func DecideDigsite(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return digWait("the quest tab has not been coloured yet")
	case StatusNotStarted:
		if s.TileKnown && DigRegionOf(s.Tile) != DigSurface {
			return digCustom(CustomDigLeave, "up out of a shaft before starting the quest")
		}
		return digTalk(DigExaminerStartStop, "start the Digsite Quest")
	}
	if !s.StageKnown {
		return digWait("the journal stage has not been read")
	}
	if !s.TileKnown {
		return digWait("the account's position has not been read")
	}
	stage, flags := digSnap(s)
	if stage == DigNotStarted || stage >= DigComplete {
		return digWait("the journal and the quest tab disagree about the Digsite Quest")
	}
	region := DigRegionOf(s.Tile)
	step := digStageStep(s, stage, flags, region)
	// Underground, only the legs that work there go on; the rest start on the surface.
	if region != DigSurface && !digWorksIn(step, region) && step.Kind != StepWait {
		return digCustom(CustomDigLeave, "up the rope to the surface")
	}
	// A step that picks pockets goes in with something to eat when the account's hitpoints would not take a bad rope's misses.
	if st, ok := digPocketFood(s, step, region); ok {
		return st
	}
	return step
}

// digStageStep is the decision for one stage once the account's place is known.
func digStageStep(s Snapshot, stage, flags int, region DigRegion) Step {
	// One bundled pass of what the bank or the mule can give for the stage, before the first walk of a run on the surface.
	if region == DigSurface {
		if st, ok := digOnce(s, fmt.Sprintf("%s-%d", KitDigBundle, stage), digBundle(s, stage), "what the bank or the mule can give for the stage"); ok {
			return st
		}
	}
	switch stage {
	case DigStamping:
		switch {
		case s.HeldID(DigObjStampedLetter) > 0:
			return digTalk(DigExaminerLetterStop, "hand the stamped letter to the Examiner")
		case s.HeldID(DigObjPlainLetter) > 0:
			return digTalk(DigCuratorStop, "have the Curator stamp the letter")
		}
		return digTalk(DigExaminerLostLetterStop, "ask the Examiner for another letter")
	case DigFirstExam:
		return digFirstExam(s, flags)
	case DigSecondExam, DigThirdExam:
		return digStudents(s, stage, flags)
	case DigImpressExpert:
		return digImpress(s, region)
	case DigMineshaftPermit:
		return digChemistry(s, region)
	case DigPouredCompound:
		return digBlastStep(s, region, "light the compound")
	case DigRemovedBlockage:
		return digAfterBlast(s, region)
	}
	return digWait("unrecognised Digsite Quest stage %d", stage)
}

// DigsiteRecord is the quest's identity and the levels the content enforces: a pocket that fails, a winch that will not take the
// account and a mix that will not mix are all worse than a quest not started.
var DigsiteRecord = Record{ID: DigsiteID, Name: DigsiteName, QuestPoints: 2,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "thieving", Level: DigThieving}, {Skill: "agility", Level: DigAgility}, {Skill: "herblore", Level: DigHerblore}}}}

// Digsite is the engine module. It never fights (NoRetaliate) and prices the one cave that bites (DigAltarFit); two deaths park it for a
// day and so does one in the blast or the altar cave (StepDeathCaps). The slot is held for the whole run: the tray, the sacks, the chest, the
// tablet and the cave workman are shared.
var Digsite = Module{
	Record: DigsiteRecord, Bank: VarrockEastBank, BankSet: true, Anchor: DigStudentsAnchor,
	ToolIDs:     digTools,
	ReadStage:   ParseDigsiteJournal,
	Sustain:     digSustain,
	NoRetaliate: true,
	MaxDeaths:   2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomDigBlast:  {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
		CustomDigTablet: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecideDigsite,
}
