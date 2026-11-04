package runerunnerplan

import (
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
)

var defaultCaps = Caps{"Mind rune": 18, "Chaos rune": 90}

// Test 3: the capped buy-out, priced unit by unit exactly as the shop does.
func TestBuyableUnderCap(t *testing.T) {
	for _, c := range []struct {
		name         string
		shop         fleetplan.RuneShop
		rune         string
		stock, cap   int
		wantN, wantC int
	}{
		{"Aubury mind full, cap 18", fleetplan.Aubury, "Mind rune", 1000, 18, 1000, 13995},
		{"Aubury mind full, cap 6", fleetplan.Aubury, "Mind rune", 1000, 6, 134, 603},
		{"Aubury mind full, cap 9", fleetplan.Aubury, "Mind rune", 1000, 9, 234, 1404},
		{"Betty mind full, cap 18", Betty, "Mind rune", 1000, 18, 1000, 13995},
		{"Lundail mind full, cap 18", fleetplan.Lundail, "Mind rune", 140, 18, 140, 1227},
		{"Aubury chaos full, cap 90", fleetplan.Aubury, "Chaos rune", 1000, 90, 1000, 70975},
		{"Lundail chaos full, cap 90", fleetplan.Lundail, "Chaos rune", 250, 90, 250, 16133},
		// Air near its base price: 4 until 25 below base, 5 until 50 below (delta 10); Lundail's
		// delta of 30 gets there three times as fast from a tenth of the shelf.
		{"Aubury air full, cap 4", fleetplan.Aubury, "Air rune", 2000, 4, 25, 100},
		{"Aubury air full, cap 5 (the default)", fleetplan.Aubury, "Air rune", 2000, 5, 50, 225},
		{"Betty air full, cap 5", Betty, "Air rune", 1000, 5, 50, 225},
		{"Lundail air full, cap 4", fleetplan.Lundail, "Air rune", 200, 4, 9, 36},
		{"Lundail air full, cap 5", fleetplan.Lundail, "Air rune", 200, 5, 17, 76},
		{"Aubury air full, cap 24 (the ceiling)", fleetplan.Aubury, "Air rune", 2000, 24, 2000, buyAllCost(fleetplan.Aubury, "Air rune", 2000)},
		{"cap below base cost", fleetplan.Aubury, "Mind rune", 1000, 2, 0, 0},
		{"empty shelf", fleetplan.Aubury, "Mind rune", 0, 18, 0, 0},
		{"no cap", fleetplan.Aubury, "Mind rune", 1000, 0, 0, 0},
	} {
		n, cost := BuyableUnderCap(c.shop, c.rune, c.stock, c.cap)
		if n != c.wantN || cost != c.wantC {
			t.Errorf("%s: (%d, %d), want (%d, %d)", c.name, n, cost, c.wantN, c.wantC)
		}
	}
	if n, _ := BuyableUnderCap(fleetplan.Lundail, "Chaos rune", 250, 45); n != 69 {
		t.Errorf("Lundail chaos at 250 under a 45 cap: %d units, want 69", n)
	}
	// The cap is exactly AffordableCapped with unlimited coins.
	n1, c1 := BuyableUnderCap(fleetplan.Aubury, "Chaos rune", 800, 45)
	n2, c2 := fleetplan.Aubury.AffordableCapped("Chaos rune", 800, 1<<31-1, 800, 45)
	if n1 != n2 || c1 != c2 {
		t.Errorf("BuyableUnderCap (%d,%d) != AffordableCapped (%d,%d)", n1, c1, n2, c2)
	}
}

// Test 4: minds are funded before chaos, the plan never exceeds what it may spend, and runes the
// shop does not sell drop out.
func TestPlanVisitPriority(t *testing.T) {
	seen := map[string]int{"Mind rune": 1000, "Chaos rune": 1000, "Air rune": 2000}
	runes := []string{"Mind rune", "Chaos rune", "Nature rune"}
	caps := Caps{"Mind rune": 18, "Chaos rune": 90, "Nature rune": 100}

	// 20,000 coins: every mind (13,995) and chaos with the rest.
	p := PlanVisit(fleetplan.Aubury, seen, 20000, runes, caps, nil)
	if len(p.Lines) != 2 || p.Lines[0].Rune != "Mind rune" || p.Lines[0].Count != 1000 {
		t.Fatalf("plan %+v: minds must be the first line, bought out", p)
	}
	if p.Lines[1].Rune != "Chaos rune" || p.Lines[1].Cost > 20000-13995 {
		t.Errorf("chaos line %+v overspends what the minds left", p.Lines[1])
	}
	sum := 0
	for _, l := range p.Lines {
		sum += l.Cost
		if l.Rune == "Nature rune" {
			t.Error("Aubury does not sell nature runes")
		}
	}
	if p.Coins != sum || p.Coins > 20000 {
		t.Errorf("plan coins %d, lines sum %d, spendable 20000", p.Coins, sum)
	}

	// 5,000 coins: all of it on minds, none left for chaos.
	p = PlanVisit(fleetplan.Aubury, seen, 5000, runes, caps, nil)
	if len(p.Lines) != 1 || p.Lines[0].Rune != "Mind rune" || p.Coins > 5000 {
		t.Errorf("a short purse must go to minds only: %+v", p)
	}

	// The spendable the caller passes is the pack less the fare: the plan never touches it.
	const inPack, fare = 1000, 300
	p = PlanVisit(fleetplan.Aubury, seen, inPack-fare, runes, caps, nil)
	if p.Coins > inPack-fare {
		t.Errorf("plan spends %d of %d with a %d fare held back", p.Coins, inPack, fare)
	}

	// An empty shelf and an over-cap shelf plan nothing.
	if p := PlanVisit(fleetplan.Aubury, map[string]int{"Mind rune": 0}, 50000, runes, caps, nil); len(p.Lines) != 0 {
		t.Errorf("empty shelf planned %+v", p)
	}
	if p := PlanVisit(fleetplan.Aubury, map[string]int{"Chaos rune": 10}, 50000, []string{"Chaos rune"}, Caps{"Chaos rune": 45}, nil); len(p.Lines) != 0 {
		t.Errorf("chaos at stock 10 costs 90 each, over a 45 cap: planned %+v", p)
	}
}

// Test 5: the reserve keeps chaos at Betty on the mule's world from spending what the minds at
// Aubury on the other world are expected to need.
func TestReserveAheadKeepsLaterMindsFunded(t *testing.T) {
	runes := []string{"Mind rune", "Chaos rune"}
	ahead := []StationEstimate{{Shop: fleetplan.Aubury, Stock: map[string]int{"Mind rune": 800, "Chaos rune": 0}}}
	_, mindsAhead := BuyableUnderCap(fleetplan.Aubury, "Mind rune", 800, 18)
	reserve := ReserveAhead(ahead, runes, defaultCaps)
	if reserve["Mind rune"] != 0 {
		t.Errorf("the top rune reserves nothing, got %d", reserve["Mind rune"])
	}
	if reserve["Chaos rune"] != mindsAhead {
		t.Fatalf("chaos reserve %d, want the 800 minds' %d", reserve["Chaos rune"], mindsAhead)
	}
	// Betty has already been emptied of minds; a 40,000-coin purse would go all on chaos
	// without the reserve.
	seen := map[string]int{"Mind rune": 0, "Chaos rune": 1000}
	const purse = 40000
	without := PlanVisit(Betty, seen, purse, runes, defaultCaps, nil)
	with := PlanVisit(Betty, seen, purse, runes, defaultCaps, reserve)
	if without.Coins <= purse-mindsAhead {
		t.Fatalf("the test needs a purse chaos would drain: without the reserve it spent %d", without.Coins)
	}
	if with.Coins > purse-mindsAhead {
		t.Errorf("with the reserve chaos spent %d, leaving %d for %d coins of minds ahead", with.Coins, purse-with.Coins, mindsAhead)
	}
	if with.Coins == 0 {
		t.Error("the reserve should trim chaos, not stop it")
	}
}

// Test 6: the draw.
func TestDrawAmount(t *testing.T) {
	base := DrawInput{TripCoins: 100000, BankCoins: 500000, CoinReserve: 10000, FareCoins: 300, EstCost: 60000}
	if d := DrawAmount(base); d.NoCoins || d.Amount != 60300 || d.Need != 60300 {
		t.Errorf("estimate-bound draw %+v, want 60300", d)
	}
	trip := base
	trip.EstCost = 250000
	if d := DrawAmount(trip); d.Amount != 100300 {
		t.Errorf("tripCoins-bound draw %+v, want 100300", d)
	}
	bank := base
	bank.BankCoins, bank.EstCost = 40000, 250000
	if d := DrawAmount(bank); d.Amount != 30000 {
		t.Errorf("bank-bound draw %+v, want the 30000 above the reserve", d)
	}
	held := base
	held.InPack = 20000
	if d := DrawAmount(held); d.Amount != 40300 {
		t.Errorf("coins in the pack must be subtracted: %+v", d)
	}
	full := base
	full.InPack = 90000
	if d := DrawAmount(full); d.Amount != 0 {
		t.Errorf("a pack already holding the need draws nothing: %+v", d)
	}
	for _, bc := range []int{0, 10000, 10000 + 300 + NoCoinsMargin - 1} {
		poor := base
		poor.BankCoins = bc
		if d := DrawAmount(poor); !d.NoCoins || d.Amount != 0 {
			t.Errorf("bank %d with a 10000 reserve: %+v, want NO COINS", bc, d)
		}
	}
}

func TestTopUpDue(t *testing.T) {
	if TopUpDue(50000, 40000, 300) {
		t.Error("a pack that covers the need does not stop at a bank")
	}
	if !TopUpDue(0, 40000, 300) {
		t.Error("a respawn with an empty pack tops up before the shop")
	}
	if TopUpDue(39500, 40000, 300) {
		t.Error("800 short is not worth a bank stop")
	}
}

// Test 18: the price check's ±10% boundary.
func TestPriceMismatchBoundary(t *testing.T) {
	for _, c := range []struct {
		predicted, spent int
		want             bool
	}{
		{1000, 1100, false},
		{1000, 900, false},
		{1000, 1101, true},
		{1000, 899, true},
		{0, 500, false},
	} {
		if got := PriceMismatch(c.predicted, c.spent); got != c.want {
			t.Errorf("PriceMismatch(%d, %d) = %v, want %v", c.predicted, c.spent, got, c.want)
		}
	}
	// The prediction prices what actually landed from the stock the line was planned at.
	plan := PlanVisit(Betty, map[string]int{"Mind rune": 1000}, 100000, []string{"Mind rune"}, defaultCaps, nil)
	if got := PredictedSpend(Betty, plan, map[string]int{"Mind rune": 1000}); got != 13995 {
		t.Errorf("predicted %d, want 13995", got)
	}
	if got := PredictedSpend(Betty, plan, map[string]int{"Mind rune": 10}); got != Betty.CostToBuy("Mind rune", 1000, 10) {
		t.Errorf("a partial buy is priced from the top of the shelf: %d", got)
	}
	// Production's older Betty (1300 at delta 30) would not match the model.
	old := Betty
	old.SellMultiplier, old.Delta = 1300, 30
	spent := old.CostToBuy("Mind rune", 1000, 500)
	if !PriceMismatch(PredictedSpend(Betty, plan, map[string]int{"Mind rune": 500}), spent) {
		t.Error("a 1300/30 shop must be caught by the price check")
	}
}

func TestWorthAndEstCost(t *testing.T) {
	full := map[string]int{"Mind rune": 1000, "Chaos rune": 1000}
	if w := Worth(fleetplan.Aubury, full, DefaultRunes(), defaultCaps); w != 2000 {
		t.Errorf("full Aubury worth %d, want 2000", w)
	}
	if c := EstCost([]StationEstimate{{Shop: fleetplan.Aubury, Stock: full}}, DefaultRunes(), defaultCaps); c != 13995+70975 {
		t.Errorf("EstCost %d", c)
	}
	if w := Worth(fleetplan.Lundail, map[string]int{"Mind rune": 140, "Chaos rune": 250}, DefaultRunes(), defaultCaps); w != 390 {
		t.Errorf("full Lundail worth %d, want 390", w)
	}
}
