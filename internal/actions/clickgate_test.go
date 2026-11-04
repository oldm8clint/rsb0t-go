package actions

import (
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// gateRig is a fake session whose server ticks when told to, a Waiter that spends virtual time (every Delay of d advances the
// clock, and the tick counter with it), and a record of the tick each click was sent in.
type gateRig struct {
	f       *fakeSession
	clock   time.Duration
	sentAt  []int // the game tick of each packet sent
	delays  int
	stopped bool
	// onTick, when set, runs after each tick's PLAYER_INFO: the server's side of a scenario.
	onTick func()
}

func newGateRig() *gateRig {
	g := &gateRig{f: newFakeSession()}
	g.f.afterSend = func(*fakeSession) { g.sentAt = append(g.sentAt, g.f.state.Tick()) }
	return g
}

func (g *gateRig) tick() {
	g.f.state.Handle(serverprot.PLAYER_INFO, []byte{0})
	if g.onTick != nil {
		g.onTick()
	}
}

func (g *gateRig) advance(d time.Duration) {
	before := g.clock / sleepTick
	g.clock += d
	for n := g.clock/sleepTick - before; n > 0; n-- {
		g.tick()
	}
}

func (g *gateRig) Delay(d time.Duration) bool {
	g.delays++
	if g.stopped {
		return false
	}
	g.advance(d)
	return true
}
func (g *gateRig) DelayUntil(cond func() bool, timeout time.Duration) bool {
	for t := time.Duration(0); t < timeout; t += 100 * time.Millisecond {
		if cond() {
			return true
		}
		if !g.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}
func (g *gateRig) DelayTicks(n int) bool { return g.Delay(time.Duration(n) * sleepTick) }

// perTick is the most packets sent within one game tick.
func (g *gateRig) perTick() int {
	most, counts := 0, map[int]int{}
	for _, t := range g.sentAt {
		counts[t]++
		most = max(most, counts[t])
	}
	return most
}

// minGap is the fewest game ticks between two consecutive packets.
func (g *gateRig) minGap() int {
	gap := 1 << 30
	for i := 1; i < len(g.sentAt); i++ {
		gap = min(gap, g.sentAt[i]-g.sentAt[i-1])
	}
	return gap
}

func TestAClickOnATargetWaitsTwoTicksButAnotherTargetDoesNot(t *testing.T) {
	g := newGateRig()
	s := g.f
	if ticks, _ := ClickWait(s, LocKey(1, 2, 3)); ticks != 0 {
		t.Fatalf("a first click waits %d tick(s)", ticks)
	}
	MarkClick(s, LocKey(1, 2, 3))
	if ticks, _ := ClickWait(s, LocKey(1, 2, 3)); ticks != ClickGapTicks {
		t.Fatalf("right after a click the next on the same target waits %d tick(s), want %d", ticks, ClickGapTicks)
	}
	if ticks, _ := ClickWait(s, LocKey(1, 2, 4)); ticks != 0 {
		t.Errorf("a click on another loc waits %d tick(s); switching targets must never be held back", ticks)
	}
	if ticks, _ := ClickWait(s, NpcKey(3)); ticks != 0 {
		t.Errorf("a click on an NPC waits %d tick(s)", ticks)
	}
	g.tick()
	if ticks, _ := ClickWait(s, LocKey(1, 2, 3)); ticks != ClickGapTicks-1 {
		t.Errorf("one tick later the wait is %d, want %d", ticks, ClickGapTicks-1)
	}
	g.tick()
	if ticks, _ := ClickWait(s, LocKey(1, 2, 3)); ticks != 0 {
		t.Errorf("two ticks later the wait is still %d", ticks)
	}
}

func TestTheGateIsPerSession(t *testing.T) {
	a, b := newGateRig(), newGateRig()
	MarkClick(a.f, NpcKey(7))
	if ticks, _ := ClickWait(b.f, NpcKey(7)); ticks != 0 {
		t.Errorf("another account's click held this one for %d tick(s)", ticks)
	}
}

func TestAStalledTickCounterDoesNotHoldAClickForEver(t *testing.T) {
	g := newGateRig()
	MarkClick(g.f, NpcKey(7))
	if ticks, wall := ClickWait(g.f, NpcKey(7)); ticks <= 0 || wall <= 0 {
		t.Fatalf("right after a click: %d ticks, %v", ticks, wall)
	}
	// The tick counter has not moved (a link that stopped); the wall clock has.
	clickGates.mu.Lock()
	gate := clickGates.m[g.f.State()]
	m := gate.last[NpcKey(7)]
	m.at = m.at.Add(-2 * clickGapWall)
	gate.last[NpcKey(7)] = m
	clickGates.mu.Unlock()
	if ticks, wall := ClickWait(g.f, NpcKey(7)); ticks != 0 || wall != 0 {
		t.Errorf("after the wall gap the click still waits %d tick(s) / %v", ticks, wall)
	}
}

func TestWaitClickGapSpendsTheGapThroughTheWaiter(t *testing.T) {
	g := newGateRig()
	MarkClick(g.f, NpcKey(1))
	if !WaitClickGap(g.f, g, NpcKey(1), nil) {
		t.Fatal("a stop was reported")
	}
	if want := ClickGapTicks * int(sleepTick/clickPoll); g.delays != want {
		t.Errorf("the gap took %d poll(s) of %v, want %d", g.delays, clickPoll, want)
	}
	if ticks, _ := ClickWait(g.f, NpcKey(1)); ticks != 0 {
		t.Errorf("after the gap the next click still waits %d tick(s)", ticks)
	}
}

func TestAStopDuringTheGapStopsTheClick(t *testing.T) {
	g := newGateRig()
	g.stopped = true
	MarkClick(g.f, NpcKey(1))
	if WaitClickGap(g.f, g, NpcKey(1), nil) {
		t.Error("the gap said go though the caller was stopped")
	}
	sends := 0
	err := GatedClick(g.f, g, NpcKey(1), nil, func() error { sends++; return nil })
	if err != ErrClickStopped || sends != 0 {
		t.Errorf("a stopped gated click returned %v after %d send(s), want ErrClickStopped and none", err, sends)
	}
}

func TestAHotLoopOfGatedClicksSendsOnePerTwoTicks(t *testing.T) {
	g := newGateRig()
	for i := 0; i < 40; i++ {
		if err := ClickLoc(g.f, g, 1, 3200, 3200, 1234); err != nil {
			t.Fatal(err)
		}
	}
	if len(g.sentAt) != 40 {
		t.Fatalf("%d packets sent for 40 clicks", len(g.sentAt))
	}
	if got := g.perTick(); got != 1 {
		t.Errorf("%d clicks in one tick; a loop with nothing to wait on must not get more than one", got)
	}
	if got := g.minGap(); got < ClickGapTicks {
		t.Errorf("two clicks %d tick(s) apart, want at least %d", got, ClickGapTicks)
	}
}

func TestAHotLoopAlternatingTwoTargetsStaysUnderTheEnginesRead(t *testing.T) {
	g := newGateRig()
	for i := 0; i < 40; i++ {
		var err error
		if i%2 == 0 {
			err = ClickLoc(g.f, g, 1, 3200, 3200, 1234)
		} else {
			err = ClickNpc(g.f, g, 1, 77)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// Each target is held to one click per two ticks, so two targets make one a tick: far under the five the engine reads.
	if got := g.perTick(); got > 2 {
		t.Errorf("%d clicks in one tick from two targets", got)
	}
}

func TestSwitchingTargetsIsNotHeldBack(t *testing.T) {
	g := newGateRig()
	if err := ClickLoc(g.f, g, 1, 10, 10, 1); err != nil {
		t.Fatal(err)
	}
	before := g.delays
	if err := ClickLoc(g.f, g, 1, 20, 20, 1); err != nil { // the next tree, as after an Ent
		t.Fatal(err)
	}
	if g.delays != before {
		t.Errorf("a click on another tree waited (%d delay(s)); the Ent rule is that the next click is not slowed", g.delays-before)
	}
}

func TestAHeldClickIsReportedOnceNotPerPoll(t *testing.T) {
	g := newGateRig()
	MarkClick(g.f, NpcKey(5))
	reports := 0
	for i := 0; i < 5; i++ {
		_ = GatedClick(g.f, g, NpcKey(5), func(int) { reports++ }, func() error { return nil })
	}
	if reports != 1 {
		t.Errorf("the held hook ran %d times for 5 held clicks; it is rate limited to one in 30 s per session", reports)
	}
}

func TestAFailedSendDoesNotMarkTheClick(t *testing.T) {
	g := newGateRig()
	g.f.sendErr = errBoom
	if err := ClickNpc(g.f, g, 1, 9); err == nil {
		t.Fatal("the send error was swallowed")
	}
	if ticks, _ := ClickWait(g.f, NpcKey(9)); ticks != 0 {
		t.Errorf("a click that never went out holds the next for %d tick(s)", ticks)
	}
}

func TestTheGatedPrimitivesSendTheSamePacketsAsThePlainOnes(t *testing.T) {
	g := newGateRig()
	plain := newFakeSession()
	plain.cheats = &anticheat.Counters{}
	g.f.cheats = &anticheat.Counters{}
	if err := ClickNpc(g.f, g, 2, 31); err != nil {
		t.Fatal(err)
	}
	if err := OpNpc(plain, 2, 31); err != nil {
		t.Fatal(err)
	}
	if string(g.f.sent[0]) != string(plain.sent[0]) {
		t.Errorf("ClickNpc sent % x, OpNpc % x", g.f.sent[0], plain.sent[0])
	}
	if g.f.sent[0][0] != byte(clientprot.OPNPC2) {
		t.Errorf("opcode %d", g.f.sent[0][0])
	}
}
