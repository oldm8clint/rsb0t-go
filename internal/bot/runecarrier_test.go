package bot

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// ---- settings ----

func TestRuneCarrierSchemaIsThePlansKeys(t *testing.T) {
	want := map[string]bool{"homeWorld": true}
	for _, k := range runecraftplan.CarrierKnownKeys() {
		want[k] = true
	}
	for k := range RuneCarrierSettings {
		if !want[k] {
			t.Errorf("the schema has %q, which the plan does not read", k)
		}
		delete(want, k)
	}
	var missing []string
	for k := range want {
		missing = append(missing, k)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the schema lacks %v", missing)
	}
	// Every default the schema declares is the plan's default.
	def := runecraftplan.DefaultCarrierSettings()
	if RuneCarrierSettings[runecraftplan.KeyKitEssence].Default != float64(def.KitEssence) ||
		RuneCarrierSettings[runecraftplan.KeyKitCoins].Default != float64(def.KitCoins) ||
		RuneCarrierSettings[runecraftplan.KeyMinHP].Default != float64(def.MinHP) ||
		RuneCarrierSettings[runecraftplan.KeyShelfFloor].Default != float64(def.ShelfFloor) ||
		RuneCarrierSettings[runecraftplan.KeyWalkRub].Default != def.WalkRub ||
		RuneCarrierSettings[runecraftplan.KeyCarrierMule].Default != def.Mule {
		t.Errorf("a schema default differs from runecraftplan.DefaultCarrierSettings %+v", def)
	}
}

func TestRuneCarrierStartRefusesBadSettings(t *testing.T) {
	for name, raw := range map[string]map[string]interface{}{
		"walk alch on":   {"walkAlch": true},
		"a typo":         {"carrierKitEssences": 100},
		"a bank source":  {"carrierSource": "bank"},
		"a huge kit":     {"carrierKitEssence": 99999999},
		"a bad min hp":   {"carrierMinHP": 3},
		"swarm settings": {"swarmSettings": 7},
	} {
		r := newSimRig(t, nil, nil, nil)
		r.ctx.Settings = raw
		b := &runeCarrierBot{}
		if err := b.OnStart(r.ctx); err == nil || !strings.Contains(err.Error(), "runecarrier") {
			t.Errorf("%s: OnStart = %v", name, err)
		}
	}
	// A good settings object gets as far as the collision pack, which a bare rig has none of.
	r := newSimRig(t, nil, nil, nil)
	r.ctx.Settings = map[string]interface{}{"runecraftCrafter": "MasterCrafter", "homeWorld": 1}
	if err := (&runeCarrierBot{}).OnStart(r.ctx); err == nil || !strings.Contains(err.Error(), "collision pack") {
		t.Errorf("OnStart with good settings = %v", err)
	}
}

// ---- the stage loop over a simulated shift ----

// simCarrier plays a carrier's shift as the live handlers leave it, so the whole shift runs through the real stage machine.
type simCarrier struct {
	place                          runecraftplan.Place
	atPost                         bool
	notes, unnoted, coins, natures int
	kitAsked, over                 bool
	overWhy                        string
	gate                           runecraftplan.GateInfo
	dead                           bool
	resting                        bool
	kitNotes                       int
	failFirst                      map[runecraftplan.CarrierStage]int
	failAlways                     map[runecraftplan.CarrierStage]bool
	calls                          map[runecraftplan.CarrierStage]int
	order                          []runecraftplan.CarrierStage
	noMove                         map[runecraftplan.CarrierStage]bool
	trips, delivered, total        int
	stopAfter                      int
	onDo                           func(s *simCarrier, st runecraftplan.CarrierStage)
}

func newSimCarrier(kit int) *simCarrier {
	return &simCarrier{
		place: runecraftplan.PlaceMainland, atPost: true, kitNotes: kit,
		gate:      runecraftplan.GateInfo{Heard: true, State: "open"},
		failFirst: map[runecraftplan.CarrierStage]int{}, failAlways: map[runecraftplan.CarrierStage]bool{},
		calls: map[runecraftplan.CarrierStage]int{}, noMove: map[runecraftplan.CarrierStage]bool{},
	}
}

func (s *simCarrier) Read() runecraftplan.CarrierSnapshot {
	return runecraftplan.CarrierSnapshot{Dead: s.dead, Resting: s.resting, Place: s.place, AtPost: s.atPost, Notes: s.notes, Unnoted: s.unnoted,
		Natures: s.natures, Coins: s.coins, Gate: s.gate, KitAsked: s.kitAsked, Over: s.over}
}
func (s *simCarrier) Fingerprint() string {
	return fmt.Sprintf("%v|%v|%d|%d|%d|%d|%v|%d", s.place, s.atPost, s.notes, s.unnoted, s.natures, s.coins, s.kitAsked, s.trips)
}
func (s *simCarrier) Prepare() bool       { return true }
func (s *simCarrier) Stopped() bool       { return s.stopAfter > 0 && s.total >= s.stopAfter }
func (s *simCarrier) Pause(time.Duration) {}
func (s *simCarrier) End(why string) {
	if !s.over {
		s.over, s.overWhy = true, why
	}
}
func (s *simCarrier) Ending() bool { return s.over }

func (s *simCarrier) Do(step runecraftplan.CarrierStep) runeStepResult {
	st := step.Stage
	s.total++
	s.calls[st]++
	s.order = append(s.order, st)
	if s.onDo != nil {
		s.onDo(s, st)
	}
	if s.failAlways[st] || s.failFirst[st] >= s.calls[st] {
		return runeFail("scripted failure of " + string(st))
	}
	if s.noMove[st] {
		return runeOK()
	}
	switch st {
	case runecraftplan.CarrierAskKit:
		s.kitAsked = true
		s.notes, s.coins = s.kitNotes, 10000
	case runecraftplan.CarrierCross, runecraftplan.CarrierWalkToStore:
		s.place, s.atPost = runecraftplan.PlaceStore, false
	case runecraftplan.CarrierShelf:
		take := min(25, s.notes)
		s.notes -= take
		s.unnoted += take
		s.coins -= 50
	case runecraftplan.CarrierWalkToGate:
		s.place = runecraftplan.PlaceGate
	case runecraftplan.CarrierTrade:
		s.delivered += s.unnoted
		s.unnoted = 0
		s.trips++
	case runecraftplan.CarrierWait:
	case runecraftplan.CarrierRest:
		s.resting = false
	case runecraftplan.CarrierCourier:
		s.natures += s.gate.Surplus
		s.gate.Surplus = 0
	case runecraftplan.CarrierGoHome:
		s.place, s.atPost = runecraftplan.PlaceMainland, true
	case runecraftplan.CarrierDeliver:
		s.notes, s.unnoted, s.natures = 0, 0, 0
	}
	return runeOK()
}

func runSim(s *simCarrier, max int) (carrierLoopResult, []string) {
	var logs []string
	res := runCarrierShift(s, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }, max)
	return res, logs
}

func TestCarrierLoopRunsAWholeShift(t *testing.T) {
	s := newSimCarrier(250)
	res, logs := runSim(s, 1000)
	if !res.Done || res.Died || res.GaveUp || s.trips != 10 || s.delivered != 250 || s.over {
		t.Fatalf("%+v trips %d delivered %d over %v\n%v", res, s.trips, s.delivered, s.over, logs)
	}
	want := map[runecraftplan.CarrierStage]int{
		runecraftplan.CarrierAskKit: 1, runecraftplan.CarrierCross: 1, runecraftplan.CarrierShelf: 10,
		runecraftplan.CarrierWalkToGate: 10, runecraftplan.CarrierTrade: 10, runecraftplan.CarrierWalkToStore: 9, runecraftplan.CarrierGoHome: 1,
	}
	for st, n := range want {
		if res.Stages[st] != n {
			t.Errorf("stage %s ran %d times, want %d (%v)", st, res.Stages[st], n, res.Stages)
		}
	}
	if s.order[0] != runecraftplan.CarrierAskKit || s.order[1] != runecraftplan.CarrierCross {
		t.Errorf("order %v", s.order[:6])
	}
}

// A kit that does not divide into loads leaves notes: they go to the mule at the post.
func TestCarrierLoopDeliversTheLeftovers(t *testing.T) {
	s := newSimCarrier(260)
	res, _ := runSim(s, 1000)
	if !res.Done || s.trips != 10 || res.Stages[runecraftplan.CarrierDeliver] != 1 || s.notes != 0 {
		t.Fatalf("%+v trips %d notes %d", res, s.trips, s.notes)
	}
}

// The crafter holding Natures beyond his keep: the kit spent, the carrier takes them home with the leftovers.
func TestCarrierLoopCouriers(t *testing.T) {
	s := newSimCarrier(100)
	s.onDo = func(s *simCarrier, st runecraftplan.CarrierStage) {
		if st == runecraftplan.CarrierTrade {
			s.gate.Surplus = 2300
		}
	}
	res, _ := runSim(s, 1000)
	if !res.Done || res.Stages[runecraftplan.CarrierCourier] != 1 || res.Stages[runecraftplan.CarrierDeliver] != 1 {
		t.Fatalf("%+v", res)
	}
}

// A stage that keeps failing ends the shift, and the same stage machine sends the carrier home with the kit instead of leaving it on
// the island.
func TestCarrierLoopAFailingStageEndsTheShiftAndGoesHome(t *testing.T) {
	s := newSimCarrier(250)
	s.failAlways[runecraftplan.CarrierShelf] = true
	res, logs := runSim(s, 1000)
	if !res.Done || s.trips != 0 || !s.over || !strings.Contains(s.overWhy, "shelf failed 3 times") {
		t.Fatalf("%+v over %q\n%v", res, s.overWhy, logs)
	}
	if res.Stages[runecraftplan.CarrierGoHome] != 1 || res.Stages[runecraftplan.CarrierDeliver] != 1 || s.notes != 0 {
		t.Fatalf("home %d deliver %d notes %d: %v", res.Stages[runecraftplan.CarrierGoHome], res.Stages[runecraftplan.CarrierDeliver], s.notes, res.Stages)
	}
}

// Trouble while already ending is the end: the account is left where it stands.
func TestCarrierLoopGivesUpWhenGoingHomeFails(t *testing.T) {
	s := newSimCarrier(250)
	s.failAlways[runecraftplan.CarrierShelf] = true
	s.failAlways[runecraftplan.CarrierGoHome] = true
	res, _ := runSim(s, 1000)
	if !res.GaveUp || res.Done || !strings.Contains(res.Reason, "go-home failed") {
		t.Fatalf("%+v", res)
	}
}

// A passing failure is retried and the shift goes on.
func TestCarrierLoopRetriesAStage(t *testing.T) {
	s := newSimCarrier(50)
	s.failFirst[runecraftplan.CarrierCross] = 2
	res, _ := runSim(s, 1000)
	if !res.Done || s.trips != 2 || s.over || res.Stages[runecraftplan.CarrierCross] != 3 {
		t.Fatalf("%+v trips %d over %v", res, s.trips, s.over)
	}
}

// A stage that "succeeds" and changes nothing is a stall, and ends the shift; the waits are not stalls.
func TestCarrierLoopStallsEndTheShiftButWaitsDoNot(t *testing.T) {
	s := newSimCarrier(250)
	s.noMove[runecraftplan.CarrierCross] = true
	res, _ := runSim(s, 1000)
	if !s.over || !strings.Contains(s.overWhy, "without anything changing") || !res.Done {
		t.Fatalf("%+v over %q", res, s.overWhy)
	}
	// The crafter away for forty waits, then back.
	w := newSimCarrier(25)
	w.onDo = func(s *simCarrier, st runecraftplan.CarrierStage) {
		if st == runecraftplan.CarrierAskKit {
			s.gate.State = "away"
		}
		if st == runecraftplan.CarrierWait && s.calls[st] >= 40 {
			s.gate.State = "open"
		}
	}
	// With the gate away after the ask the carrier crosses (usable is not needed on the island) but waits at the gate.
	w.onDo = func(s *simCarrier, st runecraftplan.CarrierStage) {
		if st == runecraftplan.CarrierWalkToGate {
			s.gate.State = "away"
		}
		if st == runecraftplan.CarrierWait && s.calls[st] >= 40 {
			s.gate.State = "open"
		}
	}
	res, _ = runSim(w, 1000)
	if !res.Done || w.over || w.trips != 1 || res.Stages[runecraftplan.CarrierWait] != 40 {
		t.Fatalf("%+v over %v trips %d waits %d", res, w.over, w.trips, res.Stages[runecraftplan.CarrierWait])
	}
}

func TestCarrierLoopDeathAndStop(t *testing.T) {
	s := newSimCarrier(250)
	s.onDo = func(s *simCarrier, st runecraftplan.CarrierStage) {
		if st == runecraftplan.CarrierWalkToGate {
			s.dead = true
		}
	}
	if res, _ := runSim(s, 1000); !res.Died || res.Done {
		t.Fatalf("%+v", res)
	}
	st := newSimCarrier(250)
	st.stopAfter = 5
	if res, _ := runSim(st, 1000); !res.Stopped {
		t.Fatalf("%+v", res)
	}
	m := newSimCarrier(250)
	if res, _ := runSim(m, 7); !res.GaveUp || !strings.Contains(res.Reason, "7 steps") {
		t.Fatalf("%+v", res)
	}
}

func TestCarrierLoopRestsOnTheIsland(t *testing.T) {
	s := newSimCarrier(25)
	s.onDo = func(s *simCarrier, st runecraftplan.CarrierStage) {
		if st == runecraftplan.CarrierCross {
			s.resting = true
		}
	}
	res, _ := runSim(s, 1000)
	if !res.Done || res.Stages[runecraftplan.CarrierRest] == 0 || s.trips != 1 {
		t.Fatalf("%+v trips %d", res, s.trips)
	}
}

func TestRoleOfAStage(t *testing.T) {
	for st, want := range map[runecraftplan.CarrierStage]mulebus.CarrierRole{
		runecraftplan.CarrierCross: mulebus.CarrierTransit, runecraftplan.CarrierShelf: mulebus.CarrierTransit, runecraftplan.CarrierWalkToGate: mulebus.CarrierTransit,
		runecraftplan.CarrierTrade: mulebus.CarrierWaiting, runecraftplan.CarrierWait: mulebus.CarrierWaiting,
		runecraftplan.CarrierCourier: mulebus.CarrierCourier, runecraftplan.CarrierGoHome: mulebus.CarrierHome, runecraftplan.CarrierDeliver: mulebus.CarrierHome,
	} {
		if got := roleOf(st); got != want || !got.Valid() {
			t.Errorf("roleOf(%s) = %q, want %q", st, got, want)
		}
	}
}

// ---- the live world over the simulated session ----

// carrierRig is a RuneCarrier named "Acct 7" standing at the mule's post, working for "MasterCrafter" on world 1.
type carrierRig struct {
	*masterRig
	b        *runeCarrierBot
	walks    []string
	shelves  []runecraftrunner.ShelfOptions
	kitCalls [][]fleetplan.ItemCount
	traded   int
	rub      string
	lastOpts fleetrunner.KitBundleOptions
}

const (
	crafterName  = "MasterCrafter"
	exitX, exitZ = 2865, 3022
)

func newCarrierRig(t *testing.T, pack []actionsim.Stack, stock map[string]int) *carrierRig {
	t.Helper()
	stackable(t, coinsObj, natRune, runecraftplan.NotedEssenceID)
	r := newSimRig(t, pack, nil, stock)
	r.ctx.Username = "Acct 7"
	r.ctx.Pack = &pathfind.Pack{Members: true}
	c := &carrierRig{masterRig: r, rub: "Ring of dueling(8)"}
	c.b = &runeCarrierBot{set: runecraftplan.DefaultCarrierSettings()}
	c.b.set.Crafter = crafterName
	c.b.set.Mule = "mule"
	oldW, oldS, oldT, oldR, oldP, oldClaims := carrierWalk, carrierShelf, carrierTrade, carrierRubItem, carrierFailPause, carrierClaims
	carrierFailPause = 5 * time.Millisecond
	// The fleet's kit claims are one process-wide record. A rig gets its own, so a claim one case leaves behind (a mule that gave the
	// essence and no coins keeps its carrier's claim for the shift) or one that a test forgets to give back never reaches the next case
	// or test, in whatever order they run.
	carrierClaims = &carrierKitClaims{by: map[string]carrierClaim{}}
	t.Cleanup(func() {
		carrierWalk, carrierShelf, carrierTrade, carrierRubItem, carrierFailPause, carrierClaims = oldW, oldS, oldT, oldR, oldP, oldClaims
	})
	carrierWalk = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		c.walks = append(c.walks, fmt.Sprintf("%d,%d/%d", dest.X, dest.Z, radius))
		r.sim.StandAt(dest.X, dest.Z)
		return true
	}
	carrierRubItem = func(*fleetrunner.Deps) string { return c.rub }
	r.sim.StandAt(2809, 3441)
	return c
}

// openGate publishes the crafter's gate on the bus.
func (c *carrierRig) openGate(state mulebus.GateState, surplus int) {
	c.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(crafterName, tile.Tile{X: exitX, Z: exitZ}, state, 0, surplus), 1))
}

// claimAfterPublish records another carrier's claim on the mule's essence, stamped a second after the rig published the mule's bank.
// carrierKitClaims.others counts only the claims strictly After the bank's UpdatedAt (an older claim is already out of the figure the
// mule published), and a claim stamped time.Now() right after the rig is built can fall in the same clock tick as the publish (the
// clock ticks every few hundred microseconds to a few milliseconds here), where it is not After and was not counted: "claimed by
// other carriers" asked the mule anyway in 8 of 300 runs. The rig's own claims record dies with the test, so there is nothing to give back.
func claimAfterPublish(me string, n int) {
	carrierClaims.claim(me, n, time.Now().Add(time.Second))
}

func (c *carrierRig) world() *carrierWorld {
	d := c.b.deps(c.ctx)
	return &carrierWorld{b: c.b, ctx: c.ctx, d: d, set: c.b.set, started: time.Now(), said: map[string]time.Time{}, delivered: map[string]int{},
		post: tile.Tile{X: 2809, Z: 3441}, postKnown: true}
}

// the kit bundle as the mule serves it: the notes and the coins arrive in the pack.
func (c *carrierRig) serveKit(notes, coins int, ring bool) {
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		c.kitCalls = append(c.kitCalls, it)
		got := map[string]int{}
		for _, x := range it {
			switch x.Item {
			case runecraftplan.EssenceName:
				c.sim.Give(runecraftplan.NotedEssenceID, min(x.Count, notes))
				got[x.Item] = min(x.Count, notes)
			case "Coins":
				c.sim.Give(coinsObj, min(x.Count, coins))
				got[x.Item] = min(x.Count, coins)
			default:
				if ring {
					c.rub = x.Item
					got[x.Item] = 1
				}
			}
		}
		if len(c.kitCalls) == 1 {
			c.lastOpts = o
		}
		return fleetrunner.KitResult{Got: got, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
}

func TestCarrierAskKitAsksTheMuleForNotesCoinsAndARing(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c.rub = ""
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(3000, 10000, true)
	w := c.world()
	if res := w.askKit(); !res.OK {
		t.Fatalf("%+v\n%v", res, c.logs)
	}
	if !w.kitAsked || runecraftrunner.Notes(w.d) != 3000 || runecraftrunner.Coins(w.d) != 10000 {
		t.Fatalf("asked %v notes %d coins %d", w.kitAsked, runecraftrunner.Notes(w.d), runecraftrunner.Coins(w.d))
	}
	if len(c.kitCalls) != 2 || len(c.kitCalls[0]) != 2 || c.kitCalls[0][0].Item != "Rune essence" || c.kitCalls[0][0].Count != 3000 || c.kitCalls[0][1].Item != "Coins" || c.kitCalls[0][1].Count != 10000 {
		t.Fatalf("kit calls %v", c.kitCalls)
	}
	post := scarabMulePresenceOf(c.ctx, "mule").Post
	if o := c.lastOpts; !o.Noted || !o.KeepNotes || o.Bank != post || len(o.Keep) != 0 {
		t.Fatalf("the notes are asked noted and kept as notes, with the pack banked first: %+v", o)
	}
	if len(c.kitCalls[1]) != 1 || c.kitCalls[1][0].Item != "Ring of dueling(8)" {
		t.Fatalf("the ring: %v", c.kitCalls[1])
	}
	if !c.logged("kit: 3,000 noted Rune essence (120 trips), 10,000 coins, run stall item Ring of dueling(8)") {
		t.Fatalf("log: %v", c.logs)
	}
	// The claim stands for the shift: the mule's published bank lags the hand-out. The shift's end gives it back.
	if got := carrierClaims.others("someone else", time.Now(), time.Time{}); got != 3000 {
		t.Fatalf("the claim stands: %d", got)
	}
	c.b.afterShift(c.ctx, carrierOutcome{Trips: 1})
	if got := carrierClaims.others("someone else", time.Now(), time.Time{}); got != 0 {
		t.Fatalf("the claim is given back at the shift's end: %d", got)
	}
}

// The ring already in the pack is kept through the bank visit and not asked for again.
func TestCarrierAskKitKeepsTheRingItHolds(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(3000, 10000, true)
	w := c.world()
	if res := w.askKit(); !res.OK {
		t.Fatalf("%+v", res)
	}
	if len(c.kitCalls) != 1 || len(c.lastOpts.Keep) != 1 || c.lastOpts.Keep[0] != "Ring of dueling(8)" {
		t.Fatalf("calls %d, keep %v", len(c.kitCalls), c.lastOpts.Keep)
	}
}

func TestCarrierAskKitRefusals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stock       map[string]int
		gate        mulebus.GateState
		claimed     int
		unpublished bool
		notes       int
		coins       int
		why         string
	}{
		{"the gate closed", map[string]int{"Rune essence": 20000}, mulebus.GateClosed, 0, false, 3000, 10000, "gate is not open"},
		{"too little essence", map[string]int{"Rune essence": 400}, mulebus.GateOpen, 0, false, 3000, 10000, "under the 500"},
		{"claimed by other carriers", map[string]int{"Rune essence": 3000}, mulebus.GateOpen, 2800, false, 3000, 10000, "after the other carriers' claims"},
		{"the bank not published", nil, mulebus.GateOpen, 0, true, 3000, 10000, "not published"},
		{"the mule gave nothing", map[string]int{"Rune essence": 20000}, mulebus.GateOpen, 0, false, 0, 10000, "gave 0 noted"},
		{"the mule gave no coins", map[string]int{"Rune essence": 20000}, mulebus.GateOpen, 0, false, 3000, 0, "coins"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCarrierRig(t, nil, tc.stock)
			if tc.unpublished {
				c.ctx.Peers = peerbank.New()
			}
			c.openGate(tc.gate, 0)
			c.serveKit(tc.notes, tc.coins, true)
			if tc.claimed > 0 {
				claimAfterPublish("Acct 9", tc.claimed)
			}
			res := c.world().askKit()
			if res.OK || !strings.Contains(res.Why, tc.why) {
				t.Errorf("%+v, want a failure containing %q", res, tc.why)
			}
		})
	}
}

// The mule's absence is a failure without a walk.
func TestCarrierAskKitWithTheMuleAway(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000})
	c.ctx.Bus = mulebus.NewBus()
	c.openGate(mulebus.GateOpen, 0)
	if res := c.world().askKit(); res.OK || !strings.Contains(res.Why, "mule is not here") || len(c.walks) != 0 {
		t.Fatalf("%+v walks %v", res, c.walks)
	}
}

func TestCarrierShelfStageCountsAndRemembersAFullShop(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 3000}, {ID: coinsObj, Count: 10000}}, nil)
	var calls []runecraftrunner.ShelfOptions
	results := []runecraftrunner.ShelfResult{
		{StockBefore: 40, StockAfter: 40, Sold: 25, Income: 0, Bought: 25, Cost: 50, UnnotedAt: 25},
		{StockBefore: 10, StockAfter: 10, Sold: 0, OutOfSpace: true, Bought: 10, Cost: 40, UnnotedAt: 10, Reason: "the shop has no room for the notes"},
		{StockBefore: 40, StockAfter: 15, Sold: 0, Bought: 25, Cost: 50, UnnotedAt: 25},
	}
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		calls = append(calls, o)
		r := results[len(calls)-1]
		c.sim.Give(runecraftplan.EssenceID, r.UnnotedAt)
		return r
	}
	w := c.world()
	if res := w.shelf(); !res.OK || w.paid != 50 {
		t.Fatalf("%+v paid %d", res, w.paid)
	}
	c.sim.Remove(runecraftplan.EssenceID, 25)
	// A visit that bought under a load and said the shop is full: a failure, and the next visit buys only.
	if res := w.shelf(); res.OK || !w.buyOnly {
		t.Fatalf("%+v buyOnly %v", res, w.buyOnly)
	}
	c.sim.Remove(runecraftplan.EssenceID, 100)
	if res := w.shelf(); !res.OK || !calls[2].BuyOnly || calls[0].BuyOnly {
		t.Fatalf("%+v calls %+v", res, calls)
	}
	if calls[0].Floor != runecraftplan.DefaultShelfFloor || calls[0].Want != runecraftplan.CarrierOffer {
		t.Fatalf("shelf options %+v", calls[0])
	}
	if !c.logged("WARNING: Jiminua's shop has no room for the notes") || !c.logged("shelf: Jiminua stock 40 Rune essence") {
		t.Fatalf("log: %v", c.logs)
	}
}

func TestCarrierTradeStage(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	var opts []runecraftrunner.OfferOptions
	var answer runecraftrunner.OfferResult
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		opts = append(opts, o)
		if answer.Done {
			c.sim.Remove(runecraftplan.EssenceID, answer.Moved)
		}
		return answer
	}
	w := c.world()
	answer = runecraftrunner.OfferResult{Done: true, Moved: 25, Requests: 3, Took: 11 * time.Second}
	if res := w.trade(); !res.OK || w.trips != 1 || w.essence != 25 {
		t.Fatalf("%+v trips %d essence %d", res, w.trips, w.essence)
	}
	if o := opts[0]; o.Crafter != crafterName || o.Mode != runecraftrunner.OfferEssence || o.Timeout != carrierTradeWait || o.Absent != carrierTradeAbsent || o.Stand != runecraftplan.GateStandTile {
		t.Fatalf("offer options %+v", o)
	}
	if !c.logged("trip 1: traded 25 essence with MasterCrafter") {
		t.Fatalf("log: %v", c.logs)
	}
	// Waits: out of view, and no screen. Neither fails.
	c.sim.Give(runecraftplan.EssenceID, 25)
	answer = runecraftrunner.OfferResult{NotHere: true, Reason: "MasterCrafter is not in view"}
	if res := w.trade(); !res.OK || w.trips != 1 {
		t.Fatalf("%+v", res)
	}
	answer = runecraftrunner.OfferResult{NoScreen: true, Requests: 120, Took: 3 * time.Minute, Reason: "no trade screen after 3m0s and 120 request(s)"}
	if res := w.trade(); !res.OK || w.tradeTimeouts != 1 {
		t.Fatalf("%+v", res)
	}
	// A screen that opened and failed is a failure.
	answer = runecraftrunner.OfferResult{Reason: "the confirm screen does not match"}
	if res := w.trade(); res.OK || !strings.Contains(res.Why, "confirm screen") || w.tradeFails != 1 {
		t.Fatalf("%+v fails %d", res, w.tradeFails)
	}
}

// A screen that closed before either side accepted (a stray packet from either player closes it for both, runecraftrunner's file comment)
// is nothing that crossed and nobody's failure: no strike, no pause, the stage machine asks again. Three of them in a row used to end the
// shift (live 16 Oct 2026: two shifts of one carrier, the second a walk home with 25 essence nobody could take).
func TestCarrierAClosedScreenIsNotAStrike(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		return runecraftrunner.OfferResult{Closed: true, Requests: 1, Took: 4 * time.Second, Reason: "the trade screen closed before it was accepted (the game said: Other player declined trade.)"}
	}
	w := c.world()
	for i := 0; i < 2*carrierStepFails; i++ {
		if res := w.trade(); !res.OK {
			t.Fatalf("close %d was a failure: %+v", i+1, res)
		}
	}
	if w.tradeFails != 0 || w.tradeCloses != 2*carrierStepFails || w.trips != 0 {
		t.Fatalf("fails %d closes %d trips %d", w.tradeFails, w.tradeCloses, w.trips)
	}
	if !c.logged("the trade screen with MasterCrafter closed before it was accepted") {
		t.Fatalf("log: %v", c.logs)
	}
	// Through the stage machine: twice the failure limit of closes in a row and the shift is still at the gate with its load, not ending.
	c.openGate(mulebus.GateOpen, 0)
	c.sim.StandAt(exitX, exitZ)
	w2 := c.world()
	steps := 0
	res := runCarrierShift(&limitedWorld{carrierWorld: w2, left: 2 * carrierStepFails, steps: &steps}, func(string, ...any) {}, 1000)
	if w2.over != "" || res.GaveUp || !res.Stopped || steps != 2*carrierStepFails || w2.tradeCloses != 2*carrierStepFails {
		t.Fatalf("the shift ended on closed screens: over %q, %+v, %d steps, %d closes", w2.over, res, steps, w2.tradeCloses)
	}
}

// limitedWorld is a carrierWorld that stops the bot after `left` steps.
type limitedWorld struct {
	*carrierWorld
	left  int
	steps *int
}

func (l *limitedWorld) Stopped() bool {
	if l.left <= 0 {
		return true
	}
	l.left--
	*l.steps++
	return false
}

// The heartbeat goes out from a goroutine for the whole shift, whatever the stage is doing: one trade step runs for a minute, and the
// crafter drops a carrier unheard for a minute (live 16 Oct 2026: a carrier waited three minutes for a trade screen as a stranger).
func TestCarrierBeatKeepsTheCarrierHeardThroughALongStep(t *testing.T) {
	old := carrierBeatEvery
	carrierBeatEvery = 10 * time.Millisecond
	t.Cleanup(func() { carrierBeatEvery = old })
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	w := c.world()
	w.heartbeat(mulebus.CarrierWaiting)
	count := func() int {
		n := 0
		for _, m := range c.ctx.Bus.Since(time.Now().Add(-time.Minute)) {
			if m.Kind == mulebus.KindRuneCarrier {
				n++
			}
		}
		return n
	}
	before := count()
	stop := w.startBeat()
	time.Sleep(80 * time.Millisecond)
	// The heartbeat itself is at most one message every mulebus.CarrierEvery, so a long step keeps the carrier heard by republishing it
	// when that is due; here the first message is fresh and the goroutine must not flood the bus.
	if n := count(); n != before {
		t.Fatalf("%d messages after %d: the goroutine republished inside the interval", n, before)
	}
	// Age the last message past the interval: the next look publishes again, in the role the step set.
	w.hbMu.Lock()
	w.pubAt = time.Now().Add(-mulebus.CarrierEvery - time.Second)
	w.hbMu.Unlock()
	deadline := time.Now().Add(time.Second)
	for count() == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if count() != before+1 {
		t.Fatalf("%d messages, want %d", count(), before+1)
	}
	got := mulebus.ReadCarriers(c.ctx.Bus, crafterName, time.Now().Add(time.Millisecond), 0)
	if len(got) != 1 || got[0].Role != mulebus.CarrierWaiting {
		t.Fatalf("%+v", got)
	}
	stop()
	stop()
	n := count()
	w.hbMu.Lock()
	w.pubAt = time.Now().Add(-mulebus.CarrierEvery - time.Second)
	w.hbMu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if count() != n {
		t.Fatal("the beat went on after stop")
	}
}

// The carrier waits beside the stand, not anywhere within three tiles of the exit.
func TestCarrierWalksToTheTileBesideTheStand(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	w := c.world()
	if res := w.walkToGate(); !res.OK || len(c.walks) != 1 || c.walks[0] != "2867,3021/0" {
		t.Fatalf("%+v walks %v", res, c.walks)
	}
}

func TestCarrierPaceLineEveryTenTrips(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: coinsObj, Count: 5000}}, nil)
	w := c.world()
	w.started = time.Now().Add(-time.Hour)
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		return runecraftrunner.OfferResult{Done: true, Moved: 25, Requests: 1}
	}
	w.paid, w.earned = 500, 100
	for i := 0; i < 10; i++ {
		w.trade()
	}
	if !c.logged("pace: 10 trip(s), 250 essence to \"MasterCrafter\" in 1h0m0s: ") {
		t.Fatalf("log: %v", c.logs)
	}
}

func TestPerHourOf(t *testing.T) {
	if perHourOf(250, time.Hour) != 250 || perHourOf(100, 30*time.Minute) != 200 || perHourOf(5, 0) != 0 {
		t.Fatal("perHourOf")
	}
}

func TestCarrierCourierTakesTheNaturesOnce(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: coinsObj, Count: 5000}}, nil)
	w := c.world()
	var modes []runecraftrunner.OfferMode
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		modes = append(modes, o.Mode)
		c.sim.Give(natRune, 2300)
		return runecraftrunner.OfferResult{Done: true, Moved: 2300}
	}
	// Away from the gate: the first call walks there, to the tile beside the crafter's stand.
	if res := w.courier(); !res.OK || len(modes) != 0 || len(c.walks) != 1 || c.walks[0] != "2867,3021/0" {
		t.Fatalf("%+v walks %v", res, c.walks)
	}
	if res := w.courier(); !res.OK || len(modes) != 1 || modes[0] != runecraftrunner.TakeNatures || w.natures != 2300 || !w.courierTried {
		t.Fatalf("%+v modes %v natures %d", res, modes, w.natures)
	}
	c.openGate(mulebus.GateOpen, 2300)
	if g := w.gate(); g.Surplus != 0 {
		t.Fatalf("a courier that holds Natures asks for no more: %+v", g)
	}
	if !c.logged("COURIER: traded 2300 Nature runes with MasterCrafter") {
		t.Fatalf("log: %v", c.logs)
	}
}

func TestCarrierDeliverHandsTheLeftoversToTheMule(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 10}, {ID: natRune, Count: 2300}, {ID: coinsObj, Count: 4000}}, nil)
	var items []string
	var rows, slots int
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, mule string, post tile.Tile, r, s int, it []string) runerunner.Delivery {
		items, rows, slots = it, r, s
		c.sim.Remove(runecraftplan.NotedEssenceID, 10)
		n := simCount(c.sim.Pack(), natRune)
		c.sim.Remove(natRune, n)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: 2}, Delivered: map[string]int{"Rune essence": 10, "Nature rune": n}}
	}
	w := c.world()
	if res := w.deliver(); !res.OK {
		t.Fatalf("%+v\n%v", res, c.logs)
	}
	// carrierKeepNatures (200 by default): the share stays in the carrier's own bank, the rest of the 2,300 crosses.
	if strings.Join(items, ",") != "Rune essence,Nature rune" || rows != 1 || slots != 2 || w.delivered["Rune essence"] != 10 || w.delivered["Nature rune"] != 2100 {
		t.Fatalf("items %v rows %d slots %d delivered %v", items, rows, slots, w.delivered)
	}
	if b := c.sim.Bank(); simCount(b, natRune) != 200 || w.kept != 200 {
		t.Fatalf("the share is kept in the bank: %v (kept %d)", b, w.kept)
	}
	if simCount(c.sim.Pack(), coinsObj) != 4000 {
		t.Fatalf("the coins stay with the account: %v", c.sim.Pack())
	}
	// A mule that took nothing: a failure that says what is left.
	c.sim.Give(runecraftplan.NotedEssenceID, 10)
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		return runerunner.Delivery{Result: donorrunner.Result{GaveUp: true, Reason: "the mule never met me"}}
	}
	if res := w.deliver(); res.OK || !strings.Contains(res.Why, "10 essence") || !strings.Contains(res.Why, "never met") {
		t.Fatalf("%+v", res)
	}
	// The mule away: no trade is tried.
	c.ctx.Bus = mulebus.NewBus()
	if res := w.deliver(); res.OK || !strings.Contains(res.Why, "not here") {
		t.Fatalf("%+v", res)
	}
}

func TestCarrierDeadAccountEndsTheShift(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 100}, {ID: coinsObj, Count: 4000}}, nil)
	w := c.world()
	msg := packet.NewWriter(64)
	msg.PJStr("Oh dear you are dead!")
	c.sim.State().Handle(serverprot.MESSAGE_GAME, msg.Data[:msg.Pos])
	if !w.Prepare() || !w.died || !w.Read().Dead {
		t.Fatalf("died %v read %+v", w.died, w.Read())
	}
	if !c.logged("WARNING: the account DIED") {
		t.Fatalf("log: %v", c.logs)
	}
}

func TestCarrierHeartbeatPublishesTheRole(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}}, nil)
	w := c.world()
	w.heartbeat(mulebus.CarrierWaiting)
	views := mulebus.ReadCarriers(c.ctx.Bus, crafterName, time.Now(), 0)
	if len(views) != 1 || views[0].Name != "acct 7" && views[0].Name != "Acct 7" || views[0].Role != mulebus.CarrierWaiting || views[0].Held != 25 {
		t.Fatalf("%+v", views)
	}
	// The same role again inside the interval is not published again; a new role is, at once.
	n := len(c.ctx.Bus.Since(time.Now().Add(-time.Minute)))
	w.heartbeat(mulebus.CarrierWaiting)
	if len(c.ctx.Bus.Since(time.Now().Add(-time.Minute))) != n {
		t.Fatal("a repeat inside the interval was published")
	}
	w.heartbeat(mulebus.CarrierCourier)
	if got := mulebus.ReadCarriers(c.ctx.Bus, crafterName, time.Now(), 0); len(got) != 1 || got[0].Role != mulebus.CarrierCourier {
		t.Fatalf("%+v", got)
	}
	// One carrier at work is not the crafter's "others".
	if n := c.b.otherCarriers(c.ctx, time.Now()); n != 0 {
		t.Fatalf("this carrier is not its own other: %d", n)
	}
}

// The whole shift through the live world and the real stage machine: the stubs stand in for the mule, the shop, the walks and the
// crafter; everything between is the carrier's own reading and deciding.
func TestCarrierLiveShiftEndToEnd(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: 1607, Count: 3}}, map[string]int{"Rune essence": 20000, "Ring of dueling(8)": 35})
	c.rub = ""
	c.b.set.KitEssence = 250
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(250, 10000, true)
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		take := min(25, runecraftrunner.Notes(d))
		c.sim.Remove(runecraftplan.NotedEssenceID, take)
		c.sim.Give(runecraftplan.EssenceID, take)
		c.sim.Remove(coinsObj, 50)
		return runecraftrunner.ShelfResult{StockBefore: 40, StockAfter: 40, Sold: take, Bought: take, Cost: 50, UnnotedAt: take}
	}
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		if here := runecraftrunner.Here(d); !runecraftplan.NearGate(here) {
			t.Errorf("a trade away from the gate: %v", here)
		}
		n := runecraftrunner.Essence(d)
		c.sim.Remove(runecraftplan.EssenceID, n)
		c.traded += n
		return runecraftrunner.OfferResult{Done: true, Moved: n, Requests: 2, Took: 9 * time.Second}
	}
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		t.Errorf("nothing is left to deliver after a kit of whole loads")
		return runerunner.Delivery{}
	}
	out := c.b.liveShift(c.ctx, "test", false)
	if out.Trips != 10 || out.Essence != 250 || c.traded != 250 || out.Failed || out.Died || out.Paid != 500 {
		t.Fatalf("%+v traded %d\n%v", out, c.traded, c.logs)
	}
	// The walks: to the post's bank is the kit ask's own; then the store, the gate and back, ten times, and home.
	if len(c.walks) != 1+1+10+9+1 || !strings.HasPrefix(c.walks[0], "2809,344") || c.walks[1] != "2767,3123/2" || !strings.HasPrefix(c.walks[len(c.walks)-1], "2809,344") {
		t.Fatalf("walks %v", c.walks)
	}
	if !c.logged("kit: 250 noted Rune essence (10 trips)") || !c.logged("trip 10: traded 25 essence") || !c.logged("home at the mule's post") {
		t.Fatalf("log: %v", c.logs)
	}
	// The carrier told the crafter what it was doing along the way.
	if got := mulebus.ReadCarriers(c.ctx.Bus, crafterName, time.Now(), 0); len(got) != 1 || got[0].Role != mulebus.CarrierHome {
		t.Fatalf("carrier messages %+v", got)
	}
}

// The shift ends early when the gate closes on the island: the kit comes home and the notes go to the mule.
func TestCarrierLiveShiftGateClosesOnTheIsland(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000})
	c.b.set.KitEssence = 250
	c.b.set.WalkRub = false
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(250, 10000, false)
	trades := 0
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		c.sim.Remove(runecraftplan.NotedEssenceID, 25)
		c.sim.Give(runecraftplan.EssenceID, 25)
		return runecraftrunner.ShelfResult{Sold: 25, Bought: 25, UnnotedAt: 25}
	}
	carrierTrade = func(d *fleetrunner.Deps, o runecraftrunner.OfferOptions) runecraftrunner.OfferResult {
		n := runecraftrunner.Essence(d)
		c.sim.Remove(runecraftplan.EssenceID, n)
		trades++
		if trades == 3 {
			c.openGate(mulebus.GateClosed, 0)
		}
		return runecraftrunner.OfferResult{Done: true, Moved: n}
	}
	var delivered int
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		delivered = runecraftrunner.Notes(c.b.deps(c.ctx))
		c.sim.Remove(runecraftplan.NotedEssenceID, delivered)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: map[string]int{"Rune essence": delivered}}
	}
	out := c.b.liveShift(c.ctx, "test", false)
	if out.Trips != 3 || out.Failed || delivered != 250-75 || out.Delivered["Rune essence"] != 175 {
		t.Fatalf("%+v delivered %d\n%v", out, delivered, c.logs)
	}
}

// A shift whose stages keep failing is sent home, not left on the island, and counts as a failed shift with a backoff.
func TestCarrierLiveShiftFailsAndBacksOff(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000})
	c.b.set.KitEssence = 250
	c.b.set.WalkRub = false
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(250, 10000, false)
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		return runecraftrunner.ShelfResult{Reason: "opening Jiminua's shop: not found"}
	}
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		c.sim.Remove(runecraftplan.NotedEssenceID, 250)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: map[string]int{"Rune essence": 250}}
	}
	out := c.b.liveShift(c.ctx, "test", false)
	if out.Trips != 0 || !out.Failed || !strings.Contains(out.Why, "shelf failed 3 times") || out.Delivered["Rune essence"] != 250 {
		t.Fatalf("%+v\n%v", out, c.logs)
	}
	c.b.afterShift(c.ctx, out)
	if c.b.fails != 1 || c.b.backoffUntil.Before(time.Now().Add(4*time.Minute)) || !c.logged("backing off until") {
		t.Fatalf("fails %d until %v\n%v", c.b.fails, c.b.backoffUntil, c.logs)
	}
	// The next failure doubles it; a shift with a trip clears it; a stop and a death are not alike.
	c.b.afterShift(c.ctx, out)
	if c.b.fails != 2 || c.b.backoffUntil.Before(time.Now().Add(9*time.Minute)) {
		t.Fatalf("fails %d until %v", c.b.fails, c.b.backoffUntil)
	}
	c.b.afterShift(c.ctx, carrierOutcome{Trips: 4, Essence: 100})
	if c.b.fails != 0 || !c.b.backoffUntil.IsZero() || c.b.trips != 4 {
		t.Fatalf("a shift with trips clears the backoff: %d %v", c.b.fails, c.b.backoffUntil)
	}
	c.b.afterShift(c.ctx, carrierOutcome{Died: true})
	if c.b.deaths != 1 || c.b.backoffUntil.Before(time.Now().Add(25*time.Minute)) {
		t.Fatalf("a death backs the carrier off half an hour: %v", c.b.backoffUntil)
	}
	c.b.backoffUntil, c.b.fails = time.Time{}, 0
	c.b.afterShift(c.ctx, carrierOutcome{Stopped: true})
	if !c.b.backoffUntil.IsZero() || c.b.fails != 0 {
		t.Fatalf("a stop sets no wait")
	}
}

// A mule that gives nothing ends the shift at once ("the kit is spent") and that is a failed shift, which backs the carrier off: it
// must not walk to the mule again every minute.
func TestCarrierLiveShiftWhenTheMuleGivesNothingIsAFailedShift(t *testing.T) {
	c := newCarrierRig(t, nil, map[string]int{"Rune essence": 20000})
	c.openGate(mulebus.GateOpen, 0)
	c.serveKit(0, 0, false)
	out := c.b.liveShift(c.ctx, "test", false)
	if out.Trips != 0 || !out.Failed || !strings.Contains(out.Why, "last failure: the mule gave 0 noted Rune essence") {
		t.Fatalf("%+v\n%v", out, c.logs)
	}
	if got := carrierClaims.others("someone else", time.Now(), time.Time{}); got != 0 {
		t.Fatalf("a kit that did not arrive gives its claim back: %d", got)
	}
	c.b.afterShift(c.ctx, out)
	if c.b.fails != 1 || !c.b.backoffUntil.After(time.Now()) {
		t.Fatalf("fails %d until %v", c.b.fails, c.b.backoffUntil)
	}
}

// ---- the bot: the check, the safe point, the resume ----

type carrierSwarm struct {
	rig                  *carrierRig
	safe                 mastercraftplan.SafeInput
	starts, stops, loops int
	// idle is the Stage 5 quest hold saying it has the account settled at a bank (swarmHold); asked records the windows it was asked about.
	idle  bool
	asked []time.Duration
}

func (f *carrierSwarm) OnStart(*Context) error {
	f.starts++
	f.rig.events = append(f.rig.events, "start")
	return nil
}
func (f *carrierSwarm) Loop(*Context) error {
	f.loops++
	f.rig.events = append(f.rig.events, "loop")
	return nil
}
func (f *carrierSwarm) OnStop(*Context) { f.stops++; f.rig.events = append(f.rig.events, "stop") }
func (f *carrierSwarm) safeInput(*Context) mastercraftplan.SafeInput {
	return f.safe
}
func (f *carrierSwarm) idlingAtBank(within time.Duration) bool {
	f.asked = append(f.asked, within)
	return f.idle
}

func newCarrierBotRig(t *testing.T, pack []actionsim.Stack) (*carrierRig, *carrierSwarm) {
	t.Helper()
	c := newCarrierRig(t, pack, map[string]int{"Rune essence": 20000})
	sw := &carrierSwarm{rig: c, safe: mastercraftplan.SafeInput{TileKnown: true, HP: 40, MaxHP: 40}}
	c.b.newSwarm = func() masterSwarm { return sw }
	c.b.shift = func(ctx *Context, why string, resumed bool) carrierOutcome {
		c.events = append(c.events, "shift")
		return carrierOutcome{Trips: 3, Essence: 75}
	}
	oldEvery := carrierCheckEvery
	t.Cleanup(func() { carrierCheckEvery = oldEvery })
	carrierCheckEvery = time.Hour
	if err := c.b.startSwarm(c.ctx); err != nil {
		t.Fatal(err)
	}
	c.events = nil
	return c, sw
}

func TestCarrierLoopStartsAShiftWhenTheGateIsOpenAtASafePoint(t *testing.T) {
	c, sw := newCarrierBotRig(t, nil)
	c.openGate(mulebus.GateOpen, 0)
	// Until the account is far from the post the safe point holds the shift off; the rig stands at the post.
	if err := c.b.Loop(c.ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.events, ",") != "stop,shift,start" || sw.stops != 1 {
		t.Fatalf("events %v\n%v", c.events, c.logs)
	}
	if !c.logged("check: a shift is due") || !c.logged("SHIFT starting") || !c.logged("SHIFT over after") || !c.logged("back to ScarabSwarm") {
		t.Fatalf("log: %v", c.logs)
	}
	// The next Loop is between checks: ScarabSwarm runs.
	c.events = nil
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "loop" {
		t.Fatalf("events %v", c.events)
	}
}

func TestCarrierLoopWaitsForTheSafePoint(t *testing.T) {
	c, sw := newCarrierBotRig(t, nil)
	c.openGate(mulebus.GateOpen, 0)
	sw.safe.InCombat = true
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "loop" || c.b.pending == "" {
		t.Fatalf("events %v pending %q", c.events, c.b.pending)
	}
	if !c.logged("a shift is due") || !c.logged("not at a safe point: in a fight") {
		t.Fatalf("log: %v", c.logs)
	}
	sw.safe.InCombat = false
	c.events = nil
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "stop,shift,start" {
		t.Fatalf("events %v", c.events)
	}
}

func TestCarrierCheckSaysWhyNot(t *testing.T) {
	cases := map[string]struct {
		setup func(c *carrierRig)
		want  string
	}{
		"no gate heard":      {func(c *carrierRig) {}, "the gate is not heard"},
		"the gate is closed": {func(c *carrierRig) { c.openGate(mulebus.GateClosed, 0) }, "the gate is closed"},
		"the crafter away":   {func(c *carrierRig) { c.openGate(mulebus.GateAway, 0) }, "the crafter is away"},
		"no crafter set":     {func(c *carrierRig) { c.b.set.Crafter = "" }, "runecraftCrafter is not set"},
		"backing off":        {func(c *carrierRig) { c.openGate(mulebus.GateOpen, 0); c.b.backoffUntil = time.Now().Add(time.Hour) }, "backing off"},
		"the mule is away": {func(c *carrierRig) {
			c.openGate(mulebus.GateOpen, 0)
			c.ctx.Bus = mulebus.NewBus()
			c.openGate(mulebus.GateOpen, 0)
		}, "the mule is not heard"},
		"others claimed it": {func(c *carrierRig) {
			c.openGate(mulebus.GateOpen, 0)
			claimAfterPublish("Acct 9", 19900) // a claim in the publish's own clock tick was not counted: this case failed one run in thirty
		}, "worth the crossing"},
	}
	for name, tc := range cases {
		c, _ := newCarrierBotRig(t, nil)
		tc.setup(c)
		c.b.check(c.ctx, time.Now())
		if c.b.pending != "" || !c.logged(tc.want) {
			t.Errorf("%s: pending %q, want a log with %q\n%v", name, c.b.pending, tc.want, c.logs)
		}
	}
	// The gate on another world is not worked for.
	c, _ := newCarrierBotRig(t, nil)
	c.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(crafterName, tile.Tile{X: exitX, Z: exitZ}, mulebus.GateOpen, 0, 0), 2))
	c.b.check(c.ctx, time.Now())
	if c.b.pending != "" || !c.logged("on world 2") {
		t.Errorf("another world: %q %v", c.b.pending, c.logs)
	}
	// The same reason is said once, not every minute.
	c, _ = newCarrierBotRig(t, nil)
	c.b.check(c.ctx, time.Now())
	c.b.check(c.ctx, time.Now().Add(time.Minute))
	n := 0
	for _, l := range c.logs {
		if strings.Contains(l, "the gate is not heard") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("said %d times: %v", n, c.logs)
	}
}

// A kit in the pack is a shift under way: Loop resumes it at once, before the check and the safe point.
func TestCarrierLoopResumesAShiftForAKitInThePack(t *testing.T) {
	c, sw := newCarrierBotRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 2500}, {ID: coinsObj, Count: 8000}})
	sw.safe.InCombat = true // not a safe point, and it does not matter
	resumed := false
	c.b.shift = func(_ *Context, why string, r bool) carrierOutcome {
		resumed = r
		c.events = append(c.events, "shift")
		return carrierOutcome{}
	}
	if err := c.b.Loop(c.ctx); err != nil || !resumed || strings.Join(c.events, ",") != "stop,shift,start" {
		t.Fatalf("events %v resumed %v\n%v", c.events, resumed, c.logs)
	}
	if !c.logged("the pack holds 2500 noted Rune essence") || !c.logged("SHIFT resuming") {
		t.Fatalf("log: %v", c.logs)
	}
}

// A shift that has just failed is not resumed at once because its kit is still in the pack: ScarabSwarm goes on until the wait is over.
func TestCarrierResumeHonoursTheBackoff(t *testing.T) {
	c, _ := newCarrierBotRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 2500}, {ID: coinsObj, Count: 8000}})
	c.b.backoffUntil = time.Now().Add(10 * time.Minute)
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "loop" {
		t.Fatalf("events %v\n%v", c.events, c.logs)
	}
	if !c.logged("the last one failed: backing off until") {
		t.Fatalf("log: %v", c.logs)
	}
	c.events = nil
	c.b.backoffUntil = time.Now().Add(-time.Second)
	if err := c.b.Loop(c.ctx); err != nil || strings.Join(c.events, ",") != "stop,shift,start" {
		t.Fatalf("events %v", c.events)
	}
}

func TestCarrierResumeReadsOnlyACarriersKit(t *testing.T) {
	for name, tc := range map[string]struct {
		pack []actionsim.Stack
		at   tile.Tile
		want bool
	}{
		"a noted kit on the mainland":             {[]actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 25}}, tile.Tile{X: 2809, Z: 3441}, true},
		"a quest account's 24 unnoted":            {[]actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 24}}, tile.Tile{X: 2809, Z: 3441}, false},
		"a load of unnoted essence on the island": {[]actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}}, tile.Tile{X: 2800, Z: 3080}, true},
		"a few notes":                             {[]actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 10}}, tile.Tile{X: 2809, Z: 3441}, false},
		"an empty pack on the island":             {nil, tile.Tile{X: 2800, Z: 3080}, false},
	} {
		c, _ := newCarrierBotRig(t, tc.pack)
		c.sim.StandAt(tc.at.X, tc.at.Z)
		if got := c.b.resumeWhy(c.ctx) != ""; got != tc.want {
			t.Errorf("%s: resume %v, want %v", name, got, tc.want)
		}
	}
}

func TestCarrierOnStopReleasesTheClaim(t *testing.T) {
	c, sw := newCarrierBotRig(t, nil)
	carrierClaims.claim("Acct 7", 3000, time.Now())
	c.b.OnStop(c.ctx)
	if carrierClaims.others("x", time.Now(), time.Time{}) != 0 || sw.stops != 1 || c.b.swarm != nil {
		t.Fatal("OnStop stops ScarabSwarm and gives the claim back")
	}
}

func TestCarrierClaimsExpireAndSumOthers(t *testing.T) {
	cl := &carrierKitClaims{by: map[string]carrierClaim{}}
	now := time.Now()
	cl.claim("A", 3000, now)
	cl.claim("b", 1000, now)
	cl.claim("C", 500, now.Add(-carrierClaimTTL-time.Second))
	if got := cl.others("a", now, time.Time{}); got != 1000 {
		t.Fatalf("others of a = %d, want b's 1000 (C's claim expired)", got)
	}
	cl.release("B")
	if got := cl.others("x", now, time.Time{}); got != 3000 {
		t.Fatalf("others of x = %d", got)
	}
	// A claim made before the mule last published is already out of the bank it published.
	if got := cl.others("x", now, now.Add(time.Second)); got != 0 {
		t.Fatalf("a claim older than the mule's last publication still counts: %d", got)
	}
	if got := cl.others("x", now, now.Add(-time.Second)); got != 3000 {
		t.Fatalf("a claim newer than it does not count: %d", got)
	}
}

// The ScarabSwarm settings travel under swarmSettings and are swapped back after.
func TestCarrierSwarmRunsOnItsOwnSettings(t *testing.T) {
	c := newCarrierRig(t, nil, nil)
	c.b.set.SwarmSettings = map[string]interface{}{"supplyMode": "mule"}
	c.ctx.Settings = map[string]interface{}{"runecraftCrafter": crafterName}
	var seen map[string]interface{}
	sw := &settingsSwarm{seen: &seen}
	c.b.newSwarm = func() masterSwarm { return sw }
	if err := c.b.startSwarm(c.ctx); err != nil {
		t.Fatal(err)
	}
	if seen["supplyMode"] != "mule" || seen["mule"] != "mule" || len(seen) != 2 {
		t.Fatalf("ScarabSwarm saw %v", seen)
	}
	c.b.stopSwarm(c.ctx)
	if c.ctx.Settings["runecraftCrafter"] != crafterName {
		t.Fatalf("settings not swapped back: %v", c.ctx.Settings)
	}
}

type settingsSwarm struct{ seen *map[string]interface{} }

func (s *settingsSwarm) OnStart(ctx *Context) error {
	m := map[string]interface{}{}
	for k, v := range ctx.Settings {
		m[k] = v
	}
	*s.seen = m
	return nil
}
func (s *settingsSwarm) Loop(*Context) error { return nil }
func (s *settingsSwarm) OnStop(*Context)     {}
func (s *settingsSwarm) safeInput(*Context) mastercraftplan.SafeInput {
	return mastercraftplan.SafeInput{}
}

// A crafter that holds a stack of bows says on his gate message that he takes 24 a trade: the shelf buys a load of 24, not 25.
func TestCarrierLoadsWhatTheCrafterCanTake(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 3000}, {ID: coinsObj, Count: 10000}}, nil)
	var wants []int
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		wants = append(wants, o.Want)
		c.sim.Give(runecraftplan.EssenceID, o.Want)
		return runecraftrunner.ShelfResult{UnnotedAt: o.Want, Bought: o.Want}
	}
	w := c.world()
	c.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(crafterName, tile.Tile{X: exitX, Z: exitZ}, mulebus.GateOpen, 0, 0).WithCapacity(24), 1))
	if res := w.shelf(); !res.OK {
		t.Fatalf("%+v", res)
	}
	c.sim.Remove(runecraftplan.EssenceID, 100)
	// The capacity back at a full load (the stack is gone), and a gate that never said: 25.
	c.ctx.Bus.Publish(mulebus.OnWorld(mulebus.GateMessage(crafterName, tile.Tile{X: exitX, Z: exitZ}, mulebus.GateOpen, 0, 0).WithCapacity(25), 1))
	if res := w.shelf(); !res.OK {
		t.Fatalf("%+v", res)
	}
	c.sim.Remove(runecraftplan.EssenceID, 100)
	c2 := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 3000}, {ID: coinsObj, Count: 10000}}, nil)
	carrierShelf = func(d *fleetrunner.Deps, o runecraftrunner.ShelfOptions) runecraftrunner.ShelfResult {
		wants = append(wants, o.Want)
		c2.sim.Give(runecraftplan.EssenceID, o.Want)
		return runecraftrunner.ShelfResult{UnnotedAt: o.Want, Bought: o.Want}
	}
	c2.openGate(mulebus.GateOpen, 0)
	if res := c2.world().shelf(); !res.OK {
		t.Fatalf("%+v", res)
	}
	if len(wants) != 3 || wants[0] != 24 || wants[1] != 25 || wants[2] != 25 {
		t.Fatalf("loads %v, want 24, 25, 25", wants)
	}
}

// The wait tile held by something the walk cannot pass (an NPC on it): the carrier goes to the gate as before, anywhere within three tiles
// of the portal's exit, and trades from there.
func TestCarrierFallsBackToTheExitWhenTheWaitTileCannotBeTaken(t *testing.T) {
	c := newCarrierRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}, {ID: coinsObj, Count: 5000}}, nil)
	carrierWalk = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		c.walks = append(c.walks, fmt.Sprintf("%d,%d/%d", dest.X, dest.Z, radius))
		if dest == runecraftplan.GateWaitTile {
			return false
		}
		c.sim.StandAt(dest.X, dest.Z)
		return true
	}
	c.sim.StandAt(2767, 3123) // the store
	w := c.world()
	if res := w.walkToGate(); !res.OK {
		t.Fatalf("%+v walks %v", res, c.walks)
	}
	if len(c.walks) != 2 || c.walks[0] != "2867,3021/0" || c.walks[1] != "2865,3022/3" {
		t.Fatalf("walks %v", c.walks)
	}
	// Short of the gate altogether and the fallback failing too: a failure, as a walk that does not arrive always was.
	carrierWalk = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool { return false }
	c.sim.StandAt(2767, 3123)
	if res := w.walkToGate(); res.OK {
		t.Fatalf("%+v", res)
	}
	// Already at the gate when the wait tile fails: trade from there, no second walk.
	n := 0
	carrierWalk = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool { n++; return false }
	c.sim.StandAt(2866, 3021)
	if res := w.walkToGate(); !res.OK || n != 1 {
		t.Fatalf("%+v after %d walks", res, n)
	}
}
