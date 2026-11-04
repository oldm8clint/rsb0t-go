package webwalk

import "rsb0t-go/internal/randomevents"

// The graph's staircases and ladders, as the random-event Guardian's way out of a room.
//
// An event monster that cannot be left along the ground (a tower's top room is 33 tiles, the Swarm's leash 15) is left
// by changing level: the engine drops an NPC's target when the target is on another level. The Guardian cannot import
// this package (this one imports it, for the strand sites), so the walker's graph is registered with it here, and
// the Guardian takes the nearest hop that shakes the monster, wherever the walker has one: no room needs a table of its
// own. See randomevents/levelexit.go.
func init() { randomevents.SetLevelExitSource(LevelExits) }

// LevelExits are the Default graph's hops that carry an account off the ground it stands on with nothing to pay,
// carry or hold: a staircase or ladder between levels, and a ladder climbing OUT of a dungeon. Left out:
//
//   - a hop with a requirement beyond membership (a quest, a skill, an item, a fare), a special crossing (a dialogue, a
//     toll), a dice roll that can hurt, or a closed trapdoor that needs opening first: the Guardian clicks one loc once;
//   - a ladder DOWN into a dungeon, which is deeper into danger, not out of it;
//   - an edge with no loc to operate.
func LevelExits() []randomevents.LevelExit {
	g, err := Default()
	if err != nil {
		return nil
	}
	var out []randomevents.LevelExit
	for i := range g.Edges {
		if x, ok := levelExitOf(&g.Edges[i]); ok {
			out = append(out, x)
		}
	}
	return out
}

// dungeonRise is how many tiles north a dungeon's coordinates sit from the surface above them (z + 6400); a
// dungeon edge that lands more than this many minus slack tiles south of where it starts climbs out.
const dungeonRise = 3000

func levelExitOf(e *Edge) (randomevents.LevelExit, bool) {
	if !e.HasLoc() || e.OpenLocID >= 0 || e.Action == "" || e.Special != nil || e.Slip != nil || !onlyMembers(e.Requires) {
		return randomevents.LevelExit{}, false
	}
	switch e.Kind {
	case KindStair:
		if !e.ChangesLevel() {
			return randomevents.LevelExit{}, false
		}
	case KindDungeon:
		if e.From.Z-e.To.Z < dungeonRise {
			return randomevents.LevelExit{}, false
		}
	default:
		return randomevents.LevelExit{}, false
	}
	return randomevents.LevelExit{
		Stand: randomevents.StrandTile{X: e.From.X, Z: e.From.Z, Level: e.From.Level},
		Land:  randomevents.StrandTile{X: e.To.X, Z: e.To.Z, Level: e.To.Level},
		LocID: e.LocID, LocX: e.LocX, LocZ: e.LocZ, Label: e.Action,
	}, true
}

// onlyMembers reports whether r asks for nothing but a members world.
func onlyMembers(r *Requires) bool {
	if r == nil {
		return true
	}
	return len(r.Skills) == 0 && len(r.Items) == 0 && len(r.Worn) == 0 && len(r.Quests) == 0 && r.FreeSlots == nil &&
		r.Currency == nil && !r.ForbidEntranaRestricted && !r.SlashTool && r.EssenceExitReturn == "" && r.EssenceEntrySetsReturn == ""
}
