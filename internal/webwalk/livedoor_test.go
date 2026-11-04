package webwalk

import (
	"strings"
	"testing"
)

func TestLiveDoorEdgesCrossTheWallAddWallFacesPuts(t *testing.T) {
	for angle, beyond := range map[int]Point{
		0: {X: 9, Z: 10}, 1: {X: 10, Z: 11}, 2: {X: 11, Z: 10}, 3: {X: 10, Z: 9},
	} {
		at := Point{X: 10, Z: 10}
		es := LiveDoorEdges(10, 10, 0, 0, angle, 1530, "Door", "Open")
		if len(es) != 2 {
			t.Fatalf("angle %d: %d edges", angle, len(es))
		}
		if es[0].From != at || es[0].To != beyond || es[1].From != beyond || es[1].To != at {
			t.Errorf("angle %d: edges %s / %s, want %s <-> %s", angle, es[0], es[1], at, beyond)
		}
		walls := map[Point]byte{}
		AddWallFaces(walls, 10, 10, 0, 0, angle)
		if !StepWalled(walls, at, beyond.X-at.X, beyond.Z-at.Z) {
			t.Errorf("angle %d: the edge does not cross the door's own wall", angle)
		}
		for _, e := range es {
			if e.Kind != KindDoor || e.Cost != DefaultCost(KindDoor) || e.LocID != 1530 || e.LocX != 10 || e.LocZ != 10 || e.Action != "Open" || !e.HasLoc() {
				t.Errorf("angle %d: edge %+v", angle, e)
			}
		}
	}
	if es := LiveDoorEdges(1, 1, 0, 0, 0, 1551, "Gate", "Open"); len(es) != 2 || es[0].Kind != KindGate {
		t.Errorf("a gate: %v", es)
	}
	if es := LiveDoorEdges(1, 1, 0, 2, 0, 1530, "Door", "Open"); es != nil {
		t.Errorf("an L-shaped wall got edges: %v", es)
	}
}

// shopStreet is Aubury's case in miniature: a street (z 0-3) and a shop (z 5-9) whose only
// way in is the doorway 22,4 -> 22,5. The map has the doorway open; live, a player shut it.
func shopStreet() (*grid, map[Point]byte, []*Edge) {
	col := newGrid().fill(0, 0, 30, 3, 0).fill(20, 5, 24, 9, 0).fill(22, 4, 22, 4, 0)
	walls := map[Point]byte{}
	AddWallFaces(walls, 22, 4, 0, 0, 1)
	return col, walls, LiveDoorEdges(22, 4, 0, 0, 1, 1530, "Door", "Open")
}

func TestPlannerHopsALiveShutDoorInsteadOfCallingTheShopUnreachable(t *testing.T) {
	col, walls, doors := shopStreet()
	p := NewPlanner(col, nil)
	from, to := Point{X: 0, Z: 0}, Point{X: 22, Z: 8}
	if r := p.Plan(from, to, Options{Walls: walls}); r.OK {
		t.Fatalf("the live wall alone should seal the shop (the live bug), got %v", r.Waypoints)
	}
	r := p.Plan(from, to, Options{Walls: walls, LiveEdges: doors})
	if !r.OK {
		t.Fatalf("no route with the live door: %s", r.Reason)
	}
	hops := r.Hops()
	if len(hops) != 1 || hops[0].LocX != 22 || hops[0].LocZ != 4 || hops[0].From != (Point{X: 22, Z: 4}) {
		t.Errorf("hops = %v", hops)
	}
	if r := p.Plan(from, to, Options{Walls: walls, LiveEdges: doors, WalkOnly: true}); r.OK {
		t.Error("a walk-only plan hopped the door")
	}
	if r := p.Plan(from, to, Options{Walls: walls, LiveEdges: doors, Avoid: map[Point]bool{{X: 22, Z: 4}: true}}); r.OK {
		t.Error("a banned live door was still used")
	}
}

// liveDoorWorld is a fakeWorld whose client reports its shut doors as live walls and live
// door edges, and stops reporting them once they are open.
type liveDoorWorld struct {
	*fakeWorld
	angle int
}

func (f *liveDoorWorld) ExtraWalls(at Point, radius int) map[Point]byte {
	walls := map[Point]byte{}
	for _, d := range f.doors {
		if !d.open {
			AddWallFaces(walls, d.at.X, d.at.Z, d.at.Level, 0, f.angle)
		}
	}
	return walls
}

func (f *liveDoorWorld) LiveDoorEdges(at Point, radius int) []*Edge {
	var out []*Edge
	for _, d := range f.doors {
		if !d.open {
			out = append(out, LiveDoorEdges(d.at.X, d.at.Z, d.at.Level, 0, f.angle, d.id, d.name, "Open")...)
		}
	}
	return out
}

func TestWalkerOpensALiveShutDoorTheMapHasOpen(t *testing.T) {
	col, _, _ := shopStreet()
	f := newFakeWorld(t, col, Point{X: 0, Z: 0})
	// Shut only for movement: the planner's grid (the pack) has the doorway open.
	f.move.wall(Point{X: 22, Z: 4}, Point{X: 22, Z: 5})
	door := &fakeDoor{at: Point{X: 22, Z: 4}, beyond: Point{X: 22, Z: 5}, id: 1530, openID: 1531, name: "Door"}
	f.doors = append(f.doors, door)
	lw := &liveDoorWorld{fakeWorld: f, angle: 1}
	w := NewWalker(NewPlanner(col, nil), lw)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	res := w.WalkTo(Point{X: 22, Z: 8}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if !door.open {
		t.Error("arrived without opening the door")
	}
	all := strings.Join(logs, "\n")
	if strings.Contains(all, "no path") || strings.Contains(all, ": scene") || strings.Contains(all, ": unstick") {
		t.Errorf("the baked walk did not plan through the live door:\n%s", all)
	}
	if !strings.Contains(all, "crossed Door at 22,4") {
		t.Errorf("no door crossing logged:\n%s", all)
	}
}

// TestAuburysShutDoorOnTheRealPack is the live failure of 2026-09-26 on the real pack: Aubury's
// door (static 1531 open at 3253,3399) shut by a player puts 1530 at 3253,3398 angle 1. From
// inside the house at 3236,3406, where every failing account made its first replan, the shop
// was unreachable with the live wall alone and is one door hop away with the live door edge.
func TestAuburysShutDoorOnTheRealPack(t *testing.T) {
	p := realPack(t)
	walls := map[Point]byte{}
	AddWallFaces(walls, 3253, 3398, 0, 0, 1)
	doors := LiveDoorEdges(3253, 3398, 0, 0, 1, 1530, "Door", "Open")
	from, aubury := Point{X: 3236, Z: 3406}, Point{X: 3253, Z: 3402}
	if r := p.Plan(from, aubury, Options{Walls: walls}); r.OK {
		t.Logf("note: the shop is reachable without the door here: %s", r.String())
	}
	r := p.Plan(from, aubury, Options{Walls: walls, LiveEdges: doors})
	if !r.OK {
		t.Fatalf("no route with the live door: %s", r.Reason)
	}
	used := false
	for _, h := range r.Hops() {
		used = used || (h.LocX == 3253 && h.LocZ == 3398)
	}
	if !used {
		t.Errorf("route does not open the shut shop door: %s", r.String())
	}
}
