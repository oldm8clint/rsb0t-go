package fleetplan

import (
	"rsb0t-go/internal/scripts/combatplan"
)

// Stat indices (UPDATE_STAT), matching gamestate's names table.
const (
	AttackStat    = 0
	DefenceStat   = 1
	StrengthStat  = 2
	HitpointsStat = 3
)

// MeleeSkill is a melee stat Stage 5 can train through the weapon's combat style.
type MeleeSkill string

const (
	MeleeAttack   MeleeSkill = "attack"
	MeleeStrength MeleeSkill = "strength"
	MeleeDefence  MeleeSkill = "defence"
)

// BalancedMeleeSkill is the melee stat that keeps Strength 5 above Attack and Attack 5 above Defence (owner, 7
// Oct 2026; combatplan.BalancedMelee has the rule), what the Cow trainer's "lowest of the three" mode trains
// now. NextMeleeSkill stays for the model that prices the old rule.
func BalancedMeleeSkill(attack, strength, defence int) MeleeSkill {
	switch combatplan.BalancedMelee(attack, strength, defence) {
	case combatplan.SkillAttack:
		return MeleeAttack
	case combatplan.SkillDefence:
		return MeleeDefence
	}
	return MeleeStrength
}
