package retaliateplan

import (
	"math"
	"testing"
)

var shortbowRapidNoAmmo = Config{AutoRetaliate: true, AttackRate: 4, Rapid: true, Ammo: false}

// two rats: attackrate 4 each, hitting two ticks apart, so one hit lands every other tick.
func twoRats() func(int) bool { return Union(EveryN(4, 1), EveryN(4, 3)) }

func steady(rolls []int, from int) []int { return Gaps(rolls[from:]) }

func allEqual(g []int, want int) bool {
	if len(g) == 0 {
		return false
	}
	for _, v := range g {
		if v != want {
			return false
		}
	}
	return true
}

func TestPlainChopIsFourTicks(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 80, func(int) bool { return false }, PolicyPlainClick())
	if s.Rolls[0] != 103 || !allEqual(Gaps(s.Rolls), 4) {
		t.Fatalf("plain: rolls %v, want first at click+3 then every 4", s.Rolls)
	}
}

func TestKnifeBatchIsThreeTicks(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 80, func(int) bool { return false }, PolicyKnife())
	if !allEqual(Gaps(s.Rolls), 3) {
		t.Fatalf("knife: gaps %v, want 3", Gaps(s.Rolls))
	}
}

func TestTwoRatsRapidShortbowRollsEveryTwoTicks(t *testing.T) {
	for _, clear := range []Op{OpClear, OpDrop} {
		s := Run(shortbowRapidNoAmmo, 120, twoRats(), PolicyRetaliate(clear))
		if !allEqual(steady(s.Rolls, 1), 2) {
			t.Fatalf("clear op %d: gaps %v, want 2 after the opening swing", clear, Gaps(s.Rolls))
		}
		if len(s.Shots) != 0 {
			t.Fatalf("no arrows were held but %d shots were fired", len(s.Shots))
		}
		if got := len(s.Rolls); got < 55 {
			t.Fatalf("only %d rolls in 120 ticks", got)
		}
	}
}

func TestTheLoopIsSelfSyncingWhateverTheHitParity(t *testing.T) {
	for phase := 0; phase < 4; phase++ {
		hits := Union(EveryN(4, phase), EveryN(4, phase+2))
		s := Run(shortbowRapidNoAmmo, 120, hits, PolicyRetaliate(OpClear))
		if !allEqual(steady(s.Rolls, 2), 2) {
			t.Fatalf("phase %d: gaps %v", phase, Gaps(s.Rolls))
		}
	}
}

func TestWithoutTheClearThereIsNoFlinch(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 80, twoRats(), PolicyPlainClick())
	if len(s.Flinches) != 0 || !allEqual(Gaps(s.Rolls), 4) {
		t.Fatalf("a chopping player has an interaction, so busy2 blocks the retaliate: flinches %v gaps %v", s.Flinches, Gaps(s.Rolls))
	}
}

func TestOneRatGivesNothing(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 120, EveryN(4, 1), PolicyRetaliate(OpClear))
	if !allEqual(Gaps(s.Rolls), 4) {
		t.Fatalf("one rat hits every 4 ticks: gaps %v, want plain 4", Gaps(s.Rolls))
	}
}

func TestTwoRatsOneTickApartAreNoBetterThanOne(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 120, Union(EveryN(4, 1), EveryN(4, 2)), PolicyRetaliate(OpClear))
	if !allEqual(Gaps(s.Rolls), 4) {
		t.Fatalf("hits at 1,2 mod 4: gaps %v, want 4 (the second hit of a pair is wasted)", Gaps(s.Rolls))
	}
}

func TestThreeSixTickHittersAlsoGiveTwo(t *testing.T) {
	g := Union(EveryN(6, 1), EveryN(6, 3), EveryN(6, 5))
	s := Run(shortbowRapidNoAmmo, 120, g, PolicyRetaliate(OpClear))
	if !allEqual(steady(s.Rolls, 1), 2) {
		t.Fatalf("three attackrate-6 monsters two ticks apart: gaps %v", Gaps(s.Rolls))
	}
}

func TestNonRapidFlinchIsTwoSoItNeedsHitsThreeApart(t *testing.T) {
	cfg := Config{AutoRetaliate: true, AttackRate: 4, Ammo: false}
	if Flinch(4, false) != 2 || Flinch(4, true) != 1 || Flinch(5, true) != 1 || Flinch(6, true) != 2 || Flinch(3, true) != 0 {
		t.Fatal("flinch is attackrate/2, minus 1 rapid")
	}
	three := Run(cfg, 120, EveryN(3, 1), PolicyRetaliate(OpClear))
	if !allEqual(steady(three.Rolls, 1), 3) {
		t.Fatalf("flinch 2, hits every 3: gaps %v, want 3", Gaps(three.Rolls))
	}
	two := Run(cfg, 120, twoRats(), PolicyRetaliate(OpClear))
	if len(two.Rolls) > 2 {
		t.Fatalf("flinch 2 with hits every 2 must not work, got %d rolls", len(two.Rolls))
	}
}

func TestAutoRetaliateOffKillsIt(t *testing.T) {
	cfg := shortbowRapidNoAmmo
	cfg.AutoRetaliate = false
	s := Run(cfg, 80, twoRats(), PolicyRetaliate(OpClear))
	if len(s.Rolls) != 0 {
		t.Fatalf("no retaliate, no flinch: %d rolls", len(s.Rolls))
	}
}

// lateClick drops the click for one hit and writes it two ticks after the hit instead of one.
func lateClick(base Policy, dropAt int) Policy {
	return func(s *Sim, hn, hp bool) []Op {
		switch s.Clock - 100 {
		case dropAt:
			return nil
		case dropAt + 1:
			return []Op{OpClickTree}
		}
		return base(s, hn, hp)
	}
}

func TestALateClickCostsSixTicksAndNoArrowsKeepsItHarmless(t *testing.T) {
	s := Run(shortbowRapidNoAmmo, 120, twoRats(), lateClick(PolicyRetaliate(OpClear), 22))
	if len(s.Shots) != 0 || len(s.Refused) < 2 {
		t.Fatalf("late click: shots %v refused %v", s.Shots, s.Refused)
	}
	worst := 0
	for _, g := range Gaps(s.Rolls) {
		if g > worst {
			worst = g
		}
	}
	if worst != 8 {
		t.Fatalf("a click one tick late should cost one 8-tick gap (the refused attack pushes the varp three ticks on), got %v", Gaps(s.Rolls))
	}
	// the same slip with arrows fires them
	armed := shortbowRapidNoAmmo
	armed.Ammo = true
	a := Run(armed, 120, twoRats(), lateClick(PolicyRetaliate(OpClear), 22))
	if len(a.Shots) == 0 {
		t.Fatal("with arrows the late click lets the retaliation fire")
	}
}

func TestAMissedClearCostsOneRoll(t *testing.T) {
	base := PolicyRetaliate(OpClear)
	p := func(s *Sim, hn, hp bool) []Op {
		if s.Clock-100 == 21 {
			return nil
		}
		return base(s, hn, hp)
	}
	s := Run(shortbowRapidNoAmmo, 120, twoRats(), p)
	fours := 0
	for _, g := range Gaps(s.Rolls) {
		switch g {
		case 4:
			fours++
		case 2:
		default:
			t.Fatalf("unexpected gap %d in %v", g, Gaps(s.Rolls))
		}
	}
	if fours != 1 {
		t.Fatalf("one missed clear should give one 4-tick gap, got %v", Gaps(s.Rolls))
	}
}

func TestNoAmmoAttackPushesTheVarpLater(t *testing.T) {
	s := New(shortbowRapidNoAmmo)
	s.AD = 0
	s.Tick(nil, true) // an idle player is hit: flinch to clock+1, attack the rat
	if len(s.Flinches) != 1 || s.AD != 101 {
		t.Fatalf("flinch: AD %d flinches %v", s.AD, s.Flinches)
	}
	s.Tick(nil, false) // AD == clock: the attack runs, no ammo
	if len(s.Refused) != 1 || s.AD != 101+3 || s.Busy2() {
		t.Fatalf("refusal: AD %d refused %v busy2 %v (want AD = clock+4-1 and the interaction stopped)", s.AD, s.Refused, s.Busy2())
	}
}

func TestRollChanceMatchesTheContentTable(t *testing.T) {
	cases := []struct {
		tree  Tree
		level int
		want  float64
	}{
		{Trees[0], 99, 1}, {Trees[0], 70, 1}, {Trees[0], 60, 255.0 / 256}, {Trees[0], 40, 207.0 / 256},
		{Trees[1], 99, 176.0 / 256}, {Trees[1], 80, 152.0 / 256},
		{Trees[2], 99, 88.0 / 256},
		{Trees[3], 99, 45.0 / 256}, {Trees[3], 80, 38.0 / 256},
		{Trees[4], 99, 22.0 / 256},
	}
	for _, c := range cases {
		got := RollChance(c.level, c.tree.Low, c.tree.High)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s L%d: %.5f want %.5f", c.tree.Name, c.level, got, c.want)
		}
	}
}

func TestUptimeAndXP(t *testing.T) {
	// Three Falador oaks at two ticks a roll: 16 ticks to fell, 28 to respawn (30 x (4000-300)/4000), 4 to swap: no waiting.
	if u := Uptime(3, 16, 4, 28); math.Abs(u-16.0*3/60) > 1e-9 {
		t.Fatalf("three oaks: uptime %v", u)
	}
	// Two oaks reachable from one tile: the chopper waits for the first to come back.
	if u := Uptime(2, 16, 1, 28); math.Abs(u-32.0/45) > 1e-9 {
		t.Fatalf("two oaks: uptime %v", u)
	}
	if x := XPPerHour(37.5, 1, 2, 1); x != 112500 {
		t.Fatalf("oak at two ticks, no losses: %v", x)
	}
	if x := XPPerHour(37.5, 1, 4, 1); x != 56250 {
		t.Fatalf("oak plain: %v", x)
	}
}

func TestMonsterNumbers(t *testing.T) {
	// rat: strength 1, strengthbonus -53 gives max hit 0; imp -37 gives 0; a goblin (strength 1, bonus 0) gives 1.
	if MonsterMaxHit(1, -53) != 0 || MonsterMaxHit(1, -37) != 0 || MonsterMaxHit(1, 0) != 1 || MonsterMaxHit(3, 0) != 1 {
		t.Fatal("max hits")
	}
	a := MonsterAttackRoll(1, 0)
	if HitChance(a, PlayerDefenceRoll(60)) >= HitChance(a, PlayerDefenceRoll(1)) {
		t.Fatal("more defence must lower the hit chance")
	}
	if p := HitChance(a, PlayerDefenceRoll(1)); p < 0.45 || p > 0.55 {
		t.Fatalf("equal attack and defence rolls: %v", p)
	}
}
