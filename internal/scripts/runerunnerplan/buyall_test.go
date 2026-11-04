package runerunnerplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
)

// LIVE 7 October 2026, spare104: a 100,000-coin pack met a 1,063,659-coin reserve for the runes at the
// stations ahead, and every rune after the first planned to buy nothing.
func TestReserveNeverVetoesTheWholePurse(t *testing.T) {
	runes := []string{"Nature rune", "Death rune", "Air rune", "Body rune"}
	caps := BuyAllCaps(runes)
	seen := map[string]int{"Death rune": 6, "Air rune": 1858, "Body rune": 851}
	reserve := map[string]int{"Nature rune": 0, "Death rune": 1063659, "Air rune": 1063659, "Body rune": 1063659}
	const purse = 99700
	p := PlanVisit(fleetplan.Aubury, seen, purse, runes, caps, reserve)
	if p.Coins <= 0 {
		t.Fatalf("a purse of %d under a huge reserve planned nothing: %+v", purse, p)
	}
	if p.Coins > purse {
		t.Errorf("plan spends %d of a %d purse", p.Coins, purse)
	}
	for _, r := range []string{"Death rune", "Air rune"} {
		found := false
		for _, l := range p.Lines {
			if l.Rune == r && l.Count > 0 {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not planned at all: %+v", r, p.Lines)
		}
	}
	// The reserve still matters: it holds half the purse back from the lower runes.
	if cr := CappedReserve(1063659, purse); cr != purse/2 {
		t.Errorf("CappedReserve %d, want %d", cr, purse/2)
	}
	if CappedReserve(100, purse) != 100 || CappedReserve(100, 0) != 0 || CappedReserve(-5, purse) != 0 {
		t.Error("CappedReserve must pass a small hold and never go negative")
	}
}

func TestBuyAllCapsReachTheShopCeiling(t *testing.T) {
	runes := []string{"Air rune", "Law rune", "Blood rune", "Nature rune"}
	caps := BuyAllCaps(runes)
	sh := fleetplan.WizardsGuild
	for _, r := range runes {
		l, ok := sh.Line(r)
		if !ok {
			t.Fatalf("%s is not at the guild", r)
		}
		if caps.For(r) < CeilingMultiple*l.Cost {
			t.Errorf("%s cap %d is under the shop's ceiling %d", r, caps.For(r), CeilingMultiple*l.Cost)
		}
		// A shelf down to a handful buys out entirely at the ceiling.
		n, _ := BuyableUnderCap(sh, r, 40, caps.For(r))
		if n != 40 {
			t.Errorf("%s: %d of 40 bought under the buy-all cap", r, n)
		}
	}
	// And the derived cap left the shelf: the live "capped ... price=24 cap=12" lines.
	if n, _ := BuyableUnderCap(sh, "Air rune", 561, 12); n >= 561 {
		t.Errorf("the derived cap 12 bought the whole 561-rune air shelf (%d)", n)
	}
}

func TestResolveBuyAllDefault(t *testing.T) {
	s := DefaultSettings()
	r, err := s.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.BuyAll || r.TripCoins != DefaultTripCoins {
		t.Fatalf("defaults buyAll=%v tripCoins=%d", r.BuyAll, r.TripCoins)
	}
	for _, name := range r.RuneList {
		if r.CapTable.For(name) != MaxPriceCap {
			t.Errorf("%s cap %d, want the buy-all %d", name, r.CapTable.For(name), MaxPriceCap)
		}
	}
	// Caps written in the settings win; buyAll off keeps the derived ones.
	w := DefaultSettings()
	w.Runes = []string{"Mind rune"}
	w.PriceCaps = []string{"Mind rune=7"}
	if r, err := w.Resolve(); err != nil || r.CapTable.For("Mind rune") != 7 {
		t.Errorf("written caps lost: %v %v", err, r.CapTable)
	}
	off := DefaultSettings()
	off.BuyAll = false
	if r, err := off.Resolve(); err != nil || r.CapTable.For("Chaos rune") >= MaxPriceCap {
		t.Errorf("buyAll off should keep the derived caps: %v %v", err, r.CapTable)
	}
	if DefaultTripCoins < 1000000 {
		t.Errorf("tripCoins %d is under the circuit's cost", DefaultTripCoins)
	}
}

func TestDrawSizedForTheWholeCircuit(t *testing.T) {
	// Live 7 October: est 1,112,744, bank 316,380: the old cap drew 100,000.
	in := DrawInput{TripCoins: DefaultTripCoins, BankCoins: 316380, CoinReserve: 10000, FareCoins: 300, EstCost: 1112744}
	d := DrawAmount(in)
	if d.Amount != 316380-10000 {
		t.Errorf("the draw took %d of a %d bank (reserve 10000): %+v", d.Amount, in.BankCoins, d)
	}
	if !strings.Contains(d.Why, "bank spares") {
		t.Errorf("the reason should name the bank as the limit: %q", d.Why)
	}
	// A bank that covers the circuit draws it in full, past the old 100,000.
	rich := in
	rich.BankCoins = 5000000
	if d := DrawAmount(rich); d.Amount != 1112744+300 {
		t.Errorf("a rich bank draws %d, want the estimate plus fare", d.Amount)
	}
	// The pack already holding the need draws nothing, and says so.
	full := rich
	full.InPack = 1200000
	d = DrawAmount(full)
	if d.Amount != 0 || !strings.Contains(d.Why, "already holds") {
		t.Errorf("full pack: %+v", d)
	}
	// A tripCoins below the estimate is named.
	capped := rich
	capped.TripCoins = 100000
	if d := DrawAmount(capped); d.Amount != 100300 || !strings.Contains(d.Why, "tripCoins") {
		t.Errorf("tripCoins-bound draw %+v", d)
	}
}

func TestCircuitTopUpSizesTheAsk(t *testing.T) {
	// Off with buyAll off or a top-up that is off.
	if b, a := CircuitTopUp(false, 150000, 300000, 1200000, 10000, 300, 316380); b != 150000 || a != 300000 {
		t.Errorf("buyAll off changed the top-up: %d %d", b, a)
	}
	if b, a := CircuitTopUp(true, 0, 300000, 1200000, 10000, 300, 0); b != 0 || a != 300000 {
		t.Errorf("a top-up that is off must stay off: %d %d", b, a)
	}
	// 316k held against a 1.2M circuit: ask for the shortfall.
	b, a := CircuitTopUp(true, 150000, 300000, 1200000, 10000, 300, 316380)
	if b != 1210300 {
		t.Errorf("below %d, want the circuit's cost plus reserve and fare", b)
	}
	if a != 1210300-316380 {
		t.Errorf("amount %d, want the shortfall %d", a, 1210300-316380)
	}
	// The mule may be asked for no more than MaxTopUpAmount, and the funds are bounded.
	if _, a := CircuitTopUp(true, 150000, 300000, 9000000, 10000, 300, 0); a != MaxTopUpAmount {
		t.Errorf("amount %d, want the %d bound", a, MaxTopUpAmount)
	}
	if b, _ := CircuitTopUp(true, 150000, 300000, 9000000, 10000, 300, 0); b != MaxCircuitFunds {
		t.Errorf("below %d, want %d", b, MaxCircuitFunds)
	}
	// Plenty held: no ask.
	if v := MuleTopUp(TopUpInput{Below: 1210300, Amount: 300000, BankCoins: 1500000, MuleWorldKnown: true, MuleHeard: true}); v.Ask != 0 {
		t.Errorf("a bank above the circuit's cost asked: %+v", v)
	}
}

func TestLeftOnShelfSaysWhy(t *testing.T) {
	sh := fleetplan.WizardsGuild
	runes := []string{"Nature rune", "Chaos rune", "Air rune", "Body rune", "Law rune"}
	caps := Caps{"Nature rune": MaxPriceCap, "Chaos rune": MaxPriceCap, "Air rune": 12, "Body rune": MaxPriceCap, "Law rune": MaxPriceCap}
	seen := map[string]int{"Nature rune": 1000, "Chaos rune": 1000, "Air rune": 561, "Body rune": 3000, "Law rune": 34}
	plan := fleetplan.PurchasePlan{Lines: []fleetplan.PlannedBuy{
		{Rune: "Nature rune", Count: 1000}, {Rune: "Chaos rune", Count: 700}, {Rune: "Body rune", Count: 3000},
	}}
	bought := map[string]int{"Nature rune": 1000, "Chaos rune": 700, "Body rune": 2900}
	got := LeftOnShelf(LeftInput{Shop: sh, Runes: runes, Caps: caps, Seen: seen, Bought: bought, Plan: plan,
		Spendable: 150000, Reserve: map[string]int{"Chaos rune": 1000000}, VisitReason: "the pack filled"})
	by := map[string]ShelfLeft{}
	for _, l := range got {
		by[l.Rune] = l
	}
	if _, ok := by["Nature rune"]; ok {
		t.Error("nothing of Nature was left")
	}
	if l := by["Chaos rune"]; l.Left != 300 || !l.ForCoins || !strings.Contains(l.Reason, "coins") || !strings.Contains(l.Reason, "75000 held") {
		t.Errorf("chaos: %+v", l)
	}
	if l := by["Air rune"]; l.Left != 561 || !strings.Contains(l.Reason, "over cap 12") {
		t.Errorf("air: %+v", l)
	}
	if l := by["Body rune"]; l.Left != 100 || !strings.Contains(l.Reason, "bought 2900 of 3000 planned") || !strings.Contains(l.Reason, "the pack filled") {
		t.Errorf("body: %+v", l)
	}
	if l := by["Law rune"]; l.Left != 34 || !strings.Contains(l.Reason, "coins") {
		t.Errorf("law: %+v", l)
	}
}

// LIVE 12 October (spare104 at the Wizards' Guild): "coins: 145056 spendable" was the visit's opening
// purse, printed against a Law rune whose turn came after the elementals had taken it. The line now
// says what was left when the rune's turn came, and marks the shelf as left for coins.
func TestLeftOnShelfNamesWhatWasLeftWhenItsTurnCame(t *testing.T) {
	sh := fleetplan.WizardsGuild
	runes := []string{"Mind rune", "Air rune", "Law rune"}
	caps := BuyAllCaps(runes)
	seen := map[string]int{"Mind rune": 1000, "Air rune": 1000, "Law rune": 34}
	// A purse that pays for the minds and the air and has a little left: Law's turn comes with
	// less than one unit's price.
	purse := sh.CostToBuy("Mind rune", 1000, 1000) + sh.CostToBuy("Air rune", 1000, 1000) + 10
	plan := PlanVisit(sh, seen, purse, runes, caps, nil)
	bought := map[string]int{}
	for _, l := range plan.Lines {
		bought[l.Rune] = l.Count
	}
	if bought["Mind rune"] != 1000 || bought["Law rune"] != 0 {
		t.Fatalf("setup: the elementals should take the purse before Law: %v (%d coins)", bought, plan.Coins)
	}
	left := LeftOnShelf(LeftInput{Shop: sh, Runes: runes, Caps: caps, Seen: seen, Bought: bought, Plan: plan, Spendable: purse})
	var law ShelfLeft
	for _, l := range left {
		if l.Rune == "Law rune" {
			law = l
		}
	}
	spent := plan.Coins
	if !law.ForCoins || law.Left != 34 || law.Cost <= 0 {
		t.Fatalf("law: %+v", law)
	}
	if want := fmt.Sprintf("coins: %d left of the visit's %d spendable when it came up", purse-spent, purse); !strings.Contains(law.Reason, want) {
		t.Errorf("law reason %q, want %q", law.Reason, want)
	}
	if law.Cost != sh.CostToBuy("Law rune", 34, 34) {
		t.Errorf("the rest of the shelf costs %d, not %d", sh.CostToBuy("Law rune", 34, 34), law.Cost)
	}
	// A rune left over its cap, or short of its plan, is not a coin shortfall.
	capped := LeftOnShelf(LeftInput{Shop: sh, Runes: []string{"Air rune"}, Caps: Caps{"Air rune": 12}, Seen: map[string]int{"Air rune": 561}, Spendable: purse})
	if len(capped) != 1 || capped[0].ForCoins || capped[0].Cost != 0 {
		t.Errorf("capped: %+v", capped)
	}
}

func TestFundableNeedDoesNotChaseCoinsTheBankLacks(t *testing.T) {
	// The live 7 October case: 100,000 in the pack, a 316,380 bank (10,000 reserve), a 1.1M circuit.
	if got := FundableNeed(100000, 1112744, true, 316380, 10000); got != 100000+306380 {
		t.Errorf("fundable need %d", got)
	}
	// A bank at its reserve funds nothing more: the stop at a shop bank is not due.
	if got := FundableNeed(100000, 1112744, true, 10000, 10000); got != 100000 || TopUpDue(100000, got, 300) {
		t.Errorf("a drained bank still sent the runner to it: %d", got)
	}
	// A need under the supply is unchanged, and an unread bank is trusted.
	if FundableNeed(1000, 5000, true, 500000, 10000) != 5000 || FundableNeed(0, 900000, false, 0, 10000) != 900000 {
		t.Error("FundableNeed changed a need the supply covers")
	}
}
