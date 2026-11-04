package actionsim_test

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
)

// THE CONFIRM SCREEN'S ORDER OF ACCEPTS (trade.rs2 [if_button,tradeconfirm:accept]). Whoever accepts SECOND completes the trade, inside the
// packet that accepts, and the one who accepted first sees "Waiting for other player." while the other's screen says "Other player has
// accepted.". A packet that clears the pending action (an op on a loc, a walk, a request) read after a completing accept finds no trade
// to close; read before it, it closes the trade for both.

const (
	cfTalisman = int32(1462)
	cfEssence  = int32(1436)
)

// confirmSim is a crafter with a talisman holding an open trade in which the partner offers 25 essence, on the confirm screen. The
// partner holds its confirm accept for the test to give.
func confirmSim(t *testing.T) (*actionsim.Session, *actions.Trade, *actionsim.Trade) {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: cfTalisman, Count: 1}})
	sim.SetStrictTrade(true)
	sim.SetSplitMessages(true)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: cfEssence, Count: 25}}, AcceptOffer: true, PartnerHoldsConfirm: true}
	sim.OpenTrade(partner)
	tr := actions.NewTrade(sim)
	if err := tr.Accept(); err != nil {
		t.Fatal(err)
	}
	if !tr.OnConfirmScreen() {
		t.Fatal("the confirm screen is not open")
	}
	return sim, tr, partner
}

// acceptAndUse is the one-write message: the confirm accept, then the talisman on the ruins.
func acceptAndUse(t *testing.T, sim *actionsim.Session) {
	t.Helper()
	b := actions.NewCoalescer(sim)
	if err := actions.NewTrade(b.Session()).Accept(); err != nil {
		t.Fatal(err)
	}
	if err := actions.OpLocU(b.Session(), 2868, 3018, 2460, cfTalisman, 0, actions.PlayerPack); err != nil {
		t.Fatal(err)
	}
	if b.Pending() != 2 {
		t.Fatalf("%d packets queued, want the accept and the use", b.Pending())
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestTheConfirmScreenSaysWhoHasAccepted(t *testing.T) {
	sim, tr, partner := confirmSim(t)
	if got := sim.State().Text(actions.TradeConfirmStatus); !strings.Contains(got, "Are you sure") || tr.OtherAccepted() {
		t.Fatalf("a fresh confirm screen says %q (other accepted %v)", got, tr.OtherAccepted())
	}
	sim.PartnerAcceptsConfirm(partner)
	if !tr.OtherAccepted() || sim.TradeDone(partner) {
		t.Fatalf("the partner accepted first: other accepted %v, done %v, says %q", tr.OtherAccepted(), sim.TradeDone(partner), sim.State().Text(actions.TradeConfirmStatus))
	}
	if err := tr.Accept(); err != nil {
		t.Fatal(err)
	}
	if !sim.TradeDone(partner) || tr.Active() || packCount(sim, cfEssence) != 25 {
		t.Fatalf("the second accept did not complete the trade: done %v active %v, %d essence", sim.TradeDone(partner), tr.Active(), packCount(sim, cfEssence))
	}
}

func TestTheSideThatAcceptsFirstWaits(t *testing.T) {
	sim, tr, partner := confirmSim(t)
	if err := tr.Accept(); err != nil {
		t.Fatal(err)
	}
	if got := sim.State().Text(actions.TradeConfirmStatus); !strings.Contains(got, "Waiting for other player") || tr.OtherAccepted() || sim.TradeDone(partner) || !tr.Active() {
		t.Fatalf("after our accept: says %q, other accepted %v, done %v", got, tr.OtherAccepted(), sim.TradeDone(partner))
	}
	sim.PartnerAcceptsConfirm(partner)
	if !sim.TradeDone(partner) || tr.Active() || packCount(sim, cfEssence) != 25 {
		t.Fatalf("the partner's accept did not complete the trade: done %v active %v", sim.TradeDone(partner), tr.Active())
	}
}

// Accept and the talisman in one message, the partner having accepted: the trade is over before the use is read, so the use closes
// nothing and the essence is in the pack.
func TestAnAcceptThatCompletesLetsTheUseThatFollowsIt(t *testing.T) {
	sim, _, partner := confirmSim(t)
	sim.PartnerAcceptsConfirm(partner)
	acceptAndUse(t, sim)
	if !sim.TradeDone(partner) || sim.StrayCloses() != 0 || packCount(sim, cfEssence) != 25 {
		t.Fatalf("done %v, stray closes %d, essence %d", sim.TradeDone(partner), sim.StrayCloses(), packCount(sim, cfEssence))
	}
}

// The same message with the partner not yet accepted: the accept only waits and the use, read after it, closes the trade for both: no
// essence, the use goes on to run.
func TestAnAcceptThatDoesNotCompleteIsClosedByTheUseThatFollowsIt(t *testing.T) {
	sim, tr, partner := confirmSim(t)
	acceptAndUse(t, sim)
	if sim.TradeDone(partner) || sim.StrayCloses() != 1 || tr.Active() || packCount(sim, cfEssence) != 0 {
		t.Fatalf("done %v, stray closes %d, active %v, essence %d", sim.TradeDone(partner), sim.StrayCloses(), tr.Active(), packCount(sim, cfEssence))
	}
}

// The partner closing the screen just ahead of the message (the same tick, read first): the accept finds no screen and the use runs.
func TestAPartnerThatClosesJustAheadOfTheMessage(t *testing.T) {
	sim, tr, partner := confirmSim(t)
	sim.PartnerAcceptsConfirm(partner)
	sim.BeforeSend = func([]byte) {
		sim.BeforeSend = nil
		sim.PartnerClosesTrade()
	}
	b := actions.NewCoalescer(sim)
	if err := actions.NewTrade(b.Session()).Accept(); err != nil {
		t.Fatal(err)
	}
	if err := actions.OpLocU(b.Session(), 2868, 3018, 2460, cfTalisman, 0, actions.PlayerPack); err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	if sim.TradeDone(partner) || tr.Active() || packCount(sim, cfEssence) != 0 {
		t.Fatalf("done %v, active %v, essence %d", sim.TradeDone(partner), tr.Active(), packCount(sim, cfEssence))
	}
}
