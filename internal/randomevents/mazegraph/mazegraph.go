// See layout.go's package doc comment for provenance. This file ports mazeGraph.ts and
// selectRoute.ts's actual logic (not just data): the wall/door edge model, the
// door-approach-side gate, and the BFS route from an arbitrary spawn tile to a tile
// that can Touch the shrine. Every exported name and constant here mirrors its TS
// counterpart 1:1 so the two can be diffed against each other if the upstream ever
// changes; Go-idiom deviations are called out inline where they occur.
package mazegraph

import (
	"fmt"
	"sync"
)

// Cell is one absolute world tile (x,z) within mapsquare 45,71.
type Cell struct{ X, Z int }

// Origin is MAZE_ORIGIN: mapsquare 45,71's own SW corner in absolute world tiles.
var Origin = Cell{X: 45 * 64, Z: 71 * 64} // (2880, 4544)

// Shrine is MAZE_SHRINE: the SW corner of the 3x3 Strange shrine (loc 3634,
// macro_maze_complete), local (31,31).
var Shrine = Cell{X: 2911, Z: 4575}

// ShrineSize is the shrine's footprint (content pack: length=3 width=3).
const ShrineSize = 3

// ShrineDoor is MAZE_SHRINE_DOOR: the shrine chamber's west door tile, local (30,32).
// The chamber's south face is a solid wall — there is only one way in.
var ShrineDoor = Cell{X: 2910, Z: 4576}

// Spawns is MAZE_SPAWNS: the four documented entrance corners (macro_maze_teleports),
// matching cmd/mazetest's own -corner flag order.
var Spawns = [4]Cell{
	{X: 2891, Z: 4597}, // NW local (11,53)
	{X: 2933, Z: 4597}, // NE local (53,53)
	{X: 2933, Z: 4555}, // SE local (53,11)
	{X: 2891, Z: 4555}, // SW local (11,11)
}

// WallID is WALL_ID: the permanent-boundary wall loc's type id.
const WallID = 3626

// ShrineLocID is MAZE_SHRINE_LOC (solveMaze.ts): the Strange shrine's own type id
// (macro_maze_complete), ported from content/scripts/_unpack/254/all.loc's
// [macro_maze_complete] entry via the original's own citation.
const ShrineLocID = 3634

// DoorDirs is DOOR_DIRS: each maze door type id's approach-gating direction (see
// DoorPassable). dir 0 = opens from either side; dir 1 = opens only from the side the
// door's own angle faces along its axis; dir 2 = opens only from the opposite side.
var DoorDirs = map[int]int{3628: 0, 3629: 1, 3630: 2, 3631: 2, 3632: 1}

// Loc is one maze-relevant placed loc, local tile coordinates (see MazeLayout).
type Loc struct {
	LX, LZ, ID, Shape, Angle int
}

// DoorInfo is one recorded door edge: which tile it's anchored on, its type id and
// angle (needed by DoorPassable and, for a live caller, by actions.OpLoc).
//
// From/To are only filled in on doors returned by SolveRoute/SelectRoute (BuildMaze
// leaves them zero): From is the cell the route crosses the door edge FROM — the tile
// a live caller must actually be standing on when it clicks Open — and To is the
// cell on the far side. A straight-wall door blocks exactly one edge between two
// cardinally adjacent tiles (its own tile and its angle-neighbour, see Sides), so From
// is always one of those two, never a diagonal neighbour: the engine's own reach check
// for a wall (engine/src/engine/routefinder/ReachStrategy.ts reachWall1) accepts the
// door's own tile, the tile across its blocked edge, and the two flanking tiles along
// the wall — never a diagonal — and the content script's side test
// (content/scripts/doors/scripts/door_procs.rs2 check_axis) then decides which way to
// move the player purely from whether the player shares the door's axis coordinate.
// This is the exact case behind the 2026-09-14 production incident: the live solver
// stood at (2902,4555), diagonal to door (2903,4554), and clicked forever (see
// internal/randomevents/maze.go).
type DoorInfo struct {
	Tile      Cell
	ID, Angle int
	From, To  Cell
}

// Sides returns the two cells a straight-wall door's blocked edge separates: the
// door's own tile and its neighbour in the direction the door's angle faces (the same
// edge straightEdge/BuildMaze record it on). Every valid approach tile for the door
// is one of these two.
func (d DoorInfo) Sides() (own, across Cell) {
	_, _, bx, bz := straightEdge(d.Tile.X, d.Tile.Z, d.Angle)
	return d.Tile, Cell{X: bx, Z: bz}
}

// LeafEdge is the edge the door's open leaf blocks for the 3 ticks after the door opens.
// open_and_close_door (content/scripts/doors/scripts/open_and_close_doors.rs2) swaps the door
// for an inviswall (the same edge, already closed here) and loc_adds the open door, a straight
// wall, on the tile across the door edge at angle+1 -- so for those 3 ticks one more edge of
// that tile is a wall. When the route crosses from the door's own tile, that tile is where the
// player lands (DoorInfo.To), and the leaf can stand right across the first step of the next
// walk: door (2898,4600)'s leaf blocks (2898,4599)-(2897,4599), the first step toward door
// (2895,4598); door (2916,4586)'s blocks (2916,4585)-(2915,4585), the first toward (2912,4584).
func (d DoorInfo) LeafEdge() (a, b Cell) {
	_, across := d.Sides()
	ax, az, bx, bz := straightEdge(across.X, across.Z, (d.Angle+1)%4)
	return Cell{X: ax, Z: az}, Cell{X: bx, Z: bz}
}

// ApproachTile picks the tile a live caller should stand on before clicking a door's
// Open option, given where the player currently is. A door produced by SolveRoute
// already knows (From); for any other DoorInfo it is whichever of the door's two
// Sides is Manhattan-closest to me — which is never a diagonal neighbour of the door
// tile, since both sides are on the door's own axis. On a tie (the player is already
// equidistant, e.g. standing exactly between them, which can't happen for adjacent
// cells, or far away in the room) the side DoorPassable accepts wins, so a gated door
// is approached from the side the content script will actually open it from.
func ApproachTile(d DoorInfo, me Cell) Cell {
	if d.From != (Cell{}) {
		return d.From
	}
	own, across := d.Sides()
	do, da := manhattan(me, own), manhattan(me, across)
	if do < da {
		return own
	}
	if da < do {
		return across
	}
	if DoorPassable(d, own.X, own.Z) {
		return own
	}
	return across
}

func manhattan(a, b Cell) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return dx + dz
}

// Graph is the built maze: wall edges (impassable, always), doors (conditionally
// passable, see DoorPassable), and the bounding box of every wall tile seen — used by
// SolveRoute to bound its BFS the same way the original does.
type Graph struct {
	WallEdge               map[string]bool
	Door                   map[string]DoorInfo
	MinX, MaxX, MinZ, MaxZ int
	// Solid holds the tiles nobody can walk onto or through: the maze's chests (centrepiece locs
	// block their tile). BuildMaze leaves it empty -- MazeLayout lists only walls and doors -- and
	// BuildLayoutGraph fills it from Chests, so every walk and route over the live layout keeps off
	// them, as the engine does.
	Solid map[Cell]bool
}

// EdgeKey is edgeKey: a canonical, direction-independent key for the edge between two
// adjacent tiles. Ported exactly, including its comparison shape (ax<bx || az<bz) —
// this only needs to be a CONSISTENT key for the same physical edge regardless of call
// order, not a strict total order, and every caller here (BuildMaze, SolveRoute) calls
// it symmetrically the same way the original does.
func EdgeKey(ax, az, bx, bz int) string {
	if ax < bx || az < bz {
		return fmt.Sprintf("%d,%d|%d,%d", ax, az, bx, bz)
	}
	return fmt.Sprintf("%d,%d|%d,%d", bx, bz, ax, az)
}

// straightEdge is straightEdge: the edge a shape-0 "wall straight" loc at (wx,wz)
// blocks, per angle (0=WEST,1=NORTH,2=EAST,3=SOUTH — confirmed against
// rsb0t-main/src/bot/event/webwalk/rsmod/flags.ts's LocAngle enum, same convention
// this project's maze.go already cites independently).
func straightEdge(wx, wz, angle int) (ax, az, bx, bz int) {
	switch angle {
	case 0:
		return wx, wz, wx - 1, wz // WEST
	case 1:
		return wx, wz, wx, wz + 1 // NORTH
	case 2:
		return wx, wz, wx + 1, wz // EAST
	default:
		return wx, wz, wx, wz - 1 // SOUTH
	}
}

// wallLAngles is WALL_L_ANGLES: a shape-2 "wall corner" loc blocks the two edges its
// angle's pair of straight-wall angles would.
var wallLAngles = map[int][2]int{0: {1, 0}, 1: {1, 2}, 2: {3, 2}, 3: {3, 0}}

// BuildMaze is buildMaze: turns the flat loc list into a Graph. Only WallID (shapes 0
// and 2) and DoorDirs ids contribute; everything else (the shrine, decoration) is
// silently ignored, matching the original exactly — MazeLayout only contains
// wall/door entries anyway (see its own doc comment), so this never matters in
// practice for the live layout, only for a synthetic test graph.
func BuildMaze(locs []Loc) *Graph {
	g := &Graph{
		WallEdge: make(map[string]bool),
		Door:     make(map[string]DoorInfo),
		Solid:    make(map[Cell]bool),
		MinX:     1<<31 - 1, MinZ: 1<<31 - 1,
		MaxX: -(1 << 31), MaxZ: -(1 << 31),
	}
	for _, l := range locs {
		wx, wz := Origin.X+l.LX, Origin.Z+l.LZ
		if l.ID == WallID {
			if wx < g.MinX {
				g.MinX = wx
			}
			if wx > g.MaxX {
				g.MaxX = wx
			}
			if wz < g.MinZ {
				g.MinZ = wz
			}
			if wz > g.MaxZ {
				g.MaxZ = wz
			}
			switch l.Shape {
			case 0:
				g.WallEdge[EdgeKey(straightEdge(wx, wz, l.Angle))] = true
			case 2:
				for _, a := range wallLAngles[l.Angle] {
					g.WallEdge[EdgeKey(straightEdge(wx, wz, a))] = true
				}
			}
		} else if _, ok := DoorDirs[l.ID]; ok {
			ax, az, bx, bz := straightEdge(wx, wz, l.Angle)
			g.Door[EdgeKey(ax, az, bx, bz)] = DoorInfo{Tile: Cell{X: wx, Z: wz}, ID: l.ID, Angle: l.Angle}
		}
	}
	return g
}

// DoorPassable is doorPassable: whether a door can be opened/walked through when
// approached from (fromX,fromZ). dir 0 opens from either side; dir 1 only from the
// side the door's own tile sits on along its angle's axis; dir 2 only from the
// opposite side.
func DoorPassable(door DoorInfo, fromX, fromZ int) bool {
	dir := DoorDirs[door.ID]
	if dir == 0 {
		return true
	}
	var axisTrue bool
	if door.Angle == 1 || door.Angle == 3 {
		axisTrue = fromZ == door.Tile.Z
	} else {
		axisTrue = fromX == door.Tile.X
	}
	if dir == 1 {
		return axisTrue
	}
	return !axisTrue
}

var cardinal = [4]Cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// isShrineTouchStand is isShrineTouchStand: true when (x,z) sits outside the shrine's
// footprint and shares a non-wall edge with it (a door edge counts, since it becomes
// passable once opened on the route).
func isShrineTouchStand(g *Graph, x, z int, shrine Cell, size int) bool {
	x1, z1 := shrine.X+size-1, shrine.Z+size-1
	if x >= shrine.X && x <= x1 && z >= shrine.Z && z <= z1 {
		return false
	}
	for _, c := range cardinal {
		nx, nz := x+c.X, z+c.Z
		if nx < shrine.X || nx > x1 || nz < shrine.Z || nz > z1 {
			continue
		}
		if g.WallEdge[EdgeKey(x, z, nx, nz)] {
			continue
		}
		return true
	}
	return false
}

// SolveRoute is solveRoute: a BFS from spawn to the nearest shrine-touch-stand tile,
// respecting wall edges (always blocked) and door edges (blocked unless
// DoorPassable). Returns the ordered list of doors that route actually opens — not
// every cell visited, matching the original's own return shape — or nil if no route
// reaches the shrine at all (a real, honest outcome per macro_event_maze's own layout,
// not a bug: solveMaze.ts's own comment notes a wrong spawn's route can dead-end on a
// wrong-side door, and its caller bails loudly rather than looping forever).
//
// Go-idiom deviation from solveRoute: the original returns only {x,z} tile pairs and
// leaves the caller to re-discover each door's type id live (via Locs.query()) before
// clicking it. This returns the full DoorInfo (tile, id, angle) instead, since this
// port's own live driver (internal/randomevents/maze.go) deliberately does NOT depend
// on live loc discovery at all (see maze.go's doc comment on why) — it already knows
// every door's real type id from this same static graph, so there is no reason to
// throw that away and force a second live lookup. Each returned door also carries
// From/To (see DoorInfo): the original relied on the real client's own click-to-walk
// to end up beside the door and on the engine to sort out the approach; this port
// walks to From explicitly, because "beside" can mean diagonal and a diagonal tile is
// exactly where the 2026-09-14 production incident got stuck.
func SolveRoute(g *Graph, spawn Cell, shrine Cell) []DoorInfo {
	finite := g.MinX <= g.MaxX
	lo := Cell{X: spawn.X, Z: spawn.Z}
	hi := Cell{X: spawn.X, Z: spawn.Z}
	if finite {
		lo, hi = Cell{X: g.MinX, Z: g.MinZ}, Cell{X: g.MaxX, Z: g.MaxZ}
	} else {
		if shrine.X < lo.X {
			lo.X = shrine.X
		}
		if shrine.Z < lo.Z {
			lo.Z = shrine.Z
		}
		if shrine.X > hi.X {
			hi.X = shrine.X
		}
		if shrine.Z > hi.Z {
			hi.Z = shrine.Z
		}
	}
	lo.X, lo.Z = lo.X-2, lo.Z-2
	hi.X, hi.Z = hi.X+2, hi.Z+2

	type prevEntry struct {
		p    Cell
		door *DoorInfo
	}
	prev := map[Cell]*prevEntry{spawn: nil}
	queue := []Cell{spawn}

	inShrine := func(x, z int) bool {
		return x >= shrine.X && x < shrine.X+ShrineSize && z >= shrine.Z && z < shrine.Z+ShrineSize
	}

	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		if isShrineTouchStand(g, cur.X, cur.Z, shrine, ShrineSize) {
			var doors []DoorInfo
			node := prev[cur]
			px, pz := cur.X, cur.Z
			for node != nil {
				if node.door != nil {
					doors = append([]DoorInfo{*node.door}, doors...)
				}
				px, pz = node.p.X, node.p.Z
				node = prev[Cell{X: px, Z: pz}]
			}
			// The chamber door itself is a valid touch stand, but the BFS never
			// crosses the door edge into the 3x3 (inShrine is excluded below), so it
			// still needs to be opened explicitly — matching the original's own
			// "why" comment.
			for _, c := range cardinal {
				nx, nz := cur.X+c.X, cur.Z+c.Z
				if nx < shrine.X || nx >= shrine.X+ShrineSize || nz < shrine.Z || nz >= shrine.Z+ShrineSize {
					continue
				}
				if ed, ok := g.Door[EdgeKey(cur.X, cur.Z, nx, nz)]; ok {
					dup := false
					for _, d := range doors {
						if d.Tile == ed.Tile {
							dup = true
							break
						}
					}
					if !dup {
						ed.From, ed.To = cur, Cell{X: nx, Z: nz}
						doors = append(doors, ed)
					}
				}
			}
			return doors
		}
		for _, c := range cardinal {
			nx, nz := cur.X+c.X, cur.Z+c.Z
			if nx < lo.X || nx > hi.X || nz < lo.Z || nz > hi.Z {
				continue
			}
			if inShrine(nx, nz) {
				continue
			}
			nk := Cell{X: nx, Z: nz}
			if g.Solid[nk] {
				continue
			}
			if _, seen := prev[nk]; seen {
				continue
			}
			ek := EdgeKey(cur.X, cur.Z, nx, nz)
			var doorPtr *DoorInfo
			if d, ok := g.Door[ek]; ok {
				if !DoorPassable(d, cur.X, cur.Z) {
					continue
				}
				dCopy := d
				dCopy.From, dCopy.To = cur, nk
				doorPtr = &dCopy
			} else if g.WallEdge[ek] {
				continue
			}
			prev[nk] = &prevEntry{p: cur, door: doorPtr}
			queue = append(queue, nk)
		}
	}
	return nil
}

// graphCache is the lazily-built, process-wide Graph — the map square is static, so
// this is built once from MazeLayout and reused, matching selectRoute.ts's own
// module-level `cached` variable. graphOnce makes the build safe for the many accounts that
// can reach it at the same moment (the cached Graph is read-only once built).
var (
	graphCache *Graph
	graphOnce  sync.Once
)

// BuildLayoutGraph returns the maze's Graph, built once from MazeLayout and cached —
// see selectRoute.ts's graph() for the original. The chests are added as Solid tiles.
func BuildLayoutGraph() *Graph {
	graphOnce.Do(func() {
		locs := make([]Loc, 0, len(MazeLayout)/LayoutStride)
		for i := 0; i+LayoutStride <= len(MazeLayout); i += LayoutStride {
			locs = append(locs, Loc{
				LX: MazeLayout[i], LZ: MazeLayout[i+1], ID: MazeLayout[i+2],
				Shape: MazeLayout[i+3], Angle: MazeLayout[i+4],
			})
		}
		g := BuildMaze(locs)
		for _, c := range Chests {
			g.Solid[c] = true
		}
		graphCache = g
	})
	return graphCache
}

// SelectRoute is selectRoute: solves a door route to the shrine from wherever the
// player actually is, using the cached static layout graph. Returns nil if no route
// exists from that tile (see SolveRoute's own doc comment on why that's a real,
// expected outcome sometimes, not a bug).
func SelectRoute(me Cell) []DoorInfo {
	return SolveRoute(BuildLayoutGraph(), me, Shrine)
}

// chebyshevDist is the Chebyshev (chessboard) distance between two cells.
func chebyshevDist(a, b Cell) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// openNeighbors returns c's cardinal neighbours that aren't blocked by a permanent
// wall edge or an as-yet-unopened door edge — i.e., tiles reachable by plain walking
// without needing to interact with anything first.
func (g *Graph) openNeighbors(c Cell) []Cell {
	var out []Cell
	for _, d := range cardinal {
		n := Cell{X: c.X + d.X, Z: c.Z + d.Z}
		if g.Solid[n] {
			continue
		}
		ek := EdgeKey(c.X, c.Z, n.X, n.Z)
		if g.WallEdge[ek] {
			continue
		}
		if _, isDoor := g.Door[ek]; isDoor {
			continue
		}
		out = append(out, n)
	}
	return out
}

// PathWithinRoom is NOT part of the original TS source — solveMaze.ts instead resends
// a raw click toward the target and polls (mazeWalk.ts's walkTowards), relying on the
// real client's own click-to-walk pathfinding to route around obstacles locally. This
// Go client has no equivalent "click somewhere far and auto-route" primitive — its
// MOVE_GAMECLICK sender (internal/actions.Walk) takes an explicit waypoint list, the
// same way internal/pathfind.Pack.FindPath's callers already work — so this is a
// small BFS over the SAME wall/door edge model SolveRoute already uses (walls always
// blocked, doors blocked until explicitly opened) to compute that waypoint list for
// walking within the player's current room, up to a tile within stopDist of target.
// Returns the cell sequence including `from` (so callers can build waypoints
// directly), or nil if target isn't reachable through open floor within maxCells
// expansions (e.g. it's on the far side of a door not yet opened, which is expected —
// the caller should be routing door-to-door, not room-to-room, with this).
func (g *Graph) PathWithinRoom(from, target Cell, stopDist, maxCells int) []Cell {
	return g.pathWithinRoom(from, target, stopDist, maxCells, nil)
}

// PathAvoiding is PathWithinRoom to exactly target that also treats every edge in avoid
// (keyed by EdgeKey) as a wall -- for a walk that must not start across a door's open leaf
// (see DoorInfo.LeafEdge). Nil when no such path exists.
func (g *Graph) PathAvoiding(from, target Cell, maxCells int, avoid map[string]bool) []Cell {
	return g.pathWithinRoom(from, target, 0, maxCells, avoid)
}

func (g *Graph) pathWithinRoom(from, target Cell, stopDist, maxCells int, avoid map[string]bool) []Cell {
	if chebyshevDist(from, target) <= stopDist {
		return []Cell{from}
	}
	prev := map[Cell]Cell{from: from}
	queue := []Cell{from}
	for head := 0; head < len(queue) && head < maxCells; head++ {
		cur := queue[head]
		if chebyshevDist(cur, target) <= stopDist {
			var path []Cell
			for c := cur; ; {
				path = append([]Cell{c}, path...)
				if c == from {
					return path
				}
				c = prev[c]
			}
		}
		for _, n := range g.openNeighbors(cur) {
			if _, seen := prev[n]; seen {
				continue
			}
			if avoid[EdgeKey(cur.X, cur.Z, n.X, n.Z)] {
				continue
			}
			prev[n] = cur
			queue = append(queue, n)
		}
	}
	return nil
}

// Waypoints is what one MOVE_GAMECLICK should carry for a cardinal cell path whose first cell
// is the player's own tile: each tile where the path turns, then the last -- never the
// player's own tile. That is the real client's own shape (Client.ts tryMove walks its route
// back from the destination and stops on reaching the source, so the source is never sent).
// A waypoint on the player's own tile is not free: the engine (PathingEntity.takeStep) spends
// that tick "reaching" it, with no step and no run step, so every walk the solver used to
// send began with a lost tick. Straight runs between corners are walked cell by cell by the
// engine exactly as the path has them, so the route is unchanged. Nil for a path of one cell.
func Waypoints(path []Cell) []Cell {
	if len(path) < 2 {
		return nil
	}
	var out []Cell
	for i := 1; i < len(path)-1; i++ {
		inX, inZ := path[i].X-path[i-1].X, path[i].Z-path[i-1].Z
		outX, outZ := path[i+1].X-path[i].X, path[i+1].Z-path[i].Z
		if inX != outX || inZ != outZ {
			out = append(out, path[i])
		}
	}
	return append(out, path[len(path)-1])
}
