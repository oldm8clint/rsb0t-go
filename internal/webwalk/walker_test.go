package webwalk

import (
	"strings"
	"testing"
	"time"
)

// fakeWorld is a Client over a grid: doors block their edge until opened, ladders and hops
// move the player only from their exact stand, and every Delay advances a fake clock and the
// player's queued movement by one tile a tick.
type fakeWorld struct {
	t       *testing.T
	plan    *grid // what the planner sees: every door shut
	move    *grid // what movement sees
	doors   []*fakeDoor
	ladders []*fakeLadder
	me      Point
	path    []Point
	now     time.Time
	acc     time.Duration
	delays  int
	cantAt  []time.Time
	ops     []string
	walks   int
	// ownStarts counts clicks whose first waypoint was the player's own tile.
	ownStarts int

	stopAfter int
	onDelay   func(n int)

	// modal is whether a dialogue is currently up, and modalsClosed counts how often the
	// walker dismissed one.
	modal        bool
	modalsClosed int
	// modalHolds makes an open modal stop every step, as Engine-TS does while anything is queued
	// (Player.updateMovement).
	modalHolds bool
}

// ModalOpen and CloseModal make fakeWorld a ModalClient.
func (f *fakeWorld) ModalOpen() bool { return f.modal }

func (f *fakeWorld) CloseModal() error {
	f.modal = false
	f.modalsClosed++
	return nil
}

type fakeDoor struct {
	at, beyond   Point
	id, openID   int
	name         string
	open, refuse bool
	// refuseTimes makes the door refuse its first n operations and then open, which is how a
	// slashable web behaves: content cuts it on `random(2) = 1`, so the same correct click
	// fails and then works.
	refuseTimes int
	// dialog makes the door answer its open click with a dialogue and stay shut, the way
	// content's own outpost_gate.rs2 answers for an account that has not completed the
	// Alfred Grimhand Barcrawl — see ModalClient's doc comment.
	dialog bool
	// stand, when set, is the one tile the door can be operated from, and lands where its script carries the player the
	// moment it opens (the Grand Tree's doors: a width-2 loc whose stand is beside it, and a force-move two tiles through).
	// A scripted door shuts again by itself; it is held (invisible, unclickable) for scriptedWindow after each crossing.
	stand *Point
	lands *Point
	// heldUntil is another account's open window: until then the door offers nothing to click.
	heldUntil time.Time
}

// scriptedWindow is how long a scripted door stays in its open state after a crossing (six ticks at the Grand Tree).
const scriptedWindow = 6 * tick

type fakeLadder struct {
	loc, stand, landing Point
	id                  int
	name, op            string
	// refuse makes the loc answer nothing, as a gangplank does while its scene is still loading.
	refuse bool
}

func newFakeWorld(t *testing.T, plan *grid, me Point) *fakeWorld {
	move := &grid{walk: plan.walk, blocked: map[[2]Point]bool{}}
	for k, v := range plan.blocked {
		move.blocked[k] = v
	}
	return &fakeWorld{t: t, plan: plan, move: move, me: me, now: time.Unix(1000, 0)}
}

func (f *fakeWorld) addDoor(at, beyond Point, id int) *fakeDoor {
	f.plan.wall(at, beyond)
	f.move.wall(at, beyond)
	d := &fakeDoor{at: at, beyond: beyond, id: id, openID: id + 1, name: "Door"}
	f.doors = append(f.doors, d)
	return d
}

func (f *fakeWorld) Tile() (Point, bool) { return f.me, true }

func (f *fakeWorld) Walk(path []Point, run bool) error {
	f.walks++
	if len(path) == 0 {
		f.t.Errorf("an empty walk click from %s", f.me)
	} else if path[0] == f.me {
		f.ownStarts++
	}
	// The engine's first waypoint is path[0], wherever it is; one on the player's tile is popped
	// (stepOnce).
	f.path = append([]Point(nil), path...)
	return nil
}

func (f *fakeWorld) stepOnce() {
	for len(f.path) > 0 && f.path[0] == f.me {
		f.path = f.path[1:]
	}
	if len(f.path) == 0 {
		return
	}
	next := f.path[0]
	if next.Level != f.me.Level {
		f.path = nil
		return
	}
	s := Point{X: f.me.X + sign(next.X-f.me.X), Z: f.me.Z + sign(next.Z-f.me.Z), Level: f.me.Level}
	mask := f.move.ExitMask(f.me.X, f.me.Z, f.me.Level)
	for d := 0; d < 8; d++ {
		if f.me.X+dirX[d] == s.X && f.me.Z+dirZ[d] == s.Z && mask&(1<<uint(d)) != 0 {
			f.me = s
			return
		}
	}
	f.path = nil // blocked: the server stops the walk
}

func (f *fakeWorld) Delay(d time.Duration) bool {
	if d <= 0 {
		return f.stopAfter == 0 || f.delays < f.stopAfter
	}
	f.delays++
	if f.stopAfter > 0 && f.delays >= f.stopAfter {
		return false
	}
	f.now = f.now.Add(d)
	f.acc += d
	for f.acc >= tick {
		f.acc -= tick
		if f.modalHolds && f.modal {
			continue
		}
		f.stepOnce()
	}
	if f.onDelay != nil {
		f.onDelay(f.delays)
	}
	return true
}

func (f *fakeWorld) Now() time.Time { return f.now }

func (f *fakeWorld) CantReachSince(mark time.Time) bool {
	for _, at := range f.cantAt {
		if !at.Before(mark) {
			return true
		}
	}
	return false
}

func (f *fakeWorld) FindLocs(q LocQuery) []LocMatch {
	var out []LocMatch
	match := func(id int, name, op string, at Point, ops ...string) {
		if at.Level != q.At.Level || Chebyshev(at, q.At) > q.Radius {
			return
		}
		if len(q.IDs) > 0 {
			hit := false
			for _, want := range q.IDs {
				hit = hit || want == id
			}
			if !hit {
				return
			}
		} else {
			hit := false
			for _, n := range q.Names {
				hit = hit || strings.EqualFold(n, name)
			}
			if !hit {
				return
			}
		}
		if q.Op != "" && !strings.EqualFold(q.Op, op) {
			return
		}
		out = append(out, LocMatch{ID: id, X: at.X, Z: at.Z, Level: at.Level, Name: name, Op: 1})
	}
	for _, d := range f.doors {
		if f.now.Before(d.heldUntil) {
			continue
		}
		if d.open {
			match(d.openID, d.name, "Close", d.at)
		} else {
			match(d.id, d.name, "Open", d.at)
		}
	}
	for _, l := range f.ladders {
		match(l.id, l.name, l.op, l.loc)
	}
	return out
}

func (f *fakeWorld) OpLoc(op, x, z, id int) error {
	f.ops = append(f.ops, strings.TrimSpace(strings.Join([]string{itoa(id)}, "")))
	for _, d := range f.doors {
		if d.at.X == x && d.at.Z == z && d.id == id && !d.open {
			if f.now.Before(d.heldUntil) {
				return nil // the loc is not there to click: another account has it open
			}
			reach := f.me == d.at || f.me == d.beyond
			if d.stand != nil {
				reach = f.me == *d.stand
			}
			if !reach {
				f.cantAt = append(f.cantAt, f.now)
				return nil
			}
			if d.dialog {
				f.modal = true // the guard talks; the gate stays shut
				return nil
			}
			if d.refuseTimes > 0 {
				d.refuseTimes--
				return nil
			}
			if !d.refuse && d.lands != nil {
				f.me, f.path = *d.lands, nil
				d.heldUntil = f.now.Add(scriptedWindow)
				return nil
			}
			if !d.refuse {
				d.open = true
				delete(f.move.blocked, [2]Point{d.at, d.beyond})
				delete(f.move.blocked, [2]Point{d.beyond, d.at})
			}
			return nil
		}
	}
	for _, l := range f.ladders {
		if l.loc.X == x && l.loc.Z == z && l.id == id {
			if f.me != l.stand {
				f.cantAt = append(f.cantAt, f.now)
				return nil
			}
			if l.refuse {
				return nil
			}
			f.me, f.path = l.landing, nil
			return nil
		}
	}
	f.t.Errorf("OpLoc on nothing: %d at %d,%d", id, x, z)
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

// house is a west room (x 0-4) and an east yard (x 5-14) split by a wall, a door at (4,2)
// opening east, and a ladder in the west room whose only stand is (1,1) (the loc is at (0,1)),
// up to a small level-1 room.
func house(t *testing.T, me Point) (*fakeWorld, *Walker, *Graph) {
	col := newGrid().fill(0, 0, 4, 4, 0).fill(5, 0, 14, 4, 0).fill(0, 0, 3, 3, 1)
	for z := 0; z <= 4; z++ {
		if z != 2 {
			col.wall(Point{X: 4, Z: z}, Point{X: 5, Z: z})
		}
	}
	delete(col.walk, Point{X: 0, Z: 1})
	f := newFakeWorld(t, col, me)
	f.addDoor(Point{X: 4, Z: 2}, Point{X: 5, Z: 2}, 1530)
	f.ladders = append(f.ladders, &fakeLadder{loc: Point{X: 0, Z: 1}, stand: Point{X: 1, Z: 1}, landing: Point{X: 1, Z: 1, Level: 1}, id: 1747, name: "Ladder", op: "Climb-up"})
	g := &Graph{Edges: []Edge{
		{From: Point{X: 4, Z: 2}, To: Point{X: 5, Z: 2}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 4, LocZ: 2},
		{From: Point{X: 5, Z: 2}, To: Point{X: 4, Z: 2}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 4, LocZ: 2},
		{From: Point{X: 1, Z: 1}, To: Point{X: 1, Z: 1, Level: 1}, Kind: KindStair, Cost: 6, LocName: "Ladder", Action: "Climb-up", LocID: 1747, OpenLocID: -1, LocX: 0, LocZ: 1},
	}}
	w := NewWalker(NewPlanner(col, g), f)
	w.Log = func(s string) { t.Log(s) }
	return f, w, g
}

func TestWalkerCrossesADoorAndClimbsALadder(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 0})
	res := w.WalkTo(Point{X: 3, Z: 3, Level: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if strings.Join(f.ops, ",") != "1530,1747" {
		t.Errorf("ops = %v, want the door then the ladder", f.ops)
	}
	if f.me != (Point{X: 3, Z: 3, Level: 1}) {
		t.Errorf("ended at %s", f.me)
	}
}

func TestWalkerRoutesAroundARefusingDoor(t *testing.T) {
	f, w, g := house(t, Point{X: 9, Z: 2})
	// A second door further south; the first one never opens.
	f.doors[0].refuse = true
	col := f.plan
	col.blocked[[2]Point{{X: 4, Z: 0}, {X: 5, Z: 0}}] = true
	second := f.addDoor(Point{X: 4, Z: 0}, Point{X: 5, Z: 0}, 1600)
	_ = second
	g.Edges = append(g.Edges,
		Edge{From: Point{X: 4, Z: 0}, To: Point{X: 5, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
		Edge{From: Point{X: 5, Z: 0}, To: Point{X: 4, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
	)
	w2 := NewWalker(NewPlanner(col, g), f)
	w2.Log = w.Log
	res := w2.WalkTo(Point{X: 1, Z: 2}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if w2.strikes[Point{X: 4, Z: 2}] < doorAvoidStrike {
		t.Errorf("strikes on the refusing door = %d", w2.strikes[Point{X: 4, Z: 2}])
	}
	if !strings.Contains(strings.Join(f.ops, ","), "1600") {
		t.Errorf("never used the second door: ops %v", f.ops)
	}
}

// A gate that answers with a dialogue is the Barbarian Outpost's west gates (2116/2115),
// reproduced live 2026-09-18: both leaves logged "did not open" and every repath planned the
// identical hop, while the dialogue the click opened stayed up for the rest of the run and
// made the engine refuse every later OPLOC. The walker must dismiss it and route around
// instead of spending its whole repath budget on a crossing that can never succeed.
func TestWalkerDismissesADialogueAGateOpensInsteadOfOpening(t *testing.T) {
	f, _, g := house(t, Point{X: 9, Z: 2})
	f.doors[0].dialog = true
	col := f.plan
	col.blocked[[2]Point{{X: 4, Z: 0}, {X: 5, Z: 0}}] = true
	f.addDoor(Point{X: 4, Z: 0}, Point{X: 5, Z: 0}, 1600)
	g.Edges = append(g.Edges,
		Edge{From: Point{X: 4, Z: 0}, To: Point{X: 5, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
		Edge{From: Point{X: 5, Z: 0}, To: Point{X: 4, Z: 0}, Kind: KindDoor, Cost: 4, LocName: "Door", Action: "Open", LocID: 1600, OpenLocID: -1, LocX: 4, LocZ: 0},
	)
	w2 := NewWalker(NewPlanner(col, g), f)
	var logs []string
	w2.Log = func(s string) { logs = append(logs, s); t.Log(s) }

	res := w2.WalkTo(Point{X: 1, Z: 2}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if f.modal {
		t.Error("the dialogue the gate opened was left standing")
	}
	if f.modalsClosed == 0 {
		t.Error("the walker never dismissed the dialogue")
	}
	if all := strings.Join(logs, "\n"); !strings.Contains(all, "answered with a dialogue instead of opening") {
		t.Errorf("the dialogue refusal was not reported:\n%s", all)
	}
	if !strings.Contains(strings.Join(f.ops, ","), "1600") {
		t.Errorf("never routed around to the second door: ops %v", f.ops)
	}
}

func TestWalkerTriesALaddersOtherStandsAfterCantReach(t *testing.T) {
	// The graph says the ladder at (0,1) is climbed from (1,1); live, it only answers from (0,2).
	col := newGrid().fill(0, 0, 4, 4, 0).fill(0, 0, 3, 3, 1)
	delete(col.walk, Point{X: 0, Z: 1})
	f := newFakeWorld(t, col, Point{X: 4, Z: 4})
	f.ladders = append(f.ladders, &fakeLadder{loc: Point{X: 0, Z: 1}, stand: Point{X: 0, Z: 2}, landing: Point{X: 0, Z: 2, Level: 1}, id: 1750, name: "Ladder", op: "Climb-up"})
	g := &Graph{Edges: []Edge{{From: Point{X: 1, Z: 1}, To: Point{X: 1, Z: 1, Level: 1}, Kind: KindStair, Cost: 6, LocName: "Ladder", Action: "Climb-up", LocID: 1750, OpenLocID: -1, LocX: 0, LocZ: 1}}}
	w := NewWalker(NewPlanner(col, g), f)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	if res := w.WalkTo(Point{X: 3, Z: 3, Level: 1}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if all := strings.Join(logs, "\n"); !strings.Contains(all, "only answered from 0,2,0") {
		t.Errorf("the working stand was not reported:\n%s", all)
	}
}

func TestWalkerGivesUpOnAnUnreachableDestinationWithoutSpinning(t *testing.T) {
	f, w, _ := house(t, Point{X: 9, Z: 2})
	f.plan.fill(30, 30, 32, 32, 0) // a sealed pocket
	start := f.now
	res := w.WalkTo(Point{X: 31, Z: 31}, WalkOptions{})
	if res.Outcome != OutcomeUnreachable {
		t.Fatalf("result = %+v", res)
	}
	elapsed := f.now.Sub(start)
	// Three no-progress passes with backoffs of 2 and 4 ticks, a scene walk and unstick each
	// pass, every rung at least a tick: well over ten seconds of game time, never zero.
	if elapsed < 10*time.Second || elapsed > 5*time.Minute {
		t.Errorf("gave up after %v of game time (%d delays)", elapsed, f.delays)
	}
	if f.delays > 2000 {
		t.Errorf("%d delays: looks like a spin", f.delays)
	}
}

func TestWalkerStopsWhenTheBotStops(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 0})
	f.stopAfter = 3
	res := w.WalkTo(Point{X: 3, Z: 3, Level: 1}, WalkOptions{})
	if res.Outcome != OutcomeStopped {
		t.Fatalf("result = %+v", res)
	}
	if f.delays > 3 {
		t.Errorf("kept going for %d delays after the stop", f.delays)
	}
}

func TestWalkerHonoursRequestRepath(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 4})
	var logs []string
	w.Log = func(s string) { logs = append(logs, s) }
	f.onDelay = func(n int) {
		if n == 2 {
			w.RequestRepath("test")
		}
	}
	if res := w.WalkTo(Point{X: 6, Z: 0}, WalkOptions{}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "repathing (1/5): repath requested") {
		t.Errorf("no repath logged:\n%s", strings.Join(logs, "\n"))
	}
}

func TestWalkerUsesAnNPCHopOnlyWithAHandler(t *testing.T) {
	col := newGrid().fill(0, 0, 3, 3, 0).fill(20, 20, 23, 23, 0)
	f := newFakeWorld(t, col, Point{X: 0, Z: 0})
	g := &Graph{Edges: []Edge{{From: Point{X: 3, Z: 3}, To: Point{X: 20, Z: 20}, Kind: KindShip, Cost: 36, LocName: "Seaman", Action: "Pay-fare", LocID: -1, OpenLocID: -1, LandsOnTile: true}}}
	w := NewWalker(NewPlanner(col, g), f)
	if res := w.WalkTo(Point{X: 22, Z: 22}, WalkOptions{MaxPasses: 1}); res.Arrived() {
		t.Fatalf("arrived without a handler: %+v", res)
	}
	called := 0
	w.NPCHop = func(e *Edge) bool {
		called++
		if f.me != e.From {
			t.Errorf("handler called from %s, not the stand %s", f.me, e.From)
		}
		f.me = e.To
		return true
	}
	if res := w.WalkTo(Point{X: 22, Z: 22}, WalkOptions{}); !res.Arrived() || called != 1 {
		t.Fatalf("result = %+v, handler called %d times", res, called)
	}
}

func TestExpandAndLocate(t *testing.T) {
	tiles := expand([]Point{{X: 0, Z: 0}, {X: 3, Z: 3}, {X: 3, Z: 5}})
	if len(tiles) != 6 || tiles[3] != (Point{X: 3, Z: 3}) || tiles[5] != (Point{X: 3, Z: 5}) {
		t.Fatalf("expand = %v", tiles)
	}
	if i, d := locate(tiles, Point{X: 3, Z: 4}, 0); i != 4 || d != 0 {
		t.Errorf("locate = %d,%d", i, d)
	}
}

// webHouse is house() with the door turned into a slashable web: the same geometry, but the
// crossing now carries Requires.SlashTool, which is what marks a barrier whose refusal is a
// coin flip rather than a reason.
func webHouse(t *testing.T, me Point) (*fakeWorld, *Walker) {
	f, _, g := house(t, me)
	for i := range g.Edges {
		if g.Edges[i].LocID == 1530 {
			g.Edges[i].LocName = "Web"
			g.Edges[i].Requires = &Requires{SlashTool: true}
		}
	}
	w := NewWalker(NewPlanner(f.plan, g), f)
	w.Log = func(s string) { t.Log(s) }
	return f, w
}

// TestWalkerCutsAWebThatFailsSeveralSwings holds crossDoor to content's own odds. web.rs2 cuts
// a web only on random(2) = 1, so five failures in a row is an ordinary run of luck (1 in 32),
// not a broken route. At the two attempts every other door gets, that run of luck reads as a
// refusal.
func TestWalkerCutsAWebThatFailsSeveralSwings(t *testing.T) {
	f, w := webHouse(t, Point{X: 9, Z: 2})
	f.doors[0].refuseTimes = 5
	res := w.WalkTo(Point{X: 1, Z: 2}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("a web that failed five swings must still be cut within %d attempts, got %+v", slashAttempts, res)
	}
}

// TestWalkerNeverBansAWebForGood is the rule a 3h7m CombatTrainer run died on. The strike
// counters live for the Walker's life, so three refused crossings used to put a door out of
// every later plan — and the Varrock sewer web at 3210,9898 is the ONLY way into the western
// sewer, so banning it made the account's own training stand permanently unreachable. It had
// crossed that web a minute earlier. Avoiding it for the rest of THIS walk is still right.
func TestWalkerNeverBansAWebForGood(t *testing.T) {
	f, w := webHouse(t, Point{X: 9, Z: 2})
	f.doors[0].refuse = true // never opens, whatever the dice would have said
	w.WalkTo(Point{X: 1, Z: 2}, WalkOptions{})
	key := Point{X: 4, Z: 2}
	if w.strikes[key] < doorBanStrikes {
		t.Fatalf("the web took only %d strikes; this test needs it to reach %d to say anything", w.strikes[key], doorBanStrikes)
	}
	if w.Banned(key) {
		t.Fatal("the web was banned for the Walker's life: every later plan through that room would answer unreachable, which is exactly how a run ended 51 tiles from its own stand")
	}
}

// A level-up chat left up stops every step; the stalled walk closes it once and arrives, without
// counting the stall as a refused step.
func TestWalkerClosesAModalThatHoldsItStill(t *testing.T) {
	f, w, _ := house(t, Point{X: 14, Z: 0})
	f.modal, f.modalHolds = true, true
	res := w.WalkTo(Point{X: 3, Z: 3, Level: 1}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if f.modalsClosed != 1 {
		t.Errorf("closed the modal %d times, want 1", f.modalsClosed)
	}
}
