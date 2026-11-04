package fleetrunner

import (
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// exitDirs is the pack's exit-mask direction table: N, E, S, W, NE, SE, SW, NW.
var exitDirs = [8][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}, {1, 1}, {1, -1}, {-1, -1}, {-1, 1}}

// PackCanStep reports whether the pack allows the one-tile step from a to b (a legal exit of a).
func PackCanStep(pk *pathfind.Pack) func(a, b tile.Tile) bool {
	return func(a, b tile.Tile) bool {
		if pk == nil {
			return false
		}
		mask := pk.ExitMask(a.X, a.Z, a.Level)
		for i, dir := range exitDirs {
			if a.X+dir[0] == b.X && a.Z+dir[1] == b.Z {
				return mask&(1<<uint(i)) != 0
			}
		}
		return false
	}
}
