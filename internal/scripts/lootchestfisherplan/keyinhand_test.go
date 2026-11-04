package lootchestfisherplan

import (
	"math/rand"
	"testing"
)

// content's crystal_chest.rs2, in the order it runs: the key is deleted, THEN the reward is added.
// This reads it from a Content checkout when RSB0T_CONTENT names one, so the rule below cannot
// outlive the script it is derived from.
func TestContentDeletesTheKeyBeforeTheReward(t *testing.T) {
	root := contentRoot(t)
	b, err := readFileIfAny(root + "/scripts/areas/area_taverly/scripts/crystal_chest.rs2")
	if err != nil {
		t.Skipf("no crystal_chest.rs2 under %s: %v", root, err)
	}
	src := string(b)
	del := indexOf(src, "inv_del(inv, crystal_key, 1);")
	reward := indexOf(src, "@crystal_chest_reward;")
	add := indexOf(src, "inv_add(inv, uncut_dragonstone, 1);")
	if del < 0 || reward < 0 || add < 0 {
		t.Fatalf("the script no longer has the lines this rule is derived from:\n%s", src)
	}
	if !(del < reward && reward < add) {
		t.Errorf("the key is no longer deleted before the reward is added (inv_del at %d, the reward call at %d, the first inv_add at %d): CanOpenWithKeyInHand is wrong", del, reward, add)
	}
}

// simulateKitTrip is simulateTrip for a pack that already carries a kit: the stacks in kit (one slot
// each, whatever they hold) and extra unstackable slots (Strange fruit), opening only while rule says
// there is room. Overflow is judged against the real pack: after the key's slot is freed and the
// reward added, the pack holds more than 28.
func simulateKitTrip(rng *rand.Rand, k int, kit []int, extra int, rule func(free int, held func(id int) int) bool) (opened int, overflowed bool) {
	held := map[int]int{}
	for _, id := range kit {
		held[id] = 1
	}
	stacks := stackableIDs()
	heldCount := func(id int) int { return held[id] }
	slots := func() int {
		n := extra
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
		if !rule(free, heldCount) {
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
		if slots() > PackSlots-(keysLeft-1) {
			return opened, true
		}
	}
	return opened, false
}

// The exact rule never lets a reward overflow, and opens more of a trip's six keys than the rule
// that did not count the key's slot, over the real drop table with the kit a chest round carries
// (water, law, the fare), without and with two Strange fruit in the pack. The fruit's slots cost the
// loot room, which is why the chest round eats them for room when that is what stands between an
// open and the next (ChestStepEat).
func TestKeyInHandRuleOpensMoreAndNeverOverflows(t *testing.T) {
	kit := []int{WaterRune, LawRune, Coins}
	const trials = 40000
	type result struct{ all, total int }
	run := func(extra int, rule func(free int, held func(id int) int) bool) result {
		rng := rand.New(rand.NewSource(7))
		var r result
		for i := 0; i < trials; i++ {
			opened, over := simulateKitTrip(rng, KeysPerTrip, kit, extra, rule)
			if over {
				t.Fatalf("a reward overflowed the pack (%d extra slots)", extra)
			}
			r.total += opened
			if opened == KeysPerTrip {
				r.all++
			}
		}
		return r
	}
	for _, extra := range []int{0, 2} {
		oldRule := run(extra, CanOpenAnother)
		exact := run(extra, CanOpenWithKeyInHand)
		allOld, allNew := float64(oldRule.all)/trials, float64(exact.all)/trials
		meanOld, meanNew := float64(oldRule.total)/trials, float64(exact.total)/trials
		t.Logf("%d extra slots: old rule opens all six on %.1f%% of trips (mean %.2f), exact rule on %.1f%% (mean %.2f)", extra, allOld*100, meanOld, allNew*100, meanNew)
		if allNew < allOld+0.05 || meanNew < meanOld+0.15 {
			t.Errorf("%d extra slots: the exact rule opens all six on %.1f%% of trips (mean %.2f) against %.1f%% (mean %.2f) for the old rule", extra, allNew*100, meanNew, allOld*100, meanOld)
		}
		if extra == 0 && allNew < 0.60 {
			t.Errorf("all six keys opened on only %.1f%% of trips", allNew*100)
		}
	}
}

// Free slots count the key being spent: the rule is one slot looser than CanOpenAnother, no more.
func TestCanOpenWithKeyInHand(t *testing.T) {
	empty := func(int) int { return 0 }
	worst := WorstCaseNewSlots(empty)
	if CanOpenWithKeyInHand(worst-2, empty) || !CanOpenWithKeyInHand(worst-1, empty) || !CanOpenWithKeyInHand(worst, empty) {
		t.Errorf("worst case %d: the rule must be free+1 >= worst", worst)
	}
	for free := -2; free <= 14; free++ {
		if CanOpenAnother(free, empty) && !CanOpenWithKeyInHand(free, empty) {
			t.Errorf("free %d: the exact rule refused what the old one allowed", free)
		}
		if CanOpenWithKeyInHand(free, empty) != CanOpenAnother(free+1, empty) {
			t.Errorf("free %d: the exact rule is the old rule one slot up", free)
		}
	}
	// With every stack held the worst case is the five swordfish (and the dragonstone).
	allHeld := func(id int) int { return 1 }
	if got := WorstCaseNewSlots(allHeld); got != 6 {
		t.Fatalf("worst with every stack held: %d", got)
	}
	if CanOpenWithKeyInHand(4, allHeld) || !CanOpenWithKeyInHand(5, allHeld) {
		t.Error("with every stack held, five free slots plus the key's make six")
	}
}
