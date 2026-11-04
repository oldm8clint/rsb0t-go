package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/doorescape"
)

// castleWorld is Rogues' Castle as the server runs it, one 600 ms tick at a time, for driving the escape ladder
// in a closed loop: the account's tile, the doors' leaves, the walk queue and the pending loc op, with Engine-TS's
// own rules for each.
//
//   - Collision is internal/doorescape's port of the engine (checked tile by tile against Engine-TS in
//     doorescape's TestAgainstTheEngine), over the doors as they stand.
//   - A MOVE_GAMECLICK replaces the walk queue with the click's corners, the first of which is the tile the
//     client thought it stood on (MoveClickHandler); each tick the player takes up to two steps (run) toward the
//     first corner, trying the diagonal, then along x, then along z (PathingEntity.takeStep), and a corner it
//     is standing on is simply used up, which costs the tick; a corner it cannot step toward is never used up.
//   - An OPLOC sets an interaction and clears nothing else (the walk queue is left intact: OpLocHandler's
//     clearPendingAction); the loc's op runs on a tick in which the player took no step and the engine's reach
//     test accepts the tile (Player.tryInteract(stepsTaken === 0)); with no queue left and no step taken it
//     answers "I can't reach that!" and forgets the interaction.
//   - Opening a door deletes the closed leaf and adds the open leaf, and a diagonal door pushes an account standing
//     on the tile the leaf lands on aside first; closing does the reverse; both leaves go back to the map's state
//     500 ticks after the last op on that door (the engine's own respawn and despawn timers).
//   - A staircase op teleports the account to the landing tile.
type castleWorld struct {
	t     *testing.T
	cat   *doorescape.Catalog
	state doorescape.State
	pos   doorescape.Tile
	tick  int

	queue  []doorescape.Tile
	run    bool
	target *worldOp

	cache      []uint32
	cacheState doorescape.State
	cacheOut   uint32

	revertAt map[int]int
	// locked are doors whose op never does anything (a quest gate, a missing key): the click lands and nothing changes.
	locked map[int]bool
	// lag is how many ticks the client's view of the doors trails the server's.
	lag     int
	history []doorescape.State

	// counters, for the assertions: opsDone is every loc op the engine ran (a locked door's included), doorClicks
	// those on each door (by catalogue index), closes the doors the account really shut.
	opsDone, walkClicks, opClicks, cantReach int
	pushes, closes                           int
	doorClicks                               map[int]int
}

type worldOp struct {
	door   int  // door index, or -1
	stair  int  // stair index, or -1
	closed bool // for a door: closing the open leaf
}

func newCastleWorld(t *testing.T, at doorescape.Tile, s doorescape.State) *castleWorld {
	t.Helper()
	cat, err := doorescape.Castle()
	if err != nil {
		t.Fatal(err)
	}
	w := &castleWorld{t: t, cat: cat, state: s, pos: at, revertAt: map[int]int{}, locked: map[int]bool{}, lag: 1, doorClicks: map[int]int{}}
	w.history = []doorescape.State{s}
	return w
}

// flags is the collision with the doors as they stand, read from a table that is rebuilt only when a door changes.
func (w *castleWorld) flags() doorescape.FlagsFunc {
	grid := w.cat.Grid
	if w.cache == nil || w.cacheState != w.state {
		f := w.cat.FlagsAt(w.state)
		w.cache = make([]uint32, grid.Len())
		for i := range w.cache {
			t := grid.TileAt(i)
			w.cache[i] = f(t.X, t.Z, t.Level)
		}
		w.cacheState = w.state
		w.cacheOut = f(-1, -1, 0) // a tile outside the grid: blocked
	}
	cache, out := w.cache, w.cacheOut
	return func(x, z, level int) uint32 {
		i := grid.Index(x, z, level)
		if i < 0 {
			return out
		}
		return cache[i]
	}
}

// Walk is a MOVE_GAMECLICK whose corners are path (path[0] is where the client believed it stood).
func (w *castleWorld) walk(path []doorescape.Tile) {
	w.walkClicks++
	if len(path) == 0 {
		return
	}
	w.queue = append(w.queue[:0], path...)
	w.run = true
}

// opLoc is an OPLOC: a loc by id at x, z, with the option number. It does nothing unless the loc is really there
// (World.getLoc) and offers the option.
func (w *castleWorld) opLoc(op, x, z, id int) {
	w.opClicks++
	for i, d := range w.cat.Doors {
		if d.Level != w.pos.Level || op != d.Op {
			continue
		}
		if c := d.Closed(); c.X == x && c.Z == z && c.ID == id && w.state.ClosedLeaf(i) {
			w.target = &worldOp{door: i, stair: -1}
			return
		}
		if o := d.Open(); o.X == x && o.Z == z && o.ID == id && w.state.OpenLeaf(i) {
			w.target = &worldOp{door: i, stair: -1, closed: true}
			return
		}
	}
	for i, s := range w.cat.Stairs {
		p := s.Placement
		if p.Level == w.pos.Level && p.X == x && p.Z == z && p.ID == id && s.Op == op {
			w.target = &worldOp{door: -1, stair: i}
			return
		}
	}
	w.target = nil
}

// step is PathingEntity.validateAndAdvanceStep: it reports whether the player moved.
func (w *castleWorld) step() bool {
	if len(w.queue) == 0 {
		return false
	}
	to := w.queue[0]
	dx, dz := sgn(to.X-w.pos.X), sgn(to.Z-w.pos.Z)
	f := w.flags()
	moved := false
	switch {
	case (dx != 0 || dz != 0) && doorescape.CanStep(f, w.pos.Level, w.pos.X, w.pos.Z, dx, dz):
		w.pos.X, w.pos.Z = w.pos.X+dx, w.pos.Z+dz
		moved = true
	case dx != 0 && dz != 0 && doorescape.CanStep(f, w.pos.Level, w.pos.X, w.pos.Z, dx, 0):
		w.pos.X += dx
		moved = true
	case dx != 0 && dz != 0 && doorescape.CanStep(f, w.pos.Level, w.pos.X, w.pos.Z, 0, dz):
		w.pos.Z += dz
		moved = true
	}
	if w.pos.X == to.X && w.pos.Z == to.Z {
		w.queue = w.queue[1:]
	}
	return moved
}

// Tick runs one server tick.
func (w *castleWorld) Tick() {
	w.tick++
	steps := 0
	if len(w.queue) > 0 {
		if w.step() {
			steps++
			if w.run && len(w.queue) > 0 && w.step() {
				steps++
			}
		}
	}
	if w.target != nil && steps == 0 {
		w.interact()
	}
	for i, at := range w.revertAt {
		if w.tick >= at {
			w.state = w.state.With(i, true, false)
			delete(w.revertAt, i)
		}
	}
	w.history = append(w.history, w.state)
	if len(w.history) > w.lag+2 {
		w.history = w.history[len(w.history)-(w.lag+2):]
	}
}

// interact is Player.tryInteract for a player who took no step this tick.
func (w *castleWorld) interact() {
	tg := w.target
	f := w.flags()
	if tg.stair >= 0 {
		s := w.cat.Stairs[tg.stair]
		if doorescape.Reached(f, w.pos.Level, w.pos.X, w.pos.Z, s.Placement, s.Type) {
			w.pos = s.Land
			w.queue = nil
			w.target = nil
			w.opsDone++
			return
		}
	} else {
		d := w.cat.Doors[tg.door]
		loc := d.Closed()
		if tg.closed {
			loc = d.Open()
		}
		if doorescape.Reached(f, w.pos.Level, w.pos.X, w.pos.Z, loc, doorescape.LocType{Width: 1, Length: 1}) {
			w.target = nil
			w.opsDone++
			w.doorClicks[tg.door]++
			if w.locked[tg.door] {
				return
			}
			w.queue = nil // the script's p_delay and any push stop the walk
			if tg.closed {
				w.closes++
				if push, ok := d.PushClose(); ok && w.pos.X == d.X && w.pos.Z == d.Z && w.pos.Level == d.Level {
					w.pos = push
					w.pushes++
				}
				w.state = w.state.With(tg.door, true, false)
			} else {
				if push, ok := d.Push(); ok && w.pos.X == d.Open().X && w.pos.Z == d.Open().Z {
					w.pos = push
					w.pushes++
				}
				w.state = w.state.With(tg.door, false, true)
			}
			w.revertAt[tg.door] = w.tick + 500
			return
		}
	}
	if len(w.queue) == 0 {
		w.cantReach++
		w.target = nil
	}
}

// toggle is another account using door i: whichever honest state it is in, it goes to the other, pushing an account
// aside that stands where the leaf lands, as the door scripts do for the account that uses the door.
func (w *castleWorld) toggle(i int) {
	d := w.cat.Doors[i]
	if d.Level != w.pos.Level {
		return
	}
	if w.state.OpenLeaf(i) {
		if push, ok := d.PushClose(); ok && w.pos.X == d.X && w.pos.Z == d.Z {
			w.pos = push
			w.pushes++
		}
		w.state = w.state.With(i, true, false)
		delete(w.revertAt, i)
	} else {
		if push, ok := d.Push(); ok && w.pos.X == d.Open().X && w.pos.Z == d.Open().Z {
			w.pos = push
			w.pushes++
		}
		w.state = w.state.With(i, false, true)
		w.revertAt[i] = w.tick + 500
	}
}

// clientState is the doors as the client sees them: the server's, lag ticks ago.
func (w *castleWorld) clientState() doorescape.State {
	i := len(w.history) - 1 - w.lag
	if i < 0 {
		i = 0
	}
	return w.history[i]
}

// sleepThrough lets the world idle until the ladder's sleep is over, as the server would while the account stands
// still: one tick is left to run for real. Anything that happens in the world meanwhile is left to the timers (the
// doors' reverts fire on the next Tick).
func (w *castleWorld) sleepThrough(until time.Time) {
	if ticks := int(until.Sub(w.now())/(600*time.Millisecond)) - 1; ticks > 0 {
		w.tick += ticks
	}
}

// now is the fake clock: a tick is 600 ms.
func (w *castleWorld) now() time.Time {
	return time.Unix(3_000_000, 0).Add(time.Duration(w.tick) * 600 * time.Millisecond)
}

func (w *castleWorld) outside() bool {
	return !w.cat.Inside(w.pos.X, w.pos.Z, w.pos.Level) && w.cat.Free(w.pos.X, w.pos.Z, w.pos.Level)
}

// escapeRun is one drive of the ladder against a world.
type escapeRun struct {
	escaped   bool
	gaveUp    bool
	ticks     int
	ladder    StrandLadder
	acted     int
	firstAct  int // the tick of the ladder's first walk or click, -1 when it never acted
	holdTicks int
	busy      int // polls on which the Guardian would have held the scripts (StrandLadder.Active)
	giveUps   int // the ladder's "STRANDED" answers
}

// driveEscape runs the ladder against w until the account is out, the ladder gives up or maxTicks pass. others, when
// set, is called each tick and may act on the world (other accounts using doors).
func driveEscape(t *testing.T, w *castleWorld, maxTicks int, others func(tick int)) escapeRun {
	t.Helper()
	return driveEscapeThrough(t, w, maxTicks, others, true)
}

// driveEscapeThrough is driveEscape that, with stopAtGiveUp false, goes on after the ladder says it is giving up (a
// site with an escape plan sleeps and tries again), counting the give-ups.
func driveEscapeThrough(t *testing.T, w *castleWorld, maxTicks int, others func(tick int), stopAtGiveUp bool) escapeRun {
	t.Helper()
	site, ok := StrandSiteAt(3282, 3936, 3)
	if !ok || site.Escape == nil {
		t.Fatal("the castle site has no Escape")
	}
	// Time is the world's: a ladder asleep lets the world idle (sleepThrough), so ticks are counted from the world's
	// clock and not from how many polls the loop made.
	r := escapeRun{firstAct: -1}
	start := w.tick
	for w.tick-start < maxTicks {
		r.ticks = w.tick - start
		if others != nil {
			others(w.tick)
		}
		w.Tick()
		r.ticks = w.tick - start
		if w.outside() {
			r.escaped = true
			return r
		}
		o := StrandObs{
			Now: w.now(), X: w.pos.X, Z: w.pos.Z, Level: w.pos.Level, HP: 30, MaxHP: 30, HPKnown: true,
			Escape: &EscapeObs{Cat: w.cat, State: w.clientState()},
		}
		if w.cat.Inside(w.pos.X, w.pos.Z, w.pos.Level) {
			s := site
			o.Site = &s
		}
		a := r.ladder.Next(o)
		if r.ladder.Active(o.Now) {
			r.busy++
		}
		if o.Now.Before(r.ladder.sleepUntil) && others == nil {
			w.sleepThrough(r.ladder.sleepUntil)
		}
		if (a.Rung == RungWalk || a.Rung == RungClick) && r.firstAct < 0 {
			r.firstAct = r.ticks
		}
		switch a.Rung {
		case RungWalk:
			r.acted++
			path := append([]doorescape.Tile{w.pos}, make([]doorescape.Tile, 0, len(a.Path))...)
			for _, p := range a.Path {
				path = append(path, doorescape.Tile{X: p.X, Z: p.Z, Level: p.Level})
			}
			w.walk(path)
		case RungClick:
			r.acted++
			st := a.Step
			w.opLoc(st.Op, st.LocX, st.LocZ, st.LocID)
		case RungWait:
			if a.Hold {
				r.holdTicks++
			}
		case RungGiveUp:
			r.gaveUp = true
			r.giveUps++
			if stopAtGiveUp {
				return r
			}
		}
		if r.ladder.GaveUp() {
			r.gaveUp = true
			return r
		}
	}
	return r
}

// enterableTiles lists every tile of the castle's compound that an account can stand on with the doors as given.
func enterableTiles(cat *doorescape.Catalog, s doorescape.State) []doorescape.Tile {
	f := cat.FlagsAt(s)
	var out []doorescape.Tile
	for level := 0; level < cat.Grid.Levels; level++ {
		r := cat.Compound[level]
		for z := r.MinZ; z <= r.MaxZ; z++ {
			for x := r.MinX; x <= r.MaxX; x++ {
				if doorescape.Enterable(f, x, z, level) {
					out = append(out, doorescape.Tile{X: x, Z: z, Level: level})
				}
			}
		}
	}
	return out
}
