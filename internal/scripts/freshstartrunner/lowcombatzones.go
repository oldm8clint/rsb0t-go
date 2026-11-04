package freshstartrunner

import (
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/webwalk"
)

// lowCombatZoneIDs are webwalk catalog zones this package's own router honours, each applied
// only at the combat levels its catalog entry names and skipped when the walk starts or ends
// inside it (webwalk.ResolveDangerZones). Kept to zones checked against the collision pack for
// a way around: unlike dangerZones above, which would have cut Catherby off, each of these
// leaves the walk a route.
//
//   - draynor-jail-guards: live 2026-09-20 on 3299c3c, herring (combat 3) walking from its
//     Lumbridge respawn to the Port Sarim ship ("boat leg: Port Sarim ship -> Karamja ->
//     Brimhaven pier") was routed through the Draynor jail compound (3128,3251 -> 3093,3243),
//     where four level-26 Jail guards (content npc 917, huntmode aggressive_melee, spawns
//     3109,3237 / 3120,3238 / 3121,3249 / 3127,3248) chased it: dead at 01:12:42, then down to
//     40% on the same route again after respawning. webwalk has avoided this compound below
//     combat 51 all along; SailLeg's walker (walkNearCore) plans on the pack directly and never
//     looked. Avoiding it costs Lumbridge -> 3093,3243 nothing (29 corners instead of 34, over
//     the z=3264 road north of the compound).
//
//   - draynor-highwayman: the same account, same road, was chased again 01:14-01:29 around
//     3096-3120,3276-3301 by the level-5 Highwayman (content npc 180 at 3110,3295, aggressive to
//     combat 10 and below). See webwalk/highwayman.go.
//
//   - white-wolf-dens (webwalk/wolfdens.go, every combat level): lab 2026-09-23 (agent
//     "wcrung", private evidence notes), wcrg13 (combat 3) chopping the
//     Camelot evergreens asked this router for the 7 tiles 2784,3503 -> 2791,3502. The pack has
//     no short way between them, and the one it planned (92 corners, 207 tiles) ran east round
//     Camelot to 2825,3463 and 2848,3439 and up the mountain's southern foot, 4 tiles from the
//     whitewolf_sentry at 2842,3450. The account broke off from the sentries 20 times in 38
//     seconds, and its webwalk walks, which do avoid this zone, then found no way back out of
//     2835,3468 for the last nine minutes of the run. With the zone, that walk has no route at all, which is the
//     answer users asked for ("avoid White Wolf Mountain always, especially for low levels").
//
//   - seers-north-highwaymen (webwalk/highwayman.go, combat 10 and below, from branch
//     claude/seersdanger): the two level-5 Highwaymen at 2735,3521 and 2749,3521 that attacked a
//     combat-3 lab walk at 2734,3525.
//
//   - rellekka-road-wolves and viking-wolves (webwalk/rellekkawolves.go, combat 64 and below, the
//     owner's ruling of 2026-09-25): the level-64 wolves on the Rellekka road and at the Fremennik
//     camp, which hunt every combat level. Checked on the real pack the same day (a pack built from
//     Engine-TS/Content 289): the same 462 fleet walks and 13,812 camp tree walks, 0 changed.
//
// The two added 2026-09-23 were checked on the real pack at combat 3 (lowcombatroutes_test.go):
// 462 fleet walks (every Catherby fisher leg down to each stand beside each spot, the Seers,
// wcfletch, crafter and mule places, the freshstart legs, door-bound legs planned with their
// doors open) and 13,812 in-camp tree walks, and not one changed outcome or length. What they do
// change is the evergreen stands east of Seers, whose far trees the pack reaches only past that
// sentry: a walk to one is refused (zoneBlocked), and an account already standing behind them
// walks out the way it came (planAvoidingZones). "seers-south-bats" is deliberately NOT here, because it is the zone that cut Catherby
// off (see dangerZones).
var lowCombatZoneIDs = []string{"draynor-jail-guards", "draynor-highwayman", "white-wolf-dens", "seers-north-highwaymen", "rellekka-road-wolves", "viking-wolves"}

// zoneAvoid is inDangerZone plus lowCombatZoneIDs resolved for this account's combat level and
// this walk's endpoints.
func zoneAvoid(d *Deps, from, to pathfind.NavPoint) func(x, z, level int) bool {
	ctx := webwalk.ZoneContext{
		Start: &webwalk.Point{X: from.X, Z: from.Z, Level: from.Level},
		Dest:  &webwalk.Point{X: to.X, Z: to.Z, Level: to.Level},
	}
	if d != nil && d.Session != nil {
		ctx.CombatLevel, ctx.CombatKnown = d.Session.State().CombatLevel()
	}
	return zoneAvoidWith(lowCombatZoneIDs, ctx)
}

// zonePocketTiles is the size below which a start the zones wall in is a pocket to walk out of,
// not somewhere to stay. Measured on the real pack at combat 3 (lowcombatroutes_test.go): a
// zone-aware search reaches 101 tiles from 2835,3468, the ledge beside the 2842,3450 sentry
// where wcrg13 ended its lab run, and 287 from 2793,3500, the far Camelot evergreen stand; from
// anywhere in Seers, Catherby or Camelot it reaches 75,151 (Kandarin, which the mountain cuts
// off from Asgarnia). A closed room or a fail-teleport pocket is small too, but the plain pack
// cannot leave those either, so they still fall through to walkNearCore's recovery.
const zonePocketTiles = 2000

// planAvoidingZones is the search findPathAvoidingDanger runs: pk's route from from to to that
// stays out of zoned, except out of a pocket. A start outside every zone but walled in by them
// (both tiles zonePocketTiles names are outside every rectangle, so the endpoint exemption does
// not free them) has only ways out that cross a zone, and refusing them all leaves the account
// there for good, which is what wcrg13's webwalk walks did for the last nine minutes of its run.
// So when the zone-aware search runs out of tiles within zonePocketTiles, this plans again
// avoiding only inDangerZone, the route this router took before the zones, and reports escaped.
// The next plan, from inside the zone, is exempt from that zone anyway (AllowWhenEndpointInside),
// and once the account is out the zones apply again.
func planAvoidingZones(pk *pathfind.Pack, from, to pathfind.NavPoint, zoned func(x, z, level int) bool) (res pathfind.PathResult, escaped bool) {
	res = pk.FindPathAvoiding(from, to, zoned)
	if res.OK || res.Reason != "unreachable" || res.Expanded >= zonePocketTiles {
		return res, false
	}
	if out := pk.FindPathAvoiding(from, to, inDangerZone); out.OK {
		return out, true
	}
	return res, false
}

// zoneBlocked reports that res, a failed zone-aware search from from to to, failed only because
// of the zones: the same pack plans that walk when nothing is avoided. That is not a sealed room,
// a door the pack bakes shut or a missing level change, so none of walkNearCore's recovery (door,
// stile, stair, the STRANDED escape ladder) can find a route the zones allow; each would only
// walk the account to an unrelated door, and six in a row report it STRANDED. wcrg13's evergreen
// walk (see lowCombatZoneIDs) is the case: the pack's one way between those two tiles runs up
// White Wolf Mountain.
//
// Same-level only, like the pack itself: a level change is tryChangeLevel's job, and planning it
// on the pack here would only flood the whole level for nothing.
func zoneBlocked(pk *pathfind.Pack, from, to pathfind.NavPoint, res pathfind.PathResult) bool {
	if res.OK || pk == nil || from.Level != to.Level || res.Reason == "start not walkable" {
		return false
	}
	return pk.FindPath(from, to).OK
}

// zoneAvoidWith is zoneAvoid for an explicit zone list and context, so a test can plan the same
// walk with and without a zone.
func zoneAvoidWith(ids []string, ctx webwalk.ZoneContext) func(x, z, level int) bool {
	rects := webwalk.ResolveDangerZones(ids, nil, ctx)
	if len(rects) == 0 {
		return inDangerZone
	}
	return func(x, z, level int) bool {
		return inDangerZone(x, z, level) || webwalk.InZones(webwalk.Point{X: x, Z: z, Level: level}, rects)
	}
}
