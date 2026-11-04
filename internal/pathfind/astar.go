package pathfind

import "sync/atomic"

// NavPoint is one absolute world tile.
type NavPoint struct {
	X, Z, Level int
}

// DX/DZ mirror PathFinder.ts's own direction table exactly — the pack's exit-mask bits
// were baked using this same ordering, so this must not be reordered independently.
var dx = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var dz = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

// cardinalSides mirrors PathFinder.ts's CARDINAL_SIDES: the four orthogonal offsets
// paired with the wall-facing bit that must be clear on the candidate tile for it to
// count as a usable approach to a blocked neighbour.
var cardinalSides = [4]struct {
	dx, dz int
	bit    byte
}{
	{0, 1, 1 << 2},
	{1, 0, 1 << 3},
	{0, -1, 1 << 0},
	{-1, 0, 1 << 1},
}

const footprintRadius = 2

func nodeID(x, z, level int) int64 {
	return int64(level)<<28 | int64(x)<<14 | int64(z)
}

func nodeX(id int64) int     { return int((id >> 14) & 0x3fff) }
func nodeZ(id int64) int     { return int(id & 0x3fff) }
func nodeLevel(id int64) int { return int((id >> 28) & 0x3) }

func chebyshev(ax, az, bx, bz int) int {
	dx := ax - bx
	if dx < 0 {
		dx = -dx
	}
	dz := az - bz
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// PathResult is the outcome of a FindPath call.
type PathResult struct {
	OK        bool
	Reason    string
	Waypoints []NavPoint // corner-compressed: direction-change points plus the endpoints
	Expanded  int
}

// Steps is how many tiles the route actually walks — the thing a caller choosing between two
// destinations by "which is nearer" almost always means, and until now never had. The package
// doc above lists four times a layer above this one took a short straight line as proof
// something could be REACHED; this is the milder, quieter version of the same mistake, taking a
// short straight line as proof something is CLOSE. ProgressiveFisher's spot selection scored
// candidates by straight-line distance while the reachability gate immediately below it was
// already running this very search to each one and discarding the answer: across water, a spot
// five tiles away in a straight line is routinely a 38-step walk round the bay.
//
// Waypoints are corner-compressed, so every segment between two consecutive ones is a straight
// run in one of the eight directions and its length is exactly the Chebyshev distance across it.
// Summing those gives the same count `internal/webwalk`'s own Route.Cost reports for a
// long-distance plan, at no extra search cost: the A* has already run by the time anyone can
// call this.
//
// A result that is not OK has no route, so Steps is 0 — check OK first. A route whose endpoints
// are the same tile is legitimately 0 as well.
func (r PathResult) Steps() int {
	n := 0
	for i := 1; i < len(r.Waypoints); i++ {
		a, b := r.Waypoints[i-1], r.Waypoints[i]
		n += chebyshev(a.X, a.Z, b.X, b.Z)
	}
	return n
}

// snapWalkable finds the nearest walkable, exit-having tile to p within radius tiles
// (Chebyshev rings), mirroring PathFinder.ts's snapWalkable for the no-edges case.
func (p *Pack) snapWalkable(pt NavPoint, radius int) (NavPoint, bool) {
	usable := func(x, z int) (NavPoint, bool) {
		if !p.Walkable(x, z, pt.Level) {
			return NavPoint{}, false
		}
		if p.ExitMask(x, z, pt.Level) == 0 {
			return NavPoint{}, false
		}
		return NavPoint{X: x, Z: z, Level: pt.Level}, true
	}
	if here, ok := usable(pt.X, pt.Z); ok {
		return here, true
	}
	for r := 1; r <= radius; r++ {
		for dxo := -r; dxo <= r; dxo++ {
			for dzo := -r; dzo <= r; dzo++ {
				if max(abs(dxo), abs(dzo)) != r {
					continue
				}
				if hit, ok := usable(pt.X+dxo, pt.Z+dzo); ok {
					return hit, true
				}
			}
		}
	}
	return NavPoint{}, false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// blockedFootprint floods outward from an unwalkable tile over other unwalkable tiles
// (within footprintRadius), the same "read the loc's solid footprint back off
// collision" trick PathFinder.ts uses since the pack does not carry loc extents
// directly.
func (p *Pack) blockedFootprint(pt NavPoint) []NavPoint {
	seen := map[[2]int]bool{{pt.X, pt.Z}: true}
	solid := []NavPoint{pt}
	stack := []NavPoint{pt}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, side := range cardinalSides {
			nx, nz := cur.X+side.dx, cur.Z+side.dz
			if chebyshev(nx, nz, pt.X, pt.Z) > footprintRadius {
				continue
			}
			key := [2]int{nx, nz}
			if seen[key] || p.Walkable(nx, nz, pt.Level) {
				continue
			}
			seen[key] = true
			tile := NavPoint{X: nx, Z: nz, Level: pt.Level}
			solid = append(solid, tile)
			stack = append(stack, tile)
		}
	}
	return solid
}

// besideAll returns every walkable tile orthogonally adjacent to one of tiles whose
// facing side isn't walled off, within footprintRadius of at.
func (p *Pack) besideAll(tiles []NavPoint, at NavPoint) map[int64]bool {
	goals := make(map[int64]bool)
	for _, t := range tiles {
		for _, side := range cardinalSides {
			cx, cz := t.X+side.dx, t.Z+side.dz
			if chebyshev(cx, cz, at.X, at.Z) > footprintRadius {
				continue
			}
			if p.Walkable(cx, cz, at.Level) && p.WallMask(cx, cz, at.Level)&side.bit == 0 {
				goals[nodeID(cx, cz, at.Level)] = true
			}
		}
	}
	return goals
}

// cardinalGoals mirrors PathFinder.ts: the walkable tiles immediately beside an
// unwalkable destination (or its whole blocked footprint, when its own neighbours are
// also solid — a multi-tile loc).
func (p *Pack) cardinalGoals(pt NavPoint) map[int64]bool {
	if p.Walkable(pt.X, pt.Z, pt.Level) {
		return nil
	}
	beside := p.besideAll([]NavPoint{pt}, pt)
	if len(beside) > 0 {
		return beside
	}
	return p.besideAll(p.blockedFootprint(pt), pt)
}

// goalCandidates expands cardinalGoals outward (via exit-mask connectivity) up to
// radius tiles from pt, falling back to a plain radius scan if that BFS finds nothing
// (e.g. pt itself is walkable, or nothing is adjacent-and-connected within range).
func (p *Pack) goalCandidates(pt NavPoint, radius int) map[int64]bool {
	goals := make(map[int64]bool)
	if p.Walkable(pt.X, pt.Z, pt.Level) {
		goals[nodeID(pt.X, pt.Z, pt.Level)] = true
		return goals
	}
	seed := p.cardinalGoals(pt)
	queue := make([]NavPoint, 0, len(seed))
	seen := make(map[int64]bool, len(seed))
	for id := range seed {
		queue = append(queue, NavPoint{X: nodeX(id), Z: nodeZ(id), Level: nodeLevel(id)})
		seen[id] = true
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		goals[nodeID(cur.X, cur.Z, cur.Level)] = true
		mask := p.ExitMask(cur.X, cur.Z, cur.Level)
		for dir := 0; dir < 8; dir++ {
			if mask&(1<<uint(dir)) == 0 {
				continue
			}
			nx, nz := cur.X+dx[dir], cur.Z+dz[dir]
			if chebyshev(nx, nz, pt.X, pt.Z) > radius {
				continue
			}
			id := nodeID(nx, nz, cur.Level)
			if seen[id] || !p.Walkable(nx, nz, cur.Level) {
				continue
			}
			seen[id] = true
			queue = append(queue, NavPoint{X: nx, Z: nz, Level: cur.Level})
		}
	}
	if len(goals) > 0 {
		return goals
	}
	for dxo := -radius; dxo <= radius; dxo++ {
		for dzo := -radius; dzo <= radius; dzo++ {
			if p.Walkable(pt.X+dxo, pt.Z+dzo, pt.Level) {
				goals[nodeID(pt.X+dxo, pt.Z+dzo, pt.Level)] = true
			}
		}
	}
	return goals
}

// minHeap is a small binary min-heap of (priority, nodeID) pairs, mirroring
// PathFinder.ts's MinHeap.
type minHeap struct {
	keys []int64
	ids  []int64
}

func (h *minHeap) push(key, id int64) {
	h.keys = append(h.keys, key)
	h.ids = append(h.ids, id)
	i := len(h.ids) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if h.keys[parent] <= h.keys[i] {
			break
		}
		h.keys[parent], h.keys[i] = h.keys[i], h.keys[parent]
		h.ids[parent], h.ids[i] = h.ids[i], h.ids[parent]
		i = parent
	}
}

func (h *minHeap) pop() int64 {
	top := h.ids[0]
	lastKey := h.keys[len(h.keys)-1]
	lastID := h.ids[len(h.ids)-1]
	h.keys = h.keys[:len(h.keys)-1]
	h.ids = h.ids[:len(h.ids)-1]
	if len(h.ids) > 0 {
		h.keys[0] = lastKey
		h.ids[0] = lastID
		i := 0
		for {
			left, right := 2*i+1, 2*i+2
			smallest := i
			if left < len(h.ids) && h.keys[left] < h.keys[smallest] {
				smallest = left
			}
			if right < len(h.ids) && h.keys[right] < h.keys[smallest] {
				smallest = right
			}
			if smallest == i {
				break
			}
			h.keys[smallest], h.keys[i] = h.keys[i], h.keys[smallest]
			h.ids[smallest], h.ids[i] = h.ids[i], h.ids[smallest]
			i = smallest
		}
	}
	return top
}

func (h *minHeap) len() int { return len(h.ids) }

const maxExpansions = 1_200_000

// searchCount and expandedCount total every A* search this process has run and the tiles those
// searches expanded. They exist so a caller that plans many routes (the combat flee) can be held to a
// bound by a test and measured in a log line: the live fleet's 15 Oct 2026 profile had 39% of all CPU
// in search, from a flee that ran dozens of failing full-component searches a plan.
var searchCount, expandedCount atomic.Int64

// SearchStats is the process-wide number of A* searches run so far and the tiles they expanded in
// all. Subtract two readings to measure the work between them.
func SearchStats() (searches, expanded int64) {
	return searchCount.Load(), expandedCount.Load()
}

// FindPath computes a walkable-tile-only route from `from` to `to` (v1: no doors,
// transports, or teleports — see the package doc). Ported from PathFinder.ts's
// findPath/search/reconstruct, with the transport/requires/policy/essence layers
// stripped since none of that is in the pack this reads yet.
func (p *Pack) FindPath(from, to NavPoint) PathResult {
	return p.FindPathAvoiding(from, to, nil)
}

// FindPathAvoiding is FindPath plus an optional avoid predicate (nil behaves exactly like
// FindPath): a tile the predicate reports true for is never expanded into during the
// search, so a route through it is never produced, but it doesn't otherwise change
// start/goal resolution — a start or destination tile that happens to fall inside an
// avoided zone still resolves normally (snapWalkable/cardinalGoals/goalCandidates don't
// consult avoid), only the tiles a route would otherwise pass THROUGH are filtered. Added
// for internal/scripts/freshstartrunner's own danger-zone avoidance (see that package's
// dangerZones var) rather than as a generic feature speculatively built ahead of a need.
func (p *Pack) FindPathAvoiding(from, to NavPoint, avoid func(x, z, level int) bool) PathResult {
	start, ok := p.snapWalkable(from, 2)
	if !ok {
		return PathResult{Reason: "start not walkable"}
	}

	cardinal := p.cardinalGoals(to)
	if len(cardinal) > 0 {
		if r := p.search(start, to, cardinal, 1, avoid); r.OK {
			return r
		}
	}

	goals := p.goalCandidates(to, 5)
	if len(goals) == 0 {
		return PathResult{Reason: "target not walkable within 5 tiles"}
	}
	slack := 5
	if len(goals) == 1 && goals[nodeID(to.X, to.Z, to.Level)] {
		slack = 0
	}
	return p.search(start, to, goals, slack, avoid)
}

func (p *Pack) search(from, to NavPoint, goals map[int64]bool, goalSlack int, avoid func(x, z, level int) bool) PathResult {
	searchCount.Add(1)
	expanded := 0
	defer func() { expandedCount.Add(int64(expanded)) }()
	startID := nodeID(from.X, from.Z, from.Level)
	goalX, goalZ := to.X, to.Z
	if avoid == nil && p.unreach.excludes(startID, goals) {
		return PathResult{Reason: "unreachable"}
	}

	gScore := make(map[int64]int)
	cameFrom := make(map[int64]int64)
	closed := make(map[int64]bool)
	open := &minHeap{}

	cheb := func(x, z int) int {
		d := chebyshev(x, z, goalX, goalZ) - goalSlack
		if d < 0 {
			d = 0
		}
		return d
	}

	gScore[startID] = 0
	open.push(int64(cheb(from.X, from.Z))*1048576, startID)

	for open.len() > 0 {
		current := open.pop()
		if closed[current] {
			continue
		}
		closed[current] = true

		if goals[current] {
			return reconstruct(startID, current, expanded, cameFrom)
		}
		expanded++
		if expanded > maxExpansions {
			return PathResult{Reason: "expansion budget exceeded", Expanded: expanded}
		}

		x, z, level := nodeX(current), nodeZ(current), nodeLevel(current)
		g := gScore[current]
		mask := p.ExitMask(x, z, level)
		for dir := 0; dir < 8; dir++ {
			if mask&(1<<uint(dir)) == 0 {
				continue
			}
			nx, nz := x+dx[dir], z+dz[dir]
			neighbor := nodeID(nx, nz, level)
			if closed[neighbor] {
				continue
			}
			if avoid != nil && avoid(nx, nz, level) {
				continue
			}
			tentative := g + 1
			if known, ok := gScore[neighbor]; ok && known <= tentative {
				continue
			}
			gScore[neighbor] = tentative
			cameFrom[neighbor] = current
			open.push(int64(tentative+cheb(nx, nz))*1048576-int64(tentative), neighbor)
		}
	}
	if avoid == nil {
		p.unreach.remember(closed)
	}
	return PathResult{Reason: "unreachable", Expanded: expanded}
}

// reconstruct walks cameFrom back to start and compresses the result to direction-change
// corners plus both endpoints — exactly the shape MOVE_GAMECLICK's waypoint encoding
// wants (see internal/actions/walk.go).
func reconstruct(start, goal int64, expanded int, cameFrom map[int64]int64) PathResult {
	var chain []int64
	for node := goal; ; {
		chain = append(chain, node)
		if node == start {
			break
		}
		prev, ok := cameFrom[node]
		if !ok {
			return PathResult{Reason: "reconstruction broke (bug)", Expanded: expanded}
		}
		node = prev
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	point := func(id int64) NavPoint {
		return NavPoint{X: nodeX(id), Z: nodeZ(id), Level: nodeLevel(id)}
	}
	stepDir := func(a, b int64) (int, int) {
		ax, az := nodeX(a), nodeZ(a)
		bx, bz := nodeX(b), nodeZ(b)
		sign := func(n int) int {
			if n > 0 {
				return 1
			} else if n < 0 {
				return -1
			}
			return 0
		}
		return sign(bx - ax), sign(bz - az)
	}

	waypoints := []NavPoint{point(chain[0])}
	for i := 1; i < len(chain); i++ {
		last := i == len(chain)-1
		turn := false
		if !last {
			dx1, dz1 := stepDir(chain[i-1], chain[i])
			dx2, dz2 := stepDir(chain[i], chain[i+1])
			turn = dx1 != dx2 || dz1 != dz2
		}
		if turn || last {
			waypoints = append(waypoints, point(chain[i]))
		}
	}

	return PathResult{OK: true, Waypoints: waypoints, Expanded: expanded}
}
