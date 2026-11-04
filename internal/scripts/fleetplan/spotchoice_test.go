package fleetplan

import (
	"rsb0t-go/internal/scripts/tile"
)

// A synthetic pen on open ground (a nil sight sees through everything): spawns the spots reach by
// distance alone. Spot s1 reaches four spawns, s2 and s3 two each, s4 and s5 the SAME single spawn
// (two tiles of one pocket), the shape of the Feldip East pen where 7 accounts stood on a spot that
// reached one Ogre.
func spotTestPen() TrainPen {
	th := func(x, z int) PenThreat {
		return PenThreat{At: tile.Tile{X: x, Z: z}, Size: 2, Cut: 6, Name: "Ogre", Herd: true}
	}
	return TrainPen{
		Name: "test ogres", Monster: "Ogre", Stand: tile.Tile{X: 140, Z: 120}, Leash: 60,
		SafeSpots: []tile.Tile{{X: 100, Z: 100}, {X: 140, Z: 100}, {X: 180, Z: 100}, {X: 100, Z: 140}, {X: 101, Z: 140}},
		Threats: []PenThreat{
			th(96, 100), th(104, 100), th(100, 96), th(100, 104), // s1: four
			th(136, 100), th(144, 100), // s2: two
			th(176, 100), th(184, 100), // s3: two
			th(100, 145), // s4 and s5: one
			{At: tile.Tile{X: 500, Z: 500}, Size: 2, Cut: 6, Name: "Ogre", Herd: true}, // outside the leash
			{At: tile.Tile{X: 102, Z: 140}, Size: 1, Cut: 6, Name: "Wolf", Herd: false},
		},
	}
}
