package combatplan

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestSpreadLadderKeepsMeleeLadderAndAddsThree(t *testing.T) {
	base, spread := MeleeLadder(), SpreadLadder()
	if len(spread) != len(base)+3+len(westRungs) {
		t.Fatalf("spread ladder has %d rungs, want %d + 3 + %d west", len(spread), len(base), len(westRungs))
	}
	for i, r := range base {
		if spread[i].Key != r.Key {
			t.Fatalf("rung %d is %q, want the MeleeLadder order kept (%q)", i, spread[i].Key, r.Key)
		}
	}
	for _, key := range []string{"barbarians", "whiteknights", "icewarriors"} {
		if _, ok := RungByKey(spread, key); !ok {
			t.Fatalf("spread ladder lacks %q", key)
		}
		if _, ok := RungByKey(base, key); ok {
			t.Fatalf("MeleeLadder must not gain %q: it is the pinned, unchanged ladder", key)
		}
	}
}

func TestSpreadRungsAreSoundRungs(t *testing.T) {
	for _, key := range []string{"barbarians", "whiteknights", "icewarriors"} {
		r, _ := RungByKey(SpreadLadder(), key)
		if r.Members {
			t.Fatalf("%s must not need a members world", key)
		}
		if r.SpawnCount() < 8 {
			t.Fatalf("%s lists %d spawns, want at least 8", key, r.SpawnCount())
		}
		if r.FromLevel >= r.ToLevel || r.MinCombatLevel < 1 {
			t.Fatalf("%s has a bad band %d-%d / combat %d", key, r.FromLevel, r.ToLevel, r.MinCombatLevel)
		}
		inLeash := 0
		for _, s := range r.Spawns {
			if tile.Chebyshev(r.Stand, s) <= r.Leash {
				inLeash++
			}
			if tile.Chebyshev(r.Stand, s) > 25 {
				t.Fatalf("%s spawn %v is %d tiles from the stand", key, s, tile.Chebyshev(r.Stand, s))
			}
		}
		if inLeash != r.SpawnCount() {
			t.Fatalf("%s: only %d of %d listed spawns are inside the %d-tile leash, so the count overstates the supply", key, inLeash, r.SpawnCount(), r.Leash)
		}
		reach := 0
		for _, s := range r.Spawns {
			if tile.Chebyshev(r.Stand, s) <= r.Monster.DisengageTiles+r.Leash {
				reach++
			}
		}
		if reach == 0 {
			t.Fatalf("%s: no spawn within reach of the stand", key)
		}
		if r.Monster.BoneItem() != "Bones" {
			t.Fatalf("%s monster bone = %q, want plain Bones (death_drop default)", key, r.Monster.BoneItem())
		}
		if r.Bank == (tile.Tile{}) || r.Entrance == "" || r.Note == "" {
			t.Fatalf("%s needs a bank, an entrance and a note", key)
		}
	}
}

func TestSpreadLadderCoversEveryLevelFiveTo74(t *testing.T) {
	for level := 5; level < 75; level++ {
		ok := false
		for _, r := range SpreadLadder() {
			if level >= r.FromLevel && level < r.ToLevel && r.TravelProven && !r.Members {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("no free proven rung at lowest-melee %d", level)
		}
	}
}

func TestBoneItemAndWantBones(t *testing.T) {
	cases := []struct {
		m     Monster
		item  string
		pray  int
		goal  int
		wants bool
	}{
		{MonsterGiant, "Big bones", 50, 37, true}, // big bones are always taken, as before
		{MonsterIceWarrior, "Bones", 30, 37, true},
		{MonsterIceWarrior, "Bones", 37, 37, false}, // the goal is met: not worth a pickup walk
		{MonsterWhiteKnight, "Bones", 1, 0, false},  // no goal set
		{MonsterCow, "Bones", 5, 37, true},          // cows do drop bones: death_drop defaults to bones
		{MonsterWarrior, "Bones", 5, 37, true},
	}
	for _, c := range cases {
		if got := c.m.BoneItem(); got != c.item {
			t.Fatalf("%s bone item = %q, want %q", c.m.Name, got, c.item)
		}
		if got := WantBones(c.m, c.pray, c.goal); got != c.wants {
			t.Fatalf("WantBones(%s, prayer %d, goal %d) = %v, want %v", c.m.Name, c.pray, c.goal, got, c.wants)
		}
	}
	if (Monster{Name: "Rat"}).BoneItem() != "" || WantBones(Monster{Name: "Rat"}, 1, 40) {
		t.Fatal("a monster with no known bone must want none")
	}
}

// The supply ranking is the reason for the weights, so pin the shape: more hit points and more
// spawns feed more, a long respawn feeds less.
func TestSupplyRanking(t *testing.T) {
	giants, _ := RungByKey(SpreadLadder(), "giants")
	iw, _ := RungByKey(SpreadLadder(), "icewarriors")
	wk, _ := RungByKey(SpreadLadder(), "whiteknights")
	barb, _ := RungByKey(SpreadLadder(), "barbarians")
	alk, _ := RungByKey(SpreadLadder(), "alkharid")
	for _, r := range SpreadLadder() {
		t.Logf("%-22s %2d spawns  %6.0f XP/h per world  %5.0f per spawn", r.Key, r.SpawnCount(), r.SupplyXPH(), SupplyPerSpawnHour(r.Monster))
	}
	if !(iw.SupplyXPH() > giants.SupplyXPH()) {
		t.Fatalf("ice warriors (%.0f/h) should out-feed the giants (%.0f/h)", iw.SupplyXPH(), giants.SupplyXPH())
	}
	if !(giants.SupplyXPH() > wk.SupplyXPH()) {
		t.Fatalf("giants (%.0f/h) should out-feed the white knights (%.0f/h): 12 spawns against 8", giants.SupplyXPH(), wk.SupplyXPH())
	}
	if !(barb.SupplyXPH() < giants.SupplyXPH()) || !(barb.SupplyXPH() > alk.SupplyXPH()*0.8) {
		t.Fatalf("barbarians %.0f should sit near the Al-Kharid warriors %.0f and under the giants %.0f", barb.SupplyXPH(), alk.SupplyXPH(), giants.SupplyXPH())
	}
	// The measured case: 12 Giants shared by 35 accounts is about 2,700-3,700 XP an hour each, which
	// is what 2 Oct's log showed (3,228 mean).
	each := giants.SupplyXPH() / 35
	if each < 2000 || each > 4500 {
		t.Fatalf("model says %.0f XP/h each for 35 accounts on 12 giants, the fleet measured about 3,200", each)
	}
	if got := SupplyPerSpawnHour(Monster{Hitpoints: 60}); got <= 0 {
		t.Fatalf("a monster with no respawn ticks must default, got %v", got)
	}
}

func TestPickRungSpreadWithoutCrowdSplitsByWeight(t *testing.T) {
	counts := map[string]int{}
	const n = 3000
	for i := 0; i < n; i++ {
		c := PickRungSpread(SpreadLadder(), 45, 60, true, Crowd{Account: fmt.Sprintf("swarm%d", i), Hitpoints: iceWarriorHitpoints})
		if !c.OK {
			t.Fatalf("blocked: %s", c.Why)
		}
		counts[c.Rung.Key]++
	}
	// Band 45 holds giants, mossgiants (4 spawns), whiteknights and icewarriors (members rungs and
	// the unproven ones are out).
	for _, k := range []string{"giants", "whiteknights", "icewarriors", "mossgiants"} {
		if counts[k] == 0 {
			t.Fatalf("rung %s got nobody: %v", k, counts)
		}
	}
	if counts["mossgiants-brimhaven"]+counts["ogres"] != 0 {
		t.Fatalf("an unproven or members rung was picked: %v", counts)
	}
	giants, _ := RungByKey(SpreadLadder(), "giants")
	iw, _ := RungByKey(SpreadLadder(), "icewarriors")
	// The shares follow the weights: ice warriors/giants supply ratio, within sampling noise.
	want := iw.SupplyXPH() / giants.SupplyXPH()
	got := float64(counts["icewarriors"]) / float64(counts["giants"])
	if got < want*0.8 || got > want*1.25 {
		t.Fatalf("ice warriors/giants share ratio %.2f, want near %.2f (%v)", got, want, counts)
	}
	// And no rung hogs the swarm the way the old deepest-supply rule did.
	for k, v := range counts {
		if float64(v)/n > 0.55 {
			t.Fatalf("rung %s holds %.0f%% of the swarm: %v", k, 100*float64(v)/n, counts)
		}
	}
}

func TestPickRungSpreadIsStable(t *testing.T) {
	a := PickRungSpread(SpreadLadder(), 45, 60, true, Crowd{Account: "acct7"})
	b := PickRungSpread(SpreadLadder(), 45, 60, true, Crowd{Account: "ACCT7"})
	if a.Rung.Key != b.Rung.Key {
		t.Fatalf("the same account (any case) must always pick the same rung: %q vs %q", a.Rung.Key, b.Rung.Key)
	}
}

func TestPickRungSpreadFillsTheEmptiestRung(t *testing.T) {
	ladder := SpreadLadder()
	seat := map[string]int{}
	for i := 0; i < 90; i++ {
		c := PickRungSpread(ladder, 45, 60, true, Crowd{Account: fmt.Sprintf("a%d", i), Loads: copyLoads(seat), Hitpoints: iceWarriorHitpoints})
		seat[c.Rung.Key]++
	}
	giants, _ := RungByKey(ladder, "giants")
	iw, _ := RungByKey(ladder, "icewarriors")
	wk, _ := RungByKey(ladder, "whiteknights")
	if seat["giants"] == 0 || seat["icewarriors"] == 0 || seat["whiteknights"] == 0 {
		t.Fatalf("water-filling left a rung empty: %v", seat)
	}
	// Load per unit of supply is level across the three big rungs, within one account.
	ratio := func(r Rung) float64 { return float64(seat[r.Key]) / r.SupplyXPH() }
	lo, hi := ratio(giants), ratio(giants)
	for _, r := range []Rung{iw, wk} {
		x := ratio(r)
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	if hi-lo > 1.0/wk.SupplyXPH()+1e-9 {
		t.Fatalf("loads are not level against supply: %v", seat)
	}
	// Against the old rule: PickRung sends everyone to the one deepest rung.
	if PickRung(ladder, 45, 60, true).Rung.Key != "giants" {
		t.Fatal("PickRung must keep answering the deepest rung (the unchanged default)")
	}
}

func copyLoads(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestPickRungSpreadAvoidsStalledRungButNotAtAnyPrice(t *testing.T) {
	ladder := SpreadLadder()
	c := PickRungSpread(ladder, 45, 60, true, Crowd{Account: "x", Avoid: map[string]bool{"giants": true, "icewarriors": true, "whiteknights": true}})
	if c.Rung.Key != "mossgiants" {
		t.Fatalf("with three rungs avoided the last free one should win, got %q", c.Rung.Key)
	}
	// Everything usable avoided: the avoid list is advice, not a block.
	c = PickRungSpread(ladder, 45, 60, true, Crowd{Account: "x", Avoid: map[string]bool{"giants": true, "icewarriors": true, "whiteknights": true, "mossgiants": true}})
	if !c.OK {
		t.Fatalf("an avoid list must never leave the account with no rung: %v", c)
	}
}

func TestPickRungSpreadHonoursGatesAndBands(t *testing.T) {
	ladder := SpreadLadder()
	// Combat 20, lowest melee 30: ice warriors (combat 50) and white knights (30) are out, the
	// Al-Kharid and Barbarian bands ended at 25.
	if c := PickRungSpread(ladder, 30, 20, true, Crowd{Account: "x"}); c.OK {
		t.Fatalf("combat 20 must not reach any rung at melee 30, got %q", c.Rung.Key)
	} else if c.Why != BlockedCombatLevel {
		t.Fatalf("reason %q, want the combat gate", c.Why)
	}
	// Melee 12: alkharid and barbarians only.
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := PickRungSpread(ladder, 12, 12, true, Crowd{Account: fmt.Sprint("n", i)})
		if !c.OK {
			t.Fatalf("melee 12 blocked: %s", c.Why)
		}
		seen[c.Rung.Key] = true
	}
	if len(seen) != 2 || !seen["alkharid"] || !seen["barbarians"] {
		t.Fatalf("melee 12 picks %v, want alkharid and barbarians", seen)
	}
	// Free world: nothing members-only is ever picked, and nothing out of band.
	if c := PickRungSpread(ladder, 99, 126, true, Crowd{}); c.OK || c.Why != BlockedNoBand {
		t.Fatalf("melee 99 should have no band, got %v", c)
	}
}

func TestRungBoard(t *testing.T) {
	b := NewRungBoard()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	b.Sit("Acct1", 1, "giants", now)
	b.Sit("acct2", 1, "giants", now)
	b.Sit("acct3", 2, "giants", now)
	b.Sit("acct4", 1, "icewarriors", now.Add(-RungSeatTTL-time.Minute))
	b.Sit("acct5", 1, "whiteknights", now)
	got := b.Loads("acct5", 1, now)
	if got["giants"] != 2 || got["whiteknights"] != 0 || got["icewarriors"] != 0 {
		t.Fatalf("loads %v: want 2 giants in world 1, own seat and the expired one left out", got)
	}
	b.Sit("acct1", 1, "whiteknights", now) // moving replaces the seat
	if got := b.Loads("", 1, now); got["giants"] != 1 || got["whiteknights"] != 2 {
		t.Fatalf("after a move loads %v", got)
	}
	b.Leave("ACCT2")
	if got := b.Loads("", 1, now); got["giants"] != 0 {
		t.Fatalf("after leaving loads %v", got)
	}
	var nilBoard *RungBoard
	nilBoard.Sit("x", 1, "giants", now) // a nil board is a no-op, not a panic
	if len(nilBoard.Loads("x", 1, now)) != 0 {
		t.Fatal("nil board has no loads")
	}
}

func TestStallEndAndAvoidSet(t *testing.T) {
	if !StallEnd("no melee xp for 15m0s") || !StallEnd("  No melee XP for 16m") {
		t.Fatal("the stall end reason must be recognised")
	}
	if StallEnd("out of Trout: the pack is empty") || StallEnd("") {
		t.Fatal("other reasons are not stalls")
	}
	if !AvoidEnd("combattrainer: could not reach rung \"whiteknights\" (x) in 3 attempts") || !AvoidEnd("no melee xp for 15m") || AvoidEnd("out of Cake") {
		t.Fatal("AvoidEnd must cover stalls and failed walks and nothing else")
	}
	now := time.Now()
	set := AvoidSet(map[string]time.Time{"giants": now.Add(time.Minute), "icewarriors": now.Add(-time.Minute)}, now)
	if !set["giants"] || set["icewarriors"] {
		t.Fatalf("avoid set %v", set)
	}
	if AvoidSet(nil, now) != nil {
		t.Fatal("no avoids is a nil set")
	}
}

// A crowded band overflows to the rungs below it, and only then: at melee 30 the band's rungs are
// the Giants and the White Knights; with them empty an account picks one of them, with them full
// it picks Barbarians or Al Kharid, which a melee-30 account was never offered before.
func TestPickRungSpreadOverflowsACrowdedBandDownward(t *testing.T) {
	ladder := SpreadLadder()
	empty := Crowd{Account: "x", Hitpoints: 40, Loads: map[string]int{"barbarians": 0}}
	if c := PickRungSpread(ladder, 30, 45, true, empty); !c.OK || (c.Rung.Key != "giants" && c.Rung.Key != "whiteknights") {
		t.Fatalf("an empty band picked %+v, want the Giants or the White Knights", c)
	}
	crowded := Crowd{Account: "x", Hitpoints: 40, Loads: map[string]int{"giants": 30, "whiteknights": 20}}
	c := PickRungSpread(ladder, 30, 45, true, crowded)
	if !c.OK || (c.Rung.Key != "barbarians" && c.Rung.Key != "alkharid") {
		t.Fatalf("a crowded band picked %+v, want the Barbarians or Al Kharid below it", c)
	}
	if got, want := expectedXPH(c.Rung, crowded), expectedXPH(mustRung(t, ladder, "giants"), crowded); got <= want {
		t.Fatalf("the overflow rung expects %.0f XP/h, not more than the crowded Giants' %.0f", got, want)
	}
	// Without loads nothing changes: the band alone, by weight.
	for i := 0; i < 50; i++ {
		if c := PickRungSpread(ladder, 30, 45, true, Crowd{Account: fmt.Sprint("n", i), Hitpoints: 40}); c.Rung.Key == "barbarians" || c.Rung.Key == "alkharid" {
			t.Fatalf("with no loads an account overflowed to %q", c.Rung.Key)
		}
	}
	// Cows are never an overflow rung.
	for _, r := range overflowRungs(ladder, 60, 70, true, crowded) {
		if r.Key == "cows" {
			t.Fatal("the cow field was offered as overflow")
		}
	}
}

func mustRung(t *testing.T, ladder []Rung, key string) Rung {
	t.Helper()
	r, ok := RungByKey(ladder, key)
	if !ok {
		t.Fatalf("no rung %q", key)
	}
	return r
}

// iceWarriorHitpoints is the base Hitpoints the Ice warrior rung asks (spread.go): the rung is in the pool only
// for an account that has it.
const iceWarriorHitpoints = 60

// The Ice warrior cave killed nine ScarabSwarm accounts of 48 to 55 hitpoints in three days (the guardian could
// not leave an event monster there; see randomevents/event_leash.go). The rung is held back from them until they
// have 60, and it is the only Herd rung.
func TestIceWarriorsAreGatedAtSixtyHitpointsAndAreTheOnlyHerd(t *testing.T) {
	iw, ok := RungByKey(SpreadLadder(), "icewarriors")
	if !ok || !iw.Herd || iw.MinHitpoints != iceWarriorHitpoints {
		t.Fatalf("icewarriors = %+v (found %v), want a Herd rung gated at %d hitpoints", iw, ok, iceWarriorHitpoints)
	}
	acct := Account{LowestMelee: 45, CombatLevel: 60, Members: true}
	for _, hp := range []int{0, 40, 48, 54, 55, 59} {
		acct.Hitpoints = hp
		if iw.Admits(acct) {
			t.Errorf("an account of %d hitpoints is admitted to the Ice warrior cave", hp)
		}
	}
	acct.Hitpoints = iceWarriorHitpoints
	if !iw.Admits(acct) {
		t.Error("an account of 60 hitpoints is refused the Ice warrior cave")
	}
	for _, r := range SpreadLadderWith(SpreadOptions{Wilderness: true}) {
		if r.Herd && r.Key != "icewarriors" {
			t.Errorf("%s is a Herd rung: no bones are buried there (a big-bone rung exists to bury them)", r.Key)
		}
	}
	// The 48-55 hitpoint accounts that died are picked elsewhere in the band, at every crowd.
	for i := 0; i < 300; i++ {
		c := PickRungSpread(SpreadLadder(), 45, 60, true, Crowd{Account: fmt.Sprint("a", i), Hitpoints: 54})
		if !c.OK || c.Rung.Key == "icewarriors" {
			t.Fatalf("a 54 hitpoint account was given %q (ok %v)", c.Rung.Key, c.OK)
		}
	}
}

func TestHerdFoodFloorIsThreeTimesTheSingleAttackerFloorAndOnlyForAHerd(t *testing.T) {
	iw, _ := RungByKey(SpreadLadder(), "icewarriors")
	giants, _ := RungByKey(SpreadLadder(), "giants")
	const bass = 13
	if single := FoodPerTrip(iw, bass); single != 5 {
		t.Fatalf("control: the Ice warrior danger floor is %d bites of Bass, the live log said 5", single)
	}
	if got := HerdFoodFloor(iw, bass); got != 15 {
		t.Errorf("HerdFoodFloor(icewarriors, Bass) = %d, want 15 (the live trips came away with 11 and 5 of the 20)", got)
	}
	if got := HerdFoodFloor(iw, 6); got != FoodLoad(iw, 6, 28) {
		t.Errorf("a weak food: floor %d, want it capped at the trip load %d", got, FoodLoad(iw, 6, 28))
	}
	if got := HerdFoodFloor(giants, bass); got != 0 {
		t.Errorf("a rung that is not a herd has a herd floor of %d", got)
	}
	// The two trips that sent accounts in to die: 11 and 5 of the 20 the trip is sized for.
	for _, came := range []int{11, 5} {
		if came >= HerdFoodFloor(iw, bass) {
			t.Errorf("a trip of %d bites is no longer short of the herd floor %d", came, HerdFoodFloor(iw, bass))
		}
	}
}

func TestAvoidEndIncludesAHerdRungLeftForWantOfFood(t *testing.T) {
	if !AvoidEnd(HerdFoodShort + ": the bank could only give 11 Bass") {
		t.Error("a run that left a herd rung for want of food does not make the next pick avoid it")
	}
	if AvoidEnd("out of Bass: the pack is empty, the bank has none") {
		t.Error("an ordinary out-of-food end now avoids the rung")
	}
	if DeathAvoidFor < time.Hour {
		t.Errorf("a death is avoided for %s: the rung killed nine of nine accounts that went back", DeathAvoidFor)
	}
}
