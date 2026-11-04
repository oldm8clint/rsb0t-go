package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
)

// liveDecide is Decide over the bot's own ledger, as decideLive does, with the mule heard and
// stocked: the gems the rig's ledger and settings allow.
func (r *masterRig) liveDecide(stock map[string]int) {
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		return mastercraftplan.Decide(r.b.set, mastercraftplan.Snapshot{
			Now: time.Now(), MuleHeard: true, MuleSameWorld: true,
			Stock: func(item string) (int, bool) { return stock[item], true }, CraftingLevel: 30,
			Ledger: &r.b.ledger, Denied: r.b.denied,
		})
	}
}

// elapse makes the wait a mode is backing off for pass.
func (r *masterRig) elapse(mode mastercraftplan.Mode) {
	b := r.b.ledger.Backoffs[string(mode)]
	b.Until = time.Now().Add(-time.Minute)
	r.b.ledger.Backoffs[string(mode)] = b
}

func (r *masterRig) logged(sub string) bool {
	for _, l := range r.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// DEFECT 1: a round that fails for a passing reason was never paused, so every check stopped
// ScarabSwarm and walked to the mule again. It now backs off 30 minutes, then 60, and a round that
// works starts the count again; the ledger on disk carries it.
func TestMasterCrafterTransientFailureBacksOffAndDoubles(t *testing.T) {
	r := newMasterRig(t)
	r.liveDecide(map[string]int{"Uncut sapphire": 900, "Chisel": 1})
	r.outcome = masterRoundOutcome{Transient: true, Why: "the walk did not arrive"}
	r.dueNow()
	r.loop(t)
	if len(r.rounds) != 1 || r.b.ledger.Fails(mastercraftplan.ModeGems) != 1 {
		t.Fatalf("rounds %v, fails %d", r.rounds, r.b.ledger.Fails(mastercraftplan.ModeGems))
	}
	until, backed := r.b.ledger.BackedOff(mastercraftplan.ModeGems, time.Now())
	if !backed || until.Before(time.Now().Add(29*time.Minute)) || until.After(time.Now().Add(31*time.Minute)) {
		t.Fatalf("backing off until %v (backed %v), want about 30 minutes", until, backed)
	}
	// THE DEFECT: the next checks must not start the round again.
	for i := 0; i < 3; i++ {
		r.dueNow()
		r.loop(t)
	}
	if len(r.rounds) != 1 {
		t.Fatalf("%d rounds: a backed-off mode must not run at every check", len(r.rounds))
	}
	// The wait over, it runs once more, fails again, and waits an hour.
	r.elapse(mastercraftplan.ModeGems)
	r.dueNow()
	r.loop(t)
	if len(r.rounds) != 2 || r.b.ledger.Fails(mastercraftplan.ModeGems) != 2 {
		t.Fatalf("rounds %d, fails %d", len(r.rounds), r.b.ledger.Fails(mastercraftplan.ModeGems))
	}
	if until, _ := r.b.ledger.BackedOff(mastercraftplan.ModeGems, time.Now()); until.Before(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("second failure waits an hour: until %v", until)
	}
	// It is persisted.
	r.b.ledger = mastercraftplan.Ledger{}
	r.b.loadLedger(r.ctx)
	if r.b.ledger.Fails(mastercraftplan.ModeGems) != 2 {
		t.Fatalf("not persisted: %+v", r.b.ledger.Backoffs)
	}
	// A round that works clears it.
	r.elapse(mastercraftplan.ModeGems)
	r.outcome = masterRoundOutcome{Produced: 40, Handed: 40, HandoverTried: true}
	r.dueNow()
	r.loop(t)
	if r.b.ledger.Fails(mastercraftplan.ModeGems) != 0 || r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 0 {
		t.Fatalf("a round that worked must reset: %+v", r.b.ledger.Backoffs)
	}
}

// DEFECT 5: the gem kit the mule defers for its per-donor window is a passing failure, and backs
// off. The same round when the mule says it has none is the other kind (a pause).
func TestMasterCrafterDeferredKitBacksOff(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Uncut sapphire": 600, "Chisel": 1})
	kit := func(declined string) {
		scarabKitBundle = func(*fleetrunner.Deps, *mulebus.Bus, string, string, []fleetplan.ItemCount, fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
			return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{"Uncut sapphire": declined, "Chisel": declined}},
				fleetrunner.KitBundleInfo{Bundled: true}
		}
	}
	d := r.b.deps(r.ctx)

	kit("declined: you have already had an order in this window")
	out := r.b.gemRound(r.ctx, d, time.Now())
	if !out.Transient || out.Produced != 0 || out.HandoverTried {
		t.Fatalf("a deferred kit is a passing failure: %+v", out)
	}
	r.b.afterRound(r.ctx, out, time.Second)
	if r.b.ledger.Fails(mastercraftplan.ModeGems) != 1 || !r.b.ledger.GemPauseUntil.IsZero() {
		t.Fatalf("fails %d pause %v", r.b.ledger.Fails(mastercraftplan.ModeGems), r.b.ledger.GemPauseUntil)
	}
	if _, backed := r.b.ledger.BackedOff(mastercraftplan.ModeGems, time.Now()); !backed {
		t.Fatal("the deferred kit must back the gem rounds off")
	}

	r2 := newSimRig(t, nil, nil, map[string]int{"Uncut sapphire": 600, "Chisel": 1})
	kit("declined: none in the bank")
	out = r2.b.gemRound(r2.ctx, r2.b.deps(r2.ctx), time.Now())
	if out.Transient {
		t.Fatalf("a mule that holds none is not a passing failure: %+v", out)
	}
	r2.b.afterRound(r2.ctx, out, time.Second)
	if r2.b.ledger.Fails(mastercraftplan.ModeGems) != 0 || r2.b.ledger.GemPauseUntil.IsZero() {
		t.Fatalf("an empty mule pauses, it does not back off: fails %d pause %v", r2.b.ledger.Fails(mastercraftplan.ModeGems), r2.b.ledger.GemPauseUntil)
	}
}

// DEFECT 4: the first walk gets ScarabSwarm's own 8-minute budget, and a walk that does not
// arrive ends the round before any bank is touched.
func TestMasterCrafterFirstWalkHasTheConsolidateBudget(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Uncut sapphire": 600, "Chisel": 1})
	var budget time.Duration
	var radius int
	masterWalkTo = func(_ *fleetrunner.Deps, _ tile.Tile, rad int, b time.Duration) bool {
		budget, radius = b, rad
		return false
	}
	opened := 0
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { opened++; return nil, false }
	out := r.b.gemRound(r.ctx, r.b.deps(r.ctx), time.Now())
	if budget != scarabConsolidateWalk || scarabConsolidateWalk != 8*time.Minute || radius != 3 {
		t.Fatalf("walk budget %s radius %d, want ScarabSwarm's consolidate trip: %s", budget, radius, scarabConsolidateWalk)
	}
	if !out.Transient || opened != 0 || !strings.Contains(out.Why, "could not reach") {
		t.Fatalf("opened %d banks: %+v", opened, out)
	}
	// The other rounds begin the same way.
	for name, run := range map[string]func() masterRoundOutcome{
		"bows":    func() masterRoundOutcome { return r.b.bowRound(r.ctx, r.b.deps(r.ctx), time.Now()) },
		"deliver": func() masterRoundOutcome { return r.b.deliverRound(r.ctx, r.b.deps(r.ctx)) },
	} {
		budget = 0
		if out := run(); !out.Transient || budget != 8*time.Minute || opened != 0 {
			t.Fatalf("%s: budget %s, opened %d, %+v", name, budget, opened, out)
		}
	}
}

// DEFECT 2: a hand-over that leaves products in the bank backs off on its own account, does not
// outrank the crafting for ever, and three in a row suspend every round, loudly, with the products
// kept in the bank.
func TestMasterCrafterFailedHandoversBackOffThenGiveUp(t *testing.T) {
	r := newMasterRig(t)
	r.liveDecide(map[string]int{"Uncut sapphire": 900, "Chisel": 1})
	failedHandover := func() {
		r.b.round = func(_ *Context, dec mastercraftplan.Decision) masterRoundOutcome {
			r.rounds = append(r.rounds, dec.Mode)
			r.b.ledger.PendingDeliver = true
			return masterRoundOutcome{Mode: dec.Mode, Produced: 50, HandoverTried: true, HandoverFailed: true, Why: "the mule never met me"}
		}
	}
	failedHandover()

	r.dueNow()
	r.loop(t) // gem round 1: cut 50, hand-over failed
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 1 || r.b.ledger.Fails(mastercraftplan.ModeGems) != 0 || !r.b.ledger.PendingDeliver {
		t.Fatalf("%+v", r.b.ledger)
	}
	// The pending hand-over now backs off: the next check goes on crafting (whose round also tries to
	// hand over) instead of running a deliver round at every check.
	r.dueNow()
	r.loop(t)
	if got := fmt.Sprint(r.rounds); got != "[gems gems]" || r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 2 {
		t.Fatalf("rounds %s, hand-over fails %d", got, r.b.ledger.Fails(mastercraftplan.ModeDeliver))
	}
	r.dueNow()
	r.loop(t) // the third failure
	if !r.b.ledger.HandoverGaveUp(r.b.set) || !r.logged("GIVING UP") {
		t.Fatalf("three failed hand-overs in a row must give up, loudly: %+v", r.b.ledger.Backoffs)
	}
	// From now on no round starts, whatever the mule holds and however long the wait has been.
	r.elapse(mastercraftplan.ModeDeliver)
	r.elapse(mastercraftplan.ModeGems)
	before := len(r.rounds)
	r.logs = nil
	for i := 0; i < 3; i++ {
		r.dueNow()
		r.loop(t)
	}
	if len(r.rounds) != before {
		t.Fatalf("%d rounds started after giving up", len(r.rounds)-before)
	}
	if !r.logged("GIVEN UP") || r.b.pending.Mode != "" {
		t.Fatalf("every check must say so loudly: %v (pending %v)", r.logs, r.b.pending)
	}
	// A restart tries again, after the wait the failures earned.
	r.b.clearGiveUp(r.ctx)
	if r.b.ledger.HandoverGaveUp(r.b.set) || r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 0 {
		t.Fatal("a restart must clear the suspension")
	}
	if !r.logged("this restart tries again") {
		t.Fatalf("the restart must say so: %v", r.logs)
	}
	if _, backed := r.b.ledger.BackedOff(mastercraftplan.ModeDeliver, time.Now()); backed {
		t.Fatal("the test elapsed this wait")
	}
}

func TestMasterCrafterHandoverSuccessResetsAndDeliverRoundsBackOff(t *testing.T) {
	r := newMasterRig(t)
	r.b.ledger.PendingDeliver = true
	// The mule is away: the deliver round never got to a hand-over, a passing failure.
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeDeliver, Transient: true, Why: "the mule is not heard"}, time.Second)
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 1 {
		t.Fatalf("%+v", r.b.ledger.Backoffs)
	}
	// A deliver round that handed everything over clears it.
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeDeliver, Handed: 90, HandoverTried: true}, time.Second)
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 0 {
		t.Fatalf("%+v", r.b.ledger.Backoffs)
	}
	// One that tried and left products counts once, not twice (it was a hand-over that failed).
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeDeliver, Transient: true, HandoverTried: true, HandoverFailed: true}, time.Second)
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 1 {
		t.Fatalf("%+v", r.b.ledger.Backoffs)
	}
}

func TestMasterCrafterAStopDuringARoundSetsNoWait(t *testing.T) {
	r := newMasterRig(t)
	r.ctx.requestStop()
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeGems, Transient: true, HandoverTried: true, HandoverFailed: true, Why: "stopped"}, time.Second)
	if len(r.b.ledger.Backoffs) != 0 || !r.b.ledger.GemPauseUntil.IsZero() {
		t.Fatalf("a Stop must not back anything off: %+v", r.b.ledger)
	}
	r2 := newMasterRig(t)
	r2.b.afterRound(r2.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeBows, Why: "no bow left"}, time.Second)
	if r2.b.ledger.BowPauseUntil.IsZero() || r2.b.ledger.Fails(mastercraftplan.ModeBows) != 0 {
		t.Fatalf("a round that found nothing for a lasting reason pauses: %+v", r2.b.ledger)
	}
}

// DEFECT 4: HP at the default 80%, the mule's side of the mountain, on the surface.
func TestMasterCrafterWaitsForThePlaceAndTheHitpoints(t *testing.T) {
	r := newMasterRig(t)
	r.decision = mastercraftplan.Decision{Mode: mastercraftplan.ModeGems, Why: "600 uncut gems"}
	far := true
	r.b.farFromMuleSeam = func(*Context) bool { return far }
	r.loop(t)
	r.loop(t)
	if len(r.rounds) != 0 {
		t.Fatal("a round started far from the mule")
	}
	far = false
	r.safe.HP = 31 // 77.5% of 40: under the default 80%
	r.loop(t)
	if len(r.rounds) != 0 {
		t.Fatal("a round started at 31/40 hitpoints")
	}
	r.safe.HP = 32
	r.loop(t)
	if len(r.rounds) != 1 {
		t.Fatalf("32/40 is 80%%: rounds %v", r.rounds)
	}
}

func TestFarFromMuleIsTheMulesSideOfTheMountainOrNearThePost(t *testing.T) {
	r := newMasterRig(t)
	st := r.ctx.Session.State()
	far := func() bool { return r.b.farFromMule(r.ctx) }
	if !far() {
		t.Fatal("no tile known is not near")
	}
	for name, c := range map[string]struct {
		x, z int
		far  bool
	}{
		"East Ardougne south bank": {2655, 3286, false},
		"Seers' Village bank":      {2725, 3493, false},
		"Catherby":                 {2809, 3441, false},
		"Edgeville bank":           {3094, 3491, true},
		"Varrock east bank":        {3253, 3420, true},
		"Al Kharid":                {3270, 3167, true},
		"Brimhaven (Karamja)":      {2800, 3150, true},
		"the Wilderness":           {2960, 3700, true},
	} {
		masterPut(t, st, c.x, c.z)
		if got := far(); got != c.far {
			t.Errorf("%s (%d,%d): far = %v, want %v", name, c.x, c.z, got, c.far)
		}
	}
	// East of the mountain but a short walk from a post that is there: not far.
	r.ctx.CurrentWorld = 1
	home := tile.Tile{X: 2870, Z: 3440}
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2870, Z: 3441}, 20, mulebus.StateHome, &home), 1))
	masterPut(t, st, 2860, 3440)
	if far() {
		t.Error("10 tiles from the post is a short walk wherever the post is")
	}
	masterPut(t, st, 2905, 3440)
	if !far() {
		t.Error("30+ tiles east of the post is not")
	}
}

// ---- rounds against a simulated bank ----

// masterSimStackable lists the objects the simulated server stacks (none unless a test says so: Nature runes and coins for the
// Nature gate's tests).
var masterSimStackable = map[int32]bool{}

// masterSimObjs is the server's note table for the items the rounds draw: a note is the id above.
func masterSimObjs() actionsim.Objs {
	bases := []int32{1623, 1607, 1619, 1603, 1625, 1609, 851, 62, 857, 68, 1777, 1436, 855, 859, 861}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return masterSimStackable[id] },
		NoteOf: func(id int32) int32 {
			for _, b := range bases {
				if b == id {
					return id + 1
				}
			}
			return 0
		},
		UnnoteOf: func(id int32) int32 {
			for _, b := range bases {
				if b+1 == id {
					return b
				}
			}
			return 0
		},
	}
}

// newSimRig is a MasterCrafter whose account is a bank-and-pack simulator standing at the mule's
// post, with the mule heard on world 1 and publishing stock. The walk, the kit ask, the bank door
// and the trade are the seams a test sets.
func newSimRig(t *testing.T, pack, bank []actionsim.Stack, stock map[string]int) *masterRig {
	t.Helper()
	sim := actionsim.New(masterSimObjs(), pack)
	sim.OpenBank(bank)
	r := &masterRig{}
	r.ctx = NewContext(sim, "main", nil, nil, nil, actionsim.Components(), mulebus.NewBus(), nil, nil, map[string]interface{}{}, func(l string) { r.logs = append(r.logs, l) })
	r.ctx.CurrentWorld = 1
	r.ctx.Peers = peerbank.New()
	var hs []peerbank.Holding
	for k, v := range stock {
		hs = append(hs, peerbank.Holding{Item: k, Count: v})
	}
	r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, hs, nil, false)
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	mindHoldStat(sim.State(), gemrunner.CraftingStat, levels.XPAtLevel(30), 30)
	r.b = &masterCrafterBot{
		set: masterOrderSettings(), mule: "mule",
		denied: map[string]bool{}, rounds: map[mastercraftplan.Mode]int{},
		store: settings.NewStore(t.TempDir()),
	}
	r.b.every = r.b.set.CheckEvery
	oldWalk, oldKit, oldOpen, oldDeliver, oldIDs := masterWalkTo, scarabKitBundle, mastercraftrunner.OpenBank, masterDeliver, mastercraftrunner.ResolveBowIDs
	t.Cleanup(func() {
		masterWalkTo, scarabKitBundle, mastercraftrunner.OpenBank, masterDeliver, mastercraftrunner.ResolveBowIDs = oldWalk, oldKit, oldOpen, oldDeliver, oldIDs
	})
	masterWalkTo = func(*fleetrunner.Deps, tile.Tile, int, time.Duration) bool { return true }
	// The hand-over draw confirms each product by the id its note arrives as: this rig has no object config, so the
	// simulator's own note table answers (twopertick-1012).
	oldNote := mastercraftrunner.NoteOfID
	t.Cleanup(func() { mastercraftrunner.NoteOfID = oldNote })
	simObjs := masterSimObjs()
	mastercraftrunner.NoteOfID = func(id int32) int32 {
		if n := simObjs.NoteOf(id); n != 0 {
			return n
		}
		return id
	}
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) {
		sim.OpenBank(sim.Bank())
		return actions.NewBank(sim, actionsim.Components()), true
	}
	r.sim = sim
	return r
}

func simCount(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// A hand-over with the mule not there leaves everything in the bank and says so: that is a failed
// hand-over, and reads as one.
func TestHandOverWithTheMuleAwayLeavesTheProductsBanked(t *testing.T) {
	r := newSimRig(t, nil, []actionsim.Stack{{ID: 1607, Count: 100}, {ID: 1633, Count: 4}}, nil)
	r.ctx.Bus = mulebus.NewBus() // no heartbeat: nobody is there
	ho := r.b.handOver(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, mastercraftrunner.GemProducts())
	if !ho.failed() || ho.Unknown || ho.Left["Sapphire"] != 100 || ho.Left["Crushed gemstone"] != 4 || !strings.Contains(ho.Why, "not here") {
		t.Fatalf("%+v", ho)
	}
	if len(r.sim.Pack()) != 0 {
		t.Fatalf("nothing is drawn for a mule that is not there: %v", r.sim.Pack())
	}
	// A bank that will not open says it could not read it: also a failure.
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	if ho := r.b.handOver(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, mastercraftrunner.GemProducts()); !ho.failed() || !ho.Unknown {
		t.Fatalf("%+v", ho)
	}
	// Nothing to carry is not a failure, and touches no bank.
	if ho := r.b.handOver(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, nil); ho.failed() {
		t.Fatalf("%+v", ho)
	}
}

// A bow round that strings 50 of a kit of 750 bows and 300 strings hands the mule the 50 strung
// bows and KEEPS the unstrung bows and the strings: the next check resumes from this account's own
// bank (BowsReadyWithOwn), so handing them back would only be traded straight back (live on
// def92a2e: 80,425 items out, 95,260 back in). The ledger counts only the strung bows (a strung and
// an unstrung bow share a name).
func TestBowRoundKeepsTheUnstrungBowsAndStrings(t *testing.T) {
	const strung, unstrung, str = 851, 62, 1777
	r := newSimRig(t, nil, []actionsim.Stack{{ID: strung, Count: 50}, {ID: unstrung, Count: 700}, {ID: str, Count: 300}, {ID: 1607, Count: 9}}, nil)
	mastercraftrunner.ResolveBowIDs = func(_ *cachecfg.Config, bow string) (bowstringrunner.IDs, error) {
		if bow != "Maple longbow" {
			return bowstringrunner.IDs{}, fmt.Errorf("not resolvable")
		}
		return bowstringrunner.IDs{Unstrung: unstrung, Strung: strung, String: str}, nil
	}
	r.b.set.BowNames = []string{"Maple longbow"}
	type call struct {
		items       []string
		rows, slots int
	}
	var calls []call
	crossed := []map[string]int{{"Maple longbow": 50}}
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, mule string, _ tile.Tile, rows, drawn int, items []string) runerunner.Delivery {
		calls = append(calls, call{items, rows, drawn})
		for _, s := range r.sim.Pack() {
			r.sim.Remove(s.ID, s.Count) // the mule takes what was offered
		}
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: crossed[len(calls)-1]}
	}
	handed, failed, why := r.b.handOverBows(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, nil)
	if failed || why != "" || handed != 50 {
		t.Fatalf("handed %d failed %v why %q", handed, failed, why)
	}
	if len(calls) != 1 {
		t.Fatalf("%d hand-overs: the strung bows only", len(calls))
	}
	if strings.Join(calls[0].items, ",") != "Maple longbow" || calls[0].rows != 1 || calls[0].slots != 1 {
		t.Fatalf("hand-over %+v: the strung bows as one note stack", calls[0])
	}
	// THE LEDGER: 50 strung at the mule, not 750.
	if got := r.b.ledger.Strung("Maple longbow"); got != 50 || r.b.ledger.BowsHanded != 50 {
		t.Fatalf("strung %d, bows handed %d: only the strung bows are the ledger's", got, r.b.ledger.BowsHanded)
	}
	bank := r.sim.Bank()
	if simCount(bank, strung) != 0 || simCount(bank, unstrung) != 700 || simCount(bank, str) != 300 || simCount(bank, 1607) != 9 {
		t.Fatalf("bank %v: the strung bows gone, the unstrung bows, the strings and the gems kept", bank)
	}
}

// When the strung bows could not be handed over the leftovers wait: one failed hand-over, not two
// trips to a mule that is not there.
func TestBowHandBackWaitsWhenTheStrungBowsDidNotCross(t *testing.T) {
	r := newSimRig(t, nil, []actionsim.Stack{{ID: 851, Count: 50}, {ID: 62, Count: 700}, {ID: 1777, Count: 300}}, nil)
	mastercraftrunner.ResolveBowIDs = func(*cachecfg.Config, string) (bowstringrunner.IDs, error) {
		return bowstringrunner.IDs{Unstrung: 62, Strung: 851, String: 1777}, nil
	}
	r.b.set.BowNames = []string{"Maple longbow"}
	calls := 0
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		calls++
		return runerunner.Delivery{Result: donorrunner.Result{GaveUp: true, Reason: "the mule never met me"}}
	}
	handed, failed, why := r.b.handOverBows(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, nil)
	if !failed || handed != 0 || calls != 1 || why == "" {
		t.Fatalf("handed %d failed %v calls %d why %q", handed, failed, calls, why)
	}
	if simCount(r.sim.Bank(), 851) != 50 || simCount(r.sim.Bank(), 62) != 700 {
		t.Fatalf("the products must still be in the bank: %v", r.sim.Bank())
	}
}

// The deliver round hands the gems and the strung bows over, and keeps the unstrung bows and the
// strings for the next bow round.
func TestDeliverRoundKeepsTheLeftoverBows(t *testing.T) {
	r := newSimRig(t, nil, []actionsim.Stack{{ID: 1607, Count: 30}, {ID: 851, Count: 5}, {ID: 62, Count: 70}, {ID: 1777, Count: 10}}, nil)
	mastercraftrunner.ResolveBowIDs = func(*cachecfg.Config, string) (bowstringrunner.IDs, error) {
		return bowstringrunner.IDs{Unstrung: 62, Strung: 851, String: 1777}, nil
	}
	r.b.set.BowNames = []string{"Maple longbow"}
	var names []string
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _ string, _ tile.Tile, _, _ int, items []string) runerunner.Delivery {
		names = append(names, strings.Join(items, ","))
		del := map[string]int{}
		for _, s := range r.sim.Pack() {
			del[fmt.Sprint(s.ID)] += r.sim.Remove(s.ID, s.Count)
		}
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: map[string]int{"Sapphire": del["1608"], "Maple longbow": del["852"] + del["63"], "Bow string": del["1778"]}}
	}
	out := r.b.deliverRound(r.ctx, r.b.deps(r.ctx))
	if out.Transient || !out.HandoverTried || out.HandoverFailed {
		t.Fatalf("%+v", out)
	}
	if len(names) != 1 || !strings.Contains(names[0], "Sapphire") || !strings.Contains(names[0], "Maple longbow") || strings.Contains(names[0], "Bow string") {
		t.Fatalf("hand-overs %q", names)
	}
	if simCount(r.sim.Bank(), 62) != 70 || simCount(r.sim.Bank(), 1777) != 10 {
		t.Fatalf("the unstrung bows and the strings stay: %v", r.sim.Bank())
	}
	if r.b.ledger.GemsHanded != 30 || r.b.ledger.Strung("Maple longbow") != 5 || r.b.ledger.PendingDeliver {
		t.Fatalf("%+v", r.b.ledger)
	}
}

// Verification of ea9bb3b0 (3 Oct): a gem round's clean hand-over carries gems only, so it must not
// clear a hand-over a bow round left pending, nor reset the hand-over backoff. Only a deliver round,
// which carries both kinds, clears them.
func TestMasterCrafterOtherKindHandoverKeepsThePendingDelivery(t *testing.T) {
	r := newMasterRig(t)
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeBows, Produced: 300, HandoverTried: true, HandoverFailed: true}, time.Second)
	r.b.ledger.PendingDeliver = true
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 1 {
		t.Fatalf("a failed bow hand-over backs the hand-over off: %+v", r.b.ledger.Backoffs)
	}
	r.b.afterRound(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeGems, Produced: 90, Handed: 90, HandoverTried: true}, time.Second)
	if r.b.ledger.Fails(mastercraftplan.ModeDeliver) != 1 || !r.b.ledger.PendingDeliver {
		t.Fatalf("a gem round's clean hand-over must leave the bow delivery pending: pending=%v %+v", r.b.ledger.PendingDeliver, r.b.ledger.Backoffs)
	}
}
