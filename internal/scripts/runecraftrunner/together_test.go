package runecraftrunner

// THE ONE-WRITE ACCEPT (CR-2; runecraftplan/together.go) against the engine's rules for a trade's confirm screen, read from trade.rs2:
//
//   - whoever accepts the confirm screen SECOND completes the trade, inside the packet that accepts it (both_moveinv, if_close); the side
//     that accepted first reads "Waiting for other player." and the other's screen reads "Other player has accepted." (actionsim.Trade,
//     PartnerHoldsConfirm);
//   - an op on a loc closes an open trade screen for both players the moment it is DECODED (OpLocUHandler: clearPendingAction); and a message's
//     packets are read in order in one tick, so an Accept that completes the trade lets the use after it through, and one that does not has
//     the trade closed under it by the use (actionsim.SetSplitMessages, SetStrictTrade).
//
// The carrier is the test's: it accepts the confirm screen after a delay of the test's choosing, or closes it in the very tick the crafter
// writes.

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

type togetherRig struct {
	t       *testing.T
	sim     *actionsim.Session
	d       *fleetrunner.Deps
	partner *actionsim.Trade
	a       runecraftplan.Altar
	ruins   locindex.Match

	mu     sync.Mutex
	events []string
	// uses counts the talisman packets the engine read; ignoreUses makes the engine ignore them (a use that did nothing), ignoreFirst only
	// the first (the second is what lands).
	uses                       int
	ignoreUses, ignoreFirstUse bool
	plans                      atomic.Int32
	planAnswer                 bool
	stop                       chan struct{}
}

func newTogetherRig(t *testing.T, essence int) *togetherRig {
	t.Helper()
	withEngineClock(t)
	sim, d := crafterSession(t, 4000, "Acct 1")
	sim.SetStrictTrade(true)
	sim.SetSplitMessages(true)
	a := runecraftplan.Nature
	sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	r := &togetherRig{t: t, sim: sim, d: d, a: a, planAnswer: true, stop: make(chan struct{}),
		partner: &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: essence}}, AcceptOffer: true, PartnerHoldsConfirm: true},
		ruins:   locindex.Match{Loc: locindex.Loc{ID: a.RuinsID, X: a.Ruins.X, Z: a.Ruins.Z, Shape: 10}, Name: runecraftplan.RuinsName, Op: 1}}
	sim.OnSend = r.onSend
	// The view's tick counter runs, as a live session's does.
	go func() {
		tk := time.NewTicker(engTick)
		defer tk.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-tk.C:
				sim.AdvanceTick()
			}
		}
	}()
	t.Cleanup(func() { close(r.stop) })
	return r
}

func (r *togetherRig) note(s string) {
	r.mu.Lock()
	r.events = append(r.events, s)
	r.mu.Unlock()
}

// onSend is the engine's read of one message: what it carried, in order, and the talisman's teleport (two ticks after the use is read, as
// the content script's p_delay(1) and p_telejump land it).
func (r *togetherRig) onSend(b []byte) {
	var ops []string
	use := false
	for _, pkt := range actionsim.Split(b) {
		switch int(pkt[0]) {
		case clientprot.IF_BUTTON:
			if len(pkt) >= 3 && g2(pkt, 1) == actions.TradeAcceptConfirm {
				ops = append(ops, "ACCEPT")
			} else {
				ops = append(ops, "BUTTON")
			}
		case clientprot.OPLOCU:
			ops = append(ops, "USE")
			use = true
		}
	}
	if len(ops) > 0 {
		r.note("msg:" + strings.Join(ops, "+"))
	}
	if use {
		r.mu.Lock()
		r.uses++
		ignore := r.ignoreUses || (r.ignoreFirstUse && r.uses == 1)
		r.mu.Unlock()
		if !ignore {
			time.AfterFunc(engTick, func() { r.sim.StandAt(r.a.Enter.X, r.a.Enter.Z) })
		}
	}
}

func (r *togetherRig) eventsOf() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *togetherRig) indexOf(ev string) int {
	for i, e := range r.eventsOf() {
		if e == ev {
			return i
		}
	}
	return -1
}

func (r *togetherRig) count(ev string) int {
	n := 0
	for _, e := range r.eventsOf() {
		if e == ev {
			n++
		}
	}
	return n
}

// carrierAcceptsAfter has the carrier accept the confirm screen delay after it opens.
func (r *togetherRig) carrierAcceptsAfter(delay time.Duration) {
	tr := actions.NewTrade(r.sim)
	go func() {
		for {
			select {
			case <-r.stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
			if tr.OnConfirmScreen() {
				time.Sleep(delay)
				r.note("carrier accepts")
				r.sim.PartnerAcceptsConfirm(r.partner)
				return
			}
		}
	}()
}

func (r *togetherRig) options() GateOptions {
	return GateOptions{Allowed: allow("acct_1"), Wait: 3 * time.Second,
		Together: &GateTogether{Altar: r.a, Ruins: r.ruins, Plan: func() bool { r.plans.Add(1); return r.planAnswer }}}
}

func (r *togetherRig) run(o GateOptions) GateResult {
	r.t.Helper()
	r.sim.OpenTrade(r.partner)
	return GateStep(r.d, NewGateWatcher(r.d), o)
}

func (r *togetherRig) inside() bool { return r.a.Inside(Here(r.d)) }

func (r *togetherRig) waitInside() bool { return waitUntil(r.d, r.inside, 2*time.Second) }

// The carrier's accept is on the screen when the crafter's wait begins to be looked at: the accept and the use go out as ONE message, the
// accept first, the trade completes in it, the essence is in the pack, nothing was closed, and the account is on its way into the temple.
func TestTheAcceptAndTheTalismanGoOutInOneMessage(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.carrierAcceptsAfter(10 * time.Millisecond)
	res := r.run(r.options())
	if res.Kind != GateReceived || res.Essence != 25 || !res.Together.Sent || res.Together.Talisman == nil || res.Together.WaitedOut {
		t.Fatalf("%+v", res)
	}
	if !res.Together.Planned || res.Together.OtherTick < res.Together.ConfirmTick || res.Together.Talisman.Tick < res.Together.OtherTick {
		t.Errorf("ticks: confirm %d, other accepted %d, written %d", res.Together.ConfirmTick, res.Together.OtherTick, res.Together.Talisman.Tick)
	}
	ev := r.eventsOf()
	if r.count("msg:ACCEPT+USE") != 1 || r.count("msg:ACCEPT") != 0 || r.uses != 1 {
		t.Fatalf("messages %v, uses %d: want one message carrying the accept and the use", ev, r.uses)
	}
	if r.indexOf("carrier accepts") > r.indexOf("msg:ACCEPT+USE") || r.indexOf("carrier accepts") < 0 {
		t.Errorf("the pair went out before the carrier's accept: %v", ev)
	}
	if got := countOf(r.sim.Pack(), essID); got != 25 || !r.sim.TradeDone(r.partner) || r.sim.StrayCloses() != 0 {
		t.Errorf("essence %d, trade done %v, stray closes %d", got, r.sim.TradeDone(r.partner), r.sim.StrayCloses())
	}
	if !r.waitInside() {
		t.Error("the talisman did not land the account in the temple")
	}
	if r.plans.Load() != 1 {
		t.Errorf("the plan was asked %d times for one screen", r.plans.Load())
	}
}

// The carrier has not accepted when the confirm screen opens: the crafter sends NOTHING (no accept on its own: that would make the carrier's
// the completing one and a talisman after it a plain use) until the line shows, then the pair goes out in one message.
func TestTheCrafterWaitsForTheCarriersAcceptBeforeItWritesAnything(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.carrierAcceptsAfter(100 * time.Millisecond) // inside GateTogetherWait (225 ms on this clock)
	res := r.run(r.options())
	if res.Kind != GateReceived || !res.Together.Sent || res.Together.WaitedOut {
		t.Fatalf("%+v", res)
	}
	ev := r.eventsOf()
	if r.indexOf("msg:ACCEPT+USE") < r.indexOf("carrier accepts") || r.indexOf("carrier accepts") < 0 || r.count("msg:ACCEPT") != 0 || r.uses != 1 {
		t.Fatalf("events %v, uses %d: the crafter must write once, after the carrier's accept", ev, r.uses)
	}
	if res.Together.OtherTick <= res.Together.ConfirmTick-1 {
		t.Errorf("the carrier's accept was seen in tick %d, the screen in %d", res.Together.OtherTick, res.Together.ConfirmTick)
	}
	if !r.waitInside() {
		t.Error("the talisman did not land the account in the temple")
	}
}

// A carrier that does not accept in GateTogetherWait is accepted the plain way: the crafter's accept goes out ALONE, no use is written, the
// carrier's later accept completes the trade, and the essence arrives. A slow carrier costs nothing but the plain path.
func TestACarrierThatIsSlowToAcceptIsAcceptedThePlainWay(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.carrierAcceptsAfter(330 * time.Millisecond) // past GateTogetherWait (225 ms on this clock), inside the tests' 300 ms close wait after the plain accept
	res := r.run(r.options())
	if res.Kind != GateReceived || res.Essence != 25 || res.Together.Sent || !res.Together.WaitedOut || res.Together.Why == "" {
		t.Fatalf("%+v", res)
	}
	ev := r.eventsOf()
	if r.count("msg:ACCEPT") != 1 || r.uses != 0 || r.indexOf("msg:ACCEPT") > r.indexOf("carrier accepts") || r.count("msg:ACCEPT+USE") != 0 {
		t.Fatalf("events %v, uses %d: want the accept alone, before the carrier's, and no use", ev, r.uses)
	}
	if r.inside() {
		t.Error("the account went into the temple without a use")
	}
	if got := countOf(r.sim.Pack(), essID); got != 25 || r.sim.StrayCloses() != 0 {
		t.Errorf("essence %d, stray closes %d", got, r.sim.StrayCloses())
	}
}

// The carrier closes the screen in the tick the crafter writes, read first: the accept finds no screen, the use runs, the account lands in
// the temple with NO essence, and the handshake says so -- a failed trade that names the talisman, never a trade that looks done.
func TestACarrierThatClosesInTheSameTickIsAFailedTradeThatSaysTheTalismanWent(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.carrierAcceptsAfter(10 * time.Millisecond)
	r.sim.BeforeSend = func(b []byte) {
		for _, pkt := range actionsim.Split(b) {
			if int(pkt[0]) == clientprot.OPLOCU {
				r.sim.BeforeSend = nil
				r.note("carrier closes")
				r.sim.PartnerClosesTrade()
				r.d.Chat.Record("Other player declined trade.")
				return
			}
		}
	}
	res := r.run(r.options())
	if res.Kind != GateFailed || !res.Together.Sent {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(res.Reason, "talisman") || !strings.Contains(res.Reason, "without the essence") {
		t.Errorf("the reason does not say the talisman went: %q", res.Reason)
	}
	if got := countOf(r.sim.Pack(), essID); got != 0 || r.sim.TradeDone(r.partner) {
		t.Errorf("essence %d, trade done %v", got, r.sim.TradeDone(r.partner))
	}
	if !r.waitInside() {
		t.Error("the use did not run, so the model is not the engine")
	}
}

// A plan that says no accepts at once: no wait for the carrier, no use. The accept goes out before the carrier's.
func TestAPlanThatSaysNoAcceptsAtOnce(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.planAnswer = false
	r.carrierAcceptsAfter(150 * time.Millisecond)
	res := r.run(r.options())
	if res.Kind != GateReceived || res.Together.Sent || res.Together.WaitedOut || res.Together.Planned {
		t.Fatalf("%+v", res)
	}
	if r.count("msg:ACCEPT") != 1 || r.uses != 0 || r.indexOf("msg:ACCEPT") > r.indexOf("carrier accepts") {
		t.Fatalf("events %v, uses %d: want the accept alone, before the carrier's", r.eventsOf(), r.uses)
	}
}

// Another caller holding the account's input (the Guardian handling an event) drops a loc click, so nothing is written together: the accept
// goes out alone, the trade completes, and the result says why.
func TestAHeldInputAcceptsThePlainWay(t *testing.T) {
	r := newTogetherRig(t, 25)
	hold := actions.HoldMovement(r.sim, nil)
	defer hold.Release()
	r.carrierAcceptsAfter(10 * time.Millisecond)
	res := r.run(r.options())
	if res.Kind != GateReceived || res.Together.Sent || !strings.Contains(res.Together.Why, "holds the account's input") {
		t.Fatalf("%+v", res)
	}
	if r.count("msg:ACCEPT") != 1 || r.uses != 0 {
		t.Fatalf("events %v, uses %d", r.eventsOf(), r.uses)
	}
}

// Only a receive of essence goes out together: a courier's hand-over of Natures (theirs offer nothing) never asks the plan.
func TestACourierTradeNeverAsksForTheOneWriteAccept(t *testing.T) {
	r := newTogetherRig(t, 0)
	r.partner.Theirs = nil
	r.partner.PartnerHoldsConfirm = false
	r.partner.AcceptConfirm = true
	o := r.options()
	o.CourierQty = func(name string) int { return 100 }
	res := r.run(o)
	if res.Kind != GateGave || res.Natures != 100 || res.Together.Planned || res.Together.Sent {
		t.Fatalf("%+v", res)
	}
	if r.plans.Load() != 0 || r.uses != 0 {
		t.Fatalf("the plan was asked %d times, %d uses", r.plans.Load(), r.uses)
	}
}

// Without GateOptions.Together the handshake is what it was: the accept the moment the screen matches, never a wait for the carrier.
func TestWithoutTheOptionTheAcceptIsImmediate(t *testing.T) {
	r := newTogetherRig(t, 25)
	r.carrierAcceptsAfter(150 * time.Millisecond)
	o := r.options()
	o.Together = nil
	res := r.run(o)
	if res.Kind != GateReceived || res.Together.Planned || res.Together.Sent {
		t.Fatalf("%+v", res)
	}
	if r.count("msg:ACCEPT") != 1 || r.indexOf("msg:ACCEPT") > r.indexOf("carrier accepts") || r.uses != 0 {
		t.Fatalf("events %v", r.eventsOf())
	}
}

// ---- the use that was written, waited for ----

func shortArrive(t *testing.T) {
	t.Helper()
	old := arriveWait
	arriveWait = 600 * time.Millisecond
	t.Cleanup(func() { arriveWait = old })
}

// A use written with the accept is waited for, not written again: the account is seen inside, one click is counted, and the engine read
// no use from UseTalismanSent.
func TestAWrittenUseIsWaitedForAndNotWrittenAgain(t *testing.T) {
	shortArrive(t)
	r := newTogetherRig(t, 25)
	// The use was written a moment ago (the engine reads it and lands the account one tick later).
	sent := &TalismanSent{Tick: r.d.Session.State().Tick(), At: time.Now()}
	r.onSend(useBytes(t, r))
	res := UseTalismanSent(r.d, r.a, r.ruins, sent)
	if !res.Arrived || res.Clicks != 1 || res.ClickTick != sent.Tick || r.uses != 1 {
		t.Fatalf("%+v, %d uses", res, r.uses)
	}
}

// A written use that did nothing (the engine dropped it): after the wait the use is sent once more, as UseTalisman does, and lands.
func TestAWrittenUseThatDidNothingIsSentOnceMore(t *testing.T) {
	shortArrive(t)
	r := newTogetherRig(t, 25)
	r.ignoreFirstUse = true
	sent := &TalismanSent{Tick: r.d.Session.State().Tick(), At: time.Now()}
	r.onSend(useBytes(t, r)) // the written one: ignored by the engine
	res := UseTalismanSent(r.d, r.a, r.ruins, sent)
	if !res.Arrived || res.Clicks != 2 || r.uses != 2 {
		t.Fatalf("%+v, %d uses", res, r.uses)
	}
}

// A nil sent is UseTalisman: it writes the use itself.
func TestUseTalismanSentWithNothingSentIsUseTalisman(t *testing.T) {
	shortArrive(t)
	r := newTogetherRig(t, 25)
	res := UseTalismanSent(r.d, r.a, r.ruins, nil)
	if !res.Arrived || res.Clicks != 1 || r.uses != 1 {
		t.Fatalf("%+v, %d uses", res, r.uses)
	}
}

// useBytes is the message the one-write accept would have written for the use, as the engine model reads it.
func useBytes(t *testing.T, r *togetherRig) []byte {
	t.Helper()
	scratch := actionsim.New(gateObjs(), nil)
	var out []byte
	scratch.OnSend = func(b []byte) { out = append([]byte(nil), b...) }
	if err := actions.OpLocU(scratch, r.ruins.X, r.ruins.Z, r.ruins.ID, int32(r.a.TalismanID), 0, PlayerPack); err != nil {
		t.Fatal(err)
	}
	return out
}
