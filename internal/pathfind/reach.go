package pathfind

// "Tiles must touch", asked as a question instead of assumed.
//
// Four times now a layer above this one has taken a short straight-line distance as proof that
// something could be walked to, and every time the world has answered with a wall. The most
// expensive version, 2026-09-19: CombatTrainer's fight loop skipped its approach walk whenever
// the target was already Chebyshev 1 away, so it never consulted this package at all — and
// Chebyshev 1 includes the tile on the far side of a palace wall. The click is accepted by the
// server, nothing downstream reports a failure, and the run picked the same unreachable warrior
// every pass for the rest of its window: 44 kills and then nothing, with not one line written.
//
// FindPath already answers the question correctly. What it does not do is answer it cheaply
// enough to ask about a dozen candidates several times a minute: a target sealed inside a
// building makes the search expand every tile of the region the account is standing in before it
// can say "unreachable". ReachableWithin bounds that to a box around the account, which is also
// the honest question for anything that picks a target by proximity — not "could I get there by
// any route at all", but "could I get there from here without walking half the map".

// ReachableWithin reports whether `to` can be reached from `from` by a route that never strays
// more than radius tiles (Chebyshev) from `from`.
//
// The goal semantics are FindPath's own, deliberately, so that a "yes" here means the very same
// search a caller's walk will run can also find its way: a walkable destination has to be reached
// exactly, and a destination standing on blocked collision is satisfied by the walkable tiles
// beside it whose facing side is not walled off. That is what "the tiles touch" means to the
// engine, and it is why this is the check rather than a distance.
//
// A route longer than the radius answers false even though a way around exists. That is the
// intended reading: the caller asked about reaching something nearby.
func (p *Pack) ReachableWithin(from, to NavPoint, radius int) PathResult {
	if radius < 0 {
		return PathResult{Reason: "negative radius"}
	}
	if from.Level != to.Level {
		return PathResult{Reason: "a different level"}
	}
	if chebyshev(from.X, from.Z, to.X, to.Z) > radius {
		return PathResult{Reason: "further away than the radius allows"}
	}
	return p.FindPathAvoiding(from, to, func(x, z, level int) bool {
		return level != from.Level || chebyshev(x, z, from.X, from.Z) > radius
	})
}
