package webwalk

// The Highwaymen as automatic low-combat zones: one north of Draynor, two north of Seers.
//
// What the engine does with a Highwayman (content npc 180 "highwayman", scripts/_unpack/225/
// all.npc: vislevel 5, size 1, wanderrange 8, huntrange 5, attackrange 3, huntmode "ranged",
// no maxrange of its own), read in the lab engine (rsb0t-main-headless/engine, branch 289):
//
//   - Who it hunts. content's all.hunt [ranged] is type=player, check_vis=lineofsight,
//     check_nottoostrong=outside_wilderness, check_notcombat=%lastcombat,
//     find_newmode=applayer2. Npc.huntPlayers skips a player when
//     `player.combatLevel > type.vislevel * 2` outside the wilderness (the
//     HuntCheckNotTooStrong.OUTSIDE_WILDERNESS test), so a level-5 Highwayman hunts combat 10
//     and below: AvoidAtOrBelowCombat is 2 x vislevel. "ranged" differs from [cowardly] only in
//     find_newmode (applayer2, an attack from up to attackrange, against opplayer2's melee);
//     the not-too-strong test is the same one, so "cowardly" does not mean "only players
//     weaker than itself" in this engine either: it means "combat at most twice its level".
//   - Where it hunts from. HuntIterator keeps a player whose CoordGrid.distanceToSW (Chebyshev,
//     from the npc's own south-west tile) is within huntrange of the npc's CURRENT tile, with
//     line of sight.
//   - Where that tile can be. Npc.wander queues a destination within wanderrange of the
//     spawn, but a chase steps up to maxrange from it (the step check in Npc.ts is
//     distanceToSW(coord, start) <= maxrange), and NpcType defaults maxrange to
//     wanderrange + 2 when the config sets none. A Highwayman that has just chased someone
//     can stand 10 tiles from his spawn and look 5 further.
//
// So the reach is maxrange + huntrange = 8 + 2 + 5 = 15 tiles around each spawn: the same 15
// the Draynor rectangle has always had (it was written as wander + hunt + 2 before the +2 was
// traced to maxrange's default). A size-1 npc adds nothing, because distanceToSW measures from
// the one tile it stands on.
//
// Draynor, live 2026-09-20 on 3299c3c: herring (combat 3), walking the Lumbridge -> Draynor
// -> Port Sarim road to the ship after a death, was chased on and off from 01:14 to 01:29
// around 3096-3120,3276-3301 and had died once on the same leg. One Highwayman,
// maps/m48_51.jm2 local 38,31 = 3110,3295. The road south of him (z 3264-3281) and the
// Draynor jail zone's detour both stay open.
//
// Seers, lab 2026-09-23 (agent "wcrung", account wcrf115, combat 3): a webwalk from the Seers
// bank to 2662,3560 planned north up the road past 2735,3506, was attacked at 2734,3525 by
// npc 180, fled, and was attacked again at 2737,3517. Two Highwaymen, maps/m42_55.jm2 NPC
// section local 47,1 and 61,1 = 2735,3521 and 2749,3521 (mapsquare 42,55 starts at
// 2688,3520). The Seers bank, its willows (2711,3508), maples (2727,3500, trees at z
// 3499-3502), normal trees and oaks all sit south or west of the rectangles, and the
// Seers-Catherby road runs well south of them; seershunters_test.go pins those tiles and its
// real-pack test plans the script routes around them.
func init() {
	KnownDangerZones = append(KnownDangerZones,
		highwaymanZone("draynor-highwayman", "Draynor Highwayman",
			"The level-5 Highwayman north of Draynor (content npc 180 at 3110,3295) hunts players of combat 10 "+
				"and below within 5 tiles of anywhere he can stand (8 tiles of wander, 10 of chase). Avoid as "+
				"transit at those levels. Not in the TS catalog.",
			[2]int{3110, 3295}),
		highwaymanZone("seers-north-highwaymen", "Highwaymen north of Seers' Village",
			"Two level-5 Highwaymen on the road north of Seers' Village (content npc 180 at 2735,3521 and "+
				"2749,3521) hunt players of combat 10 and below within 5 tiles of anywhere they can stand (8 "+
				"tiles of wander, 10 of chase). Avoid as transit at those levels. Not in the TS catalog: a "+
				"combat-3 lab walk from the Seers bank was attacked at 2734,3525.",
			[2]int{2735, 3521}, [2]int{2749, 3521}),
	)
}

// The Highwayman's hunting profile, from content and NpcType (see the comment above init).
const (
	highwaymanLevel       = 5
	highwaymanWanderRange = 8
	highwaymanHuntRange   = 5
	// highwaymanMaxRange is NpcType's default for a config with no maxrange: wanderrange + 2.
	highwaymanMaxRange = highwaymanWanderRange + 2
	// highwaymanReach is how far from his spawn a Highwayman can notice a player.
	highwaymanReach = highwaymanMaxRange + highwaymanHuntRange
)

// highwaymanZone is one automatic zone with a reach square around each spawn. Overlapping
// squares leave no gap between them, so two Highwaymen on one road need no merging.
func highwaymanZone(id, label, help string, spawns ...[2]int) DangerZone {
	rects := make([]Rect, 0, len(spawns))
	for _, s := range spawns {
		rects = append(rects, Rect{MinX: s[0] - highwaymanReach, MaxX: s[0] + highwaymanReach, MinZ: s[1] - highwaymanReach, MaxZ: s[1] + highwaymanReach, Level: 0})
	}
	return DangerZone{
		ID:                      id,
		Label:                   label,
		Help:                    help,
		Automatic:               true,
		AvoidAtOrBelowCombat:    2 * highwaymanLevel,
		AllowWhenEndpointInside: true,
		Rects:                   rects,
	}
}
