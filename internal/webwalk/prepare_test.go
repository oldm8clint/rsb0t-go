package webwalk

import (
	"testing"
	"time"
)

// Walker.Prepare is how side work that belongs to the account (ScarabSwarm's run staller) reaches every walk its Walker
// makes, whichever package made the call. It sees the options the caller gave and completes them before the walk
// begins; what it returns runs as the walk returns.
func TestPrepareCompletesEveryWalksOptionsAndRunsDoneAfterIt(t *testing.T) {
	col := newGrid().fill(0, 0, 420, 6, 0)
	dest := Point{X: 402, Z: 3}
	run := func(prepared bool, callerAside func(AsidePoll)) (clicks, polls, preps, dones int, seen WalkOptions) {
		f := newFakeWorld(t, col, Point{X: 2, Z: 3})
		w := NewWalker(NewPlanner(col, &Graph{}), f)
		if prepared {
			w.Prepare = func(o *WalkOptions) func() {
				preps++
				seen = *o
				if o.Aside == nil {
					o.Aside = func(AsidePoll) { polls++ }
				}
				if o.ClickSteps == 0 {
					o.ClickSteps = 100
				}
				return func() { dones++ }
			}
		}
		res := w.WalkTo(dest, WalkOptions{Radius: 1, Timeout: 30 * time.Minute, Aside: callerAside})
		if !res.Arrived() {
			t.Fatalf("prepared=%v: %+v", prepared, res)
		}
		return f.walks, polls, preps, dones, seen
	}

	plain, _, _, _, _ := run(false, nil)
	clicks, polls, preps, dones, seen := run(true, nil)
	if preps != 1 || dones != 1 {
		t.Fatalf("Prepare ran %d time(s) and its done %d time(s) for one walk, want 1 and 1", preps, dones)
	}
	if seen.Radius != 1 || seen.Timeout != 30*time.Minute || seen.Aside != nil || seen.ClickSteps != 0 {
		t.Errorf("Prepare saw %+v, want the caller's own options untouched (radius 1, 30 minutes, no Aside, no ClickSteps)", seen)
	}
	if polls == 0 {
		t.Error("the Aside Prepare added was never polled")
	}
	if clicks >= plain {
		t.Errorf("the ClickSteps Prepare added changed nothing: %d clicks against %d without it", clicks, plain)
	}

	// A caller's own Aside is the caller's: Prepare sees it, and the walk polls that one.
	own := 0
	_, preparePolls, preps, dones, seen := run(true, func(AsidePoll) { own++ })
	if preps != 1 || dones != 1 || seen.Aside == nil {
		t.Fatalf("with an Aside of its own: Prepare ran %d, done %d, saw Aside %v", preps, dones, seen.Aside != nil)
	}
	if own == 0 || preparePolls != 0 {
		t.Errorf("the caller's Aside was polled %d time(s) and Prepare's %d, want the caller's only", own, preparePolls)
	}
}

// The done function runs when the walk fails too: it is where a walk's loose ends (an open menu, a log line) are cleared.
func TestPrepareDoneRunsWhenTheWalkDoesNotArrive(t *testing.T) {
	col := newGrid().fill(0, 0, 20, 5, 0).fill(100, 0, 102, 5, 0) // two islands: no way across
	f := newFakeWorld(t, col, Point{X: 2, Z: 3})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	dones := 0
	w.Prepare = func(*WalkOptions) func() { return func() { dones++ } }
	if res := w.WalkTo(Point{X: 101, Z: 3}, WalkOptions{Timeout: 5 * time.Minute}); res.Arrived() {
		t.Fatalf("walked across a gap: %+v", res)
	}
	if dones != 1 {
		t.Errorf("done ran %d time(s) for a walk that did not arrive, want 1", dones)
	}
}

// A Prepare that returns no done function, and a Walker with no Prepare at all, walk as ever.
func TestPrepareMayReturnNoDoneAndIsOptional(t *testing.T) {
	col := newGrid().fill(0, 0, 60, 2, 0)
	for _, prepare := range []func(*WalkOptions) func(){nil, func(*WalkOptions) func() { return nil }} {
		f := newFakeWorld(t, col, Point{X: 0, Z: 1})
		w := NewWalker(NewPlanner(col, &Graph{}), f)
		w.Prepare = prepare
		if res := w.WalkTo(Point{X: 60, Z: 1}, WalkOptions{}); !res.Arrived() {
			t.Fatalf("result = %+v", res)
		}
	}
}
