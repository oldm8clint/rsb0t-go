package peerrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/peerplan"
	"rsb0t-go/internal/scripts/tile"
)

func busDeps(me string, bus *mulebus.Bus) *Deps {
	return &Deps{Me: me, Bus: bus, Dir: peerbank.New()}
}

// TestWatcherOnlyTakesOrdersAddressedToMe: an unaddressed order is a MULE's business. A holder
// answering one would have every spare-holder in the fleet stampede at a single asker.
func TestWatcherOnlyTakesOrdersAddressedToMe(t *testing.T) {
	bus := mulebus.NewBus()
	d := busDeps("holder", bus)
	w := NewWatcher()

	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), ""))        // broadcast
	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "someone")) // someone else
	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "HOLDER"))  // me, wrong case
	bus.Publish(mulebus.HereMessage("asker", tile.Tile{}))                                   // not an order

	got := w.Poll(d)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(got), got)
	}
	if got[0].To != "HOLDER" {
		t.Fatalf("took the wrong message: %+v", got[0])
	}
}

// TestWatcherCollapsesRepeatsWithinOneBatch: donorrunner.Await republishes while it waits, and
// those are one request being refreshed, not a queue of them.
func TestWatcherCollapsesRepeatsWithinOneBatch(t *testing.T) {
	bus := mulebus.NewBus()
	d := busDeps("holder", bus)
	w := NewWatcher()

	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "holder"))
	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 2), "holder"))
	bus.Publish(mulebus.OrderMessage("other", orderplan.Format("Bronze axe", 1), "holder"))

	got := w.Poll(d)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (one per asker): %+v", len(got), got)
	}
	// Newest wins for the repeated asker.
	for _, m := range got {
		if m.From == "asker" && m.Text != orderplan.Format("Bronze axe", 2) {
			t.Fatalf("kept the stale order for asker: %q", m.Text)
		}
	}
}

// TestWatcherSuppressesRepeatsAfterADelivery is the double-hand-out guard, and the reason
// RepeatSuppress exists: a peer builds a one-shot policy per request and so has no ledger to
// catch an asker's in-flight republish the way a mule does.
func TestWatcherSuppressesRepeatsAfterADelivery(t *testing.T) {
	bus := mulebus.NewBus()
	d := busDeps("holder", bus)
	w := NewWatcher()
	now := time.Now()

	w.Delivered("asker", now)
	bus.Publish(mulebus.OrderMessage("ASKER", orderplan.Format("Bronze axe", 1), "holder"))
	if got := w.pollAt(d, now.Add(time.Second)); len(got) != 0 {
		t.Fatalf("served a republish %v after delivering: %+v", time.Second, got)
	}

	// A genuinely new order once the window has passed is taken.
	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "holder"))
	if got := w.pollAt(d, now.Add(RepeatSuppress+time.Second)); len(got) != 1 {
		t.Fatalf("got %d, want 1 — the suppression window must not be permanent", len(got))
	}
}

func TestRepeatSuppressOutlastsAnAskersRepublishInterval(t *testing.T) {
	// The whole point is that at least one republish is covered.
	if RepeatSuppress <= 10*time.Second {
		t.Fatalf("RepeatSuppress %v must exceed donorrunner.AskRepeatEvery (10s), or a republish slips through", RepeatSuppress)
	}
}

// TestWatcherSuppressionIsPerAsker: serving one account must not silence another.
func TestWatcherSuppressionIsPerAsker(t *testing.T) {
	bus := mulebus.NewBus()
	d := busDeps("holder", bus)
	w := NewWatcher()
	now := time.Now()

	w.Delivered("asker", now)
	bus.Publish(mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "holder"))
	bus.Publish(mulebus.OrderMessage("other", orderplan.Format("Bronze axe", 1), "holder"))
	got := w.pollAt(d, now.Add(time.Second))
	if len(got) != 1 || got[0].From != "other" {
		t.Fatalf("got %+v, want only the unserved asker", got)
	}
}

// TestOnePolicyAuthorisesExactlyOneOrder pins the line between a peer and a mule.
func TestOnePolicyAuthorisesExactlyOneOrder(t *testing.T) {
	pol := onePolicy("asker", "Bronze axe", 1)

	// Somebody else's order is refused: the bus carries no authentication at all.
	d := orderplan.Prescreen(time.Now(), "stranger", orderplan.Format("Bronze axe", 1), pol, orderplan.NewLedger(orderplan.DefaultWindow))
	if d.Verdict == orderplan.Fulfil {
		t.Fatalf("a one-shot policy must not serve a different donor: %+v", d)
	}

	// A different item is refused: the catalogue is this one order and nothing else.
	d = orderplan.Prescreen(time.Now(), "asker", orderplan.Format("Rune axe", 1), pol, orderplan.NewLedger(orderplan.DefaultWindow))
	if d.Verdict == orderplan.Fulfil {
		t.Fatalf("a one-shot policy must not serve a different item: %+v", d)
	}

	// The order it was built for is allowed.
	d = orderplan.Prescreen(time.Now(), "asker", orderplan.Format("Bronze axe", 1), pol, orderplan.NewLedger(orderplan.DefaultWindow))
	if d.Verdict != orderplan.Fulfil {
		t.Fatalf("the order the policy was built for should pass: %+v", d)
	}
}

// TestOnePolicyCapsAtTheSpareNotAtQtyMax: orderplan.Prescreen CLAMPS an over-large order
// rather than declining it, and orderplan.QtyMax is a PARSER bound (1,000,000) separate from
// the catalogue caps. A peer's cap must be the live spare, so an over-large ask clamps DOWN to
// what the holder actually decided to give.
func TestOnePolicyCapsAtTheSpareNotAtQtyMax(t *testing.T) {
	pol := onePolicy("asker", "Coins", 2)
	d := orderplan.Prescreen(time.Now(), "asker", orderplan.Format("Coins", 999_999), pol, orderplan.NewLedger(orderplan.DefaultWindow))
	if d.Verdict != orderplan.Fulfil {
		t.Fatalf("expected a clamp, not a refusal: %+v", d)
	}
	if d.Qty != 2 {
		t.Fatalf("Qty = %d, want 2 — the cap must be the holder's spare, never QtyMax (%d)", d.Qty, orderplan.QtyMax)
	}
}

// TestHoldingsMergesAndDropsUnnamed.
func TestHoldingsMergesAndDropsUnnamed(t *testing.T) {
	// A nil config names nothing, so everything is dropped rather than published as "".
	if got := Holdings(nil, nil); len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

// TestPeersFromMarksAReservedHolderBusy: a holder whose turn is taken must be EXCLUDED by
// Choose, not merely ranked down.
func TestPeersFromMarksAReservedHolderBusy(t *testing.T) {
	dir := peerbank.New()
	now := time.Now()
	dir.Publish("holder", tile.Tile{X: 1, Z: 1}, true,
		[]peerbank.Holding{{Item: "Bronze axe", Count: 2}}, nil, false)

	if p := PeersFrom(dir, now); len(p) != 1 || p[0].Busy {
		t.Fatalf("an unreserved holder should not be busy: %+v", p)
	}
	dir.Reserve("holder", "someone", now, peerplan.TurnTTL)
	p := PeersFrom(dir, now)
	if len(p) != 1 || !p[0].Busy {
		t.Fatalf("a reserved holder must read as busy: %+v", p)
	}

	// And Choose must then offer nothing.
	c := peerplan.Choose(
		peerplan.Request{Asker: "me", At: tile.Tile{}, Located: true, Item: "Bronze axe", Qty: 1},
		p, peerplan.DefaultReserve(),
		func(a, b tile.Tile) (int, bool) { return 1, true },
		peerplan.Options{MuleCost: peerplan.NoMuleCost})
	if len(c.Candidates) != 0 {
		t.Fatalf("a busy holder must be excluded, got %+v", c.Candidates)
	}
}

// TestAskWithoutConfigurationFallsBackRatherThanPanicking.
func TestAskWithoutConfigurationFallsBackRatherThanPanicking(t *testing.T) {
	res := Ask(&Deps{Me: "me"}, AskOptions{Item: "Bronze axe", Qty: 1})
	if !res.UseMule {
		t.Fatalf("an unconfigured fleet should fall back to the mule, got %+v", res)
	}
	res = Ask(&Deps{Me: "me", Bus: mulebus.NewBus(), Dir: peerbank.New()}, AskOptions{Item: "Bronze axe", Qty: 1})
	if !res.UseMule {
		t.Fatalf("no Travel func should fall back to the mule, got %+v", res)
	}
}

// TestServeIgnoresWhatIsNotItsBusiness.
func TestServeIgnoresWhatIsNotItsBusiness(t *testing.T) {
	bus := mulebus.NewBus()
	d := busDeps("holder", bus)

	if r := Serve(d, ServeOptions{Allowed: []string{"asker"}}, mulebus.HereMessage("asker", tile.Tile{})); r.Served {
		t.Fatal("served a non-order message")
	}
	if r := Serve(d, ServeOptions{Allowed: []string{"asker"}},
		mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "")); r.Served {
		t.Fatal("served a BROADCAST order — that belongs to a mule")
	}
	// Not on the allow list: the bus carries no authentication.
	if r := Serve(d, ServeOptions{Allowed: []string{"someone"}},
		mulebus.OrderMessage("stranger", orderplan.Format("Bronze axe", 1), "holder")); r.Served {
		t.Fatal("served an account that is not on the allow list")
	}
	if r := Serve(d, ServeOptions{}, mulebus.OrderMessage("asker", orderplan.Format("Bronze axe", 1), "holder")); r.Served {
		t.Fatal("an empty allow list must serve nobody")
	}
}
