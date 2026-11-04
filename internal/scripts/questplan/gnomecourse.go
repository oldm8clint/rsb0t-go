package questplan

import "rsb0t-go/internal/scripts/tile"

// The Gnome Stronghold agility course, for the ladder's two Agility rungs (1 to 25, and 25 to
// 35: the Barbarian course's entry pipe itself needs 35). From the lane Q study
// (private evidence notes): gnome_course.rs2 has no level check, no
// failure and no damage on any obstacle, and pays 86.5 XP a lap at 1x (865 in the lab). The
// course progress varp is not transmitted, so the runner places itself from its tile.

// GnomeObstacle is one obstacle: the loc ids that carry it, the op, where to stand, and where its own
// movement ends.
type GnomeObstacle struct {
	Name  string
	IDs   []int
	Op    string
	Near  tile.Tile
	Stand tile.Tile
	// Land is where the obstacle's scripted movement ends (gnome_course.rs2). The account crosses the rope, the log and
	// the pipe tile by tile for several seconds: every tile on the way already looks like "in front of the next obstacle"
	// to GnomeNext, and the collision pack holds no path from them (the rope's far half is an island between two blocked
	// tiles, the pipe's inside is walled), so a lap that approaches the next obstacle from there fails its walk. The laps
	// wait for Land.
	Land GnomeLand
}

// GnomeLand is a box of tiles on one level and one row, where an obstacle may set the account down: the tile column
// depends on the side of a two-wide net or pipe the account stood at, and the engine keeps it.
type GnomeLand struct {
	Level, MinX, MaxX, Z int
}

// Has reports whether t is in the box.
func (l GnomeLand) Has(t tile.Tile) bool {
	return t.Level == l.Level && t.Z == l.Z && t.X >= l.MinX && t.X <= l.MaxX
}

// GnomeCourse is the lap in content order (loc ids from pack/loc.pack, tiles from m38_53.jm2, landings from
// gnome_course.rs2's movecoords: the log is coord -7 in z, the net coord +1 level and -2 in z, the branch up
// loc_coord +1 level and -2 in z, the rope coord +6 in x, the branch down 0_38_53_55_28, the second net coord +2 in z, the
// pipe loc_coord +6 in z).
var GnomeCourse = []GnomeObstacle{
	{"Log balance", []int{2295}, "Walk-across", tile.Tile{X: 2474, Z: 3435}, tile.Tile{X: 2474, Z: 3436}, GnomeLand{Level: 0, MinX: 2474, MaxX: 2474, Z: 3429}},
	{"Obstacle net", []int{2285}, "Climb-over", tile.Tile{X: 2473, Z: 3425}, tile.Tile{X: 2473, Z: 3426}, GnomeLand{Level: 1, MinX: 2471, MaxX: 2476, Z: 3424}},
	{"Tree branch", []int{2313}, "Climb", tile.Tile{X: 2473, Z: 3422, Level: 1}, tile.Tile{X: 2473, Z: 3423, Level: 1}, GnomeLand{Level: 2, MinX: 2473, MaxX: 2473, Z: 3420}},
	{"Balancing rope", []int{2312}, "Walk-on", tile.Tile{X: 2478, Z: 3420, Level: 2}, tile.Tile{X: 2477, Z: 3420, Level: 2}, GnomeLand{Level: 2, MinX: 2483, MaxX: 2483, Z: 3420}},
	{"Tree branch", []int{2314, 2315}, "Climb-down", tile.Tile{X: 2486, Z: 3419, Level: 2}, tile.Tile{X: 2485, Z: 3419, Level: 2}, GnomeLand{Level: 0, MinX: 2487, MaxX: 2487, Z: 3420}},
	{"Obstacle net", []int{2286}, "Climb-over", tile.Tile{X: 2485, Z: 3426}, tile.Tile{X: 2485, Z: 3425}, GnomeLand{Level: 0, MinX: 2483, MaxX: 2488, Z: 3427}},
	{"Obstacle pipe", []int{154, 4058}, "Squeeze-through", tile.Tile{X: 2484, Z: 3431}, tile.Tile{X: 2484, Z: 3430}, GnomeLand{Level: 0, MinX: 2484, MaxX: 2487, Z: 3437}},
}

// GnomeStart is the log balance's stand, where a lap begins.
var GnomeStart = tile.Tile{X: 2474, Z: 3436}

// OnGnomeCourse is the course's box (with a margin for the pipe ends and the start).
func OnGnomeCourse(t tile.Tile) bool {
	return t.X >= 2466 && t.X <= 2492 && t.Z >= 3412 && t.Z <= 3440
}

// GnomeLanded reports whether t is where obstacle i sets the account down.
func GnomeLanded(i int, t tile.Tile) bool {
	return i >= 0 && i < len(GnomeCourse) && GnomeCourse[i].Land.Has(t)
}

// InGnomeUpper reports whether t is on the course's upper floors: the level-1 platform the first net climbs to and the
// level-2 platforms the tree branch and the rope lead across. An account standing there has no walk anywhere (the planner
// holds none of the course's obstacles), and only the course's own obstacles take it down: the tree branch (2313), the
// balancing rope (2312) and the far tree branch (2314) in that order. These are the rectangles of the Guardian's strand
// site (randomevents: the Gnome agility course's upper floors); a test holds the two together.
func InGnomeUpper(t tile.Tile) bool {
	switch t.Level {
	case 1:
		return t.X >= 2470 && t.X <= 2477 && t.Z >= 3421 && t.Z <= 3425
	case 2:
		return t.X >= 2470 && t.X <= 2490 && t.Z >= 3416 && t.Z <= 3423
	}
	return false
}

// GnomeNext is the obstacle a tile on the course is in front of (TS AgilityBot resyncs the same
// way): the planes and the two sides of the nets separate every leg.
func GnomeNext(t tile.Tile) int {
	switch {
	case t.Level == 1:
		return 2
	case t.Level == 2 && t.X < 2481:
		return 3
	case t.Level == 2:
		return 4
	case !OnGnomeCourse(t):
		return 0
	case t.X >= 2480 && t.Z <= 3425:
		return 5
	case t.X >= 2480 && t.Z >= 3426 && t.Z <= 3433:
		return 6
	case t.X < 2480 && t.Z >= 3426 && t.Z <= 3430:
		return 1
	}
	return 0
}
