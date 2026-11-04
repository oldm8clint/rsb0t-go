package questmods

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The live half of Ernest the Chicken's maze (questplan/ernestmaze.go has the puzzle): getting into the basement through the
// bookcase and the ladder, pulling levers and crossing doors in the order MazeSolve gives, taking the oil can and getting
// out again. What the legs rely on, from the content (quest_haunted.rs2):
//
//   - The bookcases teleport an account standing EAST of them into the alcove ("You've found a secret door!"); the
//     alcove's lever teleports it back out to (3098,3358). Neither is a walk, so each is an op the module sends itself and
//     the pockets (ernest.go's init) have it for any other quest's walk that starts there.
//   - The ladder down lands in the maze's entry room; the ladder up (the only thing that resets the levers, with a
//     teleport spell) lands in the alcove.
//   - A lever answers with the state it left itself in; a door that does not open answers "This door is locked."; a door
//     that opens teleports the account through. The levers' bits are not transmitted: the account's belief of them (mazeMemory)
//     is built from those messages alone, and kept in the quest's side store (Hints) so a restart in the middle of the maze
//     finds the way out.
//
// The belief is a set of states, not one: an account that finds a door locked that its belief said was open, or that stands in
// a room with nothing remembered (a restarted process, a record of an earlier life), does not give up. It inspects the levers in
// its room (the content answers "The lever is up." and changes nothing), tries the doors the belief is split on, and narrows
// the belief by what the server says until the doors lead it to the ladder's room (questplan.MazeNextProbe, exercised against
// every state of the puzzle). Legal play never ends in a state without a way out, so the search finds one from wherever the
// server is; an account that is in a state without one (it was carried in) is told so by the search, and leaves by the
// teleport it carries (a spell it can pay for, a Games necklace or Ring of dueling rubbed: randomevents.EscapeTeleport).
//
// Everything the legs do to the game goes through a mazeWorld, so that the orchestration (the reset cycle, the belief, the
// passes, the way out, a restart in the middle) is tested against a simulation that plays the content's rules over the same
// puzzle model, where the live world cannot be had without a server.

// mazeWorld is what the maze legs do to the game. The live one is liveMaze; a test plays the content's rules over it.
type mazeWorld interface {
	// Here is the account's tile.
	Here() tile.Tile
	// Walk is a walk inside one room, the alcove or the closet: the plain local walker. Travel is the long walk.
	Walk(to tile.Tile, radius int) bool
	Travel(to tile.Tile, radius int) bool
	// Op finds the loc of type locID near a tile and sends op on it from a stand the engine accepts.
	Op(locID int, near tile.Tile, op string) bool
	// Said is the game messages since a mark ("" without a chat log, which HasChat reports).
	Said(since time.Time) string
	HasChat() bool
	// Wait polls cond until it holds or the timeout; Delay sleeps. Both report false when the bot is stopping.
	Wait(timeout time.Duration, cond func() bool) bool
	Delay(d time.Duration) bool
	// Log writes a line to the account's log.
	Log(format string, args ...any)
	// TeleportOut leaves the maze by the teleport the account carries (a spell it can pay for, else a rub of its jewellery)
	// and reports whether it stands outside the basement afterwards. False for an account with no teleport.
	TeleportOut() bool
	// Held counts an object in the pack; GroundItems counts an object on the ground near a tile; TakeGround picks it up.
	Held(id int) bool
	GroundItems(id int, at tile.Tile, radius int) int
	TakeGround(id int, at tile.Tile, radius int) bool
}

// liveMaze is the mazeWorld over a live account.
type liveMaze struct{ d *questrunner.Deps }

func (w liveMaze) Here() tile.Tile                          { return questrunner.CurrentTile(w.d) }
func (w liveMaze) Walk(to tile.Tile, radius int) bool       { return questrunner.WalkNear(w.d, to, radius) }
func (w liveMaze) Travel(to tile.Tile, radius int) bool     { return questrunner.Travel(w.d, to, radius) }
func (w liveMaze) Said(since time.Time) string              { return chatSince(w.d, since) }
func (w liveMaze) HasChat() bool                            { return w.d.Chat != nil }
func (w liveMaze) Wait(t time.Duration, c func() bool) bool { return waitUntil(w.d, t, c) }
func (w liveMaze) Delay(d time.Duration) bool               { return w.d.Delay(d) }
func (w liveMaze) Log(format string, args ...any)           { logf(w.d, format, args...) }
func (w liveMaze) Held(id int) bool                         { return held(w.d, id) }
func (w liveMaze) GroundItems(id int, at tile.Tile, r int) int {
	return len(questrunner.GroundItemsNear(w.d, id, at, r))
}
func (w liveMaze) TakeGround(id int, at tile.Tile, r int) bool {
	return questrunner.TakeGround(w.d, id, at, r)
}

// Op finds the loc near a tile by its type and operates it. A door that has just let someone through is an invisible wall for
// three ticks (open_and_close_door2), so the loc is asked for again a few times.
func (w liveMaze) Op(locID int, near tile.Tile, op string) bool {
	d := w.d
	for try := 0; try < 4; try++ {
		m, ok := questrunner.FindLoc(d, locID, "", op, near, 3)
		if !ok {
			if try < 3 {
				d.Delay(700 * time.Millisecond)
				continue
			}
			logf(d, "ernest: no loc %d offering %q near %d,%d", locID, op, near.X, near.Z)
			return false
		}
		if err := questrunner.OperateLoc(d, m, op); err != nil {
			logf(d, "ernest: %s (loc %d): %v", op, locID, err)
			return false
		}
		return true
	}
	return false
}

// mazeTeleportGap is how long a second teleport is not sent after one: it takes a few ticks to land.
const mazeTeleportGap = 20 * time.Second

func (w liveMaze) TeleportOut() bool {
	d := w.d
	if !randomevents.EscapeTeleport(d.Session, "ernest: trapped in the Draynor Manor basement", mazeTeleportGap, d.Log) {
		return false
	}
	return waitUntil(d, 15*time.Second, func() bool { return !questplan.InErnestBasement(questrunner.CurrentTile(d)) })
}

// mazeWalkRadius is how near a lever or a door the account stands: on its own tile, which is where the engine accepts the op
// from (a wall decoration is operated from its own tile and a straight wall from either side of its edge).
const mazeWalkRadius = 0

// mazeLeverAttempts is how many times one lever is pulled before it is given up: a pull toggles, so a missed message is
// answered by the next pull's, whose state is the truth.
const mazeLeverAttempts = 4

// mazePasses is how many times the whole climb down, solve and take is tried before the leg is given up.
const mazePasses = 3

// mazeCanWait is how long the account waits in room 9 for the oil can to respawn (100 ticks) after another account's pickup.
// A variable so that a test does not wait it out.
var mazeCanWait = 110 * time.Second

// mazeStrandedPark is how long the quest is left alone after the search found the account in a state with no way out and nothing
// to teleport with: long enough for the fleet's own rescue (a random event's fail-teleport, a rune or a necklace arriving) to have
// run. mazeRetryPark is the wait after a failure that says nothing about the account's state (a walk that would not start, an op
// that found no loc), whose cure is another try.
const (
	mazeStrandedPark = 45 * time.Minute
	mazeRetryPark    = 10 * time.Minute
)

// mazeProbeLimit bounds one search for the way out. The worst case over every state of the puzzle and every belief is 18
// probes (questplan's TestEveryStateWithAWayOutGetsOutWhateverTheAccountBelieves); the rest is for walks that fail and retry.
const mazeProbeLimit = 48

// mazeLeaveRetries is how many more times a leg that ends inside the basement tries the way out before it reports the account
// stranded and parks: a walk that failed or an op that found no loc may not again, and a park with the account still inside is
// ten minutes of the quest's slot spent in the maze.
const mazeLeaveRetries = 2

// mazeTrappedMemory is how long a verdict "every state is a dead end" is kept for the tile it was reached on: the pocket's way
// out is asked for before every walk, and the search it would run again clicks levers and doors for minutes.
const mazeTrappedMemory = 10 * time.Minute

// mazeMemory is one account's belief of the levers: the set of states the server may be in (the zero value, an empty set, reads
// as the open belief). tested is whether the server has answered it since it was taken from a record. trapped and trappedAt are the
// last verdict that no door leads out of any state the levers could be in.
type mazeMemory struct {
	mu        sync.Mutex
	belief    questplan.MazeBelief
	tested    bool
	trapped   time.Time
	trappedAt tile.Tile
	// engine is the quest engine whose side store keeps the hints, once a leg of it has used this memory: the pocket's way out
	// (leaveBasement) has only the Deps, and writes through this so that a restart never reads a record it has made stale.
	engine *questrunner.Engine
}

var (
	mazeMemMu sync.Mutex
	mazeMems  = map[*questrunner.Deps]*mazeMemory{}
)

func mazeMemoryFor(d *questrunner.Deps) *mazeMemory {
	mazeMemMu.Lock()
	defer mazeMemMu.Unlock()
	m := mazeMems[d]
	if m == nil {
		m = &mazeMemory{}
		mazeMems[d] = m
	}
	return m
}

// beliefOf is the belief and whether the server has answered it.
func (m *mazeMemory) beliefOf() (questplan.MazeBelief, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.belief.Empty() {
		return questplan.MazeAnyLevers, true
	}
	return m.belief, m.tested
}

func (m *mazeMemory) setBelief(b questplan.MazeBelief, tested bool) {
	m.mu.Lock()
	m.belief, m.tested = b, tested
	m.mu.Unlock()
}

// get is the levers when the belief is one state, and whether it is.
func (m *mazeMemory) get() (uint8, bool) {
	b, _ := m.beliefOf()
	return b.Exact()
}

// set is the belief that the levers are exactly these bits (known) or that nothing is known of them, either way fresh from the
// server.
func (m *mazeMemory) set(bits uint8, known bool) {
	if known {
		m.setBelief(questplan.MazeExactly(bits), true)
		return
	}
	m.setBelief(questplan.MazeAnyLevers, true)
}

func (m *mazeMemory) engineOr(e *questrunner.Engine) *questrunner.Engine {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e != nil {
		m.engine = e
	}
	return m.engine
}

// noteTrapped keeps the verdict for the tile it was reached on; trappedHere reports it while it holds.
func (m *mazeMemory) noteTrapped(at tile.Tile) {
	m.mu.Lock()
	m.trapped, m.trappedAt = time.Now(), at
	m.mu.Unlock()
}

func (m *mazeMemory) trappedHere(at tile.Tile) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.trapped.IsZero() && m.trappedAt == at && time.Since(m.trapped) < mazeTrappedMemory
}

func (m *mazeMemory) clearTrapped() {
	m.mu.Lock()
	m.trapped = time.Time{}
	m.mu.Unlock()
}

// The side store's hints for the maze: whether the account was inside it when the hints were written, and each lever's
// state (set: down).
const mazeHintIn = "maze-in"

func mazeHintKey(lever byte) string { return "maze-" + string(rune(lever)) }

// mazeRun is one leg's working state over an account's memory.
type mazeRun struct {
	e   *questrunner.Engine // its Store keeps the hints; nil (or no Store) keeps none
	w   mazeWorld
	mem *mazeMemory
	// trapped is whether a search of this run found that every state the levers could be in is a dead end.
	trapped bool
}

// newMazeRun builds a run. An account that stands in the maze with nothing remembered (a restarted process) takes the
// belief the side store kept the last time it pulled a lever, as a prior: the search checks it against the server and drops it
// at the first answer that contradicts it (a record from an earlier life, or from a pull whose message came late, is not a fact).
func newMazeRun(e *questrunner.Engine, w mazeWorld, mem *mazeMemory) *mazeRun {
	e = mem.engineOr(e)
	r := &mazeRun{e: e, w: w, mem: mem}
	if cur, _ := mem.beliefOf(); cur == questplan.MazeAnyLevers && e != nil && e.Store != nil && questplan.InErnestBasement(w.Here()) {
		hints := e.Store.Record(questplan.ErnestChickenID).Hints
		if hints[mazeHintIn] {
			var bits uint8
			for _, l := range questplan.MazeLevers {
				if hints[mazeHintKey(l.Name)] {
					bits |= l.Bit
				}
			}
			mem.setBelief(questplan.MazeExactly(bits), false)
			w.Log("ernest: back in the maze after a restart, the levers were left %s", questplan.MazeBitsString(bits))
		}
	}
	return r
}

// remember sets the belief to one state (or to none, known false) and writes it to the side store: inside says the account is
// in the maze (the hints mean nothing outside it).
func (r *mazeRun) remember(bits uint8, known, inside bool) {
	if known {
		r.rememberBelief(questplan.MazeExactly(bits), true, inside)
		return
	}
	r.rememberBelief(questplan.MazeAnyLevers, true, inside)
}

// rememberBelief sets the belief and writes it to the side store. The store keeps a state, not a set: a belief that is not
// exactly one state writes none, so that a restart takes nothing from it.
func (r *mazeRun) rememberBelief(b questplan.MazeBelief, tested, inside bool) {
	r.mem.setBelief(b, tested)
	if r.e == nil || r.e.Store == nil {
		return
	}
	bits, known := b.Exact()
	rec := r.e.Store.Record(questplan.ErnestChickenID)
	if rec.Hints == nil {
		rec.Hints = map[string]bool{}
	}
	changed := rec.Hints[mazeHintIn] != (known && inside)
	rec.Hints[mazeHintIn] = known && inside
	for _, l := range questplan.MazeLevers {
		down := known && inside && bits&l.Bit != 0
		if rec.Hints[mazeHintKey(l.Name)] != down {
			changed = true
		}
		rec.Hints[mazeHintKey(l.Name)] = down
	}
	if changed && r.e.Save != nil {
		r.e.Save()
	}
}

// observe lets the search's rules narrow the belief by what the server just said, and keeps the result. The room is the
// account's now, which matters to nothing but the rules' own bookkeeping.
func (r *mazeRun) observe(f func(s *questplan.MazeSearch)) {
	b, tested := r.mem.beliefOf()
	s := &questplan.MazeSearch{Room: r.room(), Belief: b, Tested: tested}
	f(s)
	r.rememberBelief(s.Belief, s.Tested, true)
}

func (r *mazeRun) room() questplan.MazeRoom { return questplan.MazeRoomAt(r.w.Here()) }

func (r *mazeRun) state() questplan.MazeState {
	bits, _ := r.mem.get()
	return questplan.MazeState{Room: r.room(), Bits: bits}
}

func stateWord(down bool) string {
	if down {
		return "down"
	}
	return "up"
}

// inspect reads one lever's state off the server ("The lever is up."), which changes nothing: the one way to read a lever whose
// pull may or may not have landed. It reports false when the account has no chat log to read the answer from, or the answer
// never came.
func (r *mazeRun) inspect(l questplan.MazeLever) (down, ok bool) {
	w := r.w
	if !w.HasChat() {
		return false, false
	}
	for attempt := 0; attempt < 2; attempt++ {
		if !w.Walk(l.Tile, mazeWalkRadius) {
			w.Log("ernest: could not reach lever %c at %d,%d", l.Name, l.Tile.X, l.Tile.Z)
			return false, false
		}
		mark := time.Now()
		if !w.Op(l.LocID, l.Tile, "Inspect") {
			return false, false
		}
		if w.Wait(6*time.Second, func() bool { _, got := questplan.MazeLeverInspected(w.Said(mark)); return got }) {
			down, _ = questplan.MazeLeverInspected(w.Said(mark))
			r.observe(func(s *questplan.MazeSearch) { s.SawLever(l.Name, down) })
			w.Log("ernest: lever %c is %s (inspected; levers %s)", l.Name, stateWord(down), r.beliefString())
			return down, true
		}
		w.Log("ernest: inspecting lever %c answered nothing", l.Name)
	}
	return false, false
}

func (r *mazeRun) beliefString() string {
	b, _ := r.mem.beliefOf()
	return b.String()
}

// pull pulls lever l until the server says it is left in the wanted state. A pull whose message did not come, or came for another
// lever, is not pulled again blind (that would toggle it back if the first had landed late): the lever is inspected, and what
// the inspect says is the truth.
func (r *mazeRun) pull(l questplan.MazeLever, down bool) bool {
	w := r.w
	for attempt := 0; attempt < mazeLeverAttempts; attempt++ {
		if !w.Walk(l.Tile, mazeWalkRadius) {
			w.Log("ernest: could not reach lever %c at %d,%d", l.Name, l.Tile.X, l.Tile.Z)
			return false
		}
		mark := time.Now()
		if !w.Op(l.LocID, l.Tile, "Pull") {
			return false
		}
		if !w.HasChat() {
			// No chat log (a harness): the pull is taken to have toggled the lever.
			w.Delay(1200 * time.Millisecond)
			r.observe(func(s *questplan.MazeSearch) { s.Pulled(l.Name) })
			b, _ := r.mem.beliefOf()
			got, known := b.LeverKnown(l.Name)
			if !known {
				w.Log("ernest: pulled lever %c with no chat log and no belief of it: cannot say how it was left", l.Name)
				return false
			}
			if got == down {
				return true
			}
			continue
		}
		if !w.Wait(6*time.Second, func() bool { _, _, ok := questplan.MazeLeverSaid(w.Said(mark)); return ok }) {
			w.Log("ernest: lever %c answered nothing", l.Name)
			r.observe(func(s *questplan.MazeSearch) { s.Belief = s.Belief.Forget(l.Name) })
			if got, ok := r.inspect(l); ok && got == down {
				return true
			}
			continue
		}
		name, wentDown, _ := questplan.MazeLeverSaid(w.Said(mark))
		if name != l.Name {
			w.Log("ernest: pulled lever %c and the server answered about lever %c", l.Name, name)
			r.observe(func(s *questplan.MazeSearch) { s.Belief = s.Belief.Forget(l.Name) })
			if got, ok := r.inspect(l); ok && got == down {
				return true
			}
			continue
		}
		r.observe(func(s *questplan.MazeSearch) {
			s.Pulled(l.Name)
			s.SawLever(l.Name, wentDown)
		})
		w.Log("ernest: lever %c is now %s (levers %s)", l.Name, stateWord(wentDown), r.beliefString())
		if attempt > 0 {
			// The message may be the late answer to an earlier pull of this lever: the inspect is the truth.
			if got, ok := r.inspect(l); ok {
				wentDown = got
			}
		}
		if wentDown == down {
			return true
		}
	}
	w.Log("ernest: could not leave lever %c %s", l.Name, stateWord(down))
	return false
}

// crossDoor opens a door from the room the account is in and waits to be in the room across it. locked is the server's
// refusal: the account's belief of the levers is wrong.
func (r *mazeRun) crossDoor(door questplan.MazeDoor, from questplan.MazeRoom) (crossed, locked bool) {
	w := r.w
	to, _ := door.Other(from)
	stand, _, ok := door.Side(from)
	if !ok {
		return false, false
	}
	if !w.Walk(stand, mazeWalkRadius) {
		w.Log("ernest: could not reach the %s door's stand %d,%d", door.Name, stand.X, stand.Z)
		return false, false
	}
	mark := time.Now()
	if !w.Op(door.LocID, stand, "Open") {
		return false, false
	}
	w.Wait(8*time.Second, func() bool { return r.room() == to || questplan.MazeDoorLocked(w.Said(mark)) })
	if r.room() == to {
		r.observe(func(s *questplan.MazeSearch) { s.SawDoor(door.Name, true) })
		w.Log("ernest: crossed %s into %s", door.Name, to)
		return true, false
	}
	locked = questplan.MazeDoorLocked(w.Said(mark))
	if locked {
		before, _ := r.mem.beliefOf()
		r.observe(func(s *questplan.MazeSearch) { s.SawDoor(door.Name, false) })
		if bits, exact := before.Exact(); exact && door.Opens(bits) {
			w.Log("ernest: door %s is locked with the levers believed %s: the belief is wrong (now %s)", door.Name, questplan.MazeBitsString(bits), r.beliefString())
		} else {
			w.Log("ernest: door %s is locked (levers %s)", door.Name, r.beliefString())
		}
	}
	return false, locked
}

// play carries out one move.
func (r *mazeRun) play(mv questplan.MazeMove) bool {
	if mv.IsPull() {
		l, _ := questplan.MazeLeverByName(mv.Lever)
		return r.pull(l, mv.Down)
	}
	door, _ := questplan.MazeDoorByName(mv.Door)
	crossed, _ := r.crossDoor(door, mv.From)
	return crossed
}

// walkTo plays the shortest moves from where the account is, on the levers as it believes them, into goal.
func (r *mazeRun) walkTo(goal questplan.MazeRoom) bool {
	if r.room() == goal {
		return true
	}
	if _, known := r.mem.get(); !known {
		r.w.Log("ernest: in the maze (%s) with the levers %s: no exact plan for the doors to %s", r.room(), r.beliefString(), goal)
		return false
	}
	st := r.state()
	moves, ok := questplan.MazeSolve(st, goal)
	if !ok {
		r.w.Log("ernest: no way from %s on levers %s to %s", st.Room, questplan.MazeBitsString(st.Bits), goal)
		return false
	}
	names := make([]string, 0, len(moves))
	for _, mv := range moves {
		names = append(names, mv.String())
	}
	r.w.Log("ernest: %d moves from %s to %s: %s", len(moves), st.Room, goal, strings.Join(names, ", "))
	for _, mv := range moves {
		if !r.play(mv) {
			return false
		}
	}
	return r.room() == goal
}

// climb sends a ladder op from its stand and waits for want.
func (r *mazeRun) climb(locID int, op string, stand tile.Tile, want func() bool) bool {
	w := r.w
	for attempt := 0; attempt < 3; attempt++ {
		if want() {
			return true
		}
		if !w.Walk(stand, 1) {
			w.Log("ernest: could not reach the ladder's stand %d,%d", stand.X, stand.Z)
			return false
		}
		if !w.Op(locID, stand, op) {
			continue
		}
		if w.Wait(10*time.Second, want) {
			return true
		}
	}
	return false
}

// descend climbs down the alcove's ladder into the maze's entry room.
func (r *mazeRun) descend() bool {
	ok := r.climb(questplan.LocPuzzleLadderDown, "Climb-down", questplan.ErnestLadderDownStand, func() bool { return r.room() == questplan.MazeEntry })
	if ok {
		bits, known := r.mem.get()
		r.remember(bits, known, true)
		r.w.Log("ernest: down the ladder into the maze")
	}
	return ok
}

// ascend climbs the maze's ladder up into the alcove, which resets the levers: all up, and believed so.
func (r *mazeRun) ascend() bool {
	ok := r.climb(questplan.LocPuzzleLadderUp, "Climb-up", questplan.ErnestLadderUpStand, func() bool { return questplan.InErnestAlcove(r.w.Here()) })
	if ok {
		r.remember(0, true, false)
		r.w.Log("ernest: up the ladder into the alcove, the levers reset")
	}
	return ok
}

// intoAlcove goes through a bookcase from the manor's ground floor. The bookcases refuse an account west of them.
func (r *mazeRun) intoAlcove() bool {
	w := r.w
	if questplan.InErnestAlcove(w.Here()) {
		return true
	}
	for _, bc := range []struct {
		loc   int
		stand tile.Tile
		at    tile.Tile
	}{
		{questplan.LocManorBookcaseL, questplan.ErnestBookcaseStand, tile.Tile{X: 3097, Z: 3358}},
		{questplan.LocManorBookcaseR, tile.Tile{X: 3098, Z: 3359}, tile.Tile{X: 3097, Z: 3359}},
	} {
		if !w.Travel(bc.stand, 0) || w.Here() != bc.stand {
			w.Log("ernest: could not stand east of the bookcase at %d,%d", bc.at.X, bc.at.Z)
			continue
		}
		if !w.Op(bc.loc, bc.at, "Search") {
			continue
		}
		if w.Wait(10*time.Second, func() bool { return questplan.InErnestAlcove(w.Here()) }) {
			w.Log("ernest: through the bookcase into the alcove")
			return true
		}
	}
	return false
}

// leaveAlcove pulls the alcove's lever, which teleports the account back out into the manor.
func (r *mazeRun) leaveAlcove() bool {
	w := r.w
	if !questplan.InErnestAlcove(w.Here()) {
		return true
	}
	for attempt := 0; attempt < 3; attempt++ {
		if !w.Walk(questplan.ErnestAlcoveLever, 0) {
			w.Log("ernest: could not reach the alcove's lever")
			return false
		}
		if !w.Op(questplan.LocAlcoveLever, questplan.ErnestAlcoveLever, "Pull") {
			continue
		}
		if w.Wait(10*time.Second, func() bool { return !questplan.InErnestAlcove(w.Here()) }) {
			w.Log("ernest: the alcove's lever opened the bookcase: back in the manor")
			return true
		}
	}
	return false
}

// toEntry gets an account that stands in the maze back to its entry room: by the search, which is the plan when the belief is one
// state with a way out and the inspects and door trials that find one when it is not.
func (r *mazeRun) toEntry() bool { return r.exploreToEntry() }

// exploreToEntry carries out questplan.MazeNextProbe until the account stands in the ladder's room: each pass the search says
// to inspect a lever, pull one, or try a door, and what the server answers narrows the belief. It reports whether the account is
// in the ladder's room (or out of the maze). A search that ends because every state the levers could be in is a dead end sets
// r.trapped; one that ends because a walk or an op failed leaves it clear, since another try may do.
func (r *mazeRun) exploreToEntry() bool {
	w := r.w
	if r.room() == questplan.MazeOutside || r.room() == questplan.MazeEntry {
		return true
	}
	if r.mem.trappedHere(w.Here()) {
		r.trapped = true
		return false
	}
	if !w.HasChat() {
		// Without a chat log nothing a lever or a door says can be read: only an exact belief can be walked.
		return r.walkTo(questplan.MazeEntry)
	}
	for probe := 0; probe < mazeProbeLimit; probe++ {
		room := r.room()
		b, tested := r.mem.beliefOf()
		s := &questplan.MazeSearch{Room: room, Belief: b, Tested: tested}
		p := s.Next()
		if s.Belief != b {
			w.Log("ernest: the believed levers (%s) have no way out of %s: not trusting them, levers %s", b, room, s.Belief)
			r.rememberBelief(s.Belief, s.Tested, true)
		}
		switch p.Kind {
		case questplan.MazeProbeArrived:
			return true
		case questplan.MazeProbeTrapped:
			r.trapped = true
			r.mem.noteTrapped(w.Here())
			w.Log("ernest: in %s with the levers %s: every state they could be in is a dead end", room, s.Belief)
			return false
		case questplan.MazeProbeInspect:
			l, _ := questplan.MazeLeverByName(p.Lever)
			if _, ok := r.inspect(l); !ok {
				return false
			}
		default:
			if p.Kind == questplan.MazeProbeDoor {
				w.Log("ernest: levers %s: trying %s", s.Belief, p.Move)
			}
			if p.Move.IsPull() {
				if !r.play(p.Move) {
					return false
				}
				continue
			}
			door, _ := questplan.MazeDoorByName(p.Move.Door)
			crossed, locked := r.crossDoor(door, p.Move.From)
			if !crossed && !locked {
				return false
			}
		}
	}
	w.Log("ernest: no way out of the maze found in %d probes (levers %s)", mazeProbeLimit, r.beliefString())
	return false
}

// escapeTrap leaves a trapped account by the teleport it carries, and reports whether it stands outside the basement now. The
// levers are not known afterwards (a spell resets them, a rub of jewellery does not), and the next descent resets them.
func (r *mazeRun) escapeTrap() bool {
	if !r.trapped {
		return false
	}
	w := r.w
	if !w.TeleportOut() {
		w.Log("ernest: trapped in %s and nothing to teleport with (no spell it can pay for, no Games necklace or Ring of dueling)", r.room())
		return false
	}
	r.trapped = false
	r.mem.clearTrapped()
	r.remember(0, false, false)
	w.Log("ernest: teleported out of the basement to %v", w.Here())
	return true
}

// enter brings the account into the maze's entry room with the levers all up, whatever state it starts in. The way down does
// not reset the levers: an account that arrives with a belief that they are not all up climbs back and down again, which does.
func (r *mazeRun) enter() bool {
	if r.room() == questplan.MazeOutside {
		if !r.intoAlcove() || !r.descend() {
			return false
		}
	} else if !r.toEntry() {
		r.escapeTrap()
		return false
	}
	if bits, known := r.mem.get(); known && bits == 0 {
		return true
	}
	r.w.Log("ernest: the levers' state is not known to be all up: up the ladder and down again to reset them")
	return r.ascend() && r.descend()
}

// takeCan picks the oil can off room 9's floor, waiting for it to respawn if another account has just taken it.
func (r *mazeRun) takeCan() bool {
	w := r.w
	if w.Held(questplan.ObjOilCan) {
		return true
	}
	if !w.Walk(questplan.ErnestOilCanSpawn, 1) {
		w.Log("ernest: could not reach the oil can's tile")
		return false
	}
	deadline := time.Now().Add(mazeCanWait)
	for !w.Held(questplan.ObjOilCan) && time.Now().Before(deadline) {
		if w.GroundItems(questplan.ObjOilCan, questplan.ErnestOilCanSpawn, 3) > 0 && w.TakeGround(questplan.ObjOilCan, questplan.ErnestOilCanSpawn, 3) {
			break
		}
		if !w.Delay(1500 * time.Millisecond) {
			return false
		}
	}
	if w.Held(questplan.ObjOilCan) {
		w.Log("ernest: the oil can is in the pack")
		return true
	}
	w.Log("ernest: no oil can on the floor of room 9 within %s", mazeCanWait)
	return false
}

// leaveMaze takes the account from wherever it is in the maze to the manor's ground floor: to the entry room by the
// doors, up the ladder, out of the alcove. It reports whether the account is out.
func (r *mazeRun) leaveMaze() bool {
	if r.room() != questplan.MazeOutside {
		if !r.toEntry() {
			return r.escapeTrap()
		}
		if !r.ascend() {
			return false
		}
	}
	return r.leaveAlcove()
}

// leaveBasement is the pocket's way out: the maze, then the alcove.
func leaveBasement(d *questrunner.Deps) bool {
	return newMazeRun(nil, liveMaze{d}, mazeMemoryFor(d)).leaveMaze()
}

// leaveAlcove is the alcove pocket's way out.
func leaveAlcove(d *questrunner.Deps) bool {
	return newMazeRun(nil, liveMaze{d}, mazeMemoryFor(d)).leaveAlcove()
}

// fetchCan is the leg: into the alcove by the bookcase, down the ladder, the doors and levers to room 9, the can, and out the
// way it came, up to mazePasses times. stranded is why the account is left somewhere it cannot plan its way out of ("" when it
// is out, or never went in).
func (r *mazeRun) fetchCan() (ok bool, stranded string) {
	w := r.w
	if w.Held(questplan.ObjOilCan) {
		if !r.leaveMaze() {
			stranded = r.stuckInside()
			return stranded == "", stranded
		}
		return true, ""
	}
	for pass := 1; pass <= mazePasses; pass++ {
		if !r.enter() {
			w.Log("ernest: pass %d: could not get into the maze with the levers reset", pass)
			return false, r.stuckInside()
		}
		if r.walkTo(questplan.MazeR9) && r.takeCan() {
			if !r.leaveMaze() {
				w.Log("ernest: the oil can is in the pack and the way out of the maze failed")
				stranded = r.stuckInside()
				return stranded == "" && w.Held(questplan.ObjOilCan), stranded
			}
			return w.Held(questplan.ObjOilCan), ""
		}
		w.Log("ernest: pass %d of %d did not reach the can: back to the ladder", pass, mazePasses)
		if !r.leaveMaze() {
			return false, r.stuckInside()
		}
	}
	return false, ""
}

// stuckInside is the end of a leg that left the account inside the basement: it tries the way out again, up to mazeLeaveRetries
// times, unless the search found every state a dead end (nothing more to try without a teleport), and then says where the account
// stands and why ("" when it is out).
func (r *mazeRun) stuckInside() string {
	for try := 1; try <= mazeLeaveRetries && !r.trapped && r.strandedAt() != ""; try++ {
		r.w.Log("ernest: still inside at %v: another try at the way out (%d of %d)", r.w.Here(), try, mazeLeaveRetries)
		if !r.w.Delay(3 * time.Second) {
			break
		}
		r.leaveMaze()
	}
	return r.strandedAt()
}

// strandedAt says where an account that is still inside the maze or the alcove stands, and "" for one that is outside both.
func (r *mazeRun) strandedAt() string {
	here := r.w.Here()
	if r.room() == questplan.MazeOutside && !questplan.InErnestAlcove(here) {
		return ""
	}
	b, _ := r.mem.beliefOf()
	state := b.String()
	if b == questplan.MazeAnyLevers {
		state = "unknown"
	}
	why := "no way out the module can plan"
	if r.trapped {
		why = "every state the levers could be in is a dead end and nothing it carries teleports"
	}
	return fmt.Sprintf("stranded in the Draynor Manor basement at %d,%d,%d (levers %s): %s", here.X, here.Z, here.Level, state, why)
}

// ernestCan is the oil can leg.
func ernestCan(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	r := newMazeRun(e, liveMaze{d}, mazeMemoryFor(d))
	ok, stranded := r.fetchCan()
	if stranded != "" {
		e.RequestPark(stranded, mazeParkFor(r.trapped))
	}
	return ok
}

// mazeParkFor is how long the quest is left alone when the account could not be taken out of the basement: long for an account
// whose every state is a dead end with nothing to teleport by, short for a failure another try may mend.
func mazeParkFor(trapped bool) time.Duration {
	if trapped {
		return mazeStrandedPark
	}
	return mazeRetryPark
}

// chatSince is the game messages since mark, "" without a chat log.
func chatSince(d *questrunner.Deps, mark time.Time) string {
	if d.Chat == nil {
		return ""
	}
	return d.Chat.Since(mark)
}
