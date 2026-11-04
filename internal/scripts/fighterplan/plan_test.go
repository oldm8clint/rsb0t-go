package fighterplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestTakenByAnother(t *testing.T) {
	cases := []struct {
		name                                      string
		isOurs, inCombat, targetsMe, targetsOther bool
		want                                      bool
	}{
		{"ours is never taken", true, true, false, true, false},
		{"free npc", false, false, false, false, false},
		{"fighting someone else", false, false, false, true, true},
		{"in combat but not with us", false, true, false, false, true},
		{"in combat with us", false, true, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TakenByAnother(c.isOurs, c.inCombat, c.targetsMe, c.targetsOther); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestEligibleTarget(t *testing.T) {
	anchor := tile.Tile{X: 0, Z: 0, Level: 0}
	base := TargetContext{Anchor: anchor, Leash: 5, SkipUntil: map[int]int64{}, NowMs: 1000}

	t.Run("out of leash", func(t *testing.T) {
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 10, Z: 10, Level: 0}}
		if EligibleTarget(rec, base) {
			t.Fatal("expected ineligible")
		}
	})
	t.Run("wrong level", func(t *testing.T) {
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1, Level: 1}}
		if EligibleTarget(rec, base) {
			t.Fatal("expected ineligible")
		}
	})
	t.Run("skipped", func(t *testing.T) {
		ctx := base
		ctx.SkipUntil = map[int]int64{1: 5000}
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}}
		if EligibleTarget(rec, ctx) {
			t.Fatal("expected ineligible while skipped")
		}
	})
	t.Run("skip expired", func(t *testing.T) {
		ctx := base
		ctx.SkipUntil = map[int]int64{1: 500}
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}}
		if !EligibleTarget(rec, ctx) {
			t.Fatal("expected eligible once skip window passed")
		}
	})
	t.Run("dead body (health 0 of nonzero total)", func(t *testing.T) {
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}, TotalHealth: 10, Health: 0}
		if EligibleTarget(rec, base) {
			t.Fatal("expected ineligible")
		}
	})
	t.Run("taken by another player, not attacking others", func(t *testing.T) {
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}, InCombat: true, TargetsMe: false}
		if EligibleTarget(rec, base) {
			t.Fatal("expected ineligible")
		}
	})
	t.Run("taken by another player, allowed", func(t *testing.T) {
		ctx := base
		ctx.AttackOthersTargets = true
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}, InCombat: true, TargetsMe: false}
		if !EligibleTarget(rec, ctx) {
			t.Fatal("expected eligible")
		}
	})
	t.Run("max distance filter", func(t *testing.T) {
		ctx := base
		ctx.MaxDistance = 2
		rec := TargetRecord{Index: 1, Tile: tile.Tile{X: 1, Z: 1}, Distance: 3}
		if EligibleTarget(rec, ctx) {
			t.Fatal("expected ineligible past max distance")
		}
	})
}

func TestScoreTargetAndPickTarget(t *testing.T) {
	ctx := TargetContext{
		Anchor: tile.Tile{}, Leash: 20, SkipUntil: map[int]int64{}, NowMs: 0,
		PreferAttackers: true,
	}
	near := TargetRecord{Index: 1, Tile: tile.Tile{X: 1}, Distance: 1}
	attacker := TargetRecord{Index: 2, Tile: tile.Tile{X: 5}, Distance: 5, TargetsMe: true}
	worn := TargetRecord{Index: 3, Tile: tile.Tile{X: 1}, Distance: 1, TotalHealth: 10, Health: 1}

	best, ok := PickTarget([]TargetRecord{near, attacker, worn}, ctx)
	if !ok || best.Index != 2 {
		t.Fatalf("expected the attacker to win regardless of distance, got %+v ok=%v", best, ok)
	}

	// Without preferAttackers, the nearest/most-worn-down free npc wins over a farther one.
	ctx.PreferAttackers = false
	best, ok = PickTarget([]TargetRecord{near, attacker}, ctx)
	if !ok || best.Index != 1 {
		t.Fatalf("expected the nearer npc, got %+v", best)
	}

	if _, ok := PickTarget(nil, ctx); ok {
		t.Fatal("expected no target from an empty list")
	}
}

// TestPickTargetSkipsTheNearestWhenItHasNoRoute is the bug this gate exists for, reduced to the
// pure decision: the nearest candidate is the one a wall is in front of, and the picker has to
// hand back the further one it can actually walk to rather than the closer one it cannot.
func TestPickTargetSkipsTheNearestWhenItHasNoRoute(t *testing.T) {
	ctx := TargetContext{Anchor: tile.Tile{}, Leash: 20, SkipUntil: map[int]int64{}, NowMs: 0}
	walled := TargetRecord{Index: 1, Tile: tile.Tile{X: 1}, Distance: 1}
	open := TargetRecord{Index: 2, Tile: tile.Tile{X: 6}, Distance: 6}

	if best, ok := PickTarget([]TargetRecord{walled, open}, ctx); !ok || best.Index != 1 {
		t.Fatalf("the control failed: without a reachability gate the nearest must win, got %+v ok=%v", best, ok)
	}

	ctx.Reachable = func(r TargetRecord) bool { return r.Index != 1 }
	best, ok := PickTarget([]TargetRecord{walled, open}, ctx)
	if !ok || best.Index != 2 {
		t.Fatalf("expected the further candidate with a route, got %+v ok=%v", best, ok)
	}
}

// TestPickTargetRefusesWhenNothingHasARoute pins the answer that stops the wedge: with every
// candidate walled off the picker says so, rather than handing back the nearest one anyway for
// the caller to click through a wall.
func TestPickTargetRefusesWhenNothingHasARoute(t *testing.T) {
	ctx := TargetContext{
		Anchor: tile.Tile{}, Leash: 20, SkipUntil: map[int]int64{}, NowMs: 0,
		Reachable: func(TargetRecord) bool { return false },
	}
	list := []TargetRecord{{Index: 1, Tile: tile.Tile{X: 1}, Distance: 1}, {Index: 2, Tile: tile.Tile{X: 2}, Distance: 2}}
	if best, ok := PickTarget(list, ctx); ok {
		t.Fatalf("expected no target when nothing is reachable, got %+v", best)
	}
}

// TestPickTargetKeepsEveryExistingPreferenceUnderTheGate guards against the gate quietly turning
// the picker into "nearest reachable": an attacker still outranks a closer free NPC, and our own
// held target still outranks an equal stranger.
func TestPickTargetKeepsEveryExistingPreferenceUnderTheGate(t *testing.T) {
	ctx := TargetContext{
		Anchor: tile.Tile{}, Leash: 20, SkipUntil: map[int]int64{}, NowMs: 0,
		PreferAttackers: true,
		Reachable:       func(TargetRecord) bool { return true },
	}
	near := TargetRecord{Index: 1, Tile: tile.Tile{X: 1}, Distance: 1}
	attacker := TargetRecord{Index: 2, Tile: tile.Tile{X: 5}, Distance: 5, TargetsMe: true}
	if best, ok := PickTarget([]TargetRecord{near, attacker}, ctx); !ok || best.Index != 2 {
		t.Fatalf("the attacker should still win with the gate open, got %+v ok=%v", best, ok)
	}

	ctx.PreferAttackers = false
	ctx.HaveOwn, ctx.OwnIndex = true, 3
	ours := TargetRecord{Index: 3, Tile: tile.Tile{X: 1}, Distance: 1}
	stranger := TargetRecord{Index: 4, Tile: tile.Tile{X: 1}, Distance: 1}
	if best, ok := PickTarget([]TargetRecord{stranger, ours}, ctx); !ok || best.Index != 3 {
		t.Fatalf("our own held target should still win a tie, got %+v ok=%v", best, ok)
	}
}

// TestPickTargetAsksReachabilityOnlyUntilItFindsOne is what keeps the gate cheap enough to run
// several times a minute: each ask is a bounded A* search, so the healthy case (the best
// candidate has a route) must cost exactly one.
func TestPickTargetAsksReachabilityOnlyUntilItFindsOne(t *testing.T) {
	asked := 0
	ctx := TargetContext{
		Anchor: tile.Tile{}, Leash: 20, SkipUntil: map[int]int64{}, NowMs: 0,
		Reachable: func(TargetRecord) bool { asked++; return true },
	}
	list := []TargetRecord{
		{Index: 1, Tile: tile.Tile{X: 1}, Distance: 1},
		{Index: 2, Tile: tile.Tile{X: 2}, Distance: 2},
		{Index: 3, Tile: tile.Tile{X: 3}, Distance: 3},
	}
	if _, ok := PickTarget(list, ctx); !ok {
		t.Fatal("expected a target")
	}
	if asked != 1 {
		t.Fatalf("asked reachability %d times for a healthy pick, want 1", asked)
	}
}

// TestPickTargetNeverAsksAboutAnIneligibleCandidate keeps the ordering right: a skipped, dead or
// out-of-leash NPC is rejected before it can cost a pathfinder search.
func TestPickTargetNeverAsksAboutAnIneligibleCandidate(t *testing.T) {
	var seen []int
	ctx := TargetContext{
		Anchor: tile.Tile{}, Leash: 5, SkipUntil: map[int]int64{9: 5000}, NowMs: 1000,
		Reachable: func(r TargetRecord) bool { seen = append(seen, r.Index); return true },
	}
	list := []TargetRecord{
		{Index: 9, Tile: tile.Tile{X: 1}, Distance: 1},                  // skipped
		{Index: 8, Tile: tile.Tile{X: 50, Z: 50}, Distance: 50},         // out of leash
		{Index: 7, Tile: tile.Tile{X: 2}, Distance: 2, TotalHealth: 10}, // dead body
		{Index: 6, Tile: tile.Tile{X: 3}, Distance: 3},                  // the only real one
	}
	best, ok := PickTarget(list, ctx)
	if !ok || best.Index != 6 {
		t.Fatalf("expected the one eligible candidate, got %+v ok=%v", best, ok)
	}
	if len(seen) != 1 || seen[0] != 6 {
		t.Fatalf("reachability was asked about %v, want only [6]", seen)
	}
}

func TestEngagementStale(t *testing.T) {
	if EngagementStale(1000, 995, 8000) {
		t.Fatal("not stale yet")
	}
	if !EngagementStale(9000, 995, 8000) {
		t.Fatal("expected stale")
	}
}

func TestShouldPanic(t *testing.T) {
	if !ShouldPanic(0.1, 0.25, 0) {
		t.Fatal("expected panic: low hp, no food")
	}
	if ShouldPanic(0.1, 0.25, 1) {
		t.Fatal("food in hand — no panic")
	}
	if ShouldPanic(0.5, 0.25, 0) {
		t.Fatal("hp above panic threshold — no panic")
	}
}

func TestWantsLoot(t *testing.T) {
	f := LootFilter{Patterns: []string{"rune ", "uncut"}, BuryBones: true, Bones: []string{"Bones", "Big bones"}}
	if !WantsLoot("Rune scimitar", f) {
		t.Fatal("expected match on 'rune ' pattern")
	}
	if !WantsLoot("Uncut sapphire", f) {
		t.Fatal("expected match on 'uncut' pattern")
	}
	if !WantsLoot("Big bones", f) {
		t.Fatal("bones wanted whatever the loot list says when buryBones is set")
	}
	if WantsLoot("", f) {
		t.Fatal("empty name never wanted")
	}
	if WantsLoot("Logs", f) {
		t.Fatal("unrelated item not wanted")
	}
}

func TestIsBoneExactOnly(t *testing.T) {
	bones := []string{"Bones", "Big bones"}
	if IsBone("Big bones pile", bones) {
		t.Fatal("IsBone must match the whole name, not a substring")
	}
	if !IsBone("big bones", bones) {
		t.Fatal("expected case-insensitive exact match")
	}
}

func TestBoneFromKill(t *testing.T) {
	kills := []KillSite{{Tile: tile.Tile{X: 10, Z: 10, Level: 0}, AtMs: 1000}}
	// Within the 0..2 south-west offset window.
	if !BoneFromKill(tile.Tile{X: 9, Z: 8, Level: 0}, kills, 2000, 180_000) {
		t.Fatal("expected a kill-site match inside the offset window")
	}
	// Outside the window (too old).
	if BoneFromKill(tile.Tile{X: 9, Z: 8, Level: 0}, kills, 1000+200_000, 180_000) {
		t.Fatal("expected no match once the window has elapsed")
	}
	// Wrong direction (north-east of the kill tile, not south-west).
	if BoneFromKill(tile.Tile{X: 12, Z: 12, Level: 0}, kills, 1500, 180_000) {
		t.Fatal("expected no match outside the offset direction")
	}
}

func TestDecideBankReasonPriority(t *testing.T) {
	base := BankTriggerState{}

	afterDeath := base
	afterDeath.AfterDeath = true
	afterDeath.Requested = true // afterDeath still outranks "requested"
	if got := DecideBankReason(afterDeath); got != BankAfterDeath {
		t.Fatalf("got %v want after-death", got)
	}

	requested := base
	requested.Requested = true
	requested.GoalReached = true // requested outranks goal
	if got := DecideBankReason(requested); got != BankAskedFor {
		t.Fatalf("got %v want asked-for", got)
	}

	outOfFood := base
	outOfFood.FoodWanted = 10
	outOfFood.FoodCount = 0
	if got := DecideBankReason(outOfFood); got != BankOutOfFood {
		t.Fatalf("got %v want out-of-food", got)
	}

	outOfFoodButKnown := outOfFood
	outOfFoodButKnown.FoodKnownEmpty = true
	if got := DecideBankReason(outOfFoodButKnown); got != BankNone {
		t.Fatalf("got %v want none once the bank is known empty", got)
	}

	loot := base
	loot.AutoBank = true
	loot.LootSlots = 12
	loot.BankAtLootSlots = 12
	if got := DecideBankReason(loot); got != BankLoot {
		t.Fatalf("got %v want loot", got)
	}

	packFull := base
	packFull.AutoBank = true
	packFull.PackFull = true
	packFull.LootSlots = 1
	packFull.BankAtLootSlots = 12
	if got := DecideBankReason(packFull); got != BankPackFull {
		t.Fatalf("got %v want pack-full", got)
	}

	timer := base
	timer.AutoBank = true
	timer.LootSlots = 1
	timer.BankAtLootSlots = 12
	timer.BankEveryMinutes = 30
	timer.MinutesSinceBank = 31
	if got := DecideBankReason(timer); got != BankTimer {
		t.Fatalf("got %v want timer", got)
	}

	if got := DecideBankReason(base); got != BankNone {
		t.Fatalf("got %v want none", got)
	}
}

func TestRestockPlan(t *testing.T) {
	held := map[string]int{"Trout": 2}
	bank := map[string]int{"Trout": 100}
	wants := []RestockWant{{Name: "Trout", Qty: 10}}
	steps := RestockPlan(wants, func(n string) int { return held[n] }, func(n string) int { return bank[n] }, 20, func(string) bool { return false })
	if len(steps) != 1 || steps[0].Qty != 8 || steps[0].Short {
		t.Fatalf("unexpected steps: %+v", steps)
	}

	// Capped by free slots for a non-stackable want.
	steps = RestockPlan(wants, func(n string) int { return held[n] }, func(n string) int { return bank[n] }, 3, func(string) bool { return false })
	if len(steps) != 1 || steps[0].Qty != 3 || !steps[0].Short {
		t.Fatalf("expected a short restock capped by free slots, got %+v", steps)
	}

	// Bank short of what's wanted.
	poorBank := map[string]int{"Trout": 1}
	steps = RestockPlan(wants, func(n string) int { return held[n] }, func(n string) int { return poorBank[n] }, 20, func(string) bool { return false })
	if len(steps) != 1 || steps[0].Qty != 1 || !steps[0].Short {
		t.Fatalf("expected a short restock from a poor bank, got %+v", steps)
	}

	// Already satisfied: no step at all.
	satisfied := map[string]int{"Trout": 20}
	steps = RestockPlan(wants, func(n string) int { return satisfied[n] }, func(n string) int { return bank[n] }, 20, func(string) bool { return false })
	if len(steps) != 0 {
		t.Fatalf("expected no steps when already satisfied, got %+v", steps)
	}
}

func TestNextMeleeStyle(t *testing.T) {
	if got := NextMeleeStyle(MeleeLevels{Attack: 10, Strength: 20, Defence: 30}); got != StyleAttack {
		t.Fatalf("got %v want attack", got)
	}
	if got := NextMeleeStyle(MeleeLevels{Attack: 30, Strength: 10, Defence: 30}); got != StyleStrength {
		t.Fatalf("got %v want strength", got)
	}
	if got := NextMeleeStyle(MeleeLevels{Attack: 30, Strength: 30, Defence: 10}); got != StyleDefence {
		t.Fatalf("got %v want defence", got)
	}
	// A tie goes to the earlier rung: attack.
	if got := NextMeleeStyle(MeleeLevels{Attack: 20, Strength: 20, Defence: 20}); got != StyleAttack {
		t.Fatalf("got %v want attack on a tie", got)
	}
}

func TestParseLevelGoalAndGoalReached(t *testing.T) {
	skill, level, ok := ParseLevelGoal("attack:70")
	if !ok || skill != "attack" || level != 70 {
		t.Fatalf("got %q %d %v", skill, level, ok)
	}
	if _, _, ok := ParseLevelGoal("attack:1"); ok {
		t.Fatal("level 1 is below the accepted range")
	}
	if _, _, ok := ParseLevelGoal("nonsense"); ok {
		t.Fatal("expected no match")
	}

	g := Goal{LevelSkill: "attack", LevelTarget: 70}
	levels := map[string]int{"attack": 70}
	if got := GoalReached(g, func(s string) int { return levels[s] }, 0, 0); got == "" {
		t.Fatal("expected the level goal to be reached")
	}
	levels["attack"] = 60
	if got := GoalReached(g, func(s string) int { return levels[s] }, 0, 0); got != "" {
		t.Fatalf("expected not reached, got %q", got)
	}

	kg := Goal{Kills: 5}
	if got := GoalReached(kg, func(string) int { return 0 }, 5, 0); got == "" {
		t.Fatal("expected the kill goal reached")
	}

	mg := Goal{Minutes: 30}
	if got := GoalReached(mg, func(string) int { return 0 }, 0, 31); got == "" {
		t.Fatal("expected the minute goal reached")
	}
}

func TestSplitNames(t *testing.T) {
	got := SplitNames(" Goblin, Man ,, Cow ")
	want := []string{"Goblin", "Man", "Cow"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestDecideStagePriority(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		want Stage
	}{
		{"death first", Snapshot{Died: true, Halted: true}, StageDeathRecovery},
		{"halted before panic", Snapshot{Halted: true, PanicNeeded: true}, StageHalted},
		{"panic before eat", Snapshot{PanicNeeded: true, NeedEat: true}, StagePanicRetreat},
		{"eat before style", Snapshot{NeedEat: true, StyleWrong: true}, StageEat},
		{"style before gear", Snapshot{StyleWrong: true, GearMissing: true}, StageFixStyle},
		{"gear before bury", Snapshot{GearMissing: true, NeedBury: true}, StageEnsureGear},
		{"bury before bank", Snapshot{NeedBury: true, BankDue: true}, StageBuryBones},
		{"bank before loot", Snapshot{BankDue: true, LootAvailable: true}, StageBankRun},
		{"loot before fight", Snapshot{LootAvailable: true, HaveFight: true}, StageLoot},
		{"fight before return", Snapshot{HaveFight: true, AnchorFar: true}, StageFight},
		{"return when nothing else", Snapshot{AnchorFar: true}, StageReturnAnchor},
		{"idle otherwise", Snapshot{}, StageIdle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.s); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}
