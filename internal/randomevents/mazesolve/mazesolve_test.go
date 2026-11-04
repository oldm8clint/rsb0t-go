package mazesolve

import (
	"errors"
	"testing"
)

// edge is a canonical, direction-independent key for one cell-to-cell connection,
// so a synthetic maze only needs to record each wall once.
type edge struct {
	a, b Cell
}

func canonicalEdge(from Cell, d Dir) edge {
	to := from.Step(d)
	if to.X < from.X || (to.X == from.X && to.Z < from.Z) {
		return edge{a: to, b: from}
	}
	return edge{a: from, b: to}
}

// gridMaze is a synthetic Prober: a set of walls (edges that can never be crossed) and
// a goal cell. Every edge not listed as a wall is open floor. crossLog records every
// Cross call, so tests can assert on probe/backtrack behavior, not just the outcome.
type gridMaze struct {
	walls    map[edge]bool
	goal     Cell
	crossLog []Cell
}

func newGridMaze(goal Cell, walls ...edge) *gridMaze {
	m := &gridMaze{walls: make(map[edge]bool), goal: goal}
	for _, w := range walls {
		m.walls[w] = true
	}
	return m
}

func (m *gridMaze) Cross(from Cell, d Dir) (Cell, bool) {
	if m.walls[canonicalEdge(from, d)] {
		return Cell{}, false
	}
	to := from.Step(d)
	m.crossLog = append(m.crossLog, to)
	return to, true
}

func (m *gridMaze) AtGoal(c Cell) bool { return c == m.goal }

func wall(from Cell, d Dir) edge { return canonicalEdge(from, d) }

func TestSolve_StartIsGoal(t *testing.T) {
	m := newGridMaze(Cell{0, 0})
	res, err := Solve(m, Cell{0, 0}, 100, nil)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(res.Path) != 1 || res.Path[0] != (Cell{0, 0}) {
		t.Fatalf("expected trivial one-cell path, got %v", res.Path)
	}
	if res.Steps != 0 {
		t.Fatalf("expected 0 probes when already at goal, got %d", res.Steps)
	}
}

func TestSolve_StraightCorridor(t *testing.T) {
	// (0,0) -> (1,0) -> (2,0) -> (3,0)=goal, no walls at all — but plain unbiased DFS
	// on an open grid (no walls to stop it) would dive North forever before ever
	// trying East, so this exercises ChebyshevOrder rather than nil, the same bias
	// internal/randomevents/maze.go applies once the maze's exit becomes visible.
	m := newGridMaze(Cell{3, 0})
	res, err := Solve(m, Cell{0, 0}, 100, ChebyshevOrder(Cell{3, 0}))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	want := []Cell{{0, 0}, {1, 0}, {2, 0}, {3, 0}}
	if !samePath(res.Path, want) {
		t.Fatalf("path = %v, want %v", res.Path, want)
	}
}

func TestSolve_DeadEndRequiresBacktrack(t *testing.T) {
	// From (0,0): North leads to a dead end (1 cell deep). East leads to the goal.
	// North/East/South/West is the fixed default order, so Solve tries the North dead
	// end first, must backtrack, then finds the goal via East.
	goal := Cell{1, 0}
	m := newGridMaze(goal,
		// Seal (0,1) into a true dead end: only its South edge, back to the start,
		// stays open.
		wall(Cell{0, 1}, North), wall(Cell{0, 1}, East), wall(Cell{0, 1}, West),
	)
	res, err := Solve(m, Cell{0, 0}, 100, nil)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	wantPath := []Cell{{0, 0}, {0, 1}, {0, 0}, {1, 0}}
	if !samePath(res.Path, wantPath) {
		t.Fatalf("path = %v, want %v (should explore the dead end, backtrack, then reach goal)", res.Path, wantPath)
	}
}

func TestSolve_UnreachableGoalReturnsErrStuck(t *testing.T) {
	// Wall off (0,0) completely from its only neighbour toward the goal.
	goal := Cell{5, 5}
	m := newGridMaze(goal,
		wall(Cell{0, 0}, North), wall(Cell{0, 0}, East),
		wall(Cell{0, 0}, South), wall(Cell{0, 0}, West),
	)
	res, err := Solve(m, Cell{0, 0}, 1000, nil)
	if !errors.Is(err, ErrStuck) {
		t.Fatalf("err = %v, want ErrStuck", err)
	}
	if len(res.Path) != 1 {
		t.Fatalf("path = %v, want the solver to never leave the start cell", res.Path)
	}
}

func TestSolve_StepBudgetExceeded(t *testing.T) {
	// A long open corridor with a low step budget: Solve should give up with
	// ErrStepBudget, not ErrStuck (the maze may still be solvable, we just stopped).
	m := newGridMaze(Cell{100, 0})
	_, err := Solve(m, Cell{0, 0}, 3, nil)
	if !errors.Is(err, ErrStepBudget) {
		t.Fatalf("err = %v, want ErrStepBudget", err)
	}
}

func TestSolve_CycleDoesNotInfiniteLoop(t *testing.T) {
	// A fully-enclosed 2x2 ring of cells (0,0)-(0,1)-(1,1)-(1,0)-(0,0), with a single
	// spur off (1,0) leading to the goal. Every edge leaving this little pocket is
	// walled off except the intended ring connections and the spur, so Solve cannot
	// wander into an unbounded open grid — it must instead notice the ring closes back
	// on an already-visited cell and retreat, rather than loop on the cycle forever.
	goal := Cell{2, 0}
	m := newGridMaze(goal,
		wall(Cell{0, 0}, South), wall(Cell{0, 0}, West),
		wall(Cell{1, 0}, South),
		wall(Cell{1, 1}, North), wall(Cell{1, 1}, East),
		wall(Cell{0, 1}, North), wall(Cell{0, 1}, West),
	)
	res, err := Solve(m, Cell{0, 0}, 200, nil)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if res.Path[len(res.Path)-1] != goal {
		t.Fatalf("last cell = %v, want goal %v", res.Path[len(res.Path)-1], goal)
	}
}

func TestSolve_ChebyshevOrderPrefersShorterPath(t *testing.T) {
	// Open 2D grid, no walls: with a goal bias, Solve should walk straight there
	// without ever visiting a cell off the direct path.
	goal := Cell{2, 2}
	m := newGridMaze(goal)
	res, err := Solve(m, Cell{0, 0}, 100, ChebyshevOrder(goal))
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	for _, c := range res.Path {
		if c.X < 0 || c.X > 2 || c.Z < 0 || c.Z > 2 {
			t.Fatalf("path wandered outside the direct box: %v in %v", c, res.Path)
		}
	}
	if res.Path[len(res.Path)-1] != goal {
		t.Fatalf("did not reach goal: %v", res.Path)
	}
	if res.Steps > 4 {
		t.Fatalf("expected the Chebyshev bias to reach a diagonal goal in 4 probes, took %d", res.Steps)
	}
}

func TestDirOppositeAndDelta(t *testing.T) {
	cases := []struct {
		d    Dir
		dx   int
		dz   int
		want Dir
	}{
		{North, 0, 1, South},
		{East, 1, 0, West},
		{South, 0, -1, North},
		{West, -1, 0, East},
	}
	for _, c := range cases {
		if dx, dz := c.d.Delta(); dx != c.dx || dz != c.dz {
			t.Errorf("%v.Delta() = (%d,%d), want (%d,%d)", c.d, dx, dz, c.dx, c.dz)
		}
		if got := c.d.Opposite(); got != c.want {
			t.Errorf("%v.Opposite() = %v, want %v", c.d, got, c.want)
		}
	}
}

func samePath(got, want []Cell) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
