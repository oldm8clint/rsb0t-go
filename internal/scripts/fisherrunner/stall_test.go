package fisherrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/webwalk"
)

// stallClock is a hand-wound clock for the staller.
type stallClock struct{ t time.Time }

func (c *stallClock) now() time.Time          { return c.t }
func (c *stallClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// stallRig is a cycle engine holding a Games necklace, with a running account on a walk.
type stallRig struct {
	e    *cycleEngine
	d    *Deps
	s    *Stall
	clk  *stallClock
	logs []string
}

func newStallRig(t *testing.T, pack ...string) *stallRig {
	cfg := realConfig(t)
	e := newCycleEngine(t, cfg)
	r := &stallRig{e: e, clk: &stallClock{t: time.Unix(1_700_000_000, 0)}}
	r.s = &Stall{On: true, Now: r.clk.now}
	r.d = &Deps{Session: e, Config: cfg, Delay: e.delay, Stall: r.s, Log: func(s string) { r.logs = append(r.logs, s) }}
	if len(pack) == 0 {
		pack = []string{"Big fishing net", "Games necklace(8)", "Raw bass x3"}
	}
	e.fill(pack...)
	e.st.Handle(serverprot.VARP_SMALL, []byte{byte(gamestate.VarpRun >> 8), byte(gamestate.VarpRun & 0xff), 1})
	e.st.Handle(serverprot.UPDATE_RUNENERGY, []byte{80})
	return r
}

// poll is one walk poll with the walker's click sinceClick ago and target tiles ahead.
func (r *stallRig) poll(sinceClick time.Duration, ahead int) {
	r.s.poll(r.d, webwalk.AsidePoll{
		Me: webwalk.Point{X: 2600, Z: 3416}, Target: webwalk.Point{X: 2600 + ahead, Z: 3416}, SinceClick: sinceClick,
	})
}

func (r *stallRig) rubs() int { return r.e.sent[clientprot.OPHELD4] }

// A running walk rubs the account's necklace through scarabplan.ShouldRub, once every three ticks, and
// not otherwise: not walking, not with the click's target close, not before the click has had two ticks,
// not in combat, not with a bank open, and never with nothing to rub.
func TestStallRubsAThirdTickOnARunningWalkOnlyRealConfig(t *testing.T) {
	r := newStallRig(t)
	necklace := unnotedID(t, r.e.cfg, "Games necklace(8)", false)
	slot := -1
	for i, row := range r.e.pack {
		if row.id == necklace {
			slot = i
		}
	}
	r.poll(2*time.Second, 20)
	if r.rubs() != 1 {
		t.Fatalf("%d rubs on a running walk with the target 20 tiles ahead, want 1", r.rubs())
	}
	if p := r.e.frames[len(r.e.frames)-1][0]; int(p[0]) != clientprot.OPHELD4 || int(g2(p, 1)) != int(necklace) || g2(p, 3) != slot || g2(p, 5) != PlayerPack {
		t.Fatalf("the rub packet %v is not op 4 on the necklace in slot %d", p, slot)
	}
	r.poll(2*time.Second, 20) // the same instant: the rub's delay has not ended
	r.clk.advance(1000 * time.Millisecond)
	r.poll(2*time.Second, 20)
	if r.rubs() != 1 {
		t.Fatalf("%d rubs inside three ticks of the last", r.rubs())
	}
	r.clk.advance(900 * time.Millisecond) // 1.9 s: past three ticks of 600 ms
	r.poll(2*time.Second, 20)
	if r.rubs() != 2 {
		t.Fatalf("%d rubs after three ticks, want 2", r.rubs())
	}

	for name, c := range map[string]struct {
		setup func(r *stallRig)
		since time.Duration
		ahead int
	}{
		"walking, not running": {setup: func(r *stallRig) {
			r.e.st.Handle(serverprot.VARP_SMALL, []byte{byte(gamestate.VarpRun >> 8), byte(gamestate.VarpRun & 0xff), 0})
		}, since: 2 * time.Second, ahead: 20},
		"the click's target close": {since: 2 * time.Second, ahead: 5},
		"a click this tick":        {since: 300 * time.Millisecond, ahead: 20},
		"a bank open": {setup: func(r *stallRig) {
			r.e.st.Handle(serverprot.IF_OPENMAIN_SIDE, []byte{byte(actions.BankMainModal >> 8), byte(actions.BankMainModal & 0xff), 0x23, 0x28})
		}, since: 2 * time.Second, ahead: 20},
		"no rub item": {setup: func(r *stallRig) {
			r.e.pack = [28]slotRow{}
			r.e.fill("Big fishing net", "Raw bass x3")
		}, since: 2 * time.Second, ahead: 20},
		"the stall off": {setup: func(r *stallRig) { r.s.On = false }, since: 2 * time.Second, ahead: 20},
	} {
		r := newStallRig(t)
		if c.setup != nil {
			c.setup(r)
		}
		r.poll(c.since, c.ahead)
		if r.rubs() != 0 {
			t.Errorf("%s: rubbed", name)
		}
	}
	// Control: the same rig with none of those is a rub.
	r = newStallRig(t)
	r.poll(2*time.Second, 20)
	if r.rubs() != 1 {
		t.Fatalf("the control rig did not rub")
	}
}

// The jewellery is any Games necklace or Ring of dueling, any charge, never a note, and the rub names are
// exactly the sixteen a keep list and a hand-off must know.
func TestRubItemsAreAnyChargeAndNeverANoteRealConfig(t *testing.T) {
	r := newStallRig(t, "Big fishing net", "-Games necklace(8)")
	if RubItemHeld(r.d) {
		t.Fatal("a noted necklace counts as a rub item")
	}
	r.e.fill("Big fishing net", "Ring of dueling(3)")
	if !RubItemHeld(r.d) {
		t.Fatal("a Ring of dueling(3) is not a rub item")
	}
	names := RubNames()
	if len(names) != 16 || names[0] != "Games necklace(8)" || names[15] != "Ring of dueling(1)" {
		t.Fatalf("RubNames = %v", names)
	}
	for _, n := range names {
		if !fleetplan.IsRubItem(n) {
			t.Errorf("%q is not a rub item", n)
		}
		if ids := r.e.cfg.ObjIDs(n); len(ids) == 0 {
			t.Errorf("the config has no %q", n)
		}
	}
}

// The walk's end closes the menu its last rub left up and says what the rubs did, with the run energy
// the walk began and ended with; a walk with no rub says nothing and sends nothing.
func TestStallEndClosesTheMenuAndLogsTheWalkRealConfig(t *testing.T) {
	r := newStallRig(t)
	r.s.begin(r.d)
	r.poll(2*time.Second, 20)
	r.clk.advance(1900 * time.Millisecond)
	r.poll(2*time.Second, 20)
	r.e.st.Handle(serverprot.UPDATE_RUNENERGY, []byte{78})
	r.s.end(r.d)
	if r.e.sent[clientprot.CLOSE_MODAL] != 1 {
		t.Fatalf("%d CLOSE_MODAL after a walk with rubs, want 1", r.e.sent[clientprot.CLOSE_MODAL])
	}
	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "run staller: 2 rub(s) on that walk, run energy 80% to 78%") {
		t.Fatalf("log %q", r.logs)
	}
	r.s.end(r.d) // nothing more to close or say
	if r.e.sent[clientprot.CLOSE_MODAL] != 1 || len(r.logs) != 1 {
		t.Fatal("a second end closed a menu or logged again")
	}
	r.s.begin(r.d)
	r.s.end(r.d)
	if r.e.sent[clientprot.CLOSE_MODAL] != 1 || len(r.logs) != 1 {
		t.Fatal("a walk with no rub closed a menu or logged")
	}
}

// The drop cycle never starts inside a rub's delay: the engine refuses a click read while the player is
// delayed (OpHeldHandler), and the cycle's clicks would be thrown away.
func TestTheCycleWaitsOutARubsDelayRealConfig(t *testing.T) {
	r := newStallRig(t)
	rubbed := r.s.RubedRecently()
	if rubbed {
		t.Fatal("a stall that has not rubbed is recently rubbed")
	}
	r.poll(2*time.Second, 20)
	if !r.s.RubedRecently() {
		t.Fatal("not recently rubbed straight after a rub")
	}
	r.clk.advance(1700 * time.Millisecond)
	if !r.s.RubedRecently() {
		t.Fatal("not recently rubbed 1.7 s (under three ticks) after a rub")
	}
	r.clk.advance(200 * time.Millisecond)
	if r.s.RubedRecently() {
		t.Fatal("still recently rubbed past three ticks")
	}
	var off *Stall
	if off.RubedRecently() || off.KeepsName("Games necklace(8)") {
		t.Fatal("a nil stall rubbed or keeps something")
	}
}

// walkAside is the rub and the cycle, in that order, and nil when neither is on.
func TestWalkAsideCombinesTheStallAndTheCycle(t *testing.T) {
	d := &Deps{}
	if d.walkAside() != nil {
		t.Fatal("a Deps with neither has side work on a walk")
	}
	d.Stall = &Stall{On: false}
	if d.walkAside() != nil {
		t.Fatal("a stall that is off has side work on a walk")
	}
	d.Stall.On = true
	if d.walkAside() == nil {
		t.Fatal("a stall that is on has no side work on a walk")
	}
}

// ---- the bank ----

func simObjsFor(cfg *cachecfg.Config) actionsim.Objs {
	noteOf := func(id int32) int32 {
		o, ok := cfg.Obj(int(id))
		if !ok {
			return 0
		}
		for _, n := range cfg.ObjIDs(o.Name) {
			if no, ok := cfg.Obj(n); ok && no.Noted() {
				return int32(n)
			}
		}
		return 0
	}
	return actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
		NoteOf:    noteOf,
		UnnoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || !o.Noted() {
				return 0
			}
			return int32(o.CertLink)
		},
	}
}

func fastDelayForTests(time.Duration) bool { time.Sleep(time.Millisecond); return true }

func stacksCount(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// A bank deposit keeps the jewellery while the stall is on and banks it when it is off; the keep list the
// caller names is kept either way.
func TestDepositExceptKeepsTheRubItemWhileTheStallIsOnRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	for _, on := range []bool{true, false} {
		sim := actionsim.New(simObjsFor(cfg), []actionsim.Stack{
			{ID: id("Big fishing net"), Count: 1}, {ID: id("Games necklace(8)"), Count: 1}, {ID: id("Raw bass"), Count: 1}, {ID: id("Raw bass"), Count: 1},
		})
		sim.OpenBank(nil)
		d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Stall: &Stall{On: on}}
		DepositExcept(d, actions.NewBank(sim, d.Components), []string{"Big fishing net"})
		pack := sim.Pack()
		if stacksCount(pack, id("Big fishing net")) != 1 || stacksCount(pack, id("Raw bass")) != 0 {
			t.Fatalf("stall %v: pack %+v", on, pack)
		}
		if kept := stacksCount(pack, id("Games necklace(8)")) == 1; kept != on {
			t.Errorf("stall %v: the necklace was kept %v (pack %+v, bank %+v)", on, kept, pack, sim.Bank())
		}
	}
}

// An account that holds no jewellery draws one from its own bank, the best charge first; one that holds
// some, or whose bank holds none, draws nothing.
func TestDrawRubItemTakesTheBestChargeFromTheBankRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	rows := []actionsim.Stack{{ID: id("Ring of dueling(3)"), Count: 2}, {ID: id("Games necklace(5)"), Count: 4}, {ID: id("Raw bass"), Count: 9}}
	sim := actionsim.New(simObjsFor(cfg), []actionsim.Stack{{ID: id("Big fishing net"), Count: 1}})
	sim.OpenBank(rows)
	var logs []string
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Stall: &Stall{On: true}, Log: func(s string) { logs = append(logs, s) }}
	bank := actions.NewBank(sim, d.Components)
	if !DrawRubItem(d, bank) {
		t.Fatalf("nothing drawn: pack %+v bank %+v", sim.Pack(), sim.Bank())
	}
	if stacksCount(sim.Pack(), id("Games necklace(5)")) != 1 || stacksCount(sim.Pack(), id("Ring of dueling(3)")) != 0 || stacksCount(sim.Bank(), id("Games necklace(5)")) != 3 {
		t.Fatalf("pack %+v bank %+v: want one Games necklace(5) drawn, the necklace before the ring", sim.Pack(), sim.Bank())
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "drew a Games necklace(5) from the bank") {
		t.Fatalf("log %q", logs)
	}
	if DrawRubItem(d, bank) {
		t.Fatal("a second draw with the pack holding one")
	}
	d.Stall.On = false
	sim2 := actionsim.New(simObjsFor(cfg), nil)
	sim2.OpenBank(rows)
	d2 := &Deps{Session: sim2, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Stall: &Stall{On: false}}
	if DrawRubItem(d2, actions.NewBank(sim2, d2.Components)) {
		t.Fatal("drew with the stall off")
	}
	sim3 := actionsim.New(simObjsFor(cfg), nil)
	sim3.OpenBank([]actionsim.Stack{{ID: id("Raw bass"), Count: 9}})
	d3 := &Deps{Session: sim3, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests, Stall: &Stall{On: true}}
	if DrawRubItem(d3, actions.NewBank(sim3, d3.Components)) {
		t.Fatal("drew from a bank holding none")
	}
}

// ReadBank counts the jewellery so the hand-off knows whether the bank can already supply the stall.
func TestReadBankCountsTheRubItemsRealConfig(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int32 { return unnotedID(t, cfg, name, false) }
	sim := actionsim.New(simObjsFor(cfg), nil)
	sim.OpenBank([]actionsim.Stack{{ID: id("Games necklace(8)"), Count: 2}, {ID: id("Ring of dueling(1)"), Count: 1}, {ID: id("Raw bass"), Count: 9}})
	d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Delay: fastDelayForTests}
	if r := ReadBank(d, actions.NewBank(sim, d.Components)); r.Rub != 3 || r.Fish != 9 {
		t.Fatalf("ReadBank = %+v, want 3 rub items and 9 fish", r)
	}
}
