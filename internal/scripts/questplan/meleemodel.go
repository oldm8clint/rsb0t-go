package questplan

import "math"

// What a melee exchange costs, from this server's own content, so a quest can refuse a fight the
// account cannot be expected to win instead of finding out from a death.
//
// Every number is content, quoted with its file (the same formulas scarabplan/damage.go cites for
// the cow field):
//
//	[proc,combat_stat](int $effective_stat, int $bonus)(int)   return(calc($effective_stat * ($bonus + 64)));
//	[proc,combat_maxhit](int $combat_stat)(int)                return(calc(($combat_stat + 320) / 640));
//	                                    - content/scripts/skill_combat/scripts/combat.rs2
//	npc max hit      = maxhit((strength + 9) * (strengthbonus + 64))      - npc/npc_combat_melee.rs2
//	npc attack roll  = (attack + 9) * (attackbonus + 64)                  - npc/npc_combat_melee.rs2
//	npc magic roll   = (magic + 9) * (magicattack + 64)                   - npc/npc_combat_magic.rs2
//	npc defence roll = (defence + 9) * (defencebonus + 64)                - npc/npc_combat.rs2
//	player attack    = (attack + 8 + style) * (attackbonus + 64)          - player/player_combat_stat.rs2
//	player max hit   = maxhit((strength + 8 + style) * (strengthbonus + 64))
//	player melee def = (defence + 8 + style) * (defencebonus + 64)
//	player magic def = ((7 * magic + 3 * defence) / 10 + 8) * (magicdefencebonus + 64)
//	a roll hits when randominc(attack roll) > randominc(defence roll); a hit deals randominc(maxhit)
//
// Prayer is 100% (none held), equipment bonuses default to 0 (PESSIMISTIC for an account that
// wields a sword and wears armour, which the pure plan cannot see), and the combat style bonus to 0.
// So the answer is a ceiling on the damage taken and a floor on the time to kill.

// MeleeFoe is the part of an npc config a melee exchange reads.
type MeleeFoe struct {
	Name                                        string
	Attack, Strength, Defence, Magic, Hitpoints int
	// AttackTicks is param attackrate, 4 when the config names none.
	AttackTicks int
	// AttackBonus and StrengthBonus are the config's params of those names (0 when it names none):
	// the foe's attack roll is (Attack+9)*(AttackBonus+64) and its maximum hit comes from
	// (Strength+9)*(StrengthBonus+64), npc_melee_attack_roll and npc_melee_maxhit above. Sir Mordred's
	// 20 and 16 and the Khazard Ogre's 22 and 20 make a maximum hit of 5 and 8 where the bare
	// strength says 4 and 6.
	AttackBonus, StrengthBonus int
	// DefenceBonus is the lowest of the config's stab, slash and crush defence params (the style
	// an account would choose to swing in): the account's attack rolls against
	// (Defence+9)*(DefenceBonus+64). 0 when it names none.
	DefenceBonus int
	// ZapMax and ZapOneIn: one attack in ZapOneIn is a magic strike of up to ZapMax (the Monk of
	// Zamorak's npc_zap_attack), rolled against the player's magic defence; 0 means melee only.
	ZapMax, ZapOneIn int
}

// MeleeFighter is the account's side of an exchange: base levels, the weapon's speed and the
// equipment bonuses the caller knows (0 for none).
type MeleeFighter struct {
	Attack, Strength, Defence, Magic int
	// AttackBonus, StrengthBonus, DefenceBonus and MagicDefenceBonus are the worn items' totals.
	AttackBonus, StrengthBonus, DefenceBonus, MagicDefenceBonus int
	// AttackTicks is the weapon's attackrate (4 for a sword, a scimitar and bare hands).
	AttackTicks int
}

// Exchange is an expected exchange to the foe's death.
type Exchange struct {
	// OurHitChance is the chance one swing hits, OurMaxHit its maximum and OurPerSwing the expected
	// damage of one swing (hit chance times half the maximum).
	OurHitChance float64
	OurMaxHit    int
	OurPerSwing  float64
	// TheirPerAttack is the expected damage of one of the foe's attacks to the account, melee and
	// zap mixed.
	TheirPerAttack float64
	// KillTicks is the expected ticks to take the foe's hitpoints off.
	KillTicks float64
	// DamageTaken is the expected hitpoints the account loses in that time.
	DamageTaken float64
}

// rollHit is the chance randominc(attack) > randominc(defence) for two rolls.
func rollHit(attack, defence int) float64 {
	if attack <= 0 {
		return 0
	}
	n, m := float64(attack+1), float64(defence+1)
	if n > m {
		return 1 - (m+1)/(2*n)
	}
	return (n - 1) / (2 * m)
}

// npcMaxHit is [proc,npc_melee_maxhit] for an npc with no strength bonus.
func npcMaxHit(strength int) int { return ((strength+9)*64 + 320) / 640 }

// MaxHit is the foe's melee maximum hit, [proc,npc_melee_maxhit] with its strength bonus.
func (f MeleeFoe) MaxHit() int { return ((f.Strength+9)*(f.StrengthBonus+64) + 320) / 640 }

// PeakHit is the hardest single hit the foe can land: its melee maximum or, for a foe that zaps, the
// zap's.
func (f MeleeFoe) PeakHit() int { return max(f.MaxHit(), f.ZapMax) }

// FoePerAttack is the hitpoints one of the foe's attacks is expected to take off an account with
// these levels (no armour, no prayer): melee and zap mixed, as MeleeExchange.TheirPerAttack.
func FoePerAttack(f MeleeFoe, p MeleeFighter) float64 {
	meleeHit := rollHit((f.Attack+9)*(f.AttackBonus+64), (p.Defence+8)*(p.DefenceBonus+64))
	perAttack := meleeHit * float64(f.MaxHit()) / 2
	if f.ZapMax > 0 && f.ZapOneIn > 0 {
		magicDef := ((7*p.Magic+3*p.Defence)/10 + 8) * (p.MagicDefenceBonus + 64)
		zapHit := rollHit((f.Magic+9)*64, magicDef)
		perZap := zapHit * float64(f.ZapMax) / 2
		share := 1 / float64(f.ZapOneIn)
		perAttack = (1-share)*perAttack + share*perZap
	}
	return perAttack
}

// MeleeExchange prices a fight between the account and a foe.
func MeleeExchange(p MeleeFighter, f MeleeFoe) Exchange {
	ourTicks, theirTicks := p.AttackTicks, f.AttackTicks
	if ourTicks <= 0 {
		ourTicks = 4
	}
	if theirTicks <= 0 {
		theirTicks = 4
	}
	var x Exchange
	// The account's swing.
	attackRoll := (p.Attack + 8) * (p.AttackBonus + 64)
	defenceRoll := (f.Defence + 9) * (f.DefenceBonus + 64)
	x.OurHitChance = rollHit(attackRoll, defenceRoll)
	x.OurMaxHit = ((p.Strength+8)*(p.StrengthBonus+64) + 320) / 640
	x.OurPerSwing = x.OurHitChance * float64(x.OurMaxHit) / 2
	// The foe's attack.
	perAttack := FoePerAttack(f, p)
	x.TheirPerAttack = perAttack
	if x.OurPerSwing > 0 {
		x.KillTicks = float64(f.Hitpoints) / x.OurPerSwing * float64(ourTicks)
		x.DamageTaken = x.KillTicks / float64(theirTicks) * perAttack
	} else {
		x.KillTicks, x.DamageTaken = math.Inf(1), math.Inf(1)
	}
	return x
}
