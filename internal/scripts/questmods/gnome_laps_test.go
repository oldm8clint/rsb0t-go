package questmods

// The Gnome course laps over a scripted course (gnomeSim): every obstacle takes the account over the tiles the content's
// own script does, one tick apart, and sets it down on its landing. The sim refuses an approach started while a movement is
// still in flight, as the collision pack does (no path from the rope's far half, none from inside the pipe), and records it:
// those two approaches are the live failures of 16 Oct 2026.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

type simMove struct {
	at time.Time
	to tile.Tile
	// lap is set on the pipe's landing: one lap done.
	lap bool
}

type simChat struct {
	at   time.Time
	text string
}

type gnomeSim struct {
	t       *testing.T
	start   time.Time
	now     time.Time
	pos     tile.Tile
	level   int // Agility
	perLap  int // levels a finished lap gives
	pending []simMove
	chat    []simChat
	log     []string

	ops     []int    // obstacle indices operated, in order
	bad     []string // approaches started from a tile the pack holds no path from
	travels []tile.Tile

	dead        bool // the ops are accepted and nothing moves
	pipeLocks   int  // the next n pipe ops are answered "the pipe is being used"
	stopAt      time.Duration
	budgetLimit time.Duration // fail the test if the run takes longer than this much virtual time
}

func newGnomeSim(t *testing.T, at tile.Tile, level, perLap int) *gnomeSim {
	start := time.Date(2026, 10, 16, 5, 30, 0, 0, time.UTC)
	return &gnomeSim{t: t, start: start, now: start, pos: at, level: level, perLap: perLap, budgetLimit: 3 * time.Hour}
}

func (s *gnomeSim) Now() time.Time { return s.now }

func (s *gnomeSim) Delay(d time.Duration) bool {
	if s.stopAt > 0 && s.now.Sub(s.start) >= s.stopAt {
		return false
	}
	s.advance(d)
	if s.now.Sub(s.start) > s.budgetLimit {
		s.t.Fatalf("the laps ran for %s of virtual time: a loop that does not end (ops so far %v)", s.budgetLimit, s.ops)
	}
	return true
}

func (s *gnomeSim) advance(d time.Duration) {
	s.now = s.now.Add(d)
	for len(s.pending) > 0 && !s.pending[0].at.After(s.now) {
		m := s.pending[0]
		s.pending = s.pending[1:]
		s.pos = m.to
		if m.lap {
			s.level += s.perLap
		}
	}
}

func (s *gnomeSim) Tile() tile.Tile { return s.pos }
func (s *gnomeSim) Agility() int    { return s.level }

func (s *gnomeSim) Said(since time.Time) string {
	var b strings.Builder
	for _, c := range s.chat {
		if !c.at.Before(since) {
			b.WriteString(strings.ToLower(c.text))
		}
	}
	return b.String()
}

func (s *gnomeSim) WalkNear(dest tile.Tile, _ int) bool {
	if len(s.pending) > 0 {
		s.bad = append(s.bad, fmt.Sprintf("walk to %v from %v mid-move", dest, s.pos))
		return false
	}
	s.pos = dest
	s.advance(2 * time.Second)
	return true
}

func (s *gnomeSim) Travel(dest tile.Tile, _ int) bool {
	s.travels = append(s.travels, dest)
	s.pending = nil
	s.pos = dest
	s.advance(30 * time.Second)
	return true
}

func (s *gnomeSim) Logf(format string, args ...any) {
	s.log = append(s.log, fmt.Sprintf(format, args...))
}

func (s *gnomeSim) logged(sub string) int {
	n := 0
	for _, l := range s.log {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// Operate is ApproachLoc + OpLoc: the approach plans a walk from where the account stands, so it fails from a tile the pack
// holds no path from (any tile with a movement still in flight), and otherwise ends on the obstacle's stand.
func (s *gnomeSim) Operate(i int) error {
	if len(s.pending) > 0 {
		s.bad = append(s.bad, fmt.Sprintf("obstacle %d approached from %v mid-move", i, s.pos))
		s.advance(2 * time.Second)
		return errors.New("could not reach the obstacle: no path from here")
	}
	ob := questplan.GnomeCourse[i]
	s.ops = append(s.ops, i)
	s.pos = ob.Stand
	if s.dead {
		return nil
	}
	step := func(d time.Duration, to tile.Tile) simMove { return simMove{at: s.now.Add(d), to: to} }
	tick := 600 * time.Millisecond
	x := s.pos.X
	switch i {
	case 0: // the log: 2474,3436 to 2474,3429, a tile a tick
		for k := 1; k <= 7; k++ {
			s.pending = append(s.pending, step(time.Duration(k)*tick, tile.Tile{X: 2474, Z: 3436 - k}))
		}
	case 1: // the net: up to level 1, two tiles on
		s.pending = append(s.pending, step(2*tick, tile.Tile{X: x, Z: 3424, Level: 1}))
	case 2: // the branch up
		s.pending = append(s.pending, step(3*tick, tile.Tile{X: 2473, Z: 3420, Level: 2}))
	case 3: // the rope: 2477 to 2483, a tile a tick
		for k := 1; k <= 6; k++ {
			s.pending = append(s.pending, step(time.Duration(k)*tick, tile.Tile{X: 2477 + k, Z: 3420, Level: 2}))
		}
	case 4: // the branch down
		s.pending = append(s.pending, step(2*tick, tile.Tile{X: 2487, Z: 3420}))
	case 5: // the second net
		s.pending = append(s.pending, step(2*tick, tile.Tile{X: x, Z: 3427}))
	case 6: // the pipe: three tiles in, a pause, a teleport on, three tiles out
		if s.pipeLocks > 0 {
			s.pipeLocks--
			s.chat = append(s.chat, simChat{s.now, "The pipe is being used"})
			return nil
		}
		s.pending = append(s.pending,
			step(1*tick, tile.Tile{X: 2484, Z: 3431}), step(2*tick, tile.Tile{X: 2484, Z: 3432}), step(3*tick, tile.Tile{X: 2484, Z: 3433}),
			step(5*tick, tile.Tile{X: 2484, Z: 3434}),
			step(6*tick, tile.Tile{X: 2484, Z: 3435}), step(7*tick, tile.Tile{X: 2484, Z: 3436}))
		s.pending = append(s.pending, simMove{at: s.now.Add(8 * tick), to: tile.Tile{X: 2484, Z: 3437}, lap: true})
	}
	return nil
}

func seq(from, to int) []int {
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i%7)
	}
	return out
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Three laps, each obstacle approached from its predecessor's landing and never from the tiles it walks the account over.
func TestGnomeLapsApproachEveryObstacleFromItsPredecessorsLanding(t *testing.T) {
	s := newGnomeSim(t, questplan.GnomeStart, 1, 1)
	if !gnomeLapsOn(s, 4, time.Hour) {
		t.Fatalf("the laps did not reach the target: level %d, log %v", s.level, s.log)
	}
	if len(s.bad) != 0 {
		t.Fatalf("approaches from tiles the pack holds no path from: %v", s.bad)
	}
	if !sameInts(s.ops, seq(0, 20)) {
		t.Fatalf("obstacles operated %v, want three laps in order", s.ops)
	}
	if n := s.logged("gnome lap "); n != 3 {
		t.Fatalf("%d lap lines, want 3: %v", n, s.log)
	}
	if s.pos.Level != 0 {
		t.Fatalf("ended on %v", s.pos)
	}
	// A lap takes about forty seconds of the course's own movement (the live laps took 85 to 100).
	if lap := s.now.Sub(s.start) / 3; lap > 60*time.Second {
		t.Fatalf("a lap took %s", lap)
	}
}

// The old check fired on the first tile that read as in front of the next obstacle. Over the same sim it approaches the next
// obstacle from the rope's far half (2481,3420,2) and from inside the pipe (2484,3434,0): the two tiles of the live log.
func TestGnomeSimReproducesTheLiveFailureOfTheOldCheck(t *testing.T) {
	s := newGnomeSim(t, questplan.GnomeStart, 1, 1)
	for n := 0; n < 14; n++ {
		here := s.Tile()
		i := questplan.GnomeNext(here)
		if err := s.Operate(i); err != nil {
			s.advance(10 * time.Second)
			continue
		}
		for waited := time.Duration(0); questplan.GnomeNext(s.Tile()) == i && waited < 20*time.Second; waited += 200 * time.Millisecond {
			s.Delay(200 * time.Millisecond)
		}
	}
	got := strings.Join(s.bad, "; ")
	for _, want := range []string{"approached from {2481 3420 2} mid-move", "approached from {2484 3434 0} mid-move"} {
		if !strings.Contains(got, want) {
			t.Errorf("the old check did not fail as it did live (%q): %v", want, s.bad)
		}
	}
}

// The laps end only on the ground: whatever second the budget runs out, the account is on level 0 when they return.
func TestGnomeLapsNeverEndOnThePlatforms(t *testing.T) {
	finished := 0
	for budget := 1; budget <= 140; budget++ {
		s := newGnomeSim(t, questplan.GnomeStart, 1, 0)
		if gnomeLapsOn(s, 99, time.Duration(budget)*time.Second) {
			t.Fatalf("budget %ds: reported the target reached", budget)
		}
		if s.pos.Level != 0 || questplan.InGnomeUpper(s.pos) {
			t.Fatalf("budget %ds: the laps ended on %v after operating %v", budget, s.pos, s.ops)
		}
		if len(s.bad) != 0 {
			t.Fatalf("budget %ds: %v", budget, s.bad)
		}
		if s.logged("finishing the lap down") > 0 {
			finished++
		}
	}
	if finished == 0 {
		t.Fatal("no budget ran out on the platforms: the test does not exercise the finish")
	}
}

// Reaching the level mid-lap on a platform does not stop the laps there either.
func TestGnomeLapsReachingTheLevelOnAPlatformFinishDown(t *testing.T) {
	s := newGnomeSim(t, questplan.GnomeStart, 1, 0)
	// The level arrives with the rope (the sim gives levels on the pipe, so raiseOnRope raises it when the rope lands).
	w := &raiseOnRope{gnomeSim: s, to: 25}
	if !gnomeLapsOn(w, 25, time.Hour) {
		t.Fatalf("target not reached: level %d, log %v", s.level, s.log)
	}
	if s.pos.Level != 0 {
		t.Fatalf("the laps ended on %v with the level reached on the rope", s.pos)
	}
	if !sameInts(s.ops, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("obstacles %v, want the climb-down after the rope that gave the level", s.ops)
	}
}

// raiseOnRope gives the level the moment the rope lands.
type raiseOnRope struct {
	*gnomeSim
	to int
}

func (r *raiseOnRope) Agility() int {
	if r.pos.Level == 2 && r.pos.X == 2483 && r.pos.Z == 3420 {
		r.level = r.to
	}
	return r.level
}

// An account found on the platforms (an earlier rung's laps cut off, a relaunch) is finished forward from the platform it
// stands on: the next obstacle in lap order, never the ones behind it.
func TestLeaveGnomeCourseFinishesForwardFromEveryPlatform(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		ops  []int
	}{
		{"after the net", tile.Tile{X: 2473, Z: 3424, Level: 1}, []int{2, 3, 4}},
		{"on the branch's stand", tile.Tile{X: 2473, Z: 3423, Level: 1}, []int{2, 3, 4}},
		{"after the branch", tile.Tile{X: 2473, Z: 3420, Level: 2}, []int{3, 4}},
		{"at the rope", tile.Tile{X: 2477, Z: 3420, Level: 2}, []int{3, 4}},
		{"after the rope", tile.Tile{X: 2483, Z: 3420, Level: 2}, []int{4}},
		{"at the far branch", tile.Tile{X: 2485, Z: 3419, Level: 2}, []int{4}},
	} {
		s := newGnomeSim(t, c.at, 10, 0)
		gnomeLapsOn(s, 0, 0)
		if s.pos.Level != 0 {
			t.Errorf("%s: left standing on %v", c.name, s.pos)
		}
		if !sameInts(s.ops, c.ops) {
			t.Errorf("%s: obstacles %v, want %v", c.name, s.ops, c.ops)
		}
		if len(s.travels) != 0 || len(s.bad) != 0 {
			t.Errorf("%s: walked off the course (%v) or approached mid-move (%v)", c.name, s.travels, s.bad)
		}
	}
	// On the ground it does nothing at all.
	s := newGnomeSim(t, tile.Tile{X: 2487, Z: 3420}, 10, 0)
	gnomeLapsOn(s, 0, 0)
	if len(s.ops) != 0 || s.now != s.start {
		t.Errorf("on the ground: operated %v, %s passed", s.ops, s.now.Sub(s.start))
	}
}

// A pipe in use by another account is waited out, not counted as a failed obstacle.
func TestGnomeLapsWaitOutTheSharedPipe(t *testing.T) {
	s := newGnomeSim(t, tile.Tile{X: 2485, Z: 3427}, 1, 1)
	s.pipeLocks = 3
	if !gnomeLapsOn(s, 2, time.Hour) {
		t.Fatalf("not reached: %v", s.log)
	}
	if !sameInts(s.ops, []int{6, 6, 6, 6}) {
		t.Fatalf("obstacles %v, want the pipe four times (three refusals and the lap)", s.ops)
	}
	if s.pos.Level != 0 {
		t.Fatalf("ended on %v", s.pos)
	}
}

// Obstacles that do nothing end the laps after gnomeMaxFails tries rather than looping for ever, and say so.
func TestGnomeLapsGiveUpOnObstaclesThatDoNothing(t *testing.T) {
	s := newGnomeSim(t, tile.Tile{X: 2473, Z: 3420, Level: 2}, 10, 0)
	s.dead = true
	if gnomeLapsOn(s, 99, 0) {
		t.Fatal("reported the target reached")
	}
	if len(s.ops) != gnomeMaxFails+1 {
		t.Fatalf("%d tries, want %d", len(s.ops), gnomeMaxFails+1)
	}
	if s.logged("would not clear") != 1 {
		t.Fatalf("no 'would not clear' line: %v", s.log)
	}
}

// A stop request ends the laps wherever they are.
func TestGnomeLapsStopWhenAskedTo(t *testing.T) {
	s := newGnomeSim(t, questplan.GnomeStart, 1, 1)
	s.stopAt = 20 * time.Second
	if gnomeLapsOn(s, 99, time.Hour) {
		t.Fatal("reported the target reached")
	}
	if s.now.Sub(s.start) > 25*time.Second {
		t.Fatalf("ran on to %s after a stop", s.now.Sub(s.start))
	}
}
