package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// voidHopWait is how long one hop is given before the next is sent: ten tiles at a run is about
// five game ticks. Once every heading has failed (voidStuckSlow hops in a row without moving) the
// walk waits voidStuckWait between tries instead: live 3 Oct, ~100 accounts sat at an invisible
// line at z 10088 sending a hop every three seconds (800+ each) to no effect. The account stays put
// for its own Genie or Old Man to fail-teleport it out, which is what got them out.
//
// "Did not move" is not the whole of being stuck. Live 14 Oct 2026 (build d5362bb2, 04:07-04:45) one account sat in the Varrock sewers' north
// cavern for 33 minutes sending a hop every three seconds toward a rally it could never reach, 684 of them: the hops turned
// away from each wall in turn, but the account also moved a tile or two between hops (its own script clicked east on every
// stall, the hops went north-west), and any move at all reset the stuck count to nought, so it never reached voidStuckSlow.
// So the walk also measures NET progress: the distance to the target must beat the best so far by voidProgressMin tiles, and
// voidFlatHops hops in a row that do not call the walk boxed in. A boxed-in walk waits voidStuckWait between hops like a stuck
// one (the account stays put for its own Genie or Old Man), and a site marked HandBack gives the account to the ladder instead.
const (
	voidHopWait   = 3 * time.Second
	voidStuckSlow = 6
	voidStuckWait = time.Minute
	// voidProgressMin is how many tiles nearer the target than ever before a hop must end to count as progress: a jitter of a
	// tile or two between hops, or a heading that ends a tile nearer, is not.
	voidProgressMin = 3
	// voidFlatHops is how many hops in a row without progress make a walk boxed in: a minute of hops at voidHopWait.
	voidFlatHops = 20
)

// voidWalk is the void walk's memory for the site it is walking out of.
type voidWalk struct {
	site         string
	lastX, lastZ int
	at           time.Time
	stuck        int
	hops         int
	// best is the nearest the walk has been to its target (valid once bestSet), flat the hops since it improved by
	// voidProgressMin, and boxed whether flat has reached voidFlatHops (cleared by the next progress).
	best    int
	bestSet bool
	flat    int
	boxed   bool
}

// progress records dist, the distance (tiles) still to go as of the hop about to be sent.
func (v *voidWalk) progress(dist int) {
	if !v.bestSet || dist <= v.best-voidProgressMin {
		v.best, v.bestSet, v.flat, v.boxed = dist, true, 0, false
		return
	}
	v.flat++
	if v.flat >= voidFlatHops {
		v.boxed = true
	}
}

// slow is whether the walk waits voidStuckWait between hops: every heading has failed, or the walk is boxed in.
func (v *voidWalk) slow() bool { return v.stuck >= voidStuckSlow || v.boxed }

// stuckCount is stuck, raised to voidStuckSlow while the walk is boxed in, for the callers that ask "has a wall turned back
// every heading" (pvpWalksToRally).
func (v *voidWalk) stuckCount() int {
	if v.boxed {
		return max(v.stuck, voidStuckSlow)
	}
	return v.stuck
}

// strandVoidWalk sends one straight hop toward site.VoidWalkTo, ignoring the collision pack, and
// notes whether the previous hop moved the account (VoidHop turns away from a wall that did not let it
// through). Called with actionMu held. It always reports the poll as taken.
func (g *Guardian) strandVoidWalk(site *StrandSite, x, z, level int) bool {
	v := &g.void
	if v.site != site.Name {
		*v = voidWalk{site: site.Name}
		g.log(fmt.Sprintf("strand: %s at %d,%d,%d — walking straight for %d,%d through open ground the pack calls solid",
			site.Name, x, z, level, site.VoidWalkTo.X, site.VoidWalkTo.Z))
	}
	g.voidHop(site.Name, x, z, level, *site.VoidWalkTo)
	return true
}

// voidHop sends one straight hop toward to unless the last one is still being given its time. The
// void walk and the PvP rally walk share it (and g.void, reset on entering a site).
func (g *Guardian) voidHop(name string, x, z, level int, to StrandTile) {
	v := &g.void
	wait := voidHopWait
	if v.slow() {
		wait = voidStuckWait
	}
	if !v.at.IsZero() && time.Since(v.at) < wait {
		return
	}
	if !v.at.IsZero() {
		if x == v.lastX && z == v.lastZ {
			v.stuck++
		} else {
			v.stuck = 0
		}
	}
	v.progress(max(abs(to.X-x), abs(to.Z-z)))
	tx, tz := VoidHop(x, z, to.X, to.Z, v.stuck)
	v.lastX, v.lastZ, v.at = x, z, time.Now()
	v.hops++
	if err := actions.Walk(g.session, []pathfind.NavPoint{{X: x, Z: z, Level: level}, {X: tx, Z: tz, Level: level}}, true); err != nil {
		g.log(fmt.Sprintf("strand: %s — void hop to %d,%d failed: %v", name, tx, tz, err))
		return
	}
	if v.hops%10 == 1 || v.stuck > 0 || v.flat == voidFlatHops {
		g.log(fmt.Sprintf("strand: %s — void hop %d from %d,%d to %d,%d (stuck %d, %d hops without progress, best %d tiles from the target)",
			name, v.hops, x, z, tx, tz, v.stuck, v.flat, v.best))
	}
}
