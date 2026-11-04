package webwalk

// The Khazard stronghold of Tree Gnome Village (maps/m39_50.jm2), the doors the walk graph did not have.
//
// The stronghold is three places the pack walls off from each other and from the world:
//
//   - the hall behind the crumbled wall (loc 2185 at 2509,3253, climbed over from the south only, one way, and
//     only once the ballista has been fired), 49 tiles, 2500..2512 by 3254..3259;
//   - the ladder room west of it (28 tiles, 2500..2506 by 3251..3256), which holds the ladder up (1747 at
//     2503,3252) and the front door;
//   - the floor above (37 tiles, 2500..2506 by 3251..3258), with the chest, reached by the ladder alone.
//
// The graph (doors.json, stairEdges.json) has the ladder both ways and nothing else: neither of the two doors was
// ever in it. So a planned walk from anywhere in the stronghold, upstairs or down, climbed the ladder as far as the
// ladder room and found it shut ("baked walk failed: no path: unreachable" from 2506,3258,1, 2505,3256,1 and the
// hall, 16 Oct 2026). The first walk inside the hall to the ladder only got through because the walker's stall
// handler opened "Door at 2505,3256" by luck; once an account was upstairs with the Khazard commander at its back
// there was no way down and no way home, and the two accounts that went up with no food died there (03:15 and
// 03:41, at 2505,3256,1).
//
// The edges below are the exits the content scripts allow (quest_tree.rs2, doors/scripts/doors.rs2):
//
//   - the inner door, loc 1530 at 2505,3256 (maps/m39_50.jm2 "0 9 56: 1530 9 3": shape 9, angle 3), is the Wizards'
//     Tower door's twin: a diagonal door of the ordinary kind, so opening it (open_door) puts its leaf on
//     movecoord(loc, door_open(angle 3, diagonal) = (-1, 0)) = 2504,3256 and sets down an account standing on
//     that tile one tile south (door_open_move_player_out_of_way). The tile the walker would stand on (2505,3256)
//     is the closed door's own, and the leaf blocks 2504,3256, so the way across is the Wizards' one, through
//     the doorway and round the leaf: 2506,3256 -> 2505,3256 -> 2505,3255 -> 2504,3255. Both stands are off the
//     leaf and off the push's landing tile. The TS bot (defs/treegnome/stronghold.ts crossInnerDoor) opens it from
//     2506,3256 and walks to the ladder the same way;
//   - the front door, loc 2184 at 2502,3250 (maps/m39_50.jm2 "0 6 50: 2184 0 1": a straight wall, angle 1, on the
//     north face of its tile), opens from the INSIDE only: [oploc1,khazard_stronghold_door] runs open_and_close_door
//     when coordz(coord) > coordz(loc_coord) and answers "The door is locked from the inside." from the south.
//     Opened from 2502,3251 it sets the account down on 2502,3250, outside. That is the one way out of the
//     stronghold that is not the crumbled wall (which is one way in), so only the outward edge exists: the planner
//     can never route an account INTO the stronghold.
//
// Pinned by khazard_test.go against the real pack and by doorescape's model of the engine's door.
const khazardStrongholdWhy = "Khazard stronghold (Tree Gnome Village): the graph had the ladder and neither door, so every walk from the " +
	"stronghold ended \"unreachable\" and two accounts died upstairs; the inner diagonal door 1530 at 2505,3256 is crossed " +
	"2506,3256 <-> 2504,3255 / 2505,3255 like the Wizards' Tower's, the front door 2184 at 2502,3250 only outward. " +
	"See khazard.go"

// khazardStrongholdEdges are the crossings added to the default graph.
var khazardStrongholdEdges = []ExtraEdge{
	{
		Edge: Edge{
			From: Point{X: 2506, Z: 3256}, To: Point{X: 2504, Z: 3255}, Kind: KindDoor, Cost: 6,
			LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 2505, LocZ: 3256,
		},
		Why: khazardStrongholdWhy,
	},
	{
		Edge: Edge{
			From: Point{X: 2505, Z: 3255}, To: Point{X: 2506, Z: 3256}, Kind: KindDoor, Cost: 6,
			LocName: "Door", Action: "Open", LocID: 1530, OpenLocID: -1, LocX: 2505, LocZ: 3256,
		},
		Why: khazardStrongholdWhy,
	},
	{
		Edge: Edge{
			From: Point{X: 2502, Z: 3251}, To: Point{X: 2502, Z: 3250}, Kind: KindDoor, Cost: 4,
			LocName: "Door", Action: "Open", LocID: 2184, OpenLocID: -1, LocX: 2502, LocZ: 3250,
		},
		Why: khazardStrongholdWhy,
	},
}
