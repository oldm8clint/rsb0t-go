package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Watch Tower (content id `itwatchtower`, %itwatchtower 0 to 14 and the bit field %itwatchtower_bits, %gutanoth_gold), ported from the TS def
// (defs/watchtower/*.ts) and checked against content 289: quests/quest_itwatchtower/{configs,scripts} (quest_itwatchtower.rs2, itwatchtower_journal.rs2,
// watchtower_wizard, og, toban, grew, gorad, ogre_guard, city_guard, skavid, enclave_guard, ogre_shaman, ogre_potion), areas/area_yanille (ogre.npc,
// ogre_trader.rs2, gutanoth.inv), skill_agility/scripts/shortcuts.rs2 (the rope swing) and the maps m39_47, m39_48, m39_147, m40_47, m40_147 and m45_73.
// Four quest points and 15,300 Magic experience (stat_advance(magic, 153000)) plus 5,000 coins and the Watchtower Teleport scroll.
//
// THE QUEST, as the stages move it:
//
//	 0 The Watchtower wizard (the tower's top floor, Yanille) asks for help. The tower's guards turn the ground ladder away until the quest is
//	   started, so the first climb is the Climbing rocks on the north wall (Agility 18, loc 2299).
//	 1 The bush at 2544,3134 (loc 2799; forty look-alike bushes search to nothing or to a decoy the wizard refuses) gives fingernails, which
//	   the wizard reads as a skavid's; he sends the account to the ogres.
//	 2 Three tribes each want a favour and each pays with a piece of the relic (relic part 1, 2, 3):
//	     Og (2506,3116, four guard ogres) hands over a key to Toban's chest; the chest is in Toban's camp, a pocket behind a cave mouth at
//	     2499,2989 (a ladder leaves it), and Og pays when the gold is back.
//	     Toban (in the camp) wants an adult dragon's bones to chew on, and pays when they are put in his hands.
//	     Grew (on an island across a rope swing at 2499,3087: the rope is used up by the crossing and the swing back needs Agility 10) wants one of
//	     Gorad's teeth, and pays with a piece of the relic and the first crystal. Gorad (level 68, 80 hitpoints, in the camp) drops the tooth when
//	     he dies; he is a real fight.
//	   The wizard is given the three parts (use each on him) and assembles the relic (3, which the journal prints as 2).
//	 4 The north-west gate of Gu'Tanoth (2504,3062): the relic used on a guard opens it. The city beyond: a rock cake stolen from the stall at 2506,3023
//	   (Thieving 15; the ogre trader beside it attacks a thief he is within 3 tiles of), given to the battlement guard (a first talk to be asked for a
//	   gift, then the cake), the low wall climbed, and the chasm jumped for 20 coins (Agility 25) to the City guard's pocket.
//	 5 The City guard sets a riddle (a talk). Its answer is a death rune used on him: the rune is taken and the Skavid map given (6).
//	 6 The six skavid caves behind their mouths (a map and a LIT light source in the pack, or the mouth teleports the account to a dark cave whose
//	   only way out is a climbing rope): the scared skavid in cave 5 first (the account learns the words), then the four talkers (Nod, Ig, Ar, Cur in
//	   caves 1 to 4), then the mad skavid in cave 6, which is behind the east gate (a bar of gold, handed over on a second approach), who tests the
//	   account with one of four phrases and hands over the second crystal (7).
//	 7 Cave nightshade (a ground spawn in cave 2 and cave 6) used on the enclave guard (2507,3037) runs the account past him into the shamans'
//	   enclave, which burns the plant (8). The enclave is a pocket: the only way out is its cave mouth at 2598,9468.
//	 8 The wizard is told the shamans cannot be touched and tells how to beat them (9).
//	 9 Guam leaf in a vial of water, jangerberries (four ground spawns on Grew's island), ground bat bones (a pestle and mortar): in that order, as
//	   any other order explodes the vial for 5 (Herblore 14). The wizard infuses the ogre potion (10).
//	10 The magic ogre potion used on an ogre shaman dissolves it (Magic 14): six times in all. The sixth hands over the third crystal and spends
//	   the potion; the Rock of Dalgroth (2590,9449, a 2x2 rock) then gives the fourth to a pickaxe (Mining 40). Shamans respawn after 102 ticks.
//	11 All four crystals shown to the wizard; the lever on his floor (2543,3115) activates the shield, pays the reward and puts the account on the
//	   tower's mirror copy (2928,4715, level 2), where the wizard's scroll is read and the ladders lead down.
//
// WHAT THE TS DEF DID NOT SAY AND THE CONTENT DOES.
//   - The shamans have no hunt mode and attack only an account that talks to them or hits them (20 and 30 damage). The enclave is NOT empty of hunters for
//     that: it is a nest of six blue dragons (level 111, fire up to 30, or 50 against a low defence roll, one attack in four), a greater demon, three
//     skeletons and six ogre chieftains (level 81, a hit of 9), all cowardly hunters that find any account of combat below twice their level (watchtowerhunt.go).
//     In Gu'Tanoth and the hills round it the hunters are the chieftains (four in the city, one outside) and the level 53 ogres (a hit of 7): four round Og,
//     four on the bank of Grew's branch, four on Grew's island, four in Toban's camp and one at the camp's cave mouth, the figures the maps give.
//   - The sixth shaman, not the robe, hands over the third crystal in the content; the robe (2617,9437) only gives it again to an account that lost it.
//   - Dragon bones (adult dragons, level 79 and up) and bat bones are nobody's gather: no shop sells either and the fleet's banks and mule hold none
//     (19 Oct 2026), so the quest parks (Provision blocked) before the first risk, with the reason, until some exist. Guam leaf, gold bars, death runes
//     and pestles are on the mule.
//   - The fleet's levels (19 Oct 2026, 357 accounts): Mining 40 on 8, Herblore 14 on 1, Agility 25 on 57, Thieving 15 and Magic 14 on nearly all. The
//     quest therefore builds up to the first step an account cannot do and parks there with the reason (the owner's rule), each gate being its own
//     step's: Agility 18 at the wall, Thieving 15 and Agility 25 at the city, Herblore 14 at the potion, Magic 14 at the shamans, Mining 40 at the rock.
//   - The wizard's lost-relic menu is offered at stage 3 only. At stage 2 a part lost after its tribe was helped is asked of its giver (Og's, Grew's and
//     Toban's menus all offer it once their tribe is helped; Grew's hides it when the wizard already holds that part) and the wizard answers "I already have
//     that part" to a spare, which is then kept (a refused piece is never given again).
//
// WHAT CAN HURT. The account never fights back (Module.NoRetaliate) and nothing is eaten inside a chat, so every visit is priced BEFORE the first walk of it
// against the hunters of its stands (WtTripOdds for the ogre sites, WtGoradOdds for the fight, WtEnclaveOdds for the dragons' nest) and a price the model
// refuses parks the quest with the reason, ahead of the first step that cannot be done safely. Inside a hunted pocket (the island, the camp, the enclave)
// nothing is priced again: the account is topped up on the way in, eats at a line above the pocket's hardest hit and walks out by the pocket's own way
// if it is hurt and has no food. The one fight, Gorad, is an explicit attack. One death parks the quest for a day.

// WatchtowerName is the quest tab's spelling (component 7349, `questlist:itwatchtower`).
const WatchtowerName = "Watch Tower"

// WatchtowerID is the engine's id.
const WatchtowerID = "itwatchtower"

// The stages (%itwatchtower, quest_itwatchtower.constant). The journal prints 2 for 2 to 5 alike except where a later line decides (see ParseWatchtowerJournal).
const (
	WtNotStarted       = 0
	WtStarted          = 1
	WtGivenFingernails = 2
	WtMadeRelic        = 3
	WtGivenRelic       = 4
	WtGivenRiddle      = 5
	WtSolvedRiddle     = 6
	WtSkavidCrystal    = 7
	WtFedNightshade    = 8
	WtLearnedPotion    = 9
	WtMadePotion       = 10
	WtFoundAll         = 11
	WtComplete         = 13
	WtReadScroll       = 14
)

// Flags the journal tells beside the stage (WtState.Flags).
const (
	WtOgSpoken uint32 = 1 << iota
	WtOgHelped
	WtGrewSpoken
	WtGrewHelped
	WtTobanSpoken
	WtTobanHelped
	WtLookingRelic
	WtMarketAsked
	WtMarketPaid
	WtLearning
	WtLearnedIg
	WtLearnedAr
	WtLearnedCur
	WtLearnedNod
	WtMinedRock
)

// WtShamans is how many shamans the potion must dissolve.
const WtShamans = 6

// WtState is the journal's reading of the quest, and what Snapshot.Stage packs.
type WtState struct {
	Stage int
	Flags uint32
	// Left is how many shamans are still to be dissolved, -1 when the page says nothing (below stage 10).
	Left int
}

// Has reports whether every flag in f is set.
func (t WtState) Has(f uint32) bool { return t.Flags&f == f }

// Pack is the int Module.ReadStage returns: the stage in the low four bits, the flags above, the shamans left (plus one) above those.
func (t WtState) Pack() int {
	return t.Stage&15 | int(t.Flags)<<4 | (t.Left+1)<<23
}

// WtStateOf unpacks Pack.
func WtStateOf(n int) WtState {
	return WtState{Stage: n & 15, Flags: uint32(n>>4) & (1<<19 - 1), Left: (n >> 23 & 7) - 1}
}

// String is the state in a log line.
func (t WtState) String() string {
	return fmt.Sprintf("stage %d flags %#x shamans left %d", t.Stage, t.Flags, t.Left)
}

// wtNormalise is NormaliseLines with the content's own line break read as a space.
func wtNormalise(lines []string) string {
	flat := make([]string, len(lines))
	for i, l := range lines {
		flat[i] = strings.ReplaceAll(l, "|", " ")
	}
	return NormaliseLines(flat)
}

// wtStageMarkers are the journal's pages, newest stage first (itwatchtower_journal.rs2 keeps the earlier paragraphs on the page). Every needle
// is a run of words inside one colour span, so a stripped tag cannot split it.
var wtStageMarkers = []struct {
	stage int
	text  string
}{
	{WtComplete, "quest complete!"},
	{WtFoundAll, "i have taken the crystals to the watchtower wizard"},
	{WtMadePotion, "he infused it into a magic ogre potion"},
	{WtLearnedPotion, "i need to get it enchanted"},
	{WtLearnedPotion, "i need to make the"},
	{WtFedNightshade, "i need to defeat the ogre shamans and find the other"},
	{WtSkavidCrystal, "the other crystals are in the shamans' enclave"},
	{WtSolvedRiddle, "i was given a map by the guard"},
	{WtGivenRiddle, "some guards gave me a puzzle"},
	{WtGivenRelic, "i gave the ogre relic to the north west guard"},
	{WtGivenFingernails, "now i need to deal with the tribal ogres"},
	{WtStarted, "i accepted the challenge of finding the lost"},
	{WtNotStarted, "i can start this quest by speaking to the watchtower wizard"},
}

// wtFlagMarkers are the lines that carry a flag. A tribe's "wants" line is dropped once the tribe is helped, so the helped line implies the spoken one.
var wtFlagMarkers = []struct {
	text string
	flag uint32
}{
	{"i returned og's stolen gold", WtOgHelped},
	{"og wants me to", WtOgSpoken},
	{"i have og's", WtOgSpoken},
	{"i knocked out one of gorad's teeth", WtGrewHelped},
	{"grew wants me to", WtGrewSpoken},
	{"i have one of gorad's teeth", WtGrewSpoken},
	{"i gave the bones to toban", WtTobanHelped},
	{"toban wants the", WtTobanSpoken},
	{"i have the dragon bones", WtTobanSpoken},
	{"the north west guard wants", WtLookingRelic},
	{"the north-east guard wants", WtMarketAsked},
	{"i gave the north-east guard a rock cake", WtMarketPaid},
	{"i have been taught a few words", WtLearning},
	{"'cur bidith' - 'ig'", WtLearnedIg},
	{"'gor cur' - 'ar'", WtLearnedAr},
	{"'bidith tanath' - 'cur'", WtLearnedCur},
	{"'gor nod' - 'nod'", WtLearnedNod},
	{"i have mined the sacred rock", WtMinedRock},
}

var wtKills = regexp.MustCompile(`kill (\d+) ogre shaman`)

// ParseWatchtowerJournal reads the state from the rendered journal. ok is false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseWatchtowerJournal(lines []string) (int, bool) {
	text := wtNormalise(lines)
	st := WtState{Stage: -1, Left: -1}
	for _, m := range wtStageMarkers {
		if strings.Contains(text, m.text) {
			st.Stage = m.stage
			break
		}
	}
	if st.Stage < 0 {
		return 0, false
	}
	for _, m := range wtFlagMarkers {
		if strings.Contains(text, m.text) {
			st.Flags |= m.flag
		}
	}
	for _, imp := range []struct{ from, to uint32 }{{WtOgHelped, WtOgSpoken}, {WtGrewHelped, WtGrewSpoken}, {WtTobanHelped, WtTobanSpoken}, {WtMarketPaid, WtMarketAsked}} {
		if st.Flags&imp.from != 0 {
			st.Flags |= imp.to
		}
	}
	if m := wtKills.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 0 && n <= WtShamans {
			st.Left = n
		}
	} else if strings.Contains(text, "i killed all the ogre shamans") {
		st.Left = 0
	}
	return st.Pack(), true
}

// Ids from content/pack (checked against the production config by TestWatchtowerIDsMatchTheProductionConfig).
const (
	WtObjCoins          = 995
	WtObjRope           = 954
	WtObjLitCandle      = 33
	WtObjLitBlackCandle = 32
	WtObjLitTorch       = 594
	WtObjVialWater      = 227
	WtObjVialEmpty      = 229
	WtObjJangerberries  = 247
	WtObjGuamLeaf       = 249
	WtObjGuamVial       = 91 // [guamvial], "Unfinished potion"
	WtObjPestle         = 233
	WtObjPickaxe        = 1265
	WtObjBatBones       = 530
	WtObjDragonBones    = 536
	WtObjDeathRune      = 560
	WtObjGoldBar        = 2357
	WtObjShark          = 385
	WtObjRelic          = 2372
	WtObjPart1          = 2373
	WtObjPart2          = 2374
	WtObjPart3          = 2375
	WtObjMap            = 2376
	WtObjTooth          = 2377
	WtObjTobanKey       = 2378
	WtObjRockCake       = 2379
	WtObjCrystal1       = 2380
	WtObjCrystal2       = 2381
	WtObjCrystal3       = 2382
	WtObjCrystal4       = 2383
	WtObjFingernails    = 2384
	WtObjDecoyRobe      = 2385
	WtObjDecoyArmour    = 2386
	WtObjDecoyDagger    = 2387
	WtObjDecoyEyePatch  = 2388
	WtObjJangerVial     = 2389
	WtObjGuamJangerVial = 2390
	WtObjGroundBones    = 2391
	WtObjStolenGold     = 2393
	WtObjOgrePotion     = 2394
	WtObjMagicPotion    = 2395
	WtObjSpellScroll    = 2396
	WtObjShamanRobe     = 2397
	WtObjNightshade     = 2398

	WtNpcWizard      = 872
	WtNpcTowerGuard  = 877
	WtNpcOg          = 853
	WtNpcGrew        = 854
	WtNpcToban       = 855
	WtNpcGorad       = 856
	WtNpcGuardEast   = 858 // [ogre_guard1], the east gate
	WtNpcGuardRelic  = 859 // [ogre_guard2], the north-west gate
	WtNpcGuardWall   = 860 // [ogre_guard3], the battlement
	WtNpcGuardChasm  = 861 // [ogre_guard4], the chasm
	WtNpcCityGuard   = 862
	WtNpcScared      = 863
	WtNpcMad         = 864
	WtNpcTalker1     = 865
	WtNpcTalker2     = 866
	WtNpcTalker3     = 867
	WtNpcTalker4     = 868
	WtNpcEnclave     = 870
	WtNpcShaman      = 871
	WtNpcTrader      = 875 // [ogre_trader2], who minds the rock cake stall
	WtNpcOgre        = 114
	WtNpcOgre2       = 115
	WtNpcChieftain   = 852
	LocWtClimbRocks  = 2299
	LocWtTowerLadder = 2833
	LocWtLadderTop   = 1746 // [laddertop], op Climb-down
	LocWtLadderUp    = 2796
	LocWtLadderDown  = 2797
	LocWtLever       = 2794
	LocWtNailBush    = 2799
	LocWtBranch      = 2326 // [tree_ropeswing4_norope], the tree the rope is used on
	LocWtSwingBack   = 2324 // [tree_ropeswing3], op Swing-on
	LocWtTobanCave   = 2811
	LocWtTobanLadder = 2812
	LocWtTobanChest  = 2790
	LocWtGateRelic   = 2788
	LocWtGateEast    = 2786
	LocWtCakeStall   = 2793
	LocWtBattlement  = 2832
	LocWtJumpIn      = 2830
	LocWtJumpOut     = 2831
	LocWtEnclaveExit = 2813
	LocWtDarkRope    = 2825
	LocWtRock        = 2816
)

// LocWtCaveIn and LocWtCaveOut are the six caves' mouths and exits, by cave 1 to 6.
var (
	LocWtCaveIn  = [6]int{2805, 2806, 2807, 2808, 2809, 2810}
	LocWtCaveOut = [6]int{2817, 2818, 2819, 2820, 2821, 2822}
)

// Tiles from the content maps and the loc index (questmods' TestWatchtowerTilesAreGroundAndTheirLocsAreThere pins them against the real data).
var (
	WtBank = YanilleBank
	// The tower: the wall climbed at stage 0 (its stand and the first floor it lands on), the ladders, the wizard's floor and the lever. The tower's
	// ladders land on the account's own tile one floor up or down (~climb_ladder with movecoord(coord, 0, 1, 0)); the two ladders of the first floor
	// are fixed (the stage-1 up ladder lands at 2549,3112,2, the wizard floor's down ladder at 2549,3112,1).
	WtWallStand        = tile.Tile{X: 2548, Z: 3120}
	WtWallLanding      = tile.Tile{X: 2548, Z: 3117, Level: 1}
	WtTowerLadderStand = tile.Tile{X: 2544, Z: 3112}
	WtFirstFloorStand  = tile.Tile{X: 2549, Z: 3112, Level: 1}
	WtWizardAnchor     = tile.Tile{X: 2549, Z: 3112, Level: 2}
	WtLeverStand       = tile.Tile{X: 2543, Z: 3116, Level: 2}
	WtTopLadderStand   = tile.Tile{X: 2549, Z: 3112, Level: 2}
	WtLowLadderStand   = tile.Tile{X: 2544, Z: 3112, Level: 1}
	WtBushStand        = tile.Tile{X: 2544, Z: 3133}
	WtBush             = tile.Tile{X: 2544, Z: 3134}
	WtCandle           = tile.Tile{X: 2547, Z: 3114}
	// The tribes. Og stands in the hills west of Yanille, Grew across the rope swing, Toban and Gorad in the camp behind the cave at 2499,2989.
	WtOgAnchor      = tile.Tile{X: 2506, Z: 3116}
	WtBranch        = tile.Tile{X: 2499, Z: 3087}
	WtBranchStand   = tile.Tile{X: 2501, Z: 3087}
	WtIslandLanding = tile.Tile{X: 2505, Z: 3087}
	WtGrewAnchor    = tile.Tile{X: 2513, Z: 3084}
	WtSwingStand    = tile.Tile{X: 2511, Z: 3091}
	WtSwingLanding  = tile.Tile{X: 2511, Z: 3096}
	WtBerries       = [4]tile.Tile{{X: 2510, Z: 3090}, {X: 2512, Z: 3080}, {X: 2516, Z: 3086}, {X: 2517, Z: 3082}}
	WtCampCave      = tile.Tile{X: 2499, Z: 2989}
	WtCampCaveStand = tile.Tile{X: 2499, Z: 2988}
	WtCampArrival   = tile.Tile{X: 2576, Z: 3029}
	WtCampLadder    = tile.Tile{X: 2575, Z: 3029}
	WtCampChest     = tile.Tile{X: 2575, Z: 3031}
	WtTobanAnchor   = tile.Tile{X: 2576, Z: 3027}
	WtGoradAnchor   = tile.Tile{X: 2577, Z: 3021}
	// Gu'Tanoth. The north-west gate (the guards stand either side of its stand), the stall, the battlement guard and the wall, the chasm's two rocks
	// and the City guard's pocket.
	WtRelicGateStand  = tile.Tile{X: 2506, Z: 3062}
	WtRelicGateInside = tile.Tile{X: 2503, Z: 3062}
	WtStallStand      = tile.Tile{X: 2506, Z: 3024}
	WtStall           = tile.Tile{X: 2506, Z: 3023}
	WtWallGuardAnchor = tile.Tile{X: 2503, Z: 3011}
	WtBattlementWest  = tile.Tile{X: 2506, Z: 3011}
	WtBattlementEast  = tile.Tile{X: 2508, Z: 3011}
	WtJumpStand       = tile.Tile{X: 2531, Z: 3026}
	WtJumpLanding     = tile.Tile{X: 2530, Z: 3029}
	WtJumpBackStand   = tile.Tile{X: 2530, Z: 3029}
	WtCityGuardAnchor = tile.Tile{X: 2541, Z: 3029}
	WtEastGateStand   = tile.Tile{X: 2550, Z: 3030}
	WtEastGateInside  = tile.Tile{X: 2549, Z: 3027}
	WtEnclaveGuard    = tile.Tile{X: 2507, Z: 3037}
	// The caves: a stand beside each mouth (cave 1 only from the south), the landing the map and the light land the account on, and where each exit
	// puts the account back.
	WtCaveStands = [6]tile.Tile{
		{X: 2561, Z: 3021}, {X: 2522, Z: 3070}, {X: 2539, Z: 3054}, {X: 2552, Z: 3054}, {X: 2552, Z: 3034}, {X: 2529, Z: 3013},
	}
	WtCaveLandings = [6]tile.Tile{
		{X: 2498, Z: 9418}, {X: 2532, Z: 9469}, {X: 2518, Z: 9455}, {X: 2498, Z: 9451}, {X: 2504, Z: 9441}, {X: 2522, Z: 9411},
	}
	WtCaveExits = [6]tile.Tile{
		{X: 2562, Z: 3024}, {X: 2524, Z: 3070}, {X: 2540, Z: 3054}, {X: 2553, Z: 3054}, {X: 2552, Z: 3034}, {X: 2529, Z: 3013},
	}
	// WtCaveTalker is each cave's skavid (spawn tile) and WtCaveWord the one word he understands (cave 5 is the scared skavid, 6 the mad one).
	WtCaveTalker = [6]tile.Tile{
		{X: 2503, Z: 9415}, {X: 2530, Z: 9465}, {X: 2515, Z: 9452}, {X: 2500, Z: 9451}, {X: 2499, Z: 9433}, {X: 2523, Z: 9411},
	}
	WtDarkLanding   = tile.Tile{X: 2505, Z: 9460}
	WtNightshade2   = tile.Tile{X: 2530, Z: 9462}
	WtNightshade6   = tile.Tile{X: 2528, Z: 9415}
	WtEnclaveArrive = tile.Tile{X: 2588, Z: 9410}
	WtEnclaveExit   = tile.Tile{X: 2598, Z: 9467}
	WtEnclaveCave   = tile.Tile{X: 2598, Z: 9468}
	WtRockStand     = tile.Tile{X: 2590, Z: 9448}
	WtRobe          = tile.Tile{X: 2617, Z: 9437}
	// The six shamans stand at these spawn tiles (they wander a little). Three are well clear of the enclave's chieftains (a dozen tiles or ten), three
	// within four tiles of one. The split is the chieftains' only: every one of the six is in a blue dragon's reach (watchtowerhunt.go), so the module takes
	// them nearest first, spawn to spawn, and does not wait about for a better one (a dissolved shaman is back after 102 ticks, and the count only goes up).
	WtShamanClear   = [3]tile.Tile{{X: 2592, Z: 9436}, {X: 2606, Z: 9438}, {X: 2599, Z: 9461}}
	WtShamanNear    = [3]tile.Tile{{X: 2607, Z: 9451}, {X: 2582, Z: 9437}, {X: 2577, Z: 9451}}
	WtMirrorLanding = tile.Tile{X: 2928, Z: 4715, Level: 2}
)

// WtCaveWords are the words the four talkers understand, by cave 1 to 4 (skavid.rs2: skavidtalker4 Nod, 1 Ig, 2 Ar, 3 Cur).
var WtCaveWords = [4]string{"Nod.", "Ig.", "Ar.", "Cur."}

// WtCaveFlag is the flag each of the four talkers' words sets.
var WtCaveFlag = [4]uint32{WtLearnedNod, WtLearnedIg, WtLearnedAr, WtLearnedCur}

// WtArea is where the account stands, as far as the quest's walks care: the pockets no walk leaves are named.
type WtArea int

const (
	// WtAreaMain is the open mainland (and a floor of the tower's ground storey).
	WtAreaMain WtArea = iota
	// WtAreaTower is the tower's first or second floor.
	WtAreaTower
	// WtAreaMirror is the tower's mirror copy the lever sends the account to.
	WtAreaMirror
	// WtAreaIsland is Grew's island.
	WtAreaIsland
	// WtAreaCamp is Toban's camp.
	WtAreaCamp
	// WtAreaLowerCity is the lower city behind the battlement.
	WtAreaLowerCity
	// WtAreaCityGuard is the City guard's side of the chasm.
	WtAreaCityGuard
	// WtAreaEastInside is the ground behind the east gate, cave 6's side.
	WtAreaEastInside
	// WtAreaCaves is one of the six skavid caves or the dark cave.
	WtAreaCaves
	// WtAreaEnclave is the shamans' enclave.
	WtAreaEnclave
)

// String names an area for a log line.
func (a WtArea) String() string {
	return [...]string{"the mainland", "the tower's upper floors", "the tower's mirror", "Grew's island", "Toban's camp", "Gu'Tanoth's lower city",
		"the City guard's side of the chasm", "the ground behind the east gate", "a skavid cave", "the shamans' enclave"}[a]
}

type wtSpan [2]int

// The three regions of Gu'Tanoth the walk graph cannot leave, row by row (z: x spans), flood-filled from the real collision pack
// (questmods' TestWatchtowerAreasMatchTheFloodFill compares every tile).
var (
	wtLowerCityRows = map[int][]wtSpan{
		3009: {{2521, 2521}}, 3010: {{2520, 2522}}, 3011: {{2508, 2523}}, 3012: {{2508, 2523}}, 3013: {{2520, 2524}}, 3014: {{2520, 2523}},
		3015: {{2522, 2524}}, 3016: {{2523, 2525}}, 3017: {{2522, 2526}, {2528, 2528}}, 3018: {{2521, 2522}, {2525, 2530}},
		3019: {{2520, 2522}, {2525, 2531}}, 3020: {{2513, 2515}, {2520, 2522}, {2526, 2532}}, 3021: {{2512, 2516}, {2519, 2522}, {2527, 2531}},
		3022: {{2512, 2522}, {2528, 2528}, {2530, 2531}}, 3023: {{2512, 2522}, {2530, 2531}}, 3024: {{2512, 2515}, {2518, 2521}, {2530, 2531}},
		3025: {{2512, 2515}, {2519, 2520}, {2530, 2531}}, 3026: {{2512, 2516}, {2519, 2519}, {2531, 2531}}, 3027: {{2512, 2515}},
	}
	wtEastInsideRows = map[int][]wtSpan{
		3012: {{2529, 2549}}, 3013: {{2529, 2549}}, 3014: {{2547, 2550}}, 3015: {{2548, 2550}}, 3016: {{2549, 2550}}, 3017: {{2550, 2552}},
		3018: {{2549, 2551}}, 3019: {{2549, 2551}}, 3020: {{2548, 2549}}, 3021: {{2547, 2548}}, 3022: {{2546, 2548}}, 3023: {{2546, 2548}},
		3024: {{2546, 2548}}, 3025: {{2546, 2549}}, 3026: {{2548, 2550}}, 3027: {{2549, 2550}},
	}
	wtCityGuardRows = map[int][]wtSpan{
		3027: {{2542, 2542}}, 3028: {{2541, 2544}}, 3029: {{2530, 2530}, {2540, 2543}}, 3030: {{2529, 2531}, {2540, 2543}},
		3031: {{2528, 2531}, {2541, 2545}}, 3032: {{2527, 2545}}, 3033: {{2529, 2544}}, 3034: {{2529, 2531}, {2541, 2543}},
		3035: {{2539, 2542}}, 3036: {{2540, 2541}},
	}
)

func wtInRows(rows map[int][]wtSpan, x, z int) bool {
	for _, sp := range rows[z] {
		if x >= sp[0] && x <= sp[1] {
			return true
		}
	}
	return false
}

// WtAreaOf is where a tile lies.
func WtAreaOf(t tile.Tile) WtArea {
	x, z := t.X, t.Z
	switch {
	case x >= 2900 && x <= 2960 && z >= 4670 && z <= 4740:
		return WtAreaMirror
	case z >= 9400 && z <= 9480 && x >= 2490 && x <= 2540:
		return WtAreaCaves
	case z >= 9400 && z <= 9480 && x >= 2560 && x <= 2623:
		return WtAreaEnclave
	}
	if t.Level == 0 {
		switch {
		case x >= 2505 && x <= 2519 && z >= 3079 && z <= 3092:
			return WtAreaIsland
		case x >= 2569 && x <= 2585 && z >= 3021 && z <= 3035:
			return WtAreaCamp
		case wtInRows(wtLowerCityRows, x, z):
			return WtAreaLowerCity
		case wtInRows(wtCityGuardRows, x, z):
			return WtAreaCityGuard
		case wtInRows(wtEastInsideRows, x, z):
			return WtAreaEastInside
		}
		return WtAreaMain
	}
	if (t.Level == 1 || t.Level == 2) && x >= 2540 && x <= 2552 && z >= 3108 && z <= 3120 {
		return WtAreaTower
	}
	return WtAreaMain
}

// WtCaveAt is the cave (1 to 6) whose landing the tile is within five of, 0 for none (the caves are separate sealed rooms; the dark cave is none).
func WtCaveAt(t tile.Tile) int {
	for i, l := range WtCaveLandings {
		if t.Level == 0 && tile.Chebyshev(t, l) <= 5 {
			return i + 1
		}
	}
	return 0
}

// WtGivePartName is the custom that hands the wizard a piece of the relic, one name for each piece (its object id) so that the wizard's refusal of one ("I
// already have that part") fails only that piece's custom and the others are still given.
func WtGivePartName(part int) string { return fmt.Sprintf("wt-give-part-%d", part) }

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomWtClimb      = "wt-climb"
	CustomWtDown       = "wt-down"
	CustomWtBush       = "wt-bush"
	CustomWtAskOg      = "wt-ask-og"
	CustomWtAskGrew    = "wt-ask-grew"
	CustomWtAskToban   = "wt-ask-toban"
	CustomWtCampIn     = "wt-camp-in"
	CustomWtCampOut    = "wt-camp-out"
	CustomWtChest      = "wt-chest"
	CustomWtGorad      = "wt-gorad"
	CustomWtIslandIn   = "wt-island-in"
	CustomWtIslandOut  = "wt-island-out"
	CustomWtBerries    = "wt-berries"
	CustomWtRelicGate  = "wt-relic-gate"
	CustomWtCake       = "wt-cake"
	CustomWtBattlement = "wt-battlement"
	CustomWtLowerOut   = "wt-lower-out"
	CustomWtJump       = "wt-jump"
	CustomWtRiddleGive = "wt-riddle-give"
	CustomWtJumpOut    = "wt-jump-out"
	CustomWtCave       = "wt-cave"
	CustomWtCaveOut    = "wt-cave-out"
	CustomWtEastGate   = "wt-east-gate"
	CustomWtMad        = "wt-mad"
	CustomWtEastOut    = "wt-east-out"
	CustomWtNightshade = "wt-nightshade"
	CustomWtEnclaveIn  = "wt-enclave-in"
	CustomWtEnclaveOut = "wt-enclave-out"
	CustomWtBrew       = "wt-brew"
	CustomWtInfuse     = "wt-infuse"
	CustomWtDissolve   = "wt-dissolve"
	CustomWtRock       = "wt-rock"
	CustomWtRobe       = "wt-robe"
	CustomWtCrystals   = "wt-crystals"
	CustomWtLever      = "wt-lever"
	CustomWtRelicBack  = "wt-relic-back"

	KitWtFood   = "wt-food"
	KitWtRelic  = "wt-relic"
	KitWtMap    = "wt-map"
	KitWtDraw   = "wt-crystals-bank"
	KitWtScarce = "wt-potion-scarce"
	KitWtBones  = "wt-bones"
	KitWtCity   = "wt-city"
	KitWtCaves  = "wt-caves"
	KitWtBar    = "wt-bar"
	KitWtPotion = "wt-potion"
	KitWtRock   = "wt-rock-kit"
	KitWtIsland = "wt-island-kit"
	KitWtParts  = "wt-parts-bank"
)

// The quest's gates and numbers.
const (
	// The levels the content enforces, each at its own step: the wall at stage 0, the stall at stage 4, the chasm at stage 4, the island's swing back, the
	// mixing at stage 9, the potion on the shamans and the rock at stage 10.
	WtAgilityWall    = 18
	WtThieving       = 15
	WtAgilityChasm   = 25
	WtAgilitySwing   = 10
	WtHerblore       = 14
	WtMagic          = 14
	WtMining         = 40
	WtChasmToll      = 20
	WtPurse          = 120
	WtPurseMin       = 60
	WtFoodQty        = 10
	WtRoom           = 6
	WtStatsPark      = 12 * time.Hour
	WtSupplyPark     = 6 * time.Hour
	WtOddsPark       = 3 * time.Hour
	WtLegPark        = 30 * time.Minute
	WtJangerberryQty = 2
	// WtEnclavePark is how long a refused enclave waits: the dragons' nest does not get easier for an account in a few hours (a level, a shield).
	WtEnclavePark = 12 * time.Hour
	// WtRelicPark is how long an account waits whose relic piece is lost and which the ogres' replacements did not mend: the asking is done once a run, so
	// once a day at the most.
	WtRelicPark = 24 * time.Hour
	// WtChieftainTicks is how long a visit to a chieftain's ground is priced at: the enclave guard's conversation and the walks round him.
	WtChieftainTicks = 40
)

// WtFoodIDs are the foods the quest eats, best first.
var WtFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// wtSustain eats at 28: a round of the four ogres of Toban's camp or Grew's island (4 x 7) must not find the account lower, and the chieftain's 9 is under it
// (the raised line of VsFoe covers the dragons' fire in the enclave, WtEnclaveOdds).
var wtSustain = Sustain{FoodIDs: WtFoodIDs, EatAt: 28}

// Ogre country's foes, from ogre.npc: the ogres (npc 114 and 115: level 53, 60 hitpoints, a swing every six ticks, bonuses 22 and 20, cowardly hunters
// of range 2), Gorad (npc 856: level 68, 80 hitpoints, bonuses 8 and 6, defence bonuses 15, 27, 21; no hunt mode, a fight only when attacked) and the
// chieftains (npc 852: level 81, 60 hitpoints, bonuses 5 and 7, defence 10, 21, 16; cowardly hunters of range 2).
var (
	// FoeOgre (the same npc 114 and 115) is declared once, in bigchompy.go.
	FoeGorad = MeleeFoe{Name: "Gorad", Attack: 54, Strength: 54, Defence: 54, Magic: 1, Hitpoints: 80, AttackTicks: 4, AttackBonus: 8, StrengthBonus: 6,
		DefenceBonus: 15}
	FoeOgreChieftain = MeleeFoe{Name: "Ogre chieftain", Attack: 75, Strength: 71, Defence: 75, Magic: 1, Hitpoints: 60, AttackTicks: 4, AttackBonus: 5,
		StrengthBonus: 7, DefenceBonus: 10}
)

// The chieftains' spawn tiles (npc 852, from the maps m39_47, m40_46 and m40_147): the four of Gu'Tanoth, one outside the city to the east, and the six of
// the enclave. They are cowardly hunters of range 2 that wander 3 from the spawn, and bodies two tiles wide.
var (
	WtChieftainSpawns   = []tile.Tile{{X: 2505, Z: 3032}, {X: 2512, Z: 3022}, {X: 2518, Z: 3041}, {X: 2528, Z: 3044}, {X: 2615, Z: 2987}}
	WtEnclaveChieftains = []tile.Tile{{X: 2568, Z: 9432}, {X: 2573, Z: 9447}, {X: 2579, Z: 9441}, {X: 2580, Z: 9431}, {X: 2611, Z: 9454}, {X: 2616, Z: 9447}}
)

// WtChieftainPad is the reach of a chieftain from its spawn: its wander range, its hunt range, a tile of slack and the other tile of its body.
const WtChieftainPad = 3 + 2 + 1 + 1

// WtAvoid is the rectangles a walk on the surface of the ogres' country keeps out of: a square round each surface chieftain. The walks that cannot be
// made round them (the enclave guard's stand, the stall) fall back to the plain walk and are priced (WtChieftainOdds).
func WtAvoid() []Rect {
	out := make([]Rect, 0, len(WtChieftainSpawns))
	for _, c := range WtChieftainSpawns {
		out = append(out, Rect{MinX: c.X - WtChieftainPad, MaxX: c.X + WtChieftainPad, MinZ: c.Z - WtChieftainPad, MaxZ: c.Z + WtChieftainPad, Level: c.Level})
	}
	return out
}

// WtClearOfChieftains reports a tile at least within tiles from every chieftain of the enclave (the shaman spawns' split into clear and near).
func WtClearOfChieftains(t tile.Tile, within int) bool {
	for _, c := range WtEnclaveChieftains {
		if tile.Chebyshev(t, c) < within {
			return false
		}
	}
	return true
}

// WtOgreOdds prices a visit among the level 53 ogres that hunt the ground round Og, Grew and Toban and the road between them: a pair of them fought one
// after the other with the food in the pack (nothing here attacks them; the price is that the account could beat what it meets).
func WtOgreOdds(s Snapshot) FightOdds {
	return MeleeChain(s, []MeleeFoe{FoeOgre, FoeOgre}, wtSustain)
}

// WtChieftainOdds prices ticks of a chieftain's attention: a conversation or a walk that cannot be fenced from them (the enclave guard's, the enclave).
// The account walks on and eats, so the pack's food counts for half what it heals; the visit must leave the account two hardest hits to the good.
func WtChieftainOdds(s Snapshot, ticks int) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	f := FoeOgreChieftain
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(ticks) / float64(f.AttackTicks) * per
	budget := float64(s.MaxHP) + float64(FoodPoolHeal(s.InvIDs, WtFoodIDs))/2
	margin := float64(2 * f.PeakHit())
	if budget-dmg < margin {
		return false, fmt.Sprintf("the ogre chieftains of Gu'Tanoth (level 81, hits up to %d) cost an account at Defence %d about %.0f hitpoints in %d ticks of their attention, and this one has %d hitpoints and %d hitpoints of food (it must keep %.0f)",
			f.PeakHit(), s.Level("defence"), dmg, ticks, s.MaxHP, FoodPoolHeal(s.InvIDs, WtFoodIDs), margin)
	}
	return true, ""
}

// WatchtowerRecord is the quest's identity. The levels are each their own step's gate (the owner's rule: build up to the first step the account cannot do),
// so the record states none: an account the ladder starts it for goes as far as it can.
var WatchtowerRecord = Record{ID: WatchtowerID, Name: WatchtowerName, QuestPoints: 4}
