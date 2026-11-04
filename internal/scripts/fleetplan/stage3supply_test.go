package fleetplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The price port against hand-worked values of shop.rs2's calc_shop_value.
func TestPriceAtMatchesShopFormula(t *testing.T) {
	cases := []struct {
		shop  RuneShop
		rune  string
		stock int
		want  int
	}{
		{Lundail, "Chaos rune", 250, 15}, // at base: exactly the obj cost
		{Lundail, "Chaos rune", 249, 15}, // 1030*15/1000 = 15.45, truncated
		{Lundail, "Chaos rune", 240, 19}, // 1300*15/1000 = 19.5
		{Lundail, "Chaos rune", 0, 90},   // capped at 6x
		{Lundail, "Air rune", 200, 4},    // base
		{Lundail, "Air rune", 125, 13},   // 3250*4/1000 = 13
		{Lundail, "Air rune", 250, 1},    // above base: int5 floors at 100, price floors at 1
		{Aubury, "Chaos rune", 900, 30},  // Aubury's delta is 10: 2000*15/1000
		{Aubury, "Air rune", 2000, 4},    // base
		{Lundail, "Blood rune", 100, 0},  // not sold
	}
	for _, c := range cases {
		if got := c.shop.PriceAt(c.rune, c.stock); got != c.want {
			t.Errorf("%s %s at stock %d = %d, want %d", c.shop.Keeper, c.rune, c.stock, got, c.want)
		}
	}
}

func TestCostToBuyWalksTheStockDownOneAtATime(t *testing.T) {
	want := 0
	for i := 0; i < 30; i++ {
		want += Lundail.PriceAt("Chaos rune", 250-i)
	}
	if got := Lundail.CostToBuy("Chaos rune", 250, 30); got != want {
		t.Fatalf("CostToBuy = %d, want the per-item walk %d", got, want)
	}
	// Clamped to the stock actually there.
	if Lundail.CostToBuy("Chaos rune", 5, 30) != Lundail.CostToBuy("Chaos rune", 5, 5) {
		t.Fatal("buying more than the stock should cost the same as buying the stock")
	}
}

func TestAffordableStopsAtCoinsAndCap(t *testing.T) {
	n, cost := Lundail.Affordable("Chaos rune", 250, 100, 1000)
	if cost > 100 || n == 0 {
		t.Fatalf("100 coins bought %d for %d", n, cost)
	}
	if next := Lundail.PriceAt("Chaos rune", 250-n); cost+next <= 100 {
		t.Fatalf("stopped at %d chaos with %d coins left when the next cost %d", n, 100-cost, next)
	}
	// Cap at 3x the base cost of an air rune (12): the first rune dearer than 12 stops it.
	n, _ = Lundail.AffordableCapped("Air rune", 200, 1<<30, 1000, 12)
	if Lundail.PriceAt("Air rune", 200-n+1) > 12 || Lundail.PriceAt("Air rune", 200-n) <= 12 {
		t.Fatalf("cap stopped after %d air: last price %d, next %d", n,
			Lundail.PriceAt("Air rune", 200-n+1), Lundail.PriceAt("Air rune", 200-n))
	}
}

func TestEstimateStockAndRestockWait(t *testing.T) {
	tick := 600 * time.Millisecond
	l, _ := Lundail.Line("Chaos rune")
	t0 := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if got := l.EstimateStock(100, t0, t0.Add(10*time.Minute), tick); got != 110 {
		t.Fatalf("10 minutes = 1000 ticks = +10 at one per 100 ticks; got %d", got)
	}
	if got := l.EstimateStock(245, t0, t0.Add(time.Hour), tick); got != 250 {
		t.Fatalf("estimate must cap at base, got %d", got)
	}
	if got := l.EstimateStock(0, time.Time{}, t0, tick); got != 250 {
		t.Fatalf("never seen means assume full, got %d", got)
	}
	if got := l.RestockWait(100, 150, tick); got != 50*100*tick {
		t.Fatalf("RestockWait = %v", got)
	}
	if got := l.RestockWait(100, 400, tick); got != 150*100*tick {
		t.Fatalf("RestockWait must cap the target at base, got %v", got)
	}
}

func TestWildernessKitRule(t *testing.T) {
	sword := KitItem{Name: "Bronze sword", Count: 1}
	ok := CheckWildernessKit(Kit{Worn: []KitItem{sword}})
	if !ok.OK || !ok.BladeWorn {
		t.Fatalf("a wielded sword and nothing else should pass: %+v", ok)
	}
	knife := CheckWildernessKit(Kit{Pack: []KitItem{{Name: "Knife", Count: 1}}})
	if !knife.OK || knife.BladeWorn {
		t.Fatalf("a carried knife and nothing else should pass: %+v", knife)
	}
	bad := []struct {
		name string
		kit  Kit
		why  string
	}{
		{"any coins at all", Kit{Worn: []KitItem{sword}, Pack: []KitItem{{Name: "Coins", Count: 1}}}, "1 coins"},
		{"purchase coins", Kit{Worn: []KitItem{sword}, Pack: []KitItem{{Name: "Coins", Count: 1056}}}, "drawn at Gundai"},
		{"staff worn", Kit{Worn: []KitItem{sword, {Name: "Staff", Count: 1}}}, "wearing Staff"},
		{"food", Kit{Worn: []KitItem{sword}, Pack: []KitItem{{Name: "Cake", Count: 1}}}, "carrying Cake"},
		{"runes", Kit{Worn: []KitItem{sword}, Pack: []KitItem{{Name: "Air rune", Count: 25}}}, "25 x Air rune"},
		{"no blade", Kit{}, "no sword or knife"},
		{"two blades", Kit{Worn: []KitItem{sword}, Pack: []KitItem{{Name: "Knife", Count: 1}}}, "more than one blade"},
		{"dagger is not the owner's blade", Kit{Worn: []KitItem{{Name: "Bronze dagger", Count: 1}}}, "wearing Bronze dagger"},
	}
	for _, b := range bad {
		v := CheckWildernessKit(b.kit)
		if v.OK {
			t.Errorf("%s: passed a kit that breaks the rule", b.name)
			continue
		}
		if !strings.Contains(strings.Join(v.Problems, "; "), b.why) {
			t.Errorf("%s: problems %q do not say %q", b.name, v.Problems, b.why)
		}
	}
}

func TestWildernessHeadingFleesToTheNearerExit(t *testing.T) {
	if got := WildernessHeading(KeepLand, false, false); got != MageBankLadder {
		t.Fatalf("inbound heads for the ladder, got %v", got)
	}
	if got := WildernessHeading(MageBankLadder, true, false); got != KeepLever {
		t.Fatalf("outbound heads for the Keep lever, got %v", got)
	}
	nearKeep := tile.Tile{X: 3150, Z: 3925}
	if got := WildernessHeading(nearKeep, false, true); got != KeepLever {
		t.Fatalf("attacked beside the Keep, inbound, should flee to the lever: %v", got)
	}
	nearLadder := tile.Tile{X: 3100, Z: 3955}
	if got := WildernessHeading(nearLadder, true, true); got != MageBankLadder {
		t.Fatalf("attacked beside the ladder, outbound, should flee down the ladder: %v", got)
	}
}

// TestStage3WillNotTrainWithoutFood is the LIVE snapshot from the 2026-09-22 lab e2e
// (`scarabe2e922`), taken from its own log line
//
//	stage 3 — training kit: Staff of air, runes map[Mind rune:50], 0 food;
//	          bank still holds map[Air rune:90]
//
// at the moment the account walked to the Ardougne cow field and hand-cast at retaliating
// cows with ten hitpoints and NOTHING TO EAT. It died there six times in that run, each
// death costing a Lumbridge respawn and a cross-map walk back.
//
// The decider's training branch used to return before the food gate ever ran, so
// Phase3Food was reachable only once the account had no casts left — by which time
// TripDue had already claimed the loop. The phase that exists to fetch food could
// therefore never fire while training, which is exactly when the food is needed.
func TestStage3WillNotTrainWithoutFood(t *testing.T) {
	live := Stage3Snapshot{
		Tile:        &tile.Tile{X: TrainStand.X, Z: TrainStand.Z},
		HP:          10,
		MaxHP:       10,
		EatBelowHP:  6,
		MagicLevel:  7,
		MagicTarget: 13,
		HaveStaff:   true,
		TrainCasts:  50,
		Food:        0,
		FoodWanted:  8,
	}
	if got := DecideStage3Phase(live); got != Phase3Food {
		t.Fatalf("an account training at the cows with 0 food and 8 wanted chose %s, want %s", got, Phase3Food)
	}
	// With food in the pack it trains, so the gate is not simply "never train".
	fed := live
	fed.Food = 8
	if got := DecideStage3Phase(fed); got != Phase3Train {
		t.Fatalf("a fed account chose %s, want %s", got, Phase3Train)
	}
	// And a bank with no food to give (FoodWanted latched to 0 by the runner's foodShortUntil) no
	// longer trains hungry (nodeaths, 2026-09-26): Phase3Hungry, whose runner thieves for food at
	// the Baker's stall. TestStage3NeverTrainsOnAnEmptyPack has the rest.
	starved := live
	starved.FoodWanted = 0
	if got := DecideStage3Phase(starved); got != Phase3Hungry {
		t.Fatalf("an account with no food available chose %s, want %s", got, Phase3Hungry)
	}
	// Hunger still beats the restock trip: eat what is in the pack first.
	hurt := live
	hurt.HP, hurt.Food = 3, 2
	if got := DecideStage3Phase(hurt); got != Phase3Eat {
		t.Fatalf("a hurt account with food chose %s, want %s", got, Phase3Eat)
	}
}

// TestStage3FoodRestockHasHysteresis is the thrash guard on the fix above: the restock STARTS
// at half the keep and RUNS ON to the keep, instead of walking the account the 40 tiles back
// to the Ardougne bank after every single bite.
func TestStage3FoodRestockHasHysteresis(t *testing.T) {
	base := Stage3Snapshot{
		Tile:        &tile.Tile{X: TrainStand.X, Z: TrainStand.Z},
		HP:          10,
		MaxHP:       10,
		EatBelowHP:  6,
		MagicLevel:  7,
		MagicTarget: 13,
		HaveStaff:   true,
		TrainCasts:  50,
		FoodWanted:  8,
	}
	for _, c := range []struct {
		name     string
		food     int
		stocking bool
		want     Phase3
	}{
		{"seven of eight, not stocking: train", 7, false, Phase3Train},
		{"five of eight, not stocking: train", 5, false, Phase3Train},
		{"four of eight, not stocking: restock", 4, false, Phase3Food},
		{"seven of eight, mid-restock: finish it", 7, true, Phase3Food},
		{"eight of eight, mid-restock: done", 8, true, Phase3Train},
	} {
		s := base
		s.Food, s.Stocking = c.food, c.stocking
		if got := DecideStage3Phase(s); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// TestStage3ThievingFoodGateHasTheSameHysteresis pins the non-training half of the food
// gate, which an account past its Magic target takes. It was missed by the first pass of the
// training fix and behaved differently from it: live run b5g115r7t logged "the bank gave 7
// food of 8 wanted: stocking at the Baker's stall" -- a 29-tile round trip for one cake.
func TestStage3ThievingFoodGateHasTheSameHysteresis(t *testing.T) {
	// Magic target already met, so `training` is false and the supply ladder is taken.
	base := Stage3Snapshot{
		Tile:        &tile.Tile{X: TrainStand.X, Z: TrainStand.Z},
		HP:          10,
		MaxHP:       10,
		EatBelowHP:  6,
		MagicLevel:  13,
		MagicTarget: 13,
		HaveStaff:   true,
		FoodWanted:  8,
	}
	for _, c := range []struct {
		name     string
		food     int
		stocking bool
		want     Phase3
	}{
		{"seven of eight, not stocking: thieve", 7, false, Phase3Thieve},
		{"four of eight, not stocking: restock", 4, false, Phase3Food},
		{"seven of eight, mid-restock: finish it", 7, true, Phase3Food},
		{"none at all: restock", 0, false, Phase3Food},
	} {
		s := base
		s.Food, s.Stocking = c.food, c.stocking
		if got := DecideStage3Phase(s); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// TestStage3AsksTheMuleForFoodBeforeRiskingTheStall pins an ordering defect that killed the
// account in BOTH runs that got as far as needing food (b5g115r7t and b51x1x5s3), at the same
// tile, 2668,3312.
//
// THE RETREAT RULE CANNOT SAVE IT, and that is measured rather than assumed. Run b51x1x5s3
// carried 29d69ada's retreat fix and died anyway, with ZERO "leaving the rung" lines:
//
//	HP DIAG: 10 -> 8 near 2668,3312,0 ... [Hero(69) Guard(20) Guard(20) Knight(46)]
//	HP DIAG:  8 -> 5
//	HP DIAG:  5 -> 2
//	DEATH DIAG: died near 2668,3312,0
//
// Four ticks from full health to dead. At the lab's 200ms tick that is about 0.8 seconds of
// wall clock, and the stage's own decision loop -- an idle floor plus a full snapshot per
// pass -- cannot come round in time. No amount of "leave when hurt" fixes a ten-hitpoint
// account that is already committed to standing there.
//
// Nothing attacks on sight, so this is self-inflicted: guard1, guard2, hero and
// knight_of_ardougne all carry NO huntrange and NO huntmode in content 289. The guards engage
// because they spot the account's own theft.
//
// So the answer is not to be there. The mule is free, safe, and posts at a bank rather than
// in a market full of guards, and the owner's own supply order puts the stockpile ahead of
// everything else. The reason it was not asked is an accident of ordering: on ARRIVAL the
// bank has not been read, so HaveStaff is true (`!known` counts as having one) and the
// staff-shaped mule branch above is skipped -- which used to fall through to training and now
// falls through to the stall.
//
// This does NOT make the stall safe, and nothing here claims it does. It only stops the stall
// being the FIRST answer when a stocked mule is one ask away. An account whose mule is empty
// or not yet due still takes the stall and still risks this death.
func TestStage3AsksTheMuleForFoodBeforeRiskingTheStall(t *testing.T) {
	base := Stage3Snapshot{
		Tile:        &tile.Tile{X: TrainStand.X, Z: TrainStand.Z},
		HP:          10,
		MaxHP:       10,
		EatBelowHP:  6,
		MagicLevel:  7,
		MagicTarget: 13,
		HaveStaff:   true,
		TrainCasts:  50,
		Food:        0,
		FoodWanted:  8,
	}
	withMule := base
	withMule.MuleFoodAskDue = true
	if got := DecideStage3Phase(withMule); got != Phase3Mule {
		t.Fatalf("a hungry account with a mule ask due chose %s, want %s", got, Phase3Mule)
	}
	// No mule due: the stall is still the fallback, exactly as before.
	if got := DecideStage3Phase(base); got != Phase3Food {
		t.Fatalf("a hungry account with no mule due chose %s, want %s", got, Phase3Food)
	}
	// A fed account is not sent to the mule just because an ask is due.
	fed := withMule
	fed.Food = 8
	if got := DecideStage3Phase(fed); got != Phase3Train {
		t.Fatalf("a fed account chose %s, want %s", got, Phase3Train)
	}
	// And the same for an account past its Magic target, which takes the other gate.
	past := withMule
	past.MagicLevel = 13
	if got := DecideStage3Phase(past); got != Phase3Mule {
		t.Fatalf("a hungry account past its Magic target chose %s, want %s", got, Phase3Mule)
	}
}
