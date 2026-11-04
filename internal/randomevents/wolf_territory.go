package randomevents

import "rsb0t-go/internal/pathfind"

// wolfDen is one aggressive spawn on White Wolf Mountain and the Chebyshev radius around it
// a low-level account is in reach of it: its wander range plus its hunt range, both copied
// from content (maps m44_53/m44_54/m44_55, npc configs whitewolf_sentry, whitewolf,
// wolfpack_leader, pack_wolf, icegiant and icewarrior — all huntmode cowardly, which in this
// engine means they attack an account weaker than themselves).
type wolfDen struct {
	x, z, r int
}

// wolfDens is White Wolf Mountain's aggressive-wolf territory, spawn by spawn, rather than
// the single 45-tile square dangerZones uses to keep flee landings off the mountain.
//
// Why it exists (live 2026-09-19, android 02:39-02:40 and craft 03:24-03:25, both reproduced
// in cmd/labfleettest -mode wolf-mountain): a combat-3 account fail-teleported to Taverley
// walks home over the pass, is attacked, and the combat flee then either stood still
// ("no reachable flee candidate" dozens of times: the visible-wolf avoidance boxes the
// account in on every side) or picked ring candidates west of the pass (2797-2813,
// 3494-3524) whose routes run straight through the wolf pack — both accounts died there.
var wolfDens = []wolfDen{
	// whitewolf_sentry (npc 96): level 25, wanderrange 3.
	{2842, 3450, 5}, {2846, 3477, 5}, {2847, 3474, 5}, {2860, 3492, 5},
	{2864, 3453, 5}, {2865, 3509, 5}, {2870, 3438, 5}, {2872, 3445, 5},
	// whitewolf (npc 97): level 38, wanderrange 3.
	{2831, 3515, 5}, {2833, 3513, 5}, {2848, 3487, 5}, {2850, 3484, 5},
	{2854, 3509, 5}, {2856, 3508, 5},
	// wolfpack_leader (141) and pack_wolf (142): wanderrange 20 around spawns spanning
	// 2836..2844, 3495..3507, so one 26-tile square covers the pack's whole reach.
	{2840, 3501, 26},
	// The summit's other cowardly hunters, seen attacking in the wolf-mountain lab run:
	// icegiant (111, level 53, huntrange 2) and icewarrior (125, level 57, huntrange 5),
	// both wanderrange 3. The giants sit beside the old flee landings 2797-2813,3505-3524.
	{2817, 3514, 5}, {2824, 3510, 5},
	{2847, 3514, 8}, {2850, 3512, 8},
}

// mountainHostileTypes are the npc types behind wolfDens. visibleWolfAvoidance used to know
// only the sentry (96), so routes were planned straight past visible white wolves, the
// pack and the summit's ice giants and warriors. It keeps them wherever they stand: the Ice
// giant and Ice warrior also fill the Asgarnian ice dungeon, and a flee through that herd is no
// better than one through the summit's (visibleWolfAvoidance's comment has the live evidence).
var mountainHostileTypes = map[int]bool{96: true, 97: true, 111: true, 125: true, 141: true, 142: true}

// inWolfTerritory reports whether p is within reach of any mountain wolf spawn. The
// mountain is surface-only, so other levels are never in it.
func inWolfTerritory(p pathfind.NavPoint) bool {
	if p.Level != 0 {
		return false
	}
	for _, d := range wolfDens {
		if chebyshev(p.X, p.Z, d.x, d.z) <= d.r {
			return true
		}
	}
	return false
}

// routeExposure counts the tiles of a corner-compressed route from origin (origin itself
// excluded) that lie in wolf territory — how long a flee along it spends in reach of a
// wolf that has not attacked yet.
func routeExposure(origin pathfind.NavPoint, corners []pathfind.NavPoint) int {
	exposed := 0
	cx, cz := origin.X, origin.Z
	for _, c := range corners {
		for cx != c.X || cz != c.Z {
			cx += sign(c.X - cx)
			cz += sign(c.Z - cz)
			if inWolfTerritory(pathfind.NavPoint{X: cx, Z: cz, Level: c.Level}) {
				exposed++
			}
		}
	}
	return exposed
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// ringFleeCandidates is one ring of biasedFleeCandidates (biased) and, separately, the
// ring's remaining candidates (rest), each with the ring and threat spacing
// localEscapeAvoiding applies. rest is empty when no candidate leans toward toward, since
// biasedFleeCandidates then already returns them all.
func ringFleeCandidates(origin pathfind.NavPoint, threatX, threatZ int, toward pathfind.NavPoint, radius, distance int) (biased, rest []pathfind.NavPoint) {
	keep := func(p pathfind.NavPoint) bool {
		if radius > distance && chebyshev(p.X, p.Z, origin.X, origin.Z) <= radius-8 {
			return false
		}
		return chebyshev(p.X, p.Z, origin.X, origin.Z) >= 15 && chebyshev(p.X, p.Z, threatX, threatZ) >= 15
	}
	seen := make(map[pathfind.NavPoint]bool)
	for _, p := range biasedFleeCandidates(origin.X, origin.Z, origin.Level, threatX, threatZ, toward, radius) {
		seen[p] = true
		if keep(p) {
			biased = append(biased, p)
		}
	}
	for _, p := range fleeCandidates(origin.X, origin.Z, origin.Level, threatX, threatZ, radius) {
		if !seen[p] && keep(p) {
			rest = append(rest, p)
		}
	}
	return biased, rest
}

// fleeLandingOK is localEscapeAvoiding's landing test: outside dangerZones, real separation
// from the account and the threat, and a route that is not a detour around a barrier.
func fleeLandingOK(origin, landed pathfind.NavPoint, threatX, threatZ int, route []pathfind.NavPoint) bool {
	if inDangerZone(landed) || chebyshev(landed.X, landed.Z, origin.X, origin.Z) < 15 || chebyshev(landed.X, landed.Z, threatX, threatZ) < 15 {
		return false
	}
	return routeSteps(origin, route) <= maxFleeDetour(chebyshev(landed.X, landed.Z, origin.X, origin.Z))
}
