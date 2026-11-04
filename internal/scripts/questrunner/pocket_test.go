package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// A test pocket far from anything a quest walks (level 3 of an empty square).
func testPocketIn(t tile.Tile) bool {
	return t.Level == 3 && t.X >= 100 && t.X <= 110 && t.Z >= 100 && t.Z <= 110
}

func TestPocketForOnlyWhenLeavingIt(t *testing.T) {
	ps := []Pocket{{Name: "test", In: testPocketIn, Leave: func(*Deps) bool { return true }}}
	in := tile.Tile{X: 105, Z: 105, Level: 3}
	exit := tile.Tile{X: 110, Z: 110, Level: 3}
	out := tile.Tile{X: 200, Z: 200, Level: 0}
	if _, ok := pocketFor(ps, in, out); !ok {
		t.Fatal("a walk from inside to outside must leave the pocket first")
	}
	if _, ok := pocketFor(ps, in, exit); ok {
		t.Fatal("a walk to the exit, inside the pocket, is the Leave's own and is not hooked")
	}
	if _, ok := pocketFor(ps, out, in); ok {
		t.Fatal("a walk from outside is not hooked")
	}
}

// Travel's hook runs the pocket's Leave once, and not again from inside that Leave's own walks.
func TestLeavePocketForRunsLeaveOnce(t *testing.T) {
	calls := 0
	var d *Deps
	p := Pocket{Name: "test pocket", In: testPocketIn, Leave: func(dd *Deps) bool {
		calls++
		// A walk the Leave makes to a tile outside: not hooked again.
		leavePocketFor(dd, tile.Tile{X: 105, Z: 105, Level: 3}, tile.Tile{X: 1, Z: 1})
		return true
	}}
	pocketsMu.Lock()
	saved := pockets
	pockets = append(append([]Pocket(nil), saved...), p)
	pocketsMu.Unlock()
	defer func() {
		pocketsMu.Lock()
		pockets = saved
		pocketsMu.Unlock()
	}()
	var logged []string
	d = &Deps{Log: func(s string) { logged = append(logged, s) }}
	leavePocketFor(d, tile.Tile{X: 105, Z: 105, Level: 3}, tile.Tile{X: 1, Z: 1})
	if calls != 1 || d.leavingPocket || len(logged) != 1 {
		t.Fatalf("Leave ran %d time(s), leaving flag %v, log %q", calls, d.leavingPocket, logged)
	}
	if name, ok := PocketAt(tile.Tile{X: 101, Z: 109, Level: 3}); !ok || name != "test pocket" {
		t.Fatalf("PocketAt: %q %v", name, ok)
	}
	if _, ok := PocketAt(tile.Tile{X: 101, Z: 109}); ok {
		t.Fatal("level 0 is not the test pocket")
	}
}

// A ladder with nothing it can run takes the account out of the pocket it stands in, by the pocket's own way out, and not more
// than once in stuckPocketGap; an account outside every pocket is left alone.
func TestAStuckLadderTakesTheAccountOutOfThePocketItStandsIn(t *testing.T) {
	calls, result := 0, true
	p := Pocket{Name: "test pocket", In: testPocketIn, Leave: func(dd *Deps) bool {
		calls++
		if !dd.leavingPocket {
			t.Error("the pocket's own walks would be hooked again: leavingPocket is not set during Leave")
		}
		return result
	}}
	pocketsMu.Lock()
	saved := pockets
	pockets = append(append([]Pocket(nil), saved...), p)
	pocketsMu.Unlock()
	defer func() {
		pocketsMu.Lock()
		pockets = saved
		pocketsMu.Unlock()
	}()
	sink := &logSink{}
	e := fakeEngine0(sink)
	in := tile.Tile{X: 105, Z: 105, Level: 3}
	if e.leaveStuckPocket(tile.Tile{X: 200, Z: 200}) || calls != 0 {
		t.Fatalf("an account outside every pocket: %d calls", calls)
	}
	if !e.leaveStuckPocket(in) || calls != 1 || !sink.has("stuck while standing in test pocket") || e.D.leavingPocket {
		t.Fatalf("first ask: %d calls, leaving %v: %v", calls, e.D.leavingPocket, sink.lines)
	}
	if e.leaveStuckPocket(in) || calls != 1 {
		t.Fatalf("a second ask inside the gap ran Leave again: %d calls", calls)
	}
	e.lastStuckPocket = time.Now().Add(-stuckPocketGap - time.Second)
	result = false
	if e.leaveStuckPocket(in) || calls != 2 || !sink.has("could not leave test pocket") {
		t.Fatalf("after the gap: %d calls: %v", calls, sink.lines)
	}
}
