package pathfind

// Reach is the ground a walk from one tile can get to, found in a single breadth-first pass by the
// same exit masks FindPath steps by, with the number of steps to every tile in it.
//
// WHY IT EXISTS. A caller that has many candidate destinations and wants the first that can be
// walked to (the combat flee's ring of candidates) used to run FindPathAvoiding on each in turn,
// and a search that cannot reach its goal expands the start's whole component before it says so.
// Where most candidates are on unreachable ground that is one full flood per candidate: the live
// fleet's 30 s profile of 15 Oct 2026 had 39% of all CPU in Pack.search, from accounts in the
// Asgarnian ice dungeon whose flee plan ran 298 searches and 57,000 expansions to reach a single
// answer, over and over. One Reach answers "can I get there" for every candidate, exactly, and the
// callers search only the candidates that can be.
//
// A Reach covers a square of the given radius around its start and one level, held in a flat array
// (a flood of the flee's 96-tile window is 149 KB and no hashing, where a map of the same ground was
// 1.4 MB of garbage a plan). It honours the same avoid predicate FindPathAvoiding does: a tile it
// reports true for is never entered, and the start is not tested. A tile outside the square is never
// entered either, as if avoid refused it, so FindPathAvoiding(from, to, avoid) with a window
// predicate of the same radius answers exactly what the Reach does.
type Reach struct {
	p      *Pack
	start  NavPoint
	level  int
	x0, z0 int // the south-west corner of the square
	w      int // its side, 2*radius+1
	steps  []int32
	size   int
}

// ReachFrom floods the ground reachable from `from` (snapped to the nearest walkable tile within
// two, as FindPath does) inside the square of the given radius (at least 2) around from, without
// entering a tile avoid reports true for. An unwalkable start answers an empty Reach.
func (p *Pack) ReachFrom(from NavPoint, radius int, avoid func(x, z, level int) bool) *Reach {
	if radius < 2 {
		radius = 2
	}
	r := &Reach{p: p, level: from.Level, x0: from.X - radius, z0: from.Z - radius, w: 2*radius + 1}
	start, ok := p.snapWalkable(from, 2)
	if !ok {
		return r
	}
	r.start = start
	r.steps = make([]int32, r.w*r.w)
	si := r.index(start.X, start.Z, start.Level)
	r.steps[si] = 1
	r.size = 1
	queue := make([]int32, 1, 1024)
	queue[0] = int32(si)
	for head := 0; head < len(queue); head++ {
		cur := int(queue[head])
		x, z := r.x0+cur%r.w, r.z0+cur/r.w
		g := r.steps[cur]
		mask := p.ExitMask(x, z, r.level)
		for dir := 0; dir < 8; dir++ {
			if mask&(1<<uint(dir)) == 0 {
				continue
			}
			nx, nz := x+dx[dir], z+dz[dir]
			ni := r.index(nx, nz, r.level)
			if ni < 0 || r.steps[ni] != 0 {
				continue
			}
			if avoid != nil && avoid(nx, nz, r.level) {
				continue
			}
			r.steps[ni] = g + 1
			r.size++
			queue = append(queue, int32(ni))
		}
	}
	return r
}

// index is the tile's cell in the square, or -1 outside it or on another level.
func (r *Reach) index(x, z, level int) int {
	if level != r.level {
		return -1
	}
	cx, cz := x-r.x0, z-r.z0
	if cx < 0 || cz < 0 || cx >= r.w || cz >= r.w {
		return -1
	}
	return cz*r.w + cx
}

// Start is the walkable tile the flood began on (the snapped start), or the zero point when the
// start was unwalkable.
func (r *Reach) Start() NavPoint { return r.start }

// Size is how many tiles the walk can reach, the start included.
func (r *Reach) Size() int { return r.size }

// Steps is how many steps the shortest walk to (x,z,level) takes, and false when the tile cannot
// be reached. Diagonal steps count one, as the engine moves, so for a reachable walkable
// destination it equals the length of the route FindPathAvoiding would return.
func (r *Reach) Steps(x, z, level int) (int, bool) {
	i := r.index(x, z, level)
	if i < 0 || r.steps == nil || r.steps[i] == 0 {
		return 0, false
	}
	return int(r.steps[i] - 1), true
}

// InteractSteps is how many steps the walk to the nearest tile an interaction with whatever stands on target is accepted from takes
// (Pack.InteractTiles: target's own tile, or a neighbour it touches with no wall between), over the ground the flood reached; -1 when
// no such tile was. It is not Steps of the target's own tile: a monster may stand on a tile the baked collision calls blocked and be as
// fightable from the tile beside it, and one behind a fence has no such tile in the flood however near it is.
func (r *Reach) InteractSteps(target NavPoint) int {
	if r.p == nil || r.size == 0 {
		return -1
	}
	best := -1
	for _, at := range r.p.InteractTiles(target) {
		if n, ok := r.Steps(at.X, at.Z, at.Level); ok && (best < 0 || n < best) {
			best = n
		}
	}
	return best
}

// Each calls fn for every reached tile, west to east along each row from the south, so the order
// is the same every time; fn returns false to stop.
func (r *Reach) Each(fn func(x, z, level, steps int) bool) {
	for i, g := range r.steps {
		if g == 0 {
			continue
		}
		if !fn(r.x0+i%r.w, r.z0+i/r.w, r.level, int(g-1)) {
			return
		}
	}
}

// goalRadius is how far from its target FindPathAvoiding looks for a tile to settle for when the
// target itself cannot be stood on.
const goalRadius = 5

// CanReach reports whether FindPathAvoiding(start, to, avoid) would find a route: whether any tile
// it would accept as arriving at `to` (the target itself when walkable, otherwise the walkable
// tiles beside it, up to five away) is in the reached ground. Exact for the same avoid predicate,
// and it expands nothing.
func (r *Reach) CanReach(to NavPoint) bool {
	if r.size == 0 || !r.near(to, goalRadius) {
		return false
	}
	for id := range r.p.goalCandidates(to, goalRadius) {
		if _, ok := r.Steps(nodeX(id), nodeZ(id), nodeLevel(id)); ok {
			return true
		}
	}
	return false
}

// near is a cheap necessary condition for CanReach: some reached tile lies within radius of pt.
// Every tile FindPathAvoiding accepts as arriving at pt is within goalRadius of it, so a candidate
// with none near it is rejected for 121 lookups instead of the goal search's allocations.
func (r *Reach) near(pt NavPoint, radius int) bool {
	for x := pt.X - radius; x <= pt.X+radius; x++ {
		for z := pt.Z - radius; z <= pt.Z+radius; z++ {
			if _, ok := r.Steps(x, z, pt.Level); ok {
				return true
			}
		}
	}
	return false
}
