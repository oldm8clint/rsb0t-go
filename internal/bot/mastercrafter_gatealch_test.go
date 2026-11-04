package bot

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// The gate's alchemy (mastercrafter_gatealch.go, runecraftplan/gatealch.go).

const (
	bowMagicShort    = 861 // Magic shortbow, strung
	bowMagicShortNot = 862 // ... as a note
	bowYewLong       = 855
	bowYewLongNot    = 856
)

// alchRig is the gate world with Magic 78, a fire staff worn, 6,000 Natures and a stack of noted Magic shortbows.
func alchRig(t *testing.T, stack int, mod func(w *masterNatureWorld)) (*masterRig, *masterNatureWorld) {
	t.Helper()
	pack := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 6000}, {ID: coinsObj, Count: 100}}
	if stack > 0 {
		pack = append(pack, actionsim.Stack{ID: bowMagicShortNot, Count: stack})
	}
	r := newNatureRig(t, pack, nil, nil)
	stackable(t, bowMagicShortNot, bowYewLongNot)
	mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(78), 78)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	oldStaff, oldCast, oldPlayers := gateAlchFireStaff, gateAlchCast, gateAlchPlayers
	t.Cleanup(func() { gateAlchFireStaff, gateAlchCast, gateAlchPlayers = oldStaff, oldCast, oldPlayers })
	gateAlchFireStaff = func(*fleetrunner.Deps) bool { return true }
	gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player { return nil }
	w := newNatureWorld(t, r, func(w *masterNatureWorld) {
		w.stand, w.haveStand = runecraftplan.GateStandTile, true
		w.set.Runners = []string{"acct_1", "acct_2"}
		w.alch = gateAlchState{on: true, bows: []runecraftrunner.AlchBowIDs{{Name: "Magic shortbow", Strung: bowMagicShort, Noted: bowMagicShortNot}, {Name: "Yew longbow", Strung: bowYewLong, Noted: bowYewLongNot}}}
		w.alch.held = map[string]int{"Magic shortbow": stack}
		w.alch.arrived, w.alch.burnFrom, w.alch.lastStack = true, time.Now(), stack
		if mod != nil {
			mod(w)
		}
	})
	w.d.Travel = &fleetrunner.Travel{Members: true}
	return r, w
}

// castAway is a cast that lands: one bow leaves the pack and a Travel counts it.
func castAway(r *masterRig, casts *int) func(*fleetrunner.Travel, *fleetrunner.Deps) bool {
	return func(t *fleetrunner.Travel, d *fleetrunner.Deps) bool {
		if !t.Alch.Enabled {
			return false // the walk alchemy is switched on around the call
		}
		if r.sim.Remove(bowMagicShortNot, 1) == 0 {
			return false
		}
		r.sim.Give(coinsObj, runecraftplan.AlchPays(1600))
		*casts++
		return true
	}
}

// THE IDLE-TIME RULE through the live world: nothing waiting or open, no carrier near, the lock over.
func TestGateAlchCastsOnlyWhenNothingCanBeAskingAndSpacesTheCasts(t *testing.T) {
	r, w := alchRig(t, 40, nil)
	casts := 0
	gateAlchCast = castAway(r, &casts)
	// 1. Idle: a cast goes out.
	w.gateAlch()
	if casts != 1 || !w.alchLocked() {
		t.Fatalf("casts %d, locked %v (%s)", casts, w.alchLocked(), w.alch.why)
	}
	// 2. The lock is not over: no second cast, however often it is asked.
	for i := 0; i < 5; i++ {
		w.gateAlch()
	}
	if casts != 1 {
		t.Fatalf("%d casts inside the lock", casts)
	}
	// 3. The lock over: the next goes.
	w.alch.castAt = time.Now().Add(-4 * time.Second)
	if w.alchLocked() {
		t.Fatal("still locked after four seconds")
	}
	w.gateAlch()
	if casts != 2 {
		t.Fatalf("%d casts after the lock", casts)
	}
	// 4. A carrier in view does not stop the cast while the stack is burned down before the gate trades: the carriers are waiting for
	// the stack to go, and it is the only way they get their trade (runecraftplan.GateAlchView.Burning).
	w.alch.castAt = time.Time{}
	gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player {
		return []gamestate.Player{{Name: "Acct 1", X: runecraftplan.GateStandTile.X + 1, Z: runecraftplan.GateStandTile.Z}}
	}
	w.gateAlch()
	if casts != 3 {
		t.Fatalf("no cast with a carrier beside the stand while burning the stack down: %d casts (%s)", casts, w.alch.why)
	}
}

// The nearest carrier in view, for the clearance rule: his carriers only (a stranger is not asking), not the one that has just traded (it is
// on its way to the store).
func TestGateAlchNearestCarrier(t *testing.T) {
	_, w := alchRig(t, 40, nil)
	at := func(dx int, names ...string) {
		gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player {
			var out []gamestate.Player
			for i, n := range names {
				out = append(out, gamestate.Player{Name: n, X: runecraftplan.GateStandTile.X + dx + i, Z: runecraftplan.GateStandTile.Z})
			}
			return out
		}
	}
	at(0)
	if w.nearestCarrier() != -1 {
		t.Fatal("nobody in view")
	}
	at(3, "Acct 1", "Acct 2")
	if got := w.nearestCarrier(); got != 3 {
		t.Fatalf("nearest %d, want 3", got)
	}
	at(1, "Bob", "Acct 2")
	if got := w.nearestCarrier(); got != 2 {
		t.Fatalf("nearest %d: Bob is not a carrier", got)
	}
	w.lastPartner, w.lastPartnerAt = "Acct 2", time.Now()
	if got := w.nearestCarrier(); got != -1 {
		t.Fatalf("nearest %d: Acct 2 has just traded", got)
	}
	w.lastPartnerAt = time.Now().Add(-2 * time.Minute)
	if got := w.nearestCarrier(); got != 2 {
		t.Fatalf("nearest %d: Acct 2 traded two minutes ago", got)
	}
}

// A cast would close a trade screen, be dropped by a delayed player's answer, or have a request refused as "busy": none goes out while a
// screen is open, a request waits or an answer is out.
func TestGateAlchNeverCastsOverATradeOrARequest(t *testing.T) {
	r, w := alchRig(t, 40, nil)
	casts := 0
	gateAlchCast = castAway(r, &casts)
	// A trade screen open.
	r.sim.OpenTrade(&actionsim.Trade{Partner: "Acct 1"})
	w.gateAlch()
	if casts != 0 || !strings.Contains(w.alch.why, "screen is open") {
		t.Fatalf("%d casts under an open screen (%s)", casts, w.alch.why)
	}
	// A request waits: the watcher reads it from the chat. With the stack held through the trades the request WILL be answered, and a cast
	// would be refused as busy or drop the answer, so none goes out (the road mode, which answers nobody until the bows are gone, casts
	// through it: TestGateAlchBurnsThroughARequestThatWaits).
	r2, w2 := alchRig(t, 40, func(w *masterNatureWorld) { w.set.AlchHold = true })
	gateAlchCast = castAway(r2, &casts)
	w2.watch = runecraftrunner.NewGateWatcher(w2.d)
	r2.ctx.Chat.Record("Acct 1:tradereq:")
	runecraftrunner.GateStep(w2.d, w2.watch, runecraftrunner.GateOptions{Wait: 30 * time.Millisecond, OnStand: func() bool { return false }})
	if w2.watch.Pending() != 1 {
		t.Fatalf("pending %d", w2.watch.Pending())
	}
	w2.gateAlch()
	if casts != 0 || !strings.Contains(w2.alch.why, "wait to be answered") {
		t.Fatalf("%d casts with a request waiting (%s)", casts, w2.alch.why)
	}
}

// What the cast needs: Magic 55, a fire staff, a Nature rune over the floor, a bow.
func TestGateAlchNeedsTheKitToCast(t *testing.T) {
	for name, mod := range map[string]func(r *masterRig, w *masterNatureWorld){
		"no fire staff": func(r *masterRig, w *masterNatureWorld) {
			gateAlchFireStaff = func(*fleetrunner.Deps) bool { return false }
		},
		"Magic 54": func(r *masterRig, w *masterNatureWorld) {
			mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(54), 54)
		},
		"no Natures":          func(r *masterRig, w *masterNatureWorld) { r.sim.Remove(natRune, 6000) },
		"switched off":        func(r *masterRig, w *masterNatureWorld) { w.alch.on = false },
		"no bows":             func(r *masterRig, w *masterNatureWorld) { r.sim.Remove(bowMagicShortNot, 40) },
		"not at the gate yet": func(r *masterRig, w *masterNatureWorld) { w.alch.arrived = false },
	} {
		r, w := alchRig(t, 40, nil)
		casts := 0
		gateAlchCast = castAway(r, &casts)
		mod(r, w)
		w.gateAlch()
		if casts != 0 {
			t.Errorf("%s: cast %d times", name, casts)
		}
	}
}

// While the stack is burned down before the first trade, the carriers wait for it: no request is answered (Hold), and the cast ignores the
// clearance rule (they are already there).
func TestGateAlchBurnsTheStackDownBeforeTheGateTrades(t *testing.T) {
	r, w := alchRig(t, 3, func(w *masterNatureWorld) { w.alch.drawn = 3 })
	casts := 0
	gateAlchCast = castAway(r, &casts)
	if !w.alchHold() {
		t.Fatal("the gate answers a request while bows are held")
	}
	gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player {
		return []gamestate.Player{{Name: "Acct 1", X: runecraftplan.GateStandTile.X, Z: runecraftplan.GateStandTile.Z + 1}}
	}
	for i := 0; i < 3; i++ {
		w.alch.castAt = time.Time{}
		w.gateAlch()
	}
	if casts != 3 || w.alchStack() != 0 {
		t.Fatalf("%d casts, %d bows left", casts, w.alchStack())
	}
	if w.alchHold() {
		t.Fatal("the gate is still held with no bows left")
	}
	// The coins are counted by the bow's value, and a line says what was done.
	w.gateAlch()
	if w.alch.coins != 3*runecraftplan.AlchPays(1600) {
		t.Fatalf("coins %d, want %d", w.alch.coins, 3*runecraftplan.AlchPays(1600))
	}
	if ps := w.alchPace(); !strings.Contains(ps, "0 bow(s) left") {
		t.Fatalf("pace %q", ps)
	}
}

// A stack that cannot be cast away ends the round (the bank takes it back) and the alchemy stays off for the next ones.
func TestGateAlchStuckStackEndsTheRound(t *testing.T) {
	r, w := alchRig(t, 40, nil)
	gateAlchCast = func(*fleetrunner.Travel, *fleetrunner.Deps) bool { return false } // casts that never go out
	_ = r
	if why := w.endReason(); why != "" {
		t.Fatalf("a fresh stack ended the round: %q", why)
	}
	// Casts have been allowed for longer than gateAlchStuckAfter and no bow has gone: the cast cannot go on.
	w.alch.stallFrom = time.Now().Add(-gateAlchStuckAfter - time.Second)
	if w.alchHold() {
		t.Fatal("the gate still holds for a stack that does not go")
	}
	why := w.endReason()
	if !strings.Contains(why, "40 noted bow(s) are held and cannot be cast away") {
		t.Fatalf("end reason %q", why)
	}
	if w.endReason() != why || !w.b.gateAlchOffUntil.After(time.Now().Add(5*time.Hour)) {
		t.Fatalf("the reason is kept and the alchemy is off for hours: %v", w.b.gateAlchOffUntil)
	}
	if !r.logged("the round ends, the bank at the post takes them back") {
		t.Fatalf("log: %v", r.logs)
	}
	// The next round does not start it.
	w2 := newNatureWorld(t, r, nil)
	w2.set.GateAlch, w2.set.AlchBows = true, 100
	w2.b = w.b
	w2.ctx.Config = &cachecfg.Config{}
	w2.initAlch()
	if w2.alch.on {
		t.Fatal("the alchemy started again inside the off time")
	}
	// And the burn running long enough ends it as well.
	_, w3 := alchRig(t, 40, nil)
	gateAlchCast = func(*fleetrunner.Travel, *fleetrunner.Deps) bool { return false }
	w3.alch.burnFrom = time.Now().Add(-runecraftplan.AlchBurnMax - time.Second)
	if why := w3.endReason(); !strings.Contains(why, "still held after") {
		t.Fatalf("%q", why)
	}
}

// THE DEADLOCK THE REVIEW FOUND (16 Oct 2026): bows are left on arrival, so the gate holds every answer until they are gone, and a carrier's
// request waits. The cast used to refuse while a request waited, so the request could not be answered and the bows could not be cast: after
// 25 seconds the stuck clock ended the round and switched the alchemy off for six hours. Through the live GateStep hooks: a request that waits
// does not stop the burn, the request is answered only once the stack is gone, and the round goes on.
func TestGateAlchBurnsThroughARequestThatWaits(t *testing.T) {
	r, w := alchRig(t, 3, func(w *masterNatureWorld) { w.alch.drawn = 3 })
	r.sim.ShowPlayers("Acct 1")
	casts := 0
	gateAlchCast = castAway(r, &casts)
	var answers atomic.Int32
	r.sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			answers.Add(1)
		}
	}
	opts := runecraftrunner.GateOptions{Allowed: w.allowed, Wait: 120 * time.Millisecond, OnStand: w.onStand, Idle: w.gateIdle, Hold: w.alchHold, Delayed: w.alchLocked}
	r.ctx.Chat.Record("Acct 1:tradereq:")
	for i := 1; i <= 3; i++ {
		w.alch.castAt = time.Time{} // the lock between casts is not what this test is about
		runecraftrunner.GateStep(w.d, w.watch, opts)
		if casts != i || w.alchStack() != 3-i {
			t.Fatalf("step %d: %d casts, %d bows left (%s)", i, casts, w.alchStack(), w.alch.why)
		}
		if answers.Load() != 0 {
			t.Fatalf("step %d: the request was answered with %d bow(s) still held (the trade of 25 would be refused)", i, w.alchStack())
		}
		if w.watch.Pending() != 1 {
			t.Fatalf("step %d: the request must stay queued: pending %d", i, w.watch.Pending())
		}
	}
	// A long time with the request waiting is not a stuck stack: the clock counts only the casts that were allowed, and they landed.
	w.alch.burnFrom = time.Now().Add(-3 * time.Minute)
	if why := w.endReason(); why != "" || w.b.gateAlchOffUntil.After(time.Now()) {
		t.Fatalf("the round was ended for a stack that went: %q, alchemy off until %v", why, w.b.gateAlchOffUntil)
	}
	// The stack gone and the last cast's lock over, the gate answers.
	w.alch.castAt = time.Time{}
	runecraftrunner.GateStep(w.d, w.watch, opts)
	if answers.Load() != 1 || w.watch.Pending() != 0 {
		t.Fatalf("answers %d, pending %d after the stack went", answers.Load(), w.watch.Pending())
	}
}

// The stuck clock runs only over casts that were ALLOWED: a screen that stays open, a random event or a lock is not a stack that cannot be
// cast. (It used to run from the last bow that fell, so any twenty-five seconds of waiting on something else ended the round.)
func TestGateAlchStuckClockRunsOnlyOverCastsThatWereAllowed(t *testing.T) {
	r, w := alchRig(t, 20, nil)
	casts := 0
	gateAlchCast = castAway(r, &casts)
	r.sim.OpenTrade(&actionsim.Trade{Partner: "Acct 1"})
	for i := 0; i < 3; i++ {
		w.gateAlch()
	}
	if !w.alch.stallFrom.IsZero() || casts != 0 {
		t.Fatalf("a cast refused under an open screen started the clock: %v, %d casts", w.alch.stallFrom, casts)
	}
	w.alch.burnFrom = time.Now().Add(-time.Minute) // the old clock, from the arrival or the last bow, would call this stuck
	if why := w.endReason(); why != "" || !w.alchCanCast() {
		t.Fatalf("a minute of waiting under a screen ended the round: %q", why)
	}
	// The screen closed: the first allowed cast starts the clock and the bow that falls stops it.
	r.sim.PartnerClosesTrade()
	w.gateAlch()
	if casts != 1 || !w.alch.stallFrom.IsZero() {
		w.gateAlch() // the fall is seen by the next look
	}
	if casts != 1 || !w.alch.stallFrom.IsZero() {
		t.Fatalf("%d casts, clock %v: the bow fell and the clock must stop", casts, w.alch.stallFrom)
	}
	// Casts allowed, sent and landing nothing for longer than the window: the stack is stuck.
	gateAlchCast = func(*fleetrunner.Travel, *fleetrunner.Deps) bool { return false }
	w.alch.castAt = time.Time{}
	w.gateAlch()
	if w.alch.stallFrom.IsZero() {
		t.Fatal("an allowed cast must start the clock")
	}
	w.alch.stallFrom = time.Now().Add(-gateAlchStuckAfter - time.Second)
	if why := w.endReason(); !strings.Contains(why, "noted bow(s) are held and cannot be cast away") {
		t.Fatalf("end reason %q", why)
	}
}

// The draw follows the road: 60 for a first round, then what the last road took, and the arrival writes the road's figures down.
func TestGateAlchDrawFollowsTheRoad(t *testing.T) {
	_, w := alchRig(t, 0, func(w *masterNatureWorld) { w.set.GateAlch, w.set.AlchBows, w.set.AlchHold = true, 100, false })
	if got := w.alchWant().Qty; got != runecraftplan.AlchFirstDraw {
		t.Fatalf("first round drew %d, want %d", got, runecraftplan.AlchFirstDraw)
	}
	w.b.gateAlchRoad.drawn, w.b.gateAlchRoad.casts, w.b.gateAlchRoad.left = 60, 41, 19
	if got := w.alchWant().Qty; got != 41+runecraftplan.AlchDrawMargin {
		t.Fatalf("after a road of 41 casts: drew %d", got)
	}
	w.b.gateAlchRoad.drawn, w.b.gateAlchRoad.casts, w.b.gateAlchRoad.left = 60, 60, 0
	if got := w.alchWant().Qty; got != 72 {
		t.Fatalf("after a road that took them all: drew %d, want 72", got)
	}
	// A stack held through the trades is cast in the idle seconds, not on the road: the setting stands.
	w.set.AlchHold = true
	if got := w.alchWant().Qty; got != 100 {
		t.Fatalf("hold mode drew %d, want the setting 100", got)
	}
	// The arrival writes the road down.
	_, w2 := alchRig(t, 17, func(w *masterNatureWorld) { w.alch.arrived, w.alch.drawn = false, 60 })
	w2.alchArrived()
	if r := w2.b.gateAlchRoad; r.drawn != 60 || r.left != 17 {
		t.Fatalf("road %+v", r)
	}
}

// The walk alchemy is on for the walk to the gate, with the fleet's alch list as its stock, and off after it.
func TestGateAlchRoadSwitchesTheWalkAlchemyOnAndOff(t *testing.T) {
	_, w := alchRig(t, 40, nil)
	w.alchRoad(true)
	if !w.d.Travel.Alch.Enabled || w.d.Travel.Alch.MinMagic != runecraftplan.AlchMagic || strings.Join(w.d.Travel.Alch.Items, ",") != strings.Join(fleetplan.DefaultAlchItems, ",") {
		t.Fatalf("%+v", w.d.Travel.Alch)
	}
	w.alchRoad(false)
	if w.d.Travel.Alch.Enabled {
		t.Fatal("still on after the walk")
	}
	// No stack, no alchemy on the road.
	_, w2 := alchRig(t, 0, nil)
	w2.alchRoad(true)
	if w2.d.Travel.Alch.Enabled {
		t.Fatal("the walk alchemy was switched on with nothing to cast")
	}
}

// Arriving at the gate with some of the stack left: said, with the road's casts, and a warning when it is long.
func TestGateAlchArrivalSaysWhatTheRoadDid(t *testing.T) {
	r, w := alchRig(t, 30, func(w *masterNatureWorld) { w.alch.arrived, w.alch.drawn = false, 100 })
	w.alchArrived()
	if !w.alch.arrived || !r.logged("alch: reached the gate with 30 of the 100 bow(s) left") {
		t.Fatalf("log: %v", r.logs)
	}
	if r.logged("WARNING: 30 bow(s) are left at the gate") == false {
		t.Fatalf("30 bows left is more than a minute of casts: %v", r.logs)
	}
}

// The kit: a bank with bows gives noted ones, the dearest first, no more than the Natures can cast; nothing for an account that cannot cast.
// It needs the object config (the real one: RSB0T_CONFIG_JAG).
func TestGateAlchKitIsDrawnAsNotesDearestFirst(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The fleet's alch list, dearest first (a user request), with the four strung bows and the unstrung yews among it.
	bows := runecraftrunner.ResolveAlchBows(cfg)
	byName := map[string]runecraftrunner.AlchBowIDs{}
	for i, b := range bows {
		byName[b.Name] = b
		if i > 0 && fleetplan.KnownAlchValue(b.Name) > fleetplan.KnownAlchValue(bows[i-1].Name) {
			t.Errorf("%s pays more than %s before it", b.Name, bows[i-1].Name)
		}
		if b.Strung == 0 || b.Noted == 0 {
			t.Errorf("%+v", b)
		}
		if b.Noted == b.Strung {
			if o, _ := cfg.Obj(int(b.Strung)); !o.Stackable {
				t.Errorf("%s: no note and not stackable", b.Name)
			}
			continue
		}
		if o, _ := cfg.Obj(int(b.Noted)); !o.Noted() || o.CertLink != int(b.Strung) {
			t.Errorf("%s: %d is not the note of %d", b.Name, b.Noted, b.Strung)
		}
	}
	for _, n := range []string{"Magic longbow", "Magic shortbow", "Yew longbow", "Yew shortbow", "Yew longbow id66", "Yew shortbow id68"} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("%s is not in the gate's stock: %+v", n, bows)
		}
	}
	if byName["Yew longbow"].Strung == byName["Yew longbow id66"].Strung || byName["Yew longbow id66"].Strung != 66 {
		t.Fatalf("the strung and the unstrung Yew longbow are told apart: %+v %+v", byName["Yew longbow"], byName["Yew longbow id66"])
	}
	ml, ms := byName["Magic longbow"], byName["Magic shortbow"]
	stackable(t, ml.Noted, ms.Noted, byName["Yew longbow"].Noted)
	bank := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsObj, Count: 80000}, {ID: ml.Strung, Count: 30}, {ID: ms.Strung, Count: 72000}, {ID: byName["Yew longbow"].Strung, Count: 500}}
	r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 6000}}, bank, map[string]int{"Nature talisman": 7})
	r.ctx.Config = cfg
	withRuneBank(t, r)
	mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(78), 78)
	oldStaff := gateAlchFireStaff
	t.Cleanup(func() { gateAlchFireStaff = oldStaff })
	gateAlchFireStaff = func(*fleetrunner.Deps) bool { return true }
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.set.GateAlch, w.set.AlchBows = true, 100 })
	w.ctx.Config = cfg
	w.initAlch()
	if !w.alch.on {
		t.Fatalf("the alchemy is off: %v", r.logs)
	}
	if why, ok := w.provision(); !ok {
		t.Fatalf("%s\n%v", why, r.logs)
	}
	p := r.sim.Pack()
	// The first round (the road not measured yet) draws runecraftplan.AlchFirstDraw, not the setting: the 30 Magic longbows first, then 30
	// Magic shortbows, as notes.
	if simCount(p, ml.Noted) != 30 || simCount(p, ms.Noted) != runecraftplan.AlchFirstDraw-30 || simCount(p, ml.Strung)+simCount(p, ms.Strung) != 0 {
		t.Fatalf("pack %v", p)
	}
	if b := r.sim.Bank(); simCount(b, ms.Strung) != 72000-(runecraftplan.AlchFirstDraw-30) || simCount(b, ml.Strung) != 0 {
		t.Fatalf("bank %d magic shortbows, %d magic longbows", simCount(b, ms.Strung), simCount(b, ml.Strung))
	}
	if w.alch.drawn != runecraftplan.AlchFirstDraw || !r.logged("alch: drew 60 noted bow(s)") {
		t.Fatalf("drawn %d, log: %v", w.alch.drawn, r.logs)
	}
	// The gate pack is the talisman, the Natures, the fare, and the one stack of bows... three kinds of note: that is the walk's to cast.
	if simCount(p, natTal) != 1 || simCount(p, natRune) != 6000 || simCount(p, coinsObj) != runecraftplan.GateFare {
		t.Fatalf("the gate pack: %v", p)
	}
	// The kit is bounded by the Natures that can cast it (over their floor).
	r2 := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: runecraftplan.AlchNaturesFloor + 12}}, bank, map[string]int{"Nature talisman": 7})
	r2.ctx.Config = cfg
	withRuneBank(t, r2)
	mindHoldStat(r2.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(78), 78)
	w2 := newNatureWorld(t, r2, func(w *masterNatureWorld) { w.set.GateAlch, w.set.AlchBows, w.set.Keep = true, 100, 0 })
	w2.ctx.Config = cfg
	w2.initAlch()
	if why, ok := w2.provision(); !ok {
		t.Fatalf("%s", why)
	}
	if got := simCount(r2.sim.Pack(), ml.Noted) + simCount(r2.sim.Pack(), ms.Noted); got != 12 {
		t.Fatalf("%d bows drawn against %d Natures over the floor: %v", got, 12, r2.sim.Pack())
	}
	// An account that cannot cast draws none, and says why.
	r3 := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 6000}}, bank, map[string]int{"Nature talisman": 7})
	r3.ctx.Config = cfg
	withRuneBank(t, r3)
	mindHoldStat(r3.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(54), 54)
	w3 := newNatureWorld(t, r3, func(w *masterNatureWorld) { w.set.GateAlch, w.set.AlchBows = true, 100 })
	w3.ctx.Config = cfg
	w3.initAlch()
	if why, ok := w3.provision(); !ok {
		t.Fatalf("%s", why)
	}
	if got := simCount(r3.sim.Pack(), ml.Noted) + simCount(r3.sim.Pack(), ms.Noted); got != 0 || !r3.logged("alch: no bows drawn (Magic 54") {
		t.Fatalf("%d bows drawn at Magic 54: %v", got, r3.logs)
	}
}

// With the setting off the gate draws nothing and casts nothing.
func TestGateAlchSettingOff(t *testing.T) {
	r := newNatureRig(t, nil, nil, nil)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.set.GateAlch = false })
	w.ctx.Config = &cachecfg.Config{}
	w.initAlch()
	if w.alch.on || w.alchWant().Qty != 0 || w.alchStack() != 0 || w.alchPace() != "" {
		t.Fatalf("%+v", w.alch)
	}
	w.set.GateAlch, w.set.AlchBows = true, 0
	w.initAlch()
	if w.alch.on {
		t.Fatal("craftRuneAlchBows 0 draws none")
	}
}

// THE HELD STACK (craftRuneAlchHold): the stack is kept through the trades. The gate is not held up by it (it takes 24), its message says
// so, and the casts keep to the idle seconds: a carrier within the clearance stops a cast.
func TestGateAlchHoldKeepsTheStackThroughTheTradesAndSaysSo(t *testing.T) {
	r, w := alchRig(t, 40, func(w *masterNatureWorld) { w.set.AlchHold = true })
	casts := 0
	gateAlchCast = castAway(r, &casts)
	if w.alchHold() || w.alchBurning() || w.alchStuckWhy() != "" {
		t.Fatalf("hold %v burning %v stuck %q: a held stack must not hold the gate up", w.alchHold(), w.alchBurning(), w.alchStuckWhy())
	}
	// The gate message says 24, with the stack; 25 without it.
	w.publish(mulebus.GateOpen, 0)
	if g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now().Add(time.Millisecond), 0); !g.Heard || g.Capacity != 24 {
		t.Fatalf("gate capacity %+v", g)
	}
	// The clearance rule is live: a carrier 5 tiles away stops the cast, one beyond the clearance does not.
	gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player {
		return []gamestate.Player{{Name: "Acct 1", X: runecraftplan.GateStandTile.X + 5, Z: runecraftplan.GateStandTile.Z}}
	}
	w.gateAlch()
	if casts != 0 || !strings.Contains(w.alch.why, "could be asking") {
		t.Fatalf("%d casts with a carrier 5 tiles away (%s)", casts, w.alch.why)
	}
	gateAlchPlayers = func(*fleetrunner.Deps) []gamestate.Player {
		return []gamestate.Player{{Name: "Acct 1", X: runecraftplan.GateStandTile.X + runecraftplan.AlchClearTiles + 1, Z: runecraftplan.GateStandTile.Z}}
	}
	w.gateAlch()
	if casts != 1 {
		t.Fatalf("%d casts with a carrier beyond the clearance (%s)", casts, w.alch.why)
	}
	// The stack gone: the message goes back to a full load.
	r.sim.Remove(bowMagicShortNot, 100)
	w.pubAt = time.Time{}
	w.publish(mulebus.GateOpen, 0)
	if g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now().Add(time.Millisecond), 0); g.Capacity != runecraftplan.CarrierOffer {
		t.Fatalf("gate capacity %+v with no stack", g)
	}
}

// The road mode (the default) keeps the gate at 25 a trade and says nothing of a capacity beyond what the pack could take.
func TestGateAlchRoadModeHoldsTheGateWhileBowsAreLeft(t *testing.T) {
	_, w := alchRig(t, 40, nil)
	if !w.alchHold() || !w.alchBurning() {
		t.Fatalf("hold %v burning %v", w.alchHold(), w.alchBurning())
	}
}

// The bank holds no bows: the mule is asked for four rounds' worth of the dearest it publishes, by the qualified name that tells the strung
// bow from the unstrung one; what it gives is banked and drawn in a second visit. A mule that gives nothing leaves the gate without
// alchemy, and the round goes on.
func TestGateAlchAsksTheMuleWhenTheBankHoldsNoBows(t *testing.T) {
	stackable(t, bowMagicShortNot, bowYewLongNot)
	bank := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsObj, Count: 80000}}
	mk := func(stock map[string]int) (*masterRig, *masterNatureWorld) {
		r := newNatureRig(t, []actionsim.Stack{{ID: natRune, Count: 6000}}, bank, stock)
		withRuneBank(t, r)
		mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(78), 78)
		w := newNatureWorld(t, r, func(w *masterNatureWorld) { w.set.GateAlch, w.set.AlchBows = true, 100 })
		w.alch = gateAlchState{on: true, bows: []runecraftrunner.AlchBowIDs{
			{Name: "Magic shortbow", Strung: bowMagicShort, Noted: bowMagicShortNot}, {Name: "Yew longbow", Strung: bowYewLong, Noted: bowYewLongNot}}}
		// A road that cast all 100 of the last round's bows: this round draws the setting's 100 (a first round draws 60).
		w.b.gateAlchRoad.drawn, w.b.gateAlchRoad.casts, w.b.gateAlchRoad.left = 100, 100, 0
		return r, w
	}
	oldStaff := gateAlchFireStaff
	t.Cleanup(func() { gateAlchFireStaff = oldStaff })
	gateAlchFireStaff = func(*fleetrunner.Deps) bool { return true }

	r, w := mk(map[string]int{"Nature talisman": 7, "Magic shortbow id861": 72000})
	var asked []fleetplan.ItemCount
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		asked = append(asked, it...)
		for _, x := range it {
			r.sim.Give(bowMagicShortNot, x.Count) // the mule's trade: the bows arrive as notes
		}
		return fleetrunner.KitResult{Got: map[string]int{"Magic shortbow id861": 400}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	if why, ok := w.provision(); !ok {
		t.Fatalf("%s\n%v", why, r.logs)
	}
	if len(asked) != 1 || asked[0].Item != "Magic shortbow id861" || asked[0].Count != 400 {
		t.Fatalf("asked %v: want 400 Magic shortbow id861", asked)
	}
	p := r.sim.Pack()
	if simCount(p, bowMagicShortNot) != 100 || w.alch.drawn != 100 {
		t.Fatalf("pack %v drawn %d", p, w.alch.drawn)
	}
	if b := r.sim.Bank(); simCount(b, bowMagicShort) != 300 {
		t.Fatalf("the other 300 stay in the bank for the next rounds: %v", b)
	}
	if !r.logged("alch: the bank holds no bows: asking mule for 400 Magic shortbow id861") {
		t.Fatalf("log: %v", r.logs)
	}

	// A mule that publishes none of them is not asked, and the round goes on without alchemy.
	r2, w2 := mk(map[string]int{"Nature talisman": 7})
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		t.Errorf("the mule was asked for %v though it publishes no bows", it)
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{}
	}
	if why, ok := w2.provision(); !ok || simCount(r2.sim.Pack(), bowMagicShortNot) != 0 || w2.alch.drawn != 0 {
		t.Fatalf("%v %s drawn %d", ok, why, w2.alch.drawn)
	}
	if !r2.logged("alch: no bows were drawn") {
		t.Fatalf("log: %v", r2.logs)
	}

	// A mule that gives nothing: no alchemy, no failure.
	r3, w3 := mk(map[string]int{"Nature talisman": 7, "Magic shortbow id861": 72000})
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{"Magic shortbow id861": "declined: busy"}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	if why, ok := w3.provision(); !ok || w3.alch.drawn != 0 {
		t.Fatalf("%v %s drawn %d", ok, why, w3.alch.drawn)
	}
	_ = r3
}

// The cast itself, through the walk alchemy's own idle cast and the real object config: a noted Magic shortbow in the pack, the Staff of
// fire worn, Magic 78 and Natures: High Level Alchemy goes out on the note (OPHELDT: the note's id and slot, the pack, the spell), the
// account is locked by it, and nothing else goes out inside the lock.
func TestGateAlchSendsTheRealCastOnANotedBow(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ms runecraftrunner.AlchBowIDs
	for _, b := range runecraftrunner.ResolveAlchBows(cfg) {
		if b.Name == "Magic shortbow" {
			ms = b
		}
	}
	if ms.Noted == 0 {
		t.Fatal("no Magic shortbow in the config")
	}
	stackable(t, ms.Noted)
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 6000}, {ID: coinsObj, Count: 100}, {ID: ms.Noted, Count: 5}}, nil, nil)
	r.ctx.Config = cfg
	r.ctx.Components = actionsim.TabComponents()
	r.sim.ShowTabs()
	r.sim.Wear(actionsim.RightHand, 1387) // the Staff of fire
	mindHoldStat(r.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(78), 78)
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) {
		w.stand, w.haveStand = runecraftplan.GateStandTile, true
		w.alch = gateAlchState{on: true, bows: runecraftrunner.ResolveAlchBows(cfg), arrived: true, burnFrom: time.Now(), lastStack: 5}
	})
	w.d.Travel = &fleetrunner.Travel{Members: true}
	if !w.fireStaff() || w.alchStack() != 5 {
		t.Fatalf("fire staff worn %v, stack %d: the rig is wrong", w.fireStaff(), w.alchStack())
	}
	// The rig opens a bank for the bank tests; at the gate there is none, and an open interface stops a cast.
	r.sim.State().Handle(serverprot.IF_CLOSE, nil)
	if main, side, _ := r.sim.State().Modals(); main != gamestate.NoModal || side != gamestate.NoModal {
		t.Fatalf("interfaces still open: %d %d", main, side)
	}
	w.gateAlch()
	var casts [][]byte
	for _, b := range r.sim.Sent() {
		if len(b) >= 9 && int(b[len(b)-9]) == clientprot.OPHELDT {
			casts = append(casts, b[len(b)-9:])
		}
	}
	if len(casts) != 1 {
		t.Fatalf("%d casts sent (%s); log %v", len(casts), w.alch.why, r.logs)
	}
	if obj := int(casts[0][1])<<8 | int(casts[0][2]); obj != int(ms.Noted) {
		t.Fatalf("the cast named obj %d, want the note %d", obj, ms.Noted)
	}
	if !w.alchLocked() {
		t.Fatal("the account is not locked by its own cast")
	}
	for i := 0; i < 3; i++ {
		w.gateAlch()
	}
	n := 0
	for _, b := range r.sim.Sent() {
		if len(b) >= 9 && int(b[len(b)-9]) == clientprot.OPHELDT {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d casts inside the lock", n)
	}
	// The walk alchemy is off again outside the call.
	if w.d.Travel.Alch.Enabled {
		t.Fatal("the walk alchemy was left on")
	}
}
