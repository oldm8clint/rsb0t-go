package combatplan

import "time"

// Targets behind doors.
//
// Three of the ladder's rungs have most of their spawns inside buildings: the Al Kharid warriors in the palace's
// rooms, the White Knights in Falador castle's courtyard and halls, the Barbarians in the village's long hall. The
// collision pack the trainer judges reach on has every door shut, and the stand each rung walks to is within
// three tiles of a wall, so an account arrived on one side of a door and read every monster on the other as
// "no route": live, 14 October 2026, 04:05-04:45, 417 "no route from here to the Barbarian", 345 to the White
// Knight and 287 to the Al-Kharid warrior, and 177 + 166 + 56 "no route from where we are standing - waiting"
// lines, each a trainer standing still while the monsters were a door away.
//
// The rules here are the ones that keep walking through a door from becoming a loop, and none of them touch the
// world, so they are tested without one:
//
//   - a target on this side of the doors always beats one behind a door (the caller ranks plain targets first),
//     so a crossing is made only when nothing is left to fight where the account stands;
//   - of the targets behind doors the cheapest route wins, then the nearer by the caller's own ranking;
//   - a route is worth walking only when it crosses doors and nothing else, and is short enough that the monster
//     does not lose the fight by chasing it back out (DoorRouteMaxCost);
//   - the crossings between two kills are counted, and the doors are held shut for a while once they pass
//     DoorStrikeLimit, so a door that never leads to a kill costs a bounded number of tries, not a shuttle.

// DoorRouteMaxCost is the dearest route, in tiles with each door charged four, the trainer walks to reach a
// monster behind a door. The probe over the real pack found every spawn of the three rungs with a door route
// between 6 and 26 from the stand; 40 leaves room for the other side of a room and refuses a trip round a
// building: a monster only chases as far as its own range from its spawn, so a long way round loses the fight
// it was walked to (combatrunner.ApproachRadius).
const DoorRouteMaxCost = 40

// DoorRoute is a planned route to a target as the door judgement reads it.
type DoorRoute struct {
	// OK is whether the planner found a route at all.
	OK bool
	// Cost is the route's cost in tiles; Doors the door and gate hops on it, Other every hop of any other kind
	// (a stair, a ladder, a ship, a hop that needs an NPC or a toll).
	Cost, Doors, Other int
}

// DoorRouteOK is whether a route is one the trainer crosses doors for, and if not why not.
func DoorRouteOK(r DoorRoute) (bool, string) {
	switch {
	case !r.OK:
		return false, "no route even through the doors"
	case r.Other > 0:
		return false, "the way there is not only doors"
	case r.Doors == 0:
		return false, "the route has no door, so it is the plain walk the box search already refused"
	case r.Cost > DoorRouteMaxCost:
		return false, "the way through the doors is too long"
	}
	return true, ""
}

// DoorCand is one target the trainer could reach by crossing doors: Rank is its place in the caller's own
// ordering (rankTargets), Cost the route's.
type DoorCand struct {
	Rank, Cost int
}

// PickDoorTarget is the index in cands of the target to cross doors for: the cheapest route, ties to the better
// rank. False for none.
func PickDoorTarget(cands []DoorCand) (int, bool) {
	best := -1
	for i, c := range cands {
		if best < 0 || c.Cost < cands[best].Cost || (c.Cost == cands[best].Cost && c.Rank < cands[best].Rank) {
			best = i
		}
	}
	return best, best >= 0
}

// DoorStrikeLimit is how many door targets are taken between two kills before the doors are held shut, and
// DoorHold how long.
const (
	DoorStrikeLimit = 4
	DoorHold        = 5 * time.Minute
)

// DoorBudget counts the door targets taken since the last kill. The zero value allows them.
type DoorBudget struct {
	strikes   int
	heldUntil time.Time
}

// Allowed is whether the trainer may take a target behind a door now.
func (b *DoorBudget) Allowed(now time.Time) bool { return !now.Before(b.heldUntil) }

// Strike records a door target taken (the crossing, the fight after it, whether or not either worked). It
// reports true when this one used the budget up and the doors are now held shut for DoorHold.
func (b *DoorBudget) Strike(now time.Time) bool {
	b.strikes++
	if b.strikes < DoorStrikeLimit {
		return false
	}
	b.strikes = 0
	b.heldUntil = now.Add(DoorHold)
	return true
}

// Kill clears the count: a door that leads to a kill is a door worth crossing again.
func (b *DoorBudget) Kill() { b.strikes = 0 }

// Strikes is the door targets taken since the last kill, for the log.
func (b *DoorBudget) Strikes() int { return b.strikes }
