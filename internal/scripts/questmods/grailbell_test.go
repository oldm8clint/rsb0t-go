package questmods

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// fakeBell is a ring stand with the bell's script behind it: a wandering maiden, three message boxes whose continue
// presses are what move the script on, and a teleport that runs only after the second. A virtual clock moves on delays.
type fakeBell struct {
	clock    time.Time
	at       tile.Tile
	bell     bool
	maidenAt func(poll int) (tile.Tile, bool)
	polls    int

	boxes        int  // boxes still up
	pressed      int  // continue presses since the last ring
	rings        int  // times the bell was rung
	heardMaiden  bool // the maiden was in earshot when the bell was last rung
	log          []string
	pressedTotal int
}

func newFakeBell(maidenAt func(poll int) (tile.Tile, bool)) *fakeBell {
	return &fakeBell{clock: time.Unix(1_700_000_000, 0), at: questplan.GrailBellRing, bell: true, maidenAt: maidenAt}
}

func (f *fakeBell) rig() bellRig {
	return bellRig{
		now:     func() time.Time { return f.clock },
		delay:   func(d time.Duration) bool { f.clock = f.clock.Add(d); return true },
		tile:    func() tile.Tile { return f.at },
		hasBell: func() bool { return f.bell },
		maiden: func() (tile.Tile, bool) {
			f.polls++
			return f.maidenAt(f.polls)
		},
		ring: func() error {
			f.rings++
			f.pressed = 0
			m, ok := f.maidenAt(f.polls)
			f.heardMaiden = ok && questplan.GrailMaidenInEarshot(f.at, m)
			f.boxes = 1 // "Ting-a-ling-a-ling!"
			return nil
		},
		boxReady: func() bool { return f.boxes > 0 },
		drive: func() bool {
			// One continue press moves the script on one box: the bell's box, the maiden's greeting (a teleport follows), the
			// castle box.
			for f.boxes > 0 {
				f.boxes--
				f.pressed++
				f.pressedTotal++
				switch {
				case f.pressed == 1 && f.heardMaiden:
					f.boxes = 1 // the maiden greets
				case f.pressed == 2 && f.heardMaiden:
					f.at = questplan.GrailCastleLanding // p_teleport
					f.boxes = 1                         // "Somehow you are now inside the castle."
				}
			}
			return true
		},
		clear: func() { f.boxes = 0 },
		logf:  func(format string, args ...any) { f.log = append(f.log, fmt.Sprintf(format, args...)) },
	}
}

func (f *fakeBell) said(sub string) bool {
	for _, l := range f.log {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// The old flow rang the bell and walked off six seconds later with every box unpressed; the script's p_pausebutton waits
// never ran on, and no maiden was asked for. Here the account waits for a maiden within four tiles, rings once, presses
// continue through the bell's box and the maiden's greeting, and is in the castle.
func TestTheBellIsRungBesideAMaidenAndItsBoxesAreContinued(t *testing.T) {
	f := newFakeBell(func(poll int) (tile.Tile, bool) {
		if poll < 6 {
			return tile.Tile{X: 2760, Z: 4688}, true // five tiles off: out of earshot
		}
		return tile.Tile{X: 2763, Z: 4690}, true // the spawn three tiles south
	})
	if !ringBellWith(f.rig()) {
		t.Fatalf("the bell did not take the account in (at %v); log %v", f.at, f.log)
	}
	if f.rings != 1 {
		t.Errorf("the bell was rung %d times, want once beside the maiden", f.rings)
	}
	if !f.heardMaiden || f.polls < 6 {
		t.Errorf("rung after %d looks for a maiden with one in earshot: %v", f.polls, f.heardMaiden)
	}
	if f.pressedTotal < 2 {
		t.Errorf("%d continue presses: the teleport runs only after the second", f.pressedTotal)
	}
	if !questplan.InGrailCastle(f.at) {
		t.Errorf("at %v after the bell", f.at)
	}
	if !f.said("waiting for one to walk by") || !f.said("pressing continue") || !f.said("inside the Grail castle") {
		t.Errorf("the log lacks the bell's lines: %v", f.log)
	}
}

// Without a maiden in earshot the bell is never rung ("Nothing happens"): the account waits out its budget and says so.
func TestTheBellIsNotRungWithoutAMaiden(t *testing.T) {
	f := newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2769, Z: 4686}, true })
	if ringBellWith(f.rig()) {
		t.Fatal("the account got in with no maiden in earshot")
	}
	if f.rings != 0 {
		t.Errorf("the bell was rung %d times with no maiden in earshot", f.rings)
	}
	if !f.said("no Grail Maiden came into earshot") {
		t.Errorf("no verdict in the log: %v", f.log)
	}
	if f.clock.Sub(time.Unix(1_700_000_000, 0)) < grailBellBudget {
		t.Errorf("gave up after %v, before the %v budget", f.clock.Sub(time.Unix(1_700_000_000, 0)), grailBellBudget)
	}
	f = newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{}, false })
	if ringBellWith(f.rig()) || f.rings != 0 {
		t.Error("no maiden in view at all")
	}
}

// A maiden that wandered out of earshot between the look and the click: the box comes, the script says "Nothing happens" and
// no teleport follows; the account asks again, and rings again once the maiden is back.
func TestTheBellAsksAgainWhenTheScriptSaidNothingHappens(t *testing.T) {
	f := newFakeBell(func(poll int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	rig := f.rig()
	ring := rig.ring
	rig.ring = func() error {
		err := ring()
		if f.rings == 1 {
			f.heardMaiden = false // she stepped out of earshot as the click landed
		}
		return err
	}
	if !ringBellWith(rig) {
		t.Fatalf("the second ring did not take the account in (at %v); log %v", f.at, f.log)
	}
	if f.rings != 2 {
		t.Errorf("the bell was rung %d times, want the failed one and the one that worked", f.rings)
	}
	if !f.said("nothing came of it") {
		t.Errorf("the failed ring is not in the log: %v", f.log)
	}
}

func TestTheBellNeedsTheBellAndIsNotRungTwice(t *testing.T) {
	f := newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	f.bell = false
	if ringBellWith(f.rig()) || f.rings != 0 || !f.said("gone from the pack") {
		t.Errorf("no bell: rings %d, log %v", f.rings, f.log)
	}
	f = newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	f.at = questplan.GrailCastleLanding
	if !ringBellWith(f.rig()) || f.rings != 0 {
		t.Errorf("an account already in the castle rang the bell %d times", f.rings)
	}
}

// stuckMenu wraps a rig so a chat box the driver will not answer stays up: the bell's box is an options menu (a random event's,
// a staller's, a leftover Fisherman's) with nothing the driver may pick, so drive gives up at once, leaves it open and reports
// false, as Dialogue.Drive does ("no preferred option ... stopping rather than guessing"). The virtual clock moves only on
// delays, so a loop that presses without waiting never ends; the guard turns that into a failure instead of a hung test.
type stuckMenu struct {
	f        *fakeBell
	drives   int
	clears   int
	runaway  bool
	stopAt   int // the delay call after which the bot is stopped (0 for never)
	delays   int
	driveRet bool // what drive reports while it leaves the box up
}

func (m *stuckMenu) rig() bellRig {
	r := m.f.rig()
	r.boxReady = func() bool { return !m.runaway && m.f.boxes > 0 }
	r.drive = func() bool {
		m.drives++
		if m.drives > 5000 {
			m.runaway = true
		}
		return m.driveRet // the box is still up
	}
	r.clear = func() { m.clears++; m.f.boxes = 0 }
	delay := r.delay
	r.delay = func(d time.Duration) bool {
		m.delays++
		if m.stopAt > 0 && m.delays >= m.stopAt {
			return false
		}
		return delay(d)
	}
	return r
}

// An options menu at the ring stand that the driver will not answer used to be pressed in a tight loop for the whole 25 s
// script window (the driver returns at once and the loop never waited), thousands of log lines a second and no stop check.
// Now each pass that leaves the box up waits, a few passes with no headway end the attempt, the menu is dismissed, and a menu
// that comes back ends the whole bell.
func TestTheBellDoesNotSpinOnAMenuItCannotAnswer(t *testing.T) {
	f := newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	m := &stuckMenu{f: f}
	if ringBellWith(m.rig()) {
		t.Fatal("the account got in through a menu nothing answered")
	}
	if m.runaway {
		t.Fatalf("pressed %d times without waiting: a hot loop", m.drives)
	}
	if want := grailBellStuckPasses * grailBellStuckRings; m.drives != want {
		t.Errorf("%d presses, want %d (%d passes on each of %d rings)", m.drives, want, grailBellStuckPasses, grailBellStuckRings)
	}
	if f.rings != grailBellStuckRings {
		t.Errorf("the bell was rung %d times, want %d", f.rings, grailBellStuckRings)
	}
	if m.clears != f.rings {
		t.Errorf("the leftover menu was dismissed %d times after %d rings", m.clears, f.rings)
	}
	if f.boxes != 0 {
		t.Error("the menu was left open")
	}
	if !f.said("could not be continued") || !f.said("stopping after") {
		t.Errorf("the log lacks the verdict: %v", f.log)
	}
	if len(f.log) > 20 {
		t.Errorf("%d log lines for one stuck menu: %v", len(f.log), f.log)
	}
	// Every pass that left the box up waited: the virtual clock saw the waits.
	if f.clock.Sub(time.Unix(1_700_000_000, 0)) < time.Duration(m.drives)*300*time.Millisecond {
		t.Errorf("%d presses in only %v", m.drives, f.clock.Sub(time.Unix(1_700_000_000, 0)))
	}
}

// A stopped bot's wait fails: the press that gave up because of the stop must end the bell, not be pressed again.
func TestTheBellStopsWhenTheBotIsStoppedMidScript(t *testing.T) {
	f := newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	m := &stuckMenu{f: f, stopAt: 1}
	if ringBellWith(m.rig()) {
		t.Fatal("a stopped bot got in")
	}
	if m.runaway || m.drives != 1 {
		t.Errorf("%d presses after the stop (runaway %v), want the one that saw it", m.drives, m.runaway)
	}
	if f.rings != 1 {
		t.Errorf("the bell was rung %d times", f.rings)
	}
}

// A press that claims progress while the box stays up still waits before the next, so the loop is bounded by the clock: the
// script window ends the attempt and the ring budget ends the bell.
func TestTheBellWaitsBetweenPressesThatLeaveTheBoxUp(t *testing.T) {
	f := newFakeBell(func(int) (tile.Tile, bool) { return tile.Tile{X: 2763, Z: 4690}, true })
	m := &stuckMenu{f: f, driveRet: true}
	if ringBellWith(m.rig()) {
		t.Fatal("the account got in through a box nothing moved")
	}
	if m.runaway {
		t.Fatalf("pressed %d times without waiting: a hot loop", m.drives)
	}
	if limit := int(grailBellBudget/(300*time.Millisecond)) + 10; m.drives > limit {
		t.Errorf("%d presses, more than one per 300ms of the %v budget (%d)", m.drives, grailBellBudget, limit)
	}
	if m.clears < 2 {
		t.Errorf("the box was dismissed %d times", m.clears)
	}
}
