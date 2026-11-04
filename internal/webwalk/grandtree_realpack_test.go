package webwalk

import (
	"testing"
)

// liveBarState is what the Barcrawl wave's accounts planned with on 16 Oct 2026: members, Waterfall done (the route from
// the Barbarian Outpost goes down the falls), The Grand Tree not (so the spirit trees and the gnome glider, which the
// stronghold's other ways in need, are out of every plan).
func liveBarState() *WorldState {
	return &WorldState{
		Members: true, Skills: map[string]int{"agility": 25, "thieving": 70}, FreeSlots: 20,
		QuestStatus: func(q string) QuestProgress {
			if q == "Waterfall Quest" {
				return QuestComplete
			}
			return QuestNotStarted
		},
		ItemCount: func(string) int { return 2000 }, WornCount: func(string) int { return 0 },
	}
}

func hopsByLoc(r Route) map[int]bool {
	out := map[int]bool{}
	for _, h := range r.Hops() {
		out[h.LocID] = true
	}
	return out
}

// Blurberry's bar (2480,3490,1, upstairs in the Grand Tree) was "unreachable" for four Barcrawl accounts on 16 Oct 2026
// (parked for twenty minutes each), and a walk of every other account crossed the Grand Tree's left door in a line of
// minutes. The planner was never the fault: every start the accounts walked from plans to the bar. What it lacked was a
// second Grand Tree door, so that "routing around the refused door leaves the bar unreachable" was true and the refusal
// stayed in the plan. This pins the starts, the route, and the answer to that question.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestEveryStartThatWalkedToBlurberrysBarPlansThereAndPastABusyDoor(t *testing.T) {
	p := realPack(t)
	bar := Point{X: 2480, Z: 3490, Level: 1}
	state := liveBarState()
	leftKey := Point{X: 2464, Z: 3492, Level: 0}
	rightKey := Point{X: 2466, Z: 3492, Level: 0}
	gateKey := Point{X: 2459, Z: 3383, Level: 0}

	// Every start the 16 Oct logs plan from: the Barbarian Outpost where the crawl begins (2542-2544,3566-3572), the
	// stronghold's south gate (2458-2462,3380-3382), inside it (2461,3385), and both sides of the Grand Tree doors.
	starts := []Point{
		{X: 2542, Z: 3567}, {X: 2542, Z: 3569}, {X: 2542, Z: 3571}, {X: 2543, Z: 3572}, {X: 2544, Z: 3566},
		{X: 2458, Z: 3382}, {X: 2460, Z: 3382}, {X: 2461, Z: 3380}, {X: 2461, Z: 3382}, {X: 2462, Z: 3381}, {X: 2461, Z: 3385},
		{X: 2465, Z: 3491}, {X: 2465, Z: 3493}, {X: 2466, Z: 3491}, {X: 2466, Z: 3493}, {X: 2466, Z: 3496, Level: 0},
	}
	plan := func(from Point, avoid ...Point) Route {
		opt := Options{AllowNPC: true, AllowSpecial: true, State: state}
		if len(avoid) > 0 {
			opt.Avoid = map[Point]bool{}
			for _, a := range avoid {
				opt.Avoid[a] = true
			}
		}
		return p.Plan(from, bar, opt)
	}

	for _, from := range starts {
		base := plan(from)
		if !base.OK {
			t.Errorf("no route from %s to the bar: %s", from, base.Reason)
			continue
		}
		through := hopsByLoc(base)
		if from.Z < 3383 && !through[190] {
			t.Errorf("the route from %s does not cross the stronghold's gate: %s", from, base.String())
		}

		// The left door refused: the walker's rule asks whether going round it still reaches the bar.
		if through[1967] || through[1968] {
			round := plan(from, leftKey)
			if !round.OK {
				t.Errorf("from %s the bar is unreachable with the Grand Tree's left door out of the plan: %s", from, round.Reason)
				continue
			}
			if round.Cost > base.Cost+40 {
				t.Errorf("from %s going round the left door costs %d against %d", from, round.Cost, base.Cost)
			}
			if !hopsByLoc(round)[1968] {
				t.Errorf("from %s the way round the left door is not the right door: %s", from, round.String())
			}
			// And the other way round.
			if back := plan(from, rightKey); !back.OK {
				t.Errorf("from %s the bar is unreachable with the right door out of the plan: %s", from, back.Reason)
			}
		}
	}

	// Controls. Both doors out: the bar is unreachable for an account that cannot use the glider or the spirit trees, so
	// the checks above are not satisfied by a state that lets everything through.
	if r := plan(Point{X: 2461, Z: 3385}, leftKey, rightKey); r.OK {
		t.Errorf("with both Grand Tree doors out the bar still plans: %s (the world state lets the spirit trees or the glider through)", r.String())
	}
	// The stronghold's south gate has no twin and no way round: it is the one queue with nothing to balance against.
	if r := plan(Point{X: 2542, Z: 3569}, gateKey); r.OK {
		t.Errorf("with the south gate out the bar still plans from the outpost: %s", r.String())
	}
}

// Without the right door in the graph the answer to "does going round the left door still reach the bar" was no: the
// walker's refusal rule kept the refused door in the plan and the refusals added up (16 Oct 2026). Rebuilding the planner
// from the graph minus the right door shows the fault this change fixes.
func TestWithoutTheRightDoorTheLeftDoorIsTheOnlyWayInToTheBar(t *testing.T) {
	p := realPack(t)
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	var without []Edge
	for _, e := range g.Edges {
		if e.LocID != 1968 {
			without = append(without, e)
		}
	}
	if len(without) != len(g.Edges)-2 {
		t.Fatalf("the default graph has %d edges on the right door, want 2", len(g.Edges)-len(without))
	}
	old := NewPlanner(p.col, &Graph{Edges: without})
	opt := Options{AllowNPC: true, AllowSpecial: true, State: liveBarState(), Avoid: map[Point]bool{{X: 2464, Z: 3492, Level: 0}: true}}
	bar := Point{X: 2480, Z: 3490, Level: 1}
	if r := old.Plan(Point{X: 2461, Z: 3385}, bar, opt); r.OK {
		t.Fatalf("without the right door the bar plans round the left one: %s", r.String())
	}
	if r := p.Plan(Point{X: 2461, Z: 3385}, bar, opt); !r.OK {
		t.Fatalf("with the right door the bar does not: %s", r.Reason)
	}
}

// The right door's edges are the left door's mirrored, and they survived the pack's walkable filter.
func TestTheGrandTreeRightDoorEdgesSurviveThePackAndMirrorTheLeftDoor(t *testing.T) {
	p := realPack(t)
	for _, x := range grandTreeDoorEdges {
		found := false
		for _, e := range p.EdgesFrom(x.Edge.From) {
			found = found || (e.LocID == x.Edge.LocID && e.To == x.Edge.To && e.Action == x.Edge.Action)
		}
		if !found {
			t.Errorf("edge %s was dropped by the planner: an end of it is not walkable in the pack", x.Edge.String())
		}
	}
	// The left door's edges, from graph.json, are the template: a two-tile jump through a width-2 door, stand and landing
	// on the door's column (open_tree_door's start_x), the door placed one tile west of its column on the row between.
	var left []Edge
	g, _ := Default()
	for _, e := range g.Edges {
		if e.LocID == 1967 {
			left = append(left, e)
		}
	}
	if len(left) != 2 {
		t.Fatalf("the graph has %d edges on the left door, want 2", len(left))
	}
	for _, r := range grandTreeDoorEdges {
		var match *Edge
		for i := range left {
			if left[i].From.Z == r.Edge.From.Z && left[i].To.Z == r.Edge.To.Z {
				match = &left[i]
			}
		}
		if match == nil {
			t.Errorf("no left-door edge runs the way %s does", r.Edge.String())
			continue
		}
		if r.Edge.From.X-match.From.X != 1 || r.Edge.To.X-match.To.X != 1 || r.Edge.LocX-match.LocX != 2 || r.Edge.LocZ != match.LocZ ||
			r.Edge.Kind != match.Kind || r.Edge.Cost != match.Cost || r.Edge.Action != match.Action || r.Edge.LocName != match.LocName {
			t.Errorf("%s is not the left door's %s shifted east: the right door stands one column east and is placed two tiles east", r.Edge.String(), match.String())
		}
	}
}
