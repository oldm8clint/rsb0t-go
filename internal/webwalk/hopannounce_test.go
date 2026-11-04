package webwalk

import (
	"testing"
	"time"
)

// jumpWorld is a fakeWorld that is also a JumpClient and records what the walker announced.
type jumpWorld struct {
	*fakeWorld
	announced []announcedHop
	withdrawn int
}

type announcedHop struct {
	name     string
	from, to Point
	ttl      time.Duration
}

func (j *jumpWorld) ExpectHop(name string, from, to Point, ttl time.Duration) func() {
	j.announced = append(j.announced, announcedHop{name, from, to, ttl})
	return func() { j.withdrawn++ }
}

// spiritTreeGraph is two islands joined by one special hop of the given span, the shape of a spirit tree crossing.
func spiritTreeGraph(span int) (*grid, *Graph) {
	col := newGrid().fill(0, 0, 3, 3, 0).fill(span, span, span+3, span+3, 0)
	g := &Graph{Edges: []Edge{{
		From: Point{X: 3, Z: 3}, To: Point{X: span, Z: span}, Kind: KindPortal, Cost: 20,
		LocName: "Spirit Tree", Action: "Talk-to", LocID: -1, OpenLocID: -1, LandsOnTile: true,
		Special: &Special{Label: "Village spirit → Varrock forest", NPC: "Spirit Tree"},
	}}}
	return col, g
}

// TestAHopThatLandsFarAwayIsAnnouncedToTheGuardian is the live line of 14 Oct 2026: 151 "[guardian] displaced ... UNEXPLAINED" for
// spirit-tree hops the walker took on purpose. The walker is the one place that knows a hop is a hop, so it says so before it takes
// one that lands far from where it is operated.
func TestAHopThatLandsFarAwayIsAnnouncedToTheGuardian(t *testing.T) {
	col, g := spiritTreeGraph(40)
	j := &jumpWorld{fakeWorld: newFakeWorld(t, col, Point{X: 0, Z: 0})}
	w := NewWalker(NewPlanner(col, g), j)
	w.SpecialHop = func(e *Edge) bool { j.me = e.To; return true }
	w.NPCHop = w.SpecialHop
	if res := w.WalkTo(Point{X: 42, Z: 42}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if len(j.announced) != 1 {
		t.Fatalf("announced %d hops, want 1: %+v", len(j.announced), j.announced)
	}
	a := j.announced[0]
	if a.name != "hop Village spirit → Varrock forest" || a.from != (Point{X: 3, Z: 3}) || a.to != (Point{X: 40, Z: 40}) || a.ttl != hopAnnounceTTL {
		t.Errorf("announced %+v", a)
	}
	if j.withdrawn != 0 {
		t.Errorf("a hop that landed was withdrawn %d time(s): the Guardian has not yet looked at the tile it landed on", j.withdrawn)
	}
}

// A hop that fails did not jump, so what was announced for it is taken back.
func TestAHopThatFailsWithdrawsItsAnnouncement(t *testing.T) {
	col, g := spiritTreeGraph(40)
	j := &jumpWorld{fakeWorld: newFakeWorld(t, col, Point{X: 0, Z: 0})}
	w := NewWalker(NewPlanner(col, g), j)
	w.SpecialHop = func(e *Edge) bool { return false }
	w.NPCHop = w.SpecialHop
	w.WalkTo(Point{X: 42, Z: 42}, WalkOptions{MaxPasses: 1})
	if len(j.announced) == 0 {
		t.Fatal("nothing was announced")
	}
	if j.withdrawn != len(j.announced) {
		t.Errorf("%d announcements, %d withdrawn: a failed hop left its landing expected", len(j.announced), j.withdrawn)
	}
}

// A hop a walk could nearly have covered reads as ordinary whatever the Guardian is told, so it is not announced.
func TestAShortHopIsNotAnnounced(t *testing.T) {
	col, g := spiritTreeGraph(hopAnnounceMin - 5)
	j := &jumpWorld{fakeWorld: newFakeWorld(t, col, Point{X: 0, Z: 0})}
	w := NewWalker(NewPlanner(col, g), j)
	w.SpecialHop = func(e *Edge) bool { j.me = e.To; return true }
	w.NPCHop = w.SpecialHop
	if res := w.WalkTo(Point{X: 26, Z: 26}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if len(j.announced) != 0 {
		t.Errorf("a %d-tile hop was announced: %+v", hopAnnounceMin-5, j.announced)
	}
}

// A hop without a special label is named by its action and loc.
func TestHopNameFallsBackToTheLoc(t *testing.T) {
	e := &Edge{Action: "Use", LocName: "Portal", Kind: KindPortal}
	if got := hopName(e); got != `hop Use "Portal"` {
		t.Errorf("hopName = %q", got)
	}
	e.Special = &Special{Label: "Cave"}
	if got := hopName(e); got != "hop Cave" {
		t.Errorf("hopName = %q", got)
	}
}
