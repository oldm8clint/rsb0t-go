package controlpanel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// renderSource wraps a staticSource so it implements RenderSource only when a test opts
// in — the same wrap-to-opt-in pattern botControlSource/connectionControlSource already
// use, so a plain staticSource keeps answering 501 for the tests that check that.
type renderSource struct {
	*staticSource
	mu       sync.Mutex
	snapshot gamestate.RenderSnapshot
	err      error
	calls    int
}

func (r *renderSource) RenderSnapshot(account string) (gamestate.RenderSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return gamestate.RenderSnapshot{}, r.err
	}
	out := r.snapshot
	out.UI.Name = account
	return out, nil
}

func (r *renderSource) snapshots() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// inputSource additionally implements StreamInputSink, which NOTHING in the real
// repository does — it exists only so the "sessionInput is advertised from a real
// capability check, not a constant" behaviour can be tested from both sides.
type inputSource struct {
	*renderSource
	acknowledged bool
	seen         []bool
}

func (i *inputSource) AcquireStreamInput(account string, enabled bool) (bool, error) {
	i.seen = append(i.seen, enabled)
	return i.acknowledged, nil
}

func ingameAccount(name string) AccountStatus {
	return AccountStatus{Name: name, State: StateIngame, SinceUnixMs: 1000, Pos: &Position{X: 3222, Z: 3218}}
}

func streamHarness(t *testing.T, src Source, at *time.Time) *harness {
	t.Helper()
	opts := testOptions()
	if at != nil {
		opts.now = func() time.Time { return *at }
	}
	return newHarness(t, Config{}, opts, src)
}

func (h *harness) postJSON(path string, body interface{}) *http.Response {
	h.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.request(http.MethodPost, path, strings.NewReader(string(data)), http.Header{"Content-Type": []string{"application/json"}})
}

func TestMetaAdvertisesNoStreamClientWithoutAnInstalledProvider(t *testing.T) {
	// The default, and every production fleet today: no provider bundle installed and a
	// Source that cannot observe a session. The panel must be able to see that plainly.
	h := streamHarness(t, &staticSource{}, nil)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.StreamClient == nil {
		t.Fatal("meta reported no streamClient object at all; the panel needs it to explain why the panel is disabled")
	}
	if m.StreamClient.SceneRenderer != "" {
		t.Errorf("sceneRenderer = %q with no provider installed, want empty", m.StreamClient.SceneRenderer)
	}
	if m.StreamClient.ExistingSession {
		t.Error("existingSession is true for a Source that cannot observe a session")
	}
	if m.StreamClient.SessionInput || m.StreamClient.CrowdPlay || m.StreamClient.MLRecording {
		t.Errorf("input/crowdplay/ML advertised with no adapter installed: %+v", m.StreamClient)
	}
	if m.StreamClient.MaxFPS != DefaultStreamMaxFPS || m.StreamClient.FrameTransport != "full-snapshot" {
		t.Errorf("frame contract = %d fps %q, want %d fps full-snapshot", m.StreamClient.MaxFPS, m.StreamClient.FrameTransport, DefaultStreamMaxFPS)
	}
	// Every stream route must refuse rather than pretend.
	for _, path := range []string{"/stream/client.js", "/stream/helper.js"} {
		if res := h.get(path); res.StatusCode != http.StatusNotImplemented {
			t.Errorf("GET %s = %d with no provider installed, want 501", path, res.StatusCode)
		}
	}
	if res := h.postJSON("/api/stream/open", map[string]string{"account": "a"}); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("open with no RenderSource = %d, want 501", res.StatusCode)
	}
	if res := h.get("/api/stream/frame?session=x"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("frame with no RenderSource = %d, want 501", res.StatusCode)
	}
}

func TestSessionInputIsNeverAdvertisedWithoutARealSink(t *testing.T) {
	// A Source that can observe but has no input sink — every build in this repository.
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, nil)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if !m.StreamClient.ExistingSession {
		t.Error("existingSession is false for a Source that does implement RenderSource")
	}
	if m.StreamClient.SessionInput {
		t.Error("sessionInput advertised without a StreamInputSink — this is exactly the overclaim the capability check exists to prevent")
	}
	if res := h.postJSON("/api/stream/input", map[string]interface{}{"session": "x", "enabled": true}); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("input = %d without a sink, want 501", res.StatusCode)
	}

	// And the other side of the same check: a Source that really does implement the sink
	// gets it advertised, and a refused lease comes back as acknowledged:false rather
	// than as a success.
	withSink := &inputSource{renderSource: &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}}
	h2 := streamHarness(t, withSink, nil)
	decode(t, h2.get("/api/meta"), &m)
	if !m.StreamClient.SessionInput {
		t.Error("sessionInput not advertised for a Source that does implement StreamInputSink")
	}
	var open streamSessionBody
	decode(t, h2.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)
	var ack struct {
		OK           bool `json:"ok"`
		Acknowledged bool `json:"acknowledged"`
	}
	decode(t, h2.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true}), &ack)
	if !ack.OK || ack.Acknowledged {
		t.Errorf("a refusing sink reported ok=%v acknowledged=%v, want ok with acknowledged=false", ack.OK, ack.Acknowledged)
	}
	if len(withSink.seen) != 1 || !withSink.seen[0] {
		t.Errorf("the sink saw %v, want one enable request", withSink.seen)
	}
}

func TestInstalledProviderManifestDrivesTheAdvertisedRenderer(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "client.js")
	if err := os.WriteFile(bundle, []byte("export async function mount(){ throw Error('fixture'); }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(manifest string) {
		if err := os.WriteFile(bundle+".manifest.json", []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A bundle with no manifest at all must fail construction, not serve a provider the
	// panel can never identify.
	if _, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", StreamClientBundle: bundle}, &staticSource{}, testOptions()); err == nil {
		t.Fatal("a provider bundle with no manifest was accepted")
	}

	// A manifest claiming a renderer identity this build does not know must fail too:
	// the panel rejects a provider whose returned identity does not match the advertised
	// one, so an unknown string would surface as an unexplainable browser mismatch.
	write(`{"sceneRenderer":"opengl-someday"}`)
	_, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", StreamClientBundle: bundle}, &staticSource{}, testOptions())
	if err == nil || !strings.Contains(err.Error(), "opengl-someday") {
		t.Fatalf("unknown renderer identity accepted or reported unclearly: %v", err)
	}

	write(`{"sceneRenderer":"webgl-tile-geometry","description":"Diagnostic live tile geometry, not the authentic 2004 scene"}`)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := newHarness(t, Config{StreamClientBundle: bundle}, testOptions(), src)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.StreamClient.SceneRenderer != StreamRendererTileGeometry {
		t.Errorf("sceneRenderer = %q, want the identity the installed manifest declares", m.StreamClient.SceneRenderer)
	}
	if m.StreamClient.SceneRenderer == StreamRendererWebGL3D {
		t.Error("a tile-geometry provider was advertised as the authentic webgl3d scene renderer")
	}
	res := h.get("/stream/client.js")
	if res.StatusCode != http.StatusOK || !strings.Contains(bodyText(t, res), "mount") {
		t.Errorf("the installed bundle was not served at its fixed same-origin path: %d", res.StatusCode)
	}
	// A sibling file is served; anything outside that one directory is not.
	if err := os.WriteFile(filepath.Join(dir, "helper.js"), []byte("export const x = 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := h.get("/stream/helper.js"); res.StatusCode != http.StatusOK {
		t.Errorf("a file beside the bundle was not served: %d", res.StatusCode)
	}
	if res := h.get("/stream/missing.js"); res.StatusCode != http.StatusNotFound {
		t.Errorf("a file that does not exist answered %d, want 404", res.StatusCode)
	}
}

// The native cache-terrain provider shipped in tools/stream must be installable: an
// earlier build knew only webgl3d and webgl-tile-geometry, so -stream-client pointing at
// it refused to start the whole control panel. It also needs the capture adapter, so
// installing it without the capture flags is a startup error rather than a dead panel.
func TestCacheTerrainProviderIsKnownAndNeedsTheCaptureAdapter(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "cache-terrain-client.js")
	if err := os.WriteFile(bundle, []byte("export async function mount(){ throw Error('fixture'); }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+".manifest.json", []byte(`{"sceneRenderer":"webgl-cache-terrain","description":"Partial native terrain view"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", StreamClientBundle: bundle}, &staticSource{}, testOptions())
	if err == nil || !strings.Contains(err.Error(), "-capture-renderer") {
		t.Fatalf("cache-terrain provider without the capture adapter accepted or reported unclearly: %v", err)
	}
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := newHarness(t, Config{StreamClientBundle: bundle, CaptureRenderer: filepath.Join(dir, "client.js"), CaptureCacheDir: dir}, testOptions(), src)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.StreamClient.SceneRenderer != StreamRendererCacheTerrain {
		t.Errorf("sceneRenderer = %q, want %q", m.StreamClient.SceneRenderer, StreamRendererCacheTerrain)
	}
	if m.StreamClient.SessionInput {
		t.Error("installing a view provider advertised session input")
	}
	// The provider runs the capture adapter inside the dashboard page, and the adapter's
	// cache decoder loads sprites as blob: images — without this the decoder hangs.
	if csp := h.get("/").Header.Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data: blob:") {
		t.Errorf("dashboard CSP with a cache-terrain provider installed does not allow blob: images: %s", csp)
	}
	plain := newHarness(t, Config{}, testOptions(), &staticSource{})
	if csp := plain.get("/").Header.Get("Content-Security-Policy"); strings.Contains(csp, "blob:") {
		t.Errorf("dashboard CSP relaxed on a build with no stream provider: %s", csp)
	}
}

// The provider files shipped in this repository must each declare an identity this
// build accepts, so a launcher pointing -stream-client at one cannot fail to start.
func TestShippedStreamProviderManifestsAreKnown(t *testing.T) {
	for _, bundle := range []string{"../../tools/stream/tile-client.js", "../../tools/stream/cache-terrain-client.js"} {
		m, err := loadStreamManifest(bundle)
		if err != nil {
			t.Errorf("%s: %v", bundle, err)
			continue
		}
		if m.SceneRenderer == StreamRendererWebGL3D {
			t.Errorf("%s claims the authentic webgl3d renderer", bundle)
		}
	}
}

func TestStreamBridgeServesSequencedFramesFromTheExistingSession(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{
		staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("fisher1"), {Name: "offline1", State: StateOffline}}},
		snapshot:     gamestate.RenderSnapshot{X: 3222, Z: 3218, Level: 0, CentreX: 3216, CentreZ: 3216},
	}
	h := streamHarness(t, src, &at)

	// An account that is not in game can never be observed, so a lease is never handed out.
	if res := h.postJSON("/api/stream/open", map[string]string{"account": "offline1"}); res.StatusCode != http.StatusConflict {
		t.Errorf("open on an offline account = %d, want 409", res.StatusCode)
	}
	if res := h.postJSON("/api/stream/open", map[string]string{"account": "nobody"}); res.StatusCode != http.StatusConflict {
		t.Errorf("open on an unknown account = %d, want 409", res.StatusCode)
	}

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1", "fps": 60}), &open)
	if open.Session == "" || open.Account != "fisher1" || open.Epoch == 0 {
		t.Fatalf("open returned %+v", open)
	}
	// The browser asked for 60; the server caps it at what it will actually serve, and
	// says so rather than silently agreeing.
	if open.FPS != DefaultStreamMaxFPS || open.MaxFPS != DefaultStreamMaxFPS {
		t.Errorf("fps = %d (max %d), want both clamped to %d", open.FPS, open.MaxFPS, DefaultStreamMaxFPS)
	}
	if open.Transport != "full-snapshot" {
		t.Errorf("transport = %q, want the honest full-snapshot label", open.Transport)
	}

	var frame streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	if frame.Seq != 1 || frame.Snapshot == nil || frame.Snapshot.X != 3222 || frame.Throttled {
		t.Fatalf("first frame = %+v", frame)
	}
	if frame.Epoch != open.Epoch {
		t.Errorf("frame epoch %d != session epoch %d", frame.Epoch, open.Epoch)
	}

	// Asking again inside the server's own minimum interval is paced, not answered with
	// a fresh full-scene copy — and it is a 200 with a retry hint, not an error.
	before := src.snapshots()
	var paced streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &paced)
	if !paced.Throttled || paced.RetryInMs <= 0 || paced.Snapshot != nil {
		t.Fatalf("a too-early frame request was not paced: %+v", paced)
	}
	if src.snapshots() != before {
		t.Errorf("a throttled request still took %d snapshot(s) off the live session", src.snapshots()-before)
	}

	at = at.Add(time.Second)
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	if frame.Seq != 2 || frame.Snapshot == nil {
		t.Fatalf("second frame = %+v", frame)
	}

	// Closing releases the lease; a frame request afterwards is gone, not silently
	// re-opened against the live session.
	h.postJSON("/api/stream/close", map[string]string{"session": open.Session})
	if res := h.get("/api/stream/frame?session=" + open.Session); res.StatusCode != http.StatusGone {
		t.Errorf("frame after close = %d, want 410", res.StatusCode)
	}
}

func TestStreamViewLeaseIsExclusiveAndExpires(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a"), ingameAccount("b")}}}
	h := streamHarness(t, src, &at)

	var first streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &first)

	// One rendered account at a time is the whole point: a second viewer is refused
	// while the first lease is live, and told which account holds it.
	res := h.postJSON("/api/stream/open", map[string]string{"account": "b"})
	body := bodyText(t, res)
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, `a`) || !strings.Contains(body, "holds the stream client lease") {
		t.Errorf("a second concurrent viewer was not refused clearly: %d %s", res.StatusCode, body)
	}

	// A tab that was closed, crashed or left behind must not hold the single slot for
	// ever: the lease expires once nobody has asked for a frame within its TTL.
	at = at.Add(DefaultStreamLeaseTTL + time.Second)
	var second streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "b"}), &second)
	if second.Session == "" || second.Session == first.Session {
		t.Fatalf("an expired lease did not free the view slot: %+v", second)
	}
	if res := h.get("/api/stream/frame?session=" + first.Session); res.StatusCode != http.StatusGone {
		t.Errorf("the superseded lease still served frames: %d", res.StatusCode)
	}

	// An operator who knows the other viewer is gone can take it over deliberately.
	at = at.Add(time.Second)
	var forced streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "a", "force": true}), &forced)
	if forced.Session == "" || forced.Session == second.Session {
		t.Fatalf("force did not take the lease over: %+v", forced)
	}
}

func TestStreamFrameRefusesAStaleSessionEpoch(t *testing.T) {
	// The reason this exists: a frame taken from a session that has already been
	// replaced by a reconnect is exactly the stale observation a provider must never
	// paint, and never decide an action from.
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, &at)

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)
	var frame streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	if frame.Seq != 1 {
		t.Fatalf("first frame = %+v", frame)
	}

	// The account dropped and came back: same name, same "ingame" state, different
	// connection. Only the signature (reconnect count and the moment the state was
	// entered) says so.
	src.accounts[0].Reconnects = 1
	src.accounts[0].SinceUnixMs = 2000
	at = at.Add(time.Second + streamEpochCheckInterval)

	res := h.get("/api/stream/frame?session=" + open.Session)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("a frame across a reconnect = %d, want 409", res.StatusCode)
	}
	if !strings.Contains(bodyText(t, res), "changed underneath") {
		t.Error("the epoch refusal did not say what actually happened")
	}
	// The lease is dropped with it, so the browser cannot keep polling a session that
	// no longer exists.
	if res := h.get("/api/stream/frame?session=" + open.Session); res.StatusCode != http.StatusGone {
		t.Errorf("the lease survived its own epoch change: %d", res.StatusCode)
	}

	// A new lease sees the new epoch, and it is genuinely different from the old one.
	var again streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &again)
	if again.Epoch == open.Epoch {
		t.Errorf("epoch %d did not change across a reconnect", again.Epoch)
	}
}

// On the real fleet a connected account's SinceUnixMs is "now minus connected seconds",
// recomputed per snapshot, so it jitters by a few hundred ms. That jitter must not look like
// a reconnect: compared exactly, it ended every live stream within a second.
func TestStreamEpochToleratesSinceJitterButNotAReconnect(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, &at)
	base := src.accounts[0].SinceUnixMs

	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)
	for i, jitter := range []int64{3, -180, 420, -7, 250} {
		src.accounts[0].SinceUnixMs = base + jitter
		at = at.Add(time.Second + streamEpochCheckInterval)
		res := h.get("/api/stream/frame?session=" + open.Session)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("frame %d with %dms of since jitter = %d, want 200: %s", i, jitter, res.StatusCode, bodyText(t, res))
		}
	}
	// A real reconnect with the same state and reconnect count still moves "since" by the
	// whole previous session, and is refused.
	src.accounts[0].SinceUnixMs = base + 10*60*1000
	at = at.Add(time.Second + streamEpochCheckInterval)
	if res := h.get("/api/stream/frame?session=" + open.Session); res.StatusCode != http.StatusConflict {
		t.Errorf("a frame across a 10-minute since jump = %d, want 409", res.StatusCode)
	}
}

// A walking account's scene is briefly unavailable while its map region rebuilds. That is
// a pause on the same connection, not the end of the stream: the frame says to wait, and
// the lease survives to serve the next real observation.
func TestStreamWaitsThroughASceneRebuild(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, &at)
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)

	src.mu.Lock()
	src.err = errors.New("account scene is unavailable")
	src.mu.Unlock()
	at = at.Add(time.Second)
	var waiting streamFrame
	res := h.get("/api/stream/frame?session=" + open.Session)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("frame during a scene rebuild = %d, want 200 with a wait", res.StatusCode)
	}
	decode(t, res, &waiting)
	if !waiting.Throttled || waiting.Waiting == "" || waiting.RetryInMs <= 0 || waiting.Snapshot != nil {
		t.Fatalf("frame during a scene rebuild = %+v, want throttled with a reason and no snapshot", waiting)
	}

	src.mu.Lock()
	src.err = nil
	src.mu.Unlock()
	at = at.Add(time.Second)
	var frame streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &frame)
	if frame.Seq != 1 || frame.Snapshot == nil {
		t.Errorf("after the rebuild = %+v, want the first real observation on the same lease", frame)
	}
}

func TestStreamEpochIsNotRederivedPerFrame(t *testing.T) {
	// Deriving an epoch means listing every account. At the bridge's own frame rate that
	// would cost more than the observation it guards on a 255-account fleet, so it is
	// cached for streamEpochCheckInterval. This test guards that it stays cached.
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	h := streamHarness(t, src, &at)
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "a"}), &open)
	listed := src.samples()
	for i := 0; i < 5; i++ {
		at = at.Add(time.Millisecond)
		h.get("/api/stream/frame?session=" + open.Session)
	}
	if grew := src.samples() - listed; grew != 0 {
		t.Errorf("the fleet account list was walked %d more time(s) inside one epoch-check interval", grew)
	}
}

func TestFakeSourceServesASyntheticSnapshotForTheStreamBridge(t *testing.T) {
	// -fake is how the whole chain (bridge, lease, provider contract, panel) is
	// exercisable with no game connection at all. The values are synthetic; the SHAPE
	// has to be the real one or the fixture proves nothing about a real provider.
	f := NewFakeSource(4, 7)
	var name string
	for _, a := range f.Accounts(nil) {
		if a.State == StateIngame {
			name = a.Name
			break
		}
	}
	if name == "" {
		// The simulated fleet ramps; drive it until something is in game.
		for i := 0; i < 200 && name == ""; i++ {
			f.Step()
			for _, a := range f.Accounts(nil) {
				if a.State == StateIngame {
					name = a.Name
					break
				}
			}
		}
	}
	if name == "" {
		t.Skip("the simulated fleet never reached an in-game account")
	}
	snap, err := f.RenderSnapshot(name)
	if err != nil {
		t.Fatalf("RenderSnapshot(%q): %v", name, err)
	}
	if snap.UI.Version != 1 || len(snap.NPCs) == 0 || len(snap.Locs) == 0 {
		t.Fatalf("synthetic snapshot is not shaped like a real one: %+v", snap)
	}
	// A real snapshot's centre is REBUILD_NORMAL's zone coordinate (8-tile units), which
	// the capture adapter replays as that packet; a tile-unit centre sends it off-world.
	if snap.CentreX != snap.X/8 || snap.CentreZ != snap.Z/8 {
		t.Errorf("scene centre %d,%d is not the zone containing %d,%d, unlike a real snapshot", snap.CentreX, snap.CentreZ, snap.X, snap.Z)
	}
	if _, err := f.RenderSnapshot("nobody-at-all"); err == nil {
		t.Error("the fake invented a snapshot for an account it does not have")
	}
}

func TestStreamFrameSizeIsMeasuredNotAssumed(t *testing.T) {
	// Not a pass/fail threshold — a recorded measurement. DefaultStreamMaxFPS exists
	// because a frame is a whole scene copy, and the only honest way to talk about that
	// cost is to have measured one. Printed so a future change to RenderSnapshot shows
	// up here in `go test -v` rather than being discovered live.
	snap := gamestate.RenderSnapshot{X: 3222, Z: 3218, CentreX: 3216, CentreZ: 3216, UI: gamestate.RenderUI{Version: 1}}
	for i := 0; i < 32; i++ {
		snap.NPCs = append(snap.NPCs, gamestate.NPC{Index: i, Type: 1000 + i, X: 3220 + i, Z: 3210 + i, Anim: -1, FaceEntity: -1})
	}
	for i := 0; i < 8; i++ {
		snap.Players = append(snap.Players, gamestate.RenderPlayer{Index: i, X: 3220 + i, Z: 3210 + i, Appearance: make([]byte, 64)})
	}
	for i := 0; i < 200; i++ {
		snap.Locs = append(snap.Locs, gamestate.LocInstance{Type: 1276, Shape: 10, X: 3200 + i%64, Z: 3200 + i/64})
	}
	data, err := json.Marshal(streamFrame{Seq: 1, Epoch: 1, Account: "fisher1", Snapshot: &snap, Transport: "full-snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	perSecond := len(data) * DefaultStreamMaxFPS
	t.Logf("one full-snapshot frame (32 npcs, 8 players, 200 locs) encodes to %d bytes; %d of them a second is %s",
		len(data), DefaultStreamMaxFPS, fmt.Sprintf("%.1f KB/s", float64(perSecond)/1024))
	if len(data) == 0 {
		t.Fatal("a frame encoded to nothing")
	}
}
