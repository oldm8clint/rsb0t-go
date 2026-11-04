package fleetplan

import (
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/levels"
)

// MeleeStyle is a weapon's combat style, in terms of the +3/+1 style bonuses
// player_combat_stat.rs2 adds to the effective stat.
type MeleeStyle string

const (
	// StyleAccurate is +3 Attack: it raises the hit chance and nothing else.
	StyleAccurate MeleeStyle = "accurate"
	// StyleAggressive is +3 Strength: it raises the max hit and nothing else.
	StyleAggressive MeleeStyle = "aggressive"
	// StyleDefensive is +3 Defence, which buys no damage at all and is here only so the
	// comparison can price the old rule honestly.
	StyleDefensive MeleeStyle = "defensive"
	// StyleControlled is +1 to each.
	StyleControlled MeleeStyle = "controlled"
)

// attackBonus and strengthBonus are the style's contributions to the two effective stats.
func (m MeleeStyle) attackBonus() int {
	switch m {
	case StyleAccurate:
		return 3
	case StyleControlled:
		return 1
	}
	return 0
}

func (m MeleeStyle) strengthBonus() int {
	switch m {
	case StyleAggressive:
		return 3
	case StyleControlled:
		return 1
	}
	return 0
}

// DamageStyle is the damage type a roll is made against — Stage 5 only ever slashes.
type DamageStyle string

const SlashStyle DamageStyle = "slash"

// Weapon is the part of an obj config the damage arithmetic reads.
type Weapon struct {
	Name          string
	SlashAttack   int // param=slashattack
	StrengthBonus int // param=strengthbonus
	AttackRate    int // param=attackrate, in ticks
	AttackLevel   int // the level needed to wield it
	Cost          int // the obj's own cost= line, which is roughly the shop price
}

// The three weapons worth pricing for this stage: what it carries today, the best thing an
// Attack-1 account may wield, and the best thing an Attack-5 account may wield. Every field is
// from content/scripts/skill_combat/configs/melee/{swords,scimitars}.obj.
var (
	BronzeSword   = Weapon{Name: "Bronze sword", SlashAttack: 3, StrengthBonus: 5, AttackRate: 4, AttackLevel: 1, Cost: 26}
	IronScimitar  = Weapon{Name: "Iron scimitar", SlashAttack: 10, StrengthBonus: 9, AttackRate: 4, AttackLevel: 1, Cost: 112}
	SteelScimitar = Weapon{Name: "Steel scimitar", SlashAttack: 15, StrengthBonus: 14, AttackRate: 4, AttackLevel: 5, Cost: 400}
)

// NPCTarget is the part of an npc config the arithmetic reads.
type NPCTarget struct {
	Name          string
	DefenceLevel  int
	SlashDefence  int
	Hitpoints     int
	XPMultiplier  int // %npc_combat_xp_multiplier, 1 unless the npc says otherwise
	MaxHitAgainst int // what it hits the account for, for the survival side of the sum
}

// CowTarget is what Stage 5 fights.
//
// The facts combatplan already owns are TAKEN from combatplan.MonsterCow rather than restated, so
// the two cannot drift: its hitpoints and the max hit it deals back. What is added here is the
// pair of numbers combatplan.Monster does not carry and this arithmetic needs — the defence LEVEL
// and the slash defence BONUS — read from content/scripts/_unpack/225/all.npc's [cow], which has
// every defence bonus at -21 and no attack/strength/defence line at all, so all three default to 1.
//
// Those two fields arguably belong on combatplan.Monster, and this whole file arguably belongs in
// combatplan, since nothing in it is Scarab-specific. It is here because combatplan is claimed by
// AJ's "bladeproof"/"combat145" work; see this branch's COLLAB row.
func CowTarget() NPCTarget {
	c := combatplan.MonsterCow
	return NPCTarget{
		Name: c.Name, Hitpoints: c.Hitpoints, MaxHitAgainst: c.MaxHit,
		DefenceLevel: 1, SlashDefence: -21, XPMultiplier: 1,
	}
}

// NPCDefenceRoll is npc_defence_roll_specific: (defence level + 9) * (defence bonus + 64).
func NPCDefenceRoll(n NPCTarget, _ DamageStyle) int {
	return (n.DefenceLevel + 9) * (n.SlashDefence + 64)
}

// PlayerAttackRoll is combat_stat(effective attack, the weapon's slash attack).
func PlayerAttackRoll(w Weapon, attack int, style MeleeStyle) int {
	return (attack + 8 + style.attackBonus()) * (w.SlashAttack + 64)
}

// PlayerMaxHit is combat_maxhit(combat_stat(effective strength, the weapon's strength bonus)),
// and integer division is deliberate — it is what the engine does, and it is why a strength level
// can buy nothing for several levels and then a whole point of max hit at once.
func PlayerMaxHit(w Weapon, strength int, style MeleeStyle) int {
	return ((strength+8+style.strengthBonus())*(w.StrengthBonus+64) + 320) / 640
}

// HitChance is P(randominc(a) > randominc(d)) for the engine's own randominc, which is uniform on
// [0, n] INCLUSIVE — so the denominators are a+1 and d+1, not a and d.
//
// Closed form rather than a simulation, so the number is exact and a test can pin it:
//
//	P = (1 / ((a+1)(d+1))) * sum over x in [0,a] of min(x, d+1)
func HitChance(attackRoll, defenceRoll int) float64 {
	if attackRoll < 0 {
		attackRoll = 0
	}
	if defenceRoll < 0 {
		defenceRoll = 0
	}
	total := 0.0
	for x := 0; x <= attackRoll; x++ {
		below := x
		if below > defenceRoll+1 {
			below = defenceRoll + 1
		}
		total += float64(below)
	}
	return total / (float64(attackRoll+1) * float64(defenceRoll+1))
}

// DamageRate is one configuration's expected output.
type DamageRate struct {
	HitChance float64
	MaxHit    int
	// PerAttack is the mean damage of one attack, hits and misses together: randominc(maxhit) is
	// uniform on [0, maxhit], so a landed hit averages maxhit/2 and CAN still be a zero.
	PerAttack float64
	// PerTick is PerAttack spread over the weapon's attack rate.
	PerTick float64
	Ticks   int
}

// ExpectedDamage prices one weapon/level/style against one target.
func ExpectedDamage(w Weapon, attack, strength int, style MeleeStyle, n NPCTarget) DamageRate {
	p := HitChance(PlayerAttackRoll(w, attack, style), NPCDefenceRoll(n, SlashStyle))
	mh := PlayerMaxHit(w, strength, style)
	per := p * float64(mh) / 2
	rate := w.AttackRate
	if rate <= 0 {
		rate = 4
	}
	return DamageRate{HitChance: p, MaxHit: mh, PerAttack: per, PerTick: per / float64(rate), Ticks: rate}
}

// NextStepStyle is the policy this change ships, and it is the one that survived being priced.
//
// It looks past the single attack a naive greedy sees, because the max hit is a STEP function of
// Strength ((effective strength * (bonus+64) + 320) / 640, integer division): several Strength
// levels can buy nothing at all and then one buys a whole extra point of max hit, which is worth
// more than every Attack level between them. So for each candidate it measures the gain at the
// next point where that stat actually changes the arithmetic, divided by the XP that costs, and
// takes the better rate.
//
// Defence is never a candidate here. Hitpoints XP is paid only for damage DEALT, and the Cow this
// stage fights has no strength line at all in all.npc, so it hits 1: the Defence the shipped rule
// bought was protection the account did not need, paid for out of the only budget it had.
func NextStepStyle(w Weapon, attack, strength, _ int, n NPCTarget) MeleeStyle {
	// What it is worth right now under each style, which already includes the style bonus.
	nowAcc := ExpectedDamage(w, attack, strength, StyleAccurate, n).PerAttack
	nowAgg := ExpectedDamage(w, attack, strength, StyleAggressive, n).PerAttack

	// Attack moves the hit chance smoothly, so its next step is simply the next level.
	atkGain := ExpectedDamage(w, attack+1, strength, StyleAccurate, n).PerAttack - nowAcc
	atkCost := float64(levels.XPAtLevel(attack+1) - levels.XPAtLevel(attack))

	// Strength's next step is the next level at which the max hit actually changes; below it,
	// every level is worth exactly nothing and a per-level comparison would never buy any of them.
	strLevel, mh := strength, PlayerMaxHit(w, strength, StyleAggressive)
	for strLevel < 99 && PlayerMaxHit(w, strLevel+1, StyleAggressive) == mh {
		strLevel++
	}
	strGain := ExpectedDamage(w, attack, strLevel+1, StyleAggressive, n).PerAttack - nowAgg
	strCost := float64(levels.XPAtLevel(strLevel+1) - levels.XPAtLevel(strength))

	if strCost <= 0 || atkCost <= 0 {
		return StyleAggressive
	}
	if atkGain/atkCost > strGain/strCost {
		return StyleAccurate
	}
	return StyleAggressive
}

// WeaponByName resolves a worn weapon's name to the bonuses the arithmetic needs, falling back to
// the Bronze sword an account that got here any other way is holding. The fallback is deliberate
// and safe in the only direction that matters: a better weapon mis-read as a bronze sword buys a
// slightly wrong style choice, never a wrong action.
func WeaponByName(name string) Weapon {
	for _, w := range []Weapon{BronzeSword, IronScimitar, SteelScimitar} {
		if w.Name == name {
			return w
		}
	}
	return BronzeSword
}

// TrainingSkill is the melee stat Stage 5 trains next, and it replaces NextMeleeSkill's
// lowest-of-three rule on the live path.
//
// Priced by SimulateStage5 against six other candidate rules (cmd/scarabstage3test -phases
// s5model, 2026-09-20): for a bronze sword carrying hitpoints 11 -> 20 it is the fastest of the
// seven at 1.51 hours of fighting, against 1.94 for lowest-of-three — 22% less. The gain is NOT
// dramatic and should not be sold as one: the model also says fighting is only about a third of
// what run e2e6 actually spent, so the food supply that stops the stage resting is the bigger
// lever and this is the cheaper one.
//
// NextMeleeSkill is kept, unchanged, because SimulateStage5 has to be able to price the rule this
// replaced.
func TrainingSkill(w Weapon, attack, strength, defence int) MeleeSkill {
	if NextStepStyle(w, attack, strength, defence, CowTarget()) == StyleAccurate {
		return MeleeAttack
	}
	return MeleeStrength
}
