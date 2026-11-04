package woodcutplan

import (
	"testing"
	"time"
)

// muleLadder is the real ask ladder, orderplan.AxeHandouts()'s items in its own order.
// Restated here rather than imported so this package keeps its "pure, no script imports" rule;
// TestMuleLadderMatchesOrderplan in internal/bot pins the two together.
var muleLadder = []string{"Rune axe", "Adamant axe", "Mithril axe", "Steel axe", "Iron axe", "Bronze axe"}

// TestMuleAxeLadderAgainstTheRealMuleBank is the fleet owner's report, as data.
//
// MEASURED 2026-09-22 from GET /api/bank?account=the mule on the live panel: the mule's 226-slot
// bank holds exactly one chopping axe, an Adamant axe, and adamant is deliberately absent from
// orderplan.AxeHandouts(). So every rung of the ladder a woodcutter can walk is empty, and the
// old code asked for all four anyway, every two minutes, after an 83-tile walk.
func TestMuleAxeLadderAgainstTheRealMuleBank(t *testing.T) {
	// The real reading, folded to the shape a bank publication gives: a WHOLE reading, so
	// every axe gets an answer, including the zeroes.
	bank := map[string]int{"Adamant axe": 1, "Steel battleaxe": 4, "Bronze pickaxe": 13, "Coins": 2331324}
	holds := func(axe string) (int, bool) { return bank[axe], true }

	ask, known := MuleAxeLadder(muleLadder, holds)
	if !known {
		t.Fatal("known = false, want true: a complete bank reading was supplied, and without known the caller cannot tell 'the mule has none' from 'nobody told us' and would keep walking to Catherby")
	}
	// Since 2026-10-04 adamant is on the ladder, so the one axe that bank held is the one rung
	// left; the four it held none of are still dropped, which is what the report was about.
	if len(ask) != 1 || ask[0] != "Adamant axe" {
		t.Fatalf("ask = %v, want [Adamant axe]: mule holds no rune, mithril, steel, iron or bronze axe, and asking for one is exactly what the owner reported", ask)
	}
}

// TestMuleAxeLadderKeepsWhatTheMuleReallyHas is the same path on the bank the mule held on
// 2026-09-21, recorded in orderplan.AxeHandouts's own doc comment: Mithril x6, Bronze x15,
// Iron x4, Steel x1, Adamant x1. Every rung is real there, so the ladder must be untouched --
// filtering must not become a reason to stop asking for an axe that is sitting there.
func TestMuleAxeLadderKeepsWhatTheMuleReallyHas(t *testing.T) {
	bank := map[string]int{"Mithril axe": 6, "Bronze axe": 15, "Iron axe": 4, "Steel axe": 1, "Adamant axe": 1}
	ask, known := MuleAxeLadder(muleLadder, func(axe string) (int, bool) { return bank[axe], true })
	if !known {
		t.Fatal("known = false, want true")
	}
	if want := muleLadder[1:]; len(ask) != len(want) {
		t.Fatalf("ask = %v, want every rung but rune kept (%v): the mule held every one of them", ask, want)
	}
	if ask[0] != "Adamant axe" {
		t.Errorf("ask[0] = %q, want %q: the caller's best-first order is the whole point of a ladder", ask[0], "Adamant axe")
	}
}

// TestMuleAxeLadderPartialStock pins the middle case: the mule has some of the ladder. Only
// the rungs it really holds may be asked for, in order.
func TestMuleAxeLadderPartialStock(t *testing.T) {
	bank := map[string]int{"Iron axe": 172, "Bronze axe": 7}
	ask, known := MuleAxeLadder(muleLadder, func(axe string) (int, bool) { return bank[axe], true })
	if !known {
		t.Fatal("known = false, want true")
	}
	want := []string{"Iron axe", "Bronze axe"}
	if len(ask) != len(want) || ask[0] != want[0] || ask[1] != want[1] {
		t.Fatalf("ask = %v, want %v: mithril and steel are not in that bank and must not be asked for", ask, want)
	}
}

// TestMuleAxeLadderWithNoInformationIsUnchanged is the nil-safety requirement, asserted rather
// than assumed: an account whose fleet has no bank directory at all (no peer sharing, or a mule
// that has not published yet) must ask exactly the blind ladder it always did.
func TestMuleAxeLadderWithNoInformationIsUnchanged(t *testing.T) {
	ask, known := MuleAxeLadder(muleLadder, nil)
	if known {
		t.Error("known = true with a nil holds, want false: claiming knowledge nobody supplied is how a fleet stops asking its mule for anything")
	}
	if len(ask) != len(muleLadder) {
		t.Fatalf("ask = %v, want the whole ladder %v unchanged", ask, muleLadder)
	}
}

// TestMuleAxeLadderKeepsAnUnknownRung covers a partial answer, which a stale directory row can
// genuinely give: "I have no opinion about this axe" must keep the rung, because a reading
// taken before a deposit would otherwise hide an axe that is really in the bank. Unknown is not
// zero -- the same rule PriceOfAxe applies to an unpriced axe.
func TestMuleAxeLadderKeepsAnUnknownRung(t *testing.T) {
	holds := func(axe string) (int, bool) {
		switch axe {
		case "Mithril axe":
			return 0, true // really read, really empty
		case "Steel axe":
			return 0, false // no opinion
		}
		return 0, true
	}
	ask, known := MuleAxeLadder(muleLadder, holds)
	if !known {
		t.Fatal("known = false, want true: two rungs were genuinely read")
	}
	if len(ask) != 1 || ask[0] != "Steel axe" {
		t.Fatalf("ask = %v, want [Steel axe]: the read-and-empty rungs go, the unknown one stays", ask)
	}
}

// TestAxeUnavailableRemembersAndLapses is the loop-breaker. Before this, a "none in the bank"
// decline was logged and thrown away, so the same four questions were asked again two minutes
// later, forever (the owner's report).
func TestAxeUnavailableRemembersAndLapses(t *testing.T) {
	var m AxeUnavailable
	t0 := time.Date(2026, 9, 22, 4, 11, 0, 0, time.UTC)

	if m.Unavailable("Mithril axe", t0) {
		t.Fatal("a fresh memory reported Mithril axe unavailable, want available: nothing has been refused yet")
	}
	m.Mark("Mithril axe", t0, 10*time.Minute)
	if !m.Unavailable("Mithril axe", t0.Add(9*time.Minute)) {
		t.Error("Mithril axe available again after 9 of 10 minutes, want still refused: this is the whole point, not asking a question already answered")
	}
	if m.Unavailable("Steel axe", t0) {
		t.Error("marking Mithril axe also refused Steel axe, want Steel unaffected: one shortage must not veto another tier")
	}
	if m.Unavailable("Mithril axe", t0.Add(11*time.Minute)) {
		t.Error("Mithril axe still refused after the memory lapsed, want available: a mule and a stall both restock, so a 'no' has to expire")
	}
}

// TestAxeUnavailableIsCaseAndSpaceInsensitive exists because the two sources spell an item
// differently in practice: orderplan's catalogue says "Mithril axe" while a roster or a chat
// line may say "mithril axe". A memory keyed on the exact spelling would silently never match.
func TestAxeUnavailableIsCaseAndSpaceInsensitive(t *testing.T) {
	var m AxeUnavailable
	t0 := time.Now()
	m.Mark(" mithril AXE ", t0, time.Minute)
	if !m.Unavailable("Mithril axe", t0) {
		t.Error("a 'no' recorded as \" mithril AXE \" did not match \"Mithril axe\", want a match: the two sources do not agree on spelling and a missed match silently restores the loop")
	}
}

// TestAxeUnavailableNeverShortensAStandingNo: a second refusal arriving while the first is
// still in force must not move the expiry closer. Two refusals are more evidence of a
// shortage, not less.
func TestAxeUnavailableNeverShortensAStandingNo(t *testing.T) {
	var m AxeUnavailable
	t0 := time.Now()
	m.Mark("Mithril axe", t0, 10*time.Minute)
	m.Mark("Mithril axe", t0, time.Second)
	if !m.Unavailable("Mithril axe", t0.Add(5*time.Minute)) {
		t.Error("a second one-second refusal shortened a standing ten-minute one, want the longer kept")
	}
}

// TestAxeUnavailableClearOnArrival: a mule that has just handed over a mithril axe plainly has
// them, so the memory must not keep the account off that rung for the rest of the window.
func TestAxeUnavailableClearOnArrival(t *testing.T) {
	var m AxeUnavailable
	t0 := time.Now()
	m.Mark("Mithril axe", t0, time.Hour)
	m.Clear("mithril axe")
	if m.Unavailable("Mithril axe", t0) {
		t.Error("Mithril axe still refused after one arrived, want the memory cleared")
	}
}

// TestNilAxeUnavailableIsSafeAndSilent is the nil-safety assertion for the memory itself: a
// caller that never records anything, and a nil pointer, must behave as if the type did not
// exist rather than panicking.
func TestNilAxeUnavailableIsSafeAndSilent(t *testing.T) {
	var m *AxeUnavailable
	now := time.Now()
	m.Mark("Mithril axe", now, time.Hour) // must not panic
	m.Clear("Mithril axe")
	if m.Unavailable("Mithril axe", now) {
		t.Error("a nil memory reported Mithril axe unavailable, want available")
	}
	if got := m.Available(muleLadder, now); len(got) != len(muleLadder) {
		t.Errorf("a nil memory filtered the ladder to %v, want all of %v", got, muleLadder)
	}
	if m.Skip(now)("Mithril axe") {
		t.Error("a nil memory's Skip refused Mithril axe, want false")
	}
}

// TestMuleDeclineIsEmptyStock separates the one decline that means "there is none" from the
// ones that do not. Getting this wrong in either direction has a live cost already recorded in
// this repo: reading a positional decline as scarcity is what walked the ladder down to a
// bronze axe while the mule held Mithril x6 (woodcutterhandout.go), and reading a real
// shortage as anything else is the owner's current report.
func TestMuleDeclineIsEmptyStock(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
		why    string
	}{
		{"none in the bank", true, "orderplan.Decide's own wording for a real, read-from-the-bank zero"},
		{"None In The Bank", true, "a Reason travels over the bus and is not case-canonical"},
		{"you are not in sight of me", false, "orderplan.DeclineNotInSight says nothing about stock; treating it as scarcity is the 2026-09-21 bronze-axe incident"},
		{"cannot read my own stock", false, "the mule admitting it does not know is not the mule saying there is none"},
		{"only 47 Crystal key banked, holding until 100", false, "a Floor gate is about a batch threshold, not an empty bank, and it clears as the mule accumulates"},
		{"over the window cap", false, "a window cap is temporary by nature and lapses on its own"},
		{"", false, "a silence is not an answer -- the same rule askMuleForOneAxeTier already applies to a timeout"},
	}
	for _, c := range cases {
		if got := MuleDeclineIsEmptyStock(c.reason); got != c.want {
			t.Errorf("MuleDeclineIsEmptyStock(%q) = %v, want %v: %s", c.reason, got, c.want, c.why)
		}
	}
}

// TestMarketRefusalIsStock does the same for the market path. The two reasons that mean the
// stall has none are the ones woodcutrunner.BuyAtMarket produces for exactly that, and the
// dangerous false positives are the ones about THIS account's coins or walking -- marking one
// of those would teach the account that an axe it can plainly buy does not exist.
func TestMarketRefusalIsStock(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
		why    string
	}{
		{`Seers Market said it cannot sell one ("sorry, out of stock")`, true, "the stall answered in public chat and awaitSellerReply already read it as a refusal"},
		{"the seller never offered it", true, "the trade opened, our coins registered, and the item never appeared: the same fact arrived at silently"},
		{"not carrying the coins to pay for it", false, "about this account, not the stall -- and remembering it would skip a tier the account could afford next trip"},
		{"the price is over the configured budget", false, "a configuration limit, not a shortage"},
		{"could not reach the market", false, "a walk that failed says nothing about stock"},
		{"nobody at the market answered", false, "an absent stall will be back; this is the reason MarketBuyResult.Busy exists beside it"},
		{"the trade closed without the item arriving", false, "a mechanical failure, and retrying it is the right move"},
	}
	for _, c := range cases {
		if got := MarketRefusalIsStock(c.reason); got != c.want {
			t.Errorf("MarketRefusalIsStock(%q) = %v, want %v: %s", c.reason, got, c.want, c.why)
		}
	}
}

// axePrices is the live price book, measured 2026-09-22 from GET /api/prices: rune 150,000 over
// 98 trades, adamant 3,150 over 7, mithril 600 over 4. Steel, iron, bronze and black have no
// completed trades and therefore no price, which is what makes them absent from a buy ladder.
func axePrices(axe string) (int, bool) {
	p, ok := DefaultAxePrices[canonicalAxe(axe)]
	return p, ok && p > 0
}

// TestAxeTargetsIsTheFallbackTheCommentPromised is the acct134 case exactly.
//
// MEASURED 2026-09-22: acct134 carries 150,000 coins and no axe, AxeWanted is a rune axe, and
// the Seers market's rune-axe stock is ZERO while a mithril axe is 600 coins and nine deep.
// woodcutteraxe.go's comment claimed the account laddered down to adamant and mithril; DecideAxeBuy
// only ever considered the one configured tier, so it said "buying 1 rune axe" forever.
func TestAxeTargetsIsTheFallbackTheCommentPromised(t *testing.T) {
	got := AxeTargets(RuneAxe, axePrices, nil)
	want := []string{RuneAxe, "Adamant axe", "Mithril axe"}
	if len(got) != len(want) {
		t.Fatalf("AxeTargets(rune) = %v, want %v: the priced tiers below the target are the fallback", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AxeTargets(rune) = %v, want %v (best first)", got, want)
		}
	}
}

// TestAxeTargetsSkipsWhatTheMarketRefused: once the stall has said it has no rune axe, the
// ladder must start at adamant. This is what turns a remembered refusal into a different
// purchase rather than a longer wait.
func TestAxeTargetsSkipsWhatTheMarketRefused(t *testing.T) {
	var m AxeUnavailable
	now := time.Now()
	m.Mark(RuneAxe, now, time.Hour)
	m.Mark("Adamant axe", now, time.Hour)

	got := AxeTargets(RuneAxe, axePrices, m.Skip(now))
	if len(got) != 1 || got[0] != "Mithril axe" {
		t.Fatalf("AxeTargets = %v, want [Mithril axe]: rune and adamant were both refused, and mithril is the tier actually in stock at 600 coins", got)
	}
}

// TestAxeTargetsKeepsAnUnpricedTarget pins the additive contract: an operator who has
// configured no prices at all must get back exactly the single target DecideAxeBuy always
// returned, so this change cannot alter their run. An unpriced CHEAPER tier is still excluded,
// because there is no safe offer to make for it -- BuyAtMarket refuses a zero price anyway.
func TestAxeTargetsKeepsAnUnpricedTarget(t *testing.T) {
	none := func(string) (int, bool) { return 0, false }
	got := AxeTargets("Steel axe", none, nil)
	if len(got) != 1 || got[0] != "Steel axe" {
		t.Fatalf("AxeTargets(steel, no prices) = %v, want [Steel axe] exactly: this is the pre-change behaviour and it must be preserved", got)
	}
	if nilPrices := AxeTargets("Steel axe", nil, nil); len(nilPrices) != 1 || nilPrices[0] != "Steel axe" {
		t.Fatalf("AxeTargets with a nil priceOf = %v, want [Steel axe]", nilPrices)
	}
}

// TestAxeTargetsNeverReachesAboveTheTarget: "better than asked for" is not this function's
// call. An operator's axeWanted is a budget statement as much as a preference, and a fleet that
// silently upgraded itself to rune axes would spend 150,000 coins an account.
func TestAxeTargetsNeverReachesAboveTheTarget(t *testing.T) {
	for _, a := range AxeTargets("Mithril axe", axePrices, nil) {
		if axeIndex(a) > axeIndex("Mithril axe") {
			t.Errorf("AxeTargets(mithril) offered %q, which is above the target", a)
		}
	}
}

// TestAxeTargetsRejectsNonsense: an empty or non-axe target yields no ladder at all rather than
// a default, matching DecideAxeBuy's own "no axe target configured".
func TestAxeTargetsRejectsNonsense(t *testing.T) {
	for _, in := range []string{"", "   ", "Rune pickaxe", "Steel battleaxe", "Broken axe"} {
		if got := AxeTargets(in, axePrices, nil); len(got) != 0 {
			t.Errorf("AxeTargets(%q) = %v, want none: only a real chopping axe is a target, and a pickaxe or battleaxe does not chop", in, got)
		}
	}
}

// TestDecideAxeBuyAmongBuysTheTierThatIsActuallyInStock walks the whole acct134 decision with
// the live numbers, one refusal at a time, and asserts what the account ends up doing.
func TestDecideAxeBuyAmongBuysTheTierThatIsActuallyInStock(t *testing.T) {
	var market AxeUnavailable
	now := time.Now()
	const coins = 150000 // acct134's real pack, measured

	// Pass one: nothing refused yet, so the configured rune axe is what it goes for. Need is
	// zero because it already carries exactly the rune price.
	buy := DecideAxeBuyAmong(nil, nil, AxeTargets(RuneAxe, axePrices, market.Skip(now)), coins, axePrices)
	if buy.Want != RuneAxe || buy.From != AxeFromMarket {
		t.Fatalf("first pass wanted %q from %q, want a rune axe from the market: that is the configured target and nothing has refused it", buy.Want, buy.From)
	}
	if buy.Need != 0 {
		t.Errorf("Need = %d, want 0: the account carries 150,000 and the rune price is 150,000", buy.Need)
	}

	// The stall has no rune axe (market stock 0, measured). Remember it.
	market.Mark(RuneAxe, now, time.Hour)
	buy = DecideAxeBuyAmong(nil, nil, AxeTargets(RuneAxe, axePrices, market.Skip(now)), coins, axePrices)
	if buy.Want != "Adamant axe" {
		t.Fatalf("after the rune axe was refused the account wanted %q, want an Adamant axe: it is the next priced tier down", buy.Want)
	}

	// No adamant axe either (market stock 0, measured -- only Adamant BATTLEaxe x10).
	market.Mark("Adamant axe", now, time.Hour)
	buy = DecideAxeBuyAmong(nil, nil, AxeTargets(RuneAxe, axePrices, market.Skip(now)), coins, axePrices)
	if buy.Want != "Mithril axe" {
		t.Fatalf("after rune and adamant were refused the account wanted %q, want a Mithril axe: stock 9 at 600 coins, which is what it should have bought hours ago", buy.Want)
	}
	if buy.Need != 0 || !buy.Priced {
		t.Errorf("Need = %d, Priced = %v; want 0 and true: 150,000 coins covers a 600-coin axe outright", buy.Need, buy.Priced)
	}
}

// TestDecideAxeBuyAmongStopsAtTheAxeInHand is the property that makes a long ladder safe. An
// account already holding the last remaining target must not buy a second copy of what it is
// using -- and this is the shape of the loop that cost twelve live woodcutters their whole
// session on 2026-09-21 (104-tile round trips at zero experience).
func TestDecideAxeBuyAmongStopsAtTheAxeInHand(t *testing.T) {
	var market AxeUnavailable
	now := time.Now()
	market.Mark(RuneAxe, now, time.Hour)
	market.Mark("Adamant axe", now, time.Hour)

	buy := DecideAxeBuyAmong([]string{"Mithril axe"}, nil, AxeTargets(RuneAxe, axePrices, market.Skip(now)), 150000, axePrices)
	if buy.Want != "" {
		t.Fatalf("wanted %q while already holding a mithril axe and every better tier refused, want nothing to do", buy.Want)
	}
}

// TestDecideAxeBuyAmongPrefersTheFreeBankedAxe: a banked axe that meets a target costs nothing
// and must beat buying one, on whichever rung of the ladder it appears.
func TestDecideAxeBuyAmongPrefersTheFreeBankedAxe(t *testing.T) {
	buy := DecideAxeBuyAmong(nil, []string{"Adamant axe"}, AxeTargets("Adamant axe", axePrices, nil), 0, axePrices)
	if buy.From != AxeFromBank || buy.Want != "Adamant axe" {
		t.Fatalf("got %q from %q, want an Adamant axe from the bank: it is already owned and free", buy.Want, buy.From)
	}
}

// TestDecideAxeBuyAmongWithNoTargetsSaysSo keeps the "" case answering the way DecideAxeBuy
// always did, so a caller's existing handling of an unconfigured target is unchanged.
func TestDecideAxeBuyAmongWithNoTargetsSaysSo(t *testing.T) {
	buy := DecideAxeBuyAmong(nil, nil, nil, 1000, axePrices)
	if buy.Want != "" || buy.Why == "" {
		t.Fatalf("got Want %q Why %q, want nothing wanted and a stated reason", buy.Want, buy.Why)
	}
}

// TestDecideAxeBuyAmongMatchesDecideAxeBuyForOneTarget is the additive assertion: with a single
// target the new function must be indistinguishable from the old one, since that is exactly
// what every existing caller and test relies on.
func TestDecideAxeBuyAmongMatchesDecideAxeBuyForOneTarget(t *testing.T) {
	cases := []struct {
		held, banked []string
		want         string
		coins        int
	}{
		{nil, nil, RuneAxe, 0},
		{nil, nil, RuneAxe, 150000},
		{[]string{"Bronze axe"}, nil, "Mithril axe", 600},
		{[]string{RuneAxe}, nil, RuneAxe, 0},
		{nil, []string{"Mithril axe"}, "Mithril axe", 0},
		{nil, nil, "Steel axe", 0},
		{nil, nil, "", 0},
	}
	for _, c := range cases {
		one := DecideAxeBuy(c.held, c.banked, c.want, c.coins, axePrices)
		among := DecideAxeBuyAmong(c.held, c.banked, []string{c.want}, c.coins, axePrices)
		if c.want == "" {
			// AxeTargets rejects an empty target, so the ladder is empty and the two agree
			// on "nothing wanted" rather than on the exact wording.
			if one.Want != "" || among.Want != "" {
				t.Errorf("an empty target wanted %q / %q, want nothing from either", one.Want, among.Want)
			}
			continue
		}
		if one != among {
			t.Errorf("held=%v banked=%v want=%q coins=%d: DecideAxeBuy = %+v but DecideAxeBuyAmong = %+v; a single-target ladder must be identical to the old call or this change is not additive",
				c.held, c.banked, c.want, c.coins, one, among)
		}
	}
}
