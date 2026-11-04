// Package controlpanelapp is the control panel program, shared by cmd/controlpanel and cmd/rsb0t: it
// connects the fleet through internal/fleet and serves the panel-based control panel
// (internal/controlpanel) on its own port, apart from cmd/fleet's dashboard. Run is the former main.
//
//	go run ./cmd/controlpanel -fake 8                     a simulated fleet: no game connection at all
//	go run ./cmd/controlpanel -config fleet.config.json   the real roster, in cmd/fleet's config format
//
// Then open http://127.0.0.1:7274/. Ctrl+C stops the server and logs every account out.
package controlpanelapp

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	// Importing internal/bot pulls in every script adapter's own init() (e.g.
	// internal/bot/barbarian.go's Register("BarbarianAgility", ...)) as a side effect,
	// which is what makes bot.Names()/mgr.StartBot below see them at all — the same
	// reason cmd/fleet/main.go imports it. (internal/controlpanel/fleetsource.go now
	// imports internal/bot too, for FleetSource.BotScripts, so this import is already
	// pulled in transitively; kept explicit here since Go dedupes init() by package and
	// this is where the roster actually calls StartBot.)
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	_ "rsb0t-go/internal/bot"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/controlpanel"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/login"
	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/serverprobe"
	"rsb0t-go/internal/spritepack"
	"rsb0t-go/internal/stallwatch"
	"rsb0t-go/internal/updatewatcher"
	"rsb0t-go/internal/worlds"
)

// accountConfig is one roster entry, matching cmd/fleet's own AccountConfig shape exactly so
// the same fleet.config.json roster drives either command. Script/Settings are optional: an
// empty Script just leaves the account connected with no bot attached, startable later from
// the panel's start/stop control.
type accountConfig struct {
	Username string `json:"username"`
	Script   string `json:"script"`
	// World is the game world this account logs in to. Absent or 0 means the fleet's own
	// (whichever world the roster's top-level wsURL names), so every roster written before
	// worlds existed keeps meaning exactly what it meant. A world is purely a different
	// wsURL -- see internal/worlds.
	World    int                    `json:"world"`
	Settings map[string]interface{} `json:"settings"`
}

// roster is the part of fleet.config.json this command reads — cmd/fleet's own FleetConfig in
// full, so a roster with local-engine modulus/exponent and shared Pack/Locs/Cache/Components
// paths (needed for any account whose Script uses them, e.g. a bank) works the same way here.
type roster struct {
	WSURL          string `json:"wsURL"`
	HTTPBase       string `json:"httpBase"`
	SharedPassword string `json:"sharedPassword"`
	// CredentialsFile is an optional JSON object of login name to password (relative to this file),
	// so accounts need not share one password; an account it does not list uses the shared password.
	CredentialsFile   string          `json:"credentialsFile"`
	LoginDelaySeconds float64         `json:"loginDelaySeconds"`
	Accounts          []accountConfig `json:"accounts"`

	// Modulus/Exponent, set (decimal strings), point this at a LOCAL test engine instead of
	// scraping production's rotating key — see fleet.config.example.json's own comment.
	// Never set these to point at example.invalid; leave blank there.
	Modulus  string `json:"modulus"`
	Exponent string `json:"exponent"`

	// PackPath/LocsPath/ConfigPath are the shared, read-only primitives a registered
	// internal/bot script needs, loaded once here and passed to every account's StartBot
	// call — mirroring cmd/fleet's own FleetConfig fields exactly. All optional.
	PackPath   string `json:"packPath"`
	LocsPath   string `json:"locsPath"`
	ConfigPath string `json:"configPath"`

	// Worlds optionally states a world's endpoints by hand:
	//
	//	"worlds": { "2": { "wsURL": "wss://w2.example.invalid/", "httpBase": "https://w2.example.invalid/" } }
	//
	// Unnecessary on the live server, whose hosts are wN.example.invalid and are derived from the
	// top-level wsURL automatically. Present for a deployment whose hosts do not follow
	// that convention, so nothing has to be guessed for real accounts.
	Worlds map[string]worlds.Entry `json:"worlds"`

	// WorldList names the worlds this deployment has, for hosts whose endpoints are
	// derivable and so need no "worlds" entry of their own:
	//
	//	"worldList": [1, 2]
	//
	// The count is configuration, not code: w2 opened on 2026-09-22 and w3 was announced the
	// same day, and adding one is an edit here plus a restart. A world that is listed but not
	// yet running is reported at startup as NOT AVAILABLE and the fleet starts anyway --
	// fleet.ProbeWorlds makes one read-only GET of each world's /crc0 and never logs in.
	WorldList []int `json:"worldList"`

	// AntiCheat false stops the ANTICHEAT side-packets; missing or true sends them as before.
	AntiCheat *bool `json:"antiCheat"`
	// DeviceIDs is the login device id mode: "fixed" (missing or empty: every account sends the
	// web client's constant id) or "per-account" (each account a stable id of its own).
	DeviceIDs string `json:"deviceIds"`
	// BrowserHeaders true opens every game WebSocket with browser-like headers (Origin, a stable
	// per-account User-Agent); missing or false sends the library's defaults.
	BrowserHeaders bool `json:"browserHeaders"`
	// PinRoutes true (with -proxies) ties every account to one route for good, kept in
	// route-pins.json beside this file, so each account always logs in from the same address;
	// new accounts go to the route with the most room. Missing or false: first route with room.
	PinRoutes bool `json:"pinRoutes"`
	// CRCTable, when it holds the nine archive checksums (the first 36 bytes of the server's
	// /crc0), is sent at every login instead of fetching /crc0. Empty fetches, as before.
	CRCTable []int64 `json:"crcTable"`
}

// checksums returns the roster's crcTable as login checksums, nil when it is empty.
func (r *roster) checksums() (*[9]int32, error) {
	if len(r.CRCTable) == 0 {
		return nil, nil
	}
	if len(r.CRCTable) != 9 {
		return nil, fmt.Errorf("crcTable wants the 9 archive checksums or none, got %d", len(r.CRCTable))
	}
	var out [9]int32
	for i, v := range r.CRCTable {
		if v < -1<<31 || v > 1<<32-1 {
			return nil, fmt.Errorf("crcTable[%d] wants a 32-bit checksum, got %d", i, v)
		}
		out[i] = int32(uint32(v))
	}
	return &out, nil
}

// Run parses the command line (the process's flags, on flag.CommandLine) and serves until Ctrl+C.
func Run() {
	configPath := flag.String("config", "fleet.config.json", "the fleet roster, in cmd/fleet's format")
	listen := flag.String("listen", controlpanel.DefaultListen, "host:port to serve on (cmd/fleet's dashboard keeps 8090)")
	count := flag.Int("count", 0, "how many roster accounts to connect, in order; 0 is all")
	fake := flag.Int("fake", 0, "serve a simulated fleet of this many accounts and connect nothing")
	layout := flag.String("layout", "", "file the panel layout is saved to; empty keeps it in the browser")
	bankState := flag.String("bank-state", "", "file each account's last-known bank contents is saved to, so the bank panel survives a disconnect/restart; empty keeps it in memory only (survives a disconnect/bot-stop, not a process restart)")
	tokenEnv := flag.String("token-env", "", "environment variable holding an access token, needed to listen beyond loopback")
	passwordEnv := flag.String("password-env", "RSB0T_FLEET_PASSWORD", "environment variable that overrides the config's sharedPassword")
	loginDelay := flag.Duration("login-delay", -1, "pause between logins; 0 connects every account at once (default: the config's loginDelaySeconds, else 3s)")
	eventHistory := flag.Int("event-history", controlpanel.DefaultEventHistory, "events kept for a page opened later")
	logHistory := flag.Int("log-history", controlpanel.DefaultLogHistory, "log lines kept for a page opened later")
	memoryHistory := flag.Int("memory-history", controlpanel.DefaultMemoryHistory, "memory samples kept, one a second")
	maxAccounts := flag.Int("max-accounts", controlpanel.DefaultMaxAccounts, "how many accounts one /api/status body lists in full; the rest are still counted (see status.counts), just not listed — raise this for a local dev-lab account ramp you actually want to see all of at once")
	marketPrices := flag.Bool("market-prices", false, "(only for a server with the same market pages as the original one) value the Loot panel's drops at what they actually trade for, read from the original server's completed-trades page, which other servers do not have (see internal/controlpanel/marketprice.go). The table is the median traded price per item over ~1,300 recent trades, refreshed at most every 30 minutes and fetched lazily, so a fleet that never opens the Loot panel never fetches it. Off, or with the site unreachable, every drop falls back to high alchemy and then the cache's own unit cost -- which is what the panel did before this existed, so turning it off costs only realism, never correctness")
	mediaPath := flag.String("media", "", "path to a saved media.jag (fetch with GET /media<crc>, crc = index 4 of /crc0 — see cmd/spritecheck) for real in-game skill icons; skills this build has no confirmed frame for still fall back to the page's own tile, and omitting this flag entirely falls back for every skill")
	perWorldLogins := flag.Bool("per-world-login-pacing", false, "EXPERIMENTAL, AND DO NOT PASS THIS UNTIL THE EXPERIMENT IN docs/multiworld.md HAS BEEN RUN: give each world its own login-rate budget instead of sharing one, so worlds fill side by side. The engine's own source says the rate counter is per world node (World.ts's loginDeviceAttempts, keyed by uid@address and held in memory ON the node), which would make this free parallelism -- but the live server runs a modified login server whose per-address CONNECTION limit is not in that source at all, so the rate budget may in fact be shared. If it is, this doubles the fleet's attempt rate against one budget, trips login response 16, and halts EVERY login fleet-wide for two minutes at a time, extending on each further rejection. Off is today's behaviour exactly")
	localRescue := flag.Bool("local-rescue-teleport", false, "LOCAL TEST ENGINE ONLY: when freshstartrunner's own stuck-detection reports an account STRANDED in a sealed pocket (e.g. a random-event fail-teleport destination with no walkable exit), send a real ::tele cheat back to Lumbridge instead of just reporting it, so an unattended local soak keeps running. No-op against production — there is no ::tele cheat there for any non-staff login — but off by default regardless; only pass this when pointed at a local Lost City engine you control")
	enableCheats := flag.Bool("enable-cheats", false, "LOCAL TEST ENGINE ONLY: allow internal/actions.Cheat to actually send the CLIENT_CHEAT packet (::tele/::give/etc, used by every random-event/combat-flee recovery in internal/randomevents). Off by default so a misconfigured run against production can never transmit this opcode — a real client only ever sends it from a staff-only admin entry a production account can't reach, so a non-staff connection sending it at all is itself a bot-detection signal, independent of whether the engine honors it. Only pass this against a local Lost City engine you control")
	pprofListen := flag.String("pprof", "", "DIAGNOSTIC ONLY, off by default: host:port to serve net/http/pprof on (e.g. 127.0.0.1:6060), for `go tool pprof http://<addr>/debug/pprof/profile` or /debug/pprof/goroutine. Empty (the default) starts nothing — this process's normal behavior, resource use and every existing endpoint are completely unchanged unless this flag is set. Bind to loopback only; this exposes no auth of its own")
	mapTiles := flag.String("maptiles", "", "directory holding a real {z}/{x}/{y}.png map tile pyramid plus meta.json (see the Engine-TS checkout's tools/pack/map/ExportTiles.ts + TilePyramid.ts, and docs/map_panel_plan_2026-09-16.md) — served at /maptiles/ for the map panel's real slippy-map view. Empty (the default) keeps the panel's existing flat-overview-image map, unchanged")
	heatmapFile := flag.String("heatmap-file", controlpanel.DefaultHeatmapFile, "file the travel heat map (where accounts have spent their time; see the Travel heat map panel) is saved to every two minutes and on shutdown, and loaded from at startup, so it keeps building across restarts. Relative to the working directory. Empty keeps it in memory only. Under -fake the default is heatmap-fake.json.gz, so synthetic walks never mix with a real fleet's file")
	heatmapMaxCells := flag.Int("heatmap-max-cells", controlpanel.DefaultHeatmapMaxCells, "cap on the travel heat map's sparse entries across every account and time bucket; past it cells are coarsened to 2x2, 4x4 then 8x8 tiles, then no new cells are added")
	heatmapOff := flag.Bool("heatmap-off", false, "do not record the travel heat map at all")
	statsState := flag.String("stats-state", controlpanel.DefaultStatsFile, "file accumulated run statistics (total XP/levels gained, deaths, random events cleared/failed, reconnects, uptime, per account and combined — see the Stats panel) are saved to, so lifetime totals survive a process restart; also holds the Loot panel's lifetime totals. Relative to the working directory; the default is stats-state.json (stats-fake.json under -fake, so a simulated fleet never mixes with a real one). Empty keeps them in memory only (still accumulate for this process's own lifetime, just not across a restart)")
	muleLedgerFile := flag.String("mule-ledger", controlpanel.DefaultMuleLedgerFile, "file every mule's finished trades (when, mule, partner, items in and out, outcome) are appended to as JSON lines, bounded to the newest forty thousand (about eleven days), and loaded from at startup; served at /api/mule-ledger for the mule dashboard. Relative to the working directory. Empty keeps the record in memory only. Unused under -fake, which simulates its own")
	mapLocs := flag.String("map-locs", "", "static loc index (cmd/locbake's own output, active-only is fine) for the map panel's bank/altar/anvil/furnace/range POI markers (/api/poi). Empty falls back to -config's own locsPath when a roster is loaded (the same index already used for pathfinding); still empty after that just answers /api/poi with an empty list")
	mapLocsAll := flag.String("map-locs-all", "", "a SEPARATE static loc index baked with cmd/locbake's -all flag (every loc, not just interactive ones) for the map panel's wall/door line overlay (/api/walls) — ordinary walls are not interactive, so -map-locs/the roster's own active-only index can supply doors/gates but never plain walls. Empty answers /api/walls with an empty list")
	mapConfig := flag.String("map-config", "", "config archive for resolving loc type names/ops for -map-locs/-map-locs-all above. Empty falls back to -config's own configPath when a roster is loaded; still empty after that disables both /api/poi and /api/walls")
	discordWebhookEnv := flag.String("discord-webhook-env", "", "environment variable holding a Discord incoming-webhook URL (same convention as -token-env: the secret never appears on the command line or in `ps`). When set, this process posts account-death, STRANDED-escalation and hourly summary alerts to that webhook (see internal/controlpanel/discord.go), rate-limited so one flaky account cannot flood the channel. Empty (the default) posts nothing — no behavior change")
	allowRemoteHost := flag.Bool("allow-remote-host", false, "REMOTE ACCESS ONLY, off by default: skip the dashboard's loopback Host-header check (see internal/controlpanel/auth.go's guard.wrap) so a reverse proxy or tunnel (e.g. cloudflared) forwarding a public hostname's traffic to this process's own loopback listener is not refused before the token check even runs. Requires -token-env: this flag refuses to start without a token, since disabling the Host check with no token would leave the dashboard open to anyone who can reach this port. Leave this off for a normal local-only deployment; nothing about that path changes")
	updateChecks := flag.Bool("update-checks", false, "ask GitHub, on a timer, whether each script's own source has a newer commit than this build (the per-script update panel; see -github-token-env). Off: no GitHub request and no token is read")
	githubTokenEnv := flag.String("github-token-env", "", "environment variable holding a GitHub API token (repo-scoped) for the per-script \"is a newer commit available\" panel (see internal/updatewatcher) — same never-on-the-command-line convention as -token-env/-discord-webhook-env. Empty checks GITHUB_TOKEN and GH_TOKEN, then falls back to `gh auth token` (this machine's own already-authenticated gh CLI, per the account owner's own setup) as a last resort. Still no token after all three disables the panel outright — it can only ever ANSWER whether a script's own paths in oldm8clint/rsb0t-go have a newer commit than this build; see BotControl's own restart-vs-hotload distinction for why nothing acts on that answer without a full rebuild and restart")
	updateCheckInterval := flag.Duration("update-check-interval", updatewatcher.DefaultInterval, "how often each script's own source paths are re-checked against GitHub for a newer commit (see -github-token-env) — minutes, not seconds: this hits a real API with real rate limits")
	fleetControlDir := flag.String("fleet-control-dir", "", "directory holding restart-fleet.bat for the dashboard's own whole-fleet Restart button (POST /api/fleet/restart-all — see run/README.txt and restartFleet's own doc comment below). Empty (the default) resolves to the directory this running executable itself sits in, matching how run/start-fleet.bat already expects controlpanel_live.exe and every run/*.bat script to sit side by side; a `go run`/`go test` build (no fixed on-disk exe path worth trusting) just makes the Restart button clearly report the script was not found rather than guessing. The whole-fleet Stop button (POST /api/fleet/stop-all) needs no directory at all — it is pure in-process graceful shutdown, the same path Ctrl+C already takes")
	captureCache := flag.String("capture-cache", "", "optional trusted packed game cache directory for on-demand full graphics")
	captureRenderer := flag.String("capture-renderer", "", "optional browser game renderer bundle built with run/build-capture-renderer.ps1")
	streamClient := flag.String("stream-client", "", "OPTIONAL, off by default: path to ONE installed stream client provider ES module, served at the fixed same-origin path /stream/client.js for the dashboard's Stream client panel (see docs/stream_client_panel.md for the provider contract, and tools/stream/tile-client.js for the reference provider shipped in this repo). The file MUST have a \"<path>.manifest.json\" sidecar declaring the renderer identity it implements; a missing, malformed or unknown-identity manifest refuses to start rather than quietly leaving the panel disabled with no explanation. Empty (the default) installs nothing: /stream/client.js answers 501, /api/meta reports no renderer, and the panel stays plainly disabled. Installing a provider does NOT enable input — session input is advertised only when a Source implements StreamInputSink, and nothing in this repository does, so the input control stays disabled and POST /api/stream/input answers 501")
	eraAssets := flag.String("era-assets", "", "OPTIONAL, off by default: a directory of game assets for eras other than this fleet's own 2004 one, with one subdirectory per era (classic/, 2009/). Assets are NEVER committed to this repository — this is the same arrangement -capture-cache already uses for the 2004 cache, for the same reasons: size (a 2009 filestore is ~86 MB), and the fact that the upstream projects publishing these caches license their own code and do not own the game art. What is found here is MEASURED at startup (entry counts for Classic's .jag containers, JS5 index entry counts for 2009) and an era skin is offered only when the installed provider declares it AND its assets are really present; a directory holding no usable era asset set is a startup error naming what was looked for. Empty (the default, and every production fleet) means no non-2004 era skin is offerable")
	streamInput := flag.Bool("stream-input", false, "OPTIONAL, off by default: let the stream client viewer take control of the one account it is viewing (its bot is stopped, and resumed on release) and click to walk and interact through that account's existing session. Refused while a bank/trade is open or for MuleReceiver unless forced; every action is validated and audit-logged (GET /api/stream/audit). Off: sessionInput stays false and input answers 501")
	streamMaxFPS := flag.Int("stream-max-fps", 0, "how often the stream client's existing-session bridge will take a fresh observation, whatever frame rate the browser asks for. 0 uses the built-in default (see internal/controlpanel.DefaultStreamMaxFPS). Read that constant's doc comment before raising this: one frame is a whole gamestate.RenderSnapshot — every nearby npc, player and loc plus the full interface state — deep-copied out of a live session and JSON-encoded, not a delta, so this is a real cost on the process running the whole fleet")
	scriptAssignmentsPath := flag.String("script-assignments", controlpanel.DefaultScriptAssignmentsFile, "file a script started from the panel (POST /api/bot/start) is saved to, with its settings, so the NEXT process start runs it again instead of the roster's script -- a switch then survives a restart without anyone editing the roster. Relative to the working directory (run/ for the live fleet); script-assignments-fake.json under -fake. Starting an account back on its roster script with settings omitted, or DELETE /api/assignments?account=, removes its entry; GET /api/assignments lists them and /api/status names each account's assignedScript and rosterScript. Empty, off or false saves nothing and starts every account from the roster, exactly as before this existed. The file holds account names, script names and settings, never a credential")
	proxiesPath := flag.String("proxies", "", "OPTIONAL, off by default: a proxy pool file (see internal/fleet/routeconfig.go for the format; keep it outside the repo or name it proxies*.json, which is gitignored). With it, accounts log in from the home address first (up to its max, default 89) and then through each enabled proxy route in order, never past a route's max, with login pacing kept per route; accounts that fit nowhere wait queued. Empty (the default) keeps the single direct route with no cap, exactly as before. The file's passwords are never printed")
	watchDefaults := fleet.DefaultWatchdogConfig()
	watchdogMode := flag.String("watchdog", string(watchDefaults.Mode), "the fleet STALL WATCHDOG (internal/fleet/watchdog.go, decision in internal/stallwatch): observe (the default: detect, diagnose and report what it would do, act on nothing), nudge (press a continue / close a modal, then report), act (nudge -> restart -> relogin -> needs attention; an account whose backpack/worn panels are unreadable outside any event skips the restart, which cannot fix that) or off. The mode can be changed at runtime, no restart, with POST /api/watchdog {\"mode\":\"act\"} (GET /api/watchdog reads it and the counts); that change is not saved, so the next start uses this flag again. In nudge or act it acts only on a running bot that has been BOTH silent and without progress (tile, XP, pack/worn/bank, world, hitpoints rising) for -watchdog-stall, never on one that is still logging what it waits for; one ladder at a time per account, at most 3 per 6h, at most -watchdog-max-actions per 10 minutes fleet-wide, 2 relogins at once, every relogin through the ordinary login pacing; nothing for a script while max(5, 3%) of its accounts are stalled at once, and nothing at all when that looks fleet-wide; accounts it has already given up on do not count toward those breakers")
	watchdogStall := flag.Duration("watchdog-stall", watchDefaults.StallAfter, "how long a running bot must be silent AND make no progress before the watchdog counts it stalled. The default is the longest silent wait the code is known to allow (stallwatch.KnownSilentWaits: a donor waiting out donorplan.MeetTimeoutComing, 10 minutes, at the mule's meet tile without a line) plus a 5-minute margin; a lower value logs a warning at start, because an account doing exactly that wait would be judged stalled")
	watchdogBlind := flag.Duration("watchdog-blind", watchDefaults.BlindAfter, "how long a running bot may be BLIND (backpack/worn panels unreadable with the tile known, outside the mime/maze squares, Guardian idle) with no progress before the watchdog counts it stalled even though it is still logging -- the woodcutter family's blind gate logs, and no wait ends blindness; its ladder is nudge (if something is open) -> relogin -> needs attention. 0 turns this path off")
	watchdogRecheck := flag.Duration("watchdog-recheck", watchDefaults.RecheckAfter, "the least time between two watchdog steps on one account")
	watchdogExempt := flag.String("watchdog-exempt", "MuleReceiver", "comma-separated script and account names the watchdog never judges STALLED or acts on for a stall (a mule's whole job is waiting at its post). It does not exempt an account from the safety net below: a bot left stopped is started again whatever it runs")
	watchdogOrphan := flag.Duration("watchdog-orphan", watchDefaults.OrphanAfter, "the watchdog's SAFETY NET (internal/stallwatch/orphan.go; act mode starts, observe and nudge only report): an account in game whose bot has been stopped this long, and was NOT stopped by an operator (the panel's stop, a disconnect, a stream take-over), by the script ending itself, or by a hop that owns the account, is started again with the script and settings it already had -- at most 3 times per account per 6h and 3 per sweep. 18 Oct 2026: a watchdog restart abandoned a slow-to-stop bot and the account stood in game with no bot for eight hours. The clock restarts at each login. 0 turns the net off")
	watchdogMaxActions := flag.Int("watchdog-max-actions", watchDefaults.FleetActions, "the most watchdog interventions (nudge, restart, relogin) fleet-wide in any 10 minutes")
	watchdogDumpDir := flag.String("watchdog-dump-dir", watchDefaults.DumpDir, "directory (relative to the working directory, so run/logs/ for the live fleet) for the one goroutine dump written when a stall is first seen, at most one per 10 minutes and 20 per process; empty writes none")
	flag.Parse()
	watchMode, ok := stallwatch.ParseMode(*watchdogMode)
	if !ok {
		log.Fatalf("controlpanel: -watchdog must be act, nudge, observe or off, not %q", *watchdogMode)
	}
	if *fake > 0 {
		explicit := false
		flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "heatmap-file" })
		if !explicit {
			*heatmapFile = "heatmap-fake.json.gz"
		}
		explicit = false
		flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "stats-state" })
		if !explicit {
			*statsState = "stats-fake.json"
		}
		explicit = false
		flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "script-assignments" })
		if !explicit {
			*scriptAssignmentsPath = controlpanel.DefaultFakeScriptAssignmentsFile
		}
	}
	if *localRescue {
		freshstartrunner.LocalRescueTeleport = true
	}
	if *enableCheats {
		actions.CheatEnabled = true
	}
	if *maxAccounts > 0 {
		controlpanel.MaxAccounts = *maxAccounts
	}

	cfg := controlpanel.Config{
		CaptureCacheDir: *captureCache,
		CaptureRenderer: *captureRenderer,
		// Stream client: nothing is installed unless -stream-client points at a real
		// provider module. controlpanel.Start fails loudly on a bundle whose manifest is
		// missing or declares a renderer identity this build does not know.
		StreamClientBundle: *streamClient,
		EraAssets:          *eraAssets,
		StreamMaxFPS:       *streamMaxFPS,
		StreamInput:        *streamInput,
		Enabled:            true,
		Listen:             *listen,
		LayoutFile:         *layout,
		EventHistory:       *eventHistory,
		LogHistory:         *logHistory,
		MemoryHistory:      *memoryHistory,
		MapTilesDir:        *mapTiles,
		AllowRemoteHost:    *allowRemoteHost,
		StatsStateFile:     *statsState,
		HeatmapFile:        *heatmapFile,
		HeatmapMaxCells:    *heatmapMaxCells,
		HeatmapOff:         *heatmapOff,
	}

	// BuildSHA/BuildCommitTime back /api/meta's buildSha field and the per-script
	// update-check panel below — see Config.BuildSHA's own doc comment for why this
	// reads Go's own automatic VCS stamping (runtime/debug.ReadBuildInfo) rather than
	// needing an -ldflags build step: a plain `go build` from inside this git checkout
	// already carries vcs.revision/vcs.time, confirmed live (2026-09-16) against this
	// exact checkout. `go run`, a build with -trimpath, or one from an exported tarball
	// with no .git at all just leaves both empty — the buildSha field and the whole
	// update-check panel quietly disappear rather than showing a stale or fabricated
	// value.
	if info, ok := debug.ReadBuildInfo(); ok {
		var modified bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				cfg.BuildSHA = s.Value
			case "vcs.time":
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					cfg.BuildCommitTime = t
				}
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if cfg.BuildSHA != "" {
			dirty := ""
			if modified {
				dirty = " (with uncommitted changes — the update check below compares against the LAST COMMIT, which may be behind what is actually running)"
			}
			log.Printf("controlpanel: build %s, committed %s%s", cfg.BuildSHA, cfg.BuildCommitTime.Format(time.RFC3339), dirty)
		} else {
			log.Println("controlpanel: no VCS build info available (go run, -trimpath, or no .git checkout) — /api/meta's buildSha and the per-script update-check panel are both disabled")
		}
	}

	// UpdateCheck: the per-script "is a newer commit available" panel (see
	// internal/updatewatcher's own doc comment for exactly what this can and cannot do
	// — it only ever answers the question; applying a fix still needs a full rebuild
	// and restart). Needs both a build SHA/time to compare against (above) and a GitHub
	// token for the private repo; either missing just leaves the panel disabled, same
	// as every other optional Config field cmd/controlpanel fills in.
	if !*updateChecks {
		log.Println("controlpanel: per-script update checks are off (-update-checks=false)")
	} else if cfg.BuildSHA != "" && !cfg.BuildCommitTime.IsZero() {
		if token := resolveGitHubToken(*githubTokenEnv); token != "" {
			cfg.UpdateCheck = controlpanel.UpdateCheckConfig{
				Token:    token,
				Interval: *updateCheckInterval,
				Scripts:  controlpanel.DefaultUpdateCheckScripts,
			}
			log.Printf("controlpanel: per-script update checks enabled against %s/%s for %d script(s), every %s", updatewatcher.DefaultOwner, updatewatcher.DefaultRepo, len(controlpanel.DefaultUpdateCheckScripts), *updateCheckInterval)
		} else {
			log.Println("controlpanel: no GitHub token found (-github-token-env, GITHUB_TOKEN, GH_TOKEN, or `gh auth token`) — the per-script update-check panel is disabled")
		}
	}
	if *statsState != "" {
		log.Printf("controlpanel: persisting accumulated run statistics to %s", *statsState)
	}
	switch {
	case *heatmapOff:
		log.Printf("controlpanel: travel heat map off (-heatmap-off)")
	case *heatmapFile != "":
		log.Printf("controlpanel: travel heat map saved to %s (cap %d cells)", *heatmapFile, *heatmapMaxCells)
	default:
		log.Printf("controlpanel: travel heat map kept in memory only (-heatmap-file is empty)")
	}
	if *streamClient != "" {
		// Deliberately says what is and is not enabled: installing a provider enables a
		// VIEW of an already-connected session and never a second login; input is a
		// separate, off-by-default flag and the line below names which way it went.
		input := "observation only — session input stays disabled (no -stream-input)"
		if *streamInput {
			// -stream-input alone is not enough: the Source must also be able to run an
			// action on the session and stop/start its bot, which only the real fleet
			// Source can. /api/meta.streamClient.sessionInput reports what actually
			// resulted; this line reports what was asked for.
			input = "session input REQUESTED (-stream-input): a viewer holding the lease can stop this account's bot and act as it"
		}
		log.Printf("controlpanel: installing the stream client provider at %s (served as /stream/client.js); %s", *streamClient, input)
	}
	if *mapTiles != "" {
		if _, err := os.Stat(*mapTiles); err != nil {
			log.Fatalf("controlpanel: -maptiles %s: %v", *mapTiles, err)
		}
		log.Printf("controlpanel: serving real map tiles from %s at /maptiles/", *mapTiles)
	}
	if *mapConfig != "" {
		cc, err := cachecfg.LoadFile(*mapConfig)
		if err != nil {
			log.Fatalf("controlpanel: -map-config %s: %v", *mapConfig, err)
		}
		cfg.LocTypes = cc
		log.Printf("controlpanel: loaded map config archive from %s", *mapConfig)
	}
	if *mapLocs != "" {
		ix, err := locindex.Load(*mapLocs)
		if err != nil {
			log.Fatalf("controlpanel: -map-locs %s: %v", *mapLocs, err)
		}
		cfg.Locs = ix
		log.Printf("controlpanel: loaded %d locs from %s for /api/poi", ix.Count(), *mapLocs)
	}
	if *mapLocsAll != "" {
		ix, err := locindex.Load(*mapLocsAll)
		if err != nil {
			log.Fatalf("controlpanel: -map-locs-all %s: %v", *mapLocsAll, err)
		}
		cfg.WallLocs = ix
		log.Printf("controlpanel: loaded %d locs from %s for /api/walls", ix.Count(), *mapLocsAll)
	}
	if *tokenEnv != "" {
		cfg.Token = os.Getenv(*tokenEnv)
		if cfg.Token == "" {
			log.Fatalf("controlpanel: environment variable %s is unset or empty", *tokenEnv)
		}
	}
	if *allowRemoteHost && cfg.Token == "" {
		log.Fatalf("controlpanel: -allow-remote-host needs -token-env too — a tunnel or reverse proxy would otherwise expose this dashboard to the internet with no auth at all")
	}
	discord := &controlpanel.DiscordClient{}
	if *discordWebhookEnv != "" {
		discord.WebhookURL = os.Getenv(*discordWebhookEnv)
		if discord.WebhookURL == "" {
			log.Fatalf("controlpanel: environment variable %s is unset or empty", *discordWebhookEnv)
		}
		log.Printf("controlpanel: Discord alerts enabled (deaths, STRANDED escalations, hourly summary)")
	}
	// Best-effort: if this process panics anywhere in main's own goroutine from here on, post
	// a notice before it actually crashes, then re-panic so exit code/behavior are unchanged.
	// Deliberately scoped to this goroutine only (Go's own recover semantics: a panic in a
	// different goroutine, e.g. inside internal/fleet's own per-session goroutines, is not
	// caught here) — a process-wide panic hook would need a much broader change than this
	// task's remote-access/alerting scope calls for.
	defer func() {
		if r := recover(); r != nil {
			discord.AlertPanic(r)
			panic(r)
		}
	}()
	if *mediaPath != "" {
		data, err := os.ReadFile(*mediaPath)
		if err != nil {
			log.Fatalf("controlpanel: reading media archive: %v", err)
		}
		media, err := archive.ParseJagFile(data)
		if err != nil {
			log.Fatalf("controlpanel: parsing media archive: %v", err)
		}
		icons, err := spritepack.SkillIconPNGs(media)
		if err != nil {
			log.Printf("controlpanel: decoding skill icons (skills with no confirmed frame still fall back to a tile): %v", err)
		}
		cfg.SkillIcons = icons
		log.Printf("controlpanel: loaded %d real skill icons from %s (of %d this build knows a frame for)", len(icons), *mediaPath, len(spritepack.SkillIconNames()))
	}

	// Loot values come from the market's own completed trades when this is on. Construction
	// fetches nothing: the table loads lazily on first use and refreshes at most every 30
	// minutes, so a fleet that never opens the Loot panel never touches the site.
	if *marketPrices {
		cfg.Prices = controlpanel.NewPriceBook(nil, controlpanel.DefaultTradePages)
		log.Printf("controlpanel: Loot values use real market prices (median of every completed trade the market lists, up to %d pages), falling back to high alchemy then the cache cost", controlpanel.DefaultTradePages)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Wire the dashboard's own whole-fleet Stop/Restart buttons (internal/controlpanel
	// /server.go's handleFleetStopAll/handleFleetRestartAll) to real process control:
	//   - Stop calls this exact same stop() CancelFunc Ctrl+C already triggers — see this
	//     function's own `<-ctx.Done()` branch further down (mgr.CloseAll(), then
	//     `<-srv.Done()`) — so the button takes the identical clean-shutdown path, not a
	//     separate one, matching this command's own doc comment at the top of this file.
	//   - Restart shells out to run/restart-fleet.bat (stop, a 3-minute cooldown against
	//     the login server's rate limit, then start-fleet.bat on whatever build is
	//     currently built) as an independent, detached process, THEN calls stop() the
	//     same way Stop does — see restartFleet's own doc comment below for why an
	//     in-process self-exec/respawn (this process racing a freshly `os.StartProcess`'d
	//     copy of itself to bind the same listen port before it exits) was deliberately
	//     rejected in favour of reusing this already-proven external launcher instead.
	cfg.OnStopAll = stop
	fcDir := *fleetControlDir
	if fcDir == "" {
		fcDir = defaultFleetControlDir()
	}
	cfg.OnRestartAll = func() error {
		if err := restartFleet(fcDir); err != nil {
			return err
		}
		log.Println("controlpanel: restart-fleet.bat launched; stopping now (every account logging out) so it can relaunch on a clean port")
		go stop()
		return nil
	}

	if *pprofListen != "" {
		startPprof(*pprofListen)
	}

	assignments := openScriptAssignments(*scriptAssignmentsPath)

	if *fake > 0 {
		src := controlpanel.NewFakeSource(*fake, time.Now().UnixNano())
		if assignments.Enabled() {
			src.SetScriptAssignments(assignments)
			for _, line := range src.ApplyScriptAssignments() {
				log.Printf("controlpanel: %s", line)
			}
		}
		go src.Run(ctx, 500*time.Millisecond)
		srv := serve(ctx, cfg, src)
		go discord.Watch(ctx, src)
		log.Printf("controlpanel: simulating %d accounts; nothing connects to a game server", *fake)
		<-srv.Done()
		return
	}

	r, err := loadRoster(*configPath)
	if err != nil {
		log.Fatalf("controlpanel: %v", err)
	}
	if strings.TrimSpace(r.WSURL) == "" || strings.TrimSpace(r.HTTPBase) == "" {
		log.Fatalf("controlpanel: %s names no game server (wsURL and httpBase are empty): choose one with `rsb0t server <address>` or on the `rsb0t setup` page", *configPath)
	}
	// Every URL below is built as httpBase + "crc0" and the like, so the base must end in "/".
	if !strings.HasSuffix(r.HTTPBase, "/") {
		r.HTTPBase += "/"
	}
	if r.Modulus != "" && serverprobe.IsLocal(r.HTTPBase) {
		// r.Modulus set is this project's own existing marker for "a local test engine, not
		// production" (see the modulus/exponent branch below) — reuse it here so the
		// dashboard's tick-rate display only ever tries a local engine's own debug route,
		// never example.invalid. A key pasted for a public server (rsb0t server -modulus) sets it
		// too, so the host must also be this machine, a private address or a test name:
		// nothing polls a stranger's server every few seconds.
		cfg.EngineHTTPBase = r.HTTPBase
	}
	password := os.Getenv(*passwordEnv)
	if password == "" {
		password = r.SharedPassword
	}
	creds, err := loadCredentials(*configPath, r.CredentialsFile)
	if err != nil {
		log.Fatalf("controlpanel: %s: %v", *configPath, err)
	}
	if len(r.Accounts) == 0 {
		log.Fatalf("controlpanel: %s needs at least one account", *configPath)
	}
	if who := missingPassword(r.Accounts, creds, password); who != "" {
		log.Fatalf("controlpanel: %s: no password for %q: give it a credentialsFile entry, or set %s or sharedPassword", *configPath, who, *passwordEnv)
	}
	n := *count
	if n <= 0 || n > len(r.Accounts) {
		n = len(r.Accounts)
	}
	accounts := r.Accounts[:n]
	delay := *loginDelay
	if delay < 0 {
		delay = time.Duration(r.LoginDelaySeconds * float64(time.Second))
		if delay <= 0 {
			delay = 3 * time.Second
		}
	}

	var mod, exp *big.Int
	if r.Modulus != "" {
		var ok bool
		mod, ok = new(big.Int).SetString(r.Modulus, 10)
		if !ok {
			log.Fatalf("controlpanel: %s: modulus is not a valid decimal integer", *configPath)
		}
		expStr := r.Exponent
		if expStr == "" {
			expStr = "65537"
		}
		exp, ok = new(big.Int).SetString(expStr, 10)
		if !ok {
			log.Fatalf("controlpanel: %s: exponent is not a valid decimal integer", *configPath)
		}
		log.Println("controlpanel: using the modulus/exponent from the roster (local engine mode) — not reading the server's client.js")
	} else {
		log.Println("controlpanel: reading the server's RSA login key from its web client")
		var err error
		mod, exp, err = fleet.FetchLoginKey(r.HTTPBase)
		if err != nil {
			log.Fatalf("controlpanel: could not read the server's login key from %sclient/client.js: %v\n"+
				"  Check the server with `rsb0t check-server <address>`. If it is online but its key cannot be found, paste the key: "+
				"`rsb0t server <address> -modulus <number> -exponent <number>`, or the key boxes on the `rsb0t setup` page.", r.HTTPBase, err)
		}
	}

	// Worlds. w2.example.invalid opened on 2026-09-22 and is reached by dialling its own wsURL and
	// nothing else -- no per-world CRC table and no per-world key (both measured byte-identical
	// to w1's). An account names its world with "world": N in its roster entry; the optional
	// top-level "worlds" object states a world's endpoints by hand where they cannot be derived
	// from this roster's own wsURL. A roster that mentions neither resolves every account to
	// exactly the endpoints it always used.
	worldEntries, err := worlds.ParseEntries(r.Worlds)
	if err != nil {
		log.Fatalf("controlpanel: %s: %v", *configPath, err)
	}
	worldMap, err := worlds.New(r.WSURL, r.HTTPBase, worldEntries)
	if err != nil {
		log.Fatalf("controlpanel: %s: %v", *configPath, err)
	}
	if err := worldMap.List(r.WorldList); err != nil {
		log.Fatalf("controlpanel: %s: %v", *configPath, err)
	}
	// Say which worlds are actually there before connecting anything. One read-only GET of
	// each world's /crc0 -- no login, no game socket, no credentials -- so an announced but
	// unlaunched world (w3, 2026-09-22) is a clear startup line rather than a fleet that
	// refuses to start or a batch of accounts that silently never connect.
	if len(worldMap.Available()) > 1 {
		for _, line := range fleet.DescribeWorlds(fleet.ProbeWorlds(worldMap)) {
			log.Printf("controlpanel: %s", line)
		}
	}

	fleetCfg := fleet.Config{WSURL: r.WSURL, HTTPBase: r.HTTPBase, Modulus: mod, Exponent: exp, BankStateFile: *bankState, Worlds: worldMap}
	if r.Modulus == "" {
		// Production scrapes the key out of the live client bundle, and the live server rotates
		// its keypair on every server restart — so the fleet must be able to go back for
		// it rather than signing forever with the one it happened to start with. Only
		// consulted after a login is rejected as out of date; see fleet/modulus.go.
		fleetCfg.ModulusSource = func() (*big.Int, error) { return fleet.FetchProductionModulus(r.HTTPBase) }
	}
	if *proxiesPath != "" {
		pool, err := fleet.LoadRoutePool(*proxiesPath)
		if err != nil {
			log.Fatalf("controlpanel: %v", err)
		}
		// Each route paces its own first logins at the login delay, and the launch loop below
		// starts accounts that many times as often, so every address ramps side by side.
		pool.Spacing = delay
		// The live server refuses the 30th login attempt from one address inside a rolling 60s window
		// (response 16), however slowly they come; 28 then a 65s rest never trips it.
		pool.LoginBurst, pool.LoginQuiet = 28, 65*time.Second
		if r.PinRoutes {
			pinPath := filepath.Join(filepath.Dir(*configPath), "route-pins.json")
			existing, err := fleet.LoadRoutePins(pinPath)
			if err != nil {
				log.Fatalf("controlpanel: %s: %v", pinPath, err)
			}
			names := make([]string, 0, len(r.Accounts))
			for _, a := range r.Accounts {
				names = append(names, a.Username)
			}
			pool.Pins = fleet.PlanRoutePins(existing, names, pool.RouteCaps())
			if err := fleet.SaveRoutePins(pinPath, pool.Pins); err != nil {
				log.Fatalf("controlpanel: %s: %v", pinPath, err)
			}
			per := map[string]int{}
			for _, id := range pool.Pins {
				per[id]++
			}
			log.Printf("controlpanel: pinRoutes is true — %d accounts pinned to one route each (%v), kept in %s", len(pool.Pins), per, pinPath)
		}
		fleetCfg.Routes = pool
	}
	if fleetCfg.Checksums, err = r.checksums(); err != nil {
		log.Fatalf("controlpanel: %s: %v", *configPath, err)
	} else if fleetCfg.Checksums != nil {
		log.Println("controlpanel: using the configured crcTable — not fetching /crc0 at each login")
	}
	if r.AntiCheat != nil && !*r.AntiCheat {
		fleetCfg.NoAntiCheat = true
		log.Println("controlpanel: antiCheat is false — ANTICHEAT side-packets are not sent")
	}
	if !login.ValidDeviceIDMode(r.DeviceIDs) {
		log.Fatalf("controlpanel: %s: deviceIds wants %q or %q, got %q", *configPath, login.DeviceIDFixed, login.DeviceIDPerAccount, r.DeviceIDs)
	}
	fleetCfg.DeviceIDs = r.DeviceIDs
	fleetCfg.BrowserHeaders = r.BrowserHeaders
	if r.BrowserHeaders {
		log.Println("controlpanel: browserHeaders is true — game WebSockets open with browser-like headers")
	}
	if r.DeviceIDs == login.DeviceIDPerAccount {
		log.Println("controlpanel: deviceIds is per-account — each account logs in with its own stable device id")
	}
	if r.PackPath != "" {
		pk, err := pathfind.Load(r.PackPath)
		if err != nil {
			log.Fatalf("controlpanel: loading pack: %v", err)
		}
		fleetCfg.Pack = pk
		log.Printf("controlpanel: loaded collision pack from %s", r.PackPath)
	}
	if r.LocsPath != "" {
		ix, err := locindex.Load(r.LocsPath)
		if err != nil {
			log.Fatalf("controlpanel: loading loc index: %v", err)
		}
		fleetCfg.Locs = ix
		log.Printf("controlpanel: loaded loc index from %s (%d locs)", r.LocsPath, ix.Count())
		// The roster's own (active-only) loc index is exactly right for /api/poi too — see
		// Config.Locs's own doc comment — so reuse it here rather than requiring a redundant
		// -map-locs flag pointing at the same file. An explicit -map-locs still wins if given.
		if cfg.Locs == nil {
			cfg.Locs = ix
		}
	}
	if r.ConfigPath != "" {
		cc, err := cachecfg.LoadFile(r.ConfigPath)
		if err != nil {
			log.Fatalf("controlpanel: loading config archive: %v", err)
		}
		fleetCfg.Cache = cc
		log.Printf("controlpanel: loaded config archive from %s", r.ConfigPath)
		if cfg.LocTypes == nil {
			cfg.LocTypes = cc
		}
	}
	// Non-fatal on failure, same as cmd/fleet: only a bank-using script needs Components, and
	// it fails clearly at StartBot/OnStart time rather than refusing to start over it here.
	if components, err := archive.FetchComponents(r.HTTPBase); err != nil {
		log.Printf("controlpanel: fetching interface archive components (bank-using scripts will fail to start): %v", err)
	} else {
		fleetCfg.Components = components
		log.Println("controlpanel: fetched interface archive components")
	}

	mgr := fleet.New(fleetCfg)
	if *perWorldLogins {
		mgr.SetPerWorldLoginPacing(true)
	}
	if *muleLedgerFile != "" {
		log.Printf("controlpanel: recording mule trades to %s", *muleLedgerFile)
	}
	mgr.SetMuleLedger(muleledger.Open(*muleLedgerFile, 0))
	mgr.Log = func(msg string) { log.Println(msg) }
	watchCfg := watchDefaults
	watchCfg.Mode, watchCfg.StallAfter, watchCfg.BlindAfter, watchCfg.RecheckAfter = watchMode, *watchdogStall, *watchdogBlind, *watchdogRecheck
	watchCfg.Exempt, watchCfg.FleetActions, watchCfg.DumpDir = stallwatch.ParseExempt(*watchdogExempt), *watchdogMaxActions, *watchdogDumpDir
	watchCfg.OrphanAfter = *watchdogOrphan
	mgr.StartWatchdog(watchCfg)
	for _, line := range mgr.RouteDescriptions() {
		log.Printf("controlpanel: %s", line)
	}
	// The order the accounts will be launched in (the mule, the main account and the carriers first: loginOrder), which is also the order the panel's
	// login queue lists the ones not yet started in.
	launch := loginOrder(accounts, assignments)
	src := controlpanel.NewFleetSource(mgr, usernames(launch), delay)
	// Give the panel somewhere to look when a Start arrives with no settings at all. Without
	// it the bot starts on the SCRIPT's defaults, and cheatSetup defaults to true -- a
	// local-engine-only flag -- so a production account's script stops on the first ::tele.
	// See FleetSource.StartBot for the live account this happened to. These are the very
	// same entries the auto-start below passes, so a panel Start and the process's own start
	// mean the same thing.
	rosterSettings := make(map[string]map[string]interface{}, len(accounts))
	for _, a := range accounts {
		if len(a.Settings) > 0 {
			rosterSettings[strings.ToLower(a.Username)] = a.Settings
		}
	}
	src.SetRosterSettings(func(account string) map[string]interface{} {
		return rosterSettings[strings.ToLower(account)]
	})
	if assignments.Enabled() {
		rosterEntries := rosterLookup(accounts)
		src.SetScriptAssignments(assignments, rosterEntries, log.Printf)
		describeAssignments(assignments, rosterEntries)
	}
	srv := serve(ctx, cfg, src)
	go discord.Watch(ctx, src)
	startCrowdPlay(ctx, src) // off unless -crowdplay-discord-token-file is given

	stagger := delay
	if routes := len(mgr.RouteDescriptions()); routes > 1 {
		stagger = delay / time.Duration(routes)
		log.Printf("controlpanel: %d routes pace their own first logins %s apart, so accounts start %s apart", routes, delay, stagger)
	}
	go func() {
		log.Printf("controlpanel: connecting %d of %d accounts, %s apart", n, len(r.Accounts), stagger)
		for i, a := range launch {
			if ctx.Err() != nil {
				return
			}
			src.LoginStarted()
			go func(a accountConfig) {
				// A saved script assignment (a panel switch from before this restart) wins
				// over the roster entry; see startupScript. Chosen BEFORE the login, so a
				// script that names its own homeWorld (ScarabSwarm's world-2 cohort) logs in
				// there directly rather than on the roster's world and then hopping.
				script, settings, why := startupScript(a, assignments, scriptRegistered)
				if why != "" {
					log.Printf("controlpanel: [%s] %s", a.Username, why)
				}
				world := startupWorld(mgr, a, script, settings)
				if world != a.World {
					log.Printf("controlpanel: [%s] logging in on world %d, the homeWorld its %s settings name (roster world %d)", a.Username, world, script, a.World)
				}
				// ConnectWorld, not Connect: an account that names a world must log in to
				// THAT world, and reconnectLoop must bring it back to the same one (see
				// internal/fleet/worldresolve.go). World 0 is exactly what Connect passed.
				mgr.ConnectWorld(a.Username, creds.passwordFor(a.Username, password), world)
				if script == "" {
					return
				}
				// waitConnected below covers the case fleet.connectAccount's own rate-limit
				// retry handles (see its doc comment): a first login attempt that hit the
				// login server's flood protection (response 16) keeps retrying with backoff
				// in the background well past this goroutine's own return from Connect, so a
				// fixed short wait here used to mean the auto-start attempt below reliably
				// failed ("not connected yet") for exactly the accounts that most needed it —
				// an operator had to notice and start each one by hand, live, twice tonight.
				// As cmd/fleet does: give the read loop a moment to observe a successful
				// login before auto-starting a bot that expects a live tile/skill state.
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				if !waitConnected(ctx, mgr, a.Username) {
					return // only returns false on shutdown (ctx.Done) — see waitConnected's own doc comment
				}
				if err := mgr.StartBot(a.Username, script, settings); err != nil {
					log.Printf("controlpanel: [%s] auto-start of %q failed: %v", a.Username, script, err)
				}
			}(a)
			if i < len(launch)-1 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(stagger):
				}
			}
		}
	}()

	<-ctx.Done()
	log.Println("controlpanel: stopping and logging every account out")
	mgr.CloseAll()
	<-srv.Done()
}

// waitConnected polls mgr for username's connected state, for a start-of-day auto-start
// racing fleet.connectAccount's own background rate-limit retry (see that function's doc
// comment) rather than a fixed short wait that reliably missed it. No timeout — live-found
// 2026-09-16, this codebase's first real 179-account ramp: an earlier version of this
// function gave up after a fixed 4-minute budget, chosen from a single earlier successful
// recovery that happened to land inside it. At full fleet scale, a big enough backlog (see
// internal/fleet/loginrate.go's maxLoginPenalty and its own doc comment on the live-lock it
// closes) can genuinely take much longer than 4 minutes to drain, and every account whose
// login landed after that budget expired simply never got its script auto-started — 85 of
// 179 accounts sat connected but idle until an operator noticed and started them all by
// hand. fleet.reconnectLoop itself already retries indefinitely by design ("a fleet meant
// to run unattended for a full day should keep trying rather than giving up after N
// attempts" — see its own doc comment); this now matches that same philosophy instead of
// silently giving up first. Returns true once connected, false only if ctx is cancelled
// (the fleet is shutting down) first — a real, bounded reason to stop, unlike an arbitrary
// timer. A short, simple poll rather than a callback/channel into internal/fleet: this is
// the CLI's own startup convenience, not a change to the Manager's public contract.
func waitConnected(ctx context.Context, mgr *fleet.Manager, username string) bool {
	for {
		for _, s := range mgr.Snapshot() {
			if s.Username == username {
				if s.Connected {
					return true
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
}

func serve(ctx context.Context, cfg controlpanel.Config, src controlpanel.Source) *controlpanel.Server {
	srv, err := controlpanel.Start(ctx, cfg, src)
	if err != nil {
		log.Fatalf("controlpanel: %v", err)
	}
	if cfg.Token != "" {
		log.Printf("controlpanel: http://%s/ (open it once as /?token=<your token>)", srv.Addr())
	} else {
		log.Printf("controlpanel: http://%s/", srv.Addr())
	}
	return srv
}

// resolveGitHubToken finds a GitHub API token for the per-script update-check panel,
// trying, in order: the environment variable named by -github-token-env (if given),
// then GITHUB_TOKEN, then GH_TOKEN, then `gh auth token` — this machine's own gh CLI,
// already authenticated with repo scope against oldm8clint/rsb0t-go per the account
// owner's own setup (confirmed live, 2026-09-16: `gh auth status` shows a logged-in
// 'repo'-scoped token). Never printed or logged; only its presence/absence is. Returns
// "" (disabling the panel, not a fatal error) when none of the four produce anything —
// a token requirement should never block the rest of the dashboard from starting.
func resolveGitHubToken(envName string) string {
	if envName != "" {
		if v := os.Getenv(envName); v != "" {
			return v
		}
	}
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// defaultFleetControlDir resolves the directory the dashboard's own whole-fleet Restart
// button looks in for restart-fleet.bat when -fleet-control-dir is not given: the
// directory holding THIS running executable, matching run/start-fleet.bat's own
// assumption that controlpanel_live.exe and every run/*.bat script sit side by side (see
// run/README.txt). Returns "" when os.Executable() fails (should not happen on a real
// Windows deployment) or effectively when running under `go run`/`go test` (a build with
// no fixed on-disk exe path worth trusting, e.g. a temp file under GOCACHE) —
// restartFleet below treats a resolved-but-wrong directory the same as "script not
// found": a Restart attempt that clearly reports it could not find restart-fleet.bat,
// never one that guesses or falls back to something surprising.
func defaultFleetControlDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// restartFleet launches dir/restart-fleet.bat (see run/restart-fleet.bat: stop, cool
// down 3 minutes against the login server's rate limit, then start on whatever build
// currently sits in dir) as an independent process, detached from this one via `cmd /C
// start` — the same technique run/start-fleet.bat itself already uses to background
// controlpanel_live.exe — so the .bat keeps running after THIS process exits rather than
// being torn down with it (a plain os/exec child on Windows is not tied to its parent's
// lifetime by default, but going through `start` also gives the relaunch its own console
// window, same as an operator double-clicking the .bat directly).
//
// Deliberately does NOT wait for the .bat to finish (it sleeps 3 minutes by design) or
// confirm the new instance actually comes back up: doing that would need this process to
// stay alive and holding its listen port well past when it needs to have given that port
// up, defeating the whole point of delegating to the external script instead of an
// in-process self-exec/respawn race. This is exactly the level of confirmation an
// operator already has double-clicking restart-fleet.bat by hand — no more, no less;
// check run/fleet.log or the dashboard itself a few minutes later to see whether it
// actually came back.
//
// A non-nil error means the LAUNCH ITSELF failed (script missing, or the OS refused to
// start cmd.exe) — the caller (Config.OnRestartAll above) then does NOT trigger a
// shutdown, so a misconfigured -fleet-control-dir or a missing script leaves the live
// fleet running rather than stopping it with no way to bring it back from the dashboard.
func restartFleet(dir string) error {
	if dir == "" {
		return fmt.Errorf("no fleet-control directory resolved (pass -fleet-control-dir, or run the built controlpanel_live.exe from run/ as usual) — refusing to guess where restart-fleet.bat is")
	}
	script := filepath.Join(dir, "restart-fleet.bat")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("restart-fleet.bat not found at %s: %w", script, err)
	}
	// `cmd /C start "title" <full path>\restart-fleet.bat` — not start-fleet.bat's own
	// `/B` — gives the relaunch its own visible console window: the same thing an
	// operator sees double-clicking the .bat directly, rather than a hidden background
	// process nobody can see if start-fleet.bat's own final `pause` is left sitting there
	// waiting for a keypress that will never come. The FULL, absolute path to the script
	// is passed deliberately rather than a bare "restart-fleet.bat" relying on cmd.Dir
	// alone: live-tested (2026-09-17) against a local -fake instance, a bare relative
	// filename here resolves correctly in an ordinary interactive session but is a
	// strictly weaker bet than an absolute path in general (e.g. if this process is ever
	// itself launched with an unexpected working directory) — the absolute path removes
	// that ambiguity entirely rather than depending on cmd.Dir being honoured exactly as
	// expected by every possible caller of this process.
	cmd := exec.Command("cmd", "/C", "start", "rsb0t-go fleet restart", script)
	cmd.Dir = dir
	// Not Wait()ed on purpose: `cmd /C start ...` exits almost immediately once it has
	// handed the .bat off to its own new console, and this process is about to shut
	// itself down anyway (see Config.OnRestartAll above) — there is nothing useful left
	// to wait for, and waiting here would block the HTTP response this is answering.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching restart-fleet.bat: %w", err)
	}
	return nil
}

func loadRoster(path string) (*roster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r roster
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return &r, nil
}
