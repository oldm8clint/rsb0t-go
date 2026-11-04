package questplan

import (
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the rooms ----

func TestTheMazeBoxesAreDisjointAndHoldEveryLeverAndDoorSide(t *testing.T) {
	for i, a := range mazeBoxes {
		for _, b := range mazeBoxes[i+1:] {
			if a.MinX <= b.MaxX && b.MinX <= a.MaxX && a.MinZ <= b.MaxZ && b.MinZ <= a.MaxZ {
				t.Errorf("the boxes of %s and %s overlap", a.Room, b.Room)
			}
		}
	}
	for _, l := range MazeLevers {
		if got := MazeRoomAt(l.Tile); got != l.Room {
			t.Errorf("lever %c at %v lies in %s, the table says %s", l.Name, l.Tile, got, l.Room)
		}
	}
	for _, d := range MazeDoors {
		if got := MazeRoomAt(d.Near); got != d.NearRoom {
			t.Errorf("door %s: its own tile %v lies in %s, the table says %s", d.Name, d.Near, got, d.NearRoom)
		}
		if got := MazeRoomAt(d.Far); got != d.FarRoom {
			t.Errorf("door %s: the tile across %v lies in %s, the table says %s", d.Name, d.Far, got, d.FarRoom)
		}
		if d.NearRoom == d.FarRoom {
			t.Errorf("door %s joins %s to itself", d.Name, d.NearRoom)
		}
		if tile.Chebyshev(d.Near, d.Far) != 1 || (d.Near.X != d.Far.X && d.Near.Z != d.Far.Z) {
			t.Errorf("door %s: %v and %v are not orthogonal neighbours", d.Name, d.Near, d.Far)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want MazeRoom
	}{
		{ErnestBasementLanding, MazeEntry}, {ErnestLadderUpStand, MazeEntry}, {ErnestOilCanSpawn, MazeR9},
		{tile.Tile{X: 3108, Z: 9760}, MazeR14}, {tile.Tile{X: 3102, Z: 9765}, MazeR2}, {tile.Tile{X: 3102, Z: 9760}, MazeR5},
		{tile.Tile{X: 3097, Z: 9760}, MazeR6}, {ErnestBookcaseStand, MazeOutside}, {ErnestAlcoveLever, MazeOutside},
		{tile.Tile{X: 3116, Z: 9754, Level: 1}, MazeOutside}, {tile.Tile{X: 3116, Z: 9754 - 20}, MazeOutside},
	} {
		if got := MazeRoomAt(c.at); got != c.want {
			t.Errorf("MazeRoomAt(%v) = %s, want %s", c.at, got, c.want)
		}
	}
	if InErnestBasement(tile.Tile{X: 3098, Z: 3358}) || !InErnestBasement(ErnestBasementLanding) {
		t.Error("InErnestBasement is not MazeRoomAt != outside")
	}
}

func TestThePocketBoxesHoldTheirStandsAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		name string
		got  bool
		want bool
	}{
		{"closet inside tile", InErnestCloset(ErnestClosetInside), true},
		{"closet tube", InErnestCloset(ErnestTubeSpawn), true},
		{"closet outside tile", InErnestCloset(ErnestClosetOutside), false},
		{"closet upstairs", InErnestCloset(tile.Tile{X: 3110, Z: 3367, Level: 1}), false},
		{"alcove lever", InErnestAlcove(ErnestAlcoveLever), true},
		{"alcove ladder stand", InErnestAlcove(ErnestLadderDownStand), true},
		{"alcove landings", InErnestAlcove(tile.Tile{X: 3096, Z: 3358}) && InErnestAlcove(tile.Tile{X: 3096, Z: 3359}), true},
		{"east of the bookcases", InErnestAlcove(ErnestBookcaseStand), false},
		{"alcove ladder-up landing", InErnestAlcove(tile.Tile{X: 3092, Z: 3361}), true},
	} {
		if c.got != c.want {
			t.Errorf("%s: %v, want %v", c.name, c.got, c.want)
		}
	}
}

// ---- the doors ----

// A door opens exactly when its rule holds, over all 64 states of the levers; spot-check the rules the content states.
func TestTheDoorRulesOpenAsTheContentSays(t *testing.T) {
	door := func(name string) MazeDoor {
		d, ok := MazeDoorByName(name)
		if !ok {
			t.Fatalf("no door %s", name)
		}
		return d
	}
	// 5to6 is lever D alone.
	for bits := uint8(0); bits < 64; bits++ {
		if got := door("5to6").Opens(bits); got != (bits&LeverD != 0) {
			t.Errorf("5to6 with %s: %v", MazeBitsString(bits), got)
		}
	}
	// 4to7 wants A and B down and C, D, E, F up: exactly one state.
	n := 0
	for bits := uint8(0); bits < 64; bits++ {
		if door("4to7").Opens(bits) {
			n++
			if bits != LeverA|LeverB {
				t.Errorf("4to7 opens with %s", MazeBitsString(bits))
			}
		}
	}
	if n != 1 {
		t.Errorf("4to7 opens in %d states, want 1", n)
	}
	// 5to8 has two ways in: D down with C up, or C D F down with A B E up.
	for bits := uint8(0); bits < 64; bits++ {
		want := (bits&LeverC == 0 && bits&LeverD != 0) ||
			(bits&(LeverA|LeverB) == 0 && bits&LeverC != 0 && bits&LeverD != 0 && bits&LeverE == 0 && bits&LeverF != 0)
		if got := door("5to8").Opens(bits); got != want {
			t.Errorf("5to8 with %s: %v, want %v", MazeBitsString(bits), got, want)
		}
	}
	// 8to9 wants F down and E up.
	if !door("8to9").Opens(LeverF) || door("8to9").Opens(LeverE|LeverF) || door("8to9").Opens(0) {
		t.Error("8to9 is not F down, E up")
	}
	if len(MazeDoors) != 9 || len(MazeLevers) != 6 {
		t.Errorf("%d doors and %d levers, the content has 9 and 6", len(MazeDoors), len(MazeLevers))
	}
	seen := map[int]bool{}
	for _, d := range MazeDoors {
		if seen[d.LocID] || d.LocID < 137 || d.LocID > 145 {
			t.Errorf("door %s has loc id %d", d.Name, d.LocID)
		}
		seen[d.LocID] = true
	}
}

// ---- the puzzle ----

// replay plays moves from a state, failing the test on an illegal one.
func replay(t *testing.T, from MazeState, moves []MazeMove) MazeState {
	t.Helper()
	s := from
	for i, mv := range moves {
		next, ok := s.Apply(mv)
		if !ok {
			t.Fatalf("move %d (%s) is not legal from %v %s", i, mv, s.Room, MazeBitsString(s.Bits))
		}
		s = next
	}
	return s
}

func TestTheShortestWayToTheOilCanIsTwentyThreeMoves(t *testing.T) {
	start := MazeState{Room: MazeEntry}
	moves, ok := MazeSolve(start, MazeR9)
	if !ok {
		t.Fatal("no way to room 9")
	}
	if len(moves) != 23 {
		t.Errorf("%d moves, want 23:\n%v", len(moves), moves)
	}
	end := replay(t, start, moves)
	if end.Room != MazeR9 {
		t.Errorf("the moves end in %s", end.Room)
	}
	// Breadth-first search finds the shortest: nothing shorter exists (no 22-move sequence reaches room 9).
	short := 0
	seen := map[MazeState]int{start: 0}
	queue := []MazeState{start}
	for len(queue) > 0 && short == 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, mv := range mazeMoves(cur) {
			next, ok := cur.Apply(mv)
			if !ok {
				t.Fatalf("mazeMoves offered %s from %v, which Apply refuses", mv, cur)
			}
			if _, dup := seen[next]; dup {
				continue
			}
			seen[next] = seen[cur] + 1
			if next.Room == MazeR9 {
				short = seen[next]
				break
			}
			queue = append(queue, next)
		}
	}
	if short != 23 {
		t.Errorf("an independent search reaches room 9 in %d moves, want 23", short)
	}
}

// The TS def's hand-derived chain (defs/ernest/basement.ts CHAIN, from tools/nav/ernest-basement-derive.ts) is a legal 23 move
// sequence from the all-up ladder room to room 9 under the rules transcribed here, so the two derivations agree.
func TestTheTSChainIsLegalAndTheSameLength(t *testing.T) {
	pull := func(lever byte, down bool, room MazeRoom) MazeMove {
		return MazeMove{Lever: lever, Down: down, From: room, To: room}
	}
	cross := func(door string, from, to MazeRoom) MazeMove { return MazeMove{Door: door, From: from, To: to} }
	chain := []MazeMove{
		pull('B', true, MazeEntry), pull('A', true, MazeEntry), cross("4to7", MazeEntry, MazeR14),
		pull('D', true, MazeR14), cross("4to5", MazeR14, MazeR5), cross("5to8", MazeR5, MazeEntry),
		pull('B', false, MazeEntry), pull('A', false, MazeEntry), cross("5to8", MazeEntry, MazeR5),
		cross("5to6", MazeR5, MazeR6), cross("3to6", MazeR6, MazeR3),
		pull('F', true, MazeR3), pull('E', true, MazeR3),
		cross("2to3", MazeR3, MazeR2), cross("1to2", MazeR2, MazeR14),
		pull('C', true, MazeR14),
		cross("1to2", MazeR14, MazeR2), cross("2to3", MazeR2, MazeR3),
		pull('E', false, MazeR3),
		cross("2to3", MazeR3, MazeR2), cross("2to5", MazeR2, MazeR5), cross("5to8", MazeR5, MazeEntry), cross("8to9", MazeEntry, MazeR9),
	}
	if len(chain) != 23 {
		t.Fatalf("the transcribed chain has %d moves", len(chain))
	}
	end := replay(t, MazeState{Room: MazeEntry}, chain)
	if end.Room != MazeR9 {
		t.Errorf("the chain ends in %s", end.Room)
	}
	// Room 9's door back: the levers the chain leaves open it, so the way out is a single crossing.
	back, ok := MazeSolve(end, MazeEntry)
	if !ok || len(back) != 1 || back[0].Door != "8to9" {
		t.Errorf("the way back from room 9 is %v (ok %v), want the one crossing of 8to9", back, ok)
	}
}

// From every state the puzzle can be in, the ladder room can be reached again: the account is never trapped by its own pulls,
// so the way out of a leg that failed half way is always a plan, never a hope. (It is the property that lets the module
// replan from the believed state instead of replaying its moves backwards.)
func TestEveryReachableMazeStateHasAWayBackToTheLadder(t *testing.T) {
	start := MazeState{Room: MazeEntry}
	seen := map[MazeState]bool{start: true}
	queue := []MazeState{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, mv := range mazeMoves(cur) {
			next, _ := cur.Apply(mv)
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	rooms := map[MazeRoom]bool{}
	for s := range seen {
		rooms[s.Room] = true
		if _, ok := MazeSolve(s, MazeEntry); !ok {
			t.Errorf("no way back to the ladder from %s with levers %s", s.Room, MazeBitsString(s.Bits))
		}
	}
	if len(rooms) != 7 {
		t.Errorf("%d rooms reachable from the ladder, want all 7", len(rooms))
	}
	t.Logf("%d (room, lever) states are reachable from the ladder and every one gets back", len(seen))
}

func TestMazeApplyRefusesWhatTheContentRefuses(t *testing.T) {
	at := MazeState{Room: MazeEntry}
	for _, c := range []struct {
		name string
		from MazeState
		mv   MazeMove
	}{
		{"a lever in another room", at, MazeMove{Lever: 'C', Down: true, From: MazeEntry, To: MazeEntry}},
		{"a pull that leaves a lever as it is", at, MazeMove{Lever: 'A', Down: false, From: MazeEntry, To: MazeEntry}},
		{"a locked door", at, MazeMove{Door: "4to7", From: MazeEntry, To: MazeR14}},
		{"a door that opens but is not in this room", MazeState{Room: MazeEntry, Bits: LeverD | LeverE | LeverF}, MazeMove{Door: "1to2", From: MazeEntry, To: MazeR2}},
		{"a door to the wrong room", MazeState{Room: MazeEntry, Bits: LeverF}, MazeMove{Door: "8to9", From: MazeEntry, To: MazeR14}},
		{"a door from the wrong room", MazeState{Room: MazeEntry, Bits: LeverF}, MazeMove{Door: "8to9", From: MazeR9, To: MazeEntry}},
		{"a lever that does not exist", at, MazeMove{Lever: 'G', Down: true, From: MazeEntry, To: MazeEntry}},
		{"a door that does not exist", at, MazeMove{Door: "9to9", From: MazeEntry, To: MazeR9}},
	} {
		if _, ok := c.from.Apply(c.mv); ok {
			t.Errorf("%s was legal", c.name)
		}
	}
	if next, ok := at.Apply(MazeMove{Lever: 'A', Down: true, From: MazeEntry, To: MazeEntry}); !ok || next.Bits != LeverA {
		t.Errorf("pulling A down: %v %v", next, ok)
	}
	if next, ok := (MazeState{Room: MazeEntry, Bits: LeverF}).Apply(MazeMove{Door: "8to9", From: MazeEntry, To: MazeR9}); !ok || next.Room != MazeR9 {
		t.Errorf("crossing 8to9 with F down: %v %v", next, ok)
	}
	if _, ok := MazeSolve(MazeState{Room: MazeR9}, MazeR9); !ok {
		t.Error("the goal room is already reached")
	}
	if moves, ok := MazeSolve(MazeState{Room: MazeEntry, Bits: 0}, MazeEntry); !ok || len(moves) != 0 {
		t.Errorf("already in the goal room: %v %v", moves, ok)
	}
}

// ---- the messages ----

func TestTheLeverMessagesAreRead(t *testing.T) {
	for _, c := range []struct {
		text  string
		lever byte
		down  bool
		ok    bool
	}{
		{"You pull lever A down.", 'A', true, true},
		{"You pull lever F up.", 'F', false, true},
		{"You pull lever C down.\nYou hear a clunk.", 'C', true, true},
		{"you pull lever b up. You hear a clunk. You pull lever D down.", 'D', true, true},
		{"Nothing interesting happens.", 0, false, false},
		{"", 0, false, false},
		{"You pull lever G down.", 0, false, false},
	} {
		lever, down, ok := MazeLeverSaid(c.text)
		if lever != c.lever || down != c.down || ok != c.ok {
			t.Errorf("%q read as %c %v %v, want %c %v %v", c.text, lever, down, ok, c.lever, c.down, c.ok)
		}
	}
	if down, ok := MazeLeverInspected("The lever is down."); !ok || !down {
		t.Errorf("an inspected lever that is down: %v %v", down, ok)
	}
	if down, ok := MazeLeverInspected("The lever is up."); !ok || down {
		t.Errorf("an inspected lever that is up: %v %v", down, ok)
	}
	if _, ok := MazeLeverInspected("You pull lever A down."); ok {
		t.Error("a pull is not an inspection")
	}
	if !MazeDoorLocked("This door is locked.") || MazeDoorLocked("You pull lever A down.") {
		t.Error("the locked-door refusal is not read")
	}
	if s := MazeBitsString(LeverA | LeverD); !strings.HasPrefix(s, "A+ B- C- D+") {
		t.Errorf("MazeBitsString = %q", s)
	}
	if _, ok := MazeLeverByName('Z'); ok {
		t.Error("lever Z exists")
	}
	if _, ok := MazeDoorByName("9to9"); ok {
		t.Error("door 9to9 exists")
	}
}

func TestADoorKnowsItsOtherSide(t *testing.T) {
	d, _ := MazeDoorByName("1to2")
	if to, ok := d.Other(MazeR14); !ok || to != MazeR2 {
		t.Errorf("Other(r1r4) = %s %v", to, ok)
	}
	if to, ok := d.Other(MazeR2); !ok || to != MazeR14 {
		t.Errorf("Other(r2) = %s %v", to, ok)
	}
	if _, ok := d.Other(MazeR9); ok {
		t.Error("room 9 is not beside 1to2")
	}
	stand, arrive, ok := d.Side(MazeR2)
	if !ok || !slices.Equal([]tile.Tile{stand, arrive}, []tile.Tile{d.Far, d.Near}) {
		t.Errorf("Side(r2) = %v %v %v", stand, arrive, ok)
	}
	if _, _, ok := d.Side(MazeOutside); ok {
		t.Error("outside is not beside 1to2")
	}
}
