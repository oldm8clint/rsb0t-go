package fleetrunner

import (
	"rsb0t-go/internal/scripts/fleetplan"
)

// PackSight reads a tile's line-of-sight blockers from the collision pack: its wall faces, which
// the pack keeps (pathfind.Pack.WallMask: bit 0 north, 1 east, 2 south, 3 west, the order
// scarabplan.Sight uses). The pack cannot tell a projectile-blocking loc from water -- both are
// simply not walkable -- so a tree on the shore reads as clear; the engine then walks the account
// on towards the target until its own line of sight holds (castreach.go in scarabplan), which is
// the worst that approximation costs. nil without a pack.
func PackSight(d *Deps) func(x, z int) fleetplan.Sight {
	if d == nil || d.Pack == nil {
		return nil
	}
	pk := d.Pack
	return func(x, z int) fleetplan.Sight { return fleetplan.Sight(pk.WallMask(x, z, 0) & 0xf) }
}
