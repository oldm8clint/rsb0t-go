package controlpanel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
)

// The single-account stream client's SERVER side: the existing-session observation
// bridge (docs/stream_client_panel.md's own "work still required, in order" item 1) and
// the /api/meta.streamClient capability contract the panel gates on.
//
// What this file genuinely does:
//   - serves ONE installed, same-origin provider ES module at /stream/client.js, from an
//     explicit on-disk path, exactly the way /capture/client.js is already served;
//   - reads the capability object that module ships beside itself and advertises the
//     renderer identity it declares — after checking the declaration's shape against a
//     known set, so a typo or a hand-edited "webgl3d" in an unrelated file cannot reach
//     the panel unnoticed;
//   - hands a browser bounded, sequenced, epoch-checked observation frames taken from the
//     account's ALREADY-CONNECTED Go session (Source's own RenderSource, the same one
//     /api/capture uses), under an exclusive, expiring view lease.
//
// What it explicitly does NOT do, and must not be described as doing:
//   - it does not render anything. Every triangle claim belongs to a browser measurement
//     of an installed provider, never to this file.
//   - it does not send input, actions or any packet into a session. The lease machinery
//     below governs the VIEW only. Input lives in streaminput.go, is OFF unless the build
//     was started with -stream-input, and is advertised (streamClient.sessionInput) only
//     when a Source can really carry it. Do not read "sessionInput: true" as "this file
//     sends packets" — it never does.
//   - it does not create, join or duplicate a game session. RenderSource reads a snapshot
//     off an existing one; there is no code path here that can log anything in.
//   - it does not serve 30/60 observations a second, whatever transport is negotiated.
//     The "scene-delta" transport (streamscene.go) cut the BYTES per frame by about 15x on
//     a measured real session, and that changed nothing about the RATE: a delta is computed
//     from the same whole RenderSnapshot. See DefaultStreamMaxFPS's own comment, and
//     GET /api/stream/stats (streamstats.go) for what an observation actually costs here.

// Renderer identities a provider may declare. An installed module that declares anything
// else is refused outright rather than advertised: the panel rejects a provider whose
// returned identity does not match the advertised one, so an unknown string here would
// simply produce an unexplainable mismatch in the browser instead of a clear server error.
//
// StreamRendererWebGL3D is the identity docs/stream_client_panel.md reserves for the real
// thing: authentic terrain, models, animation, transparency and picking on genuine GPU
// buffers. Nothing in this repository implements it yet, and CPU rasterization presented
// through a texture does not qualify (see that document's own point 2).
//
// StreamRendererTileGeometry is the deliberately weaker identity the reference provider
// shipped alongside this file declares: real WebGL triangles, but of a diagnostic tile /
// entity view built from live session observations — NOT the authentic game window.
//
// StreamRendererCacheTerrain is tools/stream/cache-terrain-client.js. It needs the capture
// adapter (-capture-renderer and -capture-cache) to decode the cache in the browser.
//
// SUPERSEDED 2026-09-21, and the original is left below rather than deleted because it was
// true when written and the correction is the useful part. This paragraph used to end "there
// are no loc/entity models, interfaces, animation or input", and the provider outgrew that:
// it now draws scenery and live loc changes, textured and animated cache models, the game's
// own side tabs, chatbox, rotating minimap and open modals, from the session's interface
// state. Checked against the provider itself rather than against its sidecar manifest, which
// is a claim and not evidence: tools/stream/cache-terrain-client.js references interfaces 40
// times, tabs 19, chat 16, scenery 14, the minimap 8 and modals 4.
//
// Input is the one clause that survived in spirit: the provider itself sends none, and a
// viewer drives the watched account only when the panel is started with -stream-client and
// -stream-input, which is a server-side path with its own refusals and audit log.
//
// This mattered operationally, not just editorially. run/'s Publish-StreamClient pins the
// provider from the MAIN CHECKOUT's working tree, and that checkout sat 231 commits behind
// main, so the live fleet served a day-old provider against current Go code -- today's
// interface decoding with yesterday's drawing. A comment saying "no interfaces" made that
// look like the design rather than a stale pin.
const (
	StreamRendererWebGL3D      = "webgl3d"
	StreamRendererTileGeometry = "webgl-tile-geometry"
	StreamRendererCacheTerrain = "webgl-cache-terrain"
)

var streamRendererIdentities = map[string]bool{
	StreamRendererWebGL3D:      true,
	StreamRendererTileGeometry: true,
	StreamRendererCacheTerrain: true,
}

// knownStreamRenderers lists streamRendererIdentities in a fixed order for error text.
var knownStreamRenderers = []string{StreamRendererWebGL3D, StreamRendererTileGeometry, StreamRendererCacheTerrain}

// DefaultStreamMaxFPS caps how often the bridge will take a fresh observation frame,
// whatever the browser asks for.
//
// This is not a placeholder to be raised casually. A frame here is a whole
// gamestate.RenderSnapshot — every nearby npc, player and loc instance, plus the full
// interface state — deep-copied out of a live session under its lock and JSON-encoded.
// The panel offers 15/30/60 FPS because that is the RENDER rate a provider should paint
// at; it is not a promise that the server will supply a new observation that often, and
// a provider is expected to interpolate between observations rather than demand one per
// painted frame. Raising this without first building the delta/diff transport
// docs/stream_client_panel.md item 1 calls for would put a per-frame full-scene copy on
// the same process that is running the whole fleet.
const DefaultStreamMaxFPS = 5

// DefaultStreamLeaseTTL is how long a view lease keeps OTHER viewers out after the browser last
// asked for a frame. A tab that is closed, crashes, or is simply left behind must not hold the
// single view slot forever; a live one renews the lease implicitly on every frame request, so no
// heartbeat of its own is needed. It is the time a rival has to wait, not the time the owner has
// to answer: a view that was merely late (a slow terrain decode on a busy machine, a throttled
// background tab) resumes its own lapsed lease on its next request as long as nobody took it
// meanwhile (handleStreamFrame), and a closing tab releases it at once (/api/stream/close).
const DefaultStreamLeaseTTL = 30 * time.Second

// StreamClientCapabilities is /api/meta.streamClient. Every field is computed from what
// is ACTUALLY installed on this build (see Server.streamCapabilities) — none of it is
// operator-assertable except the renderer identity, which comes from the installed
// module's own sidecar manifest and is checked against streamRendererIdentities above.
type StreamClientCapabilities struct {
	// SceneRenderer is the identity the installed provider module declares. Empty when
	// no module is installed, which is what keeps the panel's Load button disabled.
	SceneRenderer string `json:"sceneRenderer"`
	// Description is the installed provider's own one-line account of itself, shown in
	// the panel verbatim so a deliberately weaker provider can say so in the operator's
	// own view rather than being flattened into "GPU renderer".
	Description string `json:"description,omitempty"`
	// ExistingSession is whether this build can actually hand out observation frames
	// from an already-connected session — i.e. whether the Source implements
	// RenderSource. False makes the whole panel unusable by design: a provider with no
	// session to observe would have to invent one.
	ExistingSession bool `json:"existingSession"`
	// SessionInput is whether acknowledged, arbitrated input can actually reach the
	// session. It is computed, never asserted: true when the Source implements
	// StreamInputSink, or when this build was started with -stream-input AND the Source can
	// run an action on the session and stop/start its bot (streaminput.go's
	// streamInputAvailable). A default build passes neither test and reports false, which is
	// what a production fleet started without the flag reports.
	SessionInput bool `json:"sessionInput"`
	// CrowdPlay and MLRecording describe real adapters bound to THIS stream view, not
	// intentions and not other features with the same name. Both are false on every build
	// today: no Source implements either sink. In particular CrowdPlay-via-Discord
	// (internal/crowdplay, cmd/controlpanel/crowdplay.go) is a real, separate feature that
	// drives one fixed account from a chat channel and has nothing to do with the stream
	// view's lease, so running it does NOT make this field true.
	CrowdPlay   bool `json:"crowdplay"`
	MLRecording bool `json:"mlRecording"`
	// MaxFPS is the observation rate the server will actually serve (see
	// DefaultStreamMaxFPS) — deliberately reported so the panel can say plainly that a
	// 60 FPS render preference does not mean 60 observations a second.
	MaxFPS int `json:"maxFps"`
	// FrameTransport names what a frame is when a provider asks for NOTHING ELSE: a whole
	// snapshot. It is the default for a provider that predates the scene-delta transport,
	// not a report of what any live view is using — a provider picks its transport per
	// open (the shipped cache-terrain provider asks for "scene-delta"), the open reply
	// echoes what it got, and GET /api/stream/stats names the held view's transport. Do
	// not read this field as "this build streams whole snapshots".
	FrameTransport string `json:"frameTransport"`
	// Skins are the era skins the installed provider declares (streamskin.go), in the
	// order it declared them. Empty on every build whose provider predates skins, which
	// is what keeps the panel's skin selector hidden rather than offering a choice of
	// one. Declaring a skin is the provider's own claim that it can draw that era; it is
	// NOT evidence that this machine holds that era's assets, and not a licence to
	// redistribute them.
	Skins []StreamSkin `json:"skins,omitempty"`
	// Skin is the id used when nobody picks one — the skin marked default, else the first
	// declared. Empty when no skins are declared.
	Skin string `json:"skin,omitempty"`
	// FrameTransports lists every transport a provider may ask for at open time:
	// "full-snapshot" (the default above) and "scene-delta" (streamscene.go — a compact
	// scene, then only what changed, with periodic whole-scene resyncs). The observation
	// rate (MaxFPS) is the same for both.
	FrameTransports []string `json:"frameTransports"`
}

// streamManifest is the sidecar an installed provider ships beside its bundle, at
// "<bundle>.manifest.json". It exists so the renderer identity has a server-side source
// of truth that is part of the installed artifact rather than a flag an operator types
// next to an unrelated file.
//
// Honest limit, stated here because it is easy to overread this: a manifest is still only
// the module's own claim about itself. Nothing this process can do proves a JavaScript
// module really submits triangles to a GPU — only a browser measurement does, which is
// why docs/stream_client_panel.md keeps every rendering claim attached to a measured
// browser run. What the manifest does buy is that the claim travels WITH the installed
// code and is validated, so a mismatch between what is installed and what is advertised
// is a startup error instead of a silent surprise in the panel.
type streamManifest struct {
	SceneRenderer string `json:"sceneRenderer"`
	// Description is shown in the panel's capability list verbatim, so an honestly
	// weaker provider can say so in the operator's own view.
	Description string `json:"description"`
	// Skins are the era skins this provider declares it can draw the SAME session in
	// (streamskin.go). Optional and empty for every provider that predates them, which is
	// what keeps those providers unchanged: no skins declared means the panel shows no
	// skin selector and sends none. Unknown ids, duplicates, an unlabelled skin, two
	// defaults or a fallback loop all refuse server construction.
	Skins []StreamSkin `json:"skins,omitempty"`
}

// StreamInputSink is one of the two ways a build can advertise streamClient.sessionInput.
// Nothing in this repository implements it; it is kept because it is the shape a Source
// that owns its own input policy would present, and because declaring it is what lets
// streamCapabilities compute sessionInput from a real capability check rather than from a
// constant somebody could flip by hand.
//
// The way input is actually reachable today is the other one: streaminput.go's
// streamInputAvailable, which needs the -stream-input flag AND a Source that implements
// StreamSessionRunner, BotControl and RenderSource together. See that file for the rules
// (view lease, current epoch, an observation at most ~5s old, one action per 300ms, the
// account's bot stopped while control is held and resumed on release, every request
// audit-logged).
type StreamInputSink interface {
	// AcquireStreamInput takes an exclusive, arbitrated action lease on the account's
	// existing session, returning the lease's acknowledged state. An implementation must
	// return false, not an error, when the session's own script/guardian policy refuses.
	AcquireStreamInput(account string, enabled bool) (acknowledged bool, err error)
}

// StreamCrowdPlaySink and StreamMLSink are the same idea for the other two advertised
// capabilities: both are computed from a real interface assertion so neither can be
// advertised without an adapter existing. Neither is implemented anywhere today.
type StreamCrowdPlaySink interface {
	StreamCrowdPlayAvailable(account string) bool
}

type StreamMLSink interface {
	StreamMLRecordingAvailable(account string) bool
}

// streamFrame is one observation handed to the browser.
type streamFrame struct {
	// Seq counts observations within one view session, so a provider can drop a frame
	// that arrives out of order and an ML/CrowdPlay adapter can later key an action to
	// the exact observation it was decided from.
	Seq int64 `json:"seq"`
	// Epoch identifies the SESSION the observation came from. It changes whenever the
	// account's connection changes underneath the viewer (a logout, a drop, a
	// reconnect), which is exactly when queued clicks and stale frames become dangerous.
	// A frame request carrying a stale epoch is refused rather than answered.
	Epoch      int64                     `json:"epoch"`
	Account    string                    `json:"account"`
	CapturedAt int64                     `json:"capturedAt"`
	Snapshot   *gamestate.RenderSnapshot `json:"snapshot,omitempty"`
	// Scene and Delta are the "scene-delta" transport's payloads (streamscene.go): Scene is
	// a whole compact scene (the first frame, a resync, or a base the browser did not
	// confirm), Delta only what changed since the frame whose seq is Delta.Base. Exactly one
	// is set on a scene-delta observation; neither on a full-snapshot one.
	Scene *streamScene `json:"scene,omitempty"`
	Delta *streamDelta `json:"delta,omitempty"`
	// Tick is the session's scene tick at this observation (scene-delta only), so a
	// provider can tell a new game tick from a re-observation of the same one.
	Tick int `json:"tick,omitempty"`
	// Throttled marks a reply that carries no new observation because the caller asked
	// again inside the server's own minimum interval. RetryInMs says when the next one
	// is due. This is a 200, not an error: pacing is normal and expected.
	Throttled bool  `json:"throttled,omitempty"`
	RetryInMs int64 `json:"retryInMs,omitempty"`
	// Lapsed marks the first reply after the view's lease had run past its TTL and was resumed
	// because nobody else took it. Input control does not survive a lapse (the bot has been
	// resumed), so a viewer holding it must treat it as released.
	Lapsed bool `json:"lapsed,omitempty"`
	// Waiting, set only with Throttled, says the session is still in game but has no
	// drawable scene for a moment — the map region is being rebuilt after the account
	// walked across a region boundary, which a walking account does every minute or so.
	// That is a pause, not the end of the stream: the account's connection (its epoch) is
	// unchanged, so the viewer keeps its lease and asks again.
	Waiting string `json:"waiting,omitempty"`
	// Transport repeats FrameTransport per frame so a saved/forwarded frame is never
	// separated from the fact that it is a whole snapshot.
	Transport string `json:"transport"`
}

// streamView is the one live view lease. Exactly one exists at a time on purpose: the
// owner's own requirement is that ONE account gets a rendered window while the rest of
// the fleet stays headless, and an unbounded number of browser tabs each pulling
// full-scene snapshots off live sessions is precisely the cost that requirement exists
// to avoid.
type streamView struct {
	id      string
	account string
	// owner is the opening page's own random token (the open request's owner). A page that
	// reloads, or reopens after a dropped connection, presents the same token and takes its own
	// lease back at once; any other holder of a fresh lease still refuses a different viewer.
	owner string
	epoch int64
	fps   int
	seq   int64
	// lastFrame is when the last real observation was taken (frame pacing), lastSeen
	// when the browser last asked for anything at all (lease expiry). They differ: a
	// throttled request renews the lease without producing an observation.
	lastFrame time.Time
	lastSeen  time.Time
	// transport is what this view asked for at open: streamTransportFull or
	// streamTransportScene. sent/sentSeq/lastFull are the scene-delta transport's memory:
	// the scene the server last sent (and its seq) is the only base it can diff against,
	// and lastFull is when it last sent a whole one. One view exists at a time, so this is
	// one scene of memory, not one per viewer.
	transport string
	sent      *streamScene
	sentSeq   int64
	lastFull  time.Time
}

type streamState struct {
	mu   sync.Mutex
	view *streamView
	next int64
	// epochs is account -> the epoch this process currently believes that account's
	// session is on, plus the connection signature that epoch was derived from.
	epochs map[string]streamEpoch
	// checked is when epochs was last refreshed from the Source. Deriving an epoch means
	// listing every account, so it is refreshed on a short interval rather than on every
	// single frame request: at the bridge's own frame rate a per-frame full account list
	// across a 255-account fleet would cost more than the observation it guards.
	checked time.Time
	// status is the dashboard status line (bot, activity, hitpoints) of each account whose
	// epoch was derived, captured from the same account-list walk at no extra cost, for the
	// scene-delta transport's status section.
	status map[string]streamStatus
	// obs is the bounded ring of recent observation costs (streamObservation) and obsN the
	// total ever recorded, served read-only at GET /api/stream/stats. See that endpoint's
	// doc comment for why the cost of an observation, not the size of a frame, is the thing
	// that decides whether DefaultStreamMaxFPS can honestly be raised.
	obs  [streamObsRing]streamObservation
	obsN int64
	// throttled and waiting count the replies that carried no new observation: a caller
	// asking again inside the server's own interval, and the transient scene-rebuild window.
	throttled, waiting int64
}

// streamObsRing is how many recent observations /api/stream/stats keeps. At
// DefaultStreamMaxFPS this is about a minute and a half of history, and at 30/s about
// seventeen seconds — enough to characterise a measurement run without the ring itself
// becoming a cost. One entry is 6 machine words; the whole ring is fixed-size and lives in
// the existing streamState, so nothing here allocates per frame.
const streamObsRing = 512

// streamObservation is what one served observation cost. Recorded under the lock the frame
// handler already takes, so it adds no new synchronisation.
//
// Honest limit: SnapshotUs is wall time around the Source's RenderSnapshot, so on a busy
// process it includes time spent waiting for that one session's lock, not only the deep
// copy. That is the right number for "can the bridge serve N of these a second", and the
// wrong number for "how much CPU did the copy itself use" — do not read it as the latter.
type streamObservation struct {
	// At is when the observation was taken, ms since epoch.
	At int64 `json:"at"`
	// Transport is the view's transport and Kind what this frame actually carried:
	// "snapshot", "scene" (a whole scene: a first frame or a resync) or "delta".
	Transport string `json:"transport"`
	Kind      string `json:"kind"`
	// SnapshotUs is the Source's RenderSnapshot, ProjectUs the scene projection and diff
	// (0 on the full-snapshot transport), EncodeUs the JSON encoding, all microseconds.
	SnapshotUs int64 `json:"snapshotUs"`
	ProjectUs  int64 `json:"projectUs"`
	EncodeUs   int64 `json:"encodeUs"`
	// Bytes is the exact encoded frame body this observation produced.
	Bytes int `json:"bytes"`
}

type streamEpoch struct {
	epoch     int64
	signature string
	since     int64
}

// streamEpochSinceTolerance is how far an in-game account's reported SinceUnixMs may move
// without counting as a new connection. FleetSource derives it for a connected account as
// "now minus connected seconds" on every snapshot, so it jitters by the gap between the
// fleet snapshot and that "now" — a few ms to a few hundred. Compared exactly, that
// jitter changed the epoch on nearly every 500ms re-derivation and ended every stream on a
// REAL fleet within a second (measured against the live panel; the -fake source reports a
// stable value, which is why fixtures never saw it). A genuine reconnect moves it by the
// whole previous session's length and normally changes the state or reconnect count too.
const streamEpochSinceTolerance = 5 * time.Second

const streamEpochCheckInterval = 500 * time.Millisecond

// streamSceneRetry is how soon a viewer asks again while an in-game account's scene is
// being rebuilt (see streamFrame.Waiting).
const streamSceneRetry = 500 * time.Millisecond

// maxStreamOwnerBytes bounds a page's lease-owner token (a random UUID in practice).
const maxStreamOwnerBytes = 64

// streamCapabilities computes /api/meta.streamClient. Called per /api/meta request; all
// of it is cheap interface assertions plus one already-parsed manifest.
func (s *Server) streamCapabilities() *StreamClientCapabilities {
	_, existing := s.src.(RenderSource)
	_, input := s.src.(StreamInputSink)
	crowd := false
	if c, ok := s.src.(StreamCrowdPlaySink); ok {
		crowd = c.StreamCrowdPlayAvailable("")
	}
	ml := false
	if m, ok := s.src.(StreamMLSink); ok {
		ml = m.StreamMLRecordingAvailable("")
	}
	caps := &StreamClientCapabilities{
		SceneRenderer:   s.streamRenderer,
		Description:     s.streamDescription,
		ExistingSession: existing,
		// Input can never be advertised without BOTH a sink and a session to apply it
		// to; a sink with no observable session would be input into the dark.
		SessionInput:    (input && existing) || s.streamInputAvailable(),
		CrowdPlay:       crowd,
		MLRecording:     ml,
		MaxFPS:          s.streamMaxFPS(),
		Skins:           s.resolvedStreamSkins(),
		Skin:            s.streamDefaultSkin,
		FrameTransport:  streamTransportFull,
		FrameTransports: streamTransports,
	}
	return caps
}

func (s *Server) streamMaxFPS() int {
	if s.cfg.StreamMaxFPS > 0 {
		return s.cfg.StreamMaxFPS
	}
	return DefaultStreamMaxFPS
}

func (s *Server) streamLeaseTTL() time.Duration {
	if s.cfg.StreamLeaseTTL > 0 {
		return s.cfg.StreamLeaseTTL
	}
	return DefaultStreamLeaseTTL
}

// loadStreamManifest reads and validates the sidecar beside an installed provider bundle.
// A bundle with no readable, well-formed manifest declaring a known renderer identity is
// a configuration error the caller reports at startup — never a bundle that is served
// anyway with the capability quietly blank, which would leave the panel disabled with no
// explanation.
func loadStreamManifest(bundle string) (streamManifest, error) {
	var m streamManifest
	if bundle == "" {
		return m, nil
	}
	if _, err := os.Stat(bundle); err != nil {
		return m, fmt.Errorf("stream client bundle %s: %w", bundle, err)
	}
	path := bundle + ".manifest.json"
	data, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("stream client manifest %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("stream client manifest %s: %w", path, err)
	}
	if !streamRendererIdentities[m.SceneRenderer] {
		return m, fmt.Errorf("stream client manifest %s declares renderer %q, which this build does not know; expected one of %q", path, m.SceneRenderer, knownStreamRenderers)
	}
	return m, nil
}

// handleStreamRenderer serves the one installed provider module at the fixed same-origin
// path the panel imports. Same shape as handleCaptureRenderer: an explicit on-disk file,
// never a directory root and never a URL out of a saved layout.
func (s *Server) handleStreamRenderer(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.cfg.StreamClientBundle == "" {
		writeError(w, http.StatusNotImplemented, "no stream client provider is installed on this build")
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.cfg.StreamClientBundle)
}

// handleStreamAsset serves files a provider module sits beside — its own shaders, helper
// modules or manifest — from the same directory as the installed bundle and nowhere else.
// Kept deliberately narrow: one directory, no traversal, GET only.
func (s *Server) handleStreamAsset(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.cfg.StreamClientBundle == "" {
		writeError(w, http.StatusNotImplemented, "no stream client provider is installed on this build")
		return
	}
	name := filepath.Base(r.URL.Path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(filepath.Dir(s.cfg.StreamClientBundle), name)
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if ct, ok := assetTypes[filepath.Ext(name)]; ok {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, full)
}

type streamSessionBody struct {
	Session   string `json:"session"`
	Account   string `json:"account"`
	Epoch     int64  `json:"epoch"`
	FPS       int    `json:"fps"`
	MaxFPS    int    `json:"maxFps"`
	LeaseMs   int64  `json:"leaseMs"`
	Transport string `json:"transport"`
}

// handleStreamOpen takes the single exclusive view lease for one account. It never
// connects anything: the account must ALREADY be in game, and the only thing this proves
// it can do is read a snapshot off that existing session.
func (s *Server) handleStreamOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	rs, ok := s.src.(RenderSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet cannot observe an existing session")
		return
	}
	var req struct {
		Account   string `json:"account"`
		FPS       int    `json:"fps"`
		Force     bool   `json:"force"`
		Transport string `json:"transport"`
		Owner     string `json:"owner"`
	}
	if !readJSONBody(w, r, 1024, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, "account is required")
		return
	}
	if len(req.Owner) > maxStreamOwnerBytes {
		writeError(w, http.StatusBadRequest, "owner is too long")
		return
	}
	switch req.Transport {
	case "":
		req.Transport = streamTransportFull
	case streamTransportFull, streamTransportScene:
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown frame transport %q; this build serves %q", req.Transport, streamTransports))
		return
	}
	epoch, err := s.streamEpochFor(req.Account)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// One real observation before handing out a lease, so "the stream opened" can never
	// mean "the account turned out to have no scene at all".
	if _, err := rs.RenderSnapshot(req.Account); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	now := s.opts.now()
	s.stream.mu.Lock()
	defer s.stream.mu.Unlock()
	// A view replaced by this one (taken over, or expired) gives up any input control it
	// held, which resumes the bot it paused. Runs before the unlock; release takes only the
	// input lock.
	if old := s.stream.view; old != nil {
		oldID := old.id
		defer func() {
			if s.stream.view == nil || s.stream.view.id != oldID {
				s.releaseStreamControl(oldID, "view replaced")
			}
		}()
	}
	if v := s.stream.view; v != nil && !req.Force && !(req.Owner != "" && v.owner == req.Owner) && now.Sub(v.lastSeen) < s.streamLeaseTTL() {
		writeError(w, http.StatusConflict, fmt.Sprintf("another viewer holds the stream client lease on %q; close it first, or retry with force", v.account))
		return
	}
	s.stream.next++
	view := &streamView{
		id:        strconv.FormatInt(now.UnixNano(), 36) + "-" + strconv.FormatInt(s.stream.next, 36),
		account:   req.Account,
		owner:     req.Owner,
		epoch:     epoch,
		fps:       clampStreamFPS(req.FPS, s.streamMaxFPS()),
		lastSeen:  now,
		lastFrame: time.Time{},
		transport: req.Transport,
	}
	s.stream.view = view
	writeJSON(w, http.StatusOK, streamSessionBody{
		Session:   view.id,
		Account:   view.account,
		Epoch:     view.epoch,
		FPS:       view.fps,
		MaxFPS:    s.streamMaxFPS(),
		LeaseMs:   s.streamLeaseTTL().Milliseconds(),
		Transport: view.transport,
	})
}

// handleStreamClose releases the view lease. Idempotent, and it refuses to release
// somebody else's lease so a stale tab shutting down cannot cut off a live viewer.
func (s *Server) handleStreamClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if !readJSONBody(w, r, 1024, &req) {
		return
	}
	s.stream.mu.Lock()
	if v := s.stream.view; v != nil && v.id == req.Session {
		s.stream.view = nil
	}
	s.stream.mu.Unlock()
	s.releaseStreamControl(req.Session, "view closed")
	writeJSON(w, http.StatusOK, connActionBody{true})
}

// handleStreamFrame answers one observation from the existing session, paced by the
// server's own maximum rate and guarded by the lease and the session epoch.
func (s *Server) handleStreamFrame(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rs, ok := s.src.(RenderSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet cannot observe an existing session")
		return
	}
	id := r.URL.Query().Get("session")
	if id == "" {
		writeError(w, http.StatusBadRequest, "session is required")
		return
	}
	now := s.opts.now()
	s.stream.mu.Lock()
	view := s.stream.view
	if view == nil || view.id != id {
		s.stream.mu.Unlock()
		writeError(w, http.StatusGone, "this stream client lease is no longer held; open a new one")
		return
	}
	// A lease whose TTL has passed is still its holder's until somebody else opens one: the TTL
	// decides when a RIVAL may take the slot (handleStreamOpen), not whether the owner may carry
	// on. Refusing it here ended every view that was merely late (a decode longer than the TTL, a
	// throttled background tab) although nothing had replaced it. It resumes, and the reply says
	// so (streamFrame.Lapsed), because input control does not survive a lapse.
	lapsed := now.Sub(view.lastSeen) >= s.streamLeaseTTL()
	view.lastSeen = now
	account, epoch, fps, last, transport := view.account, view.epoch, view.fps, view.lastFrame, view.transport
	s.stream.mu.Unlock()
	if lapsed {
		s.releaseStreamControl(id, "view lease lapsed")
	}

	interval := time.Second / time.Duration(fps)
	if !last.IsZero() {
		if wait := interval - now.Sub(last); wait > 0 {
			s.stream.mu.Lock()
			s.stream.throttled++
			s.stream.mu.Unlock()
			writeJSON(w, http.StatusOK, streamFrame{Epoch: epoch, Account: account, Throttled: true, Lapsed: lapsed, RetryInMs: wait.Milliseconds() + 1, Transport: transport, CapturedAt: now.UnixMilli()})
			return
		}
	}
	// The epoch check comes before the snapshot, not after: a snapshot taken from a
	// session that has already been replaced is exactly the stale observation a provider
	// must never paint or decide an action from.
	current, err := s.streamEpochFor(account)
	if err != nil {
		s.dropStreamView(id)
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if current != epoch {
		s.dropStreamView(id)
		writeError(w, http.StatusConflict, "this account's session changed underneath the stream client; open a new one")
		return
	}
	snapStart := time.Now()
	snapshot, err := rs.RenderSnapshot(account)
	// Real wall time, not s.opts.now(): a test clock that does not advance would report
	// every observation as free, which is exactly the shape of measurement this repository
	// keeps catching itself making.
	snapshotUs := time.Since(snapStart).Microseconds()
	if err != nil {
		// The epoch check above already proved this is the same in-game connection, so
		// a missing scene here is the transient rebuild window, not a lost session. A
		// session that genuinely went away changes the account's state, which the next
		// epoch check turns into a 409 within streamEpochCheckInterval.
		s.stream.mu.Lock()
		s.stream.waiting++
		s.stream.mu.Unlock()
		writeJSON(w, http.StatusOK, streamFrame{Epoch: epoch, Account: account, Throttled: true, Lapsed: lapsed, RetryInMs: streamSceneRetry.Milliseconds(), Waiting: err.Error(), Transport: transport, CapturedAt: now.UnixMilli()})
		return
	}
	var scene streamScene
	var projectUs int64
	if transport == streamTransportScene {
		projectStart := time.Now()
		scene = projectStreamScene(&snapshot, s.streamStatusFor(account), s.streamNames())
		projectUs = time.Since(projectStart).Microseconds()
	}
	// have is the seq of the last scene-delta frame the browser actually applied. A delta is
	// only ever computed against that exact scene; anything else (a first frame, a lost or
	// aborted response, a tab that skipped one) gets a whole scene instead.
	have, _ := strconv.ParseInt(r.URL.Query().Get("have"), 10, 64)
	frame := streamFrame{Epoch: epoch, Account: account, Lapsed: lapsed, CapturedAt: now.UnixMilli(), Transport: transport}
	kind := "snapshot"
	s.stream.mu.Lock()
	if v := s.stream.view; v != nil && v.id == id {
		v.seq++
		v.lastFrame = now
		frame.Seq = v.seq
		if transport == streamTransportScene {
			diffStart := time.Now()
			if v.sent != nil && have > 0 && have == v.sentSeq && now.Sub(v.lastFull) < streamSceneResync {
				delta := diffStreamScene(v.sentSeq, v.sent, &scene)
				frame.Delta = &delta
				kind = "delta"
			} else {
				frame.Scene = &scene
				v.lastFull = now
				kind = "scene"
			}
			projectUs += time.Since(diffStart).Microseconds()
			frame.Tick = scene.Tick
			v.sent, v.sentSeq = &scene, v.seq
		}
	}
	s.stream.mu.Unlock()
	if frame.Seq == 0 {
		writeError(w, http.StatusGone, "this stream client lease was released while the frame was being taken")
		return
	}
	if transport != streamTransportScene {
		frame.Snapshot = &snapshot
	}
	encodeStart := time.Now()
	data, err := json.Marshal(frame)
	encodeUs := time.Since(encodeStart).Microseconds()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not encode the observation")
		return
	}
	s.recordStreamObservation(streamObservation{At: now.UnixMilli(), Transport: transport, Kind: kind,
		SnapshotUs: snapshotUs, ProjectUs: projectUs, EncodeUs: encodeUs, Bytes: len(data)})
	writeRawJSON(w, http.StatusOK, data)
}

// recordStreamObservation files one served observation in the bounded ring behind
// GET /api/stream/stats.
func (s *Server) recordStreamObservation(o streamObservation) {
	s.stream.mu.Lock()
	s.stream.obs[s.stream.obsN%streamObsRing] = o
	s.stream.obsN++
	s.stream.mu.Unlock()
}

// streamStatusFor is the account's dashboard status line as of its last epoch derivation.
func (s *Server) streamStatusFor(account string) streamStatus {
	s.stream.mu.Lock()
	defer s.stream.mu.Unlock()
	return s.stream.status[account]
}

// streamNames resolves npc names from the Source's config archive, nil when it has none.
func (s *Server) streamNames() npcLookup {
	if n, ok := s.src.(StreamNameSource); ok {
		return npcLookupFrom(n.StreamNames())
	}
	return nil
}

// handleStreamInput is the input half of the bridge. It answers 501 on every build in
// this repository, because no Source implements StreamInputSink — and that is the honest
// answer, not a stub to be filled in with a local flag. Sending arbitrated actions into a
// live session needs the lease/epoch/arbitration work docs/stream_client_panel.md item 1
// describes; until that exists, a panel button that appeared to work would be worse than
// one that is plainly disabled.
func (s *Server) handleStreamInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.streamInputAvailable() {
		s.handleStreamControl(w, r)
		return
	}
	sink, ok := s.src.(StreamInputSink)
	if !ok {
		writeError(w, http.StatusNotImplemented, "session input is not installed on this build: observation only")
		return
	}
	var req struct {
		Session string `json:"session"`
		Enabled bool   `json:"enabled"`
	}
	if !readJSONBody(w, r, 1024, &req) {
		return
	}
	s.stream.mu.Lock()
	view := s.stream.view
	held := view != nil && view.id == req.Session
	account := ""
	if held {
		account = view.account
		view.lastSeen = s.opts.now()
	}
	s.stream.mu.Unlock()
	if !held {
		writeError(w, http.StatusGone, "this stream client lease is no longer held; open a new one")
		return
	}
	acknowledged, err := sink.AcquireStreamInput(account, req.Enabled)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK           bool `json:"ok"`
		Acknowledged bool `json:"acknowledged"`
	}{true, acknowledged})
}

func (s *Server) dropStreamView(id string) {
	s.stream.mu.Lock()
	if v := s.stream.view; v != nil && v.id == id {
		s.stream.view = nil
	}
	s.stream.mu.Unlock()
	s.releaseStreamControl(id, "view dropped")
}

// streamEpochFor derives the account's current session epoch from the Source's own
// account list. An epoch is not read from internal/fleet — nothing there exposes a
// session generation — it is DERIVED here from the connection signature the panel already
// receives: the state plus the reconnect count plus the moment that state was entered.
// Any of those changing means the browser is now looking at a different connection, which
// is what makes a queued click or a half-painted frame unsafe.
//
// Cheap by construction: recomputed at most once every streamEpochCheckInterval, since
// listing a 255-account fleet per observation would cost more than the observation.
func (s *Server) streamEpochFor(account string) (int64, error) {
	now := s.opts.now()
	s.stream.mu.Lock()
	defer s.stream.mu.Unlock()
	if s.stream.epochs == nil {
		s.stream.epochs = make(map[string]streamEpoch)
	}
	if !s.stream.checked.IsZero() && now.Sub(s.stream.checked) < streamEpochCheckInterval {
		if known, ok := s.stream.epochs[account]; ok {
			if known.epoch == 0 {
				return 0, errors.New("account is not in game")
			}
			return known.epoch, nil
		}
	}
	s.stream.checked = now
	var found *AccountStatus
	accounts := s.src.Accounts(nil)
	for i := range accounts {
		if accounts[i].Name == account {
			found = &accounts[i]
			break
		}
	}
	if found == nil {
		delete(s.stream.epochs, account)
		return 0, errors.New("unknown account")
	}
	if s.stream.status == nil {
		s.stream.status = make(map[string]streamStatus)
	}
	s.stream.status[account] = statusFromAccount(found)
	if found.State != StateIngame {
		s.stream.epochs[account] = streamEpoch{}
		return 0, fmt.Errorf("account is %s, not in game", found.State)
	}
	signature := fmt.Sprintf("%s|%d", found.State, found.Reconnects)
	known := s.stream.epochs[account]
	drift := found.SinceUnixMs - known.since
	if drift < 0 {
		drift = -drift
	}
	if known.epoch == 0 || known.signature != signature || time.Duration(drift)*time.Millisecond > streamEpochSinceTolerance {
		known = streamEpoch{epoch: known.epoch + 1, signature: signature, since: found.SinceUnixMs}
		s.stream.epochs[account] = known
	}
	return known.epoch, nil
}

func clampStreamFPS(want, max int) int {
	if want <= 0 {
		return max
	}
	if want > max {
		return max
	}
	return want
}
