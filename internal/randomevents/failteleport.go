package randomevents

import "strconv"

// This file gives a name to a class of stranding this package's own Detect/Handle never
// covered: the Genie's and Mysterious Old Man's "ignored too long" fail-teleport (see
// docs/random_events_reference.md's 2026-09-14 addendum, written earlier this project from
// the same two primary sources cited below) can drop the player at ANY of a fixed, finite
// set of real-world locations — most are ordinary, perfectly walkable tiles a script's own
// pathfinding recovers from on its own, but a few are small, genuinely disconnected interior
// pockets with no baked route back out at all. detect.go's own KindTrapped only ever
// recognized the Mime/maze mapsquares; this file is deliberately NOT another KindTrapped
// entry (mime/maze both have a real solver — see maze.go/handle.go's HandleMime — and a
// pocket like this has none), just a named-location lookup a caller like
// freshstartrunner's STRANDED branch can use to tell an operator/dashboard WHAT the account
// is stuck in instead of a bare "no path" line, and to decide whether a teleport-spell
// escape is worth attempting.
//
// Live incident this was built from: account "acct24", frozen at x=3212,z=9615,level=0,
// logging `STRANDED: no path to 3222,3218,0 has succeeded in 6 attempts in a row` forever.
// 3212>>6,9615>>6 = mapsquare 50,150 — exactly the "lubmy basement" entry below
// (0_50_150_18_18 -> world 3218,9618, a few tiles from the observed position, well within
// normal in-room wandering; its exact index in failTeleportDestinations below has since
// shifted after this session added the previously-missing "Falador Shield Shop" entry, so
// this comment names it instead of an index that would just go stale again). Ground-truthed
// against the two primary sources this whole
// package already cites for random-event mechanics (see detect.go's package doc comment and
// docs/random_events_reference.md):
//
//  1. content/scripts/macro events/configs/macro_events_teleports.enum — the server's own
//     authoritative `macro_event_fail_teleports_free`/`macro_event_fail_teleports_members`
//     tables `macro_event_fail_teleport` (macro_events.rs2) picks from at random. Every
//     destination below is copied from there, decoded from that file's own
//     `level_mapsquareX_mapsquareZ_localX_localZ` coord format (world tile =
//     mapsquareX*64+localX, mapsquareZ*64+localZ — same convention internal/pathfind's own
//     Pack uses, see its own doc comment).
//  2. content/maps/m50_150.jm2 and content/maps/m48_149.jm2 both exist as real, static map
//     files — confirming "lubmy basement"/"Wizards' Tower basement" are genuine fixed
//     content, not a synthesized instance copy.
//
// Notably, an earlier hypothesis for this same incident (a generic "+6400 in z = an
// instanced copy of the map" convention, by analogy with other games' engines) turned out to
// be checking the right arithmetic for the wrong reason: 9615-6400=3215 does land in the
// ordinary Lumbridge coordinate range, and 6400 tiles is exactly 100 mapsquares (64 tiles
// each) — but that is this specific LostCityRS content build's own real, ground-truthed
// convention for an UNDERGROUND counterpart of a surface mapsquare (confirmed by BOTH known
// interior fail-teleport destinations: Wizards' Tower basement sits at mapsquare 48,149,
// exactly 100 mapsquares south of the real Wizards' Tower at 48,49; "lubmy basement" sits at
// 50,150, exactly 100 south of the real Lumbridge-area mapsquare 50,50) — not a
// minigame/random-event "instance" mechanism in the sense the original hypothesis meant
// (a scratch copy of the map reused across many different events/players). It is simply
// where this content build's map files happen to store certain basements, and the reason
// it strands a player is that these pockets are unreachable from the account's own tracked
// destination by ordinary same-level walking.
//
// CORRECTED, this session, after actually checking (a real baked collision pack + loc
// index, offline, no live session needed — see freshstartrunner.tryEscapeKnownPocket's own
// doc comment for the exact evidence): an earlier version of this comment claimed the
// collision pack "was never built to include these mapsquares" and that both rooms have
// "no walkable exit at all regardless of pack coverage" — both wrong. The pack DOES cover
// both: "lubmy basement" is a real 79-tile walkable connected component, Wizards' Tower
// basement a real 73-tile one — each its own small, disconnected component, the same shape
// as an ordinary sealed building tryOpenDoor already recovers from elsewhere in this
// codebase. And "lubmy basement" specifically is NOT exit-less: it has a real "Ladder"
// (Climb-up) a few tiles from the live incident's own tile, connected by a genuine FindPath
// route within that component, which climbs to a matching surface "Trapdoor" — a real human
// player's actual way down into and back out of that basement. The only reason nothing
// before this session ever found or used it is that freshstartrunner's own tryChangeLevel
// (the code that already searches for exactly this kind of fixture) only ever runs when
// from.Level != dest.Level — never true here, since a pocket shares its surface
// counterpart's own level NUMBER (only mapsquareZ is offset +100).
// freshstartrunner.tryEscapeKnownPocket is that same fixture search with that one gate
// removed. Wizards' Tower basement's own ladder object was also found nearby, but the pack
// shows a wall between it and the landing room actually observed — see that entry's own
// comment below for the honest caveat (not proven exit-less, just not yet confirmed
// reachable with the current pack). Every OTHER entry in this table is a genuinely ordinary
// building/tower floor at some other mapsquare (never mz>=100), which the existing
// tryChangeLevel/tryOpenDoor machinery already recovers from unaided — see
// LooksLikeUndergroundMapsquare's own doc comment for why mz>=100 specifically singles out
// just these two as the real "disconnected pocket" cases needing tryEscapeKnownPocket at
// all.
type failDest struct {
	level, mx, mz int
	name          string
}

// failTeleportDestinations is every entry in macro_event_fail_teleports_free/_members,
// deduplicated by (level, mapsquareX, mapsquareZ) — several ids in the members table repeat
// a free-table mapsquare verbatim (wizards tower near mizgog / lubmy basement / varrock east
// bank all appear in both enums with the same coord). Comment text is carried over from the
// enum file's own comments where one exists.
var failTeleportDestinations = []failDest{
	{1, 51, 54, "Jolly Boar Inn"},
	{1, 48, 51, "upstairs in Morgan's house, Draynor"},
	// NOT its own enum entry — the same "only one floor catalogued" gap found and fixed for
	// the Ardougne Monastery pocket below (mapsquare 40,50): this is the GROUND FLOOR of the
	// same building as "upstairs in Morgan's house, Draynor" just above (same mapsquare,
	// level 0 instead of 1), added after a real live incident this session (account
	// "acct50", running ProgressiveFisher's Go port, frozen at 3098,3270,0 while its own
	// fisherrunner.travelViaFreshStart fallback — the same pickpocket+boat route
	// freshstartrunner drives fresh accounts through — logged the generic STRANDED line).
	// Ground-truthed offline against the real collision pack + loc index (cmd/doordiag, no
	// live session needed): 3098,3270,0 is a real closed "Door" (loc id 1530, op "Open") —
	// the account fail-teleported directly onto the door's own tile — sitting on the
	// boundary of a genuine, small 17-tile walkable connected component (bbox x 3096..3100,
	// z 3266..3270) furnished with real shop fixtures (Table/Stool/Chair/Warehouse
	// shelves/Closed chest/Crate/Barrel/Drawers) plus a "Staircase" (id 1722) up to the
	// already-catalogued level-1 room above. Unlike the Ardougne Monastery ground floor,
	// this pocket is NOT sealed: the door's own north side (3098,3271, the one side the
	// static pack's ExitMask reports as outside the 17-tile component) has a confirmed real
	// 40-waypoint FindPath route all the way out to open world once past it — an ordinary
	// single-door stranding freshstartrunner.tryOpenDoor already recovers from unaided, the
	// same trivial one-hop case acct101's Varrock Palace bathroom turned out to be (see
	// tryEscapeSealedPocket's own doc comment). Catalogued here only so STRANDED logging
	// names it instead of a generic "no path" line.
	{0, 48, 51, "Morgan's house, Draynor — ground floor"},
	{2, 46, 52, "Falador general store tower"},
	{3, 50, 54, "Varrock palace, south east tower"},
	{1, 50, 50, "Lumbridge Castle (northern gate tower / spinning wheel room)"},
	// Real "wizards_tower_ladder" object (content/pack/loc.pack id 2148) found nearby
	// offline this session (a real baked collision pack + loc index, no live session
	// needed), BUT the pack shows a wall between the landing room's own connected walkable
	// component and that object's own tile — no confirmed walkable route from the actual
	// landing tile exists yet with the CURRENT collision pack. That is not proof the real
	// room has no exit at all (this pack may simply be under-explored for this specific
	// mapsquare — see this file's own top doc comment on why these mapsquares have thin
	// coverage to begin with); it is only an honest "not yet verified reachable," unlike
	// "lubmy basement" below. freshstartrunner.tryEscapeKnownPocket (this session's new
	// walk-based pocket rescue) tries here too and simply returns false today, falling
	// through to the existing Magic-spell/give-up handling exactly as before.
	{0, 48, 149, "Wizards' Tower basement"},
	{2, 51, 49, "Zeke's Scimitar shop, top floor"},
	{1, 46, 50, "Rimmington, north-eastern house"},
	{0, 46, 52, "Falador Castle"},
	{0, 45, 54, "Asgarnian Chaos Temple"},
	{0, 50, 53, "Varrock (Fountain / Museum / east bank)"},
	{0, 48, 53, "Barbarian Village"},
	{0, 46, 49, "Thurgo"},
	{1, 47, 50, "Port Sarim, above Brian's Battleaxe Bazaar"},
	// Missing from this table until this session's own audit against the enum file: the
	// members table's own val=14 entry (Falador Shield Shop, 1_46_52_31_54) has a DIFFERENT
	// level (1) than the free table's val=2 "Falador general store tower" at the same
	// mapsquare (2, 46, 52) below — the (level, mx, mz) dedup key this file already uses
	// correctly treats these as two distinct destinations, but the members-only one was
	// simply never carried over when this table was first built from the free table's own
	// 24 entries. Content-confirmed the same way every other entry here is: copied verbatim
	// from the enum file, not guessed.
	{1, 46, 52, "Falador Shield Shop"},
	{1, 48, 49, "Wizards' Tower"},
	{0, 51, 50, "North Al-Kharid, Gem stall"},
	{0, 47, 54, "Monastery"},
	{0, 48, 54, "Edgeville"},
	{2, 48, 49, "Wizards' Tower, near Mizgog"},
	// The one confirmed live in this incident. UPDATE, this session: the earlier "no known
	// walkable exit" claim below was wrong — offline verification (a real baked collision
	// pack + loc index, cmd/doordiag-equivalent checks, no live session needed) found a
	// real "Ladder" (id 1755, op "Climb-up") 3 tiles Chebyshev from the live acct24
	// incident's own tile (3212,9615,0), inside the SAME 79-tile walkable connected
	// component as that tile (confirmed via FindPath: a genuine 6-waypoint route from
	// 3212,9615 to beside the ladder). Climbing it lands beside a matching "Trapdoor" (id
	// 1568, op "Open") at 3209,3216,0 — the same x,z minus 6400 in z, back on the ordinary
	// Lumbridge-area surface. freshstartrunner.tryEscapeKnownPocket (this session's new
	// walk-based pocket rescue) now does exactly this automatically; see its own doc
	// comment for the full mechanism.
	{0, 50, 150, "\"lubmy basement\" — a sealed interior pocket south of Lumbridge with a real, walkable Ladder/Trapdoor exit (see this entry's own comment)"},
	{1, 39, 53, "Tourist Information Centre"},
	{2, 42, 53, "Thormac's shop"},
	{1, 39, 48, "South-west Yanille walls"},
	{0, 38, 52, "The Outpost, Ardougne"},
	{0, 43, 54, "Camelot, north-east outside the topiary"},
	{1, 40, 51, "East Ardougne, above the general store"},
	{1, 45, 53, "Taverley, next to Sanfew"},
	{1, 42, 54, "Seers' Village, near the spinning wheel"},
	{0, 41, 48, "2nd bridge to Hazelmere"},
	{1, 41, 49, "Port Khazard, above the anvil"},
	{0, 43, 50, "Brimhaven, northern dock"},
	{1, 40, 50, "Ardougne Monastery, eastern tower"},
	// NOT its own enum entry — this is the GROUND FLOOR directly beneath the "Ardougne
	// Monastery, eastern tower" entry just above (same mapsquare, level 0 instead of 1),
	// added after ground-truthing a real live incident this session (account "acct63",
	// frozen at 2617,3207,0 logging the same generic STRANDED line the "acct24"/lubmy-
	// basement incident that started this whole file was built from). The real chain: the
	// fail-teleport drops a player in the level-1 bedroom above (the catalogued entry), and
	// the ordinary same-level-mismatch stair search correctly climbs DOWN the real
	// "Staircase" there to reach level 0 — landing in THIS room, which turns out to be its
	// own separate, genuinely sealed 61-tile pocket (confirmed via cmd/doordiag: FindPath
	// from 2617,3207 to anywhere outside expands only 61 nodes before giving up). Unlike
	// "lubmy basement," this one has no direct stair/ladder exit at all — the level-1
	// bedroom directly above (2613-2618,3207-3211, 24 tiles) is ALSO fully sealed, no
	// further doors/stairs within 20 tiles (re-confirmed live 2026-09-16 after a second,
	// independent account, "acct32", climbed the same dead-end staircase and froze at
	// 2618,3208,1). The real way out is a two-door CHAIN instead: a closed "Door" (id 1530)
	// at 2610,3209 into a second sealed room (2602-2610,3207-3218, 64 tiles), then a second
	// closed "Door" (id 1530) at 2606,3219 opening directly onto a genuine 109,716-tile
	// outdoor component — freshstartrunner.tryEscapeSealedPocket (this session's new
	// multi-hop door-chain rescue, see its own doc comment) finds and walks exactly this
	// chain automatically; tryEscapeKnownPocket's own single-fixture search alone cannot,
	// since neither the dead-end stair nor either door in isolation leads anywhere by
	// itself.
	{0, 40, 50, "Ardougne Monastery, ground floor"},
}

// KnownFailTeleportPocket reports whether x,z,level's mapsquare is one of the fixed set of
// real locations content/scripts/macro events/configs/macro_events_teleports.enum lists as a
// Genie/Mysterious Old Man "ignored too long" fail-teleport destination (see this file's own
// doc comment for the two primary sources this is ground-truthed against), returning that
// destination's name. Checked by MAPSQUARE, not exact tile — a player who fail-teleported in
// can walk a few tiles within the destination room, same as the mime/maze mapsquare check
// this mirrors (detect.go's KindTrapped case).
func KnownFailTeleportPocket(x, z, level int) (name string, ok bool) {
	mx, mz := x>>6, z>>6
	for _, d := range failTeleportDestinations {
		if d.level == level && d.mx == mx && d.mz == mz {
			return d.name, true
		}
	}
	return "", false
}

// shipArrival is one tile content's own ship scripts put an arriving passenger down on.
type shipArrival struct {
	x, z, level int
	route       string
}

// shipArrivals is every destination coord passed to content's own ~set_sail, read from the
// call sites rather than inferred from a log. Decoded from the same
// level_mapsquareX_mapsquareZ_localX_localZ format failTeleportDestinations uses (world tile =
// mapsquareX*64+localX, mapsquareZ*64+localZ).
//
// Only the level-1 arrivals are listed, because only those can be confused with a displacement
// at all: the deck branch this feeds exists to explain a level-0 to level-1 hop. Captain
// Shanks' two runs (0_41_49_56_14 Khazard, 0_47_50_39_35 Port Sarim) arrive at level 0 and are
// deliberately left out.
//
// The two Port Sarim arrivals are the whole reason this table exists — both land in mapsquare
// 47,50 level 1, which the fail-teleport enum also catalogues as "Port Sarim, above Brian's
// Battleaxe Bazaar". See ClassifyDisplacement's own comment at the call site.
var shipArrivals = []shipArrival{
	// areas/area_karamja/scripts/customs_officer.rs2:84 — 1_47_50_24_17.
	{3032, 3217, 1, "the Karamja to Port Sarim crossing"},
	// areas/area_entrana/scripts/monk_of_entrana.rs2:7 — 1_47_50_40_31.
	{3048, 3231, 1, "the Entrana to Port Sarim crossing"},
	// areas/area_karamja/scripts/customs_officer.rs2:82 — 1_41_51_59_4.
	{2683, 3268, 1, "the Brimhaven to Ardougne crossing"},
	// areas/area_port_sarim/scripts/monk_of_entrana.rs2:23 — 1_44_52_18_3.
	{2834, 3331, 1, "the Port Sarim to Entrana crossing"},
}

// KnownShipArrival reports whether tile x,z,level is exactly where one of content's own ship
// crossings sets a passenger down, and names the crossing if so.
//
// Deliberately an EXACT tile match, unlike KnownFailTeleportPocket's mapsquare match: two of
// these arrivals share a mapsquare with a catalogued fail-teleport destination, and matching by
// square here would hide the real thing it is meant to sit in front of.
func KnownShipArrival(x, z, level int) (route string, ok bool) {
	for _, s := range shipArrivals {
		if s.level == level && s.x == x && s.z == z {
			return s.route, true
		}
	}
	return "", false
}

// LooksLikeUndergroundMapsquare reports whether mz is 100 or more mapsquares south of this
// content build's normal overworld range — the general, ground-truthed form of the
// convention KnownFailTeleportPocket's own doc comment found confirmed twice (Wizards'
// Tower basement at mz=149 vs its surface counterpart at mz=49; "lubmy basement" at mz=150
// vs the Lumbridge-area mz=50): this build stores at least some underground/basement areas
// at mapsquareZ+100 from their surface counterpart. 100 is not an arbitrary guess — every
// ordinary overworld location in macro_events_teleports.enum's own ~30-entry destination
// list (walkable towns/buildings a script recovers from unaided) falls in mz 48-54; both of
// the list's genuinely disconnected interior pockets fall at mz 149/150 (see this file's own
// top doc comment for exactly which one has since been confirmed to have a real, walkable
// exit vs. which one remains unverified). Exists as a fallback
// for a displacement KnownFailTeleportPocket's fixed list doesn't (yet) name — e.g. a future
// content revision's own new basement, or any other mechanism that lands a player in one of
// these underground copies — so a caller can still flag "this looks like an underground
// instance pocket, not an ordinary stranding" instead of staying silent just because the
// specific mapsquare isn't in the fixed table above.
func LooksLikeUndergroundMapsquare(mz int) bool {
	return mz >= 100
}

// DescribeStrandedLocation returns a short, human-readable reason x,z,level looks like a
// known "instance pocket" style stranding rather than an ordinary pathing failure — the
// fixed-list name when KnownFailTeleportPocket matches, otherwise a generic underground-
// mapsquare note when LooksLikeUndergroundMapsquare applies, otherwise "" (nothing about
// this location is unusual; treat it as an ordinary "no path" case). Callers such as
// freshstartrunner's own STRANDED handling use this to give an operator/dashboard a
// distinct, named condition instead of a generic "no path" line — see that package's
// walkNearCore for the call site this was built for.
func DescribeStrandedLocation(x, z, level int) string {
	if name, ok := KnownFailTeleportPocket(x, z, level); ok {
		return name
	}
	mx, mz := x>>6, z>>6
	if LooksLikeUndergroundMapsquare(mz) {
		return "an unmapped underground mapsquare (level " +
			strconv.Itoa(level) + ", mapsquare " + strconv.Itoa(mx) + "," + strconv.Itoa(mz) +
			") — not individually catalogued, but its mapsquareZ is >=100, this content build's " +
			"own convention for an underground counterpart of a surface area"
	}
	return ""
}
