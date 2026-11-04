package consolidateplan

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

func windBolt(t *testing.T) fleetplan.Spell {
	t.Helper()
	sp, ok := fleetplan.SpellByName("Wind Bolt")
	if !ok {
		t.Fatal("no Wind Bolt in the spell ladder")
	}
	return sp
}

func TestDefaultsAreOnWithTheLootHandOff(t *testing.T) {
	d := Defaults()
	if !d.Enabled || !d.Loot {
		t.Fatal("the loot hand-off is on by default (a user request)")
	}
	if d.LootClues {
		t.Fatal("clue scrolls stay by default: each clue step is its own obj and bank row")
	}
	if d.MinLootValue != 20_000 || d.MinLootRows != 15 || d.MaxLoads != 3 || d.MuleBankSlots != 288 || d.MuleBankReserve != 48 {
		t.Fatalf("unexpected loot defaults: %+v", d)
	}
	if d.Every != 24*time.Hour || d.MaxQueue != 2 || d.CoinsReserve != 20_000 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if !kept(d.KeepItems, "mind rune") {
		t.Fatal("Mind rune must stay out of a deposit by default (the mind share hands those down)")
	}
}

func TestNormalisedClampsNonsense(t *testing.T) {
	s := Settings{Every: time.Minute, CoinsReserve: -5, ChaosPrice: -1, EarmarkCap: -1, MaxQueue: -3}.Normalised()
	if s.Every != 24*time.Hour || s.CoinsReserve != 0 || s.ChaosPrice != 0 || s.EarmarkCap != 0 || s.MaxQueue != 0 {
		t.Fatalf("not clamped: %+v", s)
	}
	if s.MinCoins < 1 || s.MinRunes < 1 || s.MinFood < 1 {
		t.Fatalf("minimums must be at least 1: %+v", s)
	}
}

func TestKeepEarmarksChaosOnlyForAMarketBuyer(t *testing.T) {
	s := Defaults()
	sp := windBolt(t)
	air := fleetplan.StaffProvides("Staff of air")
	in := KeepInput{CastsLeft: 1000, Spell: sp, Staff: air, ChaosHeld: 400, FoodKeep: 16}

	k := KeepFor(s, in)
	if k.Runes["Chaos rune"] != 1000 {
		t.Fatalf("Wind Bolt keeps 1 chaos a cast: got %d", k.Runes["Chaos rune"])
	}
	if _, ok := k.Runes["Air rune"]; ok {
		t.Fatal("a Staff of air pays the air: no air may be kept")
	}
	if k.Coins != s.CoinsReserve || k.Food != 16 {
		t.Fatalf("a non-buyer keeps the reserve and its food: %+v", k)
	}

	in.MarketBuyer = true
	k = KeepFor(s, in)
	if want := s.CoinsReserve + 600*s.ChaosPrice; k.Coins != want {
		t.Fatalf("buyer keeps reserve + (1000-400) chaos at %d: got %d want %d", s.ChaosPrice, k.Coins, want)
	}

	in.CastsLeft = 100_000 // far from the target: the earmark caps
	k = KeepFor(s, in)
	if k.Coins != s.CoinsReserve+s.EarmarkCap {
		t.Fatalf("earmark must cap at %d: got %d", s.EarmarkCap, k.Coins)
	}
}

func TestChaosEarmark(t *testing.T) {
	cases := []struct{ short, price, cap, want int }{
		{0, 95, 0, 0}, {-5, 95, 0, 0}, {10, 0, 0, 0}, {10, 95, 0, 950}, {10, 95, 500, 500},
	}
	for _, c := range cases {
		if got := ChaosEarmark(c.short, c.price, c.cap); got != c.want {
			t.Errorf("ChaosEarmark(%d,%d,%d) = %d, want %d", c.short, c.price, c.cap, got, c.want)
		}
	}
}

func TestPlanDepositSendsOnlyTheSurplus(t *testing.T) {
	s := Defaults()
	k := Keep{Coins: 100_000, Runes: map[string]int{"Chaos rune": 1000}, Food: 16}
	h := Holdings{
		Coins: 400_000,
		Runes: map[string]int{"Chaos rune": 1500, "Death rune": 431, "Mind rune": 300, "Air rune": 70},
		Food:  map[string]int{"Lobster": 20, "Cake": 10},
	}
	p := PlanDeposit(s, h, k, 28)
	if !p.Walk || p.Why != "coins over the keep" {
		t.Fatalf("300k over the keep must walk: %+v", p)
	}
	if p.Coins != 300_000 {
		t.Fatalf("coins: got %d want 300000", p.Coins)
	}
	want := map[string]int{"Chaos rune": 500, "Death rune": 431, "Air rune": 70}
	if !reflect.DeepEqual(p.Runes, want) {
		t.Fatalf("runes: got %v want %v (Mind rune is kept by default)", p.Runes, want)
	}
	if p.FoodUnits() != 14 || p.Food["Lobster"] != 14 {
		t.Fatalf("food: 30 held, 16 kept, the most plentiful first: got %v", p.Food)
	}
	if got := p.Items(); !reflect.DeepEqual(got, []string{"Coins", "Air rune", "Chaos rune", "Death rune", "Lobster"}) {
		t.Fatalf("items order: %v", got)
	}
	if p.Count("chaos rune") != 500 || p.Count("COINS") != 300_000 || p.Count("Lobster") != 14 || p.Count("Shark") != 0 {
		t.Fatal("Count must match names ignoring case")
	}
}

func TestPlanDepositFoodIsCappedBySlots(t *testing.T) {
	p := PlanDeposit(Defaults(), Holdings{Food: map[string]int{"Lobster": 100}}, Keep{Food: 10}, 12)
	if p.FoodUnits() != 12 {
		t.Fatalf("food must fit the free slots: got %d", p.FoodUnits())
	}
	if p.Walk {
		t.Fatal("12 food is under the 20-bite minimum: no walk")
	}
}

func TestPlanDepositNeedsOneKindOverItsMinimum(t *testing.T) {
	s := Defaults()
	k := Keep{Coins: 50_000}
	p := PlanDeposit(s, Holdings{Coins: 140_000, Runes: map[string]int{"Death rune": 100}}, k, 0)
	if p.Walk {
		t.Fatalf("90k coins and 100 runes clear no minimum: %+v", p)
	}
	p = PlanDeposit(s, Holdings{Coins: 140_000, Runes: map[string]int{"Death rune": 600}}, k, 0)
	if !p.Walk || p.Why != "runes over the keep" || p.Coins != 90_000 {
		t.Fatalf("600 spare runes justify the walk, and the coins ride along: %+v", p)
	}
}

func TestPlanDepositHonoursKeepItems(t *testing.T) {
	s := Defaults()
	s.KeepItems = []string{"Coins", "Death rune"}
	p := PlanDeposit(s, Holdings{Coins: 10_000_000, Runes: map[string]int{"Death rune": 9000}}, Keep{}, 0)
	if p.Coins != 0 || p.Runes["Death rune"] != 0 || p.Walk {
		t.Fatalf("kept names never cross: %+v", p)
	}
}

func goodDue() DueInput {
	return DueInput{Enabled: true, TimerDue: true, Stage: fleetplan.StageSurvivalCombat, SafePlace: true,
		HP: 40, MaxHP: 40, MuleHeard: true, SameWorld: true, Queue: 1, QueueKnown: true}
}

func TestDueGates(t *testing.T) {
	s := Defaults()
	if ok, why := Due(s, goodDue()); !ok {
		t.Fatalf("a good moment must be due: %s", why)
	}
	cases := map[string]func(*DueInput){
		"off":         func(d *DueInput) { d.Enabled = false },
		"timer":       func(d *DueInput) { d.TimerDue = false },
		"stage 3":     func(d *DueInput) { d.Stage = fleetplan.StageMagicPrep },
		"stage 6":     func(d *DueInput) { d.Stage = fleetplan.StageKQEntry },
		"place":       func(d *DueInput) { d.SafePlace = false },
		"trip":        func(d *DueInput) { d.TripActive = true },
		"combat":      func(d *DueInput) { d.InCombat = true },
		"hurt":        func(d *DueInput) { d.Hurt = true },
		"low hp":      func(d *DueInput) { d.HP = 20 },
		"no mule":     func(d *DueInput) { d.MuleHeard = false },
		"other world": func(d *DueInput) { d.SameWorld = false },
		"queue":       func(d *DueInput) { d.Queue = 3 },
	}
	for name, mut := range cases {
		in := goodDue()
		mut(&in)
		if ok, why := Due(s, in); ok || why == "" {
			t.Errorf("%s: must not be due (ok=%v why=%q)", name, ok, why)
		}
	}
	in := goodDue()
	in.QueueKnown, in.Queue = false, 50
	if ok, _ := Due(s, in); !ok {
		t.Fatal("an unread queue does not block (the walk re-checks at the post)")
	}
	in = goodDue()
	in.SameWorld, in.Errand = false, true
	if ok, why := Due(s, in); !ok {
		t.Fatalf("a world errand carries the trip to the mule's world: %s", why)
	}
}

func TestTopUpHysteresisAndFairShare(t *testing.T) {
	// Holding half the keep or more: no ask, so a deposit down to the keep never triggers one.
	if n := TopUp(TopUpInput{Have: 500, Keep: 1000, MuleStock: 1_000_000}); n != 0 {
		t.Fatalf("at half the keep no top-up: %d", n)
	}
	// Under half: back up to the keep, when the mule has plenty.
	if n := TopUp(TopUpInput{Have: 100, Keep: 1000, MuleStock: 1_000_000, Cohort: 100}); n != 900 {
		t.Fatalf("top-up to the keep: got %d want 900", n)
	}
	// A thin stock is split evenly across the cohort, less the fleet reserve.
	if n := TopUp(TopUpInput{Have: 0, Keep: 1000, MuleStock: 10_000, Cohort: 100}); n != 90 {
		t.Fatalf("fair share of 10,000 over 100 less a tenth: got %d want 90", n)
	}
	// The per-order ceiling holds.
	if n := TopUp(TopUpInput{Have: 0, Keep: 1000, MuleStock: 1_000_000, Cohort: 1, PerOrder: 250}); n != 250 {
		t.Fatalf("per-order cap: got %d", n)
	}
	// No keep, or an empty mule: nothing.
	if TopUp(TopUpInput{Have: 0, Keep: 0, MuleStock: 100}) != 0 || TopUp(TopUpInput{Have: 0, Keep: 100, MuleStock: 0}) != 0 {
		t.Fatal("no keep or no stock asks nothing")
	}
}

func TestDepositAndTopUpNeverPingPong(t *testing.T) {
	s := Defaults()
	k := Keep{Coins: 300_000}
	for have := 0; have <= 2_000_000; have += 10_000 {
		p := PlanDeposit(s, Holdings{Coins: have}, k, 0)
		ask := TopUp(TopUpInput{Have: have, Keep: k.Coins, MuleStock: 100_000_000, Cohort: 1})
		if p.Walk && ask > 0 {
			t.Fatalf("at %d coins the account would both deposit and ask", have)
		}
		after := have - p.Coins
		if p.Walk && TopUp(TopUpInput{Have: after, Keep: k.Coins, MuleStock: 100_000_000, Cohort: 1}) > 0 {
			t.Fatalf("a deposit from %d leaves %d, which would ask straight back", have, after)
		}
	}
}

func TestChaosBudgetLiveNumbers(t *testing.T) {
	// 2 Oct 2026: Seers Market 317,554 chaos at 95; the 295 ScarabSwarm checkpoints held 106,019,861
	// coins; the fleet bought ~60,000 a day 30 Sep - 1 Oct.
	b := Budget(317_554, 95, 106_019_861, 50_000, 295, 60_000)
	if b.Cost != 30_167_630 {
		t.Fatalf("cost: %d", b.Cost)
	}
	if !b.Covered || b.Shortfall != 0 {
		t.Fatalf("the fleet covers the shelf: %+v", b)
	}
	if b.Days < 5 || b.Days > 6 {
		t.Fatalf("days at 60k/day: %.2f", b.Days)
	}
	short := Budget(1_000_000, 100, 50_000_000, 0, 0, 0)
	if short.Covered || short.Shortfall != 50_000_000 || short.Days != 0 {
		t.Fatalf("shortfall case: %+v", short)
	}
}
