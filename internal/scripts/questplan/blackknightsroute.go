package questplan

import (
	"sort"

	"rsb0t-go/internal/scripts/tile"
)

// The Black Knights' Fortress as a route: the doors, secret walls and ladders that join its rooms, written down once from
// Content 289's maps and scripts and checked tile by tile against the collision pack, the loc index and the production config
// (blackknightsfortress_pack_test.go in questplan, blackknight_pack_test.go in questmods).
//
// WHY THIS IS NOT THE WALKER'S JOB. The long-distance walker (internal/webwalk) joins the fortress with edges its graph took
// from the TypeScript client, and several of them are not edges the engine will run: the "Ladder" at 3022,3518 is entered from
// the SOUTH (loc 1750 is a ladder_directional with forceapproach 14, which the engine reads as "approach from one side only",
// rotated by the placement's angle 2), where the graph stands the account at 3021,3519, diagonal to it; the ladder at
// 3021,3510 has the graph's stand two tiles from it, behind a suit of armour. A walk that trusted them would send the click from
// a tile the engine refuses ("I can't reach that!") and nothing would happen. So the account is walked to the front door with
// the walker, and everything inside it is taken leg by leg from this table: one loc, the op, the one tile it is operated from
// and the tile it lands on.
//
// THE SHAPE OF THE PLACE (level, x, z; every number below is checked against the pack):
//
//	The front door (loc 2337 at 3016,3514) opens only on an account wearing an iron chainbody and a bronze med helm
//	(bkfortressdoor1, quest_blackknight.rs2) and lets it into the entry hall (3008..3019, 3513..3518), where the Fortress Guards
//	stand (level 20, not aggressive, content fortressguard). From the hall:
//	  - the "Wall" (loc 2341 at 3016,3517, "Push") opens into a two-tile alcove holding a ladder up (3015,3519);
//	  - the "Door" (loc 2338 at 3020,3515) is the Black Knights' banquet hall, five aggressive level-33 knights behind it. From
//	    outside it opens only after a Fortress Guard's warning and the answer "I don't care. I'm going in anyway.", which sets
//	    every knight within five tiles of 3025,3515 on the account (black_knights_aggro).
//	Upstairs the rooms are joined by ladders and doors in a way that makes the route a loop, not a line:
//	  - level 1 west (room A, 3015..3019 x 3515..3516) is reached from the alcove only through level 2: ladder up
//	    (3016,3519), a level-2 walk, ladder down (3017,3516);
//	  - room A, two plain doors east (loc 1530 at 3019,3515 and 3021,3515: they only OPEN, for 500 ticks, and the account walks
//	    through; every other door and wall of the fortress moves the account through itself), is room C's north half (3022..3024);
//	    the ladder at 3023,3513 goes up to
//	    level 2 and the one at 3025,3513 comes down into room D (3025..3029, 3511..3515), the walls between the two rooms having
//	    no door on level 1;
//	  - room D's "Sturdy door" (loc 2339 at 3025,3511) opens west into room C's south half, where the ladder at 3021,3510 goes
//	    DOWN to the grill: a corridor (3022..3025, 3510) and a shaft (3025, 3506..3509) that nothing else joins. The Grill (loc
//	    2342, a straight wall decoration, so reached from its own tile) is at the shaft's foot, 3025,3507;
//	  - the Hole (loc 2336 at 3031,3507) is in the warehouse above the witch's cauldron, behind the "Wall" at 3030,3510. That
//	    wall is reached from a corridor (x 3030) that joins the big north room, and the north room is reached from NOWHERE but
//	    the banquet hall's ladder at 3022,3518. So the cabbage's way to the hole goes through the banquet hall, and so does the
//	    way out.
//
// THE KNIGHTS. aggressive_black_knight is npc 179: level 33, 42 hitpoints, attack rate 5, huntmode cowardly with huntrange 5,
// and Npc.ts refuses a hunt when the account's combat level is more than twice the knight's (66) outside the Wilderness. They
// stand at 3022,3513 3023,3517 3027,3512 3028,3514 3029,3517 (hall), 3030,3502 3031,3502 3032,3503 (in the open ground south
// of the fortress), 3015,3515 3023,3512 3023,3516 3024,3510 (level 1) and 3014,3516 3023,3514 3026,3507 (level 2). The module
// never fights one (Module.NoRetaliate): it moves on, eats and parks on a death.

// The regions of the route: the connected pieces of floor (static collision, doors shut) the legs run between.
const (
	// BKOutside is the open ground round the fortress.
	BKOutside = "outside"
	// BKEntryHall is the hall behind the front door and west of the banquet hall's door.
	BKEntryHall = "entry hall"
	// BKAlcove0 is the secret alcove (3015..3016, 3517..3519) behind the Wall, with the ladder up.
	BKAlcove0 = "alcove"
	// BKBanquetHall is the Black Knights' hall.
	BKBanquetHall = "banquet hall"
	// BKGrillShaft is the corridor and shaft under room C's south half.
	BKGrillShaft = "grill shaft"
	// BKAlcove1 is the alcove's upper floor.
	BKAlcove1 = "alcove, upstairs"
	// BKL2West is the level-2 floor over the entry hall's west.
	BKL2West = "level 2, west"
	// BKL2East is the level-2 corridor over rooms C and D.
	BKL2East = "level 2, east"
	// BKRoomA is the level-1 room over the entry hall.
	BKRoomA = "room A"
	// BKDoorCell is the two-tile cell between room A's door and room C's.
	BKDoorCell = "door cell"
	// BKRoomCNorth is room C's north half.
	BKRoomCNorth = "room C, north"
	// BKRoomCSouth is room C's south half, with the ladder down to the grill.
	BKRoomCSouth = "room C, south"
	// BKRoomD is the room east of room C.
	BKRoomD = "room D"
	// BKNorthRoom is the big level-1 room over the banquet hall and the corridor east of it.
	BKNorthRoom = "north room"
	// BKWarehouse is the room above the cauldron, with the Hole.
	BKWarehouse = "warehouse"
)

// BKLoc is a placed loc by the id and the tile the map file gives it.
type BKLoc struct{ ID, X, Z, Level int }

// Tile is the loc's tile.
func (l BKLoc) Tile() tile.Tile { return tile.Tile{X: l.X, Z: l.Z, Level: l.Level} }

// BKLeg is one crossing: the loc operated, from which tile, and where it puts the account.
type BKLeg struct {
	Name     string
	From, To string
	Loc      BKLoc
	Op       string
	// Stand is the tile the op is sent from (a tile locreach.Tiles and the pack accept); Land is the tile the account stands on
	// afterwards (the loc's script moves it there, and a ladder keeps x and z).
	Stand, Land tile.Tile
	// Disguise is set on the crossing that checks the iron chainbody and the bronze med helm.
	Disguise bool
	// Dialogue is the answer to the menu the loc opens before it lets the account through.
	Dialogue []string
	// Opens is set on a plain door (loc 1530, [oploc1,_door_closed] ~open_door(500)): the op only swings it open, for 500 ticks, and the
	// account walks through it, where the fortress's own doors and walls move the account to Land themselves. An open door is
	// another loc (1531), so a door someone has opened is not "shut" and needs no op.
	Opens bool
	// Aggro is set on a crossing whose script sets knights on the account.
	Aggro bool
}

// Cost is the leg's weight in BKPlan: one, and three more for a crossing that sets knights on the account.
func (l BKLeg) Cost() int {
	if l.Aggro {
		return 4
	}
	return 1
}

// Loc ids of the route (production config, checked by the pack test).
const (
	LocBKFrontDoor  = 2337
	LocBKHallDoor   = 2338
	LocBKSturdyDoor = 2339
	LocBKSecretWall = 2341
	LocBKGrill      = 2342
	LocBKHole       = 2336
	LocBKDoor       = 1530
	LocBKLadderTop  = 1746 // [laddertop]: Climb-down, any side
	LocBKLadder     = 1747 // [ladder]: Climb-up, any side
	LocBKLadderTopD = 1749 // [laddertop_directional]: Climb-down, one side
	LocBKLadderD    = 1750 // [ladder_directional]: Climb-up, one side
)

// The two answers the banquet hall's guard takes.
const bkHallWarning = "I don't care. I'm going in anyway."

func bkLeg(name, from, to string, loc BKLoc, op string, stand, land tile.Tile) BKLeg {
	return BKLeg{Name: name, From: from, To: to, Loc: loc, Op: op, Stand: stand, Land: land, Opens: loc.ID == LocBKDoor}
}

func t3(x, z, l int) tile.Tile { return tile.Tile{X: x, Z: z, Level: l} }

// BKLegs is every crossing, each way. The order is the tie-break of BKPlan.
var BKLegs = func() []BKLeg {
	frontIn := bkLeg("the front door, in", BKOutside, BKEntryHall, BKLoc{LocBKFrontDoor, 3016, 3514, 0}, "Open", t3(3016, 3514, 0), t3(3016, 3515, 0))
	frontIn.Disguise = true
	hallIn := bkLeg("the banquet hall's door, in", BKEntryHall, BKBanquetHall, BKLoc{LocBKHallDoor, 3020, 3515, 0}, "Open", t3(3019, 3515, 0), t3(3020, 3515, 0))
	hallIn.Dialogue, hallIn.Aggro = []string{bkHallWarning}, true
	door3East := bkLeg("the sturdy door, east", BKRoomCSouth, BKRoomD, BKLoc{LocBKSturdyDoor, 3025, 3511, 1}, "Open", t3(3025, 3511, 1), t3(3026, 3511, 1))
	door3West := bkLeg("the sturdy door, west", BKRoomD, BKRoomCSouth, BKLoc{LocBKSturdyDoor, 3025, 3511, 1}, "Open", t3(3026, 3511, 1), t3(3025, 3511, 1))
	door3West.Aggro = true // bkfortressdoor3: opened from the east side it sets the knights within five tiles on the account
	ladderDown := bkLeg("the hall ladder, down", BKNorthRoom, BKBanquetHall, BKLoc{LocBKLadderTopD, 3022, 3518, 1}, "Climb-down", t3(3022, 3517, 1), t3(3022, 3517, 0))
	ladderDown.Aggro = true // laddertop_directional at 3022,3518: black_knights_aggro on arrival
	return []BKLeg{
		frontIn,
		bkLeg("the front door, out", BKEntryHall, BKOutside, BKLoc{LocBKFrontDoor, 3016, 3514, 0}, "Open", t3(3016, 3515, 0), t3(3016, 3514, 0)),
		bkLeg("the secret wall, in", BKEntryHall, BKAlcove0, BKLoc{LocBKSecretWall, 3016, 3517, 0}, "Push", t3(3016, 3516, 0), t3(3016, 3517, 0)),
		bkLeg("the secret wall, out", BKAlcove0, BKEntryHall, BKLoc{LocBKSecretWall, 3016, 3517, 0}, "Push", t3(3016, 3517, 0), t3(3016, 3516, 0)),
		hallIn,
		bkLeg("the banquet hall's door, out", BKBanquetHall, BKEntryHall, BKLoc{LocBKHallDoor, 3020, 3515, 0}, "Open", t3(3020, 3515, 0), t3(3019, 3515, 0)),

		bkLeg("the alcove ladder, up", BKAlcove0, BKAlcove1, BKLoc{LocBKLadderD, 3015, 3519, 0}, "Climb-up", t3(3015, 3518, 0), t3(3015, 3518, 1)),
		bkLeg("the alcove ladder, down", BKAlcove1, BKAlcove0, BKLoc{LocBKLadderTopD, 3015, 3519, 1}, "Climb-down", t3(3015, 3518, 1), t3(3015, 3518, 0)),
		bkLeg("the alcove's upper ladder, up", BKAlcove1, BKL2West, BKLoc{LocBKLadderD, 3016, 3519, 1}, "Climb-up", t3(3016, 3518, 1), t3(3016, 3518, 2)),
		bkLeg("the alcove's upper ladder, down", BKL2West, BKAlcove1, BKLoc{LocBKLadderTopD, 3016, 3519, 2}, "Climb-down", t3(3016, 3518, 2), t3(3016, 3518, 1)),
		bkLeg("room A's ladder, up", BKRoomA, BKL2West, BKLoc{LocBKLadder, 3017, 3516, 1}, "Climb-up", t3(3016, 3516, 1), t3(3016, 3516, 2)),
		bkLeg("room A's ladder, down", BKL2West, BKRoomA, BKLoc{LocBKLadderTop, 3017, 3516, 2}, "Climb-down", t3(3017, 3515, 2), t3(3017, 3515, 1)),

		bkLeg("room A's east door, east", BKRoomA, BKDoorCell, BKLoc{LocBKDoor, 3019, 3515, 1}, "Open", t3(3019, 3515, 1), t3(3020, 3515, 1)),
		bkLeg("room A's east door, west", BKDoorCell, BKRoomA, BKLoc{LocBKDoor, 3019, 3515, 1}, "Open", t3(3020, 3515, 1), t3(3019, 3515, 1)),
		bkLeg("the cell's east door, east", BKDoorCell, BKRoomCNorth, BKLoc{LocBKDoor, 3021, 3515, 1}, "Open", t3(3021, 3515, 1), t3(3022, 3515, 1)),
		bkLeg("the cell's east door, west", BKRoomCNorth, BKDoorCell, BKLoc{LocBKDoor, 3021, 3515, 1}, "Open", t3(3022, 3515, 1), t3(3021, 3515, 1)),

		bkLeg("room C's ladder, up", BKRoomCNorth, BKL2East, BKLoc{LocBKLadderD, 3023, 3513, 1}, "Climb-up", t3(3023, 3514, 1), t3(3023, 3514, 2)),
		bkLeg("room C's ladder, down", BKL2East, BKRoomCNorth, BKLoc{LocBKLadderTopD, 3023, 3513, 2}, "Climb-down", t3(3023, 3514, 2), t3(3023, 3514, 1)),
		bkLeg("room D's ladder, up", BKRoomD, BKL2East, BKLoc{LocBKLadderD, 3025, 3513, 1}, "Climb-up", t3(3025, 3514, 1), t3(3025, 3514, 2)),
		bkLeg("room D's ladder, down", BKL2East, BKRoomD, BKLoc{LocBKLadderTopD, 3025, 3513, 2}, "Climb-down", t3(3025, 3514, 2), t3(3025, 3514, 1)),

		door3East,
		door3West,
		bkLeg("the grill ladder, down", BKRoomCSouth, BKGrillShaft, BKLoc{LocBKLadderTop, 3021, 3510, 1}, "Climb-down", t3(3022, 3510, 1), t3(3022, 3510, 0)),
		bkLeg("the grill ladder, up", BKGrillShaft, BKRoomCSouth, BKLoc{LocBKLadder, 3021, 3510, 0}, "Climb-up", t3(3022, 3510, 0), t3(3022, 3510, 1)),

		bkLeg("the hall ladder, up", BKBanquetHall, BKNorthRoom, BKLoc{LocBKLadderD, 3022, 3518, 0}, "Climb-up", t3(3022, 3517, 0), t3(3022, 3517, 1)),
		ladderDown,
		bkLeg("the warehouse wall, in", BKNorthRoom, BKWarehouse, BKLoc{LocBKSecretWall, 3030, 3510, 1}, "Push", t3(3030, 3510, 1), t3(3030, 3509, 1)),
		bkLeg("the warehouse wall, out", BKWarehouse, BKNorthRoom, BKLoc{LocBKSecretWall, 3030, 3510, 1}, "Push", t3(3030, 3509, 1), t3(3030, 3510, 1)),
	}
}()

// BKRegionTiles are the tiles the table puts in each region (every leg's stand and landing), plus the two places the quest
// works: the grill's stand and the Hole's stands.
func BKRegionTiles() map[string][]tile.Tile {
	out := map[string][]tile.Tile{}
	add := func(region string, t tile.Tile) {
		for _, have := range out[region] {
			if have == t {
				return
			}
		}
		out[region] = append(out[region], t)
	}
	for _, l := range BKLegs {
		add(l.From, l.Stand)
		add(l.To, l.Land)
	}
	add(BKGrillShaft, BKGrillStand)
	for _, h := range BKHoleStands {
		add(BKWarehouse, h)
	}
	return out
}

// The places the quest works.
var (
	// BKDoorApproach is the tile outside the front door the walker is sent to: out of the knights' sentry ground (below), in
	// view of the Fortress Guards, one step from the door's stand.
	BKDoorApproach = t3(3016, 3513, 0)
	// BKWestWaypoint is open ground west of the fortress a leaving account walks to before it goes anywhere: the walker's own route
	// from the front door to Falador runs south along x 3024 past the three knights in the open ground.
	BKWestWaypoint = t3(3001, 3510, 0)
	// BKGrillStand is the grill's own tile (a straight wall decoration is reached from its own tile and no other).
	BKGrillStand = t3(3025, 3507, 0)
	// BKGrill and BKHole are the two locs the quest works on.
	BKGrill = BKLoc{LocBKGrill, 3025, 3507, 0}
	BKHole  = BKLoc{LocBKHole, 3031, 3507, 1}
	// BKHoleStands are the tiles the cabbage is dropped from: the Hole is a 1x1 loc with no forceapproach, so its four
	// neighbours, the two the warehouse's shelves leave.
	BKHoleStands = []tile.Tile{t3(3031, 3508, 1), t3(3030, 3507, 1), t3(3031, 3506, 1)}
)

// BKSentryGround is the open ground south and east of the fortress where three aggressive knights stand (3030,3502 3031,3502
// 3032,3503) and wander ten tiles: a walk to or from the door keeps out of it, which sends it round by the west (the walker's
// shortest way from Falador crosses it, and it is also the way the walker's cost-191 route takes to the door).
var BKSentryGround = Rect{MinX: 3018, MaxX: 3048, MinZ: 3488, MaxZ: 3512, Level: 0}

// bkInsideL0 are the level-0 rectangles that cover every walkable tile of the fortress's inside (those the open ground cannot
// reach on foot) and none of the open ground's: found by flooding the collision pack from 3016,3512 (the test pins it).
var bkInsideL0 = []Rect{
	{MinX: 3027, MaxX: 3030, MinZ: 3504, MaxZ: 3517},
	{MinX: 3026, MaxX: 3032, MinZ: 3505, MaxZ: 3508},
	{MinX: 3025, MaxX: 3028, MinZ: 3506, MaxZ: 3518},
	{MinX: 3021, MaxX: 3024, MinZ: 3510, MaxZ: 3518},
	{MinX: 3008, MaxX: 3012, MinZ: 3513, MaxZ: 3518},
	{MinX: 3019, MaxX: 3020, MinZ: 3513, MaxZ: 3518},
	{MinX: 3008, MaxX: 3014, MinZ: 3514, MaxZ: 3516},
	{MinX: 3015, MaxX: 3016, MinZ: 3515, MaxZ: 3519},
	{MinX: 3008, MaxX: 3018, MinZ: 3515, MaxZ: 3516},
}

// bkFortressBox bounds the fortress on every level.
var bkFortressBox = Rect{MinX: 3005, MaxX: 3034, MinZ: 3500, MaxZ: 3520}

// BKWaypointRadius is how near the west waypoint a walk to it counts as arrived.
const BKWaypointRadius = 3

// BKNeedsClearing reports whether a tile is on the open ground by the fortress and east of the west waypoint's reach: the ground
// the walker's default route to Falador or a bank crosses south along x 3024 past the three knights in BKSentryGround. An account
// that stands there after a walk through the fortress ended (a retreat, a run ending, a crossing that would not land at the front
// door) is walked to the waypoint before anything else sends it anywhere. Inside the fortress is not this: that is the way out.
func BKNeedsClearing(t tile.Tile) bool {
	return t.Level == 0 && !BKInside(t) && BKNearFortress(t) && t.X > BKWestWaypoint.X+BKWaypointRadius
}

// BKInside reports whether a tile is inside the fortress: any tile of its upper floors, and the walkable inside of level 0.
func BKInside(t tile.Tile) bool {
	if t.Level >= 1 {
		return t.X >= bkFortressBox.MinX && t.X <= bkFortressBox.MaxX && t.Z >= bkFortressBox.MinZ && t.Z <= bkFortressBox.MaxZ
	}
	for _, r := range bkInsideL0 {
		if r.Contains(t) {
			return true
		}
	}
	return false
}

// BKNearFortress is BKInside with the ground round the door and the sentries', for a decision about a walk.
func BKNearFortress(t tile.Tile) bool {
	if BKInside(t) {
		return true
	}
	return t.Level == 0 && t.X >= 2995 && t.X <= 3050 && t.Z >= 3486 && t.Z <= 3522
}

// BKPlan is the cheapest list of crossings from one region to another (BKLeg.Cost), nil when from is to or no list joins them. Ties
// go to the earlier entry of BKLegs. An unknown region has no plan.
func BKPlan(from, to string) []BKLeg {
	if from == to {
		return nil
	}
	regions := map[string]bool{}
	for _, l := range BKLegs {
		regions[l.From], regions[l.To] = true, true
	}
	if !regions[from] || !regions[to] {
		return nil
	}
	dist := map[string]int{from: 0}
	via := map[string]int{} // region -> index into BKLegs of the leg that reached it
	done := map[string]bool{}
	for {
		cur, best := "", -1
		var names []string
		for r := range dist {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, r := range names {
			if !done[r] && (best < 0 || dist[r] < best) {
				cur, best = r, dist[r]
			}
		}
		if best < 0 {
			return nil
		}
		if cur == to {
			break
		}
		done[cur] = true
		for i, l := range BKLegs {
			if l.From != cur {
				continue
			}
			if d, seen := dist[l.To]; !seen || best+l.Cost() < d {
				dist[l.To], via[l.To] = best+l.Cost(), i
			}
		}
	}
	var plan []BKLeg
	for r := to; r != from; r = BKLegs[via[r]].From {
		plan = append([]BKLeg{BKLegs[via[r]]}, plan...)
	}
	return plan
}

// BKLegNext is the first crossing of the plan from a region to another, ok false when there is none.
func BKLegNext(from, to string) (BKLeg, bool) {
	plan := BKPlan(from, to)
	if len(plan) == 0 {
		return BKLeg{}, false
	}
	return plan[0], true
}
