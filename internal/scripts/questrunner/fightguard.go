package questrunner

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// What every quest fight needs before and during the blows, in one place: the hitpoints to start at, the eat
// line from the foe, a look at the hitpoints often enough to use it, and the pace of the race
// (questplan/fightodds.go and fightrace.go have the rules and the live evidence).
//
// The shared loops (execFight, FightByMagic) take these from the target they are sent after; a quest's own
// loop (Fight Arena's casts, Merlin's Sir Mordred) builds a FightGuard and asks it.

// defaultBite is what a bite of a food the tables do not know is assumed to restore.
const defaultBite = 8

// pickBite chooses the food to eat to close deficit hitpoints: the one whose bite is the largest that fits,
// so a shark is not wasted on four missing hitpoints, else the smallest bite there is.
func pickBite(pack map[int]int, foods []int, deficit int) (int, bool) {
	best, bestHeal, found := 0, 0, false
	fits := false
	for _, id := range foods {
		if pack[id] <= 0 {
			continue
		}
		heal := questplan.FoodBiteHeal(id)
		if heal == 0 {
			heal = defaultBite
		}
		switch {
		case heal <= deficit && (!fits || heal > bestHeal):
			best, bestHeal, found, fits = id, heal, true, true
		case !fits && (!found || heal < bestHeal):
			best, bestHeal, found = id, heal, true
		}
	}
	return best, found
}

// TopUp eats until the account's hitpoints reach target (or its maximum), choosing each bite by the
// hitpoints still missing, and returns the bites eaten. It stops when the food runs out, when three bites in
// a row do not raise the hitpoints (a dialogue open, the food's delay), or when the bot is stopping. A
// leftover dialogue is closed first: while a modal is up the engine drops the click on the food.
//
// This is what the fights lacked on 16 Oct 2026: the Fight Arena account came out of Hengrad's cell at 19 of
// 53 with 22 pieces of cake and went on to the next beast at that.
func TopUp(d *Deps, foods []int, target int) int {
	// The quest's own tools are not top-up food (Merlin's bread is the beggar's, Death Plateau's trout the ale's
	// change): EatFloor leaves them alone for the same reason.
	var edible []int
	for _, id := range foods {
		if !d.isQuestTool(id) {
			edible = append(edible, id)
		}
	}
	foods = edible
	if len(foods) == 0 || target <= 0 {
		return 0
	}
	if hp, max := Hitpoints(d); max == 0 || hp >= target || hp >= max {
		return 0
	}
	ClearLeftoverModal(d)
	ate, fails := 0, 0
	for i := 0; i < 28 && fails < 3; i++ {
		hp, max := Hitpoints(d)
		if max == 0 || hp >= target || hp >= max {
			break
		}
		id, ok := pickBite(PackCounts(d), foods, min(target, max)-hp)
		if !ok {
			break
		}
		d.eatMu.Lock()
		err := OpHeldByName(d, id, "Eat")
		if err != nil {
			d.eatMu.Unlock()
			d.logf("eating %s: %v", ObjName(d, id), err)
			fails++
			if !d.Delay(600 * time.Millisecond) {
				break
			}
			continue
		}
		d.eaten.Add(1)
		waitQuietly(d, func() bool { h, _ := Hitpoints(d); return h > hp }, 2*time.Second)
		h, _ := Hitpoints(d)
		d.eatMu.Unlock()
		d.logf("fight: topping up before the blows: ate %s at %d/%d hitpoints, now %d (aiming for %d)", ObjName(d, id), hp, max, h, target)
		if h > hp {
			ate++
			fails = 0
		} else {
			fails++
		}
		if !d.Delay(300 * time.Millisecond) {
			break
		}
	}
	return ate
}

// VsFoe is the sustain with its eat line raised for the foe a fight is against (questplan.FoeEatLine) and,
// for a foe the tables know, the foe's hardest hit; an unknown foe leaves the sustain as it was.
func VsFoe(d *Deps, sus questplan.Sustain, npcID int) (questplan.Sustain, questplan.MeleeFoe, bool) {
	foe, ok := questplan.FoeByNPC(npcID)
	if !ok {
		return sus, questplan.MeleeFoe{}, false
	}
	_, maxHP := Hitpoints(d)
	return sus.VsFoe(foe, maxHP), foe, true
}

// PrepareForFight tops the account up to the level a fight with a foe whose hardest hit is peakHit starts at
// (questplan.FightStartHP; 0 is unknown, which asks for four fifths of the maximum) and returns the bites it
// ate.
func PrepareForFight(d *Deps, foods []int, peakHit int) int {
	_, maxHP := Hitpoints(d)
	return TopUp(d, foods, questplan.FightStartHP(maxHP, peakHit))
}

// FightGuard is a quest's own fight loop's view of the rules: build one per foe with NewFightGuard, call
// Prepare before the first blow, Eat (or WaitCast, which eats) between blows, and Poll once a look to learn
// whether the race is lost.
type FightGuard struct {
	d     *Deps
	foe   questplan.MeleeFoe
	known bool
	sus   questplan.Sustain
	w     fightWatch
	// scripted is whether the content scripts the account to lose to this foe (questplan.ScriptedLoss).
	scripted bool
}

// NewFightGuard guards a fight with the npc of this id; an id the tables do not know is guarded with the
// module's own eat line and the race alone.
func NewFightGuard(d *Deps, foeID int, sus questplan.Sustain) *FightGuard {
	g := &FightGuard{d: d, sus: sus, scripted: questplan.ScriptedLoss(foeID)}
	if s, foe, ok := VsFoe(d, sus, foeID); ok {
		g.sus, g.foe, g.known = s, foe, true
		g.w.peak = foe.PeakHit()
	}
	return g
}

// Sustain is the module's sustain with the eat line the foe asks for.
func (g *FightGuard) Sustain() questplan.Sustain { return g.sus }

// Foe is the foe's content stats, false for a foe the tables do not know.
func (g *FightGuard) Foe() (questplan.MeleeFoe, bool) { return g.foe, g.known }

// Prepare tops the account up to the fight's starting level and returns the bites it ate.
func (g *FightGuard) Prepare() int { return PrepareForFight(g.d, g.sus.FoodIDs, g.w.peak) }

// Eat takes a bite when the account is at or under the foe's eat line.
func (g *FightGuard) Eat() bool { return EatIfNeededVs(g.d, g.sus.FoodIDs, g.sus.EatAt, g.w.peak) }

// Poll folds the account's hitpoints and the target's health bar (as the last hitmark on it drew it; 0, 0 until
// one has) into the race and returns the verdict: FightRetreat when the race is lost, or the food is down to
// its last piece at the eat line. The re-attack half of the verdict is not meaningful here.
func (g *FightGuard) Poll(health, total int) questplan.FightVerdict {
	d := g.d
	hp, maxHP := Hitpoints(d)
	pack := PackCounts(d)
	v := fightView{Alive: true, Health: health, Total: total, HP: hp, MaxHP: maxHP,
		Food: g.sus.FoodHeld(pack), FoodHeal: questplan.FoodPoolHeal(pack, g.sus.FoodIDs), Eaten: int(d.eaten.Load()), Scripted: g.scripted}
	return questplan.DecideFight(g.w.observe(time.Now(), v, g.sus.EatAt))
}

// MeleeWinnable asks whether the account can finish the guarded foe by its own swing from here
// (questplan.MeleeOdds), with foeHealth the foe's hitpoints left (0: all of them). A foe the tables do not
// know is not judged: true.
func (g *FightGuard) MeleeWinnable(foeHealth int) (bool, string) {
	if !g.known {
		return true, ""
	}
	d := g.d
	hp, maxHP := Hitpoints(d)
	if maxHP == 0 {
		return true, ""
	}
	lv := BaseLevels(d)
	p := questplan.MeleeFighter{Attack: lv["attack"], Strength: lv["strength"], Defence: max(lv["defence"], 1), Magic: lv["magic"]}
	if foeHealth <= 0 {
		foeHealth = g.foe.Hitpoints
	}
	return questplan.MeleeOdds(g.foe, p, hp, maxHP, foeHealth, questplan.FoodBites(PackCounts(d), g.sus.FoodIDs), g.sus.EatAt)
}

// WaitCast waits out one combat cast's action delay (WaitCast), looking at the hitpoints every 300 ms and
// taking a bite when they are at or under the eat line, instead of once per cast: a cast is five ticks and a
// beast that hits every four takes a bite's worth between two looks. False when the bot is stopping.
func (g *FightGuard) WaitCast() bool { return waitCastEating(g.d, g.sus) }

// WaitCastEating is WaitCast with a bite whenever the account is at or under sus's eat line, looked for every
// 300 ms: for a quest's own cast loop that eats between casts and would otherwise look once per cast.
func WaitCastEating(d *Deps, sus questplan.Sustain) bool { return waitCastEating(d, sus) }

// waitCastEating is WaitCast with a bite whenever the account is at or under sus's eat line.
func waitCastEating(d *Deps, sus questplan.Sustain) bool {
	tick := d.Session.State().Tick
	start := tick()
	deadline := time.Now().Add(castInterval)
	for tick()-start < castTicks && time.Now().Before(deadline) {
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
		EatIfNeeded(d, sus.FoodIDs, sus.EatAt)
	}
	return true
}
