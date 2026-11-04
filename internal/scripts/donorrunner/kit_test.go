package donorrunner

import (
	"os"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/orderplan"
)

// content/pack/obj.pack ids.
const (
	kitCake     = 1891
	kitFireRune = 554
	kitShark    = 385
	kitBones    = 526
)

// kitCfg is the config archive, decoded once per test binary: it is read-only and safe to share.
var kitCfg = sync.OnceValues(func() (*cachecfg.Config, error) {
	return cachecfg.LoadFile(os.Getenv("RSB0T_CONFIG_JAG"))
})

func kitConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	if os.Getenv("RSB0T_CONFIG_JAG") == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := kitCfg()
	if err != nil {
		t.Fatalf("load %s: %v", os.Getenv("RSB0T_CONFIG_JAG"), err)
	}
	return cfg
}

func noteOf(cfg *cachecfg.Config, id int32) int32 {
	o, ok := cfg.Obj(int(id))
	if !ok {
		return 0
	}
	for _, n := range cfg.ObjIDs(o.Name) {
		if no, ok := cfg.Obj(n); ok && no.Noted() && no.CertLink == int(id) {
			return int32(n)
		}
	}
	return 0
}

func simObjs(cfg *cachecfg.Config) actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
		NoteOf:    func(id int32) int32 { return noteOf(cfg, id) },
		UnnoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || !o.Noted() {
				return 0
			}
			return int32(o.CertLink)
		},
	}
}

func count(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func fastDelay(time.Duration) bool { time.Sleep(2 * time.Millisecond); return true }

// DepositForKit banks everything but the keep list and reports the room it made.
func TestDepositForKitBanksAllButTheKeepList(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitBones, Count: 1}, {ID: kitBones, Count: 1}, {ID: kitShark, Count: 1}, {ID: kitFireRune, Count: 30}})
	sim.OpenBank(nil)
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {}, Delay: fastDelay}
	free := DepositForKit(d, actions.NewBank(sim, d.Components), []string{"Fire rune"})
	if free != 27 || count(sim.Pack(), kitFireRune) != 30 || count(sim.Bank(), kitBones) != 2 {
		t.Fatalf("free %d pack %+v bank %+v", free, sim.Pack(), sim.Bank())
	}
}

// UnnoteKit draws notes back as items as far as the pack has room, and reports what stayed banked.
// Review of r4, blocking item 3: 21 food arrived as notes and only 5 came back; the rest must be
// reported as banked, never counted as in the pack.
func TestUnnoteKitReportsWhatStayedBanked(t *testing.T) {
	cfg := kitConfig(t)
	noteCake := noteOf(cfg, kitCake)
	pack := []actionsim.Stack{{ID: noteCake, Count: 21}}
	for i := 0; i < 22; i++ {
		pack = append(pack, actionsim.Stack{ID: kitBones, Count: 1})
	}
	sim := actionsim.New(simObjs(cfg), pack)
	sim.OpenBank(nil)
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {}, Delay: fastDelay}
	res := UnnoteKit(d, actions.NewBank(sim, d.Components), []string{"Cake"})
	if res.Unnoted["Cake"] != 6 || res.Banked["Cake"] != 15 {
		t.Fatalf("unnoted %v banked %v, want 6 drawn back (the room left) and 15 banked", res.Unnoted, res.Banked)
	}
	if count(sim.Pack(), kitCake) != 6 || count(sim.Pack(), noteCake) != 0 || count(sim.Bank(), kitCake) != 15 {
		t.Fatalf("pack %+v bank %+v", sim.Pack(), sim.Bank())
	}
}

// fakeKitMule answers kit orders on bus as the MuleReceiver does: a queued ack, the plan, a trade
// request, and once the donor answers it, the kit placed on the screen one line at a time.
type fakeKitMule struct {
	sim     *actionsim.Session
	bus     *mulebus.Bus
	chat    *chatlog.Log
	plan    []mulebus.KitLine
	theirs  []actionsim.Stack
	mu      sync.Mutex
	cancels int
	// requestAfter delays the mule's trade request after its plan (a long bank visit).
	requestAfter time.Duration
}

// start creates the mule's bus watcher and then runs it. The watcher must exist BEFORE the
// donor publishes its order, or a watcher created inside the goroutine can start after the
// order and never see it; only the donor's re-publish every AskRepeatEvery (10s) rescued it,
// so an ask with a shorter budget failed every time the race went that way.
func (m *fakeKitMule) start(stop <-chan struct{}) {
	w := mulebus.NewWatcher()
	go m.run(stop, w)
}

func (m *fakeKitMule) run(stop <-chan struct{}, w *mulebus.Watcher) {
	opened := false
	m.sim.OnSend = func(b []byte) {
		if opened || len(b) < 3 || int(b[len(b)-3]) != clientprot.OPPLAYER4 {
			return
		}
		opened = true
		tr := &actionsim.Trade{Partner: "mule", AcceptOffer: true, AcceptConfirm: true}
		go func() {
			m.sim.OpenTrade(tr)
			var placed []actionsim.Stack
			for _, st := range m.theirs {
				time.Sleep(20 * time.Millisecond)
				placed = append(placed, st)
				m.sim.SetTheirs(placed)
			}
		}()
	}
	for {
		select {
		case <-stop:
			return
		case <-time.After(5 * time.Millisecond):
		}
		for _, msg := range w.Poll(m.bus) {
			switch {
			case msg.Kind == mulebus.KindKitOrder:
				m.bus.Publish(mulebus.KitAnswerMessage("mule", msg.From, msg.KitID, nil, "", time.Second))
				m.bus.Publish(mulebus.KitAnswerMessage("mule", msg.From, msg.KitID, m.plan, "", 0))
				if m.requestAfter > 0 {
					go func() { time.Sleep(m.requestAfter); m.chat.Record("Mule:tradereq:") }()
				} else {
					m.chat.Record("Mule:tradereq:")
				}
			case msg.Kind == mulebus.KindCancel && msg.KitID != "":
				m.mu.Lock()
				m.cancels++
				m.mu.Unlock()
			}
		}
	}
}

// AwaitKit end to end: one kit order, the plan, one trade with the lines placed one at a time,
// receipt per line with notes counted and reported, and a kit cancel on the way out.
func TestAwaitKitReceivesTheWholeKitInOneTrade(t *testing.T) {
	cfg := kitConfig(t)
	noteCake := noteOf(cfg, kitCake)
	sim := actionsim.New(simObjs(cfg), nil)
	sim.ShowPlayers("mule")
	chat := chatlog.New()
	bus := mulebus.NewBus()
	m := &fakeKitMule{sim: sim, bus: bus, chat: chat,
		plan:   []mulebus.KitLine{{Item: "Fire rune", Qty: 100}, {Item: "Cake", Qty: 10, Noted: true}, {Item: "Shark", Qty: 2}},
		theirs: []actionsim.Stack{{ID: kitFireRune, Count: 100}, {ID: noteCake, Count: 10}, {ID: kitShark, Count: 1}, {ID: kitShark, Count: 1}}}
	stop := make(chan struct{})
	defer close(stop)
	m.start(stop)
	d := &Deps{Session: sim, Config: cfg, Chat: chat, Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 20 * time.Second,
		Lines: []orderplan.KitLine{{Item: "Fire rune", Qty: 100}, {Item: "Cake", Qty: 10}, {Item: "Shark", Qty: 2}}})
	if !res.Answered || res.Got["Fire rune"] != 100 || res.Got["Cake"] != 10 || res.Got["Shark"] != 2 || res.Noted["Cake"] != 10 {
		t.Fatalf("result %+v", res)
	}
	if len(res.Declined) != 0 {
		t.Fatalf("declined %v", res.Declined)
	}
	// The mule's watcher polls every 5 ms; on a loaded machine its goroutine can wait longer than a
	// fixed 30 ms for a slice, so wait for the cancel (up to 5 s) and then for a second one that must not come.
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		m.mu.Lock()
		n := m.cancels
		m.mu.Unlock()
		if n > 0 {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancels != 1 {
		t.Fatalf("kit cancels published: %d, want 1", m.cancels)
	}
}

// Review of r4, blocking item 2: an ask that stops waiting tells the mule to drop the kit, and the
// order says how long it would wait. A mule that never answers comes back Answered=false, so the
// caller falls back per item.
func TestAwaitKitCancelsWhenItStopsWaiting(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	d := &Deps{Session: sim, Config: cfg, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 50 * time.Millisecond, ID: "k9",
		Lines: []orderplan.KitLine{{Item: "Cake", Qty: 5}}})
	if res.Answered || res.Reason == "" {
		t.Fatalf("result %+v", res)
	}
	var order, cancel *mulebus.Message
	for _, m := range w.Poll(bus) {
		m := m
		switch {
		case m.Kind == mulebus.KindKitOrder && order == nil:
			order = &m
		case m.Kind == mulebus.KindCancel && m.KitID == "k9":
			cancel = &m
		}
	}
	if order == nil || cancel == nil {
		t.Fatalf("order %v cancel %v", order, cancel)
	}
	if order.EtaMs <= 0 || order.EtaMs > 50 {
		t.Fatalf("the order carries a wait of %dms, want the ask's own 50ms budget", order.EtaMs)
	}
}

// Review of r4: a full pack reported 0 free slots, which the mule reads as "not reported" and
// plans a whole trade screen into. A readable full pack asks nothing.
func TestAwaitKitDoesNotAskWithAFullPack(t *testing.T) {
	cfg := kitConfig(t)
	var pack []actionsim.Stack
	for i := 0; i < 28; i++ {
		pack = append(pack, actionsim.Stack{ID: kitBones, Count: 1})
	}
	sim := actionsim.New(simObjs(cfg), pack)
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	d := &Deps{Session: sim, Config: cfg, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: time.Second, Lines: []orderplan.KitLine{{Item: "Cake", Qty: 5}}})
	if res.Reason != noRoomReason {
		t.Fatalf("result %+v, want %q", res, noRoomReason)
	}
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindKitOrder {
			t.Fatal("a full pack published a kit order")
		}
	}
}

// The order is stamped with this donor's world, so a mule elsewhere can refuse it at once.
func TestAwaitKitStampsItsWorld(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	d := &Deps{Session: sim, Config: cfg, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	d.MyWorld = 2
	AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 20 * time.Millisecond, Lines: []orderplan.KitLine{{Item: "Cake", Qty: 5}}})
	seen := false
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindKitOrder {
			seen = true
			if m.World != 2 {
				t.Fatalf("the kit order carries world %d, want 2", m.World)
			}
		}
	}
	if !seen {
		t.Fatal("no kit order was published")
	}
}

// revK S2: once the mule has sent its plan it is drawing the kit, and the donor must still be there
// when the trade opens. The ask's deadline is extended from the plan, so an ask whose budget runs
// out mid-draw still receives the kit. CONTROL-shaped: the same mule with no plan delay is
// received well inside the budget (TestAwaitKitReceivesTheWholeKitInOneTrade).
func TestAwaitKitWaitsForTheTradeOnceThePlanHasCome(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	sim.ShowPlayers("mule")
	chat := chatlog.New()
	bus := mulebus.NewBus()
	m := &fakeKitMule{sim: sim, bus: bus, chat: chat, requestAfter: 800 * time.Millisecond,
		plan:   []mulebus.KitLine{{Item: "Fire rune", Qty: 100}},
		theirs: []actionsim.Stack{{ID: kitFireRune, Count: 100}}}
	stop := make(chan struct{})
	defer close(stop)
	m.start(stop)
	d := &Deps{Session: sim, Config: cfg, Chat: chat, Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 300 * time.Millisecond,
		Lines: []orderplan.KitLine{{Item: "Fire rune", Qty: 100}}})
	if res.Got["Fire rune"] != 100 {
		t.Fatalf("the ask gave up while the mule was drawing its planned kit: %+v", res)
	}
}
