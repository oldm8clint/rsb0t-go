package webwalk

import (
	"testing"
	"time"
)

// stubClient is the smallest thing that satisfies Client. The hostile hook is gated before any
// route is planned, so these tests need no grid, no planner and no movement.
type stubClient struct {
	me  Point
	now time.Time
}

func (s *stubClient) Tile() (Point, bool)                { return s.me, true }
func (s *stubClient) Walk(path []Point, run bool) error  { return nil }
func (s *stubClient) FindLocs(q LocQuery) []LocMatch     { return nil }
func (s *stubClient) OpLoc(op, x, z, id int) error       { return nil }
func (s *stubClient) CantReachSince(mark time.Time) bool { return false }
func (s *stubClient) Delay(d time.Duration) bool         { s.now = s.now.Add(d); return true }
func (s *stubClient) Now() time.Time                     { return s.now }

// hostileStub is a stubClient that also implements HostileClient, counting how often the walker
// asked it to break off.
type hostileStub struct {
	stubClient
	calls int
	moved bool
	why   string
}

func (h *hostileStub) BreakOffFromHostiles() (bool, string) {
	h.calls++
	return h.moved, h.why
}

func newTestWalker(c Client) *Walker {
	return &Walker{Client: c}
}

// A Client that does not implement HostileClient must be left entirely alone, even on a walk
// that asked for hostile avoidance. This is the whole point of an optional interface: every
// existing Client implementation keeps compiling and keeps behaving identically.
func TestHostileTickIgnoresAClientThatDoesNotImplementIt(t *testing.T) {
	w := newTestWalker(&stubClient{now: time.Unix(1000, 0)})
	w.walk.avoidHostiles = true
	if moved, why := w.hostileTick(); moved || why != "" {
		t.Fatalf("hostileTick on a plain Client = %v,%q, want false,\"\"", moved, why)
	}
}

// Off by default: a walk that never set AvoidHostiles must not even ask, so adding the interface
// to internal/webwalk/live cannot change what any other script's walks do.
func TestHostileTickDoesNotAskUnlessTheWalkOptedIn(t *testing.T) {
	h := &hostileStub{stubClient: stubClient{now: time.Unix(1000, 0)}, moved: true, why: "broke off from the White wolf"}
	w := newTestWalker(h)
	w.walk.avoidHostiles = false
	if moved, _ := w.hostileTick(); moved {
		t.Error("hostileTick moved the player on a walk that did not opt in")
	}
	if h.calls != 0 {
		t.Errorf("client was asked %d times on a walk that did not opt in, want 0", h.calls)
	}
}

func TestHostileTickPassesTheClientsAnswerThrough(t *testing.T) {
	h := &hostileStub{stubClient: stubClient{now: time.Unix(1000, 0)}, moved: true, why: "broke off from the White wolf"}
	w := newTestWalker(h)
	w.walk.avoidHostiles = true
	moved, why := w.hostileTick()
	if !moved || why != "broke off from the White wolf" {
		t.Fatalf("hostileTick = %v,%q, want true,%q", moved, why, "broke off from the White wolf")
	}
	if h.calls != 1 {
		t.Errorf("client asked %d times, want exactly 1 per poll", h.calls)
	}
	// A client that declines must not make the walk replan.
	h.moved, h.why = false, ""
	if moved, _ := w.hostileTick(); moved {
		t.Error("hostileTick reported a move when the client declined to break off")
	}
}

// beginWalk is what resolves the option for the whole walk, so a walk that asked keeps asking
// even after a repath rebuilds nothing else.
func TestBeginWalkCarriesTheAvoidHostilesOption(t *testing.T) {
	h := &hostileStub{stubClient: stubClient{me: Point{X: 10, Z: 10}, now: time.Unix(1000, 0)}}
	w := newTestWalker(h)

	w.beginWalk(Point{X: 20, Z: 20}, WalkOptions{})
	if w.walk.avoidHostiles {
		t.Error("avoidHostiles set for a walk that did not ask for it")
	}
	w.beginWalk(Point{X: 20, Z: 20}, WalkOptions{AvoidHostiles: true})
	if !w.walk.avoidHostiles {
		t.Error("avoidHostiles not carried through from WalkOptions")
	}
}
