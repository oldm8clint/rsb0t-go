// Package live runs internal/webwalk on a game session: a webwalk.Client over actions,
// gamestate, locindex and chatlog, a WorldState read from the session, and one shared Planner
// per collision pack.
//
// A script builds a Walker once and walks with it:
//
//	w, err := live.NewWalker(live.Deps{Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs,
//	    Config: ctx.Config, Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log})
//	res := w.WalkTo(webwalk.Point{X: 2725, Z: 3493}, webwalk.WalkOptions{Radius: 2})
//
// NPC hops (ship fares) and special crossings stay out of its plans until the script sets
// Walker.NPCHop or Walker.SpecialHop to a handler that runs the dialogue.
package live

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the backpack inventory component (fisherrunner.PlayerPack).
const PlayerPack = 3214

// Deps is what a live Walker needs; bot.Context carries all of it.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components lets WorldState count worn items; nil counts none.
	Components archive.Components
	// Chat is the session's game-message log, read for "I can't reach that!"; nil never sees it.
	Chat *chatlog.Log
	// Delay is the bot's cancellable wait (bot.Context.Delay); nil sleeps and never stops.
	Delay func(time.Duration) bool
	Log   func(string)
	// Run holds run on movement clicks.
	Run bool
}

// Client is a webwalk.Client over a session.
type Client struct {
	d Deps
	// blocked are hostiles internal/hostileevade has given up on getting the account past,
	// drained by BlockedByHostiles. Guarded because the break-off runs on the walk's own
	// goroutine while the walker reads this from its poll loop.
	blockedMu sync.Mutex
	blocked   []webwalk.Point
}

// NewClient returns a Client over d.
func NewClient(d Deps) *Client { return &Client{d: d} }

// Tile is the tracked player tile.
func (c *Client) Tile() (webwalk.Point, bool) {
	x, z, level, ok := c.d.Session.State().Tile()
	return webwalk.Point{X: x, Z: z, Level: level}, ok
}

// Client implements webwalk.ViewClient: a walk waits out a view that is silent or catching up on a backlog
// (gamestate.State.ViewLag) instead of reading a stall or a jump from it.
var _ webwalk.ViewClient = (*Client)(nil)

// ViewLagging reports whether the session's picture of the game is not current.
func (c *Client) ViewLagging() (bool, string) {
	l := c.d.Session.State().ViewLag()
	return l.Lagging, l.Why()
}

// Client implements webwalk.TickClient: a stall is counted in the server's ticks, not only in the wall seconds a slow server
// stretches (webwalk/lag.go).
var _ webwalk.TickClient = (*Client)(nil)

// ServerTicks is how many game ticks the session has read.
func (c *Client) ServerTicks() int { return c.d.Session.State().Tick() }

// Walk sends one MOVE_GAMECLICK.
func (c *Client) Walk(path []webwalk.Point, run bool) error {
	pts := make([]pathfind.NavPoint, len(path))
	for i, p := range path {
		pts[i] = pathfind.NavPoint{X: p.X, Z: p.Z, Level: p.Level}
	}
	return actions.Walk(c.d.Session, pts, run)
}

// FindLocs queries the static loc index merged with the session's live loc changes.
func (c *Client) FindLocs(q webwalk.LocQuery) []webwalk.LocMatch {
	if c.d.Locs == nil || c.d.Config == nil {
		return nil
	}
	live := locindex.ChangesFrom(c.d.Session.State().LocChanges())
	base := locindex.Query{X: q.At.X, Z: q.At.Z, Level: q.At.Level, Radius: q.Radius, Op: q.Op}
	var found []locindex.Match
	if len(q.IDs) > 0 {
		base.IDs = q.IDs
		found = c.d.Locs.Find(c.d.Config, base, live)
	} else {
		for _, name := range q.Names {
			byName := base
			byName.Name = name
			found = append(found, c.d.Locs.Find(c.d.Config, byName, live)...)
		}
		sort.SliceStable(found, func(i, j int) bool { return found[i].Distance < found[j].Distance })
	}
	out := make([]webwalk.LocMatch, 0, len(found))
	for _, m := range found {
		out = append(out, webwalk.LocMatch{ID: m.ID, X: m.X, Z: m.Z, Level: m.Level, Name: m.Name, Op: m.Op})
	}
	return out
}

// Client implements webwalk.JumpClient: a hop that lands far away is announced to the account's
// random-event Guardian, which reads it as the crossing it is (randomevents.ExpectHop).
var _ webwalk.JumpClient = (*Client)(nil)

// ExpectHop announces the hop to the Guardian watching this session.
func (c *Client) ExpectHop(name string, from, to webwalk.Point, ttl time.Duration) func() {
	return randomevents.ExpectHop(c.d.Session, name, from.X, from.Z, from.Level, to.X, to.Z, to.Level, ttl)
}

// Client implements webwalk.ApproachClient, so a refused loc hop retries from the tiles the
// server's reach test accepts.
var _ webwalk.ApproachClient = (*Client)(nil)

// Approaches is locreach.Tiles for the loc m names: its placement (angle and shape) from the
// loc index and live changes, its footprint and forceapproach from the config. nil when the
// loc is not found at m's tile or its shape is not one locreach models.
func (c *Client) Approaches(m webwalk.LocMatch) []webwalk.Point {
	if c.d.Locs == nil || c.d.Config == nil {
		return nil
	}
	live := locindex.ChangesFrom(c.d.Session.State().LocChanges())
	q := locindex.Query{X: m.X, Z: m.Z, Level: m.Level, Radius: 0, IDs: []int{m.ID}}
	for _, f := range c.d.Locs.Find(c.d.Config, q, live) {
		if f.X != m.X || f.Z != m.Z || f.Level != m.Level || f.ID != m.ID {
			continue
		}
		typ, ok := c.d.Config.Loc(f.ID)
		if !ok {
			return nil
		}
		l := locreach.Loc{X: f.X, Z: f.Z, Level: f.Level, Width: typ.Width, Length: typ.Length,
			Angle: f.Angle, Shape: f.Shape, ForceApproach: typ.ForceApproach}
		if _, modelled := locreach.Reached(f.X, f.Z, l); !modelled {
			return nil
		}
		ts := locreach.Tiles(l)
		out := make([]webwalk.Point, 0, len(ts))
		for _, t := range ts {
			out = append(out, webwalk.Point{X: t.X, Z: t.Z, Level: t.Level})
		}
		return out
	}
	return nil
}

// OpLoc sends OPLOCn.
func (c *Client) OpLoc(op, x, z, id int) error { return actions.OpLoc(c.d.Session, op, x, z, id) }

// CantReachSince reports whether "I can't reach that!" arrived at or after mark.
func (c *Client) CantReachSince(mark time.Time) bool {
	if c.d.Chat == nil {
		return false
	}
	return strings.Contains(strings.ToLower(c.d.Chat.Since(mark)), "can't reach that")
}

// Delay waits through Deps.Delay.
func (c *Client) Delay(d time.Duration) bool {
	if c.d.Delay != nil {
		return c.d.Delay(d)
	}
	if d > 0 {
		time.Sleep(d)
	}
	return true
}

// Now is the wall clock.
func (c *Client) Now() time.Time { return time.Now() }

// Client implements webwalk.WallClient, so plans see the walls opened doors put up.
var _ webwalk.WallClient = (*Client)(nil)

// ExtraWalls returns the wall faces of live-added, walk-blocking wall locs (straight and L
// shapes) within radius of at, on at's level, that the static map does not already place: an
// opened door's leaves, a closed gate a player shut. Removed walls are not subtracted; the
// planner crosses those through the door graph.
func (c *Client) ExtraWalls(at webwalk.Point, radius int) map[webwalk.Point]byte {
	var walls map[webwalk.Point]byte
	c.eachLiveWall(at, radius, func(l gamestate.LocInstance, _ cachecfg.Loc) {
		if walls == nil {
			walls = map[webwalk.Point]byte{}
		}
		webwalk.AddWallFaces(walls, l.X, l.Z, l.Level, l.Shape, l.Rotate)
	})
	return walls
}

// Client implements webwalk.LiveDoorClient, so a door a player shut where the map has it open
// is a door to open rather than a wall that seals the destination.
var _ webwalk.LiveDoorClient = (*Client)(nil)

// LiveDoorEdges returns both crossings of every live-added straight wall loc within radius of
// at that offers Open: the same locs ExtraWalls walls in, so a plan hops the wall there.
func (c *Client) LiveDoorEdges(at webwalk.Point, radius int) []*webwalk.Edge {
	var out []*webwalk.Edge
	c.eachLiveWall(at, radius, func(l gamestate.LocInstance, t cachecfg.Loc) {
		if l.Shape != 0 {
			return
		}
		for _, op := range t.Ops {
			if strings.EqualFold(strings.TrimSpace(op), "Open") {
				out = append(out, webwalk.LiveDoorEdges(l.X, l.Z, l.Level, l.Shape, l.Rotate, l.Type, t.Name, "Open")...)
				return
			}
		}
	})
	return out
}

// eachLiveWall calls fn for every live-added, walk-blocking wall loc (straight or L) within
// radius of at, on at's level, that the static map does not already place.
func (c *Client) eachLiveWall(at webwalk.Point, radius int, fn func(gamestate.LocInstance, cachecfg.Loc)) {
	if c.d.Config == nil {
		return
	}
	for _, l := range c.d.Session.State().LocChanges() {
		if l.Deleted || l.Level != at.Level || (l.Shape != 0 && l.Shape != 2) {
			continue
		}
		if webwalk.Chebyshev(webwalk.Point{X: l.X, Z: l.Z}, at) > radius {
			continue
		}
		t, ok := c.d.Config.Loc(l.Type)
		if !ok || !t.BlockWalk {
			continue
		}
		static := false
		if c.d.Locs != nil {
			for _, s := range c.d.Locs.At(l.X, l.Z, l.Level) {
				if s.ID == l.Type && s.Shape == l.Shape && s.Angle == l.Rotate {
					static = true
				}
			}
		}
		if static {
			continue
		}
		fn(l, t)
	}
}

// Client implements webwalk.ModalClient, so a door whose open click answers with a dialogue
// is dismissed instead of leaving the account wedged behind an open modal.
var _ webwalk.ModalClient = (*Client)(nil)

// ModalOpen reports whether any main, side or chat modal is currently up.
func (c *Client) ModalOpen() bool {
	main, side, chat := c.d.Session.State().Modals()
	return main != gamestate.NoModal || side != gamestate.NoModal || chat != gamestate.NoModal
}

// CloseModal dismisses whatever modal is up.
func (c *Client) CloseModal() error { return actions.CloseModal(c.d.Session) }

// Client implements webwalk.EventModalClient, so a stalled walk leaves a random event's modal to its handler.
var _ webwalk.EventModalClient = (*Client)(nil)

// ModalOwnedByEvent reports whether a random event's handler is using the session (randomevents.Busy).
func (c *Client) ModalOwnedByEvent() bool { return randomevents.Busy(c.d.Session) }

// Client implements webwalk.ContinueModalClient: a paused chat is answered with its continue button.
var _ webwalk.ContinueModalClient = (*Client)(nil)

// ContinueChat presses "Click here to continue" on the open chat when it offers one.
func (c *Client) ContinueChat() bool {
	if c.d.Components == nil {
		return false
	}
	dlg := actions.NewChatDialog(c.d.Session, c.d.Components)
	return dlg.CanContinue() && dlg.Continue() == nil
}

// Client implements webwalk.CombatClient, so walks resolve danger zones against the
// player's combat level and watch for fights and death.
var _ webwalk.CombatClient = (*Client)(nil)

// CombatLevel is the local player's combat level from its appearance update.
func (c *Client) CombatLevel() (int, bool) { return c.d.Session.State().CombatLevel() }

// InCombat is gamestate's hitmark window.
func (c *Client) InCombat() bool { return c.d.Session.State().InCombat() }

// HP is the current and maximum hitpoints.
func (c *Client) HP() (cur, max int, ok bool) {
	st := c.d.Session.State()
	cur, okCur := st.HP()
	max, okMax := st.MaxHP()
	return cur, max, okCur && okMax
}

// RunEnabled reads the run varp.
func (c *Client) RunEnabled() bool { return c.d.Session.State().RunEnabled() }

// SetRun presses the controls tab's run button.
func (c *Client) SetRun(on bool) error { return actions.SetRun(c.d.Session, on) }

// DiedSince reports whether "Oh dear, you are dead!" arrived at or after mark.
func (c *Client) DiedSince(mark time.Time) bool {
	if c.d.Chat == nil {
		return false
	}
	text := strings.ToLower(c.d.Chat.Since(mark))
	i := strings.Index(text, "oh dear")
	return i >= 0 && strings.Contains(text[i:], "you are dead")
}

// Attackers are the NPCs and players on the player's level within radius that face the
// player. The walker only asks while the player is in combat, when facing means attacking.
func (c *Client) Attackers(radius int) []webwalk.Point {
	st := c.d.Session.State()
	self := st.Self()
	x, z, level, ok := st.Tile()
	if !ok || self < 0 {
		return nil
	}
	me := webwalk.Point{X: x, Z: z, Level: level}
	var out []webwalk.Point
	add := func(p webwalk.Point) {
		if p.Level == level && webwalk.Chebyshev(p, me) <= radius {
			out = append(out, p)
		}
	}
	for _, n := range st.NPCs() {
		if n.TargetsPlayer(self) {
			add(webwalk.Point{X: n.X, Z: n.Z, Level: n.Level})
		}
	}
	for _, p := range st.Players() {
		if p.TargetsPlayer(self) {
			add(webwalk.Point{X: p.X, Z: p.Z, Level: p.Level})
		}
	}
	return out
}

// WorldState snapshots what edge requires are checked against. Skills the server has not
// reported yet read as 0 and quests it has not coloured read as unknown, so gated edges fail
// closed until the session has synced, as the TS walker's live snapshot does.
func (c *Client) WorldState() *webwalk.WorldState {
	st := c.d.Session.State()
	ws := &webwalk.WorldState{Members: c.d.Pack == nil || c.d.Pack.Members, Skills: map[string]int{}}
	skills := st.Skills()
	for i, sk := range skills {
		if name := gamestate.SkillName(i); name != "" && sk.Generation > 0 {
			ws.Skills[strings.ToLower(name)] = sk.Level
		}
	}
	var pack []gamestate.SlotItem
	if inv := st.Inventory(PlayerPack); inv != nil {
		pack = inv.NonEmpty()
	}
	ws.FreeSlots = actions.FreeSlots(pack)
	cfg := c.d.Config
	ws.ItemCount = func(name string) int {
		if cfg == nil {
			return 0
		}
		return int(actions.CountByName(pack, cfg, name))
	}
	ws.WornCount = func(name string) int {
		if cfg == nil || c.d.Components == nil {
			return 0
		}
		return int(actions.CountByName(actions.NewEquipment(c.d.Session, c.d.Components).Worn(), cfg, name))
	}
	// Why CanSlashWeb counts WORN items only, and not the pack: content's
	// general/scripts/slash_checker.rs2 reads inv_getobj(worn, ^wearpos_rhand) and nothing
	// else, so the menu "Slash" op — the only thing webwalk's crossDoor ever sends — needs a
	// blade in the right hand. A Knife in the pack cuts a web solely through the use-on branch
	// (oplocu), which the Walker has no way to send. Proved live 2026-09-18 with
	// cmd/dungeonroutetest: an account ::given a Knife answered every Slash at the Varrock
	// sewer web (loc 733 at 3210,9898) with "Only a sharp blade can cut through this sticky
	// web," six times, and never crossed.
	//
	// It stays nil, which means unknown and lets the edge through, until the worn panel can
	// actually be read: a sidebar tab this client has not been assigned yet reads as nothing,
	// and calling that "no blade" would close the only route to the Moss giants for an account
	// that is in fact wielding one.
	if cfg != nil && c.d.Components != nil {
		if worn := actions.NewEquipment(c.d.Session, c.d.Components).Worn(); worn != nil {
			names := make([]string, 0, len(worn))
			for _, it := range worn {
				if obj, ok := cfg.Obj(int(it.ID)); ok {
					names = append(names, obj.Name)
				}
			}
			canSlash := webwalk.HasSlashTool(names)
			ws.CanSlashWeb = &canSlash
		}
	}
	ws.QuestStatus = func(quest string) webwalk.QuestProgress {
		status, ok := quests.Status(st, quest)
		if !ok {
			return webwalk.QuestUnknown
		}
		switch status {
		case quests.NotStarted:
			return webwalk.QuestNotStarted
		case quests.InProgress:
			return webwalk.QuestStarted
		case quests.Complete:
			return webwalk.QuestComplete
		}
		return webwalk.QuestUnknown
	}
	return ws
}

var (
	plannersMu sync.Mutex
	planners   = map[*pathfind.Pack]*webwalk.Planner{}
)

// PlannerFor returns the shared Planner for a pack over the default graph, building it once.
func PlannerFor(pack *pathfind.Pack) (*webwalk.Planner, error) {
	if pack == nil {
		return nil, errors.New("webwalk/live: no collision pack")
	}
	plannersMu.Lock()
	defer plannersMu.Unlock()
	if p, ok := planners[pack]; ok {
		return p, nil
	}
	g, err := webwalk.Default()
	if err != nil {
		return nil, err
	}
	p := webwalk.NewPlanner(pack, g)
	planners[pack] = p
	return p, nil
}

// NewWalker returns a Walker for one account's session.
func NewWalker(d Deps) (*webwalk.Walker, error) {
	if d.Session == nil {
		return nil, errors.New("webwalk/live: no session")
	}
	p, err := PlannerFor(d.Pack)
	if err != nil {
		return nil, err
	}
	c := NewClient(d)
	w := webwalk.NewWalker(p, c)
	w.Log, w.Run, w.State = d.Log, d.Run, c.WorldState
	return w, nil
}
