// Package npcsim is TEST SUPPORT: a small, strict server for the one thing actionsim does not play, an NPC the account
// clicks. Nothing outside a _test.go file imports it.
//
// Engine-TS 289 accepts an NPC option only when Player.inOperableDistance holds: reachedEntity, which wants the two tiles to
// TOUCH orthogonally with no wall between them (a diagonal does not count, and neither does the NPC's own tile). A click from
// anywhere else sets an interaction the server then tries to close with a naive walk (clientRoutefinder is on by default); that
// walk is blocked by every wall and solid tile between the two, and a player that cannot get closer is told "I can't reach
// that!" and dropped. The tests this package serves had no such rule: they faked the whole teleport, or let an option click
// work from any distance, so a click sent from two tiles away (what the Wizards' Guild stretch did, live, 57 times out of 94)
// passed in every one of them. Engine is STRICT, the reviewer's reading: a click read while the account does not reach the NPC
// is answered "I can't reach that!" and does nothing, however near the NPC is.
//
// What it plays, tick by tick (World.cycle order: packets are read first, then the world moves):
//
//   - a walk packet (MOVE_GAMECLICK, MOVE_MINIMAPCLICK, MOVE_OPCLICK) moves the account to its last waypoint, arriving after
//     as many ticks as the route is long (the king-move distance); a newer walk replaces it;
//   - an NPC option packet (OPNPC1..5) is read; the NPC must be in view and the account must reach it, else the chat line;
//   - a packet read while the player is delayed (Delay: p_delay) is dropped without a word, as the real handlers do;
//   - tests schedule what the world does (After: a teleport landing; OnTick: an NPC wandering, every tick) and react to an accepted
//     click (OnOp).
//
// The NPCs are fed to the client the way the server does, as NPC_INFO, so gamestate.State().NPCs() sees them and a walk or a
// teleport rebases them with the scene.
package npcsim

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
)

// CantReach is the chat line the engine answers a click it cannot satisfy with (Player.processInteraction).
const CantReach = "I can't reach that!"

// Click is one NPC option the engine read.
type Click struct {
	// Tick is the engine tick that read it; Option is 1..5.
	Tick, Option int
	// Index is the NPC clicked.
	Index int
	// From is where the account stood when the click was read, NPC where the NPC stood.
	From, NPC tile.Tile
	// Reached is whether the account reached the NPC; Dropped whether the packet was read while the player was delayed (it
	// is then neither reached nor refused).
	Reached, Dropped bool
}

type simNPC struct {
	typ int
	at  tile.Tile
}

type walkState struct {
	dest     tile.Tile
	arriveAt int
}

type event struct {
	at int
	fn func()
}

// Engine is the strict server. Build one with New, place the account (Stand) and the NPCs (Spawn), install what accepting a
// click does (OnOp) and Start it.
type Engine struct {
	// Sim is the session the account's code runs against; New installs its OnSend.
	Sim *actionsim.Session
	// Chat receives the server's game messages (the lines production code reads from its chatlog).
	Chat *chatlog.Log
	// Pack answers "does the engine accept an interaction from here" (pathfind.Pack.CanInteractFrom); nil means plain
	// orthogonal adjacency.
	Pack *pathfind.Pack
	// OnOp, when set, runs for every click the engine ACCEPTED, on the engine's tick, outside its lock (so it may call Delay,
	// Teleport, After, Say and the rest).
	OnOp func(c Click)
	// OnTick, when set, runs at the end of every engine tick, outside its lock: where a test moves an NPC the way the world does
	// (an NPC wandering), after the tick's packets were read.
	OnTick func(tick int)

	every time.Duration

	mu           sync.Mutex
	tick         int
	here         tile.Tile
	order        []int // the NPC indices in the order the client tracks them (NPC_INFO's OldVis order)
	npcs         map[int]*simNPC
	inbox        [][]byte
	walk         *walkState
	delayedUntil int
	events       []event
	clicks       []Click
	walks        []tile.Tile
}

// New is an engine over sim; every is the wall time one engine tick takes once Started.
func New(sim *actionsim.Session, chat *chatlog.Log, pack *pathfind.Pack, every time.Duration) *Engine {
	if every <= 0 {
		every = 20 * time.Millisecond
	}
	e := &Engine{Sim: sim, Chat: chat, Pack: pack, every: every, npcs: map[int]*simNPC{}}
	sim.OnSend = func(b []byte) {
		e.mu.Lock()
		e.inbox = append(e.inbox, append([]byte(nil), b...))
		e.mu.Unlock()
	}
	return e
}

// Start ticks the engine until the test ends.
func (e *Engine) Start(t testing.TB) {
	t.Helper()
	done := make(chan struct{})
	stopped := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		<-stopped
	})
	go func() {
		defer close(stopped)
		tk := time.NewTicker(e.every)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				e.Step()
			}
		}
	}()
}

// Stand puts the account on a tile at once.
func (e *Engine) Stand(at tile.Tile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.moveLocked(at)
}

func (e *Engine) moveLocked(at tile.Tile) {
	e.here = at
	e.Sim.StandAt(at.X, at.Z)
}

// Here is where the account stands.
func (e *Engine) Here() tile.Tile {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.here
}

// Tick is how many ticks the engine has run.
func (e *Engine) Tick() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tick
}

// Clicks is every NPC option read so far.
func (e *Engine) Clicks() []Click {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Click(nil), e.clicks...)
}

// Walks is the destination of every walk packet read (and not dropped).
func (e *Engine) Walks() []tile.Tile {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tile.Tile(nil), e.walks...)
}

// NPCAt is where an NPC stands.
func (e *Engine) NPCAt(index int) tile.Tile {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n := e.npcs[index]; n != nil {
		return n.at
	}
	return tile.Tile{}
}

// Spawn puts an NPC in view. The account must already stand within 15 tiles (NPC_INFO sends the offset in five bits).
func (e *Engine) Spawn(index, typ int, at tile.Tile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.npcs[index] = &simNPC{typ: typ, at: at}
	e.sendLocked(nil, []int{index})
	e.order = append(e.order, index)
}

// MoveNPC walks an NPC to a tile (the client sees it leave and come back there, which is all a reader of NPCs() can tell).
func (e *Engine) MoveNPC(index int, at tile.Tile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.moveNPCLocked(index, at)
}

func (e *Engine) moveNPCLocked(index int, at tile.Tile) {
	n := e.npcs[index]
	if n == nil {
		return
	}
	n.at = at
	e.sendLocked(map[int]bool{index: true}, []int{index})
	var order []int
	for _, i := range e.order {
		if i != index {
			order = append(order, i)
		}
	}
	e.order = append(order, index)
}

// sendLocked sends one NPC_INFO: the tracked NPCs in e.order, those in remove dropped, then those in add (re)entered at
// their tiles relative to the account.
func (e *Engine) sendLocked(remove map[int]bool, add []int) {
	var w bitWriter
	w.put(8, uint32(len(e.order)))
	for _, i := range e.order {
		if remove[i] {
			w.put(1, 1)
			w.put(2, 3)
		} else {
			w.put(1, 0)
		}
	}
	for _, i := range add {
		n := e.npcs[i]
		w.put(14, uint32(i))
		w.put(11, uint32(n.typ))
		w.put(5, uint32((n.at.X-e.here.X)&31))
		w.put(5, uint32((n.at.Z-e.here.Z)&31))
		w.put(1, 0) // no jump
		w.put(1, 0) // no extended info
	}
	w.put(14, 16383)
	// The reader stops when fewer than 22 bits remain; pad so it reads the terminator instead of running off.
	w.put(8, 0)
	e.Sim.State().Handle(serverprot.NPC_INFO, w.buf)
}

// After runs fn n ticks from now, on the engine's tick (outside its lock).
func (e *Engine) After(n int, fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event{at: e.tick + n, fn: fn})
}

// Delay delays the player for n more ticks (p_delay): packets read meanwhile are dropped.
func (e *Engine) Delay(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delayedUntil = e.tick + n
}

// Teleport puts the account on a tile n ticks from now (p_telejump).
func (e *Engine) Teleport(n int, at tile.Tile) {
	e.After(n, func() { e.Stand(at) })
}

// Say sends a game message line to the account.
func (e *Engine) Say(line string) {
	if e.Chat != nil {
		e.Chat.Record(line)
	}
}

// reaches is Player.inOperableDistance for an NPC: strict, and never from the NPC's own tile.
func (e *Engine) reaches(from, npc tile.Tile) bool {
	if from.Level != npc.Level || (from.X == npc.X && from.Z == npc.Z) {
		return false
	}
	if e.Pack != nil {
		return e.Pack.CanInteractFrom(pathfind.NavPoint{X: from.X, Z: from.Z, Level: from.Level}, pathfind.NavPoint{X: npc.X, Z: npc.Z, Level: npc.Level})
	}
	dx, dz := from.X-npc.X, from.Z-npc.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return dx+dz == 1
}

var optionOf = map[int]int{clientprot.OPNPC1: 1, clientprot.OPNPC2: 2, clientprot.OPNPC3: 3, clientprot.OPNPC4: 4, clientprot.OPNPC5: 5}

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Step runs one engine tick. Start calls it; a test that drives time itself calls it directly.
func (e *Engine) Step() {
	e.mu.Lock()
	e.tick++
	var run []func()
	// What the world was scripted to do this tick.
	var due, later []event
	for _, ev := range e.events {
		if ev.at <= e.tick {
			due = append(due, ev)
		} else {
			later = append(later, ev)
		}
	}
	e.events = later
	for _, ev := range due {
		run = append(run, ev.fn)
	}
	// The packets that arrived since the last tick are read now (World.processClientsIn).
	in := e.inbox
	e.inbox = nil
	delayed := e.tick <= e.delayedUntil
	for _, b := range in {
		e.readLocked(b, delayed, &run)
	}
	if w := e.walk; w != nil && e.tick >= w.arriveAt {
		e.walk = nil
		e.moveLocked(w.dest)
	}
	tick := e.tick
	e.mu.Unlock()
	e.Sim.Tick()
	for _, fn := range run {
		fn()
	}
	if e.OnTick != nil {
		e.OnTick(tick)
	}
}

func (e *Engine) readLocked(b []byte, delayed bool, run *[]func()) {
	if len(b) == 0 {
		return
	}
	switch op := int(b[0]); op {
	case clientprot.MOVE_GAMECLICK, clientprot.MOVE_MINIMAPCLICK, clientprot.MOVE_OPCLICK:
		if delayed || len(b) < 7 {
			return
		}
		dest := tile.Tile{X: g2(b, 3), Z: g2(b, 5), Level: e.here.Level}
		for i := 7; i+1 < len(b); i += 2 {
			dest = tile.Tile{X: g2(b, 3) + int(int8(b[i])), Z: g2(b, 5) + int(int8(b[i+1])), Level: e.here.Level}
		}
		dist := absInt(dest.X - e.here.X)
		if dz := absInt(dest.Z - e.here.Z); dz > dist {
			dist = dz
		}
		if dist < 1 {
			dist = 1
		}
		e.walks = append(e.walks, dest)
		e.walk = &walkState{dest: dest, arriveAt: e.tick + dist}
	case clientprot.OPNPC1, clientprot.OPNPC2, clientprot.OPNPC3, clientprot.OPNPC4, clientprot.OPNPC5:
		if len(b) < 3 {
			return
		}
		index := g2(b, 1)
		n := e.npcs[index]
		if n == nil {
			return // not in view: the handler finds no such NPC
		}
		c := Click{Tick: e.tick, Option: optionOf[op], Index: index, From: e.here, NPC: n.at}
		if delayed {
			c.Dropped = true
			e.clicks = append(e.clicks, c)
			return
		}
		c.Reached = e.reaches(e.here, n.at)
		e.clicks = append(e.clicks, c)
		if !c.Reached {
			if e.Chat != nil {
				e.Chat.Record(CantReach)
			}
			return
		}
		// The interaction ends any walk the account was making.
		e.walk = nil
		if e.OnOp != nil {
			on := e.OnOp
			*run = append(*run, func() { on(c) })
		}
	}
}

type bitWriter struct {
	buf []byte
	pos int
}

func (w *bitWriter) put(n int, v uint32) {
	for i := n - 1; i >= 0; i-- {
		if w.pos/8 >= len(w.buf) {
			w.buf = append(w.buf, 0)
		}
		if v&(1<<uint(i)) != 0 {
			w.buf[w.pos/8] |= 0x80 >> uint(w.pos%8)
		}
		w.pos++
	}
}

// OpenPack is a collision pack of open ground over the given mapsquares (mx, mz pairs), level 0: every tile walkable, every
// step legal, no wall. OpenPackWith adds solid tiles and walls.
func OpenPack(squares ...[2]int) (*pathfind.Pack, error) {
	return OpenPackWith(Terrain{}, squares...)
}

// Terrain is what OpenPackWith cuts out of open ground.
type Terrain struct {
	// Solid tiles cannot be stood on and nothing steps into or out of them.
	Solid []tile.Tile
	// Walls stand on a face of a tile and stop every step across it (a door that is shut, a building's side).
	Walls []Wall
}

// Wall is a wall on the Face side of a tile: 0 north (z+1), 1 east (x+1), 2 south, 3 west.
type Wall struct {
	At   tile.Tile
	Face int
}

type square struct {
	exit, walk, wall []byte
}

// OpenPackWith is OpenPack with the terrain cut out of it. Edits that reach into a mapsquare the pack does not hold are ignored.
func OpenPackWith(terrain Terrain, squares ...[2]int) (*pathfind.Pack, error) {
	const exitBytes, walkBytes, wallBytes = 4096, 512, 2048
	// pathfind's direction table: 0 north, 1 east, 2 south, 3 west, then the diagonals NE, SE, SW, NW.
	dx := [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	dz := [8]int{1, 0, -1, 0, 1, -1, -1, 1}
	opposite := [8]int{2, 3, 0, 1, 6, 7, 4, 5}
	// The diagonals a wall on a face also cuts (a step cannot slip across the corner of it).
	cutsDiagonals := [4][2]int{{4, 7}, {4, 5}, {5, 6}, {6, 7}}

	sq := map[[2]int]*square{}
	for _, s := range squares {
		q := &square{exit: make([]byte, exitBytes), walk: make([]byte, walkBytes), wall: make([]byte, wallBytes)}
		for i := range q.exit {
			q.exit[i] = 255
		}
		for i := range q.walk {
			q.walk[i] = 255
		}
		sq[s] = q
	}
	at := func(x, z int) (*square, int) {
		q := sq[[2]int{x >> 6, z >> 6}]
		return q, (x&63)*64 + (z & 63)
	}
	clearExit := func(x, z, dir int) {
		if q, i := at(x, z); q != nil {
			q.exit[i] &^= 1 << uint(dir)
		}
	}
	setWall := func(x, z, face int) {
		if q, i := at(x, z); q != nil {
			if i&1 != 0 {
				q.wall[i>>1] |= byte(1<<uint(face)) << 4
			} else {
				q.wall[i>>1] |= byte(1 << uint(face))
			}
		}
	}
	for _, s := range terrain.Solid {
		if q, i := at(s.X, s.Z); q != nil {
			q.walk[i>>3] &^= 1 << uint(i&7)
			q.exit[i] = 0
		}
		for dir := 0; dir < 8; dir++ {
			clearExit(s.X+dx[dir], s.Z+dz[dir], opposite[dir])
		}
	}
	for _, w := range terrain.Walls {
		f := w.Face & 3
		nx, nz := w.At.X+dx[f], w.At.Z+dz[f]
		setWall(w.At.X, w.At.Z, f)
		setWall(nx, nz, opposite[f])
		clearExit(w.At.X, w.At.Z, f)
		clearExit(nx, nz, opposite[f])
		for _, d := range cutsDiagonals[f] {
			clearExit(w.At.X, w.At.Z, d)
		}
		for _, d := range cutsDiagonals[opposite[f]] {
			clearExit(nx, nz, d)
		}
	}

	// No corner cutting: a diagonal step P to Q across the corner of the 2x2 block P, A, B, Q needs all four cardinal steps round it --
	// P to A and A to Q, P to B and B to Q (the baked pack clears the diagonal bit whenever any of them is blocked, since the engine's
	// canTravel also reads the walls on the destination's near sides), else a step could squeeze between two solid tiles or slip
	// across the corner of a wall into a walled room.
	cardinalOK := func(x, z, dir int) bool {
		q, i := at(x, z)
		return q != nil && q.exit[i]&(1<<uint(dir)) != 0
	}
	diagonalNeeds := [4][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}} // NE, SE, SW, NW
	for key, q := range sq {
		for i := range q.exit {
			x, z := key[0]<<6+i/64, key[1]<<6+i%64
			for k, need := range diagonalNeeds {
				c1, c2 := need[0], need[1]
				if !cardinalOK(x, z, c1) || !cardinalOK(x+dx[c1], z+dz[c1], c2) || !cardinalOK(x, z, c2) || !cardinalOK(x+dx[c2], z+dz[c2], c1) {
					q.exit[i] &^= 1 << uint(4+k)
				}
			}
		}
	}

	raw := []byte{'L', 'C', 'N', 'V', 2, 1, 0, 0, byte(len(squares)), byte(len(squares) >> 8)}
	for _, s := range squares {
		q := sq[s]
		raw = append(raw, byte(s[0]), byte(s[1]), 0b0001)
		raw = append(raw, q.exit...)
		raw = append(raw, q.walk...)
		raw = append(raw, q.wall...)
	}
	return pathfind.Parse(raw)
}
