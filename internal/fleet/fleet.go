// Package fleet manages many concurrent internal/client.Sessions from one process and
// reports their live state as JSON — the data source behind the local dashboard. It also
// owns starting and stopping a registered internal/bot script per account (StartBot/
// StopBot), the control surface cmd/fleet's dashboard exposes over HTTP.
package fleet

import (
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/login"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/worlds"
)

// Config is shared by every account the fleet connects.
type Config struct {
	WSURL    string
	HTTPBase string
	Modulus  *big.Int
	Exponent *big.Int
	// ModulusSource, when set, re-reads the server's CURRENT RSA login modulus (in
	// production, FetchProductionModulus). The live server rotates its keypair on every server
	// restart, and Modulus above is scraped once at startup, so without this a restart
	// leaves every account signing its login block with a key the server has thrown
	// away — rejected with response 6, which retryableLoginError quite reasonably
	// treats as terminal. See modulus.go for how a rotation is confirmed before any
	// response 6 is forgiven. Nil (a local engine, every cmd/ tool, every test) keeps
	// the previous behaviour exactly: the key never changes and response 6 stays final.
	ModulusSource func() (*big.Int, error)
	// Checksums and NoAntiCheat pass straight to client.Config: given checksums skip the /crc0
	// fetch at each login, and NoAntiCheat stops the ANTICHEAT side-packets. Zero values keep
	// the old behaviour.
	Checksums   *[9]int32
	NoAntiCheat bool
	// DeviceIDs is the login device id mode (login.DeviceIDFixed or login.DeviceIDPerAccount);
	// empty is fixed, the web client's behaviour.
	DeviceIDs string
	// BrowserHeaders sends browser-like upgrade headers (client.BrowserHeader, stable per
	// account) on every game WebSocket; false sends the library's defaults.
	BrowserHeaders bool

	// Pack/Locs/Cache are the shared, read-only primitives a registered internal/bot
	// script needs (mirroring cmd/barbtest's own -pack/-locs/-config flags) — loaded once
	// by cmd/fleet and handed to every account's StartBot call, rather than per account.
	// Nil when not configured; StartBot fails clearly rather than panicking if a started
	// script needs one that's missing.
	Pack  *pathfind.Pack
	Locs  *locindex.Index
	Cache *cachecfg.Config

	// Components is the parsed interface archive a bank-using script (e.g.
	// ProgressiveFisher) needs — see archive.FetchComponents, fetched once by
	// cmd/fleet from HTTPBase the same way Pack/Locs/Cache are loaded once from disk.
	// Nil when not configured; a script that needs it and doesn't have it fails
	// clearly at StartBot/OnStart time rather than opening a bank it can't read.
	Components archive.Components

	// Bus is the in-process wall-bus replacement (see internal/mulebus's own package
	// doc) every account in this fleet process shares — the same role one
	// BroadcastChannel played across every bot tab in rsb0t-scripts' Electron
	// renderer. Unlike Pack/Locs/Components, this needs no file path or HTTP fetch to
	// exist, so New fills in a fresh mulebus.NewBus() when left nil rather than making
	// every caller remember to set it — there is no meaningful "unconfigured" state
	// for an in-memory pub-sub the way there is for a collision pack that must be
	// baked from a file first.
	Bus *mulebus.Bus

	// Peers is the shared fleet bank directory (internal/peerbank) every account in this
	// process publishes its bank into, so an account short of an item can source it from a
	// nearer peer instead of queuing behind the one mule.
	//
	// Filled in by New when left nil, for exactly the reason Bus is: it is an in-memory map
	// with no file to bake and no endpoint to fetch, so there is no meaningful
	// "unconfigured" state to distinguish. A fleet that simply never calls PublishBank ends
	// up with an empty directory, which every peer lookup already treats as "no peer".
	Peers *peerbank.Directory

	// BankStateFile is where each account's last-known bank contents are persisted
	// (see bankstore.go), so the dashboard's bank panel still shows them after an
	// account disconnects, its bot stops, or the whole process restarts. Empty keeps
	// the cache in memory only (still survives a disconnect/stop while the process
	// keeps running, just not a restart) — the same "empty disables the file, feature
	// still works in a lesser form" convention internal/controlpanel.Config.LayoutFile
	// already uses for the panel layout.
	BankStateFile string

	// Routes, when set, is the proxy pool (routeconfig.go): accounts are admitted to the home
	// address first and then to each proxy, never past a route's cap, with login pacing per
	// route. Nil — the default — keeps the single direct route with no cap and the one shared
	// login gate, exactly as before.
	Routes *RoutePool

	// Worlds, when set, maps a world number to the endpoints an account on that world logs
	// in to (see internal/worlds, and worldresolve.go in this package). Nil — the default —
	// derives the map from WSURL/HTTPBase above, which for a single-world fleet resolves
	// every account to exactly the bytes it always used.
	//
	// A world is purely a different wsURL: the login block carries no world number, and
	// w2.example.invalid's /crc0 checksums and RSA modulus were measured byte-identical to w1's
	// on 2026-09-22, so nothing else in the login path changes per world.
	Worlds *worlds.Map
}

// account is one managed connection and the bookkeeping the dashboard needs beyond
// what *client.Session already tracks.
type account struct {
	mu            sync.RWMutex
	username      string
	password      string // kept only to relogin after a drop; never put in Snapshot/JSON.
	connecting    bool   // reserves an actual login, including a wait at the global gate.
	session       *client.Session
	connectedAt   time.Time
	packets       int
	lastErr       string
	disconnected  bool
	disconnectMsg string

	// reconnecting/reconnectAttempt track an in-progress relogin after an unexpected
	// drop (see Manager.reconnectLoop). Both are zero/false once reconnected, and stay
	// zero/false forever for an account that was deliberately closed via CloseAll.
	reconnecting     bool
	retryWake        chan struct{}
	reconnectAttempt int

	// ceilingAttempt counts consecutive login attempts turned away by the address's
	// concurrent CONNECTION ceiling (response 9), so retryAccount can probe a full
	// ceiling on its own much slower schedule instead of burning the fleet's shared
	// login budget on attempts that cannot be admitted. Reset by any other outcome,
	// success included. See loginceiling.go's own doc comment.
	ceilingAttempt int

	// routeWait is true while the account waits for a free route slot (routes.go), so the
	// dashboard can show it as queued rather than connecting. Always false with no pool.
	routeWait bool

	// loginKeyGen is the RSA login-key generation this account's most recent attempt
	// signed with, so a rejection can be told apart from one made with a key that has
	// since been replaced. See modulus.go's staleLoginKey.
	loginKeyGen int

	// deliberateDisconnect is set by Manager.Disconnect and cleared by Manager.
	// ConnectExisting (and on a fresh Connect/AddAccount, implicitly, since a new
	// account struct starts with it false). Its whole purpose is keeping
	// reconnectLoop — which only ever exists to recover from an *unexpected* drop —
	// from relogging an account back in the instant the operator told it to log out.
	// startReadLoop checks it before ever starting reconnectLoop; reconnectLoop itself
	// rechecks it before each wait/login attempt and once more right after a login
	// that raced a Disconnect call succeeds anyway, so that race is handled by logging
	// the just-opened session back out rather than by leaving it connected. See
	// Manager.Disconnect's doc comment for the full reasoning.
	deliberateDisconnect bool

	// chat captures MESSAGE_GAME text off this account's own read loop (below), so a
	// started bot can scan recent chat lines (e.g. barbarianrunner's fall/wrong-side/
	// can't-reach regexes) without owning a second read of the same connection.
	chat *chatlog.Log

	// activity captures every Manager.alogf line for this account specifically — bot
	// start/stop, guardian activity, and whichever script is running's own progress
	// lines (e.g. barbarianrunner's obstacle-by-obstacle ctx.logf calls) — the same
	// lines that otherwise only ever reached cmd/fleet's own stdout. Surfaced to the
	// dashboard via AccountStatus.RecentLog. Reuses chatlog.Log rather than a
	// second bespoke ring buffer — see that package's doc comment.
	activity *chatlog.Log

	// randomEvents is a bounded, newest-first history of this account's own
	// randomevents.Guardian outcomes — what fired and whether Handle actually cleared
	// it. Populated by parsing Guardian.kick's own literal log lines (see
	// parseRandomEvent and the guardian.Log wiring in StartBot) rather than adding a
	// second instrumentation path into internal/randomevents. Protected by mu, same as
	// every other simple field on this struct. Surfaced to the dashboard as
	// AccountStatus.RandomEvents — a separate, compact panel from the general
	// scrolling activity log, per the account owner's explicit request to see "what
	// random events fired and whether they were handled" without scrolling/searching
	// the general log for it.
	randomEvents []RandomEventOutcome

	// bankUpdatedAt is when this account's read loop last saw an UPDATE_INV_FULL/
	// UPDATE_INV_PARTIAL for actions.BankWithdraw — i.e. the last time this account's
	// bank was genuinely open and reporting contents, not just the last poll. Zero
	// until the bank has been opened at least once this session. gamestate.State.
	// KnownInventories() itself keeps no such timestamp (a component's last-known
	// contents persist forever, with no record of when they arrived), and the bank's
	// own contents never disappear from that map once the modal closes, which is
	// exactly why the dashboard needs to say "as of <time>" rather than implying this
	// is live — see AccountStatus.BankUpdatedAt's own doc comment.
	bankUpdatedAt time.Time

	script       string
	guardian     *randomevents.Guardian
	runner       *bot.Runner
	startBotMu   sync.Mutex
	resumeScript bool
	// stopBy is who last asked this account's current bot to stop through the fleet (stopsource.go),
	// reset by every StartBot. The watchdog's safety net leaves a bot an operator stopped alone and
	// starts any other left-stopped bot again (watchdog_orphan.go). Guarded by mu.
	stopBy stopSource

	// guardianAct is what this account's Guardian is handling right now, read off its own
	// log lines, for the control panel's time split (see phases.go).
	guardianAct GuardianActivity

	// hopState is this account's world and its world-hop bookkeeping, kept in its own
	// struct in worldhop.go so multi-world support costs this file one line rather than
	// five loose fields. Guarded by mu like everything else here.
	hopState

	// lastLine is when alogf last wrote a line for this account (unix nanoseconds), read by the
	// stall watchdog (watchdog.go) as "the account is still saying something". Atomic, so the
	// hot logging path takes no lock for it. The watchdog's own lines do not stamp it.
	lastLine atomic.Int64
}

// maxRandomEvents bounds account.randomEvents — a compact recent-history panel, not a
// full audit log.
const maxRandomEvents = 20

// recordRandomEvent appends one outcome to a's history, newest first, trimming past
// maxRandomEvents.
func (a *account) recordRandomEvent(name, outcome string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.randomEvents = append([]RandomEventOutcome{{Name: name, Outcome: outcome, At: time.Now().Format(time.RFC3339)}}, a.randomEvents...)
	if len(a.randomEvents) > maxRandomEvents {
		a.randomEvents = a.randomEvents[:maxRandomEvents]
	}
}

// randomEventPrefix and the outcome strings below match internal/randomevents.
// Guardian.kick's own literal log lines exactly (see guardian.go) — this is a light
// parse of an existing log line, not a second instrumentation path, since kick()
// already distinguishes every outcome this dashboard panel needs.
const randomEventPrefix = "random event: "

// parseRandomEvent extracts the event name and a short outcome code from one
// Guardian.kick log line (e.g. "random event: Pillory — cleared"). ok is false for any
// line that isn't a Guardian.kick line (e.g. an unrelated bot/script log line sharing
// the account's activity log). "handling" (fired, outcome not yet known) is parsed but
// deliberately not recorded by the caller — see the StartBot wiring — so the dashboard
// panel only ever shows finished outcomes.
//
// The outcomes are deliberately narrower than "did it clear", because counting anything
// that is not "cleared" as a failure is what made the dashboard report 17,802 failed
// random events against 6,963 cleared (2026-09-20). Two separate effects:
//
//   - "still present after handling" is a RETRY, not a terminal verdict. The guardian
//     releases the account and tries the same event instance again, so one stubborn event
//     logs it many times — measured live in a single run: one account's mime logged it 13
//     times, and Strange plants 6-8 times each, against 93 cleared across the whole fleet.
//     Counted as failures those retries swamp everything, and the same event still clears
//     on a later attempt.
//   - "rejected for this account" means the event belongs to somebody else — an ownership
//     check the guardian does deliberately (speech_owner.go). That is a correct decision,
//     not a failure, and it was previously dropped on the floor by the default case.
//
// So a caller can now tell apart: cleared, notOurs (nothing to do, correctly skipped),
// error and gaveUp (the two genuine failures), and retry (an attempt, counted separately).
func parseRandomEvent(msg string) (name, outcome string, ok bool) {
	if !strings.HasPrefix(msg, randomEventPrefix) {
		return "", "", false
	}
	rest := msg[len(randomEventPrefix):]
	const sep = " — "
	i := strings.Index(rest, sep)
	if i < 0 {
		return "", "", false
	}
	name = rest[:i]
	// The escalation give-up line is shaped differently from the rest: the event name is
	// inside the part BEFORE the separator ("gave up on Mime after 3 attempts — ignoring
	// it for 5m0s"), so it needs its own extraction rather than the tail switch below.
	// It was previously dropped by the default case, which is why a run of escalating
	// give-ups on one event left no trace in the panel at all.
	if strings.HasPrefix(name, giveUpPrefix) {
		if j := strings.Index(name, " after "); j > len(giveUpPrefix) {
			return name[len(giveUpPrefix):j], "gave_up", true
		}
		return "", "", false
	}
	switch tail := rest[i+len(sep):]; {
	case tail == "cleared":
		return name, "cleared", true
	case tail == "still present after handling":
		return name, "retry", true
	case tail == rejectedSuffix:
		return name, "not_ours", true
	case strings.HasPrefix(tail, "handler error: "):
		return name, "error", true
	case tail == "handling":
		return name, "handling", true
	default:
		return "", "", false
	}
}

// giveUpPrefix and rejectedSuffix match Guardian.kick's own literal lines (guardian.go).
const (
	giveUpPrefix   = "gave up on "
	rejectedSuffix = "rejected for this account; checking other events next"
)

// Manager owns a set of accounts and their connections.
type Manager struct {
	cfg Config

	mu        sync.RWMutex
	accounts  []*account
	stop      chan struct{}
	closeOnce sync.Once

	// bank is the cross-account bank-contents cache (see bankstore.go) — always
	// present (memory-only when cfg.BankStateFile is empty), consulted/updated by
	// Snapshot so a disconnected or stopped account still reports its last-known bank.
	bank *bankStore

	// Log receives one already-formatted line per notable fleet/bot event (guardian
	// activity, bot start/stop); nil drops them. Set by cmd/fleet before connecting
	// anyone if it wants them printed.
	Log func(string)

	// loginRate caps the TOTAL rate this Manager ever attempts real logins at, across
	// every account and every retry — see loginRateGate's own doc comment for the live
	// incident (a 96-account thundering herd of independent reconnectLoops) that made a
	// per-account-only backoff insufficient.
	loginRate *loginRateGate
	// routes is the proxy pool's admission state (routes.go); nil with no pool configured.
	routes       *routePool
	connectLogin func(client.Config) (*client.Session, error)
	retryWait    func(<-chan struct{}, time.Duration) bool

	// loot is every account's loot tally (loot.go); its zero value is ready to use.
	loot lootBook
	// deathLoot is the record of what deaths dropped and who took it back (deathloot.go).
	deathLoot deathLootBook

	// The server's current RSA login key and how many rotations have been observed, plus
	// when the key was last re-read. Guarded by modMu; see modulus.go.
	// worldFields is the per-world endpoint map (worldresolve.go), built lazily so a
	// Manager whose Config never mentions worlds behaves exactly as before.
	worldFields
	// worldGateFields is optional per-world login pacing (loginrateworld.go). OFF by
	// default, in which case every login waits on exactly the gate it always did.
	worldGateFields
	// failoverFields is the fleet-wide record of which worlds answered "full"
	// (worldfailover.go), so one account's response 7 saves the other three hundred from
	// rediscovering it.
	failoverFields
	// peerFields is the fleet's one shared peerbank.Directory (peers.go), so an account can
	// read another's bank — the mule's above all — instead of asking blind.
	peerFields
	// worldSpaceFields records which accounts NEED a particular world and who has recently
	// been asked to leave one (worldspace.go), so an account that cannot be served by a
	// different world gets a slot freed for it instead of being moved away from the one it
	// needs. Nothing is pinned by default, so this is inert until something pins.
	worldSpaceFields
	// watchdogFields is the stall watchdog (watchdog.go), started by StartWatchdog; nil until then.
	watchdogFields

	modMu          sync.Mutex
	modulus        *big.Int
	modGen         int
	modCheckedAt   time.Time
	modLastVerdict bool
}

func New(cfg Config) *Manager {
	if cfg.Bus == nil {
		cfg.Bus = mulebus.NewBus()
	}
	if cfg.Peers == nil {
		cfg.Peers = peerbank.New()
	}
	m := &Manager{cfg: cfg, stop: make(chan struct{}), bank: newBankStore(cfg.BankStateFile), loginRate: newLoginRateGate(), connectLogin: client.Connect, retryWait: waitLoginRetry}
	if cfg.Routes != nil {
		m.routes = newRoutePool(cfg.Routes, m.loginRate, cfg.WSURL, func(msg string) { m.logf("%s", msg) })
	}
	return m
}

// Routes returns each route's counters (routes.go), nil when no proxy pool is configured.
func (m *Manager) Routes() []RouteStatus {
	if m.routes == nil {
		return nil
	}
	return m.routes.statuses()
}

// RouteDescriptions returns one startup line per route (no credentials), nil with no pool.
func (m *Manager) RouteDescriptions() []string {
	if m.routes == nil {
		return nil
	}
	var out []string
	for _, r := range m.routes.routes {
		out = append(out, r.describe())
	}
	return out
}

// routeSessionEnded tells the route pool a's session is over (lived is how long it ran) and,
// when the account is leaving for good, gives its slot back. No-op with no pool.
func (m *Manager) routeSessionEnded(a *account, lived time.Duration, leaving bool) {
	if m.routes == nil {
		return
	}
	m.routes.sessionDown(a, lived, !leaving)
	if leaving {
		m.routes.release(a)
	}
}

// Cache returns the config archive this Manager was built with (nil if none was
// configured) — an accessor for callers built around a Manager (e.g.
// internal/controlpanel's FleetSource) that need to resolve an item id to a name for
// their own display, rather than duplicating the load.
func (m *Manager) Cache() *cachecfg.Config {
	return m.cfg.Cache
}

func (m *Manager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(fmt.Sprintf(format, args...))
	}
}

// alogf is logf plus recording the same formatted line onto a's own activity ring
// buffer (see the account.activity field doc), so it's readable from the dashboard via
// AccountStatus.RecentLog and not just from cmd/fleet's own stdout. Every logf call site
// in this file has a specific account in scope (fleet-wide events don't currently
// exist), so this fully replaces logf below rather than living alongside it — a nil a is
// tolerated (falls back to logf's own behavior) so callers never need a nil check.
func (m *Manager) alogf(a *account, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if a != nil {
		a.lastLine.Store(time.Now().UnixNano()) // the stall watchdog's "still saying something"
	}
	if a != nil && a.activity != nil {
		a.activity.Record(msg)
	}
	if m.Log != nil {
		m.Log(msg)
	}
}

// Connect logs one account in and starts its read loop. Errors are recorded on the
// account entry (visible to the dashboard) rather than returned, so a ramp-up loop can
// keep going past one bad account — matching cmd/prodmulti's behavior but per-account
// instead of stopping the whole batch. This is the fixed-roster path cmd/fleet/cmd/
// controlpanel's own startup loop calls once per configured account; for adding an
// account at runtime that was not in that roster, use AddAccount instead, which returns
// an error the caller can actually observe rather than only recording it silently.
func (m *Manager) Connect(username, password string) {
	m.connectAccount(username, password, 0)
}

// ConnectWorld is Connect for a roster entry that names its own world. world 0 means "the
// fleet's own", which is exactly what Connect passes, so the two share one implementation
// and a single-world roster is unaffected by this existing. An out-of-range world is
// recorded on the account as an error rather than silently connecting it to the fleet
// default — an operator who wrote a world believes the account went there.
func (m *Manager) ConnectWorld(username, password string, world int) {
	m.connectAccount(username, password, world)
}

// connectAccount is the shared implementation behind Connect and AddAccount: builds a new
// account entry, appends it to m.accounts, performs the initial login, and starts its read
// loop on success. The account is appended even when login fails (matching Connect's
// original behavior) so a bad account still shows up in Snapshot with its error visible
// rather than vanishing without a trace.
func (m *Manager) connectAccount(username, password string, world int) (*account, error) {
	a := &account{username: username, password: password, chat: chatlog.New(), activity: chatlog.New()}
	resolved, worldErr := m.worldMap().Normalise(world)
	if worldErr == nil {
		a.world = resolved
		a.hop.Home, a.hop.Current = resolved, resolved
	}
	m.mu.Lock()
	m.accounts = append(m.accounts, a)
	m.mu.Unlock()

	if worldErr != nil {
		a.mu.Lock()
		a.lastErr = worldErr.Error()
		a.mu.Unlock()
		return a, fmt.Errorf("fleet: %s: %w", username, worldErr)
	}

	if err := m.loginOnce(a); err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		a.mu.Unlock()
		// A timeout during the first login needs the same paced recovery as a
		// later drop. Permanent credentials/configuration failures remain visible.
		if isWorldFullRejection(err) {
			// The roster put this account on a world with no room. Move it before the
			// retry loop starts, so a fleet ramp does not spend its whole login budget
			// discovering the same thing once per account (worldfailover.go).
			m.failoverToOpenWorld(a)
		}
		if retryableLoginError(err) || m.rotatedKeyRetry(a, err) {
			m.alogf(a, "[%s] temporary first login failure — retrying in the background: %v", username, err)
			m.startReconnect(a)
		}
		return a, err
	}
	m.startReadLoop(a)
	return a, nil
}

// noteLoginOutcome records whether this login attempt was turned away by the address's
// concurrent CONNECTION ceiling (response 9) rather than by anything a wait can fix, so
// retryAccount can pick the right backoff schedule for the next probe. Any other outcome,
// success included, ends the ceiling streak: the ceiling is a moving server-side number,
// and one attempt getting through means the address is no longer full.
//
// See loginceiling.go's own doc comment for the evidence behind treating response 9
// differently from response 16, and for what the old shared-gate penalty on 9 actually
// cost the live fleet.
func noteLoginOutcome(a *account, err error) {
	a.mu.Lock()
	if err != nil && isConnectionCeilingRejection(err) {
		a.ceilingAttempt++
	} else {
		a.ceilingAttempt = 0
	}
	a.mu.Unlock()
}

// loginOnce performs one login attempt for a (using its stored username/password) and, on
// success, sets a.session/a.connectedAt and clears any prior disconnect bookkeeping. It does
// not start the read loop — callers do that separately so a reconnect attempt and the
// initial Connect share exactly this one login path without duplicating it.
//
// Waits on m.loginRate first — every caller (a fresh Connect, a reconnectLoop retry, an
// operator's ConnectExisting) funnels through here, so this one gate caps the TOTAL login
// attempt rate regardless of which path triggered it. See loginRateGate's own doc comment
// for why per-account backoff alone isn't enough once many accounts are retrying at once.
// A gate wait that ends because m.stop fired returns immediately with a clear error rather
// than attempting a login the fleet is shutting down anyway.
func (m *Manager) loginOnce(a *account) error {
	a.mu.Lock()
	if a.connecting || a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("fleet: %s already has a login or connection in progress", a.username)
	}
	a.connecting = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.connecting = false; a.mu.Unlock() }()

	// With a proxy pool, first take a slot on a route (routes.go) — possibly waiting in the
	// queue for one — and pace this login on that route's own gate, since the server's rate
	// and connection verdicts are per address. With no pool, r stays nil and this is the
	// single shared gate as before.
	gate := m.loginRate
	routeKey := "direct"
	var r *route
	attempted := false
	if m.routes != nil {
		var ok bool
		r, ok = m.routes.acquire(m.stop, a, func() bool { return m.deliberatelyDisconnected(a) }, func(w bool) {
			a.mu.Lock()
			a.routeWait = w
			a.mu.Unlock()
		})
		if !ok {
			if m.deliberatelyDisconnected(a) {
				return fmt.Errorf("fleet: login canceled by disconnect request")
			}
			return fmt.Errorf("fleet: shutting down")
		}
		gate, routeKey = r.gate, r.id
		defer func() {
			if !attempted {
				m.routes.abandon(r)
			}
		}()
	}
	// Optionally one gate per (route, world) rather than per route, so worlds fill side by
	// side. OFF by default and a no-op then: worldGateFor returns `gate` unchanged. See
	// loginrateworld.go for the evidence, and for why it must not be turned on before the
	// experiment.
	gate = m.worldGateFor(routeKey, m.worldOf(a), gate)
	if !gate.Wait(m.stop) {
		return fmt.Errorf("fleet: shutting down")
	}
	select {
	case <-m.stop:
		return fmt.Errorf("fleet: shutting down")
	default:
	}
	if m.deliberatelyDisconnected(a) {
		return fmt.Errorf("fleet: login canceled by disconnect request")
	}

	a.mu.RLock()
	username, password := a.username, a.password
	a.mu.RUnlock()

	// Take the key the fleet currently believes in, not the one it started with, and
	// remember which generation it was: a rejection is only forgiven as a key rotation if
	// the key has changed since this attempt. See modulus.go.
	modulus, keyGen := m.loginKey()
	a.mu.Lock()
	a.loginKeyGen = keyGen
	a.mu.Unlock()

	// Resolved from the ACCOUNT, not from the fleet: this is the one site that builds a
	// session, so the initial connect, an operator's ConnectExisting, reconnectLoop's
	// automatic retry after a drop and a world hop all dial the account's OWN world. An
	// account that names no world resolves to the fleet's own endpoints unchanged. See
	// worldresolve.go.
	accountWS, accountHTTP := m.endpointsFor(a)

	cfg := client.Config{
		WSURL:    accountWS,
		HTTPBase: accountHTTP,
		Username: username,
		Password: password,
		Modulus:  modulus,
		Exponent: m.cfg.Exponent,

		Checksums:   m.cfg.Checksums,
		NoAntiCheat: m.cfg.NoAntiCheat,
		DeviceID:    login.DeviceIDFor(m.cfg.DeviceIDs, username),
		// The client's own lines (a recovered decoder panic above all) reach the fleet log.
		Log: func(msg string) { m.alogf(a, "%s", msg) },
	}
	if m.cfg.BrowserHeaders {
		cfg.Header = client.BrowserHeader(username, accountHTTP)
	}
	if r != nil {
		cfg.DialTimeout = r.connectTimeout
		if link := r.currentLink(); link != nil {
			cfg.NetDialContext = link.dialer.DialContext
			cfg.HTTPClient = link.httpClient
			cfg.RouteKey = r.id
		}
	}
	started := time.Now()
	s, err := m.connectLogin(cfg)
	if r != nil {
		attempted = true
		m.routes.finish(a, r, err, time.Since(started))
	}
	if err != nil {
		// Only a genuine RATE rejection earns the quiet window — fleet-wide with no pool,
		// that route's alone with one (routes.finish penalized it). A connection ceiling
		// rejection gets a long per-account backoff instead (see loginceiling.go): pausing
		// the whole fleet cannot free somebody else's slot, and doing it blocked the
		// accounts whose slot HAD just come free.
		if r == nil && isLoginRateRejection(err) {
			m.loginRate.Penalize()
		}
		noteLoginOutcome(a, err)
		// Tell the fleet what this attempt learned about the world it dialled, so a world
		// that is full is known to every other account rather than rediscovered ~300 times
		// (worldfailover.go). Recording only; the move itself is retryAccount's.
		m.noteWorldOutcome(a, err)
		return err
	}
	noteLoginOutcome(a, nil)
	m.noteWorldOutcome(a, nil)

	a.mu.Lock()
	if a.deliberateDisconnect {
		a.disconnected = true
		a.mu.Unlock()
		_ = s.Logout()
		m.routeSessionEnded(a, 0, true)
		return fmt.Errorf("fleet: login canceled by disconnect request")
	}
	a.session = s
	a.connectedAt = time.Now()
	a.disconnected = false
	a.disconnectMsg = ""
	a.lastErr = ""
	a.mu.Unlock()
	return nil
}

// startReadLoop starts a's read loop in a new goroutine. On the loop exiting (the
// connection dropped or was deliberately closed), it marks the account disconnected and,
// unless the fleet itself is shutting down (m.stop closed) or the disconnect was a clean
// logout StopBot/CloseAll caused, kicks off reconnectLoop to relogin with capped backoff.
func (m *Manager) startReadLoop(a *account) {
	a.mu.RLock()
	s := a.session
	a.mu.RUnlock()

	// PLAYER PUBLIC CHAT, which the packet switch below cannot see.
	//
	// Another player's speech does not arrive as its own packet: it rides PLAYER_INFO's
	// extended data and is decoded by gamestate's notePublicChat into an Event, so the only
	// way to read it is to subscribe to the state's event stream. Until 2026-09-21 nothing
	// did, and the consequence was larger than it sounds -- a.chat recorded MESSAGE_GAME and
	// nothing else, so the control panel's whole chat feed (built from AccountStatus.RecentChat)
	// could not show a single word any player said, and a script had no way to hear one.
	//
	// That cost real diagnosis time. The woodcutters buy rune axes from a player-run stall by
	// saying "buying 1 rune axe" and waiting for the seller to open a trade; when nothing
	// happened, the panel's silent chat feed looked like evidence the fleet was not receiving
	// chat. It was not evidence of anything. A separate harness (cmd/publicchattest) then
	// proved public chat works in BOTH directions, with a 15-tile range, so the remaining
	// question is what the stall actually says -- which needs someone listening.
	//
	// Both public chat types are recorded. The type follows the SPEAKER'S rights: a staff
	// account's line arrives as ChatPublicMod and an ordinary player's as ChatPublic, and every
	// lab account is staffModLevel 2 while production players are not. Filtering on one type
	// gives a listener that works in exactly one environment, which is a trap worth naming.
	if st := s.State(); st != nil {
		st.Subscribe(func(ev gamestate.Event) {
			if line, ok := publicChatLine(ev); ok {
				a.chat.Record(line)
			}
			if ev.Kind == gamestate.EventDeath {
				m.logDeath(a, st) // the one death line every account gets (deathlog.go)
			}
			if ev.Kind == gamestate.EventInventoryChanged && ev.Comp == packComponent {
				m.noteGroundGain(a, st, ev) // a pickup of a death pile, by anyone (deathloot.go)
			}
		})
	}

	go func() {
		err := s.ReadLoop(m.stop, func(p client.Packet) {
			a.mu.Lock()
			a.packets++
			a.mu.Unlock()
			// Decoded once here, regardless of whether a bot is running yet, so
			// StartBot can hand a script a chat log that already has recent history
			// instead of starting empty every time.
			switch p.Opcode {
			case serverprot.MESSAGE_GAME:
				a.chat.Record(packet.NewReader(p.Payload).GJStr())
			case serverprot.UPDATE_INV_FULL, serverprot.UPDATE_INV_PARTIAL:
				// A fresh packet.Reader over the same bytes session.State().Handle
				// will also read — reading the leading component id here does not
				// consume or race that read. Only actions.BankWithdraw is worth
				// stamping a time onto; every other inventory component already has
				// its own known-inventories snapshot (AccountStatus.Inventories)
				// without needing a "when" at all.
				// Live 3 Oct 14:47 an inventory packet arrived with an empty payload and this read
				// panicked the whole fleet (index out of range in G2).
				if len(p.Payload) < 2 {
					break
				}
				if comID := int(packet.NewReader(p.Payload).G2()); comID == actions.BankWithdraw {
					a.mu.Lock()
					a.bankUpdatedAt = time.Now()
					a.mu.Unlock()
				}
			}
		})
		// The loop is over, so this session is too: close it. A read error leaves the
		// socket open and the keepalive goroutine still sending NO_TIMEOUT through it, and
		// the server keeps a player whose client still answers in the world for as long as
		// it does, refusing every relog with response 5. Live 5 Oct 21:37: a psiphon2 stall
		// read-timed-out 34 sessions, the fleet kept 38 sockets open on that route for 55
		// accounts in game, and those accounts were refused for over 20 minutes. Close is
		// idempotent, so the paths that already closed the session (Logout from
		// Disconnect, CloseAll or a hop) are unaffected.
		_ = s.Close()

		a.mu.Lock()
		lived := time.Since(a.connectedAt)
		a.disconnected = true
		a.session = nil // so Snapshot's Connected accurately drops while reconnecting
		if err != nil {
			a.disconnectMsg = err.Error()
		}
		// A bot mid-action against a now-dead session would just spin on send
		// errors; stop it so a reconnect starts from a clean slate. The account
		// keeps its last script name so StartBot can be called again once
		// reconnected — this does not auto-resume the bot, only the connection.
		runner := a.runner
		// A script that declared itself finished (bot.Context.RequestFinish) logged out on
		// purpose, so this drop is not the unexpected kind. Without this the account would
		// be reconnected below and its script restarted, undoing the very thing it finished
		// to do — treat it exactly like an operator-requested Disconnect instead.
		finishReason, finished := "", false
		if runner != nil {
			finishReason, finished = runner.FinishRequested()
		}
		if finished {
			a.deliberateDisconnect = true
			a.resumeScript = false
		} else if runner != nil && runner.Status().State == bot.StateRunning && !a.deliberateDisconnect {
			a.resumeScript = true
		}
		a.mu.Unlock()
		a.mu.RLock()
		leaving := a.deliberateDisconnect
		a.mu.RUnlock()
		m.routeSessionEnded(a, lived, leaving)
		if finished {
			m.alogf(a, "[%s] script finished for good (%s) — staying logged out", a.username, finishReason)
		}
		if runner != nil && runner.Status().State == bot.StateRunning {
			runner.Stop()
			m.alogf(a, "[%s] bot stopped: connection dropped", a.username)
		}

		select {
		case <-m.stop:
			// Deliberate fleet-wide shutdown (CloseAll) — do not reconnect.
			return
		default:
		}
		if err == nil {
			// s.ReadLoop returning nil error only happens via m.stop today, but
			// guard anyway: a nil error with m.stop still open would otherwise
			// spin reconnectLoop against a session that was closed on purpose
			// by something other than CloseAll (there is no such path today,
			// but this keeps the invariant explicit rather than assumed).
			return
		}
		a.mu.RLock()
		deliberate := a.deliberateDisconnect
		a.mu.RUnlock()
		if deliberate {
			// Manager.Disconnect closed this stream on purpose (via Session.Logout)
			// — this is not an unexpected drop, so reconnectLoop must not fire and
			// relog the account the operator just told to log out. See the
			// deliberateDisconnect field doc and Disconnect's own doc comment.
			m.alogf(a, "[%s] disconnected (requested)", a.username)
			return
		}
		m.alogf(a, "[%s] disconnected: %v — reconnecting", a.username, err)
		m.reconnectLoop(a)
	}()
}

// Backoff schedule for reconnectLoop: doubles from reconnectBackoffStart up to
// reconnectBackoffMax, then holds there. Retries indefinitely — a fleet meant to run
// unattended for a full day should keep trying rather than giving up after N attempts.
const (
	reconnectBackoffStart = 2 * time.Second
	reconnectBackoffMax   = 60 * time.Second
)

// reconnectBackoff returns how long to wait before reconnect attempt number attempt
// (1-indexed: the delay before the *first* retry, after the initial drop).
func reconnectBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := reconnectBackoffStart
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= reconnectBackoffMax {
			return reconnectBackoffMax
		}
	}
	return d
}

// jitterFraction randomizes reconnectBackoff's output by ±30%. Found live in production
// (2026-09-14): reconnectBackoff is a pure function of attempt number, so every account
// that dropped in the same instant (a real websocket close 1005 hit all 61 accounts on
// the same tick, most plausibly a host-side network interruption) computed the identical
// wait at every attempt and retried in perfect lockstep forever — each synchronized wave
// re-triggered the login server's flood limiter (response 16) together, so the fleet could
// never recover on its own; it needed a full process restart, which only helped because a
// fresh launch's staggered initial Connect (loginDelaySeconds apart) isn't reconnectLoop at
// all. Jitter alone doesn't fix the first wave, but it desynchronizes the fleet over a few
// retries so later waves stay under the flood threshold and the fleet self-heals without
// operator intervention.
func jitterFraction(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	factor := 0.7 + rand.Float64()*0.6 // [0.7, 1.3)
	return time.Duration(float64(d) * factor)
}

// reconnectLoop retries temporary login failures with capped exponential backoff until
// success, a permanent failure, or shutdown. On success it restarts the read loop, exactly
// as if this were the account's first Connect.
func (m *Manager) reconnectLoop(a *account) {
	if !m.claimReconnect(a) {
		return
	}
	m.retryAccount(a)
}

func (m *Manager) claimReconnect(a *account) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reconnecting || a.connecting || a.session != nil || a.deliberateDisconnect {
		return false
	}
	a.reconnecting = true
	a.reconnectAttempt = 0
	return true
}

func (m *Manager) startReconnect(a *account) {
	if m.claimReconnect(a) {
		go m.retryAccount(a)
	}
}

func (m *Manager) retryAccount(a *account) {
	clearRetryState := true
	defer func() {
		if !clearRetryState {
			return
		}
		a.mu.Lock()
		a.reconnecting = false
		a.mu.Unlock()
	}()

	for {
		select {
		case <-m.stop:
			return
		default:
		}
		if m.deliberatelyDisconnected(a) {
			m.alogf(a, "[%s] reconnect canceled: disconnect requested", a.username)
			return
		}

		a.mu.Lock()
		a.reconnectAttempt++
		attempt := a.reconnectAttempt
		ceiling := a.ceilingAttempt
		a.mu.Unlock()

		// An account parked against the server's per-address connection ceiling waits on
		// its own far slower schedule. Nothing it can do frees a slot, and retrying on
		// the ordinary schedule spends the fleet's shared login budget on attempts that
		// cannot be admitted — starving the accounts whose slot genuinely just came free.
		// See loginceiling.go for the measured numbers behind that.
		var wait time.Duration
		if ceiling > 0 {
			wait = jitterFraction(connectionCeilingBackoff(ceiling))
			// Says what is actually happening, which the old generic backoff line did
			// not: reading the live log, 176 accounts parked against a server-side
			// ceiling looked exactly like 176 accounts with ordinary connection trouble.
			m.alogf(a, "[%s] waiting %s for a connection slot — the server reports this address is at its connection limit (%d consecutive refusals); this is not a fault with this account", a.username, wait, ceiling)
		} else {
			wait = jitterFraction(reconnectBackoff(attempt))
			m.alogf(a, "[%s] reconnect attempt %d in %s", a.username, attempt, wait)
		}
		wait = m.retryWaitFor(a, wait) // a response 21 states its own wait (hoptimer.go)
		if !m.waitAccountRetry(a, wait) {
			return
		}
		if m.deliberatelyDisconnected(a) {
			m.alogf(a, "[%s] reconnect canceled: disconnect requested", a.username)
			return
		}

		if err := m.loginOnce(a); err != nil {
			a.mu.Lock()
			a.lastErr = err.Error()
			a.mu.Unlock()
			m.alogf(a, "[%s] reconnect attempt %d failed: %v", a.username, attempt, err)
			m.noteRetryHopTimer(a, err)
			// A FULL WORLD IS A REASON TO BE SOMEWHERE ELSE, never a reason to keep
			// knocking. Measured live 2026-09-22: 768 response-7 rejections across four
			// healthy routes while 57 accounts reconnected forever against a world at its
			// 1,000-player cap. Moving to a world with room and retrying there costs one
			// login; waiting costs everything that account would have earned. See
			// worldfailover.go. With one configured world this is a no-op and the ordinary
			// backoff below runs exactly as before.
			if isWorldFullRejection(err) && m.failoverToOpenWorld(a) {
				// Not a failed attempt against this account's budget: a different world is
				// a different question. Reset the backoff so the new world is tried at
				// once, still paced by the fleet's own login gate like every other login.
				a.mu.Lock()
				a.reconnectAttempt = 0
				a.mu.Unlock()
				continue
			}
			if !retryableLoginError(err) && !m.rotatedKeyRetry(a, err) {
				m.alogf(a, "[%s] reconnect stopped: login failure requires attention", a.username)
				return
			}
			continue
		}

		if m.deliberatelyDisconnected(a) {
			// Rare race: the operator called Disconnect while this login attempt
			// was already in flight. Rather than leave an account connected that
			// was just told to log out, log the freshly-opened session back out
			// immediately instead of starting its read loop. The read loop isn't
			// running yet, so Session.Logout's own ServerProt.LOGOUT wait cannot
			// be satisfied and it falls back to its documented behavior of closing
			// the socket once its reply timeout elapses — still a real logout
			// click was sent, so the server still logs the account out cleanly on
			// its side; this client-side path just can't observe the reply.
			a.mu.RLock()
			s := a.session
			a.mu.RUnlock()
			m.alogf(a, "[%s] reconnected but a disconnect was requested meanwhile — logging back out", a.username)
			if s != nil {
				_ = s.Logout()
			}
			m.routeSessionEnded(a, 0, true)
			// No read loop was ever started for this session (we're returning
			// instead of calling startReadLoop), so nothing else will clear
			// a.session/a.disconnected the way startReadLoop's own cleanup does
			// on a normal drop — do it here so Snapshot doesn't keep reporting
			// this account as Connected after its session is already closed.
			a.mu.Lock()
			a.session = nil
			a.disconnected = true
			a.mu.Unlock()
			return
		}

		m.alogf(a, "[%s] reconnected after %d attempt(s)", a.username, attempt)
		a.mu.Lock()
		a.reconnectAttempt = 0
		a.reconnecting = false
		a.mu.Unlock()
		// Release this retry's state before a read loop can observe an immediate
		// drop and start its own retry. The old defer must not clear that new loop.
		clearRetryState = false
		m.startReadLoop(a)
		a.mu.Lock()
		resume, script, runner := a.resumeScript, a.script, a.runner
		a.mu.Unlock()
		if resume && runner != nil && script != "" {
			if err := m.StartBot(a.username, script, runner.Settings()); err != nil {
				m.alogf(a, "[%s] script resume after reconnect failed: %v", a.username, err)
			}
		}
		return
	}
}

// deliberatelyDisconnected reports whether a's deliberateDisconnect flag is set.
func (m *Manager) deliberatelyDisconnected(a *account) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.deliberateDisconnect
}

// CloseAll logs every connected account out and stops every read loop.
func (m *Manager) CloseAll() {
	m.closeOnce.Do(func() {
		m.loginRate.Close()
		if m.wgates != nil {
			m.wgates.close()
		}
		if m.routes != nil {
			m.routes.close()
		}
		m.mu.RLock()
		accounts := append([]*account(nil), m.accounts...)
		m.mu.RUnlock()
		var logouts sync.WaitGroup
		for _, a := range accounts {
			a.mu.Lock()
			a.deliberateDisconnect = true
			a.resumeScript = false
			s, r, g := a.session, a.runner, a.guardian
			a.mu.Unlock()
			if r != nil {
				r.Stop()
			}
			if s == nil {
				if g != nil {
					g.Stop()
				}
				continue
			}
			logouts.Add(1)
			go func() {
				defer logouts.Done()
				// An account in combat leaves it first (settleForLogout): the server does not log a player out
				// in a fight, and a closed socket leaves the account in it for a minute with nobody to eat. The
				// Guardian outlives the settle, which uses its last-resort teleport.
				m.settleForLogout(a, s)
				if g != nil {
					g.Stop()
				}
				// Keep read loops alive to observe the server's LOGOUT reply.
				if err := s.Logout(); err != nil {
					m.logf("logout: %v", err)
				}
			}()
		}
		logouts.Wait()
		close(m.stop)
	})
}

// settleForLogout takes the account out of combat before its logout (randomevents.SettleBeforeLogout), for the
// graceful paths only: CloseAll and Disconnect. Instant for an account that is not fighting.
//
// 14 Oct 11:03 and 15 Oct 00:49, 2026: two ScarabSwarm accounts stopped in the middle of a fight stood in it
// for the minute the server waits before it forces a logout on a closed socket (it holds a logout for 16 ticks
// after combat), and logged back in at 11 of 54 and 5 of 53, one dead after twelve minutes of eating in place,
// the other in two seconds.
func (m *Manager) settleForLogout(a *account, s *client.Session) {
	if s == nil {
		return
	}
	a.mu.RLock()
	name := a.username
	a.mu.RUnlock()
	randomevents.SettleBeforeLogout(s, m.cfg.Pack, func(msg string) { m.alogf(a, "[%s] %s", name, msg) })
}

// find returns the account with this username, or nil. O(n) — fine for a roster of
// dozens/low hundreds polled a few times a second, not designed for more than that.
func (m *Manager) find(username string) *account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.accounts {
		a.mu.RLock()
		name := a.username
		a.mu.RUnlock()
		if name == username {
			return a
		}
	}
	return nil
}

// Disconnect logs one specific account out cleanly — via Session.Logout (a real
// client-side logout click, waiting for the server's reply), never a bare Close — and
// marks the account as deliberately disconnected so Manager's own reconnectLoop (which
// exists only to recover from an *unexpected* drop) does not immediately try to log it
// back in behind the operator's back. See the account.deliberateDisconnect field doc for
// how that flag is threaded through startReadLoop/reconnectLoop.
//
// If the account is presently mid-automatic-reconnect (an earlier unexpected drop,
// reconnectLoop still retrying) rather than actively connected, there is no live session
// to log out yet: this instead sets the flag and lets reconnectLoop notice it and give up
// on its own next check (including the check right after a login that was already in
// flight — see reconnectLoop). Returns an error only when there is nothing to disconnect
// at all: the account is unknown, or already fully disconnected and not reconnecting.
func (m *Manager) Disconnect(username string) error {
	return m.disconnect(username, stopOperator)
}

// disconnect is Disconnect with the reason the bot stops, which only the watchdog's safety net
// reads: the panel's and the CLI's Disconnect is an operator's, a world hop's and the watchdog's
// relogin's are theirs and start the script again themselves (stopsource.go).
func (m *Manager) disconnect(username string, by stopSource) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}

	a.mu.Lock()
	session := a.session
	runner := a.runner
	reconnecting := a.reconnecting || a.connecting
	a.deliberateDisconnect = true
	a.resumeScript = false
	if runner != nil && by == stopOperator && (session != nil || reconnecting) {
		// An operator logging an account out means the bot stays off until they start one, even if
		// the bot was already stopped (by the watchdog, or by the read loop's own cleanup of a drop).
		// Marked here, before the early returns below: mid-reconnect there is no session to log out,
		// the stale runner was stopped by the read loop with no source of its own, and without this
		// mark the safety net would start it five minutes after the operator logs the account back
		// in by hand. A call that fails ("is not connected") changes nothing and marks nothing.
		a.noteStopLocked(by)
	}
	a.mu.Unlock()

	if session == nil {
		if reconnecting {
			m.alogf(a, "[%s] disconnect requested mid-reconnect: canceling further attempts", username)
			if m.routes != nil {
				// Also wakes the account if it is waiting in the route queue.
				m.routes.release(a)
			}
			return nil
		}
		// Nothing to do — undo the flag we just set so it doesn't block a later
		// ConnectExisting/ordinary reconnect for no reason.
		a.mu.Lock()
		a.deliberateDisconnect = false
		a.mu.Unlock()
		return fmt.Errorf("fleet: %s is not connected", username)
	}

	// A bot mid-action against a session about to close would just spin on send
	// errors, same reasoning as startReadLoop's own cleanup on an unexpected drop.
	running := runner != nil && runner.Status().State == bot.StateRunning
	if running {
		a.mu.Lock()
		a.noteStopLocked(by)
		a.mu.Unlock()
		runner.Stop()
	}
	m.alogf(a, "[%s] disconnect requested", username)
	// An account in combat leaves it before the logout (settleForLogout), with its Guardian still up: the server
	// does not log a player out in a fight, and a closed socket leaves it there for a minute. A world hop and the
	// watchdog's relogin come through here too.
	m.settleForLogout(a, session)
	if running {
		a.mu.RLock()
		guardian := a.guardian
		a.mu.RUnlock()
		if guardian != nil {
			guardian.Stop()
		}
		m.alogf(a, "[%s] bot stopped: disconnect requested", username)
	}

	err := session.Logout()
	if m.routes != nil {
		// Freed only once the logout has finished, so the slot is never handed on while the
		// old session could still be open on the server.
		m.routes.release(a)
	}
	return err
}

// ConnectExisting queues a login for a specific account already known to this Manager (added via an
// earlier Connect/AddAccount) that is not currently connected — either because Disconnect
// was called on it, or because it dropped unexpectedly and reconnectLoop is not (or is no
// longer) retrying it. It is the on-demand counterpart to reconnectLoop's automatic retry.
// Acceptance returns immediately; Snapshot exposes progress and any terminal failure.
// For an account not yet known to this Manager at all, use AddAccount instead.
//
// Fails clearly rather than racing a login attempt already in progress: an unknown
// account, one that is already connected, or one reconnectLoop is presently retrying are
// all errors — wait for that to finish (or call Disconnect first, which cancels it) rather
// than calling this concurrently with it.
func (m *Manager) ConnectExisting(username string) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}

	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return fmt.Errorf("fleet: %s is already connected", username)
	}
	if a.reconnecting {
		if a.deliberateDisconnect {
			a.mu.Unlock()
			return fmt.Errorf("fleet: %s is finishing disconnect; retry shortly", username)
		}
		if a.retryWake == nil {
			a.retryWake = make(chan struct{}, 1)
		}
		select {
		case a.retryWake <- struct{}{}:
		default:
		}
		a.mu.Unlock()
		m.alogf(a, "[%s] manual retry requested — waking reconnect backoff, retaining fleet login pacing", username)
		return nil
	}
	if a.connecting {
		a.mu.Unlock()
		return fmt.Errorf("fleet: %s is already connecting", username)
	}
	// The operator asked to be logged back in — this is the one place that clears a
	// prior Disconnect's flag (a brand-new account never has it set to begin with).
	a.deliberateDisconnect = false
	a.lastErr = ""
	a.reconnecting = true
	a.reconnectAttempt = 0
	a.mu.Unlock()
	m.alogf(a, "[%s] reconnect requested — queued behind the fleet login pacing", username)
	go m.retryAccount(a)
	return nil
}

// AddAccount adds a brand-new account — one that was not part of this Manager's
// construction-time roster — connects it, and, when script is non-empty, starts that
// registered internal/bot script on it with settings (nil is fine), exactly like
// cmd/fleet/cmd/controlpanel's own startup loop does for a roster entry. This is the
// runtime counterpart to that fixed-roster path: it returns an error the caller can act on
// (a duplicate username, a login failure, a bad script name) rather than only logging it.
//
// AddAccount does not persist username/password/script/settings anywhere — see this
// package's doc comment and COLLAB.md for why runtime-added accounts are in-memory only
// for this process's lifetime, not written back to fleet.config.json.
func (m *Manager) AddAccount(username, password, script string, settings map[string]interface{}) error {
	return m.AddAccountWorld(username, password, script, 0, settings)
}

// AddAccountWorld is AddAccount for an account that names its own world; world 0 means the
// fleet's own, so AddAccount is exactly this with 0.
func (m *Manager) AddAccountWorld(username, password, script string, world int, settings map[string]interface{}) error {
	if m.find(username) != nil {
		return fmt.Errorf("fleet: %s is already in this fleet", username)
	}
	a, err := m.connectAccount(username, password, world)
	if err != nil {
		return fmt.Errorf("fleet: connect %s: %w", username, err)
	}
	m.alogf(a, "[%s] added to the fleet and connected", username)
	if script == "" {
		return nil
	}
	if err := m.StartBot(username, script, settings); err != nil {
		return fmt.Errorf("fleet: %s connected but failed to start %q: %w", username, script, err)
	}
	return nil
}

// StartBot resolves username's already-connected session and starts scriptName against
// it, wired up with the fleet's shared Pack/Locs/Cache (Config, above) and a fresh
// random-event guardian scoped to this account. It fails clearly rather than silently
// no-op-ing when the account isn't connected yet, the script name isn't registered
// (internal/bot.Register), or a bot is already running on that account — StopBot it
// first. settings is passed straight through to bot.NewContext; nil is fine.
func (m *Manager) StartBot(username, scriptName string, settings map[string]interface{}) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	m.applyPinSetting(username, settings) // before any account lock (worldhome.go)
	a.startBotMu.Lock()
	defer a.startBotMu.Unlock()
	b, ok := bot.New(scriptName)
	if !ok {
		return fmt.Errorf("fleet: no registered script %q", scriptName)
	}

	a.mu.Lock()
	// Join the previous script and handler before registering a replacement.
	// Release the account lock while joining: old log callbacks may need it.
	oldRunner, oldGuardian := a.runner, a.guardian
	if oldRunner != nil && oldRunner.Status().State == bot.StateRunning {
		script := a.script
		a.mu.Unlock()
		return fmt.Errorf("fleet: %s already has a bot running (%s)", username, script)
	}
	a.mu.Unlock()
	if oldGuardian != nil {
		oldGuardian.Stop()
		oldGuardian.Wait()
	}
	if oldRunner != nil {
		oldRunner.Wait()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return fmt.Errorf("fleet: %s is not connected yet", username)
	}
	if a.runner != nil && a.runner.Status().State == bot.StateRunning {
		return fmt.Errorf("fleet: %s already has a bot running (%s)", username, a.script)
	}

	// Register the name/interface-archive resolvers internal/randomevents needs for its
	// dialog/pick and box/lamp event kinds (Genie, Mysterious Old Man, Strange Plant,
	// Sandwich Lady, Frog, Drunken Dwarf, Mysterious box, Antique lamp — see
	// randomevents/names.go's own doc comments). Without this, Detect() silently skips
	// those two event kinds entirely, which is a real, confirmed gap: nothing in this
	// package previously called SetNameResolver/SetInterfaceArchive anywhere on the live
	// fleet/controlpanel path (only cmd/mazetest, a throwaway diagnostic tool, ever did) —
	// so a live Guardian never actually detected a Genie/Mysterious Old Man NPC as an
	// event to handle, only as an ordinary nearby NPC. That NPC's own server-side script
	// nags the player 3 times over its ai_timer and, on the 4th ignored tick, teleports
	// them to one of ~24 fixed far-away locations completely unrelated to their current
	// position (content/scripts/macro events/scripts/general/macro_event_genie.rs2 and
	// .../macro_event_mysterious_old_man.rs2's own `queue(macro_event_fail_teleport, ...)`
	// call, confirmed by reading the server's own content scripts) — this is the concrete
	// mechanism behind the real stranding incident that motivated this whole task, not
	// just a theoretical gap. SetNameResolver/SetInterfaceArchive are safe to call every
	// StartBot (idempotent process-wide registration, guarded by their own mutex); both
	// are no-ops when m.cfg.Cache/Components are nil, matching every other optional-primitive
	// pattern this package already uses (see StartBot's own Components handling elsewhere).
	if m.cfg.Cache != nil {
		randomevents.SetNameResolver(m.cfg.Cache)
	}
	if m.cfg.Components != nil {
		randomevents.SetInterfaceArchive(m.cfg.Components)
	}
	// The combat flee overlays live door/wall changes on the collision pack with this index
	// (randomevents.fleePack); without it the flee plans on the static pack as before.
	if m.cfg.Locs != nil {
		randomevents.SetLocIndex(m.cfg.Locs)
	}

	guardian := randomevents.NewGuardian(a.session, m.cfg.Pack)
	// Every script start begins on the default Lamp skill (Fishing); a script that wants another
	// (ScarabSwarm, MasterCrafter: Magic) chooses it in its own OnStart (internal/bot/lamps.go).
	randomevents.SetLampSkill(a.session, randomevents.LampFishing)
	guardian.Chat = a.chat
	prefix := fmt.Sprintf("[%s/%s] ", username, scriptName)
	a.guardianAct.Reset()
	// Random-event rewards into the Loot panel, measured off the same log lines (randomloot.go).
	eventLoot := m.randomEventLoot(a, username, prefix)
	guardian.Log = func(msg string) {
		m.alogf(a, "%s[guardian] %s", prefix, msg)
		a.guardianAct.Note(msg, time.Now())
		eventLoot.Note(msg)
		// "handling" (fired, outcome unknown yet) is intentionally not recorded here
		// — see parseRandomEvent's doc — only terminal outcomes land in the panel.
		if name, outcome, ok := parseRandomEvent(msg); ok && outcome != "handling" {
			a.recordRandomEvent(name, outcome)
		}
	}
	guardian.HopWorld = func(world int, reason string) error { return m.HopWorld(username, world, reason) }
	guardian.World = func() int { return m.WorldOf(username) }
	go guardian.Start()

	ctx := bot.NewContext(a.session, username, m.cfg.Pack, m.cfg.Locs, m.cfg.Cache, m.cfg.Components, m.cfg.Bus, guardian, a.chat, settings, func(msg string) { m.alogf(a, "%s%s", prefix, msg) })
	// Set after construction rather than as another NewContext parameter: that signature is
	// already eleven arguments and fifteen cmd/ tools call it, none of which want a peer
	// directory.
	ctx.Peers = m.cfg.Peers
	// Worlds. Set after construction for the same reason Peers is: NewContext's signature is
	// already eleven arguments and fifteen cmd/ tools call it. All four are nil/zero-safe, so
	// a standalone tool's Context behaves exactly as it did before these existed.
	//
	// ctx.WorldOf is what lets a donor answer "which world is the mule on" without the
	// roster hardcoding it into the script's settings: it reads this Manager's own live
	// per-account world, which cannot be stale for an account in this process.
	// READ THE FIELDS DIRECTLY, NOT THROUGH WorldOf/HomeWorldOf. This function already holds
	// a.mu (see the Lock/defer Unlock above) and Go's sync.RWMutex is not reentrant, so
	// calling an accessor that takes a.mu.RLock() here deadlocks the goroutine permanently —
	// found exactly that way on 2026-09-22: StartBot hung holding a.mu, the account never
	// started, and CloseAll then blocked on the same lock in test cleanup and took the whole
	// package to its 10-minute timeout. Cheap to write, expensive to debug; hence this note.
	ctx.CurrentWorld, ctx.HomeWorld = a.world, a.hop.Home
	if ctx.CurrentWorld <= 0 {
		ctx.CurrentWorld = m.FleetWorld()
	}
	if ctx.HomeWorld <= 0 {
		ctx.HomeWorld = ctx.CurrentWorld
	}
	// The fleet's shared bank directory. Without this every PublishBank is a no-op and no
	// donor can see what the mule holds (peers.go).
	ctx.Peers = m.peerDirectory()
	ctx.WorldOf = m.WorldOf
	ctx.HopWorld = func(world int, reason string) error { return m.HopWorld(username, world, reason) }
	// A script's say in its home world, and what it may know of hops and logins (worldhome.go).
	ctx.SetHomeWorld = func(world int) error { return m.SetHomeWorld(username, world) }
	ctx.HopInProgress = func() bool { return m.Hopping(username) }
	ctx.SetHopBudget = func(maxHops int, window time.Duration) error { return m.SetHopBudget(username, maxHops, window) }
	ctx.LoginBusy = m.LoginBusy
	ctx.Loot, ctx.TradeLog = m.lootRecorder(username), m.tradeRecorder()
	ctx.MuleOut = m.muleOutReader()
	ctx.ShareKills()
	// Experience reaches the skill cards and the kills that earned it (bot/lootxp.go).
	ctx.TrackExperience()
	runner := bot.NewRunner(scriptName, b, ctx)
	a.guardian = guardian
	a.runner = runner
	a.script = scriptName
	a.resumeScript = false
	a.stopBy = stopNone // a new run: nobody has stopped it
	runner.Start()
	m.alogf(a, "%sstarted", prefix)
	return nil
}

// StopBot requests the running bot on username to stop (see bot.Runner.Stop — this
// returns as soon as the request is made, it does not wait for the bot to actually
// finish) and stops its guardian. Poll Snapshot for when it's actually finished.
//
// This is the operator's explicit stop (the panel's POST /api/bot/stop, bulk stop, a stream
// take-over, cmd/fleet's dashboard): the account is marked so that the watchdog's safety net leaves
// the bot stopped until something starts a script on the account again (stopsource.go).
func (m *Manager) StopBot(username string) error {
	return m.stopBot(username, stopOperator)
}

// stopBot is StopBot with the reason the bot stops (stopsource.go). The public StopBot is the
// panel's and the CLI's, so it is an operator's explicit stop; the watchdog's restart passes its own.
func (m *Manager) stopBot(username string, by stopSource) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runner == nil {
		return fmt.Errorf("fleet: %s has no bot to stop", username)
	}
	a.runner.Stop()
	a.noteStopLocked(by)
	a.resumeScript = false
	if a.guardian != nil {
		a.guardian.Stop()
	}
	m.alogf(a, "[%s/%s] stop requested", username, a.script)
	return nil
}

// RequestHandoff asks the bot running on username for a manual mule hand-off at its next
// check (see bot.Context.RequestHandoff) — the control panel's own "hand off now" button,
// so an operator doesn't have to wait out a script's own muleAt threshold to prove a
// donor/mule pair works end to end. Fails clearly if the account has no bot running; a
// script that never calls Context.TakeHandoffRequest (every registered script except
// ProgressiveFisher, tonight) simply never acts on it — this does not fail for that case,
// since Manager has no way to know which scripts look at it and a silent no-op there
// matches how an unused setting already behaves everywhere else in this package.
func (m *Manager) RequestHandoff(username string) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runner == nil {
		return fmt.Errorf("fleet: %s has no bot running", username)
	}
	a.runner.RequestHandoff()
	m.alogf(a, "[%s/%s] mule hand-off requested from the control panel", username, a.script)
	return nil
}

// SetMuleAt live-overrides the bot running on username's own muleAt threshold (see
// bot.Context.SetMuleAt) — the control panel's other live-editable knob (2026-09-14): the
// banked-item count that starts a hand-over shouldn't require a restart to change. n <= 0
// clears the override, falling back to whatever the script was started with.
func (m *Manager) SetMuleAt(username string, n int) error {
	a := m.find(username)
	if a == nil {
		return fmt.Errorf("fleet: no such account %q", username)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runner == nil {
		return fmt.Errorf("fleet: %s has no bot running", username)
	}
	a.runner.SetMuleAt(n)
	if n > 0 {
		m.alogf(a, "[%s/%s] muleAt live-overridden to %d from the control panel", username, a.script, n)
	} else {
		m.alogf(a, "[%s/%s] muleAt override cleared from the control panel", username, a.script)
	}
	return nil
}

// AccountStatus is one account's reportable state, JSON-tagged for the dashboard.
type AccountStatus struct {
	Username      string               `json:"username"`
	Connected     bool                 `json:"connected"`
	Connecting    bool                 `json:"connecting,omitempty"`
	Disconnected  bool                 `json:"disconnected"`
	Error         string               `json:"error,omitempty"`
	DisconnectMsg string               `json:"disconnectMsg,omitempty"`
	StaffModLevel int                  `json:"staffModLevel"`
	SelfSlot      int                  `json:"selfSlot"`
	MainModal     int                  `json:"mainModal"`
	SideModal     int                  `json:"sideModal"`
	Packets       int                  `json:"packets"`
	ConnectedSecs float64              `json:"connectedSecs"`
	Inventories   map[int][]SlotStatus `json:"inventories,omitempty"`

	// Pos is the account's last known world tile (see gamestate.State.Tile), nil until
	// the session has ever had a tile fix. Its own type (TilePos, below) rather than
	// reusing anything from internal/controlpanel, which already imports this package
	// — controlpanel's FleetSource copies this into its own Position shape.
	Pos *TilePos `json:"pos,omitempty"`

	// Reconnecting/ReconnectAttempt report an in-progress relogin after an unexpected
	// drop (see Manager.reconnectLoop). ReconnectAttempt is 1 for the first retry.
	Reconnecting     bool `json:"reconnecting,omitempty"`
	ReconnectAttempt int  `json:"reconnectAttempt,omitempty"`

	// ManuallyDisconnected reports the account's own deliberateDisconnect flag — true
	// after Manager.Disconnect and cleared by ConnectExisting (or a fresh Connect/
	// AddAccount). Lets the dashboard tell "the operator logged this out on purpose,
	// offer a reconnect button" apart from "dropped unexpectedly, reconnectLoop is
	// already retrying it" — both otherwise look like plain Disconnected/!Reconnecting.
	ManuallyDisconnected bool `json:"manuallyDisconnected,omitempty"`

	// Route is the id of the route (routes.go) this account holds a slot on, and RouteWait
	// is true while it waits in the queue for one. Both are empty with no proxy pool.
	Route     string `json:"route,omitempty"`
	RouteWait bool   `json:"routeWait,omitempty"`

	// World is the game world this account is logged in to (or will dial next), and
	// HomeWorld the one its roster entry gave it. Always set, never 0 — an account that
	// named no world reports the fleet's own — so a panel can group by world without
	// having to know which case it is in. HopLog is the last world-hop line, empty for an
	// account that has never hopped.
	//
	// Both worlds share ONE coordinate space: two accounts on different worlds standing on
	// the same tile are different players who cannot see each other. Anything drawing
	// positions has to distinguish them; see docs and the panel's own map code.
	World     int    `json:"world"`
	HomeWorld int    `json:"homeWorld"`
	HopLog    string `json:"hopLog,omitempty"`

	// Script/BotState/BotError/BotStartedAt report the account's attached
	// internal/bot.Runner, if StartBot has ever been called for it. BotState is empty
	// (not "idle") until a bot has actually been started, so the dashboard can tell "no
	// bot ever attached" apart from "attached but not yet running".
	Script       string `json:"script,omitempty"`
	BotState     string `json:"botState,omitempty"`
	BotError     string `json:"botError,omitempty"`
	BotStartedAt string `json:"botStartedAt,omitempty"`

	// Settings is the settings map the account's attached internal/bot.Runner was
	// started with (see Runner.Settings), surfaced so the control panel can offer
	// "duplicate this account's settings" when adding a new account, and a settings
	// editor for an already-running one. Omitted when no bot has ever been started on
	// this account.
	Settings map[string]interface{} `json:"settings,omitempty"`

	// Skills reports current level+XP for every skill this account has ever received an
	// UPDATE_STAT for (see gamestate.State.Skills/SkillName), keyed by display name.
	// Deliberately not filtered to any one script's skill (e.g. Agility) — a fishing or
	// crafting account's relevant skill shows up here the same way, since the source is
	// the raw per-account stat table rather than anything script-specific. Omitted
	// entirely before the first stat sync (nothing connected/known yet).
	//
	// Pace ("how fast is it gaining XP") is deliberately NOT computed here. The simplest
	// robust way to show a rate is to let the dashboard's own JS remember the first XP
	// value it observes per account+skill and diff against the current one on each poll
	// — that needs no new server-side state (no per-skill history buffer to size/prune,
	// no clock-skew concerns between server and browser) and resets naturally on page
	// reload or bot restart. See dashboard.html's xpBaseline/skillsCell for the client
	// side of this.
	Skills map[string]SkillStatus `json:"skills,omitempty"`

	// RecentLog is up to maxRecentLogLines of this account's own activity log — the
	// same lines Manager.alogf both prints via Manager.Log and records here (bot
	// start/stop, guardian activity, and whichever script is running's own progress
	// lines), so they're visible from the dashboard and not just cmd/fleet's stdout.
	// Oldest first (index 0 is the oldest line still retained) — the same order a
	// terminal scrolls in, so the dashboard can render it top-to-bottom and scroll its
	// log panel to the bottom to show the newest line, matching how a live tail reads.
	RecentLog []string `json:"recentLog,omitempty"`

	// RandomEvents is this account's own recent random-event history (see the
	// account.randomEvents field doc) — a separate, compact panel from RecentLog
	// above, specifically so "what random events fired and were they handled" doesn't
	// require scrolling/searching the general activity log. Newest first (unlike
	// RecentLog, which is oldest first) since this is a summary list meant to be read
	// top-down, not a scrolling tail. Omitted while empty (no bot ever started on this
	// account, or no random event has fired yet).
	RandomEvents []RandomEventOutcome `json:"randomEvents,omitempty"`

	// RecentChat is up to maxRecentChatLines of this account's own raw game chat/
	// system-message capture (see account.chat's own doc comment — MESSAGE_GAME text,
	// verbatim, not filtered to public chat only: trade requests, "you have been
	// PKed"-style system lines and GM/staff messages all arrive this same way). Oldest
	// first, matching RecentLog's own order. This is the account's own eyes on the
	// game world, not what the bot itself is doing (that's RecentLog) — a headless
	// client has no other way to see it. Omitted while empty.
	RecentChat []string `json:"recentChat,omitempty"`

	// ChatCount is how many chat lines this account has ever captured (chatlog.Log.
	// Total(), never reset, never trimmed by RecentChat's own cap). A consumer that
	// wants a live per-line feed rather than only a polled recent-N snapshot can
	// remember the ChatCount it last saw and diff against this one — see internal/
	// controlpanel/fleetsource.go's observe(), which does exactly this to turn new
	// chat lines into individually streamed chat.message events.
	ChatCount int `json:"chatCount,omitempty"`

	// BankItems is this account's bank contents as of the last time its bank was open
	// (account.bankUpdatedAt, below) — sourced from gamestate.State.KnownInventories()
	// [actions.BankWithdraw], the same "last known, not necessarily current" snapshot
	// Inventories above already holds for every other component, just picked out and
	// labeled specifically as the bank rather than left as an unlabeled component id.
	// Omitted while the bank has never been open this session.
	BankItems []SlotStatus `json:"bankItems,omitempty"`

	// BankUpdatedAt is when BankItems was last genuinely refreshed by the server (RFC3339,
	// empty if never) — see account.bankUpdatedAt's own doc comment for why this needs
	// its own timestamp rather than being inferred from "now": the contents in BankItems
	// keep showing even long after the bank modal has closed, so the dashboard needs to
	// say plainly "as of <time>" instead of implying a live reading.
	BankUpdatedAt string `json:"bankUpdatedAt,omitempty"`

	// HPKnown/HP/MaxHP surface gamestate.State.HP/MaxHP (see health.go's own doc comment
	// for why current HP rides the Hitpoints UPDATE_STAT's "current level" byte on this
	// content build, and why MaxHP needs its own high-water-mark tracking rather than a
	// naive XP-implied level). HPKnown is false until at least one UPDATE_STAT for
	// Hitpoints has been observed, so a consumer can tell "0 HP, genuinely dead or about
	// to die" apart from "HP not known yet" rather than treating both as the zero value.
	HPKnown bool `json:"hpKnown,omitempty"`
	HP      int  `json:"hp,omitempty"`
	MaxHP   int  `json:"maxHp,omitempty"`

	// EnergyKnown/Energy surface gamestate.State.Energy() — run energy on the game's own
	// 0-100 scale. EnergyKnown is false until an UPDATE_RUNENERGY has been observed, for
	// the same reason HPKnown exists: "0% energy, the server has just switched running
	// off" and "we have not been told yet" are different facts and must not share the
	// zero value.
	//
	// Exposed because two live questions about the fleet's 16.5% walking share could only
	// be argued, not measured, without it: whether fisherrunner.RunEnergyThreshold (20 until 2026-09-25, now 1) is
	// costing anything, given that file's own argument that a fishing bot has nothing to
	// conserve energy FOR; and whether the run toggle being checked once per leg matters,
	// which depends entirely on how often energy climbs back past the threshold mid-leg.
	// Reporting only — nothing reads this to make a decision.
	EnergyKnown bool `json:"energyKnown,omitempty"`
	Energy      int  `json:"energy,omitempty"`

	// Dead mirrors gamestate.State.IsDead() — set when checkDeathLine has matched the
	// game's own "You have died" chat line (see health.go), cleared by the state's own
	// ClearDead once a script has acknowledged/recovered from it. Surfaced here purely
	// as a read: this package does not call ClearDead itself.
	Dead bool `json:"dead,omitempty"`

	// Watchdog is the stall watchdog's view of this account (watchdog.go): omitted while it has
	// nothing to say, "stalled" while a stall is detected or being worked, "needs_attention"
	// once the ladder has given up.
	Watchdog *WatchdogStatus `json:"watchdog,omitempty"`
}

// SkillStatus is one skill's current level+XP, JSON-friendly.
type SkillStatus struct {
	Level int `json:"level"`
	XP    int `json:"xp"`
}

// TilePos is one world tile: x/z and the height level, JSON-friendly.
type TilePos struct {
	X     int `json:"x"`
	Z     int `json:"z"`
	Level int `json:"level"`
}

// RandomEventOutcome is one random event internal/randomevents.Guardian detected and
// tried to handle for an account, JSON-friendly. Outcome is one of "cleared" (handled
// successfully), "still_present" (Handle ran but the event was still there afterwards),
// or "error" (Handle itself returned an error) — see parseRandomEvent.
type RandomEventOutcome struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	At      string `json:"at"` // RFC3339
}

// SlotStatus is one occupied inventory slot, JSON-friendly.
type SlotStatus struct {
	Slot  int   `json:"slot"`
	ID    int32 `json:"id"`
	Count int32 `json:"count"`
}

// maxRecentLogLines caps how many activity-log lines Snapshot hands the dashboard per
// account per poll. 50 is comfortably enough scrollback to see what a script has been
// doing for the last several minutes without bloating every /api/status response.
const maxRecentLogLines = 50

// maxRecentChatLines caps how many raw chat lines Snapshot hands the dashboard per
// account per poll — see AccountStatus.RecentChat. account.chat itself keeps up to
// chatlog's own maxLines (100); this is a separate, smaller cap on top of that purely
// for response size, matching maxRecentLogLines's own reasoning.
const maxRecentChatLines = 50

// Snapshot returns every account's current status for one dashboard refresh.
func (m *Manager) Snapshot() []AccountStatus {
	m.mu.RLock()
	accounts := append([]*account(nil), m.accounts...)
	m.mu.RUnlock()

	fleetWorld := m.FleetWorld()
	out := make([]AccountStatus, 0, len(accounts))
	for _, a := range accounts {
		a.mu.RLock()
		st := AccountStatus{
			Username:             a.username,
			Connected:            a.session != nil,
			Connecting:           a.connecting,
			Disconnected:         a.disconnected,
			Error:                a.lastErr,
			DisconnectMsg:        a.disconnectMsg,
			Packets:              a.packets,
			MainModal:            gamestate.NoModal,
			SideModal:            gamestate.NoModal,
			SelfSlot:             -1,
			Reconnecting:         a.reconnecting,
			ReconnectAttempt:     a.reconnectAttempt,
			ManuallyDisconnected: a.deliberateDisconnect,
			RouteWait:            a.routeWait,
			World:                a.world,
			HomeWorld:            a.hop.Home,
			HopLog:               a.hopLog,
		}
		if st.World <= 0 {
			st.World = fleetWorld
		}
		if st.HomeWorld <= 0 {
			st.HomeWorld = st.World
		}
		session := a.session
		connectedAt := a.connectedAt
		runner := a.runner
		script := a.script
		activity := a.activity
		chat := a.chat
		bankUpdatedAt := a.bankUpdatedAt
		if len(a.randomEvents) > 0 {
			st.RandomEvents = append([]RandomEventOutcome(nil), a.randomEvents...)
		}
		a.mu.RUnlock()

		if activity != nil {
			st.RecentLog = activity.Recent(maxRecentLogLines)
		}
		if m.routes != nil {
			st.Route = m.routes.routeOf(a)
		}

		if chat != nil {
			st.RecentChat = chat.Recent(maxRecentChatLines)
			st.ChatCount = chat.Total()
		}
		if !bankUpdatedAt.IsZero() {
			st.BankUpdatedAt = bankUpdatedAt.Format(time.RFC3339)
		}
		st.Watchdog = m.watchStatus(st.Username)

		if runner != nil {
			rst := runner.Status()
			st.Script = script
			st.BotState = string(rst.State)
			st.BotError = rst.Err
			if !rst.StartedAt.IsZero() {
				st.BotStartedAt = rst.StartedAt.Format(time.RFC3339)
			}
			if settings := runner.Settings(); len(settings) > 0 {
				st.Settings = settings
			}
		}

		if session != nil {
			st.StaffModLevel = session.StaffModLevel
			st.SelfSlot = session.State().Self()
			main, side, _ := session.State().Modals()
			st.MainModal, st.SideModal = main, side
			st.ConnectedSecs = time.Since(connectedAt).Seconds()
			if x, z, level, ok := session.State().Tile(); ok {
				st.Pos = &TilePos{X: x, Z: z, Level: level}
			}

			known := session.State().KnownInventories()
			if len(known) > 0 {
				st.Inventories = make(map[int][]SlotStatus, len(known))
				for comID, items := range known {
					slots := make([]SlotStatus, len(items))
					for i, it := range items {
						slots[i] = SlotStatus{Slot: it.Slot, ID: it.ID, Count: it.Count}
					}
					st.Inventories[comID] = slots
				}
				// BankItems picks the same known-inventories map's bank component out
				// and labels it explicitly — see the field's own doc comment for why
				// this is otherwise indistinguishable from any other component id in
				// Inventories above.
				if bankSlots, ok := known[actions.BankWithdraw]; ok && len(bankSlots) > 0 {
					st.BankItems = st.Inventories[actions.BankWithdraw]
				}
			}

			if hp, ok := session.State().HP(); ok {
				st.HPKnown = true
				st.HP = hp
				if maxHP, ok2 := session.State().MaxHP(); ok2 {
					st.MaxHP = maxHP
				}
			}
			if energy, ok := session.State().Energy(); ok {
				st.EnergyKnown = true
				st.Energy = energy
			}
			st.Dead = session.State().IsDead()

			// Skills: every stat that has ever received an UPDATE_STAT for this
			// account (Generation > 0) — see the AccountStatus.Skills doc comment
			// for why this is deliberately not filtered to any particular script's
			// skill.
			skills := session.State().Skills()
			skillMap := make(map[string]SkillStatus, gamestate.SkillCount)
			for stat, sk := range skills {
				if sk.Generation == 0 {
					continue
				}
				name := gamestate.SkillName(stat)
				if name == "" {
					continue
				}
				skillMap[name] = SkillStatus{Level: sk.Level, XP: sk.XP}
			}
			if len(skillMap) > 0 {
				st.Skills = skillMap
			}
		}

		// Bank persistence (see bankstore.go and Config.BankStateFile's own doc
		// comment): a genuinely fresh observation from a live session updates the
		// cache; otherwise (no session, or a session that just hasn't opened its bank
		// yet this run) fall back to the last thing this fleet ever saw for this
		// account, so the panel keeps showing it across a disconnect, a bot stop, or
		// even a process restart rather than the data simply vanishing.
		if len(st.BankItems) > 0 {
			m.rememberBank(a.username, st.BankItems, st.BankUpdatedAt)
		} else if cached, ok := m.cachedBank(a.username); ok {
			st.BankItems = cached.Items
			st.BankUpdatedAt = cached.UpdatedAt
		}
		out = append(out, st)
	}
	return out
}

// publicChatLine is the one line a.chat records for a player-speech Event, and whether the
// Event is player speech at all. Pure, so the filter can be tested without a session.
//
// Both public types count. The type follows the SPEAKER'S rights -- ChatPublicMod for a staff
// account, ChatPublic for an ordinary player -- and since every lab account is staffModLevel 2
// while production players are not, a listener that filters on only one type works in exactly
// one environment. That is the kind of difference that passes in the lab and fails live.
//
// A line with no speaker is dropped: gamestate cannot attribute chat until it knows the
// player's name, and an unattributed line is of no use to a reader trying to tell who said it.
func publicChatLine(ev gamestate.Event) (string, bool) {
	if ev.Kind != gamestate.EventChatMessage {
		return "", false
	}
	if ev.ChatType != gamestate.ChatPublic && ev.ChatType != gamestate.ChatPublicMod {
		return "", false
	}
	if ev.Username == "" {
		return "", false
	}
	return ev.Username + ": " + ev.Text, true
}
