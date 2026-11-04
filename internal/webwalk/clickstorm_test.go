package webwalk

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// clickNeeded is the whole of walkLeg's "should this poll click" rule. The rows that matter are the
// last two: the account near its click target, pathIdx left behind so that the next aim is the very
// tile it is already heading for, was a click EVERY poll before; with a farther tile to aim at it is
// still the one click that keeps a long walk running without a pause.
func TestClickNeeded(t *testing.T) {
	for _, c := range []struct {
		name                                       string
		clickIdx, pathIdx, aim, last, distToTarget int
		want                                       bool
	}{
		{"no click sent yet", -1, 0, 20, 80, 0, true},
		{"the last click already aims at the leg's last tile", 80, 70, 80, 80, 0, false},
		{"past the target", 40, 40, 60, 80, 9, true},
		{"well past the target", 40, 55, 75, 80, 20, true},
		{"far from the target, still walking", 40, 20, 40, 80, 12, false},
		{"near the target with a farther tile to aim at", 40, 30, 50, 80, 4, true},
		{"just outside arriveRadius", 40, 30, 50, 80, 5, false},
		{"near the target but pathIdx is stuck: the aim IS the target", 71, 51, 71, 120, 3, false},
		{"near the target, aim behind it", 71, 40, 60, 120, 2, false},
		{"standing on the target, pathIdx stuck", 71, 51, 71, 120, 0, false},
		{"near the target, aim a tile beyond it", 71, 52, 72, 120, 3, true},
	} {
		if got := clickNeeded(c.clickIdx, c.pathIdx, c.aim, c.last, c.distToTarget); got != c.want {
			t.Errorf("%s: clickNeeded(%d, %d, %d, %d, %d) = %v, want %v", c.name, c.clickIdx, c.pathIdx, c.aim, c.last, c.distToTarget, got, c.want)
		}
	}
}

// An account that keeps moving but never gets further along its route changes tile every poll, so the
// stall check never sees it (live 5 October 2026: ten minutes at 2951,3430, one tile either way).
// walkLeg now plans again once it has gone noProgressFor without getting further.
func TestWalkLegRepathsWhenItMovesButGetsNowhere(t *testing.T) {
	col := newGrid().fill(0, 0, 40, 4, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 2})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	var firstNoProgress time.Time
	w.Log = func(s string) {
		t.Log(s)
		if firstNoProgress.IsZero() && strings.Contains(s, "no progress along the route") {
			firstNoProgress = f.now
		}
	}
	var oscillating time.Time
	f.onDelay = func(n int) {
		if n < 6 {
			return
		}
		if oscillating.IsZero() {
			oscillating = f.now
		}
		// Whatever pushes the account back and forth: one tile either way, every poll.
		f.path = nil
		if n%2 == 0 {
			f.me.X = 12
		} else {
			f.me.X = 13
		}
	}
	f.stopAfter = 600
	w.WalkTo(Point{X: 30, Z: 2}, WalkOptions{Timeout: 30 * time.Minute})
	if firstNoProgress.IsZero() {
		t.Fatalf("the walk never gave up on a leg that moved and got nowhere")
	}
	if got := firstNoProgress.Sub(oscillating); got < noProgressFor || got > noProgressFor+10*time.Second {
		t.Errorf("planned again %v after the account stopped getting anywhere, want about %v", got, noProgressFor)
	}
}

// A leg that is making progress is left alone however long it is: the bound is on getting nowhere,
// not on taking time.
func TestWalkLegLeavesASlowWalkAlone(t *testing.T) {
	col := newGrid().fill(0, 0, 300, 4, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 2})
	w := NewWalker(NewPlanner(col, &Graph{}), f)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	res := w.WalkTo(Point{X: 280, Z: 2}, WalkOptions{Timeout: 30 * time.Minute})
	if !res.Arrived() {
		t.Fatalf("result %+v", res)
	}
	if f.now.Sub(time.Unix(1000, 0)) < noProgressFor {
		t.Fatalf("the walk took %v, shorter than the bound: the test proves nothing", f.now.Sub(time.Unix(1000, 0)))
	}
	if strings.Contains(strings.Join(logs, "\n"), "no progress") {
		t.Errorf("a walk that kept getting further was cut short: %v", logs)
	}
}

// engineSim is a player over a real collision pack, moving the way the engine moves one (Engine-TS 289,
// read at the source): packets are read at the start of a tick, a MOVE_GAMECLICK queues its path with
// the click's START tile first (MoveClickHandler, queueWaypoints), a step is taken toward the current
// waypoint diagonally if it can, else along x, else along z (PathingEntity.takeStep), standing on a
// waypoint pops it without a step (validateAndAdvanceStep returns -1), a walker takes one step a tick
// and a runner two (processMovement). The client sees the player's tile downLat after each tick, and
// its clicks reach the server upLat after they are sent.
type engineSim struct {
	col     Collision
	now     time.Time
	started time.Time
	tickLen time.Duration
	next    time.Time
	upLat   time.Duration
	downLat time.Duration
	jitter  time.Duration
	rng     *rand.Rand

	x, z, level int
	wp          []Point // wp[len-1] is the waypoint being walked to, as the engine stores them
	running     bool

	viewX, viewZ int
	viewQ        []simView
	packets      []simClick

	clicks, ticks int
	stopAt        Point
	finished      bool
	maxTicks      int
}

type simView struct {
	at   time.Time
	x, z int
}

type simClick struct {
	arrive time.Time
	path   []Point
}

func newEngineSim(col Collision, at Point, phase, up, down, jitter time.Duration, seed int64, running bool) *engineSim {
	s := &engineSim{col: col, now: time.Unix(1000, 0), tickLen: 600 * time.Millisecond, upLat: up, downLat: down,
		jitter: jitter, rng: rand.New(rand.NewSource(seed)), x: at.X, z: at.Z, level: at.Level, viewX: at.X, viewZ: at.Z,
		running: running, maxTicks: 3000}
	s.started = s.now
	s.next = s.now.Add(phase)
	return s
}

func (s *engineSim) Tile() (Point, bool) { return Point{X: s.viewX, Z: s.viewZ, Level: s.level}, true }

func (s *engineSim) Walk(path []Point, run bool) error {
	s.clicks++
	if len(path) > 25 { // actions.Walk sends at most 25 corners
		path = path[:25]
	}
	s.packets = append(s.packets, simClick{arrive: s.now.Add(s.upLat), path: append([]Point(nil), path...)})
	return nil
}

func (s *engineSim) FindLocs(LocQuery) []LocMatch       { return nil }
func (s *engineSim) OpLoc(op, x, z, id int) error       { return fmt.Errorf("no locs in this simulation") }
func (s *engineSim) CantReachSince(mark time.Time) bool { return false }
func (s *engineSim) Now() time.Time                     { return s.now }
func (s *engineSim) canTravel(x, z, dx, dz int) bool {
	mask := s.col.ExitMask(x, z, s.level)
	for d := 0; d < 8; d++ {
		if dirX[d] == dx && dirZ[d] == dz {
			return mask&(1<<uint(d)) != 0
		}
	}
	return dx == 0 && dz == 0
}

// advance is one validateAndAdvanceStep: toward the current waypoint, popping it once reached. It
// reports whether the player moved.
func (s *engineSim) advance() bool {
	if len(s.wp) == 0 {
		return false
	}
	target := s.wp[len(s.wp)-1]
	dx, dz := sign(target.X-s.x), sign(target.Z-s.z)
	sx, sz := 0, 0
	switch {
	case s.canTravel(s.x, s.z, dx, dz):
		sx, sz = dx, dz
	case dx != 0 && s.canTravel(s.x, s.z, dx, 0):
		sx = dx
	case dz != 0 && s.canTravel(s.x, s.z, 0, dz):
		sz = dz
	}
	s.x, s.z = s.x+sx, s.z+sz
	if target.X == s.x && target.Z == s.z {
		s.wp = s.wp[:len(s.wp)-1]
	}
	return sx != 0 || sz != 0
}

func (s *engineSim) serverTick() {
	s.ticks++
	var rest []simClick
	for _, p := range s.packets {
		if p.arrive.After(s.next) {
			rest = append(rest, p)
			continue
		}
		s.wp = s.wp[:0]
		for i := len(p.path) - 1; i >= 0; i-- {
			s.wp = append(s.wp, p.path[i])
		}
	}
	s.packets = rest
	if len(s.wp) > 0 {
		moved := s.advance()
		if s.running && moved {
			s.advance()
		}
	}
	s.viewQ = append(s.viewQ, simView{at: s.next.Add(s.downLat), x: s.x, z: s.z})
	if s.x == s.stopAt.X && s.z == s.stopAt.Z {
		s.finished = true
	}
}

// Delay is the walker's poll: tick-long waits, each a little longer than asked by the jitter, which
// is the walk's own work between polls and what lets the polls drift across the ticks.
func (s *engineSim) Delay(d time.Duration) bool {
	if d <= 0 {
		return !s.finished && s.ticks < s.maxTicks
	}
	if s.jitter > 0 {
		d += time.Duration(s.rng.Int63n(int64(s.jitter)))
	}
	end := s.now.Add(d)
	for s.now.Before(end) {
		step := 10 * time.Millisecond
		if s.now.Add(step).After(end) {
			step = end.Sub(s.now)
		}
		s.now = s.now.Add(step)
		for len(s.viewQ) > 0 && !s.viewQ[0].at.After(s.now) {
			s.viewX, s.viewZ = s.viewQ[0].x, s.viewQ[0].z
			s.viewQ = s.viewQ[1:]
		}
		for !s.now.Before(s.next) {
			s.serverTick()
			s.next = s.next.Add(s.tickLen)
		}
	}
	return !s.finished && s.ticks < s.maxTicks
}

// TestRealPackBankToChestWalkSendsNoClickStorm walks the road the main account takes from the Falador west bank
// to the Taverley gate on the chest run, 160 tiles planned over the real collision pack, against the
// engine's own movement rules (engineSim).
//
// Live, 5 October 2026, on 14 trips in 80 minutes: the account stopped at 2951,3430 on three of them
// (once for 10 minutes, with no stall in the log), and a third of the legs took two to three times
// as long as the 40 seconds a clear run takes. The cause was clickNeeded's old rule: from there the
// click's target is the tile 3 away on the route, pathIdx cannot move because the account is 3 tiles
// off the route (it cut the corner), and the walk clicked that one tile every poll, each click
// costing the tick it arrived in. The first case below stood there for 3.6 seconds, the second
// shuffled between two tiles for 127 seconds (112 clicks), both on the old rule.
func TestRealPackBankToChestWalkSendsNoClickStorm(t *testing.T) {
	p := realPack(t)
	from, dest, gate := Point{X: 2945, Z: 3368}, Point{X: 2914, Z: 3451}, Point{X: 2936, Z: 3450}
	run := func(phase, lat, jitter time.Duration, seed int64, running bool) (*engineSim, Result) {
		s := newEngineSim(p.col, from, phase, lat, lat, jitter, seed, running)
		s.stopAt = gate
		w := NewWalker(p, s)
		return s, w.WalkTo(dest, WalkOptions{Timeout: 30 * time.Minute})
	}
	for _, c := range []struct {
		name                string
		phase, lat, jitter  time.Duration
		seed                int64
		running             bool
		maxTicks, maxClicks int
	}{
		{"the stall at 2951,3430 (20 clicks, 3.6 s lost, on the old rule)", 200 * time.Millisecond, 30 * time.Millisecond, 0, 1, true, 62, 8},
		{"the shuffle between two tiles (212 ticks, 110 clicks on the old rule)", 60 * time.Millisecond, 250 * time.Millisecond, 60 * time.Millisecond, 3, false, 112, 8},
	} {
		s, res := run(c.phase, c.lat, c.jitter, c.seed, c.running)
		if !s.finished {
			t.Errorf("%s: never reached the gate: %+v", c.name, res)
			continue
		}
		if s.ticks > c.maxTicks || s.clicks > c.maxClicks {
			t.Errorf("%s: %d ticks and %d clicks to the gate, want at most %d and %d", c.name, s.ticks, s.clicks, c.maxTicks, c.maxClicks)
		}
	}
	// Every alignment of the polls to the ticks, at a short and a long round trip, walking and
	// running: about half of them hit the corner on the old rule.
	worst := 0
	for _, running := range []bool{true, false} {
		for phase := time.Duration(0); phase < 600*time.Millisecond; phase += 50 * time.Millisecond {
			for _, lat := range []time.Duration{20 * time.Millisecond, 150 * time.Millisecond} {
				s, res := run(phase, lat, 0, 1, running)
				if !s.finished {
					t.Fatalf("phase %v latency %v running %v: never reached the gate: %+v", phase, lat, running, res)
				}
				if s.clicks > 8 {
					t.Errorf("phase %v latency %v running %v: %d clicks to the gate", phase, lat, running, s.clicks)
				}
				worst = max(worst, s.ticks)
			}
		}
	}
	// 82 tiles walked is 82 ticks plus the clicks' own ticks; running is about half.
	if worst > 112 {
		t.Errorf("the slowest alignment took %d ticks to the gate", worst)
	}
}
