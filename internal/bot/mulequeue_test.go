package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/tile"
)

func queueMule(allowed ...string) (*muleBot, *Context, *mulebus.Bus) {
	bus := mulebus.NewBus()
	b := &muleBot{
		wallBus:    true,
		busWatcher: mulebus.NewWatcher(),
		pickups:    mulereceiverplan.NewPickupQueue(mulebus.CollectTTL),
		watcher:    mulerunner.NewRequestWatcher(),
		allowed:    mulerunner.AllowList(allowed),
	}
	return b, &Context{Bus: bus, Username: "mule"}, bus
}

// comings returns every KindComing on the bus by recipient, the last one each got.
func comings(bus *mulebus.Bus, since time.Time) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, m := range bus.Since(since) {
		if m.Kind == mulebus.KindComing {
			out[m.To] = time.Duration(m.EtaMs) * time.Millisecond
		}
	}
	return out
}

// TestAQuoteCountsOnlyTheDonorsStillWaitingAfterTheWholeBatch. Live 2026-09-23 00:45:01 a quote
// counted 36 donors against 30 still waiting, because each announcement was quoted where it sat
// in the batch, before cancels later in the same batch were applied. It is now the donor's own
// place in the queue, after the whole batch.
func TestAQuoteCountsOnlyTheDonorsStillWaitingAfterTheWholeBatch(t *testing.T) {
	b, ctx, bus := queueMule("acct1", "acct2", "acct3", "acct4")
	start := time.Now()
	for _, n := range []string{"acct1", "acct2", "acct3"} {
		r := mulebus.ReadyMessage(n, 20)
		r.To = "mule"
		bus.Publish(r)
		time.Sleep(2 * time.Millisecond) // three distinct announcement times
	}
	r := mulebus.ReadyMessage("acct4", 20)
	r.To = "mule"
	bus.Publish(r)
	for _, n := range []string{"acct1", "acct2"} {
		c := mulebus.CancelMessage(n)
		c.To = "mule"
		bus.Publish(c)
	}
	b.pollBus(ctx)
	got := comings(bus, start)
	cycle := b.cycle()
	if got["acct3"] != cycle || got["acct4"] != 2*cycle {
		t.Errorf("quotes = %v, want acct3 first (one cycle) and acct4 second (two): the two cancels come off first", got)
	}
	if _, quoted := got["acct1"]; quoted {
		t.Error("a donor that cancelled in the same batch was quoted")
	}
}

// TestTheQueueKeysOneDonorOneWayWhateverItIsCalled: the trade request and the completion carry
// the DISPLAY name ("Big net") where the announcement carried the LOGIN name ("big_net"); the old
// strings.ToLower keys never matched, so a completion could not remove its own announcement.
func TestTheQueueKeysOneDonorOneWayWhateverItIsCalled(t *testing.T) {
	b, ctx, bus := queueMule("big_net", "user7")
	for _, n := range []string{"big_net", "user7"} {
		r := mulebus.ReadyMessage(n, 5)
		r.To = "mule"
		bus.Publish(r)
	}
	b.pollBus(ctx)
	if _, ok := b.incoming["big net"]; !ok {
		t.Fatalf("incoming = %v, want big_net keyed as \"big net\"", b.incoming)
	}
	if q := b.deps(ctx).Queue(); len(q) != 2 || q[0] != "big net" {
		t.Errorf("queue = %v, want big_net first under the one name rule", q)
	}
	c := mulebus.CancelMessage("user7")
	c.To = "mule"
	bus.Publish(c)
	b.pollBus(ctx)
	if _, ok := b.incoming["user7"]; ok {
		t.Error("a cancel did not take the donor out of the queue")
	}
}

// TestAReservationIsQueueDepthAndNothingElse: a donor that has drawn and is walking counts in the
// depth the heartbeat publishes, but never holds the mule off cooking (owner 2026-09-19).
func TestAReservationIsQueueDepthAndNothingElse(t *testing.T) {
	b, ctx, bus := queueMule("acct1", "acct2")
	bus.Publish(mulebus.ReserveMessage("acct1", "mule"))
	bus.Publish(mulebus.ReserveMessage("acct2", "mule"))
	bus.Publish(mulebus.ReserveMessage("stranger", "mule")) // not allowed: not counted
	b.pollBus(ctx)
	if n := b.incomingPending(); n != 0 {
		t.Errorf("incomingPending = %d: a reservation must not stop a cook round", n)
	}
	if n := b.queueDepth(); n != 2 {
		t.Errorf("queueDepth = %d, want the two walking donors", n)
	}
	r := mulebus.ReadyMessage("acct1", 20)
	r.To = "mule"
	bus.Publish(r)
	b.pollBus(ctx)
	if n := b.queueDepth(); n != 2 {
		t.Errorf("queueDepth = %d after acct1 arrived, want 2 still (counted once, not twice)", n)
	}
	b.enroute["acct2"] = time.Now().Add(-enrouteTTL - time.Second)
	if n := b.queueDepth(); n != 1 {
		t.Errorf("queueDepth = %d, want an expired reservation dropped", n)
	}
}

// TestAWaitingDonorKeepsItsPlaceWhileItHeartbeats: incomingTTL runs from when the donor was last
// heard, so a long, orderly queue does not expire the donors standing in it.
func TestAWaitingDonorKeepsItsPlaceWhileItHeartbeats(t *testing.T) {
	b, ctx, bus := queueMule("acct1")
	b.incoming = map[string]time.Time{"acct1": time.Now().Add(-incomingTTL - time.Minute)}
	bus.Publish(mulebus.CollectMessage("acct1", tile.Tile{X: 2809, Z: 3441}, 20, "mule"))
	b.pollBus(ctx)
	if n := b.incomingPending(); n != 1 {
		t.Fatalf("a donor that just repeated its collect was expired out of the queue")
	}
	if at := b.incoming["acct1"]; time.Since(at) < incomingTTL {
		t.Error("the heartbeat moved the donor's place in the queue; it must only keep it alive")
	}
}

// TestEveryWaitingDonorHearsItsNewTurnAfterATrade: the quote is refreshed as the queue moves.
func TestEveryWaitingDonorHearsItsNewTurnAfterATrade(t *testing.T) {
	b, ctx, bus := queueMule()
	t0 := time.Now().Add(-time.Minute)
	b.incoming = map[string]time.Time{"acct1": t0, "acct2": t0.Add(time.Second), "acct3": t0.Add(2 * time.Second)}
	start := time.Now()
	b.refreshQuotes(ctx)
	got, c := comings(bus, start), b.cycle()
	if got["acct1"] != c || got["acct2"] != 2*c || got["acct3"] != 3*c {
		t.Errorf("refreshed quotes = %v, want 1, 2 and 3 cycles of %s", got, c)
	}
}

func TestTheMeasuredCycleFloorIsTenSeconds(t *testing.T) {
	b := &muleBot{lastTradeAt: time.Now().Add(-15 * time.Second)}
	b.noteCycle(1)
	if got := b.cycle(); got < 14*time.Second || got > 16*time.Second {
		t.Errorf("a measured 15s cycle quotes %s; the old 30s floor doubled it", got)
	}
}

// TestAPartnerKeepsItsPlaceForAFollowUpRound: a donor whose load needs a second trade screen is
// still at the head of the queue after the first, and leaves it only when it cancels.
func TestAPartnerKeepsItsPlaceUntilItCancels(t *testing.T) {
	b, ctx, bus := queueMule("acct1", "acct2")
	for _, n := range []string{"acct1", "acct2"} {
		r := mulebus.ReadyMessage(n, 26)
		r.To = "mule"
		bus.Publish(r)
		time.Sleep(2 * time.Millisecond)
	}
	b.pollBus(ctx)
	if q := b.deps(ctx).Queue(); len(q) != 2 || q[0] != "acct1" {
		t.Fatalf("queue = %v", q)
	}
	c := mulebus.CancelMessage("acct1")
	c.To = "mule"
	bus.Publish(c)
	b.pollBus(ctx)
	if q := b.deps(ctx).Queue(); len(q) != 1 || q[0] != "acct2" {
		t.Errorf("after acct1 cancelled the queue is %v, want only acct2", q)
	}
}
