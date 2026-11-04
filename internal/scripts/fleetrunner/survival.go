package fleetrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/tile"
)

// BaseLevel is a stat's base level from its XP (UPDATE_STAT's level is the current, drained or
// boosted one — current hitpoints for Hitpoints).
func BaseLevel(d *Deps, stat int) int {
	sk, ok := d.Session.State().Skill(stat)
	if !ok {
		return 0
	}
	return levels.LevelFromXP(sk.XP)
}

// MeleeResult is one Stage 5 fight.
type MeleeResult struct {
	Fought  bool
	Skill   fleetplan.MeleeSkill
	HPXP    int
	Verdict combatplan.Verdict
	// BrokeOff is a fight left at the HP floor; AttackerNames names what faced the account then.
	BrokeOff      bool
	AttackerNames []string
	// Misfire is the melee stat the fight paid when it was not Skill (the style was set for one
	// stat and the server paid another; scarabplan.MeleeStyleMisfire), or "" when it was.
	Misfire fleetplan.MeleeSkill
}

func meleeCombatSkill(s fleetplan.MeleeSkill) combatplan.Skill {
	switch s {
	case fleetplan.MeleeAttack:
		return combatplan.SkillAttack
	case fleetplan.MeleeDefence:
		return combatplan.SkillDefence
	}
	return combatplan.SkillStrength
}

// MeleeStep walks to the cow field if away, points the weapon at the lowest melee stat, and
// fights one Cow — to its death, or until the account's HP falls to hpFloor, when it walks away.
//
// Its own wait rather than combatrunner.FightOne's: FightOne blocks until the kill (up to its
// timeout) with no eye on the account's own HP. Live-found 2026-09-20 (cmd/scarabstage3test
// -phases bot356): a 1/1/1 account with 14 hitpoints and a Bronze sword hits a Cow for at most 1,
// takes about as much as it deals, and fought itself from 14 to 0 in two engagements.
func MeleeStep(d *Deps, monster string, hpFloor int, lowestOfThree bool) MeleeResult {
	cd := Combat(d)
	var res MeleeResult
	here := CurrentTile(d)
	if d.TrainPen.MagicOnly || d.TrainPen.Gated() {
		// No melee reaches a magic-only pen's herd (the lake Ducks), and a Magic pen above the low
		// ones (a giant herd from its safespot) is never meleed: the Cow pen instead, whatever the
		// caller chose (scarabplan.TrainPen.ForMelee).
		d.Logf("stage 5: %s is a Magic pen — training melee at %s instead", d.TrainPen.Name, fleetplan.CowPen.Name)
		d.TrainPen = d.TrainPen.ForMelee()
	}
	pen := d.Pen()
	if d.TrainPen.Name != "" {
		monster = pen.Herd() // a chosen pen names its own herd (pens.go)
	}
	if here == nil || tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, pen.Stand) > pen.Leash {
		if !combatrunner.TravelTo(cd, pen.Stand, 3, 3*time.Minute) {
			d.Logf("stage 5: could not reach %s from %s", pen.Name, HereIs(d))
			return res
		}
	}
	// Damage dealt is the only thing Hitpoints XP is paid for, so the style follows the
	// arithmetic in scarabplan/damage.go rather than the old "train whichever of Attack,
	// Strength and Defence is lowest" rule, which spent a third of every fight on a Defence the
	// Cow's own max hit of 1 does not need. scarabplan.TrainingSkill's doc comment has the
	// seven-way comparison that chose it, and its honest size (22% off fighting time).
	atk, str, def := BaseLevel(d, fleetplan.AttackStat), BaseLevel(d, fleetplan.StrengthStat), BaseLevel(d, fleetplan.DefenceStat)
	res.Skill = fleetplan.TrainingSkill(WieldedWeapon(d), atk, str, def)
	if lowestOfThree {
		res.Skill = fleetplan.BalancedMeleeSkill(atk, str, def)
	}
	if StepOffPenHazard(d) { // never train, or wait for a target, on the pen's hazard (pens.go)
		return res
	}
	combatrunner.EnsureTrainingStyle(cd, meleeCombatSkill(res.Skill))
	combatrunner.EnsureRetaliate(cd, true)
	npc, ok := NearestReachableMonster(d, monster)
	if !ok {
		d.Delay(1200 * time.Millisecond)
		return res
	}
	before, xpBefore := hitpointsXP(d), meleeXP(d)
	fightFrom := time.Now()
	defer EndFight(d, npc.Index, fightFrom)
	target := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if here := CurrentTile(d); here != nil && tile.Chebyshev(*here, target) > 1 {
		if !combatrunner.WalkNear(cd, target, 1, 30*time.Second) {
			return res
		}
	}
	if err := combatrunner.AttackNPC(cd, npc); err != nil {
		d.Logf("stage 5: attack: %v", err)
		return res
	}
	res.Fought = true
	var last combatplan.TargetState
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		now := combatrunner.ObserveTarget(cd, npc.Index)
		if now.Present {
			last = now
		}
		if v := combatplan.JudgeKill(last, now); v == combatplan.Killed || v == combatplan.LikelyKilled || v == combatplan.Vanished {
			res.Verdict = v
			// Stage 5 runs its own wait rather than combatrunner.FightOne's (see this
			// function's doc comment), so the OnKill hook Combat() sets never fires here.
			// Recording it on the same verdict keeps the evidence identical to the hook's:
			// combatplan.JudgeKill, not a disappearance.
			NoteKill(d, npc.Type, npc.X, npc.Z, npc.Level)
			// The cow left bones. Pick them up and bury them: it is this script's only
			// source of Prayer experience, and leaving them was throwing away both that and
			// the loot the panel never saw (owner, 2026-09-22).
			LootAndBuryBones(d, nil)
			LootValuables(d)
			break
		}
		if hp, ok := d.Session.State().HP(); ok && hp > 0 && hp <= hpFloor {
			res.BrokeOff, res.AttackerNames = true, AttackerNames(d)
			RetreatFromCows(d)
			break
		}
		if _, on := OnPenHazard(d); on { // the target led the account onto the pen's hazard
			res.AttackerNames = AttackerNames(d)
			StepOffPenHazard(d)
			break
		}
		if !d.Delay(300 * time.Millisecond) {
			break
		}
	}
	res.HPXP = hitpointsXP(d) - before
	if paid, bad := fleetplan.MeleeStyleMisfire(res.Skill, xpBefore, meleeXP(d)); bad {
		res.Misfire = paid
		st := d.Session.State()
		d.Logf("stage 5: style misfire — set for %s but the fight paid %s (combat tab %d, com_mode %d); re-selecting %s",
			res.Skill, paid, st.SideIcon(actions.CombatTab), st.CombatMode(), res.Skill)
		d.Delay(1200 * time.Millisecond) // past a bone's bury delay, which drops a button click
		if !combatrunner.ReselectTrainingStyle(cd, meleeCombatSkill(res.Skill)) {
			d.Logf("stage 5: could not re-select the %s style", res.Skill)
		}
	}
	return res
}

// meleeXP reads Attack, Strength and Defence XP for scarabplan.MeleeStyleMisfire.
func meleeXP(d *Deps) fleetplan.MeleeXP {
	xp := func(stat int) int {
		sk, ok := d.Session.State().Skill(stat)
		if !ok {
			return 0
		}
		return sk.XP
	}
	return fleetplan.MeleeXP{
		Attack:   xp(fleetplan.AttackStat),
		Strength: xp(fleetplan.StrengthStat),
		Defence:  xp(fleetplan.DefenceStat),
	}
}

// NearestReachableMonster is the nearest monster of that name in the training field that this
// account can actually walk to — combatrunner.Reachable, the same gate AJ's fighter reachability
// work (01049c2) put on AdvancedFighter and MeleeFlincher. At a magic-only pen it is the target
// this account can cast at from the pen's land (scarabplan.TrainPen.CastTile), nearest by the walk
// to where it would cast from, then by its own distance ("scarabducks", 2026-09-27). The cow field is fenced, and a Cow seen
// over the fence from outside it is nearest by distance and unreachable by any path shorter than
// the walk round to the gate; clicking it wedges the fight until the timeout.
//
// It also never returns a target somebody else is fighting ("scarabprod", 2026-09-23; see
// scarabplan/contest.go): content refuses the attack with "Someone else is fighting that." in
// single-way combat, and with a cohort at one pen that refusal used to cost a sixty-second wait per
// click. The target it does return is CLAIMED on the swarm's shared board until the fight ends, so
// two of the swarm's own accounts cannot pick the same free one in the same tick.
func NearestReachableMonster(d *Deps, monster string) (gamestate.NPC, bool) {
	cd := Combat(d)
	here := CurrentTile(d)
	pen := d.Pen()
	if d.TrainPen.Name != "" {
		monster = pen.Herd()
	}
	now := time.Now()
	sight := PackSight(d)
	var best gamestate.NPC
	bestDist, found := 0, false
	for _, n := range combatrunner.MonstersNear(cd, monster, pen.Stand, pen.Leash) {
		if Contested(d, n, now) {
			continue
		}
		t := tile.Tile{X: n.X, Z: n.Z, Level: n.Level}
		if !pen.SafeTarget(t) { // on a hazard's ground (the goblins' Moss giants, scarabplan.PenHazard)
			continue
		}
		if pen.MagicOnly {
			// A magic-only pen's herd is on water: the gate is a spell's reach from the pen's
			// land (castreach.go), and the distance is the walk to where the account casts from
			// -- nothing for a target in reach where it stands.
			cast, ok := pen.CastTile(here, t, sight)
			if !ok {
				continue
			}
			dist := 0
			if here != nil {
				// The walk first, then the Duck's own distance: of the ones in reach where the
				// account stands, the nearest is the least likely to swim out of it.
				dist = 100*tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, cast) + tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z}, tile.Tile{X: t.X, Z: t.Z})
			}
			if !found || dist < bestDist {
				best, bestDist, found = n, dist, true
			}
			continue
		}
		dist := 0
		if here != nil {
			dist = tile.Chebyshev(*here, t)
		}
		if found && dist >= bestDist {
			continue
		}
		if ok, _ := combatrunner.Reachable(cd, t); !ok {
			continue
		}
		best, bestDist, found = n, dist, true
	}
	if found && !ClaimTarget(d, best.Index) {
		return gamestate.NPC{}, false // another swarm account took it this instant; next pass
	}
	return best, found
}

// RetreatFromCows turns auto-retaliate off and walks out of the herd's reach, ending a fight: the
// Cow pen's CowRetreat, or the chosen pen's own retreat tile (pens.go).
func RetreatFromCows(d *Deps) {
	cd := Combat(d)
	combatrunner.EnsureRetaliate(cd, false)
	combatrunner.WalkNear(cd, PenRetreat(d), 1, 30*time.Second)
}

func hitpointsXP(d *Deps) int {
	sk, ok := d.Session.State().Skill(fleetplan.HitpointsStat)
	if !ok {
		return 0
	}
	return sk.XP
}

// WieldedWeapon is the weapon the account is holding, as the damage arithmetic needs it. An
// unrecognised weapon (or an empty hand, which an account whose sword was lost to a death has)
// reads as a Bronze sword, which only ever mis-prices the style choice slightly.
func WieldedWeapon(d *Deps) fleetplan.Weapon {
	for _, it := range WornItems(d) {
		if fleetplan.IsWildernessBlade(it.Name) && !strings.EqualFold(it.Name, fleetplan.KnifeItem) {
			return fleetplan.WeaponByName(it.Name)
		}
	}
	return fleetplan.BronzeSword
}
