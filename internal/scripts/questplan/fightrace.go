package questplan

import (
	"fmt"
	"time"
)

// The empirical half of "is this fight being lost": no foe stats, no model, only what the fight itself has
// shown so far. A fight is a race between the hitpoints the account can spend (its own and its food's) and
// the foe's, so once a fight has run long enough to have a pace, the rest of it can be priced from that
// pace: the foe's remaining hitpoints over the rate the account has been taking them off is how long is
// left, and the rate the account has been losing hitpoints at over that is what it costs.
//
// WHY: the entry gates (fightodds.go) can only price what the config says, and the two 16 Oct 2026 deaths
// had one thing the config did not: Merlin's account meleed Sir Mordred for three minutes without taking a
// tenth of his 38 hitpoints off while eating twenty bites, and the Fight Arena account went from 20 to 0
// hitpoints in forty seconds with 22 pieces of food in the pack. A race that is lost is visible in the first
// half minute; neither loop looked.

const (
	// RaceWarmup is how long a fight runs before its pace is believed.
	RaceWarmup = 20 * time.Second
	// RaceNoProgress is how long a fight may go on without the target's health bar moving once the account
	// has lost a third of what it can spend.
	RaceNoProgress = 60 * time.Second
	// RaceMargin is how many times the projected cost the hitpoints and food must cover for the fight to go on.
	RaceMargin = 1.15
)

// ScriptedLoss reports whether the fight against the npc of this id is one the content scripts to be lost: the
// fight loops judge no race against it (DecideFight, FightReading.Scripted), because walking away from it would
// give up the pass. Today that is Koschei's fourth form: 255 hitpoints that a bare-handed account will never
// take off, and the trial is passed by the account's hitpoints landing on exactly 0 inside the arena, so a race
// "lost" at a minute and a retreat to the bank (a counted near-death that parks the quest) would be wrong whether
// or not the honour death was about to land. A form is read off the npc now in view, not off the one the fight
// began with: the first three forms change into the next under the same npc index within one fight.
func ScriptedLoss(npcID int) bool { return npcID == NpcKoscheiFourth }

// FightRace sums the hitpoints an account has lost over a fight, poll by poll. A poll that finds
// the hitpoints higher than the last (a bite, regeneration) adds nothing, so a bite that lands in the
// same tick as a hit hides the hit: the sum is a floor on the damage taken, which only ever makes the race look
// closer than it is.
type FightRace struct {
	started bool
	start   time.Time
	hpPrev  int
	taken   float64
}

// Observe folds one poll in. hp 0 (not read yet) is skipped.
func (r *FightRace) Observe(now time.Time, hp int) {
	if !r.started {
		r.started, r.start = true, now
	}
	if hp <= 0 {
		return
	}
	if r.hpPrev > 0 && hp < r.hpPrev {
		r.taken += float64(r.hpPrev - hp)
	}
	r.hpPrev = hp
}

// Elapsed is how long the race has run at now.
func (r *FightRace) Elapsed(now time.Time) time.Duration {
	if !r.started {
		return 0
	}
	return now.Sub(r.start)
}

// Taken is the hitpoints lost so far.
func (r *FightRace) Taken() float64 { return r.taken }

// RaceReading is one look at a race.
type RaceReading struct {
	Elapsed time.Duration
	// Taken is the hitpoints the account has lost since the fight began (FightRace.Taken).
	Taken float64
	// FoeHealth and FoeTotal are the target's health bar as the last hitmark on it drew it (Total 0 until one
	// has landed).
	FoeHealth, FoeTotal int
	// HP is the account's hitpoints now, FoodHeal what the food it still holds restores in all.
	HP, FoodHeal int
	// PeakHit is the foe's hardest hit when known (0 when not): the hitpoints kept back for the walk away.
	PeakHit int
}

// RaceLost judges a race: false until the fight has run RaceWarmup and cost the account at least a hit, then
//   - lost, when the target's bar has moved and the pace so far says finishing it costs RaceMargin times what
//     the account's hitpoints and food can pay (less the hits kept back), or
//   - lost, when the bar has not moved in RaceNoProgress and the fight has cost a third of what the account
//     can pay: the Mordred fight, and every fight an eating account is not winning.
func RaceLost(r RaceReading) (bool, string) {
	if r.Elapsed < RaceWarmup {
		return false, ""
	}
	reserve := float64(max(r.PeakHit, 3))
	if r.Taken < reserve {
		return false, ""
	}
	secs := r.Elapsed.Seconds()
	pool := float64(r.HP + r.FoodHeal)
	usable := pool - reserve
	progress := 0
	if r.FoeTotal > 0 {
		progress = r.FoeTotal - r.FoeHealth
	}
	if progress <= 0 {
		if r.Elapsed >= RaceNoProgress && r.Taken*3 >= pool {
			return true, fmt.Sprintf("%.0f hitpoints lost in %s and the target's health bar has not moved (the account has %d hitpoints and food worth %d more)",
				r.Taken, r.Elapsed.Round(time.Second), r.HP, r.FoodHeal)
		}
		return false, ""
	}
	killRate := float64(progress) / secs
	cost := r.Taken / secs * (float64(r.FoeHealth) / killRate)
	if cost*RaceMargin > usable {
		return true, fmt.Sprintf("at this pace (%.0f hitpoints lost and %d of the target's %d taken off in %s) the other %d cost about %.0f more, and the account has %d hitpoints and food worth %d",
			r.Taken, progress, r.FoeTotal, r.Elapsed.Round(time.Second), r.FoeHealth, cost, r.HP, r.FoodHeal)
	}
	return false, ""
}
