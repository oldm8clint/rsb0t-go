package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/orderplan"
)

// NB3, the mule side: a donor's cancel drops its queued order.
func TestTheMuleDropsAnOrderItsDonorCancelled(t *testing.T) {
	b, ctx, bus := queueMule("acct64")
	b.handout = true
	b.handoutPolicy = orderplan.DefaultPolicy([]string{"acct64"})
	b.handoutLedger = orderplan.NewLedger(time.Hour)
	b.orders = map[string]string{}
	bus.Publish(mulebus.OrderMessage("acct64", orderplan.Format("Knife", 1), "mule"))
	b.pollBus(ctx)
	if _, ok := b.orders["acct64"]; !ok {
		t.Fatalf("control: the order was not queued: %v", b.orders)
	}
	c := mulebus.CancelMessage("acct64")
	c.To = "mule"
	bus.Publish(c)
	b.pollBus(ctx)
	if _, ok := b.orders["acct64"]; ok {
		t.Fatal("the mule kept an order its donor cancelled")
	}
}

// NB3, the mule side: an order its donor stopped re-publishing (it timed out and walked away, or
// crashed without a cancel) is dropped once muleOrderTTL passes, instead of being served later.
func TestTheMuleDropsAnOrderNobodyIsWaitingFor(t *testing.T) {
	b, ctx, bus := queueMule("acct64", "acct65")
	b.handout = true
	b.handoutPolicy = orderplan.DefaultPolicy([]string{"acct64", "acct65"})
	b.handoutLedger = orderplan.NewLedger(time.Hour)
	b.orders = map[string]string{}
	bus.Publish(mulebus.OrderMessage("acct64", orderplan.Format("Knife", 1), "mule"))
	bus.Publish(mulebus.OrderMessage("acct65", orderplan.Format("Knife", 1), "mule"))
	b.pollBus(ctx)
	// acct64 last re-published longer ago than the TTL; acct65 is still waiting.
	b.ordersAt["acct64"] = time.Now().Add(-muleOrderTTL - time.Second)
	b.pruneOrders(time.Now())
	if _, ok := b.orders["acct64"]; ok {
		t.Fatal("the mule kept an order nobody has re-published for longer than the TTL")
	}
	if _, ok := b.orders["acct65"]; !ok {
		t.Fatal("a still-waiting donor's order was dropped")
	}
}

// NB2 of the lane P re-review, the mule half: the queue depth its heartbeat publishes (and every
// donor's drawGate and every Scarab ask reads) counts the per-item hand-out ORDERS waiting, which
// are served ahead of donors. Live 2026-09-23: 23 hand-outs and 0 fisher trades in 5 minutes with
// 49 Scarab accounts, while the published queue read short.
func TestTheMuleQueueDepthCountsWaitingOrders(t *testing.T) {
	b, _, _ := queueMule("acct64", "acct65", "acct66")
	base := b.queueDepth()
	b.orders = map[string]string{"acct64": "need 1 Knife", "acct65": "need 6 Cake", "acct66": "need 1 Magic staff"}
	if got := b.queueDepth(); got != base+3 {
		t.Fatalf("queue depth with three orders waiting: %d, want %d", got, base+3)
	}
}
