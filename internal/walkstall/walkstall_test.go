package walkstall

import (
	"os"
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/webwalk"
)

func realConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func necklaceID(t *testing.T, cfg *cachecfg.Config) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs("Games necklace(8)") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			return int32(id)
		}
	}
	t.Fatal("no Games necklace(8) in this config")
	return 0
}

func runOn(s *actionsim.Session) {
	w := packet.NewWriter(8)
	w.P2(uint16(gamestate.VarpRun))
	w.P1(1)
	s.State().Handle(serverprot.VARP_SMALL, w.Data[:w.Pos])
}

func TestRubNamesAndKeeps(t *testing.T) {
	names := RubNames()
	if len(names) != 16 || names[0] != "Games necklace(8)" || names[15] != "Ring of dueling(1)" {
		t.Fatalf("RubNames = %v", names)
	}
	on, off := New(true), New(false)
	if !on.KeepsName("Games necklace(3)") || !on.KeepsName("Ring of dueling(8)") || on.KeepsName("Logs") {
		t.Fatal("an on stall must keep exactly the rub items")
	}
	if off.KeepsName("Games necklace(3)") || off.KeepNames() != nil {
		t.Fatal("an off stall kept something")
	}
	var nilStall *Stall
	if nilStall.KeepsName("Games necklace(3)") || nilStall.Active() || nilStall.RubedRecently() {
		t.Fatal("a nil stall is active")
	}
}

func TestPrepareNeedsAnItemAndAnOnStall(t *testing.T) {
	cfg := realConfig(t)
	sim := actionsim.New(actionsim.Objs{}, nil)
	env := &Env{Session: sim, Config: cfg}
	if New(false).Prepare(env) != nil {
		t.Fatal("an off stall prepared a walk")
	}
	prep := New(true).Prepare(env)
	if prep == nil {
		t.Fatal("an on stall has no Prepare")
	}
	var o webwalk.WalkOptions
	if done := prep(&o); done != nil || o.Aside != nil {
		t.Fatal("a walk with no rub item in the pack was given an aside")
	}

	sim = actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: necklaceID(t, cfg), Count: 1}})
	env = &Env{Session: sim, Config: cfg}
	prep = New(true).Prepare(env)
	o = webwalk.WalkOptions{}
	done := prep(&o)
	if o.Aside == nil || done == nil || o.ClickSteps == 0 {
		t.Fatalf("a walk with a necklace got no aside (aside %v, done %v, clicks %d)", o.Aside != nil, done != nil, o.ClickSteps)
	}
	// A caller's own aside is left to it.
	own := func(webwalk.AsidePoll) {}
	o = webwalk.WalkOptions{Aside: own}
	if prep(&o) != nil {
		t.Fatal("a walk with its own aside was taken over")
	}
}

func TestRubWhenRunningClearOfTheClick(t *testing.T) {
	cfg := realConfig(t)
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: necklaceID(t, cfg), Count: 1}})
	runOn(sim)
	env := &Env{Session: sim, Config: cfg, Label: "test"}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	s := New(true)
	s.Now = func() time.Time { return now }
	far := webwalk.AsidePoll{Me: webwalk.Point{X: 2700, Z: 3400}, Target: webwalk.Point{X: 2740, Z: 3400}, SinceClick: 3 * time.Second}

	before := len(sim.Sent())
	s.poll(env, webwalk.AsidePoll{Me: far.Me, Target: far.Target, SinceClick: 100 * time.Millisecond})
	if len(sim.Sent()) != before {
		t.Fatal("rubbed inside the walk's own click tick")
	}
	s.Busy = func() bool { return true }
	s.poll(env, far)
	if len(sim.Sent()) != before {
		t.Fatal("rubbed while another aside was busy")
	}
	s.Busy = nil
	s.poll(env, far)
	if len(sim.Sent()) != before+1 || s.Rubs != 1 || !s.RubedRecently() {
		t.Fatalf("no rub on a clear poll (sent %d, rubs %d)", len(sim.Sent())-before, s.Rubs)
	}
	s.poll(env, far)
	if len(sim.Sent()) != before+1 {
		t.Fatal("rubbed twice inside the rub's own delay")
	}
	now = now.Add(2 * time.Second)
	s.poll(env, far)
	if len(sim.Sent()) != before+2 {
		t.Fatal("no second rub three ticks on")
	}
}

func TestComposeRunsEveryAsideInOrderAndTheDonesBackwards(t *testing.T) {
	var order []string
	mk := func(name string, clicks int, aside bool) func(o *webwalk.WalkOptions) func() {
		return func(o *webwalk.WalkOptions) func() {
			if o.Aside != nil {
				t.Fatalf("%s saw an aside already set", name)
			}
			if aside {
				o.Aside = func(webwalk.AsidePoll) { order = append(order, name) }
			}
			if clicks > o.ClickSteps {
				o.ClickSteps = clicks
			}
			return func() { order = append(order, name+" done") }
		}
	}
	prep := Compose(nil, mk("a", 100, true), mk("b", 0, false), mk("c", 60, true))
	var o webwalk.WalkOptions
	done := prep(&o)
	o.Aside(webwalk.AsidePoll{})
	done()
	want := []string{"a", "c", "c done", "b done", "a done"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if o.ClickSteps != 100 {
		t.Fatalf("ClickSteps = %d, want the largest, 100", o.ClickSteps)
	}

	own := func(webwalk.AsidePoll) {}
	o = webwalk.WalkOptions{Aside: own}
	if prep(&o) != nil {
		t.Fatal("Compose ran its hooks on a walk that brought its own aside")
	}
	if Compose() != nil || Compose(nil, nil) != nil {
		t.Fatal("an empty Compose is not nil")
	}
	single := mk("x", 0, true)
	if Compose(nil, single) == nil {
		t.Fatal("a single hook was lost")
	}
}

func TestWantRun(t *testing.T) {
	for _, c := range []struct {
		on     bool
		energy int
		known  bool
		want   bool
	}{
		{false, 100, true, true},
		{false, RunEnergyThreshold, true, true},
		{false, RunEnergyThreshold - 1, true, false},
		{false, 100, false, false}, // not observed yet is unknown, not plenty
		{true, 100, true, false},   // already on
	} {
		if got := WantRun(c.on, c.energy, c.known); got != c.want {
			t.Errorf("WantRun(%v, %d, %v) = %v, want %v", c.on, c.energy, c.known, got, c.want)
		}
	}
}

func TestEnsureRunTurnsTheToggleOn(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	env := &Env{Session: sim}
	before := len(sim.Sent())
	EnsureRun(env) // no energy observed yet
	if len(sim.Sent()) != before {
		t.Fatal("turned run on with no energy reading")
	}
	w := packet.NewWriter(4)
	w.P1(80)
	sim.State().Handle(serverprot.UPDATE_RUNENERGY, w.Data[:w.Pos])
	EnsureRun(env)
	if len(sim.Sent()) != before+1 {
		t.Fatalf("sent %d packets, want the run toggle", len(sim.Sent())-before)
	}
	runOn(sim)
	EnsureRun(env)
	if len(sim.Sent()) != before+1 {
		t.Fatal("toggled run again with it already on")
	}
}
