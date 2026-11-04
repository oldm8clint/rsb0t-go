package questplan

import (
	"fmt"
	"regexp"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The Draynor Manor basement of Ernest the Chicken, as a puzzle the plan can solve and a test can check against the
// content. Nothing here touches a client.
//
// What the content does (quests/quest_haunted/scripts/quest_haunted.rs2):
//
//   - Nine doors (loc types 1to2 ... 5to8, category haunted_door) cut the basement into seven rooms. A door opens, by
//     teleporting the account through it and shutting it behind (open_and_close_door2), only while the account's six
//     levers A to F, the bits 0 to 5 of the perm varp %ernestlever, satisfy that door's rule ([oploc1,_haunted_door]);
//     otherwise "This door is locked." The rules are transcribed into mazeDoors below, one per door.
//   - Six levers (levera ... leverf, category haunted_lever) toggle their bit. [oploc1,_haunted_lever] answers "You pull
//     lever A down." with the state the pull LEFT the lever in, and [oploc2,_haunted_lever] ("Inspect") answers "The lever
//     is up." The bit is a non-transmitted varp: the messages are the only way to read it.
//   - The bits are cleared by climbing the basement's ladder UP ([oploc1,puzzle_ladder]) and by any teleport spell
//     ([proc,post_tele_checks]), and by nothing else: not by the way down (puzzle_ladder_top), not by a death, not by a
//     logout. The TS def says "every descent resets the levers"; it does not, and an account that left the maze by
//     dying finds the levers where it left them.
//   - The oil can lies on the floor of the last room, 9, behind 8to9.
//
// The seven rooms and the nine doors between them were found by flooding the baked collision pack with every door shut
// (TestTheMazeRoomsAreTheCollisionPacksComponents does it again, and so checks this table): each room is exactly one
// component, and the doors' two sides lie in the rooms this file says. The shortest way from the ladder to the can is 23
// moves, found by breadth-first search (MazeSolve) and equal to the 23 of the TS def's hand-derived chain.

// MazeRoom is one of the basement's seven rooms, or MazeOutside for anywhere else.
type MazeRoom string

// The rooms. Rooms 1 and 4 of the content's door names share a room (no door lies between them), and so do 7 and 8,
// which are the ladder's hall.
const (
	MazeOutside MazeRoom = "outside"
	MazeEntry   MazeRoom = "entry" // the ladder's hall, rooms 7 and 8; levers A and B
	MazeR14     MazeRoom = "r1r4"  // rooms 1 and 4; levers C and D
	MazeR2      MazeRoom = "r2"
	MazeR3      MazeRoom = "r3" // levers E and F
	MazeR5      MazeRoom = "r5"
	MazeR6      MazeRoom = "r6"
	MazeR9      MazeRoom = "r9" // the oil can
)

// mazeBox is a room's tile rectangle on level 0. The boxes are pairwise disjoint and cover every walkable tile of the
// basement (checked against the pack).
type mazeBox struct {
	Room                   MazeRoom
	MinX, MaxX, MinZ, MaxZ int
}

var mazeBoxes = []mazeBox{
	{MazeEntry, 3100, 3118, 9745, 9757},
	{MazeR14, 3105, 3112, 9758, 9767},
	{MazeR2, 3100, 3104, 9763, 9767},
	{MazeR5, 3100, 3104, 9758, 9762},
	{MazeR3, 3096, 3099, 9763, 9767},
	{MazeR6, 3096, 3099, 9758, 9762},
	{MazeR9, 3090, 3099, 9753, 9757},
}

// MazeRoomAt is the basement room a tile lies in, MazeOutside for any other tile.
func MazeRoomAt(t tile.Tile) MazeRoom {
	if t.Level != 0 {
		return MazeOutside
	}
	for _, b := range mazeBoxes {
		if t.X >= b.MinX && t.X <= b.MaxX && t.Z >= b.MinZ && t.Z <= b.MaxZ {
			return b.Room
		}
	}
	return MazeOutside
}

// InErnestBasement reports a tile in the maze.
func InErnestBasement(t tile.Tile) bool { return MazeRoomAt(t) != MazeOutside }

// The levers as bits of %ernestlever: a set bit means the lever is DOWN.
const (
	LeverA uint8 = 1 << iota
	LeverB
	LeverC
	LeverD
	LeverE
	LeverF
)

// MazeLever is one lever: where it is (a wall decoration, operated from its own tile), its loc type and its room.
type MazeLever struct {
	Name  byte
	Bit   uint8
	LocID int
	Tile  tile.Tile
	Room  MazeRoom
}

// MazeLevers are the six levers (maps/m48_152.jm2, pack/loc.pack levera ... leverf).
var MazeLevers = []MazeLever{
	{'A', LeverA, 146, tile.Tile{X: 3108, Z: 9745}, MazeEntry},
	{'B', LeverB, 147, tile.Tile{X: 3118, Z: 9752}, MazeEntry},
	{'C', LeverC, 148, tile.Tile{X: 3112, Z: 9760}, MazeR14},
	{'D', LeverD, 149, tile.Tile{X: 3108, Z: 9767}, MazeR14},
	{'E', LeverE, 150, tile.Tile{X: 3097, Z: 9767}, MazeR3},
	{'F', LeverF, 151, tile.Tile{X: 3096, Z: 9765}, MazeR3},
}

// MazeLeverByName is the lever called 'A' to 'F'.
func MazeLeverByName(name byte) (MazeLever, bool) {
	for _, l := range MazeLevers {
		if l.Name == name {
			return l, true
		}
	}
	return MazeLever{}, false
}

// doorRule is one way a door opens: every bit of Down set (lever down) and every bit of Up clear (lever up).
type doorRule struct{ Down, Up uint8 }

// MazeDoor is one puzzle door: its loc type, the tile it stands on (its own tile, "near") and the tile across its edge
// ("far"), the rooms either side, and the rule that opens it. The account crosses it from either side by operating it
// ("Open") from the tile it stands on.
type MazeDoor struct {
	Name              string
	LocID             int
	Near, Far         tile.Tile
	NearRoom, FarRoom MazeRoom
	rules             []doorRule
}

// Opens reports whether the door lets an account through with the levers in these bits.
func (d MazeDoor) Opens(bits uint8) bool {
	for _, r := range d.rules {
		if bits&r.Down == r.Down && bits&r.Up == 0 {
			return true
		}
	}
	return false
}

// Other is the room across the door from room, and false for a room the door does not touch.
func (d MazeDoor) Other(room MazeRoom) (MazeRoom, bool) {
	switch room {
	case d.NearRoom:
		return d.FarRoom, true
	case d.FarRoom:
		return d.NearRoom, true
	}
	return "", false
}

// Side is the tile to stand on to cross the door from room, and the tile it lands on.
func (d MazeDoor) Side(room MazeRoom) (stand, arrive tile.Tile, ok bool) {
	switch room {
	case d.NearRoom:
		return d.Near, d.Far, true
	case d.FarRoom:
		return d.Far, d.Near, true
	}
	return tile.Tile{}, tile.Tile{}, false
}

// MazeDoors are the nine doors, with the rules of [oploc1,_haunted_door] (LeverA ... LeverF are its testbit indexes
// 0 to 5), their positions from maps/m48_152.jm2 and the rooms they join.
var MazeDoors = []MazeDoor{
	{"1to2", 137, tile.Tile{X: 3105, Z: 9765}, tile.Tile{X: 3104, Z: 9765}, MazeR14, MazeR2,
		[]doorRule{{Down: LeverD | LeverE | LeverF, Up: LeverA | LeverB}}},
	{"2to3", 138, tile.Tile{X: 3100, Z: 9765}, tile.Tile{X: 3099, Z: 9765}, MazeR2, MazeR3,
		[]doorRule{{Down: LeverD | LeverF, Up: LeverB}}},
	{"4to5", 139, tile.Tile{X: 3105, Z: 9760}, tile.Tile{X: 3104, Z: 9760}, MazeR14, MazeR5,
		[]doorRule{{Down: LeverA | LeverB | LeverD}}},
	{"5to6", 140, tile.Tile{X: 3100, Z: 9760}, tile.Tile{X: 3099, Z: 9760}, MazeR5, MazeR6,
		[]doorRule{{Down: LeverD}}},
	{"8to9", 141, tile.Tile{X: 3100, Z: 9755}, tile.Tile{X: 3099, Z: 9755}, MazeEntry, MazeR9,
		[]doorRule{{Down: LeverF, Up: LeverE}}},
	{"2to5", 142, tile.Tile{X: 3102, Z: 9763}, tile.Tile{X: 3102, Z: 9762}, MazeR2, MazeR5,
		[]doorRule{{Down: LeverC | LeverD | LeverF, Up: LeverA | LeverB | LeverE}}},
	{"3to6", 143, tile.Tile{X: 3097, Z: 9763}, tile.Tile{X: 3097, Z: 9762}, MazeR3, MazeR6,
		[]doorRule{{Down: LeverD, Up: LeverB | LeverF}}},
	{"4to7", 144, tile.Tile{X: 3108, Z: 9758}, tile.Tile{X: 3108, Z: 9757}, MazeR14, MazeEntry,
		[]doorRule{{Down: LeverA | LeverB, Up: LeverC | LeverD | LeverE | LeverF}}},
	{"5to8", 145, tile.Tile{X: 3102, Z: 9758}, tile.Tile{X: 3102, Z: 9757}, MazeR5, MazeEntry,
		[]doorRule{{Down: LeverD, Up: LeverC}, {Down: LeverC | LeverD | LeverF, Up: LeverA | LeverB | LeverE}}},
}

// MazeDoorByName is the door called "1to2" ... "5to8".
func MazeDoorByName(name string) (MazeDoor, bool) {
	for _, d := range MazeDoors {
		if d.Name == name {
			return d, true
		}
	}
	return MazeDoor{}, false
}

// MazeState is where the account is in the maze and what it believes the levers are.
type MazeState struct {
	Room MazeRoom
	Bits uint8
}

// MazeMove is one step of the puzzle: pull a lever (to the Down or the up state) or cross a door.
type MazeMove struct {
	// Lever is the lever pulled ('A' to 'F'), 0 for a crossing; Down is the state it is left in.
	Lever byte
	Down  bool
	// Door is the door crossed ("1to2" ...), From and To the rooms either side of the crossing (both the lever's room
	// for a pull).
	Door     string
	From, To MazeRoom
}

// IsPull reports a lever pull.
func (m MazeMove) IsPull() bool { return m.Lever != 0 }

// String is the move for a log line.
func (m MazeMove) String() string {
	if m.IsPull() {
		state := "up"
		if m.Down {
			state = "down"
		}
		return fmt.Sprintf("pull lever %c %s", m.Lever, state)
	}
	return fmt.Sprintf("cross %s %s->%s", m.Door, m.From, m.To)
}

// Apply plays a move from this state, and reports false when it is not legal there: a lever in another room, a pull that
// would leave the lever as it is, a door that is not in this room or does not open with these bits.
func (s MazeState) Apply(m MazeMove) (MazeState, bool) {
	if m.IsPull() {
		l, ok := MazeLeverByName(m.Lever)
		if !ok || l.Room != s.Room || (s.Bits&l.Bit != 0) == m.Down {
			return s, false
		}
		s.Bits ^= l.Bit
		return s, true
	}
	d, ok := MazeDoorByName(m.Door)
	if !ok || !d.Opens(s.Bits) {
		return s, false
	}
	to, ok := d.Other(s.Room)
	if !ok || to != m.To || s.Room != m.From {
		return s, false
	}
	s.Room = to
	return s, true
}

// mazeMoves lists the legal moves out of a state, in a fixed order (levers A to F, then the doors in table order), so
// the search is deterministic.
func mazeMoves(s MazeState) []MazeMove {
	var out []MazeMove
	for _, l := range MazeLevers {
		if l.Room == s.Room {
			out = append(out, MazeMove{Lever: l.Name, Down: s.Bits&l.Bit == 0, From: s.Room, To: s.Room})
		}
	}
	for _, d := range MazeDoors {
		if to, ok := d.Other(s.Room); ok && d.Opens(s.Bits) {
			out = append(out, MazeMove{Door: d.Name, From: s.Room, To: to})
		}
	}
	return out
}

// MazeSolve is the shortest sequence of moves that takes the account from this state into the goal room, found by
// breadth-first search over the 7 rooms and 64 lever states. ok is false when no sequence exists (a state the puzzle's
// rules never reach from the ladder cannot always get out, but every state reachable from the ladder can).
func MazeSolve(from MazeState, goal MazeRoom) (moves []MazeMove, ok bool) {
	if from.Room == goal {
		return nil, true
	}
	type edge struct {
		prev MazeState
		move MazeMove
	}
	seen := map[MazeState]edge{from: {}}
	queue := []MazeState{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, m := range mazeMoves(cur) {
			next, legal := cur.Apply(m)
			if !legal {
				continue
			}
			if _, dup := seen[next]; dup {
				continue
			}
			seen[next] = edge{prev: cur, move: m}
			if next.Room == goal {
				for s := next; s != from; s = seen[s].prev {
					moves = append(moves, seen[s].move)
				}
				for i, j := 0, len(moves)-1; i < j; i, j = i+1, j-1 {
					moves[i], moves[j] = moves[j], moves[i]
				}
				return moves, true
			}
			queue = append(queue, next)
		}
	}
	return nil, false
}

// MazeBitsString spells the lever states for a log line: "A- B+ ..." with + for down.
func MazeBitsString(bits uint8) string {
	var parts []string
	for _, l := range MazeLevers {
		mark := "-"
		if bits&l.Bit != 0 {
			mark = "+"
		}
		parts = append(parts, string(rune(l.Name))+mark)
	}
	return strings.Join(parts, " ")
}

var (
	leverPulledRe    = regexp.MustCompile(`you pull lever ([a-f]) (up|down)\.?`)
	leverInspectedRe = regexp.MustCompile(`the lever is (up|down)\.?`)
)

// MazeLeverSaid reads the lever a pull message names and the state it left it in ("You pull lever A down."), the last such
// message in text winning (the log since a mark may hold two). ok is false when the text holds none.
func MazeLeverSaid(text string) (lever byte, down bool, ok bool) {
	all := leverPulledRe.FindAllStringSubmatch(strings.ToLower(text), -1)
	if len(all) == 0 {
		return 0, false, false
	}
	last := all[len(all)-1]
	return last[1][0] - 'a' + 'A', last[2] == "down", true
}

// MazeLeverInspected reads an Inspect answer ("The lever is down."): the state of the lever just inspected.
func MazeLeverInspected(text string) (down bool, ok bool) {
	all := leverInspectedRe.FindAllStringSubmatch(strings.ToLower(text), -1)
	if len(all) == 0 {
		return false, false
	}
	return all[len(all)-1][1] == "down", true
}

// MazeDoorLocked reports the refusal a door that does not open answers with.
func MazeDoorLocked(text string) bool {
	return strings.Contains(strings.ToLower(text), "this door is locked")
}
