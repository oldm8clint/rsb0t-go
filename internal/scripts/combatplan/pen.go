package combatplan

// A rung whose ground is a fenced pen: the Lumbridge cow pasture.
//
// WHY THIS FILE EXISTS. The pasture east of the castle is a closed pen. Its west side is a fence (x 3252|3253 from z 3248 to 3271,
// joined by a diagonal one to the river bank) with one opening, the double gate at 3253,3266 and 3253,3267, shut in the collision
// pack; the other sides are fences too. The quest farms learnt that on 18 October 2026 (questplan/cowpasture.go: eleven Demon
// Slayer rounds fetched no bones, 399 of 712 Prayer rung stints made no experience) and now stand inside, walk in by the gate and
// pick only the cows a walk over open ground reaches. The trainer's "cows" rung is the same pasture and had none of that:
//
//   - it counted an account as AT the rung when it stood within the Leash of the Stand (Chebyshev), and 55 floor tiles of the old
//     leash (the road north of the pen's north fence, z 3299 to 3302) are outside the pen, so an account displaced there (a random
//     event, a walk cut short) never walked in: every cow read "no route", the door planner's way round to the gate is longer than
//     DoorRouteMaxCost, and with one cow in view the recovery that walks back to the stand (it wants two) never ran;
//   - its Stand was at the pen's far north end with a leash of 8, so five of the seven spawns it listed (and so counted as supply, and
//     seated accounts by) were outside the leash, a cow of them picked only when it had wandered in; the three spawns by the gate
//     were not listed at all.
//
// The rung now carries the pen (Rung.Pen): the trainer walks in by the gate to Enter, is "at the rung" only while a walk over open
// ground reaches Enter, picks the nearest reachable cow by that walk, and walks the pen's Beat when none is in reach, as the quest
// farms do. The figures below are questplan's (CowPastureGates, CowPastureStand, CowPastureEnterRadius, CowPastureSweep,
// CowPastureSpawns): this package cannot import questplan (questplan imports it), so an external test holds the two to be equal, and
// the combatrunner real-pack tests hold the pen to the collision pack.

import "rsb0t-go/internal/scripts/tile"

// Enclosure is a fenced pen a rung's monsters stand in.
type Enclosure struct {
	// Gates are the leaves of the way in, shut in the collision pack and opened by the walker. The trainer does not need them to
	// walk (the walker finds the gate); they are what a reader of the log and the tests check the pen against.
	Gates []tile.Tile
	// Enter is a tile inside the pen. A walk in is to Enter with EnterRadius, and every floor tile within EnterRadius of Enter is
	// inside too, so the walker cannot call the walk done on the road side of the fence (stopping at the first tile inside a larger
	// radius did exactly that).
	Enter       tile.Tile
	EnterRadius int
	// Beat is the ring of stops walked when no monster can be reached, each within the client's view of the spawns around it.
	Beat []tile.Tile
}

// BeatNear is how near a stop of a Beat may be to the account and still be skipped for the next one: a walk of five tiles shows the
// account nothing new.
const BeatNear = 5

// NextBeat is the stop of the beat the account walks to next: the one after *from (an index into the beat, advanced) that is not
// within BeatNear of here. ok is false when every stop is (the account is somewhere in the middle of a short beat).
func (e Enclosure) NextBeat(from *int, here tile.Tile) (tile.Tile, bool) {
	for i := 0; i < len(e.Beat); i++ {
		stop := e.Beat[*from%len(e.Beat)]
		*from++
		if tile.Chebyshev(here, stop) > BeatNear {
			return stop, true
		}
	}
	return tile.Tile{}, false
}

// Penned reports whether the rung's ground is a fenced pen.
func (r Rung) Penned() bool { return r.Pen != nil }

// CowPen is the Lumbridge cow pasture.
var CowPen = Enclosure{
	// Loc 1551 at 3253,3266 and loc 1553 at 3253,3267, "Gate", op Open, shape 0 angle 0.
	Gates: []tile.Tile{{X: 3253, Z: 3266}, {X: 3253, Z: 3267}},
	// Five tiles east of the gate's inner side and four from the pen's middle; within 3 of it every floor tile is inside.
	Enter:       tile.Tile{X: 3258, Z: 3266},
	EnterRadius: 3,
	// The south pen, the middle, the north-west corner and the north-east, the middle one twice.
	Beat: []tile.Tile{{X: 3258, Z: 3262}, {X: 3257, Z: 3274}, {X: 3249, Z: 3288}, {X: 3258, Z: 3291}, {X: 3257, Z: 3274}},
}

// cowPenSpawns are the ten cows (npc 81; m50_50 and m50_51 of the content maps) the pen holds: the rung's whole supply. The trainer's
// stand and leash cover all of them (a test holds that).
var cowPenSpawns = []tile.Tile{
	{X: 3243, Z: 3295}, {X: 3244, Z: 3289}, {X: 3247, Z: 3284}, {X: 3254, Z: 3258}, {X: 3255, Z: 3278},
	{X: 3256, Z: 3292}, {X: 3258, Z: 3260}, {X: 3261, Z: 3259}, {X: 3261, Z: 3270}, {X: 3261, Z: 3292},
}
