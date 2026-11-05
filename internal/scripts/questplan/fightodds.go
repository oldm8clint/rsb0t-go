package questplan

import (
	"fmt"
	"strings"
)

// The shared half of "can this account win this fight, and when must it eat or stop": the foes' content
// stats, the food tables, the eat line, and an expected-value simulation of a quest fight that both the
// entry gates (Fight Arena, Merlin's Crystal) and the fight loops read.
//
// WHY THIS EXISTS (live, 16 Oct 2026, two deaths of accounts running unproven tail quests):
//
//   - Fight Arena 01:09-01:13. The account (53 hitpoints, Magic 55, 22 pieces of cake) beat the Khazard
//     Ogre at the arena's "safespot" (the ogre stood at 2602,3154 beside it and hit it down to 14/53),
//     came out of Hengrad's cell at 19/53 and started the Khazard Scorpion at 20/53 with an eat line of
//     14. The scorpion lines in the log stop thirteen seconds before the first bite of the end, which
//     then comes at 4/53 and goes 4, 8, 1, 0 two seconds apart, a drop of 11 in two seconds that no
//     scorpion (maximum hit 5) can deal and Bouncer (13) can: by the shape of the log the scorpion fell,
//     the cast loop took that for the win and drove the General's dialogue for a flat twenty seconds with
//     no bite while Bouncer was released, and the account died to him at under 15/53 with 22 pieces of
//     cake in the pack. (The log's killer, "Jeremy Servil", is the one npc still facing the account after
//     the respawn; it names nobody.)
//   - Merlin's Crystal 01:14-01:18. The fight kit had FAILED (Betty had no chaos rune, the mule ask was
//     spent), the quest went on with whatever the pack held, and the account cast three Fire Bolts at Sir
//     Mordred (38 hitpoints, healed to full whenever he would die) and then meleed him for three
//     minutes, eating 20 bites of 4 and 5 hitpoints at 8 to 12 of 53 and finally dying with none left.
//
// The content explains the rates. A bite of cake or bread heals 4 (chocolate 5), a bite of food is an
// action of its own (consume.rs2 adds 3 ticks to the account's action delay per bite), and the foes hit for
// what npc_melee_maxhit says, with the strength bonus the config names (the ogre's 20, Mordred's 16), which
// the earlier models left out: the ogre's maximum is 8, not 6, and Mordred's 5, not 4. The ogre fight in the
// log took 39 of the account's 53 hitpoints in 38 seconds (0.6 a tick), which is what the model says for an
// account at Defence 1 to 20.
//
// What the logs show is that the model's rates were not the problem and the LOOPS were: the account started
// the scorpion at 20 of 53 because nothing topped it up between fights, ate once per cast (3.2 s apart, and
// not at all while a lure walk or a dialogue was under way) at a line of 14 that a 5-hit-per-attack scorpion
// crosses between two looks, and meleed Mordred with the Attack of a mage. So the pieces here are: the
// eat line from the foe (FoeEatLine), the level to start at (FightStartHP), the odds (SimulateFight,
// JudgeFight, FightChain) that the entry gates read, and the empirical race (fightrace.go) that the loops
// read while a fight is on, which needs no model at all.
//
// SimulateFight plays a fight tick by tick on expected values, with the foe's damage scaled up a quarter for
// the rolls and the armour the pure plan cannot see (the account's own Defence level only). It is the model
// under ArenaOdds and MerlinMordredReady. It says the Fight Arena chain is lost to Bouncer (116 hitpoints, a
// maximum hit of 13, a hit every four ticks) for any account without a pack of sharks, whatever its spell:
// that is the arena's death toll in the log (eleven on 6-7 Oct, one on 12 Oct) and the reason its gate now
// prices the whole chain before the account goes in.

// Foes of the quests that fight, by npc id, stats from content (the .npc files named at each entry). An
// npc whose config names no magic stat has Magic 1; none names an attackrate except the ogre's 6.
var (
	// FoeArenaOgre is quest_arena.npc [arena_ogre]: crush, attackrate 6, attackbonus 22, strengthbonus 20.
	FoeArenaOgre = MeleeFoe{Name: "Khazard Ogre", Attack: 54, Strength: 53, Defence: 53, Magic: 1, Hitpoints: 60,
		AttackTicks: 6, AttackBonus: 22, StrengthBonus: 20}
	// FoeArenaScorpion is [arena_scorpion]: stab, defence bonuses 5/15/15.
	FoeArenaScorpion = MeleeFoe{Name: "Khazard Scorpion", Attack: 40, Strength: 39, Defence: 34, Magic: 1, Hitpoints: 40,
		AttackTicks: 4, DefenceBonus: 5}
	// FoeArenaBouncer is [arena_bouncer]: level 137, 116 hitpoints, a maximum hit of 13.
	FoeArenaBouncer = MeleeFoe{Name: "Bouncer", Attack: 120, Strength: 120, Defence: 120, Magic: 1, Hitpoints: 116, AttackTicks: 4}
	// FoeGeneralKhazard is [general_khazard].
	FoeGeneralKhazard = MeleeFoe{Name: "General Khazard", Attack: 75, Strength: 78, Defence: 80, Magic: 1, Hitpoints: 170, AttackTicks: 4}
	// FoeMordred is keep_le_faye.npc [sir_mordred]: attackbonus 20, strengthbonus 16, defence bonuses 99/82/45.
	// At his death content heals him to full and spawns Morgan Le Faye (sir_mordred.rs2), so "killing" him
	// is dealing his 38 hitpoints once.
	FoeMordred = MeleeFoe{Name: "Sir Mordred", Attack: 33, Strength: 33, Defence: 33, Magic: 1, Hitpoints: 38,
		AttackTicks: 4, AttackBonus: 20, StrengthBonus: 16, DefenceBonus: 45}
	// FoeRenegadeKnight is [renegade_knight].
	FoeRenegadeKnight = MeleeFoe{Name: "Renegade Knight", Attack: 28, Strength: 28, Defence: 28, Magic: 1, Hitpoints: 48,
		AttackTicks: 4, AttackBonus: 20, StrengthBonus: 16, DefenceBonus: 62}
	// FoeCountDraynor is [count_draynor]: defence bonuses 2/1/3.
	FoeCountDraynor = MeleeFoe{Name: "Count Draynor", Attack: 30, Strength: 25, Defence: 30, Magic: 1, Hitpoints: 35, AttackTicks: 4, DefenceBonus: 1}
	// FoeTempleGuardian is [priestperilguarddog].
	FoeTempleGuardian = MeleeFoe{Name: "Temple guardian", Attack: 20, Strength: 20, Defence: 20, Magic: 1, Hitpoints: 45, AttackTicks: 4}
	// FoeJailer is [jailer]: defence bonuses 79/63/47.
	FoeJailer = MeleeFoe{Name: "Jailer", Attack: 40, Strength: 40, Defence: 40, Magic: 1, Hitpoints: 47, AttackTicks: 4, DefenceBonus: 47}
	// FoeGhast is [ghast_vis].
	FoeGhast = MeleeFoe{Name: "Ghast", Attack: 22, Strength: 22, Defence: 18, Magic: 1, Hitpoints: 45, AttackTicks: 4}
	// FoeGiantBat is [bat]: defence bonuses 10/10/12.
	FoeGiantBat = MeleeFoe{Name: "Giant bat", Attack: 22, Strength: 22, Defence: 22, Magic: 1, Hitpoints: 32, AttackTicks: 4, DefenceBonus: 10}
	// FoeKhazardCommander is quest_tree/configs/gnome_battlefield.npc [khazard_commander] (npc 478): attack 50,
	// defence 50, strength 45, 22 hitpoints, no attackrate (4), no bonus params (a maximum hit of 5), huntmode
	// cowardly with huntrange 1. Two of them stand in the Khazard stronghold (maps/m39_50.jm2 "0 13 54: 478" in the
	// hall and "1 7 55: 478" on the chest floor), and Tree Gnome Village's orb step has to get past both: live 16 Oct
	// 2026 two accounts with no food lost 20 hitpoints to the first on the way in and the rest to the second.
	FoeKhazardCommander = MeleeFoe{Name: "Khazard commander", Attack: 50, Strength: 45, Defence: 50, Magic: 1, Hitpoints: 22, AttackTicks: 4}
	// FoeFaladorGardener is [falador_gardener] (npc 1217, scripts/_unpack/274/all.npc): attack 2, defence 5, 7 hitpoints, no
	// strength stat (1), no attackrate (4), no bonus params (a maximum hit of 1). A dig in Falador park within ten tiles of him
	// sets him on the digger (quest_hunt dig.rs2), and he respawns 50 ticks after he dies.
	FoeFaladorGardener = MeleeFoe{Name: "Gardener", Attack: 2, Strength: 1, Defence: 5, Magic: 1, Hitpoints: 7, AttackTicks: 4}
	// FoeGuardDog is [guarddog] (npc 99, scripts/_unpack/225/all.npc): level 44, attack 35, defence 37, strength 36, 49 hitpoints, no
	// attackrate (4), no bonus params (a maximum hit of 5), huntmode cowardly with huntrange 1 and wanderrange 3: it hunts only an
	// account of combat 88 or less (twice its level), and only within a tile of itself. Ten of them stand in McGrubor's Wood, where the
	// Fishing Contest digs its red vine worms, and two keep Lord Handelmort's garden.
	FoeGuardDog = MeleeFoe{Name: "Guard dog", Attack: 35, Strength: 36, Defence: 37, Magic: 1, Hitpoints: 49, AttackTicks: 4}
)

// Content ids of foes that no other plan file names.
const (
	NpcGeneralKhazard = 258
	NpcRenegadeKnight = 237
)

// questFoes is FoeByNPC's table. A foe that is not in it (Elvarg's dragonfire, the Black Demon's
// three-tile reach, the Horror mothers' forms, the Black Knight Titan) is not priced by this model and is
// fought by its own rules.
var questFoes = map[int]MeleeFoe{
	NpcArenaOgre:        FoeArenaOgre,
	NpcArenaScorpion:    FoeArenaScorpion,
	NpcArenaBouncer:     FoeArenaBouncer,
	NpcGeneralKhazard:   FoeGeneralKhazard,
	NpcKhazardCommander: FoeKhazardCommander,
	NpcMordred:          FoeMordred,
	NpcRenegadeKnight:   FoeRenegadeKnight,
	NpcFaladorGardener:  FoeFaladorGardener,
	NpcCountDraynor:     FoeCountDraynor,
	NpcTempleGuardian:   FoeTempleGuardian,
	NpcJailer:           FoeJailer,
	NpcGhastShown:       FoeGhast,
	NpcGhastHidden:      FoeGhast,
	NpcGiantBat:         FoeGiantBat,
	NpcZamorakKeyMonk:   PipKeyMonk,
	NpcManorSkeleton:    FoeManorSkeleton,
	NpcSuitOfArmour:     FoeSuitOfArmour,
	NpcBrownBear:        FoeBrownBear,
	NpcJogre:            FoeJogre,
	NpcGuardDog:         FoeGuardDog,
	NpcGoblinGuard:      FoeGoblinGuard,
	NpcSickMourner:      FoeSickMourner,
	NpcEwEarthRock:      FoeEarthElementalRock,
	TbNpcShaikahan:      FoeShaikahan,
	// The Watch Tower's ogre country (watchtower.go): the level 53 ogres, Gorad and the chieftains.
	WtNpcOgre:       FoeOgre,
	WtNpcOgre2:      FoeOgre,
	WtNpcGorad:      FoeGorad,
	WtNpcChieftain:  FoeOgreChieftain,
	NpcTtCaptain:    FoeMercenaryCaptain,
	NpcTtRowdyGuard: FoeRowdyGuard,
	NpcTtDesertWolf: FoeDesertWolf,
	NpcTtUgthanki:   FoeUgthanki,
	// Lost City's Tree spirit, Entrana zombie and the corridor's greater demon (lostcity.go).
	NpcLcTreeSpirit:   FoeTreeSpirit,
	NpcLcZombie:       FoeEntranaZombie,
	NpcLcGreaterDemon: FoeGreaterDemon,
	NpcFeldipWolf:     FoeFeldipWolf,
	NpcMtShadow:       FoeLoarShade,
	NpcMtShade:        FoeLoarShade,
}

// FoeByNPC is the content stats of the quest foe with this npc id.
func FoeByNPC(id int) (MeleeFoe, bool) {
	f, ok := questFoes[id]
	return f, ok
}

// QuestFoeIDs lists the npc ids FoeByNPC knows, for the test that pins the table against the content.
func QuestFoeIDs() []int {
	out := make([]int, 0, len(questFoes))
	for id := range questFoes {
		out = append(out, id)
	}
	return out
}

// ---------------------------------------------------------------------------------------------------------
// Food.
// ---------------------------------------------------------------------------------------------------------

// foodInfo is one eatable object: what a bite restores (content consume_normal.dbrow heal_N) and how many
// bites the object has left (a cake is three, a 2/3 cake two, a slice one).
type foodInfo struct{ Bite, Bites int }

// foodTable covers the foods the quest modules carry.
var foodTable = map[int]foodInfo{
	385: {20, 1}, 373: {14, 1}, 379: {12, 1}, 365: {13, 1}, 361: {10, 1}, 329: {9, 1}, 333: {7, 1}, // shark .. trout
	1891: {4, 3}, 1893: {4, 2}, 1895: {4, 1}, // cake, 2/3 cake, slice of cake
	1897: {5, 3}, 1899: {5, 2}, 1901: {5, 1}, // chocolate cake, 2/3, slice
	2309: {4, 1}, 2142: {3, 1}, // bread, cooked meat
}

// FoodBiteHeal is the hitpoints one bite of the food restores, 0 for an object the table does not know.
func FoodBiteHeal(id int) int { return foodTable[id].Bite }

// FoodItemHeal is what the whole object restores in all its bites.
func FoodItemHeal(id int) int { f := foodTable[id]; return f.Bite * f.Bites }

// FoodPoolHeal is the hitpoints the food in inv (of the ids in foods, all of them when foods is nil) can
// restore in all.
func FoodPoolHeal(inv map[int]int, foods []int) int {
	total := 0
	if foods == nil {
		for id, n := range inv {
			total += FoodItemHeal(id) * n
		}
		return total
	}
	for _, id := range foods {
		total += FoodItemHeal(id) * inv[id]
	}
	return total
}

// FoodBites expands the food in inv into the bites the eat loop takes, in the order it takes them: foods
// is the sustain's list, best first, and each object's bites come out before the next object's (a cake's
// first bite is one object, the 2/3 cake it leaves another that the list reaches later).
func FoodBites(inv map[int]int, foods []int) []int {
	var out []int
	for _, id := range foods {
		f := foodTable[id]
		for i := 0; i < inv[id]*f.Bites; i++ {
			out = append(out, f.Bite)
		}
	}
	return out
}

// ---------------------------------------------------------------------------------------------------------
// The eat line and the level to start a fight at.
// ---------------------------------------------------------------------------------------------------------

// FoeEatMaxHits is how many of the foe's hardest hits the account keeps above itself when it eats: it eats
// at twice the foe's maximum hit, so the hit that lands while the bite is on its way, and the next, still
// leave it standing. The module's own line (Sustain.EatAt) is the floor.
const FoeEatMaxHits = 2

// FoeEatLine is the hitpoints at or below which the account eats in a fight with a foe whose hardest hit is
// peakHit: FoeEatMaxHits of them, never under base (0 stays 0: the module never eats), and never over
// three quarters of maxHP unless base already is (a foe that hits for a third of the account's hitpoints
// is not fought, and a line above the bar would eat at full health).
func FoeEatLine(base, peakHit, maxHP int) int {
	if base <= 0 {
		return base
	}
	line := max(base, FoeEatMaxHits*peakHit)
	if maxHP > 0 {
		line = min(line, max(base, maxHP*3/4))
	}
	return line
}

// VsFoe is the sustain with its eat line raised for the foe (FoeEatLine).
func (s Sustain) VsFoe(f MeleeFoe, maxHP int) Sustain {
	s.EatAt = FoeEatLine(s.EatAt, f.PeakHit(), maxHP)
	return s
}

// FightStartHP is the hitpoints an account should have before it starts a fight with a foe whose hardest
// hit is peakHit: four of those hits, and four fifths of its hitpoints at the least, never more than the
// maximum. The Fight Arena account that started the Khazard Scorpion at 20 of 53 and the Merlin account
// that went upstairs at 50 of 53 and was at 12 two minutes later are the two ends: the first was never
// topped up, the second was, and the top-up is what the loops now take before every first blow.
func FightStartHP(maxHP, peakHit int) int {
	if maxHP <= 0 {
		return 0
	}
	return min(maxHP, max(maxHP*8/10, 4*peakHit))
}

// ---------------------------------------------------------------------------------------------------------
// The simulation.
// ---------------------------------------------------------------------------------------------------------

// FightIncomingScale is how much more than the expected damage the simulation charges the account: the
// rolls, and the armour and weapon bonuses the plan cannot see only ever make the real fight cheaper
// (the model reads the account's Defence level alone). A quarter reproduces the scorpion fight above.
const FightIncomingScale = 1.25

// fightBiteTicks is what a bite costs the account's offence: consume.rs2 adds 3 ticks to the action delay.
const fightBiteTicks = 3

// fightEatGap is the least ticks between two bites (the food's own eat delay: 2, 1 for the first bites of a
// cake).
const fightEatGap = 2

// FightPhase is one stretch of the account's offence: Actions actions of PerAction expected hitpoints, one
// every ActionTicks ticks. Actions 0 means no limit (the melee fallback).
type FightPhase struct {
	Name        string
	PerAction   float64
	ActionTicks int
	Actions     int
	// Runes are the runes one action spends, by object id (a worn staff's element left out).
	Runes map[int]int
}

// FightSimInput is one fight for SimulateFight.
type FightSimInput struct {
	Foe MeleeFoe
	// Fighter carries the account's levels (Defence mostly: how often the foe hits) and the melee swing's
	// levels for a MeleePhase.
	Fighter MeleeFighter
	// HP and MaxHP are the account's at the first blow; FoeHP the foe's hitpoints left (0: all of them).
	HP, MaxHP, FoeHP int
	// Phases are the account's offence, in order.
	Phases []FightPhase
	// Bites is the heal of each bite the pack holds, in the order they are eaten, EatLine the hitpoints at
	// or under which the account takes one.
	Bites   []int
	EatLine int
	// Scale multiplies the foe's expected damage (FightIncomingScale when 0); MaxTicks is where the fight is
	// called not won (1000, ten minutes, when 0).
	Scale    float64
	MaxTicks int
	// Extra is the hitpoints a tick that OTHER attackers (hunters that join the fight and are not fought back) take off the
	// account on average, before Scale. 0 for a fight with the foe alone.
	Extra float64
}

// FightSimResult is how a simulated fight went.
type FightSimResult struct {
	// Won is the foe's hitpoints dealt before the account's ran out and inside MaxTicks.
	Won bool
	// Ticks is how long the fight ran.
	Ticks int
	// MinHP is the lowest hitpoints reached (the foe's hit at the end of a tick, after any bite), EndHP the
	// last.
	MinHP, EndHP float64
	// BitesUsed is how many of Bites were eaten.
	BitesUsed int
	// Done is the actions each phase performed, in Phases' order.
	Done []int
	// How is why a fight that was not won ended: "dies", "runs out of time", "has nothing to attack with".
	How string
}

// SimulateFight plays a fight out tick by tick on expected values: the foe lands FoePerAttack (scaled) every
// AttackTicks ticks; the account, each tick, eats a bite when its hitpoints are at or under the line and the
// food's gap allows (a bite delays its next action by 3 ticks, consume.rs2), and otherwise performs the
// next action of its offence when its delay is up.
func SimulateFight(in FightSimInput) FightSimResult {
	scale := in.Scale
	if scale <= 0 {
		scale = FightIncomingScale
	}
	maxTicks := in.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 1000
	}
	foeTicks := in.Foe.AttackTicks
	if foeTicks <= 0 {
		foeTicks = 4
	}
	foeHP := float64(in.FoeHP)
	if in.FoeHP <= 0 {
		foeHP = float64(in.Foe.Hitpoints)
	}
	perAttack := FoePerAttack(in.Foe, in.Fighter) * scale
	extra := in.Extra * scale
	hp, maxHP := float64(in.HP), float64(in.MaxHP)
	if maxHP <= 0 {
		maxHP = hp
	}
	res := FightSimResult{MinHP: hp, Done: make([]int, len(in.Phases))}
	phase, nextAct, eatReady := 0, 1, 0
	for t := 1; t <= maxTicks; t++ {
		res.Ticks = t
		switch {
		case in.EatLine > 0 && hp <= float64(in.EatLine) && res.BitesUsed < len(in.Bites) && t >= eatReady:
			hp = min(hp+float64(in.Bites[res.BitesUsed]), maxHP)
			res.BitesUsed++
			eatReady = t + fightEatGap
			nextAct = max(nextAct, t) + fightBiteTicks
		case t >= nextAct && phase < len(in.Phases):
			p := in.Phases[phase]
			foeHP -= p.PerAction
			res.Done[phase]++
			nextAct = t + max(p.ActionTicks, 1)
			if p.Actions > 0 && res.Done[phase] >= p.Actions {
				phase++
			}
		}
		if foeHP <= 0 {
			res.Won, res.EndHP = true, hp
			return res
		}
		if t%foeTicks == 0 {
			hp -= perAttack
		}
		hp -= extra
		if t%foeTicks == 0 || extra > 0 {
			res.MinHP = min(res.MinHP, hp)
			if hp <= 0 {
				res.EndHP, res.How = hp, "dies"
				return res
			}
		}
	}
	res.EndHP = hp
	res.How = "runs out of time"
	if phase >= len(in.Phases) {
		res.How = "has nothing left to attack with"
	}
	return res
}

// FightOdds is a priced fight (or chain of fights).
type FightOdds struct {
	// OK is whether the account is expected to win with the margin; Why says what is short when it is not.
	OK  bool
	Why string
	// Sims are the fights played, in order.
	Sims []FightSimResult
}

// fightSpareBites is the bites a priced fight must leave in the pack: one in ten of them, and one at the least.
func fightSpareBites(total int) int { return max(1, total/10) }

// JudgeFight is the margin a simulated fight must clear: won, never lower than the foe's hardest hit (one
// maximum hit from the lowest point must not kill), and with a tenth of the bites held when it began left over
// for the walk away. foe names the foe in the reason.
func JudgeFight(res FightSimResult, foe MeleeFoe, bites int) (bool, string) {
	switch {
	case !res.Won:
		return false, fmt.Sprintf("the model has the account lose to %s: it %s after %d ticks having eaten %d of %d bites (lowest %.0f hitpoints)",
			foe.Name, res.How, res.Ticks, res.BitesUsed, bites, res.MinHP)
	case res.MinHP < float64(foe.PeakHit()):
		return false, fmt.Sprintf("the model has the account at %.0f hitpoints at the worst of the fight with %s, under its hardest hit (%d)",
			res.MinHP, foe.Name, foe.PeakHit())
	case bites-res.BitesUsed < fightSpareBites(bites):
		return false, fmt.Sprintf("the model uses %d of the %d bites against %s and keeps none for the way out", res.BitesUsed, bites, foe.Name)
	}
	return true, ""
}

// freeRunes is the elements the worn staves pay.
func freeRunes(worn map[int]int) map[int]bool {
	free := map[int]bool{}
	for id, n := range worn {
		if r, ok := arenaStaffRunes[id]; ok && n > 0 {
			free[r] = true
		}
	}
	return free
}

// CastPlan is the casts a pack's runes pay for, as the cast loops take them: every cast is the highest-level
// combat spell the Magic level allows whose runes the stock still holds (questrunner.BestCastableSpell, asked
// again before each cast), until the stock cannot pay any or maxCasts have been planned. A cast is worth
// the spell's maximum hit over 2.6 hitpoints (ArenaDamage's rule: the misses, and the half-average of a roll
// of 0 to the maximum). The stock is not changed.
func CastPlan(stock, worn map[int]int, magic, maxCasts int) []FightPhase {
	free := freeRunes(worn)
	left := make(map[int]int, len(stock))
	for id, n := range stock {
		left[id] = n
	}
	var plan []FightPhase
	for c := 0; c < maxCasts; c++ {
		var pick *arenaSpell
		for i := range arenaSpells {
			sp := &arenaSpells[i]
			if sp.Level > magic {
				continue
			}
			ok := true
			for id, per := range sp.Runes {
				if !free[id] && left[id] < per {
					ok = false
					break
				}
			}
			if ok {
				pick = sp // arenaSpells run lowest level first: the last affordable one is the highest
			}
		}
		if pick == nil {
			break
		}
		runes := map[int]int{}
		for id, per := range pick.Runes {
			if !free[id] {
				left[id] -= per
				runes[id] = per
			}
		}
		if n := len(plan); n > 0 && plan[n-1].Name == pick.Name {
			plan[n-1].Actions++
			continue
		}
		plan = append(plan, FightPhase{Name: pick.Name, PerAction: float64(pick.MaxHit) * 10 / 26, ActionTicks: castTicksModel,
			Actions: 1, Runes: runes})
	}
	return plan
}

// castTicksModel is a combat cast's action delay (questrunner.castTicks).
const castTicksModel = 5

// MeleePhase is the account's swing as a phase of its offence (levels only: no weapon or armour bonuses
// are assumed, and the weapon is a four-tick one), false when its Attack or Strength is unread.
func MeleePhase(p MeleeFighter, foe MeleeFoe) (FightPhase, bool) {
	if p.Attack <= 0 || p.Strength <= 0 {
		return FightPhase{}, false
	}
	x := MeleeExchange(p, foe)
	if x.OurPerSwing <= 0 {
		return FightPhase{}, false
	}
	return FightPhase{Name: "melee", PerAction: x.OurPerSwing, ActionTicks: max(p.AttackTicks, 4)}, true
}

// SpellsOf names the spells of a cast plan, in order, for a reason.
func SpellsOf(plan []FightPhase) string {
	var names []string
	for _, p := range plan {
		if p.Name != "melee" {
			names = append(names, fmt.Sprintf("%d %s", p.Actions, p.Name))
		}
	}
	if len(names) == 0 {
		return "no spell"
	}
	return strings.Join(names, ", then ")
}

// MeleeOdds prices finishing a foe by the account's own swing alone, from where a fight stands: the foe's
// hitpoints left, the account's hitpoints now, the bites in its pack and the eat line it takes them at. It is
// the question the cast loops ask when the runes run out: false (with the reason) means the swing cannot
// finish the foe inside ten minutes with the margin JudgeFight asks, and the account should stop attacking
// rather than melee on. An account whose Attack or Strength is unread cannot swing at all.
func MeleeOdds(foe MeleeFoe, p MeleeFighter, hp, maxHP, foeHP int, bites []int, eatLine int) (bool, string) {
	m, ok := MeleePhase(p, foe)
	if !ok {
		return false, fmt.Sprintf("no swing to finish %s with (Attack %d, Strength %d)", foe.Name, p.Attack, p.Strength)
	}
	res := SimulateFight(FightSimInput{Foe: foe, Fighter: p, HP: hp, MaxHP: maxHP, FoeHP: foeHP, Phases: []FightPhase{m},
		Bites: bites, EatLine: eatLine})
	if ok, why := JudgeFight(res, foe, len(bites)); !ok {
		return false, fmt.Sprintf("meleeing %s from %d hitpoints at Attack %d, Strength %d, Defence %d: %s", foe.Name, hp, p.Attack, p.Strength, p.Defence, why)
	}
	return true, ""
}

// fighterOf reads the account's combat levels off a snapshot.
func fighterOf(s Snapshot) MeleeFighter {
	return MeleeFighter{Attack: s.Level("attack"), Strength: s.Level("strength"), Defence: max(s.Level("defence"), 1), Magic: s.Level("magic")}
}

// FightChain prices a run of foes fought one after the other with one pack: for each, the account tops up to
// FightStartHP from the bites it holds, casts what the stock pays for (the runes it spends come out of the
// stock for the next foe) and swings when the runes run out, eating at the eat line the loops use (sus.EatAt
// raised for the foe). The first foe is priced from firstFoeHP when that is above 0 (a fight begun). Nothing is
// judged for an account whose maximum hitpoints are unread.
func FightChain(s Snapshot, foes []MeleeFoe, sus Sustain, firstFoeHP int) FightOdds {
	return fightChain(s, foes, sus, firstFoeHP, true, 0)
}

// MeleeChain is FightChain for a fight that is swung and never cast: the account fights each foe with its own
// Attack and Strength whatever runes the pack holds (the shared fight step, questrunner.execFight, melees; a
// quest that casts says so with a Spell). The Khazard commanders are priced this way: the account that meets
// them carries food, not runes. Nothing is judged for an account whose Attack, Strength or maximum hitpoints
// are unread.
func MeleeChain(s Snapshot, foes []MeleeFoe, sus Sustain) FightOdds {
	return fightChain(s, foes, sus, 0, false, 0)
}

// MeleeChainAmid is MeleeChain for a fight that hunters join without being fought back: extra is the hitpoints per tick that
// they take off the account in all (FightSimInput.Extra), for as long as each fight lasts. The Watch Tower's Gorad is fought
// with the camp's ogres at his back.
func MeleeChainAmid(s Snapshot, foes []MeleeFoe, sus Sustain, extra float64) FightOdds {
	return fightChain(s, foes, sus, 0, false, extra)
}

func fightChain(s Snapshot, foes []MeleeFoe, sus Sustain, firstFoeHP int, casts bool, extra float64) FightOdds {
	odds := FightOdds{OK: true}
	if s.MaxHP <= 0 || (casts && s.Level("magic") <= 0) || (!casts && (s.Level("attack") <= 0 || s.Level("strength") <= 0)) {
		return odds // hitpoints or levels not read yet: nothing to judge, as every other gate here treats an unread stat
	}
	hp := s.HP
	if hp <= 0 {
		hp = s.MaxHP
	}
	stock := make(map[int]int, len(s.InvIDs))
	for id, n := range s.InvIDs {
		stock[id] = n
	}
	bites := FoodBites(stock, sus.FoodIDs)
	fighter := fighterOf(s)
	for i, foe := range foes {
		// Top up from the bites before the first blow.
		for hp < FightStartHP(s.MaxHP, foe.PeakHit()) && len(bites) > 0 {
			hp = min(hp+bites[0], s.MaxHP)
			bites = bites[1:]
		}
		var phases []FightPhase
		if casts {
			phases = CastPlan(stock, s.WornIDs, max(fighter.Magic, 1), 400)
		}
		if m, ok := MeleePhase(fighter, foe); ok {
			phases = append(phases, m)
		}
		foeHP := 0
		if i == 0 && firstFoeHP > 0 {
			foeHP = firstFoeHP
		}
		res := SimulateFight(FightSimInput{Foe: foe, Fighter: fighter, HP: hp, MaxHP: s.MaxHP, FoeHP: foeHP, Phases: phases,
			Bites: bites, EatLine: FoeEatLine(sus.EatAt, foe.PeakHit(), s.MaxHP), Extra: extra})
		odds.Sims = append(odds.Sims, res)
		if ok, why := JudgeFight(res, foe, len(bites)); !ok {
			odds.OK, odds.Why = false, why
			return odds
		}
		// Spend what the fight spent.
		for pi, n := range res.Done {
			for id, per := range phases[pi].Runes {
				stock[id] = max(stock[id]-per*n, 0)
			}
		}
		bites = bites[min(res.BitesUsed, len(bites)):]
		hp = int(res.EndHP + 0.5)
	}
	return odds
}
