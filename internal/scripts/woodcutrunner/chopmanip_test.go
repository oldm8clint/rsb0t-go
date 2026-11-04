package woodcutrunner

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/chopmanipplan"
)

// recSession records every Send's bytes, the same way the actions package's own fake does.
type recSession struct {
	state  *gamestate.State
	cheats *anticheat.Counters
	sent   [][]byte
}

func newRecSession() *recSession {
	return &recSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (f *recSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	f.sent = append(f.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (f *recSession) State() *gamestate.State     { return f.state }
func (f *recSession) Cheats() *anticheat.Counters { return f.cheats }

// objects of the lab and the live server (content/pack/obj.pack).
const (
	tKnife = 946
	tLogs  = 1511
	tOak   = 1521
	tYew   = 1515
)

var testNames = map[int32]string{tKnife: "Knife", tLogs: "Logs", tOak: "Oak logs", tYew: "Yew logs", 1359: "Rune axe"}

// testName treats ids from 100000 as the bank note of id-100000.
func testName(id int32) (string, bool, bool) {
	if id >= 100000 {
		n, ok := testNames[id-100000]
		return n, true, ok
	}
	n, ok := testNames[id]
	return n, false, ok
}

func pack(s *gamestate.State, items ...int32) {
	p := packet.NewWriter(64)
	p.P2(PlayerPack)
	p.P2(uint16(len(items)))
	for _, id := range items {
		if id != 0 {
			p.P2(uint16(id + 1))
		} else {
			p.P2(0)
		}
		p.P1(1)
	}
	s.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

func setFletching(s *gamestate.State, level int) {
	p := packet.NewWriter(8)
	p.P1(fletchingStat)
	p.P4(0)
	p.P1(byte(level))
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

func newRun(sess *recSession) *manipRun {
	d := &Deps{Session: sess, Delay: RealDelay, Manip: &ManipConfig{Enabled: true}}
	r := startManip(d, locindex.Match{Loc: locindex.Loc{ID: 1281, X: 2719, Z: 3481}, Op: 1})
	r.name = testName
	return r
}

func TestStartManipIsNilWhenTheSettingIsOff(t *testing.T) {
	sess := newRecSession()
	if startManip(&Deps{Session: sess}, locindex.Match{}) != nil {
		t.Fatal("no Manip config must mean no manipulation")
	}
	if startManip(&Deps{Session: sess, Manip: &ManipConfig{}}, locindex.Match{}) != nil {
		t.Fatal("Enabled false must mean no manipulation")
	}
	r := startManip(&Deps{Session: sess, Manip: &ManipConfig{Enabled: true}}, locindex.Match{})
	if r == nil {
		t.Fatal("Enabled true did not start one")
	}
	r.watch.stop()
}

func TestPickBatchItemsNeedsAKnifeAndAUsableLog(t *testing.T) {
	items := func(ids ...int32) []gamestate.SlotItem {
		var out []gamestate.SlotItem
		for i, id := range ids {
			out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: id, Count: 1}})
		}
		return out
	}
	cases := []struct {
		name      string
		ids       []int32
		fletching int
		ok        bool
		log       int32
		comp      int
	}{
		{"plain log and knife", []int32{1359, tKnife, tLogs}, 1, true, tLogs, chopmanipplan.Make1Shafts},
		{"no knife", []int32{1359, tLogs}, 99, false, 0, 0},
		{"no log", []int32{1359, tKnife}, 99, false, 0, 0},
		{"oak and the level for the shortbow", []int32{tKnife, tOak}, 20, true, tOak, chopmanipplan.Make1Bow},
		{"oak and one level short", []int32{tKnife, tOak}, 19, false, 0, 0},
		{"plain log wins over oak", []int32{tKnife, tOak, tLogs}, 99, true, tLogs, chopmanipplan.Make1Shafts},
		{"noted logs cannot be cut", []int32{tKnife, 100000 + tLogs}, 99, false, 0, 0},
		{"a noted knife is not a knife", []int32{100000 + tKnife, tLogs}, 99, false, 0, 0},
	}
	for _, c := range cases {
		got, ok := pickBatchItems(items(c.ids...), testName, c.fletching)
		if ok != c.ok {
			t.Errorf("%s: ok %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (got.logID != c.log || got.carrier.MakeComp != c.comp || got.knifeID != tKnife) {
			t.Errorf("%s: got %+v, want log %d button %d", c.name, got, c.log, c.comp)
		}
	}
}

func TestOneBatchIsOneSendHoldingKnifeOnLogMake1CloseModalAndTheTreeClick(t *testing.T) {
	sess := newRecSession()
	pack(sess.state, 1359, tKnife, tLogs)
	setFletching(sess.state, 1)
	r := newRun(sess)
	defer r.watch.stop()
	if !r.write(10) {
		t.Fatal("the batch was not written")
	}
	if len(sess.sent) != 1 {
		t.Fatalf("a batch made %d sends, want one so the engine reads the three packets in the same tick", len(sess.sent))
	}
	// The one message is the three actions' own bytes back to back, in order.
	ref := newRecSession()
	_ = actions.OpHeldU(ref, tLogs, 2, PlayerPack, tKnife, 1, PlayerPack)
	_ = actions.IfButton(ref, chopmanipplan.Make1Shafts)
	_ = actions.CloseModal(ref)
	_ = actions.OpLoc(ref, 1, 2719, 3481, 1281)
	want := bytes.Join(ref.sent, nil)
	if !bytes.Equal(sess.sent[0], want) {
		t.Fatalf("batch bytes % x\nwant         % x", sess.sent[0], want)
	}
	if r.st.ctl.Stats().Batches != 1 || r.wrote != 1 {
		t.Fatalf("the controller was not told: %s, wrote %d", r.st.ctl.Stats().Line(), r.wrote)
	}
}

func TestNoBatchWithoutAKnifeAndItIsSaidOnce(t *testing.T) {
	sess := newRecSession()
	pack(sess.state, 1359, tLogs)
	var lines []string
	d := &Deps{Session: sess, Delay: RealDelay, Manip: &ManipConfig{Enabled: true}, Log: func(s string) { lines = append(lines, s) }}
	r := startManip(d, locindex.Match{Op: 1})
	r.name = testName
	defer r.watch.stop()
	for i := 0; i < 3; i++ {
		if r.write(5 + i) {
			t.Fatal("a batch went out without a knife")
		}
	}
	if len(sess.sent) != 0 {
		t.Fatalf("%d packets sent", len(sess.sent))
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "no knife") {
		t.Fatalf("log lines %q, want the reason said once", lines)
	}
	if r.open() {
		t.Fatal("open reported a batch with nothing written")
	}
}

// The lab's yew camp (a carrier fletched into a shortbow): a tree falls with the batch written after its last swing still
// on its way, the next Chop call starts on an adjacent tree in the same tick, and two batches would be read together.
func TestNoOpeningBatchBesideTheLastChopsBatchStillInFlight(t *testing.T) {
	sess := newRecSession()
	pack(sess.state, 1359, tKnife, tLogs)
	setFletching(sess.state, 1)
	st := NewManipState()
	d := &Deps{Session: sess, Delay: RealDelay, Manip: &ManipConfig{Enabled: true, State: st}}
	m := locindex.Match{Loc: locindex.Loc{ID: 1281, X: 2719, Z: 3481}, Op: 1}
	first := startManip(d, m)
	first.name = testName
	if !first.open() {
		t.Fatal("the first chop's opening batch was not written")
	}
	first.watch.stop()
	second := startManip(d, m)
	second.name = testName
	defer second.watch.stop()
	sent := len(sess.sent)
	if second.open() {
		t.Fatal("an opening batch was written beside the previous chop's, still unread")
	}
	if len(sess.sent) != sent {
		t.Fatalf("%d packets went out for the second chop's opening", len(sess.sent)-sent)
	}
}

func TestNoBatchWhileAnotherCallerHoldsTheAccountsMovement(t *testing.T) {
	sess := newRecSession()
	pack(sess.state, 1359, tKnife, tLogs)
	setFletching(sess.state, 1)
	r := newRun(sess)
	defer r.watch.stop()
	hold := actions.HoldMovement(sess, nil)
	if r.write(10) {
		t.Fatal("a batch went out while a random event's solver held the account's clicks")
	}
	if len(sess.sent) != 0 {
		t.Fatalf("%d packets reached the server through the hold; a knife and a Make 1 without the tree click would fletch the carrier", len(sess.sent))
	}
	if r.st.ctl.Stats().Batches != 0 {
		t.Fatal("the controller counted a batch that was never written")
	}
	hold.Release()
	if !r.write(11) {
		t.Fatal("the batch stayed refused after the hold was released")
	}
}

// fakeSource is a tickSource scripted by the test.
type fakeSource struct {
	sub       gamestate.Subscriber
	tick      int
	anim, at  int
	cancelled bool
}

func (f *fakeSource) Subscribe(fn gamestate.Subscriber) func() {
	f.sub = fn
	return func() { f.cancelled = true }
}
func (f *fakeSource) Tick() int            { return f.tick }
func (f *fakeSource) SelfAnim() (int, int) { return f.anim, f.at }
func (f *fakeSource) emit(tick, anim, animTick int) {
	f.tick, f.anim, f.at = tick, anim, animTick
	f.sub(gamestate.Event{Kind: gamestate.EventTick, Tick: tick})
}

func TestTickWatchQueuesEveryTickWithItsOwnAnimation(t *testing.T) {
	src := &fakeSource{}
	w := newTickWatch(src)
	d := &Deps{Delay: RealDelay}
	src.emit(10, 867, 10)  // an animation that arrived with this tick
	src.emit(11, 867, 10)  // the animation of the tick before is not this tick's
	src.emit(12, 1248, 12) // a fletch
	src.sub(gamestate.Event{Kind: gamestate.EventChatMessage, Text: "x"})
	want := []chopmanipplan.Obs{{N: 10, Anim: 867}, {N: 11, Anim: chopmanipplan.NoAnim}, {N: 12, Anim: 1248}}
	for i, w2 := range want {
		got, ok, stopped := w.next(d, time.Second)
		if !ok || stopped || got != w2 {
			t.Fatalf("observation %d: %+v ok %v stopped %v, want %+v", i, got, ok, stopped, w2)
		}
	}
	if _, ok, _ := w.next(d, 30*time.Millisecond); ok {
		t.Fatal("a chat event or an empty queue produced an observation")
	}
	w.stop()
	if !src.cancelled {
		t.Fatal("stop did not cancel the subscription")
	}
}

func TestTickWatchWakesOnTheTickAndStopsWhenAsked(t *testing.T) {
	src := &fakeSource{}
	w := newTickWatch(src)
	d := &Deps{Delay: RealDelay}
	go func() {
		time.Sleep(20 * time.Millisecond)
		src.emit(5, chopmanipplan.NoAnim, 0)
	}()
	start := time.Now()
	if o, ok, _ := w.next(d, 2*time.Second); !ok || o.N != 5 || time.Since(start) > time.Second {
		t.Fatalf("did not wake on the tick: %+v ok %v after %v", o, ok, time.Since(start))
	}
	stopNow := &Deps{Delay: func(time.Duration) bool { return false }}
	if _, ok, stopped := w.next(stopNow, 2*time.Second); ok || !stopped {
		t.Fatalf("a stop request was not honoured: ok %v stopped %v", ok, stopped)
	}
}

func TestPaceIgnoresARollTheStateHasMovedPast(t *testing.T) {
	sess := newRecSession()
	pack(sess.state, 1359, tKnife, tLogs)
	setFletching(sess.state, 1)
	r := newRun(sess)
	r.watch.stop() // replace the live feed with a scripted one
	src := &fakeSource{}
	r.watch = newTickWatch(src)
	defer r.watch.stop()
	r.st.ctl.BeginChop(9)

	// The real state stands at tick 0, the observation says tick 10: the roll is old news, and a batch written now
	// would be read after the stale tick it was meant for, so none is.
	src.emit(10, 867, 10)
	if r.pace() {
		t.Fatal("pace reported a stop")
	}
	if len(sess.sent) != 0 {
		t.Fatalf("a batch was written for a roll the state had already moved past: %d sends", len(sess.sent))
	}
}

func TestReportFoldsTheCallsAndSpeaksAtTheThreshold(t *testing.T) {
	sess := newRecSession()
	var lines []string
	d := &Deps{Session: sess, Delay: RealDelay, Log: func(s string) { lines = append(lines, s) }}
	st := NewManipState()
	st.note(true, 30, 90)
	st.note(false, 10, 40)
	st.report(d)
	if len(lines) != 0 {
		t.Fatalf("reported below the threshold: %q", lines)
	}
	st.note(true, 60, 480)
	st.report(d)
	if len(lines) != 1 {
		t.Fatalf("%d lines at the threshold: %q", len(lines), lines)
	}
	for _, want := range []string{"90 logs in 570 ticks with batches = 15.8 per 100 ticks", "10 logs in 40 ticks without = 25.0 per 100 ticks", "0 batches"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("report line %q lacks %q", lines[0], want)
		}
	}
	if st.manipTicks != 0 || st.plainTicks != 0 || st.manipLogs != 0 || st.plainLogs != 0 {
		t.Fatalf("the tallies were not reset: %+v", st)
	}
}

func TestPer100(t *testing.T) {
	if per100(5, 0) != 0 || per100(0, 100) != 0 || per100(33, 100) != 33 || per100(1, 4) != 25 {
		t.Fatal("per100 is wrong")
	}
}
