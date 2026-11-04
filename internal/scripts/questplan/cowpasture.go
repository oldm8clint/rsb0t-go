package questplan

// The Lumbridge cow pasture, east of the castle across the river: where the quest cow farms stand and how long they are given.
//
// WHY THIS FILE EXISTS. The pasture is a closed pen. Its west side is a fence (x 3252|3253 from z 3248 to 3271, joined by a diagonal
// one to the river bank) whose only opening is the double gate at 3253,3266 and 3253,3267, shut in the collision pack; the other
// sides are fences too. The farms asked the walker to arrive "within 8" (Demon Slayer) or "within 12" (the cows rungs) of the
// pasture's middle, 3259,3270, and the walker stops at the first tile inside the radius, which from the road is a tile OUTSIDE the
// gate (3252,3267 and 3251,3268 in the live log). Every cow is then behind a fence: the farm that picks cows by a walk over open
// ground finds none ("no cow in reach": 23,307 lines in the 18 October 2026 log, every one from 3252,3267 or 3251,3268, the loop
// never moving because both tiles are within 8 of the middle; 23,142 of them were one account's tight loop at the fleet's stop, the
// idle wait's refusal being ignored), and the one that picks by distance sends attacks the server answers "I can't reach that!".
// Eleven Demon Slayer rounds fetched 0 bones in 14 minutes each and parked three hours; the Prayer rungs' cows stints made +0 to
// +212 xp in twenty minutes (of 712 stints since 15 October 399 made nothing, the mean is 33 xp). The farms now stand INSIDE
// (CowPastureStand) and walk the pasture (CowPastureSweep) when the cows in view are taken.

import (
	"math"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

var (
	// CowPastureGates are the two leaves of the double gate in the pasture's west fence (loc 1551 at 3253,3266 and 1553 at 3253,3267,
	// "Gate", op Open, shape 0 angle 0). The tiles on the road side are one west of each.
	CowPastureGates = []tile.Tile{{X: 3253, Z: 3266}, {X: 3253, Z: 3267}}

	// CowPastureStand is a tile inside the pen, 5 tiles east of the gate's inner side and 4 from its middle: the farms walk here, with
	// CowPastureEnterRadius, to come in. Every tile within the radius of it is inside the pen too (tested on the real pack).
	CowPastureStand = tile.Tile{X: 3258, Z: 3266}

	// CowPastureSweep is the beat a farm walks when no cow in view can be fought, a ring of four stops (the middle one twice) each
	// within the 15 tiles the client sees of the cows' spawns: the south pen, the middle, the north-west corner and the north-east.
	CowPastureSweep = []tile.Tile{{X: 3258, Z: 3262}, {X: 3257, Z: 3274}, {X: 3249, Z: 3288}, {X: 3258, Z: 3291}, {X: 3257, Z: 3274}}

	// CowPastureSpawns are the ten cows (npc 81; m50_50 and m50_51 of the content maps) the pasture holds. A cow takes
	// CowRespawnSeconds to come back, so ten spawns are all the cows the whole fleet has here.
	CowPastureSpawns = []tile.Tile{{X: 3243, Z: 3295}, {X: 3244, Z: 3289}, {X: 3247, Z: 3284}, {X: 3254, Z: 3258}, {X: 3255, Z: 3278},
		{X: 3256, Z: 3292}, {X: 3258, Z: 3260}, {X: 3261, Z: 3259}, {X: 3261, Z: 3270}, {X: 3261, Z: 3292}}
)

const (
	// CowPastureEnterRadius is how near the stand a walk into the pen may stop. The fence is 5 tiles west of the stand, so a stop
	// anywhere within 3 is inside.
	CowPastureEnterRadius = 3

	// CowKillSeconds is a cow's whole cycle for one account: the click, the walk to it, about two blows (it has 8 hitpoints and a
	// defence bonus of -21), the death, the bones taken and, on a Prayer rung, buried. Measured: the best of 712 Prayer rung cow
	// stints in the fleet's logs of 15 to 18 October 2026 made 212 xp (47 bones, 4.5 xp each) in 20 minutes 15 seconds, which is
	// 25.8 seconds a cow with the walk in and the other accounts' competition in it; 25 is that, rounded down.
	CowKillSeconds = 25

	// CowRespawnSeconds is a cow's respawn (respawnrate=90 ticks in the npc config, 54 seconds).
	CowRespawnSeconds = 54

	// CowWindowMargin is how much longer than the expected time a farm's window is, for a slow start and a bad run of fights.
	CowWindowMargin = 1.35

	// CowWindowFloor is the shortest window a farm is given: the walk in and one sweep of the pen take a minute or two.
	CowWindowFloor = 10 * time.Minute

	// DemonCowFarmers is how many accounts Demon Slayer's bones are planned to share the pen with: its own fleet slot is three
	// (questrunner exclusiveQuests) and one more is allowed for the Prayer rungs' cows stints.
	DemonCowFarmers = 4
)

// CowSupplyPerMinute is how many cows the pen gives in a minute to everyone together: each spawn comes back CowRespawnSeconds after
// its cow is killed, and the kill takes CowKillSeconds more, so a spawn gives one cow per CowRespawnSeconds + CowKillSeconds.
func CowSupplyPerMinute() float64 {
	return float64(len(CowPastureSpawns)) * 60 / float64(CowRespawnSeconds+CowKillSeconds)
}

// CowRatePerMinute is one account's cows a minute when farmers accounts share the pen: its own pace (CowKillSeconds a cow) or its
// share of the supply, whichever is less.
func CowRatePerMinute(farmers int) float64 {
	farmers = max(farmers, 1)
	return math.Min(60/float64(CowKillSeconds), CowSupplyPerMinute()/float64(farmers))
}

// CowFarmEstimate is the expected time to kill kills cows among farmers accounts.
func CowFarmEstimate(kills, farmers int) time.Duration {
	if kills <= 0 {
		return 0
	}
	return time.Duration(float64(kills) / CowRatePerMinute(farmers) * float64(time.Minute))
}

// CowExpectedKills is the cows an account kills in window among farmers accounts.
func CowExpectedKills(window time.Duration, farmers int) int {
	return int(window.Minutes() * CowRatePerMinute(farmers))
}

// CowFarmWindow is the window a farm for kills cows among farmers accounts is given: the expected time with CowWindowMargin on
// it, in whole minutes, and at least CowWindowFloor. A farm ends as soon as it has what it came for, so the margin costs nothing
// when the cows are there and is what a crowded pen needs when they are not.
func CowFarmWindow(kills, farmers int) time.Duration {
	w := time.Duration(math.Ceil(CowFarmEstimate(kills, farmers).Minutes()*CowWindowMargin)) * time.Minute
	return max(w, CowWindowFloor)
}

// CowStints is how many stints of window, among farmers accounts, the cows need to give xpNeed experience of Prayer by their bones
// (4.5 xp a bone): what a Prayer rung is, in the units the fleet's slots come in. A rung whose number is in the dozens is not one
// the cows finish.
func CowStints(xpNeed int, window time.Duration, farmers int) int {
	if xpNeed <= 0 {
		return 0
	}
	xp10 := 45 // bones.obj bone_exp: 4.5 xp a burial
	for _, k := range BoneKinds {
		if k.ID == ObjBones {
			xp10 = k.XP10
		}
	}
	bones := (xpNeed*10 + xp10 - 1) / xp10
	per := max(CowExpectedKills(window, farmers), 1)
	return (bones + per - 1) / per
}

// DemonBonesWindow is the time one farming round for Traiborn's bones is given (questmods demonBones).
func DemonBonesWindow() time.Duration { return CowFarmWindow(DemonBonesNeeded, DemonCowFarmers) }
