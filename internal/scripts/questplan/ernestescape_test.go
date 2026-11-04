package questplan

import (
	"strings"
	"testing"
)

// The search for the way out of the basement over every state of the puzzle: a pure solver, so every one of the 448 (room,
// levers) states is checked, not a sample.

var mazeRoomsInOrder = []MazeRoom{MazeEntry, MazeR14, MazeR2, MazeR3, MazeR5, MazeR6, MazeR9}

// simProbeLimit bounds one simulated escape: a search that has not ended by then is a loop.
const simProbeLimit = 200

// simEscape plays MazeNextProbe against a server whose levers are truth, from a room and a belief, the way the live leg does:
// the answer to each probe is the content's own (an inspect reads the bit, a pull toggles it and reads it back, a door
// opens or answers locked), and the belief learns exactly that and nothing else.
func simEscape(room MazeRoom, truth uint8, belief MazeBelief) (end MazeRoom, probes int, trapped bool, trail []string) {
	search := &MazeSearch{Room: room, Belief: belief}
	for probes < simProbeLimit {
		p := search.Next()
		trail = append(trail, p.String())
		switch p.Kind {
		case MazeProbeArrived:
			return search.Room, probes, false, trail
		case MazeProbeTrapped:
			return search.Room, probes, true, trail
		case MazeProbeInspect:
			l, _ := MazeLeverByName(p.Lever)
			search.SawLever(p.Lever, truth&l.Bit != 0)
		case MazeProbeMove, MazeProbeDoor:
			if p.Move.IsPull() {
				l, _ := MazeLeverByName(p.Move.Lever)
				truth ^= l.Bit
				search.Pulled(p.Move.Lever)
				search.SawLever(p.Move.Lever, truth&l.Bit != 0)
				break
			}
			door, _ := MazeDoorByName(p.Move.Door)
			opened := door.Opens(truth)
			if opened {
				search.Room = p.Move.To
			}
			search.SawDoor(door.Name, opened)
		}
		probes++
	}
	return search.Room, probes, false, trail
}

func TestTheEscapableStatesAreTheOnesTheSolverFindsAWayOutOf(t *testing.T) {
	good, bad := 0, 0
	for _, room := range mazeRoomsInOrder {
		for b := 0; b < 64; b++ {
			_, ok := MazeSolve(MazeState{Room: room, Bits: uint8(b)}, MazeEntry)
			if ok != MazeEscapable(room, uint8(b)) {
				t.Errorf("%s %s: solver %v, MazeEscapable %v", room, MazeBitsString(uint8(b)), ok, MazeEscapable(room, uint8(b)))
			}
			if ok {
				good++
			} else {
				bad++
			}
		}
	}
	if good != 164 || bad != 284 {
		t.Errorf("%d states with a way out and %d without, the puzzle model says 164 and 284", good, bad)
	}
	if !MazeEscapable(MazeOutside, 0) {
		t.Error("a tile outside the maze is out already")
	}
}

// Every pull and every crossing is undone by the next, so what legal moves reach from the ladder's room (with any levers there)
// can always walk back to it; the states without a way out are reachable from no state in the ladder's room.
func TestEverythingLegalMovesReachFromTheLaddersRoomCanWalkBack(t *testing.T) {
	for start := 0; start < 64; start++ {
		from := MazeState{Room: MazeEntry, Bits: uint8(start)}
		seen := map[MazeState]bool{from: true}
		queue := []MazeState{from}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if !MazeEscapable(cur.Room, cur.Bits) {
				t.Fatalf("from the ladder's room with %s the moves reach %s in %s, which has no way back", MazeBitsString(uint8(start)), cur.Room, MazeBitsString(cur.Bits))
			}
			for _, mv := range mazeMoves(cur) {
				if next, ok := cur.Apply(mv); ok && !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	// From the content's start (all up) the maze holds 76 states, the number the solver explores for the can.
	seen := map[MazeState]bool{{Room: MazeEntry}: true}
	queue := []MazeState{{Room: MazeEntry}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, mv := range mazeMoves(cur) {
			if next, ok := cur.Apply(mv); ok && !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	if len(seen) != 76 {
		t.Errorf("%d states reachable from the all-up ladder room, want 76", len(seen))
	}
}

func TestABeliefIsNarrowedByWhatTheServerSaid(t *testing.T) {
	if MazeAnyLevers.Count() != 64 || MazeExactly(5).Count() != 1 {
		t.Fatal("counts")
	}
	if b, ok := MazeExactly(LeverA | LeverD).Exact(); !ok || b != LeverA|LeverD {
		t.Errorf("exact = %v %v", b, ok)
	}
	if _, ok := MazeAnyLevers.Exact(); ok {
		t.Error("64 states are not exact")
	}
	b := MazeAnyLevers.WithLever('B', true)
	if b.Count() != 32 || b.Has(0) || !b.Has(LeverB) {
		t.Errorf("B down: %d states", b.Count())
	}
	if down, known := b.LeverKnown('B'); !down || !known {
		t.Errorf("B known = %v %v", down, known)
	}
	if _, known := b.LeverKnown('C'); known {
		t.Error("C is not settled by B")
	}
	// A pull flips the lever in every state, and the message that follows settles it.
	b = MazeExactly(LeverA | LeverC).Toggle('A')
	if got, ok := b.Exact(); !ok || got != LeverC {
		t.Errorf("A pulled from A+ C+ = %v %v", got, ok)
	}
	b = MazeAnyLevers.Toggle('E')
	if b.Count() != 64 {
		t.Errorf("a pull from any state leaves any state, got %d", b.Count())
	}
	// A door that stays locked removes every state it would open in; one that opens keeps only those.
	door, _ := MazeDoorByName("5to6")
	locked := MazeAnyLevers.WithDoor("5to6", false)
	opened := MazeAnyLevers.WithDoor("5to6", true)
	if locked.Count()+opened.Count() != 64 || opened.Count() != 32 {
		t.Errorf("5to6 (D down): %d open, %d locked", opened.Count(), locked.Count())
	}
	for s := 0; s < 64; s++ {
		if opened.Has(uint8(s)) != door.Opens(uint8(s)) {
			t.Fatalf("5to6 in %s: belief and rule disagree", MazeBitsString(uint8(s)))
		}
	}
	if got := MazeAnyLevers.String(); !strings.Contains(got, "A?") || !strings.Contains(got, "64 of 64") {
		t.Errorf("String(any) = %q", got)
	}
	if got := MazeAnyLevers.WithLever('D', true).WithLever('A', false).String(); !strings.Contains(got, "A- ") || !strings.Contains(got, "D+") || !strings.Contains(got, "B?") {
		t.Errorf("String = %q", got)
	}
	if got := MazeExactly(LeverB | LeverF).String(); got != "A- B+ C- D- E- F+" {
		t.Errorf("String(exact) = %q", got)
	}
}

// The way out is found from every state that has one, whatever the account believes: nothing, the right state, or any wrong
// one (a stale record, a swallowed message), from every room.
func TestEveryStateWithAWayOutGetsOutWhateverTheAccountBelieves(t *testing.T) {
	worst, worstDesc, runs := 0, "", 0
	var total int
	for _, room := range mazeRoomsInOrder {
		for truth := 0; truth < 64; truth++ {
			if !MazeEscapable(room, uint8(truth)) {
				continue
			}
			beliefs := map[string]MazeBelief{"nothing": MazeAnyLevers}
			for guess := 0; guess < 64; guess++ {
				beliefs["exactly "+MazeBitsString(uint8(guess))] = MazeExactly(uint8(guess))
			}
			for name, belief := range beliefs {
				end, probes, trapped, trail := simEscape(room, uint8(truth), belief)
				runs++
				total += probes
				if trapped || end != MazeEntry {
					t.Fatalf("%s with %s believing %s: ended in %s (trapped %v) after %d probes: %v", room, MazeBitsString(uint8(truth)), name, end, trapped, probes, trail)
				}
				if probes > worst {
					worst, worstDesc = probes, string(room)+" "+MazeBitsString(uint8(truth))+" believing "+name
				}
			}
		}
	}
	t.Logf("%d escapes, %.1f probes on average, worst %d (%s)", runs, float64(total)/float64(runs), worst, worstDesc)
	if worst > 40 {
		t.Errorf("worst case %d probes: %s", worst, worstDesc)
	}
}

// The states without a way out are told so, after probes that change nothing but the levers, and the account never gets out by
// accident: only a teleport takes it from them.
func TestEveryStateWithoutAWayOutIsReportedTrappedAndNeverLeft(t *testing.T) {
	worst, runs := 0, 0
	for _, room := range mazeRoomsInOrder {
		for truth := 0; truth < 64; truth++ {
			if MazeEscapable(room, uint8(truth)) {
				continue
			}
			beliefs := map[string]MazeBelief{"nothing": MazeAnyLevers, "right": MazeExactly(uint8(truth)), "all up": MazeExactly(0)}
			for name, belief := range beliefs {
				end, probes, trapped, trail := simEscape(room, uint8(truth), belief)
				runs++
				if !trapped || end == MazeEntry {
					t.Fatalf("%s with %s believing %s: ended in %s (trapped %v) after %d probes: %v", room, MazeBitsString(uint8(truth)), name, end, trapped, probes, trail)
				}
				if probes > worst {
					worst = probes
				}
			}
		}
	}
	t.Logf("%d trapped runs, worst %d probes before the verdict", runs, worst)
	if worst > 40 {
		t.Errorf("a trapped account takes %d probes to be told so", worst)
	}
}

// The first probe from a room that has levers is an inspect of one that the belief leaves open, never a walk through a door
// on a guess, and an exact belief with a way out goes straight to the plan.
func TestTheFirstProbeIsAnInspectWhenALeverHereIsOpenAndThePlanWhenTheBeliefIsExact(t *testing.T) {
	p := MazeNextProbe(MazeR3, MazeAnyLevers)
	if p.Kind != MazeProbeInspect || (p.Lever != 'E' && p.Lever != 'F') {
		t.Errorf("room 3 knowing nothing: %s", p)
	}
	// Exactly the state the all-up live account in room 3 would be in after E and F down: the plan is a crossing, determined.
	exact := MazeExactly(LeverD | LeverE | LeverF)
	p = MazeNextProbe(MazeR3, exact)
	if p.Kind != MazeProbeMove {
		t.Errorf("exact belief: %s", p)
	}
	moves, _ := MazeSolve(MazeState{Room: MazeR3, Bits: LeverD | LeverE | LeverF}, MazeEntry)
	if p.Move != moves[0] {
		t.Errorf("exact belief: probe %s, solver's first move %s", p, moves[0])
	}
	if p := MazeNextProbe(MazeEntry, MazeAnyLevers); p.Kind != MazeProbeArrived {
		t.Errorf("the ladder's room: %s", p)
	}
	if p := MazeNextProbe(MazeOutside, MazeAnyLevers); p.Kind != MazeProbeArrived {
		t.Errorf("outside: %s", p)
	}
	// A belief no state fits is treated as knowing nothing, not as trapped.
	if p := MazeNextProbe(MazeR14, 0); p.Kind == MazeProbeTrapped {
		t.Errorf("an empty belief is a contradiction, not a verdict: %s", p)
	}
	// Room 9 holds no lever: the only thing to do is its door, and when its door is known locked the account is trapped.
	if p := MazeNextProbe(MazeR9, MazeAnyLevers); p.Kind != MazeProbeMove || p.Move.Door != "8to9" {
		t.Errorf("room 9 knowing nothing: %s", p)
	}
	if p := MazeNextProbe(MazeR9, MazeAnyLevers.WithDoor("8to9", false)); p.Kind != MazeProbeTrapped {
		t.Errorf("room 9 with its door locked: %s", p)
	}
}

// The two accounts of 19 Oct 2026 and the beliefs they were carrying, replayed: each in the room it stood in, believing what
// its side store said, with every state the server could have been in that has a way out.
func TestTheStrandedAccountsOf19OctGetOutFromTheirRoomsOnTheirStaleBeliefs(t *testing.T) {
	cases := []struct {
		name   string
		room   MazeRoom
		belief uint8
	}{
		// 3097,9763 is room 3's door tile; the store said A- B- C- D+ E+ F- and 3to6 answered locked.
		{"room 3 at the 3to6 door", MazeR3, LeverD | LeverE},
		// 3108,9758 is room 1/4's tile at the 4to7 door; the store said A- B- C+ D+ E- F+.
		{"room 1/4 at the 4to7 door", MazeR14, LeverC | LeverD | LeverF},
	}
	for _, c := range cases {
		door := "3to6"
		if c.room == MazeR14 {
			door = "4to7"
		}
		reached, trappedTruths := 0, 0
		for truth := 0; truth < 64; truth++ {
			// The state the server is in is not the one believed: the believed door was locked.
			d, _ := MazeDoorByName(door)
			if d.Opens(uint8(truth)) {
				continue
			}
			end, _, trapped, trail := simEscape(c.room, uint8(truth), MazeExactly(c.belief))
			switch {
			case MazeEscapable(c.room, uint8(truth)):
				if trapped || end != MazeEntry {
					t.Errorf("%s with the server in %s: ended in %s: %v", c.name, MazeBitsString(uint8(truth)), end, trail)
				}
				reached++
			default:
				if !trapped {
					t.Errorf("%s with the server in %s (no way out): not told trapped: %v", c.name, MazeBitsString(uint8(truth)), trail)
				}
				trappedTruths++
			}
		}
		t.Logf("%s: out from %d servers states, trapped on %d", c.name, reached, trappedTruths)
		if reached == 0 {
			t.Errorf("%s: no state with a way out was tried", c.name)
		}
	}
}
