package essencerunner

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/minemanipplan"
)

// objects of the lab and the live server (content/pack/obj.pack).
const (
	mmPick  = 1265 // bronze pickaxe
	mmKnife = 946
	mmLogs  = 1511
	mmOak   = 1521
)

var mmNames = map[int32]string{mmPick: "Bronze pickaxe", mmKnife: "Knife", mmLogs: "Logs", mmOak: "Oak logs", 1437: "Rune essence"}

// mmName treats ids from 100000 as the bank note of id-100000.
func mmName(*Deps) itemName {
	return func(id int32) (string, bool, bool) {
		if id >= 100000 {
			n, ok := mmNames[id-100000]
			return n, true, ok
		}
		n, ok := mmNames[id]
		return n, false, ok
	}
}

// injectManip stands in for the cache config and the equipment panel for the length of the test.
func injectManip(t *testing.T, pick minemanipplan.Pickaxe, haveIt bool) {
	t.Helper()
	oldNames, oldPick := manipNames, manipPickaxe
	manipNames = mmName
	manipPickaxe = func(*Deps) (minemanipplan.Pickaxe, bool) { return pick, haveIt }
	t.Cleanup(func() { manipNames, manipPickaxe = oldNames, oldPick })
}

func setPack(s *gamestate.State, items ...int32) {
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

func setStat(s *gamestate.State, stat, xp, level int) {
	p := packet.NewWriter(8)
	p.P1(byte(stat))
	p.P4(int32(xp))
	p.P1(byte(level))
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

// mmAnimState is the client's state with the animation the test says the server started this tick (the real state reads it out of
// a PLAYER_INFO's extended data, which the model does not encode).
type mmAnimState struct {
	*gamestate.State
	anim, at int
}

func (a *mmAnimState) SelfAnim() (int, int) { return a.anim, a.at }

// mmRig is a client and the engine model of minemanipplan.Engine: every packet the client writes is decoded back into the engine's
// events and read lag ticks after the next tick; every tick the engine runs ends in a PLAYER_INFO, the animation it started and
// the Mining experience of an essence, as the client reads them.
type mmRig struct {
	t      *testing.T
	st     *mmAnimState
	eng    *minemanipplan.Engine
	lag    int
	arrive map[int][]minemanipplan.Event
	tick   int
	xp     int
	seen   int
	sends  int
	cheats anticheat.Counters
}

func newMMRig(t *testing.T, pick minemanipplan.Pickaxe, lag int) *mmRig {
	t.Helper()
	g := &mmRig{t: t, st: &mmAnimState{State: gamestate.New()}, eng: minemanipplan.NewEngine(pick, true), lag: lag, arrive: map[int][]minemanipplan.Event{}}
	g.eng.Unbounded = true
	setPack(g.st.State, mmPick, mmKnife, mmLogs)
	setStat(g.st.State, MiningStat, 0, 1)
	setStat(g.st.State, fletchingStat, 0, 1)
	return g
}

func (g *mmRig) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := w.Data[:w.Pos]
	g.sends++
	at := g.tick + 1 + g.lag
	for len(b) > 0 {
		var ev minemanipplan.Event
		n := 0
		switch op := int(b[0]); {
		case op == clientprot.OPHELDU:
			ev, n = minemanipplan.KnifeOnLog, 13
		case op == clientprot.IF_BUTTON:
			ev, n = minemanipplan.Make1, 3
		case op == clientprot.CLOSE_MODAL:
			ev, n = minemanipplan.CloseModal, 1
		case op == clientprot.OPLOC1:
			ev, n = minemanipplan.ClickRock, 7
		case op == clientprot.RESUME_PAUSEBUTTON:
			ev, n = minemanipplan.Continue, 3
		default:
			g.t.Fatalf("mmRig: a packet the model does not read: opcode %d", op)
		}
		g.arrive[at] = append(g.arrive[at], ev)
		b = b[n:]
	}
	return nil
}
func (g *mmRig) State() *gamestate.State     { return g.st.State }
func (g *mmRig) Cheats() *anticheat.Counters { return &g.cheats }

// send queues one event for the engine, read lag ticks after the next tick (a packet the test writes by hand).
func (g *mmRig) send(ev minemanipplan.Event) {
	at := g.tick + 1 + g.lag
	g.arrive[at] = append(g.arrive[at], ev)
}

// step runs one engine tick and shows the client its output.
func (g *mmRig) step() minemanipplan.Seen {
	g.tick++
	evs := g.arrive[g.tick]
	delete(g.arrive, g.tick)
	s := g.eng.Step(evs)
	g.st.anim, g.st.at = s.Anim, g.tick
	g.st.State.Handle(serverprot.PLAYER_INFO, []byte{0})
	if s.Essence > g.seen {
		g.seen = s.Essence
		g.xp += minemanipplan.EssenceXP
		setStat(g.st.State, MiningStat, g.xp, 1)
	}
	return s
}

func (g *mmRig) deps() *Deps {
	return &Deps{Session: g, Delay: RealDelay, Manip: &ManipConfig{Enabled: true}}
}

var mmRock = locindex.Match{Loc: locindex.Loc{ID: 2491, X: 2925, Z: 4849}, Op: 1}

// start builds the call's manipulation over the rig (the live tick feed replaced by one that reads the rig's animation).
func (g *mmRig) start(d *Deps) *manipRun {
	g.t.Helper()
	r := startManip(d, mmRock)
	if r == nil {
		g.t.Fatal("startManip returned nil")
	}
	r.watch.stop()
	r.watch = newTickWatch(g.st)
	return r
}

func gapsOf(rolls []int) []int {
	var g []int
	for i := 1; i < len(rolls); i++ {
		g = append(g, rolls[i]-rolls[i-1])
	}
	return g
}

// ---- the writer, over the engine model ----

func TestTheRealWriterAndControllerRollBronzeEveryThreeTicks(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	d := g.deps()
	r := g.start(d)
	defer r.watch.stop()
	if !r.open() {
		t.Fatal("the opening batch was not written")
	}
	for i := 0; i < 600; i++ {
		g.step()
		if r.pace() {
			t.Fatal("pace reported a stop")
		}
	}
	rolls := g.eng.Rolls
	if len(rolls) < 190 {
		t.Fatalf("%d essence in 600 ticks, want about 199 (a bronze pickaxe alone makes 75)", len(rolls))
	}
	if rolls[0] != 3 {
		t.Errorf("first essence at tick %d, want 3: the opening batch is read in tick 1 and re-arms the stale varp to +2", rolls[0])
	}
	for i, gp := range gapsOf(rolls) {
		if gp != 3 {
			t.Fatalf("gap %d is %d ticks, want 3 (gaps %v)", i, gp, gapsOf(rolls)[:min(12, len(gapsOf(rolls)))])
		}
	}
	for tick, n := range g.eng.Events {
		if n > minemanipplan.BatchEvents {
			t.Fatalf("%d user events read in tick %d: a batch is %d and the engine reads %d", n, tick, minemanipplan.BatchEvents, minemanipplan.EngineEventsPerTick)
		}
	}
	if g.eng.Leaks != 0 || !g.eng.Kit {
		t.Errorf("%d carrier log(s) fletched", g.eng.Leaks)
	}
	st := r.ctl.Stats()
	if st.Lost != 0 || st.Late1 != 0 || st.Early != 0 || st.LateMore != 0 || r.wrote < 195 {
		t.Errorf("stats %s; wrote %d batches", st.Line(), r.wrote)
	}
	if g.sends != r.wrote {
		t.Errorf("%d socket writes for %d batches: a batch must be ONE write", g.sends, r.wrote)
	}
}

func TestEveryPickaxeThatGainsRollsEveryThreeTicksThroughTheRealWriter(t *testing.T) {
	for _, p := range minemanipplan.Pickaxes {
		if !p.Worth() {
			continue
		}
		injectManip(t, p, true)
		g := newMMRig(t, p, 0)
		r := g.start(g.deps())
		r.open()
		for i := 0; i < 300; i++ {
			g.step()
			r.pace()
		}
		r.watch.stop()
		gp := gapsOf(g.eng.Rolls)
		if len(gp) < 90 {
			t.Errorf("%s: %d essence in 300 ticks", p.Item, len(g.eng.Rolls))
			continue
		}
		for i, x := range gp {
			if x != 3 {
				t.Errorf("%s: gap %d is %d (gaps %v)", p.Item, i, x, gp[:min(12, len(gp))])
				break
			}
		}
	}
}

func TestALinkOneOrTwoTicksLateIsAimedEarlyThroughTheRealWriter(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	for _, lag := range []int{1, 2} {
		injectManip(t, bronze, true)
		g := newMMRig(t, bronze, lag)
		r := g.start(g.deps())
		r.open()
		for i := 0; i < 900; i++ {
			g.step()
			r.pace()
		}
		r.watch.stop()
		if got := r.ctl.Lead(); got != lag {
			t.Errorf("lag %d: aim %d: %s", lag, got, r.ctl.Stats().Line())
		}
		gp := gapsOf(g.eng.Rolls)
		for i, x := range gp[len(gp)-80:] {
			if x != 3 {
				t.Errorf("lag %d: steady-state gap %d is %d", lag, i, x)
				break
			}
		}
		if g.eng.Leaks != 0 {
			t.Errorf("lag %d: %d carrier log(s) fletched", lag, g.eng.Leaks)
		}
	}
}

// A level-up chat blocks the interaction for a tick or more; the batch read before it has moved the varp, the Continue goes out
// when the chat is seen, and the run goes on at the 3-tick cadence. This is the hold's own job (Step), played here by hand.
func TestALevelUpChatDoesNotBreakTheCadenceThroughTheRealWriter(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	g.eng.LevelEvery = 5
	r := g.start(g.deps())
	r.open()
	answered := 0
	for i := 0; i < 600; i++ {
		s := g.step()
		if s.Modal == minemanipplan.ModalChat {
			g.send(minemanipplan.Continue) // what HoldChat.Step does
			answered++
		}
		r.pace()
	}
	r.watch.stop()
	if g.eng.Chats < 30 || answered < g.eng.Chats-1 {
		t.Fatalf("%d chats opened, %d answered", g.eng.Chats, answered)
	}
	if n := len(g.eng.Rolls); n < 190 {
		t.Fatalf("%d essence in 600 ticks across %d level-ups, want about 199", n, g.eng.Chats)
	}
	if g.eng.Leaks != 0 {
		t.Errorf("%d carrier log(s) fletched across level-ups", g.eng.Leaks)
	}
}

// ---- what starts it, and what stops it ----

func TestStartManipIsNilWhenTheSettingIsOffOrThePickaxeGainsNothingOrNoKitIsHeld(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	if startManip(&Deps{Session: g}, mmRock) != nil {
		t.Fatal("no Manip config must mean no manipulation")
	}
	if startManip(&Deps{Session: g, Manip: &ManipConfig{}}, mmRock) != nil {
		t.Fatal("Enabled false must mean no manipulation")
	}
	var lines []string
	d := g.deps()
	d.Log = func(s string) { lines = append(lines, s) }
	r := startManip(d, mmRock)
	if r == nil {
		t.Fatal("Enabled with a kit held did not start one")
	}
	r.watch.stop()

	// No pickaxe the level allows.
	injectManip(t, bronze, false)
	if startManip(d, mmRock) != nil {
		t.Error("started with no pickaxe held")
	}

	// A rune pickaxe's own period is the floor.
	injectManip(t, minemanipplan.Pickaxes[0], true)
	d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
	if startManip(d, mmRock) != nil || startManip(d, mmRock) != nil {
		t.Error("started for a rune pickaxe")
	}
	floor := 0
	for _, l := range lines {
		if strings.Contains(l, "the batch's own floor") {
			floor++
		}
	}
	if floor != 1 {
		t.Errorf("the floor is said %d times, want once: %q", floor, lines)
	}

	// No kit: no knife, no log, a log the Fletching level cannot cut.
	injectManip(t, bronze, true)
	for name, items := range map[string][]int32{
		"no knife":                 {mmPick, mmLogs},
		"no log":                   {mmPick, mmKnife},
		"oak and Fletching 1":      {mmPick, mmKnife, mmOak},
		"a noted knife":            {mmPick, 100000 + mmKnife, mmLogs},
		"noted logs only":          {mmPick, mmKnife, 100000 + mmLogs},
		"nothing but the pickaxe":  {mmPick},
		"an empty pack":            {},
		"two pickaxes, no kit yet": {mmPick, mmPick},
	} {
		lines = nil
		setPack(g.st.State, items...)
		d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
		if startManip(d, mmRock) != nil {
			t.Errorf("%s: started", name)
		}
		if len(lines) != 1 || !strings.Contains(lines[0], "no knife, or no log") {
			t.Errorf("%s: log %q, want the reason said once", name, lines)
		}
	}
	// A kit that is there from Fletching 20 on: oak logs make the shortbow.
	setStat(g.st.State, fletchingStat, 0, 20)
	setPack(g.st.State, mmPick, mmKnife, mmOak)
	d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
	if r := startManip(d, mmRock); r == nil {
		t.Error("oak logs at Fletching 20 are a kit")
	} else {
		r.watch.stop()
	}
}

func TestOneBatchIsOneSendHoldingKnifeOnLogMake1CloseModalAndTheRockClick(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	r := g.start(g.deps())
	defer r.watch.stop()
	if !r.write(10) {
		t.Fatal("the batch was not written")
	}
	if g.sends != 1 {
		t.Fatalf("a batch made %d sends, want one so the engine reads the four packets in the same tick", g.sends)
	}
	got, want := g.arrive[1], minemanipplan.Batch
	if len(got) != len(want) {
		t.Fatalf("events %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d is %v, want %v", i, got[i], want[i])
		}
	}
	if r.ctl.Stats().Batches != 1 || r.wrote != 1 {
		t.Fatalf("the controller was not told: %s, wrote %d", r.ctl.Stats().Line(), r.wrote)
	}
}

// bytesSession records what the actions write, for the byte comparison.
type bytesSession struct {
	state *gamestate.State
	sent  [][]byte
}

func (b *bytesSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b.sent = append(b.sent, append([]byte(nil), w.Data[:w.Pos]...))
	return nil
}
func (b *bytesSession) State() *gamestate.State     { return b.state }
func (b *bytesSession) Cheats() *anticheat.Counters { return &anticheat.Counters{} }

func TestTheBatchBytesAreTheActionsOwn(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	st := gamestate.New()
	setPack(st, mmPick, mmKnife, mmLogs)
	one := &bytesSession{state: st}
	d := &Deps{Session: one, Delay: RealDelay, Manip: &ManipConfig{Enabled: true}}
	r := startManip(d, mmRock)
	defer r.watch.stop()
	if !r.write(10) {
		t.Fatal("not written")
	}
	ref := &bytesSession{state: st}
	_ = actions.OpHeldU(ref, mmLogs, 2, PlayerPack, mmKnife, 1, PlayerPack)
	_ = actions.IfButton(ref, chopmanipplan.Make1Shafts)
	_ = actions.CloseModal(ref)
	_ = actions.OpLoc(ref, 1, 2925, 4849, 2491)
	if len(one.sent) != 1 {
		t.Fatalf("%d sends", len(one.sent))
	}
	if want := bytes.Join(ref.sent, nil); !bytes.Equal(one.sent[0], want) {
		t.Fatalf("batch bytes % x\nwant         % x", one.sent[0], want)
	}
}

func TestNoBatchWhileTheGateOrAnotherCallerHoldsTheClick(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	r := g.start(g.deps())
	defer r.watch.stop()

	// The rock was clicked this tick: a batch is a click too and waits actions.ClickGapTicks.
	actions.MarkClick(g, r.key)
	if r.write(5) {
		t.Fatal("a batch went out inside the click gate's gap")
	}
	if g.sends != 0 || r.ctl.Stats().Batches != 0 {
		t.Fatalf("%d packets, %d batches counted", g.sends, r.ctl.Stats().Batches)
	}
	if minemanipplan.ManipPeriod < actions.ClickGapTicks {
		t.Fatalf("a batch every %d ticks is under the gate's %d", minemanipplan.ManipPeriod, actions.ClickGapTicks)
	}
	for i := 0; i < actions.ClickGapTicks; i++ {
		g.step()
	}
	// The wall clock of the gate has not run out in a fast test, the ticks have.
	if !r.write(7) {
		t.Fatal("the batch stayed refused after the gap")
	}

	// A random event's solver holds the account's clicks.
	hold := actions.HoldMovement(g, nil)
	sends := g.sends
	for i := 0; i < actions.ClickGapTicks; i++ {
		g.step()
	}
	if r.write(9) || g.sends != sends {
		t.Fatal("a batch went out while a random event's solver held the account's clicks")
	}
	hold.Release()
}

func TestNoBatchIsWrittenForAFullPack(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	r := g.start(g.deps())
	defer r.watch.stop()
	// 28 non-empty slots: the pickaxe, the knife, the log and 25 essence.
	full := []int32{mmPick, mmKnife, mmLogs}
	for len(full) < 28 {
		full = append(full, 1436)
	}
	setPack(g.st.State, full...)
	if !PackFull(g.deps()) {
		t.Fatal("the pack is not full")
	}
	if r.write(10) || g.sends != 0 {
		t.Fatal("a batch went out for a full pack: the next swing would only open the too-full box")
	}
	setPack(g.st.State, full[:27]...)
	if !r.write(11) {
		t.Fatal("the batch stayed refused with a slot free")
	}
}

func TestNoOpeningBatchBesideABatchStillInFlight(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	st := NewManipState()
	d := g.deps()
	d.Manip.State = st
	first := g.start(d)
	if !first.open() {
		t.Fatal("the first mine's opening batch was not written")
	}
	first.watch.stop()
	second := g.start(d)
	defer second.watch.stop()
	sends := g.sends
	if second.open() {
		t.Fatal("an opening batch was written beside the previous mine's, still unread")
	}
	if g.sends != sends {
		t.Fatalf("%d packets went out for the second mine's opening", g.sends-sends)
	}
}

func TestAnAnsweredChatClicksOnlyWhenNoRollComesBack(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	r := g.start(g.deps())
	defer r.watch.stop()
	r.answered()
	if r.resumeDue() {
		t.Fatal("the wait ended at once")
	}
	for i := 0; i < resumeTicks; i++ {
		g.st.State.Handle(serverprot.PLAYER_INFO, []byte{0})
	}
	if !r.resumeDue() {
		t.Fatalf("no click after %d ticks without a roll", resumeTicks)
	}
	if r.resumeDue() {
		t.Fatal("the click is due twice")
	}

	// A roll before then ends the wait: the interrupted interaction carried on by itself.
	r.answered()
	src := &fakeSource{}
	r.watch.stop()
	r.watch = newTickWatch(src)
	src.emit(g.st.State.Tick(), chopmanipplan.NoAnim, 0)
	src.xp(MiningStat, 5)
	r.pace()
	for i := 0; i < resumeTicks+2; i++ {
		g.st.State.Handle(serverprot.PLAYER_INFO, []byte{0})
	}
	if r.resumeDue() {
		t.Fatal("a roll came back and the click was still made")
	}
}

// ---- the tick feed ----

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
func (f *fakeSource) xp(skill, delta int) {
	f.sub(gamestate.Event{Kind: gamestate.EventSkillXP, Skill: skill, XPDelta: delta})
}

func TestTickWatchQueuesEveryTickAndMarksTheRollsOfThatTick(t *testing.T) {
	src := &fakeSource{}
	w := newTickWatch(src)
	d := &Deps{Delay: RealDelay}
	src.emit(10, 625, 10)
	src.xp(MiningStat, 5) // the essence of tick 10, before the loop read it: it joins the tick's observation
	src.emit(11, 625, 10) // the animation of the tick before is not this tick's
	src.emit(12, 1248, 12)
	src.xp(fletchingStat, 5) // another skill's experience is no roll
	src.xp(MiningStat, 0)    // and no experience is no roll
	want := []chopmanipplan.Obs{{N: 10, Anim: 625, Roll: true}, {N: 11, Anim: chopmanipplan.NoAnim}, {N: 12, Anim: 1248}}
	for i, w2 := range want {
		got, ok, stopped := w.next(d, time.Second)
		if !ok || stopped || got != w2 {
			t.Fatalf("observation %d: %+v ok %v stopped %v, want %+v", i, got, ok, stopped, w2)
		}
	}
	if _, ok, _ := w.next(d, 30*time.Millisecond); ok {
		t.Fatal("another skill's experience produced an observation")
	}
	// An essence whose tick was already read (the loop was quick) follows as an observation of its own.
	src.emit(13, chopmanipplan.NoAnim, 0)
	if o, ok, _ := w.next(d, time.Second); !ok || o.N != 13 || o.Roll {
		t.Fatalf("%+v ok %v", o, ok)
	}
	src.xp(MiningStat, 5)
	if o, ok, _ := w.next(d, time.Second); !ok || o.N != 13 || !o.Roll || o.Anim != chopmanipplan.NoAnim {
		t.Fatalf("the late roll mark: %+v ok %v, want tick 13 with a roll", o, ok)
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

// ---- the kit ----

func TestPickBatchItemsNeedsAKnifeAndAUsableLog(t *testing.T) {
	items := func(ids ...int32) []gamestate.SlotItem {
		var out []gamestate.SlotItem
		for i, id := range ids {
			out = append(out, gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: id, Count: 1}})
		}
		return out
	}
	name := mmName(nil)
	cases := []struct {
		name      string
		ids       []int32
		fletching int
		ok        bool
		log       int32
		comp      int
	}{
		{"plain log and knife", []int32{mmPick, mmKnife, mmLogs}, 1, true, mmLogs, chopmanipplan.Make1Shafts},
		{"no knife", []int32{mmPick, mmLogs}, 99, false, 0, 0},
		{"no log", []int32{mmPick, mmKnife}, 99, false, 0, 0},
		{"oak and the level for the shortbow", []int32{mmKnife, mmOak}, 20, true, mmOak, chopmanipplan.Make1Bow},
		{"oak and one level short", []int32{mmKnife, mmOak}, 19, false, 0, 0},
		{"plain log wins over oak", []int32{mmKnife, mmOak, mmLogs}, 99, true, mmLogs, chopmanipplan.Make1Shafts},
		{"noted logs cannot be cut", []int32{mmKnife, 100000 + mmLogs}, 99, false, 0, 0},
	}
	for _, c := range cases {
		got, ok := pickBatchItems(items(c.ids...), name, c.fletching)
		if ok != c.ok {
			t.Errorf("%s: ok %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (got.logID != c.log || got.carrier.MakeComp != c.comp || got.knifeID != mmKnife) {
			t.Errorf("%s: got %+v, want log %d button %d", c.name, got, c.log, c.comp)
		}
	}
}

func TestKitKeepIsTheKnifeAndExactlyOnePlainLogAndNothingWhenOff(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	injectManip(t, bronze, true)
	g := newMMRig(t, bronze, 0)
	d := g.deps()
	for _, c := range []struct {
		name  string
		items []int32
		want  []string
	}{
		{"the full kit", []int32{mmPick, mmKnife, mmLogs}, []string{"Knife", "Logs"}},
		{"a knife only", []int32{mmPick, mmKnife}, []string{"Knife"}},
		{"two logs: all banked, one drawn back", []int32{mmPick, mmKnife, mmLogs, mmLogs}, []string{"Knife"}},
		{"a log only", []int32{mmPick, mmLogs}, []string{"Logs"}},
		{"noted logs are not the kit", []int32{mmPick, mmKnife, 100000 + mmLogs}, []string{"Knife"}},
		{"nothing", []int32{mmPick}, nil},
	} {
		setPack(g.st.State, c.items...)
		got := KitKeep(d)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: keep %v, want %v", c.name, got, c.want)
		}
	}
	// A rune pickaxe's own period is the floor: it keeps the two slots for essence, so no kit is wanted or kept.
	injectManip(t, minemanipplan.Pickaxes[0], true)
	setPack(g.st.State, mmPick, mmKnife, mmLogs)
	if KitWanted(d) || KitKeep(d) != nil {
		t.Errorf("a rune pickaxe wants the kit: keep %v", KitKeep(d))
	}
	injectManip(t, bronze, true)
	if !KitWanted(d) {
		t.Error("a bronze pickaxe does not want the kit")
	}
	setPack(g.st.State, mmPick, mmKnife, mmLogs)
	if got := KitKeep(&Deps{Session: g}); got != nil {
		t.Errorf("with the manipulation off the keep list is %v", got)
	}
	if got := KitKeep(&Deps{Session: g, Manip: &ManipConfig{}}); got != nil {
		t.Errorf("with the manipulation disabled the keep list is %v", got)
	}
}

func drawRig(t *testing.T, pack, bank []actionsim.Stack, enabled bool) (*actionsim.Session, *Deps, *actions.Bank) {
	t.Helper()
	cfg := loadConfig(t)
	sim := actionsim.New(actionsim.Objs{}, pack)
	sim.OpenBank(bank)
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	if enabled {
		d.Manip = &ManipConfig{Enabled: true}
	}
	return sim, d, actions.NewBank(sim, actionsim.Components())
}

func idByName(t *testing.T, d *Deps, name string) int32 {
	t.Helper()
	for _, id := range d.Config.ObjIDs(name) {
		if o, ok := d.Config.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatalf("no %s in the config", name)
	return 0
}

// The real config: the knife and the plain log come out of the bank when the pack lacks them, one each, and never when the
// manipulation is off or the pack already holds them.
func TestDrawKitTakesAKnifeAndAPlainLogFromTheBankRealConfig(t *testing.T) {
	cfg := loadConfig(t)
	knife, logs := int32(0), int32(0)
	for _, id := range cfg.ObjIDs("Knife") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			knife = int32(id)
		}
	}
	for _, id := range cfg.ObjIDs("Logs") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			logs = int32(id)
		}
	}
	if knife == 0 || logs == 0 {
		t.Fatalf("config has knife %d logs %d", knife, logs)
	}
	bank := []actionsim.Stack{{ID: knife, Count: 3}, {ID: logs, Count: 40}, {ID: 995, Count: 10}}
	for _, c := range []struct {
		name    string
		pack    []actionsim.Stack
		enabled bool
		drew    bool
		knives  int
		logs    int
	}{
		{"both missing", []actionsim.Stack{{ID: 1265, Count: 1}}, true, true, 1, 1},
		{"has the knife", []actionsim.Stack{{ID: 1265, Count: 1}, {ID: knife, Count: 1}}, true, true, 1, 1},
		{"has the kit", []actionsim.Stack{{ID: 1265, Count: 1}, {ID: knife, Count: 1}, {ID: logs, Count: 1}}, true, false, 1, 1},
		{"off", []actionsim.Stack{{ID: 1265, Count: 1}}, false, false, 0, 0},
	} {
		sim, d, b := drawRig(t, c.pack, bank, c.enabled)
		if got := DrawKit(d, b); got != c.drew {
			t.Errorf("%s: drew %v, want %v (pack %v)", c.name, got, c.drew, sim.Pack())
			continue
		}
		if k, l := countOf(sim.Pack(), knife), countOf(sim.Pack(), logs); k != c.knives || l != c.logs {
			t.Errorf("%s: pack holds %d knife(s) and %d log(s), want %d and %d", c.name, k, l, c.knives, c.logs)
		}
	}
	// A bank without them draws nothing.
	sim, d, b := drawRig(t, []actionsim.Stack{{ID: 1265, Count: 1}}, []actionsim.Stack{{ID: 995, Count: 10}}, true)
	if DrawKit(d, b) || len(sim.Pack()) != 1 {
		t.Errorf("drew from a bank that holds none: %v", sim.Pack())
	}
}

// The bank visit keeps the kit: a deposit of everything but the pickaxe and the keep list leaves the knife and the log in the pack.
func TestDepositKeepsTheKitRealConfig(t *testing.T) {
	cfg := loadConfig(t)
	var knife, logs, essence int32
	for _, p := range []struct {
		name string
		dst  *int32
	}{{"Knife", &knife}, {"Logs", &logs}, {"Rune essence", &essence}} {
		for _, id := range cfg.ObjIDs(p.name) {
			if o, ok := cfg.Obj(id); ok && !o.Noted() {
				*p.dst = int32(id)
			}
		}
	}
	sim, d, b := drawRig(t, []actionsim.Stack{{ID: 1265, Count: 1}, {ID: knife, Count: 1}, {ID: logs, Count: 1}, {ID: essence, Count: 1}, {ID: essence, Count: 1}}, nil, true)
	keep := append([]string{"Bronze pickaxe"}, KitKeep(d)...)
	if len(keep) != 3 {
		t.Fatalf("keep %v", keep)
	}
	DepositAllExcept(d, b, keep)
	pack := sim.Pack()
	if countOf(pack, essence) != 0 || countOf(pack, knife) != 1 || countOf(pack, logs) != 1 || countOf(pack, 1265) != 1 {
		t.Fatalf("pack after the deposit: %v", pack)
	}
}

func TestReportFoldsTheCallsAndSpeaksAtTheThreshold(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	var lines []string
	d := &Deps{Delay: RealDelay, Log: func(s string) { lines = append(lines, s) }}
	st := NewManipState()
	st.controller(bronze)
	st.note(true, 30, 90)
	st.note(false, 10, 80)
	st.report(d)
	if len(lines) != 0 {
		t.Fatalf("reported below the threshold: %q", lines)
	}
	st.note(true, 100, 430)
	st.report(d)
	if len(lines) != 1 {
		t.Fatalf("%d lines at the threshold: %q", len(lines), lines)
	}
	for _, want := range []string{"130 essence in 520 ticks with batches = 25.0 per 100 ticks", "10 essence in 80 ticks without = 12.5 per 100 ticks", "0 batches"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("report line %q lacks %q", lines[0], want)
		}
	}
	if st.manipTicks != 0 || st.plainTicks != 0 || st.manipEssence != 0 || st.plainEssence != 0 {
		t.Fatalf("the tallies were not reset: %+v", st)
	}
}

func TestAnUpgradedPickaxeGetsAControllerForItsOwnPeriod(t *testing.T) {
	st := NewManipState()
	a := st.controller(minemanipplan.Pickaxes[5])
	if b := st.controller(minemanipplan.Pickaxes[5]); b != a {
		t.Fatal("the same pickaxe rebuilt the controller and forgot the link's measured delay")
	}
	if c := st.controller(minemanipplan.Pickaxes[3]); c == a {
		t.Fatal("a steel pickaxe kept a controller built for a bronze one's period")
	}
}

func TestThePureModelAndTheGateAgreeOnTheSpacing(t *testing.T) {
	// minemanipplan's tests restate actions.ClickGapTicks (they cannot import the client); this keeps the two honest.
	if actions.ClickGapTicks != 2 {
		t.Fatalf("actions.ClickGapTicks is %d, minemanipplan's tests assume 2", actions.ClickGapTicks)
	}
	if minemanipplan.EngineEventsPerTick != actionsim.UserEventsPerTick {
		t.Fatalf("the engine reads %d user events a tick in the model and %d in the simulator", minemanipplan.EngineEventsPerTick, actionsim.UserEventsPerTick)
	}
}
