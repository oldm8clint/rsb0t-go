package webwalk

import (
	"strings"
	"testing"
	"time"
)

// slowServer is a fakeWorld whose server reads and moves on its own clock, tick by tick, the way the engine does: a click is READ at
// the next tick and dropped when the player is delayed then (MoveClickHandler: "if (player.delayed) return false"), a delayed player
// does not move, and the view is current whatever the pace (it implements no ViewClient: the ticks come, slowly, none of them late
// enough to be silence). hold is the ticks the player is delayed for from the start (a craft's p_delay(3) is four), dropNext the
// clicks to be dropped whatever the hold (a click read late, or dropped, upstream).
type slowServer struct {
	*fakeWorld
	period   time.Duration
	acc2     time.Duration
	ticks    int
	hold     int
	dropNext int
	pending  [][]Point
	dropped  int
}

func (s *slowServer) Walk(path []Point, run bool) error {
	s.fakeWorld.walks++
	s.pending = append(s.pending, append([]Point(nil), path...))
	return nil
}

func (s *slowServer) Delay(d time.Duration) bool {
	if d <= 0 {
		return s.fakeWorld.Delay(d)
	}
	s.fakeWorld.delays++
	s.fakeWorld.now = s.fakeWorld.now.Add(d)
	s.acc2 += d
	for s.acc2 >= s.period {
		s.acc2 -= s.period
		s.ticks++
		// The packets are read before the delay is cleared: a click read in the last tick of the hold is dropped too.
		for _, p := range s.pending {
			if s.hold > 0 || s.dropNext > 0 {
				s.dropped++
				if s.dropNext > 0 {
					s.dropNext--
				}
				continue
			}
			s.fakeWorld.path = p
		}
		s.pending = nil
		if s.hold > 0 {
			s.hold--
			continue
		}
		s.fakeWorld.stepOnce()
	}
	return true
}

// tickedServer is a slowServer that also counts its ticks for the walker (TickClient).
type tickedServer struct{ *slowServer }

func (t tickedServer) ServerTicks() int { return t.slowServer.ticks }

// frozenTicks counts ticks for the walker and never advances: a client whose count has stopped without the view lagging.
type frozenTicks struct{ *slowServer }

func (frozenTicks) ServerTicks() int { return 7 }

func joinLogs(l []string) string { return strings.Join(l, "\n") }

func slowStrip(t *testing.T, build func(*slowServer) Client, s *slowServer) (*slowServer, *Walker, *[]string) {
	t.Helper()
	col := newGrid().fill(0, 0, 100, 2, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 1})
	s.fakeWorld = f
	w := NewWalker(NewPlanner(col, nil), build(s))
	var logs []string
	w.Log = func(l string) { logs = append(logs, l) }
	return s, w, &logs
}

func withoutTicks(s *slowServer) Client { return s }
func withTicks(s *slowServer) Client    { return tickedServer{s} }

// 18 Oct 2026 04:41: the walk from the Nature altar to the portal started inside the craft's four-tick delay on a server that ticked
// every few seconds. The delay is four TICKS, ten seconds at 2.5 s a tick, and every click read in it is dropped; the wall-clock
// stall (3.6 s) fired twice inside it and taught the planner that a step the account had walked a hundred times was blocked. In the
// server's own time the player has not stood still for six ticks until the delay is over and two more have passed.
func TestAServerThatTicksSlowlyIsNotAStall(t *testing.T) {
	s, w, logs := slowStrip(t, withTicks, &slowServer{period: 2500 * time.Millisecond, hold: 4})
	res := w.WalkTo(Point{X: 12, Z: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	all := joined(logs)
	if strings.Contains(all, "blocked live") || len(w.learned) != 0 {
		t.Errorf("a step was taught as blocked from a player that was held by the server:\n%s", all)
	}
	if strings.Contains(all, "repathing") {
		t.Errorf("the walk planned again from a player that was held by the server:\n%s", all)
	}
	if s.dropped == 0 {
		t.Fatal("setup: no click was read inside the delay, so the test proves nothing")
	}
	// The stall, when it comes, is counted in ticks and says so.
	if strings.Contains(all, "stalled at") && !strings.Contains(all, "game ticks in") {
		t.Errorf("the stall line does not say how long it was in the server's time:\n%s", all)
	}
}

// The same server walked by a client that does not count ticks stalls on the wall clock as it always did: the control that the test
// above is about the tick count and not about the server model.
func TestTheSameServerStallsOnTheWallClockWithoutATickCount(t *testing.T) {
	_, w, logs := slowStrip(t, withoutTicks, &slowServer{period: 2500 * time.Millisecond, hold: 4})
	w.WalkTo(Point{X: 12, Z: 1}, WalkOptions{})
	if !strings.Contains(joined(logs), "stalled at") {
		t.Errorf("a client without a tick count did not stall on a held player:\n%s", joined(logs))
	}
}

// A count of ticks that has stopped does not hold a stall off for ever: the wall-clock valve ends the wait.
func TestATickCountThatStoppedDoesNotHideAStall(t *testing.T) {
	col := newGrid().fill(0, 0, 100, 2, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 1})
	for z := 0; z <= 2; z++ {
		f.move.wall(Point{X: 10, Z: z}, Point{X: 11, Z: z})
	}
	s := &slowServer{fakeWorld: f, period: tick}
	w := NewWalker(NewPlanner(col, nil), frozenTicks{s})
	var logs []string
	w.Log = func(l string) { logs = append(logs, l) }
	if res := w.WalkTo(Point{X: 50, Z: 1}, WalkOptions{Timeout: 10 * time.Minute}); res.Arrived() {
		t.Fatal("setup: the walled road was walked")
	}
	if !strings.Contains(joinLogs(logs), "stalled at") {
		t.Errorf("a walk on a wall never stalled:\n%s", joinLogs(logs))
	}
}

// A step is taught as blocked on the third stall, not the second: two clicks that were dropped upstream (read late, or inside the delay
// of the action before) and a third that was not must not leave "blocked live" in the planner for two minutes.
func TestAStepIsNotTaughtAsBlockedFromTwoClicksThatWereNeverRead(t *testing.T) {
	s, w, logs := slowStrip(t, withoutTicks, &slowServer{period: tick, dropNext: 2})
	res := w.WalkTo(Point{X: 12, Z: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v\n%s", res, joined(logs))
	}
	all := joined(logs)
	if s.dropped != 2 {
		t.Fatalf("setup: %d clicks were dropped, want 2\n%s", s.dropped, all)
	}
	if strings.Contains(all, "blocked live") || len(w.learned) != 0 || strings.Contains(all, "repathing") {
		t.Errorf("a step was taught as blocked from two clicks that were never read:\n%s", all)
	}
	if n := strings.Count(all, "clicking again"); n != 2 || !strings.Contains(all, "2 of 3") {
		t.Errorf("%d reclicks in the log, want the two before the third click that worked:\n%s", n, all)
	}
}

// And a step the server really refuses is still taught, on the third stall: three clicks that were never read in a row are a wall.
func TestAStepThreeClicksNeverReadIsTaughtAsBlocked(t *testing.T) {
	_, w, logs := slowStrip(t, withoutTicks, &slowServer{period: tick, dropNext: 3})
	w.WalkTo(Point{X: 12, Z: 1}, WalkOptions{})
	all := joined(logs)
	if !strings.Contains(all, "is blocked live") {
		t.Errorf("a step that three clicks in a row could not take was not taught as blocked:\n%s", all)
	}
	if n := strings.Count(all, "clicking again"); n != 2 {
		t.Errorf("%d reclicks before the lesson, want 2:\n%s", n, all)
	}
}
