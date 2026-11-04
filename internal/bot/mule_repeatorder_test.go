package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/orderplan"
)

func orderMule() *muleBot {
	return &muleBot{
		handout:       true,
		handoutPolicy: orderplan.DefaultPolicy([]string{"acct64"}),
		handoutLedger: orderplan.NewLedger(time.Hour),
		orders:        map[string]string{},
	}
}

func orderCtx() *Context {
	return NewContext(nil, "mule", nil, nil, nil, nil, mulebus.NewBus(), nil, nil, nil, func(string) {})
}

// acct64, 2026-09-22 15:43:43-48: the mule delivered 15 Cake, the next bus poll re-queued the
// same "need 15 Cake" the donor had re-published while it waited, and the mule served it again to
// a donor already asking for Chaos runes. A repeat of the order just served, polled inside
// orderplan.RepeatWindow, must not be queued. CONTROL: the same message with nothing served yet
// IS queued, which is also exactly what the code before this change did after a delivery.
func TestTheMuleDoesNotRequeueTheOrderItJustServed(t *testing.T) {
	msg := mulebus.OrderMessage("acct64", orderplan.Format("Knife", 1), "mule")

	fresh := orderMule()
	fresh.takeOrder(orderCtx(), msg)
	if _, ok := fresh.orders["acct64"]; !ok {
		t.Fatalf("control: an order nothing has served must be queued, orders=%v", fresh.orders)
	}

	b := orderMule()
	b.served = map[string]servedOrder{"acct64": {text: msg.Text, at: time.Now().Add(-2 * time.Second)}}
	b.takeOrder(orderCtx(), msg)
	if _, ok := b.orders["acct64"]; ok {
		t.Fatal("the re-publish of the order just served was queued again")
	}

	// The donor's NEXT ask is a different text and is a new order.
	next := mulebus.OrderMessage("acct64", orderplan.Format("Coins", 60), "mule")
	b.takeOrder(orderCtx(), next)
	if got := b.orders["acct64"]; got != next.Text {
		t.Fatalf("a different order right after a hand-out was not queued, orders=%v", b.orders)
	}

	// And the same ask again, well after the window, is a genuine re-ask.
	late := orderMule()
	late.served = map[string]servedOrder{"acct64": {text: msg.Text, at: time.Now().Add(-orderplan.RepeatWindow - time.Second)}}
	late.takeOrder(orderCtx(), msg)
	if _, ok := late.orders["acct64"]; !ok {
		t.Fatal("the same order after the repeat window must be queued")
	}
}

// The mule now reads its bus from inside every wait, so a donor's re-publish can arrive WHILE the
// hand-out it repeats is still running, before b.served is written. It must not be queued either.
func TestAReRepublishHeardDuringTheHandOutIsNotQueued(t *testing.T) {
	msg := mulebus.OrderMessage("acct64", orderplan.Format("Knife", 1), "mule")
	b := orderMule()
	b.serving = servingOrder{key: "acct64", text: msg.Text}
	b.takeOrder(orderCtx(), msg)
	if _, ok := b.orders["acct64"]; ok {
		t.Fatal("a re-publish of the order being served was queued again")
	}
	other := mulebus.OrderMessage("acct65", orderplan.Format("Knife", 1), "mule")
	b.handoutPolicy = orderplan.DefaultPolicy([]string{"acct64", "acct65"})
	b.takeOrder(orderCtx(), other)
	if _, ok := b.orders["acct65"]; !ok {
		t.Fatal("another donor's order during a hand-out must still be queued")
	}
}

// A repeat of the order just served must not be ANSWERED either. Its own hand-out has just spent
// the donor's window, so the screen would refuse it "already handed you one recently" -- and a
// KindDeclined names no item, so the donor's NEXT Await (a different item) reads it as its own
// answer. CONTROL: a genuinely new order from the same donor inside the spent window is still
// declined out loud, as before.
func TestARepeatOfTheOrderJustServedIsNotDeclined(t *testing.T) {
	msg := mulebus.OrderMessage("acct64", orderplan.Format("Knife", 1), "mule")
	b := orderMule()
	now := time.Now()
	b.handoutLedger.Record(now.Add(-2*time.Second), "acct64", "Knife", 1)
	b.served = map[string]servedOrder{"acct64": {text: msg.Text, at: now.Add(-2 * time.Second)}}
	ctx := orderCtx()
	w := mulebus.NewWatcher()
	b.takeOrder(ctx, msg)
	for _, m := range w.Poll(ctx.Bus) {
		if m.Kind == mulebus.KindDeclined {
			t.Fatalf("the repeat of the order just served was answered with a decline: %q", m.Reason)
		}
	}

	next := mulebus.OrderMessage("acct64", orderplan.Format("Coins", 60), "mule")
	b.takeOrder(ctx, next)
	declined := false
	for _, m := range w.Poll(ctx.Bus) {
		declined = declined || m.Kind == mulebus.KindDeclined
	}
	if !declined {
		t.Fatal("control: a new order inside the spent window must still be declined")
	}
}
