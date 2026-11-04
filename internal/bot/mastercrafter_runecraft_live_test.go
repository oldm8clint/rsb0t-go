package bot

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// THE LIVE WORLD ON THE REAL DATA. The levelling round's real handlers (masterRuneWorld: the bank visit, the walk to a
// tile beside the ruins, the talisman, the walk to the altar, the craft, the walk to the portal, the portal) run through
// runLevelLoop against the fleet's own loc index, item config and collision pack, with a server that plays the content
// rules tick by tick (runecraft.rs2) -- and is strict where the real one is:
//
//   - a use on the ruins, a craft and a portal are only accepted from a tile the engine's reach test accepts
//     (internal/locreach, on the real locs' sizes, angles and shapes), else "I can't reach that!";
//   - the craft holds the player four ticks (delayedUntil = T+4); a packet read meanwhile is dropped without a word,
//     and so is a walk. World.cycle reads the packets (processClientsIn) BEFORE processPlayers clears the flag, so the
//     reads of T+1..T+4 are lost and T+5 is the first free one (the walk seam refuses a move that would be read in
//     one of them, as World.processClientsIn discards it).
//
// Needs RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX and RSB0T_COLLISION_PACK; without them this skips.

const liveTick = 25 * time.Millisecond

// liveTickDur is the tick a liveRig made now runs at: liveTick, unless a test that reads casts off a wall clock slows it.
var liveTickDur = liveTick

type liveEngine struct {
	sim  *actionsim.Session
	chat *chatlog.Log

	mu           sync.Mutex
	tick         int
	delayedUntil int
	teleportAt   int
	teleportTo   tile.Tile
	pending      [][]byte
	here         tile.Tile
	xpT          int
	level        int
	locs         map[int]locreach.Loc // by loc type id: the ruins, altars and portals of the table

	dropped, refusedWalks int
	crafts                []string
	banks                 []tile.Tile
	trace                 []string

	// craftLateTicks holds the first craft back by that many ticks, as a server that is slow to answer does (lateCraftAt is the tick
	// it lands in, lateAltar the altar); altarClicks counts the altar clicks the engine read.
	craftLateTicks, lateCraftAt, altarClicks int
	lateAltar                                runecraftplan.Altar
	// teleportLateTicks holds a talisman's teleport back by that many ticks past its usual two.
	teleportLateTicks int

	// The movement half (Engine-TS PathingEntity and Player.processInteraction), played only for a walk the account SENT
	// (MOVE_OPCLICK, the direct ops): way is the waypoints still to walk, loc the loc a click is waiting to run on, pk the pack the
	// greedy step is judged on (nil: no movement). A click read with nothing queued runs at once, as it always did here.
	pk                               *pathfind.Pack
	way                              []tile.Tile
	loc                              int
	ignoreMoves, ignoreOps           bool
	moveReads, locReads, cantReaches int
	craftTicks, exitTicks, readTicks []int
	messages                         [][]int

	// Casts (CX-1; runecraftplan/legcast.go): OPHELDT clears the interaction and keeps the walk, locks the player for p_delay(n), pays for
	// the bow and gives experience; a movement click or an op read in the tick of a cast has the route thrown away. refuseCasts makes
	// the spell's checks fail (the click is cleared all the same); bowCost is the cost the coins are paid on.
	casts, lostWalks, magicXP int
	castTicks                 []int
	refuseCasts, movedTick    bool
	bowCost                   int
}

func liveLocs(t *testing.T, cfg *cachecfg.Config, ix *locindex.Index) map[int]locreach.Loc {
	t.Helper()
	out := map[int]locreach.Loc{}
	for _, a := range runecraftplan.Altars() {
		for _, c := range []struct {
			id int
			at tile.Tile
		}{{a.RuinsID, a.Ruins}, {a.AltarID, a.AltarAt}, {a.PortalID, a.PortalAt}} {
			ms := ix.Find(cfg, locindex.Query{X: c.at.X, Z: c.at.Z, Radius: runecraftplan.RuinsRadius, IDs: []int{c.id}}, nil)
			typ, ok := cfg.Loc(c.id)
			if len(ms) != 1 || !ok {
				t.Fatalf("loc %d near %v: %d matches", c.id, c.at, len(ms))
			}
			m := ms[0]
			out[c.id] = locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
		}
	}
	return out
}

// delayed is whether a packet read at the start of this tick is dropped: the flag is cleared in processPlayers, after
// the read, so the tick delayedUntil itself still drops (World.ts: processClientsIn 355, processPlayers 692).
func (e *liveEngine) delayed() bool { return e.tick <= e.delayedUntil }

func (e *liveEngine) say(s string) { e.chat.Record(s) }

func (e *liveEngine) moveTo(t tile.Tile) {
	e.here = t
	e.sim.StandAt(t.X, t.Z)
}

func (e *liveEngine) setStat() {
	xp := e.xpT / 10
	e.level = levels.LevelFromXP(xp)
	p := packet.NewWriter(8)
	p.P1(byte(runecraftrunner.Stat))
	p.P4(int32(xp))
	p.P1(byte(e.level))
	e.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

func lg2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (e *liveEngine) step() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tick++
	e.sim.Tick()
	pend := e.pending
	e.pending = nil
	e.movedTick = false
	for _, b := range pend {
		var ops []int
		for _, pkt := range actionsim.Split(b) {
			ops = append(ops, int(pkt[0]))
			n := len(pkt)
			switch {
			case n >= 13 && int(pkt[0]) == clientprot.OPLOCU:
				e.useOnLoc(lg2(pkt, 5), lg2(pkt, 7), lg2(pkt, 11))
			case n >= 7 && int(pkt[0]) == clientprot.OPLOC1:
				e.readLoc(lg2(pkt, 5))
			case int(pkt[0]) == clientprot.MOVE_OPCLICK || int(pkt[0]) == clientprot.MOVE_GAMECLICK:
				e.readMove(pkt)
			case int(pkt[0]) == clientprot.OPHELDU:
				if !e.delayed() {
					e.loc = 0 // the clear-move: clearPendingAction and nothing else
				}
			case int(pkt[0]) == clientprot.OPHELDT:
				e.readCast(pkt)
			}
		}
		e.messages = append(e.messages, ops)
	}
	// World.processClientsIn: a movement click or an op decoded this tick, and the player delayed once the packets are in, has the route thrown away.
	if e.movedTick && e.delayed() {
		if len(e.way) > 0 {
			e.lostWalks++
		}
		e.way = nil
	}
	e.processMovement()
	if e.teleportAt != 0 && e.tick >= e.teleportAt {
		e.teleportAt = 0
		e.moveTo(e.teleportTo)
	}
	if e.lateCraftAt != 0 && e.tick >= e.lateCraftAt {
		e.lateCraftAt = 0
		e.craft(e.lateAltar)
	}
}

// readCast is OpHeldTHandler and alchemy.rs2.
func (e *liveEngine) readCast(b []byte) {
	if e.delayed() {
		e.dropped++
		return
	}
	e.loc = 0 // clearPendingAction, before the script's checks
	obj, spell := lg2(b, 1), lg2(b, 7)
	if e.refuseCasts {
		e.say("You do not have enough Nature Runes to cast this spell.")
		return
	}
	if e.packCount(int32(obj)) == 0 {
		return
	}
	xp, delay, pay := 31, 1, 4
	if spell == 1178 {
		xp, delay, pay = 65, 3, 6
	}
	cost := e.bowCost
	if cost == 0 {
		cost = 2560
	}
	e.sim.Remove(int32(obj), 1)
	e.sim.Give(995, cost*pay/10)
	e.magicXP += xp
	p := packet.NewWriter(8)
	p.P1(6)
	p.P4(int32(e.magicXP))
	p.P1(66)
	e.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	e.delayedUntil = e.tick + 1 + delay
	e.casts++
	e.castTicks = append(e.castTicks, e.tick)
}

func (e *liveEngine) reaches(loc int) bool {
	l, ok := e.locs[loc]
	if !ok {
		return false
	}
	reached, _ := locreach.Reached(e.here.X, e.here.Z, l)
	return reached
}

func (e *liveEngine) useOnLoc(loc, obj, com int) {
	if e.delayed() {
		e.dropped++
		return
	}
	e.movedTick = true
	for _, a := range runecraftplan.Altars() {
		if a.RuinsID != loc {
			continue
		}
		if obj != a.TalismanID || com != runecraftrunner.PlayerPack {
			e.say("Nothing interesting happens.")
			return
		}
		if !e.reaches(loc) {
			e.say("I can't reach that!")
			return
		}
		e.say("You hold the " + a.Name + " Talisman towards the mysterious ruins.")
		e.delayedUntil, e.teleportAt, e.teleportTo = e.tick+2+e.teleportLateTicks, e.tick+2+e.teleportLateTicks, a.Enter
		return
	}
}

// readLoc is an OPLOC1 read: dropped in a delay; run at once when no walk is queued (the plain legs, the account standing where it
// clicks), else made the interaction and run by processMovement once the walk is done, as Player.processInteraction does.
func (e *liveEngine) readLoc(loc int) {
	if e.delayed() {
		e.dropped++
		return
	}
	if e.ignoreOps {
		return
	}
	e.movedTick = true
	e.locReads++
	e.readTicks = append(e.readTicks, e.tick)
	if e.pk != nil && len(e.way) > 0 {
		e.loc = loc
		return
	}
	e.runLoc(loc)
}

// readMove is a MOVE_OPCLICK read: dropped in a delay, else its waypoints are queued (the first is the start of the path).
func (e *liveEngine) readMove(b []byte) {
	if e.delayed() {
		e.dropped++
		return
	}
	if e.ignoreMoves || e.pk == nil {
		return
	}
	if int(b[0]) == clientprot.MOVE_GAMECLICK {
		e.loc = 0
	}
	e.movedTick = true
	e.moveReads++
	body := b[2 : 2+int(b[1])]
	sx, sz := lg2(body, 1), lg2(body, 3)
	path := []tile.Tile{{X: sx, Z: sz}}
	for i := 5; i+1 < len(body); i += 2 {
		path = append(path, tile.Tile{X: sx + int(int8(body[i])), Z: sz + int(int8(body[i+1]))})
	}
	e.way = path
}

// processMovement walks the queued waypoints a tile a tick (the start tile of a path costs the step to pop) and then, as
// Player.processInteraction does, runs a waiting click in a tick in which no step was taken, or answers "I can't reach that!" when
// nothing was walked, nothing is queued and the loc is out of reach. Walking goes on while the player is delayed; the click does not run.
func (e *liveEngine) processMovement() {
	if e.pk == nil {
		return
	}
	steps := 0
	for i := 0; i < 1 && len(e.way) > 0; i++ {
		wp := e.way[0]
		if wp == e.here {
			e.way = e.way[1:]
			continue
		}
		next, moved := e.pk.EngineStep(pathfind.NavPoint{X: e.here.X, Z: e.here.Z}, pathfind.NavPoint{X: wp.X, Z: wp.Z})
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
	if e.loc == 0 || e.tick < e.delayedUntil {
		return
	}
	loc := e.loc
	switch {
	case steps == 0 && e.reaches(loc):
		e.loc = 0
		e.runLoc(loc)
	case steps == 0 && len(e.way) == 0:
		e.loc = 0
		e.cantReaches++
		e.say("I can't reach that!")
	}
}

func (e *liveEngine) opLoc1(loc int) {
	if e.delayed() {
		e.dropped++
		return
	}
	e.runLoc(loc)
}

func (e *liveEngine) runLoc(loc int) {
	for _, a := range runecraftplan.Altars() {
		switch {
		case loc == a.AltarID && a.Inside(e.here):
			if !e.reaches(loc) {
				e.cantReaches++
				e.say("I can't reach that!")
				return
			}
			e.altarClicks++
			if e.craftLateTicks > 0 && len(e.crafts) == 0 {
				// The first click is answered late; one sent meanwhile is swallowed by the delay the craft will start.
				if e.lateCraftAt == 0 {
					e.lateCraftAt, e.lateAltar = e.tick+e.craftLateTicks, a
				}
				return
			}
			e.craft(a)
		case loc == a.PortalID && a.Inside(e.here):
			if !e.reaches(loc) {
				e.cantReaches++
				e.say("I can't reach that!")
				return
			}
			e.exitTicks = append(e.exitTicks, e.tick)
			e.way = nil
			e.moveTo(a.Exit)
		}
	}
}

func (e *liveEngine) craft(a runecraftplan.Altar) {
	if e.level < a.Level {
		e.say("You need a Runecrafting level of at least 20 to craft " + a.Name + " runes.")
		return
	}
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == runecraftplan.EssenceID {
			n += s.Count
		}
	}
	if n == 0 {
		e.say("You do not have any rune stones to bind.")
		return
	}
	level := e.level
	e.sim.Remove(runecraftplan.EssenceID, n)
	e.sim.Give(int32(a.RuneID), n*a.RunesPerEssence(level))
	e.xpT += n * a.XP10
	e.setStat()
	e.crafts = append(e.crafts, a.Key)
	e.craftTicks = append(e.craftTicks, e.tick)
	e.delayedUntil = e.tick + 4
	e.say("You bind the temple's power into " + a.Name + " Runes.")
}

func liveRig(t *testing.T) (*masterRig, *liveEngine) {
	t.Helper()
	cfgPath, ixPath, packPath := os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_COLLISION_PACK")
	if cfgPath == "" || ixPath == "" || packPath == "" {
		t.Skip("set RSB0T_CONFIG_JAG, RSB0T_LOC_INDEX and RSB0T_COLLISION_PACK")
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	r := newMasterRig(t)
	sim := actionsim.New(cfgObjs(cfg), nil)
	sim.OpenBank([]actionsim.Stack{
		{ID: runecraftplan.EssenceID, Count: 5000},
		{ID: int32(runecraftplan.Air.TalismanID), Count: 1}, {ID: int32(runecraftplan.Body.TalismanID), Count: 1},
	})
	e := &liveEngine{sim: sim, chat: chatlog.New(), locs: liveLocs(t, cfg, ix)}
	sim.OnSend = func(b []byte) {
		e.mu.Lock()
		e.pending = append(e.pending, b)
		e.mu.Unlock()
	}
	r.ctx.Session, r.ctx.Config, r.ctx.Locs, r.ctx.Pack, r.ctx.Components, r.ctx.Chat = sim, cfg, ix, pk, actionsim.Components(), e.chat
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		tk := time.NewTicker(liveTickDur)
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
	// The seams: the bank opens where the world asked (recorded), a walk moves the account at once unless it is
	// delayed (a move read in a delay is discarded), and the packs' waits are real but short.
	oldOpen, oldWalk := runecraftrunner.OpenBank, masterWalkTo
	t.Cleanup(func() { runecraftrunner.OpenBank, masterWalkTo = oldOpen, oldWalk })
	runecraftrunner.OpenBank = func(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, bool) {
		e.mu.Lock()
		e.banks = append(e.banks, at)
		e.mu.Unlock()
		sim.OpenBank(sim.Bank())
		return actions.NewBank(sim, actionsim.Components()), true
	}
	masterWalkTo = func(d *fleetrunner.Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		// The move would be read at the START of the next tick (the client sends it when it has seen this one), so
		// it is discarded if the player is still delayed THEN -- the tick delayedUntil itself included, as the read
		// comes before the flag is cleared: a release one tick too early is caught, the exact one is not.
		if e.tick+1 <= e.delayedUntil {
			e.refusedWalks++
			return false
		}
		if tile.Chebyshev(e.here, dest) > radius {
			e.moveTo(dest)
		}
		return true
	}
	return r, e
}

func (e *liveEngine) packCount(id int32) int {
	n := 0
	for _, s := range e.sim.Pack() {
		if s.ID == id {
			n += s.Count
		}
	}
	return n
}

// Air to the Body switch and on, through the real handlers: three Air trips take level 19 over 20, the next
// bank is Edgeville's (not Falador's), and the Body temple is entered, crafted at and left by its own portal.
func TestTheLiveWorldLevelsAirIntoBodyOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	cfg := r.b.set
	cfg.Rune.On, cfg.Rune.LevelTo, cfg.Rune.Teleport, cfg.Rune.Round = true, 21, false, 30*time.Minute
	r.b.set = cfg
	// Level 19, 300 xp short of 20.
	e.mu.Lock()
	e.xpT = (levels.XPAtLevel(20) - 300) * 10
	e.setStat()
	e.moveTo(runecraftplan.Air.Exit)
	e.mu.Unlock()
	time.Sleep(3 * liveTick)

	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: cfg.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now(),
		level0: 19, xp0: levels.XPAtLevel(20) - 300}
	var logs []string
	res := runLevelLoop(w, func(f string, a ...any) { w.logf(f, a...) }, 400)
	logs = r.logs
	out := strings.Join(logs, "\n")
	if !res.Done || res.Transient || res.Stuck {
		t.Fatalf("%+v\n%s", res, out)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// 3 Air crafts (135 each: 4170 -> 4305 -> 4440 -> 4575), then Body from level 20 until 21.
	if len(e.crafts) < 5 || e.crafts[0] != runecraftplan.KeyAir || e.crafts[2] != runecraftplan.KeyAir || e.crafts[3] != runecraftplan.KeyBody {
		t.Fatalf("crafts %v\n%s", e.crafts, out)
	}
	if e.level < 21 {
		t.Errorf("level %d", e.level)
	}
	// Banks: Falador east for the Air trips, Edgeville for Body, never the other.
	sawAir, sawBody := false, false
	for i, at := range e.banks {
		switch at {
		case runecraftplan.Air.Bank:
			sawAir = true
			if sawBody {
				t.Errorf("bank %d: Falador east after Edgeville", i)
			}
		case runecraftplan.Body.Bank:
			sawBody = true
		default:
			t.Errorf("bank %d at %v", i, at)
		}
	}
	if !sawAir || !sawBody {
		t.Errorf("banks %v", e.banks)
	}
	// No click was lost to a delay: the portal was timed to the end of every hold.
	if e.dropped != 0 || e.refusedWalks != 0 {
		t.Errorf("dropped packets %d, refused walks %d\n%s", e.dropped, e.refusedWalks, out)
	}
	// The pack at the end: the talisman held, runes banked each trip.
	for _, want := range []string{
		"runecraft: Air ruins: Air talisman used on 'Mysterious ruins' (2984,3291) from stand",
		"arrived inside at (2841,4830) after",
		"crafted 54 Air runes (+135.0 xp, Runecrafting 19 -> 19); essence 0 left; 4-tick hold", // 2 runes an essence from level 11
		"measured experience rate: +135 xp for 27 essence at the Air altar where the table says +135: x1.00; 54 runes where 54 were expected at level 19",
		"Air reached 20 (4,575 xp); switching to Body;",
		"Body ruins: Body talisman used on 'Mysterious ruins' (3052,3444) from stand",
		"arrived inside at (2523,4838) after",
		"temple walk planned: arrival -> altar stand",
		"portal taken after waiting",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
}

// THE ROUND ITSELF, on the same world: a new round walks to the mule's post, reads the bank, levels Air into Body,
// banks the pack and goes home to the post; the outcome and the ledger say what it did.
func TestTheLiveRoundLevelsAndGoesHomeOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	set := r.b.set
	set.Rune.On, set.Rune.LevelTo, set.Rune.Teleport, set.Rune.Round = true, 21, false, 30*time.Minute
	r.b.set = set
	post := tile.Tile{X: 2809, Z: 3441}
	r.ctx.CurrentWorld = 1
	// The Loot panel's sink, set before the round's deps copy it (b.deps): the crafts are reported by runecraftrunner.Craft.
	var drops []loot.Drop
	r.ctx.Loot = func(d loot.Drop) { drops = append(drops, d) }
	r.ctx.Peers = peerbank.New()
	r.ctx.Peers.Publish("mule", post, true, []peerbank.Holding{{Item: "Rune essence", Count: 100}}, nil, false)
	r.ctx.Bus.Publish(mulebus.OnWorld(mulebus.Heartbeat("mule", tile.Tile{X: 2809, Z: 3440}, 20, mulebus.StateHome, nil), 1))
	e.mu.Lock()
	e.xpT = (levels.XPAtLevel(20) - 300) * 10
	e.setStat()
	e.moveTo(tile.Tile{X: 2820, Z: 3440}) // by the post, east of it
	e.mu.Unlock()
	time.Sleep(3 * liveTick)

	d := r.b.deps(r.ctx)
	out := r.b.runecraftRound(r.ctx, d, time.Now(), mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft, Why: "test"})
	logs := strings.Join(r.logs, "\n")
	if os.Getenv("RSB0T_TEST_LOG") != "" {
		t.Logf("outcome: %+v\n%s", out, logs)
	}
	if out.Mode != mastercraftplan.ModeRunecraft || out.Transient || out.Produced < 100 {
		t.Fatalf("%+v\n%s", out, logs)
	}
	for _, want := range []string{
		"runecraft: level plan - Runecraft 19 -> 21 needs",
		"source=auto bank ",
		"members world ok",
		"runecraft: DONE - Runecrafting 19 -> 21",
		"banked the pack at Edgeville; home at the mule's post",
	} {
		if !strings.Contains(logs, want) && !strings.Contains(out.Why, want) {
			t.Errorf("neither the log nor %q has %q:\n%s", out.Why, want, logs)
		}
	}
	l := r.b.ledger.Rune
	if l.Rounds != 1 || l.Crafts < 5 || l.Trips != l.Crafts || l.EssenceCrafted != l.Crafts*27 || l.XPGained < 800 || !l.Known() || l.RunesMade[runecraftplan.KeyAir] == 0 || l.RunesMade[runecraftplan.KeyBody] == 0 {
		t.Errorf("ledger %+v", l)
	}
	if r.b.runeLevel != 21 {
		t.Errorf("runeLevel %d", r.b.runeLevel)
	}
	// The Loot panel's Runecraft card was fed by the very crafts the ledger counted: one drop a craft, the essence bound as the
	// spend, the experience as measured, and each altar's runes the ledger's own, with the level read climbing through them.
	var essenceSpent, xpPaid, crafts int
	runesBy := map[string]int{}
	lastLevel := 0
	for _, d := range drops {
		if !loot.IsRunecraft(d.Source) || d.Events != 1 || len(d.Items) != 1 || len(d.Spent) != 1 || d.Spent[0].ID != runecraftplan.EssenceID {
			t.Fatalf("a drop that is not one craft: %+v", d)
		}
		crafts++
		essenceSpent += int(d.Spent[0].Count)
		xpPaid += d.XP
		for _, a := range runecraftplan.Altars() {
			if d.Items[0].ID == a.RuneID && d.Source.Name == a.Name+" altar" {
				runesBy[a.Key] += int(d.Items[0].Count)
			}
		}
		if d.Source.Level < lastLevel {
			t.Errorf("the level read went down: %d after %d", d.Source.Level, lastLevel)
		}
		lastLevel = d.Source.Level
	}
	if crafts != l.Crafts || essenceSpent != l.EssenceCrafted || xpPaid != l.XPGained || lastLevel != 21 ||
		runesBy[runecraftplan.KeyAir] != l.RunesMade[runecraftplan.KeyAir] || runesBy[runecraftplan.KeyBody] != l.RunesMade[runecraftplan.KeyBody] || runesBy[runecraftplan.KeyAir] == 0 {
		t.Errorf("loot drops: %d crafts, %d essence, %d xp, level %d, runes %v; ledger: %d crafts, %d essence, %d xp, runes %v",
			crafts, essenceSpent, xpPaid, lastLevel, runesBy, l.Crafts, l.EssenceCrafted, l.XPGained, l.RunesMade)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.here != post && tile.Chebyshev(e.here, post) > 3 {
		t.Errorf("the round ended at %v, not the post", e.here)
	}
	if e.dropped != 0 || e.refusedWalks != 0 {
		t.Errorf("dropped packets %d, refused walks %d", e.dropped, e.refusedWalks)
	}
	// What the round left in the bank: the runes it made, banked at the end; no essence in the pack.
	if e.packCount(runecraftplan.EssenceID) != 0 || e.packCount(int32(runecraftplan.Body.RuneID)) != 0 {
		t.Errorf("pack %v", e.sim.Pack())
	}
}

// THE END OF A ROUND INSIDE A TEMPLE (three failed crafts, or the steps used up) takes the temple's portal before anything
// else, on the real locs: the walk to a tile the engine accepts the portal from, the click, and then the bank near the
// ruins -- not "left inside the temple" and a ScarabSwarm that starts there.
func TestTheEndOfARoundTakesTheAccountOutOfATempleOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	cfg := r.b.set
	cfg.Rune.On, cfg.Rune.LevelTo, cfg.Rune.Teleport, cfg.Rune.Round = true, 21, false, 30*time.Minute
	r.b.set = cfg
	e.mu.Lock()
	e.moveTo(runecraftplan.Body.Enter)
	e.mu.Unlock()
	time.Sleep(3 * liveTick)
	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: cfg.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now(), lastAltar: runecraftplan.Body}
	what, stranded := w.finish()
	e.mu.Lock()
	defer e.mu.Unlock()
	if stranded || !strings.Contains(what, "left the temple") || runecraftplan.Body.Inside(e.here) {
		t.Fatalf("what %q stranded %v, the account stands at %v: %s", what, stranded, e.here, strings.Join(r.logs, " | "))
	}
	if e.dropped != 0 || e.refusedWalks != 0 {
		t.Errorf("dropped packets %d, refused walks %d", e.dropped, e.refusedWalks)
	}
}

// THE REVIEWER'S TRACE, through the real loop and the real stage handlers: the round's clock runs out while the account is
// inside the essence mine (a restart there, or a stretch that took longer than the round). The loop only checks the clock
// outside, so it picks the mining stage; the stretch must take the portal out (not return in the mine with the pack full, fail
// three times and end the round there), and the loop then ends the round as EXPIRED, outside, with no stage counted as failed.
func TestTheLoopLeavesTheMineWhenTheClockRunsOutOnTheRealWorld(t *testing.T) {
	r, e := liveRig(t)
	cfg := r.b.set
	cfg.Rune.On, cfg.Rune.LevelTo, cfg.Rune.Teleport, cfg.Rune.Round = true, 21, false, 30*time.Minute
	r.b.set = cfg
	mindHoldStat(e.sim.State(), 6, levels.XPAtLevel(78), 78)  // Magic 78: the Guild's door
	mindHoldStat(e.sim.State(), 14, levels.XPAtLevel(60), 60) // Mining 60 and a Rune pickaxe in the pack: the stage can run
	e.sim.Give(1275, 1)
	env := &fakeRuneMine{inMine: true, pack: 27, onExit: func() {
		e.mu.Lock()
		e.moveTo(tile.Tile{X: 2594, Z: 3086})
		e.mu.Unlock()
	}}
	withRuneMineEnv(t, env)
	e.mu.Lock()
	e.xpT = levels.XPAtLevel(19) * 10
	e.setStat()
	e.moveTo(tile.Tile{X: 2891, Z: 4830}) // inside the mine's mapsquare (45,75)
	e.mu.Unlock()
	time.Sleep(3 * liveTick)

	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now().Add(-5 * time.Hour), set: cfg.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	res := runLevelLoop(w, func(f string, a ...any) { w.logf(f, a...) }, 50)
	if !res.Expired || res.Transient || res.Stuck || res.Done {
		t.Fatalf("%+v: %s", res, strings.Join(r.logs, " | "))
	}
	if env.inMine || env.portals != 1 || w.mineFailed || res.Stages[runecraftplan.StageMine] != 1 {
		t.Errorf("inMine %v portals %d mineFailed %v stages %v", env.inMine, env.portals, w.mineFailed, res.Stages)
	}
	if region, _ := runecraftplan.WhereIs(runecraftrunner.Here(d)); region != runecraftplan.RegionOutside {
		t.Errorf("the round ended in the %s", region)
	}
}

// THE NATURE GATE'S STAND on the real data (live 16 Oct 2026: four times two 27 second failures). The trade leaves the crafter at
// (2866,3020), two tiles west of the ruins; the nearest tile the engine accepts a use from by the locreach list alone was (2867,3019), a
// tile the collision pack blocks, where the walk stops one short and the talisman is clicked from the wrong tile. ToRuins now walks to a
// stand an account can stand on, and clicks only from a tile the engine accepts.
func TestToRuinsFromWhereTheTradeLeavesTheCrafterOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	nature := runecraftplan.Nature
	for _, from := range []tile.Tile{{X: 2866, Z: 3020}, {X: 2866, Z: 3019}, {X: 2866, Z: 3021}, {X: 2865, Z: 3022}, {X: 2869, Z: 3023}} {
		e.mu.Lock()
		e.moveTo(from)
		e.delayedUntil, e.teleportAt = 0, 0
		e.mu.Unlock()
		e.sim.Give(int32(nature.TalismanID), 1)
		time.Sleep(3 * liveTick)
		d := r.b.deps(r.ctx)
		w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
			standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
		var walked []tile.Tile
		old := masterWalkKaramja
		masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
			walked = append(walked, dest)
			e.mu.Lock()
			if tile.Chebyshev(e.here, dest) > radius {
				e.moveTo(dest)
			}
			e.mu.Unlock()
			return true
		}
		res := w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
		masterWalkKaramja = old
		if !res.OK {
			t.Fatalf("from %v: %+v\n%v", from, res, r.logs)
		}
		for _, to := range walked {
			if to == (tile.Tile{X: 2867, Z: 3019}) || to == (tile.Tile{X: 2870, Z: 3021}) {
				t.Errorf("from %v: walked to the blocked tile %v", from, to)
			}
		}
		if from == (tile.Tile{X: 2866, Z: 3020}) && (len(walked) != 1 || walked[0] != runecraftplan.GateStandTile) {
			t.Errorf("from %v: walked %v, want the gate stand %v", from, walked, runecraftplan.GateStandTile)
		}
		e.mu.Lock()
		inside := nature.Inside(e.here)
		e.mu.Unlock()
		if !inside {
			t.Errorf("from %v: the account is not inside the Nature temple: %v", from, e.here)
		}
		if e.dropped != 0 {
			t.Errorf("from %v: %d packets dropped", from, e.dropped)
		}
	}
}
