package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/orderplan"
)

func TestMuleTopUp(t *testing.T) {
	now := time.Date(2026, 9, 26, 17, 0, 0, 0, time.UTC)
	base := TopUpInput{Below: 150000, Amount: 300000, BankCoins: 43000, PackCoins: 300,
		MuleWorld: 1, MuleWorldKnown: true, MyWorld: 1, MuleHeard: true, Now: now}
	for _, c := range []struct {
		name string
		edit func(*TopUpInput)
		want int
	}{
		{"live 26 Sep: 43k left, mule here", func(*TopUpInput) {}, 300000},
		{"never asked before counts as due", func(in *TopUpInput) { in.LastAsk = time.Time{} }, 300000},
		{"off: topUpBelow 0", func(in *TopUpInput) { in.Below = 0 }, 0},
		{"off: topUpAmount 0", func(in *TopUpInput) { in.Amount = 0 }, 0},
		{"bank plus pack at the threshold", func(in *TopUpInput) { in.BankCoins, in.PackCoins = 100000, 50000 }, 0},
		{"pack coins count", func(in *TopUpInput) { in.BankCoins, in.PackCoins = 10000, 200000 }, 0},
		{"mule world unknown", func(in *TopUpInput) { in.MuleWorldKnown = false }, 0},
		{"mule on another world", func(in *TopUpInput) { in.MuleWorld = 2 }, 0},
		{"mule not heard", func(in *TopUpInput) { in.MuleHeard = false }, 0},
		{"asked 5 minutes ago", func(in *TopUpInput) { in.LastAsk = now.Add(-5 * time.Minute) }, 0},
		{"asked just over TopUpRetry ago", func(in *TopUpInput) { in.LastAsk = now.Add(-TopUpRetry - time.Second) }, 300000},
	} {
		in := base
		c.edit(&in)
		v := MuleTopUp(in)
		if v.Ask != c.want {
			t.Errorf("%s: ask %d (%s), want %d", c.name, v.Ask, v.Reason, c.want)
		}
		if v.Reason == "" {
			t.Errorf("%s: no reason given", c.name)
		}
	}
}

func TestTopUpNextOrder(t *testing.T) {
	for _, c := range []struct {
		name                      string
		ask, got, orders, lastGot int
		want                      int
	}{
		{"first order asks for all of it", 300000, 0, 0, 0, 300000},
		{"mule clamped the first to 170k: ask the rest", 300000, 170000, 1, 170000, 130000},
		{"all received", 300000, 300000, 2, 130000, 0},
		{"more than asked (never negative)", 300000, 340000, 2, 170000, 0},
		{"last order brought nothing: stop", 300000, 170000, 2, 0, 0},
		{"first order brought nothing: stop", 300000, 0, 1, 0, 0},
		{"order cap reached", 900000, 510000, TopUpMaxOrders, 170000, 0},
	} {
		if got := TopUpNextOrder(c.ask, c.got, c.orders, c.lastGot); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	// A mule that always clamps to 170k: the loop places at most TopUpMaxOrders orders.
	ask, got, orders, last := TopUpMaxOrders*170000+1, 0, 0, 0
	for {
		n := TopUpNextOrder(ask, got, orders, last)
		if n <= 0 {
			break
		}
		if n > 170000 {
			n = 170000
		}
		got, last, orders = got+n, n, orders+1
	}
	if orders != TopUpMaxOrders || got != TopUpMaxOrders*170000 {
		t.Errorf("clamping mule: %d orders for %d coins, want %d orders", orders, got, TopUpMaxOrders)
	}
}

func TestAffordable(t *testing.T) {
	runes := []string{"Mind rune", "Air rune", "Chaos rune"}
	caps := Caps{"Mind rune": 18, "Air rune": 24, "Chaos rune": 90}
	// Lundail's shelf as seen live on world 2, 26 September 16:33.
	lundail := map[string]int{"Mind rune": 64, "Air rune": 144, "Chaos rune": 213}
	in := AffordInput{Shop: fleetplan.Lundail, Stock: lundail, Runes: runes, Caps: caps, MinVisitRunes: 50}

	in.Coins = 0
	if ok, n, why := Affordable(in); ok || n != 0 || why == "" {
		t.Errorf("empty purse: ok=%v n=%d why=%q, want a skip", ok, n, why)
	}
	in.Coins = 18573 // the trip's planned coins that day
	if ok, n, _ := Affordable(in); !ok || n < 50 {
		t.Errorf("18,573 coins: ok=%v n=%d, want a visit", ok, n)
	}
	in.Coins = 200 // enough for a few minds only
	if ok, n, why := Affordable(in); ok || n >= 50 || why == "" {
		t.Errorf("200 coins: ok=%v n=%d why=%q, want a skip under minVisitRunes", ok, n, why)
	}
	if !GundaiShort(AffordInput{Shop: fleetplan.Lundail, Stock: lundail, Runes: runes, Caps: caps, MinVisitRunes: 50}) {
		t.Error("a Gundai bank that never opened (0 coins) must count as short")
	}
	// Same numbers as PlanVisit without a reserve.
	in.Coins = 5034
	_, n, _ := Affordable(in)
	if want := PlanVisit(fleetplan.Lundail, lundail, 5034, runes, caps, nil).Runes(); n != want {
		t.Errorf("Affordable runes %d != PlanVisit %d", n, want)
	}
}

func TestSpendableFor(t *testing.T) {
	for _, c := range []struct {
		name                string
		pack, fare          int
		read                bool
		bank, reserve, trip int
		canDraw             bool
		want                int
	}{
		{"pack only, no draw on the way", 5000, 300, true, 500000, 10000, 100000, false, 4700},
		{"pack below the fare counts 0", 200, 300, true, 0, 10000, 100000, false, 0},
		{"live 26 Sep 16:56: bank at the reserve, pack at the fare", 300, 300, true, 10000, 10000, 100000, true, 0},
		{"a draw capped at tripCoins", 300, 300, true, 500000, 10000, 100000, true, 100000},
		{"a draw from what the bank spares", 1300, 300, true, 30000, 10000, 100000, true, 21000},
		{"bank never read: assume a full draw", 0, 300, false, 0, 10000, 100000, true, 100000},
		{"bank under its reserve adds nothing", 5300, 300, true, 5000, 10000, 100000, true, 5000},
	} {
		if got := SpendableFor(c.pack, c.fare, c.read, c.bank, c.reserve, c.trip, c.canDraw); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// A top-up order never asks more than the order grammar carries: a 2,000,000 ask was refused whole.
func TestTopUpOrdersStayUnderTheOrderCeiling(t *testing.T) {
	if TopUpOrderMax > orderplan.QtyMax {
		t.Fatalf("TopUpOrderMax %d over orderplan.QtyMax %d", TopUpOrderMax, orderplan.QtyMax)
	}
	if got := TopUpNextOrder(MaxTopUpAmount, 0, 0, 0); got != TopUpOrderMax {
		t.Fatalf("first order of a %d top-up = %d, want %d", MaxTopUpAmount, got, TopUpOrderMax)
	}
	if got := TopUpNextOrder(MaxTopUpAmount, 170000, 1, 170000); got != TopUpOrderMax {
		t.Fatalf("second order = %d", got)
	}
	if got := TopUpNextOrder(300000, 170000, 1, 170000); got != 130000 {
		t.Fatalf("the rest of a small top-up = %d, want 130000", got)
	}
}

// The old rule (topUpWholeStack off), with the floor of 13 October 2026 written out: the runner never takes
// the mule's last coins, and the mule's published coin count caps the ask above the floor.
func TestMuleTopUpLeavesTheMuleItsFloor(t *testing.T) {
	const muleCoinFloor = 100000
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	base := TopUpInput{Below: 5894578, Amount: 5884578, BankCoins: 10001, PackCoins: 0,
		MuleWorld: 1, MuleWorldKnown: true, MyWorld: 1, MuleHeard: true, Now: now, MuleFloor: muleCoinFloor}
	for _, c := range []struct {
		name  string
		coins int
		known bool
		want  int
	}{
		{"published count unknown: the whole ask", 0, false, 5884578},
		{"the mule holds far more than the ask", 9000000, true, 5884578},
		{"13 Oct: mule held 636,748", 636748, true, 536748},
		{"only 150,000: 50,000 above the floor", 150000, true, 50000},
		{"exactly the floor and the margin", muleCoinFloor + NoCoinsMargin, true, NoCoinsMargin},
		{"a sliver above the floor is not worth the ask", muleCoinFloor + NoCoinsMargin - 1, true, 0},
		{"at the floor", muleCoinFloor, true, 0},
		{"the mule published and holds none", 0, true, 0},
	} {
		in := base
		in.MuleCoins, in.MuleCoinsKnown = c.coins, c.known
		v := MuleTopUp(in)
		if v.Ask != c.want {
			t.Errorf("%s: ask %d (%s), want %d", c.name, v.Ask, v.Reason, c.want)
		}
		if c.want == 0 && !strings.Contains(v.Reason, "kept for the cohort") {
			t.Errorf("%s: no reason for holding back: %q", c.name, v.Reason)
		}
	}
	// The floor never raises an ask the mule can cover, and an ask already refused for other reasons
	// is still refused.
	in := base
	in.Amount, in.MuleCoins, in.MuleCoinsKnown = 300000, 1000000, true
	if v := MuleTopUp(in); v.Ask != 300000 {
		t.Errorf("a small ask from a rich mule: %d (%s)", v.Ask, v.Reason)
	}
	in.MuleHeard = false
	if v := MuleTopUp(in); v.Ask != 0 {
		t.Errorf("a mule not heard was asked: %d", v.Ask)
	}
}

// A hand-out limit is not an empty bank: the first full-shelf ask is twelve orders and a donor is
// handed eight a window.
func TestLimitDecline(t *testing.T) {
	for reason, want := range map[string]bool{
		"already handed you one recently":       true,
		"hand-out limit for that item is spent": true,
		"none in the bank":                      false,
		"I cannot identify Coins":               false,
		"the mule never handed it over":         false,
		"":                                      false,
		"mule: Already handed you one recently": true,
	} {
		if got := LimitDecline(reason); got != want {
			t.Errorf("LimitDecline(%q) = %v, want %v", reason, got, want)
		}
	}
}

// THE OWNER'S ASK, 14 October 2026: "request mules entire cash stack and spend it on everything instead of just
// running out". Live that day the round's bill was 1,109,730 with the reserve, the runner held 857,720 and asked
// topUpAmount, 300,000, while the mule published 11,229,284. With topUpWholeStack the ask is the whole stack above
// the floor, whatever topUpAmount or the shortfall say.
func TestMuleTopUpAsksTheMulesWholeStack(t *testing.T) {
	now := time.Date(2026, 10, 14, 13, 24, 0, 0, time.UTC)
	live := TopUpInput{Below: 1109730, Amount: 300000, BankCoins: 764906, PackCoins: 92814,
		MuleWorld: 1, MuleWorldKnown: true, MyWorld: 1, MuleHeard: true, Now: now,
		MuleCoins: 11229284, MuleCoinsKnown: true, MuleFloor: DefaultMuleCoinFloor}

	// Before: the 300,000 topUpAmount, 252,000 short of the round's bill. After: everything above the floor.
	old := live
	v := MuleTopUp(old)
	if v.Ask != 300000 {
		t.Fatalf("with the whole stack off the ask is topUpAmount clamped to the spare: %d (%s)", v.Ask, v.Reason)
	}
	live.WholeStack = true
	v = MuleTopUp(live)
	if want := 11229284 - DefaultMuleCoinFloor; v.Ask != want {
		t.Fatalf("whole stack: ask %d, want %d (%s)", v.Ask, want, v.Reason)
	}
	for _, want := range []string{"whole published stack", "11219284 of its 11229284", "10000 of them kept", "topUpAmount 300000 is not a cap"} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("reason %q lacks %q", v.Reason, want)
		}
	}

	for _, c := range []struct {
		name string
		edit func(*TopUpInput)
		want int
	}{
		{"a stack under the old 300,000 is still all of it", func(in *TopUpInput) { in.MuleCoins = 120000 }, 110000},
		{"a stack far over the old 6,000,000 cap is all of it", func(in *TopUpInput) { in.MuleCoins = 40000000 }, 40000000 - DefaultMuleCoinFloor},
		{"floor 0 takes every coin", func(in *TopUpInput) { in.MuleFloor = 0 }, 11229284},
		{"a larger floor is kept", func(in *TopUpInput) { in.MuleFloor = 250000 }, 11229284 - 250000},
		{"topUpAmount 0 still turns the top-up off", func(in *TopUpInput) { in.Amount = 0 }, 0},
		{"a runner that holds the bill is not asking", func(in *TopUpInput) { in.BankCoins = 1109730 }, 0},
		{"the mule on another world", func(in *TopUpInput) { in.MuleWorld = 2 }, 0},
		{"the mule not heard", func(in *TopUpInput) { in.MuleHeard = false }, 0},
		{"asked 5 minutes ago", func(in *TopUpInput) { in.LastAsk = now.Add(-5 * time.Minute) }, 0},
		{"a mule with only its floor and the margin", func(in *TopUpInput) { in.MuleCoins = DefaultMuleCoinFloor + NoCoinsMargin - 1 }, 0},
		{"a mule that published an empty bank", func(in *TopUpInput) { in.MuleCoins = 0 }, 0},
	} {
		in := live
		c.edit(&in)
		got := MuleTopUp(in)
		if got.Ask != c.want {
			t.Errorf("%s: ask %d (%s), want %d", c.name, got.Ask, got.Reason, c.want)
		}
		if got.Reason == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}

	// No published stack: the shortfall to the bill, never under topUpAmount, and the mule gives what it holds.
	in := live
	in.MuleCoinsKnown, in.MuleCoins = false, 0
	if got := MuleTopUp(in); got.Ask != 300000 || !strings.Contains(got.Reason, "not published") {
		t.Errorf("unpublished stack, small shortfall: %d (%s)", got.Ask, got.Reason)
	}
	in.Below, in.BankCoins, in.PackCoins = 5000000, 100000, 0
	if got := MuleTopUp(in); got.Ask != 4900000 {
		t.Errorf("unpublished stack, large shortfall: %d (%s)", got.Ask, got.Reason)
	}
}

// The whole stack is placed as one order per coinsPerOrder (1,000,000 live) until it is in: the mule's 11,219,284 is
// twelve orders, and a mule that gives less than it published ends the ask on the order that brings nothing.
func TestWholeStackIsPlacedInAsManyOrdersAsTheMuleNeeds(t *testing.T) {
	run := func(ask, mulePerOrder, muleHolds int) (got, orders int) {
		last := 0
		for {
			n := TopUpNextOrder(ask, got, orders, last)
			if n <= 0 {
				return got, orders
			}
			give := min(n, mulePerOrder, muleHolds-got)
			got, last, orders = got+give, give, orders+1
		}
	}
	if got, orders := run(11219284, 1000000, 11229284); got != 11219284 || orders != 12 {
		t.Errorf("the live stack: %d coins in %d orders, want 11219284 in 12", got, orders)
	}
	// The old cap of 12 orders would have stopped a bigger stack short; forty carries 40,000,000.
	if got, orders := run(30000000, 1000000, 30000000); got != 30000000 || orders != 30 {
		t.Errorf("a 30M stack: %d in %d orders", got, orders)
	}
	if got, orders := run(200000000, 1000000, 200000000); orders != TopUpMaxOrders || got != TopUpMaxOrders*1000000 {
		t.Errorf("a stack past the order cap: %d in %d orders", got, orders)
	}
	// The mule's real bank is 400,000 under what it published: the order after it runs dry brings nothing and ends the ask.
	if got, orders := run(11219284, 1000000, 10819284); got != 10819284 || orders != 12 {
		t.Errorf("a stack that shrank: %d in %d orders", got, orders)
	}
}

// The mule floor is kept only for a concrete consumer, and the default is sized by it: the cohort's small coin
// asks (a 1,000-coin kit line, a 500-coin fare, a 3,500-coin blade purse) in the log are all under it ten times over.
func TestDefaultMuleCoinFloorCoversTheSmallAsks(t *testing.T) {
	for name, ask := range map[string]int{"kit coin line": 1000, "fisher fare": 500, "trainer blade purse": 3500} {
		if DefaultMuleCoinFloor < 2*ask {
			t.Errorf("the floor %d does not cover a %s (%d) twice", DefaultMuleCoinFloor, name, ask)
		}
	}
	if DefaultMuleCoinFloor >= 100000 {
		t.Errorf("the floor %d is back at the old 100,000 that no consumer needs", DefaultMuleCoinFloor)
	}
	if !DefaultTopUpWholeStack {
		t.Error("the whole-stack ask is the default")
	}
	s := DefaultSettings()
	if s.MuleCoinFloor != DefaultMuleCoinFloor || !s.TopUpWholeStack {
		t.Errorf("settings defaults %d %v", s.MuleCoinFloor, s.TopUpWholeStack)
	}
	s.MuleCoinFloor = MaxMuleCoinFloor + 1
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "muleCoinFloor") {
		t.Errorf("a floor over the ceiling: %v", err)
	}
	s.MuleCoinFloor = 0
	if err := s.Validate(); err != nil && strings.Contains(err.Error(), "muleCoinFloor") {
		t.Errorf("floor 0 (take every coin) refused: %v", err)
	}
}
