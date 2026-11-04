package webwalk

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
)

// Walker.BeforeFollow: the seam a teleport decision uses (teleport-walks, a user request: "teleport runes should be
// considered every navigation trip and if it may be closer to teleport rather than run").
//
// walkOnce plans a route and follows it. The first plan is the walk's own price (cost, hops, ships), and it is exactly what a
// decision about teleporting needs to compare with: so the hook is asked right after that plan, before the route is
// followed, and with the plan in hand. A failed plan (no path, or the search ran out of budget) is shown too: a cast is the
// best way out of a walk the planner cannot find. If the hook moves the player, walkOnce reads the tile and plans again from
// the landing without spending a repath; at most one move a WalkTo (walkState.teleMoved), and at most maxTeleDecisions
// decisions that planned something.
//
// Why here, and not in Prepare or at the top of WalkTo: the plan is already computed (a hook there would plan the walk from
// here a second time), the hook sees the walk's real danger zones and hop handlers, and a caller that brought an Aside of its
// own (which Prepare must keep out of) is still covered.
//
// Why not teleport edges in the planner: a teleport lands on a fixed square from ANYWHERE, so the only useful place for
// one is the start of a walk, and edges would put a per-account "can this account cast" and a law-rune budget into the
// planner every other script shares. PlanFrom prices a landing exactly as the walk would plan from there, one search a
// candidate, which a decision spends only after its cheap gates pass.

// maxTeleDecisions bounds the BeforeFollow decisions that planned something in one WalkTo. The ladder runs a failed plan up to
// three times; a negative memo in the hook makes the repeats free, and this makes them bounded.
const maxTeleDecisions = 3

// maxLandingExpansions caps one landing's price: a landing the search cannot price in this many node expansions is not worth
// a cast (the whole-map walks average 94,000, the longest 463,000).
const maxLandingExpansions = 200000

// FollowQuery is what BeforeFollow is shown.
type FollowQuery struct {
	// Here is where the player stands, Dest where the walk is bound.
	Here, Dest Point
	// Route is the walk's first plan from Here: OK is false when the plan failed (Route.Reason says why), and a plan
	// whose route ends where the player stands (the nearest reachable tile) is the hook's to treat as no route.
	Route Route
	// Opts are the walk's options as the caller (and Prepare) gave them.
	Opts WalkOptions
	// PlanFrom prices the walk from another tile exactly as this walk would plan from there: the same options, the walk's own
	// avoids, the danger zones resolved from that tile, the live walls left out, and the search capped at
	// maxLandingExpansions nodes. One call is one planner run.
	PlanFrom func(from Point) Route
	// PlanKey is a hash of everything about the walk, apart from the landing, that decides what a landing's price is: the
	// destination, the walk's avoided rectangles and named zones, whether the automatic zones apply, the combat level they were
	// resolved with, the barriers it avoids and the squares it fled. Two queries with the same key and landing price the same,
	// so a caller may remember a price (Travel.TeleportHook does, for ten minutes).
	PlanKey func() uint64
}

// BeforeFollowResult is what a BeforeFollow hook reports back.
type BeforeFollowResult struct {
	// Moved says the hook moved the player (the walk plans again from the new tile).
	Moved bool
	// Planned says the hook priced something (a landing): only such a decision counts toward the walk's allowance, so the
	// cheap declines of a short walk never use it up.
	Planned bool
}

// PreviewFollow is the query a walk to dest with o would show BeforeFollow, planned now from the player's tile, for a caller
// that decides before it walks and does not go through WalkTo (a quest leg's BeforeTravel). The walk's flee squares and
// avoided barriers are the walk's own and do not exist yet.
func (w *Walker) PreviewFollow(dest Point, o WalkOptions) (FollowQuery, bool) {
	me, ok := w.Client.Tile()
	if !ok {
		return FollowQuery{}, false
	}
	o.Dest = dest
	return FollowQuery{
		Here: me, Dest: dest, Route: w.PlanFrom(me, dest, o), Opts: o,
		PlanFrom: func(from Point) Route { return w.PlanFrom(from, dest, o) },
		PlanKey:  func() uint64 { return w.planKey(dest, o, nil) },
	}, true
}

// beforeFollow asks the hook, if the walk may use one, and reports whether it moved the player.
func (w *Walker) beforeFollow(me, dest Point, o WalkOptions, avoid map[Point]bool, route Route, pocket bool) bool {
	if w.BeforeFollow == nil || o.NoTeleport || pocket || w.walk.teleMoved || w.walk.teleDecisions >= maxTeleDecisions {
		return false
	}
	res := w.BeforeFollow(FollowQuery{
		Here: me, Dest: dest, Route: route, Opts: o,
		PlanFrom: func(from Point) Route {
			return w.planFromTile(from, dest, o, avoid, true)
		},
		PlanKey: func() uint64 { return w.planKey(dest, o, avoid) },
	})
	if res.Planned {
		w.walk.teleDecisions++
	}
	if res.Moved {
		w.walk.teleMoved = true
		if route.OK {
			// The route line walkOnce would have written for the walk from here: the planning cost of every walk is counted
			// from these lines, and the cast hides this one behind the landing's.
			w.logf("route %s -> %s: %s", me, dest, route.String())
		}
		w.logf("moved to %s before following the route; planning again from there", w.nowTile())
	}
	return res.Moved
}

// planKey hashes what a landing's price depends on besides the landing (FollowQuery.PlanKey).
func (w *Walker) planKey(dest Point, o WalkOptions, avoid map[Point]bool) uint64 {
	h := fnv.New64a()
	put := func(vals ...int) {
		var b [8]byte
		for _, v := range vals {
			binary.LittleEndian.PutUint64(b[:], uint64(int64(v)))
			h.Write(b[:])
		}
	}
	put(dest.X, dest.Z, dest.Level)
	for _, r := range o.AvoidRects {
		put(r.MinX, r.MaxX, r.MinZ, r.MaxZ, r.Level)
	}
	put(-1)
	for _, id := range o.AvoidZones {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	put(len(o.Search.AvoidZones), o.Search.MaxExpansions)
	if o.NoAutomaticZones {
		put(1)
	} else {
		put(0)
	}
	if cc := w.combatClient(); cc != nil {
		lvl, known := cc.CombatLevel()
		if known {
			put(1, lvl)
		} else {
			put(0)
		}
	}
	if o.Hurt {
		put(-4) // a hurt walk resolves the level-based zones at every level (ZoneContext.Hurt); a healthy walk's key is what it was
	}
	put(-2)
	// The barriers this walk avoids, in a stable order (the map's own iteration is not).
	keys := make([]Point, 0, len(avoid))
	for p, on := range avoid {
		if on {
			keys = append(keys, p)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Z < b.Z
	})
	for _, p := range keys {
		put(p.X, p.Z, p.Level)
	}
	put(-3)
	for _, r := range w.walk.flee {
		put(r.MinX, r.MaxX, r.MinZ, r.MaxZ, r.Level)
	}
	return h.Sum64()
}

// nowTile is the player's tile for a log line.
func (w *Walker) nowTile() string {
	if p, ok := w.Client.Tile(); ok {
		return p.String()
	}
	return "an unknown tile"
}

// planFromTile plans dest from from the way a walk would. inWalk adds the walk's flee squares (the attackers it fled from)
// to the avoided zones; a preview has none.
func (w *Walker) planFromTile(from, dest Point, o WalkOptions, avoid map[Point]bool, inWalk bool) Route {
	zones, _ := w.resolveZonesAt(dest, o, from, true)
	opt := w.searchOptions(o.Search, avoid)
	// searchOptions layers in the zones of whichever walk ran last; a plan from another tile must use only its own.
	opt.AvoidZones = append(append([]Rect(nil), o.Search.AvoidZones...), zones...)
	if inWalk {
		opt.AvoidZones = append(opt.AvoidZones, w.walk.flee...)
	}
	if me, ok := w.Client.Tile(); !ok || me != from {
		// Walls and doors the player's live scene shows are keyed to the player's tile; they say nothing of a landing.
		if o.Search.Walls == nil {
			opt.Walls = nil
		}
		if o.Search.LiveEdges == nil {
			opt.LiveEdges = nil
		}
		if opt.MaxExpansions <= 0 || opt.MaxExpansions > maxLandingExpansions {
			opt.MaxExpansions = maxLandingExpansions
		}
	}
	return w.Planner.Plan(from, dest, opt)
}
