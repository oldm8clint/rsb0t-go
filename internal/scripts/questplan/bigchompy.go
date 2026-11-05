package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Big Chompy Bird Hunting (content id `chompybird`, %chompybird, not transmitted, 2 quest points), ported from the TS def
// (defs/chompybird/*.ts) and checked against content 289: quests/quest_chompybird/scripts/{rantz,fycie,bugs,swamp_toad,bloated_toad,
// chompy_bird,raw_chompy,ogre_chest,ogre_bow,quest_chompybird,chompybird_journal}.rs2, its configs, skill_fletching/scripts/ogre_arrows.rs2,
// skill_fletching/scripts/cut_logs.rs2, skill_firemaking/scripts/achey.rs2, skill_woodcutting (the achey tree), skill_combat's ogre bow and
// the maps m40_46, m41_46 and m41_146. The reward is Fletching 262, Cooking 1,470 and Ranged 735 experience and the ogre bow.
//
// THE QUEST, stage by stage (%chompybird, the constants in quest_chompybird.constant; the journal cannot tell 15 from 20, 30 from 35 or 40 from 45
// and the module never needs it to):
//
//	0   Rantz, east of the Ogre City, "Ok, I'll make you some 'stabbers'." (5). Nothing is asked of the account to start.
//	5   Six OGRE ARROWS made BY THE ACCOUNT (the arrows' own script sets a bit in %chompybird_kills when it makes them, and Rantz
//	    refuses any other): achey tree logs (an axe, any Woodcutting level) cut with a knife into 2 to 6 shafts each (Fletching 5), four
//	    feathers to a shaft (up to six shafts a use), a wolf's bones chiselled into 2 to 6 arrowtips each (Fletching 5 and Crafting 5) and a
//	    tip to a flighted shaft (up to six a use). Rantz takes six; the six that are left are to shoot with. Bugs, in the cave under Rantz,
//	    sells a knife and a chisel for ten coins and Fycie twenty-five feathers for fifty, both at stage 5 only and once.
//	10  Rantz's menu: "How do we make the chompys come?" sets 15 (the menu repeats, so it is taken once and then left).
//	15  The ogre chest in the cave (oploc1 Unlock, a Strength roll that drains one Strength level on a miss; stage 20) opens for fifty
//	    ticks and gives ONE pair of ogre bellows to an account that holds or banks none. The bellows are filled at the swamp bubbles by the
//	    pool (three charges) and used on a swamp toad: a bloated toad (three at the most; the fourth lets one go). A toad shown to Rantz
//	    (25: "where do we put them": the clearing south of him).
//	25  A bloated toad dropped in the clearing (inzone 0_41_46_7_18 to 0_41_46_15_27, i.e. 2631..2639, 2962..2971) is bait: the quest sets 30.
//	    Every 25 ticks it rolls one in five for a chompy bird (four rolls, then it bursts and hurts whoever is next to it for 1 or 2), and a
//	    chompy that comes shows (35) and Rantz shoots and misses (40) while the account is within twenty tiles of him.
//	40  "Come on, let me have a go" and "I'm actually quite strong" and Rantz lends the ogre bow (45). A lost bow is sold by Rantz for
//	    500 to 550 coins.
//	45  A chompy bird baited by the account (hunted, never fought back: it flees anything within two tiles) is shot with the ogre bow and
//	    the ogre arrows in the quiver (Ranged 30 for both): ten hitpoints. It dies, its body is plucked (Pluck on the dead chompy: raw chompy,
//	    bones and ten to twenty-nine feathers) and the raw chompy is picked up (50).
//	50  Rantz is shown the raw chompy (55).
//	55  The spit is used only by an account that has talked to Bugs and Fycie AFTER 55 (each tells what he wants and the choice is kept), with
//	    Cooking 30: the chompy goes on the empty spit with Rantz's, Bugs's and Fycie's seasonings in the pack, and is roasted (the roll is
//	    stat_random(cooking, 200, 255): about one in six burns at Cooking 30, which uses the seasonings up all the same). The three
//	    seasonings are known only from what the three say, so the module carries every candidate (potato or onion, equa leaves or cabbage,
//	    tomato or doogle leaves), as the TS def does.
//	60  The seasoned chompy handed to Rantz: the quest queue gives the rewards (65).
//
// HARD PARTS. (1) The wolves. The arrowtips come off wolf bones, and the only wolves the TS def and this port use are the Feldip Hills' six at
// 2602..2610,2955..2967 (npc 95: level 64, 69 hitpoints, Attack 50, Strength 55, Defence 52, a hit of up to 6, stab). They are the same wolves that
// stand a few tiles from the swamp bubbles the bellows are filled at, and a wolf hunts any account within two tiles, so the account is priced against
// one (FoeFeldipWolf, MeleeChain, the shared model) before the quest is begun and before every kill, and is parked six hours when the model loses:
// an account that cannot kill a wolf cannot make the arrows, and one that meets one at the pool cannot fill the bellows. (The desert wolves of the
// Kharid desert, level 27, drop the same bones; their country holds the Bedabin camp, the mining camp's mercenaries and the ugthanki and is not
// walked here.) (2) Nothing may be fought but wolves: chompy birds do not fight back. (3) The quest's boxes: Rantz's, Bugs's and Fycie's scripts
// raise ~objbox and ~doubleobjbox in the middle of the talk and each one holds the script until continued, so the talks are driven through
// questmods.chDrive, which clears a main-modal box between pages and takes each menu line once. (4) The raw chompy, the bellows and the toads
// are untradeable quest items: none is asked of the bank or the mule. (5) The hills' other hostiles. The wolves are not the only monsters on the ground the quest
// walks: eleven ogres (FoeOgre: level 53, 60 hitpoints, hits up to 7, huntrange 2) stand among them, one beside the onion patch and the tomato spawn (a stationary Pick),
// two beside the road from Yanille (its waypoints pass three tiles from one), and a leg from the equa leaves to the onion patch has a waypoint on a wolf's spawn tile. Only the ENCLAVE in the east (Rantz, the grove, the
// clearing, the spit, the potato, cabbage and equa patches and the cave: ChInEnclave) is clear of them, so a step whose walk starts or ends outside it (the pool, the west
// patches, the way from the bank) is priced against a wolf and against an ogre, each on its own, with the pack in hand NOW (ChGroundReady, DecideChompy's chGroundGate) and parked
// six hours when the model loses, and questmods tops the account up and walks those legs with TravelCautious.

// ChompyName is the quest tab's spelling (component 673, `questlist:chompybird`) and ChompyID the engine's id, content's own.
const (
	ChompyName = "Big Chompy Bird Hunting"
	ChompyID   = "chompybird"
)

// The stages the journal tells apart (ChParseJournal), named after the constants of quest_chompybird.constant they stand for. A stage that reads
// like its successor takes the lower number: 15 stands for 15 and 20, 30 for 30 and 35, and 40 and 45 are told apart by the bow in hand (the
// journal's own line about the bow shows only while the pack holds it).
const (
	ChNotStarted  = 0
	ChStarted     = 5
	ChGivenArrows = 10
	ChToadies     = 15
	ChShownToad   = 25
	ChDroppedToad = 30
	ChRantzMissed = 40
	ChGotBow      = 45
	ChKilled      = 50
	ChToldToCook  = 55
	ChCooked      = 60
	ChComplete    = 65
)

// The skills the quest asks for. Cooking 30 and Ranged 30 are the content's own (the spit, the bow and its arrows), Fletching 5 the shafts and the
// tips, Crafting 5 the tips (ogre_arrows.rs2). The journal lists the first three; the fourth is enforced by the chisel.
const (
	ChCooking   = 30
	ChRanged    = 30
	ChFletching = 5
	ChCrafting  = 5
)

// Object ids from content/pack/obj.pack (checked against the production config by TestChompyIDsMatchTheProductionConfig in questmods).
const (
	ObjChLogs          = 2862 // [achey_tree_logs]
	ObjChShaft         = 2864 // [ogre_arrow_shaft], stackable
	ObjChFlighted      = 2865 // [ogre_headless_arrow], "Flighted ogre arrow", stackable
	ObjChWolfBones     = 2859 // [wolf_bones]
	ObjChTips          = 2861 // [wolfbone_arrowheads], "Wolfbone arrowtips", stackable
	ObjChArrow         = 2866 // [ogre_arrow], stackable, wielded in the quiver
	ObjChBow           = 2883 // [ogre_bow]
	ObjChBellowsEmpty  = 2871 // [empty_ogre_bellows]
	ObjChBellows3      = 2872 // [filled_ogre_bellow3]
	ObjChBellows2      = 2873
	ObjChBellows1      = 2874
	ObjChToad          = 2875 // [bloated_toad]
	ObjChRawChompy     = 2876 // [raw_chompy]
	ObjChRuinedChompy  = 2880 // [ruined_chompy]
	ObjChSeasonedChomp = 2882 // [cooked_s_chompy], "Seasoned chompy"
	ObjChFeather       = 314
	ObjChChisel        = 1755
	ObjChPotato        = 1942
	ObjChOnion         = 1957
	ObjChCabbage       = 1965
	ObjChTomato        = 1982
	ObjChDoogle        = 1573 // [doogleleaves]
	ObjChEqua          = 2128 // [equa_leaves]
)

// Npc ids from content/pack/npc.pack.
const (
	NpcChRantz       = 1010
	NpcChFycie       = 1011
	NpcChBugs        = 1012
	NpcChToad        = 1013 // [toad], "Swamp toad": the ones the bellows are used on
	NpcChChompy      = 1015 // [chompy_bird]: op5 Attack
	NpcChChompyDead  = 1016 // [chompybird_dead]: op4 Pluck
	NpcFeldipWolf    = 95   // [wolf]
	NpcChBloatedToad = 1014 // [bloated_toad], the bait on the ground
)

// Loc ids from content/pack/loc.pack and the baked index.
const (
	LocChChestShut = 3377 // [chompybird_chest], "Locked Ogre chest": Unlock
	LocChChestOpen = 3378 // [chompybird_chest_open]: Search; shut again after fifty ticks
	LocChSpitEmpty = 3375 // [chompybird_spitroast_empty]: only this form answers a use of the raw chompy
	LocChBubbles   = 684  // [swampbubbles]: a use of the bellows
	LocChAchey     = 2023 // [achey_tree]: Chop
	LocChPotato    = 312  // [potato]: Pick (op 2)
	LocChCabbage   = 1161 // [cabbage]: Pick
	LocChOnion     = 3366 // [onion]: Pick
	LocChCaveMouth = 3379 // [rantzogrecaveentrance]: Enter, a 3 by 2 mouth at 2629,2998
)

// Tiles from the content maps, the loc index and the collision pack.
var (
	// ChRantzAnchor is Rantz's spawn (m40_46 "0 54 37: 1010"); he wanders two tiles.
	ChRantzAnchor = tile.Tile{X: 2630, Z: 2981}
	// ChBugsAnchor and ChFycieAnchor are the children's spawns in the cave under him (m41_146).
	ChBugsAnchor  = tile.Tile{X: 2640, Z: 9391}
	ChFycieAnchor = tile.Tile{X: 2650, Z: 9393}
	// ChChestAt is the locked chest (2637,9398, 2 by 1, angle 2) and ChChestNear a tile in front of it.
	ChChestAt   = tile.Tile{X: 2637, Z: 9398}
	ChChestNear = tile.Tile{X: 2638, Z: 9397}
	// ChSpitAt is the spit (loc 3375 at 2630,2990, 2 by 1) and ChSpitNear a tile beside it.
	ChSpitAt   = tile.Tile{X: 2630, Z: 2990}
	ChSpitNear = tile.Tile{X: 2630, Z: 2989}
	// ChBubbleAt is the one swamp bubble the bellows can reach, ChBubbleStand the tile south of it at the pool's lip; the other five
	// bubbles have no standable neighbour.
	ChBubbleAt    = tile.Tile{X: 2598, Z: 2963}
	ChBubbleStand = tile.Tile{X: 2598, Z: 2962}
	// ChBaitSpot is the middle of the clearing Rantz points at.
	ChBaitSpot = tile.Tile{X: 2636, Z: 2966}
	// ChGrove is the middle of the achey trees by Rantz.
	ChGrove = tile.Tile{X: 2626, Z: 2974}
	// ChWolfField is the middle of the Feldip wolves (m40_46 "0 38 59: 95" .. "0 46 61: 95"), ChWolfRadius how far from it a wolf is hunted.
	ChWolfField  = tile.Tile{X: 2607, Z: 2962}
	ChWolfRadius = 8
	// The seasonings' places: the patches (loc ids above) and the ground spawns (m40_46 / m41_46 objs).
	ChPotatoPatch  = tile.Tile{X: 2641, Z: 2958}
	ChCabbagePatch = tile.Tile{X: 2645, Z: 2958}
	ChOnionPatch   = tile.Tile{X: 2583, Z: 2966}
	ChEquaSpawn    = tile.Tile{X: 2639, Z: 2952}
	ChTomatoSpawn  = tile.Tile{X: 2584, Z: 2966}
	ChDoogleSpawn  = tile.Tile{X: 2562, Z: 2972}
)

// ChBaitZone is the clearing the quest accepts bait in (bloated_toad.rs2 opheld1: inzone(0_41_46_7_18, 0_41_46_15_27)), and ChRantzReach how far from
// Rantz the account must stay for him to shoot (chompy_bird.rs2 ai_queue11/13: distance under 20).
var ChBaitZone = Rect{MinX: 2631, MaxX: 2639, MinZ: 2962, MaxZ: 2971}

// ChInBaitZone reports a tile of the bait clearing.
func ChInBaitZone(t tile.Tile) bool { return ChBaitZone.Contains(t) }

// ChInCave reports a tile of Rantz's cave, the underground piece the cave mouth leads to.
func ChInCave(t tile.Tile) bool { return t.Z >= 9340 && t.Z <= 9410 && t.X >= 2620 && t.X <= 2690 }

// The arrows. ChArrowTarget is what the pack is brought to: six for Rantz and six to shoot with; ChGiveArrows what Rantz takes ("inv_total(ogre_arrow) < 6"
// is "not enough") and ChHuntArrows the pack's least for a hunt (the quiver has no count on the wire, so a quivered stack is trusted).
const (
	ChArrowTarget = 12
	ChGiveArrows  = 6
	ChHuntArrows  = 6
	// ChFeathersPerArrow is the feathers a flighted shaft takes, ChToadsMax the toads the pack may hold, ChSeasoningCount the candidates carried.
	ChFeathersPerArrow = 4
	ChToadsMax         = 3
	ChSeasoningCount   = 6
	// ChCoins is what the pack is brought to when it holds fewer than ChCoinsMin: a lost bow is bought back for up to 550.
	ChCoins    = 800
	ChCoinsMin = 150
	// ChFoodAsk is the sharks the kit asks of the bank and the mule for the wolves (optional: the price reads what the pack holds).
	ChFoodAsk = 8
)

// FoeFeldipWolf is [wolf] (npc 95, the Feldip Hills' six, content scripts/_unpack/225/all.npc): level 64, Attack 50, Strength 55, Defence 52, 69
// hitpoints, no attackrate (4), no bonus params (a hit of up to 6), stab, huntmode cowardly with huntrange 2 (an account of combat under 128 within
// two tiles with a line of sight is hunted).
var FoeFeldipWolf = MeleeFoe{Name: "Wolf", Attack: 50, Strength: 55, Defence: 52, Magic: 1, Hitpoints: 69, AttackTicks: 4}

// FoeOgre is [ogre] (npc 114 = NpcOgre, content scripts/areas/area_yanille/configs/ogre.npc): level 53, attack 43, defence 43, strength 43, 60 hitpoints, attackrate 6,
// attackbonus 22, strengthbonus 20 (a hit of up to 7, crush), huntmode cowardly with huntrange 2, wanderrange 3 and maxrange 5. Eleven of them stand in the Feldip hills (m39_46 and m40_46,
// ChOgreSpawns), two of them on the road from Yanille to Rantz and one beside the onion patch and the tomato spawn, where the seasonings are picked.
var FoeOgre = MeleeFoe{Name: "Ogre", Attack: 43, Strength: 43, Defence: 43, Magic: 1, Hitpoints: 60, AttackTicks: 6, AttackBonus: 22, StrengthBonus: 20}

// The hostile ground of the Feldip hills. ChWolfSpawns and ChOgreSpawns are the spawn tiles of the maps (m40_46 is the square at 2560,2944: "0 42 11: 95" is the wolf at
// 2602,2955, "0 24 23: 114" the ogre at 2584,2967; m39_46, the square west of it, holds the three westernmost ogres), ChWolfReach and ChOgreReach how far from its spawn a hostile can stand and still hunt an account (its wander, the wolf's
// wanderrange 6 and the ogre's maxrange 5, and the huntrange of 2 they share).
var (
	ChWolfSpawns = []tile.Tile{{X: 2602, Z: 2955}, {X: 2605, Z: 2963}, {X: 2607, Z: 2967}, {X: 2610, Z: 2958}, {X: 2610, Z: 2961}, {X: 2610, Z: 2965}}
	ChOgreSpawns = []tile.Tile{{X: 2549, Z: 2979}, {X: 2551, Z: 2955}, {X: 2552, Z: 2961}, {X: 2568, Z: 2991}, {X: 2574, Z: 2988}, {X: 2584, Z: 2967}, {X: 2585, Z: 2977}, {X: 2592, Z: 2965}, {X: 2608, Z: 2980}, {X: 2609, Z: 2990}, {X: 2614, Z: 2998}}
)

const (
	ChWolfReach = 8
	ChOgreReach = 7
)

// The enclave is the east of the hills where the quest does its work: Rantz, the achey grove, the bait clearing, the spit, the potato, cabbage and equa leaves, and the cave under
// them. No hostile reaches it (the easternmost, the ogre at 2614,2998, reaches to 2621: ChEnclaveWest is the next column), and the walks between its places keep clear of the
// hostiles on the real pack (the questmods pack test plans every pair). Everything else is hostile ground or the way to it: the pool, the west patches, the road from Yanille.
const (
	ChEnclaveWest  = 2622
	ChEnclaveEast  = 2665
	ChEnclaveSouth = 2940
	ChEnclaveNorth = 3006
)

// ChInEnclave reports a tile of the enclave: the surface box of the east side, or Rantz's cave.
func ChInEnclave(t tile.Tile) bool {
	if ChInCave(t) {
		return true
	}
	return t.Level == 0 && t.X >= ChEnclaveWest && t.X <= ChEnclaveEast && t.Z >= ChEnclaveSouth && t.Z <= ChEnclaveNorth
}

// ChLegExposed reports a walk that is not wholly inside the enclave: it starts or ends on hostile ground, or on the road to it.
func ChLegExposed(from, to tile.Tile) bool { return !(ChInEnclave(from) && ChInEnclave(to)) }

// ChHostileNear reports a tile within a wolf's or an ogre's reach of its spawn.
func ChHostileNear(t tile.Tile) bool {
	if t.Level != 0 {
		return false
	}
	for _, w := range ChWolfSpawns {
		if tile.Chebyshev(t, w) <= ChWolfReach {
			return true
		}
	}
	for _, o := range ChOgreSpawns {
		if tile.Chebyshev(t, o) <= ChOgreReach {
			return true
		}
	}
	return false
}

// chFoodIDs are the foods the quest eats (Module.Sustain; the eat loop's floor bite takes any food the pack carries).
var chFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// chSustain is the quest's survival policy: the foods it carries and the floor its eat line never goes under (a wolf's hardest hit is 6, so FoeEatLine
// raises it to 12 and the floor holds).
var chSustain = Sustain{FoodIDs: chFoodIDs, EatAt: 14}

// ChSeasoningRef is one seasoning: its object id and name, the patch's loc id (0 for a ground spawn) and the place it is worked from.
type ChSeasoningRef struct {
	ID   int
	Name string
	Loc  int
	At   tile.Tile
}

// chSeasonings are the six candidates, in the order the walk takes them (the east patches, the east ground, the west patch and ground).
var chSeasonings = []ChSeasoningRef{
	{ObjChPotato, "Potato", LocChPotato, ChPotatoPatch},
	{ObjChCabbage, "Cabbage", LocChCabbage, ChCabbagePatch},
	{ObjChEqua, "Equa leaves", 0, ChEquaSpawn},
	{ObjChOnion, "Onion", LocChOnion, ChOnionPatch},
	{ObjChTomato, "Tomato", 0, ChTomatoSpawn},
	{ObjChDoogle, "Doogle leaves", 0, ChDoogleSpawn},
}

// ChSeasoningList returns the six seasonings in the order they are fetched (a copy).
func ChSeasoningList() []ChSeasoningRef { return append([]ChSeasoningRef(nil), chSeasonings...) }

// chBellowsIDs are the four forms of the ogre bellows, empty first; chFilledBellows the ones with gas in them.
var (
	chBellowsIDs    = []int{ObjChBellowsEmpty, ObjChBellows3, ObjChBellows2, ObjChBellows1}
	chFilledBellows = []int{ObjChBellows3, ObjChBellows2, ObjChBellows1}
)

// ChBellowsIDs returns the ids of the four bellows (a copy), ChFilledBellowsIDs those that hold gas.
func ChBellowsIDs() []int       { return append([]int(nil), chBellowsIDs...) }
func ChFilledBellowsIDs() []int { return append([]int(nil), chFilledBellows...) }

// ChBellowsOf is the id of the bellows in the pack (the fullest first), 0 for none.
func ChBellowsOf(s Snapshot) int {
	for _, id := range []int{ObjChBellows3, ObjChBellows2, ObjChBellows1, ObjChBellowsEmpty} {
		if s.HeldID(id) > 0 {
			return id
		}
	}
	return 0
}

// ChBankedBellows is the id of the bellows in the bank (a bank not read is none), 0 for none.
func ChBankedBellows(s Snapshot) int {
	if !s.BankKnown {
		return 0
	}
	for _, id := range chBellowsIDs {
		if s.BankIDs[id] > 0 {
			return id
		}
	}
	return 0
}

// chToolIDs are the quest's own items, what the floor bite never eats and every bank step of the quest leaves in the pack. The seasonings are listed: a
// cabbage and a tomato can be eaten, and the walk's floor bite takes any food a pack carries.
var chToolIDs = func() []int {
	tools := []int{ObjChLogs, ObjChShaft, ObjChFlighted, ObjChWolfBones, ObjChTips, ObjChArrow, ObjChBow, ObjChToad, ObjChRawChompy, ObjChRuinedChompy,
		ObjChSeasonedChomp, ObjChFeather, ObjChChisel, ObjKnife, CoinsID, ObjChPotato, ObjChOnion, ObjChCabbage, ObjChTomato, ObjChDoogle, ObjChEqua}
	tools = append(tools, chBellowsIDs...)
	return append(tools, Axes...)
}()

// chKeep is what every bank step of the quest leaves in the pack: the tools and the foods.
var chKeep = append(append([]int(nil), chToolIDs...), chFoodIDs...)

// ChKeepIDs returns the keep list (a copy); ChToolIDs the module's tools (a copy).
func ChKeepIDs() []int { return append([]int(nil), chKeep...) }
func ChToolIDs() []int { return append([]int(nil), chToolIDs...) }

// Custom step names (internal/scripts/questmods).
const (
	CustomChBugs       = "ch-bugs"        // the knife and the chisel from Bugs (stage 5)
	CustomChChop       = "ch-chop"        // one achey log cut
	CustomChWolf       = "ch-wolf"        // one wolf killed and its bones taken
	CustomChGive       = "ch-give"        // Rantz takes six arrows and answers the toadies menu
	CustomChToadies    = "ch-toadies"     // Rantz's toadies menu
	CustomChShowToad   = "ch-show-toad"   // a bloated toad shown to Rantz
	CustomChBow        = "ch-bow"         // the ogre bow from Rantz
	CustomChChest      = "ch-chest"       // the rock lifted, the chest searched
	CustomChToad       = "ch-toad"        // the bellows filled and the toads inflated
	CustomChBait       = "ch-bait"        // a toad put down and Rantz's miss waited for
	CustomChShoot      = "ch-shoot"       // bait, a shot chompy, the carcass plucked and taken
	CustomChShowChompy = "ch-show-chompy" // the raw chompy shown to Rantz
	CustomChKids       = "ch-kids"        // Bugs and Fycie asked what they want
	CustomChSeason     = "ch-season"      // the next missing seasoning
	CustomChCook       = "ch-cook"        // the chompy on the spit
	CustomChHandOver   = "ch-hand-over"   // the seasoned chompy to Rantz
)

// KitChompy is the provisioning round's name.
const KitChompy = "ch-kit"

// The park times: ChSkillParkFor is how long the quest skips an account under one of its levels, ChFightParkFor one the model says loses to a wolf (its levels
// do not change in an hour), ChItemParkFor one whose bank, mule and shops cannot give what the kit asks, ChStuckParkFor a leg that could not be done.
const (
	ChSkillParkFor = 6 * time.Hour
	ChFightParkFor = 6 * time.Hour
	ChItemParkFor  = 3 * time.Hour
	ChStuckParkFor = 3 * time.Hour
)

// The stops. ChRantzStop's preference is the quest's start; every other talk with him names its own script (questmods).
var (
	ChRantzStop = NpcStop{Npc: "Rantz", ID: NpcChRantz, Anchor: ChRantzAnchor, Leash: 6,
		Prefer: []string{"Ok, I'll make you some 'stabbers'", "Ok, thanks"}}
	ChBugsStop = NpcStop{Npc: "Bugs", ID: NpcChBugs, Anchor: ChBugsAnchor, Leash: 6,
		Prefer: []string{"Ok, I'll give you 10 bright pretties", "Ok, thanks"}}
	ChFycieStop = NpcStop{Npc: "Fycie", ID: NpcChFycie, Anchor: ChFycieAnchor, Leash: 6,
		Prefer: []string{"Ok, I'll give you 50 bright pretties", "Ok, thanks"}}
)

// chMarkers are the journal's markers, newest stage first: chompybird_journal.rs2 keeps every earlier paragraph on the page, so the newest one decides.
// Struck-through lines read like the others (colour tags are removed).
var chMarkers = []struct {
	stage int
	text  string
}{
	{ChComplete, "quest complete!"},
	{ChCooked, "i've cooked the chompy"},
	{ChToldToCook, "i showed rantz the chompy"},
	{ChKilled, "i've killed a chompy bird!"},
	{ChGotBow, "has lent me an ogre bow"},
	{ChRantzMissed, "doesn't seem to be a very good shot"},
	{ChDroppedToad, "i dropped a fatsy toady on the spot"},
	{ChShownToad, "i managed to trap some 'fatsy toadies'"},
	{ChToadies, "rantz told me 'fatsy toadies' grow fat on swamp gas"},
	{ChGivenArrows, "it turned out that 'stabbers' are a kind of specially made"},
	{ChStarted, "i agreed to get rants some 'stabbers'"},
	{ChNotStarted, "i can start this quest by speaking to the ogre"},
}

// ParseChompyJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseChompyJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range chMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// ChSkillShort says which of the quest's skills is under its level (levels the snapshot has not read are not judged), "" for none.
func ChSkillShort(s Snapshot) string {
	var short []string
	for _, c := range []struct {
		skill string
		level int
	}{{"ranged", ChRanged}, {"cooking", ChCooking}, {"fletching", ChFletching}, {"crafting", ChCrafting}} {
		if lvl := s.Level(c.skill); lvl > 0 && lvl < c.level {
			short = append(short, fmt.Sprintf("%s %d/%d", skillLabel(c.skill), lvl, c.level))
		}
	}
	return strings.Join(short, ", ")
}

// ChFightVerdict is ChWolfReady's answer.
type ChFightVerdict struct {
	OK  bool
	Why string
}

// ChWolfReady judges whether the pack can take one Feldip wolf: the account's own swing (MeleeChain: the shared fight step melees) from the level a fight starts
// at (FightStartHP), eating by the foe's eat line, and it must win with a tenth of its bites left for the way out and never fall under the wolf's hardest hit.
// The reason names the account's levels and the food it holds, for the log.
func ChWolfReady(s Snapshot) ChFightVerdict {
	bites := len(FoodBites(s.InvIDs, chFoodIDs))
	if bites == 0 {
		return ChFightVerdict{Why: fmt.Sprintf("no food in the pack for a Feldip wolf (%d hitpoints, hits up to %d)", FoeFeldipWolf.Hitpoints, FoeFeldipWolf.MaxHit())}
	}
	odds := MeleeChain(s, []MeleeFoe{FoeFeldipWolf}, chSustain)
	if odds.OK {
		return ChFightVerdict{OK: true}
	}
	return ChFightVerdict{Why: fmt.Sprintf("%s (a Feldip wolf: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
		odds.Why, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
}

// ChGroundReady judges whether the pack can take what stands on the hills' hostile ground: a Feldip wolf, and an ogre, each on its own, by the same model as ChWolfReady (the
// account's own swing from the level a fight starts at, eating by the foe's eat line, winning with a tenth of its bites left and never falling under the foe's hardest hit). The
// ogre is judged separately because its hardest hit (7) is the wolf's (6) and one more, and a pack that has just beaten the wolf may not beat it again. It is asked of the pack in
// hand each time, not once: by the seasonings the food may be half spent. The pool's lip is a few tiles from the wolves, the onion patch and the tomato spawn one tile from an
// ogre, and the road from Yanille passes three tiles from another.
func ChGroundReady(s Snapshot) ChFightVerdict {
	bites := len(FoodBites(s.InvIDs, chFoodIDs))
	if bites == 0 {
		return ChFightVerdict{Why: fmt.Sprintf("no food in the pack for a Feldip wolf (%d hitpoints, hits up to %d) or an ogre (%d hitpoints, hits up to %d)",
			FoeFeldipWolf.Hitpoints, FoeFeldipWolf.MaxHit(), FoeOgre.Hitpoints, FoeOgre.MaxHit())}
	}
	for _, foe := range []MeleeFoe{FoeFeldipWolf, FoeOgre} {
		if odds := MeleeChain(s, []MeleeFoe{foe}, chSustain); !odds.OK {
			return ChFightVerdict{Why: fmt.Sprintf("%s (a %s of the Feldip hills: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
				odds.Why, strings.ToLower(foe.Name), s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
		}
	}
	return ChFightVerdict{OK: true}
}

// ChGroundPeakHit is the hardest single hit the hills' hostiles land, the figure a top-up before a leg is made for (FightStartHP).
func ChGroundPeakHit() int { return max(FoeFeldipWolf.PeakHit(), FoeOgre.PeakHit()) }

// chStepDest is where a step takes the account, ok false for a step that goes nowhere the hills' hostile ground matters: a bank step (the kit round's legs are the engine's own,
// made with TravelCautious, and an account with no food must be able to fetch some), a use of one item on another, a wait. A toad is caught at the pool, so a bait or a shot
// that holds none is bound for the pool's lip.
func chStepDest(s Snapshot, st Step) (tile.Tile, bool) {
	switch st.Kind {
	case StepTalk:
		return st.Stop.Anchor, true
	case StepWalk:
		return st.Anchor, true
	case StepCustom:
	default:
		return tile.Tile{}, false
	}
	switch st.Custom {
	case CustomChBugs, CustomChKids:
		return ChBugsAnchor, true
	case CustomChChop:
		return ChGrove, true
	case CustomChWolf:
		return ChWolfField, true
	case CustomChGive, CustomChToadies, CustomChShowToad, CustomChBow, CustomChShowChompy, CustomChHandOver:
		return ChRantzAnchor, true
	case CustomChChest:
		return ChChestNear, true
	case CustomChToad:
		return ChBubbleStand, true
	case CustomChBait, CustomChShoot:
		if s.HeldID(ObjChToad) == 0 {
			return ChBubbleStand, true
		}
		return ChBaitSpot, true
	case CustomChSeason:
		for _, c := range chSeasonings {
			if c.ID == st.ItemID {
				return c.At, true
			}
		}
	case CustomChCook:
		return ChSpitNear, true
	}
	return tile.Tile{}, false
}

// chGroundGate parks a step that takes the account onto the hills' hostile ground (or along the road to it, or back from it) with a pack the model says loses to a wolf and an
// ogre. Only the walks inside the enclave are free of it: Rantz, the grove, the clearing, the spit, the east patches and the cave.
func chGroundGate(s Snapshot, st Step) Step {
	dest, ok := chStepDest(s, st)
	if !ok || !ChLegExposed(s.Tile, dest) {
		return st
	}
	if v := ChGroundReady(s); !v.OK {
		return chPark(ChFightParkFor, fmt.Sprintf("not going onto the Feldip hills' hostile ground (%s) with this pack: %s", st.Reason, v.Why))
	}
	return st
}

// chArrows is the arrows held.
func chArrows(s Snapshot) int { return s.HeldID(ObjChArrow) }

// ChHasBow reports the ogre bow in the pack or worn.
func ChHasBow(s Snapshot) bool { return s.HeldID(ObjChBow) > 0 || s.WornIDs[ObjChBow] > 0 }

// ChHasHuntArrows reports arrows enough to shoot with: six in the pack, or a stack in the quiver.
func ChHasHuntArrows(s Snapshot) bool {
	return chArrows(s) >= ChHuntArrows || s.WornIDs[ObjChArrow] > 0
}

// ChHasTools reports the knife and the chisel both in the pack.
func ChHasTools(s Snapshot) bool { return s.HeldID(ObjKnife) > 0 && s.HeldID(ObjChChisel) > 0 }

// ChFeathersNeeded is the feathers the arrows still to be made take: four a flighted shaft, for every arrow the target lacks that is not flighted yet.
func ChFeathersNeeded(s Snapshot) int {
	return ChFeathersPerArrow * max(0, ChArrowTarget-chArrows(s)-s.HeldID(ObjChFlighted))
}

// chNeedsArrows reports that the arrow pipeline has work: the quest is at the arrows' stage, or at a hunt that still needs a shot and has no arrow to shoot with
// (a raw chompy in the pack, or a seasoned one, means no shot is wanted).
func chNeedsArrows(s Snapshot) bool {
	if s.Journal == StatusNotStarted || (s.StageKnown && s.Stage <= ChStarted) {
		return chArrows(s) < ChArrowTarget
	}
	if !s.StageKnown || s.Stage < ChRantzMissed || s.Stage > ChCooked || ChHasHuntArrows(s) {
		return false
	}
	return s.HeldID(ObjChRawChompy) == 0 && s.HeldID(ObjChSeasonedChomp) == 0
}

// chNeedsChop reports that the next arrow move is a tree: no flighted shaft is waiting for its tip, and the shafts and logs in hand fall short of a batch.
func chNeedsChop(s Snapshot) bool {
	remaining := ChArrowTarget - chArrows(s)
	return remaining > 0 && s.HeldID(ObjChFlighted) == 0 && s.HeldID(ObjChLogs) == 0 && s.HeldID(ObjChShaft) < min(ChGiveArrows, remaining)
}

// chKitLines is the kit the rest of the work wants in the pack from where the quest stands: coins, an axe and the feathers while arrows are to be made,
// the tools if the bank or the mule has them (Bugs sells them otherwise), and sharks for the wolves. Only the coins, the axe and the feathers are required:
// everything else is taken when a source has it and never waited for.
func chKitLines(s Snapshot) []KitLine {
	var kit []KitLine
	if s.HeldID(CoinsID) < ChCoinsMin {
		kit = append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: ChCoins, Min: ChCoinsMin, Stackable: true})
	}
	if chNeedsArrows(s) {
		if !ChHasTools(s) {
			if s.HeldID(ObjKnife) == 0 {
				kit = append(kit, KitLine{ID: ObjKnife, Name: "Knife", Qty: 1, Optional: true})
			}
			if s.HeldID(ObjChChisel) == 0 {
				kit = append(kit, KitLine{ID: ObjChChisel, Name: "Chisel", Qty: 1, Optional: true})
			}
		}
		// An axe is wanted only while shafts are still to be cut from logs.
		if chNeedsChop(s) && !AxeHeld(s) {
			kit = append(kit, AxeLine(s))
		}
		if need := ChFeathersNeeded(s); need > s.HeldID(ObjChFeather) {
			kit = append(kit, KitLine{ID: ObjChFeather, Name: "Feather", Qty: need, Stackable: true,
				Sources: []Source{{Kind: SourceShop, Shop: FluffsSardineShop, Price: 2, Note: "Gerrant's Fishing Supplies in Port Sarim keeps a thousand"}}})
		}
	}
	if len(FoodBites(s.InvIDs, chFoodIDs)) < ChFoodAsk {
		kit = append(kit, KitLine{ID: ObjShark, Name: "Shark", Qty: ChFoodAsk, Optional: true})
	}
	return kit
}

// chRequiredShort lists the kit's required lines the pack is short of; chOptionalShort reports an optional one it is short of.
func chRequiredShort(s Snapshot, kit []KitLine) []KitLine {
	var short []KitLine
	for _, l := range kit {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			short = append(short, l)
		}
	}
	return short
}

func chOptionalShort(s Snapshot, kit []KitLine) bool {
	for _, l := range kit {
		if l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			return true
		}
	}
	return false
}

func chNames(lines []KitLine) string {
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		names = append(names, strings.ToLower(l.Name))
	}
	return strings.Join(names, ", ")
}

func chCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

func chPark(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, ParkFor: d, Reason: reason}
}

func chProvision(kit []KitLine, reason string) Step {
	return Step{Kind: StepProvision, KitName: KitChompy, Kit: kit, KeepIDs: chKeep, Bank: YanilleBank, BankSet: true, Reason: reason}
}

// chKitStep is the provisioning round the work still wants, if any: the required lines first (and a wait that parks the quest once every source of one is
// dead), then the optional ones once a run.
func chKitStep(s Snapshot) (Step, bool) {
	kit := chKitLines(s)
	if short := chRequiredShort(s, kit); len(short) > 0 {
		if s.Blocked["provision:"+KitChompy] {
			return chPark(ChItemParkFor, "no "+chNames(short)+" from the bank, the mule or the shops: the arrows and the hunt cannot be done without"), true
		}
		return chProvision(kit, "the hunt's kit ("+chNames(short)+")"), true
	}
	if chOptionalShort(s, kit) && !s.Ran["provision:"+KitChompy] && !s.Failed["provision:"+KitChompy] {
		return chProvision(kit, "the knife, the chisel and the food, if the bank or the mule has them"), true
	}
	return Step{}, false
}

// chUseOn is an item used on an item in the pack.
func chUseOn(item, target int, itemName, targetName, reason string) Step {
	return Step{Kind: StepUseOn, ItemID: item, Item: itemName, TargetKind: TargetItem, TargetID: target, Target: targetName, Reason: reason}
}

// chArrowNext is the next move toward ChArrowTarget ogre arrows, ok false once they are held. The shafts are built up first (a use of feathers caps at six),
// then flighted, and the tips come off a wolf's bones one wolf at a time, each one priced before it is fought.
func chArrowNext(s Snapshot) (Step, bool) {
	arrows := chArrows(s)
	remaining := ChArrowTarget - arrows
	if remaining <= 0 {
		return Step{}, false
	}
	flighted, tips, shafts, feathers, logs, bones := s.HeldID(ObjChFlighted), s.HeldID(ObjChTips), s.HeldID(ObjChShaft), s.HeldID(ObjChFeather),
		s.HeldID(ObjChLogs), s.HeldID(ObjChWolfBones)
	switch {
	case flighted > 0 && tips > 0:
		return chUseOn(ObjChTips, ObjChFlighted, "Wolfbone arrowtips", "Flighted ogre arrow", "the tips on the flighted shafts: ogre arrows"), true
	case flighted > 0:
		if bones > 0 {
			return chUseOn(ObjChChisel, ObjChWolfBones, "Chisel", "Wolf bones", "the chisel on a wolf's bones: arrowtips"), true
		}
		if v := ChWolfReady(s); !v.OK {
			return chPark(ChFightParkFor, "not going after a Feldip wolf's bones with this pack: "+v.Why), true
		}
		if s.Failed[CustomChWolf] {
			return chPark(ChStuckParkFor, "no Feldip wolf could be killed for its bones"), true
		}
		return chCustom(CustomChWolf, "kill a Feldip wolf for its bones"), true
	}
	batch := min(ChGiveArrows, remaining)
	if shafts < batch {
		if logs > 0 {
			return chUseOn(ObjKnife, ObjChLogs, "Knife", "Achey tree logs", "the knife on the achey logs: arrow shafts"), true
		}
		if s.Failed[CustomChChop] {
			return chPark(ChStuckParkFor, "no achey tree could be chopped for logs"), true
		}
		return chCustom(CustomChChop, "chop an achey tree for logs"), true
	}
	if feathers >= ChFeathersPerArrow {
		return chUseOn(ObjChFeather, ObjChShaft, "Feather", "Ogre arrow shaft", "the feathers on the shafts: flighted shafts"), true
	}
	return chPark(ChItemParkFor, "no feathers to flight the ogre arrow shafts with"), true
}

// chBellowsStep gets the account a pair of ogre bellows, nil once it holds one: the chest in the cave first (it holds one pair, and none for an account that holds
// or banks any), then the bank when the chest said so.
func chBellowsStep(s Snapshot) *Step {
	if ChBellowsOf(s) != 0 {
		return nil
	}
	if !s.Failed[CustomChChest] {
		st := chCustom(CustomChChest, "the ogre bellows from the chest in Rantz's cave")
		return &st
	}
	if !s.BankKnown {
		st := Step{Kind: StepScanBank, Bank: YanilleBank, BankSet: true, Reason: "the chest holds no bellows: look in the bank"}
		return &st
	}
	if id := ChBankedBellows(s); id != 0 {
		st := Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Ogre bellows", Qty: 1, ID: id}}, Bank: YanilleBank, BankSet: true, KeepIDs: chKeep,
			Reason: "the pair of bellows the chest says the bank holds"}
		return &st
	}
	st := chPark(ChStuckParkFor, "no ogre bellows from the chest or the bank")
	return &st
}

// chShootingLeg is what a shot needs and the pack may lack: the bow, the arrows (made again when the pack and the quiver hold none), the bellows. nil once
// the pack can go and shoot.
func chShootingLeg(s Snapshot) *Step {
	if !ChHasBow(s) {
		if s.Failed[CustomChBow] {
			st := chPark(ChStuckParkFor, "Rantz would not lend or sell the account an ogre bow")
			return &st
		}
		st := chCustom(CustomChBow, "the ogre bow from Rantz")
		return &st
	}
	if !ChHasHuntArrows(s) {
		if !ChHasTools(s) {
			st := chPark(ChItemParkFor, "no knife or chisel to make more ogre arrows with")
			return &st
		}
		if st, ok := chArrowNext(s); ok {
			return &st
		}
	}
	return chBellowsStep(s)
}

// chCookLeg is what the roast needs: the children asked after the chompy was shown, all six seasonings, a raw chompy, then the spit.
func chCookLeg(s Snapshot) Step {
	// The children's choices are kept (varp bits) once they are made after the chompy was shown, so a roast at stage 60 does not ask again.
	if s.Stage == ChToldToCook && !s.Ran[CustomChKids] {
		if s.Failed[CustomChKids] {
			return chPark(ChStuckParkFor, "Bugs and Fycie would not say what they want on the chompy")
		}
		return chCustom(CustomChKids, "ask Bugs and Fycie what they want on the chompy")
	}
	for _, c := range chSeasonings {
		if s.HeldID(c.ID) == 0 {
			if s.Failed[CustomChSeason] {
				return chPark(ChStuckParkFor, "no "+strings.ToLower(c.Name)+" could be had for the chompy")
			}
			return Step{Kind: StepCustom, Custom: CustomChSeason, ItemID: c.ID, Reason: "the " + strings.ToLower(c.Name) + " for the chompy"}
		}
	}
	if s.HeldID(ObjChRawChompy) == 0 {
		return chHuntStep(s)
	}
	if s.Failed[CustomChCook] {
		return chPark(ChStuckParkFor, "the ogre spit would not take the chompy")
	}
	return chCustom(CustomChCook, "roast the chompy on the ogre spit")
}

// chHuntStep is the shot at a chompy: the shooting leg's missing pieces first.
func chHuntStep(s Snapshot) Step {
	if st := chShootingLeg(s); st != nil {
		return *st
	}
	if s.Failed[CustomChShoot] {
		return chPark(ChStuckParkFor, "no chompy bird was baited, shot and plucked")
	}
	return chCustom(CustomChShoot, "bait, shoot and pluck a chompy bird")
}

// chRantzTalk is a custom talk with Rantz, parked when the last one failed.
func chRantzTalk(s Snapshot, name, reason string) Step {
	if s.Failed[name] {
		return chPark(ChStuckParkFor, "Rantz's conversation ("+reason+") did not go through")
	}
	return chCustom(name, reason)
}

// DecideChompy is the whole quest, read from the journal's stage, the account's tile and the pack: decideChompy's step, parked when it takes the account across the hills'
// hostile ground with a pack that loses to what stands there (chGroundGate).
func DecideChompy(s Snapshot) Step {
	return chGroundGate(s, decideChompy(s))
}

func decideChompy(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	if why := ChSkillShort(s); why != "" {
		return chPark(ChSkillParkFor, "Big Chompy Bird Hunting wants Ranged and Cooking 30, Fletching and Crafting 5: "+why)
	}
	if s.Journal == StatusNotStarted {
		if st, ok := chKitStep(s); ok {
			return st
		}
		if v := ChWolfReady(s); !v.OK {
			return chPark(ChFightParkFor, "not starting Big Chompy Bird Hunting: its arrows come off a Feldip wolf's bones, and "+v.Why)
		}
		return Step{Kind: StepTalk, Stop: ChRantzStop, Reason: "start Big Chompy Bird Hunting"}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	stage := s.Stage
	if stage == ChNotStarted || stage >= ChComplete {
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about Big Chompy Bird Hunting"}
	}
	// Every stage's coins, and the arrows' own kit where arrows are to be made.
	if st, ok := chKitStep(s); ok {
		return st
	}
	switch {
	case stage == ChStarted:
		if chArrows(s) >= ChArrowTarget {
			return chRantzTalk(s, CustomChGive, "Rantz takes six ogre arrows")
		}
		if !ChHasTools(s) {
			if s.Failed[CustomChBugs] {
				return chPark(ChItemParkFor, "no knife or chisel from the bank, the mule or Bugs")
			}
			return chCustom(CustomChBugs, "the knife and the chisel from Bugs")
		}
		if st, ok := chArrowNext(s); ok {
			return st
		}
		return chRantzTalk(s, CustomChGive, "Rantz takes six ogre arrows")
	case stage == ChGivenArrows:
		if s.HeldID(ObjChToad) > 0 {
			return chRantzTalk(s, CustomChShowToad, "show Rantz the bloated toad")
		}
		return chRantzTalk(s, CustomChToadies, "ask Rantz how the chompys are made to come")
	case stage < ChShownToad:
		if s.HeldID(ObjChToad) > 0 {
			return chRantzTalk(s, CustomChShowToad, "show Rantz the bloated toad")
		}
		if st := chBellowsStep(s); st != nil {
			return *st
		}
		return chToadStep(s)
	case stage < ChRantzMissed:
		if st := chBellowsStep(s); st != nil && s.HeldID(ObjChToad) == 0 {
			return *st
		}
		if s.Failed[CustomChBait] {
			return chPark(ChStuckParkFor, "no chompy bird came to the bait Rantz could see")
		}
		return chCustom(CustomChBait, "bait the clearing and watch Rantz shoot")
	case stage < ChKilled:
		return chHuntStep(s)
	case stage == ChKilled:
		if s.HeldID(ObjChRawChompy) == 0 {
			return chHuntStep(s)
		}
		return chRantzTalk(s, CustomChShowChompy, "show Rantz the raw chompy")
	case stage == ChToldToCook:
		return chCookLeg(s)
	case stage == ChCooked:
		if s.HeldID(ObjChSeasonedChomp) == 0 {
			return chCookLeg(s)
		}
		return chRantzTalk(s, CustomChHandOver, "hand Rantz the seasoned chompy")
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Big Chompy Bird Hunting stage %d", stage)}
}

// chToadStep is the bellows filled and the toads inflated, parked when the last round did nothing.
func chToadStep(s Snapshot) Step {
	if s.Failed[CustomChToad] {
		return chPark(ChStuckParkFor, "no swamp toad could be bloated with the bellows")
	}
	return chCustom(CustomChToad, "fill the bellows at the swamp bubbles and bloat a swamp toad")
}

// ChompyRecord is the quest's identity: the skills are the content's own and the Record states them, so the ladder never starts an account below them.
var ChompyRecord = Record{ID: ChompyID, Name: ChompyName, QuestPoints: 2,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "ranged", Level: ChRanged}, {Skill: "cooking", Level: ChCooking},
		{Skill: "fletching", Level: ChFletching}, {Skill: "crafting", Level: ChCrafting}}}}

// BigChompy is the engine module. The wolves are fought (a lost fight is a near-death) and nothing else; two deaths park it for a day.
var BigChompy = Module{
	Record: ChompyRecord, Bank: YanilleBank, BankSet: true, Anchor: ChRantzAnchor,
	ToolIDs:   chToolIDs,
	ReadStage: ParseChompyJournal,
	Sustain:   chSustain,
	MaxDeaths: 2,
	Decide:    DecideChompy,
}
