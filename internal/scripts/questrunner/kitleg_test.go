package questrunner

// The legs that fetch a quest's kit (Imp Catcher's bank walk above all: 5 of its 6 deaths, live 4-8 Oct 2026) and the bites
// every quest walk takes. See holdRetaliateOff, EatFloor and questplan.FloorEatPercent for what the logs showed.

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// controlsSession is a session whose controls tab works: the auto-retaliate buttons set the option_nodef varp as
// player_controls.rs2 does (com_2 on is 0, com_3 off is 1). It records the buttons it was sent.
type controlsSession struct {
	st   *gamestate.State
	sent []int
}

func newControlsSession() *controlsSession { return &controlsSession{st: gamestate.New()} }

func (c *controlsSession) State() *gamestate.State     { return c.st }
func (c *controlsSession) Cheats() *anticheat.Counters { return nil }

func (c *controlsSession) Send(build func(w *packet.Packet)) error {
	p := packet.NewWriter(16)
	build(p)
	b := p.Data[:p.Pos]
	if len(b) == 3 && int(b[0]) == clientprot.IF_BUTTON {
		com := int(b[1])<<8 | int(b[2])
		c.sent = append(c.sent, com)
		switch com {
		case actions.ControlsRetaliateOn:
			c.setRetaliateVarp(0)
		case actions.ControlsRetaliateOff:
			c.setRetaliateVarp(1)
		}
	}
	return nil
}

func (c *controlsSession) setRetaliateVarp(v int) {
	p := packet.NewWriter(4)
	p.P2(uint16(gamestate.VarpAutoRetaliate))
	p.P1(byte(v))
	c.st.Handle(serverprot.VARP_SMALL, p.Data[:p.Pos])
}

// clickWatch is the flat-world walker client that notes whether auto-retaliate was on as each movement click went out.
type clickWatch struct {
	*stepClient
	st               *gamestate.State
	retaliateAtClick []bool
}

func (c *clickWatch) Walk(path []webwalk.Point, run bool) error {
	c.retaliateAtClick = append(c.retaliateAtClick, c.st.AutoRetaliateOn())
	return c.stepClient.Walk(path, run)
}

func kitLegRig(sess *controlsSession) (*Deps, *clickWatch, *[]string) {
	cl := &clickWatch{stepClient: &stepClient{me: webwalk.Point{X: 2, Z: 3}, now: time.Unix(1000, 0)}, st: sess.st}
	w := webwalk.NewWalker(webwalk.NewPlanner(flatCollision{0, 90, 0, 6}, &webwalk.Graph{}), cl)
	var logs []string
	d := &Deps{Session: sess, Delay: func(time.Duration) bool { return true }, Log: func(s string) { logs = append(logs, s) }, Walker: w}
	return d, cl, &logs
}

// A leg that fetches a kit (TravelCautious) sends every one of its clicks with auto-retaliate off and puts it back as it ends: a
// hit while it was on would drop the walk the account was following (auto_retaliate.rs2 p_opnpc(2)), and the live Imp Catcher
// accounts stood within a tile or two of the warrior women for 80 and 120 seconds. A plain Travel leaves the option alone, and so
// does a leg that finds it off already, or a quest that keeps it off (Module.NoRetaliate).
func TestCautiousLegHoldsAutoRetaliateOffForEveryClickAndPutsItBack(t *testing.T) {
	dest := tile.Tile{X: 80, Z: 3}

	sess := newControlsSession() // varp 172 untransmitted: auto-retaliate on
	d, cl, logs := kitLegRig(sess)
	if !sess.st.AutoRetaliateOn() {
		t.Fatal("fixture: auto-retaliate should start on")
	}
	if !TravelCautious(d, dest, 2) {
		t.Fatal("the kit leg did not arrive")
	}
	if len(cl.retaliateAtClick) == 0 {
		t.Fatal("the leg sent no movement click")
	}
	for i, on := range cl.retaliateAtClick {
		if on {
			t.Errorf("click %d of the kit leg went out with auto-retaliate on", i)
		}
	}
	if want := []int{actions.ControlsRetaliateOff, actions.ControlsRetaliateOn}; !slices.Equal(sess.sent, want) {
		t.Errorf("buttons sent %v, want off then on %v", sess.sent, want)
	}
	if !sess.st.AutoRetaliateOn() {
		t.Error("auto-retaliate was not put back on after the leg")
	}
	if !slices.ContainsFunc(*logs, func(s string) bool { return strings.HasPrefix(s, "kit leg:") }) {
		t.Errorf("no \"kit leg:\" log line; logs %q", *logs)
	}

	// A plain leg (a quest's own steps, its fights) touches nothing.
	sess = newControlsSession()
	d, cl, _ = kitLegRig(sess)
	if !Travel(d, dest, 2) {
		t.Fatal("the plain leg did not arrive")
	}
	if len(sess.sent) != 0 || slices.Contains(cl.retaliateAtClick, false) {
		t.Errorf("a plain leg sent %v and clicked with retaliate %v, want neither", sess.sent, cl.retaliateAtClick)
	}

	// Off already (an account that trains with it off): not this leg's to switch, so not its to put back.
	sess = newControlsSession()
	sess.setRetaliateVarp(1)
	d, _, _ = kitLegRig(sess)
	if !TravelCautious(d, dest, 2) {
		t.Fatal("the kit leg did not arrive")
	}
	if len(sess.sent) != 0 || sess.st.AutoRetaliateOn() {
		t.Errorf("with it off already the leg sent %v and left it on=%v", sess.sent, sess.st.AutoRetaliateOn())
	}

	// A quest that keeps it off for the whole run (Module.NoRetaliate sets Deps.noRetaliate) is left alone too.
	sess = newControlsSession()
	d, _, _ = kitLegRig(sess)
	d.noRetaliate = true
	if !TravelCautious(d, dest, 2) {
		t.Fatal("the kit leg did not arrive")
	}
	if len(sess.sent) != 0 {
		t.Errorf("a NoRetaliate quest's kit leg sent %v", sess.sent)
	}
}

// Every Travel leg marks the account as walking for the engine's eat loop (the floor bite is taken only then), whichever of the
// walking entry points it is, and the mark is gone when the leg returns, arrived or not.
func TestTravelMarksTheAccountAsWalkingForTheEatLoop(t *testing.T) {
	d, _, _ := kitLegRig(newControlsSession())
	var during []int32
	d.Walker.Prepare = func(*webwalk.WalkOptions) func() {
		during = append(during, d.walking.Load())
		return nil
	}
	if !Travel(d, tile.Tile{X: 60, Z: 3}, 1) || !TravelCautious(d, tile.Tile{X: 70, Z: 3}, 1) ||
		!TravelAvoiding(d, tile.Tile{X: 80, Z: 3}, 1, []webwalk.Rect{{MinX: 40, MaxX: 41, MinZ: 5, MaxZ: 6}}) {
		t.Fatal("a leg did not arrive")
	}
	if len(during) != 3 || during[0] != 1 || during[1] != 1 || during[2] != 1 {
		t.Errorf("walking was %v while the three legs ran, want 1 each", during)
	}
	if n := d.walking.Load(); n != 0 {
		t.Errorf("walking is %d after the legs, want 0", n)
	}
}

// moduleTools protects a module's tools by id and by name substring (the matching Engine.keepFunc uses), and only those.
func TestModuleToolsProtectOnlyTheModulesOwnItems(t *testing.T) {
	cfg := stallConfig(t)
	d := &Deps{Config: cfg}
	bread, shark, logs := stallObj(t, cfg, "Bread"), stallObj(t, cfg, "Shark"), stallObj(t, cfg, "Logs")
	tool := moduleTools(d, questplan.Module{ToolIDs: []int{int(shark)}, Tools: []string{"bread"}})
	if !tool(int(shark)) || !tool(int(bread)) || tool(int(logs)) {
		t.Errorf("tool(shark) %v, tool(bread) %v, tool(logs) %v, want true true false", tool(int(shark)), tool(int(bread)), tool(int(logs)))
	}
	if none := moduleTools(d, questplan.Module{}); none(int(bread)) {
		t.Error("a module with no tools protects bread")
	}
}

// ---- the floor bite ----

// floorRig is an account at hp of maxHP hitpoints carrying the given stacks, whose Eat clicks work: the item goes and the
// hitpoints rise by the food's heal.
type floorRig struct {
	d    *Deps
	sim  *actionsim.Session
	mu   sync.Mutex
	hp   int
	max  int
	heal map[int32]int
}

func (r *floorRig) setHP(hp int) {
	r.mu.Lock()
	r.hp = hp
	r.mu.Unlock()
	p := packet.NewWriter(8)
	p.P1(byte(gamestate.HitpointsStat))
	p.P4(int32(levels.XPAtLevel(r.max)))
	p.P1(byte(hp))
	r.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

func newFloorRig(t *testing.T, hp, maxHP int, pack []actionsim.Stack, heal map[int32]int) *floorRig {
	t.Helper()
	cfg := stallConfig(t)
	sim := actionsim.New(actionsim.Objs{}, pack)
	sim.ShowTabs()
	r := &floorRig{sim: sim, max: maxHP, heal: heal}
	r.d = &Deps{Session: sim, Config: cfg, Components: actionsim.TabComponents(), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }, floorGap: time.Millisecond}
	r.setHP(maxHP) // the high-water mark is the maximum
	r.setHP(hp)
	sim.OnSend = func(b []byte) {
		if len(b) < 7 || int(b[0]) != clientprot.OPHELD1 {
			return
		}
		id := int32(int(b[1])<<8 | int(b[2]))
		if sim.Remove(id, 1) != 1 {
			return
		}
		r.mu.Lock()
		next := min(r.hp+r.heal[id], r.max)
		r.mu.Unlock()
		r.setHP(next)
	}
	return r
}

func (r *floorRig) packCount(id int32) int {
	n := 0
	for _, s := range r.sim.Pack() {
		if s.ID == id {
			n += s.Count
		}
	}
	return n
}

// The live deaths: 1 to 3 of 34 hitpoints with 15 food in the pack and nothing eating. EatFloor eats whatever food the pack
// holds (the biggest heal first) at or under half the hitpoints, leaves a quest's tools alone, eats nothing above half and
// nothing that is not food.
func TestEatFloorEatsAnyCarriedFoodAtOrUnderHalfButNoQuestTool(t *testing.T) {
	cfg := stallConfig(t)
	bread, shark, logs := stallObj(t, cfg, "Bread"), stallObj(t, cfg, "Shark"), stallObj(t, cfg, "Logs")
	heal := map[int32]int{bread: 4, shark: 20}

	r := newFloorRig(t, 9, 34, []actionsim.Stack{{ID: bread, Count: 15}, {ID: logs, Count: 3}, {ID: shark, Count: 2}}, heal)
	if !EatFloor(r.d) {
		t.Fatal("no bite at 9 of 34 hitpoints with 15 bread and 2 sharks in the pack")
	}
	if r.packCount(shark) != 1 || r.packCount(bread) != 15 || r.packCount(logs) != 3 {
		t.Errorf("after the bite: sharks %d, bread %d, logs %d, want 1, 15, 3 (the biggest heal goes first)", r.packCount(shark), r.packCount(bread), r.packCount(logs))
	}
	if hp, _ := Hitpoints(r.d); hp != 29 {
		t.Errorf("hitpoints %d after the shark, want 29", hp)
	}
	if EatFloor(r.d) {
		t.Error("a bite at 29 of 34 hitpoints, above half")
	}
	if r.d.eaten.Load() != 1 {
		t.Errorf("the account's bite count is %d, want 1", r.d.eaten.Load())
	}

	// A quest tool is the quest's: Merlin's bread, Death Plateau's trout.
	r = newFloorRig(t, 5, 34, []actionsim.Stack{{ID: bread, Count: 15}}, heal)
	tool := func(id int) bool { return id == int(bread) }
	r.d.toolProtect.Store(&tool)
	if EatFloor(r.d) || r.packCount(bread) != 15 {
		t.Errorf("the floor bite ate the quest's bread (left %d)", r.packCount(bread))
	}

	// Nothing edible: nothing happens, and no click is sent.
	r = newFloorRig(t, 5, 34, []actionsim.Stack{{ID: logs, Count: 3}}, heal)
	sent := len(r.sim.Sent())
	if EatFloor(r.d) || len(r.sim.Sent()) != sent {
		t.Error("a bite with only logs in the pack")
	}

	// Two bites are two eat delays apart.
	r = newFloorRig(t, 5, 34, []actionsim.Stack{{ID: bread, Count: 15}}, heal)
	r.d.floorGap = time.Hour
	if !EatFloor(r.d) {
		t.Fatal("the first bite")
	}
	r.setHP(6)
	if EatFloor(r.d) {
		t.Error("a second bite inside the eat gap")
	}
}

// The eat loop of a quest that names no food at all (Imp Catcher), or other food than the pack carries, takes floor bites while
// a leg is under way and none while the account is standing at a step: the module's own sustain rules stay in charge there.
func TestEatLoopTakesFloorBitesOnlyWhileALegIsUnderWay(t *testing.T) {
	cfg := stallConfig(t)
	bread, shark := stallObj(t, cfg, "Bread"), stallObj(t, cfg, "Shark")
	heal := map[int32]int{bread: 4, shark: 20}

	for _, c := range []struct {
		name string
		sus  questplan.Sustain
	}{
		{"a module with no Sustain", questplan.Sustain{}},
		{"a Sustain that names other food (sharks, not the bread carried)", questplan.Sustain{FoodIDs: []int{int(shark)}, EatAt: 10}},
	} {
		r := newFloorRig(t, 9, 34, []actionsim.Stack{{ID: bread, Count: 15}}, heal)
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { eatLoop(r.d, c.sus, stop); close(done) }()

		time.Sleep(1500 * time.Millisecond) // two ticks of the loop, no leg under way
		if r.packCount(bread) != 15 {
			t.Errorf("%s: a floor bite with no leg under way", c.name)
		}
		end := r.d.beginWalk()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if hp, _ := Hitpoints(r.d); hp*100 > 34*questplan.FloorEatPercent {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		end()
		close(stop)
		<-done
		if hp, _ := Hitpoints(r.d); hp*100 <= 34*questplan.FloorEatPercent {
			t.Errorf("%s: still at %d of 34 hitpoints with %d bread after a leg's worth of ticks", c.name, hp, r.packCount(bread))
		}
	}
}
