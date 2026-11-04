package questplan

import (
	"fmt"

	"rsb0t-go/internal/scripts/tile"
)

// The hazards of Tai Bwo Wannai Trio's legs. The island's walks run between the village, the Holy Lake, Lubufu at Brimhaven, the monkeys
// beside Tamayu, the Jogre pen and Tiadeche, and the content (maps m43_46 to m46_49) puts five kinds of hunter on that ground:
//
//   - jungle spiders (npc 62: level 44, 50 hitpoints, huntmode cowardly, hunt range 1, wander range 3): hunt an account of combat 88
//     or less, and only from a tile away. Fifty-five stand in the box, among them a nest of fourteen at 2914,3032 to 2933,3057 round
//     the east of the Jogre pen.
//   - tribesmen (npc 191: level 32, cowardly, hunt range 1, default wander 5): combat 64 or less.
//   - guard dogs (npc 99: level 44, cowardly, hunt range 1, wander 3): three north of Brimhaven.
//   - Jogres (npc 113: level 53, 60 hitpoints, attack rate 6, a hit of 7, cowardly with hunt range 2, wander 3): combat 106 or
//     less. Fifteen on the surface, seven of them in the pen (2916,3055 to 2935,3062) beside the nest.
//   - the Shaikahan (npc 1172, the fightable one): see TbwtShaikahanOdds. It cannot be fenced: it wanders 32 tiles from 2906,3095.
//
// "Cowardly" is the engine's HuntCheckNotTooStrong: Npc.ts skips a player whose combat level exceeds twice the npc's vislevel outside the
// Wilderness. The fleet's accounts are combat 60 to 90, so the spiders, tribesmen and Jogres hunt nearly all of them. Snakes (level 5),
// scorpions (14) and hobgoblins (28, three at the Holy Lake) hunt only combat 10, 28 and 56 and under, and the ladder starts the quest
// at combat 45 (ladderfacts): the snakes and scorpions are ignored and the hobgoblins are not worth a fence (they wander 15 tiles).
//
// The walks keep out of a square round each spawn as wide as the ground the hunter covers: its wander range, its hunt range and a tile of
// slack (JungleSpiderPad, JungleTribesmanPad, TbwtGuardDogPad, TbwtJogrePad). The planner finds a route to every stand and back round all
// of them on the real pack (questmods' TestTbwtEveryWalkExistsRoundTheHunters).

// tbwtSpiderSpawns are the jungle spiders (npc 62)
var tbwtSpiderSpawns = []tile.Tile{
	{X: 2777, Z: 3116}, {X: 2780, Z: 3029}, {X: 2780, Z: 3031}, {X: 2796, Z: 3104}, {X: 2796, Z: 3116}, {X: 2797, Z: 3112},
	{X: 2798, Z: 3107}, {X: 2799, Z: 3115}, {X: 2810, Z: 2981}, {X: 2810, Z: 2987}, {X: 2812, Z: 2999}, {X: 2814, Z: 2996},
	{X: 2844, Z: 3061}, {X: 2850, Z: 3063}, {X: 2853, Z: 3054}, {X: 2855, Z: 3085}, {X: 2855, Z: 3089}, {X: 2857, Z: 3085},
	{X: 2858, Z: 3083}, {X: 2858, Z: 3092}, {X: 2861, Z: 3082}, {X: 2861, Z: 3088}, {X: 2862, Z: 3081}, {X: 2864, Z: 3084},
	{X: 2864, Z: 3088}, {X: 2866, Z: 3081}, {X: 2867, Z: 3086}, {X: 2868, Z: 3084}, {X: 2883, Z: 2996}, {X: 2884, Z: 3005},
	{X: 2887, Z: 3006}, {X: 2888, Z: 2946}, {X: 2888, Z: 2998}, {X: 2890, Z: 3007}, {X: 2891, Z: 2994}, {X: 2892, Z: 2946},
	{X: 2892, Z: 2997}, {X: 2892, Z: 3000}, {X: 2893, Z: 2948}, {X: 2894, Z: 3006}, {X: 2896, Z: 3000}, {X: 2914, Z: 3046},
	{X: 2915, Z: 3048}, {X: 2916, Z: 3043}, {X: 2920, Z: 3046}, {X: 2921, Z: 3040}, {X: 2923, Z: 3046}, {X: 2927, Z: 3043},
	{X: 2929, Z: 3047}, {X: 2930, Z: 3044}, {X: 2931, Z: 3032}, {X: 2932, Z: 3032}, {X: 2932, Z: 3054}, {X: 2932, Z: 3057},
	{X: 2933, Z: 3048},
}

// tbwtTribesmanSpawns are the tribesmen (npc 191)
var tbwtTribesmanSpawns = []tile.Tile{
	{X: 2771, Z: 3014}, {X: 2773, Z: 3017}, {X: 2777, Z: 3068}, {X: 2801, Z: 3067}, {X: 2842, Z: 3017}, {X: 2866, Z: 3061},
	{X: 2868, Z: 3054}, {X: 2918, Z: 2987}, {X: 2937, Z: 2993}, {X: 2938, Z: 2995}, {X: 2940, Z: 2991}, {X: 2942, Z: 2995},
}

// tbwtGuardDogSpawns are the guard dogs (npc 99)
var tbwtGuardDogSpawns = []tile.Tile{
	{X: 2784, Z: 3191}, {X: 2784, Z: 3197}, {X: 2787, Z: 3195},
}

// tbwtJogreSpawns are the Jogres of the surface (npc 113)
var tbwtJogreSpawns = []tile.Tile{
	{X: 2896, Z: 2945}, {X: 2903, Z: 2945}, {X: 2916, Z: 3055}, {X: 2916, Z: 3062}, {X: 2918, Z: 2963}, {X: 2919, Z: 3052},
	{X: 2921, Z: 2959}, {X: 2923, Z: 2985}, {X: 2924, Z: 3060}, {X: 2925, Z: 2956}, {X: 2927, Z: 3056}, {X: 2928, Z: 3058},
	{X: 2931, Z: 2987}, {X: 2932, Z: 3043}, {X: 2935, Z: 3046},
}

// The pads: the hunter's wander range, its hunt range and a tile of slack (a Jogre is two tiles wide).
const (
	TbwtGuardDogPad = 3 + 1 + 1
	TbwtJogrePad    = 3 + 2 + 1 + 1
)

// TbwtHostiles lists the spawns TbwtAvoid fences, in the shape JungleHostiles uses.
func TbwtHostiles() []JungleHostile {
	var out []JungleHostile
	for _, t := range tbwtSpiderSpawns {
		out = append(out, JungleHostile{NPC: NpcJungleSpider, Tile: t})
	}
	for _, t := range tbwtTribesmanSpawns {
		out = append(out, JungleHostile{NPC: NpcTribesman, Tile: t})
	}
	for _, t := range tbwtGuardDogSpawns {
		out = append(out, JungleHostile{NPC: NpcGuardDog, Tile: t})
	}
	for _, t := range tbwtJogreSpawns {
		out = append(out, JungleHostile{NPC: NpcJogre, Tile: t})
	}
	return out
}

// TbwtPadOf is the pad of a hostile's rectangle.
func TbwtPadOf(npc int) int {
	switch npc {
	case NpcTribesman:
		return JungleTribesmanPad
	case NpcGuardDog:
		return TbwtGuardDogPad
	case NpcJogre:
		return TbwtJogrePad
	}
	return JungleSpiderPad
}

// TbwtAvoid is the rectangles every walk of the quest on the island keeps out of.
func TbwtAvoid() []Rect {
	var out []Rect
	for _, h := range TbwtHostiles() {
		pad := TbwtPadOf(h.NPC)
		out = append(out, Rect{MinX: h.Tile.X - pad, MaxX: h.Tile.X + pad, MinZ: h.Tile.Z - pad, MaxZ: h.Tile.Z + pad, Level: h.Tile.Level})
	}
	return out
}

// InTbwtCutscene reports a tile in the sealed copy of the map the hunt takes the account to (m39_71): the account is carried there and
// back by the script and does nothing in between.
func InTbwtCutscene(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2496 && t.X <= 2559 && t.Z >= 4544 && t.Z <= 4607
}

// FoeShaikahan is [tbwt_beast] (npc 1172, quest_tbwt.npc): level 83, attack 80, defence 80, strength 25, 100 hitpoints, no attackrate
// (4) and no bonus params, so a hit of 3; size 3, huntmode aggressive_melee with hunt range 8, wander range 32 and max range 64.
var FoeShaikahan = MeleeFoe{Name: "The Shaikahan", Attack: 80, Strength: 25, Defence: 80, Magic: 1, Hitpoints: 100, AttackTicks: 4}

// TbShaikahanSwings is how many swings the beast takes at an account while it stands at Tiadeche: a conversation is about forty ticks in
// which nothing can be eaten, and the walk in and out is inside its reach when it is near.
const TbShaikahanSwings = 12

// TbwtShaikahanOdds prices a visit to Tiadeche, who stands inside the beast's wander box (2874 to 2938, 3063 to 3127), against the
// account's hitpoints. The visit begins from a top-up (the custom eats first), so the price is judged on the maximum; the account must
// stand at the end of it with three of the beast's hardest hits (3) in hand. It is a price and not a fight: nothing here ever attacks it.
func TbwtShaikahanOdds(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	f := FoeShaikahan
	per := FoePerAttack(f, fighterOf(s)) * FightIncomingScale
	dmg := float64(TbShaikahanSwings) * per
	margin := float64(3 * f.PeakHit())
	if float64(s.MaxHP)-dmg < margin {
		return false, fmt.Sprintf("The Shaikahan (level 83, hits up to %d) roams Tiadeche's ground and costs an account at Defence %d about %.0f hitpoints in a visit in which nothing can be eaten; this one has %d and the visit must leave %.0f",
			f.PeakHit(), s.Level("defence"), dmg, s.MaxHP, margin)
	}
	return true, ""
}

// tbwtFoods are the foods the quest carries and eats, best first (the jungle's).
var tbwtFoods = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// tbwtSustain eats at 18: the Jogre's hardest hit is 7 and the raised line (Sustain.VsFoe) covers it, the spiders and the beast hit for 5 and 3.
var tbwtSustain = Sustain{FoodIDs: tbwtFoods, EatAt: 18}

// TbwtJogreOdds prices the one fight of the quest, a Jogre (level 53, 60 hitpoints, a hit of 7 every six ticks) for the bones it always
// drops, on the account's own swing (Attack, Strength, Defence as MeleeChain reads them) and the food in its pack. The Decide asks it
// again at the pen's edge, with what is left of the pack.
func TbwtJogreOdds(s Snapshot) FightOdds {
	return MeleeChain(s, []MeleeFoe{FoeJogre}, tbwtSustain)
}
