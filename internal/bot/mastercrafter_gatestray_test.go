package bot

// The gate's stray pack items (mastercrafter_gatestray.go) and the room he says he has (publish/republish/capacityChanged): live 16 Oct 2026 a
// Strange fruit left in the crafter's pack by a random event made his trade 24 essence, every carrier's Offer-All of 25 was declined 121
// times, and nothing took the fruit out of the pack.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

const strangeFruit = fleetplan.StrangeFruitID

// fruitRig is the gate world with a Strange fruit in the pack (the talisman, the Natures, the fare and the fruit: 24 free slots). Eating the
// fruit works when eat is set (the simulator scripts content's [opheld1,macro_triffidfruit]).
func fruitRig(t *testing.T, eat bool, extra ...actionsim.Stack) (*masterRig, *masterNatureWorld) {
	t.Helper()
	pack := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}, {ID: strangeFruit, Count: 1}}
	pack = append(pack, extra...)
	r := newNatureRig(t, pack, nil, nil)
	if eat {
		r.sim.Bury(0, 0, map[int32]int{strangeFruit: 0})
	}
	r.sim.StandAt(runecraftplan.GateStandTile.X, runecraftplan.GateStandTile.Z)
	w := newNatureWorld(t, r, func(w *masterNatureWorld) {
		w.stand, w.haveStand = runecraftplan.GateStandTile, true
		w.set.Runners = []string{"acct_1", "acct_2"}
	})
	return r, w
}

// eatsSent is how many Eat clicks (OPHELD1) the session has sent.
func eatsSent(sim *actionsim.Session) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) > 0 && int(b[0]) == clientprot.OPHELD1 {
			n++
		}
	}
	return n
}

func gateRoom(r *masterRig) int {
	g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now().Add(time.Millisecond), 0)
	if !g.Heard {
		return -1
	}
	return g.Capacity
}

// The fruit is eaten in the idle polls, with one click and no wait, and the gate says it takes 25 again in the same poll.
func TestGateIdleEatsAStrangeFruitAndTheRoomComesBack(t *testing.T) {
	r, w := fruitRig(t, true)
	w.publish(mulebus.GateOpen, 0)
	if got := gateRoom(r); got != 24 {
		t.Fatalf("the gate says it takes %d with a fruit in the pack, want 24", got)
	}
	started := time.Now()
	w.gateIdle()
	if took := time.Since(started); took > 200*time.Millisecond {
		t.Fatalf("the idle poll stood still for %s: the eat must be one click, not a wait", took)
	}
	if eatsSent(r.sim) != 1 || runecraftrunner.PackCount(w.d, strangeFruit) != 0 {
		t.Fatalf("%d Eat click(s), %d fruit left", eatsSent(r.sim), runecraftrunner.PackCount(w.d, strangeFruit))
	}
	if got := gateRoom(r); got != runecraftplan.CarrierOffer {
		t.Fatalf("the gate still says it takes %d the moment the fruit is gone, want %d (the carriers believe the last message)", got, runecraftplan.CarrierOffer)
	}
	if !r.logged("stray: 1 Strange fruit") {
		t.Fatalf("log: %v", r.logs)
	}
	// The next look says it is gone, once.
	w.stray.lookAt = time.Time{}
	w.gateIdle()
	if !r.logged("the Strange fruit is gone from the pack (1 eaten this round)") || eatsSent(r.sim) != 1 {
		t.Fatalf("%d Eat click(s); log: %v", eatsSent(r.sim), r.logs)
	}
}

// An op on an item clears a pending action like an op on a player does: it never goes out with a screen open, a request waiting or the
// account locked by a cast, and not in a random event.
func TestGateIdleDoesNotEatWhenItWouldCostATrade(t *testing.T) {
	// A trade screen is open.
	r, w := fruitRig(t, true)
	r.sim.OpenTrade(&actionsim.Trade{Partner: "Acct 1"})
	w.gateIdle()
	w.stray.lookAt = time.Time{}
	w.clearStray()
	if eatsSent(r.sim) != 0 {
		t.Fatalf("an Eat click went out under an open trade screen")
	}
	// A request waits: the answer comes first.
	r2, w2 := fruitRig(t, true)
	r2.sim.ShowPlayers("Acct 1")
	r2.ctx.Chat.Record("Acct 1:tradereq:")
	runecraftrunner.GateStep(w2.d, w2.watch, runecraftrunner.GateOptions{Wait: 30 * time.Millisecond, OnStand: func() bool { return false }})
	if w2.watch.Pending() != 1 {
		t.Fatalf("pending %d", w2.watch.Pending())
	}
	w2.stray.lookAt = time.Time{}
	w2.clearStray()
	if eatsSent(r2.sim) != 0 {
		t.Fatalf("an Eat click went out with a request waiting")
	}
	// The last cast's lock is not over: the engine would drop the click.
	r3, w3 := fruitRig(t, true)
	w3.alch.castAt = time.Now()
	w3.clearStray()
	if eatsSent(r3.sim) != 0 {
		t.Fatalf("an Eat click went out inside a cast's lock")
	}
	// The same pack with nothing in the way: it goes.
	w3.alch.castAt = time.Time{}
	w3.stray.lookAt = time.Time{}
	w3.clearStray()
	if eatsSent(r3.sim) != 1 {
		t.Fatalf("no Eat click with nothing in the way")
	}
}

// A fruit that stays after gateStrayTries clicks (the click is dropped every time) is given up on, said once, and not clicked at for ever.
func TestGateIdleGivesUpOnAFruitThatStays(t *testing.T) {
	r, w := fruitRig(t, false)
	for i := 0; i < 3*gateStrayTries; i++ {
		w.stray.lookAt, w.stray.eatAt = time.Time{}, time.Time{}
		w.clearStray()
	}
	if got := eatsSent(r.sim); got != gateStrayTries {
		t.Fatalf("%d Eat clicks, want %d and no more", got, gateStrayTries)
	}
	if !r.logged("WARNING: 1 Strange fruit stay in the pack after 6 Eat clicks: the gate takes 24 essence a trade") {
		t.Fatalf("log: %v", r.logs)
	}
	// Said once.
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, "stay in the pack after") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the give-up was said %d times", n)
	}
}

// Anything else the gate has no use for is not touched (it could be worth something): it is named, once, and the gate says what it takes.
func TestGateIdleNamesOtherStraysAndLeavesThemAlone(t *testing.T) {
	r, w := fruitRig(t, true, actionsim.Stack{ID: 2000, Count: 1}, actionsim.Stack{ID: 2001, Count: 1})
	r.sim.Remove(strangeFruit, 1)
	w.stray.lookAt = time.Time{}
	w.clearStray()
	w.stray.lookAt = time.Time{}
	w.clearStray()
	if eatsSent(r.sim) != 0 || runecraftrunner.PackCount(w.d, 2000) != 1 || runecraftrunner.PackCount(w.d, 2001) != 1 {
		t.Fatalf("%d Eat click(s); the strays must stay", eatsSent(r.sim))
	}
	n := 0
	for _, l := range r.logs {
		if strings.Contains(l, "item 2000 x1, item 2001 x1, which the gate has no use for") && strings.Contains(l, "the gate takes 23 essence a trade") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the strays were named %d times: %v", n, r.logs)
	}
}

// What the gate uses is not a stray: the talisman, the Natures, the fare, the essence a trade brings and the noted bows.
func TestStraysAreOnlyWhatTheGateHasNoUseFor(t *testing.T) {
	r, w := alchRig(t, 40, nil)
	r.sim.Give(runecraftplan.EssenceID, 10)
	r.sim.Give(runecraftplan.NotedEssenceID, 5)
	if got := runecraftrunner.Strays(w.d, w.alch.bows); len(got) != 0 {
		t.Fatalf("the gate pack holds strays: %v", got)
	}
	r.sim.Give(strangeFruit, 1)
	r.sim.Give(2000, 1)
	if got := runecraftrunner.Strays(w.d, w.alch.bows); len(got) != 2 {
		t.Fatalf("strays %v, want the fruit and the other item", got)
	}
}

// A change in the room he has is on the bus the moment it happens, not at the next beat: the carriers read it from there.
func TestGateRepublishesTheRoomTheMomentItChanges(t *testing.T) {
	r, w := fruitRig(t, false)
	r.sim.Remove(strangeFruit, 1)
	w.publish(mulebus.GateOpen, 0)
	if got := gateRoom(r); got != runecraftplan.CarrierOffer {
		t.Fatalf("room %d", got)
	}
	// Inside the beat interval nothing is published for the same state...
	w.capacityChanged()
	// ... until the room changes.
	r.sim.Give(strangeFruit, 1)
	w.capacityChanged()
	if got := gateRoom(r); got != 24 {
		t.Fatalf("the gate still says %d after a fruit took a slot", got)
	}
	r.sim.Remove(strangeFruit, 1)
	w.capacityChanged()
	if got := gateRoom(r); got != runecraftplan.CarrierOffer {
		t.Fatalf("the gate still says %d after the fruit went", got)
	}
	// A closed gate is not announced again by a change.
	w.publish(mulebus.GateClosed, 0)
	r.sim.Give(strangeFruit, 1)
	w.capacityChanged()
	if g := mulebus.ReadGate(r.ctx.Bus, "main", time.Now().Add(time.Millisecond), 0); g.State != mulebus.GateClosed {
		t.Fatalf("a closed gate was reopened by a change of room: %+v", g)
	}
}

// The whole thing through the gate step: a carrier that offers 25 into 24 free slots is declined, and the room is republished with the
// decline so its next offer is the right one.
func TestGateDeclineRepublishesTheRoom(t *testing.T) {
	r, w := fruitRig(t, false)
	r.sim.ShowPlayers("Acct 1")
	// The gate said 25 before the fruit came.
	r.sim.Remove(strangeFruit, 1)
	w.publish(mulebus.GateOpen, 0)
	r.sim.Give(strangeFruit, 1)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 25}}, AcceptOffer: true, AcceptConfirm: true}
	r.sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			r.sim.OpenTrade(partner)
		}
	}
	w.watch = runecraftrunner.NewGateWatcher(w.d)
	r.ctx.Chat.Record("Acct 1:tradereq:")
	if res := w.Gate(); !res.OK {
		t.Fatalf("%+v\n%v", res, r.logs)
	}
	if !r.logged("declined: offered 25 essence and only 24 slots are free") || !r.logged("the gate says it takes 24 a trade") {
		t.Fatalf("log: %v", r.logs)
	}
	if got := gateRoom(r); got != 24 {
		t.Fatalf("the gate says %d after declining 25 into 24 free slots", got)
	}
	if w.trades != 0 {
		t.Fatalf("a trade was counted: %d", w.trades)
	}
}

// heldOpsSent is how many inventory-option clicks (OPHELD1-5) the session has sent.
func heldOpsSent(sim *actionsim.Session) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) == 0 {
			continue
		}
		switch int(b[0]) {
		case clientprot.OPHELD1, clientprot.OPHELD2, clientprot.OPHELD3, clientprot.OPHELD4, clientprot.OPHELD5:
			n++
		}
	}
	return n
}

// A Drunken Dwarf's Kebab and Beer and a random event's Casket are used up in the idle polls, one click each (live 17 Oct 2026 the three
// sat in the pack from 06:39 and every trade took 21 essence). The real item config names them and gives the option.
func TestGateIdleUsesUpAKebabABeerAndACasket(t *testing.T) {
	cfg := bowCfg(t)
	ids := map[string]int32{}
	for _, id := range []int32{1971, 1917, 405} {
		o, ok := cfg.Obj(int(id))
		if !ok {
			t.Fatalf("no obj %d in the config", id)
		}
		ids[o.Name] = id
	}
	for name := range gateConsumables {
		if ids[name] == 0 {
			t.Fatalf("the config names %v, want Kebab, Beer and Casket", ids)
		}
	}
	r, w := fruitRig(t, false, actionsim.Stack{ID: ids["Kebab"], Count: 1}, actionsim.Stack{ID: ids["Beer"], Count: 1}, actionsim.Stack{ID: ids["Casket"], Count: 1})
	r.sim.Remove(strangeFruit, 1)
	r.ctx.Config = cfg
	// The server takes each item on its click (eaten, drunk, opened into coins); the simulator does not, so the test does.
	for _, name := range []string{"Kebab", "Beer", "Casket"} {
		w.stray.lookAt, w.stray.usedAt = time.Time{}, time.Time{}
		w.clearStray()
		r.sim.Remove(ids[name], 1)
	}
	if got := heldOpsSent(r.sim); got != 3 {
		t.Fatalf("%d inventory clicks, want one for each of the three: %v", got, r.logs)
	}
	for _, name := range []string{"Kebab", "Beer", "Casket"} {
		said := false
		for _, l := range r.logs {
			if strings.Contains(l, "stray: "+name+" x1 (store value") {
				said = true
			}
		}
		if !said {
			t.Errorf("no stray line for the %s: %v", name, r.logs)
		}
	}
	// A fourth poll sends nothing new for a kind already clicked once while its click is gateStrayEvery old.
	w.stray.lookAt = time.Time{}
	w.clearStray()
	if got := heldOpsSent(r.sim); got != 3 {
		t.Fatalf("%d inventory clicks after a poll inside gateStrayEvery, want 3", got)
	}
}

// A cheap stray the gate cannot use up (the Beer's empty glass, a Casket's uncut sapphire) is dropped; a dear one is left alone and named.
func TestGateIdleDropsACheapStrayAndKeepsADearOne(t *testing.T) {
	cfg := bowCfg(t)
	const glass, sapphire, platebody = 1919, 1623, 1127
	for id, want := range map[int]string{glass: "Beer glass", sapphire: "Uncut sapphire", platebody: "Rune platebody"} {
		if o, ok := cfg.Obj(id); !ok || o.Name != want {
			t.Fatalf("obj %d is %q, want %q", id, o.Name, want)
		}
	}
	if o, _ := cfg.Obj(platebody); o.Cost <= gateStrayDropMax {
		t.Fatalf("the platebody costs %d, the test needs one over %d", o.Cost, gateStrayDropMax)
	}
	r, w := fruitRig(t, false, actionsim.Stack{ID: glass, Count: 1}, actionsim.Stack{ID: sapphire, Count: 1}, actionsim.Stack{ID: platebody, Count: 1})
	r.sim.Remove(strangeFruit, 1)
	r.ctx.Config = cfg
	for _, id := range []int32{glass, sapphire} {
		w.stray.lookAt, w.stray.usedAt = time.Time{}, time.Time{}
		w.clearStray()
		r.sim.Remove(id, 1)
	}
	if got := heldOpsSent(r.sim); got != 2 {
		t.Fatalf("%d inventory clicks, want a drop for the glass and the sapphire: %v", got, r.logs)
	}
	w.stray.lookAt, w.stray.usedAt = time.Time{}, time.Time{}
	w.clearStray()
	if got := heldOpsSent(r.sim); got != 2 || runecraftrunner.PackCount(w.d, platebody) != 1 {
		t.Fatalf("the platebody was clicked (%d clicks) or left the pack", got)
	}
	named := false
	for _, l := range r.logs {
		if strings.Contains(l, "Rune platebody x1, which the gate has no use for") {
			named = true
		}
	}
	if !named {
		t.Errorf("the platebody was not named: %v", r.logs)
	}
}

// A random event's Lamp is cheap by store value but the guardian rubs it: the gate never drops it.
func TestGateIdleNeverDropsAGenieLamp(t *testing.T) {
	cfg := bowCfg(t)
	const lamp = 2528
	if o, ok := cfg.Obj(lamp); !ok || o.Name != "Lamp" || o.Cost > gateStrayDropMax {
		t.Fatalf("obj %d is %+v, want a cheap Lamp", lamp, o)
	}
	r, w := fruitRig(t, false, actionsim.Stack{ID: lamp, Count: 1})
	r.sim.Remove(strangeFruit, 1)
	r.ctx.Config = cfg
	w.stray.lookAt, w.stray.usedAt = time.Time{}, time.Time{}
	w.clearStray()
	if got := heldOpsSent(r.sim); got != 0 || runecraftrunner.PackCount(w.d, lamp) != 1 {
		t.Fatalf("the Lamp was clicked (%d) or left the pack", got)
	}
}
