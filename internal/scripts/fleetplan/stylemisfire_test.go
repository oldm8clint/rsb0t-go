package fleetplan

import "testing"

// TestMeleeStyleMisfire pins the check that caught lab arm dx1 (giantsfix D6(c), 2026-09-26):
// the Cows phase logged "training defence" for 38 minutes while every fight paid Strength
// (melee 26/25/25 -> 26/52/25), so the Cows-to-trainer hand-off at 30/30/30 never came.
func TestMeleeStyleMisfire(t *testing.T) {
	cases := []struct {
		name          string
		want          MeleeSkill
		before, after MeleeXP
		paid          MeleeSkill
		misfired      bool
	}{
		{"dx1: defence chosen, strength paid", MeleeDefence, MeleeXP{9730, 10692, 8740}, MeleeXP{9730, 11332, 8740}, MeleeStrength, true},
		{"defence chosen, defence paid", MeleeDefence, MeleeXP{9730, 10692, 8740}, MeleeXP{9730, 10692, 9380}, "", false},
		{"attack chosen, strength paid", MeleeAttack, MeleeXP{100, 100, 100}, MeleeXP{100, 180, 100}, MeleeStrength, true},
		{"a miss pays nothing", MeleeStrength, MeleeXP{100, 100, 100}, MeleeXP{100, 100, 100}, "", false},
		{"controlled pays all three, the wanted one included", MeleeDefence, MeleeXP{100, 100, 100}, MeleeXP{140, 140, 140}, "", false},
		{"no reading yet", MeleeDefence, MeleeXP{}, MeleeXP{}, "", false},
	}
	for _, c := range cases {
		paid, misfired := MeleeStyleMisfire(c.want, c.before, c.after)
		if paid != c.paid || misfired != c.misfired {
			t.Errorf("%s: MeleeStyleMisfire = (%q, %v), want (%q, %v)", c.name, paid, misfired, c.paid, c.misfired)
		}
	}
}
