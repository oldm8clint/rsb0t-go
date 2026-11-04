package locindex

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// WallEdits are the straight and corner wall changes the server has made since the maps were baked, as the
// edits pathfind.Pack.WithWalls lays over the shared, immutable collision pack: a door the map places shut that
// somebody has opened stops being a wall, and one the map places open that somebody has shut becomes one.
//
// The baked pack has every door shut, so a check on something nearby that reads it alone calls a room with an
// open door sealed (live, 12 October 2026: 1,333 "reachable: 0" waits in 90 minutes at the Heroes, which the
// thieving fix bd93b05e answered with the same overlay). Each loc change replaces whatever wall-layer loc the
// map places on its tile, which is how the client's own scene treats LOC_ADD_CHANGE and LOC_DEL, so the base
// walls on a changed tile are removed and the new leaf, when there is one and it blocks walking, is added.
// Diagonal doors (shape 9) and wall decoration are left to the pack: WithWalls models the two straight shapes.
//
// ix and types may not be nil.
func WallEdits(ix *Index, types Types, changes []gamestate.LocInstance) []pathfind.WallEdit {
	var removed, added []pathfind.WallEdit
	for _, c := range changes {
		if c.Shape != 0 && c.Shape != 2 {
			continue
		}
		for _, base := range ix.At(c.X, c.Z, c.Level) {
			if base.Shape != 0 && base.Shape != 2 {
				continue
			}
			info, ok := types.Loc(base.ID)
			if !ok || !info.BlockWalk {
				continue
			}
			removed = append(removed, pathfind.WallEdit{X: base.X, Z: base.Z, Level: base.Level, Shape: base.Shape, Angle: base.Angle})
		}
		if c.Deleted {
			continue
		}
		info, ok := types.Loc(c.Type)
		if !ok || !info.BlockWalk {
			continue
		}
		added = append(added, pathfind.WallEdit{X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Rotate, Add: true})
	}
	return append(removed, added...)
}
