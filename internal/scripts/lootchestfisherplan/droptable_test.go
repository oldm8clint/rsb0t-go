package lootchestfisherplan

import (
	"math/rand"
	"testing"
)

func TestTableCoversEveryRoll(t *testing.T) {
	if got := TotalWeight(); got != RandomRange {
		t.Fatalf("branch weights sum to %d, want content's own random(%d) range — the table is mistranscribed", got, RandomRange)
	}
}

func TestNothingBranchIsSeventeenInOneHundredAndTwentyEight(t *testing.T) {
	// The branch that pays a key for one dragonstone and nothing else. Worth asserting: it is
	// the single biggest drag on the expected value and it is easy to lose when transcribing
	// an if/else chain whose last arm is implicit.
	for _, b := range Table() {
		if b.Name == "nothing but the dragonstone" {
			if b.Weight != 17 {
				t.Fatalf("the empty branch has weight %d, want 128-111=17", b.Weight)
			}
			if len(b.Rewards) != 0 {
				t.Fatalf("the empty branch has rewards %+v, want none", b.Rewards)
			}
			return
		}
	}
	t.Fatal("no empty branch: content's chain ends at $random < 111, leaving 111..127 unmatched")
}

func TestNewSlotsForCountsStacksOnceAndTheRestPerItem(t *testing.T) {
	empty := func(int) int { return 0 }
	byName := map[string]Branch{}
	for _, b := range Table() {
		byName[b.Name] = b
	}
	cases := []struct {
		name string
		want int
	}{
		{"nothing but the dragonstone", 1},    // the dragonstone alone
		{"rune bundle", 12},                   // 11 rune types, none held, + dragonstone
		{"2 rubies and 2 diamonds", 5},        // 4 gems + dragonstone
		{"3 runite bars", 4},                  // 3 bars + dragonstone
		{"5 raw swordfish and 1000 coins", 7}, // 5 fish + a coin slot + dragonstone
		{"spinach roll and 2000 coins", 3},    // roll + a coin slot + dragonstone
		{"150 noted iron ore", 2},             // one note slot + dragonstone
		{"750 coins and a key half", 3},       // coin slot + half + dragonstone
	}
	for _, tc := range cases {
		b, ok := byName[tc.name]
		if !ok {
			t.Fatalf("no branch named %q", tc.name)
		}
		if got := NewSlotsFor(b, empty); got != tc.want {
			t.Fatalf("NewSlotsFor(%q, empty pack) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestStacksAlreadyHeldCostNoNewSlot(t *testing.T) {
	var runes Branch
	for _, b := range Table() {
		if b.Name == "rune bundle" {
			runes = b
		}
	}
	holdingEveryRune := func(int) int { return 1 }
	if got := NewSlotsFor(runes, holdingEveryRune); got != 1 {
		t.Fatalf("the rune bundle needs %d new slots when every rune is already held, want 1 (just the dragonstone)", got)
	}
}

func TestWorstCaseFallsAsTheStacksFillIn(t *testing.T) {
	empty := func(int) int { return 0 }
	if got := WorstCaseNewSlots(empty); got != 12 {
		t.Fatalf("worst case on an empty pack = %d, want 12 (the rune bundle's 11 types plus the dragonstone)", got)
	}
	full := func(int) int { return 1 }
	if got := WorstCaseNewSlots(full); got != 6 {
		t.Fatalf("worst case holding every stack = %d, want 6 (five raw swordfish plus the dragonstone)", got)
	}
	if WorstCaseNewSlots(nil) != WorstCaseNewSlots(empty) {
		t.Fatal("a nil held func should read as an empty pack")
	}
}

func TestExpectedCoinsPerKeyIsTheWeightedTableNotAGuess(t *testing.T) {
	// A price book that values only the dragonstone reduces the whole table to that one item.
	only := func(id int) int64 {
		if id == UncutDragonstone {
			return 600
		}
		return 0
	}
	if got := ExpectedCoinsPerKey(only); got != 600 {
		t.Fatalf("EV with only the dragonstone priced = %v, want 600 (it is added on every open)", got)
	}
	// Every item at one coin: the EV is 1 (the dragonstone) plus the weighted count of items.
	flat := func(int) int64 { return 1 }
	got := ExpectedCoinsPerKey(flat)
	var want float64 = 1
	for _, b := range Table() {
		var n int64
		for _, r := range b.Rewards {
			n += int64(r.Count)
		}
		want += float64(b.Weight) / float64(RandomRange) * float64(n)
	}
	if got != want {
		t.Fatalf("EV at one coin an item = %v, want %v", got, want)
	}
	if ExpectedCoinsPerKey(nil) != 0 {
		t.Fatal("a nil price book should value nothing rather than inventing a price")
	}
}

// --- the simulation KeysPerTrip is chosen by ---

// simulateTrip runs one trip carrying k keys in a 28-slot pack, opening only while
// CanOpenAnother allows it, and reports how many chests were opened and whether anything
// overflowed. Overflow must be impossible: that is what the guard is for.
func simulateTrip(rng *rand.Rand, k int) (opened int, overflowed bool) {
	held := map[int]int{}
	stacks := stackableIDs()
	stacksHeld := func(id int) int { return held[id] }
	slots := func() int {
		n := 0
		for id, c := range held {
			if stacks[id] {
				n++
			} else {
				n += c
			}
		}
		return n
	}
	table := Table()
	for i := 0; i < k; i++ {
		keysLeft := k - i
		free := PackSlots - keysLeft - slots()
		if !CanOpenAnother(free, stacksHeld) {
			return opened, false
		}
		held[UncutDragonstone]++
		roll, acc := rng.Intn(RandomRange), 0
		for _, b := range table {
			acc += b.Weight
			if roll < acc {
				for _, r := range b.Rewards {
					held[r.ID] += r.Count
				}
				break
			}
		}
		opened++
		if slots() > PackSlots-(k-i-1) {
			return opened, true
		}
	}
	return opened, false
}

// stackableIDs is the table's own stacking flag per obj id. Built once per trip rather than
// scanned per slot: the simulation reads it hundreds of thousands of times.
func stackableIDs() map[int]bool {
	out := map[int]bool{}
	for _, b := range Table() {
		for _, r := range b.Rewards {
			out[r.ID] = r.Stacks
		}
	}
	return out
}

func TestKeysPerTripIsThePeakOfTheSimulation(t *testing.T) {
	// 20,000 trips per K: enough to place K within the plateau, and quick. The constant's own
	// doc comment quotes a 200,000-trip run, which differs from this one only in the third
	// significant figure.
	const trials = 20000
	// tolerance is how close to the best mean counts as "on the plateau". K=6 and K=7 sit
	// within about 0.02 opens of each other; 5 is a fifth of an open behind.
	const tolerance = 0.05
	rng := rand.New(rand.NewSource(1))
	mean := map[int]float64{}
	for k := 3; k <= 9; k++ {
		total := 0
		for i := 0; i < trials; i++ {
			opened, over := simulateTrip(rng, k)
			if over {
				t.Fatalf("K=%d overflowed the pack: CanOpenAnother let an open through that did not fit", k)
			}
			total += opened
		}
		mean[k] = float64(total) / trials
		t.Logf("K=%d: mean %.2f chests opened per trip", k, mean[k])
	}
	best, bestK := 0.0, 0
	for k, m := range mean {
		if m > best || (m == best && k < bestK) {
			best, bestK = m, k
		}
	}
	// KeysPerTrip must be at the plateau: within a hundredth of the best, and the smallest
	// such K, so the trip carries the fewest keys for the same number of opens.
	if mean[KeysPerTrip] < best-tolerance {
		t.Fatalf("KeysPerTrip=%d opens %.2f per trip but K=%d opens %.2f — the constant is stale",
			KeysPerTrip, mean[KeysPerTrip], bestK, best)
	}
	if KeysPerTrip > 3 && mean[KeysPerTrip-1] >= best-tolerance {
		t.Fatalf("KeysPerTrip=%d carries one key more than it needs: K=%d already opens %.2f of the best %.2f",
			KeysPerTrip, KeysPerTrip-1, mean[KeysPerTrip-1], best)
	}
}

func TestKeysToWithdrawNeverExceedsATripOrTheBank(t *testing.T) {
	for _, tc := range []struct{ banked, held, want int }{
		// Nothing held: draw a full trip, bounded by the bank.
		{-1, 0, 0}, {0, 0, 0}, {1, 0, 1}, {KeysPerTrip, 0, KeysPerTrip}, {99, 0, KeysPerTrip},

		// TOPPING UP, which is the case that was missing and cost spare104 a 170-tile round
		// trip for two opens. A trip ends when the pack runs out of ROOM, not keys, so coming
		// back holding a few is normal and they must be added to, not ignored.
		{53, 2, KeysPerTrip - 2}, // the live reading: "2 key(s) in hand, 53 in the bank"
		{99, 1, KeysPerTrip - 1},
		{99, KeysPerTrip - 1, 1},

		// Already a full trip's worth, or more: draw nothing.
		{99, KeysPerTrip, 0}, {99, KeysPerTrip + 3, 0},

		// The bank is the ceiling even when short.
		{2, 3, 2}, {0, 2, 0},

		// Negatives must not produce a negative draw. Held clamps to 0, so this wants a full
		// trip -- but the bank holds only 5, and the bank is still the ceiling.
		{5, -1, 5},
	} {
		if got := KeysToWithdraw(tc.banked, tc.held); got != tc.want {
			t.Fatalf("KeysToWithdraw(banked=%d, held=%d) = %d, want %d", tc.banked, tc.held, got, tc.want)
		}
	}
}
