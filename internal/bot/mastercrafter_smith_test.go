package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// smithFake plays the Varrock west bank and anvil for the trip loop: a bank of bars by name, a pack, and
// the anvil turning the pack's bars into their products. The loop's seams are replaced for the test.
type smithFake struct {
	t          *testing.T
	bank       map[string]int
	pack       map[string]int
	hammer     bool
	opens      int
	failOpens  int // opens that fail first
	noAnvil    bool
	draws      [][]mastercraftplan.SmithDraw
	smithed    map[string]int
	refuse     map[string]string // bar -> anvil reason
	bankAtTrip []int             // steel bars held when each trip began
}

func newSmithFake(t *testing.T, bank map[string]int) *smithFake {
	masterSmithRetryWait = 0
	t.Cleanup(func() { masterSmithRetryWait = 3 * time.Second })
	f := &smithFake{t: t, bank: bank, pack: map[string]int{}, hammer: true, smithed: map[string]int{}, refuse: map[string]string{}}
	oldKit, oldFind, oldApproach, oldSmith := masterSmithKitVisit, masterFindAnvil, masterApproachAnvil, masterSmithAnvil
	t.Cleanup(func() {
		masterSmithKitVisit, masterFindAnvil, masterApproachAnvil, masterSmithAnvil = oldKit, oldFind, oldApproach, oldSmith
	})
	masterSmithKitVisit = func(d *fleetrunner.Deps, products []mastercraftplan.SmithProduct, travel *fleetrunner.Travel) (mastercraftrunner.SmithKitResult, bool) {
		if f.failOpens > 0 {
			f.failOpens--
			return mastercraftrunner.SmithKitResult{}, false
		}
		f.opens++
		// The deposit: the pack's bars go back in.
		for bar, n := range f.pack {
			f.bank[bar] += n
		}
		f.pack = map[string]int{}
		var banked []gamestate.SlotItem
		for _, p := range mastercraftplan.SmithAllProducts() {
			if n := f.bank[p.Bar]; n > 0 {
				banked = append(banked, gamestate.SlotItem{Item: gamestate.Item{ID: p.BarID, Count: int32(n)}})
			}
		}
		res := mastercraftrunner.SmithKitResult{Banked: banked, Free: 27}
		if !f.hammer {
			res.NoHammer = true
			return res, true
		}
		have := func(item string) int { return f.bank[item] }
		res.Draws = mastercraftplan.PlanSmithDraws(products, have, 27)
		for _, dr := range res.Draws {
			f.bank[dr.Product.Bar] -= dr.N
			f.pack[dr.Product.Bar] += dr.N
		}
		if len(res.Draws) > 0 {
			f.draws = append(f.draws, res.Draws)
		}
		return res, true
	}
	masterFindAnvil = func(*fleetrunner.Deps, tile.Tile, int) (locindex.Match, bool) {
		return locindex.Match{Loc: locindex.Loc{ID: 2783, X: 3188, Z: 3426}}, !f.noAnvil
	}
	masterApproachAnvil = func(*fleetrunner.Deps, locindex.Match, mastercraftrunner.WalkFunc, time.Duration) bool { return true }
	masterSmithAnvil = func(_ *fleetrunner.Deps, _ locindex.Match, p mastercraftplan.SmithProduct, hold func() bool) mastercraftrunner.SmithResult {
		if why := f.refuse[p.Bar]; why != "" {
			return mastercraftrunner.SmithResult{Reason: why}
		}
		n := f.pack[p.Bar]
		makes := n / p.BarsPer
		used := makes * p.BarsPer
		f.pack[p.Bar] -= used
		f.smithed[p.Bar] += used
		return mastercraftrunner.SmithResult{Bars: used, Makes: makes, Uses: (makes + 9) / 10, XP: used * p.XPTenthsPerBar / 10, Reason: "done"}
	}
	return f
}

func (f *smithFake) run(r *masterRig, products []mastercraftplan.SmithProduct) *smithRun {
	run := &smithRun{b: r.b, ctx: r.ctx, d: &fleetrunner.Deps{}, products: products, hold: func() bool { return true }, bars: map[string]int{}}
	run.loop()
	return run
}

// THE OWNER'S WORD (8 Oct 2026): "it should get all bars out when there are 8000 not just small amounts".
// A round that started at 8001 Steel bars goes on below 8000 until the bank holds none, every trip a full
// pack (27 slots with the hammer and no rub item), and ends "used up" so the products go to the mule.
func TestSmithRoundStartedAt8001KeepsGoingBelow8000UntilZero(t *testing.T) {
	r := newMasterRig(t)
	f := newSmithFake(t, map[string]int{"Steel bar": 8001})
	set := mastercraftplan.DefaultSettings()
	// The trigger is the plan's: 8001 starts it.
	if _, ok := mastercraftplan.SmithReady(set, 60, false, stockOfMap(map[string]int{"Steel bar": 8001}), nil); !ok {
		t.Fatal("8001 Steel bars did not start a round")
	}
	run := f.run(r, mastercraftplan.SmithProductsFor(set, 60))
	if !run.usedUp || run.transient || run.reason != "no bar left in the bank to smith" {
		t.Fatalf("run %+v", run)
	}
	if f.bank["Steel bar"] != 0 || f.smithed["Steel bar"] != 8001 || run.barsTotal() != 8001 || run.tips != 8001*15 {
		t.Fatalf("bank %v smithed %v bars %d tips %d", f.bank, f.smithed, run.barsTotal(), run.tips)
	}
	// 8001 = 296 full packs of 27 and one of 9.
	if run.trips != 297 {
		t.Errorf("%d trips, want 297", run.trips)
	}
	for i, ds := range f.draws[:len(f.draws)-1] {
		if len(ds) != 1 || ds[0].N != 27 {
			t.Fatalf("trip %d drew %+v: a small batch while the bank held more", i+1, ds)
		}
	}
	// It kept going once the stock fell under the threshold: trips continued with fewer than 8000 left.
	if got := f.draws[len(f.draws)-1][0].N; got != 9 {
		t.Errorf("the last trip drew %d", got)
	}
	// "started at 8001 ... below 8000": the active ledger resumes at any count above zero.
	for _, left := range []int{7999, 5, 1} {
		if _, ok := mastercraftplan.SmithReady(set, 60, true, nil, func(item string) int {
			if item == "Steel bar" {
				return left
			}
			return 0
		}); !ok {
			t.Errorf("an active round with %d Steel bars left was not resumed", left)
		}
	}
	// The log names the starting stock and the progress.
	var progress int
	for _, l := range r.logs {
		if strings.Contains(l, "smith: trip ") && strings.Contains(l, "bank now holds") {
			progress++
		}
	}
	if progress != 297 {
		t.Errorf("%d progress lines, want one a trip (297)", progress)
	}
}

func stockOfMap(m map[string]int) mastercraftplan.Stock {
	return func(item string) (int, bool) { return m[item], true }
}

// Every kind goes through: the best bar first, then the rest, the Iron bars into whole platebodies; what
// cannot make a platebody (3 Iron bars) is left, and the round ends used up.
func TestSmithRoundWorksThroughEveryBarBestFirst(t *testing.T) {
	r := newMasterRig(t)
	f := newSmithFake(t, map[string]int{"Runite bar": 3, "Adamantite bar": 30, "Mithril bar": 100, "Steel bar": 40, "Iron bar": 53, "Silver bar": 99, "Gold bar": 99})
	run := f.run(r, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 99))
	if !run.usedUp {
		t.Fatalf("%+v", run)
	}
	if f.bank["Runite bar"] != 0 || f.bank["Adamantite bar"] != 0 || f.bank["Mithril bar"] != 0 || f.bank["Steel bar"] != 0 || f.bank["Iron bar"] != 3 {
		t.Errorf("bank after: %v", f.bank)
	}
	if f.bank["Silver bar"] != 99 || f.bank["Gold bar"] != 99 {
		t.Errorf("silver and gold bars were touched: %v", f.bank)
	}
	if run.tips != (3+30+100+40)*15 || run.bodies != 10 {
		t.Errorf("tips %d bodies %d", run.tips, run.bodies)
	}
	first := f.draws[0]
	if len(first) < 2 || first[0].Product.Bar != "Runite bar" || first[1].Product.Bar != "Adamantite bar" {
		t.Errorf("the first trip %+v: best bar first", first)
	}
	// Iron comes last of all.
	last := f.draws[len(f.draws)-1]
	if last[len(last)-1].Product.Bar != "Iron bar" {
		t.Errorf("the last trip %+v", last)
	}
	var started bool
	for _, l := range r.logs {
		started = started || strings.Contains(l, "smith: trip 1:")
	}
	if !started {
		t.Errorf("no trip line in %v", r.logs)
	}
}

func TestSmithRoundFailuresAndRefusals(t *testing.T) {
	// The bank will not open three times: the round gives up for now, transient, its flag untouched.
	r := newMasterRig(t)
	f := newSmithFake(t, map[string]int{"Steel bar": 100})
	f.failOpens = 5
	run := f.run(r, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 60))
	if run.usedUp || !run.transient || run.fails != masterSmithFails || f.smithed["Steel bar"] != 0 {
		t.Errorf("unopenable bank: %+v", run)
	}
	// No hammer anywhere: stops, transient.
	r = newMasterRig(t)
	f = newSmithFake(t, map[string]int{"Steel bar": 100})
	f.hammer = false
	run = f.run(r, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 60))
	if run.usedUp || !run.transient || !strings.Contains(run.reason, "hammer") {
		t.Errorf("no hammer: %+v", run)
	}
	// A product the anvil refuses is dropped for the rest of the round, the others carry on.
	r = newMasterRig(t)
	f = newSmithFake(t, map[string]int{"Mithril bar": 30, "Steel bar": 30})
	f.refuse["Mithril bar"] = "members"
	run = f.run(r, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 60))
	if !run.usedUp || f.smithed["Steel bar"] != 30 || f.smithed["Mithril bar"] != 0 {
		t.Errorf("a refused product: %+v smithed %v", run, f.smithed)
	}
	// An anvil that is not in the index fails the trips.
	r = newMasterRig(t)
	f = newSmithFake(t, map[string]int{"Steel bar": 100})
	f.noAnvil = true
	run = f.run(r, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 60))
	if run.usedUp || !run.transient {
		t.Errorf("no anvil: %+v", run)
	}
	// A stop ends the loop at once with nothing marked used up.
	r = newMasterRig(t)
	f = newSmithFake(t, map[string]int{"Steel bar": 100})
	run = &smithRun{b: r.b, ctx: r.ctx, d: &fleetrunner.Deps{}, products: mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 60), hold: func() bool { return false }, bars: map[string]int{}}
	run.loop()
	if run.usedUp || f.opens != 0 {
		t.Errorf("held: %+v opens %d", run, f.opens)
	}
}

// The round in progress is saved with the ledger and read back: a restart carries it on.
func TestSmithActiveSurvivesARestart(t *testing.T) {
	r := newMasterRig(t)
	r.b.ledger.SmithActive = true
	r.b.ledger.SmithBars, r.b.ledger.ArrowtipsMade, r.b.ledger.PlatebodiesMade = 11, 165, 2
	r.b.saveLedger(r.ctx)
	r.b.ledger = mastercraftplan.Ledger{}
	r.b.loadLedger(r.ctx)
	if !r.b.ledger.SmithActive || r.b.ledger.SmithBars != 11 || r.b.ledger.ArrowtipsMade != 165 || r.b.ledger.PlatebodiesMade != 2 {
		t.Fatalf("ledger %+v", r.b.ledger)
	}
	// A restart that has not read the bank yet still resumes it.
	d := mastercraftplan.Decide(r.b.set, mastercraftplan.Snapshot{Now: time.Now(), MuleHeard: true, MuleSameWorld: true,
		Stock: stockOfMap(nil), SmithingLevel: 60, Ledger: &r.b.ledger})
	if d.Mode != mastercraftplan.ModeSmith || !d.Resume {
		t.Errorf("decided %s (%s)", d.Mode, d.Why)
	}
}

// A hand-over of a failed round does not give back the bars an anvil round has drawn.
func TestDeliverKeepsTheBarsOfAnAnvilRound(t *testing.T) {
	r := newMasterRig(t)
	r.b.ledger.SmithActive = true
	if !r.b.smithKeepsBars() {
		t.Error("an active anvil round's bars would be handed back")
	}
	r.b.ledger.SmithActive = false
	if r.b.smithKeepsBars() {
		t.Error("an idle ledger keeps the bars from the hand-over")
	}
}

// The anvil round's walks alch (the owner's alch list), rub when there is nothing to alch, and keep running
// on Strange fruit; no teleport (the bank and the anvil are a hop apart).
func TestSmithTravelAlchsRubsAndKeepsRunning(t *testing.T) {
	tv := smithTravel(mastercraftplan.DefaultSettings())
	if !tv.Rub || !tv.Alch.Enabled || !tv.KeepRunning || tv.Tele.Enabled {
		t.Errorf("%+v", tv)
	}
}
