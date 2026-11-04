package webwalk

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// laneClient is the least Client a lane needs: a clock Delay advances, a tile, and a barrier that offers its op or not.
type laneClient struct {
	now    time.Time
	me     Point
	delays int
	// stopAt, when set, makes the stopAt-th Delay report a stop request.
	stopAt int
	// step is how far each Delay moves the clock (the Delay's own argument when zero).
	step time.Duration
	// sleep makes a Delay take real time too, so goroutines sharing a lane interleave.
	sleep time.Duration
	// absent, when set, says the barrier offers nothing to click on that Delay count (another account's open window).
	absent func(delays int) bool
	// onDelay runs after each Delay.
	onDelay func(delays int)
}

func (c *laneClient) Tile() (Point, bool)                { return c.me, true }
func (c *laneClient) Walk(path []Point, run bool) error  { return nil }
func (c *laneClient) OpLoc(op, x, z, id int) error       { return nil }
func (c *laneClient) CantReachSince(mark time.Time) bool { return false }
func (c *laneClient) Now() time.Time                     { return c.now }
func (c *laneClient) FindLocs(q LocQuery) []LocMatch {
	if c.absent != nil && c.absent(c.delays) {
		return nil
	}
	return []LocMatch{{ID: q.IDs[0], X: q.At.X, Z: q.At.Z, Level: q.At.Level, Op: 1}}
}

func (c *laneClient) Delay(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	c.delays++
	if c.stopAt > 0 && c.delays >= c.stopAt {
		return false
	}
	if c.step > 0 {
		d = c.step
	}
	c.now = c.now.Add(d)
	if c.sleep > 0 {
		time.Sleep(c.sleep)
	}
	if c.onDelay != nil {
		c.onDelay(c.delays)
	}
	return true
}

// The queued barriers as the walk graph has them.
func grandTreeLeft() *Edge {
	return &Edge{From: Point{X: 2465, Z: 3491}, To: Point{X: 2465, Z: 3493}, Kind: KindDoor, Cost: 4, LocName: "Tree Door", Action: "Open", LocID: 1967, OpenLocID: 1969, LocX: 2464, LocZ: 3492}
}

func grandTreeLeftBack() *Edge {
	e := grandTreeLeft()
	e.From, e.To = e.To, e.From
	return e
}

func grandTreeRight() *Edge {
	return &Edge{From: Point{X: 2466, Z: 3491}, To: Point{X: 2466, Z: 3493}, Kind: KindDoor, Cost: 4, LocName: "Tree Door", Action: "Open", LocID: 1968, OpenLocID: 1970, LocX: 2466, LocZ: 3492}
}

func grandTreeRightBack() *Edge {
	e := grandTreeRight()
	e.From, e.To = e.To, e.From
	return e
}

func gnomeGate() *Edge {
	return &Edge{From: Point{X: 2461, Z: 3382}, To: Point{X: 2461, Z: 3385}, Kind: KindGate, Cost: 4, LocName: "Gate", Action: "Open", LocID: 190, OpenLocID: -1, LocX: 2459, LocZ: 3383,
		Special: &Special{Label: "Gnome Stronghold gate (Femi boxes)", Choose: []string{"OK then"}}}
}

// lanesEmpty fails the test if a lane is still occupied, and clears every lane so one test's leak cannot fail the next.
func lanesEmpty(t *testing.T) {
	t.Helper()
	barrierLanes.mu.Lock()
	defer barrierLanes.mu.Unlock()
	for k, q := range barrierLanes.lanes {
		t.Errorf("lane %v still holds %d turn(s)", k, len(q))
	}
	barrierLanes.lanes = map[laneKey][]*laneTurn{}
}

func waitLaneDepth(t *testing.T, e *Edge, depth int) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if LaneDepth(e) == depth {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the lane never reached %d (it is %d)", depth, LaneDepth(e))
}

// holdLane takes every turn of e's barrier on an empty lane, and returns them: the barrier is full.
func holdLane(t *testing.T, e *Edge) []*LaneTurn {
	t.Helper()
	var turns []*LaneTurn
	for i := 0; i < LaneCapacity(e); i++ {
		turn, _, stopped := EnterLane(&laneClient{me: e.From}, e, nil)
		if turn == nil || stopped {
			t.Fatalf("turn %d of %d on an empty lane: %v, stopped %v", i+1, LaneCapacity(e), turn, stopped)
		}
		turns = append(turns, turn)
	}
	return turns
}

func leaveAll(turns []*LaneTurn) {
	for _, turn := range turns {
		turn.Leave()
	}
}

// The capacities are the bursts the 16 Oct 2026 wave shows (queue.go has the counts), so they are pinned here: a lane of one
// is the serial line that replay showed halving the throughput of the wave it was written for.
func TestTheBarriersTakeTheBurstTheLogShows(t *testing.T) {
	want := map[int]int{190: 3, 1967: 2, 1968: 2}
	if len(queuedBarriers) != len(want) {
		t.Fatalf("%d queued barriers, want %d", len(queuedBarriers), len(want))
	}
	for _, b := range queuedBarriers {
		if b.Capacity != want[b.LocID] {
			t.Errorf("%s (loc %d) takes %d at a time, want %d", b.Name, b.LocID, b.Capacity, want[b.LocID])
		}
	}
	if LaneCapacity(gnomeGate()) != 3 || LaneCapacity(grandTreeLeft()) != 2 || LaneCapacity(grandTreeRightBack()) != 2 {
		t.Errorf("LaneCapacity: gate %d, left %d, right %d", LaneCapacity(gnomeGate()), LaneCapacity(grandTreeLeft()), LaneCapacity(grandTreeRightBack()))
	}
	if laneMaxWait > 3*time.Minute {
		t.Errorf("a walker waits %v in line: a Barcrawl run passes four barriers inside a slot its slowest runs already fill", laneMaxWait)
	}
}

// Every queued barrier is a hop of the real walk graph, placed where the table says, so the table cannot drift from the
// edges it is for.
func TestEveryQueuedBarrierIsAHopOfTheDefaultGraph(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]int{}
	for i := range g.Edges {
		e := &g.Edges[i]
		if b := barrierSpecOf(e); b != nil {
			seen[b.LocID]++
			if !QueuedBarrier(e) {
				t.Errorf("QueuedBarrier disagrees with barrierSpecOf for %s", e.String())
			}
		}
	}
	for _, b := range queuedBarriers {
		if seen[b.LocID] < 2 {
			t.Errorf("%s (loc %d at %d,%d) has %d edge(s) in the graph, want both directions", b.Name, b.LocID, b.X, b.Z, seen[b.LocID])
		}
		if b.Capacity < 1 {
			t.Errorf("%s takes %d at a time", b.Name, b.Capacity)
		}
		if b.TwinLocID != 0 {
			twinned := false
			for _, o := range queuedBarriers {
				twinned = twinned || (o.LocID == b.TwinLocID && o.TwinLocID == b.LocID && o.TwinShift == -b.TwinShift)
			}
			if !twinned {
				t.Errorf("%s names twin %d, which does not name it back with the opposite shift", b.Name, b.TwinLocID)
			}
		}
	}
}

// The line is first come first served, a barrier's capacity at a time. Six accounts reach the Grand Tree's left door while it
// is full; they cross in the order they came (within the batch granted together) and never more than the door takes at once,
// and it does take that many at once.
func TestTheFleetQueuesAtABarrierInOrderCapacityAtATime(t *testing.T) {
	defer lanesEmpty(t)
	e := grandTreeLeft()
	capacity := LaneCapacity(e)
	holders := holdLane(t, e)
	const n = 6
	var mu sync.Mutex
	var order []int
	inside, most := 0, 0
	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			turn, _, stopped := EnterLane(&laneClient{me: e.From, sleep: time.Millisecond}, e, nil)
			if stopped || turn == nil {
				t.Errorf("account %d: turn %v, stopped %v", i, turn, stopped)
				return
			}
			mu.Lock()
			order = append(order, i)
			inside++
			most = max(most, inside)
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			turn.Leave()
		}(i)
		waitLaneDepth(t, e, capacity+i) // it is in line before the next arrives
	}
	leaveAll(holders)
	wg.Wait()
	for pos, got := range order {
		if got-1-pos >= capacity || pos-(got-1) >= capacity {
			t.Fatalf("crossed in order %v, want 1..%d (accounts granted together may swap)", order, n)
		}
	}
	if most != capacity {
		t.Errorf("%d accounts were crossing at once, want the door's %d", most, capacity)
	}
}

// The capacity is a ceiling: the account after the last place waits for one of them to be given up, and takes it.
func TestAFullBarrierMakesTheNextAccountWait(t *testing.T) {
	defer lanesEmpty(t)
	e := gnomeGate()
	holders := holdLane(t, e)
	if d := LaneDepth(e); d != LaneCapacity(e) {
		t.Fatalf("the lane holds %d, want %d", d, LaneCapacity(e))
	}
	got := make(chan *LaneTurn, 1)
	go func() {
		turn, _, _ := EnterLane(&laneClient{me: e.From, sleep: time.Millisecond}, e, nil)
		got <- turn
	}()
	waitLaneDepth(t, e, LaneCapacity(e)+1)
	select {
	case <-got:
		t.Fatal("the account after the last place crossed at once")
	case <-time.After(30 * time.Millisecond):
	}
	holders[1].Leave() // not the first in line: any place in the lane frees one
	select {
	case turn := <-got:
		if turn == nil {
			t.Fatal("the waiting account got no turn")
		}
		turn.Leave()
	case <-time.After(3 * time.Second):
		t.Fatal("the waiting account never got the place that was freed")
	}
	leaveAll(holders)
}

// A walker the bot stops while it is in line gives its place up, and the line moves on.
func TestAStoppedWalkerGivesUpItsPlaceInLine(t *testing.T) {
	defer lanesEmpty(t)
	e := gnomeGate()
	holders := holdLane(t, e)
	waiter := &laneClient{me: e.From, stopAt: 4}
	turn, _, stopped := EnterLane(waiter, e, nil)
	if turn != nil || !stopped {
		t.Fatalf("a stopped walker: turn %v, stopped %v", turn, stopped)
	}
	if d := LaneDepth(e); d != len(holders) {
		t.Errorf("the lane holds %d after the stop, want only the %d crossing", d, len(holders))
	}
	leaveAll(holders)
	leaveAll(holders) // twice is safe
}

// Nothing holds a barrier for the whole fleet: a turn that outlives its lease is taken over by the next in line, and only
// that one; a holder inside its lease keeps its place.
func TestATurnThatOutlivesItsLeaseIsTakenOver(t *testing.T) {
	defer lanesEmpty(t)
	base := time.Unix(5000, 0)
	cur := base
	laneClock = func() time.Time { return cur }
	defer func() { laneClock = time.Now }()

	e := grandTreeRight()
	stuck, _, _ := EnterLane(&laneClient{me: e.From}, e, nil)
	cur = base.Add(30 * time.Second)
	fresh, _, _ := EnterLane(&laneClient{me: e.From}, e, nil)
	if stuck == nil || fresh == nil || LaneCapacity(e) != 2 {
		t.Fatalf("turns %v and %v on an empty lane of %d", stuck, fresh, LaneCapacity(e))
	}
	// The first holder is 46 s old when the wait reaches its fifth tick, the second 16 s.
	waiter := &laneClient{me: e.From, onDelay: func(n int) {
		if n == 5 {
			cur = base.Add(laneLease + time.Second)
		}
	}}
	turn, _, stopped := EnterLane(waiter, e, nil)
	if stopped || turn == nil {
		t.Fatalf("the next in line never got the place of a holder %v past its lease: turn %v stopped %v", laneLease, turn, stopped)
	}
	if waiter.delays < 5 {
		t.Errorf("the turn was taken after %d waits, before the lease ran out", waiter.delays)
	}
	stuck.Leave() // the old holder's late Leave must not take a new one's place
	if d := LaneDepth(e); d != 2 {
		t.Errorf("the lane holds %d after the stale holder left, want the other holder and the new one", d)
	}
	// The place of the holder still inside its lease is not for the taking.
	late := &laneClient{me: e.From, stopAt: 4}
	if again, _, stopped := EnterLane(late, e, nil); again != nil || !stopped {
		t.Errorf("an account took a place from a holder inside its lease: turn %v, stopped %v", again, stopped)
	}
	fresh.Leave()
	turn.Leave()
}

// A line that does not move is not waited in for ever: after laneMaxWait the account crosses without a turn, as it did
// before there was a line.
func TestALineThatDoesNotMoveIsGivenUp(t *testing.T) {
	defer lanesEmpty(t)
	e := grandTreeLeft()
	holders := holdLane(t, e)
	defer leaveAll(holders)
	var logs []string
	waiter := &laneClient{me: e.From, step: time.Minute}
	turn, waited, stopped := EnterLane(waiter, e, func(s string) { logs = append(logs, s) })
	if turn != nil || stopped {
		t.Fatalf("turn %v, stopped %v: want the walker released without a turn", turn, stopped)
	}
	if waited < laneMaxWait || waited > laneMaxWait+2*time.Minute {
		t.Errorf("waited %v, want about %v", waited, laneMaxWait)
	}
	if d := LaneDepth(e); d != len(holders) {
		t.Errorf("the lane holds %d, want the holders only", d)
	}
	if len(logs) == 0 || !strings.Contains(logs[len(logs)-1], "crossing without one") {
		t.Errorf("log %v does not say it crossed without a turn", logs)
	}
}

// The account that gives up on the line still does not click on a barrier another crossing has open: it waits for the barrier
// to be back first, which the head of a line it has left depends on (its click would otherwise find nothing again).
func TestAnAccountThatGaveUpTheLineStillWaitsForTheBarrier(t *testing.T) {
	defer lanesEmpty(t)
	e := gnomeGate()
	holders := holdLane(t, e)
	defer leaveAll(holders)
	waiter := &laneClient{me: e.From, step: time.Minute, absent: func(int) bool { return true }}
	turn, _, stopped := EnterLane(waiter, e, nil)
	if turn != nil || stopped {
		t.Fatalf("turn %v, stopped %v", turn, stopped)
	}
	// laneMaxWait minutes of waiting in line, then at least one more wait for the barrier.
	if min := int(laneMaxWait/time.Minute) + 1; waiter.delays < min {
		t.Errorf("%d waits: it gave up the line and went straight to the barrier (want at least %d)", waiter.delays, min)
	}
	stop := &laneClient{me: e.From, step: time.Minute, absent: func(int) bool { return true }, stopAt: int(laneMaxWait/time.Minute) + 1}
	if _, _, stopped := EnterLane(stop, e, nil); !stopped {
		t.Error("a stop request while waiting for the barrier was not reported")
	}
}

// An account carried away while it waits (a random event, a teleport) is no longer the account that joined: it gives its
// place up at once instead of holding the line, and crosses without a turn when it comes back to the door.
func TestAnAccountCarriedAwayWhileInLineGivesItsPlaceUp(t *testing.T) {
	defer lanesEmpty(t)
	e := grandTreeLeft()
	holders := holdLane(t, e)
	defer leaveAll(holders)
	waiter := &laneClient{me: e.From}
	var logs []string
	waiter.onDelay = func(n int) {
		if n == 3 {
			waiter.me = Point{X: 3222, Z: 3218} // Lumbridge
		}
	}
	turn, _, stopped := EnterLane(waiter, e, func(s string) { logs = append(logs, s) })
	if turn != nil || stopped {
		t.Fatalf("turn %v, stopped %v: want the account released without a turn", turn, stopped)
	}
	if waiter.delays != 3 {
		t.Errorf("held its place for %d waits after being carried away at the third", waiter.delays)
	}
	if d := LaneDepth(e); d != len(holders) {
		t.Errorf("the lane holds %d, want the holders only", d)
	}
	if len(logs) == 0 {
		t.Error("said nothing about leaving the line")
	}
}

// The crossing before ours leaves the barrier in its open state for a few ticks (five at the gate, six at the doors); a
// click then is a click on nothing, so the account whose turn it is waits for the barrier to be back.
func TestTheNextInLineWaitsOutTheOpenWindowBeforeItClicks(t *testing.T) {
	defer lanesEmpty(t)
	e := gnomeGate()
	c := &laneClient{me: e.From, absent: func(d int) bool { return d < 5 }}
	turn, waited, stopped := EnterLane(c, e, nil)
	if turn == nil || stopped {
		t.Fatalf("turn %v, stopped %v", turn, stopped)
	}
	if c.delays != 5 || waited != 5*tick {
		t.Errorf("waited %d ticks (%v) for a window of 5, want exactly the window", c.delays, waited)
	}
	turn.Leave()

	// A barrier that never comes back is the walker's own crossing to judge, not a reason to wait for ever.
	never := &laneClient{me: e.From, absent: func(int) bool { return true }}
	turn, _, stopped = EnterLane(never, e, nil)
	if turn == nil || stopped {
		t.Fatalf("a barrier that stays open: turn %v, stopped %v", turn, stopped)
	}
	if never.delays != int(barrierShutWait/tick) {
		t.Errorf("waited %d ticks for a barrier that never came back, want %d", never.delays, int(barrierShutWait/tick))
	}
	turn.Leave()
}

// Nothing else is queued: an ordinary door gets no turn and costs no wait.
func TestAnOrdinaryDoorIsNotQueued(t *testing.T) {
	defer lanesEmpty(t)
	door := &Edge{From: Point{X: 3207, Z: 3217}, To: Point{X: 3208, Z: 3217}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 3207, LocZ: 3217}
	c := &laneClient{me: door.From}
	turn, waited, stopped := EnterLane(c, door, nil)
	if turn != nil || waited != 0 || stopped || c.delays != 0 {
		t.Errorf("an ordinary door: turn %v, waited %v, stopped %v, %d delays", turn, waited, stopped, c.delays)
	}
	if QueuedBarrier(door) || LaneDepth(door) != 0 || LaneCapacity(door) != 0 {
		t.Error("an ordinary door reads as a queued barrier")
	}
	// The same loc id at another placement, and a hop with no loc, are no queued barrier either.
	elsewhere := *grandTreeLeft()
	elsewhere.LocX = 2000
	ship := &Edge{Kind: KindShip, LocID: -1}
	if QueuedBarrier(&elsewhere) || QueuedBarrier(ship) || QueuedBarrier(nil) {
		t.Error("a door at another placement, a ship and nil read as queued barriers")
	}
}

// balanceLane sends a walk planned through a full door to the other when that saves it a wait: the other has a place free,
// or both are full and the other is two places shorter.
func TestBalanceLaneSendsAWalkToTheShorterLine(t *testing.T) {
	defer lanesEmpty(t)
	g := newGrid().fill(2463, 3489, 2468, 3491, 0).fill(2463, 3493, 2468, 3496, 0)
	left, right := grandTreeLeft(), grandTreeRight()
	edges := []Edge{*left, *grandTreeLeftBack(), *right, *grandTreeRightBack()}
	f := newFakeWorld(t, g, left.From)
	w := NewWalker(NewPlanner(g, &Graph{Edges: edges}), f)
	w.Log = func(s string) { t.Log(s) }
	if LaneCapacity(left) != 2 || LaneCapacity(right) != 2 {
		t.Fatalf("the cases below are for doors of two: left %d, right %d", LaneCapacity(left), LaneCapacity(right))
	}

	fill := func(e *Edge, n int) (undo func()) {
		var turns []*laneTurn
		for i := 0; i < n; i++ {
			turns = append(turns, barrierLanes.join(barrierSpecOf(e).key()))
		}
		return func() {
			for _, tt := range turns {
				barrierLanes.leave(barrierSpecOf(e).key(), tt)
			}
		}
	}
	for _, c := range []struct {
		name             string
		on               *Edge
		leftN, rightN    int
		want             int // the loc crossed
		wantFrom, wantTo Point
	}{
		{"no line", left, 0, 0, 1967, left.From, left.To},
		{"one on the left, a place free", left, 1, 0, 1967, left.From, left.To},
		{"left full, none on the right", left, 2, 0, 1968, right.From, right.To},
		{"left full, a place free on the right", left, 2, 1, 1968, right.From, right.To},
		{"three on the left, one on the right", left, 3, 1, 1968, right.From, right.To},
		{"both full, no saving", left, 2, 2, 1967, left.From, left.To},
		{"three on the left, two on the right: one place saved", left, 3, 2, 1967, left.From, left.To},
		{"four on the left, two on the right: two places saved", left, 4, 2, 1968, right.From, right.To},
		{"the way back, left full", grandTreeLeftBack(), 2, 0, 1968, right.To, right.From},
		{"right full, none on the left", right, 0, 2, 1967, left.From, left.To},
		{"right has a place free, left full", right, 2, 1, 1968, right.From, right.To},
	} {
		undoL, undoR := fill(left, c.leftN), fill(right, c.rightN)
		got := w.balanceLane(c.on)
		undoL()
		undoR()
		if got.LocID != c.want || got.From != c.wantFrom || got.To != c.wantTo {
			t.Errorf("%s: crossing %d from %s to %s, want %d from %s to %s", c.name, got.LocID, got.From, got.To, c.want, c.wantFrom, c.wantTo)
		}
	}
	// The gate has no twin, whatever the line.
	gate := gnomeGate()
	undo := fill(gate, 5)
	if got := w.balanceLane(gate); got != gate {
		t.Error("the gate was swapped for another hop")
	}
	undo()
	// A twin the graph does not have is not invented.
	bare := NewWalker(NewPlanner(g, &Graph{Edges: []Edge{*left}}), f)
	undo = fill(left, 4)
	if got := bare.balanceLane(left); got != left {
		t.Errorf("with no right door in the graph the walk was sent to %v", got)
	}
	undo()
}

// ---- the Walker, end to end ----------------------------------------------------------------------------------

// grandTreeWorld is the Grand Tree's south face over a fake world: the doors are scripted (width 2, operated from the
// stand beside them, carrying the player two tiles through and showing nothing to click for six ticks), the row between
// the two sides is solid.
func grandTreeWorld(t *testing.T, me Point) (f *fakeWorld, w *Walker, left, right *fakeDoor) {
	t.Helper()
	g := newGrid().fill(2455, 3480, 2475, 3491, 0).fill(2455, 3493, 2475, 3505, 0)
	f = newFakeWorld(t, g, me)
	mk := func(e *Edge) *fakeDoor {
		stand, lands := e.From, e.To
		d := &fakeDoor{at: Point{X: e.LocX, Z: e.LocZ}, beyond: e.To, id: e.LocID, openID: e.OpenLocID, name: "Tree Door", stand: &stand, lands: &lands}
		f.doors = append(f.doors, d)
		return d
	}
	l, r := grandTreeLeft(), grandTreeRight()
	left, right = mk(l), mk(r)
	w = NewWalker(NewPlanner(g, &Graph{Edges: []Edge{*l, *grandTreeLeftBack(), *r, *grandTreeRightBack()}}), f)
	w.Log = func(s string) { t.Log(s) }
	return f, w, left, right
}

// An account in the Grand Tree's line crosses in its turn and leaves the line when it is through.
func TestAWalkCrossesTheGrandTreeDoorInItsTurn(t *testing.T) {
	defer lanesEmpty(t)
	f, w, left, _ := grandTreeWorld(t, Point{X: 2462, Z: 3482})
	res := w.WalkTo(Point{X: 2462, Z: 3500}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result %+v", res)
	}
	if strings.Join(f.ops, ",") != "1967" {
		t.Errorf("ops %v, want one click on the left door", f.ops)
	}
	if LaneDepth(grandTreeLeft()) != 0 {
		t.Error("the walker is still in the door's line")
	}
	_ = left
}

// Another account has the barrier open when ours gets its turn: ours waits it out and clicks once, as soon as it is back
// (the walker's own attempts would click on nothing, wait four seconds for the step and try again, 3.4 s later than that).
func TestAWalkWaitsForTheDoorToBeBackBeforeItClicks(t *testing.T) {
	defer lanesEmpty(t)
	f, w, left, _ := grandTreeWorld(t, Point{X: 2465, Z: 3491})
	start := f.now
	left.heldUntil = start.Add(5 * time.Second)
	res := w.WalkTo(Point{X: 2462, Z: 3500}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result %+v", res)
	}
	if len(f.ops) != 1 {
		t.Errorf("ops %v, want exactly one click, after the window", f.ops)
	}
	crossedAt := left.heldUntil.Add(-scriptedWindow).Sub(start) // the door is held for a window from each crossing
	if crossedAt < 5*time.Second || crossedAt > 6*time.Second {
		t.Errorf("crossed %v in, want within a tick of the door coming back at 5s", crossedAt)
	}
}

// The wait for a turn is not walking time: a walk with a minute to spend, two minutes in line, still arrives. Both doors are
// full, so there is no twin to cross at, and the walk waits at the one it planned.
func TestTheWaitInLineDoesNotUseUpTheWalksDeadline(t *testing.T) {
	defer lanesEmpty(t)
	f, w, _, _ := grandTreeWorld(t, Point{X: 2465, Z: 3491})
	ahead := append(holdLane(t, grandTreeLeft()), holdLane(t, grandTreeRight())...)
	f.onDelay = func(n int) {
		if n == 200 { // 120 s of waiting
			leaveAll(ahead)
		}
	}
	res := w.WalkTo(Point{X: 2462, Z: 3500}, WalkOptions{Timeout: time.Minute})
	if !res.Arrived() {
		t.Fatalf("a one minute walk that spent two in line: %+v", res)
	}
	if LaneDepth(grandTreeLeft()) != 0 {
		t.Error("lanes not empty at the end")
	}
}

// A walk planned through a crowded door crosses the empty one beside it.
func TestAWalkCrossesTheEmptyDoorWhenTheLeftOneIsCrowded(t *testing.T) {
	defer lanesEmpty(t)
	f, w, _, _ := grandTreeWorld(t, Point{X: 2462, Z: 3482})
	key := barrierSpecOf(grandTreeLeft()).key()
	var crowd []*laneTurn
	for i := 0; i < 3; i++ {
		crowd = append(crowd, barrierLanes.join(key))
	}
	defer func() {
		for _, c := range crowd {
			barrierLanes.leave(key, c)
		}
	}()
	res := w.WalkTo(Point{X: 2462, Z: 3500}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result %+v", res)
	}
	if strings.Join(f.ops, ",") != "1968" {
		t.Errorf("ops %v, want one click on the right door", f.ops)
	}
}

// A crossing that fails still leaves the line, so the next account is not held for the lease.
func TestAFailedCrossingLeavesTheLine(t *testing.T) {
	defer lanesEmpty(t)
	f, w, left, right := grandTreeWorld(t, Point{X: 2465, Z: 3491})
	left.refuse, right.refuse = true, true
	res := w.WalkTo(Point{X: 2462, Z: 3500}, WalkOptions{Timeout: 2 * time.Minute})
	if res.Arrived() {
		t.Fatalf("both doors refuse, yet %+v", res)
	}
	if LaneDepth(grandTreeLeft()) != 0 || LaneDepth(grandTreeRight()) != 0 {
		t.Errorf("the line is not empty after the walk: left %d, right %d", LaneDepth(grandTreeLeft()), LaneDepth(grandTreeRight()))
	}
	if len(f.ops) == 0 {
		t.Error("never clicked a door")
	}
}
