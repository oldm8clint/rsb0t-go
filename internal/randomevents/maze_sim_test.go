package randomevents

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/randomevents/mazegraph"
)

// mazeSim is an offline stand-in for the engine's side of the maze random event, built from
// the same static layout the solver routes over (mazegraph.MazeLayout) and from the engine and
// content sources the solver is written against, so the live failures in run/fleet.log on
// 3 October 2026 can be replayed tick by tick with no server:
//
//   - Engine-TS MoveClickHandler: a MOVE_GAMECLICK is dropped while the player is delayed; else
//     every waypoint is queued, the first included, and the pending interaction is cleared.
//   - PathingEntity.validateAndAdvanceStep/takeStep: one step a tick (two running), toward the
//     current waypoint (diagonal, else along x, else along z); a waypoint the player already
//     stands on is used up with no step that tick and no run step; a blocked step stays put and
//     keeps the waypoints.
//   - OpLocHandler: dropped while delayed or when the loc is not there (a door is an inviswall
//     for 3 ticks after it opens). Player.processInteraction: a loc op fires only after movement,
//     on a tick the player took no step, from a tile that reaches the wall (its own tile or the
//     tile across it); with no waypoints left and out of reach it is dropped.
//   - macro_event_maze.rs2 [oploc1,_macro_maze_wall_door] and door_procs/open_and_close_doors:
//     p_arrivedelay (one tick's delay when the player moved last tick), p_walk(loc_coord),
//     check_axis, the approach_direction gate (a wrong side opens a mesbox), then
//     open_and_close_door: p_teleport across (via the door tile and p_delay(1) when entering from
//     a flank), loc_change(inviswall, 3) and loc_add of the open leaf on the tile across the door
//     at angle+1 for 3 ticks -- a wall that blocks one edge of that tile while it stands.
//   - [oploc1,macro_maze_complete]: if_close, then ~end_macro_maze's p_delay(5) and the return
//     teleport out of the mapsquare.
//
// The account's own script can be made to run alongside (interfere), the way the live
// webwalker kept walking while the Guardian solved (see TestMazeSimScriptWalkingAlongside).
type mazeSim struct {
	t      *testing.T
	g      *mazegraph.Graph
	st     *gamestate.State
	cheats anticheat.Counters

	now, nextTick time.Time
	tick          int
	// latency is the one-way network delay, both ways; toClient holds updates in flight.
	latency  time.Duration
	toClient []simDelivery
	// jitter, when set, makes some ticks late by up to simJitter (seeded, so replayable).
	jitter *rand.Rand

	pos          mazegraph.Cell
	out          bool
	wps          []mazegraph.Cell
	run, tempRun bool
	delayedUntil int
	lastMovement int
	modal        bool
	inbox        []simPacket
	opDoor       *mazegraph.DoorInfo
	opShrine     bool
	resume       func()
	tempEdges    map[string]int
	doorGone     map[mazegraph.Cell]int
	exitAt       int

	// legacy selects the older model the first replays were written against: a dialogue blocks every
	// op but is closed only by CLOSE_MODAL / continue (no click closes it), and MOVE_OPCLICK is not a
	// packet it knows. The default is the reference engine (Engine-TS 289): an op click and every
	// walk but MOVE_OPCLICK close the dialogue as the packet is read.
	legacy bool
	// refuseOpWalk drops MOVE_OPCLICK on an otherwise reference engine: a server that does not
	// honour a walk with a dialogue up.
	refuseOpWalk bool
	// dropWall ignores every click on a maze wall (the loc is not there for the server).
	dropWall bool
	// opClickCloses makes MOVE_OPCLICK walk the player but close the dialogue like any other walk.
	opClickCloses bool

	// The reward: [mapzone,0_45_71] is an engine-queue script that runs once the player can be
	// accessed (so not while the briefing is up) and starts macro_maze's timer, a NORMAL timer that
	// takes 1 off pct every 5 ticks it can run.
	pct            int
	mapzonePending bool
	timerOn        bool
	timerClock     int
	fires          []int // ticks the timer ran
	rewardPct      int   // pct when the statue was touched
	opWall         *mazegraph.Cell
	opChest        *mazegraph.Cell
	chestEmpty     map[mazegraph.Cell]int // tick a chest is full again
	pack           map[int]int32
	ballast        int // backpack slots taken by things that are not chest prizes
	prizes         []simPrize
	wallClicks     int                     // OPLOCs on a maze wall that reached the server
	chestClicks    int                     // OPLOCs on a closed chest that reached the server
	chestShown     map[mazegraph.Cell]bool // chests the client has been told are empty
	opClicksIn     int                     // MOVE_OPCLICKs the server acted on
	opClicksOut    int                     // MOVE_OPCLICKs the server dropped

	// interfere runs at the start of every tick; it may send through the raw session.
	interfere      func(*mazeSim)
	fromScript     bool
	scriptSent     int // packets the interferer sent
	scriptArrived  int // of those, how many reached the simulated server
	movesDropped   int // MOVE_GAMECLICKs the server dropped (delayed)
	firstStepWaste int // waypoints that were the player's own tile when queued
	crossings      []string
}

type simPacket struct {
	b          []byte
	fromScript bool
	at         time.Time // when it reaches the server
}

type simDelivery struct {
	at    time.Time
	apply func()
}

const (
	simTick   = 600 * time.Millisecond
	simJitter = 250 * time.Millisecond
)

var simBase = mazegraph.Cell{X: (364 - 6) * 8, Z: (572 - 6) * 8} // REBUILD_NORMAL zone 364,572

func newMazeSim(t *testing.T, spawn mazegraph.Cell, run bool) *mazeSim {
	t.Helper()
	start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	m := &mazeSim{
		t: t, g: mazegraph.BuildLayoutGraph(), st: gamestate.New(),
		now: start, nextTick: start.Add(simTick),
		pos: spawn, run: run, modal: true,
		tempEdges: map[string]int{}, doorGone: map[mazegraph.Cell]int{},
		pct: 100, mapzonePending: true, chestEmpty: map[mazegraph.Cell]int{}, pack: map[int]int32{},
		chestShown: map[mazegraph.Cell]bool{},
	}
	w := packet.NewWriter(4)
	w.P2(364)
	w.P2(572)
	m.st.Handle(serverprot.REBUILD_NORMAL, w.Data[:w.Pos])
	m.feedPos()
	// The server sends the backpack at login: the client knows it before the maze.
	m.feedPack()
	m.feedDialogue(simBriefingID) // the Mysterious Old Man's briefing, a chatnpc, is open on arrival
	return m
}

func (m *mazeSim) State() *gamestate.State     { return m.st }
func (m *mazeSim) Cheats() *anticheat.Counters { return &m.cheats }

func (m *mazeSim) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	m.inbox = append(m.inbox, simPacket{b: append([]byte(nil), w.Data[:w.Pos]...), fromScript: m.fromScript, at: m.now.Add(m.latency)})
	if m.fromScript {
		m.scriptSent++
	}
	return nil
}

func (m *mazeSim) Now() time.Time { return m.now }

// Sleep advances the simulated clock, running every server tick and every delivery to the
// client (a tick's position or interface update, latency after the tick) that falls due.
func (m *mazeSim) Sleep(d time.Duration) {
	target := m.now.Add(d)
	for {
		if len(m.toClient) > 0 && !m.toClient[0].at.After(target) && !m.toClient[0].at.After(m.nextTick) {
			ev := m.toClient[0]
			m.toClient = m.toClient[1:]
			m.now = ev.at
			ev.apply()
			continue
		}
		if m.nextTick.After(target) {
			break
		}
		m.now = m.nextTick
		m.step()
		next := simTick
		if m.jitter != nil {
			// A busy server's tick lands late now and then; it never runs early.
			next += time.Duration(m.jitter.IntN(int(simJitter/time.Millisecond)+1)) * time.Millisecond
		}
		m.nextTick = m.nextTick.Add(next)
	}
	m.now = target
}

// deliver hands an update to the client latency from now.
func (m *mazeSim) deliver(apply func()) {
	if m.latency == 0 {
		apply()
		return
	}
	m.toClient = append(m.toClient, simDelivery{at: m.now.Add(m.latency), apply: apply})
}

func (m *mazeSim) delayed() bool { return m.tick < m.delayedUntil }

func (m *mazeSim) step() {
	m.tick++
	if m.interfere != nil && !m.out {
		m.fromScript = true
		m.interfere(m)
		m.fromScript = false
	}
	if m.resume != nil && !m.delayed() {
		r := m.resume
		m.resume = nil
		r()
	}
	inbox := m.inbox
	m.inbox = nil
	for _, p := range inbox {
		if p.at.After(m.now) {
			m.inbox = append(m.inbox, p) // still on the wire
			continue
		}
		m.handle(p)
	}
	for k, until := range m.tempEdges {
		if m.tick >= until {
			delete(m.tempEdges, k)
		}
	}
	for k, until := range m.doorGone {
		if m.tick >= until {
			delete(m.doorGone, k)
		}
	}
	if !m.out {
		m.processTimer()
		m.process()
		m.revertChests()
	}
	if m.exitAt > 0 && m.tick >= m.exitAt {
		m.exitAt = 0
		m.out = true
		m.deliver(func() {
			w := packet.NewWriter(4)
			w.P2(uint16(3222 >> 3))
			w.P2(uint16(3218 >> 3))
			m.st.Handle(serverprot.REBUILD_NORMAL, w.Data[:w.Pos])
			m.st.Handle(serverprot.PLAYER_INFO, simBits(
				[2]uint32{1, 1}, [2]uint32{2, 3}, [2]uint32{2, 0}, [2]uint32{7, 48}, [2]uint32{7, 48},
				[2]uint32{1, 0}, [2]uint32{1, 0}, [2]uint32{8, 0}, [2]uint32{11, 2047}))
		})
		return
	}
	if !m.out {
		m.feedPos()
	}
}

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (m *mazeSim) handle(p simPacket) {
	b := p.b
	if len(b) == 0 {
		return
	}
	switch {
	case b[0] == clientprot.MOVE_GAMECLICK || b[0] == clientprot.MOVE_OPCLICK:
		opClick := b[0] == clientprot.MOVE_OPCLICK
		if p.fromScript {
			m.scriptArrived++
		}
		if m.delayed() {
			m.movesDropped++
			return
		}
		if opClick && (m.legacy || m.refuseOpWalk) {
			m.opClicksOut++
			return
		}
		n := (int(b[1]) - 3) / 2
		sx, sz := g2(b, 3), g2(b, 5)
		wps := []mazegraph.Cell{{X: sx, Z: sz}}
		for i := 0; i < n-1; i++ {
			wps = append(wps, mazegraph.Cell{X: sx + int(int8(b[7+2*i])), Z: sz + int(int8(b[8+2*i]))})
		}
		if wps[0] == m.pos {
			m.firstStepWaste++
		}
		m.wps = wps
		m.tempRun = b[2] == 1
		if opClick {
			m.opClicksIn++
			if m.opClickCloses {
				m.feedModal(false)
			}
			break // MoveClickHandler: "if (!message.opClick) player.clearPendingAction()"
		}
		m.opDoor, m.opShrine, m.opWall, m.opChest = nil, false, nil, nil
		if !m.legacy {
			m.feedModal(false) // clearPendingAction -> closeModal
		}
	case b[0] == clientprot.CLOSE_MODAL || b[0] == clientprot.RESUME_PAUSEBUTTON:
		if p.fromScript {
			m.scriptArrived++
		}
		m.feedModal(false)
	case len(b) >= 7 && b[len(b)-7] == clientprot.OPLOC1:
		if p.fromScript {
			m.scriptArrived++
		}
		if m.delayed() {
			return
		}
		x, z, id := g2(b, len(b)-6), g2(b, len(b)-4), g2(b, len(b)-2)
		if !m.legacy {
			m.feedModal(false) // OpLocHandler: clearPendingAction -> closeModal
			m.opDoor, m.opShrine, m.opWall, m.opChest = nil, false, nil, nil
		}
		switch {
		case id == mazegraph.WallID:
			m.wallClicks++
			if m.dropWall {
				return
			}
			if m.legacy {
				m.feedModal(true) // ~mesbox("I can't open that.")
				return
			}
			if c := (mazegraph.Cell{X: x, Z: z}); mazegraph.WallAnchoredAt(c) {
				m.opWall = &c
			}
		case id == mazegraph.ChestLocID:
			m.chestClicks++
			c := mazegraph.Cell{X: x, Z: z}
			if !mazegraph.IsChest(c) || m.tick < m.chestEmpty[c] {
				return // World.getLoc finds no closed chest there
			}
			m.opChest = &c
		case id == mazegraph.ShrineLocID && x == mazegraph.Shrine.X && z == mazegraph.Shrine.Z:
			m.opShrine, m.opDoor = true, nil
		default:
			if _, gone := m.doorGone[mazegraph.Cell{X: x, Z: z}]; gone {
				return
			}
			for _, d := range m.g.Door {
				if d.Tile.X == x && d.Tile.Z == z && d.ID == id {
					dc := d
					m.opDoor, m.opShrine = &dc, false
					return
				}
			}
		}
	}
}

func (m *mazeSim) process() {
	moved := 0
	// Player.updateMovement: a busy player whose click set moveClickRequest does not move while a
	// script is queued -- here the mapzone script, which waits for the briefing to go.
	queuedAndBusy := !m.legacy && m.modal && m.mapzonePending
	if !m.delayed() && len(m.wps) > 0 && !queuedAndBusy {
		steps := 1
		if m.run || m.tempRun {
			steps = 2
		}
		for i := 0; i < steps && len(m.wps) > 0; i++ {
			wp := m.wps[0]
			if wp == m.pos {
				m.wps = m.wps[1:]
				break
			}
			next, ok := m.takeStep(wp)
			if !ok {
				break
			}
			m.pos = next
			moved++
			if m.pos == wp {
				m.wps = m.wps[1:]
			}
		}
		if moved > 0 {
			m.lastMovement = m.tick + 1
		}
	}
	if m.delayed() || m.modal || moved > 0 {
		return
	}
	switch {
	case m.opDoor != nil:
		d := *m.opDoor
		own, across := d.Sides()
		if m.pos == own || m.pos == across {
			m.opDoor, m.wps = nil, nil
			m.doorScript(d)
		} else if len(m.wps) == 0 {
			m.opDoor = nil // "I can't reach that!"
		}
	case m.opWall != nil:
		if m.pos == *m.opWall {
			m.opWall, m.wps = nil, nil
			m.feedModal(true) // ~mesbox("I can't open that.")
		} else if len(m.wps) == 0 {
			m.opWall = nil // "I can't reach that!"
		}
	case m.opChest != nil:
		c := *m.opChest
		stand := false
		for _, s := range m.g.ChestStands(c) {
			stand = stand || s == m.pos
		}
		if stand {
			m.opChest, m.wps = nil, nil
			m.chestScript(c)
		} else if len(m.wps) == 0 {
			m.opChest = nil
		}
	case m.opShrine:
		if chebyshev(m.pos.X, m.pos.Z, mazegraph.Shrine.X+1, mazegraph.Shrine.Z+1) <= 2 {
			m.opShrine, m.wps = false, nil
			m.feedModal(false)
			m.rewardPct, m.timerOn = m.pct, false // end_macro_maze clears the timer before it pays
			m.exitAt = m.tick + 6
		} else if len(m.wps) == 0 {
			m.opShrine = false
		}
	}
}

func (m *mazeSim) doorScript(d mazegraph.DoorInfo) {
	if m.lastMovement >= m.tick { // p_arrivedelay
		m.delayedUntil = m.tick + 1
		m.resume = func() { m.doorBody(d) }
		return
	}
	m.doorBody(d)
}

func simDoorOpen(angle int) (int, int) {
	switch angle {
	case 0:
		return -1, 0
	case 1:
		return 0, 1
	case 2:
		return 1, 0
	default:
		return 0, -1
	}
}

func (m *mazeSim) doorBody(d mazegraph.DoorInfo) {
	// p_walk(loc_coord): from anywhere but the door tile, the door tile is behind the closed
	// door, so the engine's nearest-reachable route is the tile the player already stands on.
	m.wps = nil
	side := m.pos.Z == d.Tile.Z
	if d.Angle == 0 || d.Angle == 2 {
		side = m.pos.X == d.Tile.X
	}
	dir := mazegraph.DoorDirs[d.ID]
	if !(dir == 0 || (dir == 1 && side) || (dir == 2 && !side)) {
		m.feedModal(true) // "I don't think that's the right way."
		return
	}
	ox, oz := simDoorOpen(d.Angle)
	leaf := mazegraph.Cell{X: d.Tile.X + ox, Z: d.Tile.Z + oz}
	finish := func(dest mazegraph.Cell) {
		from := m.pos
		m.pos = dest
		m.doorGone[d.Tile] = m.tick + 3
		lx, lz := simDoorOpen((d.Angle + 1) % 4)
		m.tempEdges[mazegraph.EdgeKey(leaf.X, leaf.Z, leaf.X+lx, leaf.Z+lz)] = m.tick + 3
		m.crossings = append(m.crossings, fmt.Sprintf("door (%d,%d) %v->%v t%d", d.Tile.X, d.Tile.Z, from, dest, m.tick))
	}
	if side {
		if m.pos != d.Tile {
			m.pos = d.Tile
			m.delayedUntil = m.tick + 2
			m.resume = func() { finish(leaf) }
			return
		}
		finish(leaf)
		return
	}
	finish(d.Tile)
}

func (m *mazeSim) open(a, b mazegraph.Cell) bool {
	k := mazegraph.EdgeKey(a.X, a.Z, b.X, b.Z)
	if m.g.WallEdge[k] {
		return false
	}
	if _, door := m.g.Door[k]; door {
		return false
	}
	if _, temp := m.tempEdges[k]; temp {
		return false
	}
	if m.g.Solid[b] { // a chest is a centrepiece loc: its tile blocks
		return false
	}
	// The shrine's 3x3 is solid.
	in := func(c mazegraph.Cell) bool {
		return c.X >= mazegraph.Shrine.X && c.X < mazegraph.Shrine.X+3 && c.Z >= mazegraph.Shrine.Z && c.Z < mazegraph.Shrine.Z+3
	}
	return !in(b) || in(a)
}

func (m *mazeSim) takeStep(wp mazegraph.Cell) (mazegraph.Cell, bool) {
	dx, dz := sgn(wp.X-m.pos.X), sgn(wp.Z-m.pos.Z)
	p := m.pos
	ax, az := mazegraph.Cell{X: p.X + dx, Z: p.Z}, mazegraph.Cell{X: p.X, Z: p.Z + dz}
	diag := mazegraph.Cell{X: p.X + dx, Z: p.Z + dz}
	if dx != 0 && dz != 0 && m.open(p, ax) && m.open(ax, diag) && m.open(p, az) && m.open(az, diag) {
		return diag, true
	}
	if dx != 0 && m.open(p, ax) {
		return ax, true
	}
	if dz != 0 && m.open(p, az) {
		return az, true
	}
	return p, false
}

func simBits(fields ...[2]uint32) []byte {
	var out []byte
	pos := 0
	for _, f := range fields {
		for i := int(f[0]) - 1; i >= 0; i-- {
			for len(out) <= pos/8 {
				out = append(out, 0)
			}
			if (f[1]>>uint(i))&1 == 1 {
				out[pos/8] |= 0x80 >> uint(pos%8)
			}
			pos++
		}
	}
	return out
}

func (m *mazeSim) feedPos() {
	lx, lz := m.pos.X-simBase.X, m.pos.Z-simBase.Z
	m.deliver(func() {
		m.st.Handle(serverprot.PLAYER_INFO, simBits(
			[2]uint32{1, 1}, [2]uint32{2, 3}, [2]uint32{2, 0}, [2]uint32{7, uint32(lx)}, [2]uint32{7, uint32(lz)},
			[2]uint32{1, 0}, [2]uint32{1, 0}, [2]uint32{8, 0}, [2]uint32{11, 2047}))
	})
}

// feedModal opens (a mesbox) or closes the chat dialogue.
func (m *mazeSim) feedModal(open bool) {
	if open {
		m.feedDialogue(simMesboxID)
		return
	}
	m.modal = false
	m.deliver(func() { m.st.Handle(serverprot.IF_CLOSE, nil) })
}

// The chat interfaces the replay opens: the Old Man's briefing (a chatnpc), a mesbox and the
// prize box (an objbox) are three different interfaces, so a dialogue that replaces another one
// is a visible change of id, as it is live.
const (
	simBriefingID = 2459
	simMesboxID   = 4887
	simObjboxID   = 4884
)

// feedDialogue opens the chat interface id.
func (m *mazeSim) feedDialogue(id int) {
	m.modal = true
	m.deliver(func() {
		w := packet.NewWriter(2)
		w.P2(uint16(id))
		m.st.Handle(serverprot.IF_OPENCHAT, w.Data[:w.Pos])
	})
}

// simPrize is what one chest opening gave.
type simPrize struct {
	id    int
	count int32
	name  string
}

// simChestPrizes are macro_maze_calculate_chest_loot's eight free-to-play prizes in table order
// (the engine rolls one at random; the replay walks the table so every run is the same).
var simChestPrizes = []simPrize{
	{556, 15, "Air rune"}, {555, 10, "Water rune"}, {557, 10, "Earth rune"}, {554, 10, "Fire rune"},
	{882, 20, "Bronze arrow"}, {9140, 10, "Bolt"}, {884, 15, "Iron arrow"}, {119, 1, "Strength potion(2)"},
}

// emptyChest is loc_change(macro_maze_chestempty, 100) on a chest: it is the empty chest for 100 ticks
// and, when seen, every client in range is told.
func (m *mazeSim) emptyChest(c mazegraph.Cell, seen bool) {
	m.chestEmpty[c] = m.tick + mazegraph.ChestRespawnTicks
	if seen {
		m.chestShown[c] = true
		m.feedChest(c, true)
	}
}

// revertChests puts a chest back (and tells the clients that were told it was empty) when its
// 100 ticks are up.
func (m *mazeSim) revertChests() {
	for c := range m.chestShown {
		if m.tick >= m.chestEmpty[c] {
			delete(m.chestShown, c)
			m.feedChest(c, false)
		}
	}
}

// feedChest is the LOC_ADD_CHANGE the server sends for a chest changing between the closed chest
// and the empty one (an UPDATE_ZONE_PARTIAL_ENCLOSED for the chest's 8x8 zone).
func (m *mazeSim) feedChest(c mazegraph.Cell, empty bool) {
	id := mazegraph.ChestLocID
	if empty {
		id = mazegraph.ChestEmptyLocID
	}
	m.deliver(func() {
		lx, lz := c.X-simBase.X, c.Z-simBase.Z
		zx, zz := (lx>>3)<<3, (lz>>3)<<3
		w := packet.NewWriter(16)
		w.P1(byte(zx))
		w.P1(byte(zz))
		w.P1(serverprot.LOC_ADD_CHANGE)
		w.P1(byte((lx-zx)<<4 | (lz - zz)))
		w.P1(10 << 2)
		w.P2(uint16(id))
		m.st.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, w.Data[:w.Pos])
	})
}

// accessible is Player.canAccess: not delayed and no dialogue up.
func (m *mazeSim) accessible() bool { return !m.delayed() && !m.modal }

// processTimer is the part of World.processPlayers between the queues and the interaction:
// processTimers(NORMAL) -- a due timer runs once, only if the player can be accessed, and its
// clock restarts only when it runs -- then processEngineQueue, whose [mapzone] script starts the
// timer on the first accessible tick.
func (m *mazeSim) processTimer() {
	if m.timerOn && m.accessible() && m.tick >= m.timerClock+5 {
		m.timerClock = m.tick
		if m.pct > 0 {
			m.pct--
		}
		m.fires = append(m.fires, m.tick)
		m.feedPct()
	}
	if m.mapzonePending && m.accessible() {
		m.mapzonePending = false
		m.timerOn, m.timerClock = true, m.tick
		m.feedPct()
	}
}

// feedPct is [proc,update_macro_maze_progress]: if_settext(mazetimer:com_2, "<%xplamp>%").
func (m *mazeSim) feedPct() { m.feedPctText(fmt.Sprintf("%d%%", m.pct)) }

// feedPctText sets the overlay text on the client.
func (m *mazeSim) feedPctText(text string) {
	m.deliver(func() {
		w := packet.NewWriter(16)
		w.P2(6572)
		w.PJStr(text)
		m.st.Handle(serverprot.IF_SETTEXT, w.Data[:w.Pos])
	})
}

func (m *mazeSim) feedPack() {
	m.deliver(func() {
		var items [][2]int32
		for i := 0; i < m.ballast; i++ {
			items = append(items, [2]int32{int32(2000 + i), 1})
		}
		for _, p := range simChestPrizes {
			if n := m.pack[p.id]; n > 0 {
				items = append(items, [2]int32{int32(p.id), n})
			}
		}
		for len(items) < 28 { // UPDATE_INV_FULL carries every slot, the empty ones as id 0
			items = append(items, [2]int32{0, 0})
		}
		setFullInventory(m.st, 3214, items...)
	})
}

// chestScript is [oploc1,macro_maze_chest] -> ~macro_maze_open_chest: p_arrivedelay, anim,
// p_delay(0), loc_change to the empty chest for 100 ticks, then the prize (an objbox, a dialogue)
// while %xplamp is above 0.
func (m *mazeSim) chestScript(c mazegraph.Cell) {
	if m.lastMovement >= m.tick { // p_arrivedelay
		m.delayedUntil = m.tick + 1
		m.resume = func() { m.chestScript(c) }
		return
	}
	m.delayedUntil = m.tick + 1 // p_delay(0)
	m.resume = func() {
		m.emptyChest(c, true)
		if m.pct <= 0 {
			m.feedModal(true) // "Awww, it's empty!"
			return
		}
		p := simChestPrizes[len(m.prizes)%len(simChestPrizes)]
		m.prizes = append(m.prizes, p)
		m.pack[p.id] += p.count
		m.feedPack()
		m.feedDialogue(simObjboxID)
	}
}

// solve runs SolveMazeEventVerbose against the simulator on its simulated clock and returns
// the error, the solver's log and the simulated time from the call to the shrine touch landing.
func (m *mazeSim) solve() (error, []string, time.Duration) {
	m.t.Helper()
	prev := mazeClock
	mazeClock = m
	defer func() { mazeClock = prev }()
	var lines []string
	start := m.now
	var touched time.Duration
	_, err := SolveMazeEventVerbose(m, nil, 0, func(s string) {
		lines = append(lines, fmt.Sprintf("t%d %s", m.tick, s))
		if touched == 0 && strings.HasPrefix(s, "touching shrine") {
			touched = m.now.Sub(start)
		}
	})
	return err, lines, touched
}

func failureLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "could not stand") || strings.Contains(l, "stalled") || strings.Contains(l, "no open-floor") ||
			strings.Contains(l, "no reaction") || strings.Contains(l, "refused") || strings.Contains(l, "re-routing") {
			out = append(out, l)
		}
	}
	return out
}

// TestMazeSimSolvesFromEverySpawn replays a clean solve from each of the four
// macro_maze_teleports corners, walking and running. The solve must finish with no failed
// step or re-route, cross every door onto the tile across it, and touch the shrine.
func TestMazeSimSolvesFromEverySpawn(t *testing.T) {
	for _, lat := range simLatencies {
		for _, run := range []bool{false, true} {
			for _, sp := range mazegraph.Spawns {
				t.Run(fmt.Sprintf("%v_run=%v_lat=%v", sp, run, lat), func(t *testing.T) {
					m := newClassicSim(t, sp, run)
					m.latency = lat
					touched := solveClean(t, m)
					// The time the reward is draining for: on time ticks a 16-door route is
					// 65-71s and the 18-door one 88s (a little more with latency); the old
					// glue took 85-109s running and 124-169s walking. This bounds a regression
					// such as a sleep put back after every door (16 x 0.6s).
					budget := 76 * time.Second
					if sp == mazegraph.Spawns[3] {
						budget = 93 * time.Second
					}
					if touched > budget {
						t.Errorf("shrine touched after %.1fs, over the %.0fs budget", touched.Seconds(), budget.Seconds())
					}
				})
			}
		}
	}
}

// TestMazeSimSolvesWithLateTicks replays every spawn on a server whose ticks land up to
// simJitter late, over many seeds and latencies. This is the timing the clean replays above
// never hit and the live server did: on 3 October seven solves "opened" door (2895,4598)
// and landed on (2897,4599), the tile the player had just run past, and then found no path to
// the next door (also (2900,4567) -> (2899,4569), (2912,4584) -> (2914,4585), (2906,4586) ->
// (2904,4584)). The leaf of the door just opened stood on the player's tile for 3 ticks and
// blocked the first step of the next walk, the walk's stall check re-sent the SAME waypoints
// (whose first is the tile the walk started on) just as the player got going, the player ran
// back toward it, and the door click that followed was read as a crossing. Run against the
// old glue (62441dba), this sweep failed 2 of its 144 runs (NW and SW spawns, seed 7, 160ms)
// with the live signature: "door (2898,4600) ... opened from (2898,4600) -> (2897,4600)", then
// "no open-floor path from (2897,4600) to (2895,4598)".
func TestMazeSimSolvesWithLateTicks(t *testing.T) {
	for seed := uint64(1); seed <= 12; seed++ {
		for _, lat := range []time.Duration{20 * time.Millisecond, 90 * time.Millisecond, 160 * time.Millisecond} {
			for _, sp := range mazegraph.Spawns {
				t.Run(fmt.Sprintf("%v_seed=%d_lat=%v", sp, seed, lat), func(t *testing.T) {
					m := newClassicSim(t, sp, seed%2 == 0)
					m.latency = lat
					m.jitter = rand.New(rand.NewPCG(seed, uint64(lat)))
					solveClean(t, m)
				})
			}
		}
	}
}

// simLatencies are the one-way network delays the replays sweep: the live fleet's own
// round trips to the live server vary, and the 3 October failures depended on where in the 600ms tick
// the solver's packets landed.
var simLatencies = []time.Duration{0, 30 * time.Millisecond, 70 * time.Millisecond, 120 * time.Millisecond, 180 * time.Millisecond, 250 * time.Millisecond}

func solveClean(t *testing.T, m *mazeSim) time.Duration {
	t.Helper()
	sp, run := m.pos, m.run
	err, lines, touched := m.solve()
	if err != nil {
		t.Fatalf("solve failed: %v\n%s", err, strings.Join(lines, "\n"))
	}
	if !m.out {
		t.Fatalf("never left the maze\n%s", strings.Join(lines, "\n"))
	}
	if f := failureLines(lines); len(f) > 0 {
		t.Errorf("solve had %d failed step(s):\n%s\nfull log:\n%s", len(f), strings.Join(f, "\n"), strings.Join(lines, "\n"))
	}
	if m.firstStepWaste > 0 {
		t.Errorf("%d walk(s) began with a waypoint on the player's own tile, a tick lost each", m.firstStepWaste)
	}
	t.Logf("spawn %v run=%v lat=%v: shrine touched after %.1fs simulated, %d doors, %d MOVEs dropped",
		sp, run, m.latency, touched.Seconds(), len(m.crossings), m.movesDropped)
	return touched
}

// TestMazeSimScriptWalkingAlongside replays the dominant live failure: the account's own
// script (a webwalker mid-walk when the maze teleport landed) kept sending MOVE_GAMECLICKs
// toward home and opening "shut walls" (the maze's wall locs, whose op is a mesbox) while the
// Guardian solved. On 3 October 18 of the 25 failed maze sessions had script walk lines inside
// the handling window ("walk to 3222,3218,0: scene/unstick/backoff", "stalled near shut Wall at
// 2888,4574, opening it"), and the account was dragged to dead ends such as (2888,4574).
// Against the old glue (62441dba) this failed all four spawns with the live signatures:
// "walk leg to (2903,4555) stalled at (2891,4555)", "walk leg to (2924,4555) stalled at
// (2915,4555)", "no open-floor path from ... within the current room".
func TestMazeSimScriptWalkingAlongside(t *testing.T) {
	for _, sp := range mazegraph.Spawns {
		t.Run(fmt.Sprint(sp), func(t *testing.T) {
			m := newClassicSim(t, sp, true)
			tries := 0
			m.interfere = func(m *mazeSim) {
				if m.tick%6 == 0 || m.tick%12 == 3 {
					tries++
				}
				switch m.tick % 6 {
				case 0: // the walker's scene/unstick click toward home, south-west of the maze
					_ = actions.Walk(m, []pathfind.NavPoint{{X: m.pos.X, Z: m.pos.Z}, {X: m.pos.X - 9, Z: m.pos.Z - 9}}, true)
				case 3:
					if m.tick%12 == 3 { // "stalled near shut Wall ..., opening it"
						_ = actions.OpLoc(m, 1, m.pos.X, m.pos.Z, mazegraph.WallID)
					}
				}
			}
			err, lines, touched := m.solve()
			if err != nil {
				t.Fatalf("solve failed with the script walking alongside: %v\n%s", err, strings.Join(lines, "\n"))
			}
			if tries == 0 {
				t.Fatal("the script never tried to click")
			}
			if m.scriptArrived != 0 {
				t.Errorf("%d of the script's %d clicks reached the server while the maze was being solved", m.scriptArrived, tries)
			}
			if !strings.Contains(strings.Join(lines, "\n"), "held off") {
				t.Errorf("no line says the script's clicks were held off:\n%s", strings.Join(lines, "\n"))
			}
			t.Logf("spawn %v: solved in %.1fs simulated; the script tried %d clicks, none reached the server", sp, touched.Seconds(), tries)
		})
	}
}
