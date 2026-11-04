package webwalk

import (
	"fmt"
	"math"
)

// Collision is the walkable-tile graph a Planner searches. *pathfind.Pack satisfies it.
type Collision interface {
	Walkable(x, z, level int) bool
	// ExitMask bit d set means a step in direction d (see dirX/dirZ) is legal.
	ExitMask(x, z, level int) byte
	// WallMask bits 0-3 mean a wall on the north, east, south or west face.
	WallMask(x, z, level int) byte
}

// dirX/dirZ are PathFinder.ts's DX/DZ, the order the pack's exit bits were baked in.
var dirX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
var dirZ = [8]int{1, 0, -1, 0, 1, -1, -1, 1}

// cardinalSides is PathFinder.ts's CARDINAL_SIDES: an orthogonal offset and the wall bit that
// must be clear on the candidate tile for it to face the loc.
var cardinalSides = [4]struct {
	dx, dz int
	bit    byte
}{{0, 1, 1 << 2}, {1, 0, 1 << 3}, {0, -1, 1 << 0}, {-1, 0, 1 << 1}}

const (
	// DefaultMaxExpansions is PathFinder.ts's MAX_EXPANSIONS.
	DefaultMaxExpansions = 1200000
	footprintRadius      = 2
)

func nodeID(x, z, level int) int64 { return int64(level)<<28 | int64(x)<<14 | int64(z) }
func nodePoint(id int64) Point {
	return Point{X: int((id >> 14) & 0x3fff), Z: int(id & 0x3fff), Level: int((id >> 28) & 0x3)}
}

// Planner searches a collision pack plus a transport graph. It is safe for concurrent use.
type Planner struct {
	col   Collision
	edges map[int64][]*Edge
	long  []*Edge
	// Edges counts the graph edges kept (both endpoints walkable); Dropped the rest.
	Edges, Dropped int
}

// NewPlanner compiles g over col, dropping edges whose ends are not walkable, as addEdges
// does. g may be nil for walking only. The Planner keeps pointers into g.Edges.
func NewPlanner(col Collision, g *Graph) *Planner {
	p := &Planner{col: col, edges: map[int64][]*Edge{}}
	if g == nil {
		return p
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if !col.Walkable(e.From.X, e.From.Z, e.From.Level) || !col.Walkable(e.To.X, e.To.Z, e.To.Level) {
			p.Dropped++
			continue
		}
		id := nodeID(e.From.X, e.From.Z, e.From.Level)
		p.edges[id] = append(p.edges[id], e)
		if e.LongRange() {
			p.long = append(p.long, e)
		}
		p.Edges++
	}
	return p
}

// EdgesFrom returns the graph edges leaving a tile.
func (p *Planner) EdgesFrom(pt Point) []*Edge { return p.edges[nodeID(pt.X, pt.Z, pt.Level)] }

// Options tune one search. The zero value walks with doors, stairs and loc transports, but
// no special crossings and no NPC-operated hops, which a Walker needs a Handler for.
type Options struct {
	// MaxExpansions caps the search; 0 means DefaultMaxExpansions.
	MaxExpansions int
	// State gates edges by their Requires; nil lets every gated edge through.
	State *WorldState
	// Avoid excludes every edge whose loc placement (LocX, LocZ, From.Level) is listed.
	Avoid map[Point]bool
	// AvoidKinds excludes edge kinds (ships, shortcuts...).
	AvoidKinds map[Kind]bool
	// AllowSpecial lets special crossings (tolls, fares, dialogue gates) into the search.
	AllowSpecial bool
	// AllowNPC lets hops with no loc (ship NPCs, spirit trees, carts) into the search.
	AllowNPC bool
	// WalkOnly ignores the transport graph.
	WalkOnly bool
	// AvoidZones are no-go rectangles (see ResolveDangerZones). The search never steps or hops
	// into one from outside; from a tile inside one it may move freely until it is out, so a
	// player standing in a zone can still leave it (PathFinder.ts's outside-in rule).
	AvoidZones []Rect
	// Allow, when set, is asked about every graph edge the other options let through; false
	// leaves the edge out. A Walker uses it to plan only hops its handlers can run.
	Allow func(e *Edge) bool
	// Walls are wall faces (WallNorth...) that live locs add to the pack, keyed by tile; a walk
	// step crossing one is left out (see WallClient).
	Walls map[Point]byte
	// Blocked are single steps, {from, to}, the search leaves out.
	Blocked map[[2]Point]bool
	// LiveEdges are hops that exist only live, searched beside the graph's own edges under
	// the same rules: the doors a player shut across Walls (see LiveDoorClient).
	LiveEdges []*Edge
	// StepCost, when set, is the extra cost of walking onto a tile, on top of the step's own 1: ground a monster holds
	// (scarabrunner's pen walks) costs more than open ground, so a route that goes round it wins unless going round costs
	// more than the extra. A soft avoid, where AvoidZones is a hard one that a start inside it escapes freely: the
	// Yanille ogres' safespots route to the post through the herd (527 steps) rather than by the east gate (523, a 1%
	// difference), which no rectangle could change. It must be cheap (it is asked of every neighbour of every node), never
	// negative, and 0 for most tiles. nil costs every step 1.
	StepCost func(x, z, level int) int

	// noHeuristic forces Dijkstra; tests compare it against the bounded heuristic.
	noHeuristic bool
}

// Step is one route waypoint. Hop is set on the waypoint a transport lands on, as
// PathFinder.ts's Waypoint.transport.
type Step struct {
	Point
	Hop *Edge
}

// Route is a planned route.
type Route struct {
	OK       bool
	Reason   string
	Cost     int
	Expanded int
	// Waypoints are corner-compressed, with every hop's stand and landing kept.
	Waypoints []Step
}

// End is the route's last tile.
func (r *Route) End() Point {
	if len(r.Waypoints) == 0 {
		return Point{}
	}
	return r.Waypoints[len(r.Waypoints)-1].Point
}

// Hops lists the route's transports in order.
func (r *Route) Hops() []*Edge {
	var out []*Edge
	for _, w := range r.Waypoints {
		if w.Hop != nil {
			out = append(out, w.Hop)
		}
	}
	return out
}

// Leg is one piece of a route: a walk over Tiles (corners, first is the start) on one level,
// or a single Hop.
type Leg struct {
	Kind  Kind
	Tiles []Point
	Hop   *Edge
}

// Legs splits the route into walks and hops.
func (r *Route) Legs() []Leg {
	var legs []Leg
	var walk []Point
	flush := func() {
		if len(walk) > 1 {
			legs = append(legs, Leg{Kind: KindWalk, Tiles: walk})
		}
		walk = nil
	}
	for i, w := range r.Waypoints {
		if w.Hop != nil && i > 0 {
			flush()
			legs = append(legs, Leg{Kind: w.Hop.Kind, Hop: w.Hop})
		}
		walk = append(walk, w.Point)
	}
	flush()
	return legs
}

// String renders the route for logs and cmd/webwalkplan.
func (r *Route) String() string {
	if !r.OK {
		return fmt.Sprintf("no route: %s (expanded %d)", r.Reason, r.Expanded)
	}
	s := fmt.Sprintf("cost %d, %d waypoints, expanded %d", r.Cost, len(r.Waypoints), r.Expanded)
	for _, leg := range r.Legs() {
		if leg.Hop != nil {
			s += "\n  " + leg.Hop.String()
			continue
		}
		s += fmt.Sprintf("\n  [walk] %s -> %s (%d corners)", leg.Tiles[0], leg.Tiles[len(leg.Tiles)-1], len(leg.Tiles))
	}
	return s
}

// Plan finds a route from one tile to another: PathFinder.ts's findPath. An unwalkable
// destination (a booth, a tree) is reached by standing beside it.
func (p *Planner) Plan(from, to Point, opt Options) Route {
	start, ok := p.snapWalkable(from, 2)
	if !ok {
		return Route{Reason: fmt.Sprintf("start (%s) not walkable", from)}
	}
	if opt.MaxExpansions <= 0 {
		opt.MaxExpansions = DefaultMaxExpansions
	}
	if cardinal := p.cardinalGoals(to); len(cardinal) > 0 {
		if r := p.search(start, to, cardinal, 1, &opt); r.OK {
			return r
		}
	}
	goals := p.goalCandidates(to, 5)
	if len(goals) == 0 {
		return Route{Reason: fmt.Sprintf("target (%s) not walkable within 5 tiles", to)}
	}
	slack := 5
	if len(goals) == 1 && goals[nodeID(to.X, to.Z, to.Level)] {
		slack = 0
	}
	return p.search(start, to, goals, slack, &opt)
}

// allowed reports whether the options let an edge into the search.
func (o *Options) allowed(e *Edge) bool {
	if o.WalkOnly {
		return false
	}
	if e.Special != nil && !o.AllowSpecial {
		return false
	}
	if !e.HasLoc() && !o.AllowNPC {
		return false
	}
	if o.AvoidKinds[e.Kind] {
		return false
	}
	if len(o.Avoid) > 0 && o.Avoid[Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}] {
		return false
	}
	if e.Requires != nil && o.State != nil {
		if ok, _ := e.Requires.Check(o.State); !ok {
			return false
		}
	}
	if o.Allow != nil && !o.Allow(e) {
		return false
	}
	return true
}

func (p *Planner) usable(x, z, level int) bool {
	return p.col.ExitMask(x, z, level) != 0 || len(p.edges[nodeID(x, z, level)]) > 0
}

// snapWalkable finds the nearest walkable tile with an exit or an edge within radius, or a
// walkable dead end when nothing better is near.
func (p *Planner) snapWalkable(pt Point, radius int) (Point, bool) {
	var stranded *Point
	check := func(x, z int) bool {
		if !p.col.Walkable(x, z, pt.Level) {
			return false
		}
		if p.usable(x, z, pt.Level) {
			return true
		}
		if stranded == nil {
			stranded = &Point{X: x, Z: z, Level: pt.Level}
		}
		return false
	}
	if check(pt.X, pt.Z) {
		return pt, true
	}
	for r := 1; r <= radius; r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if abs(dx) != r && abs(dz) != r {
					continue
				}
				if check(pt.X+dx, pt.Z+dz) {
					return Point{X: pt.X + dx, Z: pt.Z + dz, Level: pt.Level}, true
				}
			}
		}
	}
	if stranded != nil {
		return *stranded, true
	}
	return Point{}, false
}

func (p *Planner) blockedFootprint(pt Point) []Point {
	seen := map[[2]int]bool{{pt.X, pt.Z}: true}
	solid := []Point{pt}
	stack := []Point{pt}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, side := range cardinalSides {
			nx, nz := cur.X+side.dx, cur.Z+side.dz
			if Chebyshev(Point{X: nx, Z: nz}, Point{X: pt.X, Z: pt.Z}) > footprintRadius {
				continue
			}
			key := [2]int{nx, nz}
			if seen[key] || p.col.Walkable(nx, nz, pt.Level) {
				continue
			}
			seen[key] = true
			t := Point{X: nx, Z: nz, Level: pt.Level}
			solid = append(solid, t)
			stack = append(stack, t)
		}
	}
	return solid
}

func (p *Planner) besideAll(tiles []Point, at Point) map[int64]bool {
	goals := map[int64]bool{}
	for _, t := range tiles {
		for _, side := range cardinalSides {
			cx, cz := t.X+side.dx, t.Z+side.dz
			if Chebyshev(Point{X: cx, Z: cz}, Point{X: at.X, Z: at.Z}) > footprintRadius {
				continue
			}
			if p.col.Walkable(cx, cz, at.Level) && p.col.WallMask(cx, cz, at.Level)&side.bit == 0 {
				goals[nodeID(cx, cz, at.Level)] = true
			}
		}
	}
	return goals
}

func (p *Planner) cardinalGoals(pt Point) map[int64]bool {
	if p.col.Walkable(pt.X, pt.Z, pt.Level) {
		return nil
	}
	if beside := p.besideAll([]Point{pt}, pt); len(beside) > 0 {
		return beside
	}
	return p.besideAll(p.blockedFootprint(pt), pt)
}

func (p *Planner) goalCandidates(pt Point, radius int) map[int64]bool {
	goals := map[int64]bool{}
	if p.col.Walkable(pt.X, pt.Z, pt.Level) {
		goals[nodeID(pt.X, pt.Z, pt.Level)] = true
		return goals
	}
	seed := p.cardinalGoals(pt)
	queue := make([]Point, 0, len(seed))
	seen := make(map[int64]bool, len(seed))
	for id := range seed {
		queue = append(queue, nodePoint(id))
		seen[id] = true
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		goals[nodeID(cur.X, cur.Z, cur.Level)] = true
		mask := p.col.ExitMask(cur.X, cur.Z, cur.Level)
		for d := 0; d < 8; d++ {
			if mask&(1<<uint(d)) == 0 {
				continue
			}
			nx, nz := cur.X+dirX[d], cur.Z+dirZ[d]
			if Chebyshev(Point{X: nx, Z: nz}, pt) > radius {
				continue
			}
			id := nodeID(nx, nz, cur.Level)
			if seen[id] || !p.col.Walkable(nx, nz, cur.Level) {
				continue
			}
			seen[id] = true
			queue = append(queue, Point{X: nx, Z: nz, Level: cur.Level})
		}
	}
	if len(goals) > 0 {
		return goals
	}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if p.col.Walkable(pt.X+dx, pt.Z+dz, pt.Level) {
				goals[nodeID(pt.X+dx, pt.Z+dz, pt.Level)] = true
			}
		}
	}
	return goals
}

// node is one search record.
type node struct {
	g      int32
	h      int32 // the heuristic, cached from the first time the search reaches the node
	seen   bool
	closed bool
	from   int64
	via    *Edge
}

type heap struct {
	keys []int64
	ids  []int64
}

func (h *heap) push(key, id int64) {
	h.keys = append(h.keys, key)
	h.ids = append(h.ids, id)
	for i := len(h.ids) - 1; i > 0; {
		parent := (i - 1) / 2
		if h.keys[parent] <= h.keys[i] {
			break
		}
		h.keys[parent], h.keys[i] = h.keys[i], h.keys[parent]
		h.ids[parent], h.ids[i] = h.ids[i], h.ids[parent]
		i = parent
	}
}

func (h *heap) pop() int64 {
	top := h.ids[0]
	n := len(h.ids) - 1
	h.keys[0], h.ids[0] = h.keys[n], h.ids[n]
	h.keys, h.ids = h.keys[:n], h.ids[:n]
	for i := 0; ; {
		l, r, s := 2*i+1, 2*i+2, i
		if l < n && h.keys[l] < h.keys[s] {
			s = l
		}
		if r < n && h.keys[r] < h.keys[s] {
			s = r
		}
		if s == i {
			break
		}
		h.keys[s], h.keys[i] = h.keys[i], h.keys[s]
		h.ids[s], h.ids[i] = h.ids[i], h.ids[s]
		i = s
	}
	return top
}

func (p *Planner) search(from, to Point, goals map[int64]bool, slack int, opt *Options) Route {
	startID := nodeID(from.X, from.Z, from.Level)
	chebAt := func(x, z int) int {
		d := Chebyshev(Point{X: x, Z: z}, Point{X: to.X, Z: to.Z}) - slack
		if d < 0 {
			return 0
		}
		return d
	}
	h := p.heuristic(to, chebAt, opt)
	var live map[int64][]*Edge
	if !opt.WalkOnly && len(opt.LiveEdges) > 0 {
		live = make(map[int64][]*Edge, len(opt.LiveEdges))
		for _, e := range opt.LiveEdges {
			id := nodeID(e.From.X, e.From.Z, e.From.Level)
			live[id] = append(live[id], e)
		}
	}

	nodes := newNodeStore()
	defer nodes.release()
	*nodes.at(startID) = node{seen: true, from: -1}
	walls := wallBoxOf(opt.Walls)
	open := &heap{}
	open.push(int64(h(from.X, from.Z))<<20, startID)
	expanded := 0
	var zones *ZoneIndex
	if len(opt.AvoidZones) > 0 {
		zones = zoneIndexFor(opt.AvoidZones)
	}
	var cur int64
	var rec node
	relax := func(id int64, cost int, via *Edge) {
		n := nodes.at(id)
		g := rec.g + int32(cost)
		if n.seen && (n.closed || n.g <= g) {
			return
		}
		hv := n.h
		if !n.seen {
			np := nodePoint(id)
			hv = int32(h(np.X, np.Z))
		}
		*n = node{g: g, h: hv, seen: true, from: cur, via: via}
		open.push(int64(g+hv)<<20-int64(g), id)
	}
	for len(open.ids) > 0 {
		cur = open.pop()
		recp := nodes.at(cur)
		if recp.closed {
			continue
		}
		recp.closed = true
		rec = *recp
		if goals[cur] {
			return reconstruct(startID, cur, int(rec.g), expanded, nodes)
		}
		expanded++
		if expanded > opt.MaxExpansions {
			return Route{Reason: fmt.Sprintf("expansion budget exceeded (%d)", opt.MaxExpansions), Expanded: expanded}
		}
		pt := nodePoint(cur)
		zoned := len(opt.AvoidZones) > 0
		escaping := zoned && zones.Contains(pt)
		mask := p.col.ExitMask(pt.X, pt.Z, pt.Level)
		for d := 0; d < 8; d++ {
			if mask&(1<<uint(d)) == 0 {
				continue
			}
			next := Point{X: pt.X + dirX[d], Z: pt.Z + dirZ[d], Level: pt.Level}
			if zoned && !escaping && zones.Contains(next) {
				continue
			}
			if walls.near(pt) && StepWalled(opt.Walls, pt, dirX[d], dirZ[d]) {
				continue
			}
			if len(opt.Blocked) > 0 && opt.Blocked[[2]Point{pt, next}] {
				continue
			}
			step := 1
			if opt.StepCost != nil {
				step += opt.StepCost(next.X, next.Z, next.Level)
			}
			relax(nodeID(next.X, next.Z, next.Level), step, nil)
		}
		if opt.WalkOnly {
			continue
		}
		// A staircase with two stand tiles on one floor is baked once per combination, so up
		// then straight back down would teleport through the wall between them; the server
		// lands you on one tile only, so a stair hop back to the level it came from is refused.
		cameFromLevel := -1
		if rec.via != nil && rec.via.Kind == KindStair && rec.from >= 0 {
			cameFromLevel = nodePoint(rec.from).Level
		}
		for _, e := range p.edges[cur] {
			if cameFromLevel >= 0 && e.Kind == KindStair && e.To.Level == cameFromLevel {
				continue
			}
			if !opt.allowed(e) {
				continue
			}
			if zoned && !escaping && zones.Contains(e.To) {
				continue
			}
			relax(nodeID(e.To.X, e.To.Z, e.To.Level), e.Cost, e)
		}
		for _, e := range live[cur] {
			if !opt.allowed(e) || !p.col.Walkable(e.To.X, e.To.Z, e.To.Level) {
				continue
			}
			if zoned && !escaping && zones.Contains(e.To) {
				continue
			}
			relax(nodeID(e.To.X, e.To.Z, e.To.Level), e.Cost, e)
		}
	}
	return Route{Reason: "unreachable", Expanded: expanded}
}

// portal is a long edge as the heuristic sees it: its entrance and the least any route through
// it can still cost from that entrance.
type portal struct {
	x, z int
	cost int
}

// heuristic returns the A* bound for one search. A route either uses no long edge, so it costs
// at least the Chebyshev distance to the goal, or its first long edge e, which it must walk to
// (at least Chebyshev(n, e.From)), pay e.Cost for, and then finish from e.To, which costs at
// least min(Chebyshev(e.To, goal), floor), floor being the package doc's bound over every long
// edge. Each term is 1-Lipschitz in Chebyshev distance and a long edge's own term is its cost
// plus that tail, so the bound stays admissible and consistent. The old bound was the floor
// alone, which a dungeon ladder landing near the goal pinned to a few tiles everywhere, so most
// searches ran as Dijkstra.
func (p *Planner) heuristic(to Point, chebAt func(x, z int) int, opt *Options) func(x, z int) int {
	if opt.noHeuristic {
		return func(int, int) int { return 0 }
	}
	floor := math.MaxInt32
	var ports []portal
	if !opt.WalkOnly {
		for _, e := range p.long {
			if !opt.allowed(e) {
				continue
			}
			if c := e.Cost + chebAt(e.To.X, e.To.Z); c < floor {
				floor = c
			}
		}
		for _, e := range p.long {
			if !opt.allowed(e) {
				continue
			}
			ports = append(ports, portal{x: e.From.X, z: e.From.Z, cost: e.Cost + min(chebAt(e.To.X, e.To.Z), floor)})
		}
	}
	if len(ports) == 0 {
		return chebAt
	}
	hb := &portalBound{ports: ports, blocks: map[int64][]portal{}, lastKey: -1}
	return func(x, z int) int {
		best := chebAt(x, z)
		for _, pt := range hb.candidates(x, z) {
			if v := chebTo(x, z, pt.x, pt.z) + pt.cost; v < best {
				best = v
			}
		}
		return best
	}
}

// portalBound finds, per 8x8 block of tiles, the few portals that can be the cheapest from
// some tile in it, so a lookup scans those rather than every long edge.
type portalBound struct {
	ports   []portal
	blocks  map[int64][]portal
	lastKey int64
	last    []portal
}

const boundBlock = 8

func (b *portalBound) candidates(x, z int) []portal {
	bx, bz := x/boundBlock, z/boundBlock
	key := int64(bx)<<32 | int64(bz)
	if key == b.lastKey {
		return b.last
	}
	c, ok := b.blocks[key]
	if !ok {
		x0, z0 := bx*boundBlock, bz*boundBlock
		x1, z1 := x0+boundBlock-1, z0+boundBlock-1
		// A portal is a candidate when its least term over the block is no more than the
		// smallest most-term any portal has over it.
		upper := math.MaxInt32
		for _, pt := range b.ports {
			if v := rectFar(x0, z0, x1, z1, pt.x, pt.z) + pt.cost; v < upper {
				upper = v
			}
		}
		for _, pt := range b.ports {
			if rectNear(x0, z0, x1, z1, pt.x, pt.z)+pt.cost <= upper {
				c = append(c, pt)
			}
		}
		b.blocks[key] = c
	}
	b.lastKey, b.last = key, c
	return c
}

func chebTo(x, z, px, pz int) int { return max(abs(x-px), abs(z-pz)) }

// rectNear is the Chebyshev distance from (px, pz) to the nearest tile of the rectangle.
func rectNear(x0, z0, x1, z1, px, pz int) int {
	dx := max(x0-px, 0, px-x1)
	dz := max(z0-pz, 0, pz-z1)
	return max(dx, dz)
}

// rectFar is the Chebyshev distance from (px, pz) to the farthest tile of the rectangle.
func rectFar(x0, z0, x1, z1, px, pz int) int {
	return max(abs(px-x0), abs(px-x1), abs(pz-z0), abs(pz-z1))
}

func reconstruct(start, goal int64, cost, expanded int, nodes *nodeStore) Route {
	var chain []int64
	for id := goal; ; {
		chain = append(chain, id)
		if id == start {
			break
		}
		id = nodes.get(id).from
		if id < 0 {
			return Route{Reason: "reconstruction broke (bug)", Expanded: expanded}
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	sign := func(n int) int {
		if n > 0 {
			return 1
		} else if n < 0 {
			return -1
		}
		return 0
	}
	dir := func(a, b int64) [2]int {
		pa, pb := nodePoint(a), nodePoint(b)
		return [2]int{sign(pb.X - pa.X), sign(pb.Z - pa.Z)}
	}
	out := []Step{{Point: nodePoint(chain[0])}}
	for i := 1; i < len(chain); i++ {
		via := nodes.get(chain[i]).via
		var viaNext *Edge
		if i+1 < len(chain) {
			viaNext = nodes.get(chain[i+1]).via
		}
		last := i == len(chain)-1
		turn := !last && via == nil && viaNext == nil && dir(chain[i-1], chain[i]) != dir(chain[i], chain[i+1])
		if via != nil || viaNext != nil || turn || last {
			out = append(out, Step{Point: nodePoint(chain[i]), Hop: via})
		}
	}
	return Route{OK: true, Cost: cost, Expanded: expanded, Waypoints: out}
}
