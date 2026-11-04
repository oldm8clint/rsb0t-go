package webwalk

// The level-64 wolves on the Rellekka road and at the Fremennik vikings' camp, as automatic
// zones for accounts at or below the wolves' own combat level.
//
// COLLAB's residual risk (1) of 2026-09-23: these spawns were in no zone, hunt every combat level,
// and the Agility-35 way north round the Seers Highwaymen passes 7 tiles from one. From content
// (branch 289), read 2026-09-25:
//
//   - [wolf], npc 95 (scripts/_unpack/225/all.npc; no area config overrides it): size 2,
//     vislevel 64, wanderrange 6, huntrange 2, huntmode cowardly, no maxrange (so NpcType's
//     default wanderrange + 2 = 8). Spawns from the NPC sections of maps m41_56 and m42_55:
//     2647,3584, 2680,3585, 2682,3592, 2706,3580 and 2717,3569.
//   - [viking_wolf], npc 1330 (scripts/quests/quest_viking/configs/viking.npc): the same wolf
//     with huntrange 1. Spawns from m41_56: 2626,3633, 2630,3631, 2630,3637, 2641,3626,
//     2642,3622 and 2644,3627.
//
// Reach is seershunters_test.go's rule, max(maxrange + huntrange, wanderrange + huntrange +
// size): 10 for the road wolves (COLLAB measured 10) and 9 for the viking wolves.
//
// Who is avoided is the owner's ruling of 2026-09-25, "avoid at low combat". Cowardly hunting
// takes every player up to twice the vislevel, 128, so the hunt rule does not say what "low"
// is. The line drawn here is the wolves' own level, 64: at or below it an account is attacked
// by something at least as strong as itself (69 hitpoints, attack 50, strength 55). Change
// wolfZoneMaxCombat to move it.
const wolfZoneMaxCombat = 64

const (
	rellekkaWolfReach = 10
	vikingWolfReach   = 9
)

var (
	rellekkaWolfSpawns = [][2]int{{2647, 3584}, {2680, 3585}, {2682, 3592}, {2706, 3580}, {2717, 3569}}
	vikingWolfSpawns   = [][2]int{{2626, 3633}, {2630, 3631}, {2630, 3637}, {2641, 3626}, {2642, 3622}, {2644, 3627}}
)

func init() {
	KnownDangerZones = append(KnownDangerZones,
		wolfZone("rellekka-road-wolves", "Wolves on the Rellekka road",
			"Five level-64 wolves (content npc 95, size 2, wander 6, hunt 2) along the road from Seers to "+
				"Rellekka hunt within 10 tiles of their spawns. Avoided at combat 64 and below. Not in the TS catalog.",
			rellekkaWolfReach, rellekkaWolfSpawns),
		wolfZone("viking-wolves", "Wolves at the Fremennik camp",
			"Six level-64 wolves (content npc 1330, size 2, wander 6, hunt 1) south-west of Rellekka hunt within "+
				"9 tiles of their spawns. Avoided at combat 64 and below. Not in the TS catalog.",
			vikingWolfReach, vikingWolfSpawns),
	)
}

func wolfZone(id, label, help string, reach int, spawns [][2]int) DangerZone {
	rects := make([]Rect, 0, len(spawns))
	for _, s := range spawns {
		rects = append(rects, Rect{MinX: s[0] - reach, MaxX: s[0] + reach, MinZ: s[1] - reach, MaxZ: s[1] + reach, Level: 0})
	}
	return DangerZone{
		ID:                      id,
		Label:                   label,
		Help:                    help,
		Automatic:               true,
		AvoidAtOrBelowCombat:    wolfZoneMaxCombat,
		AllowWhenEndpointInside: true,
		Rects:                   rects,
	}
}
