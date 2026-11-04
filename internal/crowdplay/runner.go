package crowdplay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/twitchplays"
)

// Account is the only account CrowdPlay may ever drive. The allow-list is this one name and
// is not configurable.
const Account = "crowdplay"

// Game is what the Runner needs from the fleet: the account's observed state and its
// existing session. cmd/controlpanel adapts its FleetSource; the lab test adapts its own
// session.
type Game interface {
	// Snapshot is the account's current observation.
	Snapshot(account string) (gamestate.RenderSnapshot, error)
	// Run runs fn on the account's existing session.
	Run(account string, fn func(actions.Session) error) error
	// BotRunning names the script running on the account, "" when none: the crowd never
	// fights a bot for the account.
	BotRunning(account string) string
	// Pack is the collision pack for routing a walk (nil: straight line).
	Pack() *pathfind.Pack
	// Names is the cache config (npc names and options); nil refuses !fish and !interact.
	Names() *cachecfg.Config
}

// Options are the Runner's limits. Zero values take the defaults.
type Options struct {
	PerUser    time.Duration // one command per viewer per PerUser (default 5s)
	Global     time.Duration // at most one accepted command per Global overall (default 1s)
	StepEvery  time.Duration // one queued command executed per StepEvery (default 1.2s: two game ticks)
	QueueLimit int           // pending commands (default 8); more are dropped
	Moderators map[string]bool
	Reply      func(text string) // short channel replies (refusals, what is being done)
	Log        func(string)
	Now        func() time.Time
}

// Runner is the crowd play decider: it takes chat messages, refuses what is not allowed,
// rate limits and queues the rest, and executes one command at a time on the account.
type Runner struct {
	account string
	game    Game
	opts    Options
	policy  *twitchplays.RatePolicy
	dedupe  *twitchplays.Dedupe

	mu      sync.Mutex
	queue   *twitchplays.Queue
	stats   Stats
	seenIDs []string
}

// Stats counts what the Runner did, for the log and the lab test.
type Stats struct {
	Messages, Commands, Forbidden, Invalid, RateLimited, Duplicate, Dropped, Executed, Failed, Busy int
}

// NewRunner refuses any account but Account.
func NewRunner(account string, game Game, opts Options) (*Runner, error) {
	if account != Account {
		return nil, fmt.Errorf("crowdplay: only the %q account may be driven by the crowd, not %q", Account, account)
	}
	if game == nil {
		return nil, errors.New("crowdplay: no game")
	}
	if opts.PerUser <= 0 {
		opts.PerUser = 5 * time.Second
	}
	if opts.Global <= 0 {
		opts.Global = time.Second
	}
	if opts.StepEvery <= 0 {
		opts.StepEvery = 1200 * time.Millisecond
	}
	if opts.QueueLimit <= 0 {
		opts.QueueLimit = 8
	}
	if opts.Reply == nil {
		opts.Reply = func(string) {}
	}
	if opts.Log == nil {
		opts.Log = func(string) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	mods := opts.Moderators
	return &Runner{account: account, game: game, opts: opts, dedupe: twitchplays.NewDedupe(), queue: twitchplays.NewQueue(opts.QueueLimit),
		policy: twitchplays.NewRatePolicy(opts.PerUser, opts.Global, func(a string) bool { return mods[a] })}, nil
}

// Stats returns a copy of the counters.
func (r *Runner) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

const dedupeWindow = 4096

// OnMessage handles one chat message: forbidden commands are refused with a reply, anything
// that is not a valid command is ignored, and a valid one is rate limited and queued.
func (r *Runner) OnMessage(m twitchplays.DiscordMessage) {
	ev := twitchplays.DiscordEvent(m)
	r.mu.Lock()
	r.stats.Messages++
	r.mu.Unlock()
	text := strings.TrimSpace(ev.Text)
	if !strings.HasPrefix(text, "!") {
		return
	}
	if !r.dedupe.First(ev.MessageID) {
		r.count(func(s *Stats) { s.Duplicate++ })
		return
	}
	r.mu.Lock()
	r.seenIDs = append(r.seenIDs, ev.MessageID)
	if len(r.seenIDs) > dedupeWindow { // bound the dedupe memory: forget the oldest half
		r.dedupe.Reset()
		r.seenIDs = append([]string(nil), r.seenIDs[len(r.seenIDs)/2:]...)
		for _, id := range r.seenIDs {
			r.dedupe.First(id)
		}
	}
	r.mu.Unlock()
	if why := twitchplays.Refusal(text); why != "" {
		r.count(func(s *Stats) { s.Forbidden++ })
		r.opts.Log(fmt.Sprintf("crowdplay: refused %q from %s: %s", firstWord(text), m.AuthorName, why))
		r.opts.Reply(fmt.Sprintf("%s: %s.", m.AuthorName, why))
		return
	}
	if ev.Received.IsZero() {
		ev.Received = r.opts.Now()
	}
	c, ok := twitchplays.Parse(text, "discord", ev.Author, ev.MessageID, ev.Received)
	if !ok {
		r.count(func(s *Stats) { s.Invalid++ })
		if why := twitchplays.ValidateArgs(c); why != nil && isKnown(c.Action) {
			r.opts.Reply(fmt.Sprintf("%s: %v.", m.AuthorName, why))
		}
		return
	}
	if !r.policy.Allow(c) {
		r.count(func(s *Stats) { s.RateLimited++ })
		return
	}
	r.mu.Lock()
	r.stats.Commands++
	if twitchplays.Normalize(c.Action) == "stop" {
		// Stop jumps the queue: everything pending is dropped.
		r.queue = twitchplays.NewQueue(r.opts.QueueLimit)
	}
	pushed := r.queue.Push(c)
	if !pushed {
		r.stats.Dropped++
	}
	r.mu.Unlock()
}

func isKnown(action string) bool {
	_, ok := twitchplays.AllowedActions[twitchplays.Normalize(action)]
	return ok
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}

func (r *Runner) count(f func(*Stats)) {
	r.mu.Lock()
	f(&r.stats)
	r.mu.Unlock()
}

// Loop executes one queued command every StepEvery until ctx ends.
func (r *Runner) Loop(ctx context.Context) {
	t := time.NewTicker(r.opts.StepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Step(ctx)
		}
	}
}

// Step executes at most one queued command.
func (r *Runner) Step(ctx context.Context) {
	r.mu.Lock()
	c, ok := r.queue.Pop()
	r.mu.Unlock()
	if !ok {
		return
	}
	if bot := r.game.BotRunning(r.account); bot != "" {
		r.count(func(s *Stats) { s.Busy++ })
		r.opts.Reply(fmt.Sprintf("The account is running %s right now; CrowdPlay waits until it is stopped.", bot))
		return
	}
	what, err := r.execute(ctx, c)
	if err != nil {
		r.count(func(s *Stats) { s.Failed++ })
		r.opts.Log(fmt.Sprintf("crowdplay: !%s from %s not done: %v", c.Action, c.Author, err))
		r.opts.Reply(fmt.Sprintf("!%s: %v.", c.Action, err))
		return
	}
	r.count(func(s *Stats) { s.Executed++ })
	if what != "" {
		r.opts.Log(fmt.Sprintf("crowdplay: %s (from %s)", what, c.Author))
		r.opts.Reply(what)
	}
}

// execute turns one validated command into at most one game action. The only actions it
// can ever send are a walk and an npc's first option: no item, bank, trade or shop option
// exists on this path.
func (r *Runner) execute(ctx context.Context, c twitchplays.Command) (string, error) {
	if r.account != Account { // defence in depth; NewRunner already refused
		return "", errors.New("wrong account")
	}
	switch twitchplays.Normalize(c.Action) {
	case "wait", "stop":
		return "", nil
	case "walk":
		dx, dz, steps, err := twitchplays.Walk(c)
		if err != nil {
			return "", err
		}
		snap, err := r.game.Snapshot(r.account)
		if err != nil {
			return "", err
		}
		from := pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: snap.Level}
		to := pathfind.NavPoint{X: snap.X + dx*steps, Z: snap.Z + dz*steps, Level: snap.Level}
		route := []pathfind.NavPoint{from, to}
		if p := r.game.Pack(); p != nil {
			if res := p.FindPath(from, to); res.OK && len(res.Waypoints) > 1 {
				route = res.Waypoints
			} else {
				return "", errors.New("there is no way through there")
			}
		}
		if err := r.game.Run(r.account, func(s actions.Session) error { return actions.Walk(s, route, false) }); err != nil {
			return "", err
		}
		return fmt.Sprintf("Walking %s %d.", c.Args[0], steps), nil
	case "fish":
		return r.useNpc(ctx, "fishing spot")
	case "interact":
		name, err := twitchplays.InteractTarget(c)
		if err != nil {
			return "", err
		}
		return r.useNpc(ctx, name)
	}
	return "", errors.New("unknown command")
}

// deniedOps are option labels the crowd never uses, even as an npc's first option.
var deniedOps = []string{"bank", "trade", "deposit", "collect", "give", "sell", "buy", "exchange", "attack"}

// interactRange is how far (Chebyshev tiles) an npc may be from the account.
const interactRange = 12

// approachTimeout bounds how long useNpc waits to arrive beside an npc before giving up. The
// crowd's next command waits behind it, so it is deliberately short: the account either gets
// there in a few ticks or the channel is told it could not.
const approachTimeout = 8 * time.Second

// approachPoll is how often the walk is checked against the npc's CURRENT tile. Fishing spots
// move on their own, so the arrival test cannot be against the tile the npc was on when the
// command was parsed.
const approachPoll = 300 * time.Millisecond

// useNpc walks the account beside the nearest npc with that exact name, then sends that npc's
// first option.
//
// The walk is not a nicety, it is what makes the option do anything. Measured on the lab engine
// 2026-09-20, with the account holding a small fishing net and the correct "Net" option going
// out to the correct level 1 spot (type 320, ops Net/Bait): from a tile 1 away it catches and
// the game says "You cast out your net... You catch some shrimps"; from 3 tiles away there is no
// catch and NO GAME MESSAGE AT ALL in 90 s. This engine runs with the client routefinder, so the
// server sets the interaction and waits for the client to walk -- and CrowdPlay never did, so
// every !fish and !interact beyond one tile was a silent no-op, while the channel was told the
// option had been run. It is the npc twin of the OpLocU reach gate in CLAUDE.md, and the same
// wedge internal/scripts/fisherrunner.orthogonalStand exists to avoid.
func (r *Runner) useNpc(ctx context.Context, name string) (string, error) {
	names := r.game.Names()
	if names == nil {
		return "", errors.New("npc names are not loaded on this panel")
	}
	snap, err := r.game.Snapshot(r.account)
	if err != nil {
		return "", err
	}
	// Every match within range, nearest first. The nearest is not always the one to use:
	// some Catherby fishing spots sit in a pocket with no route from where the account is
	// standing (measured -- the pack refuses a route to the one shore tile beside them), and
	// picking strictly the nearest would refuse the command while a workable spot stood two
	// tiles further on. ProgressiveFisher drops unroutable spots the same way
	// (internal/scripts/fisherrunner.Reachable).
	type match struct {
		npc *gamestate.NPC
		typ cachecfg.Npc
		d   int
	}
	var found []match
	for i := range snap.NPCs {
		n := &snap.NPCs[i]
		t, ok := names.Npc(n.Type)
		if !ok || !strings.EqualFold(t.Name, name) {
			continue
		}
		dx, dz := n.X-snap.X, n.Z-snap.Z
		if d := max(dx, -dx, dz, -dz); d <= interactRange {
			found = append(found, match{npc: n, typ: t, d: d})
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no %s within %d tiles", name, interactRange)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].d < found[j].d })

	// The refusals are judged on the NEAREST match and refuse outright. They are deliberately
	// not part of the fallback: skipping past a denied npc to a further one would let the crowd
	// reach something the model just refused by standing next to it.
	bestType := found[0].typ
	op := bestType.Ops[0]
	if op == "" {
		return "", fmt.Errorf("%s has nothing to do", bestType.Name)
	}
	if twitchplays.DeniedTarget(bestType.Name) {
		return "", fmt.Errorf("%s is not allowed in CrowdPlay", bestType.Name)
	}
	for _, d := range deniedOps {
		if strings.Contains(strings.ToLower(op), d) {
			return "", fmt.Errorf("%q is not allowed in CrowdPlay", op)
		}
	}

	from := pathfind.NavPoint{X: snap.X, Z: snap.Z, Level: snap.Level}
	var chosen *match
	var route []pathfind.NavPoint
	for i := range found {
		m := &found[i]
		if m.typ.Ops[0] != op { // only fall back to a spot offering the same thing
			continue
		}
		if m.d <= 1 {
			chosen, route = m, nil
			break
		}
		if rt, ok := approachRoute(r.game.Pack(), from, *m.npc); ok {
			chosen, route = m, rt
			break
		}
	}
	if chosen == nil {
		return "", fmt.Errorf("there is no way to get to the %s", bestType.Name)
	}
	bestType = chosen.typ
	idx := chosen.npc.Index
	if route != nil {
		if err := r.approach(ctx, idx, bestType.Name, route); err != nil {
			return "", err
		}
	}
	if err := r.game.Run(r.account, func(s actions.Session) error { return actions.OpNpc(s, 1, idx) }); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s.", op, bestType.Name), nil
}

// approach walks the given route and waits until the account is beside npc index idx. It
// re-reads the npc every poll, because the npc may be moving.
func (r *Runner) approach(ctx context.Context, idx int, what string, route []pathfind.NavPoint) error {
	if err := r.game.Run(r.account, func(s actions.Session) error { return actions.Walk(s, route, false) }); err != nil {
		return err
	}
	deadline := r.opts.Now().Add(approachTimeout)
	wasX, wasZ := -1, -1
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(approachPoll):
		}
		cur, err := r.game.Snapshot(r.account)
		if err != nil {
			return err
		}
		at, ok := npcByIndex(cur, idx)
		if !ok {
			return fmt.Errorf("the %s moved away", what)
		}
		dx, dz := at.X-cur.X, at.Z-cur.Z
		beside := cur.Level == at.Level && max(dx, -dx, dz, -dz) <= 1
		// Beside it AND no longer moving. Arriving is not enough: the route's last steps can
		// carry the account past the npc, and an OPNPC sent mid-step is judged against a tile
		// the account is about to leave. The FAILURE is measured — a lab run whose approach
		// succeeded still got no "You cast out your net..." at all — but this settle check is
		// the only part of useNpc that has NOT been exercised against a live engine: the
		// machine was wound down before it could be. It compiles, vets and unit-tests clean.
		// If it ever looks like the cause of a missed command, that is where to look first,
		// and reverting this one condition leaves the walk-before-OPNPC fix (which IS proven,
		// by the before/after at 3 tiles) standing on its own.
		settled := cur.X == wasX && cur.Z == wasZ
		wasX, wasZ = cur.X, cur.Z
		if beside && settled {
			return nil
		}
		if !r.opts.Now().Before(deadline) {
			return fmt.Errorf("could not get beside the %s", what)
		}
	}
}

func npcByIndex(snap gamestate.RenderSnapshot, idx int) (gamestate.NPC, bool) {
	for i := range snap.NPCs {
		if snap.NPCs[i].Index == idx {
			return snap.NPCs[i], true
		}
	}
	return gamestate.NPC{}, false
}

// approachRoute is a route onto a tile beside the npc.
//
// It asks the pack to path to the npc's OWN tile and lets Pack.FindPath resolve the goal. A
// fishing spot stands on water, so its tile is never walkable, and FindPath's cardinalGoals
// mirrors the client's PathFinder.ts for precisely that case: the walkable tiles immediately
// beside an unwalkable destination, wall masks respected. Hand-listing the eight neighbours
// and pathing to each was worse -- a walkable neighbour is pathed to with slack 0 and simply
// fails when it is fenced off, which is how an ordinary Catherby beach tile reported "there is
// no way to get to the Fishing spot".
func approachRoute(pack *pathfind.Pack, from pathfind.NavPoint, npc gamestate.NPC) ([]pathfind.NavPoint, bool) {
	to := pathfind.NavPoint{X: npc.X, Z: npc.Z, Level: from.Level}
	if pack == nil {
		// No collision data: walk straight at the side nearest the account and let the server
		// judge it, the same fallback !walk already takes.
		best, found := pathfind.NavPoint{}, false
		for _, o := range [][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
			t := pathfind.NavPoint{X: npc.X + o[0], Z: npc.Z + o[1], Level: from.Level}
			if !found || chebyshev(from, t) < chebyshev(from, best) {
				best, found = t, true
			}
		}
		return []pathfind.NavPoint{from, best}, found
	}
	res := pack.FindPath(from, to)
	if !res.OK || len(res.Waypoints) < 2 {
		return nil, false
	}
	return res.Waypoints, true
}

func chebyshev(a, b pathfind.NavPoint) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	return max(dx, -dx, dz, -dz)
}
