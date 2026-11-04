package runecraftrunner

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

func init() {
	// The settle of 1.2 seconds, the short-offer wait of 4 and the like are measured against a clock 40 times faster, and the
	// trade code's own bounds are tens of milliseconds, so a trade takes a fraction of a second here.
	gateClockScale = 40
	gatePoll = 4 * time.Millisecond
	gateHeaderWait, gateConfirmWait, gateCloseWait, gateArriveWait = 300*time.Millisecond, 400*time.Millisecond, 300*time.Millisecond, 200*time.Millisecond
	gateAskWindow, gateRequestLife = 60*time.Millisecond, 2*time.Second
	gateOfferWait, gateOfferRetry, gateFlicker = 300*time.Millisecond, 40*time.Millisecond, 60*time.Millisecond
}

const (
	talismanID = int32(1462)
	coinsID    = int32(995)
	// noteBowID is a noted bow (the Magic shortbow's note), the stack the gate's alchemy keeps in the pack.
	noteBowID = int32(862)
)

func gateObjs() actionsim.Objs {
	o := objs()
	stack := func(id int32) bool {
		return id == int32(runecraftplan.Nature.RuneID) || id == coinsID || id == noteBowID
	}
	o.Stackable = stack
	return o
}

// crafterSession is the crafter at the gate: the talisman, a stack of Natures and the fare in the pack, a carrier or two in view.
func crafterSession(t *testing.T, natures int, players ...string) (*actionsim.Session, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(gateObjs(), []actionsim.Stack{{ID: talismanID, Count: 1}, {ID: NatureID, Count: natures}, {ID: coinsID, Count: 100}})
	if len(players) > 0 {
		sim.ShowPlayers(players...)
	}
	d := &fleetrunner.Deps{Session: sim, Chat: chatlog.New(), Components: actionsim.Components(),
		Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	return sim, d
}

// carrierSession is a carrier with n unnoted essence, the notes, coins and a ring: the crafter in view.
func carrierSession(t *testing.T, essence int, players ...string) (*actionsim.Session, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(gateObjs(), []actionsim.Stack{{ID: runecraftplan.NotedEssenceID, Count: 2900}, {ID: coinsID, Count: 9000}})
	sim.Give(essID, essence)
	if len(players) > 0 {
		sim.ShowPlayers(players...)
	}
	d := &fleetrunner.Deps{Session: sim, Chat: chatlog.New(), Components: actionsim.Components(),
		Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	return sim, d
}

func isTradeRequest(b []byte) bool {
	return len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4
}

func countOf(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

func allow(names ...string) func(string) bool {
	return func(n string) bool {
		for _, a := range names {
			if actions.SameUsername(a, n) {
				return true
			}
		}
		return false
	}
}

// ---- the crafter ----

func TestGateStepReceivesTheCarriersLoad(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
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
	if res.Kind != GateReceived || res.Essence != 25 || res.Partner != "Acct 1" {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 25 {
		t.Fatalf("the pack holds %d essence", got)
	}
	if !sim.TradeDone(partner) || requests.Load() != 1 {
		t.Fatalf("trade done %v after %d request(s)", sim.TradeDone(partner), requests.Load())
	}
	if w.Pending() != 0 {
		t.Fatal("the answered request is forgotten")
	}
}

// A request is answered once he is on his stand (or has waited GateStandHold for it), and not before: the engine walks the asker toward
// the one it asks, which pulls the carriers and the crafter off their tiles.
func TestGateStepAnswersOnTheStandNotBefore(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			requests.Add(1)
		}
	}
	onStand := false
	idled := 0
	d.Chat.Record("Acct 1:tradereq:")
	// (GateStandHold is 1.5 s on the game's clock: the clock runs at its own speed here, so the first looks are well inside it.)
	oldScale := gateClockScale
	gateClockScale = 1
	defer func() { gateClockScale = oldScale }()
	opts := GateOptions{Wait: 20 * time.Millisecond, OnStand: func() bool { return onStand }, Idle: func() { idled++ }}
	if res := GateStep(d, w, opts); res.Kind != GateNothing || requests.Load() != 0 || w.Pending() != 1 {
		t.Fatalf("answered off the stand: %+v after %d request(s), %d pending", res, requests.Load(), w.Pending())
	}
	if idled == 0 {
		t.Fatal("the idle hook (the walk onto the stand) was never called")
	}
	onStand = true
	GateStep(d, w, opts)
	if requests.Load() != 1 || w.Pending() != 0 {
		t.Fatalf("%d request(s), %d pending after he reached the stand", requests.Load(), w.Pending())
	}
	// A request left waiting longer than GateStandHold is answered wherever he is.
	d.Chat.Record("Acct 1:tradereq:")
	onStand = false
	gateClockScale = 4000 // GateStandHold (1.5 s) is under a millisecond
	GateStep(d, w, GateOptions{Wait: 100 * time.Millisecond, OnStand: func() bool { return false }})
	if requests.Load() != 2 {
		t.Fatalf("%d request(s): a request that has waited for the stand is answered where he is", requests.Load())
	}
}

// A held gate answers nothing: the stack of bows being cast away takes the slot a trade of 25 needs, and an answer sent in the lock of a
// cast is dropped by the engine. The request stays queued and is answered the moment the hold is over.
func TestGateStepHoldsWhileBowsAreCastAwayAndWhileLocked(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			requests.Add(1)
		}
	}
	hold, locked := true, true
	d.Chat.Record("Acct 1:tradereq:")
	opts := GateOptions{Wait: 150 * time.Millisecond, Hold: func() bool { return hold }, Delayed: func() bool { return locked }}
	GateStep(d, w, opts)
	if requests.Load() != 0 || w.Pending() != 1 {
		t.Fatalf("%d request(s), %d pending while held", requests.Load(), w.Pending())
	}
	hold = false
	GateStep(d, w, opts)
	if requests.Load() != 0 || w.Pending() != 1 {
		t.Fatalf("%d request(s), %d pending while locked", requests.Load(), w.Pending())
	}
	locked = false
	GateStep(d, w, opts)
	if requests.Load() != 1 || w.Pending() != 0 {
		t.Fatalf("%d request(s), %d pending after the hold", requests.Load(), w.Pending())
	}
}

func TestGateStepIgnoresStrangers(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Bob", "Acct 1")
	w := NewGateWatcher(d)
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			requests.Add(1)
		}
	}
	d.Chat.Record("Bob:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 150 * time.Millisecond})
	if res.Kind != GateNothing || requests.Load() != 0 || len(res.Strange) != 1 || res.Strange[0] != "Bob" {
		t.Fatalf("%+v after %d request(s)", res, requests.Load())
	}
}

// A request from before the gate opened (the chat log's history) is not answered.
func TestGateStepStartsAtTheEndOfTheChat(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	d.Chat.Record("Acct 1:tradereq:")
	w := NewGateWatcher(d)
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			requests.Add(1)
		}
	}
	if res := GateStep(d, w, GateOptions{Wait: 120 * time.Millisecond}); res.Kind != GateNothing || requests.Load() != 0 {
		t.Fatalf("%+v", res)
	}
}

// An offer still growing is not taken: 5 essence, then 25 a moment later, is one trade of 25.
func TestGateStepWaitsForTheOfferToSettle(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 5}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && !sim.TradeDone(partner) {
			sim.OpenTrade(partner)
			go func() {
				time.Sleep(15 * time.Millisecond)
				sim.SetTheirs([]actionsim.Stack{{ID: essID, Count: 25}})
			}()
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 3 * time.Second})
	if res.Kind != GateReceived || res.Essence != 25 {
		t.Fatalf("%+v", res)
	}
}

// A short offer is taken once it has stood for the short-offer wait.
func TestGateStepTakesAShortOfferEventually(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 7}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && !sim.TradeDone(partner) {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	started := time.Now()
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 3 * time.Second})
	if res.Kind != GateReceived || res.Essence != 7 {
		t.Fatalf("%+v", res)
	}
	if time.Since(started) < scaledDown(runecraftplan.GateSettleMax)/2 {
		t.Fatalf("a short offer was taken after %s", time.Since(started))
	}
}

func TestGateStepRefusesWhatIsNotEssence(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 20}, {ID: coinsID, Count: 5}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 2 * time.Second})
	if res.Kind != GateRefused || !strings.Contains(res.Reason, "not Rune essence") {
		t.Fatalf("%+v", res)
	}
	if sim.TradeDone(partner) || countOf(sim.Pack(), essID) != 0 {
		t.Fatal("nothing should have crossed")
	}
}

// A carrier who offers more than the crafter has room for is declined at once, not at the engine's refusal.
func TestGateStepRefusesAnOfferThatDoesNotFit(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	for i := 0; i < 2; i++ {
		sim.Give(2000+int32(i), 1) // two slots of junk: 23 free
	}
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 25}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 2 * time.Second})
	if res.Kind != GateRefused || !strings.Contains(res.Reason, "slots are free") {
		t.Fatalf("%+v", res)
	}
}

func TestGateStepGivesACourierItsNatures(t *testing.T) {
	sim, d := crafterSession(t, 6300, "Acct 2")
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 2", AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 2:tradereq:")
	res := GateStep(d, w, GateOptions{
		Allowed: allow("Acct 2"), Wait: 3 * time.Second,
		CourierQty: func(n string) int {
			if actions.SameUsername(n, "Acct 2") {
				return runecraftplan.CourierQuantity(6300, 4000, 3000)
			}
			return 0
		},
	})
	if res.Kind != GateGave || res.Natures != 2300 {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), NatureID); got != 4000 {
		t.Fatalf("the crafter holds %d Natures, want his keep of 4000", got)
	}
}

// A partner who is not a courier and offers nothing is declined after the wait for an offer.
func TestGateStepDeclinesAnEmptyScreen(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1")
	w := NewGateWatcher(d)
	partner := &actionsim.Trade{Partner: "Acct 1", AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 3 * time.Second})
	if res.Kind != GateRefused || !strings.Contains(res.Reason, "nothing was offered") {
		t.Fatalf("%+v", res)
	}
}

// One request at a time: two carriers asking in the same moment are answered in turn, never both at once.
func TestGateStepAnswersOneRequestAtATime(t *testing.T) {
	sim, d := crafterSession(t, 4000, "Acct 1", "Acct 2")
	w := NewGateWatcher(d)
	var requests atomic.Int32
	partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: 25}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			if requests.Add(1) == 1 {
				sim.OpenTrade(partner)
			}
		}
	}
	d.Chat.Record("Acct 1:tradereq:")
	d.Chat.Record("Acct 2:tradereq:")
	res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1", "Acct 2"), Wait: 3 * time.Second})
	if res.Kind != GateReceived || res.Partner != "Acct 1" || requests.Load() != 1 {
		t.Fatalf("%+v after %d request(s)", res, requests.Load())
	}
	if w.Pending() != 1 {
		t.Fatalf("the second carrier's request should wait its turn, %d pending", w.Pending())
	}
}

// ---- the carrier ----

func TestTradeWithCrafterOffersTheLoad(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	partner := &actionsim.Trade{Partner: "main", AcceptOffer: true, AcceptConfirm: true}
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) && requests.Add(1) == 3 { // the crafter was busy for the first two
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 5 * time.Second})
	if !res.Done || res.Moved != 25 || res.Requests != 3 {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 0 {
		t.Fatalf("%d essence left in the pack", got)
	}
	if got := countOf(sim.Pack(), runecraftplan.NotedEssenceID); got != 2900 {
		t.Fatalf("the notes are the kit and stay: %d", got)
	}
	if !sim.TradeDone(partner) || !strings.Contains(res.Describe(OfferEssence, "main"), "traded 25 essence") {
		t.Fatalf("done %v: %s", sim.TradeDone(partner), res.Describe(OfferEssence, "main"))
	}
}

func TestTradeWithCrafterGivesUpWhenHeIsNotInView(t *testing.T) {
	_, d := carrierSession(t, 25) // nobody in view
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 5 * time.Second, Absent: 80 * time.Millisecond})
	if res.Done || !res.NotHere || res.Requests != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestTradeWithCrafterTimesOutWhenHeNeverAnswers(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	var requests atomic.Int32
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			requests.Add(1)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 400 * time.Millisecond})
	if res.Done || res.NotHere || res.Requests < 2 || !strings.Contains(res.Reason, "no trade screen") {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 25 {
		t.Fatalf("the load is still in the pack: %d", got)
	}
}

// The screen of somebody else is declined, and the load stays put.
func TestTradeWithCrafterDeclinesAStranger(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	partner := &actionsim.Trade{Partner: "Mallory", AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 3 * time.Second})
	if res.Done || !strings.Contains(res.Reason, "Mallory") {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 25 {
		t.Fatalf("the load is still in the pack: %d", got)
	}
}

// A crafter who never accepts: the carrier declines in the end and gets its load back.
func TestTradeWithCrafterDeclinesWhenTheOfferIsNeverAccepted(t *testing.T) {
	sim, d := carrierSession(t, 25, "main")
	partner := &actionsim.Trade{Partner: "main", AcceptOffer: false}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: OfferEssence, Timeout: 3 * time.Second})
	if res.Done || !strings.Contains(res.Reason, "took longer") {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), essID); got != 25 {
		t.Fatalf("the declined load should be back in the pack: %d", got)
	}
}

func TestCourierTakesTheNatures(t *testing.T) {
	sim, d := carrierSession(t, 0, "main")
	partner := &actionsim.Trade{Partner: "main", Theirs: []actionsim.Stack{{ID: NatureID, Count: 2300}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: TakeNatures, Timeout: 4 * time.Second})
	if !res.Done || res.Moved != 2300 {
		t.Fatalf("%+v", res)
	}
	if got := countOf(sim.Pack(), NatureID); got != 2300 {
		t.Fatalf("the courier holds %d Natures", got)
	}
	if !strings.Contains(res.Describe(TakeNatures, "main"), "2300 Nature runes") {
		t.Fatal(res.Describe(TakeNatures, "main"))
	}
}

// A courier will not accept essence or anything else on the crafter's side.
func TestCourierDeclinesAScreenWithSomethingElse(t *testing.T) {
	sim, d := carrierSession(t, 0, "main")
	partner := &actionsim.Trade{Partner: "main", Theirs: []actionsim.Stack{{ID: NatureID, Count: 100}, {ID: coinsID, Count: 50}}, AcceptOffer: true, AcceptConfirm: true}
	sim.OnSend = func(b []byte) {
		if isTradeRequest(b) {
			sim.OpenTrade(partner)
		}
	}
	res := TradeWithCrafter(d, OfferOptions{Crafter: "main", Mode: TakeNatures, Timeout: 700 * time.Millisecond})
	if res.Done || countOf(sim.Pack(), NatureID) != 0 {
		t.Fatalf("%+v", res)
	}
}

// The gate trip's provisioning: the Natures stay in the pack, everything else is banked, the Nature talisman is drawn and exactly
// the fare is drawn (the coins in the pack were banked with the rest).
func TestVisitProvisionsTheGate(t *testing.T) {
	natTal := int32(runecraftplan.Nature.TalismanID)
	pack := []actionsim.Stack{{ID: NatureID, Count: 4000}, {ID: coinsID, Count: 37}, {ID: 317, Count: 1}, {ID: airTal, Count: 1}}
	bank := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsID, Count: 90000}, {ID: essID, Count: 300}}
	sim, d := bankRig(t, pack, bank)
	res := Visit(d, falBank, Want{Talisman: natTal, Coins: runecraftplan.GateFare, Keep: []int32{NatureID}}, nil)
	if !res.Opened || res.Err != "" || res.Quick {
		t.Fatalf("%+v", res)
	}
	p := sim.Pack()
	if count(p, natTal) != 1 || count(p, NatureID) != 4000 || count(p, coinsID) != runecraftplan.GateFare || len(p) != 3 {
		t.Fatalf("the gate pack is the talisman, the Natures and the fare: %v", p)
	}
	if b := sim.Bank(); count(b, coinsID) != 90000+37-runecraftplan.GateFare || count(b, 317) != 1 || count(b, airTal) != 1 || count(b, essID) != 300 || count(b, natTal) != 0 {
		t.Fatalf("bank %v", b)
	}
	// A visit with a keep list or coins is never the one-tick quick one.
	var q = Want{Talisman: natTal, Coins: 100}
	if q.quickable(d) {
		t.Fatal("a visit that draws coins is not quick")
	}
}

// The campaign's keep: the gate pack carries the Natures the main account keeps (there is no bank on Karamja), topped up from the bank to the keep
// and never past what the bank holds, and never a second stack on top of the Natures already in the pack.
func TestVisitDrawsTheKeptNatures(t *testing.T) {
	natTal := int32(runecraftplan.Nature.TalismanID)
	for _, c := range []struct {
		name                       string
		pack, bank, keep           int
		wantPack, wantBank, wantOf int
	}{
		{"an empty pack draws the whole keep", 0, 9000, 4000, 4000, 5000, 9000},
		{"a pack holding some is topped up to the keep", 1500, 9000, 4000, 4000, 6500, 9000},
		{"a pack holding the keep draws nothing", 4000, 9000, 4000, 4000, 9000, 9000},
		{"a pack over the keep draws nothing and banks nothing", 6000, 100, 4000, 6000, 100, 100},
		{"a bank with less than the keep gives what it has", 0, 700, 4000, 700, 0, 700},
		{"a bank with none", 0, 0, 4000, 0, 0, 0},
	} {
		pack := []actionsim.Stack{{ID: coinsID, Count: 37}}
		if c.pack > 0 {
			pack = append(pack, actionsim.Stack{ID: NatureID, Count: c.pack})
		}
		bank := []actionsim.Stack{{ID: natTal, Count: 1}, {ID: coinsID, Count: 90000}}
		if c.bank > 0 {
			bank = append(bank, actionsim.Stack{ID: NatureID, Count: c.bank})
		}
		sim, d := bankRig(t, pack, bank)
		res := Visit(d, falBank, Want{Talisman: natTal, Coins: runecraftplan.GateFare, Keep: []int32{NatureID}, Natures: c.keep}, nil)
		if !res.Opened || res.Err != "" || res.Quick {
			t.Errorf("%s: %+v", c.name, res)
			continue
		}
		if got := count(sim.Pack(), NatureID); got != c.wantPack {
			t.Errorf("%s: the pack holds %d Natures, want %d", c.name, got, c.wantPack)
		}
		if got := count(sim.Bank(), NatureID); got != c.wantBank {
			t.Errorf("%s: the bank holds %d Natures, want %d", c.name, got, c.wantBank)
		}
		if res.BankNatures != c.wantBank {
			t.Errorf("%s: the visit reports %d banked, want %d", c.name, res.BankNatures, c.wantBank)
		}
		if p := sim.Pack(); count(p, natTal) != 1 || count(p, coinsID) != runecraftplan.GateFare {
			t.Errorf("%s: the talisman and the fare are still drawn: %v", c.name, p)
		}
	}
	if (Want{Talisman: 1, Natures: 100}).quickable(nil) {
		t.Fatal("a visit that draws Natures is never the one-tick quick one")
	}
}

// The gate's trade capacity: 25 with the talisman, the Natures and the fare in the pack (whatever essence it holds), 24 with a stack of
// bows to alch, and what the pack could take is what DecideGateTrade accepts.
func TestTradeCapacityCountsTheSlotsBesideTheEssence(t *testing.T) {
	_, d := crafterSession(t, 4000)
	if got := TradeCapacity(d); got != runecraftplan.CarrierOffer {
		t.Fatalf("capacity %d with the usual three items", got)
	}
	sim, d2 := crafterSession(t, 4000)
	sim.Give(essID, 25)
	if got := TradeCapacity(d2); got != runecraftplan.CarrierOffer {
		t.Fatalf("capacity %d with 25 essence in the pack (they are not what takes the room)", got)
	}
	sim3, d3 := crafterSession(t, 4000)
	sim3.Give(noteBowID, 100)
	if got := TradeCapacity(d3); got != runecraftplan.CarrierOffer-1 {
		t.Fatalf("capacity %d with a stack of bows", got)
	}
}

// A crafter holding a stack of bows to alch has 24 free slots: a load of 24 is taken, a load of 25 is refused (the engine would refuse it
// for want of room), which is why the carriers are told to bring 24 (runecraftplan.GateInfo.ShelfWant).
func TestGateStepWithAStackOfBowsTakes24NotTwentyFive(t *testing.T) {
	for _, c := range []struct {
		offer int
		kind  GateKind
	}{{24, GateReceived}, {25, GateRefused}} {
		sim, d := crafterSession(t, 4000, "Acct 1")
		sim.Give(noteBowID, 300)
		w := NewGateWatcher(d)
		partner := &actionsim.Trade{Partner: "Acct 1", Theirs: []actionsim.Stack{{ID: essID, Count: c.offer}}, AcceptOffer: true, AcceptConfirm: true}
		sim.OnSend = func(b []byte) {
			if isTradeRequest(b) {
				sim.OpenTrade(partner)
			}
		}
		d.Chat.Record("Acct 1:tradereq:")
		res := GateStep(d, w, GateOptions{Allowed: allow("Acct 1"), Wait: 3 * time.Second})
		if res.Kind != c.kind {
			t.Fatalf("an offer of %d: %+v", c.offer, res)
		}
		if c.kind == GateRefused && !strings.Contains(res.Reason, "only 24 slots are free") {
			t.Fatalf("reason %q", res.Reason)
		}
	}
}
