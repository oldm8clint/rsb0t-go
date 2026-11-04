package randomevents

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// The combat flee's planning, bounded.
//
// WHAT WENT WRONG (live 15 Oct 2026, 07:56, 30 s pprof of the 359-account fleet at 2.2 cores): 45%
// of all CPU sat under Guardian.kick -> HandleEvade -> fleeFromCombatWalking -> localEscapeAvoiding
// -> leastExposedEscape (now fleePlanner.leastExposed) -> Pack.FindPathAvoiding -> Pack.search.
// Accounts training at the Asgarnian ice dungeon's rune pen (a safespot at 3041,9588 inside a pocket
// among eleven Ice warriors) had a Swarm event land beside them and then logged "combat flee
// (walking): gave up after the deadline, still threatened" every 20 s, one for five minutes and down
// to 13 of 53 hitpoints. Reproduced on the real pack (TestSwarmFleePlanIsBoundedAtTheIceDungeonPen):
//
//   - visibleWolfAvoidance treats every visible type 125 (Ice warrior) as a wolf, so with the herd
//     in view every tile closer to a warrior than the account is now is refused, and the pocket's
//     way out leads past them: nothing is reachable under that predicate;
//   - localEscapeAvoiding then ran its ring search, an A* per candidate, and each of the ~300
//     candidates is on ground the predicate cut off, so each was a full flood of the pocket
//     (298 searches, 57,000 expansions, 131 ms a plan, more at larger distances);
//   - its last resort, leastExposed, ignores the predicate entirely and found a destination
//     through the herd, which the walker (routing with the predicate) could not reach;
//   - fleeFromCombatWalking took that as "a door shut since planning", marked the destination bad
//     and went round again with no sleep and no log, re-running all of the above until its 20 s
//     deadline, then HandleEvade's 45 s wait did it again.
//
// THE FIX, in the layer that owns it: one Reach (a single breadth-first flood of the ground the
// predicate leaves, pathfind/reachset.go) answers "can I get there" for every candidate exactly, so
// only candidates that can be reached are searched, and the walker is handed the route the plan
// found instead of searching again under different rules. A plan costs one to three floods and a
// handful of searches (fleeRouteBudget and fleeLeastExposedBudget are ceilings per phase, not
// working numbers), and a hopeless plan is remembered (fleeNoEscapeMemory) so the next kick does not
// repeat it.
//
// The flood is built when it is first needed, not up front: on open ground the first candidate is
// reachable and one search is the whole plan (304 expansions at the Lumbridge pickpocketing spot,
// TestOpenGroundPlanIsOneSearchAndNoFlood), so a flood of the 96-tile window there would be the
// plan's whole cost. It is built the moment a search fails
// (that search has already flooded the candidate's component, so the cut-off ground is known to be
// here) or after fleeFloodAfter searches have not found a landing.
//
// WHAT THE FIRST VERSION GOT WRONG, and the review that found it (15 Oct 2026, 2,000 comparisons of the
// pre-change search against the planner on the real map): one 80-search budget was shared by the
// preferred-distance passes, the least-exposed fallback and every shorter rung. On the White Wolf
// Mountain edge the main passes spend searches on candidates whose shortest route crosses wolf
// territory and is refused for it, so three origins (Catherby town 2830,3467 on HandleEvade's second
// attempt, the mountain foot 2864,3445, and 2838,3464 at distance 40) used all 80 there; the fallback
// and the rungs never ran and the account stood and fought where the old flee walked. Now:
//
//   - a search is not spent on a candidate that is certain to fail. A third flood, the ground the
//     predicate leaves AND outside wolf territory (dryReach), is the exact test for "some route to this
//     candidate crosses no territory": a candidate it cannot reach has only routes with exposure, so
//     the exposure rule would reject whatever the search returned. A walkable candidate that is itself
//     in territory is rejected the same way, with no flood at all;
//   - a candidate's verdict is remembered for the plan: the shorter rungs ask for most of the same
//     ring candidates again, and the same candidate always gets the same answer, so a rung only
//     searches what the earlier ones did not;
//   - the budget is per phase, not per plan: the main passes, the least-exposed scan and each rung
//     start their own (beginPhase), so one spent phase cannot starve the next;
//   - the least-exposed scan stops the moment it holds a landing with no exposure, because it only
//     replaces its pick for a strictly lower one and nothing is lower than none.
//
// Every one of these leaves the pick unchanged: a search is skipped only when its outcome is already
// known, and a stop only when no later candidate can win.
//
// WHAT THE BOUND DID NOT FIX (17 Oct 2026): a flee that cannot work. The first bullet above was left
// standing, so a flee in the ice dungeon was fast and still found nothing: the flood from the stand tile,
// 1 tile under the herd's refusal radii against 1,284 open within the window, said so in "1 reachable
// tiles" on every line, for ten minutes at a time, in three deaths. The cause was not the predicate but
// asking the ring search to shake a Shade, a Strange plant or a Zombie, which only distance from where they
// began shakes: an event monster with a leash is now left by walking out of it (event_leash.go), with its
// own graded margin from the herd. The refusal stays for the ring search itself (visibleWolfAvoidance,
// flee_walking.go): a threat with no leash, or an ordinary attacker, finds nowhere to go among the warriors
// and the account holds its ground (the cornered path) instead of walking through them.

const (
	// fleeWindowRadius is how far from the account a flee route may stray: escape routes are local.
	// The engine refuses a MOVE_GAMECLICK whose first waypoint is more than 104 tiles away, so the
	// window must stay inside that (flee_door_test.go pins it).
	fleeWindowRadius = 96

	// fleeRouteBudget is the most fresh A* route searches the main passes of one escape may run (the
	// toward-biased pass and the pass of the rest, every ring, for one distance). With the floods
	// answering reachability exactly, a search is only spent on a candidate that can be walked to
	// without crossing wolf territory, so this is a ceiling, not a working number: an escape that
	// reaches it has been handed a map where most candidates fail the landing rules.
	fleeRouteBudget = 80

	// fleeLeastExposedBudget is the same ceiling for the least-exposed scan, which compares routes
	// and so must look at most of a ring or two to choose: at distance 40 that is 136 candidates.
	fleeLeastExposedBudget = 160

	// fleeFloodAfter is how many route searches a plan runs on faith before it floods the reachable
	// ground to screen the rest: the first candidates are tried directly because on open ground one
	// of them is the answer. A failed search floods at once, without waiting for this.
	fleeFloodAfter = 3

	// fleeNoEscapeMemory is how long a plan that found nowhere to go is remembered for the same
	// account tile and the same threat tile. The ground does not change in eight seconds, the
	// guardian re-enters the flee every few hundred milliseconds, and a replan costs a flood.
	fleeNoEscapeMemory = 8 * time.Second
)

// escapePlan is a planned flee: where it lands and the corner-compressed route that gets there
// from the account's tile (Route[0] is that tile).
type escapePlan struct {
	Dest  pathfind.NavPoint
	Route []pathfind.NavPoint
}

// fleePlanner plans one flee from one tile. It floods the reachable ground once per set of rules
// (with the caller's avoid predicate, with wolf territory refused as well, and without the predicate
// for the least-exposed fallback) and shares those floods across the preferred distance and every
// shorter rung. Route searches are budgeted per phase (beginPhase) and a candidate is searched once
// per plan.
type fleePlanner struct {
	pack             *pathfind.Pack
	origin           pathfind.NavPoint
	threatX, threatZ int
	avoid            func(int, int, int) bool
	toward           pathfind.NavPoint

	local *pathfind.Reach // the ground inside the window that avoid leaves
	dry   *pathfind.Reach // local, and outside wolf territory too: what a route with no exposure can use
	open  *pathfind.Reach // the ground inside the window, avoid not applied

	routes      int // A* route searches spent so far, in every phase
	phaseRoutes int // searches spent in the current phase
	phaseBudget int // the current phase's ceiling

	// The ceilings the main passes and the least-exposed scan each start with. Fields so a test can
	// shrink one and show the other is not starved by it; nothing else assigns them.
	mainBudget, exposedBudget int

	rings map[[2]int]ringCandidates // ring candidates by {radius, distance}: both passes and the fallback reuse them

	mainVerdicts    map[pathfind.NavPoint]mainVerdict    // main passes: what each candidate came to
	exposedVerdicts map[pathfind.NavPoint]exposedVerdict // least-exposed scan: likewise
}

// mainVerdict is what routing to one candidate under localAvoid came to: the plan it makes, or none.
type mainVerdict struct {
	plan escapePlan
	ok   bool
}

// exposedVerdict is the same for the least-exposed scan, which ranks its landings by exposure.
type exposedVerdict struct {
	plan     escapePlan
	exposure int
	ok       bool
}

// ringCandidates is ringFleeCandidates' two halves for one ring.
type ringCandidates struct{ biased, rest []pathfind.NavPoint }

// ring is ringFleeCandidates, remembered: generating a ring sorts it, and a plan asks for the same
// ring in both passes and again in the least-exposed fallback.
func (pl *fleePlanner) ring(radius, distance int) ([]pathfind.NavPoint, []pathfind.NavPoint) {
	key := [2]int{radius, distance}
	if c, ok := pl.rings[key]; ok {
		return c.biased, c.rest
	}
	biased, rest := ringFleeCandidates(pl.origin, pl.threatX, pl.threatZ, pl.toward, radius, distance)
	if pl.rings == nil {
		pl.rings = make(map[[2]int]ringCandidates)
	}
	pl.rings[key] = ringCandidates{biased, rest}
	return biased, rest
}

func newFleePlanner(pack *pathfind.Pack, origin pathfind.NavPoint, threatX, threatZ int, avoid func(int, int, int) bool, toward pathfind.NavPoint) *fleePlanner {
	return &fleePlanner{pack: pack, origin: origin, threatX: threatX, threatZ: threatZ, avoid: avoid, toward: toward,
		mainBudget: fleeRouteBudget, exposedBudget: fleeLeastExposedBudget}
}

// window refuses the tiles a flee route may not use: another level, or beyond fleeWindowRadius.
func (pl *fleePlanner) window(x, z, level int) bool {
	return level != pl.origin.Level || chebyshev(x, z, pl.origin.X, pl.origin.Z) > fleeWindowRadius
}

// localAvoid is the window plus the caller's predicate.
func (pl *fleePlanner) localAvoid(x, z, level int) bool {
	return pl.window(x, z, level) || (pl.avoid != nil && pl.avoid(x, z, level))
}

// dryAvoid is localAvoid plus wolf territory: the ground a route with no exposure can use.
func (pl *fleePlanner) dryAvoid(x, z, level int) bool {
	return pl.localAvoid(x, z, level) || inWolfTerritory(pathfind.NavPoint{X: x, Z: z, Level: level})
}

func (pl *fleePlanner) localReach() *pathfind.Reach {
	if pl.local == nil {
		pl.local = pl.pack.ReachFrom(pl.origin, fleeWindowRadius, pl.localAvoid)
	}
	return pl.local
}

// dryReach is the ground a main-pass route may use if it is to stay out of wolf territory. Exact for
// the exposure rule: the main passes refuse a candidate whose route (the shortest one under
// localAvoid, which may cross territory) has any tile in territory, and a candidate dryReach cannot
// reach has no route that avoids territory, so every route to it is refused whichever the search
// returns. It is only meaningful for an origin outside territory (the main passes' case), and only
// differs from localReach when territory is within the window (territoryInWindow).
func (pl *fleePlanner) dryReach() *pathfind.Reach {
	if pl.dry == nil {
		pl.dry = pl.pack.ReachFrom(pl.origin, fleeWindowRadius, pl.dryAvoid)
	}
	return pl.dry
}

// territoryInWindow reports whether any tile of wolf territory can lie inside the flee window.
func (pl *fleePlanner) territoryInWindow() bool {
	if pl.origin.Level != 0 {
		return false
	}
	for _, d := range wolfDens {
		if chebyshev(d.x, d.z, pl.origin.X, pl.origin.Z) <= fleeWindowRadius+d.r {
			return true
		}
	}
	return false
}

func (pl *fleePlanner) openReach() *pathfind.Reach {
	if pl.avoid == nil {
		return pl.localReach()
	}
	if pl.open == nil {
		pl.open = pl.pack.ReachFrom(pl.origin, fleeWindowRadius, pl.window)
	}
	return pl.open
}

// screenReach is the tightest ground built so far for the main passes: the territory-free flood when
// there is one, else the plain local flood, else nil (nothing built yet).
func (pl *fleePlanner) screenReach() *pathfind.Reach {
	if pl.dry != nil {
		return pl.dry
	}
	return pl.local
}

// screenBuilt reports that the flood the main passes want exists: the territory-free one where
// territory is within the window (it is a subset of the local ground, so it refuses everything the
// local flood would and the territory-crossing candidates too), else the local one. The local flood
// alone does not count where territory is near: the least-exposed scan builds it for its own use, and
// a later rung's main passes still want the sharper one.
func (pl *fleePlanner) screenBuilt() bool {
	if pl.territoryInWindow() {
		return pl.dry != nil
	}
	return pl.local != nil
}

// buildScreen floods the ground screenBuilt describes.
func (pl *fleePlanner) buildScreen() {
	if pl.territoryInWindow() {
		pl.dryReach()
		return
	}
	pl.localReach()
}

// Stats is the plan's cost for a log line: the ground flooded and the route searches spent.
func (pl *fleePlanner) Stats() string {
	switch {
	case pl.dry != nil:
		return fmt.Sprintf("%d reachable tiles outside wolf territory, %d route searches", pl.dry.Size(), pl.routes)
	case pl.local != nil:
		return fmt.Sprintf("%d reachable tiles, %d route searches", pl.local.Size(), pl.routes)
	case pl.open != nil:
		return fmt.Sprintf("%d reachable tiles ignoring the predicate, %d route searches", pl.open.Size(), pl.routes)
	}
	return fmt.Sprintf("%d route searches, no flood needed", pl.routes)
}

// beginPhase starts a stretch of the plan with its own route-search budget, so a phase that spends
// its own cannot starve the next: the main passes, the least-exposed scan, each shorter rung.
func (pl *fleePlanner) beginPhase(budget int) {
	pl.phaseRoutes, pl.phaseBudget = 0, budget
}

// spent reports that the current phase has used its route budget.
func (pl *fleePlanner) spent() bool { return pl.phaseRoutes >= pl.phaseBudget }

// route is one A* search, counted against the plan and the current phase.
func (pl *fleePlanner) route(dest pathfind.NavPoint, avoid func(int, int, int) bool) pathfind.PathResult {
	pl.routes++
	pl.phaseRoutes++
	return pl.pack.FindPathAvoiding(pl.origin, dest, avoid)
}

// screen is the cheap, exact test before a search of the main passes: true while no flood has been
// built (the first candidates are tried directly), and afterwards worthRouting on the tightest ground.
func (pl *fleePlanner) screen(dest pathfind.NavPoint) bool {
	r := pl.screenReach()
	return r == nil || pl.worthRouting(r, dest)
}

// searched is called after each route search of the main passes: it builds the screening flood the
// moment a search fails or is refused for wolf exposure (the territory is here and the flood knows
// where it is), or once fleeFloodAfter searches have gone by.
func (pl *fleePlanner) searched(ok bool) {
	if !pl.screenBuilt() && (!ok || pl.routes >= fleeFloodAfter) {
		pl.buildScreen()
	}
}

// certainlyInTerritory reports a candidate that is itself a walkable tile in wolf territory: the
// search would land on it (a walkable target is its own and only goal) and its route would end in
// territory, which both the exposure rule and the least-exposed landing rule refuse. No search needed.
func (pl *fleePlanner) certainlyInTerritory(dest pathfind.NavPoint) bool {
	return inWolfTerritory(dest) && pl.pack.Walkable(dest.X, dest.Z, dest.Level)
}

// worthRouting is the cheap, exact screen before a search: the candidate must be reachable on the
// given ground, and a walkable one must not already be a detour longer than fleeLandingOK allows
// (the flood's step count is the route's length; two tiles of slack cover a start that had to be
// snapped onto walkable ground).
func (pl *fleePlanner) worthRouting(r *pathfind.Reach, dest pathfind.NavPoint) bool {
	if !r.CanReach(dest) {
		return false
	}
	if steps, ok := r.Steps(dest.X, dest.Z, dest.Level); ok && steps > maxFleeDetour(chebyshev(dest.X, dest.Z, pl.origin.X, pl.origin.Z))+2 {
		return false
	}
	return true
}

// escape is the flee choice for an account at the origin: the first ring candidate, toward-biased
// ones before the rest, that can be walked to, lands clear of the threat and the dangerous ground,
// and stays out of wolf territory (see localEscapeAvoiding's doc comment in handle.go for the
// rules and why each exists). Its last resort is leastExposed. The main passes and the last resort
// each have their own search budget.
func (pl *fleePlanner) escape(distance int) (escapePlan, bool) {
	if pl.pack == nil {
		return escapePlan{}, false
	}
	// Inside White Wolf Mountain's wolf territory every direction is dangerous and the
	// visible-wolf hard avoidance leaves nothing reachable; choose by exposure instead.
	// The same choice is the fallback below when no route stays out of territory.
	if inWolfTerritory(pl.origin) {
		return pl.leastExposed(distance)
	}
	pl.beginPhase(pl.mainBudget)
	// Escape routes are local (localAvoid: the 96-tile window). Avoid exploring the whole mainland
	// for every unreachable ring candidate when hundreds of accounts react together.
	//
	// The Catherby beach can have no safe reachable tile in the first ring:
	// water and the wolf exclusion overlap it. Expand only after that search
	// fails; otherwise the same failure repeats without moving the account.
	//
	// The first pass is the toward-biased candidates exactly as before. Only when no ring
	// of them works does a second pass try the rest: from the Taverley foot of White Wolf
	// Mountain every candidate toward a Catherby safe point is across the wolf pass, and
	// the way back east is the only escape that does not enter wolf territory.
	for pass := 0; pass < 2 && !pl.spent(); pass++ {
		for radius := distance; radius <= 80 && !pl.spent(); radius += 8 {
			biased, rest := pl.ring(radius, distance)
			candidates := biased
			if pass == 1 {
				candidates = rest
			}
			for _, dest := range candidates {
				if pl.spent() {
					break
				}
				if inDangerZone(dest) {
					continue
				}
				if v := pl.mainVerdict(dest); v.ok {
					return v.plan, true
				}
			}
		}
	}
	// Beside the mountain (the Taverley foot, the camp's north edge) every route can brush
	// a wolf's reach. Standing still in combat is worse than the least exposed way out.
	return pl.leastExposed(distance)
}

// mainVerdict is what the main passes make of one ring candidate: the route to it under localAvoid
// must exist, land clear (fleeLandingOK) and cross no wolf territory (a flee that walks into
// territory from outside it is refused). Settled once per plan: the same candidate always gets the
// same answer, and a shorter rung asks for most of the candidates the preferred distance did.
func (pl *fleePlanner) mainVerdict(dest pathfind.NavPoint) mainVerdict {
	if v, seen := pl.mainVerdicts[dest]; seen {
		return v
	}
	remember := func(v mainVerdict) mainVerdict {
		if pl.mainVerdicts == nil {
			pl.mainVerdicts = make(map[pathfind.NavPoint]mainVerdict)
		}
		pl.mainVerdicts[dest] = v
		return v
	}
	// Known to fail without a search: a walkable target in territory ends the route there, and a
	// target with no territory-free route from here (the flood knows) has only refused routes.
	if pl.certainlyInTerritory(dest) || !pl.screen(dest) {
		return remember(mainVerdict{})
	}
	route := pl.route(dest, pl.localAvoid)
	if !route.OK || len(route.Waypoints) == 0 {
		pl.searched(false)
		return remember(mainVerdict{})
	}
	landed := route.Waypoints[len(route.Waypoints)-1]
	if !fleeLandingOK(pl.origin, landed, pl.threatX, pl.threatZ, route.Waypoints) {
		pl.searched(true)
		return remember(mainVerdict{})
	}
	// Refuse a flee that walks into wolf territory from outside it.
	if routeExposure(pl.origin, route.Waypoints) > 0 {
		pl.searched(false)
		return remember(mainVerdict{})
	}
	pl.searched(true)
	return remember(mainVerdict{plan: escapePlan{Dest: landed, Route: route.Waypoints}, ok: true})
}

// leastExposed is the flee choice for an account inside wolf territory, and the last resort for
// one beside it. Every ring candidate (the toward-biased ones first, then the rest) is routed
// without the visible-wolf hard avoidance, which is what boxed accounts in on the mountain, and the
// reachable landing outside territory with the fewest route tiles inside it wins; ties keep the
// usual order. Landing rules are the ordinary ones: outside dangerZones, 15 tiles from the account
// and the threat, and no barrier detour (maxFleeDetour).
//
// It has its own search budget (fleeLeastExposedBudget), and it stops at the first landing with no
// exposure: a later candidate replaces the pick only for a strictly lower exposure, and nothing is
// lower than none, so what the rest of the rings would have searched cannot change the answer.
func (pl *fleePlanner) leastExposed(distance int) (escapePlan, bool) {
	if pl.pack == nil {
		return escapePlan{}, false
	}
	pl.beginPhase(pl.exposedBudget)
	reach := pl.openReach()
	var best escapePlan
	bestExposure, found := 0, false
	for radius := distance; radius <= 80 && !pl.spent(); radius += 8 {
		biased, rest := pl.ring(radius, distance)
		for _, dest := range append(append([]pathfind.NavPoint(nil), biased...), rest...) {
			if pl.spent() || (found && bestExposure == 0) {
				break
			}
			if inDangerZone(dest) || !pl.worthRouting(reach, dest) {
				continue
			}
			v := pl.exposedVerdict(dest)
			if v.ok && (!found || v.exposure < bestExposure) {
				best, bestExposure, found = v.plan, v.exposure, true
			}
		}
		// A found landing is kept unless a later ring is strictly less exposed; one more
		// ring is enough to see whether stepping out further helps.
		if found && (bestExposure == 0 || radius >= distance+8) {
			break
		}
	}
	return best, found
}

// exposedVerdict is what the least-exposed scan makes of one ring candidate: the shortest route to
// it inside the window (visible wolves not avoided), with its landing and exposure. Settled once per
// plan, like mainVerdict.
func (pl *fleePlanner) exposedVerdict(dest pathfind.NavPoint) exposedVerdict {
	if v, seen := pl.exposedVerdicts[dest]; seen {
		return v
	}
	remember := func(v exposedVerdict) exposedVerdict {
		if pl.exposedVerdicts == nil {
			pl.exposedVerdicts = make(map[pathfind.NavPoint]exposedVerdict)
		}
		pl.exposedVerdicts[dest] = v
		return v
	}
	if pl.certainlyInTerritory(dest) {
		return remember(exposedVerdict{})
	}
	route := pl.route(dest, pl.window)
	if !route.OK || len(route.Waypoints) == 0 {
		return remember(exposedVerdict{})
	}
	landed := route.Waypoints[len(route.Waypoints)-1]
	if !fleeLandingOK(pl.origin, landed, pl.threatX, pl.threatZ, route.Waypoints) || inWolfTerritory(landed) {
		return remember(exposedVerdict{})
	}
	return remember(exposedVerdict{
		plan:     escapePlan{Dest: landed, Route: route.Waypoints},
		exposure: routeExposure(pl.origin, route.Waypoints),
		ok:       true,
	})
}

// heldTo returns plan with a route that keeps to avoid: the plan's own when every tile after the
// first already does, otherwise one search for a route to the same landing under it, and false when
// there is none. nil avoid holds nothing.
func (pl *fleePlanner) heldTo(plan escapePlan, avoid func(int, int, int) bool) (escapePlan, bool) {
	if avoid == nil || routeKeepsTo(plan.Route, avoid) {
		return plan, true
	}
	pl.beginPhase(1) // its own search, never starved by the phases before it
	route := pl.route(plan.Dest, avoid)
	if !route.OK || len(route.Waypoints) == 0 {
		return escapePlan{}, false
	}
	return escapePlan{Dest: route.Waypoints[len(route.Waypoints)-1], Route: route.Waypoints}, true
}

// routeKeepsTo reports whether no tile of a corner-compressed route after its first is one avoid
// refuses.
func routeKeepsTo(route []pathfind.NavPoint, avoid func(int, int, int) bool) bool {
	for i := 1; i < len(route); i++ {
		cx, cz := route[i-1].X, route[i-1].Z
		for cx != route[i].X || cz != route[i].Z {
			cx += sign(route[i].X - cx)
			cz += sign(route[i].Z - cz)
			if avoid(cx, cz, route[i].Level) {
				return false
			}
		}
	}
	return true
}

// fleeShorterRungs are the shorter distances a flee falls back to when the preferred one finds
// nothing, longest first. Below 4, "escape" is not meaningfully different from where the account is.
var fleeShorterRungs = []int{12, 8, 6, 4}

// shorter tries each shorter rung under distance and returns the first that finds a way out, with
// the rung used.
func (pl *fleePlanner) shorter(distance int) (plan escapePlan, rung int, ok bool) {
	for _, r := range fleeShorterRungs {
		if r >= distance {
			continue
		}
		if p, found := pl.escape(r); found {
			return p, r, true
		}
	}
	return escapePlan{}, 0, false
}

// ladder is the preferred distance, then each shorter rung: HandleEvade's "is there anywhere to go".
// fellBack is the shorter distance used, 0 when the preferred one worked.
func (pl *fleePlanner) ladder(distance int) (plan escapePlan, fellBack int, ok bool) {
	if p, found := pl.escape(distance); found {
		return p, 0, true
	}
	return pl.shorter(distance)
}

// fleeNoEscape is a plan that found nowhere to go, remembered so the next kick does not repeat it.
type fleeNoEscape struct {
	x, z, level int // the account's tile
	ax, az      int // the tile of the threat it was planned against
	at          time.Time
}

// fleeNoEscapes maps actions.Session -> fleeNoEscape, recentCombat's per-session shape.
var fleeNoEscapes sync.Map

// noteNoEscape records that no flee exists from (x,z,level) against a threat at ax,az.
func noteNoEscape(s actions.Session, x, z, level, ax, az int) {
	fleeNoEscapes.Store(s, fleeNoEscape{x: x, z: z, level: level, ax: ax, az: az, at: time.Now()})
}

// noEscapeRemembered reports whether a plan from this tile against a threat at ax,az (give or take
// the tile an idle account or a wandering monster shifts) already found nowhere to go within
// fleeNoEscapeMemory.
func noEscapeRemembered(s actions.Session, x, z, level, ax, az int) bool {
	v, ok := fleeNoEscapes.Load(s)
	if !ok {
		return false
	}
	return v.(fleeNoEscape).matches(x, z, level, ax, az, time.Now())
}

func (n fleeNoEscape) matches(x, z, level, ax, az int, now time.Time) bool {
	return now.Sub(n.at) < fleeNoEscapeMemory && n.level == level &&
		chebyshev(n.x, n.z, x, z) <= 1 && chebyshev(n.ax, n.az, ax, az) <= 2
}

// forgetNoEscape drops the memory, for a flee that found a way out after all.
func forgetNoEscape(s actions.Session) { fleeNoEscapes.Delete(s) }
