package webwalk

import (
	"testing"
	"time"
)

// TestWalkerCallsAsideOnStraightLegsWithoutChangingTheWalk: WalkOptions.Aside is told where each
// click is heading and how long ago it was sent, and side work that only waits (as a cast's
// round trip does) still ends at the destination.
func TestWalkerCallsAsideOnStraightLegsWithoutChangingTheWalk(t *testing.T) {
	col := newGrid().fill(0, 0, 60, 2, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 1})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	var polls []AsidePoll
	res := w.WalkTo(Point{X: 60, Z: 1}, WalkOptions{Aside: func(p AsidePoll) {
		polls = append(polls, p)
	}})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if len(polls) == 0 {
		t.Fatal("Aside was never called")
	}
	sawFresh, sawOld := false, false
	for _, p := range polls {
		if p.SinceClick == 0 {
			sawFresh = true
		}
		if p.SinceClick >= tick {
			sawOld = true
		}
		if Chebyshev(p.Me, p.Target) > legClickSteps {
			t.Errorf("poll %+v: the target is further than one click", p)
		}
	}
	if !sawFresh || !sawOld {
		t.Errorf("SinceClick never showed both a fresh click and an older one: %+v", polls)
	}
	if last := polls[len(polls)-1]; !last.LegEnd || last.Target != (Point{X: 60, Z: 1}) {
		t.Errorf("the last poll = %+v, want the leg's end", last)
	}
	// Without Aside the same walk is unchanged, and the hook is cleared after the walk.
	if w.aside != nil {
		t.Error("Aside outlived its walk")
	}
}

// TestWalkerAsideMayDelay: an Aside that spends a tick of its own (a cast waiting out the server's
// delay) does not stall or derail the walk.
func TestWalkerAsideMayDelay(t *testing.T) {
	col := newGrid().fill(0, 0, 60, 2, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 1})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	calls := 0
	res := w.WalkTo(Point{X: 60, Z: 1}, WalkOptions{Aside: func(p AsidePoll) {
		if p.SinceClick >= tick && calls%2 == 0 {
			f.Delay(600 * time.Millisecond)
		}
		calls++
	}})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
}

// WalkOptions.ClickSteps makes a walk's clicks aim further: fewer of them for the same route, none further than
// MaxClickSteps, and the default unchanged when it is left at 0. A walk with side work (High Alchemy, a rub) wants
// that: every click is a tick the side work cannot use, and the last few tiles before the next one are tiles it cannot
// be sent on.
func TestClickStepsAimFurtherAndSendFewerClicks(t *testing.T) {
	col := newGrid().fill(0, 0, 420, 6, 0)
	run := func(steps int) (clicks, furthest int) {
		f := newFakeWorld(t, col, Point{X: 2, Z: 3})
		w := NewWalker(NewPlanner(col, &Graph{}), f)
		res := w.WalkTo(Point{X: 402, Z: 3}, WalkOptions{ClickSteps: steps, Timeout: 30 * time.Minute, Aside: func(p AsidePoll) {
			furthest = max(furthest, Chebyshev(p.Me, p.Target))
		}})
		if !res.Arrived() {
			t.Fatalf("ClickSteps %d: %+v", steps, res)
		}
		return f.walks, furthest
	}
	plain, plainFar := run(0)
	if want := 400/(legClickSteps-arriveRadius) + 2; plain > want || plainFar > legClickSteps {
		t.Errorf("default: %d clicks (at most %d), a target %d tiles off (at most %d)", plain, want, plainFar, legClickSteps)
	}
	far, farFar := run(100)
	if far >= plain || far > 400/(100-arriveRadius)+2 {
		t.Errorf("ClickSteps 100: %d clicks against %d by default, want fewer and at most %d", far, plain, 400/(100-arriveRadius)+2)
	}
	if farFar <= legClickSteps || farFar > 100 {
		t.Errorf("ClickSteps 100: the furthest target was %d tiles off, want between %d and 100", farFar, legClickSteps)
	}
	capped, cappedFar := run(500)
	if cappedFar > MaxClickSteps {
		t.Errorf("ClickSteps 500: a click aimed %d tiles off, want at most MaxClickSteps %d", cappedFar, MaxClickSteps)
	}
	if capped > far {
		t.Errorf("ClickSteps 500 sent %d clicks, more than the %d of ClickSteps 100", capped, far)
	}
}
