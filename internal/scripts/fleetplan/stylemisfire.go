package fleetplan

// MeleeXP is the account's Attack, Strength and Defence experience at one reading.
type MeleeXP struct{ Attack, Strength, Defence int }

// MeleeStyleMisfire reports a fight that paid a different melee stat from the one the combat
// style was set for: want gained nothing while another melee stat gained. paid is the stat that
// gained (Strength first on a tie, the Cows' usual damage-first answer).
//
// The client's own reading of the style (com_mode against the combat tab) is not enough on its
// own. Lab arm dx1 (giantsfix D6(c), 2026-09-26) set Defence, saw com_mode confirm it, and then
// earned Strength on every fight for 38 minutes: melee 26/25/25 went to 26/52/25, the Cows phase
// never reached 30/30/30, and the trainer never started. The XP the fight paid is the evidence
// the server actually used; this is the check that reads it.
//
// A miss (nothing gained) is not a misfire, and a controlled style, which pays all three,
// includes the wanted stat.
func MeleeStyleMisfire(want MeleeSkill, before, after MeleeXP) (paid MeleeSkill, misfired bool) {
	gained := map[MeleeSkill]bool{
		MeleeAttack:   after.Attack > before.Attack,
		MeleeStrength: after.Strength > before.Strength,
		MeleeDefence:  after.Defence > before.Defence,
	}
	if gained[want] {
		return "", false
	}
	for _, s := range []MeleeSkill{MeleeStrength, MeleeAttack, MeleeDefence} {
		if gained[s] {
			return s, true
		}
	}
	return "", false
}
