package combatplan

import "rsb0t-go/internal/scripts/tile"

// The food break-off decision, kept pure so it can be tested without a session.
//
// Why this exists at all: the trainer used to bank only when its food reached ZERO, and only
// then if its hitpoints were already under the eat threshold. A measured long run
// (-mode long -buygear=false) shows what that costs — all five of its deaths sit in the last
// seventy log lines, immediately before the run noticed it was out of food and stopped. The run
// fought its food all the way down, died five times while doing it, and only afterwards went to
// the bank to discover it was empty.
//
// A clean stop when the bank is empty is the LAST line of defence. The first one is leaving the
// rung while there is still food in the pack to eat on the way, which is what a player does and
// what these two functions decide.

// FoodReserve is how many bites a run keeps in hand as its trigger to break off for the bank.
//
// A quarter of a full load, never fewer than two. Two because one bite is a single heal and the
// walk to the bank passes whatever the rung is made of; a quarter because the reserve has to
// scale with the rung's own danger, and FoodPerTrip already sizes a load from the monster's max
// hit. It is always at least one below a full load, so a trip that fills the pack cannot come
// back still needing one — that would be a bank spin, which is bug class 2 in
// docs/script_porting_framework.md and a shape this script has already grown twice.
func FoodReserve(perTrip int) int {
	r := perTrip / 4
	if r < 2 {
		r = 2
	}
	if r > perTrip-1 {
		r = perTrip - 1
	}
	if r < 0 {
		r = 0
	}
	return r
}

// ShouldRestock reports whether a run holding have bites should break off to the bank now,
// while it still has food, rather than fighting on until it has none.
func ShouldRestock(have, perTrip int) bool {
	return have <= FoodReserve(perTrip)
}

// ---- what a trip costs, and so what it should carry ----

// DungeonZOffset is where this world keeps its dungeons: on level 0, 6400 tiles south in z of
// the surface square they sit under. Nothing on this ladder is a real level change, which is
// why a raw Chebyshev between a dungeon stand and its surface bank reads about 6,345 tiles and
// is meaningless. Every distance below folds that offset away first.
const DungeonZOffset = 6400

// ClimbTiles is what one trapdoor or manhole is worth in walking time.
//
// It is a charge rather than a measurement, and it exists because tile distance cannot see the
// thing that makes the giants rung expensive: the route out is a walk to the ladder, an op, a
// wait for the hop to land, and then a second walk on the other side. Forty tiles is the order
// of the walk to the Edgeville trapdoor from the stand and back to open ground, so a rung that
// banks through one is charged roughly double what its folded tile distance says.
const ClimbTiles = 40

// Underground reports whether a tile is on the dungeon layer.
func Underground(t tile.Tile) bool { return t.Z >= DungeonZOffset }

// surfaceOf folds a dungeon tile back onto the surface square above it.
func surfaceOf(t tile.Tile) tile.Tile {
	if Underground(t) {
		t.Z -= DungeonZOffset
	}
	return t
}

// RouteTiles is how far apart two tiles really are for a walker: Chebyshev with the dungeon
// layer folded away, plus a climb charge when exactly one end is underground.
//
// It is an estimate and it is deliberately not a pathfinder call. Both callers want a cheap,
// pure number that can be reasoned about in a test — how much food a trip is worth carrying,
// and how long a walk should be given before it is called failed — and neither wants to plan a
// route to find out.
func RouteTiles(a, b tile.Tile) int {
	d := tile.Chebyshev(surfaceOf(a), surfaceOf(b))
	if Underground(a) != Underground(b) {
		d += ClimbTiles
	}
	return d
}

// TripTiles is the round trip from a rung's stand to its bank and back.
func TripTiles(r Rung) int { return 2 * RouteTiles(r.Stand, r.Bank) }

// PackKeepSlots is how many pack slots a food load leaves free for everything that is not food.
//
// Coins for the tolls, a big bone between the drop and the burial, arrows a Ranged run picks
// back up, and headroom. Eight rather than four because the slots have to still be free at the
// WORST moment of the cycle, not on average: a full pack is its own reason to bank, so a load
// that fits exactly would answer a couple of un-buried bones with a round trip to the surface,
// which is the cost this whole change exists to remove.
const PackKeepSlots = 8

// TilesPerExtraBite is how much round trip buys one more bite of food.
//
// The number that matters is not this one but what it produces: at the Al Kharid rung, whose
// bank is 20 tiles from its stand, a load of 8; at the giants rung, whose bank is on the surface
// while the fight is underground, a full pack. Ten tiles a bite is simply the slope that puts
// those two where they belong.
const TilesPerExtraBite = 10

// BreakOffLoad is the number the break-off reserve is actually a fraction of, which is not always
// the load a trip asks for.
//
// The two came apart the moment loads were sized from the walk, and the result was measured live
// rather than reasoned about: `-mode train` on a fresh account at the cows rung, holding 5 Trout
// with an EMPTY bank, made **29 bank trips in five minutes with 0 kills** and its melee XP frozen
// at 1,164 while it stood at the Varrock booth. Neither rule was wrong by itself. FoodLoad asks
// the cows rung for 20 bites because its bank is 125 tiles away, so FoodReserve is 5 and a pack
// holding 5 is permanently "down to the reserve"; while the scarcity rule that ENDS a run is
// judged against the danger floor of 4, whose reserve is 2, so five bites is a working supply and
// the run is never allowed to stop. Having to bank forever and never being allowed to stop is a
// spin, and it is the fourth this script has grown.
//
// A bank that has already refused to fill a trip-sized load has answered the question: that
// number is not available here. So it stops being what the break-off is measured against, and the
// danger floor takes over until a later trip proves the bank can pay for the walk again. The run
// then fights its food down to the margin the MONSTER justifies and reaches the clean stop there,
// which is what the food policy was always for.
func BreakOffLoad(tripLoad, dangerFloor int, bankCanFillTrip bool) int {
	if bankCanFillTrip {
		return tripLoad
	}
	if dangerFloor < 1 {
		return 1
	}
	return dangerFloor
}

// TripLoad is what one completed bank trip proved about the trip-sized load: whether the run can
// still carry it, and if not, what stopped it.
type TripLoad struct {
	// Reached is whether the trip came away holding the whole load it asked for.
	Reached bool
	// Why names what stopped it, empty when it was reached. It is printed, so it is a phrase
	// rather than a code.
	Why string
}

// JudgeTripLoad reports whether a completed trip reached the load it asked for.
//
// The signature's whole point is the argument it does NOT have an exemption for. The first
// version of this rule lived inline in combattrainer.bankStep as
// `got < want && !wd.PackFull`, and that `!wd.PackFull` is the hole the FIFTH zero-progress
// bank loop came through. The reasoning behind it was sound and the conclusion was wrong: a
// withdrawal refused for want of pack space says nothing about the BANK, so the bank was not
// blamed — but the number the break-off is measured against is not a fact about the bank, it is
// a fact about what this run can actually carry. A pack with no room for twenty bites cannot
// carry twenty bites whatever the bank holds.
//
// The measured cost of the exemption, 2026-09-20 at the giants rung: the trip load is 20 and
// its reserve 5, while the danger floor is 6 and its reserve 2. Holding 3, 4 or 5 bites is
// therefore "at the reserve, go to the bank" and "a working supply, not allowed to stop" at the
// same time — a three-bite window in which both rules are individually right and the run can
// only walk in circles. foodShort exists to collapse that window by dropping the break-off to
// the danger floor, and the pack-full exemption stopped it latching in precisely the case that
// produces it. The run made 351 bank trips and stood at the Edgeville bank for 44m18s with its
// melee XP frozen, about 20% of a 3h42m window.
//
// So every refusal latches. A pack that gets room back, or a bank that fills a whole load
// again, clears it on the next trip — the rule is a latch, not a verdict on the run.
func JudgeTripLoad(got, want int, packFull, notInBank bool) TripLoad {
	if got >= want {
		return TripLoad{Reached: true}
	}
	switch {
	case packFull:
		return TripLoad{Why: "the pack had no room for the rest of it"}
	case notInBank:
		return TripLoad{Why: "the bank holds none of it at all"}
	default:
		return TripLoad{Why: "the bank could not supply the rest of it"}
	}
}

// XPPerHPHealed is how much melee XP one hitpoint of healing bought on the ladder, measured
// rather than modelled.
//
// From the 2026-09-20 end-to-end run, which is the only complete reading of this that exists:
// 800 Trout, at 7 hitpoints a bite, carried a fresh account to 67,448 melee XP across the cows,
// Al Kharid and giants rungs before the food policy stopped it. That is 84 XP a bite and 12 XP
// per hitpoint healed. It is a whole-ladder average and it deliberately does not model the rung,
// the gear or the eat threshold: the number it feeds is an expectation to state out loud, not a
// promise, and a wrong-by-a-third expectation stated at minute one is still worth incomparably
// more than a silent stop three hours later.
const XPPerHPHealed = 12

// FoodBudgetMarginPercent is how much is added on top of the bare estimate.
//
// Twenty per cent, because every error this estimate can make costs the same thing — a run that
// stops short — and the supply is staged once by an operator who would rather over-stage than
// repeat a three-hour window. It is not free: food sits in a bank, not in the pack, so the only
// cost of over-staging is the staging itself.
const FoodBudgetMarginPercent = 20

// FoodBudget is how many bites a run needs to reach a goal that is xpNeeded melee XP away.
//
// This answers the second defect the 2026-09-20 run left: 800 Trout reached Defence 42 of a
// wanted 45, and nobody could say beforehand that it would fall short. There is no restock path
// to fall back on and that is a property of the content rather than a gap in this script —
// every cooked-food stock line in a shop a free-to-play combat account can reach is 0, and
// World.ts:1183 tops a line up only TOWARD its stockcount, so a 0 never rises. With the mule
// descoped, the bank supply staged at the start is the whole supply, so the honest fix is to
// size it from the target and to say what it is sized for.
func FoodBudget(xpNeeded, healPerBite int) int {
	if xpNeeded <= 0 {
		return 0
	}
	if healPerBite < 1 {
		healPerBite = 1
	}
	bites := xpNeeded / (XPPerHPHealed * healPerBite)
	bites += bites * FoodBudgetMarginPercent / 100
	if bites < 1 {
		bites = 1
	}
	return bites
}

// FoodLoad is how much food one trip should actually carry: the danger floor FoodPerTrip sizes
// from the monster, plus a term for how far the trip is, clamped to the room the pack has.
//
// Why this exists, measured rather than argued. FoodPerTrip sizes a load from the monster's max
// hit alone, with no term for the walk or for free pack space, and on 2026-09-20 that was the
// whole reason the 1-to-45 run could not finish. The giants rung stands underground at 3113,9836
// and banks on the surface at 3095,3491, so every restock is a dungeon exit, a surface walk, the
// bank and the trapdoor back down — and FoodPerTrip sent it out with six bites, a usable band of
// four, while 25 pack slots sat empty for the entire run. The measured result was 217 travel
// lines against 30 combat lines, 1.2 kills per bank trip, and 149 XP/min endpoint-measured
// against 464-844 at Al Kharid: a rung worth double the XP per kill training at a fifth of the
// rate, with 0 deaths, 0 stalls and 0 respawn waits. Nothing was broken. It was commuting.
//
// So the trip's own cost now informs the load. A rung whose bank is next door keeps the number
// the monster justifies; a rung that has to climb out of a dungeon to reach its bank fills the
// pack, because the slots were free the whole time and the walk is the same length whether it
// carries four bites or twenty.
func FoodLoad(r Rung, healPerBite, packSize int) int {
	floor := FoodPerTrip(r, healPerBite)
	room := packSize - PackKeepSlots
	if room < floor {
		// A pack too small to hold even the danger floor is not a reason to carry less than the
		// monster can hit for. The floor wins and the caller lives with a tight pack.
		room = floor
	}
	want := floor + TripTiles(r)/TilesPerExtraBite
	if want > room {
		want = room
	}
	if want < floor {
		want = floor
	}
	return want
}

// HerdFoodMultiple is how many times the single-attacker danger floor (FoodPerTrip) a herd rung needs in the
// pack: the guardian's own bites while an event beside the account is handled come out of the same pack, and
// they are not rationed by the trainer's eat threshold.
const HerdFoodMultiple = 3

// HerdFoodFloor is the fewest bites a trip may come away with for a Herd rung to be trained at, 0 for any other
// rung. The Asgarnian ice dungeon's Ice warrior cave sent accounts in with 11 and 5 of the 20 bites its trip is
// sized for ("fight on at the 5-bite danger floor") and both ate the whole pack in three to ten minutes of
// cornered bites against an event monster and the herd, then died at food 0 (16 and 17 Oct 2026).
func HerdFoodFloor(r Rung, healPerBite int) int {
	if !r.Herd {
		return 0
	}
	return min(HerdFoodMultiple*FoodPerTrip(r, healPerBite), FoodLoad(r, healPerBite, 28))
}

// HerdFoodShort is the start of the end reason of a run that left a Herd rung for want of a food load (AvoidEnd
// leaves the rung alone afterwards).
const HerdFoodShort = "food short at an aggressive rung"
