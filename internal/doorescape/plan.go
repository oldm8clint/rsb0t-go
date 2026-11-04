package doorescape

import (
	"container/heap"
	"fmt"
)

// Costs, in tiles of walking. A door is a click, a wait for the leaf to change and, for a diagonal door, a push;
// a staircase is a click and a teleport. Closing a door is dear on purpose: a door that is open is somebody's
// way through (the engine keeps it open for 500 ticks and then shuts it itself), and an account that closes it
// makes the next one open it again. The planner closes a door only when nothing cheaper gets the account out.
const (
	OpenCost   = 4
	CloseCost  = 60
	StairsCost = 6
	// onLeafCost is added to opening a diagonal door from the very tile its leaf lands on: the account is
	// pushed off it, which works but wastes a tick.
	onLeafCost = 1
)

// StepKind says what one step of a plan is.
type StepKind int

const (
	// StepWalk walks Tiles, every one of them one legal step from the one before.
	StepWalk StepKind = iota
	// StepDoor operates a door's loc from Stand.
	StepDoor
	// StepStairs operates a staircase from Stand.
	StepStairs
)

func (k StepKind) String() string {
	switch k {
	case StepWalk:
		return "walk"
	case StepDoor:
		return "door"
	case StepStairs:
		return "stairs"
	}
	return "?"
}

// Step is one thing to do on the way out.
type Step struct {
	Kind StepKind
	// Tiles are the tiles of a walk, in order, not including the tile it starts from.
	Tiles []Tile
	// Door indexes Catalog.Doors (StepDoor) and Stair indexes Catalog.Stairs (StepStairs).
	Door, Stair int
	// Close is set when the door step closes the open leaf instead of opening the closed one.
	Close bool
	// Loc is the loc to operate and Op the option to send; Stand the tile to operate it from.
	Loc   Placement
	Op    int
	Stand Tile
	// After is where the account stands once the step has landed: the staircase's landing tile, or the tile a
	// diagonal door pushes it to; for a plain door the tile it was on.
	After Tile
}

// Plan is the cheapest way out found.
type Plan struct {
	OK     bool
	Reason string
	Steps  []Step
	Cost   int
	// Expanded is how many states the search settled, for the log and the CPU budget.
	Expanded int
}

// Ops is how many door and staircase steps the plan holds.
func (p Plan) Ops() int {
	n := 0
	for _, s := range p.Steps {
		if s.Kind != StepWalk {
			n++
		}
	}
	return n
}

// OpKey names one operation from one stand tile, for the set of those a caller has found not to work.
type OpKey struct {
	Kind  StepKind
	Index int
	Close bool
	Stand Tile
}

// Options tune a search.
type Options struct {
	// State is the doors as they are now; the zero value is every door open-leaf-less AND closed-leaf-less, so
	// callers pass Catalog.DefaultState or a State read off the client.
	State State
	// NoOps leaves every door and staircase out: a search for a plain walk out.
	NoOps bool
	// NoClose never closes a door.
	NoClose bool
	// AvoidStep leaves single steps out, {from, to}, which a caller saw the server refuse.
	AvoidStep map[[2]Tile]bool
	// AvoidOp leaves operations out, which a caller saw do nothing from that stand.
	AvoidOp map[OpKey]bool
	// MaxExpansions bounds the search; 0 means DefaultMaxExpansions.
	MaxExpansions int
}

// DefaultMaxExpansions is the most states one search settles before it gives up. The castle has a few hundred
// walkable tiles and a dozen doors, and a plan from its deepest room settles a few thousand states.
const DefaultMaxExpansions = 250000

type edgeKind uint8

const (
	edgeWalk edgeKind = iota
	edgeDoor
	edgeStairs
)

type stateKey uint64

func keyOf(slot int, s State) stateKey { return stateKey(uint64(slot)<<32 | uint64(s)) }
func (k stateKey) slot() int           { return int(uint64(k) >> 32) }
func (k stateKey) state() State        { return State(uint32(uint64(k))) }

type edgeRec struct {
	prev  stateKey
	kind  edgeKind
	index int
	close bool
	stand int // slot the operation was made from
	// after is the slot the account is on after the edge (the same as the target state's slot).
}

type item struct {
	cost int32
	key  stateKey
}

type minHeap []item

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	if h[i].cost != h[j].cost {
		return h[i].cost < h[j].cost
	}
	return h[i].key < h[j].key
}
func (h minHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)   { *h = append(*h, x.(item)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

// doorLocType is every door's loc config as far as reach goes: one tile, no force-approach.
var doorLocType = LocType{Width: 1, Length: 1}

// Plan finds the cheapest way from a tile to open ground outside the compound over the doors in o.State: walks
// that the engine's own step rules allow, door leaves opened (and, dearly, closed) from tiles the engine's own
// reach test accepts, and staircases down. The cost is Dijkstra's over (tile, doors), so the plan follows the
// real consequences of each door: an open leaf that lands in the way is a wall to every later step.
func (c *Catalog) Plan(from Tile, o Options) Plan {
	g := c.Grid
	start := g.Index(from.X, from.Z, from.Level)
	if start < 0 {
		return Plan{Reason: fmt.Sprintf("%s is outside the castle's collision grid", from)}
	}
	if c.Free(from.X, from.Z, from.Level) {
		return Plan{OK: true}
	}
	max := o.MaxExpansions
	if max <= 0 {
		max = DefaultMaxExpansions
	}

	var cur State
	flags := func(x, z, level int) uint32 {
		slot := g.Index(x, z, level)
		if slot < 0 {
			return blockAll
		}
		f := g.flags[slot]
		ds := c.delta[slot]
		if len(ds) == 0 {
			return f
		}
		for _, d := range ds {
			if !cur.ClosedLeaf(d.door) {
				f &^= d.closed
			}
		}
		for _, d := range ds {
			if cur.OpenLeaf(d.door) {
				f |= d.open
			}
		}
		return f
	}

	dist := map[stateKey]int32{}
	prev := map[stateKey]edgeRec{}
	h := &minHeap{}
	startKey := keyOf(start, o.State)
	dist[startKey] = 0
	heap.Push(h, item{0, startKey})
	expanded := 0
	var goal stateKey
	found := false

	relax := func(from stateKey, to stateKey, cost int32, rec edgeRec) {
		nd := dist[from] + cost
		if old, ok := dist[to]; ok && old <= nd {
			return
		}
		dist[to] = nd
		rec.prev = from
		prev[to] = rec
		heap.Push(h, item{nd, to})
	}

	for h.Len() > 0 {
		it := heap.Pop(h).(item)
		if d, ok := dist[it.key]; !ok || d < it.cost {
			continue
		}
		slot := it.key.slot()
		t := g.TileAt(slot)
		if c.Free(t.X, t.Z, t.Level) {
			goal, found = it.key, true
			break
		}
		expanded++
		if expanded > max {
			return Plan{Reason: "search budget spent", Expanded: expanded}
		}
		cur = it.key.state()

		for d := 0; d < 8; d++ {
			nx, nz := t.X+DX[d], t.Z+DZ[d]
			ns := g.Index(nx, nz, t.Level)
			if ns < 0 || !CanStep(flags, t.Level, t.X, t.Z, DX[d], DZ[d]) {
				continue
			}
			if o.AvoidStep != nil && o.AvoidStep[[2]Tile{t, {nx, nz, t.Level}}] {
				continue
			}
			relax(it.key, keyOf(ns, cur), 1, edgeRec{kind: edgeWalk})
		}
		if o.NoOps {
			continue
		}
		for i, door := range c.Doors {
			if door.Level != t.Level {
				continue
			}
			leaf := door.Open()
			if cur.ClosedLeaf(i) {
				closed := door.Closed()
				if Reached(flags, t.Level, t.X, t.Z, closed, doorLocType) && !o.AvoidOp[OpKey{StepDoor, i, false, t}] {
					ns := cur.With(i, false, true)
					land, cost := slot, int32(OpenCost)
					if push, ok := door.Push(); ok && t.X == leaf.X && t.Z == leaf.Z {
						land = g.Index(push.X, push.Z, push.Level)
						cost += onLeafCost
					}
					if land >= 0 {
						relax(it.key, keyOf(land, ns), cost, edgeRec{kind: edgeDoor, index: i, stand: slot})
					}
				}
			}
			if cur.OpenLeaf(i) && !o.NoClose {
				if Reached(flags, t.Level, t.X, t.Z, leaf, doorLocType) && !o.AvoidOp[OpKey{StepDoor, i, true, t}] {
					land, cost := slot, int32(CloseCost)
					if push, ok := door.PushClose(); ok && t.X == door.X && t.Z == door.Z {
						land = g.Index(push.X, push.Z, push.Level)
						cost += onLeafCost
					}
					if land >= 0 {
						relax(it.key, keyOf(land, cur.With(i, true, false)), cost, edgeRec{kind: edgeDoor, index: i, close: true, stand: slot})
					}
				}
			}
		}
		for i, st := range c.Stairs {
			if st.Up || st.Placement.Level != t.Level {
				continue
			}
			if !Reached(flags, t.Level, t.X, t.Z, st.Placement, st.Type) || o.AvoidOp[OpKey{StepStairs, i, false, t}] {
				continue
			}
			land := g.Index(st.Land.X, st.Land.Z, st.Land.Level)
			if land < 0 {
				continue
			}
			relax(it.key, keyOf(land, cur), StairsCost, edgeRec{kind: edgeStairs, index: i, stand: slot})
		}
	}
	if !found {
		return Plan{Reason: "no way out", Expanded: expanded}
	}

	// Walk the predecessors back, then coalesce the walks.
	var rev []stateKey
	for k := goal; k != startKey; k = prev[k].prev {
		rev = append(rev, k)
	}
	plan := Plan{OK: true, Cost: int(dist[goal]), Expanded: expanded}
	var walk *Step
	flush := func() {
		if walk != nil {
			plan.Steps = append(plan.Steps, *walk)
			walk = nil
		}
	}
	for i := len(rev) - 1; i >= 0; i-- {
		k := rev[i]
		rec := prev[k]
		to := g.TileAt(k.slot())
		switch rec.kind {
		case edgeWalk:
			if walk == nil {
				walk = &Step{Kind: StepWalk}
			}
			walk.Tiles = append(walk.Tiles, to)
		case edgeDoor:
			flush()
			d := c.Doors[rec.index]
			loc := d.Closed()
			if rec.close {
				loc = d.Open()
			}
			plan.Steps = append(plan.Steps, Step{Kind: StepDoor, Door: rec.index, Close: rec.close, Loc: loc, Op: d.Op, Stand: g.TileAt(rec.stand), After: to})
		case edgeStairs:
			flush()
			st := c.Stairs[rec.index]
			plan.Steps = append(plan.Steps, Step{Kind: StepStairs, Stair: rec.index, Loc: st.Placement, Op: st.Op, Stand: g.TileAt(rec.stand), After: to})
		}
	}
	flush()
	return plan
}
