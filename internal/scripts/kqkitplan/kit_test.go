package kqkitplan

import (
	"testing"

	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
)

func spell(t *testing.T, name string) fleetplan.Spell {
	t.Helper()
	sp, ok := fleetplan.SpellByName(name)
	if !ok {
		t.Fatalf("no %s", name)
	}
	return sp
}

func TestContentNumbersAgreeWithStageSix(t *testing.T) {
	if QueenHitpoints != fleetplan.KQQueenHP {
		t.Fatalf("Queen hitpoints %d disagree with scarabplan.KQQueenHP %d", QueenHitpoints, fleetplan.KQQueenHP)
	}
	if SteadyHitpoints != 63 {
		t.Fatalf("steady = two 31s plus one: %d", SteadyHitpoints)
	}
}

func TestEligibilityTiers(t *testing.T) {
	wb := spell(t, "Wind Bolt")
	cases := []struct {
		sk   Skills
		want Tier
	}{
		{Skills{Magic: 16, Hitpoints: 99, Prayer: 99}, TierNone}, // cannot cast Wind Bolt (17)
		{Skills{Magic: 45, Hitpoints: 31, Prayer: 99}, TierNone}, // one 31 kills
		{Skills{Magic: 45, Hitpoints: 32, Prayer: 36}, TierSwarm},
		{Skills{Magic: 45, Hitpoints: 32, Prayer: 37}, TierProtected},
		{Skills{Magic: 45, Hitpoints: 62, Prayer: 43}, TierProtected},
		{Skills{Magic: 45, Hitpoints: 63, Prayer: 37}, TierSteady},
	}
	for _, c := range cases {
		got, why := Eligibility(c.sk, wb)
		if got != c.want || why == "" {
			t.Errorf("%+v: got %s (%q), want %s", c.sk, got, why, c.want)
		}
	}
	if got, _ := Eligibility(Skills{Magic: 99, Hitpoints: 99}, fleetplan.Spell{}); got != TierNone {
		t.Fatal("no spell, no kit")
	}
	if TierSteady.String() != "steady" || TierNone.String() != "none" {
		t.Fatal("tier names")
	}
}

func TestCensus(t *testing.T) {
	wb := spell(t, "Wind Bolt")
	c := Census([]Skills{{10, 10, 1}, {45, 40, 1}, {45, 40, 40}, {50, 70, 45}, {45, 33, 1}}, wb)
	if c[TierNone] != 1 || c[TierSwarm] != 2 || c[TierProtected] != 1 || c[TierSteady] != 1 {
		t.Fatalf("census: %v", c)
	}
}

func lineFor(lines []fleetplan.AskLine, first string) (fleetplan.AskLine, bool) {
	for _, l := range lines {
		if len(l.Choices) > 0 && l.Choices[0] == first {
			return l, true
		}
	}
	return fleetplan.AskLine{}, false
}

func TestKitForANakedAccount(t *testing.T) {
	kq := fleetplan.DefaultKQSettings()
	kq.CastsPerLife = 10
	in := KitInput{Spell: spell(t, "Wind Bolt"), KQ: kq, Region: fleetplan.KQRegionWest, Lives: 3}
	lines := Kit(in)

	staff, ok := lineFor(lines, "Staff of air")
	if !ok || !staff.TryDown || staff.Count != 1 {
		t.Fatalf("a staffless account asks the staff ladder: %+v", lines)
	}
	chaos, ok := lineFor(lines, "Chaos rune")
	if !ok || chaos.Count != StarterChaos || !chaos.FairShare {
		t.Fatalf("the owner's 1,000 chaos, fair-shared (scarce): %+v", chaos)
	}
	if _, ok := lineFor(lines, "Air rune"); ok {
		t.Fatal("the kit is planned for a Staff of air: no air runes")
	}
	rope, ok := lineFor(lines, fleetplan.RopeItem)
	if !ok || rope.Count != kq.Ropes {
		t.Fatalf("ropes: %+v", rope)
	}
	coins, ok := lineFor(lines, "Coins")
	if !ok || coins.Count != fleetplan.KitCoins(fleetplan.KQRegionWest, kq) {
		t.Fatalf("coins for the walk from the west: %+v", coins)
	}
	food, ok := lineFor(lines, FoodLadder[0])
	if !ok || !food.Bulk {
		t.Fatalf("food is a bulk ask on the big-heal ladder: %+v", lines)
	}
	if want := FoodFor(kq.FoodHeal, PackSlots); food.Count != want || food.Count*fighterfood.FoodHealAmount("Lobster") < kq.FoodHeal {
		t.Fatalf("food %d must carry %d hp even as lobsters", food.Count, kq.FoodHeal)
	}
}

func TestKitAsksOnlyTheShortfall(t *testing.T) {
	kq := fleetplan.DefaultKQSettings()
	kq.CastsPerLife = 10
	in := KitInput{
		Spell: spell(t, "Wind Bolt"), Staff: "Staff of air", KQ: kq, Region: fleetplan.KQRegionAlKharid, Lives: 1,
		Have:     map[string]int{"chaos rune": 4, "Rope": kq.Ropes, "Coins": 1_000_000},
		FoodHeld: 100,
	}
	lines := Kit(in)
	if len(lines) != 1 {
		t.Fatalf("only the chaos is short: %+v", lines)
	}
	if l := lines[0]; l.Choices[0] != "Chaos rune" || l.Count != StarterChaos-4 {
		t.Fatalf("1,000 - 4 chaos: %+v", l)
	}
	in.Have["chaos rune"] = StarterChaos
	if lines := Kit(in); len(lines) != 0 {
		t.Fatalf("a full kit asks nothing: %+v", lines)
	}
}

func TestKitWithAMagicStaffNeedsAir(t *testing.T) {
	kq := fleetplan.DefaultKQSettings()
	kq.CastsPerLife = 10
	lines := Kit(KitInput{Spell: spell(t, "Wind Bolt"), Staff: "Magic staff", KQ: kq, Region: fleetplan.KQRegionAlKharid})
	air, ok := lineFor(lines, "Air rune")
	if !ok || air.Count != 20 || air.FairShare {
		t.Fatalf("a Magic staff pays no air: 2 a bolt, not scarce: %+v", air)
	}
}

func TestFoodFor(t *testing.T) {
	lob := fighterfood.FoodHealAmount("Lobster")
	if lob != 12 {
		t.Fatalf("content says a lobster heals 12, fighterfood says %d", lob)
	}
	if FoodFor(100, 28) != 9 || FoodFor(100, 5) != 5 || FoodFor(0, 28) != 0 || FoodFor(10, 0) != 0 {
		t.Fatalf("FoodFor: %d %d", FoodFor(100, 28), FoodFor(100, 5))
	}
}
