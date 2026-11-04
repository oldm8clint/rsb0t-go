package combatplan

import "testing"

// The owner's melee spread (7 Oct 2026): Strength 5 above Attack, Attack 5 above Defence.
func TestBalancedMeleeKeepsTheOffsets(t *testing.T) {
	cases := []struct {
		att, str, def int
		want          Skill
	}{
		{1, 1, 1, SkillStrength},    // Strength is under Attack+5
		{1, 6, 1, SkillAttack},      // Strength is there; Attack is under Defence+5
		{6, 6, 1, SkillStrength},    // Attack is up: Strength must follow
		{6, 11, 1, SkillDefence},    // balanced: the cycle starts with Defence
		{6, 11, 2, SkillAttack},     // Defence rose: Attack follows
		{7, 11, 2, SkillStrength},   // then Strength
		{40, 46, 35, SkillAttack},   // Strength over Attack+5 wants Attack, whatever Defence is
		{50, 55, 30, SkillDefence},  // Attack far over Defence+5, Strength matched: Defence
		{30, 20, 30, SkillStrength}, // Strength far under: Strength, the damage stat
		{45, 50, 40, SkillDefence},  // exactly balanced
		{45, 49, 40, SkillStrength}, // one under on Strength
		{44, 50, 40, SkillAttack},   // one under on Attack (e2<0) while Strength is over Attack+5: Attack
	}
	for _, c := range cases {
		if got := BalancedMelee(c.att, c.str, c.def); got != c.want {
			t.Errorf("attack %d strength %d defence %d: got %s, want %s", c.att, c.str, c.def, got, c.want)
		}
	}
}

// Following the rule from 1/1/1 climbs the three together at the offsets, and never leaves them by more than a level.
func TestBalancedMeleeClimbsAtTheOffsets(t *testing.T) {
	att, str, def := 1, 1, 1
	for i := 0; i < 600; i++ {
		switch BalancedMelee(att, str, def) {
		case SkillAttack:
			att++
		case SkillStrength:
			str++
		default:
			def++
		}
		if i > 60 { // past the start-up, where Strength runs to its offset first
			if d := str - (att + 5); d < -1 || d > 1 {
				t.Fatalf("step %d: attack %d strength %d defence %d: Strength is %+d off Attack+5", i, att, str, def, d)
			}
			if d := att - (def + 5); d < -1 || d > 1 {
				t.Fatalf("step %d: attack %d strength %d defence %d: Attack is %+d off Defence+5", i, att, str, def, d)
			}
		}
	}
	if def < 100 || att < def+4 || str < att+4 {
		t.Fatalf("after 600 levels: attack %d strength %d defence %d", att, str, def)
	}
}

func TestBalancedStatToTrainHonoursGoals(t *testing.T) {
	g := Goals{Attack: 40, Strength: 40, Defence: 40}
	// Strength at its goal while the rule wants it: the stat furthest from its goal is trained instead.
	l := Levels{Attack: 20, Strength: 40, Defence: 10}
	if s, ok := BalancedStatToTrain(l, g); !ok || s == SkillStrength {
		t.Fatalf("got %s %v: Strength is at its goal", s, ok)
	}
	// Unmet goals follow the rule.
	if s, ok := BalancedStatToTrain(Levels{Attack: 10, Strength: 12, Defence: 10}, g); !ok || s != SkillStrength {
		t.Fatalf("got %s %v, want strength (12 is under 10+5)", s, ok)
	}
	if _, ok := BalancedStatToTrain(Levels{Attack: 40, Strength: 40, Defence: 40}, g); ok {
		t.Fatal("every goal met still wants a stat")
	}
}
