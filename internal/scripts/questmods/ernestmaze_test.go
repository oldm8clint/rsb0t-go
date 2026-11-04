package questmods

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The maze legs' orchestration (the reset cycle, the belief of the levers, the passes, the way out, a restart in the middle)
// against a simulation that plays the content's own rules for every op the legs send, over the same puzzle model the plan
// solves. What it cannot show is the live ops themselves (the stands, the dialogue, the timing); the real-data tests check
// those against the pack, the loc index and the production config.

type simMsg struct {
	at   time.Time
	text string
}

// simMaze is the server of the Draynor Manor's alcove and basement for one account.
type simMaze struct {
	t    *testing.T
	here tile.Tile
	bits uint8 // the server's %ernestlever

	can        int  // oil cans in the pack
	canOnFloor bool // the can lies in room 9
	canAfter   int  // GroundItems calls that find the floor empty before the can is back

	msgs []simMsg
	log  []string

	deaf      bool        // no chat log
	drop      int         // pull messages to swallow
	lag       int         // pull messages to deliver late: with the next op of any kind, not with the pull
	afterPull func(n int) // called after the n-th pull took effect

	walkFails int // walks to refuse before they work

	teleport int // 0 nothing to teleport with, 1 a spell (the levers reset), 2 a rub of jewellery (they do not)

	pending []string // messages that arrive with the next op

	ascents, descents, pulls, crossings, locked, dropped, inspects, teleports, ops int
	events                                                                         []questplan.MazeMove
}

func newSimMaze(t *testing.T) *simMaze {
	return &simMaze{t: t, here: tile.Tile{X: 3098, Z: 3358}, canOnFloor: true}
}

func (s *simMaze) region(t tile.Tile) string {
	switch {
	case questplan.InErnestAlcove(t):
		return "alcove"
	case questplan.InErnestCloset(t):
		return "closet"
	case questplan.MazeRoomAt(t) != questplan.MazeOutside:
		return "maze:" + string(questplan.MazeRoomAt(t))
	}
	return "outside"
}

func (s *simMaze) TeleportOut() bool {
	if s.teleport == 0 {
		return false
	}
	if s.teleport == 1 {
		s.bits = 0 // ~post_tele_checks
	}
	s.here = tile.Tile{X: 3222, Z: 3218}
	s.teleports++
	return true
}

func (s *simMaze) say(text string) {
	ts := time.Now()
	s.msgs = append(s.msgs, simMsg{ts, text})
	for !time.Now().After(ts) { // the next mark is strictly after this message
	}
}

func (s *simMaze) Here() tile.Tile { return s.here }
func (s *simMaze) Walk(to tile.Tile, radius int) bool {
	if s.walkFails > 0 {
		s.walkFails--
		return false
	}
	if s.region(to) != s.region(s.here) {
		return false
	}
	s.here = to
	return true
}
func (s *simMaze) Travel(to tile.Tile, radius int) bool {
	if s.region(s.here) != "outside" || s.region(to) != "outside" {
		return false
	}
	s.here = to
	return true
}
func (s *simMaze) Said(since time.Time) string {
	var out []string
	for _, m := range s.msgs {
		if !m.at.Before(since) {
			out = append(out, m.text)
		}
	}
	return strings.Join(out, "\n")
}
func (s *simMaze) HasChat() bool { return !s.deaf }
func (s *simMaze) Wait(_ time.Duration, cond func() bool) bool {
	for i := 0; i < 3; i++ {
		if cond() {
			return true
		}
	}
	return false
}
func (s *simMaze) Delay(time.Duration) bool { return true }
func (s *simMaze) Log(format string, args ...any) {
	s.log = append(s.log, fmt.Sprintf(format, args...))
}
func (s *simMaze) Held(id int) bool { return id == questplan.ObjOilCan && s.can > 0 }
func (s *simMaze) GroundItems(id int, at tile.Tile, r int) int {
	if id != questplan.ObjOilCan || !s.canOnFloor {
		return 0
	}
	if s.canAfter > 0 {
		s.canAfter--
		return 0
	}
	return 1
}
func (s *simMaze) TakeGround(id int, at tile.Tile, r int) bool {
	if id != questplan.ObjOilCan || !s.canOnFloor || s.region(s.here) != "maze:r9" {
		return false
	}
	s.canOnFloor = false
	s.can++
	return true
}

func (s *simMaze) serverState() questplan.MazeState {
	return questplan.MazeState{Room: questplan.MazeRoomAt(s.here), Bits: s.bits}
}

// Op plays the content's script for the op, when the account stands where the engine accepts it; an op from anywhere else is
// dropped with no reply, as clientRoutefinder drops it (and so is true here: the module sent it).
func (s *simMaze) Op(locID int, near tile.Tile, op string) bool {
	here := s.here
	s.ops++
	for _, m := range s.pending {
		s.say(m)
	}
	s.pending = nil
	switch {
	case locID >= 146 && locID <= 151 && op == "Inspect":
		for _, l := range questplan.MazeLevers {
			if l.LocID != locID {
				continue
			}
			if here != l.Tile || near != l.Tile {
				s.dropped++
				return true
			}
			s.inspects++
			if !s.deaf {
				s.say("The lever is " + map[bool]string{true: "down", false: "up"}[s.bits&l.Bit != 0] + ".")
			}
		}
	case locID >= 146 && locID <= 151 && op == "Pull":
		for _, l := range questplan.MazeLevers {
			if l.LocID != locID {
				continue
			}
			if here != l.Tile || near != l.Tile {
				s.dropped++
				return true
			}
			s.bits ^= l.Bit
			s.pulls++
			down := s.bits&l.Bit != 0
			s.events = append(s.events, questplan.MazeMove{Lever: l.Name, Down: down, From: l.Room, To: l.Room})
			msg := fmt.Sprintf("You pull lever %c %s.", l.Name, map[bool]string{true: "down", false: "up"}[down])
			switch {
			case s.drop > 0:
				s.drop--
			case s.lag > 0:
				s.lag--
				s.pending = append(s.pending, msg, "You hear a clunk.")
			case !s.deaf:
				s.say(msg)
				s.say("You hear a clunk.")
			}
			if s.afterPull != nil {
				s.afterPull(s.pulls)
			}
		}
	case locID >= 137 && locID <= 145 && op == "Open":
		for _, d := range questplan.MazeDoors {
			if d.LocID != locID {
				continue
			}
			if here != d.Near && here != d.Far {
				s.dropped++
				return true
			}
			if !d.Opens(s.bits) {
				s.locked++
				s.say("This door is locked.")
				return true
			}
			from := questplan.MazeRoomAt(here)
			to, _ := d.Other(from)
			if here == d.Near {
				s.here = d.Far
			} else {
				s.here = d.Near
			}
			s.crossings++
			s.events = append(s.events, questplan.MazeMove{Door: d.Name, From: from, To: to})
		}
	case locID == questplan.LocPuzzleLadderDown && op == "Climb-down":
		if !questplan.InErnestAlcove(here) || tile.Chebyshev(here, near) > 1 {
			s.dropped++
			return true
		}
		s.here = questplan.ErnestBasementLanding
		s.descents++
	case locID == questplan.LocPuzzleLadderUp && op == "Climb-up":
		if questplan.MazeRoomAt(here) != questplan.MazeEntry || tile.Chebyshev(here, near) > 1 {
			s.dropped++
			return true
		}
		s.bits = 0 // ~reset_haunted_levers
		s.here = tile.Tile{X: 3092, Z: 3361}
		s.ascents++
	case (locID == questplan.LocManorBookcaseL || locID == questplan.LocManorBookcaseR) && op == "Search":
		if s.region(here) != "outside" || here.X < near.X+1 || tile.Chebyshev(here, near) > 1 {
			s.dropped++ // [oploc1,_manor_bookcase]: nothing from the west side
			return true
		}
		s.here = tile.Tile{X: 3096, Z: near.Z}
	case locID == questplan.LocAlcoveLever && op == "Pull":
		if !questplan.InErnestAlcove(here) || here != questplan.ErnestAlcoveLever {
			s.dropped++
			return true
		}
		s.here = tile.Tile{X: 3098, Z: 3358}
	default:
		s.t.Fatalf("the simulation has no rule for loc %d %q", locID, op)
	}
	return true
}

// replayEvents plays the server's own record of what happened through the puzzle model: every pull and crossing was legal.
func (s *simMaze) replayEvents() {
	s.t.Helper()
	st := questplan.MazeState{Room: questplan.MazeEntry}
	for i, ev := range s.events {
		if !ev.IsPull() && ev.From == questplan.MazeOutside {
			continue
		}
		// An ascent resets the levers; the events before it are a separate visit.
		next, ok := st.Apply(ev)
		if !ok {
			// A new visit starts in the entry room with the levers all up.
			st = questplan.MazeState{Room: questplan.MazeEntry}
			next, ok = st.Apply(ev)
		}
		if !ok {
			s.t.Fatalf("event %d (%s) was not legal from %v %s", i, ev, st.Room, questplan.MazeBitsString(st.Bits))
		}
		st = next
	}
}

func (s *simMaze) hasLog(sub string) bool {
	for _, l := range s.log {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func fastCanWait(t *testing.T) {
	old := mazeCanWait
	mazeCanWait = 30 * time.Millisecond
	t.Cleanup(func() { mazeCanWait = old })
}

func TestTheMazeLegFetchesTheCanAndLeavesTheLeversReset(t *testing.T) {
	s := newSimMaze(t)
	r := newMazeRun(nil, s, &mazeMemory{})
	ok, stranded := r.fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.can != 1 || s.region(s.here) != "outside" || s.here != (tile.Tile{X: 3098, Z: 3358}) {
		t.Errorf("can %d, ended at %v (%s)", s.can, s.here, s.region(s.here))
	}
	// 9 pulls and 14 crossings are the 23 moves; the account's belief of the levers was unknown, so it climbed up and down
	// once before it began (the way down resets nothing) and up once to leave.
	if s.pulls != 9 || s.crossings != 15 || s.locked != 0 || s.ascents != 2 || s.descents != 2 || s.dropped != 0 {
		t.Errorf("pulls %d crossings %d (14 to the can and 1 back) locked %d ascents %d descents %d dropped %d", s.pulls, s.crossings, s.locked, s.ascents, s.descents, s.dropped)
	}
	if s.bits != 0 {
		t.Errorf("the levers were left %s: the ladder up must reset them", questplan.MazeBitsString(s.bits))
	}
	if bits, known := r.mem.get(); bits != 0 || !known {
		t.Errorf("belief %s known %v after the climb out", questplan.MazeBitsString(bits), known)
	}
	s.replayEvents()
	for _, want := range []string{"through the bookcase into the alcove", "down the ladder", "not known to be all up", "23 moves from entry to r9", "oil can is in the pack", "the alcove's lever"} {
		if !s.hasLog(want) {
			t.Errorf("the log has no %q:\n%s", want, strings.Join(s.log, "\n"))
		}
	}
}

func TestAnAllUpBeliefSkipsTheResetCycle(t *testing.T) {
	s := newSimMaze(t)
	mem := &mazeMemory{}
	mem.set(0, true)
	ok, stranded := newMazeRun(nil, s, mem).fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.ascents != 1 || s.descents != 1 {
		t.Errorf("ascents %d descents %d, want one of each (down to begin, up to leave)", s.ascents, s.descents)
	}
}

// The way down does not reset the levers, and a death does not either: an account that comes back with the levers where it
// left them climbs up and down before it trusts them.
func TestLeversLeftWhereADeadAccountLeftThemAreResetBeforeTheMazeIsWalked(t *testing.T) {
	s := newSimMaze(t)
	s.bits = questplan.LeverB | questplan.LeverD | questplan.LeverF
	mem := &mazeMemory{}
	mem.set(s.bits, true)
	ok, stranded := newMazeRun(nil, s, mem).fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.ascents != 2 || s.locked != 0 {
		t.Errorf("ascents %d locked %d: the dirty levers were not reset first", s.ascents, s.locked)
	}
	// The same account with nothing remembered at all behaves the same.
	s = newSimMaze(t)
	s.bits = questplan.LeverA | questplan.LeverC
	if ok, _ := newMazeRun(nil, s, &mazeMemory{}).fetchCan(); !ok || s.locked != 0 {
		t.Errorf("unknown memory and dirty levers: ok %v locked %d", ok, s.locked)
	}
}

// A pull is answered with the state it left the lever in: a swallowed message is answered by the next pull's, and the belief
// is whatever the last message said.
func TestAMissedLeverMessageIsAnsweredByTheNextPull(t *testing.T) {
	s := newSimMaze(t)
	s.drop = 1
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.locked != 0 || s.pulls != 9 || s.inspects != 1 {
		t.Errorf("locked %d pulls %d inspects %d (9 pulls, and one inspect that read the lever whose message was swallowed)", s.locked, s.pulls, s.inspects)
	}
	s.replayEvents()
}

// A message that comes late is not the answer to the next pull: the lever is read back with an inspect before it is pulled again,
// so a lever is never toggled twice by one wish, and the belief never goes the wrong way round.
func TestALateLeverMessageIsNotTakenForTheAnswerToTheNextPull(t *testing.T) {
	s := newSimMaze(t)
	s.lag = 2
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.locked != 0 || s.pulls != 9 {
		t.Errorf("locked %d pulls %d: a late message was answered with a second pull", s.locked, s.pulls)
	}
	s.replayEvents()
}

func TestWithoutAChatLogEveryPullIsTakenToHaveToggled(t *testing.T) {
	s := newSimMaze(t)
	s.deaf = true
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" || s.locked != 0 || s.pulls != 9 {
		t.Fatalf("ok %v stranded %q locked %d pulls %d:\n%s", ok, stranded, s.locked, s.pulls, strings.Join(s.log, "\n"))
	}
}

// The belief is kept in the quest's side store as the account pulls, so a restart in the middle of the maze (the process gone,
// the memory with it) takes up where it left off, and the ladder up clears it.
func TestARestartInTheMiddleOfTheMazeResumesFromTheStoredBelief(t *testing.T) {
	s := newSimMaze(t)
	e := &questrunner.Engine{Store: questplan.NewStore()}
	saves := 0
	e.Save = func() { saves++ }
	r := newMazeRun(e, s, &mazeMemory{})
	if !r.enter() {
		t.Fatalf("enter failed:\n%s", strings.Join(s.log, "\n"))
	}
	moves, _ := questplan.MazeSolve(r.state(), questplan.MazeR9)
	for _, mv := range moves[:12] {
		if !r.play(mv) {
			t.Fatalf("move %s failed", mv)
		}
	}
	rec := e.Store.Record(questplan.ErnestChickenID)
	if !rec.Hints[mazeHintIn] {
		t.Fatal("the store does not say the account is in the maze")
	}
	for _, l := range questplan.MazeLevers {
		if rec.Hints[mazeHintKey(l.Name)] != (s.bits&l.Bit != 0) {
			t.Errorf("hint for lever %c is %v, the server has %v", l.Name, rec.Hints[mazeHintKey(l.Name)], s.bits&l.Bit != 0)
		}
	}
	if saves == 0 {
		t.Error("the store was never saved")
	}
	roomBefore := s.serverState()
	if roomBefore.Room == questplan.MazeEntry && roomBefore.Bits == 0 {
		t.Fatal("twelve moves in and the account is back at the start: a poor test")
	}

	// The process restarts: a new memory, the same store, the account where it was.
	s.log = nil
	r2 := newMazeRun(e, s, &mazeMemory{})
	if bits, known := r2.mem.get(); !known || bits != s.bits {
		t.Fatalf("the restarted run believes %s (known %v), the server has %s", questplan.MazeBitsString(bits), known, questplan.MazeBitsString(s.bits))
	}
	ok, stranded := r2.fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if !s.hasLog("back in the maze after a restart") {
		t.Errorf("the log does not say the run took the belief from the store:\n%s", strings.Join(s.log, "\n"))
	}
	if s.locked != 0 {
		t.Errorf("%d locked doors after the restart", s.locked)
	}
	// The climb out cleared the hints.
	if rec.Hints[mazeHintIn] {
		t.Error("the store still says the account is in the maze after the ladder up")
	}
	for _, l := range questplan.MazeLevers {
		if rec.Hints[mazeHintKey(l.Name)] {
			t.Errorf("lever %c is still down in the store", l.Name)
		}
	}
}

// With nothing remembered, in a room that is not the ladder's, the account does not give up (the leg used to say "levers
// unknown: no way to plan the doors" and park for 45 minutes): it inspects the levers in its room and tries the doors the
// belief is split on until the doors lead out, and takes the can from there.
func TestARestartInTheMazeWithNothingRememberedInspectsAndTriesDoorsUntilItIsOut(t *testing.T) {
	s := newSimMaze(t)
	s.here = tile.Tile{X: 3102, Z: 9760} // room 5
	s.bits = questplan.LeverA | questplan.LeverB | questplan.LeverD
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" || s.can != 1 {
		t.Fatalf("ok %v stranded %q can %d:\n%s", ok, stranded, s.can, strings.Join(s.log, "\n"))
	}
	if s.teleports != 0 {
		t.Errorf("%d teleports: the doors lead out of this state", s.teleports)
	}
	// The same account in the ladder's room knows the way out whatever the levers are.
	s = newSimMaze(t)
	s.here = questplan.ErnestBasementLanding
	s.bits = questplan.LeverC | questplan.LeverE
	if ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan(); !ok || stranded != "" || s.locked != 0 {
		t.Errorf("from the ladder's room: ok %v stranded %q locked %d:\n%s", ok, stranded, s.locked, strings.Join(s.log, "\n"))
	}
}

// The two accounts of 19 Oct 2026, replayed: one stood on room 3's door tile (3097,9763) with a record of A- B- C- D+ E+ F- and
// the 3to6 door locked, the other on the 4to7 door's tile in rooms 1 and 4 (3108,9758) with a record of A- B- C+ D+ E- F+ (a state
// with no way out, so the record was wrong). Whatever the server's levers were, if any door led out, each gets out: every
// state the server could have been in that has a way out is tried, with the stale record in the store and with nothing.
func TestTheStrandedAccountsOf19OctGetOutOnTheirStaleRecordsFromEveryServerState(t *testing.T) {
	cases := []struct {
		name  string
		at    tile.Tile
		room  questplan.MazeRoom
		stale uint8
	}{
		{"room 3 at the 3to6 door", tile.Tile{X: 3097, Z: 9763}, questplan.MazeR3, questplan.LeverD | questplan.LeverE},
		{"room 1/4 at the 4to7 door", tile.Tile{X: 3108, Z: 9758}, questplan.MazeR14, questplan.LeverC | questplan.LeverD | questplan.LeverF},
	}
	for _, c := range cases {
		for _, withRecord := range []bool{true, false} {
			out, trapped := 0, 0
			for truth := 0; truth < 64; truth++ {
				s := newSimMaze(t)
				s.here, s.bits = c.at, uint8(truth)
				e := &questrunner.Engine{Store: questplan.NewStore()}
				if withRecord {
					rec := e.Store.Record(questplan.ErnestChickenID)
					rec.Hints = map[string]bool{mazeHintIn: true}
					for _, l := range questplan.MazeLevers {
						rec.Hints[mazeHintKey(l.Name)] = c.stale&l.Bit != 0
					}
				}
				r := newMazeRun(e, s, &mazeMemory{})
				left := r.leaveMaze() // the pocket's way out, asked for before every walk
				if questplan.MazeEscapable(c.room, uint8(truth)) {
					if !left || s.region(s.here) != "outside" || s.teleports != 0 {
						t.Errorf("%s (record %v), server %s: left %v at %v (%s), %d teleports:\n%s", c.name, withRecord, questplan.MazeBitsString(uint8(truth)), left, s.here, s.region(s.here), s.teleports, strings.Join(s.log, "\n"))
					}
					out++
					continue
				}
				if left || !r.trapped || s.teleports != 0 {
					t.Errorf("%s (record %v), server %s has no way out: left %v trapped %v teleports %d", c.name, withRecord, questplan.MazeBitsString(uint8(truth)), left, r.trapped, s.teleports)
				}
				trapped++
			}
			t.Logf("%s (record %v): out of %d server states, trapped on %d", c.name, withRecord, out, trapped)
		}
	}
}

// Every room, every lever state with a way out, every belief the account might carry: the leg ends outside the basement and the
// alcove with the can, the levers reset, no door tried more than the search needs and no teleport used. That is Ernest's own run
// leaving the basement before it ends.
func TestTheLegLeavesTheBasementFromEveryRoomAndEveryServerStateWithAWayOut(t *testing.T) {
	rooms := map[questplan.MazeRoom]tile.Tile{}
	for _, l := range questplan.MazeLevers {
		rooms[l.Room] = l.Tile
	}
	for _, d := range questplan.MazeDoors {
		rooms[d.NearRoom], rooms[d.FarRoom] = d.Near, d.Far
	}
	runs := 0
	for room, at := range rooms {
		for truth := 0; truth < 64; truth++ {
			if !questplan.MazeEscapable(room, uint8(truth)) {
				continue
			}
			for _, belief := range []questplan.MazeBelief{questplan.MazeAnyLevers, questplan.MazeExactly(0), questplan.MazeExactly(uint8(63 - truth)), questplan.MazeExactly(uint8(truth))} {
				s := newSimMaze(t)
				s.here, s.bits = at, uint8(truth)
				mem := &mazeMemory{}
				mem.setBelief(belief, false)
				ok, stranded := newMazeRun(nil, s, mem).fetchCan()
				runs++
				if !ok || stranded != "" || s.can != 1 || s.region(s.here) != "outside" || s.bits != 0 || s.teleports != 0 {
					t.Fatalf("%s %s believing %s: ok %v stranded %q can %d at %v levers %s teleports %d:\n%s", room, questplan.MazeBitsString(uint8(truth)), belief, ok, stranded, s.can, s.here,
						questplan.MazeBitsString(s.bits), s.teleports, strings.Join(s.log, "\n"))
				}
			}
		}
	}
	t.Logf("%d legs from inside the maze all came out with the can", runs)
}

// A state with no way out is told so by the search, which pulls and inspects but opens no door that is not on a way out. With
// nothing to teleport with the account stays and says why; the verdict is kept so the pocket's way out, asked before every walk,
// does not click levers again for ten minutes; with a spell (the levers reset) or a rub of jewellery (they do not) it leaves.
func TestAnAccountInAStateWithNoWayOutIsTeleportedOutOrToldWhyItCannotLeave(t *testing.T) {
	newTrapped := func() *simMaze {
		s := newSimMaze(t)
		s.here = tile.Tile{X: 3108, Z: 9758} // room 1/4, every lever up
		return s
	}
	s := newTrapped()
	r := newMazeRun(nil, s, &mazeMemory{})
	if r.leaveMaze() || !r.trapped {
		t.Fatalf("left %v trapped %v:\n%s", false, r.trapped, strings.Join(s.log, "\n"))
	}
	// It stands where the inspects left it (the levers of its room), still inside, and says why it is not leaving.
	if got := r.strandedAt(); !strings.Contains(got, "stranded in the Draynor Manor basement at 3") || !strings.Contains(got, "dead end") || !strings.Contains(got, "nothing it carries teleports") {
		t.Errorf("strandedAt = %q", got)
	}
	if s.crossings != 0 || s.teleports != 0 {
		t.Errorf("crossings %d teleports %d", s.crossings, s.teleports)
	}
	opsBefore := s.ops
	r2 := newMazeRun(nil, s, r.mem)
	if r2.leaveMaze() || !r2.trapped || s.ops != opsBefore {
		t.Errorf("the second ask: trapped %v, %d more ops (the verdict is kept for the tile)", r2.trapped, s.ops-opsBefore)
	}
	for _, kind := range []int{1, 2} {
		s = newTrapped()
		s.teleport = kind
		r = newMazeRun(nil, s, &mazeMemory{})
		if !r.leaveMaze() || s.region(s.here) != "outside" || s.teleports != 1 {
			t.Errorf("teleport kind %d: at %v (%s) after %d teleports:\n%s", kind, s.here, s.region(s.here), s.teleports, strings.Join(s.log, "\n"))
		}
		if b, _ := r.mem.beliefOf(); b != questplan.MazeAnyLevers {
			t.Errorf("teleport kind %d: belief %s afterwards, nothing is known of the levers", kind, b)
		}
		// Back at the manor it fetches the can as any account does: the descent resets the levers first.
		if ok, stranded := newMazeRun(nil, s, r.mem).fetchCan(); !ok || stranded != "" || s.can != 1 || s.bits != 0 {
			t.Errorf("teleport kind %d: after it, ok %v stranded %q can %d levers %s", kind, ok, stranded, s.can, questplan.MazeBitsString(s.bits))
		}
	}
	// A leg that could not teleport leaves the quest alone for the long park; one that failed for another reason, the short one.
	if mazeParkFor(true) != mazeStrandedPark || mazeParkFor(false) != mazeRetryPark {
		t.Errorf("parks: trapped %s, not %s", mazeParkFor(true), mazeParkFor(false))
	}
}

// A record from an earlier life that names a state with no way out is not believed: legal play never ends there. The search
// starts again from what the server answers.
func TestARecordNamingAStateWithNoWayOutIsNotBelieved(t *testing.T) {
	s := newSimMaze(t)
	s.here = tile.Tile{X: 3102, Z: 9760} // room 5, A+ B+ D+ in the server: the way out is 5to8
	s.bits = questplan.LeverA | questplan.LeverB | questplan.LeverD
	e := &questrunner.Engine{Store: questplan.NewStore()}
	rec := e.Store.Record(questplan.ErnestChickenID)
	rec.Hints = map[string]bool{mazeHintIn: true} // every lever up in room 5: no door leads out
	r := newMazeRun(e, s, &mazeMemory{})
	if !r.leaveMaze() || s.region(s.here) != "outside" || s.teleports != 0 {
		t.Errorf("at %v (%s):\n%s", s.here, s.region(s.here), strings.Join(s.log, "\n"))
	}
	if !s.hasLog("not trusting them") {
		t.Errorf("the log does not say the record was given up:\n%s", strings.Join(s.log, "\n"))
	}
}

// A belief that is wrong (a teleport spell reset the levers behind the module's back) is found out by the first door that
// stays locked; the account is in the ladder's room still, climbs out, and the second pass goes through.
func TestAWrongBeliefIsFoundOutAtTheFirstLockedDoorAndTheNextPassMendsIt(t *testing.T) {
	s := newSimMaze(t)
	s.afterPull = func(n int) {
		if n == 2 { // both entry levers down, then the world resets under the module
			s.bits = 0
		}
	}
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	if s.locked != 1 {
		t.Errorf("%d locked doors, want the one that told the module its belief was wrong", s.locked)
	}
	if !s.hasLog("the belief is wrong") || !s.hasLog("pass 1 of 3 did not reach the can") {
		t.Errorf("the log:\n%s", strings.Join(s.log, "\n"))
	}
}

// The can is one ground spawn that comes back a minute after another account takes it: the leg waits for it, and a can that never
// comes ends the pass (three of them) with the account out of the maze and no stranding.
func TestTheCanAnotherAccountTookIsWaitedFor(t *testing.T) {
	fastCanWait(t)
	s := newSimMaze(t)
	s.canAfter = 3
	if ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan(); !ok || stranded != "" {
		t.Fatalf("ok %v stranded %q:\n%s", ok, stranded, strings.Join(s.log, "\n"))
	}
	s = newSimMaze(t)
	s.canOnFloor = false
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if ok || stranded != "" {
		t.Fatalf("a can that never comes: ok %v stranded %q", ok, stranded)
	}
	if s.region(s.here) != "outside" || s.bits != 0 {
		t.Errorf("the account ended at %v (%s) with levers %s: it must be out and reset", s.here, s.region(s.here), questplan.MazeBitsString(s.bits))
	}
	if !s.hasLog("no oil can on the floor of room 9") || !s.hasLog("pass 3 of 3 did not reach the can") {
		t.Errorf("the log:\n%s", strings.Join(s.log, "\n"))
	}
}

// An account that has the can already, wherever the maze left it, is taken out the same way.
func TestAnAccountHoldingTheCanIsTakenOut(t *testing.T) {
	for _, from := range []tile.Tile{questplan.ErnestBasementLanding, questplan.ErnestAlcoveLever, {X: 3092, Z: 3361}} {
		s := newSimMaze(t)
		s.here = from
		s.can = 1
		ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
		if !ok || stranded != "" || s.region(s.here) != "outside" {
			t.Errorf("from %v: ok %v stranded %q at %v:\n%s", from, ok, stranded, s.here, strings.Join(s.log, "\n"))
		}
	}
	// And one that holds it and stands in the manor does nothing.
	s := newSimMaze(t)
	s.can = 1
	if ok, _ := newMazeRun(nil, s, &mazeMemory{}).fetchCan(); !ok || s.pulls != 0 || s.descents != 0 {
		t.Errorf("an account outside with the can: ok %v pulls %d descents %d", ok, s.pulls, s.descents)
	}
}

// A bookcase that is not stood east of does nothing: the leg stands east of it first.
func TestTheBookcaseIsOnlyOperatedFromItsEastSide(t *testing.T) {
	s := newSimMaze(t)
	s.here = tile.Tile{X: 3090, Z: 3358} // west of the alcove's wall, in the manor's garden
	r := newMazeRun(nil, s, &mazeMemory{})
	if !r.intoAlcove() || s.dropped != 0 || !questplan.InErnestAlcove(s.here) {
		t.Errorf("into the alcove from the west: dropped %d at %v:\n%s", s.dropped, s.here, strings.Join(s.log, "\n"))
	}
	// The alcove's pocket: the lever takes the account out, and a second call is a no-op.
	if !r.leaveAlcove() || questplan.InErnestAlcove(s.here) || !r.leaveAlcove() {
		t.Errorf("out of the alcove: at %v", s.here)
	}
}

func TestStrandedAtNamesTheLeverBeliefOrSaysItIsUnknown(t *testing.T) {
	s := newSimMaze(t)
	s.here = tile.Tile{X: 3108, Z: 9760}
	mem := &mazeMemory{}
	r := newMazeRun(nil, s, mem)
	if got := r.strandedAt(); !strings.Contains(got, "3108,9760,0") || !strings.Contains(got, "unknown") {
		t.Errorf("strandedAt = %q", got)
	}
	mem.set(questplan.LeverA|questplan.LeverB, true)
	if got := r.strandedAt(); !strings.Contains(got, "A+ B+") {
		t.Errorf("strandedAt = %q", got)
	}
	s.here = tile.Tile{X: 3098, Z: 3358}
	if got := r.strandedAt(); got != "" {
		t.Errorf("strandedAt outside = %q", got)
	}
	s.here = questplan.ErnestAlcoveLever
	if got := r.strandedAt(); got == "" {
		t.Error("an account in the alcove is not out")
	}
}

// A leg that ends inside the basement because a walk failed once is tried again (twice) before it is reported stranded and
// parked, so a hiccup does not leave the account in the maze for the length of a park; one that keeps failing is reported, with
// where it stands.
func TestALegThatEndsInsideTriesTheWayOutAgainBeforeItParks(t *testing.T) {
	s := newSimMaze(t)
	s.here = tile.Tile{X: 3102, Z: 9760} // room 5
	s.bits = questplan.LeverA | questplan.LeverB | questplan.LeverD
	s.can = 1 // the can is in the pack: the leg only has to get out
	s.walkFails = 1
	ok, stranded := newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if !ok || stranded != "" || s.region(s.here) != "outside" {
		t.Fatalf("one failed walk: ok %v stranded %q at %v:\n%s", ok, stranded, s.here, strings.Join(s.log, "\n"))
	}
	if !s.hasLog("another try at the way out (1 of 2)") {
		t.Errorf("the log does not show the second try:\n%s", strings.Join(s.log, "\n"))
	}
	s = newSimMaze(t)
	s.here = tile.Tile{X: 3102, Z: 9760}
	s.bits = questplan.LeverA | questplan.LeverB | questplan.LeverD
	s.can = 1
	s.walkFails = 1000
	ok, stranded = newMazeRun(nil, s, &mazeMemory{}).fetchCan()
	if ok || !strings.Contains(stranded, "stranded in the Draynor Manor basement at 3102,9760,0") || !strings.Contains(stranded, "no way out the module can plan") {
		t.Fatalf("a walk that never works: ok %v stranded %q", ok, stranded)
	}
	if mazeParkFor(false) != mazeRetryPark {
		t.Error("a failure that is not a verdict parks for the short wait")
	}
}
