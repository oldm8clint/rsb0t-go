package questplan

import (
	"fmt"
	"strings"
	"time"
)

// The Witch's House shed fight, judged before the account goes in and while it is inside.
//
// WHAT THE LIVE LOGS SHOWED (6 to 15 Oct 2026, 523 runs of the quest). Five deaths in the shed, every
// one the same shape: the kit step had FAILED (the mule's Chaos rune ask was spent, Betty had none on
// the shelf, the bank could not be used) and the quest went on with whatever the pack held. One account
// (Magic 44, a staff of air, 500 mind runes and no fire runes) cast Wind Strike, which hits for 2 at
// most, so the four forms' 144 hitpoints took 142 casts and seven minutes; it ate five fish and died
// with none left. Another died twice the same way (35 casts at 14/35 hitpoints and no food; 39 casts
// at 9/38 and no food), and a third ran out of runes after 11 casts with a form on it and only the
// witch's catch (a teleport to the boy) saved it from dying. Nothing in the loop ever looked at the
// food or at how long the runes would last, and the cast loop had no retreat, so the only way out of
// a lost fight was a death. The module's cap was three deaths.
//
// So the account goes into the shed with the runes to finish the forms in a reasonable number of
// casts and the food to out-last the hits they land, or it does not go, and once inside it leaves
// when the fight is being lost (DecideWitchShed) instead of casting on. Both halves are pure.
//
// The model is content's, as the other fights' (fightarena.go ArenaDamage, meleemodel.go): a cast at
// a form lands when randominc(attack roll) > randominc(defence roll), the roll against the npc's
// MAGIC level, which witches_house.npc leaves at 1, so (1+9)*(0+64) = 640 against the account's
// (magic+9)*64: about nine casts in ten at Magic 44, and a hit deals randominc(maxhit). ArenaDamage
// folds that to "2.6 casts to a maximum hit"; the forms are priced the same way.

// The four forms of the shapeshifter, hitpoints in the order the shed spawns them (content
// quest_ball/configs/witches_house.npc: glob 21, spider 31, bear 41, wolf 51).
const WitchShedHitpoints = 21 + 31 + 41 + 51

// WitchDamageSpare is the share of the forms' hitpoints the held runes must be worth on top of them,
// in percent: casts miss, a form's last cast overkills, and each new form is a cast of delay.
const WitchDamageSpare = 125

// WitchLossMargin is how many times the hitpoints the model expects the fight to cost the account's
// hitpoints and food must cover. The model assumes no armour (the account's Defence level alone) and
// prices the bear in full though the real collision pack says it cannot stand beside the cast tile
// (witchshed_pack_test.go: the spider and the wolf can, from two positions, the bear from none), which
// is the room it leaves for the rolls, the walk between the glob's tile and the cast tile, and the
// bites the eat loop is late for; so the margin is smaller than Priest in Peril's 2.
const WitchLossMargin = 1.5

// witchCastTicks is a combat cast's action delay (questrunner.castTicks).
const witchCastTicks = 5

// WitchShedMaxCasts is the casts after which the forms are called not to be falling: the kit pays for
// 70 and the model needs about 50, so 130 is a fight that is going wrong (rolls, regeneration), and
// it is what the 8 minute budget allows in any case (a cast every 3.2 s).
const WitchShedMaxCasts = 130

// WitchMaxHit is the wolf's, the hardest hitter of the three that can reach the account
// (npcMaxHit(40)).
const WitchMaxHit = 5

// The forms that hit the account (attackrate is not set in the config: 4 ticks). The glob is cast at
// from outside its maxrange and never reaches the account.
var (
	WitchSpider = MeleeFoe{Name: "Witches experiment second form", Attack: 28, Strength: 20, Defence: 29, Magic: 1, Hitpoints: 31, AttackTicks: 4}
	WitchBear   = MeleeFoe{Name: "Witches experiment third form", Attack: 38, Strength: 30, Defence: 39, Magic: 1, Hitpoints: 41, AttackTicks: 4}
	WitchWolf   = MeleeFoe{Name: "Witches experiment fourth form", Attack: 48, Strength: 40, Defence: 49, Magic: 1, Hitpoints: 51, AttackTicks: 4}
)

// witchFoodHeal is the hitpoints one item of each of witchFoods restores in all (a cake is three
// bites, content consume_normal.dbrow; fighterfood.foodHeal has the per-bite figures).
var witchFoodHeal = map[int]int{
	385: 20, 373: 14, 379: 12, 365: 13, 361: 10, 329: 9, 1891: 12, 1893: 8, 1895: 4, 333: 7, 2309: 5, 2142: 3,
}

// witchFight is the shed fight's casting, played the way the cast loop plays it.
type witchFight struct {
	// Casts is the casts the pack pays for, up to the damage the forms need with the spare; ToKill is
	// the cast on which the forms' own hitpoints were dealt (0 when the runes never get there).
	Casts, ToKill int
	// Damage is the expected hitpoints those casts deal.
	Damage float64
	// Spells are the spells used, in order (a stronger one is used until its runes are gone).
	Spells []string
}

// witchSimulate plays the casts: each is the highest-level combat spell the pack still pays for
// (questrunner.BestCastableSpell, which the loop calls every cast), worth maxhit/2.6 hitpoints on
// average (ArenaDamage's rule), until the forms' hitpoints plus the spare are dealt or the runes run
// out. A worn elemental staff pays its element.
func witchSimulate(s Snapshot) witchFight {
	stock := make(map[int]int, len(s.InvIDs))
	for id, n := range s.InvIDs {
		stock[id] = n
	}
	free := map[int]bool{}
	for id, n := range s.WornIDs {
		if r, ok := arenaStaffRunes[id]; ok && n > 0 {
			free[r] = true
		}
	}
	magic := arenaMagic(s)
	need := float64(WitchShedHitpoints*WitchDamageSpare) / 100
	var f witchFight
	for f.Casts < 400 && f.Damage < need {
		var pick *arenaSpell
		for i := range arenaSpells {
			c := &arenaSpells[i]
			if c.Level > magic {
				continue
			}
			ok := true
			for id, per := range c.Runes {
				if !free[id] && stock[id] < per {
					ok = false
					break
				}
			}
			if ok {
				pick = c // arenaSpells run lowest level first, so the last affordable one is the highest
			}
		}
		if pick == nil {
			break
		}
		for id, per := range pick.Runes {
			if !free[id] {
				stock[id] -= per
			}
		}
		f.Casts++
		f.Damage += float64(pick.MaxHit) / 2.6
		if n := len(f.Spells); n == 0 || f.Spells[n-1] != pick.Name {
			f.Spells = append(f.Spells, pick.Name)
		}
		if f.ToKill == 0 && f.Damage >= WitchShedHitpoints {
			f.ToKill = f.Casts
		}
	}
	return f
}

// WitchShedLoss is the hitpoints the model expects the three forms that can reach the account to take
// off it while it casts at perCast hitpoints a cast: each form lives for its hitpoints over that and
// one cast a witchCastTicks ticks, and swings every four ticks, at the hit chance of its attack roll
// against the account's Defence (no armour) for half its maximum.
func WitchShedLoss(defence int, perCast float64) float64 {
	if defence < 1 {
		defence = 1
	}
	if perCast <= 0 {
		return 0
	}
	var total float64
	for _, f := range []MeleeFoe{WitchSpider, WitchBear, WitchWolf} {
		ticks := float64(f.Hitpoints) / perCast * witchCastTicks
		attacks := ticks / float64(f.AttackTicks)
		hit := rollHit((f.Attack+9)*64, (defence+8)*64)
		total += attacks * hit * float64(npcMaxHit(f.Strength)) / 2
	}
	return total
}

// WitchShedVerdict is WitchShedReady's answer.
type WitchShedVerdict struct {
	OK  bool
	Why string
	// Spells are the spells the pack's runes will cast, in order; Casts is the casts to take the forms'
	// hitpoints off (0 when the runes never do) and Damage the hitpoints the runes are worth in all.
	Spells []string
	Casts  int
	Damage int
	// Loss is the hitpoints the model expects the fight to cost, Pool the hitpoints and food the
	// account has to pay it with.
	Loss float64
	Pool int
}

// WitchShedReady judges whether the pack can finish the shed: the runes worth the forms' hitpoints
// with a quarter spare, in a fight short enough to be won (WitchShedMaxCasts is where it is called
// lost, and the model wants it done in three quarters of that), then the hitpoints and food worth
// WitchLossMargin times what the fight costs. An account whose hitpoints are not read yet is not
// judged on food, as Priest in Peril's gate does.
func WitchShedReady(s Snapshot) WitchShedVerdict {
	f := witchSimulate(s)
	v := WitchShedVerdict{Spells: f.Spells, Casts: f.ToKill, Damage: int(f.Damage)}
	if f.Casts == 0 {
		v.Why = fmt.Sprintf("no combat spell the pack's runes pay for at Magic %d", arenaMagic(s))
		return v
	}
	if need := WitchShedHitpoints * WitchDamageSpare / 100; v.Damage < need {
		v.Why = fmt.Sprintf("the runes (%d casts of %s) are worth about %d hitpoints and the four forms need %d", f.Casts, strings.Join(f.Spells, ", then "), v.Damage, need)
		return v
	}
	if limit := WitchShedMaxCasts * 3 / 4; f.ToKill > limit {
		v.Why = fmt.Sprintf("%s takes %d casts to finish the forms (at most %d, and a Wind Strike takes 190)", strings.Join(f.Spells, ", then "), f.ToKill, limit)
		return v
	}
	hp := s.HP
	if hp <= 0 {
		hp = s.MaxHP
	}
	if hp <= 0 {
		v.OK = true
		return v
	}
	pool := hp
	for id, n := range s.InvIDs {
		pool += witchFoodHeal[id] * n
	}
	def := s.Level("defence")
	v.Pool = pool
	v.Loss = WitchShedLoss(def, float64(WitchShedHitpoints)/float64(f.ToKill))
	if float64(pool-1) < WitchLossMargin*v.Loss {
		v.Why = fmt.Sprintf("it costs about %.0f hitpoints casting %s at Defence %d and the account has %d hitpoints and food worth %d more, less than %.1f times that",
			v.Loss, strings.Join(f.Spells, ", then "), max(def, 1), hp, pool-hp, WitchLossMargin)
		return v
	}
	v.OK = true
	return v
}

// WitchShedReading is one poll of the shed fight.
type WitchShedReading struct {
	HP, MaxHP, EatAt int
	// Food is the pieces of the sustain's food in the pack.
	Food int
	// Casts is how many casts the fight has sent.
	Casts int
	// CanCast is whether a combat spell is still paid for by the pack.
	CanCast bool
}

// DecideWitchShed judges one poll of the fight: FightRetreat gives it up and walks out, FightOn casts
// again. The rules are the ones that would have saved the five accounts that died here:
//
//   - no spell left to cast (11 casts into the kit that failed, 9 Oct): nothing the
//     account can do hurts the form, and the form is on it.
//   - down to the last food (none or one) with the hitpoints within one maximum hit of the eat line
//     (questplan.DecideFight's rule, widened by WitchMaxHit): the bite the eat loop is about to take
//     is the last one, and the walk out needs hitpoints too. The Wind Strike death (6 hitpoints, no food,
//     casting on) and the two at 14 and 9 hitpoints with no food all passed this line casting.
//   - WitchShedMaxCasts casts and the forms still standing.
func DecideWitchShed(r WitchShedReading) FightVerdict {
	if !r.CanCast {
		return FightVerdict{FightRetreat, fmt.Sprintf("no combat spell left to cast after %d casts", r.Casts)}
	}
	if v := DecideFight(FightReading{HP: r.HP, MaxHP: r.MaxHP, EatAt: r.EatAt + WitchMaxHit, Food: r.Food}); v.Action == FightRetreat {
		return v
	}
	if r.Casts >= WitchShedMaxCasts {
		return FightVerdict{FightRetreat, fmt.Sprintf("the forms are still standing after %d casts", r.Casts)}
	}
	return FightVerdict{Action: FightOn}
}

// The shed's death cap: one death in the shed skips the quest for a day, whatever MaxDeaths says. The
// account that died there did so with the same pack it would carry back in an hour.
const (
	WitchShedDeathCap    = 1
	WitchShedDeathWindow = 24 * time.Hour
	WitchShedDeathSkip   = 24 * time.Hour
)
