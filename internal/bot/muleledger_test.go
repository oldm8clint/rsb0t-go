package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/scripts/mulerunner"
)

// TestRecordTradeWritesVerifiedReceivesAndFailures proves the mule's ledger writes follow the
// trade code's own verdict: an accepted receive carries exactly the verified stacks, a failure
// moves nothing, an unhandled cycle and a stopped hand-out write nothing.
func TestRecordTradeWritesVerifiedReceivesAndFailures(t *testing.T) {
	var got []muleledger.Entry
	ctx := &Context{Username: "mule", TradeLog: func(e muleledger.Entry) { got = append(got, e) }}

	recordTrade(ctx, mulerunner.CycleResult{}) // nothing seen this cycle
	recordTrade(ctx, mulerunner.CycleResult{Handled: true, Accepted: true, Partner: "Fisher1",
		Received: []gamestate.SlotItem{{Item: gamestate.Item{ID: 995, Count: 300}}, {Item: gamestate.Item{ID: 1624, Count: 2}}}})
	recordTrade(ctx, mulerunner.CycleResult{Handled: true, Partner: "Fisher2"})
	recordHandout(ctx, "acct1", mulerunner.HandoutResult{Delivered: true, Item: "Lobster pot", Crossed: 1})
	recordHandout(ctx, "acct2", mulerunner.HandoutResult{Reason: "not banked"})
	recordHandout(ctx, "acct3", mulerunner.HandoutResult{Stopped: true})

	if len(got) != 4 {
		t.Fatalf("wrote %d entries, want 4: %+v", len(got), got)
	}
	if e := got[0]; e.Outcome != muleledger.OutcomeAccepted || e.Kind != muleledger.KindReceive || e.Mule != "mule" ||
		len(e.In) != 2 || e.In[0].ID != 995 || e.In[0].Count != 300 || e.In[1].Count != 2 {
		t.Fatalf("accepted receive = %+v", e)
	}
	if e := got[1]; e.Outcome != muleledger.OutcomeFailed || e.Partner != "Fisher2" || len(e.In) != 0 {
		t.Fatalf("failed receive = %+v", e)
	}
	if e := got[2]; e.Kind != muleledger.KindHandout || e.Outcome != muleledger.OutcomeAccepted || len(e.Out) != 1 ||
		e.Out[0].Name != "Lobster pot" || e.Out[0].Count != 1 || e.Out[0].ID != -1 {
		t.Fatalf("delivered hand-out = %+v", e)
	}
	if e := got[3]; e.Outcome != muleledger.OutcomeFailed || e.Reason != "not banked" || len(e.Out) != 0 {
		t.Fatalf("failed hand-out = %+v", e)
	}

	// No hook: nothing happens and nothing panics.
	recordTrade(&Context{}, mulerunner.CycleResult{Handled: true, Accepted: true})
}
