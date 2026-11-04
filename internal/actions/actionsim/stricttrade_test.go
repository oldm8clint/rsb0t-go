package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/pathfind"
)

func packCount(sim *actionsim.Session, id int32) int {
	n := 0
	for _, r := range sim.Pack() {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// The engine's rule for a trade screen (OpPlayerHandler and MoveClickHandler call clearPendingAction, which is clearInteraction and
// closeModal, when a packet is read; closeModal on the screen runs [if_close,trademain]): an op on a player or a walk sent while the
// screen is open closes it and gives the offer back. The older tests do not model it; a test that wants it asks.
func TestStrictTradeClosesAnOpenScreenForARequestOrAWalk(t *testing.T) {
	for name, send := range map[string]func(sim *actionsim.Session) error{
		"a trade request": func(sim *actionsim.Session) error { return actions.NewTrade(sim).Request(1) },
		"a walk": func(sim *actionsim.Session) error {
			return actions.Walk(sim, []pathfind.NavPoint{{X: 3200, Z: 3200}}, false)
		},
	} {
		sim, trade := tradeSim(t)
		sim.SetStrictTrade(true)
		runTicks(t, sim, 30*time.Millisecond)
		if err := trade.Offer(fbRune, 100, time.Second); err != nil {
			t.Fatalf("%s: offering: %v", name, err)
		}
		time.Sleep(100 * time.Millisecond)
		if !trade.Active() {
			t.Fatalf("%s: the screen is not open", name)
		}
		if err := send(sim); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		time.Sleep(100 * time.Millisecond)
		if trade.Active() || sim.TradeOpen() || sim.StrayCloses() != 1 {
			t.Fatalf("%s: the screen is still open (active %v, open %v, %d stray closes)", name, trade.Active(), sim.TradeOpen(), sim.StrayCloses())
		}
		if got := packCount(sim, fbRune); got != 500 {
			t.Fatalf("%s: the offer went back: %d runes in the pack, want 500", name, got)
		}
	}
	// Off (the default): the screen stays, as the older tests expect.
	sim, trade := tradeSim(t)
	runTicks(t, sim, 30*time.Millisecond)
	if err := actions.NewTrade(sim).Request(1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if !trade.Active() || sim.StrayCloses() != 0 {
		t.Fatal("a request closed the screen without the strict model")
	}
}

// The partner's side closing it: the same effect.
func TestPartnerClosesTrade(t *testing.T) {
	sim, trade := tradeSim(t)
	sim.PartnerClosesTrade()
	if trade.Active() || sim.TradeOpen() {
		t.Fatal("still open")
	}
}
