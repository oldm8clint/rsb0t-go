package webwalk

import (
	"testing"
	"time"
)

// blockerStub is a HostileClient that also reports hostiles it has given up on.
type blockerStub struct {
	stubClient
	blocked []Point
	drains  int
}

func (b *blockerStub) BreakOffFromHostiles() (bool, string) { return false, "" }

func (b *blockerStub) BlockedByHostiles() []Point {
	b.drains++
	out := b.blocked
	b.blocked = nil
	return out
}

func blockerWalker(b *blockerStub) *Walker {
	w := newTestWalker(b)
	w.walk.avoidHostiles = true
	return w
}

// The bug this closes, measured live 2026-09-22: six ProgressiveFisher accounts walking home
// from Karamja produced 167 break-offs against 163 clear-and-resumes and 20 "NOT pushing on
// through: level 27 against combat 3" lines, all at 0 xp, because every re-plan led straight
// back through the same Giant bats. internal/hostileevade had only two outcomes -- push
// through or keep breaking off -- and for a weak account against a strong hostile neither
// ends. This is the third one.
func TestAGivenUpHostileBecomesAnAvoidedSquare(t *testing.T) {
	b := &blockerStub{stubClient: stubClient{now: time.Unix(1000, 0)}, blocked: []Point{{X: 2755, Z: 3398}}}
	w := blockerWalker(b)
	dest := Point{X: 2809, Z: 3441}

	blocked, why := w.hostileBlocked(dest)
	if !blocked {
		t.Fatal("a hostile the client gave up on did not become an avoided square")
	}
	if why == "" {
		t.Error("no reason given; the walk log is how this gets diagnosed next time")
	}
	if len(w.walk.flee) != 1 {
		t.Fatalf("flee squares = %d, want 1", len(w.walk.flee))
	}
	if !InZones(Point{X: 2755, Z: 3398}, w.walk.flee) {
		t.Error("the square the hostile stood on is not inside the avoided rect")
	}
}

// The companion that stops this becoming a worse bug: a hostile standing ON the destination
// cannot be routed around, and writing the destination off would turn a hard walk into an
// impossible one.
func TestAHostileOnTheDestinationIsNotWrittenOff(t *testing.T) {
	dest := Point{X: 2809, Z: 3441}
	b := &blockerStub{stubClient: stubClient{now: time.Unix(1000, 0)}, blocked: []Point{dest}}
	w := blockerWalker(b)
	if blocked, _ := w.hostileBlocked(dest); blocked {
		t.Fatal("wrote off a square containing the destination")
	}
	if len(w.walk.flee) != 0 {
		t.Fatalf("flee squares = %d, want 0", len(w.walk.flee))
	}
}

// Draining matters: the walker asks on every poll of the walk, and a client that kept handing
// back the same point would re-add it every time and burn the maxFlees budget in seconds.
func TestTheBlockedListIsDrainedNotRepeated(t *testing.T) {
	b := &blockerStub{stubClient: stubClient{now: time.Unix(1000, 0)}, blocked: []Point{{X: 2755, Z: 3398}}}
	w := blockerWalker(b)
	dest := Point{X: 2809, Z: 3441}
	w.hostileBlocked(dest)
	if blocked, _ := w.hostileBlocked(dest); blocked {
		t.Fatal("the same hostile was written off twice")
	}
	if w.walk.flees != 1 {
		t.Fatalf("flees = %d, want 1 -- the budget must not be spent on a repeat", w.walk.flees)
	}
}

// Already-avoided squares are skipped, the same rule avoidAttackers uses.
func TestAnAlreadyAvoidedHostileIsNotAddedAgain(t *testing.T) {
	b := &blockerStub{stubClient: stubClient{now: time.Unix(1000, 0)}, blocked: []Point{{X: 2755, Z: 3398}}}
	w := blockerWalker(b)
	w.walk.flee = []Rect{{MinX: 2740, MaxX: 2770, MinZ: 3390, MaxZ: 3410, Level: 0}}
	if blocked, _ := w.hostileBlocked(Point{X: 2809, Z: 3441}); blocked {
		t.Fatal("added a square already inside an avoided rect")
	}
}

// A Client that does not implement HostileBlocker must be untouched -- the whole point of
// keeping it a separate optional interface.
func TestAPlainHostileClientIsNotAskedToBlock(t *testing.T) {
	h := &hostileStub{stubClient: stubClient{now: time.Unix(1000, 0)}}
	w := newTestWalker(h)
	w.walk.avoidHostiles = true
	if blocked, why := w.hostileBlocked(Point{X: 1, Z: 1}); blocked || why != "" {
		t.Fatalf("hostileBlocked on a plain HostileClient = %v,%q", blocked, why)
	}
}

// The ceiling holds. A walk that gave up on hostile after hostile could otherwise fence itself
// in until nothing is reachable.
func TestGivingUpStopsAtTheFleeCeiling(t *testing.T) {
	b := &blockerStub{stubClient: stubClient{now: time.Unix(1000, 0)}}
	w := blockerWalker(b)
	w.walk.flees = maxFlees
	b.blocked = []Point{{X: 2755, Z: 3398}}
	if blocked, _ := w.hostileBlocked(Point{X: 2809, Z: 3441}); blocked {
		t.Fatal("wrote off another hostile past the maxFlees ceiling")
	}
}
