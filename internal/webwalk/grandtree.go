package webwalk

// The Grand Tree's right door, which the walk graph did not have.
//
// The Grand Tree (Tree Gnome Stronghold, maps/m38_54.jm2) has two doors side by side on its south face, "Tree Door" locs
// of width 2: treedoorl (1967) at 2464,3492 and treedoorr (1968) at 2466,3492 ("0 32 36: 1967 10" and
// "0 34 36: 1968 10 2"). Both run [label,open_tree_door] (area_gnome/scripts/gnome_gate.rs2): swap the leaf for its open
// state for six ticks, teleport the player onto the door's column (x 2465 for the left, 2466 for the right) and force-move
// it two tiles through, north from the south side and south from the north. The graph (doors.json) has the left door's two
// edges and neither of the right's.
//
// With one door in the graph every walk into the Grand Tree, Blurberry's bar upstairs among them, went through the left
// door, which passed up to 37 accounts a minute at its busiest. The Barcrawl wave of 16 Oct 2026 (257 accounts, 09:01)
// crowded there for minutes, 3,051 "did not step through Tree Door at 2464,3492" lines against 395 crossings, while the
// right door stood idle, and when the walker's refusal rule asked whether "routing around it leaves 2480,3490,1 unreachable" the answer
// was yes, so the refused door "stays in the plan" however long it refused (noteRefusal). The right door was only ever opened
// by the stall handler's unstick pass ("stalled near shut Tree Door at 2466,3492, opening it", 84 times), and the accounts
// traced were through in seconds: 2466,3491 to 2466,3493 and back, the same two-tile jump as the left door's.
//
// The edges below are that door, both ways, so a walk that gives up on the busy left door goes round by the right one and
// queue.go can send a walk to whichever is the shorter line.
const grandTreeDoorWhy = "Grand Tree right door (treedoorr 1968 at 2466,3492): the graph had only the left door, so a refused or " +
	"crowded left door was 'the only way' into the tree; open_tree_door teleports onto x 2466 and force-moves two tiles, " +
	"2466,3491 <-> 2466,3493. See grandtree.go"

// grandTreeDoorEdges are the right door's crossings added to the default graph.
var grandTreeDoorEdges = []ExtraEdge{
	{
		Edge: Edge{
			From: Point{X: 2466, Z: 3491}, To: Point{X: 2466, Z: 3493}, Kind: KindDoor, Cost: 4,
			LocName: "Tree Door", Action: "Open", LocID: 1968, OpenLocID: 1970, LocX: 2466, LocZ: 3492,
		},
		Why: grandTreeDoorWhy,
	},
	{
		Edge: Edge{
			From: Point{X: 2466, Z: 3493}, To: Point{X: 2466, Z: 3491}, Kind: KindDoor, Cost: 4,
			LocName: "Tree Door", Action: "Open", LocID: 1968, OpenLocID: 1970, LocX: 2466, LocZ: 3492,
		},
		Why: grandTreeDoorWhy,
	},
}
