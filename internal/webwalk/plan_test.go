package webwalk

import (
	"math/rand"
	"strings"
	"testing"
)

// twoRooms is two 5x5 rooms on level 0 with a wall between x=4 and x=5, a door at (4,2)
// stepping east, and a ladder up from (1,1) to a level-1 room.
func twoRooms() (*grid, *Graph) {
	g := newGrid().fill(0, 0, 4, 4, 0).fill(5, 0, 9, 4, 0).fill(0, 0, 2, 2, 1)
	for z := 0; z <= 4; z++ {
		g.wall(Point{X: 4, Z: z}, Point{X: 5, Z: z})
	}
	door := Point{X: 4, Z: 2}
	east := Point{X: 5, Z: 2}
	gr := &Graph{Edges: []Edge{
		{From: door, To: east, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 4, LocZ: 2},
		{From: east, To: door, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 4, LocZ: 2},
		{From: Point{X: 1, Z: 1}, To: Point{X: 1, Z: 1, Level: 1}, Kind: KindStair, Cost: 6, LocName: "Ladder", Action: "Climb-up", LocID: 1747, OpenLocID: -1, LocX: 0, LocZ: 1},
		{From: Point{X: 1, Z: 1, Level: 1}, To: Point{X: 1, Z: 1}, Kind: KindStair, Cost: 6, LocName: "Ladder", Action: "Climb-down", LocID: 1746, OpenLocID: -1, LocX: 0, LocZ: 1},
	}}
	return g, gr
}

func TestPlanCrossesADoor(t *testing.T) {
	col, gr := twoRooms()
	p := NewPlanner(col, gr)
	from, to := Point{X: 0, Z: 2}, Point{X: 9, Z: 2}

	if r := NewPlanner(col, nil).Plan(from, to, Options{}); r.OK {
		t.Fatalf("walk-only plan crossed the wall: %s", r.String())
	}
	r := p.Plan(from, to, Options{})
	if !r.OK {
		t.Fatalf("plan failed: %s", r.Reason)
	}
	// 4 steps to the door tile, the door (4), 4 steps on.
	if r.Cost != 12 {
		t.Errorf("cost = %d, want 12\n%s", r.Cost, r.String())
	}
	hops := r.Hops()
	if len(hops) != 1 || hops[0].Kind != KindDoor || hops[0].From != (Point{X: 4, Z: 2}) {
		t.Fatalf("hops = %v", hops)
	}
	legs := r.Legs()
	if len(legs) != 3 || legs[0].Kind != KindWalk || legs[1].Hop == nil || legs[2].Kind != KindWalk {
		t.Fatalf("legs = %+v", legs)
	}
	if last := legs[0].Tiles[len(legs[0].Tiles)-1]; last != hops[0].From {
		t.Errorf("walk leg ends at %s, not the hop's stand %s", last, hops[0].From)
	}
	if first := legs[2].Tiles[0]; first != hops[0].To {
		t.Errorf("walk leg after the hop starts at %s, not its landing %s", first, hops[0].To)
	}
}

func TestPlanClimbsToAnotherLevel(t *testing.T) {
	col, gr := twoRooms()
	r := NewPlanner(col, gr).Plan(Point{X: 3, Z: 3}, Point{X: 2, Z: 2, Level: 1}, Options{})
	if !r.OK {
		t.Fatalf("plan failed: %s", r.Reason)
	}
	hops := r.Hops()
	if len(hops) != 1 || hops[0].Action != "Climb-up" {
		t.Fatalf("hops = %v", hops)
	}
}

func TestAvoidAndKindsExcludeEdges(t *testing.T) {
	col, gr := twoRooms()
	p := NewPlanner(col, gr)
	from, to := Point{X: 0, Z: 2}, Point{X: 9, Z: 2}
	if r := p.Plan(from, to, Options{Avoid: map[Point]bool{{X: 4, Z: 2}: true}}); r.OK {
		t.Errorf("avoided door still used: %s", r.String())
	}
	if r := p.Plan(from, to, Options{AvoidKinds: map[Kind]bool{KindDoor: true}}); r.OK {
		t.Errorf("avoided kind still used: %s", r.String())
	}
}

func TestSpecialNPCAndRequiresGates(t *testing.T) {
	col, gr := twoRooms()
	gr.Edges[0].Special = &Special{Label: "toll"}
	gr.Edges[0].Requires = &Requires{Quests: []QuestReq{{Quest: "Q", MinStatus: QuestComplete}}}
	p := NewPlanner(col, gr)
	from, to := Point{X: 0, Z: 2}, Point{X: 9, Z: 2}
	if r := p.Plan(from, to, Options{}); r.OK {
		t.Errorf("special edge used without AllowSpecial")
	}
	if r := p.Plan(from, to, Options{AllowSpecial: true}); !r.OK {
		t.Errorf("AllowSpecial with no state should fail open: %s", r.Reason)
	}
	st := &WorldState{QuestStatus: func(string) QuestProgress { return QuestStarted }}
	if r := p.Plan(from, to, Options{AllowSpecial: true, State: st}); r.OK {
		t.Errorf("quest gate ignored")
	}
	st.QuestStatus = func(string) QuestProgress { return QuestComplete }
	if r := p.Plan(from, to, Options{AllowSpecial: true, State: st}); !r.OK {
		t.Errorf("quest complete still gated: %s", r.Reason)
	}

	gr2 := &Graph{Edges: append([]Edge(nil), gr.Edges...)}
	gr2.Edges[0].Special, gr2.Edges[0].Requires, gr2.Edges[0].LocID = nil, nil, -1
	p2 := NewPlanner(col, gr2)
	if r := p2.Plan(from, to, Options{}); r.OK {
		t.Errorf("NPC edge used without AllowNPC")
	}
	if r := p2.Plan(from, to, Options{AllowNPC: true}); !r.OK {
		t.Errorf("AllowNPC: %s", r.Reason)
	}
}

func TestStairBounceIsRefused(t *testing.T) {
	// Two ground rooms split by a wall, each with a stand tile for one staircase whose top
	// is a single tile: going up from the west and straight down into the east room would be
	// a teleport through the wall.
	col := newGrid().fill(0, 0, 2, 2, 0).fill(3, 0, 5, 2, 0).fill(10, 10, 10, 10, 1)
	for z := 0; z <= 2; z++ {
		col.wall(Point{X: 2, Z: z}, Point{X: 3, Z: z})
	}
	top := Point{X: 10, Z: 10, Level: 1}
	west, east := Point{X: 2, Z: 1}, Point{X: 3, Z: 1}
	gr := &Graph{Edges: []Edge{
		{From: west, To: top, Kind: KindStair, Cost: 6, LocID: 1, OpenLocID: -1},
		{From: east, To: top, Kind: KindStair, Cost: 6, LocID: 1, OpenLocID: -1},
		{From: top, To: west, Kind: KindStair, Cost: 6, LocID: 2, OpenLocID: -1},
		{From: top, To: east, Kind: KindStair, Cost: 6, LocID: 2, OpenLocID: -1},
	}}
	if r := NewPlanner(col, gr).Plan(Point{X: 0, Z: 1}, Point{X: 5, Z: 1}, Options{}); r.OK {
		t.Fatalf("planned a stair bounce through the wall: %s", r.String())
	}
}

func TestBudgetIsReported(t *testing.T) {
	col := newGrid().fill(0, 0, 60, 60, 0)
	r := NewPlanner(col, nil).Plan(Point{X: 0, Z: 0}, Point{X: 60, Z: 60}, Options{MaxExpansions: 10})
	if r.OK || !strings.Contains(r.Reason, "budget") {
		t.Fatalf("got %+v, want a budget failure", r)
	}
}

func TestUnwalkableDestinationStandsBeside(t *testing.T) {
	col := newGrid().fill(0, 0, 6, 6, 0)
	delete(col.walk, Point{X: 3, Z: 3})
	r := NewPlanner(col, nil).Plan(Point{X: 0, Z: 0}, Point{X: 3, Z: 3}, Options{})
	if !r.OK {
		t.Fatalf("plan failed: %s", r.Reason)
	}
	end := r.End()
	if Chebyshev(end, Point{X: 3, Z: 3}) != 1 || (end.X != 3 && end.Z != 3) {
		t.Errorf("ended at %s, want an orthogonal neighbour of 3,3", end)
	}
}

// TestHeuristicMatchesDijkstra checks the bounded heuristic against plain Dijkstra on random
// grids with random long-range hops, which is what makes it safe to use instead of the TS
// planner's Dijkstra fallback.
func TestHeuristicMatchesDijkstra(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 40; trial++ {
		col := newGrid().fill(0, 0, 39, 39, 0)
		for i := 0; i < 500; i++ {
			delete(col.walk, Point{X: rng.Intn(40), Z: rng.Intn(40)})
		}
		var gr Graph
		for i := 0; i < 6; i++ {
			a := Point{X: rng.Intn(40), Z: rng.Intn(40)}
			b := Point{X: rng.Intn(40), Z: rng.Intn(40)}
			col.walk[a], col.walk[b] = true, true
			kinds := []Kind{KindShip, KindDungeon, KindDoor}
			k := kinds[rng.Intn(len(kinds))]
			gr.Edges = append(gr.Edges, Edge{From: a, To: b, Kind: k, Cost: 1 + rng.Intn(12), LocID: i, OpenLocID: -1})
		}
		p := NewPlanner(col, &gr)
		for od := 0; od < 10; od++ {
			from := Point{X: rng.Intn(40), Z: rng.Intn(40)}
			to := Point{X: rng.Intn(40), Z: rng.Intn(40)}
			col.walk[from], col.walk[to] = true, true
			a := p.Plan(from, to, Options{})
			b := p.Plan(from, to, Options{noHeuristic: true})
			if a.OK != b.OK || a.Cost != b.Cost {
				t.Fatalf("trial %d %s->%s: heuristic %v/%d, dijkstra %v/%d", trial, from, to, a.OK, a.Cost, b.OK, b.Cost)
			}
			if a.OK && a.Expanded > b.Expanded {
				t.Errorf("trial %d: heuristic expanded %d > dijkstra %d", trial, a.Expanded, b.Expanded)
			}
		}
	}
}
