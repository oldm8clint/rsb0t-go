package essencerunner

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/chopmanipplan"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/minemanipplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE REAL Mine, OVER THE ENGINE MODEL. These tests run essencerunner.Mine itself (the click closure, the hold, the chat policy, the
// tick feed, the controller, the batch writer, the stall and full-pack rules) against minemanipplan.Engine, with the real config and
// loc index (RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX; they skip without). A goroutine plays the server: every few milliseconds it runs
// one engine tick and shows the client its output the way processClientsOut orders it (chat interfaces, the tick's PLAYER_INFO, then
// the inventory and the experience). Game ticks are therefore short in wall time; nothing in Mine's manipulation path waits on
// wall time except the tick feed, which waits for the next PLAYER_INFO. A batch is only written for the newest tick, so the game
// tick must be longer than the scheduler's jitter (Windows timers are about 15 ms): 25 ms here, 600 ms live.

type e2eAnim struct {
	*gamestate.State
	anim, at *atomic.Int64
}

func (a e2eAnim) SelfAnim() (int, int) { return int(a.anim.Load()), int(a.at.Load()) }

type e2e struct {
	t      *testing.T
	sim    *actionsim.Session
	d      *Deps
	eng    *minemanipplan.Engine
	essID  int32
	every  time.Duration
	anim   atomic.Int64
	animAt atomic.Int64

	mu       sync.Mutex
	arrive   map[int][]minemanipplan.Event
	tick     int
	shown    int
	xp       int
	modal    minemanipplan.Modal
	sendOps  []int
	stopOnce sync.Once
	done     chan struct{}
	finished chan struct{}
}

const (
	e2eLevelUpChat = 4416
	e2eMesbox      = 356
)

func newE2E(t *testing.T, p minemanipplan.Pickaxe, kit bool, levelEvery int) *e2e {
	t.Helper()
	cfg := loadConfig(t)
	ix := loadLocs(t)
	start := tile.Tile{X: 2924, Z: 4848}
	rocks := ix.Find(cfg, locindex.Query{X: start.X, Z: start.Z, Radius: 20, Name: essenceplan.EssenceRock, Op: essenceplan.MineOp}, nil)
	if len(rocks) == 0 {
		t.Skipf("the loc index has no essence rock near %v", start)
	}
	id := func(name string) int32 {
		for _, i := range cfg.ObjIDs(name) {
			if o, ok := cfg.Obj(i); ok && !o.Noted() {
				return int32(i)
			}
		}
		t.Fatalf("no %s in the config", name)
		return 0
	}
	pack := []actionsim.Stack{{ID: id(p.Item), Count: 1}}
	if kit {
		pack = append(pack, actionsim.Stack{ID: id("Knife"), Count: 1}, actionsim.Stack{ID: id("Logs"), Count: 1})
	}
	e := &e2e{t: t, every: 25 * time.Millisecond, essID: id("Rune essence"), arrive: map[int][]minemanipplan.Event{}, done: make(chan struct{}), finished: make(chan struct{})}
	e.eng = minemanipplan.NewEngine(p, kit)
	e.eng.LevelEvery = levelEvery
	e.eng.Delay = -1000
	e.sim = actionsim.New(actionsim.Objs{}, pack)
	e.sim.StandAt(rocks[0].X, rocks[0].Z)
	e.sim.OnSend = e.onSend
	// The Mining and Fletching levels the client has been told, and the first experience (the first UPDATE_STAT of a skill after
	// login fires no event).
	stat := func(s, xp, level int) {
		w := packet.NewWriter(8)
		w.P1(byte(s))
		w.P4(int32(xp))
		w.P1(byte(level))
		e.sim.State().Handle(serverprot.UPDATE_STAT, w.Data[:w.Pos])
	}
	stat(MiningStat, 0, 99)
	stat(fletchingStat, 0, 99)
	e.d = &Deps{
		Session: e.sim, Locs: ix, Config: cfg, Chat: chatlog.New(),
		Components: archive.Components{
			e2eMesbox:      {ID: e2eMesbox, Type: archive.TypeLayer, Children: []int{357}},
			357:            {ID: 357, ButtonType: archive.ButtonContinue},
			e2eLevelUpChat: {ID: e2eLevelUpChat, Type: archive.TypeLayer, Children: []int{4440}},
			4440:           {ID: 4440, ButtonType: archive.ButtonContinue},
		},
		Delay: func(d time.Duration) bool { time.Sleep(d); return true },
	}
	e.anim.Store(int64(chopmanipplan.NoAnim))
	oldSrc := manipSource
	manipSource = func(st *gamestate.State) tickSource { return e2eAnim{State: st, anim: &e.anim, at: &e.animAt} }
	t.Cleanup(func() { manipSource = oldSrc })
	return e
}

// onSend files one websocket message's packets for the engine (the Send callback of the simulator).
func (e *e2e) onSend(b []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	at := e.tick + 1
	for len(b) > 0 {
		op := int(b[0])
		var ev minemanipplan.Event
		n := 0
		switch {
		case op == clientprot.OPHELDU:
			ev, n = minemanipplan.KnifeOnLog, 13
		case op == clientprot.IF_BUTTON:
			ev, n = minemanipplan.Make1, 3
		case op == clientprot.CLOSE_MODAL:
			ev, n = minemanipplan.CloseModal, 1
		case op >= clientprot.OPLOC1 && op <= clientprot.OPLOC5:
			ev, n = minemanipplan.ClickRock, 7
		case op == clientprot.RESUME_PAUSEBUTTON:
			ev, n = minemanipplan.Continue, 3
		case op == clientprot.ANTICHEAT_CYCLELOGIC2:
			b = b[2+int(b[1]):]
			continue
		case op == clientprot.NO_TIMEOUT:
			b = b[1:]
			continue
		default:
			e.t.Errorf("e2e: a packet the model does not read: opcode %d", op)
			return
		}
		e.sendOps = append(e.sendOps, op)
		e.arrive[at] = append(e.arrive[at], ev)
		b = b[n:]
	}
}

// step is one game tick: the engine, then what the client reads of it.
func (e *e2e) step() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tick++
	evs := e.arrive[e.tick]
	delete(e.arrive, e.tick)
	s := e.eng.Step(evs)
	e.anim.Store(int64(s.Anim))
	// The client counts ticks by PLAYER_INFO (StandAt sent one before the first engine tick), and the feed matches an animation to
	// the tick it arrived on: the PLAYER_INFO about to be sent.
	e.animAt.Store(int64(e.sim.State().Tick() + 1))
	if s.Modal != e.modal {
		switch s.Modal {
		case minemanipplan.ModalChat:
			e.sim.OpenChat(e2eLevelUpChat)
		case minemanipplan.ModalBox:
			e.sim.OpenChat(e2eMesbox)
		default:
			e.sim.CloseChat()
		}
		e.modal = s.Modal
	}
	e.sim.AdvanceTick() // the PLAYER_INFO; the tick feed is told here
	for e.shown < s.Essence {
		e.shown++
		e.sim.Give(e.essID, 1)
		e.xp += minemanipplan.EssenceXP
		w := packet.NewWriter(8)
		w.P1(MiningStat)
		w.P4(int32(e.xp))
		w.P1(99)
		e.sim.State().Handle(serverprot.UPDATE_STAT, w.Data[:w.Pos])
	}
}

func (e *e2e) run() {
	defer close(e.finished)
	tk := time.NewTicker(e.every)
	defer tk.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-tk.C:
			e.step()
		}
	}
}

func (e *e2e) stop() {
	e.stopOnce.Do(func() { close(e.done) })
	<-e.finished
}

func (e *e2e) mineUntilFull(max time.Duration) (MineResult, bool) {
	go e.run()
	defer e.stop()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		r := Mine(e.d)
		if r.Reason == "full" || r.Reason == "no-rock" || r.Reason == "stopped" {
			return r, r.Reason == "full"
		}
	}
	return MineResult{}, false
}

func TestTheRealMineWithABatchFillsAPackOf25EssenceInAboutThreeTicksEach(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	e := newE2E(t, bronze, true, 0)
	e.d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
	var log []string
	e.d.Log = func(s string) { log = append(log, s) }
	res, full := e.mineUntilFull(20 * time.Second)
	if !full {
		t.Fatalf("Mine did not fill the pack: %+v (log %v)", res, log)
	}
	if e.eng.Essence != 25 {
		t.Fatalf("%d essence, want 25: the knife and the log take two of the 28 slots", e.eng.Essence)
	}
	rolls := e.eng.Rolls
	if rolls[0] > 8 {
		t.Errorf("first essence at tick %d: a plain bronze click would be at 8, the opening batch at 3", rolls[0])
	}
	if len(rolls) >= 20 {
		for i, g := range gapsOf(rolls[2:]) {
			if g != 3 {
				t.Fatalf("gap %d is %d ticks, want 3 (rolls %v)", i, g, rolls)
			}
		}
	}
	span := rolls[len(rolls)-1] - rolls[0]
	t.Logf("25 essence over %d ticks (first at tick %d, the plain bronze load is 215); %d batches, %d clicks", span, rolls[0], res.Batches, res.Clicks)
	if span != 3*(25-1) {
		t.Errorf("25 essence over %d ticks, want %d", span, 3*24)
	}
	if res.Batches < 20 || res.Gained != 25 {
		t.Errorf("result %+v: want about 25 batches and 25 essence gained", res)
	}
	if e.eng.Leaks != 0 {
		t.Errorf("%d carrier log(s) were fletched", e.eng.Leaks)
	}
	for tick, n := range e.eng.Events {
		if n > minemanipplan.BatchEvents {
			t.Errorf("%d user events in tick %d", n, tick)
		}
	}
}

func TestTheRealMinePlainStillFills27InTheRatePlusOnePeriod(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	e := newE2E(t, bronze, false, 0)
	e.every = 8 * time.Millisecond
	res, full := e.mineUntilFull(40 * time.Second)
	if !full || e.eng.Essence != 27 {
		t.Fatalf("plain mine: %+v, %d essence", res, e.eng.Essence)
	}
	allEq := func(g []int, want int) bool {
		for _, x := range g {
			if x != want {
				return false
			}
		}
		return true
	}
	if !allEq(gapsOf(e.eng.Rolls), bronze.Period()) || res.Batches != 0 {
		t.Errorf("gaps %v batches %d, want all %d and none", gapsOf(e.eng.Rolls), res.Batches, bronze.Period())
	}
	if res.Clicks != 1 {
		t.Errorf("%d clicks for a plain load, want one", res.Clicks)
	}
}

func TestTheRealMineWithABatchSurvivesLevelUps(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	e := newE2E(t, bronze, true, 4)
	e.d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
	var log []string
	e.d.Log = func(s string) { log = append(log, s) }
	res, full := e.mineUntilFull(30 * time.Second)
	if !full || e.eng.Essence != 25 {
		t.Fatalf("with level-ups: %+v, %d essence (log %v)", res, e.eng.Essence, log)
	}
	if e.eng.Chats < 5 || res.Dialogs < e.eng.Chats-1 {
		t.Errorf("%d level-up chats opened, %d answered", e.eng.Chats, res.Dialogs)
	}
	// The cadence stays 3 across answered chats; at worst one plain-period gap (a slow answer).
	for i, g := range gapsOf(e.eng.Rolls) {
		if g > 3+bronze.Period() {
			t.Errorf("gap %d across a level-up is %d ticks (rolls %v)", i, g, e.eng.Rolls)
			break
		}
	}
	span := e.eng.Rolls[len(e.eng.Rolls)-1] - e.eng.Rolls[0]
	t.Logf("25 essence over %d ticks (first at %d) with %d level-up chats, %d answered; %d batches, %d clicks", span, e.eng.Rolls[0], e.eng.Chats, res.Dialogs, res.Batches, res.Clicks)
	if span > 3*24+3*bronze.Period() {
		t.Errorf("25 essence over %d ticks with %d level-ups", span, e.eng.Chats)
	}
	if e.eng.Leaks != 0 {
		t.Errorf("%d carrier log(s) fletched", e.eng.Leaks)
	}
	for tick, n := range e.eng.Events {
		if n > minemanipplan.EngineEventsPerTick {
			t.Errorf("%d user events in tick %d", n, tick)
		}
	}
}

func TestTheRealMineWithABatchAndNoKitIsThePlainMine(t *testing.T) {
	bronze := minemanipplan.Pickaxes[5]
	e := newE2E(t, bronze, false, 0)
	e.every = 8 * time.Millisecond
	e.d.Manip = &ManipConfig{Enabled: true, State: NewManipState()}
	var log []string
	e.d.Log = func(s string) { log = append(log, s) }
	res, full := e.mineUntilFull(40 * time.Second)
	if !full || e.eng.Essence != 27 || res.Batches != 0 {
		t.Fatalf("no kit: %+v, %d essence", res, e.eng.Essence)
	}
	said := 0
	for _, l := range log {
		if strings.Contains(l, "no knife, or no log") {
			said++
		}
	}
	if said != 1 {
		t.Errorf("the missing kit is said %d times: %v", said, log)
	}
}
