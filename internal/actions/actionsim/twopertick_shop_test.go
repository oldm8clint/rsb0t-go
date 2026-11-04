package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
)

// Two actions a tick (twopertick-1012): a shop buys and sells five clicks every tick.

const shopCoins = 995

func shopObjs() actionsim.Objs {
	return actionsim.Objs{Stackable: func(id int32) bool { return id == fbRune || id == shopCoins }}
}

func shopSim(t *testing.T, coins int, stock int, packRunes int) (*actionsim.Session, *actions.Shop) {
	t.Helper()
	pack := []actionsim.Stack{{ID: shopCoins, Count: coins}}
	if packRunes > 0 {
		pack = append(pack, actionsim.Stack{ID: fbRune, Count: packRunes})
	}
	sim := actionsim.New(shopObjs(), pack)
	sim.OpenShop([]actionsim.Stack{{ID: fbRune, Count: stock}}, shopCoins, 1)
	runTicks(t, sim, 30*time.Millisecond)
	return sim, actions.NewShop(sim)
}

// 120 runes in Buy-10 clicks are 12 clicks: 5, 5 and 2 in three CONSECUTIVE ticks, then one confirm.
func TestBuyCountStreamsFiveClicksATick(t *testing.T) {
	sim, shop := shopSim(t, 5000, 100000, 0)
	start := time.Now()
	got, err := shop.BuyCount(fbRune, 120, realWaiter{})
	took := time.Since(start)
	if err != nil || got != 120 {
		t.Fatalf("BuyCount = %d, %v; want 120", got, err)
	}
	if countOf(sim.Pack(), fbRune) != 120 || countOf(sim.Pack(), shopCoins) != 4880 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	most, ticks := maxPerTick(sim)
	if most != 5 || ticks != 3 {
		t.Fatalf("per tick %v, want clicks of 5, 5 and 2", sim.PerTick())
	}
	if got := sim.MaxBacklog(); got > actionsim.UserEventsPerTick {
		t.Fatalf("%d packets waiting when a tick began", got)
	}
	// Three bursts a tick apiece, not a wait for the pack to change and another tick between: the whole purchase spans
	// the three ticks (a little slack for a loaded machine's scheduling; the old rounds needed about eight).
	first, last := -1, -1
	for i, n := range sim.PerTick() {
		if n > 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if span := last - first + 1; span > 5 {
		t.Fatalf("the bursts span %d ticks: %v", span, sim.PerTick())
	}
	t.Logf("120 runes in %s over %v", took.Round(time.Millisecond), sim.PerTick())
}

// Short of coins: the first pass buys what it can, the next plans the shortfall, finds nothing moves and stops.
func TestBuyCountStopsWhenTheCoinsRunOut(t *testing.T) {
	sim, shop := shopSim(t, 35, 100000, 0)
	got, err := shop.BuyCount(fbRune, 200, realWaiter{})
	if err != nil || got != 35 {
		t.Fatalf("BuyCount = %d, %v; want the 35 the coins paid for", got, err)
	}
	if countOf(sim.Pack(), shopCoins) != 0 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	sent := 0
	for _, n := range sim.PerTick() {
		sent += n
	}
	// 20 Buy-10 clicks are planned (four bursts); two quiet looks end a pass, so the doomed bursts stop at three.
	if sent > 20 {
		t.Fatalf("%d clicks were handled for 35 runes", sent)
	}
}

// The stock caps the plan: asking for more than the shelf holds sends only the clicks that cover it.
func TestBuyCountIsCappedByTheStock(t *testing.T) {
	sim, shop := shopSim(t, 5000, 25, 0)
	got, err := shop.BuyCount(fbRune, 200, realWaiter{})
	if err != nil || got != 25 {
		t.Fatalf("BuyCount = %d, %v; want 25", got, err)
	}
	sent := 0
	for _, n := range sim.PerTick() {
		sent += n
	}
	if sent != 3 {
		t.Fatalf("%d clicks, want Buy-10, Buy-10, Buy-5", sent)
	}
}

// Selling streams the same way.
func TestSellCountStreamsFiveClicksATick(t *testing.T) {
	sim, shop := shopSim(t, 0, 10, 130)
	got, err := shop.SellCount(fbRune, 130, realWaiter{})
	if err != nil || got != 130 {
		t.Fatalf("SellCount = %d, %v; want 130", got, err)
	}
	if countOf(sim.Pack(), fbRune) != 0 || countOf(sim.Pack(), shopCoins) != 130 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	most, ticks := maxPerTick(sim)
	if most != 5 || ticks != 3 {
		t.Fatalf("per tick %v, want clicks of 5, 5 and 3", sim.PerTick())
	}
}

// LastPass says what the pass did, for the caller's log.
func TestShopLastPassIsALogLine(t *testing.T) {
	_, shop := shopSim(t, 5000, 100000, 0)
	if _, err := shop.BuyCount(fbRune, 60, realWaiter{}); err != nil {
		t.Fatal(err)
	}
	line := shop.LastPass()
	if want := "shop: bought 60 of 60 asked: 6 click(s) in 2 tick(s) of at most 5"; len(line) < len(want) || line[:len(want)] != want {
		t.Fatalf("LastPass = %q, want it to start %q", line, want)
	}
}

// An unstackable item in many slots: a sale takes from the lowest slots, so click i must name the slot the quantity
// before it leaves at the front, or only the first click of a burst lands. 25 sharks are 10, 10 and 5: three clicks,
// all handled, none refused.
func TestSellCountNamesADifferentSlotForEveryClick(t *testing.T) {
	var pack []actionsim.Stack
	for i := 0; i < 25; i++ {
		pack = append(pack, actionsim.Stack{ID: fbShark, Count: 1})
	}
	sim := actionsim.New(shopObjs(), pack)
	sim.OpenShop([]actionsim.Stack{{ID: fbRune, Count: 10}}, shopCoins, 3)
	runTicks(t, sim, 30*time.Millisecond)
	shop := actions.NewShop(sim)

	got, err := shop.SellCount(fbShark, 25, realWaiter{})
	if err != nil || got != 25 {
		t.Fatalf("SellCount = %d, %v; want 25", got, err)
	}
	if countOf(sim.Pack(), fbShark) != 0 || countOf(sim.Pack(), shopCoins) != 75 {
		t.Fatalf("pack = %v", sim.Pack())
	}
	if sim.Refused() != 0 {
		t.Fatalf("the engine refused %d click(s): they named a slot an earlier click had emptied", sim.Refused())
	}
	handled := 0
	for _, n := range sim.PerTick() {
		handled += n
	}
	if handled != 3 {
		t.Fatalf("%d clicks handled, want 3 (Sell-10, Sell-10, Sell-5)", handled)
	}
}
