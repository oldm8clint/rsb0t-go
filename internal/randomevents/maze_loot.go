package randomevents

import (
	"fmt"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents/mazegraph"
	"rsb0t-go/internal/randomevents/mazeloot"
)

// This file is the live half of the maze's reward hold and chest looting; the engine facts and the
// decisions are in the pure package mazeloot, whose doc comment is the place to start. In short:
//
//   - The reward is %xplamp, 100 at the start, and a NORMAL timer takes 1 off it every 5 ticks the
//     player is "accessible". It does not run while the player holds a dialogue (or is delayed),
//     and a due timer that was blocked runs once, not once per missed interval.
//   - Every ordinary click closes the dialogue, but MOVE_OPCLICK does not (actions.WalkOpClick), so
//     a player who opens a dialogue on purpose -- here by clicking one of the maze's own walls,
//     whose "Open" is ~mesbox("I can't open that.") -- can walk the whole of a leg with the
//     reward standing still. The click that finally uses a door, a chest or the statue closes the
//     dialogue and pays the one decrement that was being held back: about a point per action
//     window instead of a point per 3 seconds.
//   - A chest's prize is an objbox, itself a dialogue: after a chest the reward is held for free.
//
// Nothing here is trusted blind. The percentage is read back from the mazetimer overlay text (the
// server sets it on every run of the timer), a walk with a dialogue up that does not move the
// player switches the whole thing off for that solve and falls back to the ordinary solver, and a
// failed pass does the same. The ordinary solver is exactly what ran before.

var (
	// mazeStallEnabled lets the solver hold the reward by walking with a dialogue up.
	mazeStallEnabled = true
	// mazeLootEnabled lets the solver open the chests in the rooms its route passes through.
	mazeLootEnabled = true
)

const (
	// mazeStallWallReach is the most tiles the solver walks to reach a wall to click: the maze has
	// one within 2 of every tile that matters (mazegraph's TestEverySpawnIsNearAWall).
	mazeStallWallReach = 3
	// mazeStallOpenWait is how long a wall click is given to put the dialogue up.
	mazeStallOpenWait = 4 * mazeTick
	// mazeChestSettle is how long a chest click is given to put its prize in the pack: the script
	// waits out p_arrivedelay and a p_delay(0) first.
	mazeChestSettle = 6 * mazeTick
	// mazeChestTries is how many times one chest is clicked before it is given up on (another
	// player in the maze may have emptied it, and the maze is not an instance).
	mazeChestTries = 2
	// mazeChestMinPct is the reward below which the solver stops detouring for chests: they pay the
	// same at any percentage, so below this the reward is worth more than the next prize.
	mazeChestMinPct = 50
	// mazeNoHoldDetour is the most extra steps a chest may add to a room's walk when the reward
	// cannot be held. Each step then costs a tenth of a point of reward and the chest's own click a
	// whole one, for a prize worth less than a point of reward, so only chests all but on the way.
	mazeNoHoldDetour = 6
	// mazeOpWalkGiveUp is how many unanswered MOVE_OPCLICK walks, with none ever answered, make a
	// process stop trying them (every account shares one server).
	mazeOpWalkGiveUp = 3
	// mazePackSlots is the size of the backpack.
	mazePackSlots = 28
	// mazeRoomMaxCells bounds a room search: the whole maze is ~2800 tiles.
	mazeRoomMaxCells = 4000
)

// MOVE_OPCLICK walks answered / unanswered across every solve in the process.
var mazeOpWalkWorked, mazeOpWalkRefused atomic.Int32

func opWalkUsable() bool {
	return !(mazeOpWalkRefused.Load() >= mazeOpWalkGiveUp && mazeOpWalkWorked.Load() == 0)
}

// mazeReward is one solve's reward-hold and chest state.
type mazeReward struct {
	// stallOff and lootOff switch the features off for the rest of the solve.
	stallOff, lootOff bool

	hold      bool      // a dialogue the solver opened on purpose is up
	holdSince time.Time // when it came up
	held      float64   // ticks held in total
	holds     int       // dialogues opened on purpose

	pct      int       // the overlay's last reading
	pctKnown bool      // the overlay has been read at all
	pctAt    time.Time // when it last changed, i.e. when the timer last ran (or started)
	started  time.Time // when the solve began
	stale    string    // the overlay text left over from an earlier maze, ignored until it changes
	lowLog   bool      // the "stopped looting" line has been written
	fullLog  bool      // the "not enough free backpack slots" line has been written
	opWorked bool      // a MOVE_OPCLICK walk has moved the account in this solve

	looted   map[mazegraph.Cell]bool // chests opened or given up on
	sawEmpty map[mazegraph.Cell]bool // empty chests already logged
	chests   int                     // chests whose prize arrived
	prizes   map[int]int64           // what they gave
}

func (m *mazeSolver) rewardInit() {
	m.rw.looted = map[mazegraph.Cell]bool{}
	m.rw.sawEmpty = map[mazegraph.Cell]bool{}
	m.rw.prizes = map[int]int64{}
	m.rw.started = mazeClock.Now()
	m.rw.pctAt = m.rw.started
	// The client keeps the last text the overlay was given, so the previous maze's final percentage
	// is still there when this one begins: it is not a reading until the server writes a new one.
	m.rw.stale = m.s.State().Text(mazeloot.OverlayComponent)
}

// canHold reports whether the solver may open dialogues and walk with them up.
func (m *mazeSolver) canHold() bool {
	return mazeStallEnabled && !m.rw.stallOff && opWalkUsable()
}

// observe reads the overlay percentage and notes when it changes, which is when the timer ran.
func (m *mazeSolver) observe() {
	text := m.s.State().Text(mazeloot.OverlayComponent)
	if text == "" || (!m.rw.pctKnown && text == m.rw.stale) {
		return
	}
	p, ok := mazeloot.ParsePct(text)
	if !ok {
		return
	}
	if !m.rw.pctKnown || p != m.rw.pct {
		m.rw.pct, m.rw.pctKnown, m.rw.pctAt = p, true, mazeClock.Now()
	}
}

// sinceFire is how many ticks ago the timer last ran (or began, before it has run).
func (m *mazeSolver) sinceFire() float64 {
	return float64(mazeClock.Now().Sub(m.rw.pctAt)) / float64(mazeTick)
}

// pctText is the percentage for a log line.
func (m *mazeSolver) pctText() string {
	m.observe()
	if !m.rw.pctKnown {
		return "an unread percentage"
	}
	return fmt.Sprintf("%d%%", m.rw.pct)
}

// holding reports whether a dialogue the solver opened is still up. One that has since gone (the
// server closed it, or a dismissal did) ends the hold here.
func (m *mazeSolver) holding() bool {
	if !m.rw.hold {
		return false
	}
	if modalOpen(m.s) {
		return true
	}
	m.release()
	return false
}

// markHold records that the dialogue now up is the solver's own.
func (m *mazeSolver) markHold() {
	if !m.canHold() || !modalOpen(m.s) {
		return // a solver that has given up on holding treats the prize box like any other dialogue
	}
	if !m.rw.hold {
		m.rw.hold, m.rw.holdSince = true, mazeClock.Now()
		m.rw.holds++
	}
}

// release ends a hold and logs how long it lasted and where the reward stood.
func (m *mazeSolver) release() {
	if !m.rw.hold {
		return
	}
	m.rw.hold = false
	ticks := float64(mazeClock.Now().Sub(m.rw.holdSince)) / float64(mazeTick)
	m.rw.held += ticks
	m.logf("stalled %.0f ticks at %s", ticks, m.pctText())
}

// dismissOnce sends one dismissal of the open dialogue and returns at once: its own continue
// button when the interface archive is registered, else CLOSE_MODAL. dismissModals loops this.
func dismissOnce(s actions.Session) {
	if comps := currentInterfaceArchive(); comps != nil {
		chatDlg := actions.NewChatDialog(s, comps)
		if chatDlg.CanContinue() {
			if err := chatDlg.Continue(); err == nil {
				return
			}
		}
	}
	_ = actions.CloseModal(s)
}

// beforeOp readies a click that must close the dialogue the solver is holding. The engine closes it
// itself when the click is read (OpLocHandler calls clearPendingAction), so this is belt and braces
// for a server that does not: one dismissal, sent first, with no wait -- a wait would be a tick of
// access the click is about to take anyway.
//
// The client is told it is closed at once, as actions.CloseModal does. With the interface archive
// registered (the live fleet does whenever it fetched it at startup) the dismissal is the
// dialogue's continue button, which leaves the client's modal state alone until the server's
// IF_CLOSE arrives at the end of the next tick (NetworkPlayer.encodeOut), and clickDoor reads any
// dialogue it sees after the click as the server refusing it. Without this every held door read as
// "refused", was dismissed and clicked a second time -- a tick or two lost per door, and a second
// click on a door that may already be swinging.
func (m *mazeSolver) beforeOp() {
	if modalOpen(m.s) {
		dismissOnce(m.s)
		m.s.State().LocalCloseModal()
	}
	m.release()
}

// sendWalk sends one walk, as MOVE_OPCLICK when a held dialogue must survive it.
func (m *mazeSolver) sendWalk(pts []pathfind.NavPoint, keepDialogue bool) error {
	if keepDialogue {
		return actions.WalkOpClick(m.s, pts, m.runKey())
	}
	return actions.Walk(m.s, pts, m.runKey())
}

// refuseOpWalk records that a walk with a dialogue up did not move the player: the server does not
// honour it. The hold is dropped and the rest of the solve is ordinary.
func (m *mazeSolver) refuseOpWalk() {
	mazeOpWalkRefused.Add(1)
	m.rw.stallOff = true
	m.logf("a walk with the dialogue up did not move the account; walking normally from here (%d refused, %d answered so far in this process)", mazeOpWalkRefused.Load(), mazeOpWalkWorked.Load())
	m.release()
	dismissModals(m.s, m.log)
}

// dropHold gives up holding for the rest of the solve because the server closed the dialogue the
// solver was keeping up: a walk sent to keep it open did not, so the reward was running after all.
func (m *mazeSolver) dropHold(why string) {
	mazeOpWalkRefused.Add(1)
	m.rw.stallOff = true
	m.logf("%s; walking normally from here (%d refused, %d answered so far in this process)", why, mazeOpWalkRefused.Load(), mazeOpWalkWorked.Load())
	m.release()
}

// noteOpWalkWorked records the first MOVE_OPCLICK walk of a solve that moved the player.
func (m *mazeSolver) noteOpWalkWorked() {
	if m.rw.opWorked {
		return
	}
	m.rw.opWorked = true
	if mazeOpWalkWorked.Add(1) == 1 {
		m.logf("walking with the dialogue up works on this server")
	}
}

// beforeLeg opens a dialogue first when the walk to target is long enough that the timer would run
// during it (mazeloot.ShouldStall).
func (m *mazeSolver) beforeLeg(target mazegraph.Cell) {
	if !m.canHold() || m.holding() {
		return
	}
	cur, ok := currentCell(m.s, m.level)
	if !ok || cur == target {
		return
	}
	path := m.plan(cur, target)
	if len(path) < 2 {
		return
	}
	m.observe()
	leg := mazeloot.LegTicks(len(path)-1, m.runKey())
	if !mazeloot.ShouldStall(mazeloot.FreeLeft(m.sinceFire()), leg) {
		return
	}
	m.openStall()
}

// openStall puts a dialogue up on purpose by clicking a maze wall: it walks to the nearest wall
// (within mazeStallWallReach tiles) if need be, clicks "Open" and waits for the chat interface.
// It reports whether the reward is now being held. The click also dismisses whatever dialogue was
// up before it (the Old Man's briefing at the start), because the server closes it as the click is
// read.
func (m *mazeSolver) openStall() bool {
	if !m.canHold() {
		return false
	}
	cur, ok := currentCell(m.s, m.level)
	if !ok {
		return false
	}
	wall, steps, found := m.g.NearestWall(cur, mazeStallWallReach)
	if !found {
		m.logf("no wall within %d tiles of (%d,%d) to click; not holding the reward here", mazeStallWallReach, cur.X, cur.Z)
		return false
	}
	if steps > 0 && !m.walkLeg(wall) {
		return false
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if modalOpen(m.s) && !m.rw.hold {
			dismissOnce(m.s) // the briefing, or a refusal: the click closes it too, this is for a server where it does not
		}
		// The dialogue to wait for is one that OPENS after the click. Reading "a chat interface is
		// up" would take the briefing, still on the client's screen until the server's IF_CLOSE
		// arrives, for the answer to the click.
		watch := watchDialogues(m.s)
		err := actions.OpLoc(m.s, 1, wall.X, wall.Z, mazegraph.WallHighID)
		up := false
		if err == nil {
			deadline := mazeClock.Now().Add(mazeStallOpenWait)
			for !up && mazeClock.Now().Before(deadline) {
				mazeClock.Sleep(mazePoll)
				m.observe()
				up = watch.opened.Load() > 0 && modalOpen(m.s)
			}
		}
		watch.cancel()
		if err != nil {
			return false
		}
		if up {
			m.rw.hold, m.rw.holdSince = true, mazeClock.Now()
			m.rw.holds++
			return true
		}
	}
	m.rw.stallOff = true
	m.logf("clicking the wall at (%d,%d) put up no dialogue; not holding the reward for the rest of this solve", wall.X, wall.Z)
	return false
}

// dialogueWatch counts the chat dialogues that have opened since it was made.
type dialogueWatch struct {
	opened atomic.Int32
	cancel func()
}

func watchDialogues(s actions.Session) *dialogueWatch {
	w := &dialogueWatch{}
	w.cancel = s.State().Subscribe(func(e gamestate.Event) {
		if e.Kind == gamestate.EventModalChanged && e.Chat != gamestate.NoModal && e.Chat != e.PreviousChat {
			w.opened.Add(1)
		}
	})
	return w
}

// begin is the start of a pass: the first one tries to hold the reward at once, before the timer
// has run, which is possible because it only starts counting when the briefing is dismissed.
func (m *mazeSolver) begin(pass int) {
	if pass == 1 && m.canHold() && m.openStall() {
		return
	}
	dismissModals(m.s, m.log)
}

// pack is the backpack's contents by obj id. A backpack the client has not been sent yet reads as
// empty, so the first thing that arrives in it counts as a gain.
func (m *mazeSolver) pack() map[int]int64 {
	out := map[int]int64{}
	inv := m.s.State().Inventory(actions.PlayerPack)
	if inv == nil {
		return out
	}
	for _, it := range inv.Slots {
		if it.ID != 0 {
			out[int(it.ID)] += int64(it.Count)
		}
	}
	return out
}

// freeSlots counts the empty backpack slots. known is false while the client has not been sent the
// whole backpack.
func (m *mazeSolver) freeSlots() (free int, known bool) {
	inv := m.s.State().Inventory(actions.PlayerPack)
	if inv == nil || len(inv.Slots) < mazePackSlots {
		return 0, false // not sent, or not the whole pack
	}
	for _, it := range inv.Slots {
		if it.ID == 0 {
			free++
		}
	}
	return free, true
}

// packLacksRoom reports (logging once per solve) that the backpack is too full to open another
// chest. A chest's prize can take a slot and so can each of the final reward's three rolls
// (mazeloot.RewardSlots), and an inv_add with nowhere to go drops the overflow on the ground at
// the tile the account is returned to, where nothing picks it up: a chest that took the last slots
// would cost the reward it was opened on the way to. A backpack the client has not been sent (the
// server sends it at login, so this is not seen live) cannot be shown to have the room, so it
// counts as having none.
func (m *mazeSolver) packLacksRoom() bool {
	free, known := m.freeSlots()
	if known && mazeloot.ChestHasRoom(free) {
		return false
	}
	if !m.rw.fullLog {
		m.rw.fullLog = true
		if !known {
			m.logf("backpack contents not known; not opening chests")
		} else {
			m.logf("backpack has only %d free slot(s) and the reward may need %d (a chest prize %d more), so not opening chests",
				free, mazeloot.RewardSlots, mazeloot.PrizeSlots)
		}
	}
	return true
}

func itemName(id int) string {
	if r := currentResolver(); r != nil {
		if o, ok := r.Obj(id); ok && o.Name != "" {
			return o.Name
		}
	}
	return fmt.Sprintf("obj %d", id)
}

// awaitPrize waits for a chest's prize to reach the pack and describes it.
func (m *mazeSolver) awaitPrize(before map[int]int64) (gained map[int]int64, ok bool) {
	deadline := mazeClock.Now().Add(mazeChestSettle)
	for mazeClock.Now().Before(deadline) {
		mazeClock.Sleep(mazePoll)
		m.observe()
		after := m.pack()
		g := map[int]int64{}
		for id, n := range after {
			if d := n - before[id]; d > 0 {
				g[id] = d
			}
		}
		if len(g) > 0 {
			return g, true
		}
	}
	return nil, false
}

func describePrize(g map[int]int64) string {
	var out string
	for id, n := range g {
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%d x %s", n, itemName(id))
	}
	return out
}

// emptyChests are the chests the client has seen turn into the empty chest, which is what another
// player's opening leaves for 100 ticks (the maze is not an instance). The server tells every
// client in range with a loc change, so a chest we are about to walk to can be skipped unclicked.
func (m *mazeSolver) emptyChests() map[mazegraph.Cell]bool {
	out := map[mazegraph.Cell]bool{}
	for _, l := range m.s.State().Locs() {
		if l.Type != mazegraph.ChestEmptyLocID || l.Level != m.level {
			continue
		}
		if c := (mazegraph.Cell{X: l.X, Z: l.Z}); mazegraph.IsChest(c) {
			out[c] = true
		}
	}
	return out
}

// lootRoom opens the chests in the room the player is standing in on the way to end (the next
// door's approach tile), in the cheapest order. With the reward held a chest costs one point at
// most and often nothing (it shares a window with a neighbour or the door); without the hold each
// step of detour costs, so only chests within mazeNoHoldDetour extra steps are taken.
func (m *mazeSolver) lootRoom(end mazegraph.Cell) {
	if !mazeLootEnabled || m.rw.lootOff {
		return
	}
	cur, ok := currentCell(m.s, m.level)
	if !ok {
		return
	}
	if m.lowReward() {
		return
	}
	detour := -1
	if !m.canHold() {
		detour = mazeNoHoldDetour
	}
	skip := map[mazegraph.Cell]bool{}
	for c := range m.rw.looted {
		skip[c] = true
	}
	// A chest someone else emptied is full again 100 ticks later, so it is skipped for this room
	// only, not marked as done.
	room := m.g.Room(cur, mazeRoomMaxCells)
	for c := range m.emptyChests() {
		if skip[c] {
			continue
		}
		skip[c] = true
		for _, st := range m.g.ChestStands(c) {
			if room[st] && !m.rw.sawEmpty[c] {
				m.rw.sawEmpty[c] = true
				m.logf("chest at %d,%d is empty (someone else opened it in the last minute); leaving it", c.X, c.Z)
				break
			}
		}
	}
	visits := mazeloot.PlanChestVisits(m.g, mazegraph.Chests, cur, end, skip, detour)
	for _, v := range visits {
		// The backpack is checked before every chest that would be opened (so the one line saying
		// why is written only when a chest was really passed up), and again for each of a room's
		// chests: every prize that lands in a new slot narrows the room the reward has.
		if !inMazeSquare(m.s) || m.lowReward() || m.packLacksRoom() {
			return
		}
		m.lootChest(v)
	}
}

// lowReward reports (logging once) that the reward is under mazeChestMinPct.
func (m *mazeSolver) lowReward() bool {
	m.observe()
	if !m.rw.pctKnown || m.rw.pct >= mazeChestMinPct {
		return false
	}
	if !m.rw.lowLog {
		m.rw.lowLog = true
		m.logf("reward is down to %d%%; not detouring for more chests", m.rw.pct)
	}
	return true
}

// lootChest walks to a chest's stand tile and opens it, giving it up after mazeChestTries clicks
// that bring no prize.
func (m *mazeSolver) lootChest(v mazeloot.Visit) {
	m.rw.looted[v.Chest] = true
	for try := 1; try <= mazeChestTries; try++ {
		if !inMazeSquare(m.s) {
			return
		}
		if !m.walkTo(v.Stand) {
			m.logf("chest at %d,%d: could not stand on (%d,%d); leaving it", v.Chest.X, v.Chest.Z, v.Stand.X, v.Stand.Z)
			return
		}
		if m.emptyChests()[v.Chest] {
			m.logf("chest at %d,%d turned empty before we got to it (someone else opened it); leaving it", v.Chest.X, v.Chest.Z)
			return
		}
		before := m.pack()
		if m.holding() {
			m.beforeOp()
		} else {
			dismissModals(m.s, m.log)
		}
		if err := actions.OpLoc(m.s, 1, v.Chest.X, v.Chest.Z, mazegraph.ChestLocID); err != nil {
			m.logf("chest at %d,%d: click failed: %v", v.Chest.X, v.Chest.Z, err)
			return
		}
		if gained, ok := m.awaitPrize(before); ok {
			m.rw.chests++
			for id, n := range gained {
				m.rw.prizes[id] += n
			}
			m.logf("looted chest at %d,%d: %s (reward %s)", v.Chest.X, v.Chest.Z, describePrize(gained), m.pctText())
			// The prize box is a dialogue: the reward is held until the next click. The pack and the
			// dialogue reach the client in the same tick, so give the dialogue a moment to show.
			for w := time.Duration(0); w < mazeTick && !modalOpen(m.s); w += mazePoll {
				mazeClock.Sleep(mazePoll)
			}
			m.markHold()
			return
		}
		m.logf("chest at %d,%d gave nothing within %s (attempt %d of %d)", v.Chest.X, v.Chest.Z, mazeChestSettle, try, mazeChestTries)
	}
}

// summary is the closing line of a solve.
func (m *mazeSolver) summary(doors int) string {
	total := float64(mazeClock.Now().Sub(m.rw.started)) / float64(mazeTick)
	return fmt.Sprintf("finished at %s: %d doors, %d chests looted, %d dialogues held for %.0f of %.0f ticks",
		m.pctText(), doors, m.rw.chests, m.rw.holds, m.rw.held, total)
}
