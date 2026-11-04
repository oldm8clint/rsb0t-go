package questplan

import (
	"fmt"
	"math/bits"
	"strings"
	"sync"
)

// Getting out of the Draynor Manor basement when the lever state is not known: a belief over the 64 states, what an inspect,
// a pull and a door each teach about it, the states from which no door leads out, and the next thing to do. Nothing here
// touches a client; questmods/ernestmaze.go carries the decisions out and feeds what the server said back.
//
// Why it exists. The account's belief of the levers (a lever's state is not transmitted: only the messages show it) was a
// single exact state or nothing. Live 19 Oct 2026, two Stage 5 accounts stood in the basement for hours, one in room 3 and
// one in the closet of rooms 1 and 4, their belief taken from the side store of an earlier life: a door the belief said was open
// answered "This door is locked.", the belief was dropped, and with the belief gone the leg "had no way to plan the doors"
// from any room but the ladder's. It logged "stranded ... (levers unknown)" and parked 45 minutes, 25 and 20 times over, while
// the generic pocket escape and the door opener tried a ladder and a locked door they cannot reach by walking.
//
// What the content gives to learn the state by (quest_haunted.rs2):
//
//   - "Inspect" on a lever ([oploc2,_haunted_lever]) answers "The lever is up." or "The lever is down.": one lever's exact state,
//     changing nothing. Only the levers in the room the account stands in can be inspected.
//   - A pull answers the state it left the lever in, so after a pull the lever's bit is exact.
//   - A door that opens teleports the account through, and one that does not answers "This door is locked.": either way the
//     door's rule is known to hold or not in the state the server is in.
//
// A state of the puzzle is a room and the six lever bits. Every pull and every crossing is reversible, so a state reached by
// legal moves from the ladder's room can always walk back to it: of the 448 (room, levers) states 164 have a way to the ladder's
// room and 284 have none (MazeEscapable), and the 284 are not reachable from any state in the ladder's room. An account that
// is in one of them was put there (a teleport, a restart in a sealed room with the wrong bits), and only a teleport takes it
// out. MazeNextProbe finds the way out of every state of the 164 whatever it believes, and says so when every state it could be
// in is one of the 284.

// MazeBelief is the set of lever states (bits A to F, the 64 values of the content's %ernestlever low bits) the server may be
// in: bit n of the set is state n.
type MazeBelief uint64

// MazeAnyLevers is the belief of an account that knows nothing: any of the 64 states.
const MazeAnyLevers MazeBelief = ^MazeBelief(0)

// MazeExactly is the belief that the levers are exactly these bits.
func MazeExactly(bitsOf uint8) MazeBelief { return MazeBelief(1) << (bitsOf & 63) }

// Has reports whether a state is still possible.
func (m MazeBelief) Has(bitsOf uint8) bool { return m>>(bitsOf&63)&1 == 1 }

// Count is the number of states still possible.
func (m MazeBelief) Count() int { return bits.OnesCount64(uint64(m)) }

// Empty reports a belief no state fits: what was seen contradicts what was believed.
func (m MazeBelief) Empty() bool { return m == 0 }

// Exact is the one state the belief holds, and false when it holds none or several.
func (m MazeBelief) Exact() (uint8, bool) {
	if m.Count() != 1 {
		return 0, false
	}
	return uint8(bits.TrailingZeros64(uint64(m))), true
}

// Intersect keeps the states both beliefs hold.
func (m MazeBelief) Intersect(o MazeBelief) MazeBelief { return m & o }

func (m MazeBelief) keep(pred func(bitsOf uint8) bool) MazeBelief {
	var out MazeBelief
	for s := 0; s < 64; s++ {
		if m.Has(uint8(s)) && pred(uint8(s)) {
			out |= MazeBelief(1) << s
		}
	}
	return out
}

// Toggle is the belief after the account pulled a lever whatever its state was: every state with that bit flipped.
func (m MazeBelief) Toggle(lever byte) MazeBelief {
	l, ok := MazeLeverByName(lever)
	if !ok {
		return m
	}
	var out MazeBelief
	for s := 0; s < 64; s++ {
		if m.Has(uint8(s)) {
			out |= MazeBelief(1) << (uint8(s) ^ l.Bit)
		}
	}
	return out
}

// Forget is the belief when it is not known whether a pull landed (its message never came, or named another lever): the lever may
// be as believed or flipped, and the other five stay as they were.
func (m MazeBelief) Forget(lever byte) MazeBelief { return m | m.Toggle(lever) }

// WithLever keeps the states in which the lever is as seen (down is the bit set): an Inspect's answer, or a pull's.
func (m MazeBelief) WithLever(lever byte, down bool) MazeBelief {
	l, ok := MazeLeverByName(lever)
	if !ok {
		return m
	}
	return m.keep(func(b uint8) bool { return (b&l.Bit != 0) == down })
}

// WithDoor keeps the states in which the door opens (a crossing) or does not (a "This door is locked.").
func (m MazeBelief) WithDoor(door string, opened bool) MazeBelief {
	d, ok := MazeDoorByName(door)
	if !ok {
		return m
	}
	return m.keep(func(b uint8) bool { return d.Opens(b) == opened })
}

// LeverKnown reports a lever's state when every state in the belief agrees on it.
func (m MazeBelief) LeverKnown(lever byte) (down, known bool) {
	l, ok := MazeLeverByName(lever)
	if !ok || m.Empty() {
		return false, false
	}
	var seenDown, seenUp bool
	for s := 0; s < 64; s++ {
		if m.Has(uint8(s)) {
			if uint8(s)&l.Bit != 0 {
				seenDown = true
			} else {
				seenUp = true
			}
		}
	}
	return seenDown, seenDown != seenUp
}

// String spells the belief for a log line: the levers as MazeBitsString does when exact, otherwise "A- B? ..." (+ down, - up,
// ? either) and how many states remain.
func (m MazeBelief) String() string {
	if b, ok := m.Exact(); ok {
		return MazeBitsString(b)
	}
	if m.Empty() {
		return "no state fits"
	}
	var parts []string
	for _, l := range MazeLevers {
		mark := "?"
		if down, known := m.LeverKnown(l.Name); known {
			mark = "-"
			if down {
				mark = "+"
			}
		}
		parts = append(parts, string(rune(l.Name))+mark)
	}
	return fmt.Sprintf("%s (%d of 64 states)", strings.Join(parts, " "), m.Count())
}

var (
	escapeOnce sync.Once
	escapeSets map[MazeRoom]MazeBelief
)

// mazeEscapeSet is, per room, the lever states from which doors, levers and nothing else lead to the ladder's room.
func mazeEscapeSet(room MazeRoom) MazeBelief {
	escapeOnce.Do(func() {
		type state = MazeState
		rev := map[state][]state{}
		var all []state
		for _, r := range []MazeRoom{MazeEntry, MazeR14, MazeR2, MazeR3, MazeR5, MazeR6, MazeR9} {
			for b := 0; b < 64; b++ {
				all = append(all, state{Room: r, Bits: uint8(b)})
			}
		}
		for _, s := range all {
			for _, mv := range mazeMoves(s) {
				if next, ok := s.Apply(mv); ok {
					rev[next] = append(rev[next], s)
				}
			}
		}
		good := map[state]bool{}
		var queue []state
		for _, s := range all {
			if s.Room == MazeEntry {
				good[s] = true
				queue = append(queue, s)
			}
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, prev := range rev[cur] {
				if !good[prev] {
					good[prev] = true
					queue = append(queue, prev)
				}
			}
		}
		escapeSets = map[MazeRoom]MazeBelief{}
		for s := range good {
			escapeSets[s.Room] |= MazeBelief(1) << s.Bits
		}
	})
	return escapeSets[room]
}

// MazeEscapable reports whether doors and levers lead from this room with these lever states to the ladder's room. The
// ladder's room always does; a room outside the maze is out already.
func MazeEscapable(room MazeRoom, bitsOf uint8) bool {
	if room == MazeOutside {
		return true
	}
	return mazeEscapeSet(room).Has(bitsOf)
}

// MazeProbeKind is what MazeNextProbe asks the account to do next.
type MazeProbeKind int

const (
	// MazeProbeArrived: the account is in the ladder's room (or out of the maze); nothing is left to find.
	MazeProbeArrived MazeProbeKind = iota
	// MazeProbeInspect: inspect Lever, a lever in this room whose state the belief does not settle.
	MazeProbeInspect
	// MazeProbeMove: carry out Move; every state the belief still takes seriously agrees on its outcome.
	MazeProbeMove
	// MazeProbeDoor: try Move's door from this room; the belief is split on whether it opens, and either answer is learned.
	MazeProbeDoor
	// MazeProbeTrapped: every state the belief holds is one from which no door leads to the ladder's room.
	MazeProbeTrapped
)

// MazeProbe is one step of the search for the way out.
type MazeProbe struct {
	Kind  MazeProbeKind
	Lever byte     // MazeProbeInspect
	Move  MazeMove // MazeProbeMove and MazeProbeDoor (the crossing to try)
}

// String is the probe for a log line.
func (p MazeProbe) String() string {
	switch p.Kind {
	case MazeProbeArrived:
		return "arrived"
	case MazeProbeInspect:
		return fmt.Sprintf("inspect lever %c", p.Lever)
	case MazeProbeMove:
		return p.Move.String()
	case MazeProbeDoor:
		return "try " + p.Move.String()
	}
	return "trapped"
}

// MazeNextProbe is the next thing an account in this room, believing the levers are in one of these states, does to get to the
// ladder's room. It works on the states that have a way there (the others are kept in the belief, so that a contradiction
// shows, but they are not planned for), in this order:
//
//  1. Inspect each lever in the room whose state those states do not agree on: free, and it narrows the belief at once.
//  2. Take the first move of the shortest way out of any of them: a pull always (its outcome is read from the message), a
//     crossing as a Move when they all agree it opens and as a Door trial when they do not.
//
// Each probe either narrows the belief, moves the account along a way out of a state it can still be in, or (a pull) leaves it
// no worse off, since a pull is undone by the next. MazeTrapped is the answer once no state in the belief has a way out.
func MazeNextProbe(room MazeRoom, belief MazeBelief) MazeProbe {
	if room == MazeOutside || room == MazeEntry {
		return MazeProbe{Kind: MazeProbeArrived}
	}
	if belief.Empty() {
		belief = MazeAnyLevers
	}
	live := belief.Intersect(mazeEscapeSet(room))
	if live.Empty() {
		return MazeProbe{Kind: MazeProbeTrapped}
	}
	for _, l := range MazeLevers {
		if l.Room != room {
			continue
		}
		if _, known := live.LeverKnown(l.Name); !known {
			return MazeProbe{Kind: MazeProbeInspect, Lever: l.Name}
		}
	}
	var best []MazeMove
	for s := 0; s < 64; s++ {
		if !live.Has(uint8(s)) {
			continue
		}
		moves, ok := MazeSolve(MazeState{Room: room, Bits: uint8(s)}, MazeEntry)
		if ok && len(moves) > 0 && (best == nil || len(moves) < len(best)) {
			best = moves
		}
	}
	if len(best) == 0 {
		return MazeProbe{Kind: MazeProbeTrapped}
	}
	first := best[0]
	if first.IsPull() {
		return MazeProbe{Kind: MazeProbeMove, Move: first}
	}
	door, _ := MazeDoorByName(first.Door)
	if live.keep(door.Opens) == live {
		return MazeProbe{Kind: MazeProbeMove, Move: first}
	}
	return MazeProbe{Kind: MazeProbeDoor, Move: first}
}

// MazeSearch is one search for the way out: where the account stands, what it believes of the levers, and whether that belief
// has met the server since it was taken from a record.
//
// A belief taken from the side store of an earlier life is a prior, not a fact, and legal play never ends in a state without a way
// out: a prior that is such a state (room 1/4 with C, D and F down, say) is wrong, or the account was carried in. Next gives it up
// for the open belief at once, and only the server's own answers can then bring the verdict that every state left is a dead end.
type MazeSearch struct {
	Room   MazeRoom
	Belief MazeBelief
	// Tested is whether Belief has been answered by the server (a lever's message, an inspect, a door) or reset by the ladder.
	Tested bool
}

// Next is MazeNextProbe on the search's belief, with an untested belief that has no way out replaced by the open belief first.
func (s *MazeSearch) Next() MazeProbe {
	p := MazeNextProbe(s.Room, s.Belief)
	if p.Kind == MazeProbeTrapped && !s.Tested {
		s.Belief, s.Tested = MazeAnyLevers, true
		p = MazeNextProbe(s.Room, s.Belief)
	}
	return p
}

// narrow applies one observation to the belief; an observation the belief contradicts replaces it with every state the
// observation fits, which is all that is known.
func (s *MazeSearch) narrow(filter func(MazeBelief) MazeBelief) {
	s.Belief = filter(s.Belief)
	if s.Belief.Empty() {
		s.Belief = filter(MazeAnyLevers)
	}
	s.Tested = true
}

// SawLever is an inspect's answer, or a pull's after Pulled.
func (s *MazeSearch) SawLever(lever byte, down bool) {
	s.narrow(func(b MazeBelief) MazeBelief { return b.WithLever(lever, down) })
}

// Pulled is a lever pulled, whatever it answered: every state flips that bit.
func (s *MazeSearch) Pulled(lever byte) { s.Belief = s.Belief.Toggle(lever) }

// SawDoor is a door that opened (the account is now Room's other side: set Room too) or answered locked.
func (s *MazeSearch) SawDoor(door string, opened bool) {
	s.narrow(func(b MazeBelief) MazeBelief { return b.WithDoor(door, opened) })
}
