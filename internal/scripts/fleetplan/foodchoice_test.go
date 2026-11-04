package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/fighterfood"
)

// The blind ladder, used until something can read the mule's bank. Same rule as
// ChooseFoodToRequest: never a heal above the account's maximum, first.
func TestFoodLadderNeverOpensWithAnOversizedHeal(t *testing.T) {
	// A Stage 3 account: ten hitpoints. A shark heals 20.
	ladder := FoodLadderFor(10)
	if len(ladder) == 0 {
		t.Fatal("no ladder at all for a ten-hitpoint account")
	}
	if ladder[0] == "Shark" {
		t.Error("the ladder opens on Shark for a ten-hitpoint account — half of it is wasted")
	}
	if ladder[0] != "Cake" {
		t.Errorf("ladder opens on %q, want \"Cake\" — it fits and it is the deepest pile", ladder[0])
	}
	// Everything that fits must come before anything that overheals.
	seenOver := false
	for _, name := range ladder {
		over := fighterfood.FoodHealAmount(name) > 10 // judged by heal, not by a fixed list of names
		if over {
			seenOver = true
			continue
		}
		if seenOver {
			t.Errorf("%q fits but comes after an oversized food", name)
		}
	}
}

func TestFoodLadderTakesTheBigFoodOnceTheAccountIsBigEnough(t *testing.T) {
	// By Stage 6 an account has the hitpoints to use a shark in full.
	ladder := FoodLadderFor(99)
	if len(ladder) == 0 {
		t.Fatal("no ladder")
	}
	found := false
	for _, n := range ladder {
		if n == "Shark" {
			found = true
		}
	}
	if !found {
		t.Error("Shark missing from a 99-hitpoint ladder")
	}
}

func TestFoodLadderOverhealsRatherThanOfferingNothing(t *testing.T) {
	// A 1-hitpoint account: nothing fits, but starving is worse than wasting.
	ladder := FoodLadderFor(1)
	if len(ladder) == 0 {
		t.Fatal("an account too small for any food still needs something to ask for")
	}
}

// The owner's rule, 2026-09-22: accounts may "grab the mules runes to speed up theiving
// process" — "but it should grab enough so theres some left for the fleet".
func TestFairShareLeavesSomeForEveryoneElse(t *testing.T) {
	// The live mule's chaos runes, the rune the Thieving grind exists to farm.
	const chaos = 5764
	got := FairShare(2000, chaos, 110)
	if got <= 0 {
		t.Fatalf("FairShare(2000, %d, 110) = %d — a stock this size has something to give", chaos, got)
	}
	// An equal split (47 each) is under the minimum lot, so the share is a lot (a user request: hand out
	// more at a time); the first askers take lots until the published stock is down to its reserve.
	if got != FairShareMinLot {
		t.Errorf("share %d, want the %d lot", got, FairShareMinLot)
	}
	// The reserve genuinely survives: a stock whose spendable part cannot cover a lot gives nothing.
	if FairShare(2000, FairShareMinLot, 110) != 0 {
		t.Error("a stock of one lot, reserve included, was handed out")
	}
}

func TestFairShareNeverExceedsWhatWasWanted(t *testing.T) {
	// A deep stockpile and a small need: take the need, not the share.
	if got := FairShare(5, 1000000, 10); got != 5 {
		t.Errorf("FairShare(5, 1000000, 10) = %d, want 5 — a fair share is a ceiling, not a quota", got)
	}
}

func TestFairShareRefusesAStockTooThinToSplit(t *testing.T) {
	// The live case this is really for: ONE Adamant axe and a hundred accounts. Handing it to
	// whoever asks first is exactly what "leave some for the fleet" forbids.
	if got := FairShare(1, 1, 100); got != 0 {
		t.Errorf("FairShare(1, 1, 100) = %d, want 0 — one item cannot be split a hundred ways", got)
	}
	if got := FairShare(10, 50, 100); got != 0 {
		t.Errorf("FairShare(10, 50, 100) = %d, want 0 — under one each, nobody should take the lot", got)
	}
}

func TestFairShareHandlesNonsenseWithoutPanicking(t *testing.T) {
	cases := [][3]int{{0, 100, 10}, {-1, 100, 10}, {10, 0, 10}, {10, -5, 10}}
	for _, c := range cases {
		if got := FairShare(c[0], c[1], c[2]); got != 0 {
			t.Errorf("FairShare%v = %d, want 0", c, got)
		}
	}
	// An unstated cohort must behave as one of many, not as the only account.
	alone := FairShare(1000, 100000, 0)
	many := FairShare(1000, 100000, DefaultCohortShare)
	if alone != many {
		t.Errorf("an unstated cohort gave %d, a stated %d — the default must be the cohort, "+
			"not a single account", alone, many)
	}
}

// The live hole this closes, 2026-09-22: the first Stage 2 mule asks went out before the mule
// had published its bank, so they fell through to the unbounded want and acct76 took 150
// chaos runes out of a fleet stock of 5,764 in a single trade — while accounts asking a minute
// later, once the directory had a row, were correctly held to 53 each.
func TestBlindShareBoundsAnAskMadeWithoutKnowingTheStock(t *testing.T) {
	if got := BlindShare(150); got != BlindAskCap {
		t.Errorf("BlindShare(150) = %d, want %d — an unknown stock must not be asked for 150 of "+
			"anything, which is what drained the chaos runes", got, BlindAskCap)
	}
	// A hundred accounts at the cap must not empty the scarcest rune the fleet actually holds.
	const chaos = 5764
	if BlindAskCap*100 > chaos {
		t.Errorf("a hundred blind asks of %d is %d, which exceeds the %d chaos runes in stock",
			BlindAskCap, BlindAskCap*100, chaos)
	}
	// A modest want is unchanged: the cap is a ceiling, not a quota.
	if got := BlindShare(17); got != 17 {
		t.Errorf("BlindShare(17) = %d, want 17", got)
	}
	if got := BlindShare(0); got != 0 {
		t.Errorf("BlindShare(0) = %d, want 0", got)
	}
}

// Live 8 Oct: 140 Nature runes split over the cohort went out one a trade, and the mule spent its time
// trading instead of cooking. A stock-limited share under FairShareMinLot asks nothing; a small want is met.
func TestFairShareNeverHandsOutACrumb(t *testing.T) {
	if got := FairShare(1000, 140, 100); got != FairShareMinLot {
		t.Fatalf("140 natures over 100 accounts: share %d, want a lot of %d", got, FairShareMinLot)
	}
	if got := FairShare(1000, 40, 100); got != 0 {
		t.Fatalf("40 natures: share %d, want 0 (cannot cover a lot)", got)
	}
	if got := FairShare(1000, 20000, 100); got < FairShareMinLot {
		t.Fatalf("a deep stock: share %d, want at least %d", got, FairShareMinLot)
	}
	if got := FairShare(8, 4000, 100); got != 8 {
		t.Fatalf("a small want from a deep stock: %d, want 8", got)
	}
}
