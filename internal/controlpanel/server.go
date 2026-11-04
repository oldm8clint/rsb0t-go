package controlpanel

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/eraassets"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/updatewatcher"
)

// APIVersion is the version of the HTTP API and the stream. The page reads it from /api/meta.
const APIVersion = 1

//go:embed web
var webFiles embed.FS

type Server struct {
	cfg              Config
	opts             options
	src              Source
	hub              *hub
	layout           *layoutStore
	hiscores         *hiscoresCache
	market           *marketCache
	stats            *statsAggregator
	srv              *http.Server
	ln               net.Listener
	done             chan struct{}
	once             sync.Once
	err              error
	engineTick       engineTickState
	mapData          mapDataCache
	updates          *updatewatcher.Manager
	bulkConnectionMu sync.Mutex
	heat             *heatRecorder
	// phases samples every account's script phase for the time-split panel (phases.go); nil when
	// the Source cannot report phases.
	phases *phaseRecorder
	// stream is the single-account stream client's existing-session bridge (see
	// streamclient.go). streamRenderer/streamDescription come from the installed
	// provider's own sidecar manifest, read once at construction — both empty when no
	// provider is installed, which is what keeps the panel's Load button disabled.
	stream            streamState
	input             streamInputState
	streamRenderer    string
	streamDescription string
	// streamSkins/streamDefaultSkin are the era skins that same manifest declares,
	// validated at construction (streamskin.go). Both are empty for a provider that
	// declares none, which is every provider that predates era skins.
	streamSkins       []StreamSkin
	streamDefaultSkin string
	// era is the operator-supplied era asset root (internal/eraassets), measured once at
	// construction. nil on every build started without -era-assets, which its own methods
	// tolerate: every era then reports absent, and no non-2004 skin is offerable.
	era *eraassets.Store
	// eraClassic holds the Classic chrome sprites, decoded from the operator's own
	// media58.jag on first request and kept (streamera.go). Nothing is decoded on a
	// build with no -era-assets.
	eraOnce    sync.Once
	eraClassic *eraSprites
}

// engineTickState is the last-known engine tick rate, updated by a background poller when
// Config.EngineHTTPBase is set — see pollEngineTick's own doc comment.
type engineTickState struct {
	mu sync.Mutex
	ms int64
	ok bool
}

func (e *engineTickState) set(ms int64, ok bool) { e.mu.Lock(); e.ms, e.ok = ms, ok; e.mu.Unlock() }
func (e *engineTickState) get() (int64, bool)    { e.mu.Lock(); defer e.mu.Unlock(); return e.ms, e.ok }

func Start(ctx context.Context, cfg Config, src Source) (*Server, error) {
	return start(ctx, cfg, src, defaultOptions())
}
func start(ctx context.Context, cfg Config, src Source, opts options) (*Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	s, err := newServer(cfg, src, opts)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("controlpanel: listen: %w", err)
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, ConnContext: rememberConn}
	s.hub.start()
	s.stats.start()
	s.heat.start()
	s.phases.start()
	go func() { _ = s.srv.Serve(ln) }()
	go func() {
		select {
		case <-ctx.Done():
			grace, cancel := context.WithTimeout(context.Background(), opts.shutdownGrace)
			_ = s.Shutdown(grace)
			cancel()
		case <-s.done:
		}
	}()
	if s.cfg.EngineHTTPBase != "" {
		go s.pollEngineTick(ctx)
	}
	if s.updates != nil {
		go s.updates.Run(ctx)
	}
	return s, nil
}

// pollEngineTick polls a LOCAL test engine's /debug/tickrate route every 3s and stores the
// result on s.engineTick for handleEngineTick to serve — see Config.EngineHTTPBase's own doc
// comment. A fetch error (production has no such route; the local engine might be mid-restart)
// just marks the state unavailable rather than logging noise, since this is a purely cosmetic
// dashboard feature, not a health check anything else depends on.
func (s *Server) pollEngineTick(ctx context.Context) {
	client := &http.Client{Timeout: 2 * time.Second}
	url := strings.TrimRight(s.cfg.EngineHTTPBase, "/") + "/debug/tickrate"
	fetch := func() {
		resp, err := client.Get(url)
		if err != nil {
			s.engineTick.set(0, false)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			s.engineTick.set(0, false)
			return
		}
		var body struct {
			TickRateMs int64 `json:"tickRateMs"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body); err != nil || body.TickRateMs <= 0 {
			s.engineTick.set(0, false)
			return
		}
		s.engineTick.set(body.TickRateMs, true)
	}
	fetch()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-t.C:
			fetch()
		}
	}
}
func newServer(cfg Config, src Source, opts options) (*Server, error) {
	if src == nil {
		return nil, errors.New("controlpanel: no source")
	}
	cfg, err := cfg.normalised()
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, opts: opts, src: src, hub: newHub(src, cfg, opts), layout: &layoutStore{path: cfg.LayoutFile}, hiscores: newHiscoresCache(), market: newMarketCache(), stats: newStatsAggregator(cfg.StatsStateFile, src, opts.now), done: make(chan struct{})}
	s.stats.loot.prices = cfg.Prices
	s.heat = newHeatRecorder(cfg, src, opts.now)
	s.phases = newPhaseRecorder(src, opts.now)
	// A configured stream client provider whose manifest is missing, malformed or
	// declares an unknown renderer identity is a startup error, not a silently disabled
	// panel: the operator installed something, and "nothing happened" is the least
	// useful possible answer. See loadStreamManifest's own doc comment for the honest
	// limit of what a manifest proves.
	// The era asset root is opened BEFORE the provider manifest, so a build whose skins
	// depend on it fails on the assets first — the more specific error of the two.
	if era, err := eraassets.Open(cfg.EraAssets); err != nil {
		return nil, err
	} else {
		s.era = era
	}
	if manifest, err := loadStreamManifest(cfg.StreamClientBundle); err != nil {
		return nil, err
	} else {
		// The cache-terrain provider decodes the game cache in the browser through the
		// capture adapter; without both capture flags it can never draw a triangle, so
		// that is the same kind of startup error as a missing manifest.
		if manifest.SceneRenderer == StreamRendererCacheTerrain && (cfg.CaptureRenderer == "" || cfg.CaptureCacheDir == "") {
			return nil, fmt.Errorf("stream client provider %s (%s) needs -capture-renderer and -capture-cache: it decodes cache terrain through the capture adapter", cfg.StreamClientBundle, StreamRendererCacheTerrain)
		}
		// An era skin list that does not validate is the same class of startup error as
		// an unknown renderer identity: the operator installed a provider that says it
		// can draw an era this build cannot name, and a silently missing dropdown entry
		// is the least useful possible answer. See streamskin.go.
		defaultSkin, err := validateStreamSkins(manifest.Skins)
		if err != nil {
			return nil, fmt.Errorf("stream client manifest %s.manifest.json: %w", cfg.StreamClientBundle, err)
		}
		s.streamRenderer, s.streamDescription = manifest.SceneRenderer, manifest.Description
		s.streamSkins, s.streamDefaultSkin = manifest.Skins, defaultSkin
	}
	// The script-version-check panel (see internal/updatewatcher's own doc comment) is
	// entirely optional, same shape as EngineHTTPBase/MapTilesDir above: a Token with no
	// Scripts (or vice versa) can never produce a useful answer, so both are required
	// together, and BuildSHA/BuildCommitTime unset (a build with no VCS stamp at all)
	// means there is no baseline to compare anything against either.
	if cfg.UpdateCheck.Token != "" && len(cfg.UpdateCheck.Scripts) > 0 && cfg.BuildSHA != "" && !cfg.BuildCommitTime.IsZero() {
		w := &updatewatcher.Watcher{Owner: cfg.UpdateCheck.Owner, Repo: cfg.UpdateCheck.Repo, Token: cfg.UpdateCheck.Token}
		s.updates = updatewatcher.NewManager(w, cfg.BuildSHA, cfg.BuildCommitTime, cfg.UpdateCheck.Scripts)
		s.updates.Interval = cfg.UpdateCheck.Interval
	}
	return s, nil
}
func (s *Server) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}
func (s *Server) Done() <-chan struct{} {
	if s == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return s.done
}
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		s.hub.close()
		s.stats.close()
		s.heat.close()
		s.phases.close()
		if s.srv != nil {
			if err := s.srv.Shutdown(ctx); err != nil {
				s.err = err
				_ = s.srv.Close()
			}
		}
		close(s.done)
	})
	return s.err
}
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/memory", s.handleMemory)
	mux.HandleFunc("/api/inventory", s.handleInventory)
	mux.HandleFunc("/api/bank", s.handleBank)
	mux.HandleFunc("/api/hiscores", s.handleHiscores)
	mux.HandleFunc("/api/market", s.handleMarket)
	mux.HandleFunc("/api/prices", s.handlePrices)
	mux.HandleFunc("/api/layout", s.handleLayout)
	mux.HandleFunc("/api/stream", s.handleStream)
	// Accumulated run statistics (see stats.go) — always available (memory-only when
	// Config.StatsStateFile is unset), same as every other built-in aggregation this
	// package computes itself rather than reading from Source.
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/bank-all", s.handleBankAll)
	// The generated processing-route table (bankvalue.go). Static for the life of the
	// binary, so the Combined bank panel fetches it once rather than with every refresh.
	mux.HandleFunc("/api/item-routes", s.handleItemRoutes)
	// Where the time goes (phases.go) and the mule dashboard (mulepanel.go); 501 when the
	// Source cannot report phases or mule status.
	mux.HandleFunc("/api/phases", s.handlePhases)
	mux.HandleFunc("/api/mule", s.handleMule)
	mux.HandleFunc("/api/capture", s.handleCapture)
	mux.HandleFunc("/api/map-image", s.handleMapImage)
	mux.HandleFunc("/capture/cache/", s.handleCaptureCache)
	mux.HandleFunc("/capture/client.js", s.handleCaptureRenderer)
	// Single-account stream client (streamclient.go). /stream/client.js is the one fixed
	// same-origin provider path the panel imports — never a URL out of a saved layout.
	// The bridge endpoints under /api/stream/ read observations off an ALREADY-CONNECTED
	// session through RenderSource; none of them can create, join or duplicate one, and
	// /api/stream/input answers 501 unless this build was started with -stream-input AND
	// the Source can run an action on that session and stop/start its bot (streaminput.go's
	// streamInputAvailable). A Source with no RenderSource answers 501 throughout, same as
	// /api/capture.
	mux.HandleFunc("/stream/client.js", s.handleStreamRenderer)
	// The OBS browser-source page and its two assets (streamview.go). Exact paths, so they
	// win over the provider-sibling handler above; a provider must not ship siblings with
	// these three names, which would then be unreachable.
	mux.HandleFunc("/stream/view", s.handleStreamView)
	mux.HandleFunc("/stream/view.js", s.handleStreamView)
	mux.HandleFunc("/stream/view.css", s.handleStreamView)
	mux.HandleFunc(eraSpritePrefix, s.handleStreamEraAsset)
	mux.HandleFunc("/stream/", s.handleStreamAsset)
	mux.HandleFunc("/api/stream/open", s.handleStreamOpen)
	mux.HandleFunc("/api/stream/close", s.handleStreamClose)
	mux.HandleFunc("/api/stream/frame", s.handleStreamFrame)
	mux.HandleFunc("/api/stream/input", s.handleStreamInput)
	// Opt-in input (streaminput.go): 501 unless the build was started with -stream-input.
	mux.HandleFunc("/api/stream/action", s.handleStreamAction)
	mux.HandleFunc("/api/stream/audit", s.handleStreamAudit)
	// Read-only observation cost, always on (streamstats.go): what serving frames costs.
	mux.HandleFunc("/api/stream/stats", s.handleStreamStats)
	// The loot panel (lootstats.go) and the mule trade record (muleledger.go, 501 without one).
	mux.HandleFunc("/api/loot", s.handleLoot)
	// Clear one card, or one item off it, from the loot panel's data (lootclear.go). POST only.
	mux.HandleFunc("/api/loot/clear", s.handleLootClear)
	mux.HandleFunc("/api/mule-ledger", s.handleMuleLedger)
	// Bot control is optional (BotControl); a Source without it answers 501, same as /api/inventory.
	mux.HandleFunc("/api/bot/start", s.handleBotStart)
	mux.HandleFunc("/api/bot/stop", s.handleBotStop)
	mux.HandleFunc("/api/bot/start-all", func(w http.ResponseWriter, r *http.Request) { s.handleBulkBots(w, r, true) })
	mux.HandleFunc("/api/bot/stop-all", func(w http.ResponseWriter, r *http.Request) { s.handleBulkBots(w, r, false) })
	// Saved script assignments (assignments.go): GET lists them, DELETE ?account= removes one.
	mux.HandleFunc("/api/assignments", s.handleAssignments)
	// Hand-off control is optional (HandoffControl); a Source without it answers 501 too.
	mux.HandleFunc("/api/bot/handoff", s.handleBotHandoff)
	mux.HandleFunc("/api/bot/muleat", s.handleBotMuleAt)
	// The stall watchdog's mode (WatchdogControl): GET reads it, POST {"mode": ...} changes it.
	mux.HandleFunc("/api/watchdog", s.handleWatchdog)
	// Connection control is optional (ConnectionControl); a Source without it answers 501 too.
	mux.HandleFunc("/api/fleet/disconnect", s.handleFleetDisconnect)
	mux.HandleFunc("/api/fleet/connect", s.handleFleetConnect)
	mux.HandleFunc("/api/fleet/add", s.handleFleetAdd)
	for _, action := range []string{"login", "logout", "requeue"} {
		action := action
		mux.HandleFunc("/api/fleet/"+action+"-accounts", func(w http.ResponseWriter, r *http.Request) { s.handleBulkConnections(w, r, action) })
	}
	// Whole-fleet process control (Config.OnStopAll/OnRestartAll — see their own doc
	// comments) is optional the same way, gated on a Config callback rather than a Source
	// capability since it controls the OWNING PROCESS, not the fleet-of-accounts Source
	// models. POST only, same as every other mutating endpoint here, which is also what
	// keeps this unreachable through cmd/controlpanel-remote-proxy's read-only (GET/HEAD
	// only) tunnel by construction.
	mux.HandleFunc("/api/fleet/stop-all", s.handleFleetStopAll)
	mux.HandleFunc("/api/fleet/restart-all", s.handleFleetRestartAll)
	// Node telemetry is optional: existing local Source implementations remain compatible.
	mux.HandleFunc("/api/nodes", s.handleNodes)
	mux.HandleFunc("/api/nodes/", s.handleNodes)
	// Skill icons are optional (Config.SkillIcons; empty under -fake or with no media.jag
	// loaded) — 404 per name when this build has none, same shape as every other
	// optional-data 404 in this file, so the page's own fallback tile takes over per icon.
	mux.HandleFunc("/api/sprites/skill", s.handleSkillSprite)
	// Engine tick rate is optional: empty Config.EngineHTTPBase (any production fleet, or a
	// local one that didn't set it) just answers available:false, same shape either way.
	mux.HandleFunc("/api/enginetick", s.handleEngineTick)
	// Per-script version/update-check data is optional the same way (s.updates nil when
	// Config.UpdateCheck isn't fully configured) — answers available:false rather than
	// an error, same shape as /api/enginetick.
	mux.HandleFunc("/api/scriptversions", s.handleScriptVersions)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such endpoint") })
	// Real per-tile map data is optional (Config.MapTilesDir; see that field's own doc
	// comment) — served as plain static files (a {z}/{x}/{y}.png pyramid plus meta.json) from
	// wherever an operator generated it, same "generated, not committed" treatment as
	// internal/locindex's own bake. Unset (every production fleet today) registers nothing
	// here at all, so the existing flat-image map path is completely unaffected.
	if s.cfg.MapTilesDir != "" {
		mux.Handle("/maptiles/", http.StripPrefix("/maptiles/", http.FileServer(http.Dir(s.cfg.MapTilesDir))))
	}
	// /api/poi and /api/walls are optional the same way (Config.Locs/LocTypes/WallLocs —
	// see their own doc comments in config.go); each answers an empty list, not an error,
	// when its backing data isn't configured, same as MapTilesDir unset.
	mux.HandleFunc("/api/poi", s.handlePOI)
	mux.HandleFunc("/api/players", s.handleSeenPlayers)
	mux.HandleFunc("/api/heatmap", s.handleHeatmap)
	mux.HandleFunc("/api/heatmap/reset", s.handleHeatmapReset)
	mux.HandleFunc("/api/walls", s.handleWalls)
	mux.Handle("/", assetHandler())
	return newGuard(s.cfg).wrap(mux)
}

// handlePOI answers every known POI (bank/altar/anvil/furnace/range — see poiCategory's own
// doc comment) once, cached for the server's lifetime (mapDataCache) since this is static
// data baked offline, never changing while the server runs.
func (s *Server) handlePOI(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	pois := s.mapData.pois(s.cfg.Locs, s.cfg.LocTypes)
	writeJSON(w, http.StatusOK, struct {
		POI []poiEntry `json:"poi"`
	}{pois})
}

// handleWalls answers every real wall/door line segment (see computeWalls's own doc comment
// for the two disclosed simplifications: level 0 only, no corner-post shapes), cached the
// same way handlePOI is.
func (s *Server) handleWalls(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	walls := s.mapData.wallSegments(s.cfg.WallLocs, s.cfg.LocTypes)
	writeJSON(w, http.StatusOK, struct {
		Walls []wallSegment `json:"walls"`
	}{walls})
}

var assetTypes = map[string]string{".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".svg": "image/svg+xml"}

func assetHandler() http.Handler {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		ext := path.Ext(r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/") {
			ext = ".html"
		}
		if ct, ok := assetTypes[ext]; ok {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

type metaBody struct {
	Version           int      `json:"version"`
	ServerTime        int64    `json:"serverTime"`
	TickMs            int64    `json:"tickMs"`
	EventHistory      int      `json:"eventHistory"`
	LogHistory        int      `json:"logHistory"`
	MemoryHistory     int      `json:"memoryHistory"`
	LayoutPersisted   bool     `json:"layoutPersisted"`
	Inventories       bool     `json:"inventories"`
	Bank              bool     `json:"bank"`
	BotControl        bool     `json:"botControl"`
	HandoffControl    bool     `json:"handoffControl"`
	ConnectionControl bool     `json:"connectionControl"`
	Scripts           []string `json:"scripts"`
	MaxStreams        int      `json:"maxStreams"`
	MaxAccounts       int      `json:"maxAccounts"`
	Grid              gridMax  `json:"grid"`
	States            []State  `json:"states"`
	Levels            []Level  `json:"levels"`
	SkillOrder        []string `json:"skillOrder"`
	SpriteSkills      []string `json:"spriteSkills"`
	MapTiles          bool     `json:"mapTiles"`
	MapPOI            bool     `json:"mapPOI"`
	MapWalls          bool     `json:"mapWalls"`
	Stats             bool     `json:"stats"`
	StatsPersisted    bool     `json:"statsPersisted"`
	// BuildSHA is this running process's own git commit SHA (see Config.BuildSHA's own
	// doc comment for where it comes from), empty when the build has no VCS stamp.
	// UpdateChecks is whether Config.UpdateCheck is fully configured (s.updates != nil)
	// — the panel uses this to show "not configured on this build" instead of an empty
	// table when e.g. no GitHub token was supplied, the same gating pattern BotControl
	// already uses for the Script control section.
	BuildSHA     string `json:"buildSha,omitempty"`
	UpdateChecks bool   `json:"updateChecks"`
	// FleetStop/FleetRestart report whether Config.OnStopAll/OnRestartAll are wired up —
	// the header's whole-fleet Stop/Restart buttons enable only when true, the same
	// "hide what this build cannot actually do" pattern BotControl/ConnectionControl/
	// UpdateChecks above already follow. Both false for e.g. `go test`'s own harnesses,
	// which never opt in.
	FleetStop             bool `json:"fleetStop"`
	FleetRestart          bool `json:"fleetRestart"`
	BulkConnectionControl bool `json:"bulkConnectionControl"`
	// StreamClient is the single-account stream client's capability object (see
	// streamclient.go's StreamClientCapabilities). Every field is computed from what is
	// actually installed on this build; the panel gates its Load and input controls on
	// it and displays the renderer identity verbatim, so a build with no provider
	// installed reports an empty sceneRenderer and the panel stays plainly disabled.
	StreamClient *StreamClientCapabilities `json:"streamClient,omitempty"`
	// ScriptAssignments is true when this process saves panel script starts so the next start
	// replays them (assignments.go); GET /api/assignments lists what is saved.
	ScriptAssignments bool `json:"scriptAssignments"`
}
type gridMax struct {
	Columns          int `json:"columns"`
	MaxRows          int `json:"maxRows"`
	MaxPanels        int `json:"maxPanels"`
	MaxSettingsBytes int `json:"maxSettingsBytes"`
	MaxPanelAccounts int `json:"maxPanelAccounts"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	_, inventories := s.src.(InventorySource)
	_, bank := s.src.(BankSource)
	bc, hasBotControl := s.src.(BotControl)
	_, hasHandoffControl := s.src.(HandoffControl)
	_, hasConnControl := s.src.(ConnectionControl)
	ac, hasAssignments := s.src.(AssignmentControl)
	hasAssignments = hasAssignments && ac.ScriptAssignmentsView().Enabled
	scripts := []string{}
	if hasBotControl {
		if got := bc.BotScripts(); got != nil {
			scripts = got
		}
	}
	skillOrder := make([]string, 0, len(gamestate.SkillNames))
	for _, n := range gamestate.SkillNames {
		if n != "" {
			skillOrder = append(skillOrder, n)
		}
	}
	spriteSkills := make([]string, 0, len(s.cfg.SkillIcons))
	for name := range s.cfg.SkillIcons {
		spriteSkills = append(spriteSkills, name)
	}
	sort.Strings(spriteSkills)
	writeJSON(w, http.StatusOK, metaBody{APIVersion, s.opts.now().UnixMilli(), s.opts.tick.Milliseconds(), s.cfg.EventHistory, s.cfg.LogHistory, s.cfg.MemoryHistory, s.layout.persisted(), inventories, bank, hasBotControl, hasHandoffControl, hasConnControl, scripts, s.opts.maxStreams, MaxAccounts, gridMax{GridColumns, GridMaxRows, MaxPanels, MaxSettingsBytes, MaxPanelAccounts}, States, Levels, skillOrder, spriteSkills, s.cfg.MapTilesDir != "", s.cfg.Locs != nil && s.cfg.LocTypes != nil, s.cfg.WallLocs != nil && s.cfg.LocTypes != nil, true, s.cfg.StatsStateFile != "", s.cfg.BuildSHA, s.updates != nil, s.cfg.OnStopAll != nil, s.cfg.OnRestartAll != nil, hasConnControl, s.streamCapabilities(), hasAssignments})
}

// scriptVersionsBody answers GET /api/scriptversions — see internal/updatewatcher's own
// doc comment for exactly what this can and cannot report (an "update exists" ANSWER
// only; nothing here can apply one). Available false (s.updates == nil) means
// Config.UpdateCheck was not fully configured for this build — a missing token, no
// script/path mapping, or no VCS-stamped BuildSHA to compare against — not an error.
type scriptVersionsBody struct {
	Available bool   `json:"available"`
	BuildSHA  string `json:"buildSha,omitempty"`
	// Owner/Repo are the resolved GitHub repo every link in Scripts (and the running
	// build's own commit, which the commits API never hands back on its own since it
	// isn't a commit lookup) points at — so the panel can build a link for BuildSHA
	// itself without duplicating Watcher's own default-filling logic client-side.
	Owner   string                       `json:"owner,omitempty"`
	Repo    string                       `json:"repo,omitempty"`
	Scripts []updatewatcher.ScriptStatus `json:"scripts"`
	// StaleCount is how many DISTINCT SCRIPTS have a newer commit available, not a
	// commit count — matching the "N scripts have newer code available" summary line
	// the panel shows, which is what actually matters for a "should I restart the
	// fleet" decision.
	StaleCount int `json:"staleCount"`
}

func (s *Server) handleScriptVersions(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.updates == nil {
		writeJSON(w, http.StatusOK, scriptVersionsBody{})
		return
	}
	statuses := s.updates.Snapshot()
	stale := 0
	for _, st := range statuses {
		if st.Update != nil {
			stale++
		}
	}
	owner, repo := s.updates.Watcher.OwnerRepo()
	writeJSON(w, http.StatusOK, scriptVersionsBody{true, s.cfg.BuildSHA, owner, repo, statuses, stale})
}

// handleStats answers GET /api/stats — accumulated run statistics, both this process's
// own lifetime-persisted totals and its own since-this-restart contribution (see
// stats.go's own doc comment for why both are shown). Always available: the aggregator
// itself has no optional-Source-capability gate, unlike /api/inventory or /api/bank.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.stats.snapshot())
}

// handleSkillSprite serves one skill's real in-game icon (see internal/spritepack and
// Config.SkillIcons) as a PNG, 404ing per name when this build has no media archive loaded or
// no confirmed frame for that particular skill — the page falls back to its own monogram tile
// in either case, so a 404 here is an expected, ungated response, not a failure.
func (s *Server) handleSkillSprite(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	name := r.URL.Query().Get("name")
	png, ok := s.cfg.SkillIcons[name]
	if !ok {
		writeError(w, http.StatusNotFound, "no real icon for this skill in this build's media archive")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	writeRawJSON(w, http.StatusOK, encodeStatus(s.src.Accounts(nil), s.src.Logins()))
}

// engineTickBody answers GET /api/enginetick — see Config.EngineHTTPBase's own doc comment.
type engineTickBody struct {
	Available  bool  `json:"available"`
	TickRateMs int64 `json:"tickRateMs"`
}

func (s *Server) handleEngineTick(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	ms, ok := s.engineTick.get()
	writeJSON(w, http.StatusOK, engineTickBody{ok, ms})
}

type eventsBody struct {
	Events []Event `json:"events"`
	Last   uint64  `json:"last"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	q := r.URL.Query()
	var since uint64
	if v := q.Get("since"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be a whole number")
			return
		}
		since = n
	}
	limit := s.cfg.EventHistory + s.cfg.LogHistory
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive whole number")
			return
		}
		if n < limit {
			limit = n
		}
	}
	kind := q.Get("kind")
	if kind != "" && kind != KindLog && kind != kindEvents {
		writeError(w, http.StatusBadRequest, "kind must be log or event")
		return
	}
	events, last := s.hub.eventsAfter(since, limit, kind)
	writeJSON(w, http.StatusOK, eventsBody{events, last})
}

type memoryBody struct {
	IntervalMs int64       `json:"intervalMs"`
	Samples    []MemSample `json:"samples"`
	Now        MemSample   `json:"now"`
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, memoryBody{s.opts.tick.Milliseconds(), s.hub.memorySamples(), MemSample{T: s.opts.now().UnixMilli(), MemStats: s.src.MemStats()}})
}

type inventoryBody struct {
	Account     string      `json:"account"`
	Inventories []Inventory `json:"inventories"`
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	inv, ok := s.src.(InventorySource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not report inventories")
		return
	}
	name := r.URL.Query().Get("account")
	if name == "" || len(name) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	list, found := inv.Inventories(name)
	if !found {
		writeError(w, http.StatusNotFound, "the fleet has no account by that name")
		return
	}
	writeJSON(w, http.StatusOK, inventoryBody{name, clampInventories(list)})
}

// bankBody answers GET /api/bank?account=. UpdatedAtUnixMs is 0 when the bank has never
// been observed open this session — the panel shows "never opened" rather than a bogus
// timestamp in that case, same as an empty Items list.
type bankBody struct {
	Account         string `json:"account"`
	Items           []Slot `json:"items"`
	UpdatedAtUnixMs int64  `json:"updatedAt"`
}

// handleBank answers an account's last-known bank contents (see BankSource's own doc
// comment for why this is on-demand, like /api/inventory, rather than part of the
// per-tick status poll). 501 when the Source has no BankSource.
func (s *Server) handleBank(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	bk, ok := s.src.(BankSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not report bank contents")
		return
	}
	name := r.URL.Query().Get("account")
	if name == "" || len(name) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	items, updated, found := bk.Bank(name)
	if !found {
		writeError(w, http.StatusNotFound, "the fleet has no account by that name")
		return
	}
	writeJSON(w, http.StatusOK, bankBody{name, clampSlots(items), updated})
}

// handleHiscores answers one player's real example.invalid hiscores, fetched and parsed
// server-side (see hiscores.go's own doc comment for why: no separate JSON API, no
// framing block either, but parsing renders natively in this app's own style instead
// of pulling in the whole external page). A 404 from the real site (confirmed live: an
// unknown name genuinely 404s) becomes this endpoint's own 404, not a generic error.
func (s *Server) handleHiscores(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	player := r.URL.Query().Get("player")
	if player == "" || len(player) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("player must be 1 to %d bytes", maxNameBytes))
		return
	}
	result, err := s.hiscores.get(player)
	if err != nil {
		if errors.Is(err, errHiscoresNotFound) {
			writeError(w, http.StatusNotFound, "no such player on example.invalid's hiscores")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleMarket answers one example.invalid market character's bank inventory, fetched and
// parsed server-side (see market.go's own doc comment for why it cannot be fetched from
// the page itself: the dashboard's own CSP restricts connect-src to 'self'). The result
// carries the snapshot's own age, because this is a periodic snapshot of a player-run
// market and is never live stock. An unreadable page is a 502, never an empty market.
func (s *Server) handleMarket(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	// `all=1` answers every known market in one request. The panel uses it so the browser
	// makes one call instead of five, and so a market that is down is reported as down
	// rather than as an empty market.
	if r.URL.Query().Get("all") != "" {
		writeJSON(w, http.StatusOK, s.market.all())
		return
	}
	character := DefaultMarketCharacter
	if raw := r.URL.Query().Get("character"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > maxMarketCharacter {
			writeError(w, http.StatusBadRequest, "character must be a positive number")
			return
		}
		character = n
	}
	result, err := s.market.get(character)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handlePrices answers every item price the book holds, so ANY panel can value items the way
// the Loot panel does rather than each inventing its own idea of what a thing is worth. The
// users asked for market prices "displayed across the control panel", and one shared endpoint
// is how that stays consistent.
//
// Read-only, and it kicks the book's own refresh, which is non-blocking (see PriceBook.Refresh):
// a stale table is served immediately while a fresh one is fetched behind it.
func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.cfg.Prices == nil {
		writeError(w, http.StatusNotImplemented, "this fleet is not reading market prices (start it with -market-prices)")
		return
	}
	s.cfg.Prices.Refresh()
	prices, listed, fullAt, headAt, trades, err := s.cfg.Prices.Snapshot()
	body := pricesBody{
		Items: prices, Listed: listed, Trades: trades,
		Source:    "https://example.invalid/market/trades",
		MinTrades: minConfidentTrades,
	}
	if !fullAt.IsZero() {
		body.RebuiltAt = fullAt.UnixMilli()
	}
	if !headAt.IsZero() {
		body.CheckedAt = headAt.UnixMilli()
	}
	if err != nil {
		body.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, body)
}

// pricesBody is GET /api/prices. Items is keyed by lower-cased item name, matching how a
// caller holding a game item name looks one up.
type pricesBody struct {
	Items map[string]MarketPrice `json:"items"`
	// Listed is the markets' own last-sold/last-bought figures — broader cover, weaker
	// evidence, and ranked below a completed trade.
	Listed map[string]int64 `json:"listed,omitempty"`
	// Trades is how many completed trades the table was computed from.
	Trades int `json:"trades"`
	// RebuiltAt is the last full 32-page rebuild; CheckedAt is the last cheap re-read of the
	// newest page. Both are shown because "checked a minute ago" and "rebuilt half an hour
	// ago" are different claims and a panel should not blur them.
	RebuiltAt int64 `json:"rebuiltAt,omitempty"`
	CheckedAt int64 `json:"checkedAt,omitempty"`
	// MinTrades is the sample floor below which a median is not trusted, so a panel can say
	// why an item shows no market price.
	MinTrades int    `json:"minTrades"`
	Source    string `json:"source"`
	Error     string `json:"error,omitempty"`
}

// maxMarketCharacter bounds the character id so a stray query cannot turn this endpoint
// into an open fetcher for arbitrarily long upstream URLs.
const maxMarketCharacter = 1 << 24

type layoutBody struct {
	Layout    *Layout `json:"layout"`
	Persisted bool    `json:"persisted"`
	Problem   string  `json:"problem,omitempty"`
}

func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !s.layout.persisted() {
			writeJSON(w, http.StatusOK, layoutBody{})
			return
		}
		l, err := s.layout.load()
		if err != nil {
			writeJSON(w, http.StatusOK, layoutBody{Persisted: true, Problem: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, layoutBody{Layout: l, Persisted: true})
	case http.MethodPut:
		if !s.layout.persisted() {
			writeError(w, http.StatusConflict, "no layout file is configured, so the browser keeps the layout")
			return
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "send the layout as application/json")
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxLayoutBytes))
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("layout is over the %d byte limit", MaxLayoutBytes))
				return
			}
			writeError(w, http.StatusBadRequest, "could not read the layout")
			return
		}
		l, err := ParseLayout(data)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.layout.save(l); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save the layout: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, layoutBody{Layout: &l, Persisted: true})
	default:
		methodNotAllowed(w, "GET, HEAD, PUT")
	}
}

type botActionBody struct {
	OK bool `json:"ok"`
}
type botStartRequest struct {
	Account  string                 `json:"account"`
	Script   string                 `json:"script"`
	Settings map[string]interface{} `json:"settings"`
}
type botStopRequest struct {
	Account string `json:"account"`
}

// readJSONBody reads r.Body up to limit and unmarshals it into v, writing the matching error
// response (413/415/400) and returning false when that fails — the same checks handleLayout's
// PUT case makes, shared here so /api/bot/start and /api/bot/stop need not repeat them.
func readJSONBody(w http.ResponseWriter, r *http.Request, limit int64, v interface{}) bool {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "send the request as application/json")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("request is over the %d byte limit", limit))
			return false
		}
		writeError(w, http.StatusBadRequest, "could not read the request")
		return false
	}
	if err := json.Unmarshal(data, v); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse the request: "+err.Error())
		return false
	}
	return true
}

// handleBotStart starts a registered internal/bot script on an account, delegating to
// BotControl (in practice internal/fleet.Manager.StartBot through FleetSource) — the same
// control cmd/fleet's dashboard exposes as POST /api/start, matched here so this panel is a
// superset rather than a separate, read-only view. 501 when the Source has no BotControl.
func (s *Server) handleBotStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	bc, ok := s.src.(BotControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support starting or stopping bots")
		return
	}
	var req botStartRequest
	if !readJSONBody(w, r, maxBotRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if req.Script == "" {
		writeError(w, http.StatusBadRequest, "script is required")
		return
	}
	if err := bc.StartBot(req.Account, req.Script, req.Settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, botActionBody{true})
}

// handleAssignments answers GET /api/assignments with every saved script assignment and what
// the roster says beside each, and DELETE /api/assignments?account=<name> removes one. Removing
// one does not stop or restart anything: it only means the next process start uses the roster.
// 501 when the Source keeps no assignments at all.
func (s *Server) handleAssignments(w http.ResponseWriter, r *http.Request) {
	ac, ok := s.src.(AssignmentControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not save script assignments")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, http.StatusOK, ac.ScriptAssignmentsView())
	case http.MethodDelete:
		name := r.URL.Query().Get("account")
		if name == "" || len(name) > maxNameBytes {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
			return
		}
		removed, err := ac.ClearScriptAssignment(name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, assignmentClearBody{OK: true, Removed: removed})
	default:
		methodNotAllowed(w, "GET, HEAD, DELETE")
	}
}

type assignmentClearBody struct {
	OK      bool `json:"ok"`
	Removed bool `json:"removed"`
}

// handleBotStop requests the bot on an account to stop, matching POST /api/stop on cmd/fleet's
// own dashboard. It returns as soon as the request is accepted; poll /api/status for botState
// to actually reach "stopped".
func (s *Server) handleBotStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	bc, ok := s.src.(BotControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support starting or stopping bots")
		return
	}
	var req botStopRequest
	if !readJSONBody(w, r, maxBotRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if err := bc.StopBot(req.Account); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, botActionBody{true})
}

type handoffRequest struct {
	Account string `json:"account"`
}
type muleAtRequest struct {
	Account string `json:"account"`
	MuleAt  int    `json:"muleAt"`
}

// handleBotHandoff asks a running donor script for a manual mule hand-off at its next
// check, delegating to HandoffControl (in practice internal/fleet.Manager.RequestHandoff
// through FleetSource) — the account owner's own "hand off now" ask (2026-09-14), so
// proving a donor/mule pair works doesn't mean waiting out a script's own muleAt threshold.
// 501 when the Source has no HandoffControl; the underlying script simply never reacting
// (every registered script but ProgressiveFisher, tonight) is not an error this endpoint
// can detect, the same "no-op for an unused setting" shape every other optional knob has.
func (s *Server) handleBotHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	hc, ok := s.src.(HandoffControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support triggering a mule hand-off")
		return
	}
	var req handoffRequest
	if !readJSONBody(w, r, maxBotRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if err := hc.RequestHandoff(req.Account); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, botActionBody{true})
}

// handleBotMuleAt live-overrides a running donor script's own muleAt threshold, delegating
// to HandoffControl — the account owner's other ask (2026-09-14): the banked-item count
// that starts a hand-over should be editable from the panel, not just a startup setting.
// muleAt <= 0 clears the override, falling back to whatever the script was started with.
func (s *Server) handleBotMuleAt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	hc, ok := s.src.(HandoffControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support editing the mule threshold")
		return
	}
	var req muleAtRequest
	if !readJSONBody(w, r, maxBotRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if err := hc.SetMuleAt(req.Account, req.MuleAt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, botActionBody{true})
}

type watchdogModeRequest struct {
	Mode string `json:"mode"`
}

type watchdogModeBody struct {
	OK       bool               `json:"ok"`
	Previous string             `json:"previous"`
	Watchdog fleet.WatchdogInfo `json:"watchdog"`
}

// handleWatchdog answers GET /api/watchdog with the stall watchdog's live mode and counts, and
// POST /api/watchdog {"mode": "off|observe|nudge|act"} switches the mode from the next sweep,
// without a fleet restart. The switch is not saved: the next process start uses -watchdog. 501
// when the Source has no WatchdogControl; the write goes through the same guard (same-origin,
// token) as every other POST.
func (s *Server) handleWatchdog(w http.ResponseWriter, r *http.Request) {
	wc, ok := s.src.(WatchdogControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet has no stall watchdog")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSON(w, http.StatusOK, wc.WatchdogInfo())
	case http.MethodPost:
		var req watchdogModeRequest
		if !readJSONBody(w, r, maxBotRequestBytes, &req) {
			return
		}
		prev, err := wc.SetWatchdogMode(req.Mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, watchdogModeBody{OK: true, Previous: prev, Watchdog: wc.WatchdogInfo()})
	default:
		methodNotAllowed(w, "GET, HEAD, POST")
	}
}

type connActionBody struct {
	OK bool `json:"ok"`
}
type connRequest struct {
	Account string `json:"account"`
}
type addAccountRequest struct {
	Username string                 `json:"username"`
	Password string                 `json:"password"`
	Script   string                 `json:"script"`
	Settings map[string]interface{} `json:"settings"`
}

// handleFleetDisconnect logs one specific account out cleanly, delegating to
// ConnectionControl (in practice internal/fleet.Manager.Disconnect through FleetSource —
// a real Session.Logout, not a bare Close). 501 when the Source has no ConnectionControl.
func (s *Server) handleFleetDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	cc, ok := s.src.(ConnectionControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support disconnecting accounts")
		return
	}
	var req connRequest
	if !readJSONBody(w, r, maxConnRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if err := cc.Disconnect(req.Account); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, connActionBody{true})
}

// handleFleetConnect relogs an account already known to the fleet that is not currently
// connected — one Disconnect turned off, or one still-reconnecting/given-up after an
// unexpected drop — delegating to ConnectionControl.ConnectExisting. For a brand-new
// account use /api/fleet/add instead. 501 when the Source has no ConnectionControl.
func (s *Server) handleFleetConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	cc, ok := s.src.(ConnectionControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support connecting accounts")
		return
	}
	var req connRequest
	if !readJSONBody(w, r, maxConnRequestBytes, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be 1 to %d bytes", maxNameBytes))
		return
	}
	if err := cc.ConnectExisting(req.Account); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, connActionBody{true})
}

// handleFleetAdd adds a brand-new account not in the fleet's original roster, connects it,
// and (when Script is set) starts that script on it with Settings — delegating to
// ConnectionControl.AddAccount. The panel's own "duplicate an existing account's settings"
// convenience is entirely client-side: it just pre-fills Script/Settings here from another
// account's already-known values (see fleetsource.go's Accounts/AccountStatus.Settings).
// 501 when the Source has no ConnectionControl.
func (s *Server) handleFleetAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	cc, ok := s.src.(ConnectionControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not support adding accounts")
		return
	}
	var req addAccountRequest
	if !readJSONBody(w, r, maxAddAccountRequestBytes, &req) {
		return
	}
	if req.Username == "" || len(req.Username) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("username must be 1 to %d bytes", maxNameBytes))
		return
	}
	if req.Password == "" || len(req.Password) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("password must be 1 to %d bytes", maxPasswordBytes))
		return
	}
	if err := cc.AddAccount(req.Username, req.Password, req.Script, req.Settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, connActionBody{true})
}

type fleetActionBody struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// handleFleetStopAll gracefully shuts down the WHOLE fleet process — every connected
// account logged out cleanly, then the process exits — via Config.OnStopAll (see that
// field's own doc comment: cmd/controlpanel/main.go wires it to the exact same
// context.CancelFunc Ctrl+C already cancels, so this takes the identical clean-shutdown
// path). This is a whole-fleet action, deliberately separate from account.js's own
// per-account Stop (POST /api/bot/stop) and Disconnect (POST /api/fleet/disconnect).
// 501 when this build has no OnStopAll wired (e.g. every test harness that doesn't opt
// in). The response is written and flushed BEFORE the shutdown is actually triggered (in
// a goroutine): http.Server.Shutdown waits for this handler to return before it starts
// closing connections, so triggering the cancel synchronously here could race the
// response never reaching the browser.
func (s *Server) handleFleetStopAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.cfg.OnStopAll == nil {
		writeError(w, http.StatusNotImplemented, "this build has no whole-fleet stop wired up")
		return
	}
	writeJSON(w, http.StatusOK, fleetActionBody{OK: true, Message: "stopping the fleet: every account is being logged out, then the process exits"})
	go s.cfg.OnStopAll()
}

// handleFleetRestartAll relaunches the whole fleet on whatever build currently sits next
// to the running binary, via Config.OnRestartAll (see that field's own doc comment for
// exactly what this can and cannot confirm — it does NOT wait to see the new instance
// actually come back up). A non-nil error means the RELAUNCH ITSELF could not even be
// triggered (e.g. restart-fleet.bat was not found next to this binary) — the fleet is
// NOT shut down in that case, and the error is surfaced clearly rather than exiting
// blind. 501 when this build has no OnRestartAll wired.
func (s *Server) handleFleetRestartAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.cfg.OnRestartAll == nil {
		writeError(w, http.StatusNotImplemented, "this build has no whole-fleet restart wired up")
		return
	}
	if err := s.cfg.OnRestartAll(); err != nil {
		writeError(w, http.StatusBadGateway, "could not launch the restart: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fleetActionBody{OK: true, Message: "restart launched: stopping now, then a cooldown, then starting on the latest build (see run/restart-fleet.bat)"})
}
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	methodNotAllowed(w, "GET, HEAD")
	return false
}
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		code = http.StatusInternalServerError
		data = []byte(`{"error":"could not encode the response"}`)
	}
	writeRawJSON(w, code, data)
}
func writeRawJSON(w http.ResponseWriter, code int, data []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
