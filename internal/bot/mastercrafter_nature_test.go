package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// simGate plays the gate round's world as the live handlers leave it: a carrier's trade each Gate call while any are left, the
// ruins, the altar and the portal. It applies the table's experience, so a whole round runs through the real stage machine.
type simGate struct {
	level   int
	members bool

	where    runecraftplan.Region
	temple   string
	atRuins  bool
	packEss  int
	talisman bool
	end      string

	tradesLeft, endAfterTrades, trades int
	natures, xp, crafts                int

	failFirst  map[runecraftplan.GateStage]int
	failAlways map[runecraftplan.GateStage]bool
	calls      map[runecraftplan.GateStage]int
	stopAfter  int
	total      int
	order      []runecraftplan.GateStage
}

func newSimGate() *simGate {
	return &simGate{level: 44, members: true, talisman: true, tradesLeft: 3,
		failFirst: map[runecraftplan.GateStage]int{}, failAlways: map[runecraftplan.GateStage]bool{}, calls: map[runecraftplan.GateStage]int{}}
}

func (s *simGate) Read() runecraftplan.GateSnapshot {
	return runecraftplan.GateSnapshot{Level: s.level, Members: s.members, Where: s.where, Temple: s.temple, AtRuins: s.atRuins,
		PackEssence: s.packEss, Talisman: s.talisman, End: s.end}
}
func (s *simGate) Fingerprint() string {
	return fmt.Sprintf("%d|%v|%s|%v|%d|%d|%d|%d", s.level, s.where, s.temple, s.atRuins, s.packEss, s.trades, s.crafts, s.total)
}
func (s *simGate) Prepare() bool       { return true }
func (s *simGate) Stopped() bool       { return s.stopAfter > 0 && s.total >= s.stopAfter }
func (s *simGate) Pause(time.Duration) {}
func (s *simGate) enter(st runecraftplan.GateStage) (runeStepResult, bool) {
	s.total++
	s.calls[st]++
	s.order = append(s.order, st)
	if s.failAlways[st] || s.failFirst[st] >= s.calls[st] {
		return runeFail("scripted failure of " + string(st)), true
	}
	return runeStepResult{}, false
}

func (s *simGate) Gate() runeStepResult {
	if r, failed := s.enter(runecraftplan.GateTrade); failed {
		return r
	}
	if s.tradesLeft > 0 {
		s.tradesLeft--
		s.trades++
		s.packEss += 25
		s.atRuins = false
		if s.endAfterTrades > 0 && s.trades >= s.endAfterTrades {
			s.end = "scripted end"
		}
		return runeOK()
	}
	s.end = "no more carriers"
	return runeOK()
}

func (s *simGate) ToRuins(st runecraftplan.Step) runeStepResult {
	stage := runecraftplan.GateWalkToRuins
	if st.Stage == runecraftplan.StageUseTalisman {
		stage = runecraftplan.GateUseTalisman
	}
	if r, failed := s.enter(stage); failed {
		return r
	}
	if stage == runecraftplan.GateWalkToRuins {
		s.atRuins = true
		return runeOK()
	}
	s.where, s.temple, s.atRuins = runecraftplan.RegionTemple, runecraftplan.KeyNature, false
	return runeOK()
}

func (s *simGate) Craft(runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.GateCraft); failed {
		return r
	}
	s.natures += s.packEss
	s.xp += s.packEss * 9
	s.packEss = 0
	s.crafts++
	return runeOK()
}

func (s *simGate) PortalOut(runecraftplan.Step) runeStepResult {
	if r, failed := s.enter(runecraftplan.GatePortalOut); failed {
		return r
	}
	s.where, s.temple = runecraftplan.RegionOutside, ""
	return runeOK()
}

func TestGateLoopRunsTheCycleUntilTheEnd(t *testing.T) {
	s := newSimGate()
	s.tradesLeft = 40
	s.endAfterTrades = 12
	res := runGateLoop(s, func(string, ...any) {}, 5000)
	if !res.Done || s.trades != 12 || s.crafts != 12 || s.natures != 12*25 || s.xp != 12*25*9 {
		t.Fatalf("%+v: %d trades %d crafts %d Natures %d xp", res, s.trades, s.crafts, s.natures, s.xp)
	}
	if s.packEss != 0 || s.where != runecraftplan.RegionOutside {
		t.Fatalf("the round must end outside with an empty pack: %d essence, %v", s.packEss, s.where)
	}
	// The cycle is trade, walk, talisman, craft, portal, and again.
	want := []runecraftplan.GateStage{runecraftplan.GateTrade, runecraftplan.GateWalkToRuins, runecraftplan.GateUseTalisman, runecraftplan.GateCraft, runecraftplan.GatePortalOut}
	for i, st := range want {
		if s.order[i] != st {
			t.Fatalf("stage %d is %s, want %s (%v)", i, s.order[i], st, s.order[:8])
		}
	}
}

// The essence a carrier brought is crafted even when the round ends: the end rule is only looked at with an empty pack.
func TestGateLoopCraftsWhatIsHeldBeforeItEnds(t *testing.T) {
	s := newSimGate()
	s.tradesLeft = 40
	s.endAfterTrades = 1 // the end is declared by the very trade that brings the essence
	res := runGateLoop(s, func(string, ...any) {}, 5000)
	if !res.Done || s.natures != 25 || s.packEss != 0 || s.crafts != 1 {
		t.Fatalf("%+v: %d Natures, %d essence left, %d crafts", res, s.natures, s.packEss, s.crafts)
	}
}

// Waiting for a carrier is not a stall: the same stage with the same world, many times, is what a quiet gate looks like.
func TestGateLoopWaitingForCarriersIsNotAStall(t *testing.T) {
	s := newSimGate()
	s.tradesLeft = 0
	waits := 0
	w := &waitingGate{simGate: s, waits: 50}
	res := runGateLoop(w, func(string, ...any) { waits++ }, 5000)
	if !res.Done || res.Transient || w.gateCalls < 50 {
		t.Fatalf("%+v after %d gate calls", res, w.gateCalls)
	}
}

type waitingGate struct {
	*simGate
	waits, gateCalls int
}

func (w *waitingGate) Gate() runeStepResult {
	w.gateCalls++
	if w.gateCalls >= w.waits {
		w.simGate.end = "waited long enough"
	}
	return runeOK()
}
func (w *waitingGate) Fingerprint() string { return "unchanged" }

// A stage that fails three times in a row ends the round as a passing failure; one failure is retried.
func TestGateLoopFailures(t *testing.T) {
	s := newSimGate()
	s.failFirst[runecraftplan.GateUseTalisman] = 2
	s.endAfterTrades = 1
	res := runGateLoop(s, func(string, ...any) {}, 5000)
	if !res.Done || s.natures != 25 || s.calls[runecraftplan.GateUseTalisman] != 3 {
		t.Fatalf("two failures then success: %+v, %d calls", res, s.calls[runecraftplan.GateUseTalisman])
	}
	s = newSimGate()
	s.failAlways[runecraftplan.GateCraft] = true
	res = runGateLoop(s, func(string, ...any) {}, 5000)
	if !res.Transient || !strings.Contains(res.Reason, "craft failed 3 times") || s.calls[runecraftplan.GateCraft] != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestGateLoopStuckAndStopped(t *testing.T) {
	s := newSimGate()
	s.talisman = false
	if res := runGateLoop(s, func(string, ...any) {}, 100); !res.Stuck || !strings.Contains(res.Reason, "no Nature talisman") {
		t.Fatalf("%+v", res)
	}
	s = newSimGate()
	s.members = false
	if res := runGateLoop(s, func(string, ...any) {}, 100); !res.Stuck || !strings.Contains(res.Reason, "not a members world") {
		t.Fatalf("%+v", res)
	}
	s = newSimGate()
	s.tradesLeft = 100
	s.stopAfter = 7
	if res := runGateLoop(s, func(string, ...any) {}, 1000); !res.Stopped {
		t.Fatalf("%+v", res)
	}
	if res := runGateLoop(newSimGate(), func(string, ...any) {}, 3); !res.Transient || !strings.Contains(res.Reason, "3 steps without finishing") {
		t.Fatalf("%+v", res)
	}
}

// A restart in the temple with the essence in the pack crafts it and leaves; a restart in the temple with none just leaves.
func TestGateLoopResumesInsideTheTemple(t *testing.T) {
	s := newSimGate()
	s.where, s.temple, s.packEss = runecraftplan.RegionTemple, runecraftplan.KeyNature, 25
	s.end = "restart"
	res := runGateLoop(s, func(string, ...any) {}, 100)
	if !res.Done || s.natures != 25 || s.where != runecraftplan.RegionOutside {
		t.Fatalf("%+v: %d Natures, %v", res, s.natures, s.where)
	}
	s = newSimGate()
	s.where, s.temple = runecraftplan.RegionTemple, runecraftplan.KeyNature
	s.end = "restart"
	if res := runGateLoop(s, func(string, ...any) {}, 100); !res.Done || s.crafts != 0 || s.calls[runecraftplan.GatePortalOut] != 1 {
		t.Fatalf("%+v: %d crafts", res, s.crafts)
	}
}

// ---- the live world ----

func newNatureWorld(t *testing.T, r *masterRig, mod func(w *masterNatureWorld)) *masterNatureWorld {
	t.Helper()
	oldWait := natureWait
	natureWait = 150 * time.Millisecond
	t.Cleanup(func() { natureWait = oldWait })
	r.b.set.Rune.On, r.b.set.Rune.Manual = true, true
	rw := newRuneWorld(t, r, nil)
	rw.post, rw.postKnown = tile.Tile{X: 2809, Z: 3441}, true
	w := &masterNatureWorld{masterRuneWorld: rw, watch: runecraftrunner.NewGateWatcher(rw.d), strangerSaid: map[string]time.Time{}, carrierSeen: time.Now()}
	if mod != nil {
		mod(w)
	}
	return w
}

// withRuneBank points runecraftrunner's bank door at the rig's bank simulator.
func withRuneBank(t *testing.T, r *masterRig) {
	t.Helper()
	old := runecraftrunner.OpenBank
	t.Cleanup(func() { runecraftrunner.OpenBank = old })
	runecraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) {
		r.sim.OpenBank(r.sim.Bank())
		return actions.NewBank(r.sim, actionsim.Components()), true
	}
}

// stackable makes the simulated server stack these objects for the test.
func stackable(t *testing.T, ids ...int32) {
	t.Helper()
	for _, id := range ids {
		masterSimStackable[id] = true
	}
	t.Cleanup(func() {
		for _, id := range ids {
			delete(masterSimStackable, id)
		}
	})
}

// newNatureRig is newSimRig on a server that stacks Nature runes and coins.
func newNatureRig(t *testing.T, pack, bank []actionsim.Stack, stock map[string]int) *masterRig {
	t.Helper()
	stackable(t, natRune, coinsObj)
	return newSimRig(t, pack, bank, stock)
}

const (
	natTal   = 1462
	natRune  = 561
	coinsObj = 995
)

// The gate pack: the talisman is drawn, the fare is exactly 100 coins, the Natures held stay in the pack, the rest is banked.
func TestNatureProvisionLoadsTheGatePack(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 4200}, {ID: coinsObj, Count: 12}, {ID: 1607, Count: 5}},
		[]actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsObj, Count: 80000}}, map[string]int{"Nature talisman": 7})
	withRuneBank(t, r)
	w := newNatureWorld(t, r, nil)
	if why, ok := w.provision(); !ok {
		t.Fatalf("%s\n%v", why, r.logs)
	}
	p := r.sim.Pack()
	if simCount(p, natTal) != 1 || simCount(p, natRune) != 4200 || simCount(p, coinsObj) != runecraftplan.GateFare || len(p) != 3 {
		t.Fatalf("the gate pack: %v", p)
	}
	if b := r.sim.Bank(); simCount(b, 1607) != 5 || simCount(b, coinsObj) != 80000+12-runecraftplan.GateFare {
		t.Fatalf("bank %v", b)
	}
	if !r.logged("provisioned at mule's post: the Nature talisman is held, 100 coins for the fare, 4,200 Natures kept") {
		t.Fatalf("log: %v", r.logs)
	}
}

// A bank with no talisman: the mule is asked for one, the next visit draws it.
func TestNatureProvisionAsksTheMuleForTheTalisman(t *testing.T) {
	r := newNatureRig(t, nil, []actionsim.Stack{{ID: coinsObj, Count: 5000}}, map[string]int{"Nature talisman": 7})
	withRuneBank(t, r)
	var items []fleetplan.ItemCount
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		items = it
		r.sim.SetBank([]actionsim.Stack{{ID: coinsObj, Count: 5000}, {ID: natTal, Count: 1}})
		return fleetrunner.KitResult{Got: map[string]int{"Nature talisman": 1}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	w := newNatureWorld(t, r, nil)
	if why, ok := w.provision(); !ok {
		t.Fatalf("%s\n%v", why, r.logs)
	}
	if len(items) != 1 || items[0].Item != "Nature talisman" || items[0].Count != 1 {
		t.Fatalf("asked %v", items)
	}
	if p := r.sim.Pack(); simCount(p, natTal) != 1 || simCount(p, coinsObj) != runecraftplan.GateFare {
		t.Fatalf("pack %v", p)
	}
}

// A mule with no talisman to give: the round does not start.
func TestNatureProvisionWithoutATalismanFails(t *testing.T) {
	r := newNatureRig(t, nil, []actionsim.Stack{{ID: coinsObj, Count: 5000}}, map[string]int{})
	withRuneBank(t, r)
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{"Nature talisman": "declined: none in the bank"}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	w := newNatureWorld(t, r, nil)
	why, ok := w.provision()
	if ok || !strings.Contains(why, "Nature talisman") {
		t.Fatalf("%v %s", ok, why)
	}
}

// The hand-over draws the surplus from the bank and trades it; the keep stays banked.
func TestNatureHandOverDrawsOnlyTheSurplus(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 7300}}, nil, nil)
	var drawn int
	var names []string
	masterDeliver = func(d *fleetrunner.Deps, rd *donorrunner.Deps, mule string, post tile.Tile, rows, slots int, items []string) runerunner.Delivery {
		names = items
		drawn = simCount(r.sim.Pack(), natRune)
		r.sim.Remove(natRune, drawn)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: 1}, Delivered: map[string]int{"Nature rune": drawn}}
	}
	handed, why := r.b.handOverNatures(r.ctx, r.b.deps(r.ctx), tile.Tile{X: 2809, Z: 3441}, 3300)
	if handed != 3300 || why != "" || drawn != 3300 || len(names) != 1 || names[0] != "Nature rune" {
		t.Fatalf("handed %d (%q), drew %d, items %v\n%v", handed, why, drawn, names, r.logs)
	}
	if b := r.sim.Bank(); simCount(b, natRune) != 4000 {
		t.Fatalf("the keep of 4,000 stays in the bank: %v", b)
	}
	if !r.logged("HANDOVER nature mule=mule") {
		t.Fatalf("log: %v", r.logs)
	}
	// The mule away: nothing is drawn.
	r2 := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 7300}}, nil, nil)
	r2.ctx.Bus = mulebus.NewBus()
	if handed, why := r2.b.handOverNatures(r2.ctx, r2.b.deps(r2.ctx), tile.Tile{X: 2809, Z: 3441}, 3300); handed != 0 || !strings.Contains(why, "not here") {
		t.Fatalf("handed %d (%q)", handed, why)
	}
}

// Gate: a carrier's request at the gate becomes a trade, counted in the round and the ledger and logged with the carrier's name.
func TestNatureGateReceivesATradeAndCountsIt(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	r.sim.ShowPlayers("Acct 1")
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.set.Runners = []string{"acct_1"} })
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}}, AcceptOffer: true, AcceptConfirm: true}
	r.sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			r.sim.OpenTrade(partner)
		}
	}
	r.ctx.Chat.Record("Acct 1:tradereq:")
	w.watch = runecraftrunner.NewGateWatcher(w.d)
	r.ctx.Chat.Record("Acct 1:tradereq:")
	if res := w.Gate(); !res.OK {
		t.Fatalf("%+v\n%v", res, r.logs)
	}
	if w.trades != 1 || w.essence != 25 || r.b.ledger.Rune.Gate.Trades != 1 || r.b.ledger.Rune.Gate.EssenceTaken != 25 || w.lastTrade.IsZero() {
		t.Fatalf("round %d trades %d essence; ledger %+v", w.trades, w.essence, r.b.ledger.Rune.Gate)
	}
	if !r.logged("gate: trade 1 from Acct 1: 25 essence") {
		t.Fatalf("log: %v", r.logs)
	}
	// The gate was told to the carriers: open.
	g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now(), 0)
	if !g.Heard || g.State != mulebus.GateOpen {
		t.Fatalf("gate view %+v", g)
	}
}

// movesSent is how many walk packets the session has sent (MOVE_GAMECLICK and MOVE_MINIMAPCLICK: each of them closes an open trade
// screen for both players).
func movesSent(sim *actionsim.Session) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) > 0 && (int(b[0]) == clientprot.MOVE_GAMECLICK || int(b[0]) == clientprot.MOVE_MINIMAPCLICK) {
			n++
		}
	}
	return n
}

// The crafter off his stand is walked onto it with ONE click that does not block, and never while a trade screen is open: a walk packet,
// like a request, closes an open screen for both players. (The walk used to be sent first thing, before any request was read, and
// blocked up to four seconds on its arrival.)
func TestNatureGateIdleWalksOntoTheStandOnceAndNeverUnderAScreen(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.stand, w.haveStand = runecraftplan.GateStandTile, true })
	if w.onStand() {
		t.Fatal("on the stand at the exit tile")
	}
	started := time.Now()
	w.gateIdle()
	if took := time.Since(started); took > 100*time.Millisecond {
		t.Fatalf("the idle walk blocked for %s", took)
	}
	w.gateIdle()
	w.gateIdle()
	if n := movesSent(r.sim); n != 1 {
		t.Fatalf("%d walk packets for three idle polls, want 1", n)
	}
	// Two ticks later the click is sent again if he is still off the stand.
	w.standClick = time.Now().Add(-2 * standClickEvery)
	w.gateIdle()
	if n := movesSent(r.sim); n != 2 {
		t.Fatalf("%d walk packets, want 2", n)
	}
	// On the stand: nothing is sent.
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w.standClick = time.Time{}
	w.gateIdle()
	if n := movesSent(r.sim); n != 2 || !w.onStand() {
		t.Fatalf("%d walk packets on the stand, on stand %v", n, w.onStand())
	}
	// Off the stand again but a trade screen is open: no walk.
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	r.sim.OpenTrade(&actionsim.Trade{Partner: "Acct 1"})
	w.standClick = time.Time{}
	w.gateIdle()
	if n := movesSent(r.sim); n != 2 {
		t.Fatalf("a walk was sent under an open screen: %d packets", n)
	}
	// Away from the gate altogether (a respawn): Gate walks back, the idle poll does not.
	r.sim.StandAt(3222, 3218)
	w.gateIdle()
	if n := movesSent(r.sim); n != 2 {
		t.Fatalf("the idle poll walked from %v: %d packets", runecraftrunner.Here(w.d), n)
	}
}

// A trade writes the ledger in the next idle poll, not between the trade and the talisman, and never while a request waits.
func TestNatureGateWritesTheLedgerWhenIdle(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.stand, w.haveStand = runecraftplan.GateStandTile, true })
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25, Ticks: runecraftrunner.GateTicks{Ask: 10, Answer: 11, Open: 13, Close: 18}}
	}
	t.Cleanup(func() { masterGateStep = runecraftrunner.GateStep })
	if res := w.Gate(); !res.OK || w.trades != 1 {
		t.Fatalf("%+v", res)
	}
	if !w.ledgerDirty || r.b.ledger.Rune.Gate.Trades != 1 {
		t.Fatalf("dirty %v, ledger trades %d", w.ledgerDirty, r.b.ledger.Rune.Gate.Trades)
	}
	if !r.logged("ticks ask 10 answer 11 open 13 close 18") {
		t.Fatalf("the trade line carries the handshake's ticks: %v", r.logs)
	}
	w.gateIdle()
	if w.ledgerDirty {
		t.Fatal("the idle poll should have written the ledger")
	}
	// A pending request holds the write back: the handshake comes first.
	w.ledgerDirty = true
	w.watch = runecraftrunner.NewGateWatcher(w.d)
	r.ctx.Chat.Record("Acct 2:tradereq:")
	masterGateStep = runecraftrunner.GateStep
	runecraftrunner.GateStep(w.d, w.watch, runecraftrunner.GateOptions{Wait: 60 * time.Millisecond, OnStand: func() bool { return false }, Idle: w.gateIdle})
	if w.watch.Pending() != 1 || !w.ledgerDirty {
		t.Fatalf("pending %d, dirty %v: the ledger was written under a waiting request", w.watch.Pending(), w.ledgerDirty)
	}
}

// A screen that closed before anybody accepted is not a failed trade: it is counted apart, logged with the handshake's ticks, and the
// stage goes on.
func TestNatureGateAClosedScreenIsNotAFailedTrade(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, nil)
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateClosed, Partner: "Acct 1", Reason: "the screen closed before an accept (the game said: Other player declined trade.)"}
	}
	t.Cleanup(func() { masterGateStep = runecraftrunner.GateStep })
	for i := 0; i < 2*natureTradeFails; i++ {
		if res := w.Gate(); !res.OK {
			t.Fatalf("close %d: %+v", i+1, res)
		}
	}
	if w.failedTrades != 0 || w.closedTrades != 2*natureTradeFails || w.trades != 0 {
		t.Fatalf("failed %d, closed %d, trades %d", w.failedTrades, w.closedTrades, w.trades)
	}
	if !r.logged("trade screen with Acct 1 closed before it was accepted") {
		t.Fatalf("log: %v", r.logs)
	}
}

// A stranger's request is ignored and said once.
func TestNatureGateIgnoresAStrangerAndSaysSoOnce(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	r.sim.ShowPlayers("Bob")
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.set.Runners = []string{"acct_1"} })
	r.ctx.Chat.Record("Bob:tradereq:")
	for i := 0; i < 2; i++ {
		r.ctx.Chat.Record("Bob:tradereq:")
		if res := w.Gate(); !res.OK {
			t.Fatalf("%+v", res)
		}
	}
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, "trade request from Bob, who is not one of the carriers") {
			n++
		}
	}
	if n != 1 || w.trades != 0 {
		t.Fatalf("said %d times, %d trades\n%v", n, w.trades, r.logs)
	}
}

// Trades that fail are counted: one is only logged, the sixth in a row fails the stage; a trade that works clears the count; a
// refusal (a stranger's, a bad offer) is the other side's mistake and counts for nothing.
func TestNatureGateFailedTradesAreCounted(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, nil)
	var next runecraftrunner.GateResult
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return next
	}
	t.Cleanup(func() { masterGateStep = runecraftrunner.GateStep })
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "the trade never closed"}
	for i := 1; i < natureTradeFails; i++ {
		if res := w.Gate(); !res.OK || w.failedTrades != i {
			t.Fatalf("failure %d: %+v (%d counted)", i, res, w.failedTrades)
		}
	}
	// A refusal changes nothing; a good trade resets.
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateRefused, Partner: "Acct 2", Reason: "the offer holds something that is not Rune essence"}
	if res := w.Gate(); !res.OK || w.failedTrades != natureTradeFails-1 {
		t.Fatalf("%+v (%d counted)", res, w.failedTrades)
	}
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25}
	if res := w.Gate(); !res.OK || w.failedTrades != 0 {
		t.Fatalf("%+v (%d counted)", res, w.failedTrades)
	}
	next = runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "the trade never closed"}
	var last runeStepResult
	for i := 0; i < natureTradeFails; i++ {
		last = w.Gate()
	}
	if last.OK || !strings.Contains(last.Why, "trades in a row failed, the last with Acct 1") {
		t.Fatalf("%+v %v", last, r.logs)
	}
}

// A courier's trade is counted apart from the essence trades, in the round and the ledger.
func TestNatureGateCountsACourier(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, nil)
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateGave, Partner: "Acct 2", Natures: 2300}
	}
	t.Cleanup(func() { masterGateStep = runecraftrunner.GateStep })
	if res := w.Gate(); !res.OK || w.courierTrades != 1 || w.courierNature != 2300 || r.b.ledger.Rune.Gate.CourierNatures != 2300 || w.trades != 0 {
		t.Fatalf("%+v: %d courier trades, %d Natures", res, w.courierTrades, w.courierNature)
	}
	if !r.logged("courier Acct 2 took 2300 Nature runes") {
		t.Fatalf("log: %v", r.logs)
	}
}

// Out of the gate's reach the stage walks back.
func TestNatureGateWalksBackWhenAwayFromTheGate(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}}, nil, nil)
	r.sim.StandAt(3222, 3218) // Lumbridge
	w := newNatureWorld(t, r, nil)
	var walked tile.Tile
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool { walked = dest; return true }
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	if res := w.Gate(); !res.OK {
		t.Fatalf("%+v", res)
	}
	if walked != runecraftplan.Nature.Exit {
		t.Fatalf("walked to %v, want the exit tile", walked)
	}
}

// Every walk on the island goes round the spiders: to the island, from it, but not inside a temple.
func TestKaramjaWalksUseTheCorridor(t *testing.T) {
	r := newNatureRig(t, nil, nil, nil)
	var plain, island []tile.Tile
	masterWalkTo = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		plain = append(plain, dest)
		return true
	}
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, _ int, _ time.Duration) bool {
		island = append(island, dest)
		return true
	}
	t.Cleanup(func() { masterWalkKaramja = runecraftrunner.WalkKaramja })
	w := newRuneWorld(t, r, nil)
	r.sim.StandAt(2809, 3441) // the mainland
	w.walk(runecraftplan.Nature.Exit, 0, time.Minute)
	w.walk(tile.Tile{X: 2400, Z: 4838}, 0, time.Minute) // inside the temple
	r.sim.StandAt(2865, 3022)                           // on the island
	w.walk(tile.Tile{X: 2809, Z: 3441}, 3, time.Minute)
	w.walk(tile.Tile{X: 2867, Z: 3021}, 0, time.Minute)
	if len(island) != 3 || len(plain) != 1 || plain[0].X != 2400 {
		t.Fatalf("island walks %v, plain walks %v", island, plain)
	}
}

// The levelling round's snapshot reads the gate: at the gate, the Natures held, the carriers at work.
func TestRuneSnapshotReadsTheGate(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 321}}, nil, nil)
	r.b.set.Rune.On = true
	r.sim.StandAt(runecraftplan.Nature.Exit.X+1, runecraftplan.Nature.Exit.Z)
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct1", "main", tile.Tile{}, mulebus.CarrierWaiting, 25))
	r.ctx.Bus.Publish(mulebus.CarrierMessage("acct2", "main", tile.Tile{}, mulebus.CarrierTransit, 25))
	var snap mastercraftplan.Snapshot
	r.b.runeSnapshot(r.ctx, &snap)
	if !snap.Rune.AtGate || snap.Rune.NatureHeld != 321 || snap.Rune.Carriers != 2 {
		t.Fatalf("%+v", snap.Rune)
	}
}

func TestEndReasonOfTheGate(t *testing.T) {
	r := newNatureRig(t, nil, nil, nil)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) {
		w.started = time.Now().Add(-9 * time.Hour)
		w.lastTrade = time.Now()
		w.carrierSeen = time.Now()
	})
	if why := w.endReason(); !strings.Contains(why, "craftRuneMaxHours") {
		t.Fatalf("a round over its time: %q", why)
	}
	w.started = time.Now().Add(-time.Hour)
	if why := w.endReason(); why != "" {
		t.Fatalf("a round with essence arriving: %q", why)
	}
	w.set.Manual = false
	if why := w.endReason(); !strings.Contains(why, "craftRuneManual and craftRuneCampaign are off") {
		t.Fatalf("manual off: %q", why)
	}
	w.set.Manual = true
	w.died = true
	if why := w.endReason(); why != "the account died" {
		t.Fatalf("%q", why)
	}
}

// The gate message carries the Natures a courier may take.
func TestNaturePublishesTheGate(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 6300}}, nil, nil)
	r.sim.StandAt(runecraftplan.Nature.Exit.X, runecraftplan.Nature.Exit.Z)
	w := newNatureWorld(t, r, nil)
	w.publish(mulebus.GateOpen, 0)
	g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now(), 0)
	if !g.Heard || g.State != mulebus.GateOpen || g.Surplus != 2300 || g.World != 1 {
		t.Fatalf("%+v", g)
	}
	w.set.Courier = false
	w.pubAt = time.Time{}
	w.publish(mulebus.GateOpen, 0)
	if g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now(), 0); g.Surplus != 0 {
		t.Fatalf("no courier, no surplus: %+v", g)
	}
	// The same state is not repeated inside the heartbeat.
	n := len(r.ctx.Bus.Since(time.Now().Add(-time.Minute)))
	w.publish(mulebus.GateOpen, 0)
	if len(r.ctx.Bus.Since(time.Now().Add(-time.Minute))) != n {
		t.Fatal("a heartbeat inside the interval should not be republished")
	}
}

// The gate is kept on the bus while the round is busy inside one long step (the walk to the gate): a carrier believes a gate
// message for 15 seconds, and a walk takes ten minutes.
func TestNatureBeatKeepsTheGateHeardThroughALongStep(t *testing.T) {
	old := gateBeatEvery
	gateBeatEvery = 15 * time.Millisecond
	t.Cleanup(func() { gateBeatEvery = old })
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 6300}}, nil, nil)
	r.sim.StandAt(2800, 3100)
	w := newNatureWorld(t, r, nil)
	stop := w.startBeat()
	count := func() int {
		n := 0
		for _, m := range r.ctx.Bus.Since(time.Now().Add(-time.Minute)) {
			if m.Kind == mulebus.KindRuneGate {
				n++
			}
		}
		return n
	}
	time.Sleep(60 * time.Millisecond)
	if count() != 0 {
		t.Fatal("nothing is repeated before a state has been published")
	}
	w.publish(mulebus.GateOpening, 8*time.Minute)
	time.Sleep(150 * time.Millisecond)
	if n := count(); n < 4 {
		t.Fatalf("%d gate messages in 150ms at one every 15ms", n)
	}
	g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now(), 0)
	if !g.Heard || g.State != mulebus.GateOpening || g.Surplus != 2300 || g.Tile.X != 2800 {
		t.Fatalf("%+v", g)
	}
	// A closed gate is announced once and not repeated; stopping ends the beat.
	w.pubAt = time.Time{}
	w.publish(mulebus.GateClosed, 0)
	time.Sleep(30 * time.Millisecond)
	before := count()
	time.Sleep(80 * time.Millisecond)
	if count() != before {
		t.Fatal("a closed gate is not repeated")
	}
	w.pubAt = time.Time{}
	w.publish(mulebus.GateOpen, 0)
	stop()
	stop() // twice is fine
	time.Sleep(40 * time.Millisecond)
	before = count()
	time.Sleep(80 * time.Millisecond)
	if count() != before {
		t.Fatal("the beat went on after stop")
	}
}
