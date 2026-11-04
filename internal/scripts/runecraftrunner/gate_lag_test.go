package runecraftrunner

import (
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
)

// failedTrade runs one handshake whose confirm screen the carrier never accepts, so the crafter's trade fails ("the trade never closed
// after the confirm accept"), in a view that lags or does not.
func failedTrade(t *testing.T, lagging bool) GateResult {
	t.Helper()
	sim, d := crafterSession(t, 4000, "Acct 1")
	if lagging {
		sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
	}
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 25}}, AcceptOffer: true, AcceptConfirm: false}
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && requests.Add(1) == 1 {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	return GateStep(d, w, GateOptions{Allowed: allow("acct_1"), Wait: 3 * time.Second})
}

// 18 Oct 2026 19:46-19:52: ten trades failed in a view that went silent every few seconds, and each said nothing of the gate. A trade that
// failed in a lagging view says so (CR-6); the same failure in a current view does not.
func TestAFailedTradeInALaggingViewSaysSo(t *testing.T) {
	res := failedTrade(t, true)
	if res.Kind != GateFailed || !res.Lag {
		t.Fatalf("a failed trade in a lagging view: %+v", res)
	}
	res = failedTrade(t, false)
	if res.Kind != GateFailed || res.Lag {
		t.Fatalf("a failed trade in a current view: %+v", res)
	}
}

// A trade that worked in a lagging view is a trade: Lag is only read on a failure.
func TestATradeThatWorkedInALaggingViewStillWorked(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 25}}, AcceptOffer: true, AcceptConfirm: true}
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && requests.Add(1) == 1 {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("acct_1"), Wait: 3 * time.Second})
	if res.Kind != GateReceived || res.Essence != 25 {
		t.Fatalf("%+v", res)
	}
}
