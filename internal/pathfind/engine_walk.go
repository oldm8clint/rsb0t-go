package pathfind

// What the SERVER does with the waypoints a client queued, and with an op on another player, as a function of the collision pack.
//
// A MOVE_GAMECLICK carries waypoints and the engine (Engine-TS 289, MoveClickHandler with clientRoutefinder on, PathingEntity.takeStep)
// walks them as a GREEDY chain, one tile a tick: toward the waypoint diagonally, else along x, else along z, and when all three are
// refused the entity stays where it is and the waypoint stays queued, for good. It does not route. So a click on a far tile is only as
// good as the straight-ish line to it: one lone waypoint behind a pillar parks the player against the pillar, and every further click
// of the same lone waypoint does nothing (live 18 Oct 2026: the Nature gate's crafter, 56 minutes, 3 tiles from his stand).
//
// An op on a player (OPPLAYER1..5, trade included) runs only from a tile that is ORTHOGONALLY beside the target with no wall between
// (reachedEntity, exclusive: not on the same tile). From anywhere else the engine closes the distance with the NAIVE approach
// (Player.pathToPathingTarget -> NaivePathFinder.findNaivePath): ONE waypoint on the side of the target the asker is on, walked
// greedily as above, and recomputed only once reached. When that waypoint is a solid tile (a pillar), the asker stops against it,
// nothing is said and the op never runs.
//
// EngineStep, EngineWalk and NaiveApproach replay those rules on a pack. They are test oracles and cheap preflight checks ("would the
// engine get me there from here?"); they know the pack's tiles and walls, not NPCs and other players. Engine-TS's canTravel judges a
// step by the DESTINATION tile and the origin's walls, the pack's exit mask by the origin: the two agree for every step off a walkable
// tile, which is every step the replays take (a player stands on walkable ground).

// directionIndex is the index of a (sx, sz) step in the table the exit masks were baked with (dx/dz in astar.go).
func directionIndex(sx, sz int) int {
	switch {
	case sx == 0 && sz == 1:
		return 0
	case sx == 1 && sz == 0:
		return 1
	case sx == 0 && sz == -1:
		return 2
	case sx == -1 && sz == 0:
		return 3
	case sx == 1 && sz == 1:
		return 4
	case sx == 1 && sz == -1:
		return 5
	case sx == -1 && sz == -1:
		return 6
	case sx == -1 && sz == 1:
		return 7
	}
	return -1
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// canStep is Engine-TS canTravel for a width-1 entity: the pack's exit mask has the direction.
func (p *Pack) canStep(at NavPoint, sx, sz int) bool {
	dir := directionIndex(sx, sz)
	return dir >= 0 && p.ExitMask(at.X, at.Z, at.Level)&(1<<uint(dir)) != 0
}

// CanStep reports that the exit from (x, z, level) by (sx, sz), each -1, 0 or 1, is open in the pack's collision (with its live
// overrides): the test the engine's canTravel makes for a width-1 entity. The Dwarf cannon's spot check reads it.
func (p *Pack) CanStep(x, z, level, sx, sz int) bool {
	return p.canStep(NavPoint{X: x, Z: z, Level: level}, sx, sz)
}

// EngineStep is one step of PathingEntity.takeStep for a width-1 entity at from whose current waypoint is waypoint: the diagonal
// toward it, else along x, else along z. moved is false when the entity is on the waypoint or all three are refused (it stays, the
// waypoint stays queued).
func (p *Pack) EngineStep(from, waypoint NavPoint) (to NavPoint, moved bool) {
	sx, sz := sign(waypoint.X-from.X), sign(waypoint.Z-from.Z)
	if sx == 0 && sz == 0 {
		return from, false
	}
	if p.canStep(from, sx, sz) {
		return NavPoint{X: from.X + sx, Z: from.Z + sz, Level: from.Level}, true
	}
	if sx != 0 && p.canStep(from, sx, 0) {
		return NavPoint{X: from.X + sx, Z: from.Z, Level: from.Level}, true
	}
	if sz != 0 && p.canStep(from, 0, sz) {
		return NavPoint{X: from.X, Z: from.Z + sz, Level: from.Level}, true
	}
	return from, false
}

// EngineWalk follows the waypoints in order as the server does and reports where the entity ends up and whether it got to the last
// one. A waypoint that cannot be stepped toward ends the walk there for good; maxSteps bounds the walk (one tile a tick).
func (p *Pack) EngineWalk(from NavPoint, waypoints []NavPoint, maxSteps int) (end NavPoint, arrived bool) {
	cur := from
	steps := 0
	for _, wp := range waypoints {
		if wp.Level != from.Level {
			return cur, false
		}
		for cur != wp {
			if steps >= maxSteps {
				return cur, false
			}
			next, moved := p.EngineStep(cur, wp)
			if !moved {
				return cur, false
			}
			cur = next
			steps++
		}
	}
	return cur, true
}

// operableFrom is reachedEntity for two single-tile players: from is orthogonally beside target with no wall between, and not on it.
func (p *Pack) operableFrom(from, target NavPoint) bool {
	return from != target && p.CanInteractFrom(from, target)
}

// OperableFrom says the engine would run an op on a player standing on target from the tile from, with no walk at all: the tile is
// orthogonally beside the target with no wall between (a diagonal neighbour is not, and a tile on the other side of a wall is not).
func (p *Pack) OperableFrom(from, target NavPoint) bool { return p.operableFrom(from, target) }

// naiveWaypoint is NaivePathFinder.findNaivePath for two single tiles: the one waypoint the engine queues for an asker at src walking
// toward a player at dest. For two single tiles naiveDestination is always one of the target's four orthogonal neighbours, the one on
// the asker's side of him (a north-east asker gets the east neighbour, and so on), so the closing loop of findNaivePath never runs; it
// is ported anyway so the function reads line for line like the original.
func (p *Pack) naiveWaypoint(src, dest NavPoint) NavPoint {
	level := src.Level
	diagonal := src.X - dest.X + (src.Z - dest.Z)
	anti := src.X - dest.X - (src.Z - dest.Z)
	// naiveDestination with srcWidth = srcHeight = destWidth = destHeight = 1.
	const w = 1
	southWest := anti < 0
	northWest := diagonal >= w-1-(w-1)
	northEast := anti > w-w
	southEast := diagonal <= w-1-(w-1)
	var dx, dz int
	switch {
	case southWest && !northWest:
		offZ := 0
		if diagonal >= -w {
			offZ = min(diagonal+w, w-1)
		} else if anti > -w {
			offZ = -(w + anti)
		}
		dx, dz = -w+dest.X, offZ+dest.Z
	case northWest && !northEast:
		offX := 0
		if anti >= -w {
			offX = min(anti+w, w-1)
		} else if diagonal < w {
			offX = max(diagonal-w, -(w - 1))
		}
		dx, dz = offX+dest.X, w+dest.Z
	case northEast && !southEast:
		offZ := 0
		if anti <= w {
			offZ = w - anti
		} else if diagonal < w {
			offZ = max(diagonal-w, -(w - 1))
		}
		dx, dz = w+dest.X, offZ+dest.Z
	case southEast && !southWest:
		offX := 0
		if diagonal > -w {
			offX = min(diagonal+w, w-1)
		} else if anti < w {
			offX = max(anti-w, -(w - 1))
		}
		dx, dz = offX+dest.X, -w+dest.Z
	default:
		return src
	}
	d := NavPoint{X: dx, Z: dz, Level: level}
	// isDiagonal(dest tile vs target) and intersects: the destination is the waypoint as it stands.
	if abs(dx-dest.X) == 1 && abs(dz-dest.Z) == 1 || dx == dest.X && dz == dest.Z {
		return d
	}
	cur := d
	for cur.X != dest.X && cur.Z != dest.Z {
		sx, sz := sign(dest.X-cur.X), sign(dest.Z-cur.Z)
		switch {
		case p.canStep(cur, sx, sz):
			cur = NavPoint{X: cur.X + sx, Z: cur.Z + sz, Level: level}
		case sx != 0 && p.canStep(cur, sx, 0):
			cur = NavPoint{X: cur.X + sx, Z: cur.Z, Level: level}
		case sz != 0 && p.canStep(cur, 0, sz):
			cur = NavPoint{X: cur.X, Z: cur.Z + sz, Level: level}
		default:
			return cur
		}
	}
	return cur
}

// NaiveApproach replays what the engine does when a player at from runs an op on the player at target: it runs at once when from is
// operable, otherwise the asker walks the naive waypoint greedily, asks again from where it ends up, and so on. reached says the op
// would run (the asker ended on an operable tile within maxSteps); end is where the asker stands when it runs or when it is stuck.
// A walk that goes nowhere is a dead end for good (the waypoint stays queued and the engine says nothing: no "I can't reach that!"
// while a waypoint is still queued, so the asker hears no reply at all).
func (p *Pack) NaiveApproach(from, target NavPoint, maxSteps int) (end NavPoint, reached bool) {
	if from.Level != target.Level {
		return from, false
	}
	cur := from
	var wp NavPoint
	have := false
	for steps := 0; steps <= maxSteps; steps++ {
		if p.operableFrom(cur, target) {
			return cur, true
		}
		if cur == target {
			return cur, false // under the target: the engine walks it one random tile; no telling where
		}
		// Player.naivePathToTarget: a new destination only when none is queued or the queued one is not beside the target.
		if !have || abs(target.X-wp.X) > 1 || abs(target.Z-wp.Z) > 1 {
			wp, have = p.naiveWaypoint(cur, target), true
		}
		next, moved := p.EngineStep(cur, wp)
		if !moved {
			return cur, false
		}
		cur = next
		if cur == wp {
			have = false
		}
	}
	return cur, false
}
