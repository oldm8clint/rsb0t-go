package webwalk

import (
	"strings"
	"testing"
)

// A walk that ended unreachable is not planned again from the same zone for unreachableMemoFor: the
// second call answers at once (after unreachableMemoWait, so the caller's loop cannot spin) with no
// plan, and once the window has passed, or from another zone, the walk is planned as before.
func TestAnUnreachableWalkIsNotPlannedAgainForAWhile(t *testing.T) {
	start := Point{X: 3000, Z: 3000, Level: 3} // a sealed pocket nowhere catalogued as a strand site
	g := newGrid().fill(start.X-2, start.Z-2, start.X+2, start.Z+2, start.Level)
	g.fill(3158, 3949, 3160, 3951, 0)
	f, w := strandWorld(t, g, nil, start)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	plans := func() int {
		n := 0
		for _, l := range logs {
			if strings.Contains(l, "baked walk") {
				n++
			}
		}
		return n
	}
	dest := Point{X: 3159, Z: 3950, Level: 0}

	t0 := f.now
	if res := w.WalkTo(dest, WalkOptions{}); res.Outcome != OutcomeUnreachable {
		t.Fatalf("first walk: %+v, want unreachable", res)
	}
	first, firstTook := plans(), f.now.Sub(t0)
	if first == 0 {
		t.Fatalf("the first walk did not plan: %v", logs)
	}

	t1 := f.now
	res := w.WalkTo(dest, WalkOptions{})
	if res.Outcome != OutcomeUnreachable || !strings.Contains(res.Reason, "the same walk ended unreachable") {
		t.Fatalf("second walk: %+v, want refused from the memo", res)
	}
	if plans() != first {
		t.Fatalf("the second walk planned again: %v", logs)
	}
	if took := f.now.Sub(t1); took < unreachableMemoWait || took >= firstTook {
		t.Errorf("the refusal took %v; want at least %v and less than the first walk's %v", took, unreachableMemoWait, firstTook)
	}

	// Another destination is planned as before.
	other := Point{X: 3159, Z: 3949, Level: 0}
	w.WalkTo(other, WalkOptions{})
	if plans() == first {
		t.Fatalf("a different destination was refused from the memo: %v", logs)
	}

	// Past the window the first destination is planned again.
	before := plans()
	f.now = f.now.Add(unreachableMemoFor)
	w.WalkTo(dest, WalkOptions{})
	if plans() == before {
		t.Fatalf("%v later the walk was still refused: %v", unreachableMemoFor, logs)
	}
}
