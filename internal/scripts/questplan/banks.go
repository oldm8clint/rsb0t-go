package questplan

import "rsb0t-go/internal/scripts/tile"

// Bank is a bank the quest engine can walk to, by the tile in front of its booths.
type Bank struct {
	Name string
	Tile tile.Tile
}

// Banks are the booth banks the ported quests reach (tiles from content maps and the sibling
// runners' own bank stands). Members banks are included: every ladder quest is members content.
//
// Lumbridge has no bank in this era: the castle's top floor has no booth in content 289's
// m50_50 (the fourth Waterfall lab run respawned there, walked up and found none), so a death
// in Lumbridge draws its float at Draynor.
var Banks = []Bank{
	{"Draynor", tile.Tile{X: 3093, Z: 3243}},
	{"Varrock east", tile.Tile{X: 3253, Z: 3420}},
	{"Varrock west", tile.Tile{X: 3185, Z: 3436}},
	{"Edgeville", tile.Tile{X: 3094, Z: 3491}},
	{"Falador east", tile.Tile{X: 3013, Z: 3355}},
	{"Falador west", tile.Tile{X: 2946, Z: 3368}},
	{"Al Kharid", tile.Tile{X: 3269, Z: 3167}},
	{"East Ardougne north", tile.Tile{X: 2616, Z: 3332}},
	{"East Ardougne south", tile.Tile{X: 2655, Z: 3286}},
	{"Catherby", tile.Tile{X: 2809, Z: 3440}},
	{"Seers' Village", tile.Tile{X: 2725, Z: 3491}},
	{"Yanille", tile.Tile{X: 2613, Z: 3094}},
}

// NearestBank is the bank closest to t in map tiles (floors ignored).
func NearestBank(t tile.Tile) Bank {
	best, bestD := Banks[0], -1
	for _, b := range Banks {
		dx, dz := b.Tile.X-t.X, b.Tile.Z-t.Z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		d := dx
		if dz > d {
			d = dz
		}
		if bestD < 0 || d < bestD {
			best, bestD = b, d
		}
	}
	return best
}
