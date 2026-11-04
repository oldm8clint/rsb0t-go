package fisherrunner

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/fishmanipplan"
)

// recSession is a session that keeps the bytes of every write, one entry a Send.
type recSession struct {
	*fakeSession
	sent [][]byte
}

func (r *recSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	r.sent = append(r.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}

const (
	testKnife   = 946
	testLogs    = 1511
	testOakLogs = 1521
	testNote    = 1512
	testSpotIdx = 77
	testOp      = 1
)

var testNames = map[int32]string{testKnife: "Knife", testLogs: "Logs", testOakLogs: "Oak logs", testNote: "Logs"}

func testName(id int32) (string, bool, bool) {
	n, ok := testNames[id]
	return n, id == testNote, ok
}

type manipFixture struct {
	t    *testing.T
	sess *recSession
	d    *Deps
	st   *ManipState
	logs []string
	spot gamestate.NPC
	// usable is what the spot checks answer.
	usable bool
}

func newManipFixture(t *testing.T, pack ...[2]int32) *manipFixture {
	t.Helper()
	f := &manipFixture{t: t, sess: &recSession{fakeSession: newFakeSession()}, usable: true, spot: gamestate.NPC{Index: testSpotIdx, X: 2840, Z: 3432}}
	if len(pack) == 0 {
		pack = [][2]int32{{testKnife, 1}, {testLogs, 1}}
	}
	setPackForEatTest(t, f.sess.fakeSession, pack...)
	f.st = NewManipState()
	f.st.nameFn = testName
	f.st.spotOK = func() bool { return f.usable }
	now := time.Unix(1_000_000, 0)
	f.st.now = func() time.Time { return now }
	f.d = &Deps{
		Session: f.sess, Manip: &ManipConfig{Enabled: true, State: f.st},
		Log: func(s string) { f.logs = append(f.logs, s) },
	}
	// Every delay is a tick: the engine's, as the client sees it.
	f.d.Delay = func(time.Duration) bool { f.tick(); return true }
	f.placeSelf(60, 60)
	return f
}

// placeSelf puts the local player on a known tile (a REBUILD_NORMAL and a teleport PLAYER_INFO), which the quiet click needs.
func (f *manipFixture) placeSelf(localX, localZ uint32) {
	st := f.sess.State()
	rb := packet.NewWriter(8)
	rb.P2(400)
	rb.P2(400)
	st.Handle(serverprot.REBUILD_NORMAL, rb.Data[:rb.Pos])
	var bits []bool
	put := func(n int, v uint32) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, v>>uint(i)&1 == 1)
		}
	}
	put(1, 1)
	put(2, 3) // teleport
	put(2, 0) // level
	put(7, localX)
	put(7, localZ)
	put(1, 0) // jump
	put(1, 0) // extended info
	put(8, 0) // no other players
	put(11, 2047)
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	out := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			out[i/8] |= 0x80 >> uint(i%8)
		}
	}
	st.Handle(serverprot.PLAYER_INFO, out)
}

// tick is one PLAYER_INFO with no animation; anim is one that carries a self animation.
func (f *manipFixture) tick()        { f.sess.State().Handle(serverprot.PLAYER_INFO, []byte{0, 0x7f, 0xf0}) }
func (f *manipFixture) anim(seq int) { fishingAnimation(f.sess.State(), seq) }

func (f *manipFixture) hasLog(sub string) bool {
	for _, l := range f.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// expected builds the bytes the actions send alone, for comparing with what the run wrote in one.
func expected(build func(s actions.Session)) []byte {
	e := &recSession{fakeSession: newFakeSession()}
	build(e)
	var out []byte
	for _, b := range e.sent {
		out = append(out, b...)
	}
	return out
}

// ---- the kit ----

func TestPickManipItemsNeedsAKnifeAndAUsableLog(t *testing.T) {
	slot := func(id int32, n int) gamestate.SlotItem {
		return gamestate.SlotItem{Slot: n, Item: gamestate.Item{ID: id, Count: 1}}
	}
	for _, c := range []struct {
		name      string
		items     []gamestate.SlotItem
		fletching int
		wantLogs  string
		wantComp  int
		ok        bool
	}{
		{"knife and plain logs", []gamestate.SlotItem{slot(testKnife, 3), slot(testLogs, 5)}, 1, "Logs", chopmanipplan.Make1Shafts, true},
		{"oak logs the level allows", []gamestate.SlotItem{slot(testOakLogs, 1), slot(testKnife, 2)}, 40, "Oak logs", chopmanipplan.Make1Bow, true},
		{"oak logs the level does not allow", []gamestate.SlotItem{slot(testOakLogs, 1), slot(testKnife, 2)}, 5, "", 0, false},
		{"plain logs win over oak", []gamestate.SlotItem{slot(testOakLogs, 1), slot(testKnife, 2), slot(testLogs, 4)}, 40, "Logs", chopmanipplan.Make1Shafts, true},
		{"no knife", []gamestate.SlotItem{slot(testLogs, 1)}, 1, "", 0, false},
		{"no log", []gamestate.SlotItem{slot(testKnife, 1)}, 1, "", 0, false},
		{"a noted log is not one", []gamestate.SlotItem{slot(testKnife, 1), slot(testNote, 2)}, 1, "", 0, false},
	} {
		got, ok := pickManipItems(c.items, testName, c.fletching)
		if ok != c.ok || (ok && (got.carrier.Logs != c.wantLogs || got.carrier.MakeComp != c.wantComp)) {
			t.Errorf("%s: (%+v, %v), want logs %q comp %d ok %v", c.name, got, ok, c.wantLogs, c.wantComp, c.ok)
		}
	}
	got, _ := pickManipItems([]gamestate.SlotItem{slot(testKnife, 3), slot(testLogs, 5)}, testName, 1)
	if got.knifeSlot != 3 || got.logSlot != 5 || got.knifeID != testKnife || got.logID != testLogs {
		t.Errorf("slots %+v", got)
	}
}

// ---- the writes ----

// The opening batch is ONE write of four packets, in the order the engine needs: knife on the log, Make 1, CLOSE_MODAL, the
// spot click.
func TestOpeningBatchIsOneWriteOfFourPackets(t *testing.T) {
	f := newManipFixture(t)
	f.st.plainTicks = manipPlainTicks // the baseline is in: the hold opens with the pattern
	f.sess.State().Handle(serverprot.PLAYER_INFO, []byte{0, 0x7f, 0xf0})
	if err := manipClick(f.d, testOp, f.spot); err != nil {
		t.Fatal(err)
	}
	want := expected(func(s actions.Session) {
		_ = actions.OpHeldU(s, testLogs, 1, PlayerPack, testKnife, 0, PlayerPack)
		_ = actions.IfButton(s, chopmanipplan.Make1Shafts)
		_ = actions.CloseModal(s)
		_ = actions.OpNpc(s, testOp, testSpotIdx)
	})
	if len(f.sess.sent) != 1 || !bytes.Equal(f.sess.sent[0], want) {
		t.Fatalf("%d writes; first % x, want one of % x", len(f.sess.sent), f.sess.sent, want)
	}
	if r := activeRun(f.d); r == nil || r.mode != modeManip || !f.st.ctl.InFlight() {
		t.Errorf("run %+v, controller in flight %v: want the pattern running with a batch in flight", r, f.st.ctl.InFlight())
	}
}

// Nothing is written while the spot is gone or a whirlpool, and nothing but the click is written when the kit is gone.
func TestWritesAreCheckedFirst(t *testing.T) {
	f := newManipFixture(t)
	r := startManip(f.d, testOp, f.spot)
	r.mode = modeManip
	f.usable = false
	if r.write(fishmanipplan.Lone, 10) || r.write(fishmanipplan.Batch, 10) || len(f.sess.sent) != 0 {
		t.Fatalf("wrote with the spot unusable: %d writes", len(f.sess.sent))
	}
	f.usable = true
	if !r.write(fishmanipplan.Lone, 10) || len(f.sess.sent) != 1 || !bytes.Equal(f.sess.sent[0], expected(func(s actions.Session) { _ = actions.OpNpc(s, testOp, testSpotIdx) })) {
		t.Fatalf("the lone click: %d writes", len(f.sess.sent))
	}
	// The log is used up: the next batch is not written, the hold goes back to the plain click once.
	setPackForEatTest(t, f.sess.fakeSession, [2]int32{testKnife, 1})
	f.sess.sent = nil
	if r.write(fishmanipplan.Batch, 13) {
		t.Fatal("a batch with no log")
	}
	if r.mode != modeOff || len(f.sess.sent) != 1 || !f.hasLog("the knife or the log is gone") {
		t.Errorf("mode %v, %d writes, log %q after the log went", r.mode, len(f.sess.sent), f.logs)
	}
}

// ---- the policy ----

// A hold starts with a plain window (a plain click, ticks and catches counted), ends the interaction with a click on the own tile,
// waits for the varp to go stale and writes the opening batch.
func TestPlainWindowThenQuietThenThePattern(t *testing.T) {
	f := newManipFixture(t)
	manipBegin(f.d, "Big net")
	if err := manipClick(f.d, testOp, f.spot); err != nil {
		t.Fatal(err)
	}
	if want := expected(func(s actions.Session) { _ = actions.OpNpc(s, testOp, testSpotIdx) }); len(f.sess.sent) != 1 || !bytes.Equal(f.sess.sent[0], want) {
		t.Fatalf("the first hold's click: % x, want the plain click", f.sess.sent)
	}
	r := activeRun(f.d)
	if r == nil || r.mode != modePlain {
		t.Fatalf("run %+v, want a plain window", r)
	}
	for i := 0; i < manipPlainTicks-1; i++ {
		if i%12 == 0 {
			manipCatch(f.d, 1)
		}
		if !manipWait(f.d, 600*time.Millisecond) {
			t.Fatal("manipWait stopped")
		}
	}
	if f.st.plainTicks != manipPlainTicks-1 || f.st.plainFish != (manipPlainTicks-2)/12+1 || r.mode != modePlain {
		t.Fatalf("after %d ticks: plain %d ticks %d fish, mode %v", manipPlainTicks-1, f.st.plainTicks, f.st.plainFish, r.mode)
	}
	if manipAlive(f.d) {
		t.Error("a plain window is alive by the manipulation's say: the ordinary fishingActive must judge it")
	}
	manipWait(f.d, 600*time.Millisecond) // the last tick of the window: the interaction is ended
	if r.mode != modeQuiet {
		t.Fatalf("mode %v after the window, want quiet", r.mode)
	}
	if x, z, level, _ := f.sess.State().Tile(); len(f.sess.sent) != 2 || !bytes.Equal(f.sess.sent[1], expected(func(s actions.Session) {
		_ = actions.Walk(s, []pathfind.NavPoint{{X: x, Z: z, Level: level}}, false)
	})) {
		t.Fatalf("the quiet click: % x", f.sess.sent[1:])
	}
	if !manipAlive(f.d) {
		t.Error("a quiet spell is not alive: the hold would end on it")
	}
	for i := 0; i < manipQuietTicks-1; i++ {
		manipWait(f.d, 600*time.Millisecond)
	}
	if r.mode != modeQuiet || len(f.sess.sent) != 2 {
		t.Fatalf("mode %v, %d writes before the quiet spell is over", r.mode, len(f.sess.sent))
	}
	manipWait(f.d, 600*time.Millisecond)
	if r.mode != modeManip || len(f.sess.sent) != 3 || len(f.sess.sent[2]) < 20 {
		t.Fatalf("mode %v, %d writes: want the opening batch", r.mode, len(f.sess.sent))
	}
	if f.st.plainTicks != manipPlainTicks {
		t.Errorf("plain ticks %d, want %d: the quiet spell is neither plain nor manipulated", f.st.plainTicks, manipPlainTicks)
	}
}

// Without a knife or a log the hold is the plain click and the log says so once.
func TestNoKitIsThePlainClickAndSaysSoOnce(t *testing.T) {
	f := newManipFixture(t, [2]int32{testLogs, 1})
	for i := 0; i < 3; i++ {
		if err := manipClick(f.d, testOp, f.spot); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.sess.sent) != 3 || activeRun(f.d) != nil {
		t.Fatalf("%d writes, run %+v: want three plain clicks and no run", len(f.sess.sent), activeRun(f.d))
	}
	n := 0
	for _, l := range f.logs {
		if strings.Contains(l, "no knife, or no log") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the missing kit was logged %d times, want once", n)
	}
}

// The yield guard: the pattern running 300 ticks at under 85% of a plain baseline of six catches or more turns it off, with one
// plain click, for twenty minutes.
func TestYieldGuardTurnsItOffForTwentyMinutes(t *testing.T) {
	f := newManipFixture(t)
	f.st.plainTicks, f.st.plainFish = 200, 20 // 10 per 100 ticks
	f.st.manipTicks, f.st.manipFish = 299, 20 // 6.7 per 100: under 8.5
	r := startManip(f.d, testOp, f.spot)
	r.mode = modeManip
	r.guard()
	if r.mode != modeManip {
		t.Fatal("judged before 300 manipulated ticks")
	}
	f.st.manipTicks = 300
	r.guard()
	if r.mode != modeOff || len(f.sess.sent) != 1 || !f.hasLog("under 85% of the plain rate") {
		t.Fatalf("mode %v, %d writes, log %q: want it off with one plain click", r.mode, len(f.sess.sent), f.logs)
	}
	// Off for twenty minutes: the next hold is the plain click and counts nothing.
	f.sess.sent = nil
	if err := manipClick(f.d, testOp, f.spot); err != nil || len(f.sess.sent) != 1 || activeRun(f.d) != nil {
		t.Fatalf("a hold while disabled: %d writes, run %+v", len(f.sess.sent), activeRun(f.d))
	}
	// A rate at 85% or over is left alone.
	g := newManipFixture(t)
	g.st.plainTicks, g.st.plainFish = 200, 20
	g.st.manipTicks, g.st.manipFish = 400, 34 // 8.5 per 100
	gr := startManip(g.d, testOp, g.spot)
	gr.mode = modeManip
	gr.guard()
	if gr.mode != modeManip {
		t.Error("the guard fired at exactly 85% of the plain rate")
	}
	// ... and so is a baseline too thin to judge by.
	h := newManipFixture(t)
	h.st.plainTicks, h.st.plainFish = 200, 5
	h.st.manipTicks, h.st.manipFish = 900, 1
	hr := startManip(h.d, testOp, h.spot)
	hr.mode = modeManip
	hr.guard()
	if hr.mode != modeManip {
		t.Error("the guard judged against five plain catches")
	}
}

// A hold that finds the account fishing, with no fresh schedule, ends the interaction and restarts the pattern the way it
// restarts after a plain window; with the baseline missing it starts a plain window instead.
func TestAHoldThatFindsTheAccountFishingRestartsThePattern(t *testing.T) {
	f := newManipFixture(t)
	f.st.plainTicks = manipPlainTicks
	manipResume(f.d, testOp, f.spot)
	r := activeRun(f.d)
	if r == nil || r.mode != modeQuiet || len(f.sess.sent) != 1 {
		t.Fatalf("run %+v, %d writes: want the interaction ended and a quiet spell", r, len(f.sess.sent))
	}
	g := newManipFixture(t)
	manipResume(g.d, testOp, g.spot)
	if gr := activeRun(g.d); gr == nil || gr.mode != modePlain || len(g.sess.sent) != 0 {
		t.Fatalf("run %+v, %d writes: want a plain window and no write", gr, len(g.sess.sent))
	}
	// A fresh schedule is gone on with.
	h := newManipFixture(t)
	h.st.plainTicks = manipPlainTicks
	n := h.sess.State().Tick()
	h.st.ctl.Begin(n)
	h.st.ctl.Sent(fishmanipplan.Batch, n)
	h.tick()
	manipResume(h.d, testOp, h.spot)
	if hr := activeRun(h.d); hr == nil || hr.mode != modeManip || len(h.sess.sent) != 0 {
		t.Fatalf("run %+v, %d writes: want the pattern resumed with no write", hr, len(h.sess.sent))
	}
}

// ---- the pattern through the tick feed ----

// A batch confirmed by its fletch animation, then the lone click two ticks after its read tick and the next batch the tick after.
func TestPatternWritesTheLoneClickAndTheNextBatch(t *testing.T) {
	f := newManipFixture(t)
	f.st.plainTicks = manipPlainTicks
	f.tick()
	if err := manipClick(f.d, testOp, f.spot); err != nil {
		t.Fatal(err)
	}
	r := activeRun(f.d)
	lone := expected(func(s actions.Session) { _ = actions.OpNpc(s, testOp, testSpotIdx) })
	writes := func() int { return len(f.sess.sent) }
	f.anim(fishmanipplan.FletchAnim) // the batch was read in this tick, the link is not late
	r.pace()
	if writes() != 1 {
		t.Fatalf("%d writes at the batch's read tick, want none more", writes())
	}
	f.tick()
	r.pace()
	if writes() != 2 || !bytes.Equal(f.sess.sent[1], lone) {
		t.Fatalf("%d writes after the tick before the roll; want the lone click", writes())
	}
	f.tick()
	r.pace()
	if writes() != 3 || len(f.sess.sent[2]) < 20 {
		t.Fatalf("%d writes after the roll tick; want the next batch", writes())
	}
	f.anim(fishmanipplan.FletchAnim)
	r.pace()
	if !manipAlive(f.d) {
		t.Error("not alive with a batch just confirmed")
	}
}

// A controller that stops (batches never read) leaves the hold with one plain click.
func TestControllerStopFallsBackToOnePlainClick(t *testing.T) {
	f := newManipFixture(t)
	f.st.plainTicks = manipPlainTicks
	f.st.ctl = fishmanipplan.New(fishmanipplan.Settings{Enabled: true, MaxLost: 1, LostAfterMS: 1})
	var ms int64
	f.st.ctl.SetClock(func() int64 { ms += 700; return ms }) // a tick's worth of real time passes between looks
	f.tick()
	if err := manipClick(f.d, testOp, f.spot); err != nil {
		t.Fatal(err)
	}
	r := activeRun(f.d)
	for i := 0; i < 30 && r.mode == modeManip; i++ {
		f.tick()
		r.pace()
	}
	if r.mode != modeOff || !f.hasLog("batches in a row were never read") {
		t.Fatalf("mode %v, log %q: want the hold stopped by the controller", r.mode, f.logs)
	}
	if got := f.sess.sent[len(f.sess.sent)-1]; !bytes.Equal(got, expected(func(s actions.Session) { _ = actions.OpNpc(s, testOp, testSpotIdx) })) {
		t.Errorf("the last write % x is not the plain click", got)
	}
	if activeRun(f.d) != nil {
		t.Error("a stopped hold still has an active run")
	}
}

// ---- the report ----

func TestLineSaysTheMeasuredMultiplier(t *testing.T) {
	s := NewManipState()
	if l := s.Line(); !strings.Contains(l, "no plain window yet") {
		t.Errorf("empty line %q", l)
	}
	s.plainTicks, s.plainFish = 120, 10
	s.manipTicks, s.manipFish = 3100, 520
	l := s.Line()
	for _, want := range []string{"520 catches in 3100 ticks with batches (16.8 per 100)", "plain 10 in 120 ticks (8.3 per 100) = x2.0 vs plain"} {
		if !strings.Contains(l, want) {
			t.Errorf("line %q lacks %q", l, want)
		}
	}
	s.plainFish = 0
	if l := s.Line(); !strings.Contains(l, "= no ratio yet") {
		t.Errorf("a plain window with no catch gave %q", l)
	}
}

// One line at the end of a call, and one more only every 3,000 manipulated ticks inside it.
func TestReportsAtTheEndOfACallAndEveryThreeThousandTicks(t *testing.T) {
	f := newManipFixture(t)
	f.st.manipTicks, f.st.manipFish = 2999, 100
	f.st.report(f.d, false)
	if len(f.logs) != 0 {
		t.Fatalf("a line before %d ticks: %q", manipReportEvery, f.logs)
	}
	f.st.manipTicks = 3000
	f.st.report(f.d, false)
	f.st.report(f.d, false)
	if len(f.logs) != 1 || !strings.HasPrefix(f.logs[0], "fish manip: ") {
		t.Fatalf("lines %q", f.logs)
	}
	f.st.manipTicks = 3100
	manipEnd(f.d)
	if len(f.logs) != 1 {
		t.Errorf("manipEnd logged %d lines in all after only 100 new ticks, want 1", len(f.logs))
	}
	f.st.manipTicks = 3000 + manipEndReport
	manipEnd(f.d)
	if len(f.logs) != 2 {
		t.Errorf("manipEnd logged %d lines in all, want 2", len(f.logs))
	}
}

// Disabled, or no state, is the plain click and the plain delay.
func TestOffIsTheOldBehaviour(t *testing.T) {
	f := newManipFixture(t)
	f.d.Manip = nil
	if err := manipClick(f.d, testOp, f.spot); err != nil || len(f.sess.sent) != 1 {
		t.Fatalf("manipClick with no config: %v, %d writes", err, len(f.sess.sent))
	}
	n := 0
	f.d.Delay = func(time.Duration) bool { n++; return true }
	if !manipWait(f.d, 600*time.Millisecond) || n != 1 {
		t.Errorf("manipWait with no config: %d delays", n)
	}
	manipBegin(f.d, "x")
	manipSettle(f.d)
	manipEnd(f.d)
	manipCatch(f.d, 3)
	if manipAlive(f.d) || HasManipKit(f.d) {
		t.Error("a manipulation answered with none configured")
	}
	f.d.Manip = &ManipConfig{Enabled: false, State: f.st}
	f.sess.sent = nil
	if err := manipClick(f.d, testOp, f.spot); err != nil || len(f.sess.sent) != 1 || activeRun(f.d) != nil {
		t.Errorf("a disabled config: %d writes", len(f.sess.sent))
	}
}

// A bank deposit leaves the knife in the pack while the manipulation is on, and only then. The plain logs are banked and the kit's few
// are drawn back: keeping them left a pack the Fletching training had filled with logs full for ever and the bank phase repeating
// every second (live, 7 Oct 2026, 05:24).
func TestAFullPackOfLogsIsBankedNotKept(t *testing.T) {
	f := newManipFixture(t)
	for _, name := range []string{"Logs", "Oak logs", "Willow logs", "Raw bass"} {
		if depositLeaves(f.d, map[string]bool{"big fishing net": true}, name, false) {
			t.Errorf("a deposit leaves %q in the pack with the manipulation on", name)
		}
	}
	for _, name := range []string{"Big fishing net", "Knife"} {
		if !depositLeaves(f.d, map[string]bool{"big fishing net": true}, name, false) {
			t.Errorf("a deposit does not leave %q in the pack", name)
		}
	}
}

// A bank deposit leaves the knife in the pack while the manipulation is on, and only then.
func TestManipKitStaysThroughBankDeposits(t *testing.T) {
	var nilCfg *ManipConfig
	on := &ManipConfig{Enabled: true}
	off := &ManipConfig{}
	for _, c := range []struct {
		name string
		cfg  *ManipConfig
		want bool
	}{
		{"Knife", on, true}, {"knife ", on, true}, {"Logs", on, false}, {"Oak logs", on, false}, {"Raw bass", on, false},
		{"Knife", off, false}, {"Knife", nilCfg, false},
	} {
		if got := c.cfg.KeepsName(c.name); got != c.want {
			t.Errorf("KeepsName(%q) with %+v = %v, want %v", c.name, c.cfg, got, c.want)
		}
	}
}

// A FishUntil call that begins at the spot the last one was patterned on, a tick or two after it ended, takes the schedule over and
// writes nothing; another spot, a stale schedule or a plain window due start afresh.
func TestANewCallAtTheSameSpotContinuesThePattern(t *testing.T) {
	f := newManipFixture(t)
	f.tick()
	manipBegin(f.d, "Big net")
	f.st.plainTicks = manipPlainTicks
	if err := manipClick(f.d, testOp, f.spot); err != nil {
		t.Fatal(err)
	}
	if len(f.sess.sent) != 1 {
		t.Fatalf("%d writes for the opening batch", len(f.sess.sent))
	}
	manipEnd(f.d) // the call ends with the batch still in flight
	f.tick()
	if !manipResumable(f.d, f.spot) {
		t.Fatal("the next call at the same spot, a tick after, is not resumable")
	}
	if manipResumable(f.d, gamestate.NPC{Index: testSpotIdx + 1}) {
		t.Error("resumable at another spot")
	}
	manipResume(f.d, testOp, f.spot)
	r := activeRun(f.d)
	if r == nil || r.mode != modeManip || len(f.sess.sent) != 1 {
		t.Fatalf("run %+v, %d writes: want the pattern continued with no write", r, len(f.sess.sent))
	}
	// The batch the earlier call left in flight is read in the new feed's first tick: confirmed, not lost.
	f.anim(fishmanipplan.FletchAnim)
	r.pace()
	if st := f.st.ctl.Stats(); st.Confirmed != 1 || st.Lost != 0 {
		t.Errorf("stats %+v after the carried batch was read", st)
	}
	// A schedule gone stale is not resumed.
	manipEnd(f.d)
	for i := 0; i < 8; i++ {
		f.tick()
	}
	if manipResumable(f.d, f.spot) {
		t.Error("a schedule eight ticks old was resumed")
	}
	// Nor is one while a plain window is due.
	g := newManipFixture(t)
	if manipResumable(g.d, g.spot) {
		t.Error("resumable with no baseline taken")
	}
}
