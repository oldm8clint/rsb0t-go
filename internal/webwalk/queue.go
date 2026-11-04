package webwalk

import (
	"fmt"
	"sync"
	"time"
)

// Barriers that pass a few accounts at a time, and the fleet's turn at them.
//
// Three crossings in the Tree Gnome Stronghold swap their loc for a few ticks while a player is carried through, so a
// player who clicks after another account's script has run clicks on nothing:
//
//   - the stronghold's south gate (190 at 2459,3383): [oploc1,gnome_areagate] (area_gnome/scripts/gnome_gate.rs2) walks the
//     player to the gate's stand, p_delays, and only then deletes the gate for five ticks, puts its open leaves and two
//     invisible walls in its place and force-moves the player three tiles through;
//   - the Grand Tree's two doors (1967 at 2464,3492 and 1968 at 2466,3492): [oploc1,treedoorl] and [oploc1,treedoorr] change
//     the leaf for six ticks, teleport the player onto the door's column and force-move it two tiles through.
//
// One account does not notice. The Barcrawl wave of 16 Oct 2026 (257 accounts promoted onto Blurberry's bar at 09:01, every
// one of them through those three barriers within about ten minutes) did: 154 of 230 hit "'Gate' with Open not found" (967
// lines), the left Grand Tree door logged 3,051 "did not step through Tree Door" lines against 395 crossings, and the
// refusals added up to a walk that the ladder called unreachable (5 accounts, 4 of them parked their quest for twenty
// minutes) or that ran its six minutes out. The right Grand Tree door, which the walk graph did not have, stood idle.
//
// So the fleet queues. The accounts of one process line up in the order they arrive at a barrier, and the first Capacity of
// them in line hold a turn at once; the rest wait. A turn holder waits for the barrier to be back (the crossing before has it
// open for five or six ticks) and then clicks. A turn lasts from the grant to the landing (or, at the gate, until the first
// dialogue line: Femi's boxes talk is a first-crossing dialogue that leaves the gate alone and takes half a minute). Two
// doors that do the same job are balanced: a walk planned through a full one crosses the other when that saves it a wait.
//
// Why a few at a time and not one. The first version of this queue let one account cross at a time, on the premise that
// the barriers' physical limit is one crossing per window. The same wave's own log says otherwise. The gate's script
// deletes the loc only after its first p_delay, so every click that the server processes in the tick of the first one (and
// in the one before the delete) finds the loc and gets through: the gate carried up to 40 crossings a minute (09:14 and
// 09:16), 82 of its seconds had two or more arrivals, and 20 had three or four; the left door carried up to 37 a minute
// with 7 seconds of three. A serial line (click, landing, wait for the loc to return: about seven ticks, 4.2 s) tops out at
// about 14 a minute. Replaying the 436 gate crossings of the wave through a first-come first-served line at 3.6, 4.2 and
// 5.0 s a crossing, the median wait is 5.4, 7.3 and 10.0 minutes with one at a time (95 and 179 crossings wait more than
// 12 minutes at the last two), 0.5, 1.2 and 2.1 minutes with two, and 0.0, 0.0 and 0.2 minutes with three (the longest
// wait is 0.7 minute). The 213 Barcrawl runs that completed took a median of 34 minutes (the 90th percentile is 53.5) against a
// slot of 56, 31 more ran out the slot, and a run passes four of these barriers, so a serial line would have traded five
// parked quests for a slowdown that ends more runs on the slot's clock.
//
// Capacity is the burst the log shows: 3 at the gate (4 in the busiest second, 6 in four), 2 at each door (the pair is 4).
//
// The doors differ in how a burst gets through. open_tree_door changes the leaf in the tick of the click, so a second click
// in that tick is dropped (Player.validateTarget: the loc's type changed); the account whose click was dropped finds the door
// open and walks through it inside the six ticks (Walker.crossDoor), which is how the left door passed three in a second. The
// gate gives a burst by its p_delay and the doors by their open leaf, and either way two or three together lose nothing that
// one at a time would keep; ten together are the free-for-all that lost 967 clicks.

// barrierSpec is one queued barrier: its placement, and the twin that does the same job beside it.
type barrierSpec struct {
	LocID, X, Z int
	Name        string
	// Capacity is how many accounts hold a turn at once: the burst of clicks the barrier passes in one window.
	Capacity int
	// TwinLocID is the loc of the barrier beside this one that does the same job (0: none), and TwinShift how far its stands
	// and landings are from this one's, in x.
	TwinLocID, TwinShift int
}

var queuedBarriers = []barrierSpec{
	{LocID: 190, X: 2459, Z: 3383, Name: "the Gnome Stronghold gate", Capacity: 3},
	{LocID: 1967, X: 2464, Z: 3492, Name: "the Grand Tree's left door", Capacity: 2, TwinLocID: 1968, TwinShift: 1},
	{LocID: 1968, X: 2466, Z: 3492, Name: "the Grand Tree's right door", Capacity: 2, TwinLocID: 1967, TwinShift: -1},
}

// barrierSpecOf is the queued barrier an edge crosses, or nil.
func barrierSpecOf(e *Edge) *barrierSpec {
	if e == nil || !e.HasLoc() || e.From.Level != 0 {
		return nil
	}
	for i := range queuedBarriers {
		b := &queuedBarriers[i]
		if e.LocID == b.LocID && e.LocX == b.X && e.LocZ == b.Z {
			return b
		}
	}
	return nil
}

// QueuedBarrier reports whether the edge crosses a barrier the fleet takes turns at.
func QueuedBarrier(e *Edge) bool { return barrierSpecOf(e) != nil }

// LaneCapacity is how many accounts may cross the edge's barrier at once, 0 for an edge that is no queued barrier.
func LaneCapacity(e *Edge) int {
	if b := barrierSpecOf(e); b != nil {
		return b.Capacity
	}
	return 0
}

type laneKey struct{ locID, x, z int }

func (b *barrierSpec) key() laneKey { return laneKey{b.LocID, b.X, b.Z} }

const (
	// laneLease is how long a turn may be held, on the wall clock, before the next in line takes its place: a crossing is a
	// few seconds, a first-crossing dialogue at the gate releases its turn when it starts, and nothing may hold a barrier
	// for the whole fleet.
	laneLease = 45 * time.Second
	// laneMaxWait is how long, on the account's own clock, one walker waits in line before it crosses without a turn. A
	// Barcrawl run passes four of these barriers inside a slot of 56 minutes that its slowest runs already fill, and the
	// replay in the header puts the longest wait under a minute at the barrier's capacity, so a wait of minutes means the
	// line is not moving and the account is better off as it was before there was one.
	laneMaxWait = 3 * time.Minute
	// barrierShutWait is how long the account in turn waits for the barrier to be back after the crossing before it.
	barrierShutWait = 10 * tick
	// maxLaneCredit bounds how much of a walk's deadline one queued crossing may add (crossHop): the wait for a turn, the
	// wait for the barrier, a first-crossing dialogue and the crossing itself.
	maxLaneCredit = 5 * time.Minute
	// laneStrayTiles is how far from where it joined the line an account may be carried (a random event, a teleport, a
	// flight from a fight) before it gives its place up: it is no longer the account that was in line.
	laneStrayTiles = 6
)

// laneClock is the clock a lease runs on; tests replace it.
var laneClock = time.Now

// laneTurn is one account's place in a lane.
type laneTurn struct {
	granted   bool
	grantedAt time.Time
}

// laneBook holds the fleet's lanes, one per queued barrier. Every Walker of the process shares barrierLanes.
type laneBook struct {
	mu    sync.Mutex
	lanes map[laneKey][]*laneTurn
}

var barrierLanes = &laneBook{lanes: map[laneKey][]*laneTurn{}}

func (b *laneBook) join(k laneKey) *laneTurn {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := &laneTurn{}
	b.lanes[k] = append(b.lanes[k], t)
	return t
}

// ready reports whether t holds a turn, granting it when t is among the first capacity in line. A turn held past its lease by
// an account ahead of t is dropped on the way (a hung bot), so it cannot keep its place for the whole fleet.
func (b *laneBook) ready(k laneKey, t *laneTurn, capacity int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.granted {
		return true
	}
	now := laneClock()
	q := b.lanes[k]
	kept := make([]*laneTurn, 0, len(q))
	place := -1
	for _, x := range q {
		switch {
		case x == t:
			place = len(kept)
			kept = append(kept, x)
		case place < 0 && x.granted && now.Sub(x.grantedAt) > laneLease:
			// ahead of t and past its lease: gone
		default:
			kept = append(kept, x)
		}
	}
	b.lanes[k] = kept
	if place >= 0 && place < capacity {
		t.granted, t.grantedAt = true, now
		return true
	}
	return false
}

func (b *laneBook) leave(k laneKey, t *laneTurn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.lanes[k]
	for i, x := range q {
		if x == t {
			b.lanes[k] = append(q[:i:i], q[i+1:]...)
			break
		}
	}
	if len(b.lanes[k]) == 0 {
		delete(b.lanes, k)
	}
}

func (b *laneBook) depth(k laneKey) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lanes[k])
}

// LaneTurn is a place in a barrier's lane, held from EnterLane until Leave. A nil LaneTurn (the edge is no queued barrier,
// or the wait for a turn was given up) is valid, and Leave on it does nothing.
type LaneTurn struct {
	book *laneBook
	key  laneKey
	turn *laneTurn
	once sync.Once
}

// Leave gives the turn up. It is safe to call twice, and on a nil turn.
func (t *LaneTurn) Leave() {
	if t == nil {
		return
	}
	t.once.Do(func() { t.book.leave(t.key, t.turn) })
}

// LaneDepth is how many accounts are crossing or waiting to cross the edge's barrier, 0 for an edge that is no queued
// barrier.
func LaneDepth(e *Edge) int {
	b := barrierSpecOf(e)
	if b == nil {
		return 0
	}
	return barrierLanes.depth(b.key())
}

// EnterLane takes a place in the line at the edge's barrier and waits for a turn on c's clock, then waits for the barrier
// to be back (the crossing before has it open for a few ticks). waited is the time all that took. The turn is nil, at once,
// when the edge is no queued barrier, and after laneMaxWait when the line did not move or laneStrayTiles when the account
// was carried away from where it joined (the account then crosses as it did before there was a line, after the wait for the
// barrier in the first case). stopped is the client's stop request; the place in line is given up.
func EnterLane(c Client, e *Edge, log func(string)) (turn *LaneTurn, waited time.Duration, stopped bool) {
	spec := barrierSpecOf(e)
	if spec == nil {
		return nil, 0, false
	}
	key := spec.key()
	t := barrierLanes.join(key)
	began := c.Now()
	say := func(format string, args ...interface{}) {
		if log != nil {
			log(fmt.Sprintf(format, args...))
		}
	}
	joined, haveJoined := c.Tile()
	told := false
	for !barrierLanes.ready(key, t, spec.Capacity) {
		if !told {
			told = true
			say("%s: %d ahead of us in line (%d cross at a time), waiting for the turn", spec.Name, max(barrierLanes.depth(key)-1, 0), spec.Capacity)
		}
		if c.Now().Sub(began) >= laneMaxWait {
			barrierLanes.leave(key, t)
			say("%s: no turn in %v; crossing without one", spec.Name, laneMaxWait)
			// Without a turn the account still does not click on a barrier that another crossing has open.
			if !awaitBarrier(c, e) {
				return nil, c.Now().Sub(began), true
			}
			return nil, c.Now().Sub(began), false
		}
		if me, ok := c.Tile(); ok && haveJoined && (me.Level != joined.Level || Chebyshev(me, joined) > laneStrayTiles) {
			barrierLanes.leave(key, t)
			say("%s: carried from %s to %s while in line; crossing without a turn", spec.Name, joined, me)
			return nil, c.Now().Sub(began), false
		}
		if !c.Delay(tick) {
			barrierLanes.leave(key, t)
			return nil, c.Now().Sub(began), true
		}
	}
	turn = &LaneTurn{book: barrierLanes, key: key, turn: t}
	if told {
		say("%s: our turn after %v", spec.Name, c.Now().Sub(began).Round(time.Second))
	}
	// The crossing before ours has the barrier open for a few ticks yet (five at the gate, six at the doors); a click on it
	// now is a click on nothing.
	if !awaitBarrier(c, e) {
		turn.Leave()
		return nil, c.Now().Sub(began), true
	}
	return turn, c.Now().Sub(began), false
}

// awaitBarrier waits up to barrierShutWait for the edge's barrier to offer its op again, or for the player to be on the
// landing. false is the client's stop request.
func awaitBarrier(c Client, e *Edge) bool {
	shut := func() bool {
		if me, ok := c.Tile(); ok && me == e.To {
			return true
		}
		for _, m := range c.FindLocs(LocQuery{At: Point{X: e.LocX, Z: e.LocZ, Level: e.From.Level}, Radius: 1, IDs: []int{e.LocID}, Op: e.Action}) {
			if m.X == e.LocX && m.Z == e.LocZ {
				return true
			}
		}
		return false
	}
	end := c.Now().Add(barrierShutWait)
	for !shut() {
		if !c.Now().Before(end) {
			return true // the walker's own crossing says what is wrong with it
		}
		if !c.Delay(tick) {
			return false
		}
	}
	return true
}

// balanceLane is the hop to cross in place of e: the twin barrier's, when e's lane is full (an account arriving would wait)
// and the twin has a place free or is at least two shorter, and e itself otherwise. The twin's edge must be in the graph (the
// Planner's index).
func (w *Walker) balanceLane(e *Edge) *Edge {
	spec := barrierSpecOf(e)
	if spec == nil || spec.TwinLocID == 0 || w.Planner == nil {
		return e
	}
	mine := barrierLanes.depth(spec.key())
	if mine < spec.Capacity {
		return e
	}
	var twin *barrierSpec
	for i := range queuedBarriers {
		if queuedBarriers[i].LocID == spec.TwinLocID {
			twin = &queuedBarriers[i]
		}
	}
	if twin == nil {
		return e
	}
	theirs := barrierLanes.depth(twin.key())
	if theirs >= twin.Capacity && theirs+2 > mine {
		return e
	}
	stand := Point{X: e.From.X + spec.TwinShift, Z: e.From.Z, Level: e.From.Level}
	for _, t := range w.Planner.EdgesFrom(stand) {
		if t.LocID == twin.LocID && t.LocX == twin.X && t.LocZ == twin.Z && t.To.Z == e.To.Z && t.To.Level == e.To.Level {
			w.logf("%s has %d in line and %s %d: crossing there instead", spec.Name, mine, twin.Name, theirs)
			return t
		}
	}
	return e
}
