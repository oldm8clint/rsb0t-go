package restockrunner

// These tests drive the WHOLE appeal/pledge/grant conversation over a real mulebus.Bus — a real
// Campaign on one side and real Answerers on the other, exchanging real messages. Nothing here
// is mocked, because nothing here needs a session: the only part of a restock that touches the
// game is the trip itself (donate.go), and that is what the live harness is for.

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/tile"
)

var post = tile.Tile{X: 2725, Z: 3491}

// lowMule is the live situation on 2026-09-21: 9,000,000 coins against a floor of 12,000,000
// derived from the mule's own configured 6,000,000-per-window hand-out ceiling.
func lowMule() restockplan.Need {
	return restockplan.Need{Item: restockplan.CoinsItem, Held: 9_000_000, Floor: 12_000_000, Target: 24_000_000}
}

func TestOneWholeConversation(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()

	mule := NewCampaign(3)
	// One donor with a real surplus, one with nothing spare. Answerers are created before the
	// appeal is published, which is what a running fleet looks like.
	rich := NewAnswerer("mule")
	broke := NewAnswerer("mule")

	// ---- the mule notices it is low and broadcasts ----
	step := mule.Step(now, bus, "mule", post, lowMule())
	if step.Appealed == nil {
		t.Fatalf("a mule at 9M against a 12M floor did not appeal: %s", step.Why)
	}
	if step.Appealed.Qty != 15_000_000 {
		t.Fatalf("appealed for %d, want 15,000,000", step.Appealed.Qty)
	}
	if len(step.Granted) != 0 {
		t.Fatalf("granted %d donors before anyone pledged", len(step.Granted))
	}

	// ---- the donors hear it ----
	if _, called, why := rich.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 80_000}); called {
		t.Fatalf("a broadcast appeal was read as a grant: %s", why)
	}
	if p, _ := rich.Stats(); p != 1 {
		t.Fatalf("the rich donor sent %d pledges, want 1", p)
	}
	if _, called, why := broke.Poll(now, bus, "fisher two", restockplan.DonorState{Banked: 300}); called {
		t.Fatal("a donor with 300 coins was called")
	} else if p, _ := broke.Stats(); p != 0 {
		t.Fatalf("a donor with 300 coins pledged anyway (%s)", why)
	}

	// ---- the mule collects the pledge and sends for the donor ----
	now = now.Add(2 * time.Second)
	step = mule.Step(now, bus, "mule", post, lowMule())
	if step.Pledged != 1 {
		t.Fatalf("the mule took %d pledges off the bus, want 1", step.Pledged)
	}
	if len(step.Granted) != 1 || step.Granted[0].To != "fisher one" {
		t.Fatalf("grants = %+v; want one, to \"fisher one\"", step.Granted)
	}
	want := 80_000 - restockplan.DonorReserveCoins
	if step.Granted[0].Qty != want {
		t.Fatalf("granted %d; the pledge was %d after the reserve", step.Granted[0].Qty, want)
	}

	// ---- the donor hears the grant, and only the donor it names ----
	now = now.Add(time.Second)
	call, called, why := rich.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 80_000})
	if !called {
		t.Fatalf("the granted donor was not called: %s", why)
	}
	if call.Mule != "mule" || call.Item != restockplan.CoinsItem || call.Post != post {
		t.Fatalf("call = %+v; want mule/Coins/%v", call, post)
	}
	if call.Qty != want {
		t.Fatalf("called for %d, want %d", call.Qty, want)
	}
	if _, called, _ := broke.Poll(now, bus, "fisher two", restockplan.DonorState{Banked: 300}); called {
		t.Fatal("a grant addressed to another account was taken")
	}

	// ---- the receipt: only the mule's own count settles it ----
	if got := mule.Outstanding(now); got != want {
		t.Fatalf("Outstanding = %d while the donor walks, want %d", got, want)
	}
	mule.Received(now, "fisher one", 78_000)
	if got := mule.Outstanding(now); got != 0 {
		t.Fatalf("Outstanding = %d after the donor arrived, want 0", got)
	}
	_, _, _, received := mule.Stats()
	if received != 78_000 {
		t.Fatalf("recorded %d received; it must be the real delta, not the %d pledged", received, want)
	}
}

// The stampede. Ninety accounts all hold coins; at most MaxInFlight may ever be walking.
func TestNinetyDonorsAreNotAllSentAtOnce(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	mule := NewCampaign(3)

	// Every donor exists BEFORE the appeal goes out. A mulebus.Watcher starts from the moment
	// it is made (matching mulerunner.NewRequestWatcher), so an account whose script starts
	// after an appeal simply misses that one and hears the next — which is the whole reason
	// restockplan.AppealInterval re-broadcasts, and is not what this test is about.
	donors := make([]*Answerer, 90)
	names := make([]string, 90)
	for i := range donors {
		donors[i] = NewAnswerer("mule")
		names[i] = fmt.Sprintf("donor%02d", i)
	}
	if step := mule.Step(now, bus, "mule", post, lowMule()); step.Appealed == nil {
		t.Fatalf("no appeal: %s", step.Why)
	}
	for i := range donors {
		if _, called, _ := donors[i].Poll(now, bus, names[i], restockplan.DonorState{Banked: 10_000 + i*1000}); called {
			t.Fatalf("%s was called by a broadcast", names[i])
		}
	}

	now = now.Add(time.Second)
	step := mule.Step(now, bus, "mule", post, lowMule())
	if step.Pledged != 90 {
		t.Fatalf("the mule heard %d pledges, want 90", step.Pledged)
	}
	if len(step.Granted) != 3 {
		t.Fatalf("sent for %d donors at once; the whole point is that it is at most 3", len(step.Granted))
	}
	// Richest first: donor89 (99,000 banked) down.
	if step.Granted[0].To != "donor89" {
		t.Fatalf("first grant went to %q; the largest pledge must go first", step.Granted[0].To)
	}

	// Exactly three accounts believe they were called.
	now = now.Add(time.Second)
	calledCount := 0
	for i, a := range donors {
		if _, called, _ := a.Poll(now, bus, names[i], restockplan.DonorState{Banked: 10_000 + i*1000}); called {
			calledCount++
		}
	}
	if calledCount != 3 {
		t.Fatalf("%d of 90 donors set off; want 3", calledCount)
	}

	// Three donors carrying 288,000 between them do not cover a 3,000,000 shortfall, so the
	// mule SHOULD appeal again — but for that much less. Netting the in-flight money off the
	// want is what stops a fleet answering the same shortfall over and over.
	inFlight := mule.Outstanding(now)
	if inFlight != 99_000+98_000+97_000-3*restockplan.DonorReserveCoins {
		t.Fatalf("in flight = %d", inFlight)
	}
	now = now.Add(restockplan.AppealInterval + time.Second)
	step = mule.Step(now, bus, "mule", post, lowMule())
	if step.Appealed == nil {
		t.Fatalf("stopped appealing with only %d of a 3,000,000 shortfall in flight: %s", inFlight, step.Why)
	}
	if step.Appealed.Qty != 15_000_000-inFlight {
		t.Fatalf("re-appealed for %d; want 15,000,000 less the %d already walking", step.Appealed.Qty, inFlight)
	}
}

// A donor must never be walked somewhere by a message from an account that is not its mule.
// The bus carries no authentication of any kind, which is exactly why this is checked.
func TestADonorOnlyAnswersItsOwnMule(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	a := NewAnswerer("mule")

	impostor := NewCampaign(3)
	if step := impostor.Step(now, bus, "notmule1", tile.Tile{X: 3100, Z: 3500}, lowMule()); step.Appealed == nil {
		t.Fatal("the impostor did not appeal, so the test proves nothing")
	}
	if _, called, _ := a.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 500_000}); called {
		t.Fatal("a donor was called by an account that is not its mule")
	}
	if p, _ := a.Stats(); p != 0 {
		t.Fatalf("a donor pledged %d time(s) to an account that is not its mule", p)
	}
}

// A grant is re-checked against the CURRENT reading, not obeyed. An account that spent its
// surplus between pledging and being called must not set off with nothing.
func TestAGrantIsRecheckedAgainstTheCurrentBalance(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	mule := NewCampaign(1)
	a := NewAnswerer("mule")

	mule.Step(now, bus, "mule", post, lowMule())
	if _, called, _ := a.Poll(now, bus, "spender", restockplan.DonorState{Banked: 200_000}); called {
		t.Fatal("called by a broadcast")
	}
	now = now.Add(time.Second)
	if step := mule.Step(now, bus, "mule", post, lowMule()); len(step.Granted) != 1 {
		t.Fatalf("granted %d, want 1", len(step.Granted))
	}
	// It spent the lot on an axe in the meantime.
	now = now.Add(time.Second)
	_, called, why := a.Poll(now, bus, "spender", restockplan.DonorState{Banked: 150_000, Earmarked: 150_000})
	if called {
		t.Fatal("an account whose coins are all earmarked set off to donate them")
	}
	if why == "" {
		t.Fatal("it declined the grant without saying why")
	}
}

// The mule must not keep asking the fleet for money that is already walking towards it.
func TestPromisedMoneyStopsTheMuleAskingTwice(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	mule := NewCampaign(3)
	// The answerer exists before the appeal, as a running donor does: a mulebus.Watcher sees only
	// what is published after it starts (its checkpoint is time.Now()), so an answerer made after
	// the appeal missed it wherever the clock ticks finer than the two calls (Linux, not Windows).
	a := NewAnswerer("mule")
	mule.Step(now, bus, "mule", post, lowMule())

	// One very rich donor pledges more than the whole shortfall.
	if _, called, _ := a.Poll(now, bus, "whale", restockplan.DonorState{Banked: 20_000_000}); called {
		t.Fatal("called by a broadcast")
	}
	now = now.Add(time.Second)
	if step := mule.Step(now, bus, "mule", post, lowMule()); len(step.Granted) != 1 {
		t.Fatalf("granted %d, want 1", len(step.Granted))
	}

	now = now.Add(restockplan.AppealInterval + time.Second)
	if step := mule.Step(now, bus, "mule", post, lowMule()); step.Appealed != nil {
		t.Fatalf("appealed again for %d while %d was in flight", step.Appealed.Qty, mule.Outstanding(now))
	}
	// Once the grant lapses unfulfilled, it must appeal again rather than wait forever.
	now = now.Add(restockplan.GrantTTL + time.Second)
	if step := mule.Step(now, bus, "mule", post, lowMule()); step.Appealed == nil {
		t.Fatalf("a lapsed grant left the mule permanently silent: %s", step.Why)
	}
}

// A healthy mule says nothing at all, and a mule with no restock configuration says nothing
// either — failing closed rather than broadcasting into a fleet that was never set up for this.
func TestAHealthyOrUnconfiguredMuleIsSilent(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	for _, c := range []struct {
		name string
		need restockplan.Need
	}{
		{"well above its floor", restockplan.Need{Item: restockplan.CoinsItem, Held: 29_837_076, Floor: 12_000_000, Target: 24_000_000}},
		{"no floor configured", restockplan.Need{Item: restockplan.CoinsItem, Held: 0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := NewCampaign(3)
			step := m.Step(now, bus, "mule", post, c.need)
			if step.Appealed != nil {
				t.Fatalf("appealed: %+v", step.Appealed)
			}
			if step.Why == "" {
				t.Fatal("said nothing and gave no reason — an operator reading the log learns nothing")
			}
			a := NewAnswerer("mule")
			if _, called, _ := a.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 500_000}); called {
				t.Fatal("a donor was called with no appeal on the bus")
			}
		})
	}
}

func TestNoBusOrNoNameIsANoOpNotAPanic(t *testing.T) {
	now := time.Now()
	m := NewCampaign(3)
	if step := m.Step(now, nil, "mule", post, lowMule()); step.Appealed != nil || step.Why == "" {
		t.Fatalf("a nil bus: %+v", step)
	}
	if step := m.Step(now, mulebus.NewBus(), "", post, lowMule()); step.Appealed != nil || step.Why == "" {
		t.Fatalf("a nameless mule: %+v", step)
	}
	a := NewAnswerer("")
	if _, called, why := a.Poll(now, mulebus.NewBus(), "me", restockplan.DonorState{Banked: 500_000}); called || why == "" {
		t.Fatal("an unconfigured answerer did something")
	}
}

// The cooldown is what spreads the load across a fleet instead of draining whoever polls first.
func TestADonorRestsBetweenTrips(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	m := NewCampaign(3)
	a := NewAnswerer("mule")

	m.Step(now, bus, "mule", post, lowMule())
	if _, called, _ := a.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 500_000}); called {
		t.Fatal("called by a broadcast")
	}
	if p, _ := a.Stats(); p != 1 {
		t.Fatal("did not pledge the first time")
	}
	a.Donated(now)

	now = now.Add(restockplan.AppealInterval + time.Second)
	m.Step(now, bus, "mule", post, lowMule())
	if _, called, _ := a.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 500_000}); called {
		t.Fatal("called inside the cooldown")
	}
	if p, _ := a.Stats(); p != 1 {
		t.Fatalf("pledged again %s after donating; the cooldown is %s", restockplan.AppealInterval, restockplan.AnswerCooldown)
	}

	// Past the cooldown it answers again. Only the pledge count is asserted, deliberately: a
	// mulebus.Watcher checkpoints on the wall clock and Bus.Since is INCLUSIVE of it, so a
	// message published inside the same clock tick as a poll can be delivered a second time —
	// on Windows, where the clock can sit still for a whole test, that is every message. That
	// is a property of the shared bus, not of this feature, and both sides here are built to
	// tolerate it (a re-delivered grant is re-screened against the current balance and the
	// cooldown; a re-delivered appeal produces a pledge the Board folds into the existing one
	// by name). Asserting "no grant was returned" would be asserting on the clock.
	now = now.Add(restockplan.AnswerCooldown)
	m.Step(now, bus, "mule", post, lowMule())
	_, called, _ := a.Poll(now, bus, "fisher one", restockplan.DonorState{Banked: 500_000})
	p, _ := a.Stats()
	if p < 2 && !called {
		t.Fatalf("still silent %s after donating, past the %s cooldown: %d pledge(s), called=%v",
			restockplan.AnswerCooldown, restockplan.AnswerCooldown, p, called)
	}
}
