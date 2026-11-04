package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// resumeLoopRig is a MasterCrafter's Loop over the chest rig's bank-and-pack simulator, with the REAL
// safe-point place rule (the other rigs answer it by a seam, which is how a rule that stopped every
// restart on the Falador road went unseen): decisions are withChest's over a snapshot read from the
// bus the way decideLive reads it, the round is a stub that records itself and ends the chain, and
// the swarm is the fake. The mule's heartbeat is on the bus and the account is on its post's side
// of the mountain until a test moves it.
func resumeLoopRig(t *testing.T, pack []actionsim.Stack) (*masterRig, *cachecfg.Config) {
	t.Helper()
	stock := map[string]int{"Uncut sapphire": 900, "Chisel": 1, mastercraftplan.ChestKeyItem: 400}
	r, cfg := chestRig(t, nil, nil, stock)
	r.freshSession(cfg, pack) // as a restarted script finds it: the pack as it was, no bank read
	r.safe = mastercraftplan.SafeInput{TileKnown: true, HP: 40, MaxHP: 40}
	r.b.newSwarm = func() masterSwarm { return &fakeMasterSwarm{rig: r} }
	r.b.decide = func(ctx *Context) mastercraftplan.Decision {
		r.checks++
		p := scarabMulePresenceOf(ctx, r.b.mule)
		return r.b.withChest(ctx, mastercraftplan.Snapshot{
			Now: time.Now(), MuleHeard: p.Heard, MuleSameWorld: p.SameWorld, CraftingLevel: 30,
			Stock:  func(item string) (int, bool) { return stock[item], true },
			Ledger: &r.b.ledger, Denied: r.b.denied,
		})
	}
	r.b.round = func(_ *Context, dec mastercraftplan.Decision) masterRoundOutcome {
		r.events = append(r.events, "round:"+string(dec.Mode))
		r.rounds = append(r.rounds, dec.Mode)
		return masterRoundOutcome{Mode: dec.Mode, Transient: true} // a passing failure ends the chain
	}
	if err := r.b.startSwarm(r.ctx); err != nil {
		t.Fatal(err)
	}
	r.events = nil
	return r, cfg
}

// The restart that walked the main account away from the chest for nothing: a chest round under way, on the
// Falador road with keys of its own, starts there although the road is "far from the mule", and does
// not wait out the gem round the mule's bank would fund; an account anywhere else, or with no keys,
// is as far from the mule as ever and runs nothing from here.
func TestAChestRoundUnderWayStartsOnTheFaladorRoadWhateverThePlaceRule(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	keys := []actionsim.Stack{{ID: key, Count: 1}, {ID: key, Count: 1}}

	r, _ := resumeLoopRig(t, keys)
	r.b.ledger.Chest.KeysBank = 141
	r.sim.StandAt(2951, 3430) // the road, where the click storm used to stand
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "stop,round:chest,start" {
		t.Fatalf("on the Falador road with keys of its own: events %q, want the chest round at once", got)
	}

	for _, c := range []struct {
		name string
		at   tile.Tile
		want string
	}{
		{"Varrock, east of the mountain, keys of its own but not on the Falador side", tile.Tile{X: 3212, Z: 3424}, "loop"},
		// West of the mountain is the mule's side: the gem round the mule's bank funds is due and safe.
		{"Ardougne, on the mule's side", tile.Tile{X: 2661, Z: 3301}, "stop,round:gems,start"},
	} {
		r, _ := resumeLoopRig(t, keys)
		r.b.ledger.Chest.KeysBank = 141
		r.sim.StandAt(c.at.X, c.at.Z)
		r.loop(t)
		if got := strings.Join(r.events, ","); got != c.want {
			t.Errorf("%s: events %q, want %q", c.name, got, c.want)
		}
	}

	// On the road with nothing of its own: the gems wait for the walk to the post, as they always did.
	r, _ = resumeLoopRig(t, nil)
	r.sim.StandAt(2951, 3430)
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "loop" {
		t.Errorf("on the road with no keys: events %q", got)
	}
}

// A round under way still waits for the safe point's other rules: the hit, the hurt, the open bank.
func TestAChestRoundUnderWayStillWaitsForTheRestOfTheSafePoint(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	r, _ := resumeLoopRig(t, []actionsim.Stack{{ID: key, Count: 1}})
	r.b.ledger.Chest.KeysBank = 141
	r.sim.StandAt(2946, 3369)
	r.safe.InCombat = true
	r.loop(t)
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "loop,loop" {
		t.Fatalf("in a fight: events %q", got)
	}
	if r.b.pending.Mode != mastercraftplan.ModeChest || !r.b.pending.Resume {
		t.Fatalf("pending %+v: the round under way waits for its safe point, it is not dropped", r.b.pending)
	}
	r.safe.InCombat = false
	r.loop(t)
	if got := strings.Join(r.events, ","); got != "loop,loop,stop,round:chest,start" {
		t.Fatalf("out of the fight: events %q", got)
	}
}

// A fleet restart starts the script before the mule is heard, so the first decision sees no chest
// round under way and the next is two minutes off: by then the idle swarm has walked the account to
// the post. The first pass that can hear the mule decides again.
func TestADecisionMadeBlindIsMadeAgainWhenTheMuleIsHeard(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	r, _ := resumeLoopRig(t, []actionsim.Stack{{ID: key, Count: 1}})
	r.b.ledger.Chest.KeysBank = 141
	r.ctx.Bus = mulebus.NewBus() // nobody heard yet
	r.sim.StandAt(2946, 3369)

	r.loop(t)
	if r.checks != 1 || !r.b.blindCheck || r.b.every != masterCrafterRecheck || strings.Join(r.events, ",") != "loop" {
		t.Fatalf("mule unheard: checks %d, blind %v, every %v, events %v", r.checks, r.b.blindCheck, r.b.every, r.events)
	}
	r.loop(t)
	r.loop(t)
	if r.checks != 1 {
		t.Fatalf("%d checks while the mule is still unheard: nothing to decide on, the two minutes stand", r.checks)
	}
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	r.events = nil
	r.loop(t)
	if r.checks != 2 || r.b.blindCheck {
		t.Fatalf("the mule is heard: checks %d, blind %v; want it decided again at once", r.checks, r.b.blindCheck)
	}
	if got := strings.Join(r.events, ","); got != "stop,round:chest,start" {
		t.Fatalf("events %q: the round under way did not start the moment the mule was heard", got)
	}
	if !strings.Contains(strings.Join(r.logs, "\n"), "deciding again now") {
		t.Errorf("the log does not say it decided again: %v", r.logs)
	}
}

// The account's own place and pack are read the same way: a check made before the tile is known
// decides again once it is.
func TestADecisionMadeBeforeTheTileIsKnownIsMadeAgain(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	r, _ := resumeLoopRig(t, []actionsim.Stack{{ID: key, Count: 1}})
	r.b.ledger.Chest.KeysBank = 141
	r.loop(t) // no StandAt: the session has not read where the account is
	if r.checks != 1 || !r.b.blindCheck {
		t.Fatalf("checks %d, blind %v", r.checks, r.b.blindCheck)
	}
	r.loop(t)
	if r.checks != 1 {
		t.Fatalf("%d checks with the tile still unknown", r.checks)
	}
	r.sim.StandAt(2946, 3369)
	r.events = nil
	r.loop(t)
	if r.checks != 2 || strings.Join(r.events, ",") != "stop,round:chest,start" {
		t.Fatalf("checks %d, events %v: the tile is known, the round under way should start", r.checks, r.events)
	}
	// A decision that was never blind is not made again every pass.
	r2, _ := resumeLoopRig(t, nil)
	r2.sim.StandAt(2809, 3441)
	r2.loop(t)
	r2.loop(t)
	r2.loop(t)
	if r2.checks != 1 || r2.b.blindCheck {
		t.Fatalf("a check that could read everything: checks %d, blind %v", r2.checks, r2.b.blindCheck)
	}
}

// Waiting for its safe point, a round under way does not send the idle swarm walking to the post (it
// would be walked back); every other account walks home as it always did.
func TestTheIdleSwarmHoldsStillForAChestRoundUnderWay(t *testing.T) {
	cfg := chestCfg(t)
	key := itemID(t, cfg, "Crystal key", false)
	run := func(pack []actionsim.Stack, keysBank int) int {
		r, _ := resumeLoopRig(t, pack)
		r.b.newSwarm = func() masterSwarm { return &masterPostIdle{b: r.b} }
		if err := r.b.startSwarm(r.ctx); err != nil {
			t.Fatal(err)
		}
		r.b.ledger.Chest.KeysBank = keysBank
		r.ctx.requestStop() // every wait returns at once
		walks := 0
		masterWalkTo = func(_ *fleetrunner.Deps, _ tile.Tile, _ int, _ time.Duration) bool { walks++; return true }
		r.sim.StandAt(2946, 3369)
		r.sim.OpenBank(nil) // an open interface: not a safe point, so nothing starts
		r.loop(t)
		if len(r.rounds) != 0 {
			t.Fatalf("a round started with an interface open: %v", r.rounds)
		}
		return walks
	}
	if n := run([]actionsim.Stack{{ID: key, Count: 1}}, 141); n != 0 {
		t.Errorf("%d walk(s) to the post while a chest round under way waited for its safe point", n)
	}
	if n := run(nil, 0); n != 1 {
		t.Errorf("%d walk(s) to the post with no round under way, want 1", n)
	}
}
