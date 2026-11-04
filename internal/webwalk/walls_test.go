package webwalk

import (
	"strings"
	"testing"
	"time"
)

func TestAddWallFacesMatchesTheEngine(t *testing.T) {
	walls := map[Point]byte{}
	// Lumbridge's closed Large door: shape 0, angle 0 at 3217,3218 walls its west face.
	AddWallFaces(walls, 3217, 3218, 0, 0, 0)
	if walls[Point{X: 3217, Z: 3218}] != WallWest || walls[Point{X: 3216, Z: 3218}] != WallEast {
		t.Fatalf("straight west = %v", walls)
	}
	if !StepWalled(walls, Point{X: 3216, Z: 3218}, 1, 0) || !StepWalled(walls, Point{X: 3217, Z: 3218}, -1, 0) {
		t.Error("the door does not block crossing it")
	}
	if !StepWalled(walls, Point{X: 3216, Z: 3217}, 1, 1) {
		t.Error("a diagonal past the wall's end is not blocked")
	}
	if StepWalled(walls, Point{X: 3216, Z: 3218}, 0, 1) {
		t.Error("a step along the wall is blocked")
	}

	south := map[Point]byte{}
	AddWallFaces(south, 5, 5, 0, 0, 3)
	if !StepWalled(south, Point{X: 5, Z: 5}, 0, -1) || !StepWalled(south, Point{X: 5, Z: 4}, 0, 1) {
		t.Errorf("straight south = %v", south)
	}
	l := map[Point]byte{}
	AddWallFaces(l, 5, 5, 0, 2, 1)
	if l[Point{X: 5, Z: 5}] != WallNorth|WallEast || l[Point{X: 5, Z: 6}] != WallSouth || l[Point{X: 6, Z: 5}] != WallWest {
		t.Errorf("L north = %v", l)
	}
	none := map[Point]byte{}
	AddWallFaces(none, 5, 5, 0, 1, 0)
	if len(none) != 0 {
		t.Errorf("a corner shape added faces: %v", none)
	}
}

func TestPlannerRoutesAroundLiveWalls(t *testing.T) {
	col := newGrid().fill(0, 0, 6, 6, 0)
	p := NewPlanner(col, nil)
	walls := map[Point]byte{}
	for x := 0; x <= 5; x++ {
		AddWallFaces(walls, x, 3, 0, 0, 3) // a wall along z=3's south face, open at x=6
	}
	r := p.Plan(Point{X: 0, Z: 0}, Point{X: 0, Z: 6}, Options{Walls: walls})
	if !r.OK {
		t.Fatalf("no route: %s", r.Reason)
	}
	tiles := expand(stepPoints(r.Waypoints))
	for i := 1; i < len(tiles); i++ {
		a, b := tiles[i-1], tiles[i]
		if StepWalled(walls, a, b.X-a.X, b.Z-a.Z) {
			t.Fatalf("route steps %s -> %s through the wall", a, b)
		}
	}
	if r.Cost <= 6 {
		t.Errorf("cost %d: the route did not go round", r.Cost)
	}
	blocked := map[[2]Point]bool{{{X: 0, Z: 0}, {X: 0, Z: 1}}: true, {{X: 0, Z: 0}, {X: 1, Z: 1}}: true, {{X: 0, Z: 0}, {X: 1, Z: 0}}: true}
	if r := p.Plan(Point{X: 0, Z: 0}, Point{X: 3, Z: 3}, Options{Blocked: blocked}); r.OK {
		t.Errorf("left the corner through blocked steps: %v", r.Waypoints)
	}
}

// liveWallWorld is a fakeWorld whose client reports live walls.
type liveWallWorld struct {
	*fakeWorld
	walls map[Point]byte
}

func (f *liveWallWorld) ExtraWalls(at Point, radius int) map[Point]byte { return f.walls }

// hall is Lumbridge's case in miniature: a 10x10 room where two leaf-sized walls the pack
// does not have (an opened double door) block the straight line from (5,9) down to (5,0).
func hall(t *testing.T) (*fakeWorld, map[Point]byte) {
	col := newGrid().fill(0, 0, 9, 9, 0)
	f := newFakeWorld(t, col, Point{X: 5, Z: 9})
	f.move = &grid{walk: col.walk, blocked: map[[2]Point]bool{}}
	walls := map[Point]byte{}
	for x := 5; x <= 6; x++ {
		AddWallFaces(walls, x, 5, 0, 0, 3)
		f.move.wall(Point{X: x, Z: 5}, Point{X: x, Z: 4})
	}
	return f, walls
}

func TestWalkerPlansAroundLiveWallsWithoutStalling(t *testing.T) {
	f, walls := hall(t)
	lw := &liveWallWorld{fakeWorld: f, walls: walls}
	w := NewWalker(NewPlanner(f.plan, nil), lw)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	start := f.now
	if res := w.WalkTo(Point{X: 5, Z: 0}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if all := strings.Join(logs, "\n"); strings.Contains(all, "stalled") {
		t.Errorf("stalled although the live walls were known:\n%s", all)
	}
	if took := f.now.Sub(start); took > 20*time.Second {
		t.Errorf("took %v", took)
	}
}

func TestWalkerRepathsAtOnceAfterATeleport(t *testing.T) {
	col := newGrid().fill(0, 0, 40, 20, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 0})
	w := NewWalker(NewPlanner(col, nil), f)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	f.onDelay = func(n int) {
		if n == 4 {
			f.me, f.path = Point{X: 9, Z: 8}, nil // near the old line, but not on it
		}
	}
	if res := w.WalkTo(Point{X: 40, Z: 0}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "(teleported)") {
		t.Errorf("no teleport repath:\n%s", all)
	}
	if strings.Contains(all, "stalled") {
		t.Errorf("stalled after the teleport:\n%s", all)
	}
}

func TestWalkerLearnsAStepTheServerRefuses(t *testing.T) {
	f, _ := hall(t)
	w := NewWalker(NewPlanner(f.plan, nil), f)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	start := f.now
	res := w.WalkTo(Point{X: 5, Z: 0}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "is blocked live") {
		t.Errorf("no step learned:\n%s", all)
	}
	if strings.Contains(all, ": scene") || strings.Contains(all, ": unstick") {
		t.Errorf("escalated past the baked walk:\n%s", all)
	}
	if took := f.now.Sub(start); took > 90*time.Second {
		t.Errorf("took %v of game time", took)
	}
}
