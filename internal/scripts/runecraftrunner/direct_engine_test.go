package runecraftrunner

// THE NATURE TEMPLE AGAINST A MODEL OF THE ENGINE'S MOVEMENT AND INTERACTION (CR-1, runecraftplan/direct.go).
//
// The plain engine of runner_test.go accepts an altar click from anywhere. The direct ops walk, so this one plays the rules they depend on,
// tick by tick, from Engine-TS 289:
//
//   - a MOVE_OPCLICK queues its waypoints (MoveClickHandler; the first waypoint is the start of the path and a start the player stands on
//     costs the tick's step to pop it), walked one tile a tick, or two when the click ran, by PathingEntity.takeStep's greedy chain
//     (pathfind.Pack.EngineStep);
//   - an OPLOC sets the interaction and NOTHING ELSE: the engine does not path to a loc (Player.pathToPathingTarget returns for a target
//     that is not a PathingEntity), so a click with no walk behind it from a tile that does not reach the loc ends in "I can't reach that!";
//   - Player.processInteraction: movement, THEN tryInteract(stepsTaken === 0) -- a loc's op runs in a tick in which the player took no
//     step, so the tick AFTER the last step -- or, with no steps, no waypoints and no reach, "I can't reach that!" and the interaction is
//     dropped;
//   - the craft is p_delay(3): a packet READ in the next four ticks is dropped without a word, the walk already queued carries on, and
//     the interaction is not processed while the player is delayed;
//   - a cast (OPHELDT, CX-1) clears the interaction and keeps the walk, locks the player for p_delay(1) (Low Level Alchemy) or p_delay(3)
//     (High), and a movement click or an op read in the SAME tick as a cast that delayed the player has the walk thrown away
//     (World.processClientsIn: `if (player.userPath.length > 0 || player.opcalled) { if (player.delayed) player.unsetMapFlag() }`);
//   - the uplink can be slow (lagTicks): a message written between two ticks is read that many ticks later than the next one, which is a
//     round trip of more than a tick for a client that reacts to a frame the moment it sees it.
//
// The packets are split the way the engine reads a websocket message (actionsim.Split), so a walk and a click written together are read in
// the same tick, in order.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/pathfind/packtest"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// dirTick is the movement engine's tick. It is twice the other engines' (engineTick): a walk and a hold are a dozen ticks, and a
// scheduling hiccup of the test machine must be a small fraction of one.
const dirTick = 2 * engineTick

// dirTickDur is the tick a dirEngine made now runs at: dirTick, unless a test that reads exact ticks off a wall clock slows it.
var dirTickDur = dirTick

var (
	// The Nature temple in miniature, in its real coordinates: the arrival tile, the stand four steps north of it that the altar is
	// clicked from, and the portal's stand (the arrival tile itself).
	natStandAltar  = tile.Tile{X: 2400, Z: 4839}
	natStandPortal = tile.Tile{X: 2400, Z: 4835}
)

// natureTemplePack is open ground with the altar's 3x3 footprint and the portal's tile solid, and whatever else a test puts in the way.
func natureTemplePack(t *testing.T, extra ...tile.Tile) *pathfind.Pack {
	t.Helper()
	solid := map[[2]int]bool{{2400, 4834}: true}
	for x := 2399; x <= 2401; x++ {
		for z := 4840; z <= 4842; z++ {
			solid[[2]int{x, z}] = true
		}
	}
	for _, e := range extra {
		solid[[2]int{e.X, e.Z}] = true
	}
	pk, err := packtest.Ground(2390, 4825, 2410, 4850, func(x, z int) bool { return solid[[2]int{x, z}] })
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func natureAltarMatch() locindex.Match {
	a := runecraftplan.Nature
	return locindex.Match{Loc: locindex.Loc{ID: a.AltarID, X: a.AltarAt.X, Z: a.AltarAt.Z, Shape: 10}, Name: runecraftplan.AltarName, Op: 1}
}

func naturePortalMatch() locindex.Match {
	a := runecraftplan.Nature
	return locindex.Match{Loc: locindex.Loc{ID: a.PortalID, X: a.PortalAt.X, Z: a.PortalAt.Z, Shape: 10}, Name: runecraftplan.PortalName, Op: 1}
}

type dirRead struct {
	tick int
	op   int
}

// dirMsg is a message written by the client and the tick it becomes readable in.
type dirMsg struct {
	b  []byte
	at int
}

type dirEngine struct {
	sim  *actionsim.Session
	chat *chatlog.Log
	pk   *pathfind.Pack
	a    runecraftplan.Altar

	altar, portal locreach.Loc

	mu           sync.Mutex
	tick         int
	delayedUntil int
	pending      []dirMsg
	here         tile.Tile
	way          []tile.Tile
	target       string
	xpT, level   int
	runClick     bool

	// Casts (CX-1). bows is the noted bow the pack holds (bowNoteID); refuseCasts makes the spell's checks fail (no rune): the click is
	// cleared and nothing locks the player. lagTicks delays every message, moved this tick says a movement click or an op was accepted.
	refuseCasts, ignoreOpsAfterCast bool
	lagTicks                        int
	movedThisTick                   bool
	casts, lostWalks                int
	castTicks                       []int
	magicXP                         int

	// What a test reads: the ticks each packet was read in, the ticks the craft and the exit happened in, and counts.
	reads                                      []dirRead
	craftTicks, exitTicks                      []int
	dropped, crafts, portals, cantReach, moves int
	messages                                   [][]int

	// Quirks. ignoreMoves swallows every walk (a hold would), ignoreOps every loc click, noXP makes a craft give no experience, and
	// craftLateTicks and portalLateTicks hold the first craft and the first exit back by that many ticks after the op runs.
	ignoreMoves, ignoreOps, noXP bool
	craftLateTicks, lateCraftAt  int
	portalLateTicks, latePortal  int
}

func newDirEngine(t *testing.T, pk *pathfind.Pack, pack []actionsim.Stack, at tile.Tile, level int) (*dirEngine, *fleetrunner.Deps) {
	t.Helper()
	sim := actionsim.New(objs(), pack)
	a := runecraftplan.Nature
	e := &dirEngine{sim: sim, chat: chatlog.New(), pk: pk, a: a, level: level, xpT: levels.XPAtLevel(level) * 10,
		altar:  locreach.Loc{X: a.AltarAt.X, Z: a.AltarAt.Z, Width: 3, Length: 3, Shape: 10},
		portal: locreach.Loc{X: a.PortalAt.X, Z: a.PortalAt.Z, Width: 1, Length: 1, Shape: 10}}
	e.setStat()
	e.moveTo(at)
	sim.OnSend = func(b []byte) {
		e.mu.Lock()
		e.pending = append(e.pending, dirMsg{b: b, at: e.tick + 1 + e.lagTicks})
		e.mu.Unlock()
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		tk := time.NewTicker(dirTickDur)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-tk.C:
				e.step()
			}
		}
	}()
	d := &fleetrunner.Deps{
		Session: sim, Chat: e.chat, Components: actionsim.Components(), Pack: pk,
		Delay: func(dur time.Duration) bool { time.Sleep(dur); return true },
	}
	return e, d
}

func (e *dirEngine) moveTo(t tile.Tile) {
	e.here = t
	e.sim.StandAt(t.X, t.Z)
}

func (e *dirEngine) setStat() {
	xp, level := e.xpT/10, levels.LevelFromXP(e.xpT/10)
	e.level = level
	e.sim.State().Handle(serverprot.UPDATE_STAT, statPacket(Stat, xp, level))
}

func (e *dirEngine) say(s string) { e.chat.Record(s) }

// readDelayed is whether a packet read at the start of this tick is dropped; accessDelayed whether the player's interaction is held (the
// flag is cleared in processPlayers, after the reads).
func (e *dirEngine) readDelayed() bool   { return e.tick <= e.delayedUntil }
func (e *dirEngine) accessDelayed() bool { return e.tick < e.delayedUntil }

func (e *dirEngine) step() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tick++
	e.sim.Tick()
	var pend [][]byte
	var later []dirMsg
	for _, m := range e.pending {
		if m.at <= e.tick {
			pend = append(pend, m.b)
		} else {
			later = append(later, m)
		}
	}
	e.pending = later
	e.movedThisTick = false
	for _, msg := range pend {
		var ops []int
		for _, pkt := range actionsim.Split(msg) {
			ops = append(ops, int(pkt[0]))
			e.read(pkt)
		}
		e.messages = append(e.messages, ops)
	}
	// World.processClientsIn: a movement click or an op decoded this tick, and the player delayed once the packets are in (a cast in the same
	// tick), has the route thrown away.
	if e.movedThisTick && e.readDelayed() {
		if len(e.way) > 0 {
			e.lostWalks++
		}
		e.way = nil
	}
	if e.lateCraftAt != 0 && e.tick >= e.lateCraftAt {
		e.lateCraftAt = 0
		e.craft()
	}
	if e.latePortal != 0 && e.tick >= e.latePortal {
		e.latePortal = 0
		e.exit()
	}
	e.process()
}

func (e *dirEngine) read(b []byte) {
	switch op := int(b[0]); op {
	case clientprot.MOVE_OPCLICK, clientprot.MOVE_GAMECLICK:
		if e.readDelayed() {
			e.dropped++
			return
		}
		if e.ignoreMoves {
			return
		}
		if op == clientprot.MOVE_GAMECLICK {
			e.target = "" // clearPendingAction: only the walk half of an op leaves the interaction alone
		}
		e.moves++
		e.movedThisTick = true
		body := b[2 : 2+int(b[1])]
		sx, sz := g2(body, 1), g2(body, 3)
		path := []tile.Tile{{X: sx, Z: sz}}
		for i := 5; i+1 < len(body); i += 2 {
			path = append(path, tile.Tile{X: sx + int(int8(body[i])), Z: sz + int(int8(body[i+1]))})
		}
		e.way, e.runClick = path, body[0] == 1
		e.reads = append(e.reads, dirRead{e.tick, op})
	case clientprot.OPLOC1:
		if e.readDelayed() {
			e.dropped++
			return
		}
		if e.ignoreOps {
			return
		}
		if e.ignoreOpsAfterCast && e.casts > 0 {
			return
		}
		e.movedThisTick = true
		switch g2(b, 5) {
		case e.a.AltarID:
			e.target = "altar"
		case e.a.PortalID:
			e.target = "portal"
		}
		e.reads = append(e.reads, dirRead{e.tick, op})
	case clientprot.OPHELDU:
		// The clear-move: an item named at a slot that does not hold it. clearPendingAction and nothing else.
		if e.readDelayed() {
			e.dropped++
			return
		}
		e.target = ""
		e.reads = append(e.reads, dirRead{e.tick, op})
	case clientprot.OPHELDT:
		if e.readDelayed() {
			e.dropped++
			return
		}
		e.target = "" // OpHeldTHandler: clearPendingAction, before the script's checks
		e.reads = append(e.reads, dirRead{e.tick, op})
		e.cast(g2(b, 1), g2(b, 7))
	}
}

// cast is alchemy.rs2: the checks, the bow gone for coins, the experience, and the p_delay.
func (e *dirEngine) cast(obj, spellCom int) {
	if e.refuseCasts {
		e.say("You do not have enough Nature Runes to cast this spell.")
		return
	}
	if e.count(int32(obj)) == 0 {
		return
	}
	xp, delay, pay := 31, 1, 4
	if spellCom == 1178 {
		xp, delay, pay = 65, 3, 6
	}
	e.sim.Remove(int32(obj), 1)
	e.sim.Give(995, bowCost*pay/10)
	e.magicXP += xp
	e.sim.State().Handle(serverprot.UPDATE_STAT, statPacket(6, e.magicXP, 66))
	e.delayedUntil = e.tick + 1 + delay
	e.casts++
	e.castTicks = append(e.castTicks, e.tick)
}

// process is Player.processInteraction.
func (e *dirEngine) process() {
	steps := 0
	speed := 1
	if e.runClick {
		speed = 2
	}
	for i := 0; i < speed && len(e.way) > 0; i++ {
		wp := e.way[0]
		if wp == e.here {
			e.way = e.way[1:] // popping a waypoint the player stands on costs the step
			continue
		}
		next, moved := e.pk.EngineStep(nav(e.here), nav(wp))
		if !moved {
			break
		}
		e.here = tile.Tile{X: next.X, Z: next.Z}
		steps++
		e.sim.StandAt(e.here.X, e.here.Z)
		if e.here == wp {
			e.way = e.way[1:]
		}
	}
	if e.target == "" || e.accessDelayed() {
		return
	}
	loc := e.altar
	if e.target == "portal" {
		loc = e.portal
	}
	reached, _ := locreach.Reached(e.here.X, e.here.Z, loc)
	switch {
	case reached && steps == 0:
		if e.target == "altar" {
			if e.craftLateTicks > 0 && e.crafts == 0 && e.lateCraftAt == 0 {
				e.lateCraftAt = e.tick + e.craftLateTicks
			} else {
				e.craft()
			}
		} else if e.portalLateTicks > 0 && e.portals == 0 && e.latePortal == 0 {
			e.latePortal = e.tick + e.portalLateTicks
		} else {
			e.exit()
		}
		e.target = ""
	case !reached && len(e.way) == 0 && steps == 0:
		e.cantReach++
		e.say("I can't reach that!")
		e.target = ""
	}
}

func (e *dirEngine) exit() {
	e.portals++
	e.exitTicks = append(e.exitTicks, e.tick)
	e.way = nil
	e.moveTo(e.a.Exit)
}

func (e *dirEngine) craft() {
	if e.level < e.a.Level {
		e.say("You need a Runecrafting level of at least 44 to craft Nature runes.")
		return
	}
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == essID {
			n += s.Count
		}
	}
	if n == 0 {
		e.say("You do not have any rune stones to bind.")
		return
	}
	level := e.level
	e.sim.Remove(essID, n)
	e.sim.Give(int32(e.a.RuneID), n*e.a.RunesPerEssence(level))
	if !e.noXP {
		e.xpT += n * e.a.XP10
		e.setStat()
	}
	e.crafts++
	e.craftTicks = append(e.craftTicks, e.tick)
	e.delayedUntil = e.tick + 4
	e.say("You bind the temple's power into Nature Runes.")
}

func (e *dirEngine) count(id int32) int {
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == id {
			n += s.Count
		}
	}
	return n
}

// readsOf are the ticks the packets with opcode op were read in.
func (e *dirEngine) readsOf(op int) []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []int
	for _, r := range e.reads {
		if r.op == op {
			out = append(out, r.tick)
		}
	}
	return out
}

func natureKit(n int) []actionsim.Stack { return essence(n) }

// bowNoteID is the noted strung bow the cast tests alch and bowCost its content cost.
const (
	bowNoteID = int32(9001)
	bowCost   = 2560
)

// natureKitWithBows is the gate's pack with a stack of bows to cast on.
func natureKitWithBows(essenceN, bows int) []actionsim.Stack {
	return append(essence(essenceN), actionsim.Stack{ID: bowNoteID, Count: bows})
}

// shortTicks shortens the wait budgets that a game tick stretches (tickPace) for a test whose engine ticks in 25 ms.
func shortTicks(t *testing.T) {
	t.Helper()
	old := tickPace
	tickPace = 3 * dirTick
	t.Cleanup(func() { tickPace = old })
}

func joinOps(ops []int) string {
	var s []string
	for _, op := range ops {
		switch op {
		case clientprot.MOVE_OPCLICK:
			s = append(s, "MOVE_OPCLICK")
		case clientprot.OPLOC1:
			s = append(s, "OPLOC1")
		default:
			s = append(s, "?")
		}
	}
	return strings.Join(s, "+")
}
