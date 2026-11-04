package randomevents

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// The execution of a level exit, on a hand-made plan whose stand is far from the account: the reviewer's finding on
// the first version, which counted eight tiles from the ORIGIN as "left" and so returned while the account was still
// walking to a stand twelve tiles off, with the staircase never clicked. No real pack is needed: the plan is given,
// and the fake server moves the account the way the live one does (a walk a couple of tiles at a time, the loc's
// click setting it down at the landing).

// exitWalker is the fake server's walk: after a MOVE_GAMECLICK the account is stepped toward the stand a few tiles
// at a time, on its own goroutine, so the poll loop under test sees it arrive between two of its polls.
type exitWalker struct {
	wg   sync.WaitGroup
	stop chan struct{}
}

func newExitWalker(t *testing.T) *exitWalker {
	t.Helper()
	w := &exitWalker{stop: make(chan struct{})}
	t.Cleanup(func() { close(w.stop); w.wg.Wait() })
	return w
}

// start walks the account of s along its row to standX, step tiles every `every`.
func (w *exitWalker) start(s *overlapSession, standX, step int, every time.Duration) {
	x, z, level, _ := s.state.Tile()
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for x != standX {
			select {
			case <-w.stop:
				return
			case <-time.After(every):
			}
			d := standX - x
			if d > step {
				d = step
			} else if d < -step {
				d = -step
			}
			x += d
			setExactTile(s.state, x, z, level)
		}
	}()
}

func exitTestResolver(t *testing.T) {
	t.Helper()
	withResolver(t, locResolver{
		foodResolver: foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{365: "Bass"}}, objOps: map[int][5]string{365: {"Eat"}}},
		locOps:       map[int][5]string{1740: {"Climb-down"}, 1750: {"Climb-up"}},
	})
	shortenLevelExitClocks(t)
}

// farExit is a plan from origin to a stand `tiles` east of it on the same row, with the loc on the stand's next tile.
func farExit(origin pathfind.NavPoint, tiles int, land StrandTile, locID int, label string) levelExitPlan {
	stand := StrandTile{origin.X + tiles, origin.Z, origin.Level}
	return levelExitPlan{
		Exit:  LevelExit{Stand: stand, Land: land, LocID: locID, LocX: stand.X, LocZ: stand.Z + 1, Label: label},
		Route: []pathfind.NavPoint{origin, {X: stand.X, Z: stand.Z, Level: stand.Level}},
		Steps: tiles,
	}
}

// The staircase twelve tiles off: the account is walked there, and only THEN is the loc clicked; the walk is not the hop.
func TestRunLevelExitClicksOnlyOnceTheAccountIsOnAStandTwelveTilesOff(t *testing.T) {
	exitTestResolver(t)
	origin := pathfind.NavPoint{X: 3090, Z: 3162, Level: 2}
	plan := farExit(origin, 12, StrandTile{3101, 3158, 1}, 1740, "Climb-down")

	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3090, Z: 3163, Level: 2})
	setHP(s.state, 54, 54)
	walker := newExitWalker(t)
	clicks, walks := 0, 0
	var atClick []int
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			walks++
			walker.start(s, plan.Exit.Stand.X, 2, 10*time.Millisecond)
		case clientprot.OPLOC1:
			clicks++
			x, z, l, _ := s.state.Tile()
			atClick = []int{x, z, l}
			setExactTile(s.state, plan.Exit.Land.X, plan.Exit.Land.Z, plan.Exit.Land.Level)
		}
	}
	var logs []string
	if !runLevelExit(s, origin, plan, func(l string) { logs = append(logs, l) }) {
		t.Fatalf("the account did not leave\n%s", strings.Join(logs, "\n"))
	}
	if walks != 1 || clicks != 1 {
		t.Fatalf("walks %d clicks %d, want 1 and 1\n%s", walks, clicks, strings.Join(logs, "\n"))
	}
	if len(atClick) != 3 || atClick[0] != plan.Exit.Stand.X || atClick[1] != plan.Exit.Stand.Z || atClick[2] != 2 {
		t.Fatalf("the loc was clicked from %v, not from the stand %v", atClick, plan.Exit.Stand)
	}
	if _, _, l, _ := s.state.Tile(); l != 1 {
		t.Fatalf("level %d afterwards, want 1", l)
	}
}

// The reviewer's reproduction: the walk puts the account eight tiles along, once, and then it stands (a stall, a door,
// a slow link). Eight tiles from the origin is not a hop. Left at that, the first version returned true with no click.
func TestRunLevelExitDoesNotCountEightTilesFromTheOriginAsLeft(t *testing.T) {
	exitTestResolver(t)
	origin := pathfind.NavPoint{X: 3111, Z: 3162, Level: 2}
	plan := farExit(origin, 12, StrandTile{3101, 3158, 1}, 1740, "Climb-down")

	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3111, Z: 3163, Level: 2})
	setHP(s.state, 54, 54)
	clicks := 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			setExactTile(s.state, origin.X+8, origin.Z, origin.Level)
		case clientprot.OPLOC1:
			clicks++
			setExactTile(s.state, plan.Exit.Land.X, plan.Exit.Land.Z, plan.Exit.Land.Level)
		}
	}
	var logs []string
	if !runLevelExit(s, origin, plan, func(l string) { logs = append(logs, l) }) {
		t.Fatalf("the account did not leave\n%s", strings.Join(logs, "\n"))
	}
	if clicks != 1 {
		t.Fatalf("clicked the staircase %d times, want 1: the account left before the loc was operated\n%s", clicks, strings.Join(logs, "\n"))
	}
}

// A ladder out of a dungeon sets the account down on the SAME level, thousands of tiles away: the jump is what shows
// it, and it must not be read before the click either.
func TestRunLevelExitSeesALadderOutOfADungeonOnlyByItsJump(t *testing.T) {
	exitTestResolver(t)
	origin := pathfind.NavPoint{X: 3092, Z: 9570, Level: 0}
	plan := farExit(origin, 12, StrandTile{3104, 3170, 0}, 1750, "Climb-up")

	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3092, Z: 9571, Level: 0})
	setHP(s.state, 54, 54)
	walker := newExitWalker(t)
	clicks := 0
	var atClick []int
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			walker.start(s, plan.Exit.Stand.X, 2, 10*time.Millisecond)
		case clientprot.OPLOC1:
			clicks++
			x, z, l, _ := s.state.Tile()
			atClick = []int{x, z, l}
			setExactTile(s.state, plan.Exit.Land.X, plan.Exit.Land.Z, plan.Exit.Land.Level)
		}
	}
	var logs []string
	if !runLevelExit(s, origin, plan, func(l string) { logs = append(logs, l) }) {
		t.Fatalf("the account did not leave\n%s", strings.Join(logs, "\n"))
	}
	if clicks != 1 || len(atClick) != 3 || atClick[0] != plan.Exit.Stand.X {
		t.Fatalf("clicks %d from %v, want one from the stand %v\n%s", clicks, atClick, plan.Exit.Stand, strings.Join(logs, "\n"))
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "level exit: left 3092,9570,0 for 3104,3170,0") {
		t.Fatalf("no 'left' line:\n%s", joined)
	}
}

// The walk is slow and the wait for it runs out part-way: the account is then eight tiles or more from the origin AND
// from the stand, and it is still only walking. A click that sets nothing down is given up on after its clicks, not
// reported as a hop (measuring from the stand, as the review suggested, would have called this one left).
func TestRunLevelExitDoesNotCallASlowWalkTheHop(t *testing.T) {
	exitTestResolver(t)
	origin := pathfind.NavPoint{X: 3040, Z: 3162, Level: 0}
	plan := farExit(origin, 30, StrandTile{3041, 9600, 0}, 1750, "Climb-up")

	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3040, Z: 3163, Level: 0})
	setHP(s.state, 54, 54)
	walker := newExitWalker(t)
	clicks, firstClickX := 0, 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			walker.start(s, plan.Exit.Stand.X, 1, 40*time.Millisecond) // 30 tiles take 1.2 s; the wait is 500 ms
		case clientprot.OPLOC1:
			if clicks++; clicks == 1 {
				firstClickX, _, _, _ = s.state.Tile()
			}
		}
	}
	var logs []string
	if runLevelExit(s, origin, plan, func(l string) { logs = append(logs, l) }) {
		t.Fatalf("a walk was reported as the hop\n%s", strings.Join(logs, "\n"))
	}
	if clicks != levelExitClicks {
		t.Fatalf("clicks %d, want %d\n%s", clicks, levelExitClicks, strings.Join(logs, "\n"))
	}
	if firstClickX <= origin.X+8 || firstClickX >= plan.Exit.Stand.X-8 {
		t.Fatalf("the account was at x=%d at the first click: the test needs it part-way (more than 8 from the origin %d and from the stand %d)", firstClickX, origin.X, plan.Exit.Stand.X)
	}
}

// Something else carries the account off while it walks to the stand (a summons, a teleport another script cast): the
// monster is shaken and the loc is not clicked.
func TestRunLevelExitStopsWhenSomethingElseCarriesTheAccountOff(t *testing.T) {
	exitTestResolver(t)
	origin := pathfind.NavPoint{X: 3090, Z: 3162, Level: 2}
	plan := farExit(origin, 12, StrandTile{3101, 3158, 1}, 1740, "Climb-down")

	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3090, Z: 3163, Level: 2})
	setHP(s.state, 54, 54)
	clicks := 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			setExactTile(s.state, origin.X+200, origin.Z, origin.Level)
		case clientprot.OPLOC1:
			clicks++
		}
	}
	if !runLevelExit(s, origin, plan, nil) {
		t.Fatal("an account carried 200 tiles away was not reported as gone")
	}
	if clicks != 0 {
		t.Fatalf("clicked the staircase %d times after the account was carried off", clicks)
	}
}
