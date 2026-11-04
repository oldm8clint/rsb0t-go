package consolidateplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
)

func TestKitAskHandOverGates(t *testing.T) {
	cases := []struct {
		name string
		in   KitAskInput
		edit func(*Settings)
		want bool
	}{
		{"a Stage 5 ask for a kit", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Lobster"}}, nil, true},
		{"the food ask", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskFood, Kit: []string{"Cake"}}, nil, true},
		{"Stage 3 hands over too (every stage, 7 Oct 2026)", KitAskInput{Stage: fleetplan.StageMagicPrep, Purpose: fleetplan.MuleAskStage3, Kit: []string{"Chaos rune"}}, nil, true},
		{"Stage 2", KitAskInput{Stage: fleetplan.StageFoodAndFunds, Purpose: fleetplan.MuleAskStage2, Kit: []string{"Cake"}}, nil, true},
		{"Stage 6", KitAskInput{Stage: fleetplan.StageKQEntry, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Rope"}}, nil, true},
		{"the tutorial stage has nothing", KitAskInput{Stage: fleetplan.StageTutorialFunds, Purpose: fleetplan.MuleAskStage2, Kit: []string{"Cake"}}, nil, false},
		{"a hungry Stage 3 ask is not delayed", KitAskInput{Stage: fleetplan.StageMagicPrep, Purpose: fleetplan.MuleAskFoodHungry, Kit: []string{"Cake"}}, nil, false},
		{"the Stage 2 starter kit", KitAskInput{Stage: fleetplan.StageFoodAndFunds, Purpose: fleetplan.MuleAskStarter, Kit: []string{"Rope"}}, nil, false},
		{"a hungry ask is not delayed", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskFoodHungry, Kit: []string{"Cake"}}, nil, false},
		{"the starter kit", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStarter, Kit: []string{"Rope"}}, nil, false},
		{"nothing asked", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5}, nil, false},
		{"consolidateOnKitAsk off", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Lobster"}}, func(s *Settings) { s.OnKitAsk = false }, false},
		{"consolidateToMule off", KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Lobster"}}, func(s *Settings) { s.Enabled = false }, false},
	}
	for _, c := range cases {
		s := Defaults()
		if c.edit != nil {
			c.edit(&s)
		}
		_, got, why := KitAskHandOver(s, c.in)
		if got != c.want || (!got && why == "") {
			t.Errorf("%s: %v (%q), want %v", c.name, got, why, c.want)
		}
	}
}

// What the ask names stays, so nothing is handed over and asked back in the same visit; the minimums
// are the trip's, capped lower.
func TestKitAskHandOverKeepsTheKitAndLowersTheMinimums(t *testing.T) {
	s, ok, _ := KitAskHandOver(Defaults(), KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5,
		Kit: []string{"Nature rune", "Lobster", " Fire rune "}})
	if !ok {
		t.Fatal("no hand-over")
	}
	if s.MinCoins != KitAskMinCoins || s.MinRunes != KitAskMinRunes || s.MinLootRows != KitAskMinLootRows {
		t.Fatalf("minimums %+v", s)
	}
	// A setting already lower than the cap stays.
	lo := Defaults()
	lo.MinCoins = 5_000
	if s2, _, _ := KitAskHandOver(lo, KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Lobster"}}); s2.MinCoins != 5_000 {
		t.Fatalf("a lower minimum was raised: %d", s2.MinCoins)
	}
	hold := Holdings{Coins: 300_000, Runes: map[string]int{"Nature rune": 800, "Chaos rune": 700}, Food: map[string]int{"Lobster": 60}}
	keep := Keep{Coins: 50_000, Food: 10}
	p := PlanDeposit(s, hold, keep, 100)
	if !p.Walk || p.Coins != 250_000 {
		t.Fatalf("coins over the keep cross: %+v", p)
	}
	if _, has := p.Runes["Nature rune"]; has {
		t.Fatalf("the asked Nature rune must not cross: %+v", p.Runes)
	}
	if p.Runes["Chaos rune"] != 700 {
		t.Fatalf("an unasked spare rune crosses: %+v", p.Runes)
	}
	if len(p.Food) != 0 {
		t.Fatalf("the asked food must not cross: %+v", p.Food)
	}
	// Loot: an asked item is kit; another is loot.
	in := stage5Kit()
	in.Keep = s.KeepItems
	if k, _ := Classify("Nature rune", in); k != KindRune {
		t.Fatalf("a rune is a rune row (the keep rides PlanDeposit): %v", k)
	}
	if k, _ := Classify("Lobster", in); k != KindKit {
		t.Fatalf("an asked food is kept: %v", k)
	}
}

// The small surplus a trip would not walk for is still worth a trade when the ask is going anyway.
func TestKitAskHandOverAcceptsASmallerSurplus(t *testing.T) {
	trip := Defaults()
	ask, _, _ := KitAskHandOver(trip, KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Lobster"}})
	in := HandOffInput{
		Holdings: Holdings{Coins: 90_000},
		Keep:     Keep{Coins: 50_000},
		Bank: []BankRow{
			{Name: "Coins", ID: 995, Count: 90_000, Value: 1},
			{Name: "Uncut ruby", ID: 1619, Count: 4, Value: 1500},
		},
		Kit:       stage5Kit(),
		MuleKinds: map[string]bool{"uncut ruby": true},
		MuleKnown: true,
	}
	if h := PlanHandOff(trip, in); h.Walk {
		t.Fatalf("the trip's minimums should refuse 6,000 of loot and 40,000 coins: %+v", h)
	}
	h := PlanHandOff(ask, in)
	if !h.Walk || len(h.Loot) != 1 || h.Deposit.Coins != 40_000 {
		t.Fatalf("the kit ask's hand-over: %+v", h)
	}
}

// Rub jewellery stays: two of it across every charge; a jeweller's stock beyond that is loot.
func TestPlanLootKeepsTheRubJewellery(t *testing.T) {
	rows := []BankRow{
		{Name: "Games necklace(8)", ID: 3853, Count: 1, Value: 500},
		{Name: "Ring of dueling(8)", ID: 2552, Count: 1, Value: 500},
		{Name: "Games necklace(4)", ID: 3857, Count: 5, Value: 400},
		{Name: "Amulet of glory", ID: 1704, Count: 1, Value: 900},
	}
	lines, kept := PlanLoot(rows, stage5Kit())
	got := map[string]int{}
	for _, l := range lines {
		got[l.Name] = l.Count
	}
	if got["Games necklace(8)"] != 0 || got["Ring of dueling(8)"] != 0 {
		t.Fatalf("the first two rub pieces stay: %v", got)
	}
	if got["Games necklace(4)"] != 5 || got["Amulet of glory"] != 1 {
		t.Fatalf("the rest is loot: %v", got)
	}
	if !strings.Contains(kept["Games necklace(8)"], "jewellery") {
		t.Fatalf("kept: %v", kept)
	}
}

// Stage 2 and 3 accounts spend coins on fares, shops and market chaos: they keep at least KitAskEarlyReserve
// whatever consolidateCoinsReserve says, where Stage 5 keeps exactly the setting.
func TestKitAskHandOverKeepsAPurseForTheEarlyStages(t *testing.T) {
	lo := Defaults()
	lo.CoinsReserve = 0
	for _, stage := range []int{fleetplan.StageFoodAndFunds, fleetplan.StageMagicPrep} {
		s, ok, _ := KitAskHandOver(lo, KitAskInput{Stage: stage, Purpose: fleetplan.MuleAskStage3, Kit: []string{"Cake"}})
		if !ok || s.CoinsReserve != KitAskEarlyReserve {
			t.Fatalf("stage %d: reserve %d (ok %v), want %d", stage, s.CoinsReserve, ok, KitAskEarlyReserve)
		}
		keep := KeepFor(s, KeepInput{FoodKeep: 8})
		p := PlanDeposit(s, Holdings{Coins: 30_000}, keep, 100)
		if p.Coins != 25_000 || !p.Walk {
			t.Fatalf("stage %d: 30,000 coins hand over 25,000 and keep 5,000: %+v", stage, p)
		}
	}
	s5, _, _ := KitAskHandOver(lo, KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Kit: []string{"Cake"}})
	if s5.CoinsReserve != 0 {
		t.Fatalf("Stage 5 keeps the setting as it always did: %d", s5.CoinsReserve)
	}
	// The default 20,000 (13 Oct 2026) is not lowered for anyone, and covers a quest provision and a fare.
	if s3, _, _ := KitAskHandOver(Defaults(), KitAskInput{Stage: fleetplan.StageMagicPrep, Purpose: fleetplan.MuleAskStage3, Kit: []string{"Cake"}}); s3.CoinsReserve != 20_000 {
		t.Fatalf("the default reserve was changed: %d", s3.CoinsReserve)
	}
	if d := Defaults().CoinsReserve; d < 12_500+600 {
		t.Fatalf("a %d-coin reserve cannot pay Dragon Slayer's ~12,500 of provisions and a 600-coin fare draw", d)
	}
}

// Under Magic 55 an account cannot cast High Level Alchemy: every Nature rune it holds goes on a kit ask,
// whatever its keep, its keep list or the ask itself says, and even a handful justifies the trade.
func TestKitAskHandsEveryNatureRuneUnderMagic55(t *testing.T) {
	in := func(magic int) KitAskInput {
		return KitAskInput{Stage: fleetplan.StageSurvivalCombat, Purpose: fleetplan.MuleAskStage5, Magic: magic,
			Kit: []string{"Lobster", "Nature rune"}}
	}
	base := Defaults()
	base.KeepItems = append(base.KeepItems, "Nature rune") // even a keep-list entry does not hold them back
	hold := Holdings{Runes: map[string]int{"Nature rune": 7, "Chaos rune": 40}}
	keep := Keep{Runes: map[string]int{"Nature rune": 400}} // the walk-alch kit's keep

	s, ok, _ := KitAskHandOver(base, in(54))
	if !ok || !s.AllNatures {
		t.Fatalf("Magic 54 hands every Nature rune over: %+v", s)
	}
	p := PlanDeposit(s, hold, keep, 100)
	if p.Runes["Nature rune"] != 7 || p.Natures != 7 || !p.Walk || p.Why != "Nature runes the account cannot cast" {
		t.Fatalf("all 7 Nature runes cross, and alone they justify the trade: %+v", p)
	}
	if p.Runes["Chaos rune"] != 40 {
		t.Fatalf("once the trade is made every kind's surplus goes with it: %+v", p.Runes)
	}
	h := PlanHandOff(s, HandOffInput{Holdings: hold, Keep: keep, Kit: stage5Kit()})
	if !h.Walk || h.Deposit.Natures != 7 {
		t.Fatalf("the whole hand-off walks for the natures: %+v", h)
	}

	// Magic 55 and up keeps its kit; an unknown level (0) changes nothing.
	for _, magic := range []int{55, 66, 0} {
		s, _, _ := KitAskHandOver(base, in(magic))
		if s.AllNatures {
			t.Fatalf("Magic %d is not under the cutoff", magic)
		}
		p := PlanDeposit(s, hold, keep, 100)
		if _, has := p.Runes["Nature rune"]; has || p.Natures != 0 {
			t.Fatalf("Magic %d keeps its natures: %+v", magic, p)
		}
	}
	if !HandsNatures(1) || HandsNatures(0) || HandsNatures(55) {
		t.Fatal("HandsNatures boundaries")
	}
	if NatureCutoff != fleetplan.HighLevelAlchemy.Level {
		t.Fatalf("the cutoff is High Level Alchemy's level, %d", fleetplan.HighLevelAlchemy.Level)
	}
	// Low Magic and no natures held: nothing forces a trade.
	s, _, _ = KitAskHandOver(Defaults(), in(30))
	if p := PlanDeposit(s, Holdings{Runes: map[string]int{"Chaos rune": 10}}, Keep{}, 100); p.Walk || p.Natures != 0 {
		t.Fatalf("no natures, no forced trade: %+v", p)
	}
}
