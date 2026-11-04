package hops

import (
	"testing"
	"time"

	"rsb0t-go/internal/webwalk"
)

// walkWorld is the minimum webwalk.Client approach needs: a player that takes one step a tick
// toward the last point of whatever it was told to walk to, and a clock Delay advances.
// findNPC is never reached, so no session is needed.
//
// A click costs ONE TICK before the first step, and that is the whole point of the model rather
// than a detail. A movement click is sent, the server acts on it on its next cycle, and the new
// position only reaches this client in the PLAYER_INFO after that. So a caller that clicks a
// square and then waits a single tick sees the player exactly where it was — which is why
// dividing a six-second budget among nine candidate squares moved a live account nowhere at
// all, however many squares it tried.
type walkWorld struct {
	me webwalk.Point
	// dest is where the last click aimed, and pending is its unspent tick of latency.
	dest    *webwalk.Point
	pending bool
	now     time.Time
	walks   int
}

func newWalkWorld(me webwalk.Point) *walkWorld {
	return &walkWorld{me: me, now: time.Unix(1000, 0)}
}

func (f *walkWorld) Tile() (webwalk.Point, bool) { return f.me, true }

func (f *walkWorld) Walk(path []webwalk.Point, run bool) error {
	f.walks++
	d := path[len(path)-1]
	f.dest, f.pending = &d, true
	return nil
}

func (f *walkWorld) FindLocs(webwalk.LocQuery) []webwalk.LocMatch { return nil }
func (f *walkWorld) OpLoc(op, x, z, id int) error                 { return nil }
func (f *walkWorld) CantReachSince(time.Time) bool                { return false }
func (f *walkWorld) Now() time.Time                               { return f.now }

func (f *walkWorld) Delay(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	f.now = f.now.Add(d)
	if f.pending {
		f.pending = false // the click's round trip; no step this tick
		return true
	}
	if f.dest == nil || *f.dest == f.me {
		return true
	}
	f.me = webwalk.Point{X: f.me.X + sign(f.dest.X-f.me.X), Z: f.me.Z + sign(f.dest.Z-f.me.Z), Level: f.me.Level}
	return true
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// TestApproachReachesAnNpcSeveralTilesAway is the live failure, reproduced at the tiles it
// happened on. Measured 2026-09-22 (run/fleet.log, 269 accounts): "Ardougne->Brimhaven ship:
// could not get beside Captain Barnaby" 1,063 times against 74 arrivals on that ship, which is
// why a Stage 3 account in East Ardougne could not reach Varrock — the boat chain is the only
// wolf-free route east. The old budget gave each of approachTargets' nine candidate squares
// approachFor/9 = 666ms, barely one 600ms tick, and re-clicked a new square at the end of each
// slice, so an account four to nine tiles from Barnaby's wandering position shuffled on the spot
// until the whole budget was gone.
//
// One tile a tick is the honest floor: a running account arrives sooner.
func TestApproachReachesAnNpcSeveralTilesAway(t *testing.T) {
	stand := webwalk.Point{X: 2683, Z: 3272} // the edge's baked stand
	// Every distinct tile the live log names Barnaby standing on, worst ones first.
	for _, npc := range []webwalk.Point{
		{X: 2682, Z: 3276}, {X: 2677, Z: 3276}, {X: 2674, Z: 3270}, {X: 2674, Z: 3276},
	} {
		f := newWalkWorld(stand)
		h := New(Deps{Client: f, Log: func(s string) { t.Log(s) }})
		if !h.approach(npc, nil) {
			t.Errorf("could not get beside an npc at %s from the stand %s in %v (ended at %s, %d clicks); this is the live Captain Barnaby failure",
				npc, stand, approachFor, f.me, f.walks)
		}
	}
}

// TestApproachFollowsAWanderingNpc: Barnaby paces his dock, and aiming once at where he stood
// when the hop started is not enough. relocate is what npcHop passes so the walk re-aims.
func TestApproachFollowsAWanderingNpc(t *testing.T) {
	f := newWalkWorld(webwalk.Point{X: 2683, Z: 3272})
	h := New(Deps{Client: f, Log: func(s string) { t.Log(s) }})
	npc := webwalk.Point{X: 2677, Z: 3276}
	moves := 0
	relocate := func() (webwalk.Point, bool) {
		// Step him one tile west every third poll, five times, then let him stand.
		if moves < 15 && moves%3 == 0 {
			npc = webwalk.Point{X: npc.X - 1, Z: npc.Z, Level: npc.Level}
		}
		moves++
		return npc, true
	}
	if !h.approach(npc, relocate) {
		t.Errorf("could not get beside an npc that walked away while we approached (ended at %s, npc at %s, %d clicks)", f.me, npc, f.walks)
	}
}

// TestApproachBudgetIsNotOneTick pins the arithmetic the old code got wrong, without needing a
// world: whatever approachTargets offers, one candidate gets enough time to be walked to.
func TestApproachBudgetIsNotOneTick(t *testing.T) {
	from := webwalk.Point{X: 2683, Z: 3272}
	for _, dest := range []webwalk.Point{
		{X: 2683, Z: 3273}, // one tile
		{X: 2679, Z: 3276}, // four
		{X: 2674, Z: 3276}, // nine
	} {
		d := webwalk.Chebyshev(from, dest)
		got := approachBudget(from, dest)
		if got < time.Duration(d)*tick {
			t.Errorf("a %d-tile candidate gets %v, which is less than the %v it takes to walk there at one tile a tick", d, got, time.Duration(d)*tick)
		}
		if got < approachMinPerTarget {
			t.Errorf("a %d-tile candidate gets %v, below the floor %v", d, got, approachMinPerTarget)
		}
	}
	// The bug in one line: the old budget was approachFor divided by the candidate count.
	if old := approachFor / 9; old >= 2*tick {
		t.Logf("note: approachFor/9 is now %v, so the old arithmetic would no longer be one tick either", old)
	}
}

// oldApproach is the loop this package had before 2026-09-22, reproduced exactly: a six-second
// budget divided equally among approachTargets' nine candidate squares, the candidate list
// computed once from where the player started, and no re-aim at a wandering npc.
//
// It is here as the control for TestApproachReachesAnNpcSeveralTilesAway. A test that passes on
// code that was already working proves nothing, and docs/trustworthy_harnesses.md is a list of
// the times this repo shipped exactly that.
func oldApproach(h *Handler, at webwalk.Point) bool {
	const oldApproachFor = 6 * time.Second
	me, ok := h.me()
	if !ok || webwalk.Chebyshev(me, at) <= 1 {
		return true
	}
	targets := approachTargets(me, at)
	perTarget := oldApproachFor / time.Duration(len(targets))
	deadline := h.d.Client.Now().Add(oldApproachFor)
	for _, dest := range targets {
		if !h.d.Client.Now().Before(deadline) {
			break
		}
		from, ok := h.me()
		if !ok {
			return false
		}
		if webwalk.Chebyshev(from, at) <= 1 {
			return true
		}
		if err := h.d.Client.Walk([]webwalk.Point{from, dest}, false); err != nil {
			continue
		}
		end := h.d.Client.Now().Add(perTarget)
		if end.After(deadline) {
			end = deadline
		}
		for h.d.Client.Now().Before(end) {
			if !h.delay(tick) {
				return false
			}
			if cur, ok := h.me(); ok && webwalk.Chebyshev(cur, at) <= 1 {
				return true
			}
		}
	}
	cur, _ := h.me()
	return webwalk.Chebyshev(cur, at) <= 1
}

// TestTheOldApproachMissedTheSameNpc is that control. Barnaby's furthest observed tile is nine
// from the edge's stand, and nine steps cannot happen inside six seconds of 600ms ticks however
// they are divided.
func TestTheOldApproachMissedTheSameNpc(t *testing.T) {
	stand := webwalk.Point{X: 2683, Z: 3272}
	npc := webwalk.Point{X: 2674, Z: 3276} // 151 of the live failures name this tile
	f := newWalkWorld(stand)
	h := New(Deps{Client: f, Log: func(s string) { t.Log(s) }})
	if oldApproach(h, npc) {
		t.Fatalf("the control reached the npc under the OLD budget (ended at %s after %d clicks), so TestApproachReachesAnNpcSeveralTilesAway is not testing the fix", f.me, f.walks)
	}
	t.Logf("old budget: ended at %s, %d tiles short, after %d clicks", f.me, webwalk.Chebyshev(f.me, npc), f.walks)
}
