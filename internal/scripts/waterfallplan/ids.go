// Package waterfallplan is the pure half of the Waterfall Quest port: the stage model, the area
// model, the loadouts, the 18-pillar acknowledgement ledger and the decide() that picks the next
// leg from observable state. Ported from the TypeScript client's AIOQuester module
// src/bot/api/ai/quests/defs/waterfall.ts, and checked line by line against the lab engine's own
// content (content/scripts/quests/quest_waterfall/scripts/*.rs2), which is RS2 2004 content, not
// RuneScape Classic: the owner's wiki link describes the Classic quest, and where the two differ
// this package follows the server.
//
// It is pure: no client, actions, fleet or bot imports and no I/O, per
// docs/script_porting_framework.md. internal/scripts/waterfallrunner is the live half.
//
// # How progress is read
//
// %waterfall_quest (varp 65) and %waterfall_golrie_and_puzzle (varp 66) are scope=perm with no
// transmit, so the client never sees either. What IS readable, in the order decide() uses it:
//
//  1. The quest tab's colour (internal/quests, "Waterfall Quest", component 7350): not started,
//     in progress, complete.
//  2. The rendered journal (waterfall_journal.rs2), which names every stage exactly. ParseJournal
//     matches it newest entry first, because every later entry repeats the earlier history.
//  3. Items held and worn, by object id: the two "A key" objects (293 Golrie's, 298
//     Baxtorian's) and the two "Glarial's urn" objects (296 full, 297 empty) share names.
//  4. Where the account stands (AreaOf): the mound, the tree bank and the ledge are one-way, and
//     the three dungeons are only left by their own exits.
//
// # Where this deliberately differs from the TypeScript module
//
//   - The pillar loop. The TS placeRune SKIPS a rune it does not hold, returns success, and then
//     uses the amulet on the statue anyway. With any of the 18 (pillar, rune) bits unset the
//     statue drops a boulder for damage_self(20), which kills a 20-hitpoint account at full
//     health. Here the statue is used only when every one of the 18 pairs was acknowledged by the
//     server in THIS visit ("You place the rune on the stand." or "You remember putting that type
//     of rune there."), never by counting runes (PillarLedger).
//   - The way out. The TS leaves the raised room by the chalice's "Take treasure" washout. At stage
//     10 content answers that with "The chalice only contains some old ashes" and nothing else,
//     so the TS finished the quest with the account still in the dungeon. Here the exit after
//     completion is walked explicitly: the raised-room door with the key, the south door with the
//     key, the fire-giant lane, the entry door, the barrel.
//   - Sourcing. The TS self-funds by pickpocketing and buys runes at Betty's. Here the runner
//     tries the account's own bank, then one bundled mule ask, then shops (Aemad's rope, Aubury's
//     runes), so an empty mule still works (the owner's all-in-one rule, 2026-09-23).
//   - The chest stand. The TS stands NORTH of the tomb chest (2530,9845). The chest is placed at
//     angle 1 with forceapproach=north, which rotates to "east only", so the runner computes the
//     stand with internal/locreach instead of carrying a tile.
package waterfallplan

import "rsb0t-go/internal/scripts/tile"

// QuestName is the quest tab's own spelling (internal/quests' table: component 7350).
const QuestName = "Waterfall Quest"

// Item ids, from content/pack/obj.pack. The quest's own objects are 292-299.
const (
	ItemBook         = 292 // baxtorian_book_waterfall_quest: "Book on baxtorian", op Read
	ItemGolrieKey    = 293 // golrie_key_waterfall_quest: "A key" (the Tree Gnome Village one)
	ItemPebble       = 294 // glarials_pebble_waterfall_quest
	ItemAmulet       = 295 // glarials_amulet_waterfall_quest (wearable; the statue takes it from the PACK)
	ItemFullUrn      = 296 // glarials_urn_full_waterfall_quest
	ItemEmptyUrn     = 297 // glarials_urn_empty_waterfall_quest
	ItemBaxKey       = 298 // baxtorian_key_waterfall_quest: "A key" (the dungeon one)
	ItemMithrilSeeds = 299 // the reward: 40, stackable
	ItemWaterRune    = 555
	ItemAirRune      = 556
	ItemEarthRune    = 557
	ItemRope         = 954
	ItemCoins        = 995
	ItemShark        = 385
	ItemDiamond      = 1601
	ItemGoldBar      = 2357
)

// Loc ids, from content/pack/loc.pack, with the placements the static loc index holds
// (verified against the baked index: see waterfallrunner's real-pack test).
const (
	LocRaft         = 1987 // "Log raft", op1 Board, 2509,3493
	LocBookcase     = 1989 // "Bookcase", op1 Search, 2520,3426 level 1, forceapproach=east
	LocGolrieCrate  = 1990 // "Crate", op1 Search, 2548,9565
	LocGolrieGate   = 1991 // "Door" (a prison gate), 2515,9575, wall shape 0 angle 1
	LocTombstone    = 1992 // "Tombstone of glarial", 2x2 at 2558,3444, op1 Read
	LocCoffin       = 1993 // "Tomb of glarial", 2x1 at 2542,9811 angle 1, op1 Search
	LocChestClosed  = 1994 // "Closed chest", 2530,9844 angle 1, forceapproach=north (rotates to east)
	LocChestOpen    = 1995 // "Open chest", op1 Search (a live loc change from 1994)
	LocRock         = 1996 // "Rock", op1 Swim to, 2512,3468
	LocRockRoped    = 1997 // the same rock for eight ticks after a rope lands on it
	LocBaxCrate     = 1999 // "Crate", op1 Search, 2589,9888
	LocEntryDoor    = 2000 // "Door", 2575,9861: op1 back out to the ledge
	LocKeyedDoor    = 2002 // "Door", six placements; op1 is "The door is locked." below z 9895
	LocPillar       = 2004 // "Pillar", no ops; six in the puzzle room
	LocStatue       = 2006 // "Statue of Glarial", 2565,9916 in the puzzle room
	LocLedgeDoor    = 2010 // "Ledge", op1 Open, 2511,3464
	LocChalice      = 2014 // "Chalice of eternity", 2x2 at 2603,9910, op1 Take treasure
	LocDeadTree     = 2020 // "Dead tree", op1 Climb (a TRAP: 8 damage and a washout), 2512,3465
	LocBarrel       = 2022 // "Barrel", op1 Get in (a no-damage washout), 2512,3463
	LocLadderDown   = 1754 // "Ladder", Climb-down: the Tree Gnome Village one is at 2533,3155
	LocLadderUp     = 1757 // "Ladder", Climb-up: 2533,9555 (TGV) and 2556,9844 (the tomb)
	LocLargeDoorA   = 1516 // "Large door" halves in the waterfall dungeon
	LocLargeDoorB   = 1519
	LocStaircaseUp  = 1738 // the Hemenster tourist centre's stairs up to the bookcase floor
	LocStaircaseOff = 1740
)

// NPC ids, from content/pack/npc.pack.
const (
	NpcHadley   = 302
	NpcGerald   = 303
	NpcAlmera   = 304
	NpcHudon    = 305
	NpcGolrie   = 306
	NpcFireGnt  = 110
	NpcMossGnt  = 112
	NpcSpider   = 58
	NpcGiantSkl = 93
)

// Tiles, all from content coordinates (level_mx_mz_lx_lz, x = mx*64+lx, z = mz*64+lz) or from
// the static loc index; the stands are re-derived by the runner with internal/locreach where a
// loc is involved.
var (
	// ArdougneBank is the East Ardougne north bank, the TS module's bank and Stage 5's home.
	ArdougneBank = tile.Tile{X: 2616, Z: 3332}
	// AlmeraAnchor is Almera's spawn, inside her house by the falls (m39_54 26,42).
	AlmeraAnchor = tile.Tile{X: 2522, Z: 3498}
	// RaftStand is beside the Log raft (loc 1987 at 2509,3493, 1x2): the tile the webwalk graph's
	// raft edge boards from.
	RaftStand = tile.Tile{X: 2510, Z: 3493}
	// MoundLanding is where the raft puts the account: 0_39_54_16_25.
	MoundLanding = tile.Tile{X: 2512, Z: 3481}
	// HudonStand is inside Hudon's talk zone (inzone 2510-2514, 3476-3481) beside his spawn.
	HudonStand = tile.Tile{X: 2511, Z: 3481}
	// SwimStand is the mound tile the TS swims from: in the Hudon zone and north of the rock,
	// which both [aploc1,crossing_rock] and [aplocu,crossing_rock] require.
	SwimStand = tile.Tile{X: 2512, Z: 3477}
	// RockTile is the crossing rock (loc 1996).
	RockTile = tile.Tile{X: 2512, Z: 3468}
	// TreeBankLanding is where the rope on the rock leaves the account: the rock + (1, 0).
	TreeBankLanding = tile.Tile{X: 2513, Z: 3468}
	// DeadTreeTile is the dead tree (loc 2020); DeadTreeStand is the tile the webwalk graph's
	// rope edge uses, north of it.
	DeadTreeTile  = tile.Tile{X: 2512, Z: 3465}
	DeadTreeStand = tile.Tile{X: 2512, Z: 3466}
	// LedgeLanding is where the rope on the tree puts the account: 0_39_54_15_7.
	LedgeLanding = tile.Tile{X: 2511, Z: 3463}
	// LedgeDoorTile is the ledge door (loc 2010) and BarrelTile the escape barrel (loc 2022).
	LedgeDoorTile = tile.Tile{X: 2511, Z: 3464}
	BarrelTile    = tile.Tile{X: 2512, Z: 3463}
	// Washout is ^waterfall_fail_coord (0_39_53_31_21): every no-damage washout lands here.
	Washout = tile.Tile{X: 2527, Z: 3413}
	// BookcaseTile is the bookcase upstairs in the Hemenster tourist centre, and BookcaseStand
	// its east side (forceapproach=east).
	BookcaseTile  = tile.Tile{X: 2520, Z: 3426, Level: 1}
	BookcaseStand = tile.Tile{X: 2521, Z: 3426, Level: 1}
	// TGVLadderStand is the Tree Gnome Village ladder's stand on the surface (loc 1754 at
	// 2533,3155), and TGVLanding where it puts the account (the webwalk graph's edge).
	TGVLadderStand = tile.Tile{X: 2533, Z: 3156}
	TGVLanding     = tile.Tile{X: 2533, Z: 9556}
	// GolrieCrateTile is the key crate (loc 1990).
	GolrieCrateTile = tile.Tile{X: 2548, Z: 9565}
	// GolrieGateTile is Golrie's gate (loc 1991, a wall on the north edge of this tile);
	// GolrieGateOutside is the tile south of it, GolrieGateInside the one north of it.
	GolrieGateTile    = tile.Tile{X: 2515, Z: 9575}
	GolrieGateOutside = tile.Tile{X: 2515, Z: 9575}
	GolrieGateInside  = tile.Tile{X: 2515, Z: 9576}
	// GolrieAnchor is Golrie's spawn inside his cell (m39_149 19,45).
	GolrieAnchor = tile.Tile{X: 2515, Z: 9581}
	// TombstoneTile is the 2x2 Tombstone of Glarial (loc 1992).
	TombstoneTile = tile.Tile{X: 2558, Z: 3444}
	// TombLanding is where the pebble puts the account: 0_39_153_58_52.
	TombLanding = tile.Tile{X: 2554, Z: 9844}
	// ChestTile is the tomb chest (loc 1994/1995) and CoffinTile the coffin (loc 1993).
	ChestTile  = tile.Tile{X: 2530, Z: 9844}
	CoffinTile = tile.Tile{X: 2542, Z: 9811}
	// TombLadderTile is the tomb's ladder up (loc 1757, forceapproach rotated to east only),
	// TombLadderStand its only legal stand, and TombExit where it lands on the surface.
	TombLadderTile  = tile.Tile{X: 2556, Z: 9844}
	TombLadderStand = tile.Tile{X: 2557, Z: 9844}
	TombExit        = tile.Tile{X: 2556, Z: 3445}
	// DungeonEntry is where the ledge door puts the account: 0_40_154_15_5, which is also the
	// entry door (loc 2000) back out to the ledge.
	DungeonEntry = tile.Tile{X: 2575, Z: 9861}
	// BaxCrateTile is the key crate (loc 1999) and BaxCrateStand its west side, which the TS
	// chose because the south side touches a level-45 giant skeleton's spawn (2589,9886).
	BaxCrateTile  = tile.Tile{X: 2589, Z: 9888}
	BaxCrateStand = tile.Tile{X: 2588, Z: 9888}
	// LaneGate is the west Large door (1519 at 2565,9881): south of it is the shadow-spider
	// room, north of it the fire-giant lane. LaneWait is the tile south of it where the runner
	// waits for the lane to clear; LaneEnd is the south keyed door's south stand.
	LaneGate = tile.Tile{X: 2565, Z: 9881}
	LaneWait = tile.Tile{X: 2565, Z: 9880}
	// SouthDoorTile is the south keyed door (loc 2002, a wall on the north edge of 2568,9893):
	// SouthDoorSouth and SouthDoorNorth are its two sides.
	SouthDoorTile  = tile.Tile{X: 2568, Z: 9893}
	SouthDoorSouth = tile.Tile{X: 2568, Z: 9893}
	SouthDoorNorth = tile.Tile{X: 2568, Z: 9894}
	// PuzzleDoorTile is the puzzle keyed door (loc 2002 on the north edge of 2566,9901);
	// PuzzleDoorSouth is ^waterfall_original_room_door_coord (0_40_154_6_45).
	PuzzleDoorTile  = tile.Tile{X: 2566, Z: 9901}
	PuzzleDoorSouth = tile.Tile{X: 2566, Z: 9901}
	PuzzleDoorNorth = tile.Tile{X: 2566, Z: 9902}
	// StatueTile is the Statue of Glarial in the puzzle room (loc 2006) and StatueStand the tile
	// south of it.
	StatueTile  = tile.Tile{X: 2565, Z: 9916}
	StatueStand = tile.Tile{X: 2565, Z: 9915}
	// RaisedDoorTile is the raised room's keyed door (loc 2002 at 2604,9900) and RaisedDoorNorth
	// ^waterfall_raised_room_door_coord (0_40_154_44_45), where the key lands the account at
	// stage 8 and above.
	RaisedDoorTile  = tile.Tile{X: 2604, Z: 9900}
	RaisedDoorNorth = tile.Tile{X: 2604, Z: 9901}
	// RaisedLanding is where the statue puts the account (0_40_154_43_58); ChaliceTile is the
	// 2x2 Chalice of eternity (loc 2014) and ChaliceStand the tile south of its footprint.
	RaisedLanding = tile.Tile{X: 2603, Z: 9914}
	ChaliceTile   = tile.Tile{X: 2603, Z: 9910}
	ChaliceStand  = tile.Tile{X: 2603, Z: 9909}
	// HemensterMossGiants is the four moss giants (level 42, max hit 6) at 2549-2556,3401-3409
	// (maps/m39_53.jm2) that the default washout-to-bank route passes at distance 0, widened by
	// their wander range. Every surface walk this quest makes keeps out of it.
	HemensterMossGiants = Rect{MinX: 2544, MaxX: 2561, MinZ: 3396, MaxZ: 3414}
)

// Rect is an inclusive tile rectangle on level 0. The runner converts it to webwalk.Rect.
type Rect struct{ MinX, MaxX, MinZ, MaxZ int }

// Contains reports whether t is inside r (level ignored).
func (r Rect) Contains(t tile.Tile) bool {
	return t.X >= r.MinX && t.X <= r.MaxX && t.Z >= r.MinZ && t.Z <= r.MaxZ
}

// KeyedDoors are the six placements of loc 2002. The runner tells the webwalk planner to avoid
// every one of them: op1 answers "The door is locked." to anyone below z 9895, so a route across
// one is a route the Walker cannot walk. The quest's own legs use the key on the two it needs.
var KeyedDoors = []tile.Tile{
	{X: 2542, Z: 9892}, {X: 2606, Z: 9892}, {X: 2568, Z: 9893},
	{X: 2540, Z: 9900}, {X: 2604, Z: 9900}, {X: 2566, Z: 9901},
}
