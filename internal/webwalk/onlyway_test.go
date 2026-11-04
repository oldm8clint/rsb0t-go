package webwalk

import (
	"strings"
	"testing"
	"time"
)

// doorEdge is house()'s door, leaving the west room for the east yard.
func doorEdge(g *Graph) *Edge {
	for i := range g.Edges {
		if g.Edges[i].LocID == 1530 && g.Edges[i].From == (Point{X: 4, Z: 2}) {
			return &g.Edges[i]
		}
	}
	return nil
}

// TestARefusedBarrierThatIsTheOnlyWayOutStaysInThePlan is the rule that 68 accounts needed on
// 2026-09-22. They were converted to ScarabSwarm while standing inside the Fishing Guild, a
// sealed room whose only exits are the doors 2025 at 2611,3394 and 1530 at 2611,3398. Door 2025
// refused 98 crossings and answered 97 in the same hour, so three refusals in a row is ordinary
// luck — and three refusals used to ban the placement for the Walker's LIFE. Every later plan
// then answered "unreachable from here (best 111 tiles)" for a 112-tile walk, forever, and the
// account never touched the door again.
//
// Routing around a barrier is only ever an answer when there is somewhere to route to.
func TestARefusedBarrierThatIsTheOnlyWayOutStaysInThePlan(t *testing.T) {
	_, w, g := house(t, Point{X: 1, Z: 2}) // inside the sealed west room
	e := doorEdge(g)
	if e == nil {
		t.Fatal("house() no longer has the door this test is about")
	}
	key := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	dest := Point{X: 9, Z: 2} // the yard, reachable only through that door
	avoid := map[Point]bool{}
	opt := w.searchOptions(Options{}, avoid)
	for i := 0; i < doorBanStrikes+2; i++ {
		w.noteRefusal(e, avoid, dest, opt)
	}
	if w.strikes[key] < doorBanStrikes {
		t.Fatalf("the door took only %d strikes; this test needs %d to say anything", w.strikes[key], doorBanStrikes)
	}
	if avoid[key] {
		t.Errorf("the only door out of the room was excluded from this walk's plans, which makes %s unreachable and leaves the account with nothing to try", dest)
	}
	if w.Banned(key) {
		t.Errorf("the only door out of the room was banned; that is what stranded 68 accounts inside the Fishing Guild")
	}
}

// TestARefusedBarrierWithAWayAroundIsStillAvoidedAndBanned is the control: the guard above must
// not have turned the strike machinery off. With a second door in the wall, routing around the
// refusing one is a real answer, so it is taken.
func TestARefusedBarrierWithAWayAroundIsStillAvoidedAndBanned(t *testing.T) {
	f, _, g := house(t, Point{X: 1, Z: 2})
	// A second door further south, as TestWalkerRoutesAroundARefusingDoor builds one.
	f.plan.blocked[[2]Point{{X: 4, Z: 0}, {X: 5, Z: 0}}] = true
	f.addDoor(Point{X: 4, Z: 0}, Point{X: 5, Z: 0}, 1600)
	g.Edges = append(g.Edges,
		Edge{From: Point{X: 4, Z: 0}, To: Point{X: 5, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
		Edge{From: Point{X: 5, Z: 0}, To: Point{X: 4, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
	)
	w2 := NewWalker(NewPlanner(f.plan, g), f)
	w2.Log = func(s string) { t.Log(s) }
	e := doorEdge(g)
	key := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	dest := Point{X: 9, Z: 2}
	avoid := map[Point]bool{}
	opt := w2.searchOptions(Options{}, avoid)
	for i := 0; i < doorBanStrikes; i++ {
		w2.noteRefusal(e, avoid, dest, opt)
	}
	if !avoid[key] {
		t.Errorf("a door that refused %d crossings with a second door in the same wall was not routed around", doorBanStrikes)
	}
	if !w2.Banned(key) {
		t.Errorf("a door that refused %d crossings with a way around it was not banned; the strike machinery has been turned off, not narrowed", doorBanStrikes)
	}
}

// TestABarrierBanLapses is the belt to the guard's braces. onlyWayTo asks the planner, and a
// planner can be wrong about the live world — a door that is the only way out of a room the
// account has since left, a destination that moved. A ban that never lapses turns any such
// mistake into a permanently stranded account, which is the failure this whole file is about,
// so it lapses.
func TestABarrierBanLapses(t *testing.T) {
	f, _, g := house(t, Point{X: 1, Z: 2})
	f.plan.blocked[[2]Point{{X: 4, Z: 0}, {X: 5, Z: 0}}] = true
	f.addDoor(Point{X: 4, Z: 0}, Point{X: 5, Z: 0}, 1600)
	g.Edges = append(g.Edges,
		Edge{From: Point{X: 4, Z: 0}, To: Point{X: 5, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
	)
	w2 := NewWalker(NewPlanner(f.plan, g), f)
	w2.Log = func(s string) { t.Log(s) }
	e := doorEdge(g)
	key := Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}
	avoid := map[Point]bool{}
	for i := 0; i < doorBanStrikes; i++ {
		w2.noteRefusal(e, avoid, Point{X: 9, Z: 2}, w2.searchOptions(Options{}, avoid))
	}
	if !w2.Banned(key) {
		t.Fatalf("the door was not banned after %d strikes, so this test cannot say whether the ban lapses", doorBanStrikes)
	}
	f.now = f.now.Add(doorBanFor - time.Second)
	if !w2.Banned(key) {
		t.Errorf("the ban lapsed before %v, so a barrier that genuinely will not open comes straight back into the plans", doorBanFor)
	}
	f.now = f.now.Add(2 * time.Second)
	if w2.Banned(key) {
		t.Errorf("the ban outlived %v; a ban that never ends is how an account stays stranded forever", doorBanFor)
	}
	if opt := w2.searchOptions(Options{}, map[Point]bool{}); opt.Avoid[key] {
		t.Errorf("the lapsed ban is still being fed to the planner as an avoid")
	}
}

// TestAnOnlyWayThatNeverCrossesEndsTheWalkInsteadOfSpinning is acct18 on 5 Oct 2026: inside Tree
// Gnome Village with its Spirit Tree, the only way out in the graph, "not interactable", the
// walker planned the same 123,230-node route and its onlyWayTo probe about five times a second.
// The barrier stays in the plan (the rule above), but from onlyWayGiveUp refusals in a row the
// walk fails with that reason and waits onlyWayPause first, so a walk's tries are few and slow.
func TestAnOnlyWayThatNeverCrossesEndsTheWalkInsteadOfSpinning(t *testing.T) {
	f, w, _ := house(t, Point{X: 1, Z: 2}) // inside the sealed west room
	f.doors[0].refuse = true               // never opens
	var lines []string
	w.Log = func(s string) { lines = append(lines, s) }
	start := f.now
	res := w.WalkTo(Point{X: 9, Z: 2}, WalkOptions{Timeout: 10 * time.Minute})
	if res.Arrived() {
		t.Fatalf("arrived through a door that never opens: %+v", res)
	}
	key := Point{X: 4, Z: 2}
	if w.Banned(key) {
		t.Fatal("the only door out was banned")
	}
	gaveUp := 0
	for _, l := range lines {
		if strings.Contains(l, "refused") && strings.Contains(l, "crossings in a row") {
			gaveUp++
		}
	}
	if gaveUp == 0 {
		t.Fatalf("the walk never gave up on the refusing only way:\n%s", strings.Join(lines, "\n"))
	}
	tries := w.strikes[key]
	elapsed := f.now.Sub(start)
	// Without the pause a ten-minute walk tried the door every tick or two.
	if perMinute := float64(tries) / elapsed.Minutes(); perMinute > 10 {
		t.Fatalf("%d tries in %v (%.1f a minute): still spinning", tries, elapsed, perMinute)
	}
	t.Logf("%d tries in %v, %d give-ups", tries, elapsed, gaveUp)
}

func TestOnlyWayPause(t *testing.T) {
	for n, want := range map[int]time.Duration{
		onlyWayGiveUp:      5 * time.Second,
		onlyWayGiveUp + 1:  10 * time.Second,
		onlyWayGiveUp + 11: time.Minute,
		onlyWayGiveUp + 50: time.Minute,
	} {
		if got := onlyWayPause(n); got != want {
			t.Errorf("onlyWayPause(%d) = %v, want %v", n, got, want)
		}
	}
}
