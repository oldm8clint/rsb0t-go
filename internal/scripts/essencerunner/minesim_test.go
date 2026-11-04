package essencerunner

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE MINE, AS ENGINE-TS 289 RUNS IT. mineSim is the part of the engine and of content's mining and level-up scripts that decides
// whether an account with a full pack gets out of the essence mine, played tick by tick against the real essencerunner code, the
// real collision pack, loc index and config (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG). The rules, each with the
// engine source it is copied from:
//
//   - INPUT (NetworkPlayer.decodeIn, ClientGameProtCategory): a message reaches the player's buffer when it is sent and is read in
//     order, at most five USER events (clicks, walks, Continue, CLOSE_MODAL) a tick; the rest wait for the next tick. CLIENT events
//     (the anti-cheat side packets) are read, up to twenty, and do nothing.
//   - A CLICK ON A LOC (OpLocHandler): refused while delayed; otherwise Player.clearPendingAction (drops the interaction and closes
//     any chat, which kills the script paused in it) and sets the interaction.
//   - A WALK (MoveClickHandler, MOVE_GAMECLICK): refused while delayed; otherwise clearPendingAction as above, and queues the path.
//     World.processClientsIn then sets moveClickRequest, and Player.updateMovement takes no step while a move click is pending, the
//     player is busy (a chat open or delayed) AND anything is queued (queue or engine queue).
//   - CONTINUE (ResumePauseButtonHandler): finishes the script paused on its pause button, which closes the chat
//     (Player.executeScript: closeModal(false)). CLOSE_MODAL only sets requestModalClose, honoured in processQueues.
//   - THE TICK (World.cycle, processPlayers): queues, then the engine queue (runs a script only when canAccess(): not protected, no
//     chat open, not delayed), then processInteraction (a loc's op runs only after movement, and only if no step was taken), then
//     movement.
//   - MINING (content skill_mining/scripts/mining.rs2 mining_firstswing / mining_continue / get_ore_essence): a full pack opens
//     the message box `~mesbox("Your inventory is too full ...")` (interface 356) and ends the interaction (no p_oploc); otherwise
//     the swing is rolled every mining_rate ticks and each run ends in p_oploc(3), which keeps the interaction alive.
//   - LEVELING (Player.addXp, levelup.rs2): a level-up queues [changestat] and [advancestat] on the ENGINE queue; the second opens
//     the level-up chat (4416) and pauses on its button, once canAccess().
//   - THE PORTAL (essence_mine.rs2): p_delay(1), then a teleport out of the mine.
//
// What it leaves out on purpose: collision (the account steps along the corners of the route it is sent), other players, random
// events. Time is virtual: d.Delay advances it, and every packet the client sends costs it sendCost of CPU, which is what makes an
// unthrottled loop send thousands of clicks in the time the server takes to answer one (lab, 15 Oct 2026: 1,448 clicks in 173 ms).

const (
	simTick     = 600 * time.Millisecond
	simSendCost = 120 * time.Microsecond
	simRate     = 8 // bronze pickaxe mining_rate
	simUserCap  = 5
	simBudget   = 1500 * time.Second

	idPickaxe = 1265 // bronze pickaxe
	idEssence = essenceplan.EssenceID

	chatLevelUp = 4416
	chatMesbox  = 356
)

var (
	simOnce  sync.Once
	simIndex *locindex.Index
	simErr   error
)

func loadLocs(t testing.TB) *locindex.Index {
	t.Helper()
	path := os.Getenv("RSB0T_LOC_INDEX")
	if path == "" {
		t.Skip("set RSB0T_LOC_INDEX")
	}
	simOnce.Do(func() { simIndex, simErr = locindex.Load(path) })
	if simErr != nil {
		t.Fatal(simErr)
	}
	return simIndex
}

// simPkt is one client packet the server will read.
type simPkt struct {
	op   int
	x, z int
	id   int
	path []tile.Tile
	user bool
}

type simTarget struct {
	loc  tile.Tile
	id   int
	op   int
	kind string // "rock" or "portal"
}

type mineSim struct {
	t   testing.TB
	cl  *actionsim.Session
	d   *Deps
	log []string

	// virtual time
	now      time.Duration
	nextTick time.Duration
	tickN    int

	// the places
	rock, portal tile.Tile
	rockID       int
	portalID     int
	outside      tile.Tile

	// the player (Player.ts)
	pos               tile.Tile
	target            *simTarget
	chat              int // modalChat, -1 when none
	paused            string
	protect           bool
	delayed           int
	teleportOut       bool
	engineQ           []string
	moveClickRequest  bool
	requestModalClose bool
	waypoints         []tile.Tile
	stepsTaken        int
	actionDelay       int
	mined             int
	levelUps          map[int]bool // the nth essence mined queues a level-up

	// the wire
	inbox    []simPkt
	lastChat int

	// what the tests read
	maxBacklog   int
	oplocs       int
	rockClicks   int
	walks        int
	resumes      int
	closes       int
	chatsOpened  map[int]int
	fullTick     int // the tick the pack filled
	outTick      int // the tick the player left the mine
	handledTicks []int
}

// newMineSim puts the account beside the real essence rock nearest (2924,4848), with the real portal in reach of a short walk.
func newMineSim(t testing.TB, levelUps ...int) *mineSim {
	t.Helper()
	cfg := loadConfig(t)
	ix := loadLocs(t)
	pk := loadRealPack(t)
	start := tile.Tile{X: 2924, Z: 4848}
	rocks := ix.Find(cfg, locindex.Query{X: start.X, Z: start.Z, Radius: 20, Name: essenceplan.EssenceRock, Op: essenceplan.MineOp}, nil)
	portals := ix.Find(cfg, locindex.Query{X: start.X, Z: start.Z, Radius: 20, Name: essenceplan.PortalName, Op: essenceplan.PortalOp}, nil)
	if len(rocks) == 0 || len(portals) == 0 {
		t.Skipf("the loc index has no essence rock (%d) or portal (%d) near %v", len(rocks), len(portals), start)
	}
	m := &mineSim{
		t: t, chat: -1, lastChat: -1, nextTick: simTick, pos: start,
		rock: tile.Tile{X: rocks[0].X, Z: rocks[0].Z}, rockID: rocks[0].ID,
		portal: tile.Tile{X: portals[0].X, Z: portals[0].Z}, portalID: portals[0].ID,
		outside:     tile.Tile{X: 2590, Z: 3086},
		levelUps:    map[int]bool{},
		chatsOpened: map[int]int{},
	}
	for _, n := range levelUps {
		m.levelUps[n] = true
	}
	m.cl = actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: idPickaxe, Count: 1}})
	m.cl.StandAt(start.X, start.Z)
	m.cl.OnSend = m.onSend
	m.d = &Deps{
		Session: m.cl, Pack: pk, Locs: ix, Config: cfg, Chat: chatlog.New(),
		Components: archive.Components{
			chatMesbox:  {ID: chatMesbox, Type: archive.TypeLayer, Children: []int{357}},
			357:         {ID: 357, ButtonType: archive.ButtonContinue},
			chatLevelUp: {ID: chatLevelUp, Type: archive.TypeLayer, Children: []int{4440}},
			4440:        {ID: 4440, ButtonType: archive.ButtonContinue},
		},
		Delay: m.delay,
		Log:   func(s string) { m.log = append(m.log, fmt.Sprintf("[%s] %s", m.now.Round(100*time.Millisecond), s)) },
	}
	return m
}

func (m *mineSim) over() bool { return m.now >= simBudget }

// delay is Deps.Delay: time passes, the server ticks.
func (m *mineSim) delay(d time.Duration) bool {
	m.advance(d)
	return !m.over()
}

func (m *mineSim) advance(d time.Duration) {
	m.now += d
	for m.now >= m.nextTick {
		m.nextTick += simTick
		m.tick()
	}
}

// ---- the wire ----

func g2s(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

// onSend splits one websocket message into packets, files them in the buffer, and charges the client's CPU for it.
func (m *mineSim) onSend(b []byte) {
	for len(b) > 0 {
		op := int(b[0])
		switch {
		case op >= clientprot.OPLOC1 && op <= clientprot.OPLOC5:
			m.inbox = append(m.inbox, simPkt{op: op, x: g2s(b, 1), z: g2s(b, 3), id: g2s(b, 5), user: true})
			b = b[7:]
		case op == clientprot.RESUME_PAUSEBUTTON:
			m.inbox = append(m.inbox, simPkt{op: op, user: true})
			b = b[3:]
		case op == clientprot.CLOSE_MODAL:
			m.inbox = append(m.inbox, simPkt{op: op, user: true})
			b = b[1:]
		case op == clientprot.MOVE_GAMECLICK || op == clientprot.MOVE_OPCLICK:
			n := int(b[1])
			body := b[2 : 2+n]
			sx, sz := g2s(body, 1), g2s(body, 3)
			path := []tile.Tile{{X: sx, Z: sz}}
			for i := 5; i+1 < len(body); i += 2 {
				path = append(path, tile.Tile{X: sx + int(int8(body[i])), Z: sz + int(int8(body[i+1]))})
			}
			m.inbox = append(m.inbox, simPkt{op: op, path: path, user: true})
			b = b[2+n:]
		case op == clientprot.ANTICHEAT_CYCLELOGIC2:
			b = b[2+int(b[1]):] // a client event: read, and nothing happens
		case op == clientprot.NO_TIMEOUT:
			b = b[1:]
		default:
			m.t.Fatalf("mineSim: a packet it does not model: opcode %d", op)
			return
		}
	}
	m.advance(simSendCost)
}

// ---- the player (Player.ts) ----

func (m *mineSim) busy() bool      { return m.delayed > 0 || m.chat != -1 }
func (m *mineSim) canAccess() bool { return !m.protect && !m.busy() }

// closeModal is Player.closeModal(): the chat goes, the script paused in it is dropped, protected access ends unless delayed.
func (m *mineSim) closeModal() {
	if m.delayed == 0 {
		m.protect = false
	}
	if m.chat == -1 {
		return
	}
	m.chat = -1
	m.paused = ""
}

func (m *mineSim) clearPendingAction() {
	m.target = nil
	m.closeModal()
}

// openChat is openChatModal plus the pause the script ends in.
func (m *mineSim) openChat(id int, script string) {
	m.chat = id
	m.paused = script
	m.protect = true // executeScript keeps the protected access a suspended script was run with
	m.chatsOpened[id]++
}

// read is one packet's handler; it reports whether the engine counts it as a handled USER event.
func (m *mineSim) read(p simPkt, opcalled *bool, walked *bool) bool {
	switch {
	case p.op >= clientprot.OPLOC1 && p.op <= clientprot.OPLOC5:
		m.oplocs++
		if m.delayed > 0 {
			return false // OpLocHandler: `if (player.delayed) { UnsetMapFlag; return false }`
		}
		var tg *simTarget
		switch {
		case p.x == m.rock.X && p.z == m.rock.Z && p.id == m.rockID:
			tg = &simTarget{loc: m.rock, id: p.id, op: p.op - clientprot.OPLOC1 + 1, kind: "rock"}
			m.rockClicks++
		case p.x == m.portal.X && p.z == m.portal.Z && p.id == m.portalID:
			tg = &simTarget{loc: m.portal, id: p.id, op: p.op - clientprot.OPLOC1 + 1, kind: "portal"}
		default:
			return false
		}
		m.clearPendingAction()
		m.target = tg
		*opcalled = true
		return true
	case p.op == clientprot.MOVE_GAMECLICK || p.op == clientprot.MOVE_OPCLICK:
		m.walks++
		if m.delayed > 0 {
			m.waypoints = nil // UnsetMapFlag
			return false
		}
		if p.op == clientprot.MOVE_GAMECLICK {
			m.clearPendingAction()
		}
		m.waypoints = append([]tile.Tile(nil), p.path...)
		*walked = true
		return true
	case p.op == clientprot.RESUME_PAUSEBUTTON:
		m.resumes++
		if m.paused == "" {
			return false // ResumePauseButtonHandler: nothing paused on its button
		}
		m.paused = ""
		m.protect = false
		m.chat = -1 // executeScript: a finished script closes its chat (closeModal(false))
		return true
	case p.op == clientprot.CLOSE_MODAL:
		m.closes++
		m.requestModalClose = true
		return true
	}
	return false
}

func chebyshev(a, b tile.Tile) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

func stepToward(from, to int) int {
	switch {
	case to > from:
		return 1
	case to < from:
		return -1
	}
	return 0
}

// tick is World.cycle for the one player.
func (m *mineSim) tick() {
	m.tickN++
	m.stepsTaken = 0
	if len(m.inbox) > m.maxBacklog {
		m.maxBacklog = len(m.inbox)
	}
	if m.delayed > 0 {
		m.delayed--
		if m.delayed == 0 && m.teleportOut {
			m.teleportOut = false
			m.pos = m.outside
			m.waypoints = nil
			m.target = nil
			m.outTick = m.tickN
		}
	}

	// processClientsIn: decodeIn reads until five user events were handled (the others it refuses are client events, at most twenty).
	handled, read := 0, 0
	opcalled, walked := false, false
	for len(m.inbox) > 0 && handled < simUserCap && read < simUserCap+20 {
		p := m.inbox[0]
		m.inbox = m.inbox[1:]
		read++
		if m.read(p, &opcalled, &walked) {
			handled++
		}
	}
	m.handledTicks = append(m.handledTicks, handled)
	if walked || opcalled {
		if m.delayed > 0 {
			m.waypoints = nil
		} else if !m.busy() && opcalled {
			m.moveClickRequest = false
		} else {
			m.moveClickRequest = true
		}
	}

	// processPlayers: queues, engine queue, interaction, movement.
	if m.requestModalClose {
		m.requestModalClose = false
		m.closeModal()
	}
	for i := 0; i < len(m.engineQ); {
		if !m.canAccess() {
			break
		}
		switch m.engineQ[i] {
		case "changestat":
		case "levelup":
			m.openChat(chatLevelUp, "levelup")
		}
		m.engineQ = append(m.engineQ[:i], m.engineQ[i+1:]...)
	}
	m.processInteraction()

	m.flush()
}

// processInteraction is Player.processInteraction for a loc target: movement first, and the op only if no step was taken.
func (m *mineSim) processInteraction() {
	m.updateMovement()
	if m.target == nil || !m.canAccess() || m.stepsTaken != 0 {
		return
	}
	if chebyshev(m.pos, m.target.loc) > 1 {
		if len(m.waypoints) == 0 {
			m.target = nil // "I can't reach that!"
		}
		return
	}
	tg := m.target
	m.target = nil // tryInteract: the target is held aside while the script runs
	m.waypoints = nil
	switch tg.kind {
	case "rock":
		m.mining(tg)
	case "portal":
		m.delayed, m.teleportOut = 1, true // p_delay(1); p_telejump
	}
}

// updateMovement is Player.updateMovement: no step while a move click is pending, the player is busy and anything is queued.
func (m *mineSim) updateMovement() {
	if m.moveClickRequest && m.busy() && len(m.engineQ) > 0 {
		return
	}
	if len(m.waypoints) == 0 {
		return
	}
	w := m.waypoints[0]
	m.pos.X += stepToward(m.pos.X, w.X)
	m.pos.Z += stepToward(m.pos.Z, w.Z)
	m.stepsTaken++
	if m.pos.X == w.X && m.pos.Z == w.Z {
		m.waypoints = m.waypoints[1:]
	}
}

// mining is mining_firstswing (op 1) / mining_continue (op 3) with get_ore_essence.
func (m *mineSim) mining(tg *simTarget) {
	if len(m.cl.Pack()) >= 28 {
		m.openChat(chatMesbox, "mesbox") // anim(null); ~mesbox("Your inventory is too full ..."); return -- no p_oploc, the interaction ends
		return
	}
	again := func(op int) { m.target = &simTarget{loc: tg.loc, id: tg.id, op: op, kind: "rock"} }
	if tg.op == 1 {
		switch {
		case m.actionDelay < m.tickN:
			m.actionDelay = m.tickN + simRate
			again(1) // p_oploc(1)
		case m.actionDelay == m.tickN:
			m.getOre(again)
		default:
			again(3)
		}
		return
	}
	switch {
	case m.actionDelay < m.tickN:
		m.actionDelay = m.tickN + simRate
		again(3)
	case m.actionDelay == m.tickN:
		m.getOre(again)
	default:
		again(3)
	}
}

func (m *mineSim) getOre(again func(op int)) {
	m.cl.Give(idEssence, 1)
	m.mined++
	if m.levelUps[m.mined] {
		m.engineQ = append(m.engineQ, "changestat", "levelup")
	}
	again(3)
	if len(m.cl.Pack()) >= 28 && m.fullTick == 0 {
		m.fullTick = m.tickN
	}
}

// flush is processClientsOut for what the client reads: the chat opens and closes, then the tick's PLAYER_INFO.
func (m *mineSim) flush() {
	switch {
	case m.chat != m.lastChat && m.chat != -1:
		m.cl.State().Handle(serverprot.IF_OPENCHAT, []byte{byte(m.chat >> 8), byte(m.chat)})
	case m.chat != m.lastChat:
		m.cl.State().Handle(serverprot.IF_CLOSE, nil)
	}
	m.lastChat = m.chat
	m.cl.StandAt(m.pos.X, m.pos.Z)
}

// ---- a scenario ----

// mineOutcome is what a run of "mine until the pack is full, then take the portal" came to.
type mineOutcome struct {
	left         bool
	leaveTicks   int // ticks from the pack filling to the account being outside
	fullTick     int
	maxBacklog   int
	oplocs       int
	rockClicks   int
	resumes      int
	chatsOpened  map[int]int
	endedAtTicks int
}

// run is runecraftrunner.RunStretch's loop over essenceplan.DecideStage, cut to its two stages: Mine while the pack is not full,
// then PortalOut, up to portalTries times.
func (m *mineSim) run(mine func(*Deps) MineResult, portalTries int) mineOutcome {
	d := m.d
	for !PackFull(d) && !m.over() {
		r := mine(d)
		if r.Reason == "no-rock" || r.Reason == "stopped" {
			break
		}
	}
	for try := 0; try < portalTries && InMine(d) && !m.over(); try++ {
		PortalOut(d)
	}
	out := mineOutcome{
		left: !InMine(d), fullTick: m.fullTick, maxBacklog: m.maxBacklog, oplocs: m.oplocs, rockClicks: m.rockClicks,
		resumes: m.resumes, chatsOpened: m.chatsOpened, endedAtTicks: m.tickN,
	}
	if out.left && m.outTick > 0 && m.fullTick > 0 {
		out.leaveTicks = m.outTick - m.fullTick
	}
	return out
}

func (o mineOutcome) String() string {
	return fmt.Sprintf("left=%v leaveTicks=%d fullTick=%d endedAt=%d maxBacklog=%d oplocs=%d rockClicks=%d resumes=%d chats=%v",
		o.left, o.leaveTicks, o.fullTick, o.endedAtTicks, o.maxBacklog, o.oplocs, o.rockClicks, o.resumes, o.chatsOpened)
}

// mineBeforeTheFix is Mine as claude/batch2-1007 had it: the hold loop answers an open continue-able chat by RETURNING "dialog", and
// the stretch's loop (which has nothing to wait on) calls it again at once.
func mineBeforeTheFix(d *Deps) MineResult {
	here := currentTile(d)
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 20, Name: essenceplan.EssenceRock, Op: essenceplan.MineOp}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		return MineResult{Reason: "no-rock"}
	}
	rockTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	if tile.Chebyshev(here, rockTile) > 1 {
		WalkNear(d, rockTile, 1)
	}
	if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
		return MineResult{Reason: "no-rock"}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	count := EssenceHeld(d)
	gained := 0
	lastGain := time.Now()
	for {
		if dlg.CanContinue() {
			return MineResult{Gained: gained, Reason: "dialog"}
		}
		if !InMine(d) {
			return MineResult{Gained: gained, Reason: "left-mine"}
		}
		now := EssenceHeld(d)
		if now > count {
			gained += now - count
			count = now
			lastGain = time.Now()
		}
		if PackFull(d) {
			return MineResult{Gained: gained, Reason: "full"}
		}
		if time.Since(lastGain) > essenceplan.MineStallTimeout {
			return MineResult{Gained: gained, Reason: "stalled"}
		}
		if !d.Delay(mineTickInterval) {
			return MineResult{Gained: gained, Reason: "stopped"}
		}
	}
}

// ---- the tests ----

// The live failure, reproduced: a level-up opens its chat in the middle of a load, the old loop answers it with thousands of clicks
// on the rock in the time the server takes to close the chat once, and the server reads five a tick. The pack fills with the
// clicks still queued; the Continue and the walk that follow wait behind them, and the "inventory is too full" box is reopened by
// each queued click as it is read. The miner is still in the mine two minutes after the pack filled (live: 8-11 minutes).
func TestTheOldMineLoopStrandsAMinerAfterALevelUpMidLoad(t *testing.T) {
	m := newMineSim(t, 22)
	out := m.run(mineBeforeTheFix, 4)
	t.Logf("old loop: %s", out)
	if out.rockClicks < 500 {
		t.Errorf("the old loop sent %d click(s) on the rock; the failure it is being kept to reproduce needs hundreds", out.rockClicks)
	}
	if out.maxBacklog < 500 {
		t.Errorf("the server had at most %d packets waiting; the failure needs hundreds", out.maxBacklog)
	}
	if out.left && out.leaveTicks < 200 {
		t.Errorf("the old loop got out %d ticks (%s) after the pack filled; the live account took minutes", out.leaveTicks, time.Duration(out.leaveTicks)*simTick)
	}
}

// The fix: the level-up is answered with its Continue, the rock is clicked a handful of times, nothing queues in front of the walk,
// and the miner is out of the mine seconds after the pack fills.
func TestAMinerThatLevelsUpMidLoadStillLeavesQuickly(t *testing.T) {
	m := newMineSim(t, 22)
	out := m.run(Mine, 4)
	t.Logf("new loop: %s", out)
	if !out.left {
		t.Fatalf("still in the mine: %s\nlog: %v", out, m.log)
	}
	if out.rockClicks > 4 {
		t.Errorf("%d click(s) on the rock for one load with one level-up; want the first and one after the chat", out.rockClicks)
	}
	if out.maxBacklog > simUserCap {
		t.Errorf("the server had %d packets waiting at the start of a tick; want at most the %d it reads", out.maxBacklog, simUserCap)
	}
	if out.resumes < 1 {
		t.Errorf("the level-up chat was never answered with Continue")
	}
	if out.leaveTicks > 40 {
		t.Errorf("out %d ticks after the pack filled; want well under 40 (live main: about 15)", out.leaveTicks)
	}
}

// Every shape of load: no level-up (the pack-full box and the walk meet), the level-up on the very last essence (the chat and the
// full pack together), two level-ups back to back, and the level-up chat already open when the rock is clicked.
func TestMinersLeaveWhenEveryKindOfLevelUpComesAndGoes(t *testing.T) {
	cases := []struct {
		name     string
		levelUps []int
		chatUp   bool
	}{
		{"no level-up", nil, false},
		{"a level-up on the first essence", []int{1}, false},
		{"a level-up mid load", []int{14}, false},
		{"a level-up on the last essence", []int{27}, false},
		{"a level-up on the one before the last", []int{26}, false},
		{"two level-ups in a row", []int{20, 21}, false},
		{"three level-ups", []int{5, 15, 25}, false},
		{"the chat is already up when the rock is clicked", []int{22}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMineSim(t, c.levelUps...)
			if c.chatUp {
				m.openChat(chatLevelUp, "levelup")
				m.flush()
			}
			out := m.run(Mine, 4)
			if !out.left {
				t.Fatalf("still in the mine: %s\nlog: %v", out, m.log)
			}
			if out.maxBacklog > simUserCap {
				t.Errorf("%d packets waiting at the start of a tick; the engine reads %d: %s", out.maxBacklog, simUserCap, out)
			}
			if out.rockClicks > 3+2*len(c.levelUps) {
				t.Errorf("%d clicks on the rock: %s", out.rockClicks, out)
			}
			if out.leaveTicks > 40 {
				t.Errorf("out %d ticks after the pack filled: %s", out.leaveTicks, out)
			}
		})
	}
}

// A chat the click is closing is not an interruption: the old loop returned "dialog" for it; the new one waits for the server's
// IF_CLOSE and mines on, with no second click.
func TestAChatUpWhenTheRockIsClickedIsLeftToTheClick(t *testing.T) {
	m := newMineSim(t)
	m.openChat(chatLevelUp, "levelup")
	m.flush()
	r := Mine(m.d)
	if r.Clicks != 1 || r.Dialogs != 0 {
		t.Errorf("the call clicked %d time(s) and answered %d chat(s); want 1 and 0 (the click closes the chat; %s)", r.Clicks, r.Dialogs, r.Reason)
	}
	if r.Reason != "full" {
		t.Errorf("reason %q, want full (log %v)", r.Reason, m.log)
	}
}

// The gate on its own, with no engine and no data: however Mine is called, the second click on the rock waits two ticks. (The gate
// itself is internal/actions' and is tested there; this keeps the rock's key and the essence miner's use of it honest.)
func TestRockClicksAreSpacedTwoTicksApart(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.SetTicked(true)
	sim.StandAt(2924, 4848)
	var delays int
	w := actions.DelayWaiter(func(time.Duration) bool { delays++; sim.Tick(); return true })
	key := actions.LocKey(2925, 4849, 2491)
	if ticks, _ := actions.ClickWait(sim, key); ticks != 0 {
		t.Fatalf("a first click waits %d tick(s)", ticks)
	}
	actions.MarkClick(sim, key)
	if ticks, _ := actions.ClickWait(sim, key); ticks != actions.ClickGapTicks {
		t.Fatalf("right after a click the next waits %d tick(s), want %d", ticks, actions.ClickGapTicks)
	}
	if !actions.WaitClickGap(sim, w, key, nil) {
		t.Fatal("a stop was reported")
	}
	if delays != actions.ClickGapTicks { // this Delay is one server tick each
		t.Errorf("the gap took %d delay(s) of one tick each, want %d", delays, actions.ClickGapTicks)
	}
	if ticks, _ := actions.ClickWait(sim, key); ticks != 0 {
		t.Errorf("after the gap the next click still waits %d tick(s)", ticks)
	}
}

// A stop during the gap is a stop.
func TestAStopDuringTheClickGapStopsTheClick(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2924, 4848)
	key := actions.LocKey(2925, 4849, 2491)
	actions.MarkClick(sim, key)
	if actions.WaitClickGap(sim, actions.DelayWaiter(func(time.Duration) bool { return false }), key, nil) {
		t.Error("the gap said go though the caller was stopped")
	}
}
