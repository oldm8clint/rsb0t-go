package pathfind

import "sort"

// "Can I interact with that from here?", asked of the pack instead of of a distance.
//
// FindPath already knows the engine's rule — cardinalGoals/besideAll resolve a blocked
// destination to the walkable tiles that ORTHOGONALLY touch its footprint with no wall on the
// facing side, which is exactly what inOperableDistance/reachRectangle1 check server-side. What
// no caller could ask until now is the same question about a tile it is already standing on,
// and every caller that wanted to has written `Chebyshev(here, target) <= 1` instead. Chebyshev
// 1 includes the four diagonals, and a diagonal neither touches orthogonally nor clears a wall,
// so the click is accepted by the client, ignored by the server, and nothing downstream reports
// a failure. This repo has shipped that bug three times.
//
// InteractTiles and CanInteractFrom expose the existing rule so a caller can stop guessing.

// interactGoals is the set of tiles an interaction with whatever stands on target is accepted
// from, keyed by nodeID.
//
// A blocked target (the ordinary case: a tree, a bank booth, a door) is cardinalGoals' own
// answer, the same set FindPath steers a walk toward. A walkable target — a ground item, an NPC,
// a loc that does not block — is reachable from the tile itself and from its four orthogonal
// neighbours whose facing side is not walled off; the diagonals are excluded there too, because
// the server's check does not care whether the target blocks.
func (p *Pack) interactGoals(target NavPoint) map[int64]bool {
	if !p.Walkable(target.X, target.Z, target.Level) {
		return p.cardinalGoals(target)
	}
	goals := make(map[int64]bool, 5)
	goals[nodeID(target.X, target.Z, target.Level)] = true
	for _, side := range cardinalSides {
		cx, cz := target.X+side.dx, target.Z+side.dz
		if p.Walkable(cx, cz, target.Level) && p.WallMask(cx, cz, target.Level)&side.bit == 0 {
			goals[nodeID(cx, cz, target.Level)] = true
		}
	}
	return goals
}

// CanInteractFrom reports whether the engine would accept an interaction with whatever stands
// on target from the tile from. It answers false for a diagonal approach and for a tile whose
// facing side is walled off, which is the whole point of asking.
func (p *Pack) CanInteractFrom(from, target NavPoint) bool {
	if from.Level != target.Level {
		return false
	}
	return p.interactGoals(target)[nodeID(from.X, from.Z, from.Level)]
}

// InteractTiles lists every tile CanInteractFrom would answer true for, nearest-corner first so
// the order is stable for tests and logs. An empty result means nothing can reach the target at
// all — it is sealed behind walls, or it is not in the pack.
func (p *Pack) InteractTiles(target NavPoint) []NavPoint {
	goals := p.interactGoals(target)
	out := make([]NavPoint, 0, len(goals))
	for id := range goals {
		out = append(out, NavPoint{X: nodeX(id), Z: nodeZ(id), Level: nodeLevel(id)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}
