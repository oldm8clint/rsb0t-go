package hops

import (
	"testing"
	"time"

	"rsb0t-go/internal/webwalk"
)

// stepWorld is the smallest webwalk.Client that can exercise finishOnLandingTile: it holds a
// tile and moves onto a walk's destination only after the Nth click, which is how content's own
// three-tick inviswall behaves at the Al Kharid toll gate.
type stepWorld struct {
	me        webwalk.Point
	walks     int
	moveOnNth int // 0 means never move
	now       time.Time
}

func (w *stepWorld) Tile() (webwalk.Point, bool) { return w.me, true }

func (w *stepWorld) Walk(path []webwalk.Point, run bool) error {
	w.walks++
	if w.moveOnNth > 0 && w.walks >= w.moveOnNth {
		w.me = path[len(path)-1]
	}
	return nil
}

func (w *stepWorld) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch { return nil }
func (w *stepWorld) OpLoc(op, x, z, id int) error                   { return nil }
func (w *stepWorld) CantReachSince(mark time.Time) bool             { return false }
func (w *stepWorld) Now() time.Time                                 { return w.now }

func (w *stepWorld) Delay(d time.Duration) bool {
	w.now = w.now.Add(d)
	return true
}

// tollEdge is the southern leaf as the graph now carries it: operated from 3267, landing one
// tile BEYOND the gate square at 3269. The server drops the payer on 3268 in between.
func tollEdge() *webwalk.Edge {
	return &webwalk.Edge{
		Kind: webwalk.KindDoor, LocName: "Gate", Action: "Open", LocID: 2882,
		LocX: 3268, LocZ: 3227,
		From:    webwalk.Point{X: 3267, Z: 3227},
		To:      webwalk.Point{X: 3269, Z: 3227},
		Special: &webwalk.Special{Label: "Al Kharid toll gate", Choose: []string{"Yes, ok."}},
	}
}

func newStepHandler(w *stepWorld) *Handler {
	return New(Deps{Client: w, Log: func(string) {}})
}

// The ordinary case: the toll drops the payer on the gate square and the hop steps off it.
func TestFinishOnLandingTileStepsOffTheGateSquare(t *testing.T) {
	w := &stepWorld{me: webwalk.Point{X: 3268, Z: 3227}, moveOnNth: 1}
	if !newStepHandler(w).finishOnLandingTile(tollEdge(), "toll") {
		t.Fatalf("did not step off the gate square; ended at %s", w.me)
	}
	if w.me != (webwalk.Point{X: 3269, Z: 3227}) {
		t.Errorf("ended at %s, want the landing 3269,3227", w.me)
	}
}

// The barrier case, and the reason the click is re-issued rather than sent once: content walls
// all four sides of the landing square for three ticks, so the first clicks do nothing.
func TestFinishOnLandingTileOutlastsTheBarrier(t *testing.T) {
	w := &stepWorld{me: webwalk.Point{X: 3268, Z: 3227}, moveOnNth: 3}
	if !newStepHandler(w).finishOnLandingTile(tollEdge(), "toll") {
		t.Fatalf("gave up before the barrier expired; ended at %s after %d clicks", w.me, w.walks)
	}
	if w.walks < 2 {
		t.Errorf("only %d clicks sent; a single click cannot outlast a three-tick barrier", w.walks)
	}
}

// The regression this whole fix exists for. Paying again from the gate square teleports the
// player BACK west, two tiles from the landing. That must read as a failure, so the walker
// routes around the crossing instead of paying the toll again — the old radius-of-two arrival
// check called this success, which is how one run paid 953 tolls.
func TestFinishOnLandingTileRejectsTheWrongWayShuttle(t *testing.T) {
	w := &stepWorld{me: webwalk.Point{X: 3267, Z: 3227}, moveOnNth: 1}
	if newStepHandler(w).finishOnLandingTile(tollEdge(), "toll") {
		t.Fatal("a crossing that ended two tiles from its landing reported success")
	}
	if w.walks != 0 {
		t.Errorf("sent %d clicks from a tile that is not beside the landing; it should refuse outright", w.walks)
	}
}

// Already standing on the landing is a no-op, not another click.
func TestFinishOnLandingTileIsANoOpWhenAlreadyThere(t *testing.T) {
	w := &stepWorld{me: webwalk.Point{X: 3269, Z: 3227}}
	if !newStepHandler(w).finishOnLandingTile(tollEdge(), "toll") {
		t.Fatal("standing on the landing did not count as arrived")
	}
	if w.walks != 0 {
		t.Errorf("sent %d clicks while already on the landing", w.walks)
	}
}

// The tile a hop is operated FROM is never a landing, and this is a regression test for a defect
// a live run caught rather than a hypothetical.
//
// Moving the toll edge to land one tile beyond the gate put its From (3267) exactly
// arrivalRadius from its To (3269). drive's first landing check runs immediately after the op and
// BEFORE the dialogue is answered, so it reported the crossing complete while the player stood
// still and the toll went unpaid — the live log read "arrived at 3269,3227" and then "ended at
// 3267,3227, which is not beside the landing", twice per leaf, until the repath budget was gone.
func TestLandedNeverAcceptsTheTileTheHopStartsFrom(t *testing.T) {
	e := tollEdge()
	for _, c := range []struct {
		at   webwalk.Point
		want bool
		why  string
	}{
		{webwalk.Point{X: 3267, Z: 3227}, false, "the From tile, before the toll is even paid"},
		{webwalk.Point{X: 3268, Z: 3227}, true, "the gate square the server actually teleports the payer to"},
		{webwalk.Point{X: 3269, Z: 3227}, true, "the landing itself"},
	} {
		w := &stepWorld{me: c.at}
		if got := newStepHandler(w).landed(e); got != c.want {
			t.Errorf("landed(at %s) = %v, want %v — %s", c.at, got, c.want, c.why)
		}
	}
}

// A step that never lands has to end, and end as a failure rather than spinning.
func TestFinishOnLandingTileGivesUpWhenItCannotStep(t *testing.T) {
	w := &stepWorld{me: webwalk.Point{X: 3268, Z: 3227}, moveOnNth: 0}
	if newStepHandler(w).finishOnLandingTile(tollEdge(), "toll") {
		t.Fatal("reported success without ever reaching the landing")
	}
	if w.walks == 0 {
		t.Error("never even tried to click")
	}
}
