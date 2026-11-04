package donorrunner

import (
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/orderplan"
)

// A world errand's kit waits in the mule's line for 20-30 minutes (scarabplan.ErrandOrderWait):
// KitAsk.Hold lets it notice the mule leaving and give the wait up, and KitAsk.Republish puts back a
// kit a restarted mule lost. Neither needs the cache config: the mule here only ever queues the kit.

// queueOnlyMule answers every kit order "queued" and never plans it: a long line.
func queueOnlyMule(bus *mulebus.Bus, stop <-chan struct{}, orders *atomic.Int32, cancels *atomic.Int32) {
	w := mulebus.NewWatcher()
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			for _, msg := range w.Poll(bus) {
				switch {
				case msg.Kind == mulebus.KindKitOrder:
					orders.Add(1)
					bus.Publish(mulebus.KitAnswerMessage("mule", msg.From, msg.KitID, nil, "", time.Minute))
				case msg.Kind == mulebus.KindCancel && msg.KitID != "":
					cancels.Add(1)
				}
			}
		}
	}()
}

func TestAwaitKitHoldGivesUpAWaitInLineAndCancelsTheKit(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	bus := mulebus.NewBus()
	var orders, cancels atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	queueOnlyMule(bus, stop, &orders, &cancels)
	var holds atomic.Int32
	d := &Deps{Session: sim, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	start := time.Now()
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 30 * time.Second,
		Lines: []orderplan.KitLine{{Item: "Chaos rune", Qty: 300}},
		Hold: func() string {
			if holds.Add(1) >= 5 {
				return "the mule has not been heard"
			}
			return ""
		}})
	if !res.Answered || res.Reason != "the mule has not been heard" {
		t.Fatalf("the hold's reason ends the wait: %+v", res)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the hold ended a 30 s wait at once, not after %s", took)
	}
	time.Sleep(50 * time.Millisecond)
	if cancels.Load() != 1 {
		t.Fatalf("a wait given up withdraws the kit: %d cancels", cancels.Load())
	}
}

// CONTROL: with no Hold and no Republish a queued kit is published once and waited out, as before.
func TestAwaitKitWithoutAHoldWaitsOutItsBudgetAndPublishesOnce(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	bus := mulebus.NewBus()
	var orders, cancels atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	queueOnlyMule(bus, stop, &orders, &cancels)
	d := &Deps{Session: sim, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 300 * time.Millisecond,
		Lines: []orderplan.KitLine{{Item: "Chaos rune", Qty: 300}}})
	if !res.Answered || res.Reason != "the mule never handed the kit over" {
		t.Fatalf("result %+v", res)
	}
	time.Sleep(30 * time.Millisecond)
	if n := orders.Load(); n != 1 {
		t.Fatalf("an answered kit is not re-published without Republish: %d orders", n)
	}
}

func TestAwaitKitRepublishesAKitStillInLine(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	bus := mulebus.NewBus()
	var orders, cancels atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	queueOnlyMule(bus, stop, &orders, &cancels)
	w := mulebus.NewWatcher()
	d := &Deps{Session: sim, Chat: chatlog.New(), Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "acct1"}
	AwaitKit(d, KitAsk{Mule: "mule", Me: "acct1", Bus: bus, Timeout: 600 * time.Millisecond, ID: "k1",
		Lines: []orderplan.KitLine{{Item: "Chaos rune", Qty: 300}}, Republish: 100 * time.Millisecond})
	time.Sleep(30 * time.Millisecond)
	if n := orders.Load(); n < 3 {
		t.Fatalf("a kit waiting in line is published again every Republish: %d orders in 600 ms at 100 ms", n)
	}
	last := 1 << 30
	for _, m := range w.Poll(bus) {
		if m.Kind != mulebus.KindKitOrder {
			continue
		}
		if m.KitID != "k1" {
			t.Fatalf("a re-publish keeps the kit's id (the mule keeps its place in line): %q", m.KitID)
		}
		if m.EtaMs > last {
			t.Fatalf("each re-publish carries the wait LEFT, not the whole budget again: %d after %d", m.EtaMs, last)
		}
		last = m.EtaMs
	}
}
