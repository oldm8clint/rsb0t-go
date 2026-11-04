package woodcutrunner

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// sleepyDeps is a Deps whose Delay really waits a little, so goroutines queued on one stall
// interleave the way live accounts do.
func sleepyDeps() *Deps {
	return &Deps{Delay: func(time.Duration) bool { time.Sleep(2 * time.Millisecond); return true }}
}

// waitUntil polls cond for up to a second.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func queued(seller string) (pri, bg int) {
	t := turnFor(seller)
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.priWait, t.bgWait
}

// TestBackgroundBuyerYieldsTheStallToAPriorityBuyer is the MarketBulkBuy failure of 2026-09-25
// 20:40 in miniature: an axe upgrade holds the Seers Market's turn, the mule arrives with 9,500 coins
// for chaos runes, and must be the next one served -- not lose to the axe buyer behind it, and not
// give up after thirty seconds.
func TestBackgroundBuyerYieldsTheStallToAPriorityBuyer(t *testing.T) {
	const stall = "Priority Test Stall"
	d := sleepyDeps()

	axe, ok := acquireMarketTurnAs(d, stall, time.Second, true)
	if !ok {
		t.Fatal("a background buyer could not take a free stall")
	}
	if axe.priorityWaiting() {
		t.Fatal("priorityWaiting before anyone queued")
	}

	var bulkGot atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		if g, ok := acquireMarketTurnAs(d, stall, 2*time.Second, false); ok {
			bulkGot.Store(true)
			g.release()
		}
	}()
	waitUntil(t, "the priority buyer to queue", func() bool { p, _ := queued(stall); return p == 1 })

	// The holder can see it, which is what BuyAtMarket checks between asks.
	if !axe.priorityWaiting() {
		t.Fatal("a background holder cannot see the priority buyer waiting behind it")
	}
	// Another axe buyer arriving now is told "busy" at once rather than queueing ahead of or
	// behind the priority buyer.
	start := time.Now()
	if _, ok := acquireMarketTurnAs(d, stall, time.Second, true); ok {
		t.Fatal("a second background buyer took a turn while a priority buyer was waiting")
	}
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("a background buyer stood %s in a queue it may not join; want an immediate refusal", waited)
	}
	if turnFor(stall).mayQueue(true) {
		t.Error("mayQueue(background) = true with a priority buyer waiting")
	}

	axe.release()
	<-done
	if !bulkGot.Load() {
		t.Fatal("the priority buyer did not get the turn the background buyer gave up")
	}
}

// TestFreeTurnGoesToTheWaitingPriorityBuyer: when the turn frees while both kinds are waiting, the
// background buyer must not snatch it on its next poll.
func TestFreeTurnGoesToTheWaitingPriorityBuyer(t *testing.T) {
	const stall = "Free Turn Test Stall"
	d := sleepyDeps()
	holder, ok := acquireMarketTurnAs(d, stall, time.Second, false)
	if !ok {
		t.Fatal("could not take a free stall")
	}
	var order []string
	var mu = make(chan struct{}, 1)
	record := func(who string) { mu <- struct{}{}; order = append(order, who); <-mu }

	bgDone := make(chan bool)
	go func() {
		g, ok := acquireMarketTurnAs(d, stall, time.Second, true)
		if ok {
			record("axe")
			g.release()
		}
		bgDone <- ok
	}()
	waitUntil(t, "the background buyer to queue", func() bool { _, b := queued(stall); return b == 1 })

	priDone := make(chan bool)
	go func() {
		g, ok := acquireMarketTurnAs(d, stall, 2*time.Second, false)
		if ok {
			record("chaos")
			time.Sleep(20 * time.Millisecond)
			g.release()
		}
		priDone <- ok
	}()
	waitUntil(t, "the priority buyer to queue", func() bool { p, _ := queued(stall); return p == 1 })

	holder.release()
	if !<-priDone {
		t.Fatal("the priority buyer never got the freed turn")
	}
	<-bgDone
	if len(order) == 0 || order[0] != "chaos" {
		t.Fatalf("service order %v, want the priority buyer first", order)
	}
}

// TestBackgroundQueueIsCappedFleetWide: at most marketBackgroundQueueCap axe buyers stand waiting
// for one stall. The rest are refused at once and back off, instead of 24 accounts each polling
// the stall every 90 seconds.
func TestBackgroundQueueIsCappedFleetWide(t *testing.T) {
	const stall = "Queue Cap Test Stall"
	d := sleepyDeps()
	holder, ok := acquireMarketTurnAs(d, stall, time.Second, true)
	if !ok {
		t.Fatal("could not take a free stall")
	}
	firstDone := make(chan bool)
	go func() {
		g, ok := acquireMarketTurnAs(d, stall, 2*time.Second, true)
		if ok {
			g.release()
		}
		firstDone <- ok
	}()
	waitUntil(t, "the first background buyer to queue", func() bool { _, b := queued(stall); return b == marketBackgroundQueueCap })

	if turnFor(stall).mayQueue(true) {
		t.Error("mayQueue(background) = true with the queue full")
	}
	start := time.Now()
	if _, ok := acquireMarketTurnAs(d, stall, time.Second, true); ok {
		t.Fatal("a background buyer beyond the cap got the turn")
	}
	if waited := time.Since(start); waited > 200*time.Millisecond {
		t.Errorf("a background buyer beyond the cap waited %s; want an immediate refusal", waited)
	}
	// An ordinary buyer is never capped.
	if !turnFor(stall).mayQueue(false) {
		t.Error("mayQueue(ordinary) = false; priority buyers are never capped")
	}
	holder.release()
	if !<-firstDone {
		t.Fatal("the queued background buyer did not get the turn when it freed")
	}
	if p, b := queued(stall); p != 0 || b != 0 {
		t.Errorf("queue counts left at %d/%d after everyone left", p, b)
	}
}

// slot is one pack slot holding count of id.
func slot(id, count int32) gamestate.SlotItem {
	return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: count}}
}

// fakeObjs is objNames over a fixed table.
type fakeObjs map[int]cachecfg.Obj

func (f fakeObjs) Obj(id int) (cachecfg.Obj, bool) { o, ok := f[id]; return o, ok }

// TestMarketReceiptCountsANotedAxe is the adamant-axe loss: the stall hands an axe over as a
// NOTE, and the receipt counted only unnoted items (packCountByName, since 2026-09-23), so a
// purchase that landed read as "the trade closed without the item arriving" and the trip bought
// another. acct141 and Acct215 each hold five adamant axes from it.
func TestMarketReceiptCountsANotedAxe(t *testing.T) {
	objs := fakeObjs{
		1357: {ID: 1357, Name: "Adamant axe", CertLink: -1, CertTemplate: -1},
		1358: {ID: 1358, Name: "Adamant axe", CertLink: 1357, CertTemplate: 799},
		995:  {ID: 995, Name: "Coins", CertLink: -1, CertTemplate: -1},
	}
	before := marketReceiptCount([]gamestate.SlotItem{slot(995, 146850)}, "Adamant axe", objs)
	after := marketReceiptCount([]gamestate.SlotItem{slot(995, 143700), slot(1358, 1)}, "Adamant axe", objs)
	if before != 0 || after != 1 {
		t.Fatalf("receipt count before %d, after a noted axe arrived %d; want 0 then 1", before, after)
	}
	if got := marketReceiptCount([]gamestate.SlotItem{slot(1357, 1), slot(1358, 2)}, " adamant AXE ", objs); got != 3 {
		t.Errorf("unnoted 1 + noted 2 counted %d, want 3", got)
	}
	if got := marketReceiptCount([]gamestate.SlotItem{slot(1358, 1)}, "Adamant axe", nil); got != 0 {
		t.Errorf("no config counted %d, want 0", got)
	}
}

// TestBuyAtMarketLeavesAStockTheFleetKnowsIsEmptyAlone: once the stall has told any account it
// has no rune axe, a buyer does not walk, does not take the turn and sends nothing -- and the
// refusal still reads as a stock answer so the per-account tier ladder moves down.
func TestBuyAtMarketLeavesAStockTheFleetKnowsIsEmptyAlone(t *testing.T) {
	const stall = "Sold Out Test Stall"
	s := &sentSession{state: gamestate.New()}
	d := &Deps{Session: s, Delay: func(time.Duration) bool { return true }}
	o := MarketBuyOptions{Seller: stall, Item: "Rune axe", Qty: 1, Price: 150000, MaxCoins: 165000, Background: true}

	until := fleetSoldOut.Mark(stall, "Rune axe", time.Now())
	defer fleetSoldOut.Clear(stall, "Rune axe")
	if u, ok := MarketSoldOutUntil(stall, "rune axe", time.Now()); !ok || !u.Equal(until) {
		t.Fatalf("MarketSoldOutUntil = %v,%v, want %v,true", u, ok, until)
	}

	res := BuyAtMarket(d, o)
	if !res.SoldOut || res.Got || res.Busy {
		t.Fatalf("BuyAtMarket = %+v, want SoldOut and nothing else", res)
	}
	if !woodcutplan.MarketRefusalIsStock(res.Reason) {
		t.Errorf("reason %q is not read as a stock refusal, so the tier ladder would not move down", res.Reason)
	}
	if !strings.Contains(res.Reason, until.Format("15:04")) {
		t.Errorf("reason %q does not say until when", res.Reason)
	}
	if s.sent != 0 {
		t.Errorf("sent %d packets for an item the fleet knows is sold out", s.sent)
	}
	if _, ok := MarketSoldOutUntil("", "Rune axe", time.Now()); ok {
		t.Error("an unnamed stall matched a note")
	}

	// A buyer with priority is NOT turned away by the note: it goes on to its own checks (here the
	// missing price, which needs no session), because its purchase cannot wait hours.
	pri := o
	pri.Background, pri.Price = false, 0
	if r := BuyAtMarket(d, pri); r.SoldOut || !strings.Contains(r.Reason, "no price is configured") {
		t.Errorf("a priority buyer was stopped by the sold-out note: %+v", r)
	}
}
