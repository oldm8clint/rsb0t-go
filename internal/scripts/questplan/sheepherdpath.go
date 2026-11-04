package questplan

// Sheep Herder's push planner: the pure half of herding a diseased sheep over the enclosure's gate by prodding it, ported from the
// TypeScript def's herdPath.ts and checked against content 289 (quests/quest_sheepherder diseased_sheep.rs2). It knows nothing of a
// client: it reads the ground through a HerdGrid and answers "which tile do I stand on, and which way does the sheep go".
//
// THE PUSH. [opnpc1,plaguesheep_N] (prod_sheep) sends the sheep ONE tile along coord_direction(player, sheep): a cardinal direction
// that points away from the player (a diagonal offset goes to the longer axis, east or west on a tie). So the push an account
// wants names the tile it has to stand on, the one directly opposite the direction, and a push whose stand is not walkable does not
// exist however open the sheep's own destination is. The Prod op reaches the sheep from that stand or from none, so the stand also
// has to be able to step onto the sheep (the second sheep's field ends in a fence both sides of which are walkable and neither
// passable). The sheep's own step is npc_walk(movecoord_indirection(sheep, direction, 1)): it goes if the ground lets it.
//
// THE TRAPS. A walkable tile with no standable side is a trap: a sheep can wander onto it and no prod can move it, and the engine
// frees it only by the wander mode's 500 tick teleport home, which a prod resets. So the planner searches twice, once over the tiles
// that touch no trap and once over everything, and charges a flat penalty for a route that leaves the clear network (HerdOffNetwork).
// One potential for both searches: switching potentials at the boundary sent a sheep back and forth for 250 pushes.
//
// THE JUMP. After the npc_walk the script reads inzone_coord_pair_table(sheepherder_pen_gate, npc_coord): a sheep standing on the
// gate zone's three columns west of the fence (x 2592..2594, z 3360..3363) is teleported into the enclosure by any prod issued then,
// whatever its direction. Reaching that zone is the goal of the search.

// HerdGrid is what the planner needs to know about the ground, so the search is testable without a scene.
type HerdGrid interface {
	// Walkable reports whether a tile can be stood on at all.
	Walkable(x, z int) bool
	// CanStep reports whether a size-1 entity standing on (x, z) may take one cardinal step (dx, dz).
	CanStep(x, z, dx, dz int) bool
}

// HerdBox is an inclusive tile rectangle on the ground floor.
type HerdBox struct{ X0, X1, Z0, Z1 int }

// Contains reports whether (x, z) lies in the box.
func (b HerdBox) Contains(x, z int) bool { return x >= b.X0 && x <= b.X1 && z >= b.Z0 && z <= b.Z1 }

// HerdDir is a cardinal direction.
type HerdDir struct{ DX, DZ int }

// HerdDirs are north, east, south and west: coord_direction returns nothing else.
var HerdDirs = [4]HerdDir{{0, 1}, {1, 0}, {0, -1}, {-1, 0}}

// HerdMargin is how far outside the sheep and goal bounding box the search may wander.
const HerdMargin = 24

// HerdOffNetwork is how much worse (in pushes) a route that leaves the trap-free network counts.
const HerdOffNetwork = 1000

func herdKey(x, z int) int { return x*100_000 + z }

// HerdPushable reports whether a sheep on (x, z) can be pushed one tile in dir: it can step that way, the tile beyond is ground, the
// stand opposite is ground, and the stand can step onto the sheep.
func HerdPushable(g HerdGrid, x, z int, dir HerdDir) bool {
	return g.CanStep(x, z, dir.DX, dir.DZ) &&
		g.Walkable(x+dir.DX, z+dir.DZ) &&
		g.Walkable(x-dir.DX, z-dir.DZ) &&
		g.CanStep(x-dir.DX, z-dir.DZ, dir.DX, dir.DZ)
}

// HerdPinned reports whether no push at all can be issued at (x, z): a trap.
func HerdPinned(g HerdGrid, x, z int) bool {
	if !g.Walkable(x, z) {
		return false
	}
	for _, dir := range HerdDirs {
		if HerdPushable(g, x, z, dir) {
			return false
		}
	}
	return true
}

// herdRisky reports whether (x, z) touches a trap, and so is one wander step from losing the sheep.
func herdRisky(x, z int, pinned func(x, z int) bool) bool {
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if pinned(x+dx, z+dz) {
				return true
			}
		}
	}
	return false
}

func herdBounds(sx, sz int, goal HerdBox) HerdBox {
	return HerdBox{
		X0: min(sx, goal.X0) - HerdMargin, X1: max(sx, goal.X1) + HerdMargin,
		Z0: min(sz, goal.Z0) - HerdMargin, Z1: max(sz, goal.Z1) + HerdMargin,
	}
}

// herdSearch is the backwards search from the goal: each tile's distance in pushes to a goal tile, skipping tiles skip names.
func herdSearch(g HerdGrid, box, goal HerdBox, skip func(x, z int) bool) map[int]int {
	dist := map[int]int{}
	type pos struct{ x, z int }
	var queue []pos
	skipped := func(x, z int) bool { return skip != nil && skip(x, z) }
	for x := goal.X0; x <= goal.X1; x++ {
		for z := goal.Z0; z <= goal.Z1; z++ {
			if !g.Walkable(x, z) || skipped(x, z) {
				continue
			}
			// The jump prod needs a stand of its own, or a sheep parked in the gate zone can never be sent over.
			stand := false
			for _, d := range HerdDirs {
				if g.Walkable(x-d.DX, z-d.DZ) {
					stand = true
					break
				}
			}
			if !stand {
				continue
			}
			dist[herdKey(x, z)] = 0
			queue = append(queue, pos{x, z})
		}
	}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		next := dist[herdKey(cur.x, cur.z)] + 1
		for _, dir := range HerdDirs {
			px, pz := cur.x-dir.DX, cur.z-dir.DZ
			if !box.Contains(px, pz) {
				continue
			}
			if _, seen := dist[herdKey(px, pz)]; seen || skipped(px, pz) {
				continue
			}
			if !g.Walkable(px, pz) || !HerdPushable(g, px, pz, dir) {
				continue
			}
			dist[herdKey(px, pz)] = next
			queue = append(queue, pos{px, pz})
		}
	}
	return dist
}

// HerdPlan holds both route maps for one sheep position.
type HerdPlan struct {
	// Clear are routes that never touch a trap's neighbourhood; absent where none exists.
	Clear map[int]int
	// Any are routes over every tile a push can drive.
	Any map[int]int
}

// NewHerdPlan searches both ways from the goal, sharing one trap lookup, for a sheep near (sx, sz).
func NewHerdPlan(g HerdGrid, sx, sz int, goal HerdBox) HerdPlan {
	box := herdBounds(sx, sz, goal)
	memo := map[int]bool{}
	pinned := func(x, z int) bool {
		k := herdKey(x, z)
		if v, ok := memo[k]; ok {
			return v
		}
		v := HerdPinned(g, x, z)
		memo[k] = v
		return v
	}
	return HerdPlan{
		Clear: herdSearch(g, box, goal, func(x, z int) bool { return herdRisky(x, z, pinned) }),
		Any:   herdSearch(g, box, goal, nil),
	}
}

// Cost is the pushes from (x, z) to the goal; a route that touches a trap counts HerdOffNetwork worse. ok is false when no push route
// exists at all.
func (p HerdPlan) Cost(x, z int) (int, bool) {
	if c, ok := p.Clear[herdKey(x, z)]; ok {
		return c, true
	}
	if c, ok := p.Any[herdKey(x, z)]; ok {
		return c + HerdOffNetwork, true
	}
	return 0, false
}

// HerdDirections is every legal push from the sheep's tile, closest to the goal first. plan, when given, is reused.
func HerdDirections(g HerdGrid, x, z int, goal HerdBox, plan *HerdPlan) []HerdDir {
	routes := plan
	if routes == nil {
		p := NewHerdPlan(g, x, z, goal)
		routes = &p
	}
	type scored struct {
		dir  HerdDir
		cost int
	}
	var out []scored
	for _, dir := range HerdDirs {
		if !HerdPushable(g, x, z, dir) {
			continue
		}
		c, ok := routes.Cost(x+dir.DX, z+dir.DZ)
		if !ok {
			continue
		}
		out = append(out, scored{dir, c})
	}
	// Stable insertion sort: at most four entries, and equal costs keep HerdDirs' order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].cost < out[j-1].cost; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	dirs := make([]HerdDir, len(out))
	for i, s := range out {
		dirs[i] = s.dir
	}
	return dirs
}

// HerdAnyStand lists every direction a prod may be issued in for a sheep already standing in the gate zone, where the jump reads the
// sheep's own coordinate and any prod sends it over: the directions whose stand tile exists and can step onto the sheep, nearest
// stand to (meX, meZ) first.
func HerdAnyStand(g HerdGrid, x, z, meX, meZ int) []HerdDir {
	var dirs []HerdDir
	for _, d := range HerdDirs {
		if g.Walkable(x-d.DX, z-d.DZ) && g.CanStep(x-d.DX, z-d.DZ, d.DX, d.DZ) {
			dirs = append(dirs, d)
		}
	}
	cost := func(d HerdDir) int { return max(abs(x-d.DX-meX), abs(z-d.DZ-meZ)) }
	for i := 1; i < len(dirs); i++ {
		for j := i; j > 0 && cost(dirs[j]) < cost(dirs[j-1]); j-- {
			dirs[j], dirs[j-1] = dirs[j-1], dirs[j]
		}
	}
	return dirs
}
