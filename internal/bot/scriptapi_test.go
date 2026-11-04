package bot

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// apiSession is an actions.Session over a real gamestate.State that records what is sent.
type apiSession struct {
	st     *gamestate.State
	cheats anticheat.Counters

	mu        sync.Mutex
	sent      [][]byte
	afterSend func()
}

func newAPISession() *apiSession { return &apiSession{st: gamestate.New()} }

func (f *apiSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(32)
	build(w)
	f.mu.Lock()
	f.sent = append(f.sent, append([]byte(nil), w.Data[:w.Pos]...))
	after := f.afterSend
	f.mu.Unlock()
	if after != nil {
		after()
	}
	return nil
}

func (f *apiSession) State() *gamestate.State     { return f.st }
func (f *apiSession) Cheats() *anticheat.Counters { return &f.cheats }

func (f *apiSession) sentPackets() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.sent...)
}

// bits packs [width, value] fields most-significant bit first, as PLAYER_INFO's bit access does.
func bits(fields ...[2]uint32) []byte {
	var out []byte
	pos := 0
	for _, f := range fields {
		for i := int(f[0]) - 1; i >= 0; i-- {
			for len(out) <= pos/8 {
				out = append(out, 0)
			}
			if (f[1]>>uint(i))&1 == 1 {
				out[pos/8] |= 0x80 >> uint(pos%8)
			}
			pos++
		}
	}
	return out
}

func rebuildAt(st *gamestate.State, zoneX, zoneZ int) {
	w := packet.NewWriter(4)
	w.P2(uint16(zoneX))
	w.P2(uint16(zoneZ))
	st.Handle(serverprot.REBUILD_NORMAL, w.Data[:w.Pos])
}

// placeAt sends a PLAYER_INFO teleporting the local player to a build-local tile.
func placeAt(st *gamestate.State, level, localX, localZ int) {
	st.Handle(serverprot.PLAYER_INFO, bits(
		[2]uint32{1, 1}, [2]uint32{2, 3},
		[2]uint32{2, uint32(level)}, [2]uint32{7, uint32(localX)}, [2]uint32{7, uint32(localZ)},
		[2]uint32{1, 0}, [2]uint32{1, 0},
		[2]uint32{8, 0}, [2]uint32{11, 2047},
	))
}

func tick(st *gamestate.State) {
	st.Handle(serverprot.PLAYER_INFO, bits([2]uint32{1, 0}, [2]uint32{8, 0}, [2]uint32{11, 2047}))
}

// readyScene builds a scene with the player on a known tile.
func readyScene(st *gamestate.State) {
	rebuildAt(st, 400, 400)
	placeAt(st, 0, 60, 60)
}

// ticker feeds a tick every interval until the returned func is called.
func ticker(st *gamestate.State, interval time.Duration) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				tick(st)
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

func apiContext(sess *apiSession, log func(string)) *Context {
	return NewContext(sess, "", nil, nil, nil, nil, nil, nil, nil, nil, log)
}

func TestDelayTicksWaitsForObservedTicks(t *testing.T) {
	sess := newAPISession()
	ctx := apiContext(sess, nil)
	stop := ticker(sess.st, 2*time.Millisecond)
	defer stop()

	start := sess.st.Tick()
	if !ctx.DelayTicks(3) {
		t.Fatal("DelayTicks(3) returned false with ticks arriving")
	}
	if got := sess.st.Tick() - start; got < 3 {
		t.Fatalf("returned after %d ticks, want at least 3", got)
	}
	if !ctx.DelayTicks(0) {
		t.Fatal("DelayTicks(0) on a running context must return true")
	}
}

func TestDelayTicksGivesUpWithoutTicksOrSession(t *testing.T) {
	old := tickStallTimeout
	tickStallTimeout = 30 * time.Millisecond
	defer func() { tickStallTimeout = old }()

	sess := newAPISession()
	ctx := apiContext(sess, nil)
	began := time.Now()
	if ctx.DelayTicks(1) {
		t.Fatal("DelayTicks returned true with no ticks arriving")
	}
	if time.Since(began) > time.Second {
		t.Fatalf("stall detection took %v", time.Since(began))
	}

	if NewContext(nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil).DelayTicks(1) {
		t.Fatal("DelayTicks without a session must return false")
	}
}

func TestDelayTicksReturnsOnStop(t *testing.T) {
	sess := newAPISession()
	ctx := apiContext(sess, nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctx.requestStop()
	}()
	began := time.Now()
	if ctx.DelayTicks(1000) {
		t.Fatal("DelayTicks returned true after a stop")
	}
	if time.Since(began) > time.Second {
		t.Fatalf("stop took %v to take effect", time.Since(began))
	}
}

func TestDelayUntilTicks(t *testing.T) {
	sess := newAPISession()
	ctx := apiContext(sess, nil)
	stop := ticker(sess.st, 2*time.Millisecond)
	defer stop()

	base := sess.st.Tick()
	if !ctx.DelayUntilTicks(func() bool { return sess.st.Tick() >= base+2 }, 50) {
		t.Fatal("condition that becomes true within the budget reported false")
	}

	base = sess.st.Tick()
	if ctx.DelayUntilTicks(func() bool { return false }, 3) {
		t.Fatal("condition that never holds reported true")
	}
	if got := sess.st.Tick() - base; got < 3 {
		t.Fatalf("gave up after %d ticks, want at least 3", got)
	}

	calls := 0
	if !ctx.DelayUntilTicks(func() bool { calls++; return true }, 0) || calls != 1 {
		t.Fatalf("maxTicks 0 must check the condition exactly once, got %d calls", calls)
	}
}

func TestCadenceFromLoopDelay(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want Cadence
	}{
		{0, FrameCadence},
		{600 * time.Millisecond, Cadence{Ticks: 1}},
		{250 * time.Millisecond, Cadence{Every: 250 * time.Millisecond}},
	}
	for _, c := range cases {
		if got := CadenceFromLoopDelay(c.in); got != c.want {
			t.Errorf("CadenceFromLoopDelay(%v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestPaceWaitsTicksOrWallClock(t *testing.T) {
	sess := newAPISession()
	ctx := apiContext(sess, nil)
	stop := ticker(sess.st, 2*time.Millisecond)
	defer stop()

	base := sess.st.Tick()
	if !ctx.Pace(Cadence{Ticks: 2}) || sess.st.Tick()-base < 2 {
		t.Fatal("Pace(2 ticks) did not wait two ticks")
	}
	began := time.Now()
	if !ctx.Pace(Cadence{Every: 15 * time.Millisecond}) || time.Since(began) < 15*time.Millisecond {
		t.Fatal("Pace(15ms) did not wait")
	}
}

// eventBot subscribes through ctx.On in OnStart and stops itself after one loop.
type eventBot struct {
	mu    sync.Mutex
	ticks int
}

func (b *eventBot) OnStart(ctx *Context) error {
	ctx.On(func(ev gamestate.Event) {
		b.mu.Lock()
		b.ticks++
		b.mu.Unlock()
	}, gamestate.EventTick)
	return nil
}

func (b *eventBot) Loop(ctx *Context) error {
	st := ctx.Session.State()
	tick(st)
	st.Handle(serverprot.VARP_SMALL, []byte{0, 173, 1}) // a varp.changed, filtered out
	tick(st)
	ctx.requestStop()
	return nil
}

func (b *eventBot) OnStop(ctx *Context) {}

func (b *eventBot) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ticks
}

func TestOnFiltersKindsAndUnsubscribesWhenTheBotFinishes(t *testing.T) {
	sess := newAPISession()
	b := &eventBot{}
	r := NewRunner("events", b, apiContext(sess, nil))
	r.Start()
	r.Wait()
	if got := b.count(); got != 2 {
		t.Fatalf("tick callbacks during the run = %d, want 2", got)
	}
	tick(sess.st)
	if got := b.count(); got != 2 {
		t.Fatalf("a subscription survived the bot finishing: %d callbacks", got)
	}

	if cancel := NewContext(nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil).On(func(gamestate.Event) {}); cancel == nil {
		t.Fatal("On without a session must still return a usable cancel func")
	}
}

type recordingTask struct {
	name     string
	valid    bool
	err      error
	log      *[]string
	executed int
}

func (r *recordingTask) Validate(ctx *Context) bool {
	*r.log = append(*r.log, "validate "+r.name)
	return r.valid
}

func (r *recordingTask) Execute(ctx *Context) error {
	*r.log = append(*r.log, "execute "+r.name)
	r.executed++
	return r.err
}

func TestTaskBotWaitsForTheScene(t *testing.T) {
	sess := newAPISession()
	var logs []string
	var calls []string
	ctx := apiContext(sess, func(s string) { logs = append(logs, s) })
	bot := &TaskBot{Cadence: Cadence{Every: time.Millisecond}}
	bot.Add(&recordingTask{name: "a", valid: true, log: &calls})

	for i := 0; i < 3; i++ {
		if err := bot.Loop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("tasks ran before the scene was ready: %v", calls)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "waiting for scene (state=0) before tasks") {
		t.Fatalf("scene-wait logs = %q, want one throttled line", logs)
	}
}

func TestTaskBotRunsTheFirstValidTask(t *testing.T) {
	sess := newAPISession()
	readyScene(sess.st)
	var calls []string
	ctx := apiContext(sess, nil)
	a := &recordingTask{name: "a", valid: false, log: &calls}
	b := &recordingTask{name: "b", valid: true, log: &calls}
	c := &recordingTask{name: "c", valid: true, log: &calls}
	bot := &TaskBot{Tasks: []Task{a, b, c}, Cadence: Cadence{Every: time.Millisecond}}

	if err := bot.Loop(ctx); err != nil {
		t.Fatal(err)
	}
	want := "validate a,validate b,execute b"
	if got := strings.Join(calls, ","); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}

	b.err = errors.New("boom")
	if err := bot.Loop(ctx); err == nil || err.Error() != "boom" {
		t.Fatalf("Loop error = %v, want the task's error", err)
	}

	fn := TaskFunc{ValidateFunc: func(*Context) bool { return true }}
	if !fn.Validate(ctx) || fn.Execute(ctx) != nil || (TaskFunc{}).Validate(ctx) {
		t.Fatal("TaskFunc adapter misbehaves")
	}
}

type branch struct {
	valid            bool
	success, failure TreeNode
}

func (b *branch) Validate(*Context) bool { return b.valid }
func (b *branch) Success() TreeNode      { return b.success }
func (b *branch) Failure() TreeNode      { return b.failure }

type leaf struct {
	name string
	hit  *string
}

func (l *leaf) Execute(*Context) error { *l.hit = l.name; return nil }

func TestTreeBotWalksBranchesToALeaf(t *testing.T) {
	sess := newAPISession()
	readyScene(sess.st)
	ctx := apiContext(sess, nil)
	var hit string
	root := &branch{valid: false, success: &leaf{"yes", &hit}, failure: &branch{valid: true, success: &leaf{"inner", &hit}, failure: &leaf{"no", &hit}}}
	bot := &TreeBot{Root: func(*Context) TreeNode { return root }, Cadence: Cadence{Every: time.Millisecond}}
	if err := bot.OnStart(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bot.Loop(ctx); err != nil {
		t.Fatal(err)
	}
	if hit != "inner" {
		t.Fatalf("executed leaf %q, want inner", hit)
	}

	loop := &branch{valid: true}
	loop.success = loop
	bot.Root = func(*Context) TreeNode { return loop }
	if err := bot.Loop(ctx); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("cyclic tree error = %v", err)
	}

	bot.Root = func(*Context) TreeNode { return 42 }
	if err := bot.Loop(ctx); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("bad node error = %v", err)
	}

	if err := (&TreeBot{}).OnStart(ctx); err == nil {
		t.Fatal("a TreeBot without Root must fail to start")
	}
}

func TestTeleportConfirmsArrival(t *testing.T) {
	sess := newAPISession()
	readyScene(sess.st)
	ctx := apiContext(sess, nil)

	sess.afterSend = func() { go placeAt(sess.st, 0, 90, 30) } // 30 tiles away
	arrived, err := ctx.Teleport("Varrock", time.Second)
	if err != nil || !arrived {
		t.Fatalf("Teleport = %v, %v; want arrived", arrived, err)
	}
	w := packet.NewWriter(4)
	w.P1Enc(clientprot.IF_BUTTON)
	w.P2(1164)
	if got := sess.sentPackets(); len(got) != 1 || string(got[0]) != string(w.Data[:w.Pos]) {
		t.Fatalf("sent %x, want one IF_BUTTON 1164", got)
	}

	sess.afterSend = nil
	arrived, err = ctx.Teleport("Lumbridge", 30*time.Millisecond)
	if err != nil || arrived {
		t.Fatalf("Teleport with no movement = %v, %v; want not arrived", arrived, err)
	}

	if _, err := ctx.Teleport("Paddewwa", time.Millisecond); err == nil {
		t.Fatal("an unknown teleport must return an error")
	}
	if _, err := apiContext(newAPISession(), nil).Teleport("Varrock", time.Millisecond); err != errNoTile {
		t.Fatalf("Teleport without a tile = %v, want errNoTile", err)
	}
}
