package freshstartrunner

import "rsb0t-go/internal/pathfind"

// LiveNavigationPack is the baked pack with the doors and walls the server has opened or shut since the bake
// (LocChanges) laid over it: what a reachability check on something nearby must read, since the bake has every
// door shut.
func LiveNavigationPack(d *Deps) *pathfind.Pack { return liveNavigationPack(d) }

// Closed map walls and the server's rotated/open stages must agree during routing.
func liveNavigationPack(d *Deps) *pathfind.Pack {
	if d.Pack == nil || d.Locs == nil || d.Config == nil {
		return d.Pack
	}
	var removed, added []pathfind.WallEdit
	for _, c := range d.Session.State().LocChanges() {
		if c.Shape != 0 && c.Shape != 2 {
			continue
		}
		for _, base := range d.Locs.At(c.X, c.Z, c.Level) {
			if base.Shape != 0 && base.Shape != 2 {
				continue
			}
			info, ok := d.Config.Loc(base.ID)
			if !ok || !info.BlockWalk {
				continue
			}
			removed = append(removed, pathfind.WallEdit{X: base.X, Z: base.Z, Level: base.Level, Shape: base.Shape, Angle: base.Angle})
		}
		if c.Deleted || (c.Shape != 0 && c.Shape != 2) {
			continue
		}
		info, ok := d.Config.Loc(c.Type)
		if !ok || !info.BlockWalk {
			continue
		}
		added = append(added, pathfind.WallEdit{X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Rotate, Add: true})
	}
	return d.Pack.WithWalls(append(removed, added...))
}
