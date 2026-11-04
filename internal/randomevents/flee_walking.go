package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// This file is the real, production-viable counterpart to fleeFromCombat/HandleEvade's own
// ::tele-based escape in handle.go -- see fleeFromCombat's own doc comment for exactly why
// ::tele exists (a live 200-account local-engine soak found walking too slow and killed
// several accounts) and why it can never run against production: example.invalid gives a real
// account no such staff command, it only works here because this whole soak runs against
// the local test engine with cheats enabled. Users asked for a genuine
// walking/running escape that actually holds up, so this started as a NEW, separately
// callable path, proven out on its own (unit tests below) before fleeFromCombat's own
// actions.CheatEnabled gate was added to call it directly whenever cheats are off -- see
// fleeFromCombat's own doc comment in handle.go. That gate is what a real example.invalid launch
// runs today (CheatEnabled defaults false), so this file is no longer a standalone,
// unwired experiment: it is the actual production combat-flee path, verified end-to-end
// against a local no-cheats soak (see COLLAB.md's most recent entries) rather than only unit
// tests.
//
// Why a naive walk isn't enough (this is the actual problem, not just "call walkTo"):
// actions.Walk only builds and sends one MOVE_GAMECLICK packet and returns as soon as it's
// acknowledged by Send -- see its own doc comment, it does not block until the account
// arrives. This package's existing walkTo helper (handle.go) papers over that by sleeping a
// single fixed, blind "rough travel budget" Sleep after issuing the walk, checking
// combat/position only before and after, never once during. That is exactly the shape the
// ORIGINAL walking flee had before it was replaced by ::tele: a single blind multi-second
// commitment with zero monitoring in the middle, during which a chasing "event monster" (see
// fleeDistFor's own doc comment two files over) can land a lethal hit with nothing watching.
//
// fleeFromCombatWalking instead:
//   - turns running on (mirroring the real client's own RunManager.ts: under attack, any
//     energy at all is worth spending to break off, not gated behind the normal cruising
//     energy floor -- see wantRunForFlee below), since running halves the tiles-per-second
//     exposure window for the same route;
//   - polls InCombat()/Tile() every fleeWalkPollInterval WHILE nominally still travelling,
//     instead of sleeping the whole estimated duration blind;
//   - if combat is still ongoing after fleeWalkLegInterval with no progress, treats the
//     current destination as not having created real separation and picks a fresh, farther
//     candidate (biasedFleeCandidates/walkableFlee, dist escalated via fleeDistFor -- the
//     same proven candidate search and escalation curve the tele path already uses) and
//     re-issues the walk, rather than waiting out the rest of the original estimate;
//   - is bounded overall by fleeWalkDeadline, the same shape as this package's other retry
//     loops (strangePlantSolveDeadline, genieSolveDeadline), so a genuinely stuck case can't
//     hold this (and Guardian's actionMu, for any caller going through TryLock) forever.
//
// This mirrors the real, production-proven client's own eventEvade.ts/RandomEvents.ts shape
// (ring-based flee candidates farthest-from-threat first, a reachability filter, a bounded
// walkTo-with-timeout) plus RunManager.ts's run-under-attack rule, translated into this
// codebase's own primitives (pathfind.Pack, actions.Walk, actions.SetRun) rather than
// reinventing pathfinding or candidate selection.
//
// Now wired into fleeFromCombat's own actions.CheatEnabled gate (handle.go) -- the path
// every no-cheats run takes, including guardian.go's kick() and EmergencyFlee, both of
// which only ever call fleeFromCombat, never this function directly. HandleEvade (the
// specific-tracked-hostile "event monster" handler, above in handle.go) now
// uses this same monitored walker with the bounded expanded local planner.

// The walking flee's clocks. Variables only so a test can shorten them; nothing else assigns them.
var (
	// fleeWalkPollInterval is how often fleeFromCombatWalking re-samples InCombat()/Tile()
	// while a walk is nominally in flight. Matches the chunk size sleepWatchingCombat
	// already uses elsewhere in this package for the same reason: fine enough to shrink the
	// reaction window to a few hundred ms, not so fine it spins the CPU or the connection.
	fleeWalkPollInterval = 250 * time.Millisecond

	// fleeWalkLegInterval is how long one flee leg is watched before its progress is judged
	// (see fleeProgressTiles). Five live 600ms ticks: a new walk's first step lands one to
	// two ticks after it is sent, and the old 2s leg (three ticks) judged many walks before
	// they had started. A chasing NPC is still watched every fleeWalkPollInterval.
	fleeWalkLegInterval = 3 * time.Second

	// fleeWalkDeadline bounds the whole escape attempt, the same shape as
	// strangePlantSolveDeadline/genieSolveDeadline elsewhere in this package.
	fleeWalkDeadline = 20 * time.Second
)

const (
	// fleeHoldGroundEvery spaces a cornered account's eat-or-fight-back actions: about three
	// game ticks, one eat's delay.
	fleeHoldGroundEvery = 1800 * time.Millisecond

	// fleeWalkArrivalTolerance is how close (Chebyshev) counts as "arrived" at a flee
	// destination -- generous on purpose, since the goal is separation from the threat,
	// not landing on an exact tile.
	fleeWalkArrivalTolerance = 2

	// fleeProgressTiles is the ground a leg must cover for its destination to be kept: at
	// least a tick of walking, well under a leg of running.
	fleeProgressTiles = 2

	// badDestRadius is how close a later flee route may pass to a destination a leg made no
	// progress toward (see withoutBadDests).
	badDestRadius = 3
)

// wantRunForFlee decides whether fleeFromCombatWalking should turn running on before
// issuing (or re-issuing) a flee walk. Ports RunManager.ts's own shouldEnableRun for this
// one specific case: under attack, any energy at all is worth spending to break off (that
// file's own ENERGY_MIN_ATTACKED=1), never gated behind the normal cruising energy floor.
// Pure and unit-testable on its own, unlike the rest of this file, which needs a live
// actions.Session/pathfind.Pack -- see handle_test.go's own doc comment for why this
// package doesn't fake those in tests.
func wantRunForFlee(runOn bool, energy int, energyOK bool) bool {
	if runOn {
		return false
	}
	return energyOK && energy >= 1
}

// reachedFleeDest reports whether (x,z,level) counts as having arrived at dest, within
// fleeWalkArrivalTolerance tiles on the same level. Pure and unit-testable.
func reachedFleeDest(x, z, level int, dest pathfind.NavPoint) bool {
	return level == dest.Level && chebyshev(x, z, dest.X, dest.Z) <= fleeWalkArrivalTolerance
}

// fleeFromCombatWalking is the walking/running counterpart to fleeFromCombat -- see this
// file's own package doc comment above for the full design and why a naive single walkTo
// isn't enough. dist is the starting flee ring distance (callers typically pass
// baseFleeDist, or fleeDistFor(attempt) if they already track an escalating attempt count
// of their own); this function escalates it itself when a destination is reached with the
// threat still following, or when a leg makes no progress.
//
// Returns nil once either real separation is confirmed (stillThreatened false: no recent hit,
// no event monster threatening the account, no recorded attacker still pursuing it) or
// fleeWalkDeadline has passed -- like fleeFromCombat, this reports success by return
// value only via error (a real send/network failure), not by whether the account ended
// up safe; callers that need to know should re-check s.State().InCombat() themselves
// afterward, the same pattern HandleEvade already uses against its own tracked NPC index.
//
// Rebuilt 2026-09-20 after three live deaths on c36f809 (spare102 and spare 21 in the
// Fishing Guild, herring at Catherby), all of the same shape: nine or ten 2s legs, every one
// "still in combat after this leg — redirecting", the account never getting away. Reproduced
// in the lab only once the engine ran at the live 600ms tick (cmd/labfleettest -mode
// guild-flee -flee-tick 600; the shared lab engine runs 200ms ticks, where a 2s leg is ten
// ticks and the old flee escaped every time). At 600ms a 2s leg is just over three ticks and
// the first step of a new walk lands one to two ticks after it is sent, so the chaser is
// still in hit range when every leg ends. The old loop then:
//
//   - threw the destination away and planned a fresh ring around the account's CURRENT tile
//     with no threat position, so consecutive plans pointed in opposite directions (east,
//     north, east, north: the lab trace ran back and forth over four tiles for twenty
//     seconds beside its Swarm), and
//   - sent each new route from the tile the client last saw. The engine queues every
//     waypoint of a MOVE_GAMECLICK including the first (PathingEntity.queueWaypoints), so a
//     route that starts a tile or two behind the account walks it back there first.
//
// A single walk away from a fresh Swarm, sent once and left alone, escaped cleanly at 600ms
// in every lab probe, with auto retaliate on or off. So now:
//
//   - The flee runs away from an anchor, not from itself: the attacker's tile when the flee
//     starts (currentAttacker), else the account's own. An event monster is leashed to where
//     it spawned (Engine-TS Npc.targetWithinMaxRange: macro_swarm maxrange 15, river trolls 5)
//     and that is next to the account when the fight starts, so separation from the anchor is
//     what ends the chase.
//   - A walk is sent only when the destination changes, and without its first waypoint when
//     the account may have moved on from it (walkFleeLeg). While a leg gains ground the same
//     destination is kept and nothing is re-sent. (fe625f3 reverted an earlier
//     hold-the-destination attempt, 50ab9f4, which held it without an anchor and so never
//     got farther from the monster's leash point; escalation on arrival is kept here.)
//   - A destination is replaced when it is reached with the threat still following (farther,
//     dist escalating as before), or when a leg covers fewer than fleeProgressTiles: that
//     destination is marked bad and routes near it are refused, so the next plan tries a
//     different way out.
//   - Planning uses fleePack: the static collision pack with this session's observed door
//     and wall changes applied, so a closed door is not routed through.
//   - Every heading and redirect line names the attacker and where the account actually is.
//
// Auto retaliate is deliberately left alone: its IF_BUTTON runs p_stopaction (content
// player_controls.rs2), which cancelled the flee walk sent with it in the lab.
//
// Planning is bounded and walked as planned (15 Oct 2026, after a 30 s profile of the live fleet put
// 45% of its CPU here; the chain is in flee_plan.go):
//
//   - planFleeLeg floods the reachable ground once (fleePlanner) instead of searching each ring
//     candidate, so a plan costs a flood and a few searches however many candidates are cut off;
//   - the route the plan found is the route walked (walkFleeRoute), not a second search under
//     different rules whose failure sent the loop round again with no pause and no log;
//   - a plan that finds nowhere to go is remembered for fleeNoEscapeMemory for the same tile and
//     threat (noteNoEscape): the account holds its ground (eats) and the guardian's next kick does
//     not repeat the flood;
//   - an event monster with a leash is left by walking out of it (event_leash.go), not by the ring search, except
//     inside White Wolf Mountain's wolf territory, where the leash does not look at where it ends and
//     the least-exposed ring flee applies;
//   - the ring flee itself never walks nearer a visible wolf, Ice giant or Ice warrior than the account
//     stands (visibleWolfAvoidance), on the mountain and in the Asgarnian ice dungeon alike. Where that
//     leaves nowhere to go the account holds its ground (the cornered path), which is the right answer to
//     a threat that cannot be left by distance and does little harm (the Drunken Dwarf) and the wrong
//     one to walk through eleven aggressive warriors for.
func fleeFromCombatWalking(s actions.Session, pack *pathfind.Pack, dist int, log func(string)) error {
	_, err := fleeWalking(s, pack, dist, log)
	return err
}

// fleeWalking is fleeFromCombatWalking, and also says whether the account was STUCK: it was still threatened
// when the deadline passed and had not left its starting tile (fleeProgressTiles), whether because every plan
// found nowhere to go or because the walks it was sent on never moved it.
// HandleEvade needs the difference. It validated an escape with its own planner before calling the
// walker, so a walker that then found none was the two disagreeing, and the account stood where it was for
// the whole 20 s and then again, minute after minute, with the guardian holding the trainer (live 16-17 Oct
// 2026: three deaths in the Asgarnian ice dungeon, each 1.5 to 10 minutes of "no reachable flee candidate"
// from one tile). A stuck flee is handed to the cornered path (cornered.go) instead.
func fleeWalking(s actions.Session, pack *pathfind.Pack, dist int, log func(string)) (stuck bool, err error) {
	return fleeWalkingFrom(s, pack, dist, 1, log)
}

// fleeWalkingFrom is fleeWalking that starts at the guardian's attempt number for the event, not at the first:
// the attempt sets how far past an event monster's leash a walk aims (leashBreakFor adds four tiles an attempt).
// HandleEvade's own plan and its wait both say "the next attempt walks farther", and without this the walker
// planned every attempt as the first, so a leash that a walk of the first length had not shaken was walked again
// at the same length from where the account stood.
func fleeWalkingFrom(s actions.Session, pack *pathfind.Pack, dist, attempt0 int, log func(string)) (stuck bool, err error) {
	deadline := time.Now().Add(fleeWalkDeadline)
	attempt := max(1, attempt0)
	x0, z0, level0, ok := s.State().Tile()
	if !ok {
		return false, nil
	}
	anchorX, anchorZ := x0, z0
	if att, found := currentAttacker(s.State()); found {
		anchorX, anchorZ = att.X, att.Z
	}

	var dest pathfind.NavPoint
	haveDest := false
	var bad []pathfind.NavPoint
	triedDoor, triedExit := false, false
	var lastHold time.Time
	// stuckLegs counts the judged legs in a row that moved the account nowhere; lagHeld is the time spent waiting for a lagging view
	// (fleelag.go).
	stuckLegs := 0
	var lagHeld time.Duration
	for time.Now().Before(deadline) {
		x, z, level, ok := s.State().Tile()
		if !ok {
			return false, nil
		}
		if fleeRelocated(x0, z0, level0, x, z, level) {
			// Died (and respawned) or teleported: the threat this flee was planned against is no
			// longer anywhere near, and fleeing "away" from it would only walk off from the
			// respawn point. Live 2026-09-25 on a local Engine-TS 289: a combat-3 account died to a
			// Shadow spider in the Waterfall dungeon and kept fleeing from 2566,9878 at Lumbridge.
			// Deliberately not State.IsDead: that stays set until a script's death recovery clears
			// it, so it would also stop a genuine flee at the respawn point.
			if log != nil {
				log(fmt.Sprintf("combat flee: now at %d,%d,%d, far from where it began (%d,%d,%d) — the threat is left behind, stopping", x, z, level, x0, z0, level0))
			}
			return false, nil
		}
		// Verifying separation, not just that InCombat() lapsed — see
		// HostileEventNpcNearby's and pursuingAttacker's doc comments for the live
		// re-engagement patterns this closes.
		if !walkerThreatened(s, x, z) {
			return false, nil
		}

		origin := pathfind.NavPoint{X: x, Z: z, Level: level}
		if !haveDest {
			if noEscapeRemembered(s, x, z, level, anchorX, anchorZ) {
				// This tile against this threat already found nowhere to go a moment ago, and neither
				// has changed: do not flood the map again, only hold the ground (eat, fight back
				// where that is allowed) until the memory lapses or the threat is gone.
				if s.State().InCombat() && time.Since(lastHold) >= fleeHoldGroundEvery {
					lastHold = time.Now()
					fleeCornered(s, log)
				}
				time.Sleep(fleeWalkPollInterval)
				continue
			}
			live := fleePack(s, pack)
			avoid := visibleWolfAvoidance(s, origin)
			started := time.Now()
			plan, how, found, stats := planFleeLeg(s, live, origin, anchorX, anchorZ, dist, attempt, avoid, withoutBadDests(avoid, bad, origin), bad)
			if !found {
				// Nowhere at the preferred distance and no shorter escape on this side: (once) back
				// out through the door just crossed (flee_door.go). The shorter escape comes first so
				// a door is only used when the room truly holds nothing.
				if !triedDoor {
					triedDoor = true
					if fleeDoorBack(s, anchorX, anchorZ, log) {
						continue
					}
				}
				// Or up or down the staircase or ladder the walker knows (levelexit.go): an NPC drops its target
				// when the target changes level, so a room with a stair in it is not a dead end whatever stands in
				// the door. Once, like the door.
				if !triedExit {
					triedExit = true
					if fleeLevelExit(s, live, origin, anchorX, anchorZ, log) {
						return false, nil
					}
				}
			}
			if !found && s.State().InCombat() && time.Since(lastHold) >= fleeHoldGroundEvery {
				// Truly cornered and being hit: leave by a teleport when hurt enough to need one, else eat
				// or fight back rather than stand still.
				lastHold = time.Now()
				fleeCornered(s, log)
			}
			if !found {
				noteNoEscape(s, x, z, level, anchorX, anchorZ)
				if log != nil {
					log(fmt.Sprintf("combat flee (walking): no reachable flee candidate found from %d,%d,%d (%s; %s; planned in %s, %s) — not planning again for %s",
						x, z, level, attackerDiag(s.State()), how, time.Since(started).Round(time.Millisecond), stats, fleeNoEscapeMemory))
				}
				// A plain sleep, not sleepWatchingCombat: this loop runs while already in
				// combat, so that would return at once and busy-loop (live 2026-09-18,
				// crafter1: 140 identical lines in one second while holding actionMu).
				time.Sleep(fleeWalkPollInterval)
				attempt++
				dist = fleeDistFor(attempt)
				continue
			}
			forgetNoEscape(s)
			dest = plan.Dest
			sent, err := walkFleeRoute(s, plan.Route)
			if err != nil {
				return false, err
			}
			if !sent {
				// Nothing to walk: mark the destination bad and let the leg pause below, not spin.
				bad = append(bad, dest)
				time.Sleep(fleeWalkPollInterval)
				continue
			}
			haveDest = true
			if log != nil {
				log(fmt.Sprintf("combat flee (walking): heading to %d,%d,%d from %d,%d (attempt %d, dist %d, away from %d,%d; %s; %s, planned in %s, %s)",
					dest.X, dest.Z, dest.Level, x, z, attempt, dist, anchorX, anchorZ, attackerDiag(s.State()), how, time.Since(started).Round(time.Millisecond), stats))
			}
		}

		// Actively monitor this leg instead of blindly sleeping out a travel estimate. A leg is judged in the server's time, not only the
		// wall's, and not at all while the view is silent or catching up (fleelag.go).
		st := s.State()
		clock := newLegClock(st, time.Now())
		arrived, judged := false, false
		cx, cz := x, z
		for time.Now().Before(deadline) {
			time.Sleep(fleeWalkPollInterval)
			lagging := st.ViewLagging()
			if lagging {
				lagHeld += fleeWalkPollInterval
				if lagHeld <= fleeLagHoldMax {
					deadline = deadline.Add(fleeWalkPollInterval)
				}
			}
			if time.Since(lastHold) >= fleeHoldGroundEvery && fleeEatIfLow(s, log) {
				lastHold = time.Now()
			}
			nx, nz, nl, ok := st.Tile()
			if !ok {
				return false, nil
			}
			if !walkerThreatened(s, nx, nz) {
				return false, nil
			}
			cx, cz = nx, nz
			if reachedFleeDest(nx, nz, nl, dest) {
				arrived = true
				break
			}
			if lagging {
				// The tile the view shows may be behind the player: this leg is not over until the view is current, and
				// its clock starts again when it is.
				clock = newLegClock(st, time.Now())
				continue
			}
			if clock.judgeable(st, time.Now(), fleeWalkLegInterval) {
				judged = true
				break
			}
		}
		moved := chebyshev(cx, cz, x, z)
		switch {
		case arrived:
			if log != nil {
				log(fmt.Sprintf("combat flee (walking): reached %d,%d but still threatened (%s) — going farther", dest.X, dest.Z, attackerDiag(s.State())))
			}
			stuckLegs = 0
			haveDest = false
			attempt++
			dist = fleeDistFor(attempt)
		case judged && moved < fleeProgressTiles:
			stuckLegs++
			if log != nil {
				// The modals and the held delay are the two things that make the engine sit on a walk click without a word (a walk is
				// not read while a modal is open and the queue is not empty; a delayed player's clicks are dropped): the line says
				// which, so the next stall is classifiable (fleelag.go).
				main, chat, side := st.Modals()
				log(fmt.Sprintf("combat flee (walking): moved %d tile(s) toward %d,%d this leg (now %d,%d; %s) — marking it bad and redirecting (%d leg(s) in a row, %d game ticks, modals %d/%d/%d, held %s)",
					moved, dest.X, dest.Z, cx, cz, attackerDiag(s.State()), stuckLegs, st.Tick()-clock.tick, main, chat, side, actions.DelayLeft(s, time.Now()).Round(100*time.Millisecond)))
			}
			bad = append(bad, dest)
			haveDest = false
			attempt++
			dist = fleeDistFor(attempt)
			// Nothing it sends is moving the account: leave by teleport, when what hits it is more than food answers.
			if stuckLegs >= fleeStuckLegs && fleeStuckDanger(s) && fleeStuckEscape(s, log) {
				lastHold = time.Now()
			}
		case judged:
			stuckLegs = 0 // gaining ground: keep the destination, send nothing.
		}
	}

	if x, z, _, ok := s.State().Tile(); ok && walkerThreatened(s, x, z) {
		stuck = chebyshev(x, z, x0, z0) < fleeProgressTiles
		if log != nil {
			log(fmt.Sprintf("combat flee (walking): gave up after the deadline at %d,%d, still threatened (%s)", x, z, attackerDiag(s.State())))
		}
	}
	return stuck, nil
}

// walkFleeRoute sends the planned route, with running on if there is any energy for it. sent is
// false when there is no route to send.
//
// The route's first waypoint (origin, the tile the client last saw) is dropped when there
// is anything after it: the engine walks every queued waypoint in order, so an account that
// has already stepped past origin would otherwise be walked back to it before fleeing on.
//
// The route is the one the plan found, not a second search: this used to route again from origin
// to the planned destination under the visible-wolf predicate, which the plan's last resort
// ignores, so a destination the plan could reach and the walker could not was planned again at
// once, with no pause and no log, until the 20 s deadline (live 15 Oct 2026, the ice dungeon).
func walkFleeRoute(s actions.Session, route []pathfind.NavPoint) (sent bool, err error) {
	if len(route) == 0 {
		return false, nil
	}
	if energy, energyOK := s.State().Energy(); wantRunForFlee(s.State().RunEnabled(), energy, energyOK) {
		_ = actions.SetRun(s, true)
	}
	return true, actions.Walk(s, fleeWaypoints(route), true)
}

// planFleeLeg plans one leg of the walking flee from origin, away from the anchor, and says how:
//
//   - An event monster with a leash (event_leash.go: the Swarm, the Strange plant, the Shade, the Zombie,
//     Rock Golem, Watchman, River troll and Tree spirit) that has the account as its target is left by
//     walking out of its leash, whatever the ring search would choose, unless the account is in wolf
//     territory (the leash declines there).
//   - Otherwise the ring search at dist, then each shorter rung (fleePlanner), planned under
//     planAvoid (the visible wolves, Ice giants and Ice warriors, and the tiles near destinations earlier
//     legs failed to reach). The plan's last resort ignores that predicate, so its route is held to
//     wolfAvoid, the visible wolves alone, as the walker always held it: a route that passes closer to a
//     visible wolf, Ice warrior or giant than the account already stands is searched again under it, and
//     refused if there is none. That is also what keeps a threat that has no leash (the Drunken Dwarf, or
//     any ordinary attacker) from being fled through the Ice warrior cave's herd: it finds nowhere to go
//     and the account holds its ground (TestUnleashedThreatInTheIceCaveIsNeverFledThroughTheHerd).
//
// stats is the cost of the plan, for the log.
func planFleeLeg(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, anchorX, anchorZ, dist, attempt int, wolfAvoid, planAvoid func(int, int, int) bool, bad []pathfind.NavPoint) (plan escapePlan, how string, found bool, stats string) {
	if monster, ok := threateningLeashed(s.State(), origin.X, origin.Z); ok {
		if res, ok := leashFor(s, live, origin, monster, attempt, withoutBadDests(nil, bad, origin)); ok {
			return res.escapePlan, describeLeash(s, res), true, fmt.Sprintf("%d reachable tiles, 1 route search", res.Tiles)
		}
	}
	pl := newFleePlanner(live, origin, anchorX, anchorZ, planAvoid, safeRefFor(s))
	plan, ok := pl.escape(dist)
	how = fmt.Sprintf("ring %d", dist)
	if !ok {
		var rung int
		if plan, rung, ok = pl.shorter(dist); ok {
			how = fmt.Sprintf("shorter ring %d", rung)
		} else {
			how = fmt.Sprintf("no landing reachable at %d tiles or any shorter ring", dist)
		}
	}
	if ok {
		if plan, ok = pl.heldTo(plan, wolfAvoid); !ok {
			how = "the only landing needs a route closer to a visible wolf, Ice warrior or giant than the account stands"
		}
	}
	return plan, how, ok, pl.Stats()
}

// fleeWaypoints is a route without its starting tile, unless that is all there is.
func fleeWaypoints(route []pathfind.NavPoint) []pathfind.NavPoint {
	if len(route) > 1 {
		return route[1:]
	}
	return route
}

// withoutBadDests extends avoid so no flee route passes within badDestRadius of a destination
// a previous leg made no progress toward — "that way does not work", not only "that tile".
// Tiles that close to the account itself stay allowed, so its own start is never excluded.
func withoutBadDests(avoid func(int, int, int) bool, bad []pathfind.NavPoint, origin pathfind.NavPoint) func(int, int, int) bool {
	if len(bad) == 0 {
		return avoid
	}
	return func(x, z, level int) bool {
		if avoid != nil && avoid(x, z, level) {
			return true
		}
		if chebyshev(x, z, origin.X, origin.Z) <= badDestRadius {
			return false
		}
		for _, b := range bad {
			if level == b.Level && chebyshev(x, z, b.X, b.Z) <= badDestRadius {
				return true
			}
		}
		return false
	}
}

// Avoid routing past visible White wolves, not just landing inside their known area.
// An account already close to a wolf may move outward; blocking its origin would
// otherwise prevent the very escape this predicate is intended to permit.
//
// KEYED ON THE NPC TYPE, WHEREVER IT STANDS, and that is deliberate. Types 111 and 125 (the Ice giant and Ice
// warrior, added because the mountain's summit has both) also fill the Asgarnian ice dungeon, where an
// eleven-warrior herd puts a refusal radius round every tile the account could step to: the flood from a
// stand there is one tile. That boxed in nine ScarabSwarm accounts between 15 and 17 Oct 2026, each with a
// random event monster at its side and no step taken, and it was tempting to confine the rule to the
// mountain. It is not confined, because a flee that ignores the herd walks through it: the Ice warrior rune
// pen is a safespot among the warriors, and without this rule a threat with no leash (the Drunken Dwarf,
// strength 1) sent the account on a 24-step route with 23 tiles inside five of a warrior's spawn
// (reproduced on the real pack, TestUnleashedThreatInTheIceCaveIsNeverFledThroughTheHerd). What boxed the
// accounts in was fleeing a Shade, a Strange plant or a Zombie with the ring search at all: each of them is
// left by walking out of its leash (event_leash.go), which has its own, graded margin from the herd and is
// tried first (planFleeLeg), so the herd rule now only ever meets a threat that distance cannot shake.
func visibleWolfAvoidance(s actions.Session, from pathfind.NavPoint) func(int, int, int) bool {
	type exclusion struct{ x, z, r int }
	var wolves []exclusion
	for _, npc := range s.State().NPCs() {
		if mountainHostileTypes[npc.Type] {
			r := min(12, max(1, chebyshev(from.X, from.Z, npc.X, npc.Z)))
			wolves = append(wolves, exclusion{npc.X, npc.Z, r})
		}
	}
	if len(wolves) == 0 {
		return nil
	}
	return func(x, z, level int) bool {
		if level != from.Level {
			return false
		}
		for _, w := range wolves {
			if chebyshev(x, z, w.x, w.z) < w.r {
				return true
			}
		}
		return false
	}
}

// routeSteps is the tile count of a corner-compressed route from origin (diagonal steps
// count one, as the engine moves).
func routeSteps(origin pathfind.NavPoint, corners []pathfind.NavPoint) int {
	steps, prev := 0, origin
	for _, c := range corners {
		steps += chebyshev(prev.X, prev.Z, c.X, c.Z)
		prev = c
	}
	return steps
}

// maxFleeDetour bounds how far a flee may walk to land straight-line distance away. A
// candidate beyond it sits on the far side of a barrier, and the route there goes around
// through whatever lies in between. Live 2026-09-19 and reproduced in the lab: from the
// Taverley foot of White Wolf Mountain (2875,3428) the Catherby beach 20 tiles west
// (2855,3428) is a 179-step route over the whole wolf-held pass.
func maxFleeDetour(straight int) int {
	return 2*straight + 10
}

// fleeRelocatedTiles is how far from its start a flee can be before it is no longer the same
// flee. A flee lasts at most fleeWalkDeadline (20s, ~33 ticks) and a running account covers two
// tiles a tick, so fleeing alone cannot get 66 tiles away; beyond 80 only a respawn or a teleport
// has moved the account.
const fleeRelocatedTiles = 80

// fleeRelocated reports whether the account has left the flee's starting point by a respawn or a
// teleport rather than by fleeing: a different level, or more than fleeRelocatedTiles away.
func fleeRelocated(x0, z0, level0, x, z, level int) bool {
	return level != level0 || chebyshev(x0, z0, x, z) > fleeRelocatedTiles
}
