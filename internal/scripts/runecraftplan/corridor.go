package runecraftplan

// The Karamja corridor: the road a carrier (and the crafter) walks between the Brimhaven pier, Jiminua's Jungle Store and the
// Nature ruins, and the monsters that stand beside it.
//
// WHY A TABLE AND NOT AvoidHostiles. The walker's hostile avoidance knows two species, Giant bats and White wolves
// (webwalk/hostile.go; bot/mulehome.go). Jungle spiders are level 44 with 50 hitpoints, huntrange 1 and the "cowardly" hunt mode
// that attacks any player not too strong for them (content/scripts/_unpack/225/all.npc [jungle_spider], all.hunt [cowardly]);
// a carrier at 25 hitpoints meets one 1 tile from the shortest road at (2796,3104). The only mechanism that keeps a walk off a
// monster the avoider does not know is WalkOptions.AvoidRects (the mulehome.go precedent for the wolves), so the carrier plans
// with a rectangle around every spawn of the monsters that attack: the spawn tile grown by what the monster can reach, its
// wander range plus its hunt range (plus its size less one).
//
// The spawn tiles are the NPC sections of content/maps/m42..46_45..50.jm2, every spider, tribesman and scorpion on level 0 inside
// the island's box (the same extraction TestSpawnTableMatchesTheMaps repeats when it is given the content checkout). Snakes
// (level 5, 6 hitpoints) are left on the road: they stand ON it at (2847,3042), and keeping off them as well costs 201 steps
// instead of 122. Monkeys are passive.
//
// Measured on the real collision pack (TestCorridorOnTheRealPack): pier to Jiminua 114 steps shortest, 115 with the boxes;
// Jiminua to the ruins' exit tile 119 shortest, 122 with the boxes.

import "rsb0t-go/internal/scripts/tile"

// Box is an inclusive world-tile rectangle on level 0.
type Box struct{ MinX, MaxX, MinZ, MaxZ int }

// Contains reports whether the tile is inside the box on level 0.
func (b Box) Contains(t tile.Tile) bool {
	return t.Level == 0 && t.X >= b.MinX && t.X <= b.MaxX && t.Z >= b.MinZ && t.Z <= b.MaxZ
}

// Hostile is a kind of monster the corridor keeps away from.
type Hostile struct {
	Name string
	// Level is the combat level shown, Wander the wanderrange from the spawn, Hunt the huntrange, Size the tiles it covers.
	Level, Wander, Hunt, Size int
}

// Reach is how far from its spawn tile the monster's attack can start: it wanders Wander tiles, notices a player Hunt tiles
// away, and a monster wider than a tile reaches Size-1 further.
func (h Hostile) Reach() int { return h.Wander + h.Hunt + max(0, h.Size-1) }

// The monsters (content 225 configs, which 289 keeps for these).
var (
	JungleSpider = Hostile{Name: "Jungle spider", Level: 44, Wander: 3, Hunt: 1, Size: 1}
	Tribesman    = Hostile{Name: "Tribesman", Level: 32, Wander: 5, Hunt: 1, Size: 1}
	Scorpion     = Hostile{Name: "Scorpion", Level: 14, Wander: 12, Hunt: 2, Size: 2}
)

// Spawn is one spawn tile of a monster kind.
type Spawn struct {
	Kind Hostile
	At   tile.Tile
}

// Box is the ground the spawn's monster can reach.
func (s Spawn) Box() Box {
	r := s.Kind.Reach()
	return Box{MinX: s.At.X - r, MaxX: s.At.X + r, MinZ: s.At.Z - r, MaxZ: s.At.Z + r}
}

func spawns(kind Hostile, xz ...[2]int) []Spawn {
	out := make([]Spawn, 0, len(xz))
	for _, p := range xz {
		out = append(out, Spawn{Kind: kind, At: tile.Tile{X: p[0], Z: p[1]}})
	}
	return out
}

// KaramjaSpawns is every spider, tribesman and scorpion spawn inside the island's box (x 2700..2960, z 2880..3240), by
// the map files.
func KaramjaSpawns() []Spawn {
	var out []Spawn
	out = append(out, spawns(JungleSpider,
		[2]int{2767, 3204}, [2]int{2768, 3210}, [2]int{2777, 3116}, [2]int{2780, 3029}, [2]int{2780, 3031},
		[2]int{2796, 3104}, [2]int{2796, 3116}, [2]int{2797, 3112}, [2]int{2798, 3107}, [2]int{2799, 3115},
		[2]int{2810, 2981}, [2]int{2810, 2987}, [2]int{2812, 2999}, [2]int{2814, 2996},
		[2]int{2844, 3061}, [2]int{2850, 3063}, [2]int{2853, 3054},
		[2]int{2855, 3085}, [2]int{2855, 3089}, [2]int{2857, 3085}, [2]int{2858, 3083}, [2]int{2858, 3092},
		[2]int{2861, 3082}, [2]int{2861, 3088}, [2]int{2862, 3081}, [2]int{2864, 3084}, [2]int{2864, 3088},
		[2]int{2866, 3081}, [2]int{2867, 3086}, [2]int{2868, 3084},
		[2]int{2883, 2996}, [2]int{2884, 3005}, [2]int{2887, 3006}, [2]int{2888, 2946}, [2]int{2888, 2998},
		[2]int{2890, 3007}, [2]int{2891, 2994}, [2]int{2892, 2946}, [2]int{2892, 2997}, [2]int{2892, 3000},
		[2]int{2893, 2948}, [2]int{2894, 3006}, [2]int{2896, 3000},
		[2]int{2914, 3046}, [2]int{2915, 3048}, [2]int{2916, 3043}, [2]int{2920, 3046}, [2]int{2921, 3040},
		[2]int{2923, 3046}, [2]int{2927, 3043}, [2]int{2929, 3047}, [2]int{2930, 3044}, [2]int{2931, 3032},
		[2]int{2932, 3032}, [2]int{2932, 3054}, [2]int{2932, 3057}, [2]int{2933, 3048})...)
	out = append(out, spawns(Tribesman,
		[2]int{2723, 3159}, [2]int{2724, 3158}, [2]int{2724, 3161}, [2]int{2725, 3163}, [2]int{2728, 3158},
		[2]int{2729, 3158}, [2]int{2729, 3164}, [2]int{2730, 3162}, [2]int{2731, 3161},
		[2]int{2771, 3014}, [2]int{2773, 3017}, [2]int{2777, 3068}, [2]int{2801, 3067}, [2]int{2842, 3017},
		[2]int{2866, 3061}, [2]int{2868, 3054},
		[2]int{2918, 2987}, [2]int{2937, 2993}, [2]int{2938, 2995}, [2]int{2940, 2991}, [2]int{2942, 2995})...)
	out = append(out, spawns(Scorpion,
		[2]int{2765, 2978}, [2]int{2768, 2980}, [2]int{2785, 2955}, [2]int{2786, 2947}, [2]int{2788, 2948},
		[2]int{2843, 3156}, [2]int{2853, 3159}, [2]int{2861, 3165}, [2]int{2862, 3180}, [2]int{2863, 3169},
		[2]int{2865, 3156}, [2]int{2870, 3159})...)
	return out
}

// AvoidBoxes is the box of every spawn in KaramjaSpawns: the rectangles a carrier's walk plans around.
func AvoidBoxes() []Box {
	sp := KaramjaSpawns()
	out := make([]Box, 0, len(sp))
	for _, s := range sp {
		out = append(out, s.Box())
	}
	return out
}

// DangerAt reports the first monster kind whose box holds t, and whether one does.
func DangerAt(t tile.Tile) (Hostile, bool) {
	for _, s := range KaramjaSpawns() {
		if s.Box().Contains(t) {
			return s.Kind, true
		}
	}
	return Hostile{}, false
}

// The places of the corridor.
var (
	// Landing is where Captain Barnaby's ship from Ardougne puts the traveller on Brimhaven's pier.
	Landing = tile.Tile{X: 2772, Z: 3234}
	// JiminuaAt is Jiminua, the Jungle Store's keeper, who never leaves her shop (moverestrict=indoors).
	JiminuaAt = tile.Tile{X: 2767, Z: 3122}
	// StoreStand is the tile a carrier walks to for her: beside her counter, inside the store.
	StoreStand = tile.Tile{X: 2767, Z: 3123}
	// ArdougnePier is Captain Barnaby's pier on the mainland.
	ArdougnePier = tile.Tile{X: 2683, Z: 3272}
)

// Karamja is the island on the surface, Brimhaven's pier down to the ruins (the temples count as on it, by InNatureTemple and
// the temple squares, not by this box).
var Karamja = Box{MinX: 2700, MaxX: 2960, MinZ: 2880, MaxZ: 3240}

// OnKaramja reports whether t is on the island's surface.
func OnKaramja(t tile.Tile) bool { return Karamja.Contains(t) }
