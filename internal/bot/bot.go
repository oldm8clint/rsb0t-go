// Package bot is the runtime layer that turns a pure plan package (barbarianplan,
// fisherplan, ...) plus the live primitives (a client.Session, a pathfind.Pack, a
// locindex.Index, a running randomevents.Guardian, ...) into an actual controllable, running
// bot: something a fleet or control panel can start, stop and observe, instead of a linear
// cmd/*test tool with a bare main().
//
// It is deliberately minimal — glue, not a framework. A Bot is anything with
// OnStart/Loop/OnStop; a Context carries everything a script needs, including a cancellable
// Delay/DelayUntil so a running bot can actually be stopped instead of blocking a raw
// time.Sleep past a stop request. A Runner drives one Bot against one Context and reports
// Status() for a dashboard to poll. Register/New is a small name -> factory registry so
// fleet/dashboard code can start a script by name without importing every script package.
package bot

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/randomevents"
)

// Bot is a runnable script.
//
//   - OnStart runs once before the first Loop call: positioning, local-engine-only cheat
//     setup, sanity checks.
//   - Loop is called repeatedly by a Runner until it returns a non-nil error or a stop is
//     requested between calls. What one call does is up to the script: BarbarianAgility (see
//     barbarian.go) runs exactly one obstacle attempt per call, so a stop takes effect within
//     one obstacle rather than a whole lap. A script with no natural small unit of work can
//     instead run its own internal loop inside a single Loop call, checking
//     ctx.Stopped()/ctx.Delay between its own iterations, and return nil whenever it wants to
//     yield back to the Runner (e.g. once per lap, or not until told to stop).
//   - OnStop runs exactly once after the Runner decides to stop, whether that was a requested
//     Stop() or an error from OnStart/Loop.
type Bot interface {
	OnStart(ctx *Context) error
	Loop(ctx *Context) error
	OnStop(ctx *Context)
}

// Context is everything a Bot needs from the outside world, plus the cancellation primitive
// that makes a running bot's blocking waits actually stoppable.
type Context struct {
	Session actions.Session
	// Username is this account's own login name. Most scripts never need it (they act
	// on the session, not on their own identity), but a script that announces itself to
	// something outside the session — MuleReceiver's own mulebus heartbeat is the first
	// one — needs a name to put on the wire, and nothing in actions.Session/gamestate.State
	// exposes it (Players() only ever returns *other* visible players, by design — see
	// its own doc comment). Set by internal/fleet from the same username it already logs
	// in with; empty for a caller that never set it (e.g. a bare unit test Context).
	Username string
	Pack     *pathfind.Pack
	Locs     *locindex.Index
	Config   *cachecfg.Config
	Guardian *randomevents.Guardian
	Chat     *chatlog.Log
	// Components is the parsed interface archive a bank-using script (e.g.
	// ProgressiveFisher, see internal/bot/fisher.go) needs to resolve the bank's
	// deposit-side component. Nil when the fleet wasn't given an httpBase to fetch it
	// from, or the fetch failed — a script that needs it should fail clearly (see
	// fisherrunner.BankFish) rather than opening a bank it can't actually read.
	Components archive.Components
	// Bus is the shared in-process wall-bus (see internal/mulebus) every account in the
	// same fleet process publishes to and polls — MuleReceiver's own replacement for
	// rsb0t-scripts' BroadcastChannel-based muleProtocol.ts transport (see
	// internal/bot/mule.go). Nil when a caller didn't wire one up (e.g. a standalone
	// single-account test with nothing else to talk to); a script that wants it should
	// treat that as "no bus available" rather than assuming it's always set, the same
	// way it already treats a nil Pack/Locs/Components.
	Bus *mulebus.Bus
	// Peers is the shared fleet bank directory (see internal/peerbank): who is holding what
	// and where they are standing, so an account can take a spare from the NEAREST account
	// that has one instead of every account walking to the single mule. Nil when a caller
	// did not wire one up, and every entry point in internal/bot/peershare.go treats that as
	// "peer sharing is off" and falls back to the mule — the same way this Context already
	// treats a nil Bus/Pack/Locs/Components.
	//
	// It is fed by accounts publishing their OWN bank at the moment they already have it
	// open, never by polling the fleet: internal/controlpanel's AllBanks answers the same
	// question for the panel but is unreachable from here (controlpanel -> fleet -> bot is
	// an import cycle), and pulling would cost the gamestate locks /api/status needs. See
	// internal/peerbank's package doc.
	Peers    *peerbank.Directory
	Settings map[string]interface{}
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
	// Loot receives what a loot source really yielded (see internal/loot): a casket or
	// oyster opened, an NPC pickpocketed or killed and its drops taken. Set by
	// internal/fleet, which tallies it per account for the control panel's loot panel; nil
	// (a bare test Context, a standalone tool) records nothing. Scripts pass it straight to
	// their runner package's own Deps.Loot.
	Loot func(loot.Drop)
	// TradeLog receives one entry per trade a MuleReceiver finished (internal/muleledger),
	// written only after the trade code reached its verdict; nil records nothing. Set by
	// internal/fleet. Never read back by any trade decision.
	TradeLog func(muleledger.Entry)
	// MuleOut answers "what has this mule handed out since then?" from the same trade record: the
	// items (lower-case names, units that crossed in accepted hand-outs, every line of a kit) and when
	// the oldest trade the record still holds for that mule was written (zero: it holds none, so
	// nothing can be said about any time). Set by internal/fleet. MasterCrafter's storage round reads
	// it to leave alone what the fleet is still being given; nil (a bare test Context, a standalone
	// tool) says nothing is known, which that round reads as "everything is in use".
	MuleOut func(mule string, since time.Time) (items map[string]int64, recordFrom time.Time)

	// --- Worlds. See worldhop.go in this package, internal/worlds and
	// internal/scripts/worldhopplan. All four are set by internal/fleet and all four are
	// nil/zero-safe: a script that never asks about worlds, and every standalone cmd/ tool,
	// behaves exactly as it did before these existed.
	//
	// CurrentWorld is the world this account is logged in to right now; HomeWorld the one
	// its roster entry gave it. Both 0 outside a fleet, which Context.World() and
	// Context.HomeWorldNumber() report as worlds.Default rather than 0. Named
	// CurrentWorld, not World, only because the reader below is the method World().
	CurrentWorld, HomeWorld int
	// WorldOf answers which world another account in this fleet is on, 0 when it is not in
	// this process. This is how a donor finds its MULE's world without hardcoding it. Nil
	// outside a fleet.
	WorldOf func(account string) int
	// HopWorld asks the fleet to move this account to another world: log out, log back in
	// there, and restart this script (the script does NOT survive — see worldhop.go). It
	// returns as soon as the hop is accepted. Nil outside a fleet, and a script must treat
	// nil as "hopping is not available here" rather than as a failure.
	HopWorld func(world int, reason string) error
	// SetHomeWorld makes world this account's home for the fleet's hop bookkeeping: where a
	// failed hop lands it and where HopHome sends it. It changes nothing about the session
	// already open. HopInProgress reports whether the fleet is in the middle of a hop for this
	// account right now (a hop outlives the script that asked for it). LoginBusy reports the
	// fleet's login path being busy, a restart ramp. All three nil outside a fleet (and on a
	// build of internal/fleet that does not set them); see the readers in worldhop.go.
	SetHomeWorld  func(world int) error
	HopInProgress func() bool
	LoginBusy     func() bool
	// SetHopBudget raises the fleet's per-account hop bound (default four hops in thirty minutes)
	// for a script that plans its own hops and keeps to its own, lower-rate budget (a rotating
	// RuneRunner). Nil outside a fleet.
	SetHopBudget func(maxHops int, window time.Duration) error

	stop     chan struct{}
	stopOnce sync.Once

	// subMu guards subs, the cancel funcs of subscriptions made through On (events.go), which
	// the Runner cancels when the bot finishes.
	subMu sync.Mutex
	subs  []func()

	// liveMu guards handoffRequested/muleAtOverride below — the control panel's live
	// hand-off trigger and threshold override (see RequestHandoff/SetMuleAt). Deliberately
	// separate from Settings, which stays immutable after NewContext by this package's own
	// long-standing convention (see Runner.Settings' doc comment) — these two fields are a
	// narrow, explicitly-mutable side channel for exactly the two knobs the account owner
	// asked to be live-editable from the panel (2026-09-14: "hand off now" plus the
	// muleAt threshold), not a general settings-mutation mechanism.
	liveMu           sync.Mutex
	handoffRequested bool
	muleAtOverride   *int
	// finishRequested and finishReason are set by RequestFinish: a script saying it is
	// done for good, not merely stopping. internal/fleet reads it through
	// Runner.FinishRequested to keep its reconnect loop from logging the account straight
	// back in and restarting the script. Guarded by liveMu with the other two live knobs.
	finishRequested bool
	finishReason    string

	// report is the script's own phase and published status, for the control panel's time
	// split and mule dashboard. See phase.go.
	report scriptReport

	// kills remembers this account's recent kills so a drop picked up afterwards is attributed
	// to the kill that left it (loot.KillLog; lootcombat.go). Fed by NoteKill.
	kills loot.KillLog

	// xp holds the experience waiting for the next Drop that earned it (lootxp.go); nil until
	// TrackExperience wraps Loot.
	xp *xpWatch
}

// NewContext builds a Context ready to hand to a Runner. settings and log may both be nil.
func NewContext(session actions.Session, username string, pack *pathfind.Pack, locs *locindex.Index, cfg *cachecfg.Config, components archive.Components, bus *mulebus.Bus, guardian *randomevents.Guardian, chat *chatlog.Log, settings map[string]interface{}, log func(string)) *Context {
	if settings == nil {
		settings = map[string]interface{}{}
	}
	if chat == nil {
		chat = chatlog.New()
	}
	return &Context{
		Session:    session,
		Username:   username,
		Pack:       pack,
		Locs:       locs,
		Config:     cfg,
		Guardian:   guardian,
		Chat:       chat,
		Components: components,
		Bus:        bus,
		Settings:   settings,
		Log:        log,
		stop:       make(chan struct{}),
	}
}

// requestStop closes the stop channel exactly once; safe to call more than once or
// concurrently, and safe to call from within a Bot itself (e.g. to end after N laps).
func (c *Context) requestStop() {
	c.stopOnce.Do(func() { close(c.stop) })
}

// Stopped reports whether a stop has been requested.
func (c *Context) Stopped() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// Delay sleeps for d, returning false immediately — without waiting out the rest of d — if a
// stop is requested while it's asleep. This is what makes a running bot's blocking waits
// (walking, waiting for xp, waiting out a random event) actually cancellable instead of
// running a whole obstacle/lap past a stop request. A zero or negative d just checks Stopped.
func (c *Context) Delay(d time.Duration) bool {
	if d <= 0 {
		return !c.Stopped()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.stop:
		return false
	case <-t.C:
		return true
	}
}

// DelayUntil polls cond (every 100ms) until it reports true, timeout elapses, or a stop is
// requested. Returns true only when cond became true before either of the others.
func (c *Context) DelayUntil(cond func() bool, timeout time.Duration) bool {
	const interval = 100 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		wait := interval
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		if !c.Delay(wait) {
			return false
		}
	}
}

func (c *Context) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(fmt.Sprintf(format, args...))
	}
}

// RequestHandoff asks a running donor script (ProgressiveFisher today; any future script
// built on internal/scripts/donorrunner) to start a mule hand-over at its own next
// hand-off check, regardless of whether its own muleAt threshold has been reached yet —
// the control panel's manual "hand off now" trigger users asked for
// (2026-09-14), so a fresh account doesn't have to wait out a high default threshold just
// to prove the donor/mule loop works end to end. A script that never checks
// TakeHandoffRequest simply never sees it; nothing else in this package acts on it.
func (c *Context) RequestHandoff() {
	c.liveMu.Lock()
	c.handoffRequested = true
	c.liveMu.Unlock()
}

// TakeHandoffRequest reports whether a manual hand-off was requested since the last call,
// clearing the request either way — a script should call this at most once per hand-off
// decision (fisher.go's Loop does, right where it evaluates its own threshold) so a request
// made while a hand-over is already in progress isn't silently lost, but also isn't acted
// on twice.
func (c *Context) TakeHandoffRequest() bool {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	v := c.handoffRequested
	c.handoffRequested = false
	return v
}

// SetMuleAt live-overrides a running donor script's own muleAt threshold — the control
// panel's other ask (2026-09-14): the banked-item count that starts a hand-over should be
// editable while the script runs, not just a startup setting. n <= 0 clears the override,
// falling back to whatever the script was started with.
func (c *Context) SetMuleAt(n int) {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	if n <= 0 {
		c.muleAtOverride = nil
		return
	}
	v := n
	c.muleAtOverride = &v
}

// MuleAtOverride returns the live muleAt override and whether SetMuleAt has set one.
func (c *Context) MuleAtOverride() (int, bool) {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	if c.muleAtOverride == nil {
		return 0, false
	}
	return *c.muleAtOverride, true
}

// RequestFinish declares that this script has finished its whole job and the account should
// stay logged out — not "stop this run", which is what Stop already means.
//
// The distinction matters because of what happens after an ordinary logout. When a session
// drops while a bot is running, internal/fleet's read loop sets resumeScript and its
// reconnect loop logs the account back in and starts the script again — correct for a
// dropped connection, and exactly wrong for a script that logged out on purpose because it
// reached its goal. A script that sends actions.Logout without calling this first gets
// pulled straight back into the game.
//
// Calling this does not itself log out or stop anything: a script should call it, then stop
// (returning from Loop after requestStop, or letting its own OnStop run). The reason is for
// the log only.
func (c *Context) RequestFinish(reason string) {
	c.liveMu.Lock()
	c.finishRequested = true
	c.finishReason = reason
	c.liveMu.Unlock()
}

// FinishRequested reports whether the script declared itself finished for good, and why.
// Unlike TakeHandoffRequest this does not clear the flag: it is a terminal state, and every
// later reader (the read loop's cleanup, the reconnect loop, a dashboard) must see the same
// answer.
func (c *Context) FinishRequested() (string, bool) {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	return c.finishReason, c.finishRequested
}

// IntSetting reads an int-shaped setting, tolerating the float64 a JSON-decoded
// map[string]interface{} produces for a bare number, falling back to def when absent or of
// an unexpected type.
func IntSetting(m map[string]interface{}, key string, def int) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return def
	}
}

// BoolSetting reads a bool setting, falling back to def when absent or of an unexpected type.
func BoolSetting(m map[string]interface{}, key string, def bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return def
}

// StringSetting reads a string setting, falling back to def when absent, empty, or of an
// unexpected type.
func StringSetting(m map[string]interface{}, key string, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

// FloatSetting reads a float-shaped setting, tolerating the int a Go caller might pass
// directly, falling back to def when absent or of an unexpected type.
func FloatSetting(m map[string]interface{}, key string, def float64) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return def
	}
}
