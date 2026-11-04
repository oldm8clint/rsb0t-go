package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/clientprot"
)

// Two actions a tick (twopertick-1012): several offer lines on the trade screen in one burst.

// tradeSim is a pack of a rune stack, five sharks, some bones, a few logs and a bucket, on an open offer screen.
func tradeSim(t *testing.T) (*actionsim.Session, *actions.Trade) {
	t.Helper()
	pack := []actionsim.Stack{
		{ID: fbRune, Count: 500},
		{ID: fbShark, Count: 1}, {ID: fbShark, Count: 1}, {ID: fbShark, Count: 1}, {ID: fbShark, Count: 1}, {ID: fbShark, Count: 1},
		{ID: fbBones, Count: 1}, {ID: fbBones, Count: 1},
		{ID: fbLog, Count: 1}, {ID: fbLog, Count: 1}, {ID: fbLog, Count: 1},
		{ID: eqJunk, Count: 1},
	}
	sim := actionsim.New(fbObjs(), pack)
	sim.OpenTrade(&actionsim.Trade{Partner: "Receiver"})
	return sim, actions.NewTrade(sim)
}

// A whole stack, an exact count and another whole stack are four user events (an Offer-X is two), all in the
// tick the first goes out in, and the offer screen shows the three lines.
func TestOfferManyPlacesAKitInOneTick(t *testing.T) {
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)

	r := trade.OfferMany([]actions.TradeLine{
		{ID: fbRune, Qty: 500, All: true},
		{ID: fbShark, Qty: 3},
		{ID: fbBones, Qty: 2, All: true},
	}, realWaiter{})
	if !r.OK() || len(r.Registered) != 3 || len(r.Retried) != 0 {
		t.Fatalf("batch = %+v", r)
	}
	if r.Bursts != 1 || r.Events != 4 {
		t.Fatalf("%d event(s) in %d burst(s), want 4 in 1", r.Events, r.Bursts)
	}
	if most, ticks := maxPerTick(sim); most != 4 || ticks != 1 {
		t.Fatalf("the engine read %v, want 4 events in 1 tick", sim.PerTick())
	}
	mine := trade.MyOffer()
	if actions.CountByID(mine, fbRune) != 500 || actions.CountByID(mine, fbShark) != 3 || actions.CountByID(mine, fbBones) != 2 {
		t.Fatalf("offer screen = %v, want 500 runes, 3 sharks, 2 bones", mine)
	}
	if actions.CountByID(trade.SidePack(), fbShark) != 2 {
		t.Fatalf("two sharks should be left in the pack: %v", trade.SidePack())
	}
	if sim.State().CountDialogOpen() {
		t.Fatal("the blind-answered count dialog left the client's dialog flag set")
	}
	// An Offer-X's count is never split from its button.
	ops := opcodes(sim.Sent())
	for i, op := range ops {
		if op == clientprot.INV_BUTTON5 && (i+1 >= len(ops) || ops[i+1] != clientprot.RESUME_P_COUNTDIALOG) {
			t.Fatalf("Offer-X not followed by its count: %v", ops)
		}
	}
	t.Log(r.Log())
}

// Seven events are 5 + 2: the Offer-X pair that would be the sixth and seventh is not split, and nothing is
// queued ahead of the engine.
func TestOfferManyNeverSplitsACountFromItsButton(t *testing.T) {
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)

	r := trade.OfferMany([]actions.TradeLine{
		{ID: fbRune, Qty: 100},         // 2
		{ID: fbShark, Qty: 2},          // 2
		{ID: fbBones, Qty: 1},          // 2 (the sixth and seventh event: the pair moves to the next tick)
		{ID: fbLog, Qty: 3, All: true}, // 1
	}, realWaiter{})
	if !r.OK() {
		t.Fatalf("batch = %+v", r)
	}
	if r.Events != 7 || r.Bursts != 2 {
		t.Fatalf("%d events in %d bursts, want 7 in 2", r.Events, r.Bursts)
	}
	if got := sim.MaxBacklog(); got > actionsim.UserEventsPerTick {
		t.Fatalf("%d packets waiting when a tick began", got)
	}
	for i, n := range sim.PerTick() {
		if n > actionsim.UserEventsPerTick {
			t.Fatalf("tick %d read %d", i, n)
		}
	}
	mine := trade.MyOffer()
	if actions.CountByID(mine, fbRune) != 100 || actions.CountByID(mine, fbShark) != 2 || actions.CountByID(mine, fbBones) != 1 || actions.CountByID(mine, fbLog) != 3 {
		t.Fatalf("offer screen = %v", mine)
	}
}

// The player is delayed when the burst lands (a spell just cast, a random event): the lines are refused and go
// on the screen one at a time afterwards.
func TestOfferManyRetriesLinesTheServerRefused(t *testing.T) {
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)
	sim.Delay(2)

	r := trade.OfferMany([]actions.TradeLine{{ID: fbRune, Qty: 500, All: true}, {ID: fbBones, Qty: 2, All: true}}, clampWaiter{})
	// The inventory buttons of the trade side panel are refused while the player is delayed
	// (InvButtonHandler), so the burst is dropped and the retry lands once the delay is over.
	if !r.OK() {
		t.Fatalf("batch = %+v", r)
	}
	if len(r.Retried) != 2 {
		t.Fatalf("registered %v retried %v, want both on the retry", r.Registered, r.Retried)
	}
	if actions.CountByID(trade.MyOffer(), fbRune) != 500 {
		t.Fatalf("offer screen = %v", trade.MyOffer())
	}
}

// A line the pack does not hold sends nothing and is reported; a duplicate object is merged; the rest goes.
func TestOfferManyReportsMissingAndMergesDuplicates(t *testing.T) {
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)

	r := trade.OfferMany([]actions.TradeLine{
		{ID: fbShark, Qty: 2},
		{ID: fbOre, Qty: 4, All: true}, // not held
		{ID: fbShark, Qty: 1},          // merged into the first: 3 in all
	}, realWaiter{})
	if len(r.Missing) != 1 || r.Missing[0] != fbOre || r.Wanted != 2 {
		t.Fatalf("batch = %+v", r)
	}
	if len(r.Registered) != 1 || actions.CountByID(trade.MyOffer(), fbShark) != 3 {
		t.Fatalf("batch = %+v offer = %v, want 3 sharks", r, trade.MyOffer())
	}
	if n := len(sim.Sent()); n != 2 {
		t.Fatalf("%d packets sent, want the one Offer-X pair", n)
	}
	// Off the offer screen it refuses and sends nothing.
	sim2 := actionsim.New(fbObjs(), []actionsim.Stack{{ID: fbShark, Count: 1}})
	if got := actions.NewTrade(sim2).OfferMany([]actions.TradeLine{{ID: fbShark, Qty: 1, All: true}}, realWaiter{}); got.Err == nil || len(sim2.Sent()) != 0 {
		t.Fatalf("no trade screen: %+v, %d sent", got, len(sim2.Sent()))
	}
}

// Two lines of one object merge: two whole-stack lines are the stack once, not twice.
func TestOfferManyMergesDuplicateWholeStackLines(t *testing.T) {
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)
	r := trade.OfferMany([]actions.TradeLine{{ID: fbRune, Qty: 500, All: true}, {ID: fbRune, Qty: 500, All: true}}, realWaiter{})
	if !r.OK() || r.Wanted != 1 || len(r.Registered) != 1 || len(r.Retried) != 0 {
		t.Fatalf("batch = %+v", r)
	}
	if actions.CountByID(trade.MyOffer(), fbRune) != 500 {
		t.Fatalf("offer screen = %v", trade.MyOffer())
	}
}
