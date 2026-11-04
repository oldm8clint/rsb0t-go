package randomevents

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// What an account does about an event monster: it leaves the monster's leash.
//
// THE FACTS (content 289 and Engine-TS 289, both read, not guessed):
//
//   - Every hostile random event monster is set to opplayer2 on its owner when it spawns (content
//     scripts/macro events/scripts/*/macro_event_*.rs2: swarm, river troll, rock golem, shade, zombie,
//     watchman, tree spirit, and the Strange plant when its second 45-tick timer fires) and nothing else
//     ever gives it a target again: none of them has a hunt mode. The fight lasts until the monster loses
//     its target.
//   - It loses its target in exactly two ways (Npc.validateTarget): the target is on another level, or the
//     target is more than maxrange+1 tiles (Chebyshev, the corner excepted) from the tile the monster was
//     ADDED at (startX/startZ, not where it has since walked). It then drops to its default mode, wander,
//     and its next ai_timer deletes it once it is in wander mode and more than 3 tiles from the player or on
//     another level (macro_event_lost_hostile, macro_events.rs2). Teleporting away does the same.
//   - maxrange is per monster, from scripts/macro events/configs/antimacro.npc (pinned by
//     TestEventLeashTableMatchesContent): the Swarm 15, the Strange plant 3, the Shade 3, the Zombie,
//     Rock Golem, Watchman, River troll and Tree spirit 5. The Drunken Dwarf's is 10000, so it cannot be
//     left and is not in the table.
//   - The Swarm hits rarely and weakly (strength 10, attackrate 7) but never stops, and has more
//     hitpoints than anything an account kills in a useful time. The others hit hard: the Shade takes its
//     level from the owner's combat level (level 120, hitpoints 120 for combat 61 to 90), the Strange plant
//     has defence 99 and 200 hitpoints and poisons for severity 10 on every hit. None of them is made
//     harmless by fighting, and every one of them is made harmless by distance.
//   - Live proof for the Swarm: an account at the Asgarnian ice dungeon's rune pen finally walked from
//     3041,9588 to 3022,9588 (18 tiles from the Swarm's tile) on 15 Oct 2026, and "random event: event
//     monster — cleared" was logged six seconds later.
//
// So the answer to an event monster that cannot be shaken is not to fight it, not to stand and train beside
// it (it cannot be left to chew through the account's food while the guardian holds the action lock), and not
// to run the generic ring flee, whose destination can be anywhere 20 tiles off, such as through the Ice
// warriors a cave pen is a safespot FROM. It is the shortest walk to a tile more than the leash from where
// the monster began, along the reachable ground, away from anything else that would attack the account on the
// way. On the real map that is a handful of steps from the Ice warriors' cave stand for a plant or a Shade
// (event_leash_realpack_test.go), where the ring flee found nothing.
//
// ONE PLACE IT DOES NOT APPLY: White Wolf Mountain. The shortest way out of a leash is the wrong question
// there, because it does not care where it stops. On the real map, from 54 origins inside the mountain's wolf
// territory (a Swarm beside the account, nothing else in view), 32 leash walks ended inside the territory,
// with the account standing in the wolves' reach, and 10 also spent more tiles in it on the way than the
// ordinary flee (2857,3486: a walk to 2827,3521 with 51 tiles of exposure against the ordinary flee's 32 to
// 2909,3434). The ordinary flee for an account in territory is fleePlanner.leastExposed: it refuses a
// landing in territory and ranks the routes by the tiles they spend in it, which is what the wolf code was
// written for after the live deaths recorded in wolf_territory.go. So an event monster that finds the account
// in wolf territory is fled like any other (leashPlan declines), and once the account is out the monster is
// left by the ordinary flee's own escalation or, if it is still there, by the leash from outside.

const (
	// swarmNpcType is macro_swarm's npc id (hostileEventNpcIDs' 411).
	swarmNpcType = 411

	// angryPlantType is macro_triffidseed_angry, the Strange plant after it turns on its owner. The
	// passive seed (plantSeedType) attacks nobody and has no place in the leash table.
	angryPlantType = 408

	// plantSeedType is macro_triffidseed, the Strange plant until its second timer fires and
	// npc_changetype_keepall makes it the angry one: the same NPC index on the same tile, whose spawn tile
	// is where the leash is measured from.
	plantSeedType = 407

	// swarmLeash is macro_swarm's maxrange: the Swarm follows its target while that target is within
	// swarmLeash+1 tiles of where it was added.
	swarmLeash = 15

	// leashBreakMargin is how many tiles past maxrange a walk aims from the monster's spawn tile: the leash
	// is lost beyond maxrange+1, so maxrange+2 is the first tile that works and one more leaves a tile for
	// the account stopping short (the live 3022,9588 that cleared a Swarm was swarmLeash+3 away).
	leashBreakMargin = 3

	// leashSpawnMemory is how long a remembered spawn tile is trusted for a monster index: an index is
	// reused by the next NPC the engine adds.
	leashSpawnMemory = 15 * time.Minute
)

// eventLeashes maps an event monster's npc type to its maxrange.
var eventLeashes = buildEventLeashes()

func buildEventLeashes() map[int]int {
	m := map[int]int{swarmNpcType: swarmLeash, angryPlantType: 3}
	span := func(lo, hi, maxrange int) {
		for t := lo; t <= hi; t++ {
			m[t] = maxrange
		}
	}
	span(391, 396, 5) // River troll
	span(413, 418, 5) // Rock Golem
	span(419, 424, 5) // Zombie
	span(425, 430, 3) // Shade
	span(431, 436, 5) // Watchman
	span(438, 443, 5) // Tree spirit
	return m
}

// eventLeashOf is the leash of an event monster type: how far from its spawn tile it follows its target.
func eventLeashOf(npcType int) (maxrange int, ok bool) {
	maxrange, ok = eventLeashes[npcType]
	return
}

// isLeashedEvent reports whether an NPC type is an event monster that loses its target at a known distance
// from where it began, so that leaving that distance is the answer to it.
func isLeashedEvent(npcType int) bool {
	_, ok := eventLeashes[npcType]
	return ok
}

// leashBreakFor is the distance from the monster's spawn the attempt'th walk aims for: leashBreakMargin past
// its maxrange, and four more each time the last one arrived and the monster was still there (a spawn
// remembered late).
func leashBreakFor(maxrange, attempt int) int {
	if attempt < 1 {
		attempt = 1
	}
	return min(maxrange+leashBreakMargin+4*(attempt-1), max(maxFleeDist, maxrange+leashBreakMargin))
}

// leashExposureLayers are the margins, in tiles, the walk keeps from other things that attack, tried widest
// first. A layer that leaves no way out is dropped for the next; below the last the walk is not offered
// (the generic flee then applies, as for any other event monster).
var leashExposureLayers = []int{8, 5, 3}

// leashSpawn is where an event monster was first seen, which is where the engine added it.
type leashSpawn struct {
	index, npcType int
	x, z, level    int
	at             time.Time
}

// leashSpawns maps actions.Session -> leashSpawn, recentCombat's per-session shape.
var leashSpawns sync.Map

// noteLeashSpawn returns the tile the monster npcIndex was added at: the one remembered for it, or, the
// first time it is seen, x,z where it stands now. An event monster is added one tile from its owner and
// starts chasing within three ticks, and the guardian sees it within a poll of that, so where it is first
// seen is where it began (startX/startZ in the engine).
func noteLeashSpawn(s actions.Session, npcIndex, npcType, x, z, level int) (int, int) {
	if v, ok := leashSpawns.Load(s); ok {
		sp := v.(leashSpawn)
		if sp.index == npcIndex && sameLeashNPC(sp.npcType, npcType) && sp.level == level && time.Since(sp.at) < leashSpawnMemory {
			return sp.x, sp.z
		}
	}
	leashSpawns.Store(s, leashSpawn{index: npcIndex, npcType: npcType, x: x, z: z, level: level, at: time.Now()})
	return x, z
}

// sameLeashNPC reports whether two npc types are one event monster: equal, or the Strange plant before and
// after it turns hostile (changetype keeps its index, tile and start tile).
func sameLeashNPC(a, b int) bool {
	return a == b || (a == plantSeedType && b == angryPlantType) || (a == angryPlantType && b == plantSeedType)
}

// threateningLeashed is the leashed event monster, if any, that has this account as its target (or has just
// appeared beside it): the nearest one threatensAccount accepts within hostileEngageDist of x,z.
func threateningLeashed(state *gamestate.State, x, z int) (gamestate.NPC, bool) {
	var best gamestate.NPC
	bestDist, found := 0, false
	for _, npc := range state.NPCs() {
		if !isLeashedEvent(npc.Type) {
			continue
		}
		d := chebyshev(npc.X, npc.Z, x, z)
		if d > hostileEngageDist || !threatensAccount(npc, state, x, z) {
			continue
		}
		if !found || d < bestDist {
			best, bestDist, found = npc, d, true
		}
	}
	return best, found
}

// hazardSite is the tile of something that attacks, other than the monster being left, that the walk keeps
// off.
type hazardSite struct{ x, z int }

// otherAttackers lists the visible NPCs near the account that can attack and are not an event monster: the
// Ice warriors beside a rune pen, a goblin camp, the dungeon's spiders. The event monsters are excluded
// because each hunts only its own owner (the monster being left is the threat; a river troll or golem beside
// the account is somebody else's).
func otherAttackers(state *gamestate.State, origin pathfind.NavPoint) []hazardSite {
	var out []hazardSite
	for _, npc := range state.NPCs() {
		if npc.Level != origin.Level || hostileEventNpcIDs[npc.Type] || chebyshev(npc.X, npc.Z, origin.X, origin.Z) > 2*hostileEngageDist {
			continue
		}
		if !canAttack(npc) {
			continue
		}
		out = append(out, hazardSite{npc.X, npc.Z})
	}
	return out
}

// leashAvoid is the ground the walk may not use: outside the flee window, wolf territory (the plan is not
// made for an account already in it, so on the walk's ground it is always refused), and anywhere nearer to
// another attacker than margin tiles, never more than the account is already (so a safespot between hostiles
// can be left the way it was entered). margin 0 applies no hazard at all. extra adds tiles the caller
// refuses (the destinations an earlier leg failed to reach).
func leashAvoid(origin pathfind.NavPoint, others []hazardSite, margin int, extra func(int, int, int) bool) func(x, z, level int) bool {
	type exclusion struct{ x, z, r int }
	var ex []exclusion
	if margin > 0 {
		for _, o := range others {
			ex = append(ex, exclusion{o.x, o.z, min(margin, max(1, chebyshev(origin.X, origin.Z, o.x, o.z)))})
		}
	}
	outsideWolves := !inWolfTerritory(origin)
	return func(x, z, level int) bool {
		if level != origin.Level || chebyshev(x, z, origin.X, origin.Z) > fleeWindowRadius {
			return true
		}
		if outsideWolves && inWolfTerritory(pathfind.NavPoint{X: x, Z: z, Level: level}) {
			return true
		}
		for _, e := range ex {
			if chebyshev(x, z, e.x, e.z) < e.r {
				return true
			}
		}
		return extra != nil && extra(x, z, level)
	}
}

// leashTile picks where the walk ends: of the tiles the account can reach, the nearest by steps that is at
// least breakDist from the monster's spawn and clear of the monster where it is now, outside the dangerous
// ground. Ties go to the tile farther from the spawn, then to the lower x and z, so the choice does not
// depend on map order. The account's own tile is never chosen (a walk of no steps leaves the monster exactly
// where it was).
func leashTile(r *pathfind.Reach, origin pathfind.NavPoint, spawnX, spawnZ, monsterX, monsterZ, breakDist int) (pathfind.NavPoint, bool) {
	var best pathfind.NavPoint
	bestSteps, bestFar, found := 0, 0, false
	r.Each(func(x, z, level, steps int) bool {
		if steps == 0 || level != origin.Level {
			return true
		}
		far := chebyshev(x, z, spawnX, spawnZ)
		if far < breakDist || chebyshev(x, z, monsterX, monsterZ) < min(breakDist, 15) {
			return true
		}
		p := pathfind.NavPoint{X: x, Z: z, Level: level}
		if inDangerZone(p) {
			return true
		}
		better := !found || steps < bestSteps ||
			(steps == bestSteps && (far > bestFar || (far == bestFar && (x < best.X || (x == best.X && z < best.Z)))))
		if better {
			best, bestSteps, bestFar, found = p, steps, far, true
		}
		return true
	})
	return best, found
}

// leashResult is a leash walk and what it took to find.
type leashResult struct {
	escapePlan
	Margin int // the exposure margin that left a way out, 0 when no other attacker is in view
	Others int // other attackers in view
	Tiles  int // tiles the account can reach at that margin
}

// leashPlan is the shortest walk out of a monster's leash. spawnX,spawnZ is the tile it was added at and
// monsterX,monsterZ where it stands now; breakDist (leashBreakFor) is how far from the spawn to end. The
// widest exposure margin that leaves a way out is used. ok is false when no reachable tile is far enough at
// any margin: a sealed room, a pocket whose only way out passes the whole herd. It is also false for an
// account inside White Wolf Mountain's wolf territory, where the ordinary least-exposed flee applies instead
// (see the facts above).
func leashPlan(pack *pathfind.Pack, origin pathfind.NavPoint, spawnX, spawnZ, monsterX, monsterZ, breakDist int, others []hazardSite, extra func(int, int, int) bool) (res leashResult, ok bool) {
	if pack == nil {
		return leashResult{}, false
	}
	// In wolf territory the ordinary flee, which counts exposure, is the right one.
	if inWolfTerritory(origin) {
		return leashResult{}, false
	}
	// An account already past the leash by the recorded spawn is being followed anyway (the spawn was
	// recorded late): get clear of the monster where it is instead.
	if chebyshev(origin.X, origin.Z, spawnX, spawnZ) >= breakDist {
		spawnX, spawnZ = monsterX, monsterZ
	}
	layers := leashExposureLayers
	if len(others) == 0 {
		layers = []int{0}
	}
	for _, layer := range layers {
		avoid := leashAvoid(origin, others, layer, extra)
		reach := pack.ReachFrom(origin, fleeWindowRadius, avoid)
		dest, found := leashTile(reach, origin, spawnX, spawnZ, monsterX, monsterZ, breakDist)
		if !found {
			continue
		}
		route := pack.FindPathAvoiding(origin, dest, avoid)
		if !route.OK || len(route.Waypoints) == 0 {
			continue
		}
		return leashResult{
			escapePlan: escapePlan{Dest: route.Waypoints[len(route.Waypoints)-1], Route: route.Waypoints},
			Margin:     layer, Others: len(others), Tiles: reach.Size(),
		}, true
	}
	return leashResult{}, false
}

// leashFor plans the leash walk for the event monster npc on the session's live view: the spawn it was first
// seen at, the other attackers in view, and the distance for this attempt.
func leashFor(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, monster gamestate.NPC, attempt int, extra func(int, int, int) bool) (leashResult, bool) {
	maxrange, ok := eventLeashOf(monster.Type)
	if !ok {
		return leashResult{}, false
	}
	spawnX, spawnZ := noteLeashSpawn(s, monster.Index, monster.Type, monster.X, monster.Z, monster.Level)
	return leashPlan(live, origin, spawnX, spawnZ, monster.X, monster.Z, leashBreakFor(maxrange, attempt), otherAttackers(s.State(), origin), extra)
}

// evadeLeash is HandleEvade's probe: for an event that is a leashed monster, the leash walk from origin,
// planned on the monster as the state sees it (the event's own record of it when it has left view).
func evadeLeash(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, ev *Event, attempt int) (leashResult, bool) {
	if !isLeashedEvent(ev.NpcType) {
		return leashResult{}, false
	}
	monster := gamestate.NPC{Index: ev.NpcIndex, Type: ev.NpcType, X: ev.ThreatX, Z: ev.ThreatZ, Level: origin.Level}
	for _, npc := range s.State().NPCs() {
		if npc.Index == ev.NpcIndex && npc.Type == ev.NpcType {
			monster = npc
			break
		}
	}
	return leashFor(s, live, origin, monster, attempt, nil)
}

// describeLeash is the log fragment for a leash walk.
func describeLeash(s actions.Session, res leashResult) string {
	spawnX, spawnZ := -1, -1
	if v, ok := leashSpawns.Load(s); ok {
		sp := v.(leashSpawn)
		spawnX, spawnZ = sp.x, sp.z
	}
	clear := "no other attacker in view"
	if res.Margin > 0 {
		clear = fmt.Sprintf("%d tiles clear of the %d other attackers in view", res.Margin, res.Others)
	}
	return fmt.Sprintf("event leash: %d steps to %d,%d, %d from where it began (%d,%d), %s",
		routeSteps(res.Route[0], res.Route[1:]), res.Dest.X, res.Dest.Z, chebyshev(res.Dest.X, res.Dest.Z, spawnX, spawnZ), spawnX, spawnZ, clear)
}
