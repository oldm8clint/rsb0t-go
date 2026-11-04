package fighterfood

import "testing"

func TestLagVolleysAreOneEveryFourTicksTheFirstAtOnce(t *testing.T) {
	for _, c := range []struct{ ticks, want int }{{-3, 0}, {0, 0}, {1, 1}, {4, 1}, {5, 2}, {8, 2}, {9, 3}, {12, 3}, {13, 4}, {15, 4}, {30, 8}} {
		if got := LagVolleys(c.ticks); got != c.want {
			t.Errorf("LagVolleys(%d) = %d, want %d", c.ticks, got, c.want)
		}
	}
}

// The level estimate is the six Watchman tiers of content's macro_watchman1 to 6 (vislevels 14, 29, 49, 79, 120, 159).
func TestLevelMaxHitReproducesTheWatchmanTiers(t *testing.T) {
	for _, c := range []struct{ level, want int }{{14, 2}, {29, 3}, {49, 5}, {79, 7}, {120, 11}, {159, 14}, {0, 0}, {-3, 0}} {
		if got := LevelMaxHit(c.level); got != c.want {
			t.Errorf("LevelMaxHit(%d) = %d, want %d", c.level, got, c.want)
		}
	}
}

func TestLagCapIsSeventyPercentRoundedUp(t *testing.T) {
	for _, c := range []struct{ max, want int }{{0, 0}, {-5, 0}, {10, 7}, {54, 38}, {55, 39}, {99, 70}} {
		if got := LagCap(c.max); got != c.want {
			t.Errorf("LagCap(%d) = %d, want %d", c.max, got, c.want)
		}
	}
}

// The 18 Oct 04:41 account: a Paladin thief at 54 maximum hitpoints, a Watchman event that hits for up to 11 beside the Paladin's 3, and
// an episode that hid 12 ticks.
func TestLagEatLineTable(t *testing.T) {
	const thief = 11 + 3 // the Watchman event and a failed steal
	for _, c := range []struct {
		name                    string
		maxHP, base, sum, ticks int
		want                    int
	}{
		{"no lag keeps the line", 54, 22, thief, 0, 22},
		{"the 04:41 episode: three volleys of 14 are over the cap, which is 38", 54, 22, thief, 12, 38},
		{"a bare stall of four seconds (7 ticks) is two volleys, 28", 54, 22, thief, 7, 28},
		{"a small hiding is one volley, 14, under the base: the base stands", 54, 22, thief, 3, 22},
		{"nothing that can hit it keeps the line", 54, 22, 0, 12, 22},
		{"a line over the cap is never lowered", 54, 40, thief, 12, 40},
		{"an unknown maximum keeps the line", 0, 22, thief, 12, 22},
		{"a negative hiding keeps the line", 54, 22, thief, -4, 22},
		{"the half line against one Ogre after a bad lag", 55, 27, 7, 13, 28}, // 4 volleys of 7
		{"a small account against a Cow", 10, 5, 1, 12, 5},
	} {
		if got := LagEatLine(c.maxHP, c.base, c.sum, c.ticks); got != c.want {
			t.Errorf("%s: LagEatLine(%d, %d, %d, %d) = %d, want %d", c.name, c.maxHP, c.base, c.sum, c.ticks, got, c.want)
		}
	}
}

// The line only ever rises with the hiding and with what can hit it, never falls below the base and never goes over the cap (or the base,
// when that is higher).
func TestLagEatLineIsMonotoneBoundedAndNeverBelowTheBase(t *testing.T) {
	for _, maxHP := range []int{10, 33, 54, 55, 99} {
		for _, base := range []int{0, maxHP / 3, maxHP / 2, LagCap(maxHP), maxHP} {
			prevSum := make([]int, 0)
			for sum := 0; sum <= 40; sum += 3 {
				prev := -1
				for ticks := 0; ticks <= 40; ticks++ {
					got := LagEatLine(maxHP, base, sum, ticks)
					if got < base {
						t.Fatalf("LagEatLine(%d, %d, %d, %d) = %d: under the base", maxHP, base, sum, ticks, got)
					}
					if got > max(base, LagCap(maxHP)) {
						t.Fatalf("LagEatLine(%d, %d, %d, %d) = %d: over the cap %d", maxHP, base, sum, ticks, got, LagCap(maxHP))
					}
					if got < prev {
						t.Fatalf("LagEatLine(%d, %d, %d, %d) = %d fell from %d with more ticks hidden", maxHP, base, sum, ticks, got, prev)
					}
					prev = got
				}
				prevSum = append(prevSum, LagEatLine(maxHP, base, sum, 12))
			}
			for i := 1; i < len(prevSum); i++ {
				if prevSum[i] < prevSum[i-1] {
					t.Fatalf("LagEatLine(%d, %d, ., 12) fell with more that can hit: %v", maxHP, base, prevSum)
				}
			}
		}
	}
}

// The share form eats at exactly the hitpoints the line form does, through ShouldEat's own strict comparison, and leaves a share the lag
// does not raise untouched.
func TestLagEatFracEatsAtTheLine(t *testing.T) {
	const maxHP = 54
	in := func(hp int, frac float64) bool {
		return ShouldEat(EatInput{HP: hp, MaxHP: maxHP, Heal: 13, FoodCount: 5, Mode: EatThreshold, ThresholdFrac: frac})
	}
	// No lag, and a lag that does not reach the base: the share is returned as it was.
	if got := LagEatFrac(maxHP, 0.5, 14, 0); got != 0.5 {
		t.Errorf("no lag: %v, want the share untouched", got)
	}
	if got := LagEatFrac(maxHP, 0.5, 14, 3); got != 0.5 {
		t.Errorf("a one-volley lag under the base: %v, want the share untouched", got)
	}
	if got := LagEatFrac(maxHP, 0.5, 0, 12); got != 0.5 {
		t.Errorf("nothing that can hit it: %v, want the share untouched", got)
	}
	// 12 hidden ticks, hits of 14: the line is the cap, 38.
	frac := LagEatFrac(maxHP, 0.5, 14, 12)
	for hp := 1; hp <= maxHP; hp++ {
		if want := hp <= 38; in(hp, frac) != want {
			t.Errorf("hp %d of %d at share %v: eats = %v, want %v (the line is 38)", hp, maxHP, frac, in(hp, frac), want)
		}
	}
	// And the plain 40% share: 21 and under without lag, 38 and under with it.
	if !in(21, 0.4) || in(22, 0.4) {
		t.Error("setup: the 40% share should eat at 21 and not at 22 of 54")
	}
	frac = LagEatFrac(maxHP, 0.4, 14, 12)
	if !in(38, frac) || in(39, frac) {
		t.Errorf("40%% share after a bad lag = %v: should eat at 38 and not at 39 of 54", frac)
	}
}
