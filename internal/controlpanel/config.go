package controlpanel

import (
	"fmt"
	"net"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/updatewatcher"
)

// MaxAccounts is how many accounts one status body lists; the rest are counted, not
// listed (see hub.go's own use of it). A plain var rather than a const, overridable via
// cmd/controlpanel's -max-accounts flag: found live (2026-09-15) that the local dev lab's
// own account-ramp testing (hundreds of accounts, working toward the wire protocol's real
// 2047-player ceiling) needs to actually SEE more than 256 of them in the dashboard at
// once, unlike a real production fleet (dozens of accounts) which never approaches this.
var MaxAccounts = DefaultMaxAccounts

// Config is the control panel's settings.
//
// Why: internal/config's control panel section has not landed on main. Until it does,
// cmd/controlpanel fills this from flags; afterwards the caller copies that section in here and
// this package keeps no dependency on the settings loader.
type Config struct {
	// Enabled must be true; Start does nothing without it.
	Enabled bool
	// CaptureCacheDir contains trusted packed game cache files; empty disables graphics assets.
	CaptureCacheDir string
	// CaptureRenderer is the explicitly built browser renderer bundle, not an arbitrary asset root.
	CaptureRenderer string

	// StreamClientBundle is the on-disk path of ONE installed stream client provider ES
	// module, served at the fixed same-origin path /stream/client.js the panel imports
	// (see internal/controlpanel/streamclient.go, and docs/stream_client_panel.md for
	// the provider contract). Files sitting beside it are served under /stream/<name>,
	// one directory deep with no traversal, so a provider can ship its own helper
	// modules.
	//
	// The bundle MUST have a "<path>.manifest.json" sidecar declaring the renderer
	// identity it implements; a missing, malformed or unknown-identity manifest fails
	// server construction rather than quietly disabling the panel. Empty (the default,
	// and every production fleet today) installs nothing: /stream/client.js answers 501
	// and /api/meta reports an empty streamClient.sceneRenderer, which is what keeps the
	// panel's Load button disabled.
	//
	// Installing a provider does NOT enable input. See StreamInputSink: session input is
	// advertised only when a Source actually implements it, and nothing does.
	StreamClientBundle string
	// EraAssets is the operator-supplied directory of game assets for eras other than the
	// fleet's own 2004 one (internal/eraassets), with one subdirectory per era. Empty (the
	// default, and every production fleet) means no non-2004 era skin can be offered, no
	// matter what a provider declares it can draw. Assets deliberately never live in this
	// repository: see internal/eraassets' own package comment for the three reasons.
	EraAssets string

	// StreamMaxFPS caps how often the existing-session bridge takes a fresh observation,
	// whatever frame rate the browser asks for. 0 uses DefaultStreamMaxFPS — read that
	// constant's own doc comment before raising this: a frame is a whole
	// gamestate.RenderSnapshot deep-copied out of a live session, not a delta.
	StreamMaxFPS int

	// StreamLeaseTTL is how long the single exclusive view lease survives without the
	// browser asking for another frame. 0 uses DefaultStreamLeaseTTL.
	StreamLeaseTTL time.Duration

	// StreamInput turns on the stream client's opt-in session input (streaminput.go): a
	// viewer holding the view lease may take control of that one account — its bot is
	// stopped and resumed on release — and click to walk and interact. Off by default; off,
	// sessionInput is false and every input route answers 501.
	StreamInput bool
	// Listen is host:port, DefaultListen when empty. A host that is not loopback needs a Token.
	Listen string
	// Token, when set, is required on every request. The caller resolves it from an environment
	// variable or a file; this package only ever sees the value.
	Token string
	// LayoutFile is where the panel layout is saved. Empty keeps the layout in the browser.
	LayoutFile string
	// EventHistory and LogHistory are how many events and log lines are kept for a page that opens
	// later, and MemoryHistory how many memory samples, one per tick. 0 is the default.
	EventHistory  int
	LogHistory    int
	MemoryHistory int

	// SkillIcons is skill display name -> PNG-encoded real in-game icon bytes (see
	// internal/spritepack.SkillIconPNGs), decoded once by the caller (cmd/controlpanel) from a
	// fetched media.jag and handed in here — a static asset independent of any one fleet
	// connection, so it lives on Config rather than Source. Nil (the zero value) is fine and
	// common (a fake fleet, or no media archive available): every skill then falls back to the
	// account.js panel's own monogram tile, no server-side error either way.
	SkillIcons map[string][]byte

	// Prices values Loot panel drops at what they actually trade for on this server, read
	// from the market's own completed trades (see marketprice.go). Nil leaves every drop
	// valued at high alchemy and then the cache's unit cost, which is what it was before --
	// so this is additive, and a fleet that cannot reach the market site is unaffected.
	Prices *PriceBook

	// EngineHTTPBase, when set, points at a LOCAL test engine's HTTP base (e.g.
	// "http://localhost/") whose /debug/tickrate route (local-commit-only on the engine side,
	// see engine's web.ts) this package polls every few seconds to show the lab's current
	// game-tick speed on the dashboard — set live by the ::speed cheat, and easy to forget
	// after ramping it up for a fast local batch test. Empty (the default, and always for a
	// production fleet, which has no such route) just hides that part of the header.
	EngineHTTPBase string

	// MapTilesDir, when set, points at a directory holding a real Leaflet-style
	// {z}/{x}/{y}.png tile pyramid plus a meta.json (see the Engine-TS checkout's
	// tools/pack/map/ExportTiles.ts and TilePyramid.ts, and docs/map_panel_plan_2026-09-16.md
	// Part 5 for exactly how that directory is produced and what meta.json holds — the
	// pixel<->game-tile origin the map.js viewer needs). This is a generated artifact, like
	// internal/locindex's own bake, so it is never committed to this repo; an operator points
	// a running control panel at wherever they generated it. Empty (the default, and always
	// today's production fleet, which has none) falls back to the panel's existing flat
	// overview-image map — nothing about that path changes when this is unset.
	MapTilesDir string

	// StatsStateFile is where accumulated run statistics (see stats.go — total XP/levels
	// gained, deaths, random events cleared/failed, reconnects, uptime, per account and
	// combined) are persisted, so the dashboard's stats panel keeps its lifetime totals
	// across a process restart — the account owner's explicit "always remember previous
	// session data of things that are important" ask. Empty keeps the cache in memory
	// only (still accumulates for as long as the process runs, just not across a
	// restart) — the same "empty disables the file" convention LayoutFile and
	// fleet.Config.BankStateFile already use.
	StatsStateFile string

	// HeatmapFile is where the travel heat map (see heatmap.go) is persisted, so it keeps
	// building across restarts. Empty keeps it in memory only. HeatmapMaxCells bounds its
	// sparse maps (0 means DefaultHeatmapMaxCells); HeatmapOff switches recording off.
	HeatmapFile     string
	HeatmapMaxCells int
	HeatmapOff      bool

	// Locs and LocTypes back /api/poi (bank booths, altars, anvils, ...) — the same static
	// loc index and config archive real bot scripts already query for pathfinding (see e.g.
	// internal/scripts/mulerunner's own locindex.Query{Name: "Bank booth", Op:
	// "Use-quickly"}). An "active only" bake (cmd/locbake's default, no -all) is exactly
	// right here: every fixture category /api/poi looks for is interactive by definition, so
	// nothing real is missing, and it's the same index a real fleet already loads for
	// pathfinding — no separate bake needed for POIs. Both optional; either nil answers
	// /api/poi with an empty list.
	Locs     *locindex.Index
	LocTypes *cachecfg.Config

	// WallLocs backs /api/walls (the minimap-style white-wall/red-door line overlay) — a
	// SEPARATE static loc index that must be baked WITH cmd/locbake's own -all flag (every
	// loc, not just interactive ones): ordinary walls are not interactive, so the
	// active-only index above would only ever reveal doors/gates, never a plain wall. Uses
	// the same LocTypes as Locs above (loc type definitions don't depend on which locs a
	// particular bake happened to keep). Optional; nil answers /api/walls with an empty list.
	WallLocs *locindex.Index

	// BuildSHA/BuildCommitTime are the running binary's own VCS commit info — read via
	// runtime/debug.ReadBuildInfo() by cmd/controlpanel/main.go (Go's own automatic VCS
	// stamping on a `go build` from inside a git checkout — no ldflags plumbing needed)
	// and handed in here, the same "static build-time fact, not owned by any Source"
	// role Config.SkillIcons already plays. Both empty (a stripped/-trimpath build, or a
	// checkout with no .git at all) just hides the build-SHA field of /api/meta and
	// disables UpdateCheck below outright, the same "quietly does less" shape every
	// other optional Config field here has.
	BuildSHA        string
	BuildCommitTime time.Time

	// UpdateCheck configures the optional "does a newer commit exist for this script"
	// panel (see internal/updatewatcher's own package doc comment). This can only ever
	// ANSWER that question — see HOTRELOAD.md and BotControl's own restart-is-not-
	// hotload distinction in source.go: nothing in this codebase can apply a found
	// update without a full binary rebuild and process restart, and the UI text this
	// backs must never imply otherwise. UpdateCheck.Token empty (the default) disables
	// the whole feature outright, same as BuildSHA empty above — there is deliberately
	// no unauthenticated fallback, since the repo this checks is private.
	UpdateCheck UpdateCheckConfig

	// AllowRemoteHost, when true, skips the loopback Host-header check in auth.go's
	// guard.wrap (see that function's own comment on why it exists: defense against DNS
	// rebinding attacks from an arbitrary browser tab). Needed for a reverse proxy or tunnel
	// (e.g. cloudflared's quick-tunnel mode) that forwards a real public hostname's traffic
	// to this process's own loopback listener — from THIS process's point of view that
	// connection still originates from loopback (the tunnel binary is what makes the
	// 127.0.0.1:PORT connection), so Listen itself staying loopback never trips the
	// non-loopback branch below, but every request through the tunnel carries the tunnel's
	// public Host header, which the unmodified Host check would refuse before the token
	// check even runs — see cmd/controlpanel's -allow-remote-host flag. normalised refuses
	// to enable this without a Token: the Host check is what stands in for auth when there
	// is no token, so removing it with nothing in its place would leave the dashboard open
	// to anyone who can reach this port once it is actually tunneled to the internet.
	// Default false; nothing about a normal loopback-only deployment changes unless this is
	// explicitly set.
	AllowRemoteHost bool

	// OnStopAll, when set, is called by POST /api/fleet/stop-all (server.go's
	// handleFleetStopAll) to trigger a graceful WHOLE-PROCESS shutdown — every connected
	// account logged out cleanly, then the process exits. cmd/controlpanel/main.go wires
	// this to the exact same context.CancelFunc its own Ctrl+C handling already cancels
	// (see that command's own doc comment: "Ctrl+C stops the server and logs every
	// account out") so the dashboard button takes the identical clean-shutdown path, not
	// a separate one. This is a WHOLE-FLEET action, distinct from the existing
	// account.js panel's per-account Stop (POST /api/bot/stop, one script) or Disconnect
	// (POST /api/fleet/disconnect, one connection). Nil (the default — every caller of
	// this package before this button existed, and every test harness that does not
	// opt in) leaves the endpoint answering 501, the same "quietly unavailable" shape
	// every other optional Config-backed capability here already has.
	OnStopAll func()

	// OnRestartAll, when set, is called by POST /api/fleet/restart-all (handleFleetRestartAll)
	// to relaunch the fleet on whatever build currently sits next to the running binary.
	// This deliberately does NOT attempt an in-process self-exec/respawn (no
	// os.StartProcess racing this same process to bind its listen port before the old one
	// exits) — cmd/controlpanel/main.go instead shells out to the exact same
	// run/restart-fleet.bat an operator already double-clicks by hand (stop, a 3-minute
	// cooldown against the login server's rate limit, then start-fleet.bat), launched as
	// an independent, detached process that outlives this one, then triggers the same
	// graceful shutdown OnStopAll does. A non-nil returned error means the RELAUNCH ITSELF
	// could not even be triggered (e.g. restart-fleet.bat was not found next to this
	// binary) — the fleet is NOT shut down in that case, since a broken launch with the
	// process already gone would leave nothing running with no way back from the
	// dashboard. Nothing this process can see confirms the NEW instance actually comes
	// up afterwards — the same visibility an operator double-clicking restart-fleet.bat
	// by hand already has, and no less. Nil (the default) leaves the endpoint 501.
	OnRestartAll func() error
}

// UpdateCheckConfig is Config.UpdateCheck — see that field's own doc comment for the
// hard limit on what this can ever do (report only, never apply).
type UpdateCheckConfig struct {
	// Token is a GitHub API token with at least read access to the repo below — in
	// practice `gh auth token` on a machine cmd/controlpanel's operator has already
	// authenticated (see cmd/controlpanel's -github-token-env flag). Empty disables the
	// whole feature: the repo this checks is private, and this package never falls back
	// to an unauthenticated request that would just 404.
	Token string
	// Owner/Repo default to updatewatcher.DefaultOwner/DefaultRepo (this project's own
	// repo) when empty — only worth overriding in a test.
	Owner string
	Repo  string
	// Interval is how often every script is re-checked; updatewatcher.DefaultInterval
	// (5 minutes) when zero. Deliberately minutes, not seconds — see that constant's own
	// doc comment on GitHub's rate limit.
	Interval time.Duration
	// Scripts maps each registered internal/bot script name to the repo-relative paths
	// its own code lives under (see updatewatcher.ScriptSource). Empty disables the
	// feature the same as Token empty — see DefaultUpdateCheckScripts for this build's
	// own mapping of every script internal/bot currently registers.
	Scripts []updatewatcher.ScriptSource
}

// DefaultUpdateCheckScripts is the repo-path mapping for every script internal/bot
// registers as of this change (see internal/bot/registry.go's Register calls) — kept
// here rather than derived automatically because the mapping (which directories make up
// one script's "own code") is a judgment call the registry itself has no way to express.
// A script's own internal/bot/<x>.go adapter is always included; the matching
// internal/scripts/<x>plan and internal/scripts/<x>runner packages are included where
// they exist for that script. Deliberately excludes every script currently being ported
// by other concurrent work (thiever/woodcutter/fighter/flincher and their plan/runner
// packages) — those are not registered in internal/bot yet, so BotScripts() would not
// even offer them for a panel to show a version for; add them here once (and only once)
// they are actually registered.
var DefaultUpdateCheckScripts = []updatewatcher.ScriptSource{
	{Script: "BarbarianAgility", Paths: []string{"internal/bot/barbarian.go", "internal/scripts/barbarianplan", "internal/scripts/barbarianrunner"}},
	{Script: "ProgressiveCrafter", Paths: []string{"internal/bot/crafter.go", "internal/scripts/crafterplan", "internal/scripts/crafterrunner"}},
	{Script: "EssenceMiner", Paths: []string{"internal/bot/essence.go", "internal/bot/essence_supply.go", "internal/scripts/essenceplan", "internal/scripts/essencerunner", "internal/scripts/minemanipplan", "internal/scripts/runecraftrunner"}},
	{Script: "ProgressiveFisher", Paths: []string{"internal/bot/fisher.go", "internal/scripts/fisherplan", "internal/scripts/fisherrunner"}},
	{Script: "MuleReceiver", Paths: []string{"internal/bot/mule.go", "internal/scripts/mulereceiverplan", "internal/scripts/mulerunner"}},
}

// Limits and defaults. Every buffer the control panel holds is sized by one of these.
const (
	// DefaultListen keeps clear of cmd/fleet's dashboard on 8090.
	DefaultListen        = "127.0.0.1:7274"
	DefaultEventHistory  = 256
	DefaultLogHistory    = 256
	MinHistory           = 16
	MaxHistory           = 2048
	DefaultMemoryHistory = 300
	MinMemoryHistory     = 10
	MaxMemoryHistory     = 3600

	// MaxStreams is how many browser tabs may hold a live stream at once.
	MaxStreams = 4
	// DefaultMaxAccounts is MaxAccounts' starting value — see that var's own doc comment.
	DefaultMaxAccounts = 256
	// MaxCounters is how many counters an account shows.
	MaxCounters = 8
	// MaxInventories and MaxInventorySlots bound one /api/inventory answer.
	MaxInventories    = 16
	MaxInventorySlots = 512

	maxNameBytes        = 32
	maxBotBytes         = 48
	maxActivityBytes    = 96
	maxErrorBytes       = 4000
	maxCounterNameBytes = 24
	maxKindBytes        = 24
	maxTextBytes        = 512
	// maxBotStateBytes covers internal/bot.State's longest value ("running", "stopped",
	// "error", "idle") with room to spare.
	maxBotStateBytes = 16
	// maxBotRequestBytes bounds a POST /api/bot/start or /api/bot/stop body, the same order
	// of magnitude as MaxSettingsBytes (one panel's settings) since a start request carries
	// a script's settings object too.
	maxBotRequestBytes = 4 << 10
	// maxConnRequestBytes bounds a POST /api/fleet/disconnect or /api/fleet/connect body —
	// just an account name, so far smaller than maxBotRequestBytes.
	maxConnRequestBytes = 1 << 10
	// maxAddAccountRequestBytes bounds a POST /api/fleet/add body: username, password and
	// a script's settings object, so the same order of magnitude as maxBotRequestBytes plus
	// room for the password.
	maxAddAccountRequestBytes = 4 << 10
	// maxPasswordBytes bounds a password in a POST /api/fleet/add body — generous for any
	// real password without letting the field alone exhaust maxAddAccountRequestBytes.
	maxPasswordBytes = 256
	// maxAccountSettingsBytes bounds AccountStatus.Settings once it is clamped into a
	// status body: large enough for any real script's settings map (see internal/bot/*.go's
	// own doc comments for what a real one looks like), small enough that one misbehaving
	// script can't bloat every /api/status response. A map over the limit is dropped
	// entirely (see clampSettings) rather than truncated key-by-key, since a partial
	// settings map would be actively misleading for the "duplicate this account's
	// settings" feature it exists to support.
	maxAccountSettingsBytes = 4 << 10
)

// options are the knobs tests turn and users do not.
type options struct {
	tick          time.Duration // status and memory sampling while a stream is open
	writeTimeout  time.Duration // a stream write that takes longer ends the stream
	keepAlive     time.Duration // a comment line on an otherwise quiet stream
	queue         int           // event frames buffered per stream before dropping
	maxStreams    int
	sourceBuffer  int // the channel handed to Source.Subscribe
	shutdownGrace time.Duration
	now           func() time.Time
}

func defaultOptions() options {
	return options{
		tick:         time.Second,
		writeTimeout: 10 * time.Second,
		keepAlive:    15 * time.Second,
		// queue was 32 until live-observed (2026-09-16, a real 179-account fleet) to
		// overflow within about a minute of normal play: a busy real fleet's chat/log
		// lines can arrive in bursts well past what one flush-loop pass of handleStream
		// drains before the next one is scheduled. The client now resyncs the gap via
		// GET /api/events when it is told frames were dropped (see store.js's
		// scheduleResync), so this no longer needs to be sized to *never* overflow —
		// just large enough that a normal burst does not need that fallback on every
		// tick under real load.
		queue:         256,
		maxStreams:    MaxStreams,
		sourceBuffer:  256,
		shutdownGrace: 5 * time.Second,
		now:           time.Now,
	}
}

// normalised fills defaults, clamps the history sizes and refuses an exposed listener with no token.
func (c Config) normalised() (Config, error) {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, fmt.Errorf("controlpanel: listen %q: %w", c.Listen, err)
	}
	if !isLoopbackName(host) && c.Token == "" {
		return c, fmt.Errorf("controlpanel: listen %q is reachable from other machines; set an access token first", c.Listen)
	}
	// AllowRemoteHost is a second, IP-independent way this dashboard ends up reachable from
	// the internet (see that field's own doc comment) that the check above cannot catch: it
	// only looks at Listen, which a tunnel never changes. Enforce the same "a token is
	// required before you are reachable from the internet" rule here too.
	if c.AllowRemoteHost && c.Token == "" {
		return c, fmt.Errorf("controlpanel: AllowRemoteHost is set with no Token; a reverse proxy or tunnel would expose this dashboard to the internet with no auth at all — set an access token first")
	}
	c.EventHistory = clampInt(c.EventHistory, DefaultEventHistory, MinHistory, MaxHistory)
	c.LogHistory = clampInt(c.LogHistory, DefaultLogHistory, MinHistory, MaxHistory)
	c.MemoryHistory = clampInt(c.MemoryHistory, DefaultMemoryHistory, MinMemoryHistory, MaxMemoryHistory)
	return c, nil
}

func clampInt(v, def, lo, hi int) int {
	switch {
	case v == 0:
		return def
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// isLoopbackName is true for localhost and loopback IPs. An empty host means every interface.
func isLoopbackName(host string) bool {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
