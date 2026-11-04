package fleetplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// MagicStat is UPDATE_STAT's Magic index (combatrunner.MagicStat).
const MagicStat = 6

// DefaultTrainSpell is what training casts: Wind Strike — the only spell open from level 1, and
// with a Staff of air it costs one mind rune a cast and nothing else.
const DefaultTrainSpell = "Wind Strike"

// DefaultTrainCasts is how many training casts' runes one supply round stocks and one mule ask brings.
// 1,000 since 6 Oct 2026 (it was 300, set when 1 -> 13 was 1,833 Magic XP, about 280 Wind Strikes).
//
// THE OWNER, 6 Oct: "scarabswarm accounts could be bringing more runes when they train magic to save on bank
// trips constantly" and "definetly should be taking more runes with them when fighting npcs". The live fleet
// asked the mule for 212, 192 and 300 runes at a time (the casts to the next Magic level or the 300 cap,
// whichever was less), each ask a walk of 300 to 450 tiles each way to Catherby, 152 Stage 3 asks in the six
// and a half hours to 15:46. A round of 1,000 is about 50 minutes of casting at five ticks a cast and fits one
// kit trade (orderplan.OpenMaxPerOrder, scarabrunner's defaultKitPerOrder) and one pack slot (runes stack).
// It is bounded only by the casts to the goal the account is really training towards (RoundCasts), never by
// the next level, the next held step or the end of a slice.
const DefaultTrainCasts = 1000

// The training field: the ten Cows (level 2, 8 hitpoints, not aggressive) in the pen north of the
// East Ardougne market, read from maps/m41_52.jm2 (2657-2664,3341-3351). The nearest low-level,
// non-aggressive monster to the Stage 3 home — the Chickens at 2691,3273 are four, the Monkeys and
// Snakes by the zoo are behind its fences.
var (
	TrainMonster = "Cow"
	// THE FARMER AT THIS FIELD IS A BYSTANDER, NOT A THREAT, and that is worth writing down
	// because the 2026-09-22 handoff recorded the opposite ("a Farmer (level 7) in the DEATH
	// DIAG") and it sent one round of work chasing it.
	//
	// Three measurements, all from run bu32bqms8's own log:
	//   1. Only TWO of the six Stage 3 deaths that run were at this field at all. The other
	//      FOUR were at 2668,3312 -- the Baker's stall, during STAGE 2 -- with Guards (20) and
	//      a Hero (69) in view. "Six deaths, every one at the cow field" is wrong on both
	//      counts.
	//   2. The damage ticks here are ONE hitpoint at a time, which is a level-2 Cow's maximum,
	//      not a level-7 Farmer's.
	//   3. The Farmer is ABSENT from most damage ticks (2614, 2615, 2621, 2624, 2625, 2627 and
	//      2628 are all pure Cow) while the account keeps losing health. Something hitting the
	//      account every tick cannot be a thing that is usually not there.
	// Content agrees: guard1/farmer carry no huntmode or huntrange, so neither is aggressive.
	//
	// The cow-field deaths were death by a thousand cow hits on an account with NO FOOD, which
	// is the defect fixed in DecideStage3Phase and stage3GetFood. Do not re-open the Farmer.
	TrainStand = tile.Tile{X: 2660, Z: 3345}
	TrainLeash = 10
)

// StaffOfAir is the owner's upgrade staff: it makes every air rune free.
const StaffOfAir = "Staff of air"

// PlainStaff is Zaff's Staff (15 gp, 5 stock regrowing one a minute) — the fallback that still
// autocasts (category weapon_staff) but saves no rune.
const PlainStaff = "Staff"

// IsAutocastStaff reports whether an item name is a staff this stage can autocast with.
func IsAutocastStaff(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "staff" || n == "magic staff" || strings.HasPrefix(n, "staff of ")
}

// CurrentTarget is the spell and cast count Stage 3 is stocking runes for right now: the training
// spell below the Magic target, the KQ spell at or above it.
func (s Stage3Settings) CurrentTarget(magicLevel int) (Spell, int) {
	n := s.Normalised()
	if magicLevel < n.MagicTarget {
		sp, ok := SpellByName(n.TrainSpell)
		if !ok {
			sp, _ = SpellByName(DefaultTrainSpell)
		}
		return sp, n.TrainCasts
	}
	return n.TargetSpell(), n.CastsTarget
}

// ZaffStand is Zaff's shop floor in Varrock (npc 546 at 3203,3433).
var ZaffStand = tile.Tile{X: 3203, Z: 3433}

// ZaffStaffOfAirCost is the coins carried to Zaff for a Staff of air: obj cost 1500, and his
// shop's delta of 20 adds 2% per item below his base stock of 2 — 1530 with one left.
const ZaffStaffOfAirCost = 1600
