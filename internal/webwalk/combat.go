package webwalk

import (
	"fmt"
	"time"

	"rsb0t-go/internal/randomevents"
)

// OutcomeDied means the player died during the walk. The walker stops rather than replanning
// from the respawn point.
const OutcomeDied Outcome = "died"

// CombatClient is optional on a Client. When the Client implements it, a walk resolves
// danger zones against the player's combat level, notices a death, holds course under attack
// (as the TS WalkExecutor does), and can run or flee per WalkOptions.Combat. Without it, zones
// with a combat condition apply (an unknown level fails safe) and combat is not watched.
type CombatClient interface {
	// CombatLevel is the player's combat level; false until the server has sent it.
	CombatLevel() (int, bool)
	// InCombat reports a hitmark on the player within the last few ticks.
	InCombat() bool
	// HP is the player's current and maximum hitpoints; false until known.
	HP() (cur, max int, ok bool)
	RunEnabled() bool
	SetRun(on bool) error
	// DiedSince reports whether the death message arrived at or after mark.
	DiedSince(mark time.Time) bool
	// Attackers are the tiles of NPCs and players within radius that target the player.
	Attackers(radius int) []Point
}

// CombatOptions tune what a walk does when the player is attacked. The zero value is the TS
// behaviour: keep walking, and do not count the stall a fight causes as a failed walk.
type CombatOptions struct {
	// RunWhenAttacked turns run on (and clicks with run held) once the player is in combat.
	RunWhenAttacked bool
	// RunBelowHP turns run on once hitpoints are at or below this percentage of the maximum;
	// 0 disables it.
	RunBelowHP int
	// FleeWhenAttacked drops the route when an attacker targets the player and plans again
	// with a no-go square of FleeRadius around each attacker, so the new route leaves the
	// fight and does not walk back into it. At most maxFlees times a walk.
	FleeWhenAttacked bool
	// FleeRadius is the half-width of each attacker's square; 0 means defaultFleeRadius.
	FleeRadius int
}

func (c CombatOptions) any() bool { return c.RunWhenAttacked || c.RunBelowHP > 0 || c.FleeWhenAttacked }

const (
	defaultFleeRadius = 6
	maxFlees          = 4
	attackerRadius    = 15
	// combatReclick is how long a fighting player may stand still before the walker clicks
	// again when a combat option is set (a walk click cancels auto retaliate's attack).
	combatReclick = 2 * tick
)

// walkState is one WalkTo's zones and combat bookkeeping.
type walkState struct {
	started time.Time
	zones   []Rect
	// catalog is the part of zones that came from KnownDangerZones (named and automatic), as
	// opposed to the caller's own AvoidRects: what planOutOfPocket may set aside.
	catalog []Rect
	// pocket is set while the route being followed is planOutOfPocket's: its clicks may cross
	// the catalog zones too, or the first one out of the pocket would be refused.
	pocket       bool
	flee         []Rect
	flees        int
	combat       CombatOptions
	running      bool
	warnedCombat bool
	// avoidHostiles is WalkOptions.AvoidHostiles, resolved once for the walk. See
	// HostileClient: it is opt-in so no existing caller's walks change.
	avoidHostiles bool
	// clickTiles is the last movement click's tiles as the engine steps them (click), for
	// AsidePoll.Path.
	clickTiles []Point
	// teleDecisions counts the BeforeFollow decisions that planned something this walk (maxTeleDecisions bounds them),
	// teleMoved that one moved the player: at most one move a walk, across the ladder's rungs.
	teleDecisions int
	teleMoved     bool
	// crossCredit is the time this walk's ship crossings have taken (crossHop): the wait for a sailing is not walking, so
	// it does not count against the walk's deadline (pastDeadline).
	crossCredit time.Duration
	// clicks counts the movement clicks this walk has sent (sendWalk): an abandoned walk that never clicked has no path to
	// stop (stopMoving).
	clicks int
	// lagHeld is the time this walk has waited for a lagging view (holdForLag), lagCredit what the deadline gives back for
	// it, and blindSaid whether NoBlind's refusal has been logged.
	lagHeld, lagCredit time.Duration
	blindSaid          bool
}

func (w *Walker) combatClient() CombatClient {
	cc, _ := w.Client.(CombatClient)
	return cc
}

// beginWalk resolves the walk's danger zones (the named ones, the ad-hoc ones and, unless
// opted out, the automatic ones, as WalkExecutor.walkTo does) and resets combat state.
func (w *Walker) beginWalk(dest Point, o WalkOptions) {
	w.walk = walkState{started: w.Client.Now(), combat: o.Combat, avoidHostiles: o.AvoidHostiles}
	var ctx ZoneContext
	w.walk.zones, ctx = w.resolveZones(dest, o)
	w.walk.catalog = ResolveDangerZones(o.AvoidZones, nil, ctx)
	if len(w.walk.zones) > 0 {
		w.logf("walk to %s avoids %d zone rectangle(s) (combat level %d, known %v)", dest, len(w.walk.zones), ctx.CombatLevel, ctx.CombatKnown)
	}
}

// resolveZones is the danger-zone list a walk to dest with o avoids, from the player's live
// tile and combat level. Shared by beginWalk and Plan so a preview can never disagree with
// the walk it previews.
func (w *Walker) resolveZones(dest Point, o WalkOptions) ([]Rect, ZoneContext) {
	me, ok := w.Client.Tile()
	return w.resolveZonesAt(dest, o, me, ok)
}

// resolveZonesAt is resolveZones for a walk that starts at me (haveMe false: an unknown start). The start matters: a zone
// that allows an endpoint inside it exempts a walk that STARTS inside it, so the zones a walk from a teleport's landing
// would avoid are not the ones the account's own tile resolves.
func (w *Walker) resolveZonesAt(dest Point, o WalkOptions, me Point, haveMe bool) ([]Rect, ZoneContext) {
	ctx := ZoneContext{IncludeAutomatic: !o.NoAutomaticZones, Dest: &dest, Hurt: o.Hurt}
	if haveMe {
		start := me
		ctx.Start = &start
		// A pocket whose only way out leads into the Wilderness zone (the Mage Bank, the King Black
		// Dragon's lair): the zone rule refuses every hop into a zone from outside it, so with the
		// automatic zones on the first plan from there is always "unreachable". See
		// randomevents.PocketAt for the three accounts that sat in the Mage Bank for twelve hours.
		if ctx.IncludeAutomatic {
			if name, in := randomevents.PocketAt(me.X, me.Z, me.Level); in {
				ctx.IncludeAutomatic = false
				// Said only when the walk is bound out of the pocket: a Mage Bank trip's own walks to
				// Gundai and Lundail stay inside it and need no remark.
				if _, destIn := randomevents.PocketAt(dest.X, dest.Z, dest.Level); !destIn {
					w.logf("walk from %s: standing in %s, whose way out runs through the danger zones; planning with the automatic zones off", me, name)
				}
			}
		}
	}
	if cc := w.combatClient(); cc != nil {
		ctx.CombatLevel, ctx.CombatKnown = cc.CombatLevel()
	}
	return ResolveDangerZones(o.AvoidZones, o.AvoidRects, ctx), ctx
}

// activeZones are the walk's danger zones and the squares it has fled from.
func (w *Walker) activeZones() []Rect {
	if len(w.walk.flee) == 0 {
		return w.walk.zones
	}
	out := make([]Rect, 0, len(w.walk.zones)+len(w.walk.flee))
	out = append(out, w.walk.zones...)
	return append(out, w.walk.flee...)
}

func (w *Walker) runFlag() bool { return w.Run || w.walk.running }

func (w *Walker) inCombat() bool {
	cc := w.combatClient()
	return cc != nil && cc.InCombat()
}

func (w *Walker) died() bool {
	cc := w.combatClient()
	return cc != nil && cc.DiedSince(w.walk.started)
}

// combatTick applies WalkOptions.Combat once per walk poll. It turns run on when asked, and
// returns flee when new attackers mean the route should be planned again around them.
func (w *Walker) combatTick(dest Point) (flee bool, why string) {
	cc := w.combatClient()
	c := w.walk.combat
	if cc == nil || !c.any() {
		return false, ""
	}
	in := cc.InCombat()
	reason := ""
	if c.RunWhenAttacked && in {
		reason = "under attack"
	}
	if c.RunBelowHP > 0 {
		if cur, max, ok := cc.HP(); ok && max > 0 && cur*100 <= c.RunBelowHP*max {
			reason = fmt.Sprintf("hitpoints %d/%d", cur, max)
		}
	}
	if reason != "" && !w.walk.running {
		w.walk.running = true
		if !cc.RunEnabled() {
			if err := cc.SetRun(true); err != nil {
				w.logf("%s: turning run on failed: %v", reason, err)
			} else {
				w.logf("%s: turning run on", reason)
			}
		} else {
			w.logf("%s: run is already on, clicking with run held", reason)
		}
	}
	if !in || !c.FleeWhenAttacked || w.walk.flees >= maxFlees {
		return false, ""
	}
	r := c.FleeRadius
	if r <= 0 {
		r = defaultFleeRadius
	}
	var added []Point
	for _, a := range cc.Attackers(attackerRadius) {
		if InZones(a, w.walk.flee) {
			continue
		}
		sq := Rect{MinX: a.X - r, MaxX: a.X + r, MinZ: a.Z - r, MaxZ: a.Z + r, Level: a.Level}
		if sq.Contains(dest) {
			continue
		}
		w.walk.flee = append(w.walk.flee, sq)
		added = append(added, a)
	}
	if len(added) == 0 {
		return false, ""
	}
	w.walk.flees++
	return true, fmt.Sprintf("attacked by %d attacker(s) at %v, planning around them (flee %d/%d)", len(added), added, w.walk.flees, maxFlees)
}

// zonePocketTiles is the size below which a start the danger zones wall in is a pocket to walk
// out of, not somewhere to stay; freshstartrunner's constant of the same name has the real-pack
// measurements (101 tiles from the 2835,3468 ledge, 287 from the far Camelot evergreens, 75,151
// from anywhere in Seers, Catherby or Camelot).
const zonePocketTiles = 2000

// planOutOfPocket is route, or, when route failed only because catalog danger zones wall the
// player into a pocket, a route out of the pocket. A start outside every rectangle but surrounded
// by them has no exemption (AllowWhenEndpointInside needs an endpoint INSIDE), so every search
// from it dies within a few hundred tiles and the walk fails for good: wcrg13 lost the last nine
// minutes of its run that way on 2026-09-22. freshstartrunner.planAvoidingZones fixed its own
// router; this is the same rule for the Walker.
//
// The route it returns ends at pocketExit's tile, the nearest one outside every zone that can
// only be reached by crossing one, not at dest: walkOnce plans again from there with the zones
// back on. Planning all the way to dest without the zones would let the escape follow a zone for
// as long as it is the shorter way, which from the 2835,3468 ledge is the length of the wolf pass
// (review, 2026-09-25).
//
// Only the catalog's zones are set aside. The caller's own AvoidRects and the squares this walk
// fled from stay hard: a caller that named a rectangle meant it, and MuleReceiver's route home
// relies on that. Plan (the preview) does not escape pockets and still reports them unreachable.
func (w *Walker) planOutOfPocket(me, dest Point, opt Options, route Route) (Route, bool) {
	if route.OK || route.Reason != "unreachable" || route.Expanded >= zonePocketTiles || len(w.walk.catalog) == 0 {
		return route, false
	}
	hard := rectsWithout(opt.AvoidZones, w.walk.catalog)
	if len(hard) == len(opt.AvoidZones) {
		return route, false
	}
	exit, ok := pocketExit(w.Planner.col, me, dest, w.walk.catalog, hard)
	if !ok {
		return route, false
	}
	esc := opt
	esc.AvoidZones = hard
	esc.WalkOnly = true
	out := w.Planner.Plan(me, exit, esc)
	if !out.OK || out.End() != exit {
		return route, false
	}
	w.logf("walk from %s: the danger zones wall in a pocket of %d tiles, so crossing out to %s", me, route.Expanded, exit)
	return out, true
}

// pocketExitBudget bounds pocketExit's search; a zone crossing longer than this is not a way out
// of a pocket but a walk through the zone.
const pocketExitBudget = 20000

// pocketExit is the nearest tile, by steps, outside every zone from which the zones do NOT wall the
// player in again (its open region is larger than zonePocketTiles or holds dest), reached from me by crossing zoned tiles and never hard ones: where a walk out of
// a pocket should stop and plan again with the zones. Same level only.
//
// It maps the pocket first (every tile reachable without entering a zone), then searches through
// the zones from its edge, so a step into a zone and straight back is never taken for a way out.
// A region reached that way which is itself smaller than zonePocketTiles is not a way out either:
// it joins the pocket and the search goes on from its edge. Live on the lab engine 2026-09-25, from
// wcrg13's ledge at 2835,3468, the nearest tile past the zone was 2850,3450, a one-tile gap between
// two dens; the walk escaped there, found itself walled in again, escaped back, and repeated that
// until it timed out.
func pocketExit(col Collision, me, dest Point, zoned, hard []Rect) (Point, bool) {
	neighbours := func(p Point, visit func(Point)) {
		mask := col.ExitMask(p.X, p.Z, p.Level)
		for d := 0; d < 8; d++ {
			if mask&(1<<uint(d)) == 0 {
				continue
			}
			next := Point{X: p.X + dirX[d], Z: p.Z + dirZ[d], Level: p.Level}
			if !InZones(next, hard) {
				visit(next)
			}
		}
	}
	pocket := map[Point]bool{}
	crossSeen := map[Point]bool{}
	var crossing []Point
	// absorb floods the open (unzoned) region around start into the pocket, up to limit tiles,
	// queueing the zoned tiles on its edge for the crossing search. It reports whether the region
	// is itself walled in: within limit tiles and without the destination in it.
	absorb := func(start Point, limit int) bool {
		region := map[Point]bool{start: true}
		queue := []Point{start}
		var edge []Point
		for len(queue) > 0 {
			if len(region) > limit || region[dest] {
				return false
			}
			cur := queue[0]
			queue = queue[1:]
			neighbours(cur, func(next Point) {
				switch {
				case InZones(next, zoned):
					edge = append(edge, next)
				case !region[next] && !pocket[next]:
					region[next] = true
					queue = append(queue, next)
				}
			})
		}
		for p := range region {
			pocket[p] = true
		}
		for _, e := range edge {
			if !crossSeen[e] {
				crossSeen[e] = true
				crossing = append(crossing, e)
			}
		}
		return true
	}
	if !absorb(me, zonePocketTiles) {
		return Point{}, false // not a pocket at all
	}
	for n := 0; len(crossing) > 0 && n < pocketExitBudget; n++ {
		cur := crossing[0]
		crossing = crossing[1:]
		var exits []Point
		neighbours(cur, func(next Point) {
			if pocket[next] {
				return
			}
			if !InZones(next, zoned) {
				exits = append(exits, next)
				return
			}
			if !crossSeen[next] {
				crossSeen[next] = true
				crossing = append(crossing, next)
			}
		})
		for _, exit := range exits {
			if pocket[exit] {
				continue
			}
			if !absorb(exit, zonePocketTiles) {
				return exit, true
			}
		}
	}
	return Point{}, false
}

// clickZones is the zones a movement click keeps out of: the walk's, less the catalog's while
// following a route out of a pocket.
func (w *Walker) clickZones() []Rect {
	if w.walk.pocket {
		return rectsWithout(w.activeZones(), w.walk.catalog)
	}
	return w.activeZones()
}

// rectsWithout is from with each rectangle of drop removed once.
func rectsWithout(from, drop []Rect) []Rect {
	left := map[Rect]int{}
	for _, r := range drop {
		left[r]++
	}
	out := make([]Rect, 0, len(from))
	for _, r := range from {
		if left[r] > 0 {
			left[r]--
			continue
		}
		out = append(out, r)
	}
	return out
}
