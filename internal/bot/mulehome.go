package bot

import (
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The mule's route home, kept out of White Wolf Mountain.
//
// Live 2026-09-19: after fail-teleports and a death, `mule` (combat 3, 10 hitpoints, no
// food) spent hours trying to walk back to its post from the east. Every land route from
// Taverley, Falador or Lumbridge to Catherby crosses the pass, and neither of the walks the
// mule used knew that: fisherrunner.ReturnToCatherby's four webwalk attempts avoid only the
// automatic danger zones (the named "white-wolf-mountain" rectangle is not one, and it stops
// short of the eastern foothills anyway), its boat fallback and freshstartrunner.SailLeg
// both short-circuit to a plain on-foot walk whenever plain pathfinding can reach Catherby at
// all, and ensureHome skipped the whole thing for anything within fisherplan.InCatherby's 60
// tiles, which includes the foothills east of the pass. The mule was attacked there by the
// whitewolf_sentry spawns at 2870,3438 and 2872,3445 again and again; the combat flee
// correctly ran away from the attacker, which sat between it and home, so every flee pushed
// it further east; it died twice and no donor could hand off for about 2.5 hours.
//
// The fix is scoped to the mule: returnHome plans with the wolf dens as no-go zones
// (randomevents.WolfTerritorySquares, from the content spawn files, not a hand-drawn box) and
// only walks a route that avoids them. From the east that route is the boat chain, which the
// mule's own webwalk.Walker runs through internal/webwalk/hops (Port Sarim ship, Karamja,
// Brimhaven ship, Ardougne, the coast road); short of the fare it pickpockets at Lumbridge,
// or on Karamja takes Luthas's banana job, both freshstartrunner steps the fishers already
// use. ProgressiveFisher's routing is unchanged.

const (
	// muleWolfMargin widens each den's wander-plus-hunt reach so a planned route passes with
	// room to spare rather than on the exact tile a sentry can first notice the mule from.
	muleWolfMargin = 2
	// muleHomeBudget bounds one ensureHome call; the Loop simply calls again.
	muleHomeBudget = 30 * time.Minute
	// muleHomeWalkTimeout bounds one planned walk. The whole boat chain from Lumbridge is
	// about 780 cost units; the webwalk boats proof took ~3 minutes for the ships and the
	// Karamja crossing, and the Ardougne road took ~4 minutes, so 15 minutes has room.
	muleHomeWalkTimeout = 15 * time.Minute
	// muleLocalWalkTimeout and muleLocalRadius are the short, door-aware walk ensureHome has
	// always used on Catherby's own side (the range room and back, the beach).
	muleLocalWalkTimeout = 60 * time.Second
	muleLocalRadius      = 12
	// muleFundSpotRadius is how close to the Lumbridge pickpocket spot the walk there counts.
	muleFundSpotRadius = 5
	// mulePlanReuse bounds how long a "no wolf-free route" verdict is reused while the mule
	// earns its fare in one place; a new tile or a coin count change replans at once.
	mulePlanReuse = 30 * time.Second
)

// muleWolfRects is wolf territory as webwalk no-go rectangles on the surface.
func muleWolfRects() []webwalk.Rect {
	squares := randomevents.WolfTerritorySquares(muleWolfMargin)
	out := make([]webwalk.Rect, 0, len(squares))
	for _, s := range squares {
		out = append(out, webwalk.Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	return out
}

func wwPoint(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z, Level: t.Level} }

// muleLocalWalkSafe reports whether the mule is on Catherby's side of the pass, where the
// plain local walk home cannot go near a wolf: within a few tiles of the post, or in the
// Catherby area with a plain route home that never enters wolf territory. The eastern
// foothills are inside fisherplan.InCatherby's radius, and their plain route home is over
// the mountain, so they fail the second test.
func muleLocalWalkSafe(pk *pathfind.Pack, here, home tile.Tile, rects []webwalk.Rect) bool {
	if pk == nil {
		return false
	}
	// A hop of muleLocalRadius tiles cannot cross the pass, whatever town the post is in, so
	// the short walk home never needs the wolf check. This sits ABOVE the Catherby test so a
	// mule posted elsewhere (postTile) still gets its own short walk home instead of
	// replanning the whole map for a few tiles. Nothing changes for a Catherby post:
	// anything within muleLocalRadius (12) of that bank is inside InCatherby's own 60.
	if here.Level == home.Level && tile.Chebyshev(here, home) <= muleLocalRadius {
		return true
	}
	// Past the short hop this is a question about White Wolf Mountain specifically, and the
	// answer is written against Catherby's side of it: a tile that is not in Catherby has no
	// plain route home that this function is willing to vouch for.
	if !fisherplan.InCatherby(here) {
		return false
	}
	res := pk.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: home.X, Z: home.Z, Level: home.Level})
	corners := make([]tile.Tile, 0, len(res.Waypoints))
	for _, w := range res.Waypoints {
		corners = append(corners, tile.Tile{X: w.X, Z: w.Z, Level: w.Level})
	}
	return mulereceiverplan.PlainRouteSafe(here, res.OK, corners, func(p tile.Tile) bool { return webwalk.InZones(wwPoint(p), rects) })
}

// mulePlanCache remembers the last wolf-free plan verdict so fare-earning steps, which run
// every few seconds in one place, do not replan the whole map each time.
type mulePlanCache struct {
	valid bool
	at    time.Time
	from  tile.Tile
	coins int
	ok    bool
	fare  int
}

func (c *mulePlanCache) get(here tile.Tile, coins int) (ok bool, fare int, hit bool) {
	if !c.valid || time.Since(c.at) > mulePlanReuse || coins != c.coins || here.Level != c.from.Level || tile.Chebyshev(here, c.from) > 6 {
		return false, 0, false
	}
	return c.ok, c.fare, true
}

func (c *mulePlanCache) put(here tile.Tile, coins int, ok bool, fare int) {
	*c = mulePlanCache{valid: true, at: time.Now(), from: here, coins: coins, ok: ok, fare: fare}
}

// routeFare is the coins a planned route spends on fares and tolls, all of them together.
func routeFare(r webwalk.Route) int {
	total := 0
	for _, h := range r.Hops() {
		if h.Requires != nil && h.Requires.Currency != nil && h.Requires.Currency.Name == freshstart.Coins {
			total += h.Requires.Currency.Amount
		}
	}
	return total
}

// returnHome takes the mule back to its post without entering wolf territory. See this
// file's top comment. Returns true on the post; false on a stop request or once
// muleHomeBudget runs out (the caller retries next Loop).
func (b *muleBot) returnHome(ctx *Context) bool {
	if ctx.Stopped() {
		return false
	}
	home := *b.homeTile
	rects := muleWolfRects()
	// AvoidHostiles (the static-spawn break-off) is off on purpose. It knows two species,
	// giant bats and White wolves, and these routes already keep clear of both (the bats are
	// an automatic zone, the wolves are rects). What it adds here is harm: its escape ring
	// is not zone-aware, and in the lab a mule starting beside the 2870,3438 sentry broke off
	// NORTH, up the mountain (2865,3443 -> 2861,3465), instead of taking its planned route
	// east out of the sentry's 5-tile chase range. A hit on the way is still the Guardian's
	// combat flee, whose fleePlanner.leastExposed is wolf-territory aware.
	walk := webwalk.WalkOptions{Radius: 0, Timeout: muleHomeWalkTimeout, AvoidRects: rects}
	d := b.deps(ctx)
	fd := muleFreshStartDeps(d, b.walker)
	coinDeps := muleFisherDeps(d, b.walker)
	thief := freshstartrunner.NewThief()
	var cache mulePlanCache
	var last mulereceiverplan.HomeStep
	// One warning per tile, not one per second of muleHomeBudget. See fisherrunner.NoteArrival.
	localNoted := map[tile.Tile]bool{}
	// A failed pickpocket deals stun damage (content ~fail_pick_pocket calls ~damage_self),
	// which the Guardian's blind InCombat flee reads as an attack. Live 2026-09-19 after the
	// first deploy of this route: the mule fled 20-36 tiles from the Lumbridge Men after every
	// failure and walked back, earning ~4 coins in 3 minutes. The fisher's fund phase hit the
	// same thing and suppresses the flee only while standing at the spot (fisher.go); so does
	// this. Travel keeps the flee.
	expectCombat := func(on bool) {
		if ctx.Guardian != nil {
			ctx.Guardian.CombatExpected = on
		}
	}
	defer expectCombat(false)
	deadline := time.Now().Add(muleHomeBudget)
	for !ctx.Stopped() && time.Now().Before(deadline) {
		expectCombat(false)
		if !waitForGuardian(ctx) {
			return false
		}
		cur := currentTile(ctx)
		if cur == nil {
			if !ctx.Delay(time.Second) {
				return false
			}
			continue
		}
		here := *cur
		coins := fisherrunner.Coins(coinDeps)
		_, away := freshstart.PickpocketSpotFor(&here, freshstart.PickpocketSpot)
		v := mulereceiverplan.HomeView{
			AtHome:    here == home,
			Coins:     coins,
			Fare:      freshstart.FareRemaining(&here, freshstart.DefaultFare),
			OnKaramja: freshstart.OnKaramjaSide(here),
		}
		v.FarFromFundSpot = away && !v.OnKaramja
		if !v.AtHome {
			v.LocalSafe = muleLocalWalkSafe(ctx.Pack, here, home, rects)
			// muleLocalWalkSafe's first branch is a 12-tile radius that never asks whether a
			// route home exists, so a sealed tile close to the post reads as safe and the mule
			// spends its whole 30-minute budget on a local walk that cannot arrive, writing one
			// line at the start and nothing after. Observability only, see
			// internal/scripts/fisherrunner/arrivalnote.go.
			if v.LocalSafe && !localNoted[here] {
				localNoted[here] = true
				fisherrunner.NoteArrival(ctx.Pack, ctx.Log, "the mule's local walk home", here, here, home)
			}
		}
		if !v.AtHome && !v.LocalSafe {
			ok, fare, hit := cache.get(here, coins)
			if !hit {
				route := b.walker.Plan(wwPoint(home), walk)
				ok, fare = route.OK, routeFare(route)
				cache.put(here, coins, ok, fare)
				// Logged once it can be walked, not on every coin earned while funding.
				if ok && coins >= fare {
					ctx.logf("mule home: wolf-free route from %d,%d,%d: cost %d, %d hop(s), %d coins in fares", here.X, here.Z, here.Level, route.Cost, len(route.Hops()), fare)
				}
			}
			v.SafeRoute, v.RouteFare = ok, fare
		}
		step := mulereceiverplan.DecideHome(v)
		if step != last {
			ctx.logf("mule home: %s (at %d,%d,%d, %d coins, fare still ahead %d)", step, here.X, here.Z, here.Level, coins, v.Fare)
			last = step
		}
		switch step {
		case mulereceiverplan.HomeArrived:
			return true
		case mulereceiverplan.HomeWalkLocal:
			if freshstartrunner.WalkNear(fd, home, 0, muleLocalWalkTimeout) {
				return true
			}
			cache = mulePlanCache{}
			if !ctx.Delay(time.Second) {
				return false
			}
		case mulereceiverplan.HomeWalk:
			res := b.walker.WalkTo(wwPoint(home), walk)
			if res.Arrived() {
				return true
			}
			cache = mulePlanCache{}
			ctx.logf("mule home: walk did not arrive (%v: %s) at %v", res.Outcome, res.Reason, res.End)
			if res.Outcome == webwalk.OutcomeStopped || !ctx.Delay(2*time.Second) {
				return false
			}
		case mulereceiverplan.HomeToFundSpot:
			if !muleMakeFareRoom(ctx, ctx.Config) {
				if !ctx.Delay(30 * time.Second) {
					return false
				}
				continue
			}
			spot := freshstart.PickpocketSpot
			res := b.walker.WalkTo(wwPoint(spot), webwalk.WalkOptions{Radius: muleFundSpotRadius, Timeout: muleHomeWalkTimeout, AvoidRects: rects})
			if res.Outcome == webwalk.OutcomeStopped {
				return false
			}
			cache = mulePlanCache{}
			if !res.Arrived() {
				// PickpocketStep has its own way of finding a Man and of walking to the spot
				// when none turns up; let it try rather than walking the same failed route.
				ctx.logf("mule home: walk to the Lumbridge pickpocket spot did not arrive (%v: %s) at %v — pickpocketing from here", res.Outcome, res.Reason, res.End)
				freshstartrunner.PickpocketStep(fd, thief, muleFundOpts(fundTarget(v)))
			}
		case mulereceiverplan.HomeFund:
			if !muleMakeFareRoom(ctx, ctx.Config) {
				if !ctx.Delay(30 * time.Second) {
					return false
				}
				continue
			}
			expectCombat(tile.Chebyshev(here, freshstart.PickpocketSpot) <= pickpocketCombatRadius)
			freshstartrunner.PickpocketStep(fd, thief, muleFundOpts(fundTarget(v)))
		}
	}
	if !ctx.Stopped() {
		ctx.logf("mule home: not back on the post after %s — trying again", muleHomeBudget)
	}
	return false
}

// fundTarget is the coins to earn before setting off: the fares still ahead, or the whole
// planned route's fares when the planner found one that costs more.
func fundTarget(v mulereceiverplan.HomeView) int {
	if v.SafeRoute && v.RouteFare > v.Fare {
		return v.RouteFare
	}
	return v.Fare
}

func muleFundOpts(fare int) freshstartrunner.PickpocketOpts {
	return freshstartrunner.PickpocketOpts{
		Target: freshstart.DefaultPickpocketTarget, Spot: freshstart.PickpocketSpot,
		RestBelow: freshstart.RestBelowHP, Need: fare,
	}
}
