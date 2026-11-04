package fleetplan

import (
	"rsb0t-go/internal/scripts/tile"
)

// penHostile is one aggressive NPC spawn near a herd: its spawn (south-west tile), its size, its
// chase cut (maxrange + 1; maxrange defaults to wanderrange + 2) and its name.
type penHostile struct {
	at   tile.Tile
	size int
	cut  int
	hunt int
	name string
}

// Hostiles read from content 289's maps (m*_*.jm2 "level x z: id", pack/npc.pack, and
// scripts/_unpack/225/all.npc) on 3 Oct 2026: every spawn with a huntmode within 32 tiles of the herd.
var (
	varrockSewerHostiles = []penHostile{
		{tile.Tile{X: 3130, Z: 9860}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3132, Z: 9904}, 1, 11, 1, "Skeleton"},
		{tile.Tile{X: 3132, Z: 9908}, 1, 11, 1, "Skeleton"},
		{tile.Tile{X: 3132, Z: 9914}, 1, 11, 1, "Skeleton"},
		{tile.Tile{X: 3132, Z: 9921}, 1, 8, 3, "Thug"},
		{tile.Tile{X: 3134, Z: 9911}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3137, Z: 9871}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3138, Z: 9874}, 1, 11, 1, "Skeleton"},
		{tile.Tile{X: 3139, Z: 9877}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3139, Z: 9885}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3140, Z: 9893}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3141, Z: 9869}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3141, Z: 9873}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3143, Z: 9904}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3144, Z: 9869}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3146, Z: 9892}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3147, Z: 9883}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3147, Z: 9899}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3148, Z: 9904}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3150, Z: 9889}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3150, Z: 9907}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3151, Z: 9884}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3157, Z: 9905}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 3159, Z: 9900}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 3163, Z: 9878}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 3167, Z: 9879}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 3171, Z: 9884}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3174, Z: 9891}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3175, Z: 9881}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3176, Z: 9883}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3178, Z: 9880}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3178, Z: 9888}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3180, Z: 9883}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3183, Z: 9885}, 1, 13, 1, "Deadly red spider"},
		{tile.Tile{X: 3186, Z: 9892}, 1, 13, 1, "Deadly red spider"},
	}
	edgevilleGiantHostiles = []penHostile{
		{tile.Tile{X: 3099, Z: 9832}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3100, Z: 9836}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3106, Z: 9827}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3107, Z: 9835}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3110, Z: 9845}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3111, Z: 9841}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3113, Z: 9831}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3116, Z: 9832}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3117, Z: 9835}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3117, Z: 9843}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3117, Z: 9850}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3118, Z: 9871}, 1, 6, 1, "Hobgoblin"},
		{tile.Tile{X: 3119, Z: 9862}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3122, Z: 9844}, 2, 6, 2, "Giant"},
		{tile.Tile{X: 3124, Z: 9861}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3126, Z: 9865}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3130, Z: 9860}, 1, 12, 1, "Zombie"},
		{tile.Tile{X: 3137, Z: 9871}, 1, 12, 1, "Skeleton"},
		{tile.Tile{X: 3141, Z: 9869}, 1, 12, 1, "Skeleton"},
	}
	iceDungeonHostiles = []penHostile{
		{tile.Tile{X: 3044, Z: 9581}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3046, Z: 9575}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3048, Z: 9583}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3049, Z: 9590}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3052, Z: 9568}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3053, Z: 9576}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3054, Z: 9564}, 2, 6, 2, "Ice giant"},
		{tile.Tile{X: 3056, Z: 9583}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3056, Z: 9587}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3057, Z: 9566}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3057, Z: 9568}, 2, 6, 2, "Ice giant"},
		{tile.Tile{X: 3062, Z: 9576}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3062, Z: 9581}, 1, 6, 5, "Ice warrior"},
		{tile.Tile{X: 3065, Z: 9571}, 2, 6, 2, "Ice giant"},
	}
	ardougneMossGiantHostiles = []penHostile{
		{tile.Tile{X: 2549, Z: 3408}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 2554, Z: 3401}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 2554, Z: 3409}, 2, 6, 2, "Moss giant"},
		{tile.Tile{X: 2556, Z: 3406}, 2, 6, 2, "Moss giant"},
	}
)

// leashHolds is Npc.targetWithinMaxRange's op-trigger test for a target on t.
func (h penHostile) leashHolds(t tile.Tile) bool {
	dx, dz := Abs(t.X-h.at.X), Abs(t.Z-h.at.Z)
	return max(dx, dz) <= h.cut && !(dx == h.cut && dz == h.cut)
}

func herdOf(hostiles []penHostile, name string) []tile.Tile {
	var out []tile.Tile
	for _, h := range hostiles {
		if h.name == name {
			out = append(out, h.at)
		}
	}
	return out
}

// giantHerds are the herds the survey searches: each with its hostiles and a tile the walk in
// reaches (the safespots must be walked to from it).
var giantHerds = []struct {
	name     string
	hostiles []penHostile
	herd     string
	stand    tile.Tile
}{
	{"Varrock sewer moss giants", varrockSewerHostiles, "Moss giant", tile.Tile{X: 3159, Z: 9900}},
	{"Edgeville dungeon hill giants", edgevilleGiantHostiles, "Giant", tile.Tile{X: 3113, Z: 9836}},
	{"Asgarnian ice dungeon ice giants", iceDungeonHostiles, "Ice giant", tile.Tile{X: 3058, Z: 9568}},
	{"North Ardougne moss giants", ardougneMossGiantHostiles, "Moss giant", tile.Tile{X: 2559, Z: 3405}},
}
