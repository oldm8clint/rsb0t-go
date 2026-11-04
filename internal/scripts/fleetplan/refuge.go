package fleetplan

import "rsb0t-go/internal/scripts/tile"

// THE NEAREST GROUND NO HUNTER HOLDS (ogresafe-1017, 17 Oct 2026).
//
// A safespot is a tile proven out of every NPC's melee, and what makes that safe is not that nothing has noticed the account
// there but that an NPC keeps a melee target only within its maxrange of its spawn (PenThreat.Holds): one that is already
// fighting the account gives it up the moment the account is on a tile it does not hold. So an account that finds itself off
// its safespot with the herd on it (a random event moved it, a lagging view walked it) does not need the safespot, or the
// pen's bank, or a fight: it needs the nearest tile no threat holds, by the shortest walk. This finds it.

// refugeDirs is the eight steps a BFS takes, in the order of the pack's exit bits (N, E, S, W, NE, SE, SW, NW).
var refugeDirs = [8][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}, {1, 1}, {1, -1}, {-1, -1}, {-1, 1}}

// NearestRefuge is the shortest walk from `from` to a tile held(x, z) is false of, over steps canStep allows, at most maxSteps
// long: every tile walked, the start excluded, the refuge last. prefer, when set, breaks a tie between refuges at the same
// distance (a safespot over plain ground). ok is false when `from` is not held (nothing to flee: path is nil) or when no refuge
// is within maxSteps.
func NearestRefuge(from tile.Tile, held func(x, z int) bool, canStep func(a, b tile.Tile) bool, prefer func(t tile.Tile) bool, maxSteps int) (path []tile.Tile, ok bool) {
	if !held(from.X, from.Z) {
		return nil, false
	}
	type node struct {
		at     tile.Tile
		parent int
	}
	nodes := []node{{at: from, parent: -1}}
	seen := map[[2]int]bool{{from.X, from.Z}: true}
	layer := []int{0}
	for step := 1; step <= maxSteps && len(layer) > 0; step++ {
		var next []int
		best := -1
		for _, ni := range layer {
			cur := nodes[ni].at
			for _, dir := range refugeDirs {
				to := tile.Tile{X: cur.X + dir[0], Z: cur.Z + dir[1], Level: cur.Level}
				if seen[[2]int{to.X, to.Z}] || !canStep(cur, to) {
					continue
				}
				seen[[2]int{to.X, to.Z}] = true
				nodes = append(nodes, node{at: to, parent: ni})
				idx := len(nodes) - 1
				if !held(to.X, to.Z) {
					if best < 0 || (prefer != nil && prefer(to) && !prefer(nodes[best].at)) {
						best = idx
					}
					continue
				}
				next = append(next, idx)
			}
		}
		if best >= 0 {
			for i := best; nodes[i].parent >= 0; i = nodes[i].parent {
				path = append(path, nodes[i].at)
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path, true
		}
		layer = next
	}
	return nil, false
}
