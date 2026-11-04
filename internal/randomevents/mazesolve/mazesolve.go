// Package mazesolve is a from-scratch, graph-based solver for an unknown maze whose
// layout is discovered only by probing it one step at a time — the shape of problem
// the Mime and maze random events pose (see internal/randomevents/detect.go's package
// doc comment and COLLAB.md's "maze worktree" entries for the full background).
//
// This is NOT a port of RandomEvents.ts's own maze/ subpackage (mazeGraph/mazeLayout/
// mazeWalk/selectRoute/solveMaze) — that source was never available to this project
// (checked: reference/rsb0t-scripts/ only curated the four originally-requested
// scripts' dependencies, and the maze solver was never one of them). This is instead a
// standard, independently-written graph algorithm for the same class of problem:
// depth-first search with backtracking over a maze that is revealed one edge at a time
// by the caller's own Prober, rather than known up front. It has no notion of "walls"
// or "loc ids" at all — every real-world detail (wall locs, doors that auto-close,
// which cell is the exit) lives entirely in the Prober implementation, which is what
// makes this package testable against a synthetic maze with no live connection (see
// mazesolve_test.go).
package mazesolve

import (
	"errors"
	"fmt"
)

// Dir is one of the four cardinal directions a step can move in. Only cardinal
// movement is modelled — the maze content this was built for (macro_event_maze.rs2)
// is a grid of single-tile cells joined by straight walls/doors on their four sides,
// not diagonals.
type Dir int

const (
	North Dir = iota
	East
	South
	West
)

// allDirs is the fixed fallback exploration order when no OrderFunc is supplied.
var allDirs = [4]Dir{North, East, South, West}

// Delta returns the (dx, dz) tile offset one step in this direction represents.
func (d Dir) Delta() (dx, dz int) {
	switch d {
	case North:
		return 0, 1
	case East:
		return 1, 0
	case South:
		return 0, -1
	case West:
		return -1, 0
	default:
		return 0, 0
	}
}

// Opposite returns the reverse direction, used when Solve backtracks along the same
// edge it arrived by.
func (d Dir) Opposite() Dir {
	return (d + 2) % 4
}

// String names the direction, for logging.
func (d Dir) String() string {
	switch d {
	case North:
		return "north"
	case East:
		return "east"
	case South:
		return "south"
	case West:
		return "west"
	default:
		return "?"
	}
}

// Cell is one maze grid coordinate. Callers may use absolute world tile coordinates
// directly (the live driver does — see internal/randomevents/maze.go) or any other
// coordinate space; Solve never interprets these beyond equality and Step.
type Cell struct{ X, Z int }

// Step returns the neighbouring cell one tile away in direction d.
func (c Cell) Step(d Dir) Cell {
	dx, dz := d.Delta()
	return Cell{X: c.X + dx, Z: c.Z + dz}
}

// String renders the cell as "(x,z)", for logging.
func (c Cell) String() string {
	return fmt.Sprintf("(%d,%d)", c.X, c.Z)
}

// Prober is the world Solve explores. A live implementation wraps real interactions
// (open a door loc, walk one tile) and position polling; a test implementation is a
// synthetic maze layout (see mazesolve_test.go's gridMaze).
//
// Solve never caches "this edge is open" across a backtrack: the real macro_event_maze
// content (content/scripts/macro events/scripts/general/macro_event_maze.rs2's
// open_and_close_door call, `loc_change(inviswall, 3)` then a 3-tick reversion) shows
// this event's doors auto-close a few ticks after being walked through, so every
// crossing — forward or backtracking — is a fresh attempt through Cross, exactly like
// a real player would have to re-open a door they already walked through once.
type Prober interface {
	// Cross attempts to move one step from `from` in direction d — opening a door and
	// stepping through, or just walking, as the implementation sees fit — and reports
	// whether the position actually changed. Solve assumes a successful Cross moves
	// exactly one tile (the returned Cell equals from.Step(d)); a Prober that can jump
	// further (e.g. a door with a multi-tile movecoord offset) will confuse Solve's
	// backtracking, which retraces by crossing the opposite direction from wherever it
	// ended up — document any such behavior on the concrete Prober if it applies.
	Cross(from Cell, d Dir) (to Cell, ok bool)

	// AtGoal reports whether c is the maze's exit (or close enough to it that arriving
	// there counts as solved — a live Prober can fold "walked next to the finish loc
	// and clicked it" into this call).
	AtGoal(c Cell) bool
}

// OrderFunc biases which untried directions from a cell are attempted first — for
// example, "whichever gets Chebyshev-closer to a known goal tile" when the maze's exit
// has already been spotted (see internal/randomevents/maze.go's use of this). It
// receives the not-yet-tried directions for that cell and returns them in the order to
// try; returning a subset or a differently-ordered slice both work, but every element
// of candidates should normally still appear once. A nil OrderFunc keeps Solve's fixed
// North/East/South/West order. This only affects which working path is found first and
// how many wasted probes happen along the way — Solve still eventually tries every
// direction from every reachable cell before giving up on it, so a bad or trivial
// OrderFunc cannot make a solvable maze look unsolvable.
type OrderFunc func(from Cell, candidates []Dir) []Dir

// Result is what Solve found.
type Result struct {
	// Path is every cell Solve's tracked position actually occupied, in order,
	// starting at the start cell — including cells visited twice while backtracking.
	Path []Cell
	// Steps is the number of Cross attempts made (successful or not).
	Steps int
}

var (
	// ErrStuck means every reachable cell's every direction has been tried (including
	// backtracking all the way to the start) and the goal was never reached — the maze
	// has no path from start to a cell satisfying AtGoal, as far as Cross/AtGoal ever
	// reported.
	ErrStuck = errors.New("mazesolve: exhausted every reachable direction without finding the goal")
	// ErrStepBudget means maxSteps was reached before ErrStuck or success — raise
	// maxSteps, or treat this as "probably solvable, ran out of patience" versus
	// ErrStuck's "provably not reachable from what was explored."
	ErrStepBudget = errors.New("mazesolve: reached the step budget before finding the goal")
)

type frame struct {
	cell       Cell
	arrivedVia Dir
	remaining  []Dir
}

func orderedDirs(cell Cell, order OrderFunc) []Dir {
	cand := make([]Dir, len(allDirs))
	copy(cand, allDirs[:])
	if order != nil {
		return order(cell, cand)
	}
	return cand
}

// Solve explores an unknown maze via depth-first search with backtracking: a standard
// graph-based maze-solving strategy that needs no prior knowledge of the maze's
// layout, only a Prober to try one step at a time and say whether it worked. It is
// guaranteed to find a path to a cell satisfying AtGoal if one exists among the cells
// Cross can ever reach, and to terminate (ErrStuck) if none does, in both cases within
// a number of Cross calls bounded by twice the number of reachable (cell, direction)
// pairs (each edge is tried once forward, and — for edges leading to a dead end —
// once more on the way back). maxSteps is a hard ceiling on top of that, mainly to
// bound a live run against a real server rather than trusting the maze is finite.
func Solve(p Prober, start Cell, maxSteps int, order OrderFunc) (Result, error) {
	path := []Cell{start}
	if p.AtGoal(start) {
		return Result{Path: path}, nil
	}

	visited := map[Cell]bool{start: true}
	stack := []frame{{cell: start, remaining: orderedDirs(start, order)}}
	steps := 0

	for len(stack) > 0 {
		top := &stack[len(stack)-1]

		if len(top.remaining) == 0 {
			// Every direction from this cell has been tried (successfully or not):
			// backtrack to the parent, retracing the edge we arrived by.
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				break
			}
			if steps >= maxSteps {
				return Result{Path: path, Steps: steps}, ErrStepBudget
			}
			steps++
			parent := stack[len(stack)-1].cell
			if _, ok := p.Cross(top.cell, top.arrivedVia.Opposite()); !ok {
				return Result{Path: path, Steps: steps}, ErrStuck
			}
			path = append(path, parent)
			continue
		}

		d := top.remaining[0]
		top.remaining = top.remaining[1:]

		if steps >= maxSteps {
			return Result{Path: path, Steps: steps}, ErrStepBudget
		}
		steps++
		to, ok := p.Cross(top.cell, d)
		if !ok {
			continue
		}

		if visited[to] {
			// Looped back to an already-explored cell — nothing new to learn from
			// there, so immediately retreat to keep the tracked position consistent
			// with the top of the stack.
			if steps >= maxSteps {
				return Result{Path: path, Steps: steps}, ErrStepBudget
			}
			steps++
			if _, ok := p.Cross(to, d.Opposite()); !ok {
				return Result{Path: path, Steps: steps}, ErrStuck
			}
			continue
		}

		visited[to] = true
		path = append(path, to)
		if p.AtGoal(to) {
			return Result{Path: path, Steps: steps}, nil
		}
		stack = append(stack, frame{cell: to, arrivedVia: d, remaining: orderedDirs(to, order)})
	}

	return Result{Path: path, Steps: steps}, ErrStuck
}

// ChebyshevOrder returns an OrderFunc that always tries the direction bringing the
// tracked position Chebyshev-closer to goal first — a cheap "walk toward the exit if
// you can see it" bias, matching how internal/randomevents/maze.go uses this once the
// maze's finish loc becomes visible. It never skips a direction, so it cannot turn a
// solvable maze into an unsolvable one; it only changes which working path is found
// first.
func ChebyshevOrder(goal Cell) OrderFunc {
	dist := func(c Cell) int {
		dx := c.X - goal.X
		if dx < 0 {
			dx = -dx
		}
		dz := c.Z - goal.Z
		if dz < 0 {
			dz = -dz
		}
		if dx > dz {
			return dx
		}
		return dz
	}
	return func(from Cell, candidates []Dir) []Dir {
		out := make([]Dir, len(candidates))
		copy(out, candidates)
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				di := dist(from.Step(out[j]))
				dj := dist(from.Step(out[j-1]))
				if di >= dj {
					break
				}
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
		return out
	}
}
