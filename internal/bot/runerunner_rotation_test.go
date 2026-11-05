package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// The worlds these tests rotate over: numbers no other test's shop board uses, since boards are
// per world and process-wide.
var rotTestWorlds = []interface{}{41.0, 42.0, 43.0}

type rotRig struct {
	b    *runeRunnerBot
	ctx  *Context
	sim  *actionsim.Session
	hops []int
	logs []string
}

func (r *rotRig) log() string { return strings.Join(r.logs, "\n") }

// newRotRig is a started rotating RuneRunner standing at (x, z) on world, at full hitpoints, whose
// world hops are recorded instead of made.
func newRotRig(t *testing.T, name string, world, x, z int, extra map[string]interface{}) *rotRig {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{Stackable: func(int32) bool { return true }, NoteOf: func(int32) int32 { return 0 }, UnnoteOf: func(int32) int32 { return 0 }}, nil)
	r := &rotRig{sim: sim}
	set := map[string]interface{}{"checkpointDir": t.TempDir(), "worlds": rotTestWorlds, "rotationPhase": 0.0}
	for k, v := range extra {
		set[k] = v
	}
	r.ctx = NewContext(sim, name, nil, nil, nil, actionsim.Components(), nil, nil, nil, set, func(l string) { r.logs = append(r.logs, l) })
	r.ctx.CurrentWorld = world
	r.ctx.HopWorld = func(w int, _ string) error { r.hops = append(r.hops, w); return nil }
	masterPut(t, sim.State(), x, z)
	fightGuardHP(sim.State(), 30, 30)
	r.b = &runeRunnerBot{said: map[string]time.Time{}}
	resolved, err := decodeRuneRunnerSettings(r.ctx.Settings)
	if err != nil {
		t.Fatal(err)
	}
	r.b.set = resolved
	if err := r.b.startState(r.ctx); err != nil {
		t.Fatal(err)
	}
	return r
}

// at puts the rig's round at the station with this key (the first, for a repeated hop key).
func (r *rotRig) at(t *testing.T, key string) {
	t.Helper()
	ring := runerunnerplan.BuildRotationRing(runerunnerplan.RotationInput{M: 41, Worlds: []int{41, 42, 43},
		Shops: r.b.set.ShopList, Phase: 0})
	i := runerunnerplan.Index(ring, key)
	if i < 0 {
		t.Fatalf("no %s in %s", key, runerunnerplan.PlanString(ring, 0))
	}
	r.b.st.StartRound(ring, i, time.Now())
	r.b.st.Rotation = true
	r.b.resumed = true
}

func releaseBoards(names ...string) {
	for _, w := range []int{41, 42, 43} {
		for _, n := range names {
			for _, s := range []fleetplan.RuneShop{runerunnerplan.Betty, fleetplan.Aubury, fleetplan.Lundail} {
				fleetrunner.ShopsFor(w).Release(s, n)
			}
		}
	}
}

// The live settings of the three world-3 runners carried homeWorld, which the strict decode
// refused, so they never started (3 Oct: "started" and then nothing). It loads now, and so does
// the rotation's own default.
func TestRuneRunnerLiveHomeWorldSettingsLoad(t *testing.T) {
	s, err := decodeRuneRunnerSettings(map[string]interface{}{
		"homeWorld": 3.0, "mule": "mule", "shops": []interface{}{"Betty"}, "zaffStaffs": 2.0,
		"runes":     []interface{}{"Mind rune", "Air rune", "Water rune", "Earth rune", "Fire rune", "Chaos rune", "Body rune"},
		"priceCaps": []interface{}{"Mind rune=18", "Air rune=24", "Water rune=24", "Earth rune=24", "Fire rune=24", "Chaos rune=90", "Body rune=30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Rotation || s.HomeWorld != 3 || len(s.RuneList) != 7 {
		t.Errorf("live settings: rotation %v home %d runes %v", s.Rotation, s.HomeWorld, s.RuneList)
	}
	s, err = decodeRuneRunnerSettings(map[string]interface{}{"rotate": "ON", "shops": "Betty", "swarmWhileWaiting": "false",
		"swarmSettings": `{"maxStage": 5, "homeWorld": 2}`})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Rotation || s.Rotate != "on" || s.SwarmOn() || s.SwarmSettings["maxStage"] != 5.0 || len(s.RuneList) != 8 {
		t.Errorf("rotate on: %+v", s)
	}
	for _, c := range []struct {
		raw  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"rotate": "maybe"}, "not one of"},
		{map[string]interface{}{"swarmWhileWaiting": "yes"}, "not true or false"},
		{map[string]interface{}{"swarmSettings": "[1]"}, "not a JSON object"},
		{map[string]interface{}{"swarmSettings": 5.0}, "not an object"},
		{map[string]interface{}{"swarmSettings": map[string]interface{}{"swarmSettings": map[string]interface{}{}}}, "of its own"},
		{map[string]interface{}{"maxHopsPerHour": 31.0}, "maxHopsPerHour"},
	} {
		if _, err := decodeRuneRunnerSettings(c.raw); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.raw, err, c.want)
		}
	}
}

// A rotation hop to a full shelf holds it on that world's board and hops; the cursor moves past
// the hop and the hop is counted for the budget.
func TestRotationHopHoldsAndHops(t *testing.T) {
	r := newRotRig(t, "rotbuyer1", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, nil)
	defer releaseBoards("rotbuyer1")
	r.at(t, "hop42")
	cursor := r.b.st.Cursor
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[cursor], fleetrunner.Here(r.b.deps(r.ctx)))
	if len(r.hops) != 1 || r.hops[0] != 42 {
		t.Fatalf("hops %v\n%s", r.hops, r.log())
	}
	if r.b.st.Cursor != cursor+1 || len(r.b.st.HopTimes) != 1 || r.b.st.HopPending != 42 {
		t.Errorf("cursor %d->%d hop times %v pending %d", cursor, r.b.st.Cursor, r.b.st.HopTimes, r.b.st.HopPending)
	}
	if held := fleetrunner.ShopsFor(42).Held(runerunnerplan.Betty, "Mind rune", "rotbuyer1", time.Now()); held < 200 {
		t.Errorf("Betty@42's minds held %d before the hop", held)
	}
}

// Two runners are not at the same shop on the same world: a shelf another runner holds is not
// worth the hop, and the circuit skips to the next pair.
func TestRotationHopSkipsAPairAnotherRunnerHolds(t *testing.T) {
	r := newRotRig(t, "rotbuyer2", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, nil)
	defer releaseBoards("rotbuyer2", "rotother")
	now := time.Now()
	board := fleetrunner.ShopsFor(42)
	for _, l := range runerunnerplan.Betty.Lines {
		board.Reserve(runerunnerplan.Betty, l.Rune, "rotother", l.Base, now, time.Hour, 600*time.Millisecond)
	}
	r.at(t, "hop42")
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[r.b.st.Cursor], fleetrunner.Here(r.b.deps(r.ctx)))
	if len(r.hops) != 0 {
		t.Fatalf("hopped %v onto a held shelf", r.hops)
	}
	if s, _ := r.b.st.Current(); s.Key() != "hop43" {
		t.Errorf("after the skip the cursor is at %s, want hop43\n%s", s.Key(), r.log())
	}
	if !strings.Contains(r.log(), "under hopMinGain") {
		t.Errorf("no skip reason in\n%s", r.log())
	}
}

// The hop budget: a twelfth hop inside the hour that would have to wait more than five minutes
// skips its pair; the hop on the world the runner is already on is no hop at all.
func TestRotationHopBudgetAndHere(t *testing.T) {
	r := newRotRig(t, "rotbuyer3", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, nil)
	defer releaseBoards("rotbuyer3")
	r.b.set.MaxHopsPerHour = 12 // a set budget (the default is now the ceiling the two-minute gap allows)
	now := time.Now()
	for i := 0; i < 12; i++ {
		r.b.st.HopTimes = append(r.b.st.HopTimes, now.Add(-time.Duration(40-i*3)*time.Minute))
	}
	r.at(t, "hop42")
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[r.b.st.Cursor], fleetrunner.Here(r.b.deps(r.ctx)))
	if len(r.hops) != 0 || !strings.Contains(r.log(), "hop budget") {
		t.Fatalf("hops %v\n%s", r.hops, r.log())
	}
	r.at(t, "hop41")
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[r.b.st.Cursor], fleetrunner.Here(r.b.deps(r.ctx)))
	if len(r.hops) != 0 || r.b.st.Done["hop41"].Outcome != runerunnerplan.OutcomeHere {
		t.Errorf("hop to its own world: %v %+v", r.hops, r.b.st.Done["hop41"])
	}
	if s, _ := r.b.st.Current(); s.Key() != "Betty@41" {
		t.Errorf("after the here-hop: %s", s.Key())
	}
}

// Live 3 Oct: a Lundail-only runner whose Lundail was stopped by lundailMaxDeaths re-planned every
// minute (1,533 rounds) because the stopped shelf's regrowth timed the wait. A gated pair no longer
// times it.
func TestRuneRunnerIdleIgnoresAGatedLundail(t *testing.T) {
	r := newRotRig(t, "rotbuyer4", 41, 2809, 3441, map[string]interface{}{"shops": []interface{}{"Lundail"}, "worlds": []interface{}{}})
	if r.b.set.Rotation {
		t.Fatal("one shop and rotate auto is the classic ring")
	}
	r.b.st.StartRound(runerunnerplan.BuildRing(runerunnerplan.RingInput{M: 44, Shops: r.b.set.ShopList}), 0, time.Now())
	now := time.Now()
	// An open Lundail nobody has read is estimated full: the one-minute floor (what the live
	// runner saw, rightly, while Lundail was open).
	if w := r.b.idleWait(r.ctx); w != time.Minute {
		t.Fatalf("an open Lundail times the wait at the floor, got %s", w)
	}
	r.b.st.LundailDeaths = []time.Time{now.Add(-time.Hour), now.Add(-2 * time.Hour)}
	if w := r.b.idleWait(r.ctx); w != 20*time.Minute {
		t.Errorf("Lundail stopped by deaths: wait %s, want idleMaxMinutes", w)
	}
}

// runeFakeSwarm stands in for ScarabSwarm: it records what it was started with and how often it
// ran, and reports the safe-point reading the test gives it.
type runeFakeSwarm struct {
	started, loops, stopped int
	settings                map[string]interface{}
	in                      mastercraftplan.SafeInput
}

func (f *runeFakeSwarm) OnStart(ctx *Context) error {
	f.started++
	f.settings = ctx.Settings
	return nil
}
func (f *runeFakeSwarm) Loop(*Context) error { f.loops++; return nil }
func (f *runeFakeSwarm) OnStop(*Context)     { f.stopped++ }
func (f *runeFakeSwarm) safeInput(*Context) mastercraftplan.SafeInput {
	return f.in
}

// The switch: a circuit that ends with the next one under swarmResumeRunes hands over first (a
// delivery-only round, every rune), then runs ScarabSwarm on its own settings; the restock check
// sends it back at a safe point only, and ctx gets the runner's settings back.
func TestRuneRunnerSwarmBetweenCircuits(t *testing.T) {
	r := newRotRig(t, "rotbuyer5", 41, 2809, 3441, map[string]interface{}{
		"swarmSettings": map[string]interface{}{"maxStage": 5.0}, "swarmResumeRunes": 1000.0, "swarmCheckMinutes": 5.0,
		"swarmReadyPercent": 0.0, // this test is of the swarmResumeRunes rule alone; the share has its own test below
	})
	fake := &runeFakeSwarm{in: mastercraftplan.SafeInput{TileKnown: true, InCombat: true, HP: 30, MaxHP: 30}}
	r.b.newSwarm = func() masterSwarm { return fake }
	worth := 300
	r.b.circuitWorthSeam = func(*Context) int { return worth }
	runnerSettings := r.ctx.Settings

	// A rotation round ends: the next circuit expects 300 runes, under 1000.
	r.at(t, "hop41")
	if !r.b.afterRoundSwarm(r.ctx) || !r.b.st.SwarmDue || runerunnerplan.PlanString(r.b.st.Ring, 0) != "[deliver@41]" {
		t.Fatalf("no hand-over round: due %v ring %s\n%s", r.b.st.SwarmDue, runerunnerplan.PlanString(r.b.st.Ring, 0), r.log())
	}
	if v := r.b.deliverVerdict(r.ctx, 5, fleetrunner.Here(r.b.deps(r.ctx))); v.Action == runerunnerplan.DeliverSkip && strings.Contains(v.Reason, "deliverMinRunes") {
		t.Errorf("the hand-over before ScarabSwarm must take even 5 runes: %+v", v)
	}
	// The delivery-only round ends: ScarabSwarm.
	if !r.b.afterRoundSwarm(r.ctx) || !r.b.st.Swarm {
		t.Fatalf("not switched\n%s", r.log())
	}
	if err := r.b.Loop(r.ctx); err != nil {
		t.Fatal(err)
	}
	if fake.started != 1 || fake.loops != 1 || fake.settings["maxStage"] != 5.0 || fake.settings["mule"] != "mule" {
		t.Fatalf("swarm started %d loops %d settings %v", fake.started, fake.loops, fake.settings)
	}
	// Not due yet; then due with the circuit still poor: stays.
	r.b.st.SwarmChecked = time.Now().Add(-6 * time.Minute)
	_ = r.b.Loop(r.ctx)
	if r.b.swarmBack != "" || fake.loops != 2 || !strings.Contains(r.log(), "staying on ScarabSwarm") {
		t.Fatalf("back %q loops %d\n%s", r.b.swarmBack, fake.loops, r.log())
	}
	// The shelves regrow: due back, but ScarabSwarm is in a fight -- it carries on.
	worth = 1200
	r.b.st.SwarmChecked = time.Now().Add(-6 * time.Minute)
	_ = r.b.Loop(r.ctx)
	if r.b.swarmBack == "" || !r.b.st.Swarm || fake.stopped != 0 || fake.loops != 3 {
		t.Fatalf("back %q swarm %v stopped %d loops %d", r.b.swarmBack, r.b.st.Swarm, fake.stopped, fake.loops)
	}
	// Out of the fight at the Catherby post: stopped, settings back, a rotation round from here.
	fake.in = mastercraftplan.SafeInput{TileKnown: true, HP: 30, MaxHP: 30}
	_ = r.b.Loop(r.ctx)
	if r.b.st.Swarm || fake.stopped != 1 || len(r.b.st.Ring) == 0 || !r.b.st.Rotation {
		t.Fatalf("swarm %v stopped %d ring %v\n%s", r.b.st.Swarm, fake.stopped, r.b.st.Ring, r.log())
	}
	if r.ctx.Settings["checkpointDir"] != runnerSettings["checkpointDir"] || r.ctx.Settings["maxStage"] != nil {
		t.Errorf("ctx kept ScarabSwarm's settings: %v", r.ctx.Settings)
	}
	// A circuit worth the threshold does not switch.
	r.at(t, "hop41")
	if r.b.afterRoundSwarm(r.ctx) {
		t.Error("switched with the circuit worth 1200")
	}
}

// The circuit is walked only once it would buy swarmReadyPercent of a full-shelf circuit: 14,000 of 30,000 hands over and logs how long
// the restock takes; 18,000 stays on the circuit.
func TestRuneRunnerSwarmWaitsForTheShelvesToRegrow(t *testing.T) {
	r := newRotRig(t, "rotbuyer7", 41, 2809, 3441, map[string]interface{}{"swarmSettings": map[string]interface{}{"maxStage": 5.0}})
	r.b.newSwarm = func() masterSwarm { return &runeFakeSwarm{in: mastercraftplan.SafeInput{TileKnown: true}} }
	worth := 14000
	r.b.circuitWorthSeam = func(*Context) int { return worth }
	r.b.circuitWorthAtSeam = func(_ *Context, after time.Duration) int {
		if after >= fullShelfHorizon {
			return 30000
		}
		return min(30000, 10000+int(after.Minutes())*200) // regrowing 200 runes a minute from 10,000
	}
	r.at(t, "hop41")
	if !r.b.afterRoundSwarm(r.ctx) || !r.b.st.SwarmDue {
		t.Fatalf("14,000 of 30,000 should hand over\n%s", r.log())
	}
	if !strings.Contains(r.log(), "restock: circuit worth 14000 of 30000 at full shelves (target 18000): ready in about 40m0s") {
		t.Errorf("no restock line\n%s", r.log())
	}
	r.b.st.SwarmDue = false
	worth = 18000
	r.at(t, "hop41")
	if r.b.afterRoundSwarm(r.ctx) {
		t.Errorf("18,000 is the target: stay on the circuit")
	}
}

// The phase survives a restart (a ScarabSwarm hop), and the place rule takes the east side.
func TestRuneRunnerSwarmPhaseSurvivesARestart(t *testing.T) {
	r := newRotRig(t, "rotbuyer6", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, nil)
	r.b.enterSwarm(r.ctx)
	dir := r.ctx.Settings["checkpointDir"]
	var lines []string
	ctx := NewContext(r.sim, "rotbuyer6", nil, nil, nil, actionsim.Components(), nil, nil, nil,
		map[string]interface{}{"checkpointDir": dir, "worlds": rotTestWorlds}, func(l string) { lines = append(lines, l) })
	b := &runeRunnerBot{said: map[string]time.Time{}}
	b.set, _ = decodeRuneRunnerSettings(ctx.Settings)
	if err := b.startState(ctx); err != nil {
		t.Fatal(err)
	}
	if !b.st.Swarm || !strings.Contains(strings.Join(lines, "\n"), "ScarabSwarm phase") {
		t.Errorf("phase lost: %v %q", b.st.Swarm, lines)
	}
	if r.b.farFromCircuit(r.ctx) {
		t.Error("Port Sarim is on the circuit's side")
	}
	masterPut(t, r.sim.State(), 3100, 3700) // the Wilderness
	if !r.b.farFromCircuit(r.ctx) {
		t.Error("the Wilderness is far from the circuit")
	}
}
