package hops

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/webwalk"
)

// quietSession is a Session that sends nothing anywhere and keeps a real gamestate, enough for drive's dialogue reads.
type quietSession struct {
	st     *gamestate.State
	cheats *anticheat.Counters
}

func newQuietSession() *quietSession {
	return &quietSession{st: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (s *quietSession) Send(build func(w *packet.Packet)) error {
	build(packet.NewWriter(32))
	return nil
}
func (s *quietSession) State() *gamestate.State     { return s.st }
func (s *quietSession) Cheats() *anticheat.Counters { return s.cheats }

// gateWorld is the Gnome stronghold gate as one account sees it: a gate to click, and what the click does. crossAfter is
// how many waits after the click the script carries the player through; dialogue instead opens Femi's first-crossing
// talk, which never carries it anywhere.
type gateWorld struct {
	me         webwalk.Point
	now        time.Time
	sess       *quietSession
	crossAfter int
	dialogue   bool
	clicked    atomic.Bool
	waits      int
	stop       atomic.Bool
	pace       time.Duration
}

func (g *gateWorld) Tile() (webwalk.Point, bool)               { return g.me, true }
func (g *gateWorld) Walk(path []webwalk.Point, run bool) error { return nil }
func (g *gateWorld) CantReachSince(time.Time) bool             { return false }
func (g *gateWorld) Now() time.Time                            { return g.now }

func (g *gateWorld) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	return []webwalk.LocMatch{{ID: 190, X: 2459, Z: 3383, Name: "Gate", Op: 1}}
}

func (g *gateWorld) OpLoc(op, x, z, id int) error {
	g.clicked.Store(true)
	g.waits = 0
	if g.dialogue {
		g.sess.st.ChatModalID = 4882
	}
	return nil
}

func (g *gateWorld) Delay(d time.Duration) bool {
	if d <= 0 {
		return !g.stop.Load()
	}
	if g.stop.Load() {
		return false
	}
	g.now = g.now.Add(d)
	if g.pace > 0 {
		time.Sleep(g.pace)
	}
	if g.clicked.Load() && !g.dialogue {
		g.waits++
		if g.waits >= g.crossAfter {
			g.me = webwalk.Point{X: 2461, Z: 3385}
		}
	}
	return true
}

func gnomeGateEdge() *webwalk.Edge {
	return &webwalk.Edge{
		Kind: webwalk.KindGate, LocName: "Gate", Action: "Open", LocID: 190, LocX: 2459, LocZ: 3383, OpenLocID: -1,
		From: webwalk.Point{X: 2461, Z: 3382}, To: webwalk.Point{X: 2461, Z: 3385}, Cost: 4,
		Special: &webwalk.Special{Label: "Gnome Stronghold gate (Femi boxes)", Choose: []string{"OK then"}},
	}
}

func newGateHandler(g *gateWorld) *Handler {
	g.sess = newQuietSession()
	return New(Deps{
		Client: g, Session: g.sess, Log: func(string) {},
		Components: archive.Components{
			4882: {ID: 4882, Children: []int{4883}},
			4883: {ID: 4883, ButtonType: archive.ButtonContinue},
		},
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A full gate makes the next account wait: the gate takes a few accounts at a time (webwalk.LaneCapacity), and an account
// whose turn it is not does not click, and clicks when one of the accounts ahead is through.
func TestTheGateHopWaitsItsTurnThenCrosses(t *testing.T) {
	e := gnomeGateEdge()
	var ahead []*webwalk.LaneTurn
	for i := 0; i < webwalk.LaneCapacity(e); i++ {
		turn, _, _ := webwalk.EnterLane(&gateWorld{me: e.From}, e, nil)
		if turn == nil {
			t.Fatalf("no turn %d on an empty lane", i+1)
		}
		ahead = append(ahead, turn)
	}
	g := &gateWorld{me: e.From, crossAfter: 2, pace: time.Millisecond}
	h := newGateHandler(g)
	done := make(chan bool, 1)
	go func() { done <- h.locHop(e) }()

	waitFor(t, "the account to join the line", func() bool { return webwalk.LaneDepth(e) == len(ahead)+1 })
	time.Sleep(30 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the hop finished while the gate was full")
	default:
	}
	if g.clicked.Load() {
		t.Error("the account clicked the gate out of turn")
	}
	ahead[0].Leave()
	select {
	case ok := <-done:
		if !ok {
			t.Error("the hop failed once it was the account's turn")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the hop never finished after an account ahead left")
	}
	if !g.clicked.Load() {
		t.Error("never clicked the gate")
	}
	for _, turn := range ahead {
		turn.Leave()
	}
	if d := webwalk.LaneDepth(e); d != 0 {
		t.Errorf("the line holds %d after the crossing", d)
	}
}

// The first crossing is Femi's boxes talk, which leaves the gate alone and takes half a minute: the turn is given up when
// the talk starts, so the other 229 accounts are not held behind it.
func TestTheTurnIsGivenUpWhenAFirstCrossingDialogueStarts(t *testing.T) {
	e := gnomeGateEdge()
	g := &gateWorld{me: e.From, dialogue: true, pace: 5 * time.Millisecond}
	h := newGateHandler(g)
	done := make(chan bool, 1)
	go func() { done <- h.locHop(e) }()

	waitFor(t, "the account to click the gate", func() bool { return g.clicked.Load() })
	waitFor(t, "the dialogue to give the turn up", func() bool { return webwalk.LaneDepth(e) == 0 })
	select {
	case <-done:
		t.Fatal("the hop had finished before the dialogue gave up the turn: this proves nothing about the release")
	default:
	}
	// Another account takes the gate while the first is still in the talk.
	other, _, _ := webwalk.EnterLane(&gateWorld{me: e.From}, e, nil)
	if other == nil {
		t.Error("the next account got no turn while the first was in the dialogue")
	}
	other.Leave()

	g.stop.Store(true)
	select {
	case ok := <-done:
		if ok {
			t.Error("a dialogue that never crossed reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the hop did not end after the stop")
	}
	if d := webwalk.LaneDepth(e); d != 0 {
		t.Errorf("the line holds %d at the end", d)
	}
}

// tickGate is the Gnome stronghold gate and the players who cross it, over server ticks the way World.processPlayers runs them
// (gnome_gate.rs2): once a tick, in player order, each player does what its script says next.
//
//   - A click the client sent during a tick is processed at the end of it. If the gate is there the script starts and
//     p_delays; if it is gone the click is dropped (lost).
//   - The next tick the script deletes the gate for five ticks, so a click that is processed after it, in that tick or the
//     four after, finds nothing. Clicks processed in the tick of the first one, by a player ahead of it in order, and in
//     the tick before the delete, all find the gate: that is how a burst of accounts gets through one window.
//   - Three ticks after that the player is carried through and stands on the landing.
//
// A client sees the world as the tick left it: the gate is absent from the tick after the delete for five ticks. Clients run
// between ticks and block in Delay until the next one, so a wave is deterministic but for the order the accounts reach the
// line.
type tickGate struct {
	mu        sync.Mutex
	cond      *sync.Cond
	tick      int
	deletedAt int
	accts     []*tickAcct
	live      int
	waiting   int
	lost      int
	crossed   int
}

type tickAcct struct {
	g      *tickGate
	me     webwalk.Point
	click  bool
	stage  int
	landAt int
}

var tickBase = time.Unix(1000, 0)

func newTickGate(n int, from webwalk.Point) *tickGate {
	g := &tickGate{deletedAt: -100, live: n}
	g.cond = sync.NewCond(&g.mu)
	for i := 0; i < n; i++ {
		g.accts = append(g.accts, &tickAcct{g: g, me: from})
	}
	return g
}

func (a *tickAcct) Tile() (webwalk.Point, bool) {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	return a.me, true
}

func (a *tickAcct) Walk(path []webwalk.Point, run bool) error { return nil }
func (a *tickAcct) CantReachSince(time.Time) bool             { return false }

func (a *tickAcct) Now() time.Time {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	return tickBase.Add(time.Duration(a.g.tick) * tick)
}

func (a *tickAcct) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	if d := a.g.tick - a.g.deletedAt; d >= 1 && d <= 5 {
		return nil
	}
	return []webwalk.LocMatch{{ID: 190, X: q.At.X, Z: q.At.Z, Name: "Gate", Op: 1}}
}

func (a *tickAcct) OpLoc(op, x, z, id int) error {
	a.g.mu.Lock()
	defer a.g.mu.Unlock()
	if a.stage == 0 {
		a.click = true
	}
	return nil
}

// Delay blocks until the server has processed as many ticks as d is long.
func (a *tickAcct) Delay(d time.Duration) bool {
	for n := int((d + tick - 1) / tick); n > 0; n-- {
		g := a.g
		g.mu.Lock()
		target := g.tick + 1
		g.waiting++
		g.cond.Broadcast()
		for g.tick < target {
			g.cond.Wait()
		}
		g.mu.Unlock()
	}
	return true
}

func (g *tickGate) finish() {
	g.mu.Lock()
	g.live--
	g.cond.Broadcast()
	g.mu.Unlock()
}

// step processes one tick for every player, in order.
func (g *tickGate) step(landing webwalk.Point) {
	for _, a := range g.accts {
		switch a.stage {
		case 1:
			g.deletedAt = g.tick
			a.stage, a.landAt = 2, g.tick+3
		case 2:
			if g.tick >= a.landAt {
				a.me, a.stage = landing, 0
				g.crossed++
			}
		default:
			if a.click {
				a.click = false
				if d := g.tick - g.deletedAt; d >= 0 && d <= 4 {
					g.lost++
				} else {
					a.stage = 1
				}
			}
		}
	}
}

// run advances the server, one tick each time every live account is blocked in Delay, until every account is done. It reports
// the ticks it took, or false when limit ticks were not enough.
func (g *tickGate) run(landing webwalk.Point, limit int) (ticks int, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.live > 0 {
		for g.waiting < g.live {
			g.cond.Wait()
		}
		if g.live == 0 {
			break
		}
		if g.tick >= limit {
			return g.tick, false
		}
		g.step(landing)
		g.tick++
		g.waiting = 0
		g.cond.Broadcast()
	}
	return g.tick, true
}

type waveResult struct {
	ticks, lost, failures, worst int
	crossed                      int
}

// runWave sends n accounts at the gate edge e, one every gap ticks (a wave arriving over a few seconds, the shape of the
// 16 Oct one), each repeating the hop as the walker's repaths do until it crosses or has tried 6 times.
func runWave(t *testing.T, e *webwalk.Edge, n, gap, limit int) waveResult {
	t.Helper()
	g := newTickGate(n, e.From)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var res waveResult
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer g.finish()
			a := g.accts[i]
			h := New(Deps{Client: a, Session: newQuietSession(), Log: func(string) {}})
			a.Delay(time.Duration(i*gap) * tick)
			failed := 0
			for attempt := 0; attempt < 6; attempt++ {
				if h.locHop(e) {
					break
				}
				failed++
			}
			mu.Lock()
			res.failures += failed
			res.worst = max(res.worst, failed)
			mu.Unlock()
		}(i)
	}
	ticks, ok := g.run(e.To, limit)
	if !ok {
		t.Fatalf("the wave of %d was still at the gate after %d ticks", n, limit)
	}
	wg.Wait()
	res.ticks, res.lost, res.crossed = ticks, g.lost, g.crossed
	return res
}

// A wave arriving at the gate over a few seconds crosses it in bursts. The gate's script deletes the loc a tick after the
// first click, so accounts that click in that window all get through (82 seconds of the 16 Oct wave had two or more
// arrivals, 20 had three or four, up to 40 a minute): a line that lets one account cross at a time passes about one in
// seven ticks and would take 16 windows for 16 accounts, where three at a time takes 6. Nobody is stranded behind the line,
// and the clicks the line cannot save (a burst of three that straddle a tick) are rare enough that the hop repeated as the
// walker repeats it gets everyone through.
func TestAWaveThroughTheGateCrossesInBurstsNotOneByOne(t *testing.T) {
	e := gnomeGateEdge()
	const n, gap = 16, 2
	res := runWave(t, e, n, gap, 600)
	t.Logf("%d accounts, one every %d ticks: %d ticks, %d clicks lost, %d failed hops (worst %d), %d crossings", n, gap, res.ticks, res.lost, res.failures, res.worst, res.crossed)
	if res.crossed != n {
		t.Errorf("%d of %d crossed", res.crossed, n)
	}
	if res.worst >= 6 {
		t.Errorf("an account failed all its 6 tries")
	}
	if d := webwalk.LaneDepth(e); d != 0 {
		t.Errorf("the line holds %d at the end", d)
	}
	// A window is about seven ticks (click, delete, five gone). Measured over this wave: 41 ticks with the gate's three at a
	// time, 62 with two, 111 with one. 90 fails a line that passes fewer than two a window.
	if res.ticks > 90 {
		t.Errorf("the wave took %d ticks: the gate is passing fewer than three accounts a window", res.ticks)
	}
	// The line is no worse than the free-for-all on clicks that find the gate gone, at the same demand.
	free := *e
	free.LocX = 2000 // a gate nobody queues at
	control := runWave(t, &free, n, gap, 1200)
	t.Logf("without a line: %d ticks, %d clicks lost, %d failed hops (worst %d), %d crossings", control.ticks, control.lost, control.failures, control.worst, control.crossed)
	if res.lost+res.failures > control.lost+control.failures {
		t.Errorf("the line cost more failed clicks (%d lost, %d failed) than none (%d lost, %d failed)", res.lost, res.failures, control.lost, control.failures)
	}
}
