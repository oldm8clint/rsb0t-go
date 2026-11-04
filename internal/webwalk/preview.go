package webwalk

// Plan returns the route WalkTo(dest, o) would start out on from the player's current tile,
// without moving the player: the same danger zones (named, ad-hoc and automatic, resolved
// against the live combat level), the same hop handlers and CanHop filter, live walls and
// world state (coins for a fare, quests, skills). It reads no state of a walk in progress and
// changes none, so it is safe to call between walks to decide whether to walk at all.
//
// The first caller is MuleReceiver's route home (internal/bot/mulehome.go): with White Wolf
// Mountain's territory passed as AvoidRects, a route that is not OK means "no wolf-free way
// home with what this account holds", which is the signal to earn a boat fare rather than
// start a walk that could only fail or, worse, go over the mountain.
func (w *Walker) Plan(dest Point, o WalkOptions) Route {
	me, ok := w.Client.Tile()
	if !ok {
		return Route{Reason: "position unknown"}
	}
	return w.PlanFrom(me, dest, o)
}

// PlanFrom is Plan for a walk that starts at from instead of where the player stands: the route WalkTo(dest, o) would plan if
// the player were at from. The danger zones are resolved from from (a start inside a zone exempts the walk from it, so
// the exemption of the player's own tile must not leak to a landing across the map); the live walls and doors, which are
// keyed to the player's tile, are left out unless from is that tile. It reads no state of a walk in progress and changes
// none. The first caller is a teleport decision: what would the walk cost from the landing?
func (w *Walker) PlanFrom(from, dest Point, o WalkOptions) Route {
	return w.planFromTile(from, dest, o, nil, false)
}
