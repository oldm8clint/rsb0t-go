package fleetplan

import (
	"math"
	"math/rand"
	"testing"
)

// TestHitChanceMatchesTheEngineRoll pins the closed form against the roll the engine actually
// makes — randominc(attack) > randominc(defence), both inclusive — rather than against a
// remembered formula. If someone "fixes" HitChance to the exclusive form this fails.
func TestHitChanceMatchesTheEngineRoll(t *testing.T) {
	rng := rand.New(rand.NewSource(20260920))
	for _, c := range [][2]int{{603, 430}, {804, 430}, {430, 430}, {100, 4000}, {0, 5}} {
		hits := 0
		const trials = 400000
		for i := 0; i < trials; i++ {
			if rng.Intn(c[0]+1) > rng.Intn(c[1]+1) {
				hits++
			}
		}
		got, want := HitChance(c[0], c[1]), float64(hits)/trials
		if math.Abs(got-want) > 0.004 {
			t.Errorf("attack %d vs defence %d: closed form %.4f, %d simulated rolls gave %.4f", c[0], c[1], got, trials, want)
		}
	}
}

// TestCowNumbersAreTheContentNumbers pins the two rolls a reader would otherwise have to take on
// trust, worked by hand from the content quoted in damage.go's package comment.
func TestCowNumbersAreTheContentNumbers(t *testing.T) {
	cow := CowTarget()
	// (defence 1 + 9) * (slash defence -21 + 64) = 10 * 43
	if got := NPCDefenceRoll(cow, SlashStyle); got != 430 {
		t.Errorf("cow defence roll = %d, content says (1+9)*(-21+64) = 430", got)
	}
	// (attack 1 + 8 + accurate 3) * (bronze sword slash attack 3 + 64) = 12 * 67
	if got := PlayerAttackRoll(BronzeSword, 1, StyleAccurate); got != 804 {
		t.Errorf("attack roll = %d, content says (1+8+3)*(3+64) = 804", got)
	}
	// ((strength 1 + 8 + aggressive 3) * (strength bonus 5 + 64) + 320) / 640 = (828+320)/640 = 1
	if got := PlayerMaxHit(BronzeSword, 1, StyleAggressive); got != 1 {
		t.Errorf("max hit = %d, content says (12*69+320)/640 = 1", got)
	}
}

// TestMaxHitIsAStepFunction is the fact the whole training rule turns on: several Strength levels
// in a row buy nothing at all, and then one buys a whole point. A per-level greedy cannot see this.
func TestMaxHitIsAStepFunction(t *testing.T) {
	steps := 0
	prev := PlayerMaxHit(BronzeSword, 1, StyleAggressive)
	for lvl := 2; lvl <= 30; lvl++ {
		if mh := PlayerMaxHit(BronzeSword, lvl, StyleAggressive); mh != prev {
			steps++
			prev = mh
		}
	}
	if steps >= 29 {
		t.Fatalf("max hit changed at every one of 29 levels, so it is not a step function here")
	}
	if steps == 0 {
		t.Fatalf("max hit never changed from strength 1 to 30, which cannot be right")
	}
}

// TestTrainingSkillNeverPicksDefence is the substance of the Stage 5 change, stated as a test so
// a later edit that reinstates the lowest-of-three rule fails here rather than silently.
func TestTrainingSkillNeverPicksDefence(t *testing.T) {
	for atk := 1; atk <= 40; atk++ {
		for str := 1; str <= 40; str++ {
			if got := TrainingSkill(BronzeSword, atk, str, 1); got == MeleeDefence {
				t.Fatalf("attack %d strength %d: chose Defence, which cannot raise damage", atk, str)
			}
		}
	}
}

// TestTrainingSkillBuysTheStrengthStep checks the rule really does look past the next level: at a
// Strength where the very next level buys nothing but a level three away buys a whole point of max
// hit, a naive greedy would choose Attack forever.
func TestTrainingSkillBuysTheStrengthStep(t *testing.T) {
	strengthPicks := 0
	for str := 1; str <= 30; str++ {
		if TrainingSkill(BronzeSword, 1, str, 1) == MeleeStrength {
			strengthPicks++
		}
	}
	if strengthPicks == 0 {
		t.Fatal("the rule never once chose Strength, which is the 26/1/1 failure the first attempt at this had")
	}
}
