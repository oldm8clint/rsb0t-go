package runecraftplan

import (
	"fmt"
	"strings"
)

// THE DIRECT-OP LEGS (CR-1 of docs/xp_rate_plan_2026-10-19.md, masterspeed step 2, 19 Oct 2026).
//
// Today the Nature gate's two walking legs are walk-then-click. Inside the temple the crafter sends a walk to the altar stand, polls until it
// SEES the account there (the walker looks every 600 ms, a whole tick, so the arrival is noticed up to a tick late, and on a route with a
// round trip near a tick the click that follows misses the next read too), and only then sends the altar click; after the craft's hold it does
// the same for the portal. The legs log (legs.go) put the two at 5.5 ticks inside to craft and 9.6 craft to exit; a client that walks 4 tiles
// and clicks the tick after it lands cannot be faster than 5 and 9.
//
// A DIRECT OP sends the walk and the click in ONE websocket message, the way the real client does a click on a loc: a MOVE_OPCLICK with the
// route to the stand, then the OPLOC. The engine reads both in one tick, walks the route and runs the op in the first tick after the last
// step (Player.processInteraction: tryInteract runs after updateMovement, and a loc's op needs stepsTaken === 0, so the op fires the tick
// AFTER the arrival, exactly as a click sent on arrival would). Nothing is gained on a zero-latency link that polls the tick the account
// lands; what is gained is every tick the polling and the round trip cost, and the account's own reaction (two reactions per leg become one).
//
// The engine does NOT path to a loc by itself: Player.pathToPathingTarget returns at once for a target that is not a PathingEntity, and with
// clientRoutefinder on (the default, and the live server) the walk is whatever the client queued. An OPLOC sent alone from four tiles away
// ends in "I can't reach that!". That is why the click rides with the route, and why the route is the pack's (a lone waypoint behind a pillar
// parks the account, runecraftrunner/gatewalk.go).
//
// SAFETY. A direct op is only ever an optimisation of a leg that has a plain way (walk to the stand, then click): any failure of the direct
// attempt (the engine said "I can't reach that!", nothing happened at all, the craft did not verify) falls back to the plain way, in the same
// stage where the essence is still in the pack, and for the next DirectBackoff cycles of that leg. DirectGiveUp failures in a row switch the
// leg off for the rest of the round. A result that was not SEEN in a lagging view says nothing about the op and changes nothing.

// Leg is one of the two walking legs of the cycle.
type Leg int

// The legs.
const (
	// LegAltar is the walk to the altar and the Craft-rune click; LegPortal the walk to the portal and its Use click.
	LegAltar Leg = iota
	LegPortal
	legCount
)

func (l Leg) String() string {
	if l == LegPortal {
		return "portal"
	}
	return "altar"
}

// The direct ops' bounds.
const (
	// DirectBackoff is how many cycles of a leg walk the plain way after a direct attempt failed, before the direct op is tried again.
	DirectBackoff = 10
	// DirectGiveUp is how many direct attempts of one leg in a row may fail before the leg is plain for the rest of the round.
	DirectGiveUp = 3
	// DirectMaxSteps is the longest route a direct op sends: the temple legs are four steps, so a route past this is not the leg the plan
	// knows and is walked the plain way.
	DirectMaxSteps = 12
)

// DirectOutcome is what one direct attempt came to.
type DirectOutcome int

// The outcomes.
const (
	// DirectOK: the op did what the plain way does (the craft verified, the account was seen outside).
	DirectOK DirectOutcome = iota
	// DirectFailed: the op demonstrably did not do it (the engine refused, or nothing happened in a view that was keeping up, or the craft
	// took the essence and gave no experience).
	DirectFailed
	// DirectUnseen: the result was not seen in time in a view that was lagging, so nothing is known about the op.
	DirectUnseen
)

// Leg modes for the cycle's log line (LegInput.AltarMode, PortalMode): "" for a leg that was walked the plain way without a direct attempt.
const (
	ModeDirect   = "direct"
	ModeFallback = "fell back"
)

type directLeg struct {
	// fails is the direct attempts in a row that failed; wait the plain cycles still to run before the next attempt; off: given up.
	fails, wait int
	off         bool
	// The round's counts: attempts that worked, attempts that failed, attempts not seen, cycles walked the plain way.
	ok, failed, unseen, plain int
}

// DirectOps is a round's memory of how the direct ops are doing. A nil *DirectOps (and one made with on false) never tries one.
type DirectOps struct {
	on   bool
	legs [legCount]directLeg
}

// NewDirectOps is the state for a round; on is craftRuneDirectOps.
func NewDirectOps(on bool) *DirectOps { return &DirectOps{on: on} }

// On reports whether the direct ops are switched on at all.
func (d *DirectOps) On() bool { return d != nil && d.on }

// Try reports whether the leg is to be sent as a direct op now. A leg in its back-off (or given up) answers false and counts the plain cycle
// the caller is about to run, so the back-off is measured in cycles. Call it once per cycle of the leg, after every other reason to walk the
// plain way has been ruled out (a lagging view, no route): a false from Try is a cycle spent.
func (d *DirectOps) Try(l Leg) bool {
	if !d.On() || l < 0 || l >= legCount {
		return false
	}
	s := &d.legs[l]
	if s.off {
		s.plain++
		return false
	}
	if s.wait > 0 {
		s.wait--
		s.plain++
		return false
	}
	return true
}

// Note records what a direct attempt came to and returns a line for the log ("" when nothing is worth saying): the plain-way notice after a
// failure, with the back-off, or that the leg was given up. why is the failure, in words.
func (d *DirectOps) Note(l Leg, o DirectOutcome, why string) string {
	if !d.On() || l < 0 || l >= legCount {
		return ""
	}
	s := &d.legs[l]
	switch o {
	case DirectOK:
		s.ok++
		back := s.fails > 0
		s.fails = 0
		if back {
			return fmt.Sprintf("direct op: the %s leg worked again", l)
		}
	case DirectUnseen:
		s.unseen++
	case DirectFailed:
		s.failed++
		s.fails++
		if s.fails >= DirectGiveUp {
			s.off = true
			return fmt.Sprintf("direct op: the %s leg failed %d times in a row (%s): walking to the stand and clicking for the rest of the round", l, s.fails, why)
		}
		s.wait = DirectBackoff
		return fmt.Sprintf("direct op: the %s leg failed (%s): walking to the stand and clicking instead, and for the next %d cycle(s)", l, why, DirectBackoff)
	}
	return ""
}

// Summary is one line of the round's counts for the log.
func (d *DirectOps) Summary() string {
	if !d.On() {
		return "direct ops: off"
	}
	var parts []string
	for l := Leg(0); l < legCount; l++ {
		s := d.legs[l]
		p := fmt.Sprintf("%s %d direct, %d failed, %d not seen, %d plain", l, s.ok, s.failed, s.unseen, s.plain)
		if s.off {
			p += " (given up)"
		}
		parts = append(parts, p)
	}
	return "direct ops: " + strings.Join(parts, "; ")
}

// Tried is how many direct attempts the leg has had (worked, failed or not seen).
func (d *DirectOps) Tried(l Leg) int {
	if d == nil || l < 0 || l >= legCount {
		return 0
	}
	s := d.legs[l]
	return s.ok + s.failed + s.unseen
}

// GivenUp reports whether the leg is plain for the rest of the round.
func (d *DirectOps) GivenUp(l Leg) bool {
	return d != nil && l >= 0 && l < legCount && d.legs[l].off
}
