package runecraftrunner

// The trade handshake against a model of the ENGINE, not of one side: a crafter and several carriers, each a simulated session, and an
// engine between them that decodes packets one tick at a time and runs the trade scripts as content 289 does (interface_trade/scripts/
// trade.rs2 [opplayer4], OpPlayerHandler, MoveClickHandler):
//
//   - an op on a player (OPPLAYER1..5) or a walk that is not the walk half of an op calls clearPendingAction() the moment it is
//     DECODED, and with a trade screen open that closes it for BOTH players ("Other player declined trade.");
//   - [opplayer4] is refused when the target is busy ("<name> is busy at the moment."), otherwise tells the asker "Sending trade offer...",
//     and either opens the screens (the target had asked the asker first) or marks the target as asked and sends it "<name>:tradereq:".
//
// Live 16 Oct 2026 one handshake in five was closed 0.4 s after it opened. The first test here asks the way the code did then (every two
// ticks, whatever the game said) and checks the model produces that failure; the others run the handshake as it is now.

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// The test clock: a game tick of 600 ms is engTick (gateClockScale 8). The clock was once 40 times faster (a 15 ms tick), and on a
// loaded machine a goroutine that waits 30 ms for the scheduler is two ticks late, which closed screens the code had nothing to do with.
var (
	engTick = 75 * time.Millisecond
	engLat  = 20 * time.Millisecond // one way, server to client or back
)

type engSide struct {
	name    string
	sim     *actionsim.Session
	d       *fleetrunner.Deps
	chat    *chatlog.Log
	status  int // 1: asked by somebody (^tradestatus_received)
	partner int // the side this one last asked, -1 for nobody
}

type engPacket struct {
	from int
	b    []byte
	at   time.Time
}

// tradeEngine is side 0 (the crafter, who sees carrier i at player index i) and sides 1..n (the carriers, who see the crafter at index 1).
type tradeEngine struct {
	mu    sync.Mutex
	sides []*engSide
	queue []engPacket
	// open is the pair of sides whose screens are open, -1 when none; openedAt when they opened.
	open     [2]int
	openedAt time.Time
	stop     chan struct{}
	// halt ends every side's waits (Deps.Delay answers false): a test that has its trades stops its carriers at once.
	halt atomic.Bool
	wg   sync.WaitGroup
	t0   time.Time

	strays, opens, busyRefusals, declines atomic.Int32
	// log is every packet the engine decoded, as "side:what", for a failure message.
	log []string
	// geom, when set, puts the sides on a map (gate_geometry_test.go): an op reaches its target only as the engine's approach does.
	geom *engGeom
}

func newTradeEngine(t *testing.T, crafter string, carriers ...string) *tradeEngine {
	t.Helper()
	e := &tradeEngine{stop: make(chan struct{}), open: [2]int{-1, -1}, t0: time.Now()}
	mk := func(name string, essence int) *engSide {
		sim := actionsim.New(gateObjs(), []actionsim.Stack{{ID: talismanID, Count: 1}, {ID: coinsID, Count: 100}})
		sim.Give(essID, essence)
		chat := chatlog.New()
		d := &fleetrunner.Deps{Session: sim, Chat: chat, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return !e.halt.Load() }}
		return &engSide{name: name, sim: sim, d: d, chat: chat, partner: -1}
	}
	e.sides = append(e.sides, mk(crafter, 0))
	for _, c := range carriers {
		e.sides = append(e.sides, mk(c, runecraftplan.CarrierOffer))
	}
	for i, s := range e.sides {
		i := i
		s.sim.OnSend = func(b []byte) { e.receive(i, b) }
	}
	e.sides[0].sim.ShowPlayers(carriers...)
	for _, s := range e.sides[1:] {
		s.sim.ShowPlayers(crafter)
	}
	return e
}

func (e *tradeEngine) start() {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		t := time.NewTicker(engTick)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				e.process(time.Now())
			case <-e.stop:
				return
			}
		}
	}()
}

func (e *tradeEngine) close() {
	close(e.stop)
	e.wg.Wait()
}

func isMovePacket(b []byte) bool {
	return len(b) > 0 && (int(b[0]) == clientprot.MOVE_GAMECLICK || int(b[0]) == clientprot.MOVE_MINIMAPCLICK)
}

func (e *tradeEngine) receive(from int, b []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if isMovePacket(b) || isTradeRequest(b) || isDeclinePacket(b) {
		e.queue = append(e.queue, engPacket{from: from, b: append([]byte(nil), b...), at: time.Now().Add(engLat)})
	}
}

// isDeclinePacket is the Decline button of the trade screen (IF_BUTTON on trademain:decline) or a CLOSE_MODAL: either closes the screen for
// both players ([if_close,trademain] returns the offers and tells the partner "Other player declined trade.").
func isDeclinePacket(b []byte) bool {
	switch {
	case len(b) >= 3 && int(b[0]) == clientprot.IF_BUTTON:
		return int(b[1])<<8|int(b[2]) == actions.TradeDecline
	case len(b) >= 1 && int(b[0]) == clientprot.CLOSE_MODAL:
		return true
	}
	return false
}

// targetOf is the side an OPPLAYER packet names: the crafter sees carrier i at index i, a carrier sees the crafter at index 1.
func (e *tradeEngine) targetOf(from int, b []byte) int {
	idx := int(b[len(b)-2])<<8 | int(b[len(b)-1])
	if from == 0 {
		return idx
	}
	return 0
}

func (e *tradeEngine) stamp() string {
	return time.Since(e.t0).Round(time.Millisecond).String() + "@" + strconv.Itoa(int(time.Now().UnixMilli()%100000))
}

func (e *tradeEngine) inOpen(s int) bool { return e.open[0] == s || e.open[1] == s }

// out collects what the tick sends to the clients, delivered together one latency later and in order.
type engOut []func()

func (e *tradeEngine) say(out *engOut, to int, text string) {
	*out = append(*out, func() { e.sides[to].chat.Record(text) })
}

// clearPending is clearPendingAction() on a decoded packet: with a screen open the sender's closes the screen of both.
func (e *tradeEngine) clearPending(out *engOut, from int, why string) {
	if !e.inOpen(from) {
		return
	}
	e.strays.Add(1)
	other := e.open[0]
	if other == from {
		other = e.open[1]
	}
	e.log = append(e.log, e.stamp()+" stray close by "+e.sides[from].name+" ("+why+") "+time.Since(e.openedAt).Round(time.Millisecond).String()+" after the open; its chat "+strings.Join(e.sides[from].chat.Recent(6), " | "))
	e.open = [2]int{-1, -1}
	*out = append(*out, func() { e.sides[other].sim.PartnerClosesTrade(); e.sides[from].sim.PartnerClosesTrade() })
	e.say(out, other, "Other player declined trade.")
}

// decline is a side closing the screen itself (Decline or CLOSE_MODAL): the other side's screen closes too and it is told so. It is the
// decliner's own doing, not a stray packet.
func (e *tradeEngine) decline(out *engOut, from int) {
	if !e.inOpen(from) {
		return
	}
	other := e.open[0]
	if other == from {
		other = e.open[1]
	}
	e.declines.Add(1)
	e.log = append(e.log, e.stamp()+" "+e.sides[from].name+" declines")
	e.open = [2]int{-1, -1}
	*out = append(*out, func() { e.sides[other].sim.PartnerClosesTrade() })
	e.say(out, other, "Other player declined trade.")
}

func (e *tradeEngine) process(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out engOut
	rest := e.queue[:0]
	var due []engPacket
	for _, p := range e.queue {
		if p.at.After(now) {
			rest = append(rest, p)
		} else {
			due = append(due, p)
		}
	}
	e.queue = rest
	for _, p := range due {
		switch {
		case isDeclinePacket(p.b):
			e.decline(&out, p.from)
		case isMovePacket(p.b):
			e.clearPending(&out, p.from, "walk")
			e.geomMove(p.from, p.b)
		case isTradeRequest(p.b):
			e.clearPending(&out, p.from, "request")
			e.ask(&out, p.from, e.targetOf(p.from, p.b))
		}
	}
	// A pair whose screens have both gone (completed or declined) is no longer a trade.
	if e.open[0] >= 0 && now.After(e.openedAt.Add(4*engLat)) && !e.sides[e.open[0]].sim.TradeOpen() && !e.sides[e.open[1]].sim.TradeOpen() {
		e.open = [2]int{-1, -1}
	}
	if len(out) > 0 {
		time.AfterFunc(engLat, func() {
			for _, f := range out {
				f()
			}
		})
	}
}

// ask is [opplayer4] run by side x on side y.
func (e *tradeEngine) ask(out *engOut, x, y int) {
	if y < 0 || y >= len(e.sides) || x == y {
		return
	}
	sx, sy := e.sides[x], e.sides[y]
	e.log = append(e.log, e.stamp()+" "+sx.name+" asks "+sy.name)
	if !e.geomReaches(x, y) {
		return
	}
	if e.inOpen(y) {
		e.busyRefusals.Add(1)
		e.say(out, x, sy.name+" is busy at the moment.")
		return
	}
	e.say(out, x, "Sending trade offer...")
	sx.partner = y
	if sy.partner == x && sx.status == 1 {
		e.opens.Add(1)
		e.log = append(e.log, e.stamp()+" OPEN "+sx.name+" "+sy.name)
		e.open, e.openedAt = [2]int{x, y}, time.Now()
		sx.status, sy.status = 0, 0
		tx := &actionsim.Trade{Partner: sy.name, AcceptOffer: true, AcceptConfirm: true}
		ty := &actionsim.Trade{Partner: sx.name, AcceptOffer: true, AcceptConfirm: true}
		tx.OnMine = func(m []actionsim.Stack) { sy.sim.SetTheirs(m) }
		ty.OnMine = func(m []actionsim.Stack) { sx.sim.SetTheirs(m) }
		*out = append(*out, func() { sx.sim.OpenTrade(tx); sy.sim.OpenTrade(ty) })
		return
	}
	sy.status = 1
	e.say(out, y, sx.name+":tradereq:")
}

// leave takes the crafter out of every carrier's view (he went into the temple) and back.
func (e *tradeEngine) leave() {
	for _, s := range e.sides[1:] {
		s.sim.HidePlayers()
	}
}

func (e *tradeEngine) back() {
	for _, s := range e.sides[1:] {
		s.sim.ShowPlayers(e.sides[0].name)
	}
}

// engineRun is the outcome of one scenario.
type engineRun struct {
	received, closedByStray, failed, trades, refused atomic.Int32
	failures                                         []string
	// sizes are the essence each trade the crafter received brought.
	sizes []int
	mu    sync.Mutex
}

func (r *engineRun) fail(s string) {
	r.mu.Lock()
	r.failures = append(r.failures, s)
	r.mu.Unlock()
}

// crafterLoop is the bot's Gate() loop: GateStep over and over, the temple after every trade (his pack emptied, the carriers lose sight
// of him for temple ticks), the watcher reset on his return.
func (e *tradeEngine) crafterLoop(r *engineRun, stop *atomic.Bool, temple int) {
	s := e.sides[0]
	w := NewGateWatcher(s.d)
	for !stop.Load() {
		res := GateStep(s.d, w, GateOptions{Wait: 3 * engTick})
		switch res.Kind {
		case GateRefused:
			r.refused.Add(1)
		case GateReceived:
			r.received.Add(1)
			r.mu.Lock()
			r.sizes = append(r.sizes, res.Essence)
			r.mu.Unlock()
			e.leave()
			s.sim.Remove(essID, res.Essence)
			time.Sleep(time.Duration(temple) * engTick)
			e.back()
			w.Reset(s.d)
		case GateClosed:
			r.closedByStray.Add(1)
		case GateFailed:
			if !e.halt.Load() {
				r.fail("crafter: " + res.Reason)
			}
		}
	}
}

// carrierLoop is a carrier's trips: trade, walk to the store and back (trip ticks), a fresh load.
func (e *tradeEngine) carrierLoop(side int, r *engineRun, stop *atomic.Bool, trip int, ask func(*fleetrunner.Deps, OfferOptions) OfferResult) {
	s := e.sides[side]
	for !stop.Load() {
		res := ask(s.d, OfferOptions{Crafter: e.sides[0].name, Mode: OfferEssence, Timeout: 400 * engTick, Absent: 0})
		switch {
		case res.Done:
			r.trades.Add(1)
			time.Sleep(time.Duration(trip) * engTick)
			// The shelf tops the pack up to a full load: what the carrier kept from a smaller trade counts toward it.
			s.sim.Give(essID, runecraftplan.CarrierOffer-countOf(s.sim.Pack(), essID))
		case res.Closed:
			r.closedByStray.Add(1)
		case res.NoScreen, res.NotHere:
		default:
			if !e.halt.Load() {
				r.fail(s.name + ": " + res.Reason)
			}
		}
	}
}

// oldAsker is the carrier as it was: ask every two ticks until the screen is up, whatever the game said of the last request.
func oldAsker(d *fleetrunner.Deps, o OfferOptions) OfferResult {
	trade := actions.NewTrade(d.Session)
	started := time.Now()
	var last time.Time
	for time.Since(started) < o.Timeout {
		if trade.Active() {
			return carrierScreen(d, trade, o, OfferResult{})
		}
		if time.Since(last) >= 2*engTick {
			if err := trade.RequestPlayer(o.Crafter); err == nil {
				last = time.Now()
			}
		}
		if !d.Delay(gatePoll) {
			return OfferResult{Reason: "stopped"}
		}
	}
	return OfferResult{NoScreen: true}
}

func withEngineClock(t *testing.T) {
	t.Helper()
	oldWin, oldPoll, oldScale := gateAskWindow, gatePoll, gateClockScale
	oldOffer, oldRetry, oldFlicker := gateOfferWait, gateOfferRetry, gateFlicker
	scale := float64(600*time.Millisecond) / float64(engTick)
	gateAskWindow, gatePoll, gateClockScale = 6*engTick, 4*time.Millisecond, scale
	gateOfferWait, gateOfferRetry, gateFlicker = time.Duration(float64(3*time.Second)/scale), time.Duration(float64(1200*time.Millisecond)/scale), time.Duration(float64(600*time.Millisecond)/scale)
	t.Cleanup(func() {
		gateAskWindow, gatePoll, gateClockScale = oldWin, oldPoll, oldScale
		gateOfferWait, gateOfferRetry, gateFlicker = oldOffer, oldRetry, oldFlicker
	})
}

func runEngine(t *testing.T, carriers int, want int32, ask func(*fleetrunner.Deps, OfferOptions) OfferResult) (*tradeEngine, *engineRun) {
	t.Helper()
	return runEngineWith(t, carriers, want, ask, nil, 60*time.Second)
}

// runEngineWith is runEngine with a setup that runs once the sides exist (a stray in the crafter's pack, say) and a deadline.
func runEngineWith(t *testing.T, carriers int, want int32, ask func(*fleetrunner.Deps, OfferOptions) OfferResult, setup func(e *tradeEngine), deadline time.Duration) (*tradeEngine, *engineRun) {
	t.Helper()
	withEngineClock(t)
	names := make([]string, carriers)
	for i := range names {
		names[i] = "Swarm " + string(rune('1'+i))
	}
	e := newTradeEngine(t, "Crafter", names...)
	if setup != nil {
		setup(e)
	}
	e.start()
	defer e.close()
	r := &engineRun{}
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); e.crafterLoop(r, &stop, 8) }()
	for i := range names {
		i := i
		wg.Add(1)
		go func() { defer wg.Done(); e.carrierLoop(i+1, r, &stop, 14+5*i, ask) }()
	}
	limit := time.Now().Add(deadline)
	for r.received.Load() < want && time.Now().Before(limit) {
		time.Sleep(10 * time.Millisecond)
	}
	stop.Store(true)
	e.halt.Store(true)
	wg.Wait()
	return e, r
}

// The model reproduces what the live fleet saw. Carriers that ask every two ticks, whatever the game said, close screens that have just
// opened: with three carriers in the queue a good share of the handshakes is closed under them.
func TestTheEngineModelClosesScreensForABlindAsker(t *testing.T) {
	e, r := runEngine(t, 3, 10, oldAsker)
	t.Logf("received %d, stray closes %d, closed under the code %d, opens %d, busy refusals %d", r.received.Load(), e.strays.Load(), r.closedByStray.Load(), e.opens.Load(), e.busyRefusals.Load())
	if e.strays.Load() == 0 {
		t.Fatalf("the model never closed a screen for a carrier that asks every two ticks; its log: %v", e.log)
	}
}

// With the quiet rule no handshake is closed: three carriers queueing for one crafter who spends twelve ticks in the temple between
// trades, thirty trades, and not one stray close.
func TestNoHandshakeIsClosedUnderTheCarriers(t *testing.T) {
	e, r := runEngine(t, 3, 14, TradeWithCrafter)
	t.Logf("received %d, stray closes %d, closed %d, opens %d, busy refusals %d", r.received.Load(), e.strays.Load(), r.closedByStray.Load(), e.opens.Load(), e.busyRefusals.Load())
	if r.received.Load() < 14 {
		t.Fatalf("only %d trades in the time allowed; failures %v; log %v", r.received.Load(), r.failures, e.log)
	}
	if e.strays.Load() != 0 || r.closedByStray.Load() != 0 || len(r.failures) != 0 {
		t.Fatalf("stray closes %d, closed %d, failures %v\nlog %v", e.strays.Load(), r.closedByStray.Load(), r.failures, e.log)
	}
	if r.trades.Load() < 13 {
		t.Fatalf("the carriers counted %d done trades for %d received", r.trades.Load(), r.received.Load())
	}
}

// A single carrier on its own and eight in a row: the same.
func TestTheHandshakeHoldsForOneCarrierAndForEight(t *testing.T) {
	for _, n := range []int{1, 8} {
		e, r := runEngine(t, n, 6, TradeWithCrafter)
		if e.strays.Load() != 0 || r.closedByStray.Load() != 0 || len(r.failures) != 0 || r.received.Load() < 6 {
			t.Fatalf("%d carrier(s): received %d, stray %d, closed %d, failures %v\nlog %v", n, r.received.Load(), e.strays.Load(), r.closedByStray.Load(), r.failures, e.log)
		}
	}
}

func TestEngineModelSelfCheck(t *testing.T) {
	// The model's own rule: the asker opens only when the target had asked it; a request to a player in an open trade is refused.
	e := newTradeEngine(t, "Crafter", "Acct 1", "Acct 2")
	var out engOut
	e.ask(&out, 1, 0)
	if e.sides[0].status != 1 || e.opens.Load() != 0 {
		t.Fatal("the first request marks the target, it does not open")
	}
	e.ask(&out, 0, 1)
	if e.opens.Load() != 1 || !e.inOpen(0) || !e.inOpen(1) {
		t.Fatal("the answer opens the screens")
	}
	e.ask(&out, 2, 0)
	if e.busyRefusals.Load() != 1 {
		t.Fatal("a request to a player in an open trade is refused as busy")
	}
	for _, f := range out {
		f()
	}
	if !strings.Contains(strings.Join(e.sides[2].chat.Recent(5), "|"), "busy at the moment") {
		t.Fatalf("the refused asker is told: %v", e.sides[2].chat.Recent(5))
	}
}

// THE CRAFTER'S ROOM (16 Oct 2026): a Strange fruit from a random event takes one of his pack slots, so he has room for 24 essence, not
// 25. Carriers that offer everything (an Offer-All of 25) are declined, every time, for as long as the fruit stays.
func crafterWithStrays(n int) func(e *tradeEngine) {
	return func(e *tradeEngine) {
		e.sides[0].sim.Give(NatureID, 4000) // the gate pack: the talisman and the fare are in the model already, and the Nature stack
		for i := 0; i < n; i++ {
			e.sides[0].sim.Give(int32(2000+i), 1)
		}
	}
}

// The control: carriers that do not follow his room are declined, again and again, and nothing is traded.
func TestACrafterWithAStrayDeclinesCarriersThatOfferEverything(t *testing.T) {
	e, r := runEngineWith(t, 2, 3, TradeWithCrafter, crafterWithStrays(1), 6*time.Second)
	t.Logf("received %d, refused %d, declines %d, failures %v", r.received.Load(), r.refused.Load(), e.declines.Load(), r.failures)
	if r.received.Load() != 0 || r.refused.Load() == 0 {
		t.Fatalf("the model did not decline the Offer-All of 25 into 24 free slots: received %d refused %d\nlog %v", r.received.Load(), r.refused.Load(), e.log)
	}
}

// Carriers that follow the room he says (his gate message, here read off his pack) trade 24 and keep the rest: no decline at all, and the
// handshake holds with one carrier, three and eight, with one stray in the pack and with three.
func TestCarriersFollowTheRoomOfACrafterWithStrays(t *testing.T) {
	for _, c := range []struct{ carriers, strays, room int }{{1, 1, 24}, {3, 1, 24}, {8, 1, 24}, {3, 3, 22}} {
		var crafterDeps *fleetrunner.Deps
		ask := func(d *fleetrunner.Deps, o OfferOptions) OfferResult {
			o.Cap = func() int { return TradeCapacity(crafterDeps) }
			return TradeWithCrafter(d, o)
		}
		e, r := runEngineWith(t, c.carriers, 8, ask, func(e *tradeEngine) {
			crafterWithStrays(c.strays)(e)
			crafterDeps = e.sides[0].d
			if got := TradeCapacity(crafterDeps); got != c.room {
				t.Fatalf("the crafter's room is %d, want %d", got, c.room)
			}
		}, 60*time.Second)
		if r.received.Load() < 8 {
			t.Fatalf("%d carrier(s), %d stray(s): only %d trades; failures %v; log %v", c.carriers, c.strays, r.received.Load(), r.failures, e.log)
		}
		if r.refused.Load() != 0 || e.declines.Load() != 0 || e.strays.Load() != 0 || r.closedByStray.Load() != 0 || len(r.failures) != 0 {
			t.Fatalf("%d carrier(s), %d stray(s): refused %d, declines %d, stray closes %d, closed %d, failures %v\nlog %v", c.carriers, c.strays, r.refused.Load(), e.declines.Load(),
				e.strays.Load(), r.closedByStray.Load(), r.failures, e.log)
		}
		r.mu.Lock()
		for i, n := range r.sizes {
			if n != c.room {
				t.Errorf("%d carrier(s), %d stray(s): trade %d brought %d essence, want %d", c.carriers, c.strays, i+1, n, c.room)
			}
		}
		r.mu.Unlock()
	}
}
