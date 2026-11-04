package mazegraph

import "sync"

// The maze's chests and the walls the solver clicks to hold its reward, ported from
// content/maps/m45_71.jm2 (the LOC section) and content/scripts/macro events/scripts/general/
// macro_event_maze.rs2. MazeLayout carries only walls and doors, so these are separate data.

const (
	// ChestLocID is macro_maze_chest, the closed chest (op1 "Open"). [oploc1,macro_maze_chest] runs
	// ~macro_maze_open_chest: p_arrivedelay, a one tick p_delay(0), loc_change to ChestEmptyLocID for
	// ChestRespawnTicks, then ~macro_maze_calculate_chest_loot (one of ten small prizes, an objbox
	// that stays open until it is clicked away).
	ChestLocID = 3635
	// ChestEmptyLocID is macro_maze_chestempty (op1 "Search": "Awww, it's empty!"), what a chest
	// looks like for ChestRespawnTicks after anyone opened it. The maze is not an instance, so
	// another player in it can have emptied a chest first.
	ChestEmptyLocID = 3636
	// ChestRespawnTicks is ^macro_maze_chest_ticks: a chest can be opened again 100 ticks (60 s)
	// after it was last opened.
	ChestRespawnTicks = 100
	// WallHighID is macro_maze_wallhigh, the same loc id as WallID: its op1 "Open" is
	// [oploc1,macro_maze_wallhigh] ~mesbox("I can't open that."), a chat dialogue that stays open
	// until it is dismissed. The solver clicks one to open a dialogue on purpose, which is the one
	// state in which the reward timer stands still and the player can still walk (see mazeloot).
	WallHighID = WallID
)

// Chests are the twelve closed chests' tiles (world coordinates), from the jm2's
// "0 lx lz: 3635 10 angle" lines: local (10,55) (11,41) (15,48) (16,47) (19,35) (20,34) (21,16)
// (37,46) (38,46) (44,28) (45,29) (50,51). Each is a 1x1 centrepiece, so its tile is solid and it
// is opened from a cardinal neighbour with no wall or door between (engine
// RectangleBoundary.reachRectangle1; see Graph.ChestStands). Four pairs sit diagonally next to
// each other: (2895,4592)+(2896,4591), (2899,4579)+(2900,4578), (2917,4590)+(2918,4590) and
// (2924,4572)+(2925,4573).
var Chests = []Cell{
	{X: 2890, Z: 4599}, {X: 2891, Z: 4585}, {X: 2895, Z: 4592}, {X: 2896, Z: 4591},
	{X: 2899, Z: 4579}, {X: 2900, Z: 4578}, {X: 2901, Z: 4560}, {X: 2917, Z: 4590},
	{X: 2918, Z: 4590}, {X: 2924, Z: 4572}, {X: 2925, Z: 4573}, {X: 2930, Z: 4595},
}

// IsChest reports whether c is one of Chests.
func IsChest(c Cell) bool {
	for _, ch := range Chests {
		if ch == c {
			return true
		}
	}
	return false
}

// ChestStands lists the tiles a chest can be opened from: its cardinal neighbours with no wall
// edge, door edge or solid tile in the way. The engine accepts a 1x1 rectangle loc only from a
// tile that is orthogonally adjacent with no wall flag on the shared edge (reachRectangle1), never
// diagonally.
func (g *Graph) ChestStands(chest Cell) []Cell {
	var out []Cell
	for _, d := range cardinal {
		n := Cell{X: chest.X + d.X, Z: chest.Z + d.Z}
		if g.Solid[n] {
			continue
		}
		ek := EdgeKey(chest.X, chest.Z, n.X, n.Z)
		if g.WallEdge[ek] {
			continue
		}
		if _, door := g.Door[ek]; door {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Room is every tile reachable from `from` over open floor, closed doors counting as walls and
// solid tiles (the chests) as blocked, in at most maxCells expansions. It includes `from`.
func (g *Graph) Room(from Cell, maxCells int) map[Cell]bool {
	seen := map[Cell]bool{from: true}
	queue := []Cell{from}
	for head := 0; head < len(queue) && head < maxCells; head++ {
		for _, n := range g.openNeighbors(queue[head]) {
			if seen[n] {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return seen
}

// Distances is the number of cardinal steps of the shortest open-floor walk from `from` to every
// tile of its room (maxCells bounds the search), `from` itself at 0.
func (g *Graph) Distances(from Cell, maxCells int) map[Cell]int {
	dist := map[Cell]int{from: 0}
	queue := []Cell{from}
	for head := 0; head < len(queue) && head < maxCells; head++ {
		cur := queue[head]
		for _, n := range g.openNeighbors(cur) {
			if _, seen := dist[n]; seen {
				continue
			}
			dist[n] = dist[cur] + 1
			queue = append(queue, n)
		}
	}
	return dist
}

var (
	wallAnchors     map[Cell]bool
	wallAnchorsOnce sync.Once
)

// wallAnchored is whether a maze wall loc (WallID, any shape) is anchored on c. The player can
// operate a wall from its own tile whatever its shape (engine ReachStrategy.reachWall: a source on
// the destination tile is always reached), so a tile with one is a place to click "Open" for the
// "I can't open that." dialogue without moving first.
func wallAnchored(c Cell) bool {
	wallAnchorsOnce.Do(func() {
		m := make(map[Cell]bool, len(MazeLayout)/LayoutStride)
		for i := 0; i+LayoutStride <= len(MazeLayout); i += LayoutStride {
			if MazeLayout[i+2] == WallID {
				m[Cell{X: Origin.X + MazeLayout[i], Z: Origin.Z + MazeLayout[i+1]}] = true
			}
		}
		wallAnchors = m
	})
	return wallAnchors[c]
}

// WallAnchoredAt reports whether a wall loc (macro_maze_wallhigh) sits on tile c.
func WallAnchoredAt(c Cell) bool { return wallAnchored(c) }

// NearestWall finds the closest tile (by open-floor steps, at most maxSteps) with a wall anchored on
// it, starting with `from` itself; the wall is clicked at that tile's coordinates. It returns the
// tile, the steps to reach it and whether there is one.
func (g *Graph) NearestWall(from Cell, maxSteps int) (Cell, int, bool) {
	if wallAnchored(from) {
		return from, 0, true
	}
	dist := map[Cell]int{from: 0}
	queue := []Cell{from}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		if dist[cur] >= maxSteps {
			continue
		}
		for _, n := range g.openNeighbors(cur) {
			if _, seen := dist[n]; seen {
				continue
			}
			dist[n] = dist[cur] + 1
			if wallAnchored(n) {
				return n, dist[n], true
			}
			queue = append(queue, n)
		}
	}
	return Cell{}, 0, false
}
