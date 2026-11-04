package bot

// The Seers Market jam of 2026-09-25: 24 ProgressiveWCFletch accounts asked the stall for rune and
// adamant axes it had none of, each one learning that for itself every ten minutes, and held its
// one-buyer turn for about 85% of the time -- so ScarabSwarm's chaos buys and the mule's
// MarketBulkBuy lost it. These run the real ladder and backoff glue with no session or server.

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

func axeBuyerBot(seller string) *woodcutterBot {
	b := testWoodcutterBot("mule")
	b.settings.MarketSeller = seller
	b.settings.AxeWanted = woodcutplan.RuneAxe
	b.settings.AxePrice = 150000
	b.settings.AxeBudget = 165000
	return b
}

// TestAxeLadderSkipsWhatTheStallToldAnotherAccount: acct164 hears "I have no rune axe right now";
// acct138, which has never asked, must not walk up and ask for one -- and once every priced tier
// is known gone, it has nothing to buy and keeps the axe it is chopping with.
func TestAxeLadderSkipsWhatTheStallToldAnotherAccount(t *testing.T) {
	const stall = "Ladder Test Stall"
	now := time.Now()
	acct138 := axeBuyerBot(stall)

	full := woodcutplan.AxeTargets(acct138.settings.AxeWanted, acct138.settings.PriceOfAxe, acct138.marketSkip(now))
	if want := []string{"Rune axe", "Adamant axe", "Mithril axe"}; !reflect.DeepEqual(full, want) {
		t.Fatalf("ladder with nothing known = %v, want %v", full, want)
	}

	// Another account heard it. This account's own memory is empty.
	woodcutrunner.MarkMarketSoldOut(stall, "Rune axe", now)
	defer woodcutrunner.ClearMarketSoldOut(stall, "Rune axe")
	got := woodcutplan.AxeTargets(acct138.settings.AxeWanted, acct138.settings.PriceOfAxe, acct138.marketSkip(now))
	if want := []string{"Adamant axe", "Mithril axe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ladder after another account heard 'no rune axe' = %v, want %v", got, want)
	}

	woodcutrunner.MarkMarketSoldOut(stall, "Adamant axe", now)
	defer woodcutrunner.ClearMarketSoldOut(stall, "Adamant axe")
	woodcutrunner.MarkMarketSoldOut(stall, "Mithril axe", now)
	defer woodcutrunner.ClearMarketSoldOut(stall, "Mithril axe")
	gone := woodcutplan.AxeTargets(acct138.settings.AxeWanted, acct138.settings.PriceOfAxe, acct138.marketSkip(now))
	if len(gone) != 0 {
		t.Fatalf("ladder with every tier sold out fleet-wide = %v, want nothing to buy", gone)
	}
	buy := woodcutplan.DecideAxeBuyAmong([]string{"Steel axe"}, nil, gone, 150000, acct138.settings.PriceOfAxe)
	if buy.Want != "" {
		t.Errorf("with the market out of every tier the account still wants to buy %q, want it to keep its steel axe", buy.Want)
	}
	if tiers := acct138.soldOutTiers(now); !reflect.DeepEqual(tiers, []string{"Rune axe", "Adamant axe", "Mithril axe"}) {
		t.Errorf("soldOutTiers = %v", tiers)
	}

	// The note is hours, not the per-account ten minutes.
	later := now.Add(woodcutplan.DefaultUnavailableFor + time.Minute)
	if again := woodcutplan.AxeTargets(acct138.settings.AxeWanted, acct138.settings.PriceOfAxe, acct138.marketSkip(later)); len(again) != 0 {
		t.Errorf("eleven minutes on the ladder is back to %v; the fleet note must outlast the per-account one", again)
	}

	// A different stall is a different stock.
	other := axeBuyerBot("Some Other Stall")
	if l := woodcutplan.AxeTargets(other.settings.AxeWanted, other.settings.PriceOfAxe, other.marketSkip(now)); len(l) != 3 {
		t.Errorf("another stall's ladder = %v, want all three tiers", l)
	}
}

// TestMarketBusyWaitBacksOffAndResets: the fixed 90 s retry kept the losers in step. Consecutive
// lost turns now wait longer, and a trip that got the turn resets it.
func TestMarketBusyWaitBacksOffAndResets(t *testing.T) {
	b := axeBuyerBot("Seers Market")
	first := b.marketBusyWait()
	if first < woodcutplan.MarketBusyFirst*3/4 || first > woodcutplan.MarketBusyFirst*5/4 {
		t.Fatalf("first lost turn waits %s, want about %s", first, woodcutplan.MarketBusyFirst)
	}
	var third time.Duration
	b.marketBusyWait()
	third = b.marketBusyWait()
	if floor := 4 * woodcutplan.MarketBusyFirst * 3 / 4; third < floor {
		t.Fatalf("third lost turn in a row waits %s, want at least %s (doubling per loss)", third, floor)
	}
	for i := 0; i < 20; i++ {
		if w := b.marketBusyWait(); w > woodcutplan.MarketBusyMax*5/4 {
			t.Fatalf("lost-turn wait %s is over the cap", w)
		}
	}
	// What axeStep does after any trip that was not a lost turn.
	b.marketLosses = 0
	if w := b.marketBusyWait(); w > woodcutplan.MarketBusyFirst*5/4 {
		t.Errorf("after getting a turn the next loss waits %s, want the first-loss wait again", w)
	}
}

// TestBulkBuyWaitsAtTheStallOnALostTurn: the mule's MarketBulkBuy lost the turn seven times in
// eight on 2026-09-25 and walked 106 tiles home after every one. A lost turn is now waited out at
// the stall, a bounded number of times, and anything that is not a lost turn is final at once.
func TestBulkBuyWaitsAtTheStallOnALostTurn(t *testing.T) {
	busy := woodcutrunner.MarketBuyResult{Busy: true, Reason: "another account is buying from Seers Market right now"}
	var pauses []time.Duration
	delay := func(d time.Duration) bool { pauses = append(pauses, d); return true }

	calls := 0
	res := buyWaitingAtStall(delay, 4, 20*time.Second, func() woodcutrunner.MarketBuyResult {
		calls++
		if calls < 3 {
			return busy
		}
		return woodcutrunner.MarketBuyResult{Got: true, Paid: 9500}
	}, func(int, woodcutrunner.MarketBuyResult) bool { return true })
	if !res.Got || calls != 3 || len(pauses) != 2 {
		t.Fatalf("got=%v after %d calls and %d pauses, want the third try to buy after two waits", res.Got, calls, len(pauses))
	}

	// Bounded: four tries in all, then the lost turn is reported as it was.
	calls, pauses = 0, nil
	res = buyWaitingAtStall(delay, 4, 20*time.Second, func() woodcutrunner.MarketBuyResult { calls++; return busy }, nil)
	if !res.Busy || calls != 4 || len(pauses) != 3 {
		t.Fatalf("busy=%v after %d calls and %d pauses, want 4 tries and 3 waits", res.Busy, calls, len(pauses))
	}

	// A refusal that is not a lost turn is final: no waiting on "I have no chaos rune".
	calls = 0
	res = buyWaitingAtStall(delay, 4, 20*time.Second, func() woodcutrunner.MarketBuyResult {
		calls++
		return woodcutrunner.MarketBuyResult{Reason: "Seers Market said it cannot sell one"}
	}, nil)
	if calls != 1 || res.Busy {
		t.Fatalf("a stock refusal was retried %d times", calls)
	}

	// onBusy can end the wait (the ledger no longer covers the trade), and a stop is honoured.
	calls = 0
	buyWaitingAtStall(delay, 4, 20*time.Second, func() woodcutrunner.MarketBuyResult { calls++; return busy },
		func(int, woodcutrunner.MarketBuyResult) bool { return false })
	if calls != 1 {
		t.Errorf("onBusy returning false still retried (%d calls)", calls)
	}
	calls = 0
	res = buyWaitingAtStall(func(time.Duration) bool { return false }, 4, 20*time.Second,
		func() woodcutrunner.MarketBuyResult { calls++; return busy }, nil)
	if calls != 1 || !res.Stopped {
		t.Errorf("a stop during the wait: %d calls, stopped=%v", calls, res.Stopped)
	}
}

// TestEmptyLadderKeepsTheAxeOrRescuesTheAxeless: with every tier known sold out, an account holding
// an axe keeps chopping with it, and one holding NONE still goes to Bob's -- the rescue a refused
// market trip used to reach, which the fleet note must not skip.
func TestEmptyLadderKeepsTheAxeOrRescuesTheAxeless(t *testing.T) {
	const stall = "Empty Ladder Test Stall"
	now := time.Now()
	b := axeBuyerBot(stall)
	if action, _ := b.emptyLadder([]string{"Steel axe"}, now); action != ladderNothing {
		t.Fatalf("nothing sold out: action %d, want ladderNothing", action)
	}
	for _, a := range []string{"Rune axe", "Adamant axe", "Mithril axe"} {
		woodcutrunner.MarkMarketSoldOut(stall, a, now)
		defer woodcutrunner.ClearMarketSoldOut(stall, a)
	}
	if action, gone := b.emptyLadder([]string{"Steel axe"}, now); action != ladderKeepAxe || len(gone) != 3 {
		t.Errorf("holding a steel axe: action %d gone %v, want ladderKeepAxe over three tiers", action, gone)
	}
	if action, gone := b.emptyLadder(nil, now); action != ladderRescue || len(gone) == 0 || gone[0] != "Rune axe" {
		t.Errorf("holding no axe: action %d gone %v, want ladderRescue naming the rune axe first", action, gone)
	}
}
