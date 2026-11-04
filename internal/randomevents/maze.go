package randomevents

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents/mazegraph"
)

// This file actually escapes the "maze" trapped event detected in detect.go
// (mapsquare 45,71). It does NOT attempt Mime (mapsquare 31,74): see Handle's own doc
// comment on its KindTrapped case for exactly why that one's still out of scope.
//
// This is a PORT of rsb0t-main's real, original solver
// (src/bot/runtime/randomevents/maze/{mazeGraph,mazeLayout,selectRoute,solveMaze}.ts),
// found in the FULL rsb0t-main checkout — a sibling directory of this repo's own
// worktree, not reference/rsb0t-scripts/ (see COLLAB.md's "maze worktree" entries for
// the full account; an earlier from-scratch attempt, internal/randomevents/mazesolve,
// is left in the tree as an independently-useful generic solver but is not used here).
//
// The graph/route logic lives in internal/randomevents/mazegraph (pure, no live
// dependencies, tests port the original's pinned real-map assertions). This file is
// only the live glue: for each door the route names, stand on its approach tile, open
// it with its statically-known type id, confirm the player actually crossed the edge,
// then Touch the shrine.
//
// Why this glue exists in its current shape — the 2026-09-14 production incident,
// account `spare2`, stuck for hours at (2902,4555) logging "door 1/18 at
// (2903,4554) would not open" every 3 seconds. Two real, independently sufficient
// causes, both found by reading the engine/content sources rather than guessed:
//
//  1. **A chat modal was open the whole time, and OPLOC is silently absorbed while one
//     is.** The real event's own start (content/scripts/macro events/scripts/general/
//     macro_event_maze.rs2, [proc,start_macro_maze]) ends with a
//     ~chatnpc_specific("Mysterious Old Man", ...) — a p_pausebutton chat modal that
//     stays open until the player clicks "Click here to continue". The engine's
//     OpLocHandler accepts the packet and sets the interaction, but
//     Player.processInteraction only ever calls tryInteract/pathToTarget when
//     canAccess() is true, and canAccess() is `!protect && !delayed &&
//     !containsModalInterface()` (engine/src/engine/entity/Player.ts). MOVE_GAMECLICK
//     is NOT gated the same way (MoveClickHandler only checks `delayed`), which is
//     exactly why the account could walk 11 tiles from the SW spawn to the door and
//     then never open it. The TS original clears this ("clearMesbox") before routing
//     and before every door click; the previous port dropped that step. The same
//     mechanism also swallows every click after a wrong-side attempt, whose
//     ~mesbox("I don't think that's the right way.") is another pausebutton modal.
//     dismissModals below is the port of clearMesbox: press continue when the
//     interface archive is registered (see SetInterfaceArchive), otherwise
//     CLOSE_MODAL — the engine's own closeModal() aborts a PAUSEBUTTON-suspended
//     script cleanly either way.
//
//  2. **The solver stood diagonal to the door.** The old walk stopped anywhere
//     Chebyshev-adjacent to the door tile, and the BFS naturally arrives at the
//     diagonal (2902,4555) first. A straight-wall loc is only operable from its own
//     tile, the tile across its blocked edge, or the two tiles flanking it along the
//     wall (engine ReachStrategy.reachWall1) — never a diagonal — and the door script's
//     own side test (door_procs.rs2 check_axis) decides which way to move the player
//     purely from whether the player shares the door's axis coordinate. mazegraph's
//     route now records the exact cell it crosses each door edge from (DoorInfo.From),
//     and openDoor walks onto that tile (stopDist 0) before clicking. See
//     mazegraph.ApproachTile and its tests for the never-diagonal guarantee.
//
// The 3 October 2026 failures (59 of 94 solves; run/fleet.log, 113 "could not stand on
// approach tile", 79 "walk leg ... stalled", 34 "no open-floor path") had three more causes,
// each reproduced offline by maze_sim_test.go's engine replay:
//
//  3. **The account's own script kept walking.** A maze teleport lands mid-script; a
//     webwalker part-way through a walk home kept clicking toward it ("walk to 3222,3218,0:
//     scene/unstick/backoff") and opened "shut walls" (the maze walls, whose op is a mesbox
//     that then swallowed the solver's door clicks), dragging the account to dead ends such as
//     (2888,4574). 18 of the 25 failed sessions had script walk lines inside the handling. The
//     solver now holds the account's movement (actions.HoldMovement) while it stands in the
//     maze: the script's Walk/OpLoc calls are dropped and counted, the solver's go out.
//
//  4. **A stalled walk was re-sent from where it started.** For the 3 ticks after a door
//     opens, its open leaf is a wall on the tile across it (mazegraph.DoorInfo.LeafEdge) —
//     the tile the player lands on when crossing from the door's own tile — and it can stand
//     across the next walk's first step (door (2898,4600) then (2895,4598); (2916,4586) then
//     (2912,4584)). The engine just holds the player until it goes, but the walk's 1.5 s stall
//     check fired first and re-sent the SAME waypoints, whose first was the tile the walk
//     started on. The player, by then on the move, ran back toward it, the door click
//     landed meanwhile, and the first tile change was read as a crossing ("door (2895,4598)
//     opened ... -> (2897,4599)", 7 times; also (2900,4567), (2906,4586), (2912,4584)); the
//     next door then had "no open-floor path". Now a walk is never re-sent: a stalled or
//     short walk is re-planned from the live tile; the stall wait (4 ticks) outlasts the
//     leaf; a walk planned inside the leaf's life routes round it when that costs at most
//     two tiles; and a door counts as opened only when the player stands on DoorInfo.To.
//
//  5. **Time thrown away on every door.** The reward (%xplamp) drops 1% every 5 ticks while
//     the player is not busy. Every walk sent the player's own tile as its first waypoint,
//     which the engine spends a whole tick "reaching" (no step, no run step); the solver
//     slept a tick after every door that the door script does not need (open_and_close_door
//     only delays when entered from a flank, which the route never does); legs of 25 cells
//     stopped the player between them; and a walking account walked. Walks are now sent as
//     the real client sends them (mazegraph.Waypoints: corners, never the start tile, a whole
//     room in one click) with the run key held while run energy lasts.
//
// The 13 October 2026 change (maze_loot.go, mazeloot, docs/maze_reward_hold_2026-10-13.md) is about
// the reward, not the route. A solve took ~100 s live and the reward (%xplamp, 100 at the start)
// drops a point every 5 ticks the account is "accessible", so it finished near 68%. The timer is a
// NORMAL one: it does not run while the player holds a dialogue, a blocked timer runs once when the
// block ends (not once per missed interval), and MOVE_OPCLICK -- the walk half of a loc click --
// walks the player without closing the dialogue. So the solver now clicks one of the maze's own
// walls (its "Open" is ~mesbox("I can't open that.")) to put a dialogue up, walks each long leg with
// WalkOpClick, and lets the click that uses a door, a chest or the statue close it: one point lost
// per action window instead of one per 3 seconds. It also opens the three chests in the rooms the
// route passes through (a chest's prize box is a dialogue too). Per Engine-TS the click that uses a
// door closes the dialogue as the packet is read (OpLocHandler.ts line 53), so reading (1) above as
// "OPLOC is ignored while a dialogue is up" is wrong for that engine; the solver still dismisses
// first, and everything new is checked as it goes -- a walk with a dialogue up that does not move
// the account, or a failed pass, drops the rest of the solve back to the classic order.
//
// Verification is behavioural and real: [oploc1,_macro_maze_wall_door] →
// ~open_and_close_door p_teleports the player across the edge (onto the door's own
// tile when clicked from the far side, or one tile past it when clicked from its own
// tile), so "the tracked tile is now DoorInfo.To" is the server's own confirmation the
// door opened; a chat modal appearing instead is the server's own refusal. Every failure
// re-routes from wherever the player actually is (mazegraph.SelectRoute is a BFS from an
// arbitrary tile) rather than assuming the player is still in step with a pre-computed
// route.
//
// Static locs (the walls and doors) never arrive over the wire — they live in the
// client's own map files, which this client doesn't parse for this mapsquare — so
// gamestate.State.Locs() is empty here by design, not by bug. Nothing in this file
// needs it: door ids/positions come from mazegraph.MazeLayout and the server validates
// OPLOC against its own authoritative zone.
type LocResolver interface {
	Loc(id int) (cachecfg.Loc, bool)
}

const (
	// mazeTick is one game tick.
	mazeTick = 600 * time.Millisecond
	// mazePoll is how often a wait looks at the tracked tile.
	mazePoll = 100 * time.Millisecond
	// mazeWalkStallWait is how long a walk may show no movement at all before it is
	// re-planned from the live tile and sent again: four ticks, longer than the three a
	// freshly opened door's leaf can hold the player up (the engine keeps the waypoints and
	// carries on by itself once the leaf goes), so that case is never re-sent.
	mazeWalkStallWait = 4 * mazeTick
	// mazeWalkSends bounds how many clicks one walk to an approach tile may take (a stall
	// or a stop short of the end re-plans from the live tile and clicks again).
	mazeWalkSends = 4
	// mazeLeafLife is how long a door's open leaf stands after the door opens (loc_add 3).
	mazeLeafLife = 3 * mazeTick
	// mazeLeafDetour is the most extra tiles a walk takes to stay clear of a standing leaf.
	mazeLeafDetour = 2
	// mazeRunMinEnergy is the run energy (percent) below which the run key is not held; the
	// account's own run setting still applies.
	mazeRunMinEnergy = 10
	// mazeDoorSettleWait bounds how long a single OpLoc attempt on a door is given to
	// either move the player across the edge or produce a refusal modal.
	mazeDoorSettleWait = 3000 * time.Millisecond
	// mazeDoorRetries is how many OpLoc clicks a single door gets (re-walking to the
	// approach tile between them) before the whole route is recomputed.
	mazeDoorRetries = 3
	// mazeReroutes bounds how many times one SolveMazeEvent call recomputes the route
	// from the live tile after a door or walk failure before reporting the error —
	// Guardian re-invokes Handle for trapped events regardless (see guardian.go), so
	// this only needs to cover in-flight desync, not be infinite.
	mazeReroutes = 3
	// mazePathMaxCells bounds PathWithinRoom's own BFS — generous against the whole
	// maze's ~53x53 tile extent.
	mazePathMaxCells = 4000
	// mazeExitWait is how long to wait after touching the shrine for the mapsquare to
	// change: end_macro_maze is jingle + anim + p_delay(5) + ~macro_return_teleport,
	// so ~4s normally; 12s matches the TS original's own budget.
	mazeExitWait = 12 * time.Second
	// mazeWalkLegMax is the most waypoints one MOVE_GAMECLICK carries (actions.Walk's own
	// maxWaypoints, the real client's 25). Corners, not cells: every room in this maze is
	// far under it, so a walk is one click.
	mazeWalkLegMax = 25
)

// modalOpen reports whether the engine would currently refuse to act on an OPLOC for
// this player because a main or chat interface is open (Player.containsModalInterface
// — the side tab alone doesn't count, matching the engine's own check).
func modalOpen(s actions.Session) bool {
	main, _, chat := s.State().Modals()
	return main != -1 || chat != -1
}

// dismissModals ports solveMaze.ts's clearMesbox: clear any open chat/main modal so
// the next OPLOC actually executes (see this file's doc comment for the engine
// mechanics). Prefers the dialogue's own continue button when the interface archive
// is registered — the same thing a real player does — and otherwise sends
// CLOSE_MODAL, which the engine's closeModal() handles identically for a
// pausebutton-suspended script. Bounded, since a modal that keeps re-opening is not
// something clicking harder fixes.
func dismissModals(s actions.Session, log func(string)) {
	for i := 0; i < 6 && modalOpen(s); i++ {
		main, _, chat := s.State().Modals()
		dismissOnce(s)
		if log != nil && i == 0 {
			log(fmt.Sprintf("dismissing open modal (main=%d chat=%d) so door clicks aren't ignored", main, chat))
		}
		mazeClock.Sleep(mazeTick)
	}
}

func currentCell(s actions.Session, level int) (mazegraph.Cell, bool) {
	x, z, l, ok := s.State().Tile()
	if !ok || l != level {
		return mazegraph.Cell{}, false
	}
	return mazegraph.Cell{X: x, Z: z}, true
}

// mazeSolver is one SolveMazeEvent call's live state.
type mazeSolver struct {
	s     actions.Session // the movement hold's Owner: the only clicks the server sees
	level int
	g     *mazegraph.Graph
	log   func(string)
	// leaf is the EdgeKey of the last opened door's leaf, standing until leafTill.
	leaf     string
	leafTill time.Time
	// rw is the reward hold and chest looting (maze_loot.go).
	rw mazeReward
}

func (m *mazeSolver) logf(format string, args ...any) {
	if m.log != nil {
		m.log(fmt.Sprintf(format, args...))
	}
}

// plan is the cell path from cur to target through the room's open floor (doors count as
// closed), kept clear of a door leaf that is still standing when that costs at most
// mazeLeafDetour extra tiles. Nil when target is not in the player's room.
func (m *mazeSolver) plan(cur, target mazegraph.Cell) []mazegraph.Cell {
	path := m.g.PathWithinRoom(cur, target, 0, mazePathMaxCells)
	if path == nil || m.leaf == "" || !mazeClock.Now().Before(m.leafTill) || !pathCrosses(path, m.leaf) {
		return path
	}
	if alt := m.g.PathAvoiding(cur, target, mazePathMaxCells, map[string]bool{m.leaf: true}); alt != nil && len(alt) <= len(path)+mazeLeafDetour {
		return alt
	}
	return path // the engine holds the player until the leaf goes, then carries on
}

func pathCrosses(path []mazegraph.Cell, edge string) bool {
	for i := 1; i < len(path); i++ {
		if mazegraph.EdgeKey(path[i-1].X, path[i-1].Z, path[i].X, path[i].Z) == edge {
			return true
		}
	}
	return false
}

// runKey is whether to hold the run key (MOVE_GAMECLICK's ctrlHeld): yes unless run energy
// is known to be low. The engine itself ignores it below 1%.
func (m *mazeSolver) runKey() bool {
	energy, ok := m.s.State().Energy()
	return !ok || energy >= mazeRunMinEnergy
}

// walkOutcome is how one walk click ended.
type walkOutcome int

const (
	walkArrived walkOutcome = iota // on the click's last waypoint
	walkStalled                    // never moved
	walkShort                      // moved, then stopped (or ran out of time) short of the end
)

// walkTo walks the player onto exactly target through the room's open floor, first putting a
// dialogue up to hold the reward when the leg is long enough to matter (beforeLeg).
func (m *mazeSolver) walkTo(target mazegraph.Cell) bool {
	m.beforeLeg(target)
	return m.walkLeg(target)
}

// walkLeg is walkTo without the reward hold. Each click is planned from the live tile, so a
// stall or a stop short of the end re-plans from wherever the player really is; nothing is ever
// re-sent from a tile the player has left. While the solver holds a dialogue the walk goes out as
// MOVE_OPCLICK so the dialogue survives it; a walk that then does not move the player ends the
// hold for the rest of the solve (refuseOpWalk).
func (m *mazeSolver) walkLeg(target mazegraph.Cell) bool {
	var last mazegraph.Cell
	for sends := 0; sends < mazeWalkSends; sends++ {
		cur, ok := currentCell(m.s, m.level)
		if !ok {
			return false
		}
		last = cur
		if cur == target {
			return true
		}
		path := m.plan(cur, target)
		if path == nil {
			m.logf("no open-floor path from (%d,%d) to (%d,%d) within the current room", cur.X, cur.Z, target.X, target.Z)
			return false
		}
		wps := mazegraph.Waypoints(path)
		if len(wps) > mazeWalkLegMax {
			wps = wps[:mazeWalkLegMax] // the next click carries on from where this one ends
		}
		pts := make([]pathfind.NavPoint, len(wps))
		for i, c := range wps {
			pts[i] = pathfind.NavPoint{X: c.X, Z: c.Z, Level: m.level}
		}
		keep := m.holding()
		if keep && !opWalkUsable() {
			m.release()
			dismissModals(m.s, m.log)
			keep = false
		}
		if err := m.sendWalk(pts, keep); err != nil {
			m.logf("walk send error: %v", err)
			return false
		}
		end := wps[len(wps)-1]
		out := m.await(cur, end, len(path))
		if keep {
			if out == walkStalled {
				m.refuseOpWalk()
				continue
			}
			if !modalOpen(m.s) {
				// It moved, but the dialogue is gone: this server closes it for a MOVE_OPCLICK too, so
				// the reward has been running all along. Holding is not worth its clicks here.
				m.dropHold("a walk sent to keep the dialogue up closed it")
			} else {
				m.noteOpWalkWorked()
			}
		}
		if out == walkArrived && end == target {
			return true
		}
	}
	if now, ok := currentCell(m.s, m.level); ok {
		last = now
	}
	m.logf("walk leg to (%d,%d) stalled at (%d,%d)", target.X, target.Z, last.X, last.Z)
	return false
}

// await waits for the player to stand on end after a click from `from` over `cells` tiles.
func (m *mazeSolver) await(from, end mazegraph.Cell, cells int) walkOutcome {
	deadline := mazeClock.Now().Add(time.Duration(cells+4)*mazeTick + mazeWalkStallWait)
	last, stillSince := from, mazeClock.Now()
	for mazeClock.Now().Before(deadline) {
		mazeClock.Sleep(mazePoll)
		m.observe()
		now, ok := currentCell(m.s, m.level)
		if !ok {
			continue
		}
		if now == end {
			return walkArrived
		}
		if now != last {
			last, stillSince = now, mazeClock.Now()
		} else if mazeClock.Now().Sub(stillSince) > mazeWalkStallWait {
			break
		}
	}
	if last == from {
		return walkStalled
	}
	return walkShort
}

// doorOutcome is what one OpLoc click on a door produced, judged from the server's
// own observable reactions (see this file's doc comment).
type doorOutcome int

const (
	doorOpened  doorOutcome = iota // the player stands across the door edge (DoorInfo.To)
	doorRefused                    // a chat modal appeared (wrong side / can't open)
	doorNothing                    // no crossing within the settle window
)

// clickDoor sends one Open on the door and waits for the server's reaction. Only the player
// standing on the door's far side (door.To, where open_and_close_door teleports them) counts
// as opened: any other tile change is not the door's doing.
func (m *mazeSolver) clickDoor(door mazegraph.DoorInfo) (doorOutcome, mazegraph.Cell) {
	before, ok := currentCell(m.s, m.level)
	if !ok {
		return doorNothing, before
	}
	if err := actions.OpLoc(m.s, 1, door.Tile.X, door.Tile.Z, door.ID); err != nil {
		return doorNothing, before
	}
	crossed := func(now mazegraph.Cell) bool {
		if door.To == (mazegraph.Cell{}) { // not from SolveRoute: no known far side
			return now != before
		}
		return now == door.To
	}
	deadline := mazeClock.Now().Add(mazeDoorSettleWait)
	for mazeClock.Now().Before(deadline) {
		mazeClock.Sleep(mazePoll)
		m.observe()
		if now, ok := currentCell(m.s, m.level); ok && crossed(now) {
			return doorOpened, now
		}
		if modalOpen(m.s) {
			now, _ := currentCell(m.s, m.level)
			return doorRefused, now
		}
	}
	now, _ := currentCell(m.s, m.level)
	return doorNothing, now
}

// openDoor stands on the door's approach tile (never a diagonal — see
// mazegraph.ApproachTile), clears any modal that would make the engine ignore the
// click, clicks Open, and confirms the player really crossed the edge. Retries the
// click a bounded number of times, re-walking to the approach tile each time.
func (m *mazeSolver) openDoor(door mazegraph.DoorInfo) error {
	for attempt := 1; attempt <= mazeDoorRetries; attempt++ {
		// A dialogue the solver is holding to keep the reward up survives the walk and is closed
		// by the click itself (beforeOp); every retry, and every door reached without a hold, is
		// the classic order: clear it, walk, clear it, click.
		if attempt > 1 || !m.holding() {
			m.release()
			dismissModals(m.s, m.log)
		}
		me, ok := currentCell(m.s, m.level)
		if !ok {
			return fmt.Errorf("no known position")
		}
		stand := mazegraph.ApproachTile(door, me)
		if !m.walkTo(stand) {
			return fmt.Errorf("could not stand on approach tile (%d,%d) for door (%d,%d)", stand.X, stand.Z, door.Tile.X, door.Tile.Z)
		}
		if attempt == 1 && m.holding() {
			m.beforeOp()
		} else {
			m.release()
			dismissModals(m.s, m.log)
		}
		outcome, now := m.clickDoor(door)
		switch outcome {
		case doorOpened:
			m.logf("door (%d,%d) type %d opened from (%d,%d) -> (%d,%d) (attempt %d)", door.Tile.X, door.Tile.Z, door.ID, stand.X, stand.Z, now.X, now.Z, attempt)
			a, b := door.LeafEdge()
			m.leaf, m.leafTill = mazegraph.EdgeKey(a.X, a.Z, b.X, b.Z), mazeClock.Now().Add(mazeLeafLife)
			// No wait here: open_and_close_door leaves the player undelayed after its
			// teleport (it delays only when entered from a flank), so the next walk is taken
			// at once; a click the server did drop is caught by the walk's stall check.
			return nil
		case doorRefused:
			m.logf("door (%d,%d) type %d refused from (%d,%d) — server put up a dialogue (attempt %d)", door.Tile.X, door.Tile.Z, door.ID, stand.X, stand.Z, attempt)
			dismissModals(m.s, m.log)
		default:
			m.logf("door (%d,%d) type %d: no crossing from (%d,%d) within %s, now at (%d,%d) (attempt %d)", door.Tile.X, door.Tile.Z, door.ID, stand.X, stand.Z, mazeDoorSettleWait, now.X, now.Z, attempt)
		}
	}
	return fmt.Errorf("door (%d,%d) type %d would not open after %d attempts", door.Tile.X, door.Tile.Z, door.ID, mazeDoorRetries)
}

func inMazeSquare(s actions.Session) bool {
	x, z, level, ok := s.State().Tile()
	return ok && level == 0 && x>>6 == mazeSquareMX && z>>6 == mazeSquareMZ
}

// SolveMazeEvent attempts to actually escape the maze random event from the player's
// current tile, computing the door route via mazegraph.SelectRoute and — if a
// LocResolver is available to look up the shrine's own "Touch" option label —
// clicking through it to genuinely finish the event. See this file's doc comment for
// the mechanics this is built against.
//
// maxSteps is unused by this implementation (kept for API/call-site compatibility with
// Handle, which was written against the earlier from-scratch mazesolve-based
// approach): a static, pre-known door route has no need for a live step budget the
// way blind maze exploration did.
//
// Returns the number of doors actually opened, and an error: nil only on confirmed
// completion (the shrine was touched and the tracked mapsquare left 45,71); a
// descriptive error naming exactly which door or walk step failed otherwise.
func SolveMazeEvent(s actions.Session, res LocResolver, maxSteps int) (int, error) {
	return SolveMazeEventVerbose(s, res, maxSteps, nil)
}

// SolveMazeEventVerbose is SolveMazeEvent with an optional per-step log callback
// (route summary, each door's open/fail outcome) for live diagnosis; Handle never
// needs this detail so it always calls the plain SolveMazeEvent above, but
// cmd/mazetest does.
//
// It takes the account's movement for as long as the account stands in the maze
// (actions.HoldMovement): the script's own walk and loc clicks are dropped until it is out,
// including between Guardian retries after an error; a solve that leaves releases it.
func SolveMazeEventVerbose(s actions.Session, res LocResolver, _ int, log func(string)) (int, error) {
	_, _, level, ok := s.State().Tile()
	if !ok {
		return 0, fmt.Errorf("randomevents: SolveMazeEvent: no known position")
	}
	hold := actions.HoldMovement(s, func() bool { return inMazeSquare(s) })
	defer func() {
		if n := hold.Dropped(); n > 0 && log != nil {
			log(fmt.Sprintf("held off %d walk/loc click(s) the account's script sent while the maze was being solved", n))
		}
	}()
	m := &mazeSolver{s: hold.Owner, level: level, g: mazegraph.BuildLayoutGraph(), log: log}
	n, err := m.solve(res)
	if err == nil {
		hold.Release()
	}
	return n, err
}

func (m *mazeSolver) solve(res LocResolver) (int, error) {
	s := m.s
	doorsOpened := 0
	m.rewardInit()

	var lastErr error
	solved := false
	for pass := 1; pass <= mazeReroutes && !solved; pass++ {
		// The event's own start leaves the Mysterious Old Man's briefing modal open
		// (see this file's doc comment) — clear it before anything else. The first pass does it
		// by putting a dialogue of its own up, which holds the reward from the first tick (begin).
		m.begin(pass)
		start, ok := currentCell(s, m.level)
		if !ok {
			return doorsOpened, fmt.Errorf("randomevents: SolveMazeEvent: lost position")
		}
		route := mazegraph.SelectRoute(start)
		if route == nil {
			return doorsOpened, fmt.Errorf("randomevents: SolveMazeEvent: no route to the shrine from (%d,%d) — see mazegraph.SolveRoute's doc comment for why this can be a real, honest outcome", start.X, start.Z)
		}
		m.logf("route (pass %d): %d door(s) from (%d,%d), first (%d,%d) approached from (%d,%d)", pass, len(route), start.X, start.Z, route[0].Tile.X, route[0].Tile.Z, route[0].From.X, route[0].From.Z)
		lastErr = nil
		for i, door := range route {
			if !inMazeSquare(s) {
				return doorsOpened, nil // left the maze mid-route (shouldn't happen, but don't fight it)
			}
			m.lootRoom(door.From)
			if err := m.openDoor(door); err != nil {
				lastErr = fmt.Errorf("randomevents: SolveMazeEvent: door %d/%d at (%d,%d): %w", i+1, len(route), door.Tile.X, door.Tile.Z, err)
				m.logf("%s — re-routing from the live tile", lastErr.Error())
				// A failed pass: the rest of the solve is the classic one, with no dialogue held and
				// no chest detours, so the retry is the solver that has always worked.
				m.rw.stallOff, m.rw.lootOff = true, true
				m.release()
				break
			}
			doorsOpened++
		}
		solved = lastErr == nil
	}
	if !solved {
		return doorsOpened, lastErr
	}

	// The chamber door's own open action lands the player on (2911,4576), inside the
	// shrine's footprint (matching solveMaze.ts's own "post-door tile" note) — walk to
	// the west face only if that somehow didn't happen.
	if me, ok := currentCell(s, m.level); ok && chebyshev(me.X, me.Z, mazegraph.Shrine.X, mazegraph.Shrine.Z) > 2 {
		m.walkTo(mazegraph.ShrineDoor)
	}

	option := 1
	if res != nil {
		if info, ok := res.Loc(mazegraph.ShrineLocID); ok {
			if idx := cachecfg.OpIndex(info.Ops, "Touch"); idx > 0 {
				option = idx
			}
		}
	}
	for attempt := 1; attempt <= mazeDoorRetries; attempt++ {
		if attempt == 1 && m.holding() {
			m.beforeOp()
		} else {
			m.release()
			dismissModals(s, m.log)
		}
		mx, mz, _, _ := s.State().Tile()
		m.logf("touching shrine (type %d) at (%d,%d), option %d, from (%d,%d) (attempt %d)", mazegraph.ShrineLocID, mazegraph.Shrine.X, mazegraph.Shrine.Z, option, mx, mz, attempt)
		if err := actions.OpLoc(s, option, mazegraph.Shrine.X, mazegraph.Shrine.Z, mazegraph.ShrineLocID); err != nil {
			return doorsOpened, fmt.Errorf("randomevents: SolveMazeEvent: touching the shrine: %w", err)
		}
		// end_macro_maze: if_close; jingle; anim; p_delay(5); ~macro_return_teleport.
		deadline := mazeClock.Now().Add(mazeExitWait)
		for mazeClock.Now().Before(deadline) {
			mazeClock.Sleep(200 * time.Millisecond)
			if !inMazeSquare(s) {
				if nx, nz, nl, ok := s.State().Tile(); ok {
					m.logf("left the maze: now at (%d,%d,%d)", nx, nz, nl)
				}
				m.logf("%s", m.summary(doorsOpened))
				return doorsOpened, nil
			}
		}
		m.logf("shrine touch did not leave the maze within the wait; retrying")
	}
	return doorsOpened, fmt.Errorf("randomevents: SolveMazeEvent: touched the shrine but the mapsquare didn't change — the event may not have actually completed")
}

// mazeClock is the solver's clock: the real one in production, a simulated one in tests
// (maze_sim_test.go), so a whole maze can be replayed tick by tick without waiting it out.
var mazeClock interface {
	Now() time.Time
	Sleep(time.Duration)
} = realMazeClock{}

type realMazeClock struct{}

func (realMazeClock) Now() time.Time        { return time.Now() }
func (realMazeClock) Sleep(d time.Duration) { time.Sleep(d) }
